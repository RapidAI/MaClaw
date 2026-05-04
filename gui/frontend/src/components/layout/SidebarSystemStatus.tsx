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

const STATUS_DOT = String.fromCharCode(0x25cf);
const CREDIT_SEPARATOR = ` ${String.fromCharCode(0x00b7)} `;

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
}: SidebarSystemStatusProps) => {
    const providerLabel = sidebarCurrentProviderTokenUsage.provider || textForLang(lang, 'Provider', '\u667a\u8c31\u7f16\u7a0b', '\u667a\u8b5c\u7de8\u7a0b');
    const onlineText = textForLang(lang, 'Online', '\u5728\u7ebf', '\u5728\u7dda');
    const offlineText = textForLang(lang, 'Offline', '\u79bb\u7ebf', '\u96e2\u7dda');
    const imOnline = qqBotStatus === 'connected' || telegramStatus === 'connected' || weixinStatus === 'connected';
    const statusSignals = [
        { label: 'LLM', on: maclawLLMOnline },
        { label: textForLang(lang, 'Net', '\u667a\u7f51', '\u667a\u7db2'), on: agentNetRunning },
        { label: textForLang(lang, 'Mob', '\u79fb\u52a8', '\u79fb\u52d5'), on: !!remoteActivationStatus?.activated },
        { label: 'IM', on: imOnline },
    ];
    const creditTitle = sidebarHubCredits
        ? textForLang(lang, 'Expires', '\u6709\u6548\u671f', '\u6709\u6548\u671f') + ': ' + formatSidebarHubExpiry(sidebarHubCredits)
            + CREDIT_SEPARATOR + textForLang(lang, 'Total', '\u603b\u91cf', '\u7e3d\u91cf') + ' ' + formatSidebarHubTotalCredits(sidebarHubCredits)
            + CREDIT_SEPARATOR + textForLang(lang, 'Used', '\u5df2\u7528', '\u5df2\u7528') + ' ' + formatSidebarHubUsedCredits(sidebarHubCredits)
            + CREDIT_SEPARATOR + textForLang(lang, 'Left', '\u5269\u4f59', '\u5269\u9918') + ' ' + (sidebarHubCredits.authorized ? (sidebarHubCredits.unlimited ? unlimitedHubCreditText : formatSidebarCredit(sidebarHubCredits.remaining)) : noHubAuthorizationText)
        : textForLang(lang, 'Credits unavailable', '\u989d\u5ea6\u4fe1\u606f\u6682\u4e0d\u53ef\u7528', '\u984d\u5ea6\u8cc7\u8a0a\u66ab\u4e0d\u53ef\u7528');
    const remainingCredit = sidebarHubCredits?.authorized
        ? (sidebarHubCredits.unlimited ? unlimitedHubCreditText : formatSidebarCredit(sidebarHubCredits.remaining))
        : noHubAuthorizationText;

    return (
        <div className="sidebar-system-status">
            <div className="sidebar-system-status__panel">
                <div className="sidebar-system-status__signals" aria-label="System status">
                    {statusSignals.map(({ label, on }) => (
                        <span className="sidebar-system-status__signal" data-online={on ? 'true' : 'false'} key={label} title={`${STATUS_DOT} ${label} ${on ? onlineText : offlineText}`}>
                            <span className="sidebar-system-status__dot" aria-hidden="true" />
                            <span className="sidebar-system-status__signal-label">{label}</span>
                        </span>
                    ))}
                </div>

                <div className="sidebar-system-status__usage">
                    <span className="sidebar-system-status__provider" title={providerLabel}>
                        {providerLabel}
                    </span>
                    <span className="sidebar-system-status__tokens">
                        <strong>{formatSidebarTokens(sidebarCurrentProviderTokenUsage.total)}</strong>
                        <span className="sidebar-system-status__tokens-unit">tokens</span>
                    </span>
                </div>

                {sidebarCurrentProviderTokenUsage.isHubService && (
                    <div className="sidebar-system-status__credits" title={creditTitle}>
                        <div className="sidebar-system-status__credit-grid">
                            <span className="sidebar-system-status__metric sidebar-system-status__metric--expiry">
                                <span className="sidebar-system-status__metric-label">{textForLang(lang, 'Valid', '\u6709\u6548\u671f', '\u6709\u6548\u671f')}</span>
                                <span className="sidebar-system-status__metric-value">{formatSidebarHubExpiry(sidebarHubCredits)}</span>
                            </span>
                            <span className="sidebar-system-status__metric sidebar-system-status__metric--total">
                                <span className="sidebar-system-status__metric-label">{textForLang(lang, 'Total', '\u603b\u91cf', '\u7e3d\u91cf')}</span>
                                <span className="sidebar-system-status__metric-value">{formatSidebarHubTotalCredits(sidebarHubCredits)}</span>
                            </span>
                            <span className="sidebar-system-status__metric sidebar-system-status__metric--used">
                                <span className="sidebar-system-status__metric-label">{textForLang(lang, 'Used', '\u5df2\u7528', '\u5df2\u7528')}</span>
                                <span className="sidebar-system-status__metric-value">{formatSidebarHubUsedCredits(sidebarHubCredits)}</span>
                            </span>
                            <span className="sidebar-system-status__metric sidebar-system-status__metric--remaining">
                                <span className="sidebar-system-status__metric-label">{textForLang(lang, 'Left', '\u5269\u4f59', '\u5269\u9918')}</span>
                                <span className="sidebar-system-status__metric-value">{remainingCredit}</span>
                            </span>
                        </div>
                        {showHubCreditAction && (
                            <button type="button" onClick={openHubCreditsPage} className="sidebar-system-status__buy">
                                {textForLang(lang, 'Buy', '\u8d2d\u4e70', '\u8cfc\u8cb7')}
                            </button>
                        )}
                    </div>
                )}
            </div>
        </div>
    );
};
