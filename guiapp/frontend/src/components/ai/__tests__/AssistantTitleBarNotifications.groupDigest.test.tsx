// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AssistantTitleBarNotifications } from "../AssistantTitleBarNotifications";
import { overlayTheme } from "../aiAssistantPanelTheme";

vi.mock("../../../../wailsjs/runtime", () => ({
    BrowserOpenURL: vi.fn(),
    EventsOn: vi.fn(() => () => {}),
    EventsOff: vi.fn(),
}));

const groupID = "cg_0123456789abcdef0123456789abcdef";

const digestNotice = {
    id: "digest-notice-1",
    title: "群组文档",
    content: "阅读群组「论文组」在你加入前已有 3 份文档。点「确认」打开伴读的文档管理查看。",
    category: "custom",
    priority: "important",
    is_read: false,
    created_at: new Date().toISOString(),
    source: "companion_group_digest",
    source_id: `${groupID}:user-1`,
};

const openShare = vi.fn();
const openLibrary = vi.fn();
const markRead = vi.fn();
const markAll = vi.fn();

function installApp() {
    (window as unknown as { go: unknown }).go = {
        main: {
            App: {
                GetUnreadNotifications: async () => [digestNotice],
                MarkNotificationRead: (id: string) => markRead(id),
                MarkAllNotificationsRead: () => markAll(),
                ListFileCompanionGroupInvites: async () => [],
                OpenFileCompanionShareFromNotification: (token: string) => openShare(token),
                ShowFileCompanionLibrary: () => openLibrary(),
            },
        },
    };
}

beforeEach(() => {
    localStorage.clear();
    openShare.mockReset();
    openLibrary.mockReset();
    markRead.mockReset();
    markAll.mockReset();
    openShare.mockResolvedValue("C:/library/shared.pdf");
    openLibrary.mockResolvedValue(undefined);
    markRead.mockResolvedValue(undefined);
    markAll.mockResolvedValue(undefined);
    installApp();
});

afterEach(() => {
    delete (window as unknown as { go?: unknown }).go;
    localStorage.clear();
});

function renderBell() {
    return render(
        <AssistantTitleBarNotifications
            active
            inline={false}
            lang="zh"
            theme={overlayTheme}
        />,
    );
}

describe("AssistantTitleBarNotifications group digest", () => {
    it("shows the merged notice once and opens 文档管理 from 确认", async () => {
        renderBell();
        fireEvent.click(await screen.findByRole("button", { name: "1 条未读通知" }));
        fireEvent.click(await screen.findByTestId("notification-item-digest-notice-1"));

        const confirm = await screen.findByTestId("notification-digest-open");
        expect(confirm.textContent).toBe("确认");
        // The digest never gets the single-document 打开 action.
        expect(screen.queryByTestId("notification-share-open")).toBeNull();

        fireEvent.click(confirm);
        await waitFor(() => expect(openLibrary).toHaveBeenCalledTimes(1));
        expect(openShare).not.toHaveBeenCalled();
        await waitFor(() => expect(markRead).toHaveBeenCalledWith("digest-notice-1"));
        await waitFor(() => expect(screen.queryByTestId("notification-panel")).toBeNull());
    });

    it("keeps the notice open and shows the error when the binding is missing", async () => {
        (window as unknown as { go: { main: { App: Record<string, unknown> } } }).go.main.App.ShowFileCompanionLibrary = undefined;
        renderBell();
        fireEvent.click(await screen.findByRole("button", { name: "1 条未读通知" }));
        fireEvent.click(await screen.findByTestId("notification-item-digest-notice-1"));
        fireEvent.click(await screen.findByTestId("notification-digest-open"));

        await waitFor(() => expect(screen.getByTestId("notification-share-error").textContent).not.toBe(""));
        expect(openLibrary).not.toHaveBeenCalled();
        // Selecting the row already marked the notice read; the failed 确认
        // keeps the detail open so the user can retry instead of closing it.
        expect(screen.getByTestId("notification-digest-open")).toBeTruthy();
        expect(screen.getByTestId("notification-panel")).toBeTruthy();
    });
});
