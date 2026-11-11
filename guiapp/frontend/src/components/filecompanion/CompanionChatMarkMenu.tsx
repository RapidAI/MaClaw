// Right-click menu for a selection inside the agent's answer. It mirrors the
// PDF reader's menu (same keyboard behaviour, same look) but acts on the
// transcript: copy the quote, paint it with one of the four highlight colours,
// or clear a highlight the reader right-clicked on.

import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties } from "react";
import { companionFill, companionSay, getCompanionUiLang } from "../../i18n/mnCompanion";
import { capPdfAnnotationDraft } from "./chatAnnotations";
import { CHAT_MARK_COLORS, type ChatMarkColor } from "./chatMarks";

export type ChatMenuState = { x: number; y: number };

type ChatMenuGlyphKind = "copy" | "highlight" | "erase" | "note" | "pen" | "trash";

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
                <path d="m7 21-4.3-4.3c-1-1-1-2.5 0-3.4l9.6-9.6c1-1 2.5-1 3.4 0l5.6 5.6c1 1 1 2.5 0 3.4L13 21" />
                <path d="M22 21H7" />
                <path d="m5 11 9 9" />
            </svg>
        );
    }
    if (kind === "trash") {
        return (
            <svg className="companion-pdf-menu-glyph" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d="M5.5 6h13" />
                <path d="M9.5 6V4.3h5V6" />
                <path d="m7.2 6 .8 13a1.8 1.8 0 0 0 1.8 1.7h4.4a1.8 1.8 0 0 0 1.8-1.7l.8-13" />
                <path d="M10.2 9.5v8" />
                <path d="M13.8 9.5v8" />
            </svg>
        );
    }
    if (kind === "note") {
        return (
            <svg className="companion-pdf-menu-glyph" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d="M4.5 4.5h15v11h-8.5L7 19.5v-4H4.5z" />
                <path d="M7.5 8.5h9" />
                <path d="M7.5 11.5h6" />
            </svg>
        );
    }
    if (kind === "pen") {
        return (
            <svg className="companion-pdf-menu-glyph" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d="m14 5 5 5-8.5 8.5a2 2 0 0 1-2.8 0l-2.2-2.2a2 2 0 0 1 0-2.8z" />
                <path d="m8.5 10.5 5 5" />
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
    canAnnotate,
    canRenameAnnotation,
    canDeleteAnnotation,
    annotationName,
    onCopy,
    onPaint,
    onClear,
    onAnnotate,
    onRenameAnnotation,
    onDeleteAnnotation,
    onClose,
    lang,
}: {
    state: ChatMenuState;
    quote: string;
    hasSelection: boolean;
    canPaint: boolean;
    canClear: boolean;
    canAnnotate: boolean;
    canRenameAnnotation: boolean;
    canDeleteAnnotation: boolean;
    annotationName: string;
    onCopy: () => void;
    onPaint: (color: ChatMarkColor) => void;
    onClear: () => void;
    onAnnotate: (body: string) => void;
    onRenameAnnotation: (name: string) => void;
    onDeleteAnnotation: () => void;
    onClose: () => void;
    lang?: string;
}) {
    const shownLang = lang || getCompanionUiLang();
    const say = (zh: string) => companionSay(shownLang, zh);
    const rootRef = useRef<HTMLDivElement>(null);
    const [place, setPlace] = useState({ left: state.x, top: state.y });
    const [annotationOpen, setAnnotationOpen] = useState(false);
    const [annotationText, setAnnotationText] = useState("");
    const [noteEditor, setNoteEditor] = useState(false);
    const [noteDraft, setNoteDraft] = useState("");
    const editorOpen = annotationOpen || noteEditor;

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
    }, [state.x, state.y, quote, canPaint, canClear, canAnnotate, canRenameAnnotation, canDeleteAnnotation, editorOpen]);

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
            if (event.key !== "Escape" || editorOpen) return;
            event.preventDefault();
            event.stopPropagation();
            onClose();
        };
        window.addEventListener("keydown", onKey, true);
        return () => window.removeEventListener("keydown", onKey, true);
    }, [onClose, editorOpen]);

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
        const keyTarget = event.target as HTMLElement | null;
        if (keyTarget?.closest?.("input, textarea")) return;
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
    const imeKey = (event: React.KeyboardEvent) => event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229;
    const submitAnnotation = () => {
        const text = annotationText.trim();
        if (!text) return;
        onAnnotate(text);
        setAnnotationOpen(false);
        setAnnotationText("");
    };
    const submitRename = () => {
        const name = noteDraft.trim();
        if (!name) return;
        onRenameAnnotation(name);
    };

    return (
        <div
            ref={rootRef}
            className={annotationOpen ? "companion-pdf-menu companion-pdf-menu--editing" : "companion-pdf-menu"}
            data-testid="chat-highlight-menu"
            role="menu"
            aria-label={say("选中文本操作")}
            style={{ left: place.left, top: place.top, "--pdf-menu-x": `${place.left}px`, "--pdf-menu-y": `${place.top}px` } as CSSProperties}
            onMouseDown={(event) => event.stopPropagation()}
            onContextMenu={(event) => {
                // The writing field is where a reader pastes. Taking its
                // right-click would leave the browser paste menu closed.
                const target = event.target as HTMLElement | null;
                if (target?.closest?.("textarea, input")) {
                    event.stopPropagation();
                    return;
                }
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
            {canAnnotate ? (
                <div className="companion-pdf-menu-group">
                    {annotationOpen ? (
                        <div data-testid="chat-annotation-editor" className="companion-pdf-menu-editor">
                            <textarea
                                className="companion-pdf-menu-field"
                                data-testid="chat-annotation-input"
                                value={annotationText}
                                autoFocus
                                rows={3}
                                placeholder={say("写批注…")}
                                aria-label={say("写批注…")}
                                onChange={(event) => {
                                    const next = capPdfAnnotationDraft(event.target.value);
                                    if (next !== event.target.value) event.target.value = next;
                                    setAnnotationText(next);
                                }}
                                onKeyDown={(event) => {
                                    if (event.key === "Escape") {
                                        if (imeKey(event)) {
                                            event.stopPropagation();
                                            return;
                                        }
                                        event.preventDefault();
                                        event.stopPropagation();
                                        setAnnotationOpen(false);
                                        setAnnotationText("");
                                        return;
                                    }
                                    if (event.key === "Enter" && !event.shiftKey) {
                                        if (imeKey(event)) return;
                                        event.preventDefault();
                                        submitAnnotation();
                                    }
                                }}
                            />
                            <div className="companion-pdf-menu-editor-actions">
                                <button type="button" className="companion-pdf-menu-btn" data-testid="chat-annotation-cancel" onMouseDown={(event) => { event.preventDefault(); event.stopPropagation(); }} onClick={() => { setAnnotationOpen(false); setAnnotationText(""); }}>{say("取消")}</button>
                                <button type="button" className="companion-pdf-menu-btn companion-pdf-menu-btn--accent" data-testid="chat-annotation-save" disabled={!annotationText.trim()} onMouseDown={(event) => { event.preventDefault(); event.stopPropagation(); }} onClick={submitAnnotation}>{say("保存批注")}</button>
                            </div>
                        </div>
                    ) : (
                        <button type="button" role="menuitem" data-testid="chat-menu-annotate" className="companion-pdf-menu-item" {...activate(() => setAnnotationOpen(true))}>
                            <ChatMenuGlyph kind="note" />
                            <span className="companion-pdf-menu-item-label">{say("批注")}</span>
                        </button>
                    )}
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
            {canRenameAnnotation || canDeleteAnnotation ? (
                <div className="companion-pdf-menu-group">
                    {noteEditor ? (
                        <div data-testid="chat-annotation-name-editor" className="companion-pdf-bookmark-editor">
                            <input
                                data-testid="chat-annotation-name"
                                value={noteDraft}
                                autoFocus
                                maxLength={40}
                                placeholder={say("批注名")}
                                aria-label={say("批注名")}
                                onChange={(event) => setNoteDraft(event.target.value)}
                                onKeyDown={(event) => {
                                    if (event.key === "Escape") {
                                        if (imeKey(event)) {
                                            event.stopPropagation();
                                            return;
                                        }
                                        event.preventDefault();
                                        event.stopPropagation();
                                        setNoteEditor(false);
                                        return;
                                    }
                                    if (event.key === "Enter") {
                                        if (imeKey(event)) return;
                                        event.preventDefault();
                                        submitRename();
                                    }
                                }}
                            />
                            <div className="companion-pdf-bookmark-editor-actions">
                                <button type="button" className="companion-pdf-menu-btn" data-testid="chat-annotation-name-cancel" onMouseDown={(event) => { event.preventDefault(); event.stopPropagation(); }} onClick={() => setNoteEditor(false)}>{say("取消")}</button>
                                <button type="button" className="companion-pdf-menu-btn companion-pdf-menu-btn--accent" data-testid="chat-annotation-name-save" disabled={!noteDraft.trim()} onMouseDown={(event) => { event.preventDefault(); event.stopPropagation(); }} onClick={submitRename}>{say("确定")}</button>
                            </div>
                        </div>
                    ) : (
                        <>
                            {canRenameAnnotation ? (
                                <button
                                    type="button"
                                    role="menuitem"
                                    data-testid="chat-annotation-rename"
                                    className="companion-pdf-menu-item"
                                    {...activate(() => {
                                        setNoteDraft(annotationName);
                                        setNoteEditor(true);
                                    })}
                                >
                                    <ChatMenuGlyph kind="pen" />
                                    <span className="companion-pdf-menu-item-label">{say("批注改名")}</span>
                                </button>
                            ) : null}
                            {canDeleteAnnotation ? (
                                <button type="button" role="menuitem" data-testid="chat-annotation-delete" className="companion-pdf-menu-item companion-pdf-menu-item--danger" {...activate(onDeleteAnnotation)}>
                                    <ChatMenuGlyph kind="trash" />
                                    <span className="companion-pdf-menu-item-label">{say("删除批注")}</span>
                                </button>
                            ) : null}
                        </>
                    )}
                </div>
            ) : null}
        </div>
    );
}
