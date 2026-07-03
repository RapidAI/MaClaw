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
    GetHubUserRanking: vi.fn().mockResolvedValue({ error: 'hub not configured' }),
    SendRemoteRegistrationContactCode: vi.fn().mockResolvedValue({ ok: true, code_length: 6, expires_min: 5 }),
    VerifyRemoteRegistrationContactCode: vi.fn().mockResolvedValue({ ok: true }),
}));

import { AboutPanel } from '../AboutPanel';
import { GetHubUserRanking, ProbeRemoteHub, SendRemoteRegistrationContactCode, VerifyRemoteRegistrationContactCode } from '../../../wailsjs/go/main/App';

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
        aboutContactErrorPhoneAlreadyRegistered: 'This phone number is already registered in the current tenant.',
        aboutContactErrorInvalidCode: 'The verification code is incorrect or has expired.',
        aboutContactErrorVerifyLocked: 'Too many failed attempts. Please try again later.',
        aboutContactErrorMailNotConfigured: 'Email verification is not configured for the current tenant.',
        aboutContactErrorSmsDailyLimitReached: 'The SMS verification limit has been reached. Please try again tomorrow.',
        aboutContactErrorRateLimited: 'Requests are too frequent. Please wait and try again.',
        aboutContactErrorMachineUnauthorized: 'The current machine registration is invalid. Please register again.',
        aboutContactErrorTenantMismatch: 'The current account does not belong to this tenant.',
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

        expect(screen.getByRole('heading', { name: '智员 6 程启' })).toBeTruthy();
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

    it('treats phone account identity as registered phone, not registered email', () => {
        render(<AboutPanel {...baseProps} config={{ remote_tenant_name: 'Acme Team', remote_nickname: 'Build Desk', remote_hub_url: 'https://hub.example', remote_email: 'phone:19900001111', remote_machine_id: 'm_123', remote_machine_token: 'mt_123' }} />);

        expect(screen.getByText('19900001111')).toBeTruthy();
        expect(screen.queryByText('phone:19900001111')).toBeNull();
        expect(screen.getAllByText('Set').length).toBeGreaterThanOrEqual(1);
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
