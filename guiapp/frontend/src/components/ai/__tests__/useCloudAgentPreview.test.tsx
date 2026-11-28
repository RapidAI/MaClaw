import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { RemoteAssistantSession, RemoteLivePart } from '../../layout/remoteAssistantSession';

const previewMock = vi.hoisted(() => vi.fn());

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: () => () => {},
    EventsOff: vi.fn(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetCodingWorkbenchFilePreview: previewMock,
}));

import { useCloudAgentPreview } from '../useCloudAgentPreview';

function event(body: Record<string, unknown>): string {
    return `Coding Agent Event: ${JSON.stringify({ version: 1, agent: 'coding', phase: 'running', ...body })}`;
}

function progress(text: string, seq = 1): RemoteLivePart {
    return { kind: 'progress', seq, text };
}

function session(kind: string, parts: RemoteLivePart[] = [], projectPath = 'cloud-agent:inst-1'): RemoteAssistantSession {
    return {
        grantId: 'g',
        machineName: '云端',
        projectPath,
        taskName: 'snake',
        cloud: true,
        cloudKind: kind,
        canSend: true,
        offline: false,
        error: '',
        messages: [],
        live: {
            requestId: 'req-1',
            displayText: '做游戏',
            parts,
            streamDone: false,
            responded: false,
            baseline: 0,
            lastEventAt: 1,
        },
    };
}

describe('useCloudAgentPreview', () => {
    beforeEach(() => {
        previewMock.mockReset();
    });

    it('adopts a created write and focuses the file', async () => {
        previewMock.mockResolvedValue({
            path: 'src/snake.cpp',
            content: 'int main(){}\n',
            language: 'cpp',
            original: 'should-not-attach',
        });
        const focus = { current: vi.fn() };
        const parts = [progress(event({
            event: 'tool_finished',
            detail: 'write_file',
            outcome: 'success',
            files: ['src/snake.cpp'],
            created: true,
            added: 12,
            removed: 0,
        }))];
        const { result } = renderHook(() => useCloudAgentPreview(session('coding', parts), focus));
        await waitFor(() => {
            expect(result.current.state.files.get('src/snake.cpp')?.opType).toBe('create');
        });
        const file = result.current.state.files.get('src/snake.cpp');
        expect(file?.original).toBeUndefined();
        expect(file?.content).toBe('int main(){}\n');
        expect(file?.forceOpen).toBe(true);
        expect(file?.projectPath).toBe('cloud-agent:inst-1');
        expect(result.current.active).toBe(true);
        expect(focus.current).toHaveBeenCalled();
        expect(previewMock).toHaveBeenCalledWith('cloud-agent:inst-1', 'src/snake.cpp');
    });

    it('diffs a modify against the returned baseline', async () => {
        previewMock.mockResolvedValue({
            path: 'src/snake.cpp',
            content: 'after\n',
            language: 'cpp',
            original: 'before\n',
        });
        const focus = { current: null as (() => void) | null };
        const parts = [progress(event({
            event: 'tool_finished',
            detail: 'write_file',
            outcome: 'success',
            files: ['src/snake.cpp'],
            added: 3,
            removed: 1,
        }))];
        const { result } = renderHook(() => useCloudAgentPreview(session('coding', parts), focus));
        await waitFor(() => {
            expect(result.current.state.files.get('src/snake.cpp')?.opType).toBe('modify');
        });
        expect(result.current.state.files.get('src/snake.cpp')?.original).toBe('before\n');
    });

    it('does not fetch for a session task, a deleted path, or a repeated identical part', async () => {
        previewMock.mockResolvedValue({ path: 'src/snake.cpp', content: 'v1\n', language: 'cpp', original: 'old\n' });
        const focus = { current: null as (() => void) | null };
        const write = progress(event({
            event: 'tool_finished',
            detail: 'write_file',
            outcome: 'success',
            files: ['src/snake.cpp'],
            added: 3,
            removed: 1,
        }));
        const { result, rerender } = renderHook(
            ({ current }: { current: RemoteAssistantSession }) => useCloudAgentPreview(current, focus),
            { initialProps: { current: session('session', [write]) } },
        );
        await act(async () => { await Promise.resolve(); });
        expect(previewMock).not.toHaveBeenCalled();
        expect(result.current.active).toBe(false);

        rerender({ current: session('coding', [progress(event({
            event: 'diff_summary',
            phase: 'result',
            file_changes: [{ path: 'src/gone.cpp', added: 0, removed: 2, deleted: true }],
        }))]) });
        await act(async () => { await Promise.resolve(); });
        expect(previewMock).not.toHaveBeenCalled();

        rerender({ current: session('coding', [write]) });
        await waitFor(() => expect(previewMock).toHaveBeenCalledTimes(1));
        const calls = previewMock.mock.calls.length;
        rerender({ current: session('coding', [{ ...write, seq: 2 }]) });
        await act(async () => { await Promise.resolve(); });
        expect(previewMock).toHaveBeenCalledTimes(calls);
    });

    it('refetches when a later event reports different counts, and does not open a binary', async () => {
        previewMock
            .mockResolvedValueOnce({ path: 'src/snake.cpp', content: 'v1\n', language: 'cpp', original: 'old\n' })
            .mockRejectedValueOnce(new Error('binary'));
        const focus = { current: vi.fn() };
        const first = progress(event({
            event: 'tool_finished',
            detail: 'write_file',
            outcome: 'success',
            files: ['src/snake.cpp'],
            added: 1,
            removed: 1,
        }));
        const { result, rerender } = renderHook(
            ({ current }: { current: RemoteAssistantSession }) => useCloudAgentPreview(current, focus),
            { initialProps: { current: session('coding', [first]) } },
        );
        await waitFor(() => expect(result.current.state.files.get('src/snake.cpp')?.content).toBe('v1\n'));

        rerender({ current: session('coding', [first, progress(event({
            event: 'diff_summary',
            phase: 'result',
            file_changes: [{ path: 'src/bin.dat', added: 4, removed: 0, created: true }],
        }), 2)]) });
        await waitFor(() => expect(previewMock).toHaveBeenCalledTimes(2));
        await waitFor(() => expect(result.current.refreshToken).toBeGreaterThan(1));
        expect(result.current.state.files.has('src/bin.dat')).toBe(false);
        expect(result.current.state.files.get('src/snake.cpp')?.opType).toBe('modify');
    });

    it('reloads the directory once when one update writes several files', async () => {
        previewMock.mockImplementation(async (_project: string, path: string) => ({
            path,
            content: `${path}\n`,
            language: 'cpp',
        }));
        const focus = { current: vi.fn() };
        const parts = [progress(event({
            event: 'diff_summary',
            phase: 'result',
            file_changes: [
                { path: 'Makefile', added: 3, removed: 0, created: true },
                { path: 'snake.cpp', added: 20, removed: 0, created: true },
            ],
        }))];
        const { result } = renderHook(() => useCloudAgentPreview(session('coding', parts), focus));
        await waitFor(() => {
            expect(result.current.state.files.has('snake.cpp')).toBe(true);
            expect(result.current.refreshToken).toBe(1);
        });
        expect(result.current.state.files.has('Makefile')).toBe(true);
        expect(focus.current).toHaveBeenCalledTimes(1);
        expect(previewMock).toHaveBeenCalledTimes(2);
    });

    it('reloads the directory before a file preview returns', async () => {
        let release: () => void = () => {};
        previewMock.mockImplementation((_project: string, path: string) => new Promise((resolve) => {
            release = () => resolve({ path, content: `${path}\n`, language: 'cpp' });
        }));
        const focus = { current: vi.fn() };
        const parts = [progress(event({
            event: 'tool_finished',
            detail: 'write_file',
            outcome: 'success',
            files: ['snake.cpp'],
            created: true,
            added: 4,
            removed: 0,
        }))];
        const { result } = renderHook(() => useCloudAgentPreview(session('coding', parts), focus));
        await waitFor(() => expect(result.current.refreshToken).toBe(1));
        expect(result.current.state.files.size).toBe(0);
        expect(focus.current).not.toHaveBeenCalled();
        expect(previewMock).toHaveBeenCalledTimes(1);
        await act(async () => { release(); });
        await waitFor(() => expect(result.current.state.files.has('snake.cpp')).toBe(true));
        expect(result.current.refreshToken).toBe(1);
        expect(focus.current).toHaveBeenCalledTimes(1);
    });

    it('reloads the directory when a file is deleted and refetches a later rewrite', async () => {
        previewMock.mockResolvedValue({ path: 'src/snake.cpp', content: 'v1\n', language: 'cpp', original: 'old\n' });
        const focus = { current: vi.fn() };
        const write = progress(event({
            event: 'tool_finished',
            detail: 'write_file',
            outcome: 'success',
            files: ['src/snake.cpp'],
            added: 3,
            removed: 1,
        }));
        const { result, rerender } = renderHook(
            ({ current }: { current: RemoteAssistantSession }) => useCloudAgentPreview(current, focus),
            { initialProps: { current: session('coding', [write]) } },
        );
        await waitFor(() => {
            expect(result.current.refreshToken).toBe(1);
            expect(result.current.state.files.has('src/snake.cpp')).toBe(true);
        });
        const calls = previewMock.mock.calls.length;
        const focuses = focus.current.mock.calls.length;
        rerender({ current: session('coding', [write, progress(event({
            event: 'diff_summary',
            phase: 'result',
            file_changes: [{ path: 'src/snake.cpp', added: 0, removed: 3, deleted: true }],
        }), 2)]) });
        await waitFor(() => expect(result.current.refreshToken).toBe(2));
        expect(previewMock).toHaveBeenCalledTimes(calls);
        expect(focus.current).toHaveBeenCalledTimes(focuses);
        expect(result.current.state.files.has('src/snake.cpp')).toBe(true);

        rerender({ current: session('coding', [write]) });
        await waitFor(() => expect(previewMock).toHaveBeenCalledTimes(calls + 1));
        expect(result.current.refreshToken).toBe(3);
    });

    it('does not reload the directory when only line counts change', async () => {
        previewMock.mockImplementation(async (_project: string, path: string) => ({
            path,
            content: `${previewMock.mock.calls.length}\n`,
            language: 'cpp',
            original: 'old\n',
        }));
        const focus = { current: vi.fn() };
        const first = progress(event({
            event: 'tool_finished',
            detail: 'write_file',
            outcome: 'success',
            files: ['src/snake.cpp'],
            added: 3,
            removed: 1,
        }));
        const { result, rerender } = renderHook(
            ({ current }: { current: RemoteAssistantSession }) => useCloudAgentPreview(current, focus),
            { initialProps: { current: session('coding', [first]) } },
        );
        await waitFor(() => {
            expect(result.current.refreshToken).toBe(1);
            expect(result.current.state.files.get('src/snake.cpp')?.content).toBe('1\n');
        });
        rerender({ current: session('coding', [first, progress(event({
            event: 'tool_finished',
            detail: 'write_file',
            outcome: 'success',
            files: ['src/snake.cpp'],
            added: 8,
            removed: 2,
        }), 2)]) });
        await waitFor(() => expect(result.current.state.files.get('src/snake.cpp')?.content).toBe('2\n'));
        expect(result.current.refreshToken).toBe(1);
        expect(previewMock).toHaveBeenCalledTimes(2);
    });

    it('replays a finished turn, and still fetches a later live edit', async () => {
        previewMock.mockImplementation(async (_project: string, path: string) => ({
            path,
            content: 'v1\n',
            language: 'cpp',
            original: 'old\n',
        }));
        const focus = { current: vi.fn() };
        const write = progress(event({
            event: 'tool_finished',
            detail: 'write_file',
            outcome: 'success',
            files: ['src/snake.cpp'],
            added: 3,
            removed: 1,
        }));
        const finished = session('coding', [write]);
        finished.live = { ...finished.live!, responded: true, streamDone: true };
        const { result, rerender } = renderHook(
            ({ current }: { current: RemoteAssistantSession }) => useCloudAgentPreview(current, focus),
            { initialProps: { current: finished } },
        );
        await waitFor(() => expect(result.current.state.files.has('src/snake.cpp')).toBe(true));
        expect(previewMock).toHaveBeenCalledTimes(1);
        expect(previewMock).toHaveBeenCalledWith('cloud-agent:inst-1', 'src/snake.cpp');

        const live = session('coding', [write, progress(event({
            event: 'tool_finished',
            detail: 'write_file',
            outcome: 'success',
            files: ['src/board.cpp'],
            created: true,
            added: 4,
            removed: 0,
        }), 2)]);
        rerender({ current: live });
        await waitFor(() => expect(result.current.state.files.has('src/board.cpp')).toBe(true));
        expect(previewMock).toHaveBeenCalledTimes(2);
        expect(previewMock).toHaveBeenLastCalledWith('cloud-agent:inst-1', 'src/board.cpp');
        expect(result.current.state.files.has('src/snake.cpp')).toBe(true);
    });

    it('replays a write stored on the transcript after the live round is gone', async () => {
        previewMock.mockResolvedValue({ path: 'src/snake.cpp', content: 'v1\n', language: 'cpp', original: 'old\n' });
        const focus = { current: vi.fn() };
        const finished = session('coding', []);
        finished.live = null;
        finished.messages = [{
            role: 'assistant',
            content: '写好了',
            codingTimeline: [{
                id: 'p1',
                sequence: 1,
                kind: 'progress',
                timestamp: 1,
                content: event({
                    event: 'tool_finished',
                    detail: 'write_file',
                    outcome: 'success',
                    files: ['src/snake.cpp'],
                    added: 3,
                    removed: 1,
                }),
            }],
        }];
        const { result } = renderHook(() => useCloudAgentPreview(finished, focus));
        await waitFor(() => expect(result.current.state.files.has('src/snake.cpp')).toBe(true));
        expect(previewMock).toHaveBeenCalledWith('cloud-agent:inst-1', 'src/snake.cpp');
        expect(result.current.state.files.get('src/snake.cpp')?.opType).toBe('modify');
    });

    it('fetches again when the same coding task is opened again', async () => {
        previewMock.mockResolvedValue({ path: 'src/snake.cpp', content: 'v1\n', language: 'cpp', original: 'old\n' });
        const focus = { current: vi.fn() };
        const write = progress(event({
            event: 'tool_finished',
            detail: 'write_file',
            outcome: 'success',
            files: ['src/snake.cpp'],
            added: 3,
            removed: 1,
        }));
        const finished = session('coding', [write]);
        finished.live = { ...finished.live!, responded: true, streamDone: true };
        const { rerender } = renderHook(
            ({ current }: { current: RemoteAssistantSession | null }) => useCloudAgentPreview(current, focus),
            { initialProps: { current: finished as RemoteAssistantSession | null } },
        );
        await waitFor(() => expect(previewMock).toHaveBeenCalledTimes(1));
        rerender({ current: null });
        await act(async () => { await Promise.resolve(); });
        rerender({ current: finished });
        await waitFor(() => expect(previewMock).toHaveBeenCalledTimes(2));
    });

    it('does not let a late chip fetch replace the file picked next', async () => {
        const pending: Record<string, (value: unknown) => void> = {};
        previewMock.mockImplementation((_project: string, path: string) => new Promise((resolve) => {
            pending[path] = resolve;
        }));
        const focus = { current: vi.fn() };
        const parts = [progress(event({
            event: 'tool_finished',
            detail: 'write_file',
            outcome: 'success',
            files: ['src/snake.cpp'],
            created: true,
            added: 4,
            removed: 0,
        }))];
        const { result } = renderHook(() => useCloudAgentPreview(session('coding', parts), focus));
        await waitFor(() => expect(typeof pending['src/snake.cpp']).toBe('function'));
        await act(async () => {
            pending['src/snake.cpp']({ path: 'src/snake.cpp', content: 'v1\n', language: 'cpp' });
        });
        await waitFor(() => expect(result.current.state.files.has('src/snake.cpp')).toBe(true));
        act(() => { result.current.focusPath('src/late.cpp'); });
        await waitFor(() => expect(typeof pending['src/late.cpp']).toBe('function'));
        act(() => { result.current.focusPath('src/snake.cpp'); });
        await act(async () => {
            pending['src/late.cpp']({ path: 'src/late.cpp', content: 'late\n', language: 'cpp' });
        });
        expect(result.current.state.files.has('src/late.cpp')).toBe(false);
        expect(result.current.state.activeFilePath).toBe('src/snake.cpp');
    });

    it('does not reopen the pane when a chip fetch returns after close', async () => {
        const pending: Record<string, (value: unknown) => void> = {};
        previewMock.mockImplementation((_project: string, path: string) => new Promise((resolve) => {
            pending[path] = resolve;
        }));
        const focus = { current: vi.fn() };
        const parts = [progress(event({
            event: 'tool_finished',
            detail: 'write_file',
            outcome: 'success',
            files: ['src/snake.cpp'],
            created: true,
            added: 4,
            removed: 0,
        }))];
        const { result } = renderHook(() => useCloudAgentPreview(session('coding', parts), focus));
        await waitFor(() => expect(typeof pending['src/snake.cpp']).toBe('function'));
        await act(async () => {
            pending['src/snake.cpp']({ path: 'src/snake.cpp', content: 'v1\n', language: 'cpp' });
        });
        await waitFor(() => expect(result.current.active).toBe(true));
        act(() => { result.current.focusPath('src/late.cpp'); });
        await waitFor(() => expect(typeof pending['src/late.cpp']).toBe('function'));
        act(() => { result.current.closePanel(); });
        expect(result.current.active).toBe(false);
        await act(async () => {
            pending['src/late.cpp']({ path: 'src/late.cpp', content: 'late\n', language: 'cpp' });
        });
        expect(result.current.active).toBe(false);
        expect(result.current.state.files.has('src/late.cpp')).toBe(false);
    });

    it('focuses an adopted file without reading it again', async () => {
        previewMock.mockResolvedValue({ path: 'src/snake.cpp', content: 'v1\n', language: 'cpp' });
        const focus = { current: vi.fn() };
        const parts = [progress(event({
            event: 'tool_finished',
            detail: 'write_file',
            outcome: 'success',
            files: ['src/snake.cpp'],
            created: true,
            added: 4,
            removed: 0,
        }))];
        const { result } = renderHook(() => useCloudAgentPreview(session('coding', parts), focus));
        await waitFor(() => expect(result.current.state.files.has('src/snake.cpp')).toBe(true));
        act(() => { result.current.focusPath('src/snake.cpp'); });
        expect(previewMock).toHaveBeenCalledTimes(1);
        expect(focus.current).toHaveBeenCalled();
        expect(result.current.state.activeFilePath).toBe('src/snake.cpp');
    });

    it('keeps a live fetch when the round finishes while the preview is in flight', async () => {
        let release: () => void = () => {};
        previewMock.mockImplementation((_project: string, path: string) => new Promise((resolve) => {
            release = () => resolve({ path, content: 'v1\n', language: 'cpp', original: 'old\n' });
        }));
        const focus = { current: vi.fn() };
        const write = progress(event({
            event: 'tool_finished',
            detail: 'write_file',
            outcome: 'success',
            files: ['src/snake.cpp'],
            added: 3,
            removed: 1,
        }));
        const { result, rerender } = renderHook(
            ({ current }: { current: RemoteAssistantSession }) => useCloudAgentPreview(current, focus),
            { initialProps: { current: session('coding', [write]) } },
        );
        await waitFor(() => expect(previewMock).toHaveBeenCalledTimes(1));
        const finished = session('coding', [write]);
        finished.live = { ...finished.live!, responded: true, streamDone: true };
        rerender({ current: finished });
        await act(async () => { release(); });
        await waitFor(() => expect(result.current.state.files.get('src/snake.cpp')?.content).toBe('v1\n'));
        expect(previewMock).toHaveBeenCalledTimes(1);
    });

    it('retries a transient read once', async () => {
        previewMock
            .mockRejectedValueOnce(new Error('这个文件暂时读取失败'))
            .mockResolvedValueOnce({ path: 'src/snake.cpp', content: 'v1\n', language: 'cpp' });
        const focus = { current: vi.fn() };
        const parts = [progress(event({
            event: 'tool_finished',
            detail: 'write_file',
            outcome: 'success',
            files: ['src/snake.cpp'],
            created: true,
            added: 4,
            removed: 0,
        }))];
        const { result } = renderHook(() => useCloudAgentPreview(session('coding', parts), focus));
        await waitFor(() => expect(result.current.state.files.get('src/snake.cpp')?.content).toBe('v1\n'));
        expect(previewMock).toHaveBeenCalledTimes(2);
        expect(result.current.refreshToken).toBe(1);
    });

    it('opens the pane from the files control without a write', () => {
        const focus = { current: null as (() => void) | null };
        const { result } = renderHook(() => useCloudAgentPreview(session('session'), focus));
        expect(result.current.active).toBe(false);
        act(() => { result.current.openFiles(); });
        expect(result.current.active).toBe(true);
        expect(previewMock).not.toHaveBeenCalled();
    });
});
