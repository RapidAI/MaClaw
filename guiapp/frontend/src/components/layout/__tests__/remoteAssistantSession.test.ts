// @vitest-environment jsdom
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
    applyRemoteStreamPayload,
    applyRemoteWatchSnapshot,
    bindRemoteAssistantActions,
    bindRemoteStreamRecovery,
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
        bindRemoteStreamRecovery(null);
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
        bindRemoteStreamRecovery(null);
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

    it('keeps folding proto-2 frames after a mid-turn watch snapshot', () => {
        publishRemoteAssistantSession(session());
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r1',
            project_path: 'D:/tasks/a',
            display_text: '〔远控〕继续',
            cursor: 10,
            parts: [
                { kind: 'thinking', seq: 1, text: '想想', done: true },
                { kind: 'text', seq: 2, text: '半句' },
            ],
        }, undefined, 10);
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1',
            event: 'token', cursor_from: 11, cursor: 11, seq: 11, text: '继续输出',
        });
        const live = remoteAssistantSessionSnapshot()?.live;
        expect(live?.requestId).toBe('r1');
        expect(live?.parts.filter(part => part.kind === 'text').map(part => part.text).join('')).toBe('半句继续输出');
    });

    it('does not regress the reduce state when a watch snapshot trails newer frames', () => {
        publishRemoteAssistantSession(session());
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1',
            event: 'round_started', cursor_from: 1, cursor: 1, seq: 1, display_text: '新一轮',
        });
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1',
            event: 'token', cursor_from: 2, cursor: 5, seq: 5, pieces: ['新', '内容'],
        });
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r1',
            project_path: 'D:/tasks/a',
            display_text: '新一轮',
            cursor: 4,
            parts: [{ kind: 'text', seq: 1, text: '新' }],
        }, undefined, 4);
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1',
            event: 'token', cursor_from: 6, cursor: 6, seq: 6, text: '尾巴',
        });
        const live = remoteAssistantSessionSnapshot()?.live;
        expect(live?.parts.filter(part => part.kind === 'text').map(part => part.text).join('')).toBe('新内容尾巴');
    });

    it('seeds the reducer from watch parts so the next proto-2 frame appends', () => {
        publishRemoteAssistantSession(session());
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r1',
            project_path: 'D:/tasks/a',
            display_text: '问',
            cursor: 4,
            responded: false,
            parts: [
                { kind: 'thinking', seq: 1, text: '想想', done: false },
                { kind: 'text', seq: 2, text: '半句' },
                { kind: 'progress', seq: 3, text: 'Coding Agent: running T2 - edit parser', coding: { phase: 'running', task_id: 'T2', title: 'edit parser', run_id: 'run-1' } },
            ],
        });
        expect(texts()).toEqual(['user:问', 'assistant:半句']);
        const seeded = remoteAssistantSessionSnapshot()?.live;
        expect(seeded?.parts.some(part => part.kind === 'thinking' && part.text === '想想')).toBe(true);
        expect(seeded?.parts.some(part => part.kind === 'progress' && part.text.includes('T2'))).toBe(true);
        applyRemoteStreamPayload({
            grant_id: 'g1',
            project_path: 'D:/tasks/a',
            request_id: 'r1',
            event: 'token',
            cursor: 5,
            cursor_from: 5,
            text: '续',
            pieces: ['续'],
        });
        const live = remoteAssistantSessionSnapshot()?.live;
        const body = (live?.parts || []).filter(part => part.kind === 'text').map(part => part.text).join('');
        expect(body).toBe('半句续');
        expect(live?.responded).toBe(false);
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

    function assistantBody() {
        const live = remoteAssistantSessionSnapshot()?.live;
        return (live?.parts || []).filter(part => part.kind === 'text').map(part => part.text).join('');
    }

    it('asks for a fresh watch once when a proto-2 frame opens a cursor hole', () => {
        const recover = vi.fn();
        bindRemoteStreamRecovery(recover);
        publishRemoteAssistantSession(session());
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r1',
            project_path: 'D:/tasks/a',
            cursor: 10,
            parts: [{ kind: 'text', seq: 1, text: '甲' }],
        }, undefined, 10);
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1',
            event: 'token', cursor: 12, cursor_from: 12, text: '乙',
        });
        expect(recover).toHaveBeenCalledTimes(1);
        expect(recover).toHaveBeenCalledWith('g1', 'D:/tasks/a');
        expect(assistantBody()).toBe('甲');
        expect(remoteAssistantSessionSnapshot()?.syncing).toBe(true);
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1',
            event: 'token', cursor: 13, cursor_from: 13, text: '丙',
        });
        expect(recover).toHaveBeenCalledTimes(1);
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r1',
            project_path: 'D:/tasks/a',
            cursor: 13,
            parts: [{ kind: 'text', seq: 1, text: '甲乙丙' }],
        }, undefined, 13);
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1',
            event: 'token', cursor: 14, cursor_from: 14, text: '丁',
        });
        expect(assistantBody()).toBe('甲乙丙丁');
        expect(recover).toHaveBeenCalledTimes(1);
        expect(remoteAssistantSessionSnapshot()?.syncing).toBe(false);
    });

    it('asks for a fresh watch when the resync frame has no live body', () => {
        const recover = vi.fn();
        bindRemoteStreamRecovery(recover);
        publishRemoteAssistantSession(session());
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r1',
            project_path: 'D:/tasks/a',
            cursor: 10,
            parts: [{ kind: 'text', seq: 1, text: '甲' }],
        }, undefined, 10);
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a',
            event: 'snapshot', resync: true, cursor: 40,
        });
        expect(recover).toHaveBeenCalledTimes(1);
        expect(assistantBody()).toBe('甲');
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a',
            event: 'snapshot', resync: true, cursor: 41,
        });
        expect(recover).toHaveBeenCalledTimes(1);
    });

    it('a caught-up watermark lets the next hole ask for watch again', () => {
        const recover = vi.fn();
        bindRemoteStreamRecovery(recover);
        publishRemoteAssistantSession(session());
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r1',
            project_path: 'D:/tasks/a',
            cursor: 10,
            parts: [{ kind: 'text', seq: 1, text: '甲' }],
        }, undefined, 10);
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a',
            event: 'snapshot', resync: true, cursor: 40,
        });
        expect(recover).toHaveBeenCalledTimes(1);
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', null, undefined, 40);
        expect(assistantBody()).toBe('甲');
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1',
            event: 'token', cursor: 41, cursor_from: 41, text: '续',
        });
        expect(assistantBody()).toBe('甲续');
        expect(recover).toHaveBeenCalledTimes(1);
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a',
            event: 'snapshot', resync: true, cursor: 50,
        });
        expect(recover).toHaveBeenCalledTimes(2);
    });

    it('asks once more when the watch reply is behind cursors already dropped', () => {
        const recover = vi.fn();
        bindRemoteStreamRecovery(recover);
        publishRemoteAssistantSession(session());
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r1',
            project_path: 'D:/tasks/a',
            cursor: 10,
            parts: [{ kind: 'text', seq: 1, text: '甲' }],
        }, undefined, 10);
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1',
            event: 'token', cursor: 12, cursor_from: 12, text: '乙',
        });
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1',
            event: 'token', cursor: 15, cursor_from: 15, text: '丙',
        });
        expect(recover).toHaveBeenCalledTimes(1);
        expect(assistantBody()).toBe('甲');
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', null, undefined, 12);
        expect(recover).toHaveBeenCalledTimes(2);
        expect(assistantBody()).toBe('甲');
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', null, undefined, 12);
        expect(recover).toHaveBeenCalledTimes(2);
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1',
            event: 'token', cursor: 13, cursor_from: 13, text: '续',
        });
        expect(assistantBody()).toBe('甲续');
        expect(recover).toHaveBeenCalledTimes(2);
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r1',
            project_path: 'D:/tasks/a',
            cursor: 15,
            parts: [{ kind: 'text', seq: 1, text: '甲乙丙' }],
        }, undefined, 15);
        expect(assistantBody()).toBe('甲乙丙');
        expect(recover).toHaveBeenCalledTimes(2);
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1',
            event: 'token', cursor: 16, cursor_from: 16, text: '丁',
        });
        expect(assistantBody()).toBe('甲乙丙丁');
        expect(recover).toHaveBeenCalledTimes(2);
    });

    it('does not ask again when the watch reply stays on the same watermark', () => {
        const recover = vi.fn();
        bindRemoteStreamRecovery(recover);
        publishRemoteAssistantSession(session());
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r1',
            project_path: 'D:/tasks/a',
            cursor: 10,
            parts: [{ kind: 'text', seq: 1, text: '甲' }],
        }, undefined, 10);
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a',
            event: 'snapshot', resync: true, cursor: 40,
        });
        expect(recover).toHaveBeenCalledTimes(1);
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', null, undefined, 10);
        expect(assistantBody()).toBe('甲');
        expect(recover).toHaveBeenCalledTimes(1);
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a',
            event: 'snapshot', resync: true, cursor: 41,
        });
        expect(recover).toHaveBeenCalledTimes(1);
    });

    it('asks again when a new hole opens after a short watch reply', () => {
        const recover = vi.fn();
        bindRemoteStreamRecovery(recover);
        publishRemoteAssistantSession(session());
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r1',
            project_path: 'D:/tasks/a',
            cursor: 10,
            parts: [{ kind: 'text', seq: 1, text: '甲' }],
        }, undefined, 10);
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1',
            event: 'token', cursor: 15, cursor_from: 15, text: '乙',
        });
        expect(recover).toHaveBeenCalledTimes(1);
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', null, undefined, 12);
        expect(recover).toHaveBeenCalledTimes(2);
        expect(assistantBody()).toBe('甲');
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a',
            event: 'snapshot', resync: true, cursor: 20,
        });
        expect(recover).toHaveBeenCalledTimes(3);
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r1',
            project_path: 'D:/tasks/a',
            cursor: 20,
            parts: [{ kind: 'text', seq: 1, text: '齐' }],
        }, undefined, 20);
        expect(assistantBody()).toBe('齐');
        expect(recover).toHaveBeenCalledTimes(3);
    });

    it('folds a higher snapshot of the next request and still ignores an older one', () => {
        publishRemoteAssistantSession(session());
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r1',
            project_path: 'D:/tasks/a',
            cursor: 10,
            parts: [{ kind: 'text', seq: 1, text: '旧' }],
        }, undefined, 10);
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r2',
            project_path: 'D:/tasks/a',
            cursor: 4,
            parts: [{ kind: 'text', seq: 1, text: '更旧' }],
        }, undefined, 4);
        expect(assistantBody()).toBe('旧');
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r2',
            project_path: 'D:/tasks/a',
            display_text: '下一问',
            cursor: 20,
            parts: [{ kind: 'text', seq: 1, text: '新' }],
        }, undefined, 20);
        expect(remoteAssistantSessionSnapshot()?.live?.requestId).toBe('r2');
        expect(assistantBody()).toBe('新');
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r2',
            event: 'token', cursor: 21, cursor_from: 21, text: '续',
        });
        expect(assistantBody()).toBe('新续');
    });

    const wireSnapshots = JSON.parse(readFileSync(join(dirname(fileURLToPath(import.meta.url)), '../../../../../testdata/viewer_wire_snapshots.json'), 'utf8')) as {
        watch_event: Record<string, unknown>;
        responded_event: Record<string, unknown>;
    };

    function seedMarshaledLive(event: Record<string, unknown>) {
        const live = event.live as {
            project_path?: string;
            request_id?: string;
            cursor?: number;
            text?: string;
            parts?: Array<{ kind?: string; text?: string }>;
        };
        expect(live.text).toBeUndefined();
        expect(Array.isArray(live.parts)).toBe(true);
        const projectPath = String(live.project_path || '');
        expect(projectPath.length).toBeGreaterThan(0);
        publishRemoteAssistantSession(session({ projectPath }));
        applyRemoteWatchSnapshot('g1', projectPath, live, undefined, Number(event.cursor));
        const cursor = Number(live.cursor);
        expect(cursor).toBeGreaterThan(0);
        applyRemoteStreamPayload({
            grant_id: 'g1',
            project_path: projectPath,
            request_id: live.request_id,
            event: 'token',
            cursor: cursor + 1,
            cursor_from: cursor + 1,
            text: '续',
            pieces: ['续'],
        });
        return remoteAssistantSessionSnapshot()?.live;
    }

    it('seeds from the marshaled watch live so the next proto-2 frame appends', () => {
        const live = seedMarshaledLive(wireSnapshots.watch_event);
        const body = (live?.parts || []).filter(part => part.kind === 'text').map(part => part.text).join('');
        expect(body).toBe('helloworld续');
        expect(live?.parts.some(part => part.kind === 'thinking' && part.text === '想想' && part.done)).toBe(true);
        expect(live?.parts.some(part => part.kind === 'progress' && part.text.includes('T2') && part.text.includes('edit parser'))).toBe(true);
        expect(live?.responded).toBe(false);
    });

    it('seeds from the marshaled responded snapshot and keeps the final', () => {
        const live = seedMarshaledLive(wireSnapshots.responded_event);
        const body = (live?.parts || []).filter(part => part.kind === 'text').map(part => part.text).join('');
        expect(body).toBe('终稿续');
        expect(live?.parts.some(part => part.kind === 'thinking' && part.text === '想想' && part.done)).toBe(true);
        expect(live?.parts.some(part => part.kind === 'progress' && part.text.includes('T2') && part.text.includes('run-1'))).toBe(true);
        expect(live?.responded).toBe(true);
    });

    it('keeps the streamed answer when the polled transcript is only the capped prefix', () => {
        const cap = '甲'.repeat(4000);
        const full = `${cap}乙乙乙`;
        publishRemoteAssistantSession(session());
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1',
            event: 'response', response: { text: full },
        });
        publishRemoteAssistantSession(session({
            messages: [{ role: 'assistant', content: `${cap}…`, localFilePaths: ['/tmp/报告.pdf'] }],
        }));
        const snap = remoteAssistantSessionSnapshot();
        const assistants = (snap?.messages || []).filter(message => message.role === 'assistant');
        expect(assistants).toHaveLength(1);
        expect(assistants[0].content).toBe(full);
        expect(assistants[0].localFilePaths).toEqual(['/tmp/报告.pdf']);
        expect(snap?.live?.responded).toBe(true);
    });

    it('retires the live row once the transcript has the full answer', () => {
        publishRemoteAssistantSession(session());
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1',
            event: 'response', response: { text: '终稿' },
        });
        publishRemoteAssistantSession(session({
            messages: [{ role: 'assistant', content: '终稿' }],
        }));
        const snap = remoteAssistantSessionSnapshot();
        expect(snap?.live).toBeUndefined();
        expect((snap?.messages || []).map(message => message.content)).toEqual(['终稿']);
    });

    it('replaces a snapshot tail with the final that contains it', () => {
        const cap = '甲'.repeat(4000);
        const tail = '乙'.repeat(80);
        const full = `${cap}中段${tail}`;
        publishRemoteAssistantSession(session());
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r1',
            project_path: 'D:/tasks/a',
            cursor: 8,
            parts: [{ kind: 'text', text: tail }],
        });
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1',
            event: 'response', cursor: 9, cursor_from: 9,
            terminal: { text: full },
        });
        expect(assistantBody()).toBe(full);
        publishRemoteAssistantSession(session({
            messages: [{ role: 'assistant', content: `${cap}…`, localFilePaths: ['/tmp/报告.pdf'] }],
        }));
        const assistants = (remoteAssistantSessionSnapshot()?.messages || []).filter(message => message.role === 'assistant');
        expect(assistants).toHaveLength(1);
        expect(assistants[0].content).toBe(full);
        expect(assistants[0].localFilePaths).toEqual(['/tmp/报告.pdf']);
    });

    it('keeps a snapshot tail when the trimmed history is a different slice', () => {
        const cap = '甲'.repeat(4000);
        const tail = `${'乙'.repeat(80)}结尾`;
        publishRemoteAssistantSession(session());
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r1',
            project_path: 'D:/tasks/a',
            cursor: 8,
            responded: true,
            parts: [{ kind: 'text', text: tail }],
        });
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1',
            event: 'response', cursor: 9, cursor_from: 9,
            terminal: { text: cap },
        });
        publishRemoteAssistantSession(session({
            messages: [{ role: 'assistant', content: `${cap}…` }],
        }));
        const snap = remoteAssistantSessionSnapshot();
        const assistants = (snap?.messages || []).filter(message => message.role === 'assistant');
        expect(assistants.map(message => message.content)).toEqual([`${cap}\n\n…\n\n${tail}`]);
        expect(snap?.live?.responded).toBe(true);
    });

    it('joins the opening to the ending when that row is already this round', () => {
        const cap = '甲'.repeat(4000);
        const tail = `${'乙'.repeat(80)}结尾`;
        publishRemoteAssistantSession(session({
            messages: [
                { role: 'user', content: '写报告' },
                { role: 'assistant', content: `${cap}…`, localFilePaths: ['/tmp/报告.pdf'] },
            ],
        }));
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r1',
            project_path: 'D:/tasks/a',
            display_text: '〔远控〕写报告',
            cursor: 8,
            responded: true,
            parts: [{ kind: 'text', text: tail }],
        });
        const assistants = (remoteAssistantSessionSnapshot()?.messages || []).filter(message => message.role === 'assistant');
        expect(assistants).toHaveLength(1);
        expect(assistants[0].content).toBe(`${cap}\n\n…\n\n${tail}`);
        expect(assistants[0].localFilePaths).toEqual(['/tmp/报告.pdf']);
        expect((remoteAssistantSessionSnapshot()?.messages || []).filter(message => message.role === 'user').map(message => message.content)).toEqual(['写报告']);
    });

    it('stitches an opening and an ending that share a run', () => {
        const shared = '重叠'.repeat(40);
        const cap = `${'甲'.repeat(4000 - shared.length)}${shared}`;
        const rest = `${'乙'.repeat(30)}结尾`;
        const tail = `${shared}${rest}`;
        publishRemoteAssistantSession(session({
            messages: [
                { role: 'user', content: '写报告' },
                { role: 'assistant', content: `${cap}…` },
            ],
        }));
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r1',
            project_path: 'D:/tasks/a',
            display_text: '写报告',
            cursor: 8,
            responded: true,
            parts: [{ kind: 'text', text: tail }],
        });
        const assistants = (remoteAssistantSessionSnapshot()?.messages || []).filter(message => message.role === 'assistant');
        expect(assistants).toHaveLength(1);
        expect(assistants[0].content).toBe(`${cap}${rest}`);
    });

    it('keeps a gap when the opening and the ending only share a mark', () => {
        const cap = `${'甲'.repeat(3999)}。`;
        const tail = `。${'乙'.repeat(40)}结尾`;
        publishRemoteAssistantSession(session({
            messages: [
                { role: 'user', content: '写报告' },
                { role: 'assistant', content: `${cap}…` },
            ],
        }));
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r1',
            project_path: 'D:/tasks/a',
            display_text: '写报告',
            cursor: 8,
            responded: true,
            parts: [{ kind: 'text', text: tail }],
        });
        const assistants = (remoteAssistantSessionSnapshot()?.messages || []).filter(message => message.role === 'assistant');
        expect(assistants).toHaveLength(1);
        expect(assistants[0].content).toBe(`${cap}\n\n…\n\n${tail}`);
    });

    it('leaves an older trimmed answer when the live round is the next prompt', () => {
        const cap = '甲'.repeat(4000);
        const tail = `${'乙'.repeat(40)}结尾`;
        publishRemoteAssistantSession(session({
            messages: [
                { role: 'user', content: '上一问' },
                { role: 'assistant', content: `${cap}…` },
            ],
        }));
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r2',
            project_path: 'D:/tasks/a',
            display_text: '新问题',
            cursor: 8,
            responded: true,
            parts: [{ kind: 'text', text: tail }],
        });
        expect((remoteAssistantSessionSnapshot()?.messages || []).map(message => `${message.role}:${message.content}`)).toEqual([
            'user:上一问',
            `assistant:${cap}…`,
            'user:新问题',
            `assistant:${tail}`,
        ]);
    });

    it('keeps the streamed answer after the round has been quiet', () => {
        vi.useFakeTimers();
        vi.setSystemTime(new Date('2026-01-01T00:00:00Z'));
        try {
            const cap = '甲'.repeat(4000);
            const full = `${cap}乙乙乙`;
            publishRemoteAssistantSession(session());
            applyRemoteStreamPayload({
                grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1',
                event: 'response', response: { text: full },
            });
            const history = () => session({
                messages: [{ role: 'assistant', content: `${cap}…`, localFilePaths: ['/tmp/报告.pdf'] }],
            });
            publishRemoteAssistantSession(history());
            vi.advanceTimersByTime(120_000);
            publishRemoteAssistantSession(history());
            const assistants = (remoteAssistantSessionSnapshot()?.messages || []).filter(message => message.role === 'assistant');
            expect(assistants).toHaveLength(1);
            expect(assistants[0].content).toBe(full);
            expect(assistants[0].localFilePaths).toEqual(['/tmp/报告.pdf']);
        } finally {
            vi.useRealTimers();
        }
    });

    it('retires a live opening the trimmed history already shows', () => {
        const cap = `开头${'甲'.repeat(3998)}`;
        publishRemoteAssistantSession(session());
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r1',
            project_path: 'D:/tasks/a',
            cursor: 4,
            responded: true,
            parts: [{ kind: 'text', text: '开头' }],
        });
        publishRemoteAssistantSession(session({
            messages: [{ role: 'assistant', content: `${cap}…` }],
        }));
        const snap = remoteAssistantSessionSnapshot();
        expect(snap?.live).toBeUndefined();
        expect((snap?.messages || []).map(message => message.content)).toEqual([`${cap}…`]);
    });

    it('keeps the streamed draft when the final is a shorter prefix', () => {
        const draft = `${'甲'.repeat(80)}乙乙乙`;
        publishRemoteAssistantSession(session());
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1',
            event: 'token', cursor: 1, cursor_from: 1, text: draft,
        });
        applyRemoteStreamPayload({
            grant_id: 'g1', project_path: 'D:/tasks/a', request_id: 'r1',
            event: 'response', cursor: 2, cursor_from: 2,
            terminal: { text: '甲'.repeat(40) },
        });
        expect(assistantBody()).toBe(draft);
    });
});

describe('remoteAssistantSession mirrored coding trail', () => {
    beforeEach(() => {
        publishRemoteAssistantSession(null);
        bindRemoteAssistantActions(null);
        bindRemoteStreamRecovery(null);
    });

    const startedJson = (command: string) => `Coding Agent Event: ${JSON.stringify({
        version: 1, agent: 'coding', event: 'tool_started', phase: 'running', title: '', detail: 'bash', command,
    })}`;
    const finishedJson = (command: string, outcome = 'success') => `Coding Agent Event: ${JSON.stringify({
        version: 1, agent: 'coding', event: 'tool_finished', phase: 'running', title: '', detail: 'bash', command, outcome,
    })}`;

    function remoteFrame(event: string, cursor: number, extra: Record<string, unknown> = {}, requestId = 'r1') {
        return {
            grant_id: 'g1', project_path: 'D:/tasks/a', request_id: requestId,
            event, proto: 2, cursor, cursor_from: cursor, seq: cursor,
            ...extra,
        };
    }

    function liveRow() {
        return remoteAssistantSessionSnapshot()?.messages.find(message => message.remoteLive);
    }

    it('folds the relay thinking sequence into one thought and merges started with finished', () => {
        publishRemoteAssistantSession(session());
        applyRemoteStreamPayload(remoteFrame('round_started', 1, { display_text: '〔远控〕改一下' }));
        // The desktop relay always writes state on thinking envelopes —
        // started opens the block, deltas append, done adopts the full text.
        applyRemoteStreamPayload(remoteFrame('thinking', 2, { state: 'started' }));
        applyRemoteStreamPayload(remoteFrame('thinking', 3, { state: 'delta', text: '先想想' }));
        applyRemoteStreamPayload(remoteFrame('thinking', 4, { state: 'delta', text: '，再动手' }));
        applyRemoteStreamPayload(remoteFrame('thinking', 5, { state: 'done', text: '先想想，再动手' }));
        applyRemoteStreamPayload(remoteFrame('progress', 6, { text: startedJson('ls -la') }));
        applyRemoteStreamPayload(remoteFrame('progress', 7, { text: finishedJson('ls -la') }));
        applyRemoteStreamPayload(remoteFrame('token', 8, { text: '改好了。' }));

        const row = liveRow();
        expect(row?.codingTimeline?.map(item => item.kind)).toEqual(['thinking', 'progress']);
        expect(row?.codingTimeline?.[0].content).toBe('先想想，再动手');
        expect(row?.codingTimeline?.[1].content).toContain('"event":"tool_finished"');
        expect(row?.codingTimeline?.[1].content).toContain('ls -la');
    });

    it('keeps styled session tool lines out of the coding timeline', () => {
        publishRemoteAssistantSession(session());
        applyRemoteStreamPayload(remoteFrame('round_started', 1));
        applyRemoteStreamPayload(remoteFrame('progress', 2, { text: '工具 · 执行命令 (bash)\nls -la' }));
        const row = liveRow();
        expect(row?.codingTimeline).toBeUndefined();
        // The styled line still folds into the bubble's toolCalls timeline.
        expect(row?.toolCalls?.[0].detail).toBe('ls -la');
    });

    it('a watch snapshot seeds the interleaved trail for a mid-turn join', () => {
        publishRemoteAssistantSession(session());
        applyRemoteWatchSnapshot('g1', 'D:/tasks/a', {
            request_id: 'r1',
            project_path: 'D:/tasks/a',
            responded: false,
            parts: [
                { kind: 'thinking', seq: 1, text: '先看目录。', done: true },
                { kind: 'progress', seq: 2, text: startedJson('ls -la') },
            ],
        }, undefined, 42);
        const row = liveRow();
        expect(row?.codingTimeline?.map(item => item.kind)).toEqual(['thinking', 'progress']);
        expect(row?.codingTimeline?.[0].content).toBe('先看目录。');
    });
});
