import ReactMarkdown from 'react-markdown';
import type { MouseEvent } from 'react';
import { useState } from 'react';
import remarkGfm from 'remark-gfm';
import rehypeRaw from 'rehype-raw';
import { BrowserOpenURL } from '../../wailsjs/runtime';
import { remoteCardStyle, remoteMutedCardStyle, remoteSectionTitleStyle, remoteBodyTextStyle } from './remote/styles';
import { MemoryHealthDialog } from './MemoryHealthDialog';

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
        ? '虎爪·涅槃 （TigerClaw）'
        : t("aboutProductName");

    const [showHealthDialog, setShowHealthDialog] = useState(false);

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
        </div>
    );
}
