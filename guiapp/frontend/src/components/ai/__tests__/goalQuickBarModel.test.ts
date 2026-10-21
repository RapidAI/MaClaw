import { describe, expect, it } from "vitest";
import { formatGoalElapsed, goalBarView } from "../goalQuickBarModel";

const base = {
    visible: true,
    goal_id: "g1",
    objective: "以现有性能为基础，提升20%",
    turns_used: 2,
    max_turns: 50,
};

describe("goalBarView", () => {
    it("shows planning while the goal is still being split", () => {
        const view = goalBarView({ ...base, status: "active", phase: "plan" });
        expect(view.visible).toBe(true);
        expect(view.running).toBe(true);
        expect(view.label).toBe("planning");
        expect(view.turns).toBe("2/50");
    });

    it("shows continue after every non-success stop", () => {
        expect(goalBarView({ ...base, status: "paused", summary: "Auto-paused: no tool calls" }).label).toBe("paused-notool");
        expect(goalBarView({ ...base, status: "paused", summary: "SSH 会话还没有连上" }).label).toBe("paused-remote");
        const remoteReason = "这个任务的执行环境是 dev.example 的 /src/app。SSH 会话还没有连上，请在远程连接面板重新连接后再继续。";
        const remote = goalBarView({ ...base, status: "paused", summary: remoteReason });
        expect(remote.label).toBe("paused-remote");
        expect(remote.reason).toBe(remoteReason);
        expect(goalBarView({ ...base, status: "budget_limited" }).label).toBe("budget");
        expect(goalBarView({ ...base, status: "failed", summary: "编译失败" }).reason).toBe("编译失败");
        expect(goalBarView({ ...base, status: "active", awaiting_user: true }).running).toBe(false);
        expect(goalBarView({ ...base, status: "active", awaiting_user: true }).label).toBe("waiting");
        for (const status of ["paused", "budget_limited", "failed"]) {
            expect(goalBarView({ ...base, status }).running).toBe(false);
        }
    });

    it("formats elapsed time like the Codex goal bar", () => {
        const start = 1_700_000_000;
        expect(formatGoalElapsed(start, (start + 8) * 1000)).toBe("8s");
        expect(formatGoalElapsed(start, (start + 12 * 60 + 4) * 1000)).toBe("12m 4s");
        expect(formatGoalElapsed(start, (start + 3 * 3600 + 33 * 60 + 35) * 1000)).toBe("3h 33m 35s");
        expect(formatGoalElapsed(0, Date.now())).toBe("");
    });

    it("hides the bar after success, delete, or an empty record", () => {
        expect(goalBarView({ ...base, status: "complete" }).visible).toBe(false);
        expect(goalBarView({ ...base, status: "cleared", visible: false }).visible).toBe(false);
        expect(goalBarView(null).visible).toBe(false);
    });
});
