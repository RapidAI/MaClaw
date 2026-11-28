// @vitest-environment jsdom
import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useAppUpdateFlow } from '../useAppUpdateFlow';
import { EVENT_APP_UPDATE_AVAILABLE, EVENT_APP_UPDATE_DOWNLOADED } from '../../../constants/events';

const cancelDownloadMock = vi.hoisted(() => vi.fn());
const eventHandlers = vi.hoisted(() => new Map<string, Array<(...args: any[]) => void>>());

vi.mock('../../../../wailsjs/go/main/App', () => ({
    CancelDownload: cancelDownloadMock,
    DeferDownloadedUpdateInstall: vi.fn(async () => undefined),
    DeferDownloadedUpdateInstallToNextStartup: vi.fn(async () => undefined),
    DownloadUpdateWithSHA256: vi.fn(async () => 'C:/Downloads/MaClaw-Setup-8.2.exe'),
    IgnoreUpdateVersion: vi.fn(async () => undefined),
    InstallDownloadedUpdate: vi.fn(async () => undefined),
    RegisterDownloadedUpdate: vi.fn(async () => undefined),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    BrowserOpenURL: vi.fn(),
    EventsOn: (name: string, cb: (...args: any[]) => void) => {
        const list = eventHandlers.get(name) || [];
        list.push(cb);
        eventHandlers.set(name, list);
        return () => undefined;
    },
    EventsOff: vi.fn(),
}));

const emit = (name: string, payload: any) => {
    for (const cb of eventHandlers.get(name) || []) cb(payload);
};

const renderFlow = () => renderHook(() => useAppUpdateFlow({
    brandDisplayName: 'MaClaw',
    isWindows: true,
    translate: (key: string) => key,
    formatText: (key: string) => key,
    showToastMessage: vi.fn(),
}));

beforeEach(() => {
    eventHandlers.clear();
    cancelDownloadMock.mockClear();
    window.localStorage.clear();
});

afterEach(() => {
    cleanup();
});

describe('useAppUpdateFlow check_failed transparency', () => {
    const downloadedV82 = {
        has_update: true,
        latest_version: 'V8.2.0.12010',
        channel: 'stable',
        installer_path: 'C:/Downloads/MaClaw-Setup-8.2.0.12010.exe',
    };

    it('a failed recheck does not wipe a completed download', () => {
        const { result } = renderFlow();
        act(() => { emit(EVENT_APP_UPDATE_DOWNLOADED, { ...downloadedV82 }); });
        expect(result.current.installerPath).toBe(downloadedV82.installer_path);

        act(() => {
            result.current.handleUpdateResultChange({ check_failed: true, has_update: false, error: 'network down' });
        });
        // The modal may render the error, but the ready installer must survive.
        expect(result.current.updateResult?.check_failed).toBe(true);
        expect(result.current.installerPath).toBe(downloadedV82.installer_path);
        expect(cancelDownloadMock).not.toHaveBeenCalled();
    });

    it('a retry landing on the same version after a failure keeps the download', () => {
        const { result } = renderFlow();
        act(() => { emit(EVENT_APP_UPDATE_DOWNLOADED, { ...downloadedV82 }); });
        act(() => {
            result.current.handleUpdateResultChange({ check_failed: true, has_update: false });
        });
        act(() => {
            result.current.handleUpdateResultChange({
                has_update: true,
                latest_version: 'V8.2.0.12010',
                channel: 'stable',
            });
        });
        expect(result.current.installerPath).toBe(downloadedV82.installer_path);
        expect(cancelDownloadMock).not.toHaveBeenCalled();
    });

    it('a genuinely different target after a failure still resets the download', () => {
        const { result } = renderFlow();
        act(() => { emit(EVENT_APP_UPDATE_DOWNLOADED, { ...downloadedV82 }); });
        act(() => {
            result.current.handleUpdateResultChange({ check_failed: true, has_update: false });
        });
        act(() => {
            result.current.handleUpdateResultChange({
                has_update: true,
                latest_version: 'V8.3.0.12011',
                channel: 'stable',
            });
        });
        expect(result.current.installerPath).toBe('');
    });

    it('a channel wobble hands the completed download back instead of losing it', () => {
        const { result } = renderFlow();
        act(() => { emit(EVENT_APP_UPDATE_DOWNLOADED, { ...downloadedV82 }); });
        expect(result.current.installerPath).toBe(downloadedV82.installer_path);

        // stable v8.2 -> beta v8.3: the beta target starts clean.
        act(() => {
            result.current.handleUpdateResultChange({
                has_update: true,
                latest_version: 'V8.3.0.12011-beta.1',
                channel: 'beta',
            });
        });
        expect(result.current.installerPath).toBe('');

        // beta -> back to the same stable target: the completed installer is
        // still on disk and verified, so the dialog offers it again instead
        // of making the user re-download the same bytes.
        act(() => {
            result.current.handleUpdateResultChange({
                has_update: true,
                latest_version: 'V8.2.0.12010',
                channel: 'stable',
            });
        });
        expect(result.current.installerPath).toBe(downloadedV82.installer_path);
    });

    it('a stash never leaks across targets that differ only by channel', () => {
        const { result } = renderFlow();
        act(() => { emit(EVENT_APP_UPDATE_DOWNLOADED, { ...downloadedV82 }); });
        // Same version on the beta channel is a different target: the stable
        // installer must not be offered for it.
        act(() => {
            result.current.handleUpdateResultChange({
                has_update: true,
                latest_version: 'V8.2.0.12010',
                channel: 'beta',
            });
        });
        expect(result.current.installerPath).toBe('');
    });

    it('a dismissed version stays dismissed after a silent download completes', () => {
        window.localStorage.setItem('maclaw:dismissed-app-update-version', 'V8.2.0.12010');
        const { result } = renderFlow();
        act(() => { emit(EVENT_APP_UPDATE_DOWNLOADED, { ...downloadedV82 }); });
        // Neither the notice nor the modal state may resurface the version.
        expect(result.current.appUpdateAvailable).toBeNull();
        expect(result.current.installerPath).toBe('');
        expect(result.current.showUpdateModal).toBe(false);
    });

    it('the downloaded event for a different version is unaffected by dismissal', () => {
        window.localStorage.setItem('maclaw:dismissed-app-update-version', 'V8.1.0.11999');
        const { result } = renderFlow();
        act(() => { emit(EVENT_APP_UPDATE_DOWNLOADED, { ...downloadedV82 }); });
        expect(result.current.installerPath).toBe(downloadedV82.installer_path);
    });

    it('a direct target change still cancels the in-flight download', () => {
        const { result } = renderFlow();
        act(() => {
            result.current.handleUpdateResultChange({
                has_update: true,
                latest_version: 'V8.2.0.12010',
                channel: 'stable',
            });
        });
        act(() => {
            result.current.handleUpdateResultChange({
                has_update: true,
                latest_version: 'V8.3.0.12011',
                channel: 'stable',
            });
        });
        expect(cancelDownloadMock).toHaveBeenCalledTimes(1);
    });

    it('the first-ever failed check does not cancel an unrelated silent download', () => {
        const { result } = renderFlow();
        act(() => {
            result.current.handleUpdateResultChange({ check_failed: true, has_update: false });
        });
        expect(cancelDownloadMock).not.toHaveBeenCalled();
    });
});


describe('useAppUpdateFlow stale downloaded events', () => {
    it('a completed download of a different version must not hijack the live target', () => {
        const { result } = renderFlow();
        // User already picked a newer beta target in the dialog.
        act(() => {
            result.current.handleUpdateResultChange({
                has_update: true,
                latest_version: 'V9.0.0.12100',
                channel: 'beta',
            });
        });
        // A background silent download of the older stable build completes.
        act(() => {
            emit(EVENT_APP_UPDATE_DOWNLOADED, {
                latest_version: 'V8.2.0.12010',
                channel: 'stable',
                installer_path: 'C:/Downloads/MaClaw-Setup-8.2.0.12010.exe',
            });
        });
        // The dialog still decides on the beta target; the stale completion
        // must not reset it, steal the installer path, or cancel downloads.
        expect(result.current.updateResult?.latest_version).toBe('V9.0.0.12100');
        expect(result.current.updateResult?.channel).toBe('beta');
        expect(result.current.installerPath).toBe('');
        expect(cancelDownloadMock).not.toHaveBeenCalled();
        // The notice still records the ready stable installer.
        expect(result.current.appUpdateAvailable?.latest_version).toBe('V8.2.0.12010');
    });

    it('a completed download matching the live target takes over as usual', () => {
        const { result } = renderFlow();
        act(() => {
            result.current.handleUpdateResultChange({
                has_update: true,
                latest_version: 'V8.2.0.12010',
                channel: 'stable',
            });
        });
        act(() => {
            emit(EVENT_APP_UPDATE_DOWNLOADED, {
                latest_version: 'V8.2.0.12010',
                channel: 'stable',
                installer_path: 'C:/Downloads/MaClaw-Setup-8.2.0.12010.exe',
            });
        });
        expect(result.current.updateResult?.latest_version).toBe('V8.2.0.12010');
        expect(result.current.installerPath).toBe('C:/Downloads/MaClaw-Setup-8.2.0.12010.exe');
    });
});


describe('useAppUpdateFlow notice preservation', () => {
    it('deciding an older version keeps a newer update notice alive', async () => {
        const { result } = renderFlow();
        // A newer stable release was notified in the background.
        act(() => {
            emit(EVENT_APP_UPDATE_AVAILABLE, {
                has_update: true,
                latest_version: 'V8.3.0.12050',
                channel: 'stable',
            });
        });
        // The dialog is deciding on the older downloaded target.
        act(() => {
            result.current.handleUpdateResultChange({
                has_update: true,
                latest_version: 'V8.2.0.12010',
                channel: 'stable',
            });
        });
        await act(async () => { await result.current.handleDeferInstall(2); });
        // Deferring v8.2 must not destroy the v8.3 notice: its one-shot emit
        // was already consumed, so a cleared notice would never resurface.
        expect(result.current.appUpdateAvailable?.latest_version).toBe('V8.3.0.12050');
        expect(result.current.showUpdateModal).toBe(false);
    });

    it('deciding the notified version retires its notice', async () => {
        const { result } = renderFlow();
        act(() => {
            emit(EVENT_APP_UPDATE_AVAILABLE, {
                has_update: true,
                latest_version: 'V8.2.0.12010',
                channel: 'stable',
            });
        });
        act(() => {
            result.current.handleUpdateResultChange({
                has_update: true,
                latest_version: 'V8.2.0.12010',
                channel: 'stable',
            });
        });
        await act(async () => { await result.current.handleDeferInstall(2); });
        expect(result.current.appUpdateAvailable).toBeNull();
    });
});
