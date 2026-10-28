// Companion chat highlights live beside the transcript. An answer is rendered
// Markdown, so a highlight is stored as a character range inside one message
// of the conversation, never as markup inside the answer text. The synchronous
// mirror keeps the chat panel rendering without awaiting a binding;
// hydrateChatMarks fills it once at startup from LoadCompanionReaderState, and
// SaveCompanionChatMarks is the durable copy in the desktop SQLite database.

import { enqueueCompanionReaderWrite } from "./companionReaderWriteQueue";
import { pdfMarkPathKey } from "./pdfMarks";

export const CHAT_MARK_COLORS = [
    { id: "yellow", label: "黄色", fill: "#ffe08a" },
    { id: "green", label: "绿色", fill: "#b6e7c3" },
    { id: "blue", label: "蓝色", fill: "#b9d4f5" },
    { id: "pink", label: "粉色", fill: "#f6c1d8" },
] as const;

export type ChatMarkColor = (typeof CHAT_MARK_COLORS)[number]["id"];

export type ChatMark = {
    /** Index of the message in the tab transcript. */
    message: number;
    color: ChatMarkColor;
    /** Character offsets into the message's rendered text. */
    start: number;
    end: number;
    /** The text that was selected, so a re-rendered answer can re-anchor. */
    text: string;
};

const MAX_MARKS = 2000;
// The quote is also what re-anchors a mark, so a range longer than this is
// stored as its first CHAT_MARK_TEXT_MAX characters on both ends.
export const CHAT_MARK_TEXT_MAX = 2000;

const fallback = new Map<string, ChatMark[]>();

// Synchronous mirror of the SQLite store, keyed by pdfMarkPathKey.
let store: Record<string, unknown> = {};
// Counts saves so a startup read that began before a save cannot overwrite the
// newer in-memory state with its older snapshot.
let saveRevision = 0;

export function hydrateChatMarks(value: unknown): void {
    store = value && typeof value === "object" && !Array.isArray(value)
        ? { ...(value as Record<string, unknown>) }
        : {};
    fallback.clear();
}

export function chatMarksSaveRevision(): number {
    return saveRevision;
}

const writeSeq = new Map<string, number>();

function saveChatMarksToHost(key: string, marks: ChatMark[]): void {
    const app = (window as unknown as { go?: { main?: { App?: Record<string, unknown> } } }).go?.main?.App;
    const save = app?.SaveCompanionChatMarks;
    if (typeof save !== "function") return;
    const seq = (writeSeq.get(key) ?? 0) + 1;
    writeSeq.set(key, seq);
    const body = JSON.stringify(marks);
    enqueueCompanionReaderWrite(async () => {
        try {
            await (save as (path: string, body: string) => Promise<unknown>)(key, body);
        } catch {
            // Keep the marks for this session so the chat still shows them.
            // Only the newest failed write may supply the fallback.
            if (writeSeq.get(key) === seq) fallback.set(key, marks);
        }
    });
}

function oneColor(value: unknown): ChatMarkColor | null {
    for (const color of CHAT_MARK_COLORS) {
        if (value === color.id) return color.id;
    }
    return null;
}

function oneMark(value: unknown): ChatMark | null {
    if (!value || typeof value !== "object") return null;
    const raw = value as { message?: unknown; color?: unknown; start?: unknown; end?: unknown; text?: unknown };
    const message = Math.floor(Number(raw.message));
    if (!Number.isFinite(message) || message < 0 || message > 100000) return null;
    const color = oneColor(raw.color);
    if (!color) return null;
    const start = Math.floor(Number(raw.start));
    const end = Math.floor(Number(raw.end));
    if (!Number.isFinite(start) || !Number.isFinite(end) || start < 0 || end <= start) return null;
    const text = String(raw.text ?? "").trim().slice(0, CHAT_MARK_TEXT_MAX);
    if (!text) return null;
    return { message, color, start, end, text };
}

/** Returns null when the value is not a list. An empty list is a real import. */
export function normalizeChatMarks(value: unknown): ChatMark[] | null {
    const list = Array.isArray(value) ? value : null;
    if (!list) return null;
    const marks: ChatMark[] = [];
    for (const item of list) {
        const mark = oneMark(item);
        if (mark) marks.push(mark);
    }
    // The accumulator appends, so the tail is the newest work. Overflowing the
    // cap must drop the oldest marks, not the ones just painted.
    return marks.length > MAX_MARKS ? marks.slice(marks.length - MAX_MARKS) : marks;
}

export function loadChatMarks(path: string): ChatMark[] {
    const key = pdfMarkPathKey(path);
    if (!key) return [];
    const held = fallback.get(key);
    if (held) return held;
    return normalizeChatMarks(store[key]) ?? [];
}

// A reader's own highlight on an answer. The companion window listens and
// pushes the project to the shares this document hosts; applying a received
// share must not announce, or two readers' copies would echo updates.
export const CHAT_MARKS_CHANGED_EVENT = "maclaw:companion-chat-marks-changed";

export function saveChatMarks(path: string, value: unknown, announce = false): ChatMark[] {
    const key = pdfMarkPathKey(path);
    const marks = normalizeChatMarks(value) ?? [];
    if (!key) return marks;
    fallback.delete(key);
    if (marks.length === 0) delete store[key];
    else store[key] = marks;
    saveRevision += 1;
    saveChatMarksToHost(key, marks);
    if (announce) {
        try {
            window.dispatchEvent(new CustomEvent(CHAT_MARKS_CHANGED_EVENT, { detail: { path } }));
        } catch {
            // A non-browser host has no event bus; the host write still ran.
        }
    }
    return marks;
}
