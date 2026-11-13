import { describe, expect, it, vi } from 'vitest';
import {
    defaultTaskDraft,
    isDraftDefault,
    withCloudWorkspace,
    withMachine,
    withExpert,
    withLatexTemplate,
    withLocalWorkspace,
    withRemoteWorkspace,
    withTaskType,
    withWorkflow,
    withWorkspace,
    WORKFLOW_AUTO,
} from '../taskDraft';
import { LATEX_BLANK_TEMPLATE_ID, LATEX_EXPERT_ID } from '../../../../utils/latexTemplates';
import {
    draftToTaskCreateOptions,
    runTaskConfigSend,
    shortTaskLaunchTitle,
    shouldCreateUnifiedTask,
    type ExpertNavigation,
    type TaskConfigSendBindings,
    type TaskLaunchNavigation,
    type UnifiedTaskCreateOptions,
} from '../taskConfigSend';
import type { Mock } from 'vitest';

type MockBindings = {
    createTaskUnified: Mock<(opts: UnifiedTaskCreateOptions) => Promise<{ projectPath: string; warning?: string; ownConversation?: boolean }>>;
    ensureCodingArmed: Mock<(projectPath: string) => Promise<unknown>>;
    openTaskLaunch: Mock<(nav: TaskLaunchNavigation) => void>;
    openExpert: Mock<(nav: ExpertNavigation) => void>;
};

function makeBindings(overrides?: Partial<TaskConfigSendBindings>): MockBindings {
    return {
        createTaskUnified: vi.fn<(opts: UnifiedTaskCreateOptions) => Promise<{ projectPath: string; warning?: string; ownConversation?: boolean }>>().mockResolvedValue({ projectPath: 'D:/tasks/unified-task' }),
        ensureCodingArmed: vi.fn<(projectPath: string) => Promise<unknown>>().mockResolvedValue(undefined),
        openTaskLaunch: vi.fn<(nav: TaskLaunchNavigation) => void>(),
        openExpert: vi.fn<(nav: ExpertNavigation) => void>(),
        ...overrides,
    } as MockBindings;
}

describe('shouldCreateUnifiedTask', () => {
    it('keeps the default draft on the legacy path (never intercepted)', () => {
        expect(shouldCreateUnifiedTask(defaultTaskDraft())).toBe(false);
        // Legacy-compatible variations of "default" also stay untouched.
        expect(shouldCreateUnifiedTask(withLocalWorkspace(defaultTaskDraft(), ''))).toBe(false);
    });

    it('intercepts once any dimension is configured', () => {
        expect(shouldCreateUnifiedTask(withTaskType(defaultTaskDraft(), 'coding'))).toBe(true);
        expect(shouldCreateUnifiedTask(withExpert(defaultTaskDraft(), 'exp-1', '法务专家'))).toBe(true);
        expect(shouldCreateUnifiedTask(withWorkflow(defaultTaskDraft(), 'wf-1'))).toBe(true);
        // 「自动判断」也是显式选择 → 建任务（模板 id 仍为空，语义拦截不变）
        expect(shouldCreateUnifiedTask(withWorkflow(defaultTaskDraft(), WORKFLOW_AUTO))).toBe(true);
        expect(shouldCreateUnifiedTask(withLocalWorkspace(defaultTaskDraft(), 'D:/work'))).toBe(true);
        expect(shouldCreateUnifiedTask(withCloudWorkspace(defaultTaskDraft(), 'cw-1', '云端'))).toBe(true);
    });
});

describe('draftToTaskCreateOptions', () => {
    it('maps chat × default local to chat mode', () => {
        const opts = draftToTaskCreateOptions(' 帮我写周报 ', defaultTaskDraft());
        expect(opts).toMatchObject({
            name: '帮我写周报',
            mode: 'chat',
            workingDir: '',
            cloudWorkspaceId: '',
            expertId: '',
            workflowTemplateId: '',
        });
    });

    it('maps coding × local dir to coding_dev with WorkingDir', () => {
        const draft = withLocalWorkspace(withTaskType(defaultTaskDraft(), 'coding'), 'D:/repo/app');
        const opts = draftToTaskCreateOptions('修复登录 bug', draft);
        expect(opts.mode).toBe('coding_dev');
        expect(opts.workingDir).toBe('D:/repo/app');
        expect(opts.remote).toBeUndefined();
    });

    it('opens the first cloud task on the workspace tab', async () => {
        const bindings = makeBindings();
        const draft = withCloudWorkspace(defaultTaskDraft(), 'cws-math', '数学');
        const result = await runTaskConfigSend({ text: '整理讲义', draft, bindings });
        expect(result.ok).toBe(true);
        expect(bindings.openTaskLaunch).toHaveBeenCalledWith(expect.objectContaining({
            projectPath: 'D:/tasks/unified-task',
            cloudWorkspaceId: 'cws-math',
            initialMessage: '整理讲义',
        }));
        const nav = bindings.openTaskLaunch.mock.calls[0][0];
        expect(nav.fileWorkspaceId).toBeUndefined();
    });

    it('opens a second task on the same cloud workspace as its own chat', async () => {
        const bindings = makeBindings();
        bindings.createTaskUnified.mockResolvedValue({
            projectPath: 'D:/tasks/second-cloud',
            ownConversation: true,
        });
        const draft = withCloudWorkspace(defaultTaskDraft(), 'cws-math', '数学');
        const result = await runTaskConfigSend({ text: '再出一份习题', draft, bindings });
        expect(result.ok).toBe(true);
        expect(bindings.openTaskLaunch).toHaveBeenCalledWith(expect.objectContaining({
            projectPath: 'D:/tasks/second-cloud',
            initialMessage: '再出一份习题',
        }));
        const nav = bindings.openTaskLaunch.mock.calls[0][0];
        expect(nav.cloudWorkspaceId).toBeUndefined();
        expect(nav.fileWorkspaceId).toBe('cws-math');
    });

    it('maps chat × cloud to cloud mode with CloudWorkspaceID', () => {
        const draft = withCloudWorkspace(defaultTaskDraft(), 'cw-9', '云端工作区');
        const opts = draftToTaskCreateOptions('整理文档', draft);
        expect(opts.mode).toBe('cloud');
        expect(opts.cloudWorkspaceId).toBe('cw-9');
    });

    it('maps remote workspaces to remote_coding_dev with the Remote structure', () => {
        const draft = withRemoteWorkspace(defaultTaskDraft(), {
            host: '192.168.1.10',
            port: 2222,
            user: 'dev',
            password: 'secret',
            workDir: '/srv/app',
        });
        const opts = draftToTaskCreateOptions('排查线上故障', draft);
        expect(opts.mode).toBe('remote_coding_dev');
        expect(opts.remote).toEqual({
            host: '192.168.1.10',
            port: 2222,
            user: 'dev',
            password: 'secret',
            workDir: '/srv/app',
        });
    });

    it('passes expertId/workflowTemplateId through (mutual exclusion held by the bar)', () => {
        const expertDraft = withExpert(defaultTaskDraft(), 'exp-1', '法务专家');
        expect(draftToTaskCreateOptions('审合同', expertDraft)).toMatchObject({
            mode: 'chat',
            expertId: 'exp-1',
            expertName: '法务专家',
            workflowTemplateId: '',
        });
        const workflowDraft = withWorkflow(defaultTaskDraft(), 'wf-1');
        const opts = draftToTaskCreateOptions('生成 PPT', workflowDraft);
        expect(opts).toMatchObject({ workflowTemplateId: 'wf-1', expertId: '' });
        expect(opts.params).toEqual({});
    });

    it('maps workflow 无 (null) to an empty template id and 自动判断 (auto) likewise', () => {
        // null = 无：不传模板（首条消息由发送层跳过语义拦截）
        expect(draftToTaskCreateOptions('随便聊聊', defaultTaskDraft())).toMatchObject({
            workflowTemplateId: '',
        });
        // 'auto' = 自动判断：空模板 id = 现状语义拦截
        expect(draftToTaskCreateOptions('生成一份 PPT', withWorkflow(defaultTaskDraft(), WORKFLOW_AUTO))).toMatchObject({
            workflowTemplateId: '',
        });
    });
});

describe('runTaskConfigSend interception', () => {
    it('default draft never calls CreateTaskUnified', async () => {
        const bindings = makeBindings();
        const result = await runTaskConfigSend({
            text: '随便聊聊',
            draft: defaultTaskDraft(),
            bindings,
        });
        expect(result.ok).toBe(true);
        expect(bindings.createTaskUnified).not.toHaveBeenCalled();
        expect(bindings.openTaskLaunch).not.toHaveBeenCalled();
        expect(bindings.openExpert).not.toHaveBeenCalled();
    });

    it('coding × local without a directory is blocked before creating', async () => {
        const bindings = makeBindings();
        const draft = withTaskType(defaultTaskDraft(), 'coding');
        const result = await runTaskConfigSend({ text: '修复登录 bug', draft, bindings, isZh: true });
        expect(result.ok).toBe(false);
        if (!result.ok) expect(result.error).toContain('工作目录');
        expect(bindings.createTaskUnified).not.toHaveBeenCalled();
        expect(bindings.openTaskLaunch).not.toHaveBeenCalled();
        expect(bindings.openExpert).not.toHaveBeenCalled();
    });

    it('coding × local with a blank directory is blocked too (trim guard)', async () => {
        const bindings = makeBindings();
        // withWorkspace 直塞空白路径（绕过 withLocalWorkspace 的 trim）也应拦下。
        const draft = withWorkspace(withTaskType(defaultTaskDraft(), 'coding'), { kind: 'local', localPath: '   ' });
        const result = await runTaskConfigSend({ text: '修复登录 bug', draft, bindings });
        expect(result.ok).toBe(false);
        expect(bindings.createTaskUnified).not.toHaveBeenCalled();
    });

    it('chat without a directory still sends (default directory stays allowed for chat)', async () => {
        const bindings = makeBindings();
        const result = await runTaskConfigSend({ text: '随便聊聊', draft: defaultTaskDraft(), bindings, force: true });
        expect(result.ok).toBe(true);
        expect(bindings.createTaskUnified).toHaveBeenCalledTimes(1);
    });

    it('non-default draft calls CreateTaskUnified with mapped options and opens the task launch', async () => {
        const bindings = makeBindings();
        const draft = withLocalWorkspace(withTaskType(defaultTaskDraft(), 'coding'), 'D:/repo/app');
        const result = await runTaskConfigSend({
            text: '修复登录 bug',
            draft,
            bindings,
        });
        expect(result.ok).toBe(true);
        expect(bindings.createTaskUnified).toHaveBeenCalledTimes(1);
        expect(bindings.createTaskUnified).toHaveBeenCalledWith(expect.objectContaining({
            name: '修复登录 bug',
            mode: 'coding_dev',
            workingDir: 'D:/repo/app',
        }));
        expect(bindings.ensureCodingArmed).toHaveBeenCalledWith('D:/tasks/unified-task');
        expect(bindings.openTaskLaunch).toHaveBeenCalledWith(expect.objectContaining({
            projectPath: 'D:/tasks/unified-task',
            taskTitle: '修复登录 bug',
            initialMessage: '修复登录 bug',
            agentMode: 'coding_dev',
        }));
        expect(bindings.openExpert).not.toHaveBeenCalled();
    });

    it('keeps the full first message and stores a short description as the task name', async () => {
        const bindings = makeBindings();
        const body = `第一行很长${"字".repeat(90)}\n第二行也要留下`;
        const draft = withTaskType(defaultTaskDraft(), 'chat');
        const result = await runTaskConfigSend({ text: body, draft, bindings, force: true });
        expect(result.ok).toBe(true);
        const described = shortTaskLaunchTitle(body);
        expect(bindings.createTaskUnified).toHaveBeenCalledWith(expect.objectContaining({ name: described }));
        const launch = bindings.openTaskLaunch.mock.calls[0][0] as TaskLaunchNavigation;
        expect(launch.initialMessage).toBe(body);
        expect(launch.taskTitle).toBe(described);
        expect([...launch.taskTitle].length).toBeLessThanOrEqual(25);
        expect(launch.taskTitle.startsWith("第一行很长")).toBe(true);
        expect(launch.taskTitle.endsWith("…")).toBe(true);
        expect(launch.taskTitle).not.toContain("第二行");
    });

    it('expert draft opens the task record and only names the expert', async () => {
        const bindings = makeBindings();
        const draft = withExpert(defaultTaskDraft(), 'exp-1', '法务专家');
        const result = await runTaskConfigSend({
            text: '审一下这份合同',
            draft,
            bindings,
        });
        expect(result.ok).toBe(true);
        expect(bindings.createTaskUnified).toHaveBeenCalledWith(expect.objectContaining({
            name: '审一下这份合同',
            expertId: 'exp-1',
            expertName: '法务专家',
        }));
        expect(bindings.openExpert).not.toHaveBeenCalled();
        expect(bindings.openTaskLaunch).toHaveBeenCalledWith(expect.objectContaining({
            projectPath: 'D:/tasks/unified-task',
            initialMessage: '审一下这份合同',
            expertId: 'exp-1',
            expertName: '法务专家',
        }));
    });

    it('writes the chosen LaTeX template into the selected directory before opening the expert', async () => {
        const materializeLatexDocument = vi.fn().mockResolvedValue({
            relativePath: 'main.tex',
            initialMessage: '模板已就绪\n\n写一篇论文',
        });
        const bindings = makeBindings({ materializeLatexDocument });
        const draft = withLocalWorkspace(
            withLatexTemplate(withExpert(defaultTaskDraft(), LATEX_EXPERT_ID, 'LaTeX 论文专家'), 'tpl-ieee', 'IEEE'),
            'F:/latex-test',
        );
        const result = await runTaskConfigSend({ text: '写一篇论文', draft, bindings });
        expect(result.ok).toBe(true);
        expect(bindings.createTaskUnified).toHaveBeenCalledWith(expect.objectContaining({
            expertId: LATEX_EXPERT_ID,
            workingDir: 'F:/latex-test',
        }));
        expect(materializeLatexDocument).toHaveBeenCalledWith(expect.objectContaining({
            projectPath: 'D:/tasks/unified-task',
            templateId: 'tpl-ieee',
            templateName: 'IEEE',
            userText: '写一篇论文',
        }));
        expect(bindings.openExpert).not.toHaveBeenCalled();
        expect(bindings.openTaskLaunch).toHaveBeenCalledWith(expect.objectContaining({
            projectPath: 'D:/tasks/unified-task',
            initialMessage: '模板已就绪\n\n写一篇论文',
            expertId: LATEX_EXPERT_ID,
            latexRelativePath: 'main.tex',
        }));
    });

    it('drops the new LaTeX paper when the template cannot be written', async () => {
        const abandonFreshLatexTask = vi.fn().mockResolvedValue(undefined);
        const materializeLatexDocument = vi.fn().mockRejectedValue(new Error('disk full'));
        const bindings = makeBindings({ materializeLatexDocument, abandonFreshLatexTask });
        const draft = withExpert(defaultTaskDraft(), LATEX_EXPERT_ID, 'LaTeX 论文专家');
        const result = await runTaskConfigSend({ text: '写一篇论文', draft, bindings });
        expect(result.ok).toBe(false);
        expect(abandonFreshLatexTask).toHaveBeenCalledWith('D:/tasks/unified-task');
        expect(bindings.openExpert).not.toHaveBeenCalled();
    });

    it('keeps a blank LaTeX template when none was picked', async () => {
        const materializeLatexDocument = vi.fn().mockResolvedValue({
            relativePath: 'main.tex',
            initialMessage: '空白论文',
        });
        const bindings = makeBindings({ materializeLatexDocument });
        const draft = withExpert(defaultTaskDraft(), LATEX_EXPERT_ID, 'LaTeX 论文专家');
        await runTaskConfigSend({ text: '先给提纲', draft, bindings });
        expect(materializeLatexDocument).toHaveBeenCalledWith(expect.objectContaining({
            templateId: LATEX_BLANK_TEMPLATE_ID,
        }));
    });

    it('creation failure reports the error and never navigates', async () => {
        const bindings = makeBindings({
            createTaskUnified: vi.fn().mockRejectedValue(new Error('端口被占用')),
        });
        const draft = withLocalWorkspace(withTaskType(defaultTaskDraft(), 'coding'), 'D:/repo/app');
        const result = await runTaskConfigSend({ text: '改代码', draft, bindings, isZh: true });
        expect(result.ok).toBe(false);
        if (!result.ok) expect(result.error).toContain('端口被占用');
        expect(bindings.openTaskLaunch).not.toHaveBeenCalled();
        expect(bindings.openExpert).not.toHaveBeenCalled();
    });

    it('empty path result is treated as a failure', async () => {
        const bindings = makeBindings({ createTaskUnified: vi.fn().mockResolvedValue({ projectPath: '  ' }) });
        const draft = withLocalWorkspace(withTaskType(defaultTaskDraft(), 'coding'), 'D:/repo/app');
        const result = await runTaskConfigSend({ text: '改代码', draft, bindings, isZh: true });
        expect(result.ok).toBe(false);
    });

    it('path + warning (partial failure) still opens the task and surfaces the warning', async () => {
        const bindings = makeBindings({
            createTaskUnified: vi.fn().mockResolvedValue({
                projectPath: 'D:/tasks/wf-degraded',
                warning: 'workflow engine unavailable',
            }),
        });
        const draft = withWorkflow(defaultTaskDraft(), 'wf-1');
        const result = await runTaskConfigSend({ text: '生成 PPT', draft, bindings });
        expect(result.ok).toBe(true);
        if (result.ok) expect(result.warning).toContain('workflow engine unavailable');
        expect(bindings.openTaskLaunch).toHaveBeenCalledWith(expect.objectContaining({
            projectPath: 'D:/tasks/wf-degraded',
            warning: 'workflow engine unavailable',
        }));
    });

    it('remote prepare failure defers the first message until reconnect', async () => {
        const bindings = makeBindings({
            createTaskUnified: vi.fn().mockResolvedValue({
                projectPath: 'D:/tasks/remote-degraded',
                warning: 'ssh dial failed',
            }),
        });
        const draft = withRemoteWorkspace(defaultTaskDraft(), {
            host: '192.168.1.10',
            port: 22,
            user: 'dev',
            password: 'secret',
            workDir: '/srv/app',
        });
        const result = await runTaskConfigSend({ text: '排查线上故障', draft, bindings });
        expect(result.ok).toBe(true);
        expect(bindings.openTaskLaunch).toHaveBeenCalledWith(expect.objectContaining({
            projectPath: 'D:/tasks/remote-degraded',
            agentMode: 'remote_coding_dev',
            remoteNeedsReconnect: true,
            warning: 'ssh dial failed',
        }));
    });

    it('workflow 无 (null) marks the first message to skip workflow interception', async () => {
        const bindings = makeBindings();
        // 非默认 draft（编程类型），工作流保持「无」
        const draft = withLocalWorkspace(withTaskType(defaultTaskDraft(), 'coding'), 'D:/repo/app');
        const result = await runTaskConfigSend({ text: '修复登录 bug', draft, bindings });
        expect(result.ok).toBe(true);
        expect(bindings.openTaskLaunch).toHaveBeenCalledWith(expect.objectContaining({
            noWorkflowInterception: true,
        }));
    });

    it('workflow 自动判断 does NOT set the skip-interception flag', async () => {
        const bindings = makeBindings();
        const draft = withLocalWorkspace(withTaskType(withWorkflow(defaultTaskDraft(), WORKFLOW_AUTO), 'coding'), 'D:/repo/app');
        const result = await runTaskConfigSend({ text: '做个报表', draft, bindings });
        expect(result.ok).toBe(true);
        const nav = bindings.openTaskLaunch.mock.calls[0][0] as { noWorkflowInterception?: boolean };
        expect(nav.noWorkflowInterception).toBeUndefined();
    });

    it('EnsureCodingWorkbenchArmed failure does not block the launch (record already exists)', async () => {
        const bindings = makeBindings({
            ensureCodingArmed: vi.fn().mockRejectedValue(new Error('workbench busy')),
        });
        const draft = withLocalWorkspace(withTaskType(defaultTaskDraft(), 'coding'), 'D:/repo/app');
        const result = await runTaskConfigSend({ text: '修复登录 bug', draft, bindings });
        expect(result.ok).toBe(true);
        expect(bindings.openTaskLaunch).toHaveBeenCalledWith(expect.objectContaining({
            projectPath: 'D:/tasks/unified-task',
            agentMode: 'coding_dev',
        }));
    });
});

describe('remote machine target', () => {
    const machine = { grantId: 'gb', machineName: 'B机' };

    it('a machine target is a non-default draft routed to unified interception', () => {
        const draft = withMachine(defaultTaskDraft(), machine);
        expect(isDraftDefault(draft)).toBe(false);
        expect(shouldCreateUnifiedTask(draft)).toBe(true);
        expect(draft.machine).toEqual(machine);
        // remote-control create_task cannot carry expert/workflow/workspace/window
        expect(draft.expertId).toBeNull();
        expect(draft.workflowTemplateId).toBeNull();
        expect(draft.workspace.kind).toBe('local');
        expect(draft.runWindow?.mode).toBe('immediate');
    });

    it('withMachine(null) returns to a default local draft', () => {
        const draft = withMachine(withMachine(defaultTaskDraft(), machine), null);
        expect(draft.machine).toBeNull();
        expect(isDraftDefault(draft)).toBe(true);
    });

    it('a machine-target coding task is not blocked by the local-directory rule (create_task carries no directory)', async () => {
        const createRemoteMachineTask = vi.fn().mockResolvedValue({ projectPath: 'D:/remote/t1' });
        const openRemoteMachineTask = vi.fn();
        const bindings = makeBindings({ createRemoteMachineTask, openRemoteMachineTask });
        const draft = withMachine(withTaskType(defaultTaskDraft(), 'coding'), machine);
        const result = await runTaskConfigSend({ text: '整理这个仓库', draft, bindings });
        expect(result.ok).toBe(true);
        expect(createRemoteMachineTask).toHaveBeenCalledTimes(1);
        expect(bindings.createTaskUnified).not.toHaveBeenCalled();
    });

    it('creates on the remote machine and opens it without touching local create', async () => {
        const createRemoteMachineTask = vi.fn().mockResolvedValue({ projectPath: 'D:/remote/t1' });
        const openRemoteMachineTask = vi.fn();
        const bindings = makeBindings({ createRemoteMachineTask, openRemoteMachineTask });
        const draft = withMachine(withTaskType(defaultTaskDraft(), 'coding'), machine);
        const result = await runTaskConfigSend({ text: '整理这个仓库', draft, bindings });
        expect(result.ok).toBe(true);
        expect(createRemoteMachineTask).toHaveBeenCalledWith(expect.objectContaining({
            grantId: 'gb',
            mode: 'coding_dev',
            text: '整理这个仓库',
        }));
        expect(bindings.createTaskUnified).not.toHaveBeenCalled();
        expect(openRemoteMachineTask).toHaveBeenCalledWith({ grantId: 'gb', projectPath: 'D:/remote/t1' });
        expect(bindings.openTaskLaunch).not.toHaveBeenCalled();
    });

    it('chat type maps to chat mode on the remote machine', async () => {
        const createRemoteMachineTask = vi.fn().mockResolvedValue({ projectPath: 'D:/remote/t2' });
        const bindings = makeBindings({ createRemoteMachineTask, openRemoteMachineTask: vi.fn() });
        const draft = withMachine(defaultTaskDraft(), machine);
        const result = await runTaskConfigSend({ text: '写个总结', draft, bindings });
        expect(result.ok).toBe(true);
        expect(createRemoteMachineTask).toHaveBeenCalledWith(expect.objectContaining({ mode: 'chat' }));
    });

    it('first-message failure returns warning but still opens the remote task', async () => {
        const createRemoteMachineTask = vi.fn().mockResolvedValue({
            projectPath: 'D:/remote/t3',
            warning: '任务已在对方电脑创建，但首条指令未发出',
        });
        const openRemoteMachineTask = vi.fn();
        const bindings = makeBindings({ createRemoteMachineTask, openRemoteMachineTask });
        const draft = withMachine(defaultTaskDraft(), machine);
        const result = await runTaskConfigSend({ text: 'hello', draft, bindings });
        expect(result).toMatchObject({ ok: true, warning: '任务已在对方电脑创建，但首条指令未发出' });
        expect(openRemoteMachineTask).toHaveBeenCalledWith({ grantId: 'gb', projectPath: 'D:/remote/t3' });
    });

    it('remote create failure surfaces the error and never opens', async () => {
        const createRemoteMachineTask = vi.fn().mockRejectedValue(new Error('对方离线'));
        const openRemoteMachineTask = vi.fn();
        const bindings = makeBindings({ createRemoteMachineTask, openRemoteMachineTask });
        const draft = withMachine(defaultTaskDraft(), machine);
        const result = await runTaskConfigSend({ text: 'hello', draft, bindings });
        expect(result.ok).toBe(false);
        expect(openRemoteMachineTask).not.toHaveBeenCalled();
        expect(bindings.openTaskLaunch).not.toHaveBeenCalled();
    });
});
