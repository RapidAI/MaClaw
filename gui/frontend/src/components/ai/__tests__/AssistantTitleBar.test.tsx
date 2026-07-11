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

const renderTitleBar = (workflowEnabled?: boolean) => render(
    <AssistantTitleBar
        clearHistory={vi.fn()}
        inline={false}
        lang="zh"
        maximized={false}
        onClose={vi.fn()}
        onToggleWorkflow={vi.fn()}
        projectSearchOpen={false}
        refreshNews={vi.fn()}
        setThemeMode={vi.fn()}
        setTtsEnabled={vi.fn()}
        showMaximizeToggle={false}
        theme={overlayTheme}
        themeMode="light"
        title="AI助手"
        trialReflectEnabled={false}
        ttsEnabled={false}
        ttsPlaying={false}
        toggleProjectSearch={vi.fn()}
        workflowEnabled={workflowEnabled}
    />,
);

describe('AssistantTitleBar workflow toggle', () => {
    it('defaults the workflow detection switch to off when workflowEnabled is unset', () => {
        renderTitleBar();

        const toggle = screen.getByTestId('workflow-toggle-btn');
        expect(toggle.getAttribute('aria-checked')).toBe('false');
        expect(toggle.getAttribute('aria-label')).toBe('工作流识别已关闭，点击开启');
    });

    it('shows workflow detection as on when explicitly enabled', () => {
        renderTitleBar(true);

        const toggle = screen.getByTestId('workflow-toggle-btn');
        expect(toggle.getAttribute('aria-checked')).toBe('true');
        expect(toggle.getAttribute('aria-label')).toBe('工作流识别已开启，点击关闭');
    });

    it('exposes mobile documents as a title-bar icon near tools', () => {
        renderTitleBar();
        const btn = screen.getByTestId('mobile-docs-titlebar-btn');
        expect(btn.getAttribute('aria-label')).toContain('Mobile');
    });
});
