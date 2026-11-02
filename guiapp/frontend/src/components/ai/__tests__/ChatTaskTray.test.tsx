import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { ChatTaskTray } from "../ChatTaskTray";
import type { ChatTaskView } from "../chatTaskModel";

const theme = {
    btnColor: "#2563eb", divider: "#eee", fieldBg: "#fff", inputBarBorder: "#ddd",
    text: "#111", textMuted: "#666", bg: "#fff",
} as any;

const running: ChatTaskView = {
    id: "ct-9", sessionKey: "desktop-user",
    title: "工作目录：C:\\a。1) 生成 PPT", status: "running",
};

describe("ChatTaskTray", () => {
    it("lists open tasks with a cleaned title and cancels via the chip button", () => {
        const onCancel = vi.fn();
        render(<ChatTaskTray tasks={[running]} canDispatch={false} lang="zh" theme={theme} onRunInBackground={() => {}} onCancel={onCancel} />);
        expect(screen.getByText("1 项还在做")).toBeTruthy();
        expect(screen.getByText("1) 生成 PPT")).toBeTruthy();
        fireEvent.click(screen.getByRole("button", { name: "取消" }));
        expect(onCancel).toHaveBeenCalledWith("ct-9");
    });

    it("shows the hint and an enabled run-in-background button with no open tasks", () => {
        render(<ChatTaskTray tasks={[]} canDispatch={true} lang="zh" theme={theme} onRunInBackground={() => {}} onCancel={vi.fn()} />);
        expect(screen.getByText("比较长的事先做着，这里可以继续聊")).toBeTruthy();
        expect((screen.getByTestId("run-in-background") as HTMLButtonElement).disabled).toBe(false);
    });

    it("disables run-in-background without input and renders nothing when idle", () => {
        const { container } = render(<ChatTaskTray tasks={[]} canDispatch={false} lang="zh" theme={theme} onRunInBackground={() => {}} onCancel={vi.fn()} />);
        expect(container.childElementCount).toBe(0);
        render(<ChatTaskTray tasks={[running]} canDispatch={false} lang="zh" theme={theme} onRunInBackground={() => {}} onCancel={vi.fn()} />);
        expect((screen.getByTestId("run-in-background") as HTMLButtonElement).disabled).toBe(true);
    });
});
