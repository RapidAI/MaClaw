import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { main } from '../../../../wailsjs/go/models';

const runtimeHandlers = new Map<string, (payload?: unknown) => void>();
const startRemoteSessionMock = vi.fn();
const startRemoteHandoffSessionMock = vi.fn();
const listValidProvidersMock = vi.fn();
const loadConfigMock = vi.fn();
const listRemoteToolMetadataMock = vi.fn();
const listRemoteSessionsMock = vi.fn();
const getRemoteActivationStatusMock = vi.fn();
const getRemoteConnectionStatusMock = vi.fn();
const getLastRemoteSmokeReportMock = vi.fn();
const getRemoteToolReadinessMock = vi.fn();
const getRemoteToolLaunchProbeMock = vi.fn();
const getRemotePTYProbeMock = vi.fn();
const runRemoteToolSmokeMock = vi.fn();
const activateRemoteMock = vi.fn();
const probeRemoteHubMock = vi.fn();
const verifyRemoteActivationMock = vi.fn();
const reconnectRemoteHubMock = vi.fn();
const clearRemoteActivationMock = vi.fn();
const saveConfigMock = vi.fn();
const checkToolsStatusMock = vi.fn();
const installToolOnDemandMock = vi.fn();
const sendRemoteSessionInputMock = vi.fn();
const interruptRemoteSessionMock = vi.fn();
const killRemoteSessionMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    ActivateRemote: (...args: unknown[]) => activateRemoteMock(...args),
    CheckToolsStatus: (...args: unknown[]) => checkToolsStatusMock(...args),
    ClearRemoteActivation: (...args: unknown[]) => clearRemoteActivationMock(...args),
    GetLastRemoteSmokeReport: (...args: unknown[]) => getLastRemoteSmokeReportMock(...args),
    GetRemoteActivationStatus: (...args: unknown[]) => getRemoteActivationStatusMock(...args),
    GetRemoteConnectionStatus: (...args: unknown[]) => getRemoteConnectionStatusMock(...args),
    GetRemotePTYProbe: (...args: unknown[]) => getRemotePTYProbeMock(...args),
    GetRemoteToolLaunchProbe: (...args: unknown[]) => getRemoteToolLaunchProbeMock(...args),
    GetRemoteToolReadiness: (...args: unknown[]) => getRemoteToolReadinessMock(...args),
    InstallToolOnDemand: (...args: unknown[]) => installToolOnDemandMock(...args),
    LoadConfig: (...args: unknown[]) => loadConfigMock(...args),
    ListRemoteSessions: (...args: unknown[]) => listRemoteSessionsMock(...args),
    ListRemoteToolMetadata: (...args: unknown[]) => listRemoteToolMetadataMock(...args),
    ListValidProviders: (...args: unknown[]) => listValidProvidersMock(...args),
    ProbeRemoteHub: (...args: unknown[]) => probeRemoteHubMock(...args),
    ReconnectRemoteHub: (...args: unknown[]) => reconnectRemoteHubMock(...args),
    RunRemoteToolSmoke: (...args: unknown[]) => runRemoteToolSmokeMock(...args),
    SaveConfig: (...args: unknown[]) => saveConfigMock(...args),
    SendRemoteSessionInput: (...args: unknown[]) => sendRemoteSessionInputMock(...args),
    InterruptRemoteSession: (...args: unknown[]) => interruptRemoteSessionMock(...args),
    KillRemoteSession: (...args: unknown[]) => killRemoteSessionMock(...args),
    StartRemoteHandoffSession: (...args: unknown[]) => startRemoteHandoffSessionMock(...args),
    StartRemoteSession: (...args: unknown[]) => startRemoteSessionMock(...args),
    VerifyRemoteActivation: (...args: unknown[]) => verifyRemoteActivationMock(...args),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn((event: string, handler: (payload?: unknown) => void) => {
        runtimeHandlers.set(event, handler);
        return () => runtimeHandlers.delete(event);
    }),
    EventsOff: vi.fn((event: string) => {
        runtimeHandlers.delete(event);
    }),
}));

import { useRemotePanel } from '../useRemotePanel';

function buildConfig(overrides: Record<string, unknown> = {}) {
    return new main.AppConfig({
        active_tool: 'claude',
        remote_enabled: true,
        remote_hub_url: 'https://hub.example.com',
        remote_email: 'user@example.com',
        projects: [{ id: 'p1', path: '/workspace', use_proxy: false }],
        claude: {
            current_model: '王',
            models: [
                { model_name: 'Original', api_key: '', model_id: '' },
                { model_name: '王', api_key: 'secret', model_id: 'model-wang' },
            ],
        },
        codex: {
            current_model: 'DeepSeek',
            models: [
                { model_name: 'Original', api_key: '', model_id: '' },
                { model_name: 'DeepSeek', api_key: 'codex-key', model_id: 'deepseek-chat' },
            ],
        },
        ...overrides,
    });
}

describe('useRemotePanel provider sync', () => {
    beforeEach(() => {
        runtimeHandlers.clear();
        vi.clearAllMocks();
        startRemoteSessionMock.mockResolvedValue(undefined);
        startRemoteHandoffSessionMock.mockResolvedValue(undefined);
        loadConfigMock.mockResolvedValue(buildConfig());
        listValidProvidersMock.mockResolvedValue([
            { name: 'Original', model_id: '', is_default: false },
            { name: '王', model_id: 'model-wang', is_default: true },
        ]);
        listRemoteToolMetadataMock.mockResolvedValue([{ name: 'claude', display_name: 'Claude Code' }, { name: 'codex', display_name: 'Codex' }]);
        listRemoteSessionsMock.mockResolvedValue([]);
        getRemoteActivationStatusMock.mockResolvedValue({ activated: true });
        getRemoteConnectionStatusMock.mockResolvedValue({ connected: true });
        getLastRemoteSmokeReportMock.mockResolvedValue({ exists: false, report: null });
        getRemoteToolReadinessMock.mockResolvedValue({});
        getRemoteToolLaunchProbeMock.mockResolvedValue({});
        getRemotePTYProbeMock.mockResolvedValue({});
        runRemoteToolSmokeMock.mockResolvedValue({});
        activateRemoteMock.mockResolvedValue(undefined);
        probeRemoteHubMock.mockResolvedValue({ invitation_code_required: false });
        verifyRemoteActivationMock.mockResolvedValue(true);
        reconnectRemoteHubMock.mockResolvedValue(undefined);
        clearRemoteActivationMock.mockResolvedValue(undefined);
        saveConfigMock.mockResolvedValue(undefined);
        checkToolsStatusMock.mockResolvedValue([]);
        installToolOnDemandMock.mockResolvedValue(undefined);
        sendRemoteSessionInputMock.mockResolvedValue(undefined);
        interruptRemoteSessionMock.mockResolvedValue(undefined);
        killRemoteSessionMock.mockResolvedValue(undefined);
    });

    it('uses tool current_model for quick start instead of stale remote provider state', async () => {
        const { result } = renderHook(() => useRemotePanel({
            config: buildConfig(),
            setConfig: vi.fn(),
            setToolStatuses: vi.fn(),
            getSelectedProjectForRemote: () => '/workspace',
            selectedProjectForLaunch: 'p1',
            navTab: 'settings',
            translate: (key: string) => key,
            formatText: (key: string, values?: Record<string, string>) => values ? `${key}:${JSON.stringify(values)}` : key,
            localizeText: (en: string) => en,
            showToastMessage: vi.fn(),
            onDemandInstallingTool: '',
            setOnDemandInstallingTool: vi.fn(),
        }));

        await waitFor(() => {
            expect(result.current.selectedProvider).toBe('王');
        });

        act(() => {
            void result.current.quickStartRemoteSession('claude');
        });

        await waitFor(() => {
            expect(startRemoteSessionMock).toHaveBeenCalledWith('claude', '/workspace', false, '王', 'desktop');
        });
    });

    it('syncs remote selected provider back to shared tool current_model', async () => {
        const setConfig = vi.fn();
        const config = buildConfig({ active_tool: 'codex' });
        listValidProvidersMock.mockResolvedValue([
            { name: 'Original', model_id: '', is_default: false },
            { name: 'DeepSeek', model_id: 'deepseek-chat', is_default: true },
        ]);

        const { result } = renderHook(() => useRemotePanel({
            config,
            setConfig,
            setToolStatuses: vi.fn(),
            getSelectedProjectForRemote: () => '/workspace',
            selectedProjectForLaunch: 'p1',
            navTab: 'settings',
            translate: (key: string) => key,
            formatText: (key: string, values?: Record<string, string>) => values ? `${key}:${JSON.stringify(values)}` : key,
            localizeText: (en: string) => en,
            showToastMessage: vi.fn(),
            onDemandInstallingTool: '',
            setOnDemandInstallingTool: vi.fn(),
        }));

        await waitFor(() => {
            expect(result.current.selectedRemoteTool).toBe('codex');
            expect(result.current.selectedProvider).toBe('DeepSeek');
        });

        act(() => {
            result.current.setSelectedProvider('Original');
        });

        await waitFor(() => {
            expect(saveConfigMock).toHaveBeenCalled();
            expect(setConfig).toHaveBeenCalled();
        });

        const savedConfig = saveConfigMock.mock.calls.at(-1)?.[0] as main.AppConfig;
        expect(savedConfig.codex.current_model).toBe('Original');
    });

    it('preserves pending default launch mode when saving selected provider', async () => {
        const config = buildConfig({
            active_tool: 'codex',
            default_launch_mode: 'local',
            remote_enabled: false,
        });
        listValidProvidersMock.mockResolvedValue([
            { name: 'Original', model_id: '', is_default: false },
            { name: 'DeepSeek', model_id: 'deepseek-chat', is_default: true },
        ]);

        const { result } = renderHook(() => useRemotePanel({
            config,
            setConfig: vi.fn(),
            getPendingDefaultLaunchMode: () => 'remote',
            setToolStatuses: vi.fn(),
            getSelectedProjectForRemote: () => '/workspace',
            selectedProjectForLaunch: 'p1',
            navTab: 'settings',
            translate: (key: string) => key,
            formatText: (key: string, values?: Record<string, string>) => values ? `${key}:${JSON.stringify(values)}` : key,
            localizeText: (en: string) => en,
            showToastMessage: vi.fn(),
            onDemandInstallingTool: '',
            setOnDemandInstallingTool: vi.fn(),
        }));

        await waitFor(() => {
            expect(result.current.selectedProvider).toBe('DeepSeek');
        });

        act(() => {
            result.current.setSelectedProvider('Original');
        });

        await waitFor(() => {
            expect(saveConfigMock).toHaveBeenCalled();
        });

        const savedConfig = saveConfigMock.mock.calls.at(-1)?.[0] as main.AppConfig;
        expect(savedConfig.codex.current_model).toBe('Original');
        expect(savedConfig.default_launch_mode).toBe('remote');
        expect(savedConfig.remote_enabled).toBe(false);
    });

    it('preserves pending default launch mode when saving another remote field', async () => {
        const setConfig = vi.fn();
        loadConfigMock.mockResolvedValue(buildConfig({
            default_launch_mode: 'local',
            remote_enabled: false,
            remote_email: 'old@example.com',
        }));

        const { result } = renderHook(() => useRemotePanel({
            config: buildConfig({
                default_launch_mode: 'local',
                remote_enabled: false,
                remote_email: 'old@example.com',
            }),
            setConfig,
            getPendingDefaultLaunchMode: () => 'remote',
            setToolStatuses: vi.fn(),
            getSelectedProjectForRemote: () => '/workspace',
            selectedProjectForLaunch: 'p1',
            navTab: 'settings',
            translate: (key: string) => key,
            formatText: (key: string, values?: Record<string, string>) => values ? `${key}:${JSON.stringify(values)}` : key,
            localizeText: (en: string) => en,
            showToastMessage: vi.fn(),
            onDemandInstallingTool: '',
            setOnDemandInstallingTool: vi.fn(),
        }));

        await act(async () => {
            await result.current.saveRemoteConfigField({ remote_email: 'new@example.com' });
        });

        const savedConfig = saveConfigMock.mock.calls.at(-1)?.[0] as main.AppConfig;
        expect(savedConfig.remote_email).toBe('new@example.com');
        expect(savedConfig.default_launch_mode).toBe('remote');
        expect(savedConfig.remote_enabled).toBe(false);
    });

    it('leaves default launch mode untouched when no pending mode exists', async () => {
        loadConfigMock.mockResolvedValue(buildConfig({
            default_launch_mode: 'local',
            remote_enabled: false,
            remote_email: 'old@example.com',
        }));

        const { result } = renderHook(() => useRemotePanel({
            config: buildConfig({
                default_launch_mode: 'local',
                remote_enabled: false,
                remote_email: 'old@example.com',
            }),
            setConfig: vi.fn(),
            getPendingDefaultLaunchMode: () => null,
            setToolStatuses: vi.fn(),
            getSelectedProjectForRemote: () => '/workspace',
            selectedProjectForLaunch: 'p1',
            navTab: 'settings',
            translate: (key: string) => key,
            formatText: (key: string, values?: Record<string, string>) => values ? `${key}:${JSON.stringify(values)}` : key,
            localizeText: (en: string) => en,
            showToastMessage: vi.fn(),
            onDemandInstallingTool: '',
            setOnDemandInstallingTool: vi.fn(),
        }));

        await act(async () => {
            await result.current.saveRemoteConfigField({ remote_email: 'new@example.com' });
        });

        const savedConfig = saveConfigMock.mock.calls.at(-1)?.[0] as main.AppConfig;
        expect(savedConfig.remote_email).toBe('new@example.com');
        expect(savedConfig.default_launch_mode).toBe('local');
        expect(savedConfig.remote_enabled).toBe(false);
    });

    it('does not change default_launch_mode when saving remote_enabled directly', async () => {
        loadConfigMock.mockResolvedValue(buildConfig({
            default_launch_mode: 'local',
            remote_enabled: false,
        }));

        const { result } = renderHook(() => useRemotePanel({
            config: buildConfig({
                default_launch_mode: 'local',
                remote_enabled: false,
            }),
            setConfig: vi.fn(),
            setToolStatuses: vi.fn(),
            getSelectedProjectForRemote: () => '/workspace',
            selectedProjectForLaunch: 'p1',
            navTab: 'settings',
            translate: (key: string) => key,
            formatText: (key: string, values?: Record<string, string>) => values ? `${key}:${JSON.stringify(values)}` : key,
            localizeText: (en: string) => en,
            showToastMessage: vi.fn(),
            onDemandInstallingTool: '',
            setOnDemandInstallingTool: vi.fn(),
        }));

        await act(async () => {
            await result.current.saveRemoteConfigField({ remote_enabled: true });
        });

        const savedConfig = saveConfigMock.mock.calls.at(-1)?.[0] as main.AppConfig;
        expect(savedConfig.remote_enabled).toBe(true);
        expect(savedConfig.default_launch_mode).toBe('local');
    });

    it('preserves explicitly supplied launch mode and remote enabled independently', async () => {
        loadConfigMock.mockResolvedValue(buildConfig({
            default_launch_mode: 'remote',
            remote_enabled: true,
        }));

        const { result } = renderHook(() => useRemotePanel({
            config: buildConfig({
                default_launch_mode: 'remote',
                remote_enabled: true,
            }),
            setConfig: vi.fn(),
            setToolStatuses: vi.fn(),
            getSelectedProjectForRemote: () => '/workspace',
            selectedProjectForLaunch: 'p1',
            navTab: 'settings',
            translate: (key: string) => key,
            formatText: (key: string, values?: Record<string, string>) => values ? `${key}:${JSON.stringify(values)}` : key,
            localizeText: (en: string) => en,
            showToastMessage: vi.fn(),
            onDemandInstallingTool: '',
            setOnDemandInstallingTool: vi.fn(),
        }));

        await act(async () => {
            await result.current.saveRemoteConfigField({ default_launch_mode: 'local', remote_enabled: true });
        });

        const savedConfig = saveConfigMock.mock.calls.at(-1)?.[0] as main.AppConfig;
        expect(savedConfig.default_launch_mode).toBe('local');
        expect(savedConfig.remote_enabled).toBe(true);
    });
});
