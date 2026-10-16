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
        title: String(raw.title || raw.Title || "Background task").trim() || "Background task",
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

export function chatTaskCardSummary(task: ChatTaskView): string {
    if (task.status === "needs_input" && task.question) return task.question;
    if (task.status === "failed" && task.error) return task.error;
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
    return [...messages, {
        id,
        role: "assistant",
        kind: "chatTask",
        content: String(payload.text || "").trim() || "Background task finished.",
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
