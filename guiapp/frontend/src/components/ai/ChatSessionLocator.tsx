import { memo, useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type WheelEvent } from "react";
import type { Theme } from "./aiAssistantPanelTheme";
import { localizeText } from "./aiAssistantI18n";
import { locatorWheelTargetsTranscript, type ChatSessionLocatorTurn } from "./chatSessionLocatorModel";
import "./ChatSessionLocator.css";

/**
 * Right-edge index of user commands. Ticks stay visible; hovering the stack
 * opens every command as one line. Clicking a line is the caller's jump.
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
    const closingRef = useRef(false);
    const listRef = useRef<HTMLDivElement>(null);
    // Closed ticks and a freshly opened card show the latest command.
    // Scrolling up in the card keeps that place when older rows arrive.
    const stickEndRef = useRef(true);
    useLayoutEffect(() => {
        const list = listRef.current;
        if (!list) return;
        // The first measure can be 0 while the transcript is hidden (the new-task
        // wizard). Stick again when the rail actually gets a height.
        const stickToEnd = () => {
            if (!stickEndRef.current) return;
            list.scrollTop = list.scrollHeight;
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
    if (turns.length === 0) return null;
    const style = {
        "--csl-bg": theme.fieldBg,
        "--csl-border": theme.titleBarBorder,
        "--csl-text": theme.text,
        "--csl-tick": theme.textMuted,
        "--csl-hover": theme.isDark ? "rgba(255,255,255,0.08)" : "rgba(15,23,42,0.06)",
        "--csl-shadow": theme.isDark ? "0 8px 24px rgba(0,0,0,0.45)" : "0 8px 24px rgba(15,23,42,0.12)",
    } as CSSProperties;
    return (
        <nav
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
                onMouseEnter={() => { stickEndRef.current = true; setOpen(true); onWarm?.(); }}
                onMouseLeave={(event) => {
                    const next = event.relatedTarget;
                    if (next instanceof Node && event.currentTarget.contains(next)) return;
                    if (event.currentTarget.contains(document.activeElement)) return;
                    stickEndRef.current = true;
                    setOpen(false);
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
                        return;
                    }
                    stickEndRef.current = atEnd;
                }}
                onFocus={() => {
                    if (closingRef.current) return;
                    setOpen(true);
                    onWarm?.();
                }}
                onBlur={(event) => {
                    const next = event.relatedTarget;
                    if (next instanceof Node && event.currentTarget.contains(next)) return;
                    stickEndRef.current = true;
                    setOpen(false);
                }}
                onKeyDown={(event) => {
                    if (event.key !== "Escape") return;
                    event.preventDefault();
                    event.stopPropagation();
                    // focus() moves the caret back here and would reopen via onFocus.
                    closingRef.current = true;
                    stickEndRef.current = true;
                    setOpen(false);
                    event.currentTarget.focus();
                    closingRef.current = false;
                }}
                onWheel={(event: WheelEvent<HTMLDivElement>) => {
                    const list = event.currentTarget;
                    if (!locatorWheelTargetsTranscript(open, list.scrollHeight, list.clientHeight, list.scrollTop, event.deltaY)) return;
                    onTranscriptWheel?.(event.deltaY);
                }}
            >
                {turns.map((turn) => (
                    <button
                        key={turn.id}
                        type="button"
                        className="chat-session-locator-turn"
                        data-testid="chat-session-locator-turn"
                        data-turn-id={turn.id}
                        tabIndex={open ? 0 : -1}
                        aria-label={turn.label}
                        onMouseDown={(event) => {
                            // Pointer activation must not take focus. The earlier-messages
                            // dialog restores the focused node when it closes, and onFocus
                            // would open this card again over the row just revealed.
                            if (event.button !== 0) return;
                            event.preventDefault();
                        }}
                        onClick={() => {
                            const active = document.activeElement;
                            const list = listRef.current;
                            if (list && active instanceof HTMLElement && list.contains(active)) active.blur();
                            onJump(turn.id);
                            stickEndRef.current = true;
                            setOpen(false);
                        }}
                    >
                        <span className="chat-session-locator-label">{turn.label}</span>
                        <span className="chat-session-locator-tick" aria-hidden="true" />
                    </button>
                ))}
            </div>
        </nav>
    );
});
