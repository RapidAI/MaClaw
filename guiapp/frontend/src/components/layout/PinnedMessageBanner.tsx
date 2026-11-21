import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { BrowserOpenURL } from "../../../wailsjs/runtime";
import { windowNoDragRegionProps } from "../../utils/windowDrag";
import { localizeText } from "../../i18n";
import { pickNotificationText } from "../../i18n/notificationLocale";
import type { PinnedMessage } from "../ai/usePinnedNotifications";

/** How long one pinned slide stays up before the next one to its right. */
export const PINNED_BANNER_SLIDE_MS = 10_000;

/**
 * PinnedMessageBanner — the 置顶消息 floating card above the sidebar status
 * row (the account / bell line). Shows up to four slides: the permanent pin
 * leftmost, then the latest three regular pinned messages. With more than one
 * slide the card advances left to right every 10 seconds and wraps. The clock
 * follows the message id, recorded on the same render that shows the slide,
 * so a pin inserted ahead of it does not replace the one on screen. A list
 * refresh that keeps the same ids does not restart the remaining time. The
 * clock runs only while the card is on screen: hovering it, keyboard-focusing
 * a control on it, or opening the account popover (which hides this card)
 * holds the current slide. A pointer click does not. Leaving the card, or
 * the pointer leaving the window, starts a fresh 10 seconds. The dots still
 * jump by hand. The close button appears on hover and hides every visible
 * message for the current session only — the next app launch shows the
 * banner again.
 *
 * Fixed positioning is required because the sidebar scroll wrapper clips
 * absolute overlays (same reason the status and devices popovers are fixed).
 * The card anchors to the status row: its edges align with the row and it
 * floats 8px above it.
 */
export function PinnedMessageBanner({
    messages,
    lang,
    anchorRef,
    onDismiss,
}: {
    messages: PinnedMessage[];
    lang: string;
    /** The sidebar status row (.mc-workbench-status-row) the card floats above. */
    anchorRef: React.RefObject<HTMLDivElement | null>;
    /** Hides every visible message until the app restarts. */
    onDismiss: () => void;
}) {
    const [activeId, setActiveId] = useState<string | null>(null);
    const [hovered, setHovered] = useState(false);
    const [focused, setFocused] = useState(false);
    // The account popover hides this card with visibility. Time spent hidden
    // is not part of the 10 seconds the reader actually sees.
    const [covered, setCovered] = useState(false);
    const [pos, setPos] = useState<{ left: number; bottom: number; width: number } | null>(null);
    const rafRef = useRef<number | null>(null);
    const cardRef = useRef<HTMLDivElement | null>(null);
    // A click focuses a dot or the button. That pointer focus must not keep
    // the clock stopped after the pointer leaves; keyboard focus still does.
    const pointerFocusRef = useRef(false);
    // The clock reads the latest list without treating a new array of the
    // same ids as a reason to restart the remaining 10 seconds.
    const messagesRef = useRef(messages);
    messagesRef.current = messages;

    // An unrecorded slide is index 0. A pin published ahead of it would then
    // replace the message on screen. Adopt that id during this render, before
    // the clock effect, and adopt the first slide again when the current id
    // has left the list.
    const recorded = activeId != null && messages.some((message) => message.id === activeId);
    const shownId = recorded ? activeId : (messages[0]?.id ?? null);
    if (shownId !== activeId) setActiveId(shownId);

    const matched = shownId == null ? -1 : messages.findIndex((message) => message.id === shownId);
    const safeIndex = matched >= 0 ? matched : 0;
    const current = messages[safeIndex];
    const rotationHeld = hovered || focused || covered;
    const messageIds = messages.map((message) => message.id).join('\0');

    useEffect(() => {
        const list = messagesRef.current;
        if (list.length <= 1 || rotationHeld) return;
        const from = list.findIndex((message) => message.id === activeId);
        const start = from >= 0 ? from : 0;
        const next = list[(start + 1) % list.length];
        if (!next) return;
        const timer = window.setTimeout(() => setActiveId(next.id), PINNED_BANNER_SLIDE_MS);
        return () => window.clearTimeout(timer);
    }, [rotationHeld, activeId, messageIds]);

    useEffect(() => {
        const el = cardRef.current;
        if (!el) return;
        const scope = el.closest('.sidebar-system-status');
        if (!scope) return;
        const sync = () => {
            const open = scope.querySelector(".mc-workbench-status-popover[data-open='true']") != null;
            setCovered(open);
            // The card can reappear under a pointer that never re-entered it
            // (the popover took pointer-events away). :hover still matches.
            if (!open && el.matches(':hover')) setHovered(true);
        };
        sync();
        const obs = new MutationObserver(sync);
        obs.observe(scope, { attributes: true, subtree: true, attributeFilter: ['data-open'] });
        return () => obs.disconnect();
    }, [messages.length]);

    useEffect(() => {
        const clearPointerFocus = () => { pointerFocusRef.current = false; };
        window.addEventListener('pointerup', clearPointerFocus);
        window.addEventListener('pointercancel', clearPointerFocus);
        return () => {
            window.removeEventListener('pointerup', clearPointerFocus);
            window.removeEventListener('pointercancel', clearPointerFocus);
        };
    }, []);

    useEffect(() => {
        // mouseleave on the card can miss a pointer that exits the window.
        // Listening on the document for a bubbling mouseout is wrong too: that
        // event can report no relatedTarget while the pointer is still inside
        // the card. The listener stays up for the life of the card so a window
        // exit in the same turn as entering is not missed.
        const onLeaveWindow = () => setHovered(false);
        document.documentElement.addEventListener('mouseleave', onLeaveWindow);
        return () => document.documentElement.removeEventListener('mouseleave', onLeaveWindow);
    }, []);

    const updatePosition = useCallback(() => {
        const anchor = anchorRef.current;
        if (!anchor) {
            setPos(null);
            return;
        }
        const rect = anchor.getBoundingClientRect();
        const width = Math.max(160, Math.round(rect.width));
        const left = Math.min(
            Math.max(Math.round(rect.left), 8),
            Math.max(8, Math.round(window.innerWidth - width - 8)),
        );
        const bottom = Math.round(window.innerHeight - rect.top + 8);
        const next = { left, bottom, width };
        setPos((prev) => (prev && prev.left === next.left && prev.bottom === next.bottom && prev.width === next.width ? prev : next));
    }, [anchorRef]);

    const scheduleReposition = useCallback(() => {
        if (rafRef.current != null) return;
        rafRef.current = requestAnimationFrame(() => {
            rafRef.current = null;
            updatePosition();
        });
    }, [updatePosition]);

    useLayoutEffect(() => {
        updatePosition();
    }, [updatePosition]);

    useEffect(() => {
        if (messages.length === 0) return;
        window.addEventListener('resize', scheduleReposition);
        window.addEventListener('scroll', scheduleReposition, true);
        return () => {
            window.removeEventListener('resize', scheduleReposition);
            window.removeEventListener('scroll', scheduleReposition, true);
            if (rafRef.current != null) {
                cancelAnimationFrame(rafRef.current);
                rafRef.current = null;
            }
        };
    }, [messages.length, scheduleReposition]);

    const openLink = useCallback(() => {
        if (!current?.link_url) return;
        try {
            BrowserOpenURL(current.link_url);
        } catch {
            window.open(current.link_url, '_blank', 'noopener,noreferrer');
        }
    }, [current]);

    if (!current || messages.length === 0) return null;

    const posStyle: React.CSSProperties = pos ? {
        position: 'fixed',
        left: pos.left,
        bottom: pos.bottom,
        width: pos.width,
    } : {
        position: 'fixed',
        left: 0,
        top: 0,
        visibility: 'hidden',
        pointerEvents: 'none',
    };

    const text = pickNotificationText(lang, current);
    const closeLabel = localizeText(lang, 'Close pinned messages (shown again next launch)', '关闭置顶消息（下次启动仍会显示）', '關閉置頂消息（下次啟動仍會顯示）');
    const buttonLabel = text.button_text || localizeText(lang, 'View details', '查看详情', '查看詳情');

    return (
        <div
            ref={cardRef}
            className="mc-pinned-banner"
            data-testid="pinned-message-banner"
            data-open={pos ? 'true' : 'false'}
            // The positioning style rides inside windowNoDragRegionProps so the
            // card is never a window-drag region (and styles stay merged).
            {...windowNoDragRegionProps(posStyle)}
            onMouseEnter={() => setHovered(true)}
            onMouseLeave={() => setHovered(false)}
            onPointerDown={() => { pointerFocusRef.current = true; }}
            onFocus={() => {
                if (pointerFocusRef.current) {
                    pointerFocusRef.current = false;
                    setFocused(false);
                    return;
                }
                setFocused(true);
            }}
            onBlur={(event) => {
                const next = event.relatedTarget;
                if (next instanceof Node && event.currentTarget.contains(next)) return;
                setFocused(false);
            }}
        >
            <button
                type="button"
                className="mc-pinned-banner__close"
                aria-label={closeLabel}
                title={closeLabel}
                onClick={onDismiss}
            >
                <svg width="11" height="11" viewBox="0 0 14 14" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" aria-hidden="true">
                    <path d="M3 3l8 8M11 3l-8 8" />
                </svg>
            </button>
            <div className="mc-pinned-banner__body">
                <div className="mc-pinned-banner__title">
                    {current.pinned_forever ? <span className="mc-pinned-banner__forever" aria-hidden="true" /> : null}
                    <span className="mc-pinned-banner__title-text">{text.title}</span>
                </div>
                {text.content ? (
                    <div className="mc-pinned-banner__content">{text.content}</div>
                ) : null}
            </div>
            <div className="mc-pinned-banner__footer">
                <div className="mc-pinned-banner__dots" role="tablist" aria-label={localizeText(lang, 'Pinned messages', '置顶消息', '置頂消息')}>
                    {messages.map((message, i) => (
                        <button
                            key={message.id}
                            type="button"
                            role="tab"
                            aria-selected={i === safeIndex}
                            aria-label={pickNotificationText(lang, message).title || String(i + 1)}
                            className={`mc-pinned-banner__dot${i === safeIndex ? ' is-active' : ''}`}
                            onClick={() => setActiveId(message.id)}
                        />
                    ))}
                </div>
                <div className="mc-pinned-banner__nav">
                    {current.link_url ? (
                        <button type="button" className="mc-pinned-banner__cta" onClick={openLink}>
                            {buttonLabel}
                        </button>
                    ) : null}
                </div>
            </div>
        </div>
    );
}
