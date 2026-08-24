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
