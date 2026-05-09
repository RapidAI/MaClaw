// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { AppStatusMessageBar } from '../AppStatusMessageBar';

const noop = vi.fn();

function renderBar(config: Record<string, unknown>, hideAgentNet = false, showLansenger = false) {
    render(
        <AppStatusMessageBar
            status="Ready"
            lang="en"
            config={config}
            qqBotStatus=""
            telegramStatus=""
            weixinStatus=""
            lansengerStatus=""
            maclawLLMOnline
            maclawLLMConfigured
            remoteActivated
            agentNetRunning={false}
            hideAgentNet={hideAgentNet}
            showLansenger={showLansenger}
            navTab="ai"
            settingsTab="llm"
            backgroundInstallStatus=""
            lobsterOffline="/offline.png"
            lobsterHalf="/half.png"
            onOpenIMSettings={noop}
            onOpenLLMSettings={noop}
        />,
    );
}

describe('AppStatusMessageBar AgentNet warning', () => {
    it('does not require AgentNet when it is disabled in config', () => {
        renderBar({ agentnet_enabled: false });

        expect(screen.queryByText('AgentNet not connected')).toBeNull();
    });

    it('warns when AgentNet is enabled but not running', () => {
        renderBar({ agentnet_enabled: true });

        expect(screen.getByText('AgentNet not connected')).toBeTruthy();
    });

    it('does not require AgentNet when the brand hides it', () => {
        renderBar({ agentnet_enabled: true }, true);

        expect(screen.queryByText('AgentNet not connected')).toBeNull();
    });

    it('ignores Lansenger config when Lansenger is hidden for the brand', () => {
        renderBar({ agentnet_enabled: false, lansenger_enabled: true }, false, false);

        expect(screen.queryByText('IM not connected')).toBeNull();
    });

    it('warns about Lansenger only when Lansenger is visible for the brand', () => {
        renderBar({ agentnet_enabled: false, lansenger_enabled: true }, false, true);

        expect(screen.getByText('IM not connected')).toBeTruthy();
    });

    it('shows active coding agent task status in the bottom status bar', () => {
        render(
            <AppStatusMessageBar
                status="Ready"
                lang="en"
                config={{ agentnet_enabled: false }}
                qqBotStatus=""
                telegramStatus=""
                weixinStatus=""
                lansengerStatus=""
                maclawLLMOnline
                maclawLLMConfigured
                remoteActivated
                agentNetRunning={false}
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
