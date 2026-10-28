import { chatMarksSaveRevision, hydrateChatMarks } from "./chatMarks";
import { FILE_COMPANION_INPUT_HISTORY_KEY, fileCompanionInputHistorySaveRevision, hydrateFileCompanionInputHistory } from "./fileCompanionInputHistory";
import { hydratePdfDrawings, pdfDrawingsSaveRevision } from "./pdfDrawings";
import { hydratePdfMarks, PDF_MARK_STORAGE_KEY, pdfMarksSaveRevision } from "./pdfMarks";
import { hydratePdfTranslations, PDF_TRANSLATION_STORAGE_KEY, pdfTranslationsSaveRevision } from "./pdfTranslations";

// The companion reader used to keep its highlights and typed-question history
// in the WebView localStorage. That origin can be cleared with site data, so
// both now live in the desktop SQLite database. This module reads the old
// browser documents once, hands them to the host to import, and removes them
// only after the host reported success. Everything else in the app can keep
// reading the synchronous mirrors the two modules maintain.

type ReaderStateBinding = (...args: unknown[]) => Promise<unknown>;

// The app may paint before a slow host read returns (main.tsx bounds the wait
// at 2s). A reader mounted before that hand-off subscribes here and re-reads
// the mirrors when the state finally arrives.
const hydratedListeners = new Set<() => void>();

export function subscribeCompanionReaderState(listener: () => void): () => void {
    hydratedListeners.add(listener);
    return () => {
        hydratedListeners.delete(listener);
    };
}

function notifyCompanionReaderHydrated(): void {
    for (const listener of [...hydratedListeners]) listener();
}

function readerStateBinding(name: string): ReaderStateBinding | null {
    const app = (window as unknown as { go?: { main?: { App?: Record<string, unknown> } } }).go?.main?.App;
    const binding = app?.[name];
    return typeof binding === "function" ? binding as ReaderStateBinding : null;
}

function readLegacy(key: string): string {
    try {
        return window.localStorage.getItem(key) || "";
    } catch {
        return "";
    }
}

// The host import skips a browser document larger than its 8MB value bound.
const LEGACY_DOCUMENT_MAX_CHARS = 8 << 20;

// legacyDocument keeps the host import strict: a corrupt or oversized browser
// document is skipped instead of failing the whole read, which would also
// block the state already stored in SQLite.
function legacyDocument(raw: string, expectArray = false): string {
    if (!raw || raw.length > LEGACY_DOCUMENT_MAX_CHARS) return "";
    try {
        const value = JSON.parse(raw);
        if (expectArray) return Array.isArray(value) ? raw : "";
        return value && typeof value === "object" && !Array.isArray(value) ? raw : "";
    } catch {
        return "";
    }
}

function dropLegacy(key: string, value: string): void {
    if (!value) return;
    try {
        window.localStorage.removeItem(key);
    } catch {
        // The host already owns the data; a storage error only leaves a stale key.
    }
}

// hydrateCompanionReaderState loads the reader state from SQLite and fills the
// synchronous mirrors. A missing binding (web preview, older desktop) leaves
// the mirrors empty instead of failing startup.
export async function hydrateCompanionReaderState(): Promise<void> {
    const load = readerStateBinding("LoadCompanionReaderState");
    if (!load) return;
    const legacyMarks = readLegacy(PDF_MARK_STORAGE_KEY);
    const legacyTranslations = readLegacy(PDF_TRANSLATION_STORAGE_KEY);
    const legacyHistory = readLegacy(FILE_COMPANION_INPUT_HISTORY_KEY);
    // Capture the save counters before awaiting: a highlight or a typed
    // question saved while this read is in flight is newer than its snapshot.
    const marksRevision = pdfMarksSaveRevision();
    const translationsRevision = pdfTranslationsSaveRevision();
    const drawingsRevision = pdfDrawingsSaveRevision();
    const chatMarksRevision = chatMarksSaveRevision();
    const historyRevision = fileCompanionInputHistorySaveRevision();
    const raw = await load(
        legacyDocument(legacyMarks),
        legacyDocument(legacyTranslations),
        legacyDocument(legacyHistory, true),
    );
    let payload: unknown;
    try {
        payload = JSON.parse(String(raw ?? "{}"));
    } catch {
        return;
    }
    const record = payload && typeof payload === "object" ? payload as Record<string, unknown> : {};
    if (pdfMarksSaveRevision() === marksRevision) hydratePdfMarks(record.pdf_marks);
    if (pdfTranslationsSaveRevision() === translationsRevision) hydratePdfTranslations(record.pdf_translations);
    if (pdfDrawingsSaveRevision() === drawingsRevision) hydratePdfDrawings(record.pdf_drawings);
    if (chatMarksSaveRevision() === chatMarksRevision) hydrateChatMarks(record.chat_marks);
    if (fileCompanionInputHistorySaveRevision() === historyRevision) hydrateFileCompanionInputHistory(record.input_history);
    // Only now is the browser copy redundant.
    dropLegacy(PDF_MARK_STORAGE_KEY, legacyMarks);
    dropLegacy(PDF_TRANSLATION_STORAGE_KEY, legacyTranslations);
    dropLegacy(FILE_COMPANION_INPUT_HISTORY_KEY, legacyHistory);
    notifyCompanionReaderHydrated();
}
