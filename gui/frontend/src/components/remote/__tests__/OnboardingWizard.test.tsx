import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react';
import type { Mock } from 'vitest';

const ActivateRemoteMock = vi.fn();
const GetRemoteActivationStatusMock = vi.fn();
const GetMaclawLLMProvidersMock = vi.fn();
const ProbeRemoteHubMock = vi.fn();
const StartOpenAIOAuthMock = vi.fn();
const StartCodeGenSSOMock = vi.fn();
const StartCodeGenSSOEmbeddedMock = vi.fn();
const WaitCodeGenSSOResultMock = vi.fn();
const CancelCodeGenSSOPollingMock = vi.fn();
const FetchCodeGenModelsMock = vi.fn();
const SaveCodeGenModelChoiceMock = vi.fn();
const GetWeixinStatusMock = vi.fn();
const StartWeixinQRLoginMock = vi.fn();
const PollWeixinQRStatusMock = vi.fn();
const StartFreeProxyMock = vi.fn();
const StopFreeProxyMock = vi.fn();
const IsFreeProxyRunningMock = vi.fn();
const DetectBrowserMock = vi.fn();
const DangbeiLoginMock = vi.fn();
const DangbeiFinishLoginMock = vi.fn();
const DangbeiEnsureAuthMock = vi.fn();
const GetFreeProxyModelsMock = vi.fn();
const GetFreeProxyModelMock = vi.fn();
const SetFreeProxyModelMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetMaclawLLMProviders: (...args: unknown[]) => GetMaclawLLMProvidersMock(...args),
    SaveMaclawLLMProviders: vi.fn(),
            TestMaclawLLM: vi.fn(),
    ActivateRemote: (...args: unknown[]) => ActivateRemoteMock(...args),
    ProbeRemoteHub: (...args: unknown[]) => ProbeRemoteHubMock(...args),
    StartOpenAIOAuth: (...args: unknown[]) => StartOpenAIOAuthMock(...args),
    StartCodeGenSSO: (...args: unknown[]) => StartCodeGenSSOMock(...args),
    StartCodeGenSSOEmbedded: (...args: unknown[]) => StartCodeGenSSOEmbeddedMock(...args),
    WaitCodeGenSSOResult: (...args: unknown[]) => WaitCodeGenSSOResultMock(...args),
    CancelCodeGenSSOPolling: (...args: unknown[]) => CancelCodeGenSSOPollingMock(...args),
    FetchCodeGenModels: (...args: unknown[]) => FetchCodeGenModelsMock(...args),
    SaveCodeGenModelChoice: (...args: unknown[]) => SaveCodeGenModelChoiceMock(...args),
    GetRemoteActivationStatus: (...args: unknown[]) => GetRemoteActivationStatusMock(...args),
    GetWeixinStatus: (...args: unknown[]) => GetWeixinStatusMock(...args),
    StartWeixinQRLogin: (...args: unknown[]) => StartWeixinQRLoginMock(...args),
    PollWeixinQRStatus: (...args: unknown[]) => PollWeixinQRStatusMock(...args),
    StartFreeProxy: (...args: unknown[]) => StartFreeProxyMock(...args),
    StopFreeProxy: (...args: unknown[]) => StopFreeProxyMock(...args),
    IsFreeProxyRunning: (...args: unknown[]) => IsFreeProxyRunningMock(...args),
    DetectBrowser: (...args: unknown[]) => DetectBrowserMock(...args),
    DangbeiLogin: (...args: unknown[]) => DangbeiLoginMock(...args),
    DangbeiFinishLogin: (...args: unknown[]) => DangbeiFinishLoginMock(...args),
    DangbeiEnsureAuth: (...args: unknown[]) => DangbeiEnsureAuthMock(...args),
    GetFreeProxyModels: (...args: unknown[]) => GetFreeProxyModelsMock(...args),
    GetFreeProxyModel: (...args: unknown[]) => GetFreeProxyModelMock(...args),
    SetFreeProxyModel: (...args: unknown[]) => SetFreeProxyModelMock(...args),
}));

import { OnboardingWizard } from '../OnboardingWizard';

describe('OnboardingWizard registration', () => {
    let baseProps: {
        lang: string;
        hubUrl: string;
        email: string;
        uiMode: string;
        onClose: Mock;
        onLLMConfigured: Mock;
        onRegistered: Mock;
        onSaveField: Mock;
    };

    beforeEach(() => {
        vi.clearAllMocks();
        baseProps = {
            lang: 'en',
            hubUrl: 'http://hub.example.com',
            email: '',
            uiMode: '',
            onClose: vi.fn(),
            onLLMConfigured: vi.fn(),
            onRegistered: vi.fn(),
            onSaveField: vi.fn(),
        };
        GetMaclawLLMProvidersMock.mockResolvedValue({ providers: [] });
        ProbeRemoteHubMock.mockResolvedValue({ invitation_code_required: false });
        GetWeixinStatusMock.mockResolvedValue('');
        CancelCodeGenSSOPollingMock.mockResolvedValue(undefined);
        DetectBrowserMock.mockResolvedValue({ found: 'false' });
        GetFreeProxyModelsMock.mockResolvedValue([]);
        GetFreeProxyModelMock.mockResolvedValue('');
        DangbeiEnsureAuthMock.mockResolvedValue('');
        IsFreeProxyRunningMock.mockResolvedValue(false);
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it('marks registration done after activation status refresh succeeds', async () => {
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true });
        GetRemoteActivationStatusMock.mockResolvedValue({ activated: true });

        render(<OnboardingWizard {...baseProps} />);

        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'user@example.com' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        fireEvent.click(screen.getByRole('button', { name: 'Confirm & Register' }));

        await waitFor(() => {
            expect(screen.getByText(/Registration successful/)).toBeTruthy();
        });
        expect(baseProps.onSaveField).toHaveBeenCalledWith({ remote_email: 'user@example.com' });
        expect(baseProps.onRegistered).toHaveBeenCalledTimes(1);
        expect(GetRemoteActivationStatusMock).toHaveBeenCalledTimes(1);
    });

    it('accepts backend success when returned machine credentials exist', async () => {
        ActivateRemoteMock.mockResolvedValue({ machine_id: 'mid-1', machine_token: 'tok-1' });
        GetRemoteActivationStatusMock.mockResolvedValue({ activated: false });

        render(<OnboardingWizard {...baseProps} />);

        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'user@example.com' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        fireEvent.click(screen.getByRole('button', { name: 'Confirm & Register' }));

        await waitFor(() => {
            expect(screen.getByText(/Registration successful/)).toBeTruthy();
        });
        expect(baseProps.onRegistered).toHaveBeenCalledTimes(1);
    });

    it('does not block success UI on parent registration refresh', async () => {
        let releaseParentRefresh: (() => void) | null = null;
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true });
        GetRemoteActivationStatusMock.mockResolvedValue({ activated: true });
        baseProps.onRegistered.mockImplementation(() => new Promise<void>((resolve) => {
            releaseParentRefresh = resolve;
        }));

        render(<OnboardingWizard {...baseProps} />);

        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'user@example.com' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        fireEvent.click(screen.getByRole('button', { name: 'Confirm & Register' }));

        await waitFor(() => {
            expect(baseProps.onRegistered).toHaveBeenCalledTimes(1);
        });
        expect(screen.getByText(/Registration successful/)).toBeTruthy();
        expect(screen.getByRole('button', { name: '✅ Registered' })).toBeTruthy();

        await act(async () => {
            releaseParentRefresh?.();
        });

        await waitFor(() => {
            expect(screen.getByText(/Registration successful/)).toBeTruthy();
        });
    });

    it('clears busy state and shows timeout when activation never resolves', async () => {
        vi.useFakeTimers();
        ActivateRemoteMock.mockImplementation(() => new Promise(() => {}));

        render(<OnboardingWizard {...baseProps} />);

        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'user@example.com' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        fireEvent.click(screen.getByRole('button', { name: 'Confirm & Register' }));

        expect(screen.getByRole('button', { name: 'Registering...' })).toBeTruthy();

        await act(async () => {
            await vi.advanceTimersByTimeAsync(35_000);
        });

        expect(screen.getByText(/Registration timed out\. Please retry\./)).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Register' })).toBeTruthy();
        expect(baseProps.onRegistered).not.toHaveBeenCalled();
    });
});
