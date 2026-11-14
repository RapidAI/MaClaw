// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
    applyRemoteStreamPayload,
    applyRemoteWatchSnapshot,
    bindRemoteAssistantActions,
    publishRemoteAssistantSession,
    remoteAssistantSend,
    remoteAssistantSessionSnapshot,
} from '../remoteAssistantSession';
import type { RemoteAssistantSession } from '../remoteAssistantSession';

function session(overrides: Partial<RemoteAssistantSession> = {}): RemoteAssistantSession {
    return {
        grantId: 'g1',
        machineName: '家里',
        projectPath: 'D:/tasks/a',
        taskName: 'a',
        canSend: true,
        offline: false,
        error: '',
        messages: [],
        ...overrides,
    };
}

function texts() {
    return (remoteAssistantSessionSnapshot()?.messages || []).map(message => `${message.role}:${message.content}`);
}

describe('remoteAssistantSession sent-line echo', () => {
    beforeEach(() => {
        bindRemoteAssistantActions(null);
        publishRemoteAssistantSession(null);
    });

    it('shows the sent line immediately and keeps it ahead of the reply', async () => {
        publishRemoteAssistantSession(session());
        bindRemoteAssistantActions({ send: vi.fn().mockResolvedValue(true), stop: vi.fn(), close: vi.fn() });
        await remoteAssistantSend('继续');
        expect(texts()).toEqual(['user:继续']);
        // The remote answers without echoing the input (workflow-handled turn):
        // the sent line must stay, before the reply it triggered.
        publishRemoteAssistantSession(session({ messages: [{ role: 'assistant', content: '已继续执行' }] }));
        expect(texts()).toEqual(['user:继续', 'assistant:已继续执行']);
    });

    it('settles the echo when the transcript lands the real user row', async () => {
        publishRemoteAssistantSession(session());
        bindRemoteAssistantActions({ send: vi.fn().mockResolvedValue(true), stop: vi.fn(), close: vi.fn() });
        await remoteAssistantSend('继续');
        publishRemoteAssistantSession(session({
            messages: [
                { role: 'user', content: '继续' },
                { role: 'assistant', content: '已继续执行' },
            ],
        }));
        expect(texts()).toEqual(['user:继续', 'assistant:已继续执行']);
    });

    it('drops the echo when the send fails', async () => {
        publishRemoteAssistantSession(session());
        bindRemoteAssistantActions({ send: vi.fn().mockResolvedValue(false), stop: vi.fn(), close: vi.fn() });
        await remoteAssistantSend('继续');
        expect(texts()).toEqual([]);
    });

    it('keeps the echo slot where the send happened, not after new output', async () => {
        publishRemoteAssistantSession(session({ messages: [{ role: 'assistant', content: '上次完成' }] }));
        bindRemoteAssistantActions({ send: vi.fn().mockResolvedValue(true), stop: vi.fn(), close: vi.fn() });
        await remoteAssistantSend('整理报告');
        publishRemoteAssistantSession(session({
            messages: [
                { role: 'assistant', content: '上次完成' },
                { role: 'assistant', content: '正在整理…' },
            ],
        }));
        expect(texts()).toEqual(['assistant:上次完成', 'user:整理报告', 'assistant:正在整理…']);
    });

    it('keeps a second identical send visible until its own echo lands', async () => {
        publishRemoteAssistantSession(session());
        const send = vi.fn().mockResolvedValue(true);
        bindRemoteAssistantActions({ send, stop: vi.fn(), close: vi.fn() });
        await remoteAssistantSend('继续');
        // The first send landed; the second has not — both stay visible.
        publishRemoteAssistantSession(session({ messages: [{ role: 'user', content: '继续' }] }));
        await remoteAssistantSend('继续');
        expect(texts()).toEqual(['user:继续', 'user:继续']);
        publishRemoteAssistantSession(session({
            messages: [
                { role: 'user', content: '继续' },
                { role: 'assistant', content: '第一次回复' },
                { role: 'user', content: '继续' },
            ],
        }));
        expect(texts()).toEqual(['user:继续', 'assistant:第一次回复', 'user:继续']);
    });
});

describe('remoteAssistantSession live mirror', () => {
    beforeEach(() => {
        bindRemoteAssistantActions(null);
        publishRemoteAssistantSession(null);
    });

    it('grows the open thinking block on delta events and seals it on done', () => {
        publishRemoteAssistantSession(session());
        applyRemoteStreamPayload({ grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1', event: 'thinking', state: 'started', seq: 1 });
        applyRemoteStreamPayload({ grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1', event: 'thinking', state: 'delta', text: '先想', seq: 2 });
        applyRemoteStreamPayload({ grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1', event: 'thinking', state: 'delta', text: '一想', seq: 3 });
        let live = remoteAssistantSessionSnapshot()?.live;
        expect(live?.parts).toEqual([{ kind: 'thinking', seq: 1, text: '先想一想', done: false }]);

        applyRemoteStreamPayload({ grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1', event: 'thinking', state: 'done', text: '先想一想', seq: 4 });
        live = remoteAssistantSessionSnapshot()?.live;
        expect(live?.parts).toEqual([{ kind: 'thinking', seq: 1, text: '先想一想', done: true }]);
    });

    it('treats heartbeat as liveness without painting a part', () => {
        publishRemoteAssistantSession(session());
        applyRemoteStreamPayload({ grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1', event: 'heartbeat', seq: 1 });
        const live = remoteAssistantSessionSnapshot()?.live;
        expect(live?.requestId).toBe('r1');
        expect(live?.parts).toEqual([]);
    });

    it('mirrors the workbench checklist onto the session without a live round', () => {
        publishRemoteAssistantSession(session());
        applyRemoteStreamPayload({
            grant_id: 'g1',
            project_path: 'D:/tasks/a',
            event: 'workbench_steps',
            workbench: {
                steps_revision: 2,
                step_statuses: [{ index: 1, title: '实现功能', status: 'running' }],
            },
        });
        const snap = remoteAssistantSessionSnapshot();
        expect(snap?.workbench?.steps_revision).toBe(2);
        expect(snap?.workbench?.step_statuses).toHaveLength(1);
        expect(snap?.live).toBeFalsy();
    });

    it('applies the watch snapshot live turn and workbench together', () => {
        publishRemoteAssistantSession(session());
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r1',
            project_path: 'D:/tasks/a',
            display_text: '〔远控〕继续',
            parts: [
                { kind: 'thinking', seq: 1, text: '想想', done: false },
                { kind: 'text', seq: 2, text: '半句' },
            ],
        }, { step_statuses: [{ index: 1, title: 'plan', status: 'running' }] });
        const snap = remoteAssistantSessionSnapshot();
        expect(snap?.live?.requestId).toBe('r1');
        expect(snap?.workbench?.step_statuses).toHaveLength(1);
        expect(texts()).toEqual(['user:继续', 'assistant:半句']);
    });

    it('keeps the live round when a response for an older request lands late', () => {
        publishRemoteAssistantSession(session());
        applyRemoteStreamPayload({ grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r2', event: 'round_started', display_text: '第二轮', seq: 1 });
        applyRemoteStreamPayload({ grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r2', event: 'token', text: '进行中', seq: 2 });
        applyRemoteStreamPayload({ grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1', event: 'response', response: { text: '上一轮' }, seq: 3 });
        const live = remoteAssistantSessionSnapshot()?.live;
        expect(live?.requestId).toBe('r2');
        expect(live?.responded).toBe(false);
        expect(live?.parts).toEqual([{ kind: 'text', seq: 2, text: '进行中' }]);
    });

    it('ignores a watch snapshot that trails the live round', () => {
        publishRemoteAssistantSession(session());
        applyRemoteStreamPayload({ grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r2', event: 'round_started', display_text: '新一轮', seq: 1 });
        applyRemoteStreamPayload({ grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r2', event: 'token', text: '新内容', seq: 2 });
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r1',
            project_path: 'D:/tasks/a',
            display_text: '旧轮',
            parts: [{ kind: 'text', seq: 1, text: '旧内容' }],
        });
        const live = remoteAssistantSessionSnapshot()?.live;
        expect(live?.requestId).toBe('r2');
        expect(live?.parts).toEqual([{ kind: 'text', seq: 2, text: '新内容' }]);
    });
});
