import { describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { ChatTaskCard } from "../ChatTaskCard";
import type { ChatTaskView } from "../chatTaskModel";

const theme = {
    inputBarBorder: "#ddd", fieldBg: "#fff", text: "#111", textMuted: "#666", bg: "#fff",
} as any;
function interruptedTask(): ChatTaskView {
    return {
        id: "ct-1", sessionKey: "desktop-user", title: "Slides", status: "interrupted",
        question: "which color?", artifactPaths: [], updatedAtUnix: 2,
    };
}

describe("ChatTaskCard recovery", () => {
    it("resumes the same interrupted task with an optional answer", async () => {
        render(<ChatTaskCard task={interruptedTask()} theme={theme} lang="zh" />);
        const input = screen.getByLabelText("补充") as HTMLInputElement;
        fireEvent.change(input, { target: { value: "blue" } });
        const event = new Promise<CustomEvent>(resolve => {
            window.addEventListener("ai-chat-task-action", resolve as EventListener, { once: true });
        });
        fireEvent.click(screen.getByRole("button", { name: "继续任务" }));
        await expect(event).resolves.toMatchObject({ detail: { action: "resume", taskId: "ct-1", text: "blue" } });
        expect(input.value).toBe("");
    });
});

describe("ChatTaskCard rendering", () => {
    it("shows the cleaned title and a human progress line while running", () => {
        const task: ChatTaskView = {
            id: "ct-2", sessionKey: "desktop-user",
            title: "工作目录：C:\\Users\\ma139\\Desktop\\作业。任务两步： 1) 生成 Word 讲稿文档",
            status: "running",
            progress: "工具 · 执行技能 (manage_skill)",
        };
        render(<ChatTaskCard task={task} theme={theme} lang="zh" />);
        expect(screen.getByText("1) 生成 Word 讲稿文档")).toBeTruthy();
        expect(screen.getByText("正在执行技能")).toBeTruthy();
        expect(screen.getByText("manage_skill")).toBeTruthy();
        expect(screen.getByText("进行中")).toBeTruthy();
        expect(screen.queryByText(/工作目录/)).toBeNull();
    });

    it("shows the question and answers into the waiting task", async () => {
        const task: ChatTaskView = {
            id: "ct-3", sessionKey: "desktop-user", title: "配图补进",
            status: "needs_input", question: "用哪一版封面？",
        };
        render(<ChatTaskCard task={task} theme={theme} lang="zh" />);
        expect(screen.getByText("用哪一版封面？")).toBeTruthy();
        expect(screen.getByText("等待你的回答")).toBeTruthy();
        const input = screen.getByLabelText("回答") as HTMLInputElement;
        fireEvent.change(input, { target: { value: "蓝色" } });
        const event = new Promise<CustomEvent>(resolve => {
            window.addEventListener("ai-chat-task-action", resolve as EventListener, { once: true });
        });
        fireEvent.keyDown(input, { key: "Enter" });
        await expect(event).resolves.toMatchObject({ detail: { action: "answer", taskId: "ct-3", text: "蓝色" } });
        expect(input.value).toBe("");
    });

    it("keeps the steer input on running tasks and cancels from the card", async () => {
        const task: ChatTaskView = {
            id: "ct-4", sessionKey: "desktop-user", title: "生成 PPT",
            status: "running", progress: "工具 · 调用工具 (async_wait)",
        };
        render(<ChatTaskCard task={task} theme={theme} lang="zh" />);
        expect(screen.getByText("async_wait")).toBeTruthy();
        const input = screen.getByLabelText("补充") as HTMLInputElement;
        fireEvent.change(input, { target: { value: "加目录页" } });
        const event = new Promise<CustomEvent>(resolve => {
            window.addEventListener("ai-chat-task-action", resolve as EventListener, { once: true });
        });
        fireEvent.keyDown(input, { key: "Enter" });
        await expect(event).resolves.toMatchObject({ detail: { action: "steer", taskId: "ct-4", text: "加目录页" } });
        const cancel = new Promise<CustomEvent>(resolve => {
            window.addEventListener("ai-chat-task-action", resolve as EventListener, { once: true });
        });
        fireEvent.click(screen.getByRole("button", { name: "取消" }));
        await expect(cancel).resolves.toMatchObject({ detail: { action: "cancel", taskId: "ct-4" } });
    });
});
