import { useState, useEffect, useCallback, useRef, useMemo } from "react";
import {
    ClawNetListTasks, ClawNetGetCredits,
    ClawNetBidOnTask, ClawNetSubmitTaskResult, ClawNetApproveTask,
    ClawNetRejectTask, ClawNetCancelTask, ClawNetMatchTasks,
    ClawNetCreateTask, ClawNetBrowseNetworkTasks, ClawNetPublishTasksToHub,
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

const MAX_TASKS_LOCAL = 12;
const MAX_TASKS_TOTAL = 24;
const POLL_INTERVAL_MS = 30_000;

type ViewMode = "all" | "matched" | "network";

export function ClawNetTaskBoard({ lang, clawNetRunning }: Props) {
    const [tasks, setTasks] = useState<ClawNetTask[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [credits, setCredits] = useState<{ balance: number; tier: string } | null>(null);
    const [viewMode, setViewMode] = useState<ViewMode>("all");
    const [actionBusy, setActionBusy] = useState<string | null>(null);
    const [actionMsg, setActionMsg] = useState<{ text: string; type: "success" | "info" | "error" } | null>(null);
    const msgTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const refreshRef = useRef(0); // guard stale responses
    // Create task form
    const [showCreate, setShowCreate] = useState(false);
    const [newTitle, setNewTitle] = useState("");
    const [newReward, setNewReward] = useState(100);

    const zh = lang?.startsWith("zh");

    const refresh = useCallback(async () => {
        if (!clawNetRunning) return;
        const seq = ++refreshRef.current;
        setLoading(true);
        setError("");
        try {
            // Fire primary fetch + credits in parallel
            const primaryFetch = viewMode === "matched"
                ? ClawNetMatchTasks()
                : viewMode === "network"
                    ? ClawNetBrowseNetworkTasks()
                    : ClawNetListTasks("");

            const [res, creditsRes] = await Promise.all([
                primaryFetch,
                ClawNetGetCredits().catch(() => null),
            ]);

            // Stale guard: discard if a newer refresh was triggered
            if (seq !== refreshRef.current) return;

            if (res.ok) {
                let finalTasks: ClawNetTask[] = (res.tasks || []).slice(
                    0, viewMode === "network" ? MAX_TASKS_TOTAL : MAX_TASKS_LOCAL,
                );
                // In "all" mode, also fetch network tasks and merge
                if (viewMode === "all") {
                    try {
                        const netRes = await ClawNetBrowseNetworkTasks();
                        if (seq !== refreshRef.current) return;
                        if (netRes.ok && netRes.tasks?.length) {
                            const localIds = new Set(finalTasks.map((t) => t.id));
                            const netTasks = (netRes.tasks as ClawNetTask[]).filter(t => !localIds.has(t.id));
                            finalTasks = [...finalTasks, ...netTasks].slice(0, MAX_TASKS_TOTAL);
                        }
                    } catch { /* keep local tasks */ }
                }
                setTasks(finalTasks);
            } else {
                setTasks([]);
                setError(res.error || "Failed to load tasks");
            }

            if (creditsRes?.ok) setCredits({ balance: creditsRes.balance, tier: creditsRes.tier });

            // Publish local tasks to Hub in background
            if (viewMode !== "network") {
                ClawNetPublishTasksToHub().catch(() => {});
            }
        } catch (e) {
            if (seq !== refreshRef.current) return;
            setError(String(e));
        } finally {
            if (seq === refreshRef.current) setLoading(false);
        }
    }, [clawNetRunning, viewMode]);

    useEffect(() => {
        refresh();
        if (!clawNetRunning) return;
        const timer = setInterval(refresh, POLL_INTERVAL_MS);
        return () => clearInterval(timer);
    }, [refresh, clawNetRunning]);

    // Cleanup message timer on unmount
    useEffect(() => {
        return () => {
            if (msgTimerRef.current) clearTimeout(msgTimerRef.current);
        };
    }, []);

    const showMsg = useCallback((text: string, type: "success" | "info" | "error", ms = 3000) => {
        if (msgTimerRef.current) clearTimeout(msgTimerRef.current);
        setActionMsg({ text, type });
        msgTimerRef.current = setTimeout(() => setActionMsg(null), ms);
    }, []);

    const doAction = useCallback(async (label: string, fn: () => Promise<any>) => {
        setActionBusy(label);
        setActionMsg(null);
        try {
            const res = await fn();
            if (res.ok) {
                showMsg(zh ? "操作成功" : "Success", "success");
                refresh();
            } else {
                showMsg(res.error || "Failed", "error");
            }
        } catch (e) {
            showMsg(String(e), "error");
        } finally {
            setActionBusy(null);
        }
    }, [zh, showMsg, refresh]);

    const handleCreate = useCallback(async () => {
        if (!newTitle.trim()) return;
        const reward = Math.max(0, Math.floor(newReward));
        if (reward !== 0 && reward < 100) {
            showMsg(zh ? "赏金最低 100 🐚（或 0 表示免费协作）" : "Minimum reward is 100 🐚 (or 0 for free collaboration)", "info", 4000);
            return;
        }
        if (reward > 0 && credits && credits.balance < reward) {
            showMsg(zh ? `余额不足（当前 ${credits.balance} 🐚，需要 ${reward} 🐚）` : `Insufficient balance (have ${credits.balance} 🐚, need ${reward} 🐚)`, "info", 4000);
            return;
        }
        await doAction("create", () => ClawNetCreateTask(newTitle.trim(), reward));
        setNewTitle("");
        setNewReward(100);
        setShowCreate(false);
    }, [newTitle, newReward, credits, zh, showMsg, doAction]);

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

    const btnStyle = useMemo(() => (active?: boolean): React.CSSProperties => ({
        background: active ? "#6366f1" : "none",
        color: active ? "#fff" : "#64748b",
        border: active ? "1px solid #6366f1" : "1px solid #e2e8f0",
        borderRadius: "6px",
        padding: "3px 10px",
        fontSize: "0.72rem",
        cursor: "pointer",
        fontWeight: active ? 600 : 400,
    }), []);

    const smallBtn = useMemo(() => (disabled?: boolean): React.CSSProperties => ({
        background: "none",
        border: "1px solid #e2e8f0",
        borderRadius: "4px",
        padding: "2px 8px",
        fontSize: "0.65rem",
        cursor: disabled ? "not-allowed" : "pointer",
        color: disabled ? "#cbd5e1" : "#6366f1",
        opacity: disabled ? 0.5 : 1,
    }), []);

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
                    <button style={btnStyle(viewMode === "network")} onClick={() => setViewMode("network")}>
                        {zh ? "网络" : "Network"}
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
                <div style={{
                    fontSize: "0.75rem",
                    color: actionMsg.type === "success" ? "#16a34a" : actionMsg.type === "info" ? "#2563eb" : "#ef4444",
                    background: actionMsg.type === "success" ? "#f0fdf4" : actionMsg.type === "info" ? "#eff6ff" : "#fef2f2",
                    padding: "5px 10px",
                    borderRadius: "6px",
                    marginBottom: "8px",
                }}>
                    {actionMsg.type === "info" && "💡 "}{actionMsg.text}
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
                            type="number" value={newReward} min={0}
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
                    {viewMode === "matched" ? (zh ? "暂无匹配任务" : "No matched tasks") : viewMode === "network" ? (zh ? "暂无网络任务（其他节点尚未发布任务到 Hub）" : "No network tasks (peers haven't published to Hub yet)") : (zh ? "暂无任务" : "No tasks available")}
                </div>
            )}

            {/* Task cards – 2 per row */}
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "6px" }}>
                {tasks.map((task) => {
                    const sc = STATUS_COLORS[task.status] || STATUS_COLORS.open;
                    return (
                        <div key={task.id} style={{
                            background: "#fff", border: "1px solid #e2e8f0", borderRadius: "7px",
                            padding: "7px 10px", display: "flex", flexDirection: "column", gap: "3px",
                            transition: "box-shadow 0.15s, border-color 0.15s",
                            minWidth: 0, overflow: "hidden",
                        }}
                        onMouseEnter={(e) => { e.currentTarget.style.boxShadow = "0 1px 8px rgba(99,102,241,0.10)"; e.currentTarget.style.borderColor = "#c7d2fe"; }}
                        onMouseLeave={(e) => { e.currentTarget.style.boxShadow = "none"; e.currentTarget.style.borderColor = "#e2e8f0"; }}
                        >
                            {/* Row 1: title + status */}
                            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: "4px" }}>
                                <div title={task.title} style={{ fontSize: "0.76rem", fontWeight: 600, color: "#1e293b", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", flex: 1, minWidth: 0 }}>
                                    {task.title}
                                </div>
                                <span style={{ fontSize: "0.6rem", fontWeight: 500, background: sc.bg, color: sc.text, padding: "0px 5px", borderRadius: "6px", flexShrink: 0, whiteSpace: "nowrap", lineHeight: "16px" }}>
                                    {zh ? sc.label_zh : sc.label_en}
                                </span>
                            </div>

                            {task.description && (
                                <div title={task.description} style={{ fontSize: "0.68rem", color: "#94a3b8", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                                    {task.description}
                                </div>
                            )}

                            {/* Row 2: reward + actions */}
                            <div style={{ display: "flex", alignItems: "center", gap: "4px", marginTop: "auto" }}>
                                <span style={{ fontSize: "0.7rem", fontWeight: 600, color: "#b45309" }}>
                                    🐚 {task.reward}
                                </span>
                                <span style={{ flex: 1 }} />
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
                                            ✓
                                        </button>
                                        <button style={smallBtn(!!actionBusy)} disabled={!!actionBusy}
                                            onClick={() => doAction("reject-" + task.id, () => ClawNetRejectTask(task.id))}>
                                            ✗
                                        </button>
                                    </>
                                )}
                                {(task.status === "open" || task.status === "assigned") && (
                                    <button style={smallBtn(!!actionBusy)} disabled={!!actionBusy}
                                        onClick={() => doAction("cancel-" + task.id, () => ClawNetCancelTask(task.id))}>
                                        ✗
                                    </button>
                                )}
                            </div>
                        </div>
                    );
                })}
            </div>
        </div>
    );
}
