import ReactMarkdown from 'react-markdown';
import type { MouseEvent } from 'react';
import { useState, useEffect, useCallback, useRef } from 'react';
import remarkGfm from 'remark-gfm';
import rehypeRaw from 'rehype-raw';
import { BrowserOpenURL, EventsOn } from '../../wailsjs/runtime';
import { ProbeRemoteHub, ReadErrorLog, GetHubUserRanking, SendRemoteRegistrationContactCode, VerifyRemoteRegistrationContactCode } from '../../wailsjs/go/main/App';
import type { main } from '../../wailsjs/go/models';
import { useSafeBackdropDismiss } from '../hooks/useSafeBackdropDismiss';
import { remoteCardStyle, remoteMutedCardStyle, remoteSectionTitleStyle, remoteBodyTextStyle } from './remote/styles';
import { MemoryHealthDialog } from './MemoryHealthDialog';
import { SecurityEventsDialog } from './SecurityEventsDialog';

// Load Monoton font for the stylized "6" in product name
const monotonLink = document.querySelector('link[href*="Monoton"]');
if (!monotonLink) {
    const link = document.createElement('link');
    link.rel = 'stylesheet';
    link.href = 'https://fonts.googleapis.com/css2?family=Monoton&display=swap';
    document.head.appendChild(link);
}

type BrandInfo = {
    id: string;
    displayName: string;
    displayNameCN: string;
    slogan: string;
    author: string;
    businessContact: string;
    websiteURL: string;
    githubURL: string;
    iconPath: string;
};

type RemoteProbeIdentity = {
    tenant_id?: string;
    tenant_name?: string;
};

type AboutPanelProps = {
    currentIcon: string;
    brandInfo: BrandInfo | null;
    appVersion: string;
    buildNumber: string;
    thanksContent: string;
    config?: Partial<main.AppConfig> | null;
    t: (key: string) => string;
    onOpenWebsite: () => void;
    onCheckUpdate: () => void;
    onShowInstallLog: () => void;
    onOpenBugReport: () => void;
    onOpenGithub: () => void;
    onRegister: () => void;
    onClearRegistration: () => void;
    onRegistrationContactUpdated?: () => void;
};

const localHubCenterPattern = /(?:^|\/\/|\[)(?:127(?:\.\d{1,3}){3}|0\.0\.0\.0|::1|localhost)(?::|\]|\/|$)/i;
const HUB_RANKING_REFRESH_INTERVAL_MS = 30 * 60_000;
const HUB_RANKING_STARTUP_RETRY_DELAYS_MS = [30_000, 2 * 60_000, 8 * 60_000] as const;

const phoneAccountPrefix = 'phone:';

function registrationEmailFromConfig(value: unknown): string {
    const email = String(value || '').trim();
    return email.toLowerCase().startsWith(phoneAccountPrefix) ? '' : email;
}

function registrationPhoneFromConfig(remoteMobile: unknown, remoteEmail: unknown): string {
    const mobile = String(remoteMobile || '').trim();
    if (mobile) return mobile;
    const account = String(remoteEmail || '').trim();
    if (account.toLowerCase().startsWith(phoneAccountPrefix)) {
        return account.slice(phoneAccountPrefix.length).trim();
    }
    return '';
}

function registrationProbeIdentityFromConfig(remoteEmail: unknown, remoteMobile: unknown): string {
    const account = String(remoteEmail || '').trim();
    if (account) return account;
    const mobile = String(remoteMobile || '').trim();
    return mobile ? `${phoneAccountPrefix}${mobile}` : '';
}

const MarkdownLink = ({ node, ...props }: any) => (
    <a
        {...props}
        className="themed-markdown-link"
        onClick={(e: MouseEvent<HTMLAnchorElement>) => {
            e.preventDefault();
            if (props.href) BrowserOpenURL(props.href);
        }}
    />
);

export function AboutPanel({
    currentIcon,
    brandInfo,
    appVersion,
    buildNumber,
    thanksContent,
    config,
    t,
    onOpenWebsite,
    onCheckUpdate,
    onShowInstallLog,
    onOpenBugReport,
    onOpenGithub,
    onRegister,
    onClearRegistration,
    onRegistrationContactUpdated,
}: AboutPanelProps) {
    const slogan = brandInfo?.slogan || t("slogan");
    const author = brandInfo?.author || 'Dr. Daniel';
    const businessContact = brandInfo?.businessContact || t("businessCooperation");
    const showGithubActions = Boolean(brandInfo?.githubURL) || brandInfo?.id !== 'qianxin';
    const emptyValue = t("aboutUnsetValue");
    const [remoteTenant, setRemoteTenant] = useState<{ id?: string; name?: string }>({
        id: String(config?.remote_tenant_id || ''),
        name: String(config?.remote_tenant_name || ''),
    });

    useEffect(() => {
        setRemoteTenant({
            id: String(config?.remote_tenant_id || ''),
            name: String(config?.remote_tenant_name || ''),
        });
    }, [config?.remote_hub_url, config?.remote_email, config?.remote_tenant_id, config?.remote_tenant_name]);

    const probeIdentity = registrationProbeIdentityFromConfig(config?.remote_email, (config as any)?.remote_mobile);
    useEffect(() => {
        const hubURL = String(config?.remote_hub_url || '').trim();
        if (!hubURL || !probeIdentity || remoteTenant.id || remoteTenant.name) return;
        let cancelled = false;
        ProbeRemoteHub(hubURL, probeIdentity)
            .then((result: RemoteProbeIdentity) => {
                if (cancelled) return;
                const id = String(result?.tenant_id || '').trim();
                const name = String(result?.tenant_name || '').trim();
                if (id || name) setRemoteTenant({ id, name });
            })
            .catch(() => undefined);
        return () => { cancelled = true; };
    }, [config?.remote_hub_url, probeIdentity, remoteTenant.id, remoteTenant.name]);

    const hasRegisteredMachine = String(config?.remote_machine_id || '').trim() !== '' && String(config?.remote_machine_token || '').trim() !== '';
    const tenantLabel = remoteTenant.name || remoteTenant.id || emptyValue;
    const registeredName = hasRegisteredMachine ? (String(config?.remote_nickname || '').trim() || String(config?.remote_machine_name || '').trim() || String(config?.remote_machine_id || '').trim() || emptyValue) : emptyValue;
    const hubURL = String(config?.remote_hub_url || '').trim() || emptyValue;
    // Display the first discovered public HubCenter URL (from the discovery list),
    // falling back to the preferred URL. HubCenter is a public control-plane
    // endpoint, so loopback addresses are ignored for this identity view.
    const hubCenterURL = (() => {
        const discoveredList = (config as any)?.remote_hubcenter_urls as string[] | undefined;
        if (Array.isArray(discoveredList) && discoveredList.length > 0) {
            const publicURL = discoveredList.find(u => {
                const trimmed = u.trim();
                return trimmed && !localHubCenterPattern.test(trimmed);
            });
            if (publicURL) return publicURL.trim();
        }
        const preferred = String(config?.remote_hubcenter_url || '').trim();
        if (localHubCenterPattern.test(preferred)) return emptyValue;
        return preferred || emptyValue;
    })();
    const remoteEmailRaw = registrationEmailFromConfig(config?.remote_email);
    const remoteMobileRaw = registrationPhoneFromConfig((config as any)?.remote_mobile, config?.remote_email);
    const remoteEmail = remoteEmailRaw || emptyValue;
    const remoteMobile = remoteMobileRaw || emptyValue;
    const machineID = String(config?.remote_machine_id || '').trim() || emptyValue;
    const [contactDialog, setContactDialog] = useState<null | { kind: 'email' | 'phone' }>(null);
    const [contactValue, setContactValue] = useState('');
    const [contactCode, setContactCode] = useState('');
    const [contactMessage, setContactMessage] = useState('');
    const [contactBusy, setContactBusy] = useState(false);
    const [contactBusyAction, setContactBusyAction] = useState<'' | 'send' | 'verify'>('');
    const [contactCodeSent, setContactCodeSent] = useState(false);
    const contactDialogTitle = contactDialog?.kind === 'phone' ? t("aboutSetRegisterPhone") : t("aboutSetRegisterEmail");
    const contactValueLabel = contactDialog?.kind === 'phone' ? t("aboutRegisterPhone") : t("aboutRegisterEmail");
    const contactInputType = contactDialog?.kind === 'phone' ? 'tel' : 'email';
    const openContactDialog = (kind: 'email' | 'phone') => {
        setContactDialog({ kind });
        setContactValue(kind === 'phone' ? remoteMobileRaw : remoteEmailRaw);
        setContactCode('');
        setContactMessage('');
        setContactCodeSent(false);
    };
    const sendContactCode = async () => {
        if (!contactDialog) return;
        setContactBusy(true);
        setContactBusyAction('send');
        setContactMessage('');
        setContactCode('');
        setContactCodeSent(false);
        try {
            const result = await SendRemoteRegistrationContactCode(contactDialog.kind, contactValue.trim()) as any;
            const length = Number(result?.code_length || 6);
            const expires = Number(result?.expires_min || 5);
            setContactCodeSent(true);
            setContactMessage(t("aboutContactCodeSent").replace("{length}", String(length)).replace("{minutes}", String(expires)));
        } catch (err: any) {
            setContactMessage(String(err?.message || err || t("aboutContactCodeFailed")));
        } finally {
            setContactBusy(false);
            setContactBusyAction('');
        }
    };
    const verifyContactCode = async () => {
        if (!contactDialog) return;
        setContactBusy(true);
        setContactBusyAction('verify');
        setContactMessage('');
        try {
            await VerifyRemoteRegistrationContactCode(contactDialog.kind, contactValue.trim(), contactCode.trim());
            setContactMessage(t("aboutContactVerified"));
            onRegistrationContactUpdated?.();
            window.setTimeout(() => setContactDialog(null), 350);
        } catch (err: any) {
            setContactMessage(String(err?.message || err || t("aboutContactVerifyFailed")));
        } finally {
            setContactBusy(false);
            setContactBusyAction('');
        }
    };

    const productName = (() => {
        if (!brandInfo?.id || brandInfo.id === 'maclaw') {
            return t("aboutProductName");
        }
        if (brandInfo.id === 'qianxin') {
            return '\u864e\u722a 6 \u7a0b\u542f';
        }
        if (brandInfo.id === 'metastaff') {
            return '\u667a\u5458 6 \u7a0b\u542f';
        }
        const cnName = String(brandInfo.displayNameCN || '').trim();
        const displayName = String(brandInfo.displayName || '').trim();
        return [cnName, displayName].filter(Boolean).join(' ') || t("aboutProductName");
    })();

    // Render product name with Monoton-styled "6"
    const renderProductName = () => {
        const raw = productName;
        const sixIndex = raw.indexOf('6');
        if (sixIndex === -1) {
            return <>{raw}</>;
        }
        return (
            <>
                {raw.slice(0, sixIndex)}
                <span style={{
                    fontFamily: "'Monoton', cursive",
                    fontSize: '1.15em',
                    verticalAlign: 'baseline',
                    letterSpacing: '-0.02em',
                    color: 'var(--theme-primary-strong)',
                }}>6</span>
                {raw.slice(sixIndex + 1)}
            </>
        );
    };

    const [showHealthDialog, setShowHealthDialog] = useState(false);
    const [showSecurityEvents, setShowSecurityEvents] = useState(false);
    const [showErrorLog, setShowErrorLog] = useState(false);
    const [errorLogLines, setErrorLogLines] = useState<string[]>([]);
    const [errorLogLoading, setErrorLogLoading] = useState(false);
    const { backdropProps: errorLogBackdropProps, dialogProps: errorLogDialogProps } = useSafeBackdropDismiss(() => setShowErrorLog(false));

    // Hub user ranking stats
    const [ranking, setRanking] = useState<{ totalTokens: number; durationSeconds: number; tokenRank: number; durationRank: number; totalUsers: number } | null>(null);
    const rankingLoadedRef = useRef(false);

    const fetchRanking = useCallback((): Promise<boolean> => {
        if (!hasRegisteredMachine) {
            rankingLoadedRef.current = false;
            setRanking(null);
            return Promise.resolve(false);
        }
        return GetHubUserRanking()
            .then((result) => {
                const r = result as { total_tokens?: number; duration_seconds?: number; token_rank?: number; duration_rank?: number; total_users?: number; error?: string } | null;
                if (!r || r.error) {
                    setRanking(null);
                    return false;
                }
                rankingLoadedRef.current = true;
                setRanking({
                    totalTokens: r.total_tokens || 0,
                    durationSeconds: r.duration_seconds || 0,
                    tokenRank: r.token_rank || 0,
                    durationRank: r.duration_rank || 0,
                    totalUsers: r.total_users || 0,
                });
                return true;
            })
            .catch(() => {
                setRanking(null);
                return false;
            });
    }, [hasRegisteredMachine]);

    useEffect(() => {
        if (!hasRegisteredMachine) {
            rankingLoadedRef.current = false;
            setRanking(null);
            return;
        }
        let cancelled = false;
        const retryTimers: number[] = [];
        const attempt = (retryIndex: number) => {
            if (rankingLoadedRef.current) return;
            fetchRanking().then((loaded) => {
                if (cancelled || loaded || rankingLoadedRef.current || retryIndex >= HUB_RANKING_STARTUP_RETRY_DELAYS_MS.length) return;
                const timer = window.setTimeout(() => attempt(retryIndex + 1), HUB_RANKING_STARTUP_RETRY_DELAYS_MS[retryIndex]);
                retryTimers.push(timer);
            });
        };
        attempt(0);
        return () => {
            cancelled = true;
            retryTimers.forEach(timer => window.clearTimeout(timer));
        };
    }, [fetchRanking, hasRegisteredMachine]);

    // Stable ref to latest fetchRanking — avoids re-subscribing event listener on identity change.
    const fetchRankingRef = useRef(fetchRanking);
    useEffect(() => { fetchRankingRef.current = fetchRanking; }, [fetchRanking]);

    useEffect(() => {
        if (!hasRegisteredMachine) return;
        const interval = window.setInterval(() => {
            fetchRankingRef.current();
        }, HUB_RANKING_REFRESH_INTERVAL_MS);
        return () => window.clearInterval(interval);
    }, [hasRegisteredMachine]);

    // Refresh ranking when token usage changes — throttled (60s min interval)
    useEffect(() => {
        if (!hasRegisteredMachine) return;
        let throttleTimer: number | undefined;
        let pending = false;
        const onTokenUsageChanged = () => {
            if (throttleTimer !== undefined) { pending = true; return; }
            throttleTimer = window.setTimeout(() => {
                throttleTimer = undefined;
                fetchRankingRef.current();
                if (pending) {
                    pending = false;
                    throttleTimer = window.setTimeout(() => { throttleTimer = undefined; fetchRankingRef.current(); }, 60_000);
                }
            }, 5_000);
        };
        const unsubscribe = EventsOn("llm-token-usage-changed", onTokenUsageChanged);
        return () => {
            window.clearTimeout(throttleTimer);
            if (typeof unsubscribe === 'function') unsubscribe();
        };
    }, [hasRegisteredMachine]);

    useEffect(() => {
        if (!showErrorLog) return;
        setErrorLogLines([]);
        setErrorLogLoading(true);
        ReadErrorLog()
            .then((lines: string[]) => setErrorLogLines(lines || []))
            .catch((err: unknown) => {
                console.error('ReadErrorLog failed:', err);
                setErrorLogLines([`Error loading log: ${err}`]);
            })
            .finally(() => setErrorLogLoading(false));
    }, [showErrorLog]);

    // Format duration: seconds → "Xh Ym" or "Ym"
    const formatDuration = (seconds: number): string => {
        if (seconds <= 0) return '-';
        const hours = Math.floor(seconds / 3600);
        const minutes = Math.floor((seconds % 3600) / 60);
        if (hours > 0) return `${hours}h ${minutes}m`;
        return `${minutes}m`;
    };

    // Format tokens: add thousand separators
    const formatTokens = (tokens: number): string => {
        if (tokens <= 0) return '-';
        return tokens.toLocaleString();
    };

    // Format rank with medal emoji for top 3
    const formatRank = (rank: number, total: number): string => {
        if (rank <= 0) return '';
        const medal = rank === 1 ? '🥇' : rank === 2 ? '🥈' : rank === 3 ? '🥉' : '';
        return `${medal} ${t("aboutRankPrefix")}${rank}/${total}${t("aboutRankSuffix")}`;
    };

    const formatRankingValue = (value: string, rank: number, total: number): string => {
        const rankText = formatRank(rank, total);
        return rankText ? `${value} ${rankText}` : value;
    };

    const bestRankingIcon = (() => {
        const ranks = [ranking?.tokenRank || 0, ranking?.durationRank || 0].filter(rank => rank > 0);
        if (ranks.length === 0) return '🏅';
        const best = Math.min(...ranks);
        if (best === 1) return '🏆';
        if (best === 2) return '🥈';
        if (best === 3) return '🥉';
        return '🏅';
    })();

    return (
        <div className="about-page">
            <div className="about-page__container">
                <section className="about-hero-card" style={remoteCardStyle}>
                    <div className="about-hero-card__icon-wrap" style={remoteMutedCardStyle}>
                        <img src={currentIcon} alt="Logo" className="about-hero-card__icon" />
                    </div>
                    <div className="about-hero-card__body">
                        <h2 className="about-hero-card__title">{renderProductName()}</h2>
                        <p className="about-hero-card__slogan">{slogan}</p>
                        <div className="about-version-row">
                            <span className="about-version-badge">{t("version")} {appVersion}</span>
                            <button className="btn-link about-update-inline-button" onClick={onCheckUpdate}>{t("onlineUpdate")}</button>
                        </div>
                        <div className="about-meta-inline">
                            <span>{t("author")}: {author}</span>
                            <span className="about-meta-dot">•</span>
                            <span>{businessContact}</span>
                        </div>
                    </div>
                </section>

                <section className="about-identity-card" style={remoteCardStyle}>
                    <div className="about-card-heading">
                        <div>
                            <p className="about-actions-card__desc" style={remoteBodyTextStyle}>
                                {t("aboutIdentityDesc")}
                            </p>
                        </div>
                        {hasRegisteredMachine ? (
                            <button
                                className="about-status-pill is-online"
                                style={{ cursor: 'pointer', border: 'none', background: 'var(--theme-danger-bg)', color: 'var(--theme-danger)' }}
                                onClick={onClearRegistration}
                                title={t("aboutClearRegistration")}
                            >
                                {t("aboutClearBtn")}
                            </button>
                        ) : (
                            <button
                                className="about-status-pill"
                                style={{ cursor: 'pointer', border: 'none', background: 'var(--theme-primary-soft)', color: 'var(--theme-primary)' }}
                                onClick={onRegister}
                                title={t("aboutRegisterHub")}
                            >
                                {t("aboutRegisterBtn")}
                            </button>
                        )}
                    </div>
                    <dl className="about-identity-table">
                        <div className="about-identity-row">
                            <div className="about-identity-item about-identity-item--strong">
                                <dt className="about-kv-label">{t("aboutTenantName")}</dt>
                                <dd className="about-identity-value about-identity-value--strong">{tenantLabel}</dd>
                            </div>
                            <div className="about-identity-item about-identity-item--strong">
                                <dt className="about-kv-label">{t("aboutRegisteredName")}</dt>
                                <dd className="about-identity-value about-identity-value--strong">{registeredName}</dd>
                            </div>
                        </div>
                        <div className="about-identity-row">
                            <div className="about-identity-item">
                                <dt className="about-kv-label">{t("aboutHubUrl")}</dt>
                                <dd className="about-identity-value about-identity-value--muted">{hubURL}</dd>
                            </div>
                            <div className="about-identity-item">
                                <dt className="about-kv-label">{t("aboutHubCenterUrl")}</dt>
                                <dd className="about-identity-value about-identity-value--muted">{hubCenterURL}</dd>
                            </div>
                        </div>
                        <div className="about-identity-row">
                            <div className="about-identity-item">
                                <dt className="about-kv-label">{t("aboutRegisterPhone")}</dt>
                                <dd className="about-identity-value about-identity-value--muted about-contact-value">
                                    <span>{remoteMobile}</span>
                                    {hasRegisteredMachine && !remoteMobileRaw && (
                                        <button type="button" className="about-inline-action" onClick={() => openContactDialog('phone')}>{t("aboutSetContactBtn")}</button>
                                    )}
                                </dd>
                            </div>
                            <div className="about-identity-item">
                                <dt className="about-kv-label">{t("aboutRegisterEmail")}</dt>
                                <dd className="about-identity-value about-identity-value--muted about-contact-value">
                                    <span>{remoteEmail}</span>
                                    {hasRegisteredMachine && !remoteEmailRaw && (
                                        <button type="button" className="about-inline-action" onClick={() => openContactDialog('email')}>{t("aboutSetContactBtn")}</button>
                                    )}
                                </dd>
                            </div>
                        </div>
                        <div className="about-identity-row">
                            <div className="about-identity-item">
                                <dt className="about-kv-label">{t("remoteActivation")}</dt>
                                <dd className="about-identity-value about-identity-value--muted">{hasRegisteredMachine ? t("remoteActivated") : t("aboutNotRegistered")}</dd>
                            </div>
                            <div className="about-identity-item">
                                <dt className="about-kv-label">{t("aboutMachineId")}</dt>
                                <dd className="about-identity-value about-identity-value--mono">{machineID}</dd>
                            </div>
                        </div>
                        {hasRegisteredMachine && (
                            <div className="about-identity-row">
                                <div className="about-identity-item">
                                    <dt className="about-kv-label">
                                        {bestRankingIcon} {t("aboutTotalOnline")} <span className="about-rank-badge" style={{ marginLeft: 0 }}>({t("aboutPeriodMonthly")})</span>
                                    </dt>
                                    <dd className="about-identity-value about-identity-value--muted">
                                        {ranking
                                            ? formatRankingValue(formatDuration(ranking.durationSeconds), ranking.durationRank, ranking.totalUsers)
                                            : emptyValue}
                                    </dd>
                                </div>
                                <div className="about-identity-item">
                                    <dt className="about-kv-label">
                                        {t("aboutTotalTokens")} <span className="about-rank-badge" style={{ marginLeft: 0 }}>({t("aboutPeriodMonthly")})</span>
                                    </dt>
                                    <dd className="about-identity-value about-identity-value--muted">
                                        {ranking
                                            ? formatRankingValue(formatTokens(ranking.totalTokens), ranking.tokenRank, ranking.totalUsers)
                                            : emptyValue}
                                    </dd>
                                </div>
                            </div>
                        )}
                    </dl>
                </section>

                <section className="about-actions-card" style={remoteCardStyle}>
                    <div className="about-card-heading">
                        <div>
                            <div style={remoteSectionTitleStyle}>{t("quickActionsTitle")}</div>
                        </div>
                    </div>
                    <div className="about-action-grid">
                        <button className="btn-link about-action-button" onClick={onOpenWebsite}>{t("officialWebsite")}</button>
                        <button className="btn-link about-action-button" onClick={onShowInstallLog}>{t("installLog")}</button>
                        <button className="btn-link about-action-button" onClick={() => setShowHealthDialog(true)}>{t("memoryHealth")}</button>
                        <button className="btn-link about-action-button" onClick={() => setShowSecurityEvents(true)}>{t("securityEvents")}</button>
                        <button className="btn-link about-action-button" onClick={() => setShowErrorLog(true)}>{t("errorLog")}</button>
                        {showGithubActions && (
                            <>
                                <button className="btn-link about-action-button" onClick={onOpenBugReport}>{t("bugReport")}</button>
                                <button className="btn-link about-action-button" onClick={onOpenGithub}>{t("codeRepository")}</button>
                            </>
                        )}
                    </div>
                </section>

                {thanksContent.trim() && (
                    <section className="about-actions-card about-thanks-card" style={remoteCardStyle}>
                        <div className="about-actions-card__header about-thanks-header">
                            <div>
                                <div style={remoteSectionTitleStyle} className="about-thanks-title">{t("thanks")}</div>
                            </div>
                        </div>
                        <div className="about-thanks-content markdown-content">
                            <ReactMarkdown
                                remarkPlugins={[remarkGfm]}
                                // @ts-ignore
                                rehypePlugins={[rehypeRaw]}
                                components={{ a: MarkdownLink }}
                            >
                                {thanksContent}
                            </ReactMarkdown>
                        </div>
                    </section>
                )}
            </div>
            <MemoryHealthDialog
                open={showHealthDialog}
                onClose={() => setShowHealthDialog(false)}
                t={t}
            />
            <SecurityEventsDialog
                open={showSecurityEvents}
                onClose={() => setShowSecurityEvents(false)}
                t={t}
            />
            {contactDialog && (
                <div className="modal-overlay">
                    <div className="modal-content about-contact-dialog">
                        <div className="about-contact-dialog__header">
                            <h3>{contactDialogTitle}</h3>
                            <button className="modal-close" onClick={() => setContactDialog(null)}>&times;</button>
                        </div>
                        <p className="about-contact-dialog__desc">{t("aboutContactDialogDesc")}</p>
                        <label className="form-label">{contactValueLabel}</label>
                        <input
                            className="form-input"
                            type={contactInputType}
                            value={contactValue}
                            onChange={e => { setContactValue(e.target.value); setContactCodeSent(false); setContactMessage(''); }}
                            placeholder={contactDialog.kind === 'phone' ? t("aboutRegisterPhonePlaceholder") : t("aboutRegisterEmailPlaceholder")}
                        />
                        <label className="form-label">{t("aboutVerifyCode")}</label>
                        <div className="about-contact-dialog__code-row">
                            <input
                                className="form-input"
                                value={contactCode}
                                onChange={e => setContactCode(e.target.value)}
                                placeholder={t("aboutVerifyCodePlaceholder")}
                            />
                            <button className="btn-link about-action-button" disabled={contactBusy || !contactValue.trim()} onClick={sendContactCode}>
                                {contactBusyAction === 'send' ? t("loading") : t("aboutSendCodeBtn")}
                            </button>
                        </div>
                        {contactMessage && <div className="about-contact-dialog__message">{contactMessage}</div>}
                        <div className="modal-actions">
                            <button className="btn-hide" onClick={() => setContactDialog(null)}>{t("cancel")}</button>
                            <button className="btn-primary" disabled={contactBusy || !contactCode.trim() || !contactCodeSent} onClick={verifyContactCode}>
                                {contactBusyAction === 'verify' ? t("loading") : t("aboutVerifyAndSaveBtn")}
                            </button>
                        </div>
                    </div>
                </div>
            )}
            {showErrorLog && (
                <div className="modal-overlay" {...errorLogBackdropProps}>
                    <div
                        className="modal-content"
                        style={{ width: '700px', maxWidth: '90vw' }}
                        {...errorLogDialogProps}
                    >
                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '15px' }}>
                            <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                                <h3 style={{ margin: 0, color: 'var(--theme-primary)' }}>{t("errorLogTitle")}</h3>
                                {!errorLogLoading && errorLogLines.length > 0 && (
                                    <span style={{
                                        fontSize: '0.75rem',
                                        color: 'var(--theme-danger)',
                                        backgroundColor: 'var(--theme-surface-muted)',
                                        padding: '2px 8px',
                                        borderRadius: '10px',
                                        fontWeight: 600
                                    }}>
                                        {errorLogLines.length}
                                    </span>
                                )}
                            </div>
                            <button className="modal-close" onClick={() => setShowErrorLog(false)}>&times;</button>
                        </div>
                        <div
                            className="elegant-scrollbar"
                            style={{
                                backgroundColor: 'var(--theme-surface-muted)',
                                color: 'var(--theme-text-primary)',
                                padding: '15px',
                                borderRadius: '8px',
                                height: '400px',
                                overflowY: 'auto',
                                fontFamily: 'monospace',
                                fontSize: '0.8rem',
                                whiteSpace: 'pre-wrap',
                                textAlign: 'left',
                                marginBottom: '15px'
                            }}>
                            {errorLogLoading ? (
                                <div style={{ color: 'var(--theme-text-muted)', fontStyle: 'italic' }}>
                                    {t("loading")}...
                                </div>
                            ) : errorLogLines.length === 0 ? (
                                <div style={{ color: 'var(--theme-text-muted)', fontStyle: 'italic' }}>
                                    {t("errorLogEmpty")}
                                </div>
                            ) : (
                                errorLogLines.map((line, index) => {
                                    const isFatal = /fatal|panic/i.test(line);
                                    return (
                                        <div key={index} style={{
                                            color: 'var(--theme-danger)',
                                            fontWeight: isFatal ? 600 : 'normal',
                                            marginBottom: '2px',
                                            borderBottom: '1px solid var(--theme-border)',
                                            paddingBottom: '2px'
                                        }}>
                                            {line}
                                        </div>
                                    );
                                })
                            )}
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
}
