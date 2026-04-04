import { useState, useEffect, useCallback, useRef } from "react";
import { SendAIAssistantMessage, ClearAIAssistantHistory, FetchNews, IsAIAssistantReady, GetAIAssistantInitStatus, CancelAIAssistantSession, SelectAIAssistantFile } from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";

export interface CancelAIAssistantResult {
    canceledText: string;
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

export interface ChatMessage {
    id: string;
    role: 'user' | 'assistant' | 'progress' | 'error' | 'system';
    kind?: 'news';
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
const MAX_PERSISTED_MESSAGES = 200;
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

interface ActiveRound {
    generation: number;
    phase: 'idle' | 'requesting' | 'streaming';
    assistantMessageId: string | null;
}

function createIdleRound(generation: number): ActiveRound {
    return {
        generation,
        phase: 'idle',
        assistantMessageId: null,
    };
}

function isRoundIdle(round: ActiveRound): boolean {
    return round.phase === 'idle' && round.assistantMessageId === null;
}

function sameActiveRound(left: ActiveRound, right: ActiveRound): boolean {
    return left.generation === right.generation
        && left.phase === right.phase
        && left.assistantMessageId === right.assistantMessageId;
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

function hasEmptyAssistantPlaceholder(messages: ChatMessage[], assistantMessageId: string | null): boolean {
    if (!assistantMessageId) return false;
    const tail = messages[messages.length - 1];
    if (tail?.id === assistantMessageId) {
        return isAssistantPlaceholder(tail);
    }
    const index = findLastIndex(messages, message => message.id === assistantMessageId);
    return index >= 0 && isAssistantPlaceholder(messages[index]);
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

function finalizeRoundMessage(messages: ChatMessage[], assistantMessageId: string | null, response: any): ChatMessage[] {
    const finalizeMessage = (message: ChatMessage): ChatMessage => ({
        ...message,
        content: message.content || response.text || '',
        fields: response.fields,
        actions: normalizeActions(response.actions),
        localFilePath: response.local_file_path,
        localFilePaths: response.local_file_paths,
        thumbnailBase64: response.thumbnail_base64,
    });
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
    const nextMessages = errorText
        ? replaceRoundWithError(messages, assistantMessageId, errorText)
        : response?.error
            ? replaceRoundWithError(messages, assistantMessageId, response.error)
            : finalizeRoundMessage(messages, assistantMessageId, response);
    return hasEmptyAssistantPlaceholder(nextMessages, assistantMessageId)
        ? removeRoundMessage(nextMessages, assistantMessageId)
        : nextMessages;
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

export function useAIAssistant() {
    const [messages, setMessages] = useState<ChatMessage[]>(loadPersistedMessages);
    const [progressMessages, setProgressMessages] = useState<ChatMessage[]>([]);
    const [selectedFilePath, setSelectedFilePath] = useState("");
    const [initStatus, setInitStatus] = useState<AIAssistantInitStatus>("connecting");
    const [scrollToTopSeq, setScrollToTopSeq] = useState(0);
    const [activeRound, setActiveRound] = useState<ActiveRound>(IDLE_ROUND);
    const activeRoundRef = useRef<ActiveRound>(IDLE_ROUND);
    const initStatusRef = useRef<AIAssistantInitStatus>("connecting");
    const selectedFilePathRef = useRef("");
    const latestNewsPayloadRef = useRef<string>("[]");
    const progressTailRef = useRef<string | null>(null);
    const scrollOnNextNewsRef = useRef(true);

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
        EventsOn(INIT_PROGRESS_EVENT, progressHandler);

        return () => {
            cancelled = true;
            clearPollTimer();
            EventsOff(INIT_PROGRESS_EVENT);
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
    const lastPersistedPayloadRef = useRef<string | null>(null);
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
        return () => {
            if (persistTimerRef.current) {
                clearTimeout(persistTimerRef.current);
                persistTimerRef.current = null;
            }
        };
    }, [messages]);
    useEffect(() => {
        lastPersistedPayloadRef.current = serializePersistedMessages(latestMessagesRef.current);
        return () => {
            if (persistTimerRef.current) {
                clearTimeout(persistTimerRef.current);
                persistTimerRef.current = null;
            }
            persistMessages(latestMessagesRef.current);
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
        const tokenHandler = (delta: string) => {
            const { generation, assistantMessageId } = activeRoundRef.current;
            if (!assistantMessageId || generation === 0) return;
            setMessages(prev => appendTokenToRound(prev, assistantMessageId, delta));
        };

        const newRoundHandler = () => {
            const { generation } = activeRoundRef.current;
            if (!generation) return;
            ensureRoundPlaceholder(generation);
        };

        const streamDoneHandler = () => {
            transitionRound(current => {
                if (current.phase !== 'streaming') return current;
                return { ...current, phase: 'requesting' };
            });
        };

        EventsOn(STREAM_TOKEN_EVENT, tokenHandler);
        EventsOn(NEW_ROUND_EVENT, newRoundHandler);
        EventsOn(STREAM_DONE_EVENT, streamDoneHandler);
        return () => {
            EventsOff(STREAM_TOKEN_EVENT);
            EventsOff(NEW_ROUND_EVENT);
            EventsOff(STREAM_DONE_EVENT);
        };
    }, [ensureRoundPlaceholder, transitionRound]);

    const sendMessage = useCallback(async (text: string) => {
        const outgoingText = buildOutgoingMessage(text, selectedFilePathRef.current);
        if (outgoingText.trim() === "" || activeRoundRef.current.phase !== 'idle') return;

        const generation = activeRoundRef.current.generation + 1;
        const assistantMessageId = nextId();
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
        });
        setMessages(prev => [...prev, userMsg, placeholderMsg]);

        try {
            const response = await SendAIAssistantMessage(outgoingText);
            setSelectedFile("");
            setMessages(prev => resolveSendResult(prev, assistantMessageId, response));
        } catch (err: any) {
            setMessages(prev => resolveSendResult(prev, assistantMessageId, null, err?.message || String(err)));
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
        persistMessages([]);
        setMessages([]);
        progressTailRef.current = null;
        setProgressMessages([]);
        latestNewsPayloadRef.current = '[]';
        scrollOnNextNewsRef.current = true;
        doFetchNews();
        try {
            await ClearAIAssistantHistory();
        } catch (_) {
        }
    }, [doFetchNews, resetActiveRound, setSelectedFile]);

    const executeAction = useCallback((command: string) => {
        return sendMessage(command);
    }, [sendMessage]);

    useEffect(() => {
        const handler = (progressText: string) => {
            if (progressTailRef.current === progressText) {
                return;
            }
            progressTailRef.current = progressText;
            setProgressMessages(prev => appendProgressText(prev, progressText));
        };
        EventsOn(PROGRESS_EVENT, handler);
        return () => {
            EventsOff(PROGRESS_EVENT);
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
            return {
                canceledText: (await CancelAIAssistantSession()) || "",
            };
        } catch {
            return { canceledText: "" };
        }
    }, [resetActiveRound]);

    return { messages, progressMessages, sending, streaming, visualBusy, ready, initStatus, selectedFilePath, browseFile, clearSelectedFile, sendMessage, clearHistory, executeAction, refreshNews: doFetchNews, scrollToTopSeq, cancelSession };
}

// Polyfill for Array.findLastIndex (not available in all environments)
export function findLastIndex<T>(arr: T[], predicate: (item: T) => boolean): number {
    for (let i = arr.length - 1; i >= 0; i--) {
        if (predicate(arr[i])) return i;
    }
    return -1;
}
