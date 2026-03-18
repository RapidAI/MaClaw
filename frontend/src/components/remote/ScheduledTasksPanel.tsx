import { useState, useEffect, useCallback } from "react";
import {
    ListScheduledTasks,
    CreateScheduledTask,
    UpdateScheduledTask,
    DeleteScheduledTask,
    PauseScheduledTask,
    ResumeScheduledTask,
} from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import { colors, radius } from "./styles";

interface ScheduledTask {
    id: string;
    name: string;
    action: string;
    hour: number;
    minute: number;
    day_of_week: number;
    day_of_month: number;
    start_date: string;
    end_date: string;
    status: string;
    created_at: string;
    last_run_at: string | null;
    next_run_at: string | null;
    run_count: number;
    last_result: string;
    last_error: string;
}

const WEEKDAYS_ZH = ["周日", "周一", "周二", "周三", "周四", "周五", "周六"];
const WEEKDAYS_EN = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

const inputStyle: React.CSSProperties = {
    width: "100%", padding: "7px 10px", fontSize: "0.8rem",
    border: `1px solid ${colors.border}`, borderRadius: 4,
    background: colors.surface, color: colors.text, boxSizing: "border-box",
};
const labelStyle: React.CSSProperties = {
    fontSize: "0.76rem", color: colors.textSecondary, marginBottom: 4, display: "block",
};

type Props = { lang: string };

function fmtDate(s: string | null, isZh: boolean): string {
    if (!s) return "-";
    try { return new Date(s).toLocaleString(isZh ? "zh-CN" : "en-US"); }
    catch { return s; }
}

function scheduleDesc(t: ScheduledTask, isZh: boolean): string {
    const weekdays = isZh ? WEEKDAYS_ZH : WEEKDAYS_EN;
    const time = `${String(t.hour).padStart(2, "0")}:${String(t.minute).padStart(2, "0")}`;

    // One-time task: start_date == end_date (both non-empty)
    const isOneTime = t.start_date && t.end_date && t.start_date === t.end_date;

    let desc = "";
    if (isOneTime) {
        desc = isZh ? `${t.start_date} ${time}（仅一次）` : `${t.start_date} ${time} (once)`;
    } else if (t.day_of_month > 0) {
        desc = isZh ? `每月${t.day_of_month}号 ${time}` : `${t.day_of_month}th of month at ${time}`;
    } else if (t.day_of_week >= 0 && t.day_of_week <= 6) {
        desc = isZh ? `每${weekdays[t.day_of_week]} ${time}` : `Every ${weekdays[t.day_of_week]} at ${time}`;
    } else {
        desc = isZh ? `每天 ${time}` : `Daily at ${time}`;
    }
    if (!isOneTime && (t.start_date || t.end_date)) {
        desc += ` (${t.start_date || "..."} ~ ${t.end_date || "..."})`;
    }
    return desc;
}

const STATUS_COLORS: Record<string, string> = {
    active: "#059669",
    paused: "#d97706",
    expired: "#8b95a5",
};

export function ScheduledTasksPanel({ lang }: Props) {
    const isZh = lang?.startsWith("zh");
    const t = useCallback((zh: string, en: string) => isZh ? zh : en, [isZh]);

    const [tasks, setTasks] = useState<ScheduledTask[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [dlgOpen, setDlgOpen] = useState(false);
    const [editTask, setEditTask] = useState<ScheduledTask | null>(null);
    const [deleteTarget, setDeleteTarget] = useState<string | null>(null);

    // Form state
    const [fName, setFName] = useState("");
    const [fAction, setFAction] = useState("");
    const [fHour, setFHour] = useState(9);
    const [fMinute, setFMinute] = useState(0);
    const [fDow, setFDow] = useState(-1);
    const [fDom, setFDom] = useState(-1);
    const [fStartDate, setFStartDate] = useState("");
    const [fEndDate, setFEndDate] = useState("");
    const [saving, setSaving] = useState(false);

    const loadTasks = useCallback(async () => {
        setLoading(true); setError("");
        try {
            const list = await ListScheduledTasks();
            setTasks(Array.isArray(list) ? list : []);
        } catch (e) { setError(String(e)); }
        setLoading(false);
    }, []);

    useEffect(() => { loadTasks(); }, [loadTasks]);

    // Refresh when tasks are changed from the agent chat side.
    useEffect(() => {
        EventsOn("scheduled-tasks-changed", loadTasks);
        return () => { EventsOff("scheduled-tasks-changed"); };
    }, [loadTasks]);

    const openCreate = () => {
        setEditTask(null); setFName(""); setFAction(""); setFHour(9); setFMinute(0);
        setFDow(-1); setFDom(-1); setFStartDate(""); setFEndDate(""); setError(""); setDlgOpen(true);
    };

    const openEdit = (task: ScheduledTask) => {
        setEditTask(task); setFName(task.name); setFAction(task.action);
        setFHour(task.hour); setFMinute(task.minute); setFDow(task.day_of_week);
        setFDom(task.day_of_month); setFStartDate(task.start_date || "");
        setFEndDate(task.end_date || ""); setError(""); setDlgOpen(true);
    };

    const handleSave = async () => {
        if (!fName.trim() || !fAction.trim()) return;
        const hour = Math.max(0, Math.min(23, fHour));
        const minute = Math.max(0, Math.min(59, fMinute));
        setSaving(true); setError("");
        try {
            if (editTask) {
                await UpdateScheduledTask(editTask.id, {
                    name: fName.trim(), action: fAction.trim(),
                    hour, minute,
                    day_of_week: fDow, day_of_month: fDom,
                    start_date: fStartDate, end_date: fEndDate,
                });
            } else {
                await CreateScheduledTask(fName.trim(), fAction.trim(), hour, minute, fDow, fDom, fStartDate, fEndDate);
            }
            setDlgOpen(false); await loadTasks();
        } catch (e) { setError(String(e)); }
        setSaving(false);
    };

    const handleDelete = async (id: string) => {
        setError("");
        try { await DeleteScheduledTask(id); setDeleteTarget(null); await loadTasks(); }
        catch (e) { setError(String(e)); }
    };

    const handleTogglePause = async (task: ScheduledTask) => {
        setError("");
        try {
            if (task.status === "active") { await PauseScheduledTask(task.id); }
            else { await ResumeScheduledTask(task.id); }
            await loadTasks();
        } catch (e) { setError(String(e)); }
    };

    return (
        <div style={{ padding: 0 }}>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 10 }}>
                <span style={{ fontSize: "0.76rem", color: colors.textSecondary }}>
                    {tasks.length} {t("个定时任务", "scheduled task(s)")}
                </span>
                <button onClick={openCreate} style={{
                    padding: "4px 14px", fontSize: "0.76rem", fontWeight: 600,
                    background: "#6366f1", color: "#fff", border: "none",
                    borderRadius: radius.md, cursor: "pointer",
                }}>
                    + {t("新建", "New")}
                </button>
            </div>

            {error && <div role="alert" style={{ color: colors.danger, fontSize: "0.76rem", marginBottom: 8 }}>{error}</div>}
            {loading && <div style={{ fontSize: "0.76rem", color: colors.textMuted }}>{t("加载中…", "Loading…")}</div>}

            {/* Task list */}
            <div style={{ display: "flex", flexDirection: "column", gap: 6, maxHeight: "400px", overflowY: "auto" }}>
                {tasks.length === 0 && !loading && (
                    <div style={{ fontSize: "0.78rem", color: colors.textMuted, textAlign: "center", padding: "20px 0" }}>
                        {t("暂无定时任务。可通过上方按钮创建，或在聊天中告诉 MaClaw 每天9点做XX", "No scheduled tasks. Create one above, or tell MaClaw in chat.")}
                    </div>
                )}
                {tasks.map(task => (
                    <div key={task.id} style={{ border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: "8px 10px", background: colors.surface }}>
                        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: 8 }}>
                            <div style={{ flex: 1, minWidth: 0 }}>
                                <div style={{ display: "flex", alignItems: "center", gap: 6, marginBottom: 4 }}>
                                    <span style={{
                                        fontSize: "0.66rem", fontWeight: 600, padding: "1px 6px",
                                        borderRadius: radius.sm, color: "#fff",
                                        background: STATUS_COLORS[task.status] || colors.textMuted,
                                    }}>{task.status}</span>
                                    <span style={{ fontSize: "0.8rem", fontWeight: 600, color: colors.text }}>{task.name}</span>
                                </div>
                                <div style={{ fontSize: "0.72rem", color: colors.textMuted, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
                                    🎯 {task.action}
                                    {" · "}⏰ {scheduleDesc(task, isZh)}
                                    {task.next_run_at && <>{" · "}{t("下次", "Next")}: {fmtDate(task.next_run_at, isZh)}</>}
                                    {task.run_count > 0 && <>{" · "}{t("已执行", "Runs")}: {task.run_count}</>}
                                </div>
                                {task.last_error && (
                                    <div style={{ fontSize: "0.68rem", color: colors.danger, marginTop: 2 }}>⚠️ {task.last_error}</div>
                                )}
                            </div>
                            <div style={{ display: "flex", gap: 4, flexShrink: 0 }}>
                                {task.status !== "expired" && (
                                    <button onClick={() => handleTogglePause(task)}
                                        title={task.status === "active" ? t("暂停", "Pause") : t("恢复", "Resume")}
                                        style={{ padding: "3px 8px", fontSize: "0.7rem", cursor: "pointer", background: "none", border: `1px solid ${colors.border}`, borderRadius: radius.sm, color: colors.textSecondary }}>
                                        {task.status === "active" ? "⏸️" : "▶️"}
                                    </button>
                                )}
                                <button onClick={() => openEdit(task)} title={t("编辑", "Edit")}
                                    style={{ padding: "3px 8px", fontSize: "0.7rem", cursor: "pointer", background: "none", border: `1px solid ${colors.border}`, borderRadius: radius.sm, color: colors.textSecondary }}>
                                    ✏️
                                </button>
                                <button onClick={() => setDeleteTarget(task.id)} title={t("删除", "Delete")}
                                    style={{ padding: "3px 8px", fontSize: "0.7rem", cursor: "pointer", background: "none", border: `1px solid ${colors.border}`, borderRadius: radius.sm, color: colors.danger }}>
                                    🗑️
                                </button>
                            </div>
                        </div>
                    </div>
                ))}
            </div>

            {/* Delete confirmation */}
            {deleteTarget && (
                <div style={{ position: "fixed", inset: 0, background: "rgba(0,0,0,0.3)", display: "flex", alignItems: "center", justifyContent: "center", zIndex: 9999 }} onClick={() => setDeleteTarget(null)}>
                    <div role="dialog" aria-modal="true" onClick={e => e.stopPropagation()} style={{ background: colors.surface, borderRadius: radius.lg, padding: "20px 24px", minWidth: 280, boxShadow: "0 8px 30px rgba(0,0,0,0.12)" }}>
                        <p style={{ fontSize: "0.82rem", marginBottom: 16 }}>{t("确定删除这个定时任务？", "Delete this scheduled task?")}</p>
                        <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
                            <button onClick={() => setDeleteTarget(null)} style={{ padding: "5px 14px", fontSize: "0.76rem", border: `1px solid ${colors.border}`, borderRadius: radius.md, background: colors.surface, cursor: "pointer" }}>{t("取消", "Cancel")}</button>
                            <button onClick={() => handleDelete(deleteTarget)} style={{ padding: "5px 14px", fontSize: "0.76rem", border: "none", borderRadius: radius.md, background: colors.danger, color: "#fff", cursor: "pointer" }}>{t("删除", "Delete")}</button>
                        </div>
                    </div>
                </div>
            )}

            {/* Create / Edit dialog */}
            {dlgOpen && (
                <div style={{ position: "fixed", inset: 0, background: "rgba(0,0,0,0.3)", display: "flex", alignItems: "center", justifyContent: "center", zIndex: 9999 }} onClick={() => setDlgOpen(false)}>
                    <div role="dialog" aria-modal="true" onClick={e => e.stopPropagation()} style={{ background: colors.surface, borderRadius: radius.lg, padding: "20px 24px", width: 440, maxWidth: "90vw", boxShadow: "0 8px 30px rgba(0,0,0,0.12)" }}>
                        <h4 style={{ fontSize: "0.82rem", margin: "0 0 14px", color: colors.text }}>
                            {editTask ? t("编辑定时任务", "Edit Scheduled Task") : t("新建定时任务", "New Scheduled Task")}
                        </h4>

                        <div style={{ marginBottom: 10 }}>
                            <label style={labelStyle}>{t("任务名称", "Task Name")}</label>
                            <input value={fName} onChange={e => setFName(e.target.value)} placeholder={t("如：每日代码审查", "e.g. Daily code review")} style={inputStyle} />
                        </div>

                        <div style={{ marginBottom: 10 }}>
                            <label style={labelStyle}>{t("执行内容（到时发给 Agent 执行）", "Action (sent to Agent at trigger time)")}</label>
                            <textarea value={fAction} onChange={e => setFAction(e.target.value)} rows={3}
                                placeholder={t("如：检查项目 /home/dev/myapp 的测试是否通过，如果失败发送报告", "e.g. Run tests for /home/dev/myapp and report failures")}
                                style={{ ...inputStyle, resize: "vertical", fontFamily: "inherit" }} />
                        </div>

                        <div style={{ display: "flex", gap: 8, marginBottom: 10 }}>
                            <div style={{ flex: 1 }}>
                                <label style={labelStyle}>{t("小时", "Hour")} (0-23)</label>
                                <input type="number" min={0} max={23} value={fHour} onChange={e => setFHour(Number(e.target.value))} style={inputStyle} />
                            </div>
                            <div style={{ flex: 1 }}>
                                <label style={labelStyle}>{t("分钟", "Minute")} (0-59)</label>
                                <input type="number" min={0} max={59} value={fMinute} onChange={e => setFMinute(Number(e.target.value))} style={inputStyle} />
                            </div>
                        </div>

                        <div style={{ display: "flex", gap: 8, marginBottom: 10 }}>
                            <div style={{ flex: 1 }}>
                                <label style={labelStyle}>{t("星期", "Day of Week")}</label>
                                <select value={fDow} onChange={e => setFDow(Number(e.target.value))} style={{ ...inputStyle }}>
                                    <option value={-1}>{t("每天", "Every day")}</option>
                                    {(isZh ? WEEKDAYS_ZH : WEEKDAYS_EN).map((d, i) => (
                                        <option key={i} value={i}>{d}</option>
                                    ))}
                                </select>
                            </div>
                            <div style={{ flex: 1 }}>
                                <label style={labelStyle}>{t("每月几号", "Day of Month")}</label>
                                <select value={fDom} onChange={e => setFDom(Number(e.target.value))} style={{ ...inputStyle }}>
                                    <option value={-1}>{t("不限", "Any")}</option>
                                    {Array.from({ length: 31 }, (_, i) => i + 1).map(d => (
                                        <option key={d} value={d}>{d}</option>
                                    ))}
                                </select>
                            </div>
                        </div>

                        <div style={{ display: "flex", gap: 8, marginBottom: 14 }}>
                            <div style={{ flex: 1 }}>
                                <label style={labelStyle}>{t("开始日期（可选）", "Start Date (optional)")}</label>
                                <input type="date" value={fStartDate} onChange={e => setFStartDate(e.target.value)} style={inputStyle} />
                            </div>
                            <div style={{ flex: 1 }}>
                                <label style={labelStyle}>{t("结束日期（可选）", "End Date (optional)")}</label>
                                <input type="date" value={fEndDate} onChange={e => setFEndDate(e.target.value)} style={inputStyle} />
                            </div>
                        </div>

                        <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
                            <button onClick={() => setDlgOpen(false)} style={{ padding: "5px 14px", fontSize: "0.76rem", border: `1px solid ${colors.border}`, borderRadius: radius.md, background: colors.surface, cursor: "pointer" }}>
                                {t("取消", "Cancel")}
                            </button>
                            <button onClick={handleSave} disabled={saving || !fName.trim() || !fAction.trim()} style={{
                                padding: "5px 14px", fontSize: "0.76rem", border: "none", borderRadius: radius.md,
                                background: "#6366f1", color: "#fff", cursor: "pointer",
                                opacity: saving || !fName.trim() || !fAction.trim() ? 0.5 : 1,
                            }}>
                                {saving ? t("保存中…", "Saving…") : t("保存", "Save")}
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
}
