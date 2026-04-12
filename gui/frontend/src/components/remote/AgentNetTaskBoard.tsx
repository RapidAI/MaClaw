import { useState, useEffect, useCallback, useRef, useMemo } from "react";
import {
    AgentNetListTasks, AgentNetGetCredits,
    AgentNetSubmitTaskResult, AgentNetApproveTask,
    AgentNetRejectTask,
    AgentNetCreateTask, AgentNetBrowseNetworkTasks, AgentNetPublishTasksToHub,
    AgentNetManualPickTask,
} from "../../../wailsjs/go/main/App";
import { colors } from "./styles";
import { useToast } from "../Toast";

type Props = {
    lang: string;
    agentNetRunning: boolean;
};

interface AgentNetTask {
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
    created:   { bg: "var(--theme-success-bg)", text: "var(--theme-success)", label_zh: "开放", label_en: "Open" },
    open:      { bg: "var(--theme-success-bg)", text: "var(--theme-success)", label_zh: "开放", label_en: "Open" },
    claimed:   { bg: "var(--theme-info-bg)", text: "var(--theme-primary)", label_zh: "已认领", label_en: "Claimed" },
    assigned:  { bg: "var(--theme-info-bg)", text: "var(--theme-primary)", label_zh: "已分配", label_en: "Assigned" },
    submitted: { bg: "var(--theme-warning-bg)", text: "var(--theme-warning)", label_zh: "已提交", label_en: "Submitted" },
    accepted:  { bg: "var(--theme-success-bg)", text: "var(--theme-success)", label_zh: "已通过", label_en: "Accepted" },
    approved:  { bg: "var(--theme-success-bg)", text: "var(--theme-success)", label_zh: "已通过", label_en: "Approved" },
    rejected:  { bg: "var(--theme-danger-bg)", text: "var(--theme-danger)", label_zh: "已拒绝", label_en: "Rejected" },
    cancelled: { bg: "var(--theme-surface-muted)", text: "var(--theme-text-muted)", label_zh: "已取消", label_en: "Cancelled" },
};

const MAX_TASKS_LOCAL = 12;
const MAX_TASKS_TOTAL = 24;
const POLL_INTERVAL_MS = 30_000;

type ViewMode = "all" | "network";

const localizeText = (lang: string | undefined, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

export function AgentNetTaskBoard({ lang, agentNetRunning }: Props) {
    const [tasks, setTasks] = useState<AgentNetTask[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [credits, setCredits] = useState<{ balance: number; tier: string } | null>(null);
    const [viewMode, setViewMode] = useState<ViewMode>("all");
    const [actionBusy, setActionBusy] = useState<string | null>(null);
    const [manualPickId, setManualPickId] = useState<string | null>(null);
    const { showToast, dismissToast } = useToast();
    const refreshRef = useRef(0); // guard stale responses
    // Create task form
    const [showCreate, setShowCreate] = useState(false);
    const [newTitle, setNewTitle] = useState("");
    const [newReward, setNewReward] = useState(100);

    const refresh = useCallback(async () => {
        if (!agentNetRunning) return;
        const seq = ++refreshRef.current;
        setLoading(true);
        setError("");
        try {
            // Fire primary fetch + credits in parallel
            const primaryFetch = viewMode === "network"
                    ? AgentNetBrowseNetworkTasks()
                    : AgentNetListTasks("");

            const [res, creditsRes] = await Promise.all([
                primaryFetch,
                AgentNetGetCredits().catch(() => null),
            ]);

            // Stale guard: discard if a newer refresh was triggered
            if (seq !== refreshRef.current) return;

            if (res.ok) {
                let finalTasks: AgentNetTask[] = (res.tasks || []).slice(
                    0, viewMode === "network" ? MAX_TASKS_TOTAL : MAX_TASKS_LOCAL,
                );
                // In "all" mode, also fetch network tasks and merge
                if (viewMode === "all") {
                    try {
                        const netRes = await AgentNetBrowseNetworkTasks();
                        if (seq !== refreshRef.current) return;
                        if (netRes.ok && netRes.tasks?.length) {
                            // Normalize IDs to handle Wails uppercase key serialization
                            const localIds = new Set(finalTasks.map((t) => t.id || (t as any).ID || ""));
                            const netTasks = (netRes.tasks as AgentNetTask[]).filter(t => {
                                const tid = t.id || (t as any).ID || "";
                                return tid && !localIds.has(tid);
                            });
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
                AgentNetPublishTasksToHub().catch(() => {});
            }
        } catch (e) {
            if (seq !== refreshRef.current) return;
            setError(String(e));
        } finally {
            if (seq === refreshRef.current) setLoading(false);
        }
    }, [agentNetRunning, viewMode]);

    useEffect(() => {
        refresh();
        if (!agentNetRunning) return;
        const timer = setInterval(refresh, POLL_INTERVAL_MS);
        return () => clearInterval(timer);
    }, [refresh, agentNetRunning]);

    // (no cleanup needed — toast is global)

    const doAction = useCallback(async (label: string, fn: () => Promise<any>) => {
        setActionBusy(label);
        try {
            const res = await fn();
            if (res.ok) {
                showToast(localizeText(lang, "Success", "操作成功"), "success");
                refresh();
            } else {
                showToast(res.error || "Failed", "error");
            }
        } catch (e) {
            showToast(String(e), "error");
        } finally {
            setActionBusy(null);
        }
    }, [lang, showToast, refresh]);

    const handleCreate = useCallback(async () => {
        if (!newTitle.trim()) return;
        const reward = Math.max(0, Math.floor(newReward));
        if (reward !== 0 && reward < 100) {
            showToast(localizeText(lang, "Minimum reward is 100 🐚 (or 0 for free collaboration)", "赏金最低 100 🐚（或 0 表示免费协作）"), "warning", 4000);
            return;
        }
        if (reward > 0 && credits && credits.balance < reward) {
            showToast(localizeText(lang, `Insufficient balance (have ${credits.balance} 🐚, need ${reward} 🐚)`, `余额不足（当前 ${credits.balance} 🐚，需要 ${reward} 🐚）`), "warning", 4000);
            return;
        }
        await doAction("create", () => AgentNetCreateTask(newTitle.trim(), reward));
        setNewTitle("");
        setNewReward(100);
        setShowCreate(false);
    }, [newTitle, newReward, credits, lang, showToast, doAction]);

    const handleManualPick = useCallback(async (taskId: string) => {
        setManualPickId(taskId);
        const pickingToastId = showToast(localizeText(lang, "⏳ Picking up and executing task...", "⏳ 正在接单并执行任务..."), "info", 60000);
        try {
            const res = await AgentNetManualPickTask(taskId);
            dismissToast(pickingToastId);
            if (res.ok) {
                showToast(localizeText(lang, "✅ Task completed and submitted", "✅ 任务已完成并提交"), "success", 5000);
                refresh();
            } else {
                showToast(res.error || localizeText(lang, "Failed to pick task", "接单失败"), "error", 6000);
            }
        } catch (e) {
            dismissToast(pickingToastId);
            showToast(String(e), "error", 6000);
        } finally {
            setManualPickId(null);
        }
    }, [lang, showToast, dismissToast, refresh]);

    if (!agentNetRunning) {
        return (
            <div style={{ padding: "40px 20px", textAlign: "center", color: colors.textMuted }}>
                <div style={{ fontSize: "3rem", marginBottom: "12px" }}>🦞</div>
                <div style={{ fontSize: "1rem", fontWeight: 600, marginBottom: "6px", color: colors.text }}>
                    {localizeText(lang, "AgentNet Not Connected", "智网未连接")}
                </div>
                <div style={{ fontSize: "0.82rem", color: colors.textSecondary }}>
                    {localizeText(lang, "Enable AgentNet in Settings → AgentNet", "请在设置 → 智网中启用 AgentNet")}
                </div>
            </div>
        );
    }

    const btnStyle = useMemo(() => (active?: boolean): React.CSSProperties => ({
        background: active ? colors.primary : "none",
        color: active ? colors.onPrimary : colors.textSecondary,
        border: active ? `1px solid ${colors.primary}` : `1px solid ${colors.border}`,
        borderRadius: "var(--radius-sm, 4px)",
        padding: "3px 10px",
        fontSize: "0.72rem",
        cursor: "pointer",
        fontWeight: active ? 600 : 400,
        transition: "all 0.15s",
    }), []);

    const smallBtn = useMemo(() => (disabled?: boolean): React.CSSProperties => ({
        background: "none",
        border: `1px solid ${colors.border}`,
        borderRadius: "var(--radius-sm, 4px)",
        padding: "2px 8px",
        fontSize: "0.65rem",
        cursor: disabled ? "not-allowed" : "pointer",
        color: disabled ? colors.textMuted : colors.primary,
        opacity: disabled ? 0.5 : 1,
        transition: "all 0.15s",
    }), []);

    return (
        <div style={{ padding: "0 15px", width: "100%", boxSizing: "border-box" }}>
            {/* Header */}
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: "12px", flexWrap: "wrap", gap: "6px" }}>
                <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
                    {credits && (
                        <span style={{
                            fontSize: "0.75rem", background: "var(--theme-warning-bg)", color: "var(--theme-warning)",
                            padding: "2px 8px", borderRadius: "10px", fontWeight: 500,
                        }}>
                            🐚 {credits.balance ?? 0} {credits.tier && `· ${credits.tier}`}
                        </span>
                    )}
                </div>
                <div style={{ display: "flex", gap: "4px", alignItems: "center" }}>
                    <button style={btnStyle(viewMode === "all")} onClick={() => setViewMode("all")}>
                        {localizeText(lang, "All", "全部")}
                    </button>
                    <button style={btnStyle(viewMode === "network")} onClick={() => setViewMode("network")}>
                        {localizeText(lang, "Network", "网络")}
                    </button>
                    <button style={btnStyle()} onClick={() => setShowCreate(!showCreate)}>
                        + {localizeText(lang, "Post", "发布")}
                    </button>
                    <button onClick={refresh} disabled={loading} style={btnStyle()}>
                        {loading ? "..." : "↻"}
                    </button>
                </div>
            </div>

            {/* Action feedback is now shown via global toast */}

            {/* Create task form */}
            {showCreate && (
                <div style={{ background: colors.surfaceMuted, borderRadius: "8px", padding: "10px 12px", marginBottom: "10px", display: "flex", gap: "8px", alignItems: "center", flexWrap: "wrap" }}>
                    <input
                        value={newTitle}
                        onChange={(e) => setNewTitle(e.target.value)}
                        placeholder={localizeText(lang, "Task title...", "任务标题...")}
                        style={{ flex: 1, minWidth: "120px", border: `1px solid ${colors.border}`, borderRadius: "6px", padding: "4px 8px", fontSize: "0.78rem", background: colors.surface, color: colors.text }}
                    />
                    <div style={{ display: "flex", alignItems: "center", gap: "4px" }}>
                        <span style={{ fontSize: "0.75rem" }}>🐚</span>
                        <input
                            type="number" value={newReward} min={0}
                            onChange={(e) => setNewReward(Number(e.target.value))}
                            style={{ width: "50px", border: `1px solid ${colors.border}`, borderRadius: "6px", padding: "4px 6px", fontSize: "0.78rem", background: colors.surface, color: colors.text }}
                        />
                    </div>
                    <button onClick={handleCreate} disabled={!newTitle.trim() || actionBusy === "create"} className="btn-primary" style={{ padding: "4px 12px", fontSize: "0.72rem" }}>
                        {localizeText(lang, "Post", "发布")}
                    </button>
                </div>
            )}

            {error && (
                <div style={{ fontSize: "0.78rem", color: "var(--theme-danger)", marginBottom: "10px", padding: "6px 10px", background: "var(--theme-danger-bg)", borderRadius: "6px" }}>
                    {error}
                </div>
            )}

            {tasks.length === 0 && !loading && !error && (
                <div style={{ textAlign: "center", color: colors.textMuted, padding: "30px 0", fontSize: "0.85rem" }}>
                    {viewMode === "network"
                            ? localizeText(lang, "No network tasks (peers haven't published to Hub yet)", "暂无网络任务（其他节点尚未发布任务到 Hub）")
                            : localizeText(lang, "No tasks available", "暂无任务")}
                </div>
            )}

            {/* Task cards – 2 per row */}
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "6px" }}>
                {tasks.map((rawTask) => {
                    // Wails may serialize Go struct fields with uppercase keys when nested in map[string]interface{}.
                    // Normalize field access to handle both cases (e.g. Status vs status).
                    const task: AgentNetTask = {
                        id: rawTask.id || (rawTask as any).ID || "",
                        title: rawTask.title || (rawTask as any).Title || "",
                        description: rawTask.description || (rawTask as any).Description,
                        status: rawTask.status || (rawTask as any).Status || "",
                        reward: rawTask.reward ?? (rawTask as any).Reward ?? 0,
                        creator: rawTask.creator || (rawTask as any).Creator,
                        assignee: rawTask.assignee || (rawTask as any).Assignee,
                        created_at: rawTask.created_at || (rawTask as any).CreatedAt || (rawTask as any).created_at,
                    };
                    const normalizedStatus = (task.status || "").toLowerCase();
                    const isOpen = normalizedStatus === "open" || normalizedStatus === "created" || normalizedStatus === "" || !normalizedStatus;
                    const sc = STATUS_COLORS[normalizedStatus] || STATUS_COLORS.created;
                    return (
                        <div key={task.id} style={{
                            background: colors.surface, border: `1px solid ${colors.border}`, borderRadius: "var(--radius-lg, 8px)",
                            padding: "7px 10px", display: "flex", flexDirection: "column", gap: "3px",
                            transition: "box-shadow 0.15s, border-color 0.15s",
                            minWidth: 0, overflow: "hidden",
                        }}
                        onMouseEnter={(e) => { e.currentTarget.style.boxShadow = "0 1px 6px rgba(45,55,72,0.10)"; e.currentTarget.style.borderColor = colors.primaryLight || "var(--theme-primary)"; }}
                        onMouseLeave={(e) => { e.currentTarget.style.boxShadow = "none"; e.currentTarget.style.borderColor = colors.border; }}
                        >
                            {/* Row 1: title + status */}
                            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: "4px" }}>
                                <div title={task.title} style={{ fontSize: "0.76rem", fontWeight: 600, color: colors.text, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", flex: 1, minWidth: 0 }}>
                                    {task.title}
                                </div>
                                <span style={{ fontSize: "0.6rem", fontWeight: 500, background: sc.bg, color: sc.text, padding: "0px 5px", borderRadius: "6px", flexShrink: 0, whiteSpace: "nowrap", lineHeight: "16px" }}>
                                    {localizeText(lang, sc.label_en, sc.label_zh)}
                                </span>
                            </div>

                            {task.description && (
                                <div title={task.description} style={{ fontSize: "0.68rem", color: colors.textMuted, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                                    {task.description}
                                </div>
                            )}

                            {/* Row 2: reward + actions (right-aligned) */}
                            <div style={{ display: "flex", alignItems: "center", gap: "4px", marginTop: "auto" }}>
                                <span style={{ fontSize: "0.7rem", fontWeight: 600, color: "var(--theme-warning)" }}>
                                    🐚 {task.reward}
                                </span>
                                <span style={{ flex: 1 }} />
                                {normalizedStatus === "assigned" && (
                                    <button style={smallBtn(!!actionBusy || !!manualPickId)} disabled={!!actionBusy || !!manualPickId}
                                        onClick={() => doAction("submit-" + task.id, () => AgentNetSubmitTaskResult(task.id, ""))}>
                                        {localizeText(lang, "Submit", "提交")}
                                    </button>
                                )}
                                {normalizedStatus === "submitted" && (
                                    <>
                                        <button style={smallBtn(!!actionBusy || !!manualPickId)} disabled={!!actionBusy || !!manualPickId}
                                            onClick={() => doAction("approve-" + task.id, () => AgentNetApproveTask(task.id))}>
                                            ✓
                                        </button>
                                        <button style={smallBtn(!!actionBusy || !!manualPickId)} disabled={!!actionBusy || !!manualPickId}
                                            onClick={() => doAction("reject-" + task.id, () => AgentNetRejectTask(task.id))}>
                                            ✗
                                        </button>
                                    </>
                                )}
                                {/* 接单按钮 – 仅开放任务显示 */}
                                {isOpen && (
                                    <button
                                        className="btn-primary"
                                        style={{
                                            padding: "2px 10px",
                                            fontSize: "0.68rem",
                                            fontWeight: 600,
                                            cursor: (!!actionBusy || !!manualPickId) ? "not-allowed" : "pointer",
                                            opacity: (!!actionBusy || !!manualPickId) && manualPickId !== task.id ? 0.5 : 1,
                                            ...(manualPickId === task.id ? {
                                                background: "var(--theme-warning-bg)",
                                                color: "var(--theme-warning)",
                                                border: "1px solid var(--theme-warning)",
                                                boxShadow: "none",
                                            } : {}),
                                        }}
                                        disabled={!!actionBusy || !!manualPickId}
                                        onClick={() => handleManualPick(task.id)}
                                    >
                                        {manualPickId === task.id ? localizeText(lang, "Running...", "执行中...") : localizeText(lang, "🤖 Pick", "🤖 接单")}
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
