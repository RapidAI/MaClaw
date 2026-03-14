import { useState, useRef, useCallback, useEffect, type Dispatch, type SetStateAction } from "react";
import type { RemoteSessionView, ImportantEventView } from "./types";
import {
    colors,
    radius,
    remoteSubLabelStyle,
    remoteInfoCardStyle,
    remoteSidePanelStyle,
} from "./styles";

// Strip ANSI escape sequences and non-printable control characters from terminal output
const ansiRe = /\x1b(?:\[[0-9;?]*[a-zA-Z~^$]|\].*?(?:\x07|\x1b\\)|[()#][A-Z0-9]?|[a-zA-Z])/g;
const controlRe = /[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]/g;
const multiSpaceRe = / {2,}/g;
const stripAnsi = (s: string): string => s.replace(ansiRe, " ").replace(controlRe, "").replace(multiSpaceRe, " ");

type SendStatus = "idle" | "sending" | "sent" | "failed";

type Props = {
    session: RemoteSessionView;
    remoteInputDrafts: Record<string, string>;
    setRemoteInputDrafts: Dispatch<SetStateAction<Record<string, string>>>;
    sendRemoteInput: (sessionID: string) => Promise<boolean>;
    interruptRemoteSession: (sessionID: string) => Promise<void>;
    killRemoteSession: (sessionID: string) => Promise<void>;
    showToastMessage: (message: string, duration?: number) => void;
    translate: (key: string) => string;
    formatText: (key: string, values?: Record<string, string>) => string;
    /** Called when the user clicks the preview area to open the fullscreen console */
    onOpenConsole?: (sessionID: string) => void;
};

const genericTitles = new Set(["参考文献", "Reference", "Untitled", "Project"]);

const getLaunchSourceLabel = (source?: string) => {
    if (source === "mobile") return "手机启动";
    if (source === "handoff") return "本地转远程";
    return "远程";
};

const getStatusStyle = (status?: string) => {
    const value = String(status || "").toLowerCase();
    if (value === "error" || value === "failed") return { background: colors.dangerBg, color: "#9b2c2c" };
    if (value === "waiting_input") return { background: colors.warningBg, color: colors.warning };
    if (["stopped", "finished", "killed", "closed", "done", "completed", "terminated", "exited"].includes(value)) {
        return { background: colors.bg, color: colors.textSecondary };
    }
    return { background: colors.accentBg, color: colors.primaryDark };
};

const getPathLeaf = (value?: string) => {
    if (!value) return "";
    const normalized = value.replace(/\\/g, "/").replace(/\/+$/, "");
    const parts = normalized.split("/").filter(Boolean);
    return parts[parts.length - 1] || "";
};

const getDisplayTitle = (session: RemoteSessionView) => {
    const pathLeaf = getPathLeaf(session.project_path) || getPathLeaf(session.workspace_root) || getPathLeaf(session.workspace_path);
    const rawTitle = String(session.title || "").trim();
    if (pathLeaf) return pathLeaf;
    if (rawTitle && !genericTitles.has(rawTitle)) return rawTitle;
    return session.tool || session.id;
};

const getLaunchSourceStyle = (source: string) => {
    if (source === "mobile") return { background: colors.successBg, color: "#276749" };
    if (source === "handoff") return { background: "#f3f0ff", color: "#553c9a" };
    return { background: colors.bg, color: colors.textSecondary };
};

const getSeverityStyle = (severity?: string): React.CSSProperties => {
    switch (severity) {
        case "error": return { borderLeft: "3px solid #c53030", background: colors.dangerBg };
        case "warning": return { borderLeft: "3px solid #b7791f", background: colors.warningBg };
        case "success": return { borderLeft: "3px solid #2f855a", background: colors.successBg };
        default: return { borderLeft: `3px solid ${colors.border}`, background: colors.bg };
    }
};

const formatEventTime = (ts?: number): string => {
    if (!ts) return "";
    const d = new Date(ts * 1000);
    return d.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
};

export function RemoteSessionCard(props: Props) {
    const {
        session,
        remoteInputDrafts,
        setRemoteInputDrafts,
        sendRemoteInput,
        interruptRemoteSession,
        killRemoteSession,
        showToastMessage,
        translate,
        formatText,
        onOpenConsole,
    } = props;

    const [sendStatus, setSendStatus] = useState<SendStatus>("idle");
    const [showOutput, setShowOutput] = useState(false);
    const [showEvents, setShowEvents] = useState(false);
    const sendTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const outputEndRef = useRef<HTMLDivElement | null>(null);

    // Cleanup send-status timer on unmount
    useEffect(() => {
        return () => {
            if (sendTimerRef.current) clearTimeout(sendTimerRef.current);
        };
    }, []);

    const handleSend = useCallback(async () => {
        const text = (remoteInputDrafts[session.id] || "").trim();
        if (!text || sendStatus === "sending") return;
        setSendStatus("sending");
        try {
            const ok = await sendRemoteInput(session.id);
            setSendStatus(ok ? "sent" : "failed");
        } catch {
            setSendStatus("failed");
        }
        if (sendTimerRef.current) clearTimeout(sendTimerRef.current);
        sendTimerRef.current = setTimeout(() => setSendStatus("idle"), 1800);
    }, [remoteInputDrafts, session.id, sendRemoteInput, sendStatus]);

    // Auto-scroll output to bottom when new lines arrive
    const rawPreviewLines = session.preview?.preview_lines || [];
    const previewLines = rawPreviewLines.map((l) => stripAnsi(l).trimEnd()).filter((l) => l.length > 0);
    useEffect(() => {
        if (showOutput && outputEndRef.current) {
            outputEndRef.current.scrollIntoView({ behavior: "smooth" });
        }
    }, [previewLines.length, showOutput]);

    const sendButtonLabel =
        sendStatus === "sending" ? "发送中…" :
        sendStatus === "sent" ? "已发送 ✓" :
        sendStatus === "failed" ? "发送失败 ✗" :
        "发送指令";

    const sendButtonStyle: React.CSSProperties | undefined =
        sendStatus === "sent" ? { background: colors.successBg, color: colors.success, borderColor: colors.success } :
        sendStatus === "failed" ? { background: colors.dangerBg, color: colors.danger, borderColor: colors.danger } :
        undefined;

    const launchSource = session.launch_source || session.summary?.source || "desktop";
    const launchSourceLabel = getLaunchSourceLabel(launchSource);
    const statusText = session.status || session.summary?.status || translate("remoteStatusUnknown");
    const statusStyle = getStatusStyle(statusText);
    const sourceStyle = getLaunchSourceStyle(launchSource);
    const currentTask = session.summary?.current_task || "-";
    const lastResult = session.summary?.last_result || "-";
    const progressSummary = session.summary?.progress_summary || "-";
    const suggestedAction = session.summary?.suggested_action || "";
    const lastCommand = session.summary?.last_command || "";
    const importantFiles = session.summary?.important_files || [];
    const displayTitle = getDisplayTitle(session);
    const events = session.events || [];
    const hasOutput = previewLines.length > 0;
    const hasEvents = events.length > 0;

    return (
        <div
            style={{
                border: `1px solid ${colors.border}`,
                borderRadius: radius.lg,
                background: colors.surface,
                overflow: "hidden",
            }}
        >
            {/* Main grid: info + side panel */}
            <div
                style={{
                    display: "grid",
                    gridTemplateColumns: "minmax(0, 2.2fr) minmax(220px, 1fr)",
                }}
            >
                <div style={{ padding: "10px 12px", minWidth: 0 }}>
                    {/* Header row */}
                    <div
                        style={{
                            display: "grid",
                            gridTemplateColumns: "minmax(160px, 1.2fr) minmax(90px, 0.7fr) minmax(80px, 0.7fr) minmax(0, 1.4fr)",
                            gap: "8px",
                            alignItems: "start",
                        }}
                    >
                        <div style={{ minWidth: 0 }}>
                            <div style={remoteSubLabelStyle}>实例</div>
                            <div style={{ fontSize: "0.84rem", fontWeight: 600, color: colors.text, marginBottom: "2px", wordBreak: "break-word" }}>
                                {displayTitle}
                            </div>
                            <div style={{ fontSize: "0.68rem", color: colors.textSecondary, wordBreak: "break-word" }}>{session.id}</div>
                        </div>

                        <div>
                            <div style={remoteSubLabelStyle}>类型</div>
                            <span style={{ display: "inline-flex", alignItems: "center", padding: "2px 8px", borderRadius: radius.pill, fontSize: "0.7rem", fontWeight: 600, background: sourceStyle.background, color: sourceStyle.color }}>
                                {launchSourceLabel}
                            </span>
                        </div>

                        <div>
                            <div style={remoteSubLabelStyle}>状态</div>
                            <span style={{ display: "inline-flex", alignItems: "center", padding: "2px 8px", borderRadius: radius.pill, fontSize: "0.7rem", fontWeight: 600, background: statusStyle.background, color: statusStyle.color }}>
                                {statusText}
                            </span>
                        </div>

                        <div style={{ minWidth: 0 }}>
                            <div style={remoteSubLabelStyle}>项目与工具</div>
                            <div style={{ fontSize: "0.74rem", color: colors.text, lineHeight: 1.4, wordBreak: "break-word" }}>{session.project_path || "-"}</div>
                            <div style={{ fontSize: "0.68rem", color: colors.textSecondary, marginTop: "2px" }}>工具: {session.tool || "-"}</div>
                        </div>
                    </div>

                    {/* Summary cards */}
                    <div style={{ display: "grid", gridTemplateColumns: "repeat(3, minmax(0, 1fr))", gap: "6px", marginTop: "8px" }}>
                        <div style={remoteInfoCardStyle}>
                            <div style={remoteSubLabelStyle}>当前任务</div>
                            <div style={{ fontSize: "0.74rem", color: colors.text, lineHeight: 1.4, wordBreak: "break-word" }}>{currentTask}</div>
                        </div>
                        <div style={remoteInfoCardStyle}>
                            <div style={remoteSubLabelStyle}>最近结果</div>
                            <div style={{ fontSize: "0.74rem", color: colors.text, lineHeight: 1.4, wordBreak: "break-word" }}>{lastResult}</div>
                        </div>
                        <div style={remoteInfoCardStyle}>
                            <div style={remoteSubLabelStyle}>进度</div>
                            <div style={{ fontSize: "0.74rem", color: colors.text, lineHeight: 1.4, wordBreak: "break-word" }}>{progressSummary}</div>
                        </div>
                    </div>

                    {/* Extra summary info: suggested action, last command, important files */}
                    {(suggestedAction || lastCommand || importantFiles.length > 0) && (
                        <div style={{ display: "flex", gap: "6px", marginTop: "6px", flexWrap: "wrap", alignItems: "center" }}>
                            {suggestedAction && (
                                <span style={{ fontSize: "0.7rem", padding: "2px 8px", borderRadius: radius.pill, background: colors.warningBg, color: colors.warning, fontWeight: 500 }}>
                                    💡 {suggestedAction}
                                </span>
                            )}
                            {lastCommand && (
                                <span style={{ fontSize: "0.7rem", padding: "2px 8px", borderRadius: radius.sm, background: "#1a202c", color: "#e2e8f0", fontFamily: "monospace" }}>
                                    $ {lastCommand}
                                </span>
                            )}
                            {importantFiles.length > 0 && (
                                <span style={{ fontSize: "0.68rem", color: colors.textMuted }}>
                                    📁 {importantFiles.slice(0, 3).join(", ")}{importantFiles.length > 3 ? ` +${importantFiles.length - 3}` : ""}
                                </span>
                            )}
                        </div>
                    )}

                    {/* Toggle buttons for output & events */}
                    <div style={{ display: "flex", gap: "6px", marginTop: "8px" }}>
                        <button
                            onClick={() => setShowOutput((v) => !v)}
                            style={{
                                border: `1px solid ${colors.border}`,
                                borderRadius: radius.sm,
                                background: showOutput ? colors.primaryDark : colors.bg,
                                color: showOutput ? "#fff" : colors.textSecondary,
                                fontSize: "0.7rem",
                                padding: "3px 10px",
                                cursor: "pointer",
                                fontWeight: 500,
                            }}
                        >
                            {showOutput ? "▼" : "▶"} 输出 {hasOutput ? `(${previewLines.length})` : "(空)"}
                        </button>
                        {hasEvents && (
                            <button
                                onClick={() => setShowEvents((v) => !v)}
                                style={{
                                    border: `1px solid ${colors.border}`,
                                    borderRadius: radius.sm,
                                    background: showEvents ? colors.primaryDark : colors.bg,
                                    color: showEvents ? "#fff" : colors.textSecondary,
                                    fontSize: "0.7rem",
                                    padding: "3px 10px",
                                    cursor: "pointer",
                                    fontWeight: 500,
                                }}
                            >
                                {showEvents ? "▼" : "▶"} 事件 ({events.length})
                            </button>
                        )}
                    </div>
                </div>

                {/* Side panel: actions + input */}
                <div style={remoteSidePanelStyle}>
                    <div>
                        <div style={{ ...remoteSubLabelStyle, marginBottom: "6px" }}>操作</div>
                        <div style={{ display: "flex", flexDirection: "column", gap: "5px" }}>
                            <button className="btn-primary" disabled={sendStatus === "sending"} style={sendButtonStyle} onClick={handleSend}>
                                {sendButtonLabel}
                            </button>
                            <button
                                className="btn-secondary"
                                onClick={async () => {
                                    try {
                                        await interruptRemoteSession(session.id);
                                        showToastMessage(translate("remoteInterruptSent"), 2500);
                                    } catch (err) {
                                        showToastMessage(formatText("remoteInterruptFailed", { error: String(err) }), 4000);
                                    }
                                }}
                            >
                                中断实例
                            </button>
                            <button
                                className="btn-secondary"
                                style={{ background: colors.dangerBg, color: "#9b2c2c", borderColor: "#feb2b2" }}
                                onClick={async () => {
                                    try {
                                        await killRemoteSession(session.id);
                                        showToastMessage(translate("remoteKillSent"), 2500);
                                    } catch (err) {
                                        showToastMessage(formatText("remoteKillFailed", { error: String(err) }), 4000);
                                    }
                                }}
                            >
                                停止实例
                            </button>
                        </div>
                    </div>

                    <div>
                        <div style={{ ...remoteSubLabelStyle, marginBottom: "6px" }}>快速输入</div>
                        <input
                            className="form-input"
                            style={{ width: "100%" }}
                            value={remoteInputDrafts[session.id] || ""}
                            onChange={(e) => setRemoteInputDrafts((prev) => ({ ...prev, [session.id]: e.target.value }))}
                            onKeyDown={(e) => {
                                if (e.key === "Enter" && !e.nativeEvent.isComposing) {
                                    e.preventDefault();
                                    handleSend();
                                }
                            }}
                            placeholder="输入指令后回车发送"
                            disabled={sendStatus === "sending"}
                        />
                    </div>
                </div>
            </div>

            {/* Output preview panel (terminal-like) — click to open fullscreen console */}
            {showOutput && (
                <div
                    style={{ borderTop: `1px solid ${colors.border}`, cursor: onOpenConsole ? "pointer" : undefined }}
                    onClick={onOpenConsole ? () => onOpenConsole(session.id) : undefined}
                    title={onOpenConsole ? "点击打开全屏终端" : undefined}
                >
                    {/* Terminal title bar */}
                    <div style={{
                        display: "flex", alignItems: "center", gap: "8px",
                        padding: "5px 12px", background: "#2d2d2d",
                        borderBottom: "1px solid #3a3a3a",
                    }}>
                        <span style={{ width: 10, height: 10, borderRadius: "50%", background: "#ff5f57", display: "inline-block" }} />
                        <span style={{ width: 10, height: 10, borderRadius: "50%", background: "#febc2e", display: "inline-block" }} />
                        <span style={{ width: 10, height: 10, borderRadius: "50%", background: "#28c840", display: "inline-block" }} />
                        <span style={{ flex: 1, textAlign: "center", fontSize: "0.68rem", color: "#888", fontFamily: "monospace" }}>
                            {session.tool || "terminal"} — {previewLines.length} lines
                        </span>
                        {onOpenConsole && (
                            <span style={{ fontSize: "0.68rem", color: "#6a9955", fontFamily: "monospace", flexShrink: 0 }}>
                                ⛶ 全屏
                            </span>
                        )}
                    </div>
                    {/* Terminal body */}
                    <div className="terminal-output">
                        {previewLines.length === 0 ? (
                            <span style={{ color: "#555" }}>$ _</span>
                        ) : (
                            previewLines.map((line, i) => (
                                <div key={i} style={{ minHeight: "1.2em" }}>
                                    {line || "\u00A0"}
                                </div>
                            ))
                        )}
                        <div ref={outputEndRef} />
                    </div>
                </div>
            )}

            {/* Events timeline */}
            {showEvents && hasEvents && (
                <div style={{ borderTop: `1px solid ${colors.border}`, padding: "8px 12px", maxHeight: "200px", overflowY: "auto" }}>
                    <div style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
                        {events.map((evt: ImportantEventView, i: number) => (
                            <div
                                key={evt.event_id || i}
                                style={{
                                    ...getSeverityStyle(evt.severity),
                                    borderRadius: radius.sm,
                                    padding: "5px 10px",
                                    fontSize: "0.72rem",
                                }}
                            >
                                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: "8px" }}>
                                    <span style={{ fontWeight: 600, color: colors.text }}>
                                        {evt.title || evt.type || "事件"}
                                        {evt.count && evt.count > 1 ? ` (×${evt.count})` : ""}
                                    </span>
                                    <span style={{ fontSize: "0.65rem", color: colors.textMuted, whiteSpace: "nowrap" }}>
                                        {formatEventTime(evt.created_at)}
                                    </span>
                                </div>
                                {evt.summary && (
                                    <div style={{ color: colors.textSecondary, marginTop: "2px", lineHeight: 1.4 }}>{evt.summary}</div>
                                )}
                                {evt.command && (
                                    <div style={{ marginTop: "2px", fontFamily: "monospace", fontSize: "0.68rem", color: "#4a5568", background: "rgba(0,0,0,0.04)", padding: "2px 6px", borderRadius: "3px", display: "inline-block" }}>
                                        $ {evt.command}
                                    </div>
                                )}
                                {evt.related_file && (
                                    <div style={{ fontSize: "0.65rem", color: colors.textMuted, marginTop: "2px" }}>📁 {evt.related_file}</div>
                                )}
                            </div>
                        ))}
                    </div>
                </div>
            )}
        </div>
    );
}
