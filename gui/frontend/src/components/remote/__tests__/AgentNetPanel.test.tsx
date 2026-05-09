import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, waitFor, fireEvent, screen } from '@testing-library/react';

const AgentNetStopDaemonMock = vi.fn();
const AgentNetIsRunningMock = vi.fn();
const AgentNetGetStatusMock = vi.fn();
const AgentNetGetPeersMock = vi.fn();
const AgentNetGetCreditsMock = vi.fn();
const AgentNetGetBinaryPathMock = vi.fn();
const AgentNetEnsureDaemonWithDownloadMock = vi.fn();
const AgentNetHasIdentityMock = vi.fn();
const AgentNetExportIdentityMock = vi.fn();
const AgentNetImportIdentityMock = vi.fn();
const AgentNetOnlineBackupKeyMock = vi.fn();
const AgentNetOnlineRestoreKeyMock = vi.fn();
const AgentNetGetLeaderboardMock = vi.fn();
const AgentNetAutoPickerGetStatusMock = vi.fn();
const AgentNetAutoPickerConfigureMock = vi.fn();
const AgentNetAutoPickerTriggerNowMock = vi.fn();
const AgentNetGetDaemonInfoMock = vi.fn();
const AgentNetManualUpdateMock = vi.fn();
const EventsOnMock = vi.fn();
const EventsOffMock = vi.fn();
const showConfirmMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    AgentNetStopDaemon: (...args: unknown[]) => AgentNetStopDaemonMock(...args),
    AgentNetIsRunning: (...args: unknown[]) => AgentNetIsRunningMock(...args),
    AgentNetGetStatus: (...args: unknown[]) => AgentNetGetStatusMock(...args),
    AgentNetGetPeers: (...args: unknown[]) => AgentNetGetPeersMock(...args),
    AgentNetGetCredits: (...args: unknown[]) => AgentNetGetCreditsMock(...args),
    AgentNetGetBinaryPath: (...args: unknown[]) => AgentNetGetBinaryPathMock(...args),
    AgentNetEnsureDaemonWithDownload: (...args: unknown[]) => AgentNetEnsureDaemonWithDownloadMock(...args),
    AgentNetHasIdentity: (...args: unknown[]) => AgentNetHasIdentityMock(...args),
    AgentNetExportIdentity: (...args: unknown[]) => AgentNetExportIdentityMock(...args),
    AgentNetImportIdentity: (...args: unknown[]) => AgentNetImportIdentityMock(...args),
    AgentNetOnlineBackupKey: (...args: unknown[]) => AgentNetOnlineBackupKeyMock(...args),
    AgentNetOnlineRestoreKey: (...args: unknown[]) => AgentNetOnlineRestoreKeyMock(...args),
    AgentNetGetLeaderboard: (...args: unknown[]) => AgentNetGetLeaderboardMock(...args),
    AgentNetAutoPickerGetStatus: (...args: unknown[]) => AgentNetAutoPickerGetStatusMock(...args),
    AgentNetAutoPickerConfigure: (...args: unknown[]) => AgentNetAutoPickerConfigureMock(...args),
    AgentNetAutoPickerTriggerNow: (...args: unknown[]) => AgentNetAutoPickerTriggerNowMock(...args),
    AgentNetGetDaemonInfo: (...args: unknown[]) => AgentNetGetDaemonInfoMock(...args),
    AgentNetManualUpdate: (...args: unknown[]) => AgentNetManualUpdateMock(...args),
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

import { AgentNetPanel } from '../AgentNetPanel';

function renderPanel(agentNetEnabled: boolean, extraConfig: Record<string, unknown> = {}) {
    const saveRemoteConfigField = vi.fn();
    const onRunningChange = vi.fn();
    render(
        <AgentNetPanel
            config={{ agentnet_enabled: agentNetEnabled, ...extraConfig } as any}
            saveRemoteConfigField={saveRemoteConfigField}
            lang="zh-Hans"
            onRunningChange={onRunningChange}
        />,
    );
    return { saveRemoteConfigField, onRunningChange };
}

describe('AgentNetPanel guard behavior', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        AgentNetIsRunningMock.mockResolvedValue(false);
        AgentNetGetBinaryPathMock.mockResolvedValue('AgentNet');
        AgentNetHasIdentityMock.mockResolvedValue({ ok: true, exists: false, path: '' });
        AgentNetAutoPickerGetStatusMock.mockResolvedValue({ ok: true, enabled: false, running: false });
        AgentNetGetDaemonInfoMock.mockResolvedValue({ ok: true, pid: 0, bin_path: 'AgentNet' });
        AgentNetEnsureDaemonWithDownloadMock.mockResolvedValue({ ok: true });
        AgentNetStopDaemonMock.mockResolvedValue(undefined);
        AgentNetManualUpdateMock.mockResolvedValue({ ok: true, updated: true, restarted: true });
        AgentNetExportIdentityMock.mockResolvedValue({ ok: true, path: '/tmp/id.key' });
        AgentNetImportIdentityMock.mockResolvedValue({ ok: true, restarted: false });
        AgentNetOnlineBackupKeyMock.mockResolvedValue({ ok: true });
        AgentNetOnlineRestoreKeyMock.mockResolvedValue({ ok: true, restarted: false });
        AgentNetGetLeaderboardMock.mockResolvedValue({ ok: true, leaderboard: [] });
        AgentNetGetStatusMock.mockResolvedValue({ ok: true, peers: 0, unread_dm: 0, version: '1.0.0' });
        AgentNetGetPeersMock.mockResolvedValue({ ok: true, peers: [] });
        AgentNetGetCreditsMock.mockResolvedValue({ ok: true, balance: 0 });
        showConfirmMock.mockResolvedValue(true);
    });

    it('does not auto-start when AgentNet is disabled', async () => {
        renderPanel(false);

        await waitFor(() => {
            expect(AgentNetGetBinaryPathMock).toHaveBeenCalled();
        });

        expect(AgentNetEnsureDaemonWithDownloadMock).not.toHaveBeenCalled();
    });

    it('auto-starts only when AgentNet is enabled and offline', async () => {
        renderPanel(true);

        await waitFor(() => {
            expect(AgentNetEnsureDaemonWithDownloadMock).toHaveBeenCalledTimes(1);
        });
    });

    it('saving disabled toggle does not trigger start', async () => {
        const { saveRemoteConfigField } = renderPanel(true);

        await waitFor(() => {
            expect(AgentNetEnsureDaemonWithDownloadMock).toHaveBeenCalledTimes(1);
        });
        AgentNetEnsureDaemonWithDownloadMock.mockClear();

        fireEvent.click(screen.getByLabelText(/启用智网/));

        expect(saveRemoteConfigField).toHaveBeenCalledWith({ agentnet_enabled: false });
        expect(AgentNetEnsureDaemonWithDownloadMock).not.toHaveBeenCalled();
    });
});
