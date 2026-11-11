// @vitest-environment jsdom
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useTaskRemoteControl } from '../useTaskRemoteControl';

vi.mock('../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(() => () => {}),
}));

// never[] accepts narrower concrete signatures (contravariance), so the
// house-style typed vi.fn mocks assign cleanly.
type Binding = (...args: never[]) => Promise<unknown>;

function deferred() {
    let resolve!: (value: unknown) => void;
    let reject!: (reason?: unknown) => void;
    const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
    return { promise, resolve: (value: unknown) => resolve(value), reject };
}

function remoteState(overrides: Record<string, unknown> = {}) {
    return {
        machine_id: 'self',
        machines: [],
        grants: [
            { id: 'ga', machine_id: 'mac-a', machine_name: 'A机', permission: 'operate', status: 'active', online: true, same_account: true },
            { id: 'gb', machine_id: 'mac-b', machine_name: 'B机', permission: 'operate', status: 'active', online: true, same_account: true },
        ],
        pending: [],
        invites: [],
        controlled: [],
        ...overrides,
    };
}

function setBindings(bindings: Record<string, Binding>) {
    (window as unknown as { go?: unknown }).go = { main: { App: bindings } };
}

async function setup(state = remoteState()) {
    const listState = vi.fn(async () => JSON.parse(JSON.stringify(state)));
    const op = vi.fn((_grantId: string, _op: string, _body?: string) => Promise.resolve('{}'));
    setBindings({
        ListRemoteControlState: listState,
        RemoteControlOp: op,
        RevokeRemoteControl: vi.fn(async () => true),
    });
    const result = renderHook(() => useTaskRemoteControl()).result;
    await act(async () => {});
    return { result, listState, op };
}

describe('useTaskRemoteControl', () => {
    afterEach(() => {
        delete (window as unknown as { go?: unknown }).go;
        cleanup();
    });
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('discards a stale task poll for a machine the person already left', async () => {
        const stale = deferred();
        let call = 0;
        const op = vi.fn((_grantId: string, op: string) => {
            if (op === 'list_tasks') {
                call += 1;
                return call === 1 ? stale.promise : Promise.resolve(JSON.stringify({ tasks: [{ id: 'tb', name: 'B任务', project_path: 'D:/b' }] }));
            }
            return Promise.resolve('{}');
        });
        setBindings({ ListRemoteControlState: vi.fn(async () => JSON.parse(JSON.stringify(remoteState()))), RemoteControlOp: op });
        const { result } = renderHook(() => useTaskRemoteControl());
        await act(async () => {});

        act(() => result.current.selectGrant('ga'));
        await act(async () => {});
        expect(op).toHaveBeenCalledWith('ga', 'list_tasks', '');

        // The person leaves for machine B while A's poll is still in the air.
        act(() => result.current.selectGrant('gb'));
        await waitFor(() => expect(result.current.tasks.map(task => task.id)).toEqual(['tb']));

        // A's answer lands afterwards and must not paint over B's list.
        await act(async () => { stale.resolve(JSON.stringify({ tasks: [{ id: 'ta', name: 'A任务', project_path: 'D:/a' }] })); });
        await act(async () => { await new Promise(resolve => setTimeout(resolve, 10)); });
        expect(result.current.tasks.map(task => task.id)).toEqual(['tb']);
    });

    it('clears selection, transcript, and waits when the grant is revoked', async () => {
        const { result } = await setup();
        act(() => result.current.selectGrant('ga'));
        await act(async () => {});

        await act(async () => { await result.current.revoke('ga'); });

        expect(result.current.selected).toBeNull();
        expect(result.current.detailPath).toBe('');
        expect(result.current.detail).toBeNull();
        expect(result.current.awaitingReply).toBe(false);
    });

    it('ends the send wait when the remote transcript answers', async () => {
        let messages = [{ role: 'user', content: 'hello' }];
        const op = vi.fn((_grantId: string, op: string) => {
            if (op === 'get_task') {
                return Promise.resolve(JSON.stringify({ messages, artifacts: [] }));
            }
            if (op === 'list_tasks') {
                return Promise.resolve(JSON.stringify({ tasks: [{ id: 'ta', name: 'A任务', project_path: 'D:/x' }] }));
            }
            return Promise.resolve('{}');
        });
        setBindings({ ListRemoteControlState: vi.fn(async () => JSON.parse(JSON.stringify(remoteState()))), RemoteControlOp: op });
        const { result } = renderHook(() => useTaskRemoteControl());
        await act(async () => {});
        act(() => result.current.selectGrant('ga'));
        await act(async () => {});
        await act(async () => { await result.current.openTask('D:/x'); });
        expect(result.current.awaitingReply).toBe(false);

        await act(async () => {
            const ok = await result.current.sendMessage('D:/x', '继续');
            expect(ok).toBe(true);
        });
        expect(result.current.awaitingReply).toBe(true);

        // The remote computer stores the turn: a longer transcript whose tail
        // is not the person's own row settles the wait.
        messages = [
            { role: 'user', content: 'hello' },
            { role: 'user', content: '继续' },
            { role: 'assistant', content: 'done' },
        ];
        await act(async () => { await result.current.openTask('D:/x'); });
        expect(result.current.awaitingReply).toBe(false);
    });

    it('keeps a conversation error against a failing state poll', async () => {
        const listState = vi.fn(async () => JSON.parse(JSON.stringify(remoteState())));
        const op = vi.fn((_grantId: string, op: string) => {
            if (op === 'get_task') return Promise.reject(new Error('会话挂了'));
            return Promise.resolve(JSON.stringify({ tasks: [] }));
        });
        setBindings({ ListRemoteControlState: listState, RemoteControlOp: op });
        const { result } = renderHook(() => useTaskRemoteControl());
        await act(async () => {});
        act(() => result.current.selectGrant('ga'));
        await act(async () => {});
        await act(async () => { await result.current.openTask('D:/x'); });
        expect(result.current.errorKind).toBe('conversation');

        // The 20s state poll fails too; the conversation error stays up.
        await act(async () => {
            listState.mockRejectedValueOnce(new Error('状态挂了'));
            await result.current.refresh();
        });
        expect(result.current.error).toBe('会话挂了');
    });
});
