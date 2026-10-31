// Companion PDF bookmarks live with the document project, the same way
// highlights do. They are not written into the PDF. The store is the desktop
// SQLite database (companion_reader_state.db). This module keeps a synchronous
// mirror so the reader can render without awaiting a binding; hydratePdfBookmarks
// fills it once at startup. Array order is the order the reader added them.

import { loadChatMarks } from "./chatMarks";
import { enqueueCompanionReaderWrite } from "./companionReaderWriteQueue";
import { loadPdfDrawings } from "./pdfDrawings";
import { loadPdfMarks, pdfMarkPathKey } from "./pdfMarks";
import { loadPdfAnnotations } from "./pdfTranslations";

export const PDF_BOOKMARK_COLORS = [
    { id: "yellow", label: "黄色", fill: "#ffe08a", ink: "#c9930a" },
    { id: "orange", label: "橙色", fill: "#ffc078", ink: "#d97816" },
    { id: "red", label: "红色", fill: "#f5a3a3", ink: "#c44848" },
    { id: "pink", label: "粉色", fill: "#f6c1d8", ink: "#c74884" },
    { id: "purple", label: "紫色", fill: "#d4c2f0", ink: "#7a4eb8" },
    { id: "blue", label: "蓝色", fill: "#b9d4f5", ink: "#2f6bbd" },
    { id: "teal", label: "青色", fill: "#9ad8d0", ink: "#1f8a80" },
    { id: "green", label: "绿色", fill: "#b6e7c3", ink: "#2f8f5b" },
] as const;

export type PdfBookmarkColor = (typeof PDF_BOOKMARK_COLORS)[number]["id"];

export type PdfBookmark = {
    id: string;
    page: number;
    x: number;
    y: number;
    color: PdfBookmarkColor;
    name: string;
};

export const PDF_BOOKMARK_LIMIT = 200;
const MAX_NAME = 40;
/** Hanging ribbon. A closer pair steps sideways so the tails do not cover each other. */
export const PDF_BOOKMARK_TICK_PX = 40;
/** Painted width. Matches the ribbon viewBox so the shape is not stretched. */
export const PDF_BOOKMARK_TICK_WIDTH_PX = 16;
/** Next ribbon. Two pixels past the painted width keep the shadows apart. */
export const PDF_BOOKMARK_TICK_LANE_PX = PDF_BOOKMARK_TICK_WIDTH_PX + 2;
/** Keep the marked line a short way below the pager, not under it. */
export const PDF_BOOKMARK_SCROLL_INSET = 72;

const FILL: Record<PdfBookmarkColor, string> = {
    yellow: "#ffe08a",
    orange: "#ffc078",
    red: "#f5a3a3",
    pink: "#f6c1d8",
    purple: "#d4c2f0",
    blue: "#b9d4f5",
    teal: "#9ad8d0",
    green: "#b6e7c3",
};

const INK: Record<PdfBookmarkColor, string> = {
    yellow: "#c9930a",
    orange: "#d97816",
    red: "#c44848",
    pink: "#c74884",
    purple: "#7a4eb8",
    blue: "#2f6bbd",
    teal: "#1f8a80",
    green: "#2f8f5b",
};

const fallback = new Map<string, PdfBookmark[]>();

let store: Record<string, unknown> = {};
let saveRevision = 0;

export function hydratePdfBookmarks(value: unknown): void {
    store = value && typeof value === "object" && !Array.isArray(value)
        ? { ...(value as Record<string, unknown>) }
        : {};
    fallback.clear();
}

export function pdfBookmarksSaveRevision(): number {
    return saveRevision;
}

const writeSeq = new Map<string, number>();

function savePdfBookmarksToHost(key: string, bookmarks: PdfBookmark[]): void {
    const app = (window as unknown as { go?: { main?: { App?: Record<string, unknown> } } }).go?.main?.App;
    const save = app?.SaveCompanionPdfBookmarks;
    if (typeof save !== "function") return;
    const seq = (writeSeq.get(key) ?? 0) + 1;
    writeSeq.set(key, seq);
    const body = JSON.stringify(bookmarks);
    enqueueCompanionReaderWrite(async () => {
        try {
            await (save as (path: string, body: string) => Promise<unknown>)(key, body);
        } catch {
            if (writeSeq.get(key) === seq) fallback.set(key, bookmarks);
        }
    });
}

export function pdfBookmarkFill(color: PdfBookmarkColor): string {
    return FILL[color];
}

export function pdfBookmarkInk(color: PdfBookmarkColor): string {
    return INK[color];
}

function oneFraction(value: unknown): number | null {
    const n = Number(value);
    if (!Number.isFinite(n) || n < 0 || n > 1) return null;
    return n;
}

function oneBookmark(value: unknown): PdfBookmark | null {
    if (!value || typeof value !== "object") return null;
    const raw = value as { id?: unknown; page?: unknown; x?: unknown; y?: unknown; color?: unknown; name?: unknown };
    const color = String(raw.color || "");
    if (!(color in FILL)) return null;
    const page = Math.floor(Number(raw.page));
    if (!Number.isFinite(page) || page < 1 || page > 100000) return null;
    const x = oneFraction(raw.x);
    const y = oneFraction(raw.y);
    if (x === null || y === null) return null;
    const id = String(raw.id || "").trim();
    // The project cleaner drops an id longer than 80 bytes. Keep the same
    // identity here instead of saving a shortened id the project would reject.
    if (!id || id.length > 80) return null;
    const name = String(raw.name || "").replace(/\s+/g, " ").trim();
    const chars = Array.from(name);
    if (chars.length === 0) return null;
    return {
        id,
        page,
        x,
        y,
        color: color as PdfBookmarkColor,
        name: chars.slice(0, MAX_NAME).join(""),
    };
}

/** Returns null when the value is not a list. An empty list is a real import.
 *  Order is the order they were added. The same id keeps that place and takes
 *  the later fields. Past the cap, the oldest entries drop so a new bookmark lands. */
export function normalizePdfBookmarks(value: unknown): PdfBookmark[] | null {
    const list = Array.isArray(value) ? value : null;
    if (!list) return null;
    const bookmarks: PdfBookmark[] = [];
    const indexById = new Map<string, number>();
    for (const item of list) {
        const bookmark = oneBookmark(item);
        if (!bookmark) continue;
        const at = indexById.get(bookmark.id);
        if (at === undefined) {
            indexById.set(bookmark.id, bookmarks.length);
            bookmarks.push(bookmark);
        } else {
            bookmarks[at] = bookmark;
        }
    }
    if (bookmarks.length > PDF_BOOKMARK_LIMIT) {
        return bookmarks.slice(bookmarks.length - PDF_BOOKMARK_LIMIT);
    }
    return bookmarks;
}

/** Top of the hanging ribbon, in page pixels. A mark near the bottom slides up so the tail stays on the page. */
export function pdfBookmarkTickTop(y: number, pageHeight: number): number {
    if (!(pageHeight > 0) || !Number.isFinite(y)) return 0;
    const mark = Math.min(1, Math.max(0, y)) * pageHeight;
    const overflow = mark + PDF_BOOKMARK_TICK_PX - pageHeight;
    const top = overflow > 0 ? mark - overflow : mark;
    return Math.round(top > 0 ? top : 0);
}

/** Lane index for the edge ticks on one page. Zero stays on the page edge.
 *  Lanes use the ribbon's top after it has slid up to stay on the page, so a
 *  mark near the bottom does not hang on the ribbon above it. */
export function pdfBookmarkTickLanes(marks: { y: number }[], pageHeight: number): number[] {
    const height = pageHeight > 0 ? pageHeight : 0;
    const lanes: number[] = [];
    const out = new Array<number>(marks.length).fill(0);
    const order = marks.map((mark, index) => ({
        index,
        top: height > 0 ? pdfBookmarkTickTop(mark.y, height) : 0,
    }));
    order.sort((a, b) => a.top - b.top || a.index - b.index);
    for (const mark of order) {
        let lane = 0;
        if (height > 0) {
            while (lane < lanes.length && mark.top < lanes[lane] + PDF_BOOKMARK_TICK_PX - 0.5) lane += 1;
        }
        lanes[lane] = mark.top;
        out[mark.index] = lane;
    }
    return out;
}

export function pdfBookmarkScrollTop(pageTop: number, pageHeight: number, y: number): number {
    if (!(pageHeight > 0) || !Number.isFinite(pageTop) || !Number.isFinite(y)) return 0;
    const next = pageTop + y * pageHeight - PDF_BOOKMARK_SCROLL_INSET;
    return next > 0 ? next : 0;
}

/** A scroll leaves the page's content coordinate alone: the visual top moves
 *  opposite scrollTop, so pageTop stays put. A frozen test rect instead adds
 *  the scroll delta onto pageTop. Neither of those is the page moving in the
 *  document. A different height, or a pageTop change that scroll does not
 *  explain, means the shell grew or the pages above it moved. */
export function pdfBookmarkLayoutMoved(
    prev: { pageTop: number; pageHeight: number; scrollTop: number } | null,
    next: { pageTop: number; pageHeight: number; scrollTop: number },
): boolean {
    if (!(next.pageHeight > 0)) return false;
    if (!prev) return true;
    if (Math.abs(next.pageHeight - prev.pageHeight) > 0.5) return true;
    const topDelta = next.pageTop - prev.pageTop;
    if (Math.abs(topDelta) <= 0.5) return false;
    const scrollDelta = next.scrollTop - prev.scrollTop;
    return Math.abs(topDelta - scrollDelta) > 0.5;
}

/** The reader moved. pageTop is the page's content coordinate, so a wheel,
 *  a drag, or the pager leaves it put and only changes scrollTop. Anchoring
 *  and a shell above the mark move pageTop. A height change in the same
 *  frame still counts as the reader when the content coordinate stayed. */
export function pdfBookmarkScrollIsReader(
    prev: { pageTop: number; pageHeight: number; scrollTop: number } | null,
    next: { pageTop: number; pageHeight: number; scrollTop: number },
): boolean {
    if (!prev) return false;
    if (Math.abs(next.scrollTop - prev.scrollTop) <= 1) return false;
    return Math.abs(next.pageTop - prev.pageTop) <= 0.5;
}

export function loadPdfBookmarks(path: string): PdfBookmark[] {
    const key = pdfMarkPathKey(path);
    if (!key) return [];
    const held = fallback.get(key);
    if (held) return held;
    return normalizePdfBookmarks(store[key]) ?? [];
}

const listeners = new Set<(key: string) => void>();

function emitPdfBookmarks(key: string): void {
    if (!key) return;
    for (const listener of [...listeners]) listener(key);
}

export function subscribePdfBookmarks(listener: (key: string) => void): () => void {
    listeners.add(listener);
    return () => {
        listeners.delete(listener);
    };
}

// A reader's own add, rename, or delete. The companion window listens and
// pushes the project to the shares this document hosts. Applying a received
// share must not announce, or two readers would echo updates back and forth.
export const PDF_BOOKMARKS_CHANGED_EVENT = "maclaw:companion-pdf-bookmarks-changed";

export function savePdfBookmarks(path: string, value: unknown, announce = false): PdfBookmark[] {
    const key = pdfMarkPathKey(path);
    const bookmarks = normalizePdfBookmarks(value) ?? [];
    if (!key) return bookmarks;
    fallback.delete(key);
    if (bookmarks.length === 0) delete store[key];
    else store[key] = bookmarks;
    saveRevision += 1;
    savePdfBookmarksToHost(key, bookmarks);
    emitPdfBookmarks(key);
    if (announce) {
        try {
            window.dispatchEvent(new CustomEvent(PDF_BOOKMARKS_CHANGED_EVENT, { detail: { path } }));
        } catch {
            // A non-browser host has no event bus; the host write still ran.
        }
    }
    return bookmarks;
}

export function newPdfBookmarkId(): string {
    const rand = Math.random().toString(36).slice(2, 8);
    return `b_${Date.now().toString(36)}_${rand}`;
}

// Every reader mark travels in one project payload: highlights, answer marks,
// placed notes, freehand drawings, and bookmarks.
export function companionProjectMarksJSON(path: string): string {
    return JSON.stringify({
        highlights: loadPdfMarks(path),
        chatMarks: loadChatMarks(path),
        annotations: loadPdfAnnotations(path),
        drawings: loadPdfDrawings(path),
        bookmarks: loadPdfBookmarks(path),
    });
}
