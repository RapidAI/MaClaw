import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, cleanup, fireEvent, waitFor } from '@testing-library/react';
import * as fc from 'fast-check';
import { AIAssistantPanel } from '../AIAssistantPanel';
import type { ChatMessage, CancelAIAssistantResult, NewsCardData, ChatAction } from '../useAIAssistant';

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

function makeMsg(overrides: Partial<ChatMessage> & { role: ChatMessage['role'] }): ChatMessage {
    return {
        id: `test-${Math.random()}`,
        content: overrides.content ?? '',
        timestamp: Date.now(),
        ...overrides,
    };
}

function makeNews(id: string, overrides: Partial<NewsCardData> = {}): ChatMessage {
    const title = overrides.title ?? 'Pinned news';
    const body = overrides.body ?? 'Pinned body';
    return makeMsg({
        id: `news-${id}`,
        role: 'system',
        kind: 'news',
        content: body,
        news: {
            articleId: id,
            category: overrides.category ?? 'notice',
            title,
            body,
            icon: overrides.icon ?? '📢',
        },
    });
}

function defaultPanelProps(): React.ComponentProps<typeof AIAssistantPanel> {
    return {
        onClose: () => {},
        lang: 'en',
        state: {
            messages: [],
            progressMessages: [],
            sending: false,
            streaming: false,
            ready: true,
            submittedPrompts: [],
        },
        actions: {
            sendMessage: async () => {},
            clearHistory: async () => {},
            executeAction: async () => {},
            refreshNews: () => {},
        },
    };
}

function renderPanel(overrides: Partial<React.ComponentProps<typeof AIAssistantPanel>> = {}) {
    const base = defaultPanelProps();
    const props: React.ComponentProps<typeof AIAssistantPanel> = {
        ...base,
        ...overrides,
        state: {
            ...base.state,
            ...overrides.state,
        },
        actions: {
            ...base.actions,
            ...overrides.actions,
        },
        window: {
            ...base.window,
            ...overrides.window,
        },
    };
    return render(<AIAssistantPanel {...props} />);
}

describe('AIAssistantPanel property tests', () => {
    afterEach(() => {
        cleanup();
        scrollIntoViewMock.mockClear();
        scrollToMock.mockClear();
    });

    it('shows trial-reflect badge when mode is enabled', () => {
        const { getByText } = renderPanel({
            state: { messages: [], sending: false, streaming: false, ready: true, trialReflectEnabled: true },
        });

        expect(getByText('Trial+Reflect')).toBeTruthy();
    });

    it('keeps latest conversation visible when reopened with history', () => {
        const messages: ChatMessage[] = [
            makeNews('1'),
            makeMsg({ role: 'user', content: 'Earlier question' }),
            makeMsg({ role: 'assistant', content: 'Latest answer' }),
        ];

        renderPanel({ state: { messages, scrollToTopSeq: 1, sending: false, streaming: false, ready: true } });

        expect(scrollToMock).not.toHaveBeenCalled();
        expect(scrollIntoViewMock).toHaveBeenCalled();
    });

    it('scrolls to bottom when panel becomes ready with conversation history', () => {
        const messages: ChatMessage[] = [
            makeNews('1'),
            makeMsg({ role: 'user', content: 'Earlier question' }),
            makeMsg({ role: 'assistant', content: 'Latest answer' }),
        ];

        const props = defaultPanelProps();
        const { rerender } = renderPanel({ state: { messages, ready: false, sending: false, streaming: false } });

        scrollIntoViewMock.mockClear();
        scrollToMock.mockClear();

        rerender(
            <AIAssistantPanel
                {...props}
                state={{
                    ...props.state,
                    messages,
                    sending: false,
                    streaming: false,
                    ready: true,
                }}
            />
        );

        expect(scrollToMock).not.toHaveBeenCalled();
        expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'auto' });
    });

    it('scrolls to top when only pinned news exist', () => {
        const messages: ChatMessage[] = [
            makeNews('1'),
        ];

        renderPanel({ state: { messages, scrollToTopSeq: 1, sending: false, streaming: false, ready: true } });

        expect(scrollToMock).toHaveBeenCalledWith({ top: 0, behavior: 'smooth' });
    });

    it('restores canceled text back into the textarea', async () => {
        const cancelSession = vi.fn<() => Promise<CancelAIAssistantResult>>().mockResolvedValue({
            canceledText: 'repeat this request',
        });

        const { getByTestId } = renderPanel({
            state: { messages: [], sending: true, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction: async () => {}, refreshNews: () => {}, cancelSession },
        });

        fireEvent.click(getByTestId('ai-cancel-progress'));

        await waitFor(() => {
            expect(cancelSession).toHaveBeenCalledTimes(1);
            expect((getByTestId('ai-input') as HTMLTextAreaElement).value).toBe('repeat this request');
        });
    });

    it('shows background launch control when available', () => {
        const { getByTestId } = renderPanel({
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: {
                sendMessage: async () => {},
                sendMessageInBackground: async () => {},
                clearHistory: async () => {},
                executeAction: async () => {},
                refreshNews: () => {},
            },
        });

        expect(getByTestId('ai-send-background').getAttribute('title')).toBe('Run in background');
    });

    it('ctrl+enter sends to background when available', async () => {
        const sendMessage = vi.fn<() => Promise<void>>().mockResolvedValue();
        const sendMessageInBackground = vi.fn<() => Promise<void>>().mockResolvedValue();
        const { getByTestId } = renderPanel({
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: {
                sendMessage,
                sendMessageInBackground,
                clearHistory: async () => {},
                executeAction: async () => {},
                refreshNews: () => {},
            },
        });

        fireEvent.change(getByTestId('ai-input'), { target: { value: 'background via shortcut' } });
        fireEvent.keyDown(getByTestId('ai-input'), { key: 'Enter', ctrlKey: true });

        await waitFor(() => {
            expect(sendMessageInBackground).toHaveBeenCalledWith('background via shortcut');
        });
        expect(sendMessage).not.toHaveBeenCalled();
    });

    it('clicking background launch control triggers background send', async () => {
        const sendMessageInBackground = vi.fn<() => Promise<void>>().mockResolvedValue();
        const { getByTestId } = renderPanel({
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: {
                sendMessage: async () => {},
                sendMessageInBackground,
                clearHistory: async () => {},
                executeAction: async () => {},
                refreshNews: () => {},
            },
        });

        fireEvent.change(getByTestId('ai-input'), { target: { value: 'long task' } });
        fireEvent.click(getByTestId('ai-send-background'));

        await waitFor(() => {
            expect(sendMessageInBackground).toHaveBeenCalledWith('long task');
        });
    });

    it('renders background launch identifiers inside a visible system message', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'system',
                content: [
                    '已转到后台运行。',
                    '任务会显示在“任务管理”里的后台列表。',
                    'session_id: session-trace',
                    'job_id: job-trace',
                    'run_id: run-trace',
                ].join('\n'),
            }),
        ];

        const { getByText } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
        });

        expect(getByText('session_id: session-trace')).toBeTruthy();
        expect(getByText('job_id: job-trace')).toBeTruthy();
        expect(getByText('run_id: run-trace')).toBeTruthy();
    });

    it('renders session-only background launch messages without extra ids', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'system',
                content: [
                    '已转到后台运行。',
                    '任务会显示在“任务管理”里的后台列表。',
                    'session_id: session-only',
                ].join('\n'),
            }),
        ];

        const { container, getByText } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
        });

        expect(getByText('session_id: session-only')).toBeTruthy();
        expect(container.textContent).not.toContain('job_id:');
        expect(container.textContent).not.toContain('run_id:');
    });

    it('renders trace summary and counts as assistant field cards', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: 'done',
                fields: [
                    { label: 'Trace', value: 'trial loop stabilized after one retry' },
                    { label: 'Trace events', value: '8' },
                    { label: 'Evidence', value: '3' },
                    { label: 'Run ID', value: 'run-trace-1' },
                    { label: 'Job ID', value: 'job-trace-1' },
                ],
                actions: [{ label: 'View trace', command: '__view_trace__ run-trace-1', style: 'default' }],
            }),
        ];

        const executeAction = vi.fn<() => Promise<void>>().mockResolvedValue();
        const { container, getByText } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction, refreshNews: () => {} },
        });

        const fieldCards = Array.from(container.querySelectorAll('[data-testid="field-card"]')).map(el => el.textContent || '');
        expect(fieldCards).toContain('Trace:trial loop stabilized after one retry');
        expect(fieldCards).toContain('Trace events:8');
        expect(fieldCards).toContain('Evidence:3');
        expect(fieldCards).toContain('Run ID:run-trace-1');
        expect(fieldCards).toContain('Job ID:job-trace-1');
        expect(getByText('View trace')).toBeTruthy();
    });

    it('renders trace detail system messages with trace field cards', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'system',
                kind: 'trace',
                content: [
                    'Trace details for run-trace-1',
                    'Summary: trial loop stabilized after one retry',
                    'Event kinds: trial.started, trial.observed',
                ].join('\n'),
                fields: [
                    { label: 'Run ID', value: 'run-trace-1' },
                    { label: 'Job ID', value: 'job-trace-1' },
                    { label: 'Trace events', value: '8' },
                    { label: 'Evidence', value: '3' },
                    { label: 'Status', value: 'completed' },
                ],
            }),
        ];

        const { container, getByText } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
        });

        const fieldCards = Array.from(container.querySelectorAll('[data-testid="field-card"]')).map(el => el.textContent || '');
        expect(fieldCards).toContain('Run ID:run-trace-1');
        expect(fieldCards).toContain('Job ID:job-trace-1');
        expect(fieldCards).toContain('Trace events:8');
        expect(fieldCards).toContain('Evidence:3');
        expect(fieldCards).toContain('Status:completed');
        expect(getByText('Trace details for run-trace-1')).toBeTruthy();
        expect(getByText('Summary: trial loop stabilized after one retry')).toBeTruthy();
    });

    it('ctrl+enter background send records prompt into history', async () => {
        const sendMessageInBackground = vi.fn<() => Promise<void>>().mockResolvedValue();
        const recordSubmittedPrompt = vi.fn();
        const { getByTestId } = renderPanel({
            state: { messages: [], submittedPrompts: [], sending: false, streaming: false, ready: true },
            actions: {
                sendMessage: async () => {},
                sendMessageInBackground,
                recordSubmittedPrompt,
                clearHistory: async () => {},
                executeAction: async () => {},
                refreshNews: () => {},
            },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'background via shortcut' } });
        fireEvent.keyDown(input, { key: 'Enter', ctrlKey: true });

        await waitFor(() => {
            expect(recordSubmittedPrompt).toHaveBeenCalledWith('background via shortcut');
            expect(sendMessageInBackground).toHaveBeenCalledWith('background via shortcut');
        });
    });

    it('normal send records each manual prompt once so consecutive duplicates can be deduplicated upstream', async () => {
        const sendMessage = vi.fn<() => Promise<void>>().mockResolvedValue();
        const recordSubmittedPrompt = vi.fn();
        const { getByTestId } = renderPanel({
            state: { messages: [], submittedPrompts: [], sending: false, streaming: false, ready: true },
            actions: {
                sendMessage,
                recordSubmittedPrompt,
                clearHistory: async () => {},
                executeAction: async () => {},
                refreshNews: () => {},
            },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'same prompt' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await waitFor(() => {
            expect(recordSubmittedPrompt).toHaveBeenCalledTimes(1);
            expect(recordSubmittedPrompt).toHaveBeenCalledWith('same prompt');
            expect(sendMessage).toHaveBeenCalledWith('same prompt');
        });
    });

    it('Escape exits history browsing and restores the pre-history draft', async () => {
        const { getByTestId } = renderPanel({
            state: { messages: [], submittedPrompts: ['older prompt', 'latest prompt'], sending: false, streaming: false, ready: true },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'draft text' } });
        input.setSelectionRange(input.value.length, input.value.length);

        fireEvent.keyDown(input, { key: 'ArrowUp' });
        await waitFor(() => expect(input.value).toBe('latest prompt'));

        fireEvent.keyDown(input, { key: 'Escape' });
        await waitFor(() => expect(input.value).toBe('draft text'));
    });

    it('preserves edited history entries while browsing', async () => {
        const { getByTestId } = renderPanel({
            state: { messages: [], submittedPrompts: ['older prompt', 'latest prompt'], sending: false, streaming: false, ready: true },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'draft text' } });
        input.setSelectionRange(input.value.length, input.value.length);

        fireEvent.keyDown(input, { key: 'ArrowUp' });
        await waitFor(() => expect(input.value).toBe('latest prompt'));

        fireEvent.change(input, { target: { value: 'latest prompt edited' } });
        input.setSelectionRange(input.value.length, input.value.length);

        fireEvent.keyDown(input, { key: 'ArrowUp' });
        await waitFor(() => expect(input.value).toBe('older prompt'));

        input.setSelectionRange(input.value.length, input.value.length);
        fireEvent.keyDown(input, { key: 'ArrowDown' });
        await waitFor(() => expect(input.value).toBe('latest prompt edited'));
    });

    it('does not recall history when ArrowUp is pressed away from the first line', async () => {
        const { getByTestId } = renderPanel({
            state: { messages: [], submittedPrompts: ['history prompt'], sending: false, streaming: false, ready: true },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'line 1\nline 2' } });
        input.setSelectionRange(input.value.length, input.value.length);

        fireEvent.keyDown(input, { key: 'ArrowUp' });

        await waitFor(() => expect(input.value).toBe('line 1\nline 2'));
    });
    it('keeps draft text when the panel is closed and reopened through controlled state', async () => {
        let draftInputValue = '';
        const setDraftInputValue = vi.fn((next: string) => {
            draftInputValue = next;
        });
        const props = defaultPanelProps();

        const { getByTestId, rerender } = render(
            <AIAssistantPanel
                {...props}
                state={{
                    ...props.state,
                    ready: true,
                    draftInputValue,
                }}
                actions={{
                    ...props.actions,
                    setDraftInputValue,
                }}
            />
        );

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'draft survives reopen' } });

        rerender(<div />);
        rerender(
            <AIAssistantPanel
                {...props}
                state={{
                    ...props.state,
                    ready: true,
                    draftInputValue,
                }}
                actions={{
                    ...props.actions,
                    setDraftInputValue,
                }}
            />
        );

        await waitFor(() => {
            expect((getByTestId('ai-input') as HTMLTextAreaElement).value).toBe('draft survives reopen');
        });
    });

    it('shows animated progress cancel control while sending', () => {
        const { getByTestId, queryByTitle } = renderPanel({
            state: { messages: [], sending: true, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction: async () => {}, refreshNews: () => {}, cancelSession: async () => ({ canceledText: '' }) },
        });

        expect(getByTestId('ai-cancel-progress')).toBeTruthy();
        expect(queryByTitle('Send')).toBeNull();
    });

    it('shows cancel without spinner after streaming finishes but request is still locked', () => {
        const { getByTestId, queryByTitle } = renderPanel({
            state: { messages: [], sending: true, streaming: false, visualBusy: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction: async () => {}, refreshNews: () => {}, cancelSession: async () => ({ canceledText: '' }) },
        });

        expect(getByTestId('ai-cancel-progress').textContent).toContain('■');
        expect(queryByTitle('Send')).toBeNull();
    });

    it('keeps the textarea read-only while the request is still in flight', () => {
        const { getByTestId } = renderPanel({
            state: { messages: [], sending: true, streaming: false, visualBusy: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction: async () => {}, refreshNews: () => {}, cancelSession: async () => ({ canceledText: '' }) },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        expect(input.disabled).toBe(false);
        expect(input.readOnly).toBe(true);
        expect(input.getAttribute('aria-readonly')).toBe('true');
    });

    it('keeps the textarea disabled while initialization is not ready', () => {
        const { getByTestId } = renderPanel({
            state: { messages: [], sending: false, streaming: false, ready: false },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        expect(input.disabled).toBe(true);
        expect(input.readOnly).toBe(false);
    });

    it('falls back to streaming state for the busy spinner when visualBusy is omitted', () => {
        const { getByTestId } = renderPanel({
            state: { messages: [], sending: true, streaming: true, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction: async () => {}, refreshNews: () => {}, cancelSession: async () => ({ canceledText: '' }) },
        });

        expect(getByTestId('ai-cancel-progress').textContent).not.toContain('■');
    });

    it('clicking the progress control triggers cancel', async () => {
        const cancelSession = vi.fn<() => Promise<CancelAIAssistantResult>>().mockResolvedValue({
            canceledText: '',
        });
        const { getByTestId } = renderPanel({
            state: { messages: [], sending: true, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction: async () => {}, refreshNews: () => {}, cancelSession },
        });

        fireEvent.click(getByTestId('ai-cancel-progress'));

        await waitFor(() => {
            expect(cancelSession).toHaveBeenCalledTimes(1);
        });
    });

    it('renders native-style inline window controls', () => {
        const { getByTestId } = renderPanel({
            window: { inline: true, maximized: false, onToggleMaximize: vi.fn(), onHideWindow: vi.fn() },
        });

        expect(getByTestId('ai-hide-toggle').getAttribute('title')).toBe('Minimize window');
        expect(getByTestId('ai-maximize-toggle').getAttribute('title')).toBe('Maximize window');
    });

    it('double-clicking the title bar toggles inline fullscreen', () => {
        const onToggleMaximize = vi.fn();
        const { getByTestId } = renderPanel({
            window: { inline: true, maximized: false, onToggleMaximize },
        });

        fireEvent.doubleClick(getByTestId('ai-title-bar'));

        expect(onToggleMaximize).toHaveBeenCalledTimes(1);
    });

    it('separates title bar tools from window controls', () => {
        const { getByTestId } = renderPanel({
            window: { inline: true, maximized: false, onToggleMaximize: vi.fn(), onHideWindow: vi.fn() },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction: async () => {}, refreshNews: () => {}, onOpenTutorial: vi.fn() },
        });

        const toolsGroup = getByTestId('ai-titlebar-tools-group');
        const windowGroup = getByTestId('ai-titlebar-window-group');

        expect(toolsGroup).toBeTruthy();
        expect(windowGroup).toBeTruthy();
        expect(windowGroup.style.borderLeft).toContain('solid');
        expect(windowGroup.style.marginLeft).toBe('16px');
        expect(windowGroup.querySelector('[data-testid="ai-hide-toggle"]')).toBeTruthy();
        expect(windowGroup.querySelector('[data-testid="ai-maximize-toggle"]')).toBeTruthy();
    });

    it('Property 8: fields, actions, and errors are fully rendered', () => {
        const fieldArb = fc.record({
            label: fc.string({ minLength: 1, maxLength: 20 }).filter(s => s.trim().length > 0),
            value: fc.string({ minLength: 1, maxLength: 40 }).filter(s => s.trim().length > 0),
        });

        const actionArb: fc.Arbitrary<Pick<ChatAction, 'label' | 'command' | 'style'>> = fc.record({
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

                    if (fields.length > 0 || actions.length > 0) {
                        messages.push(makeMsg({
                            role: 'assistant',
                            content: 'Response text',
                            fields: fields.length > 0 ? fields : undefined,
                            actions: actions.length > 0 ? actions : undefined,
                        }));
                    }

                    if (errorOpt !== null) {
                        messages.push(makeMsg({
                            role: 'error',
                            content: errorOpt,
                        }));
                    }

                    if (messages.length === 0) return;

                    cleanup();

                    const { container } = renderPanel({ state: { messages, sending: false, streaming: false, ready: true } });
                    const fieldCards = container.querySelectorAll('[data-testid="field-card"]');
                    const fieldTexts = Array.from(fieldCards).map(el => el.textContent || '');

                    for (const f of fields) {
                        const found = fieldTexts.some(t => t.includes(f.label) && t.includes(f.value));
                        expect(found).toBe(true);
                    }

                    const actionButtons = container.querySelectorAll('[data-testid="action-button"]');
                    expect(actionButtons.length).toBe(actions.length);

                    if (errorOpt !== null) {
                        expect(container.textContent).toContain(errorOpt);
                    }
                },
            ),
            { numRuns: 40 },
        );
    });
});
