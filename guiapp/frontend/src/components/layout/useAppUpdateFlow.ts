import { useCallback, useEffect, useRef, useState } from 'react';
import {
    CancelDownload,
    DeferDownloadedUpdateInstall,
    DeferDownloadedUpdateInstallToNextStartup,
    DownloadUpdateWithSHA256,
    IgnoreUpdateVersion,
    InstallDownloadedUpdate,
    RegisterDownloadedUpdate,
} from '../../../wailsjs/go/main/App';
import { BrowserOpenURL, EventsOff, EventsOn } from '../../../wailsjs/runtime';
import { EVENT_APP_UPDATE_AVAILABLE, EVENT_APP_UPDATE_DOWNLOADED } from '../../constants/events';
import { rollbackUpdateResult, type RollbackRelease } from '../modals/UpdateModal';
import type { AssistantUpdatePayload } from '../ai/AssistantUpdateNotice';

const DISMISSED_APP_UPDATE_VERSION_KEY = "maclaw:dismissed-app-update-version";

function safeEventsOn(eventName: string, callback: (...args: any[]) => void) {
    try {
        return EventsOn(eventName, callback);
    } catch {
        return undefined;
    }
}

function safeEventsOff(eventName: string, ...additionalEventNames: string[]) {
    try {
        EventsOff(eventName, ...additionalEventNames);
    } catch {
        // Runtime events are unavailable in a plain browser dev session.
    }
}

function safeBrowserOpenURL(url: string) {
    try {
        BrowserOpenURL(url);
    } catch {
        window.open(url, "_blank", "noopener,noreferrer");
    }
}

function installerFileNameFromURL(value: unknown): string {
    const first = String(value || '').split(/[|\r\n]/).map(v => v.trim()).find(Boolean);
    if (!first) return '';
    try {
        const pathname = new URL(first).pathname;
        const name = decodeURIComponent(pathname.slice(pathname.lastIndexOf('/') + 1));
        return /^(MaClaw|TigerClaw|QAgent|MetaStaff)-(Setup\.exe|Universal\.pkg)$/i.test(name) ? name : '';
    } catch {
        return '';
    }
}

// Mirrors versionedInstallerFileName in the backend: qualify the download
// destination with the target version so installers of different releases
// never share one path. A fixed shared name let a silent background download
// overwrite the bytes of a rollback (or vice versa) while the user was still
// deciding on it.
function versionedInstallerFileName(fileName: string, version: string): string {
    fileName = String(fileName || '').trim();
    version = String(version || '').trim();
    if (!fileName || !version) return fileName;
    const cleaned = version.replace(/[^A-Za-z0-9._-]/g, '_').replace(/^[._-]+|[._-]+$/g, '');
    if (!cleaned) return fileName;
    const dot = fileName.lastIndexOf('.');
    if (dot <= 0) return `${fileName}-${cleaned}`;
    return `${fileName.slice(0, dot)}-${cleaned}${fileName.slice(dot)}`;
}

export interface AppUpdateFlowOptions {
    brandDisplayName: unknown;
    isWindows: boolean;
    translate: (key: string) => string;
    formatText: (key: string, values?: Record<string, string>) => string;
    showToastMessage: (message: string, duration?: number) => void;
}

// useAppUpdateFlow owns the online-update dialog state machine: background
// and manual installer downloads, the explicit install-now/defer/next-startup/
// ignore decisions, and the rollback target switching. The backend never
// installs on its own; every install path here starts from a user choice.
export function useAppUpdateFlow({
    brandDisplayName,
    isWindows,
    translate,
    formatText,
    showToastMessage,
}: AppUpdateFlowOptions) {
    const [showUpdateModal, setShowUpdateModal] = useState(false);
    const [updateResult, setUpdateResult] = useState<any>(null);
    const updateResultRef = useRef<any>(null);
    const [appUpdateAvailable, setAppUpdateAvailable] = useState<AssistantUpdatePayload | null>(null);
    const [isDownloading, setIsDownloading] = useState(false);
    const [downloadProgress, setDownloadProgress] = useState(0);
    const [activeDownloadSource, setActiveDownloadSource] = useState("");
    const [downloadError, setDownloadError] = useState("");
    const [installerPath, setInstallerPath] = useState("");
    // Mirror of installerPath for the event handlers (they cannot read state
    // directly) plus a per-target stash: a completed download belongs to its
    // target fingerprint, so wobbling between channels (stable → beta →
    // stable) or rollback picks can hand the same file back instead of
    // forcing a second download of bytes that are still on disk and still
    // verified in the backend session.
    const installerPathRef = useRef("");
    const installerPathStashRef = useRef<Record<string, string>>({});
    useEffect(() => {
        installerPathRef.current = installerPath;
    }, [installerPath]);
    // True between a successful download and the app quitting for the
    // installer. The window closes on its own, so the modal has to say what is
    // happening instead of leaving the user on a "download complete" screen.
    const [isInstallingUpdate, setIsInstallingUpdate] = useState(false);
    const [updateDecisionPending, setUpdateDecisionPending] = useState(false);
    const updateDecisionInFlightRef = useRef(false);

    // Escape hatch for a launch that reported success but never quit the app.
    // Armed only after InstallDownloadedUpdate resolves — see startInstaller.
    const installQuitWatchdogRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const clearInstallQuitWatchdog = useCallback(() => {
        if (installQuitWatchdogRef.current != null) {
            clearTimeout(installQuitWatchdogRef.current);
            installQuitWatchdogRef.current = null;
        }
    }, []);
    useEffect(() => () => clearInstallQuitWatchdog(), [clearInstallQuitWatchdog]);

    // Name of the installer currently being downloaded. Cancel must target the
    // same key the backend registered; rebuilding it from the brand can miss.
    const activeDownloadFileNameRef = useRef("");
    const cancelActiveDownload = useCallback(() => {
        const installerBrand = String(brandDisplayName || "MaClaw").trim() || "MaClaw";
        const fileName = activeDownloadFileNameRef.current ||
            versionedInstallerFileName(
                `${installerBrand}-${isWindows ? "Setup.exe" : "Universal.pkg"}`,
                String(updateResultRef.current?.latest_version || ""),
            );
        CancelDownload(fileName);
        activeDownloadFileNameRef.current = "";
    }, [brandDisplayName, isWindows]);

    // Bumped whenever the update target changes. An in-flight download or
    // installer launch captures it so a late completion cannot offer or run
    // a file the dialog no longer describes.
    const updateTargetGenerationRef = useRef(0);
    // Re-entrancy across a single render: isInstallingUpdate only flips after
    // the next commit, so a fast double invoke would launch the installer
    // twice (two ShellExecute calls / stacked consent prompts).
    const installLaunchInFlightRef = useRef(false);
    // Swap the check result, dropping any downloaded-installer state when the
    // target changed. A same-target refresh keeps the completed download so a
    // re-check does not force a second download; a different version/channel
    // must never let "Install Now" run a file that does not match the
    // version the dialog shows.
    const handleUpdateResultChange = useCallback((next: any) => {
        const targetOf = (r: any) => [r?.latest_version, r?.channel, r?.is_rollback ? 1 : 0, r?.sha256].join("|");
        if (next?.check_failed) {
            // A failed check carries no target identity of its own. Treating
            // it as a target change would wipe a completed or in-progress
            // download whenever a transient network error interrupts a manual
            // recheck. The modal still renders the error through the state,
            // but the ref keeps pointing at the last real target: a retry
            // that lands on the same version then matches it and preserves
            // the download, while a genuinely different target still resets.
            setUpdateResult(next);
            return;
        }
        // React StrictMode may replay a state updater, so target-change side
        // effects cannot live inside setUpdateResult. The ref is the event-
        // handler snapshot; the state itself is only written here.
        if (targetOf(updateResultRef.current) !== targetOf(next)) {
            updateTargetGenerationRef.current += 1;
            // A download for the superseded target is useless: stop it so
            // a fresh download can start right away instead of waiting for
            // the old one to finish and be discarded. Only a real previous
            // target can own such a download: when the dialog had no target
            // yet, this fallback cancel would hit an unrelated silent
            // background download of the very version now being offered.
            if (String(updateResultRef.current?.latest_version || "").trim()) {
                cancelActiveDownload();
            }
            // A COMPLETED download is different from an in-flight one: its
            // bytes are version-qualified on disk and stay verified in the
            // backend session, so park the path under the old target's
            // fingerprint instead of throwing it away. Wobbling back (beta
            // toggle, rollback re-pick) then restores it below rather than
            // re-downloading the same installer.
            if (installerPathRef.current) {
                installerPathStashRef.current[targetOf(updateResultRef.current)] = installerPathRef.current;
            }
            clearInstallQuitWatchdog();
            setIsDownloading(false);
            // A background target change can arrive while ShellExecute is
            // still parked on the consent prompt. Keep the installing state
            // until that launch returns; resetting it here would invite a
            // second consent prompt for the new target.
            if (!installLaunchInFlightRef.current) {
                setIsInstallingUpdate(false);
            }
            setInstallerPath(installerPathStashRef.current[targetOf(next)] || "");
            setDownloadError("");
            setDownloadProgress(0);
            setActiveDownloadSource("");
        }
        updateResultRef.current = next;
        setUpdateResult(next);
    }, [clearInstallQuitWatchdog, cancelActiveDownload]);

    const handleOpenAppUpdate = useCallback(() => {
        if (!appUpdateAvailable) return;
        handleUpdateResultChange(appUpdateAvailable);
        // Opening the dialog must not clear an installer launch that is still
        // waiting for consent. Successful launches quit the app; failed ones
        // reset this state themselves, and the quit watchdog covers a launch
        // that reported success without the app exiting.
        setShowUpdateModal(true);
    }, [appUpdateAvailable, handleUpdateResultChange]);

    const handleOpenAppReleaseNotes = useCallback(() => {
        const releaseUrl = String(appUpdateAvailable?.release_url || (appUpdateAvailable as any)?.ReleaseUrl || "").trim();
        if (releaseUrl) {
            safeBrowserOpenURL(releaseUrl);
            return;
        }
        handleOpenAppUpdate();
    }, [appUpdateAvailable, handleOpenAppUpdate]);

    const handleDismissAppUpdate = useCallback((latestVersion: string) => {
        const normalizedVersion = String(latestVersion || "").trim();
        if (normalizedVersion) {
            try {
                window.localStorage.setItem(DISMISSED_APP_UPDATE_VERSION_KEY, normalizedVersion);
            } catch {
                // Ignore storage failures; clearing in-memory notice still honors this click.
            }
        }
        setAppUpdateAvailable(null);
    }, []);

    // Hands a downloaded installer to the backend after the explicit Install
    // Now choice and quits the app. Returns true when the installer was
    // launched; the app is about to exit in that case.
    //
    // `translate` is read through a ref: it is rebuilt on every render, so
    // depending on it directly would give this callback a new identity each
    // time and defeat the point of memoising it.
    const translateRef = useRef(translate);
    translateRef.current = translate;
    const formatTextRef = useRef(formatText);
    formatTextRef.current = formatText;
    const showToastMessageRef = useRef(showToastMessage);
    showToastMessageRef.current = showToastMessage;
    const startInstaller = useCallback(async (path: string): Promise<boolean> => {
        if (!path || installLaunchInFlightRef.current || updateDecisionInFlightRef.current) return false;
        installLaunchInFlightRef.current = true;
        setIsInstallingUpdate(true);
        // Clear the previous failure: a retry should not show the old reason
        // while it is in progress.
        setDownloadError("");
        const generation = updateTargetGenerationRef.current;
        try {
            // The target version lets the app detect on the next start
            // whether this update actually landed.
            const targetVersion = String(updateResult?.latest_version || "").trim();
            await InstallDownloadedUpdate(targetVersion);
            if (generation !== updateTargetGenerationRef.current) {
                // The update target changed while the launch was in flight
                // (consent prompt or launch-retry wait). Do not arm the quit
                // watchdog for a superseded install; release the guard so the
                // new target can be launched.
                installLaunchInFlightRef.current = false;
                setIsInstallingUpdate(false);
                return false;
            }
            // The backend quits ~500ms after this returns and the exit
            // controller bounds the whole shutdown, so surviving well past
            // this window means the quit never happened (a browser/dev
            // session, a blocked shutdown) and the manual fallback is due.
            // The timer cannot start earlier: the awaited call legitimately
            // blocks on the elevation consent prompt and on the launch
            // retry, and on slower machines either can stretch past any
            // fixed deadline without anything being wrong.
            // Drop a previous attempt's timer: in a session that never quits
            // it would otherwise fire inside this launch's grace window.
            clearInstallQuitWatchdog();
            installQuitWatchdogRef.current = setTimeout(() => {
                // Quit never came — reopen the manual path, which means
                // letting a fresh launch run again.
                installLaunchInFlightRef.current = false;
                setIsInstallingUpdate(false);
                setDownloadError((previous) => previous || translateRef.current("installStillPending"));
            }, 20000);
            return true;
        } catch (err) {
            installLaunchInFlightRef.current = false;
            // A superseded launch failure belongs to the old target; the
            // result change already reset the dialog, so report nothing.
            if (generation !== updateTargetGenerationRef.current) return false;
            console.error("Install launch error:", err);
            // The app stays open (the backend refuses to quit when the
            // installer did not start), so surface why and fall back to the
            // manual button instead of leaving the user on a dead screen.
            // The message goes in the dialog, which is already open: a toast
            // would show the same text twice.
            setIsInstallingUpdate(false);
            setDownloadError(err instanceof Error ? err.message : String(err));
            return false;
        }
    }, [updateResult, clearInstallQuitWatchdog]);

    // Same-render double-invoke guard: isDownloading only flips on the next
    // commit, so a fast double click could still run two downloads (the
    // backend would cancel one, but that churn is avoidable).
    const downloadInFlightRef = useRef(false);
    const handleDownload = async () => {
        if (!updateResult || downloadInFlightRef.current) return;
        // Use download_url if available (added in backend update), fallback to release_url
        const downloadUrl = updateResult.download_url || updateResult.release_url;
        if (!downloadUrl) return;
        downloadInFlightRef.current = true;
        const generation = updateTargetGenerationRef.current;

        setIsDownloading(true);
        setDownloadProgress(0);
        setActiveDownloadSource("");
        setDownloadError("");
        setInstallerPath("");
        setIsInstallingUpdate(false);

        // Keep the installer name aligned with the active OEM brand. The
        // backend selects the same target when building rollback URLs.
        const installerBrand = String(brandDisplayName || "MaClaw").trim() || "MaClaw";
        const fallbackFileName = `${installerBrand}-${isWindows ? "Setup.exe" : "Universal.pkg"}`;
        // Prefer the asset name carried by the signed update URL. This keeps
        // OEM installers correct even if brand metadata is still loading.
        // The version qualifier keeps this download from overwriting (or being
        // overwritten by) an installer of a different target release.
        const baseFileName = installerFileNameFromURL(downloadUrl) || fallbackFileName;
        const fileName = versionedInstallerFileName(baseFileName, String(updateResult?.latest_version || ""));
        // Remember the exact name: cancel has to target the same key the
        // backend registered, and the fallback name can differ from the one
        // the release URL actually carries.
        activeDownloadFileNameRef.current = fileName;
        const expectedSHA256 = updateResult.sha256 || "";

        try {
            const path = await DownloadUpdateWithSHA256(downloadUrl, fileName, expectedSHA256);
            if (generation !== updateTargetGenerationRef.current) {
                // The update target changed while this download ran (a
                // recheck or a rollback pick): the file belongs to the old
                // target, so do not offer or install it under the new
                // version the dialog now shows.
                setIsDownloading(false);
                return;
            }
            const targetVersion = String(updateResult?.latest_version || "").trim();
            // A rollback target must keep its identity all the way into the
            // deferred-install record: at execution time the backend validates
            // it against the rollback catalogue, because a deliberate rollback
            // is never the latest release a channel check would look for.
            await RegisterDownloadedUpdate(targetVersion, path, updateResult?.is_rollback ? "rollback" : String(updateResult?.channel || ""));
            setInstallerPath(path);
            // The promise result is authoritative; progress events are auxiliary
            // and may be unavailable in browser-based development sessions.
            setDownloadProgress(100);
            setIsDownloading(false);
            // Downloading is not install approval. Leave the completed
            // installer in the dialog so the user explicitly chooses install
            // now, a deferred time, next startup, or ignoring this release.
        } catch (err: any) {
            // A superseded download's failure belongs to the old target; the
            // result change already reset the dialog, so report nothing.
            if (generation !== updateTargetGenerationRef.current) return;
            console.error("Download error:", err);
            const message = err instanceof Error ? err.message : String(err);
            if (/cancelled|canceled/i.test(message)) {
                // A user cancel is not a failure: the cancelled progress event
                // may already have set the localized note — clear it and return
                // to the update offer rather than showing an error band.
                setDownloadError("");
            } else {
                // Some failures (for example, an unavailable Downloads folder) happen
                // before the backend can emit a download-progress error event.
                setDownloadError(message);
            }
            setIsDownloading(false);
        } finally {
            downloadInFlightRef.current = false;
        }
    };

    const handleRollbackRelease = (release: RollbackRelease | null) => {
        if (!release?.download_url) return;
        // The shared fingerprint guard decides whether the previous
        // download/install state still applies to this target; re-picking the
        // same rollback keeps a completed download instead of forcing a
        // second one.
        handleUpdateResultChange(rollbackUpdateResult(release));
    };
    const handleCancelDownload = () => cancelActiveDownload();

    // Explicit install action: the downloaded installer never launches until
    // the user chooses Install Now.
    const handleInstall = async () => {
        if (!installerPath) return;
        await startInstaller(installerPath);
    };

    const runUpdateDecision = useCallback(async (
        action: (version: string) => Promise<void>,
        successMessage: () => string,
    ) => {
        const version = String(updateResult?.latest_version || "").trim();
        // State only flips on the next commit, so a fast double click could
        // otherwise persist the same defer/ignore choice twice.
        if (!version || updateDecisionInFlightRef.current || installLaunchInFlightRef.current) return false;
        updateDecisionInFlightRef.current = true;
        setUpdateDecisionPending(true);
        setDownloadError("");
        const generation = updateTargetGenerationRef.current;
        try {
            await action(version);
            if (generation !== updateTargetGenerationRef.current) {
                // A background recheck may supersede this target while the
                // defer/ignore write is in flight. The write belongs to the
                // old target; never let it close or clear the newer update
                // choice now being shown.
                return false;
            }
            setShowUpdateModal(false);
            setAppUpdateAvailable(null);
            showToastMessageRef.current(successMessage(), 5000);
            return true;
        } catch (err) {
            if (generation !== updateTargetGenerationRef.current) return false;
            setDownloadError(err instanceof Error ? err.message : String(err));
            return false;
        } finally {
            updateDecisionInFlightRef.current = false;
            setUpdateDecisionPending(false);
        }
    }, [updateResult?.latest_version]);

    const handleDeferInstall = useCallback(async (hours: number) => {
        await runUpdateDecision(
            version => DeferDownloadedUpdateInstall(version, hours),
            () => formatTextRef.current("updateInstallDeferred", { hours: String(hours) }),
        );
    }, [runUpdateDecision]);

    const handleDeferInstallToNextStartup = useCallback(async () => {
        await runUpdateDecision(
            version => DeferDownloadedUpdateInstallToNextStartup(version),
            () => translateRef.current("updateInstallNextStartupDeferred"),
        );
    }, [runUpdateDecision]);

    const handleIgnoreThisUpdate = useCallback(async () => {
        const ignored = await runUpdateDecision(
            version => IgnoreUpdateVersion(version),
            () => translateRef.current("updateIgnored"),
        );
        if (ignored) {
            setInstallerPath("");
            updateResultRef.current = null;
            setUpdateResult(null);
        }
    }, [runUpdateDecision]);

    // Backend update events. Registered once at mount: the handlers reach
    // current state through refs, matching the semantics they had inside the
    // App component's mount effect.
    useEffect(() => {
        safeEventsOn("download-progress", (data: any) => {
            if (data.status === "downloading") {
                // The source-switch event carries only the host: coercing a
                // missing percentage would put NaN on the bar and aria-valuenow.
                if (data.percentage != null) setDownloadProgress(Math.floor(data.percentage));
                if (data.source) setActiveDownloadSource(String(data.source));
            } else if (data.status === "verifying") {
                // SHA256 verification in progress — keep showing progress at 100%
                setDownloadProgress(100);
                setIsDownloading(true);
            } else if (data.status === "completed") {
                setDownloadProgress(100);
                setIsDownloading(false);
            } else if (data.status === "error") {
                setDownloadError(data.error);
                setIsDownloading(false);
            } else if (data.status === "cancelled") {
                // A user cancel is not a failure: return to the update offer.
                setDownloadError("");
                setIsDownloading(false);
            }
        });

        safeEventsOn(EVENT_APP_UPDATE_AVAILABLE, (result: AssistantUpdatePayload) => {
            const payload = result as AssistantUpdatePayload & Record<string, unknown>;
            if (!(payload.has_update || payload.HasUpdate)) return;
            const latestVersion = String(payload.latest_version || payload.LatestVersion || "").trim();
            if (latestVersion) {
                try {
                    if (window.localStorage.getItem(DISMISSED_APP_UPDATE_VERSION_KEY) === latestVersion) return;
                } catch {
                    // localStorage may be unavailable in some embedded/dev contexts.
                }
            }
            setAppUpdateAvailable({ ...result, has_update: true, latest_version: latestVersion });
        });

        safeEventsOn(EVENT_APP_UPDATE_DOWNLOADED, (payload: any) => {
            const installer = String(payload?.installer_path || "").trim();
            const latestVersion = String(payload?.latest_version || "").trim();
            if (!installer || !latestVersion) return;
            // Keep the notice either way, but a user-picked rollback is a
            // deliberate choice: a background download of the newest release
            // completing right now must not cancel that download or steal the
            // dialog. The notice can reopen the newer target once the rollback
            // decision is done.
            setAppUpdateAvailable({ ...payload, has_update: true, latest_version: latestVersion });
            if (updateResultRef.current?.is_rollback) {
                return;
            }
            handleUpdateResultChange({ ...payload, has_update: true, latest_version: latestVersion });
            if (!installLaunchInFlightRef.current) {
                clearInstallQuitWatchdog();
                setIsInstallingUpdate(false);
            }
            setIsDownloading(false);
            setDownloadProgress(100);
            setActiveDownloadSource("");
            setDownloadError("");
            setInstallerPath(installer);
            setShowUpdateModal(true);
        });

        return () => {
            safeEventsOff("download-progress");
            safeEventsOff(EVENT_APP_UPDATE_AVAILABLE);
            safeEventsOff(EVENT_APP_UPDATE_DOWNLOADED);
        };
        // Mount-only, exactly like the App component effect these handlers
        // used to live in: they read current state through refs.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    return {
        showUpdateModal,
        setShowUpdateModal,
        updateResult,
        appUpdateAvailable,
        isDownloading,
        downloadProgress,
        activeDownloadSource,
        downloadError,
        setDownloadError,
        installerPath,
        isInstallingUpdate,
        setIsInstallingUpdate,
        updateDecisionPending,
        handleUpdateResultChange,
        handleOpenAppUpdate,
        handleOpenAppReleaseNotes,
        handleDismissAppUpdate,
        handleDownload,
        handleCancelDownload,
        handleInstall,
        handleRollbackRelease,
        handleDeferInstall,
        handleDeferInstallToNextStartup,
        handleIgnoreThisUpdate,
    };
}
