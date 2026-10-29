import { memo, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, ReactNode } from "react";
import { AIAssistantAttachmentPreviewDataURL, ConversationViewArchiveCount, LoadAIAssistantUIArchive, LoadProjectTabConversationArchive } from "../../../wailsjs/go/main/App";
import type { ChatMessage } from "./useAIAssistant";
import type { Theme } from "./aiAssistantPanelTheme";
import { localizeText } from "./aiAssistantI18n";
import { messageIsLocalSession } from "./aiAssistantPanelSessionUtils";
import { splitConversationView } from "./conversationViewWindow";
import { MessageContentRenderer } from "./MessageContentRenderer";
import { reasoningTrailMarkdownOptions } from "./aiAssistantMarkdown";
import { capReasoningForDisplay, cleanReasoningTrailForBody, resolveVisibleAssistantReply, separateReasoningFromBody } from "./assistantReasoningBody";
import { completedAssistantSummary, stripAssistantToolCallMarkers } from "./assistantToolCall";
import { stripCodingAgentAuditSections } from "./codingAgentUserFinish";
import { isBrowserEchoFieldLabel } from "./rolePrefixDisplay";
import { isImageAttachmentPath, userTurnDisplayView, type UserTurnDisplayAttachment } from "./userTurnDisplay";
import { renderScreenshotPreview } from "./aiAssistantMarkdownMedia";
import { UserAttachmentChip } from "./UserAttachmentChip";

/** Rows mounted per page. The archive holds up to 800 messages; parsing every Markdown body on open freezes the dialog. */
export const CONVERSATION_ARCHIVE_RENDER_PAGE = 24;

interface ConversationViewArchiveOptions {
    messages: ChatMessage[];
    /** "desktop" or "tab:<tabId>". */
    scope: string;
    enabled: boolean;
    lang: string;
    theme: Theme;
}

function sameStrings(left: readonly string[] | undefined, right: readonly string[] | undefined): boolean {
    const a = left || [];
    const b = right || [];
    if (a.length !== b.length) return false;
    for (let index = 0; index < a.length; index += 1) {
        if (a[index] !== b[index]) return false;
    }
    return true;
}

function sameAttachmentNames(prev: ChatMessage, next: ChatMessage): boolean {
    const left = prev.attachments || [];
    const right = next.attachments || [];
    if (left.length !== right.length) return false;
    for (let index = 0; index < left.length; index += 1) {
        if (left[index].fileName !== right[index].fileName
            || left[index].filePath !== right[index].filePath
            || !!left[index].isImage !== !!right[index].isImage) return false;
    }
    return true;
}

function archiveRowUnchanged(prev: ChatMessage, next: ChatMessage): boolean {
    return prev.role === next.role
        && prev.content === next.content
        && prev.reasoning === next.reasoning
        && prev.resultText === next.resultText
        && prev.resultStatus === next.resultStatus
        && prev.localFilePath === next.localFilePath
        && sameStrings(prev.localFilePaths, next.localFilePaths)
        && (prev.toolCalls?.length || 0) === (next.toolCalls?.length || 0)
        && (prev.codingTimeline?.length || 0) === (next.codingTimeline?.length || 0)
        && prev.thumbnailBase64 === next.thumbnailBase64
        && prev.imageKey === next.imageKey
        && sameAttachmentNames(prev, next)
        && sameFields(prev, next);
}

function sameFields(prev: ChatMessage, next: ChatMessage): boolean {
    const left = prev.fields || [];
    const right = next.fields || [];
    if (left.length !== right.length) return false;
    for (let index = 0; index < left.length; index += 1) {
        if (left[index]?.label !== right[index]?.label || left[index]?.value !== right[index]?.value) return false;
    }
    return true;
}

/** Same rule as the live bubble: a capture with text is hidden unless a saved file owns it. */
function archiveScreenshot(message: ChatMessage): string {
    const data = message.thumbnailBase64 || message.imageKey || "";
    if (!data) return "";
    const hasSavedArtifact = assistantSavedPaths(message).length > 0;
    // Any field counts, including browser echo: the live bubble uses the same check.
    const hasText = !!(message.content?.trim() || message.reasoning?.trim() || message.fields?.length);
    return hasSavedArtifact || !hasText ? data : "";
}

/** Saved files for one reply. `localFilePath` stays even when the list omitted it. */
function assistantSavedPaths(message: ChatMessage): string[] {
    const paths: string[] = [];
    const seen = new Set<string>();
    const push = (path: string | undefined) => {
        const trimmed = path?.trim();
        if (!trimmed) return;
        const key = trimmed.toLowerCase();
        if (seen.has(key)) return;
        seen.add(key);
        paths.push(trimmed);
    };
    push(message.localFilePath);
    for (const path of message.localFilePaths || []) push(path);
    return paths;
}

/** Chips the live bubble actually draws. Browser echo and recorder metadata stay out. */
function visibleArchiveFields(message: ChatMessage): Array<{ label: string; value: string }> {
    const out: Array<{ label: string; value: string }> = [];
    for (const field of message.fields || []) {
        const label = String(field?.label || "").trim();
        const lower = label.toLowerCase();
        if (!label || lower === "recording_title" || lower === "recording_purpose" || isBrowserEchoFieldLabel(label)) continue;
        const value = String(field?.value || "").trim();
        if (!value) continue;
        out.push({ label, value });
    }
    return out;
}

function baseName(filePath: string): string {
    return filePath.split(/[\\/]/).filter(Boolean).pop() || filePath;
}

/** History stores no image bytes. Resolve the same preview the live chip uses, from the saved path. */
function ArchiveSavedScreenshot({ filePath, lang, theme: t }: { filePath: string; lang: string; theme: Theme }) {
    const [data, setData] = useState("");
    useEffect(() => {
        let active = true;
        setData("");
        void AIAssistantAttachmentPreviewDataURL(filePath)
            .then((url) => {
                if (!active) return;
                const text = String(url || "");
                const marker = "base64,";
                const at = text.indexOf(marker);
                setData(at >= 0 ? text.slice(at + marker.length) : text);
            })
            .catch(() => {
                if (active) setData("");
            });
        return () => { active = false; };
    }, [filePath]);
    if (!data) return null;
    return <>{renderScreenshotPreview(data, filePath, t, lang)}</>;
}

/** Keep object identity for unchanged rows so an archive reload does not reparse Markdown. */
export function reuseArchiveRows(prev: ChatMessage[], next: ChatMessage[]): ChatMessage[] {
    if (prev.length === 0 || next.length === 0) return next;
    const prevById = new Map(prev.map((message) => [message.id, message]));
    let reused = 0;
    const merged = next.map((message) => {
        const old = prevById.get(message.id);
        if (old && archiveRowUnchanged(old, message)) {
            reused += 1;
            return old;
        }
        return message;
    });
    if (reused === next.length && prev.length === next.length && merged.every((message, index) => message === prev[index])) return prev;
    return merged;
}

function dedupeMessages(messages: ChatMessage[]): ChatMessage[] {
    const seen = new Set<string>();
    const out: ChatMessage[] = [];
    for (const message of messages) {
        if (!message.id || seen.has(message.id)) continue;
        seen.add(message.id);
        out.push(message);
    }
    return out;
}

function asChatMessages(raw: unknown): ChatMessage[] {
    if (!Array.isArray(raw)) return [];
    return raw.filter((message) => {
        if (!message || typeof message !== "object") return false;
        const row = message as ChatMessage;
        return !!row.id && !!row.role;
    }) as ChatMessage[];
}

function messageText(value: unknown): string {
    return typeof value === "string" ? value : "";
}

/**
 * Text the archive row shows. Pure helpers only: the live transcript's reply
 * cleaners keep a single shared cursor, and calling them here would reset the
 * open conversation mid-stream.
 *
 * User rows drop host prompt blocks. Assistant rows use the same settled-reply
 * split as the live bubble (summary, audit cut, CoT kept out of the answer).
 */
export function archiveMessageView(message: ChatMessage): { body: string; reasoning: string; attachments: UserTurnDisplayAttachment[] } {
    if (message.role === "user") {
        const view = userTurnDisplayView(messageText(message.content), message.attachments);
        return {
            body: view.text.trim() ? view.text : "",
            reasoning: "",
            attachments: view.attachments.filter((attachment) => attachment.fileName || attachment.filePath),
        };
    }
    if (message.role !== "assistant") {
        const content = messageText(message.content).trim();
        return { body: content || messageText(message.reasoning).trim(), reasoning: "", attachments: [] };
    }
    const rawContent = messageText(message.content);
    const rawReasoning = messageText(message.reasoning).trim();
    const cleanedReasoning = rawReasoning ? cleanReasoningTrailForBody(capReasoningForDisplay(rawReasoning)) : "";
    const coding = !!message.codingTimeline?.length;
    const toolCalls = !!message.toolCalls?.length;
    const visible = coding
        ? separateReasoningFromBody(rawContent, cleanedReasoning)
        : toolCalls
            ? { content: rawContent, reasoning: cleanedReasoning.trim() }
            : resolveVisibleAssistantReply(rawContent, cleanedReasoning);
    const settled = toolCalls || message.resultStatus === "incomplete" || message.resultStatus === "completed" || rawContent.includes("maclaw-tool:");
    const summary = settled ? completedAssistantSummary(visible.content, message.resultText) : visible.content;
    let body = stripCodingAgentAuditSections(stripAssistantToolCallMarkers(summary)).trim();
    let reasoning = stripAssistantToolCallMarkers(visible.reasoning).trim();
    // A short trail with no answer is the message. A tool turn's empty content
    // stays empty; the trail remains behind the thinking control.
    if (!body && reasoning && !toolCalls) {
        body = reasoning;
        reasoning = "";
    }
    return { body, reasoning, attachments: [] };
}

const archiveRowStyle: CSSProperties = {
    marginBottom: "12px",
    minWidth: 0,
};

/** Read-only row. Assistant text uses the same Markdown pipeline as the live transcript. User text stays pre-wrap. */
const ArchiveMessageRow = memo(function ArchiveMessageRow({ message, lang, theme: t }: { message: ChatMessage; lang: string; theme: Theme }) {
    const view = useMemo(() => archiveMessageView(message), [message]);
    const screenshot = useMemo(() => archiveScreenshot(message), [message]);
    const savedPaths = useMemo(() => message.role === "assistant" ? assistantSavedPaths(message) : [], [message]);
    const fields = useMemo(() => message.role === "assistant" ? visibleArchiveFields(message) : [], [message]);
    // Inline bytes win. After a restart those bytes are gone and only the path remains.
    const imagePath = savedPaths.find((path) => isImageAttachmentPath(path)) || "";
    const restoredImagePath = screenshot ? "" : imagePath;
    const shotPath = message.localFilePath?.trim() || imagePath;
    const savedNames = savedPaths.map(baseName).filter(Boolean);
    const isUser = message.role === "user";
    const [reasoningOpen, setReasoningOpen] = useState(false);
    const hasPicture = !!screenshot || !!restoredImagePath;
    return (
        <div data-testid="conversation-archive-message" style={archiveRowStyle}>
            <div style={{ fontSize: "11px", color: t.textMuted, marginBottom: "2px" }}>
                {isUser
                    ? localizeText(lang, "You", "你", "你")
                    : localizeText(lang, "Assistant", "助手", "助手")}
            </div>
            {screenshot ? renderScreenshotPreview(screenshot, shotPath || undefined, t, lang) : null}
            {restoredImagePath ? <ArchiveSavedScreenshot filePath={restoredImagePath} lang={lang} theme={t} /> : null}
            {view.body ? (
                <div style={{ fontSize: "13px", lineHeight: 1.55, minWidth: 0, overflowWrap: "break-word", whiteSpace: isUser ? "pre-wrap" : "normal" }}>
                    <MessageContentRenderer content={view.body} theme={t} isUser={isUser} messageId={message.id} />
                </div>
            ) : view.attachments.length === 0 && !hasPicture && savedNames.length === 0 && fields.length === 0 ? (
                <div style={{ fontSize: "13px", lineHeight: 1.55, color: t.textMuted }}>
                    {localizeText(lang, "(no text)", "（无文本）", "（無文本）")}
                </div>
            ) : null}
            {fields.length > 0 ? (
                <div data-testid="conversation-archive-fields" style={{ marginTop: view.body ? "6px" : 0, fontSize: "12px", lineHeight: 1.45 }}>
                    {fields.map((field) => (
                        <div key={`${field.label}:${field.value}`} style={{ overflowWrap: "break-word" }}>
                            <span style={{ color: t.textMuted }}>{field.label}: </span>
                            <span>{field.value}</span>
                        </div>
                    ))}
                </div>
            ) : null}
            {view.attachments.length > 0 ? (
                <div data-testid="conversation-archive-files" style={{ display: "flex", flexWrap: "wrap", gap: "6px", marginTop: view.body ? "6px" : 0 }}>
                    {view.attachments.map((attachment, index) => (
                        <UserAttachmentChip key={`${attachment.filePath}-${index}`} attachment={attachment} theme={t} lang={lang} />
                    ))}
                </div>
            ) : null}
            {savedNames.length > 0 ? (
                <div data-testid="conversation-archive-saved" style={{ marginTop: "4px", fontSize: "12px", color: t.textMuted, overflowWrap: "break-word" }}>
                    {savedNames.join("、")}
                </div>
            ) : null}
            {view.reasoning ? (
                <div style={{ marginTop: "6px" }}>
                    <button
                        type="button"
                        data-testid="conversation-archive-reasoning"
                        aria-expanded={reasoningOpen}
                        onClick={() => setReasoningOpen((open) => !open)}
                        style={{ background: "none", border: "none", padding: 0, color: t.textMuted, cursor: "pointer", fontSize: "12px" }}
                    >
                        {localizeText(lang, "Thinking process...", "思考过程...", "思考過程...")}
                    </button>
                    {reasoningOpen ? (
                        <div style={{ marginTop: "6px", minWidth: 0, overflowWrap: "break-word" }}>
                            <MessageContentRenderer content={view.reasoning} theme={t} messageId={`${message.id}:reasoning`} markdownOptions={reasoningTrailMarkdownOptions} />
                        </div>
                    ) : null}
                </div>
            ) : null}
        </div>
    );
});

export function useConversationViewArchive({ messages, scope, enabled, lang, theme: t }: ConversationViewArchiveOptions): {
    visibleMessages: ChatMessage[];
    earlierCount: number;
    banner: ReactNode;
    dialog: ReactNode;
} {
    const { visible, earlier } = useMemo(() => splitConversationView(messages), [messages]);
    const [serverCount, setServerCount] = useState(0);
    const [open, setOpen] = useState(false);
    const [loading, setLoading] = useState(false);
    const [rows, setRows] = useState<ChatMessage[]>([]);
    const [mountedCount, setMountedCount] = useState(CONVERSATION_ARCHIVE_RENDER_PAGE);
    const loadGen = useRef(0);
    const messagesRef = useRef(messages);
    const earlierRef = useRef(earlier);
    const scopeRef = useRef(scope);
    messagesRef.current = messages;
    earlierRef.current = earlier;
    scopeRef.current = scope;
    const dialogRef = useRef<HTMLDivElement>(null);
    const returnFocusRef = useRef<HTMLElement | null>(null);
    const scrollerRef = useRef<HTMLDivElement>(null);
    const sentinelRef = useRef<HTMLDivElement>(null);
    const paging = useRef(false);
    const sentinelSeen = useRef(false);
    const lastFillHeight = useRef(0);

    useEffect(() => {
        if (!enabled || !scope) {
            setServerCount(0);
            return;
        }
        let cancelled = false;
        const loadCount = typeof ConversationViewArchiveCount === "function"
            ? ConversationViewArchiveCount(scope)
            : Promise.resolve(0);
        void Promise.resolve(loadCount).then((count) => {
            if (!cancelled) setServerCount(Math.max(0, Number(count) || 0));
        }).catch(() => {
            if (!cancelled) setServerCount(0);
        });
        return () => {
            cancelled = true;
        };
    }, [enabled, scope, messages.length]);

    const seenScope = useRef(scope);
    useEffect(() => {
        if (seenScope.current === scope) return;
        seenScope.current = scope;
        loadGen.current += 1;
        setOpen(false);
        setRows([]);
        setMountedCount(CONVERSATION_ARCHIVE_RENDER_PAGE);
    }, [scope]);

    useEffect(() => {
        if (!open) {
            const back = returnFocusRef.current;
            returnFocusRef.current = null;
            if (back?.isConnected) back.focus();
            return;
        }
        const active = document.activeElement;
        const dialog = dialogRef.current;
        if (active instanceof HTMLElement && !dialog?.contains(active)) {
            returnFocusRef.current = active;
        }
        dialog?.focus({ preventScroll: true });
        const onKey = (event: KeyboardEvent) => {
            if (event.key === "Escape") setOpen(false);
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [open]);

    useEffect(() => {
        sentinelSeen.current = false;
        lastFillHeight.current = 0;
    }, [rows, open]);

    const earlierCount = Math.max(earlier.length, serverCount);
    const openArchive = () => {
        const gen = ++loadGen.current;
        if (!open) setMountedCount(CONVERSATION_ARCHIVE_RENDER_PAGE);
        setOpen(true);
        setLoading(true);
        const load = scope.startsWith("tab:")
            ? (typeof LoadProjectTabConversationArchive === "function"
                ? LoadProjectTabConversationArchive(scope.slice(4))
                : Promise.resolve([]))
            : (typeof LoadAIAssistantUIArchive === "function"
                ? LoadAIAssistantUIArchive()
                : Promise.resolve([]));
        void Promise.resolve(load).then((remote) => {
            if (loadGen.current !== gen) return;
            const currentMessages = messagesRef.current;
            const currentEarlier = earlierRef.current;
            const currentScope = scopeRef.current;
            const known = new Set(currentMessages.map((message) => message.id));
            const older = asChatMessages(remote).filter((message) => {
                if (currentScope === "desktop" && !messageIsLocalSession(message)) return false;
                return !message.id || !known.has(message.id);
            });
            const next = dedupeMessages([...older, ...currentEarlier]);
            setRows((prev) => reuseArchiveRows(prev, next));
        }).catch(() => {
            if (loadGen.current !== gen) return;
            const next = dedupeMessages(earlierRef.current);
            setRows((prev) => reuseArchiveRows(prev, next));
        }).finally(() => {
            if (loadGen.current === gen) setLoading(false);
        });
    };

    const shownRows = rows.slice(0, mountedCount);
    const hasMoreRows = shownRows.length < rows.length;
    const showMore = () => setMountedCount((count) => Math.min(rows.length, count + CONVERSATION_ARCHIVE_RENDER_PAGE));

    useEffect(() => {
        if (!open || !hasMoreRows) return;
        const node = sentinelRef.current;
        const root = scrollerRef.current;
        if (!node || !root || typeof IntersectionObserver === "undefined") return;
        paging.current = false;
        const observer = new IntersectionObserver((entries) => {
            const visible = entries.some((entry) => entry.isIntersecting);
            if (!visible) {
                sentinelSeen.current = false;
                return;
            }
            const scroller = scrollerRef.current;
            // No layout yet (or a zero-height host): do not walk the whole archive.
            if (!scroller || scroller.clientHeight <= 0 || scroller.scrollHeight <= 0 || paging.current) return;
            const overflows = scroller.scrollHeight > scroller.clientHeight + 8;
            if (!overflows && lastFillHeight.current > 0 && scroller.scrollHeight <= lastFillHeight.current) return;
            // The end staying on screen after a fill is not a new scroll.
            if (overflows && sentinelSeen.current) return;
            if (!overflows) lastFillHeight.current = scroller.scrollHeight;
            sentinelSeen.current = true;
            paging.current = true;
            setMountedCount((count) => Math.min(rows.length, count + CONVERSATION_ARCHIVE_RENDER_PAGE));
        }, { root });
        observer.observe(node);
        return () => observer.disconnect();
    }, [open, hasMoreRows, rows.length, mountedCount]);

    const banner = earlierCount > 0 ? (
        <button
            type="button"
            data-testid="conversation-archive-banner"
            onClick={openArchive}
            style={{
                display: "block",
                margin: "0 auto 12px",
                padding: "4px 12px",
                borderRadius: "999px",
                border: `1px solid ${t.titleBarBorder}`,
                background: t.fieldBg,
                color: t.textMuted,
                fontSize: "12px",
                lineHeight: 1.4,
                cursor: "pointer",
            }}
        >
            {localizeText(lang, `View ${earlierCount} earlier messages`, `查看更早的 ${earlierCount} 条对话`, `查看更早的 ${earlierCount} 條對話`)}
        </button>
    ) : null;

    const dialog = open ? (
        <div
            ref={dialogRef}
            role="dialog"
            aria-modal="true"
            tabIndex={-1}
            data-testid="conversation-archive-dialog"
            aria-label={localizeText(lang, "Earlier messages", "更早的对话", "更早的對話")}
            style={{
                position: "fixed",
                inset: "48px 24px",
                zIndex: 40,
                display: "flex",
                flexDirection: "column",
                minHeight: 0,
                overflow: "hidden",
                background: t.bg,
                color: t.text,
                border: `1px solid ${t.titleBarBorder}`,
                borderRadius: "12px",
                boxShadow: "0 16px 48px rgba(0,0,0,0.18)",
                outline: "none",
            }}
        >
            <div style={{ display: "flex", alignItems: "center", gap: "8px", padding: "10px 14px", borderBottom: `1px solid ${t.titleBarBorder}` }}>
                <span style={{ flex: 1, fontSize: "13px", fontWeight: 600 }}>
                    {localizeText(lang, "Earlier messages", "更早的对话", "更早的對話")}
                </span>
                <button
                    type="button"
                    data-testid="conversation-archive-close"
                    onClick={() => setOpen(false)}
                    style={{ background: "none", border: "none", color: t.textMuted, cursor: "pointer", fontSize: "12px" }}
                >
                    {localizeText(lang, "Close", "关闭", "關閉")}
                </button>
            </div>
            <div ref={scrollerRef} style={{ flex: 1, minHeight: 0, minWidth: 0, overflowY: "auto", overscrollBehavior: "contain", padding: "12px 14px" }}>
                {loading && rows.length > 0 ? (
                    <div style={{ color: t.textMuted, fontSize: "12px", marginBottom: "8px" }}>{localizeText(lang, "Loading...", "加载中...", "載入中...")}</div>
                ) : null}
                {rows.length === 0 ? (
                    <div style={{ color: t.textMuted, fontSize: "12px" }}>{loading
                        ? localizeText(lang, "Loading...", "加载中...", "載入中...")
                        : localizeText(lang, "No earlier messages", "没有更早的对话", "沒有更早的對話")}</div>
                ) : (
                    <>
                        {shownRows.map((message) => (
                            <ArchiveMessageRow key={message.id} message={message} lang={lang} theme={t} />
                        ))}
                        {hasMoreRows ? (
                            <>
                                <div ref={sentinelRef} data-testid="conversation-archive-more-sentinel" style={{ height: 1 }} />
                                <button
                                    type="button"
                                    data-testid="conversation-archive-more"
                                    onClick={showMore}
                                    style={{
                                        display: "block",
                                        margin: "4px auto 8px",
                                        padding: "4px 12px",
                                        borderRadius: "999px",
                                        border: `1px solid ${t.titleBarBorder}`,
                                        background: t.fieldBg,
                                        color: t.textMuted,
                                        fontSize: "12px",
                                        cursor: "pointer",
                                    }}
                                >
                                    {localizeText(lang, "Show more", "显示更多", "顯示更多")}
                                </button>
                            </>
                        ) : null}
                    </>
                )}
            </div>
        </div>
    ) : null;

    return { visibleMessages: visible, earlierCount, banner, dialog };
}
