import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import type { RemoteSessionView } from '../types';

const SendRemoteSessionInputMock = vi.fn();
const SendRemoteSessionRawInputMock = vi.fn();
const SendRemoteSessionImageMock = vi.fn();
const CaptureRemoteScreenshotMock = vi.fn();
const CaptureRemoteWindowScreenshotMock = vi.fn();
const InterruptRemoteSessionMock = vi.fn();
const GetBrowserSessionTraceMock = vi.fn();
const InvokeBrowserToolMock = vi.fn();
const ClipboardSetTextMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    SendRemoteSessionInput: (...args: unknown[]) => SendRemoteSessionInputMock(...args),
    SendRemoteSessionRawInput: (...args: unknown[]) => SendRemoteSessionRawInputMock(...args),
    SendRemoteSessionImage: (...args: unknown[]) => SendRemoteSessionImageMock(...args),
    CaptureRemoteScreenshot: (...args: unknown[]) => CaptureRemoteScreenshotMock(...args),
    CaptureRemoteWindowScreenshot: (...args: unknown[]) => CaptureRemoteWindowScreenshotMock(...args),
    InterruptRemoteSession: (...args: unknown[]) => InterruptRemoteSessionMock(...args),
    GetBrowserSessionTrace: (...args: unknown[]) => GetBrowserSessionTraceMock(...args),
    InvokeBrowserTool: (...args: unknown[]) => InvokeBrowserToolMock(...args),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    ClipboardSetText: (...args: unknown[]) => ClipboardSetTextMock(...args),
}));

import { RemoteSessionConsole } from '../RemoteSessionConsole';

function buildSession(overrides: Partial<RemoteSessionView> = {}): RemoteSessionView {
    return {
        id: 'sess-1',
        tool: 'claude',
        title: 'Remote AskUserQuestion',
        project_path: 'D:/workprj/aicoder',
        status: 'waiting_input',
        execution_mode: 'sdk',
        summary: {
            status: 'waiting_input',
            pending_question: {
                tool_use_id: 'toolu_123',
                tool_name: 'AskUserQuestion',
                header: 'Auth method',
                question: 'Which auth method should we use?',
                hint: 'Pick one option to continue',
                options: [
                    { label: 'OAuth', description: 'Use browser OAuth flow' },
                    { label: 'API Key', description: 'Use static credential' },
                ],
            },
        },
        preview: { preview_lines: [] },
        raw_output_lines: ['assistant: waiting for answer'],
        events: [],
        output_images: [],
        ...overrides,
    };
}

describe('RemoteSessionConsole AskUserQuestion UI', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        SendRemoteSessionInputMock.mockResolvedValue(undefined);
        SendRemoteSessionRawInputMock.mockResolvedValue(undefined);
        SendRemoteSessionImageMock.mockResolvedValue(undefined);
        CaptureRemoteScreenshotMock.mockResolvedValue(undefined);
        CaptureRemoteWindowScreenshotMock.mockResolvedValue(undefined);
        InterruptRemoteSessionMock.mockResolvedValue(undefined);
        GetBrowserSessionTraceMock.mockResolvedValue(null);
        InvokeBrowserToolMock.mockResolvedValue('{}');
        ClipboardSetTextMock.mockResolvedValue(undefined);
        vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
            cb(0);
            return 0;
        });
        Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {
            configurable: true,
            value: vi.fn(),
        });
    });

    it('renders pending question details and waiting-input placeholder', async () => {
        const session = buildSession();
        const setRemoteInputDrafts = vi.fn();

        render(
            <RemoteSessionConsole
                session={session}
                remoteInputDrafts={{}}
                setRemoteInputDrafts={setRemoteInputDrafts}
                killRemoteSession={vi.fn().mockResolvedValue(undefined)}
                refreshSessionsOnly={vi.fn().mockResolvedValue(undefined)}
                onClose={vi.fn()}
            />,
        );

        expect(screen.getByText('Auth method')).toBeTruthy();
        expect(screen.getByText('Which auth method should we use?')).toBeTruthy();
        expect(screen.getByText('OAuth')).toBeTruthy();
        expect(screen.getByText('API Key')).toBeTruthy();
        expect(screen.getByText('Hint: Pick one option to continue')).toBeTruthy();

        const input = screen.getByPlaceholderText('回答问题以继续...') as HTMLInputElement;
        expect(input.value).toBe('');

        fireEvent.click(screen.getByRole('button', { name: 'OAuth' }));
        expect(setRemoteInputDrafts).toHaveBeenCalledWith(expect.any(Function));
        const updater = setRemoteInputDrafts.mock.calls.at(-1)?.[0] as (prev: Record<string, string>) => Record<string, string>;
        expect(updater({})).toEqual({ 'sess-1': 'OAuth' });
    });

    it('sends selected AskUserQuestion answer through structured input', async () => {
        const session = buildSession();
        const refreshSessionsOnly = vi.fn().mockResolvedValue(undefined);
        const setRemoteInputDrafts = vi.fn();

        render(
            <RemoteSessionConsole
                session={session}
                remoteInputDrafts={{}}
                setRemoteInputDrafts={setRemoteInputDrafts}
                killRemoteSession={vi.fn().mockResolvedValue(undefined)}
                refreshSessionsOnly={refreshSessionsOnly}
                onClose={vi.fn()}
            />,
        );

        fireEvent.change(screen.getByPlaceholderText('回答问题以继续...'), { target: { value: 'API Key' } });
        fireEvent.click(screen.getByTitle('发送'));

        await waitFor(() => {
            expect(SendRemoteSessionInputMock).toHaveBeenCalledWith('sess-1', 'API Key\n');
        });
        await waitFor(() => {
            expect(refreshSessionsOnly).toHaveBeenCalled();
        });
        expect(screen.getByText('✓ "API Key"')).toBeTruthy();
    });

    it('keeps input enabled while session status stays busy after send', async () => {
        const session = buildSession({
            status: 'busy',
            summary: {
                status: 'busy',
                current_task: 'Running TodoWrite',
            },
        });

        render(
            <RemoteSessionConsole
                session={session}
                remoteInputDrafts={{ 'sess-1': 'hi again' }}
                setRemoteInputDrafts={vi.fn()}
                killRemoteSession={vi.fn().mockResolvedValue(undefined)}
                refreshSessionsOnly={vi.fn().mockResolvedValue(undefined)}
                onClose={vi.fn()}
            />,
        );

        const input = screen.getByDisplayValue('hi again') as HTMLInputElement;
        expect(input.disabled).toBe(false);
    });
});

describe('RemoteSessionConsole browser controls', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        SendRemoteSessionInputMock.mockResolvedValue(undefined);
        SendRemoteSessionRawInputMock.mockResolvedValue(undefined);
        SendRemoteSessionImageMock.mockResolvedValue(undefined);
        CaptureRemoteScreenshotMock.mockResolvedValue(undefined);
        CaptureRemoteWindowScreenshotMock.mockResolvedValue(undefined);
        InterruptRemoteSessionMock.mockResolvedValue(undefined);
        GetBrowserSessionTraceMock.mockResolvedValue(null);
        InvokeBrowserToolMock.mockResolvedValue('{}');
        ClipboardSetTextMock.mockResolvedValue(undefined);
        vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
            cb(0);
            return 0;
        });
        Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {
            configurable: true,
            value: vi.fn(),
        });
    });

    it('renders browser wait controls and usage hint', () => {
        const session = buildSession({
            tool: 'browser',
            launch_source: 'browser',
            execution_mode: 'browser-agent',
            status: 'running',
            current_url: 'https://example.com',
            summary: {
                status: 'running',
                suggested_action: 'Run browser_observe to capture refs and page state',
            },
            latest_refs: [{ ref: '@e1', selector: 'button.submit', name: 'Submit' }],
        });

        render(
            <RemoteSessionConsole
                session={session}
                remoteInputDrafts={{}}
                setRemoteInputDrafts={vi.fn()}
                killRemoteSession={vi.fn().mockResolvedValue(undefined)}
                refreshSessionsOnly={vi.fn().mockResolvedValue(undefined)}
                onClose={vi.fn()}
            />,
        );

        expect(screen.getByText('Browser controls')).toBeTruthy();
        expect(screen.getByText(/建议先执行一次 browser_observe/)).toBeTruthy();
        expect(screen.getByRole('button', { name: '等待' })).toBeTruthy();
        expect(screen.getByPlaceholderText('等待毫秒数，例如 1000')).toBeTruthy();
    });

    it('invokes browser_wait with target and duration', async () => {
        InvokeBrowserToolMock.mockResolvedValue('{"display":"已等待 1500ms"}');
        const refreshSessionsOnly = vi.fn().mockResolvedValue(undefined);
        const session = buildSession({
            id: 'browser-1',
            tool: 'browser',
            launch_source: 'browser',
            execution_mode: 'browser-agent',
            status: 'running',
            summary: { status: 'running' },
            latest_refs: [{ ref: '@e1', selector: 'button.submit', name: 'Submit' }],
        });

        render(
            <RemoteSessionConsole
                session={session}
                remoteInputDrafts={{}}
                setRemoteInputDrafts={vi.fn()}
                killRemoteSession={vi.fn().mockResolvedValue(undefined)}
                refreshSessionsOnly={refreshSessionsOnly}
                onClose={vi.fn()}
            />,
        );

        fireEvent.change(screen.getByPlaceholderText('ref 或 selector，例如 @e1 / button.submit'), { target: { value: '@e1' } });
        fireEvent.change(screen.getByPlaceholderText('等待毫秒数，例如 1000'), { target: { value: '1500' } });
        fireEvent.click(screen.getByRole('button', { name: '等待' }));

        await waitFor(() => {
            expect(InvokeBrowserToolMock).toHaveBeenCalledWith('browser_wait', { session_id: 'browser-1', ref: '@e1', duration_ms: 1500 });
        });
        await waitFor(() => {
            expect(refreshSessionsOnly).toHaveBeenCalled();
        });
        expect(screen.getByText('✓ 已等待 1500ms')).toBeTruthy();
    });

    it('shows recent browser target recommendation when action target is empty', () => {
        const session = buildSession({
            id: 'browser-4',
            tool: 'browser',
            launch_source: 'browser',
            execution_mode: 'browser-agent',
            status: 'running',
            summary: { status: 'running' },
            latest_refs: [{ ref: '@recent', selector: 'button.primary', name: 'Primary action' }],
        });

        render(
            <RemoteSessionConsole
                session={session}
                remoteInputDrafts={{}}
                setRemoteInputDrafts={vi.fn()}
                killRemoteSession={vi.fn().mockResolvedValue(undefined)}
                refreshSessionsOnly={vi.fn().mockResolvedValue(undefined)}
                onClose={vi.fn()}
            />,
        );

        expect(screen.getByText(/最近可用目标：/)).toBeTruthy();
        expect(screen.getByText('@recent')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: '使用最近目标' }));
        expect((screen.getByPlaceholderText('ref 或 selector，例如 @e1 / button.submit') as HTMLInputElement).value).toBe('@recent');
    });

    it('shows recent target quick actions and copies extract result', async () => {
        InvokeBrowserToolMock.mockResolvedValueOnce('{"display":"已点击最近目标"}');
        const session = buildSession({
            id: 'browser-5',
            tool: 'browser',
            launch_source: 'browser',
            execution_mode: 'browser-agent',
            status: 'running',
            summary: { status: 'running' },
            latest_refs: [{ ref: '@quick', selector: 'button.quick', name: 'Quick action' }],
        });

        render(
            <RemoteSessionConsole
                session={session}
                remoteInputDrafts={{}}
                setRemoteInputDrafts={vi.fn()}
                killRemoteSession={vi.fn().mockResolvedValue(undefined)}
                refreshSessionsOnly={vi.fn().mockResolvedValue(undefined)}
                onClose={vi.fn()}
            />,
        );

        fireEvent.click(screen.getByRole('button', { name: '点击最近目标' }));
        await waitFor(() => {
            expect(InvokeBrowserToolMock).toHaveBeenCalledWith('browser_click', { session_id: 'browser-5', ref: '@quick' });
        });
    });

    it('copies browser extract result', async () => {
        InvokeBrowserToolMock.mockResolvedValue('{"display":"已提取目标内容","data":{"content":"copy me"}}');
        const session = buildSession({
            id: 'browser-6',
            tool: 'browser',
            launch_source: 'browser',
            execution_mode: 'browser-agent',
            status: 'running',
            summary: { status: 'running' },
        });

        render(
            <RemoteSessionConsole
                session={session}
                remoteInputDrafts={{}}
                setRemoteInputDrafts={vi.fn()}
                killRemoteSession={vi.fn().mockResolvedValue(undefined)}
                refreshSessionsOnly={vi.fn().mockResolvedValue(undefined)}
                onClose={vi.fn()}
            />,
        );

        fireEvent.change(screen.getByPlaceholderText('提取目标，例如：页面主标题、正文摘要'), { target: { value: '正文摘要' } });
        fireEvent.click(screen.getByRole('button', { name: '提取' }));

        await waitFor(() => {
            expect(screen.getByText('copy me')).toBeTruthy();
        });

        fireEvent.click(screen.getByRole('button', { name: '复制结果' }));
        await waitFor(() => {
            expect(ClipboardSetTextMock).toHaveBeenCalledWith('copy me');
        });
    });

    it('extracts recent target and clears extract result', async () => {
        InvokeBrowserToolMock.mockResolvedValue('{"display":"已提取最近目标","data":{"content":"recent content"}}');
        const session = buildSession({
            id: 'browser-7',
            tool: 'browser',
            launch_source: 'browser',
            execution_mode: 'browser-agent',
            status: 'running',
            summary: { status: 'running' },
            latest_refs: [{ ref: '@recentExtract', selector: 'div.content', name: 'Content block' }],
        });

        render(
            <RemoteSessionConsole
                session={session}
                remoteInputDrafts={{}}
                setRemoteInputDrafts={vi.fn()}
                killRemoteSession={vi.fn().mockResolvedValue(undefined)}
                refreshSessionsOnly={vi.fn().mockResolvedValue(undefined)}
                onClose={vi.fn()}
            />,
        );

        fireEvent.click(screen.getByRole('button', { name: '提取最近目标' }));
        await waitFor(() => {
            expect(InvokeBrowserToolMock).toHaveBeenCalledWith('browser_extract', {
                session_id: 'browser-7',
                query: '最近目标内容',
                format: 'text',
                ref: '@recentExtract',
            });
        });
        await waitFor(() => {
            expect(screen.getByText('recent content')).toBeTruthy();
        });

        fireEvent.click(screen.getByRole('button', { name: '清空结果' }));
        expect(screen.queryByText('recent content')).toBeNull();
    });

    it('shows suggested action and runs wait from ref shortcuts', async () => {
        InvokeBrowserToolMock.mockResolvedValue('{"display":"已等待元素出现"}');
        const refreshSessionsOnly = vi.fn().mockResolvedValue(undefined);
        const session = buildSession({
            id: 'browser-3',
            tool: 'browser',
            launch_source: 'browser',
            execution_mode: 'browser-agent',
            status: 'running',
            summary: {
                status: 'running',
                suggested_action: 'Run browser_observe to capture refs and page state',
            },
            latest_refs: [{ ref: '@e1', selector: 'button.submit', name: 'Submit' }],
            preview: { preview_lines: ['preview'] },
        });

        render(
            <RemoteSessionConsole
                session={session}
                remoteInputDrafts={{}}
                setRemoteInputDrafts={vi.fn()}
                killRemoteSession={vi.fn().mockResolvedValue(undefined)}
                refreshSessionsOnly={refreshSessionsOnly}
                onClose={vi.fn()}
            />,
        );

        expect(screen.getByText(/建议操作：Run browser_observe to capture refs and page state/)).toBeTruthy();

        fireEvent.click(screen.getByRole('button', { name: '预览' }));
        fireEvent.click(screen.getByRole('button', { name: '等待 @e1' }));

        await waitFor(() => {
            expect(InvokeBrowserToolMock).toHaveBeenCalledWith('browser_wait', { session_id: 'browser-3', ref: '@e1', duration_ms: 1000 });
        });
        await waitFor(() => {
            expect(refreshSessionsOnly).toHaveBeenCalled();
        });
    });
});
