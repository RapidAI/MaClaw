/** @vitest-environment jsdom */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, cleanup, waitFor } from '@testing-library/react';

const BrowserOpenURLMock = vi.fn();

vi.mock('../../../wailsjs/runtime', () => ({
    BrowserOpenURL: (...args: unknown[]) => BrowserOpenURLMock(...args),
    EventsOn: vi.fn().mockReturnValue(() => {}),
}));

vi.mock('../../../wailsjs/go/main/App', () => ({
    ReadErrorLog: vi.fn().mockResolvedValue([]),
    ProbeRemoteHub: vi.fn().mockResolvedValue({}),
    GetRemoteRegistrationProfile: vi.fn().mockResolvedValue({}),
    GetHubUserRanking: vi.fn().mockResolvedValue({ error: 'hub not configured' }),
    SendRemoteRegistrationContactCode: vi.fn().mockResolvedValue({ ok: true, code_length: 6, expires_min: 5 }),
    VerifyRemoteRegistrationContactCode: vi.fn().mockResolvedValue({ ok: true }),
    PatchConfigFields: vi.fn().mockResolvedValue({}),
    CreateMobileAuthDesktopQRSession: vi.fn().mockResolvedValue({
        qr_payload: '{"v":2,"type":"maclaw_mobile_desktop_authorization","session_id":"maqr_test","hub_url":"https://hub.example"}',
        expires_at: '2026-07-05T12:00:00Z',
    }),
}));

import { AboutPanel } from '../AboutPanel';
import { CreateMobileAuthDesktopQRSession, GetHubUserRanking, GetRemoteRegistrationProfile, PatchConfigFields, ProbeRemoteHub, SendRemoteRegistrationContactCode, VerifyRemoteRegistrationContactCode } from '../../../wailsjs/go/main/App';

const baseProps = {
    currentIcon: '/logo.png',
    brandInfo: null,
    appVersion: '1.2.3',
    buildNumber: '10001',
    thanksContent: '',
    config: null,
    t: (key: string) => ({
        aboutProductName: 'MaClaw Bedrock',
        slogan: 'Master your code, seize the machine.',
        version: 'Version',
        buildLabel: 'Build',
        author: 'Author',
        businessCooperation: 'Contact: WeChat znsoft',
        aboutUnsetValue: 'Not configured',
        aboutIdentityTitle: 'Hub Identity',
        aboutIdentityDesc: 'Current tenant and registered instance on Hub.',
        aboutNotRegistered: 'Not registered',
        aboutRegisterBtn: 'Register',
        aboutClearBtn: 'Clear',
        aboutMobileAuthQRButton: 'Mobile Auth QR',
        aboutMobileAuthQRTitle: 'Mobile Authentication QR',
        aboutMobileAuthQRDesc: 'Scan with mobile app.',
        aboutMobileAuthQRExpiresAt: 'Expires at {time}',
        aboutMobileAuthQREmpty: 'Hub did not return a QR payload.',
        aboutMobileAuthQRFailed: 'Failed to create mobile authentication QR code.',
        aboutRefreshQRBtn: 'Refresh QR',
        aboutRegisterHub: 'Register to Hub',
        aboutClearRegistration: 'Clear registration and re-register',
        aboutTenantName: 'Tenant',
        aboutRegisteredName: 'Registered Name',
        aboutHubUrl: 'Hub URL',
        aboutHubCenterUrl: 'Hub Center URL',
        aboutAccountEmail: 'Account',
        aboutRegisterPhone: 'Registered Phone',
        aboutRegisterEmail: 'Registered Email',
        aboutSetContactBtn: 'Set',
        aboutSetRegisterPhone: 'Set Registered Phone',
        aboutSetRegisterEmail: 'Set Registered Email',
        aboutContactDialogDesc: 'Complete and verify this registration detail through Hub.',
        aboutRegisterPhonePlaceholder: 'Enter phone number',
        aboutRegisterEmailPlaceholder: 'name@example.com',
        aboutVerifyCode: 'Verification Code',
        aboutVerifyCodePlaceholder: 'Enter code',
        aboutSendCodeBtn: 'Send Code',
        aboutVerifyAndSaveBtn: 'Verify and Save',
        aboutContactCodeSent: '{length}-digit code sent. It expires in {minutes} minutes.',
        aboutContactCodeFailed: 'Failed to send verification code.',
        aboutContactVerified: 'Verified and saved.',
        aboutContactVerifyFailed: 'Verification failed.',
        aboutContactErrorEmailAlreadyRegistered: 'This email is already registered in the current tenant.',
        aboutContactErrorEmailRoutedToAnotherHub: 'This email belongs to another Hub and cannot be bound in the current tenant.',
        aboutContactErrorEmailDomainNotAllowed: 'This email domain is not allowed for the current tenant.',
        aboutContactErrorEmailBindCheckFailed: 'Failed to confirm this email for the current tenant. Please try again later.',
        aboutContactErrorEmailLookupFailed: 'Failed to check whether this email is already registered. Please try again later.',
        aboutContactErrorEmailBindFailed: 'Failed to bind the email. Please try again later.',
        aboutContactErrorPhoneAlreadyRegistered: 'This phone number is already registered in the current tenant.',
        aboutContactErrorPhoneLookupFailed: 'Failed to check whether this phone number is already registered. Please try again later.',
        aboutContactErrorPhoneRouteCheckFailed: 'Failed to confirm this phone number for the current tenant. Please try again later.',
        aboutContactErrorPhoneBindFailed: 'Failed to bind the phone number. Please try again later.',
        aboutContactErrorPhoneUseSmsRegistration: 'Phone verification must use the registration SMS flow. Please send the code again.',
        aboutContactErrorInvalidCode: 'The verification code is incorrect or has expired.',
        aboutContactErrorVerifyLocked: 'Too many failed attempts. Please try again later.',
        aboutContactErrorMailNotConfigured: 'Email verification is not configured for the current tenant.',
        aboutContactErrorMailSendFailed: "Failed to send the email verification code. Please check the current tenant's email configuration.",
        aboutContactErrorCodeGenFailed: 'Failed to generate a verification code. Please try again later.',
        aboutContactErrorPhoneRegistrationDisabled: 'Phone registration is not enabled for the current tenant.',
        aboutContactErrorInvalidEmail: 'Please enter a valid email address.',
        aboutContactErrorInvalidPhoneNumber: 'Please enter a valid phone number.',
        aboutContactErrorInvalidSmsVerifyRequest: 'The SMS verification request is invalid. Please check the phone number or code.',
        aboutContactErrorSmsConfigUnavailable: 'SMS verification configuration is unavailable for the current tenant.',
        aboutContactErrorSmsLimitCheckFailed: 'Failed to check the SMS verification limit. Please try again later.',
        aboutContactErrorSmsSendFailed: "Failed to send the SMS verification code. Please check the current tenant's SMS configuration.",
        aboutContactErrorSmsCheckFailed: 'Failed to verify the SMS code. Please try again later.',
        aboutContactErrorSmsDailyLimitReached: 'The SMS verification limit has been reached. Please try again tomorrow.',
        aboutContactErrorRateLimited: 'Requests are too frequent. Please wait and try again.',
        aboutContactErrorMachineUnauthorized: 'The current machine registration is invalid. Please register again.',
        aboutContactErrorTenantMismatch: 'The current account does not belong to this tenant.',
        aboutContactErrorUserNotFound: 'The current registered user was not found. Please register again.',
        aboutContactErrorInvalidContactKind: 'Please choose email or phone number.',
        aboutMachineId: 'Machine ID',
        remoteActivation: 'Registration',
        remoteActivated: 'Registered',
        aboutTotalOnline: 'Online Time',
        aboutTotalTokens: 'Token Usage',
        aboutRankPrefix: '#',
        aboutRankSuffix: '',
        aboutPeriodMonthly: 'this month',
        quickActionsTitle: 'Quick Actions',
        quickActionsDesc: 'Open official resources, check updates, or report issues.',
        officialWebsite: 'Official Website',
        onlineUpdate: 'Online Update',
        installLog: 'View Log',
        memoryHealth: 'Memory Health',
        securityEvents: 'Security Events',
        errorLog: 'Error Log',
        errorLogTitle: 'Error Log',
        errorLogEmpty: 'No errors found in the log.',
        loading: 'Loading',
        cancel: 'Cancel',
        bugReport: 'Problem Feedback',
        codeRepository: 'Code Repository',
        thanks: 'Thanks',
    }[key] ?? key),
    onOpenWebsite: vi.fn(),
    onCheckUpdate: vi.fn(),
    onShowInstallLog: vi.fn(),
    onOpenBugReport: vi.fn(),
    onOpenGithub: vi.fn(),
    onRegister: vi.fn(),
    onClearRegistration: vi.fn(),
};

describe('AboutPanel', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        cleanup();
    });

    it('renders thanks content at the bottom when provided', () => {
        render(
            <AboutPanel
                {...baseProps}
                thanksContent={'感谢所有贡献者\n\n- Alice\n- Bob'}
            />,
        );

        expect(screen.getByText('Thanks')).toBeTruthy();
        expect(screen.getByText('感谢所有贡献者')).toBeTruthy();
        expect(screen.getByText('Alice')).toBeTruthy();
        expect(screen.getByText('Bob')).toBeTruthy();
    });

    it('opens markdown links through BrowserOpenURL', () => {
        render(
            <AboutPanel
                {...baseProps}
                thanksContent={'[thanks link](https://github.com/rapidaicoder/msg/blob/main/thanks.md)'}
            />,
        );

        fireEvent.click(screen.getByText('thanks link'));

        expect(BrowserOpenURLMock).toHaveBeenCalledWith('https://github.com/rapidaicoder/msg/blob/main/thanks.md');
    });

    it('calls the code repository action from the code repository button', () => {
        render(<AboutPanel {...baseProps} />);

        fireEvent.click(screen.getByText('Code Repository'));

        expect(baseProps.onOpenGithub).toHaveBeenCalledTimes(1);
    });

    it('renders MetaStaff about product name with the stylized 6 pattern', () => {
        render(
            <AboutPanel
                {...baseProps}
                brandInfo={{
                    id: 'metastaff',
                    displayName: 'MetaStaff',
                    displayNameCN: '智员',
                    slogan: 'Master your code, seize the machine.',
                    author: 'Dr. Daniel',
                    businessContact: 'Contact: WeChat znsoft',
                    websiteURL: 'https://maclaw.top',
                    githubURL: 'https://github.com/nicedoc/maclaw',
                    iconPath: 'build/appicon.png',
                }}
            />,
        );

        expect(screen.getByRole('heading', { name: '智员 6 破茧' })).toBeTruthy();
    });

    it('renders current tenant and registered hub instance name', () => {
        render(<AboutPanel {...baseProps} config={{ remote_tenant_name: 'Acme Team', remote_nickname: 'Build Desk', remote_hub_url: 'https://hub.example', remote_email: 'dev@example.com', remote_machine_id: 'm_123', remote_machine_token: 'mt_123' }} />);

        expect(screen.getByText('Acme Team')).toBeTruthy();
        expect(screen.getByText('Build Desk')).toBeTruthy();
        expect(screen.getByText('https://hub.example')).toBeTruthy();
    });

    it('shows setup actions for missing registration contact details and verifies email', async () => {
        const onRegistrationContactUpdated = vi.fn();
        render(<AboutPanel {...baseProps} config={{ remote_tenant_name: 'Acme Team', remote_nickname: 'Build Desk', remote_hub_url: 'https://hub.example', remote_machine_id: 'm_123', remote_machine_token: 'mt_123' }} onRegistrationContactUpdated={onRegistrationContactUpdated} />);

        const setButtons = screen.getAllByText('Set');
        expect(setButtons.length).toBeGreaterThanOrEqual(2);

        fireEvent.click(setButtons[1]);
        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'dev@example.com' } });
        fireEvent.click(screen.getByText('Send Code'));

        await waitFor(() => expect(SendRemoteRegistrationContactCode).toHaveBeenCalledWith('email', 'dev@example.com'));

        fireEvent.change(screen.getByPlaceholderText('Enter code'), { target: { value: '123456' } });
        fireEvent.click(screen.getByText('Verify and Save'));

        await waitFor(() => expect(VerifyRemoteRegistrationContactCode).toHaveBeenCalledWith('email', 'dev@example.com', '123456'));
        expect(onRegistrationContactUpdated).toHaveBeenCalledTimes(1);
    });

    it('does not refresh registration contact details when verification fails', async () => {
        const onRegistrationContactUpdated = vi.fn();
        vi.mocked(VerifyRemoteRegistrationContactCode).mockRejectedValueOnce(new Error('INVALID_VERIFY_CODE: bad code'));
        render(<AboutPanel {...baseProps} config={{ remote_tenant_name: 'Acme Team', remote_nickname: 'Build Desk', remote_hub_url: 'https://hub.example', remote_machine_id: 'm_123', remote_machine_token: 'mt_123' }} onRegistrationContactUpdated={onRegistrationContactUpdated} />);

        const setButtons = screen.getAllByText('Set');
        fireEvent.click(setButtons[1]);
        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'dev@example.com' } });
        fireEvent.click(screen.getByText('Send Code'));
        await screen.findByText('6-digit code sent. It expires in 5 minutes.');

        fireEvent.change(screen.getByPlaceholderText('Enter code'), { target: { value: '000000' } });
        fireEvent.click(screen.getByText('Verify and Save'));

        expect(await screen.findByText('The verification code is incorrect or has expired.')).toBeTruthy();
        expect(onRegistrationContactUpdated).not.toHaveBeenCalled();
    });

    it('localizes registration contact backend error codes', async () => {
        vi.mocked(SendRemoteRegistrationContactCode).mockRejectedValueOnce(new Error('EMAIL_ALREADY_REGISTERED: Email is already registered'));
        render(<AboutPanel {...baseProps} config={{ remote_tenant_name: 'Acme Team', remote_nickname: 'Build Desk', remote_hub_url: 'https://hub.example', remote_machine_id: 'm_123', remote_machine_token: 'mt_123' }} />);

        const setButtons = screen.getAllByText('Set');
        fireEvent.click(setButtons[1]);
        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'dev@example.com' } });
        fireEvent.click(screen.getByText('Send Code'));

        expect(await screen.findByText('This email is already registered in the current tenant.')).toBeTruthy();
        expect(screen.queryByText(/EMAIL_ALREADY_REGISTERED/)).toBeNull();
    });

    it('localizes email route errors from the current tenant check', async () => {
        vi.mocked(SendRemoteRegistrationContactCode).mockRejectedValueOnce(new Error('EMAIL_ROUTED_TO_ANOTHER_HUB: hub_other'));
        render(<AboutPanel {...baseProps} config={{ remote_tenant_name: 'Acme Team', remote_nickname: 'Build Desk', remote_hub_url: 'https://hub.example', remote_machine_id: 'm_123', remote_machine_token: 'mt_123' }} />);

        const setButtons = screen.getAllByText('Set');
        fireEvent.click(setButtons[1]);
        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'dev@example.com' } });
        fireEvent.click(screen.getByText('Send Code'));

        expect(await screen.findByText('This email belongs to another Hub and cannot be bound in the current tenant.')).toBeTruthy();
        expect(screen.queryByText(/EMAIL_ROUTED_TO_ANOTHER_HUB/)).toBeNull();
    });

    it('localizes email verification delivery failures', async () => {
        vi.mocked(SendRemoteRegistrationContactCode).mockRejectedValueOnce(new Error('MAIL_SEND_FAILED: smtp rejected recipient'));
        render(<AboutPanel {...baseProps} config={{ remote_tenant_name: 'Acme Team', remote_nickname: 'Build Desk', remote_hub_url: 'https://hub.example', remote_machine_id: 'm_123', remote_machine_token: 'mt_123' }} />);

        const setButtons = screen.getAllByText('Set');
        fireEvent.click(setButtons[1]);
        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'dev@example.com' } });
        fireEvent.click(screen.getByText('Send Code'));

        expect(await screen.findByText("Failed to send the email verification code. Please check the current tenant's email configuration.")).toBeTruthy();
        expect(screen.queryByText(/MAIL_SEND_FAILED/)).toBeNull();
    });

    it('localizes phone contact SMS send failures', async () => {
        vi.mocked(SendRemoteRegistrationContactCode).mockRejectedValueOnce(new Error('SMS_VERIFY_SEND_FAILED: provider rejected template'));
        render(<AboutPanel {...baseProps} config={{ remote_tenant_name: 'Acme Team', remote_nickname: 'Build Desk', remote_hub_url: 'https://hub.example', remote_machine_id: 'm_123', remote_machine_token: 'mt_123' }} />);

        const setButtons = screen.getAllByText('Set');
        fireEvent.click(setButtons[0]);
        fireEvent.change(screen.getByPlaceholderText('Enter phone number'), { target: { value: '17090134628' } });
        fireEvent.click(screen.getByText('Send Code'));

        expect(await screen.findByText("Failed to send the SMS verification code. Please check the current tenant's SMS configuration.")).toBeTruthy();
        expect(screen.queryByText(/SMS_VERIFY_SEND_FAILED/)).toBeNull();
    });

    it('localizes local contact validation failures', async () => {
        vi.mocked(SendRemoteRegistrationContactCode).mockRejectedValueOnce(new Error('INVALID_EMAIL: valid email is required'));
        render(<AboutPanel {...baseProps} config={{ remote_tenant_name: 'Acme Team', remote_nickname: 'Build Desk', remote_hub_url: 'https://hub.example', remote_mobile: '17090134628', remote_machine_id: 'm_123', remote_machine_token: 'mt_123' } as any} />);

        fireEvent.click(screen.getByText('Set'));
        fireEvent.change(screen.getByPlaceholderText('name@example.com'), { target: { value: 'not-an-email' } });
        fireEvent.click(screen.getByText('Send Code'));

        expect(await screen.findByText('Please enter a valid email address.')).toBeTruthy();
        expect(screen.queryByText(/INVALID_EMAIL/)).toBeNull();
    });

    it('treats phone account identity as registered phone, not registered email', () => {
        render(<AboutPanel {...baseProps} config={{ remote_tenant_name: 'Acme Team', remote_nickname: 'Build Desk', remote_hub_url: 'https://hub.example', remote_email: 'phone:19900001111', remote_machine_id: 'm_123', remote_machine_token: 'mt_123' }} />);

        expect(screen.getByText('19900001111')).toBeTruthy();
        expect(screen.queryByText('phone:19900001111')).toBeNull();
        expect(screen.getAllByText('Set').length).toBeGreaterThanOrEqual(1);
    });

    it('shows mobile auth QR action before clear for registered phone accounts', async () => {
        render(<AboutPanel {...baseProps} config={{ remote_tenant_name: 'Acme Team', remote_nickname: 'Build Desk', remote_hub_url: 'https://hub.example', remote_email: 'phone:19900001111', remote_machine_id: 'm_123', remote_machine_token: 'mt_123' }} />);

        const mobileButton = screen.getByText('Mobile Auth QR');
        const clearButton = screen.getByText('Clear');
        expect(mobileButton.compareDocumentPosition(clearButton) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

        fireEvent.click(mobileButton);

        await waitFor(() => expect(CreateMobileAuthDesktopQRSession).toHaveBeenCalledTimes(1));
        expect(await screen.findByText('Mobile Authentication QR')).toBeTruthy();
        expect(screen.getByText('Expires at 2026-07-05T12:00:00Z')).toBeTruthy();
    });

    it('hides mobile auth QR action when registered account has no phone', () => {
        render(<AboutPanel {...baseProps} config={{ remote_tenant_name: 'Acme Team', remote_nickname: 'Build Desk', remote_hub_url: 'https://hub.example', remote_email: 'dev@example.com', remote_machine_id: 'm_123', remote_machine_token: 'mt_123' }} />);

        expect(screen.queryByText('Mobile Auth QR')).toBeNull();
    });

    it('hydrates missing registered phone from logged-in hub profile before public probe', async () => {
        vi.mocked(GetRemoteRegistrationProfile).mockResolvedValueOnce({ tenant_name: 'Acme Team', phone_number: '17090134628' });

        render(<AboutPanel {...baseProps} config={{ remote_nickname: 'Build Desk', remote_hub_url: 'https://hub.example', remote_email: 'dev@example.com', remote_machine_id: 'm_123', remote_machine_token: 'mt_123' }} />);

        expect(await screen.findByText('17090134628')).toBeTruthy();
        await waitFor(() => expect(GetRemoteRegistrationProfile).toHaveBeenCalledTimes(1));
        expect(ProbeRemoteHub).not.toHaveBeenCalled();
    });

    it('hydrates missing registered phone from hub probe for email accounts', async () => {
        vi.mocked(ProbeRemoteHub).mockResolvedValueOnce({ tenant_name: 'Acme Team', phone_number: '17090134628' });
        render(<AboutPanel {...baseProps} config={{ remote_tenant_name: 'Acme Team', remote_nickname: 'Build Desk', remote_hub_url: 'https://hub.example', remote_email: 'dev@example.com', remote_machine_id: 'm_123', remote_machine_token: 'mt_123' }} />);

        expect(await screen.findByText('17090134628')).toBeTruthy();
        await waitFor(() => expect(PatchConfigFields).toHaveBeenCalledWith({ remote_mobile: '17090134628' }));
    });

    it('probes tenant metadata with a phone account identity', async () => {
        render(<AboutPanel {...baseProps} config={{ remote_hub_url: 'https://hub.example', remote_email: 'phone:19900001111', remote_machine_id: 'm_123', remote_machine_token: 'mt_123' }} />);

        await waitFor(() => expect(ProbeRemoteHub).toHaveBeenCalledWith('https://hub.example', 'phone:19900001111'));
    });

    it('shows monthly ranking rows for a newly registered user with zero usage', async () => {
        vi.mocked(GetHubUserRanking).mockResolvedValueOnce({
            total_tokens: 0,
            duration_seconds: 0,
            token_rank: 0,
            duration_rank: 0,
            total_users: 0,
        });

        render(<AboutPanel {...baseProps} config={{ remote_tenant_name: 'Acme Team', remote_nickname: 'Build Desk', remote_hub_url: 'https://hub.example', remote_email: 'dev@example.com', remote_machine_id: 'm_123', remote_machine_token: 'mt_123' }} />);

        await waitFor(() => {
            expect(GetHubUserRanking).toHaveBeenCalled();
        });
        expect(screen.getByText((_, element) => element?.tagName === 'DT' && element.textContent?.includes('Online Time') === true)).toBeTruthy();
        expect(screen.getByText((_, element) => element?.tagName === 'DT' && element.textContent?.includes('Token Usage') === true)).toBeTruthy();
    });

    it('uses saved machine name when hub nickname is not available', () => {
        render(<AboutPanel {...baseProps} config={{ remote_tenant_id: 'tenant_acme', remote_machine_name: 'workstation-01', remote_machine_id: 'm_123', remote_machine_token: 'mt_123' }} />);

        expect(screen.getByText('tenant_acme')).toBeTruthy();
        expect(screen.getByText('workstation-01')).toBeTruthy();
    });

    it('does not show stale hub nickname when machine is not registered', () => {
        render(<AboutPanel {...baseProps} config={{ remote_tenant_name: 'Acme Team', remote_nickname: 'Old Desk' }} />);

        expect(screen.getByText('Acme Team')).toBeTruthy();
        expect(screen.queryByText('Old Desk')).toBeNull();
        expect(screen.getAllByText('Not configured').length).toBeGreaterThan(0);
    });

    it('does not show registered state when machine token is missing', () => {
        render(<AboutPanel {...baseProps} config={{ remote_tenant_name: 'Acme Team', remote_nickname: 'Old Desk', remote_machine_id: 'm_123' }} />);

        expect(screen.getAllByText('Not registered').length).toBeGreaterThan(0);
        expect(screen.queryByText('Old Desk')).toBeNull();
    });

    it('refreshes probed tenant when hub identity changes', async () => {
        vi.mocked(ProbeRemoteHub)
            .mockResolvedValueOnce({ tenant_name: 'Team One' })
            .mockResolvedValueOnce({ tenant_name: 'Team Two' });

        const { rerender } = render(<AboutPanel {...baseProps} config={{ remote_hub_url: 'https://hub-one.example', remote_email: 'dev@example.com' }} />);

        expect(await screen.findByText('Team One')).toBeTruthy();

        rerender(<AboutPanel {...baseProps} config={{ remote_hub_url: 'https://hub-two.example', remote_email: 'dev@example.com' }} />);

        await waitFor(() => {
            expect(ProbeRemoteHub).toHaveBeenLastCalledWith('https://hub-two.example', 'dev@example.com');
        });
        expect(await screen.findByText('Team Two')).toBeTruthy();
    });

    it('refreshes probed tenant when phone registration identity changes', async () => {
        vi.mocked(ProbeRemoteHub)
            .mockResolvedValueOnce({ tenant_name: 'Phone Team One' })
            .mockResolvedValueOnce({ tenant_name: 'Phone Team Two' });

        const { rerender } = render(<AboutPanel {...baseProps} config={{ remote_hub_url: 'https://hub.example', remote_mobile: '19900001111' } as any} />);

        expect(await screen.findByText('Phone Team One')).toBeTruthy();

        rerender(<AboutPanel {...baseProps} config={{ remote_hub_url: 'https://hub.example', remote_mobile: '19900002222' } as any} />);

        await waitFor(() => {
            expect(ProbeRemoteHub).toHaveBeenLastCalledWith('https://hub.example', 'phone:19900002222');
        });
        expect(await screen.findByText('Phone Team Two')).toBeTruthy();
    });

    it('hides thanks section when content is empty', () => {
        render(<AboutPanel {...baseProps} thanksContent="   " />);

        expect(screen.queryByText('Thanks')).toBeNull();
    });
});
