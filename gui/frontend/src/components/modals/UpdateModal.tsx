import { useEffect, useRef, useState } from 'react';
import { CheckUpdate, CheckUpdateBeta, ListRollbackReleases, PatchConfigFields } from '../../../wailsjs/go/main/App';

// Info panels use var(--theme-info-bg). Latest marker uses the check icon escape below.

export type RollbackRelease = {
    build: string;
    published_at: string;
    download_url: string;
    sha256?: string;
    download_unavailable?: boolean;
};

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
    onRollbackReleaseChange?: (release: RollbackRelease | null) => void;
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
    if (normalizedHost.includes('myqcloud.com') || normalizedHost.includes('cos.')) return 'Tencent Cloud COS';
    if (normalizedHost.includes('cloudflare') || normalizedHost.includes('r2.')) return 'Cloudflare R2';
    return host;
}

export function rollbackUpdateResult(release: RollbackRelease) {
    return {
        has_update: true,
        latest_version: release.build,
        tag_name: release.build,
        download_url: release.download_url,
        sha256: release.sha256 || '',
        download_unavailable: Boolean(release.download_unavailable),
        channel: 'stable',
        is_rollback: true,
    };
}

export function rollbackReleaseDate(release: Pick<RollbackRelease, 'published_at'>): string {
    const publishedAt = String(release.published_at || '').trim();
    // The server sends an ISO timestamp. Keep its calendar date stable across
    // timezones rather than converting it to the browser's local date.
    return publishedAt ? publishedAt.slice(0, 10) : '';
}

export function rollbackReleaseLabel(release: { build?: unknown; published_at?: unknown }): string {
    const build = String(release.build || '').trim();
    const date = rollbackReleaseDate({ published_at: String(release.published_at || '') });
    return date ? `${build} · ${date}` : build;
}

export function failedUpdateResult(message: string) {
    return {
        has_update: false,
        check_failed: true,
        latest_version: '?',
        message,
        release_url: '',
    };
}

/** True when the cached result can be restored as a stable-channel snapshot. */
export function isRestorableStableResult(result: unknown): boolean {
    if (!result || typeof result !== 'object') return false;
    const r = result as { channel?: string; check_failed?: boolean };
    if (r.check_failed) return false;
    // Missing channel = legacy stable-only payload; accept it.
    if (!r.channel || r.channel === 'stable') return true;
    return false;
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
    onRollbackReleaseChange,
}: UpdateModalProps) => {
    const [betaChecked, setBetaChecked] = useState(preferBetaChannel ?? false);
    const [betaLoading, setBetaLoading] = useState(false);
    const [rollbackOpen, setRollbackOpen] = useState(false);
    const [rollbackLoading, setRollbackLoading] = useState(false);
    const [rollbackError, setRollbackError] = useState('');
    const [rollbackReleases, setRollbackReleases] = useState<RollbackRelease[]>([]);
    // Cache the stable result so we can restore it when user unchecks beta
    const stableResultRef = useRef(updateResult);
    // Monotonic id so out-of-order CheckUpdate* responses cannot clobber newer UI state
    // (e.g. user unchecks beta while a beta fetch is still in flight).
    const requestIdRef = useRef(0);
    const downloadSource = downloadSourceName(activeDownloadSource || updateResult?.download_url || updateResult?.release_url);
    const checkFailed = Boolean(updateResult?.check_failed);
    const hasUpdate = Boolean(updateResult?.has_update);
    const titleKey = checkFailed ? 'updateCheckFailed' : hasUpdate ? 'foundNewVersion' : 'isLatestVersion';

    const beginRequest = () => {
        requestIdRef.current += 1;
        return requestIdRef.current;
    };

    const fetchPreferredBeta = () => {
        const reqId = beginRequest();
        setBetaLoading(true);
        return CheckUpdateBeta(appVersion)
            .then((res: any) => {
                if (reqId !== requestIdRef.current) return;
                // Prefer-beta may surface a formal release; keep that as the stable cache.
                if (res?.channel === 'stable') {
                    stableResultRef.current = res;
                }
                onUpdateResultChange?.(res);
            })
            .catch(() => {
                if (reqId !== requestIdRef.current) return;
                // Network / both-channel failure — surface as a check error, not "no beta".
                onUpdateResultChange?.(failedUpdateResult(t('updateCheckFailedMessage')));
            })
            .finally(() => {
                if (reqId !== requestIdRef.current) return;
                setBetaLoading(false);
            });
    };

    const fetchStable = () => {
        const reqId = beginRequest();
        setBetaLoading(true);
        return CheckUpdate(appVersion)
            .then((res: any) => {
                if (reqId !== requestIdRef.current) return;
                stableResultRef.current = res;
                onUpdateResultChange?.(res);
            })
            .catch(() => {
                if (reqId !== requestIdRef.current) return;
                onUpdateResultChange?.(failedUpdateResult(t('updateCheckFailedMessage')));
            })
            .finally(() => {
                if (reqId !== requestIdRef.current) return;
                setBetaLoading(false);
            });
    };

    // When modal opens with beta preference already enabled, the opening payload may
    // still be a stable-only check (e.g. older callers). Re-check preferred channels
    // only when the payload has no channel field (legacy/partial). Prefer-beta results
    // may legitimately be channel=stable when formal is newer than beta.
    const didInitialBetaFetch = useRef(false);
    useEffect(() => {
        if (
            preferBetaChannel &&
            !didInitialBetaFetch.current &&
            !isDownloading &&
            !updateResult?.check_failed &&
            !updateResult?.channel
        ) {
            didInitialBetaFetch.current = true;
            if (isRestorableStableResult(updateResult)) {
                stableResultRef.current = updateResult;
            }
            void fetchPreferredBeta();
        }
        return () => {
            // Drop any in-flight responses after unmount.
            requestIdRef.current += 1;
        };
    }, []); // eslint-disable-line react-hooks/exhaustive-deps

    const showRollback = () => {
        setRollbackOpen(true);
        setRollbackError('');
        setRollbackLoading(true);
        ListRollbackReleases()
            .then((releases: RollbackRelease[]) => setRollbackReleases(Array.isArray(releases) ? releases : []))
            .catch(() => {
                setRollbackReleases([]);
                setRollbackError(t('rollbackLoadFailed'));
            })
            .finally(() => setRollbackLoading(false));
    };

    const selectRollbackRelease = (release: RollbackRelease) => {
        onRollbackReleaseChange?.(release);
        setRollbackOpen(false);
    };
    const handleBetaToggle = (checked: boolean) => {
        setBetaChecked(checked);
        // Persist the preference so startup auto-check uses the right channel
        PatchConfigFields({ prefer_beta_channel: checked }).catch(() => {});
        if (checked) {
            // Save current stable result before switching to beta (only if it's actually stable)
            if (isRestorableStableResult(updateResult)) {
                stableResultRef.current = updateResult;
            }
            void fetchPreferredBeta();
        } else {
            // Invalidate in-flight preferred fetches so they cannot overwrite the restore.
            const cached = stableResultRef.current;
            if (isRestorableStableResult(cached)) {
                beginRequest();
                setBetaLoading(false);
                onUpdateResultChange?.(cached);
            } else {
                void fetchStable();
            }
        }
    };

    return (
        <div className="modal-overlay">
            <div className="modal-content update-modal" role="dialog" aria-modal="true" aria-labelledby="update-modal-title">
                <div className="update-modal__header">
                    <div>
                        <p className="update-modal__eyebrow">{t("onlineUpdate")}</p>
                        <h3 id="update-modal-title">{t(titleKey)}</h3>
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

                {rollbackOpen ? (
                    <div className="update-modal__info update-modal__rollback" aria-live="polite">
                        <div className="update-modal__rollback-heading">
                            <div>
                                <div className="update-modal__label">{t('rollbackVersions')}</div>
                                <p>{t('rollbackHint')}</p>
                            </div>
                            <button type="button" className="btn-link" onClick={() => setRollbackOpen(false)}>{t('back')}</button>
                        </div>
                        {rollbackLoading ? <p className="update-modal__checking">{t('checkingUpdate')}</p> : rollbackError ? (
                            <p className="update-modal__error-message">{rollbackError}</p>
                        ) : rollbackReleases.length === 0 ? (
                            <p className="update-modal__message">{t('rollbackEmpty')}</p>
                        ) : (
                            <div className="update-modal__rollback-list">
                                {rollbackReleases.map((release) => (
                                    <button
                                        type="button"
                                        className="update-modal__rollback-item"
                                        key={String(release.build)}
                                        disabled={isDownloading || release.download_unavailable}
                                        onClick={() => selectRollbackRelease(release)}
                                    >
                                        <strong>{release.build}</strong>
                                        <span>{rollbackReleaseDate(release)}</span>
                                    </button>
                                ))}
                            </div>
                        )}
                    </div>
                ) : betaLoading ? (
                    <div className="update-modal__info update-modal__info--center">
                        <p className="update-modal__checking">{t("checkingUpdate")}</p>
                    </div>
                ) : checkFailed ? (
                    <div className="update-modal__info update-modal__info--error" role="alert">
                        <div className="update-modal__label">{t("currentVersion")}</div>
                        <div className="update-modal__version update-modal__version--current">v{appVersion}</div>
                        <p className="update-modal__error-message">{updateResult.message || t("updateCheckFailedMessage")}</p>
                    </div>
                ) : hasUpdate ? (
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
                                    <div className="update-modal__version update-modal__version--latest">
                                        {updateResult.latest_version}
                                        {updateResult.channel === 'beta' && (
                                            <span className="update-modal__channel-badge" title={t('betaChannel')}>
                                                {t('betaChannel')}
                                            </span>
                                        )}
                                    </div>
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
                                                    <p className="update-modal__message">{updateResult.is_rollback ? t("rollbackSelectedMsg") : t("foundNewVersionMsg")}</p>
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
                    {!rollbackOpen && <button type="button" className="btn-link" disabled={isDownloading || betaLoading} onClick={showRollback}>{t('rollback')}</button>}
                    <button className="btn-primary" disabled={isDownloading} onClick={onClose}>{t("close")}</button>
                </div>
            </div>
        </div>
    );
};
