// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { hydrateCompanionReaderState, subscribeCompanionReaderState } from "./companionReaderState";
import { chatMarksSaveRevision, hydrateChatMarks, loadChatMarks, saveChatMarks } from "./chatMarks";
import { fileCompanionInputHistory, FILE_COMPANION_INPUT_HISTORY_KEY, hydrateFileCompanionInputHistory } from "./fileCompanionInputHistory";
import { hydratePdfMarks, loadPdfMarks, PDF_MARK_STORAGE_KEY, pdfMarksSaveRevision, savePdfMarks } from "./pdfMarks";
import { hydratePdfTranslations, loadPdfTranslations, PDF_TRANSLATION_STORAGE_KEY } from "./pdfTranslations";

const loadState = vi.fn();

function installHostBindings(): void {
    (window as unknown as { go: unknown }).go = {
        main: {
            App: {
                LoadCompanionReaderState: (...args: unknown[]) => loadState(...args),
            },
        },
    };
}

beforeEach(() => {
    localStorage.clear();
    loadState.mockReset();
    hydratePdfMarks({});
    hydratePdfTranslations({});
    hydrateChatMarks({});
    hydrateFileCompanionInputHistory([]);
    installHostBindings();
});

describe("companion reader state migration", () => {
    it("imports the legacy browser documents and hydrates every mirror", async () => {
        localStorage.setItem(PDF_MARK_STORAGE_KEY, JSON.stringify({
            "c:/docs/paper.pdf": [{
                id: "m1",
                page: 1,
                color: "green",
                rects: [{ x: 0.1, y: 0.1, w: 0.2, h: 0.03 }],
                text: "Hello",
            }],
        }));
        localStorage.setItem(PDF_TRANSLATION_STORAGE_KEY, JSON.stringify({
            "c:/docs/paper.pdf": [{
                id: "t1",
                text: "Hello",
                translation: "你好",
                parts: [{ page: 1, rects: [{ x: 0.1, y: 0.1, w: 0.2, h: 0.03 }] }],
            }],
        }));
        localStorage.setItem(FILE_COMPANION_INPUT_HISTORY_KEY, JSON.stringify(["总结这一页"]));
        loadState.mockResolvedValue(JSON.stringify({
            pdf_marks: {
                "c:/docs/paper.pdf": [{
                    id: "m1",
                    page: 1,
                    color: "green",
                    rects: [{ x: 0.1, y: 0.1, w: 0.2, h: 0.03 }],
                    text: "Hello",
                }],
            },
            pdf_translations: {
                "c:/docs/paper.pdf": [{
                    id: "t1",
                    text: "Hello",
                    translation: "你好",
                    parts: [{ page: 1, rects: [{ x: 0.1, y: 0.1, w: 0.2, h: 0.03 }] }],
                }],
            },
            input_history: ["总结这一页"],
        }));

        await hydrateCompanionReaderState();

        expect(loadState).toHaveBeenCalledWith(
            JSON.stringify({
                "c:/docs/paper.pdf": [{
                    id: "m1",
                    page: 1,
                    color: "green",
                    rects: [{ x: 0.1, y: 0.1, w: 0.2, h: 0.03 }],
                    text: "Hello",
                }],
            }),
            JSON.stringify({
                "c:/docs/paper.pdf": [{
                    id: "t1",
                    text: "Hello",
                    translation: "你好",
                    parts: [{ page: 1, rects: [{ x: 0.1, y: 0.1, w: 0.2, h: 0.03 }] }],
                }],
            }),
            JSON.stringify(["总结这一页"]),
        );
        expect(loadPdfMarks("C:/docs/Paper.PDF").some((mark) => mark.id === "m1")).toBe(true);
        expect(loadPdfTranslations("C:/docs/Paper.PDF").some((item) => item.translation === "你好")).toBe(true);
        expect(fileCompanionInputHistory()).toEqual(["总结这一页"]);
        // The host owns the data now, so the browser keys are gone.
        expect(localStorage.getItem(PDF_MARK_STORAGE_KEY)).toBeNull();
        expect(localStorage.getItem(PDF_TRANSLATION_STORAGE_KEY)).toBeNull();
        expect(localStorage.getItem(FILE_COMPANION_INPUT_HISTORY_KEY)).toBeNull();
    });

    it("skips a corrupt legacy document but still hydrates the stored state", async () => {
        localStorage.setItem(PDF_MARK_STORAGE_KEY, "{not json");
        localStorage.setItem(FILE_COMPANION_INPUT_HISTORY_KEY, JSON.stringify({ not: "a list" }));
        loadState.mockResolvedValue(JSON.stringify({
            pdf_marks: {
                "c:/docs/paper.pdf": [{
                    id: "m1",
                    page: 1,
                    color: "green",
                    rects: [{ x: 0.1, y: 0.1, w: 0.2, h: 0.03 }],
                    text: "Hello",
                }],
            },
            pdf_translations: {},
            input_history: [],
        }));

        await hydrateCompanionReaderState();

        expect(loadState).toHaveBeenCalledWith("", "", "");
        expect(loadPdfMarks("C:/docs/Paper.PDF").some((mark) => mark.id === "m1")).toBe(true);
        expect(localStorage.getItem(PDF_MARK_STORAGE_KEY)).toBeNull();
        expect(localStorage.getItem(FILE_COMPANION_INPUT_HISTORY_KEY)).toBeNull();
    });

    it("keeps a highlight saved while the startup read was in flight", async () => {
        let release: (value: string) => void = () => undefined;
        loadState.mockImplementation(() => new Promise<string>((resolve) => {
            release = resolve;
        }));
        const hydrating = hydrateCompanionReaderState();
        const before = pdfMarksSaveRevision();
        savePdfMarks("C:/docs/new.pdf", [{
            id: "race",
            page: 2,
            color: "blue",
            rects: [{ x: 0.2, y: 0.2, w: 0.1, h: 0.02 }],
            text: "Typed now",
        }]);
        expect(pdfMarksSaveRevision()).toBeGreaterThan(before);

        release(JSON.stringify({ pdf_marks: {}, pdf_translations: {}, input_history: [] }));
        await hydrating;

        expect(loadPdfMarks("C:/docs/new.pdf").some((mark) => mark.id === "race")).toBe(true);
    });

    it("still hydrates the mirrors that were not written during the read", async () => {
        let release: (value: string) => void = () => undefined;
        loadState.mockImplementation(() => new Promise<string>((resolve) => {
            release = resolve;
        }));
        const hydrating = hydrateCompanionReaderState();
        savePdfMarks("C:/docs/new.pdf", [{
            id: "race",
            page: 2,
            color: "blue",
            rects: [{ x: 0.2, y: 0.2, w: 0.1, h: 0.02 }],
            text: "Typed now",
        }]);

        release(JSON.stringify({
            pdf_marks: {},
            pdf_translations: {
                "c:/docs/paper.pdf": [{
                    id: "t1",
                    text: "Hello",
                    translation: "你好",
                    parts: [{ page: 1, rects: [{ x: 0.1, y: 0.1, w: 0.2, h: 0.03 }] }],
                }],
            },
            input_history: ["上一问"],
        }));
        await hydrating;

        expect(loadPdfMarks("C:/docs/new.pdf").some((mark) => mark.id === "race")).toBe(true);
        expect(loadPdfTranslations("C:/docs/Paper.PDF")).toHaveLength(1);
        expect(fileCompanionInputHistory()).toEqual(["上一问"]);
    });

    it("hydrates the highlights painted on the agent's answers", async () => {
        loadState.mockResolvedValue(JSON.stringify({
            pdf_marks: {},
            pdf_translations: {},
            chat_marks: {
                "c:/docs/paper.pdf": [{ message: 1, color: "green", start: 3, end: 8, text: "lo wo" }],
            },
            input_history: [],
        }));

        await hydrateCompanionReaderState();

        expect(loadChatMarks("C:/docs/Paper.PDF")).toEqual([{ message: 1, color: "green", start: 3, end: 8, text: "lo wo" }]);
    });

    it("keeps a chat highlight saved while the startup read was in flight", async () => {
        let release: (value: string) => void = () => undefined;
        loadState.mockImplementation(() => new Promise<string>((resolve) => {
            release = resolve;
        }));
        const hydrating = hydrateCompanionReaderState();
        const before = chatMarksSaveRevision();
        saveChatMarks("C:/docs/new.pdf", [{ message: 1, color: "pink", start: 0, end: 5, text: "Hello" }]);
        expect(chatMarksSaveRevision()).toBeGreaterThan(before);

        release(JSON.stringify({ pdf_marks: {}, pdf_translations: {}, chat_marks: {}, input_history: [] }));
        await hydrating;

        expect(loadChatMarks("C:/docs/new.pdf")).toEqual([{ message: 1, color: "pink", start: 0, end: 5, text: "Hello" }]);
    });

    it("leaves the mirrors empty and keeps the browser keys when the host is absent", async () => {
        localStorage.setItem(PDF_MARK_STORAGE_KEY, JSON.stringify({
            "c:/docs/paper.pdf": [{
                id: "m1",
                page: 1,
                color: "green",
                rects: [{ x: 0.1, y: 0.1, w: 0.2, h: 0.03 }],
                text: "Hello",
            }],
        }));
        delete (window as unknown as { go?: unknown }).go;

        await hydrateCompanionReaderState();

        expect(loadPdfMarks("C:/docs/Paper.PDF")).toHaveLength(0);
        expect(localStorage.getItem(PDF_MARK_STORAGE_KEY)).not.toBeNull();
    });

    it("notifies mounted readers when the state arrives", async () => {
        const listener = vi.fn();
        const unsubscribe = subscribeCompanionReaderState(listener);
        loadState.mockResolvedValue(JSON.stringify({ pdf_marks: {}, pdf_translations: {}, input_history: [] }));

        try {
            await hydrateCompanionReaderState();
        } finally {
            unsubscribe();
        }

        expect(listener).toHaveBeenCalledTimes(1);
    });

    it("does not notify when the host read fails", async () => {
        const listener = vi.fn();
        const unsubscribe = subscribeCompanionReaderState(listener);
        loadState.mockRejectedValue(new Error("database is locked"));

        try {
            await expect(hydrateCompanionReaderState()).rejects.toThrow("database is locked");
        } finally {
            unsubscribe();
        }

        expect(listener).not.toHaveBeenCalled();
    });

    it("keeps the browser keys when the host import fails", async () => {
        localStorage.setItem(FILE_COMPANION_INPUT_HISTORY_KEY, JSON.stringify(["总结这一页"]));
        loadState.mockRejectedValue(new Error("database is locked"));

        await expect(hydrateCompanionReaderState()).rejects.toThrow("database is locked");

        expect(fileCompanionInputHistory()).toEqual([]);
        expect(localStorage.getItem(FILE_COMPANION_INPUT_HISTORY_KEY)).not.toBeNull();
    });
});
