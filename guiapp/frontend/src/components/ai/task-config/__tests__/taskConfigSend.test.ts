import { describe, expect, it, vi } from 'vitest';
import {
    defaultTaskDraft,
    withCloudWorkspace,
    withExpert,
    withLocalWorkspace,
    withRemoteWorkspace,
    withTaskType,
    withWorkflow,
    WORKFLOW_AUTO,
} from '../taskDraft';
import {
    draftToTaskCreateOptions,
    runTaskConfigSend,
    shouldCreateUnifiedTask,
    type ExpertNavigation,
    type TaskConfigSendBindings,
    type TaskLaunchNavigation,
    type UnifiedTaskCreateOptions,
} from '../taskConfigSend';
import type { Mock } from 'vitest';

type MockBindings = {
    createTaskUnified: Mock<(opts: UnifiedTaskCreateOptions) => Promise<{ projectPath: string; warning?: string }>>;
    ensureCodingArmed: Mock<(projectPath: string) => Promise<unknown>>;
    openTaskLaunch: Mock<(nav: TaskLaunchNavigation) => void>;
    openExpert: Mock<(nav: ExpertNavigation) => void>;
};

function makeBindings(overrides?: Partial<TaskConfigSendBindings>): MockBindings {
    return {
        createTaskUnified: vi.fn<(opts: UnifiedTaskCreateOptions) => Promise<{ projectPath: string; warning?: string }>>().mockResolvedValue({ projectPath: 'D:/tasks/unified-task' }),
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
            initialMessage: '修复登录 bug',
            agentMode: 'coding_dev',
        }));
        expect(bindings.openExpert).not.toHaveBeenCalled();
    });

    it('expert draft routes to the expert open chain, not the task launch', async () => {
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
        expect(bindings.openTaskLaunch).not.toHaveBeenCalled();
        expect(bindings.openExpert).toHaveBeenCalledWith(expect.objectContaining({
            id: 'exp-1',
            name: '法务专家',
            initialMessage: '审一下这份合同',
        }));
    });

    it('creation failure reports the error and never navigates', async () => {
        const bindings = makeBindings({
            createTaskUnified: vi.fn().mockRejectedValue(new Error('端口被占用')),
        });
        const draft = withTaskType(defaultTaskDraft(), 'coding');
        const result = await runTaskConfigSend({ text: '改代码', draft, bindings, isZh: true });
        expect(result.ok).toBe(false);
        if (!result.ok) expect(result.error).toContain('端口被占用');
        expect(bindings.openTaskLaunch).not.toHaveBeenCalled();
        expect(bindings.openExpert).not.toHaveBeenCalled();
    });

    it('empty path result is treated as a failure', async () => {
        const bindings = makeBindings({ createTaskUnified: vi.fn().mockResolvedValue({ projectPath: '  ' }) });
        const draft = withTaskType(defaultTaskDraft(), 'coding');
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
        const draft = withTaskType(defaultTaskDraft(), 'coding');
        const result = await runTaskConfigSend({ text: '修复登录 bug', draft, bindings });
        expect(result.ok).toBe(true);
        expect(bindings.openTaskLaunch).toHaveBeenCalledWith(expect.objectContaining({
            noWorkflowInterception: true,
        }));
    });

    it('workflow 自动判断 does NOT set the skip-interception flag', async () => {
        const bindings = makeBindings();
        const draft = withTaskType(withWorkflow(defaultTaskDraft(), WORKFLOW_AUTO), 'coding');
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
