// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { applyCloudAgentStream, applyCloudAgentWatch, cloudAgentRound, cloudAgentSessionKey, cloudAgentStoreKey, resetCloudAgentRounds } from '../cloudAgentSession';
import { cloudAgentCatalog, openCloudAgent, resetCloudAssistantForTests, startCloudAgentFromNewTask } from '../cloudAgentAssistant';
import { remoteAssistantSend, remoteAssistantSessionSnapshot, remoteAssistantStop } from '../remoteAssistantSession';
import { cloudTurnRunning, noteCloudTurn, resetCloudTurnNotes, type CloudAgentListItem } from '../taskPlace';

const streamHandlers: Array<(payload: unknown) => void> = [];

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: (_name: string, fn: (payload: unknown) => void) => {
        streamHandlers.push(fn);
        return () => {};
    },
}));

const item: CloudAgentListItem = {
    id: 'cag_1',
    name: '演示',
    kind: 'session',
    status: 'idle',
    updatedAt: '2026-11-23T00:00:00Z',
};

function setBindings(bindings: Record<string, (...args: never[]) => Promise<unknown>>) {
    (window as unknown as { go?: unknown }).go = { main: { App: bindings } };
}

afterEach(() => {
    resetCloudAssistantForTests();
    resetCloudAgentRounds();
    resetCloudTurnNotes();
    streamHandlers.length = 0;
    localStorage.removeItem('maclaw.cloud-agent-opened.v1');
    delete (window as unknown as { go?: unknown }).go;
});

describe('cloud agent assistant', () => {
    it('treats a missing Hub switch as closed, and an empty enabled list as open', () => {
        expect(cloudAgentCatalog([{ id: 'cag_1', name: '演示', kind: 'session', status: 'idle' }])).toEqual({
            instances: [{ id: 'cag_1', name: '演示', kind: 'session', status: 'idle' }],
            enabled: false,
        });
        expect(cloudAgentCatalog({ instances: [], enabled: false }).enabled).toBe(false);
        expect(cloudAgentCatalog({ instances: [], enabled: true })).toEqual({ instances: [], enabled: true });
        expect(cloudAgentCatalog(null)).toEqual({ instances: [], enabled: false });
    });

    it('returns the instance when control does not open, instead of looking uncreated', async () => {
        setBindings({
            CreateCloudAgent: vi.fn(async () => ({ id: 'cag_1', name: '演示', kind: 'session', status: 'idle' })),
        });
        const result = await startCloudAgentFromNewTask('演示', 'session', '你好', 'zh');
        expect(result.id).toBe('cag_1');
        expect(result.warning).toContain('云端控制不可用');
    });

    it('opens in the same panel as a remote task and does not mark the computer offline', async () => {
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1' }] });
            if (name === 'cloud_get_task') return JSON.stringify({ messages: ['user\n之前', 'assistant\nhello'] });
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        await openCloudAgent(item, 'zh');
        const session = remoteAssistantSessionSnapshot();
        expect(session?.machineName).toBe('云端');
        expect(session?.taskName).toBe('演示');
        expect(session?.projectPath).toBe('cloud-agent:cag_1');
        expect(session?.cloud).toBe(true);
        expect(session?.offline).toBe(false);
        expect(session?.canSend).toBe(true);
        expect(session?.error).toBe('');
        expect(session?.messages.map(message => message.content)).toEqual(['之前', 'hello']);
    });

    it('creates a task on the first send and shows a streamed draft', async () => {
        const op = vi.fn(async (_grant: string, name: string, body?: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [] });
            if (name === 'cloud_create_task') {
                expect(JSON.parse(String(body)).title).toBe('你好');
                return JSON.stringify({ accepted: true, task_id: 'task-9' });
            }
            if (name === 'cloud_send_message') return '{}';
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        await openCloudAgent(item, 'zh');
        expect(await remoteAssistantSend('你好')).toBe(true);
        const names = op.mock.calls.map(call => call[1]);
        const createdAt = names.indexOf('cloud_create_task');
        const sentAt = names.indexOf('cloud_send_message');
        expect(createdAt).toBeGreaterThanOrEqual(0);
        expect(sentAt).toBeGreaterThan(createdAt);
        const sent = op.mock.calls.find(call => call[1] === 'cloud_send_message');
        const payload = JSON.parse(String(sent?.[2]));
        expect(payload.task_id).toBe('task-9');
        expect(payload.text).toBe('你好');
        expect(payload.client_message_id).toMatch(/^cm_/);

        streamHandlers[0]?.({
            grant_id: 'grant-1',
            session_key: 'cloud-agent:cag_1:task-9',
            project_path: '',
            event: 'token',
            request_id: 'req_1',
            text: 'pong',
            proto: 2,
            cursor: 1,
        });
        const live = remoteAssistantSessionSnapshot();
        expect(live?.offline).toBe(false);
        expect(live?.messages.some(message => message.content === 'pong')).toBe(true);
        expect(live?.live?.parts.some(part => part.kind === 'text' && part.text === 'pong')).toBe(true);
        expect(live?.live?.responded).toBe(false);
        expect(live?.busy).toBe(true);
        expect(cloudTurnRunning('cag_1', 'idle')).toBe(true);
    });

    it('a finished watch is idle even when the list row still says running', async () => {
        const session = cloudAgentSessionKey('cag_1', 'task-1');
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1' }] });
            if (name === 'cloud_get_task') return JSON.stringify({ messages: ['assistant\ndone'] });
            if (name === 'cloud_watch_task') {
                return JSON.stringify({
                    request_id: 'req-1',
                    session_key: session,
                    cursor: 2,
                    responded: true,
                    parts: [{ kind: 'text', seq: 1, text: 'done' }],
                });
            }
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        await openCloudAgent({ ...item, status: 'running' }, 'zh');
        const sessionView = remoteAssistantSessionSnapshot();
        expect(sessionView?.busy).toBe(false);
        expect(sessionView?.messages.some(message => message.content === 'done')).toBe(true);
        expect(cloudTurnRunning('cag_1', 'running')).toBe(false);
    });

    it('a follow-up send stays busy and closes the previous ask', async () => {
        const session = cloudAgentSessionKey('cag_1', 'task-1');
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1' }] });
            if (name === 'cloud_get_task') return JSON.stringify({ messages: ['assistant\n请确认推送'] });
            if (name === 'cloud_watch_task') {
                return JSON.stringify({
                    request_id: 'req-1',
                    session_key: session,
                    cursor: 2,
                    responded: true,
                    parts: [{ kind: 'text', seq: 1, text: '请确认推送' }],
                    terminal: {
                        text: '请确认推送',
                        confirmation: { id: 'commit-approval;task-1' },
                        actions: [{ label: '仅这一次', command: '仅这一次' }],
                    },
                });
            }
            if (name === 'cloud_send_message') return '{}';
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        await openCloudAgent(item, 'zh');
        const opened = remoteAssistantSessionSnapshot();
        expect(opened?.busy).toBe(false);
        expect(opened?.messages.some(message => message.actions?.some(action => action.command === '仅这一次'))).toBe(true);
        expect(await remoteAssistantSend('继续')).toBe(true);
        const waiting = remoteAssistantSessionSnapshot();
        expect(waiting?.busy).toBe(true);
        expect(waiting?.messages.some(message => message.actions?.some(action => action.command === '仅这一次'))).toBe(false);
        expect(cloudTurnRunning('cag_1', 'idle')).toBe(true);
        streamHandlers[0]?.({
            grant_id: 'grant-1',
            session_key: session,
            project_path: '',
            event: 'round_started',
            request_id: 'req-2',
            display_text: '继续',
            cursor: 3,
        });
        expect(remoteAssistantSessionSnapshot()?.busy).toBe(true);
        streamHandlers[0]?.({
            grant_id: 'grant-1',
            session_key: session,
            project_path: '',
            event: 'response',
            request_id: 'req-2',
            cursor: 4,
            text: '好',
            terminal: { text: '好' },
        });
        const done = remoteAssistantSessionSnapshot();
        expect(done?.busy).toBe(false);
        expect(done?.messages.some(message => message.content === '好')).toBe(true);
        expect(cloudTurnRunning('cag_1', 'running')).toBe(false);
    });

    it('a new request closes an ask left by the previous round', () => {
        const session = cloudAgentSessionKey('cag_1', 'task-1');
        const key = applyCloudAgentWatch('grant-1', session, {
            request_id: 'req-1',
            session_key: session,
            cursor: 2,
            responded: true,
            parts: [{ kind: 'text', seq: 1, text: '请确认推送' }],
            terminal: {
                text: '请确认推送',
                confirmation: { id: 'commit-approval;task-1' },
                actions: [{ label: '仅这一次', command: '仅这一次' }],
            },
        });
        expect(cloudAgentRound(key).cards.some(card => !card.expired && card.kind === 'confirmation')).toBe(true);
        applyCloudAgentStream({
            grant_id: 'grant-1',
            session_key: session,
            event: 'round_started',
            request_id: 'req-2',
            cursor: 3,
            display_text: '继续',
        });
        expect(cloudAgentRound(key).cards.every(card => card.expired)).toBe(true);
    });

    it('reopens from the watch snapshot when the stored round cursor is already higher', async () => {
        const session = cloudAgentSessionKey('cag_1', 'task-1');
        const seeded = applyCloudAgentWatch('grant-1', session, {
            request_id: 'req-old',
            session_key: session,
            cursor: 9,
            responded: false,
            parts: [{ kind: 'text', seq: 1, text: 'stale' }],
        });
        expect(cloudAgentRound(seeded).body).toContain('stale');
        expect(cloudAgentRound(seeded).last).toBe(9);
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1' }] });
            if (name === 'cloud_get_task') return JSON.stringify({ messages: ['assistant\nfresh'] });
            if (name === 'cloud_watch_task') {
                return JSON.stringify({
                    request_id: 'req-new',
                    session_key: session,
                    cursor: 3,
                    responded: true,
                    parts: [{ kind: 'text', seq: 1, text: 'fresh' }],
                });
            }
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        await openCloudAgent({ ...item, status: 'running' }, 'zh');
        const key = cloudAgentStoreKey('grant-1', session);
        expect(cloudAgentRound(key).body).toContain('fresh');
        expect(cloudAgentRound(key).body).not.toContain('stale');
        const view = remoteAssistantSessionSnapshot();
        expect(view?.busy).toBe(false);
        expect(view?.messages.some(message => message.content.includes('stale'))).toBe(false);
        expect(view?.messages.some(message => message.content.includes('fresh'))).toBe(true);
        expect(cloudTurnRunning('cag_1', 'running')).toBe(false);
    });

    it('an empty watch drops a stale in-flight mark and the old draft', async () => {
        const session = cloudAgentSessionKey('cag_1', 'task-1');
        applyCloudAgentWatch('grant-1', session, {
            request_id: 'req-old',
            session_key: session,
            cursor: 9,
            parts: [{ kind: 'text', seq: 1, text: 'stale' }],
        });
        noteCloudTurn('cag_1', true);
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1' }] });
            if (name === 'cloud_get_task') return JSON.stringify({ messages: ['assistant\nhistory'] });
            if (name === 'cloud_watch_task') return JSON.stringify({ session_key: session, cursor: 0, parts: [] });
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        await openCloudAgent({ ...item, status: 'running' }, 'zh');
        const view = remoteAssistantSessionSnapshot();
        expect(view?.busy).toBe(false);
        expect(view?.messages.some(message => message.content.includes('stale'))).toBe(false);
        expect(view?.messages.some(message => message.content.includes('history'))).toBe(true);
        expect(cloudTurnRunning('cag_1', 'running')).toBe(false);
    });

    it('a rejected send does not leave the task in progress', async () => {
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1' }] });
            if (name === 'cloud_get_task') return JSON.stringify({ messages: [] });
            if (name === 'cloud_send_message') throw new Error('发送失败');
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        await openCloudAgent(item, 'zh');
        expect(await remoteAssistantSend('你好')).toBe(false);
        expect(remoteAssistantSessionSnapshot()?.busy).toBe(false);
        expect(remoteAssistantSessionSnapshot()?.error).toContain('发送失败');
        expect(cloudTurnRunning('cag_1', 'idle')).toBe(false);
    });

    it('stop closes the turn because cancel does not stream a response', async () => {
        let releaseStop: (value: string) => void = () => {};
        const stopGate = new Promise<string>((resolve) => { releaseStop = resolve; });
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1' }] });
            if (name === 'cloud_get_task') return JSON.stringify({ messages: [] });
            if (name === 'cloud_stop_task') return stopGate;
            if (name === 'cloud_send_message') return '{}';
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        await openCloudAgent(item, 'zh');
        expect(await remoteAssistantSend('你好')).toBe(true);
        streamHandlers[0]?.({
            grant_id: 'grant-1',
            session_key: 'cloud-agent:cag_1:task-1',
            project_path: '',
            event: 'token',
            request_id: 'req_1',
            text: 'pong',
            cursor: 1,
        });
        expect(remoteAssistantSessionSnapshot()?.busy).toBe(true);
        const stopping = remoteAssistantStop();
        for (let tick = 0; tick < 8 && !remoteAssistantSessionSnapshot()?.activity?.stopping; tick += 1) {
            await Promise.resolve();
        }
        expect(remoteAssistantSessionSnapshot()?.activity?.stopping).toBe(true);
        expect(remoteAssistantSessionSnapshot()?.busy).toBe(true);
        releaseStop(JSON.stringify({ ok: true, cancelled: ['req_1'] }));
        expect(await stopping).toBe(true);
        const after = remoteAssistantSessionSnapshot();
        expect(after?.busy).toBe(false);
        expect(after?.activity?.stopping).toBeFalsy();
        expect(after?.live?.responded).toBe(true);
        expect(after?.messages.some(message => message.content === 'pong')).toBe(true);
        expect(cloudTurnRunning('cag_1', 'running')).toBe(false);
    });

    it('a history reload in flight does not drop a newer send', async () => {
        let historyCalls = 0;
        let releaseHistory: (value: string) => void = () => {};
        const historyGate = new Promise<string>((resolve) => { releaseHistory = resolve; });
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1' }] });
            if (name === 'cloud_get_task') {
                historyCalls += 1;
                if (historyCalls === 1) return JSON.stringify({ messages: ['user\n之前'] });
                return historyGate;
            }
            if (name === 'cloud_send_message') return '{}';
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        await openCloudAgent(item, 'zh');
        streamHandlers[0]?.({
            grant_id: 'grant-1',
            session_key: 'cloud-agent:cag_1:task-1',
            project_path: '',
            event: 'response',
            request_id: 'req-1',
            cursor: 1,
            text: '好',
        });
        expect(historyCalls).toBe(2);
        expect(await remoteAssistantSend('下一句')).toBe(true);
        releaseHistory(JSON.stringify({ messages: ['user\n之前', 'assistant\n好'] }));
        for (let tick = 0; tick < 8; tick += 1) await Promise.resolve();
        const contents = remoteAssistantSessionSnapshot()?.messages.map(message => message.content) || [];
        expect(contents.some(content => content.includes('下一句'))).toBe(true);
    });

    it('the opening history fetch does not drop a send that started during it', async () => {
        let releaseHistory: (value: string) => void = () => {};
        const historyGate = new Promise<string>((resolve) => { releaseHistory = resolve; });
        let sawHistory = false;
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1' }] });
            if (name === 'cloud_get_task') {
                sawHistory = true;
                return historyGate;
            }
            if (name === 'cloud_send_message') return '{}';
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        const opening = openCloudAgent(item, 'zh');
        for (let tick = 0; tick < 20 && !sawHistory; tick += 1) await Promise.resolve();
        expect(sawHistory).toBe(true);
        streamHandlers[0]?.({
            grant_id: 'grant-1',
            session_key: 'cloud-agent:cag_1:task-1',
            project_path: '',
            event: 'token',
            request_id: 'req-1',
            text: '部分',
            cursor: 1,
        });
        expect(await remoteAssistantSend('下一句')).toBe(true);
        releaseHistory(JSON.stringify({ messages: ['user\n之前'] }));
        await opening;
        for (let tick = 0; tick < 8; tick += 1) await Promise.resolve();
        const contents = remoteAssistantSessionSnapshot()?.messages.map(message => message.content) || [];
        expect(contents.some(content => content.includes('下一句'))).toBe(true);
        expect(contents.some(content => content.includes('之前'))).toBe(false);
    });

    it('a slower history fetch does not replace a newer one', async () => {
        let releaseOpen: (value: string) => void = () => {};
        let releaseReload: (value: string) => void = () => {};
        const openGate = new Promise<string>((resolve) => { releaseOpen = resolve; });
        const reloadGate = new Promise<string>((resolve) => { releaseReload = resolve; });
        let historyCalls = 0;
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1' }] });
            if (name === 'cloud_get_task') {
                historyCalls += 1;
                return historyCalls === 1 ? openGate : reloadGate;
            }
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        const opening = openCloudAgent(item, 'zh');
        for (let tick = 0; tick < 20 && historyCalls < 1; tick += 1) await Promise.resolve();
        expect(historyCalls).toBe(1);
        streamHandlers[0]?.({
            grant_id: 'grant-1',
            session_key: 'cloud-agent:cag_1:task-1',
            project_path: '',
            event: 'response',
            request_id: 'req-1',
            cursor: 1,
            text: '新',
        });
        for (let tick = 0; tick < 20 && historyCalls < 2; tick += 1) await Promise.resolve();
        expect(historyCalls).toBe(2);
        releaseReload(JSON.stringify({ messages: ['assistant\n新'] }));
        for (let tick = 0; tick < 8; tick += 1) await Promise.resolve();
        expect(remoteAssistantSessionSnapshot()?.messages.some(message => message.content.includes('新'))).toBe(true);
        releaseOpen(JSON.stringify({ messages: ['assistant\n旧'] }));
        await opening;
        for (let tick = 0; tick < 8; tick += 1) await Promise.resolve();
        const contents = remoteAssistantSessionSnapshot()?.messages.map(message => message.content) || [];
        expect(contents.some(content => content.includes('新'))).toBe(true);
        expect(contents.some(content => content.includes('旧'))).toBe(false);
    });

    it('a rejected send after opening another task clears the old in-progress mark', async () => {
        let rejectSend: (err: Error) => void = () => {};
        const sendGate = new Promise<string>((_resolve, reject) => { rejectSend = reject; });
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1' }] });
            if (name === 'cloud_get_task') return JSON.stringify({ messages: [] });
            if (name === 'cloud_send_message') return sendGate;
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async (id: string) => (id === 'cag_2' ? 'grant-2' : 'grant-1')),
            RemoteControlOp: op,
        });
        await openCloudAgent(item, 'zh');
        const sending = remoteAssistantSend('你好');
        for (let tick = 0; tick < 8 && !cloudTurnRunning('cag_1', 'idle'); tick += 1) await Promise.resolve();
        expect(cloudTurnRunning('cag_1', 'idle')).toBe(true);
        await openCloudAgent({ ...item, id: 'cag_2', name: '另一个' }, 'zh');
        rejectSend(new Error('发送失败'));
        expect(await sending).toBe(false);
        expect(cloudTurnRunning('cag_1', 'idle')).toBe(false);
        const view = remoteAssistantSessionSnapshot();
        expect(view?.projectPath).toBe('cloud-agent:cag_2');
        expect(view?.error).toBe('');
    });

    it('a send accepted after opening another task stays on the task that sent it', async () => {
        let releaseSend: (value: string) => void = () => {};
        const sendGate = new Promise<string>((resolve) => { releaseSend = resolve; });
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1' }] });
            if (name === 'cloud_get_task') return JSON.stringify({ messages: [] });
            if (name === 'cloud_send_message') return sendGate;
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async (id: string) => (id === 'cag_2' ? 'grant-2' : 'grant-1')),
            RemoteControlOp: op,
        });
        await openCloudAgent(item, 'zh');
        const sending = remoteAssistantSend('你好');
        for (let tick = 0; tick < 8 && !cloudTurnRunning('cag_1', 'idle'); tick += 1) await Promise.resolve();
        expect(cloudTurnRunning('cag_1', 'idle')).toBe(true);
        await openCloudAgent({ ...item, id: 'cag_2', name: '另一个' }, 'zh');
        releaseSend('{}');
        expect(await sending).toBe(true);
        expect(cloudTurnRunning('cag_1', 'idle')).toBe(true);
        const view = remoteAssistantSessionSnapshot();
        expect(view?.projectPath).toBe('cloud-agent:cag_2');
        expect(view?.taskName).toBe('另一个');
        expect(view?.messages.some(message => message.content.includes('你好'))).toBe(false);
    });

    it('keeps a token that arrived while the opening watch was in flight', async () => {
        let releaseWatch: (value: string) => void = () => {};
        const watchGate = new Promise<string>((resolve) => { releaseWatch = resolve; });
        const session = cloudAgentSessionKey('cag_1', 'task-1');
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1' }] });
            if (name === 'cloud_get_task') return JSON.stringify({ messages: [] });
            if (name === 'cloud_watch_task') return watchGate;
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        const opening = openCloudAgent(item, 'zh');
        for (let tick = 0; tick < 20 && !op.mock.calls.some(call => call[1] === 'cloud_watch_task'); tick += 1) {
            await Promise.resolve();
        }
        expect(op.mock.calls.some(call => call[1] === 'cloud_watch_task')).toBe(true);
        streamHandlers[0]?.({
            grant_id: 'grant-1',
            session_key: session,
            project_path: '',
            event: 'token',
            request_id: 'req-1',
            text: '刚到',
            cursor: 1,
        });
        releaseWatch(JSON.stringify({ session_key: session, cursor: 0, parts: [] }));
        await opening;
        const key = cloudAgentStoreKey('grant-1', session);
        expect(cloudAgentRound(key).body).toContain('刚到');
        expect(remoteAssistantSessionSnapshot()?.messages.some(message => message.content.includes('刚到'))).toBe(true);
    });

    it('a newer watch snapshot still replaces the token that arrived during it', async () => {
        let releaseWatch: (value: string) => void = () => {};
        const watchGate = new Promise<string>((resolve) => { releaseWatch = resolve; });
        const session = cloudAgentSessionKey('cag_1', 'task-1');
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1' }] });
            if (name === 'cloud_get_task') return JSON.stringify({ messages: ['assistant\n快照'] });
            if (name === 'cloud_watch_task') return watchGate;
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        const opening = openCloudAgent(item, 'zh');
        for (let tick = 0; tick < 20 && !op.mock.calls.some(call => call[1] === 'cloud_watch_task'); tick += 1) {
            await Promise.resolve();
        }
        streamHandlers[0]?.({
            grant_id: 'grant-1',
            session_key: session,
            project_path: '',
            event: 'token',
            request_id: 'req-1',
            text: '刚到',
            cursor: 1,
        });
        releaseWatch(JSON.stringify({
            request_id: 'req-1',
            session_key: session,
            cursor: 2,
            responded: true,
            parts: [{ kind: 'text', seq: 1, text: '快照' }],
        }));
        await opening;
        const key = cloudAgentStoreKey('grant-1', session);
        expect(cloudAgentRound(key).body).toContain('快照');
        expect(cloudAgentRound(key).body).not.toContain('刚到');
    });

    it('a previous round closing does not clear a follow-up that was sent from another task', async () => {
        let releaseSend: (value: string) => void = () => {};
        const sendGate = new Promise<string>((resolve) => { releaseSend = resolve; });
        const session = cloudAgentSessionKey('cag_1', 'task-1');
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1' }] });
            if (name === 'cloud_get_task') return JSON.stringify({ messages: [] });
            if (name === 'cloud_send_message') return sendGate;
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async (id: string) => (id === 'cag_2' ? 'grant-2' : 'grant-1')),
            RemoteControlOp: op,
        });
        await openCloudAgent(item, 'zh');
        streamHandlers[0]?.({
            grant_id: 'grant-1',
            session_key: session,
            project_path: '',
            event: 'token',
            request_id: 'req-1',
            text: '部分',
            cursor: 1,
        });
        const sending = remoteAssistantSend('下一句');
        for (let tick = 0; tick < 8 && !cloudTurnRunning('cag_1', 'idle'); tick += 1) await Promise.resolve();
        expect(cloudTurnRunning('cag_1', 'idle')).toBe(true);
        await openCloudAgent({ ...item, id: 'cag_2', name: '另一个' }, 'zh');
        streamHandlers[0]?.({
            grant_id: 'grant-1',
            session_key: session,
            project_path: '',
            event: 'response',
            request_id: 'req-1',
            cursor: 2,
            text: '好',
            terminal: { text: '好' },
        });
        expect(cloudTurnRunning('cag_1', 'idle')).toBe(true);
        streamHandlers[0]?.({
            grant_id: 'grant-1',
            session_key: session,
            project_path: '',
            event: 'round_started',
            request_id: 'req-2',
            cursor: 3,
            display_text: '下一句',
        });
        expect(cloudTurnRunning('cag_1', 'idle')).toBe(true);
        streamHandlers[0]?.({
            grant_id: 'grant-1',
            session_key: session,
            project_path: '',
            event: 'response',
            request_id: 'req-2',
            cursor: 4,
            text: '好',
            terminal: { text: '好' },
        });
        expect(cloudTurnRunning('cag_1', 'idle')).toBe(false);
        releaseSend('{}');
        await sending;
    });

    it('a history reload keeps a send the server has not stored yet', async () => {
        let historyCalls = 0;
        let releaseReload: (value: string) => void = () => {};
        const reloadGate = new Promise<string>((resolve) => { releaseReload = resolve; });
        const session = cloudAgentSessionKey('cag_1', 'task-1');
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1' }] });
            if (name === 'cloud_get_task') {
                historyCalls += 1;
                if (historyCalls === 1) return JSON.stringify({ messages: ['user\n之前'] });
                return reloadGate;
            }
            if (name === 'cloud_send_message') return '{}';
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        await openCloudAgent(item, 'zh');
        expect(await remoteAssistantSend('下一句')).toBe(true);
        streamHandlers[0]?.({
            grant_id: 'grant-1',
            session_key: session,
            project_path: '',
            event: 'response',
            request_id: 'req-1',
            cursor: 1,
            text: '好',
            terminal: { text: '好' },
        });
        for (let tick = 0; tick < 8 && historyCalls < 2; tick += 1) await Promise.resolve();
        expect(historyCalls).toBe(2);
        releaseReload(JSON.stringify({ messages: ['user\n之前', 'assistant\n好'] }));
        for (let tick = 0; tick < 8; tick += 1) await Promise.resolve();
        const contents = remoteAssistantSessionSnapshot()?.messages.map(message => message.content) || [];
        expect(contents.filter(content => content.includes('下一句'))).toEqual(['下一句']);
        expect(contents.some(content => content.includes('好'))).toBe(true);
        const sentAt = contents.findIndex(content => content.includes('下一句'));
        const replyAt = contents.findIndex(content => content.includes('好'));
        expect(replyAt).toBeGreaterThanOrEqual(0);
        expect(sentAt).toBeGreaterThan(replyAt);
    });

    it('places the previous reply above the follow-up line once', async () => {
        const session = cloudAgentSessionKey('cag_1', 'task-1');
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1' }] });
            if (name === 'cloud_get_task') return JSON.stringify({ messages: ['assistant\n旧回复'] });
            if (name === 'cloud_watch_task') {
                return JSON.stringify({
                    request_id: 'req-1',
                    session_key: session,
                    cursor: 2,
                    responded: true,
                    parts: [{ kind: 'text', seq: 1, text: '旧回复' }],
                });
            }
            if (name === 'cloud_send_message') return '{}';
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        await openCloudAgent(item, 'zh');
        expect(await remoteAssistantSend('下一句')).toBe(true);
        const contents = remoteAssistantSessionSnapshot()?.messages.map(message => message.content) || [];
        expect(contents.filter(content => content.includes('旧回复'))).toEqual(['旧回复']);
        const replyAt = contents.findIndex(content => content.includes('旧回复'));
        const sentAt = contents.findIndex(content => content.includes('下一句'));
        expect(sentAt).toBeGreaterThan(replyAt);
    });

    it('reopening a task whose lane is gone clears a follow-up mark', async () => {
        let watches = 0;
        let releaseSend: (value: string) => void = () => {};
        const sendGate = new Promise<string>((resolve) => { releaseSend = resolve; });
        const session = cloudAgentSessionKey('cag_1', 'task-1');
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1' }] });
            if (name === 'cloud_get_task') return JSON.stringify({ messages: [] });
            if (name === 'cloud_watch_task') {
                watches += 1;
                if (watches >= 3) return JSON.stringify({ session_key: session, cursor: 0, parts: [] });
                return '{}';
            }
            if (name === 'cloud_send_message') return sendGate;
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async (id: string) => (id === 'cag_2' ? 'grant-2' : 'grant-1')),
            RemoteControlOp: op,
        });
        await openCloudAgent(item, 'zh');
        streamHandlers[0]?.({
            grant_id: 'grant-1',
            session_key: session,
            project_path: '',
            event: 'token',
            request_id: 'req-1',
            text: '部分',
            cursor: 1,
        });
        const sending = remoteAssistantSend('下一句');
        for (let tick = 0; tick < 8 && !cloudTurnRunning('cag_1', 'idle'); tick += 1) await Promise.resolve();
        expect(cloudTurnRunning('cag_1', 'idle')).toBe(true);
        await openCloudAgent({ ...item, id: 'cag_2', name: '另一个' }, 'zh');
        expect(cloudTurnRunning('cag_1', 'idle')).toBe(true);
        await openCloudAgent(item, 'zh');
        expect(cloudTurnRunning('cag_1', 'idle')).toBe(false);
        releaseSend('{}');
        await sending;
        expect(cloudTurnRunning('cag_1', 'idle')).toBe(false);
    });

    it('a parked send shows the queued badge until its round starts', async () => {
        const session = cloudAgentSessionKey('cag_1', 'task-1');
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1' }] });
            if (name === 'cloud_get_task') return JSON.stringify({ messages: [] });
            if (name === 'cloud_send_message') {
                return JSON.stringify({ accepted: true, queued: true, queue_id: 'que_1', task_id: 'task-1' });
            }
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        await openCloudAgent(item, 'zh');
        expect(await remoteAssistantSend('排队的话')).toBe(true);
        const parked = remoteAssistantSessionSnapshot()?.messages.find(message => message.role === 'user' && message.content === '排队的话');
        expect(parked?.queued).toBe(true);

        // The srv started the parked turn — the badge goes away with it.
        streamHandlers[0]?.({
            grant_id: 'grant-1',
            session_key: session,
            project_path: '',
            event: 'round_started',
            request_id: 'req-1',
            cursor: 1,
            display_text: '排队的话',
        });
        const started = remoteAssistantSessionSnapshot()?.messages.find(message => message.role === 'user' && message.content === '排队的话');
        expect(started?.queued).toBeFalsy();
    });

    it('folds styled tool progress into the live row tool calls', async () => {
        const session = cloudAgentSessionKey('cag_1', 'task-1');
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1' }] });
            if (name === 'cloud_get_task') return JSON.stringify({ messages: [] });
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        await openCloudAgent(item, 'zh');
        streamHandlers[0]?.({
            grant_id: 'grant-1',
            session_key: session,
            project_path: '',
            event: 'round_started',
            request_id: 'req-1',
            cursor: 1,
            display_text: '干活',
        });
        streamHandlers[0]?.({
            grant_id: 'grant-1',
            session_key: session,
            project_path: '',
            event: 'progress',
            request_id: 'req-1',
            cursor: 2,
            text: '工具 · 执行命令 (bash)\ngit status',
        });
        const row = remoteAssistantSessionSnapshot()?.messages.find(message => message.remoteLive);
        expect(row?.toolCalls?.length).toBe(1);
        expect(row?.toolCalls?.[0].name).toBe('bash');
        expect(row?.toolCalls?.[0].action).toBe('执行命令');
        expect(row?.toolCalls?.[0].detail).toBe('git status');
        expect(row?.content).toContain('<!--maclaw-tool:');
    });

    it('carries produced files from history onto transcript rows', async () => {
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1' }] });
            if (name === 'cloud_get_task') return JSON.stringify({
                messages: ['user\n做个表', 'assistant\n报表已生成'],
                files: [null, ['out/report.csv']],
            });
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        await openCloudAgent(item, 'zh');
        const rows = remoteAssistantSessionSnapshot()?.messages || [];
        expect(rows[0].localFilePaths).toBeUndefined();
        expect(rows[1].localFilePaths).toEqual(['out/report.csv']);
    });

    it('attaches a response terminal local_file_paths to the live row', async () => {
        const session = cloudAgentSessionKey('cag_1', 'task-1');
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1' }] });
            if (name === 'cloud_get_task') return JSON.stringify({ messages: [] });
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        await openCloudAgent(item, 'zh');
        streamHandlers[0]?.({
            grant_id: 'grant-1',
            session_key: session,
            event: 'round_started',
            request_id: 'req-1',
            cursor: 1,
            display_text: '出图',
        });
        streamHandlers[0]?.({
            grant_id: 'grant-1',
            session_key: session,
            event: 'response',
            request_id: 'req-1',
            cursor: 2,
            text: '图好了',
            terminal: { text: '图好了', local_file_paths: ['pics/a.png'] },
        });
        const row = remoteAssistantSessionSnapshot()?.messages.find(message => message.localFilePaths);
        expect(row?.localFilePaths).toEqual(['pics/a.png']);

        // A new turn owns a new artifact list — the previous round's files
        // stay on its history row, not on the next live row.
        streamHandlers[0]?.({
            grant_id: 'grant-1',
            session_key: session,
            event: 'round_started',
            request_id: 'req-2',
            cursor: 3,
            display_text: '再来一张',
        });
        const key = cloudAgentStoreKey('grant-1', session);
        expect(cloudAgentRound(key).artifacts).toEqual([]);
    });
});

describe('cloud stream resync', () => {
    it('re-watches the task when a stream frame gaps', async () => {
        const session = cloudAgentSessionKey('cag_1', 'task-1');
        const key = cloudAgentStoreKey('grant-1', session);
        let watchCalls = 0;
        const op = vi.fn(async (_grant: string, name: string) => {
            if (name === 'cloud_list_tasks') return JSON.stringify({ tasks: [{ id: 'task-1' }] });
            if (name === 'cloud_get_task') return JSON.stringify({ messages: [] });
            if (name === 'cloud_watch_task') {
                watchCalls += 1;
                if (watchCalls === 1) return JSON.stringify({ session_key: session, cursor: 0, parts: [] });
                return JSON.stringify({
                    session_key: session,
                    request_id: 'req-1',
                    cursor: 5,
                    parts: [{ kind: 'text', seq: 1, text: 'recovered body' }],
                });
            }
            return '{}';
        });
        setBindings({
            OpenCloudAgentGrant: vi.fn(async () => 'grant-1'),
            RemoteControlOp: op,
        });
        await openCloudAgent(item, 'zh');
        const before = watchCalls;
        streamHandlers[0]?.({
            grant_id: 'grant-1',
            session_key: session,
            event: 'round_started',
            request_id: 'req-1',
            cursor: 1,
            display_text: 'hi',
        });
        // Cursors 2-4 never arrived: the reducer marks the gap.
        streamHandlers[0]?.({
            grant_id: 'grant-1',
            session_key: session,
            event: 'token',
            request_id: 'req-1',
            cursor: 5,
            cursor_from: 5,
            text: 'tail',
        });
        expect(cloudAgentRound(key).gap).toBe(true);
        await vi.waitFor(() => expect(watchCalls).toBeGreaterThan(before));
        await vi.waitFor(() => {
            expect(cloudAgentRound(key).gap).toBe(false);
            expect(cloudAgentRound(key).body).toContain('recovered body');
        });
    });
});
