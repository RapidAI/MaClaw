// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { SidebarTaskManagement } from '../SidebarTaskManagement';
import type { ComponentProps } from 'react';
import { GetProjectScene, OpenFileOrShowInFolder, SelectWorkingDir } from '../../../../wailsjs/go/main/App';
import { EventsEmit } from '../../../../wailsjs/runtime';

const { getProjectSceneMock, openFileOrShowInFolderMock, selectWorkingDirMock, eventsEmitMock } = vi.hoisted(() => ({
    getProjectSceneMock: vi.fn(),
    openFileOrShowInFolderMock: vi.fn(),
    selectWorkingDirMock: vi.fn(),
    eventsEmitMock: vi.fn(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetProjectScene: getProjectSceneMock,
    OpenFileOrShowInFolder: openFileOrShowInFolderMock,
    SelectWorkingDir: selectWorkingDirMock,
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

function renderTaskManagement(overrides: Partial<ComponentProps<typeof SidebarTaskManagement>> = {}) {
    const props: ComponentProps<typeof SidebarTaskManagement> = {
        lang: 'en',
        tasks: [baseProject],
        renamingTaskPath: null,
        setRenamingTaskPath: vi.fn(),
        renameValue: '',
        setRenameValue: vi.fn(),
        resumeTask: vi.fn(),
        continueWorkflowProject: vi.fn(),
        createTask: vi.fn(),
        refreshTasks: vi.fn(),
        taskContextMenu: null,
        setTaskContextMenu: vi.fn(),
        renameTask: vi.fn(),
        pinTask: vi.fn(),
        hideTask: vi.fn(),
        ...overrides,
    };
    const view = render(<SidebarTaskManagement {...props} />);
    return { ...props, container: view.container };
}

afterEach(() => {
    vi.restoreAllMocks();
    eventsEmitMock.mockClear();
    selectWorkingDirMock.mockReset();
    document.getElementById('App')?.remove();
});

describe('SidebarTaskManagement', () => {
    it('switches tasks only on double click', () => {
        const resumeTask = vi.fn();
        renderTaskManagement({ resumeTask });

        fireEvent.click(screen.getByText('Build dashboard'));
        expect(resumeTask).not.toHaveBeenCalled();

        fireEvent.doubleClick(screen.getByText('Build dashboard'));
        expect(resumeTask).toHaveBeenCalledWith(baseProject.project_path);
    });

    it('hides recent projects without tangible output', () => {
        renderTaskManagement({
            tasks: [
                { ...baseProject, has_output: false },
                { ...baseProject, id: 'task-2', name: 'Saved report', project_path: 'D:/work/tasks/saved-report', has_output: true },
            ],
        });

        expect(screen.queryByText('Build dashboard')).toBeNull();
        expect(screen.getByText('Saved report')).toBeTruthy();
    });

    it('shows the empty state when every recent project lacks output', () => {
        renderTaskManagement({ tasks: [{ ...baseProject, has_output: false }] });

        expect(screen.getByText('No tasks')).toBeTruthy();
        expect(screen.queryByText('No saved tasks')).toBeNull();
        expect(screen.queryByText('Build dashboard')).toBeNull();
    });

    it('uses system-style SVG icons instead of text type labels', () => {
        const { container } = renderTaskManagement({
            tasks: [
                baseProject,
                { ...baseProject, id: 'task-2', name: 'Forked task', project_path: 'D:/work/tasks/forked-task', tags: ['forked_task'] },
                { ...baseProject, id: 'task-3', name: 'Pinned task', project_path: 'D:/work/tasks/pinned-task', pinned: true },
            ],
        });

        expect(screen.getByLabelText('Task').querySelector('svg')).toBeTruthy();
        expect(screen.getByLabelText('Referenced task').querySelector('svg')).toBeTruthy();
        expect(screen.getByLabelText('Pinned task').querySelector('svg')).toBeTruthy();
        expect(container.textContent).not.toContain('TASK');
        expect(container.textContent).not.toContain('REF');
        expect(container.textContent).not.toContain('PIN');
    });

    it('uses a clear SVG create icon instead of a plain plus glyph', () => {
        renderTaskManagement();

        const createButton = screen.getByTitle('Create task');

        expect(createButton.querySelector('svg')).toBeTruthy();
        expect(createButton.textContent).not.toContain('+');
    });

    it('requests saving the current main chat as a task from the header button', () => {
        const dispatchSpy = vi.spyOn(window, 'dispatchEvent');
        renderTaskManagement();

        const saveButton = screen.getByTitle('Save current chat as task');
        expect(screen.getByText('Save as Task')).toBeTruthy();
        expect(saveButton.querySelector('svg')).toBeTruthy();
        fireEvent.click(saveButton);

        expect(dispatchSpy).toHaveBeenCalledWith(expect.objectContaining({ type: 'ai-save-current-chat-as-task' }));
    });

    it('blocks task switching while the assistant is warming up', () => {
        const resumeTask = vi.fn();
        const onTaskSwitchBlocked = vi.fn();
        renderTaskManagement({ assistantReady: false, resumeTask, onTaskSwitchBlocked });

        fireEvent.doubleClick(screen.getByText('Build dashboard'));

        expect(resumeTask).not.toHaveBeenCalled();
        expect(onTaskSwitchBlocked).toHaveBeenCalledTimes(1);
    });

    it('shows restore progress and ignores duplicate opens while a task is opening', async () => {
        let resolveOpen: () => void = () => {};
        const resumeTask = vi.fn(() => new Promise<void>((resolve) => {
            resolveOpen = resolve;
        }));
        renderTaskManagement({ resumeTask });

        fireEvent.doubleClick(screen.getByText('Build dashboard'));
        fireEvent.doubleClick(screen.getByText('Build dashboard'));

        expect(resumeTask).toHaveBeenCalledTimes(1);
        expect(screen.getByText('Restoring...')).toBeTruthy();
        expect(screen.getByLabelText('Restoring task')).toBeTruthy();

        resolveOpen();
        await waitFor(() => expect(screen.queryByText('Restoring...')).toBeNull());
    });

    it('emits project close event when removing a task', async () => {
        const hideTask = vi.fn().mockResolvedValue(undefined);
        const refreshTasks = vi.fn();
        const setTaskContextMenu = vi.fn();
        renderTaskManagement({
            hideTask,
            refreshTasks,
            setTaskContextMenu,
            taskContextMenu: { x: 10, y: 20, projectPath: baseProject.project_path, name: baseProject.name, pinned: false },
        });

        fireEvent.click(screen.getByText('Remove'));

        await waitFor(() => expect(hideTask).toHaveBeenCalledWith(baseProject.project_path));
        expect(EventsEmit).toHaveBeenCalledWith('project-task:closed', baseProject.project_path);
        expect(refreshTasks).toHaveBeenCalledTimes(1);
        expect(setTaskContextMenu).toHaveBeenCalledWith(null);
    });

    it('still refreshes after remove when project close event emit fails', async () => {
        eventsEmitMock.mockImplementationOnce(() => { throw new Error('runtime unavailable'); });
        const hideTask = vi.fn().mockResolvedValue(undefined);
        const refreshTasks = vi.fn();
        const setTaskContextMenu = vi.fn();
        renderTaskManagement({
            hideTask,
            refreshTasks,
            setTaskContextMenu,
            taskContextMenu: { x: 10, y: 20, projectPath: baseProject.project_path, name: baseProject.name, pinned: false },
        });

        fireEvent.click(screen.getByText('Remove'));

        await waitFor(() => expect(hideTask).toHaveBeenCalledWith(baseProject.project_path));
        expect(refreshTasks).toHaveBeenCalledTimes(1);
        expect(setTaskContextMenu).toHaveBeenCalledWith(null);
    });


    it('opens source-backed evidence from the sidebar task row', async () => {
        getProjectSceneMock.mockResolvedValue({
            project_path: baseProject.project_path,
            name: baseProject.name,
            entry_count: 3,
            recent_artifacts: [{ title: 'Design decision', source_url: 'D:/refs/design.md', source_hint: 'full: read_file' }],
        });
        renderTaskManagement();

        fireEvent.click(screen.getByLabelText('Scene details'));

        expect(await screen.findByText('Design decision')).toBeTruthy();
        expect(GetProjectScene).toHaveBeenCalledWith(baseProject.project_path);

        fireEvent.click(screen.getByLabelText('Open artifact source'));
        expect(OpenFileOrShowInFolder).toHaveBeenCalledWith('D:/refs/design.md');
    });

    it('shows unfinished workflow affordance without changing default task open', async () => {
        const resumeTask = vi.fn();
        const continueWorkflowProject = vi.fn();
        getProjectSceneMock.mockResolvedValue({
            project_path: baseProject.project_path,
            name: baseProject.name,
            active_workflow: { type: 'coding', phase: 'tasks', project_path: 'D:/work/tasks/source-workflow' },
            recent_artifacts: [{ title: 'Task plan', source_url: 'D:/refs/task-plan.md' }],
        });
        renderTaskManagement({
            resumeTask,
            continueWorkflowProject,
            tasks: [{ ...baseProject, active_workflow: { type: 'coding', phase: 'tasks', project_path: 'D:/work/tasks/source-workflow' } }],
        });

        expect(screen.getByText('Stage output')).toBeTruthy();

        fireEvent.doubleClick(screen.getByText('Build dashboard'));
        expect(resumeTask).toHaveBeenCalledWith(baseProject.project_path);
        expect(continueWorkflowProject).not.toHaveBeenCalled();

        fireEvent.click(screen.getByLabelText('Scene details'));
        expect(await screen.findByText('Original workflow unfinished')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Continue workflow' }));

        expect(continueWorkflowProject).toHaveBeenCalledWith('D:/work/tasks/source-workflow');
    });

    it('creates a task from the header add button', () => {
        const createTask = vi.fn();
        renderTaskManagement({ createTask });

        fireEvent.click(screen.getByTitle('Create task'));
        fireEvent.change(screen.getByLabelText('Task command'), { target: { value: '  New   research   task  ' } });
        fireEvent.click(screen.getByRole('button', { name: 'OK' }));

        expect(createTask).toHaveBeenCalledWith('New research task');
    });

    it('passes the selected working folder when creating a task', async () => {
        selectWorkingDirMock.mockResolvedValue('D:/work/selected-folder');
        const createTask = vi.fn();
        renderTaskManagement({ createTask });

        fireEvent.click(screen.getByTitle('Create task'));
        fireEvent.click(screen.getByRole('button', { name: 'Choose working folder' }));

        expect(SelectWorkingDir).toHaveBeenCalledTimes(1);
        await waitFor(() => expect(screen.getByText('D:/work/selected-folder')).toBeTruthy());

        fireEvent.change(screen.getByLabelText('Task command'), { target: { value: 'Run local task' } });
        fireEvent.click(screen.getByRole('button', { name: 'OK' }));

        expect(createTask).toHaveBeenCalledWith('Run local task', 'D:/work/selected-folder');
    });

    it('marks the create dialog with the current theme', () => {
        renderTaskManagement({ themeMode: 'dark' });

        fireEvent.click(screen.getByTitle('Create task'));

        expect(screen.getByLabelText('Task command').closest('.modal-backdrop')?.getAttribute('data-ai-theme')).toBe('dark');
    });

    it('falls back to the app theme when the portaled create dialog has no theme prop', () => {
        const appRoot = document.createElement('div');
        appRoot.id = 'App';
        appRoot.setAttribute('data-ai-theme', 'dark');
        document.body.appendChild(appRoot);
        renderTaskManagement({ themeMode: undefined });

        fireEvent.click(screen.getByTitle('Create task'));

        expect(screen.getByLabelText('Task command').closest('.modal-backdrop')?.getAttribute('data-ai-theme')).toBe('dark');
    });

    it('carries the selected dark scheme into the portaled create dialog', () => {
        const appRoot = document.createElement('div');
        appRoot.id = 'App';
        appRoot.setAttribute('data-ai-theme', 'dark');
        appRoot.setAttribute('data-ai-dark-scheme', 'graphite');
        document.body.appendChild(appRoot);
        renderTaskManagement({ themeMode: 'dark' });

        fireEvent.click(screen.getByTitle('Create task'));

        expect(screen.getByLabelText('Task command').closest('.modal-backdrop')?.getAttribute('data-ai-dark-scheme')).toBe('graphite');
    });

    it('portals the create dialog outside the sidebar container so the backdrop covers the window', () => {
        const { container } = renderTaskManagement();

        fireEvent.click(screen.getByTitle('Create task'));

        const backdrop = screen.getByLabelText('Task command').closest('.modal-backdrop');
        expect(backdrop).toBeTruthy();
        expect(backdrop?.parentElement).toBe(document.body);
        expect(container.contains(backdrop)).toBe(false);
    });

    it('raises the create dialog backdrop above app chrome and fixed dropdowns', () => {
        renderTaskManagement();

        fireEvent.click(screen.getByTitle('Create task'));

        const backdrop = screen.getByLabelText('Task command').closest('.modal-backdrop') as HTMLElement;
        expect(Number(backdrop.style.zIndex)).toBeGreaterThan(99999);
    });

    it('dismisses any task context menu before opening the create dialog', () => {
        const setTaskContextMenu = vi.fn();
        renderTaskManagement({
            setTaskContextMenu,
            taskContextMenu: { x: 10, y: 20, projectPath: baseProject.project_path, name: baseProject.name, pinned: false },
        });

        fireEvent.click(screen.getByTitle('Create task'));

        expect(setTaskContextMenu).toHaveBeenCalledWith(null);
        expect(screen.getByRole('dialog', { name: 'Create task' })).toBeTruthy();
    });

    it('does not ask the parent to clear a missing context menu when opening the create dialog', () => {
        const setTaskContextMenu = vi.fn();
        renderTaskManagement({ setTaskContextMenu, taskContextMenu: null });

        fireEvent.click(screen.getByTitle('Create task'));

        expect(setTaskContextMenu).not.toHaveBeenCalled();
        expect(screen.getByRole('dialog', { name: 'Create task' })).toBeTruthy();
    });

    it('closes the create dialog with Escape from any dialog control', () => {
        renderTaskManagement();

        fireEvent.click(screen.getByTitle('Create task'));
        fireEvent.keyDown(screen.getByRole('button', { name: 'Cancel' }), { key: 'Escape' });

        expect(screen.queryByLabelText('Task command')).toBeNull();
    });

    it('does not close the create dialog when a backdrop click starts inside the dialog', () => {
        renderTaskManagement();

        fireEvent.click(screen.getByTitle('Create task'));
        const input = screen.getByLabelText('Task command');
        const backdrop = input.closest('.modal-backdrop') as HTMLElement;
        const dialog = input.closest('.modal-content') as HTMLElement;

        fireEvent.mouseDown(dialog);
        fireEvent.click(backdrop);

        expect(screen.getByLabelText('Task command')).toBeTruthy();
    });

    it('limits very long task names before creating', () => {
        const createTask = vi.fn();
        renderTaskManagement({ createTask });

        fireEvent.click(screen.getByTitle('Create task'));
        fireEvent.change(screen.getByLabelText('Task command'), { target: { value: 'a'.repeat(2100) } });
        fireEvent.click(screen.getByRole('button', { name: 'OK' }));

        expect(createTask).toHaveBeenCalledWith('a'.repeat(2000));
    });

    it('ignores duplicate create clicks while the task is being created', async () => {
        let resolveCreate: () => void = () => {};
        const createTask = vi.fn(() => new Promise<void>((resolve) => {
            resolveCreate = resolve;
        }));
        renderTaskManagement({ createTask });

        const createButton = screen.getByTitle('Create task');
        fireEvent.click(createButton);
        fireEvent.change(screen.getByLabelText('Task command'), { target: { value: 'New research task' } });
        const submitButton = screen.getByRole('button', { name: 'OK' });
        fireEvent.click(submitButton);
        fireEvent.click(submitButton);

        expect(createTask).toHaveBeenCalledTimes(1);
        expect((createButton as HTMLButtonElement).disabled).toBe(true);
        expect((submitButton as HTMLButtonElement).disabled).toBe(true);

        resolveCreate();
        await waitFor(() => expect((createButton as HTMLButtonElement).disabled).toBe(false));
    });
});
