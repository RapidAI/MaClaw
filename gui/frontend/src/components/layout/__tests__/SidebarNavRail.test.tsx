// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type React from 'react';

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetHubUserRanking: vi.fn().mockResolvedValue({ error: 'hub not configured' }),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    BrowserOpenURL: vi.fn(),
    EventsOn: vi.fn().mockReturnValue(() => {}),
}));

import { SidebarNavRail } from '../SidebarNavRail';
import { GetHubUserRanking } from '../../../../wailsjs/go/main/App';
import { BrowserOpenURL, EventsOn } from '../../../../wailsjs/runtime';
import { miniAppLabels } from '../../../i18n/maclawMiniAppLabels';

beforeEach(() => {
    vi.mocked(BrowserOpenURL).mockClear();
    vi.mocked(EventsOn).mockClear();
    vi.mocked(EventsOn).mockReturnValue(() => {});
    vi.mocked(GetHubUserRanking).mockReset();
    vi.mocked(GetHubUserRanking).mockResolvedValue({ error: 'hub not configured' });
});

afterEach(() => {
    vi.useRealTimers();
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

        expect(screen.queryByTitle(miniAppLabels.short.en)).toBeNull();
    });

    it('hides the apps entry when disabled', () => {
        renderRail({ showAppEntry: false });

        expect(screen.queryByTitle(miniAppLabels.short.en)).toBeNull();
        expect(screen.getByTestId('fav-ve-ve-1')).toBeTruthy();
    });

    it('shows the apps entry when enabled', () => {
        renderRail({ showAppEntry: true });

        expect(screen.getByTitle(miniAppLabels.short.en)).toBeTruthy();
    });

    it('renders AI assistant icon badge markup for theme contrast tokens', () => {
        renderRail({ navTab: 'settings' });

        const aiEntry = screen.getByTitle('AI Asst');
        const iconBadge = aiEntry.querySelector('.ai-nav-icon-badge');
        const icon = aiEntry.querySelector('.ai-nav-icon');

        expect(iconBadge).toBeTruthy();
        expect(icon).toBeTruthy();
        // Glyph inherits badge color via currentColor; dark theme sets --ai-icon-inactive-fg.
        expect(icon?.getAttribute('stroke')).toBe('currentColor');
        expect(icon?.getAttribute('stroke-width')).toBe('2');
        expect(document.querySelector('.left-nav-item--ai.active')).toBeNull();
    });

    it('marks AI nav active for selected high-contrast badge state', () => {
        renderRail({ navTab: 'ai' });

        const aiEntry = screen.getByTitle('AI Asst');
        expect(aiEntry.classList.contains('active')).toBe(true);
        expect(aiEntry.querySelector('.ai-nav-icon-badge')).toBeTruthy();
        expect(aiEntry.querySelector('.ai-nav-icon')?.getAttribute('stroke')).toBe('currentColor');
    });

    it('shows a pending ranking mark for registered users without a ranking yet', () => {
        renderRail({ remoteActivationStatus: { activated: true }, config: { remote_hub_url: 'https://hub.example/' } });

        fireEvent.click(screen.getByTitle('Monthly ranking pending'));

        expect(BrowserOpenURL).toHaveBeenCalledWith('https://hub.example/user-ranking');
        // Icon title and visible label both say "Rank".
        expect(screen.getAllByText('Rank').length).toBeGreaterThan(0);
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

    it('shows returned monthly ranking data even when both ranks are not positive yet', async () => {
        vi.mocked(GetHubUserRanking).mockResolvedValueOnce({
            total_tokens: 0,
            duration_seconds: 0,
            token_rank: 0,
            duration_rank: 0,
            total_users: 1,
            period: 'monthly',
        });

        renderRail({ remoteActivationStatus: { activated: true }, config: { remote_hub_url: 'https://hub.example/' } });

        await waitFor(() => expect(screen.getByTitle('This month: Token #-/1, Online #-/1')).toBeTruthy());

        expect(screen.queryByTitle('Monthly ranking pending')).toBeNull();
        // Icon title and visible label both say "Rank".
        expect(screen.getAllByText('Rank').length).toBeGreaterThan(0);
    });

    it('retries startup ranking fetch with exponential backoff until it gets valid data', async () => {
        vi.useFakeTimers();
        vi.mocked(GetHubUserRanking)
            .mockResolvedValueOnce({ error: 'ranking pending' })
            .mockResolvedValueOnce({
                total_tokens: 120,
                duration_seconds: 0,
                token_rank: 4,
                duration_rank: 0,
                total_users: 9,
                period: 'monthly',
            });

        renderRail({ remoteActivationStatus: { activated: true }, config: { remote_hub_url: 'https://hub.example/' } });

        await act(async () => {
            await Promise.resolve();
        });

        expect(GetHubUserRanking).toHaveBeenCalledTimes(1);
        expect(screen.getByTitle('Monthly ranking pending')).toBeTruthy();

        await act(async () => {
            await vi.advanceTimersByTimeAsync(30_000);
            await Promise.resolve();
        });

        expect(screen.getByTitle('This month: Token #4/9, Online #-/9')).toBeTruthy();
        expect(screen.queryByTitle('Monthly ranking pending')).toBeNull();
        expect(GetHubUserRanking).toHaveBeenCalledTimes(2);

        await act(async () => {
            await vi.advanceTimersByTimeAsync(2 * 60_000);
            await Promise.resolve();
        });

        expect(GetHubUserRanking).toHaveBeenCalledTimes(2);
    });

    it('stops the startup retry chain when another refresh gets valid data first', async () => {
        vi.useFakeTimers();
        vi.mocked(GetHubUserRanking)
            .mockResolvedValueOnce({ error: 'ranking pending' })
            .mockResolvedValueOnce({
                total_tokens: 120,
                duration_seconds: 0,
                token_rank: 4,
                duration_rank: 0,
                total_users: 9,
                period: 'monthly',
            });

        renderRail({ remoteActivationStatus: { activated: true }, config: { remote_hub_url: 'https://hub.example/' } });

        await act(async () => {
            await Promise.resolve();
        });

        const tokenUsageHandler = vi.mocked(EventsOn).mock.calls.find(([eventName]) => eventName === 'llm-token-usage-changed')?.[1] as (() => void) | undefined;
        expect(tokenUsageHandler).toBeTruthy();
        tokenUsageHandler?.();

        await act(async () => {
            await vi.advanceTimersByTimeAsync(5_000);
            await Promise.resolve();
        });

        expect(screen.getByTitle('This month: Token #4/9, Online #-/9')).toBeTruthy();
        expect(GetHubUserRanking).toHaveBeenCalledTimes(2);

        await act(async () => {
            await vi.advanceTimersByTimeAsync(25_000);
            await Promise.resolve();
        });

        expect(GetHubUserRanking).toHaveBeenCalledTimes(2);
    });

    it('continues startup retries when a stale startup response was superseded by a failed refresh', async () => {
        vi.useFakeTimers();
        let resolveStartup: (value: unknown) => void = () => {};
        const startupResponse = new Promise((resolve) => { resolveStartup = resolve; });
        vi.mocked(GetHubUserRanking)
            .mockReturnValueOnce(startupResponse)
            .mockResolvedValueOnce({ error: 'ranking pending' })
            .mockResolvedValueOnce({
                total_tokens: 120,
                duration_seconds: 0,
                token_rank: 4,
                duration_rank: 0,
                total_users: 9,
                period: 'monthly',
            });

        renderRail({ remoteActivationStatus: { activated: true }, config: { remote_hub_url: 'https://hub.example/' } });

        await act(async () => {
            await Promise.resolve();
        });

        const tokenUsageHandler = vi.mocked(EventsOn).mock.calls.find(([eventName]) => eventName === 'llm-token-usage-changed')?.[1] as (() => void) | undefined;
        tokenUsageHandler?.();

        await act(async () => {
            await vi.advanceTimersByTimeAsync(5_000);
            await Promise.resolve();
        });

        expect(GetHubUserRanking).toHaveBeenCalledTimes(2);

        await act(async () => {
            resolveStartup({ error: 'ranking pending' });
            await Promise.resolve();
        });

        await act(async () => {
            await vi.advanceTimersByTimeAsync(30_000);
            await Promise.resolve();
        });

        expect(screen.getByTitle('This month: Token #4/9, Online #-/9')).toBeTruthy();
        expect(GetHubUserRanking).toHaveBeenCalledTimes(3);
    });

    it('falls back to the 30 minute periodic refresh after startup retries are exhausted', async () => {
        vi.useFakeTimers();
        vi.mocked(GetHubUserRanking)
            .mockResolvedValueOnce({ error: 'ranking pending' })
            .mockResolvedValueOnce({ error: 'ranking pending' })
            .mockResolvedValueOnce({ error: 'ranking pending' })
            .mockResolvedValueOnce({ error: 'ranking pending' })
            .mockResolvedValueOnce({
                total_tokens: 120,
                duration_seconds: 0,
                token_rank: 4,
                duration_rank: 0,
                total_users: 9,
                period: 'monthly',
            });

        renderRail({ remoteActivationStatus: { activated: true }, config: { remote_hub_url: 'https://hub.example/' } });

        await act(async () => {
            await Promise.resolve();
        });

        expect(GetHubUserRanking).toHaveBeenCalledTimes(1);

        await act(async () => {
            await vi.advanceTimersByTimeAsync(30_000 + 2 * 60_000 + 8 * 60_000);
            await Promise.resolve();
        });

        expect(GetHubUserRanking).toHaveBeenCalledTimes(4);
        expect(screen.getByTitle('Monthly ranking pending')).toBeTruthy();

        await act(async () => {
            await vi.advanceTimersByTimeAsync(30 * 60_000 - (30_000 + 2 * 60_000 + 8 * 60_000) - 1);
            await Promise.resolve();
        });

        expect(GetHubUserRanking).toHaveBeenCalledTimes(4);

        await act(async () => {
            await vi.advanceTimersByTimeAsync(1);
            await Promise.resolve();
        });

        expect(screen.getByTitle('This month: Token #4/9, Online #-/9')).toBeTruthy();
        expect(screen.queryByTitle('Monthly ranking pending')).toBeNull();
    });

    it('switches to AI before opening a favorite digital employee conversation', () => {
        const props = renderRail({ showAppEntry: true });

        fireEvent.click(screen.getByTestId('fav-ve-ve-1'));

        expect(props.switchTool).toHaveBeenCalledWith('ai');
        expect(props.onStartVEConversation).toHaveBeenCalledWith('ve-1');
    });
});
