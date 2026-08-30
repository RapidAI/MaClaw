// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { cloudWorkspaceNameMapFromEntitlement, isActiveTaskRow, isProjectTabOpen, SidebarTaskManagement, taskCreationLabel, taskSecondaryLabelFor, workflowStatusForTask } from '../SidebarTaskManagement';
import type { ComponentProps, ReactElement } from 'react';
import { GetProjectScene, OpenFileOrShowInFolder, SelectWorkingDir } from '../../../../wailsjs/go/main/App';
import { EventsEmit } from '../../../../wailsjs/runtime';
import { DialogProvider } from '../../CustomDialog';
import { __resetCloudWorkspaceDisplayNamesForTests, rememberCloudWorkspaceDisplayName } from '../../ai/codingTaskMode';

const {
    getProjectSceneMock,
    openFileOrShowInFolderMock,
    selectWorkingDirMock,
    eventsEmitMock,
    cloudWorkspaceEntitlementMock,
    createCloudWorkspaceMock,
    renameCloudWorkspaceMock,
    deleteCloudWorkspaceMock,
    forceDeleteCloudWorkspaceMock,
    restoreCloudWorkspaceMock,
    restoreCloudWorkspaceTasksMock,
    prepareCloudWorkspaceMock,
} = vi.hoisted(() => {
    return {
        getProjectSceneMock: vi.fn(),
        openFileOrShowInFolderMock: vi.fn(),
        selectWorkingDirMock: vi.fn(),
        eventsEmitMock: vi.fn(),
        cloudWorkspaceEntitlementMock: vi.fn().mockResolvedValue({ enabled: false }),
        createCloudWorkspaceMock: vi.fn(),
        renameCloudWorkspaceMock: vi.fn(),
        deleteCloudWorkspaceMock: vi.fn(),
        forceDeleteCloudWorkspaceMock: vi.fn(),
        restoreCloudWorkspaceMock: vi.fn(),
        restoreCloudWorkspaceTasksMock: vi.fn().mockResolvedValue([]),
        prepareCloudWorkspaceMock: vi.fn().mockResolvedValue({ local_path: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant_default/cws_a' }),
    };
});

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
    ForceDeleteCloudWorkspace: forceDeleteCloudWorkspaceMock,
    RestoreCloudWorkspace: restoreCloudWorkspaceMock,
    RestoreCloudWorkspaceTasks: restoreCloudWorkspaceTasksMock,
    PrepareCloudWorkspace: prepareCloudWorkspaceMock,
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsEmit: eventsEmitMock,
    EventsOn: vi.fn(() => vi.fn()),
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
    const wrap = (node: ReactElement) => <DialogProvider>{node}</DialogProvider>;
    const view = render(wrap(<SidebarTaskManagement {...props} />));
    return { ...props, container: view.container, rerender: (ui: ReactElement) => view.rerender(wrap(ui)) };
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
    forceDeleteCloudWorkspaceMock.mockReset();
    restoreCloudWorkspaceMock.mockReset();
    restoreCloudWorkspaceTasksMock.mockReset();
    restoreCloudWorkspaceTasksMock.mockResolvedValue([]);
    prepareCloudWorkspaceMock.mockReset();
    prepareCloudWorkspaceMock.mockResolvedValue({ local_path: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant_default/cws_a' });
    __resetCloudWorkspaceDisplayNamesForTests();
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

describe('isActiveTaskRow', () => {
    it('highlights the matching project task and ignores other open tabs', () => {
        expect(isActiveTaskRow(baseProject, { projectPath: 'D:\\work\\tasks\\build-dashboard' })).toBe(true);
        expect(isActiveTaskRow(baseProject, { projectPath: 'D:/work/tasks/other' })).toBe(false);
        expect(isActiveTaskRow(baseProject, null)).toBe(false);
        expect(isActiveTaskRow(baseProject, {})).toBe(false);
    });

    it('highlights expert rows by expert id, not by a coincidental project path', () => {
        const expertTask = { ...baseProject, tags: ['task_management', 'source:expert:paper-review'] };
        expect(isActiveTaskRow(expertTask, { expertId: 'paper-review' })).toBe(true);
        expect(isActiveTaskRow(expertTask, { expertId: 'other' })).toBe(false);
        expect(isActiveTaskRow(expertTask, { projectPath: baseProject.project_path })).toBe(false);
        expect(isActiveTaskRow(baseProject, { expertId: 'paper-review' })).toBe(false);
    });

    it('highlights a cloud workspace row even when resume rebound the tab onto a cache path', () => {
        const cloudTask = {
            ...baseProject,
            project_path: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_math',
            tags: ['task_management', 'cloud_workspace:cws_math'],
        };
        expect(isActiveTaskRow(cloudTask, {
            projectPath: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_math/chapter-1.md',
        })).toBe(true);
        expect(isActiveTaskRow(cloudTask, {
            projectPath: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_other',
        })).toBe(false);
        expect(isActiveTaskRow(baseProject, {
            projectPath: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_math',
        })).toBe(false);
        const cacheOnlyOnWorkDir = {
            ...baseProject,
            project_path: 'D:/work/tasks/math-book',
            working_dir: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_math',
        };
        expect(isActiveTaskRow(cacheOnlyOnWorkDir, {
            projectPath: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_math/chapter-1.md',
        })).toBe(true);
        expect(isActiveTaskRow(cacheOnlyOnWorkDir, {
            projectPath: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_math',
        })).toBe(true);
        const taggedOnly = {
            ...baseProject,
            project_path: 'D:/work/tasks/math-book',
            tags: ['task_management', 'cloud_workspace:cws_math'],
        };
        expect(isActiveTaskRow(taggedOnly, {
            projectPath: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_math',
            cloudWorkspaceId: 'cws_math',
        })).toBe(true);
    });
});

describe('taskSecondaryLabelFor', () => {
    const cloudTask = {
        name: '长江学者申请',
        preview: '长江学者申请',
        project_path: 'D:/work/tasks/cloud-named',
        tags: ['task_management', 'cloud_workspace:cws_named'],
    };

    it('keeps a local task preview unchanged', () => {
        expect(taskSecondaryLabelFor(baseProject)).toBe('Recent task preview');
    });

    it('shows the Hub workspace name under a renamed cloud task', () => {
        const names = cloudWorkspaceNameMapFromEntitlement({
            workspaces: [{ id: 'cws_named', name: '长江学者课题申请材料' }],
            deleted: [],
        });
        expect(taskSecondaryLabelFor(cloudTask, names)).toBe('长江学者课题申请材料');
    });

    it('prefers the workspace name over a distinct preview snippet', () => {
        const names = new Map([['cws_named', '标书项目']]);
        expect(taskSecondaryLabelFor({
            ...cloudTask,
            preview: '最近编辑了投标文件',
        }, names)).toBe('标书项目');
    });

    it('reads recently deleted workspace names', () => {
        const names = cloudWorkspaceNameMapFromEntitlement({
            workspaces: [],
            deleted: [{ id: 'cws_named', name: '旧标书工作区' }],
        });
        expect(taskSecondaryLabelFor(cloudTask, names)).toBe('旧标书工作区');
    });

    it('uses the remembered display name when entitlement is empty', () => {
        rememberCloudWorkspaceDisplayName('cws_named', '缓存工作区名');
        expect(taskSecondaryLabelFor(cloudTask, new Map())).toBe('缓存工作区名');
    });

    it('lets an active workspace name win over a deleted alias', () => {
        const names = cloudWorkspaceNameMapFromEntitlement({
            workspaces: [{ id: 'cws_named', name: '当前名' }],
            deleted: [{ id: 'cws_named', name: '已删除名' }],
        });
        expect(names.get('cws_named')).toBe('当前名');
        expect(taskSecondaryLabelFor(cloudTask, names)).toBe('当前名');
    });

    it('does not leak a workspace id or task preview when the Hub name is unknown', () => {
        expect(taskSecondaryLabelFor({
            ...cloudTask,
            name: '新建云端工作区任务',
            preview: '新建云端工作区任务',
            tags: ['task_management', 'cloud_workspace:cws_offline'],
        }, new Map())).toBe('');
        expect(taskSecondaryLabelFor({
            ...cloudTask,
            preview: '最近编辑了投标文件',
        }, new Map())).toBe('');
    });

    it('resolves the workspace name from a cache path when tags are missing', () => {
        const names = new Map([['cws_named', '标书项目']]);
        expect(taskSecondaryLabelFor({
            preview: '长江学者申请',
            project_path: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_named',
        }, names)).toBe('标书项目');
    });

    it('does not show a cache path when the cloud workspace id cannot be parsed', () => {
        expect(taskSecondaryLabelFor({
            preview: '最近编辑了投标文件',
            project_path: 'C:/Users/me/.maclaw/data/cloud-workspaces/',
        })).toBe('');
    });

    it('keeps a local path fallback when the preview is empty', () => {
        expect(taskSecondaryLabelFor({
            preview: '',
            project_path: 'D:/work/tasks/build-dashboard',
        })).toBe('D:/work/tasks/build-dashboard');
    });
});

describe('SidebarTaskManagement', () => {
    it('highlights the task matching the current AI tab and clears it for the local assistant', () => {
        const expertTask = {
            id: 'task-expert',
            name: 'Paper review',
            project_path: 'D:/work/tasks/expert-paper',
            tags: ['task_management', 'source:expert:paper-review'],
        };
        const rendered = renderTaskManagement({
            tasks: [baseProject, expertTask],
            activeAssistantTask: { projectPath: baseProject.project_path },
        });
        const { rerender, container, ...props } = rendered;
        expect(container.querySelectorAll('[data-testid="sidebar-task-row"]').length).toBe(2);

        const rows = screen.getAllByTestId('sidebar-task-row');
        expect(rows[0].getAttribute('data-active')).toBe('true');
        expect(rows[1].getAttribute('data-active')).toBe('false');
        expect(rows[0].querySelector('.sidebar-task-row')?.classList.contains('is-active')).toBe(true);
        expect(rows[0].querySelector('.sidebar-task-row')?.getAttribute('aria-current')).toBe('true');
        expect(rows[0].querySelector('.sidebar-task-row')?.getAttribute('style') || '').toContain('theme-primary');

        rerender(<SidebarTaskManagement {...props} tasks={[baseProject, expertTask]} activeAssistantTask={{ expertId: 'paper-review' }} />);
        const expertRows = screen.getAllByTestId('sidebar-task-row');
        expect(expertRows[0].getAttribute('data-active')).toBe('false');
        expect(expertRows[1].getAttribute('data-active')).toBe('true');

        rerender(<SidebarTaskManagement {...props} tasks={[baseProject, expertTask]} activeAssistantTask={null} />);
        const cleared = screen.getAllByTestId('sidebar-task-row');
        expect(cleared.every((row) => row.getAttribute('data-active') === 'false')).toBe(true);
        expect(cleared.every((row) => !row.querySelector('.sidebar-task-row.is-active'))).toBe(true);
    });

    it('scrolls the current task into view once the matching row mounts', () => {
        const original = HTMLElement.prototype.scrollIntoView;
        const scrollIntoView = vi.fn();
        HTMLElement.prototype.scrollIntoView = scrollIntoView;
        try {
            const rendered = renderTaskManagement({
                tasks: [],
                activeAssistantTask: { projectPath: baseProject.project_path },
            });
            expect(scrollIntoView).not.toHaveBeenCalled();
            const { rerender, container, ...props } = rendered;
            expect(container.querySelector('[data-testid="sidebar-task-row"]')).toBeNull();
            rerender(<SidebarTaskManagement {...props} tasks={[baseProject]} activeAssistantTask={{ projectPath: baseProject.project_path }} />);
            expect(scrollIntoView).toHaveBeenCalled();
            scrollIntoView.mockClear();
            rerender(<SidebarTaskManagement {...props} tasks={[baseProject]} activeAssistantTask={{ projectPath: baseProject.project_path }} taskListVisible={false} />);
            expect(scrollIntoView).not.toHaveBeenCalled();
            rerender(<SidebarTaskManagement {...props} tasks={[baseProject]} activeAssistantTask={{ projectPath: baseProject.project_path }} taskListVisible={true} />);
            expect(scrollIntoView).toHaveBeenCalled();
        } finally {
            HTMLElement.prototype.scrollIntoView = original;
        }
    });

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

    it('reveals the in-app cloud file panel when a cloud workspace task is restored by double-click', async () => {
        const resumeTask = vi.fn().mockResolvedValue(undefined);
        const revealed: Array<{ projectPath: string; workingDir: string }> = [];
        const onReveal = (event: Event) => {
            const detail = (event as CustomEvent<{ projectPath?: string; workingDir?: string }>).detail;
            revealed.push({
                projectPath: String(detail?.projectPath || ''),
                workingDir: String(detail?.workingDir || ''),
            });
        };
        window.addEventListener('ai-reveal-cloud-workspace-files', onReveal);
        try {
            renderTaskManagement({
                lang: 'zh',
                resumeTask,
                tasks: [{
                    ...baseProject,
                    name: '云端工作区任务1',
                    tags: ['task_management', 'cloud_workspace:cws_a'],
                    working_dir: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_a',
                }],
            });
            fireEvent.doubleClick(screen.getByText('云端工作区任务1'));
            await waitFor(() => {
                expect(resumeTask).toHaveBeenCalledWith(baseProject.project_path, expect.objectContaining({
                    project_path: baseProject.project_path,
                    tags: ['task_management', 'cloud_workspace:cws_a'],
                }));
            });
            expect(revealed).toEqual([{
                projectPath: baseProject.project_path,
                workingDir: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_a',
            }]);
        } finally {
            window.removeEventListener('ai-reveal-cloud-workspace-files', onReveal);
        }
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

    it('shows Browse in the context menu for cloud workspace tasks', () => {
        renderTaskManagement({
            lang: 'zh',
            taskContextMenu: {
                x: 10,
                y: 20,
                projectPath: baseProject.project_path,
                name: '云端工作区任务1',
                pinned: false,
                tags: ['cloud_workspace:cws_a'],
            },
        });
        expect(screen.getByTestId('task-context-browse-cloud')).toBeTruthy();
        expect(screen.getByText('浏览')).toBeTruthy();
        expect(screen.queryByTestId('task-context-browse-cloud')?.getAttribute('data-disabled')).toBeNull();
    });

    it('hides Browse for ordinary tasks', () => {
        renderTaskManagement({
            taskContextMenu: {
                x: 10,
                y: 20,
                projectPath: baseProject.project_path,
                name: baseProject.name,
                pinned: false,
            },
        });
        expect(screen.queryByTestId('task-context-browse-cloud')).toBeNull();
    });

    it('pulls the cloud workspace then opens the task so the in-app file browser can show it', async () => {
        const resumeTask = vi.fn();
        renderTaskManagement({
            lang: 'zh',
            resumeTask,
            taskContextMenu: {
                x: 10,
                y: 20,
                projectPath: baseProject.project_path,
                name: '云端工作区任务1',
                pinned: false,
                tags: ['cloud_workspace:cws_a'],
                workingDir: 'C:/stale-cache',
            },
        });
        const revealed: string[] = [];
        const onReveal = (event: Event) => {
            revealed.push(String((event as CustomEvent<{ projectPath?: string }>).detail?.projectPath || ''));
        };
        window.addEventListener('ai-reveal-cloud-workspace-files', onReveal);
        try {
            fireEvent.click(screen.getByTestId('task-context-browse-cloud'));
            await waitFor(() => {
                expect(resumeTask).toHaveBeenCalledWith(baseProject.project_path, expect.objectContaining({
                    project_path: baseProject.project_path,
                    tags: ['cloud_workspace:cws_a'],
                }));
            });
            expect(prepareCloudWorkspaceMock).not.toHaveBeenCalled();
            expect(openFileOrShowInFolderMock).not.toHaveBeenCalled();
            expect(revealed).toEqual([baseProject.project_path]);
        } finally {
            window.removeEventListener('ai-reveal-cloud-workspace-files', onReveal);
        }
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

    it('marks cloud workspace tasks with a dedicated icon and badge', () => {
        renderTaskManagement({
            tasks: [{
                ...baseProject,
                id: 'cloud-1',
                name: 'Cloud task',
                project_path: 'D:/work/tasks/cloud',
                tags: ['task_management', 'cloud_workspace:cws_1'],
            }],
        });

        expect(screen.getByLabelText('Cloud workspace task')).toBeTruthy();
        expect(screen.getByTestId('task-cloud-workspace-badge').textContent || '').toMatch(/Cloud workspace|云端工作区|雲端工作區/i);
        expect(screen.getByTestId('sidebar-task-row').getAttribute('data-task-kind')).toBe('cloud_workspace');
    });

    it('shows the Hub cloud workspace name under a renamed cloud task', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValueOnce({
            enabled: true,
            workspaces: [{ id: 'cws_named', name: '长江学者课题申请材料' }],
        });
        renderTaskManagement({
            lang: 'zh',
            tasks: [{
                ...baseProject,
                id: 'cloud-named',
                name: '长江学者申请',
                preview: '长江学者申请',
                project_path: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_named',
                working_dir: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_named',
                tags: ['task_management', 'cloud_workspace:cws_named'],
            }],
        });

        await waitFor(() => {
            expect(screen.getByTestId('task-secondary-label').textContent).toBe('长江学者课题申请材料');
        });
        const title = screen.getByTestId('sidebar-task-row').querySelector('.sidebar-task-row')?.getAttribute('title') || '';
        expect(title).toContain('长江学者申请');
        expect(title).toContain('长江学者课题申请材料');
        expect(title).not.toMatch(/cloud-workspaces/i);
        expect(title).not.toContain('C:/Users/me/.maclaw');
    });

    it('does not use a cache path as the cloud task title when the name is empty', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValueOnce({
            enabled: true,
            workspaces: [{ id: 'cws_named', name: '标书项目' }],
        });
        renderTaskManagement({
            lang: 'zh',
            tasks: [{
                ...baseProject,
                name: '',
                preview: '',
                project_path: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_named',
                working_dir: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_named',
                tags: ['task_management', 'cloud_workspace:cws_named'],
            }],
        });

        await waitFor(() => {
            expect(screen.getByTestId('sidebar-task-row').textContent || '').toContain('标书项目');
        });
        const row = screen.getByTestId('sidebar-task-row');
        expect(row.textContent || '').not.toMatch(/cloud-workspaces/i);
        expect(screen.queryByTestId('task-secondary-label')).toBeNull();
    });

    it('prefers the workspace name over a meaningful cloud task preview', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValueOnce({
            enabled: true,
            workspaces: [{ id: 'cws_named_preview', name: '标书项目' }],
        });
        renderTaskManagement({
            tasks: [{
                ...baseProject,
                name: '投标跟进',
                preview: '最近编辑了投标文件',
                project_path: 'D:/work/tasks/cloud-named-preview',
                tags: ['task_management', 'cloud_workspace:cws_named_preview'],
            }],
        });

        await waitFor(() => {
            expect(screen.getByTestId('task-secondary-label').textContent).toBe('标书项目');
        });
    });

    it('does not show a workspace id or duplicate title when Hub naming data is unavailable', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValueOnce({ enabled: true, workspaces: [] });
        renderTaskManagement({
            tasks: [{
                ...baseProject,
                name: '新建云端工作区任务',
                preview: '新建云端工作区任务',
                tags: ['task_management', 'cloud_workspace:cws_offline'],
            }],
        });

        await waitFor(() => expect(cloudWorkspaceEntitlementMock).toHaveBeenCalled());
        const row = screen.getByTestId('sidebar-task-row');
        expect(row.textContent || '').toContain('新建云端工作区任务');
        expect(row.textContent || '').not.toContain('cws_offline');
        expect(screen.queryByTestId('task-secondary-label')).toBeNull();
    });

    it('uses a recently deleted Hub workspace name in the task subtitle', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValueOnce({
            enabled: true,
            workspaces: [],
            deleted: [{ id: 'cws_deleted', name: '旧标书工作区' }],
        });
        renderTaskManagement({
            tasks: [{
                ...baseProject,
                name: '长江学者申请',
                preview: '长江学者申请',
                tags: ['task_management', 'cloud_workspace:cws_deleted'],
            }],
        });

        await waitFor(() => {
            expect(screen.getByTestId('task-secondary-label').textContent).toBe('旧标书工作区');
        });
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

    it('shows a disabled cloud task type when entitlement is not granted', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({ enabled: false });
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(screen.getByTitle('创建任务'));

        expect(screen.getByRole('dialog', { name: '创建任务' })).toBeTruthy();
        const cloudBtn = await screen.findByTestId('task-workspace-kind-cloud') as HTMLButtonElement;
        expect(cloudBtn.disabled).toBe(true);
        expect(cloudBtn.getAttribute('aria-label')).toBe('云端工作区');
        await waitFor(() => {
            expect(screen.getByTestId('task-cloud-workspace-denied').textContent).toContain('未开放云端工作区');
        });
        fireEvent.click(cloudBtn);
        expect(screen.queryByTestId('task-cloud-workspace-list')).toBeNull();
        expect(screen.queryByTestId('task-cloud-workspace-create')).toBeNull();
        expect(document.getElementById('task-working-directory')).toBeTruthy();
        expect(screen.queryByTestId('task-cloud-workspace-hub-banner')).toBeNull();
        expect(screen.queryByTestId('task-cloud-overview')).toBeNull();
    });

    it('shows a header-style cloud SVG icon and lists bound versus blank workspaces', async () => {
        const resumeTask = vi.fn().mockResolvedValue(undefined);
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 2,
            workspaces: [
                { id: 'cws_a', name: '标书项目' },
                { id: 'cws_b', name: '空白工作区' },
            ],
            deleted: [],
        });
        renderTaskManagement({
            lang: 'zh',
            resumeTask,
            tasks: [{
                ...baseProject,
                name: '跨设备任务',
                tags: ['task_management', 'cloud_workspace:cws_a'],
            }],
        });

        const cloudButton = await screen.findByTestId('task-cloud-overview');
        const createButton = screen.getByTitle('创建任务');
        const cloudSvg = cloudButton.querySelector('svg');
        const cloudPath = cloudSvg?.querySelector('path');
        expect(cloudSvg).toBeTruthy();
        expect(cloudSvg?.getAttribute('width')).toBe('16');
        expect(cloudPath?.getAttribute('fill')).toBe('none');
        expect(cloudPath?.getAttribute('stroke')).toBe('currentColor');
        expect(cloudButton.style.borderRadius).toBe(createButton.style.borderRadius);
        expect(cloudButton.style.background).toBe(createButton.style.background);
        expect(cloudButton.style.color).toBe(createButton.style.color);
        fireEvent.click(cloudButton);
        expect(await screen.findByTestId('task-cloud-overview-dialog')).toBeTruthy();
        expect(screen.getByTestId('task-cloud-overview-summary').textContent).toBe('现有 2 个工作区 / 最多 5 个 · 1 个已关联任务 · 1 个未关联');
        expect(screen.getByTestId('task-cloud-overview-summary').getAttribute('title')).toBe('Hub 管理员最多允许 5 个云端工作区');
        expect(screen.getByText('打开任务：跨设备任务')).toBeTruthy();
        expect(screen.getByText('未关联任务 · 创建云端任务')).toBeTruthy();
        expect(screen.getByText('删除工作区')).toBeTruthy();

        fireEvent.click(screen.getByTestId('task-cloud-overview-bound'));
        await waitFor(() => {
            expect(resumeTask).toHaveBeenCalledWith(
                baseProject.project_path,
                expect.objectContaining({ name: '跨设备任务' }),
            );
        });
        expect(screen.queryByTestId('task-cloud-overview-dialog')).toBeNull();
    });

    it('omits the hub quota from the overview summary when it is unknown', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            used: 1,
            workspaces: [{ id: 'cws_a', name: '标书项目' }],
            deleted: [],
        });
        renderTaskManagement({
            lang: 'zh',
            tasks: [{
                ...baseProject,
                tags: ['task_management', 'cloud_workspace:cws_a'],
            }],
        });

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        const summary = await screen.findByTestId('task-cloud-overview-summary');
        expect(summary.textContent).toBe('共 1 个工作区 · 1 个已关联任务 · 0 个未关联');
        expect(summary.getAttribute('title')).toBeNull();
    });

    it('opens a cloud create dialog from a blank workspace and can be closed', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 1,
            workspaces: [{ id: 'cws_b', name: '空白工作区' }],
            deleted: [],
        });
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        fireEvent.click(await screen.findByTestId('task-cloud-overview-blank'));
        expect(await screen.findByRole('dialog', { name: '创建云端工作区任务' })).toBeTruthy();
        expect(screen.queryByTestId('task-cloud-overview-dialog')).toBeNull();
        expect(screen.getByTestId('task-workspace-kind-cloud').getAttribute('aria-pressed')).toBe('true');
    });

    it('deletes an unlinked workspace from the overview into recently deleted', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 1,
            workspaces: [{ id: 'cws_b', name: '人工智能数学基础书编写' }],
            deleted: [],
        });
        deleteCloudWorkspaceMock.mockResolvedValue({
            id: 'cws_b',
            name: '人工智能数学基础书编写',
            deleted_at: '2026-08-29T00:00:00Z',
            purge_after: '2026-09-05T00:00:00Z',
        });
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        fireEvent.click(screen.getByTestId('task-cloud-overview-blank-delete'));
        fireEvent.click(screen.getByText('确认删除'));
        await waitFor(() => {
            expect(deleteCloudWorkspaceMock).toHaveBeenCalledWith('cws_b');
        });
        expect(screen.queryByTestId('task-cloud-overview-blank')).toBeNull();
        expect(screen.getByTestId('task-cloud-overview-deleted')).toBeTruthy();
        expect(screen.getByText('人工智能数学基础书编写')).toBeTruthy();
        expect(screen.getByTestId('task-cloud-overview-restore')).toBeTruthy();
    });

    it('restores a recently deleted workspace from the overview', async () => {
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
        });
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        expect(await screen.findByTestId('task-cloud-overview-deleted')).toBeTruthy();
        fireEvent.click(screen.getByTestId('task-cloud-overview-restore'));
        await waitFor(() => {
            expect(restoreCloudWorkspaceMock).toHaveBeenCalledWith('cws_dead');
        });
        await waitFor(() => {
            expect(screen.queryByTestId('task-cloud-overview-deleted')).toBeNull();
        });
        expect(screen.getByTestId('task-cloud-overview-blank')).toBeTruthy();
    });

    it('asks to permanently delete a workspace in a custom dialog instead of window.confirm', async () => {
        const confirmSpy = vi.spyOn(window, 'confirm');
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 0,
            workspaces: [],
            deleted: [{
                id: 'cws_dead',
                name: '人工智能数学基础书编写',
                deleted_at: '2026-08-27T10:00:00Z',
                purge_after: '2026-09-03T10:00:00Z',
            }],
        });
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        const forceDelete = await screen.findByTestId('task-cloud-overview-force-delete');
        expect(forceDelete.getAttribute('aria-label')).toBe('强制删除「人工智能数学基础书编写」');
        fireEvent.click(forceDelete);

        const dialog = await screen.findByRole('dialog', { name: '强制删除' });
        expect(within(dialog).getByText('永久删除「人工智能数学基础书编写」及全部远程文件？此操作不可撤销。')).toBeTruthy();
        expect(confirmSpy).not.toHaveBeenCalled();
        expect(forceDeleteCloudWorkspaceMock).not.toHaveBeenCalled();

        fireEvent.click(within(dialog).getByRole('button', { name: '取消' }));
        await waitFor(() => expect(screen.queryByRole('dialog', { name: '强制删除' })).toBeNull());
        expect(forceDeleteCloudWorkspaceMock).not.toHaveBeenCalled();
        expect(screen.getByTestId('task-cloud-overview-dialog')).toBeTruthy();
        confirmSpy.mockRestore();
    });

    it('names the workspace in the force-delete dialog when the row has no display name', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 0,
            workspaces: [],
            deleted: [{ id: 'cws_anon' }],
        });
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        fireEvent.click(await screen.findByTestId('task-cloud-overview-force-delete'));
        expect(within(await screen.findByRole('dialog', { name: '强制删除' })).getByText('永久删除此工作区及全部远程文件？此操作不可撤销。')).toBeTruthy();
    });

    it('permanently deletes a recently deleted workspace after custom dialog confirmation', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 0,
            workspaces: [],
            deleted: [{
                id: 'cws_dead',
                name: '人工智能数学基础书编写',
                deleted_at: '2026-08-27T10:00:00Z',
                purge_after: '2026-09-03T10:00:00Z',
            }],
        });
        forceDeleteCloudWorkspaceMock.mockResolvedValue(undefined);
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        fireEvent.click(await screen.findByTestId('task-cloud-overview-force-delete'));
        fireEvent.click(within(await screen.findByRole('dialog', { name: '强制删除' })).getByRole('button', { name: '确定' }));

        await waitFor(() => expect(forceDeleteCloudWorkspaceMock).toHaveBeenCalledWith('cws_dead'));
        await waitFor(() => expect(screen.queryByTestId('task-cloud-overview-deleted')).toBeNull());
        expect(screen.getByTestId('task-cloud-overview-dialog')).toBeTruthy();
        expect(screen.queryByTestId('task-cloud-overview-bound')).toBeNull();
        expect(screen.queryByTestId('task-cloud-overview-blank')).toBeNull();
    });

    it('does not purge after confirm if the workspace left recently deleted', async () => {
        const item = { id: 'cws_dead', name: '人工智能数学基础书编写' };
        let finishOverviewReload: (value: unknown) => void = () => {};
        cloudWorkspaceEntitlementMock
            .mockResolvedValueOnce({
                enabled: true,
                quota: 5,
                used: 0,
                workspaces: [],
                deleted: [item],
            })
            .mockImplementationOnce(() => new Promise(resolve => {
                finishOverviewReload = resolve;
            }));
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        fireEvent.click(await screen.findByTestId('task-cloud-overview-force-delete'));
        expect(await screen.findByRole('dialog', { name: '强制删除' })).toBeTruthy();

        await act(async () => {
            finishOverviewReload({
                enabled: true,
                quota: 5,
                used: 0,
                workspaces: [],
                deleted: [],
            });
        });
        await waitFor(() => expect(screen.queryByTestId('task-cloud-overview-deleted')).toBeNull());

        fireEvent.click(within(screen.getByRole('dialog', { name: '强制删除' })).getByRole('button', { name: '确定' }));
        expect(await screen.findByTestId('task-cloud-overview-error')).toBeTruthy();
        expect(screen.getByTestId('task-cloud-overview-error').textContent).toContain('最近删除');
        expect(forceDeleteCloudWorkspaceMock).not.toHaveBeenCalled();
    });

    it('keeps a recently deleted workspace visible when permanent delete fails', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 0,
            workspaces: [],
            deleted: [{
                id: 'cws_dead',
                name: '人工智能数学基础书编写',
            }],
        });
        forceDeleteCloudWorkspaceMock.mockRejectedValue(new Error('purge denied'));
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        fireEvent.click(await screen.findByTestId('task-cloud-overview-force-delete'));
        fireEvent.click(within(await screen.findByRole('dialog', { name: '强制删除' })).getByRole('button', { name: '确定' }));

        expect(await screen.findByTestId('task-cloud-overview-error')).toBeTruthy();
        expect(screen.getByTestId('task-cloud-overview-error').textContent).toContain('purge denied');
        expect(forceDeleteCloudWorkspaceMock).toHaveBeenCalledWith('cws_dead');
        expect(screen.getByTestId('task-cloud-overview-deleted')).toBeTruthy();
        expect(screen.getByText('人工智能数学基础书编写')).toBeTruthy();
    });

    it('keeps the cloud workspace overview open when the force-delete dialog is dismissed with Escape', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 0,
            workspaces: [],
            deleted: [{
                id: 'cws_dead',
                name: '人工智能数学基础书编写',
            }],
        });
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        fireEvent.click(await screen.findByTestId('task-cloud-overview-force-delete'));
        expect(await screen.findByRole('dialog', { name: '强制删除' })).toBeTruthy();

        fireEvent.keyDown(window, { key: 'Escape' });
        await waitFor(() => expect(screen.queryByRole('dialog', { name: '强制删除' })).toBeNull());
        expect(forceDeleteCloudWorkspaceMock).not.toHaveBeenCalled();
        expect(screen.getByTestId('task-cloud-overview-dialog')).toBeTruthy();
    });

    it('does not list a deleted cloud task as an unlinked workspace', async () => {
        const hideTask = vi.fn().mockImplementation(async () => {
            cloudWorkspaceEntitlementMock.mockResolvedValue({
                enabled: true,
                quota: 5,
                used: 0,
                workspaces: [],
                deleted: [{ id: 'cws_a', name: '人工智能数学基础书编写' }],
            });
        });
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 1,
            workspaces: [{ id: 'cws_a', name: '人工智能数学基础书编写' }],
            deleted: [],
        });
        const cloudTask = {
            ...baseProject,
            name: '人工智能数学基础书编写',
            tags: ['task_management', 'cloud_workspace:cws_a'],
        };
        renderTaskManagement({
            lang: 'zh',
            hideTask,
            tasks: [cloudTask],
            taskContextMenu: {
                x: 10,
                y: 20,
                projectPath: cloudTask.project_path,
                name: cloudTask.name,
                pinned: false,
                tags: cloudTask.tags,
            },
        });

        fireEvent.click(screen.getByTestId('task-context-remove'));
        await waitFor(() => expect(hideTask).toHaveBeenCalledWith(cloudTask.project_path));

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        await waitFor(() => {
            expect(screen.queryByTestId('task-cloud-overview-bound')).toBeNull();
            expect(screen.queryByTestId('task-cloud-overview-blank')).toBeNull();
            expect(screen.getByTestId('task-cloud-overview-deleted')).toBeTruthy();
        });
        expect(within(screen.getByTestId('task-cloud-overview-deleted')).getByText('人工智能数学基础书编写')).toBeTruthy();
        expect(screen.queryByTestId('task-cloud-overview-syncing')).toBeNull();
        expect((screen.getByTestId('task-cloud-overview-new') as HTMLButtonElement).disabled).toBe(false);
    });

    it('stops waiting for a restored task after that cloud task is removed', async () => {
        let resolveRestore: (value: unknown[]) => void = () => {};
        restoreCloudWorkspaceTasksMock.mockImplementation(() => new Promise(resolve => {
            resolveRestore = resolve;
        }));
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 1,
            workspaces: [{ id: 'cws_a', name: '人工智能数学基础书编写' }],
            deleted: [],
        });
        const hideTask = vi.fn().mockResolvedValue(undefined);
        const cloudTask = {
            ...baseProject,
            name: '人工智能数学基础书编写',
            tags: ['task_management', 'cloud_workspace:cws_a'],
        };
        renderTaskManagement({
            lang: 'zh',
            hideTask,
            tasks: [cloudTask],
            taskContextMenu: {
                x: 10,
                y: 20,
                projectPath: cloudTask.project_path,
                name: cloudTask.name,
                pinned: false,
                tags: cloudTask.tags,
            },
        });

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        expect(await screen.findByTestId('task-cloud-overview-syncing')).toBeTruthy();
        fireEvent.click(screen.getByTestId('task-cloud-overview-close'));
        fireEvent.click(screen.getByTestId('task-context-remove'));
        await waitFor(() => expect(hideTask).toHaveBeenCalledWith(cloudTask.project_path));
        resolveRestore([{
            name: '人工智能数学基础书编写',
            project_path: cloudTask.project_path,
            tags: ['task_management', 'cloud_workspace:cws_a'],
        }]);

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        await waitFor(() => {
            expect(screen.queryByTestId('task-cloud-overview-syncing')).toBeNull();
        });
        expect((screen.getByTestId('task-cloud-overview-new') as HTMLButtonElement).disabled).toBe(false);
    });

    it('does not restore a deleted workspace from a stale entitlement fetch', async () => {
        let resolveEntitlement: (value: unknown) => void = () => {};
        cloudWorkspaceEntitlementMock.mockImplementationOnce(() => new Promise(resolve => {
            resolveEntitlement = resolve;
        })).mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 0,
            workspaces: [],
            deleted: [{ id: 'cws_a', name: '人工智能数学基础书编写' }],
        });
        const hideTask = vi.fn().mockResolvedValue(undefined);
        const cloudTask = {
            ...baseProject,
            name: '人工智能数学基础书编写',
            tags: ['task_management', 'cloud_workspace:cws_a'],
        };
        const rendered = renderTaskManagement({
            lang: 'zh',
            hideTask,
            tasks: [cloudTask],
            taskContextMenu: {
                x: 10,
                y: 20,
                projectPath: cloudTask.project_path,
                name: cloudTask.name,
                pinned: false,
                tags: cloudTask.tags,
            },
        });

        fireEvent.click(screen.getByTestId('task-context-remove'));
        await waitFor(() => expect(hideTask).toHaveBeenCalledWith(cloudTask.project_path));
        rendered.rerender(
            <SidebarTaskManagement
                lang="zh"
                tasks={[]}
                renamingTaskPath={null}
                setRenamingTaskPath={rendered.setRenamingTaskPath}
                renameValue=""
                setRenameValue={rendered.setRenameValue}
                resumeTask={rendered.resumeTask}
                createTask={rendered.createTask}
                refreshTasks={rendered.refreshTasks}
                taskContextMenu={null}
                setTaskContextMenu={rendered.setTaskContextMenu}
                renameTask={rendered.renameTask}
                pinTask={rendered.pinTask}
                hideTask={hideTask}
            />,
        );
        await act(async () => {
            resolveEntitlement({
                enabled: true,
                quota: 5,
                used: 1,
                workspaces: [{ id: 'cws_a', name: '人工智能数学基础书编写' }],
                deleted: [],
            });
        });

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        await waitFor(() => {
            expect(screen.queryByTestId('task-cloud-overview-blank')).toBeNull();
        });
    });

    it('shows an overview error when deleting an unlinked workspace fails', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 1,
            workspaces: [{ id: 'cws_b', name: '人工智能数学基础书编写' }],
            deleted: [],
        });
        deleteCloudWorkspaceMock.mockRejectedValue(new Error('占用中（其他设备）'));
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        fireEvent.click(await screen.findByTestId('task-cloud-overview-blank-delete'));
        fireEvent.click(screen.getByText('确认删除'));
        expect(await screen.findByTestId('task-cloud-overview-error')).toBeTruthy();
        expect(screen.getByTestId('task-cloud-overview-error').textContent).toContain('占用中');
        expect(screen.getByTestId('task-cloud-overview-blank')).toBeTruthy();
    });

    it('opens a new cloud task dialog from the overview and closes with the close button', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 0,
            workspaces: [],
            deleted: [],
        });
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        fireEvent.click(await screen.findByTestId('task-cloud-overview-close'));
        expect(screen.queryByTestId('task-cloud-overview-dialog')).toBeNull();

        fireEvent.click(screen.getByTestId('task-cloud-overview'));
        fireEvent.click(await screen.findByTestId('task-cloud-overview-new'));
        expect(await screen.findByRole('dialog', { name: '创建云端工作区任务' })).toBeTruthy();
    });

    it('closes the cloud overview with Escape and does not stack it on the create dialog', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 1,
            workspaces: [{ id: 'cws_a', name: '标书项目' }],
            deleted: [],
        });
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        expect(await screen.findByTestId('task-cloud-overview-dialog')).toBeTruthy();
        fireEvent.keyDown(window, { key: 'Escape' });
        expect(screen.queryByTestId('task-cloud-overview-dialog')).toBeNull();

        fireEvent.click(screen.getByTitle('创建任务'));
        expect(await screen.findByRole('dialog', { name: '创建任务' })).toBeTruthy();
        fireEvent.click(screen.getByTestId('task-cloud-overview'));
        expect(await screen.findByTestId('task-cloud-overview-dialog')).toBeTruthy();
        expect(screen.queryByRole('dialog', { name: '创建任务' })).toBeNull();
    });

    it('shows Hub-down and lease state on overview rows', async () => {
        cloudWorkspaceEntitlementMock
            .mockResolvedValueOnce({
                enabled: true,
                quota: 5,
                used: 1,
                workspaces: [{
                    id: 'cws_busy',
                    name: '占用中',
                    lease_in_use: true,
                    lease_holder: 'other-pc',
                }],
                deleted: [],
            })
            .mockRejectedValueOnce(new Error('hub down'));
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        expect(await screen.findByTestId('task-cloud-overview-blank')).toBeTruthy();
        expect(screen.getByTestId('task-cloud-workspace-lease').textContent).toContain('占用中（其他设备');
        expect(await screen.findByTestId('task-cloud-overview-hub-banner')).toBeTruthy();
    });

    it('keeps the overview open when a linked task cannot switch yet', async () => {
        const resumeTask = vi.fn();
        const onTaskSwitchBlocked = vi.fn();
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 1,
            workspaces: [{ id: 'cws_a', name: '标书项目' }],
            deleted: [],
        });
        renderTaskManagement({
            lang: 'zh',
            assistantReady: false,
            onTaskSwitchBlocked,
            resumeTask,
            tasks: [{
                ...baseProject,
                name: '跨设备任务',
                tags: ['task_management', 'cloud_workspace:cws_a'],
            }],
        });

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        fireEvent.click(await screen.findByTestId('task-cloud-overview-bound'));
        expect(screen.getByTestId('task-cloud-overview-dialog')).toBeTruthy();
        expect(resumeTask).not.toHaveBeenCalled();
        expect(onTaskSwitchBlocked).toHaveBeenCalled();
    });

    it('still refreshes restored tasks if the overview or create dialog opens first', async () => {
        let resolveRestore: (value: unknown[]) => void = () => {};
        restoreCloudWorkspaceTasksMock.mockImplementation(() => new Promise(resolve => {
            resolveRestore = resolve;
        }));
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 1,
            workspaces: [{ id: 'cws_a', name: '标书项目' }],
            deleted: [],
        });
        const refreshTasks = vi.fn();
        renderTaskManagement({ lang: 'zh', refreshTasks, tasks: [] });

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        expect(await screen.findByTestId('task-cloud-overview-syncing')).toBeTruthy();
        fireEvent.click(screen.getByTestId('task-cloud-overview-close'));
        fireEvent.click(screen.getByTitle('创建任务'));

        expect(refreshTasks).not.toHaveBeenCalled();
        resolveRestore([]);
        await waitFor(() => expect(refreshTasks).toHaveBeenCalled());
    });

    it('does not create a cloud task from a blank workspace while restore is still syncing', async () => {
        let resolveRestore: (value: unknown[]) => void = () => {};
        restoreCloudWorkspaceTasksMock.mockImplementation(() => new Promise(resolve => {
            resolveRestore = resolve;
        }));
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 1,
            workspaces: [{ id: 'cws_a', name: '标书项目' }],
            deleted: [],
        });
        renderTaskManagement({ lang: 'zh', tasks: [] });

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        const blank = await screen.findByTestId('task-cloud-overview-blank') as HTMLButtonElement;
        const createNew = screen.getByTestId('task-cloud-overview-new') as HTMLButtonElement;
        expect(blank.disabled).toBe(true);
        expect(createNew.disabled).toBe(true);
        fireEvent.click(blank);
        fireEvent.click(createNew);
        expect(screen.queryByRole('dialog', { name: '创建云端工作区任务' })).toBeNull();

        resolveRestore([]);
        await waitFor(() => {
            expect((screen.getByTestId('task-cloud-overview-blank') as HTMLButtonElement).disabled).toBe(false);
        });
    });

    it('keeps blank workspaces disabled until restored tasks land in the list', async () => {
        restoreCloudWorkspaceTasksMock.mockResolvedValue([{
            name: '跨设备任务',
            project_path: 'D:/cloud/cws_a',
            tags: ['task_management', 'cloud_workspace:cws_a'],
        }]);
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 1,
            workspaces: [{ id: 'cws_a', name: '标书项目' }],
            deleted: [],
        });
        const rendered = renderTaskManagement({ lang: 'zh', tasks: [] });

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        expect(await screen.findByTestId('task-cloud-overview-syncing')).toBeTruthy();
        expect((screen.getByTestId('task-cloud-overview-blank') as HTMLButtonElement).disabled).toBe(true);

        rendered.rerender(
            <SidebarTaskManagement
                lang="zh"
                tasks={[{
                    ...baseProject,
                    name: '跨设备任务',
                    project_path: 'D:/cloud/cws_a',
                    tags: ['task_management', 'cloud_workspace:cws_a'],
                }]}
                renamingTaskPath={null}
                setRenamingTaskPath={rendered.setRenamingTaskPath}
                renameValue=""
                setRenameValue={rendered.setRenameValue}
                resumeTask={rendered.resumeTask}
                createTask={rendered.createTask}
                refreshTasks={rendered.refreshTasks}
                taskContextMenu={null}
                setTaskContextMenu={rendered.setTaskContextMenu}
                renameTask={rendered.renameTask}
                pinTask={rendered.pinTask}
                hideTask={rendered.hideTask}
            />,
        );

        await waitFor(() => {
            expect(screen.queryByTestId('task-cloud-overview-syncing')).toBeNull();
        });
        expect(screen.getByTestId('task-cloud-overview-bound')).toBeTruthy();
        expect(screen.queryByTestId('task-cloud-overview-blank')).toBeNull();
    });

    it('waits for restored tasks even when Hub returns PascalCase task rows', async () => {
        restoreCloudWorkspaceTasksMock.mockResolvedValue([{
            Name: '跨设备任务',
            ProjectPath: 'D:/cloud/cws_a',
            WorkingDir: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_a',
            Tags: ['task_management', 'cloud_workspace:cws_a'],
        }]);
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 1,
            workspaces: [{ id: 'cws_a', name: '标书项目' }],
            deleted: [],
        });
        renderTaskManagement({ lang: 'zh', tasks: [] });

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        await waitFor(() => expect(restoreCloudWorkspaceTasksMock).toHaveBeenCalled());
        await act(async () => {
            await Promise.resolve();
        });
        expect(screen.getByTestId('task-cloud-overview-syncing')).toBeTruthy();
        expect((screen.getByTestId('task-cloud-overview-blank') as HTMLButtonElement).disabled).toBe(true);
    });

    it('closes the overview if Hub entitlement is revoked while it is open', async () => {
        cloudWorkspaceEntitlementMock
            .mockResolvedValueOnce({
                enabled: true,
                quota: 5,
                used: 1,
                workspaces: [{ id: 'cws_a', name: '标书项目' }],
                deleted: [],
            })
            .mockResolvedValueOnce({ enabled: false });
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(await screen.findByTestId('task-cloud-overview'));
        await waitFor(() => {
            expect(screen.queryByTestId('task-cloud-overview-dialog')).toBeNull();
            expect(screen.queryByTestId('task-cloud-overview')).toBeNull();
        });
    });

    it('explains an unbound machine instead of a generic grant denial', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({ enabled: false, reason: 'machine_unbound' });
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(screen.getByTitle('创建任务'));
        await waitFor(() => {
            expect(screen.getByTestId('task-cloud-workspace-denied').textContent).toContain('尚未绑定 Hub 用户');
        });
        expect((screen.getByTestId('task-workspace-kind-cloud') as HTMLButtonElement).disabled).toBe(true);
    });

    it('shows a non-blocking Hub-down banner and keeps cloud type visible but disabled', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: false,
            hub_unavailable: true,
            banner: 'Hub 不可用，云端工作区暂不可用',
        });
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(screen.getByTitle('创建任务'));

        expect(screen.getByRole('dialog', { name: '创建任务' })).toBeTruthy();
        expect((await screen.findByTestId('task-cloud-workspace-hub-banner')).textContent).toBe('Hub 不可用，云端工作区暂不可用');
        expect((await screen.findByTestId('task-workspace-kind-cloud') as HTMLButtonElement).disabled).toBe(true);
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
            expect(createTask).toHaveBeenCalledWith('新建云端工作区任务', undefined, undefined, undefined, 'cws_a');
        });
    });

    it('treats cloud workspace as its own type and does not keep local coding mode', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 1,
            workspaces: [{ id: 'cws_a', name: '标书项目' }],
            deleted: [],
        });
        const createTask = vi.fn();
        renderTaskManagement({ lang: 'zh', createTask });

        fireEvent.click(screen.getByTitle('创建任务'));
        fireEvent.click(document.getElementById('task-management-coding-mode')!);
        fireEvent.click(await screen.findByTestId('task-workspace-kind-cloud'));
        expect(screen.getByTestId('task-workspace-kind-cloud').getAttribute('aria-pressed')).toBe('true');
        expect(document.getElementById('task-management-coding-mode')?.getAttribute('aria-pressed')).toBe('false');
        fireEvent.click(screen.getByRole('button', { name: '创建并打开' }));
        await waitFor(() => {
            expect(createTask).toHaveBeenCalledWith('新建云端工作区任务', undefined, undefined, undefined, 'cws_a');
        });
        expect(createTask.mock.calls[0][2]).toBeUndefined();
    });

    it('auto-selects a free cloud workspace ahead of one occupied on another device', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 2,
            workspaces: [
                { id: 'cws_busy', name: '占用中', lease_in_use: true, lease_holder: 'other-pc' },
                { id: 'cws_free', name: '空闲' },
            ],
            deleted: [],
        });
        const createTask = vi.fn();
        renderTaskManagement({ lang: 'zh', createTask });

        fireEvent.click(screen.getByTitle('创建任务'));
        fireEvent.click(await screen.findByTestId('task-workspace-kind-cloud'));
        fireEvent.click(screen.getByRole('button', { name: '创建并打开' }));
        await waitFor(() => {
            expect(createTask).toHaveBeenCalledWith('新建云端工作区任务', undefined, undefined, undefined, 'cws_free');
        });
    });

    it('prefers an unbound cloud workspace; Open existing reopens the bound task', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 2,
            workspaces: [
                { id: 'cws_bound', name: '已绑定' },
                { id: 'cws_free', name: '空闲工作区' },
            ],
            deleted: [],
        });
        const createTask = vi.fn().mockResolvedValue(undefined);
        renderTaskManagement({
            lang: 'zh',
            createTask,
            tasks: [{
                ...baseProject,
                tags: ['task_management', 'cloud_workspace:cws_bound'],
            }],
        });

        fireEvent.click(screen.getByTitle('创建任务'));
        fireEvent.click(await screen.findByTestId('task-workspace-kind-cloud'));
        const workspaceList = await screen.findByTestId('task-cloud-workspace-list');
        expect(within(workspaceList).getByText('空闲工作区')).toBeTruthy();
        expect(screen.getByTestId('task-cloud-workspace-bound').textContent).toContain('已有任务');

        fireEvent.click(within(workspaceList).getByText('已绑定'));
        expect(screen.getByRole('button', { name: '打开现有任务' })).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: '打开现有任务' }));
        await waitFor(() => {
            expect(createTask).toHaveBeenCalledWith('新建云端工作区任务', undefined, undefined, undefined, 'cws_bound');
        });
        expect(createCloudWorkspaceMock).not.toHaveBeenCalled();
        expect(screen.queryByRole('button', { name: '打开现有任务' })).toBeNull();
        expect(await screen.findByTestId('task-list-notice')).toBeTruthy();
        expect(screen.getByTestId('task-list-notice').textContent).toContain('现有任务');
    });

    it('Create & open on a bound workspace provisions a new Hub workspace and opens a new panel', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 1,
            workspaces: [{ id: 'cws_bound', name: '工作区 1' }],
            deleted: [],
        });
        createCloudWorkspaceMock.mockResolvedValue({ id: 'cws_new', name: '工作区 2' });
        const createTask = vi.fn().mockResolvedValue(undefined);
        renderTaskManagement({
            lang: 'zh',
            createTask,
            tasks: [{
                ...baseProject,
                name: '云端工作区任务1',
                tags: ['task_management', 'cloud_workspace:cws_bound'],
                project_path: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_bound',
                working_dir: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_bound',
            }],
        });

        fireEvent.click(screen.getByTitle('创建任务'));
        fireEvent.click(await screen.findByTestId('task-workspace-kind-cloud'));
        expect(within(await screen.findByTestId('task-cloud-workspace-list')).getByText('工作区 1')).toBeTruthy();
        expect(screen.getByTestId('task-cloud-workspace-bound').textContent).toContain('新面板');
        expect(screen.getByRole('button', { name: '打开现有任务' })).toBeTruthy();
        expect((screen.getByRole('button', { name: '创建并打开' }) as HTMLButtonElement).disabled).toBe(false);

        fireEvent.click(screen.getByRole('button', { name: '创建并打开' }));
        await waitFor(() => {
            expect(createCloudWorkspaceMock).toHaveBeenCalledWith('');
        });
        await waitFor(() => {
            expect(createTask).toHaveBeenCalledWith('新建云端工作区任务', undefined, undefined, undefined, 'cws_new');
        });
        expect(screen.queryByTestId('task-list-notice')).toBeNull();
        expect(screen.queryByRole('button', { name: '创建并打开' })).toBeNull();
    });

    it('releases a workspace provisioned for a bound create if opening fails', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 1,
            workspaces: [{ id: 'cws_bound', name: '工作区 1' }],
            deleted: [],
        });
        createCloudWorkspaceMock.mockResolvedValue({ id: 'cws_new', name: '工作区 2' });
        deleteCloudWorkspaceMock.mockResolvedValue({
            id: 'cws_new',
            name: '工作区 2',
            deleted_at: '2026-08-29T00:00:00Z',
        });
        const createTask = vi.fn().mockRejectedValue(new Error('prepare failed'));
        renderTaskManagement({
            lang: 'zh',
            createTask,
            tasks: [{
                ...baseProject,
                name: '云端工作区任务1',
                tags: ['task_management', 'cloud_workspace:cws_bound'],
            }],
        });

        fireEvent.click(screen.getByTitle('创建任务'));
        fireEvent.click(await screen.findByTestId('task-workspace-kind-cloud'));
        fireEvent.click(screen.getByRole('button', { name: '创建并打开' }));
        await waitFor(() => expect(createCloudWorkspaceMock).toHaveBeenCalledWith(''));
        await waitFor(() => expect(deleteCloudWorkspaceMock).toHaveBeenCalledWith('cws_new'));
        expect(await screen.findByTestId('create-task-error')).toBeTruthy();
        const activeRows = screen.queryAllByTestId('task-cloud-workspace-row');
        expect(activeRows.some(row => row.textContent?.includes('工作区 2'))).toBe(false);
    });

    it('treats a cloud cache path as bound even when tags are missing', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 1,
            workspaces: [{ id: 'cws_bound', name: '工作区 1' }],
            deleted: [],
        });
        createCloudWorkspaceMock.mockResolvedValue({ id: 'cws_new', name: '工作区 2' });
        const createTask = vi.fn().mockResolvedValue(undefined);
        renderTaskManagement({
            lang: 'zh',
            createTask,
            tasks: [{
                ...baseProject,
                name: '云端工作区任务1',
                project_path: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_bound',
            }],
        });

        fireEvent.click(screen.getByTitle('创建任务'));
        fireEvent.click(await screen.findByTestId('task-workspace-kind-cloud'));
        expect(await screen.findByTestId('task-cloud-workspace-bound')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: '创建并打开' }));
        await waitFor(() => {
            expect(createTask).toHaveBeenCalledWith('新建云端工作区任务', undefined, undefined, undefined, 'cws_new');
        });
        expect(createCloudWorkspaceMock).toHaveBeenCalledWith('');
        expect(screen.queryByTestId('task-list-notice')).toBeNull();
    });

    it('disables Create & open on a bound workspace when quota is full', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 1,
            used: 1,
            workspaces: [{ id: 'cws_bound', name: '工作区 1' }],
            deleted: [],
        });
        const createTask = vi.fn().mockResolvedValue(undefined);
        renderTaskManagement({
            lang: 'zh',
            createTask,
            tasks: [{
                ...baseProject,
                tags: ['cloud_workspace:cws_bound'],
            }],
        });

        fireEvent.click(screen.getByTitle('创建任务'));
        fireEvent.click(await screen.findByTestId('task-workspace-kind-cloud'));
        expect(await screen.findByRole('button', { name: '打开现有任务' })).toBeTruthy();
        expect((screen.getByRole('button', { name: '创建并打开' }) as HTMLButtonElement).disabled).toBe(true);
        fireEvent.click(screen.getByRole('button', { name: '打开现有任务' }));
        await waitFor(() => {
            expect(createTask).toHaveBeenCalledWith('新建云端工作区任务', undefined, undefined, undefined, 'cws_bound');
        });
        expect(createCloudWorkspaceMock).not.toHaveBeenCalled();
    });

    it('shows sidebar progress while a cloud workspace task is being opened', async () => {
        let resolveCreate: () => void = () => {};
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 1,
            workspaces: [{ id: 'cws_a', name: '标书项目' }],
            deleted: [],
        });
        const createTask = vi.fn(() => new Promise<void>((resolve) => {
            resolveCreate = resolve;
        }));
        renderTaskManagement({ lang: 'zh', createTask });

        fireEvent.click(screen.getByTitle('创建任务'));
        fireEvent.click(await screen.findByTestId('task-workspace-kind-cloud'));
        fireEvent.click(await screen.findByRole('button', { name: '创建并打开' }));
        expect(await screen.findByTestId('task-autocreate-progress')).toBeTruthy();
        expect(screen.getByTestId('task-autocreate-progress').textContent).toContain('正在打开云端工作区');
        expect(screen.queryByRole('button', { name: '创建并打开' })).toBeNull();

        await act(async () => {
            resolveCreate();
        });
        await waitFor(() => expect(screen.queryByTestId('task-autocreate-progress')).toBeNull());
    });

    it('keeps a just-created cloud workspace if a stale entitlement fetch returns later', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 1,
            workspaces: [{ id: 'cws_a', name: '标书项目' }],
            deleted: [],
        });
        createCloudWorkspaceMock.mockResolvedValue({ id: 'cws_new', name: '工作区 2' });
        const createTask = vi.fn().mockResolvedValue(undefined);
        renderTaskManagement({ lang: 'zh', createTask });

        fireEvent.click(screen.getByTitle('创建任务'));
        fireEvent.click(await screen.findByTestId('task-workspace-kind-cloud'));
        expect(await screen.findByText('标书项目')).toBeTruthy();

        let resolveStale: (value: unknown) => void = () => {};
        cloudWorkspaceEntitlementMock.mockImplementation(() => new Promise(resolve => {
            resolveStale = resolve;
        }));
        fireEvent.click(screen.getByTitle('创建任务'));
        fireEvent.click(await screen.findByTestId('task-workspace-kind-cloud'));
        fireEvent.click(screen.getByTestId('task-cloud-workspace-create'));
        expect(await screen.findByText('工作区 2')).toBeTruthy();

        await act(async () => {
            resolveStale({
                enabled: true,
                quota: 5,
                used: 1,
                workspaces: [{ id: 'cws_a', name: '标书项目' }],
                deleted: [],
            });
        });
        expect(screen.getByText('工作区 2')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: '创建并打开' }));
        await waitFor(() => {
            expect(createTask).toHaveBeenCalledWith('新建云端工作区任务', undefined, undefined, undefined, 'cws_new');
        });
    });

    it('shows cloud workspace controls when Wails returns PascalCase entitlement fields', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            Enabled: true,
            Quota: 5,
            Used: 1,
            Workspaces: [{
                ID: 'cws_a',
                Name: '标书项目',
                UsedBytes: 2048,
                UpdatedAt: '2026-08-28T10:00:00Z',
                LeaseInUse: true,
                LeaseHolder: 'other-pc',
            }],
            Deleted: [],
        });
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(screen.getByTitle('创建任务'));
        expect(await screen.findByTestId('task-workspace-kind')).toBeTruthy();
        fireEvent.click(screen.getByTestId('task-workspace-kind-cloud'));
        expect(await screen.findByTestId('task-cloud-workspace-list')).toBeTruthy();
        expect(screen.getByText('标书项目')).toBeTruthy();
        expect(screen.getByTestId('task-cloud-workspace-lease').textContent).toContain('占用中（其他设备');
    });

    it('maps Hub nested lease onto the occupied badge', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 1,
            workspaces: [{
                id: 'cws_a',
                name: '标书项目',
                used_bytes: 2048,
                updated_at: '2026-08-28T10:00:00Z',
                lease: { held: true, machine_id: 'm-other', machine_name: 'other-pc', is_self: false },
            }],
            deleted: [],
        });
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(screen.getByTitle('创建任务'));
        fireEvent.click(await screen.findByTestId('task-workspace-kind-cloud'));
        expect((await screen.findByTestId('task-cloud-workspace-lease')).textContent).toContain('占用中（其他设备：other-pc）');
    });

    it('keeps the cloud type visible for remote coding but does not open a cloud workspace', async () => {
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
        expect(await screen.findByTestId('task-workspace-kind-cloud')).toBeTruthy();
        fireEvent.click(document.getElementById('task-management-remote-coding-mode')!);

        expect((screen.getByTestId('task-workspace-kind-cloud') as HTMLButtonElement).disabled).toBe(false);
        expect(screen.getByTestId('task-workspace-kind-cloud').getAttribute('aria-pressed')).toBe('false');
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

    it('restores cloud workspace tasks into the list after entitlement loads', async () => {
        const refreshTasks = vi.fn();
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 1,
            workspaces: [{ id: 'cws_a', name: '标书项目', task_name: '跨设备任务', task_mode: 'coding_dev' }],
            deleted: [],
        });
        restoreCloudWorkspaceTasksMock.mockResolvedValue([{
            project_path: 'C:/Users/me/.maclaw/data/tasks/cloud-1',
            name: '跨设备任务',
            tags: ['task_management', 'cloud_workspace:cws_a', 'coding_dev'],
        }]);
        renderTaskManagement({ lang: 'zh', refreshTasks });

        await waitFor(() => {
            expect(restoreCloudWorkspaceTasksMock).toHaveBeenCalled();
        });
        await waitFor(() => {
            expect(refreshTasks).toHaveBeenCalled();
        });
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

    it('treats the workspace list length as used when Hub omits used', async () => {
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 1,
            workspaces: [{ id: 'cws_full', name: '已占用' }],
            deleted: [],
        });
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(screen.getByTitle('创建任务'));
        fireEvent.click(await screen.findByTestId('task-workspace-kind-cloud'));
        const createBtn = await screen.findByTestId('task-cloud-workspace-create') as HTMLButtonElement;
        expect(createBtn.disabled).toBe(true);
        expect(createBtn.title).toContain('配额');
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

    it('force-deletes a recently deleted workspace from the create dialog with a custom confirm', async () => {
        const confirmSpy = vi.spyOn(window, 'confirm');
        cloudWorkspaceEntitlementMock.mockResolvedValue({
            enabled: true,
            quota: 5,
            used: 0,
            workspaces: [],
            deleted: [{
                id: 'cws_dead',
                name: '旧项目',
            }],
        });
        forceDeleteCloudWorkspaceMock.mockResolvedValue(undefined);
        renderTaskManagement({ lang: 'zh' });

        fireEvent.click(screen.getByTitle('创建任务'));
        fireEvent.click(await screen.findByTestId('task-workspace-kind-cloud'));
        fireEvent.click(await screen.findByTestId('task-cloud-workspace-force-delete'));

        const dialog = await screen.findByRole('dialog', { name: '强制删除' });
        expect(within(dialog).getByText('永久删除「旧项目」及全部远程文件？此操作不可撤销。')).toBeTruthy();
        expect(confirmSpy).not.toHaveBeenCalled();
        fireEvent.click(within(dialog).getByRole('button', { name: '确定' }));

        await waitFor(() => expect(forceDeleteCloudWorkspaceMock).toHaveBeenCalledWith('cws_dead'));
        await waitFor(() => expect(screen.queryByTestId('task-cloud-workspace-deleted')).toBeNull());
        expect(screen.getByRole('dialog', { name: '创建云端工作区任务' })).toBeTruthy();
        confirmSpy.mockRestore();
    });
});
