import { describe, expect, it } from 'vitest';
import { resolveActiveRuntime, resolveRuntimeProgress } from './EnvCheckIcons';

describe('resolveActiveRuntime', () => {
    it('returns null when logs do not mention a runtime', () => {
        expect(resolveActiveRuntime([])).toBeNull();
        expect(resolveActiveRuntime(['[1/4] Checking Visual C++ Redistributable...'])).toBeNull();
    });

    it('picks the latest matching runtime from the log tail', () => {
        expect(resolveActiveRuntime(['[2/4] Checking Node.js...'])).toBe('node');
        expect(resolveActiveRuntime([
            '[2/4] Checking Node.js...',
            'Node.js is already installed.',
            '[3/4] Checking Git...',
        ])).toBe('git');
        expect(resolveActiveRuntime([
            'Checking Git...',
            '[4/4] Checking Python environment...',
        ])).toBe('python');
    });

    it('does not regress when a later log mentions an earlier runtime', () => {
        expect(resolveActiveRuntime([
            '[4/4] Checking Python environment...',
            'npm warn deprecated glob@7',
        ])).toBe('python');
    });
});

describe('resolveRuntimeProgress', () => {
    it('starts nearly empty before any log arrives', () => {
        expect(resolveRuntimeProgress([])).toEqual({
            active: null,
            done: new Set(),
            skipped: new Set(),
            percent: 8,
        });
    });

    it('treats unmatched early steps as a small advance', () => {
        const progress = resolveRuntimeProgress(['[1/4] Checking Visual C++ Redistributable...']);
        expect(progress.active).toBeNull();
        expect(progress.percent).toBe(16);
        expect(progress.done.size).toBe(0);
        expect(progress.skipped.size).toBe(0);
    });

    it('marks earlier runtimes done when a later step is active', () => {
        const progress = resolveRuntimeProgress([
            '[2/4] Checking Node.js...',
            '[3/4] Checking Git...',
        ]);
        expect(progress.active).toBe('git');
        expect([...progress.done]).toEqual(['node']);
        expect(progress.skipped.size).toBe(0);
        expect(progress.percent).toBe(62);
    });

    it('does not mark a skipped runtime done when the check jumps to Python', () => {
        const progress = resolveRuntimeProgress([
            'Checking Node.js...',
            'Checking Python environment...',
        ]);
        expect(progress.active).toBe('python');
        expect([...progress.done]).toEqual(['node']);
        expect(progress.skipped.size).toBe(0);
        expect(progress.percent).toBe(86);
    });

    it('fills the bar when the base environment check completes', () => {
        const progress = resolveRuntimeProgress([
            '[4/4] Checking Python environment...',
            '— Base environment check complete.',
        ]);
        expect(progress.active).toBeNull();
        expect([...progress.done]).toEqual(['python']);
        expect([...progress.skipped]).toEqual(['node', 'git']);
        expect(progress.percent).toBe(100);
    });

    it('marks every runtime ready when the full Windows sequence completes', () => {
        const progress = resolveRuntimeProgress([
            '[2/4] Checking Node.js...',
            '[3/4] Checking Git...',
            '[4/4] Checking Python environment...',
            '— Base environment check complete.',
        ]);
        expect([...progress.done]).toEqual(['node', 'git', 'python']);
        expect(progress.skipped.size).toBe(0);
        expect(progress.percent).toBe(100);
    });
});
