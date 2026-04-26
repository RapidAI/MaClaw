import { useState, useEffect, useCallback } from "react";
import {
    ListScheduledTasks,
    CreateScheduledTask,
    UpdateScheduledTask,
    DeleteScheduledTask,
    PauseScheduledTask,
    ResumeScheduledTask,
    TriggerScheduledTask,
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
    interval_minutes: number;
    start_date: string;
    end_date: string;
    task_type: string;
    status: string;
    created_at: string;
    last_run_at: string | null;
    next_run_at: string | null;
    run_count: number;
    last_result: string;
    last_error: string;
}

const WEEKDAYS = {
    'zh-Hans': ["周日", "周一", "周二", "周三", "周四", "周五", "周六"],
    'zh-Hant': ["週日", "週一", "週二", "週三", "週四", "週五", "週六"],
    'en': ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"],
} as const;

const inputStyle: React.CSSProperties = {
    width: "100%", padding: "7px 10px", fontSize: "0.8rem",
    border: `1px solid ${colors.border}`, borderRadius: 4,
    background: colors.surface, color: colors.text, boxSizing: "border-box",
};
const labelStyle: React.CSSProperties = {
    fontSize: "0.76rem", color: colors.textSecondary, marginBottom: 4, display: "block",
};

type Props = { lang: string };

function getWeekdays(lang: string): readonly string[] {
    const key = lang === 'zh-Hant' ? 'zh-Hant' : lang === 'zh-Hans' ? 'zh-Hans' : 'en';
    return WEEKDAYS[key];
}

function getDateTimeLocale(lang: string): string {
    return lang === 'zh-Hans' ? 'zh-CN' : lang === 'zh-Hant' ? 'zh-TW' : 'en-US';
}

function fmtDate(s: string | null, lang: string): string {
    if (!s) return "-";
    try { return new Date(s).toLocaleString(getDateTimeLocale(lang)); }
    catch { return s; }
}

function formatInterval(minutes: number, lang: string): string {
    if (minutes <= 0) return "";
    const isZh = lang === 'zh-Hans' || lang === 'zh-Hant';
    if (minutes >= 1440) {
        const days = minutes / 1440;
        return Number.isInteger(days)
            ? (isZh ? `${days}天` : `${days} day${days !== 1 ? "s" : ""}`)
            : (isZh ? `${days.toFixed(1)}天` : `${days.toFixed(1)} days`);
    }
    if (minutes >= 60) {
        const hours = minutes / 60;
        return Number.isInteger(hours)
            ? (isZh ? `${hours}小时` : `${hours} hour${hours !== 1 ? "s" : ""}`)
            : (isZh ? `${hours.toFixed(1)}小时` : `${hours.toFixed(1)} hours`);
    }
    return isZh ? `${minutes}分钟` : `${minutes} min`;
}

function scheduleDesc(t: ScheduledTask, lang: string): string {
    const isZh = lang === 'zh-Hans' || lang === 'zh-Hant';
    const weekdays = getWeekdays(lang);
    const time = `${String(t.hour).padStart(2, "0")}:${String(t.minute).padStart(2, "0")}`;

    // One-time task: start_date == end_date (both non-empty)
    const isOneTime = t.start_date && t.end_date && t.start_date === t.end_date;

    // Interval mode: repeat every N minutes
    if (t.interval_minutes > 0) {
        const ivStr = formatInterval(t.interval_minutes, lang);
        let desc = isZh ? `每${ivStr}` : `Every ${ivStr}`;
        desc += isZh ? `（首次 ${time}）` : ` (first at ${time})`;
        if (t.start_date || t.end_date) {
            desc += ` (${t.start_date || "..."} ~ ${t.end_date || "..."})`;
        }
        return desc;
    }

    let desc = "";
    if (isOneTime) {
        desc = lang === 'zh-Hans' ? `${t.start_date} ${time}（仅一次）`
            : lang === 'zh-Hant' ? `${t.start_date} ${time}（僅一次）`
            : `${t.start_date} ${time} (once)`;
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
    expired: "var(--theme-text-muted)",
};

export function ScheduledTasksPanel({ lang }: Props) {
    const t = useCallback((en: string, zhHans: string, zhHant: string = zhHans) =>
        lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en, [lang]);

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
    const [fIntervalMin, setFIntervalMin] = useState(0);
    const [fTaskType, setFTaskType] = useState("reminder");
    const [fStartDate, setFStartDate] = useState("");
    const [fEndDate, setFEndDate] = useState("");
    const [fScheduleMode, setFScheduleMode] = useState<"fixed" | "interval">("fixed");
    const [saving, setSaving] = useState(false);
    const [triggering, setTriggering] = useState<string | null>(null);

    const loadTasks = useCallback(async () => {
        setLoading(true); setError("");
        try {
            const list = await ListScheduledTasks();
            const all = Array.isArray(list) ? list : [];
            // Filter out expired tasks — they are auto-deleted on the backend,
            // but hide them immediately on the frontend as well.
            setTasks(all.filter((t: ScheduledTask) => t.status !== "expired"));
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
        setFDow(-1); setFDom(-1); setFIntervalMin(0); setFTaskType("reminder");
        setFStartDate(""); setFEndDate(""); setFScheduleMode("fixed"); setError(""); setDlgOpen(true);
    };

    const openEdit = (task: ScheduledTask) => {
        setEditTask(task); setFName(task.name); setFAction(task.action);
        setFHour(task.hour); setFMinute(task.minute); setFDow(task.day_of_week);
        setFDom(task.day_of_month); setFIntervalMin(task.interval_minutes || 0);
        setFTaskType(task.task_type || "reminder");
        setFStartDate(task.start_date || ""); setFEndDate(task.end_date || "");
        setFScheduleMode(task.interval_minutes > 0 ? "interval" : "fixed");
        setError(""); setDlgOpen(true);
    };

    const handleSave = async () => {
        if (!fName.trim() || !fAction.trim()) return;
        const hour = Math.max(0, Math.min(23, fHour));
        const minute = Math.max(0, Math.min(59, fMinute));
        const intervalMinutes = fScheduleMode === "interval" ? Math.max(0, fIntervalMin || 0) : 0;
        if (fScheduleMode === "interval" && intervalMinutes < 1) {
            setError(t("Interval must be at least 1 minute", "间隔至少为 1 分钟"));
            return;
        }
        const taskType = fTaskType || "reminder";
        setSaving(true); setError("");
        try {
            if (editTask) {
                await UpdateScheduledTask(editTask.id, {
                    name: fName.trim(), action: fAction.trim(),
                    hour, minute,
                    day_of_week: fScheduleMode === "fixed" ? fDow : -1,
                    day_of_month: fScheduleMode === "fixed" ? fDom : -1,
                    interval_minutes: intervalMinutes,
                    task_type: taskType,
                    start_date: fStartDate, end_date: fEndDate,
                });
            } else {
                await CreateScheduledTask(
                    fName.trim(), fAction.trim(), hour, minute,
                    fScheduleMode === "fixed" ? fDow : -1,
                    fScheduleMode === "fixed" ? fDom : -1,
                    intervalMinutes,
                    fStartDate, fEndDate, taskType,
                );
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

    const handleTrigger = async (id: string) => {
        setError(""); setTriggering(id);
        try {
            await TriggerScheduledTask(id);
            // Brief visual feedback then refresh — execution continues in background.
            setTimeout(() => { setTriggering(null); loadTasks(); }, 600);
        } catch (e) { setError(String(e)); setTriggering(null); }
    };

    return (
        <div style={{ padding: 0 }}>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 10 }}>
                <span style={{ fontSize: "0.76rem", color: colors.textSecondary }}>
                    {tasks.length} {t("scheduled task(s)", "个定时任务")}
                </span>
                <button onClick={openCreate} style={{
                    padding: "4px 14px", fontSize: "0.76rem", fontWeight: 600,
                    background: colors.primary, color: "#fff", border: "none",
                    borderRadius: radius.md, cursor: "pointer",
                }}>
                    + {t("New", "新建")}
                </button>
            </div>

            {error && <div role="alert" style={{ color: colors.danger, fontSize: "0.76rem", marginBottom: 8 }}>{error}</div>}
            {loading && <div style={{ fontSize: "0.76rem", color: colors.textMuted }}>{t("Loading…", "加载中…")}</div>}

            {/* Task list */}
            <div style={{ display: "flex", flexDirection: "column", gap: 6, maxHeight: "400px", overflowY: "auto" }}>
                {tasks.length === 0 && !loading && (
                    <div style={{ fontSize: "0.78rem", color: colors.textMuted, textAlign: "center", padding: "20px 0" }}>
                        {t("No scheduled tasks. Create one above, or tell MaClaw in chat.", "暂无定时任务。可通过上方按钮创建，或在聊天中告诉 MaClaw 每天9点做XX")}
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
                                    {task.task_type === "process" && (
                                        <span style={{
                                            fontSize: "0.62rem", fontWeight: 500, padding: "1px 5px",
                                            borderRadius: radius.sm, color: colors.primary,
                                            border: `1px solid ${colors.primary}`,
                                        }}>{t("process", "处理型")}</span>
                                    )}
                                    <span style={{ fontSize: "0.8rem", fontWeight: 600, color: colors.text }}>{task.name}</span>
                                </div>
                                <div style={{ fontSize: "0.72rem", color: colors.textMuted, wordBreak: "break-word", maxHeight: 120, overflowY: "auto", whiteSpace: "pre-wrap" }}>
                                    🎯 {task.action}
                                    {"\n"}⏰ {scheduleDesc(task, lang)}
                                    {task.next_run_at && <>{"\n"}{t("Next", "下次")}: {fmtDate(task.next_run_at, lang)}</>}
                                    {task.run_count > 0 && <>{" · "}{t("Runs", "已执行")}: {task.run_count}</>}
                                </div>
                                {task.last_error && (
                                    <div style={{ fontSize: "0.68rem", color: colors.danger, marginTop: 2 }}>⚠️ {task.last_error}</div>
                                )}
                            </div>
                            <div style={{ display: "flex", gap: 4, flexShrink: 0 }}>
                                {task.status === "active" && (
                                    <button onClick={() => handleTrigger(task.id)}
                                        disabled={triggering === task.id}
                                        title={t("Run Now", "立即运行")}
                                        style={{ padding: "3px 8px", fontSize: "0.7rem", cursor: "pointer", background: "none", border: `1px solid ${colors.border}`, borderRadius: radius.sm, color: colors.primary, opacity: triggering === task.id ? 0.5 : 1 }}>
                                        {triggering === task.id ? "⏳" : "▶️"}
                                    </button>
                                )}
                                {task.status !== "expired" && (
                                    <button onClick={() => handleTogglePause(task)}
                                        title={task.status === "active" ? t("Pause", "暂停") : t("Resume", "恢复")}
                                        style={{ padding: "3px 8px", fontSize: "0.7rem", cursor: "pointer", background: "none", border: `1px solid ${colors.border}`, borderRadius: radius.sm, color: colors.textSecondary }}>
                                        {task.status === "active" ? "⏸️" : "▶️"}
                                    </button>
                                )}
                                <button onClick={() => openEdit(task)} title={t("Edit", "编辑")}
                                    style={{ padding: "3px 8px", fontSize: "0.7rem", cursor: "pointer", background: "none", border: `1px solid ${colors.border}`, borderRadius: radius.sm, color: colors.textSecondary }}>
                                    ✏️
                                </button>
                                <button onClick={() => setDeleteTarget(task.id)} title={t("Delete", "删除")}
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
                <div style={{ position: "fixed", inset: 0, background: colors.overlay, display: "flex", alignItems: "center", justifyContent: "center", zIndex: 9999 }} onClick={() => setDeleteTarget(null)}>
                    <div role="dialog" aria-modal="true" onClick={e => e.stopPropagation()} style={{ background: colors.surface, borderRadius: radius.lg, padding: "20px 24px", minWidth: 280, boxShadow: "0 8px 30px rgba(0,0,0,0.12)" }}>
                        <p style={{ fontSize: "0.82rem", marginBottom: 16 }}>{t("Delete this scheduled task?", "确定删除这个定时任务？")}</p>
                        <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
                            <button onClick={() => setDeleteTarget(null)} style={{ padding: "5px 14px", fontSize: "0.76rem", border: `1px solid ${colors.border}`, borderRadius: radius.md, background: colors.surface, color: colors.text, cursor: "pointer" }}>{t("Cancel", "取消")}</button>
                            <button onClick={() => handleDelete(deleteTarget)} style={{ padding: "5px 14px", fontSize: "0.76rem", border: "none", borderRadius: radius.md, background: colors.danger, color: "#fff", cursor: "pointer" }}>{t("Delete", "删除")}</button>
                        </div>
                    </div>
                </div>
            )}

            {/* Create / Edit dialog */}
            {dlgOpen && (
                <div style={{ position: "fixed", inset: 0, background: colors.overlay, display: "flex", alignItems: "center", justifyContent: "center", zIndex: 9999 }} onClick={() => setDlgOpen(false)}>
                    <div role="dialog" aria-modal="true" onClick={e => e.stopPropagation()} style={{ background: colors.surface, borderRadius: radius.lg, padding: "20px 24px", width: 440, maxWidth: "90vw", boxShadow: "0 8px 30px rgba(0,0,0,0.12)" }}>
                        <h4 style={{ fontSize: "0.82rem", margin: "0 0 14px", color: colors.text }}>
                            {editTask ? t("Edit Scheduled Task", "编辑定时任务") : t("New Scheduled Task", "新建定时任务")}
                        </h4>

                        <div style={{ marginBottom: 10 }}>
                            <label style={labelStyle}>{t("Task Name", "任务名称")}</label>
                            <input value={fName} onChange={e => setFName(e.target.value)} placeholder={t("e.g. Daily code review", "如：每日代码审查")} style={inputStyle} />
                        </div>

                        <div style={{ marginBottom: 10 }}>
                            <label style={labelStyle}>{t("Action (sent to Agent at trigger time)", "执行内容（到时发给 Agent 执行）")}</label>
                            <textarea value={fAction} onChange={e => setFAction(e.target.value)} rows={3}
                                placeholder={t("e.g. Run tests for /home/dev/myapp and report failures", "如：检查项目 /home/dev/myapp 的测试是否通过，如果失败发送报告")}
                                style={{ ...inputStyle, resize: "vertical", fontFamily: "inherit" }} />
                        </div>

                        {/* Schedule mode toggle */}
                        <div style={{ marginBottom: 10 }}>
                            <label style={labelStyle}>{t("Schedule Mode", "调度模式")}</label>
                            <div style={{ display: "flex", gap: 0, borderRadius: 4, overflow: "hidden", border: `1px solid ${colors.border}` }}>
                                <button type="button" onClick={() => setFScheduleMode("fixed")} style={{
                                    flex: 1, padding: "6px 0", fontSize: "0.76rem", cursor: "pointer", border: "none",
                                    background: fScheduleMode === "fixed" ? colors.primary : colors.surface,
                                    color: fScheduleMode === "fixed" ? "#fff" : colors.text,
                                    fontWeight: fScheduleMode === "fixed" ? 600 : 400,
                                }}>
                                    {t("Fixed Time", "固定时间")}
                                </button>
                                <button type="button" onClick={() => setFScheduleMode("interval")} style={{
                                    flex: 1, padding: "6px 0", fontSize: "0.76rem", cursor: "pointer", border: "none",
                                    borderLeft: `1px solid ${colors.border}`,
                                    background: fScheduleMode === "interval" ? colors.primary : colors.surface,
                                    color: fScheduleMode === "interval" ? "#fff" : colors.text,
                                    fontWeight: fScheduleMode === "interval" ? 600 : 400,
                                }}>
                                    {t("Repeat Interval", "间隔重复")}
                                </button>
                            </div>
                        </div>

                        {fScheduleMode === "interval" ? (
                            <>
                                {/* Interval mode: every N minutes */}
                                <div style={{ marginBottom: 10 }}>
                                    <label style={labelStyle}>{t("Repeat every (minutes)", "重复间隔（分钟）")}</label>
                                    <input type="number" min={1} value={fIntervalMin || ""} onChange={e => setFIntervalMin(Number(e.target.value))}
                                        placeholder={t("e.g. 60 = every hour, 1440 = every day", "如：60=每小时，1440=每天")}
                                        style={inputStyle} />
                                    {fIntervalMin > 0 && (
                                        <div style={{ fontSize: "0.7rem", color: colors.textMuted, marginTop: 2 }}>
                                            ≈ {formatInterval(fIntervalMin, lang)}
                                        </div>
                                    )}
                                </div>
                                <div style={{ display: "flex", gap: 8, marginBottom: 10 }}>
                                    <div style={{ flex: 1 }}>
                                        <label style={labelStyle}>{t("First run hour", "首次执行小时")} (0-23)</label>
                                        <input type="number" min={0} max={23} value={fHour} onChange={e => setFHour(Number(e.target.value))} style={inputStyle} />
                                    </div>
                                    <div style={{ flex: 1 }}>
                                        <label style={labelStyle}>{t("First run minute", "首次执行分钟")} (0-59)</label>
                                        <input type="number" min={0} max={59} value={fMinute} onChange={e => setFMinute(Number(e.target.value))} style={inputStyle} />
                                    </div>
                                </div>
                            </>
                        ) : (
                            <>
                                {/* Fixed time mode */}
                                <div style={{ display: "flex", gap: 8, marginBottom: 10 }}>
                                    <div style={{ flex: 1 }}>
                                        <label style={labelStyle}>{t("Hour", "小时")} (0-23)</label>
                                        <input type="number" min={0} max={23} value={fHour} onChange={e => setFHour(Number(e.target.value))} style={inputStyle} />
                                    </div>
                                    <div style={{ flex: 1 }}>
                                        <label style={labelStyle}>{t("Minute", "分钟")} (0-59)</label>
                                        <input type="number" min={0} max={59} value={fMinute} onChange={e => setFMinute(Number(e.target.value))} style={inputStyle} />
                                    </div>
                                </div>

                                <div style={{ display: "flex", gap: 8, marginBottom: 10 }}>
                                    <div style={{ flex: 1 }}>
                                        <label style={labelStyle}>{t("Day of Week", "星期")}</label>
                                        <select value={fDow} onChange={e => setFDow(Number(e.target.value))} style={{ ...inputStyle }}>
                                            <option value={-1}>{t("Every day", "每天")}</option>
                                            {getWeekdays(lang).map((d, i) => (
                                                <option key={i} value={i}>{d}</option>
                                            ))}
                                        </select>
                                    </div>
                                    <div style={{ flex: 1 }}>
                                        <label style={labelStyle}>{t("Day of Month", "每月几号")}</label>
                                        <select value={fDom} onChange={e => setFDom(Number(e.target.value))} style={{ ...inputStyle }}>
                                            <option value={-1}>{t("Any", "不限")}</option>
                                            {Array.from({ length: 31 }, (_, i) => i + 1).map(d => (
                                                <option key={d} value={d}>{d}</option>
                                            ))}
                                        </select>
                                    </div>
                                </div>
                            </>
                        )}

                        {/* Task type */}
                        <div style={{ marginBottom: 10 }}>
                            <label style={labelStyle}>{t("Task Type", "任务类型")}</label>
                            <select value={fTaskType} onChange={e => setFTaskType(e.target.value)} style={{ ...inputStyle }}>
                                <option value="reminder">{t("Reminder (skip if missed)", "提醒型（错过则跳过）")}</option>
                                <option value="process">{t("Process (catch up if missed)", "处理型（错过则补做）")}</option>
                            </select>
                        </div>

                        <div style={{ display: "flex", gap: 8, marginBottom: 14 }}>
                            <div style={{ flex: 1 }}>
                                <label style={labelStyle}>{t("Start Date (optional)", "开始日期（可选）")}</label>
                                <input type="date" value={fStartDate} onChange={e => setFStartDate(e.target.value)} style={inputStyle} />
                            </div>
                            <div style={{ flex: 1 }}>
                                <label style={labelStyle}>{t("End Date (optional)", "结束日期（可选）")}</label>
                                <input type="date" value={fEndDate} onChange={e => setFEndDate(e.target.value)} style={inputStyle} />
                            </div>
                        </div>

                        <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
                            <button onClick={() => setDlgOpen(false)} style={{ padding: "5px 14px", fontSize: "0.76rem", border: `1px solid ${colors.border}`, borderRadius: radius.md, background: colors.surface, color: colors.text, cursor: "pointer" }}>
                                {t("Cancel", "取消")}
                            </button>
                            <button onClick={handleSave} disabled={saving || !fName.trim() || !fAction.trim()} style={{
                                padding: "5px 14px", fontSize: "0.76rem", border: "none", borderRadius: radius.md,
                                background: colors.primary, color: "#fff", cursor: "pointer",
                                opacity: saving || !fName.trim() || !fAction.trim() ? 0.5 : 1,
                            }}>
                                {saving ? t("Saving…", "保存中…") : t("Save", "保存")}
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
}
