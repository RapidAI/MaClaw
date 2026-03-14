import { useState, useRef, useCallback, useEffect, type Dispatch, type SetStateAction } from "react";
import type { RemoteSessionView } from "./types";
import { SendRemoteSessionInput, SendRemoteSessionRawInput, InterruptRemoteSession } from "../../../wailsjs/go/main/App";

type Props = {
    session: RemoteSessionView;
    remoteInputDrafts: Record<string, string>;
    setRemoteInputDrafts: Dispatch<SetStateAction<Record<string, string>>>;
    killRemoteSession: (sessionID: string) => Promise<void>;
    refreshSessionsOnly: () => Promise<void>;
    onClose: () => void;
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
    const prevRawCountRef = useRef(0);
    const prevLastLineRef = useRef("");
    const sendInfoTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

    const status = (session.summary?.status || session.status || "unknown").toLowerCase();
    const sessionClosed = TERMINAL_STATUSES.has(status);
    const disabled = sessionClosed || sending;
    const isSDK = session.execution_mode === "sdk";

    const rawLines = session.raw_output_lines || session.preview?.preview_lines || [];

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

    // Auto-scroll to bottom when output changes
    useEffect(() => {
        const lastLine = rawLines.length > 0 ? rawLines[rawLines.length - 1] : "";
        if (rawLines.length !== prevRawCountRef.current || lastLine !== prevLastLineRef.current) {
            prevRawCountRef.current = rawLines.length;
            prevLastLineRef.current = lastLine;
            outputEndRef.current?.scrollIntoView({ behavior: "smooth" });
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

    const statusColor = status === "running" || status === "busy" ? "#4ec9b0"
        : status === "waiting_input" ? "#dcdcaa" : "#808080";

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
                    {!isSDK && (
                        <button onClick={handleEsc} disabled={sessionClosed}
                            style={actionBtnStyle} title="Escape">
                            Esc
                        </button>
                    )}
                    <button onClick={handleCtrlC} disabled={sessionClosed}
                        style={{ ...actionBtnStyle, color: "#e8a838" }}
                        title={isSDK ? "中断" : "Ctrl+C"}>
                        {isSDK ? "⏸" : "⌃C"}
                    </button>
                    <button onClick={handleKill} disabled={sessionClosed}
                        style={{ ...actionBtnStyle, color: "#f44747" }}
                        title="终止">
                        Kill
                    </button>
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
                    rawLines.map((line, i) => (
                        <div key={i} style={{ minHeight: "1.4em" }}>{line || "\u00A0"}</div>
                    ))
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
                <button onClick={handleSend} disabled={disabled}
                    style={{ ...inputBtnStyle, color: "#569cd6", borderColor: "#569cd6" }}
                    title="发送">
                    {sending ? "…" : "⏎"}
                </button>
            </div>
        </div>
    );
}
