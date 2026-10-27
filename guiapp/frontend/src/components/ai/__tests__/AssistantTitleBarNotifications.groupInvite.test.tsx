// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AssistantTitleBarNotifications } from "../AssistantTitleBarNotifications";
import { overlayTheme } from "../aiAssistantPanelTheme";

const eventHandlers = new Map<string, (payload: unknown) => void>();

vi.mock("../../../../wailsjs/runtime", () => ({
    BrowserOpenURL: vi.fn(),
    EventsOn: vi.fn((name: string, handler: (payload: unknown) => void) => {
        eventHandlers.set(name, handler);
        return () => eventHandlers.delete(name);
    }),
    EventsOff: vi.fn(),
}));

function emitRuntimeEvent(name: string, payload: unknown) {
    eventHandlers.get(name)?.(payload);
}
const invite = {
    id: "cg_0123456789abcdef0123456789abcdef",
    name: "论文组",
    owner: "owner@example.com",
};

const hubNotice = {
    id: "notice-1",
    title: "系统维护",
    content: "今晚维护窗口。",
    category: "maintenance",
    priority: "normal",
    is_read: false,
    created_at: new Date().toISOString(),
};

const list = vi.fn();
const accept = vi.fn();
const decline = vi.fn();
const markRead = vi.fn();
const markAll = vi.fn();

beforeEach(() => {
    localStorage.clear();
    list.mockReset();
    accept.mockReset();
    decline.mockReset();
    markRead.mockReset();
    markAll.mockReset();
    list.mockResolvedValue([invite]);
    accept.mockResolvedValue(undefined);
    decline.mockResolvedValue(undefined);
    markRead.mockResolvedValue(undefined);
    markAll.mockResolvedValue(undefined);
    (window as unknown as { go: unknown }).go = {
        main: {
            App: {
                GetUnreadNotifications: async () => [hubNotice],
                MarkNotificationRead: (id: string) => markRead(id),
                MarkAllNotificationsRead: () => markAll(),
                ListFileCompanionGroupInvites: () => list(),
                AcceptFileCompanionGroupInvite: (id: string) => accept(id),
                DeclineFileCompanionGroupInvite: (id: string) => decline(id),
            },
        },
    };
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

describe("AssistantTitleBarNotifications group invites", () => {
    it("lists a pending invite in the bell and joins without marking it read", async () => {
        renderBell();
        const bell = await screen.findByRole("button", { name: "2 条未读通知" });
        fireEvent.click(bell);

        const row = await screen.findByTestId(`notification-item-companion-group:${invite.id}`);
        expect(row.textContent).toContain("论文组");
        expect(row.textContent).toContain("打开处理");
        expect(screen.getByText("系统维护")).toBeTruthy();

        fireEvent.click(row);
        expect(await screen.findByTestId("notification-group-invite-accept")).toBeTruthy();
        expect(screen.getByTestId("notification-group-invite-decline")).toBeTruthy();
        expect(screen.getByTestId("notification-group-invite-ignore")).toBeTruthy();
        expect(markRead).not.toHaveBeenCalled();

        fireEvent.click(screen.getByTestId("notification-group-invite-accept"));
        await waitFor(() => expect(accept).toHaveBeenCalledWith(invite.id));
        await waitFor(() => expect(screen.queryByText("入群邀请")).toBeNull());
        expect(screen.getByText("系统维护")).toBeTruthy();
        expect(decline).not.toHaveBeenCalled();
        expect(markRead).not.toHaveBeenCalled();
    });

    it("keeps the invite after 全部标为已读", async () => {
        renderBell();
        fireEvent.click(await screen.findByRole("button", { name: "2 条未读通知" }));
        await screen.findByText("入群邀请");
        fireEvent.click(screen.getByTestId("notification-mark-all-read"));
        await waitFor(() => expect(markAll).toHaveBeenCalled());
        expect(screen.getByText("入群邀请")).toBeTruthy();
        expect(screen.getByRole("button", { name: "1 条未读通知" })).toBeTruthy();
        expect(markRead).not.toHaveBeenCalled();
    });

    it("holds the other invite's buttons while one decision is in flight", async () => {
        const other = { ...invite, id: "cg_abcdef0123456789abcdef0123456789", name: "另一组" };
        list.mockResolvedValue([invite, other]);
        let resolveAccept: (() => void) | undefined;
        accept.mockImplementation(() => new Promise<void>((resolve) => { resolveAccept = resolve; }));
        renderBell();
        fireEvent.click(await screen.findByRole("button", { name: "3 条未读通知" }));
        fireEvent.click(await screen.findByTestId(`notification-item-companion-group:${invite.id}`));
        fireEvent.click(await screen.findByTestId("notification-group-invite-accept"));
        await waitFor(() => expect(accept).toHaveBeenCalledTimes(1));
        expect(screen.getByTestId("notification-group-invite-hold").textContent).toBe("正在处理这条邀请。");
        expect(screen.getByTestId("notification-group-invite-accept").hasAttribute("disabled")).toBe(true);
        fireEvent.click(screen.getByTestId("notification-detail-back"));
        fireEvent.click(await screen.findByTestId(`notification-item-companion-group:${other.id}`));
        const acceptButton = await screen.findByTestId("notification-group-invite-accept");
        expect(acceptButton.hasAttribute("disabled")).toBe(true);
        expect(screen.getByTestId("notification-group-invite-decline").hasAttribute("disabled")).toBe(true);
        expect(screen.getByTestId("notification-group-invite-ignore").hasAttribute("disabled")).toBe(true);
        expect(screen.getByTestId("notification-group-invite-hold").textContent).toBe("正在处理另一条邀请。");
        fireEvent.click(acceptButton);
        fireEvent.click(screen.getByTestId("notification-group-invite-decline"));
        expect(accept).toHaveBeenCalledTimes(1);
        expect(decline).not.toHaveBeenCalled();
        resolveAccept?.();
        await waitFor(() => expect(screen.getByTestId("notification-group-invite-accept").hasAttribute("disabled")).toBe(false));
        expect(screen.queryByTestId("notification-group-invite-hold")).toBeNull();
        expect(screen.queryByText("论文组")).toBeNull();
        expect(screen.getByTestId("notification-detail-content").textContent).toContain("另一组");
    });

    it("keeps a failed decision visible after opening another invite", async () => {
        const other = { ...invite, id: "cg_abcdef0123456789abcdef0123456789", name: "另一组" };
        list.mockResolvedValue([invite, other]);
        let rejectAccept: ((reason: unknown) => void) | undefined;
        accept.mockImplementation(() => new Promise((_resolve, reject) => { rejectAccept = reject; }));
        renderBell();
        fireEvent.click(await screen.findByRole("button", { name: "3 条未读通知" }));
        fireEvent.click(await screen.findByTestId(`notification-item-companion-group:${invite.id}`));
        fireEvent.click(await screen.findByTestId("notification-group-invite-accept"));
        await waitFor(() => expect(accept).toHaveBeenCalledTimes(1));
        fireEvent.click(screen.getByTestId("notification-detail-back"));
        fireEvent.click(await screen.findByTestId(`notification-item-companion-group:${other.id}`));
        await screen.findByTestId("notification-group-invite-hold");
        await act(async () => { rejectAccept?.("没有找到入群邀请"); });
        expect((await screen.findByTestId("notification-group-invite-action-error")).textContent).toBe("「论文组」未处理：没有找到入群邀请");
        expect(screen.queryByTestId("notification-group-invite-hold")).toBeNull();
        expect(screen.getByTestId("notification-group-invite-accept").hasAttribute("disabled")).toBe(false);
        fireEvent.click(screen.getByTestId("notification-detail-back"));
        expect(screen.getByTestId("notification-group-invite-action-error").textContent).toBe("「论文组」未处理：没有找到入群邀请");
        expect(screen.getByTestId(`notification-item-companion-group:${invite.id}`)).toBeTruthy();
    });

    it("shows a failed first invite load instead of an empty inbox", async () => {
        list.mockRejectedValue("暂时无法连接云端");
        renderBell();
        fireEvent.click(await screen.findByRole("button", { name: "1 条未读通知" }));
        expect((await screen.findByTestId("notification-group-invite-list-error")).textContent).toBe("暂时无法连接云端");
        expect(screen.getByText("系统维护")).toBeTruthy();
        expect(screen.queryByText("入群邀请")).toBeNull();
    });

    it("refreshes the invite list when a pushed invitation arrives", async () => {
        list.mockResolvedValue([]);
        renderBell();
        await waitFor(() => expect(list).toHaveBeenCalled());
        expect(screen.queryByTestId(`notification-item-companion-group:${invite.id}`)).toBeNull();

        // A push from the Hub says an invitation was just created. The row must
        // appear without waiting for the 30s poll.
        list.mockResolvedValue([invite]);
        await act(async () => {
            emitRuntimeEvent("notification:new", { source: "companion_group_invite", source_id: `${invite.id}:user-1` });
        });
        // One unread Hub notice plus the invitation that just arrived.
        fireEvent.click(await screen.findByRole("button", { name: "2 条未读通知" }));
        expect(await screen.findByTestId(`notification-item-companion-group:${invite.id}`)).toBeTruthy();
    });

    it("refreshes the invite list when a reconnect pulls an invitation notice", async () => {
        list.mockResolvedValue([]);
        renderBell();
        await waitFor(() => expect(list).toHaveBeenCalled());
        expect(screen.queryByTestId(`notification-item-companion-group:${invite.id}`)).toBeNull();

        // After a reconnect the Hub push is gone; the pulled unread list is what
        // reports the invitation, so the row must appear from that signal.
        list.mockResolvedValue([invite]);
        await act(async () => {
            emitRuntimeEvent("notification:sync", [
                hubNotice,
                { id: "n1", title: "入群邀请", content: "x", category: "custom", priority: "important", is_read: false, created_at: new Date().toISOString(), source: "companion_group_invite", source_id: `${invite.id}:user-1` },
            ]);
        });
        fireEvent.click(await screen.findByRole("button", { name: "2 条未读通知" }));
        expect(await screen.findByTestId(`notification-item-companion-group:${invite.id}`)).toBeTruthy();
        expect(screen.getByText("系统维护")).toBeTruthy();
    });

    it("does not list an invite twice when its delivery notice is also unread", async () => {
        // The Hub sends both the invite row and the notification that carried
        // it. Only the actionable row belongs in the bell.
        const notice = {
            id: "notice-invite",
            title: "入群邀请",
            content: "owner@example.com 邀请你加入阅读群组「论文组」。",
            category: "custom",
            priority: "important",
            is_read: false,
            created_at: new Date().toISOString(),
            source: "companion_group_invite",
            source_id: `${invite.id}:user-1`,
        };
        (window as unknown as { go: { main: { App: Record<string, unknown> } } }).go.main.App.GetUnreadNotifications =
            async () => [notice, hubNotice];
        renderBell();
        fireEvent.click(await screen.findByRole("button", { name: "2 条未读通知" }));
        expect(await screen.findByTestId(`notification-item-companion-group:${invite.id}`)).toBeTruthy();
        expect(screen.queryByTestId("notification-item-notice-invite")).toBeNull();
        expect(screen.getByText("系统维护")).toBeTruthy();
    });
});
