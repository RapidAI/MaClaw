// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type React from 'react';
import { SidebarNavRail } from '../SidebarNavRail';

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

    it('switches to AI before opening a favorite digital employee conversation', () => {
        const props = renderRail({ showAppEntry: true });

        fireEvent.click(screen.getByTestId('fav-ve-ve-1'));

        expect(props.switchTool).toHaveBeenCalledWith('ai');
        expect(props.onStartVEConversation).toHaveBeenCalledWith('ve-1');
    });
});
