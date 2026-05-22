import { useRef, useState } from 'react';
import { CheckUpdateBeta } from '../../../wailsjs/go/main/App';

// Info panels use var(--theme-info-bg). Latest marker replaces legacy \u2714\uFE0F with CSS.

type UpdateModalProps = {
    updateResult: any;
    appVersion: string;
    isDownloading: boolean;
    downloadProgress: number;
    installerPath: string;
    downloadError: string;
    t: (key: string) => string;
    onCancelDownload: () => void;
    onDownload: () => void;
    onInstall: () => void;
    onClose: () => void;
    onUpdateResultChange?: (result: any) => void;
};

export const UpdateModal = ({
    updateResult,
    appVersion,
    isDownloading,
    downloadProgress,
    installerPath,
    downloadError,
    t,
    onCancelDownload,
    onDownload,
    onInstall,
    onClose,
    onUpdateResultChange,
}: UpdateModalProps) => {
    const [betaChecked, setBetaChecked] = useState(false);
    const [betaLoading, setBetaLoading] = useState(false);
    // Cache the stable result so we can restore it when user unchecks beta
    const stableResultRef = useRef(updateResult);

    const handleBetaToggle = (checked: boolean) => {
        setBetaChecked(checked);
        if (checked) {
            // Save current stable result before switching to beta
            stableResultRef.current = updateResult;
            setBetaLoading(true);
            CheckUpdateBeta(appVersion).then((res: any) => {
                onUpdateResultChange?.(res);
                setBetaLoading(false);
            }).catch(() => {
                onUpdateResultChange?.({ has_update: false, latest_version: t("betaNoUpdate") });
                setBetaLoading(false);
            });
        } else {
            // Restore the cached stable result
            onUpdateResultChange?.(stableResultRef.current);
        }
    };

    return (
        <div className="modal-overlay">
            <div className="modal-content update-modal">
                <div className="update-modal__header">
                    <h3>{t("foundNewVersion")}</h3>
                    <label className="update-modal__beta-toggle">
                        <input
                            type="checkbox"
                            checked={betaChecked}
                            onChange={(e) => handleBetaToggle(e.target.checked)}
                            disabled={isDownloading || betaLoading}
                        />
                        {t("betaChannel")}
                    </label>
                </div>

                {betaLoading ? (
                    <div className="update-modal__info update-modal__info--center">
                        <p className="update-modal__checking">{t("checkingUpdate")}</p>
                    </div>
                ) : updateResult.has_update ? (
                    <>
                        <div className="update-modal__info update-modal__info--version">
                            <div className="update-modal__label">{t("currentVersion")}</div>
                            <div className="update-modal__version update-modal__version--current">v{appVersion}</div>
                            <div className="update-modal__label">{t("latestVersion")}</div>
                            <div className="update-modal__version update-modal__version--latest">{updateResult.latest_version}</div>
                        </div>

                        <div className="update-modal__workflow">
                            {isDownloading ? (
                                <div className="update-modal__download">
                                    <div className="update-modal__download-head">
                                        <span>{t("downloading")}</span>
                                        <span>{downloadProgress}%</span>
                                    </div>
                                    <div className="update-modal__progress-track">
                                        <div className="update-modal__progress-bar" style={{ width: String(downloadProgress) + '%' }} />
                                    </div>
                                    <button
                                        className="btn-link update-modal__cancel-download"
                                        onClick={onCancelDownload}
                                    >
                                        {t("cancelDownload")}
                                    </button>
                                </div>
                            ) : installerPath ? (
                                <div className="update-modal__complete">
                                    <p>{t("downloadComplete")}</p>
                                    <button className="btn-primary update-modal__full-button" onClick={onInstall}>
                                        {t("installNow")}
                                    </button>
                                </div>
                            ) : (
                                <div>
                                    {downloadError && (
                                        <div className="update-modal__error-block">
                                            <p>{t("downloadError").replace("{error}", downloadError)}</p>
                                            <button className="btn-primary update-modal__full-button update-modal__danger-button" onClick={onDownload}>
                                                {t("retry")}
                                            </button>
                                        </div>
                                    )}
                                    {!downloadError && (
                                        <>
                                            <p className="update-modal__message">{t("foundNewVersionMsg")}</p>
                                            <button className="btn-primary update-modal__full-button" onClick={onDownload}>
                                                {t("downloadAndUpdate")}
                                            </button>
                                        </>
                                    )}
                                </div>
                            )}
                        </div>
                    </>
                ) : (
                    <div className="update-modal__info">
                        <div className="update-modal__label">{t("currentVersion")}</div>
                        <div className="update-modal__version update-modal__version--current">v{appVersion}</div>
                        <div className="update-modal__label">{t("latestVersion")}</div>
                        <div className="update-modal__version update-modal__version--latest update-modal__version--spaced">{updateResult.latest_version}</div>
                        <p className="update-modal__latest-ok">{t("isLatestVersion")}</p>
                    </div>
                )}
                <div className="update-modal__actions">
                    <button className="btn-primary" disabled={isDownloading} onClick={onClose}>{t("close")}</button>
                </div>
            </div>
        </div>
    );
};
