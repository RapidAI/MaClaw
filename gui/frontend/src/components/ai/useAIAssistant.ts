import { useState, useEffect, useCallback, useRef } from "react";
import { SendAIAssistantMessage, SendBtwQuery, ClearAIAssistantHistory, FetchNews, IsAIAssistantReady, GetAIAssistantInitStatus, CancelAIAssistantSession, CancelAIAssistantTask, SelectAIAssistantFiles, StartAIAssistantBackgroundTask, GetTrialReflectEnabled, GetAIAssistantTrace, LoadConfig, ListRemoteSessions, ResolveCriticalConfirm, InjectAIAssistantSupplementary } from "../../../wailsjs/go/main/App";
import { main } from "../../../wailsjs/go/models";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";

export interface CancelAIAssistantResult {
    canceledText: string;
}

export interface StartAIAssistantBackgroundTaskResult {
    accepted?: boolean;
    mode?: string;
    session_id?: string;
    sessionID?: string;
    job_id?: string;
    jobID?: string;
    run_id?: string;
    runID?: string;
    error?: string;
}

export interface AIAssistantBackgroundLaunchResult {
    sessionID: string;
    jobID?: string;
    runID?: string;
}

interface AIAssistantSendResult {
    text?: string;
    Text?: string;
    error?: string;
    Error?: string;
    deferred?: boolean;
    Deferred?: boolean;
    clear_ui?: boolean;
    ClearUI?: boolean;
    confirmed_resume?: boolean;
    ConfirmedResume?: boolean;
    fields?: any;
    Fields?: any;
    actions?: any;
    Actions?: any;
    confirmation?: AIAssistantResponseConfirmation;
    Confirmation?: AIAssistantResponseConfirmation;
    unfinished_slot?: AIAssistantResponseUnfinishedSlot;
    UnfinishedSlot?: AIAssistantResponseUnfinishedSlot;
    local_file_path?: string;
    LocalFilePath?: string;
    local_file_paths?: string[];
    LocalFilePaths?: string[];
    thumbnail_base64?: string;
    ThumbnailBase64?: string;
    request_id?: string;
    RequestID?: string;
    trace_status?: string;
    TraceStatus?: string;
    trace_summary?: string;
    TraceSummary?: string;
    trace_event_count?: number;
    TraceEventCount?: number;
    evidence_count?: number;
    EvidenceCount?: number;
    trial_reflect_summary?: string;
    TrialReflectSummary?: string;
    trial_reflect_status?: string;
    TrialReflectStatus?: string;
    trial_reflect_failures?: number;
    TrialReflectFailures?: number;
    job_id?: string;
    JobID?: string;
    run_id?: string;
    RunID?: string;
}

interface SendMessageOptions {
    resumeSlotID?: string;
    startNewTask?: boolean;
    dismissSlotID?: string;
    uiAction?: boolean;
}

interface AIAssistantRemoteSessionView {
    id?: string;
    launch_source?: string;
    job_id?: string;
    run_id?: string;
    status?: string;
}

interface AIAssistantPendingTask {
    requestId: string;
    sessionID?: string;
    jobID?: string;
    runID?: string;
}

interface AIAssistantStreamEvent {
    request_id?: string;
    text?: string;
}

export interface NewsCardData {
    articleId: string;
    category: NewsCategory;
    title: string;
    body: string;
    icon: string;
}

export type NewsCategory = 'notice' | 'update' | 'tip' | 'alert' | '';
export type AIAssistantInitStatus = 'connecting' | 'loading' | 'warming' | 'ready';

export type ChatActionStyle = 'default' | 'danger';

export interface ChatAction {
    label: string;
    command: string;
    style: ChatActionStyle;
}

export interface ChatConfirmation {
    id: string;
    summary: string;
    taskType?: string;
    targetPaths?: string[];
    plannedActions?: string[];
    riskFlags?: string[];
    revisionHints?: string[];
    status?: string;
}

export interface ChatUnfinishedSlot {
    slotID?: string;
    title?: string;
    summary?: string;
    projectPath?: string;
    status?: string;
    actions?: ChatAction[];
}

interface AIAssistantTraceView {
    job_id?: string;
    run_id?: string;
    status?: string;
    summary?: string;
    event_count?: number;
    evidence_count?: number;
    trial_reflect_summary?: {
        attempt_count?: number;
        attempted_tools?: string[];
        failure_count?: number;
        failure_categories?: string[];
        recovered?: boolean;
        final_outcome?: string;
        strategy_note?: string;
    };
    events?: Array<{ kind?: string; summary?: string }>;
    evidence?: Array<{ source_kind?: string; category?: string; summary?: string }>;
}

interface AIAssistantResponseConfirmation {
    id?: string;
    ID?: string;
    summary?: string;
    Summary?: string;
    task_type?: string;
    TaskType?: string;
    target_paths?: string[];
    TargetPaths?: string[];
    planned_actions?: string[];
    PlannedActions?: string[];
    risk_flags?: string[];
    RiskFlags?: string[];
    revision_hints?: string[];
    RevisionHints?: string[];
    status?: string;
    Status?: string;
}

interface AIAssistantResponseUnfinishedSlot {
    slot_id?: string;
    SlotID?: string;
    title?: string;
    Title?: string;
    summary?: string;
    Summary?: string;
    project_path?: string;
    ProjectPath?: string;
    status?: string;
    Status?: string;
    actions?: ChatAction[];
    Actions?: ChatAction[];
}

interface AIAssistantPreferences {
    showTraceEntry: boolean;
}

export interface ChatMessage {
    id: string;
    role: 'user' | 'assistant' | 'progress' | 'error' | 'system';
    kind?: 'news' | 'trace';
    content: string;
    news?: NewsCardData;
    fields?: Array<{ label: string; value: string }>;
    actions?: ChatAction[];
    confirmation?: ChatConfirmation;
    unfinishedSlot?: ChatUnfinishedSlot;
    localFilePath?: string;
    localFilePaths?: string[];
    thumbnailBase64?: string;
    requestId?: string;
    timestamp: number;
    /** Workflow document link — phase ID for opening doc preview. */
    workflowPhaseID?: string;
    /** Workflow document link label (e.g. "📄 需求文档"). */
    workflowDocLabel?: string;
}

// Auto-incrementing ID to avoid collisions from rapid messages / progress events.
let _nextMsgId = 1;
function nextId(): string {
    return `msg-${Date.now()}-${_nextMsgId++}`;
}

const STREAM_TOKEN_EVENT = "ai-assistant-token";
const NEW_ROUND_EVENT = "ai-assistant-new-round";
const STREAM_DONE_EVENT = "ai-assistant-stream-done";
const INIT_PROGRESS_EVENT = "ai-assistant-init-progress";
const PROGRESS_EVENT = "ai-assistant-progress";

// ---------------------------------------------------------------------------
// localStorage persistence for chat history across app restarts
// ---------------------------------------------------------------------------
export const AI_ASSISTANT_HISTORY_STORAGE_KEY = "ai-assistant-history";
export const AI_ASSISTANT_PROMPT_HISTORY_STORAGE_KEY = "ai-assistant-prompt-history";
const MAX_PERSISTED_MESSAGES = 200;
const MAX_PERSISTED_PROMPTS = 100;
const FILE_PATH_PROMPT_PREFIX = "[用户选择的本地文件路径]";
const IMAGE_FILE_EXTENSIONS = new Set([".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".svg", ".ico", ".tif", ".tiff"]);
const MAX_LIVE_PROGRESS_MESSAGES = 100;
const HIDDEN_PROGRESS_PATTERNS = [
    /^[🚀⏳✅❌]\s*Skill\b/,
    /^[🖥️⏳]\s*正在执行命令处理文件，请稍候(?:\.\.\.)?$/,
    /^[⏳]\s*命令仍在执行中（已\s*\d+s）:/,
    /^[🛠️🧠💾🚀📦⏳]\s*正在生成并执行脚本，准备继续完成交付(?:\.\.\.)?$/,
];

function shouldHideProgressText(progressText: string): boolean {
    const trimmed = progressText.trim();
    if (!trimmed) return true;
    return HIDDEN_PROGRESS_PATTERNS.some(pattern => pattern.test(trimmed));
}

function removeMessageAtIndex(messages: ChatMessage[], index: number): ChatMessage[] {
    if (index < 0 || index >= messages.length) return messages;
    if (index === messages.length - 1) return messages.slice(0, -1);
    return [...messages.slice(0, index), ...messages.slice(index + 1)];
}

function appendProgressText(messages: ChatMessage[], progressText: string): ChatMessage[] {
    const lastMessage = messages[messages.length - 1];
    if (lastMessage?.content === progressText) {
        return messages;
    }
    const next = [...messages, {
        id: nextId(),
        role: 'progress' as const,
        content: progressText,
        timestamp: Date.now(),
    }];
    if (next.length <= MAX_LIVE_PROGRESS_MESSAGES) return next;
    return next.slice(-MAX_LIVE_PROGRESS_MESSAGES);
}

function normalizeLocalFilePaths(localFilePath: unknown, localFilePaths: unknown): string[] | undefined {
    const normalized = new Set<string>();
    if (typeof localFilePath === 'string' && localFilePath.trim()) {
        normalized.add(localFilePath.trim());
    }
    if (Array.isArray(localFilePaths)) {
        for (const entry of localFilePaths) {
            if (typeof entry !== 'string') continue;
            const trimmed = entry.trim();
            if (!trimmed) continue;
            normalized.add(trimmed);
        }
    }
    return normalized.size > 0 ? Array.from(normalized) : undefined;
}

export function isImageFilePath(filePath: string): boolean {
    const normalized = filePath.trim().toLowerCase();
    if (!normalized) return false;
    const queryIndex = normalized.search(/[?#]/);
    const pathname = queryIndex >= 0 ? normalized.slice(0, queryIndex) : normalized;
    return Array.from(IMAGE_FILE_EXTENSIONS).some(ext => pathname.endsWith(ext));
}

export function buildOutgoingMessage(text: string, selectedFilePath: string): string {
    const trimmedText = text.trim();
    const trimmedPath = selectedFilePath.trim();
    if (!trimmedPath) return trimmedText;
    const pathInstructions = isImageFilePath(trimmedPath)
        ? "这是用户已经提供的本地图片文件。不要调用 screenshot 或重新截图；请直接使用这些路径，并优先用 read_file 或 open 查看图片内容后回答。"
        : "请直接使用这些路径；如需查看内容可调用 read_file、open 等工具。";
    const fileBlock = [
        FILE_PATH_PROMPT_PREFIX,
        trimmedPath,
        pathInstructions,
    ].join("\n");
    return trimmedText ? `${trimmedText}\n\n${fileBlock}` : fileBlock;
}

export function buildOutgoingMessageMulti(text: string, filePaths: string[]): string {
    const trimmedText = text.trim();
    const validPaths = filePaths.map(p => p.trim()).filter(Boolean);
    if (validPaths.length === 0) return trimmedText;

    const hasImages = validPaths.some(isImageFilePath);
    const pathInstructions = hasImages
        ? "这是用户已经提供的本地文件。图片文件不要调用 screenshot 或重新截图；请直接使用这些路径。"
        : "请直接使用这些路径；如需查看内容可调用 read_file、open 等工具。";

    const fileBlock = [
        FILE_PATH_PROMPT_PREFIX,
        ...validPaths,
        pathInstructions,
    ].join("\n");

    return trimmedText ? `${trimmedText}\n\n${fileBlock}` : fileBlock;
}

function normalizeSelectedFilePath(filePath: string): string {
    return filePath.trim();
}

function loadPersistedMessages(): ChatMessage[] {
    try {
        const raw = localStorage.getItem(AI_ASSISTANT_HISTORY_STORAGE_KEY);
        if (!raw) return [];
        const parsed = JSON.parse(raw);
        if (!Array.isArray(parsed)) return [];
        // Only restore user / assistant / error messages
        // Skip transient progress and system (news) messages
        return parsed.filter(
            (m: any) => m && m.id && m.role && m.role !== 'progress' && m.role !== 'system'
        ) as ChatMessage[];
    } catch {
        return [];
    }
}

function loadPersistedPrompts(): string[] {
    try {
        const raw = localStorage.getItem(AI_ASSISTANT_PROMPT_HISTORY_STORAGE_KEY);
        if (!raw) return [];
        const parsed = JSON.parse(raw);
        if (!Array.isArray(parsed)) return [];
        return parsed
            .filter((value: unknown): value is string => typeof value === 'string')
            .map(value => value.trim())
            .filter(Boolean)
            .slice(-MAX_PERSISTED_PROMPTS);
    } catch {
        return [];
    }
}

function serializePersistedMessages(msgs: ChatMessage[]): string | null {
    // Only persist meaningful messages; skip progress, system, and empty content.
    // Strip thumbnailBase64 to avoid blowing up localStorage (5MB limit).
    const toSave = msgs
        .filter(m => m.role !== 'progress' && m.role !== 'system' && m.content !== '')
        .slice(-MAX_PERSISTED_MESSAGES)
        .map(m => {
            if (!m.thumbnailBase64) return m;
            const { thumbnailBase64: _, ...rest } = m;
            return rest;
        });
    return toSave.length === 0 ? null : JSON.stringify(toSave);
}

function persistMessages(msgs: ChatMessage[]) {
    try {
        const serialized = serializePersistedMessages(msgs);
        if (serialized === null) {
            localStorage.removeItem(AI_ASSISTANT_HISTORY_STORAGE_KEY);
            return;
        }
        localStorage.setItem(AI_ASSISTANT_HISTORY_STORAGE_KEY, serialized);
    } catch {
        // localStorage full or unavailable — silently ignore
    }
}

function appendSubmittedPrompt(prompts: string[], prompt: string): string[] {
    const trimmed = prompt.trim();
    if (!trimmed) return prompts;
    if (prompts[prompts.length - 1] === trimmed) return prompts;
    return [...prompts, trimmed].slice(-MAX_PERSISTED_PROMPTS);
}

function persistPrompts(prompts: string[]) {
    try {
        const normalized = prompts
            .map(prompt => prompt.trim())
            .filter(Boolean)
            .slice(-MAX_PERSISTED_PROMPTS);
        if (normalized.length === 0) {
            localStorage.removeItem(AI_ASSISTANT_PROMPT_HISTORY_STORAGE_KEY);
            return;
        }
        localStorage.setItem(AI_ASSISTANT_PROMPT_HISTORY_STORAGE_KEY, JSON.stringify(normalized));
    } catch {
        // localStorage full or unavailable — silently ignore
    }
}

interface ActiveRound {
    generation: number;
    phase: 'idle' | 'requesting' | 'streaming';
    assistantMessageId: string | null;
    requestId: string;
}

function createIdleRound(generation: number): ActiveRound {
    return {
        generation,
        phase: 'idle',
        assistantMessageId: null,
        requestId: '',
    };
}

function isRoundIdle(round: ActiveRound): boolean {
    return round.phase === 'idle' && round.assistantMessageId === null;
}

function sameActiveRound(left: ActiveRound, right: ActiveRound): boolean {
    return left.generation === right.generation
        && left.phase === right.phase
        && left.assistantMessageId === right.assistantMessageId
        && left.requestId === right.requestId;
}

const IDLE_ROUND: ActiveRound = createIdleRound(0);

function isAssistantPlaceholder(msg: ChatMessage): boolean {
    return msg.role === 'assistant' && msg.content === '' && !msg.fields?.length && !msg.thumbnailBase64 && !msg.localFilePaths?.length && !msg.localFilePath;
}

function appendAssistantPlaceholder(messages: ChatMessage[], assistantMessageId: string, requestId = ''): ChatMessage[] {
    const index = messages.findIndex(msg => msg.id === assistantMessageId);
    if (index >= 0) return messages;
    return [...messages, {
        id: assistantMessageId,
        role: 'assistant',
        content: '',
        requestId: requestId || undefined,
        timestamp: Date.now(),
    }];
}

function updateTailMessage(messages: ChatMessage[], messageId: string | null, updater: (message: ChatMessage) => ChatMessage | null): ChatMessage[] | null {
    if (!messageId || messages.length === 0) return null;
    const lastIndex = messages.length - 1;
    if (messages[lastIndex].id !== messageId) return null;
    const updated = updater(messages[lastIndex]);
    if (updated === messages[lastIndex]) return messages;
    if (updated === null) {
        return messages.slice(0, -1);
    }
    const next = [...messages];
    next[lastIndex] = updated;
    return next;
}

function updateMessageById(messages: ChatMessage[], messageId: string | null, updater: (message: ChatMessage) => ChatMessage | null): ChatMessage[] {
    if (!messageId) return messages;
    const index = findLastIndex(messages, msg => msg.id === messageId);
    if (index < 0) return messages;
    const updated = updater(messages[index]);
    if (updated === messages[index]) return messages;
    if (updated === null) {
        return removeMessageAtIndex(messages, index);
    }
    const next = [...messages];
    next[index] = updated;
    return next;
}

function updateMessageByRequestId(messages: ChatMessage[], requestId: string | null, updater: (message: ChatMessage) => ChatMessage | null): ChatMessage[] {
    const normalizedRequestId = typeof requestId === 'string' ? requestId.trim() : '';
    if (!normalizedRequestId) return messages;
    const index = findLastIndex(messages, msg => msg.role === 'assistant' && msg.requestId === normalizedRequestId);
    if (index < 0) return messages;
    const updated = updater(messages[index]);
    if (updated === messages[index]) return messages;
    if (updated === null) {
        return removeMessageAtIndex(messages, index);
    }
    const next = [...messages];
    next[index] = updated;
    return next;
}

function hasRoundMessage(messages: ChatMessage[], assistantMessageId: string | null, requestId: string | null): boolean {
    if (assistantMessageId && findLastIndex(messages, msg => msg.id === assistantMessageId) >= 0) {
        return true;
    }
    const normalizedRequestId = typeof requestId === 'string' ? requestId.trim() : '';
    return !!normalizedRequestId && findLastIndex(messages, msg => msg.role === 'assistant' && msg.requestId === normalizedRequestId) >= 0;
}

function updateRoundMessage(messages: ChatMessage[], assistantMessageId: string | null, requestId: string | null, updater: (message: ChatMessage) => ChatMessage | null): ChatMessage[] {
    const updatedTail = updateTailMessage(messages, assistantMessageId, updater);
    if (updatedTail) return updatedTail;
    const updatedById = updateMessageById(messages, assistantMessageId, updater);
    if (updatedById !== messages) return updatedById;
    return updateMessageByRequestId(messages, requestId, updater);
}

function markLatestConfirmationAsRunning(messages: ChatMessage[]): ChatMessage[] {
    const index = findLastIndex(messages, message => message.role === 'assistant' && !!message.confirmation);
    if (index < 0) return messages;
    const target = messages[index];
    const confirmation = target.confirmation;
    if (!confirmation) return messages;
    const nextStatus = 'running';
    if (confirmation.status === nextStatus) return messages;
    const next = [...messages];
    next[index] = {
        ...target,
        confirmation: {
            ...confirmation,
            status: nextStatus,
        },
    };
    return next;
}

function appendTokenToMessage(message: ChatMessage, delta: string): ChatMessage {
    const nextContent = message.content ? message.content + delta : delta;
    if (nextContent === message.content) return message;
    return {
        ...message,
        content: nextContent,
    };
}

function appendTokenToRound(messages: ChatMessage[], assistantMessageId: string | null, delta: string): ChatMessage[] {
    if (!assistantMessageId || !delta || messages.length === 0) return messages;
    const lastIndex = messages.length - 1;
    const tail = messages[lastIndex];
    if (tail.id === assistantMessageId) {
        const updatedTail = appendTokenToMessage(tail, delta);
        if (updatedTail === tail) return messages;
        const next = [...messages];
        next[lastIndex] = updatedTail;
        return next;
    }
    const index = findLastIndex(messages, message => message.id === assistantMessageId);
    if (index < 0) return messages;
    const updatedMessage = appendTokenToMessage(messages[index], delta);
    if (updatedMessage === messages[index]) return messages;
    const next = [...messages];
    next[index] = updatedMessage;
    return next;
}

function checkInitReadiness(): Promise<{ ready: boolean; status: AIAssistantInitStatus }> {
    return Promise.allSettled([IsAIAssistantReady(), GetAIAssistantInitStatus()]).then(([readyResult, statusResult]) => ({
        ready: readyResult.status === 'fulfilled' && !!readyResult.value,
        status: statusResult.status === 'fulfilled' ? normalizeInitStatus(statusResult.value) : 'connecting',
    }));
}

function replaceNewsMessages(messages: ChatMessage[], newsMessages: ChatMessage[]): ChatMessage[] {
    const existingNews = messages.filter(isPinnedNewsMessage);
    if (existingNews.length === newsMessages.length && existingNews.every((msg, idx) => samePinnedNews(msg, newsMessages[idx]))) {
        return messages;
    }
    const filtered = messages.filter(message => !isPinnedNewsMessage(message));
    return [...newsMessages, ...filtered];
}

function serializeNewsMessages(newsMessages: ChatMessage[]): string {
    return JSON.stringify(newsMessages.map(message => ({
        id: message.id,
        content: message.content,
        news: message.news,
    })));
}

function normalizeTraceFields(response: any, showTraceEntry: boolean): Array<{ label: string; value: string }> {
    if (!showTraceEntry) return [];
    const fields: Array<{ label: string; value: string }> = [];
    const traceSummary = typeof response?.trace_summary === 'string' ? response.trace_summary.trim() : '';
    const trialReflectSummary = typeof response?.trial_reflect_summary === 'string' ? response.trial_reflect_summary.trim() : '';
    const trialReflectStatus = typeof response?.trial_reflect_status === 'string' ? response.trial_reflect_status.trim() : '';
    const trialReflectFailures = typeof response?.trial_reflect_failures === 'number' ? response.trial_reflect_failures : 0;
    const traceEventCount = typeof response?.trace_event_count === 'number' ? response.trace_event_count : 0;
    const evidenceCount = typeof response?.evidence_count === 'number' ? response.evidence_count : 0;
    const runID = typeof response?.run_id === 'string' ? response.run_id.trim() : '';
    const jobID = typeof response?.job_id === 'string' ? response.job_id.trim() : '';
    if (traceSummary) {
        fields.push({ label: 'Trace', value: traceSummary });
    }
    if (trialReflectStatus) {
        fields.push({ label: 'Recovery', value: formatRecoveryStatus(trialReflectStatus) });
    }
    if (trialReflectFailures > 0) {
        fields.push({ label: 'Failures', value: String(trialReflectFailures) });
    }
    if (trialReflectSummary) {
        fields.push({ label: 'Trial reflect', value: trialReflectSummary });
    }
    if (traceEventCount > 0) {
        fields.push({ label: 'Trace events', value: String(traceEventCount) });
    }
    if (evidenceCount > 0) {
        fields.push({ label: 'Evidence', value: String(evidenceCount) });
    }
    if (runID) {
        fields.push({ label: 'Run ID', value: runID });
    }
    if (jobID) {
        fields.push({ label: 'Job ID', value: jobID });
    }
    return fields;
}

function isTokenFieldLabel(label: string): boolean {
    const normalized = label.trim().toLowerCase();
    return normalized === 'input tokens'
        || normalized === 'output tokens'
        || normalized === 'total tokens';
}

function normalizeResponseFields(fields: any, showDetailEntry = false): Array<{ label: string; value: string }> | undefined {
    if (!Array.isArray(fields) || fields.length === 0) return undefined;
    const normalized = fields
        .filter(field => field && typeof field === 'object')
        .map(field => ({
            label: typeof field.label === 'string' ? field.label : (typeof field.Label === 'string' ? field.Label : ''),
            value: typeof field.value === 'string' ? field.value : (typeof field.Value === 'string' ? field.Value : ''),
        }))
        .filter(field => field.label && field.value)
        .filter(field => showDetailEntry || !isTokenFieldLabel(field.label));
    return normalized.length > 0 ? normalized : undefined;
}

function normalizeStringArray(values: unknown): string[] | undefined {
    if (!Array.isArray(values)) return undefined;
    const normalized = values
        .filter((value): value is string => typeof value === 'string')
        .map(value => value.trim())
        .filter(Boolean);
    return normalized.length > 0 ? normalized : undefined;
}

function normalizeConfirmation(raw: AIAssistantResponseConfirmation | null | undefined): ChatConfirmation | undefined {
    if (!raw || typeof raw !== 'object') return undefined;
    const id = typeof raw.id === 'string' ? raw.id.trim() : (typeof raw.ID === 'string' ? raw.ID.trim() : '');
    const summary = typeof raw.summary === 'string' ? raw.summary.trim() : (typeof raw.Summary === 'string' ? raw.Summary.trim() : '');
    if (!summary) return undefined;
    const taskType = typeof raw.task_type === 'string' ? raw.task_type.trim() : (typeof raw.TaskType === 'string' ? raw.TaskType.trim() : '');
    const status = typeof raw.status === 'string' ? raw.status.trim() : (typeof raw.Status === 'string' ? raw.Status.trim() : '');
    return {
        id,
        summary,
        taskType: taskType || undefined,
        targetPaths: normalizeStringArray(raw.target_paths ?? raw.TargetPaths),
        plannedActions: normalizeStringArray(raw.planned_actions ?? raw.PlannedActions),
        riskFlags: normalizeStringArray(raw.risk_flags ?? raw.RiskFlags),
        revisionHints: normalizeStringArray(raw.revision_hints ?? raw.RevisionHints),
        status: status || undefined,
    };
}

function normalizeUnfinishedSlot(raw: AIAssistantResponseUnfinishedSlot | null | undefined): ChatUnfinishedSlot | undefined {
    if (!raw || typeof raw !== 'object') return undefined;
    const slotID = typeof raw.slot_id === 'string' ? raw.slot_id.trim() : (typeof raw.SlotID === 'string' ? raw.SlotID.trim() : '');
    const title = typeof raw.title === 'string' ? raw.title.trim() : (typeof raw.Title === 'string' ? raw.Title.trim() : '');
    const summary = typeof raw.summary === 'string' ? raw.summary.trim() : (typeof raw.Summary === 'string' ? raw.Summary.trim() : '');
    const projectPath = typeof raw.project_path === 'string' ? raw.project_path.trim() : (typeof raw.ProjectPath === 'string' ? raw.ProjectPath.trim() : '');
    const status = typeof raw.status === 'string' ? raw.status.trim() : (typeof raw.Status === 'string' ? raw.Status.trim() : '');
    const actions = normalizeActions(raw.actions ?? raw.Actions);
    if (!slotID && !title && !summary) return undefined;
    return {
        slotID: slotID || undefined,
        title: title || undefined,
        summary: summary || undefined,
        projectPath: projectPath || undefined,
        status: status || undefined,
        actions,
    };
}

function normalizeSendResponse(response: AIAssistantSendResult | null | undefined, showDetailEntry = false): AIAssistantSendResult {
    const raw = response || {};
    return {
        ...raw,
        text: typeof raw.text === 'string' ? raw.text : (typeof raw.Text === 'string' ? raw.Text : ''),
        error: typeof raw.error === 'string' ? raw.error : (typeof raw.Error === 'string' ? raw.Error : ''),
        fields: normalizeResponseFields(raw.fields ?? raw.Fields, showDetailEntry),
        actions: raw.actions ?? raw.Actions,
        confirmation: normalizeConfirmation(raw.confirmation ?? raw.Confirmation),
        unfinished_slot: normalizeUnfinishedSlot((raw as any).unfinished_slot ?? (raw as any).UnfinishedSlot),
        local_file_path: typeof raw.local_file_path === 'string' ? raw.local_file_path : (typeof raw.LocalFilePath === 'string' ? raw.LocalFilePath : ''),
        local_file_paths: Array.isArray(raw.local_file_paths) ? raw.local_file_paths : (Array.isArray(raw.LocalFilePaths) ? raw.LocalFilePaths : undefined),
        thumbnail_base64: typeof raw.thumbnail_base64 === 'string' ? raw.thumbnail_base64 : (typeof raw.ThumbnailBase64 === 'string' ? raw.ThumbnailBase64 : ''),
        request_id: typeof raw.request_id === 'string' ? raw.request_id : (typeof raw.RequestID === 'string' ? raw.RequestID : ''),
        trace_status: typeof raw.trace_status === 'string' ? raw.trace_status : (typeof raw.TraceStatus === 'string' ? raw.TraceStatus : ''),
        trace_summary: typeof raw.trace_summary === 'string' ? raw.trace_summary : (typeof raw.TraceSummary === 'string' ? raw.TraceSummary : ''),
        trace_event_count: typeof raw.trace_event_count === 'number' ? raw.trace_event_count : (typeof raw.TraceEventCount === 'number' ? raw.TraceEventCount : undefined),
        evidence_count: typeof raw.evidence_count === 'number' ? raw.evidence_count : (typeof raw.EvidenceCount === 'number' ? raw.EvidenceCount : undefined),
        deferred: typeof raw.deferred === 'boolean' ? raw.deferred : (typeof raw.Deferred === 'boolean' ? raw.Deferred : false),
        clear_ui: typeof raw.clear_ui === 'boolean' ? raw.clear_ui : (typeof raw.ClearUI === 'boolean' ? raw.ClearUI : false),
        confirmed_resume: typeof raw.confirmed_resume === 'boolean' ? raw.confirmed_resume : (typeof raw.ConfirmedResume === 'boolean' ? raw.ConfirmedResume : false),
        trial_reflect_summary: typeof raw.trial_reflect_summary === 'string' ? raw.trial_reflect_summary : (typeof raw.TrialReflectSummary === 'string' ? raw.TrialReflectSummary : ''),
        trial_reflect_status: typeof raw.trial_reflect_status === 'string' ? raw.trial_reflect_status : (typeof raw.TrialReflectStatus === 'string' ? raw.TrialReflectStatus : ''),
        trial_reflect_failures: typeof raw.trial_reflect_failures === 'number' ? raw.trial_reflect_failures : (typeof raw.TrialReflectFailures === 'number' ? raw.TrialReflectFailures : undefined),
        job_id: typeof raw.job_id === 'string' ? raw.job_id : (typeof raw.JobID === 'string' ? raw.JobID : ''),
        run_id: typeof raw.run_id === 'string' ? raw.run_id : (typeof raw.RunID === 'string' ? raw.RunID : ''),
    };
}

function formatRecoveryStatus(status: string): string {
    const normalized = status.trim().toLowerCase();
    switch (normalized) {
        case 'recovered_success':
            return 'Recovered after retry';
        case 'partial_success':
            return 'Partial success';
        case 'success':
            return 'Success';
        case 'failed':
            return 'Failed';
        default:
            return status.trim();
    }
}

function mergeResponseFields(responseFields: any, traceFields: Array<{ label: string; value: string }>): Array<{ label: string; value: string }> | undefined {
    const baseFields = Array.isArray(responseFields) ? responseFields : [];
    const merged = [...baseFields, ...traceFields];
    return merged.length > 0 ? merged : undefined;
}

function buildTraceDetailCommand(runID: string): string {
    return `__view_trace__ ${runID}`;
}

function buildTraceDetailFields(view: AIAssistantTraceView, runID: string): Array<{ label: string; value: string }> {
    const fields: Array<{ label: string; value: string }> = [
        { label: 'Run ID', value: runID },
    ];
    const jobID = typeof view.job_id === 'string' ? view.job_id.trim() : '';
    const status = typeof view.status === 'string' ? view.status.trim() : '';
    const trialReflect = view.trial_reflect_summary;
    if (trialReflect?.final_outcome) {
        fields.push({ label: 'Recovery', value: formatRecoveryStatus(trialReflect.final_outcome) });
    }
    if (typeof trialReflect?.failure_count === 'number' && trialReflect.failure_count > 0) {
        fields.push({ label: 'Failures', value: String(trialReflect.failure_count) });
    }
    if (jobID) {
        fields.push({ label: 'Job ID', value: jobID });
    }
    if (typeof view.event_count === 'number' && view.event_count > 0) {
        fields.push({ label: 'Trace events', value: String(view.event_count) });
    }
    if (typeof view.evidence_count === 'number' && view.evidence_count > 0) {
        fields.push({ label: 'Evidence', value: String(view.evidence_count) });
    }
    if (status) {
        fields.push({ label: 'Status', value: status });
    }
    return fields;
}

function buildTraceDetailMessage(view: AIAssistantTraceView, runID: string): string {
    const lines: string[] = [`Trace details for ${runID}`];
    if (typeof view.summary === 'string' && view.summary.trim()) {
        lines.push(`Summary: ${view.summary.trim()}`);
    }
    const trialReflect = view.trial_reflect_summary;
    if (typeof trialReflect?.final_outcome === 'string' && trialReflect.final_outcome.trim()) {
        lines.push(`Recovery status: ${formatRecoveryStatus(trialReflect.final_outcome)}`);
    }
    if (typeof trialReflect?.strategy_note === 'string' && trialReflect.strategy_note.trim()) {
        lines.push(`Trial-reflect: ${trialReflect.strategy_note.trim()}`);
    }
    if (typeof trialReflect?.final_outcome === 'string' && trialReflect.final_outcome.trim()) {
        lines.push(`Recovery: ${trialReflect.final_outcome.trim()}`);
    }
    if (Array.isArray(trialReflect?.failure_categories) && trialReflect.failure_categories.length > 0) {
        lines.push(`Failure categories: ${trialReflect.failure_categories.join(', ')}`);
    }
    if (typeof view.event_count === 'number' && view.event_count > 0) {
        lines.push(`Events: ${view.event_count}`);
    }
    if (typeof view.evidence_count === 'number' && view.evidence_count > 0) {
        lines.push(`Evidence: ${view.evidence_count}`);
    }
    const eventKinds = Array.isArray(view.events)
        ? view.events.map(event => String(event?.kind || '').trim()).filter(Boolean).slice(0, 4)
        : [];
    if (eventKinds.length > 0) {
        lines.push(`Event kinds: ${eventKinds.join(', ')}`);
    }
    const evidenceKinds = Array.isArray(view.evidence)
        ? view.evidence
            .map(item => {
                const source = String(item?.source_kind || '').trim();
                const category = String(item?.category || '').trim();
                return [source, category].filter(Boolean).join('/');
            })
            .filter(Boolean)
            .slice(0, 4)
        : [];
    if (evidenceKinds.length > 0) {
        lines.push(`Evidence kinds: ${evidenceKinds.join(', ')}`);
    }
    return lines.join('\n');
}

function buildTraceDetailAction(response: any, showTraceEntry: boolean): ChatAction[] | undefined {
    if (!showTraceEntry) return undefined;
    const runID = typeof response?.run_id === 'string' ? response.run_id.trim() : '';
    if (!runID) return undefined;
    return [{ label: 'View trace', command: buildTraceDetailCommand(runID), style: 'default' }];
}

const shortChitChatEdgePunctuationPattern = /^[\s"'“”‘’`()（）\[\]【】<>《》,，.。!！?？~～…:：;；、\-—_]+|[\s"'“”‘’`()（）\[\]【】<>《》,，.。!！?？~～…:：;；、\-—_]+$/g;
const shortChitChatChineseIdlePattern = /^(没事|没事了|没有)(啊|呀|啦|呢|吧|哦|喔|哈|哇|嘛|的)?$/;
const shortChitChatChineseThanksPattern = /^(谢谢)(啊|呀|啦|呢|吧|哦|喔|哈)?$/;
const shortChitChatChineseGreetingPattern = /^(你好|你好呀|你好啊|嗨|哈喽)(啊|呀|啦|呢|吧|哦|喔|哈)?$/;

function normalizeShortChitChatToken(text: string): string {
    let cleaned = text.trim().toLowerCase();
    if (!cleaned) return '';
    while (true) {
        const next = cleaned.replace(shortChitChatEdgePunctuationPattern, '').trim();
        if (next === cleaned) break;
        cleaned = next;
    }
    cleaned = cleaned.split(/\s+/).filter(Boolean).join(' ');
    if (!cleaned) return '';
    if (shortChitChatChineseIdlePattern.test(cleaned)) return '没事';
    if (shortChitChatChineseThanksPattern.test(cleaned)) return '谢谢';
    if (shortChitChatChineseGreetingPattern.test(cleaned)) return '你好';
    return new Set([
        '没事',
        'nothing',
        'none',
        'hi',
        'hello',
        'ok',
        'okay',
        'thanks',
        'thank you',
        '谢谢',
    ]).has(cleaned) ? cleaned : '';
}

function selectVisibleEmptyResultSummary(response: any): string {
    const candidates = [response?.trace_summary, response?.trial_reflect_summary]
        .filter((value): value is string => typeof value === 'string')
        .map(value => value.trim())
        .filter(Boolean);
    return candidates.find(isVisibleEmptyResultSummary) || '';
}

function isVisibleEmptyResultSummary(summary: string): boolean {
    const trimmed = summary.trim();
    if (!trimmed) return false;

    const normalized = trimmed.toLowerCase();
    const normalizedEcho = normalizeShortChitChatToken(normalized.replace(/^(summary|result|trace summary|结果|摘要)\s*[:：-]?\s*/i, '').trim());
    if (normalizedEcho) {
        return false;
    }

    const promptLikeMarkers = [
        '当前工作目录',
        'primary working directory',
        'current working directory',
        'project directory',
        'default directory',
        'continue the conversation',
        'resume directly',
        'user:',
        'assistant:',
        'task:',
        '任务：',
        '请帮我',
        '帮我',
        '请实现',
        '请修复',
        '请重建',
        'you are',
    ];
    if (promptLikeMarkers.some(marker => normalized.includes(marker))) {
        return false;
    }

    const executionSignals = [
        'failed',
        'failure',
        'error',
        'stopped',
        'timeout',
        'cancel',
        'killed',
        'retry',
        'recovered',
        'generated',
        'created',
        'saved',
        'wrote',
        'written',
        'exported',
        'uploaded',
        'downloaded',
        'prepared',
        'delivered',
        'found',
        'produced',
        '执行',
        '失败',
        '错误',
        '超时',
        '取消',
        '停止',
        '重试',
        '恢复',
        '生成',
        '创建',
        '保存',
        '写入',
        '导出',
        '上传',
        '下载',
        '准备',
        '找到',
        '文件',
    ];
    return executionSignals.some(signal => normalized.includes(signal));
}

function buildEmptyTerminalFallback(response: any): string {
    const traceStatus = typeof response?.trace_status === 'string' ? response.trace_status.trim().toLowerCase() : '';
    const fallback = selectVisibleEmptyResultSummary(response);
    if (traceStatus === 'failed' || traceStatus === 'timeout' || traceStatus === 'cancelled' || traceStatus === 'stopped') {
        return fallback ? `任务未完成可交付结果。${fallback}` : '任务未完成可交付结果。可查看 Trace 了解失败位置。';
    }
    return fallback ? `任务已结束，但没有生成可展示的结果。${fallback}` : '任务已结束，但没有生成可展示的结果。可查看 Trace 了解详情。';
}

function isFailedTerminalTraceStatus(status: unknown): boolean {
    const normalized = typeof status === 'string' ? status.trim().toLowerCase() : '';
    return normalized === 'failed' || normalized === 'timeout' || normalized === 'cancelled' || normalized === 'stopped';
}

function hasVisibleTerminalPayload(response: any): boolean {
    const text = typeof response?.text === 'string' ? response.text.trim() : '';
    const localFilePath = typeof response?.local_file_path === 'string' ? response.local_file_path.trim() : '';
    const localFilePaths = normalizeLocalFilePaths(localFilePath, response?.local_file_paths);
    const thumbnailBase64 = typeof response?.thumbnail_base64 === 'string' ? response.thumbnail_base64.trim() : '';
    return !!text || !!thumbnailBase64 || !!localFilePaths?.length;
}

/** Sources whose response.text is semantically unrelated to streamed content. */
const SPECIAL_RESPONSE_SOURCES: ReadonlySet<string> = new Set([
    'ask_user', 'cancel', 'file_delivery', 'screenshot',
]);

// rolePrefixPattern matches hallucinated role prefixes (e.g. "Browser: ..." or
// "Tool: ...") at the start of a line, with optional Markdown block-level markers.
// This is the frontend equivalent of the Go-side rolePrefixRe / rolePrefixLineRe.
const rolePrefixPattern = /^[\s>*\-]*(?:\d+\.\s*)?(Browser|Tool)\s*(?::[ \t]?|：)/m;

/**
 * Strip hallucinated role prefixes from LLM output text.
 * Frontend safety net — catches anything the backend streaming filter missed.
 *
 * Case 1: Prefix at the start of text → strip prefix, keep content after it.
 * Case 2: Prefix in the middle → remove the Browser: line only, keep the rest.
 *         Unlike the backend stripRolePrefixHallucination (which truncates at
 *         the prefix — correct for single-LLM-call output where content after
 *         is a duplicate), this frontend version operates on streamedContent
 *         which accumulates across multiple agent loop iterations. Content
 *         after Browser: is from subsequent iterations, not a duplicate.
 *
 * Code blocks (``` fenced) are excluded to avoid false positives.
 */
function stripRolePrefixFrontend(text: string): string {
    if (!text) return text;
    // Fast path: no known prefix keyword present.
    if (!text.includes('Browser') && !text.includes('Tool')) return text;
    if (!text.includes(':') && !text.includes('\uff1a')) return text;

    // Split into code-block-aware segments.
    const parts: Array<{ text: string; isCode: boolean }> = [];
    let rest = text;
    while (rest.length > 0) {
        const idx = rest.indexOf('```');
        if (idx < 0) {
            parts.push({ text: rest, isCode: false });
            break;
        }
        if (idx > 0) {
            parts.push({ text: rest.slice(0, idx), isCode: false });
        }
        const closeIdx = rest.indexOf('```', idx + 3);
        if (closeIdx < 0) {
            parts.push({ text: rest.slice(idx), isCode: true });
            break;
        }
        const end = closeIdx + 3;
        parts.push({ text: rest.slice(idx, end), isCode: true });
        rest = rest.slice(end);
    }

    // Scan non-code segments for role prefix.
    let absOffset = 0;
    for (const part of parts) {
        if (!part.isCode) {
            const match = rolePrefixPattern.exec(part.text);
            if (match && match.index !== undefined) {
                const matchAbsStart = absOffset + match.index;
                const prefixEnd = matchAbsStart + match[0].length;
                const before = text.slice(0, matchAbsStart).trimEnd();
                if (!before) {
                    // Case 1: prefix at start — strip it, keep everything after.
                    return text.slice(prefixEnd).trimStart();
                }
                // Case 2: prefix in middle — remove the Browser: line only.
                // Find the end of the line containing the prefix.
                let lineEnd = text.indexOf('\n', prefixEnd);
                if (lineEnd < 0) {
                    // Browser: line is the last line — just keep content before.
                    return before;
                }
                // Splice out the Browser: line, keep before + after.
                const after = text.slice(lineEnd + 1);
                return before + '\n' + after;
            }
        }
        absOffset += part.text.length;
    }
    return text;
}

export function resolveFinalRoundContent(message: ChatMessage, response: any): string {
    const finalText = typeof response?.text === 'string' ? response.text : '';
    const streamedContent = message.content || '';
    const responseSource: string = typeof response?.response_source === 'string' ? response.response_source : '';

    // --- Layer 1: Source check ---
    // Special handler paths produce text semantically unrelated to streamed
    // content (e.g. ask_user structured questions, cancel messages, file
    // delivery notices, screenshot captions). Always use finalText for these.
    if (SPECIAL_RESPONSE_SOURCES.has(responseSource)) {
        return finalText;
    }

    // --- Layer 2: Length comparison ---
    // When streamed content is significantly longer than finalText (>= 2×),
    // the response text is just the last iteration's fragment from a
    // multi-round agent loop. Preserve the complete accumulated output.
    const finalTextLen = finalText.trim().length;
    if (streamedContent && finalText && finalTextLen > 0
        && streamedContent.length >= finalTextLen * 2
        && (!responseSource || responseSource === 'agent_loop')) {
        // Apply role prefix stripping — streamedContent bypasses backend
        // post-processing (stripRolePrefixHallucination) because it comes
        // from the streaming token path, not from resp.Text.
        return stripRolePrefixFrontend(streamedContent);
    }

    // --- Layer 3: endsWith fallback ---
    // Original improvement #19: if streamed content ends with the response
    // text, the response text is the final iteration's tail — keep the full
    // streamed content.
    if (streamedContent && finalText && streamedContent.length > finalText.length) {
        if (streamedContent.endsWith(finalText)) {
            return stripRolePrefixFrontend(streamedContent);
        }
    }

    // --- Subsequent fallbacks (unchanged) ---
    if (finalText) {
        return finalText;
    }
    if (hasVisibleTerminalPayload(response)) {
        return stripRolePrefixFrontend(streamedContent);
    }
    if (isFailedTerminalTraceStatus(response?.trace_status)) {
        return buildEmptyTerminalFallback(response);
    }
    if (streamedContent) {
        return stripRolePrefixFrontend(streamedContent);
    }
    return buildEmptyTerminalFallback(response);
}

function finalizeRoundMessage(messages: ChatMessage[], assistantMessageId: string | null, requestId: string | null, response: any, preferences: AIAssistantPreferences): ChatMessage[] {
    const finalizeMessage = (message: ChatMessage): ChatMessage | null => {
        const nextContent = resolveFinalRoundContent(message, response);
        const nextFields = mergeResponseFields(response.fields, normalizeTraceFields(response, preferences.showTraceEntry));
        const responseActions = normalizeActions(response.actions) || [];
        const traceActions = buildTraceDetailAction(response, preferences.showTraceEntry) || [];
        const nextActions = [...responseActions, ...traceActions];
        const nextLocalFilePath = typeof response.local_file_path === 'string' ? response.local_file_path.trim() : '';
        const nextLocalFilePaths = normalizeLocalFilePaths(nextLocalFilePath, response.local_file_paths);
        const nextThumbnailBase64 = response.thumbnail_base64;
        if (!nextContent && !nextFields?.length && !nextActions?.length && !nextThumbnailBase64 && !nextLocalFilePaths?.length) {
            return null;
        }
        return {
            ...message,
            content: nextContent,
            fields: nextFields,
            actions: nextActions.length > 0 ? nextActions : undefined,
            confirmation: response.confirmation,
            unfinishedSlot: (response as any).unfinished_slot,
            localFilePath: nextLocalFilePath || undefined,
            localFilePaths: nextLocalFilePaths,
            thumbnailBase64: nextThumbnailBase64,
        };
    };
    return updateRoundMessage(messages, assistantMessageId, requestId, finalizeMessage);
}

function replaceRoundWithError(messages: ChatMessage[], assistantMessageId: string | null, requestId: string | null, errorText: string): ChatMessage[] {
    if (!assistantMessageId && !requestId) {
        return [...messages, {
            id: nextId(),
            role: 'error',
            content: errorText,
            timestamp: Date.now(),
        }];
    }
    const replaceWithError = (message: ChatMessage): ChatMessage => ({
        ...message,
        role: 'error',
        content: errorText,
        timestamp: Date.now(),
    });
        const nextMessages = updateRoundMessage(messages, assistantMessageId, requestId, replaceWithError);
        return hasRoundMessage(messages, assistantMessageId, requestId)
            ? nextMessages
            : [...messages, {
                id: nextId(),
                role: 'error',
                content: errorText,
                timestamp: Date.now(),
            }];
}

function removeRoundMessage(messages: ChatMessage[], assistantMessageId: string | null, requestId: string | null): ChatMessage[] {
    return updateRoundMessage(messages, assistantMessageId, requestId, () => null);
}

function resolveSendResult(messages: ChatMessage[], assistantMessageId: string | null, requestId: string | null, response: any, preferences: AIAssistantPreferences, errorText?: string): ChatMessage[] {
    if (!errorText && response?.clear_ui) {
        // Backend signaled a conversation reset (/new, /reset, /clear).
        // Clear all existing messages and show only the reset confirmation.
        const resetText = response.text || response.error || '';
        if (!resetText) return [];
        return [{
            id: nextId(),
            role: 'assistant' as const,
            content: resetText,
            timestamp: Date.now(),
        }];
    }
    return errorText
        ? replaceRoundWithError(messages, assistantMessageId, requestId, errorText)
        : response?.error
            ? replaceRoundWithError(messages, assistantMessageId, requestId, response.error)
            : finalizeRoundMessage(messages, assistantMessageId, requestId, response, preferences);
}

function normalizeActionStyle(style: unknown): ChatActionStyle {
    return style === 'danger' ? 'danger' : 'default';
}

function normalizeInitStatus(status: unknown): AIAssistantInitStatus {
    return status === 'loading' || status === 'warming' || status === 'ready' ? status : 'connecting';
}

function normalizeActions(actions: any): ChatAction[] | undefined {
    if (!Array.isArray(actions) || actions.length === 0) return undefined;
    return actions
        .filter(action => action && typeof action.label === 'string' && typeof action.command === 'string')
        .map(action => ({
            label: action.label,
            command: action.command,
            style: normalizeActionStyle(action.style),
        }));
}

function normalizeNewsCategory(category: unknown): NewsCategory {
    return category === 'notice' || category === 'update' || category === 'tip' || category === 'alert' ? category : '';
}

function normalizeStreamEvent(raw: unknown): AIAssistantStreamEvent {
    if (raw && typeof raw === 'object') {
        const event = raw as AIAssistantStreamEvent;
        return {
            request_id: typeof event.request_id === 'string' ? event.request_id : '',
            text: typeof event.text === 'string' ? event.text : '',
        };
    }
    if (typeof raw === 'string') {
        const trimmed = raw.trim();
        if (!trimmed) return { request_id: '', text: '' };
        if (trimmed.startsWith('{') || trimmed.startsWith('[')) {
            try {
                return normalizeStreamEvent(JSON.parse(trimmed));
            } catch {
                return { request_id: '', text: raw };
            }
        }
        return { request_id: '', text: raw };
    }
    return { request_id: '', text: '' };
}

function matchesActiveRequest(round: ActiveRound, event: AIAssistantStreamEvent): boolean {
    if (!round.requestId || round.generation === 0) return false;
    return !event.request_id || event.request_id === round.requestId;
}

function isConfirmationApprovalCommand(text: string): boolean {
    const lower = text.trim().toLowerCase();
    if (!lower) return false;
    const phrases = [
        '确认', '确认了', '可以', '可以开始', '开始吧', '继续', '继续吧', '没问题', '好的开始', '就这样', '按这个来',
        'ok', 'okay', 'confirmed', 'confirm', 'go ahead', 'looks good', 'start', 'continue',
    ];
    return phrases.some(phrase => lower === phrase || lower.includes(phrase));
}

function matchesActiveProgressRequest(round: ActiveRound, event: AIAssistantStreamEvent): boolean {
    if (!round.requestId || round.generation === 0 || round.phase === 'idle') return false;
    return !!event.request_id && event.request_id === round.requestId;
}

function subscribeEvent(eventName: string, handler: (...args: any[]) => void): () => void {
    const unsubscribe = EventsOn(eventName, handler);
    return typeof unsubscribe === 'function' ? unsubscribe : () => EventsOff(eventName);
}

function resolveSendRequestID(response: AIAssistantSendResult | null | undefined): string {
    return typeof response?.request_id === 'string' ? response.request_id.trim() : '';
}

function createForegroundRequestID(): string {
    return `desktop-ai-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}

function resolveBackgroundSessionID(result: StartAIAssistantBackgroundTaskResult | null | undefined): string {
    return String(result?.session_id || result?.sessionID || "").trim();
}

function resolveBackgroundJobID(result: StartAIAssistantBackgroundTaskResult | null | undefined): string {
    return String(result?.job_id || result?.jobID || "").trim();
}

function resolveBackgroundRunID(result: StartAIAssistantBackgroundTaskResult | null | undefined): string {
    return String(result?.run_id || result?.runID || "").trim();
}

function normalizeSessionID(value: unknown): string {
    return typeof value === 'string' ? value.trim() : '';
}

function normalizeStatusKey(value: unknown): string {
    return typeof value === 'string' ? value.trim().toLowerCase() : '';
}

function normalizeLaunchSource(value: unknown): string {
    return typeof value === 'string' ? value.trim().toLowerCase() : '';
}

function isTerminalRemoteStatus(status: unknown): boolean {
    const key = normalizeStatusKey(status);
    return key === 'stopped'
        || key === 'finished'
        || key === 'failed'
        || key === 'killed'
        || key === 'exited'
        || key === 'closed'
        || key === 'done'
        || key === 'error'
        || key === 'completed'
        || key === 'terminated';
}

function matchesPendingTaskSession(session: AIAssistantRemoteSessionView, pendingTask: AIAssistantPendingTask | null): boolean {
    if (!pendingTask) return false;
    const sessionID = normalizeSessionID(session?.id);
    const jobID = normalizeSessionID(session?.job_id);
    const runID = normalizeSessionID(session?.run_id);
    if (pendingTask.sessionID && sessionID === pendingTask.sessionID) return true;
    if (pendingTask.jobID && jobID === pendingTask.jobID) return true;
    if (pendingTask.runID && runID === pendingTask.runID) return true;
    return false;
}

async function resolvePendingAITask(requestId: string, response: AIAssistantSendResult | null | undefined): Promise<AIAssistantPendingTask | null> {
    const runID = normalizeSessionID(response?.run_id);
    const jobID = normalizeSessionID(response?.job_id);
    if (!response?.deferred && !runID && !jobID) {
        return null;
    }
    try {
        const sessions = await ListRemoteSessions() as AIAssistantRemoteSessionView[];
        for (const session of Array.isArray(sessions) ? sessions : []) {
            if (normalizeLaunchSource(session?.launch_source) !== 'ai') continue;
            if (isTerminalRemoteStatus(session?.status)) continue;
            const sessionRunID = normalizeSessionID(session?.run_id);
            const sessionJobID = normalizeSessionID(session?.job_id);
            if (runID && sessionRunID && sessionRunID === runID) {
                return {
                    requestId,
                    sessionID: normalizeSessionID(session?.id) || undefined,
                    jobID: sessionJobID || jobID || undefined,
                    runID: sessionRunID || runID || undefined,
                };
            }
            if (jobID && sessionJobID && sessionJobID === jobID) {
                return {
                    requestId,
                    sessionID: normalizeSessionID(session?.id) || undefined,
                    jobID: sessionJobID || jobID || undefined,
                    runID: sessionRunID || runID || undefined,
                };
            }
        }
    } catch {
        return null;
    }
    return null;
}

function buildBackgroundLaunchNotice(result: AIAssistantBackgroundLaunchResult): string {
    const detailLines = [
        "已转到后台运行。",
        `任务会显示在“任务管理”里的后台列表。`,
        `session_id: ${result.sessionID}`,
    ];
    if (result.jobID) detailLines.push(`job_id: ${result.jobID}`);
    if (result.runID) detailLines.push(`run_id: ${result.runID}`);
    return detailLines.join("\n");
}

function createSystemMessage(content: string): ChatMessage {
    return {
        id: nextId(),
        role: 'system',
        content,
        timestamp: Date.now(),
    };
}

function createTraceMessage(content: string, fields: Array<{ label: string; value: string }>): ChatMessage {
    return {
        id: nextId(),
        role: 'system',
        kind: 'trace',
        content,
        fields,
        timestamp: Date.now(),
    };
}

function createUserMessage(content: string): ChatMessage {
    return {
        id: nextId(),
        role: 'user',
        content,
        timestamp: Date.now(),
    };
}

function createErrorMessage(content: string): ChatMessage {
    return {
        id: nextId(),
        role: 'error',
        content,
        timestamp: Date.now(),
    };
}

function appendBackgroundLaunchMessages(messages: ChatMessage[], outgoingText: string, result: AIAssistantBackgroundLaunchResult): ChatMessage[] {
    return [
        ...messages,
        createUserMessage(outgoingText),
        createSystemMessage(buildBackgroundLaunchNotice(result)),
    ];
}

function createNewsMessage(article: any): ChatMessage {
    const iconByCategory: Record<Exclude<NewsCategory, ''>, string> = { notice: '📢', update: '🚀', tip: '💡', alert: '⚠️' };
    const category = normalizeNewsCategory(article?.category);
    const title = typeof article?.title === 'string' ? article.title : '';
    const body = typeof article?.content === 'string' ? article.content : '';
    const articleId = String(article?.id ?? nextId());
    return {
        id: `news-${articleId}`,
        role: 'system',
        kind: 'news',
        content: body,
        news: {
            articleId,
            category,
            title,
            body,
            icon: category ? iconByCategory[category] : '📄',
        },
        timestamp: Date.now(),
    };
}

export function isPinnedNewsMessage(message: ChatMessage): boolean {
    return message.kind === 'news' && !!message.news;
}

function samePinnedNews(left: ChatMessage, right: ChatMessage): boolean {
    if (!isPinnedNewsMessage(left) || !isPinnedNewsMessage(right)) return false;
    const leftNews = left.news;
    const rightNews = right.news;
    if (!leftNews || !rightNews) return false;
    return leftNews.articleId === rightNews.articleId
        && leftNews.category === rightNews.category
        && leftNews.title === rightNews.title
        && leftNews.body === rightNews.body
        && leftNews.icon === rightNews.icon;
}

export function useAIAssistant(options?: { refreshSessionsOnly?: () => Promise<void> }) {
    const [messages, setMessages] = useState<ChatMessage[]>(loadPersistedMessages);
    const [submittedPrompts, setSubmittedPrompts] = useState<string[]>(loadPersistedPrompts);
    const [draftInputValue, setDraftInputValue] = useState("");
    const [progressMessages, setProgressMessages] = useState<ChatMessage[]>([]);
    const [selectedFilePaths, setSelectedFilePaths] = useState<string[]>([]);
    const [trialReflectEnabled, setTrialReflectEnabled] = useState(false);
    const [preferences, setPreferences] = useState<AIAssistantPreferences>({ showTraceEntry: false });
    const [initStatus, setInitStatus] = useState<AIAssistantInitStatus>("connecting");
    const [scrollToTopSeq, setScrollToTopSeq] = useState(0);
    const [activeRound, setActiveRound] = useState<ActiveRound>(IDLE_ROUND);
    const [pendingTask, setPendingTask] = useState<AIAssistantPendingTask | null>(null);
    const activeRoundRef = useRef<ActiveRound>(IDLE_ROUND);
    const pendingTaskRef = useRef<AIAssistantPendingTask | null>(null);
    const initStatusRef = useRef<AIAssistantInitStatus>("connecting");
    const latestNewsPayloadRef = useRef<string>("[]");
    const progressTailRef = useRef<string | null>(null);
    const scrollOnNextNewsRef = useRef(true);
    const refreshSessionsOnlyRef = useRef(options?.refreshSessionsOnly);

    useEffect(() => {
        refreshSessionsOnlyRef.current = options?.refreshSessionsOnly;
    }, [options?.refreshSessionsOnly]);

    const setPendingTaskState = useCallback((nextTask: AIAssistantPendingTask | null) => {
        pendingTaskRef.current = nextTask;
        setPendingTask(current => {
            const same = current?.requestId === nextTask?.requestId
                && current?.sessionID === nextTask?.sessionID
                && current?.jobID === nextTask?.jobID
                && current?.runID === nextTask?.runID;
            return same ? current : nextTask;
        });
    }, []);

    useEffect(() => {
        let cancelled = false;
        GetTrialReflectEnabled()
            .then(enabled => {
                if (!cancelled) setTrialReflectEnabled(!!enabled);
            })
            .catch(() => {
                if (!cancelled) setTrialReflectEnabled(false);
            });
        LoadConfig()
            .then(config => {
                if (!cancelled) {
                    const appConfig = new main.AppConfig(config || {});
                    setPreferences({ showTraceEntry: !!appConfig?.show_ai_trace_entry });
                    setTrialReflectEnabled(!!appConfig?.trial_reflect_enabled);
                }
            })
            .catch(() => {
                if (!cancelled) {
                    setPreferences({ showTraceEntry: false });
                }
            });
        const cleanup = subscribeEvent("config-changed", (cfg?: unknown) => {
            if (cancelled) return;
            const appConfig = new main.AppConfig(cfg || {});
            setPreferences({ showTraceEntry: !!appConfig?.show_ai_trace_entry });
            setTrialReflectEnabled(!!appConfig?.trial_reflect_enabled);
        });
        return () => {
            cancelled = true;
            cleanup();
        };
    }, []);

    const sending = activeRound.phase !== 'idle' || !!pendingTask;
    const streaming = activeRound.phase === 'streaming';
    const visualBusy = streaming;
    const ready = initStatus === 'ready';

    const setInitStatusState = useCallback((nextStatus: AIAssistantInitStatus) => {
        if (initStatusRef.current === nextStatus) {
            return nextStatus;
        }
        initStatusRef.current = nextStatus;
        setInitStatus(current => current === nextStatus ? current : nextStatus);
        return nextStatus;
    }, []);

    useEffect(() => {
        let cancelled = false;
        let pollTimer: ReturnType<typeof setTimeout> | null = null;

        const clearPollTimer = () => {
            if (pollTimer) {
                clearTimeout(pollTimer);
                pollTimer = null;
            }
        };

        const scheduleCheck = () => {
            if (cancelled || initStatusRef.current === 'ready' || pollTimer) return;
            pollTimer = setTimeout(() => {
                pollTimer = null;
                void check();
            }, 1500);
        };

        const check = async () => {
            try {
                const { ready: isReady, status } = await checkInitReadiness();
                if (cancelled || initStatusRef.current === 'ready') return;
                if (isReady) {
                    clearPollTimer();
                    setInitStatusState('ready');
                    return;
                }
                setInitStatusState(status);
                scheduleCheck();
            } catch {
                if (!cancelled && initStatusRef.current !== 'ready') {
                    scheduleCheck();
                }
            }
        };

        void check();

        const progressHandler = (status: string) => {
            const nextStatus = normalizeInitStatus(status);
            if (nextStatus === 'ready') {
                clearPollTimer();
            }
            setInitStatusState(nextStatus);
        };
        const offInitProgress = subscribeEvent(INIT_PROGRESS_EVENT, progressHandler);

        return () => {
            cancelled = true;
            clearPollTimer();
            offInitProgress();
        };
    }, [setInitStatusState]);

    const setSelectedFiles = useCallback((nextPaths: string[]) => {
        const normalized = nextPaths.map(normalizeSelectedFilePath).filter(Boolean);
        setSelectedFilePaths(normalized);
        return normalized;
    }, []);

    const addSelectedFiles = useCallback((newPaths: string[]) => {
        const normalized = newPaths.map(normalizeSelectedFilePath).filter(Boolean);
        if (normalized.length === 0) return;
        setSelectedFilePaths(prev => {
            const existing = new Set(prev);
            return [...prev, ...normalized.filter(p => !existing.has(p))];
        });
    }, []);

    const removeSelectedFile = useCallback((index: number) => {
        setSelectedFilePaths(prev => prev.filter((_, i) => i !== index));
    }, []);

    const setRoundState = useCallback((next: ActiveRound) => {
        const current = activeRoundRef.current;
        if (sameActiveRound(current, next)) {
            return current;
        }
        activeRoundRef.current = next;
        setActiveRound(prev => sameActiveRound(prev, next) ? prev : next);
        return next;
    }, []);

    const transitionRound = useCallback((updater: (current: ActiveRound) => ActiveRound) => {
        const next = updater(activeRoundRef.current);
        if (next === activeRoundRef.current || sameActiveRound(next, activeRoundRef.current)) {
            return activeRoundRef.current;
        }
        return setRoundState(next);
    }, [setRoundState]);

    const resetActiveRound = useCallback((generation?: number) => {
        const current = activeRoundRef.current;
        const next = generation === undefined
            ? (current.generation === 0 ? IDLE_ROUND : createIdleRound(current.generation))
            : (generation === 0 ? IDLE_ROUND : createIdleRound(generation));
        if (sameActiveRound(current, next)) {
            return current;
        }
        return setRoundState(next);
    }, [setRoundState]);

    const ensureRoundPlaceholder = useCallback((generation: number) => {
        const current = activeRoundRef.current;
        if (current.generation !== generation) {
            return null;
        }
        const assistantMessageId = current.assistantMessageId || nextId();
        if (current.phase !== 'streaming' || current.assistantMessageId !== assistantMessageId) {
            setRoundState({
                generation,
                phase: 'streaming',
                assistantMessageId,
                requestId: current.requestId,
            });
        }
        setMessages(prev => appendAssistantPlaceholder(prev, assistantMessageId, current.requestId));
        return assistantMessageId;
    }, [setRoundState]);

    const finalizeRound = useCallback((generation: number) => {
        if (activeRoundRef.current.generation !== generation) return;
        resetActiveRound();
    }, [resetActiveRound]);

    const persistTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const latestMessagesRef = useRef(messages);
    const latestPromptsRef = useRef(submittedPrompts);
    const lastPersistedPayloadRef = useRef<string | null>(null);
    const lastPersistedPromptsPayloadRef = useRef<string | null>(null);
    const persistOnUnmountRef = useRef(true);
    useEffect(() => {
        latestMessagesRef.current = messages;
        const nextPayload = serializePersistedMessages(messages);
        if (nextPayload === lastPersistedPayloadRef.current) {
            if (persistTimerRef.current) {
                clearTimeout(persistTimerRef.current);
                persistTimerRef.current = null;
            }
            return;
        }
        if (persistTimerRef.current) clearTimeout(persistTimerRef.current);
        persistTimerRef.current = setTimeout(() => {
            persistTimerRef.current = null;
            const payload = serializePersistedMessages(latestMessagesRef.current);
            if (payload === lastPersistedPayloadRef.current) return;
            lastPersistedPayloadRef.current = payload;
            persistMessages(latestMessagesRef.current);
        }, 300);
    }, [messages]);
    useEffect(() => {
        latestPromptsRef.current = submittedPrompts;
        const nextPayload = JSON.stringify(submittedPrompts);
        if (nextPayload === lastPersistedPromptsPayloadRef.current) {
            return;
        }
        lastPersistedPromptsPayloadRef.current = nextPayload;
        persistPrompts(submittedPrompts);
    }, [submittedPrompts]);
    useEffect(() => {
        lastPersistedPayloadRef.current = serializePersistedMessages(latestMessagesRef.current);
        lastPersistedPromptsPayloadRef.current = JSON.stringify(latestPromptsRef.current);
        return () => {
            if (persistTimerRef.current) {
                clearTimeout(persistTimerRef.current);
                persistTimerRef.current = null;
            }
            if (!persistOnUnmountRef.current) {
                return;
            }
            const payload = serializePersistedMessages(latestMessagesRef.current);
            if (payload !== lastPersistedPayloadRef.current) {
                lastPersistedPayloadRef.current = payload;
                persistMessages(latestMessagesRef.current);
            }
            const promptPayload = JSON.stringify(latestPromptsRef.current);
            if (promptPayload !== lastPersistedPromptsPayloadRef.current) {
                lastPersistedPromptsPayloadRef.current = promptPayload;
                persistPrompts(latestPromptsRef.current);
            }
        };
    }, []);

    // Fetch latest news from Hub Center and prepend as system messages.
    const doFetchNews = useCallback(() => {
        FetchNews().then((articles: any[]) => {
            const newsMessages = Array.isArray(articles) ? articles.map(createNewsMessage) : [];
            const nextPayload = serializeNewsMessages(newsMessages);
            if (nextPayload === latestNewsPayloadRef.current) {
                return;
            }
            latestNewsPayloadRef.current = nextPayload;
            setMessages(prev => replaceNewsMessages(prev, newsMessages));
            if (scrollOnNextNewsRef.current && newsMessages.length > 0) {
                scrollOnNextNewsRef.current = false;
                setScrollToTopSeq(s => s + 1);
            }
        }).catch(() => { /* silently ignore news fetch failures */ });
    }, []);

    // Fetch on mount + refresh every 6 hours.
    const newsFetchedRef = useRef(false);
    useEffect(() => {
        if (!newsFetchedRef.current) {
            newsFetchedRef.current = true;
            doFetchNews();
        }
        const SIX_HOURS = 6 * 60 * 60 * 1000;
        const timer = setInterval(doFetchNews, SIX_HOURS);
        return () => clearInterval(timer);
    }, [doFetchNews]);

    useEffect(() => {
        const refreshPendingTask = async () => {
            const currentTask = pendingTaskRef.current;
            if (!currentTask) return;
            try {
                const sessions = await ListRemoteSessions() as AIAssistantRemoteSessionView[];
                const stillActive = (Array.isArray(sessions) ? sessions : []).some(session => {
                    if (normalizeLaunchSource(session?.launch_source) !== 'ai') return false;
                    if (isTerminalRemoteStatus(session?.status)) return false;
                    return matchesPendingTaskSession(session, currentTask);
                });
                if (!stillActive && pendingTaskRef.current?.requestId === currentTask.requestId) {
                    setPendingTaskState(null);
                }
            } catch {
            }
        };

        void refreshPendingTask();
        if (!pendingTask) return;

        const handleRemoteStateChanged = () => {
            void refreshPendingTask();
        };
        const offRemoteStateChanged = subscribeEvent('remote-state-changed', handleRemoteStateChanged);
        const offRemoteSessionChanged = subscribeEvent('remote-session-changed', handleRemoteStateChanged);
        const timer = setInterval(() => {
            void refreshPendingTask();
        }, 2000);

        return () => {
            clearInterval(timer);
            offRemoteStateChanged();
            offRemoteSessionChanged();
        };
    }, [pendingTask, setPendingTaskState]);

    useEffect(() => {
        const tokenHandler = (payload: unknown) => {
            const currentRound = activeRoundRef.current;
            const event = normalizeStreamEvent(payload);
            if (!matchesActiveRequest(currentRound, event)) return;
            if (!currentRound.assistantMessageId || !event.text) return;
            setMessages(prev => updateTailMessage(prev, currentRound.assistantMessageId, message => appendTokenToMessage(message, event.text || ''))
                ?? updateMessageById(prev, currentRound.assistantMessageId, message => appendTokenToMessage(message, event.text || '')));
        };

        const newRoundHandler = (payload: unknown) => {
            const currentRound = activeRoundRef.current;
            const event = normalizeStreamEvent(payload);
            if (!matchesActiveRequest(currentRound, event)) return;
            ensureRoundPlaceholder(currentRound.generation);
        };

        const streamDoneHandler = (payload: unknown) => {
            const currentRound = activeRoundRef.current;
            const event = normalizeStreamEvent(payload);
            if (!matchesActiveRequest(currentRound, event)) return;
            transitionRound(current => {
                if (current.phase !== 'streaming') return current;
                return { ...current, phase: 'requesting' };
            });
        };

        const offStreamToken = subscribeEvent(STREAM_TOKEN_EVENT, tokenHandler);
        const offNewRound = subscribeEvent(NEW_ROUND_EVENT, newRoundHandler);
        const offStreamDone = subscribeEvent(STREAM_DONE_EVENT, streamDoneHandler);
        return () => {
            offStreamToken();
            offNewRound();
            offStreamDone();
        };
    }, [ensureRoundPlaceholder, transitionRound]);

    const sendMessage = useCallback(async (text: string, options?: SendMessageOptions) => {
        // Callers (e.g. handleSend in AIAssistantPanel) are responsible for
        // embedding file paths into `text` via buildOutgoingMessageMulti before calling here.
        const outgoingText = text.trim();
        if (outgoingText === "" || activeRoundRef.current.phase !== 'idle') return;

        const generation = activeRoundRef.current.generation + 1;
        const assistantMessageId = nextId();
        const requestId = createForegroundRequestID();
        const userMsg: ChatMessage = {
            id: nextId(),
            role: 'user',
            content: outgoingText,
            timestamp: Date.now(),
        };
        const placeholderMsg: ChatMessage = {
            id: assistantMessageId,
            role: 'assistant',
            content: '',
            requestId,
            timestamp: Date.now(),
        };
        const approvalMessage = isConfirmationApprovalCommand(text);

        setRoundState({
            generation,
            phase: 'requesting',
            assistantMessageId,
            requestId,
        });
        setMessages(prev => {
            const nextMessages = approvalMessage ? markLatestConfirmationAsRunning(prev) : prev;
            return [...nextMessages, userMsg, placeholderMsg];
        });

        try {
            const rawResponse = await SendAIAssistantMessage({
                text: outgoingText,
                request_id: requestId,
                resume_slot_id: options?.resumeSlotID,
                start_new_task: options?.startNewTask,
                dismiss_slot_id: options?.dismissSlotID,
                ui_action: options?.uiAction,
            }) as AIAssistantSendResult;
            const response = normalizeSendResponse(rawResponse, preferences.showTraceEntry);
            const responseRequestId = resolveSendRequestID(response);
            const effectiveRequestId = responseRequestId || requestId;
            if (activeRoundRef.current.generation !== generation && activeRoundRef.current.requestId !== effectiveRequestId) {
                return;
            }
            if (responseRequestId && responseRequestId !== requestId) {
                setRoundState({
                    generation,
                    phase: activeRoundRef.current.phase,
                    assistantMessageId,
                    requestId: responseRequestId,
                });
            }
            setMessages(prev => resolveSendResult(prev, assistantMessageId, effectiveRequestId, response, preferences));
            if (response.clear_ui) {
                // Backend signaled conversation reset — clear transient UI state
                // and immediately flush persistence to avoid stale debounced writes.
                if (persistTimerRef.current) {
                    clearTimeout(persistTimerRef.current);
                    persistTimerRef.current = null;
                }
                lastPersistedPayloadRef.current = null;
                localStorage.removeItem(AI_ASSISTANT_HISTORY_STORAGE_KEY);
                progressTailRef.current = null;
                setProgressMessages([]);
            }
            if (response.deferred) {
                setPendingTaskState(await resolvePendingAITask(effectiveRequestId, response) ?? { requestId: effectiveRequestId, jobID: response.job_id || undefined, runID: response.run_id || undefined });
            } else {
                setPendingTaskState(null);
            }
        } catch (err: any) {
            setMessages(prev => resolveSendResult(prev, assistantMessageId, requestId, null, preferences, err?.message || String(err)));
            setPendingTaskState(null);
        } finally {
            finalizeRound(generation);
        }
    }, [finalizeRound, preferences, setRoundState]);

    const sendMessageInBackground = useCallback(async (text: string) => {
        // Callers are responsible for embedding file paths into `text` before calling here.
        const outgoingText = text.trim();
        if (outgoingText === "" || activeRoundRef.current.phase !== 'idle') return;

        const generation = activeRoundRef.current.generation + 1;
        setRoundState({
            generation,
            phase: 'requesting',
            assistantMessageId: null,
            requestId: '',
        });

        try {
            const response = await StartAIAssistantBackgroundTask({
                text: outgoingText,
                force_background: true,
            }) as StartAIAssistantBackgroundTaskResult;
            const sessionID = resolveBackgroundSessionID(response);
            if (!response?.accepted || !sessionID) {
                throw new Error(response?.error || 'failed to start background task');
            }
            const launchResult: AIAssistantBackgroundLaunchResult = {
                sessionID,
                jobID: resolveBackgroundJobID(response) || undefined,
                runID: resolveBackgroundRunID(response) || undefined,
            };
            setMessages(prev => appendBackgroundLaunchMessages(prev, outgoingText, launchResult));
            await refreshSessionsOnlyRef.current?.();
        } catch (err: any) {
            setMessages(prev => [...prev, createUserMessage(outgoingText), createErrorMessage(err?.message || String(err))]);
        } finally {
            finalizeRound(generation);
        }
    }, [finalizeRound, setRoundState]);

    // sendBtwMessage sends a /btw side query via a dedicated backend binding.
    // Unlike sendMessage, this does NOT check activeRound.phase — it can run
    // while the main agent loop is active. It does NOT affect activeRound state,
    // so the main loop's streaming/progress continues uninterrupted.
    //
    // The result is appended as a pair of user+assistant messages to the chat.
    // Streaming tokens arrive on "ai-btw-token" events (separate from the main
    // "ai-assistant-token" channel).
    const sendBtwMessage = useCallback(async (query: string) => {
        const trimmedQuery = query.trim();
        if (!trimmedQuery) {
            // Show usage help as a local message — no backend call needed.
            setMessages(prev => [...prev,
                { id: nextId(), role: 'user' as const, content: '/btw', timestamp: Date.now() },
                { id: nextId(), role: 'assistant' as const, content: '用法: /btw <查询内容>\n\n示例:\n  /btw 最新的 Go 1.23 有什么新特性\n  /btw React 19 的主要变化\n  /btw 这个项目用了什么框架', timestamp: Date.now() },
            ]);
            return;
        }

        const requestId = `btw-${Date.now()}`;
        const userMsgId = nextId();
        const assistantMsgId = nextId();

        // Add user message and placeholder immediately.
        const userMsg: ChatMessage = {
            id: userMsgId,
            role: 'user',
            content: '/btw ' + trimmedQuery,
            timestamp: Date.now(),
        };
        const placeholderMsg: ChatMessage = {
            id: assistantMsgId,
            role: 'assistant',
            content: '',
            requestId,
            timestamp: Date.now(),
        };
        setMessages(prev => [...prev, userMsg, placeholderMsg]);

        // Listen for streaming tokens on the /btw-specific channel.
        const tokenHandler = (payload: unknown) => {
            const event = typeof payload === 'string' ? JSON.parse(payload) : (payload as any);
            if (event?.request_id !== requestId) return;
            const delta = event?.text || '';
            if (!delta) return;
            setMessages(prev => prev.map(m =>
                m.id === assistantMsgId ? { ...m, content: m.content + delta } : m
            ));
        };
        const progressHandler = (payload: unknown) => {
            const event = typeof payload === 'string' ? JSON.parse(payload) : (payload as any);
            if (event?.request_id !== requestId) return;
            // Progress is informational — could show in a status bar, but for
            // simplicity we skip it. The streaming tokens provide real-time feedback.
        };

        EventsOn("ai-btw-token", tokenHandler);
        EventsOn("ai-btw-progress", progressHandler);

        try {
            const response = await SendBtwQuery(trimmedQuery, requestId) as any;
            const finalText = response?.text || response?.error || '查询失败';
            // Replace placeholder content with the final result.
            // If streaming already populated content, the final text is authoritative.
            setMessages(prev => prev.map(m =>
                m.id === assistantMsgId ? { ...m, content: finalText } : m
            ));
        } catch (err: any) {
            setMessages(prev => prev.map(m =>
                m.id === assistantMsgId ? { ...m, content: `❌ /btw 查询失败: ${err?.message || String(err)}` } : m
            ));
        } finally {
            EventsOff("ai-btw-token");
            EventsOff("ai-btw-progress");
        }
    }, []);

    const browseFile = useCallback(async () => {
        try {
            const selected = await SelectAIAssistantFiles();
            if (!selected || selected.length === 0) return; // User cancelled or no files selected
            const validPaths = selected.filter(p => p && p.trim());
            if (validPaths.length > 0) {
                addSelectedFiles(validPaths);
            }
        } catch (err) {
            console.error('Failed to select files:', err);
        }
    }, [addSelectedFiles]);

    const clearSelectedFile = useCallback(() => {
        setSelectedFiles([]);
    }, [setSelectedFiles]);

    const clearHistory = useCallback(async () => {
        resetActiveRound();
        setPendingTaskState(null);
        setSelectedFiles([]);
        if (persistTimerRef.current) {
            clearTimeout(persistTimerRef.current);
            persistTimerRef.current = null;
        }
        latestMessagesRef.current = [];
        lastPersistedPayloadRef.current = null;
        persistOnUnmountRef.current = false;
        localStorage.removeItem(AI_ASSISTANT_HISTORY_STORAGE_KEY);
        setMessages([]);
        setDraftInputValue("");
        progressTailRef.current = null;
        setProgressMessages([]);
        latestNewsPayloadRef.current = '[]';
        scrollOnNextNewsRef.current = true;
        doFetchNews();
        try {
            await ClearAIAssistantHistory();
        } catch (_) {
        } finally {
            persistOnUnmountRef.current = true;
        }
    }, [doFetchNews, resetActiveRound, setSelectedFiles]);

    const recordSubmittedPrompt = useCallback((prompt: string) => {
        setSubmittedPrompts(prev => appendSubmittedPrompt(prev, prompt));
    }, []);

    const executeAction = useCallback(async (command: string) => {
        // Handle critical-risk skill installation confirmation responses.
        const criticalConfirmMatch = command.match(/^__resolve_critical_confirm__\s+(\S+)\s+(confirm|reject)$/);
        if (criticalConfirmMatch) {
            const confirmID = criticalConfirmMatch[1] || '';
            const confirmed = criticalConfirmMatch[2] === 'confirm';
            // Remove buttons from the confirmation message immediately so the
            // user sees their click was registered. This is not a workaround —
            // it's the standard UI pattern for one-shot action buttons.
            const feedbackText = confirmed
                ? '\n\n✅ 已确认，正在安装...'
                : '\n\n❌ 已拒绝安装。';
            setMessages(prev => prev.map(m =>
                m.actions?.some(a => a.command.includes(confirmID))
                    ? { ...m, actions: undefined, content: m.content + feedbackText }
                    : m
            ));
            try {
                await ResolveCriticalConfirm(confirmID, confirmed);
            } catch (err: any) {
                // Backend returned an error (e.g. confirmation expired).
                // Show the error to the user.
                setMessages(prev => [...prev, createErrorMessage(err?.message || String(err))]);
            }
            return;
        }
        const traceMatch = command.match(/^__view_trace__\s+(\S+)$/);
        if (traceMatch) {
            const runID = traceMatch[1]?.trim() || '';
            if (!runID) return;
            try {
                const view = await GetAIAssistantTrace(runID) as AIAssistantTraceView;
                setMessages(prev => [...prev, createTraceMessage(buildTraceDetailMessage(view, runID), buildTraceDetailFields(view, runID))]);
            } catch (err: any) {
                setMessages(prev => [...prev, createErrorMessage(err?.message || String(err))]);
            }
            return;
        }
        const resumeMatch = command.match(/^__resume_unfinished__\s+(\S+)$/);
        if (resumeMatch) {
            return sendMessage('继续上次未完成任务', { resumeSlotID: resumeMatch[1]?.trim() || '' });
        }
        // Backward compat: __start_new_task__ is no longer emitted by the
        // backend (merged into __dismiss_unfinished__), but keep the handler
        // in case older backend versions are still in use.
        if (command === '__start_new_task__') {
            return sendMessage('开始一个新任务', { startNewTask: true, uiAction: true });
        }
        const dismissMatch = command.match(/^__dismiss_unfinished__\s+(\S+)$/);
        if (dismissMatch) {
            return sendMessage('放弃上次未完成任务', { dismissSlotID: dismissMatch[1]?.trim() || '', startNewTask: true, uiAction: true });
        }
        return sendMessage(command);
    }, [sendMessage]);

    useEffect(() => {
        const handler = (payload: unknown) => {
            const event = normalizeStreamEvent(payload);
            const progressText = event.text || (typeof payload === 'string' ? payload : '');
            if (!progressText) return;
            if (!matchesActiveProgressRequest(activeRoundRef.current, event)) {
                return;
            }
            if (shouldHideProgressText(progressText)) {
                return;
            }
            if (progressTailRef.current === progressText) {
                return;
            }
            progressTailRef.current = progressText;
            setProgressMessages(prev => appendProgressText(prev, progressText));
        };
        const offProgress = subscribeEvent(PROGRESS_EVENT, handler);
        return () => {
            offProgress();
        };
    }, []);

    // Listen for critical-risk skill installation confirmation events from the backend.
    useEffect(() => {
        const handler = (payload: unknown) => {
            if (!payload || typeof payload !== 'object') return;
            const data = payload as Record<string, unknown>;
            const confirmID = typeof data.confirm_id === 'string' ? data.confirm_id : '';
            const summary = typeof data.summary === 'string' ? data.summary : '';
            if (!confirmID) return;
            const msg: ChatMessage = {
                id: nextId(),
                role: 'assistant',
                content: summary,
                actions: [
                    { label: '✅ 确认安装', command: `__resolve_critical_confirm__ ${confirmID} confirm`, style: 'default' as const },
                    { label: '❌ 拒绝安装', command: `__resolve_critical_confirm__ ${confirmID} reject`, style: 'danger' as const },
                ],
                timestamp: Date.now(),
            };
            setMessages(prev => [...prev, msg]);
        };
        const offCriticalConfirm = subscribeEvent('critical-risk-confirm', handler);
        return () => {
            offCriticalConfirm();
        };
    }, []);

    const cancelSession = useCallback(async (): Promise<CancelAIAssistantResult> => {
        const canceledRound = activeRoundRef.current;
        const pendingTaskAtCancel = pendingTaskRef.current;
        const nextGeneration = canceledRound.generation + 1;
        if (!canceledRound.assistantMessageId && isRoundIdle(canceledRound) && !pendingTaskAtCancel) {
            return { canceledText: "" };
        }
        if (canceledRound.assistantMessageId) {
            setMessages(prev => removeRoundMessage(prev, canceledRound.assistantMessageId, canceledRound.requestId));
        }
        resetActiveRound(nextGeneration);
        setPendingTaskState(null);
        try {
            if (pendingTaskAtCancel?.sessionID) {
                await CancelAIAssistantTask(pendingTaskAtCancel.sessionID);
                return { canceledText: "" };
            }
            if (!canceledRound.assistantMessageId) {
                const lastSessionMessage = [...latestMessagesRef.current]
                    .reverse()
                    .find((message) => message.role === 'system' && /session_id:\s*/i.test(message.content));
                const sessionIDMatch = lastSessionMessage?.content.match(/session_id:\s*(\S+)/i);
                const sessionID = sessionIDMatch?.[1]?.trim() || "";
                if (sessionID) {
                    await CancelAIAssistantTask(sessionID);
                    return { canceledText: "" };
                }
            }
            return {
                canceledText: (await CancelAIAssistantSession()) || "",
            };
        } catch {
            return { canceledText: "" };
        }
    }, [resetActiveRound, setPendingTaskState]);

    // injectSupplementary sends a supplementary message into the running
    // agent loop without cancelling it. Returns true if the injection was
    // accepted (a loop is active), false if no loop is running (caller
    // should fall back to normal sendMessage).
    // On success, a user message bubble is added to the chat so the user
    // sees visual confirmation of what was injected.
    const injectSupplementary = useCallback(async (text: string): Promise<boolean> => {
        try {
            const accepted = await InjectAIAssistantSupplementary(text);
            if (accepted) {
                // Show the injected text as a user message in the chat area
                // so the user has visual confirmation.
                setMessages(prev => [...prev, createUserMessage("💬 " + text)]);
            }
            return accepted;
        } catch {
            return false;
        }
    }, []);

    return { messages, submittedPrompts, draftInputValue, progressMessages, sending, streaming, visualBusy, ready, initStatus, selectedFilePaths, trialReflectEnabled, browseFile, clearSelectedFile, removeSelectedFile, sendMessage, sendBtwMessage, sendMessageInBackground, clearHistory, recordSubmittedPrompt, setDraftInputValue, executeAction, refreshNews: doFetchNews, scrollToTopSeq, cancelSession, injectSupplementary };
}

// Polyfill for Array.findLastIndex (not available in all environments)
export function findLastIndex<T>(arr: T[], predicate: (item: T) => boolean): number {
    for (let i = arr.length - 1; i >= 0; i--) {
        if (predicate(arr[i])) return i;
    }
    return -1;
}
