import { useState, useEffect, useRef } from 'react';
import { SIDEBAR_NAV_RAIL_WIDTH } from './sidebarLayout';
import { SystemPopupMenu, type SystemMenuItem } from './SystemPopupMenu';
import { FavoriteEmployeeButtons, type FavoriteEmployeeSlot } from './FavoriteEmployeeButtons';
import { SystemIcon, AboutIcon, SkillsIcon, MCPIcon, GossipIcon, RankingIcon, MobileDocsIcon, KnowledgeIcon } from './SidebarNavIcons';
import { SidebarBrandHeader, SidebarPrimaryNav } from './SidebarNavRailPieces';
import { openSettingsTab } from '../../utils/settingsNavigation';
import { GetHubUserInvitationStatus } from '../../../wailsjs/go/main/App';
import { BrowserOpenURL } from '../../../wailsjs/runtime';
import { systemRankingLabel, useSidebarHubRanking } from './sidebarHubRanking';
import { miniAppShortLabel } from '../../i18n/maclawMiniAppLabels';
import { expertsNavLabel, expertsPageTitle, toolsNavLabel, toolsPageTitle, utilitiesNavLabel, utilitiesPageTitle } from '../../i18n/utilitiesLabels';
import { HubInvitationDialog } from '../HubInvitationDialog';

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
};

const HUB_INVITATION_STATUS_REFRESH_INTERVAL_MS = 30_000;

function InviteGiftIcon() {
    return <svg width="19" height="19" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M20 12v7a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1v-7"/><path d="M2 8h20v4H2z"/><path d="M12 8v12"/><path d="M12 8H7.5a2.5 2.5 0 1 1 2.5-2.5V8"/><path d="M12 8h4.5A2.5 2.5 0 1 0 14 5.5V8"/></svg>;
}

const zhHans = {
    aiAssistant: 'AI \u52a9\u624b',
    system: '\u7cfb\u7edf',
};

const zhHant = {
    aiAssistant: 'AI \u52a9\u624b',
    system: '\u7cfb\u7d71',
};

function buildUserRankingURL(hubURL: string, tenantID?: string) {
    const base = (hubURL || '').replace(/\/+$/, '');
    if (!base) return '';
    try {
        const url = new URL(base + '/user-ranking');
        const tid = String(tenantID || '').trim();
        if (tid) url.searchParams.set('tenant_id', tid);
        return url.toString();
    } catch {
        return '';
    }
}

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
    favoriteEmployees = [],
    veAuthorized = false,
    onStartVEConversation = () => {},
    onReorderFavorites = () => {},
    onRemoveFavorite = () => {},
    onRenameFavorite = () => {},
    showAppEntry = false,
    showUtilitiesEntry = true,
    showToolsEntry = false,
    utilitiesLabel,
    settingsTab,
}: SidebarNavRailProps) => {
    const [systemMenuOpen, setSystemMenuOpen] = useState(false);
    const systemMenuOpenerRef = useRef<HTMLElement | null>(null);
    const [extensionsMenuOpen, setExtensionsMenuOpen] = useState(false);
    const [extensionsMenuTop, setExtensionsMenuTop] = useState(0);
    const extensionsMenuOpenerRef = useRef<HTMLElement | null>(null);
    const [libraryMenuOpen, setLibraryMenuOpen] = useState(false);
    const [libraryMenuTop, setLibraryMenuTop] = useState(0);
    const libraryMenuOpenerRef = useRef<HTMLElement | null>(null);
    const [invitationEnabled, setInvitationEnabled] = useState(false);
    const [invitationDialogOpen, setInvitationDialogOpen] = useState(false);
    const rankingURL = buildUserRankingURL(config?.remote_hub_url || '', config?.remote_tenant_id);
    const showRanking = config?.show_hub_ranking !== false && !!rankingURL;
    const ranking = useSidebarHubRanking(showRanking, !!remoteActivationStatus?.activated);
    const rankingLabel = systemRankingLabel(lang, ranking, t('ranking'));
    const invitationRequestSeqRef = useRef(0);

    // The server is authoritative: a disabled tenant deliberately renders no
    // invitation button or separator, rather than a disabled-looking control.
    // Hub administrators can change the switch while MaClaw is open, so refresh
    // when the window returns to the foreground and on a short interval.
    useEffect(() => {
        if (!remoteActivationStatus?.activated) {
            invitationRequestSeqRef.current += 1;
            setInvitationEnabled(false);
            setInvitationDialogOpen(false);
            return;
        }
        let cancelled = false;
        const refreshInvitationStatus = () => {
            // A foreground event performs an immediate refresh, so polling
            // while the desktop app is hidden only wastes Hub requests.
            if (document.visibilityState === 'hidden') return;
            const requestSeq = ++invitationRequestSeqRef.current;
            GetHubUserInvitationStatus().then((result: { enabled?: boolean; error?: string } | null) => {
                if (cancelled || requestSeq !== invitationRequestSeqRef.current) return;
                const enabled = !!result?.enabled && !result?.error;
                setInvitationEnabled(enabled);
                if (!enabled) setInvitationDialogOpen(false);
            }).catch(() => {
                if (cancelled || requestSeq !== invitationRequestSeqRef.current) return;
                setInvitationEnabled(false);
                setInvitationDialogOpen(false);
            });
        };
        const onVisibilityChange = () => {
            if (document.visibilityState === 'visible') refreshInvitationStatus();
        };
        refreshInvitationStatus();
        const interval = window.setInterval(refreshInvitationStatus, HUB_INVITATION_STATUS_REFRESH_INTERVAL_MS);
        document.addEventListener('visibilitychange', onVisibilityChange);
        return () => {
            cancelled = true;
            window.clearInterval(interval);
            document.removeEventListener('visibilitychange', onVisibilityChange);
        };
    }, [remoteActivationStatus?.activated, config?.remote_hub_url, config?.remote_viewer_token, config?.remote_tenant_id]);
    const aiAssistantLabel = lang === 'zh-Hans' ? zhHans.aiAssistant : lang === 'zh-Hant' ? zhHant.aiAssistant : 'AI Asst';
    const appsLabel = miniAppShortLabel(lang);
    const workflowLabel = lang === 'zh-Hans' ? '工作流' : lang === 'zh-Hant' ? '工作流' : 'Workflow';
    const resolvedUtilitiesLabel = utilitiesLabel || (showToolsEntry ? expertsNavLabel(lang) : utilitiesNavLabel(lang));
    const resolvedUtilitiesTitle = showToolsEntry ? expertsPageTitle(lang) : utilitiesPageTitle(lang);
    const resolvedToolsLabel = toolsNavLabel(lang);
    const resolvedToolsTitle = toolsPageTitle(lang);
    const systemLabel = lang === 'zh-Hans' ? zhHans.system : lang === 'zh-Hant' ? zhHant.system : 'System';
    // The running-task badge moved to SidebarSystemStatus (see WorkbenchTaskCounts);
    // the rail keeps an explicit 0-count tap target so the nav item stays stable.
    const runningTaskCount = 0;
    const extensionsLabel = lang === 'zh-Hans' ? '扩展' : lang === 'zh-Hant' ? '擴展' : 'Extensions';
    const connectorsLabel = lang === 'zh-Hans' ? '连接器' : lang === 'zh-Hant' ? '連接器' : 'Connectors';
    const libraryLabel = lang === 'zh-Hans' ? '资料库' : lang === 'zh-Hant' ? '資料庫' : 'Library';
    const mobileDocsLabel = lang === 'zh-Hans' ? '移动文稿库' : lang === 'zh-Hant' ? '行動文稿庫' : 'Mobile documents';
    const knowledgeLabel = lang === 'zh-Hans' ? '知识库' : lang === 'zh-Hant' ? '知識庫' : 'Knowledge base';
    const knowledgeActive = navTab === 'settings' && settingsTab === 'knowledge';
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
    ];
    const toggleSystemMenu = (target: HTMLElement) => {
        if (!systemMenuOpen) {
            systemMenuOpenerRef.current = target;
            setExtensionsMenuOpen(false);
            setLibraryMenuOpen(false);
        }
        setSystemMenuOpen(prev => !prev);
    };
    const toggleExtensionsMenu = (target: HTMLElement) => {
        if (!extensionsMenuOpen) {
            extensionsMenuOpenerRef.current = target;
            setExtensionsMenuTop(target.offsetTop + target.offsetHeight / 2);
            setSystemMenuOpen(false);
            setLibraryMenuOpen(false);
        }
        setExtensionsMenuOpen(prev => !prev);
    };
    const toggleLibraryMenu = (target: HTMLElement) => {
        if (!libraryMenuOpen) {
            libraryMenuOpenerRef.current = target;
            setLibraryMenuTop(target.offsetTop + target.offsetHeight / 2);
            setSystemMenuOpen(false);
            setExtensionsMenuOpen(false);
        }
        setLibraryMenuOpen(prev => !prev);
    };
    const selectLibraryMenuItem = (id: string) => {
        if (id === 'knowledge') {
            openSettingsTab('knowledge');
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
            <SidebarPrimaryNav navTab={navTab} aiAssistantLabel={aiAssistantLabel} appsLabel={appsLabel} showAppEntry={showAppEntry} showUtilitiesEntry={showUtilitiesEntry} showToolsEntry={showToolsEntry} switchTool={switchTool} extensionsLabel={extensionsLabel} extensionsMenuOpen={extensionsMenuOpen} onToggleExtensionsMenu={toggleExtensionsMenu} libraryMenuOpen={libraryMenuOpen} onToggleLibraryMenu={toggleLibraryMenu} knowledgeActive={knowledgeActive} workflowLabel={workflowLabel} utilitiesLabel={resolvedUtilitiesLabel} utilitiesTitle={resolvedUtilitiesTitle} toolsLabel={resolvedToolsLabel} toolsTitle={resolvedToolsTitle} />
            {showAppEntry && veAuthorized && favoriteEmployees.length > 0 && (
                <div
                    aria-hidden="true"
                    style={{
                        width: '60%',
                        height: '1px',
                        margin: '6px 0',
                        background: 'linear-gradient(90deg, transparent 0%, var(--theme-border) 25%, var(--theme-text-muted) 50%, var(--theme-border) 75%, transparent 100%)',
                        opacity: 0.4,
                    }}
                />
            )}
            <FavoriteEmployeeButtons
                slots={favoriteEmployees}
                veAuthorized={veAuthorized}
                onStartConversation={(veId) => {
                    switchTool('ai');
                    onStartVEConversation(veId);
                }}
                onReorder={onReorderFavorites}
                onRemove={onRemoveFavorite}
                onRename={onRenameFavorite}
                lang={lang}
            />
            <div style={{ flex: 1 }} />
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
                    <span style={{ fontSize: '0.72rem', lineHeight: 1, fontWeight: 700 }}>{systemLabel}</span>
                </div>
                {invitationEnabled && (
                    <>
                        <div aria-hidden="true" style={{ width: '60%', height: 1, margin: '3px 0', background: 'var(--theme-border)', opacity: .7 }} />
                        <button
                            type="button"
                            className="sidebar-item left-nav-item"
                            onClick={() => setInvitationDialogOpen(true)}
                            title={lang === 'zh-Hans' ? '邀请好友' : lang === 'zh-Hant' ? '邀請好友' : 'Invite friends'}
                            style={{ flexDirection: 'column', padding: '5px 0', width: '100%', gap: '2px', border: 'none', background: 'transparent', color: 'var(--theme-primary)', cursor: 'pointer', position: 'relative' }}
                        >
                            <span className="sidebar-icon" style={{ margin: 0, display: 'inline-flex' }}><InviteGiftIcon /></span>
                            <span style={{ fontSize: '.66rem', lineHeight: 1, fontWeight: 800 }}>{lang === 'en' ? 'Invite' : '邀请'}</span>
                            <span aria-hidden="true" style={{ position: 'absolute', top: 5, right: '25%', width: 5, height: 5, borderRadius: '50%', background: '#ef5d6c' }} />
                        </button>
                    </>
                )}
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
                    onSelect={(id) => switchTool(id)}
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
            <HubInvitationDialog open={invitationDialogOpen} onClose={() => setInvitationDialogOpen(false)} lang={lang} />
        </div>
    );
};
