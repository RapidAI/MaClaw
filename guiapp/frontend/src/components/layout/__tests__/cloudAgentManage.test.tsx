// @vitest-environment jsdom
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { CloudAgentEntryButton, CloudAgentManage } from '../CloudAgentManage';
import { applyCloudAgentWatch, cloudAgentRound, cloudAgentSessionKey, cloudAgentStoreKey, expireCloudAgentCards, resetCloudAgentRounds } from '../cloudAgentSession';

const streamHandlers: Array<(payload: unknown) => void> = [];

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: (_name: string, fn: (payload: unknown) => void) => {
        streamHandlers.push(fn);
        return () => {};
    },
}));

function setBindings(bindings: Record<string, (...args: never[]) => Promise<unknown>>) {
    (window as unknown as { go?: unknown }).go = { main: { App: bindings } };
}

describe('cloud agent page', () => {
    afterEach(() => {
        cleanup();
        resetCloudAgentRounds();
        streamHandlers.length = 0;
        delete (window as unknown as { go?: unknown }).go;
    });

    it('mounts 云端管理 and keeps an empty project path on the grant session key', async () => {
        const op = vi.fn(async (grant: string, name: string, _body?: string) => {
            expect(grant).toBe('grant-1');
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1', title: 'ship' }] });
            if (name === 'cloud_get_task') {
                streamHandlers[0](JSON.stringify({
                    grant_id: 'grant-1',
                    session_key: 'cloud-agent:cag_1:task-1',
                    project_path: '',
                    event: 'token',
                    text: 'hi',
                    proto: 2,
                    cursor: 1,
                }));
                return JSON.stringify({ messages: ['old history'] });
            }
            return '{}';
        });
        setBindings({
            ListCloudAgents: vi.fn(async () => [{ id: 'cag_1', name: 'demo', kind: 'coding', status: 'idle' }]),
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        render(<CloudAgentManage lang="zh" onClose={() => {}} />);
        expect(screen.getByText('云端管理')).toBeTruthy();
        fireEvent.click(await screen.findByTestId('cloud-agent-instance-cag_1'));
        fireEvent.click(await screen.findByTestId('cloud-agent-task-task-1'));
        await waitFor(() => {
            expect(op.mock.calls.some((call) => call[1] === 'cloud_watch_task')).toBe(true);
        });
        const session = cloudAgentSessionKey('cag_1', 'task-1');
        expect(session).toBe('cloud-agent:cag_1:task-1');
        expect(cloudAgentStoreKey('grant-1', session)).toBe('grant-1\ncloud-agent:cag_1:task-1');
        expect(cloudAgentStoreKey('grant-1', '')).toBe('');
        await waitFor(() => expect(streamHandlers.length).toBeGreaterThan(0));
        await waitFor(() => expect(screen.getByTestId('cloud-agent-live').textContent).toContain('hi'));
        expect(screen.getByTestId('cloud-agent-live').textContent).not.toContain('old history');
        expect(op.mock.calls.some((call) => call[1] === 'cloud_list_tasks')).toBe(true);
    });

    it('the task-list entry is 云端管理 and the page does not use the desktop live key', () => {
        render(<CloudAgentEntryButton lang="zh" onOpen={() => {}} />);
        const entry = screen.getByTestId('task-pane-cloud-agents');
        expect(entry.textContent).toContain('云端管理');
        expect(entry.textContent).not.toContain('云端工作区');
        const root = resolve(__dirname, '..');
        const page = readFileSync(resolve(root, 'CloudAgentManage.tsx'), 'utf8');
        const fold = readFileSync(resolve(root, 'cloudAgentSession.ts'), 'utf8');
        const sidebar = readFileSync(resolve(root, 'SidebarTaskManagement.tsx'), 'utf8');
        expect(page.includes('liveKey') || page.includes('bindRemoteStreamEvents') || page.includes('applyRemoteStreamPayload') || page.includes('applyRemoteWatchSnapshot')).toBe(false);
        expect(fold.includes('liveKey') || fold.includes('bindRemoteStreamEvents') || fold.includes('applyRemoteStreamPayload') || fold.includes('applyRemoteWatchSnapshot')).toBe(false);
        expect(page).toContain('data-testid="task-pane-cloud-agents"');
        const wizardAt = sidebar.indexOf('data-testid="task-pane-new-task-wizard"');
        const entryAt = sidebar.indexOf('<CloudAgentEntryButton');
        expect(wizardAt).toBeGreaterThan(-1);
        expect(entryAt).toBeGreaterThan(wizardAt);
        expect(sidebar).toContain('data-testid="task-pane-new-task-wizard"');
    });

    it('creates, steers, and stops a task when none is selected, and folds the watch snapshot', async () => {
        const op = vi.fn(async (_grant: string, name: string, _body?: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [] });
            if (name === 'cloud_create_task') return JSON.stringify({ accepted: true, task_id: 'task-new' });
            if (name === 'cloud_watch_task') {
                return JSON.stringify({
                    request_id: 'req-1',
                    session_key: 'cloud-agent:cag_1:task-new',
                    project_path: 'should-stay-empty',
                    display_text: '〔云端〕start work',
                    parts: [{ kind: 'text', seq: 1, text: 'from-watch' }],
                    cursor: 2,
                    stream_done: false,
                    responded: false,
                });
            }
            return JSON.stringify({ accepted: true, request_id: 'req-1', task_id: 'task-new' });
        });
        setBindings({
            ListCloudAgents: vi.fn(async () => [{ id: 'cag_1', name: 'demo', kind: 'session', status: 'idle' }]),
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        render(<CloudAgentManage lang="zh" onClose={() => {}} />);
        fireEvent.click(await screen.findByTestId('cloud-agent-instance-cag_1'));
        await waitFor(() => expect(op.mock.calls.some((call) => call[1] === 'cloud_list_tasks')).toBe(true));
        expect(screen.queryByTestId('cloud-agent-git')).toBeNull();
        fireEvent.change(screen.getByLabelText('消息'), { target: { value: 'start work' } });
        fireEvent.click(screen.getByText('发送'));
        await waitFor(() => {
            const names = op.mock.calls.map((call) => call[1]);
            expect(names).toEqual(['cloud_list_tasks', 'cloud_create_task', 'cloud_watch_task', 'cloud_send_message']);
        });
        const send = op.mock.calls.find((call) => call[1] === 'cloud_send_message');
        expect(String(send?.[2])).toContain('"task_id":"task-new"');
        expect(String(send?.[2])).toContain('start work');
        await waitFor(() => expect(screen.getByTestId('cloud-agent-live').textContent).toContain('from-watch'));
        const key = cloudAgentStoreKey('grant-1', cloudAgentSessionKey('cag_1', 'task-new'));
        expect(cloudAgentRound(key).body).toContain('from-watch');
        fireEvent.click(screen.getByText('停止'));
        await waitFor(() => {
            const stop = op.mock.calls.find((call) => call[1] === 'cloud_stop_task');
            expect(String(stop?.[2])).toContain('task-new');
        });
    });

    it('connects GitHub from a coding instance and does not show the token again', async () => {
        const connect = vi.fn(async (_id?: string, _opts?: any) => ({ connected: true, repo: 'acme/demo', author_name: 'Ada', author_email: 'ada@example.com' }));
        setBindings({
            ListCloudAgents: vi.fn(async () => [{ id: 'cag_9', name: 'code', kind: 'coding', status: 'idle' }]),
            OpenCloudAgentGrant: vi.fn(async () => 'grant-9'),
            RemoteControlOp: vi.fn(async () => JSON.stringify({ tasks: [] })),
            ConnectCloudAgentGit: connect,
        });
        render(<CloudAgentManage lang="zh" onClose={() => {}} />);
        fireEvent.click(await screen.findByTestId('cloud-agent-instance-cag_9'));
        expect(await screen.findByTestId('cloud-agent-git')).toBeTruthy();
        fireEvent.change(screen.getByLabelText('代码库'), { target: { value: 'acme/demo' } });
        fireEvent.change(screen.getByLabelText('作者'), { target: { value: 'Ada' } });
        fireEvent.change(screen.getByLabelText('邮箱'), { target: { value: 'ada@example.com' } });
        fireEvent.change(screen.getByLabelText('令牌'), { target: { value: 'real-token-not-shown' } });
        fireEvent.click(screen.getByText('连接'));
        await waitFor(() => expect(connect).toHaveBeenCalledTimes(1));
        expect(connect.mock.calls[0][0]).toBe('cag_9');
        expect(connect.mock.calls[0][1]).toMatchObject({
            service: 'github',
            origin: 'https://github.com',
            repo: 'acme/demo',
            author_name: 'Ada',
            author_email: 'ada@example.com',
            token: 'real-token-not-shown',
        });
        await waitFor(() => expect(screen.getByTestId('cloud-agent-manage').textContent).toContain('acme/demo'));
        expect(screen.getByTestId('cloud-agent-manage').textContent).not.toContain('real-token-not-shown');
        expect((screen.getByLabelText('令牌') as HTMLInputElement).value).toBe('');
    });

    it('shows the push confirmation inside the round and sends the chosen answer', async () => {
        const op = vi.fn(async (_grant: string, name: string, _body?: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1', title: 'ship' }] });
            return '{}';
        });
        setBindings({
            ListCloudAgents: vi.fn(async () => [{ id: 'cag_1', name: 'demo', kind: 'coding', status: 'idle' }]),
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        render(<CloudAgentManage lang="zh" onClose={() => {}} />);
        fireEvent.click(await screen.findByTestId('cloud-agent-instance-cag_1'));
        fireEvent.click(await screen.findByTestId('cloud-agent-task-task-1'));
        await waitFor(() => expect(streamHandlers.length).toBeGreaterThan(0));
        await act(async () => {
            streamHandlers[0](JSON.stringify({
            grant_id: 'grant-1',
            session_key: 'cloud-agent:cag_1:task-1',
            project_path: '',
            event: 'response',
            cursor: 1,
            text: '请确认推送',
            terminal: {
                text: '请确认推送',
                ask_user_question: '请确认推送',
                ask_user_options_json: '["仅这一次","本任务内允许","拒绝"]',
                ask_user_input_type: 'choice',
            },
        }));
        });
        const key = cloudAgentStoreKey('grant-1', cloudAgentSessionKey('cag_1', 'task-1'));
        await waitFor(() => expect(cloudAgentRound(key).cards.some((card) => card.kind === 'confirmation' && card.confirmationId)).toBe(true));
        expect(screen.getByText('仅这一次')).toBeTruthy();
        expect(screen.getByText('本任务内允许')).toBeTruthy();
        expect(screen.getByText('拒绝')).toBeTruthy();
        fireEvent.click(screen.getByText('仅这一次'));
        await waitFor(() => {
            const send = op.mock.calls.find((call) => call[1] === 'cloud_send_message');
            expect(String(send?.[2])).toContain('仅这一次');
            expect(String(send?.[2])).toContain('task-1');
        });
    });

    it('drops the previous instance tasks while the next grant is still opening', async () => {
        let releaseSecond: () => void = () => {};
        const secondGate = new Promise<void>((resolve) => { releaseSecond = resolve; });
        const op = vi.fn(async (grant: string, name: string) => {
            if (name !== 'cloud_list_tasks') return '{}';
            if (grant === 'grant-1') return JSON.stringify({ tasks: [{ id: 'task-a', title: 'A' }] });
            return JSON.stringify({ tasks: [] });
        });
        setBindings({
            ListCloudAgents: vi.fn(async () => [
                { id: 'cag_1', name: 'one', kind: 'session', status: 'idle' },
                { id: 'cag_2', name: 'two', kind: 'session', status: 'idle' },
            ]),
            OpenCloudAgentGrant: vi.fn(async (id: string) => {
                if (id === 'cag_2') await secondGate;
                return id === 'cag_2' ? 'grant-2' : 'grant-1';
            }),
            RemoteControlOp: op,
        });
        render(<CloudAgentManage lang="zh" onClose={() => {}} />);
        fireEvent.click(await screen.findByTestId('cloud-agent-instance-cag_1'));
        expect(await screen.findByTestId('cloud-agent-task-task-a')).toBeTruthy();
        fireEvent.click(screen.getByTestId('cloud-agent-instance-cag_2'));
        await waitFor(() => expect(screen.queryByTestId('cloud-agent-task-task-a')).toBeNull());
        releaseSecond();
        await waitFor(() => expect(op.mock.calls.some((call) => call[0] === 'grant-2' && call[1] === 'cloud_list_tasks')).toBe(true));
        expect(screen.queryByTestId('cloud-agent-task-task-a')).toBeNull();
    });

    it('shows a create failure and does not send', async () => {
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [] });
            if (name === 'cloud_create_task') throw new Error('配额已满');
            return '{}';
        });
        setBindings({
            ListCloudAgents: vi.fn(async () => [{ id: 'cag_1', name: 'demo', kind: 'session', status: 'idle' }]),
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        render(<CloudAgentManage lang="zh" onClose={() => {}} />);
        fireEvent.click(await screen.findByTestId('cloud-agent-instance-cag_1'));
        await waitFor(() => expect(op.mock.calls.some((call) => call[1] === 'cloud_list_tasks')).toBe(true));
        fireEvent.change(screen.getByLabelText('消息'), { target: { value: 'start work' } });
        fireEvent.click(screen.getByText('发送'));
        expect((await screen.findByRole('alert')).textContent).toContain('配额已满');
        expect(op.mock.calls.some((call) => call[1] === 'cloud_send_message')).toBe(false);
    });

    it('clears the previous confirmation before the next task watch returns', async () => {
        let releaseNext: () => void = () => {};
        const gate = new Promise<void>((resolve) => { releaseNext = resolve; });
        const op = vi.fn(async (_grant: string, name: string, body?: string) => {
            if (name === 'cloud_list_tasks') {
                return JSON.stringify({ tasks: [{ id: 'task-1', title: 'A' }, { id: 'task-2', title: 'B' }] });
            }
            if (name === 'cloud_watch_task' && String(body || '').includes('task-2')) {
                await gate;
                return JSON.stringify({
                    request_id: 'req-2',
                    session_key: 'cloud-agent:cag_1:task-2',
                    project_path: '',
                    cursor: 1,
                    parts: [],
                });
            }
            return JSON.stringify({ messages: [] });
        });
        setBindings({
            ListCloudAgents: vi.fn(async () => [{ id: 'cag_1', name: 'demo', kind: 'coding', status: 'idle' }]),
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        render(<CloudAgentManage lang="zh" onClose={() => {}} />);
        fireEvent.click(await screen.findByTestId('cloud-agent-instance-cag_1'));
        fireEvent.click(await screen.findByTestId('cloud-agent-task-task-1'));
        await waitFor(() => expect(streamHandlers.length).toBeGreaterThan(0));
        await act(async () => {
            streamHandlers[0](JSON.stringify({
                grant_id: 'grant-1',
                session_key: 'cloud-agent:cag_1:task-1',
                project_path: '',
                event: 'response',
                cursor: 1,
                text: '请确认推送',
                terminal: {
                    text: '请确认推送',
                    confirmation: { id: 'commit-approval;task-1' },
                    actions: [{ label: '仅这一次', command: '仅这一次' }],
                },
            }));
        });
        expect(await screen.findByText('仅这一次')).toBeTruthy();
        fireEvent.click(screen.getByTestId('cloud-agent-task-task-2'));
        await waitFor(() => expect(screen.queryByText('仅这一次')).toBeNull());
        releaseNext();
        await waitFor(() => expect(op.mock.calls.some((call) => call[1] === 'cloud_watch_task' && String(call[2] || '').includes('task-2'))).toBe(true));
        expect(screen.queryByText('仅这一次')).toBeNull();
    });

    it('restores confirmation buttons from a watch snapshot once', () => {
        const session = cloudAgentSessionKey('cag_1', 'task-1');
        const snapshot = {
            request_id: 'req-1',
            session_key: session,
            project_path: '',
            cursor: 4,
            display_text: '〔云端〕请确认推送',
            parts: [{ kind: 'text', seq: 1, text: '请确认推送' }],
            terminal: {
                text: '请确认推送',
                ask_user_question: '请确认推送',
                ask_user_options_json: '["仅这一次","本任务内允许","拒绝"]',
                ask_user_input_type: 'choice',
                confirmation: { id: 'commit-approval;task-1' },
                actions: [
                    { label: '仅这一次', command: '仅这一次' },
                    { label: '本任务内允许', command: '本任务内允许' },
                    { label: '拒绝', command: '拒绝' },
                ],
            },
        };
        const key = applyCloudAgentWatch('grant-1', session, snapshot);
        applyCloudAgentWatch('grant-1', session, snapshot);
        const round = cloudAgentRound(key);
        const cards = round.cards.filter((card) => !card.expired && card.kind === 'confirmation');
        expect(cards).toHaveLength(1);
        expect((cards[0] as any).actions.map((action: any) => action.command)).toEqual(['仅这一次', '本任务内允许', '拒绝']);
        expect(round.body).toContain('请确认推送');
    });

    it('drops a confirmation when the main panel expires that ask', async () => {
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1', title: 'ship' }] });
            return '{}';
        });
        setBindings({
            ListCloudAgents: vi.fn(async () => [{ id: 'cag_1', name: 'demo', kind: 'coding', status: 'idle' }]),
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        render(<CloudAgentManage lang="zh" onClose={() => {}} />);
        fireEvent.click(await screen.findByTestId('cloud-agent-instance-cag_1'));
        fireEvent.click(await screen.findByTestId('cloud-agent-task-task-1'));
        await waitFor(() => expect(streamHandlers.length).toBeGreaterThan(0));
        await act(async () => {
            streamHandlers[0](JSON.stringify({
                grant_id: 'grant-1',
                session_key: 'cloud-agent:cag_1:task-1',
                project_path: '',
                event: 'response',
                cursor: 1,
                text: '请确认推送',
                terminal: {
                    text: '请确认推送',
                    confirmation: { id: 'commit-approval;task-1' },
                    actions: [{ label: '仅这一次', command: '仅这一次' }],
                },
            }));
        });
        expect(await screen.findByText('仅这一次')).toBeTruthy();
        const key = cloudAgentStoreKey('grant-1', cloudAgentSessionKey('cag_1', 'task-1'));
        await act(async () => {
            expireCloudAgentCards(key);
        });
        await waitFor(() => expect(screen.queryByText('仅这一次')).toBeNull());
    });

    it('stop closes the ask because cancel does not stream a response', async () => {
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1', title: 'ship' }] });
            if (name === 'cloud_stop_task') return JSON.stringify({ ok: true, cancelled: ['req-1'] });
            return '{}';
        });
        setBindings({
            ListCloudAgents: vi.fn(async () => [{ id: 'cag_1', name: 'demo', kind: 'coding', status: 'idle' }]),
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        render(<CloudAgentManage lang="zh" onClose={() => {}} />);
        fireEvent.click(await screen.findByTestId('cloud-agent-instance-cag_1'));
        fireEvent.click(await screen.findByTestId('cloud-agent-task-task-1'));
        await waitFor(() => expect(streamHandlers.length).toBeGreaterThan(0));
        await act(async () => {
            streamHandlers[0](JSON.stringify({
                grant_id: 'grant-1',
                session_key: 'cloud-agent:cag_1:task-1',
                project_path: '',
                event: 'response',
                cursor: 1,
                text: '请确认推送',
                terminal: {
                    text: '请确认推送',
                    confirmation: { id: 'commit-approval;task-1' },
                    actions: [{ label: '仅这一次', command: '仅这一次' }],
                },
            }));
        });
        expect(await screen.findByText('仅这一次')).toBeTruthy();
        fireEvent.click(screen.getByText('停止'));
        await waitFor(() => expect(op.mock.calls.some((call) => call[1] === 'cloud_stop_task')).toBe(true));
        await waitFor(() => expect(screen.queryByText('仅这一次')).toBeNull());
        const key = cloudAgentStoreKey('grant-1', cloudAgentSessionKey('cag_1', 'task-1'));
        expect(cloudAgentRound(key).responded).toBe(true);
    });
});
