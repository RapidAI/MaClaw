import agentnetIcon from '../../assets/images/agentnet.svg';
import { SIDEBAR_NAV_RAIL_WIDTH } from './sidebarLayout';

type SidebarNavRailProps = {
    navTab: string;
    brandInfo: { id: string } | null;
    currentIcon: string;
    brandSidebarName: string;
    switchTool: (tool: string) => void;
    lang: string;
    maclawLLMOnline: boolean;
    agentNetRunning: boolean;
    remoteActivationStatus: any;
    runningTaskCount: number;
    t: (key: string) => string;
    gossipAllowed: boolean;
    config: any;
    sidebarExpanded: boolean;
    setSidebarExpanded: (updater: (prev: boolean) => boolean) => void;
};

const zhHans = {
    aiAssistant: 'AI \u52a9\u624b',
    monitor: '\u76d1\u63a7',
    agentNet: '\u667a\u7f51',
    collapse: '\u6536\u8d77',
    more: '\u66f4\u591a',
};

const zhHant = {
    aiAssistant: 'AI \u52a9\u624b',
    monitor: '\u76e3\u63a7',
    agentNet: '\u667a\u7db2',
};

const icon = {
    lobster: '\u{1F99E}',
    monitor: '\u{1F4E1}',
    skills: '\u{1F9E9}',
    mcp: '\u{1F50C}',
    gossip: '\u{1F5E3}\uFE0F',
    collapse: '\u25B4',
    more: '\u00B7\u00B7\u00B7',
    settings: '\u2699\uFE0F',
    about: '\u2139\uFE0F',
};

export const SidebarNavRail = ({
    navTab,
    brandInfo,
    currentIcon,
    brandSidebarName,
    switchTool,
    lang,
    maclawLLMOnline,
    agentNetRunning,
    remoteActivationStatus,
    runningTaskCount,
    t,
    gossipAllowed,
    config,
    sidebarExpanded,
    setSidebarExpanded,
}: SidebarNavRailProps) => {
    const aiAssistantLabel = lang === 'zh-Hans' ? zhHans.aiAssistant : lang === 'zh-Hant' ? zhHant.aiAssistant : 'AI Asst';
    const isTigerClaw = brandInfo?.id === 'qianxin';
    const agentNetHealthy = isTigerClaw || agentNetRunning;
    const navItems = [
        {
            id: 'remote',
            configKey: 'show_nav_monitor',
            icon: <span className="sidebar-icon" style={{ margin: 0, fontSize: '1rem', position: 'relative' }}>{icon.monitor}{runningTaskCount > 0 && (<span style={{ position: 'absolute', top: '-5px', right: '-8px', minWidth: '18px', height: '18px', lineHeight: '18px', fontSize: '10px', fontWeight: 700, textAlign: 'center', padding: runningTaskCount > 99 ? '0 2px' : '0 3px', borderRadius: '999px', background: 'var(--theme-danger)', color: '#ffffff', boxShadow: '0 1px 3px rgba(0,0,0,0.3)', zIndex: 10 }}>{runningTaskCount > 99 ? '99+' : runningTaskCount}</span>)}</span>,
            label: lang === 'zh-Hans' ? zhHans.monitor : lang === 'zh-Hant' ? zhHant.monitor : 'Monitor',
        },
        { id: 'skills', configKey: 'show_nav_skills', icon: <span className="sidebar-icon" style={{ margin: 0, fontSize: '1rem' }}>{icon.skills}</span>, label: t('skills') },
        { id: 'mcp', configKey: 'show_nav_mcp', icon: <span className="sidebar-icon" style={{ margin: 0, fontSize: '1rem' }}>{icon.mcp}</span>, label: 'MCP' },
        ...(gossipAllowed ? [{ id: 'gossip', configKey: 'show_nav_gossip', icon: <span className="sidebar-icon" style={{ margin: 0, fontSize: '1rem' }}>{icon.gossip}</span>, label: t('gossip') }] : []),
        ...(!isTigerClaw ? [{ id: 'agentnet', configKey: 'show_nav_agentnet', icon: <img src={agentnetIcon} alt="AgentNet" style={{ width: '18px', height: '18px', margin: 0 }} />, label: lang === 'zh-Hans' ? zhHans.agentNet : lang === 'zh-Hant' ? zhHant.agentNet : 'AgentNet' }] : []),
    ];
    const isPinned = (item: typeof navItems[0]) => (config as any)?.[item.configKey] !== false;
    const pinnedItems = navItems.filter(isPinned);
    const collapsedItems = navItems.filter(item => !isPinned(item));
    const renderItem = (item: typeof navItems[0]) => (
        <div key={item.id} className={'sidebar-item left-nav-item ' + (navTab === item.id ? 'active' : '')} onClick={() => switchTool(item.id)} style={{ flexDirection: 'column', padding: '5px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: navTab === item.id ? '3px solid var(--theme-text-muted)' : '3px solid transparent', justifyContent: 'center', position: item.id === 'remote' ? 'relative' as const : undefined }} title={item.label}>
            {item.icon}
            <span style={{ fontSize: '0.72rem', lineHeight: 1, fontWeight: 700 }}>{item.label}</span>
        </div>
    );

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
        }}>
            <div className="sidebar-header" style={{ height: '86px', padding: '4px 0 6px 0', justifyContent: 'flex-start', width: '100%', flexDirection: 'column', gap: '2px' }}>
                {brandInfo?.id === 'qianxin' ? (
                    <img src={currentIcon} alt="Logo" className="sidebar-logo" style={{ width: '34px', height: '34px', objectFit: 'contain' }} />
                ) : (
                    <div style={{ width: '42px', height: '34px', overflow: 'hidden', display: 'flex', justifyContent: 'center', alignItems: 'flex-start' }}>
                        <img src={currentIcon} alt="Logo" style={{ width: '74px', height: '56px', objectFit: 'contain', transform: 'translateY(-2px)' }} />
                    </div>
                )}
                <div style={{ color: '#d94b3d', fontSize: '0.78rem', fontWeight: 800, lineHeight: 1, fontFamily: 'Georgia, serif' }}>{brandSidebarName}</div>
            </div>

            <div
                className={'sidebar-item left-nav-item left-nav-item--ai ' + (navTab === 'ai' ? 'active' : '')}
                onClick={() => { switchTool('ai'); }}
                style={{ flexDirection: 'column', padding: '6px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: navTab === 'ai' ? '3px solid var(--primary-color)' : '3px solid transparent', justifyContent: 'center' }}
                title={aiAssistantLabel}
            >
                <span className="sidebar-icon" style={{ margin: 0, display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: '2.22rem', height: '2.22rem', borderRadius: '50%', padding: '4px', background: (() => { const llm = maclawLLMOnline; const net = agentNetHealthy; const mob = remoteActivationStatus?.activated; return (llm && net && mob) ? 'var(--theme-primary-strong)' : (!llm && !net && !mob) ? 'var(--theme-text-muted)' : 'var(--theme-primary)'; })(), boxShadow: navTab === 'ai' ? '0 0 0 2px color-mix(in srgb, var(--theme-primary) 28%, transparent), 0 0 14px color-mix(in srgb, var(--theme-primary) 72%, transparent), 0 0 30px color-mix(in srgb, var(--theme-primary) 48%, transparent)' : '0 0 12px color-mix(in srgb, var(--theme-primary) 24%, transparent)', transition: 'box-shadow 0.24s ease, background 0.3s ease, filter 0.24s ease', filter: navTab === 'ai' ? 'saturate(1.16)' : 'none' }}>
                    <span style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', width: '100%', height: '100%', borderRadius: '50%', background: 'var(--theme-surface)', fontSize: '1.42rem', lineHeight: 1 }}>{icon.lobster}</span>
                </span>
                <span style={{ fontSize: '0.72rem', lineHeight: 1, fontWeight: 700 }}>{aiAssistantLabel}</span>
            </div>

            {pinnedItems.map(renderItem)}
            {collapsedItems.length > 0 && (
                <div className="sidebar-item left-nav-item" onClick={() => setSidebarExpanded(prev => !prev)} style={{ flexDirection: 'column', padding: '4px 0', width: '100%', gap: '2px', borderLeft: 'none', borderRight: '3px solid transparent', justifyContent: 'center', cursor: 'pointer', opacity: 0.7 }} title={sidebarExpanded ? (lang === 'zh-Hans' ? zhHans.collapse : 'Collapse') : (lang === 'zh-Hans' ? zhHans.more : 'More')}>
                    <span style={{ fontSize: '1.05rem', lineHeight: 1 }}>{sidebarExpanded ? icon.collapse : icon.more}</span>
                </div>
            )}
            {sidebarExpanded && collapsedItems.map(renderItem)}

            <div style={{ flex: 1 }}></div>

            <div className={'sidebar-item left-nav-item ' + (navTab === 'settings' ? 'active' : '')} onClick={() => { switchTool('settings'); }} style={{ flexDirection: 'column', padding: '5px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: navTab === 'settings' ? '3px solid var(--theme-text-muted)' : '3px solid transparent', justifyContent: 'center' }} title={t('settings')}>
                <span className="sidebar-icon" style={{ margin: 0, fontSize: '1.08rem' }}>{icon.settings}</span>
                <span style={{ fontSize: '0.72rem', lineHeight: 1, fontWeight: 700 }}>{t('settings')}</span>
            </div>

            <div className={'sidebar-item left-nav-item ' + (navTab === 'about' ? 'active' : '')} onClick={() => switchTool('about')} style={{ flexDirection: 'column', padding: '5px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: navTab === 'about' ? '3px solid var(--theme-text-muted)' : '3px solid transparent', justifyContent: 'center' }} title={t('about')}>
                <span className="sidebar-icon" style={{ margin: 0, fontSize: '1.08rem' }}>{icon.about}</span>
                <span style={{ fontSize: '0.72rem', lineHeight: 1, fontWeight: 700 }}>{t('about')}</span>
            </div>
        </div>
    );
};
