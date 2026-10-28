import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';

let mockSendResponse: any = { text: '', error: '', fields: null, actions: null, deferred: true };
const runtimeHandlers = new Map<string, (payload?: unknown) => void>();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    SendAIAssistantMessage: vi.fn(async (req: any) => {
        if (req && typeof req === 'object' && req.request_id && !mockSendResponse.request_id) {
            return { ...mockSendResponse, request_id: req.request_id };
        }
        return mockSendResponse;
    }),
    ClearAIAssistantHistory: vi.fn(async () => {}),
    ClearAIAssistantHistoryForSession: vi.fn(async () => {}),
    ClearAIAssistantUIState: vi.fn(async () => {}),
    LoadAIAssistantUIState: vi.fn(async () => ({ messages: [], prompts: [] })),
    SaveAIAssistantUIState: vi.fn(async () => {}),
    IsAIAssistantReady: vi.fn(async () => true),
    GetAIAssistantInitStatus: vi.fn(async () => 'ready'),
    GetTrialReflectEnabled: vi.fn(async () => false),
    GetAIAssistantTrace: vi.fn(async () => ({ summary: '', event_count: 0, evidence_count: 0, events: [], evidence: [] })),
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
    InjectAIAssistantGuideReferenceForSessionWithID: vi.fn(async () => true),
    HasAIAssistantGuideReferenceForSessionWithID: vi.fn(async () => false),
    SubmitAgentView: vi.fn(async () => ({ text: '', error: '' })),
    DismissAgentView: vi.fn(async () => ({ text: '', error: '' })),
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

const A = 'desktop-user:D:/tasks/running-a';
const B = 'desktop-user:D:/tasks/other-b';

describe('scratch: busy survives a tab switch', () => {
    beforeEach(() => {
        runtimeHandlers.clear();
        mockSendResponse = { text: '', error: '', fields: null, actions: null, deferred: true };
    });

    it('keeps the sending session busy after the active session changes', async () => {
        const { result, rerender } = renderHook(
            (props: { activeSessionKey: string }) => useAIAssistant({ activeSessionKey: props.activeSessionKey }),
            { initialProps: { activeSessionKey: A } },
        );
        await waitFor(() => expect(result.current.ready).toBe(true));

        await act(async () => {
            await result.current.sendMessage('run it', { project_path: 'D:/tasks/running-a' });
        });
        expect(result.current.busySessionKeys).toEqual([A]);

        rerender({ activeSessionKey: B });
        await act(async () => {});

        console.log('after switch busy=', JSON.stringify(result.current.busySessionKeys), 'sending=', result.current.sending);
        expect(result.current.busySessionKeys).toEqual([A]);
    });
});
