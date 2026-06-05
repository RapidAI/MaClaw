import { act, renderHook } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { BUFFER_QUEUE_STORAGE_KEY, useBufferQueue } from '../useBufferQueue';
import { forgetAIAssistantSessionRounds } from '../useAIAssistant';

afterEach(() => {
    localStorage.removeItem(BUFFER_QUEUE_STORAGE_KEY);
});

describe('useBufferQueue', () => {
    it('keeps visible queue scoped to the active session', () => {
        localStorage.setItem(BUFFER_QUEUE_STORAGE_KEY, JSON.stringify([
            { id: 'local-one', text: 'local queued', attachments: [], createdAt: 1, sessionKey: 'desktop-user' },
            { id: 'project-one', text: 'project queued', attachments: [], createdAt: 2, sessionKey: 'desktop-user:D:/tasks/fork-a' },
        ]));

        const { result } = renderHook(() => useBufferQueue('desktop-user:D:/tasks/fork-a'));

        expect(result.current.queue.map(entry => entry.id)).toEqual(['project-one']);
    });

    it('reorders only the active session entries while preserving other sessions', () => {
        localStorage.setItem(BUFFER_QUEUE_STORAGE_KEY, JSON.stringify([
            { id: 'local-one', text: 'local queued', attachments: [], createdAt: 1, sessionKey: 'desktop-user' },
            { id: 'project-one', text: 'first project', attachments: [], createdAt: 2, sessionKey: 'desktop-user:D:/tasks/fork-a' },
            { id: 'local-two', text: 'second local', attachments: [], createdAt: 3, sessionKey: 'desktop-user' },
            { id: 'project-two', text: 'second project', attachments: [], createdAt: 4, sessionKey: 'desktop-user:D:/tasks/fork-a' },
            { id: 'project-three', text: 'third project', attachments: [], createdAt: 5, sessionKey: 'desktop-user:D:/tasks/fork-a' },
        ]));

        const { result } = renderHook(() => useBufferQueue('desktop-user:D:/tasks/fork-a'));

        act(() => result.current.reorderEntry(0, 2));

        expect(result.current.queue.map(entry => entry.id)).toEqual(['project-two', 'project-three', 'project-one']);
        expect(JSON.parse(localStorage.getItem(BUFFER_QUEUE_STORAGE_KEY) || '[]').map((entry: any) => entry.id)).toEqual([
            'local-one',
            'local-two',
            'project-two',
            'project-three',
            'project-one',
        ]);
    });

    it('clears only the active session queue', () => {
        localStorage.setItem(BUFFER_QUEUE_STORAGE_KEY, JSON.stringify([
            { id: 'local-one', text: 'local queued', attachments: [], createdAt: 1, sessionKey: 'desktop-user' },
            { id: 'project-one', text: 'project queued', attachments: [], createdAt: 2, sessionKey: 'desktop-user:D:/tasks/fork-a' },
        ]));

        const { result } = renderHook(() => useBufferQueue('desktop-user:D:/tasks/fork-a'));

        act(() => result.current.clearQueue());

        expect(result.current.queue).toEqual([]);
        expect(JSON.parse(localStorage.getItem(BUFFER_QUEUE_STORAGE_KEY) || '[]').map((entry: any) => entry.id)).toEqual(['local-one']);
    });

    it('drops queued input when a project session is forgotten', () => {
        localStorage.setItem(BUFFER_QUEUE_STORAGE_KEY, JSON.stringify([
            { id: 'local-one', text: 'local queued', attachments: [], createdAt: 1, sessionKey: 'desktop-user' },
            { id: 'project-one', text: 'project queued', attachments: [], createdAt: 2, sessionKey: 'desktop-user:D:/tasks/fork-a' },
        ]));

        const { result } = renderHook(() => useBufferQueue('desktop-user:D:/tasks/fork-a'));
        expect(result.current.queue.map(entry => entry.id)).toEqual(['project-one']);

        act(() => forgetAIAssistantSessionRounds('desktop-user:D:/tasks/fork-a'));

        expect(result.current.queue).toEqual([]);
        expect(JSON.parse(localStorage.getItem(BUFFER_QUEUE_STORAGE_KEY) || '[]').map((entry: any) => entry.id)).toEqual(['local-one']);
    });
});
