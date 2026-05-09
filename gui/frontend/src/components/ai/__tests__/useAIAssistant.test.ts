import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import * as fc from 'fast-check';
import { main } from '../../../../wailsjs/go/models';

let mockSendResponse: any = { text: 'ok', error: '', fields: null, actions: null, request_id: 'req-default' };
let mockSendError: Error | null = null;
const runtimeHandlers = new Map<string, (payload?: unknown) => void>();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    SendAIAssistantMessage: vi.fn(async (req: string | { text?: string; request_id?: string }) => {
        if (mockSendError) throw mockSendError;
        if (mockSendResponse && typeof req === 'object' && req?.request_id && !mockSendResponse.request_id) {
            return { ...mockSendResponse, request_id: req.request_id };
        }
        return mockSendResponse;
    }),
    ClearAIAssistantHistory: vi.fn(async () => {}),
    IsAIAssistantReady: vi.fn(async () => true),
    GetAIAssistantInitStatus: vi.fn(async () => 'ready'),
    GetTrialReflectEnabled: vi.fn(async () => false),
    GetAIAssistantTrace: vi.fn(async () => ({ summary: 'trace ok', event_count: 2, evidence_count: 1, events: [], evidence: [] })),
    LoadConfig: vi.fn(async () => ({ show_ai_trace_entry: false })),
    CancelAIAssistantSession: vi.fn(async () => {}),
    CancelAIAssistantTask: vi.fn(async () => {}),
    StartAIAssistantBackgroundTask: vi.fn(async () => ({ accepted: true, session_id: 'session-test' })),
    ListRemoteSessions: vi.fn(async () => []),
    FetchNews: vi.fn(async () => []),
    SelectAIAssistantFiles: vi.fn(async () => []),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn((event: string, handler: (payload?: unknown) => void) => {
        runtimeHandlers.set(event, handler);
    }),
    EventsOff: vi.fn((event: string) => {
        runtimeHandlers.delete(event);
    }),
}));

import { useAIAssistant, buildOutgoingMessage, buildOutgoingMessageMulti, AI_ASSISTANT_HISTORY_STORAGE_KEY, AI_ASSISTANT_PROMPT_HISTORY_STORAGE_KEY, CANCELED_BY_USER_LINE, isPinnedNewsMessage, type ChatAction } from '../useAIAssistant';
import { ClearAIAssistantHistory, SendAIAssistantMessage, CancelAIAssistantSession, CancelAIAssistantTask, StartAIAssistantBackgroundTask, FetchNews, SelectAIAssistantFiles, GetAIAssistantInitStatus, GetTrialReflectEnabled, GetAIAssistantTrace, IsAIAssistantReady, LoadConfig, ListRemoteSessions } from '../../../../wailsjs/go/main/App';

function renderAssistantHook() {
    return renderHook(() => useAIAssistant());
}

function parseSentRequest(index = 0) {
    const calls = (SendAIAssistantMessage as any).mock.calls;
    const request = calls[index]?.[0];
    expect(request).toBeDefined();
    return request as { text: string; request_id?: string };
}

function requestEvent(indexOrText: number | string = 0, text = '') {
    const index = typeof indexOrText === 'number' ? indexOrText : 0;
    const eventText = typeof indexOrText === 'string' ? indexOrText : text;
    const req = parseSentRequest(index);
    return { request_id: req.request_id || '', text: eventText };
}

function otherRequestEvent(text = '') {
    return { request_id: 'other-request', text };
}

function messageContents(messages: Array<{ content: string }>) {
    return messages.map(message => message.content);
}

function emitRuntimeEvent(event: string, payload: unknown = '') {
    const handler = runtimeHandlers.get(event);
    expect(handler).toBeDefined();
    handler?.(payload);
}

function resetAppMocks() {
    (SendAIAssistantMessage as any).mockReset();
    (SendAIAssistantMessage as any).mockImplementation(async (req: string | { text?: string; request_id?: string }) => {
        if (mockSendError) throw mockSendError;
        if (mockSendResponse && typeof req === 'object' && req?.request_id && !mockSendResponse.request_id) {
            return { ...mockSendResponse, request_id: req.request_id };
        }
        return mockSendResponse;
    });
    (ClearAIAssistantHistory as any).mockReset();
    (ClearAIAssistantHistory as any).mockImplementation(async () => {});
    (IsAIAssistantReady as any).mockReset();
    (IsAIAssistantReady as any).mockImplementation(async () => true);
    (GetAIAssistantInitStatus as any).mockReset();
    (GetAIAssistantInitStatus as any).mockImplementation(async () => 'ready');
    (GetTrialReflectEnabled as any).mockReset();
    (GetTrialReflectEnabled as any).mockImplementation(async () => false);
    (GetAIAssistantTrace as any).mockReset();
    (GetAIAssistantTrace as any).mockImplementation(async () => ({ summary: 'trace ok', event_count: 2, evidence_count: 1, events: [], evidence: [] }));
    (LoadConfig as any).mockReset();
    (LoadConfig as any).mockImplementation(async () => new main.AppConfig({ show_ai_trace_entry: false, trial_reflect_enabled: false }));
    (CancelAIAssistantSession as any).mockReset();
    (CancelAIAssistantSession as any).mockImplementation(async () => {});
    (CancelAIAssistantTask as any).mockReset();
    (CancelAIAssistantTask as any).mockImplementation(async () => {});
    (StartAIAssistantBackgroundTask as any).mockReset();
    (StartAIAssistantBackgroundTask as any).mockImplementation(async () => ({ accepted: true, session_id: 'session-test' }));
    (ListRemoteSessions as any).mockReset();
    (ListRemoteSessions as any).mockImplementation(async () => []);
    (FetchNews as any).mockReset();
    (FetchNews as any).mockImplementation(async () => []);
    (SelectAIAssistantFiles as any).mockReset();
    (SelectAIAssistantFiles as any).mockImplementation(async () => []);
}

function assistantMessages(messages: Array<{ role: string; content: string; fields?: unknown; actions?: unknown; confirmation?: { status?: string } }>) {
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
        mockSendResponse = { text: 'ok', error: '', fields: null, actions: null, request_id: 'req-default' };
        mockSendError = null;
        runtimeHandlers.clear();
        resetAppMocks();
        localStorage.clear();
    });

    afterEach(() => {
        localStorage.clear();
        runtimeHandlers.clear();
        resetAppMocks();
    });

    it('preserves llm_token_usage when AppConfig is reconstructed on the frontend', () => {
        const cfg = new main.AppConfig({
            show_ai_trace_entry: false,
            trial_reflect_enabled: false,
            llm_token_usage: {
                '智谱龙虾': { input_tokens: 12, output_tokens: 34, total_tokens: 46 },
            },
        });

        const reconstructed = new main.AppConfig({
            ...cfg,
            trial_reflect_enabled: true,
        });

        expect(reconstructed.llm_token_usage).toEqual({
            '智谱龙虾': { input_tokens: 12, output_tokens: 34, total_tokens: 46 },
        });
    });

    it('loads trial-reflect mode from config on mount', async () => {
        (GetTrialReflectEnabled as any).mockResolvedValueOnce(true);
        (LoadConfig as any).mockResolvedValueOnce(new main.AppConfig({ show_ai_trace_entry: false, trial_reflect_enabled: true }));

        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(GetTrialReflectEnabled).toHaveBeenCalledTimes(1);
            expect(result.current.trialReflectEnabled).toBe(true);
        });
    });

    it('updates trial-reflect badge state after config-changed events', async () => {
        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(result.current.trialReflectEnabled).toBe(false);
        });

        act(() => {
            emitRuntimeEvent('config-changed', new main.AppConfig({ trial_reflect_enabled: true, show_ai_trace_entry: false }));
        });

        await waitFor(() => {
            expect(result.current.trialReflectEnabled).toBe(true);
        });
    });

    it('background launch stores visible session, job, and run identifiers in a system message', async () => {
        (StartAIAssistantBackgroundTask as any).mockResolvedValueOnce({
            accepted: true,
            session_id: 'session-trace',
            job_id: 'job-trace',
            run_id: 'run-trace',
        });

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessageInBackground('trace this task');
        });

        expect(StartAIAssistantBackgroundTask).toHaveBeenCalledWith({
            text: buildOutgoingMessage('trace this task', ''),
            force_background: true,
        });
        expect(result.current.messages.map(m => ({ role: m.role, content: m.content }))).toEqual([
            { role: 'user', content: 'trace this task' },
            {
                role: 'system',
                content: [
                    '已转到后台运行。',
                    '任务会显示在“任务管理”里的后台列表。',
                    'session_id: session-trace',
                    'job_id: job-trace',
                    'run_id: run-trace',
                ].join('\n'),
            },
        ]);
    });

    it('background launch still shows session identifier when job and run ids are absent', async () => {
        (StartAIAssistantBackgroundTask as any).mockResolvedValueOnce({
            accepted: true,
            session_id: 'session-only',
        });

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessageInBackground('background only');
        });

        const systemMessage = result.current.messages.find(m => m.role === 'system');
        expect(systemMessage?.content).toContain('session_id: session-only');
        expect(systemMessage?.content).not.toContain('job_id:');
        expect(systemMessage?.content).not.toContain('run_id:');
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

    it('browseFile adds files and selectedFilePaths state is updated', async () => {
        (SelectAIAssistantFiles as any)
            .mockResolvedValueOnce(['  /tmp/example.png  '])
            .mockResolvedValueOnce(['/tmp/another.txt']);

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.browseFile();
        });
        expect(result.current.selectedFilePaths).toEqual(['/tmp/example.png']);

        await act(async () => {
            await result.current.browseFile();
        });
        expect(result.current.selectedFilePaths).toEqual(['/tmp/example.png', '/tmp/another.txt']);

        // sendMessage receives pre-formatted text from the panel's handleSend;
        // it no longer injects file paths itself to avoid double-injection.
        await act(async () => {
            const preFormatted = buildOutgoingMessageMulti('inspect this', ['/tmp/example.png', '/tmp/another.txt']);
            await result.current.sendMessage(preFormatted);
        });

        expect(SendAIAssistantMessage).toHaveBeenLastCalledWith({
            text: buildOutgoingMessageMulti('inspect this', ['/tmp/example.png', '/tmp/another.txt']),
            request_id: expect.any(String),
        });
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

    it('records submitted prompts and restores them on remount', async () => {
        const { result, unmount } = renderAssistantHook();

        await act(async () => {
            result.current.recordSubmittedPrompt('first prompt');
            await result.current.sendMessage('first prompt');
            result.current.recordSubmittedPrompt('second prompt');
            await result.current.sendMessage('second prompt');
        });

        expect(result.current.submittedPrompts).toEqual(['first prompt', 'second prompt']);
        await waitFor(() => {
            expect(JSON.parse(localStorage.getItem(AI_ASSISTANT_PROMPT_HISTORY_STORAGE_KEY) || '[]')).toEqual(['first prompt', 'second prompt']);
        });

        unmount();

        const { result: remounted } = renderAssistantHook();
        expect(remounted.current.submittedPrompts).toEqual(['first prompt', 'second prompt']);
    });

    it('deduplicates consecutive submitted prompts', async () => {
        const { result } = renderAssistantHook();

        await act(async () => {
            result.current.recordSubmittedPrompt('same prompt');
            await result.current.sendMessage('same prompt');
            result.current.recordSubmittedPrompt('same prompt');
            await result.current.sendMessage('same prompt');
            result.current.recordSubmittedPrompt('different prompt');
            await result.current.sendMessage('different prompt');
        });

        expect(result.current.submittedPrompts).toEqual(['same prompt', 'different prompt']);
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

    it('keeps visible history when backend resets context and excludes older turns from later context', async () => {
        localStorage.setItem(AI_ASSISTANT_HISTORY_STORAGE_KEY, JSON.stringify([
            { id: 'old-user', role: 'user', content: 'old question', timestamp: 1 },
            { id: 'old-assistant', role: 'assistant', content: 'old answer', timestamp: 2 },
        ]));
        (SendAIAssistantMessage as any)
            .mockImplementationOnce(async (req: { request_id?: string }) => ({ text: 'new answer', clear_ui: true, request_id: req.request_id }))
            .mockImplementationOnce(async (req: { request_id?: string }) => ({ text: 'follow answer', request_id: req.request_id }));

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('new task');
        });

        expect(messageContents(result.current.messages)).toEqual([
            'old question',
            'old answer',
            'new task',
            'new answer',
        ]);
        expect(localStorage.getItem(AI_ASSISTANT_HISTORY_STORAGE_KEY)).not.toBeNull();

        await act(async () => {
            await result.current.sendMessage('follow up');
        });

        const followRequest = parseSentRequest(1) as any;
        expect(followRequest.recent_messages?.map((m: any) => m.content)).toEqual([
            'new task',
            'new answer',
        ]);
    });

    it('clears visible history for explicit reset commands without reusing old context', async () => {
        localStorage.setItem(AI_ASSISTANT_HISTORY_STORAGE_KEY, JSON.stringify([
            { id: 'old-user', role: 'user', content: 'old question', timestamp: 1 },
            { id: 'old-assistant', role: 'assistant', content: 'old answer', timestamp: 2 },
        ]));
        (SendAIAssistantMessage as any)
            .mockImplementationOnce(async (req: { request_id?: string }) => ({ text: 'history cleared', clear_ui: true, request_id: req.request_id }))
            .mockImplementationOnce(async (req: { request_id?: string }) => ({ text: 'fresh answer', request_id: req.request_id }));

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('/clear');
        });

        expect(messageContents(result.current.messages)).toEqual(['history cleared']);
        expect(localStorage.getItem(AI_ASSISTANT_HISTORY_STORAGE_KEY)).toBeNull();

        await act(async () => {
            await result.current.sendMessage('fresh start');
        });

        const freshRequest = parseSentRequest(1) as any;
        expect(freshRequest.recent_messages?.map((m: any) => m.content) ?? []).toEqual([]);
    });

    it('ignores stale foreground responses after clearHistory resets the active round', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null; request_id?: string }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('will be cleared');
        });
        const req = parseSentRequest();

        await act(async () => {
            await result.current.clearHistory();
        });
        expect(result.current.messages).toEqual([]);

        await act(async () => {
            pending.resolve({ text: '', error: 'stale error', fields: null, actions: null, request_id: req.request_id });
            await pending.promise;
        });

        expect(messageContents(result.current.messages)).not.toContain('stale error');
        expect(result.current.messages).toEqual([]);
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
            emitRuntimeEvent('ai-assistant-new-round', requestEvent());
            emitRuntimeEvent('ai-assistant-new-round', requestEvent());
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
            emitRuntimeEvent('ai-assistant-token', requestEvent('hello'));
            emitRuntimeEvent('ai-assistant-new-round', requestEvent());
            emitRuntimeEvent('ai-assistant-token', requestEvent(' world'));
            emitRuntimeEvent('ai-assistant-stream-done', requestEvent());
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
            emitRuntimeEvent('ai-assistant-new-round', requestEvent());
            emitRuntimeEvent('ai-assistant-token', requestEvent('hello'));
        });

        expect(result.current.streaming).toBe(true);
        expect(result.current.visualBusy).toBe(true);

        await act(async () => {
            emitRuntimeEvent('ai-assistant-stream-done', requestEvent());
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

    it('keeps overall sending active after stream-done while the request is still finishing tool work', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('hello');
        });

        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round', requestEvent());
            emitRuntimeEvent('ai-assistant-token', requestEvent('hello'));
            emitRuntimeEvent('ai-assistant-stream-done', requestEvent());
        });

        expect(result.current.sending).toBe(true);
        expect(result.current.streaming).toBe(false);
        expect(result.current.visualBusy).toBe(false);

        await act(async () => {
            pending.resolve({ text: 'hello', error: '', fields: null, actions: null });
            await pending.promise;
        });

        expect(result.current.sending).toBe(false);
    });

    it('keeps sending true after the foreground response returns while an AI session is still active', async () => {
        mockSendResponse = {
            text: '🔔 编程会话还在运行中。回复「继续」可以继续看护，回复其它内容正常对话。',
            error: '',
            fields: null,
            actions: null,
            request_id: 'req-default',
            deferred: true,
            run_id: 'run-active-session',
            job_id: 'job-active-session',
        };
        (ListRemoteSessions as any)
            .mockResolvedValueOnce([
                {
                    id: 'sess-active-ai',
                    launch_source: 'ai',
                    status: 'busy',
                    run_id: 'run-active-session',
                    job_id: 'job-active-session',
                },
            ])
            .mockResolvedValueOnce([
                {
                    id: 'sess-active-ai',
                    launch_source: 'ai',
                    status: 'busy',
                    run_id: 'run-active-session',
                    job_id: 'job-active-session',
                },
            ]);

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('keep watching');
        });

        expect(result.current.sending).toBe(true);
        expect(result.current.streaming).toBe(false);
        expect(result.current.visualBusy).toBe(false);
    });

    it('keeps sending true when the active AI session matches by job id only', async () => {
        mockSendResponse = {
            text: '🔔 编程会话还在运行中。回复「继续」可以继续看护，回复其它内容正常对话。',
            error: '',
            fields: null,
            actions: null,
            request_id: 'req-default',
            deferred: true,
            job_id: 'job-only-active-session',
        };
        (ListRemoteSessions as any)
            .mockResolvedValueOnce([
                {
                    id: 'sess-job-only-ai',
                    launch_source: 'ai',
                    status: 'busy',
                    job_id: 'job-only-active-session',
                },
            ])
            .mockResolvedValueOnce([
                {
                    id: 'sess-job-only-ai',
                    launch_source: 'ai',
                    status: 'busy',
                    job_id: 'job-only-active-session',
                },
            ]);

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('track by job id');
        });

        expect(result.current.sending).toBe(true);
        expect(result.current.streaming).toBe(false);
        expect(result.current.visualBusy).toBe(false);
    });

    it('keeps the pending AI task lock when refresh events only retain the tracked session id', async () => {
        mockSendResponse = {
            text: '🔔 编程会话还在运行中。回复「继续」可以继续看护，回复其它内容正常对话。',
            error: '',
            fields: null,
            actions: null,
            request_id: 'req-default',
            deferred: true,
            run_id: 'run-session-id-refresh',
            job_id: 'job-session-id-refresh',
        };
        (ListRemoteSessions as any)
            .mockResolvedValueOnce([
                {
                    id: 'sess-refresh-by-id',
                    launch_source: 'ai',
                    status: 'busy',
                    run_id: 'run-session-id-refresh',
                    job_id: 'job-session-id-refresh',
                },
            ])
            .mockResolvedValueOnce([
                {
                    id: 'sess-refresh-by-id',
                    launch_source: 'ai',
                    status: 'busy',
                },
            ])
            .mockResolvedValueOnce([
                {
                    id: 'sess-refresh-by-id',
                    launch_source: 'ai',
                    status: 'busy',
                },
            ]);

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('keep tracking by session id');
        });

        expect(result.current.sending).toBe(true);

        await act(async () => {
            emitRuntimeEvent('remote-state-changed');
            await Promise.resolve();
        });

        await waitFor(() => {
            expect(result.current.sending).toBe(true);
        });
        expect(result.current.streaming).toBe(false);
        expect(result.current.visualBusy).toBe(false);
    });

    it('does not keep sending true for unrelated remote sessions', async () => {
        mockSendResponse = {
            text: '🔔 编程会话还在运行中。回复「继续」可以继续看护，回复其它内容正常对话。',
            error: '',
            fields: null,
            actions: null,
            request_id: 'req-default',
            deferred: true,
            run_id: 'run-expected-session',
            job_id: 'job-expected-session',
        };
        (ListRemoteSessions as any).mockResolvedValueOnce([
            {
                id: 'sess-other-ai',
                launch_source: 'ai',
                status: 'busy',
                run_id: 'run-other-session',
                job_id: 'job-other-session',
            },
            {
                id: 'sess-terminal-match',
                launch_source: 'ai',
                status: 'exited',
                run_id: 'run-expected-session',
                job_id: 'job-expected-session',
            },
            {
                id: 'sess-non-ai-match',
                launch_source: 'manual',
                status: 'busy',
                run_id: 'run-expected-session',
                job_id: 'job-expected-session',
            },
        ]);

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('finish only when the matching ai task is active');
        });

        expect(result.current.sending).toBe(false);
        expect(result.current.streaming).toBe(false);
        expect(result.current.visualBusy).toBe(false);
    });

    it('clears the pending AI task lock after the tracked remote session exits', async () => {
        mockSendResponse = {
            text: '🔔 编程会话还在运行中。回复「继续」可以继续看护，回复其它内容正常对话。',
            error: '',
            fields: null,
            actions: null,
            request_id: 'req-default',
            deferred: true,
            run_id: 'run-finished-session',
            job_id: 'job-finished-session',
        };
        (ListRemoteSessions as any)
            .mockResolvedValueOnce([
                {
                    id: 'sess-finished-ai',
                    launch_source: 'ai',
                    status: 'busy',
                    run_id: 'run-finished-session',
                    job_id: 'job-finished-session',
                },
            ])
            .mockResolvedValueOnce([
                {
                    id: 'sess-finished-ai',
                    launch_source: 'ai',
                    status: 'busy',
                    run_id: 'run-finished-session',
                    job_id: 'job-finished-session',
                },
            ])
            .mockResolvedValueOnce([
                {
                    id: 'sess-finished-ai',
                    launch_source: 'ai',
                    status: 'exited',
                    run_id: 'run-finished-session',
                    job_id: 'job-finished-session',
                },
            ]);

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('wait for exit');
        });

        expect(result.current.sending).toBe(true);

        await act(async () => {
            emitRuntimeEvent('remote-state-changed');
            await Promise.resolve();
        });

        await waitFor(() => {
            expect(result.current.sending).toBe(false);
        });
    });

    it('cancels the tracked AI task session when only the pending remote task remains', async () => {
        mockSendResponse = {
            text: '🔔 编程会话还在运行中。回复「继续」可以继续看护，回复其它内容正常对话。',
            error: '',
            fields: null,
            actions: null,
            request_id: 'req-default',
            deferred: true,
            run_id: 'run-cancel-session',
            job_id: 'job-cancel-session',
        };
        (ListRemoteSessions as any)
            .mockResolvedValueOnce([
                {
                    id: 'sess-cancel-ai',
                    launch_source: 'ai',
                    status: 'busy',
                    run_id: 'run-cancel-session',
                    job_id: 'job-cancel-session',
                },
            ])
            .mockResolvedValueOnce([
                {
                    id: 'sess-cancel-ai',
                    launch_source: 'ai',
                    status: 'busy',
                    run_id: 'run-cancel-session',
                    job_id: 'job-cancel-session',
                },
            ]);

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('cancel later');
        });

        await act(async () => {
            await result.current.cancelSession();
        });

        expect(CancelAIAssistantTask).toHaveBeenCalledWith('sess-cancel-ai');
        expect(CancelAIAssistantSession).not.toHaveBeenCalled();
        expect(result.current.sending).toBe(false);
    });

    it('finalizes an empty terminal response into a visible fallback with trace details', async () => {
        const pending = deferred<any>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('make pdf');
        });

        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round', requestEvent());
            emitRuntimeEvent('ai-assistant-stream-done', requestEvent());
        });

        await act(async () => {
            pending.resolve({
                text: '',
                error: '',
                fields: null,
                actions: null,
                trace_summary: 'PDF generation stopped before writing the file',
                trace_event_count: 4,
                evidence_count: 2,
                run_id: 'run-empty-result',
                job_id: 'job-empty-result',
            });
            await pending.promise;
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toContain('任务已结束，但没有生成可展示的结果。');
        expect(assistantMessages(result.current.messages)[0].content).toContain('PDF generation stopped before writing the file');
        expect(assistantMessages(result.current.messages)[0].fields).toBeUndefined();
        expect(assistantMessages(result.current.messages)[0].actions).toBeUndefined();
        expect(result.current.sending).toBe(false);
        expect(result.current.streaming).toBe(false);
        expect(result.current.visualBusy).toBe(false);
    });

    it('shows failed empty terminal responses as a failure fallback', async () => {
        mockSendResponse = {
            text: '',
            error: '',
            fields: null,
            actions: null,
            trace_summary: 'PDF generation failed after tool execution',
            trace_status: 'failed',
            run_id: 'run-failed-empty',
        };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('make pdf');
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe('任务未完成可交付结果。PDF generation failed after tool execution');
    });

    it('suppresses conversational echo summaries with punctuation for short chit-chat replies', async () => {
        mockSendResponse = {
            text: '',
            error: '',
            fields: null,
            actions: null,
            trace_summary: '结果：没事。',
            run_id: 'run-empty-conversation-zh-punct',
        };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('没事。');
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe('任务已结束，但没有生成可展示的结果。可查看 Trace 了解详情。');
    });

    it('suppresses english conversational echo summaries with punctuation for short chit-chat replies', async () => {
        mockSendResponse = {
            text: '',
            error: '',
            fields: null,
            actions: null,
            trace_summary: 'summary: nothing...',
            run_id: 'run-empty-conversation-en-punct',
        };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('nothing...');
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe('任务已结束，但没有生成可展示的结果。可查看 Trace 了解详情。');
    });

    it('suppresses ok-style conversational echo summaries', async () => {
        mockSendResponse = {
            text: '',
            error: '',
            fields: null,
            actions: null,
            trace_summary: 'summary: okay.',
            run_id: 'run-empty-conversation-ok',
        };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('okay.');
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe('任务已结束，但没有生成可展示的结果。可查看 Trace 了解详情。');
    });

    it('keeps direct short chit-chat replies visible instead of applying empty fallback', async () => {
        mockSendResponse = {
            text: '好，没问题。我在这，有需要随时叫我。',
            error: '',
            fields: null,
            actions: null,
            trace_summary: '',
            run_id: 'run-direct-chitchat',
        };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('没事。');
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe('好，没问题。我在这，有需要随时叫我。');
    });

    it('keeps direct english short chit-chat replies visible instead of applying empty fallback', async () => {
        mockSendResponse = {
            text: 'No problem. I\'m here if you need anything.',
            error: '',
            fields: null,
            actions: null,
            trace_summary: '',
            run_id: 'run-direct-chitchat-en',
        };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('nothing...');
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe('No problem. I\'m here if you need anything.');
    });

    it('suppresses prompt-like empty terminal summaries to avoid repeating user input', async () => {
        mockSendResponse = {
            text: '',
            error: '',
            fields: null,
            actions: null,
            trace_summary: '请帮我生成一个 PDF，并保存在当前工作目录',
            run_id: 'run-empty-prompt-like',
        };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('make pdf');
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe('任务已结束，但没有生成可展示的结果。可查看 Trace 了解详情。');
    });

    it('suppresses conversational echo summaries for short chit-chat replies', async () => {
        mockSendResponse = {
            text: '',
            error: '',
            fields: null,
            actions: null,
            trace_summary: '没事',
            run_id: 'run-empty-conversation-zh',
        };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('没事');
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe('任务已结束，但没有生成可展示的结果。可查看 Trace 了解详情。');
    });

    it('suppresses conversational echo summaries for short english chit-chat replies', async () => {
        mockSendResponse = {
            text: '',
            error: '',
            fields: null,
            actions: null,
            trace_summary: 'nothing',
            run_id: 'run-empty-conversation-en',
        };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('nothing');
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe('任务已结束，但没有生成可展示的结果。可查看 Trace 了解详情。');
    });

    it('keeps execution-like empty terminal summaries visible', async () => {
        mockSendResponse = {
            text: '',
            error: '',
            fields: null,
            actions: null,
            trace_summary: '文件 review.pdf 已准备好，但未返回正文摘要',
            run_id: 'run-empty-execution-like',
        };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('make pdf');
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe('任务已结束，但没有生成可展示的结果。文件 review.pdf 已准备好，但未返回正文摘要');
    });

    it('finalizes the active tail message without changing streamed content when no terminal status is reported', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('tail finalize');
        });

        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round', requestEvent());
            emitRuntimeEvent('ai-assistant-token', requestEvent('streamed'));
            emitRuntimeEvent('ai-assistant-stream-done', requestEvent());
        });

        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null });
            await pending.promise;
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe('streamed');
    });

    it('replaces streamed partial content with failed terminal fallback', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null; request_id: string; trace_status: string; trace_summary: string }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('tail finalize');
        });

        const req = requestEvent();
        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round', req);
            emitRuntimeEvent('ai-assistant-token', { request_id: req.request_id, text: 'streamed' });
            emitRuntimeEvent('ai-assistant-stream-done', req);
        });

        await act(async () => {
            pending.resolve({
                text: '',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
                trace_status: 'failed',
                trace_summary: 'LLM stream failed after partial output',
            });
            await pending.promise;
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe('任务未完成可交付结果。LLM stream failed after partial output');
    });

    it('replaces streamed partial content with response.error instead of appending a second message', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null; request_id: string }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('tail finalize error');
        });

        const req = requestEvent();
        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round', req);
            emitRuntimeEvent('ai-assistant-token', { request_id: req.request_id, text: 'streamed' });
        });

        await act(async () => {
            pending.resolve({
                text: '',
                error: 'LLM 调用失败: status=529',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
            });
            await pending.promise;
        });

        expect(result.current.messages.filter(message => message.role === 'assistant' || message.role === 'error')).toHaveLength(1);
        expect(result.current.messages.find(message => message.role === 'error')?.content).toBe('LLM 调用失败: status=529');
        expect(messageContents(result.current.messages)).not.toContain('streamed');
    });

    it('keeps streamed tokens on the active tail message across many updates', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('stream a lot');
        });

        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round', requestEvent());
            for (let i = 0; i < 50; i++) {
                emitRuntimeEvent('ai-assistant-token', requestEvent(String(i % 10)));
            }
            emitRuntimeEvent('ai-assistant-stream-done', requestEvent());
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
            emitRuntimeEvent('ai-assistant-new-round', requestEvent());
            emitRuntimeEvent('ai-assistant-new-round', requestEvent());
            emitRuntimeEvent('ai-assistant-stream-done', requestEvent());
            emitRuntimeEvent('ai-assistant-stream-done', requestEvent());
        });

        expect(result.current.sending).toBe(true);
        expect(result.current.streaming).toBe(false);
        expect(assistantMessages(result.current.messages)).toHaveLength(1);

        await act(async () => {
            pending.resolve({ text: 'done', error: '', fields: null, actions: null });
            await pending.promise;
        });
    });

    it('cancelSession preserves active round content, marks it cancelled, and ignores stale completion', async () => {
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
            emitRuntimeEvent('ai-assistant-new-round', requestEvent());
            emitRuntimeEvent('ai-assistant-token', requestEvent('partial'));
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe('partial');

        let cancelResult;
        await act(async () => {
            cancelResult = await result.current.cancelSession();
        });

        expect(cancelResult).toEqual({ canceledText: 'retry first request' });
        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe(`partial\n${CANCELED_BY_USER_LINE}`);
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
        expect(messageContents(result.current.messages)).toContain(`partial\n${CANCELED_BY_USER_LINE}`);
        expect(messageContents(result.current.messages)).toContain('fresh reply');

        await act(async () => {
            first.resolve({ text: 'stale reply', error: '', fields: null, actions: null });
            await first.promise;
        });

        expect(messageContents(result.current.messages)).not.toContain('stale reply');
        expect(assistantMessages(result.current.messages)).toHaveLength(2);
        expect(assistantMessages(result.current.messages).map(m => m.content)).toEqual([
            `partial\n${CANCELED_BY_USER_LINE}`,
            'fresh reply',
        ]);
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

    it('normalizes structured unfinished slot payloads into assistant messages', async () => {
        mockSendResponse = {
            text: '检测到未完成任务',
            error: '',
            actions: null,
            unfinished_slot: {
                slot_id: 'slot-1',
                title: '继续 Daily Paper',
                summary: '还差最后一轮整理',
                project_path: 'D:/work/project',
                status: 'pending_resume',
                actions: [
                    { label: '继续上次任务', command: '__resume_unfinished__ slot-1', style: 'default' },
                    { label: '开始新任务', command: '__start_new_task__', style: 'default' },
                ],
            },
        };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('新的消息');
        });

        const assistantMsg = result.current.messages.find(m => m.role === 'assistant');
        expect(assistantMsg?.unfinishedSlot).toEqual({
            slotID: 'slot-1',
            title: '继续 Daily Paper',
            summary: '还差最后一轮整理',
            projectPath: 'D:/work/project',
            status: 'pending_resume',
            actions: [
                { label: '继续上次任务', command: '__resume_unfinished__ slot-1', style: 'default' },
                { label: '开始新任务', command: '__start_new_task__', style: 'default' },
            ],
        });
    });

    it('executeAction routes explicit unfinished slot commands through sendMessage options', async () => {
        const calls: any[] = [];
        (SendAIAssistantMessage as any).mockImplementation(async (req: any) => {
            calls.push(req);
            return { text: 'ok', error: '', fields: null, actions: null, request_id: req.request_id || 'req' };
        });

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.executeAction('__resume_unfinished__ slot-99');
            await result.current.executeAction('__start_new_task__');
            await result.current.executeAction('__dismiss_unfinished__ slot-99');
        });

        expect(calls[0]?.resume_slot_id).toBe('slot-99');
        expect(calls[1]?.start_new_task).toBe(true);
        expect(calls[2]?.dismiss_slot_id).toBe('slot-99');
        expect(calls[2]?.start_new_task).toBe(true);
    });

    it('normalizes confirmation payloads into assistant messages', async () => {
        mockSendResponse = {
            text: '请先确认',
            error: '',
            fields: null,
            actions: [
                { label: '确认并开始', command: '确认，按这个开始', style: 'default' },
                { label: '取消', command: '取消这个任务', style: 'default' },
            ],
            confirmation: {
                id: 'c1',
                summary: '我理解你想让我修复登录问题',
                task_type: 'coding',
                target_paths: ['D:/work/project'],
                planned_actions: ['检查登录流程'],
                risk_flags: ['会修改代码'],
                revision_hints: ['如目录不对请直接改正'],
                status: 'pending',
            },
        };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('修登录 bug');
        });

        const assistantMsg = result.current.messages.find(m => m.role === 'assistant');
        expect(assistantMsg?.content).toBe('请先确认');
        expect(assistantMsg?.confirmation).toEqual({
            id: 'c1',
            summary: '我理解你想让我修复登录问题',
            taskType: 'coding',
            targetPaths: ['D:/work/project'],
            plannedActions: ['检查登录流程'],
            riskFlags: ['会修改代码'],
            revisionHints: ['如目录不对请直接改正'],
            status: 'pending',
        });
        expect(assistantMsg?.actions).toEqual([
            { label: '确认并开始', command: '确认，按这个开始', style: 'default' },
            { label: '取消', command: '取消这个任务', style: 'default' },
        ]);
    });

    it('supports PascalCase confirmation payload fields', async () => {
        mockSendResponse = {
            Text: '请确认再继续',
            Error: '',
            Actions: null,
            Confirmation: {
                ID: 'c2',
                Summary: '默认工作目录：D:/fixed/project',
                TaskType: 'coding',
                TargetPaths: ['D:/fixed/project'],
                PlannedActions: ['修改代码'],
                RiskFlags: ['影响现有逻辑'],
                RevisionHints: ['补充正确目录'],
                Status: 'pending',
            },
        };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('继续');
        });

        const assistantMsg = result.current.messages.find(m => m.role === 'assistant');
        expect(assistantMsg?.content).toBe('请确认再继续');
        expect(assistantMsg?.confirmation).toEqual({
            id: 'c2',
            summary: '默认工作目录：D:/fixed/project',
            taskType: 'coding',
            targetPaths: ['D:/fixed/project'],
            plannedActions: ['修改代码'],
            riskFlags: ['影响现有逻辑'],
            revisionHints: ['补充正确目录'],
            status: 'pending',
        });
    });

    it('marks the latest confirmation as running before approval request resolves', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
        mockSendResponse = {
            text: '请先确认',
            error: '',
            fields: null,
            actions: [
                { label: '确认并开始', command: '确认，按这个开始', style: 'default' },
            ],
            confirmation: {
                id: 'c1',
                summary: '我理解你想让我修复登录问题',
                task_type: 'coding',
                target_paths: ['D:/work/project'],
                planned_actions: ['检查登录流程'],
                risk_flags: ['会修改代码'],
                revision_hints: ['如目录不对请直接改正'],
                status: 'pending',
            },
        };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('修登录 bug');
        });

        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        await act(async () => {
            void result.current.executeAction('确认，按这个开始');
        });

        const assistantMsgs = assistantMessages(result.current.messages);
        expect(assistantMsgs).toHaveLength(2);
        expect(assistantMsgs[0].confirmation?.status).toBe('running');
        expect(assistantMsgs[1].content).toBe('');

        await act(async () => {
            pending.resolve({ text: '已开始处理', error: '', fields: null, actions: null });
            await pending.promise;
        });

        const finalAssistantMsgs = assistantMessages(result.current.messages);
        expect(finalAssistantMsgs[0].confirmation?.status).toBe('running');
        expect(finalAssistantMsgs[1].content).toBe('已开始处理');
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

    it('preserves draft input state across rerenders until cleared', async () => {
        const { result, rerender } = renderAssistantHook();

        act(() => {
            result.current.setDraftInputValue('draft survives rerender');
        });
        rerender();

        expect(result.current.draftInputValue).toBe('draft survives rerender');

        await act(async () => {
            await result.current.clearHistory();
        });

        expect(result.current.draftInputValue).toBe('');
    });

    it('hides token usage fields by default when detail entry is disabled', async () => {
        mockSendResponse = {
            Text: 'done',
            Error: '',
            Fields: [
                { Label: 'Input tokens', Value: '120' },
                { Label: 'Output tokens', Value: '30' },
                { Label: 'Total tokens', Value: '150' },
            ],
            Actions: null,
            RequestID: 'req-pascal',
        };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('show token usage');
        });

        const assistantMsg = result.current.messages.find(m => m.role === 'assistant');
        expect(assistantMsg?.content).toBe('done');
        expect(assistantMsg?.fields).toBeUndefined();
    });

    it('keeps token usage fields when detail entry is enabled', async () => {
        (LoadConfig as any).mockResolvedValueOnce({ show_ai_trace_entry: true });
        mockSendResponse = {
            Text: 'done',
            Error: '',
            Fields: [
                { Label: 'Input tokens', Value: '120' },
                { Label: 'Output tokens', Value: '30' },
                { Label: 'Total tokens', Value: '150' },
            ],
            Actions: null,
            RequestID: 'req-pascal',
        };

        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(LoadConfig).toHaveBeenCalled();
            expect(result.current.messages).toEqual([]);
        });
        act(() => {
            emitRuntimeEvent('config-changed', new main.AppConfig({ show_ai_trace_entry: true, trial_reflect_enabled: false }));
        });

        await act(async () => {
            await result.current.sendMessage('show token usage');
        });

        const assistantMsg = result.current.messages.find(m => m.role === 'assistant');
        expect(assistantMsg?.content).toBe('done');
        expect(assistantMsg?.fields).toEqual([
            { label: 'Input tokens', value: '120' },
            { label: 'Output tokens', value: '30' },
            { label: 'Total tokens', value: '150' },
        ]);
    });

    it('hides token usage fields from normalized assistant response by default', async () => {
        mockSendResponse = {
            text: 'done',
            error: '',
            fields: [
                { label: 'Input tokens', value: '120' },
                { label: 'Output tokens', value: '30' },
                { label: 'Total tokens', value: '150' },
            ],
            actions: null,
            input_tokens: 120,
            output_tokens: 30,
            total_tokens: 150,
        };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('show token usage');
        });

        const assistantMsg = result.current.messages.find(m => m.role === 'assistant');
        expect(assistantMsg?.fields).toBeUndefined();
    });

    it('keeps token usage fields from normalized assistant response when detail entry is enabled', async () => {
        (LoadConfig as any).mockResolvedValueOnce({ show_ai_trace_entry: true });
        mockSendResponse = {
            text: 'done',
            error: '',
            fields: [
                { label: 'Input tokens', value: '120' },
                { label: 'Output tokens', value: '30' },
                { label: 'Total tokens', value: '150' },
            ],
            actions: null,
            input_tokens: 120,
            output_tokens: 30,
            total_tokens: 150,
        };

        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(LoadConfig).toHaveBeenCalled();
            expect(result.current.messages).toEqual([]);
        });
        act(() => {
            emitRuntimeEvent('config-changed', new main.AppConfig({ show_ai_trace_entry: true, trial_reflect_enabled: false }));
        });

        await act(async () => {
            await result.current.sendMessage('show token usage');
        });

        const assistantMsg = result.current.messages.find(m => m.role === 'assistant');
        expect(assistantMsg?.fields).toEqual([
            { label: 'Input tokens', value: '120' },
            { label: 'Output tokens', value: '30' },
            { label: 'Total tokens', value: '150' },
        ]);
    });

    it('hides trace summary fields by default when trace entry is disabled', async () => {
        mockSendResponse = {
            text: 'done',
            error: '',
            fields: [{ label: 'Existing', value: 'keep me' }],
            actions: null,
            trace_summary: 'trial loop stabilized after one retry',
            trace_event_count: 8,
            evidence_count: 3,
            trial_reflect_summary: 'tools=bash; failures=1; repeat guard avoided duplicate failed actions; recovered after failure',
            trial_reflect_status: 'recovered_success',
            trial_reflect_failures: 1,
            run_id: 'run-trace-1',
            job_id: 'job-trace-1',
        };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('show trace');
        });

        const assistantMsg = result.current.messages.find(m => m.role === 'assistant');
        expect(assistantMsg?.fields).toEqual([
            { label: 'Existing', value: 'keep me' },
        ]);
        expect(assistantMsg?.actions).toBeUndefined();
    });

    it('shows trace fields and action when trace entry is enabled in config', async () => {
        (LoadConfig as any).mockResolvedValueOnce({ show_ai_trace_entry: true });
        mockSendResponse = {
            text: 'done',
            error: '',
            fields: [{ label: 'Existing', value: 'keep me' }],
            actions: null,
            trace_summary: 'trial loop stabilized after one retry',
            trace_event_count: 8,
            evidence_count: 3,
            trial_reflect_summary: 'tools=bash; failures=1; repeat guard avoided duplicate failed actions; recovered after failure',
            trial_reflect_status: 'recovered_success',
            trial_reflect_failures: 1,
            run_id: 'run-trace-1',
            job_id: 'job-trace-1',
        };

        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(LoadConfig).toHaveBeenCalled();
        });

        await act(async () => {
            await result.current.sendMessage('show trace');
        });

        const assistantMsg = result.current.messages.find(m => m.role === 'assistant');
        expect(assistantMsg?.fields).toEqual([
            { label: 'Existing', value: 'keep me' },
            { label: 'Trace', value: 'trial loop stabilized after one retry' },
            { label: 'Recovery', value: 'Recovered after retry' },
            { label: 'Failures', value: '1' },
            { label: 'Trial reflect', value: 'tools=bash; failures=1; repeat guard avoided duplicate failed actions; recovered after failure' },
            { label: 'Trace events', value: '8' },
            { label: 'Evidence', value: '3' },
            { label: 'Run ID', value: 'run-trace-1' },
            { label: 'Job ID', value: 'job-trace-1' },
        ]);
        expect(assistantMsg?.actions).toEqual([
            { label: 'View trace', command: '__view_trace__ run-trace-1', style: 'default' },
        ]);
    });

    it('executeAction fetches and appends trace detail messages for trace actions', async () => {
        (GetAIAssistantTrace as any).mockResolvedValueOnce({
            job_id: 'job-trace-1',
            status: 'completed',
            summary: 'trial loop stabilized after one retry',
            event_count: 8,
            evidence_count: 3,
            trial_reflect_summary: {
                failure_count: 1,
                failure_categories: ['args'],
                final_outcome: 'recovered_success',
                strategy_note: 'tools=bash; failures=1; categories=args; repeat guard avoided duplicate failed actions; recovered after failure',
            },
            events: [{ kind: 'trial.started' }, { kind: 'trial.observed' }],
            evidence: [{ source_kind: 'trial_reflect', category: 'repeat_guard' }],
        });
        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.executeAction('__view_trace__ run-trace-1');
        });

        expect(GetAIAssistantTrace).toHaveBeenCalledWith('run-trace-1');
        const systemMessage = result.current.messages.findLast(m => m.role === 'system');
        expect(systemMessage?.kind).toBe('trace');
        expect(systemMessage?.fields).toEqual([
            { label: 'Run ID', value: 'run-trace-1' },
            { label: 'Recovery', value: 'Recovered after retry' },
            { label: 'Failures', value: '1' },
            { label: 'Job ID', value: 'job-trace-1' },
            { label: 'Trace events', value: '8' },
            { label: 'Evidence', value: '3' },
            { label: 'Status', value: 'completed' },
        ]);
        expect(systemMessage?.content).toContain('Trace details for run-trace-1');
        expect(systemMessage?.content).toContain('Summary: trial loop stabilized after one retry');
        expect(systemMessage?.content).toContain('Recovery status: Recovered after retry');
        expect(systemMessage?.content).toContain('Trial-reflect: tools=bash; failures=1; categories=args; repeat guard avoided duplicate failed actions; recovered after failure');
        expect(systemMessage?.content).toContain('Recovery: recovered_success');
        expect(systemMessage?.content).toContain('Failure categories: args');
        expect(systemMessage?.content).toContain('Events: 8');
        expect(systemMessage?.content).toContain('Evidence: 3');
        expect(systemMessage?.content).toContain('Event kinds: trial.started, trial.observed');
        expect(systemMessage?.content).toContain('Evidence kinds: trial_reflect/repeat_guard');
    });

    it('executeAction surfaces trace fetch failures as error messages', async () => {
        (GetAIAssistantTrace as any).mockRejectedValueOnce(new Error('trace not found'));
        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.executeAction('__view_trace__ missing-run');
        });

        const errorMessage = result.current.messages.findLast(m => m.role === 'error');
        expect(errorMessage?.content).toBe('trace not found');
    });

    it('omits empty trace summary fields when counts are absent', async () => {
        mockSendResponse = {
            text: 'done',
            error: '',
            fields: null,
            actions: null,
            trace_summary: '   ',
            trace_event_count: 0,
            evidence_count: 0,
        };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('show no trace');
        });

        const assistantMsg = result.current.messages.find(m => m.role === 'assistant');
        expect(assistantMsg?.fields).toBeUndefined();
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

    it('ignores stream events from a different request id', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null; request_id: string }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('only my stream');
        });

        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round', otherRequestEvent());
            emitRuntimeEvent('ai-assistant-token', otherRequestEvent('wrong'));
            emitRuntimeEvent('ai-assistant-stream-done', otherRequestEvent());
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe('');
        expect(result.current.streaming).toBe(false);
        expect(result.current.sending).toBe(true);

        const req = parseSentRequest();
        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round', { request_id: req.request_id || '', text: '' });
            emitRuntimeEvent('ai-assistant-token', { request_id: req.request_id || '', text: 'right' });
            emitRuntimeEvent('ai-assistant-stream-done', { request_id: req.request_id || '', text: '' });
        });

        expect(assistantMessages(result.current.messages)[0].content).toBe('right');

        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null, request_id: req.request_id || '' });
            await pending.promise;
        });
    });

    it('keeps stale final responses from replacing a newer round when the original placeholder was removed', async () => {
        const first = deferred<{ text: string; error: string; fields: null; actions: null; request_id: string }>();
        const second = deferred<{ text: string; error: string; fields: null; actions: null; request_id: string }>();
        (SendAIAssistantMessage as any)
            .mockImplementationOnce(() => first.promise)
            .mockImplementationOnce(() => second.promise);
        (CancelAIAssistantSession as any).mockResolvedValueOnce('retry first request');

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('first request');
        });

        const firstReq = requestEvent(0);
        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round', firstReq);
            emitRuntimeEvent('ai-assistant-token', { request_id: firstReq.request_id, text: 'first partial' });
        });

        await act(async () => {
            await result.current.cancelSession();
        });

        await act(async () => {
            void result.current.sendMessage('second request');
        });

        const secondReq = requestEvent(1);
        await act(async () => {
            second.resolve({ text: 'fresh reply', error: '', fields: null, actions: null, request_id: secondReq.request_id || '' });
            await second.promise;
        });

        await act(async () => {
            first.resolve({ text: '', error: 'stale error', fields: null, actions: null, request_id: firstReq.request_id || '' });
            await first.promise;
        });

        expect(messageContents(result.current.messages)).toContain('fresh reply');
        expect(messageContents(result.current.messages)).not.toContain('stale error');
    });

    it('shows grace-round wrap-up progress without changing busy state', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('make pdf');
        });

        expect(result.current.sending).toBe(true);
        expect(result.current.streaming).toBe(false);

        await act(async () => {
            emitRuntimeEvent('ai-assistant-progress', { request_id: requestEvent().request_id, text: '⏳ 已接近最大推理轮次，正在基于现有信息收尾并生成最终结果…' });
        });

        expect(result.current.progressMessages[result.current.progressMessages.length - 1]?.content).toBe('⏳ 已接近最大推理轮次，正在基于现有信息收尾并生成最终结果…');
        expect(result.current.sending).toBe(true);
        expect(result.current.streaming).toBe(false);
        expect(result.current.visualBusy).toBe(false);

        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null, local_file_path: '/tmp/review.pdf' } as any);
            await pending.promise;
        });

        expect(result.current.sending).toBe(false);
        expect(result.current.streaming).toBe(false);
        expect(result.current.visualBusy).toBe(false);
    });

    it('ignores late progress after the round has finalized', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null; request_id: string; trace_summary?: string }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('make pdf');
        });

        const req = requestEvent();
        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null, request_id: req.request_id, trace_summary: 'PDF generation stopped before writing the file' });
            await pending.promise;
        });

        expect(result.current.progressMessages).toHaveLength(0);

        await act(async () => {
            emitRuntimeEvent('ai-assistant-progress', { request_id: req.request_id, text: '正在执行工具，请稍候...' });
        });

        expect(result.current.progressMessages).toHaveLength(0);
    });

    it('ignores progress without the active request id', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null; request_id: string; local_file_path?: string }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('make pdf');
        });

        await act(async () => {
            emitRuntimeEvent('ai-assistant-progress', 'legacy progress');
            emitRuntimeEvent('ai-assistant-progress', { request_id: 'other-request', text: 'wrong progress' });
        });

        expect(result.current.progressMessages).toHaveLength(0);

        const req = requestEvent();
        await act(async () => {
            emitRuntimeEvent('ai-assistant-progress', { request_id: req.request_id, text: 'right progress' });
        });

        expect(result.current.progressMessages).toHaveLength(1);
        expect(result.current.progressMessages[0].content).toBe('right progress');

        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null, request_id: req.request_id, local_file_path: '/tmp/review.pdf' });
            await pending.promise;
        });
    });

    it('opens and clears AgentView from lifecycle events', async () => {
        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(runtimeHandlers.has('agent-view:lifecycle')).toBe(true);
        });

        act(() => {
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'open',
                view: {
                    id: 'expense-form',
                    type: 'form',
                    title: 'Expense',
                    fields: [{ name: 'amount', label: 'Amount', type: 'number', value: 86 }],
                },
            });
        });

        expect(result.current.agentView?.id).toBe('expense-form');

        act(() => {
            emitRuntimeEvent('agent-view:lifecycle', { action: 'dismiss', view_id: 'expense-form' });
        });

        expect(result.current.agentView).toBeNull();
    });

    it('keeps lifecycle complete result views visible', async () => {
        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(runtimeHandlers.has('agent-view:lifecycle')).toBe(true);
        });

        act(() => {
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'complete',
                view: {
                    id: 'tool-result',
                    type: 'result_browser',
                    title: 'Result',
                    results: [{ title: 'Status', status: 'Committed' }],
                },
            });
        });

        expect(result.current.agentView?.id).toBe('tool-result');
    });

    it('keeps coding agent progress events visible in live progress state', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null; request_id: string; local_file_path?: string }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('fix stale edit guard');
        });

        const req = requestEvent();
        await act(async () => {
            emitRuntimeEvent('ai-assistant-progress', { request_id: req.request_id, text: 'Coding Agent: running T2 - Fix stale edit guard' });
        });

        expect(result.current.progressMessages).toHaveLength(1);
        expect(result.current.progressMessages[0].role).toBe('progress');
        expect(result.current.progressMessages[0].content).toBe('Coding Agent: running T2 - Fix stale edit guard');
        expect(result.current.sending).toBe(true);

        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null, request_id: req.request_id, local_file_path: '/tmp/review.pdf' });
            await pending.promise;
        });
    });

    it('keeps single local_file_path responses visible without fallback text', async () => {
        mockSendResponse = {
            text: '',
            error: '',
            fields: null,
            actions: null,
            local_file_path: '/tmp/review.pdf',
        };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('make pdf');
        });

        const assistantMsg = result.current.messages.find(m => m.role === 'assistant');
        expect(assistantMsg?.content).toBe('');
        expect(assistantMsg?.localFilePath).toBe('/tmp/review.pdf');
        expect(assistantMsg?.localFilePaths).toEqual(['/tmp/review.pdf']);
    });

    it('deduplicates consecutive identical progress events', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null; request_id: string; local_file_path?: string }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('track progress');
        });

        const req = requestEvent();
        await act(async () => {
            emitRuntimeEvent('ai-assistant-progress', { request_id: req.request_id, text: 'same progress' });
            emitRuntimeEvent('ai-assistant-progress', { request_id: req.request_id, text: 'same progress' });
            emitRuntimeEvent('ai-assistant-progress', { request_id: req.request_id, text: 'same progress' });
        });

        expect(result.current.progressMessages).toHaveLength(1);
        expect(result.current.progressMessages[0].content).toBe('same progress');

        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null, request_id: req.request_id, local_file_path: '/tmp/review.pdf' });
            await pending.promise;
        });
    });

    it('clearHistory resets progress dedupe state', async () => {
        const first = deferred<{ text: string; error: string; fields: null; actions: null; request_id: string; local_file_path?: string }>();
        const second = deferred<{ text: string; error: string; fields: null; actions: null; request_id: string; local_file_path?: string }>();
        (SendAIAssistantMessage as any)
            .mockImplementationOnce(() => first.promise)
            .mockImplementationOnce(() => second.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('track progress');
        });

        const req = requestEvent();
        await act(async () => {
            emitRuntimeEvent('ai-assistant-progress', { request_id: req.request_id, text: 'same progress' });
            emitRuntimeEvent('ai-assistant-progress', { request_id: req.request_id, text: 'same progress' });
        });
        expect(result.current.progressMessages).toHaveLength(1);

        await act(async () => {
            await result.current.clearHistory();
        });
        expect(result.current.progressMessages).toEqual([]);

        await act(async () => {
            void result.current.sendMessage('track progress again');
        });

        const nextReq = requestEvent(1);
        await act(async () => {
            emitRuntimeEvent('ai-assistant-progress', { request_id: nextReq.request_id, text: 'same progress' });
        });
        expect(result.current.progressMessages).toHaveLength(1);
        expect(result.current.progressMessages[0].content).toBe('same progress');

        await act(async () => {
            second.resolve({ text: '', error: '', fields: null, actions: null, request_id: nextReq.request_id, local_file_path: '/tmp/review-2.pdf' });
            await second.promise;
        });

        await act(async () => {
            first.resolve({ text: '', error: '', fields: null, actions: null, request_id: req.request_id, local_file_path: '/tmp/review.pdf' });
            await first.promise;
        });
    });

    it('Property 10: assistant reply is populated after progress messages', async () => {
        await fc.assert(
            fc.asyncProperty(
                fc.string({ minLength: 1, maxLength: 40 }).filter(s => s.trim().length > 0),
                fc.array(fc.string({ minLength: 1, maxLength: 40 }), { minLength: 1, maxLength: 5 }),
                async (userText, progressTexts) => {
                    const { result, unmount } = renderAssistantHook();

                    (SendAIAssistantMessage as any).mockImplementationOnce(async (req: { request_id?: string }) => {
                        await Promise.resolve();
                        for (const pt of progressTexts) {
                            emitRuntimeEvent('ai-assistant-progress', { request_id: req.request_id || '', text: pt });
                        }
                        return { text: 'done', error: '', fields: null, actions: null, request_id: req.request_id || '' };
                    });

                    await act(async () => {
                        await result.current.sendMessage(userText);
                    });

                    const msgs = result.current.messages;
                    const assistantMsg = msgs.find(m => m.role === 'assistant');
                    const progressMsgs = result.current.progressMessages;

                    expect(assistantMsg).toBeDefined();
                    if (progressTexts.some(text => text.trim().length > 0)) {
                        expect(progressMsgs.length).toBeGreaterThan(0);
                    }
                    expect(assistantMsg!.content).toBe('done');

                    for (const pt of progressTexts) {
                        if (pt.trim().length === 0) continue;
                        expect(progressMsgs.find(m => m.content === pt)).toBeDefined();
                    }

                    unmount();
                },
            ),
            { numRuns: 100 },
        );
    }, 10000);
});
