import { useState, useRef, useCallback, useEffect, type Dispatch, type SetStateAction } from "react";
import type { RemoteSessionView, ImportantEventView } from "./types";
import { colors } from "./styles";

// Strip ANSI escape sequences and non-printable control characters
const ansiRe = /\x1b(?:\[[0-9;?]*[a-zA-Z]|\[[0-9;?]*[~^$]|\].*?(?:\x07|\x1b\\)|\([A-Z0-9]|[()#][A-Z0-9]?|[a-zA-Z])/g;
const controlRe = /[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]/g;
const stripAnsi = (s: string): string => s.replace(ansiRe, " ").replace(controlRe, "").replace(/ {2,}/g, " ");

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
    onClose: () => void;
};

const TERMINAL_STATUSES = new Set(["stopped", "finished", "failed", "killed", "exited", "closed", "done", "completed", "terminated", "error"]);

/* ── Status helpers (matching PWA logic) ── */

type StatusMeta = { icon: string; tone: string; label: string };

const statusMeta = (status: string, severity?: string): StatusMeta => {
    const s = status.toLowerCase();
    if (s === "waiting_input") return { icon: "⏸", tone: "warn", label: "等待输入" };
    if (s === "busy" || s === "running") return { icon: "⏳", tone: "info", label: "运行中" };
    if (s === "error" || s === "failed") return { icon: "✗", tone: "bad", label: "错误" };
    if (s === "unreachable") return { icon: "⚠", tone: "bad", label: "不可达" };
    if (TERMINAL_STATUSES.has(s)) return { icon: "●", tone: "muted", label: status };
    if (severity === "error") return { icon: "⚠", tone: "bad", label: status };
    if (severity === "warning" || severity === "warn") return { icon: "⚠", tone: "warn", label: status };
    return { icon: "●", tone: "info", label: status };
};

const toneColors: Record<string, { bg: string; fg: string; dot: string; border: string }> = {
    info: { bg: "rgba(15,79,214,0.08)", fg: "#0a3ba6", dot: "#0f4fd6", border: "rgba(15,79,214,0.18)" },
    good: { bg: "rgba(22,118,80,0.08)", fg: "#167650", dot: "#167650", border: "rgba(22,118,80,0.18)" },
    warn: { bg: "rgba(156,105,0,0.1)", fg: "#9c6900", dot: "#9c6900", border: "rgba(156,105,0,0.18)" },
    bad: { bg: "rgba(193,62,53,0.08)", fg: "#c13e35", dot: "#c13e35", border: "rgba(193,62,53,0.18)" },
    muted: { bg: colors.bg, fg: colors.textMuted, dot: colors.textMuted, border: colors.border },
};

const getEventIcon = (type?: string): { icon: string; color: string } => {
    if (!type) return { icon: "◌", color: "" };
    if (type.startsWith("file.")) return { icon: "📄", color: "" };
    if (type === "command.started") return { icon: "▶", color: "#0f4fd6" };
    if (type === "command.success") return { icon: "✓", color: "#167650" };
    if (type === "command.failed") return { icon: "✗", color: "#c13e35" };
    if (type === "task.completed") return { icon: "✅", color: "#167650" };
    if (type === "input.required") return { icon: "⏸", color: "#9c6900" };
    if (type.includes("error")) return { icon: "⚠", color: "#c13e35" };
    return { icon: "◌", color: "" };
};

const formatEventTime = (ts?: number): string => {
    if (!ts) return "";
    const d = new Date(ts * 1000);
    return d.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
};

/* ── Collapsible section component ── */
function DetailSection(props: {
    title: string;
    defaultExpanded?: boolean;
    badge?: string;
    children: React.ReactNode;
}) {
    const [expanded, setExpanded] = useState(props.defaultExpanded ?? false);
    return (
        <div style={{
            marginTop: "12px",
            border: `1px solid rgba(17,55,122,0.1)`,
            borderRadius: "16px",
            background: "rgba(255,255,255,0.92)",
            overflow: "hidden",
        }}>
            <button
                type="button"
                onClick={() => setExpanded((v) => !v)}
                style={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    width: "100%",
                    minHeight: "46px",
                    padding: "12px 16px",
                    border: "none",
                    background: "transparent",
                    cursor: "pointer",
                    color: colors.text,
                    fontSize: "0.88rem",
                    fontWeight: 800,
                    letterSpacing: "-0.01em",
                    borderRadius: 0,
                }}
            >
                <span>
                    {props.title}
                    {props.badge && <span style={{ marginLeft: "6px", fontSize: "0.72rem", fontWeight: 600, color: colors.textMuted }}>({props.badge})</span>}
                </span>
                <strong style={{
                    fontSize: "0.82rem",
                    color: colors.textMuted,
                    transition: "transform 0.18s ease",
                    transform: expanded ? "rotate(180deg)" : "rotate(0deg)",
                }}>⌄</strong>
            </button>
            {expanded && (
                <div style={{ padding: "0 16px 16px" }}>
                    {props.children}
                </div>
            )}
        </div>
    );
}

/* ── Task banner (matches PWA behavior) ── */
function TaskBanner(props: { status: string; severity?: string; waitingForUser?: boolean; lastResult?: string }) {
    const { status, severity, waitingForUser, lastResult } = props;
    const s = status.toLowerCase();
    const sessionClosed = TERMINAL_STATUSES.has(s);
    const isTaskCompleted = s === "waiting_input" && waitingForUser;
    const isBusy = s === "busy" || s === "running";
    const lastResultLower = (lastResult || "").toLowerCase();
    const isCommandFailed = severity === "warn" && lastResultLower.includes("fail");
    const isCommandSuccess = isBusy && (lastResultLower.includes("success") || lastResultLower.includes("passed") || lastResultLower.includes("completed"));

    if (sessionClosed || s === "unreachable") return null;

    let icon = "";
    let text = "";
    let tone = "";
    if (isTaskCompleted) { icon = "✅"; text = "任务已完成，等待你的下一步指令"; tone = "good"; }
    else if (isCommandFailed) { icon = "⚠"; text = "上一条命令执行失败"; tone = "bad"; }
    else if (isCommandSuccess) { icon = "✓"; text = "命令执行成功"; tone = "info"; }
    else if (isBusy) { icon = "⏳"; text = "正在执行中…"; tone = "info"; }
    else return null;

    const tc = toneColors[tone] || toneColors.info;
    return (
        <div style={{
            display: "flex",
            alignItems: "center",
            gap: "10px",
            marginTop: "10px",
            padding: "12px 14px",
            borderRadius: "14px",
            fontSize: "0.82rem",
            fontWeight: 700,
            background: tc.bg,
            border: `1px solid ${tc.border}`,
            color: tc.fg,
            animation: "fadeSoft 0.3s ease",
        }}>
            <span style={{ fontSize: "1rem", flex: "0 0 auto" }}>{icon}</span>
            <span style={{ flex: 1, lineHeight: 1.4 }}>{text}</span>
        </div>
    );
}

/* ── Send feedback indicator (matches PWA) ── */
function SendFeedback(props: { status: SendStatus }) {
    if (props.status === "idle") return null;
    const map: Record<string, { bg: string; border: string; fg: string; text: string }> = {
        sending: { bg: "rgba(15,79,214,0.08)", border: "rgba(15,79,214,0.14)", fg: "#0a3ba6", text: "正在发送…" },
        sent: { bg: "rgba(22,118,80,0.08)", border: "rgba(22,118,80,0.16)", fg: "#167650", text: "指令已发送 ✓" },
        failed: { bg: "rgba(193,62,53,0.08)", border: "rgba(193,62,53,0.16)", fg: "#c13e35", text: "发送失败 ✗" },
    };
    const s = map[props.status] || map.sending;
    return (
        <div style={{
            display: "flex",
            alignItems: "center",
            gap: "6px",
            marginTop: "8px",
            padding: "8px 14px",
            borderRadius: "14px",
            fontSize: "0.78rem",
            fontWeight: 700,
            background: s.bg,
            border: `1px solid ${s.border}`,
            color: s.fg,
        }}>
            {s.text}
        </div>
    );
}

/* ── Main console component ── */
export function RemoteSessionConsole(props: Props) {
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
        onClose,
    } = props;

    const [sendStatus, setSendStatus] = useState<SendStatus>("idle");
    const sendTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const textareaRef = useRef<HTMLTextAreaElement | null>(null);
    const previewEndRef = useRef<HTMLDivElement | null>(null);
    const [composing, setComposing] = useState(false);

    const summary = session.summary || {};
    const preview = session.preview || {};
    const events = session.events || [];
    const status = summary.status || session.status || "unknown";
    const severity = summary.severity || "info";
    const meta = statusMeta(status, severity);
    const tc = toneColors[meta.tone] || toneColors.info;
    const sessionClosed = TERMINAL_STATUSES.has(status.toLowerCase());
    const controlsDisabled = sessionClosed || status === "unreachable";

    const rawPreviewLines = preview.preview_lines || [];
    const previewLines = rawPreviewLines.map((l) => stripAnsi(l).trimEnd()).filter((l) => l.length > 0);

    const currentTask = summary.current_task || "-";
    const lastResult = summary.last_result || "-";
    const progressSummary = summary.progress_summary || "-";
    const suggestedAction = summary.suggested_action || "-";

    // Metric card styling (matching PWA logic)
    const lastResultLower = (summary.last_result || "").toLowerCase();
    const isResultSuccess = lastResultLower.includes("success") || lastResultLower.includes("passed") || lastResultLower.includes("completed");
    const isResultFailure = severity === "error" || lastResultLower.includes("fail") || lastResultLower.includes("error");
    const lastResultTone = lastResult === "-" ? "muted" : isResultFailure ? "bad" : isResultSuccess ? "good" : "";
    const progressTone = progressSummary === "-" ? "muted" : "";
    const actionClickable = suggestedAction !== "-";

    // Auto-scroll preview
    useEffect(() => {
        if (previewEndRef.current) {
            previewEndRef.current.scrollIntoView({ behavior: "smooth" });
        }
    }, [previewLines.length]);

    // Focus textarea on open
    useEffect(() => {
        setTimeout(() => textareaRef.current?.focus(), 100);
    }, []);

    // Close on Escape
    useEffect(() => {
        const handler = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
        window.addEventListener("keydown", handler);
        return () => window.removeEventListener("keydown", handler);
    }, [onClose]);

    const handleSend = useCallback(async () => {
        const text = (remoteInputDrafts[session.id] || "").trim();
        if (!text || sendStatus === "sending") return;
        console.log(`[console] handleSend: text=${JSON.stringify(text)}, sessionId=${session.id}`);
        setSendStatus("sending");
        try {
            const ok = await sendRemoteInput(session.id);
            console.log(`[console] sendRemoteInput returned: ${ok}`);
            setSendStatus(ok ? "sent" : "failed");
        } catch (e) {
            console.error("[console] handleSend error:", e);
            setSendStatus("failed");
        }
        if (sendTimerRef.current) clearTimeout(sendTimerRef.current);
        sendTimerRef.current = setTimeout(() => setSendStatus("idle"), 4000);
    }, [remoteInputDrafts, session.id, sendRemoteInput, sendStatus]);

    const applySuggested = () => {
        if (!actionClickable) return;
        setRemoteInputDrafts((prev) => ({ ...prev, [session.id]: suggestedAction }));
        textareaRef.current?.focus();
    };

    /* ── Shared inline styles ── */
    const metricStyle = (tone?: string): React.CSSProperties => ({
        borderRadius: "14px",
        border: `1px solid ${tone === "bad" ? "rgba(193,62,53,0.18)" : tone === "good" ? "rgba(22,118,80,0.2)" : "rgba(17,55,122,0.1)"}`,
        background: tone === "bad" ? "rgba(193,62,53,0.06)" : tone === "good" ? "rgba(22,118,80,0.06)" : "linear-gradient(180deg, rgba(255,255,255,0.98) 0%, rgba(244,249,255,0.96) 100%)",
        padding: "10px 12px",
        minHeight: "72px",
        display: "grid",
        alignContent: "start",
        gap: "6px",
    });
    const metricLabelStyle: React.CSSProperties = {
        margin: 0,
        fontSize: "0.66rem",
        color: colors.textMuted,
        textTransform: "uppercase",
        letterSpacing: "0.08em",
        fontWeight: 800,
    };
    const metricValueStyle = (tone?: string): React.CSSProperties => ({
        margin: 0,
        fontSize: "0.8rem",
        lineHeight: 1.45,
        fontWeight: 700,
        color: tone === "muted" ? colors.textMuted : tone === "bad" ? "#c13e35" : tone === "good" ? "#167650" : colors.text,
        wordBreak: "break-word",
    });

    const sourceLabel = session.launch_source === "mobile" ? "📱 手机启动" : session.launch_source === "handoff" ? "🔀 本地转远程" : "☁️ 远程";

    return (
        <div
            style={{ position: "fixed", inset: 0, zIndex: 10000, display: "flex", alignItems: "center", justifyContent: "center", background: "rgba(0,0,0,0.4)" }}
            onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
        >
            <div
                style={{
                    width: "min(94vw, 780px)",
                    maxHeight: "min(92vh, 800px)",
                    background: "linear-gradient(180deg, rgba(255,255,255,0.96) 0%, rgba(240,247,255,0.94) 100%)",
                    borderRadius: "22px",
                    border: "1px solid rgba(255,255,255,0.65)",
                    boxShadow: "0 24px 70px rgba(20,49,102,0.18), inset 0 1px 0 rgba(255,255,255,0.7)",
                    display: "flex",
                    flexDirection: "column",
                    overflow: "hidden",
                }}
                onClick={(e) => e.stopPropagation()}
            >

                {/* ── Header hero ── */}
                <div style={{
                    padding: "16px 18px",
                    background: "linear-gradient(180deg, rgba(244,249,255,0.99) 0%, rgba(234,243,255,0.98) 100%)",
                    borderBottom: "1px solid rgba(17,55,122,0.1)",
                    flexShrink: 0,
                }}>
                    <div style={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between", gap: "12px" }}>
                        <div style={{ minWidth: 0, flex: 1 }}>
                            <h2 style={{ margin: "0 0 4px", fontSize: "1.1rem", letterSpacing: "-0.03em", color: colors.text }}>
                                {summary.title || session.tool || "远程会话"}
                            </h2>
                            <p style={{ margin: 0, color: colors.textMuted, fontSize: "0.78rem", lineHeight: 1.5 }}>
                                {session.tool || "tool"} · {session.id.length > 24 ? session.id.slice(0, 22) + "…" : session.id} · {sourceLabel}
                            </p>
                        </div>
                        <button
                            onClick={onClose}
                            style={{
                                border: "none",
                                background: "rgba(255,255,255,0.96)",
                                borderRadius: "999px",
                                width: "32px",
                                height: "32px",
                                display: "flex",
                                alignItems: "center",
                                justifyContent: "center",
                                cursor: "pointer",
                                fontSize: "0.9rem",
                                color: colors.textMuted,
                                boxShadow: `0 0 0 1px ${colors.border}`,
                                flexShrink: 0,
                                padding: 0,
                            }}
                            title="关闭"
                        >✕</button>
                    </div>

                    {/* Status strip */}
                    <div style={{
                        display: "flex",
                        alignItems: "center",
                        gap: "12px",
                        marginTop: "12px",
                        padding: "10px 14px",
                        borderRadius: "14px",
                        background: "rgba(255,255,255,0.86)",
                        border: "1px solid rgba(17,55,122,0.1)",
                    }}>
                        <div style={{
                            width: "12px",
                            height: "12px",
                            borderRadius: "50%",
                            background: tc.dot,
                            boxShadow: `0 0 0 6px ${tc.bg}`,
                            flexShrink: 0,
                        }} />
                        <div style={{ minWidth: 0, display: "grid", gap: "2px" }}>
                            <strong style={{ fontSize: "0.8rem", letterSpacing: "-0.01em", color: tc.fg }}>
                                {meta.icon} {meta.label}
                            </strong>
                            <span style={{ color: colors.textMuted, fontSize: "0.74rem", lineHeight: 1.45 }}>
                                {currentTask !== "-" ? currentTask : sourceLabel}
                            </span>
                        </div>
                    </div>
                </div>

                {/* ── Scrollable body ── */}
                <div style={{ flex: 1, minHeight: 0, overflowY: "auto", padding: "0 18px 18px" }}>

                    {/* Metric grid (2x2, matching PWA) */}
                    <div style={{ display: "grid", gridTemplateColumns: "repeat(2, minmax(0, 1fr))", gap: "8px", marginTop: "12px" }}>
                        <div style={{ ...metricStyle("info"), background: "linear-gradient(180deg, rgba(15,79,214,0.08) 0%, rgba(255,255,255,0.98) 100%)", borderColor: "rgba(15,79,214,0.16)" }}>
                            <p style={metricLabelStyle}>当前任务</p>
                            <p style={metricValueStyle(currentTask === "-" ? "muted" : undefined)}>{currentTask}</p>
                        </div>
                        <div style={metricStyle(lastResultTone)}>
                            <p style={metricLabelStyle}>最近结果</p>
                            <p style={metricValueStyle(lastResultTone)}>{lastResult}</p>
                        </div>
                        <div style={metricStyle(progressTone)}>
                            <p style={metricLabelStyle}>进度</p>
                            <p style={metricValueStyle(progressTone)}>{progressSummary}</p>
                        </div>
                        <div
                            style={{
                                ...metricStyle(),
                                background: "linear-gradient(180deg, rgba(255,191,71,0.1) 0%, rgba(255,255,255,0.98) 100%)",
                                borderColor: "rgba(255,191,71,0.2)",
                                cursor: actionClickable ? "pointer" : "default",
                                transition: "transform 0.12s ease",
                            }}
                            onClick={applySuggested}
                            title={actionClickable ? "点击填入指令输入框" : undefined}
                        >
                            <p style={metricLabelStyle}>建议操作</p>
                            <p style={metricValueStyle(suggestedAction === "-" ? "muted" : undefined)}>{suggestedAction}</p>
                        </div>
                    </div>

                    {/* Task banner */}
                    <TaskBanner status={status} severity={severity} waitingForUser={summary.waiting_for_user} lastResult={summary.last_result} />

                    {/* Send feedback */}
                    <SendFeedback status={sendStatus} />

                    {/* Events section (collapsible) */}
                    <DetailSection title="最近事件" badge={events.length > 0 ? String(events.length) : undefined} defaultExpanded={false}>
                        {events.length === 0 ? (
                            <div style={{ padding: "12px", borderRadius: "14px", border: "1px dashed rgba(15,79,214,0.16)", background: "rgba(247,251,255,0.78)", textAlign: "center" }}>
                                <p style={{ margin: 0, color: colors.textMuted, fontSize: "0.76rem" }}>暂无事件</p>
                            </div>
                        ) : (
                            <div style={{ display: "grid", gap: "8px", marginTop: "4px" }}>
                                {events.slice(-10).reverse().map((evt: ImportantEventView, i: number) => {
                                    const evtMeta = getEventIcon(evt.type);
                                    return (
                                        <div
                                            key={evt.event_id || i}
                                            style={{
                                                padding: "10px 12px",
                                                borderRadius: "14px",
                                                background: "rgba(255,255,255,0.76)",
                                                border: `1px solid ${colors.border}`,
                                                color: evtMeta.color || colors.text,
                                            }}
                                        >
                                            <strong style={{ display: "block", marginBottom: "3px", fontSize: "0.8rem" }}>
                                                {evtMeta.icon} {evt.title || evt.type || "事件"}
                                                {evt.count && evt.count > 1 ? ` (×${evt.count})` : ""}
                                                <span style={{ float: "right", fontSize: "0.68rem", color: colors.textMuted, fontWeight: 400 }}>
                                                    {formatEventTime(evt.created_at)}
                                                </span>
                                            </strong>
                                            {evt.summary && (
                                                <div style={{ color: colors.textMuted, fontSize: "0.76rem", lineHeight: 1.45 }}>{evt.summary}</div>
                                            )}
                                            {evt.command && (
                                                <div style={{ marginTop: "3px", fontFamily: "Consolas, 'SFMono-Regular', monospace", fontSize: "0.72rem", color: "#4a5568", background: "rgba(0,0,0,0.04)", padding: "2px 8px", borderRadius: "6px", display: "inline-block" }}>
                                                    $ {evt.command}
                                                </div>
                                            )}
                                        </div>
                                    );
                                })}
                            </div>
                        )}
                    </DetailSection>

                    {/* Preview output section (collapsible) */}
                    <DetailSection title="输出预览" badge={previewLines.length > 0 ? String(previewLines.length) : undefined} defaultExpanded={previewLines.length > 0}>
                        <div style={{
                            padding: "12px",
                            borderRadius: "14px",
                            background: "rgba(255,255,255,0.76)",
                            border: `1px solid ${colors.border}`,
                            fontFamily: "Consolas, 'SFMono-Regular', monospace",
                            whiteSpace: "pre-wrap",
                            wordBreak: "break-word",
                            fontSize: "0.76rem",
                            lineHeight: 1.65,
                            maxHeight: "320px",
                            overflowY: "auto",
                            color: colors.text,
                        }}>
                            {previewLines.length === 0 ? (
                                <span style={{ color: colors.textMuted, fontStyle: "italic" }}>暂无输出</span>
                            ) : (
                                previewLines.join("\n")
                            )}
                            <div ref={previewEndRef} />
                        </div>
                    </DetailSection>

                    {/* Controls section (collapsible, default expanded) */}
                    <DetailSection title="控制台" defaultExpanded={true}>
                        <label style={{
                            display: "block",
                            marginBottom: "6px",
                            fontSize: "0.74rem",
                            fontWeight: 700,
                            color: colors.textMuted,
                            textTransform: "uppercase",
                            letterSpacing: "0.03em",
                        }}>
                            指令输入
                        </label>
                        <textarea
                            ref={textareaRef}
                            rows={4}
                            style={{
                                width: "100%",
                                border: `1px solid ${colors.border}`,
                                borderRadius: "14px",
                                padding: "12px 14px",
                                fontFamily: "inherit",
                                fontSize: "0.82rem",
                                color: colors.text,
                                background: "rgba(247,250,255,0.96)",
                                outline: "none",
                                resize: "vertical",
                                lineHeight: 1.5,
                                boxSizing: "border-box",
                            }}
                            value={remoteInputDrafts[session.id] || ""}
                            onChange={(e) => setRemoteInputDrafts((prev) => ({ ...prev, [session.id]: e.target.value }))}
                            onCompositionStart={() => setComposing(true)}
                            onCompositionEnd={() => setComposing(false)}
                            onKeyDown={(e) => {
                                if (e.key === "Enter" && !e.shiftKey && !composing) {
                                    e.preventDefault();
                                    handleSend();
                                }
                            }}
                            placeholder={controlsDisabled ? (sessionClosed ? "会话已结束" : "桌面端离线") : "告诉工具下一步做什么…"}
                            disabled={controlsDisabled || sendStatus === "sending"}
                        />
                        <div style={{ display: "flex", flexWrap: "wrap", gap: "8px", marginTop: "12px" }}>
                            <button
                                className="btn-primary"
                                style={{ padding: "10px 18px", borderRadius: "999px", fontWeight: 700, fontSize: "0.82rem" }}
                                disabled={controlsDisabled || sendStatus === "sending"}
                                onClick={handleSend}
                            >
                                {sendStatus === "sending" ? "发送中…" : "发送"}
                            </button>
                            <button
                                className="btn-secondary"
                                style={{ padding: "10px 18px", borderRadius: "999px", fontWeight: 700, fontSize: "0.82rem" }}
                                disabled={controlsDisabled}
                                onClick={async () => {
                                    try {
                                        await interruptRemoteSession(session.id);
                                        showToastMessage(translate("remoteInterruptSent"), 2500);
                                    } catch (err) {
                                        showToastMessage(formatText("remoteInterruptFailed", { error: String(err) }), 4000);
                                    }
                                }}
                            >
                                中断
                            </button>
                            <button
                                className="btn-secondary"
                                style={{
                                    padding: "10px 18px",
                                    borderRadius: "999px",
                                    fontWeight: 700,
                                    fontSize: "0.82rem",
                                    background: "rgba(193,62,53,0.1)",
                                    color: "#c13e35",
                                    borderColor: "rgba(193,62,53,0.2)",
                                }}
                                disabled={controlsDisabled}
                                onClick={async () => {
                                    try {
                                        await killRemoteSession(session.id);
                                        showToastMessage(translate("remoteKillSent"), 2500);
                                        onClose();
                                    } catch (err) {
                                        showToastMessage(formatText("remoteKillFailed", { error: String(err) }), 4000);
                                    }
                                }}
                            >
                                终止会话
                            </button>
                        </div>
                    </DetailSection>

                </div>
            </div>
        </div>
    );
}
