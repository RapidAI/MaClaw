import { useState, useEffect, useCallback, useRef } from "react";
import { SendAIAssistantMessage, ClearAIAssistantHistory } from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";

export interface ChatMessage {
    id: string;
    role: 'user' | 'assistant' | 'progress' | 'error';
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

export function useAIAssistant() {
    const [messages, setMessages] = useState<ChatMessage[]>([]);
    const [sending, setSending] = useState(false);
    // Ref-based guard prevents concurrent sends (React state is async).
    const sendingRef = useRef(false);
    // Track the current streaming message ID so token events know where to append.
    const streamingMsgIdRef = useRef<string | null>(null);

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

        // New round: agent loop started a new LLM call — create a new assistant bubble.
        const newRoundHandler = () => {
            const newId = nextId();
            streamingMsgIdRef.current = newId;
            const newMsg: ChatMessage = {
                id: newId,
                role: 'assistant',
                content: '',
                timestamp: Date.now(),
            };
            setMessages(prev => [...prev, newMsg]);
        };

        EventsOn(STREAM_TOKEN_EVENT, tokenHandler);
        EventsOn(NEW_ROUND_EVENT, newRoundHandler);
        return () => {
            EventsOff(STREAM_TOKEN_EVENT);
            EventsOff(NEW_ROUND_EVENT);
        };
    }, []);

    const sendMessage = useCallback(async (text: string) => {
        if (text.trim() === "" || sendingRef.current) return;
        sendingRef.current = true;
        setSending(true);

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
        streamingMsgIdRef.current = null;
        setMessages([]);
    }, []);

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

    return { messages, sending, sendMessage, clearHistory, executeAction };
}

// Polyfill for Array.findLastIndex (not available in all environments)
function findLastIndex<T>(arr: T[], predicate: (item: T) => boolean): number {
    for (let i = arr.length - 1; i >= 0; i--) {
        if (predicate(arr[i])) return i;
    }
    return -1;
}
