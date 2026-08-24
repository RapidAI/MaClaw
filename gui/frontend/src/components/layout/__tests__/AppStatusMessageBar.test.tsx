// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { AppStatusMessageBar } from '../AppStatusMessageBar';

const noop = vi.fn();

afterEach(() => {
    cleanup();
});

describe('AppStatusMessageBar', () => {
    it('renders nothing when idle so the main shell has no blank bottom strip', () => {
        const { container } = render(
            <AppStatusMessageBar
                status=""
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
            />,
        );
        expect(container.firstChild).toBeNull();
        expect(screen.queryByTestId('app-status-message-bar')).toBeNull();
    });

    it('does not pin coding-agent tool status on the bottom status bar', () => {
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
            />,
        );

        expect(screen.queryByTestId('app-status-coding-agent')).toBeNull();
        expect(screen.getByTestId('app-status-message-bar').textContent).toContain('Ready');
    });

    it('supports an inline variant for the AI quick-settings row', () => {
        render(
            <AppStatusMessageBar
                variant="inline"
                status="Saved"
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
            />,
        );
        const bar = screen.getByTestId('app-status-message-bar');
        expect(bar.getAttribute('data-variant')).toBe('inline');
        expect(bar.className).toContain('status-message--inline');
        expect(bar.textContent).toContain('Saved');
    });

    it('does not keep the bottom bar open for coding-agent progress alone', () => {
        const { container } = render(
            <AppStatusMessageBar
                status=""
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
            />,
        );
        expect(container.firstChild).toBeNull();
        expect(screen.queryByTestId('app-status-coding-agent')).toBeNull();
    });
});
