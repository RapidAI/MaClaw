import { useState } from 'react';
import { SIDEBAR_NAV_RAIL_WIDTH } from './sidebarLayout';
import { SystemPopupMenu, type SystemMenuItem } from './SystemPopupMenu';
import { FavoriteEmployeeButtons, type FavoriteEmployeeSlot } from './FavoriteEmployeeButtons';
import { AppsRailIcon } from './SidebarNavIcons';

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

const icon = {
    lobster: '\u{1F99E}',
    monitor: '\u{1F4E1}',
    skills: '\u{1F9E9}',
    mcp: '\u{1F50C}',
    gossip: '\u{1F5E3}\uFE0F',
    settings: '\u2699\uFE0F',
    about: '\u2139\uFE0F',
    system: '\u2699\uFE0F',
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
    favoriteEmployees = [],
    veAuthorized = false,
    onStartVEConversation = () => {},
    onReorderFavorites = () => {},
    onRemoveFavorite = () => {},
    onRenameFavorite = () => {},
    showAppEntry = false,
}: SidebarNavRailProps) => {
    const [systemMenuOpen, setSystemMenuOpen] = useState(false);

    const aiAssistantLabel = lang === 'zh-Hans' ? zhHans.aiAssistant : lang === 'zh-Hant' ? zhHant.aiAssistant : 'AI Asst';
    const appsLabel = lang === 'zh-Hans' ? zhHans.apps : lang === 'zh-Hant' ? zhHant.apps : 'Apps';
    const systemLabel = lang === 'zh-Hans' ? zhHans.system : lang === 'zh-Hant' ? zhHant.system : 'System';
    const isTigerClaw = brandInfo?.id === 'qianxin';

    const systemMenuItems: SystemMenuItem[] = [
        { id: 'settings', icon: <span>{icon.settings}</span>, label: lang === 'zh-Hans' ? zhHans.settings : lang === 'zh-Hant' ? zhHant.settings : 'Settings', visible: true },
        { id: 'remote', icon: <span>{icon.monitor}</span>, label: lang === 'zh-Hans' ? zhHans.monitor : lang === 'zh-Hant' ? zhHant.monitor : 'Monitor', visible: true, badge: runningTaskCount > 0 ? runningTaskCount : undefined },
        { id: 'skills', icon: <span>{icon.skills}</span>, label: t('skills'), visible: true },
        { id: 'mcp', icon: <span>{icon.mcp}</span>, label: 'MCP', visible: true },
        { id: 'gossip', icon: <span>{icon.gossip}</span>, label: t('gossip'), visible: gossipAllowed },
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
            <div className="sidebar-header" style={{ height: '56px', padding: '4px 0 2px 0', justifyContent: 'flex-start', width: '100%', flexDirection: 'column', gap: '1px' }}>
                {brandInfo?.id === 'qianxin' ? (
                    <img src={currentIcon} alt="Logo" className="sidebar-logo" style={{ width: '30px', height: '30px', objectFit: 'contain' }} />
                ) : (
                    <div style={{ width: '38px', height: '28px', overflow: 'hidden', display: 'flex', justifyContent: 'center', alignItems: 'flex-start' }}>
                        <img src={currentIcon} alt="Logo" style={{ width: '64px', height: '48px', objectFit: 'contain', transform: 'translateY(-2px)' }} />
                    </div>
                )}
                <div style={{ color: isTigerClaw ? 'var(--theme-primary-strong)' : 'var(--theme-primary)', fontSize: isTigerClaw ? '0.64rem' : '0.72rem', fontWeight: 800, lineHeight: 1, fontFamily: 'Georgia, serif' }}>{brandSidebarName}</div>
            </div>

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
                    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#fff" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                        <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
                        <circle cx="12" cy="10" r="1" fill="#fff" stroke="none" />
                        <circle cx="8" cy="10" r="1" fill="#fff" stroke="none" />
                        <circle cx="16" cy="10" r="1" fill="#fff" stroke="none" />
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
                <span className="sidebar-icon" style={{ margin: 0, fontSize: '1.08rem' }}>{icon.system}</span>
                <span style={{ fontSize: '0.72rem', lineHeight: 1, fontWeight: 700 }}>{systemLabel}</span>
            </div>

            <div className={'sidebar-item left-nav-item ' + (navTab === 'about' ? 'active' : '')} onClick={() => switchTool('about')} style={{ flexDirection: 'column', padding: '5px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: '1px solid transparent', boxShadow: navTab === 'about' ? 'inset -1px 0 0 var(--theme-text-muted)' : 'none', justifyContent: 'center' }} title={t('about')}>
                <span className="sidebar-icon" style={{ margin: 0, fontSize: '1.08rem' }}>{icon.about}</span>
                <span style={{ fontSize: '0.72rem', lineHeight: 1, fontWeight: 700 }}>{t('about')}</span>
            </div>

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
