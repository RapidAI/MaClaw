// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { TaskTabSwitcher } from '../TaskTabSwitcher';
import type { AITab } from '../AITabTypes';
import type { TaskManagementItem } from '../../layout/SidebarTaskManagement';

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

describe('TaskTabSwitcher with task list', () => {
    const taskListTabs: AITab[] = [
        { id: 'local', type: 'local', title: 'Local', closable: false },
        { id: 'proj-1', type: 'project', title: 'Old title', projectPath: 'D:/work/tasks/build-dashboard', closable: true },
        { id: 've-1', type: 've', title: 'Digital employee', veId: 've-1', closable: true },
    ];
    const taskRows: TaskManagementItem[] = [
        { id: 't1', name: 'Build dashboard', project_path: 'D:/work/tasks/build-dashboard' },
        { id: 't2', name: 'Review notes', project_path: 'D:/work/tasks/review-notes' },
        { id: 't3', name: '', project_path: 'D:/work/tasks/some-very-long-directory/unopened-task' },
    ];

    function renderTaskSwitcher(overrides: Partial<Parameters<typeof TaskTabSwitcher>[0]> = {}) {
        const props = {
            tabs: taskListTabs,
            tasks: taskRows,
            activeTabId: 'proj-1',
            lang: 'en',
            onActivate: vi.fn(),
            onClose: vi.fn(),
            onOpenTask: vi.fn(),
            ...overrides,
        };
        render(<TaskTabSwitcher {...props} />);
        return props;
    }

    it('lists local tab, every visible task row, and orphan open tabs', () => {
        renderTaskSwitcher();

        // 1 local + 3 tasks + 1 orphan VE tab
        expect(screen.getByTestId('task-tab-switcher-btn').textContent).toContain('(5)');

        openSwitcher();

        expect(screen.getByTestId('task-tab-switcher-item-local')).toBeTruthy();
        expect(screen.getByTestId('task-tab-switcher-item-proj-1')).toBeTruthy();
        expect(screen.getByTestId('task-tab-switcher-task-t2')).toBeTruthy();
        expect(screen.getByTestId('task-tab-switcher-task-t3')).toBeTruthy();
        expect(screen.getByTestId('task-tab-switcher-item-ve-1')).toBeTruthy();
        // Task rows show the task name, not the tab's stored title.
        expect(screen.queryByText('Old title')).toBeNull();
        expect(screen.getByText('Build dashboard')).toBeTruthy();
        // Empty task names fall back to a sanitized path-derived title.
        expect(screen.getByText('unopened-task')).toBeTruthy();
    });

    it('activates the open tab when clicking an opened task row', () => {
        const { onActivate, onOpenTask } = renderTaskSwitcher();
        openSwitcher();

        fireEvent.click(screen.getByTestId('task-tab-switcher-item-proj-1'));

        expect(onActivate).toHaveBeenCalledWith('proj-1');
        expect(onOpenTask).not.toHaveBeenCalled();
        expect(screen.getByTestId('task-tab-switcher-btn').getAttribute('aria-expanded')).toBe('false');
    });

    it('opens the task when clicking an unopened task row, without a close button', () => {
        const { onActivate, onOpenTask } = renderTaskSwitcher();
        openSwitcher();

        const row = screen.getByTestId('task-tab-switcher-task-t2');
        expect(row.className).toContain('mc-task-tab-switcher__item--unopened');
        expect(row.querySelector('.mc-task-tab-switcher__close')).toBeNull();

        fireEvent.click(row);

        expect(onOpenTask).toHaveBeenCalledWith('D:/work/tasks/review-notes', taskRows[1]);
        expect(onActivate).not.toHaveBeenCalled();
    });

    it('marks the active task row and closes only opened tasks', () => {
        const { onClose } = renderTaskSwitcher();
        openSwitcher();

        expect(screen.getByTestId('task-tab-switcher-item-proj-1').getAttribute('data-active')).toBe('true');
        expect(screen.getByTestId('task-tab-switcher-task-t2').getAttribute('data-active')).toBeNull();

        fireEvent.click(screen.getByTestId('task-tab-switcher-close-proj-1'));
        expect(onClose).toHaveBeenCalledWith('proj-1');
    });

    it('matches a task to its tab by cloud workspace identity across paths', () => {
        const cloudTabs: AITab[] = [
            { id: 'local', type: 'local', title: 'Local', closable: false },
            { id: 'cws-tab', type: 'project', title: 'Cloud task', projectPath: 'E:/new-cache/cloud-workspaces/tenant/cws-42', cloudWorkspaceId: 'cws-42', closable: true },
        ];
        const cloudTasks: TaskManagementItem[] = [
            { id: 'ct1', name: 'Cloud task', project_path: 'D:/old-cache/cloud-workspaces/tenant/cws-42', tags: ['cloud_workspace:cws-42'] },
        ];
        const { onActivate, onOpenTask } = renderTaskSwitcher({ tabs: cloudTabs, tasks: cloudTasks, activeTabId: 'local' });
        openSwitcher();

        fireEvent.click(screen.getByTestId('task-tab-switcher-item-cws-tab'));

        expect(onActivate).toHaveBeenCalledWith('cws-tab');
        expect(onOpenTask).not.toHaveBeenCalled();
    });
});
