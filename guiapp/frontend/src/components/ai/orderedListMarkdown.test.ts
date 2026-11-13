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

    it("leaves a dotted number that has no list body", () => {
        expect(splitMidLineOrderedListMarkers("node 18.")).toBe("node 18.");
        expect(splitMidLineOrderedListMarkers("listen 8080.")).toBe("listen 8080.");
        expect(splitMidLineOrderedListMarkers("1.\r2.")).toBe("1.\n2.");
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

    it("does not treat hyphenated line ranges or parentheticals as glued lists", () => {
        expect(splitMidLineOrderedListMarkers("snake.cpp (lines 251-504) to confirm")).toBe(
            "snake.cpp (lines 251-504) to confirm",
        );
        expect(splitMidLineOrderedListMarkers("see (1996) list")).toBe("see (1996) list");
        expect(splitMidLineOrderedListMarkers("read file.cpp (lines 10-20) next")).toBe(
            "read file.cpp (lines 10-20) next",
        );
        expect(splitMidLineOrderedListMarkers("done-1) next")).toBe("done-1) next");
    });

    it("does not split math expressions glued to operator chars", () => {
        expect(splitMidLineOrderedListMarkers("联合训练一个插补器和一个 (n+1) 类判别器")).toBe(
            "联合训练一个插补器和一个 (n+1) 类判别器",
        );
        expect(splitMidLineOrderedListMarkers("set x=2. value")).toBe("set x=2. value");
        expect(splitMidLineOrderedListMarkers("a*3) next")).toBe("a*3) next");
        expect(splitMidLineOrderedListMarkers("约 n×2. 倍")).toBe("约 n×2. 倍");
        // Packed-marker path: "(n+1) … (n+2) …" is two parentheticals, not items.
        expect(splitMidLineOrderedListMarkers("(n+1) 类与 (n+2) 类")).toBe("(n+1) 类与 (n+2) 类");
        expect(splitMidLineOrderedListMarkers("见 (1) 项与 (2) 项")).toBe("见 (1) 项与 (2) 项");
        // Subscript-like bracket glue takes the same blocked-prev path.
        expect(splitMidLineOrderedListMarkers("见 arr[1. 项与 arr[2. 项")).toBe("见 arr[1. 项与 arr[2. 项");
        // Identifier glue: language/package names keep their version digits.
        expect(splitMidLineOrderedListMarkers("用 C#1. 实现")).toBe("用 C#1. 实现");
        expect(splitMidLineOrderedListMarkers("升级 pkg@1. 后")).toBe("升级 pkg@1. 后");
        // Operand-binding math symbols glue a following digit-run the same way.
        expect(splitMidLineOrderedListMarkers("令 x=√2. 继续")).toBe("令 x=√2. 继续");
        expect(splitMidLineOrderedListMarkers("求和 ∑1. 项")).toBe("求和 ∑1. 项");
        expect(splitMidLineOrderedListMarkers("映射 x→1. 处")).toBe("映射 x→1. 处");
    });

    it("does not split spaced markers whose gap follows an operator", () => {
        // Inside a list item's body.
        expect(splitMidLineOrderedListMarkers("1. 设 x + 2. y 时")).toBe("1. 设 x + 2. y 时");
        expect(splitMidLineOrderedListMarkers("1. 取 a - 2. b 后")).toBe("1. 取 a - 2. b 后");
        // Packed expansion in prose.
        expect(splitMidLineOrderedListMarkers("结果 1. a + 2. b")).toBe("结果 1. a + 2. b");
        // A real packed list after ordinary text still expands.
        expect(splitMidLineOrderedListMarkers("要点 1. a 2. b")).toBe("要点\n1. a\n2. b");
    });

    it("keeps table rows intact (markers inside cells are column text)", () => {
        const numbered = "| 1. 旧版 | 2. 新版 | 3. 最新 |";
        expect(splitMidLineOrderedListMarkers(numbered)).toBe(numbered);
        const glued = "| 步骤1) 先做 | 2) 再做 | 备注 |";
        expect(splitMidLineOrderedListMarkers(glued)).toBe(glued);
        const math = "| (n+1)-class discriminator | (n+1) 类判别器 | n 个真实类 |";
        expect(splitMidLineOrderedListMarkers(math)).toBe(math);
        // Non-table lines around a table still get repaired.
        expect(splitMidLineOrderedListMarkers("如下：1. a\n| 1. x | 2. y |\n完成。1. b")).toBe(
            "如下：\n1. a\n| 1. x | 2. y |\n完成。\n1. b",
        );
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
