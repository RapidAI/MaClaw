// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { SidebarNavRail } from '../SidebarNavRail';

const baseProps = {
    navTab: 'ai',
    currentIcon: '/icon.png',
    brandSidebarName: 'TigerClaw',
    switchTool: vi.fn(),
    lang: 'en',
    maclawLLMOnline: true,
    agentNetRunning: false,
    remoteActivationStatus: { activated: true },
    runningTaskCount: 0,
    t: (key: string) => key,
    gossipAllowed: true,
    config: {},
    sidebarExpanded: false,
    setSidebarExpanded: vi.fn(),
};

describe('SidebarNavRail AgentNet visibility', () => {
    it('hides AgentNet for TigerClaw', () => {
        render(<SidebarNavRail {...baseProps} brandInfo={{ id: 'qianxin' }} />);
        fireEvent.click(screen.getByTitle('System'));

        expect(screen.queryByText('AgentNet')).toBeNull();
    });

    it('shows AgentNet for the default brand', () => {
        render(<SidebarNavRail {...baseProps} brandInfo={{ id: 'maclaw' }} />);
        fireEvent.click(screen.getByTitle('System'));

        expect(screen.getByText('AgentNet')).toBeTruthy();
    });
});
