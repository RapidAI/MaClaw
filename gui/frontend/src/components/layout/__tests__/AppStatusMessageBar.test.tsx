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
        expect(status.textContent).toContain('Coding');
        expect(status.textContent).toContain('Running');
        expect(status.textContent).toContain('T2');
        expect(status.textContent).toContain('Fix stale edit guard');
        expect(status.getAttribute('role')).toBe('status');
        expect(status.getAttribute('aria-live')).toBe('polite');
        expect(status.getAttribute('aria-label')).toContain('Fix stale edit guard');
        expect(status.getAttribute('aria-label')).toMatch(/Coding\s*\u00b7\s*Running/);
    });

    it('uses soft amber (not red) for failed coding-agent status; dark mode brightens', () => {
        const { rerender } = render(
            <AppStatusMessageBar
                status="Ready"
                lang="zh-Hans"
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
                codingAgentProgress={{ phase: 'failed', taskID: 'T1', title: 'Compile check' }}
            />,
        );
        let status = screen.getByTestId('app-status-coding-agent');
        expect(status.style.color).toBe('rgb(161, 98, 7)');
        expect(status.style.color).not.toBe('rgb(196, 61, 52)');
        expect(status.getAttribute('data-tone-accent')).toBe('#a16207');

        rerender(
            <AppStatusMessageBar
                status="Ready"
                lang="zh-Hans"
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
                codingAgentProgress={{ phase: 'failed', taskID: 'T1', title: 'Compile check' }}
                isDark
            />,
        );
        status = screen.getByTestId('app-status-coding-agent');
        expect(status.style.color).toBe('rgb(224, 178, 83)');
        expect(status.getAttribute('data-tone-accent')).toBe('#e0b253');
    });
});
