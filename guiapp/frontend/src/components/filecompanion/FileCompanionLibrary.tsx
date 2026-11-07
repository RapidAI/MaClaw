import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { companionFill, cs, getCompanionUiLang, setCompanionUiLang } from "../../i18n/mnCompanion";
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
    ListFileCompanionCatalog,
    CreateFileCompanionFolder,
    RenameFileCompanionFolder,
    DeleteFileCompanionFolder,
    MoveFileCompanionLibraryItem,
    SetFileCompanionDefaultFolder,
    FileCompanionFolderForPath,
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
import { companionProjectExportJSON, companionProjectMarksJSON, savePdfBookmarks } from "./pdfBookmarks";
import { loadPdfMarkCounts, loadPdfMarks, pdfMarkPathKey, savePdfMarks } from "./pdfMarks";
import { saveChatAnnotations } from "./chatAnnotations";
import { saveChatMarks } from "./chatMarks";
import { savePdfTranslations } from "./pdfTranslations";
import { savePdfDrawings } from "./pdfDrawings";

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
    chatAnnotations?: string;
    annotations?: string;
    // drawings is the same import-only value for freehand marks.
    drawings?: string;
    // bookmarks is the same import-only value for named places on the paper.
    bookmarks?: string;
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
    // replyTo names the reader note that answer belongs to. decision is the
    // host's review of a reader note: pending, approved, or ignored.
    host?: boolean;
    replyTo?: string;
    decision?: string;
};

export function companionNoteKey(value: string | undefined): string {
    return (value ?? "").trim();
}

export function companionNoteDecision(note: CompanionShareNote): string {
    return (note.decision ?? "").trim().toLowerCase();
}

// pending counts reader notes the open list still marks 待审核: an explicit
// pending decision, or none, with no host reply. explicitPending is the
// library badge: only an explicit pending decision, so a note that never
// entered review does not look like it is waiting.
// total is discussions, not stored notes. A host answer names the reader
// note it belongs to, so that question and its answer count once. A host
// line with no reply counts on its own. An answer whose question has already
// left the capped list still counts: it is the only record of that thread.
export function companionDiscussionCounts(notes: CompanionShareNote[] | undefined): { total: number; pending: number; explicitPending: number; answered: Set<string> } {
    const list = notes ?? [];
    const ids = new Set<string>();
    for (const note of list) {
        const id = companionNoteKey(note.id);
        if (id) ids.add(id);
    }
    const answered = new Set<string>();
    for (const note of list) {
        const replyTo = companionNoteKey(note.replyTo);
        if (note.host && replyTo) answered.add(replyTo);
    }
    let total = 0;
    let pending = 0;
    let explicitPending = 0;
    for (const note of list) {
        const replyTo = companionNoteKey(note.replyTo);
        if (!(note.host && replyTo && ids.has(replyTo))) total++;
        if (note.host) continue;
        const id = companionNoteKey(note.id);
        if (!id || answered.has(id)) continue;
        const decision = companionNoteDecision(note);
        if (decision === "pending") {
            pending++;
            explicitPending++;
            continue;
        }
        if (decision === "") pending++;
    }
    return { total, pending, explicitPending, answered };
}

export type CompanionShareBinding = {
    path: string;
    token: string;
    mine: boolean;
    notes: CompanionShareNote[];
    revision: number;
    // marksRevision is the host's reader-marks sequence. A reader whose copy
    // is behind fetches the small marks blob instead of the project zip.
    marksRevision: number;
    // documentRevision is the project zip sequence. Questions travel in that
    // zip, so a downloaded host copy must not upload until this revision is
    // on disk.
    documentRevision: number;
};

export type CompanionShareUpdate = {
    path: string;
    highlights: string;
    // chatMarks is present only when the host's project carries answer
    // highlights. A legacy project omits it and leaves local marks alone.
    chatMarks: string;
    chatMarksKnown: boolean;
    // chatAnnotations is the host's notes on the answers. A legacy project
    // omits the field and leaves this reader's own notes alone.
    chatAnnotations: string;
    chatAnnotationsKnown: boolean;
    // annotations is the host's placed notes, also optional for legacy
    // projects so local notes survive.
    annotations: string;
    annotationsKnown: boolean;
    // drawings is the host's freehand strokes, optional for the same reason.
    drawings: string;
    drawingsKnown: boolean;
    // bookmarks is the host's named places, optional so a legacy project
    // leaves this reader's bookmarks alone.
    bookmarks: string;
    bookmarksKnown: boolean;
    // translations are the document cards that are not placed notes. A share
    // update leaves them off so a reader's own translations stay.
    translations?: string;
    translationsKnown?: boolean;
    messages: unknown[];
    project: boolean;
    replaceMessages: boolean;
    // keepReaderMarks leaves highlights, notes, drawings, and bookmarks as
    // they are. The transcript in messages still applies.
    keepReaderMarks?: boolean;
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

// Chinese keys. cs() at module load would freeze zh-Hans, because activeLang
// is still the default until a screen calls setCompanionUiLang.
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
    if (!cleaned) return cs("请填写群组名称");
    if ([...cleaned].length > 40) return cs("群组名称过长");
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

function textOf(record: Record<string, unknown> | null | undefined, camel: string, pascal: string): string {
    if (!record) return "";
    const value = record[camel] ?? record[pascal];
    return typeof value === "string" ? value : "";
}

function companionLibraryPaint(listed: unknown): { items: CompanionLibraryItem[]; notice: string; syncing: boolean; libraryRev: number } {
    const record = listed && typeof listed === "object" ? listed as Record<string, unknown> : {};
    const raw = Array.isArray(record.items) ? record.items : Array.isArray(record.Items) ? record.Items : [];
    return {
        items: raw.map(readItem).filter((item): item is CompanionLibraryItem => item !== null),
        notice: textOf(record, "notice", "Notice"),
        syncing: record.syncing === true || record.Syncing === true,
        libraryRev: numberOf(record, "libraryRev", "LibraryRev"),
    };
}

export function companionLibrarySnapshotKey(listed: unknown): string {
    return JSON.stringify(companionLibraryPaint(listed));
}

function boolOf(record: Record<string, unknown> | null | undefined, camel: string, pascal: string): boolean {
    if (!record) return false;
    const value = record[camel] ?? record[pascal];
    return value === true;
}

function numberOf(record: Record<string, unknown> | null | undefined, camel: string, pascal: string): number {
    if (!record) return 0;
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
    return (raw && cs(raw)) || cs("操作失败");
}

function readItem(raw: unknown): CompanionLibraryItem | null {
    if (!raw || typeof raw !== "object") return null;
    const record = raw as Record<string, unknown>;
    const name = textOf(record, "name", "Name");
    const id = textOf(record, "id", "ID") || name;
    if (!id && !name) return null;
    return {
        id,
        name: name || cs("未命名文档"),
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
        chatAnnotations: textOf(record, "chatAnnotations", "ChatAnnotations"),
        annotations: textOf(record, "annotations", "Annotations"),
        drawings: textOf(record, "drawings", "Drawings"),
        bookmarks: textOf(record, "bookmarks", "Bookmarks"),
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
            name: textOf(group, "name", "Name") || cs("未命名群组"),
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

type LibraryGlyphName = "share" | "globe" | "lock" | "person" | "people" | "copy" | "x" | "back" | "import" | "folder";

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
            {name === "folder" ? (
                <>
                    <path {...stroke} d="M4.5 8.2V17a1.8 1.8 0 0 0 1.8 1.8h11.4A1.8 1.8 0 0 0 19.5 17V9.6a1.6 1.6 0 0 0-1.6-1.6h-6.2L9.8 6.2H6.1A1.6 1.6 0 0 0 4.5 7.8v.4z" />
                </>
            ) : null}
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
        const hasChatAnnotations = Object.prototype.hasOwnProperty.call(record, "chatAnnotations") || Object.prototype.hasOwnProperty.call(record, "ChatAnnotations");
        const chatAnnotations = record.chatAnnotations ?? record.ChatAnnotations;
        const hasAnnotations = Object.prototype.hasOwnProperty.call(record, "annotations") || Object.prototype.hasOwnProperty.call(record, "Annotations");
        const annotations = record.annotations ?? record.Annotations;
        const hasDrawings = Object.prototype.hasOwnProperty.call(record, "drawings") || Object.prototype.hasOwnProperty.call(record, "Drawings");
        const drawings = record.drawings ?? record.Drawings;
        const hasBookmarks = Object.prototype.hasOwnProperty.call(record, "bookmarks") || Object.prototype.hasOwnProperty.call(record, "Bookmarks");
        const bookmarks = record.bookmarks ?? record.Bookmarks;
        return {
            path,
            highlights: textOf(record, "highlights", "Highlights"),
            chatMarks: typeof chatMarks === "string" ? chatMarks : "",
            chatMarksKnown: hasChatMarks,
            chatAnnotations: typeof chatAnnotations === "string" ? chatAnnotations : "",
            chatAnnotationsKnown: hasChatAnnotations,
            annotations: typeof annotations === "string" ? annotations : "",
            annotationsKnown: hasAnnotations,
            drawings: typeof drawings === "string" ? drawings : "",
            drawingsKnown: hasDrawings,
            bookmarks: typeof bookmarks === "string" ? bookmarks : "",
            bookmarksKnown: hasBookmarks,
            messages: Array.isArray(messages) ? messages : [],
            project: boolOf(record, "project", "Project"),
            replaceMessages: hasMessages,
        };
    }).filter((item): item is CompanionShareUpdate => item !== null);
}

// A cloud refresh can finish while an older list call is still returning the
// disk it read at the start. The older answer must not cover the refresh.
// Disk answers taken during one refresh share the generation from its start,
// and the first of them wins, so a slower read cannot cover a newer one. The
// hub index is a newer generation. The next refresh reuses the finished
// list's generation for its own disk answer, so a file opened after that list
// still appears; a late copy of the finished list must not cover it.
// libraryRev is the exception: a delete saves the library while that refresh
// is still copying, and the disk answer keeps the old generation on purpose.
// The newer revision has to paint, or the card the reader just removed stays
// up until the download finishes — and forever, when the refresh settles
// without another event.
let companionLibrarySeenGen = -1;
let companionLibrarySeenSyncing = false;
let companionLibrarySeenRev = 0;

export function takeCompanionLibraryList(listed: unknown): boolean {
    const record = listed && typeof listed === "object" ? listed as Record<string, unknown> : null;
    if (!record) return false;
    const genRaw = record.syncGen ?? record.SyncGen;
    const gen = typeof genRaw === "number" ? genRaw : 0;
    const syncing = record.syncing === true || record.Syncing === true;
    const rev = numberOf(record, "libraryRev", "LibraryRev");
    if (companionLibrarySeenRev > 0 && rev < companionLibrarySeenRev) return false;
    if (gen < companionLibrarySeenGen && !(rev > companionLibrarySeenRev)) return false;
    // The snapshot already on screen is this refresh. Keep it, unless the
    // library itself was saved again (a delete) after that snapshot was taken.
    if (gen === companionLibrarySeenGen && companionLibrarySeenSyncing && !(rev > companionLibrarySeenRev)) return false;
    if (gen > companionLibrarySeenGen) companionLibrarySeenGen = gen;
    companionLibrarySeenSyncing = syncing;
    if (rev > companionLibrarySeenRev) companionLibrarySeenRev = rev;
    return true;
}

// Rows the reader just removed. A refresh snapshot that was already in flight
// still names them and carries an older libraryRev; it must not put the card
// back. A later save (opening the file, importing the link) has a newer
// revision and the card returns.
const companionLibraryRemovedRows = new Map<string, number>();

export function companionLibraryRowKey(item: { id: string; source: string; token?: string }): string {
    if (item.source === "cloud" && item.token) return `cloud:${item.token}`;
    return `id:${item.id}`;
}

export function companionLibraryHideRow(item: { id: string; source: string; token?: string }, libraryRev: number, appliedRev: number) {
    const at = libraryRev > appliedRev ? libraryRev : appliedRev + 1;
    companionLibraryRemovedRows.set(companionLibraryRowKey(item), at);
    // The delete response is the newest library. A catalog already built keeps
    // the old revision and a newer sync generation; accepting it paints the
    // card back into the window before this view can filter it.
    if (libraryRev > companionLibrarySeenRev) companionLibrarySeenRev = libraryRev;
    notifyCompanionLibraryRows();
}

// A 取消分享 puts the owner's file back in 本机项目. The list that carries that
// row can be dropped while a refresh is still copying, so the card is shown
// from the delete itself until a list at this revision names it.
const companionLibraryRestoredRows = new Map<string, { item: CompanionLibraryItem; at: number }>();
let companionLibraryRowEpochValue = 0;
const companionLibraryRowListeners = new Set<() => void>();

function notifyCompanionLibraryRows() {
    companionLibraryRowEpochValue += 1;
    companionLibraryRowListeners.forEach((listener) => listener());
}

export function subscribeCompanionLibraryRows(listener: () => void): () => void {
    companionLibraryRowListeners.add(listener);
    return () => { companionLibraryRowListeners.delete(listener); };
}

export function companionLibraryRowEpoch(): number {
    return companionLibraryRowEpochValue;
}

export function companionLibraryShowRestored(item: CompanionLibraryItem, libraryRev: number, appliedRev: number) {
    const at = libraryRev > appliedRev ? libraryRev : appliedRev + 1;
    companionLibraryRestoredRows.set(item.id, { item, at });
    notifyCompanionLibraryRows();
}

export function companionLibraryApplyLocalEdits<T extends { id: string; source: string; token?: string }>(items: T[], libraryRev: number): T[] {
    let changed = false;
    const kept: T[] = [];
    for (const item of items) {
        const deletedAt = companionLibraryRemovedRows.get(companionLibraryRowKey(item));
        if (deletedAt != null && libraryRev < deletedAt) {
            changed = true;
            continue;
        }
        kept.push(item);
    }
    const extras: T[] = [];
    for (const pending of companionLibraryRestoredRows.values()) {
        if (libraryRev >= pending.at) continue;
        if (kept.some((row) => row.id === pending.item.id)) continue;
        extras.push(pending.item as T);
        changed = true;
    }
    if (!changed) return items;
    return extras.length > 0 ? [...extras, ...kept] : kept;
}

export function companionLibraryTakeRestored<T extends { id: string }>(items: T[], libraryRev: number): T[] {
    if (companionLibraryRestoredRows.size === 0) return items;
    const present = new Set(items.map((item) => item.id));
    const extras: T[] = [];
    for (const [id, pending] of [...companionLibraryRestoredRows]) {
        if (present.has(id) || libraryRev >= pending.at) {
            companionLibraryRestoredRows.delete(id);
            continue;
        }
        extras.push(pending.item as T);
    }
    return extras.length > 0 ? [...extras, ...items] : items;
}

function companionRestoredLocalItem(raw: Record<string, unknown> | null | undefined, fallbackName: string): CompanionLibraryItem | null {
    if (!boolOf(raw, "restored", "Restored")) return null;
    const path = textOf(raw, "path", "Path");
    const id = textOf(raw, "localId", "LocalID");
    if (!path || !id) return null;
    const name = textOf(raw, "name", "Name") || fallbackName;
    return {
        id,
        name: name || cs("未命名文档"),
        path,
        source: "local",
        openedAt: "",
        owner: "",
        shareUrl: "",
        expiresAt: "",
        visibility: "",
        size: numberOf(raw, "size", "Size"),
        permanent: false,
        mine: false,
        revision: 0,
        target: "",
        groupId: "",
        expired: false,
        highlights: "",
        notes: [],
        token: "",
    };
}

export function companionLibraryWithoutRemoved<T extends { id: string; source: string; token?: string }>(items: T[], libraryRev: number): T[] {
    if (companionLibraryRemovedRows.size === 0) return items;
    const present = new Set<string>();
    const keep: T[] = [];
    for (const item of items) {
        const key = companionLibraryRowKey(item);
        present.add(key);
        const deletedAt = companionLibraryRemovedRows.get(key);
        if (deletedAt != null && libraryRev < deletedAt) continue;
        if (deletedAt != null) companionLibraryRemovedRows.delete(key);
        keep.push(item);
    }
    for (const [key, deletedAt] of [...companionLibraryRemovedRows]) {
        if (libraryRev >= deletedAt && !present.has(key)) companionLibraryRemovedRows.delete(key);
    }
    return keep;
}

export function resetCompanionLibraryList(): void {
    companionLibrarySeenGen = -1;
    companionLibrarySeenSyncing = false;
    companionLibrarySeenRev = 0;
    companionLibraryRemovedRows.clear();
    companionLibraryRestoredRows.clear();
    companionLibraryRowEpochValue = 0;
    companionLibraryRowListeners.forEach((listener) => listener());
}

export function companionLibrarySync(raw: unknown): { updates: CompanionShareUpdate[]; removed: string[]; removedHosts: Record<string, string> } {
    if (!raw || typeof raw !== "object") return { updates: [], removed: [], removedHosts: {} };
    const record = raw as Record<string, unknown>;
    const removed = readRemoved(record.removed ?? record.Removed);
    return {
        updates: readUpdates(record.updates ?? record.Updates),
        removed,
        removedHosts: readRemovedHosts(record.removedHosts ?? record.RemovedHosts, removed),
    };
}

// companionWithdrawnNotice is what a participant sees before an opened article
// closes because the host cancelled the share. The host name is who to ask
// for another copy. With no name, the sentence still says to ask the host.
export function companionWithdrawnNotice(articleCount: number, hosts: string[]): string {
    const lang = getCompanionUiLang();
    const withdrawn = articleCount > 1 ? cs("这些文章已经取消分享") : cs("该文章已经取消分享");
    const names: string[] = [];
    for (const host of hosts) {
        const name = host.trim();
        if (name && !names.includes(name)) names.push(name);
    }
    const joiner = lang === "zh-Hans" || lang === "zh-Hant" ? "、" : ", ";
    const ask = names.length > 0 ? companionFill(lang, "请联系主持人 ${names}", { names: names.join(joiner) }) : cs("请联系分享的主持人");
    return companionFill(lang, "${withdrawn}，如果需要继续阅读，${ask}", { withdrawn, ask });
}

// readRemovedHosts names the host of each withdrawn path, so the window can
// tell a reader whom to ask when the tab closes. Only paths that actually
// closed keep a name; a stale or extra entry is ignored. The library path and
// the host map can disagree on slash direction or drive-letter case. Older
// backends send only the path list.
function readRemovedHosts(raw: unknown, removed: string[]): Record<string, string> {
    if (!raw || typeof raw !== "object" || removed.length === 0) return {};
    const byKey = new Map<string, string>();
    for (const path of removed) {
        const key = companionSharePathKey(path);
        if (key && !byKey.has(key)) byKey.set(key, path);
    }
    const out: Record<string, string> = {};
    for (const [path, label] of Object.entries(raw as Record<string, unknown>)) {
        const canonical = byKey.get(companionSharePathKey(path));
        if (!canonical || typeof label !== "string" || label.trim() === "") continue;
        if (!out[canonical]) out[canonical] = label.trim();
    }
    return out;
}

function readNote(raw: unknown): CompanionShareNote | null {
    if (!raw || typeof raw !== "object") return null;
    const record = raw as Record<string, unknown>;
    const id = textOf(record, "id", "ID").trim();
    const text = textOf(record, "text", "Text");
    if (!id || !text) return null;
    return {
        id,
        author: textOf(record, "author", "Author"),
        text,
        createdAt: textOf(record, "createdAt", "CreatedAt") || textOf(record, "created_at", "created_at"),
        host: boolOf(record, "host", "Host"),
        replyTo: (textOf(record, "replyTo", "ReplyTo") || textOf(record, "reply_to", "reply_to")).trim(),
        decision: textOf(record, "decision", "Decision").trim().toLowerCase(),
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

export type CompanionLibraryFolder = { id: string; name: string };

const companionFolderNameCap = 40;

const companionReservedFolderNames = ["全部", "未分类", "all", "uncategorized", "бүгд", "ангилаагүй"];

function companionReservedFolderName(name: string): boolean {
    const folded = name.toLocaleLowerCase();
    if (companionReservedFolderNames.some((reserved) => reserved.toLocaleLowerCase() === folded)) return true;
    // The rail already shows these two captions in the active language.
    return ["全部", "未分类"].some((key) => cs(key).toLocaleLowerCase() === folded);
}

export function companionFolderNameError(name: string, existing?: { id: string; name: string }[], ignoreID = ""): string {
    const cleaned = name.trim();
    if (!cleaned || /[\r\n\u0000]/.test(cleaned)) return cs("请填写文件夹名称");
    if ([...cleaned].length > companionFolderNameCap) return cs("文件夹名称过长");
    if (companionReservedFolderName(cleaned)) return cs("全部和未分类不能用作文件夹名称");
    const folded = cleaned.toLocaleLowerCase();
    if (existing?.some((folder) => folder.id !== ignoreID && folder.name.trim().toLocaleLowerCase() === folded)) {
        return cs("已有同名文件夹");
    }
    return "";
}

// companionLibraryNameMatch is the 按文件名过滤 test. An empty query keeps
// every row. Matching is a case-insensitive substring of the file name.
export function companionLibraryNameMatch(name: string, query: string): boolean {
    const needle = query.trim().toLocaleLowerCase();
    if (!needle) return true;
    return name.toLocaleLowerCase().includes(needle);
}

// companionFolderUncategorized is the catalog sentinel for a row moved to
// 未分类. Folder ids are fld_ plus hex, so this value cannot be a folder.
const companionFolderUncategorized = "none";

// companionLibraryDragType is the payload of a document card dragged onto a
// folder. text/plain carries the same id with a prefix so an OS file drop,
// which is also text, is not treated as a library row.
export const companionLibraryDragType = "application/x-maclaw-library-item";
const companionLibraryDragTextPrefix = "maclaw-library:";

function companionDragItemID(transfer: DataTransfer | null): string {
    if (!transfer) return "";
    const typed = transfer.getData(companionLibraryDragType);
    if (typed) return typed;
    const plain = transfer.getData("text/plain");
    return plain.startsWith(companionLibraryDragTextPrefix) ? plain.slice(companionLibraryDragTextPrefix.length) : "";
}

// A library drag is text/plain. A text field would otherwise insert that
// payload. Fields inside a folder row are left to the row: the row files
// the document, and the drop cancels the insert.
function companionDragTextField(target: EventTarget | null): HTMLInputElement | HTMLTextAreaElement | null {
    if (target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement) return target;
    return null;
}

// companionFolderPathCandidates lists the spellings a catalog path key may
// use. Go stores the Windows canonical path: backslashes and a lowercased
// drive. The library row can still carry the other slash direction.
function companionFolderPathCandidates(path: string): string[] {
    const trimmed = path.trim();
    if (!trimmed) return [];
    const forward = trimmed.replace(/\\/g, "/");
    const backward = forward.replace(/\//g, "\\");
    const out = [trimmed, forward, backward];
    const driveOrUnc = /^[a-zA-Z]:\//.test(forward) || forward.startsWith("//") || trimmed.startsWith("\\\\");
    if (driveOrUnc) {
        out.push(trimmed.toLowerCase(), forward.toLowerCase(), backward.toLowerCase());
    }
    return out;
}

// companionLibraryFolderID is the folder a row belongs to. The item key is
// the row id. "none" is an explicit 未分类 and does not fall through to the
// path key. The path key keeps the folder when that row is replaced by a
// share of the same file.
export function companionLibraryFolderID(item: { id: string; path: string }, placements: Record<string, string> | null | undefined): string {
    if (!placements) return "";
    const byItem = placements["item:" + item.id];
    if (byItem === companionFolderUncategorized) return "";
    if (byItem) return byItem;
    for (const key of companionFolderPathCandidates(item.path || "")) {
        const folderID = placements["path:" + key];
        if (!folderID || folderID === companionFolderUncategorized) continue;
        return folderID;
    }
    return "";
}

export function readCompanionLibraryCatalog(raw: unknown): { rev: number; folders: CompanionLibraryFolder[]; placements: Record<string, string>; defaultId: string } {
    const record = raw && typeof raw === "object" ? raw as Record<string, unknown> : {};
    const rev = numberOf(record, "rev", "Rev");
    const rawFolders = Array.isArray(record.folders) ? record.folders : Array.isArray(record.Folders) ? record.Folders : [];
    const folders: CompanionLibraryFolder[] = [];
    for (const item of rawFolders) {
        if (!item || typeof item !== "object") continue;
        const row = item as Record<string, unknown>;
        const id = textOf(row, "id", "ID");
        const name = textOf(row, "name", "Name");
        if (!id || !name) continue;
        folders.push({ id, name });
    }
    const rawPlaces = record.placements ?? record.Placements;
    const placements: Record<string, string> = {};
    if (rawPlaces && typeof rawPlaces === "object" && !Array.isArray(rawPlaces)) {
        for (const [key, value] of Object.entries(rawPlaces as Record<string, unknown>)) {
            if (typeof value === "string" && value) placements[key] = value;
        }
    }
    const defaultId = textOf(record, "defaultId", "DefaultID");
    return { rev, folders, placements, defaultId: folders.some((folder) => folder.id === defaultId) ? defaultId : "" };
}

// companionInstalledSharePath is a file this machine accepted into the share
// directory. The folder name is the share token, so a tab restored after the
// window restarts is still that discussion until the library list says the
// link is gone.
export function companionInstalledSharePath(path: string): boolean {
    const text = path.trim().replace(/\\/g, "/");
    return /(?:^|\/)file-companion\/library-files\/cd_[0-9a-f]{32}\//i.test(text);
}

// companionShouldPullShareMarks reports whether this machine should fetch the
// host's highlights, annotations, drawings, and bookmarks. A participant always
// does. The machine that authored the paper already has them. Another sign-in
// of that same host only has the downloaded copy, and that copy has to follow
// the marks the other participants already see.
export function companionShouldPullShareMarks(row: { mine: boolean; path: string; marksRevision: number }): boolean {
    if (!row.marksRevision) return false;
    if (!row.mine) return true;
    return companionInstalledSharePath(row.path);
}

// companionZipKeepsAppliedMarks reports that a project zip must not replace
// highlights already shown. The zip is the document revision, and a marks
// fetch can land first with a newer revision while that download is still
// running. A highlight this seat is about to upload wins for the same reason:
// the zip would replace it and the upload would then send the replacement.
// An older hub omits the marks revision, and then the zip is the only copy
// unless a local paint is in flight.
export function companionZipKeepsAppliedMarks(applied: number, listedRevision: number, painting = false): boolean {
    if (painting) return true;
    return listedRevision > 0 && applied > listedRevision;
}

// companionShareDocumentCatchUp moves the document revision this machine
// believes is on disk. The hub index advertises the revision still
// downloading and must not count as stored. A list call marked syncing is
// the file already here; a quiet refresh never publishes a finished list,
// so that row has to count or a question waits forever. A finished list
// settles only up to a revision the index did not leave ahead.
export function companionShareDocumentCatchUp(
    current: { listed: number; applied: number },
    doc: number,
    syncing: boolean,
    catalog: boolean,
): { listed: number; applied: number } {
    let listed = current.listed;
    let applied = current.applied;
    if (doc > listed) listed = doc;
    if (catalog) return { listed, applied };
    if (doc > applied && (!syncing || doc >= listed)) applied = doc;
    return { listed, applied };
}

// companionShareMarksBehind reports that a question on this downloaded host
// copy must wait. Uploading first would send the local project, which still
// lacks the highlights or the questions the other machine already published.
// The authoring paper is never behind; it is the copy being painted.
// project is omitted when only the marks sequence is known. An unknown
// project list waits. A listed document revision ahead of the copy on disk
// waits until a finished list says that zip is stored.
export function companionShareMarksBehind(
    path: string,
    marksListed: number,
    marksApplied: number,
    project?: { listed: number; applied: number; known: boolean },
): boolean {
    if (!companionInstalledSharePath(path)) return false;
    if (project) {
        if (!project.known) return true;
        if (project.listed > project.applied) return true;
    }
    if (!(marksListed > 0)) return false;
    return marksApplied < marksListed;
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
            marksRevision: numberOf(row, "marksRevision", "MarksRevision"),
            documentRevision: numberOf(row, "revision", "Revision"),
        });
    }
    return out;
}

// readRemoved lists the paths this refresh withdrew. A path appears once:
// the notice counts articles, and the tab close is keyed by path.
function readRemoved(raw: unknown): string[] {
    if (!Array.isArray(raw)) return [];
    const out: string[] = [];
    for (const item of raw) {
        if (typeof item !== "string" || item === "" || out.includes(item)) continue;
        out.push(item);
    }
    return out;
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
    if (visibility === "private") return cs("私密");
    if (visibility === "person") return cs("指定的人");
    if (visibility === "group") return cs("群组");
    return cs("公开");
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
    const lang = getCompanionUiLang();
    if (item.expired) return item.expiresAt ? companionFill(lang, "已过期 · ${date}", { date: item.expiresAt.slice(0, 10) }) : cs("已过期");
    if (item.permanent || (item.source === "cloud" && !item.expiresAt)) return cs("永久");
    if (item.expiresAt) return companionFill(lang, "至 ${date}", { date: item.expiresAt.slice(0, 10) });
    if (item.openedAt) return item.openedAt.slice(0, 10);
    return "";
}

// sharedMarkTitle is the hover text on the 已分享 mark: the share's own range
// and expiry when this row carries them, and where 修改 lives otherwise.
function sharedMarkTitle(item: CompanionLibraryItem, row: CompanionLibraryItem | null): string {
    if (row) {
        const range = audienceLabel(row.visibility || item.visibility);
        const when = whenLabel(row);
        return item.mine ? companionFill(getCompanionUiLang(), "已分享 — ${range}，${when}", { range, when }) : companionFill(getCompanionUiLang(), "收到的分享 — ${range}，${when}", { range, when });
    }
    return cs("已分享 — 到云端项目里可以修改分享范围和有效期");
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
    return (kind || cs("文档")).slice(0, 5).toUpperCase();
}

function dateOf(timestamp: string): string {
    return timestamp ? timestamp.slice(0, 10) : "";
}

// companionOwnPaper is a file this computer shared from its own disk.
// A copy downloaded for a share lives under library-files and is not one.
function companionOwnPaper(path: string): boolean {
    const norm = path.replace(/\\/g, "/").toLowerCase();
    return path.trim() !== "" && !norm.includes("/file-companion/library-files/");
}

// companionRemoveConfirm is what the row's delete button asks. A cloud row can
// be removed here without touching its link, and only an owned share has 修改
// to revoke it; say which case this is or a reader takes the wrong path and the
// duplicate rule still blocks the next share. Removing the owner's share puts
// the file back in 本机项目. Only the local delete clears reading data.
function companionRemoveConfirm(item: CompanionLibraryItem, name: string): { message: string; title: string } {
    if (item.source !== "cloud") {
        return {
            title: cs("删除伴读数据"),
            message: companionFill(getCompanionUiLang(), "确认删除“${name}”？\n源文件会保留。本机的高亮、讨论和阅读记录会被清除。\n此操作不可撤销。", { name }),
        };
    }
    if (item.mine && companionOwnPaper(item.path)) {
        if (!item.expired) {
            // Removing an owner's live cloud row revokes the link so every
            // participant drops the copy and closes the tab. The file itself
            // returns to 本机项目.
            return {
                title: cs("取消分享"),
                message: companionFill(getCompanionUiLang(), "确认取消分享“${name}”？\n所有参与者会从文档管理移除，并在伴读中关闭该文档。\n没有其他分享时，这份文档会回到本机项目，高亮和阅读记录会保留。\n在本机项目里删除才会清除这些数据。", { name }),
            };
        }
        return {
            title: cs("移除分享"),
            message: companionFill(getCompanionUiLang(), "确认移除“${name}”的分享？\n分享信息会去掉，这份文档会回到本机项目。\n高亮和阅读记录会保留。在本机项目里删除才会清除这些数据。", { name }),
        };
    }
    if (item.expired) {
        return {
            title: cs("移除文档"),
            message: companionFill(getCompanionUiLang(), "确认从文档管理移除“${name}”？\n源文件会保留；这份分享已过期。\n本机的高亮、讨论和阅读记录会被清除。\n此操作不可撤销。", { name }),
        };
    }
    return {
        title: cs("移除文档"),
        message: companionFill(getCompanionUiLang(), "确认从文档管理移除“${name}”？\n源文件会保留；这份分享仍属于分享者。\n本机的高亮、讨论和阅读记录会被清除。\n此操作不可撤销。", { name }),
    };
}

async function copyText(text: string, showToast: LibraryToast): Promise<void> {
    try {
        await navigator.clipboard.writeText(text);
        showToast(cs("已复制分享链接"), "success", 6000);
    } catch {
        showToast(cs("复制失败，请手动选择链接"), "error", 6000);
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
        const root = ref.current;
        if (!root) return;
        const active = document.activeElement;
        // The list can finish after the user has started typing in this dialog.
        // Leave that field focused.
        if (active instanceof HTMLElement && active !== root && root.contains(active)) {
            focusedRef.current = true;
            return;
        }
        const node = root.querySelector<T>(selector);
        if (!node) return;
        node.focus();
        focusedRef.current = true;
    }, [selector, ready]);
    return ref;
}

// FolderDeletePrompt asks what to do with the documents in a folder before
// that folder is removed. Merge moves them. Remove drops them from 文档管理
// and leaves the source files on disk.
function FolderDeletePrompt({
    folder,
    count,
    folders,
    busy,
    onCancel,
    onConfirm,
}: {
    folder: CompanionLibraryFolder;
    count: number;
    folders: CompanionLibraryFolder[];
    busy: boolean;
    onCancel: () => void;
    onConfirm: (mode: "merge" | "remove", targetID: string) => void;
}) {
    const others = folders.filter((item) => item.id !== folder.id);
    const [mode, setMode] = useState<"merge" | "remove">(others.length > 0 ? "merge" : "remove");
    const [targetID, setTargetID] = useState(others[0]?.id ?? "");
    const mergeBlocked = mode === "merge" && (others.length === 0 || !targetID);
    const cardRef = useRef<HTMLDivElement>(null);
    useEffect(() => {
        cardRef.current?.focus();
    }, []);
    useEffect(() => {
        const onKey = (event: KeyboardEvent) => {
            if (event.key !== "Escape" || busy) return;
            event.preventDefault();
            event.stopImmediatePropagation();
            onCancel();
        };
        window.addEventListener("keydown", onKey, true);
        return () => window.removeEventListener("keydown", onKey, true);
    }, [busy, onCancel]);
    return (
        <div
            className="file-companion-folder-prompt"
            role="presentation"
            onMouseDown={(event) => {
                if (event.target === event.currentTarget && !busy) onCancel();
            }}
        >
            <div
                className="file-companion-folder-prompt-card"
                role="dialog"
                aria-modal="true"
                aria-labelledby="file-companion-folder-delete-title"
                data-testid="file-companion-folder-delete"
                tabIndex={-1}
                ref={cardRef}
                onMouseDown={(event) => event.stopPropagation()}
            >
                <h3 id="file-companion-folder-delete-title">{cs("删除文件夹")}</h3>
                <p>
                    {companionFill(getCompanionUiLang(), "「${name}」里有 ${count} 个文档。可以合并到其他文件夹，或从文档管理删除这些文档。源文件会保留。", { name: folder.name, count })}
                </p>
                <label className="file-companion-folder-choice">
                    <input
                        type="radio"
                        name="file-companion-folder-delete-mode"
                        data-testid="file-companion-folder-delete-merge"
                        checked={mode === "merge"}
                        disabled={others.length === 0 || busy}
                        onChange={() => setMode("merge")}
                    />
                    <span>{cs("合并到其他文件夹")}</span>
                </label>
                <select
                    className="file-companion-dialog-input"
                    data-testid="file-companion-folder-delete-target"
                    aria-label={cs("合并到其他文件夹")}
                    value={targetID}
                    disabled={mode !== "merge" || others.length === 0 || busy}
                    onChange={(event) => setTargetID(event.target.value)}
                >
                    {others.length === 0 ? <option value="">{cs("没有其他文件夹可以合并")}</option> : null}
                    {others.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
                </select>
                <label className="file-companion-folder-choice">
                    <input
                        type="radio"
                        name="file-companion-folder-delete-mode"
                        data-testid="file-companion-folder-delete-remove"
                        checked={mode === "remove"}
                        disabled={busy}
                        onChange={() => setMode("remove")}
                    />
                    <span>{cs("删除这些文档")}</span>
                </label>
                {mode === "remove" ? <p className="file-companion-folder-prompt-note">{cs("本机文档会清除伴读数据。你的云端分享会取消，文档回到未分类。收到的分享会从列表移除。")}</p> : null}
                <div className="file-companion-folder-prompt-actions">
                    <button type="button" className="file-companion-dialog-secondary" data-testid="file-companion-folder-delete-cancel" disabled={busy} onClick={onCancel}>
                        {cs("取消")}
                    </button>
                    <button
                        type="button"
                        className={mode === "remove" ? "file-companion-dialog-primary file-companion-folder-danger" : "file-companion-dialog-primary"}
                        data-testid="file-companion-folder-delete-confirm"
                        disabled={busy || mergeBlocked}
                        onClick={() => onConfirm(mode, mode === "merge" ? targetID : "")}
                    >
                        {busy ? cs("删除中…") : mode === "merge" ? cs("合并并删除") : cs("删除文档并移除文件夹")}
                    </button>
                </div>
            </div>
        </div>
    );
}

// CompanionLibraryView is the 文档管理 screen: every local document and every
// accepted share is one 项目 card — the document, its annotations, and its
// discussion travel together. A single level of folders groups those cards.
// Drag a card by its type badge onto a folder, or onto 未分类, to categorize
// it. The title stays a click. The move menu does the same thing from the
// keyboard. A file-name field narrows the list.
// Actions open a document for reading, share it, sync the newest marks, or
// pack it as a companion project zip.
const companionCatalogChangedEvent = "maclaw:file-companion-catalog";

export function CompanionLibraryView({
    lang,
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
    lang?: string;
    activePath: string;
    showToast: LibraryToast;
    onOpen: (path: string) => void;
    onExit: () => void;
    onShareUpdated?: (update: CompanionShareUpdate) => void;
    onListed?: (listed: unknown) => boolean | void;
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
    if (lang) setCompanionUiLang(lang);
    const seeded = useMemo(() => companionLibraryPaint(listedSnapshot), [listedSnapshot]);
    const [items, setItems] = useState<CompanionLibraryItem[]>(seeded.items);
    const [notice, setNotice] = useState(seeded.notice);
    const [loading, setLoading] = useState(listedSnapshot == null);
    const [syncing, setSyncing] = useState(seeded.syncing);
    const [rowBusy, setRowBusy] = useState<Record<string, string>>({});
    const [folders, setFolders] = useState<CompanionLibraryFolder[]>([]);
    const [placements, setPlacements] = useState<Record<string, string>>({});
    const [defaultFolderID, setDefaultFolderID] = useState("");
    // "" is every document. "none" is 未分类. Any other value is a folder id.
    const [selectedFolder, setSelectedFolder] = useState("");
    const [nameQuery, setNameQuery] = useState("");
    // 所有 shows both rails. 本地 and 云端 keep one rail.
    const [sourceFilter, setSourceFilter] = useState<"all" | "local" | "cloud">("all");
    const [folderDraft, setFolderDraft] = useState("");
    const [editingID, setEditingID] = useState("");
    const [editingName, setEditingName] = useState("");
    const [pendingDelete, setPendingDelete] = useState<(CompanionLibraryFolder & { count: number }) | null>(null);
    const [folderBusy, setFolderBusy] = useState(false);
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
    // The folder-delete choice owns Escape the same way the confirm dialog does.
    const folderPromptRef = useRef(false);
    folderPromptRef.current = pendingDelete !== null;
    const folderBusyRef = useRef(false);
    // Set in dragstart before React paints, so the first dragover can accept
    // the drop. The folder is the class the card was in when the drag began.
    const dragItemIDRef = useRef("");
    const dragFolderRef = useRef("");
    const dragCardRef = useRef<HTMLElement | null>(null);
    const dropRowRef = useRef<HTMLElement | null>(null);
    // Same-direction rail nudges are throttled. A direction change is not.
    const dragScrollRef = useRef({ at: 0, dir: 0 });
    // The rail's border box does not move when its content scrolls, so one
    // measurement covers the gesture. "short" means the list already fits.
    const dragRailBoxRef = useRef<{ top: number; bottom: number } | "short" | null>(null);
    // The dialog that owns the in-progress drag. Text fields and folder
    // controls under it ignore the pointer until the drag ends.
    const dragRootRef = useRef<HTMLElement | null>(null);
    // Set while a rename submit is in flight. Blur fires before the click that
    // submits, and also when Enter leaves the field, so blur must not cancel
    // a rename that is already being saved.
    const renameHoldRef = useRef(false);
    const catalogRevRef = useRef(0);
    // showConfirm replaces a dialog that is already open and resolves the first
    // one as cancel. A second click before React disables the button would
    // close the real confirm and clear confirmOpenRef while it is still showing.
    const deleteGate = useRef(false);
    // The last list object this view painted. The window echoes it back as
    // listedSnapshot; comparing identity keeps that echo from painting twice.
    const appliedListRef = useRef<unknown>(listedSnapshot ?? null);
    const appliedSnapshotKeyRef = useRef(companionLibrarySnapshotKey(listedSnapshot));
    const paintedRef = useRef(listedSnapshot != null);
    // The library revision last painted. A delete stamps its own revision so a
    // refresh snapshot from before that save cannot bring the card back.
    const appliedLibraryRevRef = useRef(0);
    const libraryRowEpoch = useSyncExternalStore(subscribeCompanionLibraryRows, companionLibraryRowEpoch, companionLibraryRowEpoch);
    useEffect(() => {
        setItems((current) => companionLibraryApplyLocalEdits(current, appliedLibraryRevRef.current));
    }, [libraryRowEpoch]);

    const applyList = useCallback((listed: unknown, alreadyTaken = false) => {
        if (!alreadyTaken && !takeCompanionLibraryList(listed)) return;
        const paint = companionLibraryPaint(listed);
        const visible = companionLibraryTakeRestored(companionLibraryWithoutRemoved(paint.items, paint.libraryRev), paint.libraryRev);
        if (paint.libraryRev > appliedLibraryRevRef.current) appliedLibraryRevRef.current = paint.libraryRev;
        const snapshotKey = JSON.stringify(paint) + "\0" + visible.map((item) => item.id).join("\0");
        if (paintedRef.current && appliedSnapshotKeyRef.current === snapshotKey) return;
        appliedSnapshotKeyRef.current = snapshotKey;
        setItems(visible);
        setNotice(paint.notice);
        setSyncing(paint.syncing);
        paintedRef.current = true;
    }, []);

    const reload = useCallback(async () => {
        const started = libraryDeleteGen?.current ?? 0;
        if (!paintedRef.current) setLoading(true);
        try {
            const listed = await ListFileCompanionLibrary() as { items?: unknown[]; Items?: unknown[]; updates?: unknown; Updates?: unknown; removed?: unknown; Removed?: unknown };
            if (libraryDeleteGen && libraryDeleteGen.current !== started) return;
            // The window records this generation once, then paints the open
            // document. Taking it here as well dropped notes and share bindings
            // whenever this view's read finished first.
            const handoff = listedRef.current;
            if (handoff) {
                if (handoff(listed) === false) return;
                appliedListRef.current = listed;
                applyList(listed, true);
                return;
            }
            appliedListRef.current = listed;
            applyList(listed);
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
        applyList(listedSnapshot, true);
    }, [listedSnapshot, applyList]);

    const applyCatalog = useCallback((raw: unknown) => {
        const next = readCompanionLibraryCatalog(raw);
        // A list that started before a create or a move can return rev 0.
        // Any older rev, including 0, must not cover the catalog already on screen.
        if (next.rev < catalogRevRef.current) return;
        catalogRevRef.current = next.rev;
        setFolders(next.folders);
        setPlacements(next.placements);
        setDefaultFolderID(next.defaultId);
    }, []);

    const refreshCatalog = useCallback(async () => {
        if (typeof ListFileCompanionCatalog !== "function") return;
        try {
            applyCatalog(await ListFileCompanionCatalog());
        } catch {
            // The document list still opens when the folder file cannot be read.
        }
    }, [applyCatalog]);

    // A library poll can replace a row id without passing back through the
    // folder file. List again so the server copies the path placement onto
    // the row that now owns the file. The same ids do not ask twice.
    const catalogItemKey = useMemo(() => items.map((item) => item.id).sort().join("\n"), [items]);
    useEffect(() => {
        void refreshCatalog();
    }, [refreshCatalog, catalogItemKey]);
    useEffect(() => {
        const onCatalog = () => { void refreshCatalog(); };
        window.addEventListener(companionCatalogChangedEvent, onCatalog);
        return () => window.removeEventListener(companionCatalogChangedEvent, onCatalog);
    }, [refreshCatalog]);

    useEffect(() => {
        if (selectedFolder === "" || selectedFolder === "none") return;
        if (!folders.some((folder) => folder.id === selectedFolder)) setSelectedFolder("");
    }, [folders, selectedFolder]);

    useEffect(() => {
        const onKey = (event: KeyboardEvent) => {
            if (event.key !== "Escape" || event.defaultPrevented || dialogOpenRef.current || confirmOpenRef.current || folderPromptRef.current) return;
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
    const marksJSONOf = (path: string): string => companionProjectMarksJSON(path);

    const syncRow = async (item: CompanionLibraryItem) => {
        if (!item.path || rowBusy[item.id]) return;
        beginRow(item.id, "sync");
        try {
            await SyncFileCompanionShare(item.path, marksJSONOf(item.path));
            showToast(cs("已同步到云端"), "success", 6000);
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
            const dest = await FileCompanionExportProject(item.path, companionProjectExportJSON(item.path, item.notes));
            if (dest) {
                showToast(cs("已导出项目"), "success", 6000);
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
            const name = item.name.trim() || cs("这个项目");
            const confirm = companionRemoveConfirm(item, name);
            confirmOpenRef.current = true;
            let ok = false;
            try {
                ok = await showConfirm(
                    confirm.message,
                    confirm.title,
                    {
                        confirmText: item.source === "cloud" && item.mine && companionOwnPaper(item.path)
                            ? (item.expired ? cs("移除分享") : cs("取消分享"))
                            : cs("删除"),
                        cancelText: cs("取消"),
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
                    const raw = await DeleteFileCompanionShare(item.token) as Record<string, unknown>;
                    if (libraryDeleteGen) libraryDeleteGen.current += 1;
                    const rev = numberOf(raw, "libraryRev", "LibraryRev");
                    const restoredLocal = companionRestoredLocalItem(raw, item.name);
                    companionLibraryHideRow(item, rev, appliedLibraryRevRef.current);
                    if (restoredLocal) companionLibraryShowRestored(restoredLocal, rev, appliedLibraryRevRef.current);
                    appliedSnapshotKeyRef.current = "";
                    setItems((current) => companionLibraryApplyLocalEdits(
                        current.filter((row) => companionLibraryRowKey(row) !== companionLibraryRowKey(item)),
                        appliedLibraryRevRef.current,
                    ));
                    showToast(cs("已取消分享，参与者会移除并关闭该文档"), "success", 6000);
                } else {
                    const raw = await DeleteFileCompanionProject(item.id, item.path) as Record<string, unknown>;
                    // Any list that started before this delete finished still has
                    // the project. Drop it. The reload below starts after the bump.
                    if (libraryDeleteGen) libraryDeleteGen.current += 1;
                    const rev = numberOf(raw, "libraryRev", "LibraryRev");
                    const restoredLocal = companionRestoredLocalItem(raw, item.name);
                    companionLibraryHideRow(item, rev, appliedLibraryRevRef.current);
                    if (restoredLocal) companionLibraryShowRestored(restoredLocal, rev, appliedLibraryRevRef.current);
                    appliedSnapshotKeyRef.current = "";
                    setItems((current) => companionLibraryApplyLocalEdits(
                        current.filter((row) => companionLibraryRowKey(row) !== companionLibraryRowKey(item)),
                        appliedLibraryRevRef.current,
                    ));
                    const path = textOf(raw, "path", "Path") || item.path;
                    const cleared = boolOf(raw, "cleared", "Cleared");
                    const promoted = boolOf(raw, "promoted", "Promoted");
                    const restored = boolOf(raw, "restored", "Restored");
                    if (cleared && path) {
                        savePdfMarks(path, []);
                        savePdfTranslations(path, []);
                        savePdfDrawings(path, []);
                        savePdfBookmarks(path, []);
                        saveChatMarks(path, []);
                        saveChatAnnotations(path, []);
                        onProjectDeleted?.(path);
                    }
                    showToast(
                        restored
                            ? cs("已回到本机项目，阅读记录仍在")
                            : promoted
                                ? cs("这份文档已经是云端项目")
                                : cleared ? cs("已删除伴读数据，源文件仍在") : cs("已从文档管理移除，源文件仍在"),
                        "success",
                        6000,
                    );
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

    const beginFolder = () => {
        if (folderBusyRef.current) return false;
        folderBusyRef.current = true;
        setFolderBusy(true);
        return true;
    };

    const endFolder = () => {
        folderBusyRef.current = false;
        setFolderBusy(false);
    };

    const setDefaultFolder = async (folderID: string) => {
        if (typeof SetFileCompanionDefaultFolder !== "function" || !beginFolder()) return;
        try {
            applyCatalog(await SetFileCompanionDefaultFolder(folderID));
        } catch (err) {
            showToast(libraryError(err), "error", 6000);
        } finally {
            endFolder();
        }
    };

    const defaultButton = (folderID: string, testId: string) => {
        const active = folderID === defaultFolderID;
        return (
            <button
                type="button"
                className="file-companion-folder-icon file-companion-folder-default"
                data-testid={testId}
                aria-pressed={active}
                aria-label={active ? cs("默认") : cs("设为默认")}
                title={active ? cs("默认") : cs("设为默认")}
                disabled={folderBusy}
                onClick={() => { if (!active) void setDefaultFolder(folderID); }}
            >
                {active ? cs("默认") : cs("设为默认")}
            </button>
        );
    };

    const createFolder = async () => {
        const problem = companionFolderNameError(folderDraft, folders);
        if (problem) {
            showToast(problem, "error", 6000);
            return;
        }
        if (typeof CreateFileCompanionFolder !== "function" || !beginFolder()) return;
        const known = new Set(folders.map((folder) => folder.id));
        try {
            const saved = readCompanionLibraryCatalog(await CreateFileCompanionFolder(folderDraft.trim()));
            applyCatalog(saved);
            setFolderDraft("");
            const created = saved.folders.find((folder) => !known.has(folder.id));
            if (created) setSelectedFolder(created.id);
            showToast(cs("已创建文件夹"), "success", 6000);
        } catch (err) {
            showToast(libraryError(err), "error", 6000);
        } finally {
            endFolder();
        }
    };

    const renameFolder = async (id: string) => {
        let started = false;
        try {
            const cleaned = editingName.trim();
            const current = folders.find((folder) => folder.id === id);
            if (current && current.name === cleaned) {
                setEditingID("");
                return;
            }
            const problem = companionFolderNameError(editingName, folders, id);
            if (problem) {
                showToast(problem, "error", 6000);
                return;
            }
            if (typeof RenameFileCompanionFolder !== "function" || !beginFolder()) return;
            started = true;
            try {
                applyCatalog(await RenameFileCompanionFolder(id, cleaned));
                setEditingID("");
                showToast(cs("已重命名文件夹"), "success", 6000);
            } catch (err) {
                showToast(libraryError(err), "error", 6000);
            }
        } finally {
            if (started) endFolder();
            renameHoldRef.current = false;
        }
    };

    const commitDeleteFolder = async (id: string, mode: "" | "merge" | "remove", targetID: string) => {
        if (typeof DeleteFileCompanionFolder !== "function" || !beginFolder()) return;
        let removed = false;
        try {
            applyCatalog(await DeleteFileCompanionFolder(id, mode, targetID));
            if (selectedFolder === id) setSelectedFolder("");
            setPendingDelete(null);
            setEditingID("");
            showToast(cs("已删除文件夹"), "success", 6000);
            removed = mode === "remove";
        } catch (err) {
            showToast(libraryError(err), "error", 6000);
            await refreshCatalog();
            // A partial remove can drop some rows before the folder stays.
            removed = mode === "remove";
        } finally {
            endFolder();
        }
        if (!removed) return;
        try {
            await reload();
        } catch (err) {
            showToast(libraryError(err), "error", 6000);
        }
    };

    const askDeleteFolder = async (folder: CompanionLibraryFolder, count: number) => {
        if (count > 0) {
            setPendingDelete({ ...folder, count });
            return;
        }
        if (deleteGate.current) return;
        deleteGate.current = true;
        confirmOpenRef.current = true;
        let ok = false;
        try {
            ok = await showConfirm(
                companionFill(getCompanionUiLang(), "确认删除空文件夹「${name}」？", { name: folder.name }),
                cs("删除文件夹"),
                { confirmText: cs("删除"), cancelText: cs("取消"), confirmVariant: "danger" },
            );
        } finally {
            confirmOpenRef.current = false;
        }
        try {
            if (ok) await commitDeleteFolder(folder.id, "", "");
        } finally {
            deleteGate.current = false;
        }
    };

    const moveItem = async (item: CompanionLibraryItem, folderID: string) => {
        if (typeof MoveFileCompanionLibraryItem !== "function" || !beginFolder()) return;
        try {
            applyCatalog(await MoveFileCompanionLibraryItem(item.id, item.path || "", folderID));
            const folder = folders.find((entry) => entry.id === folderID);
            showToast(
                folder
                    ? companionFill(getCompanionUiLang(), "已移动到「${name}」", { name: folder.name })
                    : cs("已移出文件夹"),
                "success",
                6000,
            );
        } catch (err) {
            showToast(libraryError(err), "error", 6000);
        } finally {
            endFolder();
        }
    };

    const libraryView = useMemo(() => {
        const known = new Set(folders.map((folder) => folder.id));
        const placedOf = (item: CompanionLibraryItem) => {
            const id = companionLibraryFolderID(item, placements);
            return known.has(id) ? id : "";
        };
        const inFolder = (item: CompanionLibraryItem) => {
            if (selectedFolder === "") return true;
            const id = placedOf(item);
            if (selectedFolder === "none") return id === "";
            return id === selectedFolder;
        };
        const inSource = (item: CompanionLibraryItem) => {
            if (sourceFilter === "local") return item.source !== "cloud";
            if (sourceFilter === "cloud") return item.source === "cloud";
            return true;
        };
        const localItems: CompanionLibraryItem[] = [];
        const cloudItems: CompanionLibraryItem[] = [];
        let placedCount = 0;
        let scopedCount = 0;
        for (const item of items) {
            if (!inFolder(item)) continue;
            placedCount += 1;
            if (!inSource(item)) continue;
            scopedCount += 1;
            if (!companionLibraryNameMatch(item.name, nameQuery)) continue;
            if (item.source === "cloud") cloudItems.push(item);
            else localItems.push(item);
        }
        const counts = new Map<string, number>();
        for (const folder of folders) counts.set(folder.id, 0);
        let uncategorized = 0;
        for (const item of items) {
            const id = placedOf(item);
            if (!id) {
                uncategorized++;
                continue;
            }
            counts.set(id, (counts.get(id) ?? 0) + 1);
        }
        const countOf = (folderID: string) => {
            if (folderID === "") return items.length;
            if (folderID === "none") return uncategorized;
            return counts.get(folderID) ?? 0;
        };
        return {
            placedOf,
            localItems,
            cloudItems,
            placedCount,
            scopedCount,
            shownCount: localItems.length + cloudItems.length,
            countOf,
        };
    }, [items, folders, placements, selectedFolder, nameQuery, sourceFilter]);
    const { localItems, cloudItems, placedCount, shownCount, scopedCount, countOf, placedOf } = libraryView;
    const nameNarrowed = narrowedLibrary(nameQuery, selectedFolder);
    const hideLocal = sourceFilter === "cloud" || (nameNarrowed && localItems.length === 0);
    const hideCloud = sourceFilter === "local" || (nameNarrowed && cloudItems.length === 0);

    const endLibraryDrag = () => {
        dragItemIDRef.current = "";
        dragFolderRef.current = "";
        dragScrollRef.current.at = 0;
        dragScrollRef.current.dir = 0;
        dragRailBoxRef.current = null;
        dropRowRef.current?.removeAttribute("data-drop");
        dropRowRef.current = null;
        const card = dragCardRef.current;
        if (card) {
            card.removeAttribute("data-dragging");
            card.style.contentVisibility = "";
        }
        dragCardRef.current = null;
        dragRootRef.current?.removeAttribute("data-library-drag");
        dragRootRef.current = null;
    };
    // Unmount clears a drag that is still in flight. The ref follows the
    // latest clearer, and that clearer only touches the drag refs. dragend
    // on the window still runs when the source node is already gone, so the
    // text fields do not stay click-through.
    const endLibraryDragRef = useRef(endLibraryDrag);
    endLibraryDragRef.current = endLibraryDrag;
    useEffect(() => {
        const end = () => endLibraryDragRef.current();
        window.addEventListener("dragend", end);
        return () => {
            window.removeEventListener("dragend", end);
            end();
        };
    }, []);

    // key is "none" for 未分类 or a folder id. 全部 is not a drop target.
    // A card already in that class is refused so the row does not light up.
    // Highlight is written on the row itself: a dragover must not re-render
    // every document card.
    const acceptFolderDrop = (key: string) => {
        if (!key || !dragItemIDRef.current || folderBusyRef.current) return false;
        const next = key === "none" ? "" : key;
        if (next !== "" && !folders.some((folder) => folder.id === next)) return false;
        return next !== dragFolderRef.current;
    };

    const paintDropRow = (row: HTMLElement | null) => {
        if (dropRowRef.current === row) return;
        dropRowRef.current?.removeAttribute("data-drop");
        if (row) row.setAttribute("data-drop", "over");
        dropRowRef.current = row;
    };

    const folderDrop = (key: string) => ({
        onDragEnter: (event: React.DragEvent<HTMLDivElement>) => {
            if (!acceptFolderDrop(key)) return;
            event.preventDefault();
            paintDropRow(event.currentTarget);
        },
        onDragOver: (event: React.DragEvent<HTMLDivElement>) => {
            if (!acceptFolderDrop(key)) {
                if (dropRowRef.current === event.currentTarget) paintDropRow(null);
                return;
            }
            event.preventDefault();
            event.dataTransfer.dropEffect = "move";
            paintDropRow(event.currentTarget);
        },
        onDragLeave: (event: React.DragEvent<HTMLDivElement>) => {
            const next = event.relatedTarget;
            if (next instanceof Node && event.currentTarget.contains(next)) return;
            if (dropRowRef.current === event.currentTarget) paintDropRow(null);
        },
        onDrop: (event: React.DragEvent<HTMLDivElement>) => {
            // The payload survives dragend. A host that ends the drag before
            // the drop still files the document.
            const id = companionDragItemID(event.dataTransfer) || dragItemIDRef.current;
            const next = key === "none" ? "" : key;
            const item = id ? items.find((row) => row.id === id) : undefined;
            const known = next === "" || folders.some((folder) => folder.id === next);
            if (!item || !known || folderBusyRef.current || placedOf(item) === next) {
                if (dropRowRef.current === event.currentTarget) paintDropRow(null);
                return;
            }
            event.preventDefault();
            endLibraryDrag();
            void moveItem(item, next);
        },
    });

    // The rail scrolls on its own. Only the outer strip moves, and a folder
    // under the pointer stays put so the drop can land on it.
    const onFolderRailDragOver = (event: React.DragEvent<HTMLDivElement>) => {
        if (!dragItemIDRef.current) return;
        const rail = event.currentTarget;
        let box = dragRailBoxRef.current;
        if (!box) {
            if (rail.scrollHeight <= rail.clientHeight + 1) {
                dragRailBoxRef.current = "short";
                return;
            }
            const rect = rail.getBoundingClientRect();
            box = { top: rect.top, bottom: rect.bottom };
            dragRailBoxRef.current = box;
        }
        if (box === "short") return;
        const fromTop = event.clientY - box.top;
        const fromBottom = box.bottom - event.clientY;
        const edge = 18;
        let dir = 0;
        if (fromTop >= 0 && fromTop < edge) dir = -1;
        else if (fromBottom >= 0 && fromBottom < edge) dir = 1;
        if (!dir) return;
        const overRow = event.target instanceof Element && !!event.target.closest(".file-companion-folder-row");
        if (overRow && (dir < 0 ? fromTop > 8 : fromBottom > 8)) return;
        const now = event.timeStamp || performance.now();
        const prev = dragScrollRef.current;
        if (prev.dir === dir && now - prev.at < 70) return;
        dragScrollRef.current = { at: now, dir };
        rail.scrollTop += dir * 8;
    };
    // Blink takes the drag-image bitmap when dragstart returns. The card stays
    // painted through that snapshot. The fade waits until the next turn, and
    // the inline visibility is cleared when the drag ends.
    const beginLibraryDrag = (
        event: React.DragEvent<HTMLElement>,
        item: CompanionLibraryItem,
        canDrag: boolean,
    ) => {
        if (!canDrag) {
            event.preventDefault();
            return;
        }
        // text/plain is the payload every webview accepts.
        // The custom type is optional and is what drop reads first.
        event.dataTransfer.effectAllowed = "move";
        event.dataTransfer.setData("text/plain", companionLibraryDragTextPrefix + item.id);
        try {
            event.dataTransfer.setData(companionLibraryDragType, item.id);
        } catch {
            // A host that rejects a custom type still has the prefixed text.
        }
        dragItemIDRef.current = item.id;
        dragFolderRef.current = placedOf(item);
        const card = event.currentTarget.closest(".file-companion-project");
        if (!(card instanceof HTMLElement)) return;
        const previous = dragCardRef.current;
        if (previous && previous !== card) {
            previous.removeAttribute("data-dragging");
            previous.style.contentVisibility = "";
        }
        dragCardRef.current = card;
        card.style.contentVisibility = "visible";
        const root = event.currentTarget.closest(".file-companion-library-dialog");
        if (root instanceof HTMLElement) {
            dragRootRef.current = root;
            root.setAttribute("data-library-drag", "true");
        }
        const rect = card.getBoundingClientRect();
        const rawX = event.clientX - rect.left;
        const rawY = event.clientY - rect.top;
        const hotX = Number.isFinite(rawX) ? rawX : 16;
        const hotY = Number.isFinite(rawY) ? rawY : 16;
        try {
            // The hotspot is where the pointer grabbed the card. A fixed
            // corner makes the ghost jump up to the title.
            event.dataTransfer.setDragImage(
                card,
                Math.max(0, Math.min(rect.width > 0 ? rect.width : hotX, hotX)),
                Math.max(0, Math.min(rect.height > 0 ? rect.height : hotY, hotY)),
            );
        } catch {
            // The dragged node stays the ghost.
        }
        queueMicrotask(() => {
            if (dragCardRef.current === card) card.setAttribute("data-dragging", "true");
        });
    };
    // A local document that was already published has a cloud row beside it.
    // The local card must offer 修改 on that row instead of 分享, or the same
    // paper ends up with two links. An expired link is not live, so it does
    // not hold that seat any more.
    const sharedByPath = useMemo(() => {
        const map = new Map<string, CompanionLibraryItem>();
        for (const item of items) {
            if (item.source !== "cloud" || !item.mine || item.expired || !item.path) continue;
            const key = companionSharePathKey(item.path);
            if (key && !map.has(key)) map.set(key, item);
        }
        return map;
    }, [items]);
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
        const discussion = item.source === "cloud" ? companionDiscussionCounts(item.notes) : null;
        const notes = discussion?.total ?? 0;
        const pendingReviews = item.host ? (discussion?.explicitPending ?? 0) : 0;
        const meta = [
            item.source === "cloud"
                ? (item.mine ? cs("我的分享") : (item.owner || cs("别人的分享")))
                : cs("本机"),
            fmtSize(item.size),
            // A cloud row's openedAt is when the share was created, so the
            // card says 分享于 instead of 打开于.
            dateOf(item.openedAt) ? companionFill(getCompanionUiLang(), item.source === "cloud" ? "分享于 ${date}" : "打开于 ${date}", { date: dateOf(item.openedAt) }) : "",
        ].filter(Boolean).join(" · ");
        const isOpen = item.path === activePath;
        // A cloud row is itself a share. A local row is only shared when this
        // person already published the same path, and then 修改 edits that row.
        // An expired row no longer holds either seat: the paper can go out again.
        const shareRow = item.source === "cloud"
            ? (item.mine && !item.expired ? item : null)
            : (sharedByPath.get(companionSharePathKey(item.path)) ?? null);
        const alreadyShared = Boolean(shareRow) || (item.source === "cloud" && !item.expired);
        const canDrag = folders.length > 0 && !busy && !folderBusy;
        const kind = (
            <span className="file-companion-project-kind" data-kind={kindOf(item.name)} aria-hidden="true">{kindLabel(item.name)}</span>
        );
        return (
            <div
                key={item.id}
                className="file-companion-project"
                data-testid={`file-companion-library-card-${item.id}`}
                data-open={isOpen ? "true" : undefined}
            >
                {folders.length > 0 ? (
                    <div
                        className="file-companion-project-drag"
                        data-testid={`file-companion-library-drag-${item.id}`}
                        draggable={canDrag}
                        title={cs("拖到左侧文件夹以分类")}
                        onDragStart={(event) => beginLibraryDrag(event, item, canDrag)}
                        onDragEnd={() => endLibraryDragRef.current()}
                    >
                        <span className="file-companion-project-grip" aria-hidden="true" />
                        {kind}
                    </div>
                ) : kind}
                <div className="file-companion-project-main">
                    <div className="file-companion-project-titleline">
                        <button
                            type="button"
                            className="file-companion-project-title"
                            data-testid={`file-companion-library-item-${item.id}`}
                            title={item.path ? companionFill(getCompanionUiLang(), "打开 ${name}", { name: item.name }) : companionFill(getCompanionUiLang(), "导入 ${name} 到本机", { name: item.name })}
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
                        {isOpen ? <span className="file-companion-badge" data-tone="active">{cs("打开中")}</span> : null}
                        {!item.path && item.source === "cloud" ? <span className="file-companion-badge" data-tone="pending">{cs("未导入")}</span> : null}
                    </div>
                    <p
                        className="file-companion-project-meta"
                        data-testid={`file-companion-library-meta-${item.id}`}
                        draggable={canDrag}
                        onDragStart={(event) => beginLibraryDrag(event, item, canDrag)}
                        onDragEnd={() => endLibraryDragRef.current()}
                    >
                        <span className="file-companion-badge" data-tone="source">{item.source === "cloud" ? cs("云端") : cs("本机")}</span>
                        {item.source === "cloud" ? <span className="file-companion-badge">{audienceLabel(item.visibility)}</span> : null}
                        {item.source === "cloud" && item.host ? (
                            <span className="file-companion-badge" data-tone="active">{cs("主持人")}</span>
                        ) : null}
                        <span className="file-companion-badge">{whenLabel(item)}</span>
                        {marks > 0 ? <span className="file-companion-badge" data-tone="mark">{companionFill(getCompanionUiLang(), "标注 ${count}", { count: marks })}</span> : null}
                        {notes > 0 ? <span className="file-companion-badge" data-tone="note">{companionFill(getCompanionUiLang(), "讨论 ${count}", { count: notes })}</span> : null}
                        {pendingReviews > 0 ? <span className="file-companion-badge" data-tone="pending" data-testid={`file-companion-library-pending-${item.id}`}>{companionFill(getCompanionUiLang(), "待审核 ${count}", { count: pendingReviews })}</span> : null}
                        <span className="file-companion-project-facts">{meta}</span>
                    </p>
                    {item.shareUrl && !item.expired ? <small className="file-companion-project-url" data-testid={`file-companion-library-url-${item.id}`}>{item.shareUrl}</small> : null}
                </div>
                <div className="file-companion-project-actions">
                    {folders.length > 0 ? (
                        <select
                            className="file-companion-folder-move"
                            data-testid={`file-companion-library-move-${item.id}`}
                            aria-label={cs("移动到文件夹")}
                            title={cs("移动到文件夹")}
                            value={placedOf(item)}
                            disabled={Boolean(busy) || folderBusy}
                            onChange={(event) => { void moveItem(item, event.target.value); }}
                        >
                            <option value="">{cs("未分类")}</option>
                            {folders.map((folder) => <option key={folder.id} value={folder.id}>{folder.name}</option>)}
                        </select>
                    ) : null}
                    {item.expired ? (
                        <span
                            className="file-companion-badge"
                            data-tone="mark"
                            data-testid={`file-companion-library-expired-mark-${item.id}`}
                            title={cs("分享已过期，可以重新分享")}
                        >
                            {cs("已过期")}
                        </span>
                    ) : alreadyShared ? (
                        <span
                            className="file-companion-project-shared"
                            data-testid={`file-companion-library-shared-mark-${item.id}`}
                            title={sharedMarkTitle(item, shareRow)}
                        >
                            <LibraryGlyph name="share" size={12} />
                            <span>{item.source === "cloud" && !item.mine ? cs("收到的") : cs("已分享")}</span>
                        </span>
                    ) : (
                        <button
                            type="button"
                            className="file-companion-project-action"
                            data-testid={`file-companion-library-share-action-${item.id}`}
                            disabled={!item.path || Boolean(busy)}
                            title={cs("分享 — 上传文档、高亮和聊天，生成云端链接")}
                            onClick={() => onShareProject(item.path)}
                        >
                            {cs("分享")}
                        </button>
                    )}
                    {item.source === "cloud" && shareRow ? (
                        <button
                            type="button"
                            className="file-companion-project-action"
                            data-testid={`file-companion-library-edit-share-${item.id}`}
                            disabled={Boolean(busy)}
                            title={cs("修改分享 — 调整分享范围、有效期，链接不变")}
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
                            {cs("修改")}
                        </button>
                    ) : null}
                    {item.path && item.source === "cloud" && item.host ? (
                        <button
                            type="button"
                            className="file-companion-project-action"
                            data-testid={`file-companion-library-sync-${item.id}`}
                            disabled={Boolean(busy)}
                            title={cs("同步 — 把这份项目的新标注和聊天推到云端")}
                            onClick={() => { void syncRow(item); }}
                        >
                            {busy === "sync" ? cs("同步中…") : cs("同步")}
                        </button>
                    ) : null}
                    {item.path ? (
                        <button
                            type="button"
                            className="file-companion-project-action"
                            data-testid={`file-companion-library-export-${item.id}`}
                            disabled={Boolean(busy)}
                            title={cs("导出项目 — 打包文档、高亮、批注和讨论为 zip")}
                            onClick={() => { void exportRow(item); }}
                        >
                            {busy === "export" ? cs("导出中…") : cs("导出")}
                        </button>
                    ) : null}
                    <button
                        type="button"
                        className="file-companion-project-action"
                        data-testid={`file-companion-library-delete-${item.id}`}
                        disabled={Boolean(busy)}
                        title={item.source === "cloud"
                            ? (item.mine && companionOwnPaper(item.path)
                                ? (item.expired
                                    ? cs("移除分享 — 文档回到本机项目，阅读记录保留")
                                    : cs("取消分享 — 参与者会移除并关闭该文档，本机文档回到本机项目"))
                                : item.mine && !item.expired
                                    ? cs("取消分享 — 参与者会移除并关闭该文档")
                                    : item.expired ? cs("移除已过期的分享") : cs("从文档管理移除；云端分享仍属于分享者"))
                            : cs("删除伴读数据，不删除源文件")}
                        onClick={() => { void deleteRow(item); }}
                    >
                        {busy === "delete"
                            ? (item.source === "cloud" && item.mine ? cs("取消中…") : cs("删除中…"))
                            : item.source === "cloud" && item.mine && !item.expired ? cs("取消分享") : item.source === "cloud" ? cs("移除") : cs("删除")}
                    </button>
                </div>
            </div>
        );
    };

    return (
        <div className="file-companion-dialog" data-testid="file-companion-library-panel" role="dialog" aria-modal="true" aria-labelledby="file-companion-library-title">
            <div className="file-companion-dialog-scrim" data-testid="file-companion-library-scrim" onMouseDown={() => { if (confirmOpenRef.current || folderPromptRef.current) return; onExit(); }} />
            <div
                className="file-companion-dialog-card file-companion-library-dialog"
                ref={cardRef}
                onDragOver={(event) => {
                    const field = companionDragTextField(event.target);
                    if (!field || field.closest(".file-companion-folder-row")) return;
                    if (!dragItemIDRef.current && !companionDragItemID(event.dataTransfer)) return;
                    event.preventDefault();
                    event.dataTransfer.dropEffect = "none";
                }}
                onDrop={(event) => {
                    const field = companionDragTextField(event.target);
                    if (!field) return;
                    if (!dragItemIDRef.current && !companionDragItemID(event.dataTransfer)) return;
                    event.preventDefault();
                }}
            >
                <header className="file-companion-dialog-head">
                    <div className="file-companion-dialog-heading">
                        <h2 id="file-companion-library-title">{cs("文档管理")}</h2>
                        <p className="file-companion-dialog-sub">
                            {items.length > 0
                                ? companionFill(getCompanionUiLang(), "每个项目包含文档、高亮和讨论，共 ${count} 个", { count: items.length })
                                : loading || syncing
                                    ? cs("每个项目包含文档、高亮和讨论")
                                    : cs("每个项目包含文档、高亮和讨论，还没有项目")}
                        </p>
                    </div>
                    <div className="file-companion-dialog-head-actions">
                        <button
                            type="button"
                            className="file-companion-dialog-secondary"
                            data-testid="file-companion-library-import-open"
                            title={cs("导入分享 — 粘贴链接存进本机")}
                            onClick={() => onImportLink("")}
                        >
                            {cs("导入分享")}
                        </button>
                        <button
                            type="button"
                            className="file-companion-dialog-x"
                            data-testid="file-companion-library-back"
                            aria-label={cs("关闭")}
                            title={cs("关闭")}
                            onClick={onExit}
                        >
                            <LibraryGlyph name="x" size={13} />
                            <span className="file-companion-sr">{cs("关闭")}</span>
                        </button>
                    </div>
                </header>
                <div className="file-companion-library-body">
                    <aside className="file-companion-library-rail" aria-label={cs("文件夹")}>
                        <form
                            className="file-companion-library-folder-create"
                            data-testid="file-companion-folder-create"
                            onSubmit={(event) => {
                                event.preventDefault();
                                void createFolder();
                            }}
                        >
                            <input
                                className="file-companion-dialog-input"
                                data-testid="file-companion-folder-name"
                                aria-label={cs("新文件夹")}
                                placeholder={cs("新文件夹")}
                                value={folderDraft}
                                disabled={folderBusy}
                                onChange={(event) => setFolderDraft(event.target.value)}
                                onKeyDown={(event) => {
                                    // Enter confirms an IME candidate. It must not also create the folder.
                                    if (event.nativeEvent.isComposing && event.key === "Enter") event.preventDefault();
                                }}
                            />
                            <button type="submit" className="file-companion-dialog-primary" data-testid="file-companion-folder-add" disabled={folderBusy || !folderDraft.trim()}>
                                {cs("创建")}
                            </button>
                        </form>
                        <div
                            className="file-companion-library-tree"
                            role="tree"
                            aria-label={cs("文件夹")}
                            data-testid="file-companion-library-folders"
                            onDragOver={onFolderRailDragOver}
                        >
                            <FolderTreeRow
                                testId="file-companion-folder-all"
                                label={cs("全部")}
                                count={countOf("")}
                                selected={selectedFolder === ""}
                                onSelect={() => setSelectedFolder("")}
                            />
                            <FolderTreeRow
                                testId="file-companion-folder-none"
                                label={cs("未分类")}
                                count={countOf("none")}
                                selected={selectedFolder === "none"}
                                onSelect={() => setSelectedFolder("none")}
                                drop={folderDrop("none")}
                                trail={defaultButton("", "file-companion-folder-default-none")}
                            />
                            {folders.map((folder) => (
                                <div key={folder.id} className="file-companion-folder-row" role="treeitem" aria-selected={selectedFolder === folder.id} aria-level={1} {...folderDrop(folder.id)}>
                                    {editingID === folder.id ? (
                                        <form
                                            className="file-companion-folder-rename"
                                            onSubmit={(event) => {
                                                event.preventDefault();
                                                renameHoldRef.current = true;
                                                void renameFolder(folder.id);
                                            }}
                                        >
                                            <input
                                                className="file-companion-dialog-input"
                                                data-testid={`file-companion-folder-rename-input-${folder.id}`}
                                                aria-label={cs("重命名文件夹")}
                                                value={editingName}
                                                disabled={folderBusy}
                                                autoFocus
                                                onChange={(event) => setEditingName(event.target.value)}
                                                onBlur={() => {
                                                    if (renameHoldRef.current) return;
                                                    setEditingID("");
                                                }}
                                                onKeyDown={(event) => {
                                                    if (event.nativeEvent.isComposing && event.key === "Enter") {
                                                        event.preventDefault();
                                                        return;
                                                    }
                                                    if (event.nativeEvent.isComposing) return;
                                                    if (event.key === "Escape") {
                                                        event.preventDefault();
                                                        event.stopPropagation();
                                                        setEditingID("");
                                                    }
                                                }}
                                            />
                                        </form>
                                    ) : (
                                        <button
                                            type="button"
                                            className="file-companion-folder-name"
                                            data-testid={`file-companion-folder-${folder.id}`}
                                            onClick={() => setSelectedFolder(folder.id)}
                                        >
                                            <LibraryGlyph name="folder" size={14} />
                                            <span>{folder.name}</span>
                                        </button>
                                    )}
                                    <span className="file-companion-folder-count">{countOf(folder.id)}</span>
                                    {defaultButton(folder.id, `file-companion-folder-default-${folder.id}`)}
                                    <button
                                        type="button"
                                        className="file-companion-folder-icon"
                                        data-testid={`file-companion-folder-rename-${folder.id}`}
                                        aria-label={editingID === folder.id ? cs("保存") : cs("重命名文件夹")}
                                        title={editingID === folder.id ? cs("保存") : cs("重命名文件夹")}
                                        disabled={folderBusy}
                                        onMouseDown={(event) => {
                                            // Keep the field focused so blur does not cancel the name
                                            // before this click can save it.
                                            if (editingID === folder.id) event.preventDefault();
                                        }}
                                        onClick={() => {
                                            if (editingID === folder.id) {
                                                renameHoldRef.current = true;
                                                void renameFolder(folder.id);
                                                return;
                                            }
                                            setEditingID(folder.id);
                                            setEditingName(folder.name);
                                            setSelectedFolder(folder.id);
                                        }}
                                    >
                                        {editingID === folder.id ? cs("保存") : cs("重命名")}
                                    </button>
                                    <button
                                        type="button"
                                        className="file-companion-folder-icon"
                                        data-testid={`file-companion-folder-delete-${folder.id}`}
                                        aria-label={cs("删除文件夹")}
                                        title={cs("删除文件夹")}
                                        disabled={folderBusy}
                                        onClick={() => { void askDeleteFolder(folder, countOf(folder.id)); }}
                                    >
                                        {cs("删除")}
                                    </button>
                                </div>
                            ))}
                        </div>
                    </aside>
                    <div className="file-companion-library-main">
                        <div className="file-companion-library-filter">
                            <input
                                className="file-companion-dialog-input"
                                data-testid="file-companion-library-filter"
                                aria-label={cs("按文件名过滤")}
                                placeholder={cs("按文件名过滤")}
                                value={nameQuery}
                                onChange={(event) => setNameQuery(event.target.value)}
                            />
                            {nameQuery ? (
                                <button
                                    type="button"
                                    className="file-companion-dialog-secondary"
                                    data-testid="file-companion-library-filter-clear"
                                    onClick={() => setNameQuery("")}
                                >
                                    {cs("清除过滤")}
                                </button>
                            ) : null}
                            <div className="file-companion-segment file-companion-library-source" role="group" aria-label={cs("来源")}>
                                <button type="button" aria-pressed={sourceFilter === "all"} data-testid="file-companion-library-source-all" onClick={() => setSourceFilter("all")}>{cs("所有")}</button>
                                <button type="button" aria-pressed={sourceFilter === "local"} data-testid="file-companion-library-source-local" onClick={() => setSourceFilter("local")}>{cs("本地")}</button>
                                <button type="button" aria-pressed={sourceFilter === "cloud"} data-testid="file-companion-library-source-cloud" onClick={() => setSourceFilter("cloud")}>{cs("云端")}</button>
                            </div>
                        </div>
                        {nameQuery.trim() ? (
                            <p className="file-companion-library-filter-count" data-testid="file-companion-library-filter-count">
                                {companionFill(getCompanionUiLang(), "显示 ${shown} / 共 ${count} 个", { shown: shownCount, count: scopedCount })}
                            </p>
                        ) : null}
                        {notice ? <p className="file-companion-library-notice" data-testid="file-companion-library-notice" role="status">{cs(notice)}</p> : null}
                        {loading ? <p className="file-companion-library-note">{cs("正在读取…")}</p> : null}
                        {syncing && !loading ? <p className="file-companion-library-note">{cs("正在同步云端分享")}</p> : null}
                        {hideLocal && hideCloud && !loading && !syncing ? (
                            <p className="file-companion-library-empty" data-testid="file-companion-library-filter-empty">
                                {companionLibraryEmptyLabel(nameQuery, selectedFolder, sourceFilter, placedCount)}
                            </p>
                        ) : (
                            <>
                                {hideLocal ? null : (
                                    <section className="file-companion-library-section" data-testid="file-companion-library-local" aria-label={cs("本机项目")}>
                                        <h3 className="file-companion-dialog-label">{cs("本机项目")}<span className="file-companion-dialog-optional">{localItems.length}</span></h3>
                                        {localItems.length === 0
                                            ? (loading || syncing ? null : <p className="file-companion-library-empty">{cs("还没有本机项目 — 打开或导入一个文档开始")}</p>)
                                            : <div className="file-companion-project-list">{localItems.map(renderRow)}</div>}
                                    </section>
                                )}
                                {hideCloud ? null : (
                                    <section className="file-companion-library-section" data-testid="file-companion-library-cloud" aria-label={cs("云端项目")}>
                                        <h3 className="file-companion-dialog-label">{cs("云端项目")}<span className="file-companion-dialog-optional">{cloudItems.length}</span></h3>
                                        {cloudItems.length === 0
                                            ? (loading || syncing ? null : <p className="file-companion-library-empty">{cs("还没有云端项目 — 分享后会出现在这里")}</p>)
                                            : <div className="file-companion-project-list">{cloudItems.map(renderRow)}</div>}
                                    </section>
                                )}
                            </>
                        )}
                    </div>
                </div>
                {pendingDelete ? (
                    <FolderDeletePrompt
                        folder={pendingDelete}
                        count={pendingDelete.count}
                        folders={folders}
                        busy={folderBusy}
                        onCancel={() => { if (!folderBusy) setPendingDelete(null); }}
                        onConfirm={(mode, targetID) => { void commitDeleteFolder(pendingDelete.id, mode, targetID); }}
                    />
                ) : null}
            </div>
        </div>
    );
}

function narrowedLibrary(query: string, selectedFolder: string): boolean {
    return query.trim() !== "" || selectedFolder !== "";
}

// The combined empty line is only reached when the visible rails are gone.
// A folder that still holds the other source is not empty.
function companionLibraryEmptyLabel(query: string, selectedFolder: string, sourceFilter: "all" | "local" | "cloud", placedCount: number): string {
    if (query.trim() || (sourceFilter !== "all" && placedCount > 0)) return cs("没有匹配的文件");
    if (selectedFolder === "none") return cs("未分类里还没有文档");
    return cs("这个文件夹里还没有文档");
}

function FolderTreeRow({
    testId,
    label,
    count,
    selected,
    onSelect,
    drop,
    trail,
}: {
    testId: string;
    label: string;
    count: number;
    selected: boolean;
    onSelect: () => void;
    trail?: React.ReactNode;
    drop?: {
        onDragEnter: (event: React.DragEvent<HTMLDivElement>) => void;
        onDragOver: (event: React.DragEvent<HTMLDivElement>) => void;
        onDragLeave: (event: React.DragEvent<HTMLDivElement>) => void;
        onDrop: (event: React.DragEvent<HTMLDivElement>) => void;
    };
}) {
    return (
        <div className="file-companion-folder-row" role="treeitem" aria-selected={selected} aria-level={1} {...drop}>
            <button type="button" className="file-companion-folder-name" data-testid={testId} onClick={onSelect}>
                <span>{label}</span>
            </button>
            <span className="file-companion-folder-count">{count}</span>
            {trail}
        </div>
    );
}

function companionPlaceFileName(path: string): string {
    const name = path.trim().replace(/\\/g, "/").split("/").pop() || "";
    return name === "." || name === ".." ? "" : name;
}

// companionPlaceFolderID is the folder this file is already in. The catalog
// publishes that on the path key. A later browser add must offer this folder,
// so confirming does not move the file onto the default. No path key means
// the file is still uncategorized, and the default is the offer.
function companionPlaceFolderID(path: string, placements: Record<string, string> | null | undefined): string {
    if (!placements) return "";
    for (const key of companionFolderPathCandidates(path)) {
        const folderID = placements["path:" + key];
        if (!folderID || folderID === companionFolderUncategorized) continue;
        return folderID;
    }
    return "";
}

export function CompanionPlaceDialog({ path, name = "", onClose }: { path: string; name?: string; onClose: () => void }) {
    const [folders, setFolders] = useState<CompanionLibraryFolder[]>([]);
    const [selected, setSelected] = useState("");
    const [keptName, setKeptName] = useState("");
    const [busy, setBusy] = useState(false);
    const [ready, setReady] = useState(false);
    const [failed, setFailed] = useState(false);
    const [known, setKnown] = useState(false);
    const [notice, setNotice] = useState("");
    const touchedRef = useRef(false);
    const busyRef = useRef(false);
    const rootRef = useRef<HTMLDivElement>(null);
    const fileName = name.trim() || companionPlaceFileName(path);
    useEffect(() => {
        touchedRef.current = false;
        busyRef.current = false;
        setBusy(false);
        setReady(false);
        setFailed(false);
        setKnown(false);
        setNotice("");
        setSelected("");
        setKeptName("");
        setFolders([]);
        let dead = false;
        void (async () => {
            try {
                const catalog = readCompanionLibraryCatalog(await ListFileCompanionCatalog());
                if (dead || touchedRef.current) return;
                // The path key can belong to a hidden row of the same file.
                // The offer is the row an empty-id move changes. Explicit
                // 未分类 does not inherit that path key. A failed read must
                // not claim the file is uncategorized.
                let current = companionPlaceFolderID(path, catalog.placements);
                let know = true;
                if (typeof FileCompanionFolderForPath === "function") {
                    try {
                        const folder = await FileCompanionFolderForPath(path);
                        if (dead || touchedRef.current) return;
                        if (typeof folder === "string") current = folder.trim();
                    } catch (err) {
                        if (!dead && !touchedRef.current) setNotice(libraryError(err));
                        current = "";
                        know = false;
                    }
                }
                if (dead || touchedRef.current) return;
                const kept = catalog.folders.find((folder) => folder.id === current);
                setFolders(catalog.folders);
                setSelected(kept ? kept.id : catalog.defaultId);
                setKeptName(kept ? kept.name : "");
                setKnown(know);
            } catch (err) {
                if (!dead && !touchedRef.current) {
                    setNotice(libraryError(err));
                    setFailed(true);
                }
            } finally {
                if (!dead) setReady(true);
            }
        })();
        return () => { dead = true; };
    }, [path]);
    useEffect(() => {
        if (!ready) return;
        const root = rootRef.current;
        if (!root) return;
        const active = document.activeElement;
        if (active instanceof Node && root.contains(active) && active !== root) return;
        const checked = root.querySelector<HTMLInputElement>('input[type="radio"]:checked:not([disabled])');
        (checked ?? root.querySelector<HTMLButtonElement>("button:not([disabled])"))?.focus();
    }, [ready]);
    useEffect(() => {
        const onKey = (event: KeyboardEvent) => {
            if (event.key === "Escape") {
                if (busyRef.current) return;
                event.preventDefault();
                event.stopImmediatePropagation();
                onClose();
                return;
            }
            if (event.key !== "Tab") return;
            const root = rootRef.current;
            if (!root) return;
            const items = Array.from(root.querySelectorAll<HTMLElement>("input, button, textarea, select, a[href]")).filter((el) => !el.hasAttribute("disabled") && el.tabIndex !== -1);
            if (items.length === 0) {
                event.preventDefault();
                event.stopImmediatePropagation();
                return;
            }
            const first = items[0];
            const last = items[items.length - 1];
            const active = document.activeElement;
            if (event.shiftKey && (active === first || !root.contains(active))) {
                event.preventDefault();
                event.stopImmediatePropagation();
                last.focus();
            } else if (!event.shiftKey && (active === last || !root.contains(active))) {
                event.preventDefault();
                event.stopImmediatePropagation();
                first.focus();
            }
        };
        window.addEventListener("keydown", onKey, true);
        return () => window.removeEventListener("keydown", onKey, true);
    }, [onClose]);
    const choose = (folderID: string) => {
        touchedRef.current = true;
        setSelected(folderID);
    };
    const confirm = async () => {
        if (!ready || failed || busyRef.current || typeof MoveFileCompanionLibraryItem !== "function") return;
        touchedRef.current = true;
        busyRef.current = true;
        setBusy(true);
        setNotice("");
        try {
            await MoveFileCompanionLibraryItem("", path, selected);
            window.dispatchEvent(new Event(companionCatalogChangedEvent));
            onClose();
        } catch (err) {
            setNotice(libraryError(err));
            busyRef.current = false;
            setBusy(false);
        }
    };
    return (
        <div ref={rootRef} className="file-companion-dialog file-companion-place" data-testid="file-companion-place" role="dialog" aria-modal="true" aria-labelledby="file-companion-place-title">
            <div className="file-companion-dialog-scrim" data-testid="file-companion-place-scrim" onMouseDown={() => { if (!busyRef.current) onClose(); }} />
            <form
                className="file-companion-dialog-card"
                onSubmit={(event) => {
                    event.preventDefault();
                    if (!ready || failed || busyRef.current) return;
                    void confirm();
                }}
            >
                <header className="file-companion-dialog-head">
                    <div className="file-companion-dialog-heading">
                        <h2 id="file-companion-place-title">{cs("选择分类")}</h2>
                        <p className="file-companion-dialog-sub">{cs("这篇文档放入哪个分类？")}</p>
                        {fileName ? <p className="file-companion-dialog-doc" data-testid="file-companion-place-name">{fileName}</p> : null}
                    </div>
                </header>
                <div className="file-companion-dialog-body">
                    {ready && !failed ? (
                        <div className="file-companion-dialog-section" role="radiogroup" aria-labelledby="file-companion-place-title">
                            <label className="file-companion-folder-choice">
                                <input type="radio" name="file-companion-place-folder" checked={selected === ""} disabled={busy} onChange={() => choose("")} />
                                {cs("未分类")}
                            </label>
                            {folders.map((folder) => (
                                <label key={folder.id} className="file-companion-folder-choice">
                                    <input
                                        type="radio"
                                        name="file-companion-place-folder"
                                        data-testid={`file-companion-place-${folder.id}`}
                                        checked={selected === folder.id}
                                        disabled={busy}
                                        onChange={() => choose(folder.id)}
                                    />
                                    {folder.name}
                                </label>
                            ))}
                        </div>
                    ) : failed ? null : (
                        <p className="file-companion-library-note" role="status">{cs("正在读取…")}</p>
                    )}
                    {ready && !failed && known ? (
                        <p className="file-companion-library-note">
                            {keptName
                                ? companionFill(getCompanionUiLang(), "取消后这篇文档仍留在「${name}」。", { name: keptName })
                                : cs("取消后这篇文档留在未分类。")}
                        </p>
                    ) : null}
                    {notice ? <p className="file-companion-library-notice" role="status">{notice}</p> : null}
                </div>
                <footer className="file-companion-dialog-foot">
                    <button type="button" className="file-companion-dialog-secondary" data-testid="file-companion-place-cancel" disabled={busy} onClick={onClose}>
                        {cs("取消")}
                    </button>
                    <button type="submit" className="file-companion-dialog-primary" data-testid="file-companion-place-confirm" disabled={!ready || failed || busy}>
                        {cs("确定")}
                    </button>
                </footer>
            </form>
        </div>
    );
}

// CompanionGroupsDialog is the 阅读群组 popup: a group list on the left and
// the selected group's settings on the right. Admins rename, re-icon, invite,
// remove members, or dissolve; a member can leave.
export function CompanionGroupsDialog({
    lang,
    showToast,
    onClose,
}: {
    lang?: string;
    showToast: LibraryToast;
    onClose: () => void;
}) {
    if (lang) setCompanionUiLang(lang);
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
            showToast(cs("已创建群组"), "success", 6000);
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
            showToast(cs("请填写要邀请的邮箱或手机号"), "error", 6000);
            return;
        }
        if (!beginGroupAction()) return;
        try {
            rememberGroup(await AddFileCompanionGroupMember(id, contact));
            setGroupContact((current) => ({ ...current, [id]: "" }));
            showToast(cs("已发送入群邀请"), "success", 6000);
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
            showToast(cs("已移出群组"), "success", 6000);
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
            showToast(cs("已更新群组"), "success", 6000);
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
            showToast(cs("已解散群组"), "success", 6000);
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
            showToast(cs("已退出群组"), "success", 6000);
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
                        <h2 id="file-companion-groups-title">{cs("阅读群组")}</h2>
                        <p className="file-companion-dialog-sub">{cs("用邮箱或手机号邀请，对方在主界面通知里同意后加入。")}</p>
                    </div>
                    <button
                        type="button"
                        className="file-companion-dialog-x"
                        data-testid="file-companion-groups-back"
                        aria-label={cs("关闭")}
                        title={cs("关闭")}
                        onClick={onClose}
                    >
                        <LibraryGlyph name="x" size={13} />
                        <span className="file-companion-sr">{cs("关闭")}</span>
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
                                aria-label={cs("新群组名称")}
                                placeholder={cs("新群组名称")}
                                value={groupName}
                                onChange={(event) => setGroupName(event.target.value)}
                            />
                            <button type="submit" className="file-companion-dialog-primary" data-testid="file-companion-group-create" disabled={groupBusy}>
                                {cs("创建")}
                            </button>
                        </form>
                        {notice ? <p className="file-companion-library-notice" data-testid="file-companion-groups-notice" role="status">{notice}</p> : null}
                        {loading ? <p className="file-companion-library-note">{cs("正在读取…")}</p> : null}
                        <div
                            className="file-companion-groups-list"
                            role="listbox"
                            aria-label={cs("我的群组")}
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
                                    <p>{cs("还没有群组")}</p>
                                    <small>{cs("创建后可以邀请同伴一起读同一份文档。")}</small>
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
                                            <small>{group.admin ? cs("管理员") : (group.owner || cs("成员"))}{group.members.length > 0 ? companionFill(getCompanionUiLang(), " · ${count} 人", { count: group.members.length }) : ""}</small>
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
                                <p>{cs("选择左侧的群组查看设置")}</p>
                            </div>
                        ) : (
                            <>
                                <div className="file-companion-groups-detail-head">
                                    <span className="file-companion-group-mark file-companion-group-mark--lg" data-icon={selected.admin ? selectedEdit?.icon : companionGroupIcon(selected.icon)} aria-hidden="true">
                                        <GroupGlyph icon={selected.admin ? (selectedEdit?.icon ?? selected.icon) : selected.icon} />
                                    </span>
                                    <div className="file-companion-library-group-title">
                                        <strong>{selected.name}</strong>
                                        <small>{selected.admin ? cs("你是管理员") : companionFill(getCompanionUiLang(), "成员 · ${owner}", { owner: selected.owner || cs("群主") })}</small>
                                    </div>
                                </div>

                                {selected.admin ? (
                                    <>
                                        <div className="file-companion-groups-field">
                                            <h3 className="file-companion-dialog-label">{cs("群组图标")}</h3>
                                            <div className="file-companion-groups-icons" role="group" aria-label={cs("群组图标")}>
                                                {companionGroupIcons.map((icon) => (
                                                    <button
                                                        key={icon}
                                                        type="button"
                                                        className="file-companion-group-mark-button"
                                                        data-testid={`file-companion-group-icon-${selected.id}-${icon}`}
                                                        aria-label={cs(companionGroupIconLabel[icon])}
                                                        title={cs(companionGroupIconLabel[icon])}
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
                                            <h3 className="file-companion-dialog-label">{cs("群组名称")}</h3>
                                            <div className="file-companion-library-inline">
                                                <input
                                                    className="file-companion-dialog-input"
                                                    data-testid={`file-companion-group-rename-${selected.id}`}
                                                    aria-label={cs("修改群组名称")}
                                                    value={selectedEdit?.name ?? selected.name}
                                                    disabled={groupBusy}
                                                    onChange={(event) => setGroupEdits((current) => ({
                                                        ...current,
                                                        [selected.id]: { name: event.target.value, icon: current[selected.id]?.icon ?? companionGroupIcon(selected.icon) },
                                                    }))}
                                                />
                                                <button type="button" className="file-companion-dialog-secondary" data-testid={`file-companion-group-save-${selected.id}`} disabled={groupBusy} onClick={() => { void saveGroup(selected.id); }}>{cs("保存")}</button>
                                            </div>
                                        </div>

                                        <div className="file-companion-groups-field">
                                            <h3 className="file-companion-dialog-label">
                                                {cs("成员")}<span className="file-companion-dialog-optional">{companionFill(getCompanionUiLang(), "${count} 人", { count: selected.members.length })}</span>
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
                                                    aria-label={cs("邀请邮箱或手机号")}
                                                    placeholder={cs("邀请的邮箱或手机号")}
                                                    value={groupContact[selected.id] || ""}
                                                    disabled={groupBusy}
                                                    onChange={(event) => setGroupContact((current) => ({ ...current, [selected.id]: event.target.value }))}
                                                />
                                                <button type="submit" className="file-companion-dialog-secondary" data-testid={`file-companion-group-add-${selected.id}`} disabled={groupBusy}>{cs("邀请")}</button>
                                            </form>
                                            {selected.members.length === 0 ? (
                                                <p className="file-companion-dialog-hint">{cs("还没有成员。邀请后对方会收到通知。")}</p>
                                            ) : (
                                                <div className="file-companion-library-members">
                                                    {selected.members.map((member) => (
                                                        <div key={member.contact} className="file-companion-library-member">
                                                            <span className="file-companion-library-member-contact">{member.contact}</span>
                                                            <small className="file-companion-library-member-status" data-ok={member.status === "accepted" ? "true" : undefined}>{member.status === "accepted" ? cs("已加入") : member.status === "ignored" ? cs("已忽略") : cs("待确认")}</small>
                                                            <button
                                                                type="button"
                                                                className="file-companion-library-x"
                                                                data-testid={`file-companion-group-remove-${selected.id}-${member.contact}`}
                                                                aria-label={companionFill(getCompanionUiLang(), "移除 ${name}", { name: member.contact })}
                                                                title={companionFill(getCompanionUiLang(), "移除 ${name}", { name: member.contact })}
                                                                disabled={groupBusy}
                                                                onClick={() => { void removeMember(selected.id, member.contact); }}
                                                            >
                                                                <LibraryGlyph name="x" size={12} />
                                                                <span className="file-companion-sr">{cs("移除")}</span>
                                                            </button>
                                                        </div>
                                                    ))}
                                                </div>
                                            )}
                                        </div>

                                        <div className="file-companion-groups-danger">
                                            <div>
                                                <strong>{cs("解散群组")}</strong>
                                                <small>{cs("成员会失去这份分享的访问权限，文档本身不受影响。")}</small>
                                            </div>
                                            {selectedConfirm === "dissolve" ? (
                                                <div className="file-companion-dialog-actions">
                                                    <button type="button" disabled={groupBusy} onClick={cancelConfirm}>{cs("取消")}</button>
                                                    <button type="button" className="file-companion-dialog-danger" data-testid={`file-companion-group-dissolve-confirm-${selected.id}`} disabled={groupBusy} onClick={() => { void dissolveGroup(selected.id); }}>{cs("确认解散")}</button>
                                                </div>
                                            ) : (
                                                <button type="button" className="file-companion-dialog-danger" data-testid={`file-companion-group-dissolve-${selected.id}`} disabled={groupBusy} onClick={() => setGroupConfirm((current) => ({ ...current, [selected.id]: "dissolve" }))}>{cs("解散群组")}</button>
                                            )}
                                        </div>
                                    </>
                                ) : (
                                    <div className="file-companion-groups-danger">
                                        <div>
                                            <strong>{cs("退出群组")}</strong>
                                            <small>{cs("退出后不再看到这个群组分享的文档。")}</small>
                                        </div>
                                        {selectedConfirm === "leave" ? (
                                            <div className="file-companion-dialog-actions">
                                                <button type="button" disabled={groupBusy} onClick={cancelConfirm}>{cs("取消")}</button>
                                                <button type="button" className="file-companion-dialog-danger" data-testid={`file-companion-group-leave-confirm-${selected.id}`} disabled={groupBusy} onClick={() => { void leaveGroup(selected.id); }}>{cs("确认退出")}</button>
                                            </div>
                                        ) : (
                                            <button type="button" className="file-companion-dialog-danger" data-testid={`file-companion-group-leave-${selected.id}`} disabled={groupBusy} onClick={() => setGroupConfirm((current) => ({ ...current, [selected.id]: "leave" }))}>{cs("退出群组")}</button>
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
    lang,
    path,
    name,
    share,
    showToast,
    onListed,
    libraryDeleteGen,
    onClose,
}: {
    lang?: string;
    path: string;
    name: string;
    // share is the existing cloud row this dialog edits. Absent means 分享.
    share?: CompanionShareRow | null;
    showToast: LibraryToast;
    onListed?: (listed: unknown) => void;
    libraryDeleteGen?: { current: number };
    onClose: () => void;
}) {
    if (lang) setCompanionUiLang(lang);
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
    const marksJSON = companionProjectMarksJSON(path);
    const highlightsCount = marks.length;

    const shareCurrent = async () => {
        // 修改 works from the share alone: a row this account created on
        // another machine has no local copy here, and it is still editable.
        if ((!editing && !path) || shareBusy) return;
        // An edit that did not touch 有效期 sends -1, which the backend reads
        // as "keep the expiry already on the link".
        const days = editing && !expiryTouched ? -1 : shareDays(span, amount);
        if (days === null) {
            showToast(cs("分享时间不正确"), "error", 6000);
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
                showToast(cs("请设置分享密码"), "error", 6000);
                return;
            }
            if (n < 4 || n > 64) {
                showToast(cs("分享密码需为 4–64 个字符"), "error", 6000);
                return;
            }
        }
        const target = visibility === "person" ? personTarget.trim() : visibility === "group" ? groupID : "";
        if (visibility === "person" && target === "") {
            showToast(cs("请填写邮箱或手机号"), "error", 6000);
            return;
        }
        if (visibility === "group" && target === "") {
            showToast(cs("请选择群组"), "error", 6000);
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
            showToast(editing ? cs("已更新分享") : cs("分享成功，链接已生成"), "success", 6000);
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
            showToast(cs("已同步到云端"), "success", 6000);
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
        const docName = name || share?.name || cs("当前文档");
        const ok = await showConfirm(
            companionFill(getCompanionUiLang(), "确认删除“${name}”的分享？\n链接会立即失效，其他人将无法再打开。\n没有其他分享时，这份文档会回到本机项目，高亮和阅读记录会保留。\n在本机项目里删除才会清除这些数据。", { name: docName }),
            cs("删除分享"),
            { confirmText: cs("删除"), cancelText: cs("取消"), confirmVariant: "danger" },
        );
        if (!ok) return;
        setShareBusy(true);
        try {
            const raw = await DeleteFileCompanionShare(token) as Record<string, unknown>;
            const rev = numberOf(raw, "libraryRev", "LibraryRev");
            const restoredLocal = companionRestoredLocalItem(raw, docName);
            companionLibraryHideRow({ id: token, source: "cloud", token }, rev, 0);
            if (restoredLocal) companionLibraryShowRestored(restoredLocal, rev, 0);
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
        showToast(cs("已删除分享"), "success", 6000);
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
            setHostError(cs("请填写参与者邮箱或手机号"));
            return;
        }
        setHostBusy(true);
        setHostError("");
        try {
            await TransferFileCompanionShareHost(token, contact);
            setPendingHost(contact);
            setHostContact("");
            showToast(cs("已发送主持人转移邀请，等待对方接受"), "success", 6000);
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
                ? companionFill(getCompanionUiLang(), "确认取消提名 ${name} 接任主持人？", { name: pendingHost })
                : cs("确认收回主持权，由你重新担任主持人？"),
            pendingHost ? cs("取消转移") : cs("收回主持权"),
            { confirmText: cs("确认"), cancelText: cs("取消"), confirmVariant: "danger" },
        );
        if (!ok) return;
        const cancelling = Boolean(pendingHost);
        setHostBusy(true);
        setHostError("");
        try {
            await RevokeFileCompanionShareHost(token, cancelling ? "cancel" : "revoke");
            if (!cancelling) setHostIsMe(true);
            setPendingHost("");
            showToast(cancelling ? cs("已取消主持人转移") : cs("已收回主持权"), "success", 6000);
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
                        <h2 id="file-companion-share-dialog-title">{editing ? cs("修改分享") : cs("分享到云端")}</h2>
                        <p className="file-companion-dialog-sub">
                            <span className="file-companion-dialog-doc">{name || share?.name || cs("当前文档")}</span>
                            {editing ? <span className="file-companion-dialog-dot">{echoExpiresAt ? companionFill(getCompanionUiLang(), "当前 ${audience} · 至 ${date}", { audience: audienceLabel(echoVisibility), date: echoExpiresAt.slice(0, 10) }) : companionFill(getCompanionUiLang(), "当前 ${audience} · 永久", { audience: audienceLabel(echoVisibility) })}</span> : null}
                            {!editing && highlightsCount > 0 ? <span className="file-companion-dialog-dot">{companionFill(getCompanionUiLang(), "标注 ${count}", { count: highlightsCount })}</span> : null}
                        </p>
                    </div>
                    <button
                        type="button"
                        className="file-companion-dialog-x"
                        data-testid="file-companion-share-dialog-close"
                        aria-label={cs("关闭")}
                        title={cs("关闭")}
                        onClick={onClose}
                    >
                        <LibraryGlyph name="x" size={13} />
                        <span className="file-companion-sr">{cs("关闭")}</span>
                    </button>
                </header>

                <div className="file-companion-dialog-body">
                    <section className="file-companion-dialog-section">
                        <h3 className="file-companion-dialog-label">{cs("可见范围")}</h3>
                        <div className="file-companion-audience" role="group" aria-label={cs("可见范围")}>
                            <button type="button" className="file-companion-audience-option" data-testid="file-companion-share-public" aria-pressed={visibility === "public"} onClick={() => setVisibility("public")}>
                                <LibraryGlyph name="globe" size={15} />
                                <span>{cs("完全公开")}</span>
                            </button>
                            <button type="button" className="file-companion-audience-option" data-testid="file-companion-share-private" aria-pressed={visibility === "private"} onClick={() => setVisibility("private")}>
                                <LibraryGlyph name="lock" size={15} />
                                <span>{cs("私密分享")}</span>
                            </button>
                            <button type="button" className="file-companion-audience-option" data-testid="file-companion-share-person" aria-pressed={visibility === "person"} onClick={() => setVisibility("person")}>
                                <LibraryGlyph name="person" size={15} />
                                <span>{cs("指定的人")}</span>
                            </button>
                            <button type="button" className="file-companion-audience-option" data-testid="file-companion-share-group" aria-pressed={visibility === "group"} onClick={() => setVisibility("group")}>
                                <LibraryGlyph name="people" size={15} />
                                <span>{cs("群组")}</span>
                            </button>
                        </div>
                        {visibility === "private" ? (
                            <input
                                className="file-companion-dialog-input"
                                data-testid="file-companion-share-password"
                                type="password"
                                aria-label={cs("分享密码")}
                                placeholder={editing ? cs("留空则保持原密码") : cs("设置密码，4–64 个字符")}
                                value={password}
                                onChange={(event) => setPassword(event.target.value)}
                            />
                        ) : null}
                        {visibility === "person" ? (
                            <input
                                className="file-companion-dialog-input"
                                data-testid="file-companion-share-target"
                                aria-label={cs("邮箱或手机号")}
                                placeholder={cs("对方的邮箱或手机号")}
                                value={personTarget}
                                onChange={(event) => setPersonTarget(event.target.value)}
                            />
                        ) : null}
                        {visibility === "group" ? (
                            groups.some((group) => group.admin) ? (
                                <select
                                    className="file-companion-dialog-input"
                                    data-testid="file-companion-share-group-pick"
                                    aria-label={cs("选择群组")}
                                    value={groupID}
                                    onChange={(event) => setGroupID(event.target.value)}
                                >
                                    <option value="">{cs("选择群组")}</option>
                                    {groups.filter((group) => group.admin).map((group) => (
                                        <option key={group.id} value={group.id}>{group.name}</option>
                                    ))}
                                </select>
                            ) : <p className="file-companion-dialog-hint">{cs("还没有管理的群组，先在「阅读群组」里创建一个。")}</p>
                        ) : null}
                    </section>

                    <section className="file-companion-dialog-section">
                        <h3 className="file-companion-dialog-label">{cs("有效期")}</h3>
                        <div className="file-companion-dialog-row">
                            <div className="file-companion-segment" role="group" aria-label={cs("有效期")}>
                                <button type="button" data-testid="file-companion-share-forever" aria-pressed={span === "forever"} onClick={() => { setSpan("forever"); setExpiryTouched(true); }}>{cs("永久")}</button>
                                <button type="button" data-testid="file-companion-share-days" aria-pressed={span === "days"} onClick={() => { setSpan("days"); setExpiryTouched(true); }}>{cs("几天")}</button>
                                <button type="button" data-testid="file-companion-share-months" aria-pressed={span === "months"} onClick={() => { setSpan("months"); setExpiryTouched(true); }}>{cs("几月")}</button>
                            </div>
                            {span !== "forever" ? (
                                <label className="file-companion-dialog-amount">
                                    <input
                                        className="file-companion-dialog-input"
                                        data-testid="file-companion-share-amount"
                                        type="number"
                                        min={1}
                                        max={span === "months" ? 122 : 3660}
                                        aria-label={span === "months" ? cs("分享月数") : cs("分享天数")}
                                        value={amount}
                                        onChange={(event) => { setAmount(event.target.value); setExpiryTouched(true); }}
                                    />
                                    <span>{span === "months" ? cs("个月") : cs("天")}</span>
                                </label>
                            ) : null}
                        </div>
                    </section>

                    {editing && share?.admin ? (
                        <section className="file-companion-dialog-section" data-testid="file-companion-share-host">
                            <h3 className="file-companion-dialog-label">{cs("主持人")}</h3>
                            <p className="file-companion-dialog-hint" data-testid="file-companion-share-host-current">
                                {hostLabel ? companionFill(getCompanionUiLang(), "当前主持人：${host}", { host: hostLabel }) : cs("当前主持人：你")}
                            </p>
                            {pendingHost ? (
                                <div className="file-companion-dialog-row">
                                    <p className="file-companion-dialog-hint" data-testid="file-companion-share-host-pending">
                                        {companionFill(getCompanionUiLang(), "已提名 ${name}，等待对方在通知里接受。", { name: pendingHost })}
                                    </p>
                                    <button type="button" className="file-companion-dialog-secondary" data-testid="file-companion-share-host-cancel" disabled={hostBusy} onClick={() => { void revokeHost(); }}>
                                        {cs("取消转移")}
                                    </button>
                                </div>
                            ) : (
                                <div className="file-companion-dialog-row">
                                    <input
                                        className="file-companion-dialog-input"
                                        data-testid="file-companion-share-host-target"
                                        aria-label={cs("新主持人的邮箱或手机号")}
                                        placeholder={cs("参与者的邮箱或手机号")}
                                        value={hostContact}
                                        onChange={(event) => setHostContact(event.target.value)}
                                    />
                                    <button type="button" className="file-companion-dialog-secondary" data-testid="file-companion-share-host-transfer" disabled={hostBusy} onClick={() => { void transferHost(); }}>
                                        {cs("转移主持权")}
                                    </button>
                                    {!hostIsMe ? (
                                        <button type="button" className="file-companion-dialog-danger" data-testid="file-companion-share-host-revoke" disabled={hostBusy} onClick={() => { void revokeHost(); }}>
                                            {cs("收回主持权")}
                                        </button>
                                    ) : null}
                                </div>
                            )}
                            <p className="file-companion-dialog-hint">{cs("对方接受后才转移；你保留管理权，可随时收回或另行指定。")}</p>
                            {hostError ? (
                                <p className="file-companion-dialog-error" data-testid="file-companion-share-host-error" role="alert">{hostError}</p>
                            ) : null}
                        </section>
                    ) : null}

                    {shareURL ? (
                        <section className="file-companion-dialog-section file-companion-dialog-result" data-testid="file-companion-share-result">
                            <h3 className="file-companion-dialog-label">{editing ? cs("分享链接") : cs("链接已生成")}</h3>
                            <div className="file-companion-dialog-linkrow">
                                <p className="file-companion-dialog-link" data-testid="file-companion-library-link">{shareURL}</p>
                                <button type="button" className="file-companion-dialog-copy" data-testid="file-companion-library-copy" aria-label={cs("复制分享链接")} title={cs("复制分享链接")} onClick={() => { void copyText(shareURL, showToast); }}>
                                    <LibraryGlyph name="copy" size={13} />
                                    <span className="file-companion-sr">{cs("复制")}</span>
                                </button>
                            </div>
                        </section>
                    ) : null}
                </div>

                <footer className="file-companion-dialog-foot">
                    <p className="file-companion-dialog-hint">
                        {editing ? cs("改范围或有效期后，链接不变，已导入的人继续可用。") : cs("分享包含文档、高亮和聊天，之后可同步新标注。")}
                    </p>
                    <div className="file-companion-dialog-actions">
                        {editing ? (
                            <button type="button" className="file-companion-dialog-danger" data-testid="file-companion-share-delete" disabled={shareBusy} onClick={() => { void deleteCurrent(); }}>
                                {cs("删除分享")}
                            </button>
                        ) : (
                            <button type="button" className="file-companion-dialog-secondary" data-testid="file-companion-library-sync" disabled={shareBusy} onClick={() => { void syncCurrent(); }}>
                                {cs("同步更新")}
                            </button>
                        )}
                        <button type="button" className="file-companion-dialog-primary" data-testid="file-companion-library-share" disabled={shareBusy} aria-busy={shareBusy} onClick={() => { void shareCurrent(); }}>
                            {shareBusy ? (editing ? cs("正在保存…") : cs("正在分享…")) : (editing ? cs("保存修改") : cs("分享到云端"))}
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
    lang,
    initialLink,
    showToast,
    onOpen,
    onShareUpdated,
    onListed,
    libraryDeleteGen,
    onClose,
}: {
    lang?: string;
    initialLink: string;
    showToast: LibraryToast;
    onOpen: (path: string) => void;
    onShareUpdated?: (update: CompanionShareUpdate) => void;
    onListed?: (listed: unknown) => void;
    libraryDeleteGen?: { current: number };
    onClose: () => void;
}) {
    if (lang) setCompanionUiLang(lang);
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
            showToast(cs("请粘贴分享链接"), "error", 6000);
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
                    chatAnnotations: imported.chatAnnotations ?? "",
                    chatAnnotationsKnown: Boolean(imported.chatAnnotations),
                    annotations: imported.annotations ?? "",
                    annotationsKnown: Boolean(imported.annotations),
                    drawings: imported.drawings ?? "",
                    drawingsKnown: Boolean(imported.drawings),
                    bookmarks: imported.bookmarks ?? "",
                    bookmarksKnown: Boolean(imported.bookmarks),
                    messages: [],
                    project: true,
                    replaceMessages: false,
                });
            }
            showToast(cs("已导入文档库"), "success", 6000);
            if (imported?.path) onOpen(imported.path);
            // The file is already on disk. A library refresh also re-reads every
            // other share, and a link from another Hub waits on that Hub again.
            // That must not keep 正在导入 up; the next poll fills the row in.
            const started = libraryDeleteGen?.current ?? 0;
            void ListFileCompanionLibrary().then((listed) => {
                if (!libraryDeleteGen || libraryDeleteGen.current === started) listedRef.current?.(listed);
            }).catch(() => undefined);
        } catch (err) {
            showToast(libraryError(err), "error", 6000);
            return;
        } finally {
            setImportBusy(false);
        }
        onClose();
    };

    const cardRef = useDialogFocus<HTMLDivElement>("input");

    return (
        <div className="file-companion-dialog" data-testid="file-companion-import-dialog" role="dialog" aria-modal="true" aria-labelledby="file-companion-import-dialog-title">
            <div className="file-companion-dialog-scrim" data-testid="file-companion-import-dialog-scrim" onMouseDown={onClose} />
            <div className="file-companion-dialog-card file-companion-share" ref={cardRef}>
                <header className="file-companion-dialog-head">
                    <div className="file-companion-dialog-heading">
                        <h2 id="file-companion-import-dialog-title">{cs("导入分享")}</h2>
                        <p className="file-companion-dialog-sub">{cs("导入后文档、高亮和聊天一起存进本机，成为一个可继续阅读的项目。")}</p>
                    </div>
                    <button
                        type="button"
                        className="file-companion-dialog-x"
                        data-testid="file-companion-import-dialog-close"
                        aria-label={cs("关闭")}
                        title={cs("关闭")}
                        onClick={onClose}
                    >
                        <LibraryGlyph name="x" size={13} />
                        <span className="file-companion-sr">{cs("关闭")}</span>
                    </button>
                </header>
                <div className="file-companion-dialog-body">
                    <section className="file-companion-dialog-section">
                        <h3 className="file-companion-dialog-label">{cs("分享链接")}</h3>
                        <input
                            className="file-companion-dialog-input"
                            data-testid="file-companion-library-import-link"
                            aria-label={cs("分享链接")}
                            placeholder={cs("粘贴分享链接")}
                            value={importLink}
                            onChange={(event) => setImportLink(event.target.value)}
                        />
                    </section>
                    <section className="file-companion-dialog-section">
                        <h3 className="file-companion-dialog-label">{cs("密码")}<span className="file-companion-dialog-optional">{cs("可选")}</span></h3>
                        <input
                            className="file-companion-dialog-input"
                            data-testid="file-companion-library-import-password"
                            type="password"
                            aria-label={cs("导入密码")}
                            placeholder={cs("私密分享才需要密码")}
                            value={importPassword}
                            onChange={(event) => setImportPassword(event.target.value)}
                        />
                    </section>
                </div>
                <footer className="file-companion-dialog-foot">
                    <div className="file-companion-dialog-actions">
                        <button type="button" className="file-companion-dialog-secondary" data-testid="file-companion-import-dialog-cancel" onClick={onClose}>
                            {cs("取消")}
                        </button>
                        <button type="button" className="file-companion-dialog-primary" data-testid="file-companion-library-import" disabled={importBusy} aria-busy={importBusy} onClick={() => { void runImport(importLink, importPassword); }}>
                            {importBusy ? cs("正在导入…") : cs("导入到文档库")}
                        </button>
                    </div>
                </footer>
            </div>
        </div>
    );
}
