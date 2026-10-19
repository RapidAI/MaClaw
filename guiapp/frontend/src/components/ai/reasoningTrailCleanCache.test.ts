import { describe, expect, it } from "vitest";
import { cleanReasoningTrailForBody } from "./assistantReasoningBody";
import { appendReasoningRepair, createReasoningRepairCursor, repairReasoningLineBreaks } from "./reasoningLineBreaks";
import { rolePrefixLineOpen } from "./rolePrefixDisplay";
import { createReasoningTrailCleanCache, rememberReasoningTrailClean } from "./reasoningTrailCleanCache";

const cuisine = [
    "• 执行环境已就绪",
    "• 正在分析任务并开始处理",
    "The user is asking about Luzhou cuisine (lines 251-",
    "504) and how it differs from Chengdu.",
    "",
    "Let me think:",
    "泸州菜偏江湖，成都菜更精细。",
    "```",
    "Tool: hidden inside the fence",
    "```",
    "Visible conclusion stays.",
].join("\n");

function grow(text: string, chunks: number[]): string[] {
    const steps: string[] = [];
    let cursor = 0;
    let index = 0;
    while (cursor < text.length) {
        const size = chunks[index % chunks.length];
        index += 1;
        cursor = Math.min(text.length, cursor + size);
        steps.push(text.slice(0, cursor));
    }
    return steps;
}

function expectSameClean(text: string, chunks: number[]): void {
    const cache = createReasoningTrailCleanCache();
    for (const step of grow(text, chunks)) {
        const got = rememberReasoningTrailClean(cache, "thought-1", step);
        const want = cleanReasoningTrailForBody(step);
        expect(got).toBe(want);
    }
    expect(rememberReasoningTrailClean(cache, "thought-1", text)).toBe(cleanReasoningTrailForBody(text));
}

describe("rememberReasoningTrailClean", () => {
    it("matches the full clean while a thought is appended", () => {
        const samples = [
            cuisine,
            `• 已接收任务\n${"Major comment: missing scratch-prompt baseline. ".repeat(30)}`,
            "Hello Tool: this is mid-line and must stay",
            "before\nTool: secret\nshould drop",
            "before\nBrowser：隐藏",
            "```\nTool: secret\n```\nstill visible",
            "answer\n## 质量审计\nhidden tail",
            "intro\n## Summary\ndetails\n## 质量审计\ngone",
            "answer\n## 质量审计\nmore\n## 需要确认执行计划\nkept",
            "plan\n### T1 do the thing\nrest",
            "see 251-\n504 in the log",
            "snake.cpp (lines 251-\n504)",
            "整理歌曲（\n1996\n）列表",
            "Need to inspect foo(\n1. Read the file\n2. Compile",
            "versions 1-\n2) Compile",
            "first paragraph.\n\nsecond paragraph.",
            "before\n```\n(lines 251-\n504)\n```\nafter",
            "$$\nx```\n$$\n(251-\n504)",
            `hello\r\nworld\r\n(251-\r\n504)`,
            "trailing stays  ",
            `tofu\u0001text\u25a1more`,
            `${"😀".repeat(40)}\n(251-\n504) done`,
            "• 已接收任务，然后继续分析真正的问题，而不是停在里程碑上。",
            "已接收任务，正在准备执行路径\nreal work",
        ];
        for (const sample of samples) {
            expectSameClean(sample, [1]);
            expectSameClean(sample, [1, 3, 8]);
            expectSameClean(sample, [64]);
        }
    });

    it("does not treat a mid-line Tool label as a role cut", () => {
        const cache = createReasoningTrailCleanCache();
        const first = rememberReasoningTrailClean(cache, "t", "Hello");
        const second = rememberReasoningTrailClean(cache, "t", "Hello Tool: stay");
        expect(second.startsWith(first)).toBe(true);
        expect(second).toBe("Hello Tool: stay");
    });

    it("resets when the thought is replaced", () => {
        const cache = createReasoningTrailCleanCache();
        rememberReasoningTrailClean(cache, "t", "see 251-\n504");
        expect(rememberReasoningTrailClean(cache, "t", "different thought")).toBe("different thought");
        expect(rememberReasoningTrailClean(cache, "other", cuisine)).toBe(cleanReasoningTrailForBody(cuisine));
    });
});

describe("appendReasoningRepair", () => {
    it("matches a one-shot repair across chunk boundaries", () => {
        const samples = [
            "see 251-\n504 in the log",
            "snake.cpp (lines 251-\n504)",
            "整理歌曲（\n1996\n）列表",
            "hello\r\nworld",
            "hello\rworld",
            "```\ncode\n```\nstill outside",
            "$$\nx```\n$$\n(251-\n504)",
            "first paragraph.\n\nsecond paragraph.",
            "\n",
            "\n\n",
        ];
        for (const sample of samples) {
            const cursor = createReasoningRepairCursor();
            for (const step of grow(sample, [1])) {
                expect(appendReasoningRepair(cursor, step)).toBe(repairReasoningLineBreaks(step));
            }
        }
    });
});

describe("rolePrefixLineOpen", () => {
    it("stays open only while the line can still become a role prefix", () => {
        expect(rolePrefixLineOpen("")).toBe(true);
        expect(rolePrefixLineOpen("   ")).toBe(true);
        expect(rolePrefixLineOpen("Tool")).toBe(true);
        expect(rolePrefixLineOpen("1.")).toBe(true);
        expect(rolePrefixLineOpen("> ")).toBe(true);
        expect(rolePrefixLineOpen("Toolbox")).toBe(false);
        expect(rolePrefixLineOpen("Hello")).toBe(false);
        expect(rolePrefixLineOpen("123 ")).toBe(false);
        expect(rolePrefixLineOpen("Tool:")).toBe(false);
    });
});
