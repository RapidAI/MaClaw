// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { SidebarAiPane, shouldShowDigitalEmployeeMiddleTabs } from '../SidebarAiPane';

vi.mock('../SidebarToolSelector', () => ({ SidebarToolSelector: () => <div data-testid="tool-selector" /> }));
vi.mock('../SidebarRecentTasks', () => ({ SidebarRecentTasks: () => <div data-testid="recent-tasks" /> }));
vi.mock('../SidebarSystemStatus', () => ({ SidebarSystemStatus: () => <div data-testid="system-status" /> }));
vi.mock('../../ai/VirtualEmployeeTab', () => ({ VirtualEmployeeTab: () => <div data-testid="digital-employees" /> }));
vi.mock('../SidebarHistorySessions', () => ({ SidebarHistorySessions: () => <div data-testid="history-sessions" /> }));

const noop = vi.fn();

function renderPane(status: any) {
    render(
        <SidebarAiPane
            recentTasksPaneWidth={260}
            lang="en"
            aiThemeMode="light"
            maclawLLMOnline
            remoteActivationStatus={{}}
            qqBotStatus=""
            telegramStatus=""
            weixinStatus=""
            lansengerStatus=""
            config={{ group_discussion: { enabled: true } }}
            activeTool="codex"
            toolDropdownOpen={false}
            setToolDropdownOpen={noop}
            recentProjects={[]}
            renamingTaskPath={null}
            setRenamingTaskPath={noop}
            renameValue=""
            setRenameValue={noop}
            resumeRecentProject={noop}
            createRecentTask={noop}
            refreshRecentProjects={noop}
            taskContextMenu={null}
            setTaskContextMenu={noop}
            renameTask={async () => undefined}
            pinTask={async () => undefined}
            hideTask={async () => undefined}
            sidebarCurrentProviderTokenUsage={{ provider: '', isHubService: false, input: 0, output: 0, total: 0 }}
            sidebarHubCredits={null}
            formatSidebarTokens={(value) => String(value)}
            formatSidebarHubExpiry={() => ''}
            formatSidebarHubTotalCredits={() => ''}
            formatSidebarHubUsedCredits={() => ''}
            formatSidebarCredit={(value) => String(value)}
            unlimitedHubCreditText="Unlimited"
            noHubAuthorizationText="No auth"
            showHubCreditAction={false}
            openHubCreditsPage={noop}
            handleRecentTasksResizeStart={noop}
            isRecentTasksResizing={false}
            switchTool={noop}
            digitalEmployeeFeatureStatus={status}
        />,
    );
}

describe('SidebarAiPane digital employee tabs', () => {
    it('shows only recent tasks when feature status is hidden', () => {
        renderPane({ visible: false, reason: 'no_digital_employees', actual_count: 0 });

        expect(screen.getByTestId('recent-tasks')).toBeTruthy();
        expect(screen.queryByText('Digital')).toBeNull();
        expect(screen.queryByText('History')).toBeNull();
    });



    it('hides digital employee tabs when visible is stale but authorization is not usable', () => {
        expect(shouldShowDigitalEmployeeMiddleTabs({ visible: true, actual_count: 1, authorization: { active: false, quota: 1 } })).toBe(false);
        expect(shouldShowDigitalEmployeeMiddleTabs({ visible: true, actual_count: 1, authorization: { active: true, quota: 0 } })).toBe(false);
        expect(shouldShowDigitalEmployeeMiddleTabs({ visible: true, actual_count: 0, authorization: { active: true, quota: 1 } })).toBe(false);

        renderPane({ visible: true, actual_count: 1, authorization: { active: false, quota: 1 } });
        expect(screen.queryByText('Digital')).toBeNull();
        expect(screen.queryByText('History')).toBeNull();
    });

    it('shows digital employees and history tabs when feature status is visible', () => {
        renderPane({ visible: true, actual_count: 1, authorization: { active: true, quota: 1 } });

        fireEvent.click(screen.getByText('Digital'));
        expect(screen.getByTestId('digital-employees')).toBeTruthy();

        fireEvent.click(screen.getByText('History'));
        expect(screen.getByTestId('history-sessions')).toBeTruthy();
    });
});
