import { beforeEach, describe, expect, it, vi } from 'vitest';

const list = vi.fn();
const claim = vi.fn();
const release = vi.fn();
const markStarted = vi.fn();
const markFailed = vi.fn();
const createUnified = vi.fn();
const startCloud = vi.fn();

vi.mock('../../../../../wailsjs/go/main/App', () => ({
    ListRunWindowTasks: (...args: unknown[]) => list(...args),
    ClaimRunWindowTask: (...args: unknown[]) => claim(...args),
    ReleaseRunWindowTask: (...args: unknown[]) => release(...args),
    MarkRunWindowTaskStarted: (...args: unknown[]) => markStarted(...args),
    MarkRunWindowTaskFailed: (...args: unknown[]) => markFailed(...args),
    CreateTaskUnified: (...args: unknown[]) => createUnified(...args),
    AbandonUnopenedFreshLatexTask: vi.fn(),
    EnsureCodingWorkbenchArmed: vi.fn(),
    SetTabWorkingDir: vi.fn(),
}));

vi.mock('../../../layout/cloudAgentAssistant', () => ({
    startCloudAgentFromNewTask: (...args: unknown[]) => startCloud(...args),
}));

import { dispatchDueRunWindows, launchRunWindowTask } from '../runWindowDispatch';

const due = {
    id: 'rw-1',
    name: '夜间整理',
    prompt: '整理',
    initialMessage: '整理',
    draft: {
        taskType: 'chat',
        expertId: '',
        expertName: '',
        workflowTemplateId: '',
        workspaceKind: 'local',
        localPath: '',
        cloudWorkspaceId: '',
        cloudName: '',
        remoteHost: '',
        remotePort: 0,
        remoteUser: '',
        remoteWorkDir: '',
        latexTemplateId: '',
        latexTemplateName: '',
    },
    status: 'pending',
    startAt: '2099-03-01T02:00:00+08:00',
    createdAt: '2099-03-01T00:00:00+08:00',
};

const now = new Date('2099-03-01T03:00:00+08:00');

describe('dispatchDueRunWindows', () => {
    beforeEach(() => {
        list.mockReset();
        claim.mockReset();
        release.mockReset();
        markStarted.mockReset();
        markFailed.mockReset();
        createUnified.mockReset();
        startCloud.mockReset();
        list.mockResolvedValue([due]);
        claim.mockResolvedValue(due);
        release.mockResolvedValue(undefined);
        markStarted.mockResolvedValue(undefined);
        markFailed.mockResolvedValue(undefined);
    });

    it('puts the row back when creation fails before a project exists', async () => {
        await dispatchDueRunWindows(async () => {
            throw new Error('创建任务失败');
        }, now);
        expect(release).toHaveBeenCalledWith('rw-1', '创建任务失败');
        expect(markFailed).not.toHaveBeenCalled();
        expect(markStarted).not.toHaveBeenCalled();
        claim.mockClear();
        list.mockResolvedValue([{ ...due, lastError: '创建任务失败' }]);
        await dispatchDueRunWindows(async () => 'D:/tasks/notes', now);
        expect(claim).not.toHaveBeenCalled();
        list.mockResolvedValue([{ ...due, lastError: '' }]);
        await dispatchDueRunWindows(async () => 'D:/tasks/notes', now);
        expect(claim).toHaveBeenCalledWith('rw-1');
    });

    it('marks the row failed after a project exists but tracking does not', async () => {
        const tracked = { ...due, id: 'rw-2' };
        list.mockResolvedValue([tracked]);
        claim.mockResolvedValue(tracked);
        markStarted.mockRejectedValue(new Error('任务不在执行中'));
        await dispatchDueRunWindows(async () => 'D:/tasks/notes', now);
        expect(markStarted).toHaveBeenCalledWith('rw-2', 'D:/tasks/notes');
        expect(markFailed).toHaveBeenCalledWith('rw-2', '任务不在执行中');
        expect(release).not.toHaveBeenCalled();
    });

    it('records one cloud instance and does not launch it again', async () => {
        const instanceID = `cag_${'ab'.repeat(16)}`;
        const cloudDue = {
            ...due,
            id: 'rw-cloud',
            prompt: '整理这个仓库',
            initialMessage: '整理这个仓库',
            draft: { ...due.draft, taskType: 'coding', place: 'cloud', workflowTemplateId: 'coding', expertId: 'expert-1' },
        };
        startCloud.mockResolvedValue({ id: instanceID });
        list.mockResolvedValue([cloudDue]);
        claim.mockResolvedValue(cloudDue);
        await dispatchDueRunWindows(task => launchRunWindowTask(task, 'zh-Hans'), now);
        expect(startCloud).toHaveBeenCalledTimes(1);
        // The wizard picks ride the scheduled launch too — workflow template
        // and expert id must reach startCloudAgentFromNewTask's setup arg.
        expect(startCloud).toHaveBeenCalledWith('整理这个仓库', 'coding', '整理这个仓库', 'zh-Hans',
            expect.objectContaining({ workflowTemplateId: 'coding', expertId: 'expert-1' }));
        expect(markStarted).toHaveBeenCalledWith('rw-cloud', `cloud-agent:${instanceID}`);
        expect(release).not.toHaveBeenCalled();
        expect(markFailed).not.toHaveBeenCalled();
        expect(createUnified).not.toHaveBeenCalled();
        list.mockResolvedValue([{ ...cloudDue, status: 'running', projectPath: `cloud-agent:${instanceID}` }]);
        await dispatchDueRunWindows(task => launchRunWindowTask(task, 'zh-Hans'), now);
        expect(startCloud).toHaveBeenCalledTimes(1);
        expect(claim).toHaveBeenCalledTimes(1);
    });

    it('keeps a created cloud instance when the first instruction was not sent', async () => {
        const instanceID = `cag_${'cd'.repeat(16)}`;
        const cloudDue = {
            ...due,
            id: 'rw-cloud-warn',
            prompt: '整理这个仓库',
            initialMessage: '整理这个仓库',
            draft: { ...due.draft, place: 'cloud' },
        };
        startCloud.mockResolvedValue({ id: instanceID, warning: '云端任务已创建，但首条指令未发出' });
        list.mockResolvedValue([cloudDue]);
        claim.mockResolvedValue(cloudDue);
        await dispatchDueRunWindows(task => launchRunWindowTask(task, 'zh-Hans'), now);
        expect(startCloud).toHaveBeenCalledTimes(1);
        expect(markStarted).not.toHaveBeenCalled();
        expect(release).not.toHaveBeenCalled();
        expect(markFailed).toHaveBeenCalledWith('rw-cloud-warn', '云端任务已创建，但首条指令未发出');
        list.mockResolvedValue([{ ...cloudDue, status: 'failed', lastError: '云端任务已创建，但首条指令未发出' }]);
        await dispatchDueRunWindows(task => launchRunWindowTask(task, 'zh-Hans'), now);
        expect(startCloud).toHaveBeenCalledTimes(1);
    });

    it('puts the cloud row back only when no instance was created', async () => {
        const cloudDue = {
            ...due,
            id: 'rw-cloud-miss',
            prompt: '整理这个仓库',
            draft: { ...due.draft, place: 'cloud' },
        };
        startCloud.mockRejectedValue(new Error('云端运行时不可用'));
        list.mockResolvedValue([cloudDue]);
        claim.mockResolvedValue(cloudDue);
        await dispatchDueRunWindows(task => launchRunWindowTask(task, 'zh-Hans'), now);
        expect(release).toHaveBeenCalledWith('rw-cloud-miss', '云端运行时不可用');
        expect(markStarted).not.toHaveBeenCalled();
        expect(markFailed).not.toHaveBeenCalled();
        list.mockResolvedValue([{ ...cloudDue, lastError: '云端运行时不可用' }]);
        await dispatchDueRunWindows(task => launchRunWindowTask(task, 'zh-Hans'), now);
        expect(claim).toHaveBeenCalledTimes(1);
        expect(startCloud).toHaveBeenCalledTimes(1);
    });
});
