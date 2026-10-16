import { describe, expect, it } from "vitest";
import { clearAssistantRoundProse, isRejectedRoundContentToken, rejectStreamedAssistantContent } from "./assistantRoundProse";

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

    it("keeps a short draft that already recorded a tool call", () => {
        const content = "让我试试\n\n<!--maclaw-tool:msg-1-->\n\n";
        const next = clearAssistantRoundProse({
            content,
            reasoning: "Need the host.",
        });
        expect(next.content).toBe(content);
        expect(next.reasoning).toBe("Need the host.\n");
    });

    it("drops a streamed draft the parser rejected", () => {
        const forecast = "根据搜索到的公开预报信息：北京今天 26°C，适合出行。";
        expect(isRejectedRoundContentToken("\x02")).toBe(true);
        expect(isRejectedRoundContentToken("北京")).toBe(false);
        expect(rejectStreamedAssistantContent({ content: forecast, reasoning: "直接搜索。\n" })).toEqual({
            content: "",
            reasoning: "直接搜索。\n",
        });
        expect(rejectStreamedAssistantContent({ content: forecast, reasoning: "直接搜索。\n" }, "")).toEqual({
            content: "",
            reasoning: "直接搜索。\n",
        });
    });

    it("restores only this round's baseline when a later draft is rejected", () => {
        const kept = "这是上一轮已经保留的实质汇报，长度超过四十个字符，下一轮的预报草稿不能把它清掉。";
        const baseline = `${kept}\n\n`;
        const forecast = `${baseline}根据搜索到的公开预报信息：北京今天 26°C，适合出行。`;
        expect(rejectStreamedAssistantContent({ content: forecast, reasoning: "直接搜索。\n" }, baseline)).toEqual({
            content: baseline,
            reasoning: "直接搜索。\n",
        });
        const already = { content: baseline, reasoning: "直接搜索。\n" };
        expect(rejectStreamedAssistantContent(already, baseline)).toBe(already);
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
