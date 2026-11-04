// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import {
    clearDomFindMarks,
    collectFindSources,
    findSlice,
    findTextHits,
    paintDomFindMarks,
    pdfFindSources,
    scrollMatchIntoScroller,
} from "./documentFind";

describe("findTextHits", () => {
    it("matches a phrase across a line break without gluing the words", () => {
        const runs = pdfFindSources([
            { str: "beta ", hasEOL: false },
            { str: "target", hasEOL: true },
            { str: "next", hasEOL: false },
        ]);
        expect(findTextHits(runs, "target").map((hit) => hit.parts)).toEqual([
            [{ run: 1, start: 0, end: 6 }],
        ]);
        expect(findTextHits(runs, "beta target")).toEqual([
            { parts: [{ run: 0, start: 0, end: 5 }, { run: 1, start: 0, end: 6 }] },
        ]);
        expect(findTextHits(runs, "target next")).toEqual([
            { parts: [{ run: 1, start: 0, end: 6 }, { run: 2, start: 0, end: 4 }] },
        ]);
        expect(findTextHits(runs, "targetnext")).toEqual([]);
        expect(findTextHits(runs, "TARGET")).toEqual([
            { parts: [{ run: 1, start: 0, end: 6 }] },
        ]);
        expect(findTextHits(runs, "  ")).toEqual([]);
    });

    it("steps by the match length so adjacent copies are both found", () => {
        expect(findTextHits([{ text: "aaaa" }], "aa")).toEqual([
            { parts: [{ run: 0, start: 0, end: 2 }] },
            { parts: [{ run: 0, start: 2, end: 4 }] },
        ]);
        expect(findTextHits([{ text: "Hello world" }], "Hell")).toEqual([
            { parts: [{ run: 0, start: 0, end: 4 }] },
        ]);
    });

    it("inserts a space where PDF runs sit apart on one line", () => {
        const runs = pdfFindSources([
            { str: "hello", transform: [12, 0, 0, 12, 40, 300], width: 40 },
            { str: "world", transform: [12, 0, 0, 12, 100, 300], width: 40 },
        ]);
        expect(findTextHits(runs, "hello world")).toEqual([
            { parts: [{ run: 0, start: 0, end: 5 }, { run: 1, start: 0, end: 5 }] },
        ]);
        const tight = pdfFindSources([
            { str: "hel", transform: [12, 0, 0, 12, 40, 300], width: 20 },
            { str: "lo", transform: [12, 0, 0, 12, 60, 300], width: 14 },
        ]);
        expect(findTextHits(tight, "hello")).toEqual([
            { parts: [{ run: 0, start: 0, end: 3 }, { run: 1, start: 0, end: 2 }] },
        ]);
    });

    it("folds accents onto the original characters", () => {
        expect(findTextHits([{ text: "İstanbul" }], "istanbul")).toEqual([
            { parts: [{ run: 0, start: 0, end: "İstanbul".length }] },
        ]);
        expect(findTextHits([{ text: "café" }], "cafe")).toEqual([
            { parts: [{ run: 0, start: 0, end: "café".length }] },
        ]);
        expect(findTextHits([{ text: "你好世界" }], "你好")).toEqual([
            { parts: [{ run: 0, start: 0, end: 2 }] },
        ]);
        expect(findTextHits([{ text: "你好\u3000世界" }], "你好 世界")).toEqual([
            { parts: [{ run: 0, start: 0, end: "你好\u3000世界".length }] },
        ]);
        expect(findTextHits([{ text: "Ｈｅｌｌｏ" }], "hello")).toEqual([
            { parts: [{ run: 0, start: 0, end: 5 }] },
        ]);
        expect(findTextHits([{ text: "Ｈｅｌｌｏ" }], "Hell")).toEqual([
            { parts: [{ run: 0, start: 0, end: 4 }] },
        ]);
        expect(findTextHits([{ text: "１２３" }], "123")).toEqual([
            { parts: [{ run: 0, start: 0, end: 3 }] },
        ]);
        expect(findTextHits([{ text: "hel\u00ADlo" }], "hello")).toEqual([
            { parts: [{ run: 0, start: 0, end: "hel\u00ADlo".length }] },
        ]);
        expect(findTextHits([{ text: "hel\u200Blo" }], "hello")).toEqual([
            { parts: [{ run: 0, start: 0, end: "hel\u200Blo".length }] },
        ]);
        expect(findTextHits([{ text: "hello  world" }], "hello world")).toEqual([
            { parts: [{ run: 0, start: 0, end: "hello  world".length }] },
        ]);
        expect(findTextHits([{ text: "Hello  world" }], "Hell")).toEqual([
            { parts: [{ run: 0, start: 0, end: 4 }] },
        ]);
        expect(findTextHits(
            [{ text: "hello" }, { text: "\u2003" }, { text: "world" }],
            "hello world",
        )).toEqual([
            { parts: [{ run: 0, start: 0, end: 5 }, { run: 1, start: 0, end: 1 }, { run: 2, start: 0, end: 5 }] },
        ]);
        expect(findTextHits(
            [{ text: "hello " }, { text: "\u2003" }, { text: "world" }],
            "hello world",
        )).toEqual([
            { parts: [{ run: 0, start: 0, end: 6 }, { run: 2, start: 0, end: 5 }] },
        ]);
    });

    it("treats a vertical run gap as a space along the baseline", () => {
        const vertical = pdfFindSources([
            { str: "hello", transform: [0, -12, 12, 0, 40, 300], width: 40 },
            { str: "world", transform: [0, -12, 12, 0, 40, 240], width: 40 },
        ]);
        expect(findTextHits(vertical, "hello world")).toEqual([
            { parts: [{ run: 0, start: 0, end: 5 }, { run: 1, start: 0, end: 5 }] },
        ]);
        expect(findTextHits(vertical, "helloworld")).toEqual([]);
    });
});

describe("findSlice", () => {
    it("keeps CJK even and gives a wide letter more of the run", () => {
        expect(findSlice("你好", 0, 1)).toEqual({ lead: 0, slice: 0.5 });
        expect(findSlice("你好", 0, 2)).toEqual({ lead: 0, slice: 1 });
        expect(findSlice("Hello", 0, 5)).toEqual({ lead: 0, slice: 1 });
        expect(findSlice("Wii", 0, 1).slice).toBeGreaterThan(findSlice("Wii", 1, 2).slice);
    });
});

describe("dom find marks", () => {
    it("joins inline runs and keeps a space across paragraphs and line breaks", () => {
        const root = document.createElement("div");
        root.innerHTML = "<p><span>hel</span><span>lo</span></p><p>world</p><p>tail<br>end</p>";
        document.body.appendChild(root);
        const sources = collectFindSources(root);
        const hits = (query: string) => findTextHits(
            sources.map((run) => ({ text: run.text, gapAfter: run.gapAfter })),
            query,
        );
        expect(hits("hello")).toHaveLength(1);
        expect(hits("helloworld")).toEqual([]);
        expect(hits("hello world")).toHaveLength(1);
        expect(hits("tail end")).toHaveLength(1);
        expect(hits("tailend")).toEqual([]);
        root.remove();
    });

    it("highlights every match, moves the active one, then restores the text", () => {
        const root = document.createElement("div");
        root.innerHTML = "<section><p>Alpha target</p></section><section><p>Later target</p></section>";
        document.body.appendChild(root);
        const sources = collectFindSources(root);
        const hits = findTextHits(sources.map((run) => ({ text: run.text, gapAfter: run.gapAfter })), "target");
        expect(hits).toHaveLength(2);
        const paint = (active: number) => {
            clearDomFindMarks(root);
            const fresh = collectFindSources(root);
            const marks = hits.flatMap((hit, index) => hit.parts.flatMap((part) => {
                const node = fresh[part.run]?.node;
                if (!node) return [];
                return [{ node, start: part.start, end: part.end, active: index === active, index }];
            }));
            paintDomFindMarks(marks);
        };
        paint(0);
        expect(root.querySelector("mark[data-find-active='true']")?.textContent).toBe("target");
        expect(root.querySelectorAll("mark.companion-preview-find")).toHaveLength(2);
        paint(1);
        const active = root.querySelectorAll("mark[data-find-active='true']");
        expect(active).toHaveLength(1);
        expect(active[0].parentElement?.textContent).toContain("Later");
        clearDomFindMarks(root);
        expect(root.querySelector("mark")).toBeNull();
        expect(root.textContent).toContain("Alpha target");
        expect(root.textContent).toContain("Later target");
        root.remove();
    });

    it("scrolls only the preview scroller", () => {
        const scroller = document.createElement("div");
        const target = document.createElement("span");
        scroller.appendChild(target);
        document.body.appendChild(scroller);
        Object.defineProperty(scroller, "clientHeight", { configurable: true, value: 100 });
        scroller.getBoundingClientRect = () => ({
            x: 0, y: 0, left: 0, top: 0, right: 200, bottom: 100, width: 200, height: 100, toJSON() { return {}; },
        });
        target.getBoundingClientRect = () => ({
            x: 0, y: 250, left: 0, top: 250, right: 40, bottom: 270, width: 40, height: 20, toJSON() { return {}; },
        });
        const before = document.documentElement.scrollTop;
        scrollMatchIntoScroller(scroller, target);
        expect(scroller.scrollTop).toBe(210);
        expect(document.documentElement.scrollTop).toBe(before);
        scroller.remove();
    });
});
