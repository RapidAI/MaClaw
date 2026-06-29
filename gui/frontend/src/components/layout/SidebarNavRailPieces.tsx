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
    let iconElement: React.ReactNode;
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
                    margin: '8px 0',
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
                    padding: '6px 0 8px 0',
                    width: '100%',
                    cursor: 'default',
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

/** Trophy SVG icon */
const TrophyIcon = ({ color }: { color: string }) => (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
        <path d="M6 9H4.5a2.5 2.5 0 0 1 0-5H6" />
        <path d="M18 9h1.5a2.5 2.5 0 0 0 0-5H18" />
        <path d="M4 22h16" />
        <path d="M10 14.66V17c0 .55-.47.98-.97 1.21C7.85 18.75 7 20.24 7 22" />
        <path d="M14 14.66V17c0 .55.47.98.97 1.21C16.15 18.75 17 20.24 17 22" />
        <path d="M18 2H6v7a6 6 0 0 0 12 0V2Z" />
    </svg>
);

/** Medal/Badge SVG icon (for ranks beyond trophy threshold) */
const MedalIcon = ({ color }: { color: string }) => (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
        <circle cx="12" cy="8" r="6" />
        <path d="M15.477 12.89 17 22l-5-3-5 3 1.523-9.11" />
    </svg>
);
