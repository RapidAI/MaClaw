import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent, act, cleanup } from '@testing-library/react';
import type { Mock } from 'vitest';

const ActivateRemoteMock = vi.fn();
const GetRemoteActivationStatusMock = vi.fn();
const GetRemoteConnectionStatusMock = vi.fn();
const GetHubLLMServiceStatusMock = vi.fn();
const RedeemHubLLMServiceMock = vi.fn();
const GetMaclawLLMProvidersMock = vi.fn();
const SaveMaclawLLMProvidersMock = vi.fn();
const TestMaclawLLMMock = vi.fn();
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

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetMaclawLLMProviders: (...args: unknown[]) => GetMaclawLLMProvidersMock(...args),
    SaveMaclawLLMProviders: (...args: unknown[]) => SaveMaclawLLMProvidersMock(...args),
    TestMaclawLLM: (...args: unknown[]) => TestMaclawLLMMock(...args),
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
    GetRemoteConnectionStatus: (...args: unknown[]) => GetRemoteConnectionStatusMock(...args),
    GetHubLLMServiceStatus: (...args: unknown[]) => GetHubLLMServiceStatusMock(...args),
    RedeemHubLLMService: (...args: unknown[]) => RedeemHubLLMServiceMock(...args),
    GetWeixinStatus: (...args: unknown[]) => GetWeixinStatusMock(...args),
    StartWeixinQRLogin: (...args: unknown[]) => StartWeixinQRLoginMock(...args),
    PollWeixinQRStatus: (...args: unknown[]) => PollWeixinQRStatusMock(...args),
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
        SaveMaclawLLMProvidersMock.mockResolvedValue(undefined);
        TestMaclawLLMMock.mockResolvedValue({ message: 'ok', supports_vision: false });
        ProbeRemoteHubMock.mockResolvedValue({ invitation_code_required: false });
        GetWeixinStatusMock.mockResolvedValue('');
        GetRemoteConnectionStatusMock.mockResolvedValue({ connected: false });
        GetHubLLMServiceStatusMock.mockResolvedValue({ active: false, skip_llm_config: false });
        RedeemHubLLMServiceMock.mockResolvedValue({ active: false, skip_llm_config: false });
        CancelCodeGenSSOPollingMock.mockResolvedValue(undefined);
    });

    afterEach(() => {
        vi.useRealTimers();
        cleanup();
    });

    it('keeps TigerClaw onboarding to SSO plus WeChat without LLM setup', async () => {
        StartCodeGenSSOEmbeddedMock.mockResolvedValue(undefined);
        WaitCodeGenSSOResultMock.mockResolvedValue({ email: 'tiger@example.com', message: 'SSO OK' });
        FetchCodeGenModelsMock.mockResolvedValue([]);
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true });

        render(<OnboardingWizard {...baseProps} brandId="qianxin" brandDisplayName="TigerClaw" />);

        expect(screen.getByText('1 / 2')).toBeTruthy();
        expect(screen.queryByText('Free trial')).toBeNull();
        expect(screen.queryByPlaceholderText('name@example.com')).toBeNull();
        expect(screen.queryByText(/Pick a provider/)).toBeNull();

        fireEvent.click(screen.getByRole('button', { name: /Enterprise SSO Login/ }));

        await waitFor(() => {
            expect(StartCodeGenSSOEmbeddedMock).toHaveBeenCalledTimes(1);
            expect(WaitCodeGenSSOResultMock).toHaveBeenCalledTimes(1);
            expect(ActivateRemoteMock).toHaveBeenCalledWith('tiger@example.com', '', '');
            expect(baseProps.onLLMConfigured).toHaveBeenCalledTimes(1);
            expect(baseProps.onRegistered).toHaveBeenCalledTimes(1);
        });

        expect(screen.getByText(/Authenticated/)).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Next' }));

        await waitFor(() => {
            expect(screen.getByText(/Scan to bind WeChat/)).toBeTruthy();
            expect(screen.getByText('2 / 2')).toBeTruthy();
        });
        expect(screen.queryByText(/Pick a provider/)).toBeNull();
        expect(GetHubLLMServiceStatusMock).not.toHaveBeenCalled();
    });

    it('auto-saves the first available TigerClaw model after SSO login', async () => {
        StartCodeGenSSOEmbeddedMock.mockResolvedValue(undefined);
        WaitCodeGenSSOResultMock.mockResolvedValue({ email: 'tiger@example.com', message: 'SSO OK', model_id: 'sso-first-model' });
        FetchCodeGenModelsMock.mockResolvedValue([
            { id: 'alphabetically-first-model', name: 'Alphabetically first model' },
            { id: 'second-model', name: 'Second model' },
        ]);
        SaveCodeGenModelChoiceMock.mockResolvedValue(undefined);
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true });

        render(<OnboardingWizard {...baseProps} brandId="qianxin" brandDisplayName="TigerClaw" />);

        fireEvent.click(screen.getByRole('button', { name: /Enterprise SSO Login/ }));

        await waitFor(() => {
            expect(SaveCodeGenModelChoiceMock).toHaveBeenCalledWith('sso-first-model', 'sso-first-model');
        });
    });

    it('lets TigerClaw browser SSO fallback continue to WeChat without LLM setup', async () => {
        StartCodeGenSSOEmbeddedMock.mockRejectedValue(new Error('embedded unavailable'));
        StartCodeGenSSOMock.mockResolvedValue({ message: 'Browser SSO OK' });

        render(<OnboardingWizard {...baseProps} brandId="qianxin" brandDisplayName="TigerClaw" />);

        fireEvent.click(screen.getByRole('button', { name: /Enterprise SSO Login/ }));

        await waitFor(() => {
            expect(screen.getByRole('button', { name: /Open in Browser/ })).toBeTruthy();
        });

        fireEvent.click(screen.getByRole('button', { name: /Open in Browser/ }));

        await waitFor(() => {
            expect(StartCodeGenSSOMock).toHaveBeenCalledTimes(1);
            expect(baseProps.onLLMConfigured).toHaveBeenCalledTimes(1);
        });

        expect(baseProps.onRegistered).not.toHaveBeenCalled();
        fireEvent.click(screen.getByRole('button', { name: 'Next' }));

        await waitFor(() => {
            expect(screen.getByText(/Scan to bind WeChat/)).toBeTruthy();
            expect(screen.getByText('2 / 2')).toBeTruthy();
        });
        expect(screen.queryByText(/Pick a provider/)).toBeNull();
    });

    it('renders optional service redeem code text in Chinese registration step', () => {
        render(<OnboardingWizard {...baseProps} lang="zh-Hans" />);

        expect(screen.getByText('服务兑换码')).toBeTruthy();
        expect(screen.getByPlaceholderText('请输入服务兑换码（可选）')).toBeTruthy();
    });

    it('redeems optional service code after registration when provided', async () => {
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true });
        RedeemHubLLMServiceMock.mockResolvedValue({ active: true, skip_llm_config: true });

        render(<OnboardingWizard {...baseProps} />);

        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'user@example.com' } });
        fireEvent.change(screen.getByPlaceholderText('Enter service redeem code (optional)'), { target: { value: ' card123 ' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        fireEvent.click(screen.getByRole('button', { name: 'Confirm & Register' }));

        await waitFor(() => {
            expect(RedeemHubLLMServiceMock).toHaveBeenCalledWith('CARD123');
        });
        expect(baseProps.onLLMConfigured).toHaveBeenCalledTimes(1);
        expect((screen.getByPlaceholderText('Enter service redeem code (optional)') as HTMLInputElement).value).toBe('');
    });

    it('skips LLM step after successful redeem even when skip_llm_config is false', async () => {
        // Backend may return skip_llm_config: false due to provider registry
        // filtering, but the LLM provider is already configured in config.json
        // by applyHubLLMServiceStatusToConfig. Step 3 should still be skipped.
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true });
        RedeemHubLLMServiceMock.mockResolvedValue({ active: true, skip_llm_config: false });

        render(<OnboardingWizard {...baseProps} />);

        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'user@example.com' } });
        fireEvent.change(screen.getByPlaceholderText('Enter service redeem code (optional)'), { target: { value: 'MYCODE' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        fireEvent.click(screen.getByRole('button', { name: 'Confirm & Register' }));

        await waitFor(() => {
            expect(RedeemHubLLMServiceMock).toHaveBeenCalledWith('MYCODE');
        });
        // onLLMConfigured should be called even when skip_llm_config is false
        expect(baseProps.onLLMConfigured).toHaveBeenCalledTimes(1);

        // Navigate to step 2 (UI Mode)
        fireEvent.click(screen.getByRole('button', { name: 'Next' }));
        // Step 2 auto-completes, click Next again — should skip step 3 and go to step 4
        // Should be on step 4 (WeChat), not step 3 (LLM)
        await waitFor(() => {
            expect(screen.getByText(/Scan to bind WeChat/)).toBeTruthy();
        });
    });

    it('does not skip LLM step when redeemed official service is queued', async () => {
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true });
        RedeemHubLLMServiceMock.mockResolvedValue({
            active: false,
            skip_llm_config: false,
            credit_grants: [{
                status: 'queued',
                retry_after_seconds: 7200,
            }],
        });

        render(<OnboardingWizard {...baseProps} />);

        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'user@example.com' } });
        fireEvent.change(screen.getByPlaceholderText('Enter service redeem code (optional)'), { target: { value: 'WAITCODE' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        fireEvent.click(screen.getByRole('button', { name: 'Confirm & Register' }));

        await waitFor(() => {
            expect(RedeemHubLLMServiceMock).toHaveBeenCalledWith('WAITCODE');
        });
        expect(baseProps.onLLMConfigured).not.toHaveBeenCalled();
        expect(await screen.findByText(/authorization starts in about 2h/i)).toBeTruthy();

        fireEvent.click(screen.getByRole('button', { name: 'Next' }));
        expect(await screen.findByText(/Pick a provider/i)).toBeTruthy();
    });

    it('prioritizes queued redeem status over exhausted older grants', async () => {
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true });
        RedeemHubLLMServiceMock.mockResolvedValue({
            active: false,
            skip_llm_config: false,
            credit_grants: [
                { status: 'exhausted' },
                { status: 'queued', retry_after_seconds: 7200 },
            ],
        });

        render(<OnboardingWizard {...baseProps} />);

        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'user@example.com' } });
        fireEvent.change(screen.getByPlaceholderText('Enter service redeem code (optional)'), { target: { value: 'WAITCODE' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        fireEvent.click(screen.getByRole('button', { name: 'Confirm & Register' }));

        expect(await screen.findByText(/authorization starts in about 2h/i)).toBeTruthy();
        expect(screen.queryByText(/credits are exhausted/i)).toBeNull();
        expect(baseProps.onLLMConfigured).not.toHaveBeenCalled();
    });

    it('does not show a zero-second retry when inactive redeem has no retry metadata', async () => {
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true });
        RedeemHubLLMServiceMock.mockResolvedValue({
            active: false,
            skip_llm_config: false,
            credit_grants: [{ status: 'period_limited' }],
        });

        render(<OnboardingWizard {...baseProps} />);

        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'user@example.com' } });
        fireEvent.change(screen.getByPlaceholderText('Enter service redeem code (optional)'), { target: { value: 'LIMITED' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        fireEvent.click(screen.getByRole('button', { name: 'Confirm & Register' }));

        expect(await screen.findByText(/period limited\. LLM setup is not skipped yet/i)).toBeTruthy();
        expect(screen.queryByText(/0s|0 seconds/i)).toBeNull();
        expect(baseProps.onLLMConfigured).not.toHaveBeenCalled();
    });

    it('marks registration done after activation succeeds', async () => {
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true });
        GetRemoteActivationStatusMock.mockResolvedValue({ activated: true });

        render(<OnboardingWizard {...baseProps} />);

        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'user@example.com' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        fireEvent.click(screen.getByRole('button', { name: 'Confirm & Register' }));

        await waitFor(() => {
            expect(screen.getByText(/Registration successful/)).toBeTruthy();
        });
        expect(screen.getByText(/Connecting to Hub in the background/)).toBeTruthy();
        expect(screen.getByText('Hub connecting')).toBeTruthy();
        expect(baseProps.onSaveField).toHaveBeenCalledWith({ remote_email: 'user@example.com' });
        expect(baseProps.onRegistered).toHaveBeenCalledTimes(1);
        expect(RedeemHubLLMServiceMock).not.toHaveBeenCalled();
        expect(GetRemoteActivationStatusMock).not.toHaveBeenCalled();
    });

    it('defaults to free trial and skips the LLM configuration step', async () => {
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true });
        // Simulate Hub connecting and LLM service being provisioned.
        GetRemoteConnectionStatusMock.mockResolvedValue({ connected: true });
        GetHubLLMServiceStatusMock.mockResolvedValue({ active: true, skip_llm_config: true });

        render(<OnboardingWizard {...baseProps} />);

        expect(screen.getByText('Free trial')).toBeTruthy();
        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'user@example.com' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        expect(screen.getByText(/remaining bonus credits/)).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Confirm & Register' }));

        await waitFor(() => {
            expect(screen.getByText(/Registration successful/)).toBeTruthy();
        });

        // Wait for Hub connection polling to verify the free trial service.
        await waitFor(() => {
            expect(baseProps.onLLMConfigured).toHaveBeenCalledTimes(1);
        });

        fireEvent.click(screen.getByRole('button', { name: 'Next' }));

        await waitFor(() => {
            expect(screen.getByText(/Scan to bind WeChat/)).toBeTruthy();
        });
    });

    it('shows the LLM configuration step when free trial is unchecked', async () => {
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true });
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                { name: 'Custom1', url: '', key: '', model: '', protocol: 'openai', is_custom: true, supports_vision: false },
            ],
        });

        render(<OnboardingWizard {...baseProps} />);

        fireEvent.click(screen.getByLabelText(/Free trial/));
        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'user@example.com' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        fireEvent.click(screen.getByRole('button', { name: 'Confirm & Register' }));

        await waitFor(() => {
            expect(screen.getByText(/Registration successful/)).toBeTruthy();
        });
        fireEvent.click(screen.getByRole('button', { name: 'Next' }));

        expect(await screen.findByText(/Pick a provider/)).toBeTruthy();
    });

    it('skips Hub registration and finishes after LLM setup in offline mode', async () => {
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                { name: 'Custom1', url: '', key: '', model: '', protocol: 'openai', is_custom: true, supports_vision: false },
            ],
        });
        TestMaclawLLMMock.mockResolvedValue({ message: 'hello', supports_vision: false });

        render(<OnboardingWizard {...baseProps} />);

        expect(screen.getByLabelText(/Online mode/)).toBeTruthy();
        fireEvent.click(screen.getByLabelText(/Offline mode/));
        expect(screen.getByText(/Hub registration will be skipped/i)).toBeTruthy();
        expect(screen.getAllByText(/cannot access the public internet/i).length).toBeGreaterThan(0);
        expect(screen.getAllByText(/web search/i).length).toBeGreaterThan(0);

        fireEvent.click(screen.getByRole('button', { name: 'Next' }));
        expect(await screen.findByText(/Pick a provider/)).toBeTruthy();
        expect(screen.getByText('2 / 2')).toBeTruthy();
        expect(screen.getByText('Mode')).toBeTruthy();
        expect(screen.queryByText(/Scan to bind WeChat/)).toBeNull();
        expect(ActivateRemoteMock).not.toHaveBeenCalled();

        fireEvent.click(await screen.findByRole('button', { name: 'Custom1' }));
        fireEvent.change(await screen.findByPlaceholderText('https://api.openai.com/v1'), { target: { value: 'https://api.example.com/v1' } });
        fireEvent.change(screen.getByPlaceholderText('gpt-4o'), { target: { value: 'gpt-test' } });
        fireEvent.change(screen.getByPlaceholderText('sk-...'), { target: { value: 'secret' } });
        fireEvent.click(screen.getByRole('button', { name: 'Test & Save' }));

        await waitFor(() => {
            expect(TestMaclawLLMMock).toHaveBeenCalledWith({
                url: 'https://api.example.com/v1',
                key: 'secret',
                model: 'gpt-test',
                protocol: 'openai',
                agent_type: 'openclaw',
                wire_api: '',
            });
        });
        await waitFor(() => {
            expect(SaveMaclawLLMProvidersMock).toHaveBeenCalledWith(
                [expect.objectContaining({ name: 'Custom1', url: 'https://api.example.com/v1', key: 'secret', model: 'gpt-test' })],
                'Custom1',
            );
        });
        expect(baseProps.onRegistered).not.toHaveBeenCalled();
        expect(baseProps.onLLMConfigured).toHaveBeenCalledTimes(1);
        expect(baseProps.onSaveField).toHaveBeenCalledWith({ onboarding_done: true });
    });

    it('returns to the normal registration UI when offline mode is turned back off', () => {
        render(<OnboardingWizard {...baseProps} />);

        fireEvent.click(screen.getByLabelText(/Offline mode/));
        expect(screen.queryByPlaceholderText('name@example.com')).toBeNull();
        expect(screen.getAllByText(/cannot access the public internet/i).length).toBeGreaterThan(0);

        fireEvent.click(screen.getByLabelText(/Online mode/));
        expect(screen.getByPlaceholderText('name@example.com')).toBeTruthy();
        expect(screen.getByText('Free trial')).toBeTruthy();
        expect(screen.queryAllByText(/cannot access the public internet/i)).toHaveLength(0);
    });

    it('ignores stale Hub LLM status responses after switching to offline mode', async () => {
        let resolveHubStatus: ((value: { active: boolean; skip_llm_config: boolean }) => void) | null = null;
        GetHubLLMServiceStatusMock.mockImplementation(() => new Promise(resolve => {
            resolveHubStatus = resolve;
        }));

        render(<OnboardingWizard {...baseProps} />);
        fireEvent.click(screen.getByLabelText(/Offline mode/));

        await act(async () => {
            resolveHubStatus?.({ active: true, skip_llm_config: true });
            await Promise.resolve();
        });

        expect(baseProps.onLLMConfigured).not.toHaveBeenCalled();
        fireEvent.click(screen.getByRole('button', { name: 'Next' }));
        expect(await screen.findByText(/Pick a provider/)).toBeTruthy();
        expect((screen.getByRole('button', { name: 'Finish' }) as HTMLButtonElement).disabled).toBe(true);
    });

    it('does not impose a timeout warning on free trial verification', async () => {
        ActivateRemoteMock.mockResolvedValue({ vip_flag: false });
        GetRemoteConnectionStatusMock.mockResolvedValue({ connected: true });
        GetHubLLMServiceStatusMock.mockResolvedValue({ active: false, skip_llm_config: false });

        render(<OnboardingWizard {...baseProps} />);

        expect(screen.getByText('Free trial')).toBeTruthy();
        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'user@example.com' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        fireEvent.click(await screen.findByRole('button', { name: 'Confirm & Register' }));

        await waitFor(() => {
            expect(screen.getByText(/Registration successful/)).toBeTruthy();
        });

        expect(screen.queryByText(/not ready/i)).toBeNull();
        expect(screen.getByText(/Verifying free trial service/)).toBeTruthy();
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
        expect(screen.getByRole('button', { name: /Registered .*Hub connecting/ })).toBeTruthy();

        await act(async () => {
            releaseParentRefresh?.();
        });

        await waitFor(() => {
            expect(screen.getByText(/Registration successful/)).toBeTruthy();
        });
    });

    it('does not block success UI on slow backend hub connect', async () => {
        let resolveActivation: ((value: { vip_flag: boolean }) => void) | null = null;
        ActivateRemoteMock.mockImplementation(() => new Promise((resolve) => {
            resolveActivation = resolve;
        }));
        GetRemoteConnectionStatusMock.mockResolvedValue({ connected: false });
        GetHubLLMServiceStatusMock.mockResolvedValue({ active: false, skip_llm_config: false });
        RedeemHubLLMServiceMock.mockResolvedValue({ active: false, skip_llm_config: false });

        render(<OnboardingWizard {...baseProps} />);

        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'user@example.com' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        fireEvent.click(screen.getByRole('button', { name: 'Confirm & Register' }));

        expect(screen.getByRole('button', { name: 'Registering...' })).toBeTruthy();

        await act(async () => {
            resolveActivation?.({ vip_flag: true });
        });

        await waitFor(() => {
            expect(screen.getByText(/Registration successful/)).toBeTruthy();
        });
        expect(screen.getByRole('button', { name: /Registered .*Hub connecting/ })).toBeTruthy();
        expect(screen.getByText('Hub connecting')).toBeTruthy();
    });

    it('switches button state after hub connection succeeds', async () => {
        vi.useFakeTimers();
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true });
        GetRemoteConnectionStatusMock
            .mockResolvedValueOnce({ connected: false })
            .mockResolvedValueOnce({ connected: true });

        render(<OnboardingWizard {...baseProps} />);

        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'user@example.com' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        fireEvent.click(screen.getByRole('button', { name: 'Confirm & Register' }));

        await act(async () => {
            await Promise.resolve();
        });

        expect(screen.getByRole('button', { name: /Registered .*Hub connecting/ })).toBeTruthy();
        expect(screen.getByText(/Connecting to Hub in the background/)).toBeTruthy();
        expect(screen.getByText('Hub connecting')).toBeTruthy();

        await act(async () => {
            await vi.advanceTimersByTimeAsync(1600);
        });

        expect(screen.getByRole('button', { name: /Registered/ })).toBeTruthy();
        expect(screen.getAllByText(/Hub connected/).length).toBeGreaterThan(0);
        expect(screen.getByText('Hub connected')).toBeTruthy();
    });

    it('shows backend registration error and clears busy state', async () => {
        ActivateRemoteMock.mockRejectedValue(new Error('boom'));

        render(<OnboardingWizard {...baseProps} />);

        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'user@example.com' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        fireEvent.click(screen.getByRole('button', { name: 'Confirm & Register' }));

        await waitFor(() => {
            expect(screen.getByText(/boom/)).toBeTruthy();
        });
        expect(screen.getByRole('button', { name: 'Register' })).toBeTruthy();
        expect(baseProps.onRegistered).not.toHaveBeenCalled();
    });

    it('tests first, then saves providers with final supports_vision in step 3', async () => {
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true });
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                { name: 'Custom1', url: '', key: '', model: '', protocol: 'openai', is_custom: true, supports_vision: false },
            ],
        });
        TestMaclawLLMMock.mockResolvedValue({ message: 'hello', supports_vision: true });

        render(<OnboardingWizard {...baseProps} />);

        fireEvent.click(screen.getByLabelText(/Free trial/));
        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'user@example.com' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        fireEvent.click(screen.getByRole('button', { name: 'Confirm & Register' }));

        await waitFor(() => {
            expect(screen.getByText(/Registration successful/)).toBeTruthy();
        });
        fireEvent.click(screen.getByRole('button', { name: 'Next' }));

        fireEvent.click(await screen.findByRole('button', { name: 'Custom1' }));
        fireEvent.change(await screen.findByPlaceholderText('https://api.openai.com/v1'), { target: { value: 'https://api.example.com/v1' } });
        fireEvent.change(screen.getByPlaceholderText('gpt-4o'), { target: { value: 'gpt-test' } });
        fireEvent.change(screen.getByPlaceholderText('sk-...'), { target: { value: 'secret' } });
        fireEvent.click(screen.getByRole('button', { name: 'Test & Save' }));

        await waitFor(() => {
            expect(TestMaclawLLMMock).toHaveBeenCalledWith({
                url: 'https://api.example.com/v1',
                key: 'secret',
                model: 'gpt-test',
                protocol: 'openai',
                agent_type: 'openclaw',
                wire_api: '',
            });
        });

        await waitFor(() => {
            expect(SaveMaclawLLMProvidersMock).toHaveBeenCalledWith(
                [expect.objectContaining({ name: 'Custom1', supports_vision: true, url: 'https://api.example.com/v1', key: 'secret', model: 'gpt-test' })],
                'Custom1',
            );
        });

        expect(TestMaclawLLMMock.mock.invocationCallOrder[0]).toBeLessThan(SaveMaclawLLMProvidersMock.mock.invocationCallOrder[0]);
        expect(baseProps.onLLMConfigured).toHaveBeenCalledTimes(1);
        expect(screen.getByText(/Scan to bind WeChat/)).toBeTruthy();
    });

    it('does not save when llm detection fails in step 3', async () => {
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true });
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                { name: 'Custom1', url: '', key: '', model: '', protocol: 'openai', is_custom: true, supports_vision: false },
            ],
        });
        TestMaclawLLMMock.mockRejectedValue(new Error('boom'));

        render(<OnboardingWizard {...baseProps} />);

        fireEvent.click(screen.getByLabelText(/Free trial/));
        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'user@example.com' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        fireEvent.click(screen.getByRole('button', { name: 'Confirm & Register' }));

        await waitFor(() => {
            expect(screen.getByText(/Registration successful/)).toBeTruthy();
        });
        fireEvent.click(screen.getByRole('button', { name: 'Next' }));

        fireEvent.click(await screen.findByRole('button', { name: 'Custom1' }));
        fireEvent.change(await screen.findByPlaceholderText('https://api.openai.com/v1'), { target: { value: 'https://api.example.com/v1' } });
        fireEvent.change(screen.getByPlaceholderText('gpt-4o'), { target: { value: 'gpt-test' } });
        fireEvent.change(screen.getByPlaceholderText('sk-...'), { target: { value: 'secret' } });
        fireEvent.click(screen.getByRole('button', { name: 'Test & Save' }));

        await waitFor(() => {
            expect(TestMaclawLLMMock).toHaveBeenCalled();
        });

        expect(SaveMaclawLLMProvidersMock).not.toHaveBeenCalled();
        expect(await screen.findByText(/boom/)).toBeTruthy();
        expect(baseProps.onLLMConfigured).not.toHaveBeenCalled();
    });
});
