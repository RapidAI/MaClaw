// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { SidebarRecentTasks } from '../SidebarRecentTasks';
import type { ComponentProps } from 'react';

const baseProject = {
    id: 'task-1',
    name: 'Build dashboard',
    project_path: 'D:/work/tasks/build-dashboard',
    preview: 'Recent task preview',
};

function renderRecentTasks(overrides: Partial<ComponentProps<typeof SidebarRecentTasks>> = {}) {
    const props: ComponentProps<typeof SidebarRecentTasks> = {
        lang: 'en',
        recentProjects: [baseProject],
        renamingTaskPath: null,
        setRenamingTaskPath: vi.fn(),
        renameValue: '',
        setRenameValue: vi.fn(),
        resumeRecentProject: vi.fn(),
        createRecentTask: vi.fn(),
        refreshRecentProjects: vi.fn(),
        taskContextMenu: null,
        setTaskContextMenu: vi.fn(),
        renameTask: vi.fn(),
        pinTask: vi.fn(),
        hideTask: vi.fn(),
        ...overrides,
    };
    render(<SidebarRecentTasks {...props} />);
    return props;
}

afterEach(() => {
    vi.restoreAllMocks();
});

describe('SidebarRecentTasks', () => {
    it('switches tasks only on double click', () => {
        const resumeRecentProject = vi.fn();
        renderRecentTasks({ resumeRecentProject });

        fireEvent.click(screen.getByText('Build dashboard'));
        expect(resumeRecentProject).not.toHaveBeenCalled();

        fireEvent.doubleClick(screen.getByText('Build dashboard'));
        expect(resumeRecentProject).toHaveBeenCalledWith(baseProject.project_path);
    });

    it('blocks task switching while the assistant is warming up', () => {
        const resumeRecentProject = vi.fn();
        const onRecentTaskSwitchBlocked = vi.fn();
        renderRecentTasks({ assistantReady: false, resumeRecentProject, onRecentTaskSwitchBlocked });

        fireEvent.doubleClick(screen.getByText('Build dashboard'));

        expect(resumeRecentProject).not.toHaveBeenCalled();
        expect(onRecentTaskSwitchBlocked).toHaveBeenCalledTimes(1);
    });

    it('creates a task from the header add button', () => {
        const createRecentTask = vi.fn();
        renderRecentTasks({ createRecentTask });

        fireEvent.click(screen.getByTitle('Create task'));
        fireEvent.change(screen.getByLabelText('Task command'), { target: { value: '  New   research   task  ' } });
        fireEvent.click(screen.getByRole('button', { name: 'OK' }));

        expect(createRecentTask).toHaveBeenCalledWith('New research task');
    });

    it('marks the create dialog with the current theme', () => {
        renderRecentTasks({ themeMode: 'dark' });

        fireEvent.click(screen.getByTitle('Create task'));

        expect(screen.getByLabelText('Task command').closest('.modal-backdrop')?.getAttribute('data-ai-theme')).toBe('dark');
    });

    it('closes the create dialog with Escape from any dialog control', () => {
        renderRecentTasks();

        fireEvent.click(screen.getByTitle('Create task'));
        fireEvent.keyDown(screen.getByRole('button', { name: 'Cancel' }), { key: 'Escape' });

        expect(screen.queryByLabelText('Task command')).toBeNull();
    });

    it('does not close the create dialog when a backdrop click starts inside the dialog', () => {
        renderRecentTasks();

        fireEvent.click(screen.getByTitle('Create task'));
        const input = screen.getByLabelText('Task command');
        const backdrop = input.closest('.modal-backdrop') as HTMLElement;
        const dialog = input.closest('.modal-content') as HTMLElement;

        fireEvent.mouseDown(dialog);
        fireEvent.click(backdrop);

        expect(screen.getByLabelText('Task command')).toBeTruthy();
    });

    it('limits very long task names before creating', () => {
        const createRecentTask = vi.fn();
        renderRecentTasks({ createRecentTask });

        fireEvent.click(screen.getByTitle('Create task'));
        fireEvent.change(screen.getByLabelText('Task command'), { target: { value: 'a'.repeat(130) } });
        fireEvent.click(screen.getByRole('button', { name: 'OK' }));

        expect(createRecentTask).toHaveBeenCalledWith('a'.repeat(120));
    });

    it('ignores duplicate create clicks while the task is being created', async () => {
        let resolveCreate: () => void = () => {};
        const createRecentTask = vi.fn(() => new Promise<void>((resolve) => {
            resolveCreate = resolve;
        }));
        renderRecentTasks({ createRecentTask });

        const createButton = screen.getByTitle('Create task');
        fireEvent.click(createButton);
        fireEvent.change(screen.getByLabelText('Task command'), { target: { value: 'New research task' } });
        const submitButton = screen.getByRole('button', { name: 'OK' });
        fireEvent.click(submitButton);
        fireEvent.click(submitButton);

        expect(createRecentTask).toHaveBeenCalledTimes(1);
        expect((createButton as HTMLButtonElement).disabled).toBe(true);
        expect((submitButton as HTMLButtonElement).disabled).toBe(true);

        resolveCreate();
        await waitFor(() => expect((createButton as HTMLButtonElement).disabled).toBe(false));
    });
});
