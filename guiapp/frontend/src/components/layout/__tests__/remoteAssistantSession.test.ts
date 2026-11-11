// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
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
