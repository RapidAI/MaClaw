import { useCallback, useState, useEffect, useRef } from 'react';
import { SIDEBAR_NAV_RAIL_WIDTH } from './sidebarLayout';
import { SystemPopupMenu, type SystemMenuItem } from './SystemPopupMenu';
import type { FavoriteEmployeeSlot } from './FavoriteEmployeeButtons';
import { BotRailIcon, CheckinRailIcon, ConnectMenuHardwareIcon, ConnectMenuIMIcon, ConnectMenuMailIcon, SystemIcon, AboutIcon, SkillsIcon, MCPIcon, GossipIcon, RankingIcon, MobileDocsIcon, KnowledgeIcon, LatexTemplateIcon, ExpertRailIcon, WorkflowIcon, ToolsRailIcon, InviteGiftIcon } from './SidebarNavIcons';
import { SidebarBrandHeader, SidebarPrimaryNav, useNavMenuToggles, visibleProFeatureTabs, type ProFeatureTab } from './SidebarNavRailPieces';
import { openSettingsTab } from '../../utils/settingsNavigation';
import { connectSoloActive } from '../../utils/settingsSolo';
import { BrowserOpenURL } from '../../../wailsjs/runtime';
import { buildUserRankingURL, systemRankingLabel, useSidebarHubRanking } from './sidebarHubRanking';
import { useSidebarHubAccess } from './useSidebarHubAccess';
import { useInProgressBotTaskCount } from './useInProgressBotTaskCount';
import { desktopBotAccountId } from '../bots/desktopBots';
import { miniAppShortLabel } from '../../i18n/maclawMiniAppLabels';
import type { SidebarHubCheckin } from '../../types/appShell';
import { expertsNavLabel, expertsPageTitle, toolsNavLabel, utilitiesNavLabel, utilitiesPageTitle } from '../../i18n/utilitiesLabels';
import { HubInvitationDialog } from '../HubInvitationDialog';
import { LATEX_TEMPLATES_NAV_TAB } from '../../utils/latexTemplates';
import { localizeText } from '../../i18n';


type SidebarNavRailProps = {
    navTab: string;
    brandInfo: { id: string } | null;
    currentIcon: string;
    brandSidebarName: string;
    switchTool: (tool: string) => void;
    lang: string;
    maclawLLMOnline?: boolean;
    remoteActivationStatus?: any;
    t: (key: string) => string;
    gossipAllowed: boolean;
    config: any;
    favoriteEmployees?: FavoriteEmployeeSlot[];
    veAuthorized?: boolean;
    onStartVEConversation?: (veId: string) => void;
    onReorderFavorites?: (newOrder: string[]) => void;
    onRemoveFavorite?: (veId: string) => void;
    onRenameFavorite?: (veId: string, name: string) => void | Promise<void>;
    showAppEntry?: boolean;
    showUtilitiesEntry?: boolean;
    showToolsEntry?: boolean;
    utilitiesLabel?: string;
    /** Current settings tab, used to highlight the library entry for the Knowledge page. */
    settingsTab?: string;
    /** Tab that asked to hide the settings category rail. Solo IM, email or hardware highlights 连接. */
    settingsNavSoloTab?: string;
    /** Live background-task count. The rail adds in-progress bot tasks itself. */
    runningTaskCount?: number;
    /** Opens System > Monitor with the background-task view selected. */
    onOpenBackgroundTasks?: () => void;
    /** Tenant daily check-in policy; absent/undefined hides the button. */
    hubCheckin?: SidebarHubCheckin;
    /** Performs the daily check-in; resolves after the reward is granted. */
    onHubCheckin?: () => Promise<void> | void;
    /** True while a check-in request is in flight. */
    hubCheckinPending?: boolean;
};

const zhHans = {
    aiAssistant: 'AI \u52a9\u624b',
    system: '\u7cfb\u7edf',
};

const zhHant = {
    aiAssistant: 'AI \u52a9\u624b',
    system: '\u7cfb\u7d71',
};

// Guard anchor: left-nav-item--ai lives in SidebarPrimaryNav.
export const SidebarNavRail = ({
    navTab,
    brandInfo,
    currentIcon,
    brandSidebarName,
    switchTool,
    lang,
    remoteActivationStatus,
    t,
    gossipAllowed,
    config,
    showAppEntry = false,
    showUtilitiesEntry = true,
    showToolsEntry = false,
    utilitiesLabel,
    settingsTab,
    settingsNavSoloTab,
    runningTaskCount = 0,
    onOpenBackgroundTasks,
    hubCheckin,
    onHubCheckin,
    hubCheckinPending = false,
}: SidebarNavRailProps) => {
    const [systemMenuOpen, setSystemMenuOpen] = useState(false);
    const systemMenuOpenerRef = useRef<HTMLElement | null>(null);
    const [extensionsMenuOpen, setExtensionsMenuOpen] = useState(false);
    const [extensionsMenuTop, setExtensionsMenuTop] = useState(0);
    const extensionsMenuOpenerRef = useRef<HTMLElement | null>(null);
    const [libraryMenuOpen, setLibraryMenuOpen] = useState(false);
    const [libraryMenuTop, setLibraryMenuTop] = useState(0);
    const libraryMenuOpenerRef = useRef<HTMLElement | null>(null);
    const [proMenuOpen, setProMenuOpen] = useState(false);
    const [proMenuTop, setProMenuTop] = useState(0);
    const proMenuOpenerRef = useRef<HTMLElement | null>(null);
    const [connectMenuOpen, setConnectMenuOpen] = useState(false);
    const [connectMenuTop, setConnectMenuTop] = useState(0);
    const connectMenuOpenerRef = useRef<HTMLElement | null>(null);
    const rankingURL = buildUserRankingURL(config?.remote_hub_url || '', config?.remote_tenant_id);
    const showRanking = config?.show_hub_ranking !== false && !!rankingURL;
    const ranking = useSidebarHubRanking(showRanking, !!remoteActivationStatus?.activated);
    const rankingLabel = systemRankingLabel(lang, ranking, t('ranking'));
    const { invitationEnabled, invitationDialogOpen, setInvitationDialogOpen, openBots, botAllowed } = useSidebarHubAccess({
        activated: !!remoteActivationStatus?.activated,
        navTab,
        switchTool,
        remoteHubUrl: config?.remote_hub_url,
        remoteViewerToken: config?.remote_viewer_token,
        remoteTenantId: config?.remote_tenant_id,
    });
    const text = (en: string, zhHansText: string, zhHantText: string = zhHansText) => localizeText(lang, en, zhHansText, zhHantText);
    const aiAssistantLabel = text('AI Asst', zhHans.aiAssistant, zhHant.aiAssistant);
    const appsLabel = miniAppShortLabel(lang);
    const workflowLabel = text('Workflows', '工作流', '工作流');
    const showWorkflowEntry = config?.show_workflow_entry !== false;
    const proFeaturesLabel = text('Features', '专业功能', '專業功能');
    const expertsMenuLabel = expertsNavLabel(lang);
    const resolvedUtilitiesLabel = utilitiesLabel || (showToolsEntry ? expertsNavLabel(lang) : utilitiesNavLabel(lang));
    const resolvedUtilitiesTitle = showToolsEntry ? expertsPageTitle(lang) : utilitiesPageTitle(lang);
    const resolvedToolsLabel = toolsNavLabel(lang);
    const systemLabel = text('System', zhHans.system, zhHant.system);
    // Background runs plus bot tasks still in progress. Scheduled tasks and
    // passthrough commands stay off this badge; they are not running.
    const botTaskCount = useInProgressBotTaskCount(desktopBotAccountId(config), botAllowed);
    const runningTaskTotal = Math.max(0, Math.trunc(Number(runningTaskCount) || 0)) + botTaskCount;
    const extensionsLabel = text('Extensions', '扩展', '擴展');
    // Check-in copy: highlighted (amber) until today's reward is claimed, then gray.
    const checkinLabel = text('Check-in', '签到', '簽到');
    const checkinCredits = Math.round((Number(hubCheckin?.credits) || 0) * 100) / 100;
    const checkinDays = Math.max(0, Math.trunc(Number(hubCheckin?.durationDays) || 0));
    const checkinTitle = hubCheckin?.checkedInToday
        ? text('Checked in today', '今日已签到', '今日已簽到')
        : checkinDays > 0
            ? text('Check in for {credits} credits, valid for {days} days', '签到领 {credits} 积分，{days} 天内有效', '簽到領 {credits} 積分，{days} 天內有效').replaceAll('{credits}', String(checkinCredits)).replaceAll('{days}', String(checkinDays))
            : text('Check in for {credits} credits', '签到领 {credits} 积分', '簽到領 {credits} 積分').replaceAll('{credits}', String(checkinCredits));
    const inviteLabel = text('Referral', '推荐', '推薦');
    const inviteTitle = text('Refer friends', '推荐好友注册', '推薦好友註冊');
    const botTitle = text('A bot is a digital assistant stationed on the remote computer around the clock.', 'Bot为24小时驻守远程电脑上的数字助理。', 'Bot為24小時駐守遠端電腦上的數位助理。');
    const connectorsLabel = text('Connectors', '连接器', '連接器');
    const connectLabel = text('Connect', '连接', '連接');
    const connectTitle = text('Connect chat, email and hardware', '连接即时通讯、邮箱和硬件', '連接即時通訊、郵箱和硬體');
    const connectEmailLabel = text('Email', '邮箱', '郵箱');
    const connectHardwareLabel = text('Hardware', '硬件', '硬體');
    const connectSolo = connectSoloActive(navTab, settingsNavSoloTab || '');
    const libraryLabel = text('Library', '资料库', '資料庫');
    const mobileDocsLabel = text('Cloud drive', '云盘', '雲端硬碟');
    const knowledgeLabel = text('Knowledge base', '知识库', '知識庫');
    const latexTemplatesLabel = text('LaTeX templates', 'Latex模板', 'Latex 模板');
    const knowledgeActive = navTab === 'settings' && settingsTab === 'knowledge';
    // The LaTeX template library is one of the library entries, so the rail item
    // stays highlighted while the page is open.
    const latexTemplatesActive = navTab === LATEX_TEMPLATES_NAV_TAB;
    const systemPageActive = navTab === 'about' || (navTab === 'gossip' && gossipAllowed);
    const systemActiveId = systemPageActive ? navTab : undefined;
    const extensionsActiveId = navTab === 'skills' || navTab === 'mcp' ? navTab : undefined;
    const libraryActiveId = navTab === 'files' ? 'documents' : knowledgeActive ? 'knowledge' : latexTemplatesActive ? LATEX_TEMPLATES_NAV_TAB : undefined;
    const systemMenuItems: SystemMenuItem[] = [
        { id: 'about', icon: <AboutIcon />, label: t('about'), visible: true },
        { id: 'gossip', icon: <GossipIcon />, label: t('gossip'), visible: gossipAllowed },
        { id: 'ranking', icon: <RankingIcon />, label: rankingLabel, visible: showRanking },
    ];
    const selectSystemMenuItem = (id: string) => {
        if (id === 'ranking') {
            if (rankingURL) BrowserOpenURL(rankingURL);
            return;
        }
        switchTool(id);
    };
    const extensionsMenuItems: SystemMenuItem[] = [
        { id: 'skills', icon: <SkillsIcon />, label: t('skills'), visible: true },
        { id: 'mcp', icon: <MCPIcon />, label: connectorsLabel, visible: true },
    ];
    const libraryMenuItems: SystemMenuItem[] = [
        { id: 'documents', icon: <MobileDocsIcon />, label: mobileDocsLabel, visible: true },
        { id: 'knowledge', icon: <KnowledgeIcon />, label: knowledgeLabel, visible: true },
        { id: LATEX_TEMPLATES_NAV_TAB, icon: <LatexTemplateIcon />, label: latexTemplatesLabel, visible: true },
    ];
    const proTabs = visibleProFeatureTabs({ showUtilitiesEntry, showWorkflowEntry, showToolsEntry });
    const proMenuByTab: Record<ProFeatureTab, SystemMenuItem> = {
        utilities: { id: 'utilities', icon: <ExpertRailIcon marked={false} />, label: expertsMenuLabel, visible: true },
        workflows: { id: 'workflows', icon: <WorkflowIcon />, label: workflowLabel, visible: true },
        tools: { id: 'tools', icon: <ToolsRailIcon />, label: resolvedToolsLabel, visible: true },
    };
    const proMenuItems = proTabs.map(id => proMenuByTab[id]);
    const { closeSiblingMenus, toggleSystemMenu, toggleExtensionsMenu, toggleLibraryMenu, toggleProMenu, toggleConnectMenu } = useNavMenuToggles({
        systemMenuOpen, extensionsMenuOpen, libraryMenuOpen, proMenuOpen, connectMenuOpen,
        systemMenuOpenerRef, extensionsMenuOpenerRef, libraryMenuOpenerRef, proMenuOpenerRef, connectMenuOpenerRef,
        setSystemMenuOpen, setExtensionsMenuOpen, setLibraryMenuOpen, setProMenuOpen, setConnectMenuOpen,
        setExtensionsMenuTop, setLibraryMenuTop, setProMenuTop, setConnectMenuTop,
    });
    useEffect(() => {
        // Split mode is what turns this rail entry into a menu. One remaining
        // destination opens directly, so a one-item popup must not stay up.
        if ((!showToolsEntry || proTabs.length < 2) && proMenuOpen) setProMenuOpen(false);
    }, [showToolsEntry, proTabs.length, proMenuOpen]);
    // The popups are position fixed and stay at their open-time viewport spot;
    // scrolling the nav content under an open menu would leave it detached
    // from its trigger, so any scroll dismisses the menus.
    const handleRailScroll = useCallback(() => {
        closeSiblingMenus();
    }, [closeSiblingMenus]);
    const selectLibraryMenuItem = (id: string) => {
        if (id === 'knowledge') {
            openSettingsTab('knowledge');
            return;
        }
        if (id === LATEX_TEMPLATES_NAV_TAB) {
            switchTool(LATEX_TEMPLATES_NAV_TAB);
            return;
        }
        switchTool('files');
    };
    const connectMenuItems: SystemMenuItem[] = [
        { id: 'im', icon: <ConnectMenuIMIcon />, label: 'IM', visible: true },
        { id: 'email', icon: <ConnectMenuMailIcon />, label: connectEmailLabel, visible: true },
        { id: 'hardware', icon: <ConnectMenuHardwareIcon />, label: connectHardwareLabel, visible: true },
    ];
    const selectConnectMenuItem = (id: string) => {
        openSettingsTab(id === 'email' ? 'email' : id === 'hardware' ? 'hardware' : 'im', { hideSettingsNav: true });
    };
    const connectActiveId = navTab === 'settings' && (settingsTab === 'im' || settingsTab === 'email' || settingsTab === 'hardware') ? settingsTab : undefined;
    return (
        <div className="mc-nav-rail" style={{
            width: `${SIDEBAR_NAV_RAIL_WIDTH}px`,
            borderRight: '1px solid var(--theme-border)',
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            padding: '6px 0',
            background: 'var(--theme-page-bg)',
            flexShrink: 0,
            position: 'relative',
        }}>
            <SidebarBrandHeader brandId={brandInfo?.id} currentIcon={currentIcon} brandSidebarName={brandSidebarName} />
            {/* Scrollable middle: when the window is too short for the full
                stack, this wrapper scrolls instead of letting flex crush the
                items (which clipped the labels in half). The brand above and
                the system trigger + popup menus below stay outside so they are
                never clipped by the scroll container. */}
            <div className="mc-nav-rail__scroll" onScroll={handleRailScroll}>
                <SidebarPrimaryNav lang={lang} navTab={navTab} aiAssistantLabel={aiAssistantLabel} appsLabel={appsLabel} showAppEntry={showAppEntry} showUtilitiesEntry={showUtilitiesEntry} showToolsEntry={showToolsEntry} switchTool={switchTool} extensionsLabel={extensionsLabel} extensionsMenuOpen={extensionsMenuOpen} onToggleExtensionsMenu={toggleExtensionsMenu} libraryMenuOpen={libraryMenuOpen} onToggleLibraryMenu={toggleLibraryMenu} knowledgeActive={knowledgeActive} latexTemplatesActive={latexTemplatesActive} workflowLabel={workflowLabel} utilitiesLabel={resolvedUtilitiesLabel} utilitiesTitle={resolvedUtilitiesTitle} runningTaskCount={runningTaskTotal} onOpenBackgroundTasks={onOpenBackgroundTasks} proMenuOpen={proMenuOpen} onToggleProMenu={toggleProMenu} proFeaturesLabel={proFeaturesLabel} showWorkflowEntry={showWorkflowEntry} connectLabel={connectLabel} connectTitle={connectTitle} connectMenuOpen={connectMenuOpen} onToggleConnectMenu={toggleConnectMenu} connectSoloActive={connectSolo} />
                <div className="snr-spacer" />
                {hubCheckin?.enabled && (
                    <button
                        type="button"
                        className={'sidebar-item left-nav-item left-nav-item--checkin' + (hubCheckin.checkedInToday ? ' checked-in' : '')}
                        data-testid="sidebar-checkin-nav"
                        aria-label={checkinTitle}
                        title={checkinTitle}
                        disabled={hubCheckin.checkedInToday || hubCheckinPending || !onHubCheckin}
                        onClick={() => { void onHubCheckin?.(); }}
                    >
                        <span className="sidebar-icon"><span className={'checkin-nav-icon-badge' + (hubCheckin.checkedInToday ? ' checkin-nav-icon-badge--done' : '')}><CheckinRailIcon /></span></span>
                        <span className="bot-nav-label">{checkinLabel}</span>
                    </button>
                )}
                {botAllowed && (
                    <button
                        type="button"
                        className={'sidebar-item left-nav-item left-nav-item--bot' + (navTab === 'bots' ? ' active' : '')}
                        data-testid="sidebar-bot-nav"
                        aria-label="Bot"
                        aria-current={navTab === 'bots' ? 'page' : undefined}
                        title={botTitle}
                        onClick={openBots}
                    >
                        <span className="sidebar-icon"><span className="bot-nav-icon-badge"><BotRailIcon /></span></span>
                        <span className="bot-nav-label">Bot</span>
                    </button>
                )}
                {invitationEnabled && (
                    <button
                        type="button"
                        className="sidebar-item left-nav-item left-nav-item--invite"
                        data-testid="sidebar-invite-nav"
                        aria-label={inviteTitle}
                        title={inviteTitle}
                        onClick={() => setInvitationDialogOpen(true)}
                    >
                        <span className="sidebar-icon"><span className="invite-nav-icon-badge"><InviteGiftIcon /></span></span>
                        <span className="bot-nav-label">{inviteLabel}</span>
                    </button>
                )}
            </div>
            <div className="mc-legacy-rail-footer">
                <div
                    className={'sidebar-item left-nav-item ' + (systemMenuOpen || systemPageActive ? 'active' : '')}
                    role="button" tabIndex={0} aria-haspopup="menu" aria-expanded={systemMenuOpen} aria-controls="system-popup-menu"
                    aria-current={systemPageActive ? 'page' : undefined}
                    onClick={event => toggleSystemMenu(event.currentTarget)}
                    onKeyDown={event => { if (event.key !== 'Enter' && event.key !== ' ') return; event.preventDefault(); toggleSystemMenu(event.currentTarget); }}
                    style={{ flexDirection: 'column', padding: '5px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: '1px solid transparent', boxShadow: systemMenuOpen || systemPageActive ? 'inset -1px 0 0 var(--theme-text-muted)' : 'none', justifyContent: 'center' }}
                    title={systemLabel}
                >
                    <span className="sidebar-icon" style={{ margin: 0, display: 'inline-flex', color: systemMenuOpen || systemPageActive ? 'var(--theme-primary)' : 'var(--theme-text-primary)' }}><SystemIcon /></span>
                    <span className="snr-system-label">{systemLabel}</span>
                </div>
            </div>
            <button type="button" role="button" className="mc-profile-rail" data-testid="system-menu-trigger" aria-label={text('System menu', '系统菜单', '系統選單')} title={text('System menu', '系统菜单', '系統選單')} aria-haspopup="menu" aria-expanded={systemMenuOpen} aria-controls="system-popup-menu" aria-current={systemPageActive ? 'page' : undefined} onClick={event => toggleSystemMenu(event.currentTarget)} onKeyDown={event => { if (event.key !== 'Enter' && event.key !== ' ') return; event.preventDefault(); toggleSystemMenu(event.currentTarget); }}>
                <span className="mc-profile-rail__avatar mc-profile-rail__avatar--system" aria-hidden="true"><SystemIcon /></span>
            </button>
            {systemMenuOpen && (
                <SystemPopupMenu
                    items={systemMenuItems}
                    onSelect={selectSystemMenuItem}
                    onClose={() => setSystemMenuOpen(false)}
                    returnFocus={() => systemMenuOpenerRef.current}
                    ariaLabel={systemLabel}
                    activeId={systemActiveId}
                />
            )}
            {extensionsMenuOpen && (
                <SystemPopupMenu
                    items={extensionsMenuItems}
                    onSelect={switchTool}
                    onClose={() => setExtensionsMenuOpen(false)}
                    returnFocus={() => extensionsMenuOpenerRef.current}
                    ariaLabel={extensionsLabel}
                    anchorTop={extensionsMenuTop}
                    excludeTriggerSelector='[data-testid="sidebar-extensions-nav"]'
                    menuId="extensions-popup-menu"
                    testIdPrefix="extensions-menu"
                    activeId={extensionsActiveId}
                />
            )}
            {libraryMenuOpen && (
                <SystemPopupMenu
                    items={libraryMenuItems}
                    onSelect={selectLibraryMenuItem}
                    onClose={() => setLibraryMenuOpen(false)}
                    returnFocus={() => libraryMenuOpenerRef.current}
                    ariaLabel={libraryLabel}
                    anchorTop={libraryMenuTop}
                    excludeTriggerSelector='[data-testid="sidebar-files-nav"]'
                    menuId="library-popup-menu"
                    testIdPrefix="library-menu"
                    activeId={libraryActiveId}
                />
            )}
            {connectMenuOpen && (
                <SystemPopupMenu
                    items={connectMenuItems}
                    onSelect={selectConnectMenuItem}
                    onClose={() => setConnectMenuOpen(false)}
                    returnFocus={() => connectMenuOpenerRef.current}
                    ariaLabel={connectTitle}
                    anchorTop={connectMenuTop}
                    excludeTriggerSelector='[data-testid="sidebar-connect-nav"]'
                    menuId="connect-popup-menu"
                    testIdPrefix="connect-menu"
                    activeId={connectActiveId}
                />
            )}
            {proMenuOpen && (
                <SystemPopupMenu
                    items={proMenuItems}
                    onSelect={(id) => switchTool(id)}
                    onClose={() => setProMenuOpen(false)}
                    returnFocus={() => proMenuOpenerRef.current}
                    ariaLabel={proFeaturesLabel}
                    activeId={(proTabs as readonly string[]).includes(navTab) ? navTab : undefined}
                    anchorTop={proMenuTop}
                    excludeTriggerSelector='[data-testid="sidebar-utilities-nav"]'
                    menuId="pro-features-popup-menu"
                    testIdPrefix="pro-features-menu"
                />
            )}
            <HubInvitationDialog open={invitationDialogOpen} onClose={() => setInvitationDialogOpen(false)} lang={lang} />
        </div>
    );
};
