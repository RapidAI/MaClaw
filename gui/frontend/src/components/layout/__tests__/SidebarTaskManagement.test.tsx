// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { isProjectTabOpen, SidebarTaskManagement, taskCreationLabel, workflowStatusForTask } from '../SidebarTaskManagement';
import type { ComponentProps } from 'react';
import { GetProjectScene, OpenFileOrShowInFolder, SelectWorkingDir } from '../../../../wailsjs/go/main/App';
import { EventsEmit } from '../../../../wailsjs/runtime';

const {
    getProjectSceneMock,
    openFileOrShowInFolderMock,
    selectWorkingDirMock,
    eventsEmitMock,
    cloudWorkspaceEntitlementMock,
    createCloudWorkspaceMock,
    renameCloudWorkspaceMock,
    deleteCloudWorkspaceMock,
    restoreCloudWorkspaceMock,
} = vi.hoisted(() => ({
    getProjectSceneMock: vi.fn(),
    openFileOrShowInFolderMock: vi.fn(),
    selectWorkingDirMock: vi.fn(),
    eventsEmitMock: vi.fn(),
    cloudWorkspaceEntitlementMock: vi.fn().mockResolvedValue({ enabled: false }),
    createCloudWorkspaceMock: vi.fn(),
    renameCloudWorkspaceMock: vi.fn(),
    deleteCloudWorkspaceMock: vi.fn(),
    restoreCloudWorkspaceMock: vi.fn(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetProjectScene: getProjectSceneMock,
    OpenFileOrShowInFolder: openFileOrShowInFolderMock,
    SelectWorkingDir: selectWorkingDirMock,
    GetRemoteCodingTaskMeta: vi.fn().mockResolvedValue({ host: '10.0.0.8', user: 'ubuntu', port: 22, work_dir: '/app' }),
    UpdateRemoteCodingTaskMeta: vi.fn().mockResolvedValue(undefined),
    TestRemoteSSHConnection: vi.fn().mockResolvedValue('SSH ok'),
    CloudWorkspaceEntitlement: cloudWorkspaceEntitlementMock,
    CreateCloudWorkspace: createCloudWorkspaceMock,
    RenameCloudWorkspace: renameCloudWorkspaceMock,
    DeleteCloudWorkspace: deleteCloudWorkspaceMock,
    RestoreCloudWorkspace: restoreCloudWorkspaceMock,
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
    return { ...props, container: view.container, rerender: view.rerender };
}

afterEach(async () => {
    // Flush pending openEditRemoteDialog / save / test microtasks so unmount is quiet.
    await act(async () => {
        await Promise.resolve();
    });
    eventsEmitMock.mockClear();
    selectWorkingDirMock.mockReset();
    getProjectSceneMock.mockReset();
    openFileOrShowInFolderMock.mockReset();
    cloudWorkspaceEntitlementMock.mockReset();
    cloudWorkspaceEntitlementMock.mockResolvedValue({ enabled: false });
    createCloudWorkspaceMock.mockReset();
    renameCloudWorkspaceMock.mockReset();
    deleteCloudWorkspaceMock.mockReset();
    restoreCloudWorkspaceMock.mockReset();
    document.getElementById('App')?.remove();
});

describe('isProjectTabOpen', () => {
    it('matches normalized path separators and drive case', () => {
        expect(isProjectTabOpen('D:/work/tasks/a', ['D:\\work\\tasks\\a'])).toBe(true);
        expect(isProjectTabOpen('d:/work/tasks/a', ['D:/work/tasks/a'])).toBe(true);
        expect(isProjectTabOpen('D:/work/tasks/a', ['D:/work/tasks/b'])).toBe(false);
        expect(isProjectTabOpen('D:/work/tasks/a', undefined)).toBe(false);
        expect(isProjectTabOpen('', ['D:/work/tasks/a'])).toBe(false);
    });
});

describe('SidebarTaskManagement', () => {
    it('shows the real workflow status and a stable task creation time rather than activity time', () => {
        renderTaskManagement({
            tasks: [{
                ...baseProject,
                active_workflow: { type: 'coding', phase: 'quality_review', pending_review: true },
                created_at: '2026-01-01T00:00:00.000Z',
            }],
        });

        expect(screen.getByTestId('task-workflow-status').textContent).toBe('Review needed · Quality Review');
        expect(screen.getByLabelText('Task status: Review needed · Quality Review')).toBeTruthy();
        expect(screen.getByTestId('task-created-at').textContent).toMatch(/^Created 2026-01-01 /);
        expect(screen.queryByText('Stage output')).toBeNull();
    });

    it('classifies workflow state without treating a coding task as active runtime', () => {
        expect(workflowStatusForTask({ status: 'blocked', phase: 'implement' }, 'en')).toEqual({
            label: 'Needs attention', detail: 'Implement', tone: 'danger',
        });
        expect(workflowStatusForTask(undefined, 'en')).toBeNull();
        expect(taskCreationLabel('2026-01-01T00:00:00.000Z', 'en')).toMatch(/^Created 2026-01-01 /);
        expect(taskCreationLabel('not-a-date', 'en')).toBe('');
    });

    it('keeps completed and cancelled workflow snapshots distinct', () => {
        expect(workflowStatusForTask({ status: 'completed', phase: 'review' }, 'en')).toEqual({
            label: 'Completed', detail: 'Review', tone: 'success',
        });
        expect(workflowStatusForTask({ status: 'cancelled', phase: 'implementation' }, 'en')).toEqual({
            label: 'Cancelled', detail: 'Implementation', tone: 'neutral',
        });
        expect(workflowStatusForTask({ status: 'cancelled', phase: 'implementation', pending_review: true }, 'en')).toEqual({
            label: 'Cancelled', detail: 'Implementation', tone: 'neutral',
        });
        expect(workflowStatusForTask({ status: 'completed', phase: 'review', pending_review: true }, 'en')).toEqual({
            label: 'Completed', detail: 'Review', tone: 'success',
        });
        expect(workflowStatusForTask({ status: ' COMPLETED ', phase: 'review' }, 'en')).toEqual({
            label: 'Completed', detail: 'Review', tone: 'success',
        });
        expect(workflowStatusForTask({ status: 'succeeded', phase: 'review' }, 'en')).toEqual({
            label: 'Completed', detail: 'Review', tone: 'success',
        });
        expect(workflowStatusForTask({ status: 'canceled', phase: 'review', pending_review: true }, 'en')).toEqual({
            label: 'Cancelled', detail: 'Review', tone: 'neutral',
        });
    });

    it('switches tasks only on double click', () => {
        const resumeTask = vi.fn();
        renderTaskManagement({ resumeTask });

        fireEvent.click(screen.getByText('Build dashboard'));
        expect(resumeTask).not.toHaveBeenCalled();

        fireEvent.doubleClick(screen.getByText('Build dashboard'));
        expect(resumeTask).toHaveBeenCalledWith(baseProject.project_path, expect.objectContaining({ project_path: baseProject.project_path }));
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

    it('shows an expert task as a normal resumable task item', () => {
        renderTaskManagement({
            tasks: [{
                ...baseProject,
                id: 'expert-task-1',
                name: 'Literature Search Expert',
                project_path: 'D:/work/tasks/literature-search-expert',
                tags: ['task_management', 'source:expert:expert-literature-search'],
            }],
        });

        expect(screen.getByText('Literature Search Expert')).toBeTruthy();
        expect(screen.getByLabelText('Task')).toBeTruthy();
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

    it('passes the clicked task row when reopening it', async () => {
        const expertTask = {
            ...baseProject,
            tags: ['task_management', 'source:expert:expert-paper'],
        };
        const resumeTask = vi.fn().mockResolvedValue(undefined);
        renderTaskManagement({ tasks: [expertTask], resumeTask });

        fireEvent.doubleClick(screen.getByText('Build dashboard'));

        await waitFor(() => expect(resumeTask).toHaveBeenCalledWith(
            expertTask.project_path,
            expect.objectContaining({ tags: expertTask.tags }),
        ));
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

    it('shows an inline progress bar on the task being removed', async () => {
        let finishRemove: (() => void) | undefined;
        const hideTask = vi.fn(() => new Promise<void>(resolve => { finishRemove = resolve; }));
        renderTaskManagement({
            hideTask,
            taskContextMenu: { x: 10, y: 20, projectPath: baseProject.project_path, name: baseProject.name, pinned: false },
        });

        fireEvent.click(screen.getByText('Remove'));

        expect((await screen.findByTestId('task-remove-progress')).getAttribute('aria-label')).toBe('Removing task');
        expect(screen.getByText('Removing task...')).toBeTruthy();
        expect(hideTask).toHaveBeenCalledWith(baseProject.project_path);

        finishRemove?.();
        await waitFor(() => expect(screen.queryByTestId('task-remove-progress')).toBeNull());
    });

    it('keeps the task in place and reports a failed removal', async () => {
        const hideTask = vi.fn().mockRejectedValue(new Error('backend unavailable'));
        const refreshTasks = vi.fn();
        renderTaskManagement({
            hideTask,
            refreshTasks,
            taskContextMenu: { x: 10, y: 20, projectPath: baseProject.project_path, name: baseProject.name, pinned: false },
        });

        fireEvent.click(screen.getByText('Remove'));

        expect((await screen.findByTestId('task-remove-error')).textContent).toContain('backend unavailable');
        expect(screen.getByText('Build dashboard')).toBeTruthy();
        expect(screen.queryByTestId('task-remove-progress')).toBeNull();
        expect(EventsEmit).not.toHaveBeenCalledWith('project-task:closed', baseProject.project_path);
        expect(refreshTasks).not.toHaveBeenCalled();
    });

    it('keeps each task removal error visible when concurrent removals fail', async () => {
        const secondProject = { ...baseProject, id: 'task-2', name: 'Review notes', project_path: 'D:/work/tasks/review-notes' };
        const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});
        const hideTask = vi.fn((projectPath: string) => Promise.reject(new Error(projectPath === baseProject.project_path ? 'first failure' : 'second failure')));
        const { rerender } = renderTaskManagement({
            tasks: [baseProject, secondProject],
            hideTask,
            taskContextMenu: { x: 10, y: 20, projectPath: baseProject.project_path, name: baseProject.name, pinned: false },
        });

        fireEvent.click(screen.getByText('Remove'));
        await screen.findByText('first failure');

        rerender(<SidebarTaskManagement
            lang="en"
            tasks={[baseProject, secondProject]}
            renamingTaskPath={null}
            setRenamingTaskPath={vi.fn()}
            renameValue=""
            setRenameValue={vi.fn()}
            resumeTask={vi.fn()}
            continueWorkflowProject={vi.fn()}
            createTask={vi.fn()}
            refreshTasks={vi.fn()}
            taskContextMenu={{ x: 10, y: 20, projectPath: secondProject.project_path, name: secondProject.name, pinned: false }}
            setTaskContextMenu={vi.fn()}
            renameTask={vi.fn()}
            pinTask={vi.fn()}
            hideTask={hideTask}
        />);
        fireEvent.click(screen.getByText('Remove'));

        await screen.findByText('second failure');
        expect(screen.getAllByTestId('task-remove-error')).toHaveLength(2);
        consoleError.mockRestore();
    });

    it('does not announce or refresh when the guarded removal is skipped', async () => {
        const hideTask = vi.fn().mockResolvedValue(false);
        const refreshTasks = vi.fn();
        renderTaskManagement({
            hideTask,
            refreshTasks,
            taskContextMenu: { x: 10, y: 20, projectPath: baseProject.project_path, name: baseProject.name, pinned: false },
        });

        fireEvent.click(screen.getByText('Remove'));

        await waitFor(() => expect(hideTask).toHaveBeenCalledWith(baseProject.project_path));
        expect(screen.getByText('Build dashboard')).toBeTruthy();
        expect(screen.queryByTestId('task-remove-progress')).toBeNull();
        expect(screen.queryByTestId('task-remove-error')).toBeNull();
        expect(EventsEmit).not.toHaveBeenCalledWith('project-task:closed', baseProject.project_path);
        expect(refreshTasks).not.toHaveBeenCalled();
    });

    it('keeps progress on every row while separate removals are in flight', async () => {
        const secondProject = { ...baseProject, id: 'task-2', name: 'Review notes', project_path: 'D:/work/tasks/review-notes' };
        const pending = new Map<string, () => void>();
        const hideTask = vi.fn((projectPath: string) => new Promise<void>(resolve => { pending.set(projectPath, resolve); }));
        const { rerender } = renderTaskManagement({
            tasks: [baseProject, secondProject],
            hideTask,
            taskContextMenu: { x: 10, y: 20, projectPath: baseProject.project_path, name: baseProject.name, pinned: false },
        });

        fireEvent.click(screen.getByText('Remove'));
        await screen.findByTestId('task-remove-progress');

        rerender(<SidebarTaskManagement
            lang="en"
            tasks={[baseProject, secondProject]}
            renamingTaskPath={null}
            setRenamingTaskPath={vi.fn()}
            renameValue=""
            setRenameValue={vi.fn()}
            resumeTask={vi.fn()}
            continueWorkflowProject={vi.fn()}
            createTask={vi.fn()}
            refreshTasks={vi.fn()}
            taskContextMenu={{ x: 10, y: 20, projectPath: secondProject.project_path, name: secondProject.name, pinned: false }}
            setTaskContextMenu={vi.fn()}
            renameTask={vi.fn()}
            pinTask={vi.fn()}
            hideTask={hideTask}
        />);
        fireEvent.click(screen.getByText('Remove'));

        await waitFor(() => expect(screen.getAllByTestId('task-remove-progress')).toHaveLength(2));
        expect(hideTask).toHaveBeenCalledTimes(2);

        pending.get(baseProject.project_path)?.();
        pending.get(secondProject.project_path)?.();
        await waitFor(() => expect(screen.queryByTestId('task-remove-progress')).toBeNull());
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

    it('blocks remove when the task tab is already open', async () => {
        const hideTask = vi.fn().mockResolvedValue(undefined);
        const refreshTasks = vi.fn();
        const setTaskContextMenu = vi.fn();
        renderTaskManagement({
            hideTask,
            refreshTasks,
            setTaskContextMenu,
            openProjectTabPaths: [baseProject.project_path.replace(/\//g, '\\')],
            taskContextMenu: { x: 10, y: 20, projectPath: baseProject.project_path, name: baseProject.name, pinned: false },
        });

        const removeItem = screen.getByTestId('task-context-remove');
        expect(removeItem.getAttribute('data-disabled')).toBe('true');
        expect(removeItem.getAttribute('aria-disabled')).toBe('true');
        fireEvent.click(removeItem);

        await act(async () => { await Promise.resolve(); });
        expect(hideTask).not.toHaveBeenCalled();
        expect(EventsEmit).not.toHaveBeenCalledWith('project-task:closed', expect.anything());
        expect(refreshTasks).not.toHaveBeenCalled();
        // Menu stays open so the user can see the disabled state / tooltip.
        expect(setTaskContextMenu).not.toHaveBeenCalledWith(null);
    });

    it('blocks remove when the matching expert tab is open', async () => {
        const expertTask = {
            ...baseProject,
            tags: ['task_management', 'source:expert:expert-paper'],
        };
        const hideTask = vi.fn().mockResolvedValue(undefined);
        const refreshTasks = vi.fn();
        const setTaskContextMenu = vi.fn();
        renderTaskManagement({
            tasks: [expertTask],
            hideTask,
            refreshTasks,
            setTaskContextMenu,
            openExpertTabIDs: ['expert-paper'],
            taskContextMenu: { x: 10, y: 20, projectPath: expertTask.project_path, name: expertTask.name, pinned: false, tags: expertTask.tags },
        });

        const removeItem = screen.getByTestId('task-context-remove');
        expect(removeItem.getAttribute('data-disabled')).toBe('true');
        fireEvent.click(removeItem);

        await act(async () => { await Promise.resolve(); });
        expect(hideTask).not.toHaveBeenCalled();
        expect(refreshTasks).not.toHaveBeenCalled();
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

    it('keeps the evidence panel actionable when loading fails', async () => {
        const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});
        getProjectSceneMock
            .mockRejectedValueOnce(new Error('service unavailable'))
            .mockResolvedValueOnce({
                project_path: baseProject.project_path,
                recent_artifacts: [{ title: 'Evidence after retry' }],
            });
        renderTaskManagement();

        fireEvent.click(screen.getByLabelText('Scene details'));
        expect(await screen.findByText('Could not load evidence')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Retry' }));

        expect(await screen.findByText('Evidence after retry')).toBeTruthy();
        expect(GetProjectScene).toHaveBeenCalledTimes(2);
        consoleError.mockRestore();
    });

    it('ignores a stale evidence response after opening another task', async () => {
        let resolveFirst: (detail: unknown) => void = () => {};
        const firstDetail = new Promise<unknown>((resolve) => { resolveFirst = resolve; });
        getProjectSceneMock
            .mockReturnValueOnce(firstDetail)
            .mockResolvedValueOnce({
                project_path: 'D:/work/tasks/second',
                name: 'Second task',
                recent_artifacts: [{ title: 'Second task evidence' }],
            });
        renderTaskManagement({
            tasks: [
                baseProject,
                { ...baseProject, id: 'task-2', name: 'Second task', project_path: 'D:/work/tasks/second' },
            ],
        });

        fireEvent.click(screen.getAllByLabelText('Scene details')[0]);
        fireEvent.click(screen.getAllByLabelText('Scene details')[1]);
        expect(await screen.findByText('Second task evidence')).toBeTruthy();

        await act(async () => {
            resolveFirst({
                project_path: baseProject.project_path,
                name: baseProject.name,
                recent_artifacts: [{ title: 'Stale first-task evidence' }],
            });
            await Promise.resolve();
        });

        expect(screen.queryByText('Stale first-task evidence')).toBeNull();
        expect(screen.getByText('Second task evidence')).toBeTruthy();
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

        expect(screen.getByTestId('task-workflow-status').textContent).toBe('In progress · Tasks');

        fireEvent.doubleClick(screen.getByText('Build dashboard'));
        expect(resumeTask).toHaveBeenCalledWith(baseProject.project_path, expect.objectContaining({ project_path: baseProject.project_path }));
        expect(continueWorkflowProject).not.toHaveBeenCalled();

        fireEvent.click(screen.getByLabelText('Scene details'));
        expect(await screen.findByText('Original workflow unfinished')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Continue workflow' }));

        expect(continueWorkflowProject).toHaveBeenCalledWith('D:/work/tasks/source-workflow');
    });

    it('prevents duplicate workflow continuation while the first request is pending', async () => {
        let resolveContinuation: () => void = () => {};
        const continueWorkflowProject = vi.fn(() => new Promise<void>(resolve => { resolveContinuation = resolve; }));
        getProjectSceneMock.mockResolvedValue({
            project_path: baseProject.project_path,
            active_workflow: { type: 'coding', phase: 'tasks', project_path: 'D:/work/tasks/source-workflow' },
        });
        renderTaskManagement({ continueWorkflowProject });

        fireEvent.click(screen.getByLabelText('Scene details'));
        const continueButton = await screen.findByRole('button', { name: 'Continue workflow' });
        fireEvent.click(continueButton);
        fireEvent.click(continueButton);

        expect(continueWorkflowProject).toHaveBeenCalledTimes(1);
        expect((continueButton as HTMLButtonElement).disabled).toBe(true);
        expect(continueButton.textContent).toBe('Opening workflow...');

        await act(async () => {
            resolveContinuation();
            await Promise.resolve();
        });
        expect((await screen.findByRole('button', { name: 'Continue workflow' }) as HTMLButtonElement).disabled).toBe(false);
    });

    it.each([
        ['completed', false, 'Workflow completed'],
        ['cancelled', true, 'Workflow cancelled'],
    ])('shows a terminal workflow outcome without offering continuation (%s)', async (status, pending_review, expectedLabel) => {
        const continueWorkflowProject = vi.fn();
        getProjectSceneMock.mockResolvedValue({
            project_path: baseProject.project_path,
            name: baseProject.name,
            active_workflow: {
                type: 'coding',
                phase: 'review',
                status,
                pending_review,
                project_path: 'D:/work/tasks/source-workflow',
            },
        });
        renderTaskManagement({ continueWorkflowProject });

        fireEvent.click(screen.getByLabelText('Scene details'));

        expect((await screen.findByTestId('task-evidence-workflow-state')).textContent).toBe(expectedLabel);
        expect(screen.queryByRole('button', { name: 'Continue workflow' })).toBeNull();
        expect(continueWorkflowProject).not.toHaveBeenCalled();
    });

    it('keeps recovery available while clearly marking a workflow that needs attention', async () => {
        const continueWorkflowProject = vi.fn();
        getProjectSceneMock.mockResolvedValue({
            project_path: baseProject.project_path,
            name: baseProject.name,
            active_workflow: {
                type: 'coding',
                phase: 'implementation',
                status: 'blocked',
                project_path: 'D:/work/tasks/source-workflow',
            },
        });
        renderTaskManagement({
            continueWorkflowProject,
            tasks: [{ ...baseProject, active_workflow: { type: 'coding', phase: 'implementation', status: 'blocked' } }],
        });

        expect(screen.getByTestId('task-workflow-status').textContent).toBe('Needs attention · Implementation');
        fireEvent.click(screen.getByLabelText('Scene details'));
        expect((await screen.findByTestId('task-evidence-workflow-state')).textContent).toBe('Workflow needs attention');

        fireEvent.click(screen.getByRole('button', { name: 'Continue workflow' }));
        expect(continueWorkflowProject).toHaveBeenCalledWith('D:/work/tasks/source-workflow');
    });

    it('creates a task from the header add button', () => {
        const createTask = vi.fn();
        renderTaskManagement({ createTask });

        fireEvent.click(screen.getByTitle('Create task'));
        expect(screen.queryByLabelText('Task command')).toBeNull();
        expect(screen.getByTestId('task-create-guidance')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Create & open' }));

        expect(createTask).toHaveBeenCalledWith('New task');
    });

    it('exposes local and remote coding modes in the create dialog footer', () => {
        renderTaskManagement();

        fireEvent.click(screen.getByTitle('Create task'));

        const codingToggle = screen.getByRole('button', { name: 'Coding' });
        const remoteToggle = screen.getByRole('button', { name: 'Remote' });
        expect(codingToggle).toBeTruthy();
        expect(remoteToggle).toBeTruthy();
        expect(codingToggle.getAttribute('aria-pressed')).toBe('false');
        expect(remoteToggle.getAttribute('aria-pressed')).toBe('false');
        expect(codingToggle.closest('[role=group]')).toBeTruthy();
    });

    it('creates a coding development task when the coding option is selected', () => {
        const createTask = vi.fn();
        renderTaskManagement({ createTask });

        fireEvent.click(screen.getByTitle('Create task'));
        fireEvent.click(screen.getByRole('button', { name: 'Coding' }));
        fireEvent.click(screen.getByRole('button', { name: 'Create & open' }));

        expect(createTask).toHaveBeenCalledWith('New local coding task', undefined, 'coding_dev');
    });

    it('opens create dialog prefilled from welcome coding-task event (local)', async () => {
        const createTask = vi.fn();
        renderTaskManagement({
            createTask,
            tasks: [{
                ...baseProject,
                tags: ['coding_dev'],
                working_dir: 'D:/work/coding-project',
            }],
        });

        act(() => {
            window.dispatchEvent(new CustomEvent('ai-open-create-coding-task', {
                // Post-param-dialog commands are fully filled (no [placeholder] fill-in).
                detail: { mode: 'coding_dev', name: '按需求实现功能\n需求描述：审核流程' },
            }));
        });

        await screen.findByTestId('task-create-guidance');
        expect(screen.getByRole('heading', { name: 'Create local coding task' })).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Coding' }).getAttribute('aria-pressed')).toBe('true');
        expect(screen.getByRole('button', { name: 'Remote' }).getAttribute('aria-pressed')).toBe('false');
        // Prefers last coding task workdir
        expect(screen.getByTitle('D:/work/coding-project')).toBeTruthy();

        fireEvent.click(screen.getByRole('button', { name: 'Create & open' }));
        await waitFor(() => {
            expect(createTask).toHaveBeenCalledWith(
                '按需求实现功能\n需求描述：审核流程',
                'D:/work/coding-project',
                'coding_dev',
            );
        });
    });

    it('auto-creates a local coding task from welcome event without opening dialog', async () => {
        const createTask = vi.fn().mockResolvedValue(undefined);
        renderTaskManagement({ createTask });

        act(() => {
            window.dispatchEvent(new CustomEvent('ai-open-create-coding-task', {
                detail: {
                    mode: 'coding_dev',
                    name: 'Implement feature\nRequirement: login',
                    workingDir: 'D:/work/app',
                    autoCreate: true,
                },
            }));
        });

        await waitFor(() => {
            expect(createTask).toHaveBeenCalledWith(
                'Implement feature\nRequirement: login',
                'D:/work/app',
                'coding_dev',
            );
        });
        expect(screen.queryByRole('heading', { name: /Create local coding task/i })).toBeNull();
    });

    it('auto-creates a remote coding task from welcome event', async () => {
        const createTask = vi.fn().mockResolvedValue(undefined);
        renderTaskManagement({ createTask });

        act(() => {
            window.dispatchEvent(new CustomEvent('ai-open-create-coding-task', {
                detail: {
                    mode: 'remote_coding_dev',
                    name: 'Fix production 5xx',
                    autoCreate: true,
                    remote: {
                        host: '10.0.0.8',
                        port: 22,
                        user: 'ubuntu',
                        password: 'secret',
                        workDir: '/app',
                    },
                },
            }));
        });

        await waitFor(() => {
            expect(createTask).toHaveBeenCalledWith(
                'Fix production 5xx',
                undefined,
                'remote_coding_dev',
                {
                    host: '10.0.0.8',
                    port: 22,
                    user: 'ubuntu',
                    password: 'secret',
                    workDir: '/app',
                },
            );
        });
    });

    it('falls back to a prefilled create dialog when welcome auto-create fails', async () => {
        const createTask = vi.fn().mockRejectedValue(new Error('无法连接到远程服务器 root@10.0.0.8:22，请检查网络和凭据'));
        renderTaskManagement({ createTask });

        act(() => {
            window.dispatchEvent(new CustomEvent('ai-open-create-coding-task', {
                detail: {
                    mode: 'remote_coding_dev',
                    name: 'Develop a new project remotely',
                    autoCreate: true,
                    remote: {
                        host: '10.0.0.8',
                        port: 22,
                        user: 'root',
                        password: 'bad-password',
                        workDir: '/home',
                    },
                },
            }));
        });

        await waitFor(() => expect(createTask).toHaveBeenCalled());
        // The fallback dialog must open with the error visible. Previously the
        // in-flight create guard swallowed openCreateDialog, leaving the user
        // with zero feedback after the welcome param dialog closed.
        expect(await screen.findByRole('dialog', { name: 'Create remote coding task' })).toBeTruthy();
        await waitFor(() => {
            expect(screen.getByTestId('create-task-error').textContent).toContain('无法连接到远程服务器');
        });
        expect((screen.getByLabelText('Host / domain') as HTMLInputElement).value).toBe('10.0.0.8');
        expect((screen.getByLabelText('Username') as HTMLInputElement).value).toBe('root');
        expect((screen.getByLabelText('Password') as HTMLInputElement).value).toBe('bad-password');
        expect((screen.getByLabelText('Remote work directory') as HTMLInputElement).value).toBe('/home');
    });

    it('shows in-flight progress while welcome auto-create is running', async () => {
        let resolveCreate: () => void = () => {};
        const createTask = vi.fn(() => new Promise<void>((resolve) => {
            resolveCreate = resolve;
        }));
        renderTaskManagement({ createTask });

        act(() => {
            window.dispatchEvent(new CustomEvent('ai-open-create-coding-task', {
                detail: {
                    mode: 'remote_coding_dev',
                    name: 'Develop a new project remotely',
                    autoCreate: true,
                    remote: { host: '10.0.0.8', port: 22, user: 'root', password: 'pw', workDir: '/home' },
                },
            }));
        });

        // While the create is in flight (SSH dial can take seconds), the user
        // must see progress instead of a silent welcome page.
        expect(await screen.findByTestId('task-autocreate-progress')).toBeTruthy();
        expect(screen.getByTestId('task-autocreate-progress').textContent).toMatch(/Connecting SSH|\u6b63\u5728\u8fde\u63a5 SSH|\u6b63\u5728\u9023\u7dda SSH/i);
        expect(screen.queryByRole('dialog', { name: 'Create remote coding task' })).toBeNull();

        await act(async () => {
            resolveCreate();
            await Promise.resolve();
        });
        await waitFor(() => expect(screen.queryByTestId('task-autocreate-progress')).toBeNull());
        // Success path: no fallback dialog.
        expect(screen.queryByRole('dialog', { name: 'Create remote coding task' })).toBeNull();
    });

    it('opens the prefilled dialog instead of dropping a welcome event during an in-flight create', async () => {
        let resolveCreate: () => void = () => {};
        const createTask = vi.fn(() => new Promise<void>((resolve) => {
            resolveCreate = resolve;
        }));
        renderTaskManagement({ createTask });

        // First auto-create goes in flight.
        act(() => {
            window.dispatchEvent(new CustomEvent('ai-open-create-coding-task', {
                detail: { mode: 'coding_dev', name: 'First task', autoCreate: true },
            }));
        });
        await screen.findByTestId('task-autocreate-progress');

        // Second welcome event arrives while the first is still running: it must
        // surface as a prefilled (busy) dialog, not vanish silently.
        act(() => {
            window.dispatchEvent(new CustomEvent('ai-open-create-coding-task', {
                detail: { mode: 'coding_dev', name: 'Second task', autoCreate: true },
            }));
        });

        expect(await screen.findByRole('dialog', { name: 'Create local coding task' })).toBeTruthy();
        expect(screen.getByTestId('task-create-guidance')).toBeTruthy();
        // No second auto-create while the first is in flight.
        expect(createTask).toHaveBeenCalledTimes(1);
        // Controls reflect the in-flight create, then unlock when it settles.
        expect((screen.getByRole('button', { name: 'Create & open' }) as HTMLButtonElement).disabled).toBe(true);
        await act(async () => {
            resolveCreate();
            await Promise.resolve();
        });
        await waitFor(() => {
            expect((screen.getByRole('button', { name: 'Create & open' }) as HTMLButtonElement).disabled).toBe(false);
        });
    });

    it('does not let stale create completion close a newer forced dialog', async () => {
        let resolveCreate: () => void = () => {};
        const createTask = vi.fn(() => new Promise<void>((resolve) => {
            resolveCreate = resolve;
        }));
        renderTaskManagement({ createTask });

        act(() => {
            window.dispatchEvent(new CustomEvent('ai-open-create-coding-task', {
                detail: { mode: 'coding_dev', name: 'First task', autoCreate: true },
            }));
        });
        await screen.findByTestId('task-autocreate-progress');

        act(() => {
            window.dispatchEvent(new CustomEvent('ai-open-create-coding-task', {
                detail: { mode: 'remote_coding_dev', name: 'Second remote task', autoCreate: true },
            }));
        });
        expect(await screen.findByRole('dialog', { name: 'Create remote coding task' })).toBeTruthy();

        await act(async () => {
            resolveCreate();
            await Promise.resolve();
        });

        expect(screen.getByRole('dialog', { name: 'Create remote coding task' })).toBeTruthy();
        expect(screen.getByTestId('task-create-guidance')).toBeTruthy();
    });

    it('does not leak a stale create failure into a newer forced dialog', async () => {
        let rejectCreate: (error: Error) => void = () => {};
        const createTask = vi.fn(() => new Promise<void>((_resolve, reject) => {
            rejectCreate = reject;
        }));
        const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});
        renderTaskManagement({ createTask });

        act(() => {
            window.dispatchEvent(new CustomEvent('ai-open-create-coding-task', {
                detail: { mode: 'coding_dev', name: 'First task', autoCreate: true },
            }));
        });
        await screen.findByTestId('task-autocreate-progress');

        act(() => {
            window.dispatchEvent(new CustomEvent('ai-open-create-coding-task', {
                detail: { mode: 'remote_coding_dev', name: 'Second remote task', autoCreate: true },
            }));
        });
        expect(await screen.findByRole('dialog', { name: 'Create remote coding task' })).toBeTruthy();

        await act(async () => {
            rejectCreate(new Error('stale SSH failure'));
            await Promise.resolve();
        });

        expect(screen.getByRole('dialog', { name: 'Create remote coding task' })).toBeTruthy();
        expect(screen.getByTestId('task-create-guidance')).toBeTruthy();
        expect(screen.queryByTestId('create-task-error')).toBeNull();
        consoleError.mockRestore();
    });

    it('opens create dialog prefilled from welcome coding-task event (remote)', async () => {
        renderTaskManagement({
            tasks: [{
                ...baseProject,
                id: 'remote-1',
                name: 'Remote coding',
                project_path: 'D:/work/tasks/remote-1',
                tags: [
                    'remote_coding_dev',
                    'remote_host:10.0.0.8',
                    'remote_user:ubuntu',
                    'remote_port:2222',
                    'remote_workdir:/home/ubuntu/app',
                ],
            }],
        });

        // After param dialog, filled commands usually have no [placeholders] left.
        act(() => {
            window.dispatchEvent(new CustomEvent('ai-open-create-coding-task', {
                detail: { mode: 'remote_coding_dev', name: '排查修复线上故障\n现象：5xx 增多' },
            }));
        });

        await screen.findByTestId('task-create-guidance');
        expect(screen.getByRole('heading', { name: 'Create remote coding task' })).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Remote' }).getAttribute('aria-pressed')).toBe('true');
        expect(screen.getByTestId('remote-coding-fields')).toBeTruthy();
        // Non-secret SSH meta reused from last remote coding task; password stays empty.
        expect((screen.getByLabelText('Host / domain') as HTMLInputElement).value).toBe('10.0.0.8');
        expect((screen.getByLabelText('Port') as HTMLInputElement).value).toBe('2222');
        expect((screen.getByLabelText('Username') as HTMLInputElement).value).toBe('ubuntu');
        expect((screen.getByLabelText('Remote work directory') as HTMLInputElement).value).toBe('/home/ubuntu/app');
        expect((screen.getByLabelText('Password') as HTMLInputElement).value).toBe('');

        // Host/user/workdir prefilled → first empty env field is password.
        await waitFor(() => {
            expect(document.activeElement).toBe(screen.getByLabelText('Password'));
        });
    });

    it('focuses remote host when SSH meta is empty', async () => {
        renderTaskManagement({ tasks: [baseProject] });

        act(() => {
            window.dispatchEvent(new CustomEvent('ai-open-create-coding-task', {
                detail: { mode: 'remote_coding_dev', name: '热修代码' },
            }));
        });

        await screen.findByTestId('task-create-guidance');
        await waitFor(() => {
            expect(document.activeElement).toBe(screen.getByLabelText('Host / domain'));
        });
    });

    it('shows remote SSH fields and creates a remote coding task', () => {
        const createTask = vi.fn();
        renderTaskManagement({ createTask });

        fireEvent.click(screen.getByTitle('Create task'));
        fireEvent.click(document.getElementById('task-management-remote-coding-mode')!);
        expect(screen.getByTestId('remote-coding-fields')).toBeTruthy();
        expect(screen.queryByLabelText('Choose working folder')).toBeNull();
        fireEvent.change(screen.getByLabelText('Host / domain'), { target: { value: '10.0.0.8' } });
        fireEvent.change(screen.getByLabelText('Port'), { target: { value: '22' } });
        fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'ubuntu' } });
        fireEvent.change(screen.getByLabelText('Password'), { target: { value: 's3cret' } });
        fireEvent.change(screen.getByLabelText('Remote work directory'), { target: { value: '/home/ubuntu/app' } });
        fireEvent.click(screen.getByRole('button', { name: 'Create & open' }));

        expect(createTask).toHaveBeenCalledWith('New remote coding task', undefined, 'remote_coding_dev', {
            host: '10.0.0.8',
            port: 22,
            user: 'ubuntu',
            password: 's3cret',
            workDir: '/home/ubuntu/app',
        });
    });

    it('moves manual remote creation to the task-list connection progress', async () => {
        let resolveCreate: () => void = () => {};
        const createTask = vi.fn(() => new Promise<void>((resolve) => {
            resolveCreate = resolve;
        }));
        renderTaskManagement({ createTask });

        fireEvent.click(screen.getByTitle('Create task'));
        fireEvent.click(screen.getByRole('button', { name: 'Remote' }));
        fireEvent.change(screen.getByLabelText('Host / domain'), { target: { value: '10.0.0.8' } });
        fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'ubuntu' } });
        fireEvent.change(screen.getByLabelText('Password'), { target: { value: 's3cret' } });
        fireEvent.change(screen.getByLabelText('Remote work directory'), { target: { value: '/home/ubuntu/app' } });
        fireEvent.click(screen.getByRole('button', { name: 'Create & open' }));

        expect(await screen.findByTestId('task-autocreate-progress')).toBeTruthy();
        expect(screen.queryByRole('dialog', { name: 'Create remote coding task' })).toBeNull();

        await act(async () => {
            resolveCreate();
            await Promise.resolve();
        });
        await waitFor(() => expect(screen.queryByTestId('task-autocreate-progress')).toBeNull());
    });

    it('prefills remote SSH blanks when switching to remote mode in the create dialog', () => {
        renderTaskManagement({
            tasks: [{
                ...baseProject,
                id: 'remote-1',
                tags: [
                    'remote_coding_dev',
                    'remote_host:10.1.1.1',
                    'remote_user:deploy',
                    'remote_port:2200',
                    'remote_workdir:/srv/app',
                ],
            }],
        });

        fireEvent.click(screen.getByTitle('Create task'));
        fireEvent.click(document.getElementById('task-management-remote-coding-mode')!);

        expect((screen.getByLabelText('Host / domain') as HTMLInputElement).value).toBe('10.1.1.1');
        expect((screen.getByLabelText('Port') as HTMLInputElement).value).toBe('2200');
        expect((screen.getByLabelText('Username') as HTMLInputElement).value).toBe('deploy');
        expect((screen.getByLabelText('Remote work directory') as HTMLInputElement).value).toBe('/srv/app');
        expect((screen.getByLabelText('Password') as HTMLInputElement).value).toBe('');
    });

    it('requires remote SSH fields before allowing submit', () => {
        const createTask = vi.fn();
        renderTaskManagement({ createTask });

        fireEvent.click(screen.getByTitle('Create task'));
        fireEvent.click(screen.getByRole('button', { name: 'Remote' }));

        const ok = screen.getByRole('button', { name: 'Create & open' }) as HTMLButtonElement;
        expect(ok.disabled).toBe(true);
        fireEvent.click(ok);
        expect(createTask).not.toHaveBeenCalled();
    });

    it('rejects an invalid remote SSH port instead of silently using the default port', async () => {
        const createTask = vi.fn();
        renderTaskManagement({ createTask });

        fireEvent.click(screen.getByTitle('Create task'));
        fireEvent.click(screen.getByRole('button', { name: 'Remote' }));
        fireEvent.change(screen.getByLabelText('Host / domain'), { target: { value: '10.0.0.8' } });
        fireEvent.change(screen.getByLabelText('Port'), { target: { value: '22abc' } });
        fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'ubuntu' } });
        fireEvent.change(screen.getByLabelText('Password'), { target: { value: 's3cret' } });
        fireEvent.change(screen.getByLabelText('Remote work directory'), { target: { value: '/home/ubuntu/app' } });
        fireEvent.click(screen.getByRole('button', { name: 'Create & open' }));

        expect(createTask).not.toHaveBeenCalled();
        expect((await screen.findByTestId('create-task-error')).textContent).toContain('Port must be a whole number from 1 to 65535.');
    });

    it('does not auto-create a welcome remote task with an invalid SSH port', async () => {
        const createTask = vi.fn();
        renderTaskManagement({ createTask });

        window.dispatchEvent(new CustomEvent('ai-open-create-coding-task', {
            detail: {
                mode: 'remote_coding_dev',
                name: 'Deploy the service',
                autoCreate: true,
                remote: { host: '10.0.0.8', port: 70000, user: 'ubuntu', password: 's3cret', workDir: '/srv/app' },
            },
        }));

        expect(await screen.findByRole('dialog', { name: 'Create remote coding task' })).toBeTruthy();
        expect(createTask).not.toHaveBeenCalled();
        expect(screen.getByTestId('create-task-error').textContent).toContain('Port must be a whole number from 1 to 65535.');
    });

    it('surfaces createTask failures for remote coding', async () => {
        const createTask = vi.fn().mockRejectedValue(new Error('无法连接到远程服务器'));
        renderTaskManagement({ createTask });

        fireEvent.click(screen.getByTitle('Create task'));
        fireEvent.click(document.getElementById('task-management-remote-coding-mode')!);
        fireEvent.change(screen.getByLabelText('Host / domain'), { target: { value: '10.0.0.1' } });
        fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'root' } });
        fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'bad' } });
        fireEvent.change(screen.getByLabelText('Remote work directory'), { target: { value: '/tmp' } });
        fireEvent.click(screen.getByRole('button', { name: 'Create & open' }));

        await waitFor(() => expect(screen.getByTestId('create-task-error').textContent).toContain('无法连接到远程服务器'));
        // Dialog stays open on failure; title reflects remote coding mode.
        expect(screen.getByRole('dialog', { name: 'Create remote coding task' })).toBeTruthy();
        expect(screen.getByTestId('task-create-guidance')).toBeTruthy();
        expect((screen.getByLabelText('Host / domain') as HTMLInputElement).value).toBe('10.0.0.1');
        expect((screen.getByLabelText('Username') as HTMLInputElement).value).toBe('root');
        expect((screen.getByLabelText('Password') as HTMLInputElement).value).toBe('bad');
        expect((screen.getByLabelText('Remote work directory') as HTMLInputElement).value).toBe('/tmp');
        await waitFor(() => {
            expect(document.activeElement).toBe(screen.getByLabelText('Password'));
        });
    });

    it('passes coding mode together with the selected working folder', async () => {
        selectWorkingDirMock.mockResolvedValue('D:/work/selected-folder');
        const createTask = vi.fn();
        renderTaskManagement({ createTask });

        fireEvent.click(screen.getByTitle('Create task'));
        fireEvent.click(screen.getByRole('button', { name: 'Choose working folder' }));
        await waitFor(() => expect(screen.getByText('D:/work/selected-folder')).toBeTruthy());
        fireEvent.click(screen.getByLabelText('Coding'));
        fireEvent.click(screen.getByRole('button', { name: 'Create & open' }));

        expect(createTask).toHaveBeenCalledWith('New local coding task', 'D:/work/selected-folder', 'coding_dev');
    });

    it('passes the selected working folder when creating a task', async () => {
        selectWorkingDirMock.mockResolvedValue('D:/work/selected-folder');
        const createTask = vi.fn();
        renderTaskManagement({ createTask });

        fireEvent.click(screen.getByTitle('Create task'));
        fireEvent.click(screen.getByRole('button', { name: 'Choose working folder' }));

        expect(SelectWorkingDir).toHaveBeenCalledTimes(1);
        await waitFor(() => expect(screen.getByText('D:/work/selected-folder')).toBeTruthy());
        fireEvent.click(screen.getByRole('button', { name: 'Create & open' }));

        expect(createTask).toHaveBeenCalledWith('New task', 'D:/work/selected-folder');
    });

    it('shows a coding task icon and pure-coding badge for coding_dev tags', () => {
        renderTaskManagement({
            tasks: [
                { ...baseProject, id: 'coding-1', name: 'Coding feature', project_path: 'D:/work/tasks/coding-feature', tags: ['coding_dev'] },
            ],
        });

        expect(screen.getByLabelText('Local pure coding environment')).toBeTruthy();
        expect(screen.getByTestId('task-coding-badge').textContent || '').toMatch(/Pure coding|纯编程|純程式/i);
    });

    it('shows Edit remote SSH in context menu for remote coding tasks', () => {
        const setTaskContextMenu = vi.fn();
        renderTaskManagement({
            setTaskContextMenu,
            taskContextMenu: {
                x: 10,
                y: 20,
                projectPath: baseProject.project_path,
                name: baseProject.name,
                pinned: false,
                isRemoteCoding: true,
                tags: ['remote_coding_dev', 'remote_host:10.0.0.8', 'remote_user:ubuntu', 'remote_port:22', 'remote_workdir:/app'],
            },
        });
        expect(screen.getByTestId('task-context-edit-remote-ssh')).toBeTruthy();
        expect(screen.getByText(/Edit remote SSH|编辑远程 SSH|編輯遠端 SSH/i)).toBeTruthy();
    });

    it('hides Edit remote SSH for ordinary tasks', () => {
        renderTaskManagement({
            taskContextMenu: {
                x: 10,
                y: 20,
                projectPath: baseProject.project_path,
                name: baseProject.name,
                pinned: false,
                isRemoteCoding: false,
            },
        });
        expect(screen.queryByTestId('task-context-edit-remote-ssh')).toBeNull();
    });

    it('opens edit remote SSH dialog with host/user/port/workdir and test button', async () => {
        const { GetRemoteCodingTaskMeta, UpdateRemoteCodingTaskMeta, TestRemoteSSHConnection } = await import('../../../../wailsjs/go/main/App');
        renderTaskManagement({
            taskContextMenu: {
                x: 10,
                y: 20,
                projectPath: baseProject.project_path,
                name: 'Remote fix',
                pinned: false,
                isRemoteCoding: true,
                tags: ['remote_coding_dev', 'remote_host:10.0.0.8', 'remote_user:ubuntu', 'remote_port:22', 'remote_workdir:/app'],
            },
        });
        fireEvent.click(screen.getByTestId('task-context-edit-remote-ssh'));
        await waitFor(() => {
            expect(screen.getByTestId('edit-remote-ssh-dialog')).toBeTruthy();
        });
        await waitFor(() => {
            expect(GetRemoteCodingTaskMeta).toHaveBeenCalled();
            expect((screen.getByTestId('edit-remote-host') as HTMLInputElement).value).toBe('10.0.0.8');
        });
        expect((screen.getByTestId('edit-remote-user') as HTMLInputElement).value).toBe('ubuntu');
        expect((screen.getByTestId('edit-remote-port') as HTMLInputElement).value).toBe('22');
        expect((screen.getByTestId('edit-remote-workdir') as HTMLInputElement).value).toBe('/app');
        expect(screen.getByTestId('edit-remote-password')).toBeTruthy();
        expect(screen.getByTestId('edit-remote-test-ssh')).toBeTruthy();

        fireEvent.change(screen.getByTestId('edit-remote-host'), { target: { value: '10.0.0.9' } });
        fireEvent.change(screen.getByTestId('edit-remote-workdir'), { target: { value: '/new/app' } });
        fireEvent.click(screen.getByTestId('edit-remote-save'));
        await waitFor(() => {
            expect(UpdateRemoteCodingTaskMeta).toHaveBeenCalledWith(
                baseProject.project_path,
                '10.0.0.9',
                'ubuntu',
                '/new/app',
                22,
            );
        });
        await waitFor(() => {
            expect(screen.getByTestId('edit-remote-info')).toBeTruthy();
        });

        fireEvent.change(screen.getByTestId('edit-remote-password'), { target: { value: 'secret' } });
        fireEvent.click(screen.getByTestId('edit-remote-test-ssh'));
        await waitFor(() => {
            expect(TestRemoteSSHConnection).toHaveBeenCalledWith(
                '10.0.0.9',
                'ubuntu',
                'secret',
                '/new/app',
                22,
            );
        });
        await waitFor(() => {
            expect(screen.getByTestId('edit-remote-info').textContent || '').toMatch(/SSH|ok|连接|連線/i);
        });
        // Wait until testing flag clears so Close is allowed.
        await waitFor(() => {
            expect((screen.getByTestId('edit-remote-test-ssh') as HTMLButtonElement).disabled).toBe(false);
        });
        fireEvent.click(within(screen.getByTestId('edit-remote-ssh-dialog')).getByRole('button', { name: /Close|关闭|關閉/i }));
        await waitFor(() => {
            expect(screen.queryByTestId('edit-remote-ssh-dialog')).toBeNull();
        });
        await act(async () => {
            await Promise.resolve();
        });
    });

    it('shows a remote pure-coding badge for remote_coding_dev tags', () => {
        renderTaskManagement({
            tasks: [
                {
                    ...baseProject,
                    id: 'remote-1',
                    name: 'Remote fix',
                    project_path: 'D:/work/tasks/remote-fix',
                    tags: ['remote_coding_dev', 'remote_host:10.0.0.8'],
                },
            ],
        });

        expect(screen.getByLabelText('Remote pure coding environment')).toBeTruthy();
        expect(screen.getByTestId('task-remote-coding-badge').textContent || '').toMatch(/Remote coding|远程编程|遠端程式/i);
        expect(screen.getByTestId('task-remote-coding-badge').textContent || '').toContain('10.0.0.8');
    });

    it('shows the maintenance intent for remote diagnosis tasks', () => {
        renderTaskManagement({
            tasks: [{
                ...baseProject,
                id: 'remote-maintenance-1',
                name: 'Diagnose incident',
                project_path: 'D:/work/tasks/remote-maintenance',
                tags: ['remote_coding_dev', 'remote_host:ops.example.test', 'source:remote_ops_diagnosis'],
            }],
        });

        const badge = screen.getByTestId('task-remote-coding-badge');
        expect(badge.textContent || '').toMatch(/Remote maintenance|远程维护|遠端維護/i);
        expect(badge.textContent || '').not.toMatch(/Remote coding|远程编程|遠端程式/i);
        expect(badge.textContent || '').toContain('ops.example.test');
    });

    it('keeps pure-coding badge visible when the coding task is also pinned', () => {
        renderTaskManagement({
            tasks: [
                {
                    ...baseProject,
                    id: 'coding-pinned',
                    name: 'Pinned coding',
                    project_path: 'D:/work/tasks/pinned-coding',
                    pinned: true,
                    tags: ['coding_dev'],
                },
            ],
        });

        expect(screen.getByLabelText('Local pure coding environment')).toBeTruthy();
        expect(screen.getByTestId('task-coding-badge')).toBeTruthy();
    });

    it('marks the create dialog with the current theme', () => {
        renderTaskManagement({ themeMode: 'dark' });

        fireEvent.click(screen.getByTitle('Create task'));

        expect(screen.getByRole('dialog').closest('.modal-backdrop')?.getAttribute('data-ai-theme')).toBe('dark');
    });

    it('falls back to the app theme when the portaled create dialog has no theme prop', () => {
        const appRoot = document.createElement('div');
        appRoot.id = 'App';
        appRoot.setAttribute('data-ai-theme', 'dark');
        document.body.appendChild(appRoot);
        renderTaskManagement({ themeMode: undefined });

        fireEvent.click(screen.getByTitle('Create task'));

        expect(screen.getByRole('dialog').closest('.modal-backdrop')?.getAttribute('data-ai-theme')).toBe('dark');
    });

    it('carries the selected dark scheme into the portaled create dialog', () => {
        const appRoot = document.createElement('div');
        appRoot.id = 'App';
        appRoot.setAttribute('data-ai-theme', 'dark');
        appRoot.setAttribute('data-ai-dark-scheme', 'graphite');
        document.body.appendChild(appRoot);
        renderTaskManagement({ themeMode: 'dark' });

        fireEvent.click(screen.getByTitle('Create task'));

        expect(screen.getByRole('dialog').closest('.modal-backdrop')?.getAttribute('data-ai-dark-scheme')).toBe('graphite');
    });

    it('portals the create dialog outside the sidebar container so the backdrop covers the window', () => {
        const { container } = renderTaskManagement();

        fireEvent.click(screen.getByTitle('Create task'));

        const backdrop = screen.getByRole('dialog').closest('.modal-backdrop');
        expect(backdrop).toBeTruthy();
        expect(backdrop?.parentElement).toBe(document.body);
        expect(container.contains(backdrop)).toBe(false);
    });

    it('raises the create dialog backdrop above app chrome and fixed dropdowns', () => {
        renderTaskManagement();

        fireEvent.click(screen.getByTitle('Create task'));

        const backdrop = screen.getByRole('dialog').closest('.modal-backdrop') as HTMLElement;
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

        expect(screen.queryByRole('dialog')).toBeNull();
    });

    it('does not close the create dialog when a backdrop click starts inside the dialog', () => {
        renderTaskManagement();

        fireEvent.click(screen.getByTitle('Create task'));
        const input = screen.getByRole('dialog');
        const backdrop = input.closest('.modal-backdrop') as HTMLElement;
        const dialog = input.closest('.modal-content') as HTMLElement;

        fireEvent.mouseDown(dialog);
        fireEvent.click(backdrop);

        expect(screen.getByRole('dialog')).toBeTruthy();
    });

    it('creates a default-titled task without a command field', () => {
        const createTask = vi.fn();
        renderTaskManagement({ createTask });

        fireEvent.click(screen.getByTitle('Create task'));
        expect(screen.queryByLabelText('Task command')).toBeNull();
        expect(screen.getByTestId('task-create-guidance')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Create & open' }));

        expect(createTask).toHaveBeenCalledWith('New task');
    });

    it('ignores duplicate create clicks while the task is being created', async () => {
        let resolveCreate: () => void = () => {};
        const createTask = vi.fn(() => new Promise<void>((resolve) => {
            resolveCreate = resolve;
        }));
        renderTaskManagement({ createTask });

        const createButton = screen.getByTitle('Create task');
        fireEvent.click(createButton);
        const submitButton = screen.getByRole('button', { name: 'Create & open' });
        fireEvent.click(submitButton);
        fireEvent.click(submitButton);

        expect(createTask).toHaveBeenCalledTimes(1);
        expect((createButton as HTMLButtonElement).disabled).toBe(true);
        expect((submitButton as HTMLButtonElement).disabled).toBe(true);

        resolveCreate();
        await waitFor(() => expect((createButton as HTMLButtonElement).disabled).toBe(false));
    });

    it('keeps the ungranted create dialog free of cloud workspace controls', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({ enabled: false });
        renderTaskManagement();

        fireEvent.click(screen.getByTitle('Create task'));

        expect(screen.getByRole('dialog', { name: 'Create task' })).toBeTruthy();
        await waitFor(() => expect(cloudWorkspaceEntitlementMock).toHaveBeenCalled());
        expect(screen.queryByTestId('task-workspace-kind')).toBeNull();
        expect(screen.queryByTestId('task-cloud-workspace-list')).toBeNull();
        expect(screen.queryByTestId('task-cloud-workspace-create')).toBeNull();
        expect(document.getElementById('task-working-directory')).toBeTruthy();
        expect(screen.queryByTestId('task-cloud-workspace-hub-banner')).toBeNull();
    });

    it('shows a non-blocking Hub-down banner without faking cloud controls', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: false,
            hub_unavailable: true,
            banner: 'Hub 不可用，云端工作区暂不可用',
        });
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(screen.getByTitle('创建任务'));

        expect(screen.getByRole('dialog', { name: '创建任务' })).toBeTruthy();
        expect((await screen.findByTestId('task-cloud-workspace-hub-banner')).textContent).toBe('Hub 不可用，云端工作区暂不可用');
        expect(screen.queryByTestId('task-workspace-kind')).toBeNull();
        expect(screen.queryByTestId('task-cloud-workspace-list')).toBeNull();
        expect(screen.queryByTestId('task-cloud-workspace-create')).toBeNull();
        expect(document.getElementById('task-working-directory')).toBeTruthy();
    });

    it('shows local/cloud workspace controls when entitlement is granted', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 1,
            workspaces: [{
                id: 'cws_a',
                name: '标书项目',
                used_bytes: 2048,
                updated_at: '2026-08-28T10:00:00Z',
                lease_in_use: true,
                lease_holder: 'other-pc',
            }],
            deleted: [],
        });
        const createTask = vi.fn();
        renderTaskManagement({ lang: 'zh', createTask });

        fireEvent.click(screen.getByTitle('创建任务'));
        expect(await screen.findByTestId('task-workspace-kind')).toBeTruthy();
        expect(screen.getByTestId('task-workspace-kind-local')).toBeTruthy();
        expect(screen.getByTestId('task-workspace-kind-cloud')).toBeTruthy();
        expect(document.getElementById('task-working-directory')).toBeTruthy();
        expect(screen.queryByTestId('task-cloud-workspace-list')).toBeNull();

        fireEvent.click(screen.getByTestId('task-workspace-kind-cloud'));
        expect(await screen.findByTestId('task-cloud-workspace-list')).toBeTruthy();
        expect(screen.getByTestId('task-cloud-workspace-create')).toBeTruthy();
        expect(document.getElementById('task-working-directory')).toBeNull();
        expect(screen.getByText('标书项目')).toBeTruthy();
        expect(screen.getByTestId('task-cloud-workspace-lease').textContent).toContain('占用中（其他设备');
        expect((screen.getByRole('button', { name: '创建并打开' }) as HTMLButtonElement).disabled).toBe(false);

        fireEvent.click(screen.getByRole('button', { name: '创建并打开' }));
        await waitFor(() => {
            expect(createTask).toHaveBeenCalledWith('新建任务', undefined, undefined, undefined, 'cws_a');
        });
    });

    it('hides cloud workspace controls for remote coding even when granted', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 1,
            workspaces: [{ id: 'cws_a', name: '标书项目' }],
            deleted: [],
        });
        const createTask = vi.fn();
        renderTaskManagement({ createTask });

        fireEvent.click(screen.getByTitle('Create task'));
        expect(await screen.findByTestId('task-workspace-kind')).toBeTruthy();
        fireEvent.click(document.getElementById('task-management-remote-coding-mode')!);

        expect(screen.queryByTestId('task-workspace-kind')).toBeNull();
        expect(screen.queryByTestId('task-cloud-workspace-list')).toBeNull();
        expect(screen.queryByTestId('task-cloud-workspace-create')).toBeNull();
        expect(screen.getByTestId('remote-coding-fields')).toBeTruthy();
        expect(createCloudWorkspaceMock).not.toHaveBeenCalled();

        fireEvent.change(screen.getByLabelText('Host / domain'), { target: { value: '10.0.0.8' } });
        fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'ubuntu' } });
        fireEvent.change(screen.getByLabelText('Password'), { target: { value: 's3cret' } });
        fireEvent.change(screen.getByLabelText('Remote work directory'), { target: { value: '/home/ubuntu/app' } });
        fireEvent.click(screen.getByRole('button', { name: 'Create & open' }));

        expect(createTask).toHaveBeenCalledWith('New remote coding task', undefined, 'remote_coding_dev', {
            host: '10.0.0.8',
            port: 22,
            user: 'ubuntu',
            password: 's3cret',
            workDir: '/home/ubuntu/app',
        });
        expect(createTask.mock.calls[0].length).toBe(4);
    });

    it('disables new cloud workspace when quota is reached', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 1,
            used: 1,
            workspaces: [{ id: 'cws_full', name: '已占用' }],
            deleted: [],
        });
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(screen.getByTitle('创建任务'));
        fireEvent.click(await screen.findByTestId('task-workspace-kind-cloud'));
        const createBtn = await screen.findByTestId('task-cloud-workspace-create') as HTMLButtonElement;
        expect(createBtn.disabled).toBe(true);
        expect(createBtn.title).toContain('配额');
        fireEvent.click(createBtn);
        expect(createCloudWorkspaceMock).not.toHaveBeenCalled();
    });

    it('restores a recently deleted cloud workspace', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 0,
            workspaces: [],
            deleted: [{
                id: 'cws_dead',
                name: '旧项目',
                deleted_at: '2026-08-27T10:00:00Z',
                purge_after: '2026-09-03T10:00:00Z',
            }],
        });
        restoreCloudWorkspaceMock.mockResolvedValue({
            id: 'cws_dead',
            name: '旧项目',
            used_bytes: 0,
            updated_at: '2026-08-28T12:00:00Z',
        });
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(screen.getByTitle('创建任务'));
        fireEvent.click(await screen.findByTestId('task-workspace-kind-cloud'));
        expect(await screen.findByTestId('task-cloud-workspace-deleted')).toBeTruthy();
        expect(screen.getByText('旧项目')).toBeTruthy();
        expect(screen.getByText('7 天内可恢复')).toBeTruthy();

        fireEvent.click(screen.getByTestId('task-cloud-workspace-restore'));
        await waitFor(() => {
            expect(restoreCloudWorkspaceMock).toHaveBeenCalledWith('cws_dead');
        });
        await waitFor(() => {
            expect((screen.getByRole('button', { name: '创建并打开' }) as HTMLButtonElement).disabled).toBe(false);
        });
        expect(screen.queryByTestId('task-cloud-workspace-deleted')).toBeNull();
    });
});
