// Cards for opinions a reader wrote on an answer. They sit on the quoted
// passage the way a PDF note sits on a selection: drag the bar to move the
// card, drag the corner to resize it, and close it down to the small mark.
// The anchor is skipped by the highlight walker, so the card's own text never
// shifts the character offsets the note is stored against.

import { useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { copyTextToClipboard } from "../ai/AssistantReplyCopyButton";
import { companionSay, getCompanionUiLang } from "../../i18n/mnCompanion";
import { chatRangeForMark } from "./chatMarkAnchors";
import {
    capPdfAnnotationDraft,
    chatAnnotationLineBoxes,
    chatAnnotationRangeLineRects,
    chatAnnotationOpensAbove,
    clampChatAnnotationPlace,
    clipPdfAnnotationBody,
    type ChatAnnotation,
    type ChatAnnotationLine,
    type ChatAnnotationPlace,
    type ChatAnnotationSize,
} from "./chatAnnotations";

function say(lang: string, zh: string): string {
    return companionSay(lang || getCompanionUiLang(), zh);
}

function imeKey(event: React.KeyboardEvent): boolean {
    const native = event.nativeEvent;
    return native.isComposing || native.keyCode === 229;
}

type AnchorLayout = { lines: ChatAnnotationLine[]; hostW: number; hostH: number };

function measureAnchors(root: HTMLElement, notes: ChatAnnotation[]): Record<string, AnchorLayout> {
    const next: Record<string, AnchorLayout> = {};
    for (const note of notes) {
        const host = root.querySelector<HTMLElement>(`[data-chat-message="${note.message}"]`);
        if (!host) continue;
        const hostBox = host.getBoundingClientRect();
        const range = chatRangeForMark(root, note);
        // The quote is no longer in this answer. A box at the corner would
        // claim the wrong sentence, so the note stays stored and draws nothing
        // until the text is back. A range without geometry still gets a box.
        if (!range) continue;
        const collected = chatAnnotationRangeLineRects(host, range);
        const hostW = hostBox.width || host.clientWidth || 1;
        const hostH = hostBox.height || host.clientHeight || 1;
        if (!collected.geometry) {
            next[note.id] = { lines: [{ left: 8, top: 8, width: 12, height: 12 }], hostW, hostH };
            continue;
        }
        // left/top are from the padding edge. The border box starts outside
        // that, so a box taken from the viewport sits off the glyphs.
        const originX = hostBox.left + host.clientLeft;
        const originY = hostBox.top + host.clientTop;
        const lines = chatAnnotationLineBoxes(collected.rects).map((line) => ({
            left: line.left - originX,
            top: line.top - originY,
            width: line.width,
            height: line.height,
        }));
        // Every line of the quote is scrolled out of its block. Keep the note
        // stored and draw it again when the line comes back.
        if (lines.length === 0) continue;
        next[note.id] = { lines, hostW, hostH };
    }
    return next;
}

function sameAnchorLayout(current: Record<string, AnchorLayout>, next: Record<string, AnchorLayout>): boolean {
    const keys = Object.keys(next);
    if (keys.length !== Object.keys(current).length) return false;
    for (const key of keys) {
        const left = current[key];
        const right = next[key];
        if (!left || left.hostW !== right.hostW || left.hostH !== right.hostH || left.lines.length !== right.lines.length) return false;
        for (let index = 0; index < left.lines.length; index += 1) {
            const a = left.lines[index];
            const b = right.lines[index];
            if (a.left !== b.left || a.top !== b.top || a.width !== b.width || a.height !== b.height) return false;
        }
    }
    return true;
}

function NoteGlyph({ kind }: { kind: "note" | "pen" | "check" | "copy" | "close" | "discard" }) {
    const common = {
        className: "companion-pdf-menu-glyph",
        viewBox: "0 0 24 24",
        fill: "none",
        stroke: "currentColor",
        strokeWidth: 1.7,
        strokeLinecap: "round" as const,
        strokeLinejoin: "round" as const,
        "aria-hidden": true,
    };
    if (kind === "check") return <svg {...common}><path d="m5 12 5 5L20 7" /></svg>;
    if (kind === "copy") {
        return (
            <svg {...common}>
                <rect x="8.5" y="8.5" width="11" height="11" rx="2" />
                <path d="M15.5 5.5H6.5a2 2 0 0 0-2 2v9" />
            </svg>
        );
    }
    if (kind === "close") return <svg {...common}><path d="m6 6 12 12M18 6 6 18" /></svg>;
    if (kind === "discard") return <svg {...common}><path d="M6 6h12M9 6V4h6v2M8 6l1 14h6l1-14" /></svg>;
    if (kind === "pen") {
        return (
            <svg {...common}>
                <path d="m14 5 5 5-8.5 8.5a2 2 0 0 1-2.8 0l-2.2-2.2a2 2 0 0 1 0-2.8z" />
                <path d="m8.5 10.5 5 5" />
            </svg>
        );
    }
    return (
        <svg {...common}>
            <path d="M4.5 4.5h15v11h-8.5L7 19.5v-4H4.5z" />
            <path d="M7.5 8.5h9" />
            <path d="M7.5 11.5h6" />
        </svg>
    );
}

function ChatAnnotationCard({
    lang,
    note,
    hostW,
    hostH,
    onClose,
    onBody,
    onPlace,
    onSize,
    onContextMenu,
}: {
    lang: string;
    note: ChatAnnotation;
    hostW: number;
    hostH: number;
    onClose: () => void;
    onBody: (id: string, body: string) => void;
    onPlace: (id: string, place: ChatAnnotationPlace | null) => void;
    onSize: (id: string, size: ChatAnnotationSize | null) => void;
    onContextMenu: (event: React.MouseEvent, id: string) => void;
}) {
    const editable = !note.shared;
    const [editing, setEditing] = useState(false);
    const [draft, setDraft] = useState("");
    const [copied, setCopied] = useState(false);
    const [copyFailed, setCopyFailed] = useState(false);
    const [autoAbove, setAutoAbove] = useState(false);
    const copiedTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
    const alive = useRef(true);
    const popRef = useRef<HTMLSpanElement | null>(null);
    const gesture = useRef<(() => void) | null>(null);
    const dragVisual = useRef<{ span: HTMLElement; x: number; y: number } | null>(null);
    const resizeVisual = useRef<{ pop: HTMLElement; w: number; h: number } | null>(null);
    useLayoutEffect(() => {
        alive.current = true;
        return () => {
            alive.current = false;
            gesture.current?.();
            if (copiedTimer.current) clearTimeout(copiedTimer.current);
        };
    }, []);
    useLayoutEffect(() => {
        const dragging = dragVisual.current;
        if (dragging) {
            const host = dragging.span.parentElement;
            const box = host?.getBoundingClientRect();
            if (box && box.width >= 1 && box.height >= 1) {
                dragging.span.style.setProperty("--pop-x", `${dragging.x * box.width}px`);
                dragging.span.style.setProperty("--pop-y", `${dragging.y * box.height}px`);
            }
        }
        const resizing = resizeVisual.current;
        if (!resizing) return;
        resizing.pop.style.width = `${resizing.w}px`;
        resizing.pop.style.height = `${resizing.h}px`;
        resizing.pop.style.maxWidth = "none";
        resizing.pop.style.maxHeight = "none";
    });
    useLayoutEffect(() => {
        if (note.place) {
            setAutoAbove((current) => (current ? false : current));
            return;
        }
        const pop = popRef.current;
        const span = pop?.parentElement;
        // A note on a document body anchors inside the preview scroller, not
        // the chat log. Both scrollers answer the same question: does the
        // card fit under the quote in this viewport.
        const scroller = span?.closest<HTMLElement>("[data-testid='file-companion-chat-log'], [data-doc-scroller]");
        if (!pop || !span || !scroller) return;
        const measure = () => {
            if (dragVisual.current || resizeVisual.current) return;
            const spanBox = span.getBoundingClientRect();
            const view = scroller.getBoundingClientRect();
            const wantAbove = chatAnnotationOpensAbove(spanBox.top, spanBox.bottom, view.top, view.bottom, pop.getBoundingClientRect().height);
            setAutoAbove((current) => (current === wantAbove ? current : wantAbove));
        };
        measure();
        // Scrolling moves the quote through the viewport without resizing the
        // bubble, and so does making the log shorter. The side is chosen again
        // from where the quote sits. A side the reader dragged stays put.
        scroller.addEventListener("scroll", measure, { capture: true, passive: true });
        const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(measure);
        observer?.observe(scroller);
        return () => {
            scroller.removeEventListener("scroll", measure, { capture: true });
            observer?.disconnect();
        };
    }, [note.id, note.body, note.name, note.text, note.place, editing, hostW, hostH]);
    const above = note.place ? note.place.above === true : autoAbove;
    const sized = Boolean(note.size) && hostW > 0 && hostH > 0;
    const popStyle: React.CSSProperties | undefined = sized && note.size ? {
        width: `${Math.max(Math.min(200, hostW), Math.min(hostW, note.size.w * hostW))}px`,
        height: `${Math.max(Math.min(100, hostH), Math.min(hostH, note.size.h * hostH))}px`,
        maxWidth: "none",
        maxHeight: "none",
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
    } : undefined;
    const stop = (event: React.MouseEvent) => {
        event.preventDefault();
        event.stopPropagation();
    };
    const finishEdit = (next: boolean) => {
        setEditing(next);
        if (!next) setDraft("");
    };
    const commit = () => {
        const body = clipPdfAnnotationBody(draft);
        if (!body) return;
        finishEdit(false);
        if (body !== note.body) onBody(note.id, body);
    };
    const copyLabel = copyFailed
        ? say(lang, "复制失败")
        : copied
            ? say(lang, "已复制")
            : say(lang, "复制批注");
    const onDragMouseDown = (event: React.MouseEvent) => {
        if (event.button !== 0 || editing) return;
        event.preventDefault();
        event.stopPropagation();
        const span = (event.currentTarget as HTMLElement).closest(".companion-pdf-translation") as HTMLElement | null;
        const host = span?.parentElement;
        if (!span || !host) return;
        const startBox = host.getBoundingClientRect();
        if (startBox.width < 1 || startBox.height < 1) return;
        gesture.current?.();
        const aboveNow = note.place ? note.place.above === true : autoAbove;
        const originX = note.place?.x ?? 0;
        const originY = note.place?.y ?? 0;
        const startX = event.clientX;
        const startY = event.clientY;
        const startLeft = startBox.left;
        const startTop = startBox.top;
        let last = clampChatAnnotationPlace(originX, originY, aboveNow);
        let stopped = false;
        const apply = (place: ChatAnnotationPlace | null, width: number, height: number) => {
            span.style.setProperty("--pop-x", `${(place?.x ?? 0) * width}px`);
            span.style.setProperty("--pop-y", `${(place?.y ?? 0) * height}px`);
            dragVisual.current = { span, x: place?.x ?? 0, y: place?.y ?? 0 };
        };
        // One correction per move keeps the card inside the chat column. The
        // log follows the newest line, so scrolling it would be undone.
        const fitInside = (place: ChatAnnotationPlace | null, width: number, height: number): ChatAnnotationPlace | null => {
            apply(place, width, height);
            const pop = span.querySelector<HTMLElement>("[role='dialog']");
            const scroller = span.closest<HTMLElement>("[data-testid='file-companion-chat-log'], [data-doc-scroller]");
            if (!pop || !scroller) return place;
            const popBox = pop.getBoundingClientRect();
            const view = scroller.getBoundingClientRect();
            const innerW = scroller.clientWidth;
            const innerH = scroller.clientHeight;
            if (popBox.width < 1 || popBox.height < 1 || innerW < 1 || innerH < 1) return place;
            const right = view.left + innerW;
            const bottom = view.top + innerH;
            let fixX = 0;
            let fixY = 0;
            if (popBox.width >= innerW - 1) fixX = view.left - popBox.left;
            else if (popBox.left < view.left) fixX = view.left - popBox.left;
            else if (popBox.right > right) fixX = right - popBox.right;
            if (popBox.height >= innerH - 1) fixY = view.top - popBox.top;
            else if (popBox.top < view.top) fixY = view.top - popBox.top;
            else if (popBox.bottom > bottom) fixY = bottom - popBox.bottom;
            if (Math.abs(fixX) < 0.5 && Math.abs(fixY) < 0.5) return place;
            return clampChatAnnotationPlace((place?.x ?? 0) + fixX / width, (place?.y ?? 0) + fixY / height, aboveNow);
        };
        const stop = () => {
            if (stopped) return;
            stopped = true;
            dragVisual.current = null;
            window.removeEventListener("mousemove", move, true);
            window.removeEventListener("mouseup", stop, true);
            window.removeEventListener("blur", stop);
            document.body.classList.remove("companion-pdf-translation-dragging");
            if (gesture.current === stop) gesture.current = null;
            const same = originX === (last?.x ?? 0)
                && originY === (last?.y ?? 0)
                && aboveNow === (last?.above === true);
            if (same) return;
            onPlace(note.id, last);
        };
        const move = (ev: MouseEvent) => {
            const now = host.getBoundingClientRect();
            if (now.width < 1 || now.height < 1) return;
            // The log can move under the pointer. Subtract that, or the card
            // jumps by the scroll instead of following the hand.
            const dx = (ev.clientX - startX) - (now.left - startLeft);
            const dy = (ev.clientY - startY) - (now.top - startTop);
            const raw = clampChatAnnotationPlace(
                originX + dx / now.width,
                originY + dy / now.height,
                aboveNow,
            );
            last = fitInside(raw, now.width, now.height);
            apply(last, now.width, now.height);
        };
        document.body.classList.add("companion-pdf-translation-dragging");
        window.addEventListener("mousemove", move, true);
        window.addEventListener("mouseup", stop, true);
        window.addEventListener("blur", stop);
        gesture.current = stop;
    };
    const onResizeMouseDown = (event: React.MouseEvent) => {
        if (event.button !== 0) return;
        event.preventDefault();
        event.stopPropagation();
        const pop = (event.currentTarget as HTMLElement).closest("[role='dialog']") as HTMLElement | null;
        const span = pop?.closest(".companion-pdf-translation") as HTMLElement | null;
        const host = span?.parentElement;
        if (!pop || !host) return;
        const startBox = host.getBoundingClientRect();
        if (startBox.width < 1 || startBox.height < 1) return;
        gesture.current?.();
        const startX = event.clientX;
        const startY = event.clientY;
        const startW = pop.offsetWidth;
        const startH = pop.offsetHeight;
        let lastW = startW;
        let lastH = startH;
        let stopped = false;
        const apply = (width: number, height: number) => {
            pop.style.width = `${width}px`;
            pop.style.height = `${height}px`;
            pop.style.maxWidth = "none";
            pop.style.maxHeight = "none";
            resizeVisual.current = { pop, w: width, h: height };
        };
        const stop = () => {
            if (stopped) return;
            stopped = true;
            resizeVisual.current = null;
            window.removeEventListener("mousemove", move, true);
            window.removeEventListener("mouseup", stop, true);
            window.removeEventListener("blur", stop);
            if (gesture.current === stop) gesture.current = null;
            if (Math.abs(lastW - startW) < 0.5 && Math.abs(lastH - startH) < 0.5) return;
            const now = host.getBoundingClientRect();
            if (now.width < 1 || now.height < 1) return;
            onSize(note.id, { w: Math.min(1, lastW / now.width), h: Math.min(1, lastH / now.height) });
        };
        const move = (ev: MouseEvent) => {
            const now = host.getBoundingClientRect();
            if (now.width < 1 || now.height < 1) return;
            const minW = Math.min(200, now.width);
            const minH = Math.min(100, now.height);
            lastW = Math.max(minW, Math.min(now.width, startW + ev.clientX - startX));
            lastH = Math.max(minH, Math.min(now.height, startH + ev.clientY - startY));
            apply(lastW, lastH);
        };
        window.addEventListener("mousemove", move, true);
        window.addEventListener("mouseup", stop, true);
        window.addEventListener("blur", stop);
        gesture.current = stop;
    };
    return (
        <span
            ref={popRef}
            className={above ? "companion-pdf-translation-pop companion-pdf-translation-pop--above" : "companion-pdf-translation-pop"}
            role="dialog"
            aria-label={say(lang, "批注")}
            data-testid={`chat-annotation-${note.id}`}
            data-shared={note.shared ? "true" : undefined}
            data-annotation-editing={editing ? "true" : undefined}
            style={popStyle}
            onMouseDown={(event) => event.stopPropagation()}
            onContextMenu={(event) => {
                // The editor is where a reader pastes. The card menu would take
                // that right-click, and the browser paste menu would never open.
                const target = event.target as HTMLElement | null;
                if (target?.closest?.("textarea")) {
                    event.stopPropagation();
                    return;
                }
                onContextMenu(event, note.id);
            }}
            onKeyDown={(event) => {
                if (!editing || event.key !== "Escape") return;
                event.stopPropagation();
                if (imeKey(event)) return;
                event.preventDefault();
                finishEdit(false);
            }}
        >
            <span className="companion-pdf-translation-chrome" onMouseDown={onDragMouseDown}>
                <span className="companion-pdf-translation-grip" title={say(lang, "拖动")} aria-hidden="true" />
                <span className="companion-pdf-translation-head">
                    <span className="companion-pdf-translation-kicker">{note.name || say(lang, "批注")}</span>
                    <span className="companion-pdf-translation-actions">
                        {editing ? (
                            <>
                                <button
                                    type="button"
                                    className="companion-pdf-translation-action"
                                    data-testid={`chat-annotation-edit-confirm-${note.id}`}
                                    title={say(lang, "确定")}
                                    aria-label={say(lang, "确定")}
                                    disabled={!draft.trim()}
                                    onMouseDown={stop}
                                    onClick={commit}
                                >
                                    <NoteGlyph kind="check" />
                                </button>
                                <button
                                    type="button"
                                    className="companion-pdf-translation-action"
                                    title={say(lang, "放弃")}
                                    aria-label={say(lang, "放弃")}
                                    onMouseDown={stop}
                                    onClick={() => finishEdit(false)}
                                >
                                    <NoteGlyph kind="discard" />
                                </button>
                            </>
                        ) : (
                            <>
                                {editable ? (
                                    <button
                                        type="button"
                                        className="companion-pdf-translation-action"
                                        data-testid={`chat-annotation-edit-${note.id}`}
                                        title={say(lang, "编辑批注")}
                                        aria-label={say(lang, "编辑批注")}
                                        onMouseDown={stop}
                                        onClick={() => {
                                            setDraft(note.body);
                                            finishEdit(true);
                                        }}
                                    >
                                        <NoteGlyph kind="pen" />
                                    </button>
                                ) : null}
                                <button
                                    type="button"
                                    className="companion-pdf-translation-action"
                                    data-testid={`chat-annotation-copy-${note.id}`}
                                    data-copy-state={copyFailed ? "failed" : copied ? "copied" : undefined}
                                    title={copyLabel}
                                    aria-label={copyLabel}
                                    onMouseDown={stop}
                                    onClick={() => {
                                        void copyTextToClipboard(note.body).then((ok) => {
                                            if (!alive.current) return;
                                            if (copiedTimer.current) clearTimeout(copiedTimer.current);
                                            if (!ok) {
                                                setCopied(false);
                                                setCopyFailed(true);
                                                copiedTimer.current = setTimeout(() => {
                                                    if (alive.current) setCopyFailed(false);
                                                }, 1600);
                                                return;
                                            }
                                            setCopyFailed(false);
                                            setCopied(true);
                                            copiedTimer.current = setTimeout(() => {
                                                if (alive.current) setCopied(false);
                                            }, 1400);
                                        });
                                    }}
                                >
                                    <NoteGlyph kind={copied ? "check" : "copy"} />
                                </button>
                                <button
                                    type="button"
                                    className="companion-pdf-translation-action"
                                    data-testid={`chat-annotation-close-${note.id}`}
                                    title={say(lang, "关闭")}
                                    aria-label={say(lang, "关闭批注")}
                                    onMouseDown={stop}
                                    onClick={onClose}
                                >
                                    <NoteGlyph kind="close" />
                                </button>
                            </>
                        )}
                    </span>
                </span>
            </span>
            <span className="companion-pdf-translation-body" data-sized={sized ? "true" : undefined} style={sized ? { flex: "1 1 auto", minHeight: 0, overflow: "auto" } : undefined}>
                {editing ? (
                    <textarea
                        className="companion-pdf-translation-edit"
                        data-testid={`chat-annotation-edit-input-${note.id}`}
                        value={draft}
                        autoFocus
                        rows={3}
                        aria-label={say(lang, "编辑批注")}
                        onChange={(event) => {
                            const next = capPdfAnnotationDraft(event.target.value);
                            if (next !== event.target.value) event.target.value = next;
                            setDraft(next);
                        }}
                        onMouseDown={(event) => event.stopPropagation()}
                        onKeyDown={(event) => {
                            if (event.key === "Escape") {
                                event.stopPropagation();
                                if (imeKey(event)) return;
                                event.preventDefault();
                                finishEdit(false);
                                return;
                            }
                            if (event.key === "Enter" && !event.shiftKey) {
                                if (imeKey(event)) return;
                                event.preventDefault();
                                event.stopPropagation();
                                commit();
                            }
                        }}
                    />
                ) : note.body}
            </span>
            {!editing && note.text ? (
                <span className="companion-pdf-translation-source" style={sized ? { flex: "0 0 auto" } : undefined} title={note.text}>
                    <span className="companion-pdf-translation-source-label">{say(lang, "原文")}</span>
                    {note.text}
                </span>
            ) : null}
            <span
                data-testid={`chat-annotation-resize-${note.id}`}
                title={say(lang, "调整大小")}
                aria-hidden="true"
                onMouseDown={onResizeMouseDown}
                style={{
                    position: "absolute",
                    right: 2,
                    bottom: 2,
                    width: 14,
                    height: 14,
                    cursor: "nwse-resize",
                    zIndex: 4,
                    borderRight: "2px solid currentColor",
                    borderBottom: "2px solid currentColor",
                    opacity: 0.45,
                    pointerEvents: "auto",
                }}
            />
        </span>
    );
}

export function CompanionChatAnnotationLayer({
    logRef,
    notes,
    lang,
    anchorKey,
    closed,
    onToggle,
    onBody,
    onPlace,
    onSize,
    onContextMenu,
}: {
    logRef: React.RefObject<HTMLElement | null>;
    notes: ChatAnnotation[];
    lang: string;
    anchorKey: string;
    closed: ReadonlySet<string>;
    onToggle: (id: string) => void;
    onBody: (id: string, body: string) => void;
    onPlace: (id: string, place: ChatAnnotationPlace | null) => void;
    onSize: (id: string, size: ChatAnnotationSize | null) => void;
    onContextMenu: (event: React.MouseEvent, id: string) => void;
}) {
    const [layout, setLayout] = useState<Record<string, AnchorLayout>>({});
    const [ready, setReady] = useState(false);
    const notesRef = useRef(notes);
    notesRef.current = notes;
    useLayoutEffect(() => {
        const root = logRef.current;
        if (!root) return;
        const apply = () => {
            const next = measureAnchors(root, notesRef.current);
            setLayout((current) => (sameAnchorLayout(current, next) ? current : next));
            setReady(true);
        };
        apply();
        // The log's own box stays put while a bubble grows: the reasoning
        // panel opens, or a figure arrives. Those moves are inside the
        // message, so the message itself has to be watched.
        const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(() => apply());
        observer?.observe(root);
        const seen = new Set<Element>();
        for (const note of notesRef.current) {
            const host = root.querySelector(`[data-chat-message="${note.message}"]`);
            if (!host || seen.has(host)) continue;
            seen.add(host);
            observer?.observe(host);
        }
        // Scrolling the log moves the bubble and the quote together. Scrolling
        // a code block or a formula does not: the quote slides inside the bubble.
        let frame = 0;
        const onInnerScroll = (event: Event) => {
            if (event.target === root) return;
            if (frame) return;
            frame = requestAnimationFrame(() => {
                frame = 0;
                apply();
            });
        };
        root.addEventListener("scroll", onInnerScroll, { capture: true, passive: true });
        return () => {
            observer?.disconnect();
            root.removeEventListener("scroll", onInnerScroll, { capture: true });
            if (frame) cancelAnimationFrame(frame);
        };
    }, [anchorKey, logRef]);
    const root = logRef.current;
    if (!ready || !root || notes.length === 0) return null;
    return (
        <>
            {notes.flatMap((note) => {
                const host = root.querySelector<HTMLElement>(`[data-chat-message="${note.message}"]`);
                const box = layout[note.id];
                if (!host || !box) return [];
                const open = !closed.has(note.id);
                return box.lines.map((line, index) => createPortal(
                    <span
                        className="companion-pdf-translation"
                        data-chat-mark-skip=""
                        data-chat-annotation-id={note.id}
                        data-testid={index === 0 ? `chat-annotation-anchor-${note.id}` : undefined}
                        style={{
                            left: line.left,
                            top: line.top,
                            width: line.width,
                            height: line.height,
                            ...(index === 0 ? {
                                "--sel-x": `${line.left}px`,
                                "--pop-x": `${(note.place?.x ?? 0) * box.hostW}px`,
                                "--pop-y": `${(note.place?.y ?? 0) * box.hostH}px`,
                                "--page-w": `${box.hostW}px`,
                            } : {}),
                        } as React.CSSProperties}
                    >
                        {index === 0 ? (
                            <button
                                type="button"
                                className="companion-pdf-translation-icon"
                                data-testid={`chat-annotation-icon-${note.id}`}
                                aria-label={say(lang, "批注")}
                                aria-expanded={open}
                                // The chip hangs 10px above the first line, which is
                                // where this answer's copy and export controls sit.
                                // Those controls are 4; the open card stays at 5.
                                style={{ zIndex: 3 }}
                                onMouseDown={(event) => {
                                    if (event.button !== 0) return;
                                    event.preventDefault();
                                    event.stopPropagation();
                                }}
                                onClick={(event) => {
                                    event.preventDefault();
                                    event.stopPropagation();
                                    onToggle(note.id);
                                }}
                                onContextMenu={(event) => onContextMenu(event, note.id)}
                            >
                                <NoteGlyph kind="note" />
                                {say(lang, "批注")}
                            </button>
                        ) : null}
                        {index === 0 && open ? (
                            <ChatAnnotationCard
                                lang={lang}
                                note={note}
                                hostW={box.hostW}
                                hostH={box.hostH}
                                onClose={() => onToggle(note.id)}
                                onBody={onBody}
                                onPlace={onPlace}
                                onSize={onSize}
                                onContextMenu={onContextMenu}
                            />
                        ) : null}
                    </span>,
                    host,
                    `${note.id}:${index}`,
                ));
            })}
        </>
    );
}
