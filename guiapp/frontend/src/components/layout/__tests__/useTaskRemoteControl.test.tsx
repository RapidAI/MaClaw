// @vitest-environment jsdom
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useTaskRemoteControl } from '../useTaskRemoteControl';
import { remoteMachineTargetSnapshot } from '../remoteAssistantSession';

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

    it('opens a task on another machine tab via openTaskForGrant', async () => {
        const op = vi.fn((_grantId: string, op: string) => {
            if (op === 'get_task') {
                return Promise.resolve(JSON.stringify({ task: { project_path: 'D:/new', name: '新任务' }, messages: [], artifacts: [] }));
            }
            if (op === 'list_tasks') {
                return Promise.resolve(JSON.stringify({ tasks: [{ id: 'tn', name: '新任务', project_path: 'D:/new' }] }));
            }
            return Promise.resolve('{}');
        });
        setBindings({ ListRemoteControlState: vi.fn(async () => JSON.parse(JSON.stringify(remoteState()))), RemoteControlOp: op });
        const { result } = renderHook(() => useTaskRemoteControl());
        await act(async () => {});
        act(() => result.current.selectGrant('ga'));
        await act(async () => {});

        // The wizard finished creating on machine B while A's tab was open:
        // the bar switches to B and opens the fresh task there.
        act(() => result.current.openTaskForGrant('gb', 'D:/new'));
        await waitFor(() => expect(result.current.detailPath).toBe('D:/new'));
        expect(result.current.selected?.id).toBe('gb');
        expect(op).toHaveBeenCalledWith('gb', 'get_task', JSON.stringify({ project_path: 'D:/new' }));
    });

    it('openTaskForGrant ignores offline machines', async () => {
        const offline = remoteState({
            grants: [{ id: 'go', machine_id: 'mac-o', machine_name: 'O机', permission: 'operate', status: 'active', online: false }],
        });
        const op = vi.fn(() => Promise.resolve('{}'));
        setBindings({ ListRemoteControlState: vi.fn(async () => JSON.parse(JSON.stringify(offline))), RemoteControlOp: op });
        const { result } = renderHook(() => useTaskRemoteControl());
        await act(async () => {});

        act(() => result.current.openTaskForGrant('go', 'D:/x'));
        await act(async () => {});
        expect(result.current.detailPath).toBe('');
        expect(op).not.toHaveBeenCalled();
    });

    it('publishes controlled machines as new-task destinations', async () => {
        await setup();
        const target = remoteMachineTargetSnapshot();
        expect(target.machines.map(m => m.grantId)).toEqual(['ga', 'gb']);
        expect(target.machines.every(m => m.operate && m.online)).toBe(true);
        expect(target.machines[0].machineName).toBe('A机');
    });

    it('refuses to send a message on a view-only grant', async () => {
        const viewOnly = remoteState({
            grants: [{ id: 'gv', machine_id: 'mac-v', machine_name: 'V机', permission: 'view', status: 'active', online: true }],
        });
        const op = vi.fn((_grantId: string, op: string) => {
            if (op === 'list_tasks') return Promise.resolve(JSON.stringify({ tasks: [] }));
            return Promise.resolve('{}');
        });
        setBindings({ ListRemoteControlState: vi.fn(async () => JSON.parse(JSON.stringify(viewOnly))), RemoteControlOp: op });
        const { result } = renderHook(() => useTaskRemoteControl());
        await act(async () => {});
        act(() => result.current.selectGrant('gv'));
        await act(async () => {});

        await act(async () => {
            expect(await result.current.sendMessage('D:/x', 'hi')).toBe(false);
        });

        expect(op.mock.calls.some(call => call[1] === 'send_message')).toBe(false);
        expect(result.current.error).toBe('当前权限只能查看');
    });
});

