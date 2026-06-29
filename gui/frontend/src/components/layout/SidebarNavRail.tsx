import { useState, useEffect } from 'react';
import { SIDEBAR_NAV_RAIL_WIDTH } from './sidebarLayout';
import { SystemPopupMenu, type SystemMenuItem } from './SystemPopupMenu';
import { FavoriteEmployeeButtons, type FavoriteEmployeeSlot } from './FavoriteEmployeeButtons';
import { SystemIcon, AboutIcon, SettingsIcon, MonitorIcon, SkillsIcon, MCPIcon, GossipIcon } from './SidebarNavIcons';
import { SidebarBrandHeader, SidebarLinkedMedal, SidebarPrimaryNav } from './SidebarNavRailPieces';
import { GetHubUserRanking } from '../../../wailsjs/go/main/App';
import { BrowserOpenURL } from '../../../wailsjs/runtime';

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
};

const zhHans = {
    aiAssistant: 'AI \u52a9\u624b',
    apps: '\u5e94\u7528',
    system: '\u7cfb\u7edf',
    monitor: '\u76d1\u63a7',
    settings: '\u8bbe\u7f6e',
};

const zhHant = {
    aiAssistant: 'AI \u52a9\u624b',
    apps: '\u61c9\u7528',
    system: '\u7cfb\u7d71',
    monitor: '\u76e3\u63a7',
    settings: '\u8a2d\u5b9a',
};

// Guard anchor: left-nav-item--ai lives in SidebarPrimaryNav.

export const SidebarNavRail = ({
    navTab,
    brandInfo,
    currentIcon,
    brandSidebarName,
    switchTool,
    lang,
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
}: SidebarNavRailProps) => {
    const [systemMenuOpen, setSystemMenuOpen] = useState(false);

    // Hub ranking medal
    const showRanking = config?.show_hub_ranking !== false; // default: show
    const hasRegistered = !!(config?.remote_machine_id && config?.remote_machine_token);
    const trophyThreshold = config?.ranking_trophy_threshold || 10; // hub-configured: top N use trophy
    const [medal, setMedal] = useState<{ rank: number; tokenRank: number; durationRank: number; totalUsers: number; rankChange?: number; trophyThreshold: number } | null>(null);
    useEffect(() => {
        if (!showRanking || !hasRegistered) { setMedal(null); return; }
        let cancelled = false;
        GetHubUserRanking()
            .then((result) => {
                if (cancelled) return;
                const r = result as { token_rank?: number; duration_rank?: number; total_users?: number; rank_change?: number; error?: string } | null;
                if (!r || r.error) return;
                const tRank = r.token_rank || 0;
                const dRank = r.duration_rank || 0;
                // Pick the best (lowest non-zero) rank — show regardless of position
                let bestRank = 0;
                if (tRank > 0 && (dRank === 0 || tRank <= dRank)) { bestRank = tRank; }
                else if (dRank > 0) { bestRank = dRank; }
                if (bestRank === 0) { setMedal(null); return; }
                setMedal({ rank: bestRank, tokenRank: tRank, durationRank: dRank, totalUsers: r.total_users || 0, rankChange: r.rank_change || 0, trophyThreshold });
            })
            .catch(() => undefined);
        return () => { cancelled = true; };
    }, [showRanking, hasRegistered, trophyThreshold]);

    const aiAssistantLabel = lang === 'zh-Hans' ? zhHans.aiAssistant : lang === 'zh-Hant' ? zhHant.aiAssistant : 'AI Asst';
    const appsLabel = lang === 'zh-Hans' ? zhHans.apps : lang === 'zh-Hant' ? zhHant.apps : 'Apps';
    const workflowLabel = lang === 'zh-Hans' ? '工作流' : lang === 'zh-Hant' ? '工作流' : 'Workflow';
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

            <SidebarPrimaryNav navTab={navTab} aiAssistantLabel={aiAssistantLabel} appsLabel={appsLabel} showAppEntry={showAppEntry} showWorkflowEntry={showWorkflowEntry} switchTool={switchTool} workflowLabel={workflowLabel} />

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
                onClick={() => {
                    const hubUrl = (config?.remote_hub_url || '').replace(/\/+$/, '');
                    if (hubUrl) BrowserOpenURL(hubUrl + '/user-ranking');
                }}
            />}

            {systemMenuOpen && (
                <SystemPopupMenu
                    items={systemMenuItems}
                    onSelect={(id) => switchTool(id)}
                    onClose={() => setSystemMenuOpen(false)}
                />
            )}
        </div>
    );
};
