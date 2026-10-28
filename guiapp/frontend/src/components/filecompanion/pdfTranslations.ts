// Companion PDF translations live beside the highlight marks. They are not
// written into the PDF and they are not a highlight colour. The store is the
// desktop SQLite database; this module keeps a synchronous mirror that
// hydratePdfTranslations fills once at startup.

import { pdfMarkPathKey, type PdfMarkRect } from "./pdfMarks";
import { enqueueCompanionReaderWrite } from "./companionReaderWriteQueue";

// Older builds kept the same document under this WebView localStorage key. It
// is read once during startup migration and removed after the import succeeds.
export const PDF_TRANSLATION_STORAGE_KEY = "maclaw.fileCompanionPdfTranslations.v1";

export type PdfTranslationPart = { page: number; rects: PdfMarkRect[] };

export type PdfTranslationPlace = { x: number; y: number; above?: boolean };

// A resized card's outer size, as a fraction of the rendered page. Fractions
// keep the box proportional when the reader zooms or the window changes.
export type PdfTranslationSize = { w: number; h: number };

export type PdfTranslation = {
    id: string;
    text: string;
    translation: string;
    parts: PdfTranslationPart[];
    clipped?: boolean;
    place?: PdfTranslationPlace;
    size?: PdfTranslationSize;
    // An annotation is a reader's own note on the selection, displayed in the
    // same card as a translation. The note body is stored in translation.
    kind?: "annotation";
    // shared marks an annotation that came from the host's project. A later
    // host sync replaces exactly these; a reader's own note is kept local.
    shared?: boolean;
};

// A draft is the in-flight request. The PDF reader remounts when the tab
// changes, so the request cannot live only in that component.
export type PdfTranslationDraft = {
    id: string;
    text: string;
    parts: PdfTranslationPart[];
};

const MAX_TRANSLATIONS = 200;
const MAX_PARTS = 30;
// One box per line. 40 raw client rects covered only about the first six lines.
const MAX_RECTS = 160;
const MAX_SOURCE = 4000;
const MAX_TRANSLATION = 12000;

const fallback = new Map<string, PdfTranslation[]>();

// Synchronous mirror of the SQLite store, keyed by pdfMarkPathKey.
let store: Record<string, unknown> = {};
let saveRevision = 0;

// hydratePdfTranslations replaces the mirror with the value read from SQLite at
// startup. A non-object clears it, which is also how tests start clean.
export function hydratePdfTranslations(value: unknown): void {
    store = value && typeof value === "object" && !Array.isArray(value)
        ? { ...(value as Record<string, unknown>) }
        : {};
    fallback.clear();
}

export function pdfTranslationsSaveRevision(): number {
    return saveRevision;
}

const writeSeq = new Map<string, number>();

function savePdfTranslationsToHost(key: string, list: PdfTranslation[]): void {
    const app = (window as unknown as { go?: { main?: { App?: Record<string, unknown> } } }).go?.main?.App;
    const save = app?.SaveCompanionPdfTranslations;
    if (typeof save !== "function") return;
    const seq = (writeSeq.get(key) ?? 0) + 1;
    writeSeq.set(key, seq);
    const body = JSON.stringify(list);
    enqueueCompanionReaderWrite(async () => {
        try {
            await (save as (path: string, body: string) => Promise<unknown>)(key, body);
        } catch {
            if (writeSeq.get(key) === seq) fallback.set(key, list);
        }
    });
}
const flight = new Map<string, PdfTranslationDraft[]>();
const flightError = new Map<string, string>();
const draggedPlace = new Map<string, PdfTranslationPlace>();
const listeners = new Set<(key: string) => void>();

const PLACE_LIMIT = 4;

export function clampPdfTranslationPlace(x: number, y: number, above?: boolean): PdfTranslationPlace | null {
    if (!Number.isFinite(x) || !Number.isFinite(y)) return null;
    const limit = (value: number) => Math.min(PLACE_LIMIT, Math.max(-PLACE_LIMIT, value));
    const place: PdfTranslationPlace = { x: limit(x), y: limit(y) };
    if (above) place.above = true;
    if (Math.abs(place.x) < 0.0005 && Math.abs(place.y) < 0.0005 && !place.above) return null;
    return place;
}

export function rememberPdfTranslationPlace(id: string, place: PdfTranslationPlace | null): void {
    if (!id) return;
    if (!place) draggedPlace.delete(id);
    else draggedPlace.set(id, place);
}

export function pdfTranslationPlace(id: string): PdfTranslationPlace | null {
    if (!id) return null;
    return draggedPlace.get(id) ?? null;
}

function emitPdfTranslations(key: string) {
    if (!key) return;
    for (const listener of [...listeners]) listener(key);
}

export function subscribePdfTranslations(listener: (key: string) => void): () => void {
    listeners.add(listener);
    return () => {
        listeners.delete(listener);
    };
}

// A reader's own annotation. The companion window listens and pushes the
// project to shares this document hosts; received annotations never announce.
export const PDF_ANNOTATIONS_CHANGED_EVENT = "maclaw:companion-pdf-annotations-changed";

export function notifyPdfAnnotationsChanged(path: string): void {
    if (!path) return;
    try {
        window.dispatchEvent(new CustomEvent(PDF_ANNOTATIONS_CHANGED_EVENT, { detail: { path } }));
    } catch {
        // A non-browser host has no event bus.
    }
}

function oneSize(value: unknown): PdfTranslationSize | null {
    if (!value || typeof value !== "object") return null;
    const raw = value as { w?: unknown; h?: unknown };
    const w = Number(raw.w);
    const h = Number(raw.h);
    if (!Number.isFinite(w) || !Number.isFinite(h) || w < 0.02 || h < 0.02) return null;
    return { w: Math.min(1, w), h: Math.min(1, h) };
}

function clipChars(text: string, limit: number): string {
    const chars = Array.from(text);
    if (chars.length <= limit) return text;
    return chars.slice(0, limit).join("");
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
    if (x + w <= 1 && y + h <= 1) return { x, y, w, h };
    const right = Math.min(1, x + w);
    const bottom = Math.min(1, y + h);
    if (right - x < 0.001 || bottom - y < 0.001) return null;
    return { x, y, w: right - x, h: bottom - y };
}

function oneTranslation(value: unknown): PdfTranslation | null {
    if (!value || typeof value !== "object") return null;
    const raw = value as { id?: unknown; text?: unknown; translation?: unknown; parts?: unknown; clipped?: unknown; place?: unknown; size?: unknown; kind?: unknown; shared?: unknown };
    const id = String(raw.id || "").trim().slice(0, 80);
    const text = clipChars(String(raw.text || "").replace(/\s+/g, " ").trim(), MAX_SOURCE);
    const translation = clipChars(String(raw.translation || "").trim(), MAX_TRANSLATION);
    if (!id || !text || !translation || !Array.isArray(raw.parts)) return null;
    const parts: PdfTranslationPart[] = [];
    for (const item of raw.parts) {
        if (parts.length >= MAX_PARTS) break;
        if (!item || typeof item !== "object") continue;
        const part = item as { page?: unknown; rects?: unknown };
        const page = Math.floor(Number(part.page));
        if (!Number.isFinite(page) || page < 1 || page > 100000 || !Array.isArray(part.rects)) continue;
        const rects: PdfMarkRect[] = [];
        for (const rect of part.rects) {
            if (rects.length >= MAX_RECTS) break;
            const next = oneRect(rect);
            if (next) rects.push(next);
        }
        if (rects.length > 0) parts.push({ page, rects });
    }
    if (parts.length === 0) return null;
    const place = onePlace(raw.place);
    const size = oneSize(raw.size);
    const kept: PdfTranslation = { id, text, translation, parts, ...(place ? { place } : {}), ...(size ? { size } : {}) };
    if (raw.kind === "annotation") kept.kind = "annotation";
    if (raw.shared === true) kept.shared = true;
    return raw.clipped === true ? { ...kept, clipped: true } : kept;
}

function onePlace(value: unknown): PdfTranslationPlace | null {
    if (!value || typeof value !== "object") return null;
    const raw = value as { x?: unknown; y?: unknown; above?: unknown };
    return clampPdfTranslationPlace(Number(raw.x), Number(raw.y), raw.above === true);
}

function normalizePdfTranslations(value: unknown): PdfTranslation[] {
    if (!Array.isArray(value)) return [];
    const list: PdfTranslation[] = [];
    for (const item of value) {
        const next = oneTranslation(item);
        if (next) list.push(next);
    }
    // The translation just written is at the end. A full store keeps that one.
    if (list.length <= MAX_TRANSLATIONS) return list;
    return list.slice(list.length - MAX_TRANSLATIONS);
}

export function loadPdfTranslations(path: string): PdfTranslation[] {
    const key = pdfMarkPathKey(path);
    if (!key) return [];
    const held = fallback.get(key);
    if (held) return held;
    return normalizePdfTranslations(store[key]);
}

// Only the host's annotations travel in a share project. Translations are
// generated answers for this reader and stay local.
export function loadPdfAnnotations(path: string): PdfTranslation[] {
    return loadPdfTranslations(path).filter((item) => item.kind === "annotation");
}

export function savePdfTranslations(path: string, value: unknown): PdfTranslation[] {
    const key = pdfMarkPathKey(path);
    const list = normalizePdfTranslations(value);
    if (!key) return list;
    fallback.delete(key);
    if (list.length === 0) delete store[key];
    else store[key] = list;
    saveRevision += 1;
    savePdfTranslationsToHost(key, list);
    emitPdfTranslations(key);
    return list;
}

export function loadPdfTranslationDrafts(path: string): PdfTranslationDraft[] {
    const key = pdfMarkPathKey(path);
    if (!key) return [];
    return (flight.get(key) ?? []).slice();
}

export function pdfTranslationError(path: string): string {
    const key = pdfMarkPathKey(path);
    if (!key) return "";
    return flightError.get(key) ?? "";
}

export function beginPdfTranslation(path: string, draft: PdfTranslationDraft): void {
    const key = pdfMarkPathKey(path);
    if (!key || !draft.id) return;
    const list = (flight.get(key) ?? []).filter((item) => item.id !== draft.id);
    list.push(draft);
    flight.set(key, list);
    flightError.delete(key);
    emitPdfTranslations(key);
}

function forgetPdfTranslationDraft(path: string, id: string): string {
    const key = pdfMarkPathKey(path);
    if (!key) return "";
    const list = (flight.get(key) ?? []).filter((item) => item.id !== id);
    if (list.length === 0) flight.delete(key);
    else flight.set(key, list);
    return key;
}

export function finishPdfTranslation(path: string, id: string): void {
    const key = forgetPdfTranslationDraft(path, id);
    if (key) emitPdfTranslations(key);
}

export function failPdfTranslation(path: string, id: string, message: string): void {
    rememberPdfTranslationPlace(id, null);
    const key = forgetPdfTranslationDraft(path, id);
    if (!key) return;
    flightError.set(key, message.trim() || "翻译失败，请稍后重试");
    emitPdfTranslations(key);
}

export function resetPdfTranslationFlight(): void {
    flight.clear();
    flightError.clear();
    draggedPlace.clear();
}

export function newPdfTranslationId(): string {
    const rand = Math.random().toString(36).slice(2, 8);
    return `t_${Date.now().toString(36)}_${rand}`;
}
