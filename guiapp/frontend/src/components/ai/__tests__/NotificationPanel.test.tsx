import type { ComponentProps } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { BrowserOpenURL } from "../../../../wailsjs/runtime";
import { NotificationPanel, type AdminNotification } from "../NotificationPanel";
import { overlayTheme } from "../aiAssistantPanelTheme";

vi.mock("../../../../wailsjs/runtime", () => ({
    BrowserOpenURL: vi.fn(),
    EventsOn: vi.fn(() => vi.fn()),
    EventsOff: vi.fn(),
}));

const panelTheme = {
    bg: overlayTheme.titleBarBg,
    text: overlayTheme.titleText,
    textMuted: overlayTheme.textMuted,
    headingColor: overlayTheme.headingColor,
    divider: overlayTheme.titleBarBorder,
    inputBarBg: overlayTheme.fieldBg,
    inputBarBorder: overlayTheme.titleBarBorder,
};

const longNotice: AdminNotification = {
    id: "invite-1",
    title: "Invite rewards are open, invite new users to register, reward 500 points",
    content: "From today, inviting a new user awards **500 points**. After the invitee finishes registration, both sides receive the reward.",
    category: "system_announcement",
    priority: "important",
    is_read: false,
    created_at: new Date().toISOString(),
};

const renderPanel = (
    overrides: Partial<ComponentProps<typeof NotificationPanel>> = {},
) =>
    render(
        <NotificationPanel
            notifications={[longNotice]}
            categoryFilter={null}
            onCategoryChange={vi.fn()}
            onMarkAllRead={vi.fn()}
            onSelectNotification={vi.fn()}
            onClose={vi.fn()}
            lang="zh"
            theme={panelTheme}
            {...overrides}
        />,
    );

describe("NotificationPanel", () => {
    beforeEach(() => {
        vi.mocked(BrowserOpenURL).mockClear();
    });

    it("shows a content preview and a view-full-notice hint in the list", () => {
        renderPanel();

        expect(screen.getByTestId("notification-item-invite-1")).toBeTruthy();
        expect(screen.getByTestId("notification-item-preview-invite-1").textContent).toContain("500 points");
        expect(screen.getByText("查看全文")).toBeTruthy();
        expect(screen.queryByTestId("notification-detail")).toBeNull();
    });

    it("opens the full markdown detail when a notification is selected", () => {
        const onBack = vi.fn();
        renderPanel({
            selectedNotification: longNotice,
            onBackFromDetail: onBack,
            detailTheme: overlayTheme,
        });

        expect(screen.getByTestId("notification-detail")).toBeTruthy();
        expect(screen.getByTestId("notification-detail-content").textContent).toContain("both sides receive the reward");
        expect(screen.getByText(longNotice.title)).toBeTruthy();
        expect(screen.queryByTestId("notification-list")).toBeNull();

        fireEvent.click(screen.getByTestId("notification-detail-back"));
        expect(onBack).toHaveBeenCalled();
    });

    it("returns keyboard focus to the selected row after back", () => {
        const { rerender } = renderPanel({ selectedNotification: longNotice });
        expect(document.activeElement?.getAttribute("data-testid")).toBe("notification-detail-back");

        rerender(
            <NotificationPanel
                notifications={[longNotice]}
                categoryFilter={null}
                onCategoryChange={vi.fn()}
                onMarkAllRead={vi.fn()}
                onSelectNotification={vi.fn()}
                onClose={vi.fn()}
                lang="zh"
                theme={panelTheme}
            />,
        );

        expect(document.activeElement?.getAttribute("data-testid")).toBe("notification-item-invite-1");
    });

    it("selects a notification from the list", () => {
        const onSelect = vi.fn();
        renderPanel({ onSelectNotification: onSelect });

        fireEvent.click(screen.getByTestId("notification-item-invite-1"));
        expect(onSelect).toHaveBeenCalledWith(longNotice);
    });

    it("opens detail without a dedicated detailTheme", () => {
        renderPanel({ selectedNotification: longNotice });

        expect(screen.getByTestId("notification-detail")).toBeTruthy();
        expect(screen.getByTestId("notification-detail-content").textContent).toContain("both sides receive the reward");
    });

    it("renders a notice with no title without crashing", () => {
        const untitled = {
            ...longNotice,
            id: "empty-title",
            title: undefined as unknown as string,
            content: "Body only.",
        };
        renderPanel({ notifications: [untitled] });
        expect(screen.getByTestId("notification-item-empty-title").textContent).toContain("Body only.");
    });

    it("renders unknown categories without crashing", () => {
        const oddNotice = {
            ...longNotice,
            id: "odd-1",
            category: "not-a-real-category" as AdminNotification["category"],
        };
        renderPanel({ notifications: [oddNotice], lang: "en" });

        expect(screen.getByTestId("notification-item-odd-1").textContent).toContain("Custom");
    });

    it("goes back from detail on Escape and closes the list on a second Escape", () => {
        const onBack = vi.fn();
        const onClose = vi.fn();
        const { rerender } = renderPanel({
            selectedNotification: longNotice,
            onBackFromDetail: onBack,
            onClose,
            detailTheme: overlayTheme,
        });

        fireEvent.keyDown(document, { key: "Escape" });
        expect(onBack).toHaveBeenCalledTimes(1);
        expect(onClose).not.toHaveBeenCalled();

        rerender(
            <NotificationPanel
                notifications={[longNotice]}
                categoryFilter={null}
                onCategoryChange={vi.fn()}
                onMarkAllRead={vi.fn()}
                onSelectNotification={vi.fn()}
                onClose={onClose}
                lang="zh"
                theme={panelTheme}
            />,
        );

        fireEvent.keyDown(document, { key: "Escape" });
        expect(onClose).toHaveBeenCalledTimes(1);
    });

    it("avoids viewport units that break scaled windows", () => {
        renderPanel();
        const panel = screen.getByTestId("notification-panel") as HTMLElement;
        expect(`${panel.style.width} ${panel.style.maxHeight} ${panel.style.height}`).not.toMatch(/v[wh]\b/i);
    });

    it("explains an empty category filter", () => {
        renderPanel({
            notifications: [longNotice],
            categoryFilter: "feature_update",
        });
        expect(screen.getByTestId("notification-empty-state").textContent).toContain("该分类没有通知");
    });

    it("keeps the category empty sentence when the invite list failed", () => {
        renderPanel({
            notifications: [longNotice],
            categoryFilter: "feature_update",
            groupInviteListError: "暂时无法连接云端",
        });
        expect(screen.getByTestId("notification-group-invite-list-error").textContent).toBe("暂时无法连接云端");
        expect(screen.getByTestId("notification-empty-state").textContent).toContain("该分类没有通知");
        expect(screen.getByTestId("notification-empty-state").textContent).not.toContain("暂时无法连接云端");
    });

    it("does not repeat a title-only notice as markdown body", () => {
        const titleOnly = {
            ...longNotice,
            content: longNotice.title,
        };
        renderPanel({ selectedNotification: titleOnly });
        expect(screen.getByText(titleOnly.title)).toBeTruthy();
        expect(screen.queryByTestId("notification-detail-content")).toBeNull();
    });

    it("opens announcement links through the desktop runtime", () => {
        renderPanel({
            selectedNotification: {
                ...longNotice,
                content: "Read the [rules](https://example.com/rules) before inviting.",
            },
        });

        fireEvent.click(screen.getByText("rules"));
        expect(vi.mocked(BrowserOpenURL)).toHaveBeenCalledWith("https://example.com/rules");
    });

    it("does not open unsafe announcement links", () => {
        renderPanel({
            selectedNotification: {
                ...longNotice,
                content: "Ignore [this](javascript:alert(1)) link.",
            },
        });

        fireEvent.click(screen.getByText("this"));
        expect(vi.mocked(BrowserOpenURL)).not.toHaveBeenCalled();
    });

    it("does not fetch remote images from announcement markdown", () => {
        const { container } = renderPanel({
            selectedNotification: {
                ...longNotice,
                content: "See ![tracker](https://evil.example/pixel.png) now.",
            },
        });

        expect(container.querySelector("img")).toBeNull();
        expect(screen.getByTestId("notification-detail-content").textContent).toContain("tracker");
    });

    it("shows a reading-group invite as a row that can be accepted, declined, or ignored", () => {
        const groupId = "cg_0123456789abcdef0123456789abcdef";
        const inviteNotice: AdminNotification = {
            id: `companion-group:${groupId}`,
            groupInviteId: groupId,
            title: "入群邀请",
            content: "owner@example.com 邀请你加入阅读群组「论文组」。同意后，可以在伴读的文档管理里看到群组文章。",
            category: "custom",
            priority: "important",
            is_read: false,
            created_at: new Date().toISOString(),
        };
        const onAction = vi.fn();
        const { rerender } = renderPanel({
            notifications: [inviteNotice, longNotice],
            onGroupInviteAction: onAction,
        });

        const row = screen.getByTestId(`notification-item-${inviteNotice.id}`);
        expect(row.textContent).toContain("入群邀请");
        expect(row.textContent).toContain("打开处理");
        expect(row.textContent).not.toContain("查看全文");
        expect(screen.getByTestId("notification-item-invite-1").textContent).toContain("查看全文");

        rerender(
            <NotificationPanel
                notifications={[inviteNotice, longNotice]}
                categoryFilter={null}
                onCategoryChange={vi.fn()}
                onMarkAllRead={vi.fn()}
                onSelectNotification={vi.fn()}
                onClose={vi.fn()}
                selectedNotification={inviteNotice}
                onBackFromDetail={vi.fn()}
                onGroupInviteAction={onAction}
                detailTheme={overlayTheme}
                lang="zh"
                theme={panelTheme}
            />,
        );

        fireEvent.click(screen.getByTestId("notification-group-invite-accept"));
        fireEvent.click(screen.getByTestId("notification-group-invite-decline"));
        fireEvent.click(screen.getByTestId("notification-group-invite-ignore"));
        expect(onAction).toHaveBeenNthCalledWith(1, groupId, "accept");
        expect(onAction).toHaveBeenNthCalledWith(2, groupId, "decline");
        expect(onAction).toHaveBeenNthCalledWith(3, groupId, "ignore");
        expect(screen.getByTestId("notification-group-invite-accept").className).toBe("ndet-invite-accept");

        rerender(
            <NotificationPanel
                notifications={[inviteNotice]}
                categoryFilter={null}
                onCategoryChange={vi.fn()}
                onMarkAllRead={vi.fn()}
                onSelectNotification={vi.fn()}
                onClose={vi.fn()}
                selectedNotification={inviteNotice}
                onGroupInviteAction={onAction}
                groupInviteBusy
                groupInviteError="没有找到入群邀请"
                lang="en"
                theme={panelTheme}
            />,
        );
        expect(screen.getByRole("button", { name: "Accept" }).hasAttribute("disabled")).toBe(true);
        expect(screen.getByRole("button", { name: "Decline" }).hasAttribute("disabled")).toBe(true);
        expect(screen.getByRole("button", { name: "Ignore" }).hasAttribute("disabled")).toBe(true);
        expect(screen.getByTestId("notification-group-invite-error").textContent).toBe("没有找到入群邀请");
    });

    it("keeps a group invite visible in other categories and does not treat it as markdown", () => {
        const groupId = "cg_abcdef0123456789abcdef0123456789";
        const inviteNotice: AdminNotification = {
            id: `companion-group:${groupId}`,
            groupInviteId: groupId,
            title: "入群邀请",
            content: "owner 邀请你加入阅读群组「[领取](https://evil.example/x)」。",
            category: "custom",
            priority: "important",
            is_read: false,
            created_at: new Date().toISOString(),
        };
        const { rerender } = renderPanel({
            notifications: [inviteNotice],
        });
        expect(screen.getByTestId("notification-mark-all-read").hasAttribute("disabled")).toBe(true);
        const row = screen.getByTestId(`notification-item-${inviteNotice.id}`);
        expect(row.textContent).toContain("[领取](https://evil.example/x)");

        rerender(
            <NotificationPanel
                notifications={[inviteNotice, longNotice]}
                categoryFilter="security_alert"
                onCategoryChange={vi.fn()}
                onMarkAllRead={vi.fn()}
                onSelectNotification={vi.fn()}
                onClose={vi.fn()}
                onGroupInviteAction={vi.fn()}
                lang="zh"
                theme={panelTheme}
            />,
        );
        expect(screen.getByText("入群邀请")).toBeTruthy();
        expect(screen.queryByTestId("notification-item-invite-1")).toBeNull();
        expect(screen.queryByTestId("notification-empty-state")).toBeNull();

        rerender(
            <NotificationPanel
                notifications={[inviteNotice]}
                categoryFilter={null}
                onCategoryChange={vi.fn()}
                onMarkAllRead={vi.fn()}
                onSelectNotification={vi.fn()}
                onClose={vi.fn()}
                selectedNotification={inviteNotice}
                onGroupInviteAction={vi.fn()}
                detailTheme={overlayTheme}
                lang="zh"
                theme={panelTheme}
            />,
        );
        const body = screen.getByTestId("notification-detail-content");
        expect(body.querySelector("a")).toBeNull();
        expect(body.textContent).toContain("https://evil.example/x");
        expect(body.textContent).toContain("[领取]");
    });

    it("keeps invite buttons off ordinary notices and off other categories", () => {
        const inviteNotice: AdminNotification = {
            id: "companion-group:cg_abcdef0123456789abcdef0123456789",
            groupInviteId: "cg_abcdef0123456789abcdef0123456789",
            title: "入群邀请",
            content: "一位用户 邀请你加入阅读群组「另一组」。",
            category: "custom",
            priority: "important",
            is_read: false,
            created_at: new Date().toISOString(),
        };
        renderPanel({
            notifications: [inviteNotice, longNotice],
            selectedNotification: longNotice,
            onGroupInviteAction: vi.fn(),
            groupInviteError: "不应出现",
            groupInviteActionError: "不应出现在这条通知",
        });
        expect(screen.queryByTestId("notification-group-invite-accept")).toBeNull();
        expect(screen.queryByTestId("notification-group-invite-error")).toBeNull();
        expect(screen.queryByTestId("notification-group-invite-action-error")).toBeNull();

        renderPanel({
            notifications: [inviteNotice, longNotice],
            categoryFilter: "custom",
        });
        expect(screen.getByText("入群邀请")).toBeTruthy();
        expect(screen.queryByTestId("notification-item-invite-1")).toBeNull();
    });

    it("shows a failed invite load without hiding other notices", () => {
        const view = renderPanel({
            notifications: [],
            groupInviteListError: "暂时无法连接云端",
        });
        expect(screen.getByTestId("notification-group-invite-list-error").textContent).toBe("暂时无法连接云端");
        expect(screen.getByTestId("notification-empty-state").textContent).not.toContain("没有通知");
        view.unmount();

        renderPanel({
            groupInviteListError: "暂时无法连接云端",
        });
        expect(screen.getByTestId("notification-group-invite-list-error").textContent).toBe("暂时无法连接云端");
        expect(screen.getByText("Invite rewards are open, invite new users to register, reward 500 points")).toBeTruthy();
    });

    it("disables invite actions and explains the hold while another decision is busy", () => {
        const groupId = "cg_abcdef0123456789abcdef0123456789";
        const inviteNotice: AdminNotification = {
            id: `companion-group:${groupId}`,
            groupInviteId: groupId,
            title: "入群邀请",
            content: "一位用户 邀请你加入阅读群组「另一组」。",
            category: "custom",
            priority: "important",
            is_read: false,
            created_at: new Date().toISOString(),
        };
        renderPanel({
            notifications: [inviteNotice],
            selectedNotification: inviteNotice,
            onGroupInviteAction: vi.fn(),
            groupInviteBusy: true,
            groupInviteHold: "正在处理另一条邀请。",
            detailTheme: overlayTheme,
        });
        expect(screen.getByTestId("notification-group-invite-accept").hasAttribute("disabled")).toBe(true);
        expect(screen.getByTestId("notification-group-invite-decline").getAttribute("aria-busy")).toBe("true");
        expect(screen.getByTestId("notification-group-invite-hold").textContent).toBe("正在处理另一条邀请。");
        expect(screen.queryByTestId("notification-group-invite-action-error")).toBeNull();
    });

    it("shows another invitation's failed decision on the list and on the open invite", () => {
        const groupId = "cg_abcdef0123456789abcdef0123456789";
        const inviteNotice: AdminNotification = {
            id: `companion-group:${groupId}`,
            groupInviteId: groupId,
            title: "入群邀请",
            content: "一位用户 邀请你加入阅读群组「另一组」。",
            category: "custom",
            priority: "important",
            is_read: false,
            created_at: new Date().toISOString(),
        };
        const failed = "「论文组」未处理：没有找到入群邀请";
        const view = renderPanel({
            notifications: [inviteNotice, longNotice],
            groupInviteActionError: failed,
        });
        expect(screen.getByTestId("notification-group-invite-action-error").textContent).toBe(failed);
        expect(screen.getByText("入群邀请")).toBeTruthy();
        view.unmount();

        renderPanel({
            notifications: [inviteNotice],
            selectedNotification: inviteNotice,
            onGroupInviteAction: vi.fn(),
            groupInviteActionError: failed,
            detailTheme: overlayTheme,
        });
        expect(screen.getByTestId("notification-group-invite-action-error").textContent).toBe(failed);
        expect(screen.queryByTestId("notification-group-invite-error")).toBeNull();
    });

    it("does not close when the urgent toast is pressed", () => {
        const onClose = vi.fn();
        renderPanel({ onClose });

        const toast = document.createElement("div");
        toast.setAttribute("data-notification-toast", "true");
        document.body.appendChild(toast);
        fireEvent.mouseDown(toast);
        expect(onClose).not.toHaveBeenCalled();
        toast.remove();
    });
});
