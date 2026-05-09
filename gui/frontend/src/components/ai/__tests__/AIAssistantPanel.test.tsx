import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, cleanup, fireEvent, waitFor } from '@testing-library/react';
import * as fc from 'fast-check';
import { AIAssistantPanel } from '../AIAssistantPanel';
import type { ChatMessage, CancelAIAssistantResult, NewsCardData, ChatAction } from '../useAIAssistant';
import type { AgentView } from '../agentViewTypes';
import { DialogProvider } from '../../CustomDialog';

const { openFileOrShowInFolderMock, showItemInFolderMock } = vi.hoisted(() => ({
    openFileOrShowInFolderMock: vi.fn().mockResolvedValue(undefined),
    showItemInFolderMock: vi.fn().mockResolvedValue(undefined),
}));

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

// 閳光偓閳光偓 Mock Wails runtime (not used by panel but imported transitively) 閳光偓閳光偓
vi.mock('../../../../wailsjs/runtime', () => ({
    BrowserOpenURL: vi.fn(),
    EventsOn: vi.fn(),
    EventsOff: vi.fn(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    OpenFileOrShowInFolder: openFileOrShowInFolderMock,
    ShowItemInFolder: showItemInFolderMock,
    SelectProjectDir: vi.fn(),
    SetWorkflowWorkingDir: vi.fn(),
    SearchProjects: vi.fn().mockResolvedValue([]),
    ResumeProject: vi.fn(),
    RenameTask: vi.fn(),
    PinTask: vi.fn(),
    HideTask: vi.fn(),
    GetTTSEnabled: vi.fn().mockResolvedValue(false),
    SetTTSEnabled: vi.fn().mockResolvedValue(undefined),
    SpeakText: vi.fn().mockResolvedValue(undefined),
    LoadConfig: vi.fn().mockResolvedValue({}),
    IsASRReady: vi.fn().mockResolvedValue(false),
    TranscribeAudioBase64: vi.fn().mockResolvedValue(""),
    NormalizeVoiceCommand: vi.fn(async (text: string) => ({ is_command: true, corrected_text: text, confidence: 1 })),
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
            icon: overrides.icon ?? '棣冩憴',
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
    return render(<AIAssistantPanel {...props} />, { wrapper: DialogProvider });
}

describe('AIAssistantPanel property tests', () => {
    afterEach(() => {
        cleanup();
        window.localStorage.clear();
        scrollIntoViewMock.mockClear();
        scrollToMock.mockClear();
        openFileOrShowInFolderMock.mockReset();
        openFileOrShowInFolderMock.mockResolvedValue(undefined);
        showItemInFolderMock.mockReset();
        showItemInFolderMock.mockResolvedValue(undefined);
    });

    it('keeps inline root as a flex item with clipped overflow', () => {
        const { getByTestId } = renderPanel({
            window: { inline: true },
            state: { messages: [], sending: false, streaming: false, ready: true },
        });

        const root = getByTestId('ai-panel-root');
        expect(root.style.flex).toBe('1 1 0%');
        expect(root.style.minWidth).toBe('0px');
        expect(root.style.minHeight).toBe('0px');
        expect(root.style.boxSizing).toBe('border-box');
        expect(root.style.overflow).toBe('hidden');
    });

    it('keeps the title bar and input area boxed inside the inline panel', () => {
        const { getByTestId, getByText } = renderPanel({
            window: { inline: true },
            actions: {
                sendMessage: async () => {},
                clearHistory: async () => {},
                executeAction: async () => {},
                refreshNews: () => {},
            },
            state: { messages: [], sending: false, streaming: false, ready: true },
        });

        const titleBar = getByTestId('ai-title-bar');
        expect(titleBar.style.minWidth).toBe('0px');
        expect(titleBar.style.boxSizing).toBe('border-box');

        const toolsGroup = getByTestId('ai-titlebar-tools-group');
        expect(toolsGroup.style.minWidth).toBe('0px');

        const windowGroup = getByTestId('ai-titlebar-window-group');
        expect(windowGroup.style.flexShrink).toBe('0');
        expect(windowGroup.style.boxSizing).toBe('border-box');

        const inputBar = getByTestId('ai-input-bar');
        expect(inputBar.style.minWidth).toBe('0px');
        expect(inputBar.style.boxSizing).toBe('border-box');
    });

    it('keeps the inline panel body as the only scroll container wrapper', () => {
        const { getByTestId } = renderPanel({
            window: { inline: true },
            state: {
                messages: [
                    makeMsg({ role: 'user', content: 'Earlier question' }),
                    makeMsg({ role: 'assistant', content: 'Latest answer' }),
                ],
                sending: false,
                streaming: false,
                ready: true,
            },
        });

        const body = getByTestId('ai-panel-body');
        expect(body.style.flex).toBe('1 1 0%');
        expect(body.style.minHeight).toBe('0px');
        expect(body.style.overflow).toBe('hidden');

        const output = getByTestId('ai-output-container');
        expect(output.style.flex).toBe('1 1 0%');
        expect(output.style.minHeight).toBe('0px');
        expect(output.style.overflowY).toBe('auto');
        expect(output.style.overflowX).toBe('hidden');

        const inputBar = getByTestId('ai-input-bar');
        expect(inputBar.style.flexShrink).toBe('0');
    });

    it('renders AgentView as an operable right-side task panel and submits structured data', async () => {
        const submitAgentView = vi.fn().mockResolvedValue(undefined);
        const agentView: AgentView = {
            id: 'mis:intent:expense_submit',
            type: 'form',
            title: 'Expense reimbursement',
            fields: [
                { name: 'amount', label: 'Amount', type: 'number', value: 86, required: true },
                { name: 'reason', label: 'Reason', type: 'text', value: 'Taxi', required: true },
            ],
            submitLabel: 'Submit expense',
        };

        const { getByText, getByDisplayValue, getByRole } = renderPanel({
            window: { inline: true },
            state: {
                messages: [],
                sending: false,
                streaming: false,
                ready: true,
                agentView,
            },
            actions: {
                submitAgentView,
            },
        });

        expect(getByText('Expense reimbursement')).toBeTruthy();
        expect(getByText('Amount')).toBeTruthy();
        expect(getByText('Reason')).toBeTruthy();
        expect(getByDisplayValue('86')).toBeTruthy();
        expect(getByDisplayValue('Taxi')).toBeTruthy();

        fireEvent.click(getByRole('button', { name: 'Submit expense' }));

        await waitFor(() => {
            expect(submitAgentView).toHaveBeenCalledWith('mis:intent:expense_submit', {
                amount: 86,
                reason: 'Taxi',
            });
        });
    });

    it('keeps compact composer actions bottom-aligned before resizing', () => {
        const { getByTestId } = renderPanel({
            window: { inline: true },
            state: { messages: [], sending: false, streaming: false, ready: true },
        });

        const row = getByTestId('ai-input-row');
        const actions = getByTestId('ai-input-actions');
        const inputBar = getByTestId('ai-input-bar');

        expect(row.style.alignItems).toBe('flex-end');
        expect(actions.getAttribute('role')).toBe('group');
        expect(actions.getAttribute('aria-label')).toBe('Input actions');
        expect(actions.style.paddingTop).toBe('0px');
        expect(actions.style.paddingBottom).toBe('4px');
        expect(inputBar.style.flex).toBe('');
    });

    it('top-aligns input actions after the composer is resized taller', () => {
        const { getByTestId } = renderPanel({
            window: { inline: true },
            state: { messages: [], sending: false, streaming: false, ready: true },
        });

        const handle = getByTestId('ai-input-resize-handle');
        fireEvent.mouseDown(handle, { clientY: 300 });
        fireEvent.mouseMove(document, { clientY: 160 });
        fireEvent.mouseUp(document);

        const row = getByTestId('ai-input-row');
        const actions = getByTestId('ai-input-actions');
        const inputBar = getByTestId('ai-input-bar');

        expect(row.style.alignItems).toBe('flex-start');
        expect(actions.style.flexShrink).toBe('0');
        expect(actions.style.paddingTop).toBe('2px');
        expect(actions.style.paddingBottom).toBe('0px');
        expect(inputBar.style.flex).toBe('1 1 auto');
    });

    it('shows tutorial action before refresh in the title bar tools group', () => {
        const { getByTestId } = renderPanel({
            actions: {
                sendMessage: async () => {},
                clearHistory: async () => {},
                executeAction: async () => {},
                refreshNews: () => {},
            },
            state: { messages: [], sending: false, streaming: false, ready: true },
        });

        const toolsGroup = getByTestId('ai-titlebar-tools-group');
        const buttons = Array.from(toolsGroup.querySelectorAll('button'));
        expect(buttons).toHaveLength(5);
        expect(buttons[0]?.getAttribute('title')).toBe('Search tasks');
        expect(buttons[1]?.getAttribute('title')).toContain('Voice readback OFF');
        expect(buttons[2]?.getAttribute('title')).toBe('Switch to dark mode');
        expect(buttons[3]?.getAttribute('title')).toBe('Refresh news');
        expect(buttons[4]?.getAttribute('title')).toBe('Clear history');
    });

    it('shows a non-executing handoff in the group discussion menu', () => {
        const { getByLabelText, getByText } = renderPanel({
            groupDiscussion: {
                config: { enabled: true, discoverable: true },
                status: {
                    enabled: true,
                    discoverable: true,
                    experts: [{ agent_id: 'expert-a' }],
                    discussions: [{ id: 'disc-1', updated_at: '2026-05-07T10:00:00Z', ready_to_summarize: true }],
                    pending_invites: [],
                    active_discussion_count: 1,
                    ready_discussion_count: 1,
                },
                onRefreshStatus: async () => {},
                onPublishProfile: async () => {},
                onOpenExperienceTrace: () => {},
            },
        });

        fireEvent.click(getByLabelText('Group discussion (GD): Group Listed'));

        expect(getByText('Safe Handoff')).toBeTruthy();
        expect(getByText(/recommended_focus_context/)).toBeTruthy();
        expect(getByText(/"action": "get_detail"/)).toBeTruthy();
        expect(getByText(/"consultation_id": "disc-1"/)).toBeTruthy();
        expect(getByText(/non_executing_boundary/)).toBeTruthy();
        expect(getByText(/no discussion was started/)).toBeTruthy();
    });

    it('omits empty consultation id from group discussion status handoff fallback', () => {
        const { getByLabelText, getByText, queryByText } = renderPanel({
            groupDiscussion: {
                config: { enabled: true, discoverable: true },
                status: {
                    enabled: true,
                    discoverable: true,
                    experts: [],
                    discussions: [],
                    pending_invites: [],
                },
                onRefreshStatus: async () => {},
                onPublishProfile: async () => {},
            },
        });

        fireEvent.click(getByLabelText('Group discussion (GD): Group Listed'));

        expect(getByText(/"action": "status"/)).toBeTruthy();
        expect(queryByText(/consultation_id/)).toBeNull();
    });

    it('keeps pending invite status handoff read-only in the group discussion menu', () => {
        const { getByLabelText, getByText } = renderPanel({
            groupDiscussion: {
                config: { enabled: true, discoverable: true },
                status: {
                    enabled: true,
                    discoverable: true,
                    experts: [],
                    discussions: [],
                    pending_invites: [{ id: 'invite-1', session_id: 'disc-invite', from_id: 'expert-a', role: 'speak', topic: 'review rollout' }],
                },
                onRefreshStatus: async () => {},
                onPublishProfile: async () => {},
            },
        });

        fireEvent.click(getByLabelText('Group discussion (GD): Group Listed'));

        expect(getByText(/"recommended_action_kind": "review_pending_invites"/)).toBeTruthy();
        expect(getByText(/"recommended_invite_id": "invite-1"/)).toBeTruthy();
        expect(getByText(/"recommended_consultation_id": "disc-invite"/)).toBeTruthy();
        expect(getByText(/"action": "status"/)).toBeTruthy();
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

    it('keeps latest conversation visible after resizing the input area', async () => {
        const messages: ChatMessage[] = [
            makeMsg({ role: 'user', content: 'Earlier question' }),
            makeMsg({ role: 'assistant', content: 'Latest answer' }),
        ];

        const { getByTestId } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
        });

        scrollIntoViewMock.mockClear();
        const handle = getByTestId('ai-input-resize-handle');
        fireEvent.mouseDown(handle, { clientY: 300 });
        fireEvent.mouseMove(document, { clientY: 240 });
        fireEvent.mouseUp(document);

        await waitFor(() => {
            expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'auto' });
        });
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

    it('allows typing in the textarea while a foreground request is still sending even after streaming stops', () => {
        const { getByTestId } = renderPanel({
            state: { messages: [], sending: true, streaming: false, ready: true },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        expect(input.disabled).toBe(false);
        expect(input.readOnly).toBe(false);
    });

    it('shows thinking placeholder while the assistant is actively streaming', () => {
        const { getByTestId } = renderPanel({
            state: { messages: [], sending: true, streaming: true, ready: true, visualBusy: true },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        expect(input.placeholder).toBe('Thinking... (you can type ahead)');
    });

    it('shows processing placeholder and visible busy hint after streaming stops but the request is still active', () => {
        const { getAllByText, getByTestId, getByText } = renderPanel({
            state: { messages: [], sending: true, streaming: false, ready: true, visualBusy: false },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        expect(input.placeholder).toBe('Running tools... (you can type ahead)');
        expect(getByText('Running tools... (you can type ahead)')).toBeTruthy();
    });

    it('fires queued input into the next agent loop as supplementary context', async () => {
        localStorage.removeItem('ai_assistant_buffer_queue');
        const injectSupplementary = vi.fn().mockResolvedValue(true);
        const sendMessage = vi.fn().mockResolvedValue(undefined);
        const recordSubmittedPrompt = vi.fn();
        const { getByTestId, queryByTestId } = renderPanel({
            state: { messages: [], sending: true, streaming: true, ready: true },
            actions: {
                sendMessage,
                injectSupplementary,
                recordSubmittedPrompt,
            },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'guide this next' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await waitFor(() => expect(getByTestId('buffer-queue-panel')).toBeTruthy());
        const fireButton = document.querySelector('[data-testid^="fire-btn-"]') as HTMLButtonElement | null;
        expect(fireButton).toBeTruthy();
        fireEvent.click(fireButton!);

        await waitFor(() => expect(injectSupplementary).toHaveBeenCalledWith('guide this next'));
        expect(sendMessage).not.toHaveBeenCalled();
        await waitFor(() => expect(queryByTestId('buffer-queue-panel')).toBeNull());
    });

    it('moves a queued edit into the main composer and appends the edited text at the queue tail', async () => {
        localStorage.removeItem('ai_assistant_buffer_queue');
        const { getAllByText, getByTestId, getByText } = renderPanel({
            state: { messages: [], sending: true, streaming: true, ready: true },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'first queued draft' } });
        fireEvent.keyDown(input, { key: 'Enter' });
        fireEvent.change(input, { target: { value: 'second queued draft' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await waitFor(() => expect(getByText('first queued draft')).toBeTruthy());
        const editButton = document.querySelector('[data-testid^="edit-btn-"]') as HTMLButtonElement | null;
        expect(editButton).toBeTruthy();
        fireEvent.click(editButton!);

        await waitFor(() => expect((getByTestId('ai-input') as HTMLTextAreaElement).value).toBe('first queued draft'));
        expect(getByTestId('buffer-queue-panel').textContent).not.toContain('first queued draft');
        expect(getByText('second queued draft')).toBeTruthy();
        expect(document.querySelector('[data-testid^="buffer-entry-textarea-"]')).toBeNull();

        fireEvent.change(input, { target: { value: 'edited queued draft' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await waitFor(() => expect(getByText('edited queued draft')).toBeTruthy());
        const rowTexts = Array.from(document.querySelectorAll('[data-testid^="buffer-entry-"]'))
            .map((node) => node.textContent || '')
            .join('\n');
        expect(rowTexts.indexOf('second queued draft')).toBeLessThan(rowTexts.indexOf('edited queued draft'));
    });

    it('keeps a queued edit in the queue after Enter even when the assistant is idle', async () => {
        localStorage.setItem('ai_assistant_buffer_queue', JSON.stringify([
            { id: 'queued-idle-edit', text: 'queued before idle edit', attachments: [], createdAt: 1 },
        ]));
        const sendMessage = vi.fn().mockResolvedValue(undefined);
        const { getByTestId, getByText } = renderPanel({
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: { sendMessage },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        await waitFor(() => expect(getByText('queued before idle edit')).toBeTruthy());

        const editButton = document.querySelector('[data-testid^="edit-btn-"]') as HTMLButtonElement | null;
        expect(editButton).toBeTruthy();
        fireEvent.click(editButton!);
        await waitFor(() => expect(input.value).toBe('queued before idle edit'));

        fireEvent.change(input, { target: { value: 'edited while idle' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await waitFor(() => expect(getByText('edited while idle')).toBeTruthy());
        expect(sendMessage).not.toHaveBeenCalled();
    });

    it('clears a queued edit without sending when the edited content is empty', async () => {
        localStorage.setItem('ai_assistant_buffer_queue', JSON.stringify([
            { id: 'queued-empty-edit', text: 'clear me from queue', attachments: [], createdAt: 1 },
        ]));
        const sendMessage = vi.fn().mockResolvedValue(undefined);
        const { getByTestId, getByText, queryByTestId } = renderPanel({
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: { sendMessage },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        await waitFor(() => expect(getByText('clear me from queue')).toBeTruthy());

        const editButton = document.querySelector('[data-testid^="edit-btn-"]') as HTMLButtonElement | null;
        expect(editButton).toBeTruthy();
        fireEvent.click(editButton!);
        await waitFor(() => expect(input.value).toBe('clear me from queue'));

        fireEvent.change(input, { target: { value: '   ' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await waitFor(() => expect(queryByTestId('buffer-queue-panel')).toBeNull());
        expect(input.value).toBe('');
        expect(sendMessage).not.toHaveBeenCalled();

        fireEvent.change(input, { target: { value: 'fresh prompt after empty edit' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await waitFor(() => expect(sendMessage).toHaveBeenCalledWith('fresh prompt after empty edit'));
    });

    it('hides background launch control even if a background handler exists', () => {
        const { queryByTestId } = renderPanel({
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: {
                sendMessage: async () => {},
                sendMessageInBackground: async () => {},
                clearHistory: async () => {},
                executeAction: async () => {},
                refreshNews: () => {},
            },
        });

        expect(queryByTestId('ai-send-background')).toBeNull();
    });

    it('renders background launch identifiers inside a visible system message', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'system',
                content: [
                    '瀹歌尪娴嗛崚鏉挎倵閸欐媽绻嶇悰灞烩偓?',
                    '娴犺濮熸导姘▔缁€鍝勬躬閳ユ粈鎹㈤崝锛勵吀閻炲棌鈧繈鍣烽惃鍕倵閸欐澘鍨悰銊ｂ偓?',
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
                    '瀹歌尪娴嗛崚鏉挎倵閸欐媽绻嶇悰灞烩偓?',
                    '娴犺濮熸导姘▔缁€鍝勬躬閳ユ粈鎹㈤崝锛勵吀閻炲棌鈧繈鍣烽惃鍕倵閸欐澘鍨悰銊ｂ偓?',
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

    it('renders structured unfinished slot cards and actions', async () => {
        const executeAction = vi.fn<() => Promise<void>>().mockResolvedValue();
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '濡偓濞村鍩屾稉鈧稉顏呮弓鐎瑰本鍨氭禒璇插閵?',
                unfinishedSlot: {
                    slotID: 'slot-1',
                    title: '缂佈呯敾 Daily Paper',
                    summary: '鏉╂ê妯婇張鈧崥搴濈鏉烆喗鏆ｉ悶?',
                    projectPath: 'D:/work/project',
                    status: 'pending_resume',
                    actions: [
                        { label: '缂佈呯敾娑撳﹥顐兼禒璇插', command: '__resume_unfinished__ slot-1', style: 'default' },
                        { label: '瀵偓婵鏌婃禒璇插', command: '__start_new_task__', style: 'default' },
                    ],
                },
            }),
        ];

        const { getByTestId, getByText } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction, refreshNews: () => {} },
        });

        expect(getByTestId('unfinished-slot-card')).toBeTruthy();
        expect(getByTestId('unfinished-slot-title').textContent).toContain('缂佈呯敾 Daily Paper');
        expect(getByTestId('unfinished-slot-summary').textContent).toContain('鏉╂ê妯婇張鈧崥搴濈鏉烆喗鏆ｉ悶?');
        expect(getByTestId('unfinished-slot-project').textContent).toContain('D:/work/project');
        expect(getByTestId('unfinished-slot-status').textContent).toBe('Status: pending resume');
        expect(getByText('缂佈呯敾娑撳﹥顐兼禒璇插')).toBeTruthy();
        expect(getByText('瀵偓婵鏌婃禒璇插')).toBeTruthy();
    });

    it('localizes unfinished slot status in Chinese', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '濡偓濞村鍩屾稉鈧稉顏呮弓鐎瑰本鍨氭禒璇插閵?',
                unfinishedSlot: {
                    slotID: 'slot-zh',
                    title: '缂佈呯敾閺冄傛崲閸?',
                    status: 'resumed',
                },
            }),
        ];

        const { getByTestId } = renderPanel({
            lang: 'zh-Hans',
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction: async () => {}, refreshNews: () => {} },
        });

        expect(getByTestId('unfinished-slot-status').textContent).toBe('状态：已恢复');
    });

    it('unfinished slot card buttons reuse executeAction', async () => {
        const executeAction = vi.fn<() => Promise<void>>().mockResolvedValue();
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '濡偓濞村鍩屾稉鈧稉顏呮弓鐎瑰本鍨氭禒璇插閵?',
                unfinishedSlot: {
                    slotID: 'slot-2',
                    title: '缂佈呯敾閺冄傛崲閸?',
                    actions: [
                        { label: '缂佈呯敾娑撳﹥顐兼禒璇插', command: '__resume_unfinished__ slot-2', style: 'default' },
                    ],
                },
            }),
        ];

        const { getByText } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction, refreshNews: () => {} },
        });

        fireEvent.click(getByText('缂佈呯敾娑撳﹥顐兼禒璇插'));

        await waitFor(() => {
            expect(executeAction).toHaveBeenCalledWith('__resume_unfinished__ slot-2');
        });
    });

    it('unfinished slot action buttons size to text without overflowing the card', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: 'unfinished task',
                unfinishedSlot: {
                    slotID: 'slot-layout',
                    title: 'layout check',
                    actions: [
                        { label: '缂佈呯敾娑撳﹥顐兼禒璇插', command: '__resume_unfinished__ slot-layout', style: 'default' },
                        { label: 'Run a very long new task button label', command: '__start_new_task__', style: 'default' },
                    ],
                },
            }),
        ];

        const { getAllByTestId } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction: async () => {}, refreshNews: () => {} },
        });

        for (const button of getAllByTestId('action-button')) {
            expect(button.style.width).toBe('auto');
            expect(button.style.height).toBe('auto');
            expect(button.style.maxWidth).toBe('100%');
            expect(button.style.whiteSpace).toBe('normal');
            expect(button.style.overflowWrap).toBe('anywhere');
        }
    });

    it('unfinished slot project path opens through the shared file handler', async () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '鐎涙ê婀張顏勭暚閹存劒鎹㈤崝掳鈧?',
                unfinishedSlot: {
                    slotID: 'slot-path',
                    summary: 'previous task stopped here',
                    projectPath: 'D:/work/project',
                },
            }),
        ];

        const { getByTitle } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction: async () => {}, refreshNews: () => {} },
        });

        fireEvent.click(getByTitle('D:/work/project'));

        await waitFor(() => {
            expect(openFileOrShowInFolderMock).toHaveBeenCalledWith('D:/work/project');
        });
    });

    it('renders confirmation cards with summary, lists, and actions', () => {
        const executeAction = vi.fn<() => Promise<void>>().mockResolvedValue();
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '鐠囧嘲鍘涚涵顔款吇閸氬骸鍟€閹笛嗩攽閵?',
                confirmation: {
                    id: 'c1',
                    summary: '閹存垹鎮婄憴锝勭稑閹厖鎱ㄦ径宥囨瑜版洟妫舵０姒巒姒涙顓诲銉ょ稊閻╊喖缍嶉敍娆?/work/project',
                    taskType: 'coding',
                    targetPaths: ['D:/work/project'],
                    plannedActions: ['check login flow', 'modify related code'],
                    riskFlags: ['will edit code directly'],
                    revisionHints: ['correct the directory if needed'],
                    status: 'pending',
                },
                actions: [
                    { label: 'Confirm and start', command: 'confirm and start', style: 'default' },
                    { label: 'Cancel', command: 'cancel this task', style: 'danger' },
                ],
            }),
        ];

        const { getByTestId, getByText } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction, refreshNews: () => {} },
        });

        expect(getByTestId('confirmation-card')).toBeTruthy();
        expect(getByTestId('confirmation-summary').textContent).toContain('/work/project');
        expect(getByTestId('confirmation-target-paths').textContent).toContain('D:/work/project');
        expect(getByTestId('confirmation-planned-actions').textContent).toContain('check login flow');
        expect(getByTestId('confirmation-risk-flags').textContent).toContain('will edit code directly');
        expect(getByTestId('confirmation-revision-hints').textContent).toContain('correct the directory if needed');
        expect(getByTestId('confirmation-status').textContent).toContain('pending');
        expect(getByText('Confirm and start')).toBeTruthy();
        expect(getByText('Cancel')).toBeTruthy();
    });

    it('confirmation card buttons reuse executeAction', async () => {
        const executeAction = vi.fn<() => Promise<void>>().mockResolvedValue();
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '鐠囬鈥樼拋銈冣偓?',
                confirmation: {
                    id: 'c2',
                    summary: '绾喛顓婚惄顔肩秿閸滃奔鎹㈤崝鈥虫倵閸愬秵澧界悰?',
                },
                actions: [
                    { label: 'Confirm and start', command: 'confirm and start', style: 'default' },
                ],
            }),
        ];

        const { getByText } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction, refreshNews: () => {} },
        });

        fireEvent.click(getByText('Confirm and start'));

        await waitFor(() => {
            expect(executeAction).toHaveBeenCalledWith('confirm and start');
        });
    });

    it('renders trace summary and counts as assistant field cards', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: 'done',
                fields: [
                    { label: 'Trace', value: 'trial loop stabilized after one retry' },
                    { label: 'Recovery', value: 'Recovered after retry' },
                    { label: 'Failures', value: '1' },
                    { label: 'Trace events', value: '8' },
                    { label: 'Evidence', value: '3' },
                    { label: 'Run ID', value: 'run-trace-1' },
                    { label: 'Job ID', value: 'job-trace-1' },
                ],
                actions: [{ label: 'View trace', command: '__view_trace__ run-trace-1', style: 'default' }],
            }),
        ];

        const executeAction = vi.fn<() => Promise<void>>().mockResolvedValue();
        const { container, getByText, getByTestId } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction, refreshNews: () => {} },
        });

        const fieldCards = Array.from(container.querySelectorAll('[data-testid="field-card"]')).map(el => el.textContent || '');
        expect(fieldCards).toContain('Trace:trial loop stabilized after one retry');
        expect(fieldCards).toContain('Recovery:Recovered after retry');
        expect(fieldCards).toContain('Failures:1');
        expect(fieldCards).toContain('Trace events:8');
        expect(fieldCards).toContain('Evidence:3');
        expect(fieldCards).toContain('Run ID:run-trace-1');
        expect(fieldCards).toContain('Job ID:job-trace-1');
        expect(getByTestId('recovery-badge').textContent).toBe('Recovered after retry');
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
                    { label: 'Recovery', value: 'Recovered after retry' },
                    { label: 'Failures', value: '1' },
                    { label: 'Job ID', value: 'job-trace-1' },
                    { label: 'Trace events', value: '8' },
                    { label: 'Evidence', value: '3' },
                    { label: 'Status', value: 'completed' },
                ],
            }),
        ];

        const { container, getByText, getAllByTestId } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
        });

        const fieldCards = Array.from(container.querySelectorAll('[data-testid="field-card"]')).map(el => el.textContent || '');
        expect(fieldCards).toContain('Run ID:run-trace-1');
        expect(fieldCards).toContain('Recovery:Recovered after retry');
        expect(fieldCards).toContain('Failures:1');
        expect(fieldCards).toContain('Job ID:job-trace-1');
        expect(fieldCards).toContain('Trace events:8');
        expect(fieldCards).toContain('Evidence:3');
        expect(fieldCards).toContain('Status:completed');
        expect(getAllByTestId('recovery-badge')[0].textContent).toBe('Recovered after retry');
        expect(getByText('Trace details for run-trace-1')).toBeTruthy();
        expect(getByText('Summary: trial loop stabilized after one retry')).toBeTruthy();
    });

    it('renders progress messages for grace-round wrap-up status when messages exist', () => {
        const { getByText } = renderPanel({
            state: {
                messages: [makeMsg({ role: 'user', content: 'make pdf' })],
                progressMessages: [makeMsg({ role: 'progress', content: '閳?瀹稿弶甯存潻鎴炴付婢堆勫腹閻炲棜鐤嗗▎鈽呯礉濮濓絽婀崺杞扮艾閻滅増婀佹穱鈩冧紖閺€璺虹啲楠炲墎鏁撻幋鎰付缂佸牏绮ㄩ弸婧锯偓?' })],
                sending: true,
                streaming: false,
                ready: true,
            },
        });

        expect(getByText('閳?瀹稿弶甯存潻鎴炴付婢堆勫腹閻炲棜鐤嗗▎鈽呯礉濮濓絽婀崺杞扮艾閻滅増婀佹穱鈩冧紖閺€璺虹啲楠炲墎鏁撻幋鎰付缂佸牏绮ㄩ弸婧锯偓?')).toBeTruthy();
    });

    it('renders coding agent progress as a visible task status row', () => {
        const { getAllByText, getByTestId, getByText } = renderPanel({
            lang: 'zh-Hans',
            state: {
                messages: [makeMsg({ role: 'user', content: 'fix this bug' })],
                progressMessages: [makeMsg({ role: 'progress', content: 'Coding Agent: running T2 - Fix stale edit guard' })],
                sending: true,
                streaming: false,
                ready: true,
            },
        });

        const status = getByTestId('coding-agent-progress');
        const titleStatus = getByTestId('coding-agent-title-status');
        expect(status.textContent).toContain('编程智能体');
        expect(status.textContent).toContain('执行中');
        expect(titleStatus.textContent).toContain('编程智能体');
        expect(titleStatus.textContent).toContain('执行中');
        expect(titleStatus.textContent).toContain('T2');
        expect(titleStatus.style.color).toBe('rgb(37, 99, 235)');
        expect(titleStatus.getAttribute('role')).toBe('status');
        expect(titleStatus.getAttribute('aria-live')).toBe('polite');
        expect(titleStatus.getAttribute('data-agent')).toBe('coding');
        expect(titleStatus.getAttribute('data-active')).toBe('true');
        expect(titleStatus.getAttribute('data-phase')).toBe('running');
        expect(titleStatus.getAttribute('data-terminal')).toBe('false');
        expect(titleStatus.getAttribute('data-task-id')).toBe('T2');
        expect(titleStatus.getAttribute('data-variant')).toBe('title-bar');
        expect(titleStatus.className).toContain('coding-agent-status--title-bar');
        expect(titleStatus.className).toContain('coding-agent-status--running');
        expect(titleStatus.getAttribute('aria-label')).toContain('Fix stale edit guard');
        expect(status.getAttribute('data-agent')).toBe('coding');
        expect(status.getAttribute('data-active')).toBe('true');
        expect(status.getAttribute('data-phase')).toBe('running');
        expect(status.getAttribute('data-terminal')).toBe('false');
        expect(status.getAttribute('data-task-id')).toBe('T2');
        expect(status.getAttribute('data-variant')).toBe('chat-progress');
        expect(status.className).toContain('coding-agent-status--chat-progress');
        expect(status.className).toContain('coding-agent-status--running');
        expect(status.getAttribute('aria-label')).toContain('Fix stale edit guard');
        expect((getByTestId('ai-input') as HTMLTextAreaElement).placeholder).toContain('编程智能体');
        expect((getByTestId('ai-input') as HTMLTextAreaElement).placeholder).toContain('T2');
        expect(getByText('T2')).toBeTruthy();
        expect(getByText('Fix stale edit guard')).toBeTruthy();
    });

    it('keeps completed coding agent history out of the title monitor once idle', () => {
        const { getByTestId, queryByTestId } = renderPanel({
            lang: 'zh-Hans',
            state: {
                messages: [makeMsg({ role: 'user', content: 'fix this bug' })],
                progressMessages: [makeMsg({ role: 'progress', content: 'Coding Agent: completed T2 - Fix stale edit guard' })],
                sending: false,
                streaming: false,
                ready: true,
            },
        });

        expect(getByTestId('coding-agent-progress').textContent).toContain('已完成');
        expect(queryByTestId('coding-agent-title-status')).toBeNull();
    });

    it('renders a terminal fallback message with trace action and fields', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '娴犺濮熼張顏勭暚閹存劕褰叉禍銈勭帛缂佹挻鐏夐妴渚綝F generation failed after tool execution',
                fields: [
                    { label: 'Trace', value: 'PDF generation failed after tool execution' },
                    { label: 'Trace events', value: '4' },
                    { label: 'Evidence', value: '2' },
                    { label: 'Run ID', value: 'run-empty-result' },
                ],
                actions: [{ label: 'View trace', command: '__view_trace__ run-empty-result', style: 'default' }],
            }),
        ];

        const executeAction = vi.fn<() => Promise<void>>().mockResolvedValue();
        const { container, getByText } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction, refreshNews: () => {} },
        });

        expect(getByText('娴犺濮熼張顏勭暚閹存劕褰叉禍銈勭帛缂佹挻鐏夐妴渚綝F generation failed after tool execution')).toBeTruthy();
        const fieldCards = Array.from(container.querySelectorAll('[data-testid="field-card"]')).map(el => el.textContent || '');
        expect(fieldCards).toContain('Trace:PDF generation failed after tool execution');
        expect(fieldCards).toContain('Trace events:4');
        expect(fieldCards).toContain('Evidence:2');
        expect(fieldCards).toContain('Run ID:run-empty-result');
        expect(getByText('View trace')).toBeTruthy();
    });

    it('renders a saved file link when only localFilePath is present', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '',
                localFilePath: '/tmp/review.pdf',
            }),
        ];

        const { getByTitle } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
        });

        expect(getByTitle('/tmp/review.pdf')).toBeTruthy();
    });

    it('opens saved file paths directly when clicked', async () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '',
                localFilePath: 'C:\\Users\\demo\\report.pdf',
            }),
        ];

        const { getByTitle } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
        });

        fireEvent.click(getByTitle('C:\\Users\\demo\\report.pdf'));

        await waitFor(() => {
            expect(openFileOrShowInFolderMock).toHaveBeenCalledWith('C:\\Users\\demo\\report.pdf');
        });
    });

    it('opens inline detected file paths when clicked', async () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '閺傚洣娆㈤崷?C:\\Users\\demo\\report.pdf閿涘矁顕幍鎾崇磻閵?',
            }),
        ];

        const { getByTitle } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
        });

        fireEvent.click(getByTitle('C:\\Users\\demo\\report.pdf'));

        await waitFor(() => {
            expect(openFileOrShowInFolderMock).toHaveBeenCalledWith('C:\\Users\\demo\\report.pdf');
        });
    });

    it('opens screenshot thumbnails via the same file handler when clicked', async () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '',
                localFilePath: 'C:\\Users\\demo\\capture.png',
                thumbnailBase64: 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+yF9kAAAAASUVORK5CYII=',
            }),
        ];

        const { getByAltText } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
        });

        fireEvent.click(getByAltText('screenshot'));

        await waitFor(() => {
            expect(openFileOrShowInFolderMock).toHaveBeenCalledWith('C:\\Users\\demo\\capture.png');
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
            />,
            { wrapper: DialogProvider }
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

        expect(getByTestId('ai-cancel-progress').textContent).not.toContain('閳?');
        expect(queryByTitle('Send')).toBeNull();
    });

    it('allows typing in the textarea while the request is still in flight', () => {
        const { getByTestId } = renderPanel({
            state: { messages: [], sending: true, streaming: false, visualBusy: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction: async () => {}, refreshNews: () => {}, cancelSession: async () => ({ canceledText: '' }) },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        expect(input.disabled).toBe(false);
        expect(input.readOnly).toBe(false);
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

        expect(getByTestId('ai-cancel-progress').textContent).not.toContain('閳?');
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
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction: async () => {}, refreshNews: () => {} },
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
            { numRuns: 12 },
        );
    });
});
