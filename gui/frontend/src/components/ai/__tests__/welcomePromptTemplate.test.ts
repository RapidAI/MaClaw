import { describe, expect, it } from "vitest";
import {
    extractHintChips,
    extractWelcomeTemplateFields,
    fillWelcomeTemplate,
    welcomeTemplateNeedsParams,
} from "../welcomePromptTemplate";
import { SCENARIO_TABS } from "../welcomeScenarioTasks";

describe("extractHintChips", () => {
    it("splits slash and middle-dot enumerations", () => {
        expect(extractHintChips("正式 / 简洁 / 强硬")).toEqual(["正式", "简洁", "强硬"]);
        expect(extractHintChips("测试/预发/生产")).toEqual(["测试", "预发", "生产"]);
        expect(extractHintChips("甲方 · 乙方")).toEqual(["甲方", "乙方"]);
    });

    it("returns empty for free-form prose", () => {
        expect(extractHintChips("一句话目标 + 关键点")).toEqual([]);
        expect(extractHintChips("粘贴内容")).toEqual([]);
        expect(extractHintChips("")).toEqual([]);
    });

    it("skips example-style hints", () => {
        expect(extractHintChips("例如 SaaS / 协作工具")).toEqual([]);
        expect(extractHintChips("e.g. SaaS / collab")).toEqual([]);
    });
});

describe("extractWelcomeTemplateFields", () => {
    it("parses labeled Chinese placeholders in order", () => {
        const template =
            "帮我做竞品分析\n行业/赛道：[例如 SaaS]\n我方产品：[名称]\n请输出结论。";
        const fields = extractWelcomeTemplateFields(template);
        expect(fields).toHaveLength(2);
        expect(fields[0]).toMatchObject({
            id: "f0",
            label: "行业/赛道",
            hint: "例如 SaaS",
        });
        expect(fields[1]).toMatchObject({
            id: "f1",
            label: "我方产品",
            hint: "名称",
        });
    });

    it("marks paste/file hints as multiline", () => {
        const template = "原始要点：[粘贴内容]\n语气：[专业]";
        const fields = extractWelcomeTemplateFields(template);
        expect(fields[0].multiline).toBe(true);
        expect(fields[1].multiline).toBe(false);
    });

    it("attaches chips for choice-style hints", () => {
        const fields = extractWelcomeTemplateFields("语气：[正式 / 简洁 / 强硬]");
        expect(fields[0].chips).toEqual(["正式", "简洁", "强硬"]);
    });

    it("handles English Label: [hint] form", () => {
        const fields = extractWelcomeTemplateFields("Industry: [e.g. SaaS]\nProduct: [name]");
        expect(fields.map((f) => f.label)).toEqual(["Industry", "Product"]);
    });

    it("uses hint as label when the line has no 标签： prefix", () => {
        const fields = extractWelcomeTemplateFields("Please cover [scope of review] carefully.");
        expect(fields).toHaveLength(1);
        expect(fields[0].label).toBe("scope of review");
        expect(fields[0].hint).toBe("scope of review");
    });
});

describe("fillWelcomeTemplate", () => {
    it("replaces placeholders with values and leaves empties blank", () => {
        const template = "主题：[主题]\n对象：[听众]\n结束";
        const fields = extractWelcomeTemplateFields(template);
        const filled = fillWelcomeTemplate(template, fields, {
            f0: "季度复盘",
            f1: "",
        });
        expect(filled).toContain("主题：季度复盘");
        expect(filled).toContain("对象：");
        expect(filled).not.toContain("[主题]");
        expect(filled).not.toContain("[听众]");
        expect(filled.endsWith("结束")).toBe(true);
    });
});

describe("welcome scenario task templates (parser smoke)", () => {
    it("catalog templates parse with labeled fields and chips arrays", () => {
        // Full structural rules live in welcomeScenarioCatalogGuards / audit tests.
        // Here we only smoke-check the field parser against real catalog text.
        const sample = SCENARIO_TABS.flatMap((t) => t.prompts).slice(0, 12);
        for (const prompt of sample) {
            for (const template of [prompt.template, prompt.templateEn]) {
                if (!template) continue;
                const fields = extractWelcomeTemplateFields(template);
                expect(welcomeTemplateNeedsParams(template)).toBe(fields.length > 0);
                expect(fields.every((f) => f.label.trim().length > 0)).toBe(true);
                expect(fields.every((f) => Array.isArray(f.chips))).toBe(true);
            }
        }
    });
});
