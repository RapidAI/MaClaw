import type { ComponentProps } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { BrowserOpenURL } from "../../../../wailsjs/runtime";
import { measureNotificationPanel, NotificationPanel, type AdminNotification } from "../NotificationPanel";

vi.mock("../../../../wailsjs/runtime", () => ({
    BrowserOpenURL: vi.fn(),
    EventsOn: vi.fn(() => vi.fn()),
    EventsOff: vi.fn(),
}));

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

    it("offers 接受/拒绝 for a pending host transfer notice", () => {
        const token = "cd_0123456789abcdef0123456789abcdef";
        const transferNotice: AdminNotification = {
            id: "host-transfer-1",
            title: "主持人转移",
            content: "admin@example.com 希望你接任文档《论文.pdf》的主持人。",
            category: "custom",
            priority: "important",
            is_read: false,
            created_at: new Date().toISOString(),
            source: "companion_host_transfer",
            source_id: token,
        };
        const onTransfer = vi.fn();
        renderPanel({
            notifications: [transferNotice],
            selectedNotification: transferNotice,
            onHostTransfer: onTransfer,
        });
        fireEvent.click(screen.getByTestId("notification-host-transfer-accept"));
        fireEvent.click(screen.getByTestId("notification-host-transfer-decline"));
        expect(onTransfer).toHaveBeenNthCalledWith(1, token, "accept");
        expect(onTransfer).toHaveBeenNthCalledWith(2, token, "decline");
        expect(screen.queryByTestId("notification-share-open")).toBeNull();
    });

    it("asks the nominee to open a host transfer, and leaves a status notice without actions", () => {
        const token = "cd_0123456789abcdef0123456789abcdef";
        const transferNotice: AdminNotification = {
            id: "host-transfer-list",
            title: "主持人转移",
            content: "admin@example.com 希望你接任文档《论文.pdf》的主持人。",
            category: "custom",
            priority: "important",
            is_read: false,
            created_at: new Date().toISOString(),
            source: "companion_host_transfer",
            source_id: token,
        };
        const statusNotice: AdminNotification = {
            id: "host-status-1",
            title: "主持人变更",
            content: "admin@example.com 已收回《论文.pdf》的主持权，管理员重新担任主持人。",
            category: "custom",
            priority: "important",
            is_read: false,
            created_at: new Date().toISOString(),
            source: "companion_host_transfer_notice",
            source_id: token,
        };
        const { rerender } = renderPanel({ notifications: [transferNotice, statusNotice] });
        expect(screen.getByTestId("notification-item-host-transfer-list").textContent).toContain("打开处理");
        expect(screen.getByTestId("notification-item-host-status-1").textContent).toContain("查看全文");
        expect(screen.getByTestId("notification-item-host-status-1").textContent).not.toContain("打开处理");

        rerender(
            <NotificationPanel
                notifications={[statusNotice]}
                categoryFilter={null}
                onCategoryChange={vi.fn()}
                onMarkAllRead={vi.fn()}
                onSelectNotification={vi.fn()}
                onClose={vi.fn()}
                selectedNotification={statusNotice}
                onHostTransfer={vi.fn()}
                lang="zh"
            />,
        );
        expect(screen.getByTestId("notification-detail-content").textContent).toContain("已收回");
        expect(screen.queryByTestId("notification-host-transfer-accept")).toBeNull();
    });

    it("offers 打开 for a shared document notice", () => {
        const token = "cd_0123456789abcdef0123456789abcdef";
        const shareNotice: AdminNotification = {
            id: "share-1",
            title: "分享文档",
            content: "owner@example.com 向你分享了文档《论文.pdf》。在伴读通知里点「打开」即可查看。",
            category: "custom",
            priority: "important",
            is_read: false,
            created_at: new Date().toISOString(),
            source: "companion_share",
            source_id: token,
        };
        const onShareOpen = vi.fn();
        const { rerender } = renderPanel({
            notifications: [shareNotice],
            selectedNotification: shareNotice,
            onShareOpen,
        });
        expect(screen.getByTestId("notification-share-open").textContent).toBe("打开");
        fireEvent.click(screen.getByTestId("notification-share-open"));
        expect(onShareOpen).toHaveBeenCalledWith(token);
        expect(screen.queryByTestId("notification-share-error")).toBeNull();

        rerender(
            <NotificationPanel
                notifications={[shareNotice]}
                categoryFilter={null}
                onCategoryChange={vi.fn()}
                onMarkAllRead={vi.fn()}
                onSelectNotification={vi.fn()}
                onClose={vi.fn()}
                selectedNotification={shareNotice}
                onShareOpen={onShareOpen}
                shareOpenError="打不开这份分享，请稍后重试。"
                lang="zh"
            />,
        );
        expect(screen.getByTestId("notification-share-error").textContent).toContain("打不开这份分享");
    });

    it("opens the full markdown detail when a notification is selected", () => {
        const onBack = vi.fn();
        renderPanel({
            selectedNotification: longNotice,
            onBackFromDetail: onBack,
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

    it("opens detail without a dedicated theme", () => {
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
                lang="zh"
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
                lang="zh"
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

describe("notification panel sizing", () => {
    const base = {
        renderedTop: 40,
        renderedRight: 900,
        clipLeft: 0,
        clipBottom: 800,
        uiScale: 1,
        preferredHeight: 460,
    };

    it("keeps the panel at 400px when the host canvas has room", () => {
        expect(measureNotificationPanel(base)).toEqual({ width: 400, maxHeight: 460 });
    });

    it("budgets the width from the host's left edge, not the bell anchor", () => {
        // The old percentage width resolved against the ~24px anchor and
        // collapsed the panel. The usable span now comes from the fixed right
        // edge back to the host canvas.
        expect(measureNotificationPanel({ ...base, renderedRight: 320 }).width).toBe(304);
    });

    it("converts rendered pixels back to layout pixels under UI zoom", () => {
        const size = measureNotificationPanel({
            ...base,
            renderedTop: 50,
            renderedRight: 500,
            clipBottom: 1000,
            uiScale: 1.25,
        });
        expect(size.width).toBe(384); // 500 / 1.25 - 16
        expect(size.maxHeight).toBe(460); // 950 / 1.25 - 8 still leaves room
    });

    it("caps the height to the space below the title bar", () => {
        expect(measureNotificationPanel({ ...base, clipBottom: 400 }).maxHeight).toBe(352);
    });

    it("falls back to sane defaults before layout exists", () => {
        expect(measureNotificationPanel({ ...base, renderedRight: 0 }).width).toBe(400);
        // A missing/zero --ui-scale must not divide by zero.
        expect(measureNotificationPanel({ ...base, uiScale: 0, clipBottom: 100 }).maxHeight).toBe(52);
    });
});
