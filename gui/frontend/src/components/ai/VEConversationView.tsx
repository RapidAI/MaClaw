import { forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState } from "react";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import { MessageContentRenderer } from "./MessageContentRenderer";
import type { Theme } from "./aiAssistantPanelTheme";
import { MentionPopover, useMentionKeyboard, type MentionParticipant } from "./MentionPopover";
import { getParticipantColor } from "./VEGroupChat";

// --- Types ---

export interface VEMessage {
    id: string;
    role: "user" | "assistant";
    content: string;
    timestamp: number;
    fromId?: string;
    fromName?: string;
    /** Marks a message as failed to send */
    sendFailed?: boolean;
    /** Attachment metadata for display */
    attachments?: VEMessageAttachment[];
}

export interface VEMessageAttachment {
    type: "text" | "image" | "file";
    filename: string;
    mimeType?: string;
    /** For images: URL to display */
    fileUrl?: string;
    /** For files: size in bytes */
    sizeBytes?: number;
}

type PendingVEAttachment = {
    name: string;
    size: number;
    path?: string;
};

type QueuedVEMessage = {
    id: string;
    content: string;
    filePaths?: string[];
};

export interface VEConversationState {
    sessionId: string | null;
    messages: VEMessage[];
    streaming: boolean;
    streamContent: string;
    streamFromId: string;
    streamFromName: string;
    error: VEConversationError | null;
    connectionState: "connected" | "disconnected" | "reconnecting";
    reconnectAttempt: number;
}

export type VEConversationError =
    | { type: "hub_disconnected"; message: string }
    | { type: "ve_offline"; message: string }
    | { type: "send_failed"; message: string }
    | { type: "session_timeout"; message: string };

export interface VEConversationViewProps {
    veId: string;
    veName: string;
    theme: Theme;
    lang?: string;
    /** Initial online status of the VE. Defaults to true (optimistic). Updated via ve:status_change events. */
    initialOnlineStatus?: "online" | "offline";
    /** Pre-existing session ID to resume (sticky session). Skips initiation if provided. */
    existingSessionId?: string;
    /** Pre-existing messages to restore on remount (tab switch). */
    initialMessages?: VEMessage[];
    /** Pre-existing input text to restore on remount (tab switch). */
    initialInputText?: string;
    /** View-only mode: history/invited sessions can render messages without allowing edits. */
    readOnly?: boolean;
    /** Override for testing Wails bindings. */
    initiateConversation?: (veId: string) => Promise<{ session_id: string; ve_id: string; ve_name: string }>;
    sendMessage?: (sessionId: string, content: string) => Promise<void>;
    sendGroupMessage?: (sessionId: string, content: string, mentionedIds: string[]) => Promise<void>;
    sendMessageWithAttachments?: (sessionId: string, content: string, filePaths: string[]) => Promise<void>;
    closeSession?: (sessionId: string) => Promise<void>;
    /** Group chat participants for @mention (empty array = no mention support) */
    participants?: MentionParticipant[];
    /** External @mention insert trigger (from right-click "Talk to" in participant panel) */
    externalMentionInsert?: { name: string; timestamp: number } | null;
    /** Notifies the parent as soon as a backend session is known. */
    onSessionIdChange?: (sessionId: string) => void;
}

/**
 * Imperative handle exposed by VEConversationView via useImperativeHandle.
 * Used by the parent to snapshot conversation state on tab switch (unmount).
 */
export interface VEConversationHandle {
    getState: () => { messages: VEMessage[]; sessionId: string | null; inputText: string };
}

// --- Constants ---

const SESSION_TIMEOUT_MS = 5000;
const RECONNECT_DELAYS = [2000, 4000, 8000, 16000, 30000]; // exponential backoff
const MAX_RECONNECT_RETRIES = 5;
const MENTION_TRIGGER_PATTERN = /(^|[^A-Za-z0-9_.-])@([^\s@]*)$/;

// --- Helpers ---

/** Generate a unique message ID */
function generateMsgId(): string {
    return `msg-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

/** Create a session with timeout */
function createSessionWithTimeout<T>(
    promise: Promise<T>,
    timeoutMs: number
): Promise<T> {
    return new Promise<T>((resolve, reject) => {
        const timeout = setTimeout(() => reject(new Error("session_timeout")), timeoutMs);
        promise.then(
            (value) => {
                clearTimeout(timeout);
                resolve(value);
            },
            (error) => {
                clearTimeout(timeout);
                reject(error);
            }
        );
    });
}

function participantNameById(participants: MentionParticipant[] | undefined, id?: string): string {
    const normalized = String(id || "").trim();
    if (!normalized || !participants?.length) return "";
    return participants.find((p) => p.id === normalized)?.name || "";
}

function looksLikeRawParticipantId(value: string): boolean {
    return /^(m_[A-Za-z0-9]+|machine[-_][A-Za-z0-9-]+|ve[-_][A-Za-z0-9-]+|profile[-_][A-Za-z0-9-]+|disc[-_][A-Za-z0-9-]+|discussion[-_][A-Za-z0-9-]+|consultation[-_][A-Za-z0-9-]+|session[-_][A-Za-z0-9-]+|[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})$/i.test(value);
}

function readableSpeakerName(
    candidate: string | undefined,
    fromId: string | undefined,
    participants: MentionParticipant[] | undefined,
    fallback: string,
): string {
    const name = String(candidate || "").trim();
    const id = String(fromId || "").trim();
    const participantName = participantNameById(participants, id);
    if (name && name !== id && !looksLikeRawParticipantId(name)) return name;
    return participantName || fallback;
}

function readableConversationPartnerName(name: string, id: string, isZh: boolean): string {
    const candidate = String(name || "").trim();
    const normalizedId = String(id || "").trim();
    if (candidate && candidate !== normalizedId && !looksLikeRawParticipantId(candidate)) return candidate;
    return isZh ? "数字员工" : "Digital employee";
}

function participantColorById(participants: MentionParticipant[] | undefined, id: string | undefined, fallback: string): string {
    const normalized = String(id || "").trim();
    const index = participants?.findIndex((p) => p.id === normalized) ?? -1;
    return index >= 0 ? getParticipantColor(index) : fallback;
}

function escapeRegExp(value: string): string {
    return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function hasMention(content: string, label: string): boolean {
    const escaped = escapeRegExp(label);
    return new RegExp(`(^|[^A-Za-z0-9_.-])@${escaped}(?=$|[^A-Za-z0-9_.-])`).test(content);
}

function mentionedParticipantIds(content: string, participants: MentionParticipant[] | undefined): string[] {
    if (!participants?.length) return [];
    const mentioned = new Set<string>();
    for (const participant of participants) {
        const name = String(participant.name || "").trim();
        if (name && hasMention(content, name)) {
            mentioned.add(participant.id);
        }
    }
    return [...mentioned];
}

// --- Component ---

export const VEConversationView = forwardRef<VEConversationHandle, VEConversationViewProps>(function VEConversationView({
    veId,
    veName,
    theme,
    lang,
    initialOnlineStatus,
    existingSessionId,
    initialMessages,
    initialInputText,
    readOnly = false,
    initiateConversation,
    sendMessage,
    sendGroupMessage,
    sendMessageWithAttachments,
    closeSession,
    participants,
    externalMentionInsert,
    onSessionIdChange,
}, ref) {
    const [state, setState] = useState<VEConversationState>({
        sessionId: existingSessionId || null,
        messages: initialMessages || [],
        streaming: false,
        streamContent: "",
        streamFromId: "",
        streamFromName: "",
        error: null,
        connectionState: existingSessionId ? "connected" : "connected",
        reconnectAttempt: 0,
    });
    const [inputText, setInputText] = useState(initialInputText || "");
    const [mentionOpen, setMentionOpen] = useState(false);
    const [mentionQuery, setMentionQuery] = useState("");
    const [mentionStart, setMentionStart] = useState(-1);
    const [mentionSelectedIndex, setMentionSelectedIndex] = useState(0);
    const [sending, setSending] = useState(false);
    const [pendingAttachments, setPendingAttachments] = useState<PendingVEAttachment[]>([]);
    // Track VE online status — input is disabled when offline.
    const [veOnline, setVeOnline] = useState(initialOnlineStatus !== "offline");

    // Refs for imperative state access (avoids stale closure in useImperativeHandle)
    const stateRef = useRef(state);
    stateRef.current = state;
    const inputTextRef = useRef(inputText);
    inputTextRef.current = inputText;

    // Expose getState() to parent via ref — parent calls this before unmount to snapshot state
    useImperativeHandle(ref, () => ({
        getState: () => ({
            messages: stateRef.current.messages,
            sessionId: stateRef.current.sessionId,
            inputText: inputTextRef.current,
        }),
    }), []);

    const mountedRef = useRef(true);
    const messagesEndRef = useRef<HTMLDivElement>(null);
    const inputRef = useRef<HTMLTextAreaElement | null>(null);
    const focusTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const queuedMessagesRef = useRef<QueuedVEMessage[]>([]);
    const sessionIdRef = useRef<string | null>(existingSessionId || null);
    const reconnectAttemptRef = useRef(0);
    const sendingRef = useRef(false);

    sendingRef.current = sending;

    const isZh = !lang || lang.startsWith("zh");
    const localSpeakerName = isZh ? "我" : "Me";
    const assistantDisplayName = useMemo(() => readableConversationPartnerName(veName, veId, isZh), [isZh, veId, veName]);

    const closeMentionPopover = useCallback(() => {
        setMentionOpen(false);
        setMentionQuery("");
        setMentionStart(-1);
        setMentionSelectedIndex(0);
    }, []);

    const scheduleInputFocus = useCallback((position?: number) => {
        if (focusTimerRef.current) clearTimeout(focusTimerRef.current);
        focusTimerRef.current = setTimeout(() => {
            focusTimerRef.current = null;
            const target = position ?? inputRef.current?.value.length ?? 0;
            inputRef.current?.focus();
            inputRef.current?.setSelectionRange(target, target);
        }, 0);
    }, []);

    const updateMentionState = useCallback((value: string, caret: number | null | undefined) => {
        if (readOnly || !participants?.length || caret == null) {
            closeMentionPopover();
            return;
        }
        const beforeCaret = value.slice(0, caret);
        const match = beforeCaret.match(MENTION_TRIGGER_PATTERN);
        if (!match) {
            closeMentionPopover();
            return;
        }
        const query = match[2] || "";
        const normalizedQuery = query.trim().toLowerCase();
        const hasMatches = !normalizedQuery || participants.some((p) =>
            p.name.toLowerCase().includes(normalizedQuery)
        );
        if (!hasMatches) {
            closeMentionPopover();
            return;
        }
        const atIndex = beforeCaret.length - query.length - 1;
        setMentionStart(atIndex);
        setMentionQuery(query);
        setMentionSelectedIndex(0);
        setMentionOpen(true);
    }, [closeMentionPopover, participants, readOnly]);

    const mentionFiltered = useMemo(() => {
        if (!participants?.length) return [];
        const query = mentionQuery.trim().toLowerCase();
        if (!query) return participants;
        return participants.filter((p) =>
            p.name.toLowerCase().includes(query)
        );
    }, [mentionQuery, participants]);

    const insertMentionParticipant = useCallback((participant: MentionParticipant) => {
        if (readOnly) return;
        const textarea = inputRef.current;
        const caret = textarea?.selectionStart ?? inputText.length;
        const start = mentionStart >= 0 ? mentionStart : caret;
        const mention = `@${participant.name} `;
        const next = `${inputText.slice(0, start)}${mention}${inputText.slice(caret)}`;
        const nextCaret = start + mention.length;
        setInputText(next);
        closeMentionPopover();
        scheduleInputFocus(nextCaret);
    }, [closeMentionPopover, inputText, mentionStart, readOnly, scheduleInputFocus]);

    const mentionKeyDown = useMentionKeyboard(mentionOpen, mentionFiltered, mentionSelectedIndex, setMentionSelectedIndex, insertMentionParticipant, closeMentionPopover);

    useEffect(() => {
        if (readOnly) closeMentionPopover();
    }, [closeMentionPopover, readOnly]);

    useEffect(() => {
        if (readOnly || !externalMentionInsert?.name) return;
        const mention = `@${externalMentionInsert.name} `;
        const textarea = inputRef.current;
        closeMentionPopover();
        setInputText((prev) => {
            const caret = textarea?.selectionStart ?? prev.length;
            const prefix = prev.slice(0, caret);
            const suffix = prev.slice(caret).replace(/^\s+/, "");
            const spacer = prefix && !prefix.endsWith(" ") ? " " : "";
            const next = `${prefix}${spacer}${mention}${suffix}`;
            scheduleInputFocus(prefix.length + spacer.length + mention.length);
            return next;
        });
    }, [closeMentionPopover, externalMentionInsert, readOnly, scheduleInputFocus]);

    // Keep sessionIdRef in sync
    useEffect(() => {
        sessionIdRef.current = state.sessionId;
        if (state.sessionId) onSessionIdChange?.(state.sessionId);
    }, [onSessionIdChange, state.sessionId]);


    // Keep reconnectAttemptRef in sync with state resets (e.g. successful session init)
    useEffect(() => {
        reconnectAttemptRef.current = state.reconnectAttempt;
    }, [state.reconnectAttempt]);

    // Scroll to bottom on new messages
    useEffect(() => {
        messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
    }, [state.messages, state.streamContent]);

    // Track VE online/offline status via events
    useEffect(() => {
        const unsub = EventsOn("ve:status_change", (data: any) => {
            if (!data) return;
            const id = data.ve_id || data.id;
            if (id !== veId) return;
            const status = data.online_status;
            if (status === "online") setVeOnline(true);
            else if (status === "offline") setVeOnline(false);
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("ve:status_change");
        };
    }, [veId]);

    // --- Session Management ---

    const initSession = useCallback(async () => {
        if (!initiateConversation) {
            try {
                const mod = await import("../../../wailsjs/go/main/App");
                const result = await createSessionWithTimeout<{ session_id: string; ve_id: string; ve_name: string }>(
                    (mod as any).InitiateVEConversation(veId),
                    SESSION_TIMEOUT_MS
                );
                if (mountedRef.current) {
                    setState((prev) => ({
                        ...prev,
                        sessionId: result.session_id,
                        error: null,
                        connectionState: "connected",
                        reconnectAttempt: 0,
                    }));
                }
                return true;
            } catch (err: any) {
                if (mountedRef.current) {
                    const errorType = err?.message?.includes("session_timeout")
                        ? "session_timeout"
                        : err?.message?.includes("offline")
                        ? "ve_offline"
                        : "hub_disconnected";
                    setState((prev) => ({
                        ...prev,
                        error: {
                            type: errorType,
                            message: err?.message || "Connection failed",
                        } as VEConversationError,
                    }));
                }
            }
            return false;
        }

        try {
            const result = await createSessionWithTimeout(
                initiateConversation(veId),
                SESSION_TIMEOUT_MS
            );
            if (mountedRef.current) {
                setState((prev) => ({
                    ...prev,
                    sessionId: result.session_id,
                    error: null,
                    connectionState: "connected",
                    reconnectAttempt: 0,
                }));
            }
            return true;
        } catch (err: any) {
            if (mountedRef.current) {
                const errorType = err?.message?.includes("session_timeout")
                    ? "session_timeout"
                    : err?.message?.includes("offline")
                    ? "ve_offline"
                    : "hub_disconnected";
                setState((prev) => ({
                    ...prev,
                    error: {
                        type: errorType,
                        message: err?.message || "Connection failed",
                    } as VEConversationError,
                }));
            }
            return false;
        }
    }, [veId, initiateConversation]);

    // Initialize session on mount (skip if resuming an existing sticky session or VE is offline)
    useEffect(() => {
        mountedRef.current = true;
        if (!existingSessionId && initialOnlineStatus !== "offline") {
            initSession();
        }
        return () => {
            mountedRef.current = false;
            if (reconnectTimerRef.current) {
                clearTimeout(reconnectTimerRef.current);
            }
            if (focusTimerRef.current) {
                clearTimeout(focusTimerRef.current);
            }
        };
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    // When VE comes online and we don't have a session yet, initiate one automatically.
    // Skip the initial render — mount effect handles the online-at-mount case.
    const didMountForAutoConnect = useRef(false);
    useEffect(() => {
        if (!didMountForAutoConnect.current) {
            didMountForAutoConnect.current = true;
            return;
        }
        if (veOnline && !sessionIdRef.current) {
            initSession();
        }
    }, [veOnline, initSession]);

    // --- Reconnection Logic ---

    const attemptReconnect = useCallback(() => {
        if (reconnectTimerRef.current) return;
        const currentAttempt = reconnectAttemptRef.current;

        if (currentAttempt >= MAX_RECONNECT_RETRIES) {
            setState((prev) => ({
                ...prev,
                connectionState: "disconnected",
                error: {
                    type: "hub_disconnected",
                    message: isZh
                        ? "\u91cd\u8fde\u5931\u8d25\uff0c\u5df2\u8fbe\u6700\u5927\u91cd\u8bd5\u6b21\u6570"
                        : "Reconnection failed after max retries",
                },
            }));
            return;
        }

        const nextAttempt = currentAttempt + 1;
        reconnectAttemptRef.current = nextAttempt;

        setState((prev) => ({
            ...prev,
            connectionState: "reconnecting",
            reconnectAttempt: nextAttempt,
        }));

        const delay = RECONNECT_DELAYS[Math.min(currentAttempt, RECONNECT_DELAYS.length - 1)];
        reconnectTimerRef.current = setTimeout(async () => {
            reconnectTimerRef.current = null;
            const connected = await initSession();
            if (!connected && mountedRef.current) {
                attemptReconnect();
            }
        }, delay);
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [initSession, isZh]);

    // --- Streaming Event Listeners ---

    useEffect(() => {
        const handleStreamChunk = (data: any) => {
            const sessionId = data?.session_id || data?.sessionId;
            const content = data?.content || data?.chunk || "";
            if (sessionId && sessionId !== sessionIdRef.current) return;
            if (!mountedRef.current) return;
            const senderId = String(data?.from_id || data?.fromId || data?.sender_id || data?.senderId || "");
            const senderName = String(data?.from_name || data?.fromName || data?.sender_name || data?.senderName || "");
            setState((prev) => {
                if (prev.streaming && prev.streamContent && senderId && prev.streamFromId && senderId !== prev.streamFromId) {
                    const completedMsg: VEMessage = {
                        id: generateMsgId(),
                        role: "assistant",
                        content: prev.streamContent,
                        timestamp: Date.now(),
                        fromId: prev.streamFromId,
                        fromName: prev.streamFromName || undefined,
                    };
                    return {
                        ...prev,
                        streaming: true,
                        streamContent: content,
                        streamFromId: senderId,
                        streamFromName: senderName,
                        messages: [...prev.messages, completedMsg],
                    };
                }
                return {
                    ...prev,
                    streaming: true,
                    streamContent: prev.streamContent + content,
                    streamFromId: senderId || prev.streamFromId,
                    streamFromName: senderName || prev.streamFromName,
                };
            });
        };

        const handleStreamEnd = (data: any) => {
            const sessionId = data?.session_id || data?.sessionId;
            if (sessionId && sessionId !== sessionIdRef.current) return;
            if (!mountedRef.current) return;
            setState((prev) => {
                const finalContent = prev.streamContent;
                if (!finalContent) return { ...prev, streaming: false, streamContent: "", streamFromId: "", streamFromName: "" };
                const newMsg: VEMessage = {
                    id: generateMsgId(),
                    role: "assistant",
                    content: finalContent,
                    timestamp: Date.now(),
                    fromId: prev.streamFromId || undefined,
                    fromName: prev.streamFromName || undefined,
                };
                return {
                    ...prev,
                    streaming: false,
                    streamContent: "",
                    streamFromId: "",
                    streamFromName: "",
                    messages: [...prev.messages, newMsg],
                };
            });
        };

        const handleDisconnect = (data: any) => {
            const sessionId = data?.session_id || data?.sessionId;
            if (sessionId && sessionId !== sessionIdRef.current) return;
            if (!mountedRef.current) return;
            if (reconnectTimerRef.current) return;
            setState((prev) => ({
                ...prev,
                connectionState: "disconnected",
            }));
            attemptReconnect();
        };

        const unsub1 = EventsOn("ve:stream_chunk", handleStreamChunk);
        const unsub2 = EventsOn("ve:stream_end", handleStreamEnd);
        const unsub3 = EventsOn("ve:disconnected", handleDisconnect);

        return () => {
            if (typeof unsub1 === "function") unsub1();
            else EventsOff("ve:stream_chunk");
            if (typeof unsub2 === "function") unsub2();
            else EventsOff("ve:stream_end");
            if (typeof unsub3 === "function") unsub3();
            else EventsOff("ve:disconnected");
        };
    }, [attemptReconnect]);

    // --- Message Sending ---

    const doSendMessage = useCallback(
        async (content: string, filePaths?: string[], messageId?: string) => {
            const sid = sessionIdRef.current;
            if (!sid) return;

            try {
                if (filePaths && filePaths.length > 0 && sendMessageWithAttachments) {
                    await sendMessageWithAttachments(sid, content, filePaths);
                } else if (sendGroupMessage && participants?.length) {
                    await sendGroupMessage(sid, content, mentionedParticipantIds(content, participants));
                } else if (sendMessage) {
                    await sendMessage(sid, content);
                } else {
                    const mod = await import("../../../wailsjs/go/main/App");
                    if (filePaths && filePaths.length > 0) {
                        await (mod as any).SendVEMessageWithAttachments(sid, content, filePaths);
                    } else if (participants?.length && typeof (mod as any).SendVEGroupMessage === "function") {
                        await (mod as any).SendVEGroupMessage(sid, content, mentionedParticipantIds(content, participants));
                    } else {
                        await (mod as any).SendVEMessage(sid, content);
                    }
                }
            } catch (err: any) {
                if (mountedRef.current) {
                    // Mark last user message as failed
                    setState((prev) => {
                        const msgs = [...prev.messages];
                        const failedIdx = messageId
                            ? msgs.findIndex((m) => m.id === messageId)
                            : msgs.findLastIndex((m) => m.role === "user");
                        if (failedIdx >= 0) {
                            msgs[failedIdx] = { ...msgs[failedIdx], sendFailed: true };
                        }
                        return {
                            ...prev,
                            messages: msgs,
                            error: {
                                type: "send_failed",
                                message: err?.message || "Send failed",
                            },
                        };
                    });
                }
            }
        },
        [participants, sendGroupMessage, sendMessage, sendMessageWithAttachments]
    );

    const drainQueuedMessages = useCallback(async () => {
        if (!sessionIdRef.current || queuedMessagesRef.current.length === 0) return;
        const queued = [...queuedMessagesRef.current];
        queuedMessagesRef.current = [];
        for (const msg of queued) {
            await doSendMessage(msg.content, msg.filePaths, msg.id);
        }
    }, [doSendMessage]);

    useEffect(() => {
        if (readOnly || state.connectionState !== "connected" || !state.sessionId) return;
        void drainQueuedMessages();
    }, [drainQueuedMessages, readOnly, state.connectionState, state.sessionId]);

    const handleSend = useCallback(async () => {
        const content = inputText.trim();
        if (readOnly) return;
        if (!content && pendingAttachments.length === 0) return;
        if (sendingRef.current) return;
        sendingRef.current = true;

        const filePaths = pendingAttachments.length > 0
            ? pendingAttachments.map((f) => f.path || "").filter(Boolean)
            : undefined;

        const userMsg: VEMessage = {
            id: generateMsgId(),
            role: "user",
            content,
            timestamp: Date.now(),
            fromId: "user",
            fromName: localSpeakerName,
            attachments: pendingAttachments.map((f) => ({
                type: classifyAttachmentType(f.name),
                filename: f.name,
                sizeBytes: f.size,
            })),
        };

        // If disconnected or still creating the session, queue the backend send
        // but still show the user's message immediately in the thread.
        if (state.connectionState !== "connected" || !state.sessionId) {
            queuedMessagesRef.current.push({ id: userMsg.id, content, filePaths });
            setState((prev) => ({
                ...prev,
                messages: [...prev.messages, userMsg],
                error: null,
            }));
            setInputText("");
            setPendingAttachments([]);
            window.setTimeout(() => { sendingRef.current = false; }, 0);
            return;
        }

        setSending(true);

        setState((prev) => ({
            ...prev,
            messages: [...prev.messages, userMsg],
            error: null,
        }));
        setInputText("");

        setPendingAttachments([]);

        try {
            await doSendMessage(content, filePaths, userMsg.id);
        } finally {
            sendingRef.current = false;
            setSending(false);
        }
    }, [inputText, state.connectionState, state.sessionId, pendingAttachments, doSendMessage, localSpeakerName, readOnly]);

    const handleKeyDown = useCallback(
        (e: React.KeyboardEvent) => {
            if (mentionKeyDown(e)) return;
            if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                handleSend();
            }
        },
        [handleSend, mentionKeyDown]
    );

    // --- Attachment Handling ---

    const handleAttachmentSelect = useCallback(async () => {
        if (readOnly) return;
        try {
            const mod = await import("../../../wailsjs/go/main/App");
            const selected = await (mod as any).SelectAIAssistantFiles?.();
            if (Array.isArray(selected) && selected.length > 0) {
                setPendingAttachments((prev) => [
                    ...prev,
                    ...selected.map((filePath: string) => ({
                        name: fileNameFromPath(filePath),
                        size: 0,
                        path: filePath,
                    })),
                ]);
                return;
            }
        } catch {
            // Fall through to browser file input for unit tests and non-Wails previews.
        }

        const input = document.createElement("input");
        input.type = "file";
        input.multiple = true;
        input.accept = ".txt,.md,.csv,.json,.xml,.yaml,.yml,.log,.go,.py,.js,.ts,.html,.css,.png,.jpg,.jpeg,.gif,.webp,.bmp,.pdf,.docx";
        input.onchange = () => {
            if (input.files) {
                setPendingAttachments((prev) => [
                    ...prev,
                    ...Array.from(input.files!).map((file) => ({
                        name: file.name,
                        size: file.size,
                        path: (file as any).path,
                    })),
                ]);
            }
        };
        input.click();
    }, [readOnly]);

    const removeAttachment = useCallback((index: number) => {
        setPendingAttachments((prev) => prev.filter((_, i) => i !== index));
    }, []);

    // --- Render ---

    return (
        <div
            data-testid="ve-conversation-view"
            style={{
                display: "flex",
                flexDirection: "column",
                height: "100%",
                background: theme.bg,
            }}
        >
            {/* Error / connection banner */}
            {(state.error || state.connectionState === "reconnecting") && (
                <div
                    data-testid="ve-error-banner"
                    style={{
                        padding: "8px 12px",
                        background: theme.errorBg || "#fef2f2",
                        color: theme.errorText || "#dc2626",
                        borderBottom: `1px solid ${theme.errorBorder || "#fecaca"}`,
                        fontSize: 12,
                        display: "flex",
                        alignItems: "center",
                        gap: 6,
                    }}
                >
                    <span>!</span>
                    <span>{state.error ? formatError(state.error, isZh) : (isZh ? "\u8fde\u63a5\u5df2\u65ad\u5f00\uff0c\u6b63\u5728\u91cd\u8fde" : "Connection lost, reconnecting")}</span>
                    {state.connectionState === "reconnecting" && (
                        <span style={{ marginLeft: "auto", fontSize: 11, opacity: 0.7 }}>
                            {isZh ? `\u91cd\u8fde\u4e2d (${state.reconnectAttempt}/${MAX_RECONNECT_RETRIES})...` : `Reconnecting (${state.reconnectAttempt}/${MAX_RECONNECT_RETRIES})...`}
                        </span>
                    )}
                </div>
            )}

            {/* Message List */}
            <div
                data-testid="ve-message-list"
                style={{
                    flex: 1,
                    overflowY: "auto",
                    padding: "12px 16px",
                }}
            >
                {state.messages.map((msg) => (
                    <MessageBubble
                        key={msg.id}
                        message={msg}
                        theme={theme}
                        isZh={isZh}
                        assistantName={readableSpeakerName(msg.fromName, msg.fromId, participants, assistantDisplayName)}
                        userName={readableSpeakerName(msg.fromName, msg.fromId, participants, localSpeakerName)}
                    />
                ))}

                {/* Streaming Indicator */}
                {state.streaming && (
                    <div data-testid="ve-streaming-indicator" style={{ marginTop: 8 }}>
                        <div
                            data-testid="ve-streaming-content"
                            style={{
                                padding: "8px 12px",
                                borderRadius: 8,
                                background: theme.fieldBg,
                                borderLeft: `3px solid ${theme.responseBorderLeft}`,
                                fontSize: 13,
                                color: theme.text,
                                wordBreak: "break-word",
                                overflowWrap: "anywhere",
                                whiteSpace: "pre-wrap",
                            }}
                        >
                            <div style={{ fontSize: 11, fontWeight: 600, color: participantColorById(participants, state.streamFromId, theme.responseBorderLeft), marginBottom: 2, whiteSpace: "normal" }}>
                                {readableSpeakerName(state.streamFromName, state.streamFromId, participants, assistantDisplayName)}
                            </div>
                            <MessageContentRenderer content={state.streamContent} theme={theme} />
                            <span className="ve-cursor-blink" style={{ opacity: 0.6 }}>▊</span>
                        </div>
                    </div>
                )}

                <div ref={messagesEndRef} />
            </div>

            {/* Attachment Preview Bar */}
            {pendingAttachments.length > 0 && (
                <div
                    data-testid="ve-attachment-preview-bar"
                    style={{
                        display: "flex",
                        gap: 6,
                        padding: "6px 12px",
                        borderTop: `1px solid ${theme.divider}`,
                        background: theme.fieldBg,
                        flexWrap: "wrap",
                    }}
                >
                    {pendingAttachments.map((file, idx) => (
                        <div
                            key={idx}
                            data-testid={`ve-attachment-preview-${idx}`}
                            style={{
                                display: "flex",
                                alignItems: "center",
                                gap: 4,
                                padding: "2px 8px",
                                borderRadius: 4,
                                background: theme.bg,
                                border: `1px solid ${theme.divider}`,
                                fontSize: 11,
                            }}
                        >
                            <span>{getAttachmentIcon(file.name)}</span>
                            <span style={{ maxWidth: 120, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                                {file.name}
                            </span>
                            <span style={{ color: theme.textMuted }}>
                                ({formatFileSize(file.size)})
                            </span>
                            <button
                                onClick={() => removeAttachment(idx)}
                                style={{
                                    border: "none",
                                    background: "none",
                                    cursor: "pointer",
                                    color: theme.closeBtnColor || "#dc2626",
                                    fontSize: 12,
                                    padding: "0 2px",
                                }}
                            >
                                x
                            </button>
                        </div>
                    ))}
                </div>
            )}

            {/* Input Area */}
            <div
                data-testid="ve-input-area"
                style={{
                    display: "flex",
                    alignItems: "flex-end",
                    gap: 8,
                    padding: "8px 12px",
                    borderTop: `1px solid ${theme.divider}`,
                    background: theme.inputBarBg,
                    opacity: veOnline && !readOnly ? 1 : 0.55,
                }}
            >
                {/* Attachment Button */}
                <button
                    data-testid="ve-attach-button"
                    onClick={handleAttachmentSelect}
                    disabled={!veOnline || readOnly}
                    style={{
                        border: "none",
                        background: "none",
                        cursor: veOnline && !readOnly ? "pointer" : "default",
                        fontSize: 18,
                        padding: "4px",
                        color: theme.textMuted,
                    }}
                    title={isZh ? "\u6dfb\u52a0\u9644\u4ef6" : "Add attachment"}
                >
                    +
                </button>

                <div style={{ position: "relative", flex: 1, minWidth: 0 }}>
                    {mentionOpen && (
                        <MentionPopover
                            filtered={mentionFiltered}
                            selectedIndex={mentionSelectedIndex}
                            onSelect={insertMentionParticipant}
                            onHover={setMentionSelectedIndex}
                            onClose={closeMentionPopover}
                            anchorRef={inputRef}
                            theme={theme}
                            lang={lang}
                        />
                    )}
                    <textarea
                    ref={inputRef}
                    data-testid="ve-input-textarea"
                    aria-label={isZh ? `\u53d1\u9001\u6d88\u606f\u7ed9 ${assistantDisplayName}` : `Message ${assistantDisplayName}`}
                    value={inputText}
                    onChange={(e) => {
                        setInputText(e.target.value);
                        updateMentionState(e.target.value, e.currentTarget.selectionStart);
                    }}
                    onClick={(e) => updateMentionState(inputText, e.currentTarget.selectionStart)}
                    onKeyUp={(e) => {
                        if (["ArrowDown", "ArrowUp", "Enter", "Escape"].includes(e.key)) return;
                        updateMentionState(e.currentTarget.value, e.currentTarget.selectionStart);
                    }}
                    onKeyDown={handleKeyDown}
                    disabled={!veOnline || readOnly}
                    placeholder={readOnly
                        ? (isZh ? "只读会话，不能继续发言" : "Read-only session")
                        : !veOnline
                        ? (isZh ? `${assistantDisplayName} \u5f53\u524d\u79bb\u7ebf\uff0c\u4e0a\u7ebf\u540e\u53ef\u7ee7\u7eed\u5bf9\u8bdd` : `${assistantDisplayName} is offline`)
                        : (isZh ? `\u53d1\u9001\u6d88\u606f\u7ed9 ${assistantDisplayName}...` : `Message ${assistantDisplayName}...`)}
                    rows={1}
                    style={{
                        width: "100%",
                        resize: "none",
                        border: `1px solid ${theme.fieldBorder}`,
                        borderRadius: 6,
                        padding: "6px 10px",
                        fontSize: 13,
                        color: theme.inputText,
                        background: veOnline && !readOnly ? theme.bg : theme.fieldBg,
                        outline: "none",
                        minHeight: 32,
                        maxHeight: 120,
                    }}
                    />
                </div>

                <button
                    data-testid="ve-send-button"
                    onClick={handleSend}
                    disabled={readOnly || !veOnline || sending || (!inputText.trim() && pendingAttachments.length === 0)}
                    style={{
                        border: "none",
                        background: theme.sendBtnBg,
                        color: theme.sendBtnColor,
                        borderRadius: 6,
                        padding: "6px 14px",
                        cursor: (readOnly || !veOnline || sending || (!inputText.trim() && pendingAttachments.length === 0)) ? "default" : "pointer",
                        fontSize: 13,
                        fontWeight: 500,
                        opacity: (readOnly || !veOnline || sending || (!inputText.trim() && pendingAttachments.length === 0)) ? 0.4 : 1,
                        transition: "opacity 0.15s",
                    }}
                    aria-label={isZh ? "\u53d1\u9001" : "Send"}
                >
                    {sending ? "..." : isZh ? "\u53d1\u9001" : "Send"}
                </button>
            </div>
        </div>
    );
});

// --- Sub-components ---

interface MessageBubbleProps {
    message: VEMessage;
    theme: Theme;
    isZh: boolean;
    assistantName: string;
    userName: string;
}

function MessageBubble({ message, theme, isZh, assistantName, userName }: MessageBubbleProps) {
    const isUser = message.role === "user";
    const speakerName = isUser ? userName : assistantName;

    return (
        <div
            data-testid={`ve-msg-${message.id}`}
            style={{
                marginBottom: 10,
                display: "flex",
                flexDirection: "column",
                alignItems: isUser ? "flex-end" : "flex-start",
            }}
        >
            <div
                data-testid={`ve-msg-label-${message.id}`}
                style={{
                    maxWidth: "80%",
                    marginBottom: 2,
                    padding: "0 4px",
                    color: theme.textMuted,
                    fontSize: 11,
                    fontWeight: 600,
                }}
            >
                {speakerName}
            </div>
            <div
                data-testid={`ve-msg-content-${message.id}`}
                style={{
                    maxWidth: "80%",
                    padding: "8px 12px",
                    borderRadius: 8,
                    background: isUser ? theme.sendBtnBg + "15" : theme.fieldBg,
                    borderLeft: isUser ? "none" : `3px solid ${theme.responseBorderLeft}`,
                    borderRight: isUser ? `3px solid ${theme.borderLeft}` : "none",
                    fontSize: 13,
                    color: theme.text,
                    wordBreak: "break-word",
                    overflowWrap: "anywhere",
                    whiteSpace: "pre-wrap",
                }}
            >
                <MessageContentRenderer content={message.content} theme={theme} isUser={isUser} />
                {message.sendFailed && (
                    <span
                        data-testid={`ve-msg-failed-${message.id}`}
                        style={{ color: theme.errorText || "#dc2626", fontSize: 11, marginLeft: 6 }}
                    >
                        {isZh ? "\u53d1\u9001\u5931\u8d25" : "Failed"}
                    </span>
                )}
            </div>

            {/* Attachment Display */}
            {message.attachments && message.attachments.length > 0 && (
                <div style={{ marginTop: 4, display: "flex", flexWrap: "wrap", gap: 4, maxWidth: "80%" }}>
                    {message.attachments.map((att, idx) => (
                        <AttachmentDisplay key={idx} attachment={att} theme={theme} />
                    ))}
                </div>
            )}
        </div>
    );
}

interface AttachmentDisplayProps {
    attachment: VEMessageAttachment;
    theme: Theme;
}

function AttachmentDisplay({ attachment, theme }: AttachmentDisplayProps) {
    if (attachment.type === "image" && attachment.fileUrl) {
        // Sanitize URL to prevent javascript: protocol XSS
        const safeUrl = attachment.fileUrl.startsWith("http://") || attachment.fileUrl.startsWith("https://") || attachment.fileUrl.startsWith("/")
            ? attachment.fileUrl
            : "";
        if (!safeUrl) return null;
        return (
            <div data-testid={`ve-att-image-${attachment.filename}`} style={{ marginTop: 4 }}>
                <img
                    src={safeUrl}
                    alt={attachment.filename}
                    style={{
                        maxWidth: 300,
                        maxHeight: 200,
                        borderRadius: 6,
                        border: `1px solid ${theme.divider}`,
                        cursor: "pointer",
                    }}
                    onClick={() => window.open(safeUrl, "_blank", "noopener,noreferrer")}
                />
            </div>
        );
    }

    // Text/Document chip
    const handleClick = () => {
        if (attachment.fileUrl) {
            const safeUrl = attachment.fileUrl.startsWith("http://") || attachment.fileUrl.startsWith("https://") || attachment.fileUrl.startsWith("/")
                ? attachment.fileUrl
                : "";
            if (safeUrl) {
                window.open(safeUrl, "_blank", "noopener,noreferrer");
            }
        }
    };

    return (
        <div
            data-testid={`ve-att-chip-${attachment.filename}`}
            style={{
                display: "inline-flex",
                alignItems: "center",
                gap: 4,
                padding: "3px 8px",
                borderRadius: 4,
                background: theme.fieldBg,
                border: `1px solid ${theme.divider}`,
                fontSize: 11,
                cursor: attachment.fileUrl ? "pointer" : "default",
            }}
            onClick={handleClick}
        >
            <span>{attachment.type === "image" ? "IMG" : attachment.type === "text" ? "TXT" : "FILE"}</span>
            <span style={{ maxWidth: 150, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                {attachment.filename}
            </span>
            {formatFileSize(attachment.sizeBytes ?? 0) && (
                <span style={{ color: theme.textMuted }}>
                    ({formatFileSize(attachment.sizeBytes ?? 0)})
                </span>
            )}
        </div>
    );
}

// --- Utility Functions ---

function formatError(error: VEConversationError, isZh: boolean): string {
    switch (error.type) {
        case "hub_disconnected":
            return isZh ? "Hub 连接中断" : "Hub disconnected";
        case "ve_offline":
            return isZh ? "该数字员工当前不在线" : "Digital employee is offline";
        case "send_failed":
            return isZh ? "消息发送失败" : "Message send failed";
        case "session_timeout":
            return isZh ? "会话创建超时（5秒）" : "Session creation timed out (5s)";
        default:
            return (error as VEConversationError).message;
    }
}

function classifyAttachmentType(filename: string): "text" | "image" | "file" {
    const ext = filename.toLowerCase().split(".").pop() || "";
    const imageExts = ["png", "jpg", "jpeg", "gif", "webp", "bmp"];
    const textExts = ["txt", "md", "csv", "json", "xml", "yaml", "yml", "log", "go", "py", "js", "ts", "html", "css"];
    if (imageExts.includes(ext)) return "image";
    if (textExts.includes(ext)) return "text";
    return "file";
}

function getAttachmentIcon(filename: string): string {
    const type = classifyAttachmentType(filename);
    switch (type) {
        case "image": return "IMG";
        case "text": return "TXT";
        default: return "FILE";
    }
}

function fileNameFromPath(filePath: string): string {
    const normalized = String(filePath || "").replace(/\\/g, "/");
    return normalized.split("/").filter(Boolean).pop() || String(filePath || "attachment");
}

function formatFileSize(bytes: number): string {
    if (!Number.isFinite(bytes) || bytes <= 0) return "";
    if (bytes < 1024) return `${bytes}B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)}MB`;
}

export { formatError, classifyAttachmentType, fileNameFromPath, formatFileSize, createSessionWithTimeout };
