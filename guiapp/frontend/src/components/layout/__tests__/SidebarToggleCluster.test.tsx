// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { SidebarToggleCluster } from '../SidebarToggleCluster';

const baseProps = {
    paneCollapsed: false,
    railCollapsed: false,
    onTogglePane: vi.fn(),
    onToggleRail: vi.fn(),
};

afterEach(() => {
    cleanup();
});

describe('SidebarToggleCluster', () => {
    it('renders both toggles with expanded (pressed) state and shortcut tooltips', () => {
        render(<SidebarToggleCluster {...baseProps} lang="zh-Hans" />);

        const paneToggle = screen.getByTestId('task-pane-toggle');
        const railToggle = screen.getByTestId('nav-rail-toggle');
        expect(paneToggle.getAttribute('aria-pressed')).toBe('true');
        expect(railToggle.getAttribute('aria-pressed')).toBe('true');
        expect(paneToggle.getAttribute('title')).toBe('收起任务列表 (Ctrl+B)');
        expect(railToggle.getAttribute('title')).toBe('收起功能栏 (Ctrl+Shift+B)');
    });

    it('flips labels and pressed state when columns are collapsed', () => {
        render(<SidebarToggleCluster {...baseProps} paneCollapsed railCollapsed lang="zh-Hans" />);

        const paneToggle = screen.getByTestId('task-pane-toggle');
        const railToggle = screen.getByTestId('nav-rail-toggle');
        expect(paneToggle.getAttribute('aria-pressed')).toBe('false');
        expect(railToggle.getAttribute('aria-pressed')).toBe('false');
        expect(paneToggle.getAttribute('title')).toBe('展开任务列表 (Ctrl+B)');
        expect(railToggle.getAttribute('title')).toBe('展开功能栏 (Ctrl+Shift+B)');
    });

    it('fires the matching toggle on click', () => {
        const onTogglePane = vi.fn();
        const onToggleRail = vi.fn();
        render(<SidebarToggleCluster {...baseProps} onTogglePane={onTogglePane} onToggleRail={onToggleRail} />);

        fireEvent.click(screen.getByTestId('task-pane-toggle'));
        fireEvent.click(screen.getByTestId('nav-rail-toggle'));
        expect(onTogglePane).toHaveBeenCalledTimes(1);
        expect(onToggleRail).toHaveBeenCalledTimes(1);
    });

    it('hides the pane toggle off the AI workbench tab (rail-only surfaces)', () => {
        render(<SidebarToggleCluster {...baseProps} showPaneToggle={false} />);

        expect(screen.queryByTestId('task-pane-toggle')).toBeNull();
        expect(screen.getByTestId('nav-rail-toggle')).toBeTruthy();
    });

    it('keeps header double-click-to-maximize handlers from firing on the buttons', () => {
        const headerDoubleClick = vi.fn();
        render(
            <div onDoubleClick={headerDoubleClick}>
                <SidebarToggleCluster {...baseProps} />
            </div>,
        );

        fireEvent.doubleClick(screen.getByTestId('task-pane-toggle'));
        fireEvent.doubleClick(screen.getByTestId('nav-rail-toggle'));
        expect(headerDoubleClick).not.toHaveBeenCalled();
    });
});
