import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { BrowserOpenURL } from "../../../wailsjs/runtime";
import { windowNoDragRegionProps } from "../../utils/windowDrag";
import { localizeText } from "../../i18n";
import type { PinnedMessage } from "../ai/usePinnedNotifications";

/**
 * PinnedMessageBanner — the 置顶消息 floating card above the sidebar status
 * row (the account / bell line). Shows up to four slides: the permanent pin
 * leftmost, then the latest three regular pinned messages, navigable with the
 * arrows and the dots. The close button appears on hover and hides every
 * visible message for the current session only — the next app launch shows
 * the banner again.
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
    const [index, setIndex] = useState(0);
    const [pos, setPos] = useState<{ left: number; bottom: number; width: number } | null>(null);
    const rafRef = useRef<number | null>(null);

    // The slides can shrink between renders; keep the index inside the range.
    const safeIndex = Math.min(index, Math.max(0, messages.length - 1));
    const current = messages[safeIndex];

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

    useEffect(() => {
        // Clamp when the slide list shrinks (a revoke removes a row).
        if (index >= messages.length) setIndex(Math.max(0, messages.length - 1));
    }, [messages.length, index]);

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

    const prevLabel = localizeText(lang, 'Previous pinned message', '上一条置顶消息', '上一條置頂消息');
    const nextLabel = localizeText(lang, 'Next pinned message', '下一条置顶消息', '下一條置頂消息');
    const closeLabel = localizeText(lang, 'Close pinned messages (shown again next launch)', '关闭置顶消息（下次启动仍会显示）', '關閉置頂消息（下次啟動仍會顯示）');
    const buttonLabel = current.button_text || localizeText(lang, 'View details', '查看详情', '查看詳情');

    return (
        <div
            className="mc-pinned-banner"
            data-testid="pinned-message-banner"
            data-open={pos ? 'true' : 'false'}
            // The positioning style rides inside windowNoDragRegionProps so the
            // card is never a window-drag region (and styles stay merged).
            {...windowNoDragRegionProps(posStyle)}
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
                    <span className="mc-pinned-banner__title-text">{current.title}</span>
                </div>
                {current.content ? (
                    <div className="mc-pinned-banner__content">{current.content}</div>
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
                            aria-label={message.title || String(i + 1)}
                            className={`mc-pinned-banner__dot${i === safeIndex ? ' is-active' : ''}`}
                            onClick={() => setIndex(i)}
                        />
                    ))}
                </div>
                <div className="mc-pinned-banner__nav">
                    {messages.length > 1 ? (
                        <>
                            <button
                                type="button"
                                className="mc-pinned-banner__arrow"
                                aria-label={prevLabel}
                                title={prevLabel}
                                disabled={safeIndex === 0}
                                onClick={() => setIndex(Math.max(0, safeIndex - 1))}
                            >
                                <svg width="10" height="10" viewBox="0 0 14 14" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                                    <path d="M9 2L4 7l5 5" />
                                </svg>
                            </button>
                            <button
                                type="button"
                                className="mc-pinned-banner__arrow"
                                aria-label={nextLabel}
                                title={nextLabel}
                                disabled={safeIndex === messages.length - 1}
                                onClick={() => setIndex(Math.min(messages.length - 1, safeIndex + 1))}
                            >
                                <svg width="10" height="10" viewBox="0 0 14 14" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                                    <path d="M5 2l5 5-5 5" />
                                </svg>
                            </button>
                        </>
                    ) : null}
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
