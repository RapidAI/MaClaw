import { describe, expect, it } from "vitest";
import { goalStageCommandFromRound, goalStageHasReport, goalStageProcessLive } from "../goalStagePresentation";

describe("goalStageCommandFromRound", () => {
    it("keeps the step assignment and drops the phase contract", () => {
        const prompt = [
            "[系统续接][execute] 按步骤执行目标：开发一个贪吃蛇游戏",
            "",
            "本轮只能提交步骤 4：实现绘制与界面。已完成的步骤不要再调用 goal。",
            "调用 goal(action=\"step\", index=4, status=\"done\", result=\"...\")。",
        ].join("\n");
        expect(goalStageCommandFromRound(prompt, "目标清单\n→ 9. 别的步骤")).toBe("本轮只能提交步骤 4：实现绘制与界面");
    });

    it("names a planning stage from the objective", () => {
        const prompt = "[系统续接][plan] 先理解并分解目标，不要实现。\n\n目标：实现一个图形界面版愤怒的小鸟\n\n本轮只做规划：\n1. 复述任务。";
        expect(goalStageCommandFromRound(prompt, "")).toBe("分解目标：实现一个图形界面版愤怒的小鸟");
    });

    it("names a plan correction", () => {
        const prompt = "[系统续接][plan] 上一轮没有把分解写入目标。本轮只补交规划，不要实现。\n\n目标：贪吃蛇\n";
        expect(goalStageCommandFromRound(prompt, "")).toBe("补交分解：贪吃蛇");
    });

    it("reads the current checklist step when the prompt has no assignment", () => {
        const display = "目标清单\n✓ 1. 环境\n→ 2. 物理内核\n进度：第 3/50 轮";
        expect(goalStageCommandFromRound("[系统续接][execute] 当前步骤", display)).toBe("本轮只能提交步骤 2：物理内核");
    });

    it("uses a short continuation line when the checklist is not a step list", () => {
        expect(goalStageCommandFromRound(
            "[系统续接] 继续推进目标：实现一个 C++ 贪吃蛇",
            "/goal 继续推进目标：实现一个 C++ 贪吃蛇",
        )).toBe("继续推进目标：实现一个 C++ 贪吃蛇");
    });

    it("caps a long step title", () => {
        const title = "画".repeat(90);
        const command = goalStageCommandFromRound(`[系统续接][execute] 标题\n\n本轮只能提交步骤 4：${title}。`, "");
        expect(Array.from(command).length).toBe(81);
        expect(command.startsWith("本轮只能提交步骤 4：画")).toBe(true);
        expect(command.endsWith("…")).toBe(true);
    });

    it("does not invent a command from the generic continuation placeholder", () => {
        expect(goalStageCommandFromRound("", "目标清单\n正在继续当前步骤")).toBe("");
    });

    it("does not use the goal objective on the execute header as the stage command", () => {
        expect(goalStageCommandFromRound("[系统续接][execute] 按步骤执行目标：贪吃蛇", "")).toBe("");
    });

    it("caps a long one-line continuation", () => {
        const title = "画".repeat(90);
        const command = goalStageCommandFromRound(`[系统续接] 继续推进目标：${title}`, "");
        expect(Array.from(command).length).toBe(81);
        expect(command.startsWith("继续推进目标：画")).toBe(true);
        expect(command.endsWith("…")).toBe(true);
    });

    it("drops a token budget tail from the checklist line it names", () => {
        expect(goalStageCommandFromRound("", "目标清单\n→ 2. 物理内核 | Token: 9/40")).toBe("本轮只能提交步骤 2：物理内核");
        expect(goalStageCommandFromRound("", "目标清单\n→ 2. 物理内核 ｜ Token：9／40")).toBe("本轮只能提交步骤 2：物理内核");
        expect(goalStageCommandFromRound("", "目标清单\n正在分解：贪吃蛇 | Token: 3/100")).toBe("分解目标：贪吃蛇");
        expect(goalStageCommandFromRound("", "目标清单\n正在补交分解：贪吃蛇 | Token: 3/100")).toBe("补交分解：贪吃蛇");
        expect(goalStageCommandFromRound("", "目标清单\n正在核对验收 | Token: 3/100")).toBe("正在核对验收");
    });
});

describe("goalStageHasReport", () => {
    it("requires a goal stage with visible report text", () => {
        expect(goalStageHasReport("goal-cont-1", "第 2 步已完成。")).toBe(true);
        expect(goalStageHasReport("goal-cont-1", "")).toBe(false);
        expect(goalStageHasReport("goal-cont-1", "<!--maclaw-tool:call-1-->")).toBe(false);
        expect(goalStageHasReport("req-1", "第 2 步已完成。")).toBe(false);
    });
});

describe("goalStageProcessLive", () => {
    it("folds a filed goal stage while the session is still busy", () => {
        expect(goalStageProcessLive(true, false, true, true)).toBe(false);
        expect(goalStageProcessLive(true, true, true, true)).toBe(false);
    });

    it("keeps an unfinished stage open and an idle stage folded", () => {
        expect(goalStageProcessLive(true, false, true, false)).toBe(true);
        expect(goalStageProcessLive(true, true, false)).toBe(true);
        expect(goalStageProcessLive(false, true, true)).toBe(false);
        expect(goalStageProcessLive(true, false, false)).toBe(false);
    });
});
