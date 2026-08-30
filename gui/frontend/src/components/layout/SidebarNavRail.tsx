import { useState, useEffect, useCallback, useRef } from 'react';
import { SIDEBAR_NAV_RAIL_WIDTH } from './sidebarLayout';
import { SystemPopupMenu, type SystemMenuItem } from './SystemPopupMenu';
import { FavoriteEmployeeButtons, type FavoriteEmployeeSlot } from './FavoriteEmployeeButtons';
import { SystemIcon, AboutIcon, SettingsIcon, MonitorIcon, SkillsIcon, MCPIcon, GossipIcon } from './SidebarNavIcons';
import { SidebarBrandHeader, SidebarLinkedMedal, SidebarPrimaryNav } from './SidebarNavRailPieces';
import { IconRankBadge } from '../ai/WorkbenchIcons';
import { GetHubUserInvitationStatus, GetHubUserRanking } from '../../../wailsjs/go/main/App';
import { BrowserOpenURL, EventsOn } from '../../../wailsjs/runtime';
import { miniAppShortLabel } from '../../i18n/maclawMiniAppLabels';
import { utilitiesNavLabel, utilitiesPageTitle } from '../../i18n/utilitiesLabels';
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
    runningTaskCount: number;
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
    showWorkflowEntry?: boolean;
    showUtilitiesEntry?: boolean;
    utilitiesLabel?: string;
};

const HUB_RANKING_REFRESH_INTERVAL_MS = 30 * 60_000;
const HUB_RANKING_STARTUP_RETRY_DELAYS_MS = [30_000, 2 * 60_000, 8 * 60_000] as const;
const HUB_INVITATION_STATUS_REFRESH_INTERVAL_MS = 30_000;

function InviteGiftIcon() {
    return <svg width="19" height="19" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M20 12v7a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1v-7"/><path d="M2 8h20v4H2z"/><path d="M12 8v12"/><path d="M12 8H7.5a2.5 2.5 0 1 1 2.5-2.5V8"/><path d="M12 8h4.5A2.5 2.5 0 1 0 14 5.5V8"/></svg>;
}

const zhHans = {
    aiAssistant: 'AI \u52a9\u624b',
    system: '\u7cfb\u7edf',
    monitor: '\u76d1\u63a7',
    settings: '\u8bbe\u7f6e',
};

const zhHant = {
    aiAssistant: 'AI \u52a9\u624b',
    system: '\u7cfb\u7d71',
    monitor: '\u76e3\u63a7',
    settings: '\u8a2d\u5b9a',
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
    runningTaskCount,
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
    showWorkflowEntry = true,
    showUtilitiesEntry = true,
    utilitiesLabel,
}: SidebarNavRailProps) => {
    const [systemMenuOpen, setSystemMenuOpen] = useState(false);
    const [invitationEnabled, setInvitationEnabled] = useState(false);
    const [invitationDialogOpen, setInvitationDialogOpen] = useState(false);

    const showRanking = config?.show_hub_ranking !== false; // default: show
    const trophyThreshold = config?.ranking_trophy_threshold || 10; // hub-configured: top N use trophy
    const [medal, setMedal] = useState<{ rank: number; tokenRank: number; durationRank: number; totalUsers: number; rankChange?: number; trophyThreshold: number } | null>(null);
    const rankingRequestSeqRef = useRef(0);
    const rankingLoadedRef = useRef(false);
    const invitationRequestSeqRef = useRef(0);
    const showRegisteredRankingMark = showRanking && !medal && !!remoteActivationStatus?.activated;
    const openUserRanking = () => {
        const url = buildUserRankingURL(config?.remote_hub_url || '', config?.remote_tenant_id);
        if (url) BrowserOpenURL(url);
    };

    const fetchRanking = useCallback((): Promise<boolean> => {
        const requestSeq = ++rankingRequestSeqRef.current;
        if (!showRanking) {
            rankingLoadedRef.current = false;
            setMedal(null);
            return Promise.resolve(false);
        }
        return GetHubUserRanking()
            .then((result) => {
                if (requestSeq !== rankingRequestSeqRef.current) return rankingLoadedRef.current;
                const r = result as { token_rank?: number; duration_rank?: number; total_users?: number; rank_change?: number; error?: string } | null;
                if (!r || r.error) {
                    setMedal(null);
                    return false;
                }
                const tRank = r.token_rank || 0;
                const dRank = r.duration_rank || 0;
                // Pick the best (lowest non-zero) rank and show the badge for any valid Hub ranking response.
                let bestRank = 0;
                if (tRank > 0 && (dRank === 0 || tRank <= dRank)) { bestRank = tRank; }
                else if (dRank > 0) { bestRank = dRank; }
                rankingLoadedRef.current = true;
                setMedal({ rank: bestRank, tokenRank: tRank, durationRank: dRank, totalUsers: r.total_users || 0, rankChange: r.rank_change || 0, trophyThreshold });
                return true;
            })
            .catch(() => {
                if (requestSeq === rankingRequestSeqRef.current) setMedal(null);
                return false;
            });
    }, [showRanking, trophyThreshold]);
    const fetchRankingRef = useRef(fetchRanking);
    useEffect(() => { fetchRankingRef.current = fetchRanking; }, [fetchRanking]);

    useEffect(() => {
        if (!showRanking || !remoteActivationStatus?.activated) return;
        const interval = window.setInterval(() => {
            fetchRankingRef.current();
        }, HUB_RANKING_REFRESH_INTERVAL_MS);
        return () => window.clearInterval(interval);
    }, [showRanking, remoteActivationStatus?.activated]);

    useEffect(() => {
        if (!showRanking || !remoteActivationStatus?.activated) {
            rankingLoadedRef.current = false;
            setMedal(null);
            return;
        }
        let cancelled = false;
        const retryTimers: number[] = [];
        const attempt = (retryIndex: number) => {
            if (rankingLoadedRef.current) return;
            fetchRanking().then((loaded) => {
                if (cancelled || loaded || rankingLoadedRef.current || retryIndex >= HUB_RANKING_STARTUP_RETRY_DELAYS_MS.length) return;
                const timer = window.setTimeout(() => attempt(retryIndex + 1), HUB_RANKING_STARTUP_RETRY_DELAYS_MS[retryIndex]);
                retryTimers.push(timer);
            });
        };
        attempt(0);
        return () => {
            cancelled = true;
            retryTimers.forEach(timer => window.clearTimeout(timer));
        };
    }, [fetchRanking, showRanking, remoteActivationStatus?.activated]);
    // Refresh ranking when token usage changes, throttled to avoid flooding Hub API.
    useEffect(() => {
        if (!showRanking) return;
        let throttleTimer: number | undefined;
        let pending = false;
        const onTokenUsageChanged = () => {
            if (throttleTimer !== undefined) {
                pending = true;
                return;
            }
            throttleTimer = window.setTimeout(() => {
                throttleTimer = undefined;
                fetchRankingRef.current();
                if (pending) {
                    pending = false;
                    throttleTimer = window.setTimeout(() => {
                        throttleTimer = undefined;
                        fetchRankingRef.current();
                    }, 60_000);
                }
            }, 5_000);
        };
        const unsubscribe = EventsOn("llm-token-usage-changed", onTokenUsageChanged);
        return () => {
            window.clearTimeout(throttleTimer);
            if (typeof unsubscribe === 'function') unsubscribe();
        };
    }, [showRanking]);
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
    const resolvedUtilitiesLabel = utilitiesLabel || utilitiesNavLabel(lang);
    const resolvedUtilitiesTitle = utilitiesLabel || utilitiesPageTitle(lang);
    const systemLabel = lang === 'zh-Hans' ? zhHans.system : lang === 'zh-Hant' ? zhHant.system : 'System';
    const systemMenuItems: SystemMenuItem[] = [
        { id: 'settings', icon: <SettingsIcon />, label: lang === 'zh-Hans' ? zhHans.settings : lang === 'zh-Hant' ? zhHant.settings : 'Settings', visible: true },
        { id: 'remote', icon: <MonitorIcon />, label: lang === 'zh-Hans' ? zhHans.monitor : lang === 'zh-Hant' ? zhHant.monitor : 'Monitor', visible: true, badge: runningTaskCount > 0 ? runningTaskCount : undefined },
        { id: 'skills', icon: <SkillsIcon />, label: t('skills'), visible: true },
        { id: 'mcp', icon: <MCPIcon />, label: 'MCP', visible: true },
        { id: 'gossip', icon: <GossipIcon />, label: t('gossip'), visible: gossipAllowed },
    ];
    return (
        <div style={{
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
            <SidebarPrimaryNav navTab={navTab} aiAssistantLabel={aiAssistantLabel} appsLabel={appsLabel} showAppEntry={showAppEntry} showWorkflowEntry={showWorkflowEntry} showUtilitiesEntry={showUtilitiesEntry} switchTool={switchTool} workflowLabel={workflowLabel} utilitiesLabel={resolvedUtilitiesLabel} utilitiesTitle={resolvedUtilitiesTitle} />
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
            <div
                className={'sidebar-item left-nav-item ' + (systemMenuOpen ? 'active' : '')}
                onClick={() => setSystemMenuOpen(prev => !prev)}
                style={{ flexDirection: 'column', padding: '5px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: '1px solid transparent', boxShadow: systemMenuOpen ? 'inset -1px 0 0 var(--theme-text-muted)' : 'none', justifyContent: 'center' }}
                title={systemLabel}
            >
                <span className="sidebar-icon" style={{ margin: 0, display: 'inline-flex', color: systemMenuOpen ? 'var(--theme-primary)' : 'var(--theme-text-primary)' }}><SystemIcon /></span>
                <span style={{ fontSize: '0.72rem', lineHeight: 1, fontWeight: 700 }}>{systemLabel}</span>
            </div>
            <div className={'sidebar-item left-nav-item ' + (navTab === 'about' ? 'active' : '')} onClick={() => switchTool('about')} style={{ flexDirection: 'column', padding: '5px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: '1px solid transparent', boxShadow: navTab === 'about' ? 'inset -1px 0 0 var(--theme-text-muted)' : 'none', justifyContent: 'center' }} title={t('about')}>
                <span className="sidebar-icon" style={{ margin: 0, display: 'inline-flex', color: navTab === 'about' ? 'var(--theme-primary)' : 'var(--theme-text-primary)' }}><AboutIcon /></span>
                <span style={{ fontSize: '0.72rem', lineHeight: 1, fontWeight: 700 }}>{t('about')}</span>
            </div>
            {medal && <SidebarLinkedMedal
                medal={medal}
                lang={lang}
                title={lang === 'zh-Hans' ? '点击查看完整排行榜' : lang === 'zh-Hant' ? '點擊查看完整排行榜' : 'View full leaderboard'}
                onClick={openUserRanking}
            />}
            {showRegisteredRankingMark && (
                <div
                    className="sidebar-medal-badge"
                    title={lang === 'zh-Hans' ? '本月排行暂未生成' : lang === 'zh-Hant' ? '本月排行暫未生成' : 'Monthly ranking pending'}
                    onClick={openUserRanking}
                    style={{
                        display: 'flex',
                        flexDirection: 'column',
                        alignItems: 'center',
                        justifyContent: 'center',
                        width: '100%',
                        minHeight: '38px',
                        padding: '3px 0 5px 0',
                        cursor: 'pointer',
                        userSelect: 'none',
                    }}
                >
                    <span aria-label="monthly ranking" style={{ lineHeight: 1, display: 'flex', alignItems: 'center' }}>
                        <IconRankBadge size={18} />
                    </span>
                    <span style={{ fontSize: '0.58rem', lineHeight: 1, color: 'var(--theme-text-muted)', fontWeight: 700, marginTop: '3px' }}>
                        {lang === 'en' ? 'Rank' : '排行'}
                    </span>
                </div>
            )}
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
            {systemMenuOpen && (
                <SystemPopupMenu
                    items={systemMenuItems}
                    onSelect={(id) => switchTool(id)}
                    onClose={() => setSystemMenuOpen(false)}
                />
            )}
            <HubInvitationDialog open={invitationDialogOpen} onClose={() => setInvitationDialogOpen(false)} lang={lang} />
        </div>
    );
};
