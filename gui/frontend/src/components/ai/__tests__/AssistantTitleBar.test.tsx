import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { AssistantTitleBar } from '../AssistantTitleBar';
import { overlayTheme } from '../aiAssistantPanelTheme';

const notificationState = vi.hoisted(() => ({
    current: {
        notifications: [] as Array<{
            id: string;
            title: string;
            content: string;
            category: 'system_announcement' | 'security_alert';
            priority: 'important' | 'normal' | 'urgent';
            is_read: boolean;
            created_at: string;
        }>,
        unreadCount: 0,
        panelOpen: false,
        categoryFilter: null as string | null,
        togglePanel: vi.fn(() => {
            notificationState.current.panelOpen = !notificationState.current.panelOpen;
        }),
        setCategoryFilter: vi.fn(),
        markRead: vi.fn(),
        markAllRead: vi.fn(),
        urgentToast: null as null | Record<string, unknown>,
        dismissUrgentToast: vi.fn(() => {
            notificationState.current.urgentToast = null;
        }),
    },
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    LoadConfig: vi.fn(),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    BrowserOpenURL: vi.fn(),
    EventsOn: vi.fn(() => vi.fn()),
    EventsOff: vi.fn(),
}));

vi.mock('../useNotifications', () => ({
    useNotifications: () => notificationState.current,
}));

const renderTitleBar = () => render(
    <AssistantTitleBar
        clearHistory={vi.fn()}
        inline={false}
        lang="zh"
        maximized={false}
        onClose={vi.fn()}
        projectSearchOpen={false}
        refreshNews={vi.fn()}
        showMaximizeToggle={false}
        theme={overlayTheme}
        themeMode="light"
        title="AI 助手"
        trialReflectEnabled={false}
        toggleProjectSearch={vi.fn()}
    />,
);

describe('AssistantTitleBar', () => {
    beforeEach(() => {
        notificationState.current.notifications = [];
        notificationState.current.unreadCount = 0;
        notificationState.current.panelOpen = false;
        notificationState.current.urgentToast = null;
        notificationState.current.markRead.mockClear();
        notificationState.current.togglePanel.mockClear();
        notificationState.current.dismissUrgentToast.mockClear();
    });

    it('exposes mobile documents as a title-bar icon near tools', () => {
        renderTitleBar();
        const btn = screen.getByTestId('mobile-docs-titlebar-btn');
        expect(btn.getAttribute('aria-label')).toContain('Mobile');
    });

    it('no longer hosts global settings controls (moved to the quick settings bar)', () => {
        renderTitleBar();
        expect(screen.queryByTestId('workflow-toggle-btn')).toBeNull();
    });

    it('opens the full notification text from the list', () => {
        const notice = {
            id: 'invite-1',
            title: '邀请奖励开启，邀请新用户注册，奖励500积分，被邀请人完成注册后双方均可领取',
            content: '即日起邀请新用户注册可获得 500 积分。被邀请人完成注册后，双方均可领取奖励。',
            category: 'system_announcement' as const,
            priority: 'important' as const,
            is_read: false,
            created_at: new Date().toISOString(),
        };
        notificationState.current.notifications = [notice];
        notificationState.current.unreadCount = 1;
        notificationState.current.panelOpen = true;

        renderTitleBar();
        fireEvent.click(screen.getByTestId('notification-item-invite-1'));

        expect(notificationState.current.markRead).toHaveBeenCalledWith('invite-1');
        expect(screen.getByTestId('notification-detail')).toBeTruthy();
        expect(screen.getByTestId('notification-detail-content').textContent).toContain('双方均可领取奖励');

        fireEvent.click(screen.getByTestId('notification-detail-back'));
        expect(screen.queryByTestId('notification-detail')).toBeNull();
        expect(screen.getByTestId('notification-list')).toBeTruthy();
    });

    it('opens full text from the urgent toast', () => {
        const notice = {
            id: 'urgent-1',
            title: 'Security window',
            content: 'Please rotate **tokens** immediately.',
            category: 'security_alert' as const,
            priority: 'urgent' as const,
            is_read: false,
            created_at: new Date().toISOString(),
        };
        notificationState.current.urgentToast = notice;
        notificationState.current.notifications = [notice];
        notificationState.current.unreadCount = 1;

        renderTitleBar();
        const toast = screen.getByTestId('notification-urgent-toast');
        expect(toast.getAttribute('role')).toBe('status');
        fireEvent.click(screen.getByTestId('notification-urgent-toast-open'));

        expect(notificationState.current.markRead).toHaveBeenCalledWith('urgent-1');
        expect(notificationState.current.togglePanel).toHaveBeenCalled();
        expect(notificationState.current.dismissUrgentToast).toHaveBeenCalled();
        expect(screen.getByTestId('notification-detail-content').textContent).toContain('tokens');
    });

    it('hides the urgent toast while the inbox is open', () => {
        notificationState.current.urgentToast = {
            id: 'urgent-3',
            title: 'Rotate now',
            content: 'Tokens expire tonight.',
            category: 'security_alert',
            priority: 'urgent',
            is_read: false,
            created_at: new Date().toISOString(),
        };
        notificationState.current.panelOpen = true;

        renderTitleBar();
        expect(screen.queryByTestId('notification-urgent-toast')).toBeNull();
    });

    it('dismisses the urgent toast with Escape when the panel is closed', () => {
        notificationState.current.urgentToast = {
            id: 'urgent-2',
            title: 'Rotate now',
            content: 'Tokens expire tonight.',
            category: 'security_alert',
            priority: 'urgent',
            is_read: false,
            created_at: new Date().toISOString(),
        };

        renderTitleBar();
        fireEvent.keyDown(document, { key: 'Escape' });
        expect(notificationState.current.dismissUrgentToast).toHaveBeenCalled();
    });
});
