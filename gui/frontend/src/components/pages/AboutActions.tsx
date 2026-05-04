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

const smallButtonStyle = { fontSize: '0.75rem', padding: '2px 6px' };

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
    const checkForUpdate = () => {
        setStatus(t('checkingUpdate'));
        CheckUpdate(appVersion).then(res => {
            console.log('CheckUpdate result:', res);
            setUpdateResult(res);
            setIsStartupUpdateCheck(false);
            setShowUpdateModal(true);
            setStatus('');
        }).catch(err => {
            console.error('CheckUpdate error:', err);
            setStatus('Check update failed: ' + err);
            setUpdateResult({
                has_update: false,
                latest_version: 'Failed to fetch',
                release_url: '',
            });
            setIsStartupUpdateCheck(false);
            setShowUpdateModal(true);
        });
    };

    return (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '12px', alignItems: 'center' }}>
            <div style={{ display: 'flex', gap: '6px', justifyContent: 'center', flexWrap: 'wrap' }}>
                <button className="btn-link" style={smallButtonStyle} onClick={() => BrowserOpenURL(brandInfo?.websiteURL || 'https://maclaw.top')}>{t('officialWebsite')}</button>
                <button className="btn-link" style={smallButtonStyle} onClick={checkForUpdate}>{t('onlineUpdate')}</button>
                <button className="btn-link" style={smallButtonStyle} onClick={() => setShowInstallLog(true)}>{t('installLog')}</button>
                {brandInfo?.githubURL ? (
                    <>
                        <button className="btn-link" style={smallButtonStyle} onClick={() => BrowserOpenURL(brandInfo.githubURL + '/issues/new')}>{t('bugReport')}</button>
                        <button className="btn-link" style={smallButtonStyle} onClick={() => BrowserOpenURL(brandInfo.githubURL)}>GitHub</button>
                    </>
                ) : brandInfo?.id !== 'qianxin' ? (
                    <>
                        <button className="btn-link" style={smallButtonStyle} onClick={() => BrowserOpenURL('https://github.com/rapidai/maclaw/issues/new')}>{t('bugReport')}</button>
                        <button className="btn-link" style={smallButtonStyle} onClick={() => BrowserOpenURL('https://github.com/rapidai/maclaw')}>GitHub</button>
                    </>
                ) : null}
            </div>
        </div>
    );
};
