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

import { useCallback, useEffect, useRef, useState, type CSSProperties } from "react";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import { MessageContentRenderer } from "./MessageContentRenderer";
import type { VirtualEmployeeEntry } from "./VirtualEmployeeTab";
import { safeAvatarDataURL } from "./virtualEmployeeAvatar";
import type { Theme } from "./aiAssistantPanelTheme";
import { looksLikeRawParticipantId } from "./localAIIdentity";
import { participantAddErrorText } from "./participantAddError";
import { addParticipantIdentityKeys, participantIdentityKeys, participantIdentityMatches } from "./participantIdentity";
import { veStatusEventInfo } from "./veStatusEvent";

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
    onAddParticipant?: (veId: string) => Promise<unknown> | unknown;
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
    /** Whether to render the compact participant-count header above messages */
    showHeader?: boolean;
    /** Participant IDs that represent the local user and should align as outgoing messages. */
    localUserIds?: string[];
    /** Optional root layout override for embedding beside other vertical siblings. */
    containerStyle?: CSSProperties;
}

// --- Constants ---

const DEFAULT_MAX_GROUP_PARTICIPANTS = 5;
const MAX_UPPER_LIMIT = 10;
const TAB_TITLE_MAX_LENGTH = 30;

// --- Helpers ---

/** Build a group tab title from participant names, truncated if too long. */
export function buildGroupTabTitle(participants: GroupParticipant[]): string {
    const distinctParticipants = dedupeGroupParticipants(participants);
    if (distinctParticipants.length === 0) return "\u7fa4\u804a";
    const names = distinctParticipants.map((p) => p.name);
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

export function virtualEmployeeParticipantId(ve: Pick<VirtualEmployeeEntry, "id" | "machine_id">): string {
    return String(ve.machine_id || ve.id || "").trim();
}

export function virtualEmployeeDisplayName(
    ve: Pick<VirtualEmployeeEntry, "id" | "machine_id" | "name">,
    index?: number,
    lang?: string,
): string {
    const rawName = String(ve.name || "").trim();
    const rawId = String(ve.id || "").trim();
    const machineId = String(ve.machine_id || "").trim();
    if (rawName && rawName !== rawId && rawName !== machineId && !looksLikeRawParticipantId(rawName)) return rawName;
    const ordinal = typeof index === "number" ? " " + (index + 1) : "";
    return !lang || lang.startsWith("zh") ? "数字员工" + ordinal : "Digital employee" + ordinal;
}


function normalizeParticipantLookupId(value: string): string {
    return String(value || "").trim().toLowerCase();
}

function participantIdentityKeySet(values: unknown[]): Set<string> {
    const out = new Set<string>();
    values.forEach((value) => addParticipantIdentityKeys(out, value));
    return out;
}

function dedupeGroupParticipants(participants: GroupParticipant[]): GroupParticipant[] {
    const seen = new Set<string>();
    const out: GroupParticipant[] = [];
    for (const participant of participants) {
        const before = seen.size;
        addParticipantIdentityKeys(seen, participant.id);
        if (seen.size !== before) out.push(participant);
    }
    return out;
}

function distinctGroupParticipantCount(participants: GroupParticipant[]): number {
    return dedupeGroupParticipants(participants).length;
}

function participantIndexForIdentity(indexMap: Map<string, number>, id: unknown): number | undefined {
    for (const key of participantIdentityKeys(id)) {
        const value = indexMap.get(key);
        if (value !== undefined) return value;
    }
    return undefined;
}

function readableGroupSpeakerName(
    candidate: string | undefined,
    fromId: string | undefined,
    participantName: string | undefined,
    isUser: boolean | undefined,
    lang: string | undefined,
): string {
    const name = String(candidate || "").trim();
    const id = String(fromId || "").trim();
    const participant = String(participantName || "").trim();
    if (name && name !== id && !looksLikeRawParticipantId(name)) return name;
    if (participant && participant !== id && !looksLikeRawParticipantId(participant)) return participant;
    if (isUser) return !lang || lang.startsWith("zh") ? "我" : "Me";
    return !lang || lang.startsWith("zh") ? "数字员工" : "Digital employee";
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
    sessionId?: string;
    currentParticipants: GroupParticipant[];
    maxGroupParticipants: number;
    theme: Theme;
    lang?: string;
    onAdd: (ve: VirtualEmployeeEntry) => Promise<unknown> | unknown;
    listVirtualEmployees?: () => Promise<VirtualEmployeeEntry[]>;
}

export function ParticipantSelector({
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
    const [addingId, setAddingId] = useState("");
    const addingIdRef = useRef("");
    const mountedRef = useRef(true);
    const limitErrorTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const popoverRef = useRef<HTMLDivElement>(null);

    const isZh = !lang || lang.startsWith("zh");

    useEffect(() => {
        mountedRef.current = true;
        return () => {
            mountedRef.current = false;
        };
    }, []);

    // Check if limit is reached
    const currentParticipantCount = distinctGroupParticipantCount(currentParticipants);
    const limitReached = currentParticipantCount >= maxGroupParticipants;

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
            // Filter out digital employees already in the group, including hub-generated ve_<machine> aliases.
            const currentIds = new Set<string>();
            currentParticipants.forEach((p) => addParticipantIdentityKeys(currentIds, p.id));
            const filtered = (all || []).filter((ve) => {
                const keys = participantIdentityKeys(ve.id, ve.machine_id, virtualEmployeeParticipantId(ve));
                return !keys.some((key) => currentIds.has(key)) && ve.online_status === "online";
            });
            if (!mountedRef.current) return;
            setAvailable(filtered);
        } catch {
            if (!mountedRef.current) return;
            setError(isZh ? "\u83b7\u53d6\u5217\u8868\u5931\u8d25" : "Failed to load");
            setAvailable([]);
        } finally {
            if (mountedRef.current) setLoading(false);
        }
    }, [currentParticipants, listVirtualEmployees, isZh]);

    const handleToggle = useCallback(() => {
        if (addingIdRef.current) return;
        if (limitReached) {
            setError(
                isZh
                    ? `\u7fa4\u804a\u4eba\u6570\u5df2\u6ee1\uff08\u6700\u591a ${maxGroupParticipants} \u4eba\uff09`
                    : `Group is full (max ${maxGroupParticipants})`
            );
            if (limitErrorTimerRef.current) clearTimeout(limitErrorTimerRef.current);
            limitErrorTimerRef.current = setTimeout(() => {
                setError("");
                limitErrorTimerRef.current = null;
            }, 3000);
            return;
        }
        if (!open) {
            fetchAvailable();
        }
        setOpen(!open);
    }, [open, limitReached, maxGroupParticipants, isZh, fetchAvailable]);

    useEffect(() => () => {
        if (limitErrorTimerRef.current) clearTimeout(limitErrorTimerRef.current);
    }, []);

    // Close popover on outside click
    useEffect(() => {
        if (!open) return;
        const handler = (e: MouseEvent) => {
            if (addingIdRef.current) return;
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
                disabled={!!addingId}
                style={{
                    border: `1px solid ${theme.divider}`,
                    background: theme.fieldBg,
                    borderRadius: "50%",
                    width: 28,
                    height: 28,
                    cursor: addingId ? "default" : "pointer",
                    opacity: addingId ? 0.6 : 1,
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
                        available.map((ve, index) => {
                            const avatarDataURL = safeAvatarDataURL(ve.avatar_data_url);
                            return (
                            <div
                                key={ve.id}
                                data-testid={`group-picker-item-${ve.id}`}
                                onClick={async () => {
                                    if (addingIdRef.current) return;
                                    addingIdRef.current = ve.id;
                                    setAddingId(ve.id);
                                    setError("");
                                    try {
                                        const result = await onAdd(ve);
                                        if (result === false || result === null) {
                                            throw new Error("participant_add_failed");
                                        }
                                        if (mountedRef.current) setOpen(false);
                                    } catch (err) {
                                        if (mountedRef.current) setError(participantAddErrorText(err, lang));
                                    } finally {
                                        addingIdRef.current = "";
                                        if (mountedRef.current) setAddingId("");
                                    }
                                }}
                                style={{
                                    padding: "6px 12px",
                                    cursor: addingId ? "default" : "pointer",
                                    opacity: addingId && addingId !== ve.id ? 0.55 : 1,
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
                                {avatarDataURL ? (
                                    <img src={avatarDataURL} alt="" style={{ width: 22, height: 22, borderRadius: "50%", objectFit: "cover", flexShrink: 0 }} />
                                ) : (
                                    <span
                                        style={{
                                            width: 6,
                                            height: 6,
                                            borderRadius: "50%",
                                            background: "#22c55e",
                                            flexShrink: 0,
                                        }}
                                    />
                                )}
                                <span>{addingId === ve.id ? (isZh ? "添加中..." : "Adding...") : virtualEmployeeDisplayName(ve, index, lang)}</span>
                            </div>
                            );
                        })}
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
    displayName?: string;
}

export function GroupMessageBubble({ message, participantIndex, theme, isUser, onDownloadAttachment, displayName }: GroupMessageBubbleProps) {
    const color = isUser ? theme.text : getParticipantColor(participantIndex);
    const hasAttachments = !!message.attachments?.length;
    const hasContent = message.content.trim().length > 0;
    const horizontalAlign = isUser ? "flex-end" : "flex-start";

    return (
        <div
            data-testid={`group-msg-${message.id}`}
            style={{ marginBottom: 10, display: "flex", flexDirection: "column", alignItems: horizontalAlign }}
        >
            {/* Participant name label */}
            <div
                data-testid={`group-msg-label-${message.id}`}
                style={{
                    fontSize: 11,
                    fontWeight: 600,
                    color,
                    marginBottom: 2,
                    paddingLeft: isUser ? 0 : 4,
                    paddingRight: isUser ? 4 : 0,
                }}
            >
                {displayName || message.fromName}
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
                    overflowWrap: "anywhere",
                    whiteSpace: "pre-wrap",
                    maxWidth: "82%",
                }}
            >
                <MessageContentRenderer content={message.content} theme={theme} isUser={isUser} />
            </div>}
            {/* Attachments */}
            {hasAttachments && (
                <div style={{ marginTop: 4, display: "flex", flexWrap: "wrap", gap: 4, paddingLeft: isUser ? 0 : 4, paddingRight: isUser ? 4 : 0, justifyContent: horizontalAlign, maxWidth: "82%" }}>
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
                                maxWidth: "100%",
                                minWidth: 0,
                            }}
                        >
                            <span style={{ flexShrink: 0 }}>{att.type === "image" ? "IMG" : att.type === "text" ? "TXT" : "FILE"}</span>
                            {att.fileUrl || att.localPath ? <button type="button" onClick={() => onDownloadAttachment?.(att, message)} title={att.localPath || att.fileUrl} style={{ border: 0, padding: 0, minWidth: 0, maxWidth: "100%", flex: "1 1 auto", display: "inline-flex", alignItems: "center", gap: 5, background: "transparent", color: theme.text, cursor: "pointer", font: "inherit" }}><span style={{ minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", textDecoration: "underline" }}>{att.filename}</span><span style={{ color: theme.textMuted, fontSize: 10, flexShrink: 0 }}>{att.localPath ? "OPEN" : "GET"}</span></button> : <span style={{ minWidth: 0, flex: "1 1 auto", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{att.filename}</span>}
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
    showHeader = true,
    localUserIds,
    containerStyle,
}: VEGroupChatProps) {
    const { maxGroupParticipants } = useGroupConfig(initialMax);
    const [offlineNotices, setOfflineNotices] = useState<string[]>([]);
    const isZh = !lang || lang.startsWith("zh");

    // Update tab title when participants change
    useEffect(() => {
        const distinctParticipants = dedupeGroupParticipants(participants);
        if (onTitleChange && distinctParticipants.length > 0) {
            onTitleChange(buildGroupTabTitle(distinctParticipants));
        }
    }, [participants, onTitleChange]);

    // Listen for ve:status_change to detect participant going offline
    useEffect(() => {
        const handler = (data: any) => {
            const { ids, status } = veStatusEventInfo(data);
            if (status === "offline") {
                const participant = participants.find((p) => ids.some((id) => participantIdentityMatches(p.id, id)));
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
            if (distinctGroupParticipantCount(participants) >= maxGroupParticipants) {
                return false; // ParticipantSelector already shows the error
            }
            try {
                if (addVEToGroup) {
                    await addVEToGroup(sessionId, virtualEmployeeParticipantId(ve));
                    return true;
                } else if (onAddParticipant) {
                    const result = await onAddParticipant(virtualEmployeeParticipantId(ve));
                    return result === false || result === null ? false : true;
                } else {
                    const mod = await import("../../../wailsjs/go/main/App");
                    await (mod as any).AddVEToGroup(sessionId, virtualEmployeeParticipantId(ve));
                    return true;
                }
            } catch (err: any) {
                console.error("Failed to add participant:", err);
                throw err;
            }
        },
        [sessionId, participants, maxGroupParticipants, addVEToGroup, onAddParticipant]
    );

    const participantCount = distinctGroupParticipantCount(participants);

    // Build participant index map for coloring. Normalize ids because Hub history
    // can preserve canonical casing while frontend state may use a local alias.
    const participantIndexMap = new Map<string, number>();
    participants.forEach((p, i) => participantIdentityKeys(p.id).forEach((key) => participantIndexMap.set(key, i)));
    const hasExplicitLocalUserIds = Array.isArray(localUserIds);
    const localUserIdSet = participantIdentityKeySet(localUserIds || []);

    return (
        <div
            data-testid="ve-group-chat-view"
            style={{ display: "flex", flexDirection: "column", height: "100%", ...containerStyle }}
        >
            {showHeader && (
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
                        {isZh ? `${participantCount} \u4f4d\u53c2\u4e0e\u8005` : `${participantCount} participants`}
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
            )}

            {/* Message list */}
            <div
                data-testid="group-message-list"
                style={{ flex: 1, minHeight: 0, overflowY: "auto", padding: "12px 16px" }}
            >
                {messages.map((msg) => {
                    const normalizedFromId = normalizeParticipantLookupId(msg.fromId);
                    const participant = participants.find((p) => participantIdentityMatches(p.id, msg.fromId));
                    const pIdx = participantIndexForIdentity(participantIndexMap, msg.fromId) ?? 0;
                    const isUser = hasExplicitLocalUserIds ? participantIdentityKeys(msg.fromId).some((key) => localUserIdSet.has(key)) : !participant;
                    const displayName = readableGroupSpeakerName(msg.fromName, msg.fromId, participant?.name, isUser, lang);
                    return (
                        <GroupMessageBubble
                            key={msg.id}
                            message={msg}
                            participantIndex={pIdx}
                            theme={theme}
                            isUser={isUser}
                            onDownloadAttachment={onDownloadAttachment}
                            displayName={displayName}
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
