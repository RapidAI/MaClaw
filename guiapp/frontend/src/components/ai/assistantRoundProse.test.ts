import { describe, expect, it } from "vitest";
import { clearAssistantRoundProse } from "./assistantRoundProse";

describe("clearAssistantRoundProse", () => {
    it("clears the answer draft while keeping prior thinking", () => {
        const next = clearAssistantRoundProse({
            content: "I'll connect next.",
            reasoning: "Need the MySQL profile first.",
        });
        expect(next.content).toBe("");
        expect(next.reasoning).toBe("Need the MySQL profile first.\n");
    });

    it("keeps thinking when the previous round had no answer text", () => {
        const next = clearAssistantRoundProse({
            content: "",
            reasoning: "First I listed connections.",
        });
        expect(next.content).toBe("");
        expect(next.reasoning).toBe("First I listed connections.\n");
    });

    it("does not add a second separator when thinking already ends with a newline", () => {
        const next = clearAssistantRoundProse({
            content: "draft",
            reasoning: "Done thinking.\n",
        });
        expect(next.content).toBe("");
        expect(next.reasoning).toBe("Done thinking.\n");
    });

    it("leaves an empty reasoning trail untouched", () => {
        const next = clearAssistantRoundProse({
            content: "draft",
            reasoning: "",
        });
        expect(next.content).toBe("");
        expect(next.reasoning).toBe("");
    });

    it("preserves substantive prose that accompanied a tool call (2026-09-18 ssh status report)", () => {
        const report = "**服务器状态已摸清：**\n\n| 服务 | 状态 |\n|---|---|\n| SSH (22) | ✅ 运行中 |\n\nCPU 占用异常，需要进一步排查。";
        const next = clearAssistantRoundProse({
            content: report,
            reasoning: "Need the MySQL profile first.",
        });
        expect(next.content).toBe(report + "\n\n");
        expect(next.reasoning).toBe("Need the MySQL profile first.\n");
    });

    it("does not add a third newline when preserved prose already ends with a blank line", () => {
        const prose = "这是一个长度超过四十字符的实质正文段落，用来验证已以空行结尾的保留正文不会得到第三个换行符。\n\n";
        const next = clearAssistantRoundProse({
            content: prose,
            reasoning: "",
        });
        expect(next.content).toBe(prose);
    });
});
