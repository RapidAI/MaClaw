import { remoteCardStyle, remoteMutedCardStyle, remoteMetaLabelStyle, remoteSectionTitleStyle, remoteBodyTextStyle } from './remote/styles';

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
    t: (key: string) => string;
    onOpenWebsite: () => void;
    onCheckUpdate: () => void;
    onShowInstallLog: () => void;
    onOpenBugReport: () => void;
    onOpenGithub: () => void;
};

export function AboutPanel({
    currentIcon,
    brandInfo,
    appVersion,
    buildNumber,
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

    return (
        <div className="about-page">
            <div className="about-page__container">
                <section className="about-hero-card" style={remoteCardStyle}>
                    <div className="about-hero-card__icon-wrap" style={remoteMutedCardStyle}>
                        <img src={currentIcon} alt="Logo" className="about-hero-card__icon" />
                    </div>
                    <div className="about-hero-card__body">
                        <h2 className="about-hero-card__title">{t("aboutProductName")}</h2>
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
                        {showGithubActions && (
                            <>
                                <button className="btn-link about-action-button" onClick={onOpenBugReport}>{t("bugReport")}</button>
                                <button className="btn-link about-action-button" onClick={onOpenGithub}>{t("codeRepository")}</button>
                            </>
                        )}
                    </div>
                </section>
            </div>
        </div>
    );
}
