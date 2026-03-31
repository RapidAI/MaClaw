import { useState, useRef, useCallback, useEffect, useMemo } from "react";
import { ShowItemInFolder } from "../../../wailsjs/go/main/App";
import { BrowserOpenURL } from "../../../wailsjs/runtime";
import type { ChatMessage } from "./useAIAssistant";
import { findLastIndex } from "./useAIAssistant";

interface AIAssistantPanelProps {
    onClose: () => void;
    lang: string; // 'zh-Hans' | 'zh-Hant' | 'en'
    messages: ChatMessage[];
    sending: boolean;
    streaming: boolean;
    ready: boolean;
    initStatus?: string; // "connecting" | "loading" | "warming" | "ready"
    sendMessage: (text: string) => Promise<void>;
    clearHistory: () => Promise<void>;
    executeAction: (command: string) => Promise<void>;
    refreshNews: () => void;
    scrollToTopSeq?: number; // bumped when panel should scroll to top (e.g. after news reload)
    inline?: boolean; // when true, render as inline content instead of overlay
    onHideWindow?: () => void; // hide the entire window (inline mode)
    onboardingIncomplete?: boolean; // true when onboarding was dismissed before completion
    onOpenOnboarding?: () => void; // callback to re-open the onboarding wizard
}

/* ── Theme definitions ── */

interface Theme {
    bg: string;
    titleBarBg: string;
    titleBarBorder: string;
    titleText: string;
    text: string;
    textMuted: string;
    inputBarBg: string;
    inputBarBorder: string;
    inputText: string;
    codeBg: string;
    codeText: string;
    codeBlockBg: string;
    codeBlockBorder: string;
    codeBlockLang: string;
    borderLeft: string;
    responseBorderLeft: string;
    headingColor: string;
    linkColor: string;
    pathColor: string;
    promptColor: string;
    userColor: string;
    divider: string;
    fieldBg: string;
    fieldBorder: string;
    fieldLabel: string;
    errorText: string;
    errorBg: string;
    errorBorder: string;
    emptyHint: string;
    boldColor: string;
    italicColor: string;
    bulletColor: string;
    quoteBorder: string;
    quoteText: string;
    btnColor: string;
    btnBorder: string;
    actionBtnColor: string;
    closeBtnColor: string;
    sendBtnColor: string;
    sendBtnBorder: string;
}

const overlayTheme: Theme = {
    bg: "#eef0f5",
    titleBarBg: "#e2e4ea",
    titleBarBorder: "#c8cad0",
    titleText: "#555",
    text: "#2d2d2d",
    textMuted: "#777",
    inputBarBg: "#ffffff",
    inputBarBorder: "#6366f1",
    inputText: "#222",
    codeBg: "#e4e6ec",
    codeText: "#b5314a",
    codeBlockBg: "#e8eaf0",
    codeBlockBorder: "#c8cad0",
    codeBlockLang: "#999",
    borderLeft: "#c8cad0",
    responseBorderLeft: "#b4b6d4",
    headingColor: "#5558d6",
    linkColor: "#5558d6",
    pathColor: "#059669",
    promptColor: "#5558d6",
    userColor: "#5558d6",
    divider: "#d0d2d8",
    fieldBg: "#e8eaf0",
    fieldBorder: "#c8cad0",
    fieldLabel: "#777",
    errorText: "#dc2626",
    errorBg: "rgba(220, 38, 38, 0.06)",
    errorBorder: "#dc2626",
    emptyHint: "#999",
    boldColor: "#1a1a1a",
    italicColor: "#333",
    bulletColor: "#888",
    quoteBorder: "#b4b6d4",
    quoteText: "#666",
    btnColor: "#5558d6",
    btnBorder: "#5558d6",
    actionBtnColor: "#777",
    closeBtnColor: "#888",
    sendBtnColor: "#fff",
    sendBtnBorder: "#5558d6",
};

const lightTheme: Theme = {
    bg: "#fafbff",
    titleBarBg: "#f0f1f5",
    titleBarBorder: "#ddd",
    titleText: "#666",
    text: "#333",
    textMuted: "#888",
    inputBarBg: "#f5f6fa",
    inputBarBorder: "#ddd",
    inputText: "#333",
    codeBg: "#f0f0f5",
    codeText: "#c7254e",
    codeBlockBg: "#f5f6fa",
    codeBlockBorder: "#ddd",
    codeBlockLang: "#aaa",
    borderLeft: "#ddd",
    responseBorderLeft: "#d4d4f7",
    headingColor: "#6366f1",
    linkColor: "#6366f1",
    pathColor: "#059669",
    promptColor: "#6366f1",
    userColor: "#6366f1",
    divider: "#e5e7eb",
    fieldBg: "#f5f6fa",
    fieldBorder: "#ddd",
    fieldLabel: "#888",
    errorText: "#dc2626",
    errorBg: "rgba(220, 38, 38, 0.06)",
    errorBorder: "#dc2626",
    emptyHint: "#aaa",
    boldColor: "#222",
    italicColor: "#444",
    bulletColor: "#999",
    quoteBorder: "#d4d4f7",
    quoteText: "#777",
    btnColor: "#6366f1",
    btnBorder: "#6366f1",
    actionBtnColor: "#888",
    closeBtnColor: "#999",
    sendBtnColor: "#6366f1",
    sendBtnBorder: "#6366f1",
};

/* ── Style constants ── */

const overlayStyle: React.CSSProperties = {
    position: "fixed",
    inset: 0,
    zIndex: 10000,
    display: "flex",
    flexDirection: "column",
    background: overlayTheme.bg,
    textAlign: "left",
    boxShadow: "0 0 40px rgba(0,0,0,0.08)",
};

const dotBase: React.CSSProperties = {
    width: 10,
    height: 10,
    borderRadius: "50%",
    display: "inline-block",
    cursor: "pointer",
};

const baseInputBtnStyle: React.CSSProperties = {
    background: "transparent",
    border: "1px solid",
    borderRadius: "4px",
    padding: "6px 12px",
    fontSize: "13px",
    fontFamily: "Consolas, monospace",
    cursor: "pointer",
    lineHeight: 1,
    minHeight: "34px",
    flexShrink: 0,
};

const baseActionBtnStyle: React.CSSProperties = {
    background: "transparent",
    border: "none",
    fontSize: "11px",
    fontFamily: "Consolas, monospace",
    cursor: "pointer",
    padding: "4px 8px",
    borderRadius: "4px",
    lineHeight: 1,
    minHeight: "28px",
    minWidth: "28px",
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
};

/* ── Themed inline markdown rendering ── */

function renderInlineMarkdown(text: string, t: Theme): React.ReactNode[] {
    if (!text) return ["\u00A0"];
    const parts: React.ReactNode[] = [];
    const re = /(`[^`]+`)|(\*\*[^*]+\*\*)|(\*[^\s*][^*]*?\*)|(\[[^\]]+\]\([^)]+\))|([A-Za-z]:\\[\w\\.\-]+(?:\.\w+)?)|((~|\/(?:Users|home|tmp|var|opt|etc|usr))[\w/.\-]+)/g;
    let lastIndex = 0;
    let match: RegExpExecArray | null;
    let idx = 0;
    while ((match = re.exec(text)) !== null) {
        if (match.index > lastIndex) {
            parts.push(text.slice(lastIndex, match.index));
        }
        const m = match[0];
        if (match[1]) {
            parts.push(<code key={idx++} style={{ background: t.codeBg, color: t.codeText, padding: "1px 4px", borderRadius: "3px", fontSize: "0.92em" }}>{m.slice(1, -1)}</code>);
        } else if (match[2]) {
            parts.push(<strong key={idx++} style={{ color: t.boldColor, fontWeight: 700 }}>{m.slice(2, -2)}</strong>);
        } else if (match[3]) {
            parts.push(<em key={idx++} style={{ color: t.italicColor }}>{m.slice(1, -1)}</em>);
        } else if (match[4]) {
            const lm = m.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
            if (lm) {
                const href = lm[2];
                if (/^https?:\/\//i.test(href)) {
                    parts.push(<a key={idx++} href="#" onClick={(e) => { e.preventDefault(); BrowserOpenURL(href); }} style={{ color: t.linkColor, textDecoration: "underline", cursor: "pointer" }}>{lm[1]}</a>);
                } else {
                    parts.push(<span key={idx++} style={{ color: t.linkColor }}>{lm[1]}</span>);
                }
            } else {
                parts.push(m);
            }
        } else if (match[5] || match[6]) {
            const filePath = m;
            parts.push(
                <a key={idx++}
                   href="#"
                   onClick={(e) => { e.preventDefault(); ShowItemInFolder(filePath); }}
                   style={{ color: t.pathColor, textDecoration: "underline", cursor: "pointer" }}
                   title={filePath}
                >📂 {filePath}</a>
            );
        }
        lastIndex = match.index + m.length;
    }
    if (lastIndex < text.length) {
        parts.push(text.slice(lastIndex));
    }
    return parts.length > 0 ? parts : ["\u00A0"];
}

function renderMarkdownLine(text: string, key: string | number, t: Theme): React.ReactNode {
    const trimmed = text.trimStart();

    const headingMatch = trimmed.match(/^(#{1,4})\s+(.+)$/);
    if (headingMatch) {
        const level = headingMatch[1].length;
        const sizes: Record<number, string> = { 1: "1.2em", 2: "1.1em", 3: "1.0em", 4: "0.95em" };
        return (
            <div key={key} style={{ fontSize: sizes[level] || "1em", fontWeight: 700, color: t.headingColor, margin: "0.4em 0 0.2em" }}>
                {renderInlineMarkdown(headingMatch[2], t)}
            </div>
        );
    }

    if (/^>\s/.test(trimmed)) {
        return (
            <div key={key} style={{ borderLeft: `2px solid ${t.quoteBorder}`, paddingLeft: "8px", color: t.quoteText, fontStyle: "italic", minHeight: "1.4em" }}>
                {renderInlineMarkdown(trimmed.slice(2), t)}
            </div>
        );
    }

    if (/^[-*]\s/.test(trimmed)) {
        return (
            <div key={key} style={{ paddingLeft: "1em", textIndent: "-0.7em", minHeight: "1.4em" }}>
                <span style={{ color: t.bulletColor }}>•</span>{" "}
                {renderInlineMarkdown(trimmed.slice(2), t)}
            </div>
        );
    }

    const numMatch = trimmed.match(/^(\d+)[.)]\s+(.+)$/);
    if (numMatch) {
        return (
            <div key={key} style={{ paddingLeft: "1.2em", textIndent: "-1.2em", minHeight: "1.4em" }}>
                <span style={{ color: t.bulletColor }}>{numMatch[1]}.</span>{" "}
                {renderInlineMarkdown(numMatch[2], t)}
            </div>
        );
    }

    return (
        <div key={key} style={{ minHeight: "1.4em" }}>
            {renderInlineMarkdown(text, t) || "\u00A0"}
        </div>
    );
}

/* ── Structured response rendering ── */

function renderContentWithCodeBlocks(content: string, t: Theme): React.ReactNode[] {
    const elements: React.ReactNode[] = [];
    const lines = content.split("\n");
    let inCodeBlock = false;
    let codeBlockLines: string[] = [];
    let codeBlockLang = "";
    let lineIdx = 0;

    const flushCodeBlock = () => {
        if (codeBlockLines.length > 0) {
            elements.push(
                <pre key={`code-${elements.length}`} style={{
                    background: t.codeBlockBg,
                    border: `1px solid ${t.codeBlockBorder}`,
                    borderRadius: "4px",
                    padding: "8px 10px",
                    margin: "4px 0",
                    fontSize: "0.9em",
                    overflowX: "auto",
                    color: t.codeText,
                    lineHeight: 1.5,
                }}>
                    {codeBlockLang && <div style={{ color: t.codeBlockLang, fontSize: "0.85em", marginBottom: "4px" }}>{codeBlockLang}</div>}
                    <code>{codeBlockLines.join("\n")}</code>
                </pre>
            );
        }
        codeBlockLines = [];
        codeBlockLang = "";
    };

    for (const line of lines) {
        if (/^```/.test(line.trimStart())) {
            if (inCodeBlock) {
                flushCodeBlock();
                inCodeBlock = false;
            } else {
                inCodeBlock = true;
                codeBlockLang = line.trimStart().slice(3).trim();
            }
        } else if (inCodeBlock) {
            codeBlockLines.push(line);
        } else {
            elements.push(renderMarkdownLine(line, `md-${lineIdx}`, t));
        }
        lineIdx++;
    }
    if (inCodeBlock) flushCodeBlock();
    return elements;
}

function renderFields(fields: Array<{ label: string; value: string }>, t: Theme): React.ReactNode {
    return (
        <div style={{ display: "flex", flexWrap: "wrap", gap: "6px", margin: "4px 0" }}>
            {fields.map((f, i) => (
                <div key={`field-${i}`} data-testid="field-card" style={{
                    background: t.fieldBg,
                    border: `1px solid ${t.fieldBorder}`,
                    borderRadius: "4px",
                    padding: "4px 8px",
                    fontSize: "12px",
                }}>
                    <span style={{ color: t.fieldLabel, marginRight: "6px" }}>{f.label}:</span>
                    <span style={{ color: t.text }}>{f.value}</span>
                </div>
            ))}
        </div>
    );
}

function renderActions(
    actions: Array<{ label: string; command: string; style: string }>,
    executeAction: (command: string) => void,
    t: Theme,
): React.ReactNode {
    return (
        <div style={{ display: "flex", flexWrap: "wrap", gap: "6px", margin: "4px 0" }}>
            {actions.map((a, i) => (
                <button
                    key={`action-${i}`}
                    data-testid="action-button"
                    onClick={() => executeAction(a.command)}
                    style={{
                        ...baseInputBtnStyle,
                        color: a.style === "danger" ? t.errorText : t.btnColor,
                        borderColor: a.style === "danger" ? t.errorText : t.btnBorder,
                        fontSize: "12px",
                        padding: "4px 10px",
                        minHeight: "28px",
                    }}
                >
                    {a.label}
                </button>
            ))}
        </div>
    );
}

/* ── Render a single ChatMessage ── */

function renderMessage(msg: ChatMessage, executeAction: (cmd: string) => void, t: Theme, isLastAssistant: boolean): React.ReactNode {
    switch (msg.role) {
        case "user":
            return (
                <div key={msg.id}>
                    <div style={{ borderTop: `1px solid ${t.divider}`, margin: "8px 0 4px 0" }} />
                    <div style={{ color: t.userColor, fontWeight: 600, padding: "3px 0 3px 1.2em", overflowWrap: "break-word", whiteSpace: "pre-wrap", textIndent: "-1.2em" }}>
                        ❯ {msg.content}
                    </div>
                </div>
            );
        case "assistant":
            return (
                <div key={msg.id} style={{
                    padding: "4px 0 4px 8px",
                    borderLeft: `2px solid ${t.responseBorderLeft}`,
                    margin: "2px 0",
                    color: t.text,
                }}>
                    {/* Streaming: show blinking cursor only on the last assistant message */}
                    {isLastAssistant && !msg.content && !msg.fields && !msg.thumbnailBase64 && !msg.localFilePaths?.length && (
                        <span style={{ opacity: 0.5, animation: "blink 1s step-end infinite" }}>▍</span>
                    )}
                    {msg.thumbnailBase64 && msg.localFilePath && (
                        <div style={{ margin: "4px 0 6px 0" }}>
                            <a href="#" onClick={(e) => { e.preventDefault(); ShowItemInFolder(msg.localFilePath!); }}
                               style={{ display: "inline-block", cursor: "pointer" }}
                               title={msg.localFilePath}>
                                <img
                                    src={`data:image/png;base64,${msg.thumbnailBase64}`}
                                    alt="screenshot"
                                    style={{
                                        maxWidth: "180px", maxHeight: "120px",
                                        borderRadius: "4px", border: `1px solid ${t.borderLeft}`,
                                        objectFit: "contain",
                                    }}
                                />
                            </a>
                        </div>
                    )}
                    {renderContentWithCodeBlocks(msg.content, t)}
                    {msg.localFilePaths && msg.localFilePaths.length > 0 && (
                        <div style={{ margin: "4px 0" }}>
                            {msg.localFilePaths.map((fp, i) => (
                                <div key={i} style={{ padding: "2px 0" }}>
                                    <a href="#"
                                       onClick={(e) => { e.preventDefault(); ShowItemInFolder(fp); }}
                                       style={{ color: t.pathColor, textDecoration: "underline", cursor: "pointer", wordBreak: "break-all" }}
                                       title={fp}>
                                        📄 文件已保存: 📁 {fp}
                                    </a>
                                </div>
                            ))}
                        </div>
                    )}
                    {msg.fields && msg.fields.length > 0 && renderFields(msg.fields, t)}
                    {msg.actions && msg.actions.length > 0 && renderActions(msg.actions, executeAction, t)}
                </div>
            );
        case "progress":
            return (
                <div key={msg.id} style={{ color: t.textMuted, fontSize: "11px", padding: "1px 0", fontStyle: "italic" }}>
                    {msg.content}
                </div>
            );
        case "system":
            return (
                <div key={msg.id} style={{
                    padding: "8px 12px",
                    margin: "4px 0",
                    borderRadius: "6px",
                    background: "linear-gradient(135deg, rgba(99,102,241,0.06), rgba(139,92,246,0.06))",
                    borderLeft: `3px solid ${t.promptColor}`,
                    color: t.text,
                    fontSize: "12px",
                    lineHeight: "1.6",
                }}>
                    {renderContentWithCodeBlocks(msg.content, t)}
                </div>
            );
        case "error":
            return (
                <div key={msg.id} style={{
                    color: t.errorText,
                    background: t.errorBg,
                    borderLeft: `2px solid ${t.errorBorder}`,
                    padding: "4px 8px",
                    margin: "2px 0",
                    borderRadius: "2px",
                    fontSize: "12px",
                }}>
                    {msg.content}
                </div>
            );
        default:
            return null;
    }
}

/* ── Inject blink animation once at module level ── */
if (typeof document !== "undefined" && !document.getElementById("ai-blink-style")) {
    const style = document.createElement("style");
    style.id = "ai-blink-style";
    style.textContent = "@keyframes blink { 50% { opacity: 0; } }";
    document.head.appendChild(style);
}

/* ── Main component ── */

export function AIAssistantPanel({ onClose, lang, messages, sending, streaming, ready, initStatus, sendMessage, clearHistory, executeAction, refreshNews, scrollToTopSeq, inline, onHideWindow, onboardingIncomplete, onOpenOnboarding }: AIAssistantPanelProps) {
    const [inputValue, setInputValue] = useState("");
    const [composing, setComposing] = useState(false);
    const inputRef = useRef<HTMLTextAreaElement | null>(null);
    const outputEndRef = useRef<HTMLDivElement | null>(null);
    const outputContainerRef = useRef<HTMLDivElement | null>(null);
    const userScrolledUpRef = useRef(false);
    const prevMsgCountRef = useRef(0);
    const scrollTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

    const t = inline ? lightTheme : overlayTheme;

    const title = lang === "en" ? "AI Assistant" : "AI 助手";
    const thinkingText = lang === "en" ? "Thinking..." : "正在思考...";

    const initStatusLabels: Record<string, Record<string, string>> = {
        connecting: { en: "Connecting to Hub...", zh: "正在连接 Hub..." },
        loading:    { en: "Loading components...", zh: "正在加载组件..." },
        warming:    { en: "Warming up...", zh: "正在预热..." },
        ready:      { en: "Ready", zh: "就绪" },
    };
    const statusKey = initStatus || "connecting";
    const initLabel = (initStatusLabels[statusKey] || initStatusLabels.connecting)[lang === "en" ? "en" : "zh"];

    const placeholderText = !ready
        ? initLabel
        : streaming
        ? (lang === "en" ? "Thinking..." : "正在思考...")
        : sending
            ? (lang === "en" ? "Processing..." : "处理中...")
            : (lang === "en" ? "Type a message..." : "输入消息...");

    // Debounced auto-scroll: coalesce rapid token updates into a single scroll
    useEffect(() => {
        if (userScrolledUpRef.current) {
            prevMsgCountRef.current = messages.length;
            return;
        }
        // New message added → scroll immediately
        if (messages.length !== prevMsgCountRef.current) {
            prevMsgCountRef.current = messages.length;
            outputEndRef.current?.scrollIntoView({ behavior: "smooth" });
            return;
        }
        // Content update on existing message (streaming tokens) → debounce 80ms
        if (scrollTimerRef.current) clearTimeout(scrollTimerRef.current);
        scrollTimerRef.current = setTimeout(() => {
            outputEndRef.current?.scrollIntoView({ behavior: "smooth" });
        }, 80);
        return () => {
            if (scrollTimerRef.current) clearTimeout(scrollTimerRef.current);
        };
    }, [messages]);

    // Track user scroll position
    const handleScroll = useCallback(() => {
        const container = outputContainerRef.current;
        if (!container) return;
        const threshold = 80;
        userScrolledUpRef.current =
            container.scrollHeight - container.scrollTop - container.clientHeight > threshold;
    }, []);

    // Scroll to top when pinned news are (re)loaded — e.g. after clear history
    // or app restart — so the user sees the pinned messages first.
    useEffect(() => {
        if (!scrollToTopSeq) return;
        const container = outputContainerRef.current;
        if (container) {
            container.scrollTo({ top: 0, behavior: "smooth" });
            userScrolledUpRef.current = true; // prevent auto-scroll-to-bottom from overriding
        }
    }, [scrollToTopSeq]);

    // Focus input on mount
    useEffect(() => {
        const timer = setTimeout(() => inputRef.current?.focus(), 100);
        return () => clearTimeout(timer);
    }, []);

    // Escape key closes panel (only in overlay mode, not inline)
    useEffect(() => {
        if (inline) return;
        const handler = (e: KeyboardEvent) => {
            if (e.key === "Escape") onClose();
        };
        window.addEventListener("keydown", handler);
        return () => window.removeEventListener("keydown", handler);
    }, [onClose, inline]);

    const handleSend = useCallback(async () => {
        const text = inputValue.trim();
        if (!text || sending) return;
        setInputValue("");
        // Reset textarea height after send
        if (inputRef.current) {
            inputRef.current.style.height = "auto";
        }
        userScrolledUpRef.current = false;
        await sendMessage(text);
    }, [inputValue, sending, sendMessage]);

    // Split messages: pinned news vs regular messages
    const { pinnedNews, otherMessages } = useMemo(() => {
        const pinned: ChatMessage[] = [];
        const other: ChatMessage[] = [];
        for (const m of messages) {
            if (m.role === 'system' && m.id.startsWith('news-')) {
                pinned.push(m);
            } else {
                other.push(m);
            }
        }
        return { pinnedNews: pinned.slice(0, 2), otherMessages: other };
    }, [messages]);

    // Memoize rendered non-news messages
    const renderedOtherMessages = useMemo(() => {
        const lastAssistantIdx = findLastIndex(messages, m => m.role === 'assistant');
        return otherMessages.map(msg => {
            const origIdx = messages.indexOf(msg);
            return renderMessage(msg, executeAction, t, origIdx === lastAssistantIdx);
        });
    }, [otherMessages, messages, executeAction, t]);

    const containerStyle: React.CSSProperties = inline
        ? { display: "flex", flexDirection: "column", background: t.bg, textAlign: "left", width: "100%", height: "100%", position: "relative" }
        : overlayStyle;

    return (
        <div style={containerStyle}>
            <style>{`.pinned-news-card > div { margin-top: 0 !important; margin-bottom: 0 !important; }`}</style>
            {/* ── Drag overlay (inline mode) ── */}
            {inline && (
                <div style={{
                    height: "30px", width: "100%",
                    position: "absolute", top: 0, left: 0, zIndex: 999,
                    '--wails-draggable': 'drag',
                } as any} />
            )}
            {/* ── Title bar ── */}
            <div style={{
                display: "flex", alignItems: "center", justifyContent: "space-between",
                padding: "0 10px", height: "36px",
                background: t.titleBarBg, borderBottom: `1px solid ${t.titleBarBorder}`,
                flexShrink: 0, gap: "6px",
                ...(inline ? { '--wails-draggable': 'drag' } as any : {}),
            }}>
                <div style={{ display: "flex", alignItems: "center", gap: "8px", minWidth: 0, flex: 1 }}>
                    {!inline && (
                        <div style={{ display: "flex", gap: "5px", flexShrink: 0 }}>
                            <span
                                style={{ ...dotBase, background: "#ff5f57" }}
                                onClick={onClose}
                                title={lang === "en" ? "Close" : "关闭"}
                            />
                        </div>
                    )}
                    <span style={{
                        color: t.titleText, fontSize: "11px",
                        fontFamily: "Consolas, 'SF Mono', monospace",
                        overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap",
                    }}>{title}</span>
                </div>
                <div style={{ display: "flex", gap: "4px", flexShrink: 0, ...(inline ? { '--wails-draggable': 'no-drag', position: 'relative', zIndex: 1000 } as any : {}) }}>
                    <button
                        {...(inline ? { onMouseDown: refreshNews } : { onClick: refreshNews })}
                        style={{ ...baseActionBtnStyle, color: t.actionBtnColor }}
                        title={lang === "en" ? "Refresh news" : "刷新消息"}
                    >
                        🔄
                    </button>
                    <button
                        {...(inline ? { onMouseDown: clearHistory } : { onClick: clearHistory })}
                        style={{ ...baseActionBtnStyle, color: t.actionBtnColor }}
                        title={lang === "en" ? "Clear history" : "清空历史"}
                    >
                        🗑️
                    </button>
                    {inline && onHideWindow && (
                    <button
                        onMouseDown={(e) => { e.preventDefault(); e.stopPropagation(); onHideWindow(); }}
                        className="btn-hide"
                        style={{ fontSize: "11px", padding: "1px 8px", cursor: "pointer" }}
                        title={lang === "en" ? "Hide" : "隐藏"}
                    >
                        {lang === "en" ? "Hide" : lang === "zh-Hant" ? "隱藏" : "隐藏"}
                    </button>
                    )}
                    {!inline && (
                    <button
                        onClick={onClose}
                        style={{ ...baseActionBtnStyle, color: t.closeBtnColor, fontSize: "14px", padding: "0 8px" }}
                        title={lang === "en" ? "Close" : "关闭"}
                    >
                        ✕
                    </button>
                    )}
                </div>
            </div>

            {/* ── Chat area ── */}
            <div
                ref={outputContainerRef}
                style={{
                    flex: 1, minHeight: 0, maxHeight: "none",
                    padding: "8px 10px", fontSize: "12px", lineHeight: 1.5,
                    overflowY: "auto", overflowX: "hidden", textAlign: "left",
                    color: t.text, background: t.bg,
                    fontFamily: "'Cascadia Code', 'Cascadia Mono', 'Consolas', 'Courier New', monospace",
                    whiteSpace: "pre-wrap", wordBreak: "break-all",
                }}
                onScroll={handleScroll}
            >
                {onboardingIncomplete ? (
                    <div style={{ display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", height: "100%", gap: "16px" }}>
                        <div style={{ color: t.textMuted, fontSize: "13px" }}>
                            {lang === "en" ? "Setup not completed" : "设置未完成"}
                        </div>
                        <button
                            onClick={onOpenOnboarding}
                            style={{
                                padding: "10px 28px", fontSize: "15px", fontWeight: 600,
                                background: "linear-gradient(135deg, #6366f1, #8b5cf6)",
                                color: "#fff", border: "none", borderRadius: "8px",
                                cursor: "pointer", transition: "opacity 0.2s",
                            }}
                            onMouseEnter={e => (e.currentTarget.style.opacity = "0.85")}
                            onMouseLeave={e => (e.currentTarget.style.opacity = "1")}
                        >
                            {lang === "en" ? "Complete Setup" : "完成设置"}
                        </button>
                    </div>
                ) : !ready ? (
                    <div style={{ display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", height: "100%", gap: "12px" }}>
                        <div style={{
                            width: "28px", height: "28px",
                            border: `3px solid ${t.inputBarBorder}`,
                            borderTop: `3px solid ${t.promptColor}`,
                            borderRadius: "50%",
                            animation: "maclaw-spin 0.8s linear infinite",
                        }} />
                        <style>{`@keyframes maclaw-spin { to { transform: rotate(360deg); } }`}</style>
                        <div style={{ color: t.textMuted, fontSize: "12px" }}>
                            {initLabel}
                        </div>
                    </div>
                ) : messages.length === 0 ? (
                    <span style={{ color: t.emptyHint }}>
                        {lang === "en" ? "Ask me anything..." : "有什么可以帮你的？"}
                    </span>
                ) : (
                    <>
                        {pinnedNews.length > 0 && (
                            <div style={{
                                display: 'grid',
                                gridTemplateColumns: pinnedNews.length >= 2 ? '1fr 1fr' : '1fr',
                                gap: '6px',
                                marginBottom: '6px',
                            }}>
                                {pinnedNews.map(msg => {
                                    // Split content into title (first line) and body (rest)
                                    const lines = msg.content.split('\n');
                                    const titleLine = lines[0] || '';
                                    const bodyLines = lines.slice(1).filter(l => l.trim() !== '');
                                    const bodyText = bodyLines.join('\n');
                                    // Plain text for tooltip (strip markdown bold markers)
                                    const plainTitle = titleLine.replace(/\*\*/g, '');
                                    const plainBody = bodyLines.map(l => l.replace(/\*\*/g, '')).join('\n');
                                    const tooltipText = plainTitle + (plainBody ? '\n' + plainBody : '');
                                    return (
                                    <div key={msg.id} className="pinned-news-card" title={tooltipText} style={{
                                        padding: "6px 8px",
                                        borderRadius: "6px",
                                        background: "linear-gradient(135deg, rgba(99,102,241,0.06), rgba(139,92,246,0.06))",
                                        borderLeft: `3px solid ${t.promptColor}`,
                                        color: t.text,
                                        fontSize: "11px",
                                        lineHeight: "1.4",
                                        overflow: "hidden",
                                    }}>
                                        {/* Title: single line, ellipsis */}
                                        <div style={{
                                            overflow: "hidden",
                                            textOverflow: "ellipsis",
                                            whiteSpace: "nowrap",
                                            fontWeight: 600,
                                        }}>
                                            {renderInlineMarkdown(titleLine, t)}
                                        </div>
                                        {/* Body: max 2 lines, ellipsis */}
                                        {bodyText && (
                                        <div style={{
                                            overflow: "hidden",
                                            display: "-webkit-box",
                                            WebkitLineClamp: 2,
                                            WebkitBoxOrient: "vertical" as any,
                                            marginTop: "2px",
                                            color: t.textMuted,
                                        }}>
                                            {renderInlineMarkdown(bodyText, t)}
                                        </div>
                                        )}
                                    </div>
                                    );
                                })}
                            </div>
                        )}
                        {renderedOtherMessages}
                    </>
                )}
                {streaming && (
                    <div style={{ color: t.textMuted, fontSize: "11px", padding: "4px 0", fontStyle: "italic" }}>
                        {thinkingText}
                    </div>
                )}
                <div ref={outputEndRef} />
            </div>

            {/* ── Input bar ── */}
            <div style={{
                display: "flex", alignItems: "flex-end", gap: "8px",
                padding: "8px 12px", paddingBottom: "max(8px, env(safe-area-inset-bottom))",
                background: t.inputBarBg, borderTop: inline ? `1px solid ${t.inputBarBorder}` : "none",
                flexShrink: 0,
                ...(inline ? {} : { margin: "0 10px 10px 10px", borderRadius: "8px", border: `1.5px solid ${t.inputBarBorder}` }),
            }}>
                <span style={{
                    color: t.promptColor, fontFamily: "Consolas, monospace",
                    fontSize: "13px", flexShrink: 0, userSelect: "none",
                    paddingBottom: "8px",
                }}>❯</span>
                <textarea
                    ref={inputRef}
                    disabled={!ready}
                    style={{
                        flex: 1, minWidth: 0, background: "transparent",
                        border: "none", outline: "none", color: t.inputText,
                        fontFamily: "Consolas, 'Courier New', monospace",
                        fontSize: "14px", padding: "8px 0",
                        resize: "none", overflow: "auto",
                        minHeight: "36px", maxHeight: "120px",
                        lineHeight: 1.4,
                        opacity: ready ? 1 : 0.5,
                    }}
                    rows={1}
                    value={inputValue}
                    onChange={(e) => {
                        setInputValue(e.target.value);
                        // Auto-resize height
                        const el = e.target;
                        el.style.height = "auto";
                        el.style.height = Math.min(el.scrollHeight, 120) + "px";
                    }}
                    onCompositionStart={() => setComposing(true)}
                    onCompositionEnd={() => setComposing(false)}
                    onKeyDown={(e) => {
                        if (e.key === "Enter" && !e.shiftKey && !composing) {
                            e.preventDefault();
                            handleSend();
                        }
                    }}
                    placeholder={placeholderText}
                    autoCapitalize="off"
                    autoCorrect="off"
                    spellCheck={false}
                />
                <button
                    onClick={handleSend}
                    disabled={!ready || sending || !inputValue.trim()}
                    style={{
                        ...baseInputBtnStyle,
                        ...(inline
                            ? { color: t.sendBtnColor, borderColor: t.sendBtnBorder }
                            : { color: t.sendBtnColor, background: t.sendBtnBorder, borderColor: t.sendBtnBorder, borderRadius: "6px" }),
                        opacity: (!ready || sending || !inputValue.trim()) ? 0.5 : 1,
                        marginBottom: "4px",
                    }}
                    title={lang === "en" ? "Send" : "发送"}
                >
                    {sending ? "…" : "⏎"}
                </button>
            </div>
        </div>
    );
}
