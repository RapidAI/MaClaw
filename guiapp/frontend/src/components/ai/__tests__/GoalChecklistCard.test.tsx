// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
    GoalChecklistCard,
    goalChecklistBodyFromText,
    goalChecklistDoneCount,
} from "../GoalChecklistCard";
import { renderMessage } from "../aiAssistantMarkdown";
import { lightTheme } from "../aiAssistantPanelTheme";

describe("goalChecklistBodyFromText", () => {
    it("parses a published checklist with marks, turns and tokens", () => {
        const body = goalChecklistBodyFromText(
            "目标清单\n✓ 1. 环境与工具链探测\n→ 2. 技术选型与架构设计\n○ 3. 工程骨架与主循环\n进度：第 5/50 轮 | Token: 1234/90000",
        );
        expect(body).not.toBeNull();
        expect(body!.title).toBe("目标清单");
        expect(body!.steps.map((step) => `${step.index}${step.mark}`)).toEqual(["1done", "2progress", "3pending"]);
        expect(body!.turns).toEqual({ current: 5, max: 50 });
        expect(body!.tokens).toEqual({ used: 1234, budget: 90000 });
        expect(body!.activity).toBe("");
        expect(body!.status).toBe("active");
        expect(goalChecklistDoneCount(body!.steps)).toBe(1);
    });

    it("parses a collapsed history snapshot where only the current step is marked", () => {
        const body = goalChecklistBodyFromText("目标清单\n→ 1. 工程骨架\n2. 物理内核");
        expect(body).not.toBeNull();
        expect(body!.steps.map((step) => `${step.index}${step.mark}`)).toEqual(["1progress", "2pending"]);
    });

    it("parses a compressed single-step continuation", () => {
        const body = goalChecklistBodyFromText("目标清单\n→ 2. 技术选型与架构设计");
        expect(body).not.toBeNull();
        expect(body!.steps).toEqual([{ index: 2, title: "技术选型与架构设计", mark: "progress" }]);
    });

    it("parses the decomposing activity with the objective", () => {
        const body = goalChecklistBodyFromText("目标清单\n正在分解：实现一个登录系统");
        expect(body).not.toBeNull();
        expect(body!.steps).toEqual([]);
        expect(body!.activity).toBe("正在分解：实现一个登录系统");
        expect(body!.turns).toBeNull();
    });

    it("keeps 核对验收 alongside the step list", () => {
        const body = goalChecklistBodyFromText("目标清单\n✓ 1. 搭建主循环\n正在核对验收");
        expect(body).not.toBeNull();
        expect(body!.steps).toHaveLength(1);
        expect(body!.activity).toBe("正在核对验收");
    });

    it("parses terminal statuses with their reason", () => {
        const failed = goalChecklistBodyFromText("目标清单\n→ 3. 工程骨架\n目标失败：构建错误未解决\n进度：第 4/50 轮");
        expect(failed!.status).toBe("failed");
        expect(failed!.statusReason).toBe("构建错误未解决");
        const paused = goalChecklistBodyFromText("目标清单\n目标已暂停：等待用户确认");
        expect(paused!.status).toBe("paused");
        expect(paused!.statusReason).toBe("等待用户确认");
        const complete = goalChecklistBodyFromText("目标清单\n✓ 1. 环境\n✓ 2. 交付\n已完成");
        expect(complete!.status).toBe("complete");
    });

    it("parses the English host variant", () => {
        const body = goalChecklistBodyFromText("Goal checklist\nContinuing: step 3");
        expect(body).not.toBeNull();
        expect(body!.activity).toBe("Continuing: step 3");
        expect(body!.turns).toBeNull();
    });

    it("returns null for arbitrary stored text", () => {
        expect(goalChecklistBodyFromText("随便一段历史文本")).toBeNull();
        expect(goalChecklistBodyFromText("")).toBeNull();
    });

    it("splits a token tail off the step, turn and status lines", () => {
        // No turn cap: the record appends the tail to whatever line is last.
        const step = goalChecklistBodyFromText("目标清单\n✓ 1. 环境与工具链探测\n○ 2. 构建验证与交付 | Token: 123/9000");
        expect(step!.steps[1].title).toBe("构建验证与交付");
        expect(step!.tokens).toEqual({ used: 123, budget: 9000 });
        const turns = goalChecklistBodyFromText("目标清单\n→ 1. 环境\n进度：第 5/50 轮 | Token: 61/9000");
        expect(turns!.turns).toEqual({ current: 5, max: 50 });
        expect(turns!.tokens).toEqual({ used: 61, budget: 9000 });
        const status = goalChecklistBodyFromText("目标清单\n✓ 1. 环境\n已完成 | Token: 5/100");
        expect(status!.status).toBe("complete");
        expect(status!.tokens).toEqual({ used: 5, budget: 100 });
        const failed = goalChecklistBodyFromText("目标清单\n目标失败：构建错误未解决 | Token: 7/100");
        expect(failed!.status).toBe("failed");
        expect(failed!.statusReason).toBe("构建错误未解决");
        expect(failed!.tokens).toEqual({ used: 7, budget: 100 });
    });
});

describe("GoalChecklistCard rendering", () => {
    it("renders steps with statuses, progress and meta chips", () => {
        const body = goalChecklistBodyFromText(
            "目标清单\n✓ 1. 环境与工具链探测\n→ 2. 技术选型与架构设计\n○ 3. 工程骨架与主循环\n进度：第 5/50 轮 | Token: 1234/90000",
        )!;
        render(<GoalChecklistCard lang="zh-cn" body={body} />);
        expect(screen.getByText("目标清单")).toBeTruthy();
        // The count chip + highlighted current row carry the running state here.
        expect(screen.queryByText("进行中")).toBeNull();
        expect(screen.getByText("1/3 完成")).toBeTruthy();
        expect(screen.getByText("2.")).toBeTruthy();
        expect(screen.getByText("技术选型与架构设计")).toBeTruthy();
        expect(screen.getByText("第 5/50 轮")).toBeTruthy();
        expect(screen.getByText("Token 1234/90000")).toBeTruthy();
        const progressRow = screen.getByText("技术选型与架构设计").closest("li");
        expect(progressRow?.getAttribute("data-mark")).toBe("progress");
        expect(progressRow?.getAttribute("aria-current")).toBe("step");
    });

    it("marks a failed goal with a reason line", () => {
        const body = goalChecklistBodyFromText("目标清单\n✓ 1. 环境\n→ 2. 骨架\n目标失败：构建错误未解决")!;
        render(<GoalChecklistCard lang="zh-cn" body={body} />);
        expect(screen.getByText("已失败")).toBeTruthy();
        expect(screen.getByText("构建错误未解决")).toBeTruthy();
    });

    it("keeps budget stops neutral amber instead of danger red", () => {
        const body = goalChecklistBodyFromText("目标清单\n→ 2. 骨架\n目标已停止：达到轮次上限")!;
        render(<GoalChecklistCard lang="zh-cn" body={body} />);
        const chip = screen.getByText("已停止").closest(".gkc-chip");
        expect(chip?.className).toContain("gkc-chip--hold");
        expect(chip?.className).not.toContain("gkc-chip--fail");
        expect(screen.getByText("达到轮次上限")).toBeTruthy();
    });

    it("shows a solo activity row while decomposing", () => {
        const body = goalChecklistBodyFromText("目标清单\n正在分解：实现一个登录系统")!;
        render(<GoalChecklistCard lang="zh-cn" body={body} />);
        expect(screen.getByText("正在分解：实现一个登录系统")).toBeTruthy();
        expect(screen.getByText("进行中")).toBeTruthy();
        expect(screen.queryByText("1.")).toBeNull();
    });

    it("shows the running chip on the compressed single-step snapshot", () => {
        const body = goalChecklistBodyFromText("目标清单\n→ 2. 技术选型与架构设计")!;
        render(<GoalChecklistCard lang="zh-cn" body={body} />);
        expect(screen.getByText("进行中")).toBeTruthy();
        expect(screen.getByText("2.")).toBeTruthy();
    });
});

describe("GoalChecklistCard via renderMessage", () => {
    it("renders the structured card for a goalChecklist system message", () => {
        render(<div>{renderMessage({
            id: "goal-1",
            role: "system",
            kind: "goalChecklist",
            content: "目标清单\n✓ 1. 环境与工具链探测\n→ 2. 技术选型与架构设计\n○ 3. 工程骨架与主循环\n进度：第 5/50 轮",
            timestamp: 1,
        }, vi.fn(), lightTheme, false, "", "zh")}</div>);
        expect(screen.getByTestId("assistant-goal-checklist-goal-1")).toBeTruthy();
        expect(screen.getByText("1/3 完成")).toBeTruthy();
        expect(screen.getByText("第 5/50 轮")).toBeTruthy();
        // The compact glyph dump line never shows on the structured card.
        expect(screen.queryByText(/✓ 1\. 环境与工具链探测/)).toBeNull();
    });

    it("keeps the legacy text card for a body the parser rejects", () => {
        render(<div>{renderMessage({
            id: "goal-2",
            role: "system",
            kind: "goalChecklist",
            content: "任意旧文本内容",
            timestamp: 1,
        }, vi.fn(), lightTheme, false, "", "zh")}</div>);
        expect(screen.getByTestId("assistant-goal-checklist-goal-2")).toBeTruthy();
        expect(screen.getByText("目标")).toBeTruthy();
        expect(screen.getByText(/任意旧文本内容/)).toBeTruthy();
    });
});
