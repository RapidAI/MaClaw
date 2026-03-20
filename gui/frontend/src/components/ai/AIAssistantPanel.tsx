import { useState, useRef, useCallback, useEffect, useMemo } from "react";
import { ShowItemInFolder } from "../../../wailsjs/go/main/App";
import type { ChatMessage } from "./useAIAssistant";

interface AIAssistantPanelProps {
    onClose: () => void;
    lang: string; // 'zh-Hans' | 'zh-Hant' | 'en'
    messages: ChatMessage[];
    sending: boolean;
    sendMessage: (text: string) => Promise<void>;
    clearHistory: () => Promise<void>;
    executeAction: (command: string) => Promise<void>;
    inline?: boolean; // when true, render as inline content instead of overlay
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

const darkTheme: Theme = {
    bg: "#0c0c0c",
    titleBarBg: "#1e1e1e",
    titleBarBorder: "#333",
    titleText: "#999",
    text: "#d4d4d4",
    textMuted: "#888",
    inputBarBg: "#1a1a1a",
    inputBarBorder: "#333",
    inputText: "#d4d4d4",
    codeBg: "#2a2a2a",
    codeText: "#ce9178",
    codeBlockBg: "#1a1a1a",
    codeBlockBorder: "#333",
    codeBlockLang: "#555",
    borderLeft: "#333",
    responseBorderLeft: "#333",
    headingColor: "#569cd6",
    linkColor: "#569cd6",
    pathColor: "#4ec9b0",
    promptColor: "#4ec9b0",
    userColor: "#4ec9b0",
    divider: "#333",
    fieldBg: "#1a1a1a",
    fieldBorder: "#333",
    fieldLabel: "#888",
    errorText: "#f44747",
    errorBg: "rgba(244, 71, 71, 0.08)",
    errorBorder: "#f44747",
    emptyHint: "#555",
    boldColor: "#e0e0e0",
    italicColor: "#c5c5c5",
    bulletColor: "#808080",
    quoteBorder: "#555",
    quoteText: "#9a9a9a",
    btnColor: "#569cd6",
    btnBorder: "#569cd6",
    actionBtnColor: "#888",
    closeBtnColor: "#ccc",
    sendBtnColor: "#569cd6",
    sendBtnBorder: "#569cd6",
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
    background: darkTheme.bg,
    textAlign: "left",
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
                    parts.push(<a key={idx++} href={href} target="_blank" rel="noopener noreferrer" style={{ color: t.linkColor, textDecoration: "underline" }}>{lm[1]}</a>);
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

function renderMessage(msg: ChatMessage, executeAction: (cmd: string) => void, t: Theme): React.ReactNode {
    switch (msg.role) {
        case "user":
            return (
                <div key={msg.id}>
                    <div style={{ borderTop: `1px solid ${t.divider}`, margin: "8px 0 4px 0" }} />
                    <div style={{ color: t.userColor, fontWeight: 600, padding: "3px 0", overflowWrap: "break-word" }}>
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

/* ── Main component ── */

export function AIAssistantPanel({ onClose, lang, messages, sending, sendMessage, clearHistory, executeAction, inline }: AIAssistantPanelProps) {
    const [inputValue, setInputValue] = useState("");
    const [composing, setComposing] = useState(false);
    const inputRef = useRef<HTMLInputElement | null>(null);
    const outputEndRef = useRef<HTMLDivElement | null>(null);
    const outputContainerRef = useRef<HTMLDivElement | null>(null);
    const userScrolledUpRef = useRef(false);
    const prevMsgCountRef = useRef(0);

    const t = inline ? lightTheme : darkTheme;

    const title = lang === "en" ? "AI Assistant" : "AI 助手";
    const thinkingText = lang === "en" ? "Thinking..." : "正在思考...";
    const placeholderText = sending
        ? (lang === "en" ? "Thinking..." : "正在思考...")
        : (lang === "en" ? "Type a message..." : "输入消息...");

    // Auto-scroll on new messages (unless user scrolled up)
    useEffect(() => {
        if (messages.length > prevMsgCountRef.current && !userScrolledUpRef.current) {
            outputEndRef.current?.scrollIntoView({ behavior: "smooth" });
        }
        prevMsgCountRef.current = messages.length;
    }, [messages.length]);

    // Track user scroll position
    const handleScroll = useCallback(() => {
        const container = outputContainerRef.current;
        if (!container) return;
        const threshold = 80;
        userScrolledUpRef.current =
            container.scrollHeight - container.scrollTop - container.clientHeight > threshold;
    }, []);

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
        userScrolledUpRef.current = false;
        await sendMessage(text);
    }, [inputValue, sending, sendMessage]);

    // Memoize rendered messages
    const renderedMessages = useMemo(
        () => messages.map((msg) => renderMessage(msg, executeAction, t)),
        [messages, executeAction, t],
    );

    const containerStyle: React.CSSProperties = inline
        ? { display: "flex", flexDirection: "column", background: t.bg, textAlign: "left", width: "100%", height: "100%" }
        : overlayStyle;

    return (
        <div style={containerStyle}>
            {/* ── Title bar ── */}
            <div style={{
                display: "flex", alignItems: "center", justifyContent: "space-between",
                padding: "0 10px", height: "36px",
                background: t.titleBarBg, borderBottom: `1px solid ${t.titleBarBorder}`,
                flexShrink: 0, gap: "6px",
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
                <div style={{ display: "flex", gap: "4px", flexShrink: 0 }}>
                    <button
                        onClick={clearHistory}
                        style={{ ...baseActionBtnStyle, color: t.actionBtnColor }}
                        title={lang === "en" ? "Clear history" : "清空历史"}
                    >
                        🗑️
                    </button>
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
                {messages.length === 0 ? (
                    <span style={{ color: t.emptyHint }}>
                        {lang === "en" ? "Ask me anything..." : "有什么可以帮你的？"}
                    </span>
                ) : (
                    renderedMessages
                )}
                {sending && (
                    <div style={{ color: t.textMuted, fontSize: "11px", padding: "4px 0", fontStyle: "italic" }}>
                        {thinkingText}
                    </div>
                )}
                <div ref={outputEndRef} />
            </div>

            {/* ── Input bar ── */}
            <div style={{
                display: "flex", alignItems: "center", gap: "6px",
                padding: "6px 10px", paddingBottom: "max(6px, env(safe-area-inset-bottom))",
                background: t.inputBarBg, borderTop: `1px solid ${t.inputBarBorder}`,
                flexShrink: 0,
            }}>
                <span style={{
                    color: t.promptColor, fontFamily: "Consolas, monospace",
                    fontSize: "13px", flexShrink: 0, userSelect: "none",
                }}>❯</span>
                <input
                    ref={inputRef}
                    type="text"
                    style={{
                        flex: 1, minWidth: 0, background: "transparent",
                        border: "none", outline: "none", color: t.inputText,
                        fontFamily: "Consolas, 'Courier New', monospace",
                        fontSize: "14px", padding: "8px 0",
                    }}
                    value={inputValue}
                    onChange={(e) => setInputValue(e.target.value)}
                    onCompositionStart={() => setComposing(true)}
                    onCompositionEnd={() => setComposing(false)}
                    onKeyDown={(e) => {
                        if (e.key === "Enter" && !composing) {
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
                    disabled={sending || !inputValue.trim()}
                    style={{ ...baseInputBtnStyle, color: t.sendBtnColor, borderColor: t.sendBtnBorder }}
                    title={lang === "en" ? "Send" : "发送"}
                >
                    {sending ? "…" : "⏎"}
                </button>
            </div>
        </div>
    );
}
