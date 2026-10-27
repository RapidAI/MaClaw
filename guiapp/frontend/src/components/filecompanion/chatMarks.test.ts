// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import { hydrateChatMarks, loadChatMarks, normalizeChatMarks, saveChatMarks } from "./chatMarks";
import { pdfMarkPathKey } from "./pdfMarks";

const mark = { message: 1, color: "yellow" as const, start: 3, end: 8, text: "lo wo" };

beforeEach(() => {
    hydrateChatMarks({});
    delete (window as unknown as { go?: unknown }).go;
});

describe("chat marks", () => {
    it("keeps marks per document path", () => {
        saveChatMarks("C:\\Docs\\Paper.PDF", [mark]);
        expect(loadChatMarks("c:/docs/paper.pdf")).toEqual([mark]);
        expect(loadChatMarks("C:/docs/other.pdf")).toEqual([]);
        expect(pdfMarkPathKey("C:\\Docs\\Paper.PDF")).toBe("c:/docs/paper.pdf");
    });

    it("drops a range that is not one of the four colours", () => {
        saveChatMarks("/tmp/a.md", [{ ...mark, color: "red" }]);
        expect(loadChatMarks("/tmp/a.md")).toEqual([]);
    });

    it("drops an empty or inverted range", () => {
        saveChatMarks("/tmp/a.md", [
            { ...mark, start: 8, end: 8 },
            { ...mark, start: 9, end: 4 },
            { ...mark, text: "  " },
        ]);
        expect(loadChatMarks("/tmp/a.md")).toEqual([]);
    });

    it("keeps the raw text so a re-rendered answer can re-anchor", () => {
        const text = "line one\nline two";
        saveChatMarks("/tmp/a.md", [{ ...mark, start: 0, end: text.length, text }]);
        expect(loadChatMarks("/tmp/a.md")[0].text).toBe(text);
    });

    it("keeps the newest marks when a document is at the cap", () => {
        const many = Array.from({ length: 2001 }, (_value, index) => ({ ...mark, start: index, end: index + 1, text: "x" }));
        saveChatMarks("/tmp/cap.md", many);
        const kept = loadChatMarks("/tmp/cap.md");
        expect(kept).toHaveLength(2000);
        // The oldest mark fell off; the one just painted stayed.
        expect(kept[0].start).toBe(1);
        expect(kept[kept.length - 1].start).toBe(2000);
    });

    it("returns null for a value that is not a list", () => {
        expect(normalizeChatMarks({ message: 1 })).toBeNull();
        expect(normalizeChatMarks([])).toEqual([]);
    });

    it("reads a started mark back after hydration", () => {
        hydrateChatMarks({ "/tmp/b.md": [{ ...mark, color: "blue" }] });
        expect(loadChatMarks("/tmp/b.md")).toEqual([{ ...mark, color: "blue" }]);
    });

    it("writes to the host and keeps the marks on a failed write", async () => {
        const calls: string[] = [];
        (window as unknown as { go: unknown }).go = {
            main: {
                App: {
                    SaveCompanionChatMarks: async (_path: string, body: string) => {
                        calls.push(body);
                        throw new Error("offline");
                    },
                },
            },
        };
        saveChatMarks("/tmp/fail.md", [mark]);
        await new Promise((resolve) => setTimeout(resolve, 0));
        expect(calls).toEqual([JSON.stringify([mark])]);
        // The failed upload must not make the highlight disappear on screen.
        expect(loadChatMarks("/tmp/fail.md")).toEqual([mark]);
    });
});
