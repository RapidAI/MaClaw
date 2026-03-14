import { useMemo, useState, type Dispatch, type SetStateAction } from "react";
import { colors, radius } from "./styles";
import { TERMINAL_SESSION_STATUSES, type RemoteSessionView } from "./types";
import { RemoteSessionConsole } from "./RemoteSessionConsole";

type Props = {
    remoteSessions: RemoteSessionView[];
    remoteInputDrafts: Record<string, string>;
    setRemoteInputDrafts: Dispatch<SetStateAction<Record<string, string>>>;
    sendRemoteInput: (sessionID: string) => Promise<boolean>;
    interruptRemoteSession: (sessionID: string) => Promise<void>;
    killRemoteSession: (sessionID: string) => Promise<void>;
    refreshSessionsOnly: () => Promise<void>;
    showToastMessage: (message: string, duration?: number) => void;
    translate: (key: string) => string;
    formatText: (key: string, values?: Record<string, string>) => string;
    localizeText: (en: string, zhHans: string, zhHant: string) => string;
};

const terminalStatuses = TERMINAL_SESSION_STATUSES;

const getPathLeaf = (value?: string) => {
    if (!value) return "";
    const normalized = value.replace(/\\/g, "/").replace(/\/+$/, "");
    const parts = normalized.split("/").filter(Boolean);
    return parts[parts.length - 1] || "";
};

const getStatusBadge = (status?: string): { label: string; bg: string; color: string } => {
    const s = String(status || "").toLowerCase();
    if (s === "error" || s === "failed") return { label: status || "error", bg: colors.dangerBg, color: "#9b2c2c" };
    if (s === "waiting_input") return { label: "等待输入", bg: colors.warningBg, color: colors.warning };
    if (terminalStatuses.has(s)) return { label: status || "stopped", bg: colors.bg, color: colors.textSecondary };
    return { label: status || "running", bg: "#eef2ff", color: "#4338ca" };
};

const getLaunchSourceTag = (source?: string): { label: string; bg: string; color: string } => {
    if (source === "mobile") return { label: "📱 手机", bg: colors.successBg, color: "#276749" };
    if (source === "handoff") return { label: "🔀 转远程", bg: "#f3f0ff", color: "#553c9a" };
    return { label: "☁️ 远程", bg: colors.bg, color: colors.textSecondary };
};

export function RemoteSessionList(props: Props) {
    const {
        remoteSessions,
        remoteInputDrafts,
        setRemoteInputDrafts,
        sendRemoteInput,
        interruptRemoteSession,
        killRemoteSession,
        refreshSessionsOnly,
        showToastMessage,
        translate,
        formatText,
    } = props;

    const [showHistory, setShowHistory] = useState(false);
    const [hiddenSessionIds, setHiddenSessionIds] = useState<string[]>([]);
    const [consoleSessionId, setConsoleSessionId] = useState<string | null>(null);

    const visibleSessions = useMemo(
        () => remoteSessions.filter((s) => !hiddenSessionIds.includes(s.id)),
        [remoteSessions, hiddenSessionIds],
    );

    const liveSessions = visibleSessions.filter((s) => {
        const st = String(s.status || s.summary?.status || "").toLowerCase();
        return !terminalStatuses.has(st);
    });
    const historySessions = visibleSessions.filter((s) => {
        const st = String(s.status || s.summary?.status || "").toLowerCase();
        return terminalStatuses.has(st);
    });

    const hideSession = (id: string) => {
        setHiddenSessionIds((prev) => (prev.includes(id) ? prev : [...prev, id]));
        if (consoleSessionId === id) setConsoleSessionId(null);
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

    const renderTable = (sessions: RemoteSessionView[], muted = false) => (
        <table style={{ width: "100%", borderCollapse: "collapse", tableLayout: "fixed" }}>
            <colgroup>
                <col style={{ width: "24%" }} />
                <col style={{ width: "18%" }} />
                <col style={{ width: "14%" }} />
                <col style={{ width: "12%" }} />
                <col style={{ width: "32%" }} />
            </colgroup>
            <thead>
                <tr style={{ background: colors.bg }}>
                    <th style={thStyle}>项目</th>
                    <th style={thStyle}>工具 / 实例</th>
                    <th style={thStyle}>状态</th>
                    <th style={thStyle}>来源</th>
                    <th style={{ ...thStyle, textAlign: "right" }}>操作</th>
                </tr>
            </thead>
            <tbody>
                {sessions.map((session) => {
                    const projectName = getPathLeaf(session.project_path) || getPathLeaf(session.workspace_root) || getPathLeaf(session.workspace_path) || "-";
                    const statusInfo = getStatusBadge(session.status || session.summary?.status);
                    const sourceInfo = getLaunchSourceTag(session.launch_source || session.summary?.source);
                    const isTerminal = terminalStatuses.has(String(session.status || session.summary?.status || "").toLowerCase());

                    return (
                        <tr
                            key={session.id}
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
                            <td style={{ ...tdStyle, textAlign: "right" }}>
                                <div style={{ display: "inline-flex", gap: "4px", alignItems: "center", flexWrap: "nowrap" }}>
                                    {!isTerminal && (
                                        <>
                                            <button
                                                style={{ ...iconBtnStyle, color: colors.primary }}
                                                title="打开控制台"
                                                onClick={() => setConsoleSessionId(session.id)}
                                            >
                                                🖥
                                            </button>
                                            <button
                                                style={{ ...iconBtnStyle, color: colors.warning }}
                                                title="中断实例"
                                                onClick={() => handleInterrupt(session.id)}
                                            >
                                                ⏸
                                            </button>
                                        </>
                                    )}
                                    <button
                                        style={{ ...iconBtnStyle, color: colors.danger }}
                                        title={isTerminal ? "移除" : "停止实例"}
                                        onClick={() => isTerminal ? hideSession(session.id) : handleKill(session.id)}
                                    >
                                        {isTerminal ? "✕" : "⏹"}
                                    </button>
                                </div>
                            </td>
                        </tr>
                    );
                })}
            </tbody>
        </table>
    );

    return (
        <div style={{ border: `1px solid ${colors.border}`, borderRadius: radius.lg, background: colors.surface, overflow: "hidden" }}>
            {/* Header */}
            <div style={{ padding: "12px 14px 8px", display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                <div style={{ fontSize: "0.84rem", fontWeight: 600, color: colors.text }}>
                    远程实例管理
                    <span style={{ fontSize: "0.72rem", fontWeight: 400, color: colors.textMuted, marginLeft: "8px" }}>
                        运行 {liveSessions.length} · 历史 {historySessions.length}
                    </span>
                </div>
                {historySessions.length > 0 && (
                    <button
                        className="btn-link"
                        style={{ fontSize: "0.72rem" }}
                        onClick={() => setShowHistory((v) => !v)}
                    >
                        {showHistory ? "隐藏历史" : `查看历史 (${historySessions.length})`}
                    </button>
                )}
            </div>

            {/* Live sessions */}
            {liveSessions.length === 0 && !showHistory ? (
                <div style={{ padding: "20px 14px", textAlign: "center", fontSize: "0.76rem", color: colors.textMuted }}>
                    当前没有运行中的远程实例
                </div>
            ) : (
                liveSessions.length > 0 && renderTable(liveSessions)
            )}

            {/* History sessions */}
            {showHistory && historySessions.length > 0 && (
                <div style={{ borderTop: `1px solid ${colors.border}` }}>
                    <div style={{ padding: "8px 14px 4px", fontSize: "0.72rem", color: colors.textMuted, fontWeight: 500 }}>
                        已结束
                    </div>
                    {renderTable(historySessions, true)}
                </div>
            )}

            {/* Console modal */}
            {consoleSessionId && (() => {
                const session = remoteSessions.find((s) => s.id === consoleSessionId);
                if (!session) return null;
                return (
                    <RemoteSessionConsole
                        session={session}
                        remoteInputDrafts={remoteInputDrafts}
                        setRemoteInputDrafts={setRemoteInputDrafts}
                        sendRemoteInput={sendRemoteInput}
                        interruptRemoteSession={interruptRemoteSession}
                        killRemoteSession={killRemoteSession}
                        refreshSessionsOnly={refreshSessionsOnly}
                        showToastMessage={showToastMessage}
                        translate={translate}
                        formatText={formatText}
                        onClose={() => setConsoleSessionId(null)}
                    />
                );
            })()}
        </div>
    );
}
