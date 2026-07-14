import { BrowserOpenURL } from '../../../wailsjs/runtime';
import { CheckUpdate } from '../../../wailsjs/go/main/App';

type BrandInfo = {
    id: string;
    websiteURL: string;
    githubURL: string;
};

type AboutActionsProps = {
    brandInfo: BrandInfo | null;
    appVersion: string;
    t: (key: string) => string;
    setStatus: (status: string) => void;
    setUpdateResult: (result: any) => void;
    setIsStartupUpdateCheck: (value: boolean) => void;
    setShowUpdateModal: (value: boolean) => void;
    setShowInstallLog: (value: boolean) => void;
};

export const AboutActions = ({
    brandInfo,
    appVersion,
    t,
    setStatus,
    setUpdateResult,
    setIsStartupUpdateCheck,
    setShowUpdateModal,
    setShowInstallLog,
}: AboutActionsProps) => {
    const repoURL = brandInfo?.githubURL || 'https://github.com/rapidai/maclaw';
    const issueURL = repoURL + '/issues/new';
    const websiteURL = brandInfo?.websiteURL || 'https://maclaw.top';
    const checkForUpdate = () => {
        setStatus(t('checkingUpdate'));
        CheckUpdate(appVersion).then(res => {
            setUpdateResult(res);
            setIsStartupUpdateCheck(false);
            setShowUpdateModal(true);
            setStatus('');
        }).catch(err => {
            setStatus('Check update failed: ' + err);
            setUpdateResult({ has_update: false, check_failed: true, message: 'Unable to check for updates. Check your connection and try again.', release_url: '' });
            setIsStartupUpdateCheck(false);
            setShowUpdateModal(true);
        });
    };
    const actions = [
        { label: t('officialWebsite'), onClick: () => BrowserOpenURL(websiteURL) },
        { label: t('onlineUpdate'), onClick: checkForUpdate },
        { label: t('installLog'), onClick: () => setShowInstallLog(true) },
        { label: t('memoryHealth'), onClick: () => BrowserOpenURL(websiteURL + '/memory-health') },
        { label: t('securityEvents'), onClick: () => BrowserOpenURL(websiteURL + '/security') },
        { label: t('errorLogs'), onClick: () => setShowInstallLog(true) },
        ...(brandInfo?.id === 'qianxin' ? [] : [
            { label: t('bugReport'), onClick: () => BrowserOpenURL(issueURL) },
            { label: t('codeRepo'), onClick: () => BrowserOpenURL(repoURL) },
        ]),
    ];

    return (
        <section className="about-card about-actions-card">
            <h3 className="about-section-title">{t('aboutQuickActions')}</h3>
            <p className="about-actions-card__desc">{t('aboutQuickActionsDesc')}</p>
            <div className="about-action-grid">
                {actions.map(action => (
                    <button key={action.label} className="about-action-button" onClick={action.onClick}>
                        {action.label}
                    </button>
                ))}
            </div>
        </section>
    );
};
