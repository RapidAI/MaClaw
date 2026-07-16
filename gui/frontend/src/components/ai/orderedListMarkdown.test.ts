import { describe, expect, it } from "vitest";
import {
    ORDERED_LIST_ITEM_LINE,
    leadingIndentColumns,
    orderedListIndentPadding,
    packedOrderedLooksLikeList,
    parseOrderedListLine,
    splitMidLineOrderedListMarkers,
} from "./orderedListMarkdown";

describe("ORDERED_LIST_ITEM_LINE", () => {
    it("parses multi-digit dot and paren markers", () => {
        expect("10. 世界杯".match(ORDERED_LIST_ITEM_LINE)?.slice(1, 4)).toEqual(["10", ".", "世界杯"]);
        expect("11) body".match(ORDERED_LIST_ITEM_LINE)?.slice(1, 4)).toEqual(["11", ")", "body"]);
        expect("100. 百项".match(ORDERED_LIST_ITEM_LINE)?.slice(1, 4)).toEqual(["100", ".", "百项"]);
    });

    it("allows empty body for streaming frames", () => {
        expect("10.".match(ORDERED_LIST_ITEM_LINE)?.slice(1, 4)).toEqual(["10", ".", undefined]);
        expect("10. ".match(ORDERED_LIST_ITEM_LINE)?.slice(1, 4)).toEqual(["10", ".", ""]);
        expect(parseOrderedListLine("10.")).toEqual({
            indentCols: 0,
            indentText: "",
            marker: "10.",
            body: "",
        });
        expect(parseOrderedListLine("  11) ")).toEqual({
            indentCols: 2,
            indentText: "  ",
            marker: "11)",
            body: "",
        });
    });

    it("rejects non-list shapes", () => {
        expect("10.no-space".match(ORDERED_LIST_ITEM_LINE)).toBeNull();
        expect("v2.0. 3".match(ORDERED_LIST_ITEM_LINE)).toBeNull();
        expect("- item".match(ORDERED_LIST_ITEM_LINE)).toBeNull();
    });
});

describe("parseOrderedListLine + indent", () => {
    it("preserves indent columns for nested ordered items", () => {
        expect(parseOrderedListLine("10. 世界杯")).toEqual({
            indentCols: 0,
            indentText: "",
            marker: "10.",
            body: "世界杯",
        });
        expect(parseOrderedListLine("  10) nested")).toEqual({
            indentCols: 2,
            indentText: "  ",
            marker: "10)",
            body: "nested",
        });
        expect(parseOrderedListLine("\t11. tabbed")).toEqual({
            indentCols: 4,
            indentText: "\t",
            marker: "11.",
            body: "tabbed",
        });
        expect(leadingIndentColumns("    x")).toBe(4);
        expect(orderedListIndentPadding(0)).toBeUndefined();
        expect(orderedListIndentPadding(2)).toBe("1em");
        expect(orderedListIndentPadding(100)).toBe("16em"); // capped at 32 cols
    });

    it("returns null for non-list lines", () => {
        expect(parseOrderedListLine("plain")).toBeNull();
        expect(parseOrderedListLine("- bullet")).toBeNull();
    });
});

describe("packedOrderedLooksLikeList", () => {
    it("accepts classic 1-based runs and long consecutive runs", () => {
        expect(packedOrderedLooksLikeList([1, 2])).toBe(true);
        expect(packedOrderedLooksLikeList([1, 2, 10, 11])).toBe(true);
        expect(packedOrderedLooksLikeList([9, 10, 11])).toBe(true);
    });

    it("rejects single markers and sparse section-like pairs", () => {
        expect(packedOrderedLooksLikeList([10])).toBe(false);
        expect(packedOrderedLooksLikeList([2, 3])).toBe(false);
        expect(packedOrderedLooksLikeList([1])).toBe(false);
        expect(packedOrderedLooksLikeList([5, 7, 9])).toBe(false);
    });
});

describe("splitMidLineOrderedListMarkers", () => {
    it("does not peel multi-digit line-start markers", () => {
        const input = "9. a\n10. b\n11. c\n100. d";
        expect(splitMidLineOrderedListMarkers(input)).toBe(input);
    });

    it("splits after CJK/punctuation glue (no space)", () => {
        expect(splitMidLineOrderedListMarkers("完成。1. 第一步")).toBe("完成。\n1. 第一步");
        expect(splitMidLineOrderedListMarkers("Note:1. first")).toBe("Note:\n1. first");
        expect(splitMidLineOrderedListMarkers("（见下）2. next")).toBe("（见下）\n2. next");
        expect(splitMidLineOrderedListMarkers("项100. 内容")).toBe("项\n100. 内容");
    });

    it("splits after punctuation even when a space separates the marker", () => {
        expect(splitMidLineOrderedListMarkers("完成。 1. 第一步")).toBe("完成。\n1. 第一步");
        expect(splitMidLineOrderedListMarkers("如下： 10. 标题")).toBe("如下：\n10. 标题");
        expect(splitMidLineOrderedListMarkers("Note: 11) body")).toBe("Note:\n11) body");
    });

    it("keeps single-digit letter glue as list breaks", () => {
        expect(splitMidLineOrderedListMarkers("text2. glued")).toBe("text\n2. glued");
        expect(splitMidLineOrderedListMarkers("done1) next")).toBe("done\n1) next");
    });

    it("expands compact multi-item ordered lines including two-digit indices", () => {
        expect(splitMidLineOrderedListMarkers("1. a 2. b 10. c 11. d")).toBe(
            "1. a\n2. b\n10. c\n11. d",
        );
        expect(splitMidLineOrderedListMarkers("9. 九 10. 十 11. 十一")).toBe(
            "9. 九\n10. 十\n11. 十一",
        );
    });

    it("preserves leading indent on ordered lines (does not treat indent as next item)", () => {
        expect(splitMidLineOrderedListMarkers("1. a\n  12. nested")).toBe("1. a\n  12. nested");
        expect(splitMidLineOrderedListMarkers("1. a 2. b\n  10. nested")).toBe(
            "1. a\n2. b\n  10. nested",
        );
        // Nested compact multi-item: expanded siblings keep the parent indent.
        expect(splitMidLineOrderedListMarkers("  1. a 2. b 10. c")).toBe(
            "  1. a\n  2. b\n  10. c",
        );
        expect(parseOrderedListLine("  12. nested")).toEqual({
            indentCols: 2,
            indentText: "  ",
            marker: "12.",
            body: "nested",
        });
    });

    it("expands packed ordered items embedded in prose prefixes", () => {
        expect(splitMidLineOrderedListMarkers("国际要闻 1. 美国 2. 伊朗 10. 阿根廷")).toBe(
            "国际要闻\n1. 美国\n2. 伊朗\n10. 阿根廷",
        );
        expect(splitMidLineOrderedListMarkers("要点 9. 九 10. 十 11. 十一")).toBe(
            "要点\n9. 九\n10. 十\n11. 十一",
        );
    });

    it("does not treat decimal-like tails on list lines as the next item", () => {
        expect(splitMidLineOrderedListMarkers("1. cost is 2. 5 yuan")).toBe(
            "1. cost is 2. 5 yuan",
        );
    });

    it("does not break currency or version-like prose", () => {
        expect(splitMidLineOrderedListMarkers("$10. 00")).toBe("$10. 00");
        expect(splitMidLineOrderedListMarkers("€10. 50")).toBe("€10. 50");
        expect(splitMidLineOrderedListMarkers("¥10. 00")).toBe("¥10. 00");
        expect(splitMidLineOrderedListMarkers("￥10. 00")).toBe("￥10. 00");
        expect(splitMidLineOrderedListMarkers("USD10. 00")).toBe("USD10. 00");
        expect(splitMidLineOrderedListMarkers("v2.0. 3. next")).toBe("v2.0. 3. next");
    });

    it("does not invent breaks for a single spaced prose number", () => {
        expect(splitMidLineOrderedListMarkers("摘要 10. 详情")).toBe("摘要 10. 详情");
        expect(splitMidLineOrderedListMarkers("foo 2. bar")).toBe("foo 2. bar");
        // Sparse section-like pair without starting at 1 and without 3-step run.
        expect(splitMidLineOrderedListMarkers("see section 2. intro and 3. details")).toBe(
            "see section 2. intro and 3. details",
        );
    });

    it("is a no-op for empty input and pure prose", () => {
        expect(splitMidLineOrderedListMarkers("")).toBe("");
        expect(splitMidLineOrderedListMarkers("今天天气不错")).toBe("今天天气不错");
    });

    it("handles punctuation glue then compact multi-item on the next logical items", () => {
        expect(splitMidLineOrderedListMarkers("如下：1. a 2. b 10. c")).toBe(
            "如下：\n1. a\n2. b\n10. c",
        );
    });

    it("normalizes real CRLF before line-wise expansion", () => {
        expect(splitMidLineOrderedListMarkers("完成。1. 项\r\n10. 二")).toBe(
            "完成。\n1. 项\n10. 二",
        );
        expect(splitMidLineOrderedListMarkers("1. a 2. b\r\n10. c")).toBe(
            "1. a\n2. b\n10. c",
        );
        // Lone CR between items (old Mac / mixed streams)
        expect(splitMidLineOrderedListMarkers("1. a\r2. b\r10. c")).toBe(
            "1. a\n2. b\n10. c",
        );
    });

    it("keeps multi-digit line lists stable (identity)", () => {
        const input = "9. 九\n10. 十\n11. 十一\n100. 百";
        expect(splitMidLineOrderedListMarkers(input)).toBe(input);
    });
});
