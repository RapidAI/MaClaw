import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import * as fc from 'fast-check';

let mockSendResponse: any = { text: 'ok', error: '', fields: null, actions: null };
let mockSendError: Error | null = null;
const runtimeHandlers = new Map<string, (payload?: string) => void>();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    SendAIAssistantMessage: vi.fn(async (_text: string) => {
        if (mockSendError) throw mockSendError;
        return mockSendResponse;
    }),
    ClearAIAssistantHistory: vi.fn(async () => {}),
    IsAIAssistantReady: vi.fn(async () => true),
    GetAIAssistantInitStatus: vi.fn(async () => 'ready'),
    CancelAIAssistantSession: vi.fn(async () => {}),
    FetchNews: vi.fn(async () => []),
    SelectAIAssistantFile: vi.fn(async () => ''),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn((event: string, handler: (text?: string) => void) => {
        runtimeHandlers.set(event, handler);
    }),
    EventsOff: vi.fn((event: string) => {
        runtimeHandlers.delete(event);
    }),
}));

import { useAIAssistant, buildOutgoingMessage, AI_ASSISTANT_HISTORY_STORAGE_KEY, isPinnedNewsMessage, type ChatAction } from '../useAIAssistant';
import { ClearAIAssistantHistory, SendAIAssistantMessage, CancelAIAssistantSession, FetchNews, SelectAIAssistantFile, GetAIAssistantInitStatus, IsAIAssistantReady } from '../../../../wailsjs/go/main/App';

function renderAssistantHook() {
    return renderHook(() => useAIAssistant());
}

function messageContents(messages: Array<{ content: string }>) {
    return messages.map(message => message.content);
}

function emitRuntimeEvent(event: string, payload = '') {
    const handler = runtimeHandlers.get(event);
    expect(handler).toBeDefined();
    handler?.(payload);
}

function assistantMessages(messages: Array<{ role: string; content: string }>) {
    return messages.filter(message => message.role === 'assistant');
}

function deferred<T>() {
    let resolve!: (value: T) => void;
    let reject!: (reason?: unknown) => void;
    const promise = new Promise<T>((res, rej) => {
        resolve = res;
        reject = rej;
    });
    return { promise, resolve, reject };
}

describe('useAIAssistant property tests', () => {
    beforeEach(() => {
        mockSendResponse = { text: 'ok', error: '', fields: null, actions: null };
        mockSendError = null;
        runtimeHandlers.clear();
        localStorage.clear();
    });

    afterEach(() => {
        localStorage.clear();
        runtimeHandlers.clear();
        vi.clearAllMocks();
    });

    it('Property 2: sendMessage grows message list with user message', async () => {
        await fc.assert(
            fc.asyncProperty(
                fc.string({ minLength: 1, maxLength: 80 }).filter(s => s.trim().length > 0),
                async (text) => {
                    const { result, unmount } = renderAssistantHook();
                    const before = result.current.messages.length;

                    await act(async () => {
                        await result.current.sendMessage(text);
                    });

                    const after = result.current.messages;
                    expect(after.length).toBeGreaterThan(before);

                    const userMsg = after.find(m => m.role === 'user' && m.content === buildOutgoingMessage(text, ''));
                    expect(userMsg).toBeDefined();

                    unmount();
                },
            ),
            { numRuns: 100 },
        );
    });

    it('browseFile normalizes repeated selections and sendMessage uses the latest selected path', async () => {
        (SelectAIAssistantFile as any)
            .mockResolvedValueOnce('  /tmp/example.png  ')
            .mockResolvedValueOnce('/tmp/example.png');

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.browseFile();
        });
        expect(result.current.selectedFilePath).toBe('/tmp/example.png');

        await act(async () => {
            await result.current.browseFile();
        });
        expect(result.current.selectedFilePath).toBe('/tmp/example.png');

        await act(async () => {
            await result.current.sendMessage('inspect this');
        });

        expect(SendAIAssistantMessage).toHaveBeenLastCalledWith(buildOutgoingMessage('inspect this', '/tmp/example.png'));
        expect(result.current.selectedFilePath).toBe('');
    });

    it('init progress ready stops follow-up polling', async () => {
        vi.useFakeTimers();
        const readyDeferred = deferred<boolean>();
        const statusDeferred = deferred<string>();
        (IsAIAssistantReady as any).mockImplementationOnce(() => readyDeferred.promise);
        (GetAIAssistantInitStatus as any).mockImplementationOnce(() => statusDeferred.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            emitRuntimeEvent('ai-assistant-init-progress', 'ready');
            readyDeferred.resolve(false);
            statusDeferred.resolve('loading');
            await Promise.resolve();
            await Promise.resolve();
        });

        expect(result.current.ready).toBe(true);
        expect(result.current.initStatus).toBe('ready');

        await act(async () => {
            vi.advanceTimersByTime(2000);
        });

        expect(IsAIAssistantReady).toHaveBeenCalledTimes(1);
        expect(GetAIAssistantInitStatus).toHaveBeenCalledTimes(1);
        vi.useRealTimers();
    });

    it('Property 3: messages remain stable across rerender', async () => {
        await fc.assert(
            fc.asyncProperty(
                fc.array(
                    fc.string({ minLength: 1, maxLength: 40 }).filter(s => s.trim().length > 0),
                    { minLength: 1, maxLength: 5 },
                ),
                async (texts) => {
                    const { result, rerender, unmount } = renderAssistantHook();

                    for (const text of texts) {
                        await act(async () => {
                            await result.current.sendMessage(text);
                        });
                    }

                    const messagesBeforeClose = result.current.messages.map(m => ({
                        id: m.id,
                        content: m.content,
                        role: m.role,
                    }));

                    rerender();

                    const messagesAfterReopen = result.current.messages;
                    expect(messagesAfterReopen.length).toBe(messagesBeforeClose.length);
                    for (let i = 0; i < messagesBeforeClose.length; i++) {
                        expect(messagesAfterReopen[i].id).toBe(messagesBeforeClose[i].id);
                        expect(messagesAfterReopen[i].content).toBe(messagesBeforeClose[i].content);
                        expect(messagesAfterReopen[i].role).toBe(messagesBeforeClose[i].role);
                    }

                    unmount();
                },
            ),
            { numRuns: 100 },
        );
    });

    it('Property 3b: persisted history is restored on remount and reset by clearHistory', async () => {
        const persistedMessages = [
            { id: 'u-1', role: 'user', content: 'remember this', timestamp: 1 },
            { id: 'a-1', role: 'assistant', content: 'restored reply', timestamp: 2 },
            { id: 'p-1', role: 'progress', content: 'skip me', timestamp: 3 },
            { id: 's-1', role: 'system', content: 'skip me too', timestamp: 4 },
        ];
        localStorage.setItem(AI_ASSISTANT_HISTORY_STORAGE_KEY, JSON.stringify(persistedMessages));

        const { result, unmount } = renderAssistantHook();
        expect(result.current.messages.map(m => m.id)).toEqual(['u-1', 'a-1']);

        await act(async () => {
            await result.current.clearHistory();
        });

        expect(result.current.messages).toEqual([]);
        await waitFor(() => {
            expect(localStorage.getItem(AI_ASSISTANT_HISTORY_STORAGE_KEY)).toBeNull();
        });

        expect(ClearAIAssistantHistory).toHaveBeenCalledTimes(1);

        unmount();

        const { result: remounted } = renderAssistantHook();
        expect(remounted.current.messages).toEqual([]);
    });

    it('Property 3c: clearHistory prevents old persisted turns from polluting the next send', async () => {
        localStorage.setItem(AI_ASSISTANT_HISTORY_STORAGE_KEY, JSON.stringify([
            { id: 'old-user', role: 'user', content: 'old question', timestamp: 1 },
            { id: 'old-assistant', role: 'assistant', content: 'old answer', timestamp: 2 },
        ]));

        const { result } = renderAssistantHook();
        expect(result.current.messages.some(m => m.content === 'old question')).toBe(true);

        await act(async () => {
            await result.current.clearHistory();
        });

        await act(async () => {
            await result.current.sendMessage('fresh start');
        });

        const contents = messageContents(result.current.messages);
        expect(contents).toContain('fresh start');
        expect(contents).not.toContain('old question');
        expect(contents).not.toContain('old answer');
    });

    it('rerender-only message changes do not rewrite persisted history', async () => {
        const setItemSpy = vi.spyOn(Storage.prototype, 'setItem');
        const removeItemSpy = vi.spyOn(Storage.prototype, 'removeItem');
        const { result, rerender } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('persist once');
        });

        await waitFor(() => {
            expect(setItemSpy).toHaveBeenCalled();
        });
        const writesAfterSend = setItemSpy.mock.calls.length;
        const removesAfterSend = removeItemSpy.mock.calls.length;

        rerender();

        await act(async () => {
            await Promise.resolve();
        });

        expect(setItemSpy.mock.calls.length).toBe(writesAfterSend);
        expect(removeItemSpy.mock.calls.length).toBe(removesAfterSend);
    });

    it('keeps exactly one assistant placeholder for the active round', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('hello');
        });

        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round');
            emitRuntimeEvent('ai-assistant-new-round');
        });

        expect(assistantMessages(result.current.messages).length).toBe(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe('');

        await act(async () => {
            pending.resolve({ text: 'done', error: '', fields: null, actions: null });
            await pending.promise;
        });

        expect(assistantMessages(result.current.messages).length).toBe(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe('done');
    });

    it('routes streamed tokens to the latest matching assistant message', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('stream please');
        });

        await act(async () => {
            emitRuntimeEvent('ai-assistant-token', 'hello');
            emitRuntimeEvent('ai-assistant-new-round');
            emitRuntimeEvent('ai-assistant-token', ' world');
            emitRuntimeEvent('ai-assistant-stream-done');
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe('hello world');
        expect(result.current.streaming).toBe(false);
        expect(result.current.sending).toBe(true);

        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null });
            await pending.promise;
        });

        expect(assistantMessages(result.current.messages)[0].content).toBe('hello world');
        expect(result.current.sending).toBe(false);
        expect(result.current.streaming).toBe(false);
    });

    it('stream-done clears visual busy before the request promise resolves', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('hello');
        });

        expect(result.current.sending).toBe(true);
        expect(result.current.streaming).toBe(false);
        expect(result.current.visualBusy).toBe(false);

        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round');
            emitRuntimeEvent('ai-assistant-token', 'hello');
        });

        expect(result.current.streaming).toBe(true);
        expect(result.current.visualBusy).toBe(true);

        await act(async () => {
            emitRuntimeEvent('ai-assistant-stream-done');
        });

        expect(result.current.sending).toBe(true);
        expect(result.current.streaming).toBe(false);
        expect(result.current.visualBusy).toBe(false);

        await act(async () => {
            pending.resolve({ text: 'hello', error: '', fields: null, actions: null });
            await pending.promise;
        });

        expect(result.current.sending).toBe(false);
        expect(result.current.visualBusy).toBe(false);
    });

    it('finalizes the active tail message without changing streamed content', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('tail finalize');
        });

        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round');
            emitRuntimeEvent('ai-assistant-token', 'streamed');
            emitRuntimeEvent('ai-assistant-stream-done');
        });

        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null });
            await pending.promise;
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe('streamed');
    });

    it('keeps streamed tokens on the active tail message across many updates', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('stream a lot');
        });

        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round');
            for (let i = 0; i < 50; i++) {
                emitRuntimeEvent('ai-assistant-token', String(i % 10));
            }
            emitRuntimeEvent('ai-assistant-stream-done');
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe('01234567890123456789012345678901234567890123456789');

        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null });
            await pending.promise;
        });
    });

    it('repeated stream transitions do not change sending state or duplicate placeholders', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('hello');
        });

        expect(result.current.sending).toBe(true);
        expect(result.current.streaming).toBe(false);

        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round');
            emitRuntimeEvent('ai-assistant-new-round');
            emitRuntimeEvent('ai-assistant-stream-done');
            emitRuntimeEvent('ai-assistant-stream-done');
        });

        expect(result.current.sending).toBe(true);
        expect(result.current.streaming).toBe(false);
        expect(assistantMessages(result.current.messages)).toHaveLength(1);

        await act(async () => {
            pending.resolve({ text: 'done', error: '', fields: null, actions: null });
            await pending.promise;
        });
    });

    it('cancelSession removes only the active round placeholder and ignores stale completion', async () => {
        const first = deferred<{ text: string; error: string; fields: null; actions: null }>();
        const second = deferred<{ text: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any)
            .mockImplementationOnce(() => first.promise)
            .mockImplementationOnce(() => second.promise);
        (CancelAIAssistantSession as any).mockResolvedValueOnce('retry first request');

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('first request');
        });

        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round');
            emitRuntimeEvent('ai-assistant-token', 'partial');
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe('partial');

        let cancelResult;
        await act(async () => {
            cancelResult = await result.current.cancelSession();
        });

        expect(cancelResult).toEqual({ canceledText: 'retry first request' });
        expect(assistantMessages(result.current.messages)).toHaveLength(0);
        expect(result.current.sending).toBe(false);

        await act(async () => {
            void result.current.sendMessage('second request');
        });

        await act(async () => {
            second.resolve({ text: 'fresh reply', error: '', fields: null, actions: null });
            await second.promise;
        });

        expect(messageContents(result.current.messages)).toContain('first request');
        expect(messageContents(result.current.messages)).toContain('second request');
        expect(messageContents(result.current.messages)).toContain('fresh reply');
        expect(messageContents(result.current.messages)).not.toContain('partial');

        await act(async () => {
            first.resolve({ text: 'stale reply', error: '', fields: null, actions: null });
            await first.promise;
        });

        expect(messageContents(result.current.messages)).not.toContain('stale reply');
        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe('fresh reply');
    });

    it('cancelSession skips backend cancel when already idle', async () => {
        const { result } = renderAssistantHook();

        let cancelResult;
        await act(async () => {
            cancelResult = await result.current.cancelSession();
        });

        expect(cancelResult).toEqual({ canceledText: '' });
        expect(CancelAIAssistantSession).not.toHaveBeenCalled();
        expect(result.current.sending).toBe(false);
        expect(result.current.messages).toEqual([]);
    });

    it('normalizes action styles from assistant responses', async () => {
        const responseActions = [
            { label: 'Safe', command: 'safe-cmd', style: 'default' },
            { label: 'Delete', command: 'danger-cmd', style: 'danger' },
            { label: 'Fallback', command: 'fallback-cmd', style: 'warning' },
        ];
        mockSendResponse = { text: 'ok', error: '', fields: null, actions: responseActions };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('normalize actions');
        });

        const assistantMsg = result.current.messages.find(m => m.role === 'assistant');
        expect(assistantMsg?.actions).toEqual([
            { label: 'Safe', command: 'safe-cmd', style: 'default' },
            { label: 'Delete', command: 'danger-cmd', style: 'danger' },
            { label: 'Fallback', command: 'fallback-cmd', style: 'default' },
        ] satisfies ChatAction[]);
    });

    it('maps fetched news into typed pinned messages', async () => {
        (FetchNews as any).mockResolvedValueOnce([
            { id: 7, category: 'tip', title: 'Read this', content: 'Structured news body' },
        ]);

        const { result } = renderAssistantHook();

        await waitFor(() => {
            const newsMessages = result.current.messages.filter(isPinnedNewsMessage);
            expect(newsMessages).toHaveLength(1);
            expect(newsMessages[0].news).toEqual({
                articleId: '7',
                category: 'tip',
                title: 'Read this',
                body: 'Structured news body',
                icon: '💡',
            });
            expect(newsMessages[0].content).toBe('Structured news body');
        });
    });

    it('refreshNews skips redundant news replacements for identical payloads', async () => {
        (FetchNews as any)
            .mockResolvedValueOnce([{ id: 7, category: 'tip', title: 'Read this', content: 'Structured news body' }])
            .mockResolvedValueOnce([{ id: 7, category: 'tip', title: 'Read this', content: 'Structured news body' }]);

        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(result.current.messages.filter(isPinnedNewsMessage)).toHaveLength(1);
        });

        const previousMessages = result.current.messages;
        const previousScrollSeq = result.current.scrollToTopSeq;

        await act(async () => {
            result.current.refreshNews();
            await Promise.resolve();
        });

        expect(result.current.messages).toBe(previousMessages);
        expect(result.current.scrollToTopSeq).toBe(previousScrollSeq);
    });

    it('Property 9: progress events appear as progress messages', async () => {
        await fc.assert(
            fc.asyncProperty(
                fc.string({ minLength: 1, maxLength: 80 }),
                async (progressText) => {
                    const { result, unmount } = renderAssistantHook();

                    await act(async () => {
                        emitRuntimeEvent('ai-assistant-progress', progressText);
                    });

                    const progressMsgs = result.current.progressMessages;
                    expect(progressMsgs.length).toBeGreaterThanOrEqual(1);
                    expect(progressMsgs.find(m => m.content === progressText)).toBeDefined();

                    unmount();
                },
            ),
            { numRuns: 100 },
        );
    });

    it('deduplicates consecutive identical progress events', async () => {
        const { result } = renderAssistantHook();

        await act(async () => {
            emitRuntimeEvent('ai-assistant-progress', 'same progress');
            emitRuntimeEvent('ai-assistant-progress', 'same progress');
            emitRuntimeEvent('ai-assistant-progress', 'same progress');
        });

        expect(result.current.progressMessages).toHaveLength(1);
        expect(result.current.progressMessages[0].content).toBe('same progress');
    });

    it('clearHistory resets progress dedupe state', async () => {
        const { result } = renderAssistantHook();

        await act(async () => {
            emitRuntimeEvent('ai-assistant-progress', 'same progress');
            emitRuntimeEvent('ai-assistant-progress', 'same progress');
        });
        expect(result.current.progressMessages).toHaveLength(1);

        await act(async () => {
            await result.current.clearHistory();
        });
        expect(result.current.progressMessages).toEqual([]);

        await act(async () => {
            emitRuntimeEvent('ai-assistant-progress', 'same progress');
        });
        expect(result.current.progressMessages).toHaveLength(1);
        expect(result.current.progressMessages[0].content).toBe('same progress');
    });

    it('Property 10: assistant reply is populated after progress messages', async () => {
        await fc.assert(
            fc.asyncProperty(
                fc.string({ minLength: 1, maxLength: 40 }).filter(s => s.trim().length > 0),
                fc.array(fc.string({ minLength: 1, maxLength: 40 }), { minLength: 1, maxLength: 5 }),
                async (userText, progressTexts) => {
                    const { result, unmount } = renderAssistantHook();

                    (SendAIAssistantMessage as any).mockImplementationOnce(async () => {
                        await act(async () => {
                            for (const pt of progressTexts) {
                                emitRuntimeEvent('ai-assistant-progress', pt);
                            }
                        });
                        return { text: 'done', error: '', fields: null, actions: null };
                    });

                    await act(async () => {
                        await result.current.sendMessage(userText);
                    });

                    const msgs = result.current.messages;
                    const assistantMsg = msgs.find(m => m.role === 'assistant');
                    const progressMsgs = result.current.progressMessages;

                    expect(assistantMsg).toBeDefined();
                    expect(progressMsgs.length).toBeGreaterThan(0);
                    expect(assistantMsg!.content).toBe('done');

                    for (const pt of progressTexts) {
                        expect(progressMsgs.find(m => m.content === pt)).toBeDefined();
                    }

                    unmount();
                },
            ),
            { numRuns: 100 },
        );
    }, 10000);
});
