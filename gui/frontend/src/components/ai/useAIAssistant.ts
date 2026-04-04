import { useState, useEffect, useCallback, useRef } from "react";
import { SendAIAssistantMessage, ClearAIAssistantHistory, FetchNews, IsAIAssistantReady, GetAIAssistantInitStatus, CancelAIAssistantSession, CancelAIAssistantTask, SelectAIAssistantFile, StartAIAssistantBackgroundTask, GetTrialReflectEnabled, GetAIAssistantTrace } from "../../../wailsjs/go/main/App";
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
    error?: string;
    fields?: any;
    actions?: any;
    local_file_path?: string;
    local_file_paths?: string[];
    thumbnail_base64?: string;
    request_id?: string;
    trace_summary?: string;
    trace_event_count?: number;
    evidence_count?: number;
    job_id?: string;
    run_id?: string;
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

interface AIAssistantTraceView {
    job_id?: string;
    run_id?: string;
    status?: string;
    summary?: string;
    event_count?: number;
    evidence_count?: number;
    events?: Array<{ kind?: string; summary?: string }>;
    evidence?: Array<{ source_kind?: string; category?: string; summary?: string }>;
}

export interface ChatMessage {
    id: string;
    role: 'user' | 'assistant' | 'progress' | 'error' | 'system';
    kind?: 'news' | 'trace';
    content: string;
    news?: NewsCardData;
    fields?: Array<{ label: string; value: string }>;
    actions?: ChatAction[];
    localFilePath?: string;
    localFilePaths?: string[];
    thumbnailBase64?: string;
    timestamp: number;
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

function normalizeSelectedFilePath(filePath: string): string {
    return filePath.trim();
}

function sameSelectedFilePath(left: string, right: string): boolean {
    return normalizeSelectedFilePath(left) === normalizeSelectedFilePath(right);
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

function appendAssistantPlaceholder(messages: ChatMessage[], assistantMessageId: string): ChatMessage[] {
    const index = messages.findIndex(msg => msg.id === assistantMessageId);
    if (index >= 0) return messages;
    return [...messages, {
        id: assistantMessageId,
        role: 'assistant',
        content: '',
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

function normalizeTraceFields(response: any): Array<{ label: string; value: string }> {
    const fields: Array<{ label: string; value: string }> = [];
    const traceSummary = typeof response?.trace_summary === 'string' ? response.trace_summary.trim() : '';
    const traceEventCount = typeof response?.trace_event_count === 'number' ? response.trace_event_count : 0;
    const evidenceCount = typeof response?.evidence_count === 'number' ? response.evidence_count : 0;
    const runID = typeof response?.run_id === 'string' ? response.run_id.trim() : '';
    const jobID = typeof response?.job_id === 'string' ? response.job_id.trim() : '';
    if (traceSummary) {
        fields.push({ label: 'Trace', value: traceSummary });
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

function buildTraceDetailAction(response: any): ChatAction[] | undefined {
    const runID = typeof response?.run_id === 'string' ? response.run_id.trim() : '';
    if (!runID) return undefined;
    return [{ label: 'View trace', command: buildTraceDetailCommand(runID), style: 'default' }];
}

function finalizeRoundMessage(messages: ChatMessage[], assistantMessageId: string | null, response: any): ChatMessage[] {
    const finalizeMessage = (message: ChatMessage): ChatMessage | null => {
        const nextContent = message.content || response.text || '';
        const nextFields = mergeResponseFields(response.fields, normalizeTraceFields(response));
        const responseActions = normalizeActions(response.actions) || [];
        const traceActions = buildTraceDetailAction(response) || [];
        const nextActions = [...responseActions, ...traceActions];
        const nextLocalFilePath = response.local_file_path;
        const nextLocalFilePaths = response.local_file_paths;
        const nextThumbnailBase64 = response.thumbnail_base64;
        if (!nextContent && !nextFields?.length && !nextActions?.length && !nextThumbnailBase64 && !nextLocalFilePaths?.length && !nextLocalFilePath) {
            return null;
        }
        return {
            ...message,
            content: nextContent,
            fields: nextFields,
            actions: nextActions.length > 0 ? nextActions : undefined,
            localFilePath: nextLocalFilePath,
            localFilePaths: nextLocalFilePaths,
            thumbnailBase64: nextThumbnailBase64,
        };
    };
    return updateTailMessage(messages, assistantMessageId, finalizeMessage)
        ?? updateMessageById(messages, assistantMessageId, finalizeMessage);
}

function replaceRoundWithError(messages: ChatMessage[], assistantMessageId: string | null, errorText: string): ChatMessage[] {
    if (!assistantMessageId) {
        return [...messages, {
            id: nextId(),
            role: 'error',
            content: errorText,
            timestamp: Date.now(),
        }];
    }
    const replaceWithError = (message: ChatMessage): ChatMessage => ({
        id: message.id,
        role: 'error',
        content: errorText,
        timestamp: Date.now(),
    });
    return updateTailMessage(messages, assistantMessageId, replaceWithError)
        ?? updateMessageById(messages, assistantMessageId, replaceWithError);
}

function removeRoundMessage(messages: ChatMessage[], assistantMessageId: string | null): ChatMessage[] {
    return updateTailMessage(messages, assistantMessageId, () => null)
        ?? updateMessageById(messages, assistantMessageId, () => null);
}

function resolveSendResult(messages: ChatMessage[], assistantMessageId: string, response: any, errorText?: string): ChatMessage[] {
    return errorText
        ? replaceRoundWithError(messages, assistantMessageId, errorText)
        : response?.error
            ? replaceRoundWithError(messages, assistantMessageId, response.error)
            : finalizeRoundMessage(messages, assistantMessageId, response);
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
    const [selectedFilePath, setSelectedFilePath] = useState("");
    const [trialReflectEnabled, setTrialReflectEnabled] = useState(false);
    const [initStatus, setInitStatus] = useState<AIAssistantInitStatus>("connecting");
    const [scrollToTopSeq, setScrollToTopSeq] = useState(0);
    const [activeRound, setActiveRound] = useState<ActiveRound>(IDLE_ROUND);
    const activeRoundRef = useRef<ActiveRound>(IDLE_ROUND);
    const initStatusRef = useRef<AIAssistantInitStatus>("connecting");
    const selectedFilePathRef = useRef("");
    const latestNewsPayloadRef = useRef<string>("[]");
    const progressTailRef = useRef<string | null>(null);
    const scrollOnNextNewsRef = useRef(true);
    const refreshSessionsOnlyRef = useRef(options?.refreshSessionsOnly);

    useEffect(() => {
        refreshSessionsOnlyRef.current = options?.refreshSessionsOnly;
    }, [options?.refreshSessionsOnly]);

    useEffect(() => {
        let cancelled = false;
        GetTrialReflectEnabled()
            .then(enabled => {
                if (!cancelled) setTrialReflectEnabled(!!enabled);
            })
            .catch(() => {
                if (!cancelled) setTrialReflectEnabled(false);
            });
        return () => {
            cancelled = true;
        };
    }, []);

    const sending = activeRound.phase !== 'idle';
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

    const setSelectedFile = useCallback((nextPath: string) => {
        const normalizedNext = normalizeSelectedFilePath(nextPath);
        if (sameSelectedFilePath(selectedFilePathRef.current, normalizedNext)) {
            return selectedFilePathRef.current;
        }
        selectedFilePathRef.current = normalizedNext;
        setSelectedFilePath(current => sameSelectedFilePath(current, normalizedNext) ? current : normalizedNext);
        return normalizedNext;
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
        setMessages(prev => appendAssistantPlaceholder(prev, assistantMessageId));
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

    const sendMessage = useCallback(async (text: string) => {
        const outgoingText = buildOutgoingMessage(text, selectedFilePathRef.current);
        if (outgoingText.trim() === "" || activeRoundRef.current.phase !== 'idle') return;

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
            timestamp: Date.now(),
        };

        setRoundState({
            generation,
            phase: 'requesting',
            assistantMessageId,
            requestId,
        });
        setMessages(prev => [...prev, userMsg, placeholderMsg]);

        try {
            const response = await SendAIAssistantMessage({ text: outgoingText, request_id: requestId }) as AIAssistantSendResult;
            const responseRequestId = resolveSendRequestID(response);
            if (responseRequestId && responseRequestId !== requestId) {
                setRoundState({
                    generation,
                    phase: activeRoundRef.current.phase,
                    assistantMessageId,
                    requestId: responseRequestId,
                });
            }
            setSelectedFile("");
            setMessages(prev => resolveSendResult(prev, assistantMessageId, response));
        } catch (err: any) {
            setMessages(prev => resolveSendResult(prev, assistantMessageId, null, err?.message || String(err)));
        } finally {
            finalizeRound(generation);
        }
    }, [finalizeRound, setRoundState, setSelectedFile]);

    const sendMessageInBackground = useCallback(async (text: string) => {
        const outgoingText = buildOutgoingMessage(text, selectedFilePathRef.current);
        if (outgoingText.trim() === "" || activeRoundRef.current.phase !== 'idle') return;

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
            setSelectedFile("");
            setMessages(prev => appendBackgroundLaunchMessages(prev, outgoingText, launchResult));
            await refreshSessionsOnlyRef.current?.();
        } catch (err: any) {
            setMessages(prev => [...prev, createUserMessage(outgoingText), createErrorMessage(err?.message || String(err))]);
        } finally {
            finalizeRound(generation);
        }
    }, [finalizeRound, setRoundState, setSelectedFile]);

    const browseFile = useCallback(async () => {
        const selected = (await SelectAIAssistantFile()) || "";
        setSelectedFile(selected);
    }, [setSelectedFile]);

    const clearSelectedFile = useCallback(() => {
        setSelectedFile("");
    }, [setSelectedFile]);

    const clearHistory = useCallback(async () => {
        resetActiveRound();
        setSelectedFile("");
        if (persistTimerRef.current) {
            clearTimeout(persistTimerRef.current);
            persistTimerRef.current = null;
        }
        latestMessagesRef.current = [];
        latestPromptsRef.current = [];
        lastPersistedPayloadRef.current = null;
        lastPersistedPromptsPayloadRef.current = null;
        persistOnUnmountRef.current = false;
        localStorage.removeItem(AI_ASSISTANT_HISTORY_STORAGE_KEY);
        localStorage.removeItem(AI_ASSISTANT_PROMPT_HISTORY_STORAGE_KEY);
        setMessages([]);
        setSubmittedPrompts([]);
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
    }, [doFetchNews, resetActiveRound, setSelectedFile]);

    const recordSubmittedPrompt = useCallback((prompt: string) => {
        setSubmittedPrompts(prev => appendSubmittedPrompt(prev, prompt));
    }, []);

    const executeAction = useCallback(async (command: string) => {
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
        return sendMessage(command);
    }, [sendMessage]);

    useEffect(() => {
        const handler = (payload: unknown) => {
            const event = normalizeStreamEvent(payload);
            const progressText = event.text || (typeof payload === 'string' ? payload : '');
            if (!progressText) return;
            if (event.request_id && activeRoundRef.current.requestId && !matchesActiveRequest(activeRoundRef.current, event)) {
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

    const cancelSession = useCallback(async (): Promise<CancelAIAssistantResult> => {
        const canceledRound = activeRoundRef.current;
        const nextGeneration = canceledRound.generation + 1;
        if (!canceledRound.assistantMessageId && isRoundIdle(canceledRound)) {
            return { canceledText: "" };
        }
        if (canceledRound.assistantMessageId) {
            setMessages(prev => removeRoundMessage(prev, canceledRound.assistantMessageId));
        }
        resetActiveRound(nextGeneration);
        try {
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
    }, [resetActiveRound]);

    return { messages, submittedPrompts, draftInputValue, progressMessages, sending, streaming, visualBusy, ready, initStatus, selectedFilePath, trialReflectEnabled, browseFile, clearSelectedFile, sendMessage, sendMessageInBackground, clearHistory, recordSubmittedPrompt, setDraftInputValue, executeAction, refreshNews: doFetchNews, scrollToTopSeq, cancelSession };
}

// Polyfill for Array.findLastIndex (not available in all environments)
export function findLastIndex<T>(arr: T[], predicate: (item: T) => boolean): number {
    for (let i = arr.length - 1; i >= 0; i--) {
        if (predicate(arr[i])) return i;
    }
    return -1;
}
