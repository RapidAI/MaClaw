import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import * as fc from 'fast-check';
import { main } from '../../../../wailsjs/go/models';

let mockSendResponse: any = { text: 'ok', error: '', fields: null, actions: null, request_id: 'req-default' };
let mockSendError: Error | null = null;
let mockUIState: any = { messages: [], prompts: [] };
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
    ClearAIAssistantHistoryForSession: vi.fn(async () => {}),
    ClearAIAssistantUIState: vi.fn(async () => { mockUIState = { messages: [], prompts: [] }; }),
    LoadAIAssistantUIState: vi.fn(async () => mockUIState),
    SaveAIAssistantUIState: vi.fn(async (state: any) => { mockUIState = state; }),
    IsAIAssistantReady: vi.fn(async () => true),
    GetAIAssistantInitStatus: vi.fn(async () => 'ready'),
    GetTrialReflectEnabled: vi.fn(async () => false),
    GetAIAssistantTrace: vi.fn(async () => ({ summary: 'trace ok', event_count: 2, evidence_count: 1, events: [], evidence: [] })),
    LoadConfig: vi.fn(async () => ({ show_ai_trace_entry: false })),
    CancelAIAssistantSession: vi.fn(async () => {}),
    CancelAIAssistantSessionForSession: vi.fn(async () => {}),
    CancelAIAssistantTask: vi.fn(async () => {}),
    StartAIAssistantBackgroundTask: vi.fn(async () => ({ accepted: true, session_id: 'session-test' })),
    ListRemoteSessions: vi.fn(async () => []),
    FetchNews: vi.fn(async () => []),
    SelectAIAssistantFiles: vi.fn(async () => []),
    InjectAIAssistantSupplementary: vi.fn(async () => false),
    InjectAIAssistantSupplementaryForSession: vi.fn(async () => false),
    InjectAIAssistantGuideReference: vi.fn(async () => true),
    InjectAIAssistantGuideReferenceForSession: vi.fn(async () => true),
    SubmitAgentView: vi.fn(async () => ({ text: 'submitted', error: '' })),
    DismissAgentView: vi.fn(async () => ({ text: 'dismissed', error: '' })),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn((event: string, handler: (payload?: unknown) => void) => {
        runtimeHandlers.set(event, handler);
    }),
    EventsOff: vi.fn((event: string) => {
        runtimeHandlers.delete(event);
    }),
}));

import { useAIAssistant, buildOutgoingMessage, buildOutgoingMessageMulti, AI_ASSISTANT_HISTORY_STORAGE_KEY, AI_ASSISTANT_PROMPT_HISTORY_STORAGE_KEY, CANCELED_BY_USER_LINE, isPinnedNewsMessage, forgetAIAssistantSessionRounds, setActiveSessionKey, type ChatAction } from '../useAIAssistant';
import { ClearAIAssistantHistory, ClearAIAssistantHistoryForSession, ClearAIAssistantUIState, SendAIAssistantMessage, CancelAIAssistantSession, CancelAIAssistantSessionForSession, CancelAIAssistantTask, StartAIAssistantBackgroundTask, FetchNews, SelectAIAssistantFiles, GetAIAssistantInitStatus, GetTrialReflectEnabled, GetAIAssistantTrace, IsAIAssistantReady, LoadAIAssistantUIState, LoadConfig, ListRemoteSessions, InjectAIAssistantSupplementary, InjectAIAssistantSupplementaryForSession, InjectAIAssistantGuideReference, InjectAIAssistantGuideReferenceForSession, SaveAIAssistantUIState, SubmitAgentView, DismissAgentView } from '../../../../wailsjs/go/main/App';

function renderAssistantHook(options?: Parameters<typeof useAIAssistant>[0]) {
    return renderHook(() => useAIAssistant(options));
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
    (ClearAIAssistantHistoryForSession as any).mockReset();
    (ClearAIAssistantHistoryForSession as any).mockImplementation(async () => {});
    (ClearAIAssistantUIState as any).mockReset();
    (ClearAIAssistantUIState as any).mockImplementation(async () => { mockUIState = { messages: [], prompts: [] }; });
    (LoadAIAssistantUIState as any).mockReset();
    (LoadAIAssistantUIState as any).mockImplementation(async () => mockUIState);
    (SaveAIAssistantUIState as any).mockReset();
    (SaveAIAssistantUIState as any).mockImplementation(async (state: any) => { mockUIState = state; });
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
    (CancelAIAssistantSessionForSession as any).mockReset();
    (CancelAIAssistantSessionForSession as any).mockImplementation(async () => {});
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
    (InjectAIAssistantSupplementary as any).mockReset();
    (InjectAIAssistantSupplementary as any).mockImplementation(async () => false);
    (InjectAIAssistantSupplementaryForSession as any).mockReset();
    (InjectAIAssistantSupplementaryForSession as any).mockImplementation(async () => false);
    (InjectAIAssistantGuideReference as any).mockReset();
    (InjectAIAssistantGuideReference as any).mockImplementation(async () => true);
    (InjectAIAssistantGuideReferenceForSession as any).mockReset();
    (InjectAIAssistantGuideReferenceForSession as any).mockImplementation(async () => true);
    (SubmitAgentView as any).mockReset();
    (SubmitAgentView as any).mockImplementation(async () => ({ text: 'submitted', error: '' }));
    (DismissAgentView as any).mockReset();
    (DismissAgentView as any).mockImplementation(async () => ({ text: 'dismissed', error: '' }));
}

function assistantMessages(messages: Array<{ role: string; content: string; reasoning?: string; fields?: unknown; actions?: unknown; confirmation?: { status?: string } }>) {
    return messages.filter(message => message.role === 'assistant');
}

async function expectStreamedPartialPreservedOnBackendError(errorText: string, partialText: string, prompt = 'tail timeout error') {
    const pending = deferred<{ text: string; error: string; fields: null; actions: null; request_id: string }>();
    (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

    const { result } = renderAssistantHook();

    await act(async () => {
        void result.current.sendMessage(prompt);
    });

    const req = requestEvent();
    await act(async () => {
        emitRuntimeEvent('ai-assistant-new-round', req);
        emitRuntimeEvent('ai-assistant-token', { request_id: req.request_id, text: partialText });
    });

    await act(async () => {
        pending.resolve({
            text: '',
            error: errorText,
            fields: null,
            actions: null,
            request_id: req.request_id || '',
        });
        await pending.promise;
    });

    expect(assistantMessages(result.current.messages)).toHaveLength(1);
    expect(assistantMessages(result.current.messages)[0].content).toBe(partialText);
    expect(result.current.messages.find(message => message.role === 'error')?.content).toBe(errorText);
}

type ProgressPayloadFactory = (req: { request_id: string; text: string }) => unknown;

async function expectProgressPayloadTimeoutBehavior(
    payloadOrFactory: unknown | ProgressPayloadFactory,
    options: { activeSessionKey?: string; expectAlive: boolean; prompt?: string },
) {
    vi.useFakeTimers();
    try {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook(options.activeSessionKey ? { activeSessionKey: options.activeSessionKey } : undefined);

        await act(async () => {
            void result.current.sendMessage(options.prompt || 'long active task');
            await Promise.resolve();
        });

        const req = requestEvent();
        const payload = typeof payloadOrFactory === 'function'
            ? (payloadOrFactory as ProgressPayloadFactory)(req)
            : payloadOrFactory;
        const beforeEmitMs = options.expectAlive ? 599_000 : 300_000;
        const afterEmitMs = options.expectAlive ? 599_000 : 300_001;

        await act(async () => {
            await vi.advanceTimersByTimeAsync(beforeEmitMs);
            emitRuntimeEvent('ai-assistant-progress', payload);
            await vi.advanceTimersByTimeAsync(afterEmitMs);
        });

        expect(result.current.progressMessages).toEqual([]);
        if (options.expectAlive) {
            expect(result.current.sending).toBe(true);
            expect(messageContents(result.current.messages).join('\n')).not.toContain('请求超时');
            await act(async () => {
                pending.resolve({ text: 'done', error: '', fields: null, actions: null });
                await pending.promise;
            });
        } else {
            expect(result.current.sending).toBe(false);
            expect(messageContents(result.current.messages).join('\n')).toContain('请求超时');
        }
    } finally {
        vi.useRealTimers();
    }
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
        mockUIState = { messages: [], prompts: [] };
        runtimeHandlers.clear();
        resetAppMocks();
        localStorage.clear();
        setActiveSessionKey('');
    });

    afterEach(() => {
        mockUIState = { messages: [], prompts: [] };
        localStorage.clear();
        runtimeHandlers.clear();
        resetAppMocks();
        setActiveSessionKey('');
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

    it('shows localized guide reference echo for the local desktop session', async () => {
        const { result } = renderAssistantHook();

        let accepted = false;
        await act(async () => {
            accepted = await result.current.guideLaunchReference('下一轮参考这个');
        });

        expect(accepted).toBe(true);
        expect(InjectAIAssistantGuideReferenceForSession).toHaveBeenCalledWith('下一轮参考这个', 'desktop-user');
        expect(InjectAIAssistantGuideReference).not.toHaveBeenCalled();
        expect(messageContents(result.current.messages)).toContain('引导已注入下一轮：\n下一轮参考这个');
    });

    it('does not echo project guide references into the local desktop history', async () => {
        const { result } = renderAssistantHook();

        let accepted = false;
        await act(async () => {
            accepted = await result.current.guideLaunchReference('项目参考', 'desktop-user:D:/tasks/demo');
        });

        expect(accepted).toBe(true);
        expect(InjectAIAssistantGuideReferenceForSession).toHaveBeenCalledWith('项目参考', 'desktop-user:D:/tasks/demo');
        expect(messageContents(result.current.messages)).not.toContain('引导已注入下一轮：\n项目参考');
    });

    it('does not show guide reference echo when the active loop rejects it', async () => {
        (InjectAIAssistantGuideReferenceForSession as any).mockResolvedValueOnce(false);
        const { result } = renderAssistantHook();

        let accepted = true;
        await act(async () => {
            accepted = await result.current.guideLaunchReference('没有运行中的 loop');
        });

        expect(accepted).toBe(false);
        expect(messageContents(result.current.messages)).not.toContain('引导已注入下一轮：\n没有运行中的 loop');
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
            lang: 'en',
        });
    });

    it('keeps selected files scoped to the active session key', async () => {
        (SelectAIAssistantFiles as any)
            .mockResolvedValueOnce(['/tmp/project.png'])
            .mockResolvedValueOnce(['/tmp/local.txt']);

        const { result } = renderAssistantHook();

        await act(async () => {
            setActiveSessionKey('desktop-user:D:/tasks/files-project');
            await result.current.browseFile();
        });
        expect(result.current.selectedFilePaths).toEqual(['/tmp/project.png']);

        await act(async () => {
            setActiveSessionKey('desktop-user');
        });
        expect(result.current.selectedFilePaths).toEqual([]);

        await act(async () => {
            await result.current.browseFile();
        });
        expect(result.current.selectedFilePaths).toEqual(['/tmp/local.txt']);

        await act(async () => {
            setActiveSessionKey('desktop-user:D:/tasks/files-project');
        });
        expect(result.current.selectedFilePaths).toEqual(['/tmp/project.png']);
    });

    it('clears selected files when a project session is forgotten', async () => {
        (SelectAIAssistantFiles as any).mockResolvedValueOnce(['/tmp/project.png']);

        const { result } = renderAssistantHook();

        await act(async () => {
            setActiveSessionKey('desktop-user:D:/tasks/files-project');
            await result.current.browseFile();
        });
        expect(result.current.selectedFilePaths).toEqual(['/tmp/project.png']);

        act(() => {
            forgetAIAssistantSessionRounds('desktop-user:D:/tasks/files-project');
        });
        expect(result.current.selectedFilePaths).toEqual([]);

        act(() => {
            setActiveSessionKey('desktop-user');
            setActiveSessionKey('desktop-user:D:/tasks/files-project');
        });
        expect(result.current.selectedFilePaths).toEqual([]);
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
            expect(mockUIState.prompts).toEqual(['first prompt', 'second prompt']);
        });

        unmount();

        const { result: remounted } = renderAssistantHook();
        await waitFor(() => {
            expect(remounted.current.submittedPrompts).toEqual(['first prompt', 'second prompt']);
        });
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
            { id: 'a-browser', role: 'assistant', content: 'Browser: leaked browser instruction', reasoning: 'thinking\nBrowser: hidden tool echo', timestamp: 3 },
            { id: 'p-1', role: 'progress', content: 'skip me', timestamp: 3 },
            { id: 's-1', role: 'system', content: 'skip me too', timestamp: 4 },
        ];
        localStorage.setItem(AI_ASSISTANT_HISTORY_STORAGE_KEY, JSON.stringify(persistedMessages));

        const { result, unmount } = renderAssistantHook();
        expect(result.current.messages.map(m => m.id)).toEqual(['u-1', 'a-1', 'a-browser']);
        expect(result.current.messages.find(m => m.id === 'a-browser')?.content).toBe('leaked browser instruction');
        expect(result.current.messages.find(m => m.id === 'a-browser')?.reasoning).toBe('thinking');

        await act(async () => {
            await result.current.clearHistory();
        });

        expect(result.current.messages).toEqual([]);
        await waitFor(() => {
            expect(localStorage.getItem(AI_ASSISTANT_HISTORY_STORAGE_KEY)).toBeNull();
        });

        expect(ClearAIAssistantHistoryForSession).toHaveBeenCalledTimes(1);

        unmount();

        const { result: remounted } = renderAssistantHook();
        expect(remounted.current.messages).toEqual([]);
    });

    it('strips assistant protocol tool-call artifacts from legacy localStorage history', async () => {
        localStorage.setItem(AI_ASSISTANT_HISTORY_STORAGE_KEY, JSON.stringify([
            { id: 'u-tool', role: 'user', content: '<tool_call[]> keep as user text', timestamp: 1 },
            {
                id: 'a-tool',
                role: 'assistant',
                content: 'visible\n<details><summary>thinking</summary>hidden</details>\n<tool_call[]>\n{"name":"write_file","arguments":{"path":"a.txt","content":"x"}}',
                timestamp: 2,
            },
            {
                id: 'a-fn',
                role: 'assistant',
                content: '<|FunctionCallBegin|>{"name":"run_command","arguments":{"cmd":"pwd"}}<|FunctionCallEnd|>',
                timestamp: 3,
            },
        ]));

        const { result } = renderAssistantHook();

        expect(result.current.messages.find(m => m.id === 'u-tool')?.content).toBe('<tool_call[]> keep as user text');
        expect(result.current.messages.find(m => m.id === 'a-tool')?.content).toBe('visible');
        expect(result.current.messages.find(m => m.id === 'a-fn')?.content).toBe('');
    });

    it('keeps legacy localStorage history when backend UI-state migration fails', async () => {
        const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
        const persistedMessages = [
            { id: 'legacy-user', role: 'user', content: 'legacy question', timestamp: 1 },
        ];
        localStorage.setItem(AI_ASSISTANT_HISTORY_STORAGE_KEY, JSON.stringify(persistedMessages));
        (SaveAIAssistantUIState as any).mockRejectedValueOnce(new Error('disk full'));

        try {
            renderAssistantHook();

            await waitFor(() => {
                expect(SaveAIAssistantUIState).toHaveBeenCalled();
            });
            expect(localStorage.getItem(AI_ASSISTANT_HISTORY_STORAGE_KEY)).toBe(JSON.stringify(persistedMessages));
        } finally {
            errorSpy.mockRestore();
        }
    });

    it('merges legacy localStorage history when backend UI-state is partial', async () => {
        const persistedMessages = [
            { id: 'legacy-user', role: 'user', content: 'legacy question', timestamp: 1 },
        ];
        mockUIState = { messages: [], prompts: ['backend prompt'] };
        localStorage.setItem(AI_ASSISTANT_HISTORY_STORAGE_KEY, JSON.stringify(persistedMessages));

        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(result.current.messages.map(m => m.content)).toEqual(['legacy question']);
            expect(result.current.submittedPrompts).toEqual(['backend prompt']);
            expect(mockUIState.messages?.map((m: any) => m.content)).toEqual(['legacy question']);
            expect(localStorage.getItem(AI_ASSISTANT_HISTORY_STORAGE_KEY)).toBeNull();
        });
    });

    it('serializes backend UI-state writes so slow saves cannot overwrite newer state', async () => {
        const firstSave = deferred<void>();
        let firstPayload: any = null;
        const laterPayloads: any[] = [];
        (SaveAIAssistantUIState as any)
            .mockImplementationOnce(async (state: any) => {
                firstPayload = state;
                await firstSave.promise;
                mockUIState = state;
            })
            .mockImplementation(async (state: any) => {
                laterPayloads.push(state);
                mockUIState = state;
            });

        const { result } = renderAssistantHook();
        await waitFor(() => expect(LoadAIAssistantUIState).toHaveBeenCalled());

        await act(async () => {
            result.current.recordSubmittedPrompt('first prompt');
        });

        await waitFor(() => expect(SaveAIAssistantUIState).toHaveBeenCalledTimes(1));
        expect(firstPayload.prompts).toEqual(['first prompt']);

        await act(async () => {
            result.current.recordSubmittedPrompt('second prompt');
        });

        expect(SaveAIAssistantUIState).toHaveBeenCalledTimes(1);

        await act(async () => {
            firstSave.resolve();
            await firstSave.promise;
        });

        await waitFor(() => expect(SaveAIAssistantUIState).toHaveBeenCalledTimes(2));
        expect(laterPayloads.at(-1)?.prompts).toEqual(['first prompt', 'second prompt']);
        expect(mockUIState.prompts).toEqual(['first prompt', 'second prompt']);
    });

    it('does not mark failed backend UI-state saves as persisted', async () => {
        const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
        (SaveAIAssistantUIState as any)
            .mockRejectedValueOnce(new Error('temporary disk error'))
            .mockImplementation(async (state: any) => { mockUIState = state; });

        try {
            const { result, unmount } = renderAssistantHook();
            await waitFor(() => expect(LoadAIAssistantUIState).toHaveBeenCalled());

            await act(async () => {
                result.current.recordSubmittedPrompt('retry me');
            });

            await waitFor(() => expect(SaveAIAssistantUIState).toHaveBeenCalledTimes(1));
            expect(mockUIState.prompts).toEqual([]);

            unmount();

            await waitFor(() => expect(SaveAIAssistantUIState).toHaveBeenCalledTimes(2));
            expect(mockUIState.prompts).toEqual(['retry me']);
        } finally {
            errorSpy.mockRestore();
        }
    });

    it('clearHistory clears messages but preserves submitted prompt history', async () => {
        const { result, unmount } = renderAssistantHook();
        await waitFor(() => expect(LoadAIAssistantUIState).toHaveBeenCalled());

        await act(async () => {
            result.current.recordSubmittedPrompt('keep prompt');
            await result.current.sendMessage('clearable message');
        });

        await waitFor(() => expect(mockUIState.prompts).toEqual(['keep prompt']));

        await act(async () => {
            await result.current.clearHistory();
        });

        expect(result.current.messages).toEqual([]);
        await waitFor(() => {
            expect(mockUIState.messages).toEqual([]);
            expect(mockUIState.prompts).toEqual(['keep prompt']);
        });

        unmount();

        const { result: remounted } = renderAssistantHook();
        await waitFor(() => {
            expect(remounted.current.messages).toEqual([]);
            expect(remounted.current.submittedPrompts).toEqual(['keep prompt']);
        });
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
        await waitFor(() => {
            expect(mockUIState.messages?.length).toBeGreaterThan(0);
        });

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

        expect(messageContents(result.current.messages)).toEqual([]);
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
        const saveStateSpy = SaveAIAssistantUIState as any;
        const removeItemSpy = vi.spyOn(Storage.prototype, 'removeItem');
        const { result, rerender } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('persist once');
        });

        await waitFor(() => {
            expect(saveStateSpy).toHaveBeenCalled();
        });
        const writesAfterSend = saveStateSpy.mock.calls.length;
        const removesAfterSend = removeItemSpy.mock.calls.length;

        rerender();

        await act(async () => {
            await Promise.resolve();
        });

        expect(saveStateSpy.mock.calls.length).toBe(writesAfterSend);
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

    it('deduplicates cumulative stream snapshots for the active assistant message', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('stream cumulative snapshots');
        });

        const first = '第1步：修改 step2b_detect_nontext_pii 的 prompt，增加 bbox 坐标请求。';
        const second = `${first}\n\n修改1：扩展 is_fa_fp 为更通用的非文字检测标识。`;
        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round', requestEvent());
            emitRuntimeEvent('ai-assistant-token', requestEvent(first));
            emitRuntimeEvent('ai-assistant-token', requestEvent(second));
            emitRuntimeEvent('ai-assistant-stream-done', requestEvent());
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe(second);
        await waitFor(() => {
            const savedState = (SaveAIAssistantUIState as any).mock.calls
                .map(([state]: [any]) => state)
                .find((state: any) => state?.messages?.some((message: any) => message.role === 'assistant' && message.content === second));
            expect(savedState).toBeTruthy();
            expect(JSON.stringify(savedState)).not.toContain('streamSnapshotMode');
            expect(JSON.stringify(savedState)).not.toContain('reasoningStreamSnapshotMode');
        });

        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null });
            await pending.promise;
        });
    });

	it('drops repeated full snapshots after snapshot mode is detected', async () => {
		const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
		(SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('stream repeated snapshots');
        });

        const first = 'Step 1: update the prompt to request bbox coordinates.';
        const second = `${first}\nStep 2: run the detector and verify outputs.`;
        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round', requestEvent());
            emitRuntimeEvent('ai-assistant-token', requestEvent(first));
            emitRuntimeEvent('ai-assistant-token', requestEvent(second));
            emitRuntimeEvent('ai-assistant-token', requestEvent(second));
            emitRuntimeEvent('ai-assistant-stream-done', requestEvent());
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe(second);

        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null });
            await pending.promise;
        });
    });

    it('keeps stream snapshot state aligned with role-prefix-stripped display text', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('stream role prefix snapshots');
        });

        const firstDisplay = 'Step 1: inspect the stream path.';
        const secondDisplay = `${firstDisplay}\nStep 2: normalize snapshots before buffering.`;
        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round', requestEvent());
            emitRuntimeEvent('ai-assistant-token', requestEvent(`Browser: ${firstDisplay}`));
            emitRuntimeEvent('ai-assistant-token', requestEvent(secondDisplay));
            emitRuntimeEvent('ai-assistant-stream-done', requestEvent());
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe(secondDisplay);

        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null });
            await pending.promise;
        });
    });

    it('keeps stream snapshot state aligned when role prefixes arrive split across tokens', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('stream split role prefix snapshots');
        });

        const firstDisplay = 'Step 1: inspect split role prefixes.';
        const secondDisplay = `${firstDisplay}\nStep 2: keep state aligned with display text.`;
        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round', requestEvent());
            emitRuntimeEvent('ai-assistant-token', requestEvent('Brow'));
            emitRuntimeEvent('ai-assistant-token', requestEvent(`ser: ${firstDisplay}`));
            emitRuntimeEvent('ai-assistant-token', requestEvent(secondDisplay));
            emitRuntimeEvent('ai-assistant-stream-done', requestEvent());
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe(secondDisplay);

        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null });
            await pending.promise;
        });
    });

    it('strips split role prefixes even without a following cumulative snapshot', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('stream split role prefix only');
        });

        const body = 'Step 1: render only the assistant-visible text.';
        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round', requestEvent());
            emitRuntimeEvent('ai-assistant-token', requestEvent('Brow'));
            emitRuntimeEvent('ai-assistant-token', requestEvent(`ser: ${body}`));
            emitRuntimeEvent('ai-assistant-stream-done', requestEvent());
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe(body);

        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null });
            await pending.promise;
        });
    });

    it('flushes pending stream buffer before resetting snapshot state on a new round', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('stream buffered snapshot across new round');
        });

        const first = 'A'.repeat(24);
        const second = `${first}B`;
        const third = `${second}C`;
        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round', requestEvent());
            emitRuntimeEvent('ai-assistant-token', requestEvent(first));
            emitRuntimeEvent('ai-assistant-token', requestEvent(second));
            emitRuntimeEvent('ai-assistant-new-round', requestEvent());
            emitRuntimeEvent('ai-assistant-token', requestEvent(third));
            emitRuntimeEvent('ai-assistant-stream-done', requestEvent());
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe(third);

        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null });
            await pending.promise;
        });
    });

    it('appends normalized buffered deltas without re-running snapshot overlap detection', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('stream buffered non-snapshot overlap');
        });

        const overlap = 'ABCDEFGHIJKLMNOPQRSTUVWX';
        const first = `Rendered prefix ${overlap}`;
        const second = overlap.slice(0, 12);
        const third = `${overlap.slice(12)} tail`;
        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round', requestEvent());
            emitRuntimeEvent('ai-assistant-token', requestEvent(first));
            emitRuntimeEvent('ai-assistant-token', requestEvent(second));
            emitRuntimeEvent('ai-assistant-token', requestEvent(third));
            emitRuntimeEvent('ai-assistant-stream-done', requestEvent());
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe(`${first}${second}${third}`);

        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null });
            await pending.promise;
        });
    });

    it('preserves intentionally repeated long streamed deltas', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('repeat stream text');
        });

        const repeated = 'Repeat this exact substantial sentence intentionally.\n';
        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round', requestEvent());
            emitRuntimeEvent('ai-assistant-token', requestEvent(repeated));
            emitRuntimeEvent('ai-assistant-token', requestEvent(repeated));
            emitRuntimeEvent('ai-assistant-stream-done', requestEvent());
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe(repeated + repeated);

        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null });
            await pending.promise;
        });
    });

    it('deduplicates unicode snapshot overlap without splitting emoji characters', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('stream emoji overlap');
        });

        const repeatedEmoji = '🧪'.repeat(24);
        const first = `Prefix ${repeatedEmoji}`;
        const second = `${repeatedEmoji} suffix`;
        await act(async () => {
            emitRuntimeEvent('ai-assistant-new-round', requestEvent());
            emitRuntimeEvent('ai-assistant-token', requestEvent(first));
            emitRuntimeEvent('ai-assistant-token', requestEvent(second));
            emitRuntimeEvent('ai-assistant-stream-done', requestEvent());
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe(`${first} suffix`);
        expect(assistantMessages(result.current.messages)[0].content).not.toContain('\uFFFD');

        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null });
            await pending.promise;
        });
    });

    it('keeps streamed thinking and appends final response reasoning', async () => {
        const pending = deferred<{ text: string; reasoning: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('show thinking');
        });

        await act(async () => {
            emitRuntimeEvent('ai-assistant-token', requestEvent('\x01先分析问题。'));
            emitRuntimeEvent('ai-assistant-token', requestEvent('最终答案'));
            emitRuntimeEvent('ai-assistant-stream-done', requestEvent());
        });

        expect(assistantMessages(result.current.messages)[0].content).toBe('最终答案');
        expect(assistantMessages(result.current.messages)[0].reasoning).toBe('先分析问题。');

        await act(async () => {
            pending.resolve({ text: '最终答案', reasoning: '再核对官方链路。', error: '', fields: null, actions: null });
            await pending.promise;
        });

        expect(assistantMessages(result.current.messages)[0].content).toBe('最终答案');
        expect(assistantMessages(result.current.messages)[0].reasoning).toBe('先分析问题。\n再核对官方链路。');
    });

    it('deduplicates final reasoning that already contains streamed thinking', async () => {
        const pending = deferred<{ text: string; reasoning: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('show thinking');
        });

        await act(async () => {
            emitRuntimeEvent('ai-assistant-token', requestEvent('\x01step one'));
            emitRuntimeEvent('ai-assistant-token', requestEvent('answer'));
        });

        await act(async () => {
            pending.resolve({ text: 'answer', reasoning: 'step one\nstep two', error: '', fields: null, actions: null });
            await pending.promise;
        });

        expect(assistantMessages(result.current.messages)[0].content).toBe('answer');
        expect(assistantMessages(result.current.messages)[0].reasoning).toBe('step one\nstep two');
    });

    it('does not deduplicate unrelated final reasoning just because it contains a short streamed word', async () => {
        const pending = deferred<{ text: string; reasoning: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('show thinking');
        });

        await act(async () => {
            emitRuntimeEvent('ai-assistant-token', requestEvent('\x01step'));
            emitRuntimeEvent('ai-assistant-token', requestEvent('answer'));
        });

        await act(async () => {
            pending.resolve({ text: 'answer', reasoning: 'next step', error: '', fields: null, actions: null });
            await pending.promise;
        });

        expect(assistantMessages(result.current.messages)[0].content).toBe('answer');
        expect(assistantMessages(result.current.messages)[0].reasoning).toBe('step\nnext step');
    });

    it('does not deduplicate a short streamed prefix of final reasoning', async () => {
        const pending = deferred<{ text: string; reasoning: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('show thinking');
        });

        await act(async () => {
            emitRuntimeEvent('ai-assistant-token', requestEvent('\x01step'));
            emitRuntimeEvent('ai-assistant-token', requestEvent('answer'));
        });

        await act(async () => {
            pending.resolve({ text: 'answer', reasoning: 'stepwise plan', error: '', fields: null, actions: null });
            await pending.promise;
        });

        expect(assistantMessages(result.current.messages)[0].content).toBe('answer');
        expect(assistantMessages(result.current.messages)[0].reasoning).toBe('step\nstepwise plan');
    });

    it('deduplicates a substantial streamed prefix of final reasoning', async () => {
        const pending = deferred<{ text: string; reasoning: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('show thinking');
        });

        await act(async () => {
            emitRuntimeEvent('ai-assistant-token', requestEvent('\x01step one'));
            emitRuntimeEvent('ai-assistant-token', requestEvent('answer'));
        });

        await act(async () => {
            pending.resolve({ text: 'answer', reasoning: 'step one and then step two', error: '', fields: null, actions: null });
            await pending.promise;
        });

        expect(assistantMessages(result.current.messages)[0].content).toBe('answer');
        expect(assistantMessages(result.current.messages)[0].reasoning).toBe('step one and then step two');
    });

    it('does not merge final reasoning on incidental one-character overlap', async () => {
        const pending = deferred<{ text: string; reasoning: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('show thinking');
        });

        await act(async () => {
            emitRuntimeEvent('ai-assistant-token', requestEvent('\x01streamed reasoning.'));
            emitRuntimeEvent('ai-assistant-token', requestEvent('answer'));
        });

        await act(async () => {
            pending.resolve({ text: 'answer', reasoning: '.final reasoning', error: '', fields: null, actions: null });
            await pending.promise;
        });

        expect(assistantMessages(result.current.messages)[0].content).toBe('answer');
        expect(assistantMessages(result.current.messages)[0].reasoning).toBe('streamed reasoning.\n.final reasoning');
    });

    it('keeps a final response that contains reasoning only', async () => {
        const pending = deferred<{ text: string; reasoning: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('reasoning only');
        });

        await act(async () => {
            pending.resolve({ text: '', reasoning: 'checking path', error: '', fields: null, actions: null });
            await pending.promise;
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe('');
        expect(assistantMessages(result.current.messages)[0].reasoning).toBe('checking path');
        await waitFor(() => {
            expect(mockUIState.messages.some((message: any) => message.content === '' && message.reasoning === 'checking path')).toBe(true);
        });
    });

    it('normalizes Go-style final Reasoning responses', async () => {
        const pending = deferred<{ Text: string; Reasoning: string; Error: string; Fields: null; Actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('go style reasoning');
        });

        await act(async () => {
            pending.resolve({ Text: '', Reasoning: 'go style thought', Error: '', Fields: null, Actions: null });
            await pending.promise;
        });

        expect(assistantMessages(result.current.messages)).toHaveLength(1);
        expect(assistantMessages(result.current.messages)[0].content).toBe('');
        expect(assistantMessages(result.current.messages)[0].reasoning).toBe('go style thought');
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

    it('treats stream-done as activity for the foreground timeout window', async () => {
        vi.useFakeTimers();
        try {
            const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
            (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

            const { result } = renderAssistantHook();

            await act(async () => {
                void result.current.sendMessage('long post-stream work');
                await Promise.resolve();
            });

            await act(async () => {
                emitRuntimeEvent('ai-assistant-new-round', requestEvent());
                emitRuntimeEvent('ai-assistant-token', requestEvent('partial'));
                await vi.advanceTimersByTimeAsync(239_000);
                emitRuntimeEvent('ai-assistant-stream-done', requestEvent());
                await vi.advanceTimersByTimeAsync(1_000);
            });

            expect(result.current.sending).toBe(true);
            expect(messageContents(result.current.messages).join('\n')).not.toContain('超时');

            await act(async () => {
                await vi.advanceTimersByTimeAsync(238_999);
            });

            expect(result.current.sending).toBe(true);
            expect(messageContents(result.current.messages).join('\n')).not.toContain('超时');

            await act(async () => {
                pending.resolve({ text: 'done', error: '', fields: null, actions: null });
                await pending.promise;
            });
        } finally {
            vi.useRealTimers();
        }
    });

    it('treats hidden backend heartbeats as activity even when they carry only session scope', async () => {
        await expectProgressPayloadTimeoutBehavior(
            { session_key: 'desktop-user', text: '__heartbeat__' },
            { expectAlive: true, prompt: 'long coding task' },
        );
    });

    it('normalizes legacy stream-event casing before applying timeout activity', async () => {
        await expectProgressPayloadTimeoutBehavior(
            (req: { request_id: string; text: string }) => ({ RequestID: req.request_id, SessionKey: 'desktop-user', Text: '__heartbeat__' }),
            { expectAlive: true, prompt: 'long legacy event task' },
        );
    });

    it('parses Wails JSON-string heartbeats before applying timeout activity', async () => {
        await expectProgressPayloadTimeoutBehavior(
            JSON.stringify({ session_key: 'desktop-user', text: '__heartbeat__' }),
            { expectAlive: true, prompt: 'long Wails event task' },
        );
    });

    it('does not let another session heartbeat reset the foreground timeout', async () => {
        await expectProgressPayloadTimeoutBehavior(
            { session_key: 'desktop-user:C:/other', text: '__heartbeat__' },
            { activeSessionKey: 'desktop-user:C:/work', expectAlive: false, prompt: 'long project task' },
        );
    });

    it('treats matching project-session heartbeats as foreground activity', async () => {
        await expectProgressPayloadTimeoutBehavior(
            { session_key: 'desktop-user:C:/work', text: '__heartbeat__' },
            { activeSessionKey: 'desktop-user:C:/work', expectAlive: true, prompt: 'long matching project task' },
        );
    });

    it('does not let a stale request heartbeat reset the active foreground timeout', async () => {
        await expectProgressPayloadTimeoutBehavior(
            { request_id: 'stale-request', session_key: 'desktop-user', text: '__heartbeat__' },
            { expectAlive: false },
        );
    });

    it('does not treat same-session progress without a request id as timeout activity', async () => {
        await expectProgressPayloadTimeoutBehavior(
            { session_key: 'desktop-user', text: 'running but unscoped' },
            { expectAlive: false },
        );
    });

    it('preserves streamed output when a foreground round times out', async () => {
        vi.useFakeTimers();
        try {
            const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
            (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

            const { result } = renderAssistantHook();

            await act(async () => {
                void result.current.sendMessage('long post-stream work');
                await Promise.resolve();
            });

            await act(async () => {
                emitRuntimeEvent('ai-assistant-new-round', requestEvent());
                emitRuntimeEvent('ai-assistant-token', requestEvent('partial output'));
                await vi.advanceTimersByTimeAsync(40);
                emitRuntimeEvent('ai-assistant-stream-done', requestEvent());
                await vi.advanceTimersByTimeAsync(600_001);
            });

            expect(result.current.sending).toBe(false);
            expect(messageContents(result.current.messages)).toContain('partial output');
            expect(messageContents(result.current.messages).join('\n')).toContain('请求超时');
            expect(assistantMessages(result.current.messages)[0].content).toBe('partial output');
        } finally {
            vi.useRealTimers();
        }
    });

    it('uses configured foreground response timeout from config', async () => {
        vi.useFakeTimers();
        try {
            (LoadConfig as any).mockResolvedValueOnce(new main.AppConfig({
                show_ai_trace_entry: false,
                trial_reflect_enabled: false,
                agent_response_timeout_sec: 600,
            }));
            const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
            (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

            const { result } = renderAssistantHook();

            await act(async () => {
                await Promise.resolve();
                await Promise.resolve();
            });
            expect(LoadConfig).toHaveBeenCalled();

            await act(async () => {
                void result.current.sendMessage('honor configured timeout');
                await Promise.resolve();
            });

            await act(async () => {
                await vi.advanceTimersByTimeAsync(240_001);
            });

            expect(result.current.sending).toBe(true);
            expect(messageContents(result.current.messages).join('\n')).not.toContain('请求超时');

            await act(async () => {
                await vi.advanceTimersByTimeAsync(360_000);
            });

            expect(result.current.sending).toBe(false);
            expect(messageContents(result.current.messages).join('\n')).toContain('600秒无响应');
        } finally {
            vi.useRealTimers();
        }
    });

    it('uses locally saved foreground response timeout without waiting for backend event', async () => {
        vi.useFakeTimers();
        try {
            const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
            (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

            const { result } = renderAssistantHook();

            await act(async () => {
                await Promise.resolve();
                window.dispatchEvent(new CustomEvent('maclaw-config-changed', {
                    detail: new main.AppConfig({ agent_response_timeout_sec: 600 }),
                }));
                await Promise.resolve();
            });

            await act(async () => {
                void result.current.sendMessage('honor locally saved timeout');
                await Promise.resolve();
            });

            await act(async () => {
                await vi.advanceTimersByTimeAsync(240_001);
            });

            expect(result.current.sending).toBe(true);
            expect(messageContents(result.current.messages).join('\n')).not.toContain('璇锋眰瓒呮椂');

            await act(async () => {
                await vi.advanceTimersByTimeAsync(360_000);
            });

            expect(result.current.sending).toBe(false);
            expect(messageContents(result.current.messages).join('\n')).toContain('600秒无响应');
        } finally {
            vi.useRealTimers();
        }
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

    it('does not turn an active remote AI task into a foreground response timeout', async () => {
        vi.useFakeTimers();
        try {
            mockSendResponse = {
                text: 'remote task is still running',
                error: '',
                fields: null,
                actions: null,
                request_id: 'req-default',
                deferred: true,
                run_id: 'run-active-timeout',
                job_id: 'job-active-timeout',
            };
            (ListRemoteSessions as any).mockResolvedValue([
                {
                    id: 'sess-active-timeout',
                    launch_source: 'ai',
                    status: 'busy',
                    run_id: 'run-active-timeout',
                    job_id: 'job-active-timeout',
                },
            ]);

            const { result } = renderAssistantHook();

            await act(async () => {
                await result.current.sendMessage('long remote task');
            });

            expect(result.current.sending).toBe(true);

            await act(async () => {
                await vi.advanceTimersByTimeAsync(600_001);
            });

            expect(result.current.sending).toBe(true);
            expect(messageContents(result.current.messages).join('\n')).not.toContain('超时');
        } finally {
            vi.useRealTimers();
        }
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

    it('clears pending AI task progress when the tracked remote session exits', async () => {
        mockSendResponse = {
            text: 'remote task is still running',
            error: '',
            fields: null,
            actions: null,
            request_id: 'req-default',
            deferred: true,
            run_id: 'run-finished-progress',
            job_id: 'job-finished-progress',
        };
        (ListRemoteSessions as any)
            .mockResolvedValueOnce([
                {
                    id: 'sess-finished-progress',
                    launch_source: 'ai',
                    status: 'busy',
                    run_id: 'run-finished-progress',
                    job_id: 'job-finished-progress',
                },
            ])
            .mockResolvedValueOnce([
                {
                    id: 'sess-finished-progress',
                    launch_source: 'ai',
                    status: 'busy',
                    run_id: 'run-finished-progress',
                    job_id: 'job-finished-progress',
                },
            ])
            .mockResolvedValueOnce([
                {
                    id: 'sess-finished-progress',
                    launch_source: 'ai',
                    status: 'exited',
                    run_id: 'run-finished-progress',
                    job_id: 'job-finished-progress',
                },
            ]);

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('wait for exit progress');
        });

        await act(async () => {
            emitRuntimeEvent('ai-assistant-progress', { request_id: 'req-default', text: 'running remote task' });
        });
        expect(result.current.progressMessages).toHaveLength(1);

        await act(async () => {
            emitRuntimeEvent('remote-state-changed');
            await Promise.resolve();
        });

        await waitFor(() => {
            expect(result.current.sending).toBe(false);
            expect(result.current.progressMessages).toEqual([]);
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

    it.each([
        ['请求超时', 'partial output before timeout'],
        ['context deadline exceeded', 'partial output before deadline'],
        ['request time out', 'partial output before spaced timeout'],
    ])('preserves streamed partial content when backend returns timeout-like error %s', async (errorText, partialText) => {
        await expectStreamedPartialPreservedOnBackendError(errorText, partialText);
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
        (CancelAIAssistantSessionForSession as any).mockResolvedValueOnce('retry first request');

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
        expect(CancelAIAssistantSessionForSession).toHaveBeenCalledWith('desktop-user');
        expect(CancelAIAssistantSession).not.toHaveBeenCalled();
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

    it('cancelSession clears live progress and stops the deferred response timeout', async () => {
        vi.useFakeTimers();
        try {
            mockSendResponse = { deferred: true, text: '', error: '', fields: null, actions: null };
            (CancelAIAssistantSessionForSession as any).mockResolvedValueOnce('');
            const { result } = renderAssistantHook();

            await act(async () => {
                await result.current.sendMessage('cancel deferred request');
            });

            const req = requestEvent();
            await act(async () => {
                emitRuntimeEvent('ai-assistant-progress', { request_id: req.request_id, text: 'running task' });
            });
            expect(result.current.progressMessages).toHaveLength(1);

            await act(async () => {
                await result.current.cancelSession();
            });

            expect(result.current.sending).toBe(false);
            expect(result.current.progressMessages).toEqual([]);

            await act(async () => {
                await vi.advanceTimersByTimeAsync(240_001);
            });

            expect(messageContents(result.current.messages).join('\n')).not.toContain('请求超时');
            expect(result.current.sending).toBe(false);
        } finally {
            vi.useRealTimers();
        }
    });

    it('sendMessage waits for backend cancel before starting next foreground request', async () => {
        const first = deferred<{ text: string; error: string; fields: null; actions: null }>();
        const cancel = deferred<string>();
        const second = deferred<{ text: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any)
            .mockImplementationOnce(() => first.promise)
            .mockImplementationOnce(() => second.promise);
        (CancelAIAssistantSessionForSession as any).mockImplementationOnce(() => cancel.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('first request');
            await Promise.resolve();
        });

        await act(async () => {
            void result.current.cancelSession();
            await Promise.resolve();
        });

        await act(async () => {
            void result.current.sendMessage('second request');
            await Promise.resolve();
            await Promise.resolve();
        });

        expect(CancelAIAssistantSessionForSession).toHaveBeenCalledWith('desktop-user');
        expect(CancelAIAssistantSession).not.toHaveBeenCalled();
        expect(SendAIAssistantMessage).toHaveBeenCalledTimes(1);

        await act(async () => {
            cancel.resolve('first request');
            await cancel.promise;
            await Promise.resolve();
        });

        expect(SendAIAssistantMessage).toHaveBeenCalledTimes(2);

        await act(async () => {
            second.resolve({ text: 'fresh reply', error: '', fields: null, actions: null });
            await second.promise;
        });

        await act(async () => {
            first.resolve({ text: 'stale reply', error: '', fields: null, actions: null });
            await first.promise;
        });

        expect(messageContents(result.current.messages)).toContain('second request');
        expect(messageContents(result.current.messages)).toContain('fresh reply');
        expect(messageContents(result.current.messages)).not.toContain('stale reply');
    });

    it('cancelSession targets the active project session key', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);
        (CancelAIAssistantSessionForSession as any).mockResolvedValueOnce('project request');

        const { result } = renderHook(() => useAIAssistant({ activeSessionKey: 'desktop-user:D:/work/project' }));

        await act(async () => {
            void result.current.sendMessage('project request', { project_path: 'D:/work/project' });
            await Promise.resolve();
        });

        await act(async () => {
            await result.current.cancelSession();
        });

        expect(CancelAIAssistantSessionForSession).toHaveBeenCalledWith('desktop-user:D:/work/project');
        expect(CancelAIAssistantSession).not.toHaveBeenCalled();

        await act(async () => {
            pending.resolve({ text: 'stale project reply', error: '', fields: null, actions: null });
            await pending.promise;
        });
    });

    it('stale canceled requests do not clear the next round response timeout controller', async () => {
        vi.useFakeTimers();
        try {
            const first = deferred<{ text: string; error: string; fields: null; actions: null; request_id: string; deferred: boolean }>();
            const second = deferred<{ text: string; error: string; fields: null; actions: null; request_id: string; deferred: boolean }>();
            (SendAIAssistantMessage as any)
                .mockImplementationOnce(() => first.promise)
                .mockImplementationOnce(() => second.promise);
            (CancelAIAssistantSessionForSession as any).mockResolvedValueOnce('');

            const { result } = renderAssistantHook();

            await act(async () => {
                void result.current.sendMessage('first slow request');
                await Promise.resolve();
            });

            await act(async () => {
                await result.current.cancelSession();
            });

            await act(async () => {
                void result.current.sendMessage('second slow request');
                await Promise.resolve();
                await Promise.resolve();
            });

            expect(SendAIAssistantMessage).toHaveBeenCalledTimes(2);

            const secondReq = requestEvent();

            await act(async () => {
                first.resolve({ text: 'stale done', error: '', fields: null, actions: null, request_id: 'stale-request', deferred: false });
                await first.promise;
            });

            await act(async () => {
                await vi.advanceTimersByTimeAsync(60_000);
                emitRuntimeEvent('ai-assistant-progress', { request_id: secondReq.request_id, text: 'second still alive' });
            });

            await act(async () => {
                await vi.advanceTimersByTimeAsync(10_000);
            });

            expect(result.current.sending).toBe(true);
            expect(messageContents(result.current.messages).join('\n')).not.toContain('超时');

            await act(async () => {
                second.resolve({ text: '', error: '', fields: null, actions: null, request_id: secondReq.request_id || '', deferred: true });
                await second.promise;
                emitRuntimeEvent('ai-assistant-response', { request_id: secondReq.request_id || '', text: 'fresh done' });
            });
        } finally {
            vi.useRealTimers();
        }
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

    it('normalizes legacy unfinished_task payloads into assistant messages', async () => {
        mockSendResponse = {
            text: '\u68c0\u6d4b\u5230\u672a\u5b8c\u6210\u4efb\u52a1',
            error: '',
            unfinished_task: {
                slot_id: 'slot-task',
                title: '\u7ee7\u7eed\u4efb\u52a1',
                status: 'interrupted',
                actions: [
                    { label: '\u7ee7\u7eed\u4e0a\u6b21\u4efb\u52a1', command: '__resume_unfinished__ slot-task', style: 'default' },
                ],
            },
        };

        const { result } = renderAssistantHook({ lang: 'zh-Hans' });

        await act(async () => {
            await result.current.sendMessage('\u7ee7\u7eed');
        });

        const assistantMsg = result.current.messages.find(m => m.role === 'assistant');
        expect(assistantMsg?.unfinishedSlot).toEqual({
            slotID: 'slot-task',
            title: '\u7ee7\u7eed\u4efb\u52a1',
            summary: undefined,
            projectPath: undefined,
            status: 'interrupted',
            actions: [
                { label: '\u7ee7\u7eed\u4e0a\u6b21\u4efb\u52a1', command: '__resume_unfinished__ slot-task', style: 'default' },
            ],
        });
    });

    it('normalizes structured recoverable session payloads into assistant messages', async () => {
        mockSendResponse = {
            text: '',
            error: '',
            recoverable_session: {
                session_id: 'sess-1',
                tool: 'claude',
                title: '\u7ee7\u7eed Daily Paper',
                summary: '\u8fd8\u5dee\u6700\u540e\u4e00\u8f6e\u6574\u7406',
                project_path: 'D:/work/project',
                status: 'exited',
                exit_reason: 'token_limit',
                resume_session_id: 'resume-123',
                resume_count: 2,
                last_progress: '\u8fd8\u5dee\u6700\u540e\u4e00\u8f6e\u6574\u7406',
                actions: [
                    { label: '\u6062\u590d\u4f1a\u8bdd', command: '__resume_session__ sess-1', style: 'default' },
                    { label: '\u5ffd\u7565\u4f1a\u8bdd', command: '__dismiss_recoverable_session__ sess-1', style: 'danger' },
                ],
            },
        };

        const { result } = renderAssistantHook({ lang: 'zh-Hans' });

        await act(async () => {
            await result.current.sendMessage('\u7ee7\u7eed');
        });

        const assistantMsg = result.current.messages.find(m => m.role === 'assistant');
        expect(assistantMsg?.content).toBe('');
        expect(assistantMsg?.recoverableSession).toEqual({
            sessionID: 'sess-1',
            tool: 'claude',
            title: '\u7ee7\u7eed Daily Paper',
            summary: '\u8fd8\u5dee\u6700\u540e\u4e00\u8f6e\u6574\u7406',
            projectPath: 'D:/work/project',
            status: 'exited',
            exitReason: 'token_limit',
            resumeSessionID: 'resume-123',
            resumeCount: 2,
            lastProgress: '\u8fd8\u5dee\u6700\u540e\u4e00\u8f6e\u6574\u7406',
            actions: [
                { label: '\u6062\u590d\u4f1a\u8bdd', command: '__resume_session__ sess-1', style: 'default' },
                { label: '\u5ffd\u7565\u4f1a\u8bdd', command: '__dismiss_recoverable_session__ sess-1', style: 'danger' },
            ],
        });

        await waitFor(() => {
            expect(mockUIState.messages?.some((m: any) => m.recoverableSession?.sessionID === 'sess-1')).toBe(true);
        });

        mockSendResponse = { text: 'ok', error: '' };
        await act(async () => {
            await result.current.executeAction('__resume_session__ sess-1');
        });

        const updatedRecoverableMsg = result.current.messages.find(m => m.recoverableSession?.sessionID === 'sess-1');
        expect(updatedRecoverableMsg?.recoverableSession?.actions).toEqual([
            { label: '\u5ffd\u7565\u4f1a\u8bdd', command: '__dismiss_recoverable_session__ sess-1', style: 'danger' },
        ]);
    });

    it('sends persisted structured task handoff cards as concise follow-up context', async () => {
        (SendAIAssistantMessage as any)
            .mockImplementationOnce(async (req: any) => ({
                text: '',
                error: '',
                request_id: req.request_id,
                recoverable_session: {
                    session_id: 'sess-context',
                    title: '\u7ee7\u7eed Daily Paper',
                    summary: '\u8fd8\u5dee\u6700\u540e\u4e00\u8f6e\u6574\u7406',
                    project_path: 'D:/work/project',
                    status: 'exited',
                },
            }))
            .mockImplementationOnce(async (req: any) => ({ text: 'ok', error: '', request_id: req.request_id }));

        const { result } = renderAssistantHook({ lang: 'zh-Hans' });

        await act(async () => {
            await result.current.sendMessage('\u7ee7\u7eed');
        });
        await act(async () => {
            await result.current.sendMessage('\u8865\u5145\u4e00\u4e0b');
        });

        const followRequest = parseSentRequest(1) as any;
        expect(followRequest.recent_messages?.map((m: any) => m.content)).toEqual([
            '\u7ee7\u7eed',
            expect.stringContaining('Assistant showed recoverable session card'),
        ]);
        expect(followRequest.recent_messages?.[1]?.content).toContain('title=\u7ee7\u7eed Daily Paper');
        expect(followRequest.recent_messages?.[1]?.content).toContain('progress=\u8fd8\u5dee\u6700\u540e\u4e00\u8f6e\u6574\u7406');
    });

    it('executeAction routes explicit unfinished slot commands through sendMessage options', async () => {
        const calls: any[] = [];
        (SendAIAssistantMessage as any).mockImplementation(async (req: any) => {
            calls.push(req);
            return { text: 'ok', error: '', fields: null, actions: null, request_id: req.request_id || 'req' };
        });

        const { result } = renderAssistantHook({ lang: 'zh-Hans' });

        await act(async () => {
            await result.current.executeAction('__resume_unfinished__ slot-99');
            await result.current.executeAction('__start_new_task__');
            await result.current.executeAction('__dismiss_unfinished__ slot-99');
            await result.current.executeAction('__resume_session__ sess-99');
            await result.current.executeAction('__dismiss_recoverable_session__ sess-99');
        });

        expect(calls[0]?.resume_slot_id).toBe('slot-99');
        expect(calls[0]?.ui_action).toBe(true);
        expect(calls[0]?.text).toBe('\u7ee7\u7eed\u4e0a\u6b21\u672a\u5b8c\u6210\u4efb\u52a1');
        expect(calls[0]?.lang).toBe('zh-Hans');
        expect(calls[1]?.start_new_task).toBe(true);
        expect(calls[1]?.text).toBe('\u5f00\u59cb\u4e00\u4e2a\u65b0\u4efb\u52a1');
        expect(calls[1]?.lang).toBe('zh-Hans');
        expect(calls[2]?.dismiss_slot_id).toBe('slot-99');
        expect(calls[2]?.start_new_task).toBe(true);
        expect(calls[2]?.text).toBe('\u5ffd\u7565\u4e0a\u6b21\u672a\u5b8c\u6210\u4efb\u52a1');
        expect(calls[2]?.lang).toBe('zh-Hans');
        expect(calls[3]?.resume_session_id).toBe('sess-99');
        expect(calls[3]?.ui_action).toBe(true);
        expect(calls[3]?.text).toBe('\u6062\u590d\u4f1a\u8bdd');
        expect(calls[3]?.lang).toBe('zh-Hans');
        expect(calls[4]?.dismiss_recoverable_session_id).toBe('sess-99');
        expect(calls[4]?.ui_action).toBe(true);
        expect(calls[4]?.text).toBe('\u5ffd\u7565\u4f1a\u8bdd');
        expect(calls[4]?.lang).toBe('zh-Hans');
        const userMessages = result.current.messages.filter(m => m.role === 'user');
        expect(userMessages[0]?.content).toBe('\u7ee7\u7eed\u4e0a\u6b21\u672a\u5b8c\u6210\u4efb\u52a1');
        expect(userMessages[2]?.content).toBe('\u5ffd\u7565\u4e0a\u6b21\u672a\u5b8c\u6210\u4efb\u52a1');
        expect(userMessages[3]?.content).toBe('\u6062\u590d\u4f1a\u8bdd');
        expect(userMessages[4]?.content).toBe('\u5ffd\u7565\u4f1a\u8bdd');
    });

    it('executeAction keeps project tab action commands on the active project session', async () => {
        const calls: any[] = [];
        (SendAIAssistantMessage as any).mockImplementation(async (req: any) => {
            calls.push(req);
            return { text: 'ok', error: '', fields: null, actions: null, request_id: req.request_id || 'req' };
        });

        const { result } = renderAssistantHook({ activeSessionKey: 'desktop-user:D:/tasks/action-project', lang: 'zh-Hans' });

        await act(async () => {
            await result.current.executeAction('__resume_unfinished__ slot-project');
            await result.current.executeAction('__dismiss_recoverable_session__ sess-project');
            await result.current.executeAction('plain project action');
        });

        expect(calls[0]?.project_path).toBe('D:/tasks/action-project');
        expect(calls[0]?.resume_slot_id).toBe('slot-project');
        expect(calls[1]?.project_path).toBe('D:/tasks/action-project');
        expect(calls[1]?.dismiss_recoverable_session_id).toBe('sess-project');
        expect(calls[2]?.project_path).toBe('D:/tasks/action-project');
        expect(calls[2]?.ui_action).toBe(true);
        expect(calls[2]?.text).toBe('plain project action');
        expect(result.current.messages.filter(m => m.role === 'user').every(m => m.sessionKey === 'desktop-user:D:/tasks/action-project')).toBe(true);
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
                Labels: {
                    Title: '执行前确认',
                    Status: '状态',
                    TargetPaths: '目标路径',
                    PlannedActions: '计划操作',
                    RiskFlags: '风险标记',
                    RevisionHints: '修订提示',
                },
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
            labels: {
                title: '执行前确认',
                status: '状态',
                target_paths: '目标路径',
                planned_actions: '计划操作',
                risk_flags: '风险标记',
                revision_hints: '修订提示',
            },
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

    it('uses localized structured confirmation action labels as the user echo', async () => {
        mockSendResponse = {
            text: '请先确认',
            error: '',
            fields: null,
            actions: [
                { label: 'Confirm and start', command: '__confirm_execution__ c1', style: 'primary' },
                { label: 'Cancel', command: '__cancel_execution__ c1', style: 'secondary' },
            ],
            confirmation: {
                id: 'c1',
                summary: '确认任务',
                task_type: 'coding',
                status: 'pending',
            },
        };

        const { result } = renderAssistantHook({ lang: 'zh-Hans' });

        await act(async () => {
            await result.current.sendMessage('修复登录问题');
        });

        mockSendResponse = { text: '已开始', error: '', fields: null, actions: null };

        await act(async () => {
            await result.current.executeAction('__confirm_execution__ c1');
        });

        const userMessages = result.current.messages.filter(m => m.role === 'user');
        expect(userMessages.at(-1)?.content).toBe('确认并开始');
        expect(parseSentRequest(1).text).toBe('__confirm_execution__ c1');
    });

    it('localizes legacy task panel fallback user echoes', async () => {
        (SubmitAgentView as any).mockImplementationOnce(async () => {
            throw new Error('legacy submit path');
        });
        (DismissAgentView as any).mockImplementationOnce(async () => {
            throw new Error('legacy dismiss path');
        });
        mockSendResponse = { text: 'ok', error: '', fields: null, actions: null };
        const { result } = renderAssistantHook({ lang: 'zh-Hans' });

        await act(async () => {
            await result.current.submitAgentView('plain-form', { value: 1 });
        });
        expect(result.current.messages.filter(m => m.role === 'user').at(-1)?.content).toBe('\u63d0\u4ea4\u7ed3\u6784\u5316\u6570\u636e');

        await act(async () => {
            await result.current.dismissAgentView('plain-form', { value: 1 });
        });
        expect(result.current.messages.filter(m => m.role === 'user').at(-1)?.content).toBe('\u5173\u95ed\u4efb\u52a1\u9762\u677f');
    });

    it('routes legacy task panel fallback injection to active desktop session', async () => {
        (SubmitAgentView as any).mockImplementationOnce(async () => {
            throw new Error('legacy submit path');
        });
        (InjectAIAssistantSupplementaryForSession as any).mockResolvedValueOnce(true);
        const { result } = renderAssistantHook({ activeSessionKey: 'desktop-user:D:/tasks/demo' });

        await act(async () => {
            await result.current.submitAgentView('plain-form', { value: 1 });
        });

        expect(InjectAIAssistantSupplementaryForSession).toHaveBeenCalledWith(
            expect.stringContaining('__agent_view_submit__'),
            'desktop-user:D:/tasks/demo',
        );
        expect(InjectAIAssistantSupplementary).not.toHaveBeenCalled();
        expect(SendAIAssistantMessage).not.toHaveBeenCalled();
    });

    it('normalizes action styles from assistant responses', async () => {
        const responseActions = [
            { label: '  Safe  ', command: '  safe-cmd  ', style: 'default' },
            { label: 'Approve', command: 'approve-cmd', style: 'primary' },
            { label: 'Later', command: 'later-cmd', style: 'secondary' },
            { label: 'Delete', command: 'danger-cmd', style: 'danger' },
            { Label: 'Pascal', Command: 'pascal-cmd', Style: 'primary' },
            { label: 'Fallback', command: 'fallback-cmd', style: 'warning' },
            { label: 'No command', style: 'primary' },
        ];
        mockSendResponse = { text: 'ok', error: '', fields: null, actions: responseActions };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('normalize actions');
        });

        const assistantMsg = result.current.messages.find(m => m.role === 'assistant');
        expect(assistantMsg?.actions).toEqual([
            { label: 'Safe', command: 'safe-cmd', style: 'default' },
            { label: 'Approve', command: 'approve-cmd', style: 'primary' },
            { label: 'Later', command: 'later-cmd', style: 'secondary' },
            { label: 'Delete', command: 'danger-cmd', style: 'danger' },
            { label: 'Pascal', command: 'pascal-cmd', style: 'primary' },
            { label: 'Fallback', command: 'fallback-cmd', style: 'default' },
        ] satisfies ChatAction[]);
    });

    it('drops empty action payloads after normalization', async () => {
        mockSendResponse = {
            text: 'ok',
            error: '',
            fields: null,
            actions: [
                { label: 'No command', style: 'primary' },
                { command: 'no-label', style: 'primary' },
                null,
            ],
        };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('normalize empty actions');
        });

        const assistantMsg = result.current.messages.find(m => m.role === 'assistant');
        expect(assistantMsg?.actions).toBeUndefined();
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
                { Label: 'Cache read tokens', Value: '96' },
                { Label: 'Cache write tokens', Value: '12' },
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
                { Label: 'Cache read tokens', Value: '96' },
                { Label: 'Cache write tokens', Value: '12' },
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
            { label: 'Cache read tokens', value: '96' },
            { label: 'Cache write tokens', value: '12' },
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
                { label: 'Cache read tokens', value: '96' },
                { label: 'Cache write tokens', value: '12' },
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
                { label: 'Cache read tokens', value: '96' },
                { label: 'Cache write tokens', value: '12' },
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
            { label: 'Cache read tokens', value: '96' },
            { label: 'Cache write tokens', value: '12' },
        ]);
    });

    it('builds token usage fields from numeric response counters when detail entry is enabled', async () => {
        (LoadConfig as any).mockResolvedValueOnce({ show_ai_trace_entry: true });
        mockSendResponse = {
            Text: 'done',
            Error: '',
            Fields: null,
            Actions: null,
            InputTokens: 120,
            OutputTokens: 30,
            TotalTokens: 150,
            CacheReadTokens: 96,
            CacheWriteTokens: 12,
        } as any;

        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(LoadConfig).toHaveBeenCalled();
            expect(result.current.messages).toEqual([]);
        });
        act(() => {
            emitRuntimeEvent('config-changed', new main.AppConfig({ show_ai_trace_entry: true, trial_reflect_enabled: false }));
        });

        await act(async () => {
            await result.current.sendMessage('show numeric token usage');
        });

        const assistantMsg = result.current.messages.find(m => m.role === 'assistant');
        expect(assistantMsg?.fields).toEqual([
            { label: 'Input tokens', value: '120' },
            { label: 'Output tokens', value: '30' },
            { label: 'Total tokens', value: '150' },
            { label: 'Cache read tokens', value: '96' },
            { label: 'Cache write tokens', value: '12' },
        ]);
    });

    it('hides numeric token usage counters when detail entry is disabled', async () => {
        mockSendResponse = {
            text: 'done',
            error: '',
            fields: null,
            actions: null,
            input_tokens: 120,
            output_tokens: 30,
            total_tokens: 150,
            cache_read_tokens: 96,
            cache_write_tokens: 12,
        };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('hide numeric token usage');
        });

        const assistantMsg = result.current.messages.find(m => m.role === 'assistant');
        expect(assistantMsg?.fields).toBeUndefined();
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
                icon: 'TIP',
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
        (CancelAIAssistantSessionForSession as any).mockResolvedValueOnce('retry first request');

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

    it('keeps AgentView state scoped to the active session key', async () => {
        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(runtimeHandlers.has('agent-view:lifecycle')).toBe(true);
        });

        act(() => {
            setActiveSessionKey('desktop-user:D:/tasks/agent-view-project');
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'open',
                view: {
                    id: 'workflow:form:project',
                    type: 'form',
                    title: 'Project form',
                    fields: [
                        { name: '_workflow_user_id', type: 'hidden', value: 'desktop-user:D:/tasks/agent-view-project' },
                    ],
                },
            });
        });
        expect(result.current.agentView?.id).toBe('workflow:form:project');

        act(() => {
            setActiveSessionKey('desktop-user');
        });
        expect(result.current.agentView).toBeNull();

        act(() => {
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'open',
                view: {
                    id: 'workflow:form:local',
                    type: 'form',
                    title: 'Local form',
                    fields: [
                        { name: '_workflow_user_id', type: 'hidden', value: 'desktop-user' },
                    ],
                },
            });
        });
        expect(result.current.agentView?.id).toBe('workflow:form:local');

        act(() => {
            setActiveSessionKey('desktop-user:D:/tasks/agent-view-project');
        });
        expect(result.current.agentView?.id).toBe('workflow:form:project');
    });

    it('mirrors workflow forms to the active task tab when the workflow owner session differs', async () => {
        const taskTabSession = 'desktop-user:C:/Users/ma139/.maclaw/data/tasks/word-123';
        const workflowOwnerSession = 'desktop-user:D:/real-working-dir';
        const pending = deferred<{ text: string; error: string; fields: null; actions: null; request_id: string }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);
        const { result } = renderAssistantHook({ activeSessionKey: taskTabSession });

        await waitFor(() => {
            expect(runtimeHandlers.has('agent-view:lifecycle')).toBe(true);
        });

        await act(async () => {
            void result.current.sendMessage('高考志愿申请');
            await Promise.resolve();
        });

        act(() => {
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'open',
                workflow_user_id: workflowOwnerSession,
                view: {
                    id: 'workflow:form:gaokao_profile',
                    type: 'form',
                    title: '高考志愿申请',
                    fields: [
                        { name: '_workflow_phase', type: 'hidden', value: 'gaokao_profile' },
                        { name: '_workflow_id', type: 'hidden', value: 'wf-gaokao' },
                        { name: '_workflow_user_id', type: 'hidden', value: workflowOwnerSession },
                        { name: 'region', label: '地区', type: 'text', value: '' },
                    ],
                },
            });
        });

        expect(result.current.agentView?.id).toBe('workflow:form:gaokao_profile');

        const sent = parseSentRequest();
        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null, request_id: sent.request_id || '' });
            await pending.promise;
        });
    });

    it('does not mirror non-workflow forms from a different owner session into the active task tab', async () => {
        const taskTabSession = 'desktop-user:C:/Users/ma139/.maclaw/data/tasks/word-123';
        const workflowOwnerSession = 'desktop-user:D:/real-working-dir';
        const pending = deferred<{ text: string; error: string; fields: null; actions: null; request_id: string }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);
        const { result } = renderAssistantHook({ activeSessionKey: taskTabSession });

        await waitFor(() => {
            expect(runtimeHandlers.has('agent-view:lifecycle')).toBe(true);
        });

        await act(async () => {
            void result.current.sendMessage('ordinary task');
            await Promise.resolve();
        });

        act(() => {
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'open',
                workflow_user_id: workflowOwnerSession,
                view: {
                    id: 'expense-form',
                    type: 'form',
                    title: 'Expense',
                    fields: [
                        { name: '_workflow_user_id', type: 'hidden', value: workflowOwnerSession },
                        { name: 'amount', label: 'Amount', type: 'number', value: 86 },
                    ],
                },
            });
        });

        expect(result.current.agentView).toBeNull();

        const sent = parseSentRequest();
        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null, request_id: sent.request_id || '' });
            await pending.promise;
        });
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

    it('ignores stale AgentView lifecycle close events for a newer view', async () => {
        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(runtimeHandlers.has('agent-view:lifecycle')).toBe(true);
        });

        act(() => {
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'open',
                view: { id: 'workflow:form:new', type: 'form', title: 'New', fields: [] },
            });
            emitRuntimeEvent('agent-view:lifecycle', { action: 'dismiss', view_id: 'workflow:form:old' });
            emitRuntimeEvent('agent-view:lifecycle', { action: 'complete', view_id: 'workflow:form:old' });
        });

        expect(result.current.agentView?.id).toBe('workflow:form:new');
    });

    it('ignores stale AgentView lifecycle open events by sequence', async () => {
        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(runtimeHandlers.has('agent-view:lifecycle')).toBe(true);
        });

        act(() => {
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'open',
                seq: 2,
                view: { id: 'workflow:form:new', type: 'form', title: 'New', fields: [] },
            });
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'open',
                seq: 1,
                view: { id: 'workflow:form:old', type: 'form', title: 'Old', fields: [] },
            });
        });

        expect(result.current.agentView?.id).toBe('workflow:form:new');
    });

    it('ignores stale lifecycle dismiss events by sequence even when workflow identity matches', async () => {
        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(runtimeHandlers.has('agent-view:lifecycle')).toBe(true);
        });

        act(() => {
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'open',
                seq: 2,
                view: {
                    id: 'workflow:form:requirements',
                    type: 'form',
                    title: 'Requirements',
                    fields: [
                        { name: '_workflow_phase', type: 'hidden', value: 'requirements' },
                        { name: '_workflow_id', type: 'hidden', value: 'wf-new' },
                        { name: '_workflow_user_id', type: 'hidden', value: 'desktop-user:C:/new' },
                    ],
                },
            });
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'dismiss',
                seq: 1,
                view_id: 'workflow:form:requirements',
                workflow_phase: 'requirements',
                workflow_id: 'wf-new',
                workflow_user_id: 'desktop-user:C:/new',
            });
        });

        expect(result.current.agentView?.id).toBe('workflow:form:requirements');
    });

    it('ignores stale workflow form close events with the same phase view id', async () => {
        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(runtimeHandlers.has('agent-view:lifecycle')).toBe(true);
        });

        act(() => {
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'open',
                view: {
                    id: 'workflow:form:requirements',
                    type: 'form',
                    title: 'Requirements',
                    fields: [
                        { name: '_workflow_phase', type: 'hidden', value: 'requirements' },
                        { name: '_workflow_id', type: 'hidden', value: 'wf-new' },
                        { name: '_workflow_user_id', type: 'hidden', value: 'desktop-user:C:/new' },
                    ],
                },
            });
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'dismiss',
                view_id: 'workflow:form:requirements',
                workflow_phase: 'requirements',
                workflow_id: 'wf-old',
                workflow_user_id: 'desktop-user:C:/old',
            });
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'complete',
                view_id: 'workflow:form:requirements',
                workflow_phase: 'requirements',
                workflow_id: 'wf-old',
                workflow_user_id: 'desktop-user:C:/old',
            });
        });

        expect(result.current.agentView?.id).toBe('workflow:form:requirements');
    });

    it('guards legacy AgentView clear events with workflow identity before lifecycle is active', async () => {
        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(runtimeHandlers.has('agent-view')).toBe(true);
            expect(runtimeHandlers.has('agent-view-clear')).toBe(true);
        });

        act(() => {
            emitRuntimeEvent('agent-view', {
                view: {
                    id: 'workflow:form:requirements',
                    type: 'form',
                    title: 'Requirements',
                    fields: [
                        { name: '_workflow_phase', type: 'hidden', value: 'requirements' },
                        { name: '_workflow_id', type: 'hidden', value: 'wf-new' },
                        { name: '_workflow_user_id', type: 'hidden', value: 'desktop-user:C:/new' },
                    ],
                },
            });
            emitRuntimeEvent('agent-view-clear', {
                view_id: 'workflow:form:requirements',
                workflow_phase: 'requirements',
                workflow_id: 'wf-old',
                workflow_user_id: 'desktop-user:C:/old',
            });
        });

        expect(result.current.agentView?.id).toBe('workflow:form:requirements');
    });

    it('ignores stale legacy AgentView open events by sequence before lifecycle is active', async () => {
        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(runtimeHandlers.has('agent-view')).toBe(true);
        });

        act(() => {
            emitRuntimeEvent('agent-view', {
                seq: 2,
                view: { id: 'workflow:form:new', type: 'form', title: 'New', fields: [] },
            });
            emitRuntimeEvent('agent-view', {
                seq: 1,
                view: { id: 'workflow:form:old', type: 'form', title: 'Old', fields: [] },
            });
        });

        expect(result.current.agentView?.id).toBe('workflow:form:new');
    });

    it('ignores stale legacy AgentView clear events by sequence before lifecycle is active', async () => {
        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(runtimeHandlers.has('agent-view')).toBe(true);
            expect(runtimeHandlers.has('agent-view-clear')).toBe(true);
        });

        act(() => {
            emitRuntimeEvent('agent-view', {
                seq: 2,
                view: {
                    id: 'workflow:form:requirements',
                    type: 'form',
                    title: 'Requirements',
                    fields: [
                        { name: '_workflow_phase', type: 'hidden', value: 'requirements' },
                        { name: '_workflow_id', type: 'hidden', value: 'wf-new' },
                        { name: '_workflow_user_id', type: 'hidden', value: 'desktop-user:C:/new' },
                    ],
                },
            });
            emitRuntimeEvent('agent-view-clear', {
                seq: 1,
                view_id: 'workflow:form:requirements',
                workflow_phase: 'requirements',
                workflow_id: 'wf-new',
                workflow_user_id: 'desktop-user:C:/new',
            });
        });

        expect(result.current.agentView?.id).toBe('workflow:form:requirements');
    });

    it('keeps workflow forms when old clear events omit workflow identity', async () => {
        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(runtimeHandlers.has('agent-view')).toBe(true);
            expect(runtimeHandlers.has('agent-view-clear')).toBe(true);
        });

        act(() => {
            emitRuntimeEvent('agent-view', {
                view: {
                    id: 'workflow:form:requirements',
                    type: 'form',
                    title: 'Requirements',
                    fields: [
                        { name: '_workflow_phase', type: 'hidden', value: 'requirements' },
                        { name: '_workflow_id', type: 'hidden', value: 'wf-new' },
                        { name: '_workflow_user_id', type: 'hidden', value: 'desktop-user:C:/new' },
                    ],
                },
            });
            emitRuntimeEvent('agent-view-clear', { view_id: 'workflow:form:requirements' });
        });

        expect(result.current.agentView?.id).toBe('workflow:form:requirements');
    });

    it('does not let malformed lifecycle events disable legacy AgentView events', async () => {
        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(runtimeHandlers.has('agent-view')).toBe(true);
            expect(runtimeHandlers.has('agent-view:lifecycle')).toBe(true);
        });

        act(() => {
            emitRuntimeEvent('agent-view:lifecycle', { action: 'unknown' });
            emitRuntimeEvent('agent-view', {
                view: { id: 'legacy-form', type: 'form', title: 'Legacy', fields: [] },
            });
        });

        expect(result.current.agentView?.id).toBe('legacy-form');
    });

    it('surfaces lifecycle AgentView errors as chat errors', async () => {
        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(runtimeHandlers.has('agent-view:lifecycle')).toBe(true);
        });

        act(() => {
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'error',
                view_id: 'expense-form',
                error: 'Amount must be greater than zero.',
            });
        });

        const last = result.current.messages[result.current.messages.length - 1];
        expect(last.role).toBe('error');
        expect(last.content).toBe('Amount must be greater than zero.');
    });

    it('keeps workflow forms visible when dismiss backend rejects', async () => {
        (DismissAgentView as any).mockImplementationOnce(async () => {
            throw new Error('save skipped phase form state: disk full');
        });
        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(runtimeHandlers.has('agent-view:lifecycle')).toBe(true);
        });

        act(() => {
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'open',
                view: {
                    id: 'workflow:form:requirements',
                    type: 'form',
                    title: 'Requirements',
                    fields: [
                        { name: '_workflow_phase', type: 'hidden', value: 'requirements' },
                        { name: '_workflow_id', type: 'hidden', value: 'wf-new' },
                        { name: '_workflow_user_id', type: 'hidden', value: 'desktop-user:C:/new' },
                    ],
                },
            });
        });

        await act(async () => {
            await result.current.dismissAgentView('workflow:form:requirements', {
                _workflow_phase: 'requirements',
                _workflow_id: 'wf-new',
                _workflow_user_id: 'desktop-user:C:/new',
            });
        });

        expect(result.current.agentView?.id).toBe('workflow:form:requirements');
        expect(messageContents(result.current.messages)).toContain('save skipped phase form state: disk full');
        expect(InjectAIAssistantSupplementary).not.toHaveBeenCalled();
    });

    it('clears workflow forms from dismiss lifecycle after backend accepts', async () => {
        (DismissAgentView as any).mockImplementationOnce(async () => {
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'dismiss',
                view_id: 'workflow:form:requirements',
                workflow_phase: 'requirements',
                workflow_id: 'wf-new',
                workflow_user_id: 'desktop-user:C:/new',
            });
            return { text: 'dismissed', error: '' };
        });
        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(runtimeHandlers.has('agent-view:lifecycle')).toBe(true);
        });

        act(() => {
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'open',
                view: {
                    id: 'workflow:form:requirements',
                    type: 'form',
                    title: 'Requirements',
                    fields: [
                        { name: '_workflow_phase', type: 'hidden', value: 'requirements' },
                        { name: '_workflow_id', type: 'hidden', value: 'wf-new' },
                        { name: '_workflow_user_id', type: 'hidden', value: 'desktop-user:C:/new' },
                    ],
                },
            });
        });

        await act(async () => {
            await result.current.dismissAgentView('workflow:form:requirements', {
                _workflow_phase: 'requirements',
                _workflow_id: 'wf-new',
                _workflow_user_id: 'desktop-user:C:/new',
            });
        });

        expect(result.current.agentView).toBeNull();
    });

    it('clearHistory dismisses workflow form agentView that normally survives dismiss', async () => {
        const { result } = renderAssistantHook();
        // Simulate a workflow form being opened
        await act(async () => {
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'open',
                seq: 10,
                session_key: 'desktop-user',
                view: { type: 'form', id: 'workflow:form:requirements', title: 'Info', fields: [{ name: 'goal', label: 'Goal' }] },
            });
        });
        expect(result.current.agentView).not.toBeNull();
        expect(result.current.agentView?.id).toBe('workflow:form:requirements');

        // Clear history — should unconditionally dismiss agentView
        await act(async () => {
            await result.current.clearHistory();
        });
        expect(result.current.agentView).toBeNull();

        // Stale event arriving after clear should NOT reopen the panel
        await act(async () => {
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'open',
                seq: 10, // same seq as before clear — stale
                session_key: 'desktop-user',
                view: { type: 'form', id: 'workflow:form:requirements', title: 'Info', fields: [{ name: 'goal', label: 'Goal' }] },
            });
        });
        expect(result.current.agentView).toBeNull();
    });

    it('opens a workflow form submit round before backend events arrive', async () => {
        const pending = deferred<{ text: string; error: string; request_id: string; deferred: boolean }>();
        (SubmitAgentView as any).mockImplementationOnce(async (payload: { request_id?: string }) => {
            emitRuntimeEvent('ai-assistant-token', { request_id: payload.request_id || '', session_key: 'desktop-user:C:/work', text: 'early token' });
            return pending.promise;
        });

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.submitAgentView('workflow:form:requirements', { project_name: 'snake' });
            await Promise.resolve();
        });

        await waitFor(() => expect(SubmitAgentView).toHaveBeenCalledTimes(1));
        const submitPayload = (SubmitAgentView as any).mock.calls[0][0] as { request_id?: string };
        expect(submitPayload.request_id).toMatch(/^desktop-ai-/);
        await waitFor(() => expect(messageContents(result.current.messages)).toContain('early token'));

        await act(async () => {
            pending.resolve({ text: '', error: '', request_id: submitPayload.request_id || '', deferred: true });
            await pending.promise;
        });

        await act(async () => {
            emitRuntimeEvent('ai-assistant-response', { request_id: submitPayload.request_id || '', text: '' });
        });

        await waitFor(() => expect(result.current.sending).toBe(false));
        expect(messageContents(result.current.messages)).toContain('early token');
    });

    it('does not wait for the previous workflow round to become idle before showing submit progress', async () => {
        const openingRound = deferred<{ text: string; error: string; request_id: string }>();
        const submitRound = deferred<{ text: string; error: string; request_id: string; deferred: boolean }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => openingRound.promise);
        (SubmitAgentView as any).mockImplementationOnce((payload: { request_id?: string }) => submitRound.promise.then(response => ({
            ...response,
            request_id: response.request_id || payload.request_id || '',
        })));

        const { result } = renderAssistantHook({ activeSessionKey: 'desktop-user:C:/work' });

        await act(async () => {
            void result.current.sendMessage('__workflow_choice__ complex wf-new', { project_path: 'C:/work' });
            await Promise.resolve();
        });
        await waitFor(() => expect(result.current.sending).toBe(true));

        await act(async () => {
            void result.current.submitAgentView('workflow:form:requirements', {
                _workflow_phase: 'requirements',
                _workflow_id: 'wf-new',
                _workflow_user_id: 'desktop-user:C:/work',
                project_name: 'snake',
            });
            await Promise.resolve();
        });

        await waitFor(() => expect(SubmitAgentView).toHaveBeenCalledTimes(1));
        const submitPayload = (SubmitAgentView as any).mock.calls[0][0] as { request_id?: string };
        expect(submitPayload.request_id).toMatch(/^desktop-ai-/);
        expect(result.current.busySessionKeys).toContain('desktop-user:C:/work');

        await act(async () => {
            openingRound.resolve({ text: '', error: '', request_id: parseSentRequest().request_id || '' });
            submitRound.resolve({ text: '', error: '', request_id: submitPayload.request_id || '', deferred: true });
            await Promise.all([openingRound.promise, submitRound.promise]);
        });
    });

    it('closes workflow form when structured submit is accepted and deferred', async () => {
        (SubmitAgentView as any).mockImplementationOnce(async (payload: { request_id?: string }) => ({
            text: '',
            error: '',
            request_id: payload.request_id || '',
            deferred: true,
        }));
        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(runtimeHandlers.has('agent-view:lifecycle')).toBe(true);
        });

        act(() => {
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'open',
                view: {
                    id: 'workflow:form:requirements',
                    type: 'form',
                    title: 'Requirements',
                    fields: [
                        { name: '_workflow_phase', type: 'hidden', value: 'requirements' },
                        { name: '_workflow_id', type: 'hidden', value: 'wf-new' },
                        { name: '_workflow_user_id', type: 'hidden', value: 'desktop-user:C:/new' },
                    ],
                },
            });
        });
        expect(result.current.agentView?.id).toBe('workflow:form:requirements');

        await act(async () => {
            await result.current.submitAgentView('workflow:form:requirements', {
                _workflow_phase: 'requirements',
                _workflow_id: 'wf-new',
                _workflow_user_id: 'desktop-user:C:/new',
            });
        });

        expect(result.current.agentView).toBeNull();
    });

    it('closes workflow form from active view owner when submitted data omits workflow user id', async () => {
        (SubmitAgentView as any).mockImplementationOnce(async (payload: { request_id?: string }) => ({
            text: '',
            error: '',
            request_id: payload.request_id || '',
            deferred: true,
        }));
        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(runtimeHandlers.has('agent-view:lifecycle')).toBe(true);
        });

        act(() => {
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'open',
                view: {
                    id: 'workflow:form:requirements',
                    type: 'form',
                    title: 'Requirements',
                    fields: [
                        { name: '_workflow_phase', type: 'hidden', value: 'requirements' },
                        { name: '_workflow_id', type: 'hidden', value: 'wf-new' },
                        { name: '_workflow_user_id', type: 'hidden', value: 'desktop-user:C:/new' },
                        { name: 'project_name', label: 'Project', type: 'text', value: 'snake' },
                    ],
                },
            });
        });
        expect(result.current.agentView?.id).toBe('workflow:form:requirements');

        await act(async () => {
            await result.current.submitAgentView('workflow:form:requirements', {
                project_name: 'snake',
            });
        });

        expect(result.current.agentView).toBeNull();
    });

    it('clears mirrored workflow forms from the active task tab after submit is accepted', async () => {
        const taskTabSession = 'desktop-user:C:/Users/ma139/.maclaw/data/tasks/word-123';
        const workflowOwnerSession = 'desktop-user:D:/real-working-dir';
        const pending = deferred<{ text: string; error: string; fields: null; actions: null; request_id: string }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);
        (SubmitAgentView as any).mockImplementationOnce(async (payload: { request_id?: string }) => ({
            text: '',
            error: '',
            request_id: payload.request_id || '',
            deferred: true,
        }));
        const { result } = renderAssistantHook({ activeSessionKey: taskTabSession });

        await waitFor(() => {
            expect(runtimeHandlers.has('agent-view:lifecycle')).toBe(true);
        });

        await act(async () => {
            void result.current.sendMessage('高考志愿申请');
            await Promise.resolve();
        });

        act(() => {
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'open',
                workflow_user_id: workflowOwnerSession,
                view: {
                    id: 'workflow:form:gaokao_profile',
                    type: 'form',
                    title: '高考志愿申请',
                    fields: [
                        { name: '_workflow_phase', type: 'hidden', value: 'gaokao_profile' },
                        { name: '_workflow_id', type: 'hidden', value: 'wf-gaokao' },
                        { name: '_workflow_user_id', type: 'hidden', value: workflowOwnerSession },
                        { name: 'region', label: '地区', type: 'text', value: '' },
                    ],
                },
            });
        });
        expect(result.current.agentView?.id).toBe('workflow:form:gaokao_profile');

        await act(async () => {
            await result.current.submitAgentView('workflow:form:gaokao_profile', {
                _workflow_phase: 'gaokao_profile',
                _workflow_id: 'wf-gaokao',
                _workflow_user_id: workflowOwnerSession,
                region: '福建',
            });
        });

        expect(result.current.agentView).toBeNull();

        const sent = parseSentRequest();
        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null, request_id: sent.request_id || '' });
            await pending.promise;
        });
    });

    it('uses workflow owner session for workflow form submit rounds', async () => {
        (SubmitAgentView as any).mockImplementationOnce(async (payload: { request_id?: string }) => ({
            text: '',
            error: '',
            request_id: payload.request_id || '',
            deferred: true,
        }));
        const { result } = renderAssistantHook({ activeSessionKey: 'desktop-user' });

        await act(async () => {
            await result.current.submitAgentView('workflow:form:requirements', {
                _workflow_phase: 'requirements',
                _workflow_id: 'wf-new',
                _workflow_user_id: 'desktop-user:C:/project',
                project_name: 'snake',
            });
        });

        const assistantMsg = result.current.messages.find(message => message.role === 'assistant' && message.requestId);
        expect(assistantMsg?.sessionKey).toBe('desktop-user:C:/project');
    });

    it('keeps workflow forms visible when submit backend rejects without lifecycle dismiss', async () => {
        (SubmitAgentView as any).mockImplementationOnce(async (payload: { request_id?: string }) => ({
            text: 'The workflow form phase is no longer current.',
            error: 'workflow phase field mismatch',
            request_id: payload.request_id || '',
            deferred: false,
        }));
        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(runtimeHandlers.has('agent-view:lifecycle')).toBe(true);
        });

        act(() => {
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'open',
                view: {
                    id: 'workflow:form:requirements',
                    type: 'form',
                    title: 'Requirements',
                    fields: [
                        { name: '_workflow_phase', type: 'hidden', value: 'requirements' },
                        { name: '_workflow_id', type: 'hidden', value: 'wf-new' },
                        { name: '_workflow_user_id', type: 'hidden', value: 'desktop-user:C:/new' },
                    ],
                },
            });
        });

        await act(async () => {
            await result.current.submitAgentView('workflow:form:requirements', {
                _workflow_phase: 'stale_phase',
                _workflow_id: 'wf-new',
                _workflow_user_id: 'desktop-user:C:/new',
            });
        });

        expect(result.current.agentView?.id).toBe('workflow:form:requirements');
    });

    it('does not fall back to synthetic workflow form submit when structured submit rejects', async () => {
        (SubmitAgentView as any).mockImplementationOnce(async () => {
            throw new Error('structured submit failed');
        });
        const { result } = renderAssistantHook();

        await waitFor(() => {
            expect(runtimeHandlers.has('agent-view:lifecycle')).toBe(true);
        });

        act(() => {
            emitRuntimeEvent('agent-view:lifecycle', {
                action: 'open',
                view: {
                    id: 'workflow:form:requirements',
                    type: 'form',
                    title: 'Requirements',
                    fields: [
                        { name: '_workflow_phase', type: 'hidden', value: 'requirements' },
                        { name: '_workflow_id', type: 'hidden', value: 'wf-new' },
                        { name: '_workflow_user_id', type: 'hidden', value: 'desktop-user:C:/new' },
                    ],
                },
            });
        });

        await act(async () => {
            await result.current.submitAgentView('workflow:form:requirements', {
                _workflow_phase: 'requirements',
                _workflow_id: 'wf-new',
                _workflow_user_id: 'desktop-user:C:/new',
            });
        });

        expect(result.current.agentView?.id).toBe('workflow:form:requirements');
        expect(messageContents(result.current.messages)).toContain('structured submit failed');
        expect(InjectAIAssistantSupplementary).not.toHaveBeenCalled();
    });

    it('stops workflow form submit timeout after final response', async () => {
        vi.useFakeTimers();
        try {
            const pending = deferred<{ text: string; error: string; request_id: string; deferred: boolean }>();
            (SubmitAgentView as any).mockImplementationOnce(async () => pending.promise);

            const { result } = renderAssistantHook();

            await act(async () => {
                void result.current.submitAgentView('workflow:form:requirements', { project_name: 'snake' });
                await Promise.resolve();
            });

            const submitPayload = (SubmitAgentView as any).mock.calls[0][0] as { request_id?: string };

            await act(async () => {
                pending.resolve({ text: '', error: '', request_id: submitPayload.request_id || '', deferred: true });
                await pending.promise;
            });

            await act(async () => {
                emitRuntimeEvent('ai-assistant-response', { request_id: submitPayload.request_id || '', text: 'done' });
                await Promise.resolve();
            });

            expect(result.current.sending).toBe(false);

            await act(async () => {
                vi.advanceTimersByTime(240_000);
            });

            expect(messageContents(result.current.messages).join('\n')).not.toContain('超时');
        } finally {
            vi.useRealTimers();
        }
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

    it('keeps Go-style artifact responses visible', async () => {
        mockSendResponse = {
            Text: '',
            Error: '',
            Fields: null,
            Actions: null,
            LocalFilePath: '/tmp/review.pdf',
            ResponseSource: ' File_Delivery ',
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

    it('keeps image_key screenshot responses visible', async () => {
        mockSendResponse = {
            text: '',
            error: '',
            fields: null,
            actions: null,
            image_key: 'screenshot-base64',
            response_source: 'agent_loop',
        };

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('take screenshot');
        });

        const assistantMsg = result.current.messages.find(m => m.role === 'assistant');
        expect(assistantMsg?.content).toBe('');
        expect(assistantMsg?.imageKey).toBe('screenshot-base64');
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

    it('deduplicates consecutive coding agent events that only differ by timestamp', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null; request_id: string; local_file_path?: string }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('track coding progress');
        });

        const req = requestEvent();
        await act(async () => {
            emitRuntimeEvent('ai-assistant-progress', { request_id: req.request_id, text: 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_started","phase":"running","task_id":"T0","title":"初始化 CMake 构建环境和依赖配置","turn_id":"turn-1","detail":"bash","ts":"2026-05-23T01:00:00Z"}' });
            emitRuntimeEvent('ai-assistant-progress', { request_id: req.request_id, text: 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_started","phase":"running","task_id":"T0","title":"初始化 CMake 构建环境和依赖配置","turn_id":"turn-1","detail":"bash","ts":"2026-05-23T01:00:01Z"}' });
            emitRuntimeEvent('ai-assistant-progress', { request_id: req.request_id, text: 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_started","phase":"running","task_id":"T0","title":"初始化 CMake 构建环境和依赖配置","turn_id":"turn-1","detail":"bash","ts":"2026-05-23T01:00:02Z"}' });
        });

        expect(result.current.progressMessages).toHaveLength(1);
        expect(result.current.progressMessages[0].content).toContain('"detail":"bash"');

        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null, request_id: req.request_id, local_file_path: '/tmp/review.pdf' });
            await pending.promise;
        });
    });

    it('caps live progress messages to the latest 30 entries', async () => {
        const pending = deferred<{ text: string; error: string; fields: null; actions: null; request_id: string; local_file_path?: string }>();
        (SendAIAssistantMessage as any).mockImplementationOnce(() => pending.promise);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('track many progress updates');
        });

        const req = requestEvent();
        await act(async () => {
            for (let i = 0; i < 35; i++) {
                emitRuntimeEvent('ai-assistant-progress', { request_id: req.request_id, text: `progress-${i}` });
            }
        });

        expect(result.current.progressMessages).toHaveLength(30);
        expect(result.current.progressMessages[0].content).toBe('progress-5');
        expect(result.current.progressMessages[29].content).toBe('progress-34');

        await act(async () => {
            pending.resolve({ text: '', error: '', fields: null, actions: null, request_id: req.request_id, local_file_path: '/tmp/review.pdf' });
            await pending.promise;
        });
    });

    it('clears live progress when a deferred response finalizes the active round', async () => {
        mockSendResponse = { deferred: true, text: '', error: '', fields: null, actions: null };
        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('track deferred progress');
        });

        const req = requestEvent();
        await act(async () => {
            emitRuntimeEvent('ai-assistant-progress', { request_id: req.request_id, text: 'running task' });
        });
        expect(result.current.sending).toBe(true);
        expect(result.current.progressMessages).toHaveLength(1);

        await act(async () => {
            emitRuntimeEvent('ai-assistant-response', { request_id: req.request_id, text: 'done', error: '' });
        });

        expect(result.current.sending).toBe(false);
        expect(result.current.progressMessages).toEqual([]);
    });

    it('keeps live progress scoped to the active session key', async () => {
        (SendAIAssistantMessage as any)
            .mockImplementationOnce(async (req: any) => ({ deferred: true, text: '', error: '', fields: null, actions: null, request_id: req.request_id }))
            .mockImplementationOnce(async (req: any) => ({ deferred: true, text: '', error: '', fields: null, actions: null, request_id: req.request_id }));
        const { result } = renderAssistantHook();

        await act(async () => {
            setActiveSessionKey('desktop-user:D:/tasks/progress-project');
            void result.current.sendMessage('project progress task', { project_path: 'D:/tasks/progress-project' });
            await Promise.resolve();
        });
        const projectReq = parseSentRequest(0);
        await act(async () => {
            emitRuntimeEvent('ai-assistant-progress', { request_id: projectReq.request_id, session_key: 'desktop-user:D:/tasks/progress-project', text: 'project progress' });
        });
        expect(result.current.progressMessages.map(m => m.content)).toEqual(['project progress']);

        await act(async () => {
            setActiveSessionKey('desktop-user');
        });
        expect(result.current.progressMessages).toEqual([]);

        await act(async () => {
            void result.current.sendMessage('local progress task');
            await Promise.resolve();
        });
        const localReq = parseSentRequest(1);
        await act(async () => {
            emitRuntimeEvent('ai-assistant-progress', { request_id: localReq.request_id, session_key: 'desktop-user', text: 'local progress' });
        });
        expect(result.current.progressMessages.map(m => m.content)).toEqual(['local progress']);

        await act(async () => {
            setActiveSessionKey('desktop-user:D:/tasks/progress-project');
        });
        expect(result.current.progressMessages.map(m => m.content)).toEqual(['project progress']);
    });

    it('ignores malformed deferred responses without the active request id', async () => {
        vi.useFakeTimers();
        try {
            mockSendResponse = { deferred: true, text: '', error: '', fields: null, actions: null };
            const { result } = renderAssistantHook();

            await act(async () => {
                await result.current.sendMessage('track malformed response');
            });

            const req = requestEvent();
            await act(async () => {
                emitRuntimeEvent('ai-assistant-progress', { request_id: req.request_id, text: 'running task' });
                emitRuntimeEvent('ai-assistant-response', { text: 'wrong terminal event', error: '' });
            });

            expect(result.current.sending).toBe(true);
            expect(messageContents(result.current.messages)).not.toContain('wrong terminal event');

            await act(async () => {
                await vi.advanceTimersByTimeAsync(600_001);
            });

            expect(result.current.sending).toBe(false);
            expect(result.current.progressMessages).toEqual([]);
        } finally {
            vi.useRealTimers();
        }
    });

    it('ignores deferred final responses with the wrong request id', async () => {
        mockSendResponse = { deferred: true, text: '', error: '', fields: null, actions: null };
        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('track wrong response id');
        });

        const req = requestEvent();
        await act(async () => {
            emitRuntimeEvent('ai-assistant-progress', { request_id: req.request_id, text: 'running task' });
            emitRuntimeEvent('ai-assistant-response', { request_id: 'other-request', text: 'wrong terminal event', error: '' });
        });

        expect(result.current.sending).toBe(true);
        expect(result.current.progressMessages).toHaveLength(1);
        expect(messageContents(result.current.messages)).not.toContain('wrong terminal event');

        await act(async () => {
            emitRuntimeEvent('ai-assistant-response', { request_id: req.request_id, text: 'right terminal event', error: '' });
        });

        expect(result.current.sending).toBe(false);
        expect(result.current.progressMessages).toEqual([]);
        expect(messageContents(result.current.messages)).toContain('right terminal event');
    });

    it('clears live progress when a deferred round times out', async () => {
        vi.useFakeTimers();
        mockSendResponse = { deferred: true, text: '', error: '', fields: null, actions: null };
        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('timeout deferred progress');
        });

        const req = requestEvent();
        await act(async () => {
            emitRuntimeEvent('ai-assistant-progress', { request_id: req.request_id, text: 'running task' });
        });
        expect(result.current.progressMessages).toHaveLength(1);

        await act(async () => {
            await vi.advanceTimersByTimeAsync(600_001);
        });

        expect(result.current.sending).toBe(false);
        expect(result.current.progressMessages).toEqual([]);
        vi.useRealTimers();
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

    it('does not queue local foreground send behind an active project tab send', async () => {
        (SendAIAssistantMessage as any)
            .mockImplementationOnce(async (req: { request_id?: string }) => ({
                text: '',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
                deferred: true,
            }))
            .mockImplementationOnce(async (req: { request_id?: string }) => ({
                text: 'local done',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
            }));

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('project keeps running', { project_path: 'D:/tasks/weather' });
            await Promise.resolve();
        });
        expect(SendAIAssistantMessage).toHaveBeenCalledTimes(1);

        await act(async () => {
            void result.current.sendMessage('local should enter agent');
            await Promise.resolve();
        });

        expect(SendAIAssistantMessage).toHaveBeenCalledTimes(2);
        expect((SendAIAssistantMessage as any).mock.calls[0][0]).toEqual(expect.objectContaining({
            text: 'project keeps running',
            project_path: 'D:/tasks/weather',
        }));
        expect((SendAIAssistantMessage as any).mock.calls[1][0]).toEqual(expect.objectContaining({
            text: 'local should enter agent',
        }));
        expect((SendAIAssistantMessage as any).mock.calls[1][0]).not.toHaveProperty('project_path');
        expect(messageContents(result.current.messages)).toContain('local done');

        await act(async () => {
            emitRuntimeEvent('ai-assistant-response', {
                text: 'project later',
                error: '',
                fields: null,
                actions: null,
                request_id: parseSentRequest(0).request_id || '',
            });
        });
        expect(messageContents(result.current.messages)).toContain('project later');
    });

    it('keeps detached project progress scoped when progress has only session key', async () => {
        (SendAIAssistantMessage as any)
            .mockImplementationOnce(async (req: { request_id?: string }) => ({
                text: '',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
                deferred: true,
            }))
            .mockImplementationOnce(async (req: { request_id?: string }) => ({
                text: 'local done',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
            }));

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('project keeps running', { project_path: 'D:/tasks/weather' });
            await Promise.resolve();
        });

        await act(async () => {
            void result.current.sendMessage('local should enter agent');
            await Promise.resolve();
        });

        await act(async () => {
            emitRuntimeEvent('ai-assistant-progress', {
                session_key: 'desktop-user:D:/tasks/weather',
                text: 'project tool still running',
            });
        });

        expect(result.current.progressMessages).toEqual([]);

        await act(async () => {
            setActiveSessionKey('desktop-user:D:/tasks/weather');
            await Promise.resolve();
        });

        expect(result.current.progressMessages).toHaveLength(1);
        expect(result.current.progressMessages[0].content).toBe('project tool still running');

        await act(async () => {
            emitRuntimeEvent('ai-assistant-response', {
                text: 'project later',
                error: '',
                fields: null,
                actions: null,
                request_id: parseSentRequest(0).request_id || '',
            });
        });
    });

    it('reports the session key that currently owns foreground busy state', async () => {
        (SendAIAssistantMessage as any).mockImplementationOnce(async (req: { request_id?: string }) => ({
            text: '',
            error: '',
            fields: null,
            actions: null,
            request_id: req.request_id || '',
            deferred: true,
        }));

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('project owns busy state', { project_path: 'D:/tasks/busy-owner' });
            await Promise.resolve();
        });

        expect(result.current.sending).toBe(true);
        expect(result.current.sendingSessionKey).toBe('desktop-user:D:/tasks/busy-owner');
        expect(result.current.busySessionKeys).toContain('desktop-user:D:/tasks/busy-owner');
        expect(result.current.panelState.sendingSessionKey).toBe('desktop-user:D:/tasks/busy-owner');
        expect(result.current.panelState.busySessionKeys).toContain('desktop-user:D:/tasks/busy-owner');
    });

    it('uses explicit project recent messages instead of local assistant history', async () => {
        localStorage.setItem(AI_ASSISTANT_HISTORY_STORAGE_KEY, JSON.stringify([
            { id: 'local-user', role: 'user', content: '南京天气', timestamp: 1 },
            { id: 'local-assistant', role: 'assistant', content: '南京本地天气结果', timestamp: 2 },
        ]));
        (SendAIAssistantMessage as any).mockImplementationOnce(async (req: { request_id?: string }) => ({
            text: 'project reply',
            error: '',
            fields: null,
            actions: null,
            request_id: req.request_id || '',
        }));

        const { result } = renderAssistantHook({ activeSessionKey: 'desktop-user:D:/tasks/beijing-weather' });

        await act(async () => {
            await result.current.sendMessage('成都天气', {
                project_path: 'D:/tasks/beijing-weather',
                recentMessages: [
                    { role: 'user', content: '北京天气' },
                    { role: 'assistant', content: '北京天气旧结果' },
                ],
            } as any);
        });

        const request = parseSentRequest(0) as any;
        expect(request.project_path).toBe('D:/tasks/beijing-weather');
        expect(request.recent_messages?.map((m: any) => m.content)).toEqual(['北京天气', '北京天气旧结果']);
        expect(request.recent_messages?.map((m: any) => m.content)).not.toContain('南京本地天气结果');
    });

    it('does not send completed project messages as local recent context', async () => {
        (SendAIAssistantMessage as any)
            .mockImplementationOnce(async (req: { request_id?: string }) => ({
                text: 'project answer should stay project scoped',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
            }))
            .mockImplementationOnce(async (req: { request_id?: string }) => ({
                text: 'local answer',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
            }));

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('project question', { project_path: 'D:/tasks/context-isolated', tabId: 'proj-context-isolated' } as any);
        });
        await act(async () => {
            await result.current.sendMessage('local question');
        });

        const localRequest = parseSentRequest(1) as any;
        const localContext = (localRequest.recent_messages || []).map((message: any) => message.content);
        expect(localContext).not.toContain('project question');
        expect(localContext).not.toContain('project answer should stay project scoped');
    });

    it('keeps detached project stream tokens after local foreground send takes over', async () => {
        (SendAIAssistantMessage as any)
            .mockImplementationOnce(async (req: { request_id?: string }) => ({
                text: '',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
                deferred: true,
            }))
            .mockImplementationOnce(async (req: { request_id?: string }) => ({
                text: 'local answer',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
            }));

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('project streams later', { project_path: 'D:/tasks/weather' });
            await Promise.resolve();
        });
        const projectReq = parseSentRequest(0);

        await act(async () => {
            void result.current.sendMessage('local takes over');
            await Promise.resolve();
        });
        expect(messageContents(result.current.messages)).toContain('local answer');
        expect(result.current.sending).toBe(true);
        expect(result.current.busySessionKeys).toContain('desktop-user:D:/tasks/weather');
        expect(result.current.busySessionKeys).not.toContain('desktop-user');

        await act(async () => {
            emitRuntimeEvent('ai-assistant-token', { request_id: projectReq.request_id || '', session_key: 'desktop-user:D:/tasks/weather', text: 'detached token' });
        });
        expect(result.current.streaming).toBe(true);
        expect(result.current.streamingSessionKeys).toContain('desktop-user:D:/tasks/weather');

        await act(async () => {
            emitRuntimeEvent('ai-assistant-response', {
                text: '',
                error: '',
                fields: null,
                actions: null,
                request_id: projectReq.request_id || '',
            });
        });

        expect(messageContents(result.current.messages)).toContain('detached token');
    });

    it('recreates a missing project assistant placeholder before applying the terminal response', async () => {
        (SendAIAssistantMessage as any).mockImplementationOnce(async (req: { request_id?: string }) => ({
            text: '',
            error: '',
            fields: null,
            actions: null,
            request_id: req.request_id || '',
            deferred: true,
        }));

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('project placeholder can disappear', { project_path: 'D:/tasks/missing-placeholder' });
            await Promise.resolve();
        });
        const projectReq = parseSentRequest(0);

        await act(async () => {
            result.current.messages.splice(1, 1);
            emitRuntimeEvent('ai-assistant-response', {
                request_id: projectReq.request_id || '',
                session_key: 'desktop-user:D:/tasks/missing-placeholder',
                text: 'project terminal survives missing placeholder',
                error: '',
                fields: null,
                actions: null,
            });
        });

        expect(messageContents(result.current.messages)).toContain('project terminal survives missing placeholder');
        expect(result.current.messages.find(m => m.content === 'project terminal survives missing placeholder')?.sessionKey).toBe('desktop-user:D:/tasks/missing-placeholder');
        expect(result.current.busySessionKeys).not.toContain('desktop-user:D:/tasks/missing-placeholder');
    });

    it('cancelSession cancels detached project round after local foreground send completes', async () => {
        (SendAIAssistantMessage as any)
            .mockImplementationOnce(async (req: { request_id?: string }) => ({
                text: '',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
                deferred: true,
            }))
            .mockImplementationOnce(async (req: { request_id?: string }) => ({
                text: 'local finished',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
            }));
        (CancelAIAssistantSessionForSession as any).mockResolvedValueOnce('');

        const { result } = renderHook(() => useAIAssistant({ activeSessionKey: 'desktop-user:D:/tasks/weather' }));

        await act(async () => {
            void result.current.sendMessage('project to cancel', { project_path: 'D:/tasks/weather' });
            await Promise.resolve();
        });
        const projectReq = parseSentRequest(0);

        await act(async () => {
            void result.current.sendMessage('local finishes first');
            await Promise.resolve();
        });
        expect(messageContents(result.current.messages)).toContain('local finished');

        await act(async () => {
            await result.current.cancelSession();
        });

        expect(CancelAIAssistantSessionForSession).toHaveBeenCalledWith('desktop-user:D:/tasks/weather');
        expect(CancelAIAssistantSession).not.toHaveBeenCalled();
        expect(messageContents(result.current.messages).join('\n')).toContain(CANCELED_BY_USER_LINE);

        await act(async () => {
            emitRuntimeEvent('ai-assistant-response', {
                text: 'stale project after cancel',
                error: '',
                fields: null,
                actions: null,
                request_id: projectReq.request_id || '',
            });
        });
        expect(messageContents(result.current.messages)).not.toContain('stale project after cancel');
    });

    it('removes reassigned synchronous request from detached round tracking', async () => {
        (SendAIAssistantMessage as any).mockImplementationOnce(async () => ({
            text: 'sync done',
            error: '',
            fields: null,
            actions: null,
            request_id: 'backend-reassigned-request',
        }));

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('sync reassigned id');
        });
        expect(messageContents(result.current.messages)).toContain('sync done');

        await act(async () => {
            emitRuntimeEvent('ai-assistant-response', {
                text: 'late duplicate should not land',
                error: '',
                fields: null,
                actions: null,
                request_id: 'backend-reassigned-request',
            });
        });

        expect(messageContents(result.current.messages)).not.toContain('late duplicate should not land');
    });

    it('keeps deferred reassigned project requests isolated under the backend request id', async () => {
        (SendAIAssistantMessage as any).mockImplementationOnce(async () => ({
            text: '',
            error: '',
            fields: null,
            actions: null,
            request_id: 'backend-deferred-project-request',
            deferred: true,
        }));

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('project deferred reassigned', { project_path: 'D:/tasks/reassigned-project' });
            await Promise.resolve();
        });
        expect(result.current.busySessionKeys).toContain('desktop-user:D:/tasks/reassigned-project');

        await act(async () => {
            emitRuntimeEvent('ai-assistant-token', {
                request_id: 'backend-deferred-project-request',
                session_key: 'desktop-user:D:/tasks/reassigned-project',
                text: 'project token ',
            });
            emitRuntimeEvent('ai-assistant-response', {
                request_id: 'backend-deferred-project-request',
                text: 'project final',
                error: '',
                fields: null,
                actions: null,
            });
        });

        expect(messageContents(result.current.messages)).toContain('project final');
        expect(result.current.busySessionKeys).not.toContain('desktop-user:D:/tasks/reassigned-project');
    });

    it('serializes sends within the same project session until the deferred round finishes', async () => {
        (SendAIAssistantMessage as any)
            .mockImplementationOnce(async (req: { request_id?: string }) => ({
                text: '',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
                deferred: true,
            }))
            .mockImplementationOnce(async (req: { request_id?: string }) => ({
                text: 'second project answer',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
            }));

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('project first', { project_path: 'D:/tasks/same-session' });
            await Promise.resolve();
        });
        const firstReq = parseSentRequest(0);

        await act(async () => {
            void result.current.sendMessage('project second', { project_path: 'D:/tasks/same-session' });
            await Promise.resolve();
        });
        expect((SendAIAssistantMessage as any).mock.calls).toHaveLength(1);

        await act(async () => {
            emitRuntimeEvent('ai-assistant-response', {
                request_id: firstReq.request_id || '',
                text: 'first project answer',
                error: '',
                fields: null,
                actions: null,
            });
            await Promise.resolve();
        });

        await waitFor(() => {
            expect((SendAIAssistantMessage as any).mock.calls).toHaveLength(2);
        });
        expect(parseSentRequest(1).text).toBe('project second');
    });

    it('forgets detached project rounds when the project tab closes', async () => {
        (SendAIAssistantMessage as any)
            .mockImplementationOnce(async (req: { request_id?: string }) => ({
                text: '',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
                deferred: true,
            }))
            .mockImplementationOnce(async (req: { request_id?: string }) => ({
                text: 'local done',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
            }));

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('project tab will close', { project_path: 'D:/tasks/close-me' });
            await Promise.resolve();
        });
        const projectReq = parseSentRequest(0);

        await act(async () => {
            void result.current.sendMessage('local done first');
            await Promise.resolve();
        });
        expect(messageContents(result.current.messages)).toContain('local done');

        act(() => {
            forgetAIAssistantSessionRounds('desktop-user:D:/tasks/close-me');
        });
        await waitFor(() => {
            expect(result.current.busySessionKeys).not.toContain('desktop-user:D:/tasks/close-me');
            expect(result.current.sending).toBe(false);
        });

        await act(async () => {
            emitRuntimeEvent('ai-assistant-response', {
                text: 'stale closed project reply',
                error: '',
                fields: null,
                actions: null,
                request_id: projectReq.request_id || '',
            });
        });

        expect(messageContents(result.current.messages)).not.toContain('stale closed project reply');
    });

    it('clears a detached pending project task when its remote session exits', async () => {
        (SendAIAssistantMessage as any)
            .mockImplementationOnce(async (req: { request_id?: string }) => ({
                text: 'project remote still running',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
                deferred: true,
                run_id: 'run-detached-exit',
                job_id: 'job-detached-exit',
            }))
            .mockImplementationOnce(async (req: { request_id?: string }) => ({
                text: 'local done after detached pending',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
            }));
        (ListRemoteSessions as any)
            .mockResolvedValueOnce([
                { id: 'sess-detached-exit', launch_source: 'ai', status: 'busy', run_id: 'run-detached-exit', job_id: 'job-detached-exit' },
            ])
            .mockResolvedValueOnce([
                { id: 'sess-detached-exit', launch_source: 'ai', status: 'busy', run_id: 'run-detached-exit', job_id: 'job-detached-exit' },
            ])
            .mockResolvedValueOnce([
                { id: 'sess-detached-exit', launch_source: 'ai', status: 'exited', run_id: 'run-detached-exit', job_id: 'job-detached-exit' },
            ]);

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('project pending exits later', { project_path: 'D:/tasks/detached-exit' });
            await Promise.resolve();
        });
        const projectReq = parseSentRequest(0);

        await act(async () => {
            void result.current.sendMessage('local takes active round');
            await Promise.resolve();
        });
        expect(messageContents(result.current.messages)).toContain('local done after detached pending');
        expect(result.current.busySessionKeys).toContain('desktop-user:D:/tasks/detached-exit');

        await act(async () => {
            emitRuntimeEvent('remote-state-changed');
            await Promise.resolve();
        });

        await waitFor(() => {
            expect(result.current.busySessionKeys).not.toContain('desktop-user:D:/tasks/detached-exit');
            expect(result.current.sending).toBe(false);
        });

        await act(async () => {
            emitRuntimeEvent('ai-assistant-response', {
                text: 'stale detached exited reply',
                error: '',
                fields: null,
                actions: null,
                request_id: projectReq.request_id || '',
            });
        });
        expect(messageContents(result.current.messages)).not.toContain('stale detached exited reply');
    });

    it('tracks multiple detached pending project tasks independently', async () => {
        let sessions: any[] = [
            { id: 'sess-pending-a', launch_source: 'ai', status: 'busy', run_id: 'run-pending-a', job_id: 'job-pending-a' },
            { id: 'sess-pending-b', launch_source: 'ai', status: 'busy', run_id: 'run-pending-b', job_id: 'job-pending-b' },
        ];
        (ListRemoteSessions as any).mockImplementation(async () => sessions);
        (SendAIAssistantMessage as any)
            .mockImplementationOnce(async (req: { request_id?: string }) => ({
                text: 'project a pending',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
                deferred: true,
                run_id: 'run-pending-a',
                job_id: 'job-pending-a',
            }))
            .mockImplementationOnce(async (req: { request_id?: string }) => ({
                text: 'project b pending',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
                deferred: true,
                run_id: 'run-pending-b',
                job_id: 'job-pending-b',
            }));

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('project a pending', { project_path: 'D:/tasks/pending-a' });
            await Promise.resolve();
        });
        await act(async () => {
            void result.current.sendMessage('project b pending', { project_path: 'D:/tasks/pending-b' });
            await Promise.resolve();
        });

        expect(result.current.busySessionKeys).toEqual(expect.arrayContaining([
            'desktop-user:D:/tasks/pending-a',
            'desktop-user:D:/tasks/pending-b',
        ]));

        sessions = [
            { id: 'sess-pending-b', launch_source: 'ai', status: 'busy', run_id: 'run-pending-b', job_id: 'job-pending-b' },
        ];
        await act(async () => {
            emitRuntimeEvent('remote-state-changed');
            await Promise.resolve();
        });

        await waitFor(() => {
            expect(result.current.busySessionKeys).not.toContain('desktop-user:D:/tasks/pending-a');
            expect(result.current.busySessionKeys).toContain('desktop-user:D:/tasks/pending-b');
            expect(result.current.sending).toBe(true);
        });

        sessions = [];
        await act(async () => {
            emitRuntimeEvent('remote-state-changed');
            await Promise.resolve();
        });

        await waitFor(() => {
            expect(result.current.busySessionKeys).not.toContain('desktop-user:D:/tasks/pending-b');
            expect(result.current.sending).toBe(false);
        });
    });

    it('unblocks a session queue when that session finishes even if another project is still pending', async () => {
        let sessions: any[] = [
            { id: 'sess-local', launch_source: 'ai', status: 'busy', run_id: 'run-local', job_id: 'job-local' },
            { id: 'sess-project', launch_source: 'ai', status: 'busy', run_id: 'run-project', job_id: 'job-project' },
        ];
        (ListRemoteSessions as any).mockImplementation(async () => sessions);
        (SendAIAssistantMessage as any)
            .mockImplementationOnce(async (req: { request_id?: string }) => ({
                text: 'local pending',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
                deferred: true,
                run_id: 'run-local',
                job_id: 'job-local',
            }))
            .mockImplementationOnce(async (req: { request_id?: string }) => ({
                text: 'project pending',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
                deferred: true,
                run_id: 'run-project',
                job_id: 'job-project',
            }))
            .mockImplementationOnce(async (req: { request_id?: string }) => ({
                text: 'local second done',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
            }));

        const { result } = renderAssistantHook();

        await act(async () => {
            void result.current.sendMessage('local first');
            await Promise.resolve();
        });
        await act(async () => {
            void result.current.sendMessage('project pending', { project_path: 'D:/tasks/still-running' });
            await Promise.resolve();
        });

        const localReq = parseSentRequest(0);
        expect(result.current.busySessionKeys).toEqual(expect.arrayContaining([
            'desktop-user',
            'desktop-user:D:/tasks/still-running',
        ]));

        await act(async () => {
            void result.current.sendMessage('local second');
            await Promise.resolve();
        });
        expect((SendAIAssistantMessage as any).mock.calls).toHaveLength(2);

        sessions = [
            { id: 'sess-project', launch_source: 'ai', status: 'busy', run_id: 'run-project', job_id: 'job-project' },
        ];
        await act(async () => {
            emitRuntimeEvent('ai-assistant-response', {
                text: 'local first done',
                error: '',
                fields: null,
                actions: null,
                request_id: localReq.request_id || '',
                session_key: 'desktop-user',
            });
            await Promise.resolve();
        });

        await waitFor(() => {
            expect((SendAIAssistantMessage as any).mock.calls).toHaveLength(3);
        });
        expect(parseSentRequest(2).text).toBe('local second');
        expect(result.current.busySessionKeys).toContain('desktop-user:D:/tasks/still-running');
    });

    it('does not let project clear_ui responses wipe local desktop history', async () => {
        (SendAIAssistantMessage as any)
            .mockImplementationOnce(async (req: { request_id?: string }) => ({
                text: 'local seed answer',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
            }))
            .mockImplementationOnce(async (req: { request_id?: string }) => ({
                text: 'project reset done',
                error: '',
                fields: null,
                actions: null,
                request_id: req.request_id || '',
                clear_ui: true,
            }));

        const { result } = renderAssistantHook();

        await act(async () => {
            await result.current.sendMessage('local seed');
        });
        await act(async () => {
            await result.current.sendMessage('/reset', { project_path: 'D:/tasks/reset-project' });
        });

        expect(messageContents(result.current.messages)).toContain('local seed');
        expect(messageContents(result.current.messages)).toContain('local seed answer');
        expect(messageContents(result.current.messages)).toContain('project reset done');
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

                    expect(assistantMsg).toBeDefined();
                    expect(assistantMsg!.content).toBe('done');
                    expect(result.current.progressMessages).toEqual([]);

                    unmount();
                },
            ),
            { numRuns: 100 },
        );
    }, 10000);
});
