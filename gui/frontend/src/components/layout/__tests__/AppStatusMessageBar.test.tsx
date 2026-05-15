// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { AppStatusMessageBar } from '../AppStatusMessageBar';

const noop = vi.fn();

describe('AppStatusMessageBar', () => {
    it('shows active coding agent task status in the bottom status bar', () => {
        render(
            <AppStatusMessageBar
                status="Ready"
                lang="en"
                config={{}}
                qqBotStatus=""
                telegramStatus=""
                weixinStatus=""
                lansengerStatus=""
                maclawLLMOnline
                maclawLLMConfigured
                remoteActivated
                navTab="ai"
                settingsTab="llm"
                backgroundInstallStatus=""
                lobsterOffline="/offline.png"
                lobsterHalf="/half.png"
                onOpenIMSettings={noop}
                onOpenLLMSettings={noop}
                codingAgentProgress={{ phase: 'running', taskID: 'T2', title: 'Fix stale edit guard' }}
            />,
        );

        const status = screen.getByTestId('app-status-coding-agent');
        expect(status.textContent).toContain('Coding Agent');
        expect(status.textContent).toContain('Running');
        expect(status.textContent).toContain('T2');
        expect(status.textContent).toContain('Fix stale edit guard');
        expect(status.getAttribute('role')).toBe('status');
        expect(status.getAttribute('aria-live')).toBe('polite');
        expect(status.getAttribute('aria-label')).toContain('Fix stale edit guard');
    });
});
