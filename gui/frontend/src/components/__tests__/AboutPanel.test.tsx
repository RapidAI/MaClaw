/** @vitest-environment jsdom */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, cleanup } from '@testing-library/react';

const BrowserOpenURLMock = vi.fn();

vi.mock('../../../wailsjs/runtime', () => ({
    BrowserOpenURL: (...args: unknown[]) => BrowserOpenURLMock(...args),
}));

vi.mock('../../../wailsjs/go/main/App', () => ({
    ReadErrorLog: vi.fn().mockResolvedValue([]),
}));

import { AboutPanel } from '../AboutPanel';

const baseProps = {
    currentIcon: '/logo.png',
    brandInfo: null,
    appVersion: '1.2.3',
    buildNumber: '10001',
    thanksContent: '',
    t: (key: string) => ({
        aboutProductName: 'MaClaw Bedrock',
        slogan: 'Master your code, seize the machine.',
        version: 'Version',
        buildLabel: 'Build',
        author: 'Author',
        businessCooperation: 'Contact: WeChat znsoft',
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

    it('hides thanks section when content is empty', () => {
        render(<AboutPanel {...baseProps} thanksContent="   " />);

        expect(screen.queryByText('Thanks')).toBeNull();
    });
});
