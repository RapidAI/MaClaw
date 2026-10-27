// Companion PDF marks live with the reader. They are not written into the PDF.
// The store is the desktop SQLite database (companion_reader_state.db). This
// module keeps a synchronous mirror so the reader can render without awaiting a
// binding; hydratePdfMarks fills it once at startup.

import { enqueueCompanionReaderWrite } from "./companionReaderWriteQueue";

// Older builds kept the same document under this WebView localStorage key. It
// is read once during startup migration and removed after the import succeeds.
export const PDF_MARK_STORAGE_KEY = "maclaw.fileCompanionPdfMarks.v1";

export const PDF_MARK_COLORS = [
    { id: "yellow", label: "黄色", fill: "#ffe08a" },
    { id: "green", label: "绿色", fill: "#b6e7c3" },
    { id: "blue", label: "蓝色", fill: "#b9d4f5" },
    { id: "pink", label: "粉色", fill: "#f6c1d8" },
] as const;

export type PdfMarkColor = (typeof PDF_MARK_COLORS)[number]["id"];

export type PdfMarkRect = { x: number; y: number; w: number; h: number };

export type PdfMark = {
    id: string;
    page: number;
    color: PdfMarkColor;
    rects: PdfMarkRect[];
    text: string;
};

const MAX_MARKS = 2000;
// Line boxes, not raw client rects. 40 covered only the top of a long page.
const MAX_RECTS = 160;
const MAX_TEXT = 500;

const FILL: Record<PdfMarkColor, string> = {
    yellow: "#ffe08a",
    green: "#b6e7c3",
    blue: "#b9d4f5",
    pink: "#f6c1d8",
};

const fallback = new Map<string, PdfMark[]>();

// Synchronous mirror of the SQLite store, keyed by pdfMarkPathKey.
let store: Record<string, unknown> = {};
// Counts saves so a startup read that began before a save cannot overwrite the
// newer in-memory state with its older snapshot.
let saveRevision = 0;

// hydratePdfMarks replaces the mirror with the value read from SQLite at
// startup. A non-object clears it, which is also how tests start clean.
export function hydratePdfMarks(value: unknown): void {
    store = value && typeof value === "object" && !Array.isArray(value)
        ? { ...(value as Record<string, unknown>) }
        : {};
    fallback.clear();
}

export function pdfMarksSaveRevision(): number {
    return saveRevision;
}

const writeSeq = new Map<string, number>();

function savePdfMarksToHost(key: string, marks: PdfMark[]): void {
    const app = (window as unknown as { go?: { main?: { App?: Record<string, unknown> } } }).go?.main?.App;
    const save = app?.SaveCompanionPdfMarks;
    if (typeof save !== "function") return;
    const seq = (writeSeq.get(key) ?? 0) + 1;
    writeSeq.set(key, seq);
    const body = JSON.stringify(marks);
    enqueueCompanionReaderWrite(async () => {
        try {
            await (save as (path: string, body: string) => Promise<unknown>)(key, body);
        } catch {
            // Keep the marks for this session so the reader still shows them.
            // Only the newest failed write may supply the fallback; an older
            // failure must not mask a newer save.
            if (writeSeq.get(key) === seq) fallback.set(key, marks);
        }
    });
}

export function pdfMarkFill(color: PdfMarkColor): string {
    return FILL[color];
}

export function pdfMarkPathKey(path: string): string {
    let key = String(path || "").trim().replace(/\\/g, "/");
    if (/^[a-zA-Z]:\//.test(key) || key.startsWith("//")) key = key.toLowerCase();
    return key;
}

function oneRect(value: unknown): PdfMarkRect | null {
    if (!value || typeof value !== "object") return null;
    const raw = value as { x?: unknown; y?: unknown; w?: unknown; h?: unknown };
    const x = Number(raw.x);
    const y = Number(raw.y);
    const w = Number(raw.w);
    const h = Number(raw.h);
    if (![x, y, w, h].every((item) => Number.isFinite(item))) return null;
    if (w < 0.001 || h < 0.001 || x < 0 || y < 0 || x > 1 || y > 1) return null;
    // Keep an in-range box as written. Rebuilding width from the right edge
    // turns 0.3 into 0.30000000000000004 and the stored mark never matches.
    if (x + w <= 1 && y + h <= 1) return { x, y, w, h };
    const right = Math.min(1, x + w);
    const bottom = Math.min(1, y + h);
    if (right - x < 0.001 || bottom - y < 0.001) return null;
    return { x, y, w: right - x, h: bottom - y };
}

function oneMark(value: unknown): PdfMark | null {
    if (!value || typeof value !== "object") return null;
    const raw = value as { id?: unknown; page?: unknown; color?: unknown; rects?: unknown; text?: unknown };
    const color = String(raw.color || "");
    if (color !== "yellow" && color !== "green" && color !== "blue" && color !== "pink") return null;
    const page = Math.floor(Number(raw.page));
    if (!Number.isFinite(page) || page < 1 || page > 100000) return null;
    if (!Array.isArray(raw.rects)) return null;
    const rects: PdfMarkRect[] = [];
    for (const item of raw.rects) {
        if (rects.length >= MAX_RECTS) break;
        const rect = oneRect(item);
        if (rect) rects.push(rect);
    }
    if (rects.length === 0) return null;
    const id = String(raw.id || "").trim().slice(0, 80);
    if (!id) return null;
    return {
        id,
        page,
        color,
        rects,
        text: String(raw.text || "").replace(/\s+/g, " ").trim().slice(0, MAX_TEXT),
    };
}

/** Returns null when the value is not a list. An empty list is a real import. */
export function normalizePdfMarks(value: unknown): PdfMark[] | null {
    const list = Array.isArray(value) ? value : null;
    if (!list) return null;
    const marks: PdfMark[] = [];
    for (const item of list) {
        if (marks.length >= MAX_MARKS) break;
        const mark = oneMark(item);
        if (mark) marks.push(mark);
    }
    return marks;
}

export function loadPdfMarks(path: string): PdfMark[] {
    const key = pdfMarkPathKey(path);
    if (!key) return [];
    const held = fallback.get(key);
    if (held) return held;
    return normalizePdfMarks(store[key]) ?? [];
}

// loadPdfMarkCounts answers the same question for many paths at once.
export function loadPdfMarkCounts(paths: string[]): Map<string, number> {
    const counts = new Map<string, number>();
    for (const path of paths) {
        const key = pdfMarkPathKey(path);
        if (!key || counts.has(key)) continue;
        const held = fallback.get(key);
        counts.set(key, held ? held.length : (normalizePdfMarks(store[key]) ?? []).length);
    }
    return counts;
}

export function savePdfMarks(path: string, value: unknown): PdfMark[] {
    const key = pdfMarkPathKey(path);
    const marks = normalizePdfMarks(value) ?? [];
    if (!key) return marks;
    fallback.delete(key);
    if (marks.length === 0) delete store[key];
    else store[key] = marks;
    saveRevision += 1;
    savePdfMarksToHost(key, marks);
    return marks;
}

export function newPdfMarkId(): string {
    const rand = Math.random().toString(36).slice(2, 8);
    return `m_${Date.now().toString(36)}_${rand}`;
}
