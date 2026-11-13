// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import { hydrateChatAnnotations, loadChatAnnotations, saveChatAnnotations } from "./chatAnnotations";
import { DOC_MARK_MESSAGE, hydrateChatMarks, loadChatMarks, saveChatMarks } from "./chatMarks";
import { hydratePdfBookmarks, loadPdfBookmarks } from "./pdfBookmarks";
import {
    docBookmarkOffset,
    loadDocAnnotations,
    loadDocBookmarks,
    loadDocDrawings,
    loadDocMarks,
    newDocBookmark,
    saveDocAnnotations,
    saveDocBookmarks,
    saveDocDrawings,
    saveDocMarks,
} from "./docMarks";
import { hydratePdfDrawings, loadPdfDrawings, savePdfDrawings } from "./pdfDrawings";

const docMark = { message: DOC_MARK_MESSAGE, color: "yellow" as const, start: 3, end: 8, text: "lo wo" };
const chatMark = { message: 2, color: "green" as const, start: 0, end: 5, text: "Hello" };

const docNote = {
    id: "n1",
    message: DOC_MARK_MESSAGE,
    start: 3,
    end: 8,
    text: "lo wo",
    body: "文档上的批注",
};
const chatNote = {
    id: "n2",
    message: 1,
    start: 0,
    end: 5,
    text: "Hello",
    body: "回答上的批注",
};

beforeEach(() => {
    hydrateChatMarks({});
    hydrateChatAnnotations({});
    hydratePdfBookmarks({});
    hydratePdfDrawings({});
    delete (window as unknown as { go?: unknown }).go;
});

describe("document marks", () => {
    it("stores body marks beside transcript marks on the same path", () => {
        saveChatMarks("/tmp/a.md", [chatMark]);
        saveDocMarks("/tmp/a.md", [docMark]);
        expect(loadChatMarks("/tmp/a.md")).toEqual([chatMark, docMark]);
        expect(loadDocMarks("/tmp/a.md")).toEqual([docMark]);
    });

    it("replaces only the document marks and keeps the transcript's", () => {
        saveChatMarks("/tmp/a.md", [chatMark, { ...docMark, text: "older" }]);
        saveDocMarks("/tmp/a.md", [{ ...docMark, color: "blue" }]);
        const all = loadChatMarks("/tmp/a.md");
        expect(all).toHaveLength(2);
        expect(all[0]).toEqual(chatMark);
        expect(all[1]).toMatchObject({ message: DOC_MARK_MESSAGE, color: "blue" });
    });

    it("pins whatever it is given to the document slot", () => {
        saveDocMarks("/tmp/a.md", [{ ...docMark, message: 4 }]);
        expect(loadDocMarks("/tmp/a.md")[0].message).toBe(DOC_MARK_MESSAGE);
    });

    it("keeps the document slot through the shared normalizer", () => {
        saveChatMarks("/tmp/a.md", [{ ...docMark, message: -2 }, docMark]);
        expect(loadChatMarks("/tmp/a.md")).toEqual([docMark]);
    });
});

describe("document annotations", () => {
    it("stores body notes beside transcript notes on the same path", () => {
        saveChatAnnotations("/tmp/a.md", [chatNote]);
        saveDocAnnotations("/tmp/a.md", [docNote]);
        expect(loadChatAnnotations("/tmp/a.md")).toHaveLength(2);
        expect(loadDocAnnotations("/tmp/a.md")).toEqual([
            expect.objectContaining({ id: "n1", message: DOC_MARK_MESSAGE }),
        ]);
    });

    it("replaces only the document notes and keeps the transcript's", () => {
        saveChatAnnotations("/tmp/a.md", [chatNote, { ...docNote, id: "gone" }]);
        saveDocAnnotations("/tmp/a.md", [{ ...docNote, body: "new" }]);
        const all = loadChatAnnotations("/tmp/a.md");
        expect(all.map((note) => note.id).sort()).toEqual(["n1", "n2"]);
        expect(loadDocAnnotations("/tmp/a.md")[0].body).toBe("new");
    });
});

describe("document bookmarks", () => {
    it("stores bookmarks on the pdfBookmarks store for the path", () => {
        saveDocBookmarks("/tmp/a.md", [newDocBookmark("第一章", "teal", 0.5)]);
        expect(loadDocBookmarks("/tmp/a.md")).toHaveLength(1);
        expect(loadPdfBookmarks("/tmp/a.md")).toHaveLength(1);
        expect(loadDocBookmarks("/tmp/a.md")[0]).toMatchObject({ page: 1, y: 0.5, color: "teal", name: "第一章" });
    });

    it("normalizes the bookmark fields for the shared cleaner", () => {
        const bookmark = newDocBookmark("  many   spaces  ", "red", 1.4);
        expect(bookmark.name).toBe("many spaces");
        expect(bookmark.y).toBe(1);
        expect(bookmark.x).toBe(0);
        expect(bookmark.page).toBe(1);
    });

    it("maps the stored fraction back to a character offset", () => {
        const bookmark = { ...newDocBookmark("here", "blue", 0.5) };
        expect(docBookmarkOffset(bookmark, 200)).toBe(100);
        expect(docBookmarkOffset(bookmark, 0)).toBe(0);
        expect(docBookmarkOffset({ ...bookmark, y: 2 }, 200)).toBe(200);
    });
});

describe("document drawings", () => {
    it("pins doc drawings to page 1 and leaves other pages alone", () => {
        const line = { id: "d1", page: 4, color: "yellow" as const, tool: "line" as const, points: [{ x: 0, y: 0 }, { x: 1, y: 1 }] };
        savePdfDrawings("/tmp/d.md", [line]);
        const box = { id: "d2", page: 9, color: "blue" as const, tool: "box" as const, points: [{ x: 0.1, y: 0.1 }, { x: 0.4, y: 0.4 }] };
        saveDocDrawings("/tmp/d.md", [box]);
        const all = loadPdfDrawings("/tmp/d.md");
        expect(all.find((drawing) => drawing.id === "d1")?.page).toBe(4);
        expect(all.find((drawing) => drawing.id === "d2")?.page).toBe(1);
        expect(loadDocDrawings("/tmp/d.md")).toHaveLength(1);
    });
});
