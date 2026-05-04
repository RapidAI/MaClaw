import { AboutActions } from './AboutActions';

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
    t,
    setStatus,
    setUpdateResult,
    setIsStartupUpdateCheck,
    setShowUpdateModal,
    setShowInstallLog,
}: AboutPageProps) => (
    <div style={{
        padding: '20px',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        textAlign: 'center',
        height: '100%',
        justifyContent: 'center',
        boxSizing: 'border-box'
    }}>
        <img src={currentIcon} alt="Logo" style={{ width: '64px', height: '64px', marginBottom: '15px' }} />
        <h2 style={{
            margin: '0 0 4px 0',
            background: 'linear-gradient(135deg, #6366f1, #8b5cf6, #a855f7)',
            WebkitBackgroundClip: 'text',
            WebkitTextFillColor: 'transparent',
            display: 'inline-block',
            fontWeight: 'bold'
        }}>{brandDisplayTitle}</h2>
        <div style={{
            fontSize: '1rem',
            fontWeight: 'bold',
            background: 'linear-gradient(135deg, #6366f1, #8b5cf6, #a855f7)',
            WebkitBackgroundClip: 'text',
            WebkitTextFillColor: 'transparent',
            marginBottom: '4px',
            display: 'inline-block'
        }}>
            {brandInfo?.slogan || t("slogan")}
        </div>
        <div style={{ fontSize: '1rem', color: 'var(--theme-text-primary)', marginBottom: '5px' }}>{t("version")} {appVersion}</div>
        <div style={{ fontSize: '0.9rem', color: 'var(--theme-text-muted)', marginBottom: '5px' }}>{brandInfo?.businessContact || t("businessCooperation")}</div>
        <div style={{ fontSize: '0.9rem', color: 'var(--theme-text-secondary)', marginBottom: '20px' }}>{t("author")}: {brandInfo?.author || 'Dr. Daniel'}</div>

        <AboutActions
            brandInfo={brandInfo}
            appVersion={appVersion}
            t={t}
            setStatus={setStatus}
            setUpdateResult={setUpdateResult}
            setIsStartupUpdateCheck={setIsStartupUpdateCheck}
            setShowUpdateModal={setShowUpdateModal}
            setShowInstallLog={setShowInstallLog}
        />

    </div>
);
