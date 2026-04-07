import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, waitFor, fireEvent, screen } from '@testing-library/react';

const ClawNetStopDaemonMock = vi.fn();
const ClawNetIsRunningMock = vi.fn();
const ClawNetGetStatusMock = vi.fn();
const ClawNetGetPeersMock = vi.fn();
const ClawNetGetCreditsMock = vi.fn();
const ClawNetGetBinaryPathMock = vi.fn();
const ClawNetEnsureDaemonWithDownloadMock = vi.fn();
const ClawNetHasIdentityMock = vi.fn();
const ClawNetExportIdentityMock = vi.fn();
const ClawNetImportIdentityMock = vi.fn();
const ClawNetOnlineBackupKeyMock = vi.fn();
const ClawNetOnlineRestoreKeyMock = vi.fn();
const ClawNetGetTransactionsMock = vi.fn();
const ClawNetGetCreditsAuditMock = vi.fn();
const ClawNetGetLeaderboardMock = vi.fn();
const ClawNetAutoPickerGetStatusMock = vi.fn();
const ClawNetAutoPickerConfigureMock = vi.fn();
const ClawNetAutoPickerTriggerNowMock = vi.fn();
const ClawNetGetDaemonInfoMock = vi.fn();
const ClawNetManualUpdateMock = vi.fn();
const EventsOnMock = vi.fn();
const EventsOffMock = vi.fn();
const showConfirmMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    ClawNetStopDaemon: (...args: unknown[]) => ClawNetStopDaemonMock(...args),
    ClawNetIsRunning: (...args: unknown[]) => ClawNetIsRunningMock(...args),
    ClawNetGetStatus: (...args: unknown[]) => ClawNetGetStatusMock(...args),
    ClawNetGetPeers: (...args: unknown[]) => ClawNetGetPeersMock(...args),
    ClawNetGetCredits: (...args: unknown[]) => ClawNetGetCreditsMock(...args),
    ClawNetGetBinaryPath: (...args: unknown[]) => ClawNetGetBinaryPathMock(...args),
    ClawNetEnsureDaemonWithDownload: (...args: unknown[]) => ClawNetEnsureDaemonWithDownloadMock(...args),
    ClawNetHasIdentity: (...args: unknown[]) => ClawNetHasIdentityMock(...args),
    ClawNetExportIdentity: (...args: unknown[]) => ClawNetExportIdentityMock(...args),
    ClawNetImportIdentity: (...args: unknown[]) => ClawNetImportIdentityMock(...args),
    ClawNetOnlineBackupKey: (...args: unknown[]) => ClawNetOnlineBackupKeyMock(...args),
    ClawNetOnlineRestoreKey: (...args: unknown[]) => ClawNetOnlineRestoreKeyMock(...args),
    ClawNetGetTransactions: (...args: unknown[]) => ClawNetGetTransactionsMock(...args),
    ClawNetGetCreditsAudit: (...args: unknown[]) => ClawNetGetCreditsAuditMock(...args),
    ClawNetGetLeaderboard: (...args: unknown[]) => ClawNetGetLeaderboardMock(...args),
    ClawNetAutoPickerGetStatus: (...args: unknown[]) => ClawNetAutoPickerGetStatusMock(...args),
    ClawNetAutoPickerConfigure: (...args: unknown[]) => ClawNetAutoPickerConfigureMock(...args),
    ClawNetAutoPickerTriggerNow: (...args: unknown[]) => ClawNetAutoPickerTriggerNowMock(...args),
    ClawNetGetDaemonInfo: (...args: unknown[]) => ClawNetGetDaemonInfoMock(...args),
    ClawNetManualUpdate: (...args: unknown[]) => ClawNetManualUpdateMock(...args),
}));

vi.mock('../../../../wailsjs/runtime/runtime', () => ({
    EventsOn: (...args: unknown[]) => EventsOnMock(...args),
    EventsOff: (...args: unknown[]) => EventsOffMock(...args),
}));

vi.mock('../../CustomDialog', () => ({
    useDialog: () => ({
        showConfirm: (...args: unknown[]) => showConfirmMock(...args),
    }),
}));

import { ClawNetPanel } from '../ClawNetPanel';

function renderPanel(clawnetEnabled: boolean, extraConfig: Record<string, unknown> = {}) {
    const saveRemoteConfigField = vi.fn();
    const onRunningChange = vi.fn();
    render(
        <ClawNetPanel
            config={{ clawnet_enabled: clawnetEnabled, ...extraConfig } as any}
            saveRemoteConfigField={saveRemoteConfigField}
            lang="zh-Hans"
            onRunningChange={onRunningChange}
        />,
    );
    return { saveRemoteConfigField, onRunningChange };
}

describe('ClawNetPanel guard behavior', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        ClawNetIsRunningMock.mockResolvedValue(false);
        ClawNetGetBinaryPathMock.mockResolvedValue('clawnet');
        ClawNetHasIdentityMock.mockResolvedValue({ ok: true, exists: false, path: '' });
        ClawNetAutoPickerGetStatusMock.mockResolvedValue({ ok: true, enabled: false, running: false });
        ClawNetGetDaemonInfoMock.mockResolvedValue({ ok: true, pid: 0, bin_path: 'clawnet' });
        ClawNetEnsureDaemonWithDownloadMock.mockResolvedValue({ ok: true });
        ClawNetStopDaemonMock.mockResolvedValue(undefined);
        ClawNetManualUpdateMock.mockResolvedValue({ ok: true, updated: true, restarted: true });
        ClawNetExportIdentityMock.mockResolvedValue({ ok: true, path: '/tmp/id.key' });
        ClawNetImportIdentityMock.mockResolvedValue({ ok: true, restarted: false });
        ClawNetOnlineBackupKeyMock.mockResolvedValue({ ok: true });
        ClawNetOnlineRestoreKeyMock.mockResolvedValue({ ok: true, restarted: false });
        ClawNetGetTransactionsMock.mockResolvedValue({ ok: true, transactions: [] });
        ClawNetGetCreditsAuditMock.mockResolvedValue({ ok: true, audit: [] });
        ClawNetGetLeaderboardMock.mockResolvedValue({ ok: true, leaderboard: [] });
        ClawNetGetStatusMock.mockResolvedValue({ ok: true, peers: 0, unread_dm: 0, version: '1.0.0' });
        ClawNetGetPeersMock.mockResolvedValue({ ok: true, peers: [] });
        ClawNetGetCreditsMock.mockResolvedValue({ ok: true, balance: 0 });
        showConfirmMock.mockResolvedValue(true);
    });

    it('does not auto-start when clawnet is disabled', async () => {
        renderPanel(false);

        await waitFor(() => {
            expect(ClawNetGetBinaryPathMock).toHaveBeenCalled();
        });

        expect(ClawNetEnsureDaemonWithDownloadMock).not.toHaveBeenCalled();
    });

    it('auto-starts only when clawnet is enabled and offline', async () => {
        renderPanel(true);

        await waitFor(() => {
            expect(ClawNetEnsureDaemonWithDownloadMock).toHaveBeenCalledTimes(1);
        });
    });

    it('saving disabled toggle does not trigger start', async () => {
        const { saveRemoteConfigField } = renderPanel(true);

        await waitFor(() => {
            expect(ClawNetEnsureDaemonWithDownloadMock).toHaveBeenCalledTimes(1);
        });
        ClawNetEnsureDaemonWithDownloadMock.mockClear();

        fireEvent.click(screen.getByLabelText('启用虾网'));

        expect(saveRemoteConfigField).toHaveBeenCalledWith({ clawnet_enabled: false });
        expect(ClawNetEnsureDaemonWithDownloadMock).not.toHaveBeenCalled();
    });
});
