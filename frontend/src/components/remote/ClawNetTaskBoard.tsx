import { useState, useEffect, useCallback } from "react";
import {
    ClawNetListTasks, ClawNetGetCredits,
    ClawNetBidOnTask, ClawNetSubmitTaskResult, ClawNetApproveTask,
    ClawNetRejectTask, ClawNetCancelTask, ClawNetMatchTasks,
    ClawNetCreateTask,
} from "../../../wailsjs/go/main/App";

type Props = {
    lang: string;
    clawNetRunning: boolean;
};

interface ClawNetTask {
    id: string;
    title: string;
    description?: string;
    status: string;
    reward: number;
    creator?: string;
    assignee?: string;
    created_at?: string;
}

const STATUS_COLORS: Record<string, { bg: string; text: string; label_zh: string; label_en: string }> = {
    open:      { bg: "#ecfdf5", text: "#059669", label_zh: "开放", label_en: "Open" },
    assigned:  { bg: "#eff6ff", text: "#2563eb", label_zh: "已分配", label_en: "Assigned" },
    submitted: { bg: "#fefce8", text: "#ca8a04", label_zh: "已提交", label_en: "Submitted" },
    approved:  { bg: "#f0fdf4", text: "#16a34a", label_zh: "已通过", label_en: "Approved" },
    rejected:  { bg: "#fef2f2", text: "#dc2626", label_zh: "已拒绝", label_en: "Rejected" },
    cancelled: { bg: "#f8fafc", text: "#94a3b8", label_zh: "已取消", label_en: "Cancelled" },
};

type ViewMode = "all" | "matched";

export function ClawNetTaskBoard({ lang, clawNetRunning }: Props) {
    const [tasks, setTasks] = useState<ClawNetTask[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [credits, setCredits] = useState<{ balance: number; tier: string } | null>(null);
    const [viewMode, setViewMode] = useState<ViewMode>("all");
    const [actionBusy, setActionBusy] = useState<string | null>(null);
    const [actionMsg, setActionMsg] = useState("");
    // Create task form
    const [showCreate, setShowCreate] = useState(false);
    const [newTitle, setNewTitle] = useState("");
    const [newReward, setNewReward] = useState(10);

    const zh = lang?.startsWith("zh");

    const refresh = useCallback(async () => {
        if (!clawNetRunning) return;
        setLoading(true);
        setError("");
        try {
            let res: any;
            if (viewMode === "matched") {
                res = await ClawNetMatchTasks();
            } else {
                res = await ClawNetListTasks("");
            }
            if (res.ok) {
                setTasks((res.tasks || []).slice(0, 12));
            } else {
                setError(res.error || "Failed to load tasks");
            }
            const c = await ClawNetGetCredits();
            if (c.ok) setCredits({ balance: c.balance, tier: c.tier });
        } catch (e) {
            setError(String(e));
        } finally {
            setLoading(false);
        }
    }, [clawNetRunning, viewMode]);

    useEffect(() => {
        refresh();
        if (!clawNetRunning) return;
        const timer = setInterval(refresh, 30000);
        return () => clearInterval(timer);
    }, [refresh, clawNetRunning]);

    const doAction = async (label: string, fn: () => Promise<any>) => {
        setActionBusy(label);
        setActionMsg("");
        try {
            const res = await fn();
            if (res.ok) {
                setActionMsg(zh ? "操作成功" : "Success");
                refresh();
            } else {
                setActionMsg(res.error || "Failed");
            }
        } catch (e) {
            setActionMsg(String(e));
        } finally {
            setActionBusy(null);
            setTimeout(() => setActionMsg(""), 3000);
        }
    };

    const handleCreate = async () => {
        if (!newTitle.trim()) return;
        await doAction("create", () => ClawNetCreateTask(newTitle.trim(), newReward));
        setNewTitle("");
        setShowCreate(false);
    };

    if (!clawNetRunning) {
        return (
            <div style={{ padding: "40px 20px", textAlign: "center", color: "#94a3b8" }}>
                <div style={{ fontSize: "3rem", marginBottom: "12px" }}>🦞</div>
                <div style={{ fontSize: "1rem", fontWeight: 600, marginBottom: "6px" }}>
                    {zh ? "虾网未连接" : "ClawNet Not Connected"}
                </div>
                <div style={{ fontSize: "0.82rem", color: "#b0b8c8" }}>
                    {zh ? "请在设置 → 虾网中启用 ClawNet" : "Enable ClawNet in Settings → ClawNet"}
                </div>
            </div>
        );
    }

    const btnStyle = (active?: boolean): React.CSSProperties => ({
        background: active ? "#6366f1" : "none",
        color: active ? "#fff" : "#64748b",
        border: active ? "1px solid #6366f1" : "1px solid #e2e8f0",
        borderRadius: "6px",
        padding: "3px 10px",
        fontSize: "0.72rem",
        cursor: "pointer",
        fontWeight: active ? 600 : 400,
    });

    const smallBtn = (disabled?: boolean): React.CSSProperties => ({
        background: "none",
        border: "1px solid #e2e8f0",
        borderRadius: "4px",
        padding: "2px 8px",
        fontSize: "0.65rem",
        cursor: disabled ? "not-allowed" : "pointer",
        color: disabled ? "#cbd5e1" : "#6366f1",
        opacity: disabled ? 0.5 : 1,
    });

    return (
        <div style={{ padding: "0 15px", width: "100%", boxSizing: "border-box" }}>
            {/* Header */}
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: "12px", flexWrap: "wrap", gap: "6px" }}>
                <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
                    <span style={{ fontSize: "1.2rem" }}>🦞</span>
                    <span style={{ fontSize: "1rem", fontWeight: 700, color: "#1e293b" }}>
                        {zh ? "虾网任务集市" : "ClawNet Task Bazaar"}
                    </span>
                    {credits && (
                        <span style={{
                            fontSize: "0.75rem", background: "#fffbeb", color: "#b45309",
                            padding: "2px 8px", borderRadius: "10px", fontWeight: 500,
                        }}>
                            🐚 {credits.balance ?? 0} {credits.tier && `· ${credits.tier}`}
                        </span>
                    )}
                </div>
                <div style={{ display: "flex", gap: "4px", alignItems: "center" }}>
                    <button style={btnStyle(viewMode === "all")} onClick={() => setViewMode("all")}>
                        {zh ? "全部" : "All"}
                    </button>
                    <button style={btnStyle(viewMode === "matched")} onClick={() => setViewMode("matched")}>
                        {zh ? "匹配" : "Matched"}
                    </button>
                    <button style={btnStyle()} onClick={() => setShowCreate(!showCreate)}>
                        + {zh ? "发布" : "Post"}
                    </button>
                    <button onClick={refresh} disabled={loading} style={btnStyle()}>
                        {loading ? "..." : "↻"}
                    </button>
                </div>
            </div>

            {/* Action feedback */}
            {actionMsg && (
                <div style={{ fontSize: "0.75rem", color: actionMsg === (zh ? "操作成功" : "Success") ? "#16a34a" : "#ef4444", marginBottom: "8px" }}>
                    {actionMsg}
                </div>
            )}

            {/* Create task form */}
            {showCreate && (
                <div style={{ background: "#f8fafc", borderRadius: "8px", padding: "10px 12px", marginBottom: "10px", display: "flex", gap: "8px", alignItems: "center", flexWrap: "wrap" }}>
                    <input
                        value={newTitle}
                        onChange={(e) => setNewTitle(e.target.value)}
                        placeholder={zh ? "任务标题..." : "Task title..."}
                        style={{ flex: 1, minWidth: "120px", border: "1px solid #e2e8f0", borderRadius: "6px", padding: "4px 8px", fontSize: "0.78rem" }}
                    />
                    <div style={{ display: "flex", alignItems: "center", gap: "4px" }}>
                        <span style={{ fontSize: "0.75rem" }}>🐚</span>
                        <input
                            type="number" value={newReward} min={1}
                            onChange={(e) => setNewReward(Number(e.target.value))}
                            style={{ width: "50px", border: "1px solid #e2e8f0", borderRadius: "6px", padding: "4px 6px", fontSize: "0.78rem" }}
                        />
                    </div>
                    <button onClick={handleCreate} disabled={!newTitle.trim() || actionBusy === "create"} style={{ ...smallBtn(!newTitle.trim()), color: "#fff", background: "#6366f1", border: "none", padding: "4px 12px" }}>
                        {zh ? "发布" : "Post"}
                    </button>
                </div>
            )}

            {error && (
                <div style={{ fontSize: "0.78rem", color: "#ef4444", marginBottom: "10px", padding: "6px 10px", background: "#fef2f2", borderRadius: "6px" }}>
                    {error}
                </div>
            )}

            {tasks.length === 0 && !loading && !error && (
                <div style={{ textAlign: "center", color: "#94a3b8", padding: "30px 0", fontSize: "0.85rem" }}>
                    {viewMode === "matched" ? (zh ? "暂无匹配任务" : "No matched tasks") : (zh ? "暂无任务" : "No tasks available")}
                </div>
            )}

            {/* Task cards grid */}
            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(240px, 1fr))", gap: "10px" }}>
                {tasks.map((task) => {
                    const sc = STATUS_COLORS[task.status] || STATUS_COLORS.open;
                    return (
                        <div key={task.id} style={{
                            background: "#fff", border: "1px solid #e2e8f0", borderRadius: "10px",
                            padding: "12px 14px", display: "flex", flexDirection: "column", gap: "6px",
                            transition: "box-shadow 0.2s, border-color 0.2s",
                        }}
                        onMouseEnter={(e) => { e.currentTarget.style.boxShadow = "0 2px 12px rgba(99,102,241,0.10)"; e.currentTarget.style.borderColor = "#c7d2fe"; }}
                        onMouseLeave={(e) => { e.currentTarget.style.boxShadow = "none"; e.currentTarget.style.borderColor = "#e2e8f0"; }}
                        >
                            {/* Title + status */}
                            <div style={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between", gap: "6px" }}>
                                <div style={{ fontSize: "0.82rem", fontWeight: 600, color: "#1e293b", lineHeight: 1.3, flex: 1, overflow: "hidden", textOverflow: "ellipsis", display: "-webkit-box", WebkitLineClamp: 2, WebkitBoxOrient: "vertical" }}>
                                    {task.title}
                                </div>
                                <span style={{ fontSize: "0.65rem", fontWeight: 500, background: sc.bg, color: sc.text, padding: "1px 6px", borderRadius: "8px", flexShrink: 0, whiteSpace: "nowrap" }}>
                                    {zh ? sc.label_zh : sc.label_en}
                                </span>
                            </div>

                            {task.description && (
                                <div style={{ fontSize: "0.72rem", color: "#64748b", lineHeight: 1.4, overflow: "hidden", textOverflow: "ellipsis", display: "-webkit-box", WebkitLineClamp: 2, WebkitBoxOrient: "vertical" }}>
                                    {task.description}
                                </div>
                            )}

                            {/* Reward + creator */}
                            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginTop: "auto" }}>
                                <span style={{ fontSize: "0.75rem", fontWeight: 600, color: "#b45309", display: "flex", alignItems: "center", gap: "3px" }}>
                                    🐚 {task.reward}
                                </span>
                                {task.creator && (
                                    <span style={{ fontSize: "0.65rem", color: "#94a3b8", fontFamily: "monospace" }}>
                                        {task.creator.slice(0, 8)}…
                                    </span>
                                )}
                            </div>

                            {/* Action buttons based on status */}
                            <div style={{ display: "flex", gap: "4px", flexWrap: "wrap", marginTop: "2px" }}>
                                {task.status === "open" && (
                                    <button style={smallBtn(!!actionBusy)} disabled={!!actionBusy}
                                        onClick={() => doAction("bid-" + task.id, () => ClawNetBidOnTask(task.id, 0, zh ? "我可以做" : "I can do this"))}>
                                        {zh ? "竞标" : "Bid"}
                                    </button>
                                )}
                                {task.status === "assigned" && (
                                    <button style={smallBtn(!!actionBusy)} disabled={!!actionBusy}
                                        onClick={() => doAction("submit-" + task.id, () => ClawNetSubmitTaskResult(task.id, ""))}>
                                        {zh ? "提交" : "Submit"}
                                    </button>
                                )}
                                {task.status === "submitted" && (
                                    <>
                                        <button style={smallBtn(!!actionBusy)} disabled={!!actionBusy}
                                            onClick={() => doAction("approve-" + task.id, () => ClawNetApproveTask(task.id))}>
                                            {zh ? "通过" : "Approve"}
                                        </button>
                                        <button style={smallBtn(!!actionBusy)} disabled={!!actionBusy}
                                            onClick={() => doAction("reject-" + task.id, () => ClawNetRejectTask(task.id))}>
                                            {zh ? "拒绝" : "Reject"}
                                        </button>
                                    </>
                                )}
                                {(task.status === "open" || task.status === "assigned") && (
                                    <button style={smallBtn(!!actionBusy)} disabled={!!actionBusy}
                                        onClick={() => doAction("cancel-" + task.id, () => ClawNetCancelTask(task.id))}>
                                        {zh ? "取消" : "Cancel"}
                                    </button>
                                )}
                            </div>

                            <div style={{ fontSize: "0.6rem", color: "#cbd5e1", fontFamily: "monospace" }}>
                                {task.id.slice(0, 12)}
                            </div>
                        </div>
                    );
                })}
            </div>
        </div>
    );
}
