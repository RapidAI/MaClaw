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
}));

import { AboutPanel } from '../AboutPanel';
import { ProbeRemoteHub } from '../../../wailsjs/go/main/App';

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

    it('hides thanks section when content is empty', () => {
        render(<AboutPanel {...baseProps} thanksContent="   " />);

        expect(screen.queryByText('Thanks')).toBeNull();
    });
});
