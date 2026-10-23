import { describe, expect, it } from "vitest";
import { formatGoalElapsed, goalBarView, goalElapsedEndMs, goalElapsedFrozen, goalElapsedText } from "../goalQuickBarModel";

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
        const waiting = goalBarView({ ...base, status: "active", awaiting_user: true });
        expect(waiting.running).toBe(true);
        expect(waiting.label).toBe("waiting");
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

    it("freezes elapsed time when the goal pauses, fails, or hits its budget", () => {
        const start = 1_700_000_000;
        const stopped = start + 65;
        const later = (start + 2 * 3600) * 1000;
        const failed = { ...base, status: "failed", created_at: start, stopped_at: stopped };
        const budget = { ...base, status: "budget_limited", created_at: start, stopped_at: stopped };
        const paused = { ...base, status: "paused", created_at: start, stopped_at: stopped };
        expect(goalElapsedFrozen(failed)).toBe(true);
        expect(goalElapsedFrozen(budget)).toBe(true);
        expect(goalElapsedFrozen(paused)).toBe(true);
        expect(goalElapsedEndMs(failed, later)).toBe(stopped * 1000);
        expect(goalElapsedEndMs(paused, later)).toBe(stopped * 1000);
        expect(formatGoalElapsed(start, goalElapsedEndMs(failed, later))).toBe("1m 5s");
        expect(formatGoalElapsed(start, goalElapsedEndMs(budget, later))).toBe("1m 5s");
        expect(goalElapsedText(paused, later)).toBe("1m 5s");
        const pausedHeld = {
            ...paused,
            stopped_at: start + 3600 + 65,
            clock_hold_seconds: 3600,
        };
        expect(goalElapsedText(pausedHeld, later)).toBe("1m 5s");

        const running = { ...base, status: "active", created_at: start };
        expect(goalElapsedFrozen(running)).toBe(false);
        expect(goalElapsedEndMs(running, later)).toBe(later);
        expect(formatGoalElapsed(start, goalElapsedEndMs({ ...failed, stopped_at: 0 }, later))).toBe("0s");

        const resumed = { ...base, status: "active", created_at: start, clock_hold_seconds: 3600 };
        const resumedAt = (start + 3600 + 65) * 1000;
        expect(goalElapsedText(resumed, resumedAt)).toBe("1m 5s");
        const failedAgain = {
            ...base,
            status: "failed",
            created_at: start,
            stopped_at: start + 3600 + 65,
            clock_hold_seconds: 3600,
        };
        expect(goalElapsedText(failedAgain, later)).toBe("1m 5s");
    });

    it("hides the bar after success, delete, or an empty record", () => {
        expect(goalBarView({ ...base, status: "complete" }).visible).toBe(false);
        expect(goalBarView({ ...base, status: "cleared", visible: false }).visible).toBe(false);
        expect(goalBarView(null).visible).toBe(false);
    });
});
