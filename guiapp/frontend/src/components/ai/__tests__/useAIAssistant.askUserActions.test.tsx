// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';

let mockSendResponse: any = { text: 'ok', error: '', request_id: 'req-default' };
let deferredSend: { resolve: (value: any) => void } | null = null;
let lastSentRequest: { request_id?: string } | null = null;
let mockUIState: any = { messages: [], prompts: [] };
const runtimeHandlers = new Map<string, (payload?: unknown) => void>();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    SendAIAssistantMessage: vi.fn((req: { request_id?: string }) => new Promise((resolve) => {
        deferredSend = { resolve };
        lastSentRequest = req;
    })),
    ClearAIAssistantHistory: vi.fn(async () => {}),
    ClearAIAssistantHistoryForSession: vi.fn(async () => {}),
    ClearAIAssistantUIState: vi.fn(async () => { mockUIState = { messages: [], prompts: [] }; }),
    LoadAIAssistantUIState: vi.fn(async () => mockUIState),
    SaveAIAssistantUIState: vi.fn(async (state: any) => { mockUIState = state; }),
    IsAIAssistantReady: vi.fn(async () => true),
    GetAIAssistantInitStatus: vi.fn(async () => 'ready'),
    GetTrialReflectEnabled: vi.fn(async () => false),
    GetAIAssistantTrace: vi.fn(async () => ({ summary: '', event_count: 0, evidence_count: 0, events: [], evidence: [] })),
    LoadConfig: vi.fn(async () => ({})),
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
    InjectAIAssistantGuideReferenceForSessionWithID: vi.fn(async () => true),
    HasAIAssistantGuideReferenceForSessionWithID: vi.fn(async () => false),
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

import { useAIAssistant } from '../useAIAssistant';

const RESPONSE_EVENT = 'ai-assistant-response';
const DONE_EVENT = 'ai-assistant-stream-done';

function emit(event: string, payload: unknown) {
    const handler = runtimeHandlers.get(event);
    if (!handler) throw new Error(`no handler subscribed for ${event}`);
    handler(payload);
}

describe('ask_user approval actions', () => {
    afterEach(() => {
        deferredSend = null;
        runtimeHandlers.clear();
    });

    it('keeps the three approval actions on the finalized ask message', async () => {
        const { result } = renderHook(() => useAIAssistant());
        await act(async () => {
            void result.current.sendMessage('给 a@b.com 发信测试');
        });
        const requestId: string | undefined = lastSentRequest?.request_id;
        expect(requestId).toBeTruthy();

        const askText = '执行 mailbox_send 缺少人的批准，这一步先不执行。要允许这一形状的调用吗？\na@b.com · 测试\n\n  1. 仅这一次\n  2. 本任务内允许\n  3. 拒绝\n\n请输入：选项编号或内容';
        await act(async () => {
            emit(DONE_EVENT, { request_id: requestId });
            emit(RESPONSE_EVENT, {
                request_id: requestId,
                session_key: 'desktop-user',
                text: askText,
                response_source: 'ask_user',
                fields: [{ label: 'Turn', value: 'fast · primary · qfmodel · in=18k out=310' }],
                actions: [
                    { label: '仅这一次', command: '仅这一次', style: '' },
                    { label: '本任务内允许', command: '本任务内允许', style: '' },
                    { label: '拒绝', command: '拒绝', style: '' },
                ],
            });
            await new Promise((r) => setTimeout(r, 30));
        });

        // The desktop dialog is the approval path now, but the ask_user card
        // still backs IM channels and every headless fallback: losing its
        // actions leaves a question the person can only answer by typing.
        const msg = result.current.messages.at(-1);
        expect(msg?.role).toBe('assistant');
        expect(msg?.actions?.length).toBe(3);
    });
});
