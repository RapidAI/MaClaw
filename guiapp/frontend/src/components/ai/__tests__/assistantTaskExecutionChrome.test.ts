import { describe, expect, it, vi } from "vitest";
import { codingStepsAllPassed, executionSecondaryChromeStyle, handleTaskExecutionHeaderDoubleClick, isTaskExecutionHeaderInteractiveTarget, resolveTaskExecutionStatus } from "../assistantTaskExecutionChrome";

describe("executionSecondaryChromeStyle", () => {
    it("clips leftover title-bar chrome out of hit-testing", () => {
        expect(executionSecondaryChromeStyle.pointerEvents).toBe("none");
        expect(executionSecondaryChromeStyle.overflow).toBe("hidden");
        expect(executionSecondaryChromeStyle.clip).toBe("rect(0, 0, 0, 0)");
    });
});

describe("isTaskExecutionHeaderInteractiveTarget", () => {
    it("treats the header itself as a restore target", () => {
        const header = document.createElement("div");
        expect(isTaskExecutionHeaderInteractiveTarget(header, header)).toBe(false);
    });

    it("ignores buttons, menus, and SVG glyphs inside header controls", () => {
        const header = document.createElement("div");
        const button = document.createElement("button");
        const summary = document.createElement("summary");
        const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
        button.append(svg);
        header.append(button, summary);
        expect(isTaskExecutionHeaderInteractiveTarget(button, header)).toBe(true);
        expect(isTaskExecutionHeaderInteractiveTarget(summary, header)).toBe(true);
        expect(isTaskExecutionHeaderInteractiveTarget(svg, header)).toBe(true);
    });

    it("labels an idle chat with a pending recovery card as interrupted", () => {
        const status = resolveTaskExecutionStatus({
            lang: "zh-Hans",
            raw: "",
            pendingReview: false,
            cancelPending: false,
            busy: false,
            hasOutput: false,
            hasMessages: true,
            workflowActive: false,
            codingStepCount: 0,
            pendingUnfinishedStatus: "interrupted",
        });
        expect(status).toEqual({ label: "已中断", tone: "pending" });
    });

    it("labels a max-round recovery card as unfinished rather than interrupted", () => {
        const status = resolveTaskExecutionStatus({
            lang: "zh-Hans",
            raw: "",
            pendingReview: false,
            cancelPending: false,
            busy: false,
            hasOutput: false,
            hasMessages: true,
            workflowActive: false,
            codingStepCount: 0,
            pendingUnfinishedStatus: "max_rounds_reached",
        });
        expect(status).toEqual({ label: "未完成", tone: "pending" });
    });

    it("still labels an idle chat without a recovery card as completed", () => {
        const status = resolveTaskExecutionStatus({
            lang: "zh-Hans",
            raw: "",
            pendingReview: false,
            cancelPending: false,
            busy: false,
            hasOutput: false,
            hasMessages: true,
            workflowActive: false,
            codingStepCount: 0,
            pendingUnfinishedStatus: "",
        });
        expect(status).toEqual({ label: "已完成", tone: "completed" });
    });

    it("shows a finished coding run as completed when the only leftover is a review word", () => {
        const status = resolveTaskExecutionStatus({
            lang: "zh-Hans",
            raw: "waiting_review passed passed",
            pendingReview: false,
            cancelPending: false,
            busy: false,
            hasOutput: false,
            hasMessages: true,
            workflowActive: false,
            codingStepCount: 2,
            codingStepsAllPassed: true,
            pendingUnfinishedStatus: "",
        });
        expect(status).toEqual({ label: "已完成", tone: "completed" });
    });

    it("keeps a live approval pending after the coding steps passed", () => {
        const status = resolveTaskExecutionStatus({
            lang: "zh-Hans",
            raw: "waiting_review passed",
            pendingReview: true,
            cancelPending: false,
            busy: false,
            hasOutput: false,
            hasMessages: true,
            workflowActive: false,
            codingStepCount: 1,
            codingStepsAllPassed: true,
            pendingUnfinishedStatus: "",
        });
        expect(status).toEqual({ label: "待处理", tone: "pending" });
    });

    it("keeps a real workflow review pending after the coding steps passed", () => {
        const status = resolveTaskExecutionStatus({
            lang: "zh-Hans",
            raw: "waiting_review passed",
            pendingReview: true,
            awaitingUserReview: true,
            cancelPending: false,
            busy: false,
            hasOutput: false,
            hasMessages: true,
            workflowActive: true,
            codingStepCount: 1,
            codingStepsAllPassed: true,
            pendingUnfinishedStatus: "",
        });
        expect(status).toEqual({ label: "待处理", tone: "pending" });
    });

    it("shows a fatal runtime failure instead of a confirmation", () => {
        const status = resolveTaskExecutionStatus({
            lang: "zh-Hans",
            raw: "failed",
            pendingReview: false,
            cancelPending: false,
            busy: false,
            hasOutput: false,
            hasMessages: true,
            workflowActive: false,
            codingStepCount: 1,
            codingStepsAllPassed: false,
            pendingUnfinishedStatus: "",
        });
        expect(status).toEqual({ label: "失败", tone: "failed" });
        expect(codingStepsAllPassed([{ status: "passed" }, { status: "passed" }])).toBe(true);
        expect(codingStepsAllPassed([{ status: "passed" }, { status: "pending" }])).toBe(false);
    });

    it("keeps a paused run paused when a phase follows the status", () => {
        const status = resolveTaskExecutionStatus({
            lang: "zh-Hans",
            raw: "paused implement",
            pendingReview: false,
            cancelPending: false,
            busy: false,
            hasOutput: true,
            hasMessages: true,
            workflowActive: false,
            codingStepCount: 1,
            codingStepsAllPassed: true,
            pendingUnfinishedStatus: "",
        });
        expect(status).toEqual({ label: "已暂停", tone: "pending" });
    });

    it("does not treat a later pause phase as a paused run", () => {
        const status = resolveTaskExecutionStatus({
            lang: "zh-Hans",
            raw: "completed pause",
            pendingReview: false,
            cancelPending: false,
            busy: false,
            hasOutput: true,
            hasMessages: true,
            workflowActive: false,
            codingStepCount: 1,
            codingStepsAllPassed: true,
            pendingUnfinishedStatus: "",
        });
        expect(status).toEqual({ label: "已完成", tone: "completed" });
    });

    it("shows an interrupted run as interrupted after the coding steps passed", () => {
        const status = resolveTaskExecutionStatus({
            lang: "zh-Hans",
            raw: "interrupted passed",
            pendingReview: true,
            cancelPending: false,
            busy: false,
            hasOutput: true,
            hasMessages: true,
            workflowActive: false,
            codingStepCount: 1,
            codingStepsAllPassed: true,
            pendingUnfinishedStatus: "",
        });
        expect(status).toEqual({ label: "已中断", tone: "pending" });
    });

    it("keeps a stop in progress visible after every coding step has passed", () => {
        const status = resolveTaskExecutionStatus({
            lang: "zh-Hans",
            raw: "passed passed",
            pendingReview: false,
            cancelPending: true,
            busy: true,
            hasOutput: true,
            hasMessages: true,
            workflowActive: false,
            codingStepCount: 2,
            codingStepsAllPassed: true,
            pendingUnfinishedStatus: "",
        });
        expect(status).toEqual({ label: "正在停止", tone: "pending" });
    });

    it("treats data-window-no-drag action chrome, including padding, as a control", () => {
        const header = document.createElement("div");
        const actions = document.createElement("div");
        actions.setAttribute("data-window-no-drag", "");
        header.append(actions);
        expect(isTaskExecutionHeaderInteractiveTarget(actions, header)).toBe(true);
    });
});

describe("handleTaskExecutionHeaderDoubleClick", () => {
    it("restores from empty header chrome and suppresses text selection", () => {
        const header = document.createElement("div");
        const onToggleMaximize = vi.fn();
        const preventDefault = vi.fn();
        handleTaskExecutionHeaderDoubleClick({ target: header, currentTarget: header, preventDefault }, onToggleMaximize);
        expect(preventDefault).toHaveBeenCalledTimes(1);
        expect(onToggleMaximize).toHaveBeenCalledTimes(1);
    });

    it("does not restore when the double-click landed on a control", () => {
        const header = document.createElement("div");
        const button = document.createElement("button");
        header.append(button);
        const onToggleMaximize = vi.fn();
        const preventDefault = vi.fn();
        handleTaskExecutionHeaderDoubleClick({ target: button, currentTarget: header, preventDefault }, onToggleMaximize);
        expect(preventDefault).not.toHaveBeenCalled();
        expect(onToggleMaximize).not.toHaveBeenCalled();
    });

    it("does not restore when the double-click landed on action-cluster padding", () => {
        const header = document.createElement("div");
        const actions = document.createElement("div");
        actions.setAttribute("data-window-no-drag", "");
        header.append(actions);
        const onToggleMaximize = vi.fn();
        const preventDefault = vi.fn();
        handleTaskExecutionHeaderDoubleClick({ target: actions, currentTarget: header, preventDefault }, onToggleMaximize);
        expect(preventDefault).not.toHaveBeenCalled();
        expect(onToggleMaximize).not.toHaveBeenCalled();
    });
});
