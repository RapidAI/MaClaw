import { describe, expect, it } from "vitest";
import {
    buildMarkdownTableModel,
    isMarkdownTableRow,
    isMarkdownTableSeparatorRow,
    normalizeMarkdownTableLine,
    parseMarkdownTableCells,
    repairMixedNarrativeTable,
} from "./aiAssistantMarkdownTable";

describe("normalizeMarkdownTableLine", () => {
    it("strips unordered and ordered list markers from pipe rows", () => {
        expect(normalizeMarkdownTableLine("- | 今天 | 晴 |")).toBe("| 今天 | 晴 |");
        expect(normalizeMarkdownTableLine("* 20日 | 多云 |")).toBe("20日 | 多云 |");
        expect(normalizeMarkdownTableLine("+ | a | b |")).toBe("| a | b |");
        expect(normalizeMarkdownTableLine("1. | a | b |")).toBe("| a | b |");
        expect(normalizeMarkdownTableLine("2) \u2192 | 30\u00b0C | <3\u7ea7 |")).toBe("\u2192 | 30\u00b0C | <3\u7ea7 |");
        expect(normalizeMarkdownTableLine("\u2022 | x | y |")).toBe("| x | y |");
        expect(normalizeMarkdownTableLine("\u00b7 | p | q |")).toBe("| p | q |");
    });

    it("leaves ordinary list items and plain text alone", () => {
        expect(normalizeMarkdownTableLine("- bring an umbrella")).toBe("- bring an umbrella");
        expect(normalizeMarkdownTableLine("| already | a table |")).toBe("| already | a table |");
        expect(normalizeMarkdownTableLine("Use A | B as a label")).toBe("Use A | B as a label");
    });

    it("is idempotent for table lines", () => {
        const once = normalizeMarkdownTableLine("- | a | b |");
        expect(normalizeMarkdownTableLine(once)).toBe(once);
    });
});

describe("isMarkdownTableRow / separator", () => {
    it("detects list-prefixed rows as table rows", () => {
        expect(isMarkdownTableRow("- | 今天 | 晴 |")).toBe(true);
        expect(isMarkdownTableRow("1. 20日 | 多云 |")).toBe(true);
    });

    it("detects list-prefixed separator rows", () => {
        expect(isMarkdownTableSeparatorRow("- | --- | --- | --- |")).toBe(true);
        expect(isMarkdownTableSeparatorRow("| --- | --- |")).toBe(true);
        expect(isMarkdownTableSeparatorRow("| 今天 | 晴 |")).toBe(false);
    });
});

describe("parseMarkdownTableCells inline opaque spans", () => {
    it("keeps literal pipes inside dollar-delimited formulas in one cell", () => {
        expect(parseMarkdownTableCells("| $P(A|B)$ | conditional probability |"))
            .toEqual(["$P(A|B)$", "conditional probability"]);
    });

    it("keeps literal pipes inside LaTeX-parentheses formulas in one cell", () => {
        expect(parseMarkdownTableCells("| \\(A|B\\) | relation |"))
            .toEqual(["\\(A|B\\)", "relation"]);
    });

    it("keeps literal pipes inside variable-length inline code spans", () => {
        expect(parseMarkdownTableCells("| ``a | b`` | code |"))
            .toEqual(["``a | b``", "code"]);
    });

    it("continues to unescape an escaped table pipe", () => {
        expect(parseMarkdownTableCells("| A \\| B | value |"))
            .toEqual(["A | B", "value"]);
    });

    it("does not mistake currency pairs for an inline formula", () => {
        expect(parseMarkdownTableCells("| $5 | $10 |"))
            .toEqual(["$5", "$10"]);
    });
});

describe("repairMixedNarrativeTable split-row repair", () => {
    it("merges weather-style rows with a wrap glyph", () => {
        const model = buildMarkdownTableModel([
            "| 日期 | 天气 | 温度 | 风力 |",
            "| --- | --- | --- | --- |",
            "| 今天 (14日) | 多云转晴 |",
            "| → | 34°C / 22°C | <3级 |",
        ]);
        expect(model).not.toBeNull();
        const repaired = repairMixedNarrativeTable(model!);
        expect(repaired.bodyRows).toHaveLength(1);
        expect(parseMarkdownTableCells(repaired.bodyRows[0])).toEqual([
            "今天 (14日)", "多云转晴", "34°C / 22°C", "<3级",
        ]);
    });

    it("merges classic 1+(N-1) without a wrap glyph", () => {
        const model = buildMarkdownTableModel([
            "日期 | 天气 | 温度 | 风力",
            "--- | --- | --- | ---",
            "|今天|",
            "|- 阴转雷阵雨 | 24~29°C | 东风 1-3级|",
        ]);
        const repaired = repairMixedNarrativeTable(model!);
        expect(parseMarkdownTableCells(repaired.bodyRows[0])).toEqual([
            "今天", "- 阴转雷阵雨", "24~29°C", "东风 1-3级",
        ]);
    });

    it("does not merge independent short rows without a wrap glyph", () => {
        const model = buildMarkdownTableModel([
            "| Key | Val | Key2 | Val2 |",
            "| --- | --- | --- | --- |",
            "| alpha | 1 |",
            "| beta | 2 |",
        ]);
        const repaired = repairMixedNarrativeTable(model!);
        expect(repaired.bodyRows).toHaveLength(2);
    });

    it("does not treat leading empty cells alone as a wrap glyph", () => {
        const model = buildMarkdownTableModel([
            "| Key | Val | Key2 | Val2 |",
            "| --- | --- | --- | --- |",
            "| alpha | 1 |",
            "|  | beta | 2 |",
        ]);
        const repaired = repairMixedNarrativeTable(model!);
        expect(repaired.bodyRows).toHaveLength(2);
    });

    it("accepts list-prefixed body rows at the model boundary", () => {
        const model = buildMarkdownTableModel([
            "| 日期 | 天气 | 温度 | 风力 |",
            "| --- | --- | --- | --- |",
            "- 20日 (周一) | 雷阵雨转多云 |",
            "- → | 29°C / 22°C | <3级 |",
        ]);
        expect(model).not.toBeNull();
        const repaired = repairMixedNarrativeTable(model!);
        expect(repaired.bodyRows).toHaveLength(1);
        expect(parseMarkdownTableCells(repaired.bodyRows[0])).toEqual([
            "20日 (周一)", "雷阵雨转多云", "29°C / 22°C", "<3级",
        ]);
    });

    it("builds header-led tables without a GFM separator or outer pipes on body rows", () => {
        const model = buildMarkdownTableModel([
            "| 日期 | 天气 | 温度 | 风力 |",
            "今天 (24日) | 雷阵雨转多云",
            "→| 30°C / 23°C | <3级 |",
            "明天 (25日) | 雷阵雨转多云",
            "→| 30°C / 24°C | <3级 |",
            // Some rows omit the wrap glyph and only use a leading pipe on the continuation.
            "周一 (27日) | 多云",
            "| 33°C / 25°C | <3级 |",
        ]);
        expect(model).not.toBeNull();
        expect(model!.headerCells).toEqual(["日期", "天气", "温度", "风力"]);
        const repaired = repairMixedNarrativeTable(model!);
        expect(repaired.bodyRows).toHaveLength(3);
        expect(parseMarkdownTableCells(repaired.bodyRows[0])).toEqual([
            "今天 (24日)", "雷阵雨转多云", "30°C / 23°C", "<3级",
        ]);
        expect(parseMarkdownTableCells(repaired.bodyRows[1])).toEqual([
            "明天 (25日)", "雷阵雨转多云", "30°C / 24°C", "<3级",
        ]);
        expect(parseMarkdownTableCells(repaired.bodyRows[2])).toEqual([
            "周一 (27日)", "多云", "33°C / 25°C", "<3级",
        ]);
    });

    it("returns null for separator-only buffers", () => {
        expect(buildMarkdownTableModel(["|---|---|---|", "| --- | --- | --- |"])).toBeNull();
    });

    it("does not treat plain prose with a single pipe as a header-led table", () => {
        // First data row must start with "|" for headerLed; prose mid-pipe alone is not a table.
        expect(buildMarkdownTableModel([
            "Use A | B as labels",
            "C | D as values",
        ])).toBeNull();
    });
});
