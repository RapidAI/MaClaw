/**
 * MentionPopover: @mention participant selector popover.
 *
 * Fully controlled component: parent owns selectedIndex and filtered list.
 * Popover only renders and reports user interactions (click, mouse hover).
 *
 * Supports:
 * - Filtered participant list display
 * - Keyboard navigation (handled by parent via useMentionKeyboard hook)
 * - Click selection
 * - Click-outside dismissal
 * - Mouse hover updates selectedIndex via onHover callback
 */

import { useCallback, useEffect, useRef } from "react";
import type { RefObject } from "react";
import type { Theme } from "./aiAssistantPanelTheme";

export interface MentionParticipant {
    id: string;
    name: string;
    online: boolean;
}

export interface MentionPopoverProps {
    /** Pre-filtered participant list (filtering done by parent) */
    filtered: MentionParticipant[];
    /** Currently highlighted index (controlled by parent) */
    selectedIndex: number;
    /** Called when a participant is selected (click) */
    onSelect: (participant: MentionParticipant) => void;
    /** Called when mouse hovers over an item (parent updates selectedIndex) */
    onHover: (index: number) => void;
    /** Called when the popover should close without selection */
    onClose: () => void;
    /** Anchor element for click-outside detection */
    anchorRef: RefObject<HTMLTextAreaElement | null>;
    /** Theme */
    theme: Theme;
    /** Language */
    lang?: string;
}

export function MentionPopover({
    filtered,
    selectedIndex,
    onSelect,
    onHover,
    onClose,
    anchorRef,
    theme,
    lang,
}: MentionPopoverProps) {
    const isZh = !lang || lang.startsWith("zh");
    const popoverRef = useRef<HTMLDivElement>(null);
    const onCloseRef = useRef(onClose);
    onCloseRef.current = onClose;

    // Keep the outside-click listener stable while still calling the latest onClose.
    useEffect(() => {
        const handleClickOutside = (e: MouseEvent) => {
            if (
                popoverRef.current &&
                !popoverRef.current.contains(e.target as Node) &&
                anchorRef.current &&
                !anchorRef.current.contains(e.target as Node)
            ) {
                onCloseRef.current();
            }
        };
        document.addEventListener("mousedown", handleClickOutside);
        return () => document.removeEventListener("mousedown", handleClickOutside);
    }, [anchorRef]);

    // Scroll selected item into view.
    useEffect(() => {
        if (!popoverRef.current) return;
        const items = popoverRef.current.querySelectorAll("[data-mention-item]");
        if (items[selectedIndex]) {
            items[selectedIndex].scrollIntoView({ block: "nearest" });
        }
    }, [selectedIndex]);

    return (
        <div
            ref={popoverRef}
            data-testid="mention-popover"
            style={{
                position: "absolute",
                bottom: "100%",
                left: 0,
                marginBottom: 4,
                zIndex: 1000,
                minWidth: 160,
                maxWidth: 240,
                maxHeight: 200,
                overflowY: "auto",
                background: theme.fieldBg || "#1e1e2e",
                border: `1px solid ${theme.divider || "#333"}`,
                borderRadius: 6,
                boxShadow: "0 4px 12px rgba(0,0,0,0.3)",
                padding: "4px 0",
            }}
        >
            {filtered.length === 0 ? (
                <div
                    style={{
                        padding: "8px 12px",
                        fontSize: 12,
                        color: theme.textMuted || "#888",
                        textAlign: "center",
                    }}
                >
                    {isZh ? "无匹配参与者" : "No matching participants"}
                </div>
            ) : (
                filtered.map((p, idx) => (
                    <div
                        key={p.id}
                        data-testid={`mention-item-${p.id}`}
                        data-mention-item=""
                        onClick={() => onSelect(p)}
                        onMouseEnter={() => onHover(idx)}
                        style={{
                            display: "flex",
                            alignItems: "center",
                            gap: 8,
                            padding: "6px 12px",
                            fontSize: 12,
                            color: theme.text || "#e0e0e0",
                            background: idx === selectedIndex
                                ? (theme.sendBtnBg || "#3b82f6") + "20"
                                : "transparent",
                            cursor: "pointer",
                            transition: "background 0.1s",
                        }}
                    >
                        {/* Online indicator */}
                        <span
                            style={{
                                width: 7,
                                height: 7,
                                borderRadius: "50%",
                                background: p.online ? "#22c55e" : "#6b7280",
                                flexShrink: 0,
                            }}
                        />
                        {/* Name */}
                        <span
                            style={{
                                overflow: "hidden",
                                textOverflow: "ellipsis",
                                whiteSpace: "nowrap",
                                flex: 1,
                            }}
                        >
                            {p.name}
                        </span>
                    </div>
                ))
            )}
        </div>
    );
}

// --- Hook: useMentionKeyboard ---

export type MentionKeyDownHandler = (e: React.KeyboardEvent) => boolean;

/**
 * Hook for mention popover keyboard navigation.
 * Parent calls the returned handler in textarea's onKeyDown.
 * Returns true if the event was consumed (parent should not process further).
 *
 * Uses refs internally to avoid stale closures: the returned handler
 * always reads the latest values of filtered/selectedIndex/onSelect/onClose.
 */
export function useMentionKeyboard(
    active: boolean,
    filtered: MentionParticipant[],
    selectedIndex: number,
    setSelectedIndex: (idx: number | ((prev: number) => number)) => void,
    onSelect: (p: MentionParticipant) => void,
    onClose: () => void
): MentionKeyDownHandler {
    const activeRef = useRef(active);
    activeRef.current = active;
    const filteredRef = useRef(filtered);
    filteredRef.current = filtered;
    const selectedIndexRef = useRef(selectedIndex);
    selectedIndexRef.current = selectedIndex;
    const onSelectRef = useRef(onSelect);
    onSelectRef.current = onSelect;
    const onCloseRef = useRef(onClose);
    onCloseRef.current = onClose;

    // Stable handler: reads latest values from refs.
    return useCallback(
        (e: React.KeyboardEvent): boolean => {
            if (!activeRef.current) return false;
            const f = filteredRef.current;
            const idx = selectedIndexRef.current;
            if (e.key === "ArrowDown") {
                e.preventDefault();
                setSelectedIndex((prev: number) => (prev + 1) % Math.max(f.length, 1));
                return true;
            }
            if (e.key === "ArrowUp") {
                e.preventDefault();
                setSelectedIndex((prev: number) => (prev - 1 + f.length) % Math.max(f.length, 1));
                return true;
            }
            if (e.key === "Enter") {
                e.preventDefault();
                if (f.length > 0 && idx < f.length) {
                    onSelectRef.current(f[idx]);
                }
                return true;
            }
            if (e.key === "Escape") {
                e.preventDefault();
                onCloseRef.current();
                return true;
            }
            return false;
        },
        [setSelectedIndex]
    );
}
