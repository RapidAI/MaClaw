// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import type { ChatMessage } from '../useAIAssistant';
import {
    activeCodingAgentProgress,
    codingAgentCommandStatusLabel,
    codingAgentCommandStatusTone,
    codingAgentCommandPreviewText,
    codingAgentCompactText,
    codingAgentComposerStatusText,
    codingAgentInputStatusText,
    codingAgentDiffCheckStatusLabel,
    codingAgentDiffCheckStatusTone,
    codingAgentDisplayText,
    codingAgentExplorationStatusLabel,
    codingAgentExplorationStatusTone,
    codingAgentFileActivityStatusLabel,
    codingAgentFileActivityStatusTone,
    codingAgentFileChangeRows,
    codingAgentFilePreviewText,
    codingAgentGuardrailStatusLabel,
    codingAgentGuardrailStatusTone,
    codingAgentQualityStatusLabel,
    codingAgentQualityStatusTone,
    codingAgentProgressTone,
    codingAgentProgressStatusText,
    formatCodingAgentDuration,
    codingAgentStatusClassName,
    codingAgentStatusDataAttrs,
    codingAgentStatusLabel,
    codingAgentStatusSelector,
    codingAgentStatusTone,
    CODING_AGENT_FAILURE_ACCENT,
    CODING_AGENT_FAILURE_ACCENT_DARK,
    adaptCodingAgentStatusTone,
    codingAgentUiIsDark,
    resolveCodingAgentStatusTone,
    codingAgentToolOutcomeLabel,
    codingAgentToolOutcomeTone,
    codingAgentOutcomeMark,
    codingAgentToolTraceText,
    codingAgentTurnSnapshotText,
    codingAgentVerificationStatusLabel,
    codingAgentVerificationStatusTone,
    codingAgentVariantDisplayText,
    isCodingAgentActivePhase,
    isCodingAgentKnownPhase,
    isCodingAgentTerminalPhase,
    latestCodingAgentTurnSnapshot,
    latestCodingAgentProgress,
    normalizeCodingAgentPhase,
    normalizeCodingAgentProgress,
    normalizeCodingAgentTaskID,
    normalizeCodingAgentTitle,
    parseCodingAgentEventProgress,
    parseCodingAgentProgress,
    codingAgentFeedStableKey,
    compactCodingTrailPath,
    codingAgentEditedFileDetailText,
    codingAgentPreviewTargetPaths,
    codingAgentToolDisplayName,
    CodingAgentPreviewFocusContext,
    isCodingAgentChatHiddenEvent,
    codingAgentMessagesHavePlainTrail,
    isCodingAgentPlainTrailEvent,
    isCodingAgentProgressContent,
    pickCodingAgentFeedHeader,
    renderCodingAgentActivityFeed,
    renderCodingAgentProgressStatus,
    renderCodingAgentWorkingTrail,
    resolveCodingAgentFeedTone,
} from '../CodingAgentProgressStatus';

const makeProgressMsg = (content: string, id = content): ChatMessage => ({
    id,
    role: 'progress',
    content,
    timestamp: 1,
});

describe('CodingAgentProgressStatus', () => {
    it('treats a missing exploratory file as a neutral lookup result', () => {
        const progress = parseCodingAgentProgress(`Coding Agent Event: ${JSON.stringify({
            version: 1,
            agent: 'coding',
            phase: 'running',
            event: 'tool_finished',
            detail: 'read_file',
            outcome: 'failed',
            summary: 'file not found: C:\\testdriver\\missing.cpp',
        })}`);
        expect(progress).toBeTruthy();
        expect(codingAgentProgressTone(progress!).accent).not.toBe(CODING_AGENT_FAILURE_ACCENT);
        expect(codingAgentProgressStatusText(progress!, 'zh-Hans')).toContain('文件或路径不存在');
    });

    it('treats remote exploratory path probes as neutral lookup results', () => {
        const missingFile = parseCodingAgentProgress(`Coding Agent Event: ${JSON.stringify({
            version: 1,
            agent: 'coding',
            phase: 'running',
            event: 'tool_finished',
            detail: 'ssh_read_file',
            outcome: 'failed',
            summary: 'No such file or directory: /srv/app/missing.go',
        })}`);
        expect(missingFile).toBeTruthy();
        expect(codingAgentProgressTone(missingFile!).accent).toBe('#64748b');
        expect(codingAgentProgressStatusText(missingFile!, 'zh-Hans')).toContain('文件或路径不存在');

        const missingDir = parseCodingAgentProgress(`Coding Agent Event: ${JSON.stringify({
            version: 1,
            agent: 'coding',
            phase: 'running',
            event: 'tool_finished',
            detail: 'ssh_list_dir',
            outcome: 'failed',
            summary: "ls: cannot access '/srv/app/nope': No such file or directory",
        })}`);
        expect(missingDir).toBeTruthy();
        expect(codingAgentProgressTone(missingDir!).accent).toBe('#64748b');
        expect(codingAgentProgressStatusText(missingDir!, 'zh-Hans')).toContain('文件或路径不存在');

        // Real remote hard failures keep the soft amber failure tone (not neutral).
        const hardFail = parseCodingAgentProgress(`Coding Agent Event: ${JSON.stringify({
            version: 1,
            agent: 'coding',
            phase: 'running',
            event: 'tool_finished',
            detail: 'ssh_bash',
            outcome: 'failed',
            summary: 'fatal error LNK1120: unresolved externals',
        })}`);
        expect(hardFail).toBeTruthy();
        expect(codingAgentProgressTone(hardFail!).accent).toBe(CODING_AGENT_FAILURE_ACCENT);
    });

    it('treats remote workspace setup probes as neutral checks', () => {
        const progress = parseCodingAgentProgress(`Coding Agent Event: ${JSON.stringify({
            version: 1,
            agent: 'coding',
            phase: 'running',
            event: 'tool_finished',
            detail: 'ssh_bash',
            outcome: 'failed',
            command: 'mkdir -p /home/test-prj3',
            summary: 'remote workspace was already unavailable',
        })}`);
        expect(progress).toBeTruthy();
        expect(codingAgentProgressTone(progress!).accent).toBe('#64748b');
        expect(codingAgentProgressStatusText(progress!, 'zh-Hans')).toContain('\u5de5\u5177\u68c0\u67e5');
    });

    it('treats an existing test-binary probe as a neutral exploratory result', () => {
        const progress = parseCodingAgentProgress(`Coding Agent Event: ${JSON.stringify({
            version: 1,
            agent: 'coding',
            phase: 'running',
            event: 'tool_finished',
            detail: 'bash',
            outcome: 'failed',
            command: 'D:\\testdriver\\build\\tests\\Release\\catch_thief_tests.exe',
            summary: 'test process returned a non-zero result',
        })}`);
        expect(progress).toBeTruthy();
        expect(codingAgentProgressTone(progress!).accent).not.toBe(CODING_AGENT_FAILURE_ACCENT);
        expect(codingAgentProgressStatusText(progress!, 'zh-Hans')).toContain('探索性测试未通过');
    });
    it('parses coding agent progress with phase, task id, and title', () => {
        expect(parseCodingAgentProgress('Coding Agent: running T2 - Fix stale edit guard')).toEqual({
            phase: 'running',
            taskID: 'T2',
            title: 'Fix stale edit guard',
        });
    });

    it('parses structured coding agent events before legacy text progress', () => {
        const raw = 'Coding Agent Event: {"version":1,"agent":"coding","event":"diff_summary","phase":"result","task_id":"t9","title":"  Review patch  ","run_id":"run-1","turn_id":"coding-turn-run-1-T9","detail":"3 files","outcome":"success","summary":"diff checked","count":3,"files":["b.go","a.go"]}';

        expect(parseCodingAgentEventProgress(raw)).toEqual({
            phase: 'result',
            taskID: 'T9',
            title: 'Review patch',
            detail: '3 files',
            outcome: 'success',
            summary: 'diff checked',
            event: 'diff_summary',
            runID: 'run-1',
            turnID: 'coding-turn-run-1-T9',
            count: 3,
            files: ['b.go', 'a.go'],
        });
        expect(parseCodingAgentProgress(raw)).toEqual({
            phase: 'result',
            taskID: 'T9',
            title: 'Review patch',
            detail: '3 files',
            outcome: 'success',
            summary: 'diff checked',
            event: 'diff_summary',
            runID: 'run-1',
            turnID: 'coding-turn-run-1-T9',
            count: 3,
            files: ['b.go', 'a.go'],
        });
        expect(codingAgentFileChangeRows(parseCodingAgentProgress(raw)!)).toEqual([
            { path: 'b.go', added: 0, removed: 0 },
            { path: 'a.go', added: 0, removed: 0 },
        ]);
    });

    it('parses per-file added and removed line counts', () => {
        const raw = 'Coding Agent Event: {"version":1,"agent":"coding","event":"diff_summary","phase":"result","task_id":"T1","title":"Patch","count":2,"added":15,"removed":4,"files":["a.go","b.go"],"file_changes":[{"path":"a.go","added":12,"removed":1},{"path":"b.go","added":3,"removed":3}]}';
        expect(parseCodingAgentEventProgress(raw)).toMatchObject({
            event: 'diff_summary',
            added: 15,
            removed: 4,
            fileChanges: [
                { path: 'a.go', added: 12, removed: 1 },
                { path: 'b.go', added: 3, removed: 3 },
            ],
        });
    });

    it('parses and displays bash commands on blocked or failed tool rows with full tooltip text', () => {
        const command = 'Remove-Item -Path "D:\\testdriver\\tests\\_parse_check.ps1" -Force; Write-Output done';
        const escapedCommand = command.replace(/\\/g, '\\\\').replace(/"/g, '\\"');
        const raw = `Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Driver","detail":"bash","command":"${escapedCommand}","outcome":"blocked","summary":"blocked by guardrail","duration_ms":1}`;

        const parsed = parseCodingAgentEventProgress(raw);
        expect(parsed?.command).toBe(command);
        expect(codingAgentCommandPreviewText(parsed!, 'en', 36)).toBe('cmd: Remove-Item -Path "D:\\testdriver\\te\u2026');

        render(
            <>
                {renderCodingAgentProgressStatus(
                    makeProgressMsg(raw),
                    { text: '#111827', fieldLabel: '#6b7280' },
                    'en',
                )}
            </>,
        );

        const preview = screen.getByTestId('coding-agent-command-preview');
        // Tool-trail shows the raw command (no "cmd:" chip label).
        expect(preview.textContent).toContain('Remove-Item');
        expect(preview.getAttribute('title')).toBe(command);
        expect(preview.getAttribute('role')).toBe('note');
        expect(preview.getAttribute('aria-label')).toBe(`Command: ${command}`);
    });

    it('shows the command for failed remote bash, but not successful bash', () => {
        expect(codingAgentCommandPreviewText({
            phase: 'running',
            title: 'Remote task',
            event: 'tool_finished',
            detail: 'ssh_bash',
            outcome: 'failed',
            command: 'make test',
        }, 'en')).toBe('cmd: make test');
        expect(codingAgentCommandPreviewText({
            phase: 'running',
            title: 'Local task',
            event: 'tool_finished',
            detail: 'bash',
            outcome: 'success',
            command: 'go test ./...',
        }, 'en')).toBeUndefined();
        expect(codingAgentCommandPreviewText({
            phase: 'running',
            title: 'Pending result',
            event: 'tool_finished',
            detail: 'bash',
            command: 'go test ./...',
        }, 'en')).toBeUndefined();
    });

    it('ignores malformed or non-coding structured events', () => {
        expect(parseCodingAgentEventProgress('Coding Agent Event: {"agent":"main","phase":"running"}')).toBeNull();
        expect(parseCodingAgentEventProgress('Coding Agent Event: not-json')).toBeNull();
    });

    it('keeps retry metadata in the title for visible task context', () => {
        expect(parseCodingAgentProgress('Coding Agent: retrying T3 - Update tests (1/2)')).toEqual({
            phase: 'retrying',
            taskID: 'T3',
            title: 'Update tests (1/2)',
        });
    });

    it('normalizes task ids so status surfaces stay visually consistent', () => {
        expect(normalizeCodingAgentTaskID(' t12 ')).toBe('T12');
        expect(normalizeCodingAgentTaskID('')).toBeUndefined();
        expect(parseCodingAgentProgress('coding agent: RUNNING t12 - lowercase id')).toEqual({
            phase: 'running',
            taskID: 'T12',
            title: 'lowercase id',
        });
    });

    it('normalizes full progress objects before display or diagnostics', () => {
        expect(normalizeCodingAgentTitle('  Fix monitor badge  ')).toBe('Fix monitor badge');
        expect(normalizeCodingAgentProgress({ phase: normalizeCodingAgentPhase('RUNNING'), taskID: ' t2 ', title: '  Fix monitor badge  ', durationMs: 0, count: 0 })).toEqual({
            phase: 'running',
            taskID: 'T2',
            title: 'Fix monitor badge',
            durationMs: 0,
            count: 0,
        });
    });

    it('parses every coding subagent progress shape emitted by the backend', () => {
        const samples = [
            ['Coding Agent: starting T1 - Plan edits', 'starting', 'Plan edits'],
            ['Coding Agent: running T1 - Plan edits', 'running', 'Plan edits'],
            ['Coding Agent: completed T1 - Plan edits', 'completed', 'Plan edits'],
            ['Coding Agent: failed T1 - Plan edits', 'failed', 'Plan edits'],
            ['Coding Agent: retrying T1 - Plan edits (1/2)', 'retrying', 'Plan edits (1/2)'],
            ['Coding Agent: skipped T1 - Plan edits', 'skipped', 'Plan edits'],
            ['Coding Agent: result T1 - Plan edits (passed)', 'result', 'Plan edits (passed)'],
        ] as const;

        for (const [raw, phase, title] of samples) {
            expect(parseCodingAgentProgress(raw)).toEqual({ phase, taskID: 'T1', title });
        }
    });

    it('ignores non coding-agent progress rows', () => {
        expect(parseCodingAgentProgress('Working on it')).toBeNull();
        expect(parseCodingAgentProgress('Agent: running T1 - other')).toBeNull();
    });

    it('normalizes supported phases and keeps unknown coding-agent phases bounded', () => {
        expect(isCodingAgentKnownPhase('running')).toBe(true);
        expect(isCodingAgentKnownPhase('RUNNING')).toBe(true);
        expect(isCodingAgentKnownPhase('queued')).toBe(false);
        expect(normalizeCodingAgentPhase('RUNNING')).toBe('running');
        expect(normalizeCodingAgentPhase('queued')).toBe('unknown');
        expect(parseCodingAgentProgress('Coding Agent: queued T7 - Waiting for tests')).toEqual({
            phase: 'unknown',
            taskID: 'T7',
            title: 'Waiting for tests',
        });
    });

    it('returns the latest coding agent progress from mixed progress messages', () => {
        const latest = latestCodingAgentProgress([
            makeProgressMsg('Preparing model'),
            makeProgressMsg('Coding Agent: running T1 - First task'),
            makeProgressMsg('Still thinking'),
            makeProgressMsg('Coding Agent: completed T1 - First task'),
        ]);

        expect(latest).toEqual({ phase: 'completed', taskID: 'T1', title: 'First task' });
    });

    it('aggregates the latest coding-agent turn into a monitor snapshot', () => {
        const messages = [
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"task_status","phase":"running","task_id":"T1","title":"First task","run_id":"run-1","turn_id":"turn-1"}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_started","phase":"running","task_id":"T1","title":"First task","run_id":"run-1","turn_id":"turn-1","detail":"read_file"}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"First task","run_id":"run-1","turn_id":"turn-1","detail":"read_file","outcome":"success","duration_ms":1250}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"guardrail_summary","phase":"result","task_id":"T1","title":"First task","run_id":"run-1","turn_id":"turn-1","outcome":"blocked","summary":"blocked | bash | category:git","count":1}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"command_summary","phase":"result","task_id":"T1","title":"First task","run_id":"run-1","turn_id":"turn-1","outcome":"failed","summary":"2 bash commands run, 1 failed: npm test","count":2}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"file_activity_summary","phase":"result","task_id":"T1","title":"First task","run_id":"run-1","turn_id":"turn-1","outcome":"changed","detail":"read 2 / modified 1 / created 1","summary":"read 2 / modified 1 / created 1; changed: a.go, b.go","count":4,"files":["a.go","b.go"]}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"quality_summary","phase":"result","task_id":"T1","title":"First task","run_id":"run-1","turn_id":"turn-1","outcome":"warning","summary":"verification not run","count":1}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"exploration_summary","phase":"result","task_id":"T1","title":"First task","run_id":"run-1","turn_id":"turn-1","outcome":"explored","summary":"searched before editing","count":2}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"verification_summary","phase":"result","task_id":"T1","title":"First task","run_id":"run-1","turn_id":"turn-1","outcome":"passed","summary":"go test ./gui passed","count":1}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"diff_check","phase":"result","task_id":"T1","title":"First task","run_id":"run-1","turn_id":"turn-1","outcome":"checked","summary":"diff --git a/a.go b/a.go","count":2}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"diff_updated","phase":"running","task_id":"T1","title":"First task","run_id":"run-1","turn_id":"turn-1","detail":"a.go (1)"}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"diff_summary","phase":"result","task_id":"T1","title":"First task","run_id":"run-1","turn_id":"turn-1","detail":"2 files","count":2,"files":["a.go","b.go"]}'),
        ];

        expect(latestCodingAgentTurnSnapshot(messages)).toEqual({
            latest: {
                phase: 'result',
                taskID: 'T1',
                title: 'First task',
                detail: '2 files',
                event: 'diff_summary',
                runID: 'run-1',
                turnID: 'turn-1',
                count: 2,
                files: ['a.go', 'b.go'],
            },
            turnID: 'turn-1',
            runID: 'run-1',
            taskID: 'T1',
            title: 'First task',
            phase: 'result',
            tool: 'read_file',
            toolOutcome: 'success',
            toolDurationMs: 1250,
            tools: [{ name: 'read_file', outcome: 'success', durationMs: 1250 }],
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
        });
    });

    it('keeps a recent coding-agent tool trace for the active turn', () => {
        const snapshot = latestCodingAgentTurnSnapshot([
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_started","phase":"running","task_id":"T1","title":"First task","turn_id":"turn-1","detail":"plan"}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"First task","turn_id":"turn-1","detail":"plan","outcome":"success","duration_ms":50}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_started","phase":"running","task_id":"T1","title":"First task","turn_id":"turn-1","detail":"read_file"}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"First task","turn_id":"turn-1","detail":"read_file","outcome":"success","duration_ms":1250}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_started","phase":"running","task_id":"T1","title":"First task","turn_id":"turn-1","detail":"apply_patch"}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"First task","turn_id":"turn-1","detail":"apply_patch","outcome":"blocked","summary":"outside project","duration_ms":300}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_started","phase":"running","task_id":"T1","title":"First task","turn_id":"turn-1","detail":"test"}'),
        ]);

        expect(snapshot?.tools).toEqual([
            { name: 'read_file', outcome: 'success', durationMs: 1250 },
            { name: 'apply_patch', outcome: 'blocked', durationMs: 300, summary: 'outside project' },
            { name: 'test' },
        ]);
        expect(snapshot && codingAgentToolTraceText(snapshot, 'en')).toBe('read_file Success 1.3s -> apply_patch Blocked 300ms (outside project) -> test');
    });

    it('does not attach a previous tool result to the latest pending coding-agent tool', () => {
        const snapshot = latestCodingAgentTurnSnapshot([
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_started","phase":"running","task_id":"T1","title":"First task","turn_id":"turn-1","detail":"apply_patch"}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"First task","turn_id":"turn-1","detail":"apply_patch","outcome":"blocked","summary":"outside project","duration_ms":300}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_started","phase":"running","task_id":"T1","title":"First task","turn_id":"turn-1","detail":"test"}'),
        ]);

        expect(snapshot?.tool).toBe('test');
        expect(snapshot?.toolOutcome).toBeUndefined();
        expect(snapshot?.toolDurationMs).toBeUndefined();
        expect(snapshot?.tools).toEqual([
            { name: 'apply_patch', outcome: 'blocked', durationMs: 300, summary: 'outside project' },
            { name: 'test' },
        ]);
    });

    it('formats a coding-agent turn snapshot for card accessibility text', () => {
        const snapshot = latestCodingAgentTurnSnapshot([
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_started","phase":"running","task_id":"T1","title":"First task","turn_id":"turn-1","detail":"read_file"}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"First task","turn_id":"turn-1","detail":"read_file","outcome":"blocked","duration_ms":1250}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"guardrail_summary","phase":"result","task_id":"T1","title":"First task","turn_id":"turn-1","outcome":"blocked","summary":"blocked | bash | category:git","count":1}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"command_summary","phase":"result","task_id":"T1","title":"First task","turn_id":"turn-1","outcome":"none","summary":"no bash commands run","count":0}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"file_activity_summary","phase":"result","task_id":"T1","title":"First task","turn_id":"turn-1","outcome":"none","detail":"read 0 / modified 0 / created 0","summary":"read 0 / modified 0 / created 0","count":0}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"quality_summary","phase":"result","task_id":"T1","title":"First task","turn_id":"turn-1","outcome":"passed","summary":"no file changes; quality gates not needed","count":0}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"exploration_summary","phase":"result","task_id":"T1","title":"First task","turn_id":"turn-1","outcome":"missing","summary":"no reads before editing","count":0}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"verification_summary","phase":"result","task_id":"T1","title":"First task","turn_id":"turn-1","outcome":"missing","summary":"no verification command detected","count":0}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"diff_check","phase":"result","task_id":"T1","title":"First task","turn_id":"turn-1","outcome":"skipped","summary":"no modified files","count":0}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"diff_summary","phase":"result","task_id":"T1","title":"First task","turn_id":"turn-1","detail":"2 files","count":2,"files":["a.go","b.go"]}'),
        ]);

        expect(snapshot && codingAgentTurnSnapshotText(snapshot, 'en')).toBe('Coding \u00b7 Result \u00b7 T1 \u00b7 2 files \u00b7 First task \u00b7 Trace: read_file Blocked 1.3s \u00b7 Tool: read_file \u00b7 Result: blocked \u00b7 Duration: 1.3s \u00b7 Guard: Blocked (blocked | bash | category:git) \u00b7 Commands: None (no bash commands run) \u00b7 Activity: None (read 0 / modified 0 / created 0) \u00b7 Quality: Passed (no file changes; quality gates not needed) \u00b7 Explore: Missing (no reads before editing) \u00b7 Verify: Not run (no verification command detected) \u00b7 Diff check: Skipped (no modified files) \u00b7 Files: a.go, b.go \u00b7 Diff: 2 files');
    });

    it('keeps zero-duration coding-agent timing visible in trace and accessibility text', () => {
        const snapshot = latestCodingAgentTurnSnapshot([
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_started","phase":"running","task_id":"T1","title":"Instant task","turn_id":"turn-1","detail":"read_file"}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Instant task","turn_id":"turn-1","detail":"read_file","outcome":"success","duration_ms":0}'),
        ]);

        expect(snapshot && codingAgentToolTraceText(snapshot, 'en')).toBe('read_file Success 0ms');
        expect(snapshot && codingAgentTurnSnapshotText(snapshot, 'en')).toContain('Duration: 0ms');
    });

    it('normalizes coding-agent guardrail labels and tones', () => {
        expect(codingAgentGuardrailStatusLabel('blocked', 'en')).toBe('Blocked');
        expect(codingAgentGuardrailStatusLabel('blocked', 'zh-Hans')).toBe('\u5df2\u62e6\u622a');
        expect(codingAgentGuardrailStatusTone('blocked').accent).toBe('#64748b');
    });

    it('normalizes coding-agent command labels and tones', () => {
        expect(codingAgentCommandStatusLabel('passed', 'en')).toBe('Passed');
        expect(codingAgentCommandStatusLabel('none', 'en')).toBe('None');
        expect(codingAgentCommandStatusLabel('failed', 'zh-Hans')).toBe('\u5931\u8d25');
        expect(codingAgentCommandStatusTone('passed').accent).toBe('#4f7f6f');
        expect(codingAgentCommandStatusTone('failed').accent).toBe(CODING_AGENT_FAILURE_ACCENT);
    });

    it('normalizes coding-agent file activity labels and tones', () => {
        expect(codingAgentFileActivityStatusLabel('changed', 'en')).toBe('Changed');
        expect(codingAgentFileActivityStatusLabel('read_only', 'en')).toBe('Read only');
        expect(codingAgentFileActivityStatusLabel('none', 'zh-Hans')).toBe('\u65e0\u52a8\u4f5c');
        expect(codingAgentFileActivityStatusTone('changed').accent).toBe('#4f7f6f');
        expect(codingAgentFileActivityStatusTone('read_only').accent).toBe('#2f6fbc');
    });

    it('normalizes coding-agent quality labels and tones', () => {
        expect(codingAgentQualityStatusLabel('passed', 'en')).toBe('Passed');
        expect(codingAgentQualityStatusLabel('warning', 'en')).toBe('Warning');
        expect(codingAgentQualityStatusLabel('failed', 'zh-Hans')).toBe('\u672a\u901a\u8fc7');
        expect(codingAgentQualityStatusTone('passed').accent).toBe('#4f7f6f');
        expect(codingAgentQualityStatusTone('warning').accent).toBe('#64748b');
        expect(codingAgentQualityStatusTone('failed').accent).toBe(CODING_AGENT_FAILURE_ACCENT);
    });

    it('normalizes coding-agent exploration labels and tones', () => {
        expect(codingAgentExplorationStatusLabel('explored', 'en')).toBe('Explored');
        expect(codingAgentExplorationStatusLabel('read_only', 'en')).toBe('Read');
        expect(codingAgentExplorationStatusLabel('missing', 'zh-Hans')).toBe('\u672a\u63a2\u7d22');
        expect(codingAgentExplorationStatusTone('explored').accent).toBe('#4f7f6f');
        expect(codingAgentExplorationStatusTone('missing').accent).toBe('#64748b');
    });

    it('normalizes coding-agent verification labels and tones', () => {
        expect(codingAgentVerificationStatusLabel('passed', 'en')).toBe('Passed');
        expect(codingAgentVerificationStatusLabel('missing', 'en')).toBe('Not run');
        expect(codingAgentVerificationStatusLabel('failed', 'zh-Hans')).toBe('\u672a\u901a\u8fc7');
        expect(codingAgentVerificationStatusTone('failed').accent).toBe(CODING_AGENT_FAILURE_ACCENT);
        expect(codingAgentVerificationStatusTone('missing').accent).toBe('#64748b');
    });

    it('normalizes coding-agent diff check labels and tones', () => {
        expect(codingAgentDiffCheckStatusLabel('checked', 'en')).toBe('Checked');
        expect(codingAgentDiffCheckStatusLabel('skipped', 'en')).toBe('Skipped');
        expect(codingAgentDiffCheckStatusLabel('failed', 'zh-Hans')).toBe('\u5931\u8d25');
        expect(codingAgentDiffCheckStatusTone('checked').accent).toBe('#4f7f6f');
        expect(codingAgentDiffCheckStatusTone('skipped').accent).toBe('#64748b');
    });

    it('normalizes coding-agent tool outcome labels and tones', () => {
        expect(codingAgentToolOutcomeLabel('success', 'en')).toBe('Success');
        expect(codingAgentToolOutcomeLabel('failed', 'zh-Hans')).toBe('\u5931\u8d25');
        expect(codingAgentToolOutcomeLabel('blocked', 'zh-Hans')).toBe('\u5df2\u963b\u65ad');
        expect(codingAgentToolOutcomeLabel('other', 'en')).toBe('Unknown');
        expect(codingAgentToolOutcomeTone('success').accent).toBe('#4f7f6f');
        expect(codingAgentToolOutcomeTone('failed').accent).toBe(CODING_AGENT_FAILURE_ACCENT);
        expect(codingAgentToolOutcomeTone('blocked').accent).toBe('#64748b');
        expect(formatCodingAgentDuration(0)).toBe('0ms');
        expect(formatCodingAgentDuration(250)).toBe('250ms');
        expect(formatCodingAgentDuration(1250)).toBe('1.3s');
        expect(formatCodingAgentDuration(65000)).toBe('1m 5s');
    });

    it('renders diagnostic tool failures with a neutral tone while keeping real failures as soft amber', () => {
        expect(codingAgentProgressTone({
            phase: 'running',
            title: 'Probe compiler',
            event: 'tool_finished',
            detail: 'bash',
            outcome: 'failed',
            summary: 'PowerShell exception: g++ is not recognized as a cmdlet',
        }).accent).toBe('#64748b');

        expect(codingAgentProgressTone({
            phase: 'running',
            title: 'Locate compiler',
            event: 'tool_finished',
            detail: 'bash',
            outcome: 'failed',
            command: 'where cl.exe 2>&1; Get-ItemProperty "HKLM:\\SOFTWARE\\Microsoft\\VisualStudio\\SxS\\VS7" 2>&1',
            summary: 'PowerShell error: cannot find the requested registry path',
        }).accent).toBe('#64748b');
        expect(codingAgentProgressStatusText({
            phase: 'running',
            title: 'Locate compiler',
            event: 'tool_finished',
            detail: 'bash',
            outcome: 'failed',
            command: 'where cl.exe 2>&1; Get-ItemProperty "HKLM:\\SOFTWARE\\Microsoft\\VisualStudio\\SxS\\VS7" 2>&1',
            summary: 'PowerShell error: cannot find the requested registry path',
        }, 'zh-Hans')).toBe('cl.exe \u4e0d\u5728 PATH\uff08\u9700\u5148 vcvars\uff09');

        expect(codingAgentProgressStatusText({
            phase: 'running',
            title: 'Locate Visual Studio',
            event: 'tool_finished',
            detail: 'bash',
            outcome: 'failed',
            command: 'Get-ChildItem "C:\\Program Files\\Microsoft Visual Studio\\2019" -ErrorAction SilentlyContinue',
            summary: 'PowerShell error: 找不到路径',
        }, 'zh-Hans')).toBe('VS \u8def\u5f84\u63a2\u6d4b\u672a\u547d\u4e2d\uff08cl \u9700 vcvars\uff09');

        expect(codingAgentProgressStatusText({
            phase: 'running',
            title: 'Probe compiler',
            event: 'tool_finished',
            detail: 'bash',
            outcome: 'failed',
            command: 'clang++ --version',
            summary: 'PowerShell exception: command probe returned exit code 1',
        }, 'zh-Hans')).toBe('clang++ \u4e0d\u5b58\u5728');

        expect(codingAgentProgressTone({
            phase: 'running',
            title: 'Probe compiler',
            event: 'tool_finished',
            detail: 'bash',
            outcome: 'failed',
            summary: 'g++: command not found',
        }).accent).toBe('#64748b');

        // Real MSVC compile via full VS path must stay a hard failure tone, not a neutral "probe".
        const msvcCompileFail = {
            phase: 'running' as const,
            title: 'Build',
            event: 'tool_finished',
            detail: 'bash',
            outcome: 'failed',
            command: 'cmd /c "call \\"C:\\\\Program Files\\\\Microsoft Visual Studio\\\\18\\\\Community\\\\VC\\\\Auxiliary\\\\Build\\\\vcvars64.bat\\" && cl /utf-8 /EHsc /Fe:test.exe hello.cpp"',
            summary: 'error C2065: undeclared identifier',
        };
        expect(codingAgentProgressTone(msvcCompileFail).accent).toBe(CODING_AGENT_FAILURE_ACCENT);
        expect(codingAgentProgressStatusText(msvcCompileFail, 'zh-Hans')).not.toMatch(/vcvars|PATH|探测/);

        // PowerShell-wrapped MSVC failure must also stay hard-failure tone (not "powershell exception" probe).
        const msvcPSFail = {
            phase: 'running' as const,
            title: 'Build',
            event: 'tool_finished',
            detail: 'bash',
            outcome: 'failed',
            command: 'cmd /c "call \\"C:\\\\Program Files\\\\Microsoft Visual Studio\\\\18\\\\Community\\\\VC\\\\Auxiliary\\\\Build\\\\vcvars64.bat\\" && cl /utf-8 hello.cpp"',
            summary: 'PowerShell exception: error C2143: syntax error',
        };
        expect(codingAgentProgressTone(msvcPSFail).accent).toBe(CODING_AGENT_FAILURE_ACCENT);

        expect(codingAgentProgressTone({
            phase: 'running',
            title: 'Locate CMake',
            event: 'tool_finished',
            detail: 'bash',
            outcome: 'failed',
            severity: 'diagnostic',
            summary: 'PowerShell error while locating optional compiler tools.',
        }).accent).toBe('#64748b');

        expect(parseCodingAgentProgress('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","title":"Probe compiler","detail":"bash","outcome":"failed","severity":"diagnostic","summary":"PowerShell error while locating optional compiler tools."}')).toMatchObject({
            phase: 'running',
            title: 'Probe compiler',
            event: 'tool_finished',
            detail: 'bash',
            outcome: 'failed',
            severity: 'diagnostic',
            summary: 'PowerShell error while locating optional compiler tools.',
        });

        expect(codingAgentProgressTone({
            phase: 'running',
            title: 'Locate CMake',
            event: 'tool_finished',
            detail: 'bash',
            outcome: 'failed',
            summary: 'Last error: INFO: Could not find files for the given pattern(s).',
        }).accent).toBe('#64748b');

        expect(codingAgentProgressTone({
            phase: 'running',
            title: 'Build project',
            event: 'tool_finished',
            detail: 'bash',
            outcome: 'failed',
            summary: 'ninja: build stopped: subcommand failed.',
        }).accent).toBe(CODING_AGENT_FAILURE_ACCENT);

        expect(codingAgentProgressTone({
            phase: 'running',
            title: 'Run tests',
            event: 'tool_finished',
            detail: 'bash',
            outcome: 'failed',
            summary: 'FAIL at D:\\test\\test_hello.cpp:11: CHECK(result == "Hello, World!")',
        }).accent).toBe(CODING_AGENT_FAILURE_ACCENT);

        expect(codingAgentProgressTone({
            phase: 'running',
            title: 'Run tests',
            event: 'tool_finished',
            detail: 'bash',
            outcome: 'failed',
            severity: 'diagnostic',
            summary: 'FAIL at D:\\test\\test_hello.cpp:11: CHECK (result == "Hello, World!")',
        }).accent).toBe(CODING_AGENT_FAILURE_ACCENT);

        expect(codingAgentProgressTone({
            phase: 'running',
            title: 'Probe deployment',
            event: 'tool_finished',
            detail: 'bash',
            outcome: 'failed',
            severity: 'diagnostic',
            summary: 'permission denied while opening /srv/app/config.yml',
        }).accent).toBe(CODING_AGENT_FAILURE_ACCENT);
    });

    it('keeps older coding-agent turns out of the latest turn snapshot', () => {
        const messages = [
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_started","phase":"running","task_id":"T1","title":"First task","turn_id":"turn-1","detail":"read_file"}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"task_status","phase":"running","task_id":"T2","title":"Second task","turn_id":"turn-2"}'),
        ];

        expect(latestCodingAgentTurnSnapshot(messages)?.tool).toBeUndefined();
        expect(latestCodingAgentTurnSnapshot(messages)?.turnID).toBe('turn-2');
    });

    it('hides active coding agent progress when the assistant is idle', () => {
        const messages = [makeProgressMsg('Coding Agent: running T1 - First task')];

        expect(activeCodingAgentProgress(messages, false)).toBeNull();
        expect(activeCodingAgentProgress(messages, true)).toEqual({ phase: 'running', taskID: 'T1', title: 'First task' });
    });

    it('does not treat terminal coding agent rows as an active monitor state', () => {
        const completed = [makeProgressMsg('Coding Agent: completed T1 - First task')];
        const failed = [makeProgressMsg('Coding Agent: failed T1 - First task')];

        expect(activeCodingAgentProgress(completed, true)).toBeNull();
        expect(activeCodingAgentProgress(failed, true)).toBeNull();
    });

    it('keeps diff summary visible in monitors while the coding turn is still active', () => {
        const diffSummary = [makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"diff_summary","phase":"result","task_id":"T1","title":"First task","detail":"2 files","count":2,"files":["a.go","b.go"]}')];

        expect(activeCodingAgentProgress(diffSummary, true)).toEqual({
            phase: 'result',
            taskID: 'T1',
            title: 'First task',
            detail: '2 files',
            event: 'diff_summary',
            count: 2,
            files: ['a.go', 'b.go'],
        });
        expect(activeCodingAgentProgress(diffSummary, false)).toBeNull();
    });

    it('keeps verification summary visible in monitors while the coding turn is still active', () => {
        const verificationSummary = [makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"verification_summary","phase":"result","task_id":"T1","title":"First task","outcome":"missing","summary":"no verification command detected","count":0}')];

        expect(activeCodingAgentProgress(verificationSummary, true)).toEqual({
            phase: 'result',
            taskID: 'T1',
            title: 'First task',
            outcome: 'missing',
            summary: 'no verification command detected',
            event: 'verification_summary',
            count: 0,
        });
        expect(activeCodingAgentProgress(verificationSummary, false)).toBeNull();
    });

    it('keeps diff check visible in monitors while the coding turn is still active', () => {
        const diffCheck = [makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"diff_check","phase":"result","task_id":"T1","title":"First task","outcome":"skipped","summary":"no modified files","count":0}')];

        expect(activeCodingAgentProgress(diffCheck, true)).toEqual({
            phase: 'result',
            taskID: 'T1',
            title: 'First task',
            outcome: 'skipped',
            summary: 'no modified files',
            event: 'diff_check',
            count: 0,
        });
        expect(activeCodingAgentProgress(diffCheck, false)).toBeNull();
    });

    it('keeps exploration summary visible in monitors while the coding turn is still active', () => {
        const explorationSummary = [makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"exploration_summary","phase":"result","task_id":"T1","title":"First task","outcome":"missing","summary":"no reads before editing","count":0}')];

        expect(activeCodingAgentProgress(explorationSummary, true)).toEqual({
            phase: 'result',
            taskID: 'T1',
            title: 'First task',
            outcome: 'missing',
            summary: 'no reads before editing',
            event: 'exploration_summary',
            count: 0,
        });
        expect(activeCodingAgentProgress(explorationSummary, false)).toBeNull();
    });

    it('keeps guardrail summary visible in monitors while the coding turn is still active', () => {
        const guardrailSummary = [makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"guardrail_summary","phase":"result","task_id":"T1","title":"First task","outcome":"blocked","summary":"blocked | bash | category:git","count":1}')];

        expect(activeCodingAgentProgress(guardrailSummary, true)).toEqual({
            phase: 'result',
            taskID: 'T1',
            title: 'First task',
            outcome: 'blocked',
            summary: 'blocked | bash | category:git',
            event: 'guardrail_summary',
            count: 1,
        });
        expect(activeCodingAgentProgress(guardrailSummary, false)).toBeNull();
    });

    it('keeps command summary visible in monitors while the coding turn is still active', () => {
        const commandSummary = [makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"command_summary","phase":"result","task_id":"T1","title":"First task","outcome":"failed","summary":"2 bash commands run, 1 failed: npm test","count":2}')];

        expect(activeCodingAgentProgress(commandSummary, true)).toEqual({
            phase: 'result',
            taskID: 'T1',
            title: 'First task',
            outcome: 'failed',
            summary: '2 bash commands run, 1 failed: npm test',
            event: 'command_summary',
            count: 2,
        });
        expect(activeCodingAgentProgress(commandSummary, false)).toBeNull();
    });

    it('preserves zero count coding-agent summary events for monitors', () => {
        const commandSummary = [makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"command_summary","phase":"result","task_id":"T1","title":"First task","outcome":"none","summary":"no bash commands run","count":0}')];
        const qualitySummary = [makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"quality_summary","phase":"result","task_id":"T1","title":"First task","outcome":"passed","summary":"no file changes; quality gates not needed","count":0}')];

        expect(activeCodingAgentProgress(commandSummary, true)?.count).toBe(0);
        expect(activeCodingAgentProgress(qualitySummary, true)?.count).toBe(0);
    });

    it('keeps file activity summary visible in monitors while the coding turn is still active', () => {
        const fileActivitySummary = [makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"file_activity_summary","phase":"result","task_id":"T1","title":"First task","outcome":"changed","detail":"read 2 / modified 1 / created 1","summary":"read 2 / modified 1 / created 1; changed: a.go, b.go","count":4,"files":["a.go","b.go"]}')];

        expect(activeCodingAgentProgress(fileActivitySummary, true)).toEqual({
            phase: 'result',
            taskID: 'T1',
            title: 'First task',
            detail: 'read 2 / modified 1 / created 1',
            outcome: 'changed',
            summary: 'read 2 / modified 1 / created 1; changed: a.go, b.go',
            event: 'file_activity_summary',
            count: 4,
            files: ['a.go', 'b.go'],
        });
        expect(activeCodingAgentProgress(fileActivitySummary, false)).toBeNull();
    });

    it('keeps quality summary visible in monitors while the coding turn is still active', () => {
        const qualitySummary = [makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"quality_summary","phase":"result","task_id":"T1","title":"First task","outcome":"warning","summary":"verification not run","count":1}')];

        expect(activeCodingAgentProgress(qualitySummary, true)).toEqual({
            phase: 'result',
            taskID: 'T1',
            title: 'First task',
            outcome: 'warning',
            summary: 'verification not run',
            event: 'quality_summary',
            count: 1,
        });
        expect(activeCodingAgentProgress(qualitySummary, false)).toBeNull();
    });

    it('uses localized labels and product-friendly display separators', () => {
        expect(codingAgentStatusLabel('failed', 'en')).toBe('Failed');
        expect(codingAgentStatusLabel('failed', 'zh-Hans')).toBe('\u5931\u8d25');
        expect(codingAgentDisplayText({ phase: 'running', taskID: 'T2', title: 'Fix stale edit guard' }, 'en'))
            .toBe('Coding \u00b7 Running \u00b7 T2 \u00b7 Fix stale edit guard');
        expect(codingAgentDisplayText({ phase: 'running', taskID: 'T2', title: 'Fix stale edit guard' }, 'zh-Hans'))
            .toBe('\u7f16\u7a0b \u00b7 \u6267\u884c\u4e2d \u00b7 T2 \u00b7 Fix stale edit guard');
        expect(codingAgentInputStatusText({ phase: 'running', taskID: 'T2', title: 'Fix stale edit guard' }, 'en')).toBe('Working...');
        expect(codingAgentInputStatusText({ phase: 'running', taskID: 'T2', title: 'Fix stale edit guard' }, 'zh-Hans')).toBe('\u5904\u7406\u4e2d\u2026');
        expect(codingAgentInputStatusText({
            phase: 'running',
            taskID: 'T1',
            title: 'Fix stale edit guard',
            event: 'tool_started',
            detail: 'bash',
            command: 'go test ./gui',
        }, 'en')).toBe('$ go test ./gui');
        expect(codingAgentComposerStatusText({ phase: 'running', taskID: 'T2', title: 'Fix stale edit guard' }, 'zh-Hans')).toBeUndefined();
        expect(codingAgentComposerStatusText({
            phase: 'running',
            taskID: 'T1',
            title: 'Fix stale edit guard',
            event: 'tool_started',
            detail: 'bash',
            command: 'go test ./gui',
        }, 'en')).toBe('$ go test ./gui');
        expect(codingAgentCompactText({ phase: 'running', taskID: 'T2', title: 'Fix stale edit guard' }, 'zh-Hans'))
            .toBe('\u7f16\u7a0b \u00b7 \u6267\u884c\u4e2d \u00b7 T2');
        expect(codingAgentCompactText({ phase: 'running', taskID: ' t2 ', title: 'Fix stale edit guard' }, 'en'))
            .toBe('Coding \u00b7 Running \u00b7 T2');
        // tool_finished detail is the tool name — omit from compact meta to avoid "Tool Success · bash" noise.
        expect(codingAgentCompactText({
            phase: 'running',
            taskID: 'T1',
            title: 'Edit',
            event: 'tool_finished',
            detail: 'bash',
            outcome: 'success',
        }, 'en')).toBe('Coding \u00b7 Tool Success \u00b7 T1');
        // tool_started still surfaces the tool name as live activity meta.
        expect(codingAgentCompactText({
            phase: 'running',
            taskID: 'T1',
            title: 'Edit',
            event: 'tool_started',
            detail: 'bash',
        }, 'en')).toBe('Coding \u00b7 Running \u00b7 T1 \u00b7 bash');
        expect(codingAgentCompactText({ phase: 'result', taskID: ' t2 ', title: 'Fix stale edit guard', event: 'diff_summary', detail: '3 files' }, 'en'))
            .toBe('Coding \u00b7 Result \u00b7 T2 \u00b7 3 files');
        expect(codingAgentCompactText({ phase: 'result', taskID: 'T2', title: 'No commands', event: 'command_summary', count: 0 }, 'en'))
            .toBe('Coding \u00b7 Result \u00b7 T2 \u00b7 0 commands');
        expect(codingAgentCompactText({ phase: 'result', taskID: 'T2', title: 'Clean quality', event: 'quality_summary', count: 0 }, 'en'))
            .toBe('Coding \u00b7 Result \u00b7 T2 \u00b7 0 issues');
        expect(codingAgentCompactText({ phase: 'result', taskID: 'T2', title: 'No diff', event: 'diff_check', count: 0 }, 'zh-Hans'))
            .toBe('\u7f16\u7a0b \u00b7 \u7ed3\u679c \u00b7 T2 \u00b7 0 \u4e2a\u53d8\u66f4');
        expect(codingAgentDisplayText({ phase: 'running', taskID: ' t2 ', title: '  Fix stale edit guard  ' }, 'en'))
            .toBe('Coding \u00b7 Running \u00b7 T2 \u00b7 Fix stale edit guard');
        expect(codingAgentVariantDisplayText({ phase: 'running', taskID: ' t2 ', title: '  Fix stale edit guard  ' }, 'en', 'sidebar'))
            .toBe('Coding \u00b7 Running \u00b7 T2 \u00b7 Fix stale edit guard');
        expect(codingAgentVariantDisplayText({
            phase: 'result',
            taskID: 'T6',
            title: 'Update parser',
            event: 'diff_summary',
            count: 4,
            files: ['a.go', 'b.go', 'c.go', 'd.go'],
        }, 'en', 'sidebar')).toBe('Coding \u00b7 Result \u00b7 T6 \u00b7 4 changes \u00b7 Update parser \u00b7 a.go, b.go, c.go +1 more');
        expect(codingAgentVariantDisplayText({ phase: 'running', taskID: ' t2 ', title: '  Fix stale edit guard  ' }, 'en', 'status-bar'))
            .toBe('Coding \u00b7 Running \u00b7 T2 \u00b7 Fix stale edit guard');
        expect(codingAgentFilePreviewText({ phase: 'result', taskID: 'T2', title: 'Done', files: ['a.go', 'b.go', 'c.go', 'd.go'] }, 'en'))
            .toBe('a.go, b.go, c.go +1 more');
        expect(codingAgentFilePreviewText({ phase: 'result', taskID: 'T2', title: 'Done', files: ['a.go', 'b.go', 'c.go', 'd.go'] }, 'zh-Hans'))
            .toBe('a.go, b.go, c.go \u7b49 1 \u4e2a');
    });

    it('maps status phases to distinct semantic tones', () => {
        expect(codingAgentStatusTone('running').accent).toBe('#2f6fbc');
        expect(codingAgentStatusTone('retrying').accent).toBe('#64748b');
        expect(codingAgentStatusTone('failed').accent).toBe(CODING_AGENT_FAILURE_ACCENT);
        expect(codingAgentStatusTone('completed').accent).toBe('#4f7f6f');
        expect(codingAgentStatusTone('result').accent).toBe('#4f7f6f');
        expect(codingAgentStatusTone('skipped').accent).toBe('#64748b');
        expect(codingAgentStatusTone('queued').accent).toBe('#2f6fbc');
        expect(codingAgentStatusLabel('queued', 'en')).toBe('Status');
        expect(codingAgentStatusLabel('queued', 'zh-Hans')).toBe('\u72b6\u6001');
    });

    it('uses event outcome tones for coding-agent summary progress rows', () => {
        expect(codingAgentProgressTone({ phase: 'result', title: 'Quality summary', event: 'quality_summary', outcome: 'failed' }).accent).toBe(CODING_AGENT_FAILURE_ACCENT);
        expect(codingAgentProgressTone({ phase: 'result', title: 'Verification summary', event: 'verification_summary', outcome: 'missing' }).accent).toBe('#64748b');
        expect(codingAgentProgressTone({ phase: 'result', title: 'Diff check', event: 'diff_check', outcome: 'checked' }).accent).toBe('#4f7f6f');
        expect(codingAgentProgressTone({ phase: 'failed', title: 'Failed' }).accent).toBe(CODING_AGENT_FAILURE_ACCENT);
    });

    it('classifies active and terminal phases for monitor diagnostics', () => {
        expect(isCodingAgentActivePhase('starting')).toBe(true);
        expect(isCodingAgentActivePhase('running')).toBe(true);
        expect(isCodingAgentActivePhase('retrying')).toBe(true);
        expect(isCodingAgentActivePhase('queued')).toBe(true);
        expect(isCodingAgentActivePhase('completed')).toBe(false);
        expect(isCodingAgentTerminalPhase('completed')).toBe(true);
        expect(isCodingAgentTerminalPhase('failed')).toBe(true);
        expect(isCodingAgentTerminalPhase('skipped')).toBe(true);
        expect(isCodingAgentTerminalPhase('result')).toBe(true);
        expect(isCodingAgentTerminalPhase('running')).toBe(false);
    });

    it('provides stable diagnostic attributes for coding-agent status surfaces', () => {
        expect(codingAgentStatusDataAttrs({ phase: normalizeCodingAgentPhase('Retrying'), taskID: ' t9 ', title: 'Review patch' }, 'title-bar')).toEqual({
            'data-agent': 'coding',
            'data-active': 'true',
            'data-change-count': '',
            'data-event': '',
            'data-phase': 'retrying',
            'data-run-id': '',
            'data-terminal': 'false',
            'data-task-id': 'T9',
            'data-turn-id': '',
            'data-variant': 'title-bar',
        });
        expect(codingAgentStatusClassName({ phase: normalizeCodingAgentPhase('Retrying'), taskID: ' t9 ', title: 'Review patch' }, 'title-bar'))
            .toBe('coding-agent-status coding-agent-status--title-bar coding-agent-status--retrying');
        expect(codingAgentStatusSelector()).toBe('.coding-agent-status');
        expect(codingAgentStatusSelector('title-bar')).toBe('.coding-agent-status[data-variant="title-bar"]');
        expect(codingAgentStatusSelector('title-bar', 'retrying')).toBe('.coding-agent-status[data-variant="title-bar"][data-phase="retrying"]');
        expect(codingAgentStatusSelector('title-bar', 'retrying', ' t9 ')).toBe('.coding-agent-status[data-variant="title-bar"][data-phase="retrying"][data-task-id="T9"]');
    });

    it('hides task-status-only chat progress until a tool trail exists', () => {
        const { container } = render(
            <>
                {renderCodingAgentProgressStatus(
                    makeProgressMsg('Coding Agent: failed T4 - Apply patch'),
                    { text: '#111827', fieldLabel: '#6b7280' },
                    'en',
                )}
            </>,
        );
        expect(container.querySelector('[data-testid="coding-agent-progress"]')).toBeNull();
    });

    it('renders an accessible visible status row for chat progress', () => {
        render(
            <>
                {renderCodingAgentProgressStatus(
                    makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"failed","task_id":"T4","title":"Apply patch","detail":"bash","outcome":"failed","command":"go test"}'),
                    { text: '#111827', fieldLabel: '#6b7280' },
                    'en',
                )}
            </>,
        );

        const status = screen.getByTestId('coding-agent-progress');
        expect(status.textContent).toContain('$');
        expect(status.textContent).toContain('go test');
        expect(status.getAttribute('role')).toBe('status');
        expect(status.getAttribute('aria-live')).toBe('polite');
        expect(status.getAttribute('aria-label')).toMatch(/T4/);
        expect(status.getAttribute('aria-label')).toMatch(/Apply patch/);
        expect(status.getAttribute('aria-label')).toMatch(/Failed/);
        expect(status.getAttribute('data-agent')).toBe('coding');
        expect(status.getAttribute('data-active')).toBe('false');
        expect(status.getAttribute('data-change-count')).toBe('');
        expect(status.getAttribute('data-phase')).toBe('failed');
        expect(status.getAttribute('data-terminal')).toBe('true');
        expect(status.getAttribute('data-task-id')).toBe('T4');
        expect(status.getAttribute('data-turn-id')).toBe('');
        expect(status.getAttribute('data-variant')).toBe('chat-progress');
        expect(status.className).toContain('coding-agent-status');
        expect(status.className).toContain('coding-agent-status--chat-progress');
        expect(status.className).toContain('coding-agent-status--failed');
        expect(status.querySelector('[data-testid="coding-agent-tool-line"]')).toBeTruthy();
    });

    it('hides quality-summary-only chat progress instead of a scorecard board', () => {
        const { container } = render(
            <>
                {renderCodingAgentProgressStatus(
                    makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"quality_summary","phase":"result","task_id":"T4","title":"Apply patch","outcome":"failed","summary":"verification not run","count":1}'),
                    { text: '#111827', fieldLabel: '#6b7280' },
                    'en',
                )}
            </>,
        );
        expect(container.querySelector('[data-testid="coding-agent-progress"]')).toBeNull();
    });

    it('detects coding progress content without false positives', () => {
        expect(isCodingAgentProgressContent('Coding Agent Event: {"version":1,"agent":"coding","event":"task_status","phase":"running"}')).toBe(true);
        expect(isCodingAgentProgressContent('Coding Agent: running T2 - Fix stale edit guard')).toBe(true);
        expect(isCodingAgentProgressContent('Coding Agent Event: {"version":1,"agent":"other","phase":"running"}')).toBe(false);
        expect(isCodingAgentProgressContent('Coding Agent: not a real status line here')).toBe(false);
        expect(isCodingAgentProgressContent('plain progress')).toBe(false);
    });

    it('builds a stable feed key from turn_id without remounting on each line', () => {
        expect(codingAgentFeedStableKey([
            { id: '1', content: 'Coding Agent Event: {"version":1,"agent":"coding","turn_id":"t-a","task_id":"T1"}' },
            { id: '2', content: 'Coding Agent Event: {"version":1,"agent":"coding","turn_id":"t-a","task_id":"T1"}' },
        ])).toBe('feed-turn-t-a');
        expect(codingAgentFeedStableKey([
            { id: 'only', content: 'Coding Agent: running T9 - Hello' },
        ])).toBe('feed-task-T9');
    });

    it('elevates feed chrome to failed when trail has critical failures while phase is running', () => {
        const tone = resolveCodingAgentFeedTone(
            normalizeCodingAgentProgress({ phase: 'running', taskID: 'T1', title: 'Fix', event: 'task_status' }),
            [
                normalizeCodingAgentProgress({
                    phase: 'running',
                    taskID: 'T1',
                    title: 'Fix',
                    event: 'tool_finished',
                    detail: 'bash',
                    outcome: 'failed',
                    command: 'cl',
                }),
            ],
            false,
        );
        expect(tone.accent).toBe(CODING_AGENT_FAILURE_ACCENT);

        const darkTone = resolveCodingAgentFeedTone(
            normalizeCodingAgentProgress({ phase: 'running', taskID: 'T1', title: 'Fix', event: 'task_status' }),
            [
                normalizeCodingAgentProgress({
                    phase: 'running',
                    taskID: 'T1',
                    title: 'Fix',
                    event: 'tool_finished',
                    detail: 'bash',
                    outcome: 'failed',
                    command: 'cl',
                }),
            ],
            true,
        );
        expect(darkTone.accent).toBe(CODING_AGENT_FAILURE_ACCENT_DARK);
        expect(darkTone.accent).not.toBe(CODING_AGENT_FAILURE_ACCENT);

        const okTone = resolveCodingAgentFeedTone(
            normalizeCodingAgentProgress({ phase: 'running', taskID: 'T1', title: 'Fix', event: 'task_status' }),
            [
                normalizeCodingAgentProgress({
                    phase: 'running',
                    taskID: 'T1',
                    title: 'Fix',
                    event: 'tool_finished',
                    detail: 'bash',
                    outcome: 'success',
                }),
            ],
            false,
        );
        expect(okTone.accent).not.toBe(CODING_AGENT_FAILURE_ACCENT);
        expect(okTone.accent).not.toMatch(/#c43d34/i);
    });

    it('uses soft amber (never red) for hard-failure outcome marks; dark mode brightens', () => {
        const failed = normalizeCodingAgentProgress({
            phase: 'failed',
            title: 'Build failed',
            event: 'tool_finished',
            detail: 'bash',
            outcome: 'failed',
            command: 'cl /c main.cpp',
            summary: 'error C2065: undeclared identifier',
        });
        const lightMark = codingAgentOutcomeMark(failed, false);
        const darkMark = codingAgentOutcomeMark(failed, true);
        expect(lightMark.glyph).toBe('\u2717');
        expect(lightMark.color).toBe(CODING_AGENT_FAILURE_ACCENT);
        expect(lightMark.color).not.toMatch(/#c43d34/i);
        expect(darkMark.color).toBe(CODING_AGENT_FAILURE_ACCENT_DARK);
    });

    it('adapts failure/neutral tones for dark UI and reads data-ai-theme', () => {
        const failed = resolveCodingAgentStatusTone(
            normalizeCodingAgentProgress({ phase: 'failed', title: 'X' }),
            true,
        );
        expect(failed.accent).toBe(CODING_AGENT_FAILURE_ACCENT_DARK);
        const lightFailed = resolveCodingAgentStatusTone(
            normalizeCodingAgentProgress({ phase: 'failed', title: 'X' }),
            false,
        );
        expect(adaptCodingAgentStatusTone(lightFailed, true).accent).toBe(CODING_AGENT_FAILURE_ACCENT_DARK);

        const neutral = codingAgentToolOutcomeTone('blocked');
        expect(adaptCodingAgentStatusTone(neutral, true).accent).toBe('#8a9ab0');
        expect(adaptCodingAgentStatusTone(neutral, false).accent).toBe('#64748b');

        expect(codingAgentUiIsDark(true)).toBe(true);
        expect(codingAgentUiIsDark(false)).toBe(false);
        let app = document.getElementById('App');
        if (!app) {
            app = document.createElement('div');
            app.id = 'App';
            document.body.appendChild(app);
        }
        app.setAttribute('data-ai-theme', 'dark');
        expect(codingAgentUiIsDark()).toBe(true);
        app.setAttribute('data-ai-theme', 'light');
        expect(codingAgentUiIsDark()).toBe(false);
        app.removeAttribute('data-ai-theme');
    });

    it('picks feed header phase from terminal task_status when it is not followed by activity', () => {
        const header = pickCodingAgentFeedHeader([
            normalizeCodingAgentProgress({
                phase: 'running',
                taskID: 'T1',
                title: 'Fix bug',
                event: 'tool_finished',
                detail: 'bash',
                outcome: 'success',
            }),
            normalizeCodingAgentProgress({
                phase: 'completed',
                taskID: 'T1',
                title: 'Fix bug',
                event: 'task_status',
            }),
        ]);
        expect(header.phase).toBe('completed');
        expect(header.taskID).toBe('T1');
        expect(header.title).toBe('Fix bug');

        // Stale completed before later tools must not win — header follows latest activity.
        const header2 = pickCodingAgentFeedHeader([
            normalizeCodingAgentProgress({
                phase: 'completed',
                taskID: 'T2',
                title: 'Done',
                event: 'task_status',
            }),
            normalizeCodingAgentProgress({
                phase: 'running',
                taskID: 'T2',
                title: 'Done',
                event: 'tool_finished',
                detail: 'bash',
                outcome: 'success',
            }),
        ]);
        expect(header2.phase).toBe('running');
    });

    it('hides pure task_status lines when tool activity is present (header carries phase)', () => {
        const { container } = render(
            <>
                {renderCodingAgentActivityFeed(
                    [
                        makeProgressMsg(
                            'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"success","command":"go test"}',
                            'tool',
                        ),
                        makeProgressMsg(
                            'Coding Agent Event: {"version":1,"agent":"coding","event":"task_status","phase":"completed","task_id":"T1","title":"Fix","turn_id":"turn-1"}',
                            'done',
                        ),
                    ],
                    { text: '#111827', fieldLabel: '#6b7280' },
                    'en',
                )}
            </>,
        );
        const status = screen.getByTestId('coding-agent-progress');
        expect(status.getAttribute('data-phase')).toBe('completed');
        expect(status.textContent).toContain('$');
        expect(status.textContent).toContain('go test');
        expect(status.textContent).not.toMatch(/Coding/);
        // Only one tool line — terminal status is folded into data-phase, not a board header.
        expect(container.querySelectorAll('[data-testid="coding-agent-tool-line"]').length).toBe(1);
        expect(container.querySelector('[data-testid="coding-agent-feed-header"]')).toBeNull();
        expect(status.getAttribute('data-coding-trail')).toBe('plain');
        expect(status.style.boxShadow).toBe('none');
        expect(status.style.background).toMatch(/transparent/i);
    });

    it('shows failure count in multi-line header while phase is still running', () => {
        render(
            <>
                {renderCodingAgentActivityFeed(
                    [
                        makeProgressMsg(
                            'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"failed","command":"go test ./pkg"}',
                            't1',
                        ),
                        makeProgressMsg(
                            'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"write_file","outcome":"success"}',
                            't2',
                        ),
                    ],
                    { text: '#111827', fieldLabel: '#6b7280' },
                    'en',
                )}
            </>,
        );
        const status = screen.getByTestId('coding-agent-progress');
        expect(status.getAttribute('data-phase')).toBe('running');
        expect(status.getAttribute('data-tone-accent')).toBe(CODING_AGENT_FAILURE_ACCENT);
        expect(status.textContent).toContain('go test ./pkg');
        expect(status.textContent).not.toMatch(/1 failed/);
        expect(screen.queryByTestId('coding-agent-feed-header')).toBeNull();
    });

    it('keeps a longer tool trail instead of only the last three rows', () => {
        const rows = Array.from({ length: 5 }, (_, index) => makeProgressMsg(
            `Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"success","command":"echo ${index}"}`,
            `tool-${index}`,
        ));
        const { container } = render(
            <>{renderCodingAgentActivityFeed(rows, { text: '#111827', fieldLabel: '#6b7280' }, 'en')}</>,
        );

        const renderedLines = container.querySelectorAll('[data-testid="coding-agent-tool-line"]');
        expect(renderedLines).toHaveLength(5);
        expect(renderedLines[0].textContent).toContain('echo 0');
        expect(renderedLines[4].textContent).toContain('echo 4');
    });

    it('renders an OpenCode-style file change table with names and line counts', () => {
        const payload = {
            version: 1,
            agent: 'coding',
            event: 'diff_summary',
            phase: 'result',
            task_id: 'T1',
            title: 'Fix parser',
            count: 3,
            added: 14,
            removed: 5,
            files: ['gui/a.go', 'gui/b.go', 'gui/new.go'],
            file_changes: [
                { path: 'gui/a.go', added: 12, removed: 3 },
                { path: 'gui/b.go', added: 0, removed: 2 },
                { path: 'gui/new.go', added: 2, removed: 0 },
            ],
        };
        render(
            <>
                {renderCodingAgentActivityFeed(
                    [makeProgressMsg(`Coding Agent Event: ${JSON.stringify(payload)}`)],
                    { text: '#111827', fieldLabel: '#6b7280' },
                    'zh-Hans',
                )}
            </>,
        );
        const table = screen.getByTestId('coding-agent-file-changes');
        expect(table.textContent).toContain('3 个文件已更改');
        expect(table.textContent).toContain('+14');
        expect(table.textContent).toContain('-5');
        const rows = table.querySelectorAll('[data-testid="coding-agent-file-change-row"]');
        expect(rows).toHaveLength(3);
        expect(rows[0].textContent).toContain('gui/a.go');
        expect(rows[0].textContent).toContain('+12');
        expect(rows[0].textContent).toContain('-3');
        expect(rows[1].textContent).toContain('gui/b.go');
        expect(rows[1].textContent).toContain('+0');
        expect(rows[1].textContent).toContain('-2');
    });

    it('renders a structured assistant note without requiring a tool line first', () => {
        render(
            <>
                {renderCodingAgentActivityFeed(
                    [makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"assistant_note","phase":"running","task_id":"T1","title":"Fix parser","detail":"I found the narrowest safe edit."}')],
                    { text: '#111827', fieldLabel: '#6b7280' },
                    'en',
                )}
            </>,
        );

        expect(screen.getByTestId('coding-agent-assistant-note').textContent).toContain('I found the narrowest safe edit.');
    });

    it('hides leaked reasoning-lane assistant notes instead of rendering tofu squares', () => {
        const payload = JSON.stringify({
            version: 1,
            agent: 'coding',
            event: 'assistant_note',
            phase: 'running',
            task_id: 'T1',
            title: 'Fix parser',
            detail: '\x01The\x01 user\x01 wants me to continue',
        });
        render(
            <>
                {renderCodingAgentActivityFeed(
                    [makeProgressMsg(`Coding Agent Event: ${payload}`)],
                    { text: '#111827', fieldLabel: '#6b7280' },
                    'en',
                )}
            </>,
        );

        expect(screen.queryByTestId('coding-agent-assistant-note')).toBeNull();
    });

    it('maps tool names to Codex trail labels and hides audit rollups from chat', () => {
        expect(codingAgentToolDisplayName('read_file')).toBe('Read');
        expect(codingAgentToolDisplayName('ssh_read_file')).toBe('Read');
        expect(codingAgentToolDisplayName('apply_patch')).toBe('Edit');
        expect(codingAgentToolDisplayName('ssh_write_file')).toBe('Write');
        expect(codingAgentToolDisplayName('bash')).toBe('$');
        expect(codingAgentToolDisplayName('ssh_bash')).toBe('$');
        expect(isCodingAgentChatHiddenEvent({ phase: 'result', title: '', event: 'command_summary' })).toBe(true);
        expect(isCodingAgentChatHiddenEvent({ phase: 'result', title: '', event: 'quality_summary' })).toBe(true);
        expect(isCodingAgentChatHiddenEvent({ phase: 'running', title: '', event: 'tool_finished', detail: 'bash' })).toBe(false);
        expect(isCodingAgentChatHiddenEvent({ phase: 'running', title: '', event: 'assistant_note', detail: 'Compiling the new hello world.' })).toBe(false);
        expect(isCodingAgentChatHiddenEvent({ phase: 'running', title: '', event: 'assistant_note', detail: '## 执行报告' })).toBe(true);
        expect(isCodingAgentChatHiddenEvent({ phase: 'running', title: '', event: 'assistant_note', detail: '\x01The\x01 user\x01 wants' })).toBe(true);
        expect(isCodingAgentPlainTrailEvent({ phase: 'running', title: '', event: 'tool_finished', detail: 'write_file' })).toBe(true);
        expect(isCodingAgentPlainTrailEvent({ phase: 'result', title: '', event: 'quality_summary' })).toBe(false);
        expect(codingAgentMessagesHavePlainTrail([])).toBe(false);
        expect(codingAgentMessagesHavePlainTrail([
            makeProgressMsg('Coding Agent: running T1 - First task'),
        ])).toBe(false);
        expect(codingAgentMessagesHavePlainTrail([
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_started","phase":"running","task_id":"T1","title":"Fix","detail":"read_file"}'),
        ])).toBe(true);
        expect(codingAgentMessagesHavePlainTrail([
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_started","phase":"running","task_id":"T1","turn_id":"turn-1","detail":"read_file"}'),
            makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"task_status","phase":"running","task_id":"T1","turn_id":"turn-2","title":"Next"}'),
        ])).toBe(false);
    });

    it('renders a Codex-style Working trail instead of a chat thinking label', () => {
        const { container } = render(
            <>{renderCodingAgentWorkingTrail({ text: '#111827', fieldLabel: '#6b7280' }, 'zh-Hans')}</>,
        );
        const trail = container.querySelector('[data-testid="coding-agent-working-trail"]');
        expect(trail?.textContent).toContain('Working');
        expect(trail?.textContent).not.toMatch(/思考|处理中/);
        expect(trail?.getAttribute('aria-label')).toBe('工作中');
        expect(trail?.getAttribute('aria-live')).toBe('off');
        expect(trail?.querySelector('.coding-agent-working-dot')).toBeTruthy();
    });

    it('renders apply_patch as Edit in the activity trail', () => {
        const { container } = render(
            <>
                {renderCodingAgentActivityFeed(
                    [makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"apply_patch","outcome":"success","files":["main.go"]}')],
                    { text: '#111827', fieldLabel: '#6b7280' },
                    'en',
                )}
            </>,
        );
        expect(container.querySelector('[data-testid="coding-agent-tool-line"]')?.textContent).toContain('Edit');
        expect(container.querySelector('[data-testid="coding-agent-tool-line"]')?.textContent).toContain('main.go');
    });

    it('collapses completed tool details but keeps a concise receipt visible', () => {
        render(
            <>
                {renderCodingAgentActivityFeed(
                    [makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Delete generated files","turn_id":"turn-1","detail":"bash","outcome":"success","command":"cmd /c del /F /Q build\\\\*.*"}')],
                    { text: '#111827', fieldLabel: '#6b7280' },
                    'en',
                )}
            </>,
        );

        const line = screen.getByTestId('coding-agent-tool-line');
        expect(line.getAttribute('data-tool-collapsed')).toBe('true');
        expect(line.textContent).toContain('Completed');
        const details = line as HTMLDetailsElement;
        expect(details.open).toBe(false);
        fireEvent.click(screen.getByText('Completed'));
        expect(details.open).toBe(true);
        expect(line.textContent).toContain('cmd /c del /F /Q build\\*.*');
    });

    it('keeps a critical completed tool failure expanded', () => {
        render(
            <>
                {renderCodingAgentActivityFeed(
                    [makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"failed","task_id":"T1","title":"Delete generated files","turn_id":"turn-1","detail":"bash","outcome":"failed","severity":"error","command":"cmd /c del /F /Q build\\\\*.*","summary":"Access is denied"}')],
                    { text: '#111827', fieldLabel: '#6b7280' },
                    'en',
                )}
            </>,
        );

        const line = screen.getByTestId('coding-agent-tool-line') as HTMLDetailsElement;
        expect(line.getAttribute('data-tool-collapsed')).toBe('false');
        expect(line.open).toBe(true);
        expect(line.textContent).toContain('cmd /c del /F /Q build\\*.*');
    });

    it('renders Edit/Write lines as Codex file cards with line counts', () => {
        expect(compactCodingTrailPath('D:/workprj/demo/hello_world.cpp')).toBe('demo/hello_world.cpp');
        expect(compactCodingTrailPath('/opt/app/src/pkg/foo.go')).toBe('pkg/foo.go');
        expect(compactCodingTrailPath('src/main.go')).toBe('src/main.go');
        expect(codingAgentEditedFileDetailText({
            phase: 'running',
            title: '',
            event: 'tool_finished',
            detail: 'write_file',
            files: ['hello_world.cpp'],
            added: 8,
            removed: 0,
            fileChanges: [{ path: 'hello_world.cpp', added: 8, removed: 0 }],
        })).toBe('hello_world.cpp (+8 -0)');
        expect(codingAgentEditedFileDetailText({
            phase: 'running',
            title: '',
            event: 'tool_finished',
            detail: 'write_file',
            files: ['D:/workprj/demo/hello_world.cpp'],
            added: 8,
            removed: 0,
            fileChanges: [{ path: 'D:/workprj/demo/hello_world.cpp', added: 8, removed: 0 }],
        })).toBe('demo/hello_world.cpp (+8 -0)');
        expect(codingAgentEditedFileDetailText({
            phase: 'running',
            title: '',
            event: 'diff_updated',
            detail: 'Edited D:/workprj/demo/hello_world.cpp (+8 -0)',
        })).toBe('Edited demo/hello_world.cpp (+8 -0)');
        expect(codingAgentEditedFileDetailText({
            phase: 'running',
            title: '',
            event: 'diff_updated',
            detail: 'Edited D:/workprj/My App/hello world.cpp (+8 -0)',
        })).toBe('Edited My App/hello world.cpp (+8 -0)');
        const { container } = render(
            <>
                {renderCodingAgentActivityFeed(
                    [
                        makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"write_file","outcome":"success","files":["hello_world.cpp"],"added":8,"removed":0,"file_changes":[{"path":"hello_world.cpp","added":8,"removed":0}]}'),
                        makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"assistant_note","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"Compiling the new hello world."}'),
                    ],
                    { text: '#111827', fieldLabel: '#6b7280' },
                    'en',
                )}
            </>,
        );
        expect(container.querySelector('[data-testid="coding-agent-tool-line"]')?.textContent).toContain('hello_world.cpp (+8 -0)');
        expect(container.querySelector('[data-testid="coding-agent-assistant-note"]')?.textContent).toBe('Compiling the new hello world.');
    });

    it('opens the preview when a trail file card is clicked', () => {
        expect(codingAgentPreviewTargetPaths({
            phase: 'running',
            title: '',
            event: 'tool_finished',
            detail: 'write_file',
            files: ['hello_world.cpp'],
        })).toEqual(['hello_world.cpp']);
        const onOpen = vi.fn();
        render(
            <CodingAgentPreviewFocusContext.Provider value={onOpen}>
                {renderCodingAgentActivityFeed(
                    [
                        makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"write_file","outcome":"success","files":["hello_world.cpp"],"added":8,"removed":0,"file_changes":[{"path":"hello_world.cpp","added":8,"removed":0}]}'),
                    ],
                    { text: '#111827', fieldLabel: '#6b7280' },
                    'en',
                )}
            </CodingAgentPreviewFocusContext.Provider>,
        );
        fireEvent.click(screen.getByTestId('coding-agent-preview-link'));
        expect(onOpen).toHaveBeenCalledWith('hello_world.cpp');
    });

    it('opens the preview when a file-change row is clicked', () => {
        const onOpen = vi.fn();
        const payload = {
            version: 1,
            agent: 'coding',
            event: 'diff_summary',
            phase: 'result',
            task_id: 'T1',
            title: 'Fix parser',
            file_changes: [{ path: 'gui/a.go', added: 12, removed: 3 }],
        };
        render(
            <CodingAgentPreviewFocusContext.Provider value={onOpen}>
                {renderCodingAgentActivityFeed(
                    [makeProgressMsg(`Coding Agent Event: ${JSON.stringify(payload)}`)],
                    { text: '#111827', fieldLabel: '#6b7280' },
                    'en',
                )}
            </CodingAgentPreviewFocusContext.Provider>,
        );
        fireEvent.click(screen.getByTestId('coding-agent-file-change-row'));
        expect(onOpen).toHaveBeenCalledWith('gui/a.go');
    });
});
