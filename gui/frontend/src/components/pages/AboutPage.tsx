import { AboutActions } from './AboutActions';
import { AboutThanksCard } from './AboutThanksCard';

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

type AboutPageProps = {
    currentIcon: string;
    brandDisplayTitle: string;
    brandInfo: BrandInfo | null;
    appVersion: string;
    preferBetaChannel?: boolean;
    t: (key: string) => string;
    setStatus: (status: string) => void;
    setUpdateResult: (result: any) => void;
    setIsStartupUpdateCheck: (value: boolean) => void;
    setShowUpdateModal: (value: boolean) => void;
    setShowInstallLog: (value: boolean) => void;
};

export const AboutPage = ({
    currentIcon,
    brandDisplayTitle,
    brandInfo,
    appVersion,
    preferBetaChannel,
    t,
    setStatus,
    setUpdateResult,
    setIsStartupUpdateCheck,
    setShowUpdateModal,
    setShowInstallLog,
}: AboutPageProps) => {
    const versionParts = appVersion.split('.');
    const buildNumber = versionParts[versionParts.length - 1] || appVersion;

    return (
        <div className="about-page" style={{ color: 'var(--theme-text-primary)' }}>
            <div className="about-page__container">
                <section className="about-card about-hero-card">
                    <div className="about-hero-card__icon-wrap">
                        <img src={currentIcon} alt="Logo" className="about-hero-card__icon" />
                    </div>
                    <div className="about-hero-card__body">
                        <h2 className="about-hero-card__title">{brandDisplayTitle}</h2>
                        <p className="about-hero-card__slogan">{brandInfo?.slogan || t('slogan')}</p>
                        <div className="about-version-row">
                            <span className="about-version-badge">{t('version')} {appVersion}</span>
                            <span className="about-build-badge">{t('aboutBuild')} {buildNumber}</span>
                        </div>
                        <div className="about-meta-inline">
                            <span>{t('author')}: {brandInfo?.author || 'Dr. Daniel'}</span>
                            <span className="about-meta-dot">•</span>
                            <span>{brandInfo?.businessContact || t('businessCooperation')}</span>
                        </div>
                    </div>
                </section>
                <AboutActions
                    brandInfo={brandInfo}
                    appVersion={appVersion}
                    preferBetaChannel={preferBetaChannel}
                    t={t}
                    setStatus={setStatus}
                    setUpdateResult={setUpdateResult}
                    setIsStartupUpdateCheck={setIsStartupUpdateCheck}
                    setShowUpdateModal={setShowUpdateModal}
                    setShowInstallLog={setShowInstallLog}
                />
                <AboutThanksCard t={t} />
            </div>
        </div>
    );
};
