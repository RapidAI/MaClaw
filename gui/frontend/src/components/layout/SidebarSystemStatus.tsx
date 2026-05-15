import type { SidebarHubCredits } from '../../types/appShell';
import type { CodingAgentProgress, CodingAgentTurnSnapshot } from '../ai/CodingAgentProgressStatus';
import { CodingAgentSidebarStatus } from './CodingAgentSidebarStatus';

type SidebarSystemStatusProps = {
    lang: string;
    maclawLLMOnline: boolean;
    showLansenger?: boolean;
    remoteActivationStatus: any;
    qqBotStatus: string;
    telegramStatus: string;
    weixinStatus: string;
    lansengerStatus: string;
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
    codingAgentProgress?: CodingAgentProgress | null;
    codingAgentTurnSnapshot?: CodingAgentTurnSnapshot | null;
};

const STATUS_DOT = String.fromCharCode(0x25cf);
const CREDIT_SEPARATOR = ` ${String.fromCharCode(0x00b7)} `;

const textForLang = (lang: string, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' || lang === 'zh' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

const formatRetryAfter = (seconds: number, retryAfterAt: string, lang: string) => {
    let totalSeconds = Number(seconds || 0);
    if (totalSeconds <= 0 && retryAfterAt) {
        const retryAt = new Date(retryAfterAt).getTime();
        if (!Number.isNaN(retryAt)) totalSeconds = Math.max(0, Math.ceil((retryAt - Date.now()) / 1000));
    }
    if (totalSeconds <= 0) return '';
    const minute = 60;
    const hour = 60 * minute;
    const day = 24 * hour;
    if (totalSeconds >= day) return textForLang(lang, `about ${Math.ceil(totalSeconds / day)}d`, `\u7ea6 ${Math.ceil(totalSeconds / day)} \u5929`, `\u7d04 ${Math.ceil(totalSeconds / day)} \u5929`);
    if (totalSeconds >= hour) return textForLang(lang, `about ${Math.ceil(totalSeconds / hour)}h`, `\u7ea6 ${Math.ceil(totalSeconds / hour)} \u5c0f\u65f6`, `\u7d04 ${Math.ceil(totalSeconds / hour)} \u5c0f\u6642`);
    return textForLang(lang, `about ${Math.max(1, Math.ceil(totalSeconds / minute))}m`, `\u7ea6 ${Math.max(1, Math.ceil(totalSeconds / minute))} \u5206\u949f`, `\u7d04 ${Math.max(1, Math.ceil(totalSeconds / minute))} \u5206\u9418`);
};

const formatHubCreditStateText = (status: string, retryText: string, lang: string) => {
    const withRetry = (enPrefix: string, zhPrefix: string, zhHantPrefix: string, enSuffix: string, zhSuffix: string, zhHantSuffix: string) => {
        if (!retryText) return textForLang(lang, enPrefix, zhPrefix, zhHantPrefix);
        const separator = CREDIT_SEPARATOR.trim();
        return textForLang(lang, `${enPrefix} ${separator} ${retryText} ${enSuffix}`, `${zhPrefix} ${separator} ${retryText}${zhSuffix}`, `${zhHantPrefix} ${separator} ${retryText}${zhHantSuffix}`);
    };
    if (status === 'period_limited') {
        return withRetry('Period limit', '\u5468\u671f\u9650\u6d41', '\u9031\u671f\u9650\u6d41', 'to recover', '\u540e\u6062\u590d', '\u5f8c\u6062\u5fa9');
    }
    if (status === 'queued') {
        return withRetry('Starts later', '\u5f85\u751f\u6548', '\u5f85\u751f\u6548', 'to start', '\u540e\u751f\u6548', '\u5f8c\u751f\u6548');
    }
    if (status === 'exhausted') {
        return textForLang(lang, 'Exhausted', '\u989d\u5ea6\u5df2\u7528\u5c3d', '\u984d\u5ea6\u5df2\u7528\u76e1');
    }
    if (status === 'expired') {
        return textForLang(lang, 'Expired', '\u6388\u6743\u5df2\u8fc7\u671f', '\u6388\u6b0a\u5df2\u904e\u671f');
    }
    return '';
};

export const SidebarSystemStatus = ({
    lang,
    maclawLLMOnline,
    showLansenger = false,
    remoteActivationStatus,
    qqBotStatus,
    telegramStatus,
    weixinStatus,
    lansengerStatus,
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
    codingAgentProgress = null,
    codingAgentTurnSnapshot = null,
}: SidebarSystemStatusProps) => {
    const providerLabel = sidebarCurrentProviderTokenUsage.provider || textForLang(lang, 'Provider', '\u667a\u8c31\u7f16\u7a0b', '\u667a\u8b5c\u7de8\u7a0b');
    const onlineText = textForLang(lang, 'Online', '\u5728\u7ebf', '\u5728\u7dda');
    const offlineText = textForLang(lang, 'Offline', '\u79bb\u7ebf', '\u96e2\u7dda');
    const imOnline = qqBotStatus === 'connected' || telegramStatus === 'connected' || weixinStatus === 'connected' || (showLansenger && lansengerStatus === 'connected');
    const statusSignals = [
        { label: 'LLM', on: maclawLLMOnline },
        { label: 'HUB', on: !!remoteActivationStatus?.activated },
        { label: 'IM', on: imOnline },
    ];
    const hubCreditStatus = String(sidebarHubCredits?.status || '').toLowerCase();
    const hubCreditRetryText = sidebarHubCredits ? formatRetryAfter(sidebarHubCredits.retryAfterSeconds, sidebarHubCredits.retryAfterAt, lang) : '';
    const hubCreditStateText = formatHubCreditStateText(hubCreditStatus, hubCreditRetryText, lang);
    const creditTitle = sidebarHubCredits
        ? textForLang(lang, 'Expires', '\u6709\u6548\u671f', '\u6709\u6548\u671f') + ': ' + formatSidebarHubExpiry(sidebarHubCredits)
            + CREDIT_SEPARATOR + textForLang(lang, 'Total', '\u603b\u91cf', '\u7e3d\u91cf') + ' ' + formatSidebarHubTotalCredits(sidebarHubCredits)
            + CREDIT_SEPARATOR + textForLang(lang, 'Used', '\u5df2\u7528', '\u5df2\u7528') + ' ' + formatSidebarHubUsedCredits(sidebarHubCredits)
            + CREDIT_SEPARATOR + (hubCreditStateText || (textForLang(lang, 'Left', '\u5269\u4f59', '\u5269\u9918') + ' ' + (sidebarHubCredits.authorized ? (sidebarHubCredits.unlimited ? unlimitedHubCreditText : formatSidebarCredit(sidebarHubCredits.remaining)) : noHubAuthorizationText)))
        : textForLang(lang, 'Credits unavailable', '\u989d\u5ea6\u4fe1\u606f\u6682\u4e0d\u53ef\u7528', '\u984d\u5ea6\u8cc7\u8a0a\u66ab\u4e0d\u53ef\u7528');
    const remainingCredit = sidebarHubCredits?.authorized
        ? (hubCreditStateText || (sidebarHubCredits.unlimited ? unlimitedHubCreditText : formatSidebarCredit(sidebarHubCredits.remaining)))
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

                {codingAgentProgress && (
                    <CodingAgentSidebarStatus progress={codingAgentProgress} snapshot={codingAgentTurnSnapshot} lang={lang} />
                )}

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
