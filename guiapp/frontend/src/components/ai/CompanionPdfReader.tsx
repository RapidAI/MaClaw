import React, { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { PreviewTaskResultFile, TranslateFileCompanionSelection } from "../../../wailsjs/go/main/App";
import { copyTextToClipboard } from "./AssistantReplyCopyButton";
import type { CodePreviewTheme } from "./FileTabBar";
import {
    isPdfInlineDataURL,
    isTaskResultPdfPreviewURL,
    localizeTaskResultPreviewError,
    objectURLFromPdfDataURL,
    type TaskResultPreviewPayload,
} from "./taskResultPreview";
import {
    PDF_MARK_COLORS,
    loadPdfMarks,
    newPdfMarkId,
    pdfMarkFill,
    pdfMarkPathKey,
    savePdfMarks,
    type PdfMark,
    type PdfMarkColor,
    type PdfMarkRect,
} from "../filecompanion/pdfMarks";
import {
    beginPdfTranslation,
    failPdfTranslation,
    finishPdfTranslation,
    clampPdfTranslationPlace,
    loadPdfTranslationDrafts,
    loadPdfTranslations,
    newPdfTranslationId,
    pdfTranslationError,
    pdfTranslationPlace,
    rememberPdfTranslationPlace,
    savePdfTranslations,
    subscribePdfTranslations,
    type PdfTranslation,
    type PdfTranslationDraft,
    type PdfTranslationPlace,
} from "../filecompanion/pdfTranslations";
import { subscribeCompanionReaderState } from "../filecompanion/companionReaderState";

export interface CompanionPdfReaderProps {
    absPath: string;
    dataUrl?: string;
    previewUrl?: string;
    theme: CodePreviewTheme;
    lang: string;
}

type PdfTextItem = { str?: string; transform?: number[]; width?: number; height?: number };

type PdfViewport = { width: number; height: number; scale: number; transform: number[] };

type PdfPage = {
    getViewport: (params: { scale: number }) => PdfViewport;
    getTextContent: () => Promise<{ items: PdfTextItem[] }>;
    render: (params: { canvasContext: CanvasRenderingContext2D; viewport: PdfViewport }) => { promise: Promise<void>; cancel: () => void };
};

type PdfDocument = {
    numPages: number;
    getPage: (page: number) => Promise<PdfPage>;
    destroy?: () => Promise<void> | void;
};

type PdfLoadingTask = { promise: Promise<PdfDocument>; destroy: () => void };

type SelectionPart = { page: number; rects: PdfMarkRect[]; text: string; clipped?: boolean };

type MenuState = {
    x: number;
    y: number;
    hitId: string;
    hitNoteId: string;
    parts: SelectionPart[];
};

type ShownTranslation = PdfTranslation & { pending?: boolean };

const PDF_TRANSLATE_CHARS = 4000;

function clipTranslateText(text: string): string {
    const chars = Array.from(text);
    if (chars.length <= PDF_TRANSLATE_CHARS) return text;
    return chars.slice(0, PDF_TRANSLATE_CHARS).join("");
}

function translateErrorText(err: unknown): string {
    const raw = err instanceof Error ? err.message : String(err ?? "");
    const text = raw.trim();
    if (!text) return "翻译失败，请稍后重试";
    if (text.includes("path is outside the open file")) return "这个文件没有打开";
    if (text.includes("file companion is not running")) return "伴读还没有打开";
    if (text.includes("MaClaw Hub login is required")) return "需要先在主窗口登录";
    if (text.includes("LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST") || /insufficient credits/i.test(text)) return "本次请求额度不足";
    return text;
}

function pendingTranslation(draft: PdfTranslationDraft | undefined): ShownTranslation | null {
    if (!draft) return null;
    const place = pdfTranslationPlace(draft.id);
    return { id: draft.id, text: draft.text, translation: "", parts: draft.parts, pending: true, ...(place ? { place } : {}) };
}

function pdfjsPublicUrl(path: string): string {
    const base = import.meta.env.BASE_URL || "./";
    const root = base.endsWith("/") ? base : `${base}/`;
    return new URL(`${root}pdfjs/${path}`, window.location.href).href;
}

function multiplyTransform(m1: number[], m2: number[]): number[] {
    const [a1, b1, c1, d1, e1, f1] = m1;
    const [a2, b2, c2, d2, e2, f2] = m2;
    return [
        a1 * a2 + c1 * b2,
        b1 * a2 + d1 * b2,
        a1 * c2 + c1 * d2,
        b1 * c2 + d1 * d2,
        a1 * e2 + c1 * f2 + e1,
        b1 * e2 + d1 * f2 + f1,
    ];
}

function textBox(item: PdfTextItem, viewport: PdfViewport): React.CSSProperties | null {
    const source = item.transform;
    if (!source || source.length < 6) return null;
    const tx = multiplyTransform(viewport.transform, source);
    const fontHeight = Math.hypot(tx[2], tx[3]) || Math.hypot(tx[0], tx[1]) || 12;
    const style: React.CSSProperties = {
        position: "absolute",
        left: tx[4],
        top: tx[5] - fontHeight,
        fontSize: fontHeight,
        lineHeight: 1,
        color: "transparent",
        whiteSpace: "pre",
        transformOrigin: "0 0",
        cursor: "text",
    };
    const base = Math.hypot(source[0], source[1]) || fontHeight;
    if (item.width && base > 0) {
        const width = (item.width / base) * Math.hypot(tx[0], tx[1]);
        if (width > 0) style.width = width;
    }
    return style;
}

function clampRect(rect: PdfMarkRect): PdfMarkRect | null {
    const x = Math.min(1, Math.max(0, rect.x));
    const y = Math.min(1, Math.max(0, rect.y));
    const right = Math.min(1, Math.max(x, rect.x + rect.w));
    const bottom = Math.min(1, Math.max(y, rect.y + rect.h));
    if (right - x < 0.001 || bottom - y < 0.001) return null;
    return { x, y, w: right - x, h: bottom - y };
}

type SelectionBox = { left: number; top: number; right: number; bottom: number; space?: boolean };

function selectionBox(rect: { left: number; top: number; right: number; bottom: number }): SelectionBox | null {
    if (rect.right - rect.left < 0.5 || rect.bottom - rect.top < 0.5) return null;
    return { left: rect.left, top: rect.top, right: rect.right, bottom: rect.bottom };
}

function boxMid(box: SelectionBox): number {
    return (box.top + box.bottom) / 2;
}

function horizontalGap(a: SelectionBox, b: SelectionBox): number {
    if (a.right < b.left) return b.left - a.right;
    if (b.right < a.left) return a.left - b.right;
    return 0;
}

// A parent quad from Range.getClientRects covers several line boxes. A tall
// formula line covers none, so height alone must not drop it.
function dropContainerBoxes(boxes: SelectionBox[]): SelectionBox[] {
    if (boxes.length < 2) return boxes;
    const heights = boxes.map((box) => box.bottom - box.top).sort((a, b) => a - b);
    const median = heights[Math.floor((heights.length - 1) / 2)];
    const kept = boxes.filter((box) => {
        if (box.bottom - box.top <= median * 2) return true;
        let covered = 0;
        for (const other of boxes) {
            if (other === box) continue;
            const encloses = box.top <= other.top + 1
                && box.bottom >= other.bottom - 1
                && box.left <= other.left + 1
                && box.right >= other.right - 1
                && box.bottom - box.top > other.bottom - other.top + 1;
            if (encloses && ++covered >= 2) return false;
        }
        return true;
    });
    return kept.length > 0 ? kept : boxes;
}

function unionBoxes(boxes: SelectionBox[]): SelectionBox {
    let left = boxes[0].left;
    let right = boxes[0].right;
    let top = boxes[0].top;
    let bottom = boxes[0].bottom;
    for (let i = 1; i < boxes.length; i += 1) {
        const box = boxes[i];
        left = Math.min(left, box.left);
        right = Math.max(right, box.right);
        top = Math.min(top, box.top);
        bottom = Math.max(bottom, box.bottom);
    }
    return { left, top, right, bottom };
}

// pdf.js keeps a gap inside a run only up to 0.6em (SPACE_IN_FLOW_MAX_FACTOR).
// Anything wider is its own whitespace run, and that run fills the gutter so
// the boxes on either side look adjacent. A gap counts as a gutter when it is
// wider than a word space and wider than the other gaps on that baseline.
// Equal stretched gaps are justification and stay one line.
function isColumnBreak(ordered: SelectionBox[], gaps: number[], index: number): boolean {
    const gap = gaps[index];
    const prev = ordered[index];
    const box = ordered[index + 1];
    const limit = 0.75 * Math.min(prev.bottom - prev.top, box.bottom - box.top);
    if (gap <= limit) return false;
    if (gaps.length < 3) return true;
    const others: number[] = [];
    for (let i = 0; i < gaps.length; i += 1) {
        if (i !== index) others.push(gaps[i]);
    }
    others.sort((a, b) => a - b);
    const median = others[Math.floor((others.length - 1) / 2)];
    return gap > median + 1;
}

function splitBaseline(boxes: SelectionBox[]): SelectionBox[] {
    const ordered = [...boxes].sort((a, b) => a.left - b.left || a.top - b.top);
    const content = ordered.filter((box) => !box.space);
    if (content.length < 2) return content;
    const gaps: number[] = [];
    for (let i = 1; i < content.length; i += 1) gaps.push(horizontalGap(content[i - 1], content[i]));
    const parts: SelectionBox[][] = [[content[0]]];
    for (let i = 0; i < gaps.length; i += 1) {
        if (isColumnBreak(content, gaps, i)) parts.push([content[i + 1]]);
        else parts[parts.length - 1].push(content[i + 1]);
    }
    return parts.map(unionBoxes);
}

// One PDF line is many runs, and the two columns of a page share a baseline.
// The shorter band is the match. A tall formula must not swallow the next line,
// and the stored midline stays put so tight lines cannot chain.
function mergeTextLineBoxes(boxes: SelectionBox[]): SelectionBox[] {
    const sorted = [...boxes].sort((a, b) => boxMid(a) - boxMid(b) || a.left - b.left);
    const groups: Array<{ mid: number; band: number; boxes: SelectionBox[] }> = [];
    for (const box of sorted) {
        const mid = boxMid(box);
        const band = (box.bottom - box.top) * 0.45;
        let group: (typeof groups)[number] | undefined;
        for (let i = groups.length - 1; i >= 0; i -= 1) {
            const item = groups[i];
            // No earlier line can match once the gap exceeds this box's own band.
            if (mid - item.mid > band) break;
            if (mid - item.mid <= Math.min(item.band, band)) {
                group = item;
                break;
            }
        }
        if (!group) {
            groups.push({ mid, band, boxes: [box] });
            continue;
        }
        group.boxes.push(box);
    }
    const lines: SelectionBox[] = [];
    for (const group of groups) lines.push(...splitBaseline(group.boxes));
    return lines;
}

// The span's border box is the PDF run. A character offset clips that width.
// Calling getClientRects on the sub-range is what drops later lines.
function spanSelectionBox(range: Range, span: HTMLElement): SelectionBox | null {
    const whole = selectionBox(span.getBoundingClientRect());
    if (!whole) return null;
    const text = span.firstChild;
    if (!text || text.nodeType !== Node.TEXT_NODE) return whole;
    const length = (text as Text).length;
    if (length <= 0) return whole;
    let start = 0;
    let end = length;
    if (range.startContainer === text) start = Math.min(Math.max(range.startOffset, 0), length);
    if (range.endContainer === text) end = Math.min(Math.max(range.endOffset, 0), length);
    if (end <= start) return null;
    if (start === 0 && end === length) return whole;
    const width = whole.right - whole.left;
    const left = whole.left + width * (start / length);
    const right = whole.left + width * (end / length);
    if (right - left < 0.5) return null;
    return { left, top: whole.top, right, bottom: whole.bottom };
}

// A whitespace run wider than an in-flow space is the gap itself, not a word.
function wideSpaceRun(span: HTMLElement, box: SelectionBox): boolean {
    const text = span.textContent ?? "";
    if (!text || !/^[\s\u00a0]+$/.test(text)) return false;
    return box.right - box.left > (box.bottom - box.top) * 0.6;
}

// contains() first. A range mock that only names the span would reject the page.
function rangeHitsPage(range: Range, page: HTMLElement): boolean {
    const start = range.startContainer;
    const end = range.endContainer;
    if (start && page.contains(start)) return true;
    if (end && end !== start && page.contains(end)) return true;
    if (typeof range.intersectsNode !== "function") return true;
    try {
        return range.intersectsNode(page);
    } catch {
        return true;
    }
}

function selectionLineBoxes(range: Range, page: HTMLElement): SelectionBox[] {
    const spans = page.querySelectorAll<HTMLElement>(".companion-pdf-text span");
    const boxes: SelectionBox[] = [];
    if (typeof range.intersectsNode === "function" && spans.length > 0) {
        let seen = false;
        for (const span of spans) {
            let hit = false;
            let threw = false;
            try {
                hit = range.intersectsNode(span);
            } catch {
                threw = true;
            }
            // Spans follow the range. After the first hit, the next miss is the end.
            if (!hit) {
                if (seen && !threw) break;
                continue;
            }
            seen = true;
            const box = spanSelectionBox(range, span);
            if (!box) continue;
            if (wideSpaceRun(span, box)) box.space = true;
            boxes.push(box);
        }
    }
    if (boxes.length > 0) return mergeTextLineBoxes(boxes);
    // A parent quad only shows up in Range.getClientRects. A span box is one run.
    const raw: SelectionBox[] = [];
    for (const rect of range.getClientRects()) {
        const box = selectionBox(rect);
        if (box) raw.push(box);
    }
    return mergeTextLineBoxes(dropContainerBoxes(raw));
}

function selectionParts(root: HTMLElement): SelectionPart[] {
    const selection = window.getSelection();
    if (!selection || selection.isCollapsed || selection.rangeCount < 1) return [];
    // Highlight storage still trims this to 500 characters. Translation uses
    // the same string once, so a longer selection is not cut off first.
    const raw = selection.toString().replace(/\s+/g, " ").trim();
    const text = clipTranslateText(raw);
    const clipped = Array.from(raw).length > PDF_TRANSLATE_CHARS;
    const range = selection.getRangeAt(0);
    const parts: SelectionPart[] = [];
    root.querySelectorAll<HTMLElement>("[data-pdf-page]").forEach((page) => {
        const pageNumber = Number(page.dataset.pdfPage);
        if (!pageNumber || !rangeHitsPage(range, page)) return;
        const box = page.getBoundingClientRect();
        if (box.width < 1 || box.height < 1) return;
        const rects: PdfMarkRect[] = [];
        for (const item of selectionLineBoxes(range, page)) {
            const left = Math.max(item.left, box.left);
            const right = Math.min(item.right, box.right);
            const top = Math.max(item.top, box.top);
            const bottom = Math.min(item.bottom, box.bottom);
            if (right - left < 0.5 || bottom - top < 0.5) continue;
            const rect = clampRect({
                x: (left - box.left) / box.width,
                y: (top - box.top) / box.height,
                w: (right - left) / box.width,
                h: (bottom - top) / box.height,
            });
            if (rect) rects.push(rect);
        }
        if (rects.length > 0) parts.push(clipped ? { page: pageNumber, rects, text, clipped: true } : { page: pageNumber, rects, text });
    });
    return parts;
}

function markAt(marks: PdfMark[], page: number, x: number, y: number): PdfMark | null {
    for (let i = marks.length - 1; i >= 0; i -= 1) {
        const mark = marks[i];
        if (mark.page !== page) continue;
        if (mark.rects.some((rect) => x >= rect.x && y >= rect.y && x <= rect.x + rect.w && y <= rect.y + rect.h)) return mark;
    }
    return null;
}

type PdfMenuGlyphKind = "highlight" | "translate" | "copy" | "check" | "close" | "erase";

function PdfMenuGlyph({ kind }: { kind: PdfMenuGlyphKind }) {
    if (kind === "highlight") {
        return (
            <svg className="companion-pdf-menu-glyph" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <rect x="3" y="14" width="11" height="3.6" rx="1" fill="#ffe08a" stroke="none" />
                <path d="M14.2 12.8 18.8 8.2 21 10.4 16.4 15H14.2z" />
            </svg>
        );
    }
    if (kind === "translate") {
        return (
            <svg className="companion-pdf-menu-glyph" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d="M2 5h12" />
                <path d="M7 2v3" />
                <path d="m5 8 6 6" />
                <path d="m4 14 6-6 2-3" />
                <path d="m22 22-5-10-5 10" />
                <path d="M14 18h6" />
            </svg>
        );
    }
    if (kind === "copy") {
        return (
            <svg className="companion-pdf-menu-glyph" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <rect x="9" y="9" width="11" height="11" rx="2" />
                <path d="M15 5.5A1.5 1.5 0 0 0 13.5 4H6a2 2 0 0 0-2 2v7.5A1.5 1.5 0 0 0 5.5 15" />
            </svg>
        );
    }
    if (kind === "check") {
        return (
            <svg className="companion-pdf-menu-glyph" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d="m5 12.5 4.5 4.5L19 7.5" />
            </svg>
        );
    }
    if (kind === "close") {
        return (
            <svg className="companion-pdf-menu-glyph" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden="true">
                <path d="m6 6 12 12" />
                <path d="m18 6-12 12" />
            </svg>
        );
    }
    return (
        <svg className="companion-pdf-menu-glyph" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="m14 5 5 5-8.5 8.5a2 2 0 0 1-2.8 0l-2.2-2.2a2 2 0 0 1 0-2.8z" />
            <path d="m8.5 10.5 5 5" />
            <path d="M5 20h14" />
        </svg>
    );
}

function noteAt(notes: PdfTranslation[], page: number, x: number, y: number): PdfTranslation | null {
    for (let i = notes.length - 1; i >= 0; i -= 1) {
        const note = notes[i];
        const part = note.parts.find((item) => item.page === page);
        if (!part) continue;
        if (part.rects.some((rect) => x >= rect.x && y >= rect.y && x <= rect.x + rect.w && y <= rect.y + rect.h)) return note;
    }
    return null;
}

/** Selection text shown as a quote in the menu head. One line of context is
 *  enough to confirm the right phrase; the rest stays in the selection. */
function menuQuote(text: string): string {
    const line = text.replace(/\s+/g, " ").trim();
    const chars = Array.from(line);
    return chars.length > 96 ? `${chars.slice(0, 96).join("")}…` : line;
}

type PdfSelectionMenuProps = {
    state: MenuState;
    selectionText: string;
    onTranslate: () => void;
    onCopy: () => void;
    onPaint: (color: PdfMarkColor) => void;
    onClearNote: () => void;
    onClearMark: () => void;
    onClose: () => void;
};

const MENU_VIEWPORT_GAP = 8;

/**
 * Right-click menu for the reader. It is a real menu: focus lands on the first
 * item, arrows move between items, and it never renders past the window edge.
 */
function PdfSelectionMenu({
    state,
    selectionText,
    onTranslate,
    onCopy,
    onPaint,
    onClearNote,
    onClearMark,
    onClose,
}: PdfSelectionMenuProps) {
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
    }, [state.x, state.y, selectionText, state.hitId, state.hitNoteId]);

    useEffect(() => {
        rootRef.current?.querySelector<HTMLElement>('[role="menuitem"]')?.focus({ preventScroll: true });
    }, []);

    // The page moves under a fixed menu, so the menu would point at nothing.
    // Scrolling inside the menu itself must not close it.
    useEffect(() => {
        const onScroll = (event: Event) => {
            const target = event.target as Node | null;
            if (target && rootRef.current?.contains(target)) return;
            onClose();
        };
        window.addEventListener("scroll", onScroll, true);
        return () => window.removeEventListener("scroll", onScroll, true);
    }, [onClose]);

    // A mouse press runs the action and closes the menu, so the click that
    // follows must not run it a second time. A press that already acted is
    // recorded here; the click handler consumes it and returns.
    const pressHandledRef = useRef<EventTarget | null>(null);

    const activate = (action: () => void) => ({
        onMouseDown: (event: React.MouseEvent) => {
            if (event.button !== 0) return;
            // Run on press, like a native menu, before the outside-click
            // listener in the reader sees the same event.
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
            // Keyboard activation (Enter/Space) arrives as a detail-0 click.
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

    const quote = selectionText ? menuQuote(selectionText) : "";
    const hasSelection = selectionText.trim().length > 0;
    // The visible quote is trimmed to one line; the tooltip carries the whole
    // selection so a truncated head can still be read in full.
    const quoteTitle = selectionText.replace(/\s+/g, " ").trim();

    return (
        <div
            ref={rootRef}
            className="companion-pdf-menu"
            data-testid="pdf-highlight-menu"
            role="menu"
            aria-label="选中文本操作"
            style={{ left: place.left, top: place.top }}
            onMouseDown={(event) => event.stopPropagation()}
            onContextMenu={(event) => {
                event.preventDefault();
                event.stopPropagation();
            }}
            onKeyDown={onKeyDown}
        >
            {quote ? (
                <p className="companion-pdf-menu-quote" data-testid="pdf-menu-quote" title={quoteTitle}>{quote}</p>
            ) : null}
            {hasSelection ? (
                <div className="companion-pdf-menu-group">
                    <button
                        type="button"
                        role="menuitem"
                        data-testid="pdf-translate"
                        className="companion-pdf-menu-item companion-pdf-menu-item--accent"
                        {...activate(onTranslate)}
                    >
                        <PdfMenuGlyph kind="translate" />
                        <span className="companion-pdf-menu-item-label">翻译</span>
                        <span className="companion-pdf-menu-item-hint">中文</span>
                    </button>
                    <button
                        type="button"
                        role="menuitem"
                        data-testid="pdf-copy"
                        className="companion-pdf-menu-item"
                        {...activate(onCopy)}
                    >
                        <PdfMenuGlyph kind="copy" />
                        <span className="companion-pdf-menu-item-label">复制原文</span>
                    </button>
                </div>
            ) : null}
            {state.parts.length > 0 ? (
                <div className="companion-pdf-menu-group">
                    <p className="companion-pdf-menu-title"><PdfMenuGlyph kind="highlight" />高亮</p>
                    <div className="companion-pdf-menu-colors" role="group" aria-label="高亮颜色">
                        {PDF_MARK_COLORS.map((color) => (
                            <button
                                key={color.id}
                                type="button"
                                role="menuitem"
                                data-testid={`pdf-highlight-${color.id}`}
                                className="companion-pdf-menu-color"
                                aria-label={`${color.label}高亮`}
                                title={`${color.label}高亮`}
                                {...activate(() => onPaint(color.id))}
                            >
                                <span style={{ background: color.fill }} />
                            </button>
                        ))}
                    </div>
                </div>
            ) : null}
            {state.hitNoteId || state.hitId ? (
                <div className="companion-pdf-menu-group">
                    {state.hitNoteId ? (
                        <button
                            type="button"
                            role="menuitem"
                            data-testid="pdf-translate-clear"
                            className="companion-pdf-menu-item companion-pdf-menu-item--danger"
                            {...activate(onClearNote)}
                        >
                            <PdfMenuGlyph kind="erase" />
                            <span className="companion-pdf-menu-item-label">取消翻译</span>
                        </button>
                    ) : null}
                    {state.hitId ? (
                        <button
                            type="button"
                            role="menuitem"
                            data-testid="pdf-highlight-clear"
                            className="companion-pdf-menu-item companion-pdf-menu-item--danger"
                            {...activate(onClearMark)}
                        >
                            <PdfMenuGlyph kind="erase" />
                            <span className="companion-pdf-menu-item-label">取消高亮</span>
                        </button>
                    ) : null}
                </div>
            ) : null}
        </div>
    );
}

function PdfPageView({
    pdf,
    pageNumber,
    width,
    marks,
    notes,
    openNoteId,
    onToggleNote,
    onPlaceNote,
    active,
}: {
    pdf: PdfDocument;
    pageNumber: number;
    width: number;
    marks: PdfMark[];
    notes: ShownTranslation[];
    openNoteId: string;
    onToggleNote: (id: string) => void;
    onPlaceNote: (id: string, place: PdfTranslationPlace | null) => void;
    active: boolean;
}) {
    const canvasRef = useRef<HTMLCanvasElement>(null);
    const pageRef = useRef<HTMLDivElement>(null);
    const popScrollKey = useRef("");
    const dragStopRef = useRef<(() => void) | null>(null);
    // The live offset is imperative. A re-render commits the last saved place
    // and would pull the card back before the pointer is released.
    const dragVisualRef = useRef<{ span: HTMLElement; x: number; y: number } | null>(null);
    const copiedTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
    const [page, setPage] = useState<PdfPage | null>(null);
    const [popAbove, setPopAbove] = useState(false);
    const [copiedId, setCopiedId] = useState("");
    const [copyFailedId, setCopyFailedId] = useState("");
    const [items, setItems] = useState<PdfTextItem[]>([]);
    const [natural, setNatural] = useState({ width: 612, height: 792 });

    useEffect(() => {
        if (!active) return undefined;
        let cancel = false;
        void pdf.getPage(pageNumber).then((next) => {
            if (cancel) return;
            setPage(next);
            const base = next.getViewport({ scale: 1 });
            setNatural({ width: base.width || 612, height: base.height || 792 });
            void next.getTextContent().then((content) => {
                if (!cancel) setItems(content.items || []);
            }).catch(() => {
                if (!cancel) setItems([]);
            });
        }).catch(() => {
            if (!cancel) setPage(null);
        });
        return () => { cancel = true; };
    }, [pdf, pageNumber, active]);

    const scale = natural.width > 0 && width > 48 ? Math.min(2.5, Math.max(0.4, (width - 24) / natural.width)) : 1;
    const viewport = page ? page.getViewport({ scale }) : { width: natural.width * scale, height: natural.height * scale, scale, transform: [scale, 0, 0, -scale, 0, natural.height * scale] };

    useEffect(() => {
        const canvas = canvasRef.current;
        if (!page || !canvas) return undefined;
        const context = canvas.getContext("2d");
        if (!context) return undefined;
        const view = page.getViewport({ scale });
        canvas.width = Math.max(1, Math.floor(view.width));
        canvas.height = Math.max(1, Math.floor(view.height));
        const task = page.render({ canvasContext: context, viewport: view });
        return () => { task.cancel(); };
    }, [page, scale]);

    const pageMarks = marks.filter((mark) => mark.page === pageNumber);
    const noteOpen = notes.some((note) => note.id === openNoteId && note.parts[0]?.page === pageNumber);
    const openHere = notes.find((note) => note.id === openNoteId && note.parts[0]?.page === pageNumber);

    useLayoutEffect(() => {
        const visual = dragVisualRef.current;
        if (!visual) return;
        const pageEl = visual.span.closest(".companion-pdf-page") as HTMLElement | null;
        const box = pageEl?.getBoundingClientRect();
        if (!box || box.width < 1 || box.height < 1) return;
        visual.span.style.setProperty("--pop-x", `${visual.x * box.width}px`);
        visual.span.style.setProperty("--pop-y", `${visual.y * box.height}px`);
    });

    useLayoutEffect(() => {
        const pageEl = pageRef.current;
        const pop = pageEl?.querySelector<HTMLElement>("[data-pdf-translation-pop]");
        const scroller = pageEl?.closest<HTMLElement>("[data-testid='companion-pdf-scroll']");
        const span = pop?.parentElement;
        if (!pageEl || !pop || !scroller || !span) {
            popScrollKey.current = "";
            setPopAbove((current) => (current ? false : current));
            return;
        }
        // The gesture owns the card until the pointer is released.
        if (dragVisualRef.current) return;
        if (openHere?.place) {
            // A saved drag keeps its anchor. Bring the card into the reader only
            // as far as the selection itself stays on screen. The visible bottom
            // is the client box, so the scrollbar is not treated as room.
            setPopAbove((current) => (current ? false : current));
            const popBox = pop.getBoundingClientRect();
            const spanBox = span.getBoundingClientRect();
            const scrollBox = scroller.getBoundingClientRect();
            const visibleHeight = scroller.clientHeight;
            if (popBox.height < 1 || visibleHeight < 1 || spanBox.height < 1) return;
            const visibleTop = scrollBox.top;
            const visibleBottom = scrollBox.top + visibleHeight;
            let delta = 0;
            if (popBox.top < visibleTop) delta = popBox.top - visibleTop;
            else if (popBox.bottom > visibleBottom) delta = popBox.bottom - visibleBottom;
            if (delta > 0) delta = Math.min(delta, Math.max(0, spanBox.top - visibleTop));
            else if (delta < 0) delta = Math.max(delta, Math.min(0, spanBox.bottom - visibleBottom));
            const key = `place|${openNoteId}|${Math.round(popBox.top)}|${Math.round(spanBox.top)}|${Math.round(visibleHeight)}|${Math.round(delta)}`;
            if (Math.abs(delta) < 1 || popScrollKey.current === key) return;
            popScrollKey.current = key;
            scroller.scrollTop += delta;
            return;
        }
        const spanBox = span.getBoundingClientRect();
        const scrollBox = scroller.getBoundingClientRect();
        const popHeight = pop.getBoundingClientRect().height;
        if (popHeight < 1 || scrollBox.height < 1) return;
        // Gaps match .companion-pdf-translation-pop top and --above bottom.
        const belowGap = 6;
        const aboveGap = 12;
        const fitsBelow = spanBox.bottom + belowGap + popHeight <= scrollBox.bottom + 1;
        const fitsAbove = spanBox.top - aboveGap - popHeight >= scrollBox.top - 1;
        const wantAbove = !fitsBelow && fitsAbove;
        setPopAbove((current) => (current === wantAbove ? current : wantAbove));
        if (wantAbove || fitsBelow) return;
        const hidden = spanBox.bottom + belowGap + popHeight - scrollBox.bottom;
        const key = `${openNoteId}|${Math.round(popHeight)}|${Math.round(scrollBox.height)}|${Math.round(spanBox.bottom)}`;
        if (hidden <= 1 || popScrollKey.current === key) return;
        popScrollKey.current = key;
        scroller.scrollTop += hidden;
    }, [openNoteId, openHere?.translation, openHere?.pending, openHere?.clipped, openHere?.place?.x, openHere?.place?.y, openHere?.place?.above, width, viewport.width, viewport.height]);

    useEffect(() => () => {
        dragStopRef.current?.();
        if (copiedTimer.current) clearTimeout(copiedTimer.current);
    }, []);

    const startPopDrag = (event: React.MouseEvent, note: ShownTranslation) => {
        if (event.button !== 0) return;
        event.preventDefault();
        event.stopPropagation();
        const span = (event.currentTarget as HTMLElement).closest(".companion-pdf-translation") as HTMLElement | null;
        const pageEl = span?.closest(".companion-pdf-page") as HTMLElement | null;
        if (!span || !pageEl) return;
        const pageBox = pageEl.getBoundingClientRect();
        if (pageBox.width < 1 || pageBox.height < 1) return;
        dragStopRef.current?.();
        const originX = note.place?.x ?? 0;
        const originY = note.place?.y ?? 0;
        const above = note.place ? note.place.above === true : popAbove;
        const startX = event.clientX;
        const startY = event.clientY;
        const startLeft = pageBox.left;
        const startTop = pageBox.top;
        let last = clampPdfTranslationPlace(originX, originY, above);
        let stopped = false;
        const apply = (place: PdfTranslationPlace | null, box: DOMRect) => {
            span.style.setProperty("--pop-x", `${(place?.x ?? 0) * box.width}px`);
            span.style.setProperty("--pop-y", `${(place?.y ?? 0) * box.height}px`);
        };
        // The card follows the pointer, then shifts back so its border box stays
        // inside the reader. A drop cannot park it off-screen or widen the page.
        const fitInside = (place: PdfTranslationPlace | null, box: DOMRect): PdfTranslationPlace | null => {
            apply(place, box);
            const pop = span.querySelector<HTMLElement>("[data-pdf-translation-pop]");
            const scroller = pageEl.closest<HTMLElement>("[data-testid='companion-pdf-scroll']");
            if (!pop || !scroller || box.width < 1 || box.height < 1) return place;
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
            return clampPdfTranslationPlace((place?.x ?? 0) + fixX / box.width, (place?.y ?? 0) + fixY / box.height, above);
        };
        const stop = () => {
            if (stopped) return;
            stopped = true;
            dragVisualRef.current = null;
            window.removeEventListener("mousemove", move, true);
            window.removeEventListener("mouseup", up, true);
            window.removeEventListener("blur", up);
            document.body.classList.remove("companion-pdf-translation-dragging");
            if (dragStopRef.current === stop) dragStopRef.current = null;
            const startedAbove = note.place ? note.place.above === true : above;
            const unchanged = originX === (last?.x ?? 0)
                && originY === (last?.y ?? 0)
                && startedAbove === (last?.above === true);
            if (unchanged) return;
            onPlaceNote(note.id, last);
        };
        const move = (ev: MouseEvent) => {
            ev.preventDefault();
            const pageNow = pageEl.getBoundingClientRect();
            if (pageNow.width < 1 || pageNow.height < 1) return;
            const dx = (ev.clientX - startX) - (pageNow.left - startLeft);
            const dy = (ev.clientY - startY) - (pageNow.top - startTop);
            const raw = clampPdfTranslationPlace(originX + dx / pageNow.width, originY + dy / pageNow.height, above);
            last = fitInside(raw, pageNow);
            apply(last, pageNow);
            dragVisualRef.current = { span, x: last?.x ?? 0, y: last?.y ?? 0 };
            rememberPdfTranslationPlace(note.id, last);
        };
        const up = () => stop();
        document.body.classList.add("companion-pdf-translation-dragging");
        window.addEventListener("mousemove", move, true);
        window.addEventListener("mouseup", up, true);
        window.addEventListener("blur", up);
        dragStopRef.current = stop;
    };

    return (
        <div
            ref={pageRef}
            className={noteOpen ? "companion-pdf-page companion-pdf-page--note" : "companion-pdf-page"}
            data-testid={`companion-pdf-page-${pageNumber}`}
            data-pdf-page={pageNumber}
            style={{ width: viewport.width, height: viewport.height, "--page-w": `${viewport.width}px` } as React.CSSProperties}
        >
            {active ? <canvas ref={canvasRef} className="companion-pdf-canvas" /> : null}
            {active ? pageMarks.map((mark) => mark.rects.map((rect, index) => (
                <div
                    key={`${mark.id}-${index}`}
                    className="companion-pdf-mark"
                    data-mark-id={mark.id}
                    data-color={mark.color}
                    style={{
                        left: `${rect.x * 100}%`,
                        top: `${rect.y * 100}%`,
                        width: `${rect.w * 100}%`,
                        height: `${rect.h * 100}%`,
                        background: pdfMarkFill(mark.color),
                    }}
                />
            ))) : null}
            {active ? (
                <div className="companion-pdf-text" data-testid={pageNumber === 1 ? "companion-pdf-text" : undefined}>
                    {items.map((item, index) => {
                        const text = String(item.str || "");
                        if (!text) return null;
                        const style = textBox(item, viewport);
                        if (!style) return null;
                        return <span key={index} style={style}>{text}</span>;
                    })}
                </div>
            ) : null}
            {active ? notes.map((note) => {
                const part = note.parts.find((item) => item.page === pageNumber);
                if (!part) return null;
                const iconHere = note.parts[0]?.page === pageNumber;
                return part.rects.map((rect, index) => (
                    <span
                        key={`${note.id}-${index}`}
                        className="companion-pdf-translation"
                        data-testid={index === 0 ? `pdf-translate-box-${note.id}` : undefined}
                        style={{
                            left: `${rect.x * 100}%`,
                            top: `${rect.y * 100}%`,
                            width: `${rect.w * 100}%`,
                            height: `${rect.h * 100}%`,
                            "--sel-x": `${rect.x * viewport.width}px`,
                            "--pop-x": `${(note.place?.x ?? 0) * viewport.width}px`,
                            "--pop-y": `${(note.place?.y ?? 0) * viewport.height}px`,
                        } as React.CSSProperties}
                    >
                        {iconHere && index === 0 ? (
                            <button
                                type="button"
                                className="companion-pdf-translation-icon"
                                data-testid={`pdf-translate-icon-${note.id}`}
                                data-pdf-translation-icon=""
                                {...(note.pending ? {} : { "data-pdf-translation-id": note.id })}
                                aria-label={note.pending ? "正在翻译" : "译文"}
                                aria-busy={note.pending ? true : undefined}
                                onMouseDown={(event) => {
                                    if (event.button !== 0) return;
                                    event.preventDefault();
                                    event.stopPropagation();
                                    onToggleNote(note.id);
                                }}
                            >
                                <svg className="companion-pdf-translation-icon-glyph" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                                    <path d="M3 5h9" />
                                    <path d="M7.5 3v2" />
                                    <path d="m5.5 8 5 5" />
                                    <path d="m4.5 13 5-5 1.5-2" />
                                    <path d="m20 20-3.6-7.4L12.8 20" />
                                    <path d="M14.4 17.2h4.4" />
                                </svg>
                                译文
                            </button>
                        ) : null}
                        {iconHere && index === 0 && openNoteId === note.id ? (
                            <span
                                className={(note.place ? note.place.above === true : popAbove) ? "companion-pdf-translation-pop companion-pdf-translation-pop--above" : "companion-pdf-translation-pop"}
                                role="dialog"
                                aria-label="译文"
                                data-testid={`pdf-translate-pop-${note.id}`}
                                data-pdf-translation-pop=""
                                onMouseDown={(event) => event.stopPropagation()}
                            >
                                <span
                                    className="companion-pdf-translation-chrome"
                                    onMouseDown={(event) => startPopDrag(event, note)}
                                >
                                <span
                                    className="companion-pdf-translation-grip"
                                    data-pdf-translation-grip=""
                                    title="拖动"
                                    aria-hidden="true"
                                />
                                <span className="companion-pdf-translation-head">
                                    <span className="companion-pdf-translation-kicker">
                                        {note.pending ? "正在翻译" : "中文译文"}
                                    </span>
                                    <span className="companion-pdf-translation-actions">
                                        <button
                                            type="button"
                                            className="companion-pdf-translation-action"
                                            data-testid={`pdf-translate-copy-${note.id}`}
                                            data-copy-state={copyFailedId === note.id ? "failed" : copiedId === note.id ? "copied" : undefined}
                                            title={copyFailedId === note.id ? "复制失败" : copiedId === note.id ? "已复制" : "复制译文"}
                                            aria-label={copyFailedId === note.id ? "复制失败" : copiedId === note.id ? "已复制" : "复制译文"}
                                            disabled={note.pending || !note.translation}
                                            onMouseDown={(event) => {
                                                event.preventDefault();
                                                event.stopPropagation();
                                            }}
                                            onClick={() => {
                                                void copyTextToClipboard(note.translation).then((ok) => {
                                                    if (copiedTimer.current) clearTimeout(copiedTimer.current);
                                                    if (!ok) {
                                                        setCopiedId("");
                                                        setCopyFailedId(note.id);
                                                        copiedTimer.current = setTimeout(() => setCopyFailedId(""), 1600);
                                                        return;
                                                    }
                                                    setCopyFailedId("");
                                                    setCopiedId(note.id);
                                                    copiedTimer.current = setTimeout(() => setCopiedId(""), 1400);
                                                });
                                            }}
                                        >
                                            <PdfMenuGlyph kind={copiedId === note.id ? "check" : "copy"} />
                                        </button>
                                        <button
                                            type="button"
                                            className="companion-pdf-translation-action"
                                            data-testid={`pdf-translate-close-${note.id}`}
                                            title="关闭"
                                            aria-label="关闭译文"
                                            onMouseDown={(event) => {
                                                event.preventDefault();
                                                event.stopPropagation();
                                            }}
                                            onClick={() => onToggleNote(note.id)}
                                        >
                                            <PdfMenuGlyph kind="close" />
                                        </button>
                                    </span>
                                </span>
                                </span>
                                <span className="companion-pdf-translation-body">
                                    {!note.pending && note.clipped ? <span className="companion-pdf-translation-clip">仅翻译了前 4000 字</span> : null}
                                    {note.pending ? (
                                        <span className="companion-pdf-translation-skeleton" aria-hidden="true">
                                            <span />
                                            <span />
                                            <span />
                                        </span>
                                    ) : note.translation}
                                </span>
                                {!note.pending && note.text ? (
                                    <span className="companion-pdf-translation-source" title={note.text}>
                                        <span className="companion-pdf-translation-source-label">原文</span>
                                        {note.text}
                                    </span>
                                ) : null}
                            </span>
                        ) : null}
                    </span>
                ));
            }) : null}
        </div>
    );
}

/**
 * Companion-only PDF reader. Marks are stored beside the file, never inside it.
 * Other previews keep the iframe viewer.
 */
export function CompanionPdfReader({ absPath, dataUrl, previewUrl, theme, lang }: CompanionPdfReaderProps) {
    const isZh = lang.startsWith("zh");
    const scrollerRef = useRef<HTMLDivElement>(null);
    const [src, setSrc] = useState("");
    const [error, setError] = useState("");
    const [loading, setLoading] = useState(true);
    const [pdf, setPdf] = useState<PdfDocument | null>(null);
    const [width, setWidth] = useState(0);
    const [seen, setSeen] = useState<Record<number, boolean>>({ 1: true });
    const [marks, setMarks] = useState<PdfMark[]>(() => loadPdfMarks(absPath));
    const [translations, setTranslations] = useState<PdfTranslation[]>(() => loadPdfTranslations(absPath));
    const [pendingNote, setPendingNote] = useState<ShownTranslation | null>(() => {
        const drafts = loadPdfTranslationDrafts(absPath);
        return pendingTranslation(drafts[drafts.length - 1]);
    });
    const [openNoteId, setOpenNoteId] = useState("");
    const [translateError, setTranslateError] = useState(() => pdfTranslationError(absPath));
    const [menu, setMenu] = useState<MenuState | null>(null);
    const marksRef = useRef(marks);
    marksRef.current = marks;
    const translationsRef = useRef(translations);
    translationsRef.current = translations;
    const translateBusyRef = useRef(loadPdfTranslationDrafts(absPath).length > 0);
    const pathRef = useRef(absPath);
    pathRef.current = absPath;

    const applyStoredTranslations = useCallback((path: string) => {
        const drafts = loadPdfTranslationDrafts(path);
        translateBusyRef.current = drafts.length > 0;
        const next = loadPdfTranslations(path);
        translationsRef.current = next;
        setTranslations(next);
        setPendingNote(pendingTranslation(drafts[drafts.length - 1]));
        setTranslateError(pdfTranslationError(path));
    }, []);

    useEffect(() => {
        applyStoredTranslations(absPath);
        setMarks(loadPdfMarks(absPath));
        setOpenNoteId("");
        setMenu(null);
        setSeen({ 1: true });
    }, [absPath, applyStoredTranslations]);

    useEffect(() => subscribePdfTranslations((key) => {
        if (key !== pdfMarkPathKey(pathRef.current)) return;
        applyStoredTranslations(pathRef.current);
    }), [applyStoredTranslations]);

    // A host read slower than the startup wait can land after this reader is
    // already mounted. Re-read both mirrors instead of painting stale state.
    useEffect(() => subscribeCompanionReaderState(() => {
        setMarks(loadPdfMarks(pathRef.current));
        applyStoredTranslations(pathRef.current);
    }), [applyStoredTranslations]);

    useEffect(() => {
        let cancelled = false;
        let objectUrl = "";
        const fail = (message: string) => {
            if (cancelled) return;
            setError(localizeTaskResultPreviewError(message, lang));
            setLoading(false);
        };
        const useSrc = (next: string) => {
            if (cancelled) return;
            setSrc(next);
            setError("");
            setLoading(false);
        };
        const tokenURL = String(previewUrl || "").trim();
        if (tokenURL && isTaskResultPdfPreviewURL(tokenURL)) {
            useSrc(tokenURL);
            return () => { cancelled = true; };
        }
        if (dataUrl && isPdfInlineDataURL(dataUrl)) {
            const next = objectURLFromPdfDataURL(dataUrl) || dataUrl;
            if (next.startsWith("blob:")) objectUrl = next;
            useSrc(next);
            return () => {
                cancelled = true;
                if (objectUrl) URL.revokeObjectURL(objectUrl);
            };
        }
        setLoading(true);
        setError("");
        setSrc("");
        void (async () => {
            try {
                const result = await PreviewTaskResultFile(absPath) as TaskResultPreviewPayload;
                if (cancelled) return;
                const preview = String(result?.preview_url || "").trim();
                if (isTaskResultPdfPreviewURL(preview)) {
                    useSrc(preview);
                    return;
                }
                const inline = String(result?.data_url || "");
                if (isPdfInlineDataURL(inline)) {
                    const next = objectURLFromPdfDataURL(inline) || inline;
                    if (next.startsWith("blob:")) objectUrl = next;
                    useSrc(next);
                    return;
                }
                fail(isZh ? "该 PDF 没有可预览的内容" : "This PDF has no content to preview");
            } catch (err) {
                fail(err instanceof Error ? err.message : String(err || ""));
            }
        })();
        return () => {
            cancelled = true;
            if (objectUrl) URL.revokeObjectURL(objectUrl);
        };
    }, [absPath, dataUrl, previewUrl, isZh, lang]);

    useEffect(() => {
        if (!src) return undefined;
        let cancelled = false;
        let task: PdfLoadingTask | null = null;
        let doc: PdfDocument | null = null;
        setPdf(null);
        void (async () => {
            try {
                const pdfjs = await import("pdfjs-dist");
                pdfjs.GlobalWorkerOptions.workerSrc = pdfjsPublicUrl("pdf.worker.min.mjs");
                const params: Record<string, unknown> = {
                    cMapUrl: pdfjsPublicUrl("cmaps/"),
                    cMapPacked: true,
                    standardFontDataUrl: pdfjsPublicUrl("standard_fonts/"),
                    isEvalSupported: false,
                    useSystemFonts: true,
                };
                if (src.startsWith("data:") || src.startsWith("blob:")) {
                    const response = await fetch(src);
                    params.data = new Uint8Array(await response.arrayBuffer());
                    params.disableRange = true;
                } else {
                    // The preview URL answers Range. The reader does not copy the file.
                    params.url = src;
                }
                task = pdfjs.getDocument(params) as unknown as PdfLoadingTask;
                const opened = await task.promise;
                if (cancelled) {
                    void opened.destroy?.();
                    return;
                }
                doc = opened;
                setPdf(opened);
            } catch (err) {
                if (!cancelled) setError(localizeTaskResultPreviewError(err instanceof Error ? err.message : String(err || ""), lang));
            }
        })();
        return () => {
            cancelled = true;
            task?.destroy();
            void doc?.destroy?.();
        };
    }, [src, lang]);

    useEffect(() => {
        const scroller = scrollerRef.current;
        if (!scroller) return undefined;
        const measure = () => setWidth(scroller.clientWidth);
        measure();
        if (typeof ResizeObserver === "undefined") return undefined;
        const observer = new ResizeObserver(measure);
        observer.observe(scroller);
        return () => observer.disconnect();
    }, [pdf, loading]);

    useEffect(() => {
        const scroller = scrollerRef.current;
        if (!scroller || !pdf || typeof IntersectionObserver === "undefined") return undefined;
        const observer = new IntersectionObserver((entries) => {
            setSeen((current) => {
                const next: Record<number, boolean> = { ...current, 1: true };
                let changed = false;
                for (const entry of entries) {
                    const page = Number((entry.target as HTMLElement).dataset.pdfPage);
                    if (!page) continue;
                    if (entry.isIntersecting && !next[page]) {
                        next[page] = true;
                        changed = true;
                    }
                }
                return changed ? next : current;
            });
        }, { root: scroller, rootMargin: "600px 0px" });
        scroller.querySelectorAll<HTMLElement>("[data-pdf-page]").forEach((page) => observer.observe(page));
        return () => observer.disconnect();
    }, [pdf]);

    useEffect(() => {
        if (!menu) return undefined;
        const onKey = (event: KeyboardEvent) => {
            if (event.key !== "Escape") return;
            event.preventDefault();
            event.stopPropagation();
            setMenu(null);
        };
        const onPointer = (event: Event) => {
            const target = event.target as Node | null;
            if (target && (target as HTMLElement).closest?.("[data-testid='pdf-highlight-menu']")) return;
            setMenu(null);
        };
        window.addEventListener("keydown", onKey, true);
        window.addEventListener("mousedown", onPointer);
        return () => {
            window.removeEventListener("keydown", onKey, true);
            window.removeEventListener("mousedown", onPointer);
        };
    }, [menu]);

    useEffect(() => {
        if (!openNoteId) return undefined;
        const onKey = (event: KeyboardEvent) => {
            if (event.key !== "Escape") return;
            event.preventDefault();
            event.stopPropagation();
            setOpenNoteId("");
        };
        const onPointer = (event: Event) => {
            const target = event.target as HTMLElement | null;
            if (target?.closest?.("[data-pdf-translation-pop],[data-pdf-translation-icon]")) return;
            setOpenNoteId("");
        };
        window.addEventListener("keydown", onKey, true);
        window.addEventListener("mousedown", onPointer);
        return () => {
            window.removeEventListener("keydown", onKey, true);
            window.removeEventListener("mousedown", onPointer);
        };
    }, [openNoteId]);

    const commit = (next: PdfMark[]) => {
        const saved = savePdfMarks(absPath, next);
        setMarks(saved);
        setMenu(null);
    };

    const paint = (color: PdfMarkColor) => {
        if (!menu || menu.parts.length === 0) return;
        const next = marksRef.current.slice();
        for (const part of menu.parts) {
            next.push({ id: newPdfMarkId(), page: part.page, color, rects: part.rects, text: part.text });
        }
        commit(next);
        window.getSelection()?.removeAllRanges();
    };

    const clearHit = () => {
        if (!menu?.hitId) return;
        commit(marksRef.current.filter((mark) => mark.id !== menu.hitId));
    };

    const closeMenu = useCallback(() => setMenu(null), []);

    const copySelection = () => {
        const text = menu?.parts[0]?.text.trim() ?? "";
        setMenu(null);
        if (!text) return;
        void copyTextToClipboard(text).then((ok) => {
            if (ok) {
                window.getSelection()?.removeAllRanges();
                return;
            }
            // A silent no-op looks like a broken menu, so say it plainly.
            setTranslateError(isZh ? "复制失败，请手动选中后复制" : "Copy failed. Select the text and copy it manually.");
        });
    };

    const clearNote = () => {
        const id = menu?.hitNoteId ?? "";
        if (!id) return;
        if (loadPdfTranslationDrafts(absPath).some((item) => item.id === id)) return;
        rememberPdfTranslationPlace(id, null);
        savePdfTranslations(absPath, loadPdfTranslations(absPath).filter((item) => item.id !== id));
        setOpenNoteId((current) => (current === id ? "" : current));
        setMenu(null);
    };

    const translateSelection = () => {
        if (translateBusyRef.current || !menu || menu.parts.length === 0) return;
        const text = menu.parts[0]?.text.trim() ?? "";
        if (!text) return;
        const requestPath = pathRef.current;
        if (loadPdfTranslationDrafts(requestPath).length > 0) {
            translateBusyRef.current = true;
            return;
        }
        translateBusyRef.current = true;
        const id = newPdfTranslationId();
        const clipped = menu.parts.some((part) => part.clipped === true);
        const parts = menu.parts.map((part) => ({ page: part.page, rects: part.rects }));
        const draft: PdfTranslationDraft = { id, text, parts };
        setMenu(null);
        setTranslateError("");
        setPendingNote(pendingTranslation(draft));
        beginPdfTranslation(requestPath, draft);
        window.getSelection()?.removeAllRanges();
        void (async () => {
            try {
                const chinese = await TranslateFileCompanionSelection(requestPath, text);
                const translation = String(chinese || "").trim();
                if (!translation) throw new Error("没有得到译文");
                const stored = loadPdfTranslations(requestPath).filter((item) => item.id !== id);
                const place = pdfTranslationPlace(id);
                const row = { id, text, translation, parts, ...(clipped ? { clipped: true as const } : {}), ...(place ? { place } : {}) };
                savePdfTranslations(requestPath, [...stored, row]);
                finishPdfTranslation(requestPath, id);
            } catch (err) {
                failPdfTranslation(requestPath, id, translateErrorText(err));
            }
        })();
    };

    const toggleNote = (id: string) => {
        setOpenNoteId((current) => (current === id ? "" : id));
    };

    const placeNote = (id: string, place: PdfTranslationPlace | null) => {
        if (loadPdfTranslationDrafts(pathRef.current).some((item) => item.id === id)) {
            rememberPdfTranslationPlace(id, place);
            const drafts = loadPdfTranslationDrafts(pathRef.current);
            setPendingNote(pendingTranslation(drafts[drafts.length - 1]));
            return;
        }
        const stored = loadPdfTranslations(pathRef.current);
        if (!stored.some((item) => item.id === id)) {
            rememberPdfTranslationPlace(id, null);
            return;
        }
        rememberPdfTranslationPlace(id, place);
        const next = stored.map((item) => {
            if (item.id !== id) return item;
            if (!place) {
                const { place: _dropped, ...rest } = item;
                return rest;
            }
            return { ...item, place };
        });
        savePdfTranslations(pathRef.current, next);
    };

    const onContextMenu = (event: React.MouseEvent) => {
        const target = event.target as HTMLElement | null;
        // The Chinese popover is ordinary text. The page menu must not take
        // its right-click, or the browser copy menu never opens.
        if (target?.closest?.("[data-pdf-translation-pop]")) return;
        event.preventDefault();
        const root = scrollerRef.current;
        if (!root) return;
        const pageEl = target?.closest?.("[data-pdf-page]") as HTMLElement | null;
        let hitId = "";
        let hitNoteId = target?.closest?.("[data-pdf-translation-id]")?.getAttribute("data-pdf-translation-id")?.trim() ?? "";
        if (pageEl) {
            const box = pageEl.getBoundingClientRect();
            const page = Number(pageEl.dataset.pdfPage);
            if (box.width > 0 && box.height > 0) {
                const x = (event.clientX - box.left) / box.width;
                const y = (event.clientY - box.top) / box.height;
                const hit = markAt(marksRef.current, page, x, y);
                if (hit) hitId = hit.id;
                if (!hitNoteId) {
                    const note = noteAt(translationsRef.current, page, x, y);
                    if (note) hitNoteId = note.id;
                }
            }
        }
        const parts = selectionParts(root);
        if (parts.length === 0 && !hitId && !hitNoteId) {
            setMenu(null);
            return;
        }
        setMenu({ x: event.clientX, y: event.clientY, hitId, hitNoteId, parts });
    };

    const fill: React.CSSProperties = { background: theme.bg, color: theme.text };
    if (error) {
        return <div data-testid="companion-pdf-error" className="companion-pdf-status" style={fill}>{error}</div>;
    }
    if (loading || !pdf) {
        return <div data-testid="companion-pdf-loading" role="status" className="companion-pdf-status" style={fill}>{isZh ? "正在加载 PDF…" : "Loading PDF…"}</div>;
    }

    const pages = [];
    for (let page = 1; page <= pdf.numPages; page += 1) pages.push(page);
    const pendingVisible = pendingNote && !translations.some((item) => item.id === pendingNote.id) ? pendingNote : null;
    const shownNotes: ShownTranslation[] = pendingVisible ? [...translations, pendingVisible] : translations;
    const selectionText = menu?.parts[0]?.text.trim() ?? "";

    return (
        <div className="companion-pdf-reader" data-testid="companion-pdf-reader" style={fill} onContextMenu={onContextMenu}>
            {translateError ? (
                <p className="companion-pdf-translate-error" role="status" data-testid="pdf-translate-error">{translateError}</p>
            ) : null}
            <div className="companion-pdf-scroll" ref={scrollerRef} data-testid="companion-pdf-scroll">
                {pages.map((page) => (
                    <PdfPageView
                        key={page}
                        pdf={pdf}
                        pageNumber={page}
                        width={width}
                        marks={marks}
                        notes={shownNotes}
                        openNoteId={openNoteId}
                        onToggleNote={toggleNote}
                        onPlaceNote={placeNote}
                        active={page === 1 || seen[page] === true}
                    />
                ))}
            </div>
            {menu ? (
                <PdfSelectionMenu
                    state={menu}
                    selectionText={selectionText}
                    onTranslate={translateSelection}
                    onCopy={copySelection}
                    onPaint={paint}
                    onClearNote={clearNote}
                    onClearMark={clearHit}
                    onClose={closeMenu}
                />
            ) : null}
        </div>
    );
}
