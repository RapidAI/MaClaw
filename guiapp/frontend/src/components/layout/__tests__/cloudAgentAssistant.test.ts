// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { resetCloudAgentRounds } from '../cloudAgentSession';
import { openCloudAgent, resetCloudAssistantForTests } from '../cloudAgentAssistant';
import { remoteAssistantSend, remoteAssistantSessionSnapshot } from '../remoteAssistantSession';
import type { CloudAgentListItem } from '../taskPlace';

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
    streamHandlers.length = 0;
    localStorage.removeItem('maclaw.cloud-agent-opened.v1');
    delete (window as unknown as { go?: unknown }).go;
});

describe('cloud agent assistant', () => {
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
    });
});
