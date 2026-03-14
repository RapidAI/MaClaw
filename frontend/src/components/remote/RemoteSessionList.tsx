import { useMemo, useState, type Dispatch, type SetStateAction } from "react";
import { RemoteSessionCard } from "./RemoteSessionCard";
import { remoteBodyTextStyle, remoteCardStyle, remoteMutedCardStyle, remotePanelGridStyle, remoteSectionTitleStyle } from "./styles";
import type { RemoteSessionView } from "./types";

type Props = {
    remoteSessions: RemoteSessionView[];
    remoteInputDrafts: Record<string, string>;
    setRemoteInputDrafts: Dispatch<SetStateAction<Record<string, string>>>;
    sendRemoteInput: (sessionID: string) => void;
    interruptRemoteSession: (sessionID: string) => Promise<void>;
    killRemoteSession: (sessionID: string) => Promise<void>;
    showToastMessage: (message: string, duration?: number) => void;
    translate: (key: string) => string;
    formatText: (key: string, values?: Record<string, string>) => string;
    localizeText: (en: string, zhHans: string, zhHant: string) => string;
};

const terminalStatuses = new Set(["stopped", "finished", "failed", "killed", "exited", "closed", "done", "completed", "terminated"]);

export function RemoteSessionList(props: Props) {
    const {
        remoteSessions,
        remoteInputDrafts,
        setRemoteInputDrafts,
        sendRemoteInput,
        interruptRemoteSession,
        killRemoteSession,
        showToastMessage,
        translate,
        formatText,
    } = props;

    const [showHistory, setShowHistory] = useState(false);
    const [hiddenSessionIds, setHiddenSessionIds] = useState<string[]>([]);

    const visibleSessions = useMemo(
        () => remoteSessions.filter((session) => !hiddenSessionIds.includes(session.id)),
        [remoteSessions, hiddenSessionIds],
    );

    const attentionSessions = visibleSessions.filter((session) => {
        const status = String(session.status || session.summary?.status || "").toLowerCase();
        return Boolean(session.summary?.waiting_for_user) || status === "waiting_input" || status === "error" || status === "failed";
    });
    const historySessions = visibleSessions.filter((session) => {
        const status = String(session.status || session.summary?.status || "").toLowerCase();
        return terminalStatuses.has(status);
    });
    const liveSessions = visibleSessions.filter((session) => {
        const status = String(session.status || session.summary?.status || "").toLowerCase();
        return !terminalStatuses.has(status);
    });
    const runningSessions = liveSessions.filter((session) => !attentionSessions.some((item) => item.id === session.id));

    const instanceTypeStats = visibleSessions.reduce((acc, session) => {
        const source = session.launch_source || session.summary?.source || "desktop";
        if (source === "mobile") acc.mobile += 1;
        else if (source === "handoff") acc.handoff += 1;
        else acc.remote += 1;
        return acc;
    }, { remote: 0, handoff: 0, mobile: 0 });

    const hideSession = (sessionID: string) => {
        setHiddenSessionIds((prev) => (prev.includes(sessionID) ? prev : [...prev, sessionID]));
    };

    const handleKillSession = async (sessionID: string) => {
        await killRemoteSession(sessionID);
        hideSession(sessionID);
    };

    const handleInterruptSession = async (sessionID: string) => {
        await interruptRemoteSession(sessionID);
    };

    const renderSessions = (sessions: RemoteSessionView[]) => (
        <div style={{ display: "flex", flexDirection: "column", gap: "10px" }}>
            {sessions.map((session) => (
                <RemoteSessionCard
                    key={session.id}
                    session={session}
                    remoteInputDrafts={remoteInputDrafts}
                    setRemoteInputDrafts={setRemoteInputDrafts}
                    sendRemoteInput={sendRemoteInput}
                    interruptRemoteSession={handleInterruptSession}
                    killRemoteSession={handleKillSession}
                    showToastMessage={showToastMessage}
                    translate={translate}
                    formatText={formatText}
                />
            ))}
        </div>
    );

    return (
        <div style={{ ...remoteCardStyle, marginBottom: "14px" }}>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: "12px", marginBottom: "12px", flexWrap: "wrap" }}>
                <div>
                    <div style={{ ...remoteSectionTitleStyle, marginBottom: 0 }}>远程实例管理</div>
                    <div style={{ fontSize: "0.74rem", color: "#64748b" }}>信息状态集中在左侧，操作按钮集中在右侧。实例停止后会立即从当前列表隐藏。</div>
                </div>
                <div style={remoteBodyTextStyle}>共 {visibleSessions.length} 个实例</div>
            </div>

            <div style={remotePanelGridStyle}>
                <div style={remoteMutedCardStyle}>
                    <div style={{ fontSize: "0.72rem", color: "#64748b", marginBottom: "6px" }}>运行中</div>
                    <div style={{ fontSize: "1rem", fontWeight: 800, color: "#0f172a" }}>{runningSessions.length}</div>
                </div>
                <div style={remoteMutedCardStyle}>
                    <div style={{ fontSize: "0.72rem", color: "#64748b", marginBottom: "6px" }}>需处理</div>
                    <div style={{ fontSize: "1rem", fontWeight: 800, color: attentionSessions.length > 0 ? "#b45309" : "#0f172a" }}>{attentionSessions.length}</div>
                </div>
                <div style={remoteMutedCardStyle}>
                    <div style={{ fontSize: "0.72rem", color: "#64748b", marginBottom: "6px" }}>类型分布</div>
                    <div style={{ fontSize: "0.82rem", fontWeight: 700, color: "#0f172a", lineHeight: 1.6 }}>
                        远程 {instanceTypeStats.remote} / 本地转远程 {instanceTypeStats.handoff} / 手机启动 {instanceTypeStats.mobile}
                    </div>
                </div>
            </div>

            {visibleSessions.length === 0 ? (
                <div style={{ fontSize: "0.78rem", color: "#94a3b8", padding: "8px 0" }}>当前没有远程实例。</div>
            ) : (
                <div style={{ display: "flex", flexDirection: "column", gap: "10px" }}>
                    {attentionSessions.length > 0 ? (
                        <div>
                            <div style={{ ...remoteSectionTitleStyle, fontSize: "0.8rem", marginBottom: "8px", color: "#b45309" }}>需处理实例</div>
                            <div style={{ fontSize: "0.74rem", color: "#7c6f64", marginBottom: "10px" }}>等待输入或报错的实例优先排到上方。</div>
                            {renderSessions(attentionSessions)}
                        </div>
                    ) : null}

                    {runningSessions.length > 0 ? (
                        <div>
                            <div style={{ ...remoteSectionTitleStyle, fontSize: "0.8rem", marginBottom: "8px" }}>运行中实例</div>
                            <div style={{ fontSize: "0.74rem", color: "#7c6f64", marginBottom: "10px" }}>当前仍在运行、可继续查看和控制的实例。</div>
                            {renderSessions(runningSessions)}
                        </div>
                    ) : null}

                    {historySessions.length > 0 ? (
                        <div style={{ borderTop: "1px solid #e5e7eb", paddingTop: "10px" }}>
                            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: "10px", marginBottom: showHistory ? "10px" : 0 }}>
                                <div>
                                    <div style={{ ...remoteSectionTitleStyle, fontSize: "0.8rem", marginBottom: "4px" }}>历史实例</div>
                                    <div style={{ fontSize: "0.74rem", color: "#7c6f64" }}>已结束的实例默认收起，界面会更清爽。</div>
                                </div>
                                <button className="btn-link" onClick={() => setShowHistory((prev) => !prev)}>
                                    {showHistory ? "收起历史" : "展开历史"}
                                </button>
                            </div>
                            {showHistory ? renderSessions(historySessions) : null}
                        </div>
                    ) : null}
                </div>
            )}
        </div>
    );
}
