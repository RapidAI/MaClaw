// Companion marks on a rendered document body. A markdown or office extract
// is one scrollable page, not a transcript, so its highlights and placed
// notes keep DOC_MARK_MESSAGE as their message index inside the same
// chatMarks/chatAnnotations lists the chat uses. One list means the reader
// state file, the share payload, and the share-apply path carry document
// marks unchanged; only the paint side filters on the message index.
// Bookmarks reuse the pdfBookmarks store for the same reason: a file is one
// kind, so an .md path never owns page bookmarks. A document bookmark keeps
// page 1 and stores its anchor in y, a fraction of the rendered text length.

import {
    CHAT_ANNOTATIONS_CHANGED_EVENT,
    loadChatAnnotations,
    saveChatAnnotations,
    type ChatAnnotation,
} from "./chatAnnotations";
import {
    DOC_MARK_MESSAGE,
    loadChatMarks,
    saveChatMarks,
    type ChatMark,
} from "./chatMarks";
import {
    loadPdfBookmarks,
    newPdfBookmarkId,
    savePdfBookmarks,
    type PdfBookmark,
    type PdfBookmarkColor,
} from "./pdfBookmarks";
import {
    loadPdfDrawings,
    savePdfDrawings,
    type PdfDrawing,
} from "./pdfDrawings";

export { CHAT_ANNOTATIONS_CHANGED_EVENT, DOC_MARK_MESSAGE };

/** Highlights painted on the document body, in stored order. */
export function loadDocMarks(path: string): ChatMark[] {
    return loadChatMarks(path).filter((mark) => mark.message === DOC_MARK_MESSAGE);
}

/** Replaces only this path's document marks. Transcript marks on the same
 *  path stay untouched. announce mirrors saveChatMarks: true means "the
 *  reader did this", which is what the share uploader listens for. */
export function saveDocMarks(path: string, marks: ChatMark[], announce = false): ChatMark[] {
    const kept = loadChatMarks(path).filter((mark) => mark.message !== DOC_MARK_MESSAGE);
    const body = marks.map((mark) => ({ ...mark, message: DOC_MARK_MESSAGE }));
    const saved = saveChatMarks(path, [...kept, ...body], announce);
    return saved.filter((mark) => mark.message === DOC_MARK_MESSAGE);
}

/** Notes placed on the document body. */
export function loadDocAnnotations(path: string): ChatAnnotation[] {
    return loadChatAnnotations(path).filter((note) => note.message === DOC_MARK_MESSAGE);
}

export function saveDocAnnotations(path: string, notes: ChatAnnotation[], announce = false): ChatAnnotation[] {
    const kept = loadChatAnnotations(path).filter((note) => note.message !== DOC_MARK_MESSAGE);
    const body = notes.map((note) => ({ ...note, message: DOC_MARK_MESSAGE }));
    const saved = saveChatAnnotations(path, [...kept, ...body], announce);
    return saved.filter((note) => note.message === DOC_MARK_MESSAGE);
}

// The document is "one page": the bookmark's page stays 1 and its anchor is
// y, the character offset as a fraction of the rendered text length. x is
// unused but kept a valid fraction for the shared cleaner.
export const DOC_BOOKMARK_PAGE = 1;
export const DOC_BOOKMARK_X = 0;

export function loadDocBookmarks(path: string): PdfBookmark[] {
    return loadPdfBookmarks(path);
}

export function saveDocBookmarks(path: string, bookmarks: PdfBookmark[], announce = false): PdfBookmark[] {
    return savePdfBookmarks(path, bookmarks, announce);
}

export function newDocBookmarkId(): string {
    return newPdfBookmarkId();
}

// Freehand strokes on the document body share the pdfDrawings store: the file
// is one kind, so a .md path never owns real PDF page drawings. The body is
// "one page", page 1, with points normalized to the whole scrolled box.
export const DOC_DRAWING_PAGE = 1;

export function loadDocDrawings(path: string): PdfDrawing[] {
    return loadPdfDrawings(path).filter((drawing) => drawing.page === DOC_DRAWING_PAGE);
}

/** Replaces only this path's page-1 drawings. announce mirrors savePdfDrawings:
 *  true means "the reader did this", which is what the share uploader listens
 *  for. */
export function saveDocDrawings(path: string, drawings: PdfDrawing[], announce = false): PdfDrawing[] {
    const kept = loadPdfDrawings(path).filter((drawing) => drawing.page !== DOC_DRAWING_PAGE);
    const body = drawings.map((drawing) => ({ ...drawing, page: DOC_DRAWING_PAGE }));
    const saved = savePdfDrawings(path, [...kept, ...body], announce);
    return saved.filter((drawing) => drawing.page === DOC_DRAWING_PAGE);
}

/** Bookmark anchor as a character offset into the document's rendered text. */
export function docBookmarkOffset(bookmark: PdfBookmark, textLength: number): number {
    if (!(textLength > 0)) return 0;
    const y = Number(bookmark.y);
    if (!Number.isFinite(y)) return 0;
    return Math.max(0, Math.min(textLength, Math.round(y * textLength)));
}

export function newDocBookmark(name: string, color: PdfBookmarkColor, fraction: number): PdfBookmark {
    const y = Number.isFinite(fraction) ? Math.max(0, Math.min(1, fraction)) : 0;
    return {
        id: newDocBookmarkId(),
        page: DOC_BOOKMARK_PAGE,
        x: DOC_BOOKMARK_X,
        y,
        color,
        name: Array.from(name.replace(/\s+/g, " ").trim()).slice(0, 40).join(""),
    };
}
