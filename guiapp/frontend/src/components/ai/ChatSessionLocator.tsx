import { memo, useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type WheelEvent } from "react";
import type { Theme } from "./aiAssistantPanelTheme";
import { localizeText } from "./aiAssistantI18n";
import { locatorCssEscape, locatorWheelTargetsTranscript, type ChatSessionLocatorTurn } from "./chatSessionLocatorModel";
import "./ChatSessionLocator.css";

/**
 * Layout Y of the topic line. offsetTop stays in the nav's own pixels, so a
 * transformed app-scale-layer does not push the line down the rail.
 */
function locatorCurrentTop(nav: HTMLElement, list: HTMLElement, row: HTMLElement): number {
    const parent = row.offsetParent;
    if (parent === nav || parent === list) {
        const origin = parent === nav ? 0 : list.offsetTop;
        return origin + row.offsetTop - list.scrollTop + row.offsetHeight / 2;
    }
    const navBox = nav.getBoundingClientRect();
    const rowBox = row.getBoundingClientRect();
    const scale = nav.offsetHeight > 0 ? navBox.height / nav.offsetHeight : 1;
    return (rowBox.top - navBox.top + rowBox.height / 2) / (scale || 1);
}

/** The tick window in the same layout pixels as locatorCurrentTop. */
function locatorTopicVisible(nav: HTMLElement, list: HTMLElement, center: number): boolean {
    if (list.offsetParent === nav) {
        return center >= list.offsetTop && center <= list.offsetTop + list.clientHeight;
    }
    const navBox = nav.getBoundingClientRect();
    const listBox = list.getBoundingClientRect();
    const scale = nav.offsetHeight > 0 ? navBox.height / nav.offsetHeight : 1;
    const top = (listBox.top - navBox.top) / (scale || 1);
    const height = listBox.height / (scale || 1);
    return center >= top && center <= top + height;
}

function pointerStillOnRail(nav: HTMLElement | null, list: HTMLElement | null, next: EventTarget | null): boolean {
    if (next instanceof Node && nav?.contains(next)) return true;
    // Focus on the list itself is only the Escape/tab anchor. It must not keep
    // the ticks highlighted after the pointer leaves. A focused turn does.
    const active = document.activeElement;
    return !!list && active instanceof HTMLElement && active !== list && list.contains(active);
}

/** Puts the single topic line on the tick the pointer or focus is on. */
function placeLocatorCurrent(nav: HTMLElement | null, list: HTMLElement | null, activeId: string | null): void {
    if (!nav) return;
    if (!activeId || !list) {
        nav.style.removeProperty("--csl-current-top");
        delete nav.dataset.currentVisible;
        return;
    }
    const row = list.querySelector(`[data-turn-id="${locatorCssEscape(activeId)}"]`);
    if (!(row instanceof HTMLElement)) {
        nav.style.removeProperty("--csl-current-top");
        delete nav.dataset.currentVisible;
        return;
    }
    const center = locatorCurrentTop(nav, list, row);
    nav.style.setProperty("--csl-current-top", `${Math.round(center)}px`);
    nav.dataset.currentVisible = locatorTopicVisible(nav, list, center) ? "true" : "false";
}

/**
 * Right-edge index of user commands. Ticks stay visible. Hovering one
 * brightens it, lengthens the two ticks on either side, and shows only
 * that command. Leaving the rail restores the ticks. Clicking jumps.
 */
export const ChatSessionLocator = memo(function ChatSessionLocator({
    turns,
    lang,
    theme,
    onJump,
    onWarm,
    onTranscriptWheel,
}: {
    turns: readonly ChatSessionLocatorTurn[];
    lang: string;
    theme: Theme;
    onJump: (id: string) => void;
    onWarm?: () => void;
    /** Wheel over the ticks, or over a list that is not scrolling itself. */
    onTranscriptWheel?: (deltaY: number) => void;
}) {
    const [open, setOpen] = useState(false);
    const [activeId, setActiveId] = useState<string | null>(null);
    const closingRef = useRef(false);
    const navRef = useRef<HTMLElement>(null);
    const listRef = useRef<HTMLDivElement>(null);
    const openRef = useRef(false);
    const activeIdRef = useRef<string | null>(null);
    openRef.current = open;
    activeIdRef.current = activeId;
    // Closed ticks and a freshly opened rail show the latest command.
    // Scrolling up keeps that place when older rows arrive.
    const stickEndRef = useRef(true);
    useLayoutEffect(() => {
        const list = listRef.current;
        if (!list) return;
        // The first measure can be 0 while the transcript is hidden (the new-task
        // wizard). Stick again when the rail actually gets a height.
        const stickToEnd = () => {
            if (stickEndRef.current) list.scrollTop = list.scrollHeight;
            // Width and height changes move the active tick. Measure again.
            placeLocatorCurrent(navRef.current, list, openRef.current ? activeIdRef.current : null);
        };
        stickToEnd();
        if (typeof ResizeObserver !== "function") return;
        const observer = new ResizeObserver(stickToEnd);
        observer.observe(list);
        return () => observer.disconnect();
    }, [open, turns]);
    // Ticks already on screen can wait for hover. An empty rail has nothing
    // to hover, so that is the only case that asks for older commands up front.
    useEffect(() => {
        if (turns.length > 0) return;
        onWarm?.();
    }, [onWarm, turns.length]);
    useLayoutEffect(() => {
        placeLocatorCurrent(navRef.current, listRef.current, open ? activeId : null);
    }, [open, activeId, turns]);
    if (turns.length === 0) return null;
    const activeIndex = activeId == null ? -1 : turns.findIndex((turn) => turn.id === activeId);
    const activeTurn = activeIndex >= 0 ? turns[activeIndex] : null;
    const closeRail = () => {
        stickEndRef.current = true;
        setActiveId(null);
        setOpen(false);
    };
    const style = {
        "--csl-bg": theme.fieldBg,
        "--csl-border": theme.titleBarBorder,
        "--csl-text": theme.text,
        "--csl-tick": theme.textMuted,
        "--csl-shadow": theme.isDark ? "0 8px 24px rgba(0,0,0,0.45)" : "0 8px 24px rgba(15,23,42,0.12)",
    } as CSSProperties;
    return (
        <nav
            ref={navRef}
            className={open ? "chat-session-locator is-open" : "chat-session-locator"}
            style={style}
            data-testid="chat-session-locator"
            aria-label={localizeText(lang, "Jump to a message", "会话定位", "會話定位")}
        >
            <div
                ref={listRef}
                className="chat-session-locator-list"
                tabIndex={0}
                aria-expanded={open}
                data-scroll-intent-boundary=""
                onMouseEnter={() => {
                    // Returning from the topic line must not snap a scrolled window.
                    if (!open) stickEndRef.current = true;
                    setOpen(true);
                    onWarm?.();
                }}
                onMouseLeave={(event) => {
                    if (pointerStillOnRail(navRef.current, event.currentTarget, event.relatedTarget)) return;
                    closeRail();
                }}
                onScroll={(event) => {
                    const list = event.currentTarget;
                    const atEnd = list.scrollHeight - list.scrollTop - list.clientHeight <= 8;
                    // Closed ticks are only a window onto the latest commands.
                    // The list is focusable, so arrow keys would otherwise slide
                    // that window with nothing on screen to show it moved.
                    if (!open) {
                        stickEndRef.current = true;
                        if (!atEnd) list.scrollTop = list.scrollHeight;
                    } else {
                        stickEndRef.current = atEnd;
                    }
                    placeLocatorCurrent(navRef.current, list, open ? activeId : null);
                }}
                onFocus={() => {
                    if (closingRef.current) return;
                    setOpen(true);
                    onWarm?.();
                }}
                onBlur={(event) => {
                    const next = event.relatedTarget;
                    if (next instanceof Node && event.currentTarget.contains(next)) return;
                    closeRail();
                }}
                onKeyDown={(event) => {
                    if (event.key !== "Escape") return;
                    event.preventDefault();
                    event.stopPropagation();
                    // focus() moves the caret back here and would reopen via onFocus.
                    closingRef.current = true;
                    closeRail();
                    event.currentTarget.focus();
                    closingRef.current = false;
                }}
                onWheel={(event: WheelEvent<HTMLDivElement>) => {
                    const list = event.currentTarget;
                    if (!locatorWheelTargetsTranscript(open, list.scrollHeight, list.clientHeight, list.scrollTop, event.deltaY)) return;
                    onTranscriptWheel?.(event.deltaY);
                }}
            >
                {turns.map((turn, index) => {
                    const distance = activeIndex < 0 ? -1 : Math.abs(index - activeIndex);
                    return (
                    <button
                        key={turn.id}
                        type="button"
                        className="chat-session-locator-turn"
                        data-testid="chat-session-locator-turn"
                        data-turn-id={turn.id}
                        data-near={distance >= 0 && distance <= 2 ? String(distance) : undefined}
                        tabIndex={open ? 0 : -1}
                        aria-label={turn.label}
                        aria-current={distance === 0 ? "true" : undefined}
                        onMouseEnter={() => setActiveId(turn.id)}
                        onFocus={() => setActiveId(turn.id)}
                        onMouseDown={(event) => {
                            // Pointer activation must not take focus. The earlier-messages
                            // dialog restores the focused node when it closes, and onFocus
                            // would open this rail again over the row just revealed.
                            if (event.button !== 0) return;
                            event.preventDefault();
                        }}
                        onClick={() => {
                            const active = document.activeElement;
                            const list = listRef.current;
                            if (list && active instanceof HTMLElement && list.contains(active)) active.blur();
                            onJump(turn.id);
                            closeRail();
                        }}
                    >
                        <span className="chat-session-locator-label" aria-hidden="true">{turn.label}</span>
                        <span className="chat-session-locator-tick" aria-hidden="true" />
                    </button>
                    );
                })}
            </div>
            {open && activeTurn ? (
                <div
                    className="chat-session-locator-current"
                    data-testid="chat-session-locator-current"
                    title={activeTurn.label}
                    aria-hidden="true"
                    onMouseDown={(event) => {
                        if (event.button !== 0) return;
                        event.preventDefault();
                    }}
                    onMouseLeave={(event) => {
                        if (pointerStillOnRail(navRef.current, listRef.current, event.relatedTarget)) return;
                        closeRail();
                    }}
                    onClick={() => {
                        const active = document.activeElement;
                        const list = listRef.current;
                        if (list && active instanceof HTMLElement && list.contains(active)) active.blur();
                        onJump(activeTurn.id);
                        closeRail();
                    }}
                >
                    {activeTurn.label}
                </div>
            ) : null}
        </nav>
    );
});
