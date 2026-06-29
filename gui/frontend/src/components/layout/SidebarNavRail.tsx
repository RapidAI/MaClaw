import { useState, useEffect } from 'react';
import { SIDEBAR_NAV_RAIL_WIDTH } from './sidebarLayout';
import { SystemPopupMenu, type SystemMenuItem } from './SystemPopupMenu';
import { FavoriteEmployeeButtons, type FavoriteEmployeeSlot } from './FavoriteEmployeeButtons';
import { AppsRailIcon, SystemIcon, AboutIcon, SettingsIcon, MonitorIcon, SkillsIcon, MCPIcon, GossipIcon } from './SidebarNavIcons';
import { SidebarBrandHeader, SidebarMedalBadge } from './SidebarNavRailPieces';
import { GetHubUserRanking } from '../../../wailsjs/go/main/App';

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

            <div
                className={'sidebar-item left-nav-item left-nav-item--ai ' + (navTab === 'ai' ? 'active' : '')}
                onClick={() => { switchTool('ai'); }}
                style={{ flexDirection: 'column', padding: '8px 4px', width: '100%', gap: '3px', borderLeft: 'none', borderRight: '1px solid transparent', boxShadow: navTab === 'ai' ? 'inset -1px 0 0 var(--theme-primary)' : 'none', justifyContent: 'center' }}
                title={aiAssistantLabel}
            >
                <div style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    width: '36px',
                    height: '36px',
                    borderRadius: '10px',
                    background: navTab === 'ai'
                        ? 'linear-gradient(135deg, var(--theme-primary), var(--theme-primary-strong))'
                        : 'linear-gradient(135deg, color-mix(in srgb, var(--theme-primary) 85%, transparent), color-mix(in srgb, var(--theme-primary) 60%, transparent))',
                    boxShadow: navTab === 'ai'
                        ? '0 2px 8px color-mix(in srgb, var(--theme-primary) 40%, transparent), 0 0 0 2px color-mix(in srgb, var(--theme-primary) 20%, transparent)'
                        : '0 1px 4px rgba(0,0,0,0.1)',
                    transition: 'all 0.2s ease',
                    cursor: 'pointer',
                }}>
                    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#fff" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
                        <path d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8v.5z" />
                        <circle cx="12" cy="11.5" r="0.8" fill="#fff" stroke="none" />
                        <circle cx="8.5" cy="11.5" r="0.8" fill="#fff" stroke="none" />
                        <circle cx="15.5" cy="11.5" r="0.8" fill="#fff" stroke="none" />
                    </svg>
                </div>
                <span style={{ fontSize: '0.68rem', lineHeight: 1, fontWeight: 700, color: navTab === 'ai' ? 'var(--theme-primary)' : 'var(--theme-text-primary)' }}>{aiAssistantLabel}</span>
            </div>

            <div style={{ width: '70%', height: '2px', margin: '4px 0 6px 0', borderRadius: '1px', background: 'linear-gradient(90deg, transparent 0%, var(--theme-border) 20%, var(--theme-text-muted) 50%, var(--theme-border) 80%, transparent 100%)', opacity: 0.5 }} />

            {showAppEntry && (
                <div
                    className={'sidebar-item left-nav-item ' + (navTab === 'apps' ? 'active' : '')}
                    onClick={() => switchTool('apps')}
                    style={{ flexDirection: 'column', padding: '5px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: '1px solid transparent', boxShadow: navTab === 'apps' ? 'inset -1px 0 0 var(--theme-primary)' : 'none', justifyContent: 'center' }}
                    title={appsLabel}
                >
                    <span className="sidebar-icon" style={{ margin: 0, display: 'inline-flex', color: navTab === 'apps' ? 'var(--theme-primary-strong)' : 'var(--theme-text-primary)' }}><AppsRailIcon /></span>
                    <span style={{ fontSize: '0.72rem', lineHeight: 1, fontWeight: 700 }}>{appsLabel}</span>
                </div>
            )}

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

            {medal && <SidebarMedalBadge medal={medal} lang={lang} />}

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
