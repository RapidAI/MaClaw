import type { SidebarHubCredits } from '../../types/appShell';

type SidebarSystemStatusProps = {
    lang: string;
    maclawLLMOnline: boolean;
    agentNetRunning: boolean;
    remoteActivationStatus: any;
    qqBotStatus: string;
    telegramStatus: string;
    weixinStatus: string;
    sidebarCurrentProviderTokenUsage: { provider: string; isHubService: boolean; input: number; output: number; total: number };
    sidebarHubCredits: SidebarHubCredits | null;
    formatSidebarTokens: (value: number) => string;
    formatSidebarHubExpiry: (credits: SidebarHubCredits | null) => string;
    formatSidebarHubTotalCredits: (credits: SidebarHubCredits | null) => string;
    formatSidebarHubUsedCredits: (credits: SidebarHubCredits | null) => string;
    formatSidebarCredit: (value: number) => string;
    unlimitedHubCreditText: string;
    noHubAuthorizationText: string;
    showHubCreditAction: boolean;
    openHubCreditsPage: () => void;
};

const sidebarCreditTone = {
    label: 'var(--theme-text-muted)',
    total: 'var(--theme-primary)',
    used: 'var(--theme-warning, #f59e0b)',
    remaining: 'var(--theme-success, #22c55e)',
};

const textForLang = (lang: string, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' || lang === 'zh' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

export const SidebarSystemStatus = ({
    lang,
    maclawLLMOnline,
    agentNetRunning,
    remoteActivationStatus,
    qqBotStatus,
    telegramStatus,
    weixinStatus,
    sidebarCurrentProviderTokenUsage,
    sidebarHubCredits,
    formatSidebarTokens,
    formatSidebarHubExpiry,
    formatSidebarHubTotalCredits,
    formatSidebarHubUsedCredits,
    formatSidebarCredit,
    unlimitedHubCreditText,
    noHubAuthorizationText,
    showHubCreditAction,
    openHubCreditsPage,
}: SidebarSystemStatusProps) => (
    <div style={{ flexShrink: 0, padding: '8px 12px 10px', borderTop: '1px solid var(--theme-border)', color: 'var(--theme-text-muted)' }}>
        <div style={{ display: 'flex', gap: '5px', marginBottom: '6px', alignItems: 'center', fontSize: '0.62rem', lineHeight: 1.15 }}>
            {[
                { label: 'LLM', on: maclawLLMOnline },
                { label: textForLang(lang, 'Net', '\u667a\u7f51', '\u667a\u7db2'), on: agentNetRunning },
                { label: textForLang(lang, 'Mob', '\u79fb\u52a8', '\u79fb\u52d5'), on: !!remoteActivationStatus?.activated },
                { label: 'IM', on: qqBotStatus === 'connected' || telegramStatus === 'connected' || weixinStatus === 'connected' },
            ].map(({ label, on }) => <span key={label}><span style={{ color: on ? 'var(--theme-primary)' : 'var(--theme-text-muted)' }}>\u25cf</span> {label}</span>)}
        </div>
        <div style={{ display: 'flex', justifyContent: 'space-between', gap: '8px', fontSize: '0.72rem', alignItems: 'center' }}>
            <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{sidebarCurrentProviderTokenUsage.provider || textForLang(lang, 'Provider', '\u667a\u8c31\u7f16\u7a0b', '\u667a\u8b5c\u7de8\u7a0b')}</span>
            <span style={{ flexShrink: 0, fontVariantNumeric: 'tabular-nums' }}>{formatSidebarTokens(sidebarCurrentProviderTokenUsage.total)} tokens</span>
        </div>
        {sidebarCurrentProviderTokenUsage.isHubService && (
            <div style={{ display: 'flex', alignItems: 'center', gap: '6px', marginTop: '5px', fontSize: '0.66rem', minWidth: 0 }}>
                <span style={{ minWidth: 0, flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontVariantNumeric: 'tabular-nums' }} title={sidebarHubCredits ? 'Expires: ' + formatSidebarHubExpiry(sidebarHubCredits) + ', total ' + formatSidebarHubTotalCredits(sidebarHubCredits) + ', used ' + formatSidebarHubUsedCredits(sidebarHubCredits) + ', remaining ' + (sidebarHubCredits.authorized ? (sidebarHubCredits.unlimited ? unlimitedHubCreditText : formatSidebarCredit(sidebarHubCredits.remaining)) : noHubAuthorizationText) : 'Credits unavailable'}>
                    <span style={{ color: sidebarCreditTone.label }}>{textForLang(lang, 'Exp', '\u6709\u6548\u671f', '\u6709\u6548\u671f')} </span><span>{formatSidebarHubExpiry(sidebarHubCredits)}</span>
                    <span style={{ color: sidebarCreditTone.label }}> \u00b7 {textForLang(lang, 'Total', '\u603b', '\u7e3d')} </span><span style={{ color: sidebarCreditTone.total }}>{formatSidebarHubTotalCredits(sidebarHubCredits)}</span>
                    <span style={{ color: sidebarCreditTone.label }}> \u00b7 {textForLang(lang, 'Used', '\u5df2\u7528', '\u5df2\u7528')} </span><span style={{ color: sidebarCreditTone.used }}>{formatSidebarHubUsedCredits(sidebarHubCredits)}</span>
                    <span style={{ color: sidebarCreditTone.label }}> \u00b7 {textForLang(lang, 'Left', '\u5269', '\u5269')} </span><span style={{ color: sidebarCreditTone.remaining }}>{sidebarHubCredits?.authorized ? (sidebarHubCredits.unlimited ? unlimitedHubCreditText : formatSidebarCredit(sidebarHubCredits.remaining)) : noHubAuthorizationText}</span>
                </span>
                {showHubCreditAction && (
                    <button type="button" onClick={openHubCreditsPage} style={{ flexShrink: 0, border: '1px solid var(--theme-danger)', background: 'color-mix(in srgb, var(--theme-danger) 12%, transparent)', color: 'var(--theme-danger)', borderRadius: '999px', padding: '2px 7px', fontSize: '0.64rem', fontWeight: 800, cursor: 'pointer' }}>
                        {textForLang(lang, 'Buy', '\u8d2d\u4e70', '\u8cfc\u8cb7')}
                    </button>
                )}
            </div>
        )}
    </div>
);
