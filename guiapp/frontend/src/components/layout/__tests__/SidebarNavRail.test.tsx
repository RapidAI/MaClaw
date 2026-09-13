// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type React from 'react';

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetHubUserInvitationStatus: vi.fn().mockResolvedValue({ enabled: false }),
    GetHubUserRanking: vi.fn().mockResolvedValue({ error: 'hub not configured' }),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    BrowserOpenURL: vi.fn(),
    EventsOn: vi.fn().mockReturnValue(() => {}),
}));

import { SidebarNavRail } from '../SidebarNavRail';
import { GetHubUserInvitationStatus, GetHubUserRanking } from '../../../../wailsjs/go/main/App';
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
    vi.mocked(GetHubUserInvitationStatus).mockReset();
    vi.mocked(GetHubUserInvitationStatus).mockResolvedValue(invitationStatus(false));
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
    const view = render(<SidebarNavRail {...props} />);
    return Object.assign(props, { unmount: view.unmount });
}


describe('SidebarNavRail system popup', () => {
    it('opens About first from the system menu without duplicating settings or the task monitor', () => {
        const props = renderRail({ lang: 'zh-Hans', runningTaskCount: 3 });

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

    it('keeps the task rail as the background monitor entry', () => {
        const onOpenBackgroundTasks = vi.fn();
        const props = renderRail({ onOpenBackgroundTasks });

        fireEvent.click(screen.getByTestId('sidebar-task-monitor-nav'));

        expect(onOpenBackgroundTasks).toHaveBeenCalledTimes(1);
        expect(props.switchTool).not.toHaveBeenCalledWith('remote');
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
        expect(screen.getByTitle('Invite friends')).toBeTruthy();

        await act(async () => {
            await vi.advanceTimersByTimeAsync(30_000);
        });

        expect(screen.queryByTitle('Invite friends')).toBeNull();
    });

    it('shows the invitation button again when Hub re-enables invitations', async () => {
        vi.mocked(GetHubUserInvitationStatus)
            .mockResolvedValueOnce(invitationStatus(false))
            .mockResolvedValueOnce(invitationStatus(true));
        renderRail({ remoteActivationStatus: { activated: true }, config: { remote_hub_url: 'https://hub.example/' } });

        await waitFor(() => expect(screen.queryByTitle('Invite friends')).toBeNull());
        Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
        document.dispatchEvent(new Event('visibilitychange'));

        await waitFor(() => expect(screen.getByTitle('Invite friends')).toBeTruthy());
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
        expect(expertsEntry.textContent).toContain('AI 专家');
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

    it('switches to AI before opening a favorite digital employee conversation', () => {
        const props = renderRail({ showAppEntry: true });

        fireEvent.click(screen.getByTestId('fav-ve-ve-1'));

        expect(props.switchTool).toHaveBeenCalledWith('ai');
        expect(props.onStartVEConversation).toHaveBeenCalledWith('ve-1');
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

    it('keeps skills and MCP only on the extensions menu, not the system menu', () => {
        renderRail({ lang: 'zh-Hans', gossipAllowed: false, config: {} });

        fireEvent.click(screen.getByTestId('system-menu-trigger'));
        expect(screen.getAllByRole('menuitem').map(item => item.getAttribute('data-testid'))).toEqual(['system-menu-about']);

        fireEvent.click(screen.getByTestId('sidebar-extensions-nav'));
        expect(screen.queryByTestId('system-popup-menu')).toBeNull();
        expect(screen.getAllByRole('menuitem').map(item => item.getAttribute('data-testid'))).toEqual([
            'extensions-menu-skills',
            'extensions-menu-mcp',
        ]);
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

describe('SidebarNavRail library menu', () => {
    it('opens the library menu from the 资料库 rail entry and opens Mobile documents', () => {
        const props = renderRail({ lang: 'zh-Hans' });
        const libraryEntry = screen.getByTestId('sidebar-files-nav');

        fireEvent.click(libraryEntry);

        expect(screen.getByTestId('library-popup-menu')).toBeTruthy();
        expect(libraryEntry.getAttribute('aria-expanded')).toBe('true');
        expect(libraryEntry.getAttribute('aria-haspopup')).toBe('menu');

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
