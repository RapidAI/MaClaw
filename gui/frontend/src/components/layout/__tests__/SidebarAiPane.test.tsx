// @vitest-environment jsdom
import { act, fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type React from 'react';
import { SidebarAiPane, isDigitalEmployeeAuthorizationUsable, shouldShowDigitalEmployeeMiddleTabs } from '../SidebarAiPane';

vi.mock('../SidebarToolSelector', () => ({ SidebarToolSelector: () => <div data-testid="tool-selector" /> }));
vi.mock('../SidebarTaskManagement', () => ({ SidebarTaskManagement: () => <div data-testid="task-management" /> }));
vi.mock('../SidebarSystemStatus', () => ({ SidebarSystemStatus: () => <div data-testid="system-status" /> }));
vi.mock('../../ai/VirtualEmployeeTab', () => ({ VirtualEmployeeTab: () => <div data-testid="digital-employees" /> }));
vi.mock('../SidebarHistorySessions', () => ({ SidebarHistorySessions: ({ enabled = true }: { enabled?: boolean }) => enabled ? <div data-testid="history-sessions" /> : null }));

const noop = vi.fn();

function renderPane(status: any, overrides: Partial<React.ComponentProps<typeof SidebarAiPane>> = {}) {
    render(
        <SidebarAiPane
            taskManagementPaneWidth={260}
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
            tasks={[]}
            renamingTaskPath={null}
            setRenamingTaskPath={noop}
            renameValue=""
            setRenameValue={noop}
            resumeTask={noop}
            continueWorkflowProject={noop}
            createTask={noop}
            refreshTasks={noop}
            taskContextMenu={null}
            setTaskContextMenu={noop}
            renameTask={async () => undefined}
            pinTask={async () => undefined}
            hideTask={async () => undefined}
            sidebarCurrentProviderTokenUsage={{ provider: '', isHubService: false, input: 0, output: 0, total: 0, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0 }}
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
            handleTaskManagementResizeStart={noop}
            isTaskManagementResizing={false}
            switchTool={noop}
            digitalEmployeeFeatureStatus={status}
            {...overrides}
        />,
    );
}

describe('SidebarAiPane digital employee tabs', () => {
    it('shows only task management when feature status is hidden', () => {
        renderPane({ visible: false, reason: 'no_digital_employees', actual_count: 0 });

        expect(screen.getByTestId('task-management')).toBeTruthy();
        expect(screen.queryByText('Digital')).toBeNull();
        expect(screen.queryByText('History')).toBeNull();
    });



    it('hides digital employee tabs when visible is stale but authorization is not usable', () => {
        expect(shouldShowDigitalEmployeeMiddleTabs({ visible: true, actual_count: 1, authorization: { active: false, quota: 1 } })).toBe(false);
        expect(shouldShowDigitalEmployeeMiddleTabs({ visible: true, actual_count: 1, authorization: { active: true, quota: 0 } })).toBe(false);
        expect(shouldShowDigitalEmployeeMiddleTabs({ visible: true, actual_count: 0, authorization: { active: true, quota: 1 } })).toBe(false);
        expect(shouldShowDigitalEmployeeMiddleTabs({ visible: true, actual_count: 1, authorization: { active: true, quota: 1, expires_at: '2026-05-15T00:00:00Z' } }, Date.parse('2026-05-16T00:00:00Z'))).toBe(false);
        expect(shouldShowDigitalEmployeeMiddleTabs({ visible: true, actual_count: 1, authorization: { active: true, quota: 1, expires_at: 'not-a-date' } }, Date.parse('2026-05-16T00:00:00Z'))).toBe(false);

        expect(shouldShowDigitalEmployeeMiddleTabs({ visible: true, actual_count: 1, authorization: { active: true, quota: 1 } })).toBe(false);

        renderPane({ visible: true, actual_count: 1, authorization: { active: false, quota: 1 } });
        expect(screen.queryByText('Digital')).toBeNull();
        expect(screen.queryByText('History')).toBeNull();
    });

    it('treats expired digital employee authorization as unusable even if active is stale', () => {
        expect(isDigitalEmployeeAuthorizationUsable({ active: true, quota: 1, expires_at: '2026-05-15T00:00:00Z' }, Date.parse('2026-05-16T00:00:00Z'))).toBe(false);
        expect(isDigitalEmployeeAuthorizationUsable({ active: true, quota: 1, expires_at: '2026-05-17T00:00:00Z' }, Date.parse('2026-05-16T00:00:00Z'))).toBe(true);
    });

    it('restores digital employee navigation when the app shell knows Hub entries are reachable', () => {
        renderPane({ visible: false, reason: 'no_digital_employees', actual_count: 0 }, { showDigitalEmployeeNavigation: true });

        fireEvent.click(screen.getByText('Digital Employees'));
        expect(screen.getByTestId('digital-employees')).toBeTruthy();

        fireEvent.click(screen.getByText('History'));
        expect(screen.getByTestId('history-sessions')).toBeTruthy();
    });

    it('keeps cached digital employee history visible when group discussion is disabled', () => {
        renderPane(
            { visible: false, reason: 'no_digital_employees', actual_count: 0 },
            { showDigitalEmployeeNavigation: true, config: { group_discussion: { enabled: false } } },
        );

        fireEvent.click(screen.getByText('History'));
        expect(screen.getByTestId('history-sessions')).toBeTruthy();
    });

    it('shows digital employees and history tabs when feature status is visible', () => {
        renderPane({ visible: true, actual_count: 1, authorization: { active: true, quota: 1, expires_at: '2999-01-01T00:00:00Z' } });

        fireEvent.click(screen.getByText('Digital Employees'));
        expect(screen.getByTestId('digital-employees')).toBeTruthy();

        fireEvent.click(screen.getByText('History'));
        expect(screen.getByTestId('history-sessions')).toBeTruthy();
    });

    it('keeps tab content in a flexible slot above system status', () => {
        renderPane({ visible: true, actual_count: 1, authorization: { active: true, quota: 1, expires_at: '2999-01-01T00:00:00Z' } });

        fireEvent.click(screen.getByText('History'));

        const contentSlot = screen.getByTestId('sidebar-ai-content-slot');
        expect(contentSlot.contains(screen.getByTestId('history-sessions'))).toBe(true);
        expect(contentSlot.contains(screen.getByTestId('system-status'))).toBe(false);
        expect(contentSlot.style.flex).toBe('1 1 0%');
        expect(contentSlot.style.minHeight).toBe('0px');
    });

    it('insets digital employee content so the pane does not press against the divider', () => {
        renderPane({ visible: true, actual_count: 1, authorization: { active: true, quota: 1, expires_at: '2999-01-01T00:00:00Z' } });

        fireEvent.click(screen.getByText('Digital Employees'));

        const middlePane = screen.getByTestId('sidebar-middle-pane-employees');
        expect(middlePane.style.paddingLeft).toBe('6px');
        expect(middlePane.style.paddingRight).toBe('6px');
        expect(middlePane.style.boxSizing).toBe('border-box');
    });

    it('insets history content with the same pane spacing', () => {
        renderPane({ visible: true, actual_count: 1, authorization: { active: true, quota: 1, expires_at: '2999-01-01T00:00:00Z' } });

        fireEvent.click(screen.getByText('History'));

        const middlePane = screen.getByTestId('sidebar-middle-pane-history');
        expect(middlePane.style.paddingLeft).toBe('6px');
        expect(middlePane.style.paddingRight).toBe('6px');
        expect(middlePane.style.boxSizing).toBe('border-box');
    });

    it('keeps task management mounted while other middle tabs are active', () => {
        renderPane({ visible: true, actual_count: 1, authorization: { active: true, quota: 1, expires_at: '2999-01-01T00:00:00Z' } });

        fireEvent.click(screen.getByText('Digital Employees'));
        // Hidden but still mounted so welcome coding-task events can open create dialog.
        expect(screen.getByTestId('task-management')).toBeTruthy();
        expect(screen.getByTestId('sidebar-middle-pane-tasks').style.display).toBe('none');
        expect(screen.getByTestId('digital-employees')).toBeTruthy();
    });

    it('switches back to tasks when a welcome coding-task event fires', () => {
        renderPane({ visible: true, actual_count: 1, authorization: { active: true, quota: 1, expires_at: '2999-01-01T00:00:00Z' } });

        fireEvent.click(screen.getByText('History'));
        expect(screen.getByTestId('history-sessions')).toBeTruthy();

        act(() => {
            window.dispatchEvent(new CustomEvent('ai-open-create-coding-task', {
                detail: { mode: 'coding_dev', name: 'Implement a feature' },
            }));
        });

        expect(screen.getByTestId('sidebar-middle-pane-tasks').style.display).not.toBe('none');
        expect(screen.queryByTestId('history-sessions')).toBeNull();
    });
});
