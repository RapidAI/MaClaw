import type { Dispatch, SetStateAction } from "react";
import type { RemoteSessionView } from "./types";
import {
    colors,
    radius,
    remoteSubLabelStyle,
    remoteInfoCardStyle,
    remoteSidePanelStyle,
} from "./styles";

type Props = {
    session: RemoteSessionView;
    remoteInputDrafts: Record<string, string>;
    setRemoteInputDrafts: Dispatch<SetStateAction<Record<string, string>>>;
    sendRemoteInput: (sessionID: string) => void;
    interruptRemoteSession: (sessionID: string) => Promise<void>;
    killRemoteSession: (sessionID: string) => Promise<void>;
    showToastMessage: (message: string, duration?: number) => void;
    translate: (key: string) => string;
    formatText: (key: string, values?: Record<string, string>) => string;
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
    } = props;

    const launchSource = session.launch_source || session.summary?.source || "desktop";
    const launchSourceLabel = getLaunchSourceLabel(launchSource);
    const statusText = session.status || session.summary?.status || translate("remoteStatusUnknown");
    const statusStyle = getStatusStyle(statusText);
    const sourceStyle = getLaunchSourceStyle(launchSource);
    const currentTask = session.summary?.current_task || "-";
    const lastResult = session.summary?.last_result || "-";
    const progressSummary = session.summary?.progress_summary || "-";
    const displayTitle = getDisplayTitle(session);

    return (
        <div
            style={{
                border: `1px solid ${colors.border}`,
                borderRadius: radius.lg,
                background: colors.surface,
                overflow: "hidden",
            }}
        >
            <div
                style={{
                    display: "grid",
                    gridTemplateColumns: "minmax(0, 2.2fr) minmax(220px, 1fr)",
                }}
            >
                <div style={{ padding: "10px 12px", minWidth: 0 }}>
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
                            <span
                                style={{
                                    display: "inline-flex",
                                    alignItems: "center",
                                    padding: "2px 8px",
                                    borderRadius: radius.pill,
                                    fontSize: "0.7rem",
                                    fontWeight: 600,
                                    background: sourceStyle.background,
                                    color: sourceStyle.color,
                                }}
                            >
                                {launchSourceLabel}
                            </span>
                        </div>

                        <div>
                            <div style={remoteSubLabelStyle}>状态</div>
                            <span
                                style={{
                                    display: "inline-flex",
                                    alignItems: "center",
                                    padding: "2px 8px",
                                    borderRadius: radius.pill,
                                    fontSize: "0.7rem",
                                    fontWeight: 600,
                                    background: statusStyle.background,
                                    color: statusStyle.color,
                                }}
                            >
                                {statusText}
                            </span>
                        </div>

                        <div style={{ minWidth: 0 }}>
                            <div style={remoteSubLabelStyle}>项目与工具</div>
                            <div style={{ fontSize: "0.74rem", color: colors.text, lineHeight: 1.4, wordBreak: "break-word" }}>{session.project_path || "-"}</div>
                            <div style={{ fontSize: "0.68rem", color: colors.textSecondary, marginTop: "2px" }}>工具: {session.tool || "-"}</div>
                        </div>
                    </div>

                    <div
                        style={{
                            display: "grid",
                            gridTemplateColumns: "repeat(3, minmax(0, 1fr))",
                            gap: "6px",
                            marginTop: "8px",
                        }}
                    >
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
                </div>

                <div style={remoteSidePanelStyle}>
                    <div>
                        <div style={{ ...remoteSubLabelStyle, marginBottom: "6px" }}>操作</div>
                        <div style={{ display: "flex", flexDirection: "column", gap: "5px" }}>
                            <button className="btn-primary" onClick={() => sendRemoteInput(session.id)}>
                                发送指令
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
                            placeholder="输入要发送给远程实例的指令"
                        />
                    </div>
                </div>
            </div>
        </div>
    );
}
