import {
    immediateRunWindow,
    type RunWindow,
    type TaskDraft,
    type WorkspaceKind,
} from "./taskDraft";
import { tailText } from "../../../i18n/mnTail"
import { normalizeLang } from "../../../i18n/langSelect"

export interface RunWindowDraftPayload {
    taskType: string;
    expertId: string;
    expertName: string;
    workflowTemplateId: string;
    workspaceKind: string;
    localPath: string;
    cloudWorkspaceId: string;
    cloudName: string;
    remoteHost: string;
    remotePort: number;
    remoteUser: string;
    remoteWorkDir: string;
    workflowParams?: Record<string, string>;
    latexTemplateId: string;
    latexTemplateName: string;
}

export interface RunWindowTaskRecord {
    id: string;
    name: string;
    prompt: string;
    initialMessage: string;
    draft: RunWindowDraftPayload;
    status: string;
    startAt: string;
    endAt?: string;
    projectPath?: string;
    sessionUserId?: string;
    createdAt: string;
    lastError?: string;
}

export interface RunWindowCreateRequest {
    name: string;
    prompt: string;
    initialMessage: string;
    draft: RunWindowDraftPayload;
    startAt: string;
    endAt: string;
}

export function isRunWindowScheduled(draft: TaskDraft | null | undefined): boolean {
    return draft?.runWindow?.mode === "scheduled";
}

export function padTime(value: number): string {
    return String(value).padStart(2, "0");
}

export function toDatetimeLocalValue(date: Date): string {
    return `${date.getFullYear()}-${padTime(date.getMonth() + 1)}-${padTime(date.getDate())}T${padTime(date.getHours())}:${padTime(date.getMinutes())}`;
}

export function nextHourLocalValue(now = new Date()): string {
    const next = new Date(now);
    next.setMinutes(0, 0, 0);
    next.setHours(next.getHours() + 1);
    return toDatetimeLocalValue(next);
}

export function shortRunWindowStamp(value: string): string {
    const matched = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(value);
    if (!matched) return value;
    return `${matched[2]}-${matched[3]} ${matched[4]}:${matched[5]}`;
}

export function runWindowChipLabel(window: RunWindow | undefined, isZh: boolean | string): string {
    if (!window || window.mode !== "scheduled") return tailText((typeof isZh === "string" ? isZh : (isZh ? "zh-Hans" : "en")), "Now", "立即");
    if (!window.startAt) return tailText((typeof isZh === "string" ? isZh : (isZh ? "zh-Hans" : "en")), "Scheduled", "定时");
    const start = shortRunWindowStamp(window.startAt);
    if (!window.endAt) return start;
    const sameDay = window.startAt.slice(0, 10) === window.endAt.slice(0, 10);
    const end = sameDay ? window.endAt.slice(11, 16) : shortRunWindowStamp(window.endAt);
    return `${start}–${end}`;
}

export type RunWindowCheck = { ok: true } | { ok: false; error: string };

export function validateRunWindow(window: RunWindow | undefined, now = new Date(), isZh: boolean | string = true): RunWindowCheck {
    if (!window || window.mode !== "scheduled") return { ok: true };
    if (!window.startAt) return { ok: false, error: tailText((typeof isZh === "string" ? isZh : (isZh ? "zh-Hans" : "en")), "Set a start time", "请设置开始时间") };
    const start = new Date(window.startAt);
    if (Number.isNaN(start.getTime())) return { ok: false, error: tailText((typeof isZh === "string" ? isZh : (isZh ? "zh-Hans" : "en")), "Start time is invalid", "开始时间无效") };
    if (start.getTime() < now.getTime() - 60_000) {
        return { ok: false, error: tailText((typeof isZh === "string" ? isZh : (isZh ? "zh-Hans" : "en")), "Start time must be in the future", "开始时间需要晚于当前时间") };
    }
    if (window.endAt) {
        const end = new Date(window.endAt);
        if (Number.isNaN(end.getTime())) return { ok: false, error: tailText((typeof isZh === "string" ? isZh : (isZh ? "zh-Hans" : "en")), "End time is invalid", "结束时间无效") };
        if (end.getTime() <= start.getTime()) {
            return { ok: false, error: tailText((typeof isZh === "string" ? isZh : (isZh ? "zh-Hans" : "en")), "End time must be after the start time", "结束时间需要晚于开始时间") };
        }
    }
    return { ok: true };
}

export function runWindowCreateRequest(name: string, prompt: string, initialMessage: string, draft: TaskDraft): RunWindowCreateRequest {
    const workspace = draft.workspace;
    const remote = workspace.kind === "remote" ? workspace.remote : undefined;
    const window = draft.runWindow || immediateRunWindow();
    return {
        name,
        prompt,
        initialMessage,
        startAt: window.startAt,
        endAt: window.endAt,
        draft: {
            taskType: draft.taskType,
            expertId: draft.expertId || "",
            expertName: draft.expertName || "",
            workflowTemplateId: draft.workflowTemplateId || "",
            workspaceKind: workspace.kind,
            localPath: workspace.localPath || "",
            cloudWorkspaceId: workspace.cloudWorkspaceId || "",
            cloudName: workspace.cloudName || "",
            remoteHost: remote?.host || "",
            remotePort: remote?.port || 0,
            remoteUser: remote?.user || "",
            remoteWorkDir: remote?.workDir || "",
            workflowParams: { ...draft.workflowParams },
            latexTemplateId: draft.latexTemplateId || "",
            latexTemplateName: draft.latexTemplateName || "",
        },
    };
}

export function taskDraftFromRunWindow(task: RunWindowTaskRecord): TaskDraft {
    const stored = task.draft || ({} as RunWindowDraftPayload);
    const kind = (stored.workspaceKind === "cloud" || stored.workspaceKind === "remote"
        ? stored.workspaceKind
        : "local") as WorkspaceKind;
    return {
        name: task.name || "",
        taskType: stored.taskType === "coding" ? "coding" : "chat",
        expertId: stored.expertId || null,
        expertName: stored.expertName || null,
        workflowTemplateId: stored.workflowTemplateId ? stored.workflowTemplateId : null,
        workspace: {
            kind,
            localPath: stored.localPath || undefined,
            cloudWorkspaceId: stored.cloudWorkspaceId || undefined,
            cloudName: stored.cloudName || undefined,
            remote: kind === "remote" ? {
                host: stored.remoteHost || "",
                port: stored.remotePort || 22,
                user: stored.remoteUser || "",
                password: "",
                workDir: stored.remoteWorkDir || "",
            } : undefined,
        },
        workflowParams: { ...(stored.workflowParams || {}) },
        latexTemplateId: stored.latexTemplateId || null,
        latexTemplateName: stored.latexTemplateName || null,
        runWindow: immediateRunWindow(),
    };
}

export function isRunWindowDue(task: RunWindowTaskRecord, now = new Date()): boolean {
    if (task.status !== "pending" || !task.startAt) return false;
    const start = new Date(task.startAt).getTime();
    if (Number.isNaN(start) || start > now.getTime()) return false;
    if (task.endAt) {
        const end = new Date(task.endAt).getTime();
        if (!Number.isNaN(end) && end <= now.getTime()) return false;
    }
    return true;
}

export function runWindowStatusLabel(status: string, isZh: boolean | string): string {
    switch (status) {
        case "pending":
            return tailText((typeof isZh === "string" ? isZh : (isZh ? "zh-Hans" : "en")), "Waiting", "等待开始");
        case "running":
            return tailText((typeof isZh === "string" ? isZh : (isZh ? "zh-Hans" : "en")), "Running", "执行中");
        case "completed":
            return tailText((typeof isZh === "string" ? isZh : (isZh ? "zh-Hans" : "en")), "Done", "已完成");
        case "missed":
            return tailText((typeof isZh === "string" ? isZh : (isZh ? "zh-Hans" : "en")), "Missed", "时段已过");
        case "failed":
            return tailText((typeof isZh === "string" ? isZh : (isZh ? "zh-Hans" : "en")), "Failed", "启动失败");
        case "stopped":
            return tailText((typeof isZh === "string" ? isZh : (isZh ? "zh-Hans" : "en")), "Paused", "已暂停");
        default:
            return status || (tailText((typeof isZh === "string" ? isZh : (isZh ? "zh-Hans" : "en")), "Unknown", "未知"));
    }
}

function runWindowLang(isZh: boolean | string): string {
    return typeof isZh === "string" ? isZh : (isZh ? "zh-Hans" : "en");
}

function runWindowChinese(isZh: boolean | string): boolean {
    if (typeof isZh !== "string") return !!isZh;
    const normalized = normalizeLang(isZh);
    return normalized === "zh-Hans" || normalized === "zh-Hant";
}

function quoteDialogName(name: string, isZh: boolean | string): string {
    const taskName = name.replace(/[\r\n]+/g, " ").trim() || (tailText(runWindowLang(isZh), "Untitled task", "未命名任务"));
    // The dialog chips one matched pair. A name that already contains the
    // delimiter would close that pair early and cut the start time off.
    if (runWindowChinese(isZh)) return /[「」]/.test(taskName) ? taskName : `「${taskName}」`;
    return /["“”]/.test(taskName) ? taskName : `"${taskName}"`;
}

export function runWindowCreatedNotice(name: string, startAt: string, endAt: string | undefined, isZh: boolean | string): { title: string; message: string } {
    const start = formatRunWindowInstant(startAt);
    const quoted = quoteDialogName(name, isZh);
    if (runWindowChinese(isZh)) {
        const lines = [`定时任务${quoted}已经创建，将于 ${start} 启动。`];
        if (endAt) lines.push(`${formatRunWindowInstant(endAt)} 如果任务未完成，将暂停。`);
        return { title: "定时任务已创建", message: lines.join("\n") };
    }
    const lang = runWindowLang(isZh);
    const endLabel = endAt ? formatRunWindowInstant(endAt) : "";
    const lines = [tailText(lang, `Scheduled task ${quoted} has been created and will start at ${start}.`, `定时任务${quoted}已经创建，将于 ${start} 启动。`)];
    if (endAt) lines.push(tailText(lang, `If it is still unfinished at ${endLabel}, it will pause.`, `${endLabel} 如果任务未完成，将暂停。`));
    return { title: tailText(lang, "Scheduled task created", "定时任务已创建"), message: lines.join("\n") };
}

export function formatRunWindowInstant(value: string | undefined): string {
    if (!value) return "";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return toDatetimeLocalValue(date).replace("T", " ");
}
