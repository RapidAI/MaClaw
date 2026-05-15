import { useCallback, useEffect, useRef, useState } from "react";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import type { Theme } from "./aiAssistantPanelTheme";

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
    /** Pre-existing session ID to resume (sticky session). Skips initiation if provided. */
    existingSessionId?: string;
    /** Override for testing — Wails bindings */
    initiateConversation?: (veId: string) => Promise<{ session_id: string; ve_id: string; ve_name: string }>;
    sendMessage?: (sessionId: string, content: string) => Promise<void>;
    sendMessageWithAttachments?: (sessionId: string, content: string, filePaths: string[]) => Promise<void>;
    closeSession?: (sessionId: string) => Promise<void>;
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

export function VEConversationView({
    veId,
    veName,
    theme,
    lang,
    existingSessionId,
    initiateConversation,
    sendMessage,
    sendMessageWithAttachments,
    closeSession,
}: VEConversationViewProps) {
    const [state, setState] = useState<VEConversationState>({
        sessionId: existingSessionId || null,
        messages: [],
        streaming: false,
        streamContent: "",
        error: null,
        connectionState: existingSessionId ? "connected" : "connected",
        reconnectAttempt: 0,
    });
    const [inputText, setInputText] = useState("");
    const [sending, setSending] = useState(false);
    const [pendingAttachments, setPendingAttachments] = useState<File[]>([]);

    const mountedRef = useRef(true);
    const messagesEndRef = useRef<HTMLDivElement>(null);
    const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const queuedMessagesRef = useRef<string[]>([]);
    const sessionIdRef = useRef<string | null>(null);
    const reconnectAttemptRef = useRef(0);

    const isZh = !lang || lang.startsWith("zh");

    // Keep sessionIdRef in sync
    useEffect(() => {
        sessionIdRef.current = state.sessionId;
    }, [state.sessionId]);

    // Keep reconnectAttemptRef in sync with state resets (e.g. successful session init)
    useEffect(() => {
        reconnectAttemptRef.current = state.reconnectAttempt;
    }, [state.reconnectAttempt]);

    // Scroll to bottom on new messages
    useEffect(() => {
        messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
    }, [state.messages, state.streamContent]);

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

    // Initialize session on mount (skip if resuming an existing sticky session)
    useEffect(() => {
        mountedRef.current = true;
        if (!existingSessionId) {
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
                        ? "重连失败，已达最大重试次数"
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
                        await doSendMessage(msg);
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

        // If disconnected, queue the message
        if (state.connectionState === "disconnected" || !state.sessionId) {
            queuedMessagesRef.current.push(content);
            setInputText("");
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

        // For file attachments, we'd need file paths (Wails uses native paths)
        // In the browser context, we pass file names; actual path resolution happens via Wails dialog
        const filePaths = pendingAttachments.length > 0
            ? pendingAttachments.map((f) => (f as any).path || f.name)
            : undefined;
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

    const handleAttachmentSelect = useCallback(() => {
        const input = document.createElement("input");
        input.type = "file";
        input.multiple = true;
        input.accept = ".txt,.md,.csv,.json,.xml,.yaml,.log,.go,.py,.js,.ts,.html,.css,.png,.jpg,.jpeg,.gif,.webp,.bmp,.pdf,.docx";
        input.onchange = () => {
            if (input.files) {
                setPendingAttachments((prev) => [...prev, ...Array.from(input.files!)]);
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
                    <span>⚠️</span>
                    <span>{formatError(state.error, isZh)}</span>
                    {state.connectionState === "reconnecting" && (
                        <span style={{ marginLeft: "auto", fontSize: 11, opacity: 0.7 }}>
                            {isZh ? `重连中 (${state.reconnectAttempt}/${MAX_RECONNECT_RETRIES})...` : `Reconnecting (${state.reconnectAttempt}/${MAX_RECONNECT_RETRIES})...`}
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
                                whiteSpace: "pre-wrap",
                            }}
                        >
                            {state.streamContent}
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
                                ×
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
                }}
            >
                {/* Attachment Button */}
                <button
                    data-testid="ve-attach-button"
                    onClick={handleAttachmentSelect}
                    style={{
                        border: "none",
                        background: "none",
                        cursor: "pointer",
                        fontSize: 18,
                        padding: "4px",
                        color: theme.textMuted,
                    }}
                    title={isZh ? "添加附件" : "Add attachment"}
                >
                    📎
                </button>

                <textarea
                    data-testid="ve-input-textarea"
                    aria-label={isZh ? `发送消息给 ${veName}` : `Message ${veName}`}
                    value={inputText}
                    onChange={(e) => setInputText(e.target.value)}
                    onKeyDown={handleKeyDown}
                    placeholder={isZh ? `发送消息给 ${veName}...` : `Message ${veName}...`}
                    rows={1}
                    style={{
                        flex: 1,
                        resize: "none",
                        border: `1px solid ${theme.fieldBorder}`,
                        borderRadius: 6,
                        padding: "6px 10px",
                        fontSize: 13,
                        color: theme.inputText,
                        background: theme.bg,
                        outline: "none",
                        minHeight: 32,
                        maxHeight: 120,
                    }}
                />

                <button
                    data-testid="ve-send-button"
                    onClick={handleSend}
                    disabled={sending || (!inputText.trim() && pendingAttachments.length === 0)}
                    style={{
                        border: `1px solid ${theme.sendBtnBorder}`,
                        background: theme.sendBtnColor,
                        color: "#fff",
                        borderRadius: 6,
                        padding: "6px 12px",
                        cursor: sending ? "not-allowed" : "pointer",
                        fontSize: 13,
                        opacity: sending || (!inputText.trim() && pendingAttachments.length === 0) ? 0.5 : 1,
                    }}
                >
                    {sending ? "..." : isZh ? "发送" : "Send"}
                </button>
            </div>
        </div>
    );
}

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
                    background: isUser ? theme.sendBtnColor + "15" : theme.fieldBg,
                    borderLeft: isUser ? "none" : `3px solid ${theme.responseBorderLeft}`,
                    borderRight: isUser ? `3px solid ${theme.borderLeft}` : "none",
                    fontSize: 13,
                    color: theme.text,
                    whiteSpace: "pre-wrap",
                    wordBreak: "break-word",
                }}
            >
                {message.content}
                {message.sendFailed && (
                    <span
                        data-testid={`ve-msg-failed-${message.id}`}
                        style={{ color: theme.errorText || "#dc2626", fontSize: 11, marginLeft: 6 }}
                    >
                        {isZh ? "⚠️ 发送失败" : "⚠️ Failed"}
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
            <span>📄</span>
            <span style={{ maxWidth: 150, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                {attachment.filename}
            </span>
            {attachment.sizeBytes != null && (
                <span style={{ color: theme.textMuted }}>
                    ({formatFileSize(attachment.sizeBytes)})
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
        case "image": return "🖼️";
        case "text": return "📝";
        default: return "📄";
    }
}

function formatFileSize(bytes: number): string {
    if (bytes < 1024) return `${bytes}B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)}MB`;
}

export { formatError, classifyAttachmentType, formatFileSize, createSessionWithTimeout };
