import { describe, expect, it } from "vitest";
import { chatTextHasTofu, firstVEStreamText, omitGoalContinuationHeader, projectAnswerSlices, sanitizeVisibleChatText, sanitizeVisibleVEText, visibleHistoryMessageContent, visibleVEStreamContent } from "../visibleChatText";

describe("visibleChatText", () => {
    it("drops reasoning-lane stream deltas and keeps assembled answers", () => {
        expect(visibleVEStreamContent("\x01private reasoning")).toBe("");
        expect(sanitizeVisibleVEText("\x01visible answer")).toBe("visible answer");
        expect(visibleVEStreamContent("I am \uEB90Kate")).toBe("I am Kate");
        expect(sanitizeVisibleChatText("\x01The\x01 user\x01 wants")).toBe("The user wants");
        expect(sanitizeVisibleVEText("\x01The\x01 user\x01 wants")).toBe("The user wants");
        expect(sanitizeVisibleChatText("reason\u25A1ing")).toBe("reasoning");
        const clean = "The user wants desktopd\n\tnext";
        expect(sanitizeVisibleChatText(clean)).toBe(clean);
        expect(chatTextHasTofu(clean)).toBe(false);
        expect(chatTextHasTofu("\x01The")).toBe(true);
        expect(chatTextHasTofu("The", 0)).toBe(false);
    });

    it("hides a goal continuation header line and keeps the rest of the turn", () => {
        const prompt = "[系统续接][execute] 按步骤执行目标：开发 c++ 贪吃蛇游戏\n\n本轮只能提交步骤 4：实现绘制与界面。";
        expect(omitGoalContinuationHeader(prompt)).toBe("本轮只能提交步骤 4：实现绘制与界面。");
        expect(omitGoalContinuationHeader("请看这段：[系统续接] 不是宿主阶段")).toBe("请看这段：[系统续接] 不是宿主阶段");
        expect(omitGoalContinuationHeader("普通回复")).toBe("普通回复");
        expect(omitGoalContinuationHeader("[系统续接][execute] 标题\n\n```\n\n\ncode\n```")).toBe("```\n\n\ncode\n```");
    });

    it("folds an opening and an ending that share a run", () => {
        const shared = "重叠".repeat(40);
        const opening = "甲".repeat(40) + shared;
        const rest = "乙".repeat(30) + "结尾";
        expect(projectAnswerSlices(opening, shared + rest)).toBe(opening + rest);
        expect(projectAnswerSlices(opening, opening + "结尾")).toBe(opening + "结尾");
        expect(projectAnswerSlices(opening + "结尾", opening)).toBe(opening + "结尾");
    });

    it("keeps a gap when the slices only share a mark and concatenates a short fragment", () => {
        const opening = "甲".repeat(39) + "。";
        const ending = "。" + "乙".repeat(40) + "结尾";
        expect(projectAnswerSlices(opening, ending)).toBe(`${opening}\n\n…\n\n${ending}`);
        expect(projectAnswerSlices("Hello", " world")).toBe("Hello world");
        expect(projectAnswerSlices("alias ", "merged")).toBe("alias merged");
        expect(projectAnswerSlices("已经写到这里", "[系统提示] 执行失败")).toBe("已经写到这里\n\n[系统提示] 执行失败");
        expect(projectAnswerSlices("已经写到这里", "[系统提示] 失败")).toBe("已经写到这里\n\n[系统提示] 失败");
    });

    it("stitches one emoji clause and does not glue a short opening onto a long answer", () => {
        const shared = "😀".repeat(8);
        const opening = `甲${shared}`;
        expect(projectAnswerSlices(opening, `${shared}结尾`)).toBe(`${opening}结尾`);
        const ending = "乙".repeat(40);
        expect(projectAnswerSlices("好的", ending)).toBe(`好的\n\n…\n\n${ending}`);
    });

    it("keeps one copy when the stream still has the newline the final trimmed off", () => {
        const body = `${"甲".repeat(40)}答案`;
        expect(projectAnswerSlices(`${body}\n`, body)).toBe(body);
        const planning = "计".repeat(20);
        expect(projectAnswerSlices(`${planning}${body}\n`, `${body}结尾`)).toBe(`${planning}${body}结尾`);
    });

    it("prefers the first non-empty string and treats history kinds correctly", () => {
        expect(firstVEStreamText("", "legacy")).toBe("legacy");
        expect(visibleHistoryMessageContent("stream_chunk", "\x01private", "visible")).toBe("");
        expect(visibleHistoryMessageContent("answer", "", "\x01kept answer")).toBe("kept answer");
    });
});
