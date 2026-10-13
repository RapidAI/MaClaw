import { beforeEach, describe, expect, it, vi } from 'vitest';

const list = vi.fn();
const claim = vi.fn();
const release = vi.fn();
const markStarted = vi.fn();
const markFailed = vi.fn();

vi.mock('../../../../../wailsjs/go/main/App', () => ({
    ListRunWindowTasks: (...args: unknown[]) => list(...args),
    ClaimRunWindowTask: (...args: unknown[]) => claim(...args),
    ReleaseRunWindowTask: (...args: unknown[]) => release(...args),
    MarkRunWindowTaskStarted: (...args: unknown[]) => markStarted(...args),
    MarkRunWindowTaskFailed: (...args: unknown[]) => markFailed(...args),
    CreateTaskUnified: vi.fn(),
    AbandonUnopenedFreshLatexTask: vi.fn(),
    EnsureCodingWorkbenchArmed: vi.fn(),
    SetTabWorkingDir: vi.fn(),
}));

import { dispatchDueRunWindows } from '../runWindowDispatch';

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
});
