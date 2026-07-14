import { describe, expect, it } from 'vitest';
import { shouldAcceptCodeEventForProject } from '../useCodePreviewState';

describe('shouldAcceptCodeEventForProject', () => {
    it('accepts empty event project path (legacy events)', () => {
        expect(shouldAcceptCodeEventForProject(undefined, 'D:/proj')).toBe(true);
        expect(shouldAcceptCodeEventForProject('', 'D:/proj')).toBe(true);
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
