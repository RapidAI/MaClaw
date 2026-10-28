// Freehand drawings live beside the PDF highlights. They are not written into
// the PDF. The store is the desktop SQLite database; this module keeps a
// synchronous mirror that hydratePdfDrawings fills once at startup.

import { enqueueCompanionReaderWrite } from "./companionReaderWriteQueue";
import { pdfMarkPathKey, type PdfMarkColor, type PdfMarkRect } from "./pdfMarks";

export type PdfDrawingTool = "line" | "box";

// A normalized point on the rendered page. Page fractions keep a stroke in
// place when the reader zooms or the window changes.
export type PdfDrawingPoint = { x: number; y: number };

export type PdfDrawing = {
    id: string;
    page: number;
    color: PdfMarkColor;
    tool: PdfDrawingTool;
    // A line keeps every sampled point; a box keeps its two corners.
    points: PdfDrawingPoint[];
    // shared marks a drawing that came from the host's project. A later host
    // sync replaces exactly these; a reader's own drawing stays local.
    shared?: boolean;
};

export const PDF_DRAWING_MAX_POINTS = 1200;
const MAX_DRAWINGS = 200;
const BOX_MIN_SIZE = 0.002;

const fallback = new Map<string, PdfDrawing[]>();

let store: Record<string, unknown> = {};
let saveRevision = 0;

export function hydratePdfDrawings(value: unknown): void {
    store = value && typeof value === "object" && !Array.isArray(value)
        ? { ...(value as Record<string, unknown>) }
        : {};
    fallback.clear();
}

export function pdfDrawingsSaveRevision(): number {
    return saveRevision;
}

const writeSeq = new Map<string, number>();

function savePdfDrawingsToHost(key: string, drawings: PdfDrawing[]): void {
    const app = (window as unknown as { go?: { main?: { App?: Record<string, unknown> } } }).go?.main?.App;
    const save = app?.SaveCompanionPdfDrawings;
    if (typeof save !== "function") return;
    const seq = (writeSeq.get(key) ?? 0) + 1;
    writeSeq.set(key, seq);
    const body = JSON.stringify(drawings);
    enqueueCompanionReaderWrite(async () => {
        try {
            await (save as (path: string, body: string) => Promise<unknown>)(key, body);
        } catch {
            // Keep the drawings for this session so the reader still shows
            // them. Only the newest failed write may supply the fallback.
            if (writeSeq.get(key) === seq) fallback.set(key, drawings);
        }
    });
}

const listeners = new Set<(key: string) => void>();

function emitPdfDrawings(key: string): void {
    if (!key) return;
    for (const listener of [...listeners]) listener(key);
}

export function subscribePdfDrawings(listener: (key: string) => void): () => void {
    listeners.add(listener);
    return () => {
        listeners.delete(listener);
    };
}

// A reader's own stroke. The companion window listens and pushes the project
// to shares this document hosts; received drawings never announce.
export const PDF_DRAWINGS_CHANGED_EVENT = "maclaw:companion-pdf-drawings-changed";

export function notifyPdfDrawingsChanged(path: string): void {
    if (!path) return;
    try {
        window.dispatchEvent(new CustomEvent(PDF_DRAWINGS_CHANGED_EVENT, { detail: { path } }));
    } catch {
        // A non-browser host has no event bus.
    }
}

function onePoint(value: unknown): PdfDrawingPoint | null {
    if (!value || typeof value !== "object") return null;
    const raw = value as { x?: unknown; y?: unknown };
    const x = Number(raw.x);
    const y = Number(raw.y);
    if (!Number.isFinite(x) || !Number.isFinite(y)) return null;
    if (x < 0 || x > 1 || y < 0 || y > 1) return null;
    return { x, y };
}

function oneDrawing(value: unknown): PdfDrawing | null {
    if (!value || typeof value !== "object") return null;
    const raw = value as { id?: unknown; page?: unknown; color?: unknown; tool?: unknown; points?: unknown; shared?: unknown };
    const color = String(raw.color || "");
    if (color !== "yellow" && color !== "green" && color !== "blue" && color !== "pink") return null;
    const page = Math.floor(Number(raw.page));
    if (!Number.isFinite(page) || page < 1 || page > 100000) return null;
    const tool = String(raw.tool || "");
    if (tool !== "line" && tool !== "box") return null;
    if (!Array.isArray(raw.points)) return null;
    const points: PdfDrawingPoint[] = [];
    for (const item of raw.points) {
        if (points.length >= PDF_DRAWING_MAX_POINTS) break;
        const point = onePoint(item);
        if (point) points.push(point);
    }
    if (points.length < 2) return null;
    if (tool === "box") {
        const start = points[0];
        const end = points[points.length - 1];
        if (Math.abs(end.x - start.x) < BOX_MIN_SIZE && Math.abs(end.y - start.y) < BOX_MIN_SIZE) return null;
        points.splice(0, points.length, start, end);
    }
    const id = String(raw.id || "").trim().slice(0, 80);
    if (!id) return null;
    const kept: PdfDrawing = { id, page, color, tool, points };
    if (raw.shared === true) kept.shared = true;
    return kept;
}

/** Returns null when the value is not a list. An empty list is a real clear. */
export function normalizePdfDrawings(value: unknown): PdfDrawing[] | null {
    if (!Array.isArray(value)) return null;
    const drawings: PdfDrawing[] = [];
    for (const item of value) {
        const drawing = oneDrawing(item);
        if (!drawing) continue;
        drawings.push(drawing);
        // The newest stroke is the one the reader just made; drop the oldest
        // from a full document instead of refusing the new one.
        if (drawings.length > MAX_DRAWINGS) drawings.shift();
    }
    return drawings;
}

export function loadPdfDrawings(path: string): PdfDrawing[] {
    const key = pdfMarkPathKey(path);
    if (!key) return [];
    const held = fallback.get(key);
    if (held) return held;
    return normalizePdfDrawings(store[key]) ?? [];
}

export function savePdfDrawings(path: string, value: unknown, announce = false): PdfDrawing[] {
    const key = pdfMarkPathKey(path);
    const drawings = normalizePdfDrawings(value) ?? [];
    if (!key) return drawings;
    fallback.delete(key);
    if (drawings.length === 0) delete store[key];
    else store[key] = drawings;
    saveRevision += 1;
    savePdfDrawingsToHost(key, drawings);
    emitPdfDrawings(key);
    if (announce) notifyPdfDrawingsChanged(path);
    return drawings;
}

export function newPdfDrawingId(): string {
    const rand = Math.random().toString(36).slice(2, 8);
    return `d_${Date.now().toString(36)}_${rand}`;
}

const boundsCache = new WeakMap<PdfDrawing, PdfMarkRect>();

export function pdfDrawingBounds(drawing: PdfDrawing): PdfMarkRect {
    const cached = boundsCache.get(drawing);
    if (cached) return cached;
    let left = 1;
    let top = 1;
    let right = 0;
    let bottom = 0;
    for (const point of drawing.points) {
        if (point.x < left) left = point.x;
        if (point.y < top) top = point.y;
        if (point.x > right) right = point.x;
        if (point.y > bottom) bottom = point.y;
    }
    const bounds = { x: left, y: top, w: right - left, h: bottom - top };
    boundsCache.set(drawing, bounds);
    return bounds;
}

// Inclusive: an eraser box that merely touches a stroke still selects it,
// which matches the marquee the reader paints.
export function pdfRectsIntersect(a: PdfMarkRect, b: PdfMarkRect): boolean {
    return a.x <= b.x + b.w && b.x <= a.x + a.w && a.y <= b.y + b.h && b.y <= a.y + a.h;
}

function pointInRect(point: PdfDrawingPoint, rect: PdfMarkRect): boolean {
    return point.x >= rect.x && point.x <= rect.x + rect.w && point.y >= rect.y && point.y <= rect.y + rect.h;
}

// Liang-Barsky: does the segment touch the selection at all? A diagonal
// stroke's bounding box covers much more than the ink, so a rectangle hit on
// the bbox alone would erase strokes the marquee never crossed.
function segmentHitsRect(a: PdfDrawingPoint, b: PdfDrawingPoint, rect: PdfMarkRect): boolean {
    const dx = b.x - a.x;
    const dy = b.y - a.y;
    const edges = [-dx, dx, -dy, dy];
    const limits = [a.x - rect.x, rect.x + rect.w - a.x, a.y - rect.y, rect.y + rect.h - a.y];
    let enter = 0;
    let leave = 1;
    for (let i = 0; i < edges.length; i += 1) {
        const edge = edges[i];
        if (edge === 0) {
            if (limits[i] < 0) return false;
            continue;
        }
        const t = limits[i] / edge;
        if (edge < 0) enter = Math.max(enter, t);
        else leave = Math.min(leave, t);
        if (enter > leave) return false;
    }
    return true;
}

export function pdfDrawingHitsRect(drawing: PdfDrawing, rect: PdfMarkRect): boolean {
    // Cheap reject first: a stroke whose bounds miss the marquee can never
    // have a point or segment inside it.
    if (!pdfRectsIntersect(pdfDrawingBounds(drawing), rect)) return false;
    if (drawing.tool === "box") return true;
    if (drawing.points.some((point) => pointInRect(point, rect))) return true;
    for (let i = 1; i < drawing.points.length; i += 1) {
        if (segmentHitsRect(drawing.points[i - 1], drawing.points[i], rect)) return true;
    }
    return false;
}
