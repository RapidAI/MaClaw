// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
    hydratePdfDrawings,
    loadPdfDrawings,
    PDF_DRAWINGS_CHANGED_EVENT,
    pdfDrawingHitsRect,
    savePdfDrawings,
    subscribePdfDrawings,
} from "./pdfDrawings";

const line = {
    id: "d1",
    page: 1,
    color: "yellow" as const,
    tool: "line" as const,
    points: [{ x: 0.1, y: 0.2 }, { x: 0.3, y: 0.4 }],
};

beforeEach(() => {
    hydratePdfDrawings({});
    delete (window as unknown as { go?: unknown }).go;
});

describe("pdf drawings", () => {
    it("keeps drawings for the same path and leaves another path empty", () => {
        savePdfDrawings("C:\\Docs\\Paper.PDF", [line]);
        expect(loadPdfDrawings("c:/docs/paper.pdf")).toEqual([line]);
        expect(loadPdfDrawings("C:/docs/other.pdf")).toEqual([]);
    });

    it("drops a colour, tool, or point that cannot be drawn", () => {
        savePdfDrawings("/tmp/a.pdf", [
            { ...line, id: "bad-color", color: "red" as "yellow" },
            { ...line, id: "bad-tool", tool: "circle" as "line" },
            { ...line, id: "off-page", points: [{ x: -0.1, y: 0.2 }, { x: 0.3, y: 0.4 }] },
            { ...line, id: "one-point", points: [{ x: 0.1, y: 0.2 }] },
            { ...line, id: "tiny-box", tool: "box" as const, points: [{ x: 0.1, y: 0.2 }, { x: 0.1005, y: 0.2005 }] },
            line,
        ]);
        const saved = loadPdfDrawings("/tmp/a.pdf");
        expect(saved).toHaveLength(1);
        expect(saved[0].id).toBe("d1");
    });

    it("keeps only the two corners of a box and remembers a host drawing", () => {
        savePdfDrawings("/tmp/box.pdf", [{
            id: "d2",
            page: 2,
            color: "blue",
            tool: "box",
            points: [{ x: 0.1, y: 0.1 }, { x: 0.4, y: 0.3 }, { x: 0.6, y: 0.5 }],
            shared: true,
        }]);
        expect(loadPdfDrawings("/tmp/box.pdf")).toEqual([{
            id: "d2",
            page: 2,
            color: "blue",
            tool: "box",
            points: [{ x: 0.1, y: 0.1 }, { x: 0.6, y: 0.5 }],
            shared: true,
        }]);
    });

    it("keeps the newest drawing when a document already holds the limit", () => {
        const many = Array.from({ length: 205 }, (_, index) => ({
            ...line,
            id: `d${index}`,
            points: [{ x: 0.1, y: 0.2 }, { x: 0.2 + index * 0.001, y: 0.3 }],
        }));
        savePdfDrawings("/tmp/full.pdf", many);
        const saved = loadPdfDrawings("/tmp/full.pdf");
        expect(saved).toHaveLength(200);
        expect(saved[0].id).toBe("d5");
        expect(saved[saved.length - 1].id).toBe("d204");
    });

    it("writes drawings through the host binding", async () => {
        const calls: Array<[string, string]> = [];
        (window as unknown as { go: unknown }).go = {
            main: {
                App: {
                    SaveCompanionPdfDrawings: async (path: string, body: string) => {
                        calls.push([path, body]);
                    },
                },
            },
        };
        try {
            savePdfDrawings("C:\\Docs\\Paper.PDF", [line]);
            await new Promise((resolve) => setTimeout(resolve, 0));
            expect(calls).toEqual([["c:/docs/paper.pdf", JSON.stringify([line])]]);
        } finally {
            delete (window as unknown as { go?: unknown }).go;
        }
    });

    it("hits a line only where the marquee crosses it", () => {
        const diagonal = { ...line, points: [{ x: 0.1, y: 0.1 }, { x: 0.9, y: 0.9 }] };
        // The bounding box of the diagonal covers this area, but the ink does not.
        expect(pdfDrawingHitsRect(diagonal, { x: 0.4, y: 0.05, w: 0.1, h: 0.1 })).toBe(false);
        expect(pdfDrawingHitsRect(diagonal, { x: 0.35, y: 0.35, w: 0.1, h: 0.1 })).toBe(true);
        const frame = { ...line, tool: "box" as const, points: [{ x: 0.6, y: 0.6 }, { x: 0.8, y: 0.8 }] };
        expect(pdfDrawingHitsRect(frame, { x: 0.5, y: 0.5, w: 0.2, h: 0.2 })).toBe(true);
        expect(pdfDrawingHitsRect(frame, { x: 0.1, y: 0.1, w: 0.1, h: 0.1 })).toBe(false);
    });

    it("tells a mounted reader about local saves and announces only on request", () => {
        const seen = vi.fn();
        const announce = vi.fn();
        const stop = subscribePdfDrawings(seen);
        window.addEventListener(PDF_DRAWINGS_CHANGED_EVENT, announce);
        try {
            savePdfDrawings("/tmp/live.pdf", [line]);
            expect(seen).toHaveBeenCalledWith("/tmp/live.pdf");
            expect(announce).not.toHaveBeenCalled();
            savePdfDrawings("/tmp/live.pdf", [line], true);
            expect(announce).toHaveBeenCalledTimes(1);
        } finally {
            stop();
            window.removeEventListener(PDF_DRAWINGS_CHANGED_EVENT, announce);
        }
    });
});
