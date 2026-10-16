// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ScheduledTasksPanel } from "../ScheduledTasksPanel";
import { ListRunWindowTasks, ListScheduledTasks } from "../../../../wailsjs/go/main/App";

vi.mock("../../../../wailsjs/go/main/App", () => ({
    ListScheduledTasks: vi.fn().mockResolvedValue([]),
    ListRunWindowTasks: vi.fn().mockResolvedValue([]),
    CreateScheduledTask: vi.fn(),
    UpdateScheduledTask: vi.fn(),
    UpdateRunWindowTaskTimes: vi.fn(),
    DeleteScheduledTask: vi.fn(),
    DeleteRunWindowTask: vi.fn(),
    PauseScheduledTask: vi.fn(),
    ResumeScheduledTask: vi.fn(),
    TriggerScheduledTask: vi.fn(),
    ListLansengerGroups: vi.fn().mockResolvedValue({ groups: [] }),
}));

vi.mock("../../../../wailsjs/runtime", () => ({
    EventsOn: vi.fn(),
    EventsOff: vi.fn(),
}));

describe("scheduled task row actions", () => {
    it("uses icon buttons for edit and delete", async () => {
        vi.mocked(ListRunWindowTasks).mockResolvedValue([{
            id: "rw-1",
            name: "北京天气，生成pdf",
            prompt: "北京天气",
            initialMessage: "北京天气",
            draft: {},
            status: "running",
            startAt: "2026-10-13T05:51:00",
            createdAt: "2026-10-13T05:00:00",
        }] as never);
        vi.mocked(ListScheduledTasks).mockResolvedValue([{
            id: "st-1",
            name: "每日摘要",
            action: "总结",
            hour: 9,
            minute: 0,
            day_of_week: -1,
            day_of_month: 0,
            interval_minutes: 0,
            start_date: "",
            end_date: "",
            task_type: "chat",
            status: "active",
            created_at: "2026-10-13T00:00:00Z",
            last_run_at: null,
            next_run_at: null,
            run_count: 0,
            last_result: "",
            last_error: "",
        }] as never);

        render(<ScheduledTasksPanel lang="zh-Hans" />);

        const editWindow = await screen.findByRole("button", { name: "编辑开始和结束时间：北京天气，生成pdf" });
        const editTask = screen.getByRole("button", { name: "编辑：每日摘要" });
        const deleteWindow = screen.getByRole("button", { name: "删除：北京天气，生成pdf" });
        const deleteTask = screen.getByRole("button", { name: "删除：每日摘要" });
        for (const button of [editWindow, editTask, deleteWindow, deleteTask]) {
            expect(button.querySelector("svg")).toBeTruthy();
            expect(button.textContent).toBe("");
        }
        expect(screen.queryByText("EDIT")).toBeNull();
        expect(screen.queryByText("DEL")).toBeNull();

        fireEvent.mouseEnter(deleteWindow);
        expect(deleteWindow.style.background).not.toBe("none");
        fireEvent.click(editWindow);
        expect(await screen.findByRole("dialog", { name: "编辑运行时段" })).toBeTruthy();
        fireEvent.click(deleteWindow);
        expect(await screen.findByText("确定删除这个计划任务？正在执行的会停止。")).toBeTruthy();
    });
});
