/**
 * VEGroupChat - Group chat features for VE conversations.
 *
 * Implements:
 * - 14.1: "+" button with participant selector (filter by AccessPolicy, exclude already-in-group)
 * - 14.3: Group tab title update (participant names, truncate) and message broadcast
 * - 14.4: Participant response labels and offline notifications
 * - 14.5: Participant limit check (max_group_participants)
 * - 14.6: Listen for ve:group_config event to update local participant limit
 */

import { useCallback, useEffect, useRef, useState } from "react";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import { MessageContentRenderer } from "./MessageContentRenderer";
import type { VirtualEmployeeEntry } from "./VirtualEmployeeTab";
import type { Theme } from "./aiAssistantPanelTheme";

// --- Types ---

export interface GroupParticipant {
    id: string;
    name: string;
    online: boolean;
}

export interface GroupMessage {
    id: string;
    fromId: string;
    fromName: string;
    content: string;
    timestamp: number;
    /** Attachment metadata */
    attachments?: GroupMessageAttachment[];
}

export interface GroupMessageAttachment {
    type: "text" | "image" | "file";
    filename: string;
    fileUrl?: string;
    localPath?: string;
    sizeBytes?: number;
}

export interface GroupChatConfig {
    maxGroupParticipants: number;
}

export interface VEGroupChatProps {
    sessionId: string;
    participants: GroupParticipant[];
    messages: GroupMessage[];
    theme: Theme;
    lang?: string;
    /** Current max group participants limit */
    maxGroupParticipants?: number;
    /** Callback to add a VE to the group */
    onAddParticipant?: (veId: string) => Promise<void>;
    /** Callback to update tab title */
    onTitleChange?: (title: string) => void;
    /** Override for testing - list available digital employees */
    listVirtualEmployees?: () => Promise<VirtualEmployeeEntry[]>;
    /** Override for testing - add VE to group binding */
    addVEToGroup?: (sessionId: string, veId: string) => Promise<void>;
    /** Download a persisted group-discussion attachment */
    onDownloadAttachment?: (attachment: GroupMessageAttachment, message: GroupMessage) => void;
    /** Whether the header should expose the participant add control */
    allowParticipantAdd?: boolean;
}

// --- Constants ---

const DEFAULT_MAX_GROUP_PARTICIPANTS = 5;
const MAX_UPPER_LIMIT = 10;
const TAB_TITLE_MAX_LENGTH = 30;

// --- Helpers ---

/** Build a group tab title from participant names, truncated if too long. */
export function buildGroupTabTitle(participants: GroupParticipant[]): string {
    if (participants.length === 0) return "\u7fa4\u804a";
    const names = participants.map((p) => p.name);
    const joined = names.join(", ");
    if (joined.length <= TAB_TITLE_MAX_LENGTH) return joined;
    // Truncate: include names until we exceed the limit
    let result = "";
    for (const name of names) {
        const candidate = result ? `${result}, ${name}` : name;
        if (candidate.length > TAB_TITLE_MAX_LENGTH - 3) {
            return result ? `${result}...` : `${name.slice(0, TAB_TITLE_MAX_LENGTH - 3)}...`;
        }
        result = candidate;
    }
    return result;
}

/** Assign a color to a participant based on their index. */
const PARTICIPANT_COLORS = [
    "#3b82f6", // blue
    "#10b981", // emerald
    "#f59e0b", // amber
    "#8b5cf6", // violet
    "#ef4444", // red
    "#06b6d4", // cyan
    "#ec4899", // pink
    "#14b8a6", // teal
    "#f97316", // orange
    "#6366f1", // indigo
];

export function getParticipantColor(index: number): string {
    return PARTICIPANT_COLORS[index % PARTICIPANT_COLORS.length];
}

// --- Hook: useGroupConfig ---

/**
 * Hook to manage group chat configuration (max participants).
 * Listens for ve:group_config events to update the limit.
 */
export function useGroupConfig(initialMax?: number): GroupChatConfig {
    const [maxGroupParticipants, setMaxGroupParticipants] = useState(
        initialMax ?? DEFAULT_MAX_GROUP_PARTICIPANTS
    );

    useEffect(() => {
        const handler = (data: any) => {
            const newMax = data?.max_group_participants ?? data?.maxGroupParticipants;
            if (typeof newMax === "number" && newMax >= 1 && newMax <= MAX_UPPER_LIMIT) {
                setMaxGroupParticipants(newMax);
            }
        };

        const unsub = EventsOn("ve:group_config", handler);
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("ve:group_config");
        };
    }, []);

    return { maxGroupParticipants };
}

// --- Component: ParticipantSelector ---

export interface ParticipantSelectorProps {
    sessionId: string;
    currentParticipants: GroupParticipant[];
    maxGroupParticipants: number;
    theme: Theme;
    lang?: string;
    onAdd: (ve: VirtualEmployeeEntry) => void;
    listVirtualEmployees?: () => Promise<VirtualEmployeeEntry[]>;
}

export function ParticipantSelector({
    sessionId,
    currentParticipants,
    maxGroupParticipants,
    theme,
    lang,
    onAdd,
    listVirtualEmployees,
}: ParticipantSelectorProps) {
    const [open, setOpen] = useState(false);
    const [available, setAvailable] = useState<VirtualEmployeeEntry[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const popoverRef = useRef<HTMLDivElement>(null);

    const isZh = !lang || lang.startsWith("zh");

    // Check if limit is reached
    const limitReached = currentParticipants.length >= maxGroupParticipants;

    const fetchAvailable = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            let fn = listVirtualEmployees;
            if (!fn) {
                const mod = await import("../../../wailsjs/go/main/App");
                fn = (mod as any).ListVirtualEmployees;
            }
            const all = await fn!();
            // Filter out digital employees already in the group
            const currentIds = new Set(currentParticipants.map((p) => p.id));
            const filtered = (all || []).filter((ve) => {
                const machineId = ve.machine_id || ve.id;
                return !currentIds.has(ve.id) && !currentIds.has(machineId) && ve.online_status === "online";
            });
            setAvailable(filtered);
        } catch {
            setError(isZh ? "\u83b7\u53d6\u5217\u8868\u5931\u8d25" : "Failed to load");
            setAvailable([]);
        } finally {
            setLoading(false);
        }
    }, [currentParticipants, listVirtualEmployees, isZh]);

    const handleToggle = useCallback(() => {
        if (limitReached) {
            setError(
                isZh
                    ? `\u7fa4\u804a\u4eba\u6570\u5df2\u6ee1\uff08\u6700\u591a ${maxGroupParticipants} \u4eba\uff09`
                    : `Group is full (max ${maxGroupParticipants})`
            );
            // Show error briefly then clear
            setTimeout(() => setError(""), 3000);
            return;
        }
        if (!open) {
            fetchAvailable();
        }
        setOpen(!open);
    }, [open, limitReached, maxGroupParticipants, isZh, fetchAvailable]);

    // Close popover on outside click
    useEffect(() => {
        if (!open) return;
        const handler = (e: MouseEvent) => {
            if (popoverRef.current && !popoverRef.current.contains(e.target as Node)) {
                setOpen(false);
            }
        };
        document.addEventListener("mousedown", handler);
        return () => document.removeEventListener("mousedown", handler);
    }, [open]);

    return (
        <div style={{ position: "relative", display: "inline-block" }} ref={popoverRef}>
            {/* "+" Button */}
            <button
                data-testid="group-add-participant-btn"
                onClick={handleToggle}
                style={{
                    border: `1px solid ${theme.divider}`,
                    background: theme.fieldBg,
                    borderRadius: "50%",
                    width: 28,
                    height: 28,
                    cursor: "pointer",
                    fontSize: 16,
                    color: theme.text,
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                }}
                title={isZh ? "\u6dfb\u52a0\u53c2\u4e0e\u8005" : "Add participant"}
            >
                +
            </button>
            {/* Error toast */}
            {error && (
                <div
                    data-testid="group-limit-error"
                    style={{
                        position: "absolute",
                        top: 32,
                        right: 0,
                        background: theme.errorBg || "#fef2f2",
                        color: theme.errorText || "#dc2626",
                        border: `1px solid ${theme.errorBorder || "#fecaca"}`,
                        borderRadius: 6,
                        padding: "6px 10px",
                        fontSize: 12,
                        whiteSpace: "nowrap",
                        zIndex: 9999,
                    }}
                >
                    {error}
                </div>
            )}

            {/* Participant picker popover */}
            {open && (
                <div
                    data-testid="group-participant-picker"
                    style={{
                        position: "absolute",
                        top: 32,
                        right: 0,
                        background: theme.bg,
                        border: `1px solid ${theme.divider}`,
                        borderRadius: 8,
                        boxShadow: "0 4px 12px rgba(0,0,0,0.15)",
                        zIndex: 9999,
                        minWidth: 200,
                        maxHeight: 240,
                        overflowY: "auto",
                        padding: "4px 0",
                    }}
                >
                    {loading && (
                        <div style={{ padding: "8px 12px", color: theme.textMuted, fontSize: 12 }}>
                            {isZh ? "\u52a0\u8f7d\u4e2d..." : "Loading..."}
                        </div>
                    )}
                    {!loading && available.length === 0 && (
                        <div
                            data-testid="group-picker-empty"
                            style={{ padding: "8px 12px", color: theme.textMuted, fontSize: 12 }}
                        >
                            {isZh ? "\u6ca1\u6709\u53ef\u6dfb\u52a0\u7684\u6570\u5b57\u5458\u5de5" : "No available digital employees"}
                        </div>
                    )}
                    {!loading &&
                        available.map((ve) => (
                            <div
                                key={ve.id}
                                data-testid={`group-picker-item-${ve.id}`}
                                onClick={() => {
                                    onAdd(ve);
                                    setOpen(false);
                                }}
                                style={{
                                    padding: "6px 12px",
                                    cursor: "pointer",
                                    fontSize: 13,
                                    color: theme.text,
                                    display: "flex",
                                    alignItems: "center",
                                    gap: 6,
                                }}
                                onMouseEnter={(e) => {
                                    (e.currentTarget as HTMLElement).style.background = theme.fieldBg;
                                }}
                                onMouseLeave={(e) => {
                                    (e.currentTarget as HTMLElement).style.background = "";
                                }}
                            >
                                <span
                                    style={{
                                        width: 6,
                                        height: 6,
                                        borderRadius: "50%",
                                        background: "#22c55e",
                                        flexShrink: 0,
                                    }}
                                />
                                <span>{ve.name}</span>
                            </div>
                        ))}
                </div>
            )}
        </div>
    );
}

// --- Component: GroupMessageBubble ---

export interface GroupMessageBubbleProps {
    message: GroupMessage;
    participantIndex: number;
    theme: Theme;
    isUser?: boolean;
    onDownloadAttachment?: (attachment: GroupMessageAttachment, message: GroupMessage) => void;
}

export function GroupMessageBubble({ message, participantIndex, theme, isUser, onDownloadAttachment }: GroupMessageBubbleProps) {
    const color = isUser ? theme.text : getParticipantColor(participantIndex);
    const hasAttachments = !!message.attachments?.length;
    const hasContent = message.content.trim().length > 0;

    return (
        <div
            data-testid={`group-msg-${message.id}`}
            style={{ marginBottom: 10 }}
        >
            {/* Participant name label */}
            <div
                data-testid={`group-msg-label-${message.id}`}
                style={{
                    fontSize: 11,
                    fontWeight: 600,
                    color,
                    marginBottom: 2,
                    paddingLeft: 4,
                }}
            >
                {message.fromName}
            </div>
            {/* Message content */}
            {(hasContent || !hasAttachments) && <div
                data-testid={`group-msg-content-${message.id}`}
                style={{
                    padding: "8px 12px",
                    borderRadius: 8,
                    background: isUser ? `${theme.sendBtnBg}15` : theme.fieldBg,
                    borderLeft: isUser ? "none" : `3px solid ${color}`,
                    fontSize: 13,
                    color: theme.text,
                    wordBreak: "break-word",
                }}
            >
                <MessageContentRenderer content={message.content} theme={theme} isUser={isUser} />
            </div>}
            {/* Attachments */}
            {hasAttachments && (
                <div style={{ marginTop: 4, display: "flex", flexWrap: "wrap", gap: 4, paddingLeft: 4 }}>
                    {message.attachments?.map((att, idx) => (
                        <div
                            key={idx}
                            data-testid={`group-msg-att-${message.id}-${idx}`}
                            style={{
                                display: "inline-flex",
                                alignItems: "center",
                                gap: 4,
                                padding: "3px 8px",
                                borderRadius: 4,
                                background: theme.fieldBg,
                                border: `1px solid ${theme.divider}`,
                                fontSize: 11,
                            }}
                        >
                            <span>{att.type === "image" ? "IMG" : att.type === "text" ? "TXT" : "FILE"}</span>
                            {att.fileUrl || att.localPath ? <button type="button" onClick={() => onDownloadAttachment?.(att, message)} title={att.localPath || att.fileUrl} style={{ border: 0, padding: 0, background: "transparent", color: theme.text, textDecoration: "underline", cursor: "pointer", font: "inherit" }}><span>{att.filename}</span><span style={{ marginLeft: 5, color: theme.textMuted, fontSize: 10 }}>{att.localPath ? "OPEN" : "GET"}</span></button> : <span>{att.filename}</span>}
                        </div>
                    ))}
                </div>
            )}
        </div>
    );
}

// --- Component: ParticipantOfflineNotice ---

export interface ParticipantOfflineNoticeProps {
    participantName: string;
    theme: Theme;
    lang?: string;
}

export function ParticipantOfflineNotice({ participantName, theme, lang }: ParticipantOfflineNoticeProps) {
    const isZh = !lang || lang.startsWith("zh");
    return (
        <div
            data-testid={`group-offline-notice-${participantName}`}
            style={{
                padding: "4px 12px",
                fontSize: 11,
                color: theme.textMuted,
                textAlign: "center",
                fontStyle: "italic",
            }}
        >
            {isZh ? `${participantName} \u5df2\u79bb\u7ebf` : `${participantName} went offline`}
        </div>
    );
}
// --- Main Component: VEGroupChatView ---

export function VEGroupChatView({
    sessionId,
    participants,
    messages,
    theme,
    lang,
    maxGroupParticipants: initialMax,
    onAddParticipant,
    onTitleChange,
    listVirtualEmployees,
    addVEToGroup,
    onDownloadAttachment,
    allowParticipantAdd = true,
}: VEGroupChatProps) {
    const { maxGroupParticipants } = useGroupConfig(initialMax);
    const [offlineNotices, setOfflineNotices] = useState<string[]>([]);
    const isZh = !lang || lang.startsWith("zh");

    // Update tab title when participants change
    useEffect(() => {
        if (onTitleChange && participants.length > 0) {
            onTitleChange(buildGroupTabTitle(participants));
        }
    }, [participants, onTitleChange]);

    // Listen for ve:status_change to detect participant going offline
    useEffect(() => {
        const handler = (data: any) => {
            const veId = data?.ve_id || data?.veId;
            const status = data?.online_status || data?.status;
            if (status === "offline") {
                const participant = participants.find((p) => p.id === veId);
                if (participant) {
                    setOfflineNotices((prev) => [...prev, participant.name]);
                }
            }
        };

        const unsub = EventsOn("ve:status_change", handler);
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("ve:status_change");
        };
    }, [participants]);

    // Handle adding a participant
    const handleAdd = useCallback(
        async (ve: VirtualEmployeeEntry) => {
            // Check limit before adding
            if (participants.length >= maxGroupParticipants) {
                return; // ParticipantSelector already shows the error
            }
            try {
                if (addVEToGroup) {
                    await addVEToGroup(sessionId, ve.id);
                } else if (onAddParticipant) {
                    await onAddParticipant(ve.id);
                } else {
                    const mod = await import("../../../wailsjs/go/main/App");
                    await (mod as any).AddVEToGroup(sessionId, ve.id);
                }
            } catch (err: any) {
                // Error handling is done by the caller
                console.error("Failed to add participant:", err);
            }
        },
        [sessionId, participants.length, maxGroupParticipants, addVEToGroup, onAddParticipant]
    );

    // Build participant index map for coloring
    const participantIndexMap = new Map<string, number>();
    participants.forEach((p, i) => participantIndexMap.set(p.id, i));

    return (
        <div
            data-testid="ve-group-chat-view"
            style={{ display: "flex", flexDirection: "column", height: "100%" }}
        >
            {/* Header with participant selector */}
            <div
                data-testid="group-chat-header"
                style={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    padding: "6px 12px",
                    borderBottom: `1px solid ${theme.divider}`,
                    background: theme.inputBarBg,
                }}
            >
                <div style={{ fontSize: 12, color: theme.textMuted }}>
                    {isZh ? `${participants.length} \u4f4d\u53c2\u4e0e\u8005` : `${participants.length} participants`}
                </div>
                {allowParticipantAdd && (
                    <ParticipantSelector
                        sessionId={sessionId}
                        currentParticipants={participants}
                        maxGroupParticipants={maxGroupParticipants}
                        theme={theme}
                        lang={lang}
                        onAdd={handleAdd}
                        listVirtualEmployees={listVirtualEmployees}
                    />
                )}
            </div>

            {/* Message list */}
            <div
                data-testid="group-message-list"
                style={{ flex: 1, overflowY: "auto", padding: "12px 16px" }}
            >
                {messages.map((msg) => {
                    const pIdx = participantIndexMap.get(msg.fromId) ?? 0;
                    const isUser = !participants.some((p) => p.id === msg.fromId);
                    return (
                        <GroupMessageBubble
                            key={msg.id}
                            message={msg}
                            participantIndex={pIdx}
                            theme={theme}
                            isUser={isUser}
                            onDownloadAttachment={onDownloadAttachment}
                        />
                    );
                })}

                {/* Offline notices */}
                {offlineNotices.map((name, idx) => (
                    <ParticipantOfflineNotice
                        key={`offline-${idx}`}
                        participantName={name}
                        theme={theme}
                        lang={lang}
                    />
                ))}
            </div>
        </div>
    );
}

export { DEFAULT_MAX_GROUP_PARTICIPANTS, MAX_UPPER_LIMIT, TAB_TITLE_MAX_LENGTH };
