import type { Dispatch, SetStateAction } from "react";
import type { RemoteSessionView } from "./types";

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
    if (value === "error" || value === "failed") return { background: "#fee2e2", color: "#b91c1c" };
    if (value === "waiting_input") return { background: "#fef3c7", color: "#b45309" };
    if (["stopped", "finished", "killed", "closed", "done", "completed", "terminated", "exited"].includes(value)) {
        return { background: "#e5e7eb", color: "#475569" };
    }
    return { background: "#dbeafe", color: "#1d4ed8" };
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
    const currentTask = session.summary?.current_task || "-";
    const lastResult = session.summary?.last_result || "-";
    const progressSummary = session.summary?.progress_summary || "-";
    const displayTitle = getDisplayTitle(session);

    return (
        <div
            style={{
                border: "1px solid rgba(148, 163, 184, 0.2)",
                borderRadius: "14px",
                background: "#ffffff",
                overflow: "hidden",
            }}
        >
            <div
                style={{
                    display: "grid",
                    gridTemplateColumns: "minmax(0, 2.2fr) minmax(260px, 1fr)",
                }}
            >
                <div style={{ padding: "14px 16px", minWidth: 0 }}>
                    <div
                        style={{
                            display: "grid",
                            gridTemplateColumns: "minmax(180px, 1.2fr) minmax(110px, 0.7fr) minmax(100px, 0.7fr) minmax(0, 1.4fr)",
                            gap: "12px",
                            alignItems: "start",
                        }}
                    >
                        <div style={{ minWidth: 0 }}>
                            <div style={{ fontSize: "0.72rem", color: "#94a3b8", marginBottom: "6px" }}>实例</div>
                            <div style={{ fontSize: "0.92rem", fontWeight: 700, color: "#0f172a", marginBottom: "4px", wordBreak: "break-word" }}>
                                {displayTitle}
                            </div>
                            <div style={{ fontSize: "0.72rem", color: "#64748b", wordBreak: "break-word" }}>{session.id}</div>
                        </div>

                        <div>
                            <div style={{ fontSize: "0.72rem", color: "#94a3b8", marginBottom: "6px" }}>类型</div>
                            <span
                                style={{
                                    display: "inline-flex",
                                    alignItems: "center",
                                    padding: "4px 10px",
                                    borderRadius: "999px",
                                    fontSize: "0.74rem",
                                    fontWeight: 700,
                                    background: launchSource === "mobile" ? "#dcfce7" : launchSource === "handoff" ? "#ede9fe" : "#e2e8f0",
                                    color: launchSource === "mobile" ? "#15803d" : launchSource === "handoff" ? "#6d28d9" : "#475569",
                                }}
                            >
                                {launchSourceLabel}
                            </span>
                        </div>

                        <div>
                            <div style={{ fontSize: "0.72rem", color: "#94a3b8", marginBottom: "6px" }}>状态</div>
                            <span
                                style={{
                                    display: "inline-flex",
                                    alignItems: "center",
                                    padding: "4px 10px",
                                    borderRadius: "999px",
                                    fontSize: "0.74rem",
                                    fontWeight: 700,
                                    background: statusStyle.background,
                                    color: statusStyle.color,
                                }}
                            >
                                {statusText}
                            </span>
                        </div>

                        <div style={{ minWidth: 0 }}>
                            <div style={{ fontSize: "0.72rem", color: "#94a3b8", marginBottom: "6px" }}>项目与工具</div>
                            <div style={{ fontSize: "0.8rem", color: "#334155", lineHeight: 1.5, wordBreak: "break-word" }}>{session.project_path || "-"}</div>
                            <div style={{ fontSize: "0.72rem", color: "#64748b", marginTop: "4px" }}>工具: {session.tool || "-"}</div>
                        </div>
                    </div>

                    <div
                        style={{
                            display: "grid",
                            gridTemplateColumns: "repeat(3, minmax(0, 1fr))",
                            gap: "10px",
                            marginTop: "14px",
                        }}
                    >
                        <div style={{ borderRadius: "10px", border: "1px solid #e2e8f0", background: "#f8fafc", padding: "10px 12px" }}>
                            <div style={{ fontSize: "0.72rem", color: "#94a3b8", marginBottom: "6px" }}>当前任务</div>
                            <div style={{ fontSize: "0.8rem", color: "#0f172a", lineHeight: 1.5, wordBreak: "break-word" }}>{currentTask}</div>
                        </div>
                        <div style={{ borderRadius: "10px", border: "1px solid #e2e8f0", background: "#f8fafc", padding: "10px 12px" }}>
                            <div style={{ fontSize: "0.72rem", color: "#94a3b8", marginBottom: "6px" }}>最近结果</div>
                            <div style={{ fontSize: "0.8rem", color: "#334155", lineHeight: 1.5, wordBreak: "break-word" }}>{lastResult}</div>
                        </div>
                        <div style={{ borderRadius: "10px", border: "1px solid #e2e8f0", background: "#f8fafc", padding: "10px 12px" }}>
                            <div style={{ fontSize: "0.72rem", color: "#94a3b8", marginBottom: "6px" }}>进度</div>
                            <div style={{ fontSize: "0.8rem", color: "#334155", lineHeight: 1.5, wordBreak: "break-word" }}>{progressSummary}</div>
                        </div>
                    </div>
                </div>

                <div
                    style={{
                        borderLeft: "1px solid #eef2f7",
                        background: "#fbfdff",
                        padding: "14px 16px",
                        display: "flex",
                        flexDirection: "column",
                        gap: "12px",
                        justifyContent: "space-between",
                    }}
                >
                    <div>
                        <div style={{ fontSize: "0.72rem", color: "#94a3b8", marginBottom: "8px" }}>操作</div>
                        <div style={{ display: "flex", flexDirection: "column", gap: "8px" }}>
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
                                style={{ background: "#fff1f2", color: "#be123c", borderColor: "#fecdd3" }}
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
                        <div style={{ fontSize: "0.72rem", color: "#94a3b8", marginBottom: "8px" }}>快速输入</div>
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
