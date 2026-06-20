import type { SidebarCreditDisplayFormatters, SidebarCurrentProviderTokenUsage, SidebarHubCredits } from '../../types/appShell';
import type { CodingAgentProgress, CodingAgentTurnSnapshot } from '../ai/CodingAgentProgressStatus';
import { localizeText } from '../../i18n';
import { CodingAgentSidebarStatus } from './CodingAgentSidebarStatus';

type SidebarSystemStatusProps = SidebarCreditDisplayFormatters & {
    lang: string;
    maclawLLMOnline: boolean;
    showLansenger?: boolean;
    remoteActivationStatus: any;
    qqBotStatus: string;
    telegramStatus: string;
    weixinStatus: string;
    lansengerStatus: string;
    backgroundTaskCount?: number;
    localLLMCacheEnabled?: boolean;
    sidebarCurrentProviderTokenUsage: SidebarCurrentProviderTokenUsage;
    sidebarHubCredits: SidebarHubCredits | null;
    unlimitedHubCreditText: string;
    noHubAuthorizationText: string;
    showHubCreditAction: boolean;
    openHubCreditsPage: () => void;
    openServiceRedeemPage?: () => void;
    openLLMSettingsPage?: () => void;
    openHubCardStorePage?: () => void;
    codingAgentProgress?: CodingAgentProgress | null;
    codingAgentTurnSnapshot?: CodingAgentTurnSnapshot | null;
};

const STATUS_DOT = String.fromCharCode(0x25cf);
const CREDIT_SEPARATOR = ` ${String.fromCharCode(0x00b7)} `;

const textForLang = localizeText;

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
        return withRetry('Period limit', '\u5468\u671f\u9650\u989d', '\u9031\u671f\u9650\u984d', 'to recover', '\u540e\u6062\u590d', '\u5f8c\u6062\u5fa9');
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
    backgroundTaskCount = 0,
    localLLMCacheEnabled = false,
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
    openServiceRedeemPage,
    openLLMSettingsPage,
    openHubCardStorePage,
    codingAgentProgress = null,
    codingAgentTurnSnapshot = null,
}: SidebarSystemStatusProps) => {
    const providerLabel = sidebarCurrentProviderTokenUsage.provider || textForLang(lang, 'Provider', '\u667a\u8c31\u7f16\u7a0b', '\u667a\u8b5c\u7de8\u7a0b');
    const isOfficialProvider = !!sidebarCurrentProviderTokenUsage.isHubService;
    const providerTitle = isOfficialProvider
        ? textForLang(lang, 'View or redeem MaClaw Official service', '\u67e5\u770b\u6216\u5151\u6362 MaClaw \u5b98\u65b9\u670d\u52a1', '\u67e5\u770b\u6216\u514c\u63db MaClaw \u5b98\u65b9\u670d\u52d9')
        : textForLang(lang, 'Configure LLM provider', '\u914d\u7f6e LLM \u670d\u52a1\u5546', '\u914d\u7f6e LLM \u670d\u52d9\u5546');
    const providerActionTitle = `${providerLabel}${CREDIT_SEPARATOR}${providerTitle}`;
    const openProviderTarget = isOfficialProvider ? openServiceRedeemPage : openLLMSettingsPage;
    const cardStoreTitle = textForLang(lang, 'Open MaClaw card store', '\u6253\u5f00 MaClaw \u670d\u52a1\u5361\u5546\u5e97', '\u6253\u958b MaClaw \u670d\u52d9\u5361\u5546\u5e97');
    const lowCreditWarning = isOfficialProvider && !!sidebarHubCredits && sidebarHubCredits.authorized && !sidebarHubCredits.unlimited && sidebarHubCredits.remaining < 1000;
    const isPeriodLimited = !!sidebarHubCredits && String(sidebarHubCredits.status || '').toLowerCase() === 'period_limited';
    const isPeriodLimitedStopped = isPeriodLimited && sidebarHubCredits!.serviceActive === false;
    const lowCreditTitle = isPeriodLimitedStopped
        ? textForLang(lang, 'Period quota used up, service stopped. Buy a top-up credits card to continue.', '\u5468\u671f\u9650\u989d\u5df2\u7528\u5c3d\uff0c\u670d\u52a1\u5df2\u505c\u6b62\u3002\u53ef\u8d2d\u4e70\u7eaf\u70b9\u5361\u8865\u5145\u7ee7\u7eed\u4f7f\u7528', '\u9031\u671f\u9650\u984d\u5df2\u7528\u76e1\uff0c\u670d\u52d9\u5df2\u505c\u6b62\u3002\u53ef\u8cfc\u8cb7\u7d14\u9ede\u5361\u88dc\u5145\u7e7c\u7e8c\u4f7f\u7528')
        : lowCreditWarning
            ? textForLang(lang, 'Credits below 1000, click to recharge', '\u4f59\u989d\u4e0d\u8db31000\uff0c\u70b9\u51fb\u5145\u503c', '\u9918\u984d\u4e0d\u8db31000\uff0c\u9ede\u64ca\u5145\u503c')
            : cardStoreTitle;
    const localCacheRequests = sidebarCurrentProviderTokenUsage.localCacheRequests ?? 0;
    const localCacheHits = sidebarCurrentProviderTokenUsage.localCacheHits ?? 0;
    const shouldDisplayCacheRate = !isOfficialProvider;
    const cacheRequests = localLLMCacheEnabled && !isOfficialProvider
        ? localCacheRequests
        : sidebarCurrentProviderTokenUsage.requests ?? 0;
    const cachedRequests = localLLMCacheEnabled && !isOfficialProvider
        ? localCacheHits
        : sidebarCurrentProviderTokenUsage.cachedRequests ?? 0;
    const cachedInput = sidebarCurrentProviderTokenUsage.cachedInput ?? 0;
    const cacheWrite = sidebarCurrentProviderTokenUsage.cacheWrite ?? 0;
    const isLocalCacheRate = localLLMCacheEnabled && !isOfficialProvider;
    const shouldShowCacheRate = shouldDisplayCacheRate && (cacheRequests > 0 || (localLLMCacheEnabled && !isOfficialProvider));
    const cacheHitRate = shouldShowCacheRate
        ? (cacheRequests > 0 ? Math.round((cachedRequests / cacheRequests) * 100) : 0)
        : null;
    const cacheTitle = cacheHitRate === null
        ? ''
        : isLocalCacheRate
            ? `${textForLang(lang, 'Local cache hit', '\u672c\u5730\u7f13\u5b58\u547d\u4e2d', '\u672c\u5730\u5feb\u53d6\u547d\u4e2d')}: ${cacheHitRate}%${CREDIT_SEPARATOR}${textForLang(lang, 'Hits', '\u547d\u4e2d', '\u547d\u4e2d')} ${cachedRequests}/${cacheRequests}`
            : `${textForLang(lang, 'Cache hit', '\u7f13\u5b58\u547d\u4e2d', '\u5feb\u53d6\u547d\u4e2d')}: ${cacheHitRate}%${CREDIT_SEPARATOR}${textForLang(lang, 'Read', '\u8bfb\u53d6', '\u8b80\u53d6')} ${formatSidebarTokens(cachedInput)}${CREDIT_SEPARATOR}${textForLang(lang, 'Write', '\u5199\u5165', '\u5beb\u5165')} ${formatSidebarTokens(cacheWrite)}`;
    const onlineText = textForLang(lang, 'Online', '\u5728\u7ebf', '\u5728\u7dda');
    const offlineText = textForLang(lang, 'Offline', '\u79bb\u7ebf', '\u96e2\u7dda');
    const imOnline = qqBotStatus === 'connected' || telegramStatus === 'connected' || weixinStatus === 'connected' || (showLansenger && lansengerStatus === 'connected');
    const backgroundTaskLabel = textForLang(lang, 'Background tasks', '\u540e\u53f0\u4efb\u52a1', '\u5f8c\u53f0\u4efb\u52d9');
    const isChineseLang = lang === 'zh-Hans' || lang === 'zh-Hant' || lang === 'zh';
    const backgroundTaskText = `${backgroundTaskLabel}${isChineseLang ? '\uff1a ' : ': '}${backgroundTaskCount}`;
    const renderStatusSignal = (label: string, on: boolean, extraTitle?: string) => (
        <span className="sidebar-system-status__signal" data-online={on ? 'true' : 'false'} title={extraTitle || `${STATUS_DOT} ${label} ${on ? onlineText : offlineText}`}>
            <span className="sidebar-system-status__dot" aria-hidden="true" />
            <span className="sidebar-system-status__signal-label">{label}</span>
        </span>
    );
    const hubOn = !!remoteActivationStatus?.activated;
    const hubTooltipLines: string[] = [`${STATUS_DOT} HUB ${hubOn ? onlineText : offlineText}`];
    if (hubOn) {
        const hubEmail = remoteActivationStatus?.email;
        const hubURL = remoteActivationStatus?.hub_url;
        const hubTenant = remoteActivationStatus?.tenant_name;
        if (hubEmail) hubTooltipLines.push(`${textForLang(lang, 'Email', '\u90ae\u7bb1', '\u90f5\u7bb1')}: ${hubEmail}`);
        if (hubURL) hubTooltipLines.push(`${textForLang(lang, 'Server', '\u670d\u52a1\u5668', '\u4f3a\u670d\u5668')}: ${hubURL}`);
        if (hubTenant) hubTooltipLines.push(`${textForLang(lang, 'Tenant', '\u79df\u6237', '\u79df\u6236')}: ${hubTenant}`);
    }
    const hubTooltip = hubTooltipLines.join('\n');
    const hubCreditStatus = String(sidebarHubCredits?.status || '').toLowerCase();
    const hubServicePeriodLimited = !!sidebarHubCredits && hubCreditStatus === 'period_limited';
    const hubServiceStoppedByPeriodLimit = hubServicePeriodLimited && sidebarHubCredits?.serviceActive === false;
    const openHubCreditAction = hubServicePeriodLimited
        ? (openServiceRedeemPage || openHubCreditsPage)
        : openHubCreditsPage;
    const hubCreditRetryText = sidebarHubCredits ? formatRetryAfter(sidebarHubCredits.retryAfterSeconds, sidebarHubCredits.retryAfterAt, lang) : '';
    const hubCreditStateText = formatHubCreditStateText(hubCreditStatus, hubCreditRetryText, lang);
    const periodLimitStopTitle = textForLang(
        lang,
        `MaClaw official service stopped: current period quota is exhausted.${hubCreditRetryText ? ` Recovers in ${hubCreditRetryText}.` : ''} Click to open Service Redeem.`,
        `MaClaw \u5b98\u65b9\u670d\u52a1\u5df2\u505c\u6b62\uff1a\u672c\u5468\u671f\u989d\u5ea6\u5df2\u7528\u5c3d\u3002${hubCreditRetryText ? `${hubCreditRetryText}\u540e\u6062\u590d\u3002` : ''}\u70b9\u51fb\u524d\u5f80\u670d\u52a1\u5151\u6362\u3002`,
        `MaClaw \u5b98\u65b9\u670d\u52d9\u5df2\u505c\u6b62\uff1a\u672c\u9031\u671f\u984d\u5ea6\u5df2\u7528\u76e1\u3002${hubCreditRetryText ? `${hubCreditRetryText}\u5f8c\u6062\u5fa9\u3002` : ''}\u9ede\u64ca\u524d\u5f80\u670d\u52d9\u5151\u63db\u3002`,
    );
    const periodLimitNoticeTitle = hubServiceStoppedByPeriodLimit
        ? periodLimitStopTitle
        : textForLang(
            lang,
            `Current MaClaw official route reached its period quota.${hubCreditRetryText ? ` Recovers in ${hubCreditRetryText}.` : ''} Click to open Service Redeem.`,
            `\u5f53\u524d MaClaw \u5b98\u65b9\u901a\u9053\u5df2\u8fbe\u5230\u672c\u5468\u671f\u9650\u989d\u3002${hubCreditRetryText ? `${hubCreditRetryText}\u540e\u6062\u590d\u3002` : ''}\u70b9\u51fb\u524d\u5f80\u670d\u52a1\u5151\u6362\u3002`,
            `\u76ee\u524d MaClaw \u5b98\u65b9\u901a\u9053\u5df2\u9054\u5230\u672c\u9031\u671f\u9650\u984d\u3002${hubCreditRetryText ? `${hubCreditRetryText}\u5f8c\u6062\u5fa9\u3002` : ''}\u9ede\u64ca\u524d\u5f80\u670d\u52d9\u5151\u63db\u3002`,
        );
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
                    {renderStatusSignal('LLM', maclawLLMOnline)}
                    {renderStatusSignal('HUB', hubOn, hubTooltip)}
                    {renderStatusSignal('IM', imOnline)}
                    <span className="sidebar-system-status__signal sidebar-system-status__background-tasks" title={backgroundTaskText}>
                        <span className="sidebar-system-status__signal-label">{backgroundTaskText}</span>
                    </span>
                </div>

                {codingAgentProgress && (
                    <CodingAgentSidebarStatus progress={codingAgentProgress} snapshot={codingAgentTurnSnapshot} lang={lang} />
                )}

                <div className="sidebar-system-status__usage">
                    {isOfficialProvider && openHubCardStorePage && (
                        <button
                            type="button"
                            className={`sidebar-system-status__provider-cart${(lowCreditWarning || isPeriodLimitedStopped) ? ' sidebar-system-status__provider-cart--alert' : ''}`}
                            onClick={openHubCardStorePage}
                            title={lowCreditTitle}
                            aria-label={lowCreditTitle}
                        >
                            <svg className="sidebar-system-status__provider-cart-icon" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                                <path d="M3 5h2.7l2.1 10.2a2 2 0 0 0 2 1.6h7.5a2 2 0 0 0 1.9-1.4l1.3-5.2H7.1" />
                                <path d="M10 20h.1M17 20h.1" />
                            </svg>
                        </button>
                    )}
                    {openProviderTarget ? (
                        <button
                            type="button"
                            className="sidebar-system-status__provider sidebar-system-status__provider--button"
                            onClick={openProviderTarget}
                            title={providerActionTitle}
                        >
                            {providerLabel}
                        </button>
                    ) : (
                        <span className="sidebar-system-status__provider" title={providerLabel}>
                            {providerLabel}
                        </span>
                    )}
                    {hubServicePeriodLimited && (
                        <button
                            type="button"
                            className="sidebar-system-status__stop-badge"
                            data-state={hubServiceStoppedByPeriodLimit ? 'stopped' : 'limited'}
                            onClick={openHubCreditAction}
                            title={periodLimitNoticeTitle}
                            aria-label={periodLimitNoticeTitle}
                        >
                            <span className="sidebar-system-status__stop-icon" aria-hidden="true">!</span>
                            <span>{hubServiceStoppedByPeriodLimit ? textForLang(lang, 'Stopped', '\u5df2\u505c\u6b62', '\u5df2\u505c\u6b62') : textForLang(lang, 'Limited', '\u9650\u989d', '\u9650\u984d')}</span>
                        </button>
                    )}
                    <span className="sidebar-system-status__tokens">
                        <strong title={cacheTitle || undefined}>{formatSidebarTokens(sidebarCurrentProviderTokenUsage.total)}</strong>
                        <span className="sidebar-system-status__tokens-unit">tokens</span>
                        {cacheHitRate !== null && (
                            <span className="sidebar-system-status__tokens-unit" title={cacheTitle}>
                                {CREDIT_SEPARATOR}{textForLang(lang, 'cache', '\u7f13\u5b58', '\u5feb\u53d6')} {cacheHitRate}%
                            </span>
                        )}
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
                            <button type="button" onClick={openHubCardStorePage ?? openHubCreditAction} className="sidebar-system-status__buy" title={cardStoreTitle}>
                                {textForLang(lang, 'Buy', '\u8d2d\u4e70', '\u8cfc\u8cb7')}
                            </button>
                        )}
                    </div>
                )}
            </div>
        </div>
    );
};
