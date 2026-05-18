import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState } from "react";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import { MessageContentRenderer } from "./MessageContentRenderer";
import type { Theme } from "./aiAssistantPanelTheme";
import type { MentionParticipant } from "./MentionPopover";

// --- Types ---

export interface VEMessage {
    id: string;
    role: "user" | "assistant";
    content: string;
    timestamp: number;
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
    content: string;
    filePaths?: string[];
};

export interface VEConversationState {
    sessionId: string | null;
    messages: VEMessage[];
    streaming: boolean;
    streamContent: string;
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
    /** Override for testing Wails bindings. */
    initiateConversation?: (veId: string) => Promise<{ session_id: string; ve_id: string; ve_name: string }>;
    sendMessage?: (sessionId: string, content: string) => Promise<void>;
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
    return Promise.race([
        promise,
        new Promise<T>((_, reject) =>
            setTimeout(() => reject(new Error("session_timeout")), timeoutMs)
        ),
    ]);
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
    initiateConversation,
    sendMessage,
    sendMessageWithAttachments,
    closeSession,
    onSessionIdChange,
}, ref) {
    const [state, setState] = useState<VEConversationState>({
        sessionId: existingSessionId || null,
        messages: initialMessages || [],
        streaming: false,
        streamContent: "",
        error: null,
        connectionState: existingSessionId ? "connected" : "connected",
        reconnectAttempt: 0,
    });
    const [inputText, setInputText] = useState(initialInputText || "");
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
    const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const queuedMessagesRef = useRef<QueuedVEMessage[]>([]);
    const sessionIdRef = useRef<string | null>(null);
    const reconnectAttemptRef = useRef(0);

    const isZh = !lang || lang.startsWith("zh");

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
            return;
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
            try {
                await initSession();
                // Deliver queued messages in order
                if (mountedRef.current && queuedMessagesRef.current.length > 0) {
                    const queued = [...queuedMessagesRef.current];
                    queuedMessagesRef.current = [];
                    for (const msg of queued) {
                        await doSendMessage(msg.content, msg.filePaths);
                    }
                }
            } catch {
                if (mountedRef.current) {
                    attemptReconnect();
                }
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
            setState((prev) => ({
                ...prev,
                streaming: true,
                streamContent: prev.streamContent + content,
            }));
        };

        const handleStreamEnd = (data: any) => {
            const sessionId = data?.session_id || data?.sessionId;
            if (sessionId && sessionId !== sessionIdRef.current) return;
            if (!mountedRef.current) return;
            setState((prev) => {
                const finalContent = prev.streamContent;
                if (!finalContent) return { ...prev, streaming: false, streamContent: "" };
                const newMsg: VEMessage = {
                    id: generateMsgId(),
                    role: "assistant",
                    content: finalContent,
                    timestamp: Date.now(),
                };
                return {
                    ...prev,
                    streaming: false,
                    streamContent: "",
                    messages: [...prev.messages, newMsg],
                };
            });
        };

        const handleDisconnect = () => {
            if (!mountedRef.current) return;
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
        async (content: string, filePaths?: string[]) => {
            const sid = sessionIdRef.current;
            if (!sid) return;

            try {
                if (filePaths && filePaths.length > 0 && sendMessageWithAttachments) {
                    await sendMessageWithAttachments(sid, content, filePaths);
                } else if (sendMessage) {
                    await sendMessage(sid, content);
                } else {
                    const mod = await import("../../../wailsjs/go/main/App");
                    if (filePaths && filePaths.length > 0) {
                        await (mod as any).SendVEMessageWithAttachments(sid, content, filePaths);
                    } else {
                        await (mod as any).SendVEMessage(sid, content);
                    }
                }
            } catch (err: any) {
                if (mountedRef.current) {
                    // Mark last user message as failed
                    setState((prev) => {
                        const msgs = [...prev.messages];
                        const lastUserIdx = msgs.findLastIndex((m) => m.role === "user");
                        if (lastUserIdx >= 0) {
                            msgs[lastUserIdx] = { ...msgs[lastUserIdx], sendFailed: true };
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
        [sendMessage, sendMessageWithAttachments]
    );

    const handleSend = useCallback(async () => {
        const content = inputText.trim();
        if (!content && pendingAttachments.length === 0) return;
        if (sending) return;

        const filePaths = pendingAttachments.length > 0
            ? pendingAttachments.map((f) => f.path || "").filter(Boolean)
            : undefined;

        // If disconnected, queue the message with any native attachment paths.
        if (state.connectionState !== "connected" || !state.sessionId) {
            queuedMessagesRef.current.push({ content, filePaths });
            setInputText("");
            setPendingAttachments([]);
            return;
        }

        setSending(true);
        const userMsg: VEMessage = {
            id: generateMsgId(),
            role: "user",
            content,
            timestamp: Date.now(),
            attachments: pendingAttachments.map((f) => ({
                type: classifyAttachmentType(f.name),
                filename: f.name,
                sizeBytes: f.size,
            })),
        };

        setState((prev) => ({
            ...prev,
            messages: [...prev.messages, userMsg],
            error: null,
        }));
        setInputText("");

        setPendingAttachments([]);

        await doSendMessage(content, filePaths);
        setSending(false);
    }, [inputText, sending, state.connectionState, state.sessionId, pendingAttachments, doSendMessage]);

    const handleKeyDown = useCallback(
        (e: React.KeyboardEvent) => {
            if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                handleSend();
            }
        },
        [handleSend]
    );

    // --- Attachment Handling ---

    const handleAttachmentSelect = useCallback(async () => {
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
    }, []);

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
            {/* Error Banner */}
            {state.error && (
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
                    <span>{formatError(state.error, isZh)}</span>
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
                    />
                ))}

                {/* Streaming Indicator */}
                {state.streaming && (
                    <div data-testid="ve-streaming-indicator" style={{ marginTop: 8 }}>
                        <div
                            style={{
                                padding: "8px 12px",
                                borderRadius: 8,
                                background: theme.fieldBg,
                                borderLeft: `3px solid ${theme.responseBorderLeft}`,
                                fontSize: 13,
                                color: theme.text,
                                wordBreak: "break-word",
                            }}
                        >
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
                    opacity: veOnline ? 1 : 0.55,
                }}
            >
                {/* Attachment Button */}
                <button
                    data-testid="ve-attach-button"
                    onClick={handleAttachmentSelect}
                    disabled={!veOnline}
                    style={{
                        border: "none",
                        background: "none",
                        cursor: veOnline ? "pointer" : "default",
                        fontSize: 18,
                        padding: "4px",
                        color: theme.textMuted,
                    }}
                    title={isZh ? "\u6dfb\u52a0\u9644\u4ef6" : "Add attachment"}
                >
                    +
                </button>

                <textarea
                    data-testid="ve-input-textarea"
                    aria-label={isZh ? `\u53d1\u9001\u6d88\u606f\u7ed9 ${veName}` : `Message ${veName}`}
                    value={inputText}
                    onChange={(e) => setInputText(e.target.value)}
                    onKeyDown={handleKeyDown}
                    disabled={!veOnline}
                    placeholder={!veOnline
                        ? (isZh ? `${veName} \u5f53\u524d\u79bb\u7ebf\uff0c\u4e0a\u7ebf\u540e\u53ef\u7ee7\u7eed\u5bf9\u8bdd` : `${veName} is offline`)
                        : (isZh ? `\u53d1\u9001\u6d88\u606f\u7ed9 ${veName}...` : `Message ${veName}...`)}
                    rows={1}
                    style={{
                        flex: 1,
                        resize: "none",
                        border: `1px solid ${theme.fieldBorder}`,
                        borderRadius: 6,
                        padding: "6px 10px",
                        fontSize: 13,
                        color: theme.inputText,
                        background: veOnline ? theme.bg : theme.fieldBg,
                        outline: "none",
                        minHeight: 32,
                        maxHeight: 120,
                    }}
                />

                <button
                    data-testid="ve-send-button"
                    onClick={handleSend}
                    disabled={!veOnline || sending || (!inputText.trim() && pendingAttachments.length === 0)}
                    style={{
                        border: "none",
                        background: theme.sendBtnBg,
                        color: theme.sendBtnColor,
                        borderRadius: 6,
                        padding: "6px 14px",
                        cursor: (!veOnline || sending || (!inputText.trim() && pendingAttachments.length === 0)) ? "default" : "pointer",
                        fontSize: 13,
                        fontWeight: 500,
                        opacity: (!veOnline || sending || (!inputText.trim() && pendingAttachments.length === 0)) ? 0.4 : 1,
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
}

function MessageBubble({ message, theme, isZh }: MessageBubbleProps) {
    const isUser = message.role === "user";

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
