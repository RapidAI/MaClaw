// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { SidebarRecentTasks } from '../SidebarRecentTasks';
import type { ComponentProps } from 'react';
import { GetProjectScene, OpenFileOrShowInFolder } from '../../../../wailsjs/go/main/App';
import { EventsEmit } from '../../../../wailsjs/runtime';

const { getProjectSceneMock, openFileOrShowInFolderMock, eventsEmitMock } = vi.hoisted(() => ({
    getProjectSceneMock: vi.fn(),
    openFileOrShowInFolderMock: vi.fn(),
    eventsEmitMock: vi.fn(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetProjectScene: getProjectSceneMock,
    OpenFileOrShowInFolder: openFileOrShowInFolderMock,
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsEmit: eventsEmitMock,
}));

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
        continueWorkflowProject: vi.fn(),
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
    eventsEmitMock.mockClear();
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

    it('hides recent projects without tangible output', () => {
        renderRecentTasks({
            recentProjects: [
                { ...baseProject, has_output: false },
                { ...baseProject, id: 'task-2', name: 'Saved report', project_path: 'D:/work/tasks/saved-report', has_output: true },
            ],
        });

        expect(screen.queryByText('Build dashboard')).toBeNull();
        expect(screen.getByText('Saved report')).toBeTruthy();
    });

    it('shows the empty state when every recent project lacks output', () => {
        renderRecentTasks({ recentProjects: [{ ...baseProject, has_output: false }] });

        expect(screen.getByText('No recent tasks')).toBeTruthy();
        expect(screen.queryByText('Build dashboard')).toBeNull();
    });

    it('blocks task switching while the assistant is warming up', () => {
        const resumeRecentProject = vi.fn();
        const onRecentTaskSwitchBlocked = vi.fn();
        renderRecentTasks({ assistantReady: false, resumeRecentProject, onRecentTaskSwitchBlocked });

        fireEvent.doubleClick(screen.getByText('Build dashboard'));

        expect(resumeRecentProject).not.toHaveBeenCalled();
        expect(onRecentTaskSwitchBlocked).toHaveBeenCalledTimes(1);
    });

    it('shows restore progress and ignores duplicate opens while a task is opening', async () => {
        let resolveOpen: () => void = () => {};
        const resumeRecentProject = vi.fn(() => new Promise<void>((resolve) => {
            resolveOpen = resolve;
        }));
        renderRecentTasks({ resumeRecentProject });

        fireEvent.doubleClick(screen.getByText('Build dashboard'));
        fireEvent.doubleClick(screen.getByText('Build dashboard'));

        expect(resumeRecentProject).toHaveBeenCalledTimes(1);
        expect(screen.getByText('Restoring...')).toBeTruthy();
        expect(screen.getByLabelText('Restoring task')).toBeTruthy();

        resolveOpen();
        await waitFor(() => expect(screen.queryByText('Restoring...')).toBeNull());
    });

    it('emits project close event when removing a recent task', async () => {
        const hideTask = vi.fn().mockResolvedValue(undefined);
        const refreshRecentProjects = vi.fn();
        const setTaskContextMenu = vi.fn();
        renderRecentTasks({
            hideTask,
            refreshRecentProjects,
            setTaskContextMenu,
            taskContextMenu: { x: 10, y: 20, projectPath: baseProject.project_path, name: baseProject.name, pinned: false },
        });

        fireEvent.click(screen.getByText('Remove'));

        await waitFor(() => expect(hideTask).toHaveBeenCalledWith(baseProject.project_path));
        expect(EventsEmit).toHaveBeenCalledWith('project-task:closed', baseProject.project_path);
        expect(refreshRecentProjects).toHaveBeenCalledTimes(1);
        expect(setTaskContextMenu).toHaveBeenCalledWith(null);
    });

    it('still refreshes after remove when project close event emit fails', async () => {
        eventsEmitMock.mockImplementationOnce(() => { throw new Error('runtime unavailable'); });
        const hideTask = vi.fn().mockResolvedValue(undefined);
        const refreshRecentProjects = vi.fn();
        const setTaskContextMenu = vi.fn();
        renderRecentTasks({
            hideTask,
            refreshRecentProjects,
            setTaskContextMenu,
            taskContextMenu: { x: 10, y: 20, projectPath: baseProject.project_path, name: baseProject.name, pinned: false },
        });

        fireEvent.click(screen.getByText('Remove'));

        await waitFor(() => expect(hideTask).toHaveBeenCalledWith(baseProject.project_path));
        expect(refreshRecentProjects).toHaveBeenCalledTimes(1);
        expect(setTaskContextMenu).toHaveBeenCalledWith(null);
    });


    it('opens source-backed evidence from the sidebar task row', async () => {
        getProjectSceneMock.mockResolvedValue({
            project_path: baseProject.project_path,
            name: baseProject.name,
            entry_count: 3,
            recent_artifacts: [{ title: 'Design decision', source_url: 'D:/refs/design.md', source_hint: 'full: read_file' }],
        });
        renderRecentTasks();

        fireEvent.click(screen.getByLabelText('Scene details'));

        expect(await screen.findByText('Design decision')).toBeTruthy();
        expect(GetProjectScene).toHaveBeenCalledWith(baseProject.project_path);

        fireEvent.click(screen.getByLabelText('Open artifact source'));
        expect(OpenFileOrShowInFolder).toHaveBeenCalledWith('D:/refs/design.md');
    });

    it('shows unfinished workflow affordance without changing default task open', async () => {
        const resumeRecentProject = vi.fn();
        const continueWorkflowProject = vi.fn();
        getProjectSceneMock.mockResolvedValue({
            project_path: baseProject.project_path,
            name: baseProject.name,
            active_workflow: { type: 'coding', phase: 'tasks', project_path: 'D:/work/tasks/source-workflow' },
            recent_artifacts: [{ title: 'Task plan', source_url: 'D:/refs/task-plan.md' }],
        });
        renderRecentTasks({
            resumeRecentProject,
            continueWorkflowProject,
            recentProjects: [{ ...baseProject, active_workflow: { type: 'coding', phase: 'tasks', project_path: 'D:/work/tasks/source-workflow' } }],
        });

        expect(screen.getByText('Stage output')).toBeTruthy();

        fireEvent.doubleClick(screen.getByText('Build dashboard'));
        expect(resumeRecentProject).toHaveBeenCalledWith(baseProject.project_path);
        expect(continueWorkflowProject).not.toHaveBeenCalled();

        fireEvent.click(screen.getByLabelText('Scene details'));
        expect(await screen.findByText('Original workflow unfinished')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Continue workflow' }));

        expect(continueWorkflowProject).toHaveBeenCalledWith('D:/work/tasks/source-workflow');
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
