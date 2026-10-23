// @vitest-environment jsdom
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { UpdateModal, downloadSourceName, failedUpdateResult, formatVersion, isRestorableStableResult, latestVersionLabel, rollbackReleaseLabel } from './UpdateModal';

describe('downloadSourceName', () => {
    it('uses the first backend download candidate and preserves signed URL query values', () => {
        expect(downloadSourceName('https://cdn.example.com/update.exe?signature=a,b|https://github.com/RapidAI/MaClaw/releases/download/v1/update.exe'))
            .toBe('cdn.example.com');
    });

    it('maps known download hosts to their user-facing station names', () => {
        expect(downloadSourceName('github.com')).toBe('GitHub Releases');
        expect(downloadSourceName('https://downloads.example.r2.dev/update.exe')).toBe('Cloudflare R2');
    });

    it('returns an empty label when no download source is available', () => {
        expect(downloadSourceName('')).toBe('');
        expect(downloadSourceName(undefined)).toBe('');
    });

    it('splits on newlines the same way the backend combines candidate URLs', () => {
        expect(downloadSourceName('https://pub.example.r2.dev/latest/app.exe\nhttps://github.com/RapidAI/MaClaw/releases/download/v1/app.exe'))
            .toBe('Cloudflare R2');
    });
});

describe('isRestorableStableResult', () => {
    it('accepts stable and legacy payloads', () => {
        expect(isRestorableStableResult({ channel: 'stable', has_update: true })).toBe(true);
        expect(isRestorableStableResult({ has_update: false })).toBe(true);
    });

    it('rejects beta and failed payloads', () => {
        expect(isRestorableStableResult({ channel: 'beta' })).toBe(false);
        expect(isRestorableStableResult({ channel: 'stable', check_failed: true })).toBe(false);
        expect(isRestorableStableResult(null)).toBe(false);
    });
});

describe('rollbackReleaseLabel', () => {
    it('shows the server-provided formal build and publication date', () => {
        expect(rollbackReleaseLabel({ build: '11970', published_at: '2026-08-28T10:00:00Z' })).toBe('11970 · 2026-08-28');
    });

    it('does not derive a version when a release field is missing', () => {
        expect(rollbackReleaseLabel({ build: '11970' })).toBe('11970');
    });
});
describe('formatVersion', () => {
    it('spells both sides of the comparison the same way', () => {
        // The backend tags releases as "V8.1.0.12434" while the running build
        // arrives bare, so the pair used to render as v8.1… / V8.1….
        expect(formatVersion('8.1.0.12431')).toBe('v8.1.0.12431');
        expect(formatVersion('V8.1.0.12434')).toBe('v8.1.0.12434');
        expect(formatVersion('v8.1.0.12434')).toBe('v8.1.0.12434');
    });

    it('keeps an empty or missing version empty', () => {
        expect(formatVersion('')).toBe('');
        expect(formatVersion(undefined)).toBe('');
        expect(formatVersion('   ')).toBe('');
    });
});

describe('latestVersionLabel', () => {
    it('prefixes a release version', () => {
        expect(latestVersionLabel({ latest_version: 'V8.1.0.12434' })).toBe('v8.1.0.12434');
    });

    it('leaves a rollback build number alone', () => {
        // A rollback target is a build id, not a version: "v12434" would be
        // a version that never existed.
        expect(latestVersionLabel({ latest_version: '12434', is_rollback: true })).toBe('12434');
    });

    it('handles a missing payload', () => {
        expect(latestVersionLabel(null)).toBe('');
        expect(latestVersionLabel({ latest_version: '  ' })).toBe('');
    });
});

describe('failedUpdateResult', () => {
    it('marks the payload as a check failure', () => {
        const got = failedUpdateResult('network down');
        expect(got.check_failed).toBe(true);
        expect(got.has_update).toBe(false);
        expect(got.message).toBe('network down');
    });
});

// The update flow installs automatically after a successful download, so these
// states decide what the user sees in the moment before the app quits.
describe('UpdateModal install states', () => {
    const translations: Record<string, string> = {
        installingUpdate: 'Installing the update…',
        installingUpdateHint: 'The app will close and restart automatically.',
        downloadComplete: 'Download complete',
        installNow: 'Install Now',
        installLaunchError: 'Failed to launch installer: {error}',
        close: 'Close',
        rollback: 'Roll back',
        currentVersion: 'Current version',
        latestVersion: 'Latest version',
        foundNewVersion: 'Update available',
        onlineUpdate: 'Online Update',
        betaChannel: 'Beta',
        downloadSource: 'Source',
    };
    const t = (key: string) => translations[key] || key;
    const baseProps = {
        updateResult: { has_update: true, latest_version: '9.9.9' },
        appVersion: '1.0.0',
        isDownloading: false,
        downloadProgress: 100,
        installerPath: 'C:/Downloads/MaClaw-Setup.exe',
        downloadError: '',
        t,
        onCancelDownload: () => {},
        onDownload: () => {},
        onInstall: () => {},
        onClose: () => {},
    };

    it('replaces the manual install button while the installer is starting', () => {
        render(<UpdateModal {...baseProps} isInstalling />);
        expect(screen.getByText('Installing the update…')).toBeTruthy();
        expect(screen.queryByText('Install Now')).toBeNull();
    });

    it('cannot be dismissed while the app is about to quit', () => {
        render(<UpdateModal {...baseProps} isInstalling />);
        const close = screen.getByText('Close') as HTMLButtonElement;
        expect(close.disabled).toBe(true);
    });

    it('offers the manual button and the reason when the automatic launch failed', () => {
        render(<UpdateModal {...baseProps} downloadError="installer did not start" />);
        expect(screen.getByText('Install Now')).toBeTruthy();
        // The caller's message is already a full sentence; wrapping it in the
        // installLaunchError template would read as "Failed to launch: it did
        // not take over" and hide the real cause.
        expect(screen.getByRole('alert').textContent).toBe('installer did not start');
    });

    it('shows the plain install button when nothing failed', () => {
        render(<UpdateModal {...baseProps} />);
        expect(screen.getByText('Install Now')).toBeTruthy();
        expect(screen.queryByRole('alert')).toBeNull();
    });
});
