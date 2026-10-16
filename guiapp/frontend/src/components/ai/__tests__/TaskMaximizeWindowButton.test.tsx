import { fireEvent, render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { TaskMaximizeWindowButton } from "../TaskMaximizeWindowButton";

describe("TaskMaximizeWindowButton", () => {
    it("shows restore copy and restores from the maximized execution header", () => {
        const onToggleMaximize = vi.fn();
        const { getByTestId } = render(
            <TaskMaximizeWindowButton lang="zh-Hans" maximized onToggleMaximize={onToggleMaximize} />,
        );

        const btn = getByTestId("task-maximize-toggle");
        expect(btn.getAttribute("title")).toBe("还原窗口");
        expect(btn.getAttribute("aria-label")).toBe("还原窗口");
        expect(btn.getAttribute("aria-pressed")).toBe("true");
        expect(btn.querySelector("svg rect")).toBeTruthy();
        fireEvent.click(btn);
        expect(onToggleMaximize).toHaveBeenCalledTimes(1);
    });

    it("keeps the maximize glyph inset so the stroke is not clipped", () => {
        const { getByTestId } = render(
            <TaskMaximizeWindowButton lang="en" maximized={false} onToggleMaximize={vi.fn()} />,
        );

        const rect = getByTestId("task-maximize-toggle").querySelector("svg rect");
        expect(rect).toBeTruthy();
        expect(Number(rect?.getAttribute("x"))).toBeGreaterThan(1);
        expect(Number(rect?.getAttribute("y"))).toBeGreaterThan(1);
    });

    it("does not toggle twice when the restore control is double-clicked", () => {
        const onToggleMaximize = vi.fn();
        const { getByTestId } = render(
            <TaskMaximizeWindowButton lang="en" maximized onToggleMaximize={onToggleMaximize} />,
        );

        const btn = getByTestId("task-maximize-toggle");
        fireEvent.click(btn, { detail: 1 });
        fireEvent.click(btn, { detail: 2 });
        expect(onToggleMaximize).toHaveBeenCalledTimes(1);
        expect(btn.getAttribute("title")).toBe("Restore window");
    });
});
