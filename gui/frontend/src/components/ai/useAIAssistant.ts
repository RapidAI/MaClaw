import { useState, useEffect, useCallback, useRef } from "react";
import { SendAIAssistantMessage, ClearAIAssistantHistory, FetchNews, IsAIAssistantReady, GetAIAssistantInitStatus, CancelAIAssistantSession } from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";

export interface ChatMessage {
    id: string;
    role: 'user' | 'assistant' | 'progress' | 'error' | 'system';
    content: string;
    fields?: Array<{ label: string; value: string }>;
    actions?: Array<{ label: string; command: string; style: string }>;
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
const STORAGE_KEY = "ai-assistant-history";
const MAX_PERSISTED_MESSAGES = 200;

function loadPersistedMessages(): ChatMessage[] {
    try {
        const raw = localStorage.getItem(STORAGE_KEY);
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
        localStorage.setItem(STORAGE_KEY, JSON.stringify(toSave));
    } catch {
        // localStorage full or unavailable — silently ignore
    }
}

export function useAIAssistant() {
    const [messages, setMessages] = useState<ChatMessage[]>(loadPersistedMessages);
    const [sending, setSending] = useState(false);
    const [streaming, setStreaming] = useState(false);
    const [ready, setReady] = useState(false);
    // Human-readable init status: "connecting" | "loading" | "warming" | "ready"
    const [initStatus, setInitStatus] = useState<string>("connecting");
    // Counter that bumps whenever the panel should scroll to top (e.g. after
    // news reload on clear / restart).  The Panel watches this value.
    const [scrollToTopSeq, setScrollToTopSeq] = useState(0);
    // Ref-based guard prevents concurrent sends (React state is async).
    const sendingRef = useRef(false);
    // Track the current streaming message ID so token events know where to append.
    const streamingMsgIdRef = useRef<string | null>(null);
    // Flag: when true, the next doFetchNews completion will scroll to top.
    const scrollOnNextNewsRef = useRef(true); // true on mount (app restart)

    // Poll backend readiness until the AI assistant is initialized.
    // Also listen for init progress events from the backend.
    useEffect(() => {
        let cancelled = false;
        const check = () => {
            IsAIAssistantReady().then(ok => {
                if (cancelled) return;
                if (ok) {
                    setReady(true);
                    setInitStatus("ready");
                } else {
                    GetAIAssistantInitStatus().then(status => {
                        if (!cancelled) setInitStatus(status || "connecting");
                    }).catch(() => {});
                    setTimeout(check, 1500);
                }
            }).catch(() => {
                if (!cancelled) setTimeout(check, 1500);
            });
        };
        check();

        const progressHandler = (status: string) => {
            if (status === "ready") {
                setReady(true);
                setInitStatus("ready");
            } else {
                setInitStatus(status);
            }
        };
        EventsOn("ai-assistant-init-progress", progressHandler);

        return () => {
            cancelled = true;
            EventsOff("ai-assistant-init-progress");
        };
    }, []);

    // Persist messages to localStorage whenever they change (debounced via ref
    // to avoid thrashing during rapid streaming token events).
    const persistTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    useEffect(() => {
        if (persistTimerRef.current) clearTimeout(persistTimerRef.current);
        persistTimerRef.current = setTimeout(() => persistMessages(messages), 300);
        return () => {
            if (persistTimerRef.current) clearTimeout(persistTimerRef.current);
            // Flush on unmount so the last state is always saved
            persistMessages(messages);
        };
    }, [messages]);

    // Fetch latest news from Hub Center and prepend as system messages.
    const doFetchNews = useCallback(() => {
        FetchNews().then((articles: any[]) => {
            if (!articles || articles.length === 0) return;
            const catIcons: Record<string, string> = { notice: '📢', update: '🚀', tip: '💡', alert: '⚠️' };
            const sysMsgs: ChatMessage[] = articles.map((a: any) => ({
                id: 'news-' + a.id,
                role: 'system' as const,
                content: `${catIcons[a.category] || '📄'} **${a.title}**\n\n${a.content}`,
                timestamp: Date.now(),
            }));
            setMessages(prev => {
                const filtered = prev.filter(m => !m.id.startsWith('news-'));
                return [...sysMsgs, ...filtered];
            });
            // After news are loaded, scroll to top only when explicitly requested
            // (app restart or clear history), not on manual refresh button clicks.
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

    // Listen for streaming token events — append delta to current streaming message.
    useEffect(() => {
        const tokenHandler = (delta: string) => {
            const msgId = streamingMsgIdRef.current;
            if (!msgId) return;
            setMessages(prev => prev.map(msg =>
                msg.id === msgId
                    ? { ...msg, content: msg.content + delta }
                    : msg
            ));
        };

        // New round means a new LLM streaming call is starting.
        const newRoundHandler = () => {
            setStreaming(true);
            let reusedId: string | null = null;
            const newId = nextId();
            setMessages(prev => {
                const lastIdx = findLastIndex(prev, m => m.role === 'assistant');
                if (lastIdx >= 0 && prev[lastIdx].content === '') {
                    reusedId = prev[lastIdx].id;
                    return prev; // no state change
                }
                return [...prev, {
                    id: newId,
                    role: 'assistant' as const,
                    content: '',
                    timestamp: Date.now(),
                }];
            });
            // Update ref after updater — React batches state, but ref is sync.
            // For the reuse case reusedId is set synchronously inside the updater
            // (same tick), so this works reliably.
            streamingMsgIdRef.current = reusedId ?? newId;
        };

        const streamDoneHandler = () => {
            setStreaming(false);
        };

        EventsOn(STREAM_TOKEN_EVENT, tokenHandler);
        EventsOn(NEW_ROUND_EVENT, newRoundHandler);
        EventsOn(STREAM_DONE_EVENT, streamDoneHandler);
        return () => {
            EventsOff(STREAM_TOKEN_EVENT);
            EventsOff(NEW_ROUND_EVENT);
            EventsOff(STREAM_DONE_EVENT);
        };
    }, []);

    const sendMessage = useCallback(async (text: string) => {
        if (text.trim() === "" || sendingRef.current) return;
        sendingRef.current = true;
        setSending(true);
        setStreaming(true);

        // 1. Add user message
        const userMsg: ChatMessage = {
            id: nextId(),
            role: 'user',
            content: text,
            timestamp: Date.now(),
        };

        // 2. Add empty assistant message as streaming placeholder
        const streamId = nextId();
        streamingMsgIdRef.current = streamId;
        const placeholderMsg: ChatMessage = {
            id: streamId,
            role: 'assistant',
            content: '',
            timestamp: Date.now(),
        };

        setMessages(prev => [...prev, userMsg, placeholderMsg]);

        try {
            const response = await SendAIAssistantMessage(text);
            streamingMsgIdRef.current = null;

            if (response.error) {
                setMessages(prev => {
                    const updated = [...prev];
                    // Find the last assistant message — if it's empty, replace with error;
                    // otherwise append an error message.
                    const lastIdx = findLastIndex(updated, m => m.role === 'assistant');
                    if (lastIdx >= 0 && updated[lastIdx].content === '') {
                        updated[lastIdx] = {
                            id: updated[lastIdx].id,
                            role: 'error',
                            content: response.error,
                            timestamp: Date.now(),
                        };
                    } else {
                        updated.push({
                            id: nextId(),
                            role: 'error',
                            content: response.error,
                            timestamp: Date.now(),
                        });
                    }
                    return updated;
                });
            } else {
                // Update the last assistant message with the complete response
                // (structured fields, actions, file paths, etc.)
                setMessages(prev => {
                    const updated = [...prev];
                    const lastIdx = findLastIndex(updated, m => m.role === 'assistant');
                    if (lastIdx >= 0) {
                        const existing = updated[lastIdx];
                        updated[lastIdx] = {
                            ...existing,
                            // Keep streamed content; use final text only if streaming produced nothing
                            content: existing.content || response.text || '',
                            fields: response.fields,
                            actions: response.actions,
                            localFilePath: response.local_file_path,
                            localFilePaths: response.local_file_paths,
                            thumbnailBase64: response.thumbnail_base64,
                        };
                    }
                    return updated;
                });
            }
        } catch (err: any) {
            streamingMsgIdRef.current = null;
            const errorMsg: ChatMessage = {
                id: nextId(),
                role: 'error',
                content: err?.message || String(err),
                timestamp: Date.now(),
            };
            setMessages(prev => [...prev, errorMsg]);
        } finally {
            sendingRef.current = false;
            setSending(false);
            setStreaming(false);
            // Clean up empty assistant bubbles left over from tool-only rounds,
            // but keep the last assistant message (it may carry structured data
            // like fields/actions even with empty text content).
            setMessages(prev => {
                const lastAssistantIdx = findLastIndex(prev, m => m.role === 'assistant');
                return prev.filter((m, i) => {
                    if (m.role !== 'assistant') return true;
                    if (i === lastAssistantIdx) return true; // always keep the last one
                    return m.content !== '' || !!m.fields?.length || !!m.thumbnailBase64 || !!m.localFilePaths?.length;
                });
            });
        }
    }, []);

    const clearHistory = useCallback(async () => {
        try {
            await ClearAIAssistantHistory();
        } catch (_) {
            // ignore clear errors
        }
        // Reset sending state in case it was stuck (e.g. after max iterations).
        sendingRef.current = false;
        setSending(false);
        setStreaming(false);
        streamingMsgIdRef.current = null;
        setMessages([]);
        localStorage.removeItem(STORAGE_KEY);
        // Re-fetch pinned news so they reappear after clearing history.
        scrollOnNextNewsRef.current = true;
        doFetchNews();
    }, [doFetchNews]);

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

    const cancelSession = useCallback(async () => {
        try {
            await CancelAIAssistantSession();
        } catch (e) {
            // ignore cancellation errors
        }
        // Reset local state
        sendingRef.current = false;
        setSending(false);
        setStreaming(false);
        streamingMsgIdRef.current = null;
    }, []);

    return { messages, sending, streaming, ready, initStatus, sendMessage, clearHistory, executeAction, refreshNews: doFetchNews, scrollToTopSeq, cancelSession };
}

// Polyfill for Array.findLastIndex (not available in all environments)
export function findLastIndex<T>(arr: T[], predicate: (item: T) => boolean): number {
    for (let i = arr.length - 1; i >= 0; i--) {
        if (predicate(arr[i])) return i;
    }
    return -1;
}
