// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { gitRepoNameShapeValid, useGitRepoNameStatus } from '../gitRepoNameStatus';

const DEBOUNCE_MS = 500;

function setBinding(fn: unknown) {
    (window as unknown as { go?: unknown }).go = { main: { App: { GitPlatformRepoNameStatus: fn } } };
}

describe('useGitRepoNameStatus', () => {
    afterEach(() => {
        vi.useRealTimers();
        delete (window as unknown as { go?: unknown }).go;
    });

    it('mirrors the backend path-segment rule', () => {
        expect(gitRepoNameShapeValid('my.repo-1_x')).toBe(true);
        expect(gitRepoNameShapeValid('has space')).toBe(false);
        expect(gitRepoNameShapeValid('.')).toBe(false);
        expect(gitRepoNameShapeValid('..')).toBe(false);
        expect(gitRepoNameShapeValid('')).toBe(false);
        expect(gitRepoNameShapeValid('a'.repeat(201))).toBe(false);
    });

    it('debounces the probe and reports the available verdict', async () => {
        vi.useFakeTimers();
        const probe = vi.fn(async () => ({ available: true, reason: '' }));
        setBinding(probe);

        const { result, rerender } = renderHook(() => useGitRepoNameStatus({
            service: 'github', credentialId: '', owner: 'ada', name: 'fresh', enabled: true,
        }));
        rerender();
        // No probe before the debounce window elapses, and no premature
        // "invalid" while waiting.
        expect(probe).not.toHaveBeenCalled();
        expect(result.current.state.status).toBe('checking');

        await act(async () => { vi.advanceTimersByTime(DEBOUNCE_MS + 10); });
        expect(probe).toHaveBeenCalledWith('github', '', 'ada', 'fresh');
        await act(async () => {});
        expect(result.current.state.status).toBe('ok');
        expect(result.current.error).toBe('');
    });

    it('reports taken with the provider reason', async () => {
        vi.useFakeTimers();
        setBinding(async () => ({ available: false, reason: '该仓库已存在' }));

        const { result } = renderHook(() => useGitRepoNameStatus({
            service: 'gitlab', credentialId: 'vcred_1', owner: 'acme', name: 'taken', enabled: true,
        }));
        await act(async () => { vi.advanceTimersByTime(DEBOUNCE_MS + 10); });
        await act(async () => {});
        expect(result.current.state.status).toBe('taken');
        expect(result.current.state.reason).toBe('该仓库已存在');
    });

    it('rejects a bad shape locally without probing and stays idle while the namespace resolves', async () => {
        vi.useFakeTimers();
        const probe = vi.fn(async () => ({ available: true, reason: '' }));
        setBinding(probe);

        const initial = renderHook(() => useGitRepoNameStatus({
            service: 'github', credentialId: '', owner: '', name: 'fresh', enabled: true,
        }));
        expect(initial.result.current.state.status).toBe('idle');
        expect(probe).not.toHaveBeenCalled();

        const bad = renderHook(() => useGitRepoNameStatus({
            service: 'github', credentialId: '', owner: 'ada', name: 'bad name!', enabled: true,
        }));
        await act(async () => { vi.advanceTimersByTime(DEBOUNCE_MS + 10); });
        expect(bad.result.current.state.status).toBe('invalid');
        expect(probe).not.toHaveBeenCalled();
    });

    it('surfaces an API failure through error so a broken probe never reads as available', async () => {
        vi.useFakeTimers();
        setBinding(async () => { throw new Error('quota exhausted'); });

        const { result } = renderHook(() => useGitRepoNameStatus({
            service: 'github', credentialId: '', owner: 'ada', name: 'fresh', enabled: true,
        }));
        await act(async () => { vi.advanceTimersByTime(DEBOUNCE_MS + 10); });
        await act(async () => {});
        expect(result.current.state.status).toBe('idle');
        expect(result.current.error).toContain('quota exhausted');
    });

    it('keeps only the newest input verdict when the name changes mid-debounce', async () => {
        vi.useRealTimers();
        const probe = vi.fn(async (_service?: string, _cred?: string, _owner?: string, name?: string) => (
            { available: name === 'second', reason: name === 'second' ? '' : '该仓库已存在' }
        ));
        setBinding(probe);

        const { result, rerender } = renderHook(({ name }: { name: string }) => useGitRepoNameStatus({
            service: 'github', credentialId: '', owner: 'ada', name, enabled: true,
        }), { initialProps: { name: 'first' } });

        rerender({ name: 'second' });
        await waitFor(() => expect(result.current.state.status).toBe('ok'), { timeout: 3000 });
        // One probe for the surviving input; the superseded one is discarded
        // rather than flashing a stale taken verdict.
        await waitFor(() => expect(probe).toHaveBeenCalledTimes(1), { timeout: 3000 });
        expect(probe.mock.calls[0][3]).toBe('second');
    });
});
