import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import {
    ChooseFileCompanionFiles,
    ClearFileCompanionChat,
    FileCompanionImportKnowledge,
    FileCompanionOpen,
    FileCompanionReplaceOffice,
    FileCompanionSaveText,
    FileCompanionUIReady,
    FileCompanionUploadCloud,
    GetFileCompanionBoot,
    SendFileCompanionMessage,
} from "../../../wailsjs/go/main/App";
import { WindowHide } from "../../../wailsjs/runtime";
import { useToast } from "../Toast";
import { AssistantReasoningPanel } from "../ai/AssistantReasoningPanel";
import { MarkdownPreview } from "../ai/CodePreviewMarkdown";
import { MessageContentRenderer } from "../ai/MessageContentRenderer";
import { reasoningTrailMarkdownOptions, renderContentWithCodeBlocks } from "../ai/aiAssistantMarkdown";
import { lightTheme } from "../ai/aiAssistantPanelTheme";
import { isHistoryResetCommandText } from "../ai/composeAction";
import { cleanReasoningTrailForBody, resolveVisibleAssistantReply } from "../ai/assistantReasoningBody";
import { stripAssistantToolCallMarkers } from "../ai/assistantToolCall";
import { FileCompanionComposer } from "./FileCompanionComposer";
import { companionVisibleText, finishCompanionReply, splitCompanionStreamDelta } from "./companionReasoning";
import { PaperResultCard } from "./PaperResultCard";
import { windowDragHandleProps } from "../../utils/windowDrag";
import type { CodePreviewTheme } from "../ai/FileTabBar";
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
};

type CompanionMessageDoc = { role?: string; Role?: string; text?: string; Text?: string; reasoning?: string; Reasoning?: string; resultPath?: string; ResultPath?: string };

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

type ChatMessage = { id: string; role: "user" | "assistant"; text: string; reasoning?: string; resultPath?: string };

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
    dirty: boolean;
    conflict: boolean;
    diskHash: string;
    confirmOverwrite: boolean;
    previewEpoch: number;
    saveError: string;
    htmlPreview: boolean;
};

type RuntimeAPI = {
    EventsOn?: (name: string, cb: (payload: unknown) => void) => void;
    EventsOff?: (name: string) => void;
    OnFileDrop?: (cb: (x: number, y: number, paths: string[]) => void, useDropTarget?: boolean) => void;
    OnFileDropOff?: () => void;
};

export function readFileCompanionBootFlag(): boolean {
    const flag = (window as unknown as { __MACLAW_FILE_COMPANION__?: boolean }).__MACLAW_FILE_COMPANION__;
    return flag === true;
}

function runtimeAPI(): RuntimeAPI | undefined {
    return (window as unknown as { runtime?: RuntimeAPI }).runtime;
}

function field<T>(doc: CompanionDoc, lower: keyof CompanionDoc, upper: keyof CompanionDoc, fallback: T): T {
    const value = doc[lower] ?? doc[upper];
    return (value === undefined || value === null ? fallback : value) as T;
}

function bootPaths(boot: { paths?: unknown; Paths?: unknown } | null | undefined): string[] {
    const raw = boot?.paths ?? boot?.Paths;
    if (!Array.isArray(raw)) return [];
    return raw.filter((item): item is string => typeof item === "string" && item.trim() !== "");
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

function exportErrorText(err: unknown): string {
    const raw = (err instanceof Error ? err.message : typeof err === "string" ? err : "").trim();
    if (raw.includes("MaClaw Hub login is required")) return "需要先在主窗口登录，才能上传到云盘";
    if (raw === "file is empty") return "文件是空的";
    if (/director/i.test(raw)) return "不支持上传文件夹";
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
        if (!text.trim() && !reasoning.trim() && !resultPath) return;
        out.push({ id: `restored:${index}`, role, text, reasoning, resultPath: resultPath || undefined });
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

function CompanionActionIcon({ name }: { name: "paper" | "knowledge" | "cloud" | "close" }) {
    const common = {
        fill: "none",
        stroke: "currentColor",
        strokeWidth: 1.8,
        strokeLinecap: "round" as const,
        strokeLinejoin: "round" as const,
    };
    return (
        <svg className="file-companion-tool-icon" width="14" height="14" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
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
            {name === "close" && (
                <>
                    <path {...common} d="M7.2 7.2 16.8 16.8" />
                    <path {...common} d="m16.8 7.2-9.6 9.6" />
                </>
            )}
        </svg>
    );
}

export default function FileCompanionWindow({ askOnOpen = true }: { askOnOpen?: boolean } = {}) {
    const [tabs, setTabs] = useState<CompanionTab[]>([]);
    const [active, setActive] = useState("");
    const [tabEnds, setTabEnds] = useState<CompanionTabEnds>({ overflow: false, canLeft: false, canRight: false });
    const [dropMessage, setDropMessage] = useState("");
    const tabsRef = useRef(tabs);
    tabsRef.current = tabs;
    const activeRef = useRef(active);
    activeRef.current = active;
    const sendingRef = useRef(false);
    const askOnOpenRef = useRef(askOnOpen);
    askOnOpenRef.current = askOnOpen;
    // Paths already given the opening question, or waiting for it. Cleared when
    // that tab closes, so a later empty open can ask again. /clear does not.
    const introArmedRef = useRef(new Set<string>());
    const introQueueRef = useRef<string[]>([]);
    const introDrainingRef = useRef(false);
    const introSerialRef = useRef(0);
    const drainIntroRef = useRef<() => void>(() => undefined);
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
    const chatEpochRef = useRef(new Map<string, number>());
    const requestEpochRef = useRef(new Map<string, number>());
    const paperRequestIds = useRef(new Set<string>());
    const acceptedHash = useRef(new Map<string, string>());
    const saveTail = useRef(new Map<string, Promise<void>>());
    const openEpoch = useRef(new Map<string, number>());
    const openSerial = useRef(0);
    const saveEpoch = useRef(new Map<string, number>());
    const bufferEpoch = useRef(new Map<string, number>());
    const saveTabRef = useRef<(tab: CompanionTab, hash: string) => Promise<"saved" | "conflict" | "error" | "skipped">>(async () => "skipped");
    const choosingRef = useRef(false);
    const chooseFilesRef = useRef<() => Promise<void>>(async () => undefined);
    const bootSettledRef = useRef(false);
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
                doc = { path: trimmed, error: err instanceof Error ? err.message : "打不开这个文件" };
            }
            if (openEpoch.current.get(trimmed) !== serial) return;
            const error = field(doc, "error", "Error", "");
            if (fromDrop && isDirectoryError(error)) {
                setDropMessage("不支持打开文件夹");
                return;
            }
            const next = tabFromDoc(doc, trimmed);
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
            setTabs((current) => current.map((tab) => tab.sessionId === parsed.sessionId ? { ...tab, status: text } : tab));
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
            const paper = paperRequestIds.current.has(parsed.requestID);
            if (paper) paperRequestIds.current.delete(parsed.requestID);
            const resultPath = paper && !errText && localPath ? localPath : "";
            const matched = tabsRef.current.find((tab) => tab.sessionId === parsed.sessionId);
            const matchedName = matched?.name.toLowerCase() ?? "";
            const reloadPath = !paper && !errText && localPath && matched && !matchedName.endsWith(".xlsx") && !matchedName.endsWith(".pptx")
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
                    const next = {
                        id,
                        role: "assistant" as const,
                        text: resultPath ? "已生成论文解读" : (shown ? finished.text : (streamed?.text || "")),
                        reasoning: finished.reasoning,
                        resultPath: resultPath || streamed?.resultPath,
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
        const onShow = () => {
            // Boot paths are applied after this listener exists. A show in that
            // gap must not treat "tabs not rendered yet" as an empty window.
            if (!bootSettledRef.current || !windowIsEmpty()) return;
            void chooseFilesRef.current();
        };
        runtime?.EventsOn?.("file-companion:open", onOpen);
        runtime?.EventsOn?.("file-companion:show", onShow);
        runtime?.EventsOn?.("file-companion:preview-reload", onReload);
        runtime?.EventsOn?.("ai-assistant-progress", onProgress);
        runtime?.EventsOn?.("ai-assistant-token", onToken);
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
        const readyPaths = (value: unknown): string[] => (
            Array.isArray(value) ? value.filter((item): item is string => typeof item === "string" && item.trim() !== "") : []
        );
        void (async () => {
            const askIfEmpty = () => {
                if (cancelled || !windowIsEmpty()) return;
                void chooseFilesRef.current();
            };
            // Ready returns paths queued after the boot read. Applying them
            // here, before the empty check, keeps the file dialog from opening
            // on top of a file the event would only deliver later.
            const finishBoot = async () => {
                const delivered = readyPaths(await Promise.resolve(FileCompanionUIReady()).catch(() => undefined));
                for (const path of delivered) {
                    if (cancelled) return;
                    await openPath(path, false, false, 0, true);
                }
                if (cancelled) return;
                bootSettledRef.current = true;
                askIfEmpty();
            };
            try {
                const boot = await bootPromise;
                if (cancelled) return;
                const paths = bootPaths(boot as { paths?: unknown; Paths?: unknown });
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
            runtime?.OnFileDropOff?.();
        };
    }, []);

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
    useEffect(() => {
        const onKey = (event: KeyboardEvent) => {
            if (!(event.ctrlKey || event.metaKey) || event.key.toLowerCase() !== "s") return;
            event.preventDefault();
            const tab = tabsRef.current.find((item) => item.path === activeRef.current);
            if (!tab || tab.conflict) return;
            void saveTabRef.current(tab, tab.loadedHash);
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, []);

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
    const chatScrollRef = useRef<HTMLDivElement | null>(null);
    const chatStick = useRef(true);
    const chatPinning = useRef(false);
    const chatActive = useRef(active);
    const chatTail = activeTab && activeTab.messages.length > 0 ? activeTab.messages[activeTab.messages.length - 1].text : "";
    useEffect(() => {
        const node = chatScrollRef.current;
        if (!node) return;
        if (chatActive.current !== active) {
            chatActive.current = active;
            chatStick.current = true;
        }
        if (!chatStick.current) return;
        chatPinning.current = true;
        node.scrollTop = node.scrollHeight;
        chatPinning.current = false;
    }, [active, activeTab?.messages.length, chatTail, activeTab?.status]);
    const kind: FilePreviewKind = activeTab ? filePreviewKindFromName(activeTab.name) : "text";
    const textEditorKind = kind === "markdown" || kind === "text" || kind === "code" || kind === "html";
    const showEditor = Boolean(activeTab && !activeTab.error && textEditorKind && (activeTab.editable || activeTab.content || activeTab.readOnlyReason));
    const showPreview = Boolean(activeTab && !activeTab.error && (kind === "pptx" || kind === "docx" || kind === "pdf" || kind === "office" || kind === "image" || kind === "video" || kind === "audio" || kind === "html" || kind === "latex"));
    const emptyFile = Boolean(activeTab && !activeTab.error && !showEditor && activeTab.size <= 0);
    const htmlPreviewing = Boolean(activeTab && kind === "html" && activeTab.htmlPreview);
    const showSource = showEditor && !htmlPreviewing;
    const showPreviewPane = showPreview && !emptyFile && (kind !== "html" || htmlPreviewing);
    const unsupported = Boolean(activeTab && !activeTab.error && !showEditor && !showPreview && !emptyFile);
    const cloudDisabled = !activeTab || !activeTab.canExport || activeTab.size > CLOUD_DISABLE_BYTES;
    const [importBusyPath, setImportBusyPath] = useState("");
    const [uploadBusyPath, setUploadBusyPath] = useState("");
    const [uploadedPaths, setUploadedPaths] = useState<ReadonlySet<string>>(() => new Set());
    const [sendingPath, setSendingPath] = useState("");
    const [paperBusyPath, setPaperBusyPath] = useState("");
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
    // One handler serves every open file, so the send lock is the whole window.
    // A per-file busy flag left the other file looking idle; its click then
    // returned before a turn existed.
    const sendBusy = Boolean(sendingPath || (activeTab && activeTab.status.trim()));
    const paperBusy = Boolean(activeTab && paperBusyPath === activeTab.path);
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
            await ClearFileCompanionChat(tab.path);
        } catch (err) {
            showToast(turnErrorText(err instanceof Error ? err.message : "没能清空聊天"), "error", 6000);
        }
        return true;
    };

    const send = async (): Promise<boolean> => {
        if (!activeTab) return false;
        const text = activeTab.draft.trim();
        if (!text) return false;
        if (isHistoryResetCommandText(text)) return clearCompanionChat(activeTab, text);
        if (sendingRef.current || activeTab.status.trim() || sendingPath === activeTab.path) return false;
        const tabPath = activeTab.path;
        const sessionId = activeTab.sessionId;
        const selection = activeTab.selection;
        const selectionStart = activeTab.selectionStart;
        chatStick.current = true;
        sendingRef.current = true;
        setSendingPath(tabPath);
        const requestID = `file-companion-${Date.now()}`;
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
                sendingRef.current = false;
                setSendingPath((current) => current === tabPath ? "" : current);
                drainIntroRef.current();
            }
        }
        // The question is sent once the user bubble is committed. The reply
        // keeps the busy lock, but ↑ must already see this line.
        void (async () => {
            try {
                await SendFileCompanionMessage({
                    path: tabPath,
                    text,
                    selection,
                    request_id: requestID,
                    ...(selection && selectionStart >= 0 ? { selection_start: selectionStart } : {}),
                });
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
                sendingRef.current = false;
                setSendingPath((current) => current === tabPath ? "" : current);
                drainIntroRef.current();
            }
        })();
        return true;
    };

    const sendPaper = async () => {
        if (!activeTab || activeTab.error || sendingRef.current || activeTab.status.trim() || sendingPath === activeTab.path) return;
        const tabPath = activeTab.path;
        const sessionId = activeTab.sessionId;
        chatStick.current = true;
        sendingRef.current = true;
        setSendingPath(tabPath);
        setPaperBusyPath(tabPath);
        const requestID = `file-companion-${Date.now()}`;
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
            sendingRef.current = false;
            setSendingPath((current) => current === tabPath ? "" : current);
            setPaperBusyPath((current) => current === tabPath ? "" : current);
            drainIntroRef.current();
        }
    };

    // One turn at a time. A new empty file asks what it is; further files wait
    // until that turn releases the lock. The question is not typed, so it is
    // not stored in the composer's input history, and it carries no selection.
    const startIntro = (tab: CompanionTab): Promise<void> => {
        const tabPath = tab.path;
        const sessionId = tab.sessionId;
        const text = FILE_COMPANION_INTRO;
        sendingRef.current = true;
        setSendingPath(tabPath);
        introSerialRef.current += 1;
        const requestID = `file-companion-intro-${Date.now()}-${introSerialRef.current}`;
        bindCompanionRequest(sessionId, requestID);
        if (activeRef.current === tabPath) chatStick.current = true;
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
                sendingRef.current = false;
                setSendingPath((current) => current === tabPath ? "" : current);
            }
        })();
    };

    const drainIntro = () => {
        if (introDrainingRef.current) return;
        while (introQueueRef.current.length > 0) {
            const path = introQueueRef.current[0];
            const tab = tabsRef.current.find((item) => item.path === path);
            if (!tab || tab.error || tab.messages.length > 0) {
                introQueueRef.current.shift();
                continue;
            }
            if (sendingRef.current || tab.status.trim()) return;
            introQueueRef.current.shift();
            introDrainingRef.current = true;
            void startIntro(tab).finally(() => {
                introDrainingRef.current = false;
                drainIntroRef.current();
            });
            return;
        }
    };
    drainIntroRef.current = drainIntro;

    useEffect(() => {
        drainIntroRef.current();
    }, [tabs, sendingPath]);

    const chooseFiles = async () => {
        if (choosingRef.current) return;
        choosingRef.current = true;
        try {
            const paths = await ChooseFileCompanionFiles();
            for (const path of paths ?? []) {
                await openPath(path, false, false, 0, true);
            }
        } catch {
            // The empty window and the 打开文件 tab stay usable.
        } finally {
            choosingRef.current = false;
        }
    };
    chooseFilesRef.current = chooseFiles;

    return (
        <div data-testid="file-companion-window" className="file-companion-window" style={{ display: "flex", flexDirection: "column", height: "100vh", background: "#f3f5f7", color: "#1e293b" }}>
            <div className="file-companion-titlebar" data-testid="file-companion-titlebar" {...windowDragHandleProps(true, { display: "flex", gap: 8, padding: 8, alignItems: "center", flexWrap: "nowrap", borderBottom: "1px solid #d9e1ec", background: "#ffffff" })}>
                <span className="mc-header-brand" data-testid="file-companion-brand" aria-label="MaClaw伴读">
                    <span className="mc-header-brand-mark" aria-hidden="true">
                        <svg viewBox="0 0 80 80" focusable="false">
                            <path d="M14 58V22l26 25 26-25v36" fill="none" stroke="currentColor" strokeWidth="10.5" strokeLinecap="round" strokeLinejoin="round" />
                        </svg>
                    </span>
                    <span>MaClaw伴读</span>
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
                <button type="button" className="file-companion-tool file-companion-tab file-companion-tab--open" data-testid="file-companion-choose" onClick={() => { void chooseFiles(); }}>打开文件</button>
                <div className="file-companion-titlebar-gap" data-testid="file-companion-titlebar-gap" />
                {activeTab ? (
                    <div className="file-companion-titlebar-actions" data-testid="file-companion-export">
                        <button
                            type="button"
                            className="file-companion-tool"
                            data-testid="file-companion-paper"
                            disabled={Boolean(activeTab.error) || sendBusy}
                            aria-busy={paperBusy}
                            onClick={() => { void sendPaper(); }}
                        >
                            <CompanionActionIcon name="paper" />
                            {paperBusy ? "正在解读…" : "论文解读"}
                        </button>
                        <button
                            type="button"
                            className="file-companion-tool"
                            data-testid="file-companion-import"
                            disabled={!activeTab.canExport || importBusy}
                            aria-busy={importBusy}
                            onClick={() => { void runExport("import"); }}
                        >
                            <CompanionActionIcon name="knowledge" />
                            {importBusy ? "正在导入…" : "导入知识库"}
                        </button>
                        <button
                            type="button"
                            className="file-companion-tool"
                            data-testid="file-companion-upload"
                            disabled={cloudDisabled || uploadBusy || uploaded}
                            aria-busy={uploadBusy}
                            title={activeTab.size > CLOUD_DISABLE_BYTES ? "文件太大，无法上传到云盘" : uploaded ? "已上传到云盘" : "上传到云盘"}
                            onClick={() => { void runExport("upload"); }}
                        >
                            <CompanionActionIcon name="cloud" />
                            {uploadBusy ? "正在上传…" : "上传到云盘"}
                        </button>
                    </div>
                ) : null}
                {/* runtime WindowHide. App.WindowHide also shows the tray pet. */}
                <button type="button" className="file-companion-tool file-companion-tool--close" data-testid="file-companion-close" title="隐藏窗口，已打开的文件会保留" onClick={() => { WindowHide(); }}><CompanionActionIcon name="close" />关闭</button>
            </div>
            {dropMessage ? <div data-testid="file-companion-drop-error" style={{ padding: "8px 12px", color: "#9a3412" }}>{dropMessage}</div> : null}
            {!activeTab ? (
                <div data-testid="file-companion-empty" style={{ flex: 1, display: "flex", alignItems: "center", justifyContent: "center" }}>
                    把文件拖到这里，或点击「打开文件」
                </div>
            ) : (
                <div className="file-companion-split" data-testid="file-companion-split" ref={splitRef}>
                    <section className="file-companion-split-preview">
                        {activeTab.error ? <div data-testid="file-companion-file-error">{fileErrorText(activeTab.error)}</div> : null}
                        {kind === "html" && !activeTab.error ? (
                            <button
                                type="button"
                                className="file-companion-tool"
                                data-testid="file-companion-html-toggle"
                                onClick={() => updateActive({ htmlPreview: !activeTab.htmlPreview })}
                            >
                                {activeTab.htmlPreview ? "源码" : "预览"}
                            </button>
                        ) : null}
                        {showSource && kind === "markdown" ? (
                            <MarkdownSourceEditor
                                key={activeTab.path}
                                value={activeTab.content}
                                readOnly={!activeTab.editable}
                                onChange={(content) => updateActive({ content, dirty: true, conflict: false, saveError: "" })}
                                onSelect={rememberSelection}
                            />
                        ) : null}
                        {showSource && kind !== "markdown" ? (
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
                        {activeTab.readOnlyReason === "permission" ? <p data-testid="file-companion-readonly-note">没有写入权限</p> : null}
                        {activeTab.readOnlyReason === "too-large" ? <p data-testid="file-companion-readonly-note">文件过大，为避免截断写回，已禁止保存</p> : null}
                        {emptyFile ? <div data-testid="file-companion-empty-file">文件是空的</div> : null}
                        {unsupported ? (
                            <div data-testid="file-companion-unsupported">
                                <p>此文件类型暂不支持预览</p>
                                <p>{activeTab.name}</p>
                                <p>{activeTab.size} 字节</p>
                            </div>
                        ) : null}
                        {showPreviewPane ? (
                            <div data-testid="file-companion-preview" style={{ flex: 1, minHeight: 160 }}>
                                <FilePreviewView
                                    key={`${activeTab.path}:${activeTab.previewEpoch}`}
                                    file={{ fileName: activeTab.name, filePath: activeTab.path, absPath: activeTab.path, content: activeTab.content, updatedAt: activeTab.previewEpoch }}
                                    theme={companionPreviewTheme}
                                    lang="zh-Hans"
                                />
                            </div>
                        ) : null}
                        {activeTab.saveError ? <p data-testid="file-companion-save-error">{turnErrorText(activeTab.saveError)}</p> : null}
                        {activeTab.conflict ? (
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
                        {activeTab.status ? <div data-testid="file-companion-progress" style={{ padding: "8px 12px", color: "#64748b" }}>{activeTab.status}</div> : null}
                        <div
                            ref={chatScrollRef}
                            data-testid="file-companion-chat-log"
                            onScroll={(event) => {
                                if (chatPinning.current) return;
                                const node = event.currentTarget;
                                chatStick.current = node.scrollHeight - node.scrollTop - node.clientHeight < 48;
                            }}
                            style={{ flex: 1, overflow: "auto", padding: 16, display: "flex", flexDirection: "column", gap: 10 }}
                        >
                            {activeTab.messages.length === 0 ? <p data-testid="file-companion-chat-empty" style={{ margin: 0, color: "#94a3b8", fontSize: 13 }}>针对当前文件提问</p> : null}
                            {activeTab.messages.map((message, index) => {
                                const mine = message.role === "user";
                                const turnLive = Boolean(activeTab.status) || sendingPath === activeTab.path;
                                const streaming = !mine && turnLive && index === activeTab.messages.length - 1;
                                const cleanedReasoning = mine ? "" : cleanReasoningTrailForBody(message.reasoning || "");
                                const visibleReply = mine
                                    ? { content: message.text, reasoning: "" }
                                    : resolveVisibleAssistantReply(message.text, cleanedReasoning, { live: streaming });
                                const displayReasoning = stripAssistantToolCallMarkers(visibleReply.reasoning).trim();
                                const paperPending = !mine && !message.resultPath && paperRequestIds.current.has(paperRequestOf(message.id));
                                const answerText = message.resultPath ? "已生成论文解读" : paperPending ? "正在生成论文解读…" : visibleReply.content;
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
                                                <MessageContentRenderer
                                                    content={answerText}
                                                    theme={lightTheme}
                                                    isUser={mine}
                                                    isStreaming={streaming && !message.resultPath}
                                                    messageId={message.id}
                                                />
                                                {message.resultPath ? <PaperResultCard filePath={message.resultPath} messageId={message.id} /> : null}
                                            </div>
                                        </div>
                                    </div>
                                );
                            })}
                        </div>
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
        </div>
    );
}
