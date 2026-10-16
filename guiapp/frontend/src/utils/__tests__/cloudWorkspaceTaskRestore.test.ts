import { beforeEach, describe, expect, it, vi } from 'vitest';

const restoreCloudWorkspaceTasks = vi.fn();

vi.mock('../../../wailsjs/go/main/App', () => ({
    RestoreCloudWorkspaceTasks: () => restoreCloudWorkspaceTasks(),
}));

import {
    __resetCloudWorkspaceTaskRestoreForTests,
    invalidateCloudWorkspaceTaskRestore,
    restoreCloudWorkspaceTasksShared,
} from '../cloudWorkspaceTaskRestore';

beforeEach(() => {
    restoreCloudWorkspaceTasks.mockReset();
    restoreCloudWorkspaceTasks.mockResolvedValue([]);
    __resetCloudWorkspaceTaskRestoreForTests();
});

describe('restoreCloudWorkspaceTasksShared', () => {
    it('shares one backend call between concurrent triggers', async () => {
        let release: (value: unknown[]) => void = () => {};
        restoreCloudWorkspaceTasks.mockImplementation(() => new Promise(resolve => {
            release = resolve;
        }));
        const first = restoreCloudWorkspaceTasksShared();
        const second = restoreCloudWorkspaceTasksShared();
        // Let the deferred backend invocation assign release before resolving.
        await Promise.resolve();
        release([{ project_path: 'p' }]);
        await expect(first).resolves.toEqual([{ project_path: 'p' }]);
        await expect(second).resolves.toEqual([{ project_path: 'p' }]);
        expect(restoreCloudWorkspaceTasks).toHaveBeenCalledTimes(1);
    });

    it('reuses the settled result within the reuse window', async () => {
        restoreCloudWorkspaceTasks.mockResolvedValue([{ project_path: 'p' }]);
        await restoreCloudWorkspaceTasksShared();
        await restoreCloudWorkspaceTasksShared();
        expect(restoreCloudWorkspaceTasks).toHaveBeenCalledTimes(1);
    });

    it('restores again after the reuse window passes', async () => {
        vi.useFakeTimers();
        try {
            restoreCloudWorkspaceTasks.mockResolvedValue([]);
            await restoreCloudWorkspaceTasksShared();
            vi.setSystemTime(Date.now() + 31_000);
            await restoreCloudWorkspaceTasksShared();
            expect(restoreCloudWorkspaceTasks).toHaveBeenCalledTimes(2);
        } finally {
            vi.useRealTimers();
        }
    });

    it('does not cache failures, so the next trigger retries', async () => {
        restoreCloudWorkspaceTasks.mockRejectedValueOnce(new Error('hub down'));
        await expect(restoreCloudWorkspaceTasksShared()).rejects.toThrow('hub down');
        restoreCloudWorkspaceTasks.mockResolvedValue([]);
        await expect(restoreCloudWorkspaceTasksShared()).resolves.toEqual([]);
        expect(restoreCloudWorkspaceTasks).toHaveBeenCalledTimes(2);
    });

    it('drops a settled result on invalidation', async () => {
        restoreCloudWorkspaceTasks.mockResolvedValue([{ project_path: 'old' }]);
        await restoreCloudWorkspaceTasksShared();
        invalidateCloudWorkspaceTaskRestore();
        restoreCloudWorkspaceTasks.mockResolvedValue([{ project_path: 'new' }]);
        await expect(restoreCloudWorkspaceTasksShared()).resolves.toEqual([{ project_path: 'new' }]);
        expect(restoreCloudWorkspaceTasks).toHaveBeenCalledTimes(2);
    });

    it('does not cache an in-flight result that settles after invalidation', async () => {
        let release: (value: unknown[]) => void = () => {};
        restoreCloudWorkspaceTasks.mockImplementation(() => new Promise(resolve => {
            release = resolve;
        }));
        const stale = restoreCloudWorkspaceTasksShared();
        await Promise.resolve();
        invalidateCloudWorkspaceTaskRestore();
        release([{ project_path: 'pre-mutation' }]);
        await expect(stale).resolves.toEqual([{ project_path: 'pre-mutation' }]);
        // The pre-mutation result must not be served to the next caller.
        restoreCloudWorkspaceTasks.mockResolvedValue([{ project_path: 'post-mutation' }]);
        await expect(restoreCloudWorkspaceTasksShared()).resolves.toEqual([{ project_path: 'post-mutation' }]);
        expect(restoreCloudWorkspaceTasks).toHaveBeenCalledTimes(2);
    });
});
