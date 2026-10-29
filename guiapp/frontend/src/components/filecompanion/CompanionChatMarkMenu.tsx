// Right-click menu for a selection inside the agent's answer. It mirrors the
// PDF reader's menu (same keyboard behaviour, same look) but acts on the
// transcript: copy the quote, paint it with one of the four highlight colours,
// or clear a highlight the reader right-clicked on.

import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { companionFill, companionSay, getCompanionUiLang } from "../../i18n/mnCompanion";
import { CHAT_MARK_COLORS, type ChatMarkColor } from "./chatMarks";

export type ChatMenuState = { x: number; y: number };

type ChatMenuGlyphKind = "copy" | "highlight" | "erase";

function ChatMenuGlyph({ kind }: { kind: ChatMenuGlyphKind }) {
    if (kind === "highlight") {
        return (
            <svg className="companion-pdf-menu-glyph" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <rect x="3" y="14" width="11" height="3.6" rx="1" fill="#ffe08a" stroke="none" />
                <path d="M14.2 12.8 18.8 8.2 21 10.4 16.4 15H14.2z" />
            </svg>
        );
    }
    if (kind === "erase") {
        return (
            <svg className="companion-pdf-menu-glyph" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d="m14 5 5 5-8.5 8.5a2 2 0 0 1-2.8 0l-2.2-2.2a2 2 0 0 1 0-2.8z" />
                <path d="m8.5 10.5 5 5" />
                <path d="M5 20h14" />
            </svg>
        );
    }
    return (
        <svg className="companion-pdf-menu-glyph" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <rect x="8.5" y="8.5" width="11" height="11" rx="2" />
            <path d="M15.5 5.5H6.5a2 2 0 0 0-2 2v9" />
        </svg>
    );
}

function menuQuote(text: string): string {
    const line = text.replace(/\s+/g, " ").trim();
    const chars = Array.from(line);
    return chars.length > 96 ? `${chars.slice(0, 96).join("")}…` : line;
}

const MENU_VIEWPORT_GAP = 8;

export function CompanionChatMarkMenu({
    state,
    quote,
    hasSelection,
    canPaint,
    canClear,
    onCopy,
    onPaint,
    onClear,
    onClose,
    lang,
}: {
    state: ChatMenuState;
    quote: string;
    hasSelection: boolean;
    canPaint: boolean;
    canClear: boolean;
    onCopy: () => void;
    onPaint: (color: ChatMarkColor) => void;
    onClear: () => void;
    onClose: () => void;
    lang?: string;
}) {
    const shownLang = lang || getCompanionUiLang();
    const say = (zh: string) => companionSay(shownLang, zh);
    const rootRef = useRef<HTMLDivElement>(null);
    const [place, setPlace] = useState({ left: state.x, top: state.y });

    useLayoutEffect(() => {
        const root = rootRef.current;
        if (!root) return;
        const measure = () => {
            const box = root.getBoundingClientRect();
            const width = box.width || root.offsetWidth;
            const height = box.height || root.offsetHeight;
            const maxLeft = Math.max(MENU_VIEWPORT_GAP, window.innerWidth - width - MENU_VIEWPORT_GAP);
            const maxTop = Math.max(MENU_VIEWPORT_GAP, window.innerHeight - height - MENU_VIEWPORT_GAP);
            const left = Math.min(Math.max(MENU_VIEWPORT_GAP, state.x), maxLeft);
            const top = Math.min(Math.max(MENU_VIEWPORT_GAP, state.y), maxTop);
            setPlace((current) => (current.left === left && current.top === top ? current : { left, top }));
        };
        measure();
        window.addEventListener("resize", measure);
        return () => window.removeEventListener("resize", measure);
    }, [state.x, state.y, quote, canPaint, canClear]);

    useEffect(() => {
        rootRef.current?.querySelector<HTMLElement>('[role="menuitem"]')?.focus({ preventScroll: true });
    }, []);

    // The transcript moves under a fixed menu, so the menu would point at
    // nothing. Scrolling inside the menu itself must not close it.
    useEffect(() => {
        const onScroll = (event: Event) => {
            const target = event.target as Node | null;
            if (target && rootRef.current?.contains(target)) return;
            onClose();
        };
        window.addEventListener("scroll", onScroll, true);
        return () => window.removeEventListener("scroll", onScroll, true);
    }, [onClose]);

    useEffect(() => {
        const onKey = (event: KeyboardEvent) => {
            if (event.key !== "Escape") return;
            event.preventDefault();
            event.stopPropagation();
            onClose();
        };
        window.addEventListener("keydown", onKey, true);
        return () => window.removeEventListener("keydown", onKey, true);
    }, [onClose]);

    // A press anywhere else dismisses the menu, like a native one. The menu's
    // own press handler stops propagation, so this never eats its action.
    useEffect(() => {
        const onPointer = (event: MouseEvent) => {
            const target = event.target as Node | null;
            if (target && rootRef.current?.contains(target)) return;
            onClose();
        };
        window.addEventListener("mousedown", onPointer);
        return () => window.removeEventListener("mousedown", onPointer);
    }, [onClose]);

    // A mouse press runs the action and closes the menu, so the click that
    // follows must not run it a second time.
    const pressHandledRef = useRef<EventTarget | null>(null);

    const activate = (action: () => void) => ({
        onMouseDown: (event: React.MouseEvent) => {
            if (event.button !== 0) return;
            event.preventDefault();
            event.stopPropagation();
            pressHandledRef.current = event.currentTarget;
            action();
        },
        onClick: (event: React.MouseEvent) => {
            if (pressHandledRef.current === event.currentTarget) {
                pressHandledRef.current = null;
                return;
            }
            event.preventDefault();
            action();
        },
    });

    const onKeyDown = (event: React.KeyboardEvent) => {
        const root = rootRef.current;
        if (!root) return;
        if (event.key === "Tab") {
            onClose();
            return;
        }
        if (event.key !== "ArrowDown" && event.key !== "ArrowUp" && event.key !== "Home" && event.key !== "End") return;
        const items = Array.from(root.querySelectorAll<HTMLElement>('[role="menuitem"]:not([disabled])'));
        if (items.length === 0) return;
        event.preventDefault();
        const active = document.activeElement as HTMLElement | null;
        const index = active ? items.indexOf(active) : -1;
        if (event.key === "Home") {
            items[0]?.focus();
            return;
        }
        if (event.key === "End") {
            items[items.length - 1]?.focus();
            return;
        }
        const step = event.key === "ArrowDown" ? 1 : -1;
        const next = index < 0
            ? (step > 0 ? 0 : items.length - 1)
            : (index + step + items.length) % items.length;
        items[next]?.focus();
    };

    const shownQuote = quote ? menuQuote(quote) : "";
    const quoteTitle = quote.replace(/\s+/g, " ").trim();

    return (
        <div
            ref={rootRef}
            className="companion-pdf-menu"
            data-testid="chat-highlight-menu"
            role="menu"
            aria-label={say("选中文本操作")}
            style={{ left: place.left, top: place.top }}
            onMouseDown={(event) => event.stopPropagation()}
            onContextMenu={(event) => {
                event.preventDefault();
                event.stopPropagation();
            }}
            onKeyDown={onKeyDown}
        >
            {shownQuote ? (
                <p className="companion-pdf-menu-quote" data-testid="chat-menu-quote" title={quoteTitle}>{shownQuote}</p>
            ) : null}
            {hasSelection ? (
                <div className="companion-pdf-menu-group">
                    <button
                        type="button"
                        role="menuitem"
                        data-testid="chat-menu-copy"
                        className="companion-pdf-menu-item"
                        {...activate(onCopy)}
                    >
                        <ChatMenuGlyph kind="copy" />
                        <span className="companion-pdf-menu-item-label">{say("复制")}</span>
                    </button>
                </div>
            ) : null}
            {canPaint ? (
                <div className="companion-pdf-menu-group">
                    <p className="companion-pdf-menu-title"><ChatMenuGlyph kind="highlight" />{say("高亮")}</p>
                    <div className="companion-pdf-menu-colors" role="group" aria-label={say("高亮颜色")}>
                        {CHAT_MARK_COLORS.map((color) => {
                            const colorName = say(color.label);
                            const colorTitle = companionFill(shownLang, "${color}高亮", { color: colorName });
                            return (
                            <button
                                key={color.id}
                                type="button"
                                role="menuitem"
                                data-testid={`chat-highlight-${color.id}`}
                                className="companion-pdf-menu-color"
                                aria-label={colorTitle}
                                title={colorTitle}
                                {...activate(() => onPaint(color.id))}
                            >
                                <span style={{ background: color.fill }} />
                            </button>
                            );
                        })}
                    </div>
                </div>
            ) : null}
            {canClear ? (
                <div className="companion-pdf-menu-group">
                    <button
                        type="button"
                        role="menuitem"
                        data-testid="chat-highlight-clear"
                        className="companion-pdf-menu-item companion-pdf-menu-item--danger"
                        {...activate(onClear)}
                    >
                        <ChatMenuGlyph kind="erase" />
                        <span className="companion-pdf-menu-item-label">{say("取消高亮")}</span>
                    </button>
                </div>
            ) : null}
        </div>
    );
}
