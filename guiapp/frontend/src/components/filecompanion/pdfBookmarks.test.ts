// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
    companionProjectMarksJSON,
    hydratePdfBookmarks,
    loadPdfBookmarks,
    normalizePdfBookmarks,
    PDF_BOOKMARKS_CHANGED_EVENT,
    pdfBookmarkLayoutMoved,
    pdfBookmarkScrollIsReader,
    pdfBookmarkScrollTop,
    pdfBookmarkTickLanes,
    pdfBookmarkTickTop,
    savePdfBookmarks,
    subscribePdfBookmarks,
} from "./pdfBookmarks";
import { hydratePdfMarks } from "./pdfMarks";

const place = {
    id: "b1",
    page: 2,
    x: 0.2,
    y: 0.4,
    color: "teal" as const,
    name: "先加",
};

beforeEach(() => {
    hydratePdfBookmarks({});
    hydratePdfMarks({});
    delete (window as unknown as { go?: unknown }).go;
});

describe("pdf bookmarks", () => {
    it("keeps addition order and drops a place that cannot be drawn", () => {
        const saved = savePdfBookmarks("/tmp/a.pdf", [
            { ...place, name: "  先   加  " },
            { ...place, id: "bad-color", color: "navy" },
            { ...place, id: "off-page", y: 1.2 },
            { ...place, id: "blank", name: "   " },
            { ...place, id: "x".repeat(81), name: "太长" },
            { ...place, id: "b2", page: 7, x: 0.4, y: 0.8, color: "orange", name: "后加" },
            { ...place, id: "long-name", name: "名".repeat(41), color: "purple" },
        ]);
        expect(saved.map((item) => item.id)).toEqual(["b1", "b2", "long-name"]);
        expect(saved[0].name).toBe("先 加");
        expect(saved[1]).toMatchObject({ page: 7, color: "orange", name: "后加" });
        expect(saved[2].name).toBe("名".repeat(40));
        expect(loadPdfBookmarks("C:/tmp/other.pdf")).toEqual([]);
        expect(normalizePdfBookmarks({ not: "a list" })).toBeNull();
    });

    it("keeps one row when the same bookmark id is repeated", () => {
        const saved = savePdfBookmarks("/tmp/a.pdf", [
            place,
            { ...place, page: 9, x: 0.3, y: 0.6, color: "blue", name: "改过" },
        ]);
        expect(saved).toEqual([{ ...place, page: 9, x: 0.3, y: 0.6, color: "blue", name: "改过" }]);
    });

    it("steps edge ticks apart and ignores a scroll that did not move the page", () => {
        expect(pdfBookmarkTickLanes([{ y: 0.5 }, { y: 0.51 }, { y: 0.9 }], 400)).toEqual([0, 1, 0]);
        // 328–368 and the shifted 360–400 overlap, so the lower ribbon steps aside.
        expect(pdfBookmarkTickLanes([{ y: 0.82 }, { y: 0.96 }], 400)).toEqual([0, 1]);
        expect(pdfBookmarkTickLanes([{ y: 0.7 }, { y: 0.96 }], 400)).toEqual([0, 0]);
        expect(pdfBookmarkTickLanes([{ y: 0.95 }, { y: 0.99 }], 400)).toEqual([0, 1]);
        expect(pdfBookmarkTickTop(0.5, 400)).toBe(200);
        expect(pdfBookmarkTickTop(0.95, 400)).toBe(360);
        expect(pdfBookmarkTickTop(1, 30)).toBe(0);
        expect(pdfBookmarkTickTop(0.2, 0)).toBe(0);
        expect(pdfBookmarkTickTop(Number.NaN, 400)).toBe(0);
        const placeBox = { pageTop: 900, pageHeight: 400, scrollTop: 0 };
        expect(pdfBookmarkLayoutMoved(null, placeBox)).toBe(true);
        expect(pdfBookmarkLayoutMoved(placeBox, placeBox)).toBe(false);
        expect(pdfBookmarkLayoutMoved(placeBox, { pageTop: 900, pageHeight: 400, scrollTop: 868 })).toBe(false);
        expect(pdfBookmarkLayoutMoved(placeBox, { pageTop: 1768, pageHeight: 400, scrollTop: 868 })).toBe(false);
        expect(pdfBookmarkLayoutMoved(placeBox, { pageTop: 900, pageHeight: 800, scrollTop: 0 })).toBe(true);
        expect(pdfBookmarkLayoutMoved(placeBox, { pageTop: 1000, pageHeight: 400, scrollTop: 0 })).toBe(true);
        expect(pdfBookmarkScrollTop(900, 400, 0.1)).toBe(868);
        expect(pdfBookmarkScrollTop(900, 800, 0.1)).toBe(908);
        expect(pdfBookmarkScrollTop(0, 100, 0)).toBe(0);
        const placed = { pageTop: 900, pageHeight: 400, scrollTop: 868 };
        expect(pdfBookmarkScrollIsReader(null, placed)).toBe(false);
        expect(pdfBookmarkScrollIsReader(placed, placed)).toBe(false);
        expect(pdfBookmarkScrollIsReader(placed, { pageTop: 900, pageHeight: 400, scrollTop: 200 })).toBe(true);
        expect(pdfBookmarkScrollIsReader(placed, { pageTop: 900, pageHeight: 800, scrollTop: 200 })).toBe(true);
        expect(pdfBookmarkScrollIsReader(placed, { pageTop: 1100, pageHeight: 400, scrollTop: 1068 })).toBe(false);
        expect(pdfBookmarkScrollIsReader(placed, { pageTop: 1100, pageHeight: 800, scrollTop: 868 })).toBe(false);
    });

    it("keeps the newest bookmarks when a document is past the cap", () => {
        const many = Array.from({ length: 205 }, (_, index) => ({
            ...place,
            id: `b${index}`,
            name: `第${index}`,
        }));
        const saved = savePdfBookmarks("/tmp/full.pdf", many);
        expect(saved).toHaveLength(200);
        expect(saved[0].id).toBe("b5");
        expect(saved[saved.length - 1].id).toBe("b204");

        const huge = Array.from({ length: 2001 }, (_, index) => ({
            ...place,
            id: `b${index}`,
            name: `第${index}`,
        }));
        const tail = savePdfBookmarks("/tmp/huge.pdf", huge);
        expect(tail).toHaveLength(200);
        expect(tail[0].id).toBe("b1801");
        expect(tail[tail.length - 1].id).toBe("b2000");

        const buried = savePdfBookmarks("/tmp/buried.pdf", [
            { ...place, id: "keep", name: "留下" },
            ...Array.from({ length: 2000 }, (_, index) => ({ ...place, id: `bad${index}`, color: "navy", name: "丢掉" })),
        ]);
        expect(buried.map((item) => item.id)).toEqual(["keep"]);
    });

    it("writes bookmarks through the host binding and announces only on request", async () => {
        const calls: Array<[string, string]> = [];
        (window as unknown as { go: unknown }).go = {
            main: {
                App: {
                    SaveCompanionPdfBookmarks: async (path: string, body: string) => {
                        calls.push([path, body]);
                    },
                },
            },
        };
        const seen = vi.fn();
        const announce = vi.fn();
        const stop = subscribePdfBookmarks(seen);
        window.addEventListener(PDF_BOOKMARKS_CHANGED_EVENT, announce);
        try {
            savePdfBookmarks("C:\\Docs\\Paper.PDF", [place]);
            expect(seen).toHaveBeenCalledWith("c:/docs/paper.pdf");
            expect(announce).not.toHaveBeenCalled();
            expect(loadPdfBookmarks("c:/docs/paper.pdf")).toEqual([place]);
            savePdfBookmarks("C:\\Docs\\Paper.PDF", [place], true);
            expect(announce).toHaveBeenCalledTimes(1);
            await new Promise((resolve) => setTimeout(resolve, 0));
            expect(calls[0]).toEqual(["c:/docs/paper.pdf", JSON.stringify([place])]);
            savePdfBookmarks("C:\\Docs\\Paper.PDF", []);
            expect(loadPdfBookmarks("c:/docs/paper.pdf")).toEqual([]);
        } finally {
            stop();
            window.removeEventListener(PDF_BOOKMARKS_CHANGED_EVENT, announce);
            delete (window as unknown as { go?: unknown }).go;
        }
    });

    it("packs bookmarks beside the other reader marks", () => {
        savePdfBookmarks("C:/docs/paper.pdf", [place]);
        expect(JSON.parse(companionProjectMarksJSON("C:/docs/paper.pdf"))).toEqual({
            highlights: [],
            chatMarks: [],
            annotations: [],
            drawings: [],
            bookmarks: [place],
        });
    });
});
