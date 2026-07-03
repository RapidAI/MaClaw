// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type React from 'react';

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetHubUserRanking: vi.fn().mockResolvedValue({ error: 'hub not configured' }),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    BrowserOpenURL: vi.fn(),
    EventsOn: vi.fn().mockReturnValue(() => {}),
}));

import { SidebarNavRail } from '../SidebarNavRail';
import { BrowserOpenURL } from '../../../../wailsjs/runtime';

beforeEach(() => {
    vi.mocked(BrowserOpenURL).mockClear();
});

function renderRail(overrides: Partial<React.ComponentProps<typeof SidebarNavRail>> = {}) {
    const props: React.ComponentProps<typeof SidebarNavRail> = {
        navTab: 'settings',
        brandInfo: null,
        currentIcon: 'logo.png',
        brandSidebarName: 'MaClaw',
        switchTool: vi.fn(),
        lang: 'en',
        runningTaskCount: 0,
        t: (key) => key,
        gossipAllowed: false,
        config: {},
        favoriteEmployees: [{ veId: 've-1', name: 'Researcher', online: true }],
        veAuthorized: true,
        onStartVEConversation: vi.fn(),
        onReorderFavorites: vi.fn(),
        ...overrides,
    };
    render(<SidebarNavRail {...props} />);
    return props;
}

describe('SidebarNavRail favorite employees', () => {
    it('hides the apps entry by default', () => {
        renderRail();

        expect(screen.queryByTitle('Apps')).toBeNull();
    });

    it('hides the apps entry when disabled', () => {
        renderRail({ showAppEntry: false });

        expect(screen.queryByTitle('Apps')).toBeNull();
        expect(screen.getByTestId('fav-ve-ve-1')).toBeTruthy();
    });

    it('shows the apps entry when enabled', () => {
        renderRail({ showAppEntry: true });

        expect(screen.getByTitle('Apps')).toBeTruthy();
    });

    it('shows a pending ranking mark for registered users without a ranking yet', () => {
        renderRail({ remoteActivationStatus: { activated: true }, config: { remote_hub_url: 'https://hub.example/' } });

        fireEvent.click(screen.getByTitle('Monthly ranking pending'));

        expect(BrowserOpenURL).toHaveBeenCalledWith('https://hub.example/user-ranking');
        expect(screen.getByText('Rank')).toBeTruthy();
    });

    it('opens the hub ranking page scoped to the configured tenant', () => {
        renderRail({ remoteActivationStatus: { activated: true }, config: { remote_hub_url: 'https://hub.example/', remote_tenant_id: 'tenant acme' } });

        fireEvent.click(screen.getByTitle('Monthly ranking pending'));

        expect(BrowserOpenURL).toHaveBeenCalledWith('https://hub.example/user-ranking?tenant_id=tenant+acme');
    });

    it('does not throw or open a ranking page for an invalid hub URL', () => {
        renderRail({ remoteActivationStatus: { activated: true }, config: { remote_hub_url: 'not a url', remote_tenant_id: 'tenant acme' } });

        expect(() => fireEvent.click(screen.getByTitle('Monthly ranking pending'))).not.toThrow();

        expect(BrowserOpenURL).not.toHaveBeenCalledWith(expect.stringContaining('/user-ranking'));
    });

    it('switches to AI before opening a favorite digital employee conversation', () => {
        const props = renderRail({ showAppEntry: true });

        fireEvent.click(screen.getByTestId('fav-ve-ve-1'));

        expect(props.switchTool).toHaveBeenCalledWith('ai');
        expect(props.onStartVEConversation).toHaveBeenCalledWith('ve-1');
    });
});
