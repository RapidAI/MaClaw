import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { TaskMoreActions } from "../TaskMoreActions";

function renderMenu(overrides: { onPreview?: () => void } = {}) {
    return render(
        <TaskMoreActions
            lang="zh"
            onSave={vi.fn()}
            onClear={vi.fn()}
            onCopyTitle={vi.fn()}
            {...overrides}
        />,
    );
}

describe("TaskMoreActions", () => {
    it("opens the preview area from the menu when a preview handler is provided", () => {
        const onPreview = vi.fn();
        const { getByTestId } = renderMenu({ onPreview });

        fireEvent.click(getByTestId("task-more-preview-btn"));

        expect(onPreview).toHaveBeenCalled();
    });

    it("hides the preview item when preview is unavailable", () => {
        renderMenu();

        expect(screen.queryByTestId("task-more-preview-btn")).toBeNull();
        expect(screen.getByText("保存为任务")).toBeTruthy();
    });
});
