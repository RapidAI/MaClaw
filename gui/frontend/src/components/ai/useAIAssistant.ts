import { useState, useEffect, useCallback, useRef } from "react";
import { SendAIAssistantMessage, ClearAIAssistantHistory, FetchNews, IsAIAssistantReady, GetAIAssistantInitStatus, CancelAIAssistantSession, SelectAIAssistantFile } from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";

export interface CancelAIAssistantResult {
    canceledText: string;
}

export interface NewsCardData {
    articleId: string;
    category: string;
    title: string;
    body: string;
    icon: string;
}

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

// ---------------------------------------------------------------------------
// localStorage persistence for chat history across app restarts
// ---------------------------------------------------------------------------
export const AI_ASSISTANT_HISTORY_STORAGE_KEY = "ai-assistant-history";
const MAX_PERSISTED_MESSAGES = 200;
const FILE_PATH_PROMPT_PREFIX = "[用户选择的本地文件路径]";
const IMAGE_FILE_EXTENSIONS = new Set([".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".svg", ".ico", ".tif", ".tiff"]);

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

function persistMessages(msgs: ChatMessage[]) {
    try {
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
        if (toSave.length === 0) {
            localStorage.removeItem(AI_ASSISTANT_HISTORY_STORAGE_KEY);
            return;
        }
        localStorage.setItem(AI_ASSISTANT_HISTORY_STORAGE_KEY, JSON.stringify(toSave));
    } catch {
        // localStorage full or unavailable — silently ignore
    }
}

interface ActiveRound {
    generation: number;
    phase: 'idle' | 'requesting' | 'streaming';
    assistantMessageId: string | null;
}

const IDLE_ROUND: ActiveRound = {
    generation: 0,
    phase: 'idle',
    assistantMessageId: null,
};

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

function updateMessageById(messages: ChatMessage[], messageId: string | null, updater: (message: ChatMessage) => ChatMessage | null): ChatMessage[] {
    if (!messageId) return messages;
    const index = messages.findIndex(msg => msg.id === messageId);
    if (index < 0) return messages;
    const updated = updater(messages[index]);
    if (updated === messages[index]) return messages;
    if (updated === null) {
        return messages.filter((_, i) => i !== index);
    }
    const next = [...messages];
    next[index] = updated;
    return next;
}

function appendTokenContent(message: ChatMessage, delta: string): ChatMessage {
    if (isAssistantPlaceholder(message) && message.content === '') {
        return {
            ...message,
            content: delta,
        };
    }
    return {
        ...message,
        content: message.content + delta,
    };
}

function appendTokenToRound(messages: ChatMessage[], assistantMessageId: string | null, delta: string): ChatMessage[] {
    if (!assistantMessageId || messages.length === 0) return messages;
    const lastIndex = messages.length - 1;
    if (messages[lastIndex].id === assistantMessageId) {
        const updated = appendTokenContent(messages[lastIndex], delta);
        if (updated === messages[lastIndex]) return messages;
        const next = [...messages];
        next[lastIndex] = updated;
        return next;
    }
    return updateMessageById(messages, assistantMessageId, message => appendTokenContent(message, delta));
}

function finalizeRoundMessage(messages: ChatMessage[], assistantMessageId: string | null, response: any): ChatMessage[] {
    return updateMessageById(messages, assistantMessageId, message => ({
        ...message,
        content: message.content || response.text || '',
        fields: response.fields,
        actions: normalizeActions(response.actions),
        localFilePath: response.local_file_path,
        localFilePaths: response.local_file_paths,
        thumbnailBase64: response.thumbnail_base64,
    }));
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
    return updateMessageById(messages, assistantMessageId, message => ({
        id: message.id,
        role: 'error',
        content: errorText,
        timestamp: Date.now(),
    }));
}

function removeRoundMessage(messages: ChatMessage[], assistantMessageId: string | null): ChatMessage[] {
    return updateMessageById(messages, assistantMessageId, () => null);
}

function normalizeActionStyle(style: unknown): ChatActionStyle {
    return style === 'danger' ? 'danger' : 'default';
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

function createNewsMessage(article: any): ChatMessage {
    const iconByCategory: Record<string, string> = { notice: '📢', update: '🚀', tip: '💡', alert: '⚠️' };
    const category = typeof article?.category === 'string' ? article.category : '';
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
            icon: iconByCategory[category] || '📄',
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
    const [ready, setReady] = useState(false);
    const [selectedFilePath, setSelectedFilePath] = useState("");
    const [initStatus, setInitStatus] = useState<string>("connecting");
    const [scrollToTopSeq, setScrollToTopSeq] = useState(0);
    const [activeRound, setActiveRound] = useState<ActiveRound>(IDLE_ROUND);
    const activeRoundRef = useRef<ActiveRound>(IDLE_ROUND);
    const scrollOnNextNewsRef = useRef(true);

    useEffect(() => {
        activeRoundRef.current = activeRound;
    }, [activeRound]);

    const sending = activeRound.phase !== 'idle';
    const streaming = activeRound.phase === 'streaming';

    useEffect(() => {
        let cancelled = false;
        let ready = false;
        let timer: ReturnType<typeof setTimeout> | null = null;
        const scheduleCheck = () => {
            if (cancelled || ready || timer) return;
            timer = setTimeout(() => {
                timer = null;
                check();
            }, 1500);
        };
        const check = () => {
            IsAIAssistantReady().then(ok => {
                if (cancelled || ready) return;
                if (ok) {
                    ready = true;
                    setReady(true);
                    setInitStatus("ready");
                } else {
                    GetAIAssistantInitStatus().then(status => {
                        if (!cancelled && !ready) setInitStatus(status || "connecting");
                    }).catch(() => {});
                    scheduleCheck();
                }
            }).catch(() => {
                if (!cancelled && !ready) scheduleCheck();
            });
        };
        check();

        const progressHandler = (status: string) => {
            if (status === "ready") {
                ready = true;
                if (timer) {
                    clearTimeout(timer);
                    timer = null;
                }
                setReady(true);
                setInitStatus("ready");
            } else {
                setInitStatus(status);
            }
        };
        EventsOn("ai-assistant-init-progress", progressHandler);

        return () => {
            cancelled = true;
            if (timer) {
                clearTimeout(timer);
                timer = null;
            }
            EventsOff("ai-assistant-init-progress");
        };
    }, []);

    const transitionRound = useCallback((updater: (current: ActiveRound) => ActiveRound) => {
        const next = updater(activeRoundRef.current);
        activeRoundRef.current = next;
        setActiveRound(next);
        return next;
    }, []);

    const resetActiveRound = useCallback(() => {
        activeRoundRef.current = IDLE_ROUND;
        setActiveRound(IDLE_ROUND);
    }, []);

    const ensureRoundPlaceholder = useCallback((generation: number) => {
        const current = activeRoundRef.current;
        if (current.generation !== generation) {
            return null;
        }
        const assistantMessageId = current.assistantMessageId || nextId();
        const nextRound: ActiveRound = {
            generation,
            phase: 'streaming',
            assistantMessageId,
        };
        activeRoundRef.current = nextRound;
        setActiveRound(nextRound);
        setMessages(prev => appendAssistantPlaceholder(prev, assistantMessageId));
        return assistantMessageId;
    }, []);

    const finalizeRound = useCallback((generation: number) => {
        if (activeRoundRef.current.generation !== generation) return;
        resetActiveRound();
    }, [resetActiveRound]);

    const persistTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const latestMessagesRef = useRef(messages);
    useEffect(() => {
        latestMessagesRef.current = messages;
        if (persistTimerRef.current) clearTimeout(persistTimerRef.current);
        persistTimerRef.current = setTimeout(() => {
            persistTimerRef.current = null;
            persistMessages(latestMessagesRef.current);
        }, 300);
    }, [messages]);
    useEffect(() => {
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
            if (!articles || articles.length === 0) return;
            const newsMessages = articles.map(createNewsMessage);
            setMessages(prev => {
                const existingNews = prev.filter(isPinnedNewsMessage);
                if (existingNews.length === newsMessages.length && existingNews.every((msg, idx) => samePinnedNews(msg, newsMessages[idx]))) {
                    return prev;
                }
                const filtered = prev.filter(m => !isPinnedNewsMessage(m));
                return [...newsMessages, ...filtered];
            });
            if (scrollOnNextNewsRef.current) {
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
        const outgoingText = buildOutgoingMessage(text, selectedFilePath);
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

        activeRoundRef.current = {
            generation,
            phase: 'requesting',
            assistantMessageId,
        };
        setActiveRound(activeRoundRef.current);
        setMessages(prev => [...prev, userMsg, placeholderMsg]);

        try {
            const response = await SendAIAssistantMessage(outgoingText);
            setSelectedFilePath("");

            if (response.error) {
                setMessages(prev => replaceRoundWithError(prev, assistantMessageId, response.error));
            } else {
                setMessages(prev => finalizeRoundMessage(prev, assistantMessageId, response));
            }
        } catch (err: any) {
            setMessages(prev => replaceRoundWithError(prev, assistantMessageId, err?.message || String(err)));
        } finally {
            finalizeRound(generation);
        }
    }, [finalizeRound, selectedFilePath]);

    const browseFile = useCallback(async () => {
        const selected = (await SelectAIAssistantFile()) || "";
        if (selected.trim()) {
            setSelectedFilePath(selected);
        }
    }, []);

    const clearSelectedFile = useCallback(() => {
        setSelectedFilePath("");
    }, []);

    const clearHistory = useCallback(async () => {
        try {
            await ClearAIAssistantHistory();
        } catch (_) {
        }
        resetActiveRound();
        setSelectedFilePath("");
        if (persistTimerRef.current) {
            clearTimeout(persistTimerRef.current);
            persistTimerRef.current = null;
        }
        persistMessages([]);
        setMessages([]);
        scrollOnNextNewsRef.current = true;
        doFetchNews();
    }, [doFetchNews, resetActiveRound]);

    const executeAction = useCallback((command: string) => {
        return sendMessage(command);
    }, [sendMessage]);

    useEffect(() => {
        const handler = (progressText: string) => {
            const progressMsg: ChatMessage = {
                id: nextId(),
                role: 'progress',
                content: progressText,
                timestamp: Date.now(),
            };
            setMessages(prev => [...prev, progressMsg]);
        };
        EventsOn("ai-assistant-progress", handler);
        return () => {
            EventsOff("ai-assistant-progress");
        };
    }, []);

    const cancelSession = useCallback(async (): Promise<CancelAIAssistantResult> => {
        const canceledRound = activeRoundRef.current;
        const nextGeneration = canceledRound.generation + 1;
        let canceledText = "";
        try {
            canceledText = (await CancelAIAssistantSession()) || "";
        } catch (e) {
        }
        if (canceledRound.assistantMessageId) {
            setMessages(prev => removeRoundMessage(prev, canceledRound.assistantMessageId));
        }
        activeRoundRef.current = {
            generation: nextGeneration,
            phase: 'idle',
            assistantMessageId: null,
        };
        setActiveRound(activeRoundRef.current);
        return { canceledText };
    }, []);

    return { messages, sending, streaming, ready, initStatus, selectedFilePath, browseFile, clearSelectedFile, sendMessage, clearHistory, executeAction, refreshNews: doFetchNews, scrollToTopSeq, cancelSession };
}

// Polyfill for Array.findLastIndex (not available in all environments)
export function findLastIndex<T>(arr: T[], predicate: (item: T) => boolean): number {
    for (let i = arr.length - 1; i >= 0; i--) {
        if (predicate(arr[i])) return i;
    }
    return -1;
}
