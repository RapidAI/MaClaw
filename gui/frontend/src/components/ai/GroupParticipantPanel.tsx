/**
 * GroupParticipantPanel - Right-side participant list for group chat tabs.
 *
 * Shows:
 * - Participant count header
 * - List of participants with online/offline status indicators
 * - "+ Invite" button to add more participants
 *
 * Designed to be rendered alongside the group chat message area in a flex row.
 */

import { type CSSProperties, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import type { Theme } from "./aiAssistantPanelTheme";
import { ParticipantSelector, useGroupConfig, virtualEmployeeDisplayName, virtualEmployeeParticipantId } from "./VEGroupChat";
import { localAINameForLang, looksLikeRawParticipantId, normalizeParticipantId } from "./localAIIdentity";
import { addParticipantIdentityKeys, participantIdentityKeys } from "./participantIdentity";
import { safeAvatarDataURL } from "./virtualEmployeeAvatar";
import { veStatusEventInfo } from "./veStatusEvent";
import { getWailsAppModule } from "../../utils/wailsAppModule";

export interface Participant {
    id: string;
    name: string;
    online: boolean;
    isLocal?: boolean;
    avatarDataURL?: string;
}

let participantAvatarEmployeeListInFlight: Promise<unknown> | null = null;

function loadParticipantAvatarEmployees(listFn: (() => Promise<unknown>) | undefined): Promise<unknown> {
    if (typeof listFn !== "function") return Promise.resolve([]);
    if (participantAvatarEmployeeListInFlight) return participantAvatarEmployeeListInFlight;
    participantAvatarEmployeeListInFlight = Promise.resolve()
        .then(() => listFn())
        .finally(() => {
            participantAvatarEmployeeListInFlight = null;
        });
    return participantAvatarEmployeeListInFlight;
}


function participantFallbackName(index: number, isZh: boolean): string {
    return isZh ? "\u53c2\u4e0e\u8005 " + (index + 1) : "Participant " + (index + 1);
}

function participantDisplayNameFor(p: Participant, index: number, isZh: boolean, lang?: string): string {
    if (p.isLocal) return localAINameForLang(lang);
    const name = String(p.name || "").trim();
    const id = String(p.id || "").trim();
    if (name && name !== id && !looksLikeRawParticipantId(name)) return name;
    return participantFallbackName(index, isZh);
}

function dedupeParticipants(participants: Participant[]): Participant[] {
    const seen = new Set<string>();
    const out: Participant[] = [];
    for (const participant of participants) {
        const before = seen.size;
        addParticipantIdentityKeys(seen, participant.id);
        if (seen.size !== before) out.push(participant);
    }
    return out;
}

function participantIconStyle(p: Participant, theme: Theme): CSSProperties {
    const isLocal = !!p.isLocal;
    return {
        position: "relative",
        width: 18,
        height: 18,
        borderRadius: 6,
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        flexShrink: 0,
        color: isLocal ? "#4f7f6f" : (theme.btnColor || "#2f5f98"),
        background: isLocal ? "rgba(79, 127, 111, 0.12)" : "rgba(47, 95, 152, 0.10)",
        border: `1px solid ${isLocal ? "rgba(79, 127, 111, 0.26)" : "rgba(47, 95, 152, 0.22)"}`,
    };
}

function ParticipantTypeIcon({ participant, theme }: { participant: Participant; theme: Theme }) {
    const stroke = "currentColor";
    const title = participant.isLocal ? "Local" : "Digital employee";
    const avatarDataURL = !participant.isLocal ? safeAvatarDataURL(participant.avatarDataURL) : "";
    return (
        <span aria-label={title} title={title} style={participantIconStyle(participant, theme)}>
            {avatarDataURL ? (
                <img
                    data-testid={`participant-avatar-${participant.id}`}
                    src={avatarDataURL}
                    alt=""
                    aria-hidden="true"
                    style={{ width: "100%", height: "100%", borderRadius: 5, objectFit: "cover", display: "block" }}
                />
            ) : participant.isLocal ? (
                <svg width="12" height="12" viewBox="0 0 16 16" fill="none" aria-hidden="true" focusable="false">
                    <rect x="3" y="4" width="10" height="8" rx="2" stroke={stroke} strokeWidth="1.4" />
                    <path d="M6 2.5v1.5M10 2.5v1.5M6 12v1.5M10 12v1.5" stroke={stroke} strokeWidth="1.3" strokeLinecap="round" />
                    <path d="M6.3 7h.01M9.7 7h.01M6.2 9.1h3.6" stroke={stroke} strokeWidth="1.4" strokeLinecap="round" />
                </svg>
            ) : (
                <svg width="12" height="12" viewBox="0 0 16 16" fill="none" aria-hidden="true" focusable="false">
                    <circle cx="8" cy="5.2" r="2.4" stroke={stroke} strokeWidth="1.4" />
                    <path d="M3.8 13c.55-2.15 2.1-3.4 4.2-3.4s3.65 1.25 4.2 3.4" stroke={stroke} strokeWidth="1.4" strokeLinecap="round" />
                </svg>
            )}
            <span
                data-testid={`participant-status-${participant.id}`}
                aria-hidden="true"
                style={{
                    position: "absolute",
                    right: -2,
                    bottom: -2,
                    width: 7,
                    height: 7,
                    borderRadius: "50%",
                    background: participant.online ? "#4f7f6f" : "#6b7280",
                    border: `1.5px solid ${theme.titleBarBg}`,
                    boxSizing: "border-box",
                }}
            />
        </span>
    );
}
export interface GroupParticipantPanelProps {
    participants: Participant[];
    theme: Theme;
    lang?: string;
    /** Whether the discussion is view-only. */
    readOnly?: boolean;
    /** Max participants allowed (from Hub config) */
    maxParticipants?: number;
    /** Callback when user clicks invite button */
    onInvite?: () => void;
    /** Callback when a VE is selected from the unified participant panel. */
    onAddParticipant?: (veId: string, veName: string) => Promise<unknown> | unknown;
    /** Session ID for listening to status changes */
    sessionId?: string;
    /** Whether this panel is currently visible. Hidden mounted tabs skip avatar refresh work. */
    active?: boolean;
    /** Callback when user right-clicks "Talk to" on a participant */
    onTalkTo?: (participant: Participant) => void;
}

export function GroupParticipantPanel({
    participants,
    theme,
    lang,
    readOnly = false,
    maxParticipants = 5,
    onInvite,
    onAddParticipant,
    sessionId,
    active = true,
    onTalkTo,
}: GroupParticipantPanelProps) {
    const isZh = !lang || lang.startsWith("zh");
    const { maxGroupParticipants } = useGroupConfig(maxParticipants);
    const participantIdKey = useMemo(() => participants
        .map((p) => normalizeParticipantId(p.id))
        .filter(Boolean)
        .sort()
        .join("\0"), [participants]);
    const participantAliasMap = useMemo(() => {
        const aliases = new Map<string, string>();
        for (const canonical of participantIdKey ? participantIdKey.split("\0") : []) {
            if (!canonical) continue;
            const keys = new Set<string>();
            addParticipantIdentityKeys(keys, canonical);
            for (const key of keys) aliases.set(key, canonical);
        }
        return aliases;
    }, [participantIdKey]);
    const participantIdSet = useMemo(() => new Set(participantAliasMap.keys()), [participantAliasMap]);

    // Online status overlay: tracks status changes from events without
    // duplicating the participants array in state. Key = participant ID, value = online.
    const [statusOverlay, setStatusOverlay] = useState<Record<string, boolean>>({});
    const [participantAvatars, setParticipantAvatars] = useState<Record<string, string>>({});
    const missingAvatarParticipantKey = useMemo(() => participants
        .filter((p) => !p.isLocal && !safeAvatarDataURL(p.avatarDataURL))
        .map((p) => normalizeParticipantId(p.id))
        .filter(Boolean)
        .sort()
        .join("\0"), [participants]);
    const suppliedAvatarParticipantKey = useMemo(() => participants
        .filter((p) => !p.isLocal && safeAvatarDataURL(p.avatarDataURL))
        .map((p) => `${normalizeParticipantId(p.id)}=${safeAvatarDataURL(p.avatarDataURL)}`)
        .filter(Boolean)
        .sort()
        .join("\0"), [participants]);

    // Context menu state
    const [contextMenu, setContextMenu] = useState<{ x: number; y: number; participant: Participant } | null>(null);
    const contextMenuRef = useRef<HTMLDivElement>(null);

    // Close context menu on click outside or Esc
    useEffect(() => {
        if (!contextMenu) return;
        const handleClick = (e: MouseEvent) => {
            if (contextMenuRef.current && !contextMenuRef.current.contains(e.target as Node)) {
                setContextMenu(null);
            }
        };
        const handleEsc = (e: KeyboardEvent) => {
            if (e.key === "Escape") setContextMenu(null);
        };
        document.addEventListener("mousedown", handleClick);
        document.addEventListener("keydown", handleEsc);
        return () => {
            document.removeEventListener("mousedown", handleClick);
            document.removeEventListener("keydown", handleEsc);
        };
    }, [contextMenu]);

    const handleContextMenu = useCallback((e: React.MouseEvent, participant: Participant) => {
        e.preventDefault();
        if (readOnly || !onTalkTo) return;
        // Clamp position to keep menu within viewport
        const menuHeight = 32; // approximate single-item menu height
        const menuWidth = 100;
        const x = Math.min(e.clientX, window.innerWidth - menuWidth - 8);
        const y = Math.min(e.clientY, window.innerHeight - menuHeight - 8);
        setContextMenu({ x, y, participant });
    }, [onTalkTo, readOnly]);

    const handleTalkTo = useCallback(() => {
        if (contextMenu && onTalkTo) {
            onTalkTo(contextMenu.participant);
        }
        setContextMenu(null);
    }, [contextMenu, onTalkTo]);

    // Listen for VE status changes
    useEffect(() => {
        const unsub = EventsOn("ve:status_change", (data: any) => {
            if (!data) return;
            const { ids, status } = veStatusEventInfo(data);
            if (status !== "online" && status !== "offline") return;
            const matched = ids.find((candidate) => participantIdSet.has(candidate));
            if (!matched) return;
            const id = participantAliasMap.get(matched) || matched;
            const online = status === "online";
            setStatusOverlay(prev => {
                if (prev[id] === online) return prev;
                return { ...prev, [id]: online };
            });
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("ve:status_change");
        };
    }, [participantAliasMap, participantIdSet]);

    useEffect(() => {
        setStatusOverlay(prev => {
            let changed = false;
            const next: Record<string, boolean> = {};
            for (const [id, online] of Object.entries(prev)) {
                if (participantAliasMap.has(id)) next[participantAliasMap.get(id) || id] = online;
                else changed = true;
            }
            return changed ? next : prev;
        });
    }, [participantAliasMap]);

    useEffect(() => {
        setParticipantAvatars(prev => {
            let changed = false;
            const next: Record<string, string> = {};
            for (const [id, avatar] of Object.entries(prev)) {
                if (participantAliasMap.has(id)) next[id] = avatar;
                else changed = true;
            }
            return changed ? next : prev;
        });
    }, [participantAliasMap]);

    useEffect(() => {
        let cancelled = false;
        if (!active) return () => { cancelled = true; };
        setParticipantAvatars(prev => {
            let changed = false;
            const next = { ...prev };
            for (const participant of participants) {
                if (participant.isLocal) continue;
                const avatar = safeAvatarDataURL(participant.avatarDataURL);
                if (!avatar) continue;
                for (const id of participantIdentityKeys(participant.id)) {
                    if (next[id] === avatar) continue;
                    next[id] = avatar;
                    changed = true;
                }
            }
            return changed ? next : prev;
        });
        if (!missingAvatarParticipantKey) {
            return () => { cancelled = true; };
        }
        getWailsAppModule()
            .then(async (mod) => {
                const listFn = (mod as any).ListVirtualEmployees;
                const rawEmployees = await loadParticipantAvatarEmployees(listFn);
                const employees = Array.isArray(rawEmployees) ? rawEmployees : [];
                if (cancelled) return;
                const avatarsById: Record<string, string> = {};
                for (const ve of employees || []) {
                    const avatar = safeAvatarDataURL(ve?.avatar_data_url);
                    if (!avatar) continue;
                    const keys = new Set<string>();
                    for (const id of [ve?.id, ve?.machine_id, virtualEmployeeParticipantId(ve)]) {
                        addParticipantIdentityKeys(keys, id);
                    }
                    for (const key of keys) {
                        avatarsById[key] = avatar;
                    }
                }
                setParticipantAvatars(prev => {
                    let changed = false;
                    const next = { ...prev };
                    for (const [id, avatar] of Object.entries(avatarsById)) {
                        if (next[id] === avatar) continue;
                        next[id] = avatar;
                        changed = true;
                    }
                    return changed ? next : prev;
                });
            })
            .catch(() => {
                // Keep the last good avatar set on transient backend failures to avoid UI flicker.
            });
        return () => { cancelled = true; };
    }, [active, missingAvatarParticipantKey, suppliedAvatarParticipantKey]);

    // Merge prop data with status/avatar overlays without mutating the participant shape.
    const resolvedParticipants = useMemo(() => participants.map(p => {
        const normalizedId = normalizeParticipantId(p.id);
        const aliases = participantIdentityKeys(p.id);
        const avatarDataURL = safeAvatarDataURL(p.avatarDataURL) || aliases.map((id) => participantAvatars[id]).find(Boolean) || "";
        const overlayKey = aliases.find((id) => statusOverlay[id] !== undefined) || normalizedId;
        const participant = { ...p };
        delete participant.avatarDataURL;
        return {
            ...participant,
            ...(avatarDataURL ? { avatarDataURL } : {}),
            online: statusOverlay[overlayKey] !== undefined ? statusOverlay[overlayKey] : p.online,
        };
    }), [participants, participantAvatars, statusOverlay]);
    const visibleParticipants = useMemo(() => dedupeParticipants(resolvedParticipants), [resolvedParticipants]);

    const limitReached = visibleParticipants.length >= maxGroupParticipants;


    return (
        <div
            data-testid="group-participant-panel"
            style={{
                width: 140,
                minWidth: 140,
                borderLeft: `1px solid ${theme.divider}`,
                display: "flex",
                flexDirection: "column",
                background: theme.titleBarBg,
                overflow: "hidden",
            }}
        >
            {/* Header */}
            <div style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                gap: 6,
                padding: "8px 10px",
                fontSize: 11,
                fontWeight: 600,
                color: theme.textMuted,
                borderBottom: `1px solid ${theme.divider}`,
            }}>
                <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                    {isZh ? `\u53c2\u4e0e\u8005 (${visibleParticipants.length})` : `Participants (${visibleParticipants.length})`}
                </span>
                {readOnly && (
                    <span style={{ flexShrink: 0, border: `1px solid ${theme.divider}`, borderRadius: 4, padding: "1px 4px", fontSize: 10, fontWeight: 500, color: theme.textMuted }}>
                        {isZh ? "\u53ea\u8bfb" : "Read-only"}
                    </span>
                )}
            </div>

            {/* Participant list */}
            <div style={{
                flex: 1,
                overflowY: "auto",
                padding: "4px 0",
            }}>
                {visibleParticipants.map((p, index) => {
                    const displayName = participantDisplayNameFor(p, index, isZh, lang);
                    return (
                    <div
                        key={p.id}
                        onContextMenu={(e) => handleContextMenu(e, p)}
                        style={{
                            display: "flex",
                            alignItems: "center",
                            gap: 6,
                            padding: "5px 10px",
                            fontSize: 12,
                            color: theme.text,
                            cursor: !readOnly && onTalkTo ? "context-menu" : "default",
                        }}
                        title={displayName}
                    >
                        <ParticipantTypeIcon participant={p} theme={theme} />
                        {/* Name */}
                        <span style={{
                            overflow: "hidden",
                            textOverflow: "ellipsis",
                            whiteSpace: "nowrap",
                            flex: 1,
                        }}>
                            {displayName}
                        </span>
                    </div>
                    );
                })}

                {visibleParticipants.length === 0 && (
                    <div style={{
                        padding: "12px 10px",
                        fontSize: 11,
                        color: theme.textMuted,
                        textAlign: "center",
                    }}>
                        {isZh ? "\u6682\u65e0\u53c2\u4e0e\u8005" : "No participants"}
                    </div>
                )}
            </div>

            {/* Invite button */}
            {(onInvite || onAddParticipant) && !readOnly && (
                <div style={{
                    padding: "6px 10px",
                    borderTop: `1px solid ${theme.divider}`,
                }}>
                    {onAddParticipant ? (
                        <ParticipantSelector
                            sessionId={sessionId}
                            currentParticipants={visibleParticipants}
                            maxGroupParticipants={maxGroupParticipants}
                            theme={theme}
                            lang={lang}
                            onAdd={(ve) => onAddParticipant(virtualEmployeeParticipantId(ve), virtualEmployeeDisplayName(ve, undefined, lang))}
                        />
                    ) : <button
                        data-testid="group-panel-invite-btn"
                        onClick={onInvite}
                        disabled={limitReached}
                        style={{
                            width: "100%",
                            padding: "4px 8px",
                            fontSize: 11,
                            border: `1px solid ${theme.divider}`,
                            borderRadius: 4,
                            background: limitReached ? "transparent" : theme.fieldBg,
                            color: limitReached ? theme.textMuted : theme.btnColor,
                            cursor: limitReached ? "not-allowed" : "pointer",
                            opacity: limitReached ? 0.5 : 1,
                        }}
                        title={limitReached
                            ? (isZh ? `\u5df2\u8fbe\u4e0a\u9650 (${maxGroupParticipants})` : `Limit reached (${maxGroupParticipants})`)
                            : (isZh ? "\u9080\u8bf7\u6570\u5b57\u5458\u5de5" : "Invite")
                        }
                    >
                        {isZh ? "+ \u9080\u8bf7" : "+ Invite"}
                    </button>}
                </div>
            )}

            {/* Context Menu */}
            {contextMenu && (
                <div
                    ref={contextMenuRef}
                    data-testid="participant-context-menu"
                    style={{
                        position: "fixed",
                        left: contextMenu.x,
                        top: contextMenu.y,
                        zIndex: 9999,
                        background: theme.fieldBg || "#0f1720",
                        border: `1px solid ${theme.divider || "#263447"}`,
                        borderRadius: 4,
                        boxShadow: "0 2px 8px rgba(0,0,0,0.3)",
                        padding: "2px 0",
                        minWidth: 100,
                    }}
                >
                    <div
                        data-testid="context-menu-talk-to"
                        onClick={handleTalkTo}
                        style={{
                            padding: "6px 12px",
                            fontSize: 12,
                            color: theme.text,
                            cursor: "pointer",
                        }}
                        onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.background = (theme.sendBtnBg || "#2f5f98") + "20"; }}
                        onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.background = ""; }}
                    >
                        {isZh ? "\u4e0e\u5b83\u4ea4\u8c08" : "Talk to"}
                    </div>
                </div>
            )}
        </div>
    );
}
