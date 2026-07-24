import { describe, expect, it } from "vitest";
import { attachBareHeadingMarkers, normalizeInlineListMarkers } from "./aiAssistantMarkdownNormalize";

describe("normalizeInlineListMarkers ordered markers", () => {
    it("does not split multi-digit ordered markers at the start of a line", () => {
        const input = "9. 第九条\n10. 世界杯决赛对阵图来了\n11. 查尔斯国王\n100. 百项";
        expect(normalizeInlineListMarkers(input)).toBe(input);
    });

    it("does not split multi-digit paren-form markers", () => {
        const input = "9) nine\n10) ten\n11) eleven";
        expect(normalizeInlineListMarkers(input)).toBe(input);
    });

    it("still inserts a newline before single-digit markers glued after non-digit text", () => {
        expect(normalizeInlineListMarkers("完成。1. 第一步")).toBe("完成。\n1. 第一步");
        expect(normalizeInlineListMarkers("完成。 1. 第一步")).toBe("完成。\n1. 第一步");
        expect(normalizeInlineListMarkers("如下： 10. 标题")).toBe("如下：\n10. 标题");
        expect(normalizeInlineListMarkers("text2. glued")).toBe("text\n2. glued");
        expect(normalizeInlineListMarkers("done1) next")).toBe("done\n1) next");
    });

    it("expands compact multi-item ordered lines with two-digit indices", () => {
        expect(normalizeInlineListMarkers("1. a 2. b 10. c")).toBe("1. a\n2. b\n10. c");
    });

    it("expands packed ordered items after a prose prefix", () => {
        expect(normalizeInlineListMarkers("国际要闻 1. 美国 2. 伊朗 10. 阿根廷")).toBe(
            "国际要闻\n1. 美国\n2. 伊朗\n10. 阿根廷",
        );
    });

    it("keeps nested indent when expanding compact ordered items end-to-end", () => {
        expect(normalizeInlineListMarkers("  1. a 2. b 10. c")).toBe("  1. a\n  2. b\n  10. c");
        expect(normalizeInlineListMarkers("\t9. 九 10. 十")).toBe("\t9. 九\n\t10. 十");
    });

    it("does not rewrite ordered markers inside fences, bold, or inline code", () => {
        const fenced = [
            "before",
            "```",
            "10. keep",
            "1. a 2. b",
            "```",
            "after：1. x 2. y",
        ].join("\n");
        const fencedOut = normalizeInlineListMarkers(fenced);
        expect(fencedOut).toContain("```\n10. keep\n1. a 2. b\n```");
        expect(fencedOut).toContain("after：\n1. x\n2. y");

        expect(normalizeInlineListMarkers("see **10. not list** then 完成。1. yes")).toBe(
            "see **10. not list** then 完成。\n1. yes",
        );
        expect(normalizeInlineListMarkers("use `10. code` then 完成。2. item")).toBe(
            "use `10. code` then 完成。\n2. item",
        );
    });

    it("keeps multi-digit markers intact when glued after CJK text", () => {
        // Must not peel "100." into "1" + "00."; still allows compact CJK list glue.
        expect(normalizeInlineListMarkers("项100. 内容")).toBe("项\n100. 内容");
    });

    it("does not break currency / Latin multi-digit / version-like prose", () => {
        expect(normalizeInlineListMarkers("$10. 00")).toBe("$10. 00");
        expect(normalizeInlineListMarkers("€10. 50")).toBe("€10. 50");
        expect(normalizeInlineListMarkers("USD10. 00")).toBe("USD10. 00");
        // Latin + multi-digit is treated as amount/id, not compact list glue.
        expect(normalizeInlineListMarkers("item10. next")).toBe("item10. next");
        // Preceding ASCII "." must not count as list glue ("v2.0. …" ≠ "…\n0. …").
        expect(normalizeInlineListMarkers("v2.0. 3. next")).toBe("v2.0. 3. next");
        expect(normalizeInlineListMarkers("end.10. next")).toBe("end.10. next");
    });

    it("does not invent breaks for spaced multi-digit prose numbers", () => {
        // Space before the number is outside this rewrite's scope (pre-existing).
        expect(normalizeInlineListMarkers("摘要 10. 详情")).toBe("摘要 10. 详情");
        expect(normalizeInlineListMarkers("foo 2. bar")).toBe("foo 2. bar");
    });

    it("does not turn \\n inside home/Windows paths into real newlines", () => {
        // Escaped-newline rewrite must not corrupt path segments like \notes.
        const homeWin = "工作目录：~\\.maclaw\\workspace\\notes";
        expect(normalizeInlineListMarkers(homeWin)).toBe(homeWin);

        const driveWin = "Open C:\\notes\\report.pdf";
        expect(normalizeInlineListMarkers(driveWin)).toBe(driveWin);

        // Literal escaped newlines in prose still expand.
        expect(normalizeInlineListMarkers("line1\\nline2")).toBe("line1\nline2");
    });

    it("does not rewrite ordered markers inside fenced code blocks", () => {
        const input = "Before\n```text\n10. keep together\n```\nAfter";
        expect(normalizeInlineListMarkers(input)).toBe(input);
    });

    it("leaves pure prose without ordered-marker shapes unchanged", () => {
        const input = "今天天气不错，适合出行。没有列表。";
        expect(normalizeInlineListMarkers(input)).toBe(input);
    });
});

describe("attachBareHeadingMarkers list-title attach", () => {
    it("attaches bare ### to a following dash-list title", () => {
        expect(attachBareHeadingMarkers(["###", "- 北京·城区天气预报"])).toEqual([
            "### 北京·城区天气预报",
        ]);
    });

    it("attaches bare ### to a following unicode-bullet title", () => {
        expect(attachBareHeadingMarkers(["###", "\u2022 \u4eca\u65e5\u751f\u6d3b\u6307\u6570"])).toEqual([
            "### \u4eca\u65e5\u751f\u6d3b\u6307\u6570",
        ]);
    });

    it("still skips real nested headings after a bare marker", () => {
        expect(attachBareHeadingMarkers(["####", "### Existing title"])).toEqual([
            "####",
            "### Existing title",
        ]);
    });

    it("promotes only the first list item under a bare marker; later items stay lists", () => {
        expect(attachBareHeadingMarkers(["###", "- 标题", "- 后续条目"])).toEqual([
            "### 标题",
            "- 后续条目",
        ]);
    });

    it("attaches bare ### to an ordered-list title", () => {
        expect(attachBareHeadingMarkers(["###", "1. 生活指数"])).toEqual([
            "### 生活指数",
        ]);
    });
});
