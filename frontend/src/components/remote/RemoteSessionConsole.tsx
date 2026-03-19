import { useState, useRef, useCallback, useEffect, useMemo, type Dispatch, type SetStateAction } from "react";
import type { RemoteSessionView } from "./types";
import { SendRemoteSessionInput, SendRemoteSessionRawInput, SendRemoteSessionImage, CaptureRemoteScreenshot, CaptureRemoteWindowScreenshot, InterruptRemoteSession } from "../../../wailsjs/go/main/App";

type Props = {
    session: RemoteSessionView;
    remoteInputDrafts: Record<string, string>;
    setRemoteInputDrafts: Dispatch<SetStateAction<Record<string, string>>>;
    killRemoteSession: (sessionID: string) => Promise<void>;
    refreshSessionsOnly: () => Promise<void>;
    onClose: () => void;
    readOnly?: boolean;
};

const TERMINAL_STATUSES = new Set([
    "stopped", "finished", "failed", "killed", "exited",
    "closed", "done", "completed", "terminated", "error",
]);

/* ── Static styles extracted to avoid re-creation per render ── */

const overlayStyle: React.CSSProperties = {
    position: "fixed",
    inset: 0,
    zIndex: 10000,
    display: "flex",
    flexDirection: "column",
    background: "#0c0c0c",
    textAlign: "left",
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
};

const titleTextStyle: React.CSSProperties = {
    color: "#999",
    fontSize: "11px",
    fontFamily: "Consolas, 'SF Mono', monospace",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
};

const titleRightStyle: React.CSSProperties = {
    display: "flex",
    gap: "4px",
    flexShrink: 0,
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

/** Duration (ms) before the send-status feedback auto-clears. */
const SEND_INFO_TIMEOUT = 3000;

const SUPPORTED_IMAGE_TYPES = ["image/png", "image/jpeg", "image/gif", "image/webp"];
const MAX_IMAGE_SIZE = 5 * 1024 * 1024; // 5MB

// ── ANSI stripping (module-level for reuse) ──
const _ansiRe = /\x1b(?:\[[0-9;?]*[a-zA-Z]|\][^\x07\x1b]*(?:\x07|\x1b\\)?|[()][A-Z0-9]|[a-zA-Z])/g;
function stripAnsi(s: string): string { return s.replace(_ansiRe, ""); }

// ── Lightweight inline markdown → React elements ──
// Handles: **bold**, *italic*, `code`, [link](url)
// Also detects line-level patterns: ### heading, - list, > blockquote, ``` code block
function renderMarkdownLine(text: string, key: string | number): React.ReactNode {
    const trimmed = text.trimStart();

    // Heading: ### text
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

    // Blockquote: > text
    if (/^>\s/.test(trimmed)) {
        return (
            <div key={key} style={{ borderLeft: "2px solid #555", paddingLeft: "8px", color: "#9a9a9a", fontStyle: "italic", minHeight: "1.4em" }}>
                {renderInlineMarkdown(trimmed.slice(2))}
            </div>
        );
    }

    // List item: - text or * text
    if (/^[-*]\s/.test(trimmed)) {
        return (
            <div key={key} style={{ paddingLeft: "1em", textIndent: "-0.7em", minHeight: "1.4em" }}>
                <span style={{ color: "#808080" }}>•</span>{" "}
                {renderInlineMarkdown(trimmed.slice(2))}
            </div>
        );
    }

    // Numbered list: 1. text
    const numMatch = trimmed.match(/^(\d+)[.)]\s+(.+)$/);
    if (numMatch) {
        return (
            <div key={key} style={{ paddingLeft: "1.2em", textIndent: "-1.2em", minHeight: "1.4em" }}>
                <span style={{ color: "#808080" }}>{numMatch[1]}.</span>{" "}
                {renderInlineMarkdown(numMatch[2])}
            </div>
        );
    }

    // Regular line with inline markdown
    return (
        <div key={key} style={{ minHeight: "1.4em" }}>
            {renderInlineMarkdown(text) || "\u00A0"}
        </div>
    );
}

// Parse inline markdown: **bold**, *italic*, `code`, [text](url)
function renderInlineMarkdown(text: string): React.ReactNode[] {
    if (!text) return ["\u00A0"];
    const parts: React.ReactNode[] = [];
    // Regex: code, bold, italic, link — order matters
    const re = /(`[^`]+`)|(\*\*[^*]+\*\*)|(\*[^\s*][^*]*?\*)|(\[[^\]]+\]\([^)]+\))/g;
    let lastIndex = 0;
    let match: RegExpExecArray | null;
    let idx = 0;
    while ((match = re.exec(text)) !== null) {
        if (match.index > lastIndex) {
            parts.push(text.slice(lastIndex, match.index));
        }
        const m = match[0];
        if (match[1]) {
            // `code`
            parts.push(<code key={idx++} style={{ background: "#2a2a2a", color: "#ce9178", padding: "1px 4px", borderRadius: "3px", fontSize: "0.92em" }}>{m.slice(1, -1)}</code>);
        } else if (match[2]) {
            // **bold**
            parts.push(<strong key={idx++} style={{ color: "#e0e0e0", fontWeight: 700 }}>{m.slice(2, -2)}</strong>);
        } else if (match[3]) {
            // *italic*
            parts.push(<em key={idx++} style={{ color: "#c5c5c5" }}>{m.slice(1, -1)}</em>);
        } else if (match[4]) {
            // [text](url)
            const lm = m.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
            if (lm) {
                const href = lm[2];
                // Only allow http/https links to prevent javascript: XSS
                if (/^https?:\/\//i.test(href)) {
                    parts.push(<a key={idx++} href={href} target="_blank" rel="noopener noreferrer" style={{ color: "#569cd6", textDecoration: "underline" }}>{lm[1]}</a>);
                } else {
                    parts.push(<span key={idx++} style={{ color: "#569cd6" }}>{lm[1]}</span>);
                }
            } else {
                parts.push(m);
            }
        }
        lastIndex = match.index + m.length;
    }
    if (lastIndex < text.length) {
        parts.push(text.slice(lastIndex));
    }
    return parts.length > 0 ? parts : ["\u00A0"];
}

// ── Static Q&A styles ──
const userDividerStyle: React.CSSProperties = {
    borderTop: "1px solid #333",
    margin: "8px 0 4px 0",
};
const promptStyleQA: React.CSSProperties = {
    color: "#4ec9b0", fontWeight: 600, padding: "3px 0",
    overflowWrap: "break-word",
};
const responseBlockStyle: React.CSSProperties = {
    padding: "4px 0 4px 8px",
    borderLeft: "2px solid #333",
    margin: "2px 0",
    color: "#d4d4d4",
};

export function RemoteSessionConsole(props: Props) {
    const {
        session,
        remoteInputDrafts,
        setRemoteInputDrafts,
        killRemoteSession,
        refreshSessionsOnly,
        onClose,
        readOnly = false,
    } = props;

    const [sending, setSending] = useState(false);
    const [lastSendInfo, setLastSendInfo] = useState("");
    const [imageUploading, setImageUploading] = useState(false);
    const inputRef = useRef<HTMLInputElement | null>(null);
    const fileInputRef = useRef<HTMLInputElement | null>(null);
    const outputEndRef = useRef<HTMLDivElement | null>(null);
    const outputContainerRef = useRef<HTMLDivElement | null>(null);
    const [composing, setComposing] = useState(false);
    const inputValueRef = useRef("");
    const prevRawCountRef = useRef(0);
    const prevLastLineRef = useRef("");
    const sendInfoTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const [clearOffset, setClearOffset] = useState(0);

    // Local line cache: accumulate lines so content never disappears even if
    // the backend returns fewer lines on a subsequent refresh (e.g. due to
    // screen-refresh detection in PTY mode replacing the buffer).
    const lineCacheRef = useRef<string[]>([]);
    const sessionIdRef = useRef(session.id);

    const status = (session.summary?.status || session.status || "unknown").toLowerCase();
    const sessionClosed = TERMINAL_STATUSES.has(status);
    const disabled = sessionClosed || sending;
    const isSDK = session.execution_mode === "sdk";

    // Merge incoming lines with the local cache — content only grows.
    const incomingLines = session.raw_output_lines || session.preview?.preview_lines || [];
    if (sessionIdRef.current !== session.id) {
        // Different session — reset
        sessionIdRef.current = session.id;
        lineCacheRef.current = incomingLines.slice();
    } else if (incomingLines.length > lineCacheRef.current.length) {
        // Backend has more lines — take the full snapshot
        lineCacheRef.current = incomingLines.slice();
    } else if (incomingLines.length === lineCacheRef.current.length && incomingLines.length > 0) {
        // Same count — update the last line (streaming accumulator may have changed)
        lineCacheRef.current[lineCacheRef.current.length - 1] =
            incomingLines[incomingLines.length - 1];
    }
    // When incomingLines.length < cache length, keep the cache as-is.
    // This is the key fix: prevents content from disappearing.

    const rawLines = lineCacheRef.current.slice(clearOffset);

    // Helper: set status feedback with auto-clear
    const showSendInfo = useCallback((msg: string) => {
        setLastSendInfo(msg);
        if (sendInfoTimerRef.current) clearTimeout(sendInfoTimerRef.current);
        sendInfoTimerRef.current = setTimeout(() => setLastSendInfo(""), SEND_INFO_TIMEOUT);
    }, []);

    // Cleanup send-info timer on unmount
    useEffect(() => {
        return () => {
            if (sendInfoTimerRef.current) clearTimeout(sendInfoTimerRef.current);
        };
    }, []);

    // Auto-scroll to bottom when new output arrives, but only if the user
    // hasn't scrolled up to read history.
    useEffect(() => {
        const lastLine = rawLines.length > 0 ? rawLines[rawLines.length - 1] : "";
        if (rawLines.length !== prevRawCountRef.current || lastLine !== prevLastLineRef.current) {
            prevRawCountRef.current = rawLines.length;
            prevLastLineRef.current = lastLine;
            const container = outputContainerRef.current;
            // Auto-scroll when user is near the bottom (within ~2 lines)
            const threshold = 80;
            if (!container || container.scrollHeight - container.scrollTop - container.clientHeight < threshold) {
                outputEndRef.current?.scrollIntoView({ behavior: "smooth" });
            }
        }
    }, [rawLines]);

    // Focus input on open (with cleanup)
    useEffect(() => {
        const timer = setTimeout(() => inputRef.current?.focus(), 100);
        return () => clearTimeout(timer);
    }, []);

    // Close on Escape key
    useEffect(() => {
        const handler = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
        window.addEventListener("keydown", handler);
        return () => window.removeEventListener("keydown", handler);
    }, [onClose]);

    const handleSend = useCallback(async () => {
        const text = inputValueRef.current.trim();
        if (!text || sending) return;
        setSending(true);
        setLastSendInfo("");
        try {
            await SendRemoteSessionInput(session.id, text + "\n");
            showSendInfo(`✓ "${text}"`);
            inputValueRef.current = "";
            setRemoteInputDrafts((prev) => ({ ...prev, [session.id]: "" }));
            setTimeout(() => refreshSessionsOnly(), 200);
            setTimeout(() => refreshSessionsOnly(), 800);
            setTimeout(() => refreshSessionsOnly(), 2000);
            setTimeout(() => refreshSessionsOnly(), 5000);
        } catch (e) {
            showSendInfo(`✗ ${String(e)}`);
        }
        setSending(false);
    }, [session.id, sending, setRemoteInputDrafts, refreshSessionsOnly, showSendInfo]);

    const handleSendRaw = useCallback(async () => {
        const text = inputValueRef.current;
        if (!text || sending) return;
        setSending(true);
        try {
            for (let i = 0; i < text.length; i++) {
                await SendRemoteSessionRawInput(session.id, text[i]);
                if (i < text.length - 1) await new Promise((r) => setTimeout(r, 30));
            }
            await SendRemoteSessionRawInput(session.id, "\r");
            showSendInfo(`✓ raw: "${text}"`);
            inputValueRef.current = "";
            setRemoteInputDrafts((prev) => ({ ...prev, [session.id]: "" }));
            setTimeout(() => refreshSessionsOnly(), 200);
            setTimeout(() => refreshSessionsOnly(), 800);
            setTimeout(() => refreshSessionsOnly(), 2000);
        } catch (e) {
            showSendInfo(`✗ raw: ${String(e)}`);
        }
        setSending(false);
    }, [session.id, sending, setRemoteInputDrafts, refreshSessionsOnly, showSendInfo]);

    const handleInputChange = (value: string) => {
        inputValueRef.current = value;
        setRemoteInputDrafts((prev) => ({ ...prev, [session.id]: value }));
    };

    const handleCtrlC = useCallback(async () => {
        try {
            if (isSDK) {
                await InterruptRemoteSession(session.id);
                showSendInfo("✓ Interrupt (SDK)");
            } else {
                await SendRemoteSessionRawInput(session.id, "\x03");
                showSendInfo("✓ Ctrl+C");
            }
            setTimeout(() => refreshSessionsOnly(), 300);
        } catch (e) {
            showSendInfo(`✗ Ctrl+C: ${String(e)}`);
        }
    }, [session.id, isSDK, refreshSessionsOnly, showSendInfo]);

    const handleEsc = useCallback(async () => {
        try {
            await SendRemoteSessionRawInput(session.id, "\x1b");
            showSendInfo("✓ Esc");
            setTimeout(() => refreshSessionsOnly(), 300);
        } catch (e) {
            showSendInfo(`✗ Esc: ${String(e)}`);
        }
    }, [session.id, refreshSessionsOnly, showSendInfo]);

    const handleKill = useCallback(async () => {
        try { await killRemoteSession(session.id); onClose(); } catch { /* already stopped */ }
    }, [session.id, killRemoteSession, onClose]);

    const handleClear = useCallback(() => {
        setClearOffset(lineCacheRef.current.length);
    }, []);

    const handleImageFile = useCallback(async (file: File) => {
        if (!SUPPORTED_IMAGE_TYPES.includes(file.type)) {
            showSendInfo("✗ 不支持的图片格式，仅支持 PNG/JPEG/GIF/WebP");
            return;
        }
        if (file.size > MAX_IMAGE_SIZE) {
            showSendInfo("✗ 图片超过 5MB 限制");
            return;
        }
        setImageUploading(true);
        try {
            const base64 = await new Promise<string>((resolve, reject) => {
                const reader = new FileReader();
                reader.onload = () => {
                    const result = reader.result as string;
                    // Strip data URL prefix: "data:image/png;base64,..."
                    const idx = result.indexOf(",");
                    resolve(idx >= 0 ? result.slice(idx + 1) : result);
                };
                reader.onerror = () => reject(new Error("读取文件失败"));
                reader.readAsDataURL(file);
            });
            await SendRemoteSessionImage(session.id, file.type, base64);
            showSendInfo(`✓ 📷 图片已发送 (${(file.size / 1024).toFixed(0)}KB)`);
            setTimeout(() => refreshSessionsOnly(), 200);
            setTimeout(() => refreshSessionsOnly(), 800);
            setTimeout(() => refreshSessionsOnly(), 2000);
        } catch (e) {
            showSendInfo(`✗ 图片发送失败: ${String(e)}`);
        }
        setImageUploading(false);
    }, [session.id, refreshSessionsOnly, showSendInfo]);

    const handleFileInputChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
        const file = e.target.files?.[0];
        if (file) handleImageFile(file);
        // Reset so the same file can be selected again
        e.target.value = "";
    }, [handleImageFile]);

    const handlePaste = useCallback((e: ClipboardEvent) => {
        if (!isSDK || sessionClosed) return;
        const items = e.clipboardData?.items;
        if (!items) return;
        for (let i = 0; i < items.length; i++) {
            if (items[i].type.startsWith("image/")) {
                e.preventDefault();
                const file = items[i].getAsFile();
                if (file) handleImageFile(file);
                return;
            }
        }
    }, [isSDK, sessionClosed, handleImageFile]);

    // Listen for paste events on the overlay
    useEffect(() => {
        const handler = (e: Event) => handlePaste(e as ClipboardEvent);
        window.addEventListener("paste", handler);
        return () => window.removeEventListener("paste", handler);
    }, [handlePaste]);

    const handleScreenshot = useCallback(async () => {
        const title = prompt("输入窗口标题（留空截全屏）：");
        if (title === null) return; // cancelled
        setImageUploading(true);
        try {
            if (title.trim()) {
                await CaptureRemoteWindowScreenshot(session.id, title.trim());
                showSendInfo(`✓ 📸 窗口截图已发送: ${title.trim()}`);
            } else {
                await CaptureRemoteScreenshot(session.id);
                showSendInfo("✓ 📸 全屏截图已发送");
            }
            setTimeout(() => refreshSessionsOnly(), 500);
        } catch (e) {
            showSendInfo(`✗ 截图失败: ${String(e)}`);
        }
        setImageUploading(false);
    }, [session.id, refreshSessionsOnly, showSendInfo]);

    const statusColor = status === "running" || status === "busy" ? "#4ec9b0"
        : status === "waiting_input" ? "#dcdcaa" : "#808080";

    // ── Build output elements ──
    const outputElements = useMemo((): React.ReactNode[] => {
        // Build a map: rawLineIndex → images to show after that line.
        // Images use after_line_idx relative to the full RawOutputLines;
        // adjust for clearOffset so they align with the visible rawLines.
        const imagesByLine = new Map<number, typeof session.output_images>();
        if (session.output_images) {
            for (const img of session.output_images) {
                const adjusted = img.after_line_idx - clearOffset;
                if (adjusted < 0) continue; // image is before the visible area
                const list = imagesByLine.get(adjusted) || [];
                list.push(img);
                imagesByLine.set(adjusted, list);
            }
        }

        // Pre-process: merge lines that are SDK streaming fragments into the previous line.
        // 1. Lines starting with "/" that look like word fragments (e.g. "/ON/AC")
        // 2. Windows path continuations (e.g. "D:\" followed by "workprj\test")
        const merged: string[] = [];
        const mergedToRawIdx: number[] = []; // maps merged index → last raw line index consumed
        for (let i = 0; i < rawLines.length; i++) {
            const cleaned = stripAnsi(rawLines[i]);
            const prev = merged.length > 0 ? merged[merged.length - 1] : "";
            if (
                prev.length > 0 &&
                cleaned.length > 0 &&
                cleaned[0] === "/" &&
                cleaned.length <= 30 &&
                !/^\/[a-z][\w.\-]*\//.test(cleaned)
            ) {
                merged[merged.length - 1] += cleaned;
                mergedToRawIdx[merged.length - 1] = i;
            } else if (
                /[A-Z]:\\$/.test(prev) &&
                cleaned.length > 0 &&
                /^[\w.\-\\]/.test(cleaned)
            ) {
                merged[merged.length - 1] += cleaned;
                mergedToRawIdx[merged.length - 1] = i;
            } else {
                merged.push(cleaned);
                mergedToRawIdx.push(i);
            }
        }

        const elements: React.ReactNode[] = [];
        let responseLines: React.ReactNode[] = [];
        let inCodeBlock = false;
        let codeBlockLines: string[] = [];
        let codeBlockLang = "";
        let plainTextBuf: string[] = [];

        // Helper: render pending images for a given merged line index
        const renderImagesAfterMergedIdx = (mergedIdx: number) => {
            const rawIdx = mergedToRawIdx[mergedIdx];
            if (rawIdx === undefined) return;
            const imgs = imagesByLine.get(rawIdx);
            if (!imgs || imgs.length === 0) return;
            for (const img of imgs) {
                responseLines.push(
                    <div key={`img-${img.image_id}`} style={{ margin: "8px 0", textAlign: "left" }}>
                        <div style={{ color: "#666", fontSize: "10px", marginBottom: "4px" }}>
                            🖼 {img.media_type}
                        </div>
                        <img
                            src={`data:${img.media_type};base64,${img.data}`}
                            alt="session output"
                            style={{
                                maxWidth: "100%",
                                maxHeight: "400px",
                                borderRadius: "4px",
                                border: "1px solid #333",
                                display: "block",
                            }}
                        />
                    </div>
                );
            }
        };

        // Smart join: no space between CJK characters to avoid extra spaces in Chinese/Japanese text
        const smartJoinPlain = (arr: string[]): string => {
            if (arr.length === 0) return "";
            let out = arr[0];
            for (let j = 1; j < arr.length; j++) {
                const prev = out, cur = arr[j];
                if (prev.length === 0 || cur.length === 0) { out += cur; continue; }
                const lastCh = prev.charCodeAt(prev.length - 1);
                const firstCh = cur.charCodeAt(0);
                const lastCJK = (lastCh >= 0x2E80 && lastCh <= 0x9FFF) || (lastCh >= 0xF900 && lastCh <= 0xFAFF) || (lastCh >= 0xFF00 && lastCh <= 0xFFEF);
                const firstCJK = (firstCh >= 0x2E80 && firstCh <= 0x9FFF) || (firstCh >= 0xF900 && firstCh <= 0xFAFF) || (firstCh >= 0xFF00 && firstCh <= 0xFFEF);
                if (lastCJK || firstCJK) { out += cur; } else { out += " " + cur; }
            }
            return out;
        };

        const flushCodeBlock = () => {
            if (codeBlockLines.length > 0) {
                responseLines.push(
                    <pre key={`code-${responseLines.length}`} style={{
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

        const flushResponse = () => {
            if (responseLines.length > 0) {
                elements.push(
                    <div key={`resp-${elements.length}`} style={responseBlockStyle}>
                        {responseLines}
                    </div>
                );
                responseLines = [];
            }
        };

        for (let i = 0; i < merged.length; i++) {
            const line = merged[i];
            const isUserInput = line.startsWith("❯ ");

            if (isUserInput) {
                if (inCodeBlock) { flushCodeBlock(); inCodeBlock = false; }
                flushResponse();
                if (elements.length > 0) {
                    elements.push(<div key={`div-${i}`} style={userDividerStyle} />);
                }
                elements.push(
                    <div key={`line-${i}`} style={promptStyleQA}>
                        {line}
                    </div>
                );
            } else {
                // Handle code fences
                if (/^```/.test(line.trimStart())) {
                    if (inCodeBlock) {
                        flushCodeBlock();
                        inCodeBlock = false;
                    } else {
                        inCodeBlock = true;
                        codeBlockLang = line.trimStart().slice(3).trim();
                    }
                    continue;
                }
                if (inCodeBlock) {
                    codeBlockLines.push(line);
                } else {
                    // Check if this is a "special" line (heading, list, blockquote, etc.)
                    const trimmed = line.trimStart();
                    const isSpecialLine = /^(#{1,4}\s|>\s|[-*]\s|\d+[.)]\s|⚡|✓|✅|✗|⚠|❌|[A-Z]:\\|~\/|\/[a-z][\w.\-]*\/)/.test(trimmed);
                    if (isSpecialLine) {
                        // Flush any accumulated plain text first
                        if (plainTextBuf.length > 0) {
                            responseLines.push(
                                <div key={`plain-${i}`} style={{ minHeight: "1.4em" }}>
                                    {renderInlineMarkdown(smartJoinPlain(plainTextBuf))}
                                </div>
                            );
                            plainTextBuf = [];
                        }
                        // List items: buffer for continuation merging (SDK streaming
                        // may split "- Image analysis" into "- Image" + "analysis")
                        const listMatch = trimmed.match(/^[-*]\s(.*)$/);
                        const numMatch = !listMatch ? trimmed.match(/^(\d+)[.)]\s+(.+)$/) : null;
                        if (listMatch || numMatch) {
                            // Look ahead: merge subsequent plain-text continuation lines
                            let itemText = listMatch ? listMatch[1] : numMatch![2];
                            while (i + 1 < merged.length) {
                                const nextLine = merged[i + 1];
                                const nextTrimmed = nextLine.trimStart();
                                if (nextTrimmed === "" || /^(#{1,4}\s|>\s|[-*]\s|\d+[.)]\s|⚡|✓|✅|✗|⚠|❌|[A-Z]:\\|~\/|\/[a-z][\w.\-]*\/|```)/.test(nextTrimmed) || nextLine.startsWith("❯ ")) break;
                                itemText += " " + nextTrimmed;
                                i++;
                            }
                            if (listMatch) {
                                responseLines.push(renderMarkdownLine("- " + itemText, `line-${i}`));
                            } else {
                                responseLines.push(renderMarkdownLine(numMatch![1] + ". " + itemText, `line-${i}`));
                            }
                        } else {
                            responseLines.push(renderMarkdownLine(line, `line-${i}`));
                        }
                    } else if (trimmed === "") {
                        // Empty line — flush plain buffer as paragraph break
                        if (plainTextBuf.length > 0) {
                            responseLines.push(
                                <div key={`plain-${i}`} style={{ minHeight: "1.4em" }}>
                                    {renderInlineMarkdown(smartJoinPlain(plainTextBuf))}
                                </div>
                            );
                            plainTextBuf = [];
                        }
                        responseLines.push(<div key={`empty-${i}`} style={{ height: "0.5em" }} />);
                    } else {
                        // Regular text — accumulate for natural word-wrap
                        plainTextBuf.push(trimmed);
                    }
                }
            }
            // After processing each merged line, render any images anchored here
            renderImagesAfterMergedIdx(i);
        }
        // Flush remaining plain text
        if (plainTextBuf.length > 0) {
            responseLines.push(
                <div key="plain-final" style={{ minHeight: "1.4em" }}>
                    {renderInlineMarkdown(smartJoinPlain(plainTextBuf))}
                </div>
            );
            plainTextBuf = [];
        }
        if (inCodeBlock) { flushCodeBlock(); }
        flushResponse();

        return elements;
    }, [rawLines, session.output_images, clearOffset]);

    return (
        <div style={overlayStyle}>
            {/* ── Title bar ── */}
            <div style={titleBarStyle}>
                <div style={titleLeftStyle}>
                    <div style={trafficLightsStyle}>
                        <span style={{ ...dotBase, background: "#ff5f57" }} />
                        <span style={{ ...dotBase, background: "#febc2e" }} />
                        <span style={{ ...dotBase, background: "#28c840" }} />
                    </div>
                    <span style={{ color: statusColor, fontSize: "9px", flexShrink: 0 }}>●</span>
                    <span style={titleTextStyle}>
                        {session.tool || "session"}{isSDK ? " (SDK)" : ""} · {status}
                    </span>
                </div>

                <div style={titleRightStyle}>
                    <button onClick={handleClear}
                        style={{ ...actionBtnStyle, color: "#569cd6" }}
                        title="清屏">
                        ⌧
                    </button>
                    {!readOnly && !isSDK && (
                        <button onClick={handleEsc} disabled={sessionClosed}
                            style={actionBtnStyle} title="Escape">
                            Esc
                        </button>
                    )}
                    {!readOnly && (
                        <button onClick={handleCtrlC} disabled={sessionClosed}
                            style={{ ...actionBtnStyle, color: "#e8a838" }}
                            title={isSDK ? "中断" : "Ctrl+C"}>
                            {isSDK ? "⏸" : "⌃C"}
                        </button>
                    )}
                    {!readOnly && (
                        <button onClick={handleKill} disabled={sessionClosed}
                            style={{ ...actionBtnStyle, color: "#f44747" }}
                            title="终止">
                            Kill
                        </button>
                    )}
                    <button onClick={onClose}
                        style={{ ...actionBtnStyle, color: "#ccc", fontSize: "14px", padding: "0 8px" }}
                        title="关闭">
                        ✕
                    </button>
                </div>
            </div>

            {/* ── Output area ── */}
            <div
                ref={outputContainerRef}
                className="terminal-output"
                style={outputAreaStyle}
            >
                {rawLines.length === 0 ? (
                    <span style={{ color: "#555" }}>$ _</span>
                ) : (
                    outputElements
                )}
                <div ref={outputEndRef} />
            </div>

            {/* ── Status feedback ── */}
            {lastSendInfo && (
                <div style={{
                    padding: "2px 10px",
                    background: lastSendInfo.startsWith("✓") ? "#0d1f0d" : "#2a0f0f",
                    color: lastSendInfo.startsWith("✓") ? "#89d185" : "#f48771",
                    fontSize: "11px",
                    fontFamily: "Consolas, monospace",
                    flexShrink: 0,
                }}>
                    {lastSendInfo}
                </div>
            )}

            {/* ── Input bar ── */}
            {!readOnly && (
            <div style={inputBarStyle}>
                <span style={promptStyle}>❯</span>
                <input
                    ref={inputRef}
                    type="text"
                    style={inputStyle}
                    value={remoteInputDrafts[session.id] || ""}
                    onChange={(e) => handleInputChange(e.target.value)}
                    onCompositionStart={() => setComposing(true)}
                    onCompositionEnd={() => setComposing(false)}
                    onKeyDown={(e) => {
                        if (e.key === "Enter" && !composing) {
                            e.preventDefault();
                            handleSend();
                        }
                    }}
                    placeholder={disabled ? (sessionClosed ? "会话已结束" : "发送中...") : (isSDK ? "输入消息..." : "输入命令...")}
                    disabled={disabled}
                    autoCapitalize="off"
                    autoCorrect="off"
                    spellCheck={false}
                />
                {!isSDK && (
                    <button onClick={handleSendRaw} disabled={disabled}
                        style={{ ...inputBtnStyle, color: "#6a9955", borderColor: "#6a9955" }}
                        title="逐字符发送 (TUI)">
                        Raw
                    </button>
                )}
                {isSDK && (
                    <>
                        <input
                            ref={fileInputRef}
                            type="file"
                            accept="image/png,image/jpeg,image/gif,image/webp"
                            style={{ display: "none" }}
                            onChange={handleFileInputChange}
                        />
                        <button
                            onClick={() => fileInputRef.current?.click()}
                            disabled={disabled || imageUploading}
                            style={{ ...inputBtnStyle, color: "#c586c0", borderColor: "#c586c0" }}
                            title="上传图片 (也可粘贴)"
                        >
                            {imageUploading ? "…" : "📷"}
                        </button>
                        <button
                            onClick={handleScreenshot}
                            disabled={disabled || imageUploading}
                            style={{ ...inputBtnStyle, color: "#4ec9b0", borderColor: "#4ec9b0" }}
                            title="截图 (全屏或指定窗口)"
                        >
                            🖥
                        </button>
                    </>
                )}
                <button onClick={handleSend} disabled={disabled}
                    style={{ ...inputBtnStyle, color: "#569cd6", borderColor: "#569cd6" }}
                    title="发送">
                    {sending ? "…" : "⏎"}
                </button>
            </div>
            )}
            {readOnly && (
                <div style={{ ...inputBarStyle, justifyContent: "center" }}>
                    <span style={{ color: "#6a6a6a", fontSize: "11px", fontFamily: "Consolas, monospace" }}>
                        🔒 AI 进程监控模式 — 仅查看
                    </span>
                </div>
            )}
        </div>
    );
}
