// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { SidebarSystemStatus } from '../SidebarSystemStatus';
import type { SidebarHubCredits } from '../../../types/appShell';

const baseCredits: SidebarHubCredits = {
    authorized: true,
    total: 100,
    used: 10,
    remaining: 90,
    tokensPerCredit: 0,
    expiresAt: '2026-05-06T00:00:00Z',
    unlimited: false,
    status: '',
    retryAfterSeconds: 0,
    retryAfterAt: '',
};

function renderStatus(credits: SidebarHubCredits) {
    render(
        <SidebarSystemStatus
            lang="zh-Hans"
            maclawLLMOnline={false}
            remoteActivationStatus={{ activated: false }}
            qqBotStatus=""
            telegramStatus=""
            weixinStatus=""
            lansengerStatus=""
            sidebarCurrentProviderTokenUsage={{ provider: 'MaClaw\u5b98\u65b9', isHubService: true, input: 0, output: 0, total: 0, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0 }}
            sidebarHubCredits={credits}
            formatSidebarTokens={(value) => String(value)}
            formatSidebarHubExpiry={() => '05/06/26'}
            formatSidebarHubTotalCredits={(value) => String(value?.total ?? 0)}
            formatSidebarHubUsedCredits={(value) => String(value?.used ?? 0)}
            formatSidebarCredit={(value) => String(value)}
            unlimitedHubCreditText="\u65e0\u9650"
            noHubAuthorizationText="\u65e0"
            showHubCreditAction={false}
            openHubCreditsPage={vi.fn()}
        />,
    );
}

describe('SidebarSystemStatus Hub credits', () => {
    it('shows period limit state with recovery time instead of remaining credits', () => {
        renderStatus({ ...baseCredits, status: 'period_limited', retryAfterSeconds: 3600 });

        expect(screen.getByText(/\u5468\u671f\u9650\u6d41/)).toBeTruthy();
        expect(screen.getByText(/\u7ea6 1 \u5c0f\u65f6\u540e\u6062\u590d/)).toBeTruthy();
        expect(screen.queryByText('90')).toBeNull();
    });

    it('shows queued state with activation time instead of remaining credits', () => {
        renderStatus({ ...baseCredits, status: 'queued', retryAfterSeconds: 7200 });

        expect(screen.getByText(/\u5f85\u751f\u6548/)).toBeTruthy();
        expect(screen.getByText(/\u7ea6 2 \u5c0f\u65f6\u540e\u751f\u6548/)).toBeTruthy();
        expect(screen.queryByText('90')).toBeNull();
    });

    it('shows expired state instead of remaining credits', () => {
        renderStatus({ ...baseCredits, status: 'expired' });

        expect(screen.getByText(/\u6388\u6743\u5df2\u8fc7\u671f/)).toBeTruthy();
        expect(screen.queryByText('90')).toBeNull();
    });

    it('shows prompt cache hit rate beside token usage', () => {
        render(
            <SidebarSystemStatus
                lang="en"
                maclawLLMOnline
                remoteActivationStatus={{ activated: true }}
                qqBotStatus=""
                telegramStatus=""
                weixinStatus=""
                lansengerStatus=""
                sidebarCurrentProviderTokenUsage={{ provider: 'MaClaw', isHubService: false, input: 100, output: 20, total: 120, cachedInput: 40, cacheWrite: 30, requests: 5, cachedRequests: 2 }}
                sidebarHubCredits={baseCredits}
                formatSidebarTokens={(value) => `${value}`}
                formatSidebarHubExpiry={() => '05/06/26'}
                formatSidebarHubTotalCredits={(value) => String(value?.total ?? 0)}
                formatSidebarHubUsedCredits={(value) => String(value?.used ?? 0)}
                formatSidebarCredit={(value) => String(value)}
                unlimitedHubCreditText="unlimited"
                noHubAuthorizationText="none"
                showHubCreditAction={false}
                openHubCreditsPage={vi.fn()}
            />,
        );

        expect(screen.getByText(/cache 40%/)).toBeTruthy();
        const cacheTitles = screen.getAllByTitle(/Cache hit: 40%/);
        expect(cacheTitles[0].getAttribute('title')).toContain('Read 40');
        expect(cacheTitles[0].getAttribute('title')).toContain('Write 30');
    });

    it('shows ssh background task count immediately after IM status', () => {
        render(
            <SidebarSystemStatus
                lang="zh-Hans"
                maclawLLMOnline
                remoteActivationStatus={{ activated: true }}
                qqBotStatus=""
                telegramStatus=""
                weixinStatus=""
                lansengerStatus=""
                sshBackgroundTaskCount={3}
                sidebarCurrentProviderTokenUsage={{ provider: 'MaClaw', isHubService: false, input: 0, output: 0, total: 12, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0 }}
                sidebarHubCredits={baseCredits}
                formatSidebarTokens={(value) => String(value)}
                formatSidebarHubExpiry={() => '05/06/26'}
                formatSidebarHubTotalCredits={(value) => String(value?.total ?? 0)}
                formatSidebarHubUsedCredits={(value) => String(value?.used ?? 0)}
                formatSidebarCredit={(value) => String(value)}
                unlimitedHubCreditText="\u65e0\u9650"
                noHubAuthorizationText="\u65e0"
                showHubCreditAction={false}
                openHubCreditsPage={vi.fn()}
            />,
        );

        const signals = screen.getByLabelText('System status');
        expect(signals.textContent).toContain('\u540e\u53f0\u4efb\u52a1\uff1a 3');
        expect(signals.textContent?.indexOf('IM')).toBeLessThan(signals.textContent?.indexOf('\u540e\u53f0\u4efb\u52a1\uff1a 3') ?? -1);
        expect(signals.textContent?.indexOf('HUB')).toBeLessThan(signals.textContent?.indexOf('IM') ?? -1);
    });

    it('shows the active coding agent task status in the sidebar monitor', () => {
        render(
            <SidebarSystemStatus
                lang="en"
                maclawLLMOnline
                remoteActivationStatus={{ activated: true }}
                qqBotStatus=""
                telegramStatus=""
                weixinStatus=""
                lansengerStatus=""
                sidebarCurrentProviderTokenUsage={{ provider: 'MaClaw', isHubService: false, input: 0, output: 0, total: 12, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0 }}
                sidebarHubCredits={baseCredits}
                formatSidebarTokens={(value) => String(value)}
                formatSidebarHubExpiry={() => '05/06/26'}
                formatSidebarHubTotalCredits={(value) => String(value?.total ?? 0)}
                formatSidebarHubUsedCredits={(value) => String(value?.used ?? 0)}
                formatSidebarCredit={(value) => String(value)}
                unlimitedHubCreditText="unlimited"
                noHubAuthorizationText="none"
                showHubCreditAction={false}
                openHubCreditsPage={vi.fn()}
                codingAgentProgress={{ phase: 'running', taskID: 'T2', title: 'Fix stale edit guard' }}
            />,
        );

        const status = screen.getByTestId('sidebar-coding-agent-status');
        expect(status.textContent).toContain('Coding Agent');
        expect(status.textContent).toContain('Task status');
        expect(status.textContent).toContain('Running');
        expect(status.textContent).toContain('T2');
        expect(status.textContent).toContain('Fix stale edit guard');
        expect(status.getAttribute('role')).toBe('status');
        expect(status.getAttribute('aria-live')).toBe('polite');
        expect(status.getAttribute('aria-label')).toContain('Fix stale edit guard');
    });

    it('shows coding agent turn details in the sidebar monitor card', () => {
        render(
            <SidebarSystemStatus
                lang="en"
                maclawLLMOnline
                remoteActivationStatus={{ activated: true }}
                qqBotStatus=""
                telegramStatus=""
                weixinStatus=""
                lansengerStatus=""
                sidebarCurrentProviderTokenUsage={{ provider: 'MaClaw', isHubService: false, input: 0, output: 0, total: 12, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0 }}
                sidebarHubCredits={baseCredits}
                formatSidebarTokens={(value) => String(value)}
                formatSidebarHubExpiry={() => '05/06/26'}
                formatSidebarHubTotalCredits={(value) => String(value?.total ?? 0)}
                formatSidebarHubUsedCredits={(value) => String(value?.used ?? 0)}
                formatSidebarCredit={(value) => String(value)}
                unlimitedHubCreditText="unlimited"
                noHubAuthorizationText="none"
                showHubCreditAction={false}
                openHubCreditsPage={vi.fn()}
                codingAgentProgress={{ phase: 'result', taskID: 'T2', title: 'Fix stale edit guard', event: 'diff_summary', detail: '2 files', count: 2, files: ['a.go', 'b.go'] }}
                codingAgentTurnSnapshot={{
                    latest: { phase: 'result', taskID: 'T2', title: 'Fix stale edit guard', event: 'diff_summary', detail: '2 files', count: 2, files: ['a.go', 'b.go'] },
                    turnID: 'turn-2',
                    taskID: 'T2',
                    title: 'Fix stale edit guard',
                    phase: 'result',
                    tool: 'git_diff',
                    toolOutcome: 'success',
                    toolDurationMs: 1250,
                    tools: [
                        { name: 'read_file', outcome: 'success', durationMs: 80 },
                        { name: 'git_diff', outcome: 'success', durationMs: 1250 },
                    ],
                    guardrailStatus: 'blocked',
                    guardrailSummary: 'blocked | bash | category:git',
                    guardrailCount: 1,
                    commandStatus: 'failed',
                    commandSummary: '2 bash commands run, 1 failed: npm test',
                    commandCount: 2,
                    fileActivityStatus: 'changed',
                    fileActivitySummary: 'read 2 / modified 1 / created 1; changed: a.go, b.go',
                    fileActivityCount: 4,
                    fileActivityDetail: 'read 2 / modified 1 / created 1',
                    qualityStatus: 'warning',
                    qualitySummary: 'verification not run',
                    qualityCount: 1,
                    explorationStatus: 'explored',
                    explorationSummary: 'searched before editing',
                    explorationCount: 2,
                    verificationStatus: 'passed',
                    verificationSummary: 'go test ./gui passed',
                    verificationCount: 1,
                    diffCheckStatus: 'checked',
                    diffCheckSummary: 'diff --git a/a.go b/a.go',
                    changeCount: 2,
                    files: ['a.go', 'b.go'],
                    diffSummary: '2 files',
                }}
            />,
        );

        const card = screen.getByTestId('sidebar-coding-agent-card');
        expect(card.getAttribute('role')).toBe('group');
        expect(card.getAttribute('aria-label')).toContain('Tool: git_diff');
        expect(card.getAttribute('aria-label')).toContain('Files: a.go, b.go');
        expect(card.getAttribute('title')).toContain('Diff: 2 files');
        expect(card.getAttribute('data-turn-id')).toBe('turn-2');
        expect(card.getAttribute('data-tool')).toBe('git_diff');
        expect(card.getAttribute('data-tool-outcome')).toBe('success');
        expect(card.getAttribute('data-tool-outcome-state')).toBe('success');
        expect(card.getAttribute('data-tool-duration-ms')).toBe('1250');
        expect(card.getAttribute('data-tool-count')).toBe('2');
        expect(card.getAttribute('data-guardrail-status')).toBe('blocked');
        expect(card.getAttribute('data-guardrail-state')).toBe('blocked');
        expect(card.getAttribute('data-guardrail-count')).toBe('1');
        expect(card.getAttribute('data-command-status')).toBe('failed');
        expect(card.getAttribute('data-command-state')).toBe('failed');
        expect(card.getAttribute('data-command-count')).toBe('2');
        expect(card.getAttribute('data-file-activity-status')).toBe('changed');
        expect(card.getAttribute('data-file-activity-state')).toBe('changed');
        expect(card.getAttribute('data-file-activity-count')).toBe('4');
        expect(card.getAttribute('data-quality-status')).toBe('warning');
        expect(card.getAttribute('data-quality-state')).toBe('warning');
        expect(card.getAttribute('data-quality-count')).toBe('1');
        expect(card.getAttribute('data-exploration-status')).toBe('explored');
        expect(card.getAttribute('data-exploration-state')).toBe('explored');
        expect(card.getAttribute('data-exploration-count')).toBe('2');
        expect(card.getAttribute('data-verification-status')).toBe('passed');
        expect(card.getAttribute('data-verification-state')).toBe('passed');
        expect(card.getAttribute('data-verification-count')).toBe('1');
        expect(card.getAttribute('data-diff-check-status')).toBe('checked');
        expect(card.getAttribute('data-diff-check-state')).toBe('checked');
        expect(card.getAttribute('data-change-count')).toBe('2');
        expect(card.getAttribute('data-file-count')).toBe('2');
        expect(card.textContent).toContain('Files');
        expect(card.textContent).toContain('a.go, b.go');
        expect(card.textContent).toContain('Trace');
        expect(card.textContent).toContain('read_file Success 80ms -> git_diff Success 1.3s');
        const trace = screen.getByTestId('sidebar-coding-agent-tool-trace');
        expect(trace.getAttribute('aria-label')).toBe('read_file Success 80ms -> git_diff Success 1.3s');
        expect(trace.querySelector('[data-tool-trace-name="read_file"]')?.getAttribute('data-tool-trace-outcome-state')).toBe('success');
        expect(trace.querySelector('[data-tool-trace-name="git_diff"]')?.getAttribute('data-tool-trace-outcome-state')).toBe('success');
        expect(card.textContent).toContain('Tool');
        expect(card.textContent).toContain('git_diff');
        expect(card.textContent).toContain('Result');
        expect(card.textContent).toContain('Success');
        expect(card.textContent).toContain('Duration');
        expect(card.textContent).toContain('1.3s');
        expect(card.textContent).toContain('Guard');
        expect(card.textContent).toContain('Blocked (1)');
        expect(screen.getByTestId('sidebar-coding-agent-guardrail').getAttribute('data-guardrail-summary')).toBe('blocked | bash | category:git');
        expect(card.textContent).toContain('Commands');
        expect(card.textContent).toContain('Failed (2)');
        expect(screen.getByTestId('sidebar-coding-agent-commands').getAttribute('data-command-summary')).toBe('2 bash commands run, 1 failed: npm test');
        expect(card.textContent).toContain('Activity');
        expect(card.textContent).toContain('Changed (read 2 / modified 1 / created 1)');
        expect(screen.getByTestId('sidebar-coding-agent-file-activity').getAttribute('data-file-activity-summary')).toBe('read 2 / modified 1 / created 1; changed: a.go, b.go');
        expect(screen.getByTestId('sidebar-coding-agent-file-activity').getAttribute('data-file-activity-detail')).toBe('read 2 / modified 1 / created 1');
        expect(card.textContent).toContain('Quality');
        expect(card.textContent).toContain('Warning (1)');
        expect(screen.getByTestId('sidebar-coding-agent-quality').getAttribute('data-quality-summary')).toBe('verification not run');
        expect(card.textContent).toContain('Explore');
        expect(card.textContent).toContain('Explored (2)');
        expect(screen.getByTestId('sidebar-coding-agent-exploration').getAttribute('data-exploration-summary')).toBe('searched before editing');
        expect(card.textContent).toContain('Verify');
        expect(card.textContent).toContain('Passed (1)');
        expect(screen.getByTestId('sidebar-coding-agent-verification').getAttribute('data-verification-summary')).toBe('go test ./gui passed');
        expect(card.textContent).toContain('Diff check');
        expect(card.textContent).toContain('Checked');
        expect(screen.getByTestId('sidebar-coding-agent-diff-check').getAttribute('data-diff-check-summary')).toBe('diff --git a/a.go b/a.go');
        expect(card.textContent).toContain('Diff');
        expect(card.textContent).toContain('2 files');
        expect(card.className).toContain('coding-agent-turn-card--success');
    });

    it('shows blocked tool outcome as a semantic sidebar badge', () => {
        render(
            <SidebarSystemStatus
                lang="en"
                maclawLLMOnline
                remoteActivationStatus={{ activated: true }}
                qqBotStatus=""
                telegramStatus=""
                weixinStatus=""
                lansengerStatus=""
                sidebarCurrentProviderTokenUsage={{ provider: 'MaClaw', isHubService: false, input: 0, output: 0, total: 12, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0 }}
                sidebarHubCredits={baseCredits}
                formatSidebarTokens={(value) => String(value)}
                formatSidebarHubExpiry={() => '05/06/26'}
                formatSidebarHubTotalCredits={(value) => String(value?.total ?? 0)}
                formatSidebarHubUsedCredits={(value) => String(value?.used ?? 0)}
                formatSidebarCredit={(value) => String(value)}
                unlimitedHubCreditText="unlimited"
                noHubAuthorizationText="none"
                showHubCreditAction={false}
                openHubCreditsPage={vi.fn()}
                codingAgentProgress={{ phase: 'running', taskID: 'T3', title: 'Guard command', event: 'tool_finished', detail: 'bash', outcome: 'blocked' }}
                codingAgentTurnSnapshot={{
                    latest: { phase: 'running', taskID: 'T3', title: 'Guard command', event: 'tool_finished', detail: 'bash', outcome: 'blocked' },
                    turnID: 'turn-3',
                    taskID: 'T3',
                    title: 'Guard command',
                    phase: 'running',
                    tool: 'bash',
                    toolOutcome: 'blocked',
                    tools: [{ name: 'bash', outcome: 'blocked', summary: 'command refused' }],
                    commandStatus: 'none',
                    commandCount: 0,
                    qualityStatus: 'passed',
                    qualityCount: 0,
                }}
            />,
        );

        const card = screen.getByTestId('sidebar-coding-agent-card');
        expect(card.getAttribute('data-tool-outcome-state')).toBe('blocked');
        expect(card.getAttribute('data-command-count')).toBe('0');
        expect(card.getAttribute('data-quality-count')).toBe('0');
        expect(card.className).toContain('coding-agent-turn-card--blocked');
        expect(card.textContent).toContain('Blocked');
        expect(card.textContent).toContain('None (0)');
        expect(card.textContent).toContain('Passed (0)');
        const blockedTool = screen.getByTestId('sidebar-coding-agent-tool-trace').querySelector('[data-tool-trace-name="bash"]');
        expect(blockedTool?.getAttribute('data-tool-trace-outcome-state')).toBe('blocked');
        expect(blockedTool?.getAttribute('data-tool-trace-summary')).toBe('command refused');
        expect(blockedTool?.getAttribute('title')).toContain('command refused');
    });

    it('keeps zero-length coding agent sidebar counts visible in data attributes', () => {
        render(
            <SidebarSystemStatus
                lang="en"
                maclawLLMOnline
                remoteActivationStatus={{ activated: true }}
                qqBotStatus=""
                telegramStatus=""
                weixinStatus=""
                lansengerStatus=""
                sidebarCurrentProviderTokenUsage={{ provider: 'MaClaw', isHubService: false, input: 0, output: 0, total: 12, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0 }}
                sidebarHubCredits={baseCredits}
                formatSidebarTokens={(value) => String(value)}
                formatSidebarHubExpiry={() => '05/06/26'}
                formatSidebarHubTotalCredits={(value) => String(value?.total ?? 0)}
                formatSidebarHubUsedCredits={(value) => String(value?.used ?? 0)}
                formatSidebarCredit={(value) => String(value)}
                unlimitedHubCreditText="unlimited"
                noHubAuthorizationText="none"
                showHubCreditAction={false}
                openHubCreditsPage={vi.fn()}
                codingAgentProgress={{ phase: 'result', taskID: 'T4', title: 'No-op coding task', event: 'diff_summary', count: 0, files: [] }}
                codingAgentTurnSnapshot={{
                    latest: { phase: 'result', taskID: 'T4', title: 'No-op coding task', event: 'diff_summary', count: 0, files: [] },
                    turnID: 'turn-4',
                    taskID: 'T4',
                    title: 'No-op coding task',
                    phase: 'result',
                    toolDurationMs: 0,
                    tools: [],
                    changeCount: 0,
                    files: [],
                }}
            />,
        );

        const card = screen.getByTestId('sidebar-coding-agent-card');
        expect(card.getAttribute('data-tool-duration-ms')).toBe('0');
        expect(card.getAttribute('data-tool-count')).toBe('0');
        expect(card.getAttribute('data-change-count')).toBe('0');
        expect(card.getAttribute('data-file-count')).toBe('0');
    });
});
