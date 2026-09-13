import { fireEvent, render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { TaskHideWindowButton } from "../TaskHideWindowButton";

describe("TaskHideWindowButton", () => {
    it("hides from a primary pointer press", () => {
        const onHideWindow = vi.fn();
        const { getByTestId } = render(<TaskHideWindowButton lang="zh-Hans" onHideWindow={onHideWindow} />);
        const btn = getByTestId("task-hide-window");
        expect(btn.getAttribute("title")).toBe("隐藏窗口");
        fireEvent.mouseDown(btn, { button: 0 });
        expect(onHideWindow).toHaveBeenCalledTimes(1);
    });

    it("hides from keyboard activation without double-firing after a pointer click", () => {
        const onHideWindow = vi.fn();
        const { getByTestId } = render(<TaskHideWindowButton lang="en" onHideWindow={onHideWindow} />);
        const btn = getByTestId("task-hide-window");
        fireEvent.click(btn, { detail: 0 });
        expect(onHideWindow).toHaveBeenCalledTimes(1);
        fireEvent.click(btn, { detail: 1 });
        expect(onHideWindow).toHaveBeenCalledTimes(1);
        expect(btn.getAttribute("aria-label")).toBe("Hide window");
    });
});
