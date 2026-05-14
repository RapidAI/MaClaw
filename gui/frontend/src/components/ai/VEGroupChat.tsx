/**
 * VEGroupChat — Group chat features for VE conversations.
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
    /** Override for testing — list available VEs */
    listVirtualEmployees?: () => Promise<VirtualEmployeeEntry[]>;
    /** Override for testing — add VE to group binding */
    addVEToGroup?: (sessionId: string, veId: string) => Promise<void>;
}

// --- Constants ---

const DEFAULT_MAX_GROUP_PARTICIPANTS = 5;
const MAX_UPPER_LIMIT = 10;
const TAB_TITLE_MAX_LENGTH = 30;

// --- Helpers ---

/** Build a group tab title from participant names, truncated if too long. */
export function buildGroupTabTitle(participants: GroupParticipant[]): string {
    if (participants.length === 0) return "群聊";
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
            // Filter out VEs already in the group
            const currentIds = new Set(currentParticipants.map((p) => p.id));
            const filtered = (all || []).filter(
                (ve) => !currentIds.has(ve.id) && ve.online_status === "online"
            );
            setAvailable(filtered);
        } catch {
            setError(isZh ? "获取列表失败" : "Failed to load");
            setAvailable([]);
        } finally {
            setLoading(false);
        }
    }, [currentParticipants, listVirtualEmployees, isZh]);

    const handleToggle = useCallback(() => {
        if (limitReached) {
            setError(
                isZh
                    ? `群聊人数已满（最多 ${maxGroupParticipants} 人）`
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
                title={isZh ? "添加参与者" : "Add participant"}
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
                            {isZh ? "加载中..." : "Loading..."}
                        </div>
                    )}
                    {!loading && available.length === 0 && (
                        <div
                            data-testid="group-picker-empty"
                            style={{ padding: "8px 12px", color: theme.textMuted, fontSize: 12 }}
                        >
                            {isZh ? "没有可添加的虚拟员工" : "No available VEs"}
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
}

export function GroupMessageBubble({ message, participantIndex, theme, isUser }: GroupMessageBubbleProps) {
    const color = isUser ? theme.text : getParticipantColor(participantIndex);

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
            <div
                style={{
                    padding: "8px 12px",
                    borderRadius: 8,
                    background: isUser ? `${theme.sendBtnColor}15` : theme.fieldBg,
                    borderLeft: isUser ? "none" : `3px solid ${color}`,
                    fontSize: 13,
                    color: theme.text,
                    whiteSpace: "pre-wrap",
                    wordBreak: "break-word",
                }}
            >
                {message.content}
            </div>
            {/* Attachments */}
            {message.attachments && message.attachments.length > 0 && (
                <div style={{ marginTop: 4, display: "flex", flexWrap: "wrap", gap: 4, paddingLeft: 4 }}>
                    {message.attachments.map((att, idx) => (
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
                            <span>{att.type === "image" ? "🖼️" : "📄"}</span>
                            <span>{att.filename}</span>
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
            {isZh
                ? `${participantName} 已离线`
                : `${participantName} went offline`}
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
                    {isZh
                        ? `${participants.length} 位参与者`
                        : `${participants.length} participants`}
                </div>
                <ParticipantSelector
                    sessionId={sessionId}
                    currentParticipants={participants}
                    maxGroupParticipants={maxGroupParticipants}
                    theme={theme}
                    lang={lang}
                    onAdd={handleAdd}
                    listVirtualEmployees={listVirtualEmployees}
                />
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
