import ReactMarkdown from 'react-markdown';
import type { MouseEvent } from 'react';
import { useState, useEffect } from 'react';
import remarkGfm from 'remark-gfm';
import rehypeRaw from 'rehype-raw';
import { BrowserOpenURL } from '../../wailsjs/runtime';
import { ProbeRemoteHub, ReadErrorLog } from '../../wailsjs/go/main/App';
import type { main } from '../../wailsjs/go/models';
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
};

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

    useEffect(() => {
        const hubURL = String(config?.remote_hub_url || '').trim();
        const email = String(config?.remote_email || '').trim();
        if (!hubURL || !email || remoteTenant.id || remoteTenant.name) return;
        let cancelled = false;
        ProbeRemoteHub(hubURL, email)
            .then((result: RemoteProbeIdentity) => {
                if (cancelled) return;
                const id = String(result?.tenant_id || '').trim();
                const name = String(result?.tenant_name || '').trim();
                if (id || name) setRemoteTenant({ id, name });
            })
            .catch(() => undefined);
        return () => { cancelled = true; };
    }, [config?.remote_hub_url, config?.remote_email, remoteTenant.id, remoteTenant.name]);

    const hasRegisteredMachine = String(config?.remote_machine_id || '').trim() !== '' && String(config?.remote_machine_token || '').trim() !== '';
    const tenantLabel = remoteTenant.name || remoteTenant.id || emptyValue;
    const registeredName = hasRegisteredMachine ? (String(config?.remote_nickname || '').trim() || String(config?.remote_machine_name || '').trim() || String(config?.remote_machine_id || '').trim() || emptyValue) : emptyValue;
    const hubURL = String(config?.remote_hub_url || '').trim() || emptyValue;
    const remoteEmail = String(config?.remote_email || '').trim() || emptyValue;
    const machineID = String(config?.remote_machine_id || '').trim() || emptyValue;

    // Override product name for TigerClaw brand on About panel
    const productName = brandInfo?.id === 'qianxin'
        ? '\u864e\u722a 6 \u7a0b\u542f'
        : t("aboutProductName");

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
                    background: 'linear-gradient(180deg, #a8d4ff 0%, #4a9eff 30%, #1a6dd4 60%, #0d3f80 100%)',
                    WebkitBackgroundClip: 'text',
                    WebkitTextFillColor: 'transparent',
                    backgroundClip: 'text',
                    textShadow: '0 1px 2px rgba(26, 109, 212, 0.3)',
                    filter: 'drop-shadow(0 0 1px rgba(74, 158, 255, 0.4))',
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
                            <span className="about-build-badge">{t("buildLabel")} {buildNumber}</span>
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
                            <div style={remoteSectionTitleStyle}>{t("aboutIdentityTitle")}</div>
                            <p className="about-actions-card__desc" style={remoteBodyTextStyle}>
                                {t("aboutIdentityDesc")}
                            </p>
                        </div>
                        {hasRegisteredMachine ? (
                            <button
                                className="about-status-pill is-online"
                                style={{ cursor: 'pointer', border: 'none', background: 'rgba(239,68,68,0.12)', color: '#ef4444' }}
                                onClick={onClearRegistration}
                                title={t("aboutClearRegistration")}
                            >
                                {t("aboutClearBtn")}
                            </button>
                        ) : (
                            <button
                                className="about-status-pill"
                                style={{ cursor: 'pointer', border: 'none', background: 'rgba(59,130,246,0.12)', color: '#3b82f6' }}
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
                                <dt className="about-kv-label">{t("aboutAccountEmail")}</dt>
                                <dd className="about-identity-value about-identity-value--muted">{remoteEmail}</dd>
                            </div>
                        </div>
                        <div className="about-identity-row">
                            <div className="about-identity-item">
                                <dt className="about-kv-label">{t("aboutMachineId")}</dt>
                                <dd className="about-identity-value about-identity-value--mono">{machineID}</dd>
                            </div>
                            <div className="about-identity-item">
                                <dt className="about-kv-label">{t("remoteActivation")}</dt>
                                <dd className="about-identity-value about-identity-value--muted">{hasRegisteredMachine ? t("remoteActivated") : t("aboutNotRegistered")}</dd>
                            </div>
                        </div>
                    </dl>
                </section>

                <section className="about-actions-card" style={remoteCardStyle}>
                    <div className="about-card-heading">
                        <div>
                            <div style={remoteSectionTitleStyle}>{t("quickActionsTitle")}</div>
                            <p className="about-actions-card__desc" style={remoteBodyTextStyle}>
                                {t("quickActionsDesc")}
                            </p>
                        </div>
                    </div>
                    <div className="about-action-grid">
                        <button className="btn-link about-action-button" onClick={onOpenWebsite}>{t("officialWebsite")}</button>
                        <button className="btn-link about-action-button" onClick={onCheckUpdate}>{t("onlineUpdate")}</button>
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
            {showErrorLog && (
                <div className="modal-overlay" onClick={() => setShowErrorLog(false)}>
                    <div className="modal-content" style={{ width: '700px', maxWidth: '90vw' }} onClick={e => e.stopPropagation()}>
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
                                            color: isFatal ? '#ef4444' : 'var(--theme-danger)',
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
