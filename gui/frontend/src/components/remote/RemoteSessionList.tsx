import React, { useMemo, useState, useEffect, useCallback, type Dispatch, type SetStateAction } from "react";
import { colors, radius } from "./styles";
import { TERMINAL_SESSION_STATUSES, type RemoteSessionView } from "./types";
import { RemoteSessionConsole } from "./RemoteSessionConsole";
import { ScheduledTasksPanel } from "./ScheduledTasksPanel";
import { PassthroughCommandsPanel } from "./PassthroughCommandsPanel";
import { countActiveBackgroundLoops } from "../layout/backgroundTaskCount";
import { ListBackgroundLoops, StopBackgroundLoop, StopAllBackgroundLoops, StopAllBackgroundTasks, DismissRemoteSession, ContinueBackgroundLoop, GetBackgroundLoopOutput } from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";

// Strip ANSI escape sequences and non-printable control characters from terminal output
const ansiRe = /\x1b(?:\[[0-9;?]*[a-zA-Z~^$]|\].*?(?:\x07|\x1b\\)|[()#][A-Z0-9]?|[a-zA-Z])/g;
const controlRe = /[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]/g;
const multiSpaceRe = / {2,}/g;
const stripAnsi = (s: string): string => s.replace(ansiRe, " ").replace(controlRe, "").replace(multiSpaceRe, " ");

type BackgroundLoopView = {
    id: string;
    slot_kind: string;   // "coding", "scheduled", "auto"
    description: string;
    iteration: number;
    max_iter: number;
    status: string;      // "running", "paused", "completed", "failed"
    session_id: string;
    started_at: string;
    queued_count: number;
};

type Props = {
    remoteSessions: RemoteSessionView[];
    remoteInputDrafts: Record<string, string>;
    setRemoteInputDrafts: Dispatch<SetStateAction<Record<string, string>>>;
    interruptRemoteSession: (sessionID: string) => Promise<void>;
    killRemoteSession: (sessionID: string) => Promise<void>;
    refreshSessionsOnly: () => Promise<void>;
    showToastMessage: (message: string, duration?: number) => void;
    translate: (key: string) => string;
    formatText: (key: string, values?: Record<string, string>) => string;
    localizeText: (en: string, zhHans: string, zhHant: string) => string;
    lang: string;
};

const terminalStatuses = TERMINAL_SESSION_STATUSES;

const getPathLeaf = (value?: string) => {
    if (!value) return "";
    const normalized = value.replace(/\\/g, "/").replace(/\/+$/, "");
    const parts = normalized.split("/").filter(Boolean);
    return parts[parts.length - 1] || "";
};

const getStatusBadge = (status?: string, localizeText?: (en: string, zhHans: string, zhHant: string) => string): { label: string; bg: string; color: string } => {
    const s = String(status || "").toLowerCase();
    if (s === "error" || s === "failed") return { label: status || "error", bg: colors.dangerBg, color: "#9b2c2c" };
    if (s === "waiting_input") return { label: localizeText ? localizeText("Waiting input", "等待输入", "等待輸入") : "等待输入", bg: colors.warningBg, color: colors.warning };
    if (s === "paused") return { label: localizeText ? localizeText("Paused", "已暂停", "已暫停") : "已暂停", bg: colors.warningBg, color: colors.warning };
    if (terminalStatuses.has(s)) return { label: status || "stopped", bg: colors.bg, color: colors.textSecondary };
    return { label: status || "running", bg: "#eef2ff", color: "#4338ca" };
};

const getLaunchSourceTag = (source: string | undefined, localizeText: (en: string, zhHans: string, zhHant: string) => string): { label: string; bg: string; color: string } => {
    if (source === "ai") return { label: "🤖 AI", bg: "#f0e6ff", color: "#6b21a8" };
    if (source === "mobile") return { label: `📱 ${localizeText("Mobile", "手机", "手機")}`, bg: colors.successBg, color: "#276749" };
    if (source === "handoff") return { label: `🔀 ${localizeText("Handoff", "转远程", "轉遠端")}`, bg: "#f3f0ff", color: "#553c9a" };
    return { label: `☁️ ${localizeText("Remote", "远程", "遠端")}`, bg: colors.bg, color: colors.textSecondary };
};

const getSlotKindTag = (kind: string, localizeText: (en: string, zhHans: string, zhHant: string) => string): { icon: string; label: string } => {
    if (kind === "coding") return { icon: "🤖", label: localizeText("Coding", "编程", "編程") };
    if (kind === "scheduled") return { icon: "⏰", label: localizeText("Scheduled", "定时", "定時") };
    if (kind === "auto") return { icon: "🌐", label: localizeText("Auto", "自动", "自動") };
    return { icon: "⚙️", label: kind };
};

const isAISession = (s: RemoteSessionView) => (s.launch_source || "") === "ai";

const isLiveSession = (s: RemoteSessionView) =>
    !terminalStatuses.has(String(s.status || s.summary?.status || "").toLowerCase());

export function RemoteSessionList(props: Props) {
    const {
        remoteSessions,
        remoteInputDrafts,
        setRemoteInputDrafts,
        interruptRemoteSession,
        killRemoteSession,
        refreshSessionsOnly,
        showToastMessage,
        translate,
        formatText,
        localizeText,
        lang,
    } = props;

    const [sessionTab, setSessionTab] = useState<"remote" | "background" | "scheduled" | "passthrough">("remote");
    const [showHistory, setShowHistory] = useState(false);
    const [hiddenSessionIds, setHiddenSessionIds] = useState<string[]>([]);
    const [consoleSessionId, setConsoleSessionId] = useState<string | null>(null);
    const [consoleReadOnly, setConsoleReadOnly] = useState(false);
    const [previewSessionIds, setPreviewSessionIds] = useState<Set<string>>(new Set());
    const [bgLoops, setBgLoops] = useState<BackgroundLoopView[]>([]);
    const [scheduledRefreshKey, setScheduledRefreshKey] = useState(0);
    // SSH/background loop output lines (polled when console is open for a non-remote session)
    const [bgLoopOutputLines, setBgLoopOutputLines] = useState<string[]>([]);

    // Fetch background loops
    const refreshBgLoops = useCallback(async () => {
        try {
            const loops = await ListBackgroundLoops();
            setBgLoops(loops || []);
        } catch { setBgLoops([]); }
    }, []);

    // EventsOn listener + 5s polling fallback
    useEffect(() => {
        refreshBgLoops();
        const cleanup = EventsOn("background-loops-changed", refreshBgLoops);
        const timer = setInterval(refreshBgLoops, 5000);
        return () => {
            if (typeof cleanup === "function") cleanup(); else EventsOff("background-loops-changed");
            clearInterval(timer);
        };
    }, [refreshBgLoops]);

    // Poll SSH/background loop output when the console is open for a session
    // that doesn't exist in remoteSessions (i.e. an SSH background loop).
    const isBgLoopConsole = consoleSessionId != null && !remoteSessions.some((s) => s.id === consoleSessionId);
    useEffect(() => {
        if (!isBgLoopConsole || !consoleSessionId) {
            setBgLoopOutputLines([]);
            return;
        }
        let cancelled = false;
        const poll = async () => {
            try {
                const lines = await GetBackgroundLoopOutput(consoleSessionId);
                if (!cancelled) setBgLoopOutputLines(lines || []);
            } catch { /* ignore */ }
        };
        poll();
        const timer = setInterval(poll, 1000);
        return () => { cancelled = true; clearInterval(timer); };
    }, [isBgLoopConsole, consoleSessionId]);

    // Remote sessions = non-AI sessions
    const remoteSess = useMemo(
        () => remoteSessions.filter((s) => !isAISession(s) && !hiddenSessionIds.includes(s.id)),
        [remoteSessions, hiddenSessionIds],
    );
    // AI sessions for the background tab
    const aiSessions = useMemo(
        () => remoteSessions.filter((s) => isAISession(s) && !hiddenSessionIds.includes(s.id)),
        [remoteSessions, hiddenSessionIds],
    );

    const visibleSessions = sessionTab === "background" ? aiSessions : remoteSess;

    const liveSessions = visibleSessions.filter((s) => {
        const st = String(s.status || s.summary?.status || "").toLowerCase();
        return !terminalStatuses.has(st);
    });
    const historySessions = visibleSessions.filter((s) => {
        const st = String(s.status || s.summary?.status || "").toLowerCase();
        return terminalStatuses.has(st);
    });

    const hideSession = (id: string) => {
        // Remove terminated session from backend so it doesn't reappear on reopen
        DismissRemoteSession(id).catch(() => { /* ignore if still active or not found */ });
        setHiddenSessionIds((prev) => (prev.includes(id) ? prev : [...prev, id]));
        if (consoleSessionId === id) setConsoleSessionId(null);
    };

    const togglePreview = (id: string) => {
        setPreviewSessionIds((prev) => {
            const next = new Set(prev);
            if (next.has(id)) next.delete(id); else next.add(id);
            return next;
        });
    };

    const handleKill = async (id: string) => {
        try {
            await killRemoteSession(id);
            hideSession(id);
            showToastMessage(translate("remoteKillSent"), 2500);
        } catch (err) {
            showToastMessage(formatText("remoteKillFailed", { error: String(err) }), 4000);
        }
    };

    const handleInterrupt = async (id: string) => {
        try {
            await interruptRemoteSession(id);
            showToastMessage(translate("remoteInterruptSent"), 2500);
        } catch (err) {
            showToastMessage(formatText("remoteInterruptFailed", { error: String(err) }), 4000);
        }
    };

    const handleStopLoop = async (loopID: string) => {
        try {
            await StopBackgroundLoop(loopID);
            showToastMessage(localizeText("Background task stopped", "后台任务已停止", "後台任務已停止"), 2500);
            refreshBgLoops();
        } catch (err) {
            showToastMessage(localizeText("Stop failed: {error}", "停止失败: {error}", "停止失敗: {error}").replace("{error}", String(err)), 4000);
        }
    };

    const handleStopAllLoops = async () => {
        try {
            const stopped = await StopAllBackgroundTasks();
            if (stopped > 0) {
                showToastMessage(localizeText("Stopped {count} tasks", "已停止 {count} 个任务", "已停止 {count} 個任務").replace("{count}", String(stopped)), 2500);
            } else {
                showToastMessage(localizeText("No running tasks to stop", "没有运行中的任务可停止", "沒有執行中的任務可停止"), 1500);
            }
            refreshBgLoops();
            refreshSessionsOnly();
        } catch (err) {
            showToastMessage(localizeText("Stop all failed: {error}", "停止全部失败: {error}", "停止全部失敗: {error}").replace("{error}", String(err)), 4000);
        }
    };

    const handleContinueLoop = async (loopID: string) => {
        try {
            await ContinueBackgroundLoop(loopID, 20);
            showToastMessage(localizeText("Extended by +20 rounds", "已续命 +20 轮", "已續命 +20 輪"), 2500);
            refreshBgLoops();
        } catch (err) {
            showToastMessage(localizeText("Extend failed: {error}", "续命失败: {error}", "續命失敗: {error}").replace("{error}", String(err)), 4000);
        }
    };

    const openConsole = (sessionId: string, readOnly: boolean) => {
        setBgLoopOutputLines([]);
        setConsoleSessionId(sessionId);
        setConsoleReadOnly(readOnly);
    };

    const thStyle: React.CSSProperties = {
        padding: "7px 10px",
        fontSize: "0.7rem",
        fontWeight: 600,
        color: colors.textMuted,
        textAlign: "left",
        borderBottom: `2px solid ${colors.border}`,
        whiteSpace: "nowrap",
        userSelect: "none",
    };

    const tdStyle: React.CSSProperties = {
        padding: "8px 10px",
        fontSize: "0.78rem",
        color: colors.text,
        borderBottom: `1px solid ${colors.border}`,
        verticalAlign: "middle",
    };

    const badgeStyle = (bg: string, color: string): React.CSSProperties => ({
        display: "inline-block",
        padding: "1px 8px",
        borderRadius: radius.pill,
        fontSize: "0.68rem",
        fontWeight: 600,
        background: bg,
        color,
        whiteSpace: "nowrap",
    });

    const iconBtnStyle: React.CSSProperties = {
        border: "none",
        background: "transparent",
        cursor: "pointer",
        padding: "3px 6px",
        borderRadius: radius.sm,
        fontSize: "0.82rem",
        lineHeight: 1,
    };

    const renderTable = (sessions: RemoteSessionView[], muted = false, isAITab = false) => (
        <table style={{ width: "100%", borderCollapse: "collapse", tableLayout: "fixed" }}>
            <colgroup>
                <col style={{ width: isAITab ? "20%" : "24%" }} />
                <col style={{ width: isAITab ? "16%" : "18%" }} />
                <col style={{ width: isAITab ? "12%" : "14%" }} />
                <col style={{ width: isAITab ? "12%" : "12%" }} />
                {isAITab && <col style={{ width: "12%" }} />}
                <col style={{ width: isAITab ? "28%" : "32%" }} />
            </colgroup>
            <thead>
                <tr style={{ background: colors.bg }}>
                    <th style={thStyle}>{localizeText("Project", "项目", "專案")}</th>
                    <th style={thStyle}>{localizeText("Tool / session", "工具 / 实例", "工具 / 實例")}</th>
                    <th style={thStyle}>{localizeText("Status", "状态", "狀態")}</th>
                    <th style={thStyle}>{localizeText("Source", "来源", "來源")}</th>
                    {isAITab && <th style={thStyle}>{localizeText("Provider", "服务商", "服務商")}</th>}
                    <th style={{ ...thStyle, textAlign: "right" }}>{localizeText("Actions", "操作", "操作")}</th>
                </tr>
            </thead>
            <tbody>
                {sessions.map((session) => {
                    const projectName = getPathLeaf(session.project_path) || getPathLeaf(session.workspace_root) || getPathLeaf(session.workspace_path) || "-";
                    const statusInfo = getStatusBadge(session.status || session.summary?.status, localizeText);
                    const sourceInfo = getLaunchSourceTag(session.launch_source || session.summary?.source, localizeText);
                    const isTerminal = terminalStatuses.has(String(session.status || session.summary?.status || "").toLowerCase());
                    const rawPreviewLines = session.raw_output_lines || session.preview?.preview_lines || [];
                    const previewLines = rawPreviewLines.map((l) => stripAnsi(l).trimEnd()).filter((l) => l.length > 0);
                    const hasPreview = previewLines.length > 0;
                    const isPreviewOpen = previewSessionIds.has(session.id);

                    return (
                        <React.Fragment key={session.id}>
                            <tr
                                style={{
                                    background: colors.surface,
                                    opacity: muted ? 0.6 : 1,
                                    transition: "background 0.15s",
                                }}
                                onMouseEnter={(e) => { if (!muted) e.currentTarget.style.background = colors.accentBg; }}
                                onMouseLeave={(e) => { if (!muted) e.currentTarget.style.background = colors.surface; }}
                            >
                                <td style={tdStyle}>
                                    <div style={{ fontWeight: 600, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={session.project_path}>
                                        {projectName}
                                    </div>
                                </td>
                                <td style={tdStyle}>
                                    <div style={{ fontWeight: 500, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                                        {session.tool || "-"}
                                    </div>
                                    <div style={{ fontSize: "0.65rem", color: colors.textMuted, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={session.id}>
                                        {session.id.length > 20 ? session.id.slice(0, 18) + "…" : session.id}
                                    </div>
                                </td>
                                <td style={tdStyle}>
                                    <span style={badgeStyle(statusInfo.bg, statusInfo.color)}>{statusInfo.label}</span>
                                </td>
                                <td style={tdStyle}>
                                    <span style={badgeStyle(sourceInfo.bg, sourceInfo.color)}>{sourceInfo.label}</span>
                                </td>
                                {isAITab && (
                                    <td style={tdStyle}>
                                        <div style={{ fontSize: "0.72rem", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={session.provider || ""}>
                                            {session.provider || "-"}
                                        </div>
                                    </td>
                                )}
                                <td style={{ ...tdStyle, textAlign: "right" }}>
                                    <div style={{ display: "inline-flex", gap: "4px", alignItems: "center", flexWrap: "nowrap" }}>
                                        <button
                                            style={{ ...iconBtnStyle, color: hasPreview ? colors.primary : colors.textMuted }}
                                            title={isPreviewOpen ? localizeText("Collapse preview", "收起预览", "收起預覽") : localizeText("Expand preview", "展开预览", "展開預覽")}
                                            onClick={() => togglePreview(session.id)}
                                        >
                                            {isPreviewOpen ? "▼" : "▶"}
                                        </button>
                                        {!isTerminal && (
                                            <>
                                                <button
                                                    style={{ ...iconBtnStyle, color: colors.primary }}
                                                    title={isAITab ? localizeText("View terminal", "查看终端", "查看終端") : localizeText("Open console", "打开控制台", "打開控制台")}
                                                    onClick={() => openConsole(session.id, isAITab)}
                                                >
                                                    🖥
                                                </button>
                                                {!isAITab && (
                                                    <button
                                                        style={{ ...iconBtnStyle, color: colors.warning }}
                                                        title={localizeText("Interrupt session", "中断实例", "中斷實例")}
                                                        onClick={() => handleInterrupt(session.id)}
                                                    >
                                                        ⏸
                                                    </button>
                                                )}
                                            </>
                                        )}
                                        {!isAITab && (
                                            <button
                                                style={{ ...iconBtnStyle, color: colors.danger }}
                                                title={isTerminal ? localizeText("Remove", "移除", "移除") : localizeText("Stop session", "停止实例", "停止實例")}
                                                onClick={() => isTerminal ? hideSession(session.id) : handleKill(session.id)}
                                            >
                                                {isTerminal ? "✕" : "⏹"}
                                            </button>
                                        )}
                                        {isAITab && isTerminal && (
                                            <button
                                                style={{ ...iconBtnStyle, color: colors.textMuted }}
                                                title={localizeText("Remove", "移除", "移除")}
                                                onClick={() => hideSession(session.id)}
                                            >
                                                ✕
                                            </button>
                                        )}
                                    </div>
                                </td>
                            </tr>
                            {/* Inline preview row */}
                            {isPreviewOpen && (
                                <tr>
                                    <td
                                        colSpan={isAITab ? 6 : 5}
                                        style={{
                                            padding: 0,
                                            borderBottom: `1px solid ${colors.border}`,
                                        }}
                                    >
                                        <div
                                            style={{
                                                cursor: "pointer",
                                                background: "#1e1e1e",
                                                transition: "background 0.15s",
                                            }}
                                            onClick={() => openConsole(session.id, isAITab)}
                                            title={localizeText("Click to open fullscreen terminal", "点击打开全屏终端", "點擊開啟全螢幕終端")}
                                            onMouseEnter={(e) => { e.currentTarget.style.background = "#252526"; }}
                                            onMouseLeave={(e) => { e.currentTarget.style.background = "#1e1e1e"; }}
                                        >
                                            <div style={{
                                                display: "flex", alignItems: "center", gap: "8px",
                                                padding: "4px 12px", background: "#2d2d2d",
                                                borderBottom: "1px solid #3a3a3a",
                                            }}>
                                                <span style={{ width: 8, height: 8, borderRadius: "50%", background: "#ff5f57", display: "inline-block" }} />
                                                <span style={{ width: 8, height: 8, borderRadius: "50%", background: "#febc2e", display: "inline-block" }} />
                                                <span style={{ width: 8, height: 8, borderRadius: "50%", background: "#28c840", display: "inline-block" }} />
                                                <span style={{ flex: 1, textAlign: "center", fontSize: "0.65rem", color: "#888", fontFamily: "monospace" }}>
                                                    {session.tool || "terminal"} — {previewLines.length} {localizeText("lines", "行", "行")}
                                                </span>
                                                <span style={{ fontSize: "0.65rem", color: "#6a9955", fontFamily: "monospace", flexShrink: 0 }}>
                                                    ⛶ {localizeText("Click fullscreen", "点击全屏", "點擊全螢幕")}
                                                </span>
                                            </div>
                                            <div style={{
                                                padding: "6px 12px",
                                                maxHeight: "180px",
                                                overflowY: "auto",
                                                fontSize: "0.72rem",
                                                fontFamily: "Consolas, 'Courier New', monospace",
                                                color: "#d4d4d4",
                                                lineHeight: 1.5,
                                                textAlign: "left",
                                            }}>
                                                {previewLines.length === 0 ? (
                                                    <span style={{ color: "#555" }}>$ _</span>
                                                ) : (
                                                    previewLines.slice(-12).map((line, i) => (
                                                        <div key={i} style={{ minHeight: "1.2em" }}>
                                                            {line || "\u00A0"}
                                                        </div>
                                                    ))
                                                )}
                                            </div>
                                        </div>
                                    </td>
                                </tr>
                            )}
                        </React.Fragment>
                    );
                })}
            </tbody>
        </table>
    );

    const renderAgentLoops = () => {
        if (bgLoops.length === 0) return null;
        return (
            <div style={{ marginBottom: "8px" }}>
                <div style={{ padding: "8px 14px 4px", fontSize: "0.72rem", color: colors.textMuted, fontWeight: 600 }}>
                    {localizeText("Agent Loop tasks", "Agent Loop 任务", "Agent Loop 任務")}
                </div>
                <table style={{ width: "100%", borderCollapse: "collapse", tableLayout: "fixed" }}>
                    <colgroup>
                        <col style={{ width: "12%" }} />
                        <col style={{ width: "30%" }} />
                        <col style={{ width: "18%" }} />
                        <col style={{ width: "14%" }} />
                        <col style={{ width: "26%" }} />
                    </colgroup>
                    <thead>
                        <tr style={{ background: colors.bg }}>
                            <th style={thStyle}>{localizeText("Type", "类型", "類型")}</th>
                            <th style={thStyle}>{localizeText("Description", "描述", "描述")}</th>
                            <th style={thStyle}>{localizeText("Rounds", "轮次", "輪次")}</th>
                            <th style={thStyle}>{localizeText("Status", "状态", "狀態")}</th>
                            <th style={{ ...thStyle, textAlign: "right" }}>{localizeText("Actions", "操作", "操作")}</th>
                        </tr>
                    </thead>
                    <tbody>
                        {bgLoops.map((loop) => {
                            const tag = getSlotKindTag(loop.slot_kind, localizeText);
                            const statusInfo = getStatusBadge(loop.status, localizeText);
                            const isPaused = loop.status === "paused";
                            const hasSess = loop.session_id && loop.session_id.length > 0;
                            return (
                                <tr key={loop.id} style={{ background: colors.surface, transition: "background 0.15s" }}
                                    onMouseEnter={(e) => { e.currentTarget.style.background = colors.accentBg; }}
                                    onMouseLeave={(e) => { e.currentTarget.style.background = colors.surface; }}
                                >
                                    <td style={tdStyle}>
                                        <span style={badgeStyle("#f0e6ff", "#6b21a8")}>{tag.icon} {tag.label}</span>
                                    </td>
                                    <td style={tdStyle}>
                                        <div style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={loop.description}>
                                            {loop.description || loop.id}
                                        </div>
                                        {loop.queued_count > 0 && (
                                            <div style={{ fontSize: "0.65rem", color: colors.textMuted }}>
                                                {localizeText("Queued", "排队", "排隊")}: {loop.queued_count}
                                            </div>
                                        )}
                                    </td>
                                    <td style={tdStyle}>
                                        <span style={{ fontSize: "0.75rem", fontFamily: "monospace" }}>
                                            {loop.iteration}/{loop.max_iter}
                                        </span>
                                    </td>
                                    <td style={tdStyle}>
                                        <span style={badgeStyle(statusInfo.bg, statusInfo.color)}>{statusInfo.label}</span>
                                    </td>
                                    <td style={{ ...tdStyle, textAlign: "right" }}>
                                        <div style={{ display: "inline-flex", gap: "4px", alignItems: "center", flexWrap: "nowrap" }}>
                                            {hasSess && (
                                                <button
                                                    style={{ ...iconBtnStyle, color: colors.primary }}
                                                    title={localizeText("View terminal", "查看终端", "查看終端")}
                                                    onClick={() => openConsole(loop.session_id, true)}
                                                >
                                                    🖥
                                                </button>
                                            )}
                                            {isPaused && (
                                                <button
                                                    style={{ ...iconBtnStyle, color: "#16a34a", fontWeight: 600, fontSize: "0.72rem" }}
                                                    title={localizeText("Extend by +20 rounds", "续命 +20 轮", "續命 +20 輪")}
                                                    onClick={() => handleContinueLoop(loop.id)}
                                                >
                                                    ▶ {localizeText("Extend", "续命", "續命")}
                                                </button>
                                            )}
                                            <button
                                                style={{ ...iconBtnStyle, color: colors.danger }}
                                                title={localizeText("Stop", "停止", "停止")}
                                                onClick={() => handleStopLoop(loop.id)}
                                            >
                                                ⏹
                                            </button>
                                        </div>
                                    </td>
                                </tr>
                            );
                        })}
                    </tbody>
                </table>
            </div>
        );
    };

    const isBackgroundTab = sessionTab === "background";
    const isScheduledTab = sessionTab === "scheduled";
    const isPassthroughTab = sessionTab === "passthrough";
    const isRemoteTab = sessionTab === "remote";
    const remoteLiveCount = useMemo(() => remoteSess.filter(isLiveSession).length, [remoteSess]);
    const bgTotalCount = countActiveBackgroundLoops(bgLoops) + aiSessions.filter(isLiveSession).length;

    const openScheduledTab = () => {
        setSessionTab("scheduled");
        setShowHistory(false);
        setScheduledRefreshKey((key) => key + 1);
    };

    return (
        <div style={{ border: `1px solid ${colors.border}`, borderRadius: radius.lg, background: colors.surface, overflow: "hidden" }}>
            {/* Header with tabs */}
            <div style={{ padding: "12px 14px 0" }}>
                <div style={{ display: "flex", alignItems: "center", gap: "0", borderBottom: `1px solid ${colors.border}` }}>
                    <button
                        onClick={() => { setSessionTab("remote"); setShowHistory(false); }}
                        style={{
                            border: "none",
                            background: sessionTab === "remote" ? colors.surface : "transparent",
                            borderBottom: sessionTab === "remote" ? `2px solid ${colors.primary}` : "2px solid transparent",
                            padding: "8px 16px",
                            fontSize: "0.8rem",
                            fontWeight: sessionTab === "remote" ? 700 : 500,
                            color: sessionTab === "remote" ? colors.primary : colors.textMuted,
                            cursor: "pointer",
                            transition: "all 0.15s",
                        }}
                    >
                        ☁️ {localizeText("Remote", "远程", "遠端")}
                        {remoteLiveCount > 0 && (
                            <span style={{ marginLeft: "6px", fontSize: "0.68rem", background: "#eef2ff", color: "#4338ca", padding: "1px 6px", borderRadius: "999px" }}>
                                {remoteLiveCount}
                            </span>
                        )}
                    </button>
                    <button
                        onClick={() => { setSessionTab("background"); setShowHistory(false); }}
                        style={{
                            border: "none",
                            background: sessionTab === "background" ? colors.surface : "transparent",
                            borderBottom: sessionTab === "background" ? `2px solid ${colors.primaryDark}` : "2px solid transparent",
                            padding: "8px 16px",
                            fontSize: "0.8rem",
                            fontWeight: sessionTab === "background" ? 700 : 500,
                            color: sessionTab === "background" ? colors.primaryDark : colors.textMuted,
                            cursor: "pointer",
                            transition: "all 0.15s",
                        }}
                    >
                        ⚙️ {localizeText("Background", "后台", "後台")}
                        {bgTotalCount > 0 && (
                            <span style={{ marginLeft: "6px", fontSize: "0.68rem", background: colors.surfaceMuted, color: colors.primaryDark, padding: "1px 6px", borderRadius: "999px" }}>
                                {bgTotalCount}
                            </span>
                        )}
                    </button>
                    <button
                        onClick={openScheduledTab}
                        style={{
                            border: "none",
                            background: sessionTab === "scheduled" ? colors.surface : "transparent",
                            borderBottom: sessionTab === "scheduled" ? `2px solid ${colors.success}` : "2px solid transparent",
                            padding: "8px 16px",
                            fontSize: "0.8rem",
                            fontWeight: sessionTab === "scheduled" ? 700 : 500,
                            color: sessionTab === "scheduled" ? colors.success : colors.textMuted,
                            cursor: "pointer",
                            transition: "all 0.15s",
                        }}
                    >
                        ⏰ {localizeText("Scheduled", "计划任务", "計劃任務")}
                    </button>
                    <button
                        onClick={() => { setSessionTab("passthrough"); setShowHistory(false); }}
                        style={{
                            border: "none",
                            background: sessionTab === "passthrough" ? colors.surface : "transparent",
                            borderBottom: sessionTab === "passthrough" ? `2px solid ${colors.warning}` : "2px solid transparent",
                            padding: "8px 16px",
                            fontSize: "0.8rem",
                            fontWeight: sessionTab === "passthrough" ? 700 : 500,
                            color: sessionTab === "passthrough" ? colors.warning : colors.textMuted,
                            cursor: "pointer",
                            transition: "all 0.15s",
                        }}
                    >
                        {localizeText("Passthrough Tasks", "直通任务", "直通任務")}
                    </button>
                    <div style={{ flex: 1 }} />
                    {isBackgroundTab && bgTotalCount > 0 && (
                        <button
                            className="btn-link"
                            style={{ fontSize: "0.72rem", marginBottom: "4px", color: colors.danger }}
                            onClick={handleStopAllLoops}
                        >
                            {localizeText("Stop all ({count})", "停止全部 ({count})", "停止全部 ({count})").replace("{count}", String(bgTotalCount))}
                        </button>
                    )}
                    {isRemoteTab && historySessions.length > 0 && (
                        <button
                            className="btn-link"
                            style={{ fontSize: "0.72rem", marginBottom: "4px" }}
                            onClick={() => setShowHistory((v) => !v)}
                        >
                            {showHistory ? localizeText("Hide history", "隐藏历史", "隱藏歷史") : localizeText("View history ({count})", "查看历史 ({count})", "查看歷史 ({count})").replace("{count}", String(historySessions.length))}
                        </button>
                    )}
                </div>
            </div>

            {/* Remote tab content */}
            {isRemoteTab && (
                <>
                    {liveSessions.length === 0 && !showHistory ? (
                        <div style={{ padding: "20px 14px", textAlign: "center", fontSize: "0.76rem", color: colors.textMuted }}>
                            {localizeText("No running remote sessions", "当前没有运行中的远程实例", "目前沒有執行中的遠端實例")}
                        </div>
                    ) : (
                        liveSessions.length > 0 && renderTable(liveSessions, false, false)
                    )}
                    {showHistory && historySessions.length > 0 && (
                        <div style={{ borderTop: `1px solid ${colors.border}` }}>
                            <div style={{ padding: "8px 14px 4px", fontSize: "0.72rem", color: colors.textMuted, fontWeight: 500 }}>
                                {localizeText("Ended", "已结束", "已結束")}
                            </div>
                            {renderTable(historySessions, true, false)}
                        </div>
                    )}
                </>
            )}

            {/* Background tab content */}
            {isBackgroundTab && (
                <>
                    {/* Agent Loop section */}
                    {renderAgentLoops()}

                    {/* AI coding sessions section */}
                    <div>
                        <div style={{ padding: "8px 14px 4px", fontSize: "0.72rem", color: colors.textMuted, fontWeight: 600 }}>
                            {localizeText("AI coding sessions", "AI 编程会话", "AI 編程會話")}
                        </div>
                        {aiSessions.length === 0 && bgLoops.length === 0 ? (
                            <div style={{ padding: "20px 14px", textAlign: "center", fontSize: "0.76rem", color: colors.textMuted }}>
                                {localizeText("No running background tasks", "当前没有运行中的后台任务", "目前沒有執行中的後台任務")}
                            </div>
                        ) : aiSessions.length === 0 ? (
                            <div style={{ padding: "10px 14px", textAlign: "center", fontSize: "0.74rem", color: colors.textMuted }}>
                                {localizeText("No AI coding sessions", "暂无 AI 编程会话", "暫無 AI 編程會話")}
                            </div>
                        ) : (
                            renderTable(aiSessions, false, true)
                        )}
                    </div>
                </>
            )}

            {/* Scheduled tab content */}
            {isScheduledTab && (
                <div style={{ padding: "8px 14px" }}>
                    <ScheduledTasksPanel lang={lang} refreshKey={scheduledRefreshKey} />
                </div>
            )}

            {isPassthroughTab && (
                <div style={{ padding: "8px 14px" }}>
                    <PassthroughCommandsPanel lang={lang} />
                </div>
            )}

            {/* Console modal */}
            {consoleSessionId && (() => {
                let session = remoteSessions.find((s) => s.id === consoleSessionId);
                // Fallback: if the session isn't in remoteSessions (e.g. background
                // loop whose session hasn't synced yet), build a minimal view from
                // the bgLoops data so the console can still open.
                if (!session) {
                    const loop = bgLoops.find((l) => l.session_id === consoleSessionId);
                    if (!loop) return null;
                    session = {
                        id: loop.session_id,
                        tool: loop.slot_kind || "ssh",
                        title: loop.description || `Agent Loop ${loop.id}`,
                        project_path: "",
                        status: loop.status === "running" ? "running" : loop.status,
                        execution_mode: "sdk",
                        raw_output_lines: bgLoopOutputLines,
                    };
                }
                return (
                    <RemoteSessionConsole
                        session={session}
                        remoteInputDrafts={remoteInputDrafts}
                        setRemoteInputDrafts={setRemoteInputDrafts}
                        killRemoteSession={killRemoteSession}
                        refreshSessionsOnly={refreshSessionsOnly}
                        onClose={() => setConsoleSessionId(null)}
                        readOnly={consoleReadOnly || isAISession(session)}
                    />
                );
            })()}
        </div>
    );
}
