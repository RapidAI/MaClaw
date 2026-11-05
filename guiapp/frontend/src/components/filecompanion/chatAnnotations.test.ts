// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import {
    chatAnnotationLineBoxes,
    chatAnnotationRangeLineRects,
    clipChatAnnotationLine,
    chatAnnotationOpensAbove,
    chatAnnotationsForShare,
    hydrateChatAnnotations,
    loadChatAnnotations,
    mergeSharedChatAnnotations,
    normalizeChatAnnotations,
    saveChatAnnotations,
    type ChatAnnotation,
} from "./chatAnnotations";

const note: ChatAnnotation = {
    id: "ca_1",
    message: 1,
    start: 0,
    end: 2,
    text: "方法",
    body: "这里要复核",
    name: "复核",
};

beforeEach(() => {
    hydrateChatAnnotations({});
    delete (window as unknown as { go?: unknown }).go;
});

describe("chat annotations", () => {
    it("keeps notes per document path and drops a note without a body", () => {
        saveChatAnnotations("C:\\Docs\\Paper.PDF", [note, { ...note, id: "ca_blank", body: "  " }]);
        expect(loadChatAnnotations("c:/docs/paper.pdf")).toEqual([note]);
        expect(loadChatAnnotations("C:/docs/other.pdf")).toEqual([]);
    });

    it("returns null for a value that is not a list", () => {
        expect(normalizeChatAnnotations({ id: "ca_1" })).toBeNull();
        expect(normalizeChatAnnotations([])).toEqual([]);
    });

    it("keeps the newest notes when a document is at the cap", () => {
        const many = Array.from({ length: 201 }, (_value, index) => ({ ...note, id: `ca_${index}`, start: index, end: index + 2 }));
        saveChatAnnotations("/tmp/cap.md", many);
        const kept = loadChatAnnotations("/tmp/cap.md");
        expect(kept).toHaveLength(200);
        expect(kept[0].id).toBe("ca_1");
        expect(kept[kept.length - 1].id).toBe("ca_200");
    });

    it("keeps a card parked above the quote and drops a zero offset", () => {
        expect(normalizeChatAnnotations([{ ...note, place: { x: 0, y: 0, above: true } }])).toEqual([
            { ...note, place: { x: 0, y: 0, above: true } },
        ]);
        expect(normalizeChatAnnotations([{ ...note, place: { x: 0, y: 0 } }])).toEqual([note]);
    });

    it("keeps one box per line and drops the box around all of them", () => {
        expect(chatAnnotationLineBoxes([
            { left: 10, top: 28, right: 40, bottom: 42 },
            { left: 10, top: 10, right: 80, bottom: 42 },
            { left: 10, top: 10, right: 80, bottom: 24 },
            { left: 0, top: 0, right: 0, bottom: 0 },
        ])).toEqual([
            { left: 10, top: 10, width: 70, height: 14 },
            { left: 10, top: 28, width: 30, height: 14 },
        ]);
        expect(chatAnnotationLineBoxes([{ left: 4, top: 6, right: 20, bottom: 18 }])).toEqual([
            { left: 4, top: 6, width: 16, height: 12 },
        ]);
        // One line is several inline boxes (plain, bold, link). A picture on
        // the next line is too tall to join them.
        expect(chatAnnotationLineBoxes([
            { left: 40, top: 8, right: 90, bottom: 26 },
            { left: 10, top: 11, right: 40, bottom: 25 },
            { left: 12, top: 30, right: 70, bottom: 90 },
        ])).toEqual([
            { left: 10, top: 8, width: 80, height: 18 },
            { left: 12, top: 30, width: 58, height: 60 },
        ]);
    });

    it("cuts a code line to its own scroller when the quote also covers the paragraph", () => {
        const host = document.createElement("div");
        const prose = document.createElement("p");
        prose.textContent = "before";
        const button = document.createElement("button");
        button.textContent = "seek";
        const well = document.createElement("div");
        well.style.overflow = "hidden";
        const pre = document.createElement("pre");
        pre.style.overflowX = "auto";
        const code = document.createElement("code");
        code.textContent = "code";
        pre.appendChild(code);
        well.appendChild(pre);
        host.append(prose, button, well);
        document.body.appendChild(host);
        const box = (left: number, top: number, right: number, bottom: number) => () => ({
            left, top, right, bottom, width: right - left, height: bottom - top, x: left, y: top, toJSON() { return {}; },
        }) as DOMRect;
        well.getBoundingClientRect = box(8, 30, 120, 84);
        pre.getBoundingClientRect = box(8, 36, 120, 80);
        const proto = Range.prototype;
        const previousRects = proto.getClientRects;
        const rect = (left: number, top: number, right: number, bottom: number) => ({
            left, top, right, bottom, width: right - left, height: bottom - top, x: left, y: top, toJSON() { return {}; },
        });
        proto.getClientRects = function getClientRects() {
            const text = this.toString();
            if (text === "before") return [rect(0, 0, 48, 18)] as unknown as DOMRectList;
            if (text === "code") return [rect(0, 40, 480, 58)] as unknown as DOMRectList;
            if (text === "seek") return [rect(0, 0, 900, 900)] as unknown as DOMRectList;
            return [] as unknown as DOMRectList;
        };
        try {
            const proseNode = prose.firstChild as Text;
            const codeNode = code.firstChild as Text;
            const range = document.createRange();
            range.setStart(proseNode, 0);
            range.setEnd(codeNode, codeNode.length);
            expect(chatAnnotationRangeLineRects(host, range)).toEqual({
                geometry: true,
                rects: [
                    { left: 0, top: 0, right: 48, bottom: 18 },
                    { left: 8, top: 40, right: 120, bottom: 58 },
                ],
            });
            const inside = document.createRange();
            inside.setStart(codeNode, 0);
            inside.setEnd(codeNode, codeNode.length);
            proto.getClientRects = function getClientRects() {
                return [rect(200, 40, 400, 58)] as unknown as DOMRectList;
            };
            expect(chatAnnotationRangeLineRects(host, inside)).toEqual({ geometry: true, rects: [] });
        } finally {
            proto.getClientRects = previousRects;
            host.remove();
        }
    });

    it("cuts a line to the frame it scrolls in", () => {
        const line = { left: 0, top: 10, right: 200, bottom: 24 };
        expect(clipChatAnnotationLine(line, [])).toEqual(line);
        expect(clipChatAnnotationLine(line, [{ left: 40, top: 0, right: 120, bottom: 80 }])).toEqual({
            left: 40, top: 10, right: 120, bottom: 24,
        });
        expect(clipChatAnnotationLine(line, [{ left: 220, top: 0, right: 300, bottom: 40 }])).toBeNull();
    });

    it("opens above the quote when the card would leave the chat column", () => {
        expect(chatAnnotationOpensAbove(400, 420, 0, 500, 120)).toBe(true);
        expect(chatAnnotationOpensAbove(100, 120, 0, 500, 120)).toBe(false);
        expect(chatAnnotationOpensAbove(20, 40, 0, 80, 120)).toBe(false);
        expect(chatAnnotationOpensAbove(100, 120, 0, 500, 0)).toBe(false);
    });

    it("replaces host notes and leaves a local note in place", () => {
        const local = { ...note, id: "ca_mine", body: "我的看法" };
        const stale = { ...note, id: "ca_old", body: "上一版", shared: true };
        const incoming = { ...note, id: "ca_host", body: "主持人看法" };
        const merged = mergeSharedChatAnnotations([local, stale], [incoming]);
        expect(merged.map((item) => item.id)).toEqual(["ca_mine", "ca_host"]);
        expect(merged[0].shared).toBeUndefined();
        expect(merged[1]).toMatchObject({ body: "主持人看法", shared: true });
    });

    it("omits the shared flag from the copy a share uploads", () => {
        saveChatAnnotations("/tmp/a.md", [{ ...note, shared: true }, { ...note, id: "ca_mine", shared: undefined }]);
        const uploaded = chatAnnotationsForShare("/tmp/a.md");
        expect(uploaded.every((item) => !("shared" in item))).toBe(true);
        expect(uploaded).toHaveLength(2);
    });

    it("writes to the host and keeps the note on a failed write", async () => {
        const calls: string[] = [];
        (window as unknown as { go: unknown }).go = {
            main: {
                App: {
                    SaveCompanionChatAnnotations: async (_path: string, body: string) => {
                        calls.push(body);
                        throw new Error("offline");
                    },
                },
            },
        };
        saveChatAnnotations("/tmp/fail.md", [note]);
        await new Promise((resolve) => setTimeout(resolve, 0));
        expect(calls).toEqual([JSON.stringify([note])]);
        expect(loadChatAnnotations("/tmp/fail.md")).toEqual([note]);
    });
});
