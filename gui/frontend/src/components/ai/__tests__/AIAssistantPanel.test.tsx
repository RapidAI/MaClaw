/**
 * Property-based tests for AIAssistantPanel component.
 *
 * Feature: ai-assistant-sidebar-icon
 * Property 8: 响应渲染完整性
 *
 * Uses fast-check for property-based testing with ≥100 iterations.
 */
import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, cleanup, fireEvent, waitFor } from '@testing-library/react';
import * as fc from 'fast-check';
import { AIAssistantPanel } from '../AIAssistantPanel';
import type { ChatMessage, CancelAIAssistantResult } from '../useAIAssistant';

const scrollIntoViewMock = vi.fn();
const scrollToMock = vi.fn();

Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {
    configurable: true,
    value: scrollIntoViewMock,
});

Object.defineProperty(HTMLElement.prototype, 'scrollTo', {
    configurable: true,
    value: scrollToMock,
});

// ── Mock Wails runtime (not used by panel but imported transitively) ──
vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(),
    EventsOff: vi.fn(),
}));

// Helper: build a ChatMessage for testing.
function makeMsg(overrides: Partial<ChatMessage> & { role: ChatMessage['role'] }): ChatMessage {
    return {
        id: `test-${Math.random()}`,
        content: overrides.content ?? '',
        timestamp: Date.now(),
        ...overrides,
    };
}

function renderPanel(overrides: Partial<React.ComponentProps<typeof AIAssistantPanel>> = {}) {
    const props: React.ComponentProps<typeof AIAssistantPanel> = {
        onClose: () => {},
        lang: 'en',
        messages: [],
        sending: false,
        streaming: false,
        ready: true,
        sendMessage: async () => {},
        clearHistory: async () => {},
        executeAction: async () => {},
        refreshNews: () => {},
        ...overrides,
    };
    return render(<AIAssistantPanel {...props} />);
}

describe('AIAssistantPanel property tests', () => {
    afterEach(() => {
        cleanup();
        scrollIntoViewMock.mockClear();
        scrollToMock.mockClear();
    });

    it('keeps latest conversation visible when reopened with history', () => {
        const messages: ChatMessage[] = [
            makeMsg({ role: 'system', id: 'news-1', content: 'Pinned news' }),
            makeMsg({ role: 'user', content: 'Earlier question' }),
            makeMsg({ role: 'assistant', content: 'Latest answer' }),
        ];

        render(
            <AIAssistantPanel
                onClose={() => {}}
                lang="en"
                messages={messages}
                sending={false}
                streaming={false}
                ready={true}
                sendMessage={async () => {}}
                clearHistory={async () => {}}
                executeAction={async () => {}}
                refreshNews={() => {}}
                scrollToTopSeq={1}
            />
        );

        expect(scrollToMock).not.toHaveBeenCalled();
        expect(scrollIntoViewMock).toHaveBeenCalled();
    });

    it('scrolls to bottom when panel becomes ready with conversation history', () => {
        const messages: ChatMessage[] = [
            makeMsg({ role: 'system', id: 'news-1', content: 'Pinned news' }),
            makeMsg({ role: 'user', content: 'Earlier question' }),
            makeMsg({ role: 'assistant', content: 'Latest answer' }),
        ];

        const { rerender } = render(
            <AIAssistantPanel
                onClose={() => {}}
                lang="en"
                messages={messages}
                sending={false}
                streaming={false}
                ready={false}
                sendMessage={async () => {}}
                clearHistory={async () => {}}
                executeAction={async () => {}}
                refreshNews={() => {}}
            />
        );

        scrollIntoViewMock.mockClear();
        scrollToMock.mockClear();

        rerender(
            <AIAssistantPanel
                onClose={() => {}}
                lang="en"
                messages={messages}
                sending={false}
                streaming={false}
                ready={true}
                sendMessage={async () => {}}
                clearHistory={async () => {}}
                executeAction={async () => {}}
                refreshNews={() => {}}
            />
        );

        expect(scrollToMock).not.toHaveBeenCalled();
        expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'auto' });
    });

    it('scrolls to top when only pinned news exist', () => {
        const messages: ChatMessage[] = [
            makeMsg({ role: 'system', id: 'news-1', content: 'Pinned news' }),
        ];

        render(
            <AIAssistantPanel
                onClose={() => {}}
                lang="en"
                messages={messages}
                sending={false}
                streaming={false}
                ready={true}
                sendMessage={async () => {}}
                clearHistory={async () => {}}
                executeAction={async () => {}}
                refreshNews={() => {}}
                scrollToTopSeq={1}
            />
        );

        expect(scrollToMock).toHaveBeenCalledWith({ top: 0, behavior: 'smooth' });
    });

    it('restores canceled text back into the textarea', async () => {
        const cancelSession = vi.fn<() => Promise<CancelAIAssistantResult>>().mockResolvedValue({
            canceledText: 'repeat this request',
        });

        const { getByTestId } = renderPanel({
            sending: true,
            cancelSession,
        });

        fireEvent.click(getByTestId('ai-cancel-progress'));

        await waitFor(() => {
            expect(cancelSession).toHaveBeenCalledTimes(1);
            expect((getByTestId('ai-input') as HTMLTextAreaElement).value).toBe('repeat this request');
        });
    });

    it('shows animated progress cancel control while sending', () => {
        const { getByTestId, queryByTitle } = renderPanel({
            sending: true,
            cancelSession: async () => ({ canceledText: '' }),
        });

        expect(getByTestId('ai-cancel-progress')).toBeTruthy();
        expect(queryByTitle('Send')).toBeNull();
    });

    it('clicking the progress control triggers cancel', async () => {
        const cancelSession = vi.fn<() => Promise<CancelAIAssistantResult>>().mockResolvedValue({
            canceledText: '',
        });
        const { getByTestId } = renderPanel({
            sending: true,
            cancelSession,
        });

        fireEvent.click(getByTestId('ai-cancel-progress'));

        await waitFor(() => {
            expect(cancelSession).toHaveBeenCalledTimes(1);
        });
    });

    it('does not overwrite newer input typed while cancel is resolving', async () => {
        let resolveCancel: ((value: CancelAIAssistantResult) => void) | undefined;
        const cancelSession = vi.fn<() => Promise<CancelAIAssistantResult>>().mockImplementation(() => new Promise(resolve => {
            resolveCancel = resolve;
        }));
        const { getByTestId } = renderPanel({
            sending: true,
            cancelSession,
        });

        fireEvent.click(getByTestId('ai-cancel-progress'));
        fireEvent.change(getByTestId('ai-input'), { target: { value: 'new draft' } });
        resolveCancel?.({ canceledText: 'stale draft' });

        await waitFor(() => {
            expect(cancelSession).toHaveBeenCalledTimes(1);
            expect((getByTestId('ai-input') as HTMLTextAreaElement).value).toBe('new draft');
        });
    });

    it('Property 8: fields, actions, and errors are fully rendered', () => {
        const fieldArb = fc.record({
            label: fc.string({ minLength: 1, maxLength: 20 }).filter(s => s.trim().length > 0),
            value: fc.string({ minLength: 1, maxLength: 40 }).filter(s => s.trim().length > 0),
        });

        const actionArb = fc.record({
            label: fc.string({ minLength: 1, maxLength: 20 }).filter(s => s.trim().length > 0),
            command: fc.string({ minLength: 1, maxLength: 30 }),
            style: fc.constantFrom('default', 'danger'),
        });

        fc.assert(
            fc.property(
                fc.array(fieldArb, { minLength: 0, maxLength: 5 }),
                fc.array(actionArb, { minLength: 0, maxLength: 4 }),
                fc.option(fc.string({ minLength: 1, maxLength: 60 }).filter(s => s.trim().length > 0)),
                (fields, actions, errorOpt) => {
                    const messages: ChatMessage[] = [];

                    // Add an assistant message with fields and actions.
                    if (fields.length > 0 || actions.length > 0) {
                        messages.push(makeMsg({
                            role: 'assistant',
                            content: 'Response text',
                            fields: fields.length > 0 ? fields : undefined,
                            actions: actions.length > 0 ? actions : undefined,
                        }));
                    }

                    // Add an error message if generated.
                    if (errorOpt !== null) {
                        messages.push(makeMsg({
                            role: 'error',
                            content: errorOpt,
                        }));
                    }

                    if (messages.length === 0) return; // skip trivial case

                    // Clean up previous render to avoid DOM leaks across iterations.
                    cleanup();

                    const { container } = render(
                        <AIAssistantPanel
                            onClose={() => {}}
                            lang="en"
                            messages={messages}
                            sending={false}
                            streaming={false}
                            ready={true}
                            sendMessage={async () => {}}
                            clearHistory={async () => {}}
                            executeAction={async () => {}}
                            refreshNews={() => {}}
                        />
                    );

                    // Verify every field label and value is rendered.
                    for (const f of fields) {
                        const fieldCards = container.querySelectorAll('[data-testid="field-card"]');
                        const texts = Array.from(fieldCards).map(el => el.textContent || '');
                        const found = texts.some(t => t.includes(f.label) && t.includes(f.value));
                        expect(found).toBe(true);
                    }

                    // Verify action button count.
                    const actionButtons = container.querySelectorAll('[data-testid="action-button"]');
                    expect(actionButtons.length).toBe(actions.length);

                    // Verify error text is rendered.
                    if (errorOpt !== null) {
                        expect(container.textContent).toContain(errorOpt);
                    }
                },
            ),
            { numRuns: 100 },
        );
    });
});
