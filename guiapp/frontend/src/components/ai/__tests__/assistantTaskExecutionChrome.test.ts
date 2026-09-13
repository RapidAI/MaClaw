import { describe, expect, it, vi } from "vitest";
import { executionSecondaryChromeStyle, handleTaskExecutionHeaderDoubleClick, isTaskExecutionHeaderInteractiveTarget } from "../assistantTaskExecutionChrome";

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
