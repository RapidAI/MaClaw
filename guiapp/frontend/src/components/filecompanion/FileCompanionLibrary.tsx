import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
    AddFileCompanionGroupMember,
    CreateFileCompanionGroup,
    ImportFileCompanionShare,
    DeleteFileCompanionGroup,
    DeleteFileCompanionProject,
    DeleteFileCompanionShare,
    LeaveFileCompanionGroup,
    ListFileCompanionGroups,
    ListFileCompanionLibrary,
    RemoveFileCompanionGroupMember,
    ShareFileCompanionDocument,
    ShowItemInFolder,
    SyncFileCompanionShare,
    UpdateFileCompanionGroup,
    UpdateFileCompanionShare,
    TransferFileCompanionShareHost,
    RevokeFileCompanionShareHost,
    FileCompanionExportProject,
} from "../../../wailsjs/go/main/App";
import { useDialog } from "../CustomDialog";
import { loadPdfMarkCounts, loadPdfMarks, pdfMarkPathKey, savePdfMarks } from "./pdfMarks";
import { loadChatMarks, saveChatMarks } from "./chatMarks";
import { loadPdfAnnotations } from "./pdfTranslations";
import { savePdfTranslations } from "./pdfTranslations";
import { loadPdfDrawings, savePdfDrawings } from "./pdfDrawings";

export type CompanionLibraryItem = {
    id: string;
    name: string;
    path: string;
    source: string;
    openedAt: string;
    owner: string;
    shareUrl: string;
    expiresAt: string;
    visibility: string;
    size: number;
    permanent: boolean;
    mine: boolean;
    // admin is the share creator; host is the current host seat for this
    // viewer. The creator keeps admin rights after transferring the host seat.
    admin?: boolean;
    host?: boolean;
    hostLabel?: string;
    pendingHost?: boolean;
    pendingHostLabel?: string;
    revision: number;
    target: string;
    groupId: string;
    // expired is derived by the backend on every list. An expired cloud row
    // keeps the project here but no longer counts as an active share.
    expired: boolean;
    highlights: string;
    // chatMarks and annotations are set on an import that unpacked a project
    // carrying answer highlights or placed notes. They are not stored on the
    // library row.
    chatMarks?: string;
    annotations?: string;
    // drawings is the same import-only value for freehand marks.
    drawings?: string;
    // Notes are other readers' discussion carried on each cloud row.
    notes: CompanionShareNote[];
    // token is the share identity on the hub. A cloud row's id is the token,
    // but a local row has none, so the edit dialog reads it here.
    token: string;
};

type ShareAudience = "public" | "private" | "person" | "group";

export type CompanionShareNote = {
    id: string;
    author: string;
    text: string;
    createdAt: string;
    // host marks the share owner's notes, including the owner agent's answer.
    // replyTo names the reader note that answer belongs to.
    host?: boolean;
    replyTo?: string;
};

export type CompanionShareBinding = {
    path: string;
    token: string;
    mine: boolean;
    notes: CompanionShareNote[];
    revision: number;
};

export type CompanionShareUpdate = {
    path: string;
    highlights: string;
    // chatMarks is present only when the host's project carries answer
    // highlights. A legacy project omits it and leaves local marks alone.
    chatMarks: string;
    chatMarksKnown: boolean;
    // annotations is the host's placed notes, also optional for legacy
    // projects so local notes survive.
    annotations: string;
    annotationsKnown: boolean;
    // drawings is the host's freehand strokes, optional for the same reason.
    drawings: string;
    drawingsKnown: boolean;
    messages: unknown[];
    project: boolean;
    replaceMessages: boolean;
};

// CompanionShareRow is the share a library row carries: who can see it and how
// long it lasts. The title bar reads it to echo the share already on screen.
export type CompanionShareRow = {
    path: string;
    token: string;
    mine: boolean;
    admin: boolean;
    host: boolean;
    hostLabel: string;
    pendingHost: boolean;
    pendingHostLabel: string;
    // owner is the share creator's label. hostLabel names the current host.
    owner: string;
    name: string;
    visibility: string;
    expiresAt: string;
    shareUrl: string;
    target: string;
    groupId: string;
};

type CompanionGroupMember = { contact: string; status: string };

const companionGroupIcons = ["book", "people", "star", "pen", "leaf", "flask"] as const;

type CompanionGroupIcon = (typeof companionGroupIcons)[number];

const companionGroupIconLabel: Record<CompanionGroupIcon, string> = {
    book: "书",
    people: "同伴",
    star: "星",
    pen: "笔",
    leaf: "叶",
    flask: "实验",
};

function companionGroupIcon(value: string): CompanionGroupIcon {
    return (companionGroupIcons as readonly string[]).includes(value) ? value as CompanionGroupIcon : "book";
}

function companionGroupNameError(name: string): string {
    const cleaned = name.trim();
    if (!cleaned) return "请填写群组名称";
    if ([...cleaned].length > 40) return "群组名称过长";
    return "";
}

type CompanionGroup = {
    id: string;
    name: string;
    owner: string;
    admin: boolean;
    icon: CompanionGroupIcon;
    members: CompanionGroupMember[];
};

type GroupEdit = { name: string; icon: string };

type ShareSpan = "forever" | "days" | "months";

type LibraryToast = (text: string, kind: "success" | "error", durationMs: number) => void;

function textOf(record: Record<string, unknown>, camel: string, pascal: string): string {
    const value = record[camel] ?? record[pascal];
    return typeof value === "string" ? value : "";
}

function boolOf(record: Record<string, unknown>, camel: string, pascal: string): boolean {
    const value = record[camel] ?? record[pascal];
    return value === true;
}

function numberOf(record: Record<string, unknown>, camel: string, pascal: string): number {
    const value = record[camel] ?? record[pascal];
    return typeof value === "number" ? value : 0;
}

// companionHostFlag reads the host seat the way the desktop does: hostKnown
// says the Hub answered, so a false host is real and not a missing field. A row
// written before host transfer existed has neither field, and its creator was
// the host, so it falls back to mine.
function companionHostFlag(record: Record<string, unknown>): boolean {
    if (Object.prototype.hasOwnProperty.call(record, "hostKnown") || Object.prototype.hasOwnProperty.call(record, "HostKnown")) {
        return boolOf(record, "hostKnown", "HostKnown") && boolOf(record, "host", "Host");
    }
    if (Object.prototype.hasOwnProperty.call(record, "host") || Object.prototype.hasOwnProperty.call(record, "Host")) {
        return boolOf(record, "host", "Host");
    }
    return boolOf(record, "mine", "Mine");
}

function libraryError(err: unknown): string {
    const raw = (err instanceof Error ? err.message : typeof err === "string" ? err : "").trim();
    return raw || "操作失败";
}

function readItem(raw: unknown): CompanionLibraryItem | null {
    if (!raw || typeof raw !== "object") return null;
    const record = raw as Record<string, unknown>;
    const name = textOf(record, "name", "Name");
    const id = textOf(record, "id", "ID") || name;
    if (!id && !name) return null;
    return {
        id,
        name: name || "未命名文档",
        path: textOf(record, "path", "Path"),
        source: textOf(record, "source", "Source") || "local",
        openedAt: textOf(record, "openedAt", "OpenedAt"),
        owner: textOf(record, "owner", "Owner"),
        shareUrl: textOf(record, "shareUrl", "ShareURL"),
        expiresAt: textOf(record, "expiresAt", "ExpiresAt"),
        visibility: textOf(record, "visibility", "Visibility"),
        size: numberOf(record, "size", "Size"),
        permanent: boolOf(record, "permanent", "Permanent"),
        mine: boolOf(record, "mine", "Mine"),
        admin: boolOf(record, "admin", "Admin") || boolOf(record, "mine", "Mine"),
        host: companionHostFlag(record),
        hostLabel: textOf(record, "hostLabel", "HostLabel") || textOf(record, "owner", "Owner"),
        pendingHost: boolOf(record, "pendingHost", "PendingHost"),
        pendingHostLabel: textOf(record, "pendingHostLabel", "PendingHostLabel"),
        revision: numberOf(record, "revision", "Revision"),
        target: textOf(record, "target", "Target"),
        groupId: textOf(record, "groupId", "GroupID"),
        expired: boolOf(record, "expired", "Expired"),
        highlights: textOf(record, "highlights", "Highlights"),
        chatMarks: textOf(record, "chatMarks", "ChatMarks"),
        annotations: textOf(record, "annotations", "Annotations"),
        drawings: textOf(record, "drawings", "Drawings"),
        notes: companionNotesFrom(record.notes ?? record.Notes),
        token: textOf(record, "token", "Token") || (id.startsWith("cd_") ? id : ""),
    };
}

function readGroups(raw: unknown): CompanionGroup[] {
    const record = raw && typeof raw === "object" ? raw as Record<string, unknown> : {};
    const list = Array.isArray(raw) ? raw : Array.isArray(record.groups) ? record.groups : Array.isArray(record.Groups) ? record.Groups : [];
    return list.map((item) => {
        if (!item || typeof item !== "object") return null;
        const group = item as Record<string, unknown>;
        const id = textOf(group, "id", "ID");
        if (!id) return null;
        const membersRaw = Array.isArray(group.members) ? group.members : Array.isArray(group.Members) ? group.Members : [];
        const members = membersRaw.map((member) => {
            if (!member || typeof member !== "object") return null;
            const row = member as Record<string, unknown>;
            const contact = textOf(row, "contact", "Contact");
            if (!contact) return null;
            return { contact, status: textOf(row, "status", "Status") || "pending" };
        }).filter((member): member is CompanionGroupMember => member !== null);
        return {
            id,
            name: textOf(group, "name", "Name") || "未命名群组",
            owner: textOf(group, "owner", "Owner"),
            admin: boolOf(group, "admin", "Admin"),
            icon: companionGroupIcon(textOf(group, "icon", "Icon")),
            members,
        };
    }).filter((group): group is CompanionGroup => group !== null);
}

function GroupGlyph({ icon }: { icon: string }) {
    const stroke = {
        fill: "none",
        stroke: "currentColor",
        strokeWidth: 1.6,
        strokeLinecap: "round" as const,
        strokeLinejoin: "round" as const,
    };
    const kind = companionGroupIcon(icon);
    return (
        <svg className="file-companion-group-glyph" viewBox="0 0 24 24" aria-hidden="true">
            {kind === "people" ? (
                <>
                    <circle cx="9" cy="8" r="2.2" {...stroke} />
                    <circle cx="16" cy="9.2" r="1.8" {...stroke} />
                    <path d="M4.8 18.2c.5-2.5 2.3-3.8 4.2-3.8s3.7 1.3 4.2 3.8" {...stroke} />
                    <path d="M13.6 14.8c1.5-.2 3 .5 3.8 2.2" {...stroke} />
                </>
            ) : null}
            {kind === "star" ? <path d="M12 4.2l1.9 4.3 4.7.4-3.6 3.1 1.1 4.6L12 14.2 7.9 16.6l1.1-4.6L5.4 8.9l4.7-.4z" {...stroke} /> : null}
            {kind === "pen" ? <path d="M14.2 5.2l4.6 4.6L9.2 19.4 4.6 19.8 5 15.2z" {...stroke} /> : null}
            {kind === "leaf" ? (
                <>
                    <path d="M6 16.5C6 10 11 5.5 18.5 5.5 18.5 13 14 18 7.5 18" {...stroke} />
                    <path d="M8 15.5C10 12 13 9.5 17 7.5" {...stroke} />
                </>
            ) : null}
            {kind === "flask" ? (
                <>
                    <path d="M10 4.5h4M11 4.5v5.2L6.8 17.2A3 3 0 0 0 9.4 21h5.2a3 3 0 0 0 2.6-3.8L13 9.7V4.5" {...stroke} />
                    <path d="M8.2 15.5h7.6" {...stroke} />
                </>
            ) : null}
            {kind === "book" ? (
                <>
                    <path d="M5 6.5h6.5A2.5 2.5 0 0 1 14 9v9.5H7.5A2.5 2.5 0 0 1 5 16z" {...stroke} />
                    <path d="M14 9h2.5A2.5 2.5 0 0 1 19 11.5V18.5h-5" {...stroke} />
                    <path d="M8 6.5v12" {...stroke} />
                </>
            ) : null}
        </svg>
    );
}

type LibraryGlyphName = "share" | "globe" | "lock" | "person" | "people" | "copy" | "x" | "back" | "import";

function LibraryGlyph({ name, size = 14 }: { name: LibraryGlyphName; size?: number }) {
    const stroke = {
        fill: "none",
        stroke: "currentColor",
        strokeWidth: 1.6,
        strokeLinecap: "round" as const,
        strokeLinejoin: "round" as const,
    };
    return (
        <svg className="file-companion-library-glyph" data-icon={name} width={size} height={size} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
            {name === "share" ? (
                <>
                    <path {...stroke} d="M6.5 12.2h11a3.5 3.5 0 0 0 .2-7 5.5 5.5 0 0 0-10.6 1.5A3.2 3.2 0 0 0 6.5 12.2Z" />
                    <path {...stroke} d="M12 20.5v-8.2" />
                    <path {...stroke} d="m9.3 14.8 2.7-2.7 2.7 2.7" />
                </>
            ) : null}
            {name === "globe" ? (
                <>
                    <circle {...stroke} cx="12" cy="12" r="8.2" />
                    <path {...stroke} d="M3.8 12h16.4" />
                    <path {...stroke} d="M12 3.8c3 3.4 3 12.9 0 16.4" />
                    <path {...stroke} d="M12 3.8c-3 3.4-3 12.9 0 16.4" />
                </>
            ) : null}
            {name === "lock" ? (
                <>
                    <rect {...stroke} x="6" y="10.5" width="12" height="9" rx="1.6" />
                    <path {...stroke} d="M8.8 10.5V8.2a3.2 3.2 0 0 1 6.4 0v2.3" />
                </>
            ) : null}
            {name === "person" ? (
                <>
                    <circle {...stroke} cx="12" cy="8.2" r="3" />
                    <path {...stroke} d="M5.5 19.5c.9-3.4 3.4-5.2 6.5-5.2s5.6 1.8 6.5 5.2" />
                </>
            ) : null}
            {name === "people" ? (
                <>
                    <circle {...stroke} cx="9" cy="8" r="2.2" />
                    <circle {...stroke} cx="16" cy="9.2" r="1.8" />
                    <path {...stroke} d="M4.8 18.2c.5-2.5 2.3-3.8 4.2-3.8s3.7 1.3 4.2 3.8" />
                    <path {...stroke} d="M13.6 14.8c1.5-.2 3 .5 3.8 2.2" />
                </>
            ) : null}
            {name === "import" ? (
                <>
                    <path {...stroke} d="M4.5 13.5v5a1.8 1.8 0 0 0 1.8 1.8h11.4a1.8 1.8 0 0 0 1.8-1.8v-5" />
                    <path {...stroke} d="M12 4v10" />
                    <path {...stroke} d="m8.6 10.6 3.4 3.4 3.4-3.4" />
                </>
            ) : null}
            {name === "copy" ? (
                <>
                    <rect {...stroke} x="9" y="9" width="10.5" height="10.5" rx="1.6" />
                    <path {...stroke} d="M15 9V6.8A1.8 1.8 0 0 0 13.2 5H6.8A1.8 1.8 0 0 0 5 6.8v6.4A1.8 1.8 0 0 0 6.8 15H9" />
                </>
            ) : null}
            {name === "x" ? <path {...stroke} d="m6.5 6.5 11 11M17.5 6.5l-11 11" /> : null}
            {name === "back" ? <path {...stroke} d="M14.5 5 8 12l6.5 7" /> : null}
        </svg>
    );
}

function readUpdates(raw: unknown): CompanionShareUpdate[] {
    if (!Array.isArray(raw)) return [];
    return raw.map((item) => {
        if (!item || typeof item !== "object") return null;
        const record = item as Record<string, unknown>;
        const path = textOf(record, "path", "Path");
        if (!path) return null;
        const hasMessages = Object.prototype.hasOwnProperty.call(record, "messages") || Object.prototype.hasOwnProperty.call(record, "Messages");
        const messages = record.messages ?? record.Messages;
        const hasChatMarks = Object.prototype.hasOwnProperty.call(record, "chatMarks") || Object.prototype.hasOwnProperty.call(record, "ChatMarks");
        const chatMarks = record.chatMarks ?? record.ChatMarks;
        const hasAnnotations = Object.prototype.hasOwnProperty.call(record, "annotations") || Object.prototype.hasOwnProperty.call(record, "Annotations");
        const annotations = record.annotations ?? record.Annotations;
        const hasDrawings = Object.prototype.hasOwnProperty.call(record, "drawings") || Object.prototype.hasOwnProperty.call(record, "Drawings");
        const drawings = record.drawings ?? record.Drawings;
        return {
            path,
            highlights: textOf(record, "highlights", "Highlights"),
            chatMarks: typeof chatMarks === "string" ? chatMarks : "",
            chatMarksKnown: hasChatMarks,
            annotations: typeof annotations === "string" ? annotations : "",
            annotationsKnown: hasAnnotations,
            drawings: typeof drawings === "string" ? drawings : "",
            drawingsKnown: hasDrawings,
            messages: Array.isArray(messages) ? messages : [],
            project: boolOf(record, "project", "Project"),
            replaceMessages: hasMessages,
        };
    }).filter((item): item is CompanionShareUpdate => item !== null);
}

export function companionLibrarySync(raw: unknown): { updates: CompanionShareUpdate[]; removed: string[] } {
    if (!raw || typeof raw !== "object") return { updates: [], removed: [] };
    const record = raw as Record<string, unknown>;
    return {
        updates: readUpdates(record.updates ?? record.Updates),
        removed: readRemoved(record.removed ?? record.Removed),
    };
}

function readNote(raw: unknown): CompanionShareNote | null {
    if (!raw || typeof raw !== "object") return null;
    const record = raw as Record<string, unknown>;
    const id = textOf(record, "id", "ID");
    const text = textOf(record, "text", "Text");
    if (!id || !text) return null;
    return {
        id,
        author: textOf(record, "author", "Author"),
        text,
        createdAt: textOf(record, "createdAt", "CreatedAt") || textOf(record, "created_at", "created_at"),
        host: boolOf(record, "host", "Host"),
        replyTo: textOf(record, "replyTo", "ReplyTo") || textOf(record, "reply_to", "reply_to"),
    };
}

export function companionNotesFrom(raw: unknown): CompanionShareNote[] {
    if (!Array.isArray(raw)) return [];
    return raw.map(readNote).filter((note): note is CompanionShareNote => note !== null);
}

// companionSharePathKey is the companion tab identity for a library path.
// FileCompanionOpen stores the Windows-lowercased absolute path. A library
// row can still carry the on-disk spelling, or the other slash direction.
export function companionSharePathKey(path: string): string {
    const text = path.trim().replace(/\\/g, "/");
    if (/^[a-zA-Z]:\//.test(text) || text.startsWith("//")) return text.toLowerCase();
    return text;
}

// companionNotesRevisionWins reports that incoming should replace the notes
// already shown. Revision 0 is an old hub, or a response that did not carry
// one, and it must not cover a list that already has a revision.
export function companionNotesRevisionWins(current: number, incoming: number): boolean {
    if (incoming > current) return true;
    return incoming === 0 && current === 0;
}

// companionNotePost reads the note POST result. An older binding returned
// the notes array alone, which has no revision.
export function companionNotePost(raw: unknown): { notes: CompanionShareNote[]; revision: number } {
    if (Array.isArray(raw)) {
        return { notes: companionNotesFrom(raw), revision: 0 };
    }
    if (!raw || typeof raw !== "object") {
        return { notes: [], revision: 0 };
    }
    const record = raw as Record<string, unknown>;
    return {
        notes: companionNotesFrom(record.notes ?? record.Notes),
        revision: numberOf(record, "notesRevision", "NotesRevision") || numberOf(record, "notes_revision", "notes_revision"),
    };
}

// companionShareNotes reads the notes carried on each cloud row. A row
// without a local path is not open, so it is left out.
export function companionShareNotes(raw: unknown): CompanionShareBinding[] {
    if (!raw || typeof raw !== "object") return [];
    const record = raw as Record<string, unknown>;
    const items = Array.isArray(record.items) ? record.items : Array.isArray(record.Items) ? record.Items : [];
    const out: CompanionShareBinding[] = [];
    for (const item of items) {
        if (!item || typeof item !== "object") continue;
        const row = item as Record<string, unknown>;
        // A lapsed link is no longer a discussion. Its cached notes stay in
        // the library row, but the open tab must stop treating it as a share.
        if (boolOf(row, "expired", "Expired")) continue;
        const path = textOf(row, "path", "Path");
        const token = textOf(row, "token", "Token");
        if (!path || !token) continue;
        // `mine` here means "this machine holds the host seat": a transferred
        // host answers discussion like the owner, even though the owner keeps
        // admin rights on the share.
        const host = companionHostFlag(row);
        out.push({
            path,
            token,
            mine: host,
            notes: companionNotesFrom(row.notes ?? row.Notes),
            revision: numberOf(row, "notesRevision", "NotesRevision"),
        });
    }
    return out;
}

function readRemoved(raw: unknown): string[] {
    if (!Array.isArray(raw)) return [];
    return raw.filter((item): item is string => typeof item === "string" && item !== "");
}

// companionMineShares reads the cloud rows this person created. A document
// that already has one of these must not be shared a second time from the
// title bar; the button edits the share it already has. An expired link is
// not a live share: the paper may be published again.
export function companionMineShares(raw: unknown): CompanionShareRow[] {
    if (!raw || typeof raw !== "object") return [];
    const record = raw as Record<string, unknown>;
    const items = Array.isArray(record.items) ? record.items : Array.isArray(record.Items) ? record.Items : [];
    const out: CompanionShareRow[] = [];
    for (const item of items) {
        if (!item || typeof item !== "object") continue;
        const row = item as Record<string, unknown>;
        if (!boolOf(row, "mine", "Mine") || boolOf(row, "expired", "Expired")) continue;
        const token = textOf(row, "token", "Token") || textOf(row, "id", "ID");
        if (!token.startsWith("cd_")) continue;
        out.push({
            path: textOf(row, "path", "Path"),
            token,
            mine: true,
            admin: true,
            host: companionHostFlag(row),
            hostLabel: textOf(row, "hostLabel", "HostLabel") || textOf(row, "owner", "Owner"),
            pendingHost: boolOf(row, "pendingHost", "PendingHost"),
            pendingHostLabel: textOf(row, "pendingHostLabel", "PendingHostLabel"),
            owner: textOf(row, "owner", "Owner"),
            name: textOf(row, "name", "Name"),
            visibility: textOf(row, "visibility", "Visibility") || "public",
            expiresAt: textOf(row, "expiresAt", "ExpiresAt"),
            shareUrl: textOf(row, "shareUrl", "ShareURL"),
            target: textOf(row, "target", "Target"),
            groupId: textOf(row, "groupId", "GroupID"),
        });
    }
    return out;
}

// sameCompanionMineShare is the title-bar row. The token staying the same is
// not enough: the host seat and the pending nomination move without a new link.
export function sameCompanionMineShare(left: CompanionShareRow | undefined, right: CompanionShareRow): boolean {
    if (!left) return false;
    return left.token === right.token
        && left.host === right.host
        && left.hostLabel === right.hostLabel
        && left.pendingHost === right.pendingHost
        && left.pendingHostLabel === right.pendingHostLabel
        && left.visibility === right.visibility
        && left.expiresAt === right.expiresAt
        && left.target === right.target
        && left.groupId === right.groupId
        && left.owner === right.owner
        && left.name === right.name
        && left.shareUrl === right.shareUrl;
}

// companionHostShares is the auto-push seat: the current host on this machine.
// The admin keeps the management row (companionMineShares) but stops pushing
// automatically after transferring the seat.
export function companionHostShares(raw: unknown): CompanionShareRow[] {
    if (!raw || typeof raw !== "object") return [];
    const record = raw as Record<string, unknown>;
    const items = Array.isArray(record.items) ? record.items : Array.isArray(record.Items) ? record.Items : [];
    const out: CompanionShareRow[] = [];
    for (const item of items) {
        if (!item || typeof item !== "object") continue;
        const row = item as Record<string, unknown>;
        if (boolOf(row, "expired", "Expired")) continue;
        const host = companionHostFlag(row);
        if (!host) continue;
        const token = textOf(row, "token", "Token") || textOf(row, "id", "ID");
        if (!token.startsWith("cd_")) continue;
        out.push({
            path: textOf(row, "path", "Path"),
            token,
            mine: true,
            admin: boolOf(row, "admin", "Admin") || boolOf(row, "mine", "Mine"),
            host: true,
            hostLabel: textOf(row, "hostLabel", "HostLabel") || textOf(row, "owner", "Owner"),
            pendingHost: boolOf(row, "pendingHost", "PendingHost"),
            pendingHostLabel: textOf(row, "pendingHostLabel", "PendingHostLabel"),
            owner: textOf(row, "owner", "Owner"),
            name: textOf(row, "name", "Name"),
            visibility: textOf(row, "visibility", "Visibility") || "public",
            expiresAt: textOf(row, "expiresAt", "ExpiresAt"),
            shareUrl: textOf(row, "shareUrl", "ShareURL"),
            target: textOf(row, "target", "Target"),
            groupId: textOf(row, "groupId", "GroupID"),
        });
    }
    return out;
}

export type CompanionShareHost = { owner: string; mine: boolean };

// companionShareHosts reads who hosts each shared path. A share's creator is
// the host of that document, so the same badge can say "you" on the host's
// screen and name the person on every participant's screen.
export function companionShareHosts(raw: unknown): Map<string, CompanionShareHost> {
    const out = new Map<string, CompanionShareHost>();
    if (!raw || typeof raw !== "object") return out;
    const record = raw as Record<string, unknown>;
    const items = Array.isArray(record.items) ? record.items : Array.isArray(record.Items) ? record.Items : [];
    for (const item of items) {
        if (!item || typeof item !== "object") continue;
        const row = item as Record<string, unknown>;
        if (boolOf(row, "expired", "Expired")) continue;
        const path = textOf(row, "path", "Path");
        const token = textOf(row, "token", "Token");
        if (!path || !token) continue;
        const key = companionSharePathKey(path);
        const host = companionHostFlag(row);
        const label = textOf(row, "hostLabel", "HostLabel") || textOf(row, "owner", "Owner");
        const held = out.get(key);
        // One file can belong to several shares. A row where this person is
        // the current host wins; otherwise the first share answers.
        if (!held || (host && !held.mine)) out.set(key, { owner: label, mine: host });
    }
    return out;
}

function audienceLabel(visibility: string): string {
    if (visibility === "private") return "私密";
    if (visibility === "person") return "指定的人";
    if (visibility === "group") return "群组";
    return "公开";
}

function shareDays(span: ShareSpan, amount: string): number | null {
    if (span === "forever") return 0;
    const value = Number(amount);
    if (!Number.isInteger(value) || value < 1) return null;
    if (span === "months") {
        const days = value * 30;
        if (days > 3660) return null;
        return days;
    }
    if (value > 3660) return null;
    return value;
}

// shareSpanFromExpiry reads a share's remaining life back into the dialog's
// 永久/几天/几月 control, so 修改 opens on what the link already has. A whole
// number of months stays months; anything else counts in days. A link that
// expires within the current day still shows the one day it has left.
function shareSpanFromExpiry(expiresAt: string): { span: ShareSpan; amount: string } {
    if (!expiresAt) return { span: "forever", amount: "7" };
    const when = Date.parse(expiresAt);
    if (Number.isNaN(when)) return { span: "forever", amount: "7" };
    const days = Math.ceil((when - Date.now()) / 86_400_000);
    if (days <= 0) return { span: "days", amount: "1" };
    if (days % 30 === 0 && days <= 3660) return { span: "months", amount: String(days / 30) };
    return { span: "days", amount: String(Math.min(days, 3660)) };
}

function whenLabel(item: CompanionLibraryItem): string {
    if (item.expired) return item.expiresAt ? `已过期 · ${item.expiresAt.slice(0, 10)}` : "已过期";
    if (item.permanent || (item.source === "cloud" && !item.expiresAt)) return "永久";
    if (item.expiresAt) return `至 ${item.expiresAt.slice(0, 10)}`;
    if (item.openedAt) return item.openedAt.slice(0, 10);
    return "";
}

// sharedMarkTitle is the hover text on the 已分享 mark: the share's own range
// and expiry when this row carries them, and where 修改 lives otherwise.
function sharedMarkTitle(item: CompanionLibraryItem, row: CompanionLibraryItem | null): string {
    if (row) {
        const range = audienceLabel(row.visibility || item.visibility);
        return item.mine ? `已分享 — ${range}，${whenLabel(row)}` : `收到的分享 — ${range}，${whenLabel(row)}`;
    }
    return "已分享 — 到云端项目里可以修改分享范围和有效期";
}

function fmtSize(size: number): string {
    if (!size || size <= 0) return "";
    if (size < 1024) return `${size} B`;
    if (size < 1024 * 1024) return `${(size / 1024).toFixed(0)} KB`;
    return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}

// kindOf reads the document kind out of the file name. The badge shows the
// extension itself, so unknown kinds degrade to the honest raw letters.
function kindOf(name: string): string {
    const base = name.replace(/\\/g, "/").split("/").pop() ?? "";
    const dot = base.lastIndexOf(".");
    if (dot < 0) return "";
    return base.slice(dot + 1).toLowerCase();
}

function kindLabel(name: string): string {
    const kind = kindOf(name);
    return (kind || "文档").slice(0, 5).toUpperCase();
}

function dateOf(timestamp: string): string {
    return timestamp ? timestamp.slice(0, 10) : "";
}

// companionRemoveConfirm is what the row's delete button asks. A cloud row can
// be removed here without touching its link, and only an owned share has 修改
// to revoke it; say which case this is or a reader takes the wrong path and the
// duplicate rule still blocks the next share.
function companionRemoveConfirm(item: CompanionLibraryItem, name: string): { message: string; title: string } {
    if (item.source !== "cloud") {
        return {
            title: "删除伴读数据",
            message: `确认删除“${name}”？\n源文件会保留。本机的高亮、讨论和阅读记录会被清除。\n此操作不可撤销。`,
        };
    }
    if (item.mine && !item.expired) {
        // Removing an owner's live cloud row is the share control; it must
        // revoke the link so every participant drops the copy and closes the
        // tab. An expired link has no reader to notify and is removed locally.
        return {
            title: "取消分享",
            message: `确认取消分享“${name}”？\n所有参与者会从文档管理移除，并在伴读中关闭该文档。\n本机的文档、高亮、批注和手绘会保留。\n此操作不可撤销。`,
        };
    }
    if (item.expired) {
        return {
            title: "移除文档",
            message: `确认从文档管理移除“${name}”？\n源文件会保留；这份分享已过期。\n本机的高亮、讨论和阅读记录会被清除。\n此操作不可撤销。`,
        };
    }
    return {
        title: "移除文档",
        message: `确认从文档管理移除“${name}”？\n源文件会保留；这份分享仍属于分享者。\n本机的高亮、讨论和阅读记录会被清除。\n此操作不可撤销。`,
    };
}

async function copyText(text: string, showToast: LibraryToast): Promise<void> {
    try {
        await navigator.clipboard.writeText(text);
        showToast("已复制分享链接", "success", 6000);
    } catch {
        showToast("复制失败，请手动选择链接", "error", 6000);
    }
}

// useDialogFocus moves focus into a dialog when it opens and gives it back to
// whatever was focused before when it closes, so a keyboard user is never
// dropped at the top of the document. When ready is false the dialog is still
// loading its content: the focus waits, because the first row it wants does
// not exist yet and the fallback would take the focus instead.
function useDialogFocus<T extends HTMLElement>(selector: string, ready = true) {
    const ref = useRef<T>(null);
    const previousRef = useRef<HTMLElement | null>(null);
    const focusedRef = useRef(false);
    useEffect(() => {
        previousRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        return () => {
            previousRef.current?.focus?.();
            previousRef.current = null;
            focusedRef.current = false;
        };
    }, []);
    useEffect(() => {
        if (!ready || focusedRef.current) return;
        const node = ref.current?.querySelector<T>(selector);
        if (!node) return;
        node.focus();
        focusedRef.current = true;
    }, [selector, ready]);
    return ref;
}

// CompanionLibraryView is the 文档管理 screen: every local document and every
// accepted share is one 项目 card — the document, its annotations, and its
// discussion travel together. Actions open it for reading, share it, sync the
// newest marks, or pack it as a companion project zip.
export function CompanionLibraryView({
    activePath,
    showToast,
    onOpen,
    onExit,
    onShareUpdated,
    onListed,
    onShareProject,
    onEditShare,
    onImportLink,
    onProjectDeleted,
    libraryDeleteGen,
    listedSnapshot,
    dialogOpen,
}: {
    activePath: string;
    showToast: LibraryToast;
    onOpen: (path: string) => void;
    onExit: () => void;
    onShareUpdated?: (update: CompanionShareUpdate) => void;
    onListed?: (listed: unknown) => void;
    onShareProject: (path: string) => void;
    // A cloud row this person created opens 修改 instead of 分享: the link
    // already exists, so the dialog edits it rather than making another.
    onEditShare: (row: CompanionShareRow) => void;
    onImportLink: (link: string) => void;
    onProjectDeleted?: (path: string) => void;
    // Bumped when a delete finishes. A list that started earlier must not paint
    // that project, its marks, or its transcript back onto the screen.
    libraryDeleteGen?: { current: number };
    // A list fetched by the share/import dialog, pushed down so this view
    // paints it without a second request.
    listedSnapshot?: unknown;
    dialogOpen?: boolean;
}) {
    const [items, setItems] = useState<CompanionLibraryItem[]>([]);
    const [notice, setNotice] = useState("");
    const [loading, setLoading] = useState(true);
    const [rowBusy, setRowBusy] = useState<Record<string, string>>({});
    const { showConfirm } = useDialog();
    const updatedRef = useRef(onShareUpdated);
    const listedRef = useRef(onListed);
    updatedRef.current = onShareUpdated;
    listedRef.current = onListed;
    // The share and import dialogs live beside this view. While one is up it
    // owns Escape, so the view must not exit on the same keypress.
    const dialogOpenRef = useRef(false);
    dialogOpenRef.current = dialogOpen === true;
    // The delete confirm is the shared custom dialog. While it is open, Escape
    // belongs to that dialog and must not also leave 文档管理.
    const confirmOpenRef = useRef(false);
    // showConfirm replaces a dialog that is already open and resolves the first
    // one as cancel. A second click before React disables the button would
    // close the real confirm and clear confirmOpenRef while it is still showing.
    const deleteGate = useRef(false);
    // The last list object this view painted. The window echoes it back as
    // listedSnapshot; comparing identity keeps that echo from painting twice.
    const appliedListRef = useRef<unknown>(null);

    const applyList = useCallback((listed: unknown) => {
        const record = listed && typeof listed === "object" ? listed as Record<string, unknown> : {};
        const next = Array.isArray(record.items) ? record.items : Array.isArray(record.Items) ? record.Items : [];
        setItems(next.map(readItem).filter((item): item is CompanionLibraryItem => item !== null));
        setNotice(textOf(record, "notice", "Notice"));
    }, []);

    const reload = useCallback(async () => {
        const started = libraryDeleteGen?.current ?? 0;
        setLoading(true);
        try {
            const listed = await ListFileCompanionLibrary() as { items?: unknown[]; Items?: unknown[]; updates?: unknown; Updates?: unknown; removed?: unknown; Removed?: unknown };
            if (libraryDeleteGen && libraryDeleteGen.current !== started) return;
            appliedListRef.current = listed;
            applyList(listed);
            // The open document reads this same list. Notes, a newer transcript,
            // and a removed copy have to land while the view is the one refreshing.
            listedRef.current?.(listed);
        } catch (err) {
            setNotice(libraryError(err));
        } finally {
            setLoading(false);
        }
    }, [libraryDeleteGen, applyList]);

    useEffect(() => {
        void reload();
    }, [reload]);

    // A dialog or the window's poll already fetched the list and pushed it
    // here. The view repaints without asking the hub again; a list this view
    // fetched itself is already on screen and is skipped by identity.
    useEffect(() => {
        if (listedSnapshot === undefined || listedSnapshot === null) return;
        if (appliedListRef.current === listedSnapshot) return;
        appliedListRef.current = listedSnapshot;
        applyList(listedSnapshot);
    }, [listedSnapshot, applyList]);

    useEffect(() => {
        const onKey = (event: KeyboardEvent) => {
            if (event.key !== "Escape" || dialogOpenRef.current || confirmOpenRef.current) return;
            event.preventDefault();
            onExit();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [onExit]);

    const beginRow = (id: string, kind: string) => {
        setRowBusy((current) => (current[id] ? current : { ...current, [id]: kind }));
    };

    const endRow = (id: string) => {
        setRowBusy((current) => {
            if (!(id in current)) return current;
            const next = { ...current };
            delete next[id];
            return next;
        });
    };

    // Every reader mark travels together: PDF highlights and freehand marks on
    // the paper, answer highlights on the chat, and placed notes. One payload
    // keeps them on one revision.
    const marksJSONOf = (path: string): string => JSON.stringify({
        highlights: loadPdfMarks(path) || [],
        chatMarks: loadChatMarks(path),
        annotations: loadPdfAnnotations(path),
        drawings: loadPdfDrawings(path),
    });

    const syncRow = async (item: CompanionLibraryItem) => {
        if (!item.path || rowBusy[item.id]) return;
        beginRow(item.id, "sync");
        try {
            await SyncFileCompanionShare(item.path, marksJSONOf(item.path));
            showToast("已同步到云端", "success", 6000);
        } catch (err) {
            showToast(libraryError(err), "error", 6000);
        }
        // A failed sync can sit beside one that did update. Reload either way
        // so the list shows the revision that landed.
        try {
            await reload();
        } finally {
            endRow(item.id);
        }
    };

    const exportRow = async (item: CompanionLibraryItem) => {
        if (!item.path || rowBusy[item.id]) return;
        beginRow(item.id, "export");
        try {
            const dest = await FileCompanionExportProject(item.path, marksJSONOf(item.path));
            if (dest) {
                showToast("已导出项目", "success", 6000);
                void ShowItemInFolder(dest).catch(() => undefined);
            }
        } catch (err) {
            showToast(libraryError(err), "error", 6000);
        } finally {
            endRow(item.id);
        }
    };

    const deleteRow = async (item: CompanionLibraryItem) => {
        if (rowBusy[item.id] || deleteGate.current) return;
        deleteGate.current = true;
        try {
            beginRow(item.id, "confirm");
            const name = item.name.trim() || "这个项目";
            const confirm = companionRemoveConfirm(item, name);
            confirmOpenRef.current = true;
            let ok = false;
            try {
                ok = await showConfirm(
                    confirm.message,
                    confirm.title,
                    {
                        confirmText: item.source === "cloud" && item.mine && !item.expired ? "取消分享" : "删除",
                        cancelText: "取消",
                        confirmVariant: "danger",
                    },
                );
            } finally {
                confirmOpenRef.current = false;
            }
            if (!ok) {
                endRow(item.id);
                return;
            }
            setRowBusy((current) => ({ ...current, [item.id]: "delete" }));
            try {
                if (item.source === "cloud" && item.mine && item.token && !item.expired) {
                    // The owner's 移除 is the share control: revoke the link on
                    // the Hub. Participants' next refresh treats the missing
                    // token as deleted and withdraws the copy and open tab.
                    await DeleteFileCompanionShare(item.token);
                    if (libraryDeleteGen) libraryDeleteGen.current += 1;
                    showToast("已取消分享，参与者会移除并关闭该文档", "success", 6000);
                } else {
                    const raw = await DeleteFileCompanionProject(item.id) as Record<string, unknown>;
                    // Any list that started before this delete finished still has
                    // the project. Drop it. The reload below starts after the bump.
                    if (libraryDeleteGen) libraryDeleteGen.current += 1;
                    const path = textOf(raw, "path", "Path") || item.path;
                    const cleared = boolOf(raw, "cleared", "Cleared");
                    if (cleared && path) {
                        savePdfMarks(path, []);
                        savePdfTranslations(path, []);
                        savePdfDrawings(path, []);
                        saveChatMarks(path, []);
                        onProjectDeleted?.(path);
                    }
                    showToast(cleared ? "已删除伴读数据，源文件仍在" : "已从文档管理移除，源文件仍在", "success", 6000);
                }
            } catch (err) {
                showToast(libraryError(err), "error", 6000);
            }
            try {
                await reload();
            } finally {
                endRow(item.id);
            }
        } finally {
            deleteGate.current = false;
        }
    };

    const localItems = items.filter((item) => item.source !== "cloud");
    const cloudItems = items.filter((item) => item.source === "cloud");
    // A local document that was already published has a cloud row beside it.
    // The local card must offer 修改 on that row instead of 分享, or the same
    // paper ends up with two links. An expired link is not live, so it does
    // not hold that seat any more.
    const sharedByPath = useMemo(() => {
        const map = new Map<string, CompanionLibraryItem>();
        for (const item of cloudItems) {
            if (!item.mine || item.expired || !item.path) continue;
            const key = companionSharePathKey(item.path);
            if (key && !map.has(key)) map.set(key, item);
        }
        return map;
    }, [cloudItems]);
    // One parse for the whole list; each row then reads its count from the map.
    const markCounts = useMemo(
        () => loadPdfMarkCounts(items.map((item) => item.path).filter(Boolean)),
        [items],
    );
    // Focus the first project row once the list has arrived; until then the
    // only thing on screen is 导入分享.
    const cardRef = useDialogFocus<HTMLDivElement>(".file-companion-project-title", !loading);

    const renderRow = (item: CompanionLibraryItem) => {
        const busy = rowBusy[item.id];
        const marks = item.path ? (markCounts.get(pdfMarkPathKey(item.path)) ?? 0) : 0;
        const notes = item.source === "cloud" ? item.notes.length : 0;
        const meta = [
            item.source === "cloud"
                ? (item.mine ? "我的分享" : (item.owner || "别人的分享"))
                : "本机",
            fmtSize(item.size),
            // A cloud row's openedAt is when the share was created, so the
            // card says 分享于 instead of 打开于.
            dateOf(item.openedAt) ? (item.source === "cloud" ? `分享于 ${dateOf(item.openedAt)}` : `打开于 ${dateOf(item.openedAt)}`) : "",
        ].filter(Boolean).join(" · ");
        const isOpen = item.path === activePath;
        // A cloud row is itself a share. A local row is only shared when this
        // person already published the same path, and then 修改 edits that row.
        // An expired row no longer holds either seat: the paper can go out again.
        const shareRow = item.source === "cloud"
            ? (item.mine && !item.expired ? item : null)
            : (sharedByPath.get(companionSharePathKey(item.path)) ?? null);
        const alreadyShared = Boolean(shareRow) || (item.source === "cloud" && !item.expired);
        return (
            <div key={item.id} className="file-companion-project" data-testid={`file-companion-library-card-${item.id}`} data-open={isOpen ? "true" : undefined}>
                <span className="file-companion-project-kind" data-kind={kindOf(item.name)} aria-hidden="true">{kindLabel(item.name)}</span>
                <div className="file-companion-project-main">
                    <div className="file-companion-project-titleline">
                        <button
                            type="button"
                            className="file-companion-project-title"
                            data-testid={`file-companion-library-item-${item.id}`}
                            title={item.path ? `打开 ${item.name}` : `导入 ${item.name} 到本机`}
                            onClick={() => {
                                if (item.path) {
                                    onOpen(item.path);
                                    return;
                                }
                                onImportLink(item.shareUrl);
                            }}
                        >
                            {item.name}
                        </button>
                        {isOpen ? <span className="file-companion-badge" data-tone="active">打开中</span> : null}
                        {!item.path && item.source === "cloud" ? <span className="file-companion-badge" data-tone="pending">未导入</span> : null}
                    </div>
                    <p className="file-companion-project-meta">
                        <span className="file-companion-badge" data-tone="source">{item.source === "cloud" ? "云端" : "本机"}</span>
                        {item.source === "cloud" ? <span className="file-companion-badge">{audienceLabel(item.visibility)}</span> : null}
                        {item.source === "cloud" && item.host ? (
                            <span className="file-companion-badge" data-tone="active">主持人</span>
                        ) : null}
                        <span className="file-companion-badge">{whenLabel(item)}</span>
                        {marks > 0 ? <span className="file-companion-badge" data-tone="mark">标注 {marks}</span> : null}
                        {notes > 0 ? <span className="file-companion-badge" data-tone="note">讨论 {notes}</span> : null}
                        <span className="file-companion-project-facts">{meta}</span>
                    </p>
                    {item.shareUrl && !item.expired ? <small className="file-companion-project-url" data-testid={`file-companion-library-url-${item.id}`}>{item.shareUrl}</small> : null}
                </div>
                <div className="file-companion-project-actions">
                    {item.expired ? (
                        <span
                            className="file-companion-badge"
                            data-tone="mark"
                            data-testid={`file-companion-library-expired-mark-${item.id}`}
                            title="分享已过期，可以重新分享"
                        >
                            已过期
                        </span>
                    ) : alreadyShared ? (
                        <span
                            className="file-companion-project-shared"
                            data-testid={`file-companion-library-shared-mark-${item.id}`}
                            title={sharedMarkTitle(item, shareRow)}
                        >
                            <LibraryGlyph name="share" size={12} />
                            <span>{item.source === "cloud" && !item.mine ? "收到的" : "已分享"}</span>
                        </span>
                    ) : (
                        <button
                            type="button"
                            className="file-companion-project-action"
                            data-testid={`file-companion-library-share-action-${item.id}`}
                            disabled={!item.path || Boolean(busy)}
                            title="分享 — 上传文档、高亮和聊天，生成云端链接"
                            onClick={() => onShareProject(item.path)}
                        >
                            分享
                        </button>
                    )}
                    {item.source === "cloud" && shareRow ? (
                        <button
                            type="button"
                            className="file-companion-project-action"
                            data-testid={`file-companion-library-edit-share-${item.id}`}
                            disabled={Boolean(busy)}
                            title="修改分享 — 调整分享范围、有效期，链接不变"
                            onClick={() => onEditShare({
                                path: shareRow.path,
                                token: shareRow.token,
                                mine: true,
                                admin: true,
                                host: shareRow.host !== false,
                                hostLabel: shareRow.hostLabel || "",
                                pendingHost: Boolean(shareRow.pendingHost),
                                pendingHostLabel: shareRow.pendingHostLabel || "",
                                owner: shareRow.owner || "",
                                name: shareRow.name,
                                visibility: shareRow.visibility,
                                expiresAt: shareRow.expiresAt,
                                shareUrl: shareRow.shareUrl,
                                target: shareRow.target,
                                groupId: shareRow.groupId,
                            })}
                        >
                            修改
                        </button>
                    ) : null}
                    {item.path && item.source === "cloud" && item.host ? (
                        <button
                            type="button"
                            className="file-companion-project-action"
                            data-testid={`file-companion-library-sync-${item.id}`}
                            disabled={Boolean(busy)}
                            title="同步 — 把这份项目的新标注和聊天推到云端"
                            onClick={() => { void syncRow(item); }}
                        >
                            {busy === "sync" ? "同步中…" : "同步"}
                        </button>
                    ) : null}
                    {item.path ? (
                        <button
                            type="button"
                            className="file-companion-project-action"
                            data-testid={`file-companion-library-export-${item.id}`}
                            disabled={Boolean(busy)}
                            title="导出项目 — 打包文档、高亮和聊天为 zip"
                            onClick={() => { void exportRow(item); }}
                        >
                            {busy === "export" ? "导出中…" : "导出"}
                        </button>
                    ) : null}
                    <button
                        type="button"
                        className="file-companion-project-action"
                        data-testid={`file-companion-library-delete-${item.id}`}
                        disabled={Boolean(busy)}
                        title={item.source === "cloud"
                            ? (item.mine && !item.expired
                                ? "取消分享 — 参与者会移除并关闭该文档"
                                : item.expired ? "移除已过期的分享" : "从文档管理移除；云端分享仍属于分享者")
                            : "删除伴读数据，不删除源文件"}
                        onClick={() => { void deleteRow(item); }}
                    >
                        {busy === "delete"
                            ? (item.source === "cloud" && item.mine ? "取消中…" : "删除中…")
                            : item.source === "cloud" && item.mine && !item.expired ? "取消分享" : item.source === "cloud" ? "移除" : "删除"}
                    </button>
                </div>
            </div>
        );
    };

    return (
        <div className="file-companion-dialog" data-testid="file-companion-library-panel" role="dialog" aria-modal="true" aria-labelledby="file-companion-library-title">
            <div className="file-companion-dialog-scrim" data-testid="file-companion-library-scrim" onMouseDown={() => { if (confirmOpenRef.current) return; onExit(); }} />
            <div className="file-companion-dialog-card file-companion-library-dialog" ref={cardRef}>
                <header className="file-companion-dialog-head">
                    <div className="file-companion-dialog-heading">
                        <h2 id="file-companion-library-title">文档管理</h2>
                        <p className="file-companion-dialog-sub">
                            每个项目包含文档、高亮和讨论，{items.length > 0 ? `共 ${items.length} 个` : "还没有项目"}
                        </p>
                    </div>
                    <div className="file-companion-dialog-head-actions">
                        <button
                            type="button"
                            className="file-companion-dialog-secondary"
                            data-testid="file-companion-library-import-open"
                            title="导入分享 — 粘贴链接存进本机"
                            onClick={() => onImportLink("")}
                        >
                            导入分享
                        </button>
                        <button
                            type="button"
                            className="file-companion-dialog-x"
                            data-testid="file-companion-library-back"
                            aria-label="关闭"
                            title="关闭"
                            onClick={onExit}
                        >
                            <LibraryGlyph name="x" size={13} />
                            <span className="file-companion-sr">关闭</span>
                        </button>
                    </div>
                </header>
                <div className="file-companion-dialog-body">
                    {notice ? <p className="file-companion-library-notice" data-testid="file-companion-library-notice" role="status">{notice}</p> : null}
                    {loading ? <p className="file-companion-library-note">正在读取…</p> : null}
                    <section className="file-companion-library-section" data-testid="file-companion-library-local" aria-label="本机项目">
                        <h3 className="file-companion-dialog-label">本机项目<span className="file-companion-dialog-optional">{localItems.length}</span></h3>
                        {localItems.length === 0
                            ? <p className="file-companion-library-empty">还没有本机项目 — 打开或导入一个文档开始</p>
                            : <div className="file-companion-project-list">{localItems.map(renderRow)}</div>}
                    </section>
                    <section className="file-companion-library-section" data-testid="file-companion-library-cloud" aria-label="云端项目">
                        <h3 className="file-companion-dialog-label">云端项目<span className="file-companion-dialog-optional">{cloudItems.length}</span></h3>
                        {cloudItems.length === 0
                            ? <p className="file-companion-library-empty">还没有云端项目 — 分享后会出现在这里</p>
                            : <div className="file-companion-project-list">{cloudItems.map(renderRow)}</div>}
                    </section>
                </div>
            </div>
        </div>
    );
}

// CompanionGroupsDialog is the 阅读群组 popup: a group list on the left and
// the selected group's settings on the right. Admins rename, re-icon, invite,
// remove members, or dissolve; a member can leave.
export function CompanionGroupsDialog({
    showToast,
    onClose,
}: {
    showToast: LibraryToast;
    onClose: () => void;
}) {
    const [groups, setGroups] = useState<CompanionGroup[]>([]);
    const [notice, setNotice] = useState("");
    const [loading, setLoading] = useState(true);
    const [groupName, setGroupName] = useState("");
    // The selected group is the one whose settings show on the right. It stays
    // on a group the list no longer has only until the next reload.
    const [selectedID, setSelectedID] = useState("");
    const [groupContact, setGroupContact] = useState<Record<string, string>>({});
    const [groupEdits, setGroupEdits] = useState<Record<string, GroupEdit>>({});
    const [groupConfirm, setGroupConfirm] = useState<Record<string, "dissolve" | "leave">>({});
    const [groupBusy, setGroupBusy] = useState(false);
    // groupBusy is still false until the next paint, so a second click in this
    // turn would start another request and then toast a failure for one that worked.
    const groupBusyRef = useRef(false);

    const reload = useCallback(async () => {
        setLoading(true);
        try {
            const listed = readGroups(await ListFileCompanionGroups());
            // A create or invite that already landed locally must survive a
            // refresh that raced ahead of it: the list is the server's view,
            // and an entry it has not written yet is still ours.
            setGroups((current) => {
                const seen = new Set(listed.map((group) => group.id));
                const kept = current.filter((group) => !seen.has(group.id));
                return kept.length === 0 ? listed : [...listed, ...kept];
            });
            setNotice("");
        } catch (err) {
            // A failed refresh is not an empty group list. Wiping the cards
            // makes a rename, dissolve, or leave that already succeeded look undone.
            setNotice(libraryError(err));
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        void reload();
    }, [reload]);

    useEffect(() => {
        const onKey = (event: KeyboardEvent) => {
            if (event.key !== "Escape") return;
            event.preventDefault();
            onClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [onClose]);

    const rememberGroup = (raw: unknown) => {
        const next = readGroups([raw])[0];
        if (!next) return;
        setGroups((current) => {
            const index = current.findIndex((item) => item.id === next.id);
            if (index < 0) return [...current, next];
            const copy = [...current];
            copy[index] = next;
            return copy;
        });
    };

    const forgetGroup = (id: string) => {
        setGroups((current) => current.filter((item) => item.id !== id));
    };

    const beginGroupAction = () => {
        if (groupBusyRef.current) return false;
        groupBusyRef.current = true;
        setGroupBusy(true);
        return true;
    };

    const endGroupAction = () => {
        groupBusyRef.current = false;
        setGroupBusy(false);
    };

    const createGroup = async () => {
        const name = groupName.trim();
        const nameError = companionGroupNameError(groupName);
        if (nameError) {
            showToast(nameError, "error", 6000);
            return;
        }
        if (!beginGroupAction()) return;
        try {
            const created = readGroups([await CreateFileCompanionGroup(name)])[0];
            rememberGroup(created);
            setGroupName("");
            // Select the new group so its settings are what the reader sees;
            // otherwise the pane keeps showing whatever was selected before.
            if (created) setSelectedID(created.id);
            showToast("已创建群组", "success", 6000);
            await reload();
        } catch (err) {
            showToast(libraryError(err), "error", 6000);
        } finally {
            endGroupAction();
        }
    };

    const addMember = async (id: string) => {
        const contact = (groupContact[id] || "").trim();
        if (!contact) {
            showToast("请填写要邀请的邮箱或手机号", "error", 6000);
            return;
        }
        if (!beginGroupAction()) return;
        try {
            rememberGroup(await AddFileCompanionGroupMember(id, contact));
            setGroupContact((current) => ({ ...current, [id]: "" }));
            showToast("已发送入群邀请", "success", 6000);
            await reload();
        } catch (err) {
            showToast(libraryError(err), "error", 6000);
        } finally {
            endGroupAction();
        }
    };

    const removeMember = async (id: string, contact: string) => {
        if (!beginGroupAction()) return;
        try {
            rememberGroup(await RemoveFileCompanionGroupMember(id, contact));
            showToast("已移出群组", "success", 6000);
            await reload();
        } catch (err) {
            showToast(libraryError(err), "error", 6000);
        } finally {
            endGroupAction();
        }
    };

    const draftOf = (group: CompanionGroup): GroupEdit => {
        const draft = groupEdits[group.id];
        if (draft) return { name: draft.name, icon: companionGroupIcon(draft.icon) };
        return { name: group.name, icon: companionGroupIcon(group.icon) };
    };

    const clearGroupDraft = (id: string) => {
        setGroupEdits((current) => {
            if (!(id in current)) return current;
            const next = { ...current };
            delete next[id];
            return next;
        });
        setGroupConfirm((current) => {
            if (!(id in current)) return current;
            const next = { ...current };
            delete next[id];
            return next;
        });
    };

    const saveGroup = async (id: string) => {
        const group = groups.find((item) => item.id === id);
        if (!group || !group.admin) return;
        const edit = draftOf(group);
        const nameError = companionGroupNameError(edit.name);
        if (nameError) {
            showToast(nameError, "error", 6000);
            return;
        }
        if (!beginGroupAction()) return;
        try {
            rememberGroup(await UpdateFileCompanionGroup(id, edit.name.trim(), edit.icon));
            showToast("已更新群组", "success", 6000);
            await reload();
            // Drop the draft only after the list arrives, so the field does not
            // fall back to the previous name while the reload is still in flight.
            clearGroupDraft(id);
        } catch (err) {
            showToast(libraryError(err), "error", 6000);
        } finally {
            endGroupAction();
        }
    };

    const dissolveGroup = async (id: string) => {
        if (!beginGroupAction()) return;
        try {
            await DeleteFileCompanionGroup(id);
            forgetGroup(id);
            // The removed group cannot stay selected: the rail would show no
            // highlighted row and the pane would silently fall back.
            if (selectedID === id) setSelectedID("");
            showToast("已解散群组", "success", 6000);
            await reload();
            clearGroupDraft(id);
        } catch (err) {
            showToast(libraryError(err), "error", 6000);
        } finally {
            endGroupAction();
        }
    };

    const leaveGroup = async (id: string) => {
        if (!beginGroupAction()) return;
        try {
            await LeaveFileCompanionGroup(id);
            forgetGroup(id);
            if (selectedID === id) setSelectedID("");
            showToast("已退出群组", "success", 6000);
            await reload();
            clearGroupDraft(id);
        } catch (err) {
            showToast(libraryError(err), "error", 6000);
        } finally {
            endGroupAction();
        }
    };

    const selected = groups.find((group) => group.id === selectedID) ?? groups[0] ?? null;
    // Focus the create field once the group list has arrived: with no group
    // yet that is the only action.
    const cardRef = useDialogFocus<HTMLDivElement>(".file-companion-groups-create input", !loading);
    const selectedEdit = selected ? draftOf(selected) : null;
    const selectedConfirm = selected ? groupConfirm[selected.id] : undefined;
    const cancelConfirm = () => {
        if (!selected) return;
        setGroupConfirm((current) => {
            if (!(selected.id in current)) return current;
            const next = { ...current };
            delete next[selected.id];
            return next;
        });
    };

    return (
        <div className="file-companion-dialog" data-testid="file-companion-groups-view" role="dialog" aria-modal="true" aria-labelledby="file-companion-groups-title">
            <div className="file-companion-dialog-scrim" data-testid="file-companion-groups-scrim" onMouseDown={onClose} />
            <div className="file-companion-dialog-card file-companion-groups-dialog" ref={cardRef}>
                <header className="file-companion-dialog-head">
                    <div className="file-companion-dialog-heading">
                        <h2 id="file-companion-groups-title">阅读群组</h2>
                        <p className="file-companion-dialog-sub">用邮箱或手机号邀请，对方在主界面通知里同意后加入。</p>
                    </div>
                    <button
                        type="button"
                        className="file-companion-dialog-x"
                        data-testid="file-companion-groups-back"
                        aria-label="关闭"
                        title="关闭"
                        onClick={onClose}
                    >
                        <LibraryGlyph name="x" size={13} />
                        <span className="file-companion-sr">关闭</span>
                    </button>
                </header>

                <div className="file-companion-groups-body">
                    <aside className="file-companion-groups-rail">
                        <form
                            className="file-companion-groups-create"
                            onSubmit={(event) => {
                                event.preventDefault();
                                void createGroup();
                            }}
                        >
                            <input
                                className="file-companion-dialog-input"
                                data-testid="file-companion-group-name"
                                aria-label="新群组名称"
                                placeholder="新群组名称"
                                value={groupName}
                                onChange={(event) => setGroupName(event.target.value)}
                            />
                            <button type="submit" className="file-companion-dialog-primary" data-testid="file-companion-group-create" disabled={groupBusy}>
                                创建
                            </button>
                        </form>
                        {notice ? <p className="file-companion-library-notice" data-testid="file-companion-groups-notice" role="status">{notice}</p> : null}
                        {loading ? <p className="file-companion-library-note">正在读取…</p> : null}
                        <div
                            className="file-companion-groups-list"
                            role="listbox"
                            aria-label="我的群组"
                            onKeyDown={(event) => {
                                // Roving tabindex: arrows move the selection, so the
                                // list is one tab stop instead of N.
                                const keys = ["ArrowDown", "ArrowUp", "Home", "End"];
                                if (!keys.includes(event.key) || groups.length === 0) return;
                                event.preventDefault();
                                const current = Math.max(0, groups.findIndex((group) => group.id === selected?.id));
                                let next = current;
                                if (event.key === "ArrowDown") next = Math.min(groups.length - 1, current + 1);
                                if (event.key === "ArrowUp") next = Math.max(0, current - 1);
                                if (event.key === "Home") next = 0;
                                if (event.key === "End") next = groups.length - 1;
                                const target = groups[next];
                                if (!target) return;
                                setSelectedID(target.id);
                                const node = event.currentTarget.querySelector<HTMLElement>(`[data-group-id="${target.id}"]`);
                                node?.focus();
                            }}
                        >
                            {groups.length === 0 && !loading ? (
                                <div className="file-companion-groups-empty">
                                    <p>还没有群组</p>
                                    <small>创建后可以邀请同伴一起读同一份文档。</small>
                                </div>
                            ) : groups.map((group) => {
                                const edit = draftOf(group);
                                const active = selected?.id === group.id;
                                return (
                                    <button
                                        key={group.id}
                                        type="button"
                                        role="option"
                                        aria-selected={active}
                                        tabIndex={active ? 0 : -1}
                                        data-group-id={group.id}
                                        className="file-companion-groups-item"
                                        data-testid={`file-companion-group-${group.id}`}
                                        onClick={() => setSelectedID(group.id)}
                                    >
                                        <span className="file-companion-group-mark" data-testid={`file-companion-group-mark-${group.id}`} data-icon={group.admin ? edit.icon : companionGroupIcon(group.icon)} aria-hidden="true">
                                            <GroupGlyph icon={group.admin ? edit.icon : group.icon} />
                                        </span>
                                        <span className="file-companion-groups-item-text">
                                            <strong>{group.name}</strong>
                                            <small>{group.admin ? "管理员" : (group.owner || "成员")}{group.members.length > 0 ? ` · ${group.members.length} 人` : ""}</small>
                                        </span>
                                    </button>
                                );
                            })}
                        </div>
                    </aside>

                    <section className="file-companion-groups-detail" data-testid="file-companion-groups">
                        {!selected ? (
                            <div className="file-companion-groups-placeholder">
                                <LibraryGlyph name="people" size={26} />
                                <p>选择左侧的群组查看设置</p>
                            </div>
                        ) : (
                            <>
                                <div className="file-companion-groups-detail-head">
                                    <span className="file-companion-group-mark file-companion-group-mark--lg" data-icon={selected.admin ? selectedEdit?.icon : companionGroupIcon(selected.icon)} aria-hidden="true">
                                        <GroupGlyph icon={selected.admin ? (selectedEdit?.icon ?? selected.icon) : selected.icon} />
                                    </span>
                                    <div className="file-companion-library-group-title">
                                        <strong>{selected.name}</strong>
                                        <small>{selected.admin ? "你是管理员" : `成员 · ${selected.owner || "群主"}`}</small>
                                    </div>
                                </div>

                                {selected.admin ? (
                                    <>
                                        <div className="file-companion-groups-field">
                                            <h3 className="file-companion-dialog-label">群组图标</h3>
                                            <div className="file-companion-groups-icons" role="group" aria-label="群组图标">
                                                {companionGroupIcons.map((icon) => (
                                                    <button
                                                        key={icon}
                                                        type="button"
                                                        className="file-companion-group-mark-button"
                                                        data-testid={`file-companion-group-icon-${selected.id}-${icon}`}
                                                        aria-label={companionGroupIconLabel[icon]}
                                                        title={companionGroupIconLabel[icon]}
                                                        aria-pressed={selectedEdit?.icon === icon}
                                                        disabled={groupBusy}
                                                        onClick={() => setGroupEdits((current) => ({
                                                            ...current,
                                                            [selected.id]: { name: current[selected.id]?.name ?? selected.name, icon },
                                                        }))}
                                                    >
                                                        <GroupGlyph icon={icon} />
                                                    </button>
                                                ))}
                                            </div>
                                        </div>

                                        <div className="file-companion-groups-field">
                                            <h3 className="file-companion-dialog-label">群组名称</h3>
                                            <div className="file-companion-library-inline">
                                                <input
                                                    className="file-companion-dialog-input"
                                                    data-testid={`file-companion-group-rename-${selected.id}`}
                                                    aria-label="修改群组名称"
                                                    value={selectedEdit?.name ?? selected.name}
                                                    disabled={groupBusy}
                                                    onChange={(event) => setGroupEdits((current) => ({
                                                        ...current,
                                                        [selected.id]: { name: event.target.value, icon: current[selected.id]?.icon ?? companionGroupIcon(selected.icon) },
                                                    }))}
                                                />
                                                <button type="button" className="file-companion-dialog-secondary" data-testid={`file-companion-group-save-${selected.id}`} disabled={groupBusy} onClick={() => { void saveGroup(selected.id); }}>保存</button>
                                            </div>
                                        </div>

                                        <div className="file-companion-groups-field">
                                            <h3 className="file-companion-dialog-label">
                                                成员<span className="file-companion-dialog-optional">{selected.members.length} 人</span>
                                            </h3>
                                            <form
                                                className="file-companion-library-inline"
                                                onSubmit={(event) => {
                                                    event.preventDefault();
                                                    void addMember(selected.id);
                                                }}
                                            >
                                                <input
                                                    className="file-companion-dialog-input"
                                                    data-testid={`file-companion-group-contact-${selected.id}`}
                                                    aria-label="邀请邮箱或手机号"
                                                    placeholder="邀请的邮箱或手机号"
                                                    value={groupContact[selected.id] || ""}
                                                    disabled={groupBusy}
                                                    onChange={(event) => setGroupContact((current) => ({ ...current, [selected.id]: event.target.value }))}
                                                />
                                                <button type="submit" className="file-companion-dialog-secondary" data-testid={`file-companion-group-add-${selected.id}`} disabled={groupBusy}>邀请</button>
                                            </form>
                                            {selected.members.length === 0 ? (
                                                <p className="file-companion-dialog-hint">还没有成员。邀请后对方会收到通知。</p>
                                            ) : (
                                                <div className="file-companion-library-members">
                                                    {selected.members.map((member) => (
                                                        <div key={member.contact} className="file-companion-library-member">
                                                            <span className="file-companion-library-member-contact">{member.contact}</span>
                                                            <small className="file-companion-library-member-status" data-ok={member.status === "accepted" ? "true" : undefined}>{member.status === "accepted" ? "已加入" : member.status === "ignored" ? "已忽略" : "待确认"}</small>
                                                            <button
                                                                type="button"
                                                                className="file-companion-library-x"
                                                                data-testid={`file-companion-group-remove-${selected.id}-${member.contact}`}
                                                                aria-label={`移除 ${member.contact}`}
                                                                title={`移除 ${member.contact}`}
                                                                disabled={groupBusy}
                                                                onClick={() => { void removeMember(selected.id, member.contact); }}
                                                            >
                                                                <LibraryGlyph name="x" size={12} />
                                                                <span className="file-companion-sr">移除</span>
                                                            </button>
                                                        </div>
                                                    ))}
                                                </div>
                                            )}
                                        </div>

                                        <div className="file-companion-groups-danger">
                                            <div>
                                                <strong>解散群组</strong>
                                                <small>成员会失去这份分享的访问权限，文档本身不受影响。</small>
                                            </div>
                                            {selectedConfirm === "dissolve" ? (
                                                <div className="file-companion-dialog-actions">
                                                    <button type="button" disabled={groupBusy} onClick={cancelConfirm}>取消</button>
                                                    <button type="button" className="file-companion-dialog-danger" data-testid={`file-companion-group-dissolve-confirm-${selected.id}`} disabled={groupBusy} onClick={() => { void dissolveGroup(selected.id); }}>确认解散</button>
                                                </div>
                                            ) : (
                                                <button type="button" className="file-companion-dialog-danger" data-testid={`file-companion-group-dissolve-${selected.id}`} disabled={groupBusy} onClick={() => setGroupConfirm((current) => ({ ...current, [selected.id]: "dissolve" }))}>解散群组</button>
                                            )}
                                        </div>
                                    </>
                                ) : (
                                    <div className="file-companion-groups-danger">
                                        <div>
                                            <strong>退出群组</strong>
                                            <small>退出后不再看到这个群组分享的文档。</small>
                                        </div>
                                        {selectedConfirm === "leave" ? (
                                            <div className="file-companion-dialog-actions">
                                                <button type="button" disabled={groupBusy} onClick={cancelConfirm}>取消</button>
                                                <button type="button" className="file-companion-dialog-danger" data-testid={`file-companion-group-leave-confirm-${selected.id}`} disabled={groupBusy} onClick={() => { void leaveGroup(selected.id); }}>确认退出</button>
                                            </div>
                                        ) : (
                                            <button type="button" className="file-companion-dialog-danger" data-testid={`file-companion-group-leave-${selected.id}`} disabled={groupBusy} onClick={() => setGroupConfirm((current) => ({ ...current, [selected.id]: "leave" }))}>退出群组</button>
                                        )}
                                    </div>
                                )}
                            </>
                        )}
                    </section>
                </div>
            </div>
        </div>
    );
}

// CompanionShareDialog is the sharing popup: audience, validity, password,
// person target, or a group pick. With no share it publishes one project and
// returns a link. With a share it edits that link — the same link keeps
// working, only who can open it and how long it lasts change.
export function CompanionShareDialog({
    path,
    name,
    share,
    showToast,
    onListed,
    libraryDeleteGen,
    onClose,
}: {
    path: string;
    name: string;
    // share is the existing cloud row this dialog edits. Absent means 分享.
    share?: CompanionShareRow | null;
    showToast: LibraryToast;
    onListed?: (listed: unknown) => void;
    libraryDeleteGen?: { current: number };
    onClose: () => void;
}) {
    const editing = Boolean(share?.token);
    const initialSpan = useMemo(() => shareSpanFromExpiry(share?.expiresAt ?? ""), [share?.expiresAt]);
    const [visibility, setVisibility] = useState<ShareAudience>((share?.visibility as ShareAudience) || "public");
    const [personTarget, setPersonTarget] = useState(share?.visibility === "person" ? share.target : "");
    const [groupID, setGroupID] = useState(share?.visibility === "group" ? share.groupId : "");
    const [groups, setGroups] = useState<CompanionGroup[]>([]);
    const [span, setSpan] = useState<ShareSpan>(initialSpan.span);
    const [amount, setAmount] = useState(initialSpan.amount);
    // 修改 shows the link's remaining life, but only sends a new expiry once
    // the reader touches 有效期. Sending the rounded-down remaining days on
    // every save would walk the expiry a day earlier each time.
    const [expiryTouched, setExpiryTouched] = useState(false);
    // 修改 starts with an empty password field: the hub keeps the one already
    // stored unless a new one is typed.
    const [password, setPassword] = useState("");
    const [shareBusy, setShareBusy] = useState(false);
    const [shareURL, setShareURL] = useState(share?.shareUrl ?? "");
    // The saved result is what the header echoes. The row the dialog was
    // opened with is a snapshot, so it still shows the old audience and
    // expiry after a save.
    const [savedEcho, setSavedEcho] = useState<{ visibility: string; expiresAt: string } | null>(null);
    const [hostContact, setHostContact] = useState("");
    const [hostLabel, setHostLabel] = useState(share?.hostLabel || share?.owner || "");
    const [pendingHost, setPendingHost] = useState(share?.pendingHostLabel || "");
    // hostIsMe follows the seat, not the label. A missing host label falls back
    // to the owner, which used to hide 收回 even after the seat had moved.
    const [hostIsMe, setHostIsMe] = useState(share?.host !== false);
    const [hostBusy, setHostBusy] = useState(false);
    const [hostError, setHostError] = useState("");
    const echoVisibility = savedEcho?.visibility ?? share?.visibility ?? "public";
    const echoExpiresAt = savedEcho ? savedEcho.expiresAt : (share?.expiresAt ?? "");
    const listedRef = useRef(onListed);
    listedRef.current = onListed;
    const { showConfirm } = useDialog();

    useEffect(() => {
        let dead = false;
        ListFileCompanionGroups().then((raw) => {
            if (!dead) setGroups(readGroups(raw));
        }).catch(() => undefined);
        return () => { dead = true; };
    }, []);

    useEffect(() => {
        setHostLabel(share?.hostLabel || share?.owner || "");
        setPendingHost(share?.pendingHostLabel || "");
        setHostIsMe(share?.host !== false);
        setHostError("");
    }, [share?.token, share?.host, share?.hostLabel, share?.owner, share?.pendingHostLabel]);

    useEffect(() => {
        const onKey = (event: KeyboardEvent) => {
            if (event.key !== "Escape") return;
            event.preventDefault();
            onClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [onClose]);

    const reload = useCallback(async () => {
        const started = libraryDeleteGen?.current ?? 0;
        const token = share?.token ?? "";
        try {
            const listed = await ListFileCompanionLibrary();
            if (libraryDeleteGen && libraryDeleteGen.current !== started) return;
            listedRef.current?.(listed);
            if (!token) return;
            const row = companionMineShares(listed).find((item) => item.token === token);
            if (!row) return;
            // The dialog's share prop is the row it opened with. A range edit
            // can cancel a nomination or return the seat, and this is the copy
            // the host section is showing.
            setHostLabel(row.hostLabel || row.owner || "");
            setPendingHost(row.pendingHostLabel || "");
            setHostIsMe(row.host);
        } catch {
            // The toast already carried the failure; the next refresh lands the row.
        }
    }, [libraryDeleteGen, share?.token]);

    // loadPdfMarks reads and parses local storage, so the dialog parses once
    // per render and both actions send the same string. Answer highlights
    // travel with the paper highlights so one revision carries both.
    const marks = loadPdfMarks(path);
    const marksJSON = JSON.stringify({ highlights: marks, chatMarks: loadChatMarks(path), annotations: loadPdfAnnotations(path), drawings: loadPdfDrawings(path) });
    const highlightsCount = marks.length;

    const shareCurrent = async () => {
        // 修改 works from the share alone: a row this account created on
        // another machine has no local copy here, and it is still editable.
        if ((!editing && !path) || shareBusy) return;
        // An edit that did not touch 有效期 sends -1, which the backend reads
        // as "keep the expiry already on the link".
        const days = editing && !expiryTouched ? -1 : shareDays(span, amount);
        if (days === null) {
            showToast("分享时间不正确", "error", 6000);
            return;
        }
        const secret = password.trim();
        // A blank password keeps the one already on the link, but only when
        // the link is private already. echoVisibility is the saved state, so
        // a second save after switching to 私密 does not ask for the password
        // again. A link that becomes private needs one now.
        const keepStoredPassword = editing && secret === "" && echoVisibility === "private";
        if (visibility === "private" && !keepStoredPassword) {
            const n = Array.from(secret).length;
            if (n === 0) {
                showToast("请设置分享密码", "error", 6000);
                return;
            }
            if (n < 4 || n > 64) {
                showToast("分享密码需为 4–64 个字符", "error", 6000);
                return;
            }
        }
        const target = visibility === "person" ? personTarget.trim() : visibility === "group" ? groupID : "";
        if (visibility === "person" && target === "") {
            showToast("请填写邮箱或手机号", "error", 6000);
            return;
        }
        if (visibility === "group" && target === "") {
            showToast("请选择群组", "error", 6000);
            return;
        }
        setShareBusy(true);
        try {
            const result = editing
                ? await UpdateFileCompanionShare(share!.token, visibility, visibility === "private" ? secret : "", target, days) as Record<string, unknown>
                : await ShareFileCompanionDocument(path, visibility, visibility === "private" ? secret : "", target, marksJSON, days) as Record<string, unknown>;
            setShareURL(textOf(result, "url", "URL"));
            setSavedEcho({
                visibility: textOf(result, "visibility", "Visibility") || visibility,
                expiresAt: textOf(result, "expiresAt", "ExpiresAt"),
            });
            // The password just sent is stored on the link now. Clearing the
            // field means the next save keeps it instead of hashing it again.
            setPassword("");
            setExpiryTouched(false);
            showToast(editing ? "已更新分享" : "分享成功，链接已生成", "success", 6000);
            await reload();
        } catch (err) {
            showToast(libraryError(err), "error", 6000);
        } finally {
            setShareBusy(false);
        }
    };

    const syncCurrent = async () => {
        if (!path || shareBusy) return;
        setShareBusy(true);
        try {
            await SyncFileCompanionShare(path, marksJSON);
            showToast("已同步到云端", "success", 6000);
        } catch (err) {
            showToast(libraryError(err), "error", 6000);
        }
        // A failed share can sit beside one that did update. Reload either way
        // so the list shows the revision that landed.
        try {
            await reload();
        } finally {
            setShareBusy(false);
        }
    };

    const deleteCurrent = async () => {
        const token = share?.token ?? "";
        if (!token || shareBusy) return;
        const docName = name || share?.name || "当前文档";
        const ok = await showConfirm(
            `确认删除“${docName}”的分享？\n链接会立即失效，其他人将无法再打开。\n源文件仍会保留。`,
            "删除分享",
            { confirmText: "删除", cancelText: "取消", confirmVariant: "danger" },
        );
        if (!ok) return;
        setShareBusy(true);
        try {
            await DeleteFileCompanionShare(token);
        } catch (err) {
            showToast(libraryError(err), "error", 6000);
            setShareBusy(false);
            return;
        }
        // The link is gone. Invalidate a list that started before the delete,
        // or its snapshot puts the row back in the title bar and 文档管理.
        if (libraryDeleteGen) libraryDeleteGen.current += 1;
        // Close before asking for the list: a slow hub must not keep a deleted
        // share's dialog on screen. The background refresh still pushes the
        // new list into 文档管理.
        showToast("已删除分享", "success", 6000);
        onClose();
        await reload();
    };

    // transferHost nominates a participant. The seat only moves after that
    // person accepts in their own notification; the admin keeps management.
    const transferHost = async () => {
        const token = share?.token ?? "";
        const contact = hostContact.trim();
        if (!token || hostBusy) return;
        if (!contact) {
            setHostError("请填写参与者邮箱或手机号");
            return;
        }
        setHostBusy(true);
        setHostError("");
        try {
            await TransferFileCompanionShareHost(token, contact);
            setPendingHost(contact);
            setHostContact("");
            showToast("已发送主持人转移邀请，等待对方接受", "success", 6000);
            await reload();
        } catch (err) {
            setHostError(libraryError(err));
        } finally {
            setHostBusy(false);
        }
    };

    // revokeHost cancels a pending nomination, or takes the seat back from the
    // current host. The admin never loses management rights.
    const revokeHost = async () => {
        const token = share?.token ?? "";
        if (!token || hostBusy) return;
        const ok = await showConfirm(
            pendingHost
                ? `确认取消提名 ${pendingHost} 接任主持人？`
                : "确认收回主持权，由你重新担任主持人？",
            pendingHost ? "取消转移" : "收回主持权",
            { confirmText: "确认", cancelText: "取消", confirmVariant: "danger" },
        );
        if (!ok) return;
        const cancelling = Boolean(pendingHost);
        setHostBusy(true);
        setHostError("");
        try {
            await RevokeFileCompanionShareHost(token, cancelling ? "cancel" : "revoke");
            if (!cancelling) setHostIsMe(true);
            setPendingHost("");
            showToast(cancelling ? "已取消主持人转移" : "已收回主持权", "success", 6000);
            await reload();
        } catch (err) {
            setHostError(libraryError(err));
            // The seat may already have moved (the nominee accepted). Show that
            // instead of leaving the cancel button on a finished nomination.
            await reload();
        } finally {
            setHostBusy(false);
        }
    };

    const cardRef = useDialogFocus<HTMLDivElement>(".file-companion-audience-option");

    return (
        <div className="file-companion-dialog" data-testid="file-companion-share-dialog" role="dialog" aria-modal="true" aria-labelledby="file-companion-share-dialog-title">
            <div className="file-companion-dialog-scrim" data-testid="file-companion-share-dialog-scrim" onMouseDown={onClose} />
            <div className="file-companion-dialog-card file-companion-share" ref={cardRef}>
                <header className="file-companion-dialog-head">
                    <div className="file-companion-dialog-heading">
                        <h2 id="file-companion-share-dialog-title">{editing ? "修改分享" : "分享到云端"}</h2>
                        <p className="file-companion-dialog-sub">
                            <span className="file-companion-dialog-doc">{name || share?.name || "当前文档"}</span>
                            {editing ? <span className="file-companion-dialog-dot">当前 {audienceLabel(echoVisibility)} · {echoExpiresAt ? `至 ${echoExpiresAt.slice(0, 10)}` : "永久"}</span> : null}
                            {!editing && highlightsCount > 0 ? <span className="file-companion-dialog-dot">标注 {highlightsCount}</span> : null}
                        </p>
                    </div>
                    <button
                        type="button"
                        className="file-companion-dialog-x"
                        data-testid="file-companion-share-dialog-close"
                        aria-label="关闭"
                        title="关闭"
                        onClick={onClose}
                    >
                        <LibraryGlyph name="x" size={13} />
                        <span className="file-companion-sr">关闭</span>
                    </button>
                </header>

                <div className="file-companion-dialog-body">
                    <section className="file-companion-dialog-section">
                        <h3 className="file-companion-dialog-label">可见范围</h3>
                        <div className="file-companion-audience" role="group" aria-label="可见范围">
                            <button type="button" className="file-companion-audience-option" data-testid="file-companion-share-public" aria-pressed={visibility === "public"} onClick={() => setVisibility("public")}>
                                <LibraryGlyph name="globe" size={15} />
                                <span>完全公开</span>
                            </button>
                            <button type="button" className="file-companion-audience-option" data-testid="file-companion-share-private" aria-pressed={visibility === "private"} onClick={() => setVisibility("private")}>
                                <LibraryGlyph name="lock" size={15} />
                                <span>私密分享</span>
                            </button>
                            <button type="button" className="file-companion-audience-option" data-testid="file-companion-share-person" aria-pressed={visibility === "person"} onClick={() => setVisibility("person")}>
                                <LibraryGlyph name="person" size={15} />
                                <span>指定的人</span>
                            </button>
                            <button type="button" className="file-companion-audience-option" data-testid="file-companion-share-group" aria-pressed={visibility === "group"} onClick={() => setVisibility("group")}>
                                <LibraryGlyph name="people" size={15} />
                                <span>群组</span>
                            </button>
                        </div>
                        {visibility === "private" ? (
                            <input
                                className="file-companion-dialog-input"
                                data-testid="file-companion-share-password"
                                type="password"
                                aria-label="分享密码"
                                placeholder={editing ? "留空则保持原密码" : "设置密码，4–64 个字符"}
                                value={password}
                                onChange={(event) => setPassword(event.target.value)}
                            />
                        ) : null}
                        {visibility === "person" ? (
                            <input
                                className="file-companion-dialog-input"
                                data-testid="file-companion-share-target"
                                aria-label="邮箱或手机号"
                                placeholder="对方的邮箱或手机号"
                                value={personTarget}
                                onChange={(event) => setPersonTarget(event.target.value)}
                            />
                        ) : null}
                        {visibility === "group" ? (
                            groups.some((group) => group.admin) ? (
                                <select
                                    className="file-companion-dialog-input"
                                    data-testid="file-companion-share-group-pick"
                                    aria-label="选择群组"
                                    value={groupID}
                                    onChange={(event) => setGroupID(event.target.value)}
                                >
                                    <option value="">选择群组</option>
                                    {groups.filter((group) => group.admin).map((group) => (
                                        <option key={group.id} value={group.id}>{group.name}</option>
                                    ))}
                                </select>
                            ) : <p className="file-companion-dialog-hint">还没有管理的群组，先在「阅读群组」里创建一个。</p>
                        ) : null}
                    </section>

                    <section className="file-companion-dialog-section">
                        <h3 className="file-companion-dialog-label">有效期</h3>
                        <div className="file-companion-dialog-row">
                            <div className="file-companion-segment" role="group" aria-label="有效期">
                                <button type="button" data-testid="file-companion-share-forever" aria-pressed={span === "forever"} onClick={() => { setSpan("forever"); setExpiryTouched(true); }}>永久</button>
                                <button type="button" data-testid="file-companion-share-days" aria-pressed={span === "days"} onClick={() => { setSpan("days"); setExpiryTouched(true); }}>几天</button>
                                <button type="button" data-testid="file-companion-share-months" aria-pressed={span === "months"} onClick={() => { setSpan("months"); setExpiryTouched(true); }}>几月</button>
                            </div>
                            {span !== "forever" ? (
                                <label className="file-companion-dialog-amount">
                                    <input
                                        className="file-companion-dialog-input"
                                        data-testid="file-companion-share-amount"
                                        type="number"
                                        min={1}
                                        max={span === "months" ? 122 : 3660}
                                        aria-label={span === "months" ? "分享月数" : "分享天数"}
                                        value={amount}
                                        onChange={(event) => { setAmount(event.target.value); setExpiryTouched(true); }}
                                    />
                                    <span>{span === "months" ? "个月" : "天"}</span>
                                </label>
                            ) : null}
                        </div>
                    </section>

                    {editing && share?.admin ? (
                        <section className="file-companion-dialog-section" data-testid="file-companion-share-host">
                            <h3 className="file-companion-dialog-label">主持人</h3>
                            <p className="file-companion-dialog-hint" data-testid="file-companion-share-host-current">
                                {hostLabel ? `当前主持人：${hostLabel}` : "当前主持人：你"}
                            </p>
                            {pendingHost ? (
                                <div className="file-companion-dialog-row">
                                    <p className="file-companion-dialog-hint" data-testid="file-companion-share-host-pending">
                                        已提名 {pendingHost}，等待对方在通知里接受。
                                    </p>
                                    <button type="button" className="file-companion-dialog-secondary" data-testid="file-companion-share-host-cancel" disabled={hostBusy} onClick={() => { void revokeHost(); }}>
                                        取消转移
                                    </button>
                                </div>
                            ) : (
                                <div className="file-companion-dialog-row">
                                    <input
                                        className="file-companion-dialog-input"
                                        data-testid="file-companion-share-host-target"
                                        aria-label="新主持人的邮箱或手机号"
                                        placeholder="参与者的邮箱或手机号"
                                        value={hostContact}
                                        onChange={(event) => setHostContact(event.target.value)}
                                    />
                                    <button type="button" className="file-companion-dialog-secondary" data-testid="file-companion-share-host-transfer" disabled={hostBusy} onClick={() => { void transferHost(); }}>
                                        转移主持权
                                    </button>
                                    {!hostIsMe ? (
                                        <button type="button" className="file-companion-dialog-danger" data-testid="file-companion-share-host-revoke" disabled={hostBusy} onClick={() => { void revokeHost(); }}>
                                            收回主持权
                                        </button>
                                    ) : null}
                                </div>
                            )}
                            <p className="file-companion-dialog-hint">对方接受后才转移；你保留管理权，可随时收回或另行指定。</p>
                            {hostError ? (
                                <p className="file-companion-dialog-error" data-testid="file-companion-share-host-error" role="alert">{hostError}</p>
                            ) : null}
                        </section>
                    ) : null}

                    {shareURL ? (
                        <section className="file-companion-dialog-section file-companion-dialog-result" data-testid="file-companion-share-result">
                            <h3 className="file-companion-dialog-label">{editing ? "分享链接" : "链接已生成"}</h3>
                            <div className="file-companion-dialog-linkrow">
                                <p className="file-companion-dialog-link" data-testid="file-companion-library-link">{shareURL}</p>
                                <button type="button" className="file-companion-dialog-copy" data-testid="file-companion-library-copy" aria-label="复制分享链接" title="复制分享链接" onClick={() => { void copyText(shareURL, showToast); }}>
                                    <LibraryGlyph name="copy" size={13} />
                                    <span className="file-companion-sr">复制</span>
                                </button>
                            </div>
                        </section>
                    ) : null}
                </div>

                <footer className="file-companion-dialog-foot">
                    <p className="file-companion-dialog-hint">
                        {editing ? "改范围或有效期后，链接不变，已导入的人继续可用。" : "分享包含文档、高亮和聊天，之后可同步新标注。"}
                    </p>
                    <div className="file-companion-dialog-actions">
                        {editing ? (
                            <button type="button" className="file-companion-dialog-danger" data-testid="file-companion-share-delete" disabled={shareBusy} onClick={() => { void deleteCurrent(); }}>
                                删除分享
                            </button>
                        ) : (
                            <button type="button" className="file-companion-dialog-secondary" data-testid="file-companion-library-sync" disabled={shareBusy} onClick={() => { void syncCurrent(); }}>
                                同步更新
                            </button>
                        )}
                        <button type="button" className="file-companion-dialog-primary" data-testid="file-companion-library-share" disabled={shareBusy} aria-busy={shareBusy} onClick={() => { void shareCurrent(); }}>
                            {shareBusy ? (editing ? "正在保存…" : "正在分享…") : (editing ? "保存修改" : "分享到云端")}
                        </button>
                    </div>
                </footer>
            </div>
        </div>
    );
}

// CompanionImportDialog pulls a share link into the library. A private share
// asks for its password; the older hubs answer with their own message.
export function CompanionImportDialog({
    initialLink,
    showToast,
    onOpen,
    onShareUpdated,
    onListed,
    libraryDeleteGen,
    onClose,
}: {
    initialLink: string;
    showToast: LibraryToast;
    onOpen: (path: string) => void;
    onShareUpdated?: (update: CompanionShareUpdate) => void;
    onListed?: (listed: unknown) => void;
    libraryDeleteGen?: { current: number };
    onClose: () => void;
}) {
    const [importLink, setImportLink] = useState(initialLink);
    const [importPassword, setImportPassword] = useState("");
    const [importBusy, setImportBusy] = useState(false);
    const updatedRef = useRef(onShareUpdated);
    const listedRef = useRef(onListed);
    updatedRef.current = onShareUpdated;
    listedRef.current = onListed;

    useEffect(() => {
        const onKey = (event: KeyboardEvent) => {
            if (event.key !== "Escape") return;
            event.preventDefault();
            onClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [onClose]);

    const runImport = async (link: string, secret: string) => {
        const trimmed = link.trim();
        if (!trimmed) {
            showToast("请粘贴分享链接", "error", 6000);
            return;
        }
        setImportBusy(true);
        try {
            const imported = readItem(await ImportFileCompanionShare(trimmed, secret.trim()));
            if (imported?.path && imported.highlights) {
                updatedRef.current?.({
                    path: imported.path,
                    highlights: imported.highlights,
                    chatMarks: imported.chatMarks ?? "",
                    chatMarksKnown: Boolean(imported.chatMarks),
                    annotations: imported.annotations ?? "",
                    annotationsKnown: Boolean(imported.annotations),
                    drawings: imported.drawings ?? "",
                    drawingsKnown: Boolean(imported.drawings),
                    messages: [],
                    project: true,
                    replaceMessages: false,
                });
            }
            showToast("已导入文档库", "success", 6000);
            try {
                const started = libraryDeleteGen?.current ?? 0;
                const listed = await ListFileCompanionLibrary();
                if (!libraryDeleteGen || libraryDeleteGen.current === started) listedRef.current?.(listed);
            } catch {
                // The import toast already reported success; the next refresh lands the row.
            }
            if (imported?.path) onOpen(imported.path);
            onClose();
        } catch (err) {
            showToast(libraryError(err), "error", 6000);
        } finally {
            setImportBusy(false);
        }
    };

    const cardRef = useDialogFocus<HTMLDivElement>("input");

    return (
        <div className="file-companion-dialog" data-testid="file-companion-import-dialog" role="dialog" aria-modal="true" aria-labelledby="file-companion-import-dialog-title">
            <div className="file-companion-dialog-scrim" data-testid="file-companion-import-dialog-scrim" onMouseDown={onClose} />
            <div className="file-companion-dialog-card file-companion-share" ref={cardRef}>
                <header className="file-companion-dialog-head">
                    <div className="file-companion-dialog-heading">
                        <h2 id="file-companion-import-dialog-title">导入分享</h2>
                        <p className="file-companion-dialog-sub">导入后文档、高亮和聊天一起存进本机，成为一个可继续阅读的项目。</p>
                    </div>
                    <button
                        type="button"
                        className="file-companion-dialog-x"
                        data-testid="file-companion-import-dialog-close"
                        aria-label="关闭"
                        title="关闭"
                        onClick={onClose}
                    >
                        <LibraryGlyph name="x" size={13} />
                        <span className="file-companion-sr">关闭</span>
                    </button>
                </header>
                <div className="file-companion-dialog-body">
                    <section className="file-companion-dialog-section">
                        <h3 className="file-companion-dialog-label">分享链接</h3>
                        <input
                            className="file-companion-dialog-input"
                            data-testid="file-companion-library-import-link"
                            aria-label="分享链接"
                            placeholder="粘贴分享链接"
                            value={importLink}
                            onChange={(event) => setImportLink(event.target.value)}
                        />
                    </section>
                    <section className="file-companion-dialog-section">
                        <h3 className="file-companion-dialog-label">密码<span className="file-companion-dialog-optional">可选</span></h3>
                        <input
                            className="file-companion-dialog-input"
                            data-testid="file-companion-library-import-password"
                            type="password"
                            aria-label="导入密码"
                            placeholder="私密分享才需要密码"
                            value={importPassword}
                            onChange={(event) => setImportPassword(event.target.value)}
                        />
                    </section>
                </div>
                <footer className="file-companion-dialog-foot">
                    <div className="file-companion-dialog-actions">
                        <button type="button" className="file-companion-dialog-secondary" data-testid="file-companion-import-dialog-cancel" onClick={onClose}>
                            取消
                        </button>
                        <button type="button" className="file-companion-dialog-primary" data-testid="file-companion-library-import" disabled={importBusy} aria-busy={importBusy} onClick={() => { void runImport(importLink, importPassword); }}>
                            {importBusy ? "正在导入…" : "导入到文档库"}
                        </button>
                    </div>
                </footer>
            </div>
        </div>
    );
}
