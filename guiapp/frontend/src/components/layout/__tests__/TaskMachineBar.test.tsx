// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { TaskMachineBar } from '../TaskMachineBar';
import { remoteAssistantSend, remoteAssistantSessionSnapshot } from '../remoteAssistantSession';
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
        errorKind: '',
        detail: null,
        detailPath: '',
        detailLoading: false,
        detailBusy: false,
        awaitingReply: false,
        awaitingStop: false,
        refresh: vi.fn(),
        selectLocal: vi.fn(),
        selectGrant: vi.fn(),
        grantSameAccount: vi.fn().mockResolvedValue(undefined),
        invite: vi.fn().mockResolvedValue(undefined),
        accept: vi.fn(),
        reject: vi.fn(),
        ignore: vi.fn(),
        revoke: vi.fn().mockResolvedValue(undefined),
        openTask: vi.fn(),
        closeDetail: vi.fn(),
        openTaskForGrant: vi.fn(),
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
        const grantTab = screen.getByTestId('task-machine-tab-g1');
        expect(grantTab.textContent).toContain('机器1');
        expect(grantTab.getAttribute('title')).toBe('家里 · 仅查看');
        expect(grantTab.getAttribute('aria-label')).toBe('家里');
        expect(screen.getByTestId('task-machine-add').textContent).toBe('+');
    });

    it('names the account on a granted tab tooltip', () => {
        const control = remote({
            state: {
                machine_id: 'mac-a',
                machines: [],
                grants: [
                    { id: 'g1', machine_id: 'mac-b', machine_name: 'mayong-pc', account: 'mayong@example.com', permission: 'operate', status: 'active', online: true },
                ],
                pending: [], invites: [], controlled: [],
            },
        });
        render(<TaskMachineBar lang="zh-CN" remote={control} onCloseGrant={vi.fn()} />);
        const grantTab = screen.getByTestId('task-machine-tab-g1');
        expect(grantTab.getAttribute('title')).toBe('mayong-pc (mayong@example.com) · 可操作');
        expect(grantTab.getAttribute('aria-label')).toBe('mayong-pc (mayong@example.com)');
    });

    it('offers edge scrollers once the tab strip overflows', () => {
        render(<TaskMachineBar lang="zh-CN" remote={remote()} onCloseGrant={vi.fn()} />);
        expect(screen.queryByTestId('task-machine-scroll-left')).toBeNull();
        const local = screen.getByTestId('task-machine-tab-local');
        expect(local.closest('.mc-task-machine-tablist')).toBeNull();
        const list = document.querySelector('.mc-task-machine-tablist') as HTMLElement;
        Object.defineProperty(list, 'scrollWidth', { value: 500, configurable: true });
        Object.defineProperty(list, 'clientWidth', { value: 100, configurable: true });
        fireEvent.scroll(list);
        const left = screen.getByTestId('task-machine-scroll-left') as HTMLButtonElement;
        const right = screen.getByTestId('task-machine-scroll-right') as HTMLButtonElement;
        expect(left.disabled).toBe(true);
        expect(right.disabled).toBe(false);
        const scrollBy = vi.fn();
        list.scrollBy = scrollBy;
        fireEvent.click(right);
        expect(scrollBy).toHaveBeenCalledWith(expect.objectContaining({ left: expect.any(Number) }));
    });

    it('moves focus across machine tabs with arrow keys', () => {
        render(<TaskMachineBar lang="zh-CN" remote={remote()} onCloseGrant={vi.fn()} />);
        const local = screen.getByTestId('task-machine-tab-local');
        const grant = screen.getByTestId('task-machine-tab-g1');
        local.focus();
        fireEvent.keyDown(local, { key: 'ArrowRight' });
        expect(document.activeElement).toBe(grant);
        fireEvent.keyDown(grant, { key: 'ArrowRight' });
        expect(document.activeElement).toBe(local);
        fireEvent.keyDown(local, { key: 'ArrowLeft' });
        expect(document.activeElement).toBe(grant);
    });

    it('marks an offline granted machine with a labelled dot, not tab text', () => {
        const control = remote({
            state: {
                machine_id: 'mac-a',
                machines: [],
                grants: [
                    { id: 'g1', machine_id: 'mac-b', machine_name: '家里', permission: 'view', status: 'active', online: false, same_account: true },
                ],
                pending: [], invites: [], controlled: [],
            },
        });
        render(<TaskMachineBar lang="zh-CN" remote={control} onCloseGrant={vi.fn()} />);
        const dot = screen.getByTestId('task-machine-offline-g1');
        expect(dot.getAttribute('aria-label')).toContain('离线');
        expect(dot.getAttribute('data-online')).toBe('false');
        // The strip stays compact: the offline state lives on the dot and its
        // tooltip, not as a text chip inside the tab.
        expect(screen.getByTestId('task-machine-tab-g1').textContent).not.toContain('离线');
    });

    it('invites another account and keeps a waiting row', async () => {
        const control = remote();
        render(<TaskMachineBar lang="zh-CN" remote={control} onCloseGrant={vi.fn()} />);
        fireEvent.click(screen.getByTestId('task-machine-add'));
        expect(control.refresh).toHaveBeenCalledTimes(1);
        expect(screen.getByTestId('task-machine-own-mac-b').textContent).toContain('家里 · 在线');
        fireEvent.change(screen.getByTestId('task-machine-account'), { target: { value: 'bob@example.com' } });
        await act(async () => {
            fireEvent.click(screen.getByTestId('task-machine-invite'));
        });
        expect(control.invite).toHaveBeenCalledWith('bob@example.com', 'view');
        expect(screen.getByTestId('task-machine-pending').textContent).toContain('等待对方处理');
        expect(screen.getByTestId('task-machine-pending').textContent).not.toContain('已忽略');
    });

    it('puts online computers first and orders them by machine name', () => {
        const control = remote({
            state: {
                machine_id: 'mac-a',
                machines: [
                    { id: 'mac-z', name: 'Z-Offline', online: false },
                    { id: 'mac-c', name: 'B-Online', online: true },
                    { id: 'mac-b', name: 'A-Online', online: true },
                ],
                grants: [], pending: [], invites: [], controlled: [],
            },
        });
        render(<TaskMachineBar lang="zh-CN" remote={control} onCloseGrant={vi.fn()} />);
        fireEvent.click(screen.getByTestId('task-machine-add'));
        const first = screen.getByTestId('task-machine-own-mac-b');
        const second = screen.getByTestId('task-machine-own-mac-c');
        const offline = screen.getByTestId('task-machine-own-mac-z');
        expect(first.compareDocumentPosition(second) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(second.compareDocumentPosition(offline) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    });

    it('never offers this computer as a grant target in the add panel', () => {
        const control = remote({
            state: {
                machine_id: 'mac-a',
                machines: [
                    { id: 'mac-a', name: '本机', online: true },
                    { id: 'mac-b', name: '家里', online: true },
                ],
                grants: [], pending: [], invites: [], controlled: [],
            },
        });
        render(<TaskMachineBar lang="zh-CN" remote={control} onCloseGrant={vi.fn()} />);
        fireEvent.click(screen.getByTestId('task-machine-add'));
        expect(screen.queryByTestId('task-machine-own-mac-a')).toBeNull();
        expect(screen.getByTestId('task-machine-own-mac-b').textContent).toContain('家里');
    });

    it('publishes a loading session while the transcript is on the way', () => {
        const control = remote({
            selected: { id: 'g1', machine_name: '家里', permission: 'operate', status: 'active', online: true },
            operate: true,
            tasks: [{ id: 't1', name: '整理报告', project_path: 'D:/tasks/a' }],
            detailPath: 'D:/tasks/a',
            detail: null,
            detailLoading: true,
        });
        render(<TaskMachineBar lang="zh-CN" remote={control} onCloseGrant={vi.fn()} />);
        const snapshot = remoteAssistantSessionSnapshot();
        expect(snapshot?.loading).toBe(true);
        expect(snapshot?.canSend).toBe(true);
        expect(snapshot?.taskName).toBe('整理报告');
        expect(snapshot?.messages).toHaveLength(0);
    });

    it('keeps a local status failure inside the add panel', () => {
        const control = remote({
            error: '远控请求失败',
            state: {
                machine_id: 'mac-a',
                machines: [],
                grants: [],
                pending: [],
                invites: [],
                controlled: [],
            },
        });
        render(<TaskMachineBar lang="zh-CN" remote={control} onCloseGrant={vi.fn()} />);
        expect(screen.queryByTestId('task-machine-error')).toBeNull();
        fireEvent.click(screen.getByTestId('task-machine-add'));
        expect(screen.getByTestId('task-machine-error').textContent).toContain('远控请求失败');
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
            detailPath: 'D:/tasks/a',
            detail: { messages: [{ role: 'user', content: 'hello' }], artifacts: [] },
        });
        render(<TaskMachineBar lang="zh-CN" remote={control} onCloseGrant={vi.fn()} />);
        expect(screen.getByTestId('task-machine-note').textContent).toContain('对方离线');
        expect(screen.queryByTestId('task-machine-send')).toBeNull();
        expect(screen.queryByTestId('task-machine-create')).toBeNull();
        expect(remoteAssistantSessionSnapshot()?.canSend).toBe(false);
        expect(remoteAssistantSessionSnapshot()?.offline).toBe(true);
    });

    it('offers send only when the grant can operate', async () => {
        const control = remote({
            selected: { id: 'g1', machine_name: '家里', permission: 'operate', status: 'active', online: true },
            operate: true,
            detailPath: 'D:/tasks/a',
            detail: { messages: [{ role: 'user', content: 'hello' }], artifacts: [{ title: 'notes.txt' }] },
        });
        render(<TaskMachineBar lang="zh-CN" remote={control} onCloseGrant={vi.fn()} />);
        expect(screen.queryByTestId('task-machine-send')).toBeNull();
        expect(remoteAssistantSessionSnapshot()?.canSend).toBe(true);
        expect(remoteAssistantSessionSnapshot()?.offline).toBe(false);
        expect(remoteAssistantSessionSnapshot()?.taskName).toBe('D:/tasks/a');
        expect(remoteAssistantSessionSnapshot()?.messages.map(message => message.content)).toContain('hello');
        await act(async () => {
            await remoteAssistantSend('继续');
        });
        expect(control.sendMessage).toHaveBeenCalledWith('D:/tasks/a', '继续');
    });

    it('keeps remote thinking in the reasoning field instead of the body', async () => {
        const control = remote({
            selected: { id: 'g1', machine_name: '家里', permission: 'operate', status: 'active', online: true },
            operate: true,
            detailPath: 'D:/tasks/a',
            detail: {
                messages: [
                    { role: 'user', content: '北京天气' },
                    { role: 'assistant', content: '', reasoning_content: 'thinking about weather' },
                    { role: 'assistant', content: '现在晴', reasoning_content: 'final thought' },
                ],
                artifacts: [],
            },
        });
        render(<TaskMachineBar lang="zh-CN" remote={control} onCloseGrant={vi.fn()} />);
        const messages = remoteAssistantSessionSnapshot()?.messages || [];
        // A turn that is still thinking has no content: it must surface as a
        // 思考过程 block, not as a prose bubble.
        const thinking = messages.find(m => m.reasoning === 'thinking about weather');
        expect(thinking?.content).toBe('');
        const answered = messages.find(m => m.content === '现在晴');
        expect(answered?.reasoning).toBe('final thought');
    });
});
