import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ConversationViewArchiveCount, LoadAIAssistantUIArchive, LoadProjectTabConversationArchive } from "../../../wailsjs/go/main/App";
import type { Theme } from "./aiAssistantPanelTheme";
import type { ChatMessage } from "./useAIAssistant";
import { useConversationViewArchive } from "./useConversationViewArchive";

vi.mock("../../../wailsjs/go/main/App", () => ({
    ConversationViewArchiveCount: vi.fn().mockResolvedValue(0),
    LoadAIAssistantUIArchive: vi.fn().mockResolvedValue([]),
    LoadProjectTabConversationArchive: vi.fn().mockResolvedValue([]),
}));

const theme = { titleBarBorder: "#ccc", fieldBg: "#fff", textMuted: "#666", bg: "#fff", text: "#111" } as Theme;

function row(id: string, extra: Partial<ChatMessage> = {}): ChatMessage {
    return { id, role: "user", content: id, timestamp: 1, ...extra };
}

function Probe({ messages, scope = "desktop" }: { messages: ChatMessage[]; scope?: string }) {
    const view = useConversationViewArchive({ messages, scope, enabled: true, lang: "zh-Hans", theme });
    return (
        <div>
            <div data-testid="visible-count">{view.visibleMessages.length}</div>
            <div data-testid="earlier-count">{view.earlierCount}</div>
            {view.banner}
            {view.dialog}
        </div>
    );
}

describe("useConversationViewArchive", () => {
    beforeEach(() => {
        vi.mocked(ConversationViewArchiveCount).mockReset().mockResolvedValue(0);
        vi.mocked(LoadAIAssistantUIArchive).mockReset().mockResolvedValue([]);
        vi.mocked(LoadProjectTabConversationArchive).mockReset().mockResolvedValue([]);
    });

    it("mounts only the hot tail and offers the earlier rows", async () => {
        const messages = Array.from({ length: 45 }, (_, index) => row(`m${index}`, { content: `body ${index}` }));
        render(<Probe messages={messages} />);
        expect(screen.getByTestId("visible-count").textContent).toBe("40");
        expect(screen.getByTestId("earlier-count").textContent).toBe("5");
        expect(screen.getByTestId("conversation-archive-banner").textContent).toContain("查看更早的 5 条对话");
        await waitFor(() => expect(ConversationViewArchiveCount).toHaveBeenCalledWith("desktop"));
    });

    it("uses the disk archive count when it is larger than the in-memory prefix", async () => {
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(12);
        render(<Probe messages={[row("m1")]} />);
        await waitFor(() => expect(screen.getByTestId("conversation-archive-banner").textContent).toContain("12"));
    });

    it("opens a plain-text viewer and skips rows already on screen", async () => {
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(2);
        vi.mocked(LoadAIAssistantUIArchive).mockResolvedValue([
            { id: "old", role: "assistant", content: "archived body", sessionKey: "desktop-user" },
            { id: "m1", role: "user", content: "already visible", sessionKey: "" },
            { id: "proj", role: "user", content: "project row", sessionKey: "desktop-user:D:/tasks/one" },
        ] as never);
        render(<Probe messages={[row("m1", { content: "already visible" })]} />);
        fireEvent.click(await screen.findByTestId("conversation-archive-banner"));
        expect(await screen.findByText("archived body")).toBeTruthy();
        expect(screen.queryByText("project row")).toBeNull();
        expect(screen.getAllByTestId("conversation-archive-message")).toHaveLength(1);
        fireEvent.keyDown(window, { key: "Escape" });
        await waitFor(() => expect(screen.queryByTestId("conversation-archive-dialog")).toBeNull());
    });

    it("loads a project tab archive by tab id", async () => {
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(1);
        vi.mocked(LoadProjectTabConversationArchive).mockResolvedValue([
            { id: "tab-old", role: "user", content: "tab archive" },
        ] as never);
        render(<Probe messages={[row("hot")]} scope="tab:proj-1" />);
        fireEvent.click(await screen.findByTestId("conversation-archive-banner"));
        expect(await screen.findByText("tab archive")).toBeTruthy();
        expect(LoadProjectTabConversationArchive).toHaveBeenCalledWith("proj-1");
    });
});
