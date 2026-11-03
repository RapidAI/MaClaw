import { describe, expect, it } from "vitest";
import { firstVEStreamText, omitGoalContinuationHeader, sanitizeVisibleChatText, sanitizeVisibleVEText, visibleHistoryMessageContent, visibleVEStreamContent } from "../visibleChatText";

describe("visibleChatText", () => {
    it("drops reasoning-lane stream deltas and keeps assembled answers", () => {
        expect(visibleVEStreamContent("\x01private reasoning")).toBe("");
        expect(sanitizeVisibleVEText("\x01visible answer")).toBe("visible answer");
        expect(visibleVEStreamContent("I am \uEB90Kate")).toBe("I am Kate");
        expect(sanitizeVisibleChatText("\x01The\x01 user\x01 wants")).toBe("The user wants");
        expect(sanitizeVisibleVEText("\x01The\x01 user\x01 wants")).toBe("The user wants");
        expect(sanitizeVisibleChatText("reason\u25A1ing")).toBe("reasoning");
    });

    it("hides a goal continuation header line and keeps the rest of the turn", () => {
        const prompt = "[系统续接][execute] 按步骤执行目标：开发 c++ 贪吃蛇游戏\n\n本轮只能提交步骤 4：实现绘制与界面。";
        expect(omitGoalContinuationHeader(prompt)).toBe("本轮只能提交步骤 4：实现绘制与界面。");
        expect(omitGoalContinuationHeader("请看这段：[系统续接] 不是宿主阶段")).toBe("请看这段：[系统续接] 不是宿主阶段");
        expect(omitGoalContinuationHeader("普通回复")).toBe("普通回复");
        expect(omitGoalContinuationHeader("[系统续接][execute] 标题\n\n```\n\n\ncode\n```")).toBe("```\n\n\ncode\n```");
    });

    it("prefers the first non-empty string and treats history kinds correctly", () => {
        expect(firstVEStreamText("", "legacy")).toBe("legacy");
        expect(visibleHistoryMessageContent("stream_chunk", "\x01private", "visible")).toBe("");
        expect(visibleHistoryMessageContent("answer", "", "\x01kept answer")).toBe("kept answer");
    });
});
