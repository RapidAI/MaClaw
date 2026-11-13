// Annotations on a rendered document: the markdown (or office extract) pane
// is "one message", so highlights, placed notes, and bookmarks reuse the chat
// mark stores with message DOC_MARK_MESSAGE and the pdfBookmarks store with
// page 1. Right-click paints a highlight, writes a note, or drops a named
// bookmark; the ribbons hang on the right edge like they do on a PDF page.
// Everything a reader does here is announced, so the companion window pushes
// it to the shares this document hosts, exactly like a PDF highlight.

import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { copyTextToClipboard } from "../ai/AssistantReplyCopyButton";
import { companionSay, getCompanionUiLang } from "../../i18n/mnCompanion";
import { subscribeCompanionReaderState } from "./companionReaderState";
import { pdfMarkPathKey } from "./pdfMarks";
import {
    PDF_BOOKMARK_TICK_LANE_PX,
    PDF_BOOKMARK_TICK_PX,
    PDF_BOOKMARK_TICK_WIDTH_PX,
    pdfBookmarkFill,
    pdfBookmarkInk,
    subscribePdfBookmarks,
    type PdfBookmark,
    type PdfBookmarkColor,
} from "./pdfBookmarks";
import {
    CHAT_MARK_TEXT_MAX,
    DOC_MARK_MESSAGE,
    subscribeChatMarks,
    type ChatMark,
    type ChatMarkColor,
    type ChatMarkStyle,
} from "./chatMarks";
import {
    clipPdfAnnotationBody,
    clipPdfAnnotationName,
    newChatAnnotationId,
    subscribeChatAnnotations,
    type ChatAnnotation,
    type ChatAnnotationPlace,
    type ChatAnnotationSize,
} from "./chatAnnotations";
import {
    applyChatHighlights,
    chatHighlightsSupported,
    chatMarkAtOffset,
    chatOffsetForPoint,
    chatRangeForMark,
    chatRangeForOffsets,
    chatSelectionSlices,
    chatTextLength,
    chatTextSpans,
    clearChatHighlights,
    type ChatSelectionSlice,
} from "./chatMarkAnchors";
import {
    DOC_DRAWING_PAGE,
    docBookmarkOffset,
    loadDocAnnotations,
    loadDocBookmarks,
    loadDocDrawings,
    loadDocMarks,
    newDocBookmark,
    saveDocAnnotations,
    saveDocBookmarks,
    saveDocDrawings,
    saveDocMarks,
} from "./docMarks";
import { CompanionChatMarkMenu } from "./CompanionChatMarkMenu";
import { CompanionChatAnnotationLayer } from "./CompanionChatAnnotations";
import { CompanionAnnotatorRail, type AnnotatorRailItem } from "./CompanionAnnotatorRail";
import { COMPANION_DRAWING_INK, CompanionDrawingLayer, type CompanionDrawTool } from "./CompanionDrawingLayer";
import {
    subscribePdfDrawings,
    type PdfDrawing,
} from "./pdfDrawings";
import { PDF_MARK_COLORS, type PdfMarkColor } from "./pdfMarks";

type DocAnchor = { offset: number; total: number };

type DocMenuState = {
    x: number;
    y: number;
    slices: ChatSelectionSlice[];
    hit: number;
    selected: string;
    annotationId: string;
    bookmarkId: string;
    anchor: DocAnchor | null;
};

type TickLayout = Record<string, { top: number; lane: number }>;

function DocBookmarkRibbon({ color }: { color: PdfBookmarkColor }) {
    const fill = pdfBookmarkFill(color);
    const ink = pdfBookmarkInk(color);
    return (
        <svg viewBox="0 0 16 40" aria-hidden="true" focusable="false">
            <path
                d="M.8.8h14.4V28L8 38.5.8 28Z"
                fill={fill}
                stroke={ink}
                strokeWidth="1.2"
                strokeLinejoin="round"
            />
            <path d="M2.15 1.8h3.15V26.6H2.15Z" fill="#fff" opacity=".5" />
            <path d="M8 2.2v26" stroke={ink} strokeWidth=".7" opacity=".4" />
        </svg>
    );
}

function docScrollerOf(element: HTMLElement | null): HTMLElement | null {
    return element?.closest<HTMLElement>("[data-doc-scroller]") ?? null;
}

export function CompanionDocAnnotator({
    path,
    lang,
    children,
}: {
    /** The document's own path — the store and share key. */
    path: string;
    lang?: string;
    children: ReactNode;
}) {
    const say = (zh: string) => companionSay(lang || getCompanionUiLang(), zh);
    const rootRef = useRef<HTMLDivElement>(null);
    const bodyRef = useRef<HTMLDivElement>(null);
    const hostRef = useRef<HTMLDivElement>(null);
    const [marks, setMarks] = useState<ChatMark[]>(() => loadDocMarks(path));
    const [notes, setNotes] = useState<ChatAnnotation[]>(() => loadDocAnnotations(path));
    const [bookmarks, setBookmarks] = useState<PdfBookmark[]>(() => loadDocBookmarks(path));
    const [drawings, setDrawings] = useState<PdfDrawing[]>(() => loadDocDrawings(path));
    const [drawMode, setDrawMode] = useState(false);
    const [drawTool, setDrawTool] = useState<CompanionDrawTool>("line");
    const [drawColor, setDrawColor] = useState<PdfMarkColor>("yellow");
    const [menu, setMenu] = useState<DocMenuState | null>(null);
    const [closedIds, setClosedIds] = useState<ReadonlySet<string>>(new Set());
    const [tickLayout, setTickLayout] = useState<TickLayout>({});
    const [domTick, setDomTick] = useState(0);
    const marksRef = useRef(marks);
    marksRef.current = marks;
    const notesRef = useRef(notes);
    notesRef.current = notes;
    const bookmarksRef = useRef(bookmarks);
    bookmarksRef.current = bookmarks;
    const menuRef = useRef(menu);
    menuRef.current = menu;
    const drawingsRef = useRef(drawings);
    drawingsRef.current = drawings;
    const drawToolRef = useRef<CompanionDrawTool>("line");
    const pathKey = pdfMarkPathKey(path);
    const highlightsReady = chatHighlightsSupported();

    // A document switch shows that document's marks, never the previous file's.
    useEffect(() => {
        setMarks(loadDocMarks(path));
        setNotes(loadDocAnnotations(path));
        setBookmarks(loadDocBookmarks(path));
        setDrawings(loadDocDrawings(path));
        setMenu(null);
        setClosedIds(new Set());
    }, [path]);

    // A received share writes the stores without announcing. The store's own
    // emitter is how the open preview learns about it and repaints.
    useEffect(() => {
        const offMarks = subscribeChatMarks((key) => {
            if (key === pathKey) setMarks(loadDocMarks(path));
        });
        const offNotes = subscribeChatAnnotations((key) => {
            if (key === pathKey) setNotes(loadDocAnnotations(path));
        });
        const offBookmarks = subscribePdfBookmarks((key) => {
            if (key === pathKey) setBookmarks(loadDocBookmarks(path));
        });
        const offDrawings = subscribePdfDrawings((key) => {
            if (key === pathKey) setDrawings(loadDocDrawings(path));
        });
        const offReader = subscribeCompanionReaderState(() => {
            setMarks(loadDocMarks(path));
            setNotes(loadDocAnnotations(path));
            setBookmarks(loadDocBookmarks(path));
            setDrawings(loadDocDrawings(path));
        });
        return () => {
            offMarks();
            offNotes();
            offBookmarks();
            offDrawings();
            offReader();
        };
    }, [path, pathKey]);

    // KaTeX keeps the raw TeX in an aria-hidden MathML block. Marking it keeps
    // the visible formula in the walked text without the annotation's source.
    const markSkippedNodes = () => {
        const host = hostRef.current;
        if (!host) return;
        for (const node of Array.from(host.querySelectorAll(".katex-mathml"))) {
            node.setAttribute("data-chat-mark-skip", "");
        }
    };

    // Highlight ranges hold live DOM nodes, so a re-rendered paragraph or a
    // streamed edit leaves them detached. Two jobs with different lifetimes:
    // this effect repaints when the stored marks change — a share write does
    // not touch the DOM, so no observer can see it — while the one below
    // re-anchors on DOM changes only, so it is not rebuilt on every paint.
    useLayoutEffect(() => {
        const root = rootRef.current;
        if (!root) return;
        markSkippedNodes();
        applyChatHighlights(root, marks, "companion-doc");
    }, [marks, path]);

    useLayoutEffect(() => {
        const host = hostRef.current;
        const root = rootRef.current;
        if (!host || !root) return;
        let frame = 0;
        const repaint = () => {
            frame = 0;
            markSkippedNodes();
            applyChatHighlights(root, marksRef.current, "companion-doc");
            setDomTick((tick) => tick + 1);
        };
        const observer = typeof MutationObserver === "undefined" ? null : new MutationObserver(() => {
            if (frame) return;
            frame = requestAnimationFrame(repaint);
        });
        observer?.observe(host, { childList: true, characterData: true, subtree: true });
        // A pane resize does not mutate the DOM, so the MutationObserver alone
        // cannot re-measure the bookmark ribbons when the text reflows.
        const resizeObserver = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(() => {
            setDomTick((tick) => tick + 1);
        });
        resizeObserver?.observe(host);
        return () => {
            observer?.disconnect();
            resizeObserver?.disconnect();
            if (frame) cancelAnimationFrame(frame);
            clearChatHighlights("companion-doc");
        };
    }, [path]);

    // Bookmarks anchor on a fraction of the walked text. Their pixel tops are
    // measured here so a resize or a re-render keeps the ribbon on its line.
    useLayoutEffect(() => {
        const host = hostRef.current;
        const body = bodyRef.current;
        if (!host || !body) return;
        const spans = chatTextSpans(host);
        const total = chatTextLength(spans);
        if (total === 0) {
            setTickLayout((current) => (Object.keys(current).length === 0 ? current : {}));
            return;
        }
        // The ribbons are positioned inside the body, so measure against its
        // box: the rail sits left of it, only the body is the offset parent.
        const hostBox = body.getBoundingClientRect();
        const items = bookmarks.map((bookmark) => {
            const offset = docBookmarkOffset(bookmark, total);
            const range = chatRangeForOffsets(spans, offset, Math.min(offset + 1, total));
            let top = 0;
            if (range && typeof range.getBoundingClientRect === "function") {
                const rect = range.getBoundingClientRect();
                if (rect.height > 0.5) top = rect.top - hostBox.top;
            }
            const maxTop = Math.max(0, hostBox.height - PDF_BOOKMARK_TICK_PX);
            return { id: bookmark.id, top: Math.max(0, Math.min(maxTop, top)) };
        });
        // Lanes: a closer pair steps inwards so the tails do not overlap.
        const lanes: number[] = [];
        const next: TickLayout = {};
        const order = items.slice().sort((a, b) => a.top - b.top);
        for (const item of order) {
            let lane = 0;
            while (lane < lanes.length && item.top < lanes[lane] + PDF_BOOKMARK_TICK_PX - 0.5) lane += 1;
            lanes[lane] = item.top;
            next[item.id] = { top: Math.round(item.top), lane };
        }
        setTickLayout((current) => {
            const keys = Object.keys(next);
            if (keys.length !== Object.keys(current).length) return next;
            return keys.every((id) => current[id]?.top === next[id]?.top && current[id]?.lane === next[id]?.lane)
                ? current
                : next;
        });
    }, [bookmarks, domTick, path]);

    const writeMarks = (next: ChatMark[]) => {
        setMarks(saveDocMarks(path, next, true));
        setMenu(null);
    };

    const writeNotes = (next: ChatAnnotation[], announce: boolean) => {
        const saved = saveDocAnnotations(path, next, announce);
        setNotes(saved);
        return saved;
    };

    const writeBookmarks = (next: PdfBookmark[]) => {
        setBookmarks(saveDocBookmarks(path, next, true));
        setMenu(null);
    };

    const paintMarks = (color: ChatMarkColor, style?: ChatMarkStyle) => {
        const current = menuRef.current;
        if (!current || current.slices.length === 0) return;
        const painted: ChatMark[] = current.slices.map((slice) => {
            const text = slice.text.slice(0, CHAT_MARK_TEXT_MAX);
            return style
                ? { message: DOC_MARK_MESSAGE, color, start: slice.start, end: slice.start + text.length, text, style }
                : { message: DOC_MARK_MESSAGE, color, start: slice.start, end: slice.start + text.length, text };
        });
        const overlaps = (mark: ChatMark, slice: ChatMark) =>
            mark.start < slice.end && mark.end > slice.start;
        const kept = marksRef.current.filter((mark) => !painted.some((slice) => overlaps(mark, slice)));
        writeMarks([...kept, ...painted]);
        window.getSelection()?.removeAllRanges();
    };

    const writeDrawings = (next: PdfDrawing[]) => {
        setDrawings(saveDocDrawings(path, next, true));
    };

    const addDrawing = (drawing: PdfDrawing) => {
        writeDrawings([...drawingsRef.current, drawing]);
    };

    const eraseDrawings = (ids: string[]) => {
        const doomed = new Set(ids);
        writeDrawings(drawingsRef.current.filter((drawing) => !doomed.has(drawing.id)));
    };

    const undoDrawing = () => {
        for (let index = drawingsRef.current.length - 1; index >= 0; index -= 1) {
            const drawing = drawingsRef.current[index];
            if (drawing.shared) continue;
            writeDrawings([...drawingsRef.current.slice(0, index), ...drawingsRef.current.slice(index + 1)]);
            return;
        }
    };

    const selectDrawTool = (next: CompanionDrawTool) => {
        if (next !== "eraser") drawToolRef.current = next;
        setDrawTool(next);
    };

    const toggleDrawMode = () => {
        setMenu(null);
        setDrawMode((current) => !current);
    };

    const clearMarkHit = () => {
        const current = menuRef.current;
        if (!current || current.hit < 0) return;
        // Marks can shift under the open menu when a share update lands, so
        // delete the mark the index pointed at by value, not by position.
        const target = marksRef.current[current.hit];
        if (!target) {
            setMenu(null);
            return;
        }
        let removed = false;
        writeMarks(marksRef.current.filter((mark) => {
            if (removed) return true;
            if (mark.message === target.message && mark.color === target.color
                && mark.start === target.start && mark.end === target.end && mark.text === target.text) {
                removed = true;
                return false;
            }
            return true;
        }));
    };

    const copySelection = () => {
        const current = menuRef.current;
        const text = (current?.selected || current?.slices.map((slice) => slice.text).join("\n") || "").trim();
        setMenu(null);
        if (text) void copyTextToClipboard(text);
    };

    const createNotes = (body: string) => {
        const current = menuRef.current;
        const clipped = clipPdfAnnotationBody(body);
        if (!current || current.slices.length === 0 || !clipped) return;
        const created = current.slices.map((slice) => {
            const text = slice.text.slice(0, CHAT_MARK_TEXT_MAX);
            const name = clipPdfAnnotationName(text);
            return {
                id: newChatAnnotationId(),
                message: DOC_MARK_MESSAGE,
                start: slice.start,
                end: slice.start + text.length,
                text,
                body: clipped,
                ...(name ? { name } : {}),
            };
        });
        writeNotes([...notesRef.current, ...created], true);
        setClosedIds((existing) => {
            if (created.every((note) => !existing.has(note.id))) return existing;
            const next = new Set(existing);
            for (const note of created) next.delete(note.id);
            return next;
        });
        setMenu(null);
        window.getSelection()?.removeAllRanges();
    };

    const editNoteBody = (id: string, body: string) => {
        const note = notesRef.current.find((item) => item.id === id);
        const clipped = clipPdfAnnotationBody(body);
        if (!note || note.shared || !clipped || clipped === note.body) return;
        writeNotes(notesRef.current.map((item) => (item.id === id ? { ...item, body: clipped } : item)), true);
    };

    const renameNote = (name: string) => {
        const id = menuRef.current?.annotationId || "";
        const note = notesRef.current.find((item) => item.id === id);
        const clipped = clipPdfAnnotationName(name);
        if (!note || note.shared || !clipped || clipped === note.name) {
            setMenu(null);
            return;
        }
        writeNotes(notesRef.current.map((item) => (item.id === id ? { ...item, name: clipped } : item)), true);
        setMenu(null);
    };

    const deleteNote = () => {
        const id = menuRef.current?.annotationId || "";
        const note = notesRef.current.find((item) => item.id === id);
        if (!note || note.shared) {
            setMenu(null);
            return;
        }
        writeNotes(notesRef.current.filter((item) => item.id !== id), true);
        setMenu(null);
    };

    const placeNote = (id: string, place: ChatAnnotationPlace | null) => {
        const note = notesRef.current.find((item) => item.id === id);
        if (!note) return;
        writeNotes(notesRef.current.map((item) => {
            if (item.id !== id) return item;
            if (!place) {
                const { place: _place, ...rest } = item;
                return rest;
            }
            return { ...item, place };
        }), note.shared !== true);
    };

    const sizeNote = (id: string, size: ChatAnnotationSize | null) => {
        const note = notesRef.current.find((item) => item.id === id);
        if (!note) return;
        writeNotes(notesRef.current.map((item) => {
            if (item.id !== id) return item;
            if (!size) {
                const { size: _size, ...rest } = item;
                return rest;
            }
            return { ...item, size };
        }), note.shared !== true);
    };

    const toggleNote = (id: string) => {
        setClosedIds((existing) => {
            const next = new Set(existing);
            if (next.has(id)) next.delete(id);
            else next.add(id);
            return next;
        });
    };

    const openNoteMenu = (event: React.MouseEvent, id: string) => {
        event.preventDefault();
        event.stopPropagation();
        const note = notesRef.current.find((item) => item.id === id);
        if (!note || note.shared) {
            setMenu(null);
            return;
        }
        setMenu({ x: event.clientX, y: event.clientY, slices: [], hit: -1, selected: note.text, annotationId: id, bookmarkId: "", anchor: null });
    };

    const addBookmark = (name: string, color: PdfBookmarkColor) => {
        const current = menuRef.current;
        const host = hostRef.current;
        const anchor = current?.anchor ?? (current && current.slices.length > 0 && host
            ? { offset: current.slices[0].start, total: chatTextLength(chatTextSpans(host)) }
            : null);
        const trimmed = name.replace(/\s+/g, " ").trim();
        if (!anchor || !(anchor.total > 0) || !trimmed) {
            setMenu(null);
            return;
        }
        const bookmark = newDocBookmark(trimmed, color, anchor.offset / anchor.total);
        if (!bookmark.name) {
            setMenu(null);
            return;
        }
        writeBookmarks([...bookmarksRef.current, bookmark]);
    };

    const renameBookmark = (name: string, color: PdfBookmarkColor) => {
        const id = menuRef.current?.bookmarkId || "";
        const trimmed = name.replace(/\s+/g, " ").trim();
        if (!id || !trimmed) return;
        writeBookmarks(bookmarksRef.current.map((item) => (item.id === id ? { ...item, name: trimmed, color } : item)));
    };

    const deleteBookmark = () => {
        const id = menuRef.current?.bookmarkId || "";
        if (!id) return;
        writeBookmarks(bookmarksRef.current.filter((item) => item.id !== id));
    };

    const jumpToBookmark = (bookmark: PdfBookmark) => {
        const host = hostRef.current;
        if (!host) return;
        const spans = chatTextSpans(host);
        const total = chatTextLength(spans);
        if (total === 0) return;
        const offset = docBookmarkOffset(bookmark, total);
        const range = chatRangeForOffsets(spans, offset, Math.min(offset + 1, total));
        if (!range || typeof range.getBoundingClientRect !== "function") return;
        const scroller = docScrollerOf(host) ?? host;
        const rect = range.getBoundingClientRect();
        const view = scroller.getBoundingClientRect();
        // A short inset keeps the marked line below the pane's top edge.
        const top = scroller.scrollTop + (rect.top - view.top) - 24;
        scroller.scrollTop = Math.max(0, top);
    };

    // A rail note jump scrolls the quoted line into view and opens the card,
    // the same result as clicking the note's own pin.
    const jumpToNote = (note: ChatAnnotation) => {
        const host = hostRef.current;
        const root = rootRef.current;
        if (!host || !root) return;
        setClosedIds((existing) => {
            if (!existing.has(note.id)) return existing;
            const next = new Set(existing);
            next.delete(note.id);
            return next;
        });
        const range = chatRangeForMark(root, note);
        if (!range || typeof range.getBoundingClientRect !== "function") return;
        const scroller = docScrollerOf(host) ?? host;
        const rect = range.getBoundingClientRect();
        const view = scroller.getBoundingClientRect();
        const top = scroller.scrollTop + (rect.top - view.top) - 24;
        scroller.scrollTop = Math.max(0, top);
    };

    // The document is rendered Markdown, so a right-click is ours only when
    // there is a selection inside the page, a highlight under the pointer, a
    // note card, or a bookmark ribbon. Anything else keeps the browser menu.
    const onDocContextMenu = (event: React.MouseEvent) => {
        const root = rootRef.current;
        const host = hostRef.current;
        if (!root || !host) return;
        const target = event.target as HTMLElement | null;
        const ribbon = target?.closest?.("[data-doc-bookmark]");
        if (ribbon && root.contains(ribbon)) {
            const id = ribbon.getAttribute("data-doc-bookmark") || "";
            const bookmark = bookmarksRef.current.find((item) => item.id === id);
            if (!bookmark) {
                setMenu(null);
                return;
            }
            event.preventDefault();
            setMenu({ x: event.clientX, y: event.clientY, slices: [], hit: -1, selected: bookmark.name, annotationId: "", bookmarkId: id, anchor: null });
            return;
        }
        const card = target?.closest?.("[data-chat-annotation-id]");
        if (card && root.contains(card)) {
            if (target?.closest?.("textarea")) return;
            event.preventDefault();
            const id = card.getAttribute("data-chat-annotation-id") || "";
            const note = notesRef.current.find((item) => item.id === id);
            if (!note || note.shared) {
                setMenu(null);
                return;
            }
            setMenu({ x: event.clientX, y: event.clientY, slices: [], hit: -1, selected: note.text, annotationId: id, bookmarkId: "", anchor: null });
            return;
        }
        let slices: ChatSelectionSlice[] = [];
        let selected = "";
        const selection = window.getSelection();
        if (selection && !selection.isCollapsed && selection.rangeCount > 0) {
            const range = selection.getRangeAt(0);
            if (root.contains(range.commonAncestorContainer)) {
                slices = chatSelectionSlices(root, range).filter((slice) => slice.message === DOC_MARK_MESSAGE);
                selected = selection.toString();
            }
        }
        let hit = -1;
        let anchor: DocAnchor | null = null;
        if (typeof document.caretRangeFromPoint === "function") {
            const caret = document.caretRangeFromPoint(event.clientX, event.clientY);
            if (caret && host.contains(caret.startContainer)) {
                const spans = chatTextSpans(host);
                const offset = chatOffsetForPoint(spans, caret.startContainer, caret.startOffset, "start");
                const total = chatTextLength(spans);
                if (total > 0) anchor = { offset, total };
                if (slices.length === 0) {
                    hit = chatMarkAtOffset(spans, marksRef.current, DOC_MARK_MESSAGE, offset);
                }
            }
        }
        // While the pen is on, a bare right-click still offers 退出手绘.
        if (slices.length === 0 && hit < 0 && !anchor && !drawMode) {
            setMenu(null);
            return;
        }
        event.preventDefault();
        setMenu({ x: event.clientX, y: event.clientY, slices, hit, selected, annotationId: "", bookmarkId: "", anchor });
    };

    const hitBookmark = menu ? bookmarks.find((item) => item.id === menu.bookmarkId) : undefined;
    const annotationAnchorKey = [
        notes.map((note) => `${note.id}:${note.start}:${note.end}`).join("|"),
        String(domTick),
        path,
    ].join("||");

    const railBookmarks: AnnotatorRailItem[] = bookmarks.map((bookmark) => ({
        id: bookmark.id,
        label: bookmark.name,
        swatch: <DocBookmarkRibbon color={bookmark.color} />,
        onJump: () => jumpToBookmark(bookmark),
    }));
    const railNotes: AnnotatorRailItem[] = notes.map((note) => ({
        id: note.id,
        label: note.name || note.text.replace(/\s+/g, " ").trim() || say("批注"),
        onJump: () => jumpToNote(note),
    }));

    return (
        <div
            ref={rootRef}
            className="companion-doc-annotator"
            data-testid="doc-annotator"
            style={{ minHeight: "100%" }}
            onContextMenu={onDocContextMenu}
        >
            {bookmarks.length + notes.length > 0 ? (
                <CompanionAnnotatorRail lang={lang} bookmarks={railBookmarks} notes={railNotes} testId="doc-annotator-rail" />
            ) : null}
            <div ref={bodyRef} className="companion-doc-annotator-body" data-testid="doc-annotator-body">
            <div className="companion-doc-draw" data-testid="doc-draw" onContextMenu={(event) => { event.preventDefault(); event.stopPropagation(); }}>
                {drawMode ? (
                    <div className="companion-pdf-draw-panel" data-testid="doc-draw-panel">
                        <div className="companion-pdf-draw-colors" role="group" aria-label={say("手绘颜色")}>
                            {PDF_MARK_COLORS.map((option) => (
                                <button
                                    key={option.id}
                                    type="button"
                                    className="companion-pdf-menu-color"
                                    data-testid={`doc-draw-color-${option.id}`}
                                    aria-label={say(`${option.label}画笔`)}
                                    aria-pressed={drawColor === option.id}
                                    disabled={drawTool === "eraser"}
                                    title={say(`${option.label}画笔`)}
                                    onClick={() => setDrawColor(option.id)}
                                >
                                    <span style={{ background: COMPANION_DRAWING_INK[option.id] }} />
                                </button>
                            ))}
                        </div>
                        <span className="companion-pdf-draw-sep" aria-hidden="true" />
                        <div className="companion-pdf-draw-tools" role="group" aria-label={say("手绘工具")}>
                            <button
                                type="button"
                                data-testid="doc-draw-line"
                                aria-label={say("画线")}
                                aria-pressed={drawTool === "line"}
                                title={say("画线")}
                                onClick={() => selectDrawTool("line")}
                            >
                                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                                    <path d="M5 19 19 5" />
                                    <circle cx="5" cy="19" r="1.5" fill="currentColor" stroke="none" />
                                    <circle cx="19" cy="5" r="1.5" fill="currentColor" stroke="none" />
                                </svg>
                            </button>
                            <button
                                type="button"
                                data-testid="doc-draw-box"
                                aria-label={say("画框")}
                                aria-pressed={drawTool === "box"}
                                title={say("画框")}
                                onClick={() => selectDrawTool("box")}
                            >
                                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                                    <rect x="4" y="6" width="16" height="12" rx="1.5" />
                                </svg>
                            </button>
                            <button
                                type="button"
                                data-testid="doc-draw-eraser"
                                aria-label={say("擦除")}
                                aria-pressed={drawTool === "eraser"}
                                title={say("擦除 — 圈选要清除的线或框")}
                                onClick={() => selectDrawTool(drawTool === "eraser" ? drawToolRef.current : "eraser")}
                            >
                                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                                    <path d="m7 21-4.3-4.3a2.4 2.4 0 0 1 0-3.4l9.6-9.6a2.4 2.4 0 0 1 3.4 0l5.6 5.6a2.4 2.4 0 0 1 0 3.4L13 21" />
                                    <path d="M22 21H7" />
                                    <path d="m5 11 9 9" />
                                </svg>
                            </button>
                        </div>
                        <span className="companion-pdf-draw-sep" aria-hidden="true" />
                        <div className="companion-pdf-draw-tools">
                            <button
                                type="button"
                                data-testid="doc-draw-undo"
                                aria-label={say("撤销上一笔")}
                                disabled={!drawings.some((item) => !item.shared)}
                                title={say("撤销上一笔")}
                                onClick={undoDrawing}
                            >
                                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                                    <path d="M9 7 4 12l5 5" />
                                    <path d="M4 12h9a5 5 0 0 1 5 5v1" />
                                </svg>
                            </button>
                        </div>
                    </div>
                ) : null}
                <button
                    type="button"
                    className="companion-pdf-draw-toggle"
                    data-testid="doc-draw-toggle"
                    aria-label={say("手绘")}
                    aria-pressed={drawMode}
                    title={drawMode ? say("退出手绘") : say("手绘 — 在文档上画线或框")}
                    onClick={toggleDrawMode}
                >
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                        <path d="M4 20h4L18.5 9.5a2.5 2.5 0 0 0-3.5-3.5L4 16.5V20Z" />
                        <path d="m13.5 7.5 3 3" />
                    </svg>
                </button>
            </div>
            <div ref={hostRef} data-chat-message={DOC_MARK_MESSAGE} data-testid="doc-annotator-page" style={{ position: "relative" }}>
                {children}
            </div>
            {bookmarks.map((bookmark) => {
                const tick = tickLayout[bookmark.id];
                if (!tick) return null;
                return (
                    <button
                        key={bookmark.id}
                        type="button"
                        className="companion-pdf-bookmark-tick companion-doc-bookmark-tick"
                        data-doc-bookmark={bookmark.id}
                        data-testid={`doc-bookmark-tick-${bookmark.id}`}
                        title={bookmark.name}
                        aria-label={say("书签") + `：${bookmark.name}`}
                        tabIndex={-1}
                        style={{
                            top: `${tick.top}px`,
                            right: `${tick.lane * PDF_BOOKMARK_TICK_LANE_PX}px`,
                            width: PDF_BOOKMARK_TICK_WIDTH_PX,
                            height: PDF_BOOKMARK_TICK_PX,
                        } as CSSProperties}
                        onMouseDown={(event) => {
                            if (event.button !== 0) return;
                            event.preventDefault();
                            event.stopPropagation();
                        }}
                        onClick={(event) => {
                            event.preventDefault();
                            event.stopPropagation();
                            jumpToBookmark(bookmark);
                        }}
                    >
                        <DocBookmarkRibbon color={bookmark.color} />
                    </button>
                );
            })}
            <CompanionDrawingLayer
                page={DOC_DRAWING_PAGE}
                drawings={drawings}
                active={drawMode}
                color={drawColor}
                tool={drawTool}
                onAdd={addDrawing}
                onErase={eraseDrawings}
                testId="doc-drawing-layer"
            />
            <CompanionChatAnnotationLayer
                logRef={rootRef}
                notes={notes}
                lang={lang || getCompanionUiLang()}
                anchorKey={annotationAnchorKey}
                closed={closedIds}
                onToggle={toggleNote}
                onBody={editNoteBody}
                onPlace={placeNote}
                onSize={sizeNote}
                onContextMenu={openNoteMenu}
            />
            </div>
            {menu ? (
                <CompanionChatMarkMenu
                    lang={lang}
                    state={menu}
                    quote={menu.selected || menu.slices.map((slice) => slice.text).join(" ")}
                    hasSelection={menu.slices.length > 0}
                    canPaint={highlightsReady && menu.slices.length > 0}
                    canClear={menu.hit >= 0}
                    canAnnotate={menu.slices.length > 0}
                    canRenameAnnotation={notes.some((note) => note.id === menu.annotationId && note.shared !== true)}
                    canDeleteAnnotation={notes.some((note) => note.id === menu.annotationId && note.shared !== true)}
                    annotationName={notes.find((note) => note.id === menu.annotationId)?.name || ""}
                    canBookmark={Boolean(menu.anchor) || menu.slices.length > 0}
                    canRenameBookmark={Boolean(hitBookmark)}
                    canDeleteBookmark={Boolean(hitBookmark)}
                    bookmarkName={hitBookmark?.name || ""}
                    bookmarkColorId={hitBookmark?.color || "yellow"}
                    onCopy={copySelection}
                    onPaint={paintMarks}
                    onClear={clearMarkHit}
                    onAnnotate={createNotes}
                    onRenameAnnotation={renameNote}
                    onDeleteAnnotation={deleteNote}
                    onAddBookmark={addBookmark}
                    onRenameBookmark={renameBookmark}
                    onDeleteBookmark={deleteBookmark}
                    drawActive={drawMode}
                    onToggleDraw={toggleDrawMode}
                    onClose={() => setMenu(null)}
                />
            ) : null}
        </div>
    );
}
