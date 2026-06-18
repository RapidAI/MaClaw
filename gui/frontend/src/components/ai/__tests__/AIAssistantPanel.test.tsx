import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, cleanup, fireEvent, waitFor, act } from '@testing-library/react';
import * as fc from 'fast-check';
import { AIAssistantPanel, canShowAssistantCodingPreviewForTab } from '../AIAssistantPanel';
import { openCurrentTenantCardStore } from '../AssistantTitleBar';
import type { ChatMessage, CancelAIAssistantResult, NewsCardData, ChatAction } from '../useAIAssistant';
import type { AgentView } from '../agentViewTypes';
import { DialogProvider } from '../../CustomDialog';

const { openFileOrShowInFolderMock, showItemInFolderMock, loadProjectContextMock, createProjectTabSessionMock, listVirtualEmployeesMock, initiateVEConversationMock, addVEToGroupMock, renameGroupDiscussionMock, runtimeEventsOnMock, runtimeEventsOffMock } = vi.hoisted(() => ({
    openFileOrShowInFolderMock: vi.fn().mockResolvedValue(undefined),
    showItemInFolderMock: vi.fn().mockResolvedValue(undefined),
    loadProjectContextMock: vi.fn().mockResolvedValue({ project_name: '', recent_progress: '', key_artifacts: [] }),
    createProjectTabSessionMock: vi.fn().mockResolvedValue(undefined),
    listVirtualEmployeesMock: vi.fn().mockResolvedValue([
        { id: 've-a', machine_id: 've-a', name: 'Agent A', online_status: 'online', status: 'online', access_policy: 'public', skill_description: 'Contracts' },
        { id: 've-b', machine_id: 've-b', name: 'Contract Bot', online_status: 'online', status: 'online', access_policy: 'public', skill_description: 'Contracts' },
    ]),
    initiateVEConversationMock: vi.fn().mockResolvedValue({ session_id: 'session-ve-a' }),
    addVEToGroupMock: vi.fn().mockResolvedValue(undefined),
    renameGroupDiscussionMock: vi.fn().mockResolvedValue({ id: 'disc-1', topic: 'Renamed group' }),
    runtimeEventsOnMock: vi.fn(),
    runtimeEventsOffMock: vi.fn(),
}));

const scrollIntoViewMock = vi.fn();
const scrollToMock = vi.fn();
const originalCreateObjectURL = Object.getOwnPropertyDescriptor(URL, 'createObjectURL');
const originalRevokeObjectURL = Object.getOwnPropertyDescriptor(URL, 'revokeObjectURL');

Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {
    configurable: true,
    value: scrollIntoViewMock,
});

Object.defineProperty(HTMLElement.prototype, 'scrollTo', {
    configurable: true,
    value: scrollToMock,
});

// Mock Wails runtime (not used by panel but imported transitively).
vi.mock('../../../../wailsjs/runtime', () => ({
    BrowserOpenURL: vi.fn(),
    EventsOn: runtimeEventsOnMock,
    EventsOff: runtimeEventsOffMock,
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    OpenFileOrShowInFolder: openFileOrShowInFolderMock,
    ShowItemInFolder: showItemInFolderMock,
    SelectProjectDir: vi.fn(),
    SetWorkflowWorkingDir: vi.fn(),
    LoadProjectContext: loadProjectContextMock,
    SearchProjects: vi.fn().mockResolvedValue([]),
    ResumeProject: vi.fn(),
    CreateProjectTabSession: createProjectTabSessionMock,
    RenameTask: vi.fn(),
    PinTask: vi.fn(),
    HideTask: vi.fn(),
    GroupDiscussionGetConsultationDetail: vi.fn().mockResolvedValue({ discussion: { id: 'disc-1', topic: 'Vendor audit', status: 'open', local_relation: 'owned_ve_invited', readonly: true, participant_ids: ['ve-a'] }, messages: [] }),
    GroupDiscussionRenameConsultation: renameGroupDiscussionMock,
    GroupDiscussionSendHistoryMessage: vi.fn().mockResolvedValue(undefined),
    GroupDiscussionDownloadAttachment: vi.fn().mockResolvedValue({ local_path: 'D:/maclaw/data/file.pdf' }),
    ListVirtualEmployees: listVirtualEmployeesMock,
    InitiateVEConversation: initiateVEConversationMock,
    AddVEToGroup: addVEToGroupMock,
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
            icon: overrides.icon ?? '\u{1F4F0}',
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

function mockURLObjectURLs(value: string) {
    Object.defineProperty(URL, 'createObjectURL', {
        configurable: true,
        value: vi.fn(() => value),
    });
    Object.defineProperty(URL, 'revokeObjectURL', {
        configurable: true,
        value: vi.fn(),
    });
}

describe('AIAssistantPanel property tests', () => {
    afterEach(() => {
        cleanup();
        vi.useRealTimers();
        window.localStorage.clear();
        scrollIntoViewMock.mockClear();
        scrollToMock.mockClear();
        openFileOrShowInFolderMock.mockReset();
        openFileOrShowInFolderMock.mockResolvedValue(undefined);
        showItemInFolderMock.mockReset();
        showItemInFolderMock.mockResolvedValue(undefined);
        loadProjectContextMock.mockReset();
        loadProjectContextMock.mockResolvedValue({ project_name: '', recent_progress: '', key_artifacts: [] });
        createProjectTabSessionMock.mockReset();
        createProjectTabSessionMock.mockResolvedValue(undefined);
        listVirtualEmployeesMock.mockReset();
        listVirtualEmployeesMock.mockResolvedValue([
            { id: 've-a', machine_id: 've-a', name: 'Agent A', online_status: 'online', status: 'online', access_policy: 'public', skill_description: 'Contracts' },
            { id: 've-b', machine_id: 've-b', name: 'Contract Bot', online_status: 'online', status: 'online', access_policy: 'public', skill_description: 'Contracts' },
        ]);
        initiateVEConversationMock.mockReset();
        initiateVEConversationMock.mockResolvedValue({ session_id: 'session-ve-a' });
        addVEToGroupMock.mockReset();
        addVEToGroupMock.mockResolvedValue(undefined);
        renameGroupDiscussionMock.mockReset();
        renameGroupDiscussionMock.mockResolvedValue({ id: 'disc-1', topic: 'Renamed group' });
        if (originalCreateObjectURL) Object.defineProperty(URL, 'createObjectURL', originalCreateObjectURL);
        else delete (URL as any).createObjectURL;
        if (originalRevokeObjectURL) Object.defineProperty(URL, 'revokeObjectURL', originalRevokeObjectURL);
        else delete (URL as any).revokeObjectURL;
    });

    it('does not allow coding preview panes on digital employee tabs', () => {
        expect(canShowAssistantCodingPreviewForTab({ type: 'local' })).toBe(true);
        expect(canShowAssistantCodingPreviewForTab({ type: 'project' })).toBe(true);
        expect(canShowAssistantCodingPreviewForTab({ type: 've' })).toBe(false);
        expect(canShowAssistantCodingPreviewForTab({ type: 'group' })).toBe(false);
    });

    it('keeps local coding preview off digital employee tabs and restores it on local tab', async () => {
        const props = defaultPanelProps();
        props.window = { inline: true };
        const { getByRole, getByTestId, queryByTestId, rerender } = render(<AIAssistantPanel {...props} />, { wrapper: DialogProvider });
        const codeFileHandler = runtimeEventsOnMock.mock.calls.filter(([eventName]) => eventName === 'code:file_update').at(-1)?.[1];
        expect(codeFileHandler).toBeTypeOf('function');

        act(() => codeFileHandler({
            file_path: '/tmp/hello/main.cpp',
            file_name: 'main.cpp',
            content: '#include <iostream>\nint main(){ std::cout << "Hello, World!"; }\n',
            op_type: 'create',
            language: 'cpp',
            session_id: 'local-coding-session',
        }));

        await waitFor(() => expect(getByTestId('code-preview-header')).toBeTruthy());

        rerender(<AIAssistantPanel
            {...props}
            pendingVEOpen={{
                id: 've-a',
                machine_id: 've-a',
                name: 'Agent A',
                online_status: 'online',
                status: 'active',
                access_policy: 'public',
                skill_description: '',
            } as any}
            onPendingVEOpenHandled={vi.fn()}
        />);

        await waitFor(() => expect(getByRole('tab', { name: 'Agent A' })).toBeTruthy());
        act(() => codeFileHandler({
            file_path: '/tmp/other-project/app.cpp',
            file_name: 'app.cpp',
            content: 'int polluted_project_preview = 1;',
            op_type: 'create',
            language: 'cpp',
            session_id: 'other-project-session',
            project_path: 'D:/other-project',
        }));
        act(() => codeFileHandler({
            file_path: '/tmp/hello/main.cpp',
            file_name: 'main.cpp',
            content: '#include <iostream>\nint main(){ std::cout << "Hello from hidden local"; }\n',
            op_type: 'modify',
            language: 'cpp',
            session_id: 'local-coding-session',
        }));
        await waitFor(() => expect(queryByTestId('code-preview-header')).toBeNull());

        rerender(<AIAssistantPanel
            {...props}
            pendingVEOpen={null}
            pendingProjectTabOpen={{ projectPath: 'D:/next-project', taskTitle: 'Next project', autoSend: false }}
            onPendingProjectTabOpenHandled={vi.fn()}
        />);
        await waitFor(() => expect(getByRole('tab', { name: 'Next project' })).toBeTruthy());

        fireEvent.click(getByTestId('ai-tab-local'));
        await waitFor(() => expect(getByTestId('code-preview-header')).toBeTruthy());
        expect(document.body.textContent || '').toContain('Hello from hidden local');
        expect(document.body.textContent || '').not.toContain('polluted_project_preview');
    });

    it('falls back to local preview ownership after closing the project owner tab', async () => {
        const props = defaultPanelProps();
        props.window = { inline: true };
        const { getByRole, getByTestId, queryByTestId, rerender } = render(<AIAssistantPanel {...props} />, { wrapper: DialogProvider });

        rerender(<AIAssistantPanel
            {...props}
            pendingProjectTabOpen={{ projectPath: 'D:/owner-project', taskTitle: 'Owner project', autoSend: false }}
            onPendingProjectTabOpenHandled={vi.fn()}
        />);
        await waitFor(() => expect(getByRole('tab', { name: 'Owner project' })).toBeTruthy());

        const projectTab = document.querySelector('[data-testid^="ai-tab-proj-"]') as HTMLElement | null;
        expect(projectTab).toBeTruthy();
        const projectTabId = projectTab!.getAttribute('data-testid')!.replace('ai-tab-', '');
        const latestCodeFileHandler = () => runtimeEventsOnMock.mock.calls.filter(([eventName]) => eventName === 'code:file_update').at(-1)?.[1];

        act(() => latestCodeFileHandler()({
            file_path: 'D:/owner-project/main.cpp',
            file_name: 'main.cpp',
            content: 'int project_owner_preview = 1;',
            op_type: 'create',
            language: 'cpp',
            session_id: 'project-owner-session',
            project_path: 'D:/owner-project',
        }));
        await waitFor(() => expect(getByTestId('code-preview-header')).toBeTruthy());
        expect(document.body.textContent || '').toContain('project_owner_preview');

        fireEvent.click(getByTestId(`ai-tab-close-${projectTabId}`));
        await waitFor(() => expect(queryByTestId(`ai-tab-${projectTabId}`)).toBeNull());
        await waitFor(() => expect(document.body.textContent || '').not.toContain('project_owner_preview'));

        act(() => latestCodeFileHandler()({
            file_path: 'D:/owner-project/stale.cpp',
            file_name: 'stale.cpp',
            content: 'int stale_project_after_close = 2;',
            op_type: 'modify',
            language: 'cpp',
            session_id: 'project-owner-session',
            project_path: 'D:/owner-project',
        }));
        expect(document.body.textContent || '').not.toContain('stale_project_after_close');

        act(() => latestCodeFileHandler()({
            file_path: '/tmp/local.cpp',
            file_name: 'local.cpp',
            content: 'int local_after_project_close = 3;',
            op_type: 'create',
            language: 'cpp',
            session_id: 'local-after-close-session',
        }));
        await waitFor(() => expect(getByTestId('code-preview-header')).toBeTruthy());
        expect(document.body.textContent || '').toContain('local_after_project_close');
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
        const { getByTestId } = renderPanel({
            window: { inline: true },
            actions: {
                sendMessage: async () => {},
                clearHistory: async () => {},
                executeAction: async () => {},
                refreshNews: () => {},
            },
            state: { messages: [{ id: 'msg-1', role: 'user', content: 'hello' }], sending: false, streaming: false, ready: true },
        });

        const titleBar = getByTestId('ai-title-bar');
        expect(titleBar.style.minWidth).toBe('0px');
        expect(titleBar.style.boxSizing).toBe('border-box');

        const toolsGroup = getByTestId('ai-titlebar-tools-group');
        expect(toolsGroup.style.minWidth).toBe('0px');

        const windowGroup = getByTestId('ai-titlebar-window-group');
        expect(windowGroup.style.flexShrink).toBe('0');
        expect(windowGroup.style.boxSizing).toBe('border-box');

        const inputStack = getByTestId('ai-input-stack');
        expect(inputStack.style.getPropertyValue('--wails-draggable')).toBe('no-drag');

        const inputBar = getByTestId('ai-input-bar');
        expect(inputBar.style.minWidth).toBe('0px');
        expect(inputBar.style.boxSizing).toBe('border-box');
        expect(inputBar.style.getPropertyValue('--wails-draggable')).toBe('no-drag');

        const inputRow = getByTestId('ai-input-row');
        expect(inputRow.style.getPropertyValue('--wails-draggable')).toBe('no-drag');

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        expect(input.style.getPropertyValue('--wails-draggable')).toBe('no-drag');
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
        expect(output.style.whiteSpace).toBe('normal');
        expect(output.style.wordBreak).toBe('normal');
        expect(output.style.overflowWrap).toBe('anywhere');

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
        const toolbar = getByTestId('ai-input-toolbar');
        const inputBar = getByTestId('ai-input-bar');

        expect(row.style.alignItems).toBe('flex-start');
        expect(toolbar.querySelector('[role="group"]')!.getAttribute('aria-label')).toBe('Input actions');
        expect(inputBar.style.flex).toBe('');
    });

    it('top-aligns input actions after the composer is resized taller', () => {
        const { getByTestId } = renderPanel({
            window: { inline: true },
            state: { messages: [{ id: 'msg-1', role: 'user', content: 'hello' }], sending: false, streaming: false, ready: true },
        });

        const handle = getByTestId('ai-input-resize-handle');
        fireEvent.mouseDown(handle, { clientY: 300 });
        fireEvent.mouseMove(document, { clientY: 160 });
        fireEvent.mouseUp(document);

        const row = getByTestId('ai-input-row');
        const toolbar = getByTestId('ai-input-toolbar');
        const inputBar = getByTestId('ai-input-bar');

        expect(row.style.alignItems).toBe('flex-start');
        expect(toolbar).toBeTruthy();
        expect(inputBar.style.flex).toBe('1 1 auto');
    });

    it('shows card store action before search in the title bar tools group', () => {
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
        expect(buttons).toHaveLength(7);
        expect(buttons[0]?.getAttribute('title')).toBe('Buy service redemption cards');
        expect(buttons[1]?.getAttribute('title')).toBe('Search tasks');
        expect(buttons[2]?.getAttribute('title')).toContain('Voice readback OFF');
        expect(buttons[3]?.getAttribute('title')).toBe('Switch to dark mode');
        expect(buttons[4]?.getAttribute('title')).toBe('Knowledge Base');
        expect(buttons[5]?.getAttribute('title')).toBe('Refresh news');
        expect(buttons[6]?.getAttribute('title')).toBe('New conversation');
    });

    it('opens current tenant card store URL from config', async () => {
        const openURL = vi.fn();

        await openCurrentTenantCardStore(async () => ({
            remote_hub_url: 'https://hub.example.com/',
            remote_tenant_id: 'tenant acme',
            remote_email: 'dev@example.com',
            remote_viewer_token: 'viewer token',
        }), openURL);

        expect(openURL).toHaveBeenCalledWith('https://hub.example.com/card_store?tenant_id=tenant%20acme&email=dev%40example.com#token=viewer%20token');
    });

    it('opens current tenant HubCenter compute store URL when hub id is configured', async () => {
        const openURL = vi.fn();

        await openCurrentTenantCardStore(async () => ({
            remote_hub_id: 'hub_1',
            remote_hub_url: 'https://hub.example.com/',
            remote_hubcenter_url: 'https://hubs.example.com/',
            remote_tenant_id: 'tenant acme',
            remote_email: 'dev@example.com',
            remote_viewer_token: 'viewer token',
        }), openURL);

        expect(openURL).toHaveBeenCalledWith('https://hubs.example.com/compute-store?hub_id=hub_1&tenant_id=tenant%20acme&email=dev%40example.com');
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

    it('shows welcome view with pinned news when only news messages exist', () => {
        const messages: ChatMessage[] = [
            makeNews('1'),
        ];

        const { getByTestId } = renderPanel({ state: { messages, scrollToTopSeq: 1, sending: false, streaming: false, ready: true } });

        // With only news messages and no conversation, the welcome view should be shown
        // (news cards are integrated into the welcome view).
        expect(getByTestId('ai-welcome-container')).toBeTruthy();
    });

    it('shows welcome view in inline (embedded panel) mode when no conversation exists', () => {
        const { getByTestId } = renderPanel({
            window: { inline: true },
            state: { messages: [], sending: false, streaming: false, ready: true },
        });

        expect(getByTestId('ai-welcome-container')).toBeTruthy();
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

    it('clears textarea content when the clear input button is clicked', async () => {
        const sendMessage = vi.fn().mockResolvedValue(undefined);
        const setDraftInputValue = vi.fn();
        const { getByTestId } = renderPanel({
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: { sendMessage, setDraftInputValue },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'draft to clear' } });
        fireEvent.click(getByTestId('ai-clear-input'));

        await waitFor(() => expect(input.value).toBe(''));
        expect(setDraftInputValue).toHaveBeenLastCalledWith('');
        expect(sendMessage).not.toHaveBeenCalled();
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

    it('does not send when Enter confirms an active IME composition', () => {
        const sendMessage = vi.fn().mockResolvedValue(undefined);
        const { getByTestId } = renderPanel({
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: { sendMessage },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'ni' } });
        fireEvent.keyDown(input, { key: 'Enter', isComposing: true });

        expect(sendMessage).not.toHaveBeenCalled();
        expect(input.value).toBe('ni');
    });

    it('tracks IME composition locally so Enter confirms text before it can send', () => {
        const sendMessage = vi.fn().mockResolvedValue(undefined);
        const { getByTestId } = renderPanel({
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: { sendMessage },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.compositionStart(input);
        fireEvent.change(input, { target: { value: 'mac' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        expect(sendMessage).not.toHaveBeenCalled();
        expect(input.value).toBe('mac');
    });

    it('does not send when compositionend arrives before the Enter keydown that committed IME text', () => {
        const sendMessage = vi.fn().mockResolvedValue(undefined);
        const { getByTestId } = renderPanel({
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: { sendMessage },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.compositionStart(input);
        fireEvent.change(input, { target: { value: 'macaron' } });
        fireEvent.compositionEnd(input);
        fireEvent.keyDown(input, { key: 'Enter' });

        expect(sendMessage).not.toHaveBeenCalled();
        expect(input.value).toBe('macaron');
    });

    it('only consumes the first Enter after compositionend so a quick second Enter can send', async () => {
        const sendMessage = vi.fn().mockResolvedValue(undefined);
        const { getByTestId } = renderPanel({
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: { sendMessage },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.compositionStart(input);
        fireEvent.change(input, { target: { value: 'quick send' } });
        fireEvent.compositionEnd(input);

        fireEvent.keyDown(input, { key: 'Enter' });
        expect(sendMessage).not.toHaveBeenCalled();

        fireEvent.keyDown(input, { key: 'Enter' });
        await waitFor(() => expect(sendMessage).toHaveBeenCalledWith('quick send'));
    });

    it('does not send when an IME Enter keydown is reported with keyCode 229', () => {
        const sendMessage = vi.fn().mockResolvedValue(undefined);
        const { getByTestId } = renderPanel({
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: { sendMessage },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'ma' } });
        fireEvent.keyDown(input, { key: 'Enter', keyCode: 229 });

        expect(sendMessage).not.toHaveBeenCalled();
        expect(input.value).toBe('ma');
    });

    it('fires queued input into the next agent loop as guide reference', async () => {
        localStorage.removeItem('ai_assistant_buffer_queue');
        const injectSupplementary = vi.fn().mockResolvedValue(false);
        const guideLaunchReference = vi.fn().mockResolvedValue(true);
        const sendMessage = vi.fn().mockResolvedValue(undefined);
        const recordSubmittedPrompt = vi.fn();
        const { getByTestId, queryByTestId } = renderPanel({
            state: { messages: [], sending: true, sendingSessionKey: 'desktop-user', streaming: true, streamingSessionKey: 'desktop-user', ready: true },
            actions: {
                sendMessage,
                injectSupplementary,
                guideLaunchReference,
                recordSubmittedPrompt,
            },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'guide this next' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await waitFor(() => expect(getByTestId('buffer-queue-panel')).toBeTruthy());
        const fireButton = document.querySelector('[data-testid^="fire-btn-"]') as HTMLButtonElement | null;
        expect(fireButton).toBeTruthy();
        expect(fireButton?.getAttribute('title')).toBe('引导发射');
        expect(fireButton?.getAttribute('aria-label')).toBe('引导进入下一次 agent loop');
        fireEvent.click(fireButton!);

        await waitFor(() => expect(guideLaunchReference).toHaveBeenCalledWith('guide this next', 'desktop-user'));
        expect(injectSupplementary).not.toHaveBeenCalled();
        expect(sendMessage).not.toHaveBeenCalled();
        await waitFor(() => expect(queryByTestId('buffer-queue-panel')).toBeNull());
    });

    it('keeps rejected local guide fire queued without supplementary fallback', async () => {
        localStorage.removeItem('ai_assistant_buffer_queue');
        const injectSupplementary = vi.fn().mockResolvedValue(true);
        const guideLaunchReference = vi.fn().mockResolvedValue(false);
        const sendMessage = vi.fn().mockResolvedValue(undefined);
        const { getByTestId, getByText } = renderPanel({
            state: { messages: [], sending: true, sendingSessionKey: 'desktop-user', streaming: true, streamingSessionKey: 'desktop-user', ready: true },
            actions: {
                sendMessage,
                injectSupplementary,
                guideLaunchReference,
            },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'stay in selected session' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await waitFor(() => expect(getByTestId('buffer-queue-panel')).toBeTruthy());
        const fireButton = document.querySelector('[data-testid^="fire-btn-"]') as HTMLButtonElement | null;
        expect(fireButton).toBeTruthy();
        fireEvent.click(fireButton!);

        await waitFor(() => expect(guideLaunchReference).toHaveBeenCalledWith('stay in selected session', 'desktop-user'));
        expect(injectSupplementary).not.toHaveBeenCalled();
        expect(sendMessage).not.toHaveBeenCalled();
        expect(getByText('stay in selected session')).toBeTruthy();
    });

    it('auto-drains input queued during a busy turn once the assistant is idle', async () => {
        localStorage.removeItem('ai_assistant_buffer_queue');
        const sendMessage = vi.fn().mockResolvedValue(true);
        const props = defaultPanelProps();
        props.actions = { ...props.actions, sendMessage };
        props.state = { ...props.state, sending: true, streaming: false, ready: true };
        const { getByTestId, queryByTestId, rerender } = render(<AIAssistantPanel {...props} />, { wrapper: DialogProvider });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'queued while busy' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await waitFor(() => expect(getByTestId('buffer-queue-panel')).toBeTruthy());
        expect(sendMessage).not.toHaveBeenCalled();

        props.state = { ...props.state, sending: false, streaming: false };
        rerender(<AIAssistantPanel {...props} />);

        await waitFor(() => expect(sendMessage).toHaveBeenCalledWith('queued while busy'));
        await waitFor(() => expect(queryByTestId('buffer-queue-panel')).toBeNull());
    });

    it('auto-drains a persisted type-ahead queue entry when the assistant is already idle', async () => {
        localStorage.setItem('ai_assistant_buffer_queue', JSON.stringify([
            { id: 'auto-drain-idle', text: 'queued before idle', attachments: [], createdAt: 1, autoDrain: true },
        ]));
        const sendMessage = vi.fn().mockResolvedValue(true);
        const { queryByTestId } = renderPanel({
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: { sendMessage },
        });

        await waitFor(() => expect(sendMessage).toHaveBeenCalledWith('queued before idle'));
        await waitFor(() => expect(queryByTestId('buffer-queue-panel')).toBeNull());
    });

    it('waits for assistant readiness before auto-draining a persisted type-ahead entry', async () => {
        localStorage.setItem('ai_assistant_buffer_queue', JSON.stringify([
            { id: 'auto-drain-not-ready', text: 'queued until ready', attachments: [], createdAt: 1, autoDrain: true },
        ]));
        const sendMessage = vi.fn().mockResolvedValue(true);
        const props = defaultPanelProps();
        props.actions = { ...props.actions, sendMessage };
        props.state = { ...props.state, messages: [], sending: false, streaming: false, ready: false };
        const { getByTestId, rerender } = render(<AIAssistantPanel {...props} />, { wrapper: DialogProvider });

        await waitFor(() => expect(getByTestId('buffer-queue-panel')).toBeTruthy());
        expect(sendMessage).not.toHaveBeenCalled();

        props.state = { ...props.state, ready: true };
        rerender(<AIAssistantPanel {...props} />);

        await waitFor(() => expect(sendMessage).toHaveBeenCalledWith('queued until ready'));
    });

    it('auto-drains all restored queue entries one by one after restart', async () => {
        localStorage.setItem('ai_assistant_buffer_queue', JSON.stringify([
            { id: 'restore-one', text: 'restored first', attachments: [], createdAt: 1 },
            { id: 'restore-two', text: 'restored second', attachments: [], createdAt: 2 },
        ]));
        const sendMessage = vi.fn().mockResolvedValue(true);
        const { queryByTestId } = renderPanel({
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: { sendMessage },
        });

        await waitFor(() => expect(sendMessage).toHaveBeenNthCalledWith(1, 'restored first'));
        await waitFor(() => expect(sendMessage).toHaveBeenNthCalledWith(2, 'restored second'));
        await waitFor(() => expect(queryByTestId('buffer-queue-panel')).toBeNull());
    });

    it('does not start the next restored queue entry until the current one is accepted', async () => {
        localStorage.setItem('ai_assistant_buffer_queue', JSON.stringify([
            { id: 'restore-slow-one', text: 'slow restored first', attachments: [], createdAt: 1 },
            { id: 'restore-slow-two', text: 'slow restored second', attachments: [], createdAt: 2 },
        ]));
        let resolveFirst!: (value: boolean) => void;
        const sendMessage = vi.fn((text: string) => {
            if (text === 'slow restored first') {
                return new Promise<boolean>((resolve) => { resolveFirst = resolve; });
            }
            return Promise.resolve(true);
        });
        renderPanel({
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: { sendMessage },
        });

        await waitFor(() => expect(sendMessage).toHaveBeenCalledTimes(1));
        expect(sendMessage).toHaveBeenCalledWith('slow restored first');
        expect(sendMessage).not.toHaveBeenCalledWith('slow restored second');

        resolveFirst(true);

        await waitFor(() => expect(sendMessage).toHaveBeenNthCalledWith(2, 'slow restored second'));
    });

    it('sanitizes restored queue attachments before auto-draining', async () => {
        localStorage.setItem('ai_assistant_buffer_queue', JSON.stringify([
            {
                id: 'restore-attachments',
                text: 'restored with attachments',
                attachments: [
                    { filePath: ' D:\\cases\\report.pdf ', fileName: '', extension: '' },
                    { filePath: '' },
                    { bogus: true },
                ],
                createdAt: 1,
            },
            { id: 'restore-empty', text: '   ', attachments: [], createdAt: 2 },
        ]));
        const sendMessage = vi.fn().mockResolvedValue(true);
        renderPanel({
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: { sendMessage },
        });

        await waitFor(() => expect(sendMessage).toHaveBeenCalledTimes(1));
        const sentText = String(sendMessage.mock.calls[0]?.[0] || '');
        expect(sentText).toContain('restored with attachments');
        expect(sentText).toContain('D:\\cases\\report.pdf');
        expect(sentText).not.toContain('[object Object]');
    });

    it('does not downgrade rejected project guide fire into global supplementary injection', async () => {
        localStorage.removeItem('ai_assistant_buffer_queue');
        const injectSupplementary = vi.fn().mockResolvedValue(true);
        const guideLaunchReference = vi.fn().mockResolvedValue(false);
        const onHandled = vi.fn();
        const { getByTestId } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/no-global-fallback',
                taskTitle: 'No global fallback task',
                autoSend: false,
            },
            onPendingProjectTabOpenHandled: onHandled,
            state: { messages: [], sending: true, sendingSessionKey: 'desktop-user:D:/tasks/no-global-fallback', streaming: true, streamingSessionKey: 'desktop-user:D:/tasks/no-global-fallback', ready: true },
            actions: {
                injectSupplementary,
                guideLaunchReference,
            },
        });

        await waitFor(() => expect(onHandled).toHaveBeenCalled());
        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'project selected session only' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await waitFor(() => expect(getByTestId('buffer-queue-panel')).toBeTruthy());
        const fireButton = document.querySelector('[data-testid^="fire-btn-"]') as HTMLButtonElement | null;
        expect(fireButton).toBeTruthy();
        fireEvent.click(fireButton!);

        await waitFor(() => expect(guideLaunchReference).toHaveBeenCalledWith('project selected session only', 'desktop-user:D:/tasks/no-global-fallback'));
        expect(injectSupplementary).not.toHaveBeenCalled();
        expect(document.body.textContent || '').toContain('project selected session only');
    });

    it('renders restored project context as a compact user-facing note', async () => {
        loadProjectContextMock.mockResolvedValue({
            project_name: '北京天气',
            recent_progress: [
                'Forked from recent task.',
                'Source task: D:\\Users\\alice\\.maclaw\\data\\tasks\\source-task',
                '查询了北京今天的天气，当前多云，适合继续跟进预报。',
                '',
                '**关键产出物:**',
                '- `D:\\Users\\alice\\.maclaw\\data\\tasks\\source-task\\weather.md`',
                '**最近产物来源:**',
                '- weather.md - `D:\\Users\\alice\\.maclaw\\data\\tasks\\source-task\\weather.md`; full: read_file',
            ].join('\n'),
            key_artifacts: ['D:\\Users\\alice\\.maclaw\\data\\tasks\\source-task\\weather.md'],
            recent_artifacts: [{ title: 'weather.md', source_url: 'D:\\Users\\alice\\.maclaw\\data\\tasks\\source-task\\weather.md', source_hint: 'full: read_file' }],
            active_workflow: '',
        });
        const onHandled = vi.fn();

        const { queryByText } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/beijing-weather',
                taskTitle: '北京天气',
                autoSend: false,
            },
            onPendingProjectTabOpenHandled: onHandled,
            state: { messages: [], sending: false, streaming: false, ready: true },
        });

        await waitFor(() => expect(onHandled).toHaveBeenCalled());
        await waitFor(() => expect(document.body.textContent || '').toContain('已恢复任务上下文：北京天气'));
        const bodyText = document.body.textContent || '';
        expect(bodyText).toContain('最近进展：查询了北京今天的天气');
        expect(bodyText).toContain('相关产物和来源已载入，AI 会参考。可以直接继续问。');
        expect(bodyText).not.toContain('Forked from recent task');
        expect(bodyText).not.toContain('Source task:');
        expect(bodyText).not.toContain('关键产出物');
        expect(bodyText).not.toContain('最近产物来源');
        expect(bodyText).not.toContain('.maclaw');
        expect(queryByText(/read_file/)).toBeNull();
    });

    it('locks a newly opened project tab until context restore finishes and queues typed input', async () => {
        let resolveContext!: (value: any) => void;
        loadProjectContextMock.mockReturnValueOnce(new Promise(resolve => {
            resolveContext = resolve;
        }));
        const sendMessage = vi.fn().mockResolvedValue(true);
        const onHandled = vi.fn();

        const { getByTestId, queryByTestId } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/prepare-before-send',
                taskTitle: 'Prepare before send',
                autoSend: false,
            },
            onPendingProjectTabOpenHandled: onHandled,
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: { sendMessage },
        });

        await waitFor(() => expect(onHandled).toHaveBeenCalled());
        expect(getByTestId('project-tab-restore-progress')).toBeTruthy();
        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        expect(input.disabled).toBe(false);
        expect(input.placeholder).toContain('Restoring task context');

        fireEvent.change(input, { target: { value: 'send only after restore' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        expect(sendMessage).not.toHaveBeenCalled();
        expect(getByTestId('buffer-queue-panel')).toBeTruthy();
        expect(document.body.textContent || '').toContain('send only after restore');
        expect((document.querySelector('[data-testid^="fire-btn-"]') as HTMLButtonElement | null)?.disabled).toBe(true);
        expect((document.querySelector('[data-testid^="edit-btn-"]') as HTMLButtonElement | null)?.disabled).toBe(true);

        await act(async () => {
            resolveContext({ project_name: 'Prepare before send', recent_progress: '', key_artifacts: [], recent_artifacts: [], active_workflow: '' });
        });

        await waitFor(() => expect(queryByTestId('project-tab-restore-progress')).toBeNull());
        await waitFor(() => expect(sendMessage).toHaveBeenCalledWith('send only after restore', expect.objectContaining({ project_path: 'D:/tasks/prepare-before-send' })));
    });

    it('releases project tab restore lock after context timeout while retry continues in background', async () => {
        loadProjectContextMock.mockReturnValue(new Promise(() => {}));
        const sendMessage = vi.fn().mockResolvedValue(true);
        const onHandled = vi.fn();

        const { getByTestId, queryByTestId } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/slow-context',
                taskTitle: 'Slow context',
                autoSend: false,
            },
            onPendingProjectTabOpenHandled: onHandled,
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: { sendMessage },
        });

        await waitFor(() => expect(onHandled).toHaveBeenCalled());
        expect(getByTestId('project-tab-restore-progress')).toBeTruthy();

        fireEvent.change(getByTestId('ai-input'), { target: { value: 'continue after timeout' } });
        fireEvent.keyDown(getByTestId('ai-input'), { key: 'Enter' });
        expect(sendMessage).not.toHaveBeenCalled();

        await waitFor(() => expect(queryByTestId('project-tab-restore-progress')).toBeNull(), { timeout: 3000 });
        await waitFor(() => expect(sendMessage).toHaveBeenCalledWith('continue after timeout', expect.objectContaining({ project_path: 'D:/tasks/slow-context' })));
    });

    it('queues send-button clicks while project context restore is preparing', async () => {
        let resolveContext!: (value: any) => void;
        loadProjectContextMock.mockReturnValueOnce(new Promise(resolve => {
            resolveContext = resolve;
        }));
        const sendMessage = vi.fn().mockResolvedValue(true);

        const { getByTestId, getByTitle } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/prepare-button-send',
                taskTitle: 'Prepare button send',
                autoSend: false,
            },
            onPendingProjectTabOpenHandled: vi.fn(),
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: { sendMessage },
        });

        await waitFor(() => expect(getByTestId('project-tab-restore-progress')).toBeTruthy());
        fireEvent.change(getByTestId('ai-input'), { target: { value: 'button waits too' } });
        fireEvent.click(getByTitle('Send (Enter)'));

        expect(sendMessage).not.toHaveBeenCalled();
        expect(getByTestId('buffer-queue-panel').textContent || '').toContain('button waits too');

        await act(async () => {
            resolveContext({ project_name: 'Prepare button send', recent_progress: '', key_artifacts: [], recent_artifacts: [], active_workflow: '' });
        });

        await waitFor(() => expect(sendMessage).toHaveBeenCalledWith('button waits too', expect.objectContaining({ project_path: 'D:/tasks/prepare-button-send' })));
    });

    it('defers project auto-send until context restore finishes', async () => {
        let resolveContext!: (value: any) => void;
        loadProjectContextMock.mockReturnValueOnce(new Promise(resolve => {
            resolveContext = resolve;
        }));
        const sendMessage = vi.fn().mockResolvedValue(true);
        const onHandled = vi.fn();

        const { getByTestId } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/autosend-after-prepare',
                taskTitle: 'Auto after prepare',
                initialMessage: 'auto waits for context',
                autoSend: true,
            },
            onPendingProjectTabOpenHandled: onHandled,
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: { sendMessage },
        });

        await waitFor(() => expect(onHandled).toHaveBeenCalled());
        expect(getByTestId('project-tab-restore-progress')).toBeTruthy();
        expect(sendMessage).not.toHaveBeenCalled();

        await act(async () => {
            resolveContext({ project_name: 'Auto after prepare', recent_progress: '', key_artifacts: [], recent_artifacts: [], active_workflow: '' });
        });

        await waitFor(() => expect(sendMessage).toHaveBeenCalledWith('auto waits for context', expect.objectContaining({ project_path: 'D:/tasks/autosend-after-prepare' })));
    });

    it('labels new task preparation as agent instance creation while input waits', async () => {
        const sendMessage = vi.fn().mockResolvedValue(true);

        const { getByTestId } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/new-agent-prepare',
                taskTitle: 'Create new agent backed task',
                autoSend: false,
                prepareMode: 'new-agent',
            },
            onPendingProjectTabOpenHandled: vi.fn(),
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: { sendMessage },
        });

        await waitFor(() => expect(getByTestId('project-tab-restore-progress').textContent || '').toContain('Creating agent instance'));
        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        expect(input.placeholder).toContain('Creating agent instance');

        fireEvent.change(input, { target: { value: 'queued while creating' } });
        fireEvent.keyDown(input, { key: 'Enter' });
        expect(sendMessage).not.toHaveBeenCalled();
        expect(loadProjectContextMock).not.toHaveBeenCalled();

        await waitFor(() => expect(sendMessage).toHaveBeenCalledWith('queued while creating', expect.objectContaining({ project_path: 'D:/tasks/new-agent-prepare' })));
    });

    it('keeps new task input queued until the project tab session is registered', async () => {
        let resolveSession!: () => void;
        createProjectTabSessionMock.mockReturnValueOnce(new Promise<void>(resolve => {
            resolveSession = resolve;
        }));
        const sendMessage = vi.fn().mockResolvedValue(true);

        const { getByTestId } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/new-agent-session-wait',
                taskTitle: 'Wait for session registration',
                autoSend: false,
                prepareMode: 'new-agent',
            },
            onPendingProjectTabOpenHandled: vi.fn(),
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: { sendMessage },
        });

        await waitFor(() => expect(getByTestId('project-tab-restore-progress').textContent || '').toContain('Creating agent instance'));
        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'send after session exists' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await new Promise(resolve => setTimeout(resolve, 180));
        expect(sendMessage).not.toHaveBeenCalled();

        await act(async () => {
            resolveSession();
        });

        await waitFor(() => expect(sendMessage).toHaveBeenCalledWith('send after session exists', expect.objectContaining({ project_path: 'D:/tasks/new-agent-session-wait' })));
    });

    it('uses the fallback delay when project tab session registration fails', async () => {
        vi.useFakeTimers();
        createProjectTabSessionMock.mockRejectedValueOnce(new Error('session failed'));
        const sendMessage = vi.fn().mockResolvedValue(true);

        const { getByTestId } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/new-agent-session-fallback',
                taskTitle: 'Fallback session registration',
                autoSend: false,
                prepareMode: 'new-agent',
            },
            onPendingProjectTabOpenHandled: vi.fn(),
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: { sendMessage },
        });

        expect(getByTestId('project-tab-restore-progress').textContent || '').toContain('Creating agent instance');
        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'fallback after registration failure' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await act(async () => {
            await Promise.resolve();
        });
        expect(sendMessage).not.toHaveBeenCalled();

        await act(async () => {
            vi.advanceTimersByTime(5000);
            await Promise.resolve();
        });

        expect(sendMessage).toHaveBeenCalledWith('fallback after registration failure', expect.objectContaining({ project_path: 'D:/tasks/new-agent-session-fallback' }));
    });

    it('drops queued new-task input when the tab closes before session registration finishes', async () => {
        let resolveSession!: () => void;
        createProjectTabSessionMock.mockReturnValueOnce(new Promise<void>(resolve => {
            resolveSession = resolve;
        }));
        const sendMessage = vi.fn().mockResolvedValue(true);

        const { getByTestId, queryByText } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/new-agent-close-before-ready',
                taskTitle: 'Close before ready',
                autoSend: false,
                prepareMode: 'new-agent',
            },
            onPendingProjectTabOpenHandled: vi.fn(),
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: { sendMessage },
        });

        await waitFor(() => expect(getByTestId('project-tab-restore-progress').textContent || '').toContain('Creating agent instance'));
        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'should be dropped on close' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        const closeEventHandler = runtimeEventsOnMock.mock.calls.filter(([eventName]) => eventName === 'project-task:closed').at(-1)?.[1];
        expect(closeEventHandler).toBeTypeOf('function');
        act(() => closeEventHandler('D:/tasks/new-agent-close-before-ready'));
        await waitFor(() => expect(queryByText('Close before ready')).toBeNull());

        await act(async () => {
            resolveSession();
            await Promise.resolve();
        });

        expect(sendMessage).not.toHaveBeenCalled();
    });

    it('does not re-enter preparation or auto-send when reopening an existing new task tab', async () => {
        const sendMessage = vi.fn().mockResolvedValue(true);
        const onHandled = vi.fn();

        const { queryByTestId, rerender } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/reopen-new-agent-existing',
                taskTitle: 'Reopen new agent existing',
                autoSend: false,
                prepareMode: 'new-agent',
            },
            onPendingProjectTabOpenHandled: onHandled,
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: { sendMessage },
        });

        await waitFor(() => expect(onHandled).toHaveBeenCalled());
        await waitFor(() => expect(queryByTestId('project-tab-restore-progress')).toBeNull());
        expect(loadProjectContextMock).not.toHaveBeenCalled();

        onHandled.mockClear();
        loadProjectContextMock.mockClear();
        const base = defaultPanelProps();
        rerender(<AIAssistantPanel
            {...base}
            actions={{ ...base.actions, sendMessage }}
            pendingProjectTabOpen={{
                projectPath: 'D:/tasks/reopen-new-agent-existing',
                taskTitle: 'Reopen new agent existing',
                autoSend: true,
                prepareMode: 'new-agent',
            }}
            onPendingProjectTabOpenHandled={onHandled}
        />);

        await waitFor(() => expect(onHandled).toHaveBeenCalled());
        expect(queryByTestId('project-tab-restore-progress')).toBeNull();
        expect(loadProjectContextMock).not.toHaveBeenCalled();
        expect(sendMessage).not.toHaveBeenCalled();
    });

    it('disables queued entry actions while a guide fire is in flight', async () => {
        localStorage.removeItem('ai_assistant_buffer_queue');
        let resolveGuide!: (value: boolean) => void;
        const guideLaunchReference = vi.fn(() => new Promise<boolean>((resolve) => {
            resolveGuide = resolve;
        }));
        const { getByTestId, getByText, queryByTestId } = renderPanel({
            state: { messages: [], sending: true, sendingSessionKey: 'desktop-user', streaming: true, streamingSessionKey: 'desktop-user', ready: true },
            actions: {
                guideLaunchReference,
            },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'pending guide lock' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await waitFor(() => expect(getByText('pending guide lock')).toBeTruthy());
        const fireButton = document.querySelector('[data-testid^="fire-btn-"]') as HTMLButtonElement | null;
        expect(fireButton).toBeTruthy();
        fireEvent.click(fireButton!);

        await waitFor(() => expect(guideLaunchReference).toHaveBeenCalledWith('pending guide lock', 'desktop-user'));
        await waitFor(() => expect((document.querySelector('[data-testid^="fire-btn-"]') as HTMLButtonElement | null)?.disabled).toBe(true));
        expect((document.querySelector('[data-testid^="edit-btn-"]') as HTMLButtonElement | null)?.disabled).toBe(true);
        expect((document.querySelector('[data-testid^="delete-btn-"]') as HTMLButtonElement | null)?.disabled).toBe(true);

        fireEvent.click(document.querySelector('[data-testid^="edit-btn-"]') as HTMLButtonElement);
        fireEvent.click(document.querySelector('[data-testid^="delete-btn-"]') as HTMLButtonElement);
        expect(getByText('pending guide lock')).toBeTruthy();

        resolveGuide(true);
        await waitFor(() => expect(queryByTestId('buffer-queue-panel')).toBeNull());
    });
    it('routes pending project auto-send messages to the created project tab only', async () => {
        let resolveSend!: (value: boolean) => void;
        const sendMessage = vi.fn(() => new Promise<boolean>((resolve) => {
            resolveSend = resolve;
        }));
        const onHandled = vi.fn();
        const localBefore = makeMsg({ role: 'user', content: 'local before auto-send' });
        const projectUser = makeMsg({ role: 'user', content: 'weather query' });
        const projectAssistant = makeMsg({ role: 'assistant', content: 'checking weather', requestId: 'req-weather' });
        const base = defaultPanelProps();
        const props = {
            ...base,
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/weather-auto',
                taskTitle: 'Weather auto task',
                initialMessage: 'weather query',
                autoSend: true,
            },
            onPendingProjectTabOpenHandled: onHandled,
            state: { ...base.state, messages: [localBefore], sending: false, streaming: false, ready: true },
            actions: { ...base.actions, sendMessage },
        };
        const { rerender, getByTestId, getByText } = render(<AIAssistantPanel {...props} />, { wrapper: DialogProvider });

        await waitFor(() => expect(sendMessage).toHaveBeenCalledWith('weather query', expect.objectContaining({
            tabId: expect.any(String),
            project_path: 'D:/tasks/weather-auto',
        })));
        const projectTabId = (((sendMessage as any).mock.calls[0]?.[1]) as any)?.tabId as string;
        fireEvent.click(getByTestId(`ai-tab-${projectTabId}`));

        rerender(<AIAssistantPanel {...props} pendingProjectTabOpen={null} state={{ ...props.state, messages: [localBefore, projectUser, projectAssistant], sending: true, streaming: true }} />);
        await waitFor(() => expect(document.body.textContent || '').toContain('weather query'));
        expect(document.body.textContent || '').toContain('checking weather');

        fireEvent.click(getByTestId('ai-tab-local'));
        expect(getByText(/local before auto-send/)).toBeTruthy();
        expect(document.body.textContent || '').not.toContain('weather query');
        expect(document.body.textContent || '').not.toContain('checking weather');

        resolveSend(true);
        rerender(<AIAssistantPanel {...props} pendingProjectTabOpen={null} state={{ ...props.state, messages: [localBefore, projectUser, projectAssistant], sending: false, streaming: false }} />);
        await waitFor(() => expect(document.body.textContent || '').not.toContain('weather query'));
        expect(document.body.textContent || '').not.toContain('checking weather');
        expect(onHandled).toHaveBeenCalled();
    });

    it('lets local assistant submit while a project tab round is running', async () => {
        let resolveProjectSend!: (value: boolean) => void;
        const sendMessage = vi.fn(() => new Promise<boolean>((resolve) => {
            resolveProjectSend = resolve;
        }));
        const onHandled = vi.fn();
        const localBefore = makeMsg({ role: 'user', content: 'local remains active' });
        const base = defaultPanelProps();
        const props = {
            ...base,
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/running-project-round',
                taskTitle: 'Running project round',
                initialMessage: 'start project work',
                autoSend: true,
            },
            onPendingProjectTabOpenHandled: onHandled,
            state: { ...base.state, messages: [localBefore], sending: false, streaming: false, ready: true },
            actions: { ...base.actions, sendMessage },
        };
        const { rerender, getByTestId, queryByTestId } = render(<AIAssistantPanel {...props} />, { wrapper: DialogProvider });

        await waitFor(() => expect(sendMessage).toHaveBeenCalledWith('start project work', expect.objectContaining({
            project_path: 'D:/tasks/running-project-round',
        })));
        rerender(<AIAssistantPanel {...props} pendingProjectTabOpen={null} state={{ ...props.state, sending: true, streaming: true }} />);

        fireEvent.click(getByTestId('ai-tab-local'));
        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'local question during project' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await waitFor(() => expect(sendMessage).toHaveBeenCalledWith('local question during project'));
        expect(queryByTestId('buffer-queue-panel')).toBeNull();
        rerender(<AIAssistantPanel {...props} pendingProjectTabOpen={null} state={{ ...props.state, sending: true, streaming: true }} />);
        fireEvent.change(getByTestId('ai-input'), { target: { value: 'second local waits for local agent' } });
        fireEvent.keyDown(getByTestId('ai-input'), { key: 'Enter' });
        expect(sendMessage).not.toHaveBeenCalledWith('second local waits for local agent');
        await waitFor(() => expect(getByTestId('buffer-queue-panel').textContent || '').toContain('second local waits for local agent'));
        resolveProjectSend(true);
    });

    it('does not keep a project tab busy after detached round messages disappear', async () => {
        let resolveProjectSend!: (value: boolean) => void;
        const sendMessage = vi.fn((_: string, options?: any) => {
            if (options?.project_path) {
                return new Promise<boolean>((resolve) => {
                    resolveProjectSend = resolve;
                });
            }
            return Promise.resolve(true);
        });
        const localBefore = makeMsg({ role: 'user', content: 'local before stale detach' });
        const projectUser = makeMsg({ role: 'user', content: 'project stale user' });
        const projectAssistant = makeMsg({ role: 'assistant', content: '', requestId: 'req-stale-detach' });
        const base = defaultPanelProps();
        const props = {
            ...base,
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/stale-detached-round',
                taskTitle: 'Stale detached round',
                initialMessage: 'start stale project work',
                autoSend: true,
            },
            onPendingProjectTabOpenHandled: vi.fn(),
            state: { ...base.state, messages: [localBefore], sending: false, streaming: false, ready: true },
            actions: { ...base.actions, sendMessage },
        };
        const { rerender, getByTestId, queryByTestId, getByText } = render(<AIAssistantPanel {...props} />, { wrapper: DialogProvider });

        await waitFor(() => expect(sendMessage).toHaveBeenCalledWith('start stale project work', expect.objectContaining({
            project_path: 'D:/tasks/stale-detached-round',
        })));
        const projectTabId = (((sendMessage as any).mock.calls[0]?.[1]) as any)?.tabId as string;

        rerender(<AIAssistantPanel {...props} pendingProjectTabOpen={null} state={{ ...props.state, messages: [localBefore, projectUser, projectAssistant], sending: true, streaming: true }} />);
        fireEvent.click(getByTestId('ai-tab-local'));
        const localInput = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(localInput, { target: { value: 'local takes over stale project' } });
        fireEvent.keyDown(localInput, { key: 'Enter' });
        await waitFor(() => expect(sendMessage).toHaveBeenCalledWith('local takes over stale project'));

        rerender(<AIAssistantPanel {...props} pendingProjectTabOpen={null} state={{ ...props.state, messages: [localBefore], sending: false, streaming: false }} />);
        const projectTabButton = queryByTestId(`ai-tab-${projectTabId}`);
        if (projectTabButton) {
            fireEvent.click(projectTabButton);
        } else {
            fireEvent.click(getByTestId('ai-tab-overflow-btn'));
            fireEvent.click(getByText('Stale detached round'));
        }
        await waitFor(() => expect((getByTestId('ai-input') as HTMLTextAreaElement).value).toBe(''));
        const projectInput = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(projectInput, { target: { value: 'project should send after cleanup' } });
        fireEvent.keyDown(projectInput, { key: 'Enter' });

        await waitFor(() => expect(sendMessage).toHaveBeenCalledWith('project should send after cleanup', expect.objectContaining({
            tabId: projectTabId,
            project_path: 'D:/tasks/stale-detached-round',
        })));
        resolveProjectSend(true);
    });

    it('replaces streaming placeholder with final content when round completes (no ghost 思考中)', async () => {
        // Regression test for appendUnique bug: when the live-sync effect writes
        // an empty assistant placeholder into projectTabMessages during streaming,
        // the wasSending→!sending effect must *replace* it (via mergeChatMessages)
        // with the final-content version from `messages`, not skip it (appendUnique
        // skipped IDs already present, leaving a ghost empty placeholder alongside
        // the real response — manifesting as "思考中..." + response text together).
        let resolveSend!: (value: boolean) => void;
        const sendMessage = vi.fn((_: string, options?: any) => {
            if (options?.project_path) {
                return new Promise<boolean>((resolve) => { resolveSend = resolve; });
            }
            return Promise.resolve(true);
        });
        const emptyPlaceholder = makeMsg({ role: 'assistant', content: '', requestId: 'req-taipei' });
        const finalAssistant = makeMsg({ ...emptyPlaceholder, content: '台北天气已记下，下次直接回复' });
        const base = defaultPanelProps();
        const props = {
            ...base,
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/taipei-weather',
                taskTitle: '台北天气',
                initialMessage: '台北天气',
                autoSend: true,
            },
            onPendingProjectTabOpenHandled: vi.fn(),
            state: { ...base.state, messages: [], sending: false, streaming: false, ready: true },
            actions: { ...base.actions, sendMessage },
        };
        const { rerender, getByTestId } = render(<AIAssistantPanel {...props} />, { wrapper: DialogProvider });
        await waitFor(() => expect(sendMessage).toHaveBeenCalledWith('台北天气', expect.objectContaining({
            project_path: 'D:/tasks/taipei-weather',
        })));
        const projectTabId = (((sendMessage as any).mock.calls[0]?.[1]) as any)?.tabId as string;
        fireEvent.click(getByTestId(`ai-tab-${projectTabId}`));

        // Phase 1: streaming — empty placeholder written to messages
        const userMsg = makeMsg({ role: 'user', content: '台北天气', sessionKey: 'desktop-user:D:/tasks/taipei-weather' });
        rerender(<AIAssistantPanel {...props} pendingProjectTabOpen={null} state={{
            ...props.state,
            messages: [userMsg, { ...emptyPlaceholder, sessionKey: 'desktop-user:D:/tasks/taipei-weather' }],
            sending: true, streaming: true,
            sendingSessionKey: 'desktop-user:D:/tasks/taipei-weather',
            streamingSessionKey: 'desktop-user:D:/tasks/taipei-weather',
            busySessionKeys: ['desktop-user:D:/tasks/taipei-weather'],
            streamingSessionKeys: ['desktop-user:D:/tasks/taipei-weather'],
        }} />);

        // Phase 2: final content arrives in messages, sending transitions to false
        resolveSend(true);
        rerender(<AIAssistantPanel {...props} pendingProjectTabOpen={null} state={{
            ...props.state,
            messages: [
                { ...userMsg, sessionKey: 'desktop-user:D:/tasks/taipei-weather' },
                { ...finalAssistant, sessionKey: 'desktop-user:D:/tasks/taipei-weather' },
            ],
            sending: false, streaming: false,
            sendingSessionKey: '',
            streamingSessionKey: '',
            busySessionKeys: [],
            streamingSessionKeys: [],
        }} />);

        // The panel must show EXACTLY ONE assistant message with the final content,
        // not a ghost empty placeholder + a separate final-content message.
        await waitFor(() => {
            expect(document.body.textContent || '').toContain('台北天气已记下');
        });
        const outputEl = document.querySelector('[data-testid="ai-output-container"]');
        const outputText = outputEl?.textContent || '';
        // Final content appears
        expect(outputText).toContain('台北天气已记下');
        // The placeholder's empty content must NOT coexist with the final text.
        // If the bug were present, the empty placeholder would still be rendered
        // and the aiAssistantMarkdown component would inject the "正在思考..."
        // italic span directly in the text stream. Detect this by counting how
        // many times the final-content substring appears — it should be exactly
        // one, not duplicated via a second assistant entry carrying the response.
        const matches = (outputText.match(/台北天气已记下/g) || []).length;
        expect(matches).toBe(1);
    });

    it('keeps project guide reference echo inside the active project tab', async () => {
        localStorage.removeItem('ai_assistant_buffer_queue');
        const guideLaunchReference = vi.fn().mockResolvedValue(true);
        const onHandled = vi.fn();
        const { getByTestId } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/weather',
                taskTitle: 'Weather task',
                autoSend: false,
            },
            onPendingProjectTabOpenHandled: onHandled,
            state: { messages: [], sending: true, sendingSessionKey: 'desktop-user:D:/tasks/weather', streaming: true, streamingSessionKey: 'desktop-user:D:/tasks/weather', ready: true },
            actions: {
                guideLaunchReference,
            },
        });

        await waitFor(() => expect(onHandled).toHaveBeenCalled());
        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'project guide context' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await waitFor(() => expect(getByTestId('buffer-queue-panel')).toBeTruthy());
        const fireButton = document.querySelector('[data-testid^="fire-btn-"]') as HTMLButtonElement | null;
        expect(fireButton).toBeTruthy();
        fireEvent.click(fireButton!);

        await waitFor(() => expect(guideLaunchReference).toHaveBeenCalledWith('project guide context', 'desktop-user:D:/tasks/weather'));
        await waitFor(() => expect(document.body.textContent || '').toContain('引导已注入下一轮'));
        expect(document.body.textContent || '').toContain('project guide context');

        fireEvent.click(getByTestId('ai-tab-local'));
        expect(document.body.textContent || '').not.toContain('引导已注入下一轮');
    });

    it('keeps delayed project guide reference echo bound to the fired tab', async () => {
        localStorage.removeItem('ai_assistant_buffer_queue');
        let resolveGuide!: (value: boolean) => void;
        const guideLaunchReference = vi.fn(() => new Promise<boolean>((resolve) => {
            resolveGuide = resolve;
        }));
        const onHandled = vi.fn();
        const { getByTestId, getByText } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/delayed-guide',
                taskTitle: 'Delayed guide task',
                autoSend: false,
            },
            onPendingProjectTabOpenHandled: onHandled,
            state: { messages: [], sending: true, sendingSessionKey: 'desktop-user:D:/tasks/delayed-guide', streaming: true, streamingSessionKey: 'desktop-user:D:/tasks/delayed-guide', ready: true },
            actions: {
                guideLaunchReference,
            },
        });

        await waitFor(() => expect(onHandled).toHaveBeenCalled());
        const projectTab = document.querySelector('[data-testid^="ai-tab-proj-"]') as HTMLElement | null;
        expect(projectTab).toBeTruthy();
        const projectTabTestId = projectTab!.getAttribute('data-testid') || '';
        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'delayed project guide' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await waitFor(() => expect(getByTestId('buffer-queue-panel')).toBeTruthy());
        const fireButton = document.querySelector('[data-testid^="fire-btn-"]') as HTMLButtonElement | null;
        expect(fireButton).toBeTruthy();
        fireEvent.click(fireButton!);
        await waitFor(() => expect(guideLaunchReference).toHaveBeenCalledWith('delayed project guide', 'desktop-user:D:/tasks/delayed-guide'));

        fireEvent.click(getByTestId('ai-tab-local'));
        resolveGuide(true);
        await waitFor(() => expect(document.body.textContent || '').not.toContain('delayed project guide'));

        await waitFor(() => expect(document.querySelector('[data-testid^="fire-btn-"]')).toBeNull());
        const visibleProjectTab = document.querySelector(`[data-testid="${projectTabTestId}"]`) as HTMLElement | null;
        if (visibleProjectTab) {
            fireEvent.click(visibleProjectTab);
        } else {
            fireEvent.click(getByTestId('ai-tab-overflow-btn'));
            fireEvent.click(getByText('Delayed guide task'));
        }
        await waitFor(() => expect(document.body.textContent || '').toContain('引导已注入下一轮'));
        expect(document.body.textContent || '').toContain('delayed project guide');
    });
    it('preserves multiple rapid project guide reference echoes', async () => {
        localStorage.setItem('ai_assistant_buffer_queue', JSON.stringify([
            { id: 'project-guide-one', text: 'first project guide', attachments: [], createdAt: 1, sessionKey: 'desktop-user:D:/tasks/rapid-guides' },
            { id: 'project-guide-two', text: 'second project guide', attachments: [], createdAt: 2, sessionKey: 'desktop-user:D:/tasks/rapid-guides' },
        ]));
        const guideLaunchReference = vi.fn().mockResolvedValue(true);
        const onHandled = vi.fn();
        const { getByTestId } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/rapid-guides',
                taskTitle: 'Rapid guide task',
                autoSend: false,
            },
            onPendingProjectTabOpenHandled: onHandled,
            state: { messages: [], sending: true, sendingSessionKey: 'desktop-user:D:/tasks/rapid-guides', streaming: true, streamingSessionKey: 'desktop-user:D:/tasks/rapid-guides', ready: true },
            actions: {
                guideLaunchReference,
            },
        });

        await waitFor(() => expect(onHandled).toHaveBeenCalled());
        await waitFor(() => expect(getByTestId('buffer-queue-panel')).toBeTruthy());
        const fireButtons = Array.from(document.querySelectorAll('[data-testid^="fire-btn-"]')) as HTMLButtonElement[];
        expect(fireButtons.length).toBeGreaterThanOrEqual(2);
        fireEvent.click(fireButtons[0]);
        fireEvent.click(fireButtons[1]);

        await waitFor(() => expect(guideLaunchReference).toHaveBeenCalledTimes(2));
        expect(document.body.textContent || '').toContain('first project guide');
        expect(document.body.textContent || '').toContain('second project guide');

        fireEvent.click(getByTestId('ai-tab-local'));
        expect(document.body.textContent || '').not.toContain('first project guide');
        expect(document.body.textContent || '').not.toContain('second project guide');
    });

    it('accepts a pasted clipboard file as an attachment and sends its path', async () => {
        const sendMessage = vi.fn().mockResolvedValue(true);
        const { getByTestId } = renderPanel({
            actions: { sendMessage },
        });
        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        const pastedFile = new File(['contract'], 'contract.pdf', { type: 'application/pdf' });
        Object.defineProperty(pastedFile, 'path', {
            configurable: true,
            value: 'D:\\cases\\contract.pdf',
        });

        fireEvent.paste(input, {
            clipboardData: {
                items: [{ kind: 'file', type: 'application/pdf', getAsFile: () => pastedFile }],
                files: [pastedFile],
            },
        });

        await waitFor(() => expect(getByTestId('ai-pending-attachments').textContent || '').toContain('contract.pdf'));
        expect(getByTestId('ai-pending-attachments').textContent || '').toContain('PDF');

        fireEvent.change(input, { target: { value: 'please review' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await waitFor(() => expect(sendMessage).toHaveBeenCalled());
        const outgoing = String(sendMessage.mock.calls[0]?.[0] || '');
        expect(outgoing).toContain('please review');
        expect(outgoing).toContain('D:\\cases\\contract.pdf');
    });

    it('keeps queued pasted image thumbnails visible until the entry is fired', async () => {
        localStorage.removeItem('ai_assistant_buffer_queue');
        mockURLObjectURLs('blob:test-image');
        const sendMessage = vi.fn().mockResolvedValue(true);
        const { getByTestId } = renderPanel({
            state: { messages: [], sending: true, streaming: true, ready: true },
            actions: { sendMessage },
        });
        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        const pastedFile = new File(['image'], 'shot.png', { type: 'image/png' });
        Object.defineProperty(pastedFile, 'path', {
            configurable: true,
            value: 'D:\\shots\\shot.png',
        });

        fireEvent.paste(input, {
            clipboardData: {
                items: [{ kind: 'file', type: 'image/png', getAsFile: () => pastedFile }],
                files: [pastedFile],
            },
        });
        fireEvent.change(input, { target: { value: 'use this image' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await waitFor(() => expect(getByTestId('buffer-queue-panel')).toBeTruthy());
        const queuedThumbnail = document.querySelector('[data-testid^="buffer-entry-"] img') as HTMLImageElement | null;
        expect(queuedThumbnail?.getAttribute('src')).toBe('blob:test-image');
        expect(sendMessage).not.toHaveBeenCalled();
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
            { id: 'queued-idle-edit', text: 'queued before idle edit', attachments: [], createdAt: 1, autoDrain: false },
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
            { id: 'queued-empty-edit', text: 'clear me from queue', attachments: [], createdAt: 1, autoDrain: false },
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
                    'Background task launched.',
                    'The task is available in the background list.',
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
                    'Background task launched.',
                    'The task is available in the background list.',
                    'session_id: session-only',
                ].join('\n'),
            }),
        ];

        const { container, getByText } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
        });

        expect(container.textContent).toContain('session_id: session-only');
        expect(container.textContent).not.toContain('job_id:');
        expect(container.textContent).not.toContain('run_id:');
    });

    it('renders structured unfinished slot cards and actions', async () => {
        const executeAction = vi.fn<() => Promise<void>>().mockResolvedValue();
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: 'Unfinished task available.',
                unfinishedSlot: {
                    slotID: 'slot-1',
                    title: 'Review Daily Paper',
                    summary: 'The previous task stopped before completion.',
                    projectPath: 'D:/work/project',
                    status: 'pending_resume',
                    actions: [
                        { label: 'Resume previous task', command: '__resume_unfinished__ slot-1', style: 'default' },
                        { label: 'Start a new task', command: '__start_new_task__', style: 'default' },
                    ],
                },
            }),
        ];

        const { getByTestId, getByText } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction, refreshNews: () => {} },
        });

        expect(getByTestId('unfinished-slot-card')).toBeTruthy();
        expect(getByTestId('unfinished-slot-title').textContent).toContain('Review Daily Paper');
        expect(getByTestId('unfinished-slot-summary').textContent).toContain('The previous task stopped before completion.');
        expect(getByTestId('unfinished-slot-project').textContent).toContain('D:/work/project');
        expect(getByTestId('unfinished-slot-status').textContent).toBeTruthy();
        expect(getByText('Resume previous task')).toBeTruthy();
        expect(getByText('Start new task')).toBeTruthy();
    });

    it('localizes unfinished slot status in Chinese', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: 'Unfinished task available.',
                unfinishedSlot: {
                    slotID: 'slot-zh',
                    title: 'Resume task',
                    status: 'resumed',
                },
            }),
        ];

        const { getByTestId } = renderPanel({
            lang: 'zh-Hans',
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction: async () => {}, refreshNews: () => {} },
        });

        expect(getByTestId('unfinished-slot-status').textContent || '').toContain('已恢复');
    });

    it('localizes interrupted unfinished slot details in Chinese', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: 'Detected an unfinished task: Login to Zhihu. Choose resume to continue it.',
                unfinishedSlot: {
                    slotID: 'slot-interrupted',
                    title: '登录知乎发表一下码卡龙 6发布感言。',
                    summary: 'Previous task stopped making progress and was moved to recovery.',
                    status: 'interrupted',
                },
            }),
        ];

        const { getByTestId, getByText, queryByText } = renderPanel({
            lang: 'zh-Hans',
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction: async () => {}, refreshNews: () => {} },
        });

        expect(getByTestId('unfinished-slot-status').textContent || '').toContain('已中断');
        expect(getByTestId('unfinished-slot-summary').textContent || '').toContain('上次任务停止推进');
        expect(getByText(/检测到未完成任务/)).toBeTruthy();
        expect(queryByText(/Previous task stopped making progress/)).toBeNull();
    });

    it('localizes unfinished slot notice fallback title in Chinese', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: 'Detected an unfinished task: Previous task stopped making progress and was moved to recovery. Choose resume to continue it.',
                unfinishedSlot: {
                    slotID: 'slot-summary-title',
                    summary: 'Previous task stopped making progress and was moved to recovery.',
                    status: 'interrupted',
                },
            }),
        ];

        const { getByText, getByTestId, queryByText } = renderPanel({
            lang: 'zh-Hans',
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction: async () => {}, refreshNews: () => {} },
        });

        expect(getByText(/\u68c0\u6d4b\u5230\u672a\u5b8c\u6210\u4efb\u52a1/)).toBeTruthy();
        expect(getByTestId('unfinished-slot-summary').textContent || '').toContain('\u4e0a\u6b21\u4efb\u52a1\u505c\u6b62\u63a8\u8fdb');
        expect((getByText(/\u68c0\u6d4b\u5230\u672a\u5b8c\u6210\u4efb\u52a1/).textContent || '')).not.toContain('\u3002\u3002');
        expect(queryByText(/Previous task stopped making progress/)).toBeNull();
    });

    it('localizes unfinished slot status in Traditional Chinese', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: 'Detected an unfinished task: Continue work. Choose resume to continue it.',
                unfinishedSlot: {
                    slotID: 'slot-hant-status',
                    title: '繼續整理報告',
                    status: 'max_rounds_reached',
                },
            }),
        ];

        const { getByTestId, getByText, queryByText } = renderPanel({
            lang: 'zh-Hant',
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction: async () => {}, refreshNews: () => {} },
        });

        expect(getByTestId('unfinished-slot-status').textContent || '').toContain('達到最大輪次');
        expect(getByText(/偵測到未完成任務/)).toBeTruthy();
        expect(queryByText(/Detected an unfinished task/)).toBeNull();
    });

    it('localizes unfinished slot notice, title, and action labels in Chinese', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: 'Detected an unfinished task: Continue system optimization. Choose resume to continue it.',
                unfinishedSlot: {
                    slotID: 'slot-localized',
                    title: '继续优化系统性能',
                    actions: [
                        { label: 'Resume previous task', command: '__resume_unfinished__ slot-localized', style: 'default' },
                        { label: 'Start new task', command: '__dismiss_unfinished__ slot-localized', style: 'default' },
                    ],
                },
            }),
        ];

        const { getByText, queryByText } = renderPanel({
            lang: 'zh-Hans',
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction: async () => {}, refreshNews: () => {} },
        });

        expect(getByText(/检测到未完成任务/)).toBeTruthy();
        expect(getByText('未完成项')).toBeTruthy();
        expect(getByText('继续上次任务')).toBeTruthy();
        expect(getByText('开始新任务')).toBeTruthy();
        expect(queryByText('Unfinished item')).toBeNull();
        expect(queryByText('Start new task')).toBeNull();
    });

    it('localizes recoverable session action labels in Chinese', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '\u53ef\u6062\u590d\u4f1a\u8bdd',
                actions: [
                    { label: 'Resume session', command: '__resume_session__ sess-1', style: 'default' },
                    { label: 'Dismiss session', command: '__dismiss_recoverable_session__ sess-1', style: 'danger' },
                ],
            }),
        ];

        const { getByText, queryByText } = renderPanel({
            lang: 'zh-Hans',
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction: async () => {}, refreshNews: () => {} },
        });

        expect(getByText('\u6062\u590d\u4f1a\u8bdd')).toBeTruthy();
        expect(getByText('\u5ffd\u7565\u4f1a\u8bdd')).toBeTruthy();
        expect(queryByText('Resume session')).toBeNull();
        expect(queryByText('Dismiss session')).toBeNull();
    });

    it('renders recoverable session cards in Chinese', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '\u68c0\u6d4b\u5230\u53ef\u6062\u590d\u4f1a\u8bdd',
                recoverableSession: {
                    sessionID: 'sess-1',
                    title: '\u7ee7\u7eed Daily Paper',
                    summary: '\u8fd8\u5dee\u6700\u540e\u4e00\u8f6e\u6574\u7406',
                    projectPath: 'D:/work/project',
                    status: 'exited',
                    actions: [
                        { label: 'Resume session', command: '__resume_session__ sess-1', style: 'default' },
                        { label: 'Dismiss session', command: '__dismiss_recoverable_session__ sess-1', style: 'danger' },
                    ],
                },
            }),
        ];

        const { getByTestId, getByText, queryByText } = renderPanel({
            lang: 'zh-Hans',
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction: async () => {}, refreshNews: () => {} },
        });

        expect(getByTestId('recoverable-session-card')).toBeTruthy();
        expect(getByTestId('recoverable-session-status').textContent || '').toContain('\u5df2\u9000\u51fa');
        expect(getByText('\u53ef\u6062\u590d\u4f1a\u8bdd')).toBeTruthy();
        expect(getByText('\u6062\u590d\u4f1a\u8bdd')).toBeTruthy();
        expect(getByText('\u5ffd\u7565\u4f1a\u8bdd')).toBeTruthy();
        expect(queryByText('Recoverable session')).toBeNull();
    });

    it('localizes recoverable session known English progress in Chinese', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '\u68c0\u6d4b\u5230\u53ef\u6062\u590d\u4f1a\u8bdd',
                recoverableSession: {
                    sessionID: 'sess-progress',
                    title: '\u7ee7\u7eed\u4efb\u52a1',
                    lastProgress: 'Previous task stopped making progress and was moved to recovery.',
                    status: 'exited',
                },
            }),
        ];

        const { getByTestId, queryByText } = renderPanel({
            lang: 'zh-Hans',
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction: async () => {}, refreshNews: () => {} },
        });

        expect(getByTestId('recoverable-session-summary').textContent || '').toContain('\u4e0a\u6b21\u4efb\u52a1\u505c\u6b62\u63a8\u8fdb');
        expect(queryByText(/Previous task stopped making progress/)).toBeNull();
    });

    it('localizes task handoff fallback statuses in Chinese', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '',
                unfinishedSlot: {
                    slotID: 'slot-failed',
                    title: 'fallback status',
                    status: 'failed',
                },
                recoverableSession: {
                    sessionID: 'sess-waiting',
                    title: 'waiting session',
                    status: 'waiting_input',
                },
            }),
        ];

        const { getByTestId, queryByText } = renderPanel({
            lang: 'zh-Hans',
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction: async () => {}, refreshNews: () => {} },
        });

        expect(getByTestId('unfinished-slot-status').textContent || '').toContain('\u5df2\u5931\u8d25');
        expect(getByTestId('recoverable-session-status').textContent || '').toContain('\u7b49\u5f85\u8f93\u5165');
        expect(queryByText(/failed/)).toBeNull();
        expect(queryByText(/waiting input|waiting_input/i)).toBeNull();
    });

    it('unfinished slot card buttons reuse executeAction', async () => {
        const executeAction = vi.fn<() => Promise<void>>().mockResolvedValue();
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: 'Unfinished task available.',
                unfinishedSlot: {
                    slotID: 'slot-2',
                    title: 'Resume task',
                    actions: [
                        { label: 'Resume previous task', command: '__resume_unfinished__ slot-2', style: 'default' },
                    ],
                },
            }),
        ];

        const { getByText } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction, refreshNews: () => {} },
        });

        fireEvent.click(getByText('Resume previous task'));

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
                        { label: 'Resume previous task', command: '__resume_unfinished__ slot-layout', style: 'default' },
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
                content: 'Unfinished task path.',
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
                content: 'Please confirm the coding task for /work/project',
                confirmation: {
                    id: 'c1',
                    summary: 'Review the login flow and related code in /work/project',
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
        expect(getByTestId('confirmation-status').textContent).toMatch(/Pending|pending|待确认/);
        expect(getByText('Confirm and start')).toBeTruthy();
        expect(getByText('Cancel')).toBeTruthy();
    });

    it('localizes confirmation fallback labels and enum values', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: 'confirm',
                confirmation: {
                    id: 'c-zh',
                    summary: 'ready',
                    taskType: 'coding',
                    status: 'running',
                },
                actions: [
                    { label: 'Confirm and start', command: '__confirm_execution__ c-zh', style: 'default' },
                ],
            }),
        ];

        const { container } = renderPanel({
            lang: 'zh-Hans',
            state: { messages, sending: false, streaming: false, ready: true },
        });

        expect(container.textContent).toContain('\u6267\u884c\u524d\u786e\u8ba4 - \u4ee3\u7801\u4efb\u52a1');
        expect(container.textContent).toContain('\u72b6\u6001: \u6267\u884c\u4e2d');
        expect(container.textContent).toContain('\u786e\u8ba4\u5e76\u5f00\u59cb');
    });

    it('renders backend-provided confirmation labels and primary action styling', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: 'confirm',
                confirmation: {
                    id: 'c-labels',
                    summary: 'ready',
                    status: 'pending',
                    targetPaths: ['D:/work/project'],
                    labels: {
                        title: 'Custom confirmation',
                        status: 'Custom status',
                        target_paths: 'Custom paths',
                    },
                },
                actions: [
                    { label: 'Confirm and start', command: '__confirm_execution__ c-labels', style: 'primary' },
                ],
            }),
        ];

        const { container, getByTestId } = renderPanel({
            lang: 'en',
            state: { messages, sending: false, streaming: false, ready: true },
        });

        expect(container.textContent).toContain('Custom confirmation');
        expect(getByTestId('confirmation-status').textContent).toContain('Custom status');
        expect(getByTestId('confirmation-target-paths').textContent).toContain('Custom paths');
        const button = getByTestId('action-button') as HTMLButtonElement;
        expect(button.style.background).not.toBe('transparent');
        expect(button.style.color).toBe('rgb(255, 255, 255)');
    });

    it('confirmation card buttons reuse executeAction', async () => {
        const executeAction = vi.fn<() => Promise<void>>().mockResolvedValue();
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: 'Please confirm this task.',
                confirmation: {
                    id: 'c2',
                    summary: 'Confirm before starting work.',
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
        expect(container.textContent).toContain('View trace');
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
                progressMessages: [makeMsg({ role: 'progress', content: 'done' })],
                sending: true,
                streaming: false,
                ready: true,
            },
        });

        expect(getByText('done')).toBeTruthy();
    });

    it('keeps tool-running status in one compact row while busy', () => {
        const { container, queryByText } = renderPanel({
            lang: 'zh-Hans',
            state: {
                messages: [makeMsg({ role: 'user', content: '南京天气' })],
                progressMessages: [makeMsg({ role: 'progress', content: '🚀 正在执行 Skill「Weather Query 🌤」...' })],
                sending: true,
                streaming: false,
                ready: true,
            },
        });

        expect(queryByText('🚀 正在执行 Skill「Weather Query 🌤」...')).toBeNull();
        expect(container.textContent).toContain('正在执行 Weather Query 🌤');
        expect(container.textContent).toContain('可继续输入');
        expect((container.textContent || '').match(/Weather Query/g) || []).toHaveLength(1);
    });

    it('summarizes shell-style tool paths without duplicating the chat row', () => {
        const { container, queryByText } = renderPanel({
            lang: 'zh-Hans',
            state: {
                messages: [makeMsg({ role: 'user', content: '南京天气' })],
                progressMessages: [makeMsg({ role: 'progress', content: '🚀 正在执行 Shell /Weather Query 🌤 / ...' })],
                sending: true,
                streaming: false,
                ready: true,
            },
        });

        expect(queryByText('🚀 正在执行 Shell /Weather Query 🌤 / ...')).toBeNull();
        expect(container.textContent).toContain('正在执行 Weather Query 🌤');
        expect(container.textContent).toContain('可继续输入');
        expect((container.textContent || '').match(/Weather Query/g) || []).toHaveLength(1);
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
        expect(status.textContent).toContain('T2');
        expect(status.textContent).toContain('Fix stale edit guard');
        expect(titleStatus.textContent).toContain('T2');
        expect(titleStatus.getAttribute('aria-label')).toContain('Fix stale edit guard');
        expect(titleStatus.textContent).toContain('T2');
        expect(titleStatus.style.color).toBe('rgb(47, 95, 152)');
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
        expect((getByTestId('ai-input') as HTMLTextAreaElement).placeholder).toContain('T2');
        expect((getByTestId('ai-input') as HTMLTextAreaElement).placeholder).toContain('T2');
        expect(getByText('T2')).toBeTruthy();
        expect(getByText('Fix stale edit guard')).toBeTruthy();
    });

    it('compacts multiple coding agent progress messages to the latest row', () => {
        const { getByText, queryByText } = renderPanel({
            lang: 'zh-Hans',
            state: {
                messages: [makeMsg({ role: 'user', content: 'fix these tasks' })],
                progressMessages: [
                    makeMsg({ role: 'progress', content: 'preflight checks done' }),
                    makeMsg({ role: 'progress', content: 'Coding Agent: running T1 - First task' }),
                    makeMsg({ role: 'progress', content: 'Coding Agent: running T2 - Second task' }),
                ],
                sending: true,
                streaming: false,
                ready: true,
            },
        });

        expect(getByText('preflight checks done')).toBeTruthy();
        expect(queryByText('First task')).toBeNull();
        expect(getByText('Second task')).toBeTruthy();
    });

    it('hides completed coding agent progress once idle', () => {
        const { queryByTestId } = renderPanel({
            lang: 'zh-Hans',
            state: {
                messages: [makeMsg({ role: 'user', content: 'fix this bug' })],
                progressMessages: [makeMsg({ role: 'progress', content: 'Coding Agent: completed T2 - Fix stale edit guard' })],
                sending: false,
                streaming: false,
                ready: true,
            },
        });

        expect(queryByTestId('coding-agent-progress')).toBeNull();
        expect(queryByTestId('coding-agent-title-status')).toBeNull();
    });

    it('renders a terminal fallback message with trace action and fields', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: 'PDF generation failed after tool execution',
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
        const { container, getAllByText, getByText } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction, refreshNews: () => {} },
        });

        expect(getAllByText('PDF generation failed after tool execution').length).toBeGreaterThan(0);
        const fieldCards = Array.from(container.querySelectorAll('[data-testid="field-card"]')).map(el => el.textContent || '');
        expect(fieldCards).toContain('Trace:PDF generation failed after tool execution');
        expect(fieldCards).toContain('Trace events:4');
        expect(fieldCards).toContain('Evidence:2');
        expect(fieldCards).toContain('Run ID:run-empty-result');
        expect(container.textContent).toContain('View trace');
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
                content: 'Saved file at C:\\Users\\demo\\report.pdf for review.',
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

    it('renders image_key screenshots without requiring a saved local file', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '',
                imageKey: 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+yF9kAAAAASUVORK5CYII=',
            }),
        ];

        const { getByAltText } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
        });

        expect(getByAltText('screenshot')).toBeTruthy();
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

    it('keeps project tab draft text isolated from the local assistant draft', async () => {
        const setDraftInputValue = vi.fn();
        const onHandled = vi.fn();

        const { getByTestId, getByText } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/draft-isolated',
                taskTitle: 'Draft isolated task',
                autoSend: false,
            },
            onPendingProjectTabOpenHandled: onHandled,
            state: { messages: [], sending: false, streaming: false, ready: true, draftInputValue: 'local draft' },
            actions: { setDraftInputValue },
        });

        await waitFor(() => expect(onHandled).toHaveBeenCalled());
        expect(getByText('Draft isolated task')).toBeTruthy();

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'project draft' } });
        expect(setDraftInputValue).not.toHaveBeenCalledWith('project draft');

        fireEvent.click(getByTestId('ai-tab-local'));
        await waitFor(() => expect((getByTestId('ai-input') as HTMLTextAreaElement).value).toBe('local draft'));

        fireEvent.click(getByTestId('ai-tab-overflow-btn'));
        fireEvent.click(getByText('Draft isolated task'));
        await waitFor(() => expect((getByTestId('ai-input') as HTMLTextAreaElement).value).toBe('project draft'));
    });

    it('does not clear the local assistant history when clearing a project tab', async () => {
        const clearHistory = vi.fn().mockResolvedValue(undefined);
        const onHandled = vi.fn();

        const { getByTitle } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/project-clear-isolated',
                taskTitle: 'Project clear isolated',
                autoSend: false,
            },
            onPendingProjectTabOpenHandled: onHandled,
            state: { messages: [], sending: false, streaming: false, ready: true },
            actions: { clearHistory },
        });

        await waitFor(() => expect(onHandled).toHaveBeenCalled());
        fireEvent.click(getByTitle('New conversation'));

        expect(clearHistory).not.toHaveBeenCalled();
    });

    it('keeps a project tab input unlocked while the local assistant session is busy', async () => {
        const sendMessage = vi.fn().mockResolvedValue(true);
        const onHandled = vi.fn();

        const { getByTestId, queryByTestId } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/beijing-weather-idle',
                taskTitle: 'Beijing weather idle',
                autoSend: false,
            },
            onPendingProjectTabOpenHandled: onHandled,
            state: {
                messages: [],
                sending: true,
                sendingSessionKey: 'desktop-user',
                streaming: true,
                streamingSessionKey: 'desktop-user',
                ready: true,
            },
            actions: { sendMessage },
        });

        await waitFor(() => expect(onHandled).toHaveBeenCalled());
        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        expect(input.placeholder).toBe('Type a message...');
        fireEvent.change(input, { target: { value: 'project tab should not inherit local busy' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await waitFor(() => expect(sendMessage).toHaveBeenCalledWith('project tab should not inherit local busy', expect.objectContaining({
            project_path: 'D:/tasks/beijing-weather-idle',
        })));
        expect(queryByTestId('buffer-queue-panel')).toBeNull();
    });

    it('keeps the local assistant input unlocked while a project tab session is busy', async () => {
        const sendMessage = vi.fn().mockResolvedValue(true);
        const onHandled = vi.fn();

        const { getByTestId, queryByTestId } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/project-busy-local-idle',
                taskTitle: 'Project busy local idle',
                autoSend: false,
            },
            onPendingProjectTabOpenHandled: onHandled,
            state: {
                messages: [{ id: 'local-msg-1', role: 'user', content: 'earlier question' }],
                sending: true,
                sendingSessionKey: 'desktop-user:D:/tasks/project-busy-local-idle',
                streaming: true,
                streamingSessionKey: 'desktop-user:D:/tasks/project-busy-local-idle',
                ready: true,
            },
            actions: { sendMessage },
        });

        await waitFor(() => expect(onHandled).toHaveBeenCalled());
        fireEvent.click(getByTestId('ai-tab-local'));
        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        expect(input.placeholder).toBe('Type a message...');
        fireEvent.change(input, { target: { value: 'local tab should not inherit project busy' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await waitFor(() => expect(sendMessage).toHaveBeenCalledWith('local tab should not inherit project busy'));
        expect(queryByTestId('buffer-queue-panel')).toBeNull();
    });

    it('keeps the local assistant input queued when its own detached session is busy', async () => {
        const sendMessage = vi.fn().mockResolvedValue(true);
        const onHandled = vi.fn();

        const { getByTestId, getByText } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/project-active-local-detached',
                taskTitle: 'Project active local detached',
                autoSend: false,
            },
            onPendingProjectTabOpenHandled: onHandled,
            state: {
                messages: [],
                sending: true,
                sendingSessionKey: 'desktop-user:D:/tasks/project-active-local-detached',
                busySessionKeys: ['desktop-user', 'desktop-user:D:/tasks/project-active-local-detached'],
                streaming: true,
                streamingSessionKey: 'desktop-user:D:/tasks/project-active-local-detached',
                streamingSessionKeys: ['desktop-user:D:/tasks/project-active-local-detached'],
                ready: true,
            },
            actions: { sendMessage },
        });

        await waitFor(() => expect(onHandled).toHaveBeenCalled());
        fireEvent.click(getByTestId('ai-tab-local'));
        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        expect(input.placeholder).toBe('Running tools... (you can type ahead)');
        fireEvent.change(input, { target: { value: 'local detached should queue' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await waitFor(() => expect(getByText('local detached should queue')).toBeTruthy());
        expect(sendMessage).not.toHaveBeenCalled();
    });

    it('keeps a project tab queued when its detached session remains busy after the active round idles', async () => {
        localStorage.removeItem('ai_assistant_buffer_queue');
        const sendMessage = vi.fn().mockResolvedValue(true);
        const onHandled = vi.fn();

        const { getByTestId, getByText } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/detached-project-still-busy',
                taskTitle: 'Detached project still busy',
                autoSend: false,
            },
            onPendingProjectTabOpenHandled: onHandled,
            state: {
                messages: [],
                progressMessages: [makeMsg({ role: 'progress', content: 'Coding Agent: running project session only' })],
                sending: false,
                busySessionKeys: ['desktop-user:D:/tasks/detached-project-still-busy'],
                streaming: false,
                streamingSessionKeys: [],
                ready: true,
            },
            actions: { sendMessage },
        });

        await waitFor(() => expect(onHandled).toHaveBeenCalled());
        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        expect(input.placeholder).toBe('Running tools... (you can type ahead)');
        expect(getByText('Coding Agent: running project session only')).toBeTruthy();
        fireEvent.change(input, { target: { value: 'project detached should queue' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await waitFor(() => expect(getByText('project detached should queue')).toBeTruthy());
        expect(sendMessage).not.toHaveBeenCalled();
    });

    it('shows thinking only for the project tab whose detached session is streaming', async () => {
        const sendMessage = vi.fn().mockResolvedValue(true);
        const onHandled = vi.fn();

        const { getByTestId } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/detached-project-streaming',
                taskTitle: 'Detached project streaming',
                autoSend: false,
            },
            onPendingProjectTabOpenHandled: onHandled,
            state: {
                messages: [{ id: 'local-msg-1', role: 'user', content: 'earlier question' }],
                sending: false,
                busySessionKeys: ['desktop-user:D:/tasks/detached-project-streaming'],
                streaming: false,
                streamingSessionKeys: ['desktop-user:D:/tasks/detached-project-streaming'],
                ready: true,
            },
            actions: { sendMessage },
        });

        await waitFor(() => expect(onHandled).toHaveBeenCalled());
        expect((getByTestId('ai-input') as HTMLTextAreaElement).placeholder).toBe('Thinking... (you can type ahead)');

        fireEvent.click(getByTestId('ai-tab-local'));
        expect((getByTestId('ai-input') as HTMLTextAreaElement).placeholder).toBe('Type a message...');
    });

    it('keeps an idle project tab input unlocked when legacy busy state has no session key', async () => {
        const sendMessage = vi.fn().mockResolvedValue(true);
        const onHandled = vi.fn();

        const { getByTestId, queryByTestId } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/legacy-busy-project-idle',
                taskTitle: 'Legacy busy project idle',
                autoSend: false,
            },
            onPendingProjectTabOpenHandled: onHandled,
            state: {
                messages: [],
                sending: true,
                streaming: true,
                ready: true,
            },
            actions: { sendMessage },
        });

        await waitFor(() => expect(onHandled).toHaveBeenCalled());
        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        expect(input.placeholder).toBe('Type a message...');
        fireEvent.change(input, { target: { value: 'legacy busy should not queue project input' } });
        fireEvent.keyDown(input, { key: 'Enter' });

        await waitFor(() => expect(sendMessage).toHaveBeenCalledWith('legacy busy should not queue project input', expect.objectContaining({
            project_path: 'D:/tasks/legacy-busy-project-idle',
        })));
        expect(queryByTestId('buffer-queue-panel')).toBeNull();
    });

    it('keeps queued project input isolated from the local assistant tab', async () => {
        localStorage.removeItem('ai_assistant_buffer_queue');
        const onHandled = vi.fn();

        const { getByTestId, queryByTestId, getByText } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/queue-isolated',
                taskTitle: 'Queue isolated task',
                autoSend: false,
            },
            onPendingProjectTabOpenHandled: onHandled,
            state: { messages: [], sending: true, sendingSessionKey: 'desktop-user:D:/tasks/queue-isolated', streaming: true, streamingSessionKey: 'desktop-user:D:/tasks/queue-isolated', ready: true, draftInputValue: 'local draft' },
        });

        await waitFor(() => expect(onHandled).toHaveBeenCalled());
        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: 'project queued only' } });
        fireEvent.keyDown(input, { key: 'Enter' });
        await waitFor(() => expect(getByTestId('buffer-queue-panel').textContent || '').toContain('project queued only'));

        fireEvent.click(getByTestId('ai-tab-local'));
        await waitFor(() => expect(queryByTestId('buffer-queue-panel')).toBeNull());

        fireEvent.click(getByTestId('ai-tab-overflow-btn'));
        fireEvent.click(getByText('Queue isolated task'));
        await waitFor(() => expect(getByTestId('buffer-queue-panel').textContent || '').toContain('project queued only'));
    });

    it('closes matching project tab on backend task closed event', async () => {
        const onHandled = vi.fn();

        const { getByText, queryByText } = renderPanel({
            pendingProjectTabOpen: {
                projectPath: 'D:/tasks/close-event',
                taskTitle: 'Close event task',
                autoSend: false,
            },
            onPendingProjectTabOpenHandled: onHandled,
            state: { messages: [], sending: false, streaming: false, ready: true },
        });

        await waitFor(() => expect(onHandled).toHaveBeenCalled());
        expect(getByText('Close event task')).toBeTruthy();

        const closeEventHandler = runtimeEventsOnMock.mock.calls.filter(([eventName]) => eventName === 'project-task:closed').at(-1)?.[1];
        expect(closeEventHandler).toBeTypeOf('function');
        act(() => closeEventHandler('D:/tasks/close-event'));

        await waitFor(() => expect(queryByText('Close event task')).toBeNull());
    });

    it('opens digital employee invite picker after converting a direct VE chat to group mode', async () => {
        const onPendingVEOpenHandled = vi.fn();
        const { getByRole, getByTestId, queryByTestId, getByText } = renderPanel({
            lang: 'en',
            pendingVEOpen: {
                id: 've-a',
                machine_id: 've-a',
                name: 'Agent A',
                online_status: 'online',
                status: 'active',
                access_policy: 'public',
                skill_description: '',
            } as any,
            onPendingVEOpenHandled,
            window: { inline: true },
        });

        await waitFor(() => expect(getByRole('tab', { name: 'Agent A' })).toBeTruthy());
        fireEvent.contextMenu(getByRole('tab', { name: /Agent A/ }));
        fireEvent.click(getByTestId('tab-menu-invite-ve'));

        await waitFor(() => expect(getByTestId('tab-participant-invite-dialog')).toBeTruthy());
        expect(queryByTestId('tab-participant-invite-item-ve-a')).toBeNull();
        await waitFor(() => expect(getByText('Contract Bot')).toBeTruthy());
        fireEvent.click(getByText('Contract Bot'));

        await waitFor(() => expect(addVEToGroupMock).toHaveBeenCalledWith('session-ve-a', 've-b'));
        expect(initiateVEConversationMock).toHaveBeenCalledWith('ve-a');
    });

    it('renames an upgraded group chat through the hub binding', async () => {
        const { getByRole, getByTestId, getByText } = renderPanel({
            lang: 'en',
            pendingVEOpen: {
                id: 've-a',
                machine_id: 've-a',
                name: 'Agent A',
                online_status: 'online',
                status: 'active',
                access_policy: 'public',
                skill_description: '',
            } as any,
            onPendingVEOpenHandled: vi.fn(),
            window: { inline: true },
        });

        await waitFor(() => expect(getByRole('tab', { name: 'Agent A' })).toBeTruthy());
        fireEvent.contextMenu(getByRole('tab', { name: /Agent A/ }));
        fireEvent.click(getByTestId('tab-menu-invite-ve'));
        await waitFor(() => expect(getByTestId('tab-participant-invite-dialog')).toBeTruthy());
        await waitFor(() => expect(getByText('Contract Bot')).toBeTruthy());
        fireEvent.click(getByText('Contract Bot'));
        await waitFor(() => expect(addVEToGroupMock).toHaveBeenCalledWith('session-ve-a', 've-b'));

        fireEvent.contextMenu(getByRole('tab', { name: /Agent A/ }));
        fireEvent.click(getByTestId('tab-menu-rename-group'));
        const input = getByTestId('rename-group-input') as HTMLInputElement;
        fireEvent.change(input, { target: { value: 'Planning room' } });
        fireEvent.click(getByTestId('rename-group-save'));

        await waitFor(() => expect(renameGroupDiscussionMock).toHaveBeenCalledWith('session-ve-a', 'Planning room'));
        await waitFor(() => expect(getByRole('tab', { name: 'Planning room' })).toBeTruthy());
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

        expect(getByTestId('ai-cancel-progress').textContent || '').not.toContain('Loading');
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
        const { getByTestId, getByText } = renderPanel({
            state: { messages: [], sending: true, streaming: true, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction: async () => {}, refreshNews: () => {}, cancelSession: async () => ({ canceledText: '' }) },
        });

        expect(getByTestId('ai-cancel-progress').textContent || '').not.toContain('Loading');
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
        expect(getByTestId('ai-hide-toggle').querySelector('svg')).toBeTruthy();
        expect(getByTestId('ai-maximize-toggle').getAttribute('title')).toBe('Maximize window');
    });

    it('double-clicking the title bar toggles inline maximized view', () => {
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
    it('opens pending history discussions as read-only group tabs', async () => {
        const onHandled = vi.fn();
        const { getByTestId, getAllByText } = renderPanel({
            pendingHistoryDiscussionOpen: {
                id: 'disc-1',
                topic: 'Vendor audit',
                local_relation: 'owned_ve_invited',
                readonly: true,
                status: 'open',
                participant_ids: ['ve-a'],
            } as any,
            onPendingHistoryDiscussionOpenHandled: onHandled,
        });

        await waitFor(() => expect(onHandled).toHaveBeenCalledTimes(1));
        expect(getByTestId('ai-tab-history-disc-1')).toBeTruthy();
        expect(getAllByText('Read-only').length).toBeGreaterThan(0);
        expect(getByTestId('ai-history-group-tab-disc-1')).toBeTruthy();
    });
});
