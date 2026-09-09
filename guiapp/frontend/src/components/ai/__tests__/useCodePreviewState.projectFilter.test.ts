import { describe, expect, it } from 'vitest';
import {
    codeFileBelongsToPreviewProject,
    filterCodePreviewStateForProject,
    initialState,
    shouldAcceptCodeEventForProject,
    type CodeFile,
} from '../useCodePreviewState';

describe('shouldAcceptCodeEventForProject', () => {
    it('rejects empty event project path on a bound project tab', () => {
        expect(shouldAcceptCodeEventForProject(undefined, 'D:/proj')).toBe(false);
        expect(shouldAcceptCodeEventForProject('', 'D:/proj')).toBe(false);
        expect(shouldAcceptCodeEventForProject(undefined, 'D:/proj', true)).toBe(false);
    });

    it('accepts empty event project path only on the unbound local tab', () => {
        expect(shouldAcceptCodeEventForProject(undefined, undefined)).toBe(true);
        expect(shouldAcceptCodeEventForProject('', '')).toBe(true);
        expect(shouldAcceptCodeEventForProject(undefined, '   ')).toBe(true);
    });

    it('matches windows paths case-insensitively with slash normalization', () => {
        expect(shouldAcceptCodeEventForProject('D:\\testprj5', 'd:/testprj5')).toBe(true);
        expect(shouldAcceptCodeEventForProject('D:/testprj5/', 'D:\\testprj5')).toBe(true);
    });

    it('accepts worktree paths nested under the active project', () => {
        expect(shouldAcceptCodeEventForProject(
            'D:/repo/.maclaw/worktrees/t1',
            'D:/repo',
        )).toBe(true);
    });

    it('rejects unrelated projects', () => {
        expect(shouldAcceptCodeEventForProject('D:/other', 'D:/repo')).toBe(false);
        // Prefix trap: D:/test must not match D:/testprj5
        expect(shouldAcceptCodeEventForProject('D:/testprj5', 'D:/test')).toBe(false);
    });

    it('requires forceOpen when no active project path', () => {
        expect(shouldAcceptCodeEventForProject('D:/repo', undefined, false)).toBe(false);
        expect(shouldAcceptCodeEventForProject('D:/repo', undefined, true)).toBe(true);
        expect(shouldAcceptCodeEventForProject('D:/repo', '', true)).toBe(true);
    });
});

function previewFile(partial: Partial<CodeFile> & Pick<CodeFile, 'filePath' | 'content'>): CodeFile {
    return {
        fileName: partial.fileName || partial.filePath.split(/[/\\]/).pop() || partial.filePath,
        opType: 'read',
        language: 'cpp',
        updatedAt: 1,
        ...partial,
    };
}

describe('codeFileBelongsToPreviewProject', () => {
    const cloudRoot = 'C:/Users/ma139/.maclaw/data/cloud-workspaces/tenant_default/cws_scholar';

    it('keeps relative explorer files on a cloud workspace tab', () => {
        expect(codeFileBelongsToPreviewProject(
            previewFile({ filePath: 'papers/INDEX.md', content: '# index' }),
            cloudRoot,
        )).toBe(true);
    });

    it('drops remote coding leftovers from a cloud workspace tab', () => {
        expect(codeFileBelongsToPreviewProject(
            previewFile({
                filePath: '/home/testprj-2/src/updater.cpp',
                absPath: '/home/testprj-2/src/updater.cpp',
                content: '#include "updater.h"',
            }),
            cloudRoot,
        )).toBe(false);
    });

    it('drops files stamped with another task identity', () => {
        expect(codeFileBelongsToPreviewProject(
            previewFile({
                filePath: '/home/testprj-2/src/updater.cpp',
                content: 'int main() {}',
                projectPath: 'D:/tasks/linux-sysinfo',
            }),
            cloudRoot,
        )).toBe(false);
    });

    it('keeps remote display paths on the coding tab that produced them', () => {
        expect(codeFileBelongsToPreviewProject(
            previewFile({
                filePath: '/home/testprj-2/src/updater.cpp',
                content: 'int main() {}',
                projectPath: 'D:/tasks/linux-sysinfo',
            }),
            'D:/tasks/linux-sysinfo',
        )).toBe(true);
    });

    it('keeps unstamped remote display paths on a Windows remote-coding tab', () => {
        expect(codeFileBelongsToPreviewProject(
            previewFile({
                filePath: '/home/testprj-2/src/updater.cpp',
                absPath: '/home/testprj-2/src/updater.cpp',
                content: '#include "updater.h"',
            }),
            'D:/tasks/linux-sysinfo',
        )).toBe(true);
    });

    it('does not treat a file path as a project-path prefix', () => {
        expect(codeFileBelongsToPreviewProject(
            previewFile({
                filePath: '/home',
                absPath: '/home',
                content: 'x',
            }),
            'C:/Users/ma139/.maclaw/data/cloud-workspaces/tenant_default/cws_scholar',
        )).toBe(false);
    });

    it('keeps task-identity stamps when belonging path is the cloud cache', () => {
        const taskDir = 'C:/Users/ma139/.maclaw/data/tasks/scholar';
        const cloudRoot = 'C:/Users/ma139/.maclaw/data/cloud-workspaces/tenant_default/cws_scholar';
        expect(codeFileBelongsToPreviewProject(
            previewFile({
                filePath: 'papers/INDEX.md',
                content: '# papers',
                projectPath: taskDir,
            }),
            taskDir,
            cloudRoot,
        )).toBe(true);
        expect(codeFileBelongsToPreviewProject(
            previewFile({
                filePath: '/home/testprj-2/src/updater.cpp',
                absPath: '/home/testprj-2/src/updater.cpp',
                content: '#include "updater.h"',
            }),
            taskDir,
            cloudRoot,
        )).toBe(false);
    });

    it('drops remote leftovers on a cloud tab before the cache path resolves', () => {
        expect(codeFileBelongsToPreviewProject(
            previewFile({
                filePath: '/home/testprj-2/src/updater.cpp',
                absPath: '/home/testprj-2/src/updater.cpp',
                content: '#include "updater.h"',
            }),
            'C:/Users/ma139/.maclaw/data/tasks/scholar',
            undefined,
            true,
        )).toBe(false);
        expect(codeFileBelongsToPreviewProject(
            previewFile({
                filePath: 'papers/INDEX.md',
                content: '# papers',
                projectPath: 'C:/Users/ma139/.maclaw/data/tasks/scholar',
            }),
            'C:/Users/ma139/.maclaw/data/tasks/scholar',
            undefined,
            true,
        )).toBe(true);
    });
});

describe('filterCodePreviewStateForProject', () => {
    it('removes leaked remote files when switching to a cloud workspace', () => {
        const cloudRoot = 'C:/Users/ma139/.maclaw/data/cloud-workspaces/tenant_default/cws_scholar';
        let state = initialState();
        state = {
            ...state,
            active: true,
            sessionID: 'remote:ssh-1',
            sessionActive: true,
            activeFilePath: '/home/testprj-2/src/updater.cpp',
            files: new Map([
                ['/home/testprj-2/src/updater.cpp', previewFile({
                    filePath: '/home/testprj-2/src/updater.cpp',
                    absPath: '/home/testprj-2/src/updater.cpp',
                    content: '#include "updater.h"',
                    language: 'cpp',
                    sessionID: 'remote:ssh-1',
                })],
                ['papers/INDEX.md', previewFile({
                    filePath: 'papers/INDEX.md',
                    content: '# papers',
                    language: 'markdown',
                    projectPath: cloudRoot,
                })],
            ]),
        };
        const next = filterCodePreviewStateForProject(state, cloudRoot);
        expect(next.files.has('/home/testprj-2/src/updater.cpp')).toBe(false);
        expect(next.files.has('papers/INDEX.md')).toBe(true);
        expect(next.activeFilePath).toBe('papers/INDEX.md');
        expect(next.sessionID).toBe('');
        expect(next.sessionActive).toBe(false);
        expect(next.active).toBe(true);
    });

    it('filters leftovers by cloud cache without using that cache as the event route', () => {
        const taskDir = 'C:/Users/ma139/.maclaw/data/tasks/scholar';
        const cloudRoot = 'C:/Users/ma139/.maclaw/data/cloud-workspaces/tenant_default/cws_scholar';
        const state = {
            ...initialState(),
            active: true,
            sessionID: 'remote:ssh-1',
            sessionActive: true,
            activeFilePath: '/home/testprj-2/src/updater.cpp',
            files: new Map([
                ['/home/testprj-2/src/updater.cpp', previewFile({
                    filePath: '/home/testprj-2/src/updater.cpp',
                    absPath: '/home/testprj-2/src/updater.cpp',
                    content: '#include "updater.h"',
                    sessionID: 'remote:ssh-1',
                })],
            ]),
        };
        const next = filterCodePreviewStateForProject(state, taskDir, cloudRoot);
        expect(next.files.size).toBe(0);
        expect(next.sessionID).toBe('');
        expect(next.sessionActive).toBe(false);
        expect(shouldAcceptCodeEventForProject(taskDir, cloudRoot)).toBe(false);
    });
});
