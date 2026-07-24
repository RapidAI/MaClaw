import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { AssistantTitleBar } from '../AssistantTitleBar';
import { overlayTheme } from '../aiAssistantPanelTheme';

vi.mock('../../../../wailsjs/go/main/App', () => ({
    LoadConfig: vi.fn(),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    BrowserOpenURL: vi.fn(),
    EventsOn: vi.fn(() => vi.fn()),
    EventsOff: vi.fn(),
}));

vi.mock('../useNotifications', () => ({
    useNotifications: () => ({
        notifications: [],
        unreadCount: 0,
        panelOpen: false,
        categoryFilter: 'all',
        togglePanel: vi.fn(),
        setCategoryFilter: vi.fn(),
        markRead: vi.fn(),
        markAllRead: vi.fn(),
        urgentToast: null,
        dismissUrgentToast: vi.fn(),
    }),
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
    it('exposes mobile documents as a title-bar icon near tools', () => {
        renderTitleBar();
        const btn = screen.getByTestId('mobile-docs-titlebar-btn');
        expect(btn.getAttribute('aria-label')).toContain('Mobile');
    });

    it('no longer hosts global settings controls (moved to the quick settings bar)', () => {
        renderTitleBar();
        expect(screen.queryByTestId('workflow-toggle-btn')).toBeNull();
    });
});
