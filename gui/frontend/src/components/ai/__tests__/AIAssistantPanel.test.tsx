import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, cleanup, fireEvent, waitFor } from '@testing-library/react';
import * as fc from 'fast-check';
import { AIAssistantPanel } from '../AIAssistantPanel';
import type { ChatMessage, CancelAIAssistantResult, NewsCardData, ChatAction } from '../useAIAssistant';
import { DialogProvider } from '../../CustomDialog';

const { openFileOrShowInFolderMock } = vi.hoisted(() => ({
    openFileOrShowInFolderMock: vi.fn(),
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

// ── Mock Wails runtime (not used by panel but imported transitively) ──
vi.mock('../../../../wailsjs/runtime', () => ({
    BrowserOpenURL: vi.fn(),
    EventsOn: vi.fn(),
    EventsOff: vi.fn(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    OpenFileOrShowInFolder: openFileOrShowInFolderMock,
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
    return render(<AIAssistantPanel {...props} />, { wrapper: DialogProvider });
}

describe('AIAssistantPanel property tests', () => {
    afterEach(() => {
        cleanup();
        window.localStorage.clear();
        scrollIntoViewMock.mockClear();
        scrollToMock.mockClear();
        openFileOrShowInFolderMock.mockReset();
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
        expect(buttons).toHaveLength(4);
        expect(buttons[0]?.getAttribute('title')).toBe('Search tasks');
        expect(buttons[1]?.getAttribute('title')).toContain('Voice readback OFF');
        expect(buttons[2]?.getAttribute('title')).toBe('Switch to dark mode');
        expect(buttons[3]?.getAttribute('title')).toBe('New conversation');
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
        const { getByTestId, getByText } = renderPanel({
            state: { messages: [], sending: true, streaming: false, ready: true, visualBusy: false },
        });

        const input = getByTestId('ai-input') as HTMLTextAreaElement;
        expect(input.placeholder).toBe('Running tools... (you can type ahead)');
        expect(getByText('Running tools... (you can type ahead)')).toBeTruthy();
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

    it('renders structured unfinished slot cards and actions', async () => {
        const executeAction = vi.fn<() => Promise<void>>().mockResolvedValue();
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '检测到一个未完成任务。',
                unfinishedSlot: {
                    slotID: 'slot-1',
                    title: '继续 Daily Paper',
                    summary: '还差最后一轮整理',
                    projectPath: 'D:/work/project',
                    status: 'pending_resume',
                    actions: [
                        { label: '继续上次任务', command: '__resume_unfinished__ slot-1', style: 'default' },
                        { label: '开始新任务', command: '__start_new_task__', style: 'default' },
                    ],
                },
            }),
        ];

        const { getByTestId, getByText } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction, refreshNews: () => {} },
        });

        expect(getByTestId('unfinished-slot-card')).toBeTruthy();
        expect(getByTestId('unfinished-slot-title').textContent).toContain('继续 Daily Paper');
        expect(getByTestId('unfinished-slot-summary').textContent).toContain('还差最后一轮整理');
        expect(getByTestId('unfinished-slot-project').textContent).toContain('D:/work/project');
        expect(getByText('继续上次任务')).toBeTruthy();
        expect(getByText('开始新任务')).toBeTruthy();
    });

    it('unfinished slot card buttons reuse executeAction', async () => {
        const executeAction = vi.fn<() => Promise<void>>().mockResolvedValue();
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '检测到一个未完成任务。',
                unfinishedSlot: {
                    slotID: 'slot-2',
                    title: '继续旧任务',
                    actions: [
                        { label: '继续上次任务', command: '__resume_unfinished__ slot-2', style: 'default' },
                    ],
                },
            }),
        ];

        const { getByText } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction, refreshNews: () => {} },
        });

        fireEvent.click(getByText('继续上次任务'));

        await waitFor(() => {
            expect(executeAction).toHaveBeenCalledWith('__resume_unfinished__ slot-2');
        });
    });

    it('unfinished slot project path opens through the shared file handler', async () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '存在未完成任务。',
                unfinishedSlot: {
                    slotID: 'slot-path',
                    summary: '上次停在这里',
                    projectPath: 'D:/work/project',
                },
            }),
        ];

        const { getByText } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction: async () => {}, refreshNews: () => {} },
        });

        fireEvent.click(getByText('📁 D:/work/project'));

        await waitFor(() => {
            expect(openFileOrShowInFolderMock).toHaveBeenCalledWith('D:/work/project');
        });
    });

    it('renders confirmation cards with summary, lists, and actions', () => {
        const executeAction = vi.fn<() => Promise<void>>().mockResolvedValue();
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '请先确认后再执行。',
                confirmation: {
                    id: 'c1',
                    summary: '我理解你想修复登录问题\n默认工作目录：D:/work/project',
                    taskType: 'coding',
                    targetPaths: ['D:/work/project'],
                    plannedActions: ['检查登录流程', '修改相关代码'],
                    riskFlags: ['会直接改代码'],
                    revisionHints: ['如目录不对请直接修正'],
                    status: 'pending',
                },
                actions: [
                    { label: '确认并开始', command: '确认，按这个开始', style: 'default' },
                    { label: '取消', command: '取消这个任务', style: 'danger' },
                ],
            }),
        ];

        const { getByTestId, getByText } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction, refreshNews: () => {} },
        });

        expect(getByTestId('confirmation-card')).toBeTruthy();
        expect(getByTestId('confirmation-summary').textContent).toContain('我理解你想修复登录问题');
        expect(getByTestId('confirmation-target-paths').textContent).toContain('D:/work/project');
        expect(getByTestId('confirmation-planned-actions').textContent).toContain('检查登录流程');
        expect(getByTestId('confirmation-risk-flags').textContent).toContain('会直接改代码');
        expect(getByTestId('confirmation-revision-hints').textContent).toContain('如目录不对请直接修正');
        expect(getByTestId('confirmation-status').textContent).toContain('pending');
        expect(getByText('确认并开始')).toBeTruthy();
        expect(getByText('取消')).toBeTruthy();
    });

    it('confirmation card buttons reuse executeAction', async () => {
        const executeAction = vi.fn<() => Promise<void>>().mockResolvedValue();
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '请确认。',
                confirmation: {
                    id: 'c2',
                    summary: '确认目录和任务后再执行',
                },
                actions: [
                    { label: '确认并开始', command: '确认，按这个开始', style: 'default' },
                ],
            }),
        ];

        const { getByText } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
            actions: { sendMessage: async () => {}, clearHistory: async () => {}, executeAction, refreshNews: () => {} },
        });

        fireEvent.click(getByText('确认并开始'));

        await waitFor(() => {
            expect(executeAction).toHaveBeenCalledWith('确认，按这个开始');
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
                progressMessages: [makeMsg({ role: 'progress', content: '⏳ 已接近最大推理轮次，正在基于现有信息收尾并生成最终结果…' })],
                sending: true,
                streaming: false,
                ready: true,
            },
        });

        expect(getByText('⏳ 已接近最大推理轮次，正在基于现有信息收尾并生成最终结果…')).toBeTruthy();
    });

    it('renders a terminal fallback message with trace action and fields', () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '任务未完成可交付结果。PDF generation failed after tool execution',
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

        expect(getByText('任务未完成可交付结果。PDF generation failed after tool execution')).toBeTruthy();
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

        const { getByText } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
        });

        expect(getByText('📄 Saved file: 📁 /tmp/review.pdf')).toBeTruthy();
    });

    it('opens saved file paths directly when clicked', async () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '',
                localFilePath: 'C:\\Users\\demo\\report.pdf',
            }),
        ];

        const { getByText } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
        });

        fireEvent.click(getByText('📄 Saved file: 📁 C:\\Users\\demo\\report.pdf'));

        await waitFor(() => {
            expect(openFileOrShowInFolderMock).toHaveBeenCalledWith('C:\\Users\\demo\\report.pdf');
        });
    });

    it('opens inline detected file paths when clicked', async () => {
        const messages: ChatMessage[] = [
            makeMsg({
                role: 'assistant',
                content: '文件在 C:\\Users\\demo\\report.pdf，请打开。',
            }),
        ];

        const { getByText } = renderPanel({
            state: { messages, sending: false, streaming: false, ready: true },
        });

        fireEvent.click(getByText('📂 C:\\Users\\demo\\report.pdf'));

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

        expect(getByTestId('ai-cancel-progress').textContent).not.toContain('■');
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
            { numRuns: 40 },
        );
    });
});
