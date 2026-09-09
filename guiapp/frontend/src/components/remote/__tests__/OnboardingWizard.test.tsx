import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent, act, cleanup } from '@testing-library/react';
import { useState } from 'react';
import type { Mock } from 'vitest';

const ActivateRemoteMock = vi.fn();
const ActivateRemoteEmailMock = vi.fn();
const ActivateRemoteSMSMock = vi.fn();
const GetRemoteRegistrationAuthMock = vi.fn();
const ResolveRemoteRegistrationTargetMock = vi.fn();
const ResolveRemoteRegistrationTargetWithInvitationMock = vi.fn();
const SendRemoteRegistrationSMSMock = vi.fn();
const SendRemoteRegistrationEmailMock = vi.fn();
const GetRemoteActivationStatusMock = vi.fn();
const GetRemoteConnectionStatusMock = vi.fn();
const GetHubLLMServiceStatusMock = vi.fn();
const RedeemHubLLMServiceMock = vi.fn();
const GetMaclawLLMProvidersMock = vi.fn();
const SaveMaclawLLMProvidersMock = vi.fn();
const TestMaclawLLMMock = vi.fn();
const TestAndSaveMaclawLLMProvidersMock = vi.fn();
const ProbeRemoteHubMock = vi.fn();
const StartOpenAIOAuthMock = vi.fn();
const StartXAIOAuthMock = vi.fn();
const CancelXAIOAuthMock = vi.fn();
const StartCodeGenSSOMock = vi.fn();
const StartCodeGenSSOEmbeddedMock = vi.fn();
const WaitCodeGenSSOResultMock = vi.fn();
const CancelCodeGenSSOPollingMock = vi.fn();
const FetchCodeGenModelsMock = vi.fn();
const SaveCodeGenModelChoiceMock = vi.fn();
const GetWeixinStatusMock = vi.fn();
const StartWeixinQRLoginMock = vi.fn();
const PollWeixinQRStatusMock = vi.fn();
const UserDataMigrationInstancesMock = vi.fn();
const UserDataMigrationStatusMock = vi.fn();
const StartUserDataMigrationImportMock = vi.fn();
const GetUserDataMigrationJobMock = vi.fn();
const ClaimReferralHandoffMock = vi.fn();
const GetReferralRegistrationStatusMock = vi.fn();
const RegisterReferralEmailMock = vi.fn();
const RegisterReferralPhoneMock = vi.fn();
const ActivateReferralRemoteEmailMock = vi.fn();
const ActivateReferralRemotePhoneMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetMaclawLLMProviders: (...args: unknown[]) => GetMaclawLLMProvidersMock(...args),
    SaveMaclawLLMProviders: (...args: unknown[]) => SaveMaclawLLMProvidersMock(...args),
    TestMaclawLLM: (...args: unknown[]) => TestMaclawLLMMock(...args),
    TestAndSaveMaclawLLMProviders: (...args: unknown[]) => TestAndSaveMaclawLLMProvidersMock(...args),
    ActivateRemote: (...args: unknown[]) => ActivateRemoteMock(...args),
    ActivateRemoteEmail: (...args: unknown[]) => ActivateRemoteEmailMock(...args),
    ActivateRemoteSMS: (...args: unknown[]) => ActivateRemoteSMSMock(...args),
    GetRemoteRegistrationAuth: (...args: unknown[]) => GetRemoteRegistrationAuthMock(...args),
    ResolveRemoteRegistrationTarget: (...args: unknown[]) => ResolveRemoteRegistrationTargetMock(...args),
	ResolveRemoteRegistrationTargetWithInvitation: (...args: unknown[]) => ResolveRemoteRegistrationTargetWithInvitationMock(...args),
    SendRemoteRegistrationSMS: (...args: unknown[]) => SendRemoteRegistrationSMSMock(...args),
    SendRemoteRegistrationEmail: (...args: unknown[]) => SendRemoteRegistrationEmailMock(...args),
    ProbeRemoteHub: (...args: unknown[]) => ProbeRemoteHubMock(...args),
    StartOpenAIOAuth: (...args: unknown[]) => StartOpenAIOAuthMock(...args),
    StartXAIOAuth: (...args: unknown[]) => StartXAIOAuthMock(...args),
    CancelOpenAIOAuth: vi.fn(),
    CancelXAIOAuth: (...args: unknown[]) => CancelXAIOAuthMock(...args),
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
    UserDataMigrationInstances: (...args: unknown[]) => UserDataMigrationInstancesMock(...args),
    UserDataMigrationStatus: (...args: unknown[]) => UserDataMigrationStatusMock(...args),
    StartUserDataMigrationImport: (...args: unknown[]) => StartUserDataMigrationImportMock(...args),
    GetUserDataMigrationJob: (...args: unknown[]) => GetUserDataMigrationJobMock(...args),
    ClaimReferralHandoff: (...args: unknown[]) => ClaimReferralHandoffMock(...args),
    GetReferralRegistrationStatus: (...args: unknown[]) => GetReferralRegistrationStatusMock(...args),
    RegisterReferralEmail: (...args: unknown[]) => RegisterReferralEmailMock(...args),
    RegisterReferralPhone: (...args: unknown[]) => RegisterReferralPhoneMock(...args),
    ActivateReferralRemoteEmail: (...args: unknown[]) => ActivateReferralRemoteEmailMock(...args),
    ActivateReferralRemotePhone: (...args: unknown[]) => ActivateReferralRemotePhoneMock(...args),
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
        GetRemoteRegistrationAuthMock.mockResolvedValue({ method: 'email', code_length: 6 });
        ResolveRemoteRegistrationTargetMock.mockImplementation(async (identity: string) => ({
            identity,
            hub_url: 'http://hub.example.com',
            method: 'email',
            code_length: 6,
        }));
		ResolveRemoteRegistrationTargetWithInvitationMock.mockImplementation(async (identity: string) => ({
			identity,
			hub_url: 'http://hub.example.com',
			method: 'email',
			code_length: 6,
		}));
        SendRemoteRegistrationSMSMock.mockResolvedValue({ ok: true, code_length: 6, expires_min: 5 });
        SendRemoteRegistrationEmailMock.mockResolvedValue({ ok: true, code_length: 6, resend_cooldown_seconds: 60 });
        ActivateRemoteEmailMock.mockResolvedValue({ email: 'user@example.com', vip_flag: false });
        ActivateRemoteEmailMock.mockImplementation((_hubURL, userEmail, _code, invitationCode) => ActivateRemoteMock(userEmail, invitationCode, ''));
        ActivateRemoteSMSMock.mockResolvedValue({ email: 'phone:13800138000', vip_flag: false });
        SaveMaclawLLMProvidersMock.mockResolvedValue(undefined);
        TestAndSaveMaclawLLMProvidersMock.mockResolvedValue({ message: 'ok', supports_vision: false });
        TestMaclawLLMMock.mockResolvedValue({ message: 'ok', supports_vision: false });
        ProbeRemoteHubMock.mockResolvedValue({ invitation_code_required: false });
        GetWeixinStatusMock.mockResolvedValue('');
        UserDataMigrationInstancesMock.mockResolvedValue([]);
        UserDataMigrationStatusMock.mockResolvedValue(null);
        StartUserDataMigrationImportMock.mockResolvedValue(null);
        GetUserDataMigrationJobMock.mockResolvedValue(null);
        GetRemoteConnectionStatusMock.mockResolvedValue({ connected: false });
        GetHubLLMServiceStatusMock.mockResolvedValue({ active: false, skip_llm_config: false });
        RedeemHubLLMServiceMock.mockResolvedValue({ active: false, skip_llm_config: false });
        CancelCodeGenSSOPollingMock.mockResolvedValue(undefined);
        ClaimReferralHandoffMock.mockResolvedValue({
            registration_session: 'referral-session',
            tenant: { id: 'tenant-referral' },
            registration_method: 'email',
        });
        GetReferralRegistrationStatusMock.mockResolvedValue({ registration_status: 'continue' });
        RegisterReferralEmailMock.mockResolvedValue(undefined);
        RegisterReferralPhoneMock.mockResolvedValue(undefined);
        ActivateReferralRemoteEmailMock.mockResolvedValue({ email: 'user@example.com' });
        ActivateReferralRemotePhoneMock.mockResolvedValue({ email: 'phone:13800138000' });
    });

    afterEach(() => {
        vi.useRealTimers();
        cleanup();
    });

    const waitForRegistrationDetails = async () => {
        await waitFor(() => {
            expect(screen.queryByRole('button', { name: /Continue|继续|Checking|检查中/ })).toBeNull();
        });
    };

    const continueRegistrationIdentity = async (identity = 'user@example.com') => {
        const identityInput = await screen.findByPlaceholderText(/Email or phone|邮箱或手机号/);
        fireEvent.change(identityInput, { target: { value: identity } });
        await waitFor(() => {
            expect((screen.getByRole('button', { name: /Continue|继续/ }) as HTMLButtonElement).disabled).toBe(false);
        });
        fireEvent.click(screen.getByRole('button', { name: /Continue|继续/ }));
        await waitForRegistrationDetails();
    };

    const confirmEmailRegistration = async () => {
        fireEvent.change(await screen.findByLabelText('Email verification code'), { target: { value: '123456' } });
        fireEvent.click(await screen.findByRole('button', { name: /Verify & continue/ }));
    };

    const mockPhoneRegistrationTarget = (hubURL = 'http://hub.example.com') => {
        ResolveRemoteRegistrationTargetMock.mockImplementation(async (identity: string) => ({
            identity,
            hub_url: hubURL,
            hub_id: 'hub-phone',
            tenant_id: 'tenant-phone',
            method: 'phone',
            code_length: 6,
        }));
    };

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

    it('persists onboarding completion only once when a save rerender replaces callbacks', async () => {
        StartCodeGenSSOEmbeddedMock.mockResolvedValue(undefined);
        WaitCodeGenSSOResultMock.mockResolvedValue({ email: 'tiger@example.com', message: 'SSO OK' });
        FetchCodeGenModelsMock.mockResolvedValue([]);
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true });
        const saveCompletion = vi.fn();

        function CompletionHarness() {
            const [, setSaveVersion] = useState(0);
            return (
                <OnboardingWizard
                    {...baseProps}
                    brandId="qianxin"
                    brandDisplayName="TigerClaw"
                    onSaveField={(patch) => {
                        saveCompletion(patch);
                        setSaveVersion(version => version + 1);
                    }}
                />
            );
        }

        render(<CompletionHarness />);
        fireEvent.click(screen.getByRole('button', { name: /Enterprise SSO Login/ }));
        await waitFor(() => expect(baseProps.onRegistered).toHaveBeenCalledTimes(1));
        fireEvent.click(screen.getByRole('button', { name: 'Next' }));
        fireEvent.click(await screen.findByRole('button', { name: 'Skip' }));

        await waitFor(() => {
            const completionWrites = saveCompletion.mock.calls.filter(([patch]) => patch?.onboarding_done === true);
            expect(completionWrites).toHaveLength(1);
            expect(completionWrites[0]).toEqual([{ onboarding_done: true }]);
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

    it('renders optional service redeem code text in registration step', async () => {
        render(<OnboardingWizard {...baseProps} />);

        expect((await screen.findAllByText('User ID')).length).toBeGreaterThan(0);
        expect(screen.queryByText('Email')).toBeNull();
        expect(screen.queryByText('Service redeem code')).toBeNull();
        await continueRegistrationIdentity();
        expect(await screen.findByText('Service redeem code')).toBeTruthy();
        expect(screen.getByPlaceholderText('Enter service redeem code (optional)')).toBeTruthy();
    });

    it('binds a completed referral email registration without another OTP or registration', async () => {
        ClaimReferralHandoffMock.mockResolvedValue({
            registration_session: 'completed-referral-session',
            tenant: { id: 'tenant-referral' },
            registration_method: 'email',
            registration_status: 'registered_rewarded',
            registered_identity: 'registered@example.com',
            registered_identity_type: 'email',
        });
        GetReferralRegistrationStatusMock.mockResolvedValue({ registration_status: 'registered_rewarded' });
        ActivateReferralRemoteEmailMock.mockResolvedValue({ email: 'registered@example.com' });

        render(<OnboardingWizard {...baseProps} referralHandoff="opaque-one-time-handoff" />);

        await waitFor(() => {
            expect(screen.getByDisplayValue('registered@example.com')).toBeTruthy();
            expect(screen.getByText(/account is registered. bind this device to continue/i)).toBeTruthy();
        });
        expect(screen.queryByLabelText('Email verification code')).toBeNull();
        expect(screen.queryByPlaceholderText('Enter invitation code (optional)')).toBeNull();

        fireEvent.click(screen.getByRole('button', { name: 'Register' }));

        await waitFor(() => {
            expect(ActivateReferralRemoteEmailMock).toHaveBeenCalledWith(
                'http://hub.example.com',
                'registered@example.com',
                'tenant-referral',
                'completed-referral-session',
            );
        });
        expect(RegisterReferralEmailMock).not.toHaveBeenCalled();
        expect(RegisterReferralPhoneMock).not.toHaveBeenCalled();
        expect(GetReferralRegistrationStatusMock).not.toHaveBeenCalled();
        expect(baseProps.onRegistered).toHaveBeenCalledTimes(1);
    });

    it('keeps a completed email referral on email enrollment if the tenant later switches to phone-only', async () => {
        ClaimReferralHandoffMock.mockResolvedValue({
            registration_session: 'completed-email-after-method-change',
            tenant: { id: 'tenant-referral' },
            registration_method: 'phone',
            registration_status: 'registered_rewarded',
            registered_identity: 'registered@example.com',
            registered_identity_type: 'email',
        });
        ActivateReferralRemoteEmailMock.mockResolvedValue({ email: 'registered@example.com' });

        render(<OnboardingWizard {...baseProps} referralHandoff="opaque-email-method-change" />);

        await waitFor(() => {
            expect(screen.getByDisplayValue('registered@example.com')).toBeTruthy();
        });
        expect(screen.queryByLabelText('Phone')).toBeNull();

        fireEvent.click(screen.getByRole('button', { name: 'Register' }));

        await waitFor(() => {
            expect(ActivateReferralRemoteEmailMock).toHaveBeenCalledWith(
                'http://hub.example.com',
                'registered@example.com',
                'tenant-referral',
                'completed-email-after-method-change',
            );
        });
        expect(ActivateReferralRemotePhoneMock).not.toHaveBeenCalled();
    });

    it('associates the identity-only user ID label with its input', async () => {
        render(<OnboardingWizard {...baseProps} />);

        const identityInput = await screen.findByLabelText(/User ID/) as HTMLInputElement;
        expect(identityInput.placeholder).toBe('Email or phone');
        expect(identityInput.getAttribute('autocomplete')).toBe('username');
        expect(identityInput.required).toBe(true);
        expect(identityInput.getAttribute('aria-required')).toBe('true');
        expect(screen.getByRole('button', { name: 'Close' })).toBeTruthy();
    });

    it('uses account identity copy instead of email copy for zh onboarding', async () => {
        render(<OnboardingWizard {...baseProps} lang="zh-Hans" />);

        const localizedIdentityLabel = await screen.findByLabelText(/用户ID/);
        expect(localizedIdentityLabel.getAttribute('placeholder')).toBe('邮箱或手机号');
    });

    it('can resolve the registration target before the initial Hub auth probe finishes', async () => {
        let resolveAuth!: (value: { method: 'email'; code_length: number }) => void;
        GetRemoteRegistrationAuthMock.mockReturnValue(new Promise(resolve => {
            resolveAuth = resolve;
        }));
        mockPhoneRegistrationTarget();

        render(<OnboardingWizard {...baseProps} />);

        expect(screen.getByRole('button', { name: 'Continue' })).toBeTruthy();
        expect(screen.queryByPlaceholderText('name@example.com')).toBeNull();
        const continueButton = screen.getByRole('button', { name: 'Continue' }) as HTMLButtonElement;
        expect(continueButton.disabled).toBe(true);
        expect(ActivateRemoteMock).not.toHaveBeenCalled();
        expect(ActivateRemoteSMSMock).not.toHaveBeenCalled();

        fireEvent.change(await screen.findByPlaceholderText('Email or phone'), { target: { value: '13800138000' } });
        await waitFor(() => {
            expect((screen.getByRole('button', { name: 'Continue' }) as HTMLButtonElement).disabled).toBe(false);
        });
        fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
        expect(await screen.findByPlaceholderText('13800138000')).toBeTruthy();
        expect(screen.queryByPlaceholderText('name@example.com')).toBeNull();

        await act(async () => {
            resolveAuth({ method: 'email', code_length: 6 });
        });
        expect(screen.queryByText(/current verification method is email registration/i)).toBeNull();
        expect(await screen.findByPlaceholderText('13800138000')).toBeTruthy();
    });

    it('prefills phone registration from the routed account identity', async () => {
        GetRemoteRegistrationAuthMock.mockResolvedValue({ method: 'phone', code_length: 6 });
        mockPhoneRegistrationTarget();

        render(<OnboardingWizard {...baseProps} email="13900139000" lang="zh-Hans" />);

        fireEvent.click(await screen.findByRole('button', { name: /继续/ }));
        await waitForRegistrationDetails();
        const phoneInput = await screen.findByPlaceholderText('13800138000') as HTMLInputElement;
        await waitFor(() => {
            expect(phoneInput.value).toBe('13900139000');
        });
    });

    it('requires a phone identity when the tenant uses phone-only registration', async () => {
        GetRemoteRegistrationAuthMock.mockResolvedValue({ method: 'phone', code_length: 6 });
        mockPhoneRegistrationTarget();

        render(<OnboardingWizard {...baseProps} />);

        fireEvent.change(await screen.findByPlaceholderText('Email or phone'), { target: { value: 'user@example.com' } });
        fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
        expect(await screen.findByText(/accepts phone registration and sign-in only/i)).toBeTruthy();
        expect(screen.queryByText('Service redeem code')).toBeNull();
        expect(ActivateRemoteMock).not.toHaveBeenCalled();
        expect(ActivateRemoteSMSMock).not.toHaveBeenCalled();
    });

    it('rejects an email identity before attempting registration when the tenant uses phone-only registration', async () => {
        GetRemoteRegistrationAuthMock.mockResolvedValue({ method: 'phone', code_length: 6 });
        mockPhoneRegistrationTarget();
        render(<OnboardingWizard {...baseProps} />);

        fireEvent.change(await screen.findByPlaceholderText('Email or phone'), { target: { value: 'new-user@example.com' } });
        fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
        expect(await screen.findByText(/accepts phone registration and sign-in only/i)).toBeTruthy();
        expect(ActivateRemoteMock).not.toHaveBeenCalled();
        expect(ActivateRemoteSMSMock).not.toHaveBeenCalled();
    });

    it('does not attempt email activation when the tenant uses phone-only registration', async () => {
        GetRemoteRegistrationAuthMock.mockResolvedValue({ method: 'phone', code_length: 6 });
        mockPhoneRegistrationTarget();
        render(<OnboardingWizard {...baseProps} />);

        fireEvent.change(await screen.findByPlaceholderText('Email or phone'), { target: { value: 'new-user@163.com' } });
        fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
        expect(await screen.findByText(/accepts phone registration and sign-in only/i)).toBeTruthy();
        expect(ActivateRemoteMock).not.toHaveBeenCalled();
    });

    it('asks for an email address when the tenant requires email registration', async () => {
        GetRemoteRegistrationAuthMock.mockResolvedValue({ method: 'email', code_length: 6 });

        render(<OnboardingWizard {...baseProps} />);

        fireEvent.change(await screen.findByPlaceholderText('Email or phone'), { target: { value: '13800138000' } });
        fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

        expect(await screen.findByText(/current verification method is email registration/i)).toBeTruthy();
        expect(screen.queryByText('Service redeem code')).toBeNull();
    });

    it('shows a diagnostic registration-auth error instead of an email-required fallback', async () => {
        GetRemoteRegistrationAuthMock.mockResolvedValue({ method: 'email', code_length: 6 });
        ResolveRemoteRegistrationTargetMock.mockRejectedValue(new Error('load registration auth config failed: 502 Bad Gateway'));

        render(<OnboardingWizard {...baseProps} />);

        fireEvent.change(await screen.findByPlaceholderText('Email or phone'), { target: { value: '13800138000' } });
        fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

        expect(await screen.findByText(/registration verification method could not be confirmed/i)).toBeTruthy();
        expect(screen.queryByText(/current verification method is email registration/i)).toBeNull();
        expect(screen.queryByText('Service redeem code')).toBeNull();
    });

    it('resolves a phone user ID before enforcing the current hub registration method', async () => {
        GetRemoteRegistrationAuthMock.mockResolvedValue({ method: 'email', code_length: 6 });
        mockPhoneRegistrationTarget('http://phone-hub.example.com');

        render(<OnboardingWizard {...baseProps} hubUrl="http://email-hub.example.com" />);

        fireEvent.change(await screen.findByPlaceholderText('Email or phone'), { target: { value: '13800138000' } });
        fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
        await waitForRegistrationDetails();

        const phoneInput = await screen.findByPlaceholderText('13800138000') as HTMLInputElement;
        expect(phoneInput.value).toBe('13800138000');
        expect(screen.queryByText(/current verification method is email registration/i)).toBeNull();

        fireEvent.click(screen.getByRole('button', { name: 'Code' }));
        await waitFor(() => {
            expect(SendRemoteRegistrationSMSMock).toHaveBeenCalledWith('http://phone-hub.example.com', '13800138000', 'tenant-phone');
        });
    });

    it('resolves a phone user ID through HubCenter when no Hub URL is cached', async () => {
        GetRemoteRegistrationAuthMock.mockResolvedValue({ method: 'email', code_length: 6 });
        mockPhoneRegistrationTarget('http://phone-hub.example.com');

        render(<OnboardingWizard {...baseProps} hubUrl="" />);

        fireEvent.change(await screen.findByPlaceholderText('Email or phone'), { target: { value: '13800138000' } });
        fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
        await waitForRegistrationDetails();

        expect(ResolveRemoteRegistrationTargetMock).toHaveBeenCalledWith('13800138000');
        const phoneInput = await screen.findByPlaceholderText('13800138000') as HTMLInputElement;
        expect(phoneInput.value).toBe('13800138000');
        expect(screen.queryByText(/current verification method is email registration/i)).toBeNull();

        fireEvent.click(screen.getByRole('button', { name: 'Code' }));
        await waitFor(() => {
            expect(SendRemoteRegistrationSMSMock).toHaveBeenCalledWith('http://phone-hub.example.com', '13800138000', 'tenant-phone');
        });
    });

    it('ignores a stale routed target when the user ID changes while resolving', async () => {
        GetRemoteRegistrationAuthMock.mockResolvedValue({ method: 'email', code_length: 6 });
        let resolveFirst!: (value: unknown) => void;
        ResolveRemoteRegistrationTargetMock.mockImplementationOnce(async (identity: string) => new Promise(resolve => {
            resolveFirst = resolve;
        })).mockImplementationOnce(async (identity: string) => ({
            identity,
            hub_url: 'http://hub.example.com',
            tenant_id: 'tenant-email',
            method: 'email',
            code_length: 6,
        }));

        render(<OnboardingWizard {...baseProps} />);

        const identityInput = await screen.findByPlaceholderText('Email or phone');
        fireEvent.change(identityInput, { target: { value: '13800138000' } });
        fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
        fireEvent.change(identityInput, { target: { value: 'user@example.com' } });

        await act(async () => {
            resolveFirst({
                identity: '13800138000',
                hub_url: 'http://phone-hub.example.com',
                tenant_id: 'tenant-phone',
                method: 'phone',
                code_length: 6,
            });
        });

        expect(screen.queryByPlaceholderText('13800138000')).toBeNull();
        expect(screen.getByPlaceholderText('Email or phone')).toBeTruthy();

        fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
        await waitForRegistrationDetails();
        expect(await screen.findByText('Service redeem code')).toBeTruthy();
        expect(screen.queryByPlaceholderText('13800138000')).toBeNull();
    });

    it('returns from registration details to the identity-only step', async () => {
        render(<OnboardingWizard {...baseProps} />);

        await continueRegistrationIdentity();
        expect(await screen.findByText('Service redeem code')).toBeTruthy();
        expect(screen.getByText('Free trial')).toBeTruthy();

        fireEvent.click(screen.getByRole('button', { name: 'Edit' }));

        expect(await screen.findByPlaceholderText('Email or phone')).toBeTruthy();
        expect(screen.queryByText('Service redeem code')).toBeNull();
        expect(screen.queryByText('Free trial')).toBeNull();
    });

    it('keeps identity continue disabled until a user ID is entered and clears stale service code on edit', async () => {
        render(<OnboardingWizard {...baseProps} />);

        const identityInput = await screen.findByPlaceholderText('Email or phone');
        await waitFor(() => {
            expect((screen.getByRole('button', { name: 'Continue' }) as HTMLButtonElement).disabled).toBe(true);
        });

        fireEvent.change(identityInput, { target: { value: 'user@example.com' } });
        fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
        await waitForRegistrationDetails();

        const serviceCodeInput = await screen.findByPlaceholderText('Enter service redeem code (optional)') as HTMLInputElement;
        fireEvent.change(serviceCodeInput, { target: { value: 'STALE-CODE' } });
        expect(serviceCodeInput.value).toBe('STALE-CODE');

        fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
        fireEvent.change(await screen.findByPlaceholderText('Email or phone'), { target: { value: 'second@example.com' } });
        fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
        await waitForRegistrationDetails();

        expect((await screen.findByPlaceholderText('Enter service redeem code (optional)') as HTMLInputElement).value).toBe('');
    });

    it('requires SMS send and code before phone registration succeeds', async () => {
        GetRemoteRegistrationAuthMock.mockResolvedValue({ method: 'phone', code_length: 6 });
        mockPhoneRegistrationTarget();
        SendRemoteRegistrationSMSMock.mockResolvedValue({ ok: true, code_length: 6, expires_min: 5 });
        ActivateRemoteSMSMock.mockResolvedValue({ email: 'phone:13800138000', vip_flag: true });

        render(<OnboardingWizard {...baseProps} />);

        await continueRegistrationIdentity('13800138000');
        const phoneInput = await screen.findByPlaceholderText('13800138000');
        const registerButton = screen.getByRole('button', { name: 'Register' }) as HTMLButtonElement;
        expect(registerButton.disabled).toBe(true);

        fireEvent.change(phoneInput, { target: { value: '13800138000' } });
        expect(registerButton.disabled).toBe(true);
        fireEvent.click(screen.getByRole('button', { name: 'Code' }));

        await waitFor(() => {
            expect(SendRemoteRegistrationSMSMock).toHaveBeenCalledWith('http://hub.example.com', '13800138000', 'tenant-phone');
        });
        expect(ActivateRemoteSMSMock).not.toHaveBeenCalled();
        expect(registerButton.disabled).toBe(true);

        fireEvent.change(screen.getByPlaceholderText('Enter 6-digit code'), { target: { value: '123456' } });
        expect(registerButton.disabled).toBe(false);
        fireEvent.click(registerButton);

        await waitFor(() => {
            expect(ActivateRemoteSMSMock).toHaveBeenCalledWith('http://hub.example.com', '13800138000', '123456', '', 'tenant-phone', 'hub-phone');
        });
        expect(ActivateRemoteMock).not.toHaveBeenCalled();
        expect(baseProps.onSaveField).toHaveBeenCalledWith({ remote_email: 'phone:13800138000', remote_mobile: '13800138000' });
        expect(baseProps.onRegistered).toHaveBeenCalledTimes(1);
    });

    it('uses email verification for an email identity when the tenant allows mixed registration', async () => {
        ResolveRemoteRegistrationTargetMock.mockResolvedValue({
            hub_url: 'http://mixed-hub.example.com',
            hub_id: 'hub-mixed',
            tenant_id: 'tenant-mixed',
            method: 'mixed',
            code_length: 6,
        });

        render(<OnboardingWizard {...baseProps} />);

        await continueRegistrationIdentity('mixed@example.com');
        const resolvedIdentity = screen.getByLabelText(/User ID/) as HTMLInputElement;
        expect(resolvedIdentity.value).toBe('mixed@example.com');
        expect(resolvedIdentity.readOnly).toBe(true);
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));

        await waitFor(() => {
            expect(SendRemoteRegistrationEmailMock).toHaveBeenCalledWith('http://mixed-hub.example.com', 'mixed@example.com', 'tenant-mixed');
        });
        expect(SendRemoteRegistrationSMSMock).not.toHaveBeenCalled();

        fireEvent.change(screen.getByLabelText('Email verification code'), { target: { value: '123456' } });
        fireEvent.click(screen.getByRole('button', { name: /Verify & continue/ }));

        await waitFor(() => {
            expect(ActivateRemoteEmailMock).toHaveBeenCalledWith('http://mixed-hub.example.com', 'mixed@example.com', '123456', '', 'tenant-mixed', 'hub-mixed');
        });
        expect(ActivateRemoteSMSMock).not.toHaveBeenCalled();
    });

    it('refreshes an unauthenticated email route before sending an OTP', async () => {
        ResolveRemoteRegistrationTargetMock
            .mockResolvedValueOnce({
                hub_url: 'http://stale-hub.example.com',
                hub_id: 'hub-stale',
                tenant_id: 'tenant-default',
                method: 'email',
                code_length: 6,
            })
            .mockResolvedValueOnce({
                hub_url: 'http://public-fallback.example.com',
                hub_id: 'hub-public',
                tenant_id: 'tenant-default',
                method: 'mixed',
                code_length: 6,
            });

        render(<OnboardingWizard {...baseProps} />);
        await continueRegistrationIdentity('fresh@example.com');
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));

        await waitFor(() => {
            expect(ResolveRemoteRegistrationTargetMock).toHaveBeenCalledTimes(2);
            expect(SendRemoteRegistrationEmailMock).toHaveBeenCalledWith('http://public-fallback.example.com', 'fresh@example.com', 'tenant-default');
        });
        fireEvent.change(await screen.findByLabelText('Email verification code'), { target: { value: '123456' } });
        fireEvent.click(await screen.findByRole('button', { name: /Verify & continue/ }));
        await waitFor(() => {
            expect(ActivateRemoteEmailMock).toHaveBeenCalledWith('http://public-fallback.example.com', 'fresh@example.com', '123456', '', 'tenant-default', 'hub-public');
        });
    });

    it('clears a stale email-delivery error when a later code request succeeds', async () => {
        ResolveRemoteRegistrationTargetMock.mockResolvedValue({
            hub_url: 'http://mixed-hub.example.com',
            hub_id: 'hub-mixed',
            tenant_id: 'tenant-mixed',
            method: 'mixed',
            code_length: 6,
        });
        SendRemoteRegistrationEmailMock
            .mockRejectedValueOnce(new Error('MAIL_NOT_CONFIGURED: Mail delivery is not configured'))
            .mockResolvedValueOnce({ ok: true, tenant_id: 'tenant-mixed', code_length: 6, resend_cooldown_seconds: 0 });

        render(<OnboardingWizard {...baseProps} />);
        await continueRegistrationIdentity('mixed@example.com');
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));

        expect(await screen.findByText(/mixed-hub\.example\.com.*not configured email delivery/i)).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Resend' }));

        await waitFor(() => expect(SendRemoteRegistrationEmailMock).toHaveBeenCalledTimes(2));
        await waitFor(() => expect(screen.queryByText(/not configured email delivery/i)).toBeNull());
    });

	it('reroutes email verification with an invitation before sending the code', async () => {
		ResolveRemoteRegistrationTargetMock.mockResolvedValue({
			hub_url: 'http://generic-hub.example.com',
			hub_id: 'hub-generic',
			method: 'email',
			code_length: 6,
		});
		ResolveRemoteRegistrationTargetWithInvitationMock.mockResolvedValue({
			hub_url: 'http://tenant-hub.example.com',
			hub_id: 'hub-tenant',
			tenant_id: 'tenant-acme',
			method: 'email',
			code_length: 6,
		});

		render(<OnboardingWizard {...baseProps} />);
		await continueRegistrationIdentity('invited@example.com');
		fireEvent.change(screen.getByPlaceholderText('Enter invitation code (optional)'), { target: { value: 'invite-1' } });
		fireEvent.click(screen.getByRole('button', { name: 'Register' }));

		await waitFor(() => {
			expect(ResolveRemoteRegistrationTargetWithInvitationMock).toHaveBeenCalledWith('invited@example.com', 'INVITE-1');
			expect(SendRemoteRegistrationEmailMock).toHaveBeenCalledWith('http://tenant-hub.example.com', 'invited@example.com', 'tenant-acme');
		});
		fireEvent.change(await screen.findByLabelText('Email verification code'), { target: { value: '123456' } });
		fireEvent.click(await screen.findByRole('button', { name: /Verify & continue/ }));
		await waitFor(() => {
			expect(ActivateRemoteEmailMock).toHaveBeenCalledWith('http://tenant-hub.example.com', 'invited@example.com', '123456', 'INVITE-1', 'tenant-acme', 'hub-tenant');
		});
	});

    it('skips the email OTP only after the invitation route advertises the bypass', async () => {
        ResolveRemoteRegistrationTargetMock.mockResolvedValue({
            hub_url: 'http://generic-hub.example.com', hub_id: 'hub-generic', method: 'email', code_length: 6,
        });
        ResolveRemoteRegistrationTargetWithInvitationMock.mockResolvedValue({
            hub_url: 'http://tenant-hub.example.com', hub_id: 'hub-tenant', tenant_id: 'tenant-acme',
            method: 'email', email_verification_required: false, code_length: 6,
        });

        render(<OnboardingWizard {...baseProps} />);
        await continueRegistrationIdentity('invited@example.com');
        fireEvent.change(screen.getByPlaceholderText('Enter invitation code (optional)'), { target: { value: 'invite-1' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));

        await waitFor(() => {
            expect(ActivateRemoteEmailMock).toHaveBeenCalledWith('http://tenant-hub.example.com', 'invited@example.com', '', 'INVITE-1', 'tenant-acme', 'hub-tenant');
        });
        expect(SendRemoteRegistrationEmailMock).not.toHaveBeenCalled();
    });

    it('does not let a stale invitation bypass register after the code is edited', async () => {
        ResolveRemoteRegistrationTargetMock.mockResolvedValue({
            hub_url: 'http://generic-hub.example.com', hub_id: 'hub-generic', method: 'email', code_length: 6,
        });
        let resolveInvitation!: (value: unknown) => void;
        ResolveRemoteRegistrationTargetWithInvitationMock.mockImplementationOnce(() => new Promise(resolve => {
            resolveInvitation = resolve;
        }));

        render(<OnboardingWizard {...baseProps} />);
        await continueRegistrationIdentity('invited@example.com');
        const invitationInput = screen.getByPlaceholderText('Enter invitation code (optional)');
        fireEvent.change(invitationInput, { target: { value: 'invite-1' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        await waitFor(() => expect(ResolveRemoteRegistrationTargetWithInvitationMock).toHaveBeenCalledWith('invited@example.com', 'INVITE-1'));

        fireEvent.change(invitationInput, { target: { value: 'invite-2' } });
        resolveInvitation({
            hub_url: 'http://tenant-hub.example.com', hub_id: 'hub-tenant', tenant_id: 'tenant-acme',
            method: 'email', email_verification_required: false, code_length: 6,
        });
        await act(async () => { await Promise.resolve(); });

        expect(ActivateRemoteEmailMock).not.toHaveBeenCalled();
        expect(SendRemoteRegistrationEmailMock).not.toHaveBeenCalled();
    });

    it('reroutes SMS verification with an invitation before sending the code', async () => {
        mockPhoneRegistrationTarget('http://generic-hub.example.com');
        ResolveRemoteRegistrationTargetWithInvitationMock.mockResolvedValue({
            hub_url: 'http://tenant-hub.example.com',
            hub_id: 'hub-tenant',
            tenant_id: 'tenant-acme',
            method: 'phone',
            code_length: 6,
        });

        render(<OnboardingWizard {...baseProps} />);
        await continueRegistrationIdentity('13800138000');
        fireEvent.change(screen.getByPlaceholderText('Enter invitation code (optional)'), { target: { value: 'invite-1' } });
        fireEvent.click(screen.getByRole('button', { name: 'Code' }));

        await waitFor(() => {
            expect(ResolveRemoteRegistrationTargetWithInvitationMock).toHaveBeenCalledWith('13800138000', 'INVITE-1');
            expect(SendRemoteRegistrationSMSMock).toHaveBeenCalledWith('http://tenant-hub.example.com', '13800138000', 'tenant-acme');
        });
        fireEvent.change(screen.getByPlaceholderText('Enter 6-digit code'), { target: { value: '123456' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        await waitFor(() => {
            expect(ActivateRemoteSMSMock).toHaveBeenCalledWith('http://tenant-hub.example.com', '13800138000', '123456', 'INVITE-1', 'tenant-acme', 'hub-tenant');
        });
    });

    it('does not send an email OTP when an invitation routes to a phone-only tenant', async () => {
        ResolveRemoteRegistrationTargetMock.mockResolvedValue({
            hub_url: 'http://generic-hub.example.com', hub_id: 'hub-generic', method: 'email', code_length: 6,
        });
        ResolveRemoteRegistrationTargetWithInvitationMock.mockResolvedValue({
            hub_url: 'http://tenant-hub.example.com', hub_id: 'hub-tenant', tenant_id: 'tenant-acme', method: 'phone', code_length: 6,
        });

        render(<OnboardingWizard {...baseProps} />);
        await continueRegistrationIdentity('invited@example.com');
        fireEvent.change(screen.getByPlaceholderText('Enter invitation code (optional)'), { target: { value: 'invite-1' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));

        expect(await screen.findByText(/phone-only tenant/i)).toBeTruthy();
        expect(SendRemoteRegistrationEmailMock).not.toHaveBeenCalled();
    });

    it('accepts an invitation route to the default tenant for email verification', async () => {
        ResolveRemoteRegistrationTargetMock.mockResolvedValue({
            hub_url: 'http://generic-hub.example.com', hub_id: 'hub-generic', method: 'email', code_length: 6,
        });
        ResolveRemoteRegistrationTargetWithInvitationMock.mockResolvedValue({
            hub_url: 'http://tenant-hub.example.com', hub_id: 'hub-tenant', tenant_id: '', method: 'email', code_length: 6,
        });

        render(<OnboardingWizard {...baseProps} />);
        await continueRegistrationIdentity('invited@example.com');
        fireEvent.change(screen.getByPlaceholderText('Enter invitation code (optional)'), { target: { value: 'invite-default' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));

        await waitFor(() => {
            expect(SendRemoteRegistrationEmailMock).toHaveBeenCalledWith('http://tenant-hub.example.com', 'invited@example.com', '');
        });
        fireEvent.change(await screen.findByLabelText('Email verification code'), { target: { value: '123456' } });
        fireEvent.click(await screen.findByRole('button', { name: /Verify & continue/ }));
        await waitFor(() => {
            expect(ActivateRemoteEmailMock).toHaveBeenCalledWith('http://tenant-hub.example.com', 'invited@example.com', '123456', 'INVITE-DEFAULT', '', 'hub-tenant');
        });
    });

    it('restores the identity route when an invitation is cleared before requesting an OTP', async () => {
        ResolveRemoteRegistrationTargetMock.mockResolvedValue({
            hub_url: 'http://generic-hub.example.com', hub_id: 'hub-generic', tenant_id: 'tenant-generic', method: 'email', code_length: 6,
        });
        ResolveRemoteRegistrationTargetWithInvitationMock.mockResolvedValue({
            hub_url: 'http://tenant-hub.example.com', hub_id: 'hub-tenant', tenant_id: 'tenant-acme', method: 'email', code_length: 6,
        });

        render(<OnboardingWizard {...baseProps} />);
        await continueRegistrationIdentity('invited@example.com');
        const invitationInput = screen.getByPlaceholderText('Enter invitation code (optional)');
        fireEvent.change(invitationInput, { target: { value: 'invite-1' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        await waitFor(() => expect(SendRemoteRegistrationEmailMock).toHaveBeenCalledWith('http://tenant-hub.example.com', 'invited@example.com', 'tenant-acme'));

        fireEvent.change(invitationInput, { target: { value: '' } });
        fireEvent.click(screen.getByRole('button', { name: 'Resend' }));
        await waitFor(() => {
            expect(SendRemoteRegistrationEmailMock).toHaveBeenLastCalledWith('http://generic-hub.example.com', 'invited@example.com', 'tenant-generic');
        });
    });

    it('ignores a stale SMS send after returning to edit the identity', async () => {
        mockPhoneRegistrationTarget();
        let resolveSMS!: (value: unknown) => void;
        SendRemoteRegistrationSMSMock.mockImplementationOnce(() => new Promise(resolve => {
            resolveSMS = resolve;
        }));

        render(<OnboardingWizard {...baseProps} />);
        await continueRegistrationIdentity('13800138000');
        fireEvent.click(screen.getByRole('button', { name: 'Code' }));
        await waitFor(() => expect(SendRemoteRegistrationSMSMock).toHaveBeenCalledTimes(1));

        fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
        resolveSMS({ ok: true, code_length: 6, expires_min: 5 });
        await act(async () => { await Promise.resolve(); });

        expect(screen.getByPlaceholderText('Email or phone')).toBeTruthy();
        expect(screen.queryByText(/Verification code sent/i)).toBeNull();
    });

    it('uses SMS verification for a phone identity when the tenant allows mixed registration', async () => {
        ResolveRemoteRegistrationTargetMock.mockResolvedValue({
            hub_url: 'http://mixed-hub.example.com',
            hub_id: 'hub-mixed',
            tenant_id: 'tenant-mixed',
            method: 'mixed',
            code_length: 6,
        });

        render(<OnboardingWizard {...baseProps} />);

        await continueRegistrationIdentity('13800138000');
        expect((screen.getByLabelText(/Phone/) as HTMLInputElement).value).toBe('13800138000');
        fireEvent.click(screen.getByRole('button', { name: 'Code' }));
        await waitFor(() => {
            expect(SendRemoteRegistrationSMSMock).toHaveBeenCalledWith('http://mixed-hub.example.com', '13800138000', 'tenant-mixed');
        });
        fireEvent.change(screen.getByPlaceholderText('Enter 6-digit code'), { target: { value: '123456' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));

        await waitFor(() => {
            expect(ActivateRemoteSMSMock).toHaveBeenCalledWith('http://mixed-hub.example.com', '13800138000', '123456', '', 'tenant-mixed', 'hub-mixed');
        });
        expect(ActivateRemoteEmailMock).not.toHaveBeenCalled();
    });

    it('explains when a phone registration is disabled after SMS verification', async () => {
        mockPhoneRegistrationTarget();
        ActivateRemoteSMSMock.mockRejectedValue(new Error('REGISTRATION_DISABLED: new user registration is disabled for this tenant'));

        render(<OnboardingWizard {...baseProps} />);

        await continueRegistrationIdentity('13800138000');
        fireEvent.click(screen.getByRole('button', { name: 'Code' }));
        await waitFor(() => {
            expect(SendRemoteRegistrationSMSMock).toHaveBeenCalled();
        });
        fireEvent.change(screen.getByLabelText(/SMS Code/), { target: { value: '123456' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));

        expect(await screen.findByText(/does not accept new phone registrations/i)).toBeTruthy();
    });

    it('treats an existing phone SMS flow as login and device binding', async () => {
        GetRemoteRegistrationAuthMock.mockResolvedValue({ method: 'phone', code_length: 6 });
        mockPhoneRegistrationTarget();
        SendRemoteRegistrationSMSMock.mockResolvedValue({ ok: true, code_length: 6, expires_min: 5, purpose: 'verify_bound_phone' });
        ActivateRemoteSMSMock.mockResolvedValue({ email: 'owner@example.com', vip_flag: false, rebound_existing_user: true });

        render(<OnboardingWizard {...baseProps} />);

        await continueRegistrationIdentity('13800138000');
        fireEvent.click(screen.getByRole('button', { name: 'Code' }));

        await waitFor(() => {
            expect(SendRemoteRegistrationSMSMock).toHaveBeenCalledWith('http://hub.example.com', '13800138000', 'tenant-phone');
        });
        fireEvent.change(screen.getByPlaceholderText('Enter 6-digit code'), { target: { value: '123456' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));

        expect(await screen.findByText(/Device binding complete/i)).toBeTruthy();
        expect(await screen.findByText(/Phone verified\. Connecting to Hub/i)).toBeTruthy();
        expect(ActivateRemoteSMSMock).toHaveBeenCalledWith('http://hub.example.com', '13800138000', '123456', '', 'tenant-phone', 'hub-phone');
        expect(baseProps.onSaveField).toHaveBeenCalledWith({ remote_email: 'owner@example.com', remote_mobile: '13800138000' });
    });

    it('localizes the daily SMS limit error when sending a phone code', async () => {
        GetRemoteRegistrationAuthMock.mockResolvedValue({ method: 'phone', code_length: 6 });
        mockPhoneRegistrationTarget();
        SendRemoteRegistrationSMSMock.mockRejectedValue(new Error('SMS_DAILY_LIMIT_REACHED: daily SMS verification limit reached; max 3 per day'));

        render(<OnboardingWizard {...baseProps} lang="zh-CN" />);

        await continueRegistrationIdentity('13800138000');
        fireEvent.click(screen.getByRole('button', { name: /验证码|Code/ }));

        expect(await screen.findByText(/今日短信验证码次数已达上限/)).toBeTruthy();
        expect(screen.getByText(/每天最多 3 次/)).toBeTruthy();
        expect(screen.queryByText(/SMS_DAILY_LIMIT_REACHED/)).toBeNull();
        expect(screen.queryByText(/daily SMS verification limit/i)).toBeNull();
    });

    it('keeps phone binding completion visible after parent refreshes the Hub URL', async () => {
        GetRemoteRegistrationAuthMock.mockResolvedValue({ method: 'phone', code_length: 6 });
        mockPhoneRegistrationTarget('http://phone-hub.example.com');
        SendRemoteRegistrationSMSMock.mockResolvedValue({ ok: true, code_length: 6, expires_min: 5, purpose: 'verify_bound_phone' });
        ActivateRemoteSMSMock.mockResolvedValue({ email: 'owner@example.com', vip_flag: false, rebound_existing_user: true });

        const { rerender } = render(<OnboardingWizard {...baseProps} hubUrl="" />);

        await continueRegistrationIdentity('13800138000');
        fireEvent.click(screen.getByRole('button', { name: 'Code' }));
        await waitFor(() => {
            expect(SendRemoteRegistrationSMSMock).toHaveBeenCalledWith('http://phone-hub.example.com', '13800138000', 'tenant-phone');
        });
        fireEvent.change(screen.getByPlaceholderText('Enter 6-digit code'), { target: { value: '123456' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));

        expect(await screen.findByText(/Device binding complete/i)).toBeTruthy();

        rerender(<OnboardingWizard {...baseProps} hubUrl="http://phone-hub.example.com" />);

        expect(await screen.findByText(/Device binding complete/i)).toBeTruthy();
        expect(screen.queryByPlaceholderText('Email or phone')).toBeNull();
        expect(screen.queryByRole('button', { name: 'Continue' })).toBeNull();
    });

    it('keeps the phone verification target locked to the confirmed user ID', async () => {
        GetRemoteRegistrationAuthMock.mockResolvedValue({ method: 'phone', code_length: 6 });
        mockPhoneRegistrationTarget();

        render(<OnboardingWizard {...baseProps} />);

        await continueRegistrationIdentity('13800138000');
        const phoneInput = await screen.findByPlaceholderText('13800138000') as HTMLInputElement;
        expect(phoneInput.value).toBe('13800138000');
        expect(phoneInput.readOnly).toBe(true);

        fireEvent.change(phoneInput, { target: { value: '13900139000' } });
        expect(phoneInput.value).toBe('13800138000');

        fireEvent.click(screen.getByRole('button', { name: 'Code' }));
        await waitFor(() => {
            expect(SendRemoteRegistrationSMSMock).toHaveBeenCalledWith('http://hub.example.com', '13800138000', 'tenant-phone');
        });
    });

    it('redeems optional service code after registration when provided', async () => {
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true });
        RedeemHubLLMServiceMock.mockResolvedValue({ active: true, skip_llm_config: true });

        render(<OnboardingWizard {...baseProps} />);

        await continueRegistrationIdentity();
        fireEvent.change(screen.getByPlaceholderText('Enter service redeem code (optional)'), { target: { value: ' card123 ' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        await confirmEmailRegistration();

        await waitFor(() => {
            expect(RedeemHubLLMServiceMock).toHaveBeenCalledWith('CARD123');
        });
        expect(baseProps.onLLMConfigured).toHaveBeenCalledTimes(1);
        expect(screen.queryByPlaceholderText('Enter service redeem code (optional)')).toBeNull();
        expect(screen.getByText(/Registration complete/)).toBeTruthy();
    });

    it('skips LLM step after successful redeem even when skip_llm_config is false', async () => {
        // Backend may return skip_llm_config: false due to provider registry
        // filtering, but the LLM provider is already configured in config.json
        // by applyHubLLMServiceStatusToConfig. Step 3 should still be skipped.
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true });
        RedeemHubLLMServiceMock.mockResolvedValue({ active: true, skip_llm_config: false });

        render(<OnboardingWizard {...baseProps} />);

        await continueRegistrationIdentity();
        fireEvent.change(screen.getByPlaceholderText('Enter service redeem code (optional)'), { target: { value: 'MYCODE' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        await confirmEmailRegistration();

        await waitFor(() => {
            expect(RedeemHubLLMServiceMock).toHaveBeenCalledWith('MYCODE');
        });
        // onLLMConfigured should be called even when skip_llm_config is false
        expect(baseProps.onLLMConfigured).toHaveBeenCalledTimes(1);

        // Navigate to step 2 (UI Mode)
        fireEvent.click(screen.getByRole('button', { name: 'Next' }));
        // Step 2 auto-completes, click Next again; should skip step 3 and go to step 4.
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

        await continueRegistrationIdentity();
        fireEvent.change(screen.getByPlaceholderText('Enter service redeem code (optional)'), { target: { value: 'WAITCODE' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        await confirmEmailRegistration();

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

        await continueRegistrationIdentity();
        fireEvent.change(screen.getByPlaceholderText('Enter service redeem code (optional)'), { target: { value: 'WAITCODE' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        await confirmEmailRegistration();

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

        await continueRegistrationIdentity();
        fireEvent.change(screen.getByPlaceholderText('Enter service redeem code (optional)'), { target: { value: 'LIMITED' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        await confirmEmailRegistration();

        expect(await screen.findByText(/period limited\. LLM setup is not skipped yet/i)).toBeTruthy();
        expect(screen.queryByText(/0s|0 seconds/i)).toBeNull();
        expect(baseProps.onLLMConfigured).not.toHaveBeenCalled();
    });

    it('shows inactive redeem reasons without crashing', async () => {
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true });
        RedeemHubLLMServiceMock.mockResolvedValue({
            active: false,
            skip_llm_config: false,
            inactive_reasons: ['grant credits are exhausted'],
        });

        render(<OnboardingWizard {...baseProps} />);

        await continueRegistrationIdentity();
        fireEvent.change(screen.getByPlaceholderText('Enter service redeem code (optional)'), { target: { value: 'USEDUP' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        await confirmEmailRegistration();

        expect(await screen.findByText(/credits are exhausted/i)).toBeTruthy();
    });

    it('skips LLM step when a period-limited grant is covered by another active official grant', async () => {
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true });
        RedeemHubLLMServiceMock.mockResolvedValue({
            active: true,
            skip_llm_config: false,
            credit_grants: [
                { status: 'period_limited', active: false, retry_after_seconds: 3600 },
                { status: 'active', active: true, credits_remaining: 50 },
            ],
        });

        render(<OnboardingWizard {...baseProps} />);

        await continueRegistrationIdentity();
        fireEvent.change(screen.getByPlaceholderText('Enter service redeem code (optional)'), { target: { value: 'COVERED' } });
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        await confirmEmailRegistration();

        await waitFor(() => {
            expect(RedeemHubLLMServiceMock).toHaveBeenCalledWith('COVERED');
        });
        expect(baseProps.onLLMConfigured).toHaveBeenCalledTimes(1);
        expect(screen.queryByText(/period limited\. LLM setup is not skipped yet/i)).toBeNull();
    });

    it('marks registration done after activation succeeds', async () => {
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true, phone_number: '17090134628' });
        GetRemoteActivationStatusMock.mockResolvedValue({ activated: true });

        render(<OnboardingWizard {...baseProps} />);

        await continueRegistrationIdentity();
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        await confirmEmailRegistration();

        await waitFor(() => {
            expect(screen.getByText(/Registration successful/)).toBeTruthy();
        });
        expect(screen.getByText(/Connecting to Hub in the background/)).toBeTruthy();
        expect(screen.getByText('Hub connecting')).toBeTruthy();
        expect(baseProps.onSaveField).toHaveBeenCalledWith({ remote_email: 'user@example.com' });
        expect(baseProps.onSaveField).toHaveBeenCalledWith({ remote_mobile: '17090134628' });
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

        await continueRegistrationIdentity();
        expect(screen.getByRole('button', { name: /Online mode/ }).getAttribute('aria-pressed')).toBe('true');
        expect((screen.getByLabelText(/Free trial/) as HTMLInputElement).checked).toBe(true);
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        await confirmEmailRegistration();

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

        await continueRegistrationIdentity();
        fireEvent.click(screen.getByLabelText(/Free trial/));
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        await confirmEmailRegistration();

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
        TestAndSaveMaclawLLMProvidersMock.mockResolvedValue({ message: 'hello', supports_vision: false });

        render(<OnboardingWizard {...baseProps} />);

        await continueRegistrationIdentity();
        expect(screen.getByLabelText(/Online mode/)).toBeTruthy();
        fireEvent.click(screen.getByLabelText(/Offline mode/));
        expect(await screen.findByRole('dialog', { name: /Offline Mode Notice/i })).toBeTruthy();
        expect(await screen.findByText(/classified or restricted networks/i)).toBeTruthy();
        expect(screen.getByText(/web search will be limited/i)).toBeTruthy();
        expect(screen.getByText(/Skip Hub registration and WeChat binding/i)).toBeTruthy();
        fireEvent.keyDown(window, { key: 'Escape' });
        await waitFor(() => {
            expect(screen.queryByRole('dialog', { name: /Offline Mode Notice/i })).toBeNull();
        });
        expect(baseProps.onClose).not.toHaveBeenCalled();

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
            expect(TestAndSaveMaclawLLMProvidersMock).toHaveBeenCalledWith(
                [expect.objectContaining({ name: 'Custom1', url: 'https://api.example.com/v1', key: 'secret', model: 'gpt-test' })],
                'Custom1',
                'Custom1',
            );
        });
        await waitFor(() => {
            expect(TestAndSaveMaclawLLMProvidersMock).toHaveBeenCalledTimes(1);
        });
        expect(baseProps.onRegistered).not.toHaveBeenCalled();
        expect(baseProps.onLLMConfigured).toHaveBeenCalledTimes(1);
        expect(baseProps.onSaveField).toHaveBeenCalledWith({ onboarding_done: true });
    });

    it('returns to the normal registration UI when offline mode is turned back off', async () => {
        render(<OnboardingWizard {...baseProps} />);

        await continueRegistrationIdentity();
        fireEvent.click(screen.getByLabelText(/Offline mode/));
        expect(screen.queryByPlaceholderText('name@example.com')).toBeNull();
        const offlineFreeTrial = screen.getByLabelText(/Free trial/) as HTMLInputElement;
        expect(offlineFreeTrial.disabled).toBe(true);
        expect(offlineFreeTrial.checked).toBe(false);
        expect(await screen.findByText(/classified or restricted networks/i)).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: /I understand/i }));

        fireEvent.click(screen.getByLabelText(/Online mode/));
        expect(screen.getByDisplayValue('user@example.com')).toBeTruthy();
        expect((screen.getByLabelText(/Free trial/) as HTMLInputElement).checked).toBe(true);
        expect(screen.queryAllByText(/classified or restricted networks/i)).toHaveLength(0);
    });

    it('ignores stale Hub LLM status responses after switching to offline mode', async () => {
        let resolveHubStatus: ((value: { active: boolean; skip_llm_config: boolean }) => void) | null = null;
        GetHubLLMServiceStatusMock.mockImplementation(() => new Promise(resolve => {
            resolveHubStatus = resolve;
        }));

        render(<OnboardingWizard {...baseProps} />);
        await continueRegistrationIdentity();
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

        await continueRegistrationIdentity();
        expect(screen.getByText('Free trial')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        await confirmEmailRegistration();

        await waitFor(() => {
            expect(screen.getByText(/Registration successful/)).toBeTruthy();
        });
        expect(screen.getByRole('status').textContent).toMatch(/Registration successful/);

        expect(screen.queryByText(/not ready/i)).toBeNull();
        expect(screen.getByText(/Verifying free trial service/)).toBeTruthy();
    });

    it('accepts backend success when returned machine credentials exist', async () => {
        ActivateRemoteMock.mockResolvedValue({ machine_id: 'mid-1', machine_token: 'tok-1' });
        GetRemoteActivationStatusMock.mockResolvedValue({ activated: false });

        render(<OnboardingWizard {...baseProps} />);

        await continueRegistrationIdentity();
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        await confirmEmailRegistration();

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

        await continueRegistrationIdentity();
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        await confirmEmailRegistration();

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

        await continueRegistrationIdentity();
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        await confirmEmailRegistration();

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
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true });
        GetRemoteConnectionStatusMock.mockResolvedValue({ connected: true });

        render(<OnboardingWizard {...baseProps} />);

        await continueRegistrationIdentity();
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        await confirmEmailRegistration();

        await waitFor(() => {
            expect(screen.getByRole('button', { name: /Registered/ })).toBeTruthy();
            expect(screen.getAllByText(/Hub connected/).length).toBeGreaterThan(0);
            expect(screen.getByText('Hub connected')).toBeTruthy();
        });
    });

    it('shows backend registration error and clears busy state', async () => {
        ActivateRemoteMock.mockRejectedValue(new Error('boom'));

        render(<OnboardingWizard {...baseProps} />);

        await continueRegistrationIdentity();
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        await confirmEmailRegistration();

        await waitFor(() => {
            expect(screen.getByText(/boom/)).toBeTruthy();
        });
        expect(screen.getByRole('alert').textContent).toMatch(/boom/);
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
        TestAndSaveMaclawLLMProvidersMock.mockResolvedValue({ message: 'hello', supports_vision: true });

        render(<OnboardingWizard {...baseProps} />);

        await continueRegistrationIdentity();
        fireEvent.click(screen.getByLabelText(/Free trial/));
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        await confirmEmailRegistration();

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
            expect(TestAndSaveMaclawLLMProvidersMock).toHaveBeenCalledWith(
                [expect.objectContaining({ name: 'Custom1', url: 'https://api.example.com/v1', key: 'secret', model: 'gpt-test' })],
                'Custom1',
                'Custom1',
            );
        });

        await waitFor(() => {
            expect(TestAndSaveMaclawLLMProvidersMock).toHaveBeenCalledTimes(1);
        });

        expect(baseProps.onLLMConfigured).toHaveBeenCalledTimes(1);
        expect(screen.getByText(/Scan to bind WeChat/)).toBeTruthy();
    });

    it('uses the dedicated xAI OAuth flow during onboarding', async () => {
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true });
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                { name: 'xAI-Grok', url: 'https://api.x.ai/v1', key: '', model: 'grok-4.5', protocol: 'openai', auth_type: 'oauth', wire_api: 'responses' },
            ],
        });
        StartXAIOAuthMock.mockResolvedValue('xAI-Grok OAuth login successful');

        render(<OnboardingWizard {...baseProps} />);

        await continueRegistrationIdentity();
        fireEvent.click(screen.getByLabelText(/Free trial/));
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        await confirmEmailRegistration();
        await waitFor(() => {
            expect(screen.getByText(/Registration successful/)).toBeTruthy();
        });
        fireEvent.click(screen.getByRole('button', { name: 'Next' }));

        fireEvent.click(await screen.findByRole('button', { name: 'xAI-Grok' }));
        expect(screen.getByText(/authorize with your xAI account/i)).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Sign in with xAI' }));

        await waitFor(() => {
            expect(StartXAIOAuthMock).toHaveBeenCalledTimes(1);
        });
        expect(StartOpenAIOAuthMock).not.toHaveBeenCalled();
        await waitFor(() => expect(baseProps.onLLMConfigured).toHaveBeenCalledTimes(1));
    });

    it('does not save when llm detection fails in step 3', async () => {
        ActivateRemoteMock.mockResolvedValue({ vip_flag: true });
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                { name: 'Custom1', url: '', key: '', model: '', protocol: 'openai', is_custom: true, supports_vision: false },
            ],
        });
        TestAndSaveMaclawLLMProvidersMock.mockRejectedValue(new Error('boom'));

        render(<OnboardingWizard {...baseProps} />);

        await continueRegistrationIdentity();
        fireEvent.click(screen.getByLabelText(/Free trial/));
        fireEvent.click(screen.getByRole('button', { name: 'Register' }));
        await confirmEmailRegistration();

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
            expect(TestAndSaveMaclawLLMProvidersMock).toHaveBeenCalled();
        });

        expect(SaveMaclawLLMProvidersMock).not.toHaveBeenCalled();
        expect(await screen.findByText(/boom/)).toBeTruthy();
        expect(baseProps.onLLMConfigured).not.toHaveBeenCalled();
    });
});
