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
