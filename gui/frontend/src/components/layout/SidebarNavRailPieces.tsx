import { AppsRailIcon, ExpertRailIcon } from './SidebarNavIcons';
import { IconRankBadge } from '../ai/WorkbenchIcons';

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
    showUtilitiesEntry?: boolean;
    switchTool: (tool: string) => void;
    workflowLabel?: string;
    utilitiesLabel: string;
    utilitiesTitle?: string;
};

const railItemLabelStyle = { fontSize: '0.72rem', lineHeight: 1.15, fontWeight: 700, textAlign: 'center', width: '100%' } as const;

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
    const rankChange = medal.rankChange || 0;

    const rankText = rank > 0 ? (lang === 'en' ? `#${rank}` : `第${rank}名`) : (lang === 'en' ? 'Rank' : '排行');

    return (
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', width: '100%' }}>
            {/* Decorative divider line between "About" and ranking */}
            <div
                aria-hidden="true"
                style={{
                    width: '70%',
                    height: '1px',
                    margin: '3px 0 2px 0',
                    background: 'linear-gradient(90deg, transparent 0%, rgba(139,157,195,0.12) 15%, rgba(139,157,195,0.35) 50%, rgba(139,157,195,0.12) 85%, transparent 100%)',
                    boxShadow: '0 1px 1px rgba(0,0,0,0.4)',
                }}
            />

            <div
                className="sidebar-medal-badge"
                title={(() => {
                    const parts: string[] = [];
                    const totalText = medal.totalUsers > 0 ? String(medal.totalUsers) : '-';
                    const tokenRankText = medal.tokenRank > 0 ? String(medal.tokenRank) : '-';
                    const durationRankText = medal.durationRank > 0 ? String(medal.durationRank) : '-';
                    parts.push(lang === 'en' ? `Token #${tokenRankText}/${totalText}` : `Token 第${tokenRankText}/${totalText}名`);
                    parts.push(lang === 'en' ? `Online #${durationRankText}/${totalText}` : `在线 第${durationRankText}/${totalText}名`);
                    const prefix = lang === 'en' ? 'This month: ' : '本月排名: ';
                    return prefix + parts.join(', ');
                })()}
                style={{
                    display: 'flex',
                    flexDirection: 'column',
                    alignItems: 'center',
                    padding: '2px 0 5px 0',
                    width: '100%',
                    cursor: 'pointer',
                    userSelect: 'none',
                    minHeight: '40px',
                    justifyContent: 'center',
                }}
            >
                <span
                    className="sidebar-medal-icon"
                    style={{ lineHeight: 1, display: 'flex', alignItems: 'center' }}
                >
                    <IconRankBadge rank={rank} size={20} />
                </span>
                <div style={{ display: 'flex', alignItems: 'center', gap: '2px', marginTop: '2px' }}>
                    <span style={{ fontSize: '0.62rem', lineHeight: 1, color: 'var(--theme-text)', fontWeight: 700 }}>
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

export const SidebarPrimaryNav = ({ navTab, aiAssistantLabel, appsLabel, showAppEntry, showWorkflowEntry, showUtilitiesEntry = true, switchTool, workflowLabel, utilitiesLabel, utilitiesTitle }: SidebarPrimaryNavProps) => {
    const isAiActive = navTab === 'ai';

    return (
        <>
            <button
                type="button"
                className={'sidebar-item left-nav-item left-nav-item--ai ' + (isAiActive ? 'active' : '')}
                onClick={() => { switchTool('ai'); }}
                title={aiAssistantLabel}
                aria-current={isAiActive ? 'page' : undefined}
            >
                <div className="ai-nav-icon-badge" aria-hidden="true">
                    <svg className="ai-nav-icon" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
                        <path d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8v.5z" />
                        <circle cx="12" cy="11.5" r="1" fill="currentColor" stroke="none" />
                        <circle cx="8.5" cy="11.5" r="1" fill="currentColor" stroke="none" />
                        <circle cx="15.5" cy="11.5" r="1" fill="currentColor" stroke="none" />
                    </svg>
                </div>
                <span className="ai-nav-label">{aiAssistantLabel}</span>
            </button>
            <div style={{ width: '70%', height: '2px', margin: '4px 0 6px 0', borderRadius: '1px', background: 'linear-gradient(90deg, transparent 0%, var(--theme-border) 20%, var(--theme-text-muted) 50%, var(--theme-border) 80%, transparent 100%)', opacity: 0.5 }} />
            {showAppEntry && (
                <div
                    className={'sidebar-item left-nav-item ' + (navTab === 'apps' ? 'active' : '')}
                    onClick={() => switchTool('apps')}
                    style={{ flexDirection: 'column', padding: '5px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: '1px solid transparent', boxShadow: navTab === 'apps' ? 'inset -1px 0 0 var(--theme-primary)' : 'none', justifyContent: 'center' }}
                    title={appsLabel}
                >
                    <span className="sidebar-icon" style={{ margin: 0, display: 'inline-flex', color: navTab === 'apps' ? 'var(--theme-primary-strong)' : 'var(--theme-text-primary)' }}><AppsRailIcon /></span>
                    <span style={railItemLabelStyle}>{appsLabel}</span>
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
                <span style={railItemLabelStyle}>{workflowLabel || '工作流'}</span>
            </div>
            <div
                className={'sidebar-item left-nav-item ' + (navTab === 'utilities' ? 'active' : '')}
                onClick={() => switchTool('utilities')}
                data-testid="sidebar-utilities-nav"
                style={{ flexDirection: 'column', padding: '5px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: '1px solid transparent', boxShadow: navTab === 'utilities' ? 'inset -1px 0 0 var(--theme-primary)' : 'none', justifyContent: 'center', display: showUtilitiesEntry ? undefined : 'none' }}
                title={utilitiesTitle || utilitiesLabel}
            >
                <span className="sidebar-icon" style={{ margin: 0, display: 'inline-flex', color: navTab === 'utilities' ? 'var(--theme-primary-strong)' : 'var(--theme-text-primary)' }}>
                    <ExpertRailIcon />
                </span>
                <span style={railItemLabelStyle}>{utilitiesLabel}</span>
            </div>
        </>
    );
};
