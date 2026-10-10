import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { TaskMoreActions } from "../TaskMoreActions";

function renderMenu(overrides: { onPreview?: () => void; onRecordSkill?: () => void; recordSkillDisabled?: boolean; recordSkillPending?: boolean } = {}) {
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

    it("starts skill recording from the menu", () => {
        const onRecordSkill = vi.fn();
        renderMenu({ onRecordSkill });

        fireEvent.click(screen.getByTestId("task-more-record-skill-btn"));

        expect(onRecordSkill).toHaveBeenCalledTimes(1);
        expect(screen.getByTestId("task-more-record-skill-btn").textContent).toBe("录制 Skill");
    });

    it("blocks skill recording while another task is recording", () => {
        const onRecordSkill = vi.fn();
        renderMenu({ onRecordSkill, recordSkillDisabled: true });

        const item = screen.getByTestId("task-more-record-skill-btn");
        expect(item.textContent).toBe("其他任务正在录制");
        fireEvent.click(item);
        expect(onRecordSkill).not.toHaveBeenCalled();
    });

    it("does not put the file companion label on the task header", () => {
        renderMenu();

        expect(screen.queryByTestId("task-open-file-companion")).toBeNull();
        expect(screen.queryByText("用伴读打开")).toBeNull();
    });

    it("holds the menu item while a recording is still being saved", () => {
        const onRecordSkill = vi.fn();
        renderMenu({ onRecordSkill, recordSkillPending: true });

        const item = screen.getByTestId("task-more-record-skill-btn") as HTMLButtonElement;
        expect(item.textContent).toBe("请稍候");
        expect(item.disabled).toBe(true);
        fireEvent.click(item);
        expect(onRecordSkill).not.toHaveBeenCalled();
    });
});
