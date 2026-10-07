import { useState, useEffect, useRef } from 'react';
import { SIDEBAR_NAV_RAIL_WIDTH } from './sidebarLayout';
import { SystemPopupMenu, type SystemMenuItem } from './SystemPopupMenu';
import type { FavoriteEmployeeSlot } from './FavoriteEmployeeButtons';
import { BotRailIcon, SystemIcon, AboutIcon, SkillsIcon, MCPIcon, GossipIcon, RankingIcon, MobileDocsIcon, KnowledgeIcon, LatexTemplateIcon, ExpertRailIcon, WorkflowIcon, InviteGiftIcon, CheckinRailIcon } from './SidebarNavIcons';
import { SidebarBrandHeader, SidebarPrimaryNav, useNavMenuToggles } from './SidebarNavRailPieces';
import { openSettingsTab } from '../../utils/settingsNavigation';
import { BrowserOpenURL } from '../../../wailsjs/runtime';
import { buildUserRankingURL, systemRankingLabel, useSidebarHubRanking } from './sidebarHubRanking';
import { useSidebarHubAccess } from './useSidebarHubAccess';
import { miniAppShortLabel } from '../../i18n/maclawMiniAppLabels';
import type { SidebarHubCheckin } from '../../types/appShell';
import { expertsNavLabel, expertsPageTitle, toolsNavLabel, toolsPageTitle, utilitiesNavLabel, utilitiesPageTitle } from '../../i18n/utilitiesLabels';
import { HubInvitationDialog } from '../HubInvitationDialog';
import { LATEX_TEMPLATES_NAV_TAB } from '../../utils/latexTemplates';

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
    /** Live running-task count, mirrored from the workbench status card. */
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
    const aiAssistantLabel = lang === 'zh-Hans' ? zhHans.aiAssistant : lang === 'zh-Hant' ? zhHant.aiAssistant : 'AI Asst';
    const appsLabel = miniAppShortLabel(lang);
    const workflowLabel = lang === 'zh-Hans' ? '工作流' : lang === 'zh-Hant' ? '工作流' : 'Workflows';
    const showWorkflowEntry = config?.show_workflow_entry !== false;
    const proFeaturesLabel = lang === 'zh-Hans' ? '专业功能' : lang === 'zh-Hant' ? '專業功能' : 'Features';
    const expertsMenuLabel = expertsNavLabel(lang);
    const resolvedUtilitiesLabel = utilitiesLabel || (showToolsEntry ? expertsNavLabel(lang) : utilitiesNavLabel(lang));
    const resolvedUtilitiesTitle = showToolsEntry ? expertsPageTitle(lang) : utilitiesPageTitle(lang);
    const resolvedToolsLabel = toolsNavLabel(lang);
    const resolvedToolsTitle = toolsPageTitle(lang);
    const systemLabel = lang === 'zh-Hans' ? zhHans.system : lang === 'zh-Hant' ? zhHant.system : 'System';
    // The workbench status card owns the detailed breakdown (see
    // WorkbenchTaskCounts); the rail only mirrors the running total as a badge on
    // the Tasks entry, so the two readouts can never disagree.
    const runningTaskTotal = Math.max(0, Math.trunc(Number(runningTaskCount) || 0));
    const extensionsLabel = lang === 'zh-Hans' ? '扩展' : lang === 'zh-Hant' ? '擴展' : 'Extensions';
    // Check-in copy: highlighted (amber) until today's reward is claimed, then gray.
    const checkinLabel = lang === 'zh-Hans' ? '签到' : lang === 'zh-Hant' ? '簽到' : 'Check-in';
    const checkinCredits = Math.round((Number(hubCheckin?.credits) || 0) * 100) / 100;
    const checkinTitle = hubCheckin?.checkedInToday
        ? (lang === 'zh-Hans' ? '今日已签到' : lang === 'zh-Hant' ? '今日已簽到' : 'Checked in today')
        : (lang === 'zh-Hans' ? `签到领 ${checkinCredits} 积分` : lang === 'zh-Hant' ? `簽到領 ${checkinCredits} 積分` : `Check in for ${checkinCredits} credits`);
    const inviteLabel = lang === 'zh-Hans' ? '推荐' : lang === 'zh-Hant' ? '推薦' : 'Referral';
    const inviteTitle = lang === 'zh-Hans' ? '推荐好友注册' : lang === 'zh-Hant' ? '推薦好友註冊' : 'Refer friends';
    const connectorsLabel = lang === 'zh-Hans' ? '连接器' : lang === 'zh-Hant' ? '連接器' : 'Connectors';
    const libraryLabel = lang === 'zh-Hans' ? '资料库' : lang === 'zh-Hant' ? '資料庫' : 'Library';
    const mobileDocsLabel = lang === 'zh-Hans' ? '云盘' : lang === 'zh-Hant' ? '雲端硬碟' : 'Cloud drive';
    const knowledgeLabel = lang === 'zh-Hans' ? '知识库' : lang === 'zh-Hant' ? '知識庫' : 'Knowledge base';
    const latexTemplatesLabel = lang === 'zh-Hans' ? 'Latex模板' : lang === 'zh-Hant' ? 'Latex 模板' : 'LaTeX templates';
    const knowledgeActive = navTab === 'settings' && settingsTab === 'knowledge';
    // The LaTeX template library is one of the library entries, so the rail item
    // stays highlighted while the page is open.
    const latexTemplatesActive = navTab === LATEX_TEMPLATES_NAV_TAB;
    const systemPageActive = navTab === 'about' || (navTab === 'gossip' && gossipAllowed);
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
    const proMenuItems: SystemMenuItem[] = [
        { id: 'utilities', icon: <ExpertRailIcon marked={false} />, label: expertsMenuLabel, visible: showUtilitiesEntry },
        { id: 'workflows', icon: <WorkflowIcon />, label: workflowLabel, visible: showWorkflowEntry },
    ];
    const { closeSiblingMenus, toggleSystemMenu, toggleExtensionsMenu, toggleLibraryMenu, toggleProMenu } = useNavMenuToggles({
        systemMenuOpen, extensionsMenuOpen, libraryMenuOpen, proMenuOpen,
        systemMenuOpenerRef, extensionsMenuOpenerRef, libraryMenuOpenerRef, proMenuOpenerRef,
        setSystemMenuOpen, setExtensionsMenuOpen, setLibraryMenuOpen, setProMenuOpen,
        setExtensionsMenuTop, setLibraryMenuTop, setProMenuTop,
    });
    const proMenuChoices = (showUtilitiesEntry ? 1 : 0) + (showWorkflowEntry ? 1 : 0);
    useEffect(() => {
        if ((!showToolsEntry || proMenuChoices < 2) && proMenuOpen) setProMenuOpen(false);
    }, [showToolsEntry, proMenuChoices, proMenuOpen]);
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
            <SidebarPrimaryNav navTab={navTab} aiAssistantLabel={aiAssistantLabel} appsLabel={appsLabel} showAppEntry={showAppEntry} showUtilitiesEntry={showUtilitiesEntry} showToolsEntry={showToolsEntry} switchTool={switchTool} extensionsLabel={extensionsLabel} extensionsMenuOpen={extensionsMenuOpen} onToggleExtensionsMenu={toggleExtensionsMenu} libraryMenuOpen={libraryMenuOpen} onToggleLibraryMenu={toggleLibraryMenu} knowledgeActive={knowledgeActive} latexTemplatesActive={latexTemplatesActive} workflowLabel={workflowLabel} utilitiesLabel={resolvedUtilitiesLabel} utilitiesTitle={resolvedUtilitiesTitle} toolsLabel={resolvedToolsLabel} toolsTitle={resolvedToolsTitle} runningTaskCount={runningTaskTotal} onOpenBackgroundTasks={onOpenBackgroundTasks} proMenuOpen={proMenuOpen} onToggleProMenu={toggleProMenu} proFeaturesLabel={proFeaturesLabel} showWorkflowEntry={showWorkflowEntry} />
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
                    title={lang === 'en' ? 'Bot. Each bot is an instance of this MaClawSrv user. They share this user\'s cloud desktop.' : 'Bot。每个 Bot 是当前用户在 MaClawSrv 上的一个实例，共用云端桌面。'}
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
            <button type="button" role="button" className="mc-profile-rail" data-testid="system-menu-trigger" aria-label={lang === 'en' ? 'System menu' : lang === 'zh-Hant' ? '系統選單' : '系统菜单'} title={lang === 'en' ? 'System menu' : lang === 'zh-Hant' ? '系統選單' : '系统菜单'} aria-haspopup="menu" aria-expanded={systemMenuOpen} aria-controls="system-popup-menu" aria-current={systemPageActive ? 'page' : undefined} onClick={event => toggleSystemMenu(event.currentTarget)} onKeyDown={event => { if (event.key !== 'Enter' && event.key !== ' ') return; event.preventDefault(); toggleSystemMenu(event.currentTarget); }}>
                <span className="mc-profile-rail__avatar mc-profile-rail__avatar--system" aria-hidden="true"><SystemIcon /></span>
            </button>
            {systemMenuOpen && (
                <SystemPopupMenu
                    items={systemMenuItems}
                    onSelect={selectSystemMenuItem}
                    onClose={() => setSystemMenuOpen(false)}
                    returnFocus={() => systemMenuOpenerRef.current}
                    ariaLabel={systemLabel}
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
                />
            )}
            {proMenuOpen && (
                <SystemPopupMenu
                    items={proMenuItems}
                    onSelect={(id) => switchTool(id)}
                    onClose={() => setProMenuOpen(false)}
                    returnFocus={() => proMenuOpenerRef.current}
                    ariaLabel={proFeaturesLabel}
                    activeId={navTab === 'utilities' || navTab === 'workflows' ? navTab : undefined}
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
