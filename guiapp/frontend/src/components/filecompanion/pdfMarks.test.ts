// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import { hydratePdfMarks, loadPdfMarkCounts, loadPdfMarks, pdfMarkPathKey, savePdfMarks } from "./pdfMarks";

const mark = {
    id: "m1",
    page: 1,
    color: "yellow" as const,
    rects: [{ x: 0.1, y: 0.2, w: 0.3, h: 0.04 }],
    text: "Hello",
};

beforeEach(() => {
    hydratePdfMarks({});
    delete (window as unknown as { go?: unknown }).go;
});

describe("pdf marks", () => {
    it("keeps marks for the same path and leaves another path empty", () => {
        savePdfMarks("C:\\Docs\\Paper.PDF", [mark]);
        expect(loadPdfMarks("c:/docs/paper.pdf")).toEqual([mark]);
        expect(loadPdfMarks("C:/docs/other.pdf")).toEqual([]);
        expect(pdfMarkPathKey("C:\\Docs\\Paper.PDF")).toBe("c:/docs/paper.pdf");
    });

    it("drops a mark that is not one of the four colours", () => {
        savePdfMarks("/tmp/a.pdf", [{ ...mark, color: "red" as "yellow" }]);
        expect(loadPdfMarks("/tmp/a.pdf")).toEqual([]);
    });

    it("counts marks for many paths in one pass", () => {
        savePdfMarks("C:\\Docs\\Paper.PDF", [mark, { ...mark, id: "m2" }]);
        savePdfMarks("/tmp/b.pdf", [mark]);
        const counts = loadPdfMarkCounts([
            "c:/docs/paper.pdf",
            "/tmp/b.pdf",
            "/tmp/none.pdf",
            "",
        ]);
        expect(counts.get("c:/docs/paper.pdf")).toBe(2);
        expect(counts.get("/tmp/b.pdf")).toBe(1);
        // A path with no marks has no entry, and a blank path is skipped.
        expect(counts.has("/tmp/none.pdf")).toBe(true);
        expect(counts.get("/tmp/none.pdf")).toBe(0);
        expect(counts.has("")).toBe(false);
    });

    it("reads the same counts as loadPdfMarks per path", () => {
        savePdfMarks("/tmp/c.pdf", [mark]);
        const counts = loadPdfMarkCounts(["/tmp/c.pdf", "/tmp/d.pdf"]);
        expect(counts.get("/tmp/c.pdf")).toBe(loadPdfMarks("/tmp/c.pdf").length);
        expect(counts.get("/tmp/d.pdf")).toBe(loadPdfMarks("/tmp/d.pdf").length);
    });

    it("writes to the host in the order the marks were saved", async () => {
        const calls: string[] = [];
        let release: () => void = () => undefined;
        const gate = new Promise<void>((resolve) => {
            release = resolve;
        });
        (window as unknown as { go: unknown }).go = {
            main: {
                App: {
                    SaveCompanionPdfMarks: async (_path: string, body: string) => {
                        calls.push(body);
                        if (calls.length === 1) await gate;
                    },
                },
            },
        };
        try {
            savePdfMarks("/tmp/order.pdf", [mark]);
            savePdfMarks("/tmp/order.pdf", []);
            await new Promise((resolve) => setTimeout(resolve, 0));
            // The clear must not overtake the save it follows on disk.
            expect(calls).toEqual([JSON.stringify([mark])]);

            release();
            await new Promise((resolve) => setTimeout(resolve, 0));
            expect(calls).toEqual([JSON.stringify([mark]), "[]"]);
        } finally {
            delete (window as unknown as { go?: unknown }).go;
        }
    });
});
