// @vitest-environment jsdom
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type React from 'react';

vi.mock('../../../../wailsjs/go/main/App', () => ({
    DesktopBotAccess: vi.fn().mockResolvedValue({ enabled: false }),
    GetHubUserInvitationStatus: vi.fn().mockResolvedValue({ enabled: false }),
    GetHubUserInvitationsPage: vi.fn().mockResolvedValue(null),
    RotateHubUserInvitation: vi.fn().mockResolvedValue(null),
    GetHubUserRanking: vi.fn().mockResolvedValue({ error: 'hub not configured' }),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    BrowserOpenURL: vi.fn(),
    EventsOn: vi.fn().mockReturnValue(() => {}),
}));

import { SidebarNavRail } from '../SidebarNavRail';
import { publishBotAccess } from '../../bots/botOpenGate';
import { DesktopBotAccess, GetHubUserInvitationStatus, GetHubUserInvitationsPage, GetHubUserRanking, RotateHubUserInvitation } from '../../../../wailsjs/go/main/App';
import { BrowserOpenURL } from '../../../../wailsjs/runtime';
import { miniAppLabels } from '../../../i18n/maclawMiniAppLabels';
import { OPEN_SETTINGS_EVENT } from '../../../utils/settingsNavigation';

const invitationStatus = (enabled: boolean) => ({ enabled } as Awaited<ReturnType<typeof GetHubUserInvitationStatus>>);

const rankingResult = (overrides: { token_rank?: number; duration_rank?: number; total_users?: number; error?: string } = {}) => ({
    total_tokens: 0,
    duration_seconds: 0,
    token_rank: 0,
    duration_rank: 0,
    total_users: 0,
    period: 'monthly',
    ...overrides,
});

beforeEach(() => {
    vi.mocked(BrowserOpenURL).mockClear();
    vi.mocked(DesktopBotAccess).mockReset();
    vi.mocked(DesktopBotAccess).mockResolvedValue({ enabled: false });
    vi.mocked(GetHubUserInvitationStatus).mockReset();
    vi.mocked(GetHubUserInvitationStatus).mockResolvedValue(invitationStatus(false));
    vi.mocked(GetHubUserInvitationsPage).mockReset();
    vi.mocked(GetHubUserInvitationsPage).mockResolvedValue({ enabled: true } as Awaited<ReturnType<typeof GetHubUserInvitationsPage>>);
    vi.mocked(RotateHubUserInvitation).mockReset();
    vi.mocked(RotateHubUserInvitation).mockResolvedValue(null as unknown as Awaited<ReturnType<typeof RotateHubUserInvitation>>);
    vi.mocked(GetHubUserRanking).mockReset();
    vi.mocked(GetHubUserRanking).mockResolvedValue(rankingResult({ error: 'hub not configured' }));
});

afterEach(() => {
    vi.useRealTimers();
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
});

function renderRail(overrides: Partial<React.ComponentProps<typeof SidebarNavRail>> = {}) {
    const props: React.ComponentProps<typeof SidebarNavRail> = {
        navTab: 'settings',
        brandInfo: null,
        currentIcon: 'logo.png',
        brandSidebarName: 'MaClaw',
        switchTool: vi.fn(),
        lang: 'en',
        t: (key) => key,
        gossipAllowed: false,
        config: {},
        favoriteEmployees: [{ veId: 've-1', name: 'Researcher', online: true }],
        veAuthorized: true,
        onStartVEConversation: vi.fn(),
        onReorderFavorites: vi.fn(),
        ...overrides,
    };
    const view = render(<SidebarNavRail {...props} />);
    return Object.assign(props, { unmount: view.unmount });
}


describe('SidebarNavRail system popup', () => {
    it('opens About first from the system menu without duplicating settings or the task monitor', () => {
        const props = renderRail({ lang: 'zh-Hans' });

        fireEvent.click(screen.getByTitle('系统菜单'));

        expect(screen.queryByTestId('system-menu-settings')).toBeNull();
        expect(screen.getByTestId('system-menu-about')).toBeTruthy();
        expect(screen.getAllByRole('menuitem')[0]).toBe(screen.getByTestId('system-menu-about'));
        expect(screen.queryByTestId('system-menu-remote')).toBeNull();

        fireEvent.click(screen.getByTestId('system-menu-about'));
        expect(props.switchTool).toHaveBeenCalledWith('about');
    });

    it('allows keyboard activation of About from the system menu', () => {
        const props = renderRail({ lang: 'zh-Hans' });

        fireEvent.click(screen.getByTitle('系统菜单'));
        const about = screen.getByTestId('system-menu-about');
        expect(about.tagName).toBe('BUTTON');
        fireEvent.keyDown(about, { key: 'Enter' });

        expect(props.switchTool).toHaveBeenCalledWith('about');
        expect(screen.queryByTestId('system-popup-menu')).toBeNull();
    });

    it('opens the system menu from the focused rail trigger', () => {
        renderRail({ lang: 'zh-Hans' });
        const trigger = screen.getByTestId('system-menu-trigger');
        expect(trigger.getAttribute('role')).toBe('button');
        fireEvent.keyDown(trigger, { key: 'Enter' });
        expect(screen.getByTestId('system-popup-menu')).toBeTruthy();
        expect(trigger.getAttribute('aria-expanded')).toBe('true');
    });

    it('closes the system menu when the same rail trigger is clicked again', () => {
        renderRail({ lang: 'zh-Hans' });
        const trigger = screen.getByTestId('system-menu-trigger');

        fireEvent.click(trigger);
        expect(screen.getByTestId('system-popup-menu')).toBeTruthy();
        // Reproduce the browser event order: document mousedown precedes the
        // trigger click. The outside handler must leave the trigger click in
        // charge of toggling the menu closed.
        fireEvent.mouseDown(trigger);
        fireEvent.click(trigger);

        expect(screen.queryByTestId('system-popup-menu')).toBeNull();
        expect(trigger.getAttribute('aria-expanded')).toBe('false');
    });

    it('returns focus to the system trigger when Escape closes the menu', async () => {
        renderRail({ lang: 'zh-Hans' });
        const trigger = screen.getByTestId('system-menu-trigger');

        fireEvent.click(trigger);
        const firstItem = screen.getByTestId('system-menu-about');
        fireEvent.keyDown(firstItem, { key: 'Escape' });
        await new Promise(resolve => window.setTimeout(resolve, 40));

        expect(screen.queryByTestId('system-popup-menu')).toBeNull();
        expect(document.activeElement).toBe(trigger);
    });

    it('uses the system gear mark for the system-menu trigger', () => {
        renderRail();

        const systemTrigger = screen.getByRole('button', { name: 'System menu' });
        const systemMark = systemTrigger.querySelector('.mc-profile-rail__avatar--system');

        expect(systemMark).toBeTruthy();
        expect(systemMark?.querySelector('svg')).toBeTruthy();
        expect(systemMark?.querySelector('svg circle')?.getAttribute('cx')).toBe('12');
        expect(systemMark?.querySelector('svg path')?.getAttribute('d')).toContain('M19.4 15');
        expect(systemTrigger.querySelector('img')).toBeNull();
    });

    it('marks the system menu trigger current on the about and gossip pages', () => {
        const about = renderRail({ navTab: 'about' });
        expect(screen.getByTestId('system-menu-trigger').getAttribute('aria-current')).toBe('page');
        about.unmount();

        const gossip = renderRail({ navTab: 'gossip', gossipAllowed: true });
        expect(screen.getByTestId('system-menu-trigger').getAttribute('aria-current')).toBe('page');
        gossip.unmount();

        const gossipHidden = renderRail({ navTab: 'gossip', gossipAllowed: false });
        expect(screen.getByTestId('system-menu-trigger').getAttribute('aria-current')).toBeNull();
        gossipHidden.unmount();

        renderRail({ navTab: 'skills' });
        expect(screen.getByTestId('system-menu-trigger').getAttribute('aria-current')).toBeNull();
    });

    it('keeps the MaClaw mark out of the rail after moving it to the main header', () => {
        renderRail();

        expect(screen.queryByTestId('sidebar-brand-mark')).toBeNull();
    });

    it('opens the task monitor from the tasks rail', () => {
        const props = renderRail();

        fireEvent.click(screen.getByTestId('sidebar-task-monitor-nav'));

        expect(props.switchTool).toHaveBeenCalledWith('remote');
    });

    it('keeps the tasks rail active while the task monitor is open', () => {
        renderRail({ navTab: 'remote' });
        expect(screen.getByTestId('sidebar-task-monitor-nav').className).toContain('active');
    });

    it('renders no running-task badge when nothing is running', () => {
        renderRail({ runningTaskCount: 0 });

        expect(screen.queryByTestId('sidebar-task-monitor-nav-badge')).toBeNull();
        expect(screen.getByTestId('sidebar-task-monitor-nav').getAttribute('aria-label')).toBe('Tasks');
    });

    it('mirrors the live running-task count on the tasks rail', () => {
        renderRail({ runningTaskCount: 3 });

        expect(screen.getByTestId('sidebar-task-monitor-nav-badge').textContent).toBe('3');
        // The badge must not be visual-only: the accessible name carries the count.
        expect(screen.getByTestId('sidebar-task-monitor-nav').getAttribute('aria-label')).toBe('Tasks: 3 running');
    });

    it('caps the running-task badge so a long count cannot break the rail', () => {
        renderRail({ runningTaskCount: 250 });

        expect(screen.getByTestId('sidebar-task-monitor-nav-badge').textContent).toBe('99+');
    });

    it('paints the running-task badge as a soft primary chip outside the glyph grayscale', () => {
        const css = readFileSync(join(process.cwd(), 'src/App.css'), 'utf8');
        const badge = css.match(/\.left-nav-item__badge \{[^}]+\}/);
        expect(badge?.[0]).toContain('var(--theme-primary)');
        expect(badge?.[0]).toContain('color: var(--theme-text-primary)');
        const darkBadge = css.match(/\.sidebar\[data-ai-theme='dark'\] \.left-nav-item \.left-nav-item__badge \{[^}]+\}/);
        expect(darkBadge?.[0]).toContain('color: var(--theme-text-primary)');
        expect(badge?.[0]).not.toContain('--theme-danger');
        expect(css).toContain('.sidebar-icon > :not(.left-nav-item__badge)');
        expect(css).not.toMatch(/\.left-nav-item:not\(\.left-nav-item--ai\) \.sidebar-icon,/);
    });

    it('ignores a non-numeric running-task count instead of rendering NaN', () => {
        renderRail({ runningTaskCount: Number.NaN });

        expect(screen.queryByTestId('sidebar-task-monitor-nav-badge')).toBeNull();
        expect(screen.getByTestId('sidebar-task-monitor-nav').getAttribute('aria-label')).toBe('Tasks');
    });

    it('prefers the background-task monitor when the tasks rail is opened', () => {
        const onOpenBackgroundTasks = vi.fn();
        const props = renderRail({ onOpenBackgroundTasks });

        fireEvent.click(screen.getByTestId('sidebar-task-monitor-nav'));

        expect(onOpenBackgroundTasks).toHaveBeenCalledTimes(1);
        // The handler already selects the remote tab; switching again would
        // reset the sub-tab it just chose.
        expect(props.switchTool).not.toHaveBeenCalled();
    });

    it('places ranking last in the system menu after gossip', () => {
        renderRail({
            lang: 'zh-Hans',
            gossipAllowed: true,
            t: (key) => ({ ranking: '排名', gossip: '八卦', about: '关于' }[key] || key),
            config: { remote_hub_url: 'https://hub.example/' },
        });

        fireEvent.click(screen.getByTestId('system-menu-trigger'));

        const items = screen.getAllByRole('menuitem');
        expect(items[items.length - 1]).toBe(screen.getByTestId('system-menu-ranking'));
        expect(items[items.length - 2]).toBe(screen.getByTestId('system-menu-gossip'));
        expect(screen.getByTestId('system-menu-ranking').textContent).toContain('排名');
        expect(screen.getByTestId('system-menu-ranking').textContent).not.toContain('价格');
    });

    it('replaces 排名 with 第n名 once Hub ranking loads', async () => {
        vi.mocked(GetHubUserRanking).mockResolvedValue(rankingResult({
            token_rank: 2,
            duration_rank: 1,
            total_users: 8,
        }));

        renderRail({
            lang: 'zh-Hans',
            remoteActivationStatus: { activated: true },
            config: { remote_hub_url: 'https://hub.example/' },
        });

        await waitFor(() => expect(GetHubUserRanking).toHaveBeenCalled());
        fireEvent.click(screen.getByTestId('system-menu-trigger'));

        await waitFor(() => {
            const rankingItem = screen.getByTestId('system-menu-ranking');
            expect(rankingItem.textContent).toContain('第1名');
            expect(rankingItem.textContent).not.toContain('价格');
            expect(rankingItem.textContent).not.toContain('排名');
        });
    });

    it('opens the hub ranking page from the system menu without switching tools', () => {
        const props = renderRail({ config: { remote_hub_url: 'https://hub.example/' } });

        fireEvent.click(screen.getByTestId('system-menu-trigger'));
        fireEvent.click(screen.getByTestId('system-menu-ranking'));

        expect(BrowserOpenURL).toHaveBeenCalledWith('https://hub.example/user-ranking');
        expect(props.switchTool).not.toHaveBeenCalledWith('ranking');
        expect(screen.queryByTestId('system-popup-menu')).toBeNull();
    });

    it('opens the hub ranking page scoped to the configured tenant', () => {
        renderRail({ config: { remote_hub_url: 'https://hub.example/', remote_tenant_id: 'tenant acme' } });

        fireEvent.click(screen.getByTestId('system-menu-trigger'));
        fireEvent.click(screen.getByTestId('system-menu-ranking'));

        expect(BrowserOpenURL).toHaveBeenCalledWith('https://hub.example/user-ranking?tenant_id=tenant+acme');
    });

    it('hides ranking when the hub URL cannot open a ranking page', () => {
        renderRail({ config: { remote_hub_url: 'not a url', remote_tenant_id: 'tenant acme' } });

        fireEvent.click(screen.getByTestId('system-menu-trigger'));

        expect(screen.queryByTestId('system-menu-ranking')).toBeNull();
        expect(BrowserOpenURL).not.toHaveBeenCalled();
    });

    it('hides ranking from the system menu when Hub ranking is disabled', () => {
        renderRail({ gossipAllowed: true, config: { remote_hub_url: 'https://hub.example/', show_hub_ranking: false } });

        fireEvent.click(screen.getByTestId('system-menu-trigger'));

        expect(screen.queryByTestId('system-menu-ranking')).toBeNull();
        const items = screen.getAllByRole('menuitem');
        expect(items[items.length - 1]).toBe(screen.getByTestId('system-menu-gossip'));
    });

    it('hides ranking when no hub URL is configured', () => {
        renderRail({ gossipAllowed: true, config: {} });

        fireEvent.click(screen.getByTestId('system-menu-trigger'));

        expect(screen.queryByTestId('system-menu-ranking')).toBeNull();
        const items = screen.getAllByRole('menuitem');
        expect(items[items.length - 1]).toBe(screen.getByTestId('system-menu-gossip'));
    });
});

describe('SidebarNavRail favorite employees', () => {
    it('removes the invitation button after Hub disables invitations while MaClaw is open', async () => {
        vi.useFakeTimers();
        vi.mocked(GetHubUserInvitationStatus)
            .mockResolvedValueOnce(invitationStatus(true))
            .mockResolvedValueOnce(invitationStatus(false));

        renderRail({ remoteActivationStatus: { activated: true }, config: { remote_hub_url: 'https://hub.example/' } });

        await act(async () => {
            await Promise.resolve();
        });
        expect(screen.getByTestId('sidebar-invite-nav')).toBeTruthy();

        await act(async () => {
            await vi.advanceTimersByTimeAsync(30_000);
        });

        expect(screen.queryByTestId('sidebar-invite-nav')).toBeNull();
    });

    it('shows the invitation button again when Hub re-enables invitations', async () => {
        vi.mocked(GetHubUserInvitationStatus)
            .mockResolvedValueOnce(invitationStatus(false))
            .mockResolvedValueOnce(invitationStatus(true));
        renderRail({ remoteActivationStatus: { activated: true }, config: { remote_hub_url: 'https://hub.example/' } });

        await waitFor(() => expect(screen.queryByTestId('sidebar-invite-nav')).toBeNull());
        Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
        document.dispatchEvent(new Event('visibilitychange'));

        await waitFor(() => expect(screen.getByTestId('sidebar-invite-nav')).toBeTruthy());
    });

    it('places the invitation entry under the bot button and opens the invitation dialog from 推荐', async () => {
        vi.mocked(DesktopBotAccess).mockResolvedValue({ enabled: true });
        vi.mocked(GetHubUserInvitationStatus).mockResolvedValue(invitationStatus(true));
        renderRail({ lang: 'zh-Hans', remoteActivationStatus: { activated: true } });

        await screen.findByTestId('sidebar-bot-nav');
        const invite = screen.getByTestId('sidebar-invite-nav');

        // The entry lives directly under the Bot icon in the live rail...
        expect(screen.getByTestId('sidebar-bot-nav')).toBe(invite.previousElementSibling);
        // ...with the Simplified-Chinese 推荐 label and the original invitation dialog wiring.
        expect(invite.textContent).toContain('推荐');
        fireEvent.click(invite);
        expect(await screen.findByText('邀请好友')).toBeTruthy();
    });

    it('styles the invitation entry as a vertical pill outside the icon grayscale', () => {
        const css = readFileSync(join(process.cwd(), 'src/App.css'), 'utf8');
        const pill = css.match(/\.sidebar \.left-nav-item\.left-nav-item--invite \{[^}]+\}/);
        expect(pill?.[0]).toMatch(/overflow:\s*visible/);
        expect(pill?.[0]).not.toContain('!important');
        const badge = css.match(/\.sidebar \.left-nav-item\.left-nav-item--invite \.sidebar-icon \.invite-nav-icon-badge \{[^}]+\}/);
        expect(badge?.[0]).toContain('filter: none');
        // The old hidden-footer invite markup is gone for good.
        expect(css).not.toContain('.snr-invite-dot');
        expect(css).not.toContain('.snr-invite-divider');
    });

    it('keeps the rail vertical rhythm compact so the bottom pills fit without scrolling', () => {
        // 13 entries stacked at ~62px each overflowed a 1600x1024 Wails window
        // and clipped 签到/Bot/推荐 behind the fixed system trigger. The rhythm
        // below keeps the full stack at roughly 850px; a scroll container is
        // the fallback for genuinely short windows only.
        const css = readFileSync(join(process.cwd(), 'src/App.css'), 'utf8');
        const generic = css.match(/\.sidebar \.left-nav-item:not\(\.left-nav-item--bot\):not\(\.left-nav-item--invite\) \{[^}]+\}/);
        expect(generic?.[0]).toContain('min-height: 52px !important');
        expect(generic?.[0]).toContain('margin: 2px 10px !important');
        // The rail shell itself stays slim around the stack.
        expect(css).toContain('.mc-nav-rail { padding: 10px 12px !important; }');
        const bot = css.match(/\.sidebar \.left-nav-item\.left-nav-item--bot \{[^}]+\}/);
        expect(bot?.[0]).toContain('min-height: 52px');
        expect(bot?.[0]).toContain('margin: 2px 6px');
        const checkin = css.match(/\.left-nav-item--checkin\.left-nav-item--checkin \{[\s\S]*?\n\}/);
        expect(checkin?.[0]).toContain('min-height: 52px !important');
        expect(checkin?.[0]).toContain('margin: 2px 6px !important');
        const invite = css.match(/\.sidebar \.left-nav-item\.left-nav-item--invite \{[^}]+\}/);
        expect(invite?.[0]).toContain('min-height: 52px');
        expect(invite?.[0]).toContain('margin: 2px 6px');
        // The fixed system trigger at the rail bottom stays compact too: the
        // user-requested smaller entry (44x46 box, 34px badge) replaced the
        // old 52x58 trigger.
        const profile = css.match(/\.mc-profile-rail \{[^}]+\}/);
        expect(profile?.[0]).toContain('height: 46px');
        expect(profile?.[0]).toContain('width: 44px');
        // The brand-gap reset must out-rank the generic !important margin, so
        // pin the full (0,5,0) doubled-class selector — a lower-specificity
        // rewrite would silently lose the cascade and reopen the gap.
        expect(css).toContain('.mc-nav-rail .mc-nav-rail__scroll > .left-nav-item.left-nav-item:first-child { margin-top: 0 !important; }');
    });

    it('does not poll invitation status while MaClaw is in the background', async () => {
        vi.useFakeTimers();
        renderRail({ remoteActivationStatus: { activated: true }, config: { remote_hub_url: 'https://hub.example/' } });

        await act(async () => {
            await Promise.resolve();
        });
        expect(GetHubUserInvitationStatus).toHaveBeenCalledTimes(1);

        Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
        await act(async () => {
            await vi.advanceTimersByTimeAsync(2 * 60_000);
        });

        expect(GetHubUserInvitationStatus).toHaveBeenCalledTimes(1);
    });

    it('renders and opens the Utilities entry when enabled', () => {
        const props = renderRail({ utilitiesLabel: 'Experts & Tools' });

        const utilitiesEntry = screen.getByTestId('sidebar-utilities-nav');
        expect(utilitiesEntry).toBeTruthy();
        expect(utilitiesEntry.getAttribute('style')).not.toContain('display: none');
        expect(utilitiesEntry.textContent).toContain('Experts & Tools');

        fireEvent.click(utilitiesEntry);

        expect(props.switchTool).toHaveBeenCalledWith('utilities');
    });

    it('splits the AI Experts and Tools entries when the dedicated tools rail is enabled', () => {
        const props = renderRail({ lang: 'zh-Hans', showToolsEntry: true });

        const expertsEntry = screen.getByTestId('sidebar-utilities-nav');
        const toolsEntry = screen.getByTestId('sidebar-tools-nav');
        expect(expertsEntry.textContent).toContain('专业功能');
        expect(screen.getByTestId('sidebar-pro-features-icon')).toBeTruthy();
        expect(expertsEntry.textContent).not.toContain('AI 专家');
        expect(expertsEntry.textContent).not.toContain('专家&工具');
        expect(toolsEntry.textContent).toContain('工具');
        expect(toolsEntry.getAttribute('title')).toBe('工具');

        fireEvent.click(toolsEntry);
        expect(props.switchTool).toHaveBeenCalledWith('tools');
    });

    it('defaults the utilities rail label to 专家&工具 in Simplified Chinese', () => {
        renderRail({ lang: 'zh-Hans' });

        const utilitiesEntry = screen.getByTestId('sidebar-utilities-nav');
        expect(utilitiesEntry.textContent).toContain('专家&工具');
        expect(utilitiesEntry.getAttribute('title')).toBe('专家&工具');
        expect(screen.getByTestId('sidebar-expert-icon')).toBeTruthy();
    });

    it('defaults Traditional Chinese utilities rail label', () => {
        renderRail({ lang: 'zh-Hant' });
        expect(screen.getByTestId('sidebar-utilities-nav').textContent).toContain('專家&工具');
    });

    it('shortens the English rail label and keeps the full name as the tooltip', () => {
        renderRail({ lang: 'en' });
        const utilitiesEntry = screen.getByTestId('sidebar-utilities-nav');
        expect(utilitiesEntry.textContent).toContain('Experts');
        expect(utilitiesEntry.textContent).not.toContain('Experts & Tools');
        expect(utilitiesEntry.getAttribute('title')).toBe('Experts & Tools');
        expect(screen.getByTestId('sidebar-expert-icon')).toBeTruthy();
    });

    it('hides the Utilities entry when disabled', () => {
        renderRail({ showUtilitiesEntry: false, utilitiesLabel: 'Utilities' });

        expect(screen.getByTestId('sidebar-utilities-nav').getAttribute('style')).toContain('display: none');
    });

    it('marks the Utilities entry active when its page is selected', () => {
        renderRail({ navTab: 'utilities', utilitiesLabel: 'Utilities' });

        expect(screen.getByTestId('sidebar-utilities-nav').classList.contains('active')).toBe(true);
    });

    it('hides the apps entry by default', () => {
        renderRail();

        expect(screen.queryByTitle(miniAppLabels.short.en)).toBeNull();
    });

    it('hides the apps entry when disabled', () => {
        renderRail({ showAppEntry: false });

        expect(screen.queryByTitle(miniAppLabels.short.en)).toBeNull();
        expect(screen.queryByTestId('fav-ve-ve-1')).toBeNull();
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
        expect(aiEntry.tagName).toBe('BUTTON');
        expect((aiEntry as HTMLButtonElement).type).toBe('button');
        expect(icon?.getAttribute('stroke')).toBe('currentColor');
        expect(icon?.getAttribute('stroke-width')).toBe('1.8');
        expect(aiEntry.getAttribute('aria-current')).toBeNull();
        expect(document.querySelector('.left-nav-item--ai.active')).toBeNull();
    });

    it('marks AI nav active for selected badge state', () => {
        renderRail({ navTab: 'ai' });

        const aiEntry = screen.getByTitle('AI Asst');
        expect(aiEntry.tagName).toBe('BUTTON');
        expect(aiEntry.classList.contains('active')).toBe(true);
        expect(aiEntry.getAttribute('aria-current')).toBe('page');
        expect(aiEntry.querySelector('.ai-nav-icon-badge')).toBeTruthy();
        expect(aiEntry.querySelector('.ai-nav-icon')?.getAttribute('stroke')).toBe('currentColor');
        expect(aiEntry.getAttribute('style')).toBeNull();
    });

    it('hides the bot entry and its denial notice when Bot is off', async () => {
        vi.mocked(DesktopBotAccess).mockResolvedValue({ enabled: false, message: '服务器没有开通bot功能' });
        renderRail({ lang: 'zh-Hans', remoteActivationStatus: { activated: true } });

        await waitFor(() => expect(DesktopBotAccess).toHaveBeenCalled());
        await act(async () => {});

        expect(screen.queryByTestId('sidebar-bot-nav')).toBeNull();
        expect(screen.queryByTestId('sidebar-bot-disabled')).toBeNull();
    });

    it('hides the bot entry without polling when the app is not activated on a Hub', async () => {
        renderRail({ remoteActivationStatus: { activated: false } });
        await act(async () => {});

        expect(screen.queryByTestId('sidebar-bot-nav')).toBeNull();
        expect(DesktopBotAccess).not.toHaveBeenCalled();
    });

    it('opens bot management from the bottom-left bot entry', async () => {
        vi.mocked(DesktopBotAccess).mockResolvedValue({ enabled: true });
        const props = renderRail({ lang: 'zh-Hans', showAppEntry: true, remoteActivationStatus: { activated: true } });
        const bot = await screen.findByTestId('sidebar-bot-nav');
        await waitFor(() => expect(DesktopBotAccess).toHaveBeenCalled());
        await act(async () => {});
        const employees = screen.getByTestId('sidebar-digital-employees-nav');

        expect(bot.classList.contains('left-nav-item--bot')).toBe(true);
        expect(bot.querySelector('[data-testid="sidebar-bot-icon"]')?.getAttribute('width')).toBe('22');
        const badge = bot.querySelector('.bot-nav-icon-badge');
        expect(badge).toBeTruthy();
        expect(badge?.classList.contains('left-nav-item__badge')).toBe(false);
        const css = readFileSync(join(process.cwd(), 'src/App.css'), 'utf8');
        expect(css).toContain('.left-nav-item:not(.left-nav-item--ai):not(.left-nav-item--bot) .sidebar-icon');
        expect(css).toMatch(/\.sidebar \.left-nav-item\.left-nav-item--bot \{[^}]*overflow:\s*visible/);
        expect(bot.textContent).toContain('Bot');
        expect(bot.compareDocumentPosition(screen.getByTestId('system-menu-trigger')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(employees.textContent).toContain('数字员工');
        expect(screen.queryByTestId('fav-ve-ve-1')).toBeNull();

        fireEvent.click(employees);
        expect(props.switchTool).toHaveBeenCalledWith('ai');

        fireEvent.click(bot);

        expect(props.switchTool).toHaveBeenCalledWith('bots');
        expect(props.onStartVEConversation).not.toHaveBeenCalled();
    });

    it('keeps the bot entry through a Hub blip, hides it and leaves the page when Hub stays down', async () => {
        vi.useFakeTimers();
        vi.mocked(DesktopBotAccess)
            .mockResolvedValueOnce({ enabled: true })
            .mockRejectedValueOnce(new Error('offline'))
            .mockRejectedValueOnce(new Error('offline'));
        const props = renderRail({ navTab: 'bots', remoteActivationStatus: { activated: true } });

        await act(async () => { await Promise.resolve(); });
        expect(screen.getByTestId('sidebar-bot-nav')).toBeTruthy();

        await act(async () => { await vi.advanceTimersByTimeAsync(30_000); });
        expect(screen.getByTestId('sidebar-bot-nav')).toBeTruthy();
        expect(props.switchTool).not.toHaveBeenCalledWith('ai');

        await act(async () => { await vi.advanceTimersByTimeAsync(30_000); });
        expect(screen.queryByTestId('sidebar-bot-nav')).toBeNull();
        expect(props.switchTool).toHaveBeenCalledWith('ai');
    });

    it('keeps the bot entry hidden when a newer denial arrives before an older poll', async () => {
        let resolveAccess: (value: { enabled: boolean }) => void = () => {};
        vi.mocked(DesktopBotAccess).mockReturnValue(new Promise(resolve => {
            resolveAccess = resolve;
        }));
        renderRail({ remoteActivationStatus: { activated: true } });

        act(() => { publishBotAccess(false); });
        await act(async () => { resolveAccess({ enabled: true }); });

        expect(screen.queryByTestId('sidebar-bot-nav')).toBeNull();
        expect(screen.queryByTestId('sidebar-bot-disabled')).toBeNull();
    });

    it('hides the bot entry when the grant is gone', async () => {
        vi.mocked(DesktopBotAccess).mockResolvedValue({ enabled: true });
        const props = renderRail({ remoteActivationStatus: { activated: true } });
        await screen.findByTestId('sidebar-bot-nav');
        await waitFor(() => expect(DesktopBotAccess).toHaveBeenCalled());
        await act(async () => {});

        act(() => { publishBotAccess(false); });

        expect(props.switchTool).not.toHaveBeenCalledWith('bots');
        expect(screen.queryByTestId('sidebar-bot-nav')).toBeNull();
        expect(screen.queryByTestId('sidebar-bot-disabled')).toBeNull();
    });

    it('leaves the bot page and hides the entry when the tenant switch no longer includes this user', async () => {
        vi.mocked(DesktopBotAccess).mockResolvedValue({ enabled: false });
        const props = renderRail({ navTab: 'bots', remoteActivationStatus: { activated: true } });

        await waitFor(() => expect(props.switchTool).toHaveBeenCalledWith('ai'));
        expect(screen.queryByTestId('sidebar-bot-nav')).toBeNull();
    });
    it('opens the extensions menu from the 扩展 rail entry and opens Skills', () => {
        const props = renderRail({ lang: 'zh-Hans' });
        const extensionsEntry = screen.getByTestId('sidebar-extensions-nav');

        fireEvent.click(extensionsEntry);

        expect(screen.getByTestId('extensions-popup-menu')).toBeTruthy();
        expect(extensionsEntry.getAttribute('aria-expanded')).toBe('true');
        expect(extensionsEntry.getAttribute('aria-haspopup')).toBe('menu');

        fireEvent.click(screen.getByTestId('extensions-menu-skills'));

        expect(props.switchTool).toHaveBeenCalledWith('skills');
        expect(screen.queryByTestId('extensions-popup-menu')).toBeNull();
    });

    it('opens the MCP connectors page from the extensions menu', () => {
        const props = renderRail({ lang: 'zh-Hans' });

        fireEvent.click(screen.getByTestId('sidebar-extensions-nav'));
        fireEvent.click(screen.getByTestId('extensions-menu-mcp'));

        expect(props.switchTool).toHaveBeenCalledWith('mcp');
        expect(screen.queryByTestId('extensions-popup-menu')).toBeNull();
    });

    it('keeps skills and MCP on the extensions menu, and leaves Token Bank off it', () => {
        renderRail({ lang: 'zh-Hans', gossipAllowed: false, config: {} });

        fireEvent.click(screen.getByTestId('system-menu-trigger'));
        expect(screen.getAllByRole('menuitem').map(item => item.getAttribute('data-testid'))).toEqual(['system-menu-about']);

        fireEvent.click(screen.getByTestId('sidebar-extensions-nav'));
        expect(screen.queryByTestId('system-popup-menu')).toBeNull();
        expect(screen.getAllByRole('menuitem').map(item => item.getAttribute('data-testid'))).toEqual([
            'extensions-menu-skills',
            'extensions-menu-mcp',
        ]);
        expect(screen.queryByTestId('extensions-menu-tokenbank')).toBeNull();
        expect(screen.getByTestId('extensions-popup-menu').textContent).not.toContain('Token');
    });

    it('marks the 扩展 entry active on the skills and mcp pages', () => {
        const first = renderRail({ navTab: 'skills' });
        expect(screen.getByTestId('sidebar-extensions-nav').classList.contains('active')).toBe(true);
        first.unmount();

        renderRail({ navTab: 'mcp' });
        expect(screen.getByTestId('sidebar-extensions-nav').classList.contains('active')).toBe(true);
    });

    it('closes the extensions menu when its rail entry is clicked again', () => {
        renderRail({ lang: 'zh-Hans' });
        const trigger = screen.getByTestId('sidebar-extensions-nav');

        fireEvent.click(trigger);
        expect(screen.getByTestId('extensions-popup-menu')).toBeTruthy();

        fireEvent.mouseDown(trigger);
        fireEvent.click(trigger);

        expect(screen.queryByTestId('extensions-popup-menu')).toBeNull();
        expect(trigger.getAttribute('aria-expanded')).toBe('false');
    });

    it('closes the extensions menu on an outside click', async () => {
        renderRail({ lang: 'zh-Hans' });

        fireEvent.click(screen.getByTestId('sidebar-extensions-nav'));
        expect(screen.getByTestId('extensions-popup-menu')).toBeTruthy();

        // The menu registers its outside-click listener on a zero-delay timer
        // so the opening click itself does not immediately close it.
        await act(async () => { await new Promise(resolve => setTimeout(resolve, 0)); });
        fireEvent.mouseDown(document.body);

        expect(screen.queryByTestId('extensions-popup-menu')).toBeNull();
    });

    it('keeps the extensions and system menus mutually exclusive', () => {
        renderRail({ lang: 'zh-Hans' });

        fireEvent.click(screen.getByTestId('sidebar-extensions-nav'));
        expect(screen.getByTestId('extensions-popup-menu')).toBeTruthy();

        fireEvent.click(screen.getByTestId('system-menu-trigger'));
        expect(screen.queryByTestId('extensions-popup-menu')).toBeNull();
        expect(screen.getByTestId('system-popup-menu')).toBeTruthy();

        fireEvent.mouseDown(screen.getByTestId('sidebar-extensions-nav'));
        fireEvent.click(screen.getByTestId('sidebar-extensions-nav'));
        expect(screen.queryByTestId('system-popup-menu')).toBeNull();
        expect(screen.getByTestId('extensions-popup-menu')).toBeTruthy();
    });

});

describe('SidebarNavRail professional features menu', () => {
    it('opens AI experts and workflows from the 专业功能 rail entry', () => {
        const props = renderRail({ lang: 'zh-Hans', showToolsEntry: true });
        const entry = screen.getByTestId('sidebar-utilities-nav');

        fireEvent.click(entry);

        expect(screen.getByTestId('pro-features-popup-menu')).toBeTruthy();
        expect(entry.getAttribute('aria-expanded')).toBe('true');
        expect(entry.getAttribute('aria-haspopup')).toBe('menu');
        expect(props.switchTool).not.toHaveBeenCalled();

        fireEvent.click(screen.getByTestId('pro-features-menu-utilities'));
        expect(props.switchTool).toHaveBeenCalledWith('utilities');
        expect(screen.queryByTestId('pro-features-popup-menu')).toBeNull();

        fireEvent.click(entry);
        fireEvent.click(screen.getByTestId('pro-features-menu-workflows'));
        expect(props.switchTool).toHaveBeenCalledWith('workflows');
        expect(screen.queryByTestId('pro-features-popup-menu')).toBeNull();
    });

    it('labels the menu in English and Traditional Chinese', () => {
        const english = renderRail({ lang: 'en', showToolsEntry: true });
        fireEvent.click(screen.getByTestId('sidebar-utilities-nav'));
        expect(screen.getByTestId('sidebar-utilities-nav').textContent).toContain('Features');
        expect(screen.getByTestId('pro-features-menu-utilities').textContent).toContain('AI Experts');
        expect(screen.getByTestId('pro-features-menu-workflows').textContent).toContain('Workflows');
        english.unmount();

        renderRail({ lang: 'zh-Hant', showToolsEntry: true });
        fireEvent.click(screen.getByTestId('sidebar-utilities-nav'));
        expect(screen.getByTestId('sidebar-utilities-nav').textContent).toContain('專業功能');
        expect(screen.getByTestId('pro-features-menu-utilities').textContent).toContain('AI 專家');
        expect(screen.getByTestId('pro-features-menu-workflows').textContent).toContain('工作流');
    });

    it('marks the 专业功能 entry active on the experts and workflows pages', () => {
        const experts = renderRail({ lang: 'zh-Hans', showToolsEntry: true, navTab: 'utilities' });
        expect(screen.getByTestId('sidebar-utilities-nav').classList.contains('active')).toBe(true);
        expect(screen.getByTestId('sidebar-utilities-nav').getAttribute('aria-current')).toBe('page');
        experts.unmount();

        renderRail({ lang: 'zh-Hans', showToolsEntry: true, navTab: 'workflows' });
        expect(screen.getByTestId('sidebar-utilities-nav').classList.contains('active')).toBe(true);
        expect(screen.getByTestId('sidebar-utilities-nav').getAttribute('aria-current')).toBe('page');
    });

    it('marks the open destination inside the professional features menu', () => {
        const view = renderRail({ lang: 'zh-Hans', showToolsEntry: true, navTab: 'utilities' });
        fireEvent.click(screen.getByTestId('sidebar-utilities-nav'));
        expect(screen.getByTestId('pro-features-menu-utilities').getAttribute('aria-current')).toBe('page');
        expect(screen.getByTestId('pro-features-menu-workflows').getAttribute('aria-current')).toBeNull();
        view.unmount();

        renderRail({ lang: 'zh-Hans', showToolsEntry: true, navTab: 'workflows' });
        fireEvent.click(screen.getByTestId('sidebar-utilities-nav'));
        expect(screen.getByTestId('pro-features-menu-workflows').getAttribute('aria-current')).toBe('page');
        expect(screen.getByTestId('pro-features-menu-utilities').getAttribute('aria-current')).toBeNull();
        expect(document.activeElement).toBe(screen.getByTestId('pro-features-menu-workflows'));
    });

    it('opens the only remaining destination directly when workflow is disabled', () => {
        const props = renderRail({ lang: 'zh-Hans', showToolsEntry: true, config: { show_workflow_entry: false } });
        const entry = screen.getByTestId('sidebar-utilities-nav');

        fireEvent.click(entry);

        expect(screen.queryByTestId('pro-features-popup-menu')).toBeNull();
        expect(entry.getAttribute('aria-haspopup')).toBeNull();
        expect(props.switchTool).toHaveBeenCalledWith('utilities');
    });

    it('opens workflows directly when the experts entry is disabled', () => {
        const props = renderRail({ lang: 'zh-Hans', showToolsEntry: true, showUtilitiesEntry: false });
        const entry = screen.getByTestId('sidebar-utilities-nav');

        fireEvent.click(entry);

        expect(screen.queryByTestId('pro-features-popup-menu')).toBeNull();
        expect(props.switchTool).toHaveBeenCalledWith('workflows');
    });

    it('hides 专业功能 when both destinations are disabled', () => {
        renderRail({
            lang: 'zh-Hans',
            showToolsEntry: true,
            showUtilitiesEntry: false,
            config: { show_workflow_entry: false },
        });

        expect(screen.getByTestId('sidebar-utilities-nav').getAttribute('style')).toContain('display: none');
    });

    it('does not treat a disabled workflow page as the current professional feature', () => {
        renderRail({ lang: 'zh-Hans', showToolsEntry: true, navTab: 'workflows', config: { show_workflow_entry: false } });

        expect(screen.getByTestId('sidebar-utilities-nav').getAttribute('aria-current')).toBeNull();
        expect(screen.getByTestId('sidebar-utilities-nav').classList.contains('active')).toBe(false);
    });

    it('keeps the professional features menu exclusive with the extensions menu', () => {
        renderRail({ lang: 'zh-Hans', showToolsEntry: true });

        fireEvent.click(screen.getByTestId('sidebar-utilities-nav'));
        expect(screen.getByTestId('pro-features-popup-menu')).toBeTruthy();

        fireEvent.click(screen.getByTestId('sidebar-extensions-nav'));
        expect(screen.queryByTestId('pro-features-popup-menu')).toBeNull();
        expect(screen.getByTestId('extensions-popup-menu')).toBeTruthy();

        fireEvent.mouseDown(screen.getByTestId('sidebar-utilities-nav'));
        fireEvent.click(screen.getByTestId('sidebar-utilities-nav'));
        expect(screen.queryByTestId('extensions-popup-menu')).toBeNull();
        expect(screen.getByTestId('pro-features-popup-menu')).toBeTruthy();
    });

    it('closes the professional features menu when its rail entry is clicked again', () => {
        renderRail({ lang: 'zh-Hans', showToolsEntry: true });
        const trigger = screen.getByTestId('sidebar-utilities-nav');

        fireEvent.click(trigger);
        expect(screen.getByTestId('pro-features-popup-menu')).toBeTruthy();

        fireEvent.mouseDown(trigger);
        fireEvent.click(trigger);

        expect(screen.queryByTestId('pro-features-popup-menu')).toBeNull();
        expect(trigger.getAttribute('aria-expanded')).toBe('false');
    });
});

describe('SidebarNavRail library menu', () => {
    it('opens the library menu from the 资料库 rail entry and opens Mobile documents', () => {
        const props = renderRail({ lang: 'zh-Hans' });
        const libraryEntry = screen.getByTestId('sidebar-files-nav');

        fireEvent.click(libraryEntry);

        expect(screen.getByTestId('library-popup-menu')).toBeTruthy();
        expect(libraryEntry.getAttribute('aria-expanded')).toBe('true');
        expect(libraryEntry.getAttribute('aria-haspopup')).toBe('menu');

        expect(screen.getByTestId('library-menu-documents').textContent).toContain('云盘');
        fireEvent.click(screen.getByTestId('library-menu-documents'));

        expect(props.switchTool).toHaveBeenCalledWith('files');
        expect(screen.queryByTestId('library-popup-menu')).toBeNull();
    });

    it('opens the Knowledge settings tab from the library menu', () => {
        const props = renderRail({ lang: 'zh-Hans' });
        const openSettingsEvents: Array<{ tab?: string }> = [];
        const listener = (event: Event) => {
            openSettingsEvents.push((event as CustomEvent<{ tab?: string }>).detail);
        };
        window.addEventListener(OPEN_SETTINGS_EVENT, listener);
        try {
            fireEvent.click(screen.getByTestId('sidebar-files-nav'));
            fireEvent.click(screen.getByTestId('library-menu-knowledge'));

            expect(openSettingsEvents).toEqual([{ tab: 'knowledge' }]);
            expect(props.switchTool).not.toHaveBeenCalled();
            expect(screen.queryByTestId('library-popup-menu')).toBeNull();
        } finally {
            window.removeEventListener(OPEN_SETTINGS_EVENT, listener);
        }
    });

    it('marks the 资料库 entry active on the files page and on the Knowledge settings tab', () => {
        const first = renderRail({ navTab: 'files' });
        expect(screen.getByTestId('sidebar-files-nav').classList.contains('active')).toBe(true);
        first.unmount();

        renderRail({ navTab: 'settings', settingsTab: 'knowledge' });
        expect(screen.getByTestId('sidebar-files-nav').classList.contains('active')).toBe(true);
        expect(screen.getByTestId('sidebar-settings-nav').classList.contains('active')).toBe(false);
    });

    it('closes the library menu when its rail entry is clicked again', () => {
        renderRail({ lang: 'zh-Hans' });
        const trigger = screen.getByTestId('sidebar-files-nav');

        fireEvent.click(trigger);
        expect(screen.getByTestId('library-popup-menu')).toBeTruthy();

        fireEvent.mouseDown(trigger);
        fireEvent.click(trigger);

        expect(screen.queryByTestId('library-popup-menu')).toBeNull();
        expect(trigger.getAttribute('aria-expanded')).toBe('false');
    });

    it('closes the library menu on an outside click', async () => {
        renderRail({ lang: 'zh-Hans' });

        fireEvent.click(screen.getByTestId('sidebar-files-nav'));
        expect(screen.getByTestId('library-popup-menu')).toBeTruthy();

        // The menu registers its outside-click listener on a zero-delay timer
        // so the opening click itself does not immediately close it.
        await act(async () => { await new Promise(resolve => setTimeout(resolve, 0)); });
        fireEvent.mouseDown(document.body);

        expect(screen.queryByTestId('library-popup-menu')).toBeNull();
    });

    it('keeps the library, extensions and system menus mutually exclusive', () => {
        renderRail({ lang: 'zh-Hans' });

        fireEvent.click(screen.getByTestId('sidebar-files-nav'));
        expect(screen.getByTestId('library-popup-menu')).toBeTruthy();

        fireEvent.click(screen.getByTestId('sidebar-extensions-nav'));
        expect(screen.queryByTestId('library-popup-menu')).toBeNull();
        expect(screen.getByTestId('extensions-popup-menu')).toBeTruthy();

        fireEvent.mouseDown(screen.getByTestId('sidebar-files-nav'));
        fireEvent.click(screen.getByTestId('sidebar-files-nav'));
        expect(screen.queryByTestId('extensions-popup-menu')).toBeNull();
        expect(screen.getByTestId('library-popup-menu')).toBeTruthy();

        fireEvent.click(screen.getByTestId('system-menu-trigger'));
        expect(screen.queryByTestId('library-popup-menu')).toBeNull();
        expect(screen.getByTestId('system-popup-menu')).toBeTruthy();
    });
});

describe('SidebarNavRail TigerClaw brand', () => {
    it('shows the TigerClaw name under the product icon', () => {
        renderRail({
            brandInfo: { id: 'qianxin' },
            brandSidebarName: 'TigerClaw',
            currentIcon: 'qianxin.png',
        });

        const header = document.querySelector('.sidebar-header--tiger');
        expect(header).toBeTruthy();
        const logo = header?.querySelector('img.sidebar-logo');
        expect(logo?.getAttribute('src')).toBe('qianxin.png');
        expect(header?.textContent).toContain('TigerClaw');
        expect(header?.querySelector('.mc-sidebar-brand-mark')).toBeNull();
    });

    it('does not paint a blue plate around the TigerClaw icon', () => {
        const css = readFileSync(join(process.cwd(), 'src/App.css'), 'utf8');
        const header = css.match(/\.sidebar-header--tiger \{[^}]+\}/);
        const rule = css.match(/\.sidebar-header--tiger \.sidebar-logo \{[^}]+\}/);
        expect(header).toBeTruthy();
        expect(header?.[0]).toContain('background: transparent');
        expect(header?.[0]).not.toContain('primary-soft');
        expect(rule).toBeTruthy();
        expect(rule?.[0]).toContain('background: transparent');
        expect(rule?.[0]).toContain('padding: 0');
        expect(rule?.[0]).toMatch(/margin-bottom:\s*0\s*!important/);
        expect(rule?.[0]).not.toContain('--mc-accent');
    });
});


describe('SidebarNavRail scroll wrapper', () => {
    it('keeps the nav and pills inside the scroll wrapper, the trigger and popups outside', () => {
        renderRail({
            lang: 'zh-Hans',
            hubCheckin: { enabled: true, credits: 5, checkedInToday: false },
        });
        const wrapper = document.querySelector('.mc-nav-rail__scroll');
        expect(wrapper).toBeTruthy();
        expect(wrapper?.querySelector('[data-testid="sidebar-checkin-nav"]')).toBeTruthy();
        expect(wrapper?.querySelector('[data-testid="sidebar-settings-nav"]')).toBeTruthy();
        // The system trigger is a direct rail child so the scroll container
        // can never clip it.
        const trigger = document.querySelector('.mc-nav-rail > [data-testid="system-menu-trigger"]');
        expect(trigger).toBeTruthy();

        fireEvent.click(screen.getByTitle('系统菜单'));
        const popup = screen.getByTestId('system-popup-menu');
        expect(wrapper?.contains(popup)).toBe(false);
    });

    it('closes open popup menus when the scroll wrapper scrolls', () => {
        renderRail({ lang: 'zh-Hans' });
        fireEvent.click(screen.getByTitle('系统菜单'));
        expect(screen.getByTestId('system-popup-menu')).toBeTruthy();

        fireEvent.scroll(document.querySelector('.mc-nav-rail__scroll') as Element);
        expect(screen.queryByTestId('system-popup-menu')).toBeNull();
    });
});
