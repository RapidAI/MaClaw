// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { isProjectTabOpen, SidebarTaskManagement } from '../SidebarTaskManagement';
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
    GetRemoteCodingTaskMeta: vi.fn().mockResolvedValue({ host: '10.0.0.8', user: 'ubuntu', port: 22, work_dir: '/app' }),
    UpdateRemoteCodingTaskMeta: vi.fn().mockResolvedValue(undefined),
    TestRemoteSSHConnection: vi.fn().mockResolvedValue('SSH ok'),
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

afterEach(async () => {
    // Flush pending openEditRemoteDialog / save / test microtasks so unmount is quiet.
    await act(async () => {
        await Promise.resolve();
    });
    eventsEmitMock.mockClear();
    selectWorkingDirMock.mockReset();
    getProjectSceneMock.mockReset();
    openFileOrShowInFolderMock.mockReset();
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

    it('exposes local and remote coding modes in the create dialog footer', () => {
        renderTaskManagement();

        fireEvent.click(screen.getByTitle('Create task'));

        const codingToggle = screen.getByRole('button', { name: 'Coding' });
        const remoteToggle = screen.getByRole('button', { name: 'Remote' });
        expect(codingToggle).toBeTruthy();
        expect(remoteToggle).toBeTruthy();
        expect(codingToggle.getAttribute('aria-pressed')).toBe('false');
        expect(remoteToggle.getAttribute('aria-pressed')).toBe('false');
        expect(codingToggle.closest('.modal-footer')).toBeTruthy();
    });

    it('creates a coding development task when the coding option is selected', () => {
        const createTask = vi.fn();
        renderTaskManagement({ createTask });

        fireEvent.click(screen.getByTitle('Create task'));
        fireEvent.change(screen.getByLabelText('Task command'), { target: { value: 'Implement login page' } });
        fireEvent.click(screen.getByRole('button', { name: 'Coding' }));
        fireEvent.click(screen.getByRole('button', { name: 'OK' }));

        expect(createTask).toHaveBeenCalledWith('Implement login page', undefined, 'coding_dev');
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

        const commandInput = await screen.findByLabelText('Task command') as HTMLTextAreaElement;
        expect(commandInput.value).toContain('按需求实现功能');
        expect(screen.getByRole('heading', { name: 'Create local coding task' })).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Coding' }).getAttribute('aria-pressed')).toBe('true');
        expect(screen.getByRole('button', { name: 'Remote' }).getAttribute('aria-pressed')).toBe('false');
        // Prefers last coding task workdir
        expect(screen.getByTitle('D:/work/coding-project')).toBeTruthy();

        // Local coding: focus the command box for review (no [placeholder] selection).
        await waitFor(() => {
            expect(document.activeElement).toBe(commandInput);
        });

        fireEvent.click(screen.getByRole('button', { name: 'OK' }));
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
        expect((screen.getByLabelText('Task command') as HTMLTextAreaElement).value).toBe('Second task');
        // No second auto-create while the first is in flight.
        expect(createTask).toHaveBeenCalledTimes(1);
        // Controls reflect the in-flight create, then unlock when it settles.
        expect((screen.getByRole('button', { name: 'OK' }) as HTMLButtonElement).disabled).toBe(true);
        await act(async () => {
            resolveCreate();
            await Promise.resolve();
        });
        await waitFor(() => {
            expect((screen.getByRole('button', { name: 'OK' }) as HTMLButtonElement).disabled).toBe(false);
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
        expect((screen.getByLabelText('Task command') as HTMLTextAreaElement).value).toBe('Second remote task');
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
        expect((screen.getByLabelText('Task command') as HTMLTextAreaElement).value).toBe('Second remote task');
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

        const commandInput = await screen.findByLabelText('Task command') as HTMLTextAreaElement;
        expect(commandInput.value).toContain('排查修复线上故障');
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

        await screen.findByLabelText('Task command');
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

        fireEvent.change(screen.getByLabelText('Task command'), { target: { value: 'Fix remote auth bug' } });
        fireEvent.change(screen.getByLabelText('Host / domain'), { target: { value: '10.0.0.8' } });
        fireEvent.change(screen.getByLabelText('Port'), { target: { value: '22' } });
        fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'ubuntu' } });
        fireEvent.change(screen.getByLabelText('Password'), { target: { value: 's3cret' } });
        fireEvent.change(screen.getByLabelText('Remote work directory'), { target: { value: '/home/ubuntu/app' } });
        fireEvent.click(screen.getByRole('button', { name: 'OK' }));

        expect(createTask).toHaveBeenCalledWith('Fix remote auth bug', undefined, 'remote_coding_dev', {
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
        fireEvent.change(screen.getByLabelText('Task command'), { target: { value: 'Fix remote auth bug' } });
        fireEvent.change(screen.getByLabelText('Host / domain'), { target: { value: '10.0.0.8' } });
        fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'ubuntu' } });
        fireEvent.change(screen.getByLabelText('Password'), { target: { value: 's3cret' } });
        fireEvent.change(screen.getByLabelText('Remote work directory'), { target: { value: '/home/ubuntu/app' } });
        fireEvent.click(screen.getByRole('button', { name: 'OK' }));

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
        fireEvent.change(screen.getByLabelText('Task command'), { target: { value: 'Incomplete remote' } });

        const ok = screen.getByRole('button', { name: 'OK' }) as HTMLButtonElement;
        expect(ok.disabled).toBe(true);
        fireEvent.click(ok);
        expect(createTask).not.toHaveBeenCalled();
    });

    it('surfaces createTask failures for remote coding', async () => {
        const createTask = vi.fn().mockRejectedValue(new Error('无法连接到远程服务器'));
        renderTaskManagement({ createTask });

        fireEvent.click(screen.getByTitle('Create task'));
        fireEvent.click(document.getElementById('task-management-remote-coding-mode')!);
        fireEvent.change(screen.getByLabelText('Task command'), { target: { value: 'Remote fail' } });
        fireEvent.change(screen.getByLabelText('Host / domain'), { target: { value: '10.0.0.1' } });
        fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'root' } });
        fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'bad' } });
        fireEvent.change(screen.getByLabelText('Remote work directory'), { target: { value: '/tmp' } });
        fireEvent.click(screen.getByRole('button', { name: 'OK' }));

        await waitFor(() => expect(screen.getByTestId('create-task-error').textContent).toContain('无法连接到远程服务器'));
        // Dialog stays open on failure; title reflects remote coding mode.
        expect(screen.getByRole('dialog', { name: 'Create remote coding task' })).toBeTruthy();
        expect((screen.getByLabelText('Task command') as HTMLTextAreaElement).value).toBe('Remote fail');
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

        fireEvent.change(screen.getByLabelText('Task command'), { target: { value: 'Build feature' } });
        fireEvent.click(screen.getByLabelText('Coding'));
        fireEvent.click(screen.getByRole('button', { name: 'OK' }));

        expect(createTask).toHaveBeenCalledWith('Build feature', 'D:/work/selected-folder', 'coding_dev');
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
