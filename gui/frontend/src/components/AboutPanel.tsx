import ReactMarkdown from 'react-markdown';
import type { MouseEvent } from 'react';
import { useState, useEffect } from 'react';
import remarkGfm from 'remark-gfm';
import rehypeRaw from 'rehype-raw';
import { BrowserOpenURL } from '../../wailsjs/runtime';
import { ReadErrorLog } from '../../wailsjs/go/main/App';
import { remoteCardStyle, remoteMutedCardStyle, remoteSectionTitleStyle, remoteBodyTextStyle } from './remote/styles';
import { MemoryHealthDialog } from './MemoryHealthDialog';
import { SecurityEventsDialog } from './SecurityEventsDialog';

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

type AboutPanelProps = {
    currentIcon: string;
    brandInfo: BrandInfo | null;
    appVersion: string;
    buildNumber: string;
    thanksContent: string;
    t: (key: string) => string;
    onOpenWebsite: () => void;
    onCheckUpdate: () => void;
    onShowInstallLog: () => void;
    onOpenBugReport: () => void;
    onOpenGithub: () => void;
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
    t,
    onOpenWebsite,
    onCheckUpdate,
    onShowInstallLog,
    onOpenBugReport,
    onOpenGithub,
}: AboutPanelProps) {
    const slogan = brandInfo?.slogan || t("slogan");
    const author = brandInfo?.author || 'Dr. Daniel';
    const businessContact = brandInfo?.businessContact || t("businessCooperation");
    const showGithubActions = Boolean(brandInfo?.githubURL) || brandInfo?.id !== 'qianxin';

    // Override product name for TigerClaw brand on About panel
    const productName = brandInfo?.id === 'qianxin'
        ? '\u864e\u722a\u00b7\u7a0b\u542f TigerClaw'
        : t("aboutProductName");

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
                        <h2 className="about-hero-card__title">{productName}</h2>
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

                <section className="about-actions-card" style={remoteCardStyle}>
                    <div className="about-actions-card__header">
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
