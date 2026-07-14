import { useEffect, useRef, useState } from 'react';
import { CheckUpdate, CheckUpdateBeta, PatchConfigFields } from '../../../wailsjs/go/main/App';

// Info panels use var(--theme-info-bg). Latest marker uses the check icon escape below.

type UpdateModalProps = {
    updateResult: any;
    appVersion: string;
    isDownloading: boolean;
    downloadProgress: number;
    activeDownloadSource?: string;
    installerPath: string;
    downloadError: string;
    preferBetaChannel?: boolean;
    t: (key: string) => string;
    onCancelDownload: () => void;
    onDownload: () => void;
    onInstall: () => void;
    onClose: () => void;
    onUpdateResultChange?: (result: any) => void;
};

export function downloadSourceName(value: unknown): string {
    // Keep this in sync with the backend's splitDownloadURLs. A URL may contain
    // commas in signed query parameters, while candidates use line breaks or |.
    const firstValue = String(value || '').trim().split(/[|\r\n]/).map(item => item.trim()).find(Boolean);
    if (!firstValue) return '';
    let host = firstValue;
    try {
        host = new URL(firstValue).hostname;
    } catch {
        // The backend sends a hostname only during an active download.
    }
    const normalizedHost = host.toLowerCase();
    if (normalizedHost === 'github.com' || normalizedHost.endsWith('.github.com')) return 'GitHub Releases';
    if (normalizedHost.includes('myqcloud.com') || normalizedHost.includes('cos.')) return '腾讯云 COS';
    if (normalizedHost.includes('cloudflare') || normalizedHost.includes('r2.')) return 'Cloudflare R2';
    return host;
}

export const UpdateModal = ({
    updateResult,
    appVersion,
    isDownloading,
    downloadProgress,
    activeDownloadSource,
    installerPath,
    downloadError,
    preferBetaChannel,
    t,
    onCancelDownload,
    onDownload,
    onInstall,
    onClose,
    onUpdateResultChange,
}: UpdateModalProps) => {
    const [betaChecked, setBetaChecked] = useState(preferBetaChannel ?? false);
    const [betaLoading, setBetaLoading] = useState(false);
    // Cache the stable result so we can restore it when user unchecks beta
    const stableResultRef = useRef(updateResult);
    const downloadSource = downloadSourceName(activeDownloadSource || updateResult?.download_url || updateResult?.release_url);

    // When modal opens with beta preference already enabled, immediately fetch beta info
    // so the user sees beta results instead of stale stable results.
    // Skip if the result already came from the beta channel (startup auto-check).
    const didInitialBetaFetch = useRef(false);
    useEffect(() => {
        if (preferBetaChannel && !didInitialBetaFetch.current && !isDownloading && updateResult?.channel !== "beta") {
            didInitialBetaFetch.current = true;
            stableResultRef.current = updateResult;
            setBetaLoading(true);
            CheckUpdateBeta(appVersion).then((res: any) => {
                onUpdateResultChange?.(res);
                setBetaLoading(false);
            }).catch(() => {
                onUpdateResultChange?.({ has_update: false, latest_version: t("betaNoUpdate") });
                setBetaLoading(false);
            });
        }
    }, []); // eslint-disable-line react-hooks/exhaustive-deps

    const handleBetaToggle = (checked: boolean) => {
        setBetaChecked(checked);
        // Persist the preference so startup auto-check uses the right channel
        PatchConfigFields({ prefer_beta_channel: checked }).catch(() => {});
        if (checked) {
            // Save current stable result before switching to beta (only if it's actually stable)
            if (!updateResult?.channel || updateResult.channel === "stable") {
                stableResultRef.current = updateResult;
            }
            setBetaLoading(true);
            CheckUpdateBeta(appVersion).then((res: any) => {
                onUpdateResultChange?.(res);
                setBetaLoading(false);
            }).catch(() => {
                onUpdateResultChange?.({ has_update: false, latest_version: t("betaNoUpdate") });
                setBetaLoading(false);
            });
        } else {
            // Restore cached stable result, or fetch fresh stable data if we never had it
            if (stableResultRef.current?.channel && stableResultRef.current.channel !== "stable") {
                setBetaLoading(true);
                CheckUpdate(appVersion).then((res: any) => {
                    stableResultRef.current = res;
                    onUpdateResultChange?.(res);
                    setBetaLoading(false);
                }).catch(() => {
                    onUpdateResultChange?.({ has_update: false, latest_version: "?" });
                    setBetaLoading(false);
                });
            } else {
                onUpdateResultChange?.(stableResultRef.current);
            }
        }
    };

    return (
        <div className="modal-overlay">
            <div className="modal-content update-modal" role="dialog" aria-modal="true" aria-labelledby="update-modal-title">
                <div className="update-modal__header">
                    <div>
                        <p className="update-modal__eyebrow">{t("onlineUpdate")}</p>
                        <h3 id="update-modal-title">{t("foundNewVersion")}</h3>
                    </div>
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
                            <div className="update-modal__version-row">
                                <div>
                                    <div className="update-modal__label">{t("currentVersion")}</div>
                                    <div className="update-modal__version update-modal__version--current">v{appVersion}</div>
                                </div>
                                <span className="update-modal__version-arrow" aria-hidden="true">→</span>
                                <div>
                                    <div className="update-modal__label">{t("latestVersion")}</div>
                                    <div className="update-modal__version update-modal__version--latest">{updateResult.latest_version}</div>
                                </div>
                            </div>
                            {downloadSource && (
                                <div className="update-modal__source" aria-live="polite">
                                    <span className="update-modal__source-label">{isDownloading ? t("downloadingFrom") : t("downloadSource")}</span>
                                    <strong>{downloadSource}</strong>
                                </div>
                            )}
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
                                            {updateResult.download_unavailable ? (
                                                <p className="update-modal__message">{t("downloadUnavailable")}</p>
                                            ) : (
                                                <>
                                                    <p className="update-modal__message">{t("foundNewVersionMsg")}</p>
                                                    <button className="btn-primary update-modal__full-button" onClick={onDownload}>
                                                        {t("downloadAndUpdate")}
                                                    </button>
                                                </>
                                            )}
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
                        <p className="update-modal__latest-ok">
                            <span className="update-modal__latest-ok-icon" aria-hidden="true">{"\u2714\uFE0F"}</span>
                            {t("isLatestVersion")}
                        </p>
                    </div>
                )}
                <div className="update-modal__actions">
                    <button className="btn-primary" disabled={isDownloading} onClick={onClose}>{t("close")}</button>
                </div>
            </div>
        </div>
    );
};
