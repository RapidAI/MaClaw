import { useState, useRef, useCallback, useEffect, type Dispatch, type SetStateAction } from "react";
import type { RemoteSessionView } from "./types";
import { SendRemoteSessionInput, SendRemoteSessionRawInput } from "../../../wailsjs/go/main/App";

type Props = {
    session: RemoteSessionView;
    remoteInputDrafts: Record<string, string>;
    setRemoteInputDrafts: Dispatch<SetStateAction<Record<string, string>>>;
    sendRemoteInput: (sessionID: string) => Promise<boolean>;
    interruptRemoteSession: (sessionID: string) => Promise<void>;
    killRemoteSession: (sessionID: string) => Promise<void>;
    refreshSessionsOnly: () => Promise<void>;
    showToastMessage: (message: string, duration?: number) => void;
    translate: (key: string) => string;
    formatText: (key: string, values?: Record<string, string>) => string;
    onClose: () => void;
};

const TERMINAL_STATUSES = new Set([
    "stopped", "finished", "failed", "killed", "exited",
    "closed", "done", "completed", "terminated", "error",
]);

export function RemoteSessionConsole(props: Props) {
    const {
        session,
        remoteInputDrafts,
        setRemoteInputDrafts,
        killRemoteSession,
        refreshSessionsOnly,
        onClose,
    } = props;

    const [sending, setSending] = useState(false);
    const [lastSendInfo, setLastSendInfo] = useState("");
    const inputRef = useRef<HTMLInputElement | null>(null);
    const outputEndRef = useRef<HTMLDivElement | null>(null);
    const outputContainerRef = useRef<HTMLDivElement | null>(null);
    const [composing, setComposing] = useState(false);
    const inputValueRef = useRef("");
    // Track previous raw line count to detect new output
    const prevRawCountRef = useRef(0);

    const status = (session.summary?.status || session.status || "unknown").toLowerCase();
    const sessionClosed = TERMINAL_STATUSES.has(status);
    const disabled = sessionClosed || sending;

    const rawLines = session.raw_output_lines || session.preview?.preview_lines || [];

    // Auto-scroll to bottom when output changes
    useEffect(() => {
        if (rawLines.length !== prevRawCountRef.current) {
            prevRawCountRef.current = rawLines.length;
            outputEndRef.current?.scrollIntoView({ behavior: "smooth" });
        }
    }, [rawLines.length]);

    // Focus input on open
    useEffect(() => {
        setTimeout(() => inputRef.current?.focus(), 100);
    }, []);

    // Close on Escape
    useEffect(() => {
        const handler = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
        window.addEventListener("keydown", handler);
        return () => window.removeEventListener("keydown", handler);
    }, [onClose]);

    // Send input: writes each character individually to the PTY for TUI
    // compatibility, then sends \r to simulate Enter.
    const handleSend = useCallback(async () => {
        const text = inputValueRef.current.trim();
        if (!text || sending) return;
        setSending(true);
        setLastSendInfo("");
        try {
            // Send the full text + newline via the standard input path.
            // The backend normalizes \n → \r\n for ConPTY.
            await SendRemoteSessionInput(session.id, text + "\n");
            setLastSendInfo(`✓ "${text}"`);
            // Clear input
            inputValueRef.current = "";
            setRemoteInputDrafts((prev) => ({ ...prev, [session.id]: "" }));
            // Schedule refreshes to pick up response
            setTimeout(() => refreshSessionsOnly(), 200);
            setTimeout(() => refreshSessionsOnly(), 800);
            setTimeout(() => refreshSessionsOnly(), 2000);
            setTimeout(() => refreshSessionsOnly(), 5000);
        } catch (e) {
            setLastSendInfo(`✗ ${String(e)}`);
        }
        setSending(false);
    }, [session.id, sending, setRemoteInputDrafts, refreshSessionsOnly]);

    // Send raw keystrokes — for TUI apps that need character-by-character input
    const handleSendRaw = useCallback(async () => {
        const text = inputValueRef.current;
        if (!text || sending) return;
        setSending(true);
        try {
            // Send each character individually with a small delay
            for (let i = 0; i < text.length; i++) {
                await SendRemoteSessionRawInput(session.id, text[i]);
                if (i < text.length - 1) {
                    await new Promise((r) => setTimeout(r, 30));
                }
            }
            // Send Enter (\r)
            await SendRemoteSessionRawInput(session.id, "\r");
            setLastSendInfo(`✓ raw: "${text}"`);
            inputValueRef.current = "";
            setRemoteInputDrafts((prev) => ({ ...prev, [session.id]: "" }));
            setTimeout(() => refreshSessionsOnly(), 200);
            setTimeout(() => refreshSessionsOnly(), 800);
            setTimeout(() => refreshSessionsOnly(), 2000);
        } catch (e) {
            setLastSendInfo(`✗ raw: ${String(e)}`);
        }
        setSending(false);
    }, [session.id, sending, setRemoteInputDrafts, refreshSessionsOnly]);

    const handleInputChange = (value: string) => {
        inputValueRef.current = value;
        setRemoteInputDrafts((prev) => ({ ...prev, [session.id]: value }));
    };

    // Send Ctrl+C (interrupt byte 0x03)
    const handleCtrlC = useCallback(async () => {
        try {
            await SendRemoteSessionRawInput(session.id, "\x03");
            setLastSendInfo("✓ Ctrl+C");
            setTimeout(() => refreshSessionsOnly(), 300);
        } catch (e) {
            setLastSendInfo(`✗ Ctrl+C: ${String(e)}`);
        }
    }, [session.id, refreshSessionsOnly]);

    // Send Escape (0x1b)
    const handleEsc = useCallback(async () => {
        try {
            await SendRemoteSessionRawInput(session.id, "\x1b");
            setLastSendInfo("✓ Esc");
            setTimeout(() => refreshSessionsOnly(), 300);
        } catch (e) {
            setLastSendInfo(`✗ Esc: ${String(e)}`);
        }
    }, [session.id, refreshSessionsOnly]);

    return (
        <div
            style={{ position: "fixed", inset: 0, zIndex: 10000, display: "flex", alignItems: "center", justifyContent: "center", background: "rgba(0,0,0,0.5)" }}
            onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
        >
            <div
                style={{
                    width: "min(96vw, 900px)",
                    height: "min(90vh, 740px)",
                    background: "#1e1e1e",
                    borderRadius: "12px",
                    border: "1px solid #333",
                    boxShadow: "0 20px 60px rgba(0,0,0,0.5)",
                    display: "flex",
                    flexDirection: "column",
                    overflow: "hidden",
                }}
                onClick={(e) => e.stopPropagation()}
            >
                {/* Title bar */}
                <div style={{
                    display: "flex", alignItems: "center", justifyContent: "space-between",
                    padding: "8px 14px", background: "#2d2d2d",
                    borderBottom: "1px solid #404040", flexShrink: 0,
                }}>
                    <div style={{ display: "flex", alignItems: "center", gap: "10px", minWidth: 0 }}>
                        <span style={{
                            color: status === "running" || status === "busy" ? "#4ec9b0"
                                : status === "waiting_input" ? "#dcdcaa" : "#808080",
                            fontSize: "10px",
                        }}>●</span>
                        <span style={{ color: "#ccc", fontSize: "13px", fontFamily: "Consolas, 'SFMono-Regular', monospace" }}>
                            {session.tool || "session"} — {status} — lines:{rawLines.length}
                        </span>
                    </div>
                    <div style={{ display: "flex", gap: "6px", flexShrink: 0 }}>
                        <button onClick={handleEsc} disabled={sessionClosed} style={btnStyle("#569cd6")} title="Escape">Esc</button>
                        <button onClick={handleCtrlC} disabled={sessionClosed} style={btnStyle("#e8a838")} title="Ctrl+C">⌃C</button>
                        <button
                            onClick={async () => { try { await killRemoteSession(session.id); onClose(); } catch {} }}
                            disabled={sessionClosed}
                            style={btnStyle("#f44747")} title="终止"
                        >Kill</button>
                        <button onClick={onClose} style={btnStyle("#808080")} title="关闭">✕</button>
                    </div>
                </div>

                {/* Output area */}
                <div
                    ref={outputContainerRef}
                    style={{
                        flex: 1, minHeight: 0, overflowY: "auto", padding: "10px 14px",
                        fontFamily: "Consolas, 'Courier New', monospace", fontSize: "13px",
                        lineHeight: 1.55, color: "#d4d4d4", whiteSpace: "pre-wrap", wordBreak: "break-word",
                    }}
                >
                    {rawLines.length === 0 ? (
                        <span style={{ color: "#666", fontStyle: "italic" }}>等待输出...</span>
                    ) : (
                        rawLines.map((line, i) => (
                            <div key={i} style={{ minHeight: "1.55em" }}>{line || "\u00A0"}</div>
                        ))
                    )}
                    <div ref={outputEndRef} />
                </div>

                {/* Status bar */}
                {lastSendInfo && (
                    <div style={{
                        padding: "3px 14px",
                        background: lastSendInfo.startsWith("✓") ? "#1a2a1a" : "#3a1d1d",
                        color: lastSendInfo.startsWith("✓") ? "#89d185" : "#f48771",
                        fontSize: "11px", fontFamily: "Consolas, monospace",
                    }}>
                        {lastSendInfo}
                    </div>
                )}

                {/* Input bar */}
                <div style={{
                    display: "flex", alignItems: "center", gap: "8px",
                    padding: "8px 14px", background: "#252526",
                    borderTop: "1px solid #404040", flexShrink: 0,
                }}>
                    <span style={{ color: "#4ec9b0", fontFamily: "Consolas, monospace", fontSize: "13px", flexShrink: 0 }}>❯</span>
                    <input
                        ref={inputRef}
                        type="text"
                        style={{
                            flex: 1, background: "transparent", border: "none", outline: "none",
                            color: "#d4d4d4", fontFamily: "Consolas, 'Courier New', monospace",
                            fontSize: "13px", padding: "6px 0",
                        }}
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
                        placeholder={disabled ? (sessionClosed ? "会话已结束" : "发送中...") : "输入命令..."}
                        disabled={disabled}
                    />
                    <button onClick={handleSend} disabled={disabled}
                        style={{ ...btnStyle("#0e639c"), padding: "5px 14px", fontSize: "12px" }}
                        title="发送 (行模式)"
                    >{sending ? "..." : "发送"}</button>
                    <button onClick={handleSendRaw} disabled={disabled}
                        style={{ ...btnStyle("#6a9955"), padding: "5px 10px", fontSize: "11px" }}
                        title="逐字符发送 (TUI模式)"
                    >Raw</button>
                </div>
            </div>
        </div>
    );
}

function btnStyle(color: string): React.CSSProperties {
    return {
        background: "transparent",
        border: `1px solid ${color}`,
        color,
        borderRadius: "4px",
        padding: "3px 10px",
        fontSize: "11px",
        fontFamily: "Consolas, monospace",
        cursor: "pointer",
        lineHeight: 1.4,
    };
}
