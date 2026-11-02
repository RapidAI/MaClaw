import type { Theme } from "./aiAssistantPanelTheme";
import { normalizeLang } from "../../i18n/langSelect";
import { tailText } from "../../i18n/mnTail";

export type ChatTaskStatus = "queued" | "running" | "needs_input" | "done" | "failed" | "canceled" | "interrupted" | string;

export interface ChatTaskView {
    id: string;
    sessionKey: string;
    title: string;
    prompt?: string;
    status: ChatTaskStatus;
    progress?: string;
    question?: string;
    resultText?: string;
    error?: string;
    artifactPaths?: string[];
    recovered?: boolean;
    updatedAtUnix?: number;
}

export interface ChatTaskResultPayload {
    text?: string;
    request_id?: string;
    session_key?: string;
    response_source?: string;
    chat_task_id?: string;
    chat_task_kind?: string;
    local_file_path?: string;
    local_file_paths?: string[];
}

const TASK_EVENT = "ai-assistant-task";

export function chatTaskEventName(): string {
    return TASK_EVENT;
}

export function isChatTaskResultResponse(response: ChatTaskResultPayload | null | undefined): boolean {
    if (!response) return false;
    const source = String(response.response_source || "").trim();
    const kind = String(response.chat_task_kind || "").trim();
    const requestID = String(response.request_id || "").trim();
    return source === "chat_task_result" || kind === "task_result" || requestID.startsWith("chat-task-result-");
}

export function chatTaskTerminal(status: string): boolean {
    return status === "done" || status === "failed" || status === "canceled" || status === "interrupted";
}

export function normalizeChatTask(raw: any): ChatTaskView | null {
    if (!raw || typeof raw !== "object") return null;
    const id = String(raw.id || raw.ID || "").trim();
    if (!id) return null;
    const paths = raw.artifact_paths || raw.artifactPaths || raw.ArtifactPaths;
    return {
        id,
        sessionKey: String(raw.session_key || raw.sessionKey || raw.SessionKey || "desktop-user").trim() || "desktop-user",
        title: String(raw.title || raw.Title || "").trim(),
        prompt: String(raw.prompt || raw.Prompt || "").trim() || undefined,
        status: String(raw.status || raw.Status || "queued").trim() || "queued",
        progress: String(raw.progress || raw.Progress || "").trim() || undefined,
        question: String(raw.question || raw.Question || "").trim() || undefined,
        resultText: String(raw.result_text || raw.resultText || raw.ResultText || "").trim() || undefined,
        error: String(raw.error || raw.Error || "").trim() || undefined,
        artifactPaths: Array.isArray(paths) ? paths.map((path: unknown) => String(path || "").trim()).filter(Boolean) : undefined,
        recovered: Boolean(raw.recovered || raw.Recovered),
        updatedAtUnix: Number(raw.updated_at_unix || raw.updatedAtUnix || raw.UpdatedAtUnix || 0) || undefined,
    };
}

export function normalizeChatTasks(raw: unknown): ChatTaskView[] {
    const list = Array.isArray(raw) ? raw : [];
    return list.map(normalizeChatTask).filter((task): task is ChatTaskView => !!task);
}

export function parseChatTaskEvent(payload: unknown): ChatTaskView | null {
    let raw = payload;
    if (typeof payload === "string") {
        try {
            raw = JSON.parse(payload);
        } catch {
            return null;
        }
    }
    return normalizeChatTask(raw);
}

const restoredOnStartup = new Set(["queued", "running", "needs_input", "failed", "interrupted"]);

export function chatTasksToRestore(tasks: ChatTaskView[]): ChatTaskView[] {
    return tasks.filter(task => restoredOnStartup.has(task.status));
}

export function upsertChatTaskList(tasks: ChatTaskView[], next: ChatTaskView): ChatTaskView[] {
    const index = tasks.findIndex(task => task.id === next.id);
    if (index < 0) return [...tasks, next];
    const copy = tasks.slice();
    copy[index] = next;
    return copy;
}

export interface ChatTaskMessage {
    id: string;
    role: "user" | "assistant" | "progress" | "error" | "system";
    kind?: string;
    content: string;
    chatTask?: ChatTaskView;
    sessionKey?: string;
    localFilePath?: string;
    localFilePaths?: string[];
    requestId?: string;
    timestamp: number;
}

export function chatTaskCardMessage(task: ChatTaskView, timestamp = Date.now()): ChatTaskMessage {
    return {
        id: `chat-task-${task.id}`,
        role: "assistant",
        kind: "chatTask",
        content: chatTaskCardSummary(task),
        chatTask: task,
        sessionKey: task.sessionKey,
        localFilePaths: task.artifactPaths,
        timestamp,
    };
}

/**
 * Card/tray title for people. Dispatch titles are the first 72 runes of the
 * worker prompt, which often opens with a working-directory banner the
 * dispatcher prepended ("工作目录：C:\…\作业。任务两步：1) …"). The job is
 * the interesting part; the path is chrome. Unknown shapes pass through.
 */
export function chatTaskDisplayTitle(task: Pick<ChatTaskView, "title">): string {
    const raw = String(task.title || "").trim();
    let title = raw
        .replace(/^(?:工作目录|Working directory)\s*[：:][^。\n]*?(?:[。]\s*|[.]\s|\n+)\s*/i, "")
        .replace(/^任务[一二两三四五六七八九十\d]+\s*步\s*[：:]\s*/, "")
        .replace(/^Task\s+steps?\s*[：:]\s*/i, "")
        .trim();
    if (!title) title = raw;
    return title;
}

export type ChatTaskTone = "accent" | "attention" | "good" | "bad" | "muted";

/** Tokens the card and tray need to tint task status. Structural so callers can pass any Theme. */
export type ChatTaskToneTheme = Pick<Theme, "btnColor" | "errorText" | "textMuted" | "isDark">;

export function chatTaskStatusTone(status: string): ChatTaskTone {
    switch (status) {
        case "running":
        case "queued":
            return "accent";
        case "needs_input":
            return "attention";
        case "done":
            return "good";
        case "failed":
            return "bad";
        default:
            return "muted";
    }
}

export function chatTaskToneColor(theme: ChatTaskToneTheme, tone: ChatTaskTone): string {
    switch (tone) {
        case "accent": return theme.btnColor || "#2563eb";
        case "attention": return theme.isDark ? "#fbbf24" : "#b45309";
        case "good": return theme.isDark ? "#4ade80" : "#15803d";
        case "bad": return theme.errorText || "#dc2626";
        default: return theme.textMuted;
    }
}

const HELPER_SLOTS = [/^1[.、]\s*(.*)$/, /^2[.、]\s*(.*)$/, /^3[.、]\s*(.*)$/];
const HELPER_FILE = /^文件\s*[:：]\s*(.*)$/;

function chatTaskSep(lang: string): string {
    const normalized = normalizeLang(lang);
    return normalized === "zh-Hans" || normalized === "zh-Hant" ? "：" : ": ";
}

function chatTaskNone(lang: string): string {
    return tailText(lang, "None", "无", "無");
}

function chatTaskSlot(lang: string, value: string): string {
    const text = value.trim();
    if (!text || text === "无" || text === "無" || text.toLowerCase() === "none") return chatTaskNone(lang);
    return text;
}

/**
 * Relabel the stored four-line helper card for the person reading it.
 * The stored text stays "1. … / 文件：…". A longer reply is left as written.
 */
export function localizeHelperReport(text: string, lang: string): string {
    const raw = String(text || "").trim();
    const lines = raw.split(/\r?\n/);
    if (lines.length !== 4) return raw;
    const slots: string[] = [];
    for (let i = 0; i < 3; i++) {
        const match = lines[i].trim().match(HELPER_SLOTS[i]);
        if (!match) return raw;
        slots.push(match[1] ?? "");
    }
    const file = lines[3].trim().match(HELPER_FILE);
    if (!file) return raw;
    const sep = chatTaskSep(lang);
    const labels = [
        tailText(lang, "What finished", "已完成", "已完成"),
        tailText(lang, "What is left", "还剩", "還剩"),
        tailText(lang, "What is blocked", "卡住", "卡住"),
    ];
    return [
        ...labels.map((label, i) => `${label}${sep}${chatTaskSlot(lang, slots[i])}`),
        `${tailText(lang, "File path", "文件", "檔案")}${sep}${chatTaskSlot(lang, file[1] ?? "")}`,
    ].join("\n");
}

function chatTaskRecordParts(error: string): { line: string; rest: string } {
    const text = error.trim();
    const breakAt = text.search(/\r?\n/);
    if (breakAt < 0) return { line: text, rest: "" };
    return { line: text.slice(0, breakAt).trim(), rest: text.slice(breakAt).replace(/^\r?\n/, "") };
}

function helperReportEmptyRecord(error: string): { fileNone: boolean; detail: string } | null {
    const { line, rest } = chatTaskRecordParts(error);
    if (line !== "helper_report_empty" && line !== "helper report has no conclusion and no path") return null;
    let fileNone = false;
    let detail = "";
    const lines = rest.split(/\r?\n/);
    for (let i = 0; i < lines.length; i++) {
        const item = lines[i].trim();
        if (item === "file: none") fileNone = true;
        if (item === "detail:") {
            detail = lines.slice(i + 1).join("\n").trim();
            break;
        }
    }
    return { fileNone, detail };
}

/**
 * The sentence under a failed background task. Known records are localized.
 * A reason the agent already wrote is kept, so it is not translated twice.
 */
export function formatChatTaskFailure(error: string | undefined, lang: string): string {
    const raw = String(error || "").trim();
    if (!raw) return "";
    const head = chatTaskRecordParts(raw).line;
    if (head === "chat_task_no_result" || head === "chat task returned no result") {
        return tailText(lang, "The task finished without a result.", "任务结束了，但没有返回结果。", "任務結束了，但沒有返回結果。");
    }
    const empty = helperReportEmptyRecord(raw);
    if (!empty) return raw;
    const lines = [
        tailText(lang, "The helper report had no conclusion and no file path.", "助手报告没有结论，也没有文件路径。", "助手報告沒有結論，也沒有檔案路徑。"),
        tailText(lang, "Conclusion lines 1, 2, and 3 are empty.", "结论第 1、2、3 行都是空的。", "結論第 1、2、3 行都是空的。"),
        empty.fileNone
            ? tailText(lang, "The file line did not name a path.", "文件一行没有写出路径。", "檔案一行沒有寫出路徑。")
            : tailText(lang, "No file path was given.", "没有给出文件路径。", "沒有給出檔案路徑。"),
    ];
    if (empty.detail) {
        lines.push(tailText(lang, "What it returned:", "实际返回：", "實際返回："));
        lines.push(empty.detail);
    }
    return lines.join("\n");
}

export function chatTaskCardSummary(task: ChatTaskView): string {
    if (task.status === "needs_input" && task.question) return task.question;
    if (task.status === "failed" && task.error) {
        const record = helperReportEmptyRecord(task.error);
        if (record) return record.detail || task.title || task.error;
        const head = chatTaskRecordParts(task.error).line;
        if (head === "chat_task_no_result" || head === "chat task returned no result") return task.title || task.error;
        return task.error;
    }
    if (task.status === "interrupted") return task.progress || "Was running when the app stopped. It was not resumed.";
    if (task.progress && !chatTaskTerminal(task.status)) return task.progress;
    if (task.resultText) return task.resultText;
    return task.title;
}

export function upsertChatTaskCard<T extends ChatTaskMessage>(messages: T[], task: ChatTaskView): T[] {
    const id = `chat-task-${task.id}`;
    const index = messages.findIndex(message => message.id === id || message.chatTask?.id === task.id);
    const existing = index >= 0 ? messages[index] : undefined;
    const next = {
        ...(existing || chatTaskCardMessage(task)),
        kind: "chatTask" as const,
        content: chatTaskCardSummary(task),
        chatTask: task,
        sessionKey: task.sessionKey || existing?.sessionKey,
        localFilePaths: task.artifactPaths || existing?.localFilePaths,
    } as T;
    if (index < 0) return [...messages, next];
    const copy = messages.slice();
    copy[index] = next;
    return copy;
}

export function appendChatTaskResultMessage<T extends ChatTaskMessage>(messages: T[], payload: ChatTaskResultPayload): T[] {
    const taskID = String(payload.chat_task_id || "").trim()
        || String(payload.request_id || "").replace(/^chat-task-result-/, "");
    const id = taskID ? `chat-task-result-${taskID}` : `chat-task-result-${Date.now()}`;
    if (messages.some(message => message.id === id)) return messages;
    const paths = Array.isArray(payload.local_file_paths) ? payload.local_file_paths.filter(Boolean) : [];
    const single = String(payload.local_file_path || "").trim();
    const files = paths.length > 0 ? paths : (single ? [single] : undefined);
    const text = String(payload.text || "").trim();
    if (!text && !(files && files.length)) return messages;
    // Files stay on localFilePaths and render as the artifact card.
    // Copying them into the body drew the path a second time above 产出物.
    return [...messages, {
        id,
        role: "assistant",
        kind: "chatTask",
        content: text,
        sessionKey: String(payload.session_key || "").trim() || undefined,
        localFilePath: single || undefined,
        localFilePaths: files,
        requestId: String(payload.request_id || "").trim() || undefined,
        timestamp: Date.now(),
    } as T];
}

type AppMethod = (...args: unknown[]) => Promise<unknown>;

function appMethod(name: string): AppMethod | null {
    if (typeof window === "undefined") return null;
    const method = (window as any)?.go?.main?.App?.[name];
    return typeof method === "function" ? method : null;
}

export async function dispatchDesktopChatTask(input: { text: string; sessionKey?: string; projectPath?: string; lang?: string }): Promise<ChatTaskView | null> {
    const method = appMethod("DispatchAIAssistantChatTask");
    if (!method) return null;
    const raw = await method({
        text: input.text,
        session_key: input.sessionKey || "",
        project_path: input.projectPath || "",
        lang: input.lang || "",
    });
    return normalizeChatTask(raw);
}

export async function listDesktopChatTasks(sessionKey = ""): Promise<ChatTaskView[]> {
    const method = appMethod("ListAIAssistantChatTasks");
    if (!method) return [];
    return normalizeChatTasks(await method(sessionKey));
}

export async function resumeDesktopChatTask(taskID: string, text = ""): Promise<ChatTaskView | null> {
    const method = appMethod("ResumeAIAssistantChatTask");
    if (!method) return null;
    return normalizeChatTask(await method(taskID, text));
}

export async function cancelDesktopChatTask(taskID: string): Promise<void> {
    const method = appMethod("CancelAIAssistantChatTask");
    if (!method) return;
    await method(taskID);
}

export async function steerDesktopChatTask(taskID: string, text: string): Promise<void> {
    const method = appMethod("SteerAIAssistantChatTask");
    if (!method) return;
    await method(taskID, text);
}

export async function answerDesktopChatTask(taskID: string, text: string): Promise<void> {
    const method = appMethod("AnswerAIAssistantChatTask");
    if (!method) return;
    await method(taskID, text);
}
