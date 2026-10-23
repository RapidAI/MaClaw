import { useEffect, useRef, useState } from 'react';
import { CheckUpdate, CheckUpdateBeta, ListRollbackReleases, PatchConfigFields } from '../../../wailsjs/go/main/App';
import { StatusGlyph } from '../ai/WorkbenchIcons';

// Info panels use var(--theme-info-bg). Status marks are SVG glyphs from
// WorkbenchIcons (StatusGlyph) rather than emoji so they inherit theme colour.

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
    // The installer has been handed off and the app is about to quit. The
    // window closes on its own, so the modal says so instead of leaving the
    // user on a "download complete" screen with nothing happening.
    isInstalling?: boolean;
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

/**
 * One version spelling for both sides of the comparison. The backend tags
 * releases as "V8.1.0.12434" while the running build is reported bare, so
 * rendering the payload verbatim put "v8.1.0.12431" next to "V8.1.0.12434".
 */
export function formatVersion(value: unknown): string {
    const raw = String(value ?? '').trim();
    if (!raw) return '';
    return `v${raw.replace(/^v/i, '')}`;
}

/**
 * Label for the right-hand version cell. A rollback target is a bare build
 * number ("12434"), not a version, so it keeps its own spelling; prefixing it
 * would advertise "v12434".
 */
export function latestVersionLabel(result: { latest_version?: unknown; is_rollback?: boolean } | null | undefined): string {
    const raw = String(result?.latest_version ?? '').trim();
    if (!raw) return '';
    return result?.is_rollback ? raw : formatVersion(raw);
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
    isInstalling = false,
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
                <header className="update-modal__header">
                    <div className="update-modal__heading">
                        <p className="update-modal__eyebrow">{t("onlineUpdate")}</p>
                        <h3 id="update-modal-title">{t(titleKey)}</h3>
                    </div>
                    <label className="update-modal__beta-toggle">
                        <span className="update-modal__beta-text">{t("betaChannel")}</span>
                        <input
                            type="checkbox"
                            checked={betaChecked}
                            onChange={(e) => handleBetaToggle(e.target.checked)}
                            disabled={isDownloading || betaLoading}
                        />
                        <span className="update-modal__switch" aria-hidden="true">
                            <span className="update-modal__switch-thumb" />
                        </span>
                    </label>
                </header>

                <div className="update-modal__body">
                    {rollbackOpen ? (
                        <section className="update-modal__rollback" aria-live="polite">
                            <div className="update-modal__rollback-heading">
                                <div>
                                    <div className="update-modal__label">{t('rollbackVersions')}</div>
                                    <p>{t('rollbackHint')}</p>
                                </div>
                                <button type="button" className="update-modal__link-button" onClick={() => setRollbackOpen(false)}>{t('back')}</button>
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
                        </section>
                    ) : betaLoading ? (
                        <section className="update-modal__status-panel update-modal__status-panel--working" aria-live="polite">
                            <div className="update-modal__status-head">
                                <span className="update-modal__spinner" aria-hidden="true" />
                                <span className="update-modal__status-title">{t("checkingUpdate")}</span>
                            </div>
                        </section>
                    ) : checkFailed ? (
                        <section className="update-modal__status-panel update-modal__status-panel--error" role="alert">
                            <div className="update-modal__status-head">
                                <StatusGlyph kind="error" size={16} />
                                <span className="update-modal__status-title">{updateResult.message || t("updateCheckFailedMessage")}</span>
                            </div>
                            <div className="update-modal__status-foot">
                                <span className="update-modal__label">{t("currentVersion")}</span>
                                <span className="update-modal__version update-modal__version--current">{formatVersion(appVersion)}</span>
                            </div>
                        </section>
                    ) : hasUpdate ? (
                        <>
                            {/* Info card: var(--theme-info-bg) keeps the version pair readable in both themes. */}
                            <section className="update-modal__card">
                                <div className="update-modal__version-row">
                                    <div className="update-modal__version-cell">
                                        <div className="update-modal__label">{t("currentVersion")}</div>
                                        <div className="update-modal__version update-modal__version--current">{formatVersion(appVersion)}</div>
                                    </div>
                                    <span className="update-modal__version-arrow" aria-hidden="true">→</span>
                                    <div className="update-modal__version-cell">
                                        <div className="update-modal__label">{t("latestVersion")}</div>
                                        <div className="update-modal__version update-modal__version--latest">
                                            {latestVersionLabel(updateResult)}
                                            {updateResult.channel === 'beta' && (
                                                <span className="update-modal__channel-badge" title={t('betaChannel')}>
                                                    {t('betaChannel')}
                                                </span>
                                            )}
                                        </div>
                                    </div>
                                </div>
                                {downloadSource && (
                                    <div className="update-modal__source">
                                        <span className="update-modal__source-label">{isDownloading ? t("downloadingFrom") : t("downloadSource")}</span>
                                        <span className="update-modal__source-value">{downloadSource}</span>
                                    </div>
                                )}
                            </section>

                            {isDownloading ? (
                                // No aria-live here: the percentage changes on
                                // every tick and a live region would announce
                                // each one. role=progressbar carries the value.
                                <section className="update-modal__status-panel update-modal__status-panel--working">
                                    <div className="update-modal__status-head">
                                        <span className="update-modal__spinner" aria-hidden="true" />
                                        <span className="update-modal__status-title">{t("downloading")}</span>
                                        <span className="update-modal__status-meta">{downloadProgress}%</span>
                                    </div>
                                    <div
                                        className="update-modal__progress-track"
                                        role="progressbar"
                                        aria-label={t("downloading")}
                                        aria-valuemin={0}
                                        aria-valuemax={100}
                                        aria-valuenow={downloadProgress}
                                    >
                                        <div className="update-modal__progress-bar" style={{ width: String(downloadProgress) + '%' }} />
                                    </div>
                                    <button
                                        type="button"
                                        className="update-modal__link-button update-modal__cancel-download"
                                        onClick={onCancelDownload}
                                    >
                                        {t("cancelDownload")}
                                    </button>
                                </section>
                            ) : isInstalling ? (
                                // The live region is on the copy only: an
                                // aria-live container that also holds the
                                // looping progress bar re-announces on every
                                // animation tick.
                                <section className="update-modal__status-panel update-modal__status-panel--install">
                                    <div className="update-modal__install">
                                        <span className="update-modal__install-mark" aria-hidden="true">
                                            <span className="update-modal__spinner" />
                                        </span>
                                        <div className="update-modal__install-copy" role="status">
                                            <p className="update-modal__install-title">{t("installingUpdate")}</p>
                                            <p className="update-modal__install-hint">{t("installingUpdateHint")}</p>
                                        </div>
                                        <div
                                            className="update-modal__progress-track update-modal__progress-track--indeterminate update-modal__install-progress"
                                            role="progressbar"
                                            aria-label={t("installingUpdate")}
                                        >
                                            <div className="update-modal__progress-bar update-modal__progress-bar--indeterminate" />
                                        </div>
                                    </div>
                                </section>
                            ) : installerPath ? (
                                <section className="update-modal__complete">
                                    {/* A failed automatic launch leaves the
                                        installer on disk: say why it stopped
                                        and let the user try again by hand.
                                        The message is already a complete
                                        sentence from the caller, so it is not
                                        wrapped in the installLaunchError
                                        template (that would read as
                                        "Failed to launch: it did not take over"). */}
                                    {downloadError && (
                                        <p className="update-modal__error-message" role="alert">
                                            {downloadError}
                                        </p>
                                    )}
                                    <span className="update-modal__complete-mark" aria-hidden="true">
                                        <StatusGlyph kind="ok" size={18} />
                                    </span>
                                    <p className="update-modal__complete-title">{t("downloadComplete")}</p>
                                    <button type="button" className="update-modal__cta-button" onClick={onInstall}>
                                        {t("installNow")}
                                    </button>
                                </section>
                            ) : downloadError ? (
                                <section className="update-modal__status-panel update-modal__status-panel--error" role="alert">
                                    <div className="update-modal__status-head">
                                        <StatusGlyph kind="error" size={16} />
                                        <span className="update-modal__status-title">
                                            {t("downloadError").replace("{error}", downloadError)}
                                        </span>
                                    </div>
                                    <button
                                        type="button"
                                        className="update-modal__cta-button"
                                        data-tone="danger"
                                        onClick={onDownload}
                                    >
                                        {t("retry")}
                                    </button>
                                </section>
                            ) : updateResult.download_unavailable ? (
                                <section className="update-modal__status-panel update-modal__status-panel--muted">
                                    <div className="update-modal__status-head">
                                        <StatusGlyph kind="info" size={16} />
                                        <span className="update-modal__status-title">{t("downloadUnavailable")}</span>
                                    </div>
                                </section>
                            ) : (
                                <div className="update-modal__cta">
                                    <p className="update-modal__message">{updateResult.is_rollback ? t("rollbackSelectedMsg") : t("foundNewVersionMsg")}</p>
                                    <button type="button" className="update-modal__cta-button" onClick={onDownload}>
                                        {t("downloadAndUpdate")}
                                    </button>
                                </div>
                            )}
                        </>
                    ) : (
                        <section className="update-modal__card update-modal__card--current">
                            <div className="update-modal__label">{t("currentVersion")}</div>
                            <div className="update-modal__version update-modal__version--current">{formatVersion(appVersion)}</div>
                            <p className="update-modal__latest-ok">
                                <span className="update-modal__latest-ok-icon" aria-hidden="true">
                                    <StatusGlyph kind="ok" size={14} />
                                </span>
                                {t("isLatestVersion")}
                            </p>
                        </section>
                    )}
                </div>

                <footer className="update-modal__actions">
                    {!rollbackOpen && (
                        <button
                            type="button"
                            className="update-modal__link-button update-modal__rollback-link"
                            disabled={isDownloading || betaLoading || isInstalling}
                            onClick={showRollback}
                        >
                            {t('rollback')}
                        </button>
                    )}
                    {/* While installing, the app is about to quit; closing the
                        modal would only hide the reason the window disappears. */}
                    <button
                        type="button"
                        className="update-modal__close"
                        disabled={isDownloading || isInstalling}
                        onClick={onClose}
                    >
                        {t("close")}
                    </button>
                </footer>
            </div>
        </div>
    );
};
