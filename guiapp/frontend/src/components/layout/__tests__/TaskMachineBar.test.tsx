// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { TaskMachineBar } from '../TaskMachineBar';
import type { useTaskRemoteControl } from '../useTaskRemoteControl';

type Remote = ReturnType<typeof useTaskRemoteControl>;

function remote(overrides: Partial<Remote> = {}): Remote {
    return {
        state: {
            machine_id: 'mac-a',
            machines: [{ id: 'mac-b', name: '家里', online: true }],
            grants: [{ id: 'g1', machine_id: 'mac-b', machine_name: '家里', permission: 'view', status: 'active', online: true, same_account: true }],
            pending: [{ id: 'p1', account: 'bob@example.com', permission: 'operate', status: 'pending' }],
            invites: [],
            controlled: [],
        },
        selected: null,
        tasks: [],
        loading: false,
        error: '',
        detail: null,
        detailPath: '',
        creating: false,
        refresh: vi.fn(),
        selectLocal: vi.fn(),
        selectGrant: vi.fn(),
        setCreating: vi.fn(),
        grantSameAccount: vi.fn().mockResolvedValue(undefined),
        invite: vi.fn().mockResolvedValue(undefined),
        accept: vi.fn(),
        reject: vi.fn(),
        ignore: vi.fn(),
        revoke: vi.fn().mockResolvedValue(undefined),
        openTask: vi.fn(),
        closeDetail: vi.fn(),
        createTask: vi.fn().mockResolvedValue(undefined),
        sendMessage: vi.fn().mockResolvedValue(undefined),
        stopTask: vi.fn(),
        renameTask: vi.fn(),
        pinTask: vi.fn(),
        operate: false,
        ...overrides,
    };
}

describe('TaskMachineBar', () => {
    afterEach(() => cleanup());

    it('puts this computer, granted machines, and add on the task-list row', () => {
        render(<TaskMachineBar lang="zh-CN" remote={remote()} onCloseGrant={vi.fn()} />);
        expect(screen.getByTestId('task-machine-tab-local').textContent).toContain('本机');
        expect(screen.getByTestId('task-machine-tab-g1').textContent).toContain('家里');
        expect(screen.getByTestId('task-machine-add').textContent).toBe('+');
    });

    it('invites another account and keeps a waiting row', async () => {
        const control = remote();
        render(<TaskMachineBar lang="zh-CN" remote={control} onCloseGrant={vi.fn()} />);
        fireEvent.click(screen.getByTestId('task-machine-add'));
        fireEvent.change(screen.getByTestId('task-machine-account'), { target: { value: 'bob@example.com' } });
        await act(async () => {
            fireEvent.click(screen.getByTestId('task-machine-invite'));
        });
        expect(control.invite).toHaveBeenCalledWith('bob@example.com', 'view');
        expect(screen.getByTestId('task-machine-pending').textContent).toContain('等待对方处理');
        expect(screen.getByTestId('task-machine-pending').textContent).not.toContain('已忽略');
    });

    it('keeps this computer fixed and lets a granted machine be closed', () => {
        render(<TaskMachineBar lang="zh-CN" remote={remote()} onCloseGrant={vi.fn()} />);
        const local = screen.getByTestId('task-machine-tab-local');
        const add = screen.getByTestId('task-machine-add');
        expect(local.compareDocumentPosition(add) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(screen.queryByRole('button', { name: /结束对本机/ })).toBeNull();
        expect(screen.getByTestId('task-machine-close-g1')).toBeTruthy();
        expect(screen.getByTestId('task-machine-pending').textContent).toContain('等待对方处理');
        expect(screen.getByTestId('task-machine-pending').textContent).not.toContain('已忽略');
    });

    it('shows offline and hides sending when the grant cannot operate', () => {
        const control = remote({
            selected: { id: 'g1', machine_name: '家里', permission: 'view', status: 'active', online: false },
            operate: false,
        });
        render(<TaskMachineBar lang="zh-CN" remote={control} onCloseGrant={vi.fn()} />);
        expect(screen.getByTestId('task-machine-note').textContent).toContain('对方离线');
        expect(screen.queryByTestId('task-machine-send')).toBeNull();
        expect(screen.queryByTestId('task-machine-create')).toBeNull();
    });

    it('offers send only when the grant can operate', async () => {
        const control = remote({
            selected: { id: 'g1', machine_name: '家里', permission: 'operate', status: 'active', online: true },
            operate: true,
            detailPath: 'D:/tasks/a',
            detail: { messages: [{ role: 'user', content: 'hello' }], artifacts: [{ title: 'notes.txt' }] },
        });
        render(<TaskMachineBar lang="zh-CN" remote={control} onCloseGrant={vi.fn()} />);
        expect(screen.getByTestId('task-machine-send')).toBeTruthy();
        expect(screen.getByTestId('task-machine-artifacts').textContent).toContain('notes.txt');
        fireEvent.change(screen.getByTestId('task-machine-send-input'), { target: { value: '继续' } });
        await act(async () => {
            fireEvent.click(screen.getByTestId('task-machine-send'));
        });
        expect(control.sendMessage).toHaveBeenCalledWith('D:/tasks/a', '继续');
    });
});
