import { act, renderHook } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { _resetIdCounter, BUFFER_QUEUE_STORAGE_KEY, useBufferQueue } from '../useBufferQueue';
import { forgetAIAssistantSessionRounds } from '../useAIAssistant';

afterEach(() => {
    localStorage.removeItem(BUFFER_QUEUE_STORAGE_KEY);
});

describe('useBufferQueue', () => {
    it('keeps queue IDs unique across renderer counter resets at the same millisecond', () => {
        vi.spyOn(Date, 'now').mockReturnValue(123456789);
        const firstHook = renderHook(() => useBufferQueue());
        let firstId = '';
        act(() => {
            firstId = firstHook.result.current.addEntry('first renderer', [])?.id || '';
        });
        firstHook.unmount();
        localStorage.removeItem(BUFFER_QUEUE_STORAGE_KEY);

        _resetIdCounter();
        const secondHook = renderHook(() => useBufferQueue());
        let secondId = '';
        act(() => {
            secondId = secondHook.result.current.addEntry('second renderer', [])?.id || '';
        });

        expect(firstId).toMatch(/^buf-123456789-0-/);
        expect(secondId).toMatch(/^buf-123456789-0-/);
        expect(secondId).not.toBe(firstId);
        vi.restoreAllMocks();
    });

    it('persists enqueue and accepted removal synchronously', () => {
        const { result } = renderHook(() => useBufferQueue());
        let entryId = '';

        act(() => {
            const entry = result.current.addEntry('durable before rerender', [], { autoDrain: true });
            entryId = entry?.id || '';
            expect(JSON.parse(localStorage.getItem(BUFFER_QUEUE_STORAGE_KEY) || '[]')).toEqual([
                expect.objectContaining({ id: entryId, text: 'durable before rerender', sessionKey: 'desktop-user' }),
            ]);
        });

        act(() => {
            result.current.removeEntry(entryId);
            expect(JSON.parse(localStorage.getItem(BUFFER_QUEUE_STORAGE_KEY) || '[]')).toEqual([]);
        });
    });

	it('reports queue ownership when removing an entry', () => {
		const { result } = renderHook(() => useBufferQueue());
		let entryId = '';
		act(() => { entryId = result.current.addEntry('owned once', [])?.id || ''; });

		let first = false;
		let second = true;
		act(() => {
			first = result.current.removeEntry(entryId);
			second = result.current.removeEntry(entryId);
		});
		expect(first).toBe(true);
		expect(second).toBe(false);
	});

    it('restores an interrupted steer as a safe next-turn send', () => {
        localStorage.setItem(BUFFER_QUEUE_STORAGE_KEY, JSON.stringify([
            { id: 'stale-steer', text: 'do not attach this to a future turn', attachments: [], createdAt: 1, autoDrain: true, steerWhenBusy: true },
        ]));

        const { result } = renderHook(() => useBufferQueue());

        expect(result.current.queue[0]).toMatchObject({
            id: 'stale-steer',
            autoDrain: true,
            steerWhenBusy: false,
        });
    });

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
