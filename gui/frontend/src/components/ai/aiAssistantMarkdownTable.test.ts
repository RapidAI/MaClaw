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
        expect(normalizeMarkdownTableLine("1. | a | b |")).toBe("| a | b |");
        expect(normalizeMarkdownTableLine("2) → | 30°C | <3级 |")).toBe("→ | 30°C | <3级 |");
        expect(normalizeMarkdownTableLine("• | x | y |")).toBe("| x | y |");
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
});
