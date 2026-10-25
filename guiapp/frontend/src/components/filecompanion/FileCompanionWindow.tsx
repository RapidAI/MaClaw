import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import {
    ChooseFileCompanionFiles,
    CancelFileCompanionTurn,
    CancelFileCompanionWebTurn,
    StopFileCompanionWebNote,
    FileCompanionDiscardWebNote,
    FileCompanionWebPlayerLeft,
    ClearFileCompanionChat,
    ClearFileCompanionWebChat,
    FileCompanionChooseConvertDestination,
    FileCompanionConvertScan,
    FileCompanionExportChatCloud,
    FileCompanionExportChatPDF,
    FileCompanionExportChatTeX,
    FileCompanionExportPDF,
    FileCompanionExportProject,
    FileCompanionExportTeX,
    FileCompanionExportWebChatCloud,
    FileCompanionExportWebChatPDF,
    FileCompanionExportWebChatTeX,
    FileCompanionExportWebPDF,
    FileCompanionExportWebTeX,
    FileCompanionFigureDataURL,
    FileCompanionImportKnowledge,
    FileCompanionOpen,
    FileCompanionOpenArxiv,
    FileCompanionSearchArxiv,
    FileCompanionOpenWebPage,
    ImportFileCompanionShare,
    FileCompanionPDFScan,
    FileCompanionPlayerHide,
    FileCompanionPlayerPlace,
    FileCompanionPlayerSupported,
    FileCompanionReplaceOffice,
    FileCompanionResumePaths,
    FileCompanionSaveText,
    FileCompanionUIReady,
    FileCompanionUploadCloud,
    ShowItemInFolder,
    GetBrandInfo,
    GetFileCompanionBoot,
    ListFileCompanionLibrary,
    SetFileCompanionOpenSet,
    SetMainWindowScreenPosition,
    AddFileCompanionShareNote,
    SendFileCompanionMessage,
    SendFileCompanionWebMessage,
} from "../../../wailsjs/go/main/App";
import { WindowFullscreen, WindowGetPosition, WindowGetSize, WindowHide, WindowSetSize, WindowUnfullscreen } from "../../../wailsjs/runtime";
import { createWindowMaximizeRestoreSession } from "../../utils/windowRestoreGeometry";
import { useToast } from "../Toast";
import { companionLibrarySync, companionNotePost, companionNotesRevisionWins, companionShareNotes, companionSharePathKey, CompanionGroupsDialog, CompanionImportDialog, CompanionLibraryView, CompanionShareDialog, type CompanionShareNote, type CompanionShareUpdate } from "./FileCompanionLibrary";
import { AssistantReasoningPanel } from "../ai/AssistantReasoningPanel";
import { copyTextToClipboard } from "../ai/AssistantReplyCopyButton";
import { MarkdownPreview } from "../ai/CodePreviewMarkdown";
import { MessageContentRenderer } from "../ai/MessageContentRenderer";
import { reasoningTrailMarkdownOptions, renderContentWithCodeBlocks, renderInlineMarkdown } from "../ai/aiAssistantMarkdown";
import { lightTheme } from "../ai/aiAssistantPanelTheme";
import type { Theme as AssistantTheme } from "../ai/aiAssistantPanelTheme";
import { isMermaidCodeFence, renderMermaidFenceToPNGDataURL } from "../ai/AssistantMermaidDiagram";
import { isHistoryResetCommandText } from "../ai/composeAction";
import { cleanReasoningTrailForBody, resolveVisibleAssistantReply } from "../ai/assistantReasoningBody";
import { stripAssistantToolCallMarkers } from "../ai/assistantToolCall";
import { useAssistantOutputScroll } from "../ai/useAssistantOutputScroll";
import { CompanionStatus } from "./CompanionNoteStatus";
import { noteClockFor, noteMediaFromKind, type NoteMedia } from "./noteStatusModel";
import { FileCompanionComposer } from "./FileCompanionComposer";
import { CompanionFigureStrip, type CompanionFigure } from "./CompanionFigureStrip";
import { companionVisibleText, finishCompanionReply, splitCompanionStreamDelta } from "./companionReasoning";
import { PaperResultCard } from "./PaperResultCard";
import { NoteResultCard } from "./NoteResultCard";
import { FILE_COMPANION_OPEN_HINT, companionVideoNotePhrase, looksLikeArxivReference, looksLikeCompanionShare, noteStampAtLineStart, type NoteStampLine } from "./companionNoteSeek";
import { windowDragHandleProps } from "../../utils/windowDrag";
import type { CodePreviewTheme } from "../ai/FileTabBar";
import { loadPdfMarks, normalizePdfMarks, savePdfMarks, type PdfMark } from "./pdfMarks";
import { FilePreviewView } from "../preview/FilePreviewView";
import { filePreviewKindFromName, type FilePreviewKind } from "../preview/filePreviewKind";

const CLOUD_DISABLE_BYTES = 400 * 1024 * 1024;
// Asked once when a new document has no transcript. A selection is not sent:
// the host would write the reply back into the file.
const FILE_COMPANION_INTRO = "这篇文档是什么？";
// A cleared chat bumps the session generation. Events bound to an older
// generation, and any unbound event after the first clear, stay off the log.
function companionEventCurrent(epochs: Map<string, number>, requests: Map<string, number>, sessionId: string, requestID: string): boolean {
    const current = epochs.get(sessionId) ?? 0;
    const bound = requests.get(requestID);
    if (bound === undefined) return current === 0;
    return bound === current;
}

const COMPANION_CHAT_MIN = 280;
const COMPANION_PREVIEW_MIN = 240;
const COMPANION_SPLIT_HANDLE = 12;

export function clampCompanionChatWidth(next: number, total: number): number {
    const roomForPreview = total - COMPANION_PREVIEW_MIN - COMPANION_SPLIT_HANDLE;
    const max = roomForPreview >= COMPANION_CHAT_MIN ? roomForPreview : COMPANION_CHAT_MIN;
    return Math.min(max, Math.max(COMPANION_CHAT_MIN, Math.round(next)));
}

/** Move only the filename row. scrollIntoView also scrolls the title bar and the page. */
export function revealCompanionTab(
    scrollport: Pick<HTMLElement, "scrollLeft" | "getBoundingClientRect">,
    tab: Pick<HTMLElement, "getBoundingClientRect">,
) {
    const port = scrollport.getBoundingClientRect();
    const item = tab.getBoundingClientRect();
    if (item.left < port.left) {
        scrollport.scrollLeft -= port.left - item.left;
    } else if (item.right > port.right) {
        scrollport.scrollLeft += item.right - port.right;
    }
}

const COMPANION_TAB_EDGE = 1;
const COMPANION_TAB_NUDGE_SLIVER = 24;

export type CompanionTabBox = { left: number; right: number };

// The companion window's screens: the document reader, the 文档管理 screen,
// and the 阅读群组 screen. 分享 overlays as a dialog on any of them.
export type CompanionView = "read" | "library" | "groups";

export type CompanionTabEnds = {
    overflow: boolean;
    canLeft: boolean;
    canRight: boolean;
};

export function companionTabScrollEnds(scrollLeft: number, scrollWidth: number, clientWidth: number): CompanionTabEnds {
    const maxScroll = Math.max(0, scrollWidth - clientWidth);
    const overflow = maxScroll > 1;
    return {
        overflow,
        canLeft: overflow && scrollLeft > 1,
        canRight: overflow && scrollLeft < maxScroll - 1,
    };
}

export function clampCompanionTabScroll(scrollLeft: number, maxScroll: number): number {
    const limit = Math.max(0, maxScroll);
    if (scrollLeft < 0) return 0;
    if (scrollLeft > limit) return limit;
    return scrollLeft;
}

/** Show an end button only when that click would move a filename. A few spare scroll pixels with every name already visible stay button-free. */
export function companionTabEndsFromBoxes(
    scrollLeft: number,
    maxScroll: number,
    port: CompanionTabBox,
    tabs: CompanionTabBox[],
): CompanionTabEnds {
    const limit = Math.max(0, maxScroll);
    const canLeft = limit > 1 && scrollLeft > 1 && companionTabNudgeDelta(port, tabs, -1) > 0;
    const canRight = limit > 1 && scrollLeft < limit - 1 && companionTabNudgeDelta(port, tabs, 1) > 0;
    return {
        overflow: canLeft || canRight,
        canLeft,
        canRight,
    };
}

function companionTabBoxes(node: HTMLElement): CompanionTabBox[] {
    return [...node.querySelectorAll<HTMLElement>('[role="tab"]')].map((tab) => {
        const rect = tab.getBoundingClientRect();
        return { left: rect.left, right: rect.right };
    });
}

function readCompanionTabEnds(node: HTMLElement): CompanionTabEnds {
    const maxScroll = Math.max(0, node.scrollWidth - node.clientWidth);
    const port = node.getBoundingClientRect();
    const boxes = companionTabBoxes(node);
    const widthKnown = port.right > port.left + 1 && boxes.some((box) => box.right > box.left + 1);
    if (!widthKnown) return companionTabScrollEnds(node.scrollLeft, node.scrollWidth, node.clientWidth);
    return companionTabEndsFromBoxes(node.scrollLeft, maxScroll, { left: port.left, right: port.right }, boxes);
}

function assignTabEnds(current: CompanionTabEnds, next: CompanionTabEnds): CompanionTabEnds {
    if (current.overflow === next.overflow && current.canLeft === next.canLeft && current.canRight === next.canRight) return current;
    return next;
}

/** Distance that brings the next clipped filename fully into the row. A sliver jumps one more tab so the click shows a new name. */
export function companionTabNudgeDelta(port: CompanionTabBox, tabs: CompanionTabBox[], direction: -1 | 1): number {
    if (direction > 0) {
        const index = tabs.findIndex((tab) => tab.right > port.right + COMPANION_TAB_EDGE);
        if (index < 0) return 0;
        const item = tabs[index];
        const sliver = item.left >= port.left - COMPANION_TAB_EDGE && item.right - port.right < COMPANION_TAB_NUDGE_SLIVER;
        const target = sliver && index + 1 < tabs.length ? tabs[index + 1] : item;
        return Math.max(0, target.right - port.right);
    }
    let index = -1;
    for (let i = tabs.length - 1; i >= 0; i -= 1) {
        if (tabs[i].left < port.left - COMPANION_TAB_EDGE) {
            index = i;
            break;
        }
    }
    if (index < 0) return 0;
    const item = tabs[index];
    const sliver = item.right <= port.right + COMPANION_TAB_EDGE && port.left - item.left < COMPANION_TAB_NUDGE_SLIVER;
    const target = sliver && index > 0 ? tabs[index - 1] : item;
    return Math.max(0, port.left - target.left);
}

const companionPreviewTheme: CodePreviewTheme = {
    bg: "#ffffff",
    text: "#1e293b",
    textMuted: "#64748b",
    border: "#e2e8f0",
    lineNumBg: "#f8fafc",
    lineNumText: "#94a3b8",
    tabBg: "#f1f5f9",
    tabActiveBg: "#ffffff",
    tabActiveText: "#0f172a",
    tabHoverBg: "#e2e8f0",
    diffAddBg: "#dcfce7",
    diffAddText: "#166534",
    diffDeleteBg: "#fee2e2",
    diffDeleteText: "#991b1b",
    syntaxKeyword: "#7c3aed",
    syntaxString: "#047857",
    syntaxComment: "#94a3b8",
    syntaxNumber: "#b45309",
    syntaxFunction: "#1d4ed8",
    syntaxType: "#0f766e",
    syntaxOperator: "#334155",
};

type CompanionDoc = {
    path?: string;
    Path?: string;
    name?: string;
    Name?: string;
    editable?: boolean;
    Editable?: boolean;
    readOnly?: boolean;
    ReadOnly?: boolean;
    canExport?: boolean;
    CanExport?: boolean;
    content?: string;
    Content?: string;
    loadedHash?: string;
    LoadedHash?: string;
    sessionId?: string;
    SessionID?: string;
    size?: number;
    Size?: number;
    error?: string;
    Error?: string;
    readOnlyReason?: string;
    ReadOnlyReason?: string;
    messages?: CompanionMessageDoc[];
    Messages?: CompanionMessageDoc[];
    highlights?: unknown;
    Highlights?: unknown;
    imported?: boolean;
    Imported?: boolean;
};

type CompanionMessageDoc = { role?: string; Role?: string; text?: string; Text?: string; reasoning?: string; Reasoning?: string; resultPath?: string; ResultPath?: string; noteCardPath?: string; NoteCardPath?: string; webNote?: boolean; WebNote?: boolean; figures?: unknown[]; Figures?: unknown[] };

type SaveResult = {
    saved?: boolean;
    Saved?: boolean;
    conflict?: boolean;
    Conflict?: boolean;
    diskHash?: string;
    DiskHash?: string;
    error?: string;
    Error?: string;
};

// Mirrors guiapp.CompanionChatTurn. Declared here rather than imported from
// models.ts so this component owns the shape the Go side accepts: a role and
// the Markdown body, nothing else.
type CompanionChatTurn = { role: "user" | "assistant"; content: string };

type ChatMessage = { id: string; role: "user" | "assistant"; text: string; reasoning?: string; resultPath?: string; noteCardPath?: string; webNote?: boolean; figures?: CompanionFigure[] };
// Stable identity keeps the scroll-follow effect from re-planning on every
// render while no file is open.
const NO_COMPANION_MESSAGES: ChatMessage[] = [];
// One stable empty result for an idle window, so an export that has nothing to
// build does not hand a new object to every effect that depends on it.
const EMPTY_CHAT_EXPORT: CompanionChatExport = { turns: [], missingFigures: 0, missingDiagrams: 0 };

// Figures come from Go with lower-case JSON tags and from tests and restored
// history as plain objects, so both spellings of each field are read.
function figureFromValue(value: unknown): CompanionFigure | null {
    if (!value || typeof value !== "object") return null;
    const item = value as { label?: unknown; Label?: unknown; caption?: unknown; Caption?: unknown; path?: unknown; Path?: unknown };
    const path = String(item.path ?? item.Path ?? "").trim();
    if (!path) return null;
    const label = String(item.label ?? item.Label ?? "").trim();
    const caption = String(item.caption ?? item.Caption ?? "").trim();
    return { label, caption, path };
}

function figuresFromValue(raw: unknown): CompanionFigure[] {
    const list = Array.isArray(raw) ? raw : [];
    const out: CompanionFigure[] = [];
    list.forEach((entry) => {
        const figure = figureFromValue(entry);
        if (figure) out.push(figure);
    });
    return out;
}

const assistantMark = ":assistant";

function paperRequestOf(messageID: string): string {
    return messageID.endsWith(assistantMark) ? messageID.slice(0, -assistantMark.length) : "";
}

type CompanionTab = {
    path: string;
    name: string;
    editable: boolean;
    readOnly: boolean;
    canExport: boolean;
    content: string;
    loadedHash: string;
    sessionId: string;
    size: number;
    error: string;
    draft: string;
    selection: string;
    selectionStart: number;
    messages: ChatMessage[];
    readOnlyReason: string;
    status: string;
    noteStartedAt?: number;
    phaseStartedAt?: number;
    dirty: boolean;
    conflict: boolean;
    diskHash: string;
    confirmOverwrite: boolean;
    previewEpoch: number;
    shareToken?: string;
    shareMine?: boolean;
    notes?: CompanionShareNote[];
    saveError: string;
    htmlPreview: boolean;
    kind?: "web";
    frameUrl?: string;
};

function tabNoteKind(tab: { kind?: "web"; name: string }): NoteMedia {
    return tab.kind === "web" ? "web" : noteMediaFromKind(filePreviewKindFromName(tab.name), tab.name);
}

type RuntimeAPI = {
    EventsOn?: (name: string, cb: (payload: unknown) => void) => void;
    EventsOff?: (name: string) => void;
    OnFileDrop?: (cb: (x: number, y: number, paths: string[]) => void, useDropTarget?: boolean) => void;
    OnFileDropOff?: () => void;
};

function runtimeAPI(): RuntimeAPI | undefined {
    return (window as unknown as { runtime?: RuntimeAPI }).runtime;
}

type ConvertDoneBody = {
    path: string;
    ok: boolean;
    scan: boolean;
    complete: boolean;
    pending: boolean;
    pdf: string;
    message: string;
    saveFailed: boolean;
};

function readConvertDone(payload: unknown): ConvertDoneBody | null {
    if (!payload || typeof payload !== "object") return null;
    const body = payload as Record<string, unknown>;
    const path = String(body.path ?? body.Path ?? "").trim();
    if (!path) return null;
    return {
        path,
        ok: (body.ok ?? body.OK) === true,
        scan: (body.scan ?? body.Scan) !== false,
        complete: (body.complete ?? body.Complete) === true,
        pending: (body.pending ?? body.Pending) === true,
        pdf: String(body.pdf ?? body.Pdf ?? "").trim(),
        message: String(body.message ?? body.Message ?? "").trim(),
        saveFailed: (body.save_failed ?? body.SaveFailed) === true,
    };
}

function withConvertFile(path: string, text: string): string {
    const name = path.split(/[/\\]/).pop() || "";
    if (!name) return text;
    const lead = `「${name}」`;
    return text.startsWith(lead) ? text : lead + text;
}

function field<T>(doc: CompanionDoc, lower: keyof CompanionDoc, upper: keyof CompanionDoc, fallback: T): T {
    const value = doc[lower] ?? doc[upper];
    return (value === undefined || value === null ? fallback : value) as T;
}

function companionDocumentPaths(raw: unknown): string[] {
    if (!Array.isArray(raw)) return [];
    return raw.filter((item): item is string => typeof item === "string" && item.trim() !== "");
}

function bootPaths(boot: { paths?: unknown; Paths?: unknown } | null | undefined): string[] {
    return companionDocumentPaths(boot?.paths ?? boot?.Paths);
}

function companionTabStaysInOpenSet(tab: { path: string; error?: string }): boolean {
    if (!tab.path) return false;
    switch (tab.error) {
        case "file not found":
        case "directories are not opened as tabs":
        case "empty path":
        case "path contains NUL":
            return false;
        default:
            return true;
    }
}

export function companionOpenSetPaths(tabs: { path: string; error?: string }[], active: string): string[] {
    const paths: string[] = [];
    const activeTab = tabs.find((tab) => tab.path === active && companionTabStaysInOpenSet(tab));
    if (activeTab?.path) paths.push(activeTab.path);
    for (const tab of tabs) {
        if (!companionTabStaysInOpenSet(tab) || paths.includes(tab.path)) continue;
        paths.push(tab.path);
    }
    return paths;
}

function isDirectoryError(error: string): boolean {
    return /director/i.test(error);
}

function fileErrorText(error: string): string {
    switch (error) {
        case "file is not readable":
        case "path is not readable":
            return "没有读取权限";
        case "file not found":
            return "找不到文件";
        case "directories are not opened as tabs":
            return "不支持打开文件夹";
        default:
            return error;
    }
}

function turnErrorText(error: string): string {
    switch (error) {
        case "truncated extract cannot be written":
            return "摘录被截断，已拒绝写回";
        case "file changed on disk":
            return "文件在磁盘上已被修改";
        case "file is read only":
        case "file is not writable":
            return "没有写入权限";
        case "path is outside the open file":
            return "这个文件没有打开";
        default:
            return error;
    }
}

function exportSaveError(raw: string): string {
    const lower = raw.toLowerCase();
    if (lower.includes("not enough space") || lower.includes("no space left") || lower.includes("disk full")) return "磁盘已满";
    if (lower.includes("access is denied") || lower.includes("permission denied")) return "没有写入权限";
    return "无法保存到该位置";
}

// Wails rejects a Go error as a plain string: window.wails.Callback passes
// message.error through, and that value is not an Error. Reading only
// Error.message hides 需要分享密码, so the password box never opens.
function bindingErrorText(err: unknown): string {
    if (err instanceof Error) return err.message.trim();
    if (typeof err === "string") return err.trim();
    return "";
}

function exportErrorText(err: unknown): string {
    const raw = bindingErrorText(err);
    if (raw.includes("MaClaw Hub login is required")) return "需要先在主窗口登录，才能上传到云盘";
    if (raw.includes("upload mobile document failed") || raw.includes("Hub upload") || raw.includes("decode upload response")) return "上传到云盘失败，请稍后重试";
    if (raw === "web session is not open") return "这个页面没有打开";
    if (raw === "path is outside the open file") return "这个文件没有打开";
    if (raw === "file is empty") return "文件是空的";
    // Only the upload of a folder uses this sentence. A save error such as
    // "The directory name is invalid", or a path that contains "director",
    // is a failed PDF write.
    if (raw === "path is a directory") return "不支持上传文件夹";
    if (raw.includes("创建目录失败") || raw.includes("写入 PDF 失败")) return exportSaveError(raw);
    if (raw === "file too large to compress safely") return "文件太大，无法上传到云盘";
    return raw || "操作失败";
}

function knowledgeResultText(result: { imported_files?: number; failed_files?: number; skipped_files?: number; last_item_reason?: string } | null | undefined): { ok: boolean; text: string } {
    const imported = Number(result?.imported_files ?? 0);
    const failed = Number(result?.failed_files ?? 0);
    const skipped = Number(result?.skipped_files ?? 0);
    if (failed > 0 && imported === 0) {
        const reason = String(result?.last_item_reason ?? "").trim();
        return { ok: false, text: reason ? `导入知识库失败：${reason}` : "导入知识库失败" };
    }
    if (imported === 0 && skipped > 0) return { ok: true, text: "文件已在桌面知识库中" };
    return { ok: true, text: "已进入桌面知识库" };
}

function highlightsFromDoc(doc: CompanionDoc): PdfMark[] | null {
    const has = Object.prototype.hasOwnProperty.call(doc, "highlights") || Object.prototype.hasOwnProperty.call(doc, "Highlights");
    if (!has) return null;
    return normalizePdfMarks(doc.highlights ?? doc.Highlights);
}

function messagesFromDoc(doc: CompanionDoc): ChatMessage[] {
    const raw = doc.messages ?? doc.Messages;
    if (!Array.isArray(raw)) return [];
    const out: ChatMessage[] = [];
    raw.forEach((item, index) => {
        if (!item || typeof item !== "object") return;
        const role = item.role ?? item.Role;
        if (role !== "user" && role !== "assistant") return;
        const text = companionVisibleText(String(item.text ?? item.Text ?? ""));
        const reasoning = role === "assistant" ? companionVisibleText(String(item.reasoning ?? item.Reasoning ?? "")) : "";
        const resultPath = role === "assistant" ? String(item.resultPath ?? item.ResultPath ?? "").trim() : "";
        const noteCardPath = role === "assistant" ? String(item.noteCardPath ?? item.NoteCardPath ?? "").trim() : "";
        const webNote = role === "assistant" && (item.webNote === true || item.WebNote === true);
        const figures = role === "assistant" ? figuresFromValue(item.figures ?? item.Figures) : [];
        if (!text.trim() && !reasoning.trim() && !resultPath && !noteCardPath) return;
        out.push({ id: `restored:${index}`, role, text, reasoning, resultPath: resultPath || undefined, noteCardPath: noteCardPath || undefined, webNote, figures: figures.length > 0 ? figures : undefined });
    });
    return out;
}

function eventBody(payload: unknown): Record<string, unknown> | null {
    if (typeof payload === "string") {
        const trimmed = payload.trim();
        if (!trimmed) return null;
        try {
            const parsed = JSON.parse(trimmed) as unknown;
            if (parsed && typeof parsed === "object") return parsed as Record<string, unknown>;
        } catch {
            return { text: payload };
        }
        return null;
    }
    if (payload && typeof payload === "object") return payload as Record<string, unknown>;
    return null;
}

function eventString(body: Record<string, unknown>, ...keys: string[]): string {
    for (const key of keys) {
        const value = body[key];
        if (typeof value === "string") return value;
    }
    return "";
}

function officePayloadJSON(text: string, name: string): string {
    const ext = name.toLowerCase().split(".").pop() || "";
    if (ext !== "xlsx" && ext !== "pptx") return "";
    let raw = text.trim();
    if (raw.startsWith("```")) {
        const nl = raw.indexOf("\n");
        if (nl >= 0) raw = raw.slice(nl + 1);
        raw = raw.trim().replace(/```$/, "").trim();
    }
    const start = raw.indexOf("{");
    const end = raw.lastIndexOf("}");
    if (start < 0 || end <= start) return "";
    const body = raw.slice(start, end + 1);
    try {
        const parsed = JSON.parse(body) as Record<string, unknown>;
        if (ext === "xlsx" && !Object.prototype.hasOwnProperty.call(parsed, "sheets")) return "";
        if (ext === "pptx" && !Object.prototype.hasOwnProperty.call(parsed, "slides")) return "";
        return body;
    } catch {
        return "";
    }
}

function tabFromDoc(doc: CompanionDoc, fallbackPath: string): CompanionTab {
    const path = field(doc, "path", "Path", fallbackPath);
    const name = field(doc, "name", "Name", path.split(/[/\\]/).pop() || path);
    return {
        path,
        name,
        editable: field(doc, "editable", "Editable", false),
        readOnly: field(doc, "readOnly", "ReadOnly", !field(doc, "editable", "Editable", false)),
        canExport: field(doc, "canExport", "CanExport", false),
        content: field(doc, "content", "Content", ""),
        loadedHash: field(doc, "loadedHash", "LoadedHash", ""),
        sessionId: field(doc, "sessionId", "SessionID", ""),
        size: Number(field(doc, "size", "Size", 0)) || 0,
        error: field(doc, "error", "Error", ""),
        draft: "",
        selection: "",
        selectionStart: -1,
        messages: messagesFromDoc(doc),
        readOnlyReason: field(doc, "readOnlyReason", "ReadOnlyReason", ""),
        status: "",
        dirty: false,
        conflict: false,
        diskHash: "",
        confirmOverwrite: false,
        previewEpoch: 0,
        saveError: "",
        htmlPreview: false,
    };
}

function isAtxHeading(line: string): boolean {
    return /^#{1,6}\s+\S/.test(line);
}

const AtxHeadingLine = memo(function AtxHeadingLine({ line, index, onActivate }: { line: string; index: number; onActivate: (index: number) => void }) {
    return (
        <div onClick={() => onActivate(index)} data-testid="file-companion-atx-line">
            <MarkdownPreview content={line} theme={companionPreviewTheme} />
        </div>
    );
});

const markdownSegmentLineCap = 2000;

type MarkdownSegment =
    | { kind: "edit"; start: number; text: string }
    | { kind: "heading"; index: number; line: string };

function fenceMarker(line: string): { char: "`" | "~"; length: number; closes: boolean } | null {
    const match = /^( {0,3})(`{3,}|~{3,})(.*)$/.exec(line);
    if (!match) return null;
    const char = match[2][0] as "`" | "~";
    const rest = match[3];
    if (char === "`" && rest.includes("`")) return null;
    return { char, length: match[2].length, closes: rest.trim() === "" };
}

function fencedLines(lines: string[]): boolean[] {
    const fenced = new Array<boolean>(lines.length).fill(false);
    let open: { char: "`" | "~"; length: number } | null = null;
    for (let i = 0; i < lines.length; i += 1) {
        const marker = fenceMarker(lines[i]);
        if (open) {
            fenced[i] = true;
            if (marker && marker.char === open.char && marker.closes && marker.length >= open.length) open = null;
            continue;
        }
        if (marker) {
            fenced[i] = true;
            open = { char: marker.char, length: marker.length };
        }
    }
    return fenced;
}

function completedHeadingLines(value: string): Set<number> | null {
    let newlines = 0;
    for (let i = 0; i < value.length; i += 1) {
        if (value.charCodeAt(i) !== 10) continue;
        newlines += 1;
        if (newlines >= markdownSegmentLineCap) return null;
    }
    const lines = value.split("\n");
    const fenced = fencedLines(lines);
    const headings = new Set<number>();
    for (let index = 0; index < lines.length; index += 1) {
        if (!fenced[index] && isAtxHeading(lines[index])) headings.add(index);
    }
    return headings;
}

function lineIsCompletedHeading(value: string, line: number, known?: Set<number> | null): boolean {
    if (line < 0) return false;
    if (known !== undefined) return known !== null && known.has(line);
    const headings = completedHeadingLines(value);
    return headings !== null && headings.has(line);
}

function normalizedLineStart(value: string, line: number): number {
    const lines = value.replace(/\r\n/g, "\n").split("\n");
    let offset = 0;
    const last = Math.max(0, Math.min(line, lines.length));
    for (let i = 0; i < last; i += 1) offset += lines[i].length + 1;
    return offset;
}

function selectionOffset(text: string, selectionStart: number): number {
    const start = Number.isFinite(selectionStart) ? selectionStart : text.length;
    return text.slice(0, start).replace(/\r\n/g, "\n").length;
}

function selectionFromEditor(text: string, start: number, end: number): { selection: string; selectionStart: number } {
    const selection = text.slice(start, end);
    if (!selection) return { selection: "", selectionStart: -1 };
    return { selection, selectionStart: selectionOffset(text, start) };
}

function documentSelection(documentText: string, segmentStart: number, segmentText: string, start: number, end: number): { selection: string; selectionStart: number } {
    const picked = selectionFromEditor(segmentText, start, end);
    if (!picked.selection) return picked;
    return { selection: picked.selection, selectionStart: normalizedLineStart(documentText, segmentStart) + picked.selectionStart };
}

function sameSelectionAnchor(content: string, selection: string, selectionStart: number): boolean {
    if (!selection) return selectionStart < 0;
    if (selectionStart < 0) return false;
    const normSelection = selection.replace(/\r\n/g, "\n");
    const normContent = content.replace(/\r\n/g, "\n");
    return normContent.slice(selectionStart, selectionStart + normSelection.length) === normSelection;
}

function caretColumn(text: string, selectionStart: number): { line: number; column: number } {
    const start = Number.isFinite(selectionStart) ? selectionStart : text.length;
    const before = text.slice(0, start);
    return { line: before.split("\n").length - 1, column: start - (before.lastIndexOf("\n") + 1) };
}

function markdownSegments(value: string, activeLine: number): MarkdownSegment[] {
    const lines = value.split("\n");
    if (lines.length > markdownSegmentLineCap) {
        return [{ kind: "edit", start: 0, text: value }];
    }
    const fenced = fencedLines(lines);
    const heading = (index: number) => !fenced[index] && isAtxHeading(lines[index]) && index !== activeLine;
    const segments: MarkdownSegment[] = [];
    let index = 0;
    while (index < lines.length) {
        if (heading(index)) {
            segments.push({ kind: "heading", index, line: lines[index] });
            index += 1;
            continue;
        }
        const start = index;
        const chunk: string[] = [];
        while (index < lines.length && !heading(index)) {
            chunk.push(lines[index]);
            index += 1;
        }
        segments.push({ kind: "edit", start, text: chunk.join("\n") });
    }
    if (segments.length === 0) {
        segments.push({ kind: "edit", start: 0, text: value });
    }
    return segments;
}

function editorSegmentStart(segments: MarkdownSegment[], activeLine: number): number {
    let first = -1;
    for (const segment of segments) {
        if (segment.kind !== "edit") continue;
        if (first < 0) first = segment.start;
        const count = segment.text.split("\n").length;
        if (activeLine >= segment.start && activeLine < segment.start + count) return segment.start;
    }
    return first;
}

function spliceSegment(documentText: string, start: number, previous: string, next: string): string {
    const lines = documentText.split("\n");
    const removed = previous.split("\n").length;
    return lines.slice(0, start).concat(next.split("\n"), lines.slice(start + removed)).join("\n");
}

function MarkdownSourceEditor({
    value,
    onChange,
    onSelect,
    readOnly,
}: {
    value: string;
    onChange: (value: string) => void;
    onSelect: (selection: string, selectionStart: number) => void;
    readOnly?: boolean;
}) {
    const [activeLine, setActiveLine] = useState(-1);
    const editorRef = useRef<HTMLTextAreaElement | null>(null);
    const pendingFocus = useRef<{ line: number; offset: number } | null>(null);
    const seenValue = useRef(value);
    const valueChanged = seenValue.current !== value;
    seenValue.current = value;
    const segments = useMemo(() => markdownSegments(value, activeLine), [value, activeLine]);
    const editorStart = useMemo(() => editorSegmentStart(segments, activeLine), [segments, activeLine]);
    const completedHeadings = useMemo(() => completedHeadingLines(value), [value]);
    useEffect(() => {
        const pending = pendingFocus.current;
        const el = editorRef.current;
        if (!pending || !el) return;
        pendingFocus.current = null;
        const lines = el.value.split("\n");
        const local = pending.line - editorStart;
        let pos = 0;
        if (local >= 0 && local < lines.length) {
            for (let i = 0; i < local; i += 1) pos += lines[i].length + 1;
            const offset = pending.offset < 0 ? lines[local].length : Math.min(pending.offset, lines[local].length);
            pos += offset;
        } else if (pending.offset < 0) {
            pos = el.value.length;
        }
        el.focus();
        el.setSelectionRange(pos, pos);
    }, [activeLine, editorStart, value]);
    const placeCaret = (segmentStart: number, text: string, selectionStart: number, nextDocument: string) => {
        const caret = caretColumn(text, selectionStart);
        const nextLine = segmentStart + caret.line;
        const nextHeadings = nextDocument === value ? completedHeadings : undefined;
        if (nextLine !== activeLine && (lineIsCompletedHeading(value, activeLine, completedHeadings) || lineIsCompletedHeading(nextDocument, nextLine, nextHeadings))) {
            pendingFocus.current = { line: nextLine, offset: caret.column };
        }
        if (nextLine !== activeLine) setActiveLine(nextLine);
    };
    const activate = useCallback((index: number) => {
        pendingFocus.current = { line: index, offset: 0 };
        setActiveLine(index);
    }, []);
    return (
        <div data-testid="file-companion-markdown">
            {segments.map((segment) => {
                if (segment.kind === "heading") {
                    return <AtxHeadingLine key={`h-${segment.index}`} line={segment.line} index={segment.index} onActivate={activate} />;
                }
                const count = segment.text.split("\n").length;
                const primary = segment.start === editorStart;
                return (
                    <textarea
                        key={`e-${segment.start}`}
                        ref={primary ? editorRef : undefined}
                        data-testid={primary ? "file-companion-editor" : undefined}
                        value={segment.text}
                        readOnly={Boolean(readOnly)}
                        spellCheck={false}
                        onChange={(event) => {
                            if (readOnly) return;
                            const el = event.target;
                            const nextDocument = spliceSegment(value, segment.start, segment.text, el.value);
                            placeCaret(segment.start, el.value, el.selectionStart, nextDocument);
                            onChange(nextDocument);
                            const picked = documentSelection(nextDocument, segment.start, el.value, el.selectionStart, el.selectionEnd);
                            onSelect(picked.selection, picked.selectionStart);
                        }}
                        onSelect={(event) => {
                            const el = event.currentTarget;
                            if (valueChanged && el.selectionStart === el.selectionEnd) return;
                            placeCaret(segment.start, el.value, el.selectionStart, value);
                            const picked = documentSelection(value, segment.start, el.value, el.selectionStart, el.selectionEnd);
                            onSelect(picked.selection, picked.selectionStart);
                        }}
                        onKeyDown={(event) => {
                            if (event.shiftKey || event.altKey || event.ctrlKey || event.metaKey) return;
                            if (event.key !== "ArrowUp" && event.key !== "ArrowDown") return;
                            const el = event.currentTarget;
                            if (el.selectionStart !== el.selectionEnd) return;
                            const local = el.value.slice(0, el.selectionStart).split("\n").length - 1;
                            const lineCount = el.value.split("\n").length;
                            if (event.key === "ArrowUp" && local === 0 && segment.start > 0) {
                                event.preventDefault();
                                pendingFocus.current = { line: segment.start - 1, offset: -1 };
                                setActiveLine(segment.start - 1);
                                return;
                            }
                            if (event.key === "ArrowDown" && local === lineCount - 1 && segment.start + lineCount < value.split("\n").length) {
                                event.preventDefault();
                                pendingFocus.current = { line: segment.start + lineCount, offset: 0 };
                                setActiveLine(segment.start + lineCount);
                            }
                        }}
                        style={{ width: "100%", minHeight: segments.length === 1 ? 220 : Math.max(44, count * 22), border: "1px solid #e2e8f0", borderRadius: 8, padding: 12, font: "14px/1.5 ui-monospace, monospace", resize: "vertical" }}
                    />
                );
            })}
        </div>
    );
}

function CompanionActionIcon({ name, size = 14 }: { name: "paper" | "knowledge" | "cloud" | "hide" | "markdown" | "latex" | "pdf" | "check" | "file" | "fullscreen" | "restore" | "project" | "library" | "convert" | "note" | "stop" | "share" | "groups"; size?: number }) {
    const common = {
        fill: "none",
        stroke: "currentColor",
        strokeWidth: 1.8,
        strokeLinecap: "round" as const,
        strokeLinejoin: "round" as const,
    };
    return (
        <svg className="file-companion-tool-icon" data-icon={name} width={size} height={size} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
            {name === "markdown" && (
                <>
                    <path {...common} d="M3.6 17.2V7.2L8 12.6l4.4-5.4v10" />
                    <path {...common} d="M16.2 8.2v7.2" />
                    <path {...common} d="m13.4 12.8 2.8 2.6 2.8-2.6" />
                </>
            )}
            {name === "latex" && (
                <>
                    <path {...common} d="M4.2 17.4 9 6.2 13.8 17.4" />
                    <path {...common} d="M6.4 13.2h5.2" />
                    <path {...common} d="m15.4 17.6 3.6-5.2" />
                    <path {...common} d="m15.4 12.4 3.6 5.2" />
                </>
            )}
            {name === "project" && (
                <>
                    <path {...common} d="M4.5 8.2h15v11.2h-15z" />
                    <path {...common} d="M4.5 8.2 8 4.6h8l3.5 3.6" />
                    <path {...common} d="M12 11.2v5.2" />
                    <path {...common} d="m9.6 14.2 2.4 2.3 2.4-2.3" />
                </>
            )}
            {name === "pdf" && (
                <>
                    <path {...common} d="M12.6 4.5H7.4a1.8 1.8 0 0 0-1.8 1.8v11.4a1.8 1.8 0 0 0 1.8 1.8h9.2a1.8 1.8 0 0 0 1.8-1.8V10z" />
                    <path {...common} d="M12.6 4.5V10h5.8" />
                    <path {...common} d="M12 13.4v4.2" />
                    <path {...common} d="m9.9 15.4 2.1 2.2 2.1-2.2" />
                </>
            )}
            {name === "note" && (
                <>
                    <path {...common} d="M5.5 4.5h9L18 8v11.5h-12.5z" />
                    <path {...common} d="M14.5 4.5V8H18" />
                    <path {...common} d="m9.4 14.6 4.9-4.9 1.9 1.9-4.9 4.9-2.1.2z" />
                </>
            )}
            {name === "share" && (
                <>
                    <path {...common} d="M6.5 12.2h11a3.5 3.5 0 0 0 .2-7 5.5 5.5 0 0 0-10.6 1.5A3.2 3.2 0 0 0 6.5 12.2Z" />
                    <path {...common} d="M12 20.5v-8.2" />
                    <path {...common} d="m9.3 14.8 2.7-2.7 2.7 2.7" />
                </>
            )}
            {name === "groups" && (
                <>
                    <circle {...common} cx="9" cy="8" r="2.2" />
                    <circle {...common} cx="16" cy="9.2" r="1.8" />
                    <path {...common} d="M4.8 18.2c.5-2.5 2.3-3.8 4.2-3.8s3.7 1.3 4.2 3.8" />
                    <path {...common} d="M13.6 14.8c1.5-.2 3 .5 3.8 2.2" />
                </>
            )}
            {name === "stop" && <rect {...common} x="7.2" y="7.2" width="9.6" height="9.6" rx="1.6" />}
            {name === "check" && <path {...common} d="m5 12.8 4.4 4.2L19 6.6" />}
            {name === "paper" && (
                <>
                    <path {...common} d="M6.5 3.5h6.8L17.5 8v12.5h-11z" />
                    <path {...common} d="M13.3 3.5V8h4.2" />
                    <path {...common} d="M9 11.5h3.6" />
                    <path {...common} d="M9 14.5h2.2" />
                    <circle {...common} cx="16.3" cy="16.5" r="2.5" />
                    <path {...common} d="m18.1 18.3 2.1 2.1" />
                </>
            )}
            {name === "library" && (
                <>
                    <path {...common} d="M5 8.2h14v11.2H5z" />
                    <path {...common} d="M8 8.2V5.2h8v3" />
                    <path {...common} d="M8 12h8" />
                    <path {...common} d="M8 15.2h5" />
                </>
            )}
            {name === "file" && (
                <>
                    <path {...common} d="M6.5 3.5h6.8L17.5 8v12.5h-11z" />
                    <path {...common} d="M13.3 3.5V8h4.2" />
                </>
            )}
            {name === "convert" && (
                <>
                    <path {...common} d="M7 4.5h6.2L17 8.2V19.5H7z" />
                    <path {...common} d="M13.2 4.5V8.2H17" />
                    <path {...common} d="M9.2 14.8a2.5 2.5 0 1 0 .6-1.9" />
                    <path {...common} d="M9.1 11.2v2h2" />
                </>
            )}
            {name === "knowledge" && (
                <>
                    <path {...common} d="M4 19.5v-15A2.5 2.5 0 0 1 6.5 2H20v20H6.5a2.5 2.5 0 0 1 0-5H20" />
                    <path {...common} d="M12 7v6" />
                    <path {...common} d="m9 10 3 3 3-3" />
                </>
            )}
            {name === "cloud" && (
                <>
                    <path {...common} d="M6.5 12.2h11a3.5 3.5 0 0 0 .2-7 5.5 5.5 0 0 0-10.6 1.5A3.2 3.2 0 0 0 6.5 12.2Z" />
                    <path {...common} d="M12 21.5V19" />
                    <path {...common} d="m9.3 19 2.7-2.7 2.7 2.7" />
                </>
            )}
            {name === "hide" && (
                <>
                    <path {...common} d="M3.2 12s3.4-5 8.8-5 8.8 5 8.8 5-3.4 5-8.8 5-8.8-5-8.8-5Z" />
                    <circle {...common} cx="12" cy="12" r="2.1" />
                    <path {...common} d="M5.2 18.8 18.8 5.2" />
                </>
            )}
            {name === "fullscreen" && (
                <>
                    <path {...common} d="M9 4.5H4.5V9" />
                    <path {...common} d="M15 4.5h4.5V9" />
                    <path {...common} d="M9 19.5H4.5V15" />
                    <path {...common} d="M15 19.5h4.5V15" />
                </>
            )}
            {name === "restore" && (
                <>
                    <path {...common} d="M9 4.5v4.5H4.5" />
                    <path {...common} d="M15 4.5v4.5h4.5" />
                    <path {...common} d="M9 19.5v-4.5H4.5" />
                    <path {...common} d="M15 19.5v-4.5h4.5" />
                </>
            )}
        </svg>
    );
}

type CompanionActionState = "idle" | "busy" | "ok" | "err";

const COMPANION_ACTION_STATE_MS = 1600;

function CompanionBubbleAction({
    name,
    label,
    doneLabel,
    failedLabel,
    state,
    testId,
    onClick,
}: {
    name: Parameters<typeof CompanionActionIcon>[0]["name"];
    label: string;
    doneLabel: string;
    failedLabel: string;
    state: CompanionActionState;
    testId: string;
    onClick: () => void;
}) {
    return (
        <button
            type="button"
            className="file-companion-bubble-action"
            data-testid={testId}
            data-state={state}
            aria-label={label}
            title={state === "ok" ? doneLabel : state === "err" ? failedLabel : label}
            aria-busy={state === "busy"}
            disabled={state === "busy"}
            onClick={(event) => {
                event.preventDefault();
                event.stopPropagation();
                onClick();
            }}
        >
            {state === "ok" ? <CompanionActionIcon name="check" size={14} /> : <CompanionActionIcon name={name} size={14} />}
        </button>
    );
}

/**
 * One control's result tick: a green check after a success, a red glyph after a
 * failure, both back to the plain icon on their own. Two controls on a bubble
 * each keep their own tick, so a finished copy does not look like a finished
 * export.
 */
function useCompanionActionState(): [CompanionActionState, (next: CompanionActionState) => void] {
    const [state, setState] = useState<CompanionActionState>("idle");
    const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
    const mounted = useRef(true);
    useEffect(() => () => {
        mounted.current = false;
        if (timer.current) clearTimeout(timer.current);
    }, []);
    const settle = (next: CompanionActionState) => {
        if (!mounted.current) return;
        setState(next);
        if (timer.current) clearTimeout(timer.current);
        if (next === "idle") return;
        timer.current = setTimeout(() => {
            if (mounted.current) setState("idle");
        }, COMPANION_ACTION_STATE_MS);
    };
    return [state, settle];
}

/**
 * The text one message contributes to anything outside the bubble: the copy
 * and export controls, and the whole-transcript export.
 *
 * Sharing this matters because a paper turn's text is not its answer. Its
 * question says "已生成论文解读" only while the narration is still being
 * written, and the real content lives in a separate result card. Copying
 * message.text straight out would put that placeholder in a PDF that then
 * claims nothing was produced.
 */
function companionMessageExportText(message: ChatMessage, streaming: boolean): string {
    const cleanedReasoning = cleanReasoningTrailForBody(message.reasoning || "");
    const visible = resolveVisibleAssistantReply(message.text, cleanedReasoning, { live: streaming });
    return visible.content;
}

/** What a transcript export managed to include, and what it could not. */
type CompanionChatExport = {
    turns: CompanionChatTurn[];
    missingFigures: number;
    missingDiagrams: number;
};

/**
 * Text of one turn that belongs in an exported transcript, or "" when it does not.
 *
 * Skipped: a reply still streaming (a half-written answer is not what the
 * reader asked to keep), a paper card (its body is a separate PDF; the bubble
 * only says 已生成论文解读), and an empty bubble.
 */
function companionChatTurnBody(message: ChatMessage, index: number, lastIndex: number, tabBusy: boolean): string {
    const mine = message.role === "user";
    if (message.resultPath) return "";
    if (!mine && tabBusy && index === lastIndex) return "";
    const content = mine ? message.text : companionMessageExportText(message, false);
    return content.trim() ? content : "";
}

function companionChatHasExportableTurn(messages: ChatMessage[], tabBusy: boolean): boolean {
    const lastIndex = messages.length - 1;
    return messages.some((message, index) => companionChatTurnBody(message, index, lastIndex, tabBusy) !== "");
}

/**
 * Every turn that belongs in an exported transcript.
 *
 * What is left is paired question and answer text, which is the only form in
 * which most answers read correctly.
 *
 * Figures are fetched as bytes and appended to their answer. The bytes have to
 * travel with the text because a path cannot be resolved on the reader's side
 * of a save dialog — the bubbles already hold these images for that reason.
 *
 * Failures are counted rather than swallowed. A figure that cannot be read
 * leaves a gap in the document, and a gap the reader was never told about is
 * indistinguishable from the export having lost it.
 */
async function companionChatExportTurns(
    messages: ChatMessage[],
    tabBusy: boolean,
    theme: AssistantTheme,
): Promise<CompanionChatExport> {
    const lastIndex = messages.length - 1;
    const turns: CompanionChatTurn[] = [];
    let missingFigures = 0;
    let missingDiagrams = 0;
    for (const [index, message] of messages.entries()) {
        let content = companionChatTurnBody(message, index, lastIndex, tabBusy);
        if (!content) continue;
        const mine = message.role === "user";
        if (!mine) {
            const inlined = await inlineCompanionDiagrams(content, theme);
            content = inlined.text;
            missingDiagrams += inlined.missing;
        }
        if (!mine) {
            const withFigures = await appendCompanionFigures(content, message.figures);
            content = withFigures.text;
            missingFigures += withFigures.missing;
        }
        turns.push({ role: mine ? "user" : "assistant", content });
    }
    return { turns, missingFigures, missingDiagrams };
}

/**
 * An answer's text with the drawings the window shows beneath it.
 *
 * Shared by both exports, because both are the answer as the reader saw it: a
 * single answer is exported with its own figures, a transcript with each
 * answer's. Figures are fetched together rather than one after another — each
 * is its own round trip to the host and the reader is waiting on all of them
 * either way — while order is kept by indexing, not by awaiting in turn.
 */
async function appendCompanionFigures(
    content: string,
    figures: CompanionFigure[] | undefined,
): Promise<{ text: string; missing: number }> {
    if (!figures?.length) return { text: content, missing: 0 };
    const loaded = await Promise.all(figures.map((figure) => loadCompanionFigureDataURL(figure.path)));
    const blocks: string[] = [];
    let missing = 0;
    figures.forEach((figure, index) => {
        const dataURL = loaded[index];
        if (!dataURL) {
            missing += 1;
            return;
        }
        blocks.push(companionFigureMarkdown(figure, dataURL));
    });
    if (blocks.length === 0) return { text: content, missing };
    return { text: `${content.trim()}\n\n${blocks.join("\n\n")}`, missing };
}

/**
 * One figure as Markdown, carrying the caption the window shows beneath it.
 *
 * The answer says "图 8.1"; a picture with no label leaves the reader matching
 * numbers to drawings themselves. Markdown ends an image's alt text at the
 * first bracket or parenthesis, so those become spaces instead of truncating
 * the caption.
 */
function companionFigureMarkdown(figure: CompanionFigure, dataURL: string): string {
    const label = [figure.label, figure.caption].filter(Boolean).join(" ").trim();
    const alt = label.replace(/[[\]()]/g, " ").replace(/\s+/g, " ").trim();
    return `![${alt}](${dataURL})`;
}

// The renderer's own fence rule: three or more backticks or tildes.
const COMPANION_FENCE_RE = /^(\s*)(`{3,}|~{3,})(.*)$/;

/**
 * Replace each Mermaid fence with the diagram it draws.
 *
 * The window draws these diagrams; the document layer cannot, having no
 * JavaScript. Handing it the picture is what keeps a diagram a diagram rather
 * than a page of Mermaid source. A fence that will not draw keeps its source,
 * which is what the reader would have seen anyway.
 */
async function inlineCompanionDiagrams(markdown: string, theme: AssistantTheme): Promise<{ text: string; missing: number }> {
    if (!markdown.includes("```") && !markdown.includes("~~~")) {
        return { text: markdown, missing: 0 };
    }
    const lines = markdown.split("\n");
    const out: string[] = [];
    let missing = 0;
    for (let i = 0; i < lines.length; i += 1) {
        const open = lines[i].match(COMPANION_FENCE_RE);
        if (!open) {
            out.push(lines[i]);
            continue;
        }
        const marker = open[2];
        let close = -1;
        for (let j = i + 1; j < lines.length; j += 1) {
            const candidate = lines[j].match(COMPANION_FENCE_RE);
            if (candidate && candidate[2][0] === marker[0] && candidate[2].length >= marker.length && candidate[3].trim() === "") {
                close = j;
                break;
            }
        }
        // An unclosed fence runs to the end of the text. The lines inside it
        // stay in that block: a later marker is not a new fence, which is how
        // an unclosed fence stays one block on screen.
        if (close < 0) {
            out.push(...lines.slice(i));
            break;
        }
        if (!isMermaidCodeFence(open[3])) {
            out.push(...lines.slice(i, close + 1));
            i = close;
            continue;
        }
        const png = await renderMermaidFenceToPNGDataURL(lines.slice(i + 1, close).join("\n"), theme);
        if (png) {
            out.push(`![](${png})`);
        } else {
            missing += 1;
            out.push(...lines.slice(i, close + 1));
        }
        i = close;
    }
    return { text: out.join("\n"), missing };
}

// One figure's bytes, already kept by the display layer. A failure yields an
// empty string; the caller counts it so the reader is told.
async function loadCompanionFigureDataURL(path: string): Promise<string> {
    try {
        return (await FileCompanionFigureDataURL(path)) || "";
    } catch {
        return "";
    }
}

/**
 * What to tell the reader once the document is written.
 *
 * The export succeeds either way; what differs is whether the reader learns
 * that part of it is missing. A figure dropped in silence reads exactly like a
 * complete export until somebody opens it.
 */
function companionExportNotice(missingFigures: number, missingDiagrams: number): string {
    const missing: string[] = [];
    if (missingFigures > 0) missing.push(`${missingFigures} 张配图`);
    if (missingDiagrams > 0) missing.push(`${missingDiagrams} 张流程图`);
    if (missing.length === 0) return "已导出 PDF";
    return `已导出 PDF，但 ${missing.join("、")}未能读取，未包含在内`;
}

function companionExportNoticeKind(missingFigures: number, missingDiagrams: number): "success" | "warning" {
    return missingFigures > 0 || missingDiagrams > 0 ? "warning" : "success";
}

function companionCloudExportNotice(uploaded: { duplicate?: boolean; duplicate_of_title?: string; title?: string } | null | undefined, missingFigures: number, missingDiagrams: number): string {
    const held = uploaded?.duplicate_of_title || uploaded?.title || "";
    const base = uploaded?.duplicate
        ? `云盘中已有相同内容${held ? `（${held}）` : ""}，未再次上传`
        : "已上传到云盘，手机端「文档」可打开";
    const missing: string[] = [];
    if (missingFigures > 0) missing.push(`${missingFigures} 张配图`);
    if (missingDiagrams > 0) missing.push(`${missingDiagrams} 张流程图`);
    if (missing.length === 0) return base;
    return `${base}，但 ${missing.join("、")}未能读取，未包含在内`;
}

/**
 * Top-right controls on one companion answer: copy the Markdown source, and
 * render it into a PDF the reader names.
 */
function CompanionMessageActions({ path, markdown, messageId, figures, webSession }: { path: string; markdown: string; messageId: string; figures?: CompanionFigure[]; webSession?: string }) {
    const [copyState, settleCopy] = useCompanionActionState();
    const [exportState, settleExport] = useCompanionActionState();
    const [texState, settleTex] = useCompanionActionState();
    const { showToast } = useToast();
    const runCopy = async () => {
        settleCopy("busy");
        settleCopy((await copyTextToClipboard(markdown)) ? "ok" : "err");
    };
    const runExport = async () => {
        settleExport("busy");
        try {
            // The same two substitutions the transcript export makes. This path
            // hands the document layer one answer's Markdown, and without them
            // a diagram would reach the page as Mermaid source and the drawings
            // the reader saw under the answer would be missing.
            const withDiagrams = await inlineCompanionDiagrams(markdown, lightTheme);
            const withFigures = await appendCompanionFigures(withDiagrams.text, figures);
            const dest = webSession
                ? await FileCompanionExportWebPDF(webSession, withFigures.text)
                : await FileCompanionExportPDF(path, withFigures.text);
            // An empty path is the reader cancelling the save dialog.
            if (dest) {
                // The two counts stay apart: a missing picture and a missing
                // diagram are different things to the reader, and adding them
                // up would report one as the other.
                showToast(
                    companionExportNotice(withFigures.missing, withDiagrams.missing),
                    companionExportNoticeKind(withFigures.missing, withDiagrams.missing),
                    6000,
                );
            }
            settleExport(dest ? "ok" : "idle");
        } catch (err) {
            showToast(exportErrorText(err), "error", 6000);
            settleExport("err");
        }
    };
    const runTeX = async () => {
        settleTex("busy");
        try {
            const dest = webSession
                ? await FileCompanionExportWebTeX(webSession, markdown)
                : await FileCompanionExportTeX(path, markdown);
            if (dest) showToast("已导出 LaTeX", "success", 6000);
            settleTex(dest ? "ok" : "idle");
        } catch (err) {
            showToast(exportErrorText(err), "error", 6000);
            settleTex("err");
        }
    };
    return (
        <div className="file-companion-bubble-actions" data-testid={`file-companion-actions-${messageId}`}>
            <CompanionBubbleAction
                name="markdown"
                label="复制为 Markdown"
                doneLabel="已复制"
                failedLabel="复制失败"
                state={copyState}
                testId={`file-companion-copy-${messageId}`}
                onClick={() => { void runCopy(); }}
            />
            <CompanionBubbleAction
                name="pdf"
                label="导出为 PDF"
                doneLabel="已导出"
                failedLabel="导出失败"
                state={exportState}
                testId={`file-companion-export-pdf-${messageId}`}
                onClick={() => { void runExport(); }}
            />
            <CompanionBubbleAction
                name="latex"
                label="导出为 LaTeX"
                doneLabel="已导出"
                failedLabel="导出失败"
                state={texState}
                testId="file-companion-export-tex"
                onClick={() => { void runTeX(); }}
            />
        </div>
    );
}

type NoteAnswerBlock = { kind: "text"; text: string } | { kind: "stamp"; stamp: NoteStampLine };

// A seekable stamp is one line. Everything else stays one markdown document so a
// $$ block, a table, or a fence is not split at each newline. The stamp's own
// sentence still goes through the inline renderer, including $...$.
// A formula row that starts with a time stays inside the open block. A chapter
// sentence, or a heading, ends a block that was left open.
function noteDisplayMathOpens(line: string): "$$" | "\\[" | "" {
    const trim = line.trim();
    if (trim.startsWith("$$") && trim.endsWith("$$") && trim.length > 4) return "";
    if (trim.startsWith("\\[") && trim.endsWith("\\]") && trim.length > 4) return "";
    if (trim === "$$" || (trim.startsWith("$$") && trim.length > 2)) return "$$";
    if (trim === "\\[" || (trim.startsWith("\\[") && trim.length > 2)) return "\\[";
    return "";
}

function noteDisplayMathCloses(line: string, open: "$$" | "\\["): boolean {
    const trim = line.trim();
    const close = open === "$$" ? "$$" : "\\]";
    return trim === close || (trim.length > close.length && trim.endsWith(close));
}

function noteFormulaBody(rest: string): boolean {
    if (rest.includes("\\") || rest.includes("&")) return true;
    const prose = rest.trim();
    if (!prose) return false;
    return !/\p{Script=Han}/u.test(prose);
}

function noteAnswerBlocks(text: string): NoteAnswerBlock[] {
    const blocks: NoteAnswerBlock[] = [];
    const pending: string[] = [];
    let math: "$$" | "\\[" | "" = "";
    const flush = () => {
        if (pending.length === 0) return;
        blocks.push({ kind: "text", text: pending.join("\n") });
        pending.length = 0;
    };
    for (const line of text.split("\n")) {
        if (math) {
            if (noteDisplayMathCloses(line, math)) {
                pending.push(line);
                math = "";
                continue;
            }
            if (line.trim().startsWith("## ")) {
                flush();
                math = "";
                pending.push(line);
                continue;
            }
            const inside = noteStampAtLineStart(line);
            if (inside && !noteFormulaBody(inside.rest)) {
                flush();
                math = "";
                blocks.push({ kind: "stamp", stamp: inside });
                continue;
            }
            pending.push(line);
            continue;
        }
        const opened = noteDisplayMathOpens(line);
        if (opened) {
            pending.push(line);
            math = opened;
            continue;
        }
        const stamp = noteStampAtLineStart(line);
        if (!stamp) {
            pending.push(line);
            continue;
        }
        flush();
        blocks.push({ kind: "stamp", stamp });
    }
    flush();
    return blocks;
}

function NoteStampAnswer({ text, path, web, previewOpen, messageId, streaming, onBlocked }: {
    text: string;
    path: string;
    web: boolean;
    previewOpen: boolean;
    messageId: string;
    streaming: boolean;
    onBlocked: (sentence: string) => void;
}) {
    const blocks = noteAnswerBlocks(text);
    if (!blocks.some((block) => block.kind === "stamp")) {
        return (
            <MessageContentRenderer
                content={text}
                theme={lightTheme}
                isUser={false}
                isStreaming={streaming}
                messageId={messageId}
            />
        );
    }
    return (
        <div>
            {blocks.map((block, index) => {
                if (block.kind === "text") {
                    return (
                        <MessageContentRenderer
                            key={`${messageId}-${index}`}
                            content={block.text}
                            theme={lightTheme}
                            isUser={false}
                            isStreaming={false}
                            messageId={`${messageId}-${index}`}
                        />
                    );
                }
                const stamp = block.stamp;
                return (
                    <div key={`${messageId}-stamp-${index}`}>
                        <button
                            type="button"
                            data-testid="file-companion-seek"
                            data-seconds={String(stamp.seconds)}
                            onClick={() => {
                                if (web) {
                                    onBlocked("网页笔记不能跳回播放位置");
                                    return;
                                }
                                if (!previewOpen) {
                                    onBlocked("预览还没打开，无法跳转");
                                    return;
                                }
                                window.dispatchEvent(new CustomEvent("file-companion:seek", { detail: { path, seconds: stamp.seconds } }));
                            }}
                        >
                            {stamp.label}
                        </button>
                        {stamp.rest ? <span>{" "}{renderInlineMarkdown(stamp.rest, lightTheme)}</span> : null}
                    </div>
                );
            })}
        </div>
    );
}

function sameShareNotes(left: CompanionShareNote[], right: CompanionShareNote[]): boolean {
    if (left.length !== right.length) return false;
    for (let i = 0; i < left.length; i++) {
        if (left[i].id !== right[i].id || left[i].text !== right[i].text || left[i].author !== right[i].author) return false;
    }
    return true;
}

type SharePart = { token: string; mine: boolean; revision: number; notes: CompanionShareNote[] };
type ShareBind = { mine: boolean; notes: CompanionShareNote[]; parts: SharePart[] };
type PendingShareNote = { path: string; text: string };
const pendingShareNoteCap = 20;

// One open file can belong to more than one share. The page shows every
// share's notes. A later row must not replace an earlier share's newer list.
function shareBindFromParts(parts: SharePart[]): ShareBind {
    const notes: CompanionShareNote[] = [];
    const seen = new Set<string>();
    let mine = parts.length > 0;
    for (const part of parts) {
        if (!part.mine) mine = false;
        for (const note of part.notes) {
            if (seen.has(note.id)) continue;
            seen.add(note.id);
            notes.push(note);
        }
    }
    return { mine, notes, parts };
}

function sharePartsFromRows(prev: ShareBind | undefined, rows: { token: string; mine: boolean; revision: number; notes: CompanionShareNote[] }[]): SharePart[] {
    const parts: SharePart[] = [];
    const seen = new Set<string>();
    for (const row of rows) {
        if (seen.has(row.token)) continue;
        seen.add(row.token);
        const old = prev?.parts.find((part) => part.token === row.token);
        if (old && !companionNotesRevisionWins(old.revision, row.revision)) {
            parts.push(old);
            continue;
        }
        const revision = old ? Math.max(old.revision, row.revision) : row.revision;
        if (old && old.mine === row.mine && old.revision === revision && sameShareNotes(old.notes, row.notes)) {
            parts.push(old);
            continue;
        }
        parts.push({ token: row.token, mine: row.mine, revision, notes: row.notes });
    }
    return parts;
}

function shareBindToken(bind: ShareBind): string {
    return bind.parts[0]?.token ?? "";
}

function SharePasswordGlyph({ name }: { name: "lock" | "eye" | "eye-off" }) {
    const stroke = {
        fill: "none",
        stroke: "currentColor",
        strokeWidth: 1.8,
        strokeLinecap: "round" as const,
        strokeLinejoin: "round" as const,
    };
    return (
        <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true" focusable="false">
            {name === "lock" ? (
                <>
                    <rect {...stroke} x="6" y="10.5" width="12" height="8.8" rx="1.6" />
                    <path {...stroke} d="M8.6 10.5V8.1a3.4 3.4 0 0 1 6.8 0v2.4" />
                </>
            ) : null}
            {name === "eye" ? (
                <>
                    <path {...stroke} d="M3.2 12s3.4-5 8.8-5 8.8 5 8.8 5-3.4 5-8.8 5-8.8-5-8.8-5Z" />
                    <circle {...stroke} cx="12" cy="12" r="2.1" />
                </>
            ) : null}
            {name === "eye-off" ? (
                <>
                    <path {...stroke} d="M3.2 12s3.4-5 8.8-5 8.8 5 8.8 5-3.4 5-8.8 5-8.8-5-8.8-5Z" />
                    <circle {...stroke} cx="12" cy="12" r="2.1" />
                    <path {...stroke} d="M5.2 18.8 18.8 5.2" />
                </>
            ) : null}
        </svg>
    );
}

type CompanionArxivHit = {
    id: string;
    title: string;
    abstract: string;
    authors: string;
    published: string;
    categories: string[];
};

function companionArxivSearchStatus(error: string, query: string, submitted: string, searched: boolean, count: number, busy: boolean): string {
    if (error) return error;
    // The request has cleared the previous list. The idle sentence would
    // tell the reader to type a keyword they have already submitted.
    if (busy) return "正在检索…";
    // The count describes the list on screen. It stays true only while the
    // box still holds the query that produced that list; a keystroke must
    // not leave "找到 N 篇" attached to a different keyword. What to try
    // next lives in the empty panel, so this line only states the result.
    const current = searched && query.trim() === submitted;
    if (current && count === 0) return "检索完成，没有结果";
    if (current && count > 0) return `找到 ${count} 篇，点击任一条下载并打开`;
    if (count > 0) return "关键字已修改，请重新搜索";
    return "输入关键字或 arXiv 编号，检索公开预印本";
}

function companionArxivHits(raw: unknown): CompanionArxivHit[] {
    if (!Array.isArray(raw)) return [];
    const hits: CompanionArxivHit[] = [];
    for (const item of raw) {
        if (!item || typeof item !== "object") continue;
        const row = item as Record<string, unknown>;
        const id = String(row.id ?? row.ID ?? "").trim();
        const title = String(row.title ?? row.Title ?? "").trim();
        const abstract = String(row.abstract ?? row.Abstract ?? "").trim();
        const authors = String(row.authors ?? row.Authors ?? "").trim();
        const published = String(row.published ?? row.Published ?? "").trim();
        const rawCategories = row.categories ?? row.Categories;
        const categories = Array.isArray(rawCategories)
            ? rawCategories.map((name) => String(name ?? "").trim()).filter(Boolean)
            : [];
        if (!id || !title) continue;
        hits.push({ id, title, abstract, authors, published, categories });
    }
    return hits;
}

export default function FileCompanionWindow({ askOnOpen = true }: { askOnOpen?: boolean } = {}) {
    const [tabs, setTabs] = useState<CompanionTab[]>([]);
    const [active, setActive] = useState("");
    const [tabEnds, setTabEnds] = useState<CompanionTabEnds>({ overflow: false, canLeft: false, canRight: false });
    const [dropMessage, setDropMessage] = useState("");
    // The 打开 menu: one button, two ways in. A local file goes straight to the
    // system dialog; an arXiv reference is fetched and opened as a tab.
    // The same window is shipped under every OEM brand, so the product name in
    // the title has to come from the backend instead of a literal. MaClaw is
    // the default so a failed call leaves the label exactly as it was.
    const [brandName, setBrandName] = useState("MaClaw");
    const [openMenu, setOpenMenu] = useState(false);
    // 独立界面 state: the window switches between the reading view, the
    // 文档管理 screen, and the 阅读群组 screen. 分享 opens over either.
    const [view, setView] = useState<CompanionView>("read");
    const [shareDialogPath, setShareDialogPath] = useState("");
    const [importDialogOpen, setImportDialogOpen] = useState(false);
    // A link pasted into 导入分享, or the one carried by a cloud row that has
    // no local copy yet. The dialog opens on it either way.
    const [importDialogLink, setImportDialogLink] = useState("");
    const [readingFull, setReadingFull] = useState(false);
    const [arxivInput, setArxivInput] = useState("");
    const [arxivBusy, setArxivBusy] = useState(false);
    const [arxivMessage, setArxivMessage] = useState("");
    const [arxivSearchOpen, setArxivSearchOpen] = useState(false);
    const [arxivSearchQuery, setArxivSearchQuery] = useState("");
    const [arxivSearchBusy, setArxivSearchBusy] = useState(false);
    const [arxivSearchError, setArxivSearchError] = useState("");
    const [arxivSearchHits, setArxivSearchHits] = useState<CompanionArxivHit[]>([]);
    const [arxivSearched, setArxivSearched] = useState(false);
    const [arxivSearchFor, setArxivSearchFor] = useState("");
    const [arxivOpeningId, setArxivOpeningId] = useState("");
    const [sharePasswordLink, setSharePasswordLink] = useState("");
    const [sharePasswordValue, setSharePasswordValue] = useState("");
    const [sharePasswordError, setSharePasswordError] = useState("");
    const [sharePasswordVisible, setSharePasswordVisible] = useState(false);
    const arxivBusyRef = useRef(false);
    const sharePasswordRef = useRef("");
    sharePasswordRef.current = sharePasswordLink;
    // Search is a modal over this menu. Escape and outside clicks belong to
    // it, the same way the share-password box does.
    const arxivSearchRef = useRef(false);
    arxivSearchRef.current = arxivSearchOpen;
    const arxivSearchBusyRef = useRef(false);
    const arxivSearchGenRef = useRef(0);
    const arxivSearchInputRef = useRef<HTMLInputElement | null>(null);
    const arxivSearchButtonRef = useRef<HTMLButtonElement | null>(null);
    const arxivSearchDialogRef = useRef<HTMLDivElement | null>(null);
    const closeArxivSearchRef = useRef<() => void>(() => undefined);
    const sharePasswordInputRef = useRef<HTMLInputElement | null>(null);
    const sharePasswordDialogRef = useRef<HTMLDivElement | null>(null);
    const closeSharePasswordRef = useRef<() => void>(() => undefined);
    const openMenuRef = useRef(false);
    openMenuRef.current = openMenu;
    // Escape ownership: a full screen (文档管理 / 阅读群组) or a popup
    // (分享 / 导入) takes Escape before a fullscreen exit may run.
    const overlayRef = useRef(false);
    overlayRef.current = view !== "read" || Boolean(shareDialogPath) || importDialogOpen || arxivSearchOpen;
    // The share dialog names the project it is sharing. The row that opened it
    // is the truth; the open tab is the fallback for the title-bar button.
    const shareDialogName = useMemo(() => {
        if (!shareDialogPath) return "";
        const tab = tabs.find((item) => companionSharePathKey(item.path) === companionSharePathKey(shareDialogPath));
        if (tab) return tab.name;
        const base = shareDialogPath.replace(/\\/g, "/").split("/").pop() ?? "";
        return base;
    }, [shareDialogPath, tabs]);
    // The destination panel owns Escape the same way 打开 does. The ref is
    // assigned once the panel flag is known, later in this render.
    const chatExportDestOpenRef = useRef(false);
    // The size written back by 恢复原始大小 is the window before 全屏, not the
    // launch size. Frameless Unfullscreen can keep the monitor rect, so the
    // captured geometry is applied again after the OS restore.
    const readingFrameRef = useRef(createWindowMaximizeRestoreSession({
        getSize: WindowGetSize,
        getPosition: WindowGetPosition,
        setSize: WindowSetSize,
        setPosition: (x, y) => { void SetMainWindowScreenPosition(x, y); },
    }));
    const readingFullRef = useRef(false);
    const readingToggleRef = useRef(false);
    const openWrapRef = useRef<HTMLDivElement | null>(null);
    const chatExportWrapRef = useRef<HTMLDivElement | null>(null);
    const chatExportButtonRef = useRef<HTMLButtonElement | null>(null);
    const arxivInputRef = useRef<HTMLInputElement | null>(null);
    const chooseButtonRef = useRef<HTMLButtonElement | null>(null);
    const tabsRef = useRef(tabs);
    tabsRef.current = tabs;
    const activeRef = useRef(active);
    activeRef.current = active;
    // A cleared chat bumps this generation. A synced transcript does the same
    // for that file, so the reply already on the way cannot land on top of it.
    const chatEpochRef = useRef(new Map<string, number>());
    const shareByPathRef = useRef(new Map<string, ShareBind>());
    // A line sent before the library list returns does not yet know whether
    // this file is the host's. It waits here and is posted only if the reader
    // is not the host.
    const pendingShareNotesRef = useRef(new Map<string, PendingShareNote[]>());
    const publishShareNoteRef = useRef<(path: string, text: string) => Promise<void>>(async () => undefined);
    const flushPendingShareNotesRef = useRef<() => void>(() => undefined);
    const applyShareNotesRef = useRef<(listed: unknown) => void>(() => undefined);
    const applyLibraryListRef = useRef<(listed: unknown) => void>(() => undefined);
    const clearShareNotes = (keys: Set<string> | null) => {
        setTabs((current) => {
            let changed = false;
            const next = current.map((tab) => {
                const key = companionSharePathKey(tab.path);
                if (keys && !keys.has(key)) return tab;
                if (!tab.shareToken && !tab.shareMine && (!tab.notes || tab.notes.length === 0)) return tab;
                changed = true;
                return { ...tab, shareToken: "", shareMine: false, notes: [] };
            });
            return changed ? next : current;
        });
    };
    applyShareNotesRef.current = (listed) => {
        const rows = companionShareNotes(listed);
        if (rows.length === 0) {
            // No share rows. Notes for a share the hub no longer has should
            // leave the page. A line sent before any list returns stays queued.
            if (shareByPathRef.current.size > 0) {
                shareByPathRef.current.clear();
                clearShareNotes(null);
            }
            return;
        }
        const grouped = new Map<string, typeof rows>();
        for (const row of rows) {
            const key = companionSharePathKey(row.path);
            const group = grouped.get(key);
            if (group) group.push(row);
            else grouped.set(key, [row]);
        }
        const removed = new Set<string>();
        for (const key of shareByPathRef.current.keys()) {
            if (!grouped.has(key)) {
                shareByPathRef.current.delete(key);
                removed.add(key);
            }
        }
        for (const key of pendingShareNotesRef.current.keys()) {
            if (!grouped.has(key)) pendingShareNotesRef.current.delete(key);
        }
        const accepted = new Map<string, ShareBind>();
        for (const [key, group] of grouped) {
            const prev = shareByPathRef.current.get(key);
            const stored = shareBindFromParts(sharePartsFromRows(prev, group));
            if (prev && prev.mine === stored.mine && sameShareNotes(prev.notes, stored.notes) && prev.parts.length === stored.parts.length && prev.parts.every((part, index) => part === stored.parts[index])) {
                continue;
            }
            shareByPathRef.current.set(key, stored);
            accepted.set(key, stored);
        }
        if (accepted.size !== 0 || removed.size !== 0) {
            setTabs((current) => {
                let changed = false;
                const next = current.map((tab) => {
                    const key = companionSharePathKey(tab.path);
                    if (removed.has(key)) {
                        if (!tab.shareToken && !tab.shareMine && (!tab.notes || tab.notes.length === 0)) return tab;
                        changed = true;
                        return { ...tab, shareToken: "", shareMine: false, notes: [] };
                    }
                    const row = accepted.get(key);
                    if (!row) return tab;
                    const token = shareBindToken(row);
                    if (tab.shareToken === token && tab.shareMine === row.mine && tab.notes === row.notes) return tab;
                    changed = true;
                    return { ...tab, shareToken: token, shareMine: row.mine, notes: row.notes };
                });
                return changed ? next : current;
            });
        }
        flushPendingShareNotesRef.current();
    };
    const applyShareUpdateRef = useRef<(update: CompanionShareUpdate) => void>(() => undefined);
    applyShareUpdateRef.current = (update) => {
        if (!update.path || !update.project) return;
        try {
            savePdfMarks(update.path, update.highlights ? JSON.parse(update.highlights) : []);
        } catch {
            savePdfMarks(update.path, []);
        }
        if (!update.replaceMessages) return;
        const updateKey = companionSharePathKey(update.path);
        const sessionId = tabsRef.current.find((tab) => companionSharePathKey(tab.path) === updateKey)?.sessionId ?? "";
        if (sessionId) {
            chatEpochRef.current.set(sessionId, (chatEpochRef.current.get(sessionId) ?? 0) + 1);
        }
        const messages = messagesFromDoc({ messages: update.messages } as CompanionDoc);
        setTabs((current) => current.map((tab) => companionSharePathKey(tab.path) === updateKey ? { ...tab, messages, status: "", previewEpoch: tab.previewEpoch + 1 } : tab));
    };
    // The window poll and 文档管理 both read the library. One apply keeps notes,
    // the sharer's transcript, and a removed copy on the same path key.
    applyLibraryListRef.current = (listed) => {
        const synced = companionLibrarySync(listed);
        for (const update of synced.updates) applyShareUpdateRef.current(update);
        applyShareNotesRef.current(listed);
        if (synced.removed.length === 0) return;
        const gone = new Set(synced.removed.map(companionSharePathKey));
        setTabs((current) => {
            const left = current.filter((tab) => !gone.has(companionSharePathKey(tab.path)));
            setActive((active) => gone.has(companionSharePathKey(active)) ? (left[0]?.path ?? "") : active);
            return left;
        });
    };
    // The library panel is not the only reader. A removed member, or a newer
    // revision, has to land while the document is already open.
    useEffect(() => {
        let stopped = false;
        const refresh = () => {
            void ListFileCompanionLibrary().then((listed) => {
                if (stopped) return;
                applyLibraryListRef.current(listed);
            }).catch(() => undefined);
        };
        refresh();
        const timer = window.setInterval(refresh, 60_000);
        window.addEventListener("focus", refresh);
        return () => {
            stopped = true;
            window.clearInterval(timer);
            window.removeEventListener("focus", refresh);
        };
    }, []);
    const openTabPaths = tabs.map((tab) => tab.path).join("\n");
    // The library list can finish before the file becomes a tab. The binding
    // stays on the ref, and this copies it onto the tab once that path is open.
    useEffect(() => {
        setTabs((current) => {
            let changed = false;
            const next = current.map((tab) => {
                const row = shareByPathRef.current.get(companionSharePathKey(tab.path));
                if (!row) return tab;
                const token = shareBindToken(row);
                if (tab.shareToken === token && tab.shareMine === row.mine && tab.notes === row.notes) return tab;
                changed = true;
                return { ...tab, shareToken: token, shareMine: row.mine, notes: row.notes };
            });
            return changed ? next : current;
        });
    }, [openTabPaths]);
    // Each open file is its own agent. This set is which of those turns are
    // still in flight. It is not a window-wide lock.
    const sendingPathsRef = useRef(new Set<string>());
    const noteBusyPathsRef = useRef(new Set<string>());
    const noteRequestPathRef = useRef(new Map<string, string>());
    // Set before WindowHide, and when the document itself is hidden (Alt+F4,
    // the title-bar control, minimize). A hide collapses the player slot and
    // can drop fullscreen. Neither one is the user leaving the video, so a
    // note keeps running. A positive slot while this is set does not clear
    // it: the click happens before the document is hidden, and placing that
    // box would let the collapse unload the page. windowRevealRef is the
    // show that arrived before the slot had a pixel. The visibility listener
    // arms it even when no web player is mounted; that layout effect is the
    // only other code that hears the show, and it is absent until a page
    // opens. The next real slot places the player. A focus event does not
    // clear this.
    // hostWindowShownRef is the Win32 fact from file-companion:window-shown.
    // null means that event has not arrived, and visibilityState still counts.
    // A reported show outranks a document that is still hidden, and a reported
    // hide outranks a document that is already visible.
    const windowHiddenRef = useRef(false);
    const windowRevealRef = useRef(false);
    const hostWindowShownRef = useRef<boolean | null>(null);
    const companionShownPlaceRef = useRef<() => void>(() => undefined);
    const requestSerialRef = useRef(0);
    const askOnOpenRef = useRef(askOnOpen);
    askOnOpenRef.current = askOnOpen;
    // Paths already given the opening question, or waiting for it. Cleared when
    // that tab closes, so a later empty open can ask again. /clear does not.
    const introArmedRef = useRef(new Set<string>());
    const introQueueRef = useRef<string[]>([]);
    const drainIntroRef = useRef<() => void>(() => undefined);
    // One counter for every companion turn. Date.now() alone collides when
    // two tabs send in the same millisecond, and the epoch map is keyed by
    // that id, so the later turn would steal the earlier one's events.
    const nextCompanionRequestID = (kind = "") => {
        requestSerialRef.current += 1;
        const prefix = kind ? `file-companion-${kind}` : "file-companion";
        return `${prefix}-${Date.now()}-${requestSerialRef.current}`;
    };
    const armIntro = (path: string) => {
        if (!askOnOpenRef.current || !path || introArmedRef.current.has(path)) return;
        introArmedRef.current.add(path);
        introQueueRef.current.push(path);
    };
    const forgetIntro = (path: string) => {
        introArmedRef.current.delete(path);
        if (!introQueueRef.current.includes(path)) return;
        introQueueRef.current = introQueueRef.current.filter((item) => item !== path);
    };
    const requestEpochRef = useRef(new Map<string, number>());
    const paperRequestIds = useRef(new Set<string>());
    const videoNoteRequestIds = useRef(new Set<string>());
    const webFullscreenRef = useRef(false);
    const domFullRef = useRef(false);
    const winFullRef = useRef(false);
    const noteBusyRef = useRef("");
    const noteBusyRequestRef = useRef("");
    const webCaptureEpochRef = useRef(0);
    const noteEpochRef = useRef(0);
    // While a web note is recording, the preview stays on that player until
    // FileCompanionWebPlayerLeft fulfills. CapturePreview reads whatever is
    // already composited. The leave call is asynchronous, so changing the
    // iframe first lets the next page into the note. A rejection has not
    // retired the watch, so the picture stays. It drops when the note call
    // returns, because capture has finished. The failed call is not retried.
    const [pinnedPlayerPath, setPinnedPlayerPath] = useState("");
    const releasedPlayerRef = useRef("");
    const leaveSentRef = useRef("");
    const leaveTicketRef = useRef(0);
    const setNoteBusyRef = useRef<(requestID: string) => void>(() => undefined);
    const webFrameRef = useRef<HTMLElement | null>(null);
    // pending until the process says it can host a top-level page. An iframe
    // painted in that gap would load the video under wails.localhost.
    const [playerHost, setPlayerHost] = useState<"pending" | "native" | "iframe">("pending");
    const webCaptureFields = () => {
        const frame = webFrameRef.current;
        const ratio = window.devicePixelRatio > 0 ? window.devicePixelRatio : 1;
        if (!frame || typeof frame.getBoundingClientRect !== "function") {
            return { rectX: 0, rectY: 0, rectW: 0, rectH: 0 };
        }
        const box = frame.getBoundingClientRect();
        return {
            rectX: box.left * ratio,
            rectY: box.top * ratio,
            rectW: box.width * ratio,
            rectH: box.height * ratio,
        };
    };
    const acceptedHash = useRef(new Map<string, string>());
    const saveTail = useRef(new Map<string, Promise<void>>());
    const openEpoch = useRef(new Map<string, number>());
    const openSerial = useRef(0);
    const saveEpoch = useRef(new Map<string, number>());
    const bufferEpoch = useRef(new Map<string, number>());
    const saveTabRef = useRef<(tab: CompanionTab, hash: string) => Promise<"saved" | "conflict" | "error" | "skipped">>(async () => "skipped");
    const [chooseBusy, setChooseBusy] = useState(false);
    const choosingRef = useRef(false);
    const chooseFilesRef = useRef<() => Promise<void>>(async () => undefined);
    const bootSettledRef = useRef(false);
    const [bootSettled, setBootSettled] = useState(false);
    const opensInFlight = useRef(0);
    const tabCommittedRef = useRef(false);
    const bootPromiseRef = useRef<Promise<unknown> | null>(null);
    const activeTabEl = useRef<HTMLButtonElement | null>(null);
    const tabsScrollRef = useRef<HTMLDivElement | null>(null);
    const tabNudgedRef = useRef(false);
    // A render with no tab and no open still running is actually empty.
    // An open that already called setTabs keeps the flag until that render.
    if (tabs.length === 0 && opensInFlight.current === 0) {
        tabCommittedRef.current = false;
    }

    const openPath = async (path: string, fromDrop: boolean, keepDirty = false, refresh = 0, focus = false) => {
        const trimmed = path.trim();
        if (!trimmed) return;
        opensInFlight.current += 1;
        try {
            const serial = openSerial.current + 1;
            openSerial.current = serial;
            openEpoch.current.set(trimmed, serial);
            const epochsAtStart = new Map(saveEpoch.current);
            let doc: CompanionDoc = {};
            try {
                doc = await FileCompanionOpen(trimmed) as CompanionDoc;
            } catch (err) {
                doc = { path: trimmed, error: bindingErrorText(err) || "打不开这个文件" };
            }
            if (openEpoch.current.get(trimmed) !== serial) return;
            const error = field(doc, "error", "Error", "");
            if (fromDrop && isDirectoryError(error)) {
                setDropMessage("不支持打开文件夹");
                return;
            }
            const next = tabFromDoc(doc, trimmed);
            const imported = doc.imported === true || doc.Imported === true;
            const importedMarks = highlightsFromDoc(doc);
            if (importedMarks && next.path && !next.error) savePdfMarks(next.path, importedMarks);
            const resolved = next.path || trimmed;
            if (refresh < 1 && (saveEpoch.current.get(resolved) ?? 0) !== (epochsAtStart.get(resolved) ?? 0)) {
                void openPath(resolved, fromDrop, keepDirty, 1, focus);
                return;
            }
            if (next.path !== trimmed) {
                const current = openEpoch.current.get(next.path) ?? 0;
                if (current > serial) return;
                openEpoch.current.set(next.path, serial);
            }
            const existing = tabsRef.current.find((tab) => tab.path === next.path);
            if (!(keepDirty && existing?.dirty)) {
                acceptedHash.current.set(next.path, next.loadedHash);
            }
            setDropMessage("");
            tabCommittedRef.current = true;
            setTabs((current) => {
                const previous = current.find((tab) => tab.path === next.path);
                // A brand-new tab with an empty transcript asks once. Reloads
                // take the branch below and keep the chat. StrictMode may run
                // this updater twice; armIntro records the path only once.
                if (!previous) {
                    if (!next.error && next.messages.length === 0) armIntro(next.path);
                    return [...current, next];
                }
                return current.map((tab) => {
                    if (tab.path !== next.path) return tab;
                    if (keepDirty && tab.dirty) {
                        return { ...tab, conflict: true, diskHash: next.loadedHash, confirmOverwrite: false };
                    }
                    if (imported) {
                        return {
                            ...next,
                            draft: tab.draft,
                            previewEpoch: tab.previewEpoch + 1,
                            htmlPreview: tab.htmlPreview,
                        };
                    }
                    const keepSelection = sameSelectionAnchor(next.content, tab.selection, tab.selectionStart);
                    return {
                        ...next,
                        messages: tab.messages,
                        draft: tab.draft,
                        selection: keepSelection ? tab.selection : "",
                        selectionStart: keepSelection ? tab.selectionStart : -1,
                        previewEpoch: tab.previewEpoch,
                        htmlPreview: tab.htmlPreview,
                    };
                });
            });
            setActive((current) => (fromDrop || focus || !current ? next.path : current));
        } finally {
            opensInFlight.current -= 1;
        }
    };

    // Kept out of the boot effect on purpose: the title label is cosmetic and
    // must not take part in the boot-settled bookkeeping that decides whether
    // an empty window opens the file dialog.
    useEffect(() => {
        let live = true;
        void Promise.resolve(FileCompanionPlayerSupported()).then(
            (ok) => { if (live) setPlayerHost(ok ? "native" : "iframe"); },
            () => { if (live) setPlayerHost("iframe"); },
        );
        return () => { live = false; };
    }, []);

    useEffect(() => {
        let cancelled = false;
        void Promise.resolve(GetBrandInfo())
            .then((info) => {
                if (cancelled) return;
                const name = typeof info?.displayName === "string" ? info.displayName.trim() : "";
                if (name) setBrandName(name);
            })
            .catch(() => undefined);
        return () => { cancelled = true; };
    }, []);

    useEffect(() => {
        let cancelled = false;
        const runtime = runtimeAPI();
        const onOpen = (payload: unknown) => {
            const list = Array.isArray(payload) ? payload.filter((item): item is string => typeof item === "string") : [];
            list.forEach((path) => { void openPath(path, false, false, 0, true); });
        };
        const onReload = (payload: unknown) => {
            const body = eventBody(payload);
            if (!body) return;
            const ext = eventString(body, "ext", "Ext").toLowerCase();
            if (ext !== ".xlsx" && ext !== ".pptx") return;
            const path = eventString(body, "path", "Path");
            if (!path) return;
            setTabs((current) => current.map((tab) => tab.path === path ? { ...tab, previewEpoch: tab.previewEpoch + 1 } : tab));
        };
        const parsedSession = (payload: unknown) => {
            const body = eventBody(payload);
            if (!body) return null;
            const sessionKey = eventString(body, "session_key", "SessionKey");
            if (!sessionKey.startsWith("file-companion:")) return null;
            const sessionId = sessionKey.slice("file-companion:".length);
            if (!sessionId) return null;
            return { body, sessionId, requestID: eventString(body, "request_id", "RequestID") || sessionId };
        };
        const onProgress = (payload: unknown) => {
            const parsed = parsedSession(payload);
            if (!parsed) return;
            if (!companionEventCurrent(chatEpochRef.current, requestEpochRef.current, parsed.sessionId, parsed.requestID)) return;
            const text = eventString(parsed.body, "text", "Text");
            if (!text || text === "__heartbeat__") return;
            setTabs((current) => current.map((tab) => {
                if (tab.sessionId !== parsed.sessionId) return tab;
                const clock = noteClockFor(tab.status, tabNoteKind(tab), tab.noteStartedAt, tab.phaseStartedAt, text, Date.now());
                return { ...tab, status: text, noteStartedAt: clock.noteStartedAt, phaseStartedAt: clock.phaseStartedAt };
            }));
        };
        const onToken = (payload: unknown) => {
            const parsed = parsedSession(payload);
            if (!parsed) return;
            if (!companionEventCurrent(chatEpochRef.current, requestEpochRef.current, parsed.sessionId, parsed.requestID)) return;
            const delta = eventString(parsed.body, "text", "Text");
            const part = splitCompanionStreamDelta(delta);
            if (!part.text && !part.reasoning) return;
            setTabs((current) => current.map((tab) => {
                if (tab.sessionId !== parsed.sessionId) return tab;
                const id = `${parsed.requestID}:assistant`;
                const messages = tab.messages.slice();
                const index = messages.findIndex((item) => item.id === id);
                if (index >= 0) {
                    const currentMessage = messages[index];
                    messages[index] = {
                        ...currentMessage,
                        text: part.text ? currentMessage.text + part.text : currentMessage.text,
                        reasoning: part.reasoning ? `${currentMessage.reasoning || ""}${part.reasoning}` : currentMessage.reasoning,
                    };
                } else {
                    messages.push({ id, role: "assistant", text: part.text, reasoning: part.reasoning });
                }
                return { ...tab, messages };
            }));
        };
        const onResponse = (payload: unknown) => {
            const parsed = parsedSession(payload);
            if (!parsed) return;
            if (!companionEventCurrent(chatEpochRef.current, requestEpochRef.current, parsed.sessionId, parsed.requestID)) return;
            const text = eventString(parsed.body, "text", "Text");
            const reasoning = eventString(parsed.body, "reasoning", "Reasoning");
            const errText = eventString(parsed.body, "error", "Error");
            const note = errText ? turnErrorText(errText) : "";
            const shown = text && note && !text.includes(note) ? `${text}\n\n${note}` : (text || note);
            const localPath = eventString(parsed.body, "local_file_path", "LocalFilePath");
            // Drawings from the open file travel with the reply, not inside its
            // text, so a cited chart is shown instead of only described.
            const figures = errText ? [] : figuresFromValue(parsed.body.file_companion_figures ?? parsed.body.FileCompanionFigures);
            const noteCardPath = eventString(parsed.body, "note_card_path", "NoteCardPath");
            const webNote = parsed.body.web_note === true || parsed.body.WebNote === true;
            const paper = paperRequestIds.current.has(parsed.requestID);
            const videoNote = videoNoteRequestIds.current.has(parsed.requestID) || Boolean(noteCardPath);
            if (paper) paperRequestIds.current.delete(parsed.requestID);
            if (videoNote) {
                videoNoteRequestIds.current.delete(parsed.requestID);
                // The card still belongs to this reply. Only this attempt's
                // path is cleared. Another tab's note keeps its own agent.
                if (noteBusyRequestRef.current === parsed.requestID) {
                    noteBusyRequestRef.current = "";
                }
                setNoteBusyRef.current(parsed.requestID);
            }
            const resultPath = paper && !videoNote && !errText && localPath ? localPath : "";
            const matched = tabsRef.current.find((tab) => tab.sessionId === parsed.sessionId);
            const matchedName = matched?.name.toLowerCase() ?? "";
            const reloadPath = !paper && !videoNote && !errText && localPath && matched && matched.kind !== "web" && !matchedName.endsWith(".xlsx") && !matchedName.endsWith(".pptx")
                ? matched.path
                : "";
            setTabs((current) => current.map((tab) => {
                if (tab.sessionId !== parsed.sessionId) return tab;
                const id = `${parsed.requestID}:assistant`;
                const messages = tab.messages.slice();
                if (shown || reasoning) {
                    const index = messages.findIndex((item) => item.id === id);
                    const streamed = index >= 0 ? messages[index] : undefined;
                    const finished = finishCompanionReply(
                        resultPath ? "" : (streamed?.text || ""),
                        streamed?.reasoning || "",
                        resultPath ? "已生成论文解读" : shown,
                        reasoning,
                    );
                    const ownedCard = noteCardPath || streamed?.noteCardPath || "";
                    // One note file per session. The turn that carries its path
                    // owns the card; an earlier bubble would open the PDF this
                    // turn just overwrote.
                    if (ownedCard) {
                        for (let i = 0; i < messages.length; i++) {
                            if (messages[i].id === id || !messages[i].noteCardPath) continue;
                            messages[i] = { ...messages[i], noteCardPath: undefined };
                        }
                    }
                    const next = {
                        id,
                        role: "assistant" as const,
                        text: resultPath ? "已生成论文解读" : (shown ? finished.text : (streamed?.text || "")),
                        reasoning: finished.reasoning,
                        resultPath: resultPath || streamed?.resultPath,
                        noteCardPath: ownedCard || undefined,
                        webNote: webNote || streamed?.webNote || matched?.kind === "web",
                        figures: figures.length > 0 ? figures : streamed?.figures,
                    };
                    if (index >= 0) messages[index] = { ...messages[index], ...next };
                    else messages.push(next);
                }
                const name = tab.name.toLowerCase();
                if (!paper && (name.endsWith(".xlsx") || name.endsWith(".pptx")) && !localPath && text && !errText) {
                    const payloadJSON = officePayloadJSON(text, tab.name);
                    if (payloadJSON) void FileCompanionReplaceOffice(tab.path, payloadJSON).catch(() => undefined);
                }
                return { ...tab, messages, status: "" };
            }));
            if (reloadPath) void openPath(reloadPath, false, true);
        };
        const windowIsEmpty = () => opensInFlight.current === 0 && !tabCommittedRef.current && tabsRef.current.length === 0;
        const showOpenPanel = () => {
            // Empty history opens this panel. The system file dialog is the
            // 打开文档或导入项目 row inside it, not this click.
            setOpenMenu(true);
        };
        const onShow = () => {
            // Boot paths are applied after this listener exists. A show in that
            // gap must not treat "tabs not rendered yet" as an empty window.
            if (!bootSettledRef.current || !windowIsEmpty()) return;
            showOpenPanel();
        };
        runtime?.EventsOn?.("file-companion:open", onOpen);
        runtime?.EventsOn?.("file-companion:show", onShow);
        runtime?.EventsOn?.("file-companion:preview-reload", onReload);
        runtime?.EventsOn?.("ai-assistant-progress", onProgress);
        runtime?.EventsOn?.("ai-assistant-token", onToken);
        const syncWebFull = (windowOnScreen = false, hideExit = false) => {
            const frame = webFrameRef.current;
            if (frame == null) {
                // The Windows flag belongs to the page that had the player.
                // A leftover true would mark the next note fullscreen after
                // that player is gone.
                domFullRef.current = false;
                winFullRef.current = false;
            }
            const on = frame != null && (domFullRef.current || winFullRef.current);
            const was = webFullscreenRef.current;
            webFullscreenRef.current = on;
            // A hidden window drops fullscreen on its own. That is not the
            // reader leaving the video. windowOnScreen is the fullscreen
            // event's own Win32 fact: the host has put the window back, so
            // this exit stops the note even when visibilityState is still
            // hidden and the latch is only waiting for a slot. A DOM
            // fullscreen change has no such fact. Host false is a hide the
            // page has already recorded.
            const hostHidden = hostWindowShownRef.current === false && !windowOnScreen;
            const latched = windowHiddenRef.current && !windowOnScreen;
            const visibilityBlocks = document.visibilityState === "hidden" && !windowOnScreen;
            // hideExit is a fullscreen drop caused by hiding the window.
            // The callback can arrive after the window is shown again.
            // windowVisible is still the real shown state, so this must not
            // be treated as the reader leaving the video.
            if (was && !on && noteBusyRef.current && !latched && !hostHidden && !visibilityBlocks && !hideExit) {
                // The recording tab is the one that started the note. Switching
                // files unmounts the iframe and exits fullscreen after active
                // already points at the other file.
                const busyPath = noteBusyRef.current;
                const tab = tabsRef.current.find((item) => item.kind === "web" && item.path === busyPath);
                if (tab) void CancelFileCompanionWebTurn(tab.sessionId, noteEpochRef.current);
            }
        };
        const onDomFull = () => {
            const frame = webFrameRef.current;
            // The iframe is gone after a tab switch. Both sides are then null,
            // and null === null would keep the note recording.
            const doc = document as Document & { webkitFullscreenElement?: Element | null };
            const active = document.fullscreenElement || doc.webkitFullscreenElement || null;
            domFullRef.current = frame != null && active === frame;
            syncWebFull();
        };
        const onWinFull = (payload: unknown) => {
            // Wails delivers Notify data as a one-element array. The test
            // runtime emits the object itself. Both have to set the flag.
            const raw = Array.isArray(payload) ? payload[0] : payload;
            const body = eventBody(raw);
            // The host sets windowVisible from the window procedure, before
            // ShowWindow or a minimize returns. visibilityState is still
            // "visible" at that moment. A missing field is a real exit only
            // while the document is visible. windowVisible true means this
            // exit is on screen now, so the latch must not swallow it.
            // Clearing the latch here is what made the next place send a
            // zero box, and that unloads the page once the window is shown.
            // The reveal stays armed until the slot covers a pixel.
            const hostShown = Boolean(body && (body.windowVisible === true || body.WindowVisible === true));
            // The exit belongs to a hide. It does not itself say the window
            // is hidden: windowVisible does. A late callback after the show
            // keeps the window visible, and latching it here would leave the
            // player off the slot.
            const hideExit = Boolean(body && (body.hideExit === true || body.HideExit === true));
            const wasLatched = windowHiddenRef.current;
            if (body && (body.windowVisible === false || body.WindowVisible === false)) {
                windowHiddenRef.current = true;
                windowRevealRef.current = false;
            } else if (hostShown) {
                hostWindowShownRef.current = true;
                if (wasLatched) windowRevealRef.current = true;
            }
            winFullRef.current = Boolean(body && (body.fullscreen === true || body.Fullscreen === true));
            syncWebFull(hostShown, hideExit);
            if (hostShown && wasLatched) companionShownPlaceRef.current();
        };
        const onCompanionVisibility = () => {
            if (document.visibilityState === "hidden") {
                // The host already said this window is on screen. A late
                // hidden visibility is the hide catching up, not a new one.
                if (hostWindowShownRef.current === true) return;
                windowHiddenRef.current = true;
                windowRevealRef.current = false;
                return;
            }
            if (hostWindowShownRef.current === false) return;
            // The player effect is mounted only while a web page is open.
            // A show while it is absent must still arm the next place.
            // Clearing the hidden flag here has no slot to measure, so a
            // later empty box would unload the page.
            if (!windowHiddenRef.current) return;
            windowRevealRef.current = true;
            companionShownPlaceRef.current();
        };
        const onHostWindow = (payload: unknown) => {
            const raw = Array.isArray(payload) ? payload[0] : payload;
            const body = eventBody(raw);
            if (!body) return;
            const shown = body.shown === true || body.Shown === true;
            const hidden = body.shown === false || body.Shown === false;
            if (!shown && !hidden) return;
            hostWindowShownRef.current = shown;
            if (!shown) {
                windowHiddenRef.current = true;
                windowRevealRef.current = false;
                return;
            }
            if (!windowHiddenRef.current) return;
            windowRevealRef.current = true;
            companionShownPlaceRef.current();
        };
        document.addEventListener("fullscreenchange", onDomFull);
        document.addEventListener("webkitfullscreenchange", onDomFull);
        document.addEventListener("visibilitychange", onCompanionVisibility);
        runtime?.EventsOn?.("file-companion:webview-fullscreen", onWinFull);
        runtime?.EventsOn?.("file-companion:window-shown", onHostWindow);
        runtime?.EventsOn?.("ai-assistant-response", onResponse);
        runtime?.OnFileDrop?.((_x, _y, paths) => {
            paths.forEach((path) => { void openPath(path, true); });
        }, true);
        // One read per window. A development remount shares it, so the first
        // call cannot drain the list and leave the second call empty.
        if (!bootPromiseRef.current) {
            bootPromiseRef.current = Promise.resolve(GetFileCompanionBoot());
        }
        const bootPromise = bootPromiseRef.current;
        const readyPaths = (value: unknown): string[] => companionDocumentPaths(value);
        void (async () => {
            const askIfEmpty = () => {
                if (cancelled || !windowIsEmpty()) return;
                showOpenPanel();
            };
            // Ready returns paths queued after the boot read. Applying them
            // here, before the empty check, keeps the open panel from covering
            // a file the event would only deliver later.
            const finishBoot = async () => {
                const delivered = readyPaths(await Promise.resolve(FileCompanionUIReady()).catch(() => undefined));
                for (const path of delivered) {
                    if (cancelled) return;
                    await openPath(path, false, false, 0, true);
                }
                if (cancelled) return;
                bootSettledRef.current = true;
                setBootSettled(true);
                askIfEmpty();
            };
            try {
                const boot = await bootPromise;
                if (cancelled) return;
                // A launch with no documents is "show the companion". The files
                // it had open last time are the session. The 打开 panel, not
                // the system file dialog, runs only when that session is empty.
                let paths = bootPaths(boot as { paths?: unknown; Paths?: unknown });
                if (paths.length === 0) {
                    paths = readyPaths(await Promise.resolve(FileCompanionResumePaths()).catch(() => undefined));
                }
                for (const path of paths) {
                    if (cancelled) return;
                    await openPath(path, false);
                }
                if (cancelled) return;
                await finishBoot();
            } catch {
                if (cancelled) return;
                await finishBoot();
            }
        })();
        return () => {
            cancelled = true;
            runtime?.EventsOff?.("file-companion:open");
            runtime?.EventsOff?.("file-companion:show");
            runtime?.EventsOff?.("file-companion:preview-reload");
            runtime?.EventsOff?.("ai-assistant-progress");
            runtime?.EventsOff?.("ai-assistant-token");
            runtime?.EventsOff?.("ai-assistant-response");
            runtime?.EventsOff?.("file-companion:webview-fullscreen");
            runtime?.EventsOff?.("file-companion:window-shown");
            document.removeEventListener("fullscreenchange", onDomFull);
            document.removeEventListener("webkitfullscreenchange", onDomFull);
            document.removeEventListener("visibilitychange", onCompanionVisibility);
            runtime?.OnFileDropOff?.();
        };
    }, []);

    // The open set is the session the next icon click restores. The payload is
    // the path list only, so a streaming token or a draft edit does not rewrite
    // it. An empty list is written after the window has shown a document, which
    // is the user closing the last tab. The empty render at boot is not that:
    // writing it would erase the files this boot is about to open.
    const openSetPayload = JSON.stringify(companionOpenSetPaths(tabs, active));
    const openSetSawDocument = useRef(false);
    const openSetSeq = useRef(0);
    const openSetChain = useRef(Promise.resolve());
    useEffect(() => {
        if (!bootSettled) return;
        const paths = JSON.parse(openSetPayload) as string[];
        if (paths.length === 0 && !openSetSawDocument.current) return;
        if (paths.length > 0) openSetSawDocument.current = true;
        // Writes overlap when the user closes a tab while the previous save
        // is still on disk. A later list must land last, or the closed tab
        // comes back.
        const seq = ++openSetSeq.current;
        openSetChain.current = openSetChain.current.then(() => {
            if (seq !== openSetSeq.current) return undefined;
            return SetFileCompanionOpenSet(paths);
        }).catch(() => undefined);
    }, [bootSettled, openSetPayload]);

    const tabStripKey = tabs.map((tab) => `${tab.path}\0${tab.name}\0${tab.dirty ? "*" : ""}`).join("\n");
    useLayoutEffect(() => {
        const node = tabsScrollRef.current;
        if (!node) return;
        tabNudgedRef.current = false;
        const activeNode = activeTabEl.current;
        if (activeNode && node.contains(activeNode)) revealCompanionTab(node, activeNode);
        setTabEnds((current) => assignTabEnds(current, readCompanionTabEnds(node)));
    }, [active]);
    useLayoutEffect(() => {
        const node = tabsScrollRef.current;
        if (!node) return;
        // Scroll must not reveal: << and >> would snap back to the active file.
        // A dirty mark remeasures, and reveals only before the user has moved
        // the row. A resize does the same, so the buttons can shrink the row
        // without hiding the active file, and a nudged row stays where it is.
        const revealUnlessNudged = () => {
            if (tabNudgedRef.current) return;
            const activeNode = activeTabEl.current;
            if (activeNode && node.contains(activeNode)) revealCompanionTab(node, activeNode);
        };
        const publish = () => {
            setTabEnds((current) => assignTabEnds(current, readCompanionTabEnds(node)));
        };
        revealUnlessNudged();
        publish();
        node.addEventListener("scroll", publish);
        const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(() => {
            revealUnlessNudged();
            publish();
        });
        observer?.observe(node);
        return () => {
            node.removeEventListener("scroll", publish);
            observer?.disconnect();
        };
    }, [tabStripKey]);

    const nudgeTabs = (direction: -1 | 1) => {
        const node = tabsScrollRef.current;
        if (!node) return;
        const before = node.scrollLeft;
        const port = node.getBoundingClientRect();
        const delta = companionTabNudgeDelta(port, companionTabBoxes(node), direction);
        const maxScroll = Math.max(0, node.scrollWidth - node.clientWidth);
        const next = delta > 0 ? clampCompanionTabScroll(before + direction * delta, maxScroll) : before;
        if (next !== before) {
            tabNudgedRef.current = true;
            node.scrollLeft = next;
        }
        setTabEnds((current) => assignTabEnds(current, readCompanionTabEnds(node)));
    };

    const activeForSave = tabs.find((item) => item.path === active);
    const toggleReadingFrame = useCallback(async () => {
        if (readingToggleRef.current) return;
        readingToggleRef.current = true;
        const session = readingFrameRef.current;
        const op = session.beginMaximize();
        try {
            if (readingFullRef.current) {
                if (typeof WindowUnfullscreen === "function") await Promise.resolve(WindowUnfullscreen());
                if (!session.isCurrent(op)) return;
                session.restoreNormal();
                readingFullRef.current = false;
                setReadingFull(false);
                return;
            }
            await session.rememberNormal(op);
            if (!session.isCurrent(op)) return;
            if (typeof WindowFullscreen === "function") await Promise.resolve(WindowFullscreen());
            if (!session.isCurrent(op)) return;
            readingFullRef.current = true;
            setReadingFull(true);
        } catch {
            // The runtime can be gone while the window is closing.
        } finally {
            readingToggleRef.current = false;
        }
    }, []);

    useEffect(() => {
        const onKey = (event: KeyboardEvent) => {
            if (event.key === "F11") {
                event.preventDefault();
                void toggleReadingFrame();
                return;
            }
            // 打开, the full screens, and the popups each own Escape
            // while they are up. A fullscreen exit would otherwise run in the same keypress.
            if (event.key === "Escape" && readingFullRef.current && !openMenuRef.current && !overlayRef.current && !chatExportDestOpenRef.current && !sharePasswordRef.current && !arxivSearchRef.current) {
                event.preventDefault();
                void toggleReadingFrame();
                return;
            }
            if (!(event.ctrlKey || event.metaKey) || event.key.toLowerCase() !== "s") return;
            event.preventDefault();
            const tab = tabsRef.current.find((item) => item.path === activeRef.current);
            if (!tab || tab.conflict) return;
            void saveTabRef.current(tab, tab.loadedHash);
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [toggleReadingFrame]);

    useEffect(() => {
        const tab = tabsRef.current.find((item) => item.path === activeRef.current);
        if (!tab || !tab.editable || tab.readOnly || tab.conflict || !tab.dirty || tab.saveError) return;
        const path = tab.path;
        const handle = window.setTimeout(() => { void saveTabRef.current(tab, tab.loadedHash); }, 600);
        return () => {
            window.clearTimeout(handle);
            if (activeRef.current === path) return;
            const latest = tabsRef.current.find((item) => item.path === path) ?? tab;
            if (!latest.editable || latest.readOnly || latest.conflict || !latest.dirty || latest.saveError) return;
            void saveTabRef.current(latest, latest.loadedHash);
        };
    }, [active, activeForSave?.path, activeForSave?.content, activeForSave?.dirty, activeForSave?.conflict, activeForSave?.saveError, activeForSave?.editable, activeForSave?.readOnly]);

    const saveTab = async (requested: CompanionTab, hash: string): Promise<"saved" | "conflict" | "error" | "skipped"> => {
        const overwrite = Boolean(requested.conflict && hash !== "" && hash === requested.diskHash);
        let outcome: "saved" | "conflict" | "error" | "skipped" = "skipped";
        const previous = saveTail.current.get(requested.path) ?? Promise.resolve();
        const run = previous.then(async () => {
            const latest = tabsRef.current.find((item) => item.path === requested.path) ?? requested;
            if (!latest.editable || latest.readOnly) {
                outcome = "skipped";
                return;
            }
            if (latest.conflict && !overwrite) {
                outcome = "skipped";
                return;
            }
            const sent = latest.content;
            const bufferAtSend = bufferEpoch.current.get(latest.path) ?? 0;
            const useHash = overwrite ? hash : (acceptedHash.current.get(latest.path) || latest.loadedHash);
            let result: SaveResult = {};
            try {
                result = await FileCompanionSaveText(latest.path, sent, useHash) as SaveResult;
            } catch (err) {
                const message = err instanceof Error ? err.message : "保存失败";
                setTabs((current) => current.map((item) => item.path === latest.path ? { ...item, saveError: message } : item));
                outcome = "error";
                return;
            }
            const conflict = Boolean(result.conflict ?? result.Conflict);
            const saved = Boolean(result.saved ?? result.Saved);
            const diskHash = String(result.diskHash ?? result.DiskHash ?? "");
            if (saved && diskHash) acceptedHash.current.set(latest.path, diskHash);
            if (saved) saveEpoch.current.set(latest.path, (saveEpoch.current.get(latest.path) ?? 0) + 1);
            const userEditedSince = (bufferEpoch.current.get(latest.path) ?? 0) !== bufferAtSend;
            setTabs((current) => current.map((item) => {
                if (item.path !== latest.path) return item;
                if (conflict) return { ...item, conflict: true, diskHash, confirmOverwrite: false, saveError: "" };
                if (!saved) return { ...item, saveError: String(result.error ?? result.Error ?? "保存失败") };
                if (userEditedSince) {
                    return { ...item, dirty: true, conflict: false, confirmOverwrite: false, loadedHash: diskHash || item.loadedHash, saveError: "" };
                }
                return { ...item, content: sent, dirty: false, conflict: false, confirmOverwrite: false, loadedHash: diskHash || item.loadedHash, saveError: "" };
            }));
            if (conflict) outcome = "conflict";
            else if (!saved) outcome = "error";
            else outcome = "saved";
        });
        saveTail.current.set(requested.path, run.then(() => undefined, () => undefined));
        await run;
        return outcome;
    };
    saveTabRef.current = saveTab;

    const updateActive = (patch: Partial<CompanionTab>) => {
        if (patch.content !== undefined) {
            const path = activeRef.current;
            bufferEpoch.current.set(path, (bufferEpoch.current.get(path) ?? 0) + 1);
        }
        setTabs((current) => current.map((tab) => tab.path === active ? { ...tab, ...patch } : tab));
    };

    const rememberSelection = (selection: string, selectionStart: number) => {
        const tab = tabsRef.current.find((item) => item.path === activeRef.current);
        if (!tab || (tab.selection === selection && tab.selectionStart === selectionStart)) return;
        updateActive({ selection, selectionStart });
    };

    const activeTab = tabs.find((tab) => tab.path === active) ?? null;
    const seenEditor = useRef(activeTab?.content ?? "");
    const editorValueChanged = seenEditor.current !== (activeTab?.content ?? "");
    seenEditor.current = activeTab?.content ?? "";
    // Follow reuses the assistant transcript's hardened scroller: the old
    // version latched off permanently on long answers, because it read the
    // distance to the bottom inside the async scroll event, where a delay
    // between content growth and the pin looks like "the reader left".
    const companionMessages = activeTab?.messages ?? NO_COMPANION_MESSAGES;
    const { handleScroll, handleUserScrollIntent, outputContainerRef, outputEndRef, scrollToBottom } = useAssistantOutputScroll({
        activityKey: active,
        hasConversation: companionMessages.length > 0,
        messages: companionMessages,
        ready: true,
    });
    // One scroller serves every file, so its scrollTop survives a tab switch.
    // A selected file always opens on its newest message, even when the follow
    // latch was left off in the previous file's transcript.
    const prevActivePathRef = useRef(active);
    useEffect(() => {
        if (prevActivePathRef.current === active) return;
        prevActivePathRef.current = active;
        scrollToBottom("auto", true, 1);
    }, [active, scrollToBottom]);
    const isWebTab = activeTab?.kind === "web";
    // The note records the player on screen. This commit is the moment that
    // player changes, before the replacement is painted. Closing the tab
    // removes it first, and that path discards the note instead.
    const webPlayerPath = isWebTab && activeTab?.frameUrl ? activeTab.path : "";
    const pinnedPlayer = tabs.find((item) => item.kind === "web" && item.frameUrl && item.path === pinnedPlayerPath) ?? null;
    const mountedPlayerPath = pinnedPlayer ? pinnedPlayer.path : webPlayerPath;
    const playerTab = tabs.find((item) => item.kind === "web" && item.frameUrl && item.path === mountedPlayerPath) ?? null;
    const holdPlayer = Boolean(playerTab && playerTab.path !== active);
    useLayoutEffect(() => {
        const shown = mountedPlayerPath;
        if (!shown) return;
        // The Windows flag stays true until an event says otherwise, including
        // after this player is gone. It belongs to the page that entered
        // fullscreen. DOM fullscreen is the element now on screen. A later
        // event can mark this page. This follows the mounted player, so a
        // held recording does not drop the flag before the picture changes.
        const frame = webFrameRef.current;
        const doc = document as Document & { webkitFullscreenElement?: Element | null };
        const fullscreenEl = document.fullscreenElement || doc.webkitFullscreenElement || null;
        domFullRef.current = frame != null && fullscreenEl === frame;
        winFullRef.current = false;
        webFullscreenRef.current = domFullRef.current;
        return () => {
            domFullRef.current = false;
            winFullRef.current = false;
            webFullscreenRef.current = false;
        };
    }, [mountedPlayerPath]);
    const playerURL = playerTab?.frameUrl ?? "";
    useLayoutEffect(() => {
        if (playerHost !== "native") return;
        const slot = webFrameRef.current;
        if (!playerURL || !slot) {
            void FileCompanionPlayerHide();
            return;
        }
        let dead = false;
        const place = () => {
            if (dead) return;
            // A hidden document, or a hide that has not reached the document
            // yet, must not be placed. An empty box unloads the page and
            // stops a YouTube note. The slot is often still full in the
            // moment after the click, so a positive box does not mean the
            // window is shown. Only a show that is waiting for a real slot
            // may place, and only once that slot covers a pixel.
            // The host's shown notice outranks a document that is still
            // hidden. Folding that notice back into the latch here would
            // drop the player until a visibility event that never arrives.
            if (windowHiddenRef.current) {
                if (!windowRevealRef.current) return;
                const waiting = slot.getBoundingClientRect();
                if (waiting.width < 1 || waiting.height < 1) return;
                windowHiddenRef.current = false;
                windowRevealRef.current = false;
            } else if (document.visibilityState === "hidden" && hostWindowShownRef.current !== true) {
                windowHiddenRef.current = true;
                windowRevealRef.current = false;
                return;
            }
            const ratio = window.devicePixelRatio > 0 ? window.devicePixelRatio : 1;
            const box = slot.getBoundingClientRect();
            void FileCompanionPlayerPlace(playerURL, box.left * ratio, box.top * ratio, box.width * ratio, box.height * ratio);
        };
        const onVisibility = () => {
            if (document.visibilityState === "hidden") {
                if (hostWindowShownRef.current === true) return;
                windowHiddenRef.current = true;
                windowRevealRef.current = false;
                return;
            }
            if (hostWindowShownRef.current === false) return;
            if (windowHiddenRef.current) windowRevealRef.current = true;
            place();
        };
        companionShownPlaceRef.current = place;
        place();
        document.addEventListener("visibilitychange", onVisibility);
        const observed = typeof ResizeObserver === "function" ? new ResizeObserver(place) : null;
        observed?.observe(slot);
        window.addEventListener("resize", place);
        return () => {
            dead = true;
            companionShownPlaceRef.current = () => undefined;
            observed?.disconnect();
            window.removeEventListener("resize", place);
            document.removeEventListener("visibilitychange", onVisibility);
        };
    }, [playerHost, playerURL]);
    useLayoutEffect(() => {
        const pinned = pinnedPlayerPath;
        if (!pinned || leaveSentRef.current === pinned) return;
        const tab = tabsRef.current.find((item) => item.kind === "web" && item.frameUrl && item.path === pinned);
        if (!tab) return;
        leaveSentRef.current = pinned;
        const ticket = leaveTicketRef.current + 1;
        leaveTicketRef.current = ticket;
        const epoch = noteEpochRef.current;
        // Fulfillment is the moment Go has retired this player. A rejection
        // leaves the watch live, so the next page must stay unpainted.
        // Retrying spins when the binding is missing. A note that has already
        // returned invalidates this ticket, because capture has finished.
        void FileCompanionWebPlayerLeft(tab.sessionId, epoch).then(() => {
            if (leaveTicketRef.current !== ticket) return;
            leaveSentRef.current = "";
            releasedPlayerRef.current = pinned;
            setPinnedPlayerPath((current) => (current === pinned ? "" : current));
        }, () => undefined);
    }, [pinnedPlayerPath]);
    const kind: FilePreviewKind = activeTab && !isWebTab ? filePreviewKindFromName(activeTab.name) : "text";
    const [convertBusyPaths, setConvertBusyPaths] = useState<ReadonlySet<string>>(() => new Set());
    const convertFlight = useRef(new Set<string>());
    const convertBusyRef = useRef(new Set<string>());
    const settleConvertRef = useRef<(body: ConvertDoneBody) => void>(() => undefined);
    const [scanPaths, setScanPaths] = useState<ReadonlySet<string>>(() => new Set());
    const [convertResults, setConvertResults] = useState<Readonly<Record<string, { pdf: string; message: string }>>>({});
    const [convertProgress, setConvertProgress] = useState<Readonly<Record<string, { done: number; total: number }>>>({});
    // A probe that started before a newer result must not put the old card back.
    const scanEpoch = useRef(0);
    const applyScanProbeRef = useRef<(path: string, result: { scan?: boolean; Scan?: boolean; path?: string; Path?: string }, epoch: number) => void>(() => undefined);
    applyScanProbeRef.current = (path, result, epoch) => {
        if (!path || epoch !== scanEpoch.current) return;
        const scan = Boolean(result.scan ?? result.Scan);
        if (!scan) {
            // This file is not a scan. A card or button remembered from an
            // earlier probe would still offer the previous conversion.
            setScanPaths((current) => {
                if (!current.has(path)) return current;
                const next = new Set(current);
                next.delete(path);
                return next;
            });
            setConvertResults((current) => {
                if (!current[path]) return current;
                const next = { ...current };
                delete next[path];
                return next;
            });
            return;
        }
        setScanPaths((current) => {
            if (current.has(path)) return current;
            const next = new Set(current);
            next.add(path);
            return next;
        });
        const ready = String(result.path ?? result.Path ?? "").trim();
        setConvertResults((current) => {
            const known = current[path];
            if (!ready) {
                if (!known) return current;
                // The cache no longer has a PDF for this source. A remembered
                // path would open a file the scan already dropped.
                const next = { ...current };
                delete next[path];
                return next;
            }
            if (known?.pdf === ready) return current;
            return { ...current, [path]: { pdf: ready, message: "" } };
        });
    };
    useEffect(() => {
        const path = activeTab?.path ?? "";
        if (!path || isWebTab || activeTab?.error || kind !== "pdf") return;
        let cancelled = false;
        const epoch = scanEpoch.current;
        void Promise.resolve(FileCompanionPDFScan(path)).then((result) => {
            if (cancelled || !result) return;
            applyScanProbeRef.current(path, result, epoch);
        }, () => undefined);
        return () => { cancelled = true; };
    }, [activeTab?.path, activeTab?.error, isWebTab, kind]);
    useEffect(() => {
        const runtime = runtimeAPI();
        const onConvert = (payload: unknown) => {
            const body = payload && typeof payload === "object" ? payload as { path?: string; Path?: string; done?: number; Done?: number; total?: number; Total?: number } : null;
            if (!body) return;
            const path = String(body.path ?? body.Path ?? "");
            const done = Number(body.done ?? body.Done ?? 0);
            const total = Number(body.total ?? body.Total ?? 0);
            if (!path || !Number.isFinite(done) || !Number.isFinite(total) || total < 1) return;
            setConvertProgress((current) => ({ ...current, [path]: { done, total } }));
        };
        runtime?.EventsOn?.("file-companion:convert-progress", onConvert);
        return () => { runtime?.EventsOff?.("file-companion:convert-progress"); };
    }, []);
    useEffect(() => {
        const runtime = runtimeAPI();
        const onDone = (payload: unknown) => {
            const body = readConvertDone(payload);
            if (!body) return;
            settleConvertRef.current(body);
        };
        runtime?.EventsOn?.("file-companion:convert-done", onDone);
        return () => { runtime?.EventsOff?.("file-companion:convert-done"); };
    }, []);
    const textEditorKind = kind === "markdown" || kind === "text" || kind === "code" || kind === "html";
    const showEditor = Boolean(activeTab && !isWebTab && !activeTab.error && textEditorKind && (activeTab.editable || activeTab.content || activeTab.readOnlyReason));
    const showPreview = Boolean(activeTab && !isWebTab && !activeTab.error && (kind === "pptx" || kind === "docx" || kind === "pdf" || kind === "xlsx" || kind === "office" || kind === "image" || kind === "video" || kind === "audio" || kind === "html" || kind === "latex"));
    const emptyFile = Boolean(activeTab && !isWebTab && !activeTab.error && !showEditor && activeTab.size <= 0);
    const htmlPreviewing = Boolean(activeTab && kind === "html" && activeTab.htmlPreview);
    const showSource = showEditor && !htmlPreviewing;
    const showPreviewPane = showPreview && !emptyFile && (kind !== "html" || htmlPreviewing);
    const unsupported = Boolean(activeTab && !isWebTab && !activeTab.error && !showEditor && !showPreview && !emptyFile);
    const cloudDisabled = !activeTab || !activeTab.canExport || activeTab.size > CLOUD_DISABLE_BYTES;
    const [importBusyPath, setImportBusyPath] = useState("");
    const [uploadBusyPath, setUploadBusyPath] = useState("");
    const [uploadedPaths, setUploadedPaths] = useState<ReadonlySet<string>>(() => new Set());
    const [sendingPaths, setSendingPaths] = useState<ReadonlySet<string>>(() => new Set());
    const [paperBusyPaths, setPaperBusyPaths] = useState<ReadonlySet<string>>(() => new Set());
    const paperBusyPathsRef = useRef<ReadonlySet<string>>(paperBusyPaths);
    const [noteBusyPaths, setNoteBusyPaths] = useState<ReadonlySet<string>>(() => new Set());
    const [chatTexOpen, setChatTexOpen] = useState(false);
    const [chatExportDestPath, setChatExportDestPath] = useState("");
    // 打开 and 导出 PDF each hang a panel under the bar. One open panel is enough.
    useEffect(() => {
        if (openMenu) setChatExportDestPath("");
    }, [openMenu]);
    const publishNoteBusy = (next: Set<string>) => {
        noteBusyPathsRef.current = next;
        setNoteBusyPaths(next);
        const web = tabsRef.current.find((tab) => tab.kind === "web" && next.has(tab.path));
        noteBusyRef.current = web?.path ?? "";
    };
    const markNoteBusy = (path: string, requestID: string, on: boolean) => {
        const next = new Set(noteBusyPathsRef.current);
        if (on) {
            next.add(path);
            if (requestID) noteRequestPathRef.current.set(requestID, path);
            publishNoteBusy(next);
            return;
        }
        // This attempt is done. A newer attempt on the same file keeps the
        // button on 停止 / 取消. Releasing by path alone would clear that one.
        if (requestID) noteRequestPathRef.current.delete(requestID);
        if (requestID) {
            for (const owner of noteRequestPathRef.current.values()) {
                if (owner === path) return;
            }
        }
        if (!next.delete(path)) return;
        publishNoteBusy(next);
    };
    setNoteBusyRef.current = (requestID: string) => {
        const path = noteRequestPathRef.current.get(requestID);
        if (!path) return;
        markNoteBusy(path, requestID, false);
    };
    const markPaperBusy = (path: string, on: boolean) => {
        if (paperBusyPathsRef.current.has(path) === on) return;
        const next = new Set(paperBusyPathsRef.current);
        if (on) next.add(path);
        else next.delete(path);
        paperBusyPathsRef.current = next;
        setPaperBusyPaths(next);
    };
    const markSending = (path: string, on: boolean) => {
        if (sendingPathsRef.current.has(path) === on) return;
        const next = new Set(sendingPathsRef.current);
        if (on) next.add(path);
        else next.delete(path);
        sendingPathsRef.current = next;
        setSendingPaths(next);
    };
    const tabIsSending = (path: string) => sendingPathsRef.current.has(path);
    const otherWebNoteRunning = (path: string) => {
        for (const busy of noteBusyPathsRef.current) {
            if (busy === path) continue;
            const tab = tabsRef.current.find((item) => item.path === busy);
            if (tab?.kind === "web") return true;
        }
        return false;
    };
    const [chatExportBusyPaths, setChatExportBusyPaths] = useState<ReadonlySet<string>>(() => new Set());
    const [projectExportBusyPath, setProjectExportBusyPath] = useState("");
    const [chatWidth, setChatWidth] = useState(360);
    const chatWidthRef = useRef(chatWidth);
    chatWidthRef.current = chatWidth;
    const splitRef = useRef<HTMLDivElement>(null);
    const splitDrag = useRef<{ x: number; width: number } | null>(null);
    const { showToast } = useToast();
    const exportFlight = useRef(new Set<string>());
    const importBusy = Boolean(activeTab && importBusyPath === activeTab.path);
    const uploadBusy = Boolean(activeTab && uploadBusyPath === activeTab.path);
    const uploaded = Boolean(activeTab && uploadedPaths.has(activeTab.path));
    // The button follows the file on screen. Another file's agent does not
    // mark this one 处理中, and a click here starts this file's own turn.
    const sendBusy = Boolean(activeTab && (sendingPaths.has(activeTab.path) || activeTab.status.trim()));
    const paperBusy = Boolean(activeTab && paperBusyPaths.has(activeTab.path));
    const convertBusy = Boolean(activeTab && convertBusyPaths.has(activeTab.path));
    const showConvert = Boolean(activeTab && !isWebTab && kind === "pdf" && !activeTab.error && scanPaths.has(activeTab.path));
    const convertCard = activeTab ? convertResults[activeTab.path] : undefined;
    const convertTitle = convertBusy && activeTab && convertProgress[activeTab.path]
        ? `已处理 ${convertProgress[activeTab.path].done}/${convertProgress[activeTab.path].total} 页`
        : "保留扫描版式，生成可以选中和搜索的 PDF";
    const noteBusy = Boolean(activeTab && noteBusyPaths.has(activeTab.path));
    const chatExportBusy = Boolean(activeTab && chatExportBusyPaths.has(activeTab.path));
    const projectExportBusy = Boolean(activeTab && projectExportBusyPath === activeTab.path);
    // Computed on demand, not at render time: whether a turn is still arriving
    // depends on which files are still sending. Reading it later
    // than the buttons would let a reply that finished streaming into the
    // transcript be judged still-writing, or the reverse.
    const buildChatTurns = useCallback(async (): Promise<CompanionChatExport> => {
        if (!activeTab) return EMPTY_CHAT_EXPORT;
        return companionChatExportTurns(
            activeTab.messages,
            Boolean(activeTab.status) || sendingPaths.has(activeTab.path),
            lightTheme,
        );
    }, [activeTab, sendingPaths]);
    const hasChatHistory = Boolean(activeTab && activeTab.messages.some((message) => message.text.trim() !== ""));
    const chatExportable = Boolean(activeTab && companionChatHasExportableTurn(
        activeTab.messages,
        Boolean(activeTab.status) || sendingPaths.has(activeTab.path),
    ));
    const canExportChat = chatExportable && !chatExportBusy;
    const chatExportDestOpen = Boolean(activeTab && chatExportDestPath === activeTab.path && canExportChat);
    chatExportDestOpenRef.current = chatExportDestOpen;
    // Drop a remembered destination when the reader leaves that file, or when
    // the button can no longer export. Leaving the path set pops the panel
    // open again on the way back. The LaTeX row is one boolean for the whole
    // window, so a tab change has to close it too.
    const chatMenuPathRef = useRef(active);
    useEffect(() => {
        if (chatMenuPathRef.current !== active || !canExportChat) {
            chatMenuPathRef.current = active;
            setChatTexOpen(false);
        }
        if (!chatExportDestPath) return;
        if (chatExportDestPath !== active || !canExportChat) setChatExportDestPath("");
    }, [active, chatExportDestPath, canExportChat]);
    const resizeChat = (next: number) => {
        setChatWidth(clampCompanionChatWidth(next, splitRef.current?.clientWidth ?? 0));
    };
    const onSplitPointerDown = (event: React.PointerEvent<HTMLDivElement>) => {
        if (event.button !== 0) return;
        event.preventDefault();
        splitDrag.current = { x: event.clientX, width: chatWidthRef.current };
        event.currentTarget.setPointerCapture?.(event.pointerId);
    };
    const onSplitPointerMove = (event: React.PointerEvent<HTMLDivElement>) => {
        const drag = splitDrag.current;
        if (!drag) return;
        resizeChat(drag.width - (event.clientX - drag.x));
    };
    const onSplitPointerEnd = (event: React.PointerEvent<HTMLDivElement>) => {
        splitDrag.current = null;
        if (event.currentTarget.hasPointerCapture?.(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
    };

    const runExport = async (kind: "import" | "upload") => {
        if (!activeTab) return;
        const path = activeTab.path;
        const flight = `${kind}:${path}`;
        if (exportFlight.current.has(flight)) return;
        exportFlight.current.add(flight);
        if (kind === "import") setImportBusyPath(path);
        else setUploadBusyPath(path);
        try {
            if (kind === "import") {
                const result = await FileCompanionImportKnowledge(path);
                const notice = knowledgeResultText(result);
                showToast(notice.text, notice.ok ? "success" : "error", 6000);
            } else {
                const uploaded = await FileCompanionUploadCloud(path);
                const held = uploaded?.duplicate_of_title || uploaded?.title || "";
                setUploadedPaths((current) => {
                    if (current.has(path)) return current;
                    const next = new Set(current);
                    next.add(path);
                    return next;
                });
                showToast(
                    uploaded?.duplicate
                        ? `云盘中已有相同内容${held ? `（${held}）` : ""}，未再次上传`
                        : "已上传到云盘，手机端「文档」可打开",
                    "success",
                    6000,
                );
            }
        } catch (err) {
            showToast(exportErrorText(err), "error", 6000);
        } finally {
            exportFlight.current.delete(flight);
            if (kind === "import") setImportBusyPath((current) => current === path ? "" : current);
            else setUploadBusyPath((current) => current === path ? "" : current);
        }
    };

    // File, cloud, and LaTeX share one flight per document. The button is
    // marked busy before figures and diagrams are inlined, so a second choice
    // cannot start while the first is still preparing the transcript. The
    // tab is the one that was clicked: a switch during the await keeps this
    // export on that document.
    const beginChatExport = (path: string) => {
        const flight = `chat:${path}`;
        if (exportFlight.current.has(flight)) return false;
        exportFlight.current.add(flight);
        setChatExportBusyPaths((current) => {
            if (current.has(path)) return current;
            const next = new Set(current);
            next.add(path);
            return next;
        });
        return true;
    };
    const endChatExport = (path: string) => {
        exportFlight.current.delete(`chat:${path}`);
        setChatExportBusyPaths((current) => {
            if (!current.has(path)) return current;
            const next = new Set(current);
            next.delete(path);
            return next;
        });
    };
    const runChatExport = async (work: (tab: CompanionTab, prepared: CompanionChatExport) => Promise<void>) => {
        const tab = activeTab;
        if (!tab) return;
        // Same rule as the button. A paper card or a reply still arriving
        // must not flash 正在导出 before the toast.
        if (!companionChatHasExportableTurn(tab.messages, Boolean(tab.status) || sendingPaths.has(tab.path))) {
            showToast("没有可导出的内容", "error", 6000);
            return;
        }
        if (!beginChatExport(tab.path)) return;
        try {
            const prepared = await buildChatTurns();
            if (prepared.turns.length === 0) {
                showToast("没有可导出的内容", "error", 6000);
                return;
            }
            await work(tab, prepared);
        } catch (err) {
            showToast(exportErrorText(err), "error", 6000);
        } finally {
            endChatExport(tab.path);
        }
    };

    const exportChat = () => runChatExport(async (tab, prepared) => {
        const dest = tab.kind === "web"
            ? await FileCompanionExportWebChatPDF(tab.sessionId, prepared.turns)
            : await FileCompanionExportChatPDF(tab.path, prepared.turns);
        // An empty path is the reader cancelling the dialog, not a failure.
        // A saved file is selected in Explorer so the reader can see where it went.
        if (dest) {
            showToast(companionExportNotice(prepared.missingFigures, prepared.missingDiagrams), companionExportNoticeKind(prepared.missingFigures, prepared.missingDiagrams), 6000);
            void ShowItemInFolder(dest).catch(() => undefined);
        }
    });

    const exportChatCloud = () => runChatExport(async (tab, prepared) => {
        const uploaded = tab.kind === "web"
            ? await FileCompanionExportWebChatCloud(tab.sessionId, prepared.turns)
            : await FileCompanionExportChatCloud(tab.path, prepared.turns);
        showToast(
            companionCloudExportNotice(uploaded, prepared.missingFigures, prepared.missingDiagrams),
            companionExportNoticeKind(prepared.missingFigures, prepared.missingDiagrams),
            6000,
        );
    });

    const exportChatTeX = () => runChatExport(async (tab, prepared) => {
        const dest = tab.kind === "web"
            ? await FileCompanionExportWebChatTeX(tab.sessionId, prepared.turns)
            : await FileCompanionExportChatTeX(tab.path, prepared.turns);
        if (dest) showToast("已导出 LaTeX", "success", 6000);
    });

    // The zip holds the open file, the highlights stored for that path, and the
    // chat the host already has. An empty dialog result is a cancel.
    const exportProject = async () => {
        if (!activeTab || activeTab.kind === "web" || projectExportBusy) return;
        const path = activeTab.path;
        const flight = `project:${path}`;
        if (exportFlight.current.has(flight)) return;
        exportFlight.current.add(flight);
        setProjectExportBusyPath(path);
        try {
            const dest = await FileCompanionExportProject(path, JSON.stringify(loadPdfMarks(path)));
            if (dest) {
                showToast("已导出项目", "success", 6000);
                void ShowItemInFolder(dest).catch(() => undefined);
            }
        } catch (err) {
            showToast(exportErrorText(err), "error", 6000);
        } finally {
            exportFlight.current.delete(flight);
            setProjectExportBusyPath((current) => (current === path ? "" : current));
        }
    };

    const bindCompanionRequest = (sessionId: string, requestID: string) => {
        requestEpochRef.current.set(requestID, chatEpochRef.current.get(sessionId) ?? 0);
    };

    // /clear, /new, and /reset empty this file's chat. They are not questions.
    // A reply that is already on the way is dropped when its events arrive.
    const clearCompanionChat = async (tab: CompanionTab, text: string): Promise<boolean> => {
        const sessionId = tab.sessionId;
        chatEpochRef.current.set(sessionId, (chatEpochRef.current.get(sessionId) ?? 0) + 1);
        setTabs((current) => current.map((item) => {
            if (item.path !== tab.path) return item;
            const draft = item.draft.trim() === text ? "" : item.draft;
            return { ...item, draft, messages: [], status: "" };
        }));
        try {
            if (tab.kind === "web") await ClearFileCompanionWebChat(tab.sessionId);
            else await ClearFileCompanionChat(tab.path);
        } catch (err) {
            showToast(turnErrorText(err instanceof Error ? err.message : "没能清空聊天"), "error", 6000);
        }
        return true;
    };

    publishShareNoteRef.current = (tabPath, text) => {
        const key = companionSharePathKey(tabPath);
        return AddFileCompanionShareNote(tabPath, text).then((raw) => {
            const posted = companionNotePost(raw);
            const prev = shareByPathRef.current.get(key);
            if (!prev) return;
            const part = prev.parts.length === 1 ? prev.parts[0] : prev.parts.find((item) => !item.mine);
            if (!part || !companionNotesRevisionWins(part.revision, posted.revision)) return;
            const revision = Math.max(part.revision, posted.revision);
            if (part.revision === revision && sameShareNotes(part.notes, posted.notes)) return;
            const stored = shareBindFromParts(prev.parts.map((item) => item.token === part.token ? { ...item, notes: posted.notes, revision } : item));
            shareByPathRef.current.set(key, stored);
            const token = shareBindToken(stored);
            setTabs((current) => current.map((tab) => companionSharePathKey(tab.path) === key ? { ...tab, shareToken: token, shareMine: stored.mine, notes: stored.notes } : tab));
        }).catch((err) => {
            showToast(turnErrorText(err instanceof Error ? err.message : "批注没有同步出去"), "error", 6000);
        });
    };
    flushPendingShareNotesRef.current = () => {
        for (const [key, queued] of pendingShareNotesRef.current) {
            const bind = shareByPathRef.current.get(key);
            if (!bind) continue;
            pendingShareNotesRef.current.delete(key);
            if (bind.mine || queued.length === 0) continue;
            const lines = queued.slice();
            void (async () => {
                for (const line of lines) {
                    await publishShareNoteRef.current(line.path, line.text);
                }
            })();
        }
    };

    const send = async (): Promise<boolean> => {
        if (!activeTab) return false;
        const text = activeTab.draft.trim();
        if (!text) return false;
        if (isHistoryResetCommandText(text)) return clearCompanionChat(activeTab, text);
        const tabPath = activeTab.path;
        if (tabIsSending(tabPath) || activeTab.status.trim()) return false;
        const sessionId = activeTab.sessionId;
        const selection = activeTab.selection;
        const selectionStart = activeTab.selectionStart;
        const mediaKind = filePreviewKindFromName(activeTab.name);
        const notePhrase = !isWebTab && (mediaKind === "video" || mediaKind === "audio") && companionVideoNotePhrase(text);
        const webNotePhrase = isWebTab && (text === "记笔记" || companionVideoNotePhrase(text));
        // One player. A second web note would record over the one already running.
        if (webNotePhrase && otherWebNoteRunning(tabPath)) return false;
        // A new question always re-arms the follow, even if the reader had
        // scrolled away during the previous answer.
        scrollToBottom("auto", true);
        markSending(tabPath, true);
        const requestID = nextCompanionRequestID();
        bindCompanionRequest(sessionId, requestID);
        let accepted = false;
        try {
            if (activeTab.dirty && activeTab.editable && !activeTab.readOnly) {
                const outcome = await saveTab(activeTab, activeTab.loadedHash);
                if (outcome !== "saved") return false;
            }
            const message: ChatMessage = { id: `${requestID}:user`, role: "user", text };
            setTabs((current) => current.map((tab) => {
                if (tab.path !== tabPath) return tab;
                // The captured text is what was sent. A draft typed while the
                // dirty save was in flight is a different line and stays.
                const draft = tab.draft.trim() === text ? "" : tab.draft;
                return { ...tab, draft, messages: [...tab.messages, message] };
            }));
            accepted = true;
        } finally {
            if (!accepted) {
                markSending(tabPath, false);
                drainIntroRef.current();
            }
        }
        // A reader’s line is a note on the host’s paper. It goes out with the
        // question and does not replace the host transcript. The host’s own
        // chat still syncs with the document. A line sent before the library
        // list arrives waits until that list says who the host is.
        if (accepted && !isWebTab) {
            const key = companionSharePathKey(tabPath);
            const shared = shareByPathRef.current.get(key);
            if (shared) {
                if (!shared.mine) void publishShareNoteRef.current(tabPath, text);
            } else {
                const queued = pendingShareNotesRef.current.get(key) ?? [];
                queued.push({ path: tabPath, text });
                if (queued.length > pendingShareNoteCap) queued.splice(0, queued.length - pendingShareNoteCap);
                pendingShareNotesRef.current.set(key, queued);
            }
        }
        // The question is sent once the user bubble is committed. The reply
        // keeps the busy lock, but ↑ must already see this line.
        let captureEpoch = 0;
        if (accepted && (notePhrase || webNotePhrase)) {
            videoNoteRequestIds.current.add(requestID);
            if (webNotePhrase) noteBusyRequestRef.current = requestID;
            markNoteBusy(tabPath, requestID, true);
            if (webNotePhrase) {
                captureEpoch = webCaptureEpochRef.current + 1;
                webCaptureEpochRef.current = captureEpoch;
                noteEpochRef.current = captureEpoch;
            }
        }
        void (async () => {
            try {
                if (isWebTab) {
                    await SendFileCompanionWebMessage({
                        sessionId,
                        text,
                        requestId: requestID,
                        purpose: webNotePhrase ? "video-note" : "",
                        fullscreen: webFullscreenRef.current,
                        ...(captureEpoch > 0 ? { captureEpoch } : {}),
                        ...webCaptureFields(),
                    });
                } else {
                    await SendFileCompanionMessage({
                        path: tabPath,
                        text,
                        selection,
                        request_id: requestID,
                        ...(notePhrase ? { purpose: "video-note" } : {}),
                        ...(selection && selectionStart >= 0 ? { selection_start: selectionStart } : {}),
                    });
                }
            } catch (err) {
                if (!companionEventCurrent(chatEpochRef.current, requestEpochRef.current, sessionId, requestID)) return;
                const note = turnErrorText(err instanceof Error ? err.message : "发送失败");
                const assistantID = `${requestID}:assistant`;
                setTabs((current) => current.map((tab) => {
                    if (tab.path !== tabPath) return tab;
                    if (tab.messages.some((item) => item.id === assistantID && item.text.trim() !== "")) return { ...tab, status: "" };
                    return { ...tab, status: "", messages: [...tab.messages, { id: assistantID, role: "assistant", text: note }] };
                }));
            } finally {
                markSending(tabPath, false);
                if (notePhrase || webNotePhrase) {
                    if (noteBusyRequestRef.current === requestID) noteBusyRequestRef.current = "";
                    markNoteBusy(tabPath, requestID, false);
                }
                drainIntroRef.current();
            }
        })();
        return true;
    };

    const sendPaper = async () => {
        if (!activeTab || activeTab.error || tabIsSending(activeTab.path) || activeTab.status.trim()) return;
        const tabPath = activeTab.path;
        const sessionId = activeTab.sessionId;
        scrollToBottom("auto", true);
        markSending(tabPath, true);
        markPaperBusy(tabPath, true);
        const requestID = nextCompanionRequestID();
        bindCompanionRequest(sessionId, requestID);
        try {
            if (activeTab.dirty && activeTab.editable && !activeTab.readOnly) {
                const outcome = await saveTab(activeTab, activeTab.loadedHash);
                if (outcome !== "saved") return;
            }
            const message: ChatMessage = { id: `${requestID}:user`, role: "user", text: "论文解读" };
            setTabs((current) => current.map((tab) => tab.path === tabPath ? { ...tab, messages: [...tab.messages, message] } : tab));
            paperRequestIds.current.add(requestID);
            try {
                await SendFileCompanionMessage({
                    path: tabPath,
                    text: "论文解读",
                    request_id: requestID,
                    purpose: "paper",
                });
            } catch (err) {
                paperRequestIds.current.delete(requestID);
                if (!companionEventCurrent(chatEpochRef.current, requestEpochRef.current, sessionId, requestID)) return;
                const note = turnErrorText(err instanceof Error ? err.message : "发送失败");
                const assistantID = `${requestID}:assistant`;
                setTabs((current) => current.map((tab) => {
                    if (tab.path !== tabPath) return tab;
                    if (tab.messages.some((item) => item.id === assistantID && item.text.trim() !== "")) return { ...tab, status: "" };
                    return { ...tab, status: "", messages: [...tab.messages, { id: assistantID, role: "assistant", text: note }] };
                }));
            }
        } finally {
            markSending(tabPath, false);
            markPaperBusy(tabPath, false);
            drainIntroRef.current();
        }
    };

    const markConvertBusy = (path: string, on: boolean) => {
        if (convertBusyRef.current.has(path) === on) return;
        const next = new Set(convertBusyRef.current);
        if (on) next.add(path);
        else next.delete(path);
        convertBusyRef.current = next;
        setConvertBusyPaths(next);
    };
    const endConvert = (path: string) => {
        markConvertBusy(path, false);
        setConvertProgress((current) => {
            if (!current[path]) return current;
            const next = { ...current };
            delete next[path];
            return next;
        });
    };
    // The click returns as soon as the job starts. This runs when that job
    // emits file-companion:convert-done, including after the reader has
    // switched to another file.
    settleConvertRef.current = (body) => {
        const tell = (text: string, type: "success" | "error") => {
            showToast(withConvertFile(body.path, text), type, 6000);
        };
        if (body.saveFailed) {
            // The selectable PDF exists. The path the reader picked was not written.
            scanEpoch.current += 1;
            tell(body.message || "无法保存到该位置", "error");
            if (body.pdf) {
                setConvertResults((current) => ({
                    ...current,
                    [body.path]: { pdf: body.pdf, message: body.message },
                }));
                void ShowItemInFolder(body.pdf).catch(() => undefined);
            }
            endConvert(body.path);
            return;
        }
        if (!body.scan) {
            // Text layer, or a file that cannot be converted. Either way the
            // remembered card is for a PDF this file no longer owns.
            scanEpoch.current += 1;
            applyScanProbeRef.current(body.path, { scan: false }, scanEpoch.current);
            tell(body.message || (body.ok ? "这份 PDF 已有文字层，可以直接选择和搜索。" : "没有生成可选中的 PDF"), body.ok ? "success" : "error");
            endConvert(body.path);
            return;
        }
        if (!body.ok || !body.pdf) {
            tell(body.message || "没有生成可选中的 PDF", "error");
            endConvert(body.path);
            // The click may already have dropped a PDF that belonged to other
            // bytes. Ask the probe, which is what the card follows.
            const epoch = scanEpoch.current;
            void Promise.resolve(FileCompanionPDFScan(body.path)).then((result) => {
                if (!result) return;
                applyScanProbeRef.current(body.path, result, epoch);
            }, () => undefined);
            return;
        }
        scanEpoch.current += 1;
        setConvertResults((current) => ({
            ...current,
            [body.path]: { pdf: body.pdf, message: body.message },
        }));
        const toastText = !body.complete || body.pending
            ? (body.message || "请再点「转换」继续。")
            : "已生成可选中文字的 PDF，原文件没有改动。";
        tell(toastText, "success");
        // The toast names the source file. Explorer selects the PDF this
        // result names: the path the reader picked when that copy was written.
        void ShowItemInFolder(body.pdf).catch(() => undefined);
        endConvert(body.path);
    };

    const convertScan = async () => {
        if (!activeTab || activeTab.error || isWebTab || kind !== "pdf") return;
        const path = activeTab.path;
        if (convertFlight.current.has(path) || convertBusyRef.current.has(path) || paperBusyPathsRef.current.has(path) || tabIsSending(path)) return;
        convertFlight.current.add(path);
        try {
            const picked = String(await FileCompanionChooseConvertDestination(path) ?? "").trim();
            if (!picked) return;
            markConvertBusy(path, true);
            const result = await FileCompanionConvertScan(path, picked);
            const started = result?.started === true || result?.Started === true;
            if (started) return;
            const busy = result?.busy === true || result?.Busy === true;
            if (busy) {
                showToast(withConvertFile(path, String(result?.message ?? result?.Message ?? "").trim() || "正在转换，请稍候。"), "info", 4000);
                return;
            }
            const scan = (result?.scan ?? result?.Scan) !== false;
            settleConvertRef.current({
                path,
                ok: (result?.ok ?? result?.OK) === true,
                scan,
                complete: result?.complete === true || result?.Complete === true,
                pending: result?.pending === true || result?.Pending === true,
                pdf: String(result?.path ?? result?.Path ?? "").trim(),
                message: String(result?.message ?? result?.Message ?? "").trim(),
                saveFailed: result?.save_failed === true || result?.SaveFailed === true,
            });
        } catch (err) {
            showToast(withConvertFile(path, exportErrorText(err)), "error", 6000);
            endConvert(path);
        } finally {
            convertFlight.current.delete(path);
        }
    };

    const sendNote = async () => {
        if (!activeTab || activeTab.error) return;
        if (noteBusy) {
            // The turn that started the note clears noteBusy when it returns.
            // Clearing it here labels the button 生成笔记 while that turn is still writing.
            if (activeTab.kind === "web") await StopFileCompanionWebNote(activeTab.sessionId, noteEpochRef.current);
            else await CancelFileCompanionTurn(activeTab.path);
            return;
        }
        if (tabIsSending(activeTab.path) || activeTab.status.trim()) return;
        const tabPath = activeTab.path;
        const sessionId = activeTab.sessionId;
        const web = activeTab.kind === "web";
        if (web && otherWebNoteRunning(tabPath)) return;
        const label = web ? "记笔记" : "生成笔记";
        const captureEpoch = web ? (webCaptureEpochRef.current + 1) : 0;
        if (web) {
            webCaptureEpochRef.current = captureEpoch;
            noteEpochRef.current = captureEpoch;
        }
        scrollToBottom("auto", true);
        markSending(tabPath, true);
        const requestID = nextCompanionRequestID();
        if (web) noteBusyRequestRef.current = requestID;
        markNoteBusy(tabPath, requestID, true);
        bindCompanionRequest(sessionId, requestID);
        videoNoteRequestIds.current.add(requestID);
        const message: ChatMessage = { id: `${requestID}:user`, role: "user", text: label };
        setTabs((current) => current.map((tab) => tab.path === tabPath ? { ...tab, messages: [...tab.messages, message] } : tab));
        try {
            if (web) {
                await SendFileCompanionWebMessage({
                    sessionId,
                    text: label,
                    requestId: requestID,
                    purpose: "video-note",
                    fullscreen: webFullscreenRef.current,
                    captureEpoch,
                    ...webCaptureFields(),
                });
            } else {
                await SendFileCompanionMessage({
                    path: tabPath,
                    text: label,
                    request_id: requestID,
                    purpose: "video-note",
                });
            }
        } catch (err) {
            videoNoteRequestIds.current.delete(requestID);
            if (!companionEventCurrent(chatEpochRef.current, requestEpochRef.current, sessionId, requestID)) return;
            const note = turnErrorText(err instanceof Error ? err.message : "发送失败");
            const assistantID = `${requestID}:assistant`;
            setTabs((current) => current.map((tab) => {
                if (tab.path !== tabPath) return tab;
                if (tab.messages.some((item) => item.id === assistantID && item.text.trim() !== "")) return { ...tab, status: "" };
                return { ...tab, status: "", messages: [...tab.messages, { id: assistantID, role: "assistant", text: note }] };
            }));
        } finally {
            markSending(tabPath, false);
            if (noteBusyRequestRef.current === requestID) noteBusyRequestRef.current = "";
            markNoteBusy(tabPath, requestID, false);
            drainIntroRef.current();
        }
    };

    // Each new empty file asks what it is on its own agent. The question is
    // not typed, so it is not stored in the composer's input history, and it
    // carries no selection. Another file's turn does not hold this one back.
    const startIntro = (tab: CompanionTab): Promise<void> => {
        const tabPath = tab.path;
        const sessionId = tab.sessionId;
        const mediaKind = tab.kind === "web" ? "" : filePreviewKindFromName(tab.name);
        if (mediaKind === "video" || mediaKind === "audio") {
            const sentence = mediaKind === "audio"
                ? "这个音频还没有笔记。点「生成笔记」，或发送「生成笔记」。"
                : "这个视频还没有笔记。点「生成笔记」，或发送「生成笔记」。";
            const requestID = nextCompanionRequestID("intro-local");
            const message: ChatMessage = { id: `${requestID}:assistant`, role: "assistant", text: sentence };
            setTabs((current) => current.map((item) => {
                if (item.path !== tabPath || item.messages.length > 0) return item;
                return { ...item, messages: [message] };
            }));
            return Promise.resolve();
        }
        const text = FILE_COMPANION_INTRO;
        markSending(tabPath, true);
        const requestID = nextCompanionRequestID("intro");
        bindCompanionRequest(sessionId, requestID);
        if (activeRef.current === tabPath) scrollToBottom("auto", true);
        const message: ChatMessage = { id: `${requestID}:user`, role: "user", text };
        setTabs((current) => current.map((item) => {
            if (item.path !== tabPath || item.messages.length > 0) return item;
            return { ...item, messages: [message] };
        }));
        return (async () => {
            try {
                await SendFileCompanionMessage({ path: tabPath, text, request_id: requestID });
            } catch (err) {
                if (!companionEventCurrent(chatEpochRef.current, requestEpochRef.current, sessionId, requestID)) return;
                const note = turnErrorText(err instanceof Error ? err.message : "发送失败");
                const assistantID = `${requestID}:assistant`;
                setTabs((current) => current.map((item) => {
                    if (item.path !== tabPath) return item;
                    if (item.messages.some((entry) => entry.id === assistantID && entry.text.trim() !== "")) return { ...item, status: "" };
                    return { ...item, status: "", messages: [...item.messages, { id: assistantID, role: "assistant", text: note }] };
                }));
            } finally {
                markSending(tabPath, false);
            }
        })();
    };

    const drainIntro = () => {
        const queued = introQueueRef.current;
        if (queued.length === 0) return;
        const stay: string[] = [];
        const start: CompanionTab[] = [];
        for (const path of queued) {
            const tab = tabsRef.current.find((item) => item.path === path);
            if (!tab || tab.error || tab.messages.length > 0) continue;
            if (tabIsSending(path) || tab.status.trim()) {
                stay.push(path);
                continue;
            }
            start.push(tab);
        }
        introQueueRef.current = stay;
        for (const tab of start) {
            void startIntro(tab).finally(() => {
                drainIntroRef.current();
            });
        }
    };
    drainIntroRef.current = drainIntro;

    useEffect(() => {
        drainIntroRef.current();
    }, [tabs, sendingPaths]);

    const chooseFiles = async () => {
        if (choosingRef.current) return;
        choosingRef.current = true;
        setChooseBusy(true);
        try {
            const paths = await ChooseFileCompanionFiles();
            for (const path of paths ?? []) {
                await openPath(path, false, false, 0, true);
            }
        } catch {
            // The empty window and the 打开文件 tab stay usable.
        } finally {
            choosingRef.current = false;
            setChooseBusy(false);
        }
    };
    chooseFilesRef.current = chooseFiles;

    // Closing the panel while focus sits inside it would drop focus to <body>
    // and restart the tab order at the top of the document (WCAG 2.4.3).
    // Keyboard closes hand focus back to the trigger. Pointer closes must
    // not: the browser moves focus as mousedown's default action, i.e. after
    // this handler runs, so "focus is still inside" cannot distinguish an
    // outside click from a keyboard close — guessing would steal focus from
    // wherever the user just clicked.
    const closeOpenMenu = useCallback((restoreFocus: boolean) => {
        setOpenMenu(false);
        if (restoreFocus) {
            chooseButtonRef.current?.focus();
        }
    }, []);

    // Only the success path asks where focus actually is: a download can take
    // long enough for the user to click into the document, and focus must not
    // be yanked back from there.
    const focusInsideOpenMenu = useCallback(() => {
        const node = openWrapRef.current;
        return !!node && document.activeElement instanceof Node && node.contains(document.activeElement);
    }, []);

    // The menu closes on any click or focus move outside it, and on Escape, so
    // it never floats over the document it belongs to. A download in flight
    // pins it open: closing it would drop the failure message into an
    // unmounted panel and leave the user with no reason for the silence.
    useEffect(() => {
        if (!openMenu) return;
        if (!arxivSearchRef.current) arxivInputRef.current?.focus();
        const onPointerDown = (event: MouseEvent) => {
            if (arxivBusyRef.current || sharePasswordRef.current || arxivSearchRef.current) return;
            const node = openWrapRef.current;
            if (node && event.target instanceof Node && node.contains(event.target)) return;
            closeOpenMenu(false);
        };
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key !== "Escape" || arxivBusyRef.current || sharePasswordRef.current || arxivSearchRef.current) return;
            closeOpenMenu(true);
        };
        document.addEventListener("mousedown", onPointerDown);
        document.addEventListener("keydown", onKeyDown);
        return () => {
            document.removeEventListener("mousedown", onPointerDown);
            document.removeEventListener("keydown", onKeyDown);
        };
    }, [openMenu, closeOpenMenu]);

    // The PDF iframe never delivers a click to the document, so the scrim
    // under the bar is the close for that page. Everything else — the title
    // bar, Escape — is handled here. Escape stops at this listener so it
    // does not also leave fullscreen.
    useEffect(() => {
        if (!chatExportDestOpen) return;
        chatExportWrapRef.current?.querySelector<HTMLButtonElement>("[data-testid='file-companion-export-chat-file']")?.focus();
        const onPointerDown = (event: MouseEvent) => {
            const node = chatExportWrapRef.current;
            if (node && event.target instanceof Node && node.contains(event.target)) return;
            setChatExportDestPath("");
        };
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key !== "Escape") return;
            event.preventDefault();
            event.stopPropagation();
            setChatExportDestPath("");
            chatExportButtonRef.current?.focus();
        };
        document.addEventListener("mousedown", onPointerDown);
        document.addEventListener("keydown", onKeyDown);
        return () => {
            document.removeEventListener("mousedown", onPointerDown);
            document.removeEventListener("keydown", onKeyDown);
        };
    }, [chatExportDestOpen]);

    const clearSharePassword = () => {
        setSharePasswordLink("");
        setSharePasswordValue("");
        setSharePasswordError("");
        setSharePasswordVisible(false);
    };
    // Assigned during render so the Escape capture listener, which is bound
    // once per open, still calls the latest closer and can return focus to
    // the URL box. The menu stays up; only this box goes away.
    closeSharePasswordRef.current = () => {
        if (arxivBusyRef.current) return;
        clearSharePassword();
        arxivInputRef.current?.focus();
    };
    const closeSharePassword = () => closeSharePasswordRef.current();

    const importCompanionShare = async (link: string, password: string) => {
        if (arxivBusyRef.current) return;
        arxivBusyRef.current = true;
        setArxivBusy(true);
        setArxivMessage("");
        try {
            const raw = await ImportFileCompanionShare(link, password) as Record<string, unknown>;
            const path = String(raw?.path || raw?.Path || "").trim();
            const highlights = String(raw?.highlights || raw?.Highlights || "");
            if (!path) {
                const failure = "导入失败：没有拿到文件";
                if (sharePasswordRef.current) setSharePasswordError(failure);
                else setArxivMessage(failure);
                return;
            }
            if (highlights) {
                applyShareUpdateRef.current({ path, highlights, messages: [], project: true, replaceMessages: false });
            }
            try {
                await openPath(path, false, false, 0, true);
            } catch {
                // The share is already in the library. A later open failure
                // must not be read as 需要分享密码 or as a failed import.
                clearSharePassword();
                setArxivMessage("文件已导入，但没有打开");
                showToast("已导入文档库", "success", 6000);
                return;
            }
            showToast("已导入文档库", "success", 6000);
            setArxivInput("");
            clearSharePassword();
            closeOpenMenu(focusInsideOpenMenu());
        } catch (err) {
            const sentence = bindingErrorText(err) || "导入失败";
            if (sentence.includes("需要分享密码")) {
                setSharePasswordLink(link);
                setSharePasswordError(password ? sentence : "");
                if (!password) setSharePasswordValue("");
                return;
            }
            if (sharePasswordRef.current) {
                setSharePasswordError(sentence);
                return;
            }
            setArxivMessage(sentence);
        } finally {
            arxivBusyRef.current = false;
            setArxivBusy(false);
        }
    };

    const submitSharePassword = () => {
        const link = sharePasswordRef.current;
        const secret = sharePasswordValue.trim();
        if (!link || arxivBusyRef.current) return;
        if (!secret) {
            setSharePasswordError("请输入分享密码");
            sharePasswordInputRef.current?.focus();
            return;
        }
        void importCompanionShare(link, secret);
    };

    const openReference = async () => {
        const value = arxivInput.trim();
        // The password box is modal. Enter on the URL field behind it must
        // not start a second import with an empty password.
        if (arxivBusyRef.current || sharePasswordRef.current) return;
        // Enter reaches here even when 确定 is disabled, so an empty box must
        // say so instead of doing nothing at all.
        if (!value) {
            setArxivMessage("请填写论文、页面或分享链接");
            return;
        }
        if (looksLikeCompanionShare(value)) {
            await importCompanionShare(value, "");
            return;
        }
        const arxiv = looksLikeArxivReference(value);
        arxivBusyRef.current = true;
        setArxivBusy(true);
        setArxivMessage("");
        try {
            if (arxiv) {
                const path = await FileCompanionOpenArxiv(value);
                if (!path) {
                    setArxivMessage("下载失败：没有拿到文件路径");
                    return;
                }
                await openPath(path, false, false, 0, true);
            } else {
                const opened = await FileCompanionOpenWebPage(value) as { kind?: string; url?: string; frameUrl?: string; sessionId?: string; error?: string; Error?: string };
                const frame = String(opened?.frameUrl || opened?.url || "").trim();
                const sessionId = String(opened?.sessionId || "").trim();
                const failure = String(opened?.error || opened?.Error || "").trim();
                if (failure || !frame || !sessionId) {
                    setArxivMessage(failure || "打不开这个页面");
                    return;
                }
                const host = (() => {
                    try { return new URL(frame).hostname; } catch { return frame; }
                })();
                const next: CompanionTab = {
                    path: frame,
                    name: host || "网页",
                    editable: false,
                    readOnly: true,
                    canExport: false,
                    content: "",
                    loadedHash: "",
                    sessionId,
                    size: 0,
                    error: "",
                    draft: "",
                    selection: "",
                    selectionStart: -1,
                    messages: [],
                    readOnlyReason: "",
                    status: "",
                    dirty: false,
                    conflict: false,
                    diskHash: "",
                    confirmOverwrite: false,
                    previewEpoch: 0,
                    saveError: "",
                    htmlPreview: false,
                    kind: "web",
                    frameUrl: frame,
                };
                const existing = tabsRef.current.find((tab) => tab.kind === "web" && (tab.frameUrl === frame || tab.sessionId === sessionId));
                if (!existing) {
                    tabCommittedRef.current = true;
                    setTabs((current) => [...current, next]);
                }
                setActive(existing?.path || frame);
            }
            setArxivInput("");
            closeOpenMenu(focusInsideOpenMenu());
        } catch (err) {
            setArxivMessage(bindingErrorText(err) || (arxiv ? "下载失败" : "打开失败"));
        } finally {
            arxivBusyRef.current = false;
            setArxivBusy(false);
        }
    };

    const dismissArxivSearch = () => {
        arxivSearchGenRef.current += 1;
        arxivSearchBusyRef.current = false;
        setArxivSearchOpen(false);
        setArxivSearchBusy(false);
        setArxivOpeningId("");
    };
    const focusInsideArxivSearch = () => {
        const node = arxivSearchDialogRef.current;
        return !!node && document.activeElement instanceof Node && node.contains(document.activeElement);
    };
    // Assigned during render so the Escape capture listener calls the latest
    // closer. A download in flight keeps the list up: closing it would hide
    // the failure the same way closing the open menu would.
    closeArxivSearchRef.current = () => {
        if (arxivBusyRef.current) return;
        dismissArxivSearch();
        arxivSearchButtonRef.current?.focus();
    };
    const closeArxivSearch = () => closeArxivSearchRef.current();

    const openArxivSearch = () => {
        if (arxivBusyRef.current || sharePasswordRef.current) return;
        setArxivSearchQuery(arxivInput.trim());
        setArxivSearchError("");
        setArxivSearchHits([]);
        setArxivSearched(false);
        setArxivSearchFor("");
        setArxivSearchBusy(false);
        arxivSearchBusyRef.current = false;
        setArxivOpeningId("");
        setArxivSearchOpen(true);
    };

    const runArxivSearch = async () => {
        if (arxivBusyRef.current || arxivSearchBusyRef.current) return;
        const q = arxivSearchQuery.trim();
        if (!q) {
            setArxivSearchError("请输入关键字");
            setArxivSearched(false);
            setArxivSearchHits([]);
            return;
        }
        const gen = ++arxivSearchGenRef.current;
        arxivSearchBusyRef.current = true;
        setArxivSearchBusy(true);
        setArxivSearchError("");
        setArxivSearchHits([]);
        setArxivSearched(false);
        try {
            const raw = await FileCompanionSearchArxiv(q);
            if (gen !== arxivSearchGenRef.current) return;
            setArxivSearchHits(companionArxivHits(raw));
            setArxivSearchFor(q);
            setArxivSearched(true);
        } catch (err) {
            if (gen !== arxivSearchGenRef.current) return;
            setArxivSearchError(bindingErrorText(err) || "搜索失败");
        } finally {
            if (gen === arxivSearchGenRef.current) {
                arxivSearchBusyRef.current = false;
                setArxivSearchBusy(false);
            }
        }
    };

    const openArxivHit = async (id: string) => {
        const paper = id.trim();
        if (!paper || arxivBusyRef.current) return;
        arxivBusyRef.current = true;
        setArxivBusy(true);
        setArxivSearchError("");
        setArxivOpeningId(paper);
        try {
            const path = await FileCompanionOpenArxiv(paper);
            if (!path) {
                setArxivSearchError("下载失败：没有拿到文件路径");
                return;
            }
            await openPath(path, false, false, 0, true);
            const restore = focusInsideArxivSearch();
            dismissArxivSearch();
            closeOpenMenu(restore);
        } catch (err) {
            setArxivSearchError(bindingErrorText(err) || "下载失败");
        } finally {
            arxivBusyRef.current = false;
            setArxivBusy(false);
            setArxivOpeningId("");
        }
    };

    // Search owns Escape and Tab while it is open, including during a
    // download. Capture runs before the open menu and before fullscreen, so
    // one keypress cannot also close the menu or leave fullscreen. Tab stays
    // inside the dialog: the URL field behind it would otherwise open whatever
    // was typed there.
    useEffect(() => {
        if (!arxivSearchOpen) return;
        const input = arxivSearchInputRef.current;
        input?.focus();
        input?.select();
        const onKey = (event: KeyboardEvent) => {
            if (event.key === "Escape") {
                event.preventDefault();
                event.stopImmediatePropagation();
                if (!arxivBusyRef.current) closeArxivSearchRef.current();
                return;
            }
            if (event.key !== "Tab") return;
            const root = arxivSearchDialogRef.current;
            if (!root) return;
            const items = Array.from(root.querySelectorAll<HTMLElement>("input, button, textarea, select, a[href]")).filter((el) => !el.hasAttribute("disabled") && el.tabIndex !== -1);
            if (items.length === 0) {
                event.preventDefault();
                return;
            }
            const first = items[0];
            const last = items[items.length - 1];
            const active = document.activeElement;
            if (event.shiftKey && (active === first || !root.contains(active))) {
                event.preventDefault();
                last.focus();
            } else if (!event.shiftKey && (active === last || !root.contains(active))) {
                event.preventDefault();
                first.focus();
            }
        };
        window.addEventListener("keydown", onKey, true);
        return () => window.removeEventListener("keydown", onKey, true);
    }, [arxivSearchOpen]);

    // The password box owns Escape and Tab, including while the import is in
    // flight. A capture listener runs before the open menu and before
    // fullscreen. Tab stays inside the box: the URL field is earlier in the
    // document and would otherwise import again. When the request ends and
    // the box is still open, focus returns to the field — disabling that
    // field for the request would have dropped the caret onto the document.
    useEffect(() => {
        if (!sharePasswordLink) return;
        if (!arxivBusy) sharePasswordInputRef.current?.focus();
        const onKey = (event: KeyboardEvent) => {
            if (event.key === "Escape") {
                event.preventDefault();
                event.stopImmediatePropagation();
                if (!arxivBusyRef.current) closeSharePasswordRef.current();
                return;
            }
            if (event.key !== "Tab") return;
            const root = sharePasswordDialogRef.current;
            if (!root) return;
            const items = Array.from(root.querySelectorAll<HTMLElement>("input, button, textarea, select, a[href]")).filter((el) => !el.hasAttribute("disabled") && el.tabIndex !== -1);
            if (items.length === 0) {
                event.preventDefault();
                return;
            }
            const first = items[0];
            const last = items[items.length - 1];
            const active = document.activeElement;
            if (event.shiftKey && (active === first || !root.contains(active))) {
                event.preventDefault();
                last.focus();
            } else if (!event.shiftKey && (active === last || !root.contains(active))) {
                event.preventDefault();
                first.focus();
            }
        };
        window.addEventListener("keydown", onKey, true);
        return () => window.removeEventListener("keydown", onKey, true);
    }, [sharePasswordLink, arxivBusy]);

    // Pin during render so the committed tree never shows the next page
    // before Go has retired this player. Closing the tab removes it; that
    // path discards the note and does not hold the picture. Once the note
    // call has returned, capture is finished, so a leave that never settled
    // must not keep the old player on screen or release a later note.
    // Coming back does not undo that retirement. Capture has stopped, so
    // the next switch shows the page that was selected.
    const webNotePath = tabs.find((item) => item.kind === "web" && item.frameUrl && noteBusyPaths.has(item.path))?.path ?? "";
    if (!webNotePath) releasedPlayerRef.current = "";
    const recordingWeb = webNotePath !== "";
    const shouldPin = recordingWeb && webPlayerPath !== webNotePath && releasedPlayerRef.current !== webNotePath;
    let nextPin = shouldPin ? webNotePath : pinnedPlayerPath;
    if (!webNotePath) {
        nextPin = "";
        if (leaveSentRef.current) {
            leaveSentRef.current = "";
            leaveTicketRef.current += 1;
        }
    }
    if (nextPin && !tabs.some((item) => item.kind === "web" && item.frameUrl && item.path === nextPin)) nextPin = "";
    if (nextPin !== pinnedPlayerPath) setPinnedPlayerPath(nextPin);

    return (
        <div data-testid="file-companion-window" className="file-companion-window" style={{ display: "flex", flexDirection: "column", height: "100vh", background: "#f3f5f7", color: "#1e293b" }}>
            <div className={openMenu || chatExportDestOpen ? "file-companion-titlebar file-companion-titlebar--menu-open" : "file-companion-titlebar"} data-testid="file-companion-titlebar" {...windowDragHandleProps(true, { display: "flex", gap: 8, padding: 8, alignItems: "center", flexWrap: "nowrap", borderBottom: "1px solid #d9e1ec", background: "#ffffff" })}>
                {chatExportDestOpen ? (
                    <div
                        className="file-companion-open-scrim"
                        data-testid="file-companion-export-chat-scrim"
                        onMouseDown={() => setChatExportDestPath("")}
                    />
                ) : null}
                <span className="mc-header-brand" data-testid="file-companion-brand" aria-label={`${brandName}伴读`}>
                    <span className="mc-header-brand-mark" aria-hidden="true">
                        <svg viewBox="0 0 80 80" focusable="false">
                            <path d="M14 58V22l26 25 26-25v36" fill="none" stroke="currentColor" strokeWidth="10.5" strokeLinecap="round" strokeLinejoin="round" />
                        </svg>
                    </span>
                    <span>{brandName}伴读</span>
                </span>
                <div className="file-companion-titlebar-files" data-testid="file-companion-titlebar-files">
                {tabEnds.overflow ? (
                    <button
                        type="button"
                        className="file-companion-tab-nudge"
                        data-testid="file-companion-tabs-left"
                        aria-label="向左移动文件标签"
                        title="向左移动文件标签"
                        disabled={!tabEnds.canLeft}
                        onClick={() => nudgeTabs(-1)}
                    >
                        {"<<"}
                    </button>
                ) : null}
                <div className="file-companion-titlebar-tabs" data-testid="file-companion-titlebar-tabs" role="tablist" aria-label="打开的文档" ref={tabsScrollRef}>
                {tabs.map((tab) => (
                    <button
                        key={tab.path}
                        type="button"
                        role="tab"
                        className="file-companion-tool file-companion-tab"
                        data-testid={`file-companion-tab-${tab.name}`}
                        title={tab.path}
                        aria-selected={tab.path === active}
                        ref={tab.path === active ? activeTabEl : undefined}
                        onClick={() => setActive(tab.path)}
                    >
                        <span className="file-companion-tab-name">{tab.name}{tab.dirty ? "*" : ""}</span>
                        <span
                            className="file-companion-tab-close"
                            role="button"
                            aria-label={`关闭 ${tab.name}`}
                            onClick={(event) => {
                                event.stopPropagation();
                                const closing = tab.path;
                                if (tab.kind === "web") {
                                    void FileCompanionDiscardWebNote(tab.sessionId).catch(() => undefined);
                                } else {
                                    void CancelFileCompanionTurn(closing).catch(() => undefined);
                                }
                                forgetIntro(closing);
                                const currentTabs = tabsRef.current;
                                const index = currentTabs.findIndex((item) => item.path === closing);
                                const rest = currentTabs.filter((item) => item.path !== closing);
                                setTabs((current) => current.filter((item) => item.path !== closing));
                                setActive((current) => {
                                    if (current !== closing) return current;
                                    return rest[Math.max(0, Math.min(index, rest.length - 1))]?.path ?? "";
                                });
                            }}
                        >
                            ×
                        </span>
                    </button>
                ))}
                </div>
                {tabEnds.overflow ? (
                    <button
                        type="button"
                        className="file-companion-tab-nudge"
                        data-testid="file-companion-tabs-right"
                        aria-label="向右移动文件标签"
                        title="向右移动文件标签"
                        disabled={!tabEnds.canRight}
                        onClick={() => nudgeTabs(1)}
                    >
                        {">>"}
                    </button>
                ) : null}
                </div>
                <div className="file-companion-open" data-testid="file-companion-open" data-window-no-drag="" ref={openWrapRef}>
                    <button
                        type="button"
                        ref={chooseButtonRef}
                        className="file-companion-tool file-companion-tab file-companion-tab--open"
                        data-testid="file-companion-choose"
                        aria-haspopup="true"
                        aria-expanded={openMenu}
                        aria-controls="file-companion-open-menu"
                        onClick={() => {
                            // A download in flight owns the panel; the toggle
                            // must not hide the progress it reports.
                            if (arxivBusyRef.current || arxivSearchRef.current) return;
                            setOpenMenu((current) => !current);
                        }}
                    >
                        打开
                        <span className="file-companion-open-caret" aria-hidden="true">
                            <svg viewBox="0 0 10 6" focusable="false">
                                <path d="M1 1l4 4 4-4" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
                            </svg>
                        </span>
                    </button>
                    {openMenu ? (
                        <>
                        {/* The PDF preview is an iframe, so a click on the page never
                            reaches the document listener that closes this panel. The
                            scrim sits in the title bar's stacking context, over that
                            frame, and takes the click instead. */}
                        <div
                            className="file-companion-open-scrim"
                            data-testid="file-companion-open-scrim"
                            onMouseDown={() => {
                                if (arxivBusyRef.current || sharePasswordRef.current || arxivSearchRef.current) return;
                                closeOpenMenu(false);
                            }}
                        />
                        <div className={playerHost === "native" && playerTab ? "file-companion-open-menu file-companion-open-menu--beside-player" : "file-companion-open-menu"} id="file-companion-open-menu" data-testid="file-companion-open-menu" role="group" aria-label="打开方式">
                            <button
                                type="button"
                                className="file-companion-open-item"
                                data-testid="file-companion-open-local"
                                disabled={arxivBusy || chooseBusy}
                                onClick={() => {
                                    if (sharePasswordRef.current || arxivSearchRef.current) return;
                                    closeOpenMenu(false);
                                    void chooseFiles();
                                }}
                            >
                                <CompanionActionIcon name="file" />
                                打开文档或导入项目
                            </button>
                            <div className="file-companion-open-sep" />
                            <div className="file-companion-open-row">
                                <input
                                    ref={arxivInputRef}
                                    className="file-companion-open-input"
                                    data-testid="file-companion-arxiv-input"
                                    aria-label={FILE_COMPANION_OPEN_HINT}
                                    value={arxivInput}
                                    disabled={arxivBusy}
                                    placeholder={FILE_COMPANION_OPEN_HINT}
                                    spellCheck={false}
                                    onChange={(event) => {
                                        setArxivInput(event.target.value);
                                        setArxivMessage("");
                                    }}
                                    onKeyDown={(event) => {
                                        if (event.key !== "Enter") return;
                                        event.preventDefault();
                                        void openReference();
                                    }}
                                />
                                <button
                                    type="button"
                                    className="file-companion-open-go"
                                    data-testid="file-companion-arxiv-go"
                                    disabled={arxivBusy || arxivInput.trim() === ""}
                                    aria-busy={arxivBusy}
                                    onClick={() => { void openReference(); }}
                                >
                                    {arxivBusy ? (looksLikeCompanionShare(arxivInput) ? "正在导入…" : looksLikeArxivReference(arxivInput) ? "下载中…" : "打开中…") : "确定"}
                                </button>
                                <button
                                    type="button"
                                    ref={arxivSearchButtonRef}
                                    className="file-companion-open-go file-companion-open-search"
                                    data-testid="file-companion-arxiv-search"
                                    disabled={arxivBusy || sharePasswordLink !== ""}
                                    aria-haspopup="dialog"
                                    aria-expanded={arxivSearchOpen}
                                    aria-controls="file-companion-arxiv-search-title"
                                    onClick={openArxivSearch}
                                >
                                    搜索
                                </button>
                            </div>
                            <p
                                className="file-companion-open-hint"
                                data-testid="file-companion-arxiv-hint"
                                data-state={arxivMessage ? "error" : "hint"}
                                role="status"
                            >
                                {arxivMessage || FILE_COMPANION_OPEN_HINT}
                            </p>
                        </div>
                        </>
                    ) : null}
                </div>
                <div className="file-companion-library-anchor" data-testid="file-companion-library-anchor" data-window-no-drag="">
                    <button
                        type="button"
                        className="file-companion-tool file-companion-tool--icon"
                        data-testid="file-companion-library"
                        aria-label="文档管理"
                        aria-pressed={view === "library"}
                        title="文档管理 — 本机文档与云端项目"
                        onClick={() => {
                            if (arxivBusyRef.current) return;
                            setChatExportDestPath("");
                            setOpenMenu(false);
                            setView((current) => current === "library" ? "read" : "library");
                        }}
                    >
                        <CompanionActionIcon name="library" />
                        <span className="file-companion-sr">文档管理</span>
                    </button>
                    <button
                        type="button"
                        className="file-companion-tool file-companion-tool--icon"
                        data-testid="file-companion-groups-open"
                        aria-label="阅读群组"
                        aria-pressed={view === "groups"}
                        title="阅读群组 — 创建群组、邀请成员、分享给群组"
                        onClick={() => {
                            if (arxivBusyRef.current) return;
                            setChatExportDestPath("");
                            setOpenMenu(false);
                            setView((current) => current === "groups" ? "read" : "groups");
                        }}
                    >
                        <CompanionActionIcon name="groups" />
                        <span className="file-companion-sr">阅读群组</span>
                    </button>
                </div>
                <div className="file-companion-titlebar-gap" data-testid="file-companion-titlebar-gap" />
                {activeTab ? (
                    <div className="file-companion-titlebar-actions" data-testid="file-companion-export">
                        <button
                            type="button"
                            className="file-companion-tool file-companion-tool--icon"
                            data-testid="file-companion-share-open"
                            disabled={Boolean(activeTab.error) || isWebTab || !activeTab.path}
                            aria-label="分享"
                            title="分享 — 上传文档、高亮和聊天，生成云端链接"
                            onClick={() => { setShareDialogPath(activeTab.path); }}
                        >
                            <CompanionActionIcon name="share" />
                            <span className="file-companion-sr">分享</span>
                        </button>
                        {showConvert ? (
                        <button
                            type="button"
                            className="file-companion-tool file-companion-tool--icon"
                            data-testid="file-companion-convert"
                            disabled={Boolean(activeTab.error) || sendBusy || paperBusy || convertBusy}
                            aria-busy={convertBusy}
                            aria-label={convertBusy ? "正在转换" : "转换"}
                            title={convertBusy ? convertTitle : "转换 — 保留扫描版式，生成可选中、可搜索的 PDF"}
                            onClick={() => { void convertScan(); }}
                        >
                            <CompanionActionIcon name="convert" />
                            <span className="file-companion-sr">{convertBusy ? "正在转换…" : "转换"}</span>
                        </button>
                        ) : null}
                        {isWebTab || kind === "video" || kind === "audio" ? null : (
                        <button
                            type="button"
                            className="file-companion-tool file-companion-tool--icon"
                            data-testid="file-companion-paper"
                            disabled={Boolean(activeTab.error) || sendBusy || convertBusy}
                            aria-busy={paperBusy}
                            aria-label="论文解读"
                            title={paperBusy ? "正在解读…" : "论文解读 — 生成本文的结构化解读报告"}
                            onClick={() => { void sendPaper(); }}
                        >
                            <CompanionActionIcon name="paper" />
                            <span className="file-companion-sr">{paperBusy ? "正在解读…" : "论文解读"}</span>
                        </button>
                        )}
                        {isWebTab || kind === "video" || kind === "audio" ? (
                        <button
                            type="button"
                            className="file-companion-tool file-companion-tool--icon"
                            data-testid="file-companion-video-note"
                            disabled={noteBusy ? false : Boolean(activeTab.error) || sendBusy}
                            aria-label={noteBusy ? (isWebTab ? "停止" : "取消") : (isWebTab ? "记笔记" : "生成笔记")}
                            title={noteBusy ? (isWebTab ? "停止 — 结束网页笔记" : "取消 — 结束笔记生成") : (isWebTab ? "记笔记 — 把网页内容整理成笔记" : "生成笔记 — 把视频、音频内容整理成笔记")}
                            onClick={() => { void sendNote(); }}
                        >
                            <CompanionActionIcon name={noteBusy ? "stop" : "note"} />
                            <span className="file-companion-sr">{noteBusy ? (isWebTab ? "停止" : "取消") : (isWebTab ? "记笔记" : "生成笔记")}</span>
                        </button>
                        ) : null}
                        <div className="file-companion-export-anchor" data-testid="file-companion-export-anchor" ref={chatExportWrapRef}>
                        <button
                            type="button"
                            ref={chatExportButtonRef}
                            className="file-companion-tool file-companion-export-chat"
                            data-testid="file-companion-export-chat"
                            disabled={!canExportChat}
                            aria-busy={chatExportBusy}
                            aria-expanded={chatExportDestOpen}
                            aria-haspopup="menu"
                            aria-controls="file-companion-export-chat-dest"
                            aria-label="导出 PDF"
                            title={canExportChat || chatExportBusy ? "导出 PDF — 按聊天区原来的格式，把全部对话导出" : hasChatHistory ? "没有可导出的正文" : "对话为空，没有可导出的内容"}
                            onClick={() => {
                                if (!canExportChat || !activeTab) return;
                                setChatTexOpen(false);
                                setOpenMenu(false);
                                // The export panel hangs from the title bar, above
                                // the reader. A management screen would cover it.
                                setView("read");
                                setChatExportDestPath((current) => current === activeTab.path ? "" : activeTab.path);
                            }}
                        >
                            <CompanionActionIcon name="pdf" />
                            <span className="file-companion-sr">{chatExportBusy ? "正在导出…" : "导出 PDF"}</span><span data-testid="file-companion-export-chat-menu" role="button" aria-label="导出格式" onClick={(event) => { event.preventDefault(); event.stopPropagation(); setChatExportDestPath(""); setChatTexOpen((current) => !current); }}><svg viewBox="0 0 10 6" aria-hidden="true" focusable="false"><path d="M1 1l4 4 4-4" fill="none" stroke="currentColor" strokeWidth="1.4" /></svg></span>{chatTexOpen ? <span data-testid="file-companion-export-chat-tex" role="menuitem" onClick={(event) => { event.preventDefault(); event.stopPropagation(); setChatTexOpen(false); void exportChatTeX(); }}>LaTeX</span> : null}
                        </button>
                        {chatExportDestOpen ? (
                            <div
                                className="file-companion-export-menu"
                                id="file-companion-export-chat-dest"
                                data-testid="file-companion-export-chat-dest"
                                role="menu"
                                aria-label="导出 PDF"
                            >
                                <button
                                    type="button"
                                    role="menuitem"
                                    className="file-companion-open-item"
                                    data-testid="file-companion-export-chat-file"
                                    onClick={() => {
                                        setChatExportDestPath("");
                                        void exportChat();
                                    }}
                                >
                                    导出文件
                                </button>
                                <button
                                    type="button"
                                    role="menuitem"
                                    className="file-companion-open-item"
                                    data-testid="file-companion-export-chat-cloud"
                                    onClick={() => {
                                        setChatExportDestPath("");
                                        void exportChatCloud();
                                    }}
                                >
                                    导出到云盘
                                </button>
                            </div>
                        ) : null}
                        </div>
                        {isWebTab ? null : (
                        <button
                            type="button"
                            className="file-companion-tool file-companion-tool--icon"
                            data-testid="file-companion-export-project"
                            disabled={!activeTab.canExport || projectExportBusy}
                            aria-busy={projectExportBusy}
                            aria-label="导出项目"
                            title={projectExportBusy ? "正在导出…" : "导出项目 — 文档、高亮和聊天记录打包成项目"}
                            onClick={() => { void exportProject(); }}
                        >
                            <CompanionActionIcon name="project" />
                            <span className="file-companion-sr">{projectExportBusy ? "正在导出…" : "导出项目"}</span>
                        </button>
                        )}
                        <button
                            type="button"
                            className="file-companion-tool file-companion-tool--icon"
                            data-testid="file-companion-import"
                            disabled={!activeTab.canExport || importBusy}
                            aria-busy={importBusy}
                            aria-label="导入知识库"
                            title={importBusy ? "正在导入…" : "导入知识库 — 存入知识库，以后可以检索"}
                            onClick={() => { void runExport("import"); }}
                        >
                            <CompanionActionIcon name="knowledge" />
                            <span className="file-companion-sr">{importBusy ? "正在导入…" : "导入知识库"}</span>
                        </button>
                        <button
                            type="button"
                            className="file-companion-tool file-companion-tool--icon"
                            data-testid="file-companion-upload"
                            disabled={cloudDisabled || uploadBusy || uploaded}
                            aria-busy={uploadBusy}
                            aria-label={uploaded ? "已上传到云盘" : "上传到云盘"}
                            title={activeTab.size > CLOUD_DISABLE_BYTES ? "文件太大，无法上传到云盘" : uploaded ? "已上传到云盘 — 手机端「文档」可打开" : uploadBusy ? "正在上传…" : "上传到云盘 — 手机端「文档」可打开"}
                            onClick={() => { void runExport("upload"); }}
                        >
                            <CompanionActionIcon name={uploaded ? "check" : "cloud"} />
                            <span className="file-companion-sr">{uploadBusy ? "正在上传…" : "上传到云盘"}</span>
                        </button>
                    </div>
                ) : null}
                <button type="button" className="file-companion-tool file-companion-tool--icon file-companion-tool--fullscreen" data-testid="file-companion-fullscreen" aria-pressed={readingFull} aria-label={readingFull ? "恢复原始大小" : "全屏"} title={readingFull ? "恢复进入全屏前的窗口大小" : "全屏阅读"} onClick={() => { void toggleReadingFrame(); }}><CompanionActionIcon name={readingFull ? "restore" : "fullscreen"} /><span className="file-companion-sr">{readingFull ? "恢复原始大小" : "全屏"}</span></button>
                {/* runtime WindowHide. App.WindowHide also shows the tray pet. */}
                <button type="button" className="file-companion-tool file-companion-tool--icon file-companion-tool--close" data-testid="file-companion-close" aria-label="隐藏" title="隐藏窗口，已打开的文件会保留" onClick={() => { windowHiddenRef.current = true; windowRevealRef.current = false; WindowHide(); }}><CompanionActionIcon name="hide" /><span className="file-companion-sr">隐藏</span></button>
            </div>
            {dropMessage ? <div data-testid="file-companion-drop-error" style={{ padding: "8px 12px", color: "#9a3412" }}>{dropMessage}</div> : null}
            {/* The reading view stays mounted under the management screens. A
                native web player is placed from this subtree's slot; unmounting
                it would empty the box and unload the page, which stops a note
                that is still recording. The screens paint over it instead. */}
            {!activeTab ? (
                <div data-testid="file-companion-empty" style={{ flex: 1, display: "flex", alignItems: "center", justifyContent: "center" }}>
                    把文件拖到这里，或点击「打开」
                </div>
            ) : (
                <div className="file-companion-split" data-testid="file-companion-split" ref={splitRef}>
                    <section className="file-companion-split-preview">
                        {holdPlayer || !activeTab.error ? null : <div data-testid="file-companion-file-error">{fileErrorText(activeTab.error)}</div>}
                        {!holdPlayer && kind === "html" && !activeTab.error ? (
                            <button
                                type="button"
                                className="file-companion-tool"
                                data-testid="file-companion-html-toggle"
                                onClick={() => updateActive({ htmlPreview: !activeTab.htmlPreview })}
                            >
                                {activeTab.htmlPreview ? "源码" : "预览"}
                            </button>
                        ) : null}
                        {!holdPlayer && showSource && kind === "markdown" ? (
                            <MarkdownSourceEditor
                                key={activeTab.path}
                                value={activeTab.content}
                                readOnly={!activeTab.editable}
                                onChange={(content) => updateActive({ content, dirty: true, conflict: false, saveError: "" })}
                                onSelect={rememberSelection}
                            />
                        ) : null}
                        {!holdPlayer && showSource && kind !== "markdown" ? (
                            <textarea
                                data-testid="file-companion-editor"
                                value={activeTab.content}
                                readOnly={!activeTab.editable}
                                spellCheck={false}
                                onChange={(event) => {
                                    if (!activeTab.editable) return;
                                    const el = event.target;
                                    const picked = selectionFromEditor(el.value, el.selectionStart, el.selectionEnd);
                                    updateActive({ content: el.value, dirty: true, conflict: false, saveError: "", selection: picked.selection, selectionStart: picked.selectionStart });
                                }}
                                onSelect={(event) => {
                                    const el = event.currentTarget;
                                    if (editorValueChanged && el.selectionStart === el.selectionEnd) return;
                                    const picked = selectionFromEditor(el.value, el.selectionStart, el.selectionEnd);
                                    rememberSelection(picked.selection, picked.selectionStart);
                                }}
                                style={{ flex: 1, minHeight: 180, border: "1px solid #e2e8f0", borderRadius: 8, padding: 12 }}
                            />
                        ) : null}
                        {!holdPlayer && activeTab.readOnlyReason === "permission" ? <p data-testid="file-companion-readonly-note">没有写入权限</p> : null}
                        {!holdPlayer && activeTab.readOnlyReason === "too-large" ? <p data-testid="file-companion-readonly-note">文件过大，为避免截断写回，已禁止保存</p> : null}
                        {!holdPlayer && emptyFile ? <div data-testid="file-companion-empty-file">文件是空的</div> : null}
                        {!holdPlayer && unsupported ? (
                            <div data-testid="file-companion-unsupported">
                                <p>此文件类型暂不支持预览</p>
                                <p>{activeTab.name}</p>
                                <p>{activeTab.size} 字节</p>
                            </div>
                        ) : null}
                        {playerTab && playerHost !== "pending" ? (
                            <div data-testid="file-companion-web" style={{ flex: 1, minHeight: 160 }}>
                                {playerHost === "native" ? (
                                    // The native WebView paints the page here. The page is the
                                    // top-level document of the companion profile, not a frame
                                    // inside wails.localhost, so its sign-in cookies stay with it.
                                    <div
                                        ref={(node) => { webFrameRef.current = node; }}
                                        data-testid="file-companion-web-frame"
                                        data-url={playerTab.frameUrl}
                                        role="region"
                                        aria-label={playerTab.name}
                                        style={{ width: "100%", height: "100%", background: "#000" }}
                                    />
                                ) : (
                                    <iframe
                                        ref={(node) => { webFrameRef.current = node; }}
                                        data-testid="file-companion-web-frame"
                                        src={playerTab.frameUrl}
                                        title={playerTab.name}
                                        referrerPolicy="strict-origin-when-cross-origin"
                                        allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture; web-share; fullscreen"
                                        allowFullScreen
                                        style={{ width: "100%", height: "100%", border: 0 }}
                                    />
                                )}
                            </div>
                        ) : null}
                        {!holdPlayer && showPreviewPane ? (
                            <div data-testid="file-companion-preview" style={{ flex: 1, minHeight: 160 }}>
                                <FilePreviewView
                                    key={`${activeTab.path}:${activeTab.previewEpoch}`}
                                    file={{ fileName: activeTab.name, filePath: activeTab.path, absPath: activeTab.path, content: activeTab.content, updatedAt: activeTab.previewEpoch }}
                                    theme={companionPreviewTheme}
                                    lang="zh-Hans"
                                    pdfAnnotate={kind === "pdf"}
                                />
                            </div>
                        ) : null}
                        {!holdPlayer && activeTab.saveError ? <p data-testid="file-companion-save-error">{turnErrorText(activeTab.saveError)}</p> : null}
                        {!holdPlayer && activeTab.conflict ? (
                            <div data-testid="file-companion-conflict">
                                文件在磁盘上已被修改
                                <button type="button" className="file-companion-tool" onClick={() => { void openPath(activeTab.path, false); }}>重新加载</button>
                                <button type="button" className="file-companion-tool" onClick={() => {
                                    if (!activeTab.confirmOverwrite) {
                                        updateActive({ confirmOverwrite: true });
                                        return;
                                    }
                                    void saveTab(activeTab, activeTab.diskHash);
                                }}>{activeTab.confirmOverwrite ? "再次确认覆盖" : "覆盖保存"}</button>
                            </div>
                        ) : null}
                    </section>
                    <div
                        className="file-companion-split-handle"
                        data-testid="file-companion-split-handle"
                        role="separator"
                        aria-orientation="vertical"
                        aria-valuemin={COMPANION_CHAT_MIN}
                        aria-valuemax={clampCompanionChatWidth(Number.MAX_SAFE_INTEGER, splitRef.current?.clientWidth ?? 0)}
                        aria-valuenow={chatWidth}
                        aria-label="拖动调整左右宽度"
                        tabIndex={0}
                        title="拖动调整左右宽度"
                        onPointerDown={onSplitPointerDown}
                        onPointerMove={onSplitPointerMove}
                        onPointerUp={onSplitPointerEnd}
                        onPointerCancel={onSplitPointerEnd}
                        onKeyDown={(event) => {
                            const delta = event.key === "ArrowLeft" ? 16 : event.key === "ArrowRight" ? -16 : 0;
                            if (delta !== 0) {
                                event.preventDefault();
                                resizeChat(chatWidthRef.current + delta);
                            } else if (event.key === "Home" || event.key === "End") {
                                event.preventDefault();
                                resizeChat(event.key === "Home" ? COMPANION_CHAT_MIN : Number.MAX_SAFE_INTEGER);
                            }
                        }}
                    />
                    <section className="file-companion-chat" data-testid="file-companion-chat" style={{ width: chatWidth }}>
                        {activeTab.status ? <CompanionStatus text={activeTab.status} kind={tabNoteKind(activeTab)} noteStartedAt={activeTab.noteStartedAt} phaseStartedAt={activeTab.phaseStartedAt} /> : null}
                        {convertCard ? (
                            <div data-testid="file-companion-convert-result">
                                <p style={{ margin: "12px 16px 0", fontSize: 13 }}>{convertCard.message || "已生成可选中文字的 PDF，原文件没有改动。"}</p>
                                <PaperResultCard filePath={convertCard.pdf} messageId="convert" />
                            </div>
                        ) : null}
                        <div
                            ref={outputContainerRef}
                            data-testid="file-companion-chat-log"
                            onScroll={handleScroll}
                            onWheel={handleUserScrollIntent}
                            onTouchMove={handleUserScrollIntent}
                            onPointerDown={handleUserScrollIntent}
                            style={{ flex: 1, overflow: "auto", padding: 16, display: "flex", flexDirection: "column", gap: 10 }}
                        >
                            {activeTab.messages.length === 0 ? <p data-testid="file-companion-chat-empty" style={{ margin: 0, color: "#94a3b8", fontSize: 13 }}>针对当前文件提问</p> : null}
                            {activeTab.messages.map((message, index) => {
                                const mine = message.role === "user";
                                const turnLive = Boolean(activeTab.status) || sendingPaths.has(activeTab.path);
                                const streaming = !mine && turnLive && index === activeTab.messages.length - 1;
                                const cleanedReasoning = mine ? "" : cleanReasoningTrailForBody(message.reasoning || "");
                                const visibleReply = mine
                                    ? { content: message.text, reasoning: "" }
                                    : resolveVisibleAssistantReply(message.text, cleanedReasoning, { live: streaming });
                                const displayReasoning = stripAssistantToolCallMarkers(visibleReply.reasoning).trim();
                                                const paperPending = !mine && !message.resultPath && paperRequestIds.current.has(paperRequestOf(message.id));
                                                const answerText = message.resultPath ? "已生成论文解读" : paperPending ? "正在生成论文解读…" : companionMessageExportText(message, streaming);
                                                // Both controls work on finished Markdown. Mid-stream text would copy a
                                                // half answer, and the paper reply exports through its own result card.
                                                const shareable = !mine && !streaming && !message.resultPath && !paperPending && answerText.trim().length > 0;
                                                return (
                                                    <div key={message.id} style={{ display: "flex", justifyContent: mine ? "flex-end" : "flex-start" }}>
                                        <div
                                            data-testid={`file-companion-message-${message.role}`}
                                            style={{
                                                margin: 0,
                                                maxWidth: "86%",
                                                minWidth: 0,
                                                padding: "8px 12px",
                                                borderRadius: mine ? "14px 14px 4px 14px" : "14px 14px 14px 4px",
                                                background: mine ? "#2f6fbc" : "#ffffff",
                                                color: mine ? "#ffffff" : "#1e293b",
                                                border: mine ? "none" : "1px solid #e2e8f0",
                                                fontSize: 14,
                                                lineHeight: 1.55,
                                                overflowWrap: "anywhere",
                                            }}
                                            >
                                                {shareable ? <CompanionMessageActions path={activeTab.path} markdown={answerText} messageId={message.id} figures={message.figures} webSession={activeTab.kind === "web" ? activeTab.sessionId : undefined} /> : null}
                                                {displayReasoning ? (
                                                    <AssistantReasoningPanel
                                                        defaultOpen={streaming}
                                                    label={streaming ? "正在思考" : "思考过程"}
                                                    lang="zh-Hans"
                                                    theme={lightTheme}
                                                    contentKey={displayReasoning}
                                                    live={streaming}
                                                >
                                                    {renderContentWithCodeBlocks(displayReasoning, lightTheme, reasoningTrailMarkdownOptions)}
                                                </AssistantReasoningPanel>
                                            ) : null}
                                            <div data-testid={mine ? undefined : "file-companion-answer"}>
                                                {mine || message.resultPath ? (
                                                <MessageContentRenderer
                                                    content={answerText}
                                                    theme={lightTheme}
                                                    isUser={mine}
                                                    isStreaming={streaming && !message.resultPath}
                                                    messageId={message.id}
                                                />
                                                ) : (
                                                <NoteStampAnswer
                                                    text={answerText}
                                                    path={activeTab.path}
                                                    web={Boolean(message.webNote || activeTab.kind === "web")}
                                                    previewOpen={showPreview}
                                                    messageId={message.id}
                                                    streaming={streaming}
                                                    onBlocked={(sentence) => showToast(sentence, "error", 6000)}
                                                />
                                                )}
                                                {message.resultPath ? <PaperResultCard filePath={message.resultPath} messageId={message.id} /> : null}
                                                {message.noteCardPath ? <NoteResultCard filePath={message.noteCardPath} messageId={message.id} /> : null}
                                                {!mine && message.figures && message.figures.length > 0 ? (
                                                    <CompanionFigureStrip figures={message.figures} messageId={message.id} />
                                                ) : null}
                                            </div>
                                        </div>
                                    </div>
                                );
                            })}
                            <div ref={outputEndRef} />
                        </div>
                        {activeTab.shareToken && (!activeTab.shareMine || (activeTab.notes && activeTab.notes.length > 0)) ? (
                            <div className="file-companion-annotations" data-testid="file-companion-annotations">
                                <p className="file-companion-annotations-kicker">批注</p>
                                {activeTab.notes && activeTab.notes.length > 0 ? activeTab.notes.map((note) => (
                                    <p key={note.id} data-testid={`file-companion-annotation-${note.id}`}>
                                        <span className="file-companion-annotations-author">{note.author || "读者"}</span>
                                        {note.text}
                                    </p>
                                )) : (
                                    <p className="file-companion-annotations-empty">你的讨论会成为批注，同步给能看到这份分享的人。</p>
                                )}
                            </div>
                        ) : null}
                        <FileCompanionComposer
                            path={activeTab.path}
                            value={activeTab.draft}
                            busy={sendBusy}
                            onChange={(draft) => updateActive({ draft })}
                            onSubmit={() => send()}
                        />
                    </section>
                </div>
            )}
            {view === "library" ? (
                <CompanionLibraryView
                    activePath={active}
                    showToast={showToast}
                    onOpen={(path) => { void openPath(path, false, false, 0, true); setView("read"); }}
                    onExit={() => setView("read")}
                    onShareUpdated={(update) => { applyShareUpdateRef.current(update); }}
                    onListed={(listed) => { applyLibraryListRef.current(listed); }}
                    onShareProject={(path) => { setImportDialogLink(""); setShareDialogPath(path); }}
                    onImportLink={(link) => { setImportDialogLink(link); setImportDialogOpen(true); }}
                    dialogOpen={Boolean(shareDialogPath) || importDialogOpen}
                />
            ) : null}
            {view === "groups" ? (
                <CompanionGroupsDialog
                    showToast={showToast}
                    onClose={() => setView("read")}
                />
            ) : null}
            {shareDialogPath ? (
                <CompanionShareDialog
                    path={shareDialogPath}
                    name={shareDialogName}
                    showToast={showToast}
                    onListed={(listed) => { applyLibraryListRef.current(listed); }}
                    onClose={() => setShareDialogPath("")}
                />
            ) : null}
            {importDialogOpen ? (
                <CompanionImportDialog
                    initialLink={importDialogLink}
                    showToast={showToast}
                    onOpen={(path) => { void openPath(path, false, false, 0, true); setView("read"); }}
                    onShareUpdated={(update) => { applyShareUpdateRef.current(update); }}
                    onListed={(listed) => { applyLibraryListRef.current(listed); }}
                    onClose={() => { setImportDialogOpen(false); setImportDialogLink(""); }}
                />
            ) : null}
            {sharePasswordLink ? (
                <div ref={sharePasswordDialogRef} className={playerHost === "native" && playerTab ? "file-companion-share-password file-companion-share-password--beside-player" : "file-companion-share-password"} data-testid="file-companion-share-password" role="dialog" aria-modal="true" aria-labelledby="file-companion-share-password-title" aria-describedby="file-companion-share-password-hint">
                    <div
                        className="file-companion-dialog-scrim"
                        data-testid="file-companion-share-password-scrim"
                        onMouseDown={() => closeSharePassword()}
                    />
                    <form
                        className="file-companion-dialog-card file-companion-share-password-card"
                        autoComplete="off"
                        onSubmit={(event) => {
                            event.preventDefault();
                            submitSharePassword();
                        }}
                    >
                        <header className="file-companion-dialog-head">
                            <span className="file-companion-share-password-mark" aria-hidden="true">
                                <SharePasswordGlyph name="lock" />
                            </span>
                            <div className="file-companion-dialog-heading">
                                <h2 id="file-companion-share-password-title">输入分享密码</h2>
                            </div>
                            <p id="file-companion-share-password-hint" className="file-companion-dialog-sub">该文档为私密分享，需要密码才能导入。</p>
                        </header>
                        <div className="file-companion-dialog-body">
                            <section className="file-companion-dialog-section">
                                <div className="file-companion-share-password-field">
                                    <input
                                        ref={sharePasswordInputRef}
                                        className="file-companion-dialog-input"
                                        data-testid="file-companion-share-password-input"
                                        type={sharePasswordVisible ? "text" : "password"}
                                        aria-label="分享密码"
                                        aria-invalid={sharePasswordError ? true : undefined}
                                        aria-describedby={sharePasswordError ? "file-companion-share-password-error" : undefined}
                                        autoComplete="new-password"
                                        spellCheck={false}
                                        placeholder="分享密码"
                                        value={sharePasswordValue}
                                        readOnly={arxivBusy}
                                        onChange={(event) => {
                                            setSharePasswordValue(event.target.value);
                                            setSharePasswordError("");
                                        }}
                                    />
                                    <button
                                        type="button"
                                        className="file-companion-share-password-reveal"
                                        aria-label={sharePasswordVisible ? "隐藏密码" : "显示密码"}
                                        aria-pressed={sharePasswordVisible}
                                        disabled={arxivBusy}
                                        onMouseDown={(event) => {
                                            // Chromium focuses a button on mousedown. That blur
                                            // drops the caret, and the next characters never
                                            // reach the password field.
                                            event.preventDefault();
                                        }}
                                        onClick={() => {
                                            setSharePasswordVisible((shown) => !shown);
                                            sharePasswordInputRef.current?.focus();
                                        }}
                                    >
                                        <SharePasswordGlyph name={sharePasswordVisible ? "eye-off" : "eye"} />
                                    </button>
                                </div>
                                {sharePasswordError ? (
                                    <p className="file-companion-dialog-error" id="file-companion-share-password-error" data-testid="file-companion-share-password-error" data-state="error" role="status">{sharePasswordError}</p>
                                ) : null}
                            </section>
                        </div>
                        <footer className="file-companion-dialog-foot">
                            <div className="file-companion-dialog-actions">
                                <button type="button" className="file-companion-dialog-secondary" data-testid="file-companion-share-password-cancel" disabled={arxivBusy} onClick={closeSharePassword}>取消</button>
                                <button type="submit" className="file-companion-dialog-primary" data-testid="file-companion-share-password-confirm" disabled={arxivBusy} aria-busy={arxivBusy}>{arxivBusy ? "正在导入…" : "导入"}</button>
                            </div>
                        </footer>
                    </form>
                </div>
            ) : null}
            {arxivSearchOpen ? (
                <div
                    ref={arxivSearchDialogRef}
                    className={playerHost === "native" && playerTab ? "file-companion-arxiv-search file-companion-arxiv-search--beside-player" : "file-companion-arxiv-search"}
                    data-testid="file-companion-arxiv-search-dialog"
                    role="dialog"
                    aria-modal="true"
                    aria-labelledby="file-companion-arxiv-search-title"
                    aria-describedby="file-companion-arxiv-search-status"
                    aria-busy={arxivSearchBusy || arxivBusy}
                >
                    <div
                        className="file-companion-dialog-scrim"
                        data-testid="file-companion-arxiv-search-scrim"
                        onMouseDown={() => closeArxivSearch()}
                    />
                    <form
                        className="file-companion-dialog-card file-companion-arxiv-search-card"
                        onSubmit={(event) => {
                            event.preventDefault();
                            void runArxivSearch();
                        }}
                    >
                        <header className="file-companion-dialog-head">
                            <div className="file-companion-dialog-heading">
                                <h2 id="file-companion-arxiv-search-title">arXiv 文献检索</h2>
                                <p className="file-companion-dialog-sub">公开预印本，最多 10 篇</p>
                            </div>
                            <button
                                type="button"
                                className="file-companion-dialog-x"
                                data-testid="file-companion-arxiv-search-close"
                                aria-label="关闭"
                                disabled={arxivBusy}
                                onClick={closeArxivSearch}
                            >
                                ×
                            </button>
                        </header>
                        <div className="file-companion-arxiv-search-bar">
                            <div className="file-companion-arxiv-search-row">
                                <span className="file-companion-arxiv-search-glyph" aria-hidden="true">
                                    <CompanionActionIcon name="paper" size={15} />
                                </span>
                                <input
                                    ref={arxivSearchInputRef}
                                    className="file-companion-dialog-input file-companion-arxiv-search-input"
                                    data-testid="file-companion-arxiv-search-input"
                                    aria-label="搜索关键字或论文编号"
                                    placeholder="关键字、作者，或论文编号"
                                    value={arxivSearchQuery}
                                    readOnly={arxivSearchBusy || arxivBusy}
                                    spellCheck={false}
                                    onChange={(event) => {
                                        setArxivSearchQuery(event.target.value);
                                        setArxivSearchError("");
                                    }}
                                />
                                <button
                                    type="submit"
                                    className="file-companion-dialog-primary"
                                    data-testid="file-companion-arxiv-search-go"
                                    disabled={arxivSearchBusy || arxivBusy}
                                    aria-busy={arxivSearchBusy}
                                >
                                    {arxivSearchBusy ? "检索中…" : "检索"}
                                </button>
                            </div>
                            <p
                                id="file-companion-arxiv-search-status"
                                className={arxivSearchError ? "file-companion-arxiv-search-status file-companion-arxiv-search-status--error" : "file-companion-arxiv-search-status"}
                                data-testid="file-companion-arxiv-search-status"
                                data-state={arxivSearchError ? "error" : "hint"}
                                role="status"
                            >
                                {companionArxivSearchStatus(arxivSearchError, arxivSearchQuery, arxivSearchFor, arxivSearched, arxivSearchHits.length, arxivSearchBusy)}
                            </p>
                        </div>
                        {arxivSearchBusy ? (
                            // The first search has nothing to show yet. Placeholder
                            // rows hold the list's shape so the card does not jump
                            // from an empty box to ten results.
                            <div className="file-companion-dialog-body" data-testid="file-companion-arxiv-search-skeleton" aria-hidden="true">
                                <ul className="file-companion-arxiv-hits">
                                    {[0, 1, 2, 3].map((row) => (
                                        <li key={row} className="file-companion-arxiv-skeleton">
                                            <span className="file-companion-arxiv-skeleton-line file-companion-arxiv-skeleton-line--title" />
                                            <span className="file-companion-arxiv-skeleton-line file-companion-arxiv-skeleton-line--meta" />
                                            <span className="file-companion-arxiv-skeleton-line file-companion-arxiv-skeleton-line--abstract" />
                                            <span className="file-companion-arxiv-skeleton-line file-companion-arxiv-skeleton-line--abstract-short" />
                                        </li>
                                    ))}
                                </ul>
                            </div>
                        ) : null}
                        {!arxivSearchBusy && arxivSearchHits.length > 0 ? (
                            <div className="file-companion-dialog-body">
                                <ul className="file-companion-arxiv-hits" aria-label="搜索结果">
                                    {arxivSearchHits.map((hit, index) => {
                                        const opening = arxivOpeningId === hit.id;
                                        return (
                                            <li key={hit.id}>
                                                <button
                                                    type="button"
                                                    className="file-companion-arxiv-hit"
                                                    data-testid={`file-companion-arxiv-hit-${hit.id.replace(/[\\/]/g, "_")}`}
                                                    // aria-disabled rather than disabled: the global
                                                    // button:disabled rule dims the whole row and the
                                                    // generic button[aria-busy] rule draws its own
                                                    // spinner beside the citation. The click handler
                                                    // already refuses while a download runs.
                                                    aria-disabled={arxivSearchBusy || arxivBusy}
                                                    aria-busy={opening}
                                                    onClick={() => { void openArxivHit(hit.id); }}
                                                >
                                                    <span className="file-companion-arxiv-hit-index" aria-hidden="true">{index + 1}</span>
                                                    <span className="file-companion-arxiv-hit-body">
                                                        <span className="file-companion-arxiv-hit-title">{hit.title}</span>
                                                        <span className="file-companion-arxiv-hit-meta">
                                                            <span className="file-companion-arxiv-hit-id">{hit.id}</span>
                                                            {hit.categories.map((category) => (
                                                                <span key={category} className="file-companion-arxiv-hit-tag">{category}</span>
                                                            ))}
                                                            {hit.published ? <span className="file-companion-arxiv-hit-date">{hit.published}</span> : null}
                                                        </span>
                                                        {hit.authors ? <span className="file-companion-arxiv-hit-authors">{hit.authors}</span> : null}
                                                        {hit.abstract ? <span className="file-companion-arxiv-hit-abstract">{hit.abstract}</span> : null}
                                                    </span>
                                                    <span className="file-companion-arxiv-hit-action">
                                                        {opening ? <span className="file-companion-arxiv-hit-status">正在下载…</span> : <span className="file-companion-arxiv-hit-open">打开</span>}
                                                    </span>
                                                </button>
                                            </li>
                                        );
                                    })}
                                </ul>
                            </div>
                        ) : null}
                        {!arxivSearchBusy && arxivSearched && arxivSearchHits.length === 0 && !arxivSearchError ? (
                            // An empty list is a result, not a dead end. Naming the
                            // query that came back empty is what makes it useful: the
                            // reader can see the box and the answer disagree, instead
                            // of reading a second copy of "no results". It belongs to
                            // the query that produced it, so a keystroke drops it —
                            // the status line above is already back to its idle
                            // sentence, and the two must not disagree.
                            arxivSearchQuery.trim() === arxivSearchFor ? (
                                <div className="file-companion-dialog-body" data-testid="file-companion-arxiv-search-empty">
                                    <div className="file-companion-arxiv-empty">
                                        <p className="file-companion-arxiv-empty-title">没有与「{arxivSearchFor}」匹配的论文</p>
                                        <p className="file-companion-arxiv-empty-hint">换一个更常见的术语，或在上方输入 arXiv 编号（如 2510.16079）后检索。</p>
                                    </div>
                                </div>
                            ) : null
                        ) : null}
                    </form>
                </div>
            ) : null}
        </div>
    );
}
