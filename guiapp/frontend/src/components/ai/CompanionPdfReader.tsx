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
    loadPdfDrawings,
    newPdfDrawingId,
    PDF_DRAWING_MAX_POINTS,
    pdfDrawingHitsRect,
    savePdfDrawings,
    subscribePdfDrawings,
    type PdfDrawing,
    type PdfDrawingPoint,
    type PdfDrawingTool,
} from "../filecompanion/pdfDrawings";
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
    notifyPdfAnnotationsChanged,
    pdfTranslationError,
    pdfTranslationPlace,
    rememberPdfTranslationPlace,
    savePdfTranslations,
    subscribePdfTranslations,
    type PdfTranslation,
    type PdfTranslationDraft,
    type PdfTranslationPlace,
    type PdfTranslationSize,
} from "../filecompanion/pdfTranslations";
import { subscribeCompanionReaderState } from "../filecompanion/companionReaderState";
import { normalizeLang } from "../../i18n/langSelect";
import { tailText } from "../../i18n/mnTail";

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
    hitNoteKind: "" | "annotation" | "translation";
    hitMark: PdfMark | null;
    parts: SelectionPart[];
};

type ShownTranslation = PdfTranslation & { pending?: boolean };

const PDF_TRANSLATE_CHARS = 4000;

function clipTranslateText(text: string): string {
    const chars = Array.from(text);
    if (chars.length <= PDF_TRANSLATE_CHARS) return text;
    return chars.slice(0, PDF_TRANSLATE_CHARS).join("");
}

const PDF_COLOR_NAME: Record<PdfMarkColor, { en: string; zh: string; mn: string }> = {
    yellow: { en: "Yellow", zh: "黄色", mn: "Шар" },
    green: { en: "Green", zh: "绿色", mn: "Ногоон" },
    blue: { en: "Blue", zh: "蓝色", mn: "Цэнхэр" },
    pink: { en: "Pink", zh: "粉色", mn: "Ягаан" },
};

function pdfSwatchLabel(id: PdfMarkColor, lang: string, kind: "highlight" | "pen"): string {
    const color = PDF_COLOR_NAME[id];
    if (normalizeLang(lang) === "mn") {
        return kind === "pen" ? `${color.mn} үзэг` : `${color.mn} тодотгол`;
    }
    return kind === "pen"
        ? tailText(lang, `${color.en} pen`, `${color.zh}笔`)
        : tailText(lang, `${color.en} highlight`, `${color.zh}高亮`);
}

function translateErrorText(err: unknown, lang: string): string {
    const raw = err instanceof Error ? err.message : String(err ?? "");
    const text = raw.trim();
    const say = (en: string, zh: string) => tailText(lang, en, zh);
    if (!text || text === "翻译失败，请稍后重试") {
        return say("Translation failed. Please try again later.", "翻译失败，请稍后重试");
    }
    if (text.includes("path is outside the open file") || text === "这个文件没有打开") {
        return say("This file is not open", "这个文件没有打开");
    }
    if (text.includes("file companion is not running") || text === "伴读还没有打开") {
        return say("File companion is not open", "伴读还没有打开");
    }
    if (text.includes("MaClaw Hub login is required") || text === "需要先在主窗口登录") {
        return say("Sign in from the main window first", "需要先在主窗口登录");
    }
    if (
        text.includes("LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST")
        || /insufficient credits/i.test(text)
        || text === "本次请求额度不足"
    ) {
        return say("Insufficient credits for this request", "本次请求额度不足");
    }
    if (text === "没有得到译文") {
        return say("No translation was returned", "没有得到译文");
    }
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

type PdfMenuGlyphKind = "highlight" | "translate" | "copy" | "check" | "close" | "erase" | "note";

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
    lang: string;
    state: MenuState;
    selectionText: string;
    onTranslate: () => void;
    onCopy: () => void;
    onPaint: (color: PdfMarkColor) => void;
    onAnnotate: (text: string) => void;
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
    lang,
    state,
    selectionText,
    onTranslate,
    onCopy,
    onPaint,
    onAnnotate,
    onClearNote,
    onClearMark,
    onClose,
}: PdfSelectionMenuProps) {
    const rootRef = useRef<HTMLDivElement>(null);
    const [place, setPlace] = useState({ left: state.x, top: state.y });
    const [annotationOpen, setAnnotationOpen] = useState(false);
    const [annotationText, setAnnotationText] = useState("");

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
    }, [state.x, state.y, selectionText, state.hitId, state.hitNoteId, annotationOpen]);

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

    const hasSelection = selectionText.trim().length > 0;
    const quoteText = hasSelection ? selectionText : (state.hitMark?.text ?? "");
    const quote = quoteText ? menuQuote(quoteText) : "";
    const canAnnotate = quoteText.trim().length > 0;
    const submitAnnotation = () => {
        const text = annotationText.trim();
        if (!text) return;
        onAnnotate(text);
        setAnnotationOpen(false);
        setAnnotationText("");
    };
    // The visible quote is trimmed to one line; the tooltip carries the whole
    // selection so a truncated head can still be read in full.
    const quoteTitle = quoteText.replace(/\s+/g, " ").trim();

    return (
        <div
            ref={rootRef}
            className="companion-pdf-menu"
            data-testid="pdf-highlight-menu"
            role="menu"
            aria-label={tailText(lang, "Selection actions", "选中文本操作")}
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
                        <span className="companion-pdf-menu-item-label">{tailText(lang, "Translate", "翻译")}</span>
                        <span className="companion-pdf-menu-item-hint">{tailText(lang, "Chinese", "中文")}</span>
                    </button>
                    <button
                        type="button"
                        role="menuitem"
                        data-testid="pdf-copy"
                        className="companion-pdf-menu-item"
                        {...activate(onCopy)}
                    >
                        <PdfMenuGlyph kind="copy" />
                        <span className="companion-pdf-menu-item-label">{tailText(lang, "Copy source", "复制原文")}</span>
                    </button>
                </div>
            ) : null}
            {canAnnotate ? (
                <div className="companion-pdf-menu-group">
                    {annotationOpen ? (
                        <div data-testid="pdf-annotation-editor" style={{ display: "grid", gap: 6, padding: "8px", minWidth: 220 }}>
                            <textarea
                                data-testid="pdf-annotation-input"
                                value={annotationText}
                                autoFocus
                                rows={3}
                                placeholder={tailText(lang, "Write a note…", "写批注…")}
                                style={{ resize: "vertical", minHeight: 64, font: "inherit", padding: "6px 8px" }}
                                onChange={(event) => setAnnotationText(event.target.value)}
                                onKeyDown={(event) => {
                                    if (event.key === "Escape") {
                                        event.preventDefault();
                                        event.stopPropagation();
                                        setAnnotationOpen(false);
                                        setAnnotationText("");
                                        return;
                                    }
                                    if (event.key === "Enter" && !event.shiftKey) {
                                        event.preventDefault();
                                        submitAnnotation();
                                    }
                                }}
                            />
                            <div style={{ display: "flex", gap: 6, justifyContent: "flex-end" }}>
                                <button
                                    type="button"
                                    data-testid="pdf-annotation-cancel"
                                    onClick={() => { setAnnotationOpen(false); setAnnotationText(""); }}
                                    onMouseDown={(event) => { event.preventDefault(); event.stopPropagation(); }}
                                    style={{ padding: "4px 8px" }}
                                >{tailText(lang, "Cancel", "取消")}</button>
                                <button
                                    type="button"
                                    data-testid="pdf-annotation-save"
                                    disabled={!annotationText.trim()}
                                    onClick={submitAnnotation}
                                    onMouseDown={(event) => { event.preventDefault(); event.stopPropagation(); }}
                                    style={{ padding: "4px 8px" }}
                                >{tailText(lang, "Save note", "保存批注")}</button>
                            </div>
                        </div>
                    ) : (
                        <button
                            type="button"
                            role="menuitem"
                            data-testid="pdf-annotate"
                            className="companion-pdf-menu-item"
                            {...activate(() => setAnnotationOpen(true))}
                        >
                            <PdfMenuGlyph kind="note" />
                            <span className="companion-pdf-menu-item-label">{tailText(lang, "Note", "批注")}</span>
                        </button>
                    )}
                </div>
            ) : null}
            {state.parts.length > 0 ? (
                <div className="companion-pdf-menu-group">
                    <p className="companion-pdf-menu-title"><PdfMenuGlyph kind="highlight" />{tailText(lang, "Highlight", "高亮")}</p>
                    <div className="companion-pdf-menu-colors" role="group" aria-label={tailText(lang, "Highlight colour", "高亮颜色")}>
                        {PDF_MARK_COLORS.map((color) => (
                            <button
                                key={color.id}
                                type="button"
                                role="menuitem"
                                data-testid={`pdf-highlight-${color.id}`}
                                className="companion-pdf-menu-color"
                                aria-label={pdfSwatchLabel(color.id, lang, "highlight")}
                                title={pdfSwatchLabel(color.id, lang, "highlight")}
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
                            <span className="companion-pdf-menu-item-label">{state.hitNoteKind === "annotation" ? tailText(lang, "Delete note", "删除批注") : tailText(lang, "Remove translation", "取消翻译")}</span>
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
                            <span className="companion-pdf-menu-item-label">{tailText(lang, "Remove highlight", "取消高亮")}</span>
                        </button>
                    ) : null}
                </div>
            ) : null}
        </div>
    );
}

// A stronger ink than the highlighter fill so a thin stroke stays readable on
// white paper while the palette still matches the highlight colours.
const PDF_DRAWING_INK: Record<PdfMarkColor, string> = {
    yellow: "#c9930a",
    green: "#2f8f5b",
    blue: "#2f6bbd",
    pink: "#c74884",
};

// Eraser is a reader mode, not a stored drawing tool.
type PdfDrawTool = PdfDrawingTool | "eraser";

// PdfDrawingLayer is the transparent page overlay used while 手绘 is on. The
// same SVG also paints stored strokes and boxes; with the pen off it has no
// pointer events, so text selection and the highlight menu keep working.
function PdfDrawingLayer({
    pageNumber,
    drawings,
    active,
    color,
    tool,
    onAdd,
    onErase,
}: {
    pageNumber: number;
    drawings: PdfDrawing[];
    active: boolean;
    color: PdfMarkColor;
    tool: PdfDrawTool;
    onAdd: (drawing: PdfDrawing) => void;
    onErase: (ids: string[]) => void;
}) {
    const surfaceRef = useRef<SVGSVGElement>(null);
    const [draft, setDraft] = useState<PdfDrawing | null>(null);
    const draftRef = useRef<PdfDrawing | null>(null);
    const [eraseRect, setEraseRect] = useState<PdfMarkRect | null>(null);
    const eraseStartRef = useRef<PdfDrawingPoint | null>(null);
    const eraseRectRef = useRef<PdfMarkRect | null>(null);
    const drawingsRef = useRef(drawings);
    drawingsRef.current = drawings;
    const stopRef = useRef<(() => void) | null>(null);
    const cleanupRef = useRef<(() => void) | null>(null);

    // The pointer handlers live on window for the duration of one stroke. A
    // tab switch unmounts this layer; leaving them behind would keep acting on
    // a canvas that is gone.
    useEffect(() => () => cleanupRef.current?.(), []);
    useEffect(() => {
        if (active) return;
        // The pen can be switched off after a mouseup that landed outside the
        // window. Throw that unfinished stroke away instead of painting it on
        // the next click.
        cleanupRef.current?.();
        cleanupRef.current = null;
        stopRef.current = null;
        draftRef.current = null;
        eraseStartRef.current = null;
        eraseRectRef.current = null;
        setDraft(null);
        setEraseRect(null);
    }, [active]);

    // Switching tools abandons a gesture whose mouseup was lost outside the
    // window; otherwise the next click would commit that stale selection.
    useEffect(() => {
        cleanupRef.current?.();
        cleanupRef.current = null;
        stopRef.current = null;
        draftRef.current = null;
        eraseStartRef.current = null;
        eraseRectRef.current = null;
        setDraft(null);
        setEraseRect(null);
    }, [tool]);

    const pointAt = (clientX: number, clientY: number): PdfDrawingPoint | null => {
        const box = surfaceRef.current?.getBoundingClientRect();
        if (!box || box.width < 1 || box.height < 1) return null;
        return {
            x: Math.min(1, Math.max(0, (clientX - box.left) / box.width)),
            y: Math.min(1, Math.max(0, (clientY - box.top) / box.height)),
        };
    };

    // The eraser is a marquee: drag a box and every local stroke or frame it
    // touches goes away. Host drawings received from a share carry `shared`
    // and stay, matching the 撤销 guard.
    const beginErase = (start: PdfDrawingPoint) => {
        eraseStartRef.current = start;
        const initial = { x: start.x, y: start.y, w: 0, h: 0 };
        eraseRectRef.current = initial;
        setEraseRect(initial);
        let frame = 0;
        let finish: () => void = () => undefined;
        const paintRect = () => {
            frame = 0;
            setEraseRect(eraseRectRef.current);
        };
        const schedulePaint = () => {
            if (frame) return;
            if (typeof window.requestAnimationFrame !== "function") {
                paintRect();
                return;
            }
            frame = window.requestAnimationFrame(paintRect);
        };
        const cancelPaint = () => {
            if (!frame) return;
            if (typeof window.cancelAnimationFrame === "function") window.cancelAnimationFrame(frame);
            frame = 0;
        };
        const move = (moveEvent: MouseEvent) => {
            const from = eraseStartRef.current;
            if (!from) return;
            const next = pointAt(moveEvent.clientX, moveEvent.clientY);
            if (!next) return;
            eraseRectRef.current = {
                x: Math.min(from.x, next.x),
                y: Math.min(from.y, next.y),
                w: Math.abs(next.x - from.x),
                h: Math.abs(next.y - from.y),
            };
            schedulePaint();
        };
        const removeListeners = () => {
            cancelPaint();
            window.removeEventListener("mousemove", move, true);
            window.removeEventListener("mouseup", finish, true);
            window.removeEventListener("blur", finish);
            if (cleanupRef.current === removeListeners) cleanupRef.current = null;
        };
        finish = () => {
            removeListeners();
            stopRef.current = null;
            const rect = eraseRectRef.current;
            eraseStartRef.current = null;
            eraseRectRef.current = null;
            setEraseRect(null);
            // A click without a drag must not erase a whole stroke.
            if (!rect || Math.max(rect.w, rect.h) < 0.002) return;
            const ids = drawingsRef.current
                .filter((drawing) => !drawing.shared && pdfDrawingHitsRect(drawing, rect))
                .map((drawing) => drawing.id);
            if (ids.length > 0) onErase(ids);
        };
        cleanupRef.current = removeListeners;
        stopRef.current = finish;
        window.addEventListener("mousemove", move, true);
        window.addEventListener("mouseup", finish, true);
        window.addEventListener("blur", finish);
    };

    const startDrawing = (event: React.MouseEvent) => {
        if (!active || event.button !== 0) return;
        event.preventDefault();
        const point = pointAt(event.clientX, event.clientY);
        if (!point) return;
        stopRef.current?.();
        if (tool === "eraser") {
            beginErase(point);
            return;
        }
        const shapeTool: PdfDrawingTool = tool === "box" ? "box" : "line";
        const drawing: PdfDrawing = {
            id: newPdfDrawingId(),
            page: pageNumber,
            color,
            tool: shapeTool,
            points: [point, point],
        };
        draftRef.current = drawing;
        setDraft(drawing);
        let frame = 0;
        let finish: () => void = () => undefined;
        const paintDraft = () => {
            frame = 0;
            setDraft(draftRef.current);
        };
        // Mouse move can fire faster than the display refresh. Repaint once per
        // frame instead of re-rendering the whole stroke on every event.
        const schedulePaint = () => {
            if (frame) return;
            if (typeof window.requestAnimationFrame !== "function") {
                paintDraft();
                return;
            }
            frame = window.requestAnimationFrame(paintDraft);
        };
        const cancelPaint = () => {
            if (!frame) return;
            if (typeof window.cancelAnimationFrame === "function") window.cancelAnimationFrame(frame);
            frame = 0;
        };
        const move = (moveEvent: MouseEvent) => {
            const current = draftRef.current;
            const box = surfaceRef.current?.getBoundingClientRect();
            if (!current || !box || box.width < 1 || box.height < 1) return;
            const next = pointAt(moveEvent.clientX, moveEvent.clientY);
            if (!next) return;
            if (current.tool === "line") {
                const last = current.points[current.points.length - 1];
                if (current.points.length >= PDF_DRAWING_MAX_POINTS) return;
                if (Math.abs(next.x - last.x) * box.width + Math.abs(next.y - last.y) * box.height < 1.5) return;
                draftRef.current = { ...current, points: [...current.points, next] };
                schedulePaint();
                return;
            }
            draftRef.current = { ...current, points: [current.points[0], next] };
            schedulePaint();
        };
        const removeListeners = () => {
            cancelPaint();
            window.removeEventListener("mousemove", move, true);
            window.removeEventListener("mouseup", finish, true);
            window.removeEventListener("blur", finish);
            if (cleanupRef.current === removeListeners) cleanupRef.current = null;
        };
        finish = () => {
            removeListeners();
            stopRef.current = null;
            const finished = draftRef.current;
            draftRef.current = null;
            setDraft(null);
            if (!finished) return;
            if (finished.tool === "line") {
                if (finished.points.length < 2) return;
            } else {
                const a = finished.points[0];
                const b = finished.points[finished.points.length - 1];
                if (Math.abs(b.x - a.x) < 0.002 && Math.abs(b.y - a.y) < 0.002) return;
            }
            onAdd(finished);
        };
        cleanupRef.current = removeListeners;
        stopRef.current = finish;
        window.addEventListener("mousemove", move, true);
        window.addEventListener("mouseup", finish, true);
        window.addEventListener("blur", finish);
    };

    const shapes = draft ? [...drawings, draft] : drawings;
    const erasing = active && tool === "eraser";
    const cursorClass = erasing
        ? " companion-pdf-drawings--eraser"
        : active && tool === "line"
            ? " companion-pdf-drawings--pen"
            : "";
    const doomed = tool === "eraser" && eraseRect
        ? new Set(
            drawings
                .filter((drawing) => !drawing.shared && pdfDrawingHitsRect(drawing, eraseRect))
                .map((drawing) => drawing.id),
        )
        : null;
    return (
        <svg
            ref={surfaceRef}
            className={active
                ? `companion-pdf-drawings companion-pdf-drawings--active${cursorClass}`
                : "companion-pdf-drawings"}
            data-testid={`pdf-drawing-layer-${pageNumber}`}
            viewBox="0 0 1 1"
            preserveAspectRatio="none"
            aria-hidden="true"
            onMouseDown={startDrawing}
        >
            {shapes.map((drawing) => {
                const marked = doomed?.has(drawing.id) === true;
                const stroke = marked ? "#dc1f25" : PDF_DRAWING_INK[drawing.color];
                const opacity = marked ? 0.35 : undefined;
                if (drawing.tool === "box") {
                    const a = drawing.points[0];
                    const b = drawing.points[drawing.points.length - 1];
                    return (
                        <rect
                            key={drawing.id}
                            data-drawing-id={drawing.id}
                            x={Math.min(a.x, b.x)}
                            y={Math.min(a.y, b.y)}
                            width={Math.abs(b.x - a.x)}
                            height={Math.abs(b.y - a.y)}
                            fill={pdfMarkFill(drawing.color)}
                            fillOpacity={0.16}
                            stroke={stroke}
                            strokeWidth={2}
                            opacity={opacity}
                            vectorEffect="non-scaling-stroke"
                        />
                    );
                }
                return (
                    <polyline
                        key={drawing.id}
                        data-drawing-id={drawing.id}
                        points={drawing.points.map((point) => `${point.x},${point.y}`).join(" ")}
                        fill="none"
                        stroke={stroke}
                        strokeWidth={2.4}
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        opacity={opacity}
                        vectorEffect="non-scaling-stroke"
                    />
                );
            })}
            {tool === "eraser" && eraseRect ? (
                <rect
                    data-testid={`pdf-erase-preview-${pageNumber}`}
                    x={eraseRect.x}
                    y={eraseRect.y}
                    width={eraseRect.w}
                    height={eraseRect.h}
                    fill="#dc1f25"
                    fillOpacity={0.08}
                    stroke="#dc1f25"
                    strokeWidth={1.5}
                    strokeDasharray="6 4"
                    pointerEvents="none"
                    vectorEffect="non-scaling-stroke"
                />
            ) : null}
        </svg>
    );
}

function PdfPageView({
    lang,
    pdf,
    pageNumber,
    width,
    marks,
    drawings,
    drawActive,
    drawColor,
    drawTool,
    onAddDrawing,
    onEraseDrawing,
    notes,
    openNoteId,
    onToggleNote,
    onPlaceNote,
    onResizeNote,
    active,
}: {
    lang: string;
    pdf: PdfDocument;
    pageNumber: number;
    width: number;
    marks: PdfMark[];
    drawings: PdfDrawing[];
    drawActive: boolean;
    drawColor: PdfMarkColor;
    drawTool: PdfDrawTool;
    onAddDrawing: (drawing: PdfDrawing) => void;
    onEraseDrawing: (ids: string[]) => void;
    notes: ShownTranslation[];
    openNoteId: string;
    onToggleNote: (id: string) => void;
    onPlaceNote: (id: string, place: PdfTranslationPlace | null) => void;
    onResizeNote: (id: string, size: PdfTranslationSize | null) => void;
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

    // Resize the card from its bottom-right corner. Both translations and
    // annotations use it; the size is stored relative to the rendered page so
    // it survives a zoom change.
    const startPopResize = (event: React.MouseEvent, note: ShownTranslation) => {
        if (event.button !== 0) return;
        event.preventDefault();
        event.stopPropagation();
        const pop = (event.currentTarget as HTMLElement).closest("[data-pdf-translation-pop]") as HTMLElement | null;
        const span = pop?.closest(".companion-pdf-translation") as HTMLElement | null;
        const pageEl = span?.closest(".companion-pdf-page") as HTMLElement | null;
        if (!pop || !span || !pageEl) return;
        const pageBox = pageEl.getBoundingClientRect();
        if (pageBox.width < 1 || pageBox.height < 1) return;
        dragStopRef.current?.();
        const startX = event.clientX;
        const startY = event.clientY;
        const startW = pop.offsetWidth;
        const startH = pop.offsetHeight;
        const minW = Math.min(200, pageBox.width);
        const minH = Math.min(100, pageBox.height);
        let lastW = startW;
        let lastH = startH;
        let stopped = false;
        const previousCursor = document.body.style.cursor;
        const apply = (width: number, height: number) => {
            pop.style.width = `${width}px`;
            pop.style.height = `${height}px`;
            pop.style.maxWidth = "none";
            pop.style.maxHeight = "none";
        };
        const move = (ev: MouseEvent) => {
            ev.preventDefault();
            const pageNow = pageEl.getBoundingClientRect();
            if (pageNow.width < 1 || pageNow.height < 1) return;
            lastW = Math.min(pageNow.width, Math.max(minW, startW + (ev.clientX - startX)));
            lastH = Math.min(pageNow.height, Math.max(minH, startH + (ev.clientY - startY)));
            apply(lastW, lastH);
        };
        const stop = () => {
            if (stopped) return;
            stopped = true;
            window.removeEventListener("mousemove", move, true);
            window.removeEventListener("mouseup", stop, true);
            window.removeEventListener("blur", stop);
            document.body.style.cursor = previousCursor;
            if (dragStopRef.current === stop) dragStopRef.current = null;
            if (lastW === startW && lastH === startH) return;
            const pageNow = pageEl.getBoundingClientRect();
            if (pageNow.width < 1 || pageNow.height < 1) return;
            onResizeNote(note.id, { w: lastW / pageNow.width, h: lastH / pageNow.height });
        };
        document.body.style.cursor = "nwse-resize";
        window.addEventListener("mousemove", move, true);
        window.addEventListener("mouseup", stop, true);
        window.addEventListener("blur", stop);
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
            {active ? (
                <PdfDrawingLayer
                    pageNumber={pageNumber}
                    drawings={drawings.filter((drawing) => drawing.page === pageNumber)}
                    active={drawActive}
                    color={drawColor}
                    tool={drawTool}
                    onAdd={onAddDrawing}
                    onErase={onEraseDrawing}
                />
            ) : null}
            {active ? notes.map((note) => {
                const part = note.parts.find((item) => item.page === pageNumber);
                if (!part) return null;
                const iconHere = note.parts[0]?.page === pageNumber;
                const annotation = note.kind === "annotation";
                // A resized card keeps its own width/height; an untouched one
                // stays sized to its content by the stylesheet.
                const sized = Boolean(note.size) && viewport.width > 0 && viewport.height > 0;
                const popStyle: React.CSSProperties | undefined = sized && note.size ? {
                    width: `${Math.max(Math.min(200, viewport.width), Math.min(viewport.width, note.size.w * viewport.width))}px`,
                    height: `${Math.max(Math.min(100, viewport.height), Math.min(viewport.height, note.size.h * viewport.height))}px`,
                    maxWidth: "none",
                    maxHeight: "none",
                    display: "flex",
                    flexDirection: "column",
                    overflow: "hidden",
                } : undefined;
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
                                aria-label={annotation ? tailText(lang, "Note", "批注") : note.pending ? tailText(lang, "Translating…", "正在翻译") : tailText(lang, "Translation", "译文")}
                                aria-busy={note.pending ? true : undefined}
                                onMouseDown={(event) => {
                                    if (event.button !== 0) return;
                                    event.preventDefault();
                                    event.stopPropagation();
                                    onToggleNote(note.id);
                                }}
                            >
                                {annotation ? (
                                    <svg className="companion-pdf-translation-icon-glyph" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                                        <path d="m14 5 5 5-8.5 8.5a2 2 0 0 1-2.8 0l-2.2-2.2a2 2 0 0 1 0-2.8z" />
                                        <path d="m8.5 10.5 5 5" />
                                        <path d="M5 20h14" />
                                    </svg>
                                ) : (
                                    <svg className="companion-pdf-translation-icon-glyph" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                                        <path d="M3 5h9" />
                                        <path d="M7.5 3v2" />
                                        <path d="m5.5 8 5 5" />
                                        <path d="m4.5 13 5-5 1.5-2" />
                                        <path d="m20 20-3.6-7.4L12.8 20" />
                                        <path d="M14.4 17.2h4.4" />
                                    </svg>
                                )}
                                {annotation ? tailText(lang, "Note", "批注") : tailText(lang, "Translation", "译文")}
                            </button>
                        ) : null}
                        {iconHere && index === 0 && openNoteId === note.id ? (
                            <span
                                className={(note.place ? note.place.above === true : popAbove) ? "companion-pdf-translation-pop companion-pdf-translation-pop--above" : "companion-pdf-translation-pop"}
                                role="dialog"
                                aria-label={annotation ? tailText(lang, "Note", "批注") : tailText(lang, "Translation", "译文")}
                                data-testid={`pdf-translate-pop-${note.id}`}
                                data-pdf-translation-pop=""
                                style={popStyle}
                                onMouseDown={(event) => event.stopPropagation()}
                            >
                                <span
                                    className="companion-pdf-translation-chrome"
                                    onMouseDown={(event) => startPopDrag(event, note)}
                                >
                                <span
                                    className="companion-pdf-translation-grip"
                                    data-pdf-translation-grip=""
                                    title={tailText(lang, "Drag", "拖动")}
                                    aria-hidden="true"
                                />
                                <span className="companion-pdf-translation-head">
                                    <span className="companion-pdf-translation-kicker">
                                        {annotation ? tailText(lang, "Note", "批注") : note.pending ? tailText(lang, "Translating…", "正在翻译") : tailText(lang, "Chinese translation", "中文译文")}
                                    </span>
                                    <span className="companion-pdf-translation-actions">
                                        <button
                                            type="button"
                                            className="companion-pdf-translation-action"
                                            data-testid={`pdf-translate-copy-${note.id}`}
                                            data-copy-state={copyFailedId === note.id ? "failed" : copiedId === note.id ? "copied" : undefined}
                                            title={copyFailedId === note.id ? tailText(lang, "Copy failed", "复制失败") : copiedId === note.id ? tailText(lang, "Copied", "已复制") : (annotation ? tailText(lang, "Copy note", "复制批注") : tailText(lang, "Copy translation", "复制译文"))}
                                            aria-label={copyFailedId === note.id ? tailText(lang, "Copy failed", "复制失败") : copiedId === note.id ? tailText(lang, "Copied", "已复制") : (annotation ? tailText(lang, "Copy note", "复制批注") : tailText(lang, "Copy translation", "复制译文"))}
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
                                            title={tailText(lang, "Close", "关闭")}
                                            aria-label={annotation ? tailText(lang, "Close note", "关闭批注") : tailText(lang, "Close translation", "关闭译文")}
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
                                <span className="companion-pdf-translation-body" style={sized ? { flex: "1 1 auto", minHeight: 0, overflow: "auto" } : undefined}>
                                    {!note.pending && note.clipped ? <span className="companion-pdf-translation-clip">{tailText(lang, "Only the first 4,000 characters were translated", "仅翻译了前 4000 字")}</span> : null}
                                    {note.pending ? (
                                        <span className="companion-pdf-translation-skeleton" aria-hidden="true">
                                            <span />
                                            <span />
                                            <span />
                                        </span>
                                    ) : note.translation}
                                </span>
                                {!note.pending && note.text ? (
                                    <span className="companion-pdf-translation-source" style={sized ? { flex: "0 0 auto" } : undefined} title={note.text}>
                                        <span className="companion-pdf-translation-source-label">{tailText(lang, "Source", "原文")}</span>
                                        {note.text}
                                    </span>
                                ) : null}
                                <span
                                    data-pdf-translation-resize=""
                                    title={tailText(lang, "Resize", "调整大小")}
                                    aria-hidden="true"
                                    onMouseDown={(event) => startPopResize(event, note)}
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
                        ) : null}
                    </span>
                ));
            }) : null}
        </div>
    );
}

/** Digits only. A blank or zero does not jump; a page past the end lands on the last page. */
function parsePdfGoto(raw: string, total: number): number | null {
    const text = raw.trim();
    if (!/^\d+$/.test(text)) return null;
    const page = Number(text);
    if (!Number.isFinite(page) || page < 1 || total < 1) return null;
    return Math.min(total, page);
}

/**
 * The page that owns the top of the scroller. A few pixels of the previous
 * page still peeking under the bar do not count. Binary search: pages stack
 * in order, so a long file does not measure every page on each scroll.
 * Returns 0 when the scroller has not been laid out yet.
 */
function pdfPageAtScrollerTop(scroller: HTMLElement, pages: ArrayLike<HTMLElement>): number {
    if (scroller.clientHeight < 1) return 0;
    const count = pages.length;
    if (count === 0) return 0;
    const edge = scroller.getBoundingClientRect().top + 8;
    let lo = 0;
    let hi = count - 1;
    while (lo < hi) {
        const mid = (lo + hi) >> 1;
        if (pages[mid].getBoundingClientRect().bottom <= edge) lo = mid + 1;
        else hi = mid;
    }
    const chosen = pages[lo];
    const page = Number(chosen.dataset.pdfPage);
    if (!(page > 0)) return 0;
    // Shells that have not been laid out share a zero box. Reporting the last
    // one would flash the final page before the first layout.
    const box = chosen.getBoundingClientRect();
    if (box.width < 1 && box.height < 1) return 0;
    return page;
}

/** Move only this scroller. scrollIntoView also scrolls the companion window. */
function scrollPdfPageToTop(scroller: HTMLElement, el: HTMLElement) {
    const delta = el.getBoundingClientRect().top - scroller.getBoundingClientRect().top;
    if (Math.abs(delta) < 1) return;
    scroller.scrollTop += delta;
}

function PdfPager({
    lang,
    total,
    scrollerRef,
    pagesBoxRef,
    onReveal,
}: {
    lang: string;
    total: number;
    scrollerRef: React.RefObject<HTMLDivElement | null>;
    pagesBoxRef: React.RefObject<HTMLDivElement | null>;
    onReveal: (page: number) => void;
}) {
    const inputRef = useRef<HTMLInputElement>(null);
    const [current, setCurrent] = useState(1);
    const [draft, setDraft] = useState("1");
    const currentRef = useRef(1);
    const pinPage = useRef(0);
    const pagesRef = useRef<NodeListOf<HTMLElement> | null>(null);
    const scrollLock = useRef(false);
    const editing = useRef(false);
    const draftAtFocus = useRef("1");
    const draftRef = useRef("1");
    draftRef.current = draft;

    const jumpToPage = (page: number) => {
        const next = parsePdfGoto(String(page), total);
        if (!next) return;
        currentRef.current = next;
        pinPage.current = next;
        editing.current = false;
        const shown = String(next);
        draftAtFocus.current = shown;
        draftRef.current = shown;
        setCurrent(next);
        setDraft(shown);
        onReveal(next);
        const scroller = scrollerRef.current;
        const el = scroller?.querySelector<HTMLElement>(`[data-pdf-page="${next}"]`);
        if (scroller && el) {
            scrollLock.current = true;
            scrollPdfPageToTop(scroller, el);
            scrollLock.current = false;
        }
        // Drop focus after the draft matches the destination, so blur does not
        // commit whatever was typed before this turn.
        inputRef.current?.blur();
    };

    const commitGoto = (raw: string) => {
        const next = parsePdfGoto(raw, total);
        if (!next) {
            const shown = String(currentRef.current);
            draftAtFocus.current = shown;
            draftRef.current = shown;
            // The field keeps focus after a rejected Enter or 前往. Leave the
            // edit open so a scroll does not rewrite the box under the caret.
            editing.current = inputRef.current === document.activeElement;
            setDraft(shown);
            return;
        }
        jumpToPage(next);
    };

    const stepPage = (delta: number) => {
        // The label trails a fling by a frame. Step from the page on screen, or
        // the click lands behind the scroll. A jump that is still settling uses
        // its own page: the shells above it may not have their real height yet.
        let base = currentRef.current;
        if (!pinPage.current) {
            const scroller = scrollerRef.current;
            const pages = pagesRef.current;
            if (scroller && pages) base = pdfPageAtScrollerTop(scroller, pages) || base;
        }
        const next = parsePdfGoto(String(base + delta), total);
        if (next) jumpToPage(next);
    };

    // A real click is press then, later, click. Acting on both turns two pages
    // once the mouse is held past the next task. Mouse acts on press. A keyboard
    // click has detail 0 and no press, so it still turns once.
    const press = (event: React.MouseEvent, act: () => void) => {
        if (event.button !== 0) return;
        event.preventDefault();
        act();
    };
    const activate = (event: React.MouseEvent, act: () => void) => {
        if (event.detail !== 0) return;
        act();
    };

    useEffect(() => {
        const scroller = scrollerRef.current;
        const pagesBox = pagesBoxRef.current;
        if (!scroller) return undefined;
        // Page shells are already mounted. Reuse the list so a scroll frame does
        // not walk the document again.
        const pages = scroller.querySelectorAll<HTMLElement>("[data-pdf-page]");
        pagesRef.current = pages;
        let frame = 0;
        let pointerDown = false;
        let aligning = false;
        const readPage = () => {
            frame = 0;
            const next = pdfPageAtScrollerTop(scroller, pages);
            if (!next || next === currentRef.current) return;
            currentRef.current = next;
            setCurrent(next);
            if (!editing.current) {
                draftRef.current = String(next);
                setDraft(String(next));
            }
        };
        // Only a size change pulls a jump back to the top of its page. A scroll
        // must not, or a short wheel flick is undone on the next frame.
        const realign = () => {
            const pinned = pinPage.current;
            const el = pinned ? pages[pinned - 1] : undefined;
            if (!el || aligning) return;
            aligning = true;
            scrollLock.current = true;
            scrollPdfPageToTop(scroller, el);
            scrollLock.current = false;
            aligning = false;
        };
        const scheduleRead = () => {
            if (frame) return;
            frame = requestAnimationFrame(readPage);
        };
        const onScroll = () => {
            if (scrollLock.current) return;
            // A click does not scroll. A drag is the pointer still being down.
            if (pointerDown) pinPage.current = 0;
            else if (pinPage.current) {
                const el = pages[pinPage.current - 1];
                // Measure now, not on the next frame. A translation card scrolls
                // during layout, and the resize observer runs before that frame.
                // Waiting would let the observer pull the jumped page back on top
                // of the card. Normal reading has no pin, so this does not run
                // on every scroll.
                if (el && Math.abs(el.getBoundingClientRect().top - scroller.getBoundingClientRect().top) >= 1) {
                    pinPage.current = 0;
                }
            }
            scheduleRead();
        };
        const onPointerDown = () => { pointerDown = true; };
        const onPointerUp = () => { pointerDown = false; };
        scroller.addEventListener("scroll", onScroll, { passive: true });
        scroller.addEventListener("pointerdown", onPointerDown);
        window.addEventListener("pointerup", onPointerUp);
        window.addEventListener("pointercancel", onPointerUp);
        const resize = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(() => {
            // scrollTop can change the scrollbar and re-enter this callback.
            if (aligning) return;
            realign();
            if (!pinPage.current || pinPage.current !== currentRef.current) scheduleRead();
        });
        resize?.observe(pagesBox ?? scroller);
        return () => {
            pagesRef.current = null;
            scroller.removeEventListener("scroll", onScroll);
            scroller.removeEventListener("pointerdown", onPointerDown);
            window.removeEventListener("pointerup", onPointerUp);
            window.removeEventListener("pointercancel", onPointerUp);
            resize?.disconnect();
            if (frame) cancelAnimationFrame(frame);
        };
    }, [pagesBoxRef, scrollerRef, total]);

    return (
        <form
            className="companion-pdf-pager"
            data-testid="companion-pdf-pager"
            aria-label={tailText(lang, `PDF pages, page ${current} of ${total}`, `PDF 页码，第 ${current} 页，共 ${total} 页`)}
            onSubmit={(event) => {
                event.preventDefault();
                commitGoto(draftRef.current);
            }}
            onContextMenu={(event) => event.stopPropagation()}
        >
            <button
                type="button"
                data-testid="companion-pdf-page-prev"
                aria-label={tailText(lang, "Previous page", "前一页")}
                title={tailText(lang, "Previous page", "前一页")}
                disabled={current <= 1}
                onMouseDown={(event) => press(event, () => stepPage(-1))}
                onClick={(event) => activate(event, () => stepPage(-1))}
            >
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                    <path d="M14 6 8 12l6 6" />
                    <path d="M8 6v12" />
                </svg>
            </button>
            <label className="companion-pdf-pager-field">
                {lang.startsWith("zh") ? <span>第</span> : null}
                <input
                    ref={inputRef}
                    className="companion-pdf-pager-input"
                    data-testid="companion-pdf-goto"
                    inputMode="numeric"
                    autoComplete="off"
                    spellCheck={false}
                    aria-label={tailText(lang, "Go to page", "跳转到页码")}
                    title={tailText(lang, "Type a page, then press Enter or Go", "输入页码，回车或点前往")}
                    value={draft}
                    onChange={(event) => {
                        const value = event.target.value.replace(/\D/g, "").slice(0, 6);
                        editing.current = true;
                        draftRef.current = value;
                        setDraft(value);
                    }}
                    onFocus={(event) => {
                        editing.current = true;
                        draftAtFocus.current = event.currentTarget.value;
                        event.currentTarget.select();
                    }}
                    onBlur={() => {
                        editing.current = false;
                        if (draftRef.current.trim() === draftAtFocus.current.trim()) {
                            setDraft(String(currentRef.current));
                            return;
                        }
                        commitGoto(draftRef.current);
                    }}
                />
                <span data-testid="companion-pdf-page-total">/ {total}{tailText(lang, "", " 页")}</span>
            </label>
            <button
                type="button"
                className="companion-pdf-pager-go"
                data-testid="companion-pdf-goto-go"
                aria-label={tailText(lang, "Go", "前往")}
                title={tailText(lang, "Go to this page", "前往该页")}
                onMouseDown={(event) => press(event, () => commitGoto(draftRef.current))}
                onClick={(event) => activate(event, () => commitGoto(draftRef.current))}
            >
                {tailText(lang, "Go", "前往")}
            </button>
            <button
                type="button"
                data-testid="companion-pdf-page-next"
                aria-label={tailText(lang, "Next page", "后一页")}
                title={tailText(lang, "Next page", "后一页")}
                disabled={current >= total}
                onMouseDown={(event) => press(event, () => stepPage(1))}
                onClick={(event) => activate(event, () => stepPage(1))}
            >
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                    <path d="m10 6 6 6-6 6" />
                    <path d="M16 6v12" />
                </svg>
            </button>
        </form>
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
    const pagesBoxRef = useRef<HTMLDivElement>(null);
    const [marks, setMarks] = useState<PdfMark[]>(() => loadPdfMarks(absPath));
    const [translations, setTranslations] = useState<PdfTranslation[]>(() => loadPdfTranslations(absPath));
    const [drawings, setDrawings] = useState<PdfDrawing[]>(() => loadPdfDrawings(absPath));
    const [drawMode, setDrawMode] = useState(false);
    const [drawColor, setDrawColor] = useState<PdfMarkColor>("yellow");
    const [drawTool, setDrawTool] = useState<PdfDrawTool>("line");
    const lastShapeToolRef = useRef<PdfDrawingTool>("line");
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

    const revealPage = useCallback((page: number) => {
        setSeen((current) => (current[page] ? current : { ...current, [page]: true }));
    }, []);

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
        setDrawings(loadPdfDrawings(absPath));
        setDrawMode(false);
        setOpenNoteId("");
        setMenu(null);
        setSeen({ 1: true });
    }, [absPath, applyStoredTranslations]);

    useEffect(() => subscribePdfTranslations((key) => {
        if (key !== pdfMarkPathKey(pathRef.current)) return;
        applyStoredTranslations(pathRef.current);
    }), [applyStoredTranslations]);

    useEffect(() => subscribePdfDrawings((key) => {
        if (key !== pdfMarkPathKey(pathRef.current)) return;
        setDrawings(loadPdfDrawings(pathRef.current));
    }), []);

    // A host read slower than the startup wait can land after this reader is
    // already mounted. Re-read the mirrors instead of painting stale state.
    useEffect(() => subscribeCompanionReaderState(() => {
        setMarks(loadPdfMarks(pathRef.current));
        setDrawings(loadPdfDrawings(pathRef.current));
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
                fail(tailText(lang, "This PDF has no content to preview", "该 PDF 没有可预览的内容"));
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
            // Escape inside the annotation editor closes only the editor; the
            // menu stays open so the note can be written or the action chosen.
            const target = event.target as HTMLElement | null;
            if (target?.closest?.("[data-testid='pdf-annotation-input']")) return;
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

    useEffect(() => {
        if (!drawMode) return undefined;
        const onKey = (event: KeyboardEvent) => {
            if (event.key === "Escape") setDrawMode(false);
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [drawMode]);

    const commit = (next: PdfMark[]) => {
        // A reader's own paint or clear is part of the shared project when this
        // document is hosted; the companion window uploads it after a debounce.
        const saved = savePdfMarks(absPath, next, true);
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
            setTranslateError(tailText(lang, "Copy failed. Select the text and copy it manually.", "复制失败，请手动选中后复制"));
        });
    };

    const clearNote = () => {
        const id = menu?.hitNoteId ?? "";
        if (!id) return;
        if (loadPdfTranslationDrafts(absPath).some((item) => item.id === id)) return;
        const stored = loadPdfTranslations(absPath);
        const removed = stored.find((item) => item.id === id);
        rememberPdfTranslationPlace(id, null);
        savePdfTranslations(absPath, stored.filter((item) => item.id !== id));
        if (removed?.kind === "annotation") notifyPdfAnnotationsChanged(absPath);
        setOpenNoteId((current) => (current === id ? "" : current));
        setMenu(null);
    };

    // A reader's own note on a selected passage (or an existing highlight).
    // It is displayed by the same card as a translation and shares with the
    // project, unlike the local LLM translations.
    const createAnnotation = (noteText: string) => {
        if (!menu) return;
        const body = noteText.trim();
        const parts = menu.parts.length > 0
            ? menu.parts.map((part) => ({ page: part.page, rects: part.rects }))
            : menu.hitMark ? [{ page: menu.hitMark.page, rects: menu.hitMark.rects }] : [];
        const source = (menu.parts[0]?.text || menu.hitMark?.text || "").trim();
        if (!body || parts.length === 0 || !source) return;
        const id = newPdfTranslationId();
        const notes: PdfTranslation = { id, kind: "annotation", text: source, translation: body, parts };
        const stored = loadPdfTranslations(absPath).filter((item) => item.id !== id);
        savePdfTranslations(absPath, [...stored, notes]);
        setOpenNoteId(id);
        setMenu(null);
        window.getSelection()?.removeAllRanges();
        notifyPdfAnnotationsChanged(absPath);
    };

    const resizeNote = (id: string, size: PdfTranslationSize | null) => {
        const stored = loadPdfTranslations(pathRef.current);
        const target = stored.find((item) => item.id === id);
        if (!target) return;
        const next = stored.map((item) => {
            if (item.id !== id) return item;
            if (!size) {
                const { size: _dropped, ...rest } = item;
                return rest;
            }
            return { ...item, size };
        });
        savePdfTranslations(pathRef.current, next);
        if (target.kind === "annotation") notifyPdfAnnotationsChanged(pathRef.current);
    };

    // A finished stroke or box is local to this reader until the companion
    // window hosts this document; that window uploads the project.
    const addDrawing = useCallback((drawing: PdfDrawing) => {
        const saved = savePdfDrawings(pathRef.current, [...loadPdfDrawings(pathRef.current), drawing], true);
        setDrawings(saved);
    }, []);

    const undoDrawing = useCallback(() => {
        const stored = loadPdfDrawings(pathRef.current);
        // A participant must not erase the host's stroke with 撤销; only the
        // newest drawing made on this machine is removable.
        let index = -1;
        for (let i = stored.length - 1; i >= 0; i -= 1) {
            if (!stored[i].shared) {
                index = i;
                break;
            }
        }
        if (index < 0) return;
        const saved = savePdfDrawings(pathRef.current, stored.filter((_, i) => i !== index), true);
        setDrawings(saved);
    }, []);

    const eraseDrawings = useCallback((ids: string[]) => {
        if (ids.length === 0) return;
        const doomed = new Set(ids);
        const stored = loadPdfDrawings(pathRef.current);
        // The layer already filters 主持人's drawings; keep the guard here so
        // a future caller cannot remove a received stroke by accident.
        const next = stored.filter((item) => item.shared || !doomed.has(item.id));
        if (next.length === stored.length) return;
        const saved = savePdfDrawings(pathRef.current, next, true);
        setDrawings(saved);
    }, []);

    const selectDrawTool = (next: PdfDrawTool) => {
        if (next !== "eraser") lastShapeToolRef.current = next;
        setDrawTool(next);
    };

    const toggleDrawMode = () => {
        if (!drawMode) {
            // Drawing and text selection compete for the same drag.
            setMenu(null);
            setOpenNoteId("");
            window.getSelection()?.removeAllRanges();
        }
        setDrawMode(!drawMode);
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
                failPdfTranslation(requestPath, id, translateErrorText(err, lang));
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
        const target = stored.find((item) => item.id === id);
        if (!target) {
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
        if (target.kind === "annotation") notifyPdfAnnotationsChanged(pathRef.current);
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
        let hitMark: PdfMark | null = null;
        let hitNoteId = target?.closest?.("[data-pdf-translation-id]")?.getAttribute("data-pdf-translation-id")?.trim() ?? "";
        let hitNoteKind: "" | "annotation" | "translation" = "";
        if (hitNoteId) {
            // A click on the card's icon carries the id, not the kind; read the
            // kind back so the menu offers 删除批注 and not 取消翻译.
            const direct = translationsRef.current.find((item) => item.id === hitNoteId);
            hitNoteKind = direct?.kind === "annotation" ? "annotation" : "translation";
        }
        if (pageEl) {
            const box = pageEl.getBoundingClientRect();
            const page = Number(pageEl.dataset.pdfPage);
            if (box.width > 0 && box.height > 0) {
                const x = (event.clientX - box.left) / box.width;
                const y = (event.clientY - box.top) / box.height;
                const hit = markAt(marksRef.current, page, x, y);
                if (hit) {
                    hitId = hit.id;
                    hitMark = hit;
                }
                if (!hitNoteId) {
                    const note = noteAt(translationsRef.current, page, x, y);
                    if (note) {
                        hitNoteId = note.id;
                        hitNoteKind = note.kind === "annotation" ? "annotation" : "translation";
                    }
                }
            }
        }
        const parts = selectionParts(root);
        if (parts.length === 0 && !hitId && !hitNoteId) {
            setMenu(null);
            return;
        }
        setMenu({ x: event.clientX, y: event.clientY, hitId, hitMark, hitNoteId, hitNoteKind, parts });
    };

    const fill: React.CSSProperties = { background: theme.bg, color: theme.text };
    if (error) {
        return <div data-testid="companion-pdf-error" className="companion-pdf-status" style={fill}>{error}</div>;
    }
    if (loading || !pdf) {
        return <div data-testid="companion-pdf-loading" role="status" className="companion-pdf-status" style={fill}>{tailText(lang, "Loading PDF…", "正在加载 PDF…")}</div>;
    }

    const pages = [];
    for (let page = 1; page <= pdf.numPages; page += 1) pages.push(page);
    const pendingVisible = pendingNote && !translations.some((item) => item.id === pendingNote.id) ? pendingNote : null;
    const shownNotes: ShownTranslation[] = pendingVisible ? [...translations, pendingVisible] : translations;
    const selectionText = menu?.parts[0]?.text.trim() ?? "";

    return (
        <div className="companion-pdf-reader" data-testid="companion-pdf-reader" style={fill} onContextMenu={onContextMenu}>
            <PdfPager
                lang={lang}
                total={pdf.numPages}
                scrollerRef={scrollerRef}
                pagesBoxRef={pagesBoxRef}
                onReveal={revealPage}
            />
            {translateError ? (
                <p className="companion-pdf-translate-error" role="status" data-testid="pdf-translate-error">{translateError}</p>
            ) : null}
            <div className="companion-pdf-stage">
            <div
                className="companion-pdf-draw"
                data-testid="pdf-draw"
                onContextMenu={(event) => {
                    event.preventDefault();
                    event.stopPropagation();
                }}
            >
                <button
                    type="button"
                    className="companion-pdf-draw-toggle"
                    data-testid="pdf-draw-toggle"
                    aria-label={tailText(lang, "Draw", "手绘")}
                    aria-pressed={drawMode}
                    title={drawMode ? tailText(lang, "Exit drawing", "退出手绘") : tailText(lang, "Draw on the PDF", "手绘 — 在 PDF 上画线或框")}
                    onClick={toggleDrawMode}
                >
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                        <path d="M4 20h4L18.5 9.5a2.5 2.5 0 0 0-3.5-3.5L4 16.5V20Z" />
                        <path d="m13.5 7.5 3 3" />
                    </svg>
                </button>
                {drawMode ? (
                    <div className="companion-pdf-draw-panel" data-testid="pdf-draw-panel">
                        <div className="companion-pdf-draw-colors" role="group" aria-label={tailText(lang, "Drawing colour", "手绘颜色")}>
                            {PDF_MARK_COLORS.map((option) => (
                                <button
                                    key={option.id}
                                    type="button"
                                    className="companion-pdf-menu-color"
                                    data-testid={`pdf-draw-color-${option.id}`}
                                    aria-label={pdfSwatchLabel(option.id, lang, "pen")}
                                    aria-pressed={drawColor === option.id}
                                    disabled={drawTool === "eraser"}
                                    title={pdfSwatchLabel(option.id, lang, "pen")}
                                    onClick={() => setDrawColor(option.id)}
                                >
                                    <span style={{ background: PDF_DRAWING_INK[option.id] }} />
                                </button>
                            ))}
                        </div>
                        <span className="companion-pdf-draw-sep" aria-hidden="true" />
                        <div className="companion-pdf-draw-tools" role="group" aria-label={tailText(lang, "Drawing tools", "手绘工具")}>
                            <button
                                type="button"
                                data-testid="pdf-draw-line"
                                aria-label={tailText(lang, "Draw a line", "画线")}
                                aria-pressed={drawTool === "line"}
                                title={tailText(lang, "Draw a line", "画线")}
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
                                data-testid="pdf-draw-box"
                                aria-label={tailText(lang, "Draw a box", "画框")}
                                aria-pressed={drawTool === "box"}
                                title={tailText(lang, "Draw a box", "画框")}
                                onClick={() => selectDrawTool("box")}
                            >
                                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                                    <rect x="4" y="6" width="16" height="12" rx="1.5" />
                                </svg>
                            </button>
                            <button
                                type="button"
                                data-testid="pdf-draw-eraser"
                                aria-label={tailText(lang, "Erase", "擦除")}
                                aria-pressed={drawTool === "eraser"}
                                title={tailText(lang, "Erase — drag a box over strokes or frames", "擦除 — 圈选要清除的线或框")}
                                onClick={() => selectDrawTool(drawTool === "eraser" ? lastShapeToolRef.current : "eraser")}
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
                                data-testid="pdf-draw-undo"
                                aria-label={tailText(lang, "Undo the last stroke", "撤销上一笔")}
                                disabled={!drawings.some((item) => !item.shared)}
                                title={tailText(lang, "Undo the last stroke", "撤销上一笔")}
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
            </div>
            <div className="companion-pdf-scroll" ref={scrollerRef} data-testid="companion-pdf-scroll">
                <div ref={pagesBoxRef}>
                {pages.map((page) => (
                    <PdfPageView
                        key={page}
                        lang={lang}
                        pdf={pdf}
                        pageNumber={page}
                        width={width}
                        marks={marks}
                        drawings={drawings}
                        drawActive={drawMode}
                        drawColor={drawColor}
                        drawTool={drawTool}
                        onAddDrawing={addDrawing}
                        onEraseDrawing={eraseDrawings}
                        notes={shownNotes}
                        openNoteId={openNoteId}
                        onToggleNote={toggleNote}
                        onPlaceNote={placeNote}
                        onResizeNote={resizeNote}
                        active={page === 1 || seen[page] === true}
                    />
                ))}
                </div>
            </div>
            </div>
            {menu ? (
                <PdfSelectionMenu
                    lang={lang}
                    state={menu}
                    selectionText={selectionText}
                    onTranslate={translateSelection}
                    onCopy={copySelection}
                    onPaint={paint}
                    onAnnotate={createAnnotation}
                    onClearNote={clearNote}
                    onClearMark={clearHit}
                    onClose={closeMenu}
                />
            ) : null}
        </div>
    );
}
