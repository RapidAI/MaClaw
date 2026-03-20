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

/* ── Style constants (matching RemoteSessionConsole) ── */

const overlayStyle: React.CSSProperties = {
    position: "fixed",
    inset: 0,
    zIndex: 10000,
    display: "flex",
    flexDirection: "column",
    background: "#0c0c0c",
    textAlign: "left",
};

const inlineContainerStyle: React.CSSProperties = {
    display: "flex",
    flexDirection: "column",
    background: "#0c0c0c",
    textAlign: "left",
    width: "100%",
    height: "100%",
};

const titleBarStyle: React.CSSProperties = {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    padding: "0 10px",
    height: "36px",
    background: "#1e1e1e",
    borderBottom: "1px solid #333",
    flexShrink: 0,
    gap: "6px",
};

const titleLeftStyle: React.CSSProperties = {
    display: "flex",
    alignItems: "center",
    gap: "8px",
    minWidth: 0,
    flex: 1,
};

const trafficLightsStyle: React.CSSProperties = {
    display: "flex",
    gap: "5px",
    flexShrink: 0,
};

const dotBase: React.CSSProperties = {
    width: 10,
    height: 10,
    borderRadius: "50%",
    display: "inline-block",
    cursor: "pointer",
};

const titleTextStyle: React.CSSProperties = {
    color: "#999",
    fontSize: "11px",
    fontFamily: "Consolas, 'SF Mono', monospace",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
};

const outputAreaStyle: React.CSSProperties = {
    flex: 1,
    minHeight: 0,
    maxHeight: "none",
    padding: "8px 10px",
    fontSize: "12px",
    lineHeight: 1.5,
    overflowY: "auto",
    overflowX: "hidden",
    textAlign: "left",
    color: "#d4d4d4",
    background: "#0c0c0c",
    fontFamily: "'Cascadia Code', 'Cascadia Mono', 'Consolas', 'Courier New', monospace",
    whiteSpace: "pre-wrap",
    wordBreak: "break-all",
};

const inputBarStyle: React.CSSProperties = {
    display: "flex",
    alignItems: "center",
    gap: "6px",
    padding: "6px 10px",
    paddingBottom: "max(6px, env(safe-area-inset-bottom))",
    background: "#1a1a1a",
    borderTop: "1px solid #333",
    flexShrink: 0,
};

const promptStyle: React.CSSProperties = {
    color: "#4ec9b0",
    fontFamily: "Consolas, monospace",
    fontSize: "13px",
    flexShrink: 0,
    userSelect: "none",
};

const inputStyle: React.CSSProperties = {
    flex: 1,
    minWidth: 0,
    background: "transparent",
    border: "none",
    outline: "none",
    color: "#d4d4d4",
    fontFamily: "Consolas, 'Courier New', monospace",
    fontSize: "14px",
    padding: "8px 0",
};

const inputBtnStyle: React.CSSProperties = {
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

const actionBtnStyle: React.CSSProperties = {
    background: "transparent",
    border: "none",
    color: "#888",
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

const responseBlockStyle: React.CSSProperties = {
    padding: "4px 0 4px 8px",
    borderLeft: "2px solid #333",
    margin: "2px 0",
    color: "#d4d4d4",
};

/* ── Inline markdown rendering (copied from RemoteSessionConsole) ── */

function renderInlineMarkdown(text: string): React.ReactNode[] {
    if (!text) return ["\u00A0"];
    const parts: React.ReactNode[] = [];
    // Match: inline code, bold, italic, markdown links, Windows paths (C:\...), Unix absolute paths (/home/... ~/...)
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
            parts.push(<code key={idx++} style={{ background: "#2a2a2a", color: "#ce9178", padding: "1px 4px", borderRadius: "3px", fontSize: "0.92em" }}>{m.slice(1, -1)}</code>);
        } else if (match[2]) {
            parts.push(<strong key={idx++} style={{ color: "#e0e0e0", fontWeight: 700 }}>{m.slice(2, -2)}</strong>);
        } else if (match[3]) {
            parts.push(<em key={idx++} style={{ color: "#c5c5c5" }}>{m.slice(1, -1)}</em>);
        } else if (match[4]) {
            const lm = m.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
            if (lm) {
                const href = lm[2];
                if (/^https?:\/\//i.test(href)) {
                    parts.push(<a key={idx++} href={href} target="_blank" rel="noopener noreferrer" style={{ color: "#569cd6", textDecoration: "underline" }}>{lm[1]}</a>);
                } else {
                    parts.push(<span key={idx++} style={{ color: "#569cd6" }}>{lm[1]}</span>);
                }
            } else {
                parts.push(m);
            }
        } else if (match[5] || match[6]) {
            // Local file path — render as clickable link
            const filePath = m;
            parts.push(
                <a key={idx++}
                   href="#"
                   onClick={(e) => { e.preventDefault(); ShowItemInFolder(filePath); }}
                   style={{ color: "#4ec9b0", textDecoration: "underline", cursor: "pointer" }}
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

function renderMarkdownLine(text: string, key: string | number): React.ReactNode {
    const trimmed = text.trimStart();

    const headingMatch = trimmed.match(/^(#{1,4})\s+(.+)$/);
    if (headingMatch) {
        const level = headingMatch[1].length;
        const sizes: Record<number, string> = { 1: "1.2em", 2: "1.1em", 3: "1.0em", 4: "0.95em" };
        return (
            <div key={key} style={{ fontSize: sizes[level] || "1em", fontWeight: 700, color: "#569cd6", margin: "0.4em 0 0.2em" }}>
                {renderInlineMarkdown(headingMatch[2])}
            </div>
        );
    }

    if (/^>\s/.test(trimmed)) {
        return (
            <div key={key} style={{ borderLeft: "2px solid #555", paddingLeft: "8px", color: "#9a9a9a", fontStyle: "italic", minHeight: "1.4em" }}>
                {renderInlineMarkdown(trimmed.slice(2))}
            </div>
        );
    }

    if (/^[-*]\s/.test(trimmed)) {
        return (
            <div key={key} style={{ paddingLeft: "1em", textIndent: "-0.7em", minHeight: "1.4em" }}>
                <span style={{ color: "#808080" }}>•</span>{" "}
                {renderInlineMarkdown(trimmed.slice(2))}
            </div>
        );
    }

    const numMatch = trimmed.match(/^(\d+)[.)]\s+(.+)$/);
    if (numMatch) {
        return (
            <div key={key} style={{ paddingLeft: "1.2em", textIndent: "-1.2em", minHeight: "1.4em" }}>
                <span style={{ color: "#808080" }}>{numMatch[1]}.</span>{" "}
                {renderInlineMarkdown(numMatch[2])}
            </div>
        );
    }

    return (
        <div key={key} style={{ minHeight: "1.4em" }}>
            {renderInlineMarkdown(text) || "\u00A0"}
        </div>
    );
}

/* ── Structured response rendering (Task 5.2) ── */

function renderContentWithCodeBlocks(content: string): React.ReactNode[] {
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
                    background: "#1a1a1a",
                    border: "1px solid #333",
                    borderRadius: "4px",
                    padding: "8px 10px",
                    margin: "4px 0",
                    fontSize: "0.9em",
                    overflowX: "auto",
                    color: "#ce9178",
                    lineHeight: 1.5,
                }}>
                    {codeBlockLang && <div style={{ color: "#555", fontSize: "0.85em", marginBottom: "4px" }}>{codeBlockLang}</div>}
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
            elements.push(renderMarkdownLine(line, `md-${lineIdx}`));
        }
        lineIdx++;
    }
    if (inCodeBlock) flushCodeBlock();
    return elements;
}

function renderFields(fields: Array<{ label: string; value: string }>): React.ReactNode {
    return (
        <div style={{ display: "flex", flexWrap: "wrap", gap: "6px", margin: "4px 0" }}>
            {fields.map((f, i) => (
                <div key={`field-${i}`} data-testid="field-card" style={{
                    background: "#1a1a1a",
                    border: "1px solid #333",
                    borderRadius: "4px",
                    padding: "4px 8px",
                    fontSize: "12px",
                }}>
                    <span style={{ color: "#888", marginRight: "6px" }}>{f.label}:</span>
                    <span style={{ color: "#d4d4d4" }}>{f.value}</span>
                </div>
            ))}
        </div>
    );
}

function renderActions(
    actions: Array<{ label: string; command: string; style: string }>,
    executeAction: (command: string) => void,
): React.ReactNode {
    return (
        <div style={{ display: "flex", flexWrap: "wrap", gap: "6px", margin: "4px 0" }}>
            {actions.map((a, i) => (
                <button
                    key={`action-${i}`}
                    data-testid="action-button"
                    onClick={() => executeAction(a.command)}
                    style={{
                        ...inputBtnStyle,
                        color: a.style === "danger" ? "#f44747" : "#569cd6",
                        borderColor: a.style === "danger" ? "#f44747" : "#569cd6",
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

function renderMessage(msg: ChatMessage, executeAction: (cmd: string) => void): React.ReactNode {
    switch (msg.role) {
        case "user":
            return (
                <div key={msg.id}>
                    <div style={{ borderTop: "1px solid #333", margin: "8px 0 4px 0" }} />
                    <div style={{ color: "#4ec9b0", fontWeight: 600, padding: "3px 0", overflowWrap: "break-word" }}>
                        ❯ {msg.content}
                    </div>
                </div>
            );
        case "assistant":
            return (
                <div key={msg.id} style={responseBlockStyle}>
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
                                        borderRadius: "4px", border: "1px solid #444",
                                        objectFit: "contain",
                                    }}
                                />
                            </a>
                        </div>
                    )}
                    {renderContentWithCodeBlocks(msg.content)}
                    {msg.localFilePaths && msg.localFilePaths.length > 0 && (
                        <div style={{ margin: "4px 0" }}>
                            {msg.localFilePaths.map((fp, i) => (
                                <div key={i} style={{ padding: "2px 0" }}>
                                    <a href="#"
                                       onClick={(e) => { e.preventDefault(); ShowItemInFolder(fp); }}
                                       style={{ color: "#4ec9b0", textDecoration: "underline", cursor: "pointer", wordBreak: "break-all" }}
                                       title={fp}>
                                        📄 文件已保存: 📁 {fp}
                                    </a>
                                </div>
                            ))}
                        </div>
                    )}
                    {msg.fields && msg.fields.length > 0 && renderFields(msg.fields)}
                    {msg.actions && msg.actions.length > 0 && renderActions(msg.actions, executeAction)}
                </div>
            );
        case "progress":
            return (
                <div key={msg.id} style={{ color: "#888", fontSize: "11px", padding: "1px 0", fontStyle: "italic" }}>
                    {msg.content}
                </div>
            );
        case "error":
            return (
                <div key={msg.id} style={{
                    color: "#f44747",
                    background: "rgba(244, 71, 71, 0.08)",
                    borderLeft: "2px solid #f44747",
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

    // Escape key closes panel
    useEffect(() => {
        const handler = (e: KeyboardEvent) => {
            if (e.key === "Escape") onClose();
        };
        window.addEventListener("keydown", handler);
        return () => window.removeEventListener("keydown", handler);
    }, [onClose]);

    const handleSend = useCallback(async () => {
        const text = inputValue.trim();
        if (!text || sending) return;
        setInputValue("");
        userScrolledUpRef.current = false;
        await sendMessage(text);
    }, [inputValue, sending, sendMessage]);

    // Memoize rendered messages
    const renderedMessages = useMemo(
        () => messages.map((msg) => renderMessage(msg, executeAction)),
        [messages, executeAction],
    );

    return (
        <div style={inline ? inlineContainerStyle : overlayStyle}>
            {/* ── Title bar ── */}
            <div style={titleBarStyle}>
                <div style={titleLeftStyle}>
                    <div style={trafficLightsStyle}>
                        <span
                            style={{ ...dotBase, background: "#ff5f57" }}
                            onClick={onClose}
                            title={lang === "en" ? "Close" : "关闭"}
                        />
                    </div>
                    <span style={titleTextStyle}>{title}</span>
                </div>
                <div style={{ display: "flex", gap: "4px", flexShrink: 0 }}>
                    <button
                        onClick={clearHistory}
                        style={{ ...actionBtnStyle, color: "#888" }}
                        title={lang === "en" ? "Clear history" : "清空历史"}
                    >
                        🗑️
                    </button>
                    <button
                        onClick={onClose}
                        style={{ ...actionBtnStyle, color: "#ccc", fontSize: "14px", padding: "0 8px" }}
                        title={lang === "en" ? "Close" : "关闭"}
                    >
                        ✕
                    </button>
                </div>
            </div>

            {/* ── Chat area ── */}
            <div
                ref={outputContainerRef}
                style={outputAreaStyle}
                onScroll={handleScroll}
            >
                {messages.length === 0 ? (
                    <span style={{ color: "#555" }}>
                        {lang === "en" ? "Ask me anything..." : "有什么可以帮你的？"}
                    </span>
                ) : (
                    renderedMessages
                )}
                {sending && (
                    <div style={{ color: "#888", fontSize: "11px", padding: "4px 0", fontStyle: "italic" }}>
                        {thinkingText}
                    </div>
                )}
                <div ref={outputEndRef} />
            </div>

            {/* ── Input bar ── */}
            <div style={inputBarStyle}>
                <span style={promptStyle}>❯</span>
                <input
                    ref={inputRef}
                    type="text"
                    style={inputStyle}
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
                    style={{ ...inputBtnStyle, color: "#569cd6", borderColor: "#569cd6" }}
                    title={lang === "en" ? "Send" : "发送"}
                >
                    {sending ? "…" : "⏎"}
                </button>
            </div>
        </div>
    );
}
