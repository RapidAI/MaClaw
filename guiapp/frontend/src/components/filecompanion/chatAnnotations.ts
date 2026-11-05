// Companion chat annotations live beside the transcript, the same way a PDF
// note lives beside a passage. An answer is rendered Markdown, so a note is
// stored as a character range inside one message, never as markup inside the
// answer. The synchronous mirror keeps the chat panel rendering without
// awaiting a binding; hydrateChatAnnotations fills it once at startup from
// LoadCompanionReaderState, and SaveCompanionChatAnnotations is the durable
// copy in the desktop SQLite database.

import { enqueueCompanionReaderWrite } from "./companionReaderWriteQueue";
import { CHAT_MARK_TEXT_MAX } from "./chatMarks";
import { pdfMarkPathKey } from "./pdfMarks";
import {
    capPdfAnnotationDraft,
    clipPdfAnnotationBody,
    clipPdfAnnotationName,
} from "./pdfTranslations";

export { capPdfAnnotationDraft, clipPdfAnnotationBody, clipPdfAnnotationName };

export type ChatAnnotationPlace = { x: number; y: number; above?: boolean };

// A resized card's outer size, as a fraction of the message bubble. Fractions
// keep the box proportional when the column changes width.
export type ChatAnnotationSize = { w: number; h: number };

export type ChatAnnotation = {
    id: string;
    /** Index of the message in the tab transcript. */
    message: number;
    /** Character offsets into the message's rendered text. */
    start: number;
    end: number;
    /** The text that was selected, so a re-rendered answer can re-anchor. */
    text: string;
    /** The reader's own opinion. This is the card body. */
    body: string;
    /** Short label. Defaults to the start of the quote. */
    name?: string;
    place?: ChatAnnotationPlace;
    size?: ChatAnnotationSize;
    // shared marks a note that came from the host's project. A later host
    // sync replaces exactly these; a reader's own note is kept local.
    shared?: boolean;
};

const MAX_NOTES = 200;
const PLACE_LIMIT = 4;

const fallback = new Map<string, ChatAnnotation[]>();

let store: Record<string, unknown> = {};
let saveRevision = 0;

export function hydrateChatAnnotations(value: unknown): void {
    store = value && typeof value === "object" && !Array.isArray(value)
        ? { ...(value as Record<string, unknown>) }
        : {};
    fallback.clear();
}

export function chatAnnotationsSaveRevision(): number {
    return saveRevision;
}

const writeSeq = new Map<string, number>();

function saveChatAnnotationsToHost(key: string, notes: ChatAnnotation[]): void {
    const app = (window as unknown as { go?: { main?: { App?: Record<string, unknown> } } }).go?.main?.App;
    const save = app?.SaveCompanionChatAnnotations;
    if (typeof save !== "function") return;
    const seq = (writeSeq.get(key) ?? 0) + 1;
    writeSeq.set(key, seq);
    const body = JSON.stringify(notes);
    enqueueCompanionReaderWrite(async () => {
        try {
            await (save as (path: string, body: string) => Promise<unknown>)(key, body);
        } catch {
            if (writeSeq.get(key) === seq) fallback.set(key, notes);
        }
    });
}

export function clampChatAnnotationPlace(x: number, y: number, above?: boolean): ChatAnnotationPlace | null {
    if (!Number.isFinite(x) || !Number.isFinite(y)) return null;
    const limit = (value: number) => Math.min(PLACE_LIMIT, Math.max(-PLACE_LIMIT, value));
    const place: ChatAnnotationPlace = { x: limit(x), y: limit(y) };
    if (above) place.above = true;
    // A card parked on the quote has no offset. above still has to be kept,
    // or a card that opened over the answer jumps back under it on the next sync.
    if (Math.abs(place.x) < 0.0005 && Math.abs(place.y) < 0.0005 && !place.above) return null;
    return place;
}

export type ChatAnnotationLine = { left: number; top: number; width: number; height: number };

type ChatAnnotationRawBox = { left: number; top: number; right: number; bottom: number };

// A bold word, a link, and the plain text around it are separate rects on one
// line. They belong in one box. A picture is much taller than the line and
// stays its own box, so it cannot swallow the lines beside it.
function chatAnnotationSameLine(a: ChatAnnotationRawBox, b: ChatAnnotationRawBox): boolean {
    const ah = a.bottom - a.top;
    const bh = b.bottom - b.top;
    const shorter = Math.min(ah, bh);
    const taller = Math.max(ah, bh);
    if (taller > shorter * 2) return false;
    const overlap = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
    return overlap >= shorter * 0.5;
}

function chatAnnotationUnion(boxes: ChatAnnotationRawBox[]): ChatAnnotationRawBox {
    let left = boxes[0].left;
    let right = boxes[0].right;
    let top = boxes[0].top;
    let bottom = boxes[0].bottom;
    for (let index = 1; index < boxes.length; index += 1) {
        const box = boxes[index];
        left = Math.min(left, box.left);
        right = Math.max(right, box.right);
        top = Math.min(top, box.top);
        bottom = Math.max(bottom, box.bottom);
    }
    return { left, top, right, bottom };
}

// Range.getClientRects includes one box per line fragment and, on some
// engines, a box around all of them. The outer box is what made a wrapped
// sentence look like one tall rectangle. A box that swallows two others is
// that outer box. Fragments of one line are then joined.
export function chatAnnotationLineBoxes(
    rects: ReadonlyArray<{ left: number; top: number; right: number; bottom: number }>,
): ChatAnnotationLine[] {
    const boxes = rects.filter((rect) => rect.right - rect.left > 0.5 && rect.bottom - rect.top > 0.5);
    const kept = boxes.filter((box) => {
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
    const source = (kept.length > 0 ? kept : boxes).slice().sort((a, b) => a.top - b.top || a.left - b.left);
    const groups: ChatAnnotationRawBox[][] = [];
    for (const box of source) {
        const group = groups.length > 0 ? groups[groups.length - 1] : undefined;
        if (!group || !group.some((item) => chatAnnotationSameLine(item, box))) {
            groups.push([box]);
            continue;
        }
        group.push(box);
    }
    return groups.map((group) => {
        const box = chatAnnotationUnion(group);
        return {
            left: box.left,
            top: box.top,
            width: box.right - box.left,
            height: box.bottom - box.top,
        };
    });
}

// A code block or a formula scrolls inside the answer. The dashed box is
// positioned on the bubble, so the part that has slid out of that frame must
// not keep covering the lines below it.
export function clipChatAnnotationLine(
    line: { left: number; top: number; right: number; bottom: number },
    frames: ReadonlyArray<{ left: number; top: number; right: number; bottom: number }>,
): { left: number; top: number; right: number; bottom: number } | null {
    let left = line.left;
    let top = line.top;
    let right = line.right;
    let bottom = line.bottom;
    for (const frame of frames) {
        left = Math.max(left, frame.left);
        top = Math.max(top, frame.top);
        right = Math.min(right, frame.right);
        bottom = Math.min(bottom, frame.bottom);
        if (right - left <= 0.5 || bottom - top <= 0.5) return null;
    }
    return { left, top, right, bottom };
}

// Same nodes the highlight walker ignores. A button or the note card can sit
// inside the DOM range without being part of the stored quote.
const CHAT_ANNOTATION_SKIP = "button, svg, script, style, noscript, template, [data-chat-mark-skip]";

type ChatAnnotationViewportBox = { left: number; top: number; right: number; bottom: number };

function chatAnnotationScrollsInside(style: CSSStyleDeclaration): boolean {
    const clips = (value: string) => value === "auto" || value === "scroll" || value === "hidden" || value === "clip";
    return clips(style.overflowX) || clips(style.overflowY);
}

function chatAnnotationPositiveBox(box: ChatAnnotationViewportBox): boolean {
    return box.right - box.left > 0.5 && box.bottom - box.top > 0.5;
}

function chatAnnotationClientRects(range: Range): ChatAnnotationViewportBox[] {
    const rects: ChatAnnotationViewportBox[] = [];
    if (typeof range.getClientRects === "function") {
        for (const rect of Array.from(range.getClientRects())) {
            rects.push({ left: rect.left, top: rect.top, right: rect.right, bottom: rect.bottom });
        }
    }
    if (rects.length === 0 && typeof range.getBoundingClientRect === "function") {
        const quote = range.getBoundingClientRect();
        rects.push({ left: quote.left, top: quote.top, right: quote.right, bottom: quote.bottom });
    }
    return rects;
}

function chatAnnotationClipFrame(element: HTMLElement): ChatAnnotationViewportBox | null {
    if (!chatAnnotationScrollsInside(window.getComputedStyle(element))) return null;
    const box = element.getBoundingClientRect();
    if (box.width <= 0.5 || box.height <= 0.5) return null;
    return { left: box.left, top: box.top, right: box.right, bottom: box.bottom };
}

// A quote that starts in the paragraph and runs into a code block has its
// common ancestor above that block. Cutting every line to that ancestor's
// frames misses the scroller, so the code line paints across the chat. Each
// fragment is cut to the scrollers it actually sits in. geometry is false
// when the range produced no box, so a card can still show where layout APIs
// are missing. A box that was produced and then cut away has left its block.
export function chatAnnotationRangeLineRects(host: HTMLElement, range: Range): { rects: ChatAnnotationViewportBox[]; geometry: boolean } {
    const rects: ChatAnnotationViewportBox[] = [];
    let geometry = false;
    const frameCache = new Map<HTMLElement, ChatAnnotationViewportBox[]>();
    const framesFor = (node: Node): ChatAnnotationViewportBox[] => {
        const start = node instanceof HTMLElement ? node : node.parentElement;
        if (!start || start === host || !host.contains(start)) return [];
        const cached = frameCache.get(start);
        if (cached) return cached;
        const own = chatAnnotationClipFrame(start);
        const higher = start.parentElement ? framesFor(start.parentElement) : [];
        const frames = own ? [own, ...higher] : higher;
        frameCache.set(start, frames);
        return frames;
    };
    const absorb = (target: Range, node: Node) => {
        const frames = framesFor(node);
        for (const rect of chatAnnotationClientRects(target)) {
            if (!chatAnnotationPositiveBox(rect)) continue;
            geometry = true;
            const clipped = clipChatAnnotationLine(rect, frames);
            if (clipped) rects.push(clipped);
        }
    };
    const absorbText = (text: Text) => {
        if (!text.data || text.parentElement?.closest(CHAT_ANNOTATION_SKIP)) return;
        let start = 0;
        let end = text.length;
        if (text === range.startContainer) start = Math.min(range.startOffset, text.length);
        if (text === range.endContainer) end = Math.min(range.endOffset, text.length);
        if (end <= start) return;
        const sub = document.createRange();
        try {
            sub.setStart(text, start);
            sub.setEnd(text, end);
        } catch {
            return;
        }
        absorb(sub, text);
    };
    const root = range.commonAncestorContainer;
    if (!root) return { rects, geometry };
    if (root.nodeType === Node.TEXT_NODE) {
        absorbText(root as Text);
        return { rects, geometry };
    }
    if (typeof document.createTreeWalker !== "function" || typeof range.intersectsNode !== "function") {
        absorb(range, root);
        return { rects, geometry };
    }
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT, {
        acceptNode(node) {
            try {
                if (!range.intersectsNode(node)) return NodeFilter.FILTER_REJECT;
            } catch {
                return NodeFilter.FILTER_REJECT;
            }
            if (node instanceof HTMLElement) {
                if (node.matches(CHAT_ANNOTATION_SKIP)) return NodeFilter.FILTER_REJECT;
                if (node.matches("img, video, canvas")) return NodeFilter.FILTER_ACCEPT;
                return NodeFilter.FILTER_SKIP;
            }
            const parent = (node as Text).parentElement;
            if (!parent || parent.closest(CHAT_ANNOTATION_SKIP) || !(node as Text).data) return NodeFilter.FILTER_REJECT;
            return NodeFilter.FILTER_ACCEPT;
        },
    });
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        if (node instanceof HTMLElement) {
            const sub = document.createRange();
            try {
                sub.selectNode(node);
            } catch {
                continue;
            }
            absorb(sub, node);
            continue;
        }
        absorbText(node as Text);
    }
    return { rects, geometry };
}

// The chat log keeps the newest line in view, so nudging scrollTop is pulled
// back. A card that does not fit under the quote opens above it. The gaps are
// the pop's top offset and the above rule's bottom offset.
export function chatAnnotationOpensAbove(
    spanTop: number,
    spanBottom: number,
    viewTop: number,
    viewBottom: number,
    popHeight: number,
): boolean {
    if (!(popHeight >= 1) || !(viewBottom - viewTop >= 1)) return false;
    const fitsBelow = spanBottom + 6 + popHeight <= viewBottom + 1;
    const fitsAbove = spanTop - 12 - popHeight >= viewTop - 1;
    return !fitsBelow && fitsAbove;
}

function oneSize(value: unknown): ChatAnnotationSize | null {
    if (!value || typeof value !== "object") return null;
    const raw = value as { w?: unknown; h?: unknown };
    const w = Number(raw.w);
    const h = Number(raw.h);
    if (!Number.isFinite(w) || !Number.isFinite(h) || w < 0.02 || h < 0.02) return null;
    return { w: Math.min(1, w), h: Math.min(1, h) };
}

function oneAnnotation(value: unknown): ChatAnnotation | null {
    if (!value || typeof value !== "object") return null;
    const raw = value as {
        id?: unknown; message?: unknown; start?: unknown; end?: unknown;
        text?: unknown; body?: unknown; name?: unknown; place?: unknown; size?: unknown; shared?: unknown;
    };
    const id = String(raw.id || "").trim().slice(0, 80);
    const message = Math.floor(Number(raw.message));
    if (!id || !Number.isFinite(message) || message < 0 || message > 100000) return null;
    const start = Math.floor(Number(raw.start));
    const end = Math.floor(Number(raw.end));
    if (!Number.isFinite(start) || !Number.isFinite(end) || start < 0 || end <= start) return null;
    const text = String(raw.text ?? "").trim().slice(0, CHAT_MARK_TEXT_MAX);
    const body = clipPdfAnnotationBody(String(raw.body ?? ""));
    if (!text || !body) return null;
    const name = clipPdfAnnotationName(String(raw.name ?? ""));
    const place = raw.place && typeof raw.place === "object"
        ? clampChatAnnotationPlace(
            Number((raw.place as { x?: unknown }).x),
            Number((raw.place as { y?: unknown }).y),
            (raw.place as { above?: unknown }).above === true,
        )
        : null;
    const size = oneSize(raw.size);
    const note: ChatAnnotation = {
        id,
        message,
        start,
        end: start + text.length,
        text,
        body,
        ...(name ? { name } : {}),
        ...(place ? { place } : {}),
        ...(size ? { size } : {}),
    };
    if (raw.shared === true) note.shared = true;
    return note;
}

/** Returns null when the value is not a list. An empty list is a real import. */
export function normalizeChatAnnotations(value: unknown): ChatAnnotation[] | null {
    if (!Array.isArray(value)) return null;
    const notes: ChatAnnotation[] = [];
    for (const item of value) {
        const note = oneAnnotation(item);
        if (note) notes.push(note);
    }
    return notes.length > MAX_NOTES ? notes.slice(notes.length - MAX_NOTES) : notes;
}

// A share update replaces the host's notes and leaves this reader's own.
// Incoming rows are the host's copy, so they are marked shared.
export function mergeSharedChatAnnotations(current: ChatAnnotation[], incoming: unknown): ChatAnnotation[] {
    const parsed = normalizeChatAnnotations(incoming) ?? [];
    const shared = parsed.map((item) => ({ ...item, shared: true as const }));
    const kept = current.filter((item) => item.shared !== true);
    return normalizeChatAnnotations([...kept, ...shared]) ?? [];
}

export function loadChatAnnotations(path: string): ChatAnnotation[] {
    const key = pdfMarkPathKey(path);
    if (!key) return [];
    const held = fallback.get(key);
    if (held) return held;
    return normalizeChatAnnotations(store[key]) ?? [];
}

// What a share upload carries. The shared flag is a local receipt of the
// host's copy; the project stores the note itself and the receiver marks it.
export function chatAnnotationsForShare(path: string): ChatAnnotation[] {
    return loadChatAnnotations(path).map((item) => {
        const { shared: _shared, ...rest } = item;
        return rest;
    });
}

export function newChatAnnotationId(): string {
    const rand = Math.random().toString(36).slice(2, 8);
    return `ca_${Date.now().toString(36)}_${rand}`;
}

// A reader's own note on an answer. The companion window listens and pushes
// the project to the shares this document hosts; applying a received share
// must not announce, or two readers' copies would echo updates.
export const CHAT_ANNOTATIONS_CHANGED_EVENT = "maclaw:companion-chat-annotations-changed";

export function saveChatAnnotations(path: string, value: unknown, announce = false): ChatAnnotation[] {
    const key = pdfMarkPathKey(path);
    const notes = normalizeChatAnnotations(value) ?? [];
    if (!key) return notes;
    fallback.delete(key);
    if (notes.length === 0) delete store[key];
    else store[key] = notes;
    saveRevision += 1;
    saveChatAnnotationsToHost(key, notes);
    if (announce) {
        try {
            window.dispatchEvent(new CustomEvent(CHAT_ANNOTATIONS_CHANGED_EVENT, { detail: { path } }));
        } catch {
            // A non-browser host has no event bus; the host write still ran.
        }
    }
    return notes;
}
