import { act, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { AITab } from "../AITabTypes";
import { VEConversationTitleBar } from "../VEConversationTitleBar";

const listeners = new Map<string, (data: unknown) => void>();

vi.mock("../../../../wailsjs/runtime", () => ({
    EventsOn: (name: string, handler: (data: unknown) => void) => {
        listeners.set(name, handler);
        return () => listeners.delete(name);
    },
    EventsOff: vi.fn(),
}));

const employee: AITab = { id: "ve-tab", type: "ve", title: "安妮", veId: "ve_emp_1", closable: true };

describe("VEConversationTitleBar", () => {
    it("follows a live offline event instead of staying on the initial status", () => {
        render(<VEConversationTitleBar tab={{ ...employee, onlineStatus: "online" }} lang="zh-CN" />);
        expect(screen.getByRole("heading", { name: "安妮" }).textContent).toBe("安妮");
        expect(screen.getByRole("status").textContent).toContain("在线");

        act(() => {
            listeners.get("ve:status_change")?.({ ve_id: "ve_emp_1", status: "offline" });
        });

        expect(screen.getByRole("status").textContent).toContain("离线");
    });

    it("shows a participant count for a group instead of a completed badge", () => {
        const group: AITab = {
            id: "group-tab",
            type: "group",
            title: "天气",
            veId: "ve_emp_1",
            participants: ["m_local", "ve_emp_1"],
            closable: true,
        };
        render(<VEConversationTitleBar tab={group} lang="zh-CN" participantCount={2} />);
        const status = screen.getByRole("status");
        expect(status.textContent).toContain("2 位参与者");
        expect(status.className).not.toContain("mc-task-execution-status--completed");
    });
});
