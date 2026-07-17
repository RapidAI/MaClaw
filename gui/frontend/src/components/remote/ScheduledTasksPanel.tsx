import { useState, useEffect, useCallback, useRef } from "react";
import {
    ListScheduledTasks,
    CreateScheduledTask,
    UpdateScheduledTask,
    DeleteScheduledTask,
    PauseScheduledTask,
    ResumeScheduledTask,
    TriggerScheduledTask,
    ListLansengerGroups,
} from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import { colors, radius, remoteActionButtonStyle, remoteDangerActionButtonStyle, remotePrimaryActionButtonStyle } from "./styles";
import { useSafeBackdropDismiss } from "../../hooks/useSafeBackdropDismiss";

interface DeliveryTarget {
    kind: "group" | "user";
    group_id?: string;
    group_name?: string;
    user_id?: string;
    mention_user_ids?: string[];
    mention_all?: boolean;
}

interface TaskDelivery {
    enabled: boolean;
    channel?: string;
    targets?: DeliveryTarget[];
    on?: string;
    prefix?: string;
    fail_on_error?: boolean;
}

type DeliveryChannel = "lansenger" | "weixin" | "telegram" | "qq";

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
    delivery?: TaskDelivery | null;
    status: string;
    created_at: string;
    last_run_at: string | null;
    next_run_at: string | null;
    run_count: number;
    last_result: string;
    last_error: string;
}

interface LansengerGroupRow {
    group_id: string;
    name: string;
}

function deliverySummary(d?: TaskDelivery | null): string {
    if (!d?.enabled || !d.targets?.length) return "";
    const ch = d.channel || "lansenger";
    const parts = d.targets.map((t) => {
        if (t.kind === "group") {
            const label = t.group_name || t.group_id || "?";
            const at = t.mention_all ? " @all" : (t.mention_user_ids?.length ? ` @${t.mention_user_ids.length}` : "");
            return `群:${label}${at}`;
        }
        const uid = t.user_id || "?";
        return uid === "self" || uid === "owner" || uid === "me" ? "私聊:自己" : `私聊:${uid}`;
    });
    const strict = d.fail_on_error ? " · 严格" : "";
    return `${ch} → ${parts.join(", ")}${strict}`;
}

/** Last-run push outcome for list badges (none | pending | ok | warn | fail). */
function deliveryRunStatus(task: ScheduledTask): "none" | "pending" | "ok" | "warn" | "fail" {
    if (!task.delivery?.enabled || !task.delivery.targets?.length) return "none";
    const result = String(task.last_result || "");
    if (result.includes("[投递警告]")) return "warn";
    const err = String(task.last_error || "");
    if (err && /delivery|投递|push fail|agent ok/i.test(err)) return "fail";
    if (task.run_count > 0 && !err) return "ok";
    if (task.run_count > 0) return "ok"; // agent may have failed; push config still valid
    return "pending";
}

function deliveryStatusLabel(st: ReturnType<typeof deliveryRunStatus>, lang: string): string {
    const zh = lang.startsWith("zh");
    switch (st) {
        case "ok": return zh ? "投递正常" : "Push OK";
        case "warn": return zh ? "投递警告" : "Push warn";
        case "fail": return zh ? "投递失败" : "Push fail";
        case "pending": return zh ? "待投递" : "Push pending";
        default: return "";
    }
}

function deliveryStatusColor(st: ReturnType<typeof deliveryRunStatus>): string {
    switch (st) {
        case "ok": return "var(--theme-success, #2a9d5c)";
        case "warn": return "var(--theme-warning, #c9a227)";
        case "fail": return colors.danger;
        case "pending": return colors.textMuted;
        default: return colors.border;
    }
}

function buildDeliveryForm(
    enabled: boolean,
    channel: DeliveryChannel,
    kind: "group" | "user",
    groupId: string,
    groupName: string,
    userId: string,
    mentionIds: string,
    mentionAll: boolean,
    failOnError: boolean,
): TaskDelivery | null {
    if (!enabled) return null;
    const ch = channel || "lansenger";
    if (ch === "weixin") {
        return {
            enabled: true,
            channel: "weixin",
            on: "success",
            fail_on_error: failOnError || undefined,
            targets: [{ kind: "user", user_id: "self" }],
        };
    }
    if (ch === "telegram" || ch === "qq") {
        const uid = (userId.trim() || "self");
        return {
            enabled: true,
            channel: ch,
            on: "success",
            fail_on_error: failOnError || undefined,
            targets: [{ kind: "user", user_id: uid }],
        };
    }
    const mentions = mentionIds.split(/[,，\s]+/).map((s) => s.trim()).filter(Boolean);
    if (kind === "group") {
        if (!groupId.trim()) return null;
        return {
            enabled: true,
            channel: "lansenger",
            on: "success",
            fail_on_error: failOnError || undefined,
            targets: [{
                kind: "group",
                group_id: groupId.trim(),
                group_name: groupName.trim() || undefined,
                mention_user_ids: mentionAll ? undefined : (mentions.length ? mentions : undefined),
                mention_all: mentionAll || undefined,
            }],
        };
    }
    if (!userId.trim()) return null;
    return {
        enabled: true,
        channel: "lansenger",
        on: "success",
        fail_on_error: failOnError || undefined,
        targets: [{ kind: "user", user_id: userId.trim() }],
    };
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

type Props = { lang: string; refreshKey?: number };
type WailsNoDragStyle = React.CSSProperties & {
    WebkitAppRegion?: "no-drag";
    "--wails-draggable"?: "no-drag";
};

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
    active: "var(--theme-success)",
    paused: "var(--theme-primary)",
    expired: "var(--theme-text-muted)",
};

export function ScheduledTasksPanel({ lang, refreshKey }: Props) {
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
    // Delivery (IM push) form
    const [fDeliveryOn, setFDeliveryOn] = useState(false);
    const [fDelChannel, setFDelChannel] = useState<DeliveryChannel>("lansenger");
    const [fDelKind, setFDelKind] = useState<"group" | "user">("group");
    const [fDelGroupId, setFDelGroupId] = useState("");
    const [fDelGroupName, setFDelGroupName] = useState("");
    const [fDelUserId, setFDelUserId] = useState("");
    const [fDelMentions, setFDelMentions] = useState("");
    const [fDelMentionAll, setFDelMentionAll] = useState(false);
    const [fDelFailOnError, setFDelFailOnError] = useState(false);
    const [lxGroups, setLxGroups] = useState<LansengerGroupRow[]>([]);
    const [lxGroupsLoading, setLxGroupsLoading] = useState(false);
    const [saving, setSaving] = useState(false);
    const [triggering, setTriggering] = useState<string | null>(null);
    const mountedRef = useRef(true);
    const loadRequestSeq = useRef(0);
    const { backdropProps: deleteBackdropProps, dialogProps: deleteDialogProps } = useSafeBackdropDismiss(() => setDeleteTarget(null));
    const { backdropProps: formBackdropProps, dialogProps: formDialogProps } = useSafeBackdropDismiss(() => setDlgOpen(false));

    useEffect(() => () => { mountedRef.current = false; }, []);

    const loadTasks = useCallback(async () => {
        const requestSeq = ++loadRequestSeq.current;
        setLoading(true); setError("");
        try {
            const list = await ListScheduledTasks();
            const all = Array.isArray(list) ? list : [];
            // Filter out expired tasks — they are auto-deleted on the backend,
            // but hide them immediately on the frontend as well.
            if (mountedRef.current && requestSeq === loadRequestSeq.current) {
                setTasks(all.filter((t: ScheduledTask) => t.status !== "expired"));
            }
        } catch (e) {
            if (mountedRef.current && requestSeq === loadRequestSeq.current) setError(String(e));
        } finally {
            if (mountedRef.current && requestSeq === loadRequestSeq.current) setLoading(false);
        }
    }, []);

    useEffect(() => { loadTasks(); }, [loadTasks, refreshKey]);

    // Refresh when tasks are changed from the agent chat side.
    useEffect(() => {
        EventsOn("scheduled-tasks-changed", loadTasks);
        return () => { EventsOff("scheduled-tasks-changed"); };
    }, [loadTasks]);

    const loadLxGroups = useCallback(async () => {
        setLxGroupsLoading(true);
        try {
            const res = await ListLansengerGroups();
            const groups = Array.isArray(res?.groups) ? res.groups : [];
            setLxGroups(groups.map((g: any) => ({
                group_id: String(g.group_id || g.GroupID || "").trim(),
                name: String(g.name || g.Name || g.group_id || "").trim(),
            })).filter((g: LansengerGroupRow) => g.group_id));
        } catch {
            setLxGroups([]);
        } finally {
            setLxGroupsLoading(false);
        }
    }, []);

    const resetDeliveryForm = () => {
        setFDeliveryOn(false);
        setFDelChannel("lansenger");
        setFDelKind("group");
        setFDelGroupId(""); setFDelGroupName(""); setFDelUserId("");
        setFDelMentions(""); setFDelMentionAll(false);
        setFDelFailOnError(false);
    };

    const openCreate = () => {
        setEditTask(null); setFName(""); setFAction(""); setFHour(9); setFMinute(0);
        setFDow(-1); setFDom(-1); setFIntervalMin(0); setFTaskType("reminder");
        setFStartDate(""); setFEndDate(""); setFScheduleMode("fixed");
        resetDeliveryForm(); setError(""); setDlgOpen(true);
        void loadLxGroups();
    };

    const openEdit = (task: ScheduledTask) => {
        setEditTask(task); setFName(task.name); setFAction(task.action);
        setFHour(task.hour); setFMinute(task.minute); setFDow(task.day_of_week);
        setFDom(task.day_of_month); setFIntervalMin(task.interval_minutes || 0);
        setFTaskType(task.task_type || "reminder");
        setFStartDate(task.start_date || ""); setFEndDate(task.end_date || "");
        setFScheduleMode(task.interval_minutes > 0 ? "interval" : "fixed");
        const d = task.delivery;
        if (d?.enabled && d.targets?.[0]) {
            const tg = d.targets[0];
            const chRaw = (d.channel || "lansenger").toLowerCase();
            const ch = (["weixin", "telegram", "qq"].includes(chRaw) ? chRaw : "lansenger") as DeliveryChannel;
            setFDeliveryOn(true);
            setFDelChannel(ch);
            setFDelFailOnError(!!d.fail_on_error);
            if (ch === "weixin") {
                setFDelKind("user");
                setFDelUserId("self");
                setFDelGroupId(""); setFDelGroupName(""); setFDelMentions(""); setFDelMentionAll(false);
            } else if (ch === "telegram" || ch === "qq") {
                setFDelKind("user");
                setFDelUserId(tg.user_id || "self");
                setFDelGroupId(""); setFDelGroupName(""); setFDelMentions(""); setFDelMentionAll(false);
            } else {
                setFDelKind(tg.kind === "user" ? "user" : "group");
                setFDelGroupId(tg.group_id || "");
                setFDelGroupName(tg.group_name || "");
                setFDelUserId(tg.user_id || "");
                setFDelMentions((tg.mention_user_ids || []).join(", "));
                setFDelMentionAll(!!tg.mention_all);
            }
        } else {
            resetDeliveryForm();
        }
        setError(""); setDlgOpen(true);
        void loadLxGroups();
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
        if (fDeliveryOn && fDelChannel === "lansenger") {
            if (fDelKind === "group" && !fDelGroupId.trim()) {
                setError(t("Select a Lansenger group for delivery", "请选择要推送的蓝信群"));
                return;
            }
            if (fDelKind === "user" && !fDelUserId.trim()) {
                setError(t("Enter a Lansenger user id for delivery", "请填写要推送的蓝信用户 ID"));
                return;
            }
        }
        if (fDeliveryOn && (fDelChannel === "telegram" || fDelChannel === "qq") && !fDelUserId.trim()) {
            setFDelUserId("self");
        }
        const taskType = fTaskType || "reminder";
        const delivery = buildDeliveryForm(
            fDeliveryOn, fDelChannel, fDelKind, fDelGroupId, fDelGroupName, fDelUserId,
            fDelMentions, fDelMentionAll, fDelFailOnError,
        );
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
                    delivery: fDeliveryOn ? delivery : null,
                });
            } else {
                const id = await CreateScheduledTask(
                    fName.trim(), fAction.trim(), hour, minute,
                    fScheduleMode === "fixed" ? fDow : -1,
                    fScheduleMode === "fixed" ? fDom : -1,
                    intervalMinutes,
                    fStartDate, fEndDate, taskType,
                );
                if (fDeliveryOn && delivery && id) {
                    await UpdateScheduledTask(id, { delivery });
                }
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
                <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                    <button onClick={loadTasks} disabled={loading} style={{
                        ...remoteActionButtonStyle,
                        padding: "4px 12px", fontSize: "0.76rem",
                        opacity: loading ? 0.6 : 1,
                    }}>
                        {t("Refresh", "刷新")}
                    </button>
                    <button onClick={openCreate} style={{
                        ...remotePrimaryActionButtonStyle,
                        padding: "4px 14px", fontSize: "0.76rem",
                    }}>
                        + {t("New", "新建")}
                    </button>
                </div>
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
                                    {(() => {
                                        const st = deliveryRunStatus(task);
                                        if (st === "none") return null;
                                        const c = deliveryStatusColor(st);
                                        return (
                                            <span title={deliverySummary(task.delivery) || undefined} style={{
                                                fontSize: "0.62rem", fontWeight: 600, padding: "1px 5px",
                                                borderRadius: radius.sm, color: c,
                                                border: `1px solid ${c}`,
                                                background: st === "warn" || st === "fail" ? "transparent" : "transparent",
                                            }}>{deliveryStatusLabel(st, lang)}</span>
                                        );
                                    })()}
                                    <span style={{ fontSize: "0.8rem", fontWeight: 600, color: colors.text }}>{task.name}</span>
                                </div>
                                <div style={{ fontSize: "0.72rem", color: colors.textMuted, wordBreak: "break-word", maxHeight: 120, overflowY: "auto", whiteSpace: "pre-wrap" }}>
                                    Action: {task.action}
                                    {"\n"}Schedule: {scheduleDesc(task, lang)}
                                    {deliverySummary(task.delivery) && <>{"\n"}{t("Push", "推送")}: {deliverySummary(task.delivery)}</>}
                                    {task.next_run_at && <>{"\n"}{t("Next", "下次")}: {fmtDate(task.next_run_at, lang)}</>}
                                    {task.run_count > 0 && <>{" · "}{t("Runs", "已执行")}: {task.run_count}</>}
                                </div>
                                {task.last_result && (
                                    <div style={{
                                        fontSize: "0.66rem", marginTop: 4, color: colors.textSecondary,
                                        maxHeight: 72, overflowY: "auto", whiteSpace: "pre-wrap", wordBreak: "break-word",
                                        borderLeft: `2px solid ${String(task.last_result).includes("[投递警告]") ? "var(--theme-warning, #c9a227)" : colors.border}`,
                                        paddingLeft: 6,
                                    }}>
                                        {t("Last result", "最近结果")}: {String(task.last_result).length > 400
                                            ? `${String(task.last_result).slice(0, 400)}…`
                                            : task.last_result}
                                    </div>
                                )}
                                {task.last_error && (
                                    <div style={{ fontSize: "0.68rem", color: colors.danger, marginTop: 2 }}>
                                        {t("Error", "错误")}: {task.last_error}
                                    </div>
                                )}
                            </div>
                            <div style={{ display: "flex", gap: 4, flexShrink: 0 }}>
                                {task.status === "active" && (
                                    <button onClick={() => handleTrigger(task.id)}
                                        disabled={triggering === task.id}
                                        title={t("Run Now", "立即运行")}
                                        style={{ padding: "3px 8px", fontSize: "0.7rem", cursor: "pointer", background: "none", border: `1px solid ${colors.border}`, borderRadius: radius.sm, color: colors.primary, opacity: triggering === task.id ? 0.5 : 1 }}>
                                        {triggering === task.id ? "..." : "RUN"}
                                    </button>
                                )}
                                {task.status !== "expired" && (
                                    <button onClick={() => handleTogglePause(task)}
                                        title={task.status === "active" ? t("Pause", "暂停") : t("Resume", "恢复")}
                                        style={{ padding: "3px 8px", fontSize: "0.7rem", cursor: "pointer", background: "none", border: `1px solid ${colors.border}`, borderRadius: radius.sm, color: colors.textSecondary }}>
                                        {task.status === "active" ? "PAUSE" : "RUN"}
                                    </button>
                                )}
                                <button onClick={() => openEdit(task)} title={t("Edit", "编辑")}
                                    style={{ padding: "3px 8px", fontSize: "0.7rem", cursor: "pointer", background: "none", border: `1px solid ${colors.border}`, borderRadius: radius.sm, color: colors.textSecondary }}>
                                    EDIT
                                </button>
                                <button onClick={() => setDeleteTarget(task.id)} title={t("Delete", "删除")}
                                    style={{ padding: "3px 8px", fontSize: "0.7rem", cursor: "pointer", background: "none", border: `1px solid ${colors.border}`, borderRadius: radius.sm, color: colors.danger }}>
                                    DEL
                                </button>
                            </div>
                        </div>
                    </div>
                ))}
            </div>

            {/* Delete confirmation */}
            {deleteTarget && (
                <div
                    style={{ position: "fixed", inset: 0, background: colors.overlay, display: "flex", alignItems: "center", justifyContent: "center", zIndex: 9999, WebkitAppRegion: "no-drag", "--wails-draggable": "no-drag" } as WailsNoDragStyle}
                    {...deleteBackdropProps}
                >
                    <div
                        role="dialog"
                        aria-modal="true"
                        style={{ background: colors.surface, borderRadius: radius.lg, padding: "20px 24px", minWidth: 280, boxShadow: "0 8px 30px rgba(0,0,0,0.12)", WebkitAppRegion: "no-drag", "--wails-draggable": "no-drag" } as WailsNoDragStyle}
                        {...deleteDialogProps}
                    >
                        <p style={{ fontSize: "0.82rem", marginBottom: 16 }}>{t("Delete this scheduled task?", "确定删除这个定时任务？")}</p>
                        <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
                            <button onClick={() => setDeleteTarget(null)} style={{ ...remoteActionButtonStyle, padding: "5px 14px", fontSize: "0.76rem" }}>{t("Cancel", "取消")}</button>
                            <button onClick={() => handleDelete(deleteTarget)} style={{ ...remoteDangerActionButtonStyle, padding: "5px 14px", fontSize: "0.76rem" }}>{t("Delete", "删除")}</button>
                        </div>
                    </div>
                </div>
            )}

            {/* Create / Edit dialog */}
            {dlgOpen && (
                <div
                    style={{ position: "fixed", inset: 0, background: colors.overlay, display: "flex", alignItems: "center", justifyContent: "center", zIndex: 9999, WebkitAppRegion: "no-drag", "--wails-draggable": "no-drag" } as WailsNoDragStyle}
                    {...formBackdropProps}
                >
                    <div
                        role="dialog"
                        aria-modal="true"
                        style={{
                        background: colors.surface, borderRadius: radius.lg, width: 440, maxWidth: "90vw",
                        maxHeight: "85vh", boxShadow: "0 8px 30px rgba(0,0,0,0.12)",
                        display: "flex", flexDirection: "column",
                        WebkitAppRegion: "no-drag", "--wails-draggable": "no-drag",
                    } as WailsNoDragStyle}
                        {...formDialogProps}
                    >
                        {/* Dialog header — fixed */}
                        <div style={{ padding: "14px 18px 8px", flexShrink: 0 }}>
                            <h4 style={{ fontSize: "0.8rem", margin: 0, color: colors.text }}>
                                {editTask ? t("Edit Scheduled Task", "编辑定时任务") : t("New Scheduled Task", "新建定时任务")}
                            </h4>
                        </div>

                        {/* Dialog body — scrollable */}
                        <div style={{ flex: 1, overflowY: "auto", padding: "0 18px", minHeight: 0 }}>
                            {error && <div role="alert" style={{ color: colors.danger, fontSize: "0.74rem", marginBottom: 6 }}>{error}</div>}

                            <div style={{ marginBottom: 6 }}>
                                <label style={labelStyle}>{t("Task Name", "任务名称")}</label>
                                <input value={fName} onChange={e => setFName(e.target.value)} placeholder={t("e.g. Daily code review", "如：每日代码审查")} style={inputStyle} />
                            </div>

                            <div style={{ marginBottom: 6 }}>
                                <label style={labelStyle}>{t("Action", "执行内容")}</label>
                                <textarea value={fAction} onChange={e => setFAction(e.target.value)} rows={2}
                                    placeholder={t("e.g. Run tests and report failures", "如：检查测试是否通过，失败则发送报告")}
                                    style={{ ...inputStyle, resize: "vertical", fontFamily: "inherit" }} />
                            </div>

                            {/* Schedule mode + Task type — same row */}
                            <div style={{ display: "flex", gap: 8, marginBottom: 6 }}>
                                <div style={{ flex: 1 }}>
                                    <label style={labelStyle}>{t("Mode", "模式")}</label>
                                    <div style={{ display: "flex", gap: 0, borderRadius: 4, overflow: "hidden", border: `1px solid ${colors.border}` }}>
                                        <button type="button" onClick={() => setFScheduleMode("fixed")} style={{
                                            flex: 1, padding: "5px 0", fontSize: "0.72rem", cursor: "pointer", border: "none",
                                            background: fScheduleMode === "fixed" ? colors.primaryLight : colors.surface,
                                            color: fScheduleMode === "fixed" ? colors.primaryDark : colors.text,
                                            fontWeight: fScheduleMode === "fixed" ? 600 : 400,
                                        }}>
                                            {t("Fixed", "固定")}
                                        </button>
                                        <button type="button" onClick={() => setFScheduleMode("interval")} style={{
                                            flex: 1, padding: "5px 0", fontSize: "0.72rem", cursor: "pointer", border: "none",
                                            borderLeft: `1px solid ${colors.border}`,
                                            background: fScheduleMode === "interval" ? colors.primaryLight : colors.surface,
                                            color: fScheduleMode === "interval" ? colors.primaryDark : colors.text,
                                            fontWeight: fScheduleMode === "interval" ? 600 : 400,
                                        }}>
                                            {t("Interval", "间隔")}
                                        </button>
                                    </div>
                                </div>
                                <div style={{ flex: 1 }}>
                                    <label style={labelStyle}>{t("Type", "类型")}</label>
                                    <select value={fTaskType} onChange={e => setFTaskType(e.target.value)} style={{ ...inputStyle, padding: "5px 8px" }}>
                                        <option value="reminder">{t("Reminder (skip)", "提醒（错过跳过）")}</option>
                                        <option value="process">{t("Process (catch up)", "处理（错过补做）")}</option>
                                    </select>
                                </div>
                            </div>

                            {fScheduleMode === "interval" ? (
                                <>
                                    <div style={{ display: "flex", gap: 8, marginBottom: 6 }}>
                                        <div style={{ flex: 2 }}>
                                            <label style={labelStyle}>{t("Every (min)", "间隔（分钟）")}</label>
                                            <input type="number" min={1} value={fIntervalMin || ""} onChange={e => setFIntervalMin(Number(e.target.value))}
                                                placeholder="60" style={inputStyle} />
                                        </div>
                                        <div style={{ flex: 1 }}>
                                            <label style={labelStyle}>{t("Hour", "时")}</label>
                                            <input type="number" min={0} max={23} value={fHour} onChange={e => setFHour(Number(e.target.value))} style={inputStyle} />
                                        </div>
                                        <div style={{ flex: 1 }}>
                                            <label style={labelStyle}>{t("Min", "分")}</label>
                                            <input type="number" min={0} max={59} value={fMinute} onChange={e => setFMinute(Number(e.target.value))} style={inputStyle} />
                                        </div>
                                    </div>
                                    {fIntervalMin > 0 && (
                                        <div style={{ fontSize: "0.68rem", color: colors.textMuted, marginTop: -4, marginBottom: 6 }}>
                                            ≈ {t("every", "每")}{formatInterval(fIntervalMin, lang)}{t(`, first at ${String(fHour).padStart(2,"0")}:${String(fMinute).padStart(2,"0")}`, `，首次 ${String(fHour).padStart(2,"0")}:${String(fMinute).padStart(2,"0")}`)}
                                        </div>
                                    )}
                                </>
                            ) : (
                                <div style={{ display: "flex", gap: 8, marginBottom: 6 }}>
                                    <div style={{ flex: 1 }}>
                                        <label style={labelStyle}>{t("Hour", "时")} (0-23)</label>
                                        <input type="number" min={0} max={23} value={fHour} onChange={e => setFHour(Number(e.target.value))} style={inputStyle} />
                                    </div>
                                    <div style={{ flex: 1 }}>
                                        <label style={labelStyle}>{t("Min", "分")} (0-59)</label>
                                        <input type="number" min={0} max={59} value={fMinute} onChange={e => setFMinute(Number(e.target.value))} style={inputStyle} />
                                    </div>
                                    <div style={{ flex: 1 }}>
                                        <label style={labelStyle}>{t("Weekday", "星期")}</label>
                                        <select value={fDow} onChange={e => setFDow(Number(e.target.value))} style={{ ...inputStyle }}>
                                            <option value={-1}>{t("All", "每天")}</option>
                                            {getWeekdays(lang).map((d, i) => (
                                                <option key={i} value={i}>{d}</option>
                                            ))}
                                        </select>
                                    </div>
                                    <div style={{ flex: 1 }}>
                                        <label style={labelStyle}>{t("Date", "几号")}</label>
                                        <select value={fDom} onChange={e => setFDom(Number(e.target.value))} style={{ ...inputStyle }}>
                                            <option value={-1}>{t("Any", "不限")}</option>
                                            {Array.from({ length: 31 }, (_, i) => i + 1).map(d => (
                                                <option key={d} value={d}>{d}</option>
                                            ))}
                                        </select>
                                    </div>
                                </div>
                            )}

                            {/* Start date + End date */}
                            <div style={{ display: "flex", gap: 8, marginBottom: 6 }}>
                                <div style={{ flex: 1 }}>
                                    <label style={labelStyle}>{t("Start", "开始日期")}</label>
                                    <input type="date" value={fStartDate} onChange={e => setFStartDate(e.target.value)} style={inputStyle} />
                                </div>
                                <div style={{ flex: 1 }}>
                                    <label style={labelStyle}>{t("End", "结束日期")}</label>
                                    <input type="date" value={fEndDate} onChange={e => setFEndDate(e.target.value)} style={inputStyle} />
                                </div>
                            </div>

                            {/* Result delivery */}
                            <div style={{ marginBottom: 8, paddingTop: 6, borderTop: `1px solid ${colors.border}` }}>
                                <label style={{ ...labelStyle, display: "flex", alignItems: "center", gap: 6, cursor: "pointer" }}>
                                    <input type="checkbox" checked={fDeliveryOn} onChange={e => setFDeliveryOn(e.target.checked)} />
                                    {t("Push result to IM", "结果推送到 IM")}
                                </label>
                                {fDeliveryOn && (
                                    <div style={{ marginTop: 6, display: "flex", flexDirection: "column", gap: 6 }}>
                                        <div style={{ display: "flex", gap: 8 }}>
                                            <div style={{ flex: 1 }}>
                                                <label style={labelStyle}>{t("Channel", "通道")}</label>
                                                <select
                                                    value={fDelChannel}
                                                    onChange={e => {
                                                        const ch = e.target.value as DeliveryChannel;
                                                        setFDelChannel(ch);
                                                        if (ch === "weixin" || ch === "telegram" || ch === "qq") {
                                                            setFDelKind("user");
                                                            if (!fDelUserId.trim() || ch === "weixin") setFDelUserId("self");
                                                        } else if (fDelUserId === "self") {
                                                            setFDelUserId("");
                                                            setFDelKind("group");
                                                        }
                                                    }}
                                                    style={{ ...inputStyle, padding: "5px 8px" }}
                                                >
                                                    <option value="lansenger">{t("Lansenger", "蓝信")}</option>
                                                    <option value="weixin">{t("WeChat (owner chat)", "微信（主人私聊）")}</option>
                                                    <option value="telegram">{t("Telegram", "Telegram")}</option>
                                                    <option value="qq">{t("QQ", "QQ")}</option>
                                                </select>
                                            </div>
                                            {fDelChannel === "lansenger" && (
                                                <div style={{ flex: 1 }}>
                                                    <label style={labelStyle}>{t("Target", "目标")}</label>
                                                    <select value={fDelKind} onChange={e => setFDelKind(e.target.value as "group" | "user")} style={{ ...inputStyle, padding: "5px 8px" }}>
                                                        <option value="group">{t("Group", "群")}</option>
                                                        <option value="user">{t("Private user", "私聊某人")}</option>
                                                    </select>
                                                </div>
                                            )}
                                        </div>
                                        {fDelChannel === "weixin" ? (
                                            <div style={{ fontSize: "0.68rem", color: colors.textMuted, lineHeight: 1.45 }}>
                                                {t(
                                                    "Pushes to your last active WeChat private chat with the bot. Chat with the bot once first.",
                                                    "推送到你与机器人最近一次微信私聊会话。首次需私聊一次；会话会记住，重启 Maclaw 后仍可用。",
                                                )}
                                            </div>
                                        ) : fDelChannel === "telegram" || fDelChannel === "qq" ? (
                                            <div>
                                                <label style={labelStyle}>
                                                    {fDelChannel === "telegram"
                                                        ? t("Chat ID (or self)", "Chat ID（或 self=最近会话）")
                                                        : t("OpenID (or self)", "OpenID（或 self=最近私聊）")}
                                                </label>
                                                <input value={fDelUserId} onChange={e => setFDelUserId(e.target.value)}
                                                    placeholder={fDelChannel === "telegram" ? "self or 123456789" : "self or openid"}
                                                    style={inputStyle} />
                                                <div style={{ fontSize: "0.66rem", color: colors.textMuted, marginTop: 2 }}>
                                                    {fDelChannel === "telegram"
                                                        ? t("self = last chat that messaged the bot. Or paste a numeric chat_id.", "self = 最近联系过机器人的会话；也可填数字 chat_id。")
                                                        : t("self = last QQ user who messaged the bot. Or paste openid.", "self = 最近私聊过机器人的用户；也可填 openid。")}
                                                </div>
                                            </div>
                                        ) : fDelKind === "group" ? (
                                            <>
                                                <div>
                                                    <label style={labelStyle}>
                                                        {t("Group", "蓝信群")}
                                                        {lxGroupsLoading ? ` (${t("loading…", "加载中…")})` : ""}
                                                    </label>
                                                    <select
                                                        value={fDelGroupId}
                                                        onChange={e => {
                                                            const id = e.target.value;
                                                            setFDelGroupId(id);
                                                            const g = lxGroups.find(x => x.group_id === id);
                                                            setFDelGroupName(g?.name || "");
                                                        }}
                                                        style={{ ...inputStyle, padding: "5px 8px" }}
                                                    >
                                                        <option value="">{t("Select group…", "选择群…")}</option>
                                                        {lxGroups.map(g => (
                                                            <option key={g.group_id} value={g.group_id}>{g.name || g.group_id}</option>
                                                        ))}
                                                    </select>
                                                    {!lxGroups.length && !lxGroupsLoading && (
                                                        <div style={{ fontSize: "0.66rem", color: colors.textMuted, marginTop: 2 }}>
                                                            {t("No groups found. Ensure Lansenger bot has joined groups.", "未拉到群列表。请确认蓝信机器人已入群且凭证有效。")}
                                                        </div>
                                                    )}
                                                </div>
                                                <div>
                                                    <label style={labelStyle}>{t("Mention user IDs (optional)", "@ 用户 ID（可选，逗号分隔）")}</label>
                                                    <input value={fDelMentions} onChange={e => setFDelMentions(e.target.value)}
                                                        disabled={fDelMentionAll}
                                                        placeholder="staffId1, staffId2" style={inputStyle} />
                                                </div>
                                                <label style={{ ...labelStyle, display: "flex", alignItems: "center", gap: 6, cursor: "pointer", marginBottom: 0 }}>
                                                    <input type="checkbox" checked={fDelMentionAll} onChange={e => setFDelMentionAll(e.target.checked)} />
                                                    {t("@all in group", "群内 @所有人")}
                                                </label>
                                            </>
                                        ) : (
                                            <div>
                                                <label style={labelStyle}>{t("User ID (staffId)", "用户 ID（staffId）")}</label>
                                                <input value={fDelUserId} onChange={e => setFDelUserId(e.target.value)}
                                                    placeholder="staffId" style={inputStyle} />
                                            </div>
                                        )}
                                        <label style={{ ...labelStyle, display: "flex", alignItems: "center", gap: 6, cursor: "pointer", marginBottom: 0 }}>
                                            <input type="checkbox" checked={fDelFailOnError} onChange={e => setFDelFailOnError(e.target.checked)} />
                                            {t("Fail task if push fails", "推送失败则任务记为失败")}
                                        </label>
                                        <div style={{ fontSize: "0.66rem", color: colors.textMuted }}>
                                            {t(
                                                "Agent runs the Action first, then the result is pushed. By default push errors only add a warning.",
                                                "到点先由 Agent 执行内容，再推送结果。默认推送失败只写警告，任务仍算成功。",
                                            )}
                                        </div>
                                    </div>
                                )}
                            </div>
                        </div>

                        {/* Dialog footer — fixed */}
                        <div style={{ padding: "10px 18px 14px", flexShrink: 0, display: "flex", justifyContent: "flex-end", gap: 8, borderTop: `1px solid ${colors.border}` }}>
                            <button onClick={() => setDlgOpen(false)} style={{ padding: "5px 14px", fontSize: "0.76rem", border: `1px solid ${colors.border}`, borderRadius: radius.md, background: colors.surface, color: colors.text, cursor: "pointer" }}>
                                {t("Cancel", "取消")}
                            </button>
                            <button onClick={handleSave} disabled={saving || !fName.trim() || !fAction.trim()} style={{
                                ...remotePrimaryActionButtonStyle,
                                padding: "5px 14px", fontSize: "0.76rem",
                                cursor: saving || !fName.trim() || !fAction.trim() ? "default" : "pointer",
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
