import { AppsRailIcon } from './SidebarNavIcons';

type SidebarMedal = {
    rank: number;
    tokenRank: number;
    durationRank: number;
    totalUsers: number;
    rankChange?: number; // positive = up, negative = down, 0 or undefined = no change
    trophyThreshold: number; // hub-configured: ranks <= this use trophy icon, beyond use medal
};

type SidebarBrandHeaderProps = {
    brandId?: string;
    currentIcon: string;
    brandSidebarName: string;
};

type SidebarMedalBadgeProps = {
    medal: SidebarMedal;
    lang: string;
};

type SidebarLinkedMedalProps = {
    medal: SidebarMedal;
    lang: string;
    title: string;
    onClick: () => void;
};

type SidebarPrimaryNavProps = {
    navTab: string;
    aiAssistantLabel: string;
    appsLabel: string;
    showAppEntry: boolean;
    showWorkflowEntry: boolean;
    switchTool: (tool: string) => void;
    workflowLabel?: string;
};

const sharedHeaderStyle = { justifyContent: 'flex-start', width: '100%', flexDirection: 'column' } as const;
const maclawHeaderStyle = { ...sharedHeaderStyle, height: '64px', padding: '0 0 2px 0', gap: '0' } as const;
const tigerClawHeaderStyle = { ...sharedHeaderStyle, height: '56px', padding: '4px 0 2px 0', gap: '1px' } as const;
const maclawLogoSlotStyle = { width: '54px', height: '48px', display: 'flex', justifyContent: 'center', alignItems: 'center' } as const;
const maclawLogoImageStyle = { width: '50px', height: '50px', objectFit: 'contain' } as const;

export const SidebarBrandHeader = ({ brandId, currentIcon, brandSidebarName }: SidebarBrandHeaderProps) => {
    const isTigerClaw = brandId === 'qianxin';
    return (
        <div className="sidebar-header" style={isTigerClaw ? tigerClawHeaderStyle : maclawHeaderStyle}>
            {isTigerClaw ? (
                <img src={currentIcon} alt="Logo" className="sidebar-logo" style={{ width: '30px', height: '30px', objectFit: 'contain' }} />
            ) : (
                <div style={maclawLogoSlotStyle}>
                    <img src={currentIcon} alt="Logo" style={maclawLogoImageStyle} />
                </div>
            )}
            <div style={{ color: isTigerClaw ? 'var(--theme-primary-strong)' : 'var(--theme-primary)', fontSize: isTigerClaw ? '0.64rem' : '0.74rem', fontWeight: 800, lineHeight: 1, fontFamily: 'Georgia, serif', transform: isTigerClaw ? undefined : 'translateY(-2px)' }}>{brandSidebarName}</div>
        </div>
    );
};

export const SidebarMedalBadge = ({ medal, lang }: SidebarMedalBadgeProps) => {
    const rank = medal.rank;
    const trophyThreshold = medal.trophyThreshold;
    const useTrophy = rank <= trophyThreshold;
    const rankChange = medal.rankChange || 0;

    // Trophy color or medal icon
    let iconElement: JSX.Element;
    if (rank === 1) {
        iconElement = <TrophyIcon color="#fbbf24" />; // gold
    } else if (rank === 2) {
        iconElement = <TrophyIcon color="#c0c0c0" />; // silver
    } else if (rank === 3) {
        iconElement = <TrophyIcon color="#cd7f32" />; // bronze
    } else if (useTrophy) {
        iconElement = <TrophyIcon color="#6b7280" />; // gray trophy
    } else {
        iconElement = <MedalIcon color="#8b9dc3" />; // medal/badge icon
    }

    const rankText = lang === 'en' ? `#${rank}` : `第${rank}名`;
    const glowClass = rank === 1 ? 'sidebar-medal--gold' : rank === 2 ? 'sidebar-medal--silver' : rank === 3 ? 'sidebar-medal--bronze' : undefined;

    return (
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', width: '100%' }}>
            {/* Decorative divider line between "About" and ranking */}
            <div
                aria-hidden="true"
                style={{
                    width: '70%',
                    height: '1px',
                    margin: '4px 0 2px 0',
                    background: 'linear-gradient(90deg, transparent 0%, rgba(139,157,195,0.12) 15%, rgba(139,157,195,0.35) 50%, rgba(139,157,195,0.12) 85%, transparent 100%)',
                    boxShadow: '0 1px 1px rgba(0,0,0,0.4)',
                }}
            />

            <div
                className="sidebar-medal-badge"
                title={(() => {
                    const parts: string[] = [];
                    if (medal.tokenRank > 0) parts.push(lang === 'en' ? `Token #${medal.tokenRank}/${medal.totalUsers}` : `Token 第${medal.tokenRank}/${medal.totalUsers}名`);
                    if (medal.durationRank > 0) parts.push(lang === 'en' ? `Online #${medal.durationRank}/${medal.totalUsers}` : `在线 第${medal.durationRank}/${medal.totalUsers}名`);
                    const prefix = lang === 'en' ? 'This month: ' : '本月排名: ';
                    return prefix + parts.join(', ');
                })()}
                style={{
                    display: 'flex',
                    flexDirection: 'column',
                    alignItems: 'center',
                    padding: '2px 0 8px 0',
                    width: '100%',
                    cursor: 'pointer',
                    userSelect: 'none',
                    minHeight: '52px',
                    justifyContent: 'center',
                }}
            >
                <span
                    className={`sidebar-medal-emoji${glowClass ? ` ${glowClass}` : ''}`}
                    style={{ fontSize: '18px', lineHeight: 1, display: 'flex', alignItems: 'center' }}
                >{iconElement}</span>
                <div style={{ display: 'flex', alignItems: 'center', gap: '2px', marginTop: '3px' }}>
                    <span style={{ fontSize: '0.62rem', lineHeight: 1, color: 'var(--theme-text-muted)', fontWeight: 600 }}>
                        {rankText}
                    </span>
                    {rankChange > 0 && (
                        <span style={{ fontSize: '0.55rem', fontWeight: 700, color: '#ef4444', lineHeight: 1 }}>↑{rankChange}</span>
                    )}
                    {rankChange < 0 && (
                        <span style={{ fontSize: '0.55rem', fontWeight: 700, color: '#22c55e', lineHeight: 1 }}>↓{Math.abs(rankChange)}</span>
                    )}
                </div>
            </div>
        </div>
    );
};

export const SidebarLinkedMedal = ({ medal, lang, title, onClick }: SidebarLinkedMedalProps) => (
    <div onClick={onClick} style={{ cursor: 'pointer', width: '100%' }} title={title}>
        <SidebarMedalBadge medal={medal} lang={lang} />
    </div>
);

export const SidebarPrimaryNav = ({ navTab, aiAssistantLabel, appsLabel, showAppEntry, showWorkflowEntry, switchTool, workflowLabel }: SidebarPrimaryNavProps) => (
    <>
        <div
            className={'sidebar-item left-nav-item left-nav-item--ai ' + (navTab === 'ai' ? 'active' : '')}
            onClick={() => { switchTool('ai'); }}
            style={{ flexDirection: 'column', padding: '8px 4px', width: '100%', gap: '3px', borderLeft: 'none', borderRight: '1px solid transparent', boxShadow: navTab === 'ai' ? 'inset -1px 0 0 var(--theme-primary)' : 'none', justifyContent: 'center' }}
            title={aiAssistantLabel}
        >
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', width: '36px', height: '36px', borderRadius: '10px', background: navTab === 'ai' ? 'linear-gradient(135deg, var(--theme-primary), var(--theme-primary-strong))' : 'linear-gradient(135deg, color-mix(in srgb, var(--theme-primary) 85%, transparent), color-mix(in srgb, var(--theme-primary) 60%, transparent))', boxShadow: navTab === 'ai' ? '0 2px 8px color-mix(in srgb, var(--theme-primary) 40%, transparent), 0 0 0 2px color-mix(in srgb, var(--theme-primary) 20%, transparent)' : '0 1px 4px rgba(0,0,0,0.1)', transition: 'all 0.2s ease', cursor: 'pointer' }}>
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
        <div
            className={'sidebar-item left-nav-item ' + (navTab === 'workflows' ? 'active' : '')}
            onClick={() => switchTool('workflows')}
            style={{ flexDirection: 'column', padding: '5px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: '1px solid transparent', boxShadow: navTab === 'workflows' ? 'inset -1px 0 0 var(--theme-primary)' : 'none', justifyContent: 'center', display: showWorkflowEntry ? undefined : 'none' }}
            title={workflowLabel || '工作流'}
        >
            <span className="sidebar-icon" style={{ margin: 0, display: 'inline-flex', color: navTab === 'workflows' ? 'var(--theme-primary-strong)' : 'var(--theme-text-primary)' }}>
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M3 3h6v6H3zM15 3h6v6h-6zM9 15h6v6H9z" />
                    <path d="M6 9v3a3 3 0 0 0 3 3h0M18 9v3a3 3 0 0 1-3 3h0" />
                </svg>
            </span>
            <span style={{ fontSize: '0.72rem', lineHeight: 1, fontWeight: 700 }}>{workflowLabel || '工作流'}</span>
        </div>
    </>
);

/** Trophy SVG icon — filled cup body for rich color */
const TrophyIcon = ({ color }: { color: string }) => (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
        <path d="M6 9H4.5a2.5 2.5 0 0 1 0-5H6" />
        <path d="M18 9h1.5a2.5 2.5 0 0 0 0-5H18" />
        <path d="M4 22h16" />
        <path d="M10 14.66V17c0 .55-.47.98-.97 1.21C7.85 18.75 7 20.24 7 22" />
        <path d="M14 14.66V17c0 .55.47.98.97 1.21C16.15 18.75 17 20.24 17 22" />
        <path d="M18 2H6v7a6 6 0 0 0 12 0V2Z" fill={color} fillOpacity={0.25} />
    </svg>
);

/** Medal/Badge SVG icon (for ranks beyond trophy threshold) */
const MedalIcon = ({ color }: { color: string }) => (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
        <circle cx="12" cy="8" r="6" />
        <path d="M15.477 12.89 17 22l-5-3-5 3 1.523-9.11" />
    </svg>
);
