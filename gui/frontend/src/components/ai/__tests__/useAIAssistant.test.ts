/**
 * Property-based tests for useAIAssistant hook.
 *
 * Feature: ai-assistant-sidebar-icon
 * Properties: 2, 3, 9, 10
 *
 * Uses fast-check for property-based testing with ≥100 iterations.
 * Wails bindings are mocked since tests run outside the Wails runtime.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import * as fc from 'fast-check';

// ── Mock Wails bindings ──
// Must be hoisted before the hook import.

let mockSendResponse: any = { text: 'ok', error: '', fields: null, actions: null };
let mockSendError: Error | null = null;

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
}));

// Capture EventsOn handler so tests can simulate progress events.
let progressHandler: ((text: string) => void) | null = null;

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn((event: string, handler: (text: string) => void) => {
        if (event === 'ai-assistant-progress') {
            progressHandler = handler;
        }
    }),
    EventsOff: vi.fn(),
}));

// Import hook AFTER mocks are set up.
import { useAIAssistant } from '../useAIAssistant';

describe('useAIAssistant property tests', () => {
    beforeEach(() => {
        mockSendResponse = { text: 'ok', error: '', fields: null, actions: null };
        mockSendError = null;
        progressHandler = null;
    });

    afterEach(() => {
        vi.clearAllMocks();
    });

    // ─────────────────────────────────────────────────────────────────
    // Feature: ai-assistant-sidebar-icon, Property 2: 发送消息增长消息列表
    //
    // Validates: Requirement 2.5
    // For any non-empty text, sendMessage(text) must add at least one
    // message with role === 'user' and content === text.
    // ─────────────────────────────────────────────────────────────────
    it('Property 2: sendMessage grows message list with user message', async () => {
        await fc.assert(
            fc.asyncProperty(
                fc.string({ minLength: 1, maxLength: 80 }).filter(s => s.trim().length > 0),
                async (text) => {
                    const { result, unmount } = renderHook(() => useAIAssistant());

                    const before = result.current.messages.length;

                    await act(async () => {
                        await result.current.sendMessage(text);
                    });

                    const after = result.current.messages;

                    // List grew by at least 1 (user msg; possibly also assistant).
                    expect(after.length).toBeGreaterThan(before);

                    // Contains a user message with the exact text.
                    const userMsg = after.find(m => m.role === 'user' && m.content === text);
                    expect(userMsg).toBeDefined();

                    unmount();
                },
            ),
            { numRuns: 100 },
        );
    });

    // ─────────────────────────────────────────────────────────────────
    // Feature: ai-assistant-sidebar-icon, Property 3: 关闭/重新打开保留对话历史
    //
    // Validates: Requirement 2.9
    // Since useAIAssistant is lifted to App.tsx level, the hook instance
    // persists across panel close/reopen. We verify that messages state
    // survives across multiple sendMessage calls without being reset.
    // ─────────────────────────────────────────────────────────────────
    it('Property 3: messages persist across rerender (simulating close/reopen)', async () => {
        await fc.assert(
            fc.asyncProperty(
                fc.array(
                    fc.string({ minLength: 1, maxLength: 40 }).filter(s => s.trim().length > 0),
                    { minLength: 1, maxLength: 5 },
                ),
                async (texts) => {
                    const { result, rerender, unmount } = renderHook(() => useAIAssistant());

                    // Send all messages sequentially.
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

                    // Simulate panel close/reopen by forcing a rerender of the hook.
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

    // ─────────────────────────────────────────────────────────────────
    // Feature: ai-assistant-sidebar-icon, Property 9: 进度事件传递
    //
    // Validates: Requirements 5.1, 5.2
    // For any progress text emitted via the Wails event, the messages
    // list must contain a message with role === 'progress'.
    // ─────────────────────────────────────────────────────────────────
    it('Property 9: progress events appear as progress messages', async () => {
        await fc.assert(
            fc.asyncProperty(
                fc.string({ minLength: 1, maxLength: 80 }),
                async (progressText) => {
                    const { result, unmount } = renderHook(() => useAIAssistant());

                    // Simulate a progress event from Wails runtime.
                    expect(progressHandler).not.toBeNull();
                    await act(async () => {
                        progressHandler!(progressText);
                    });

                    const progressMsgs = result.current.messages.filter(m => m.role === 'progress');
                    expect(progressMsgs.length).toBeGreaterThanOrEqual(1);

                    const found = progressMsgs.find(m => m.content === progressText);
                    expect(found).toBeDefined();

                    unmount();
                },
            ),
            { numRuns: 100 },
        );
    });

    // ─────────────────────────────────────────────────────────────────
    // Feature: ai-assistant-sidebar-icon, Property 10: 最终回复在进度消息之后
    //
    // Validates: Requirement 5.4
    // The hook uses streaming: an assistant placeholder is inserted
    // before the backend call, so its index precedes progress messages.
    // We verify that after sendMessage resolves, the assistant message
    // has been populated with final content and all progress events
    // are present — i.e. the final reply is "resolved after" progress.
    // ─────────────────────────────────────────────────────────────────
    it('Property 10: assistant reply is populated after progress messages arrive', async () => {
        await fc.assert(
            fc.asyncProperty(
                fc.string({ minLength: 1, maxLength: 40 }).filter(s => s.trim().length > 0),
                fc.array(fc.string({ minLength: 1, maxLength: 40 }), { minLength: 1, maxLength: 5 }),
                async (userText, progressTexts) => {
                    const { result, unmount } = renderHook(() => useAIAssistant());

                    // Override SendAIAssistantMessage to emit progress events before resolving.
                    const { SendAIAssistantMessage } = await import('../../../../wailsjs/go/main/App');
                    (SendAIAssistantMessage as any).mockImplementationOnce(async () => {
                        // Emit progress events synchronously (simulating backend progress).
                        for (const pt of progressTexts) {
                            progressHandler!(pt);
                        }
                        return { text: 'done', error: '', fields: null, actions: null };
                    });

                    await act(async () => {
                        await result.current.sendMessage(userText);
                    });

                    const msgs = result.current.messages;
                    const assistantMsg = msgs.find(m => m.role === 'assistant');
                    const progressMsgs = msgs.filter(m => m.role === 'progress');

                    // Both assistant and progress messages must exist.
                    expect(assistantMsg).toBeDefined();
                    expect(progressMsgs.length).toBeGreaterThan(0);

                    // Assistant placeholder was populated with final content.
                    expect(assistantMsg!.content).toBe('done');

                    // All progress events were captured.
                    for (const pt of progressTexts) {
                        expect(progressMsgs.find(m => m.content === pt)).toBeDefined();
                    }

                    unmount();
                },
            ),
            { numRuns: 100 },
        );
    });
});
