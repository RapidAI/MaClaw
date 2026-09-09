// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { TaskTabSwitcher } from '../TaskTabSwitcher';
import type { AITab } from '../AITabTypes';

const tabs: AITab[] = [
    { id: 'local', type: 'local', title: 'Local', closable: false },
    { id: 'proj-1', type: 'project', title: 'Build dashboard', projectPath: 'D:/work/tasks/build-dashboard', closable: true },
    { id: 'proj-2', type: 'project', title: 'Review notes', projectPath: 'D:/work/tasks/review-notes', closable: true },
];

function renderSwitcher(overrides: Partial<Parameters<typeof TaskTabSwitcher>[0]> = {}) {
    const props = {
        tabs,
        activeTabId: 'proj-1',
        lang: 'en',
        onActivate: vi.fn(),
        onClose: vi.fn(),
        ...overrides,
    };
    render(<TaskTabSwitcher {...props} />);
    return props;
}

function openSwitcher() {
    fireEvent.click(screen.getByTestId('task-tab-switcher-btn'));
}

describe('TaskTabSwitcher', () => {
    it('shows the open task count without leaking tab titles until opened', () => {
        renderSwitcher();

        expect(screen.getByTestId('task-tab-switcher-btn').textContent).toContain('(3)');
        expect(screen.queryByText('Build dashboard')).toBeNull();

        openSwitcher();

        expect(screen.getByTestId('task-tab-switcher-item-local')).toBeTruthy();
        expect(screen.getByTestId('task-tab-switcher-item-proj-1')).toBeTruthy();
        expect(screen.getByTestId('task-tab-switcher-item-proj-2')).toBeTruthy();
        expect(screen.getByText('Build dashboard')).toBeTruthy();
        expect(screen.getByText('Review notes')).toBeTruthy();
    });

    it('marks the active tab and omits the close button on non-closable tabs', () => {
        renderSwitcher();
        openSwitcher();

        expect(screen.getByTestId('task-tab-switcher-item-proj-1').getAttribute('data-active')).toBe('true');
        expect(screen.getByTestId('task-tab-switcher-item-proj-2').getAttribute('data-active')).toBeNull();
        expect(screen.queryByTestId('task-tab-switcher-close-local')).toBeNull();
        expect(screen.getByTestId('task-tab-switcher-close-proj-1')).toBeTruthy();
    });

    it('activates a tab on click and closes the dropdown', () => {
        const { onActivate } = renderSwitcher();
        openSwitcher();

        fireEvent.click(screen.getByTestId('task-tab-switcher-item-proj-2'));

        expect(onActivate).toHaveBeenCalledWith('proj-2');
        expect(screen.queryByTestId('task-tab-switcher-item-proj-2')).toBeNull();
        expect(screen.getByTestId('task-tab-switcher-btn').getAttribute('aria-expanded')).toBe('false');
    });

    it('closes a tab via its close button without activating it', () => {
        const { onActivate, onClose } = renderSwitcher();
        openSwitcher();

        fireEvent.click(screen.getByTestId('task-tab-switcher-close-proj-2'));

        expect(onClose).toHaveBeenCalledWith('proj-2');
        expect(onActivate).not.toHaveBeenCalled();
    });

    it('closes the dropdown on outside click and Escape', () => {
        renderSwitcher();
        openSwitcher();
        expect(screen.getByTestId('task-tab-switcher-item-proj-1')).toBeTruthy();

        fireEvent.mouseDown(document.body);
        expect(screen.queryByTestId('task-tab-switcher-item-proj-1')).toBeNull();

        openSwitcher();
        fireEvent.keyDown(screen.getByTestId('task-tab-switcher-btn'), { key: 'Escape' });
        expect(screen.queryByTestId('task-tab-switcher-item-proj-1')).toBeNull();
    });

    it('closes a tab with the keyboard without activating it', () => {
        const { onActivate, onClose } = renderSwitcher();
        openSwitcher();

        const closeBtn = screen.getByTestId('task-tab-switcher-close-proj-2');
        // Keydown on the close button must not bubble into the row's activate handler.
        fireEvent.keyDown(closeBtn, { key: 'Enter' });
        expect(onActivate).not.toHaveBeenCalled();
        expect(onClose).not.toHaveBeenCalled();

        // Browsers dispatch click after Enter keydown on a button.
        fireEvent.click(closeBtn);
        expect(onClose).toHaveBeenCalledWith('proj-2');
        expect(onActivate).not.toHaveBeenCalled();
    });
});
