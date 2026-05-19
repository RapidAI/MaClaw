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

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import type { Theme } from "./aiAssistantPanelTheme";
import { ParticipantSelector, useGroupConfig, virtualEmployeeDisplayName, virtualEmployeeParticipantId } from "./VEGroupChat";
import { localAINameForLang, looksLikeRawParticipantId } from "./localAIIdentity";

export interface Participant {
    id: string;
    name: string;
    online: boolean;
    isLocal?: boolean;
}


function participantFallbackName(index: number, isZh: boolean): string {
    return isZh ? "参与者 " + (index + 1) : "Participant " + (index + 1);
}

function participantDisplayNameFor(p: Participant, index: number, isZh: boolean, lang?: string): string {
    if (p.isLocal) return localAINameForLang(lang);
    const name = String(p.name || "").trim();
    const id = String(p.id || "").trim();
    if (name && name !== id && !looksLikeRawParticipantId(name)) return name;
    return participantFallbackName(index, isZh);
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
    onTalkTo,
}: GroupParticipantPanelProps) {
    const isZh = !lang || lang.startsWith("zh");
    const { maxGroupParticipants } = useGroupConfig(maxParticipants);
    const participantIdSet = useMemo(() => new Set(participants.map((p) => p.id)), [participants]);

    // Online status overlay: tracks status changes from events without
    // duplicating the participants array in state. Key = participant ID, value = online.
    const [statusOverlay, setStatusOverlay] = useState<Record<string, boolean>>({});

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
            const id = String(data.ve_id || data.id || "").trim();
            if (!id || !participantIdSet.has(id)) return;
            const online = data.online_status === "online";
            setStatusOverlay(prev => {
                if (prev[id] === online) return prev;
                return { ...prev, [id]: online };
            });
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("ve:status_change");
        };
    }, [participantIdSet]);

    useEffect(() => {
        setStatusOverlay(prev => {
            let changed = false;
            const next: Record<string, boolean> = {};
            for (const [id, online] of Object.entries(prev)) {
                if (participantIdSet.has(id)) next[id] = online;
                else changed = true;
            }
            return changed ? next : prev;
        });
    }, [participantIdSet]);

    // Merge prop data with status overlay
    const resolvedParticipants = participants.map(p => ({
        ...p,
        online: statusOverlay[p.id] !== undefined ? statusOverlay[p.id] : p.online,
    }));

    const limitReached = resolvedParticipants.length >= maxGroupParticipants;


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
                    {isZh ? `参与者 (${resolvedParticipants.length})` : `Participants (${resolvedParticipants.length})`}
                </span>
                {readOnly && (
                    <span style={{ flexShrink: 0, border: `1px solid ${theme.divider}`, borderRadius: 4, padding: "1px 4px", fontSize: 10, fontWeight: 500, color: theme.textMuted }}>
                        {isZh ? "只读" : "Read-only"}
                    </span>
                )}
            </div>

            {/* Participant list */}
            <div style={{
                flex: 1,
                overflowY: "auto",
                padding: "4px 0",
            }}>
                {resolvedParticipants.map((p, index) => {
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
                        {/* Online indicator */}
                        <span style={{
                            width: 7,
                            height: 7,
                            borderRadius: "50%",
                            background: p.online ? "#22c55e" : "#6b7280",
                            flexShrink: 0,
                        }} />
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

                {resolvedParticipants.length === 0 && (
                    <div style={{
                        padding: "12px 10px",
                        fontSize: 11,
                        color: theme.textMuted,
                        textAlign: "center",
                    }}>
                        {isZh ? "暂无参与者" : "No participants"}
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
                            currentParticipants={resolvedParticipants}
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
                            ? (isZh ? `已达上限 (${maxGroupParticipants})` : `Limit reached (${maxGroupParticipants})`)
                            : (isZh ? "邀请数字员工" : "Invite")
                        }
                    >
                        {isZh ? "+ 邀请" : "+ Invite"}
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
                        background: theme.fieldBg || "#1e1e2e",
                        border: `1px solid ${theme.divider || "#333"}`,
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
                        onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.background = (theme.sendBtnBg || "#3b82f6") + "20"; }}
                        onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.background = ""; }}
                    >
                        {isZh ? "与它交谈" : "Talk to"}
                    </div>
                </div>
            )}
        </div>
    );
}
