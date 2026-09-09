// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';

// Reproduction harness for the "long streamed reply freezes mid-way" report:
// drive the real useAIAssistant hook with a deferred backend response and a
// realistic token stream, and verify the message state accumulates the whole
// reply before the final response lands.

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

const TOKEN_EVENT = 'ai-assistant-token';
const DONE_EVENT = 'ai-assistant-stream-done';

function emit(event: string, payload: unknown) {
    const handler = runtimeHandlers.get(event);
    if (!handler) throw new Error(`no handler subscribed for ${event}`);
    handler(payload);
}

describe('long streamed reply accumulation', () => {
    afterEach(() => {
        deferredSend = null;
        runtimeHandlers.clear();
        vi.useRealTimers();
    });

    it('accumulates every token of a 30KB streamed reply before the final response', async () => {
        const intro = '抱歉，PDF生成工具当前不可用。但我已经完成了全网搜索，整理了张惠妹完整的歌曲信息。以下是详细报告：';
        const items = Array.from(
            { length: 150 },
            (_, i) => `${i + 1}. 《歌曲名称${i + 1}》 — 专辑《专辑名称${i + 1}》（${1996 + (i % 25)}年发行，时长 4:0${i % 10}，流派：流行，作词：佚名，作曲：佚名）`,
        ).join('\n\n');
        const full = `${intro}\n\n${items}`;
        expect(full.length).toBeGreaterThan(6000);

        const { result } = renderHook(() => useAIAssistant());
        await act(async () => {
            void result.current.sendMessage('全网搜索张惠妹歌曲列表');
        });
        const requestId: string | undefined = lastSentRequest?.request_id;
        expect(requestId).toBeTruthy();

        // Stream the reply in token-sized chunks, letting the 33ms flush
        // timer run every few chunks on the real clock.
        const chunkSize = 120;
        for (let offset = 0; offset < full.length; offset += chunkSize) {
            const chunk = full.slice(offset, offset + chunkSize);
            act(() => {
                emit(TOKEN_EVENT, { request_id: requestId, text: chunk });
            });
            if (offset % (chunkSize * 10) === 0) {
                await act(async () => { await new Promise((r) => setTimeout(r, 40)); });
            }
        }
        await act(async () => { await new Promise((r) => setTimeout(r, 80)); });
        act(() => {
            emit(DONE_EVENT, { request_id: requestId });
        });
        await act(async () => { await new Promise((r) => setTimeout(r, 120)); });

        const streamed = result.current.messages.at(-1);
        if (streamed?.role !== 'assistant' || !(streamed.content || '').includes('歌曲名称150')) {
            console.log('MESSAGES DEBUG', result.current.messages.map((m) => ({ role: m.role, len: (m.content || '').length, tail: (m.content || '').slice(-60) })));
        }
        expect(streamed?.role).toBe('assistant');
        const normalized = (s: string) => s.replace(/\s+/g, '');
        expect(normalized(streamed?.content || '')).toContain('歌曲名称150');
        expect((streamed?.content || '').length).toBeGreaterThan(full.length * 0.95);

        // Final backend response lands afterwards and must keep the full text.
        await act(async () => {
            deferredSend?.resolve({ text: full, error: '', request_id: requestId });
        });
        const finalMessage = result.current.messages.at(-1);
        expect(normalized(finalMessage?.content || '')).toContain('歌曲名称150');
    });
});
