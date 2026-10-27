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
const GetRemoteSessionMock = vi.fn();
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
    GetRemoteSession: (...args: unknown[]) => GetRemoteSessionMock(...args),
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
        GetRemoteSessionMock.mockResolvedValue(null);
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

        const input = screen.getByPlaceholderText('Type a message...') as HTMLInputElement;
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

        fireEvent.change(screen.getByPlaceholderText('Type a message...'), { target: { value: 'API Key' } });
        fireEvent.click(screen.getByTitle('Send'));

        await waitFor(() => {
            expect(SendRemoteSessionInputMock).toHaveBeenCalledWith('sess-1', 'API Key\n');
        });
        await waitFor(() => {
            expect(refreshSessionsOnly).toHaveBeenCalled();
        });
        // SVG check + message body (no emoji; "OK " prefix stripped for display)
        expect(screen.getByText('"API Key"')).toBeTruthy();
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

    it('loads complete output on demand and refreshes it with the preview sequence', async () => {
        const summary = buildSession({
            preview: { preview_lines: ['short preview'], output_seq: 1, updated_at: 10 },
            raw_output_lines: undefined,
            output_images: undefined,
        });
        GetRemoteSessionMock.mockResolvedValueOnce(buildSession({
            preview: summary.preview,
            raw_output_lines: ['first full line'],
            output_images: [{ image_id: 'image-1', media_type: 'image/png', data: 'cG5n', after_line_idx: 0 }],
        })).mockResolvedValueOnce(buildSession({
            preview: { preview_lines: ['short preview'], output_seq: 2, updated_at: 20 },
            raw_output_lines: ['first full line', 'second full line'],
        }));
        const props = {
            remoteInputDrafts: {},
            setRemoteInputDrafts: vi.fn(),
            killRemoteSession: vi.fn().mockResolvedValue(undefined),
            refreshSessionsOnly: vi.fn().mockResolvedValue(undefined),
            onClose: vi.fn(),
        };
        const { rerender } = render(<RemoteSessionConsole {...props} session={summary} />);

        expect(await screen.findByText('first full line')).toBeTruthy();
        expect(screen.getByRole('img').getAttribute('src')).toBe('data:image/png;base64,cG5n');
        expect(GetRemoteSessionMock).toHaveBeenCalledWith('sess-1');

        rerender(<RemoteSessionConsole
            {...props}
            session={{ ...summary, preview: { preview_lines: ['short preview'], output_seq: 2, updated_at: 20 } }}
        />);
        expect(await screen.findByText('first full line second full line')).toBeTruthy();
        expect(GetRemoteSessionMock).toHaveBeenCalledTimes(2);
    });
});

describe('RemoteSessionConsole browser sessions', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        SendRemoteSessionInputMock.mockResolvedValue(undefined);
        SendRemoteSessionRawInputMock.mockResolvedValue(undefined);
        SendRemoteSessionImageMock.mockResolvedValue(undefined);
        CaptureRemoteScreenshotMock.mockResolvedValue(undefined);
        CaptureRemoteWindowScreenshotMock.mockResolvedValue(undefined);
        InterruptRemoteSessionMock.mockResolvedValue(undefined);
        GetBrowserSessionTraceMock.mockResolvedValue(null);
        GetRemoteSessionMock.mockResolvedValue(null);
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

    it('renders browser sessions as raw terminal sessions with command input', () => {
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

        expect(screen.getByText(/browser \(browser-agent\)/)).toBeTruthy();
        expect(screen.getByText('assistant: waiting for answer')).toBeTruthy();
        expect(screen.getByPlaceholderText('Type a command...')).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Raw' })).toBeTruthy();
    });

    it('sends browser session input through raw terminal input', async () => {
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

        fireEvent.change(screen.getByPlaceholderText('Type a command...'), { target: { value: 'go' } });
        fireEvent.click(screen.getByRole('button', { name: 'Raw' }));

        await waitFor(() => {
            expect(SendRemoteSessionRawInputMock).toHaveBeenCalledWith('browser-1', 'g');
            expect(SendRemoteSessionRawInputMock).toHaveBeenCalledWith('browser-1', '\r');
        });
        await waitFor(() => {
            expect(refreshSessionsOnly).toHaveBeenCalled();
        });
    });
});
