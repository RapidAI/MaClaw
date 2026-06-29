type SidebarMedal = {
    emoji: string;
    rank: number;
    tokenRank: number;
    durationRank: number;
    totalUsers: number;
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

export const SidebarMedalBadge = ({ medal, lang }: SidebarMedalBadgeProps) => (
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
            padding: '4px 0 2px 0',
            width: '100%',
            cursor: 'default',
            userSelect: 'none',
        }}
    >
        <span
            className={`sidebar-medal-emoji sidebar-medal--${medal.rank === 1 ? 'gold' : medal.rank === 2 ? 'silver' : 'bronze'}`}
            style={{ fontSize: '18px', lineHeight: 1 }}
        >{medal.emoji}</span>
        <span style={{ fontSize: '0.6rem', lineHeight: 1, marginTop: '2px', color: 'var(--theme-text-muted)', fontWeight: 600 }}>
            {lang === 'en' ? `#${medal.rank}` : `第${medal.rank}名`}
        </span>
    </div>
);
