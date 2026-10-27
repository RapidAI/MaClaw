import { describe, expect, it, vi } from "vitest";
import { chatTaskInSession, codingStepsAllPassed, executionSecondaryChromeStyle, handleTaskExecutionHeaderDoubleClick, isTaskExecutionHeaderInteractiveTarget, openChatWorkForSession, resolveTaskExecutionStatus } from "../assistantTaskExecutionChrome";

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

    it("keeps a live run in progress when a recovery card left an interrupted marker", () => {
        const status = resolveTaskExecutionStatus({
            lang: "zh-Hans",
            raw: "interrupted passed",
            pendingReview: false,
            cancelPending: false,
            busy: true,
            hasOutput: true,
            hasMessages: true,
            workflowActive: false,
            codingStepCount: 1,
            codingStepsAllPassed: false,
            pendingUnfinishedStatus: "interrupted",
        });
        // The thought is still streaming; the marker belongs to the turn the
        // user resumed, so the header must not say 已中断.
        expect(status).toEqual({ label: "进行中", tone: "running" });
    });

    it("keeps a live run in progress over a paused or failed snapshot", () => {
        const base = {
            lang: "zh-Hans",
            pendingReview: false,
            cancelPending: false,
            busy: true,
            hasOutput: true,
            hasMessages: true,
            workflowActive: false,
            codingStepCount: 1,
            codingStepsAllPassed: false,
            pendingUnfinishedStatus: "",
        };
        for (const raw of ["paused implement", "failed", "cancelled"]) {
            const status = resolveTaskExecutionStatus({ ...base, raw });
            expect(status).toEqual({ label: "进行中", tone: "running" });
        }
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

    it("does not label a hard-exited chat with messages as completed", () => {
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
            turnIncomplete: true,
        });
        expect(status).toEqual({ label: "未完成", tone: "pending" });
    });

    it("keeps a running hard-exit turn in progress", () => {
        const status = resolveTaskExecutionStatus({
            lang: "zh-Hans",
            raw: "",
            pendingReview: false,
            cancelPending: false,
            busy: true,
            hasOutput: false,
            hasMessages: true,
            workflowActive: false,
            codingStepCount: 0,
            pendingUnfinishedStatus: "",
            turnIncomplete: true,
        });
        expect(status).toEqual({ label: "进行中", tone: "running" });
    });

    it("treats slash and backslash session keys as the same running task", () => {
        const tasks = [{
            sessionKey: "desktop-user:C:\\Users\\ma139\\.maclaw\\data\\tasks\\pdf-1",
            status: "running",
        }];
        const active = "desktop-user:C:/Users/ma139/.maclaw/data/tasks/pdf-1";
        expect(chatTaskInSession(tasks[0].sessionKey, active)).toBe(true);
        expect(openChatWorkForSession(tasks, active)).toBe("running");
        expect(openChatWorkForSession([{ sessionKey: tasks[0].sessionKey, status: "done" }], active)).toBe("");
        expect(openChatWorkForSession([{ sessionKey: "desktop-user:C:/other", status: "running" }], active)).toBe("");
        expect(chatTaskInSession(tasks[0].sessionKey + "/", active)).toBe(true);
        expect(chatTaskInSession(tasks[0].sessionKey + "\\.", active)).toBe(true);
        expect(chatTaskInSession("desktop-user:C:/Users/ma139/.maclaw/data/tasks/foo/../pdf-1", active)).toBe(true);
        expect(chatTaskInSession("desktop-user:C:/Users/ma139/.maclaw/data /tasks/pdf-1", active)).toBe(true);
        expect(chatTaskInSession("desktop-user:expert:ab\\cd", "desktop-user:expert:ab/cd")).toBe(false);
        expect(chatTaskInSession("desktop-user:expert:ab/cd", "desktop-user:expert:ab/cd")).toBe(true);
    });

    it("stays in progress while a background chat task is still running", () => {
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
            openChatWork: "running",
        });
        expect(status).toEqual({ label: "进行中", tone: "running" });
    });

    it("waits when the background chat task needs an answer", () => {
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
            openChatWork: "needs_input",
        });
        expect(status).toEqual({ label: "待处理", tone: "pending" });
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

    it("stays in progress when a running chat task outlives a failed snapshot", () => {
        const base = {
            lang: "zh-Hans",
            pendingReview: false,
            cancelPending: false,
            busy: false,
            hasOutput: false,
            hasMessages: true,
            workflowActive: false,
            codingStepCount: 1,
            codingStepsAllPassed: false,
            pendingUnfinishedStatus: "",
            openChatWork: "running" as const,
        };
        for (const raw of ["failed", "cancelled", "interrupted passed"]) {
            const status = resolveTaskExecutionStatus({ ...base, raw });
            expect(status).toEqual({ label: "进行中", tone: "running" });
        }
    });

    it("waits on the user when a chat task needs input and the snapshot still says failed", () => {
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
            pendingUnfinishedStatus: "",
            openChatWork: "needs_input",
        });
        expect(status).toEqual({ label: "待处理", tone: "pending" });
    });

    it("keeps a failed step ahead of a paused snapshot when no chat task is open", () => {
        const status = resolveTaskExecutionStatus({
            lang: "zh-Hans",
            raw: "paused failed",
            pendingReview: false,
            cancelPending: false,
            busy: false,
            hasOutput: true,
            hasMessages: true,
            workflowActive: false,
            codingStepCount: 1,
            pendingUnfinishedStatus: "",
        });
        expect(status).toEqual({ label: "失败", tone: "failed" });
    });

    it("shows stopping ahead of a running chat task and a failed snapshot", () => {
        const status = resolveTaskExecutionStatus({
            lang: "zh-Hans",
            raw: "failed",
            pendingReview: false,
            cancelPending: true,
            busy: true,
            hasOutput: false,
            hasMessages: true,
            workflowActive: false,
            codingStepCount: 1,
            pendingUnfinishedStatus: "",
            openChatWork: "running",
        });
        expect(status).toEqual({ label: "正在停止", tone: "pending" });
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
