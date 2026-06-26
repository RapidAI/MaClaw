// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import type { ChatMessage } from '../useAIAssistant';
import {
    activeCodingAgentProgress,
    codingAgentCommandStatusLabel,
    codingAgentCommandStatusTone,
    codingAgentCompactText,
    codingAgentDiffCheckStatusLabel,
    codingAgentDiffCheckStatusTone,
    codingAgentDisplayText,
    codingAgentExplorationStatusLabel,
    codingAgentExplorationStatusTone,
    codingAgentFileActivityStatusLabel,
    codingAgentFileActivityStatusTone,
    codingAgentFilePreviewText,
    codingAgentGuardrailStatusLabel,
    codingAgentGuardrailStatusTone,
    codingAgentQualityStatusLabel,
    codingAgentQualityStatusTone,
    codingAgentProgressTone,
    formatCodingAgentDuration,
    codingAgentStatusClassName,
    codingAgentStatusDataAttrs,
    codingAgentStatusLabel,
    codingAgentStatusSelector,
    codingAgentStatusTone,
    codingAgentToolOutcomeLabel,
    codingAgentToolOutcomeTone,
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
    renderCodingAgentProgressStatus,
} from '../CodingAgentProgressStatus';

const makeProgressMsg = (content: string, id = content): ChatMessage => ({
    id,
    role: 'progress',
    content,
    timestamp: 1,
});

describe('CodingAgentProgressStatus', () => {
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

        expect(snapshot && codingAgentTurnSnapshotText(snapshot, 'en')).toBe('Coding Agent | Task status | Result | T1 | 2 files | First task | a.go, b.go | Trace: read_file Blocked 1.3s | Tool: read_file | Result: blocked | Duration: 1.3s | Guard: Blocked (blocked | bash | category:git) | Commands: None (no bash commands run) | Activity: None (read 0 / modified 0 / created 0) | Quality: Passed (no file changes; quality gates not needed) | Explore: Missing (no reads before editing) | Verify: Not run (no verification command detected) | Diff check: Skipped (no modified files) | Files: a.go, b.go | Diff: 2 files');
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
        expect(codingAgentCommandStatusTone('failed').accent).toBe('#c43d34');
    });

    it('normalizes coding-agent file activity labels and tones', () => {
        expect(codingAgentFileActivityStatusLabel('changed', 'en')).toBe('Changed');
        expect(codingAgentFileActivityStatusLabel('read_only', 'en')).toBe('Read only');
        expect(codingAgentFileActivityStatusLabel('none', 'zh-Hans')).toBe('\u65e0\u52a8\u4f5c');
        expect(codingAgentFileActivityStatusTone('changed').accent).toBe('#4f7f6f');
        expect(codingAgentFileActivityStatusTone('read_only').accent).toBe('#2f5f98');
    });

    it('normalizes coding-agent quality labels and tones', () => {
        expect(codingAgentQualityStatusLabel('passed', 'en')).toBe('Passed');
        expect(codingAgentQualityStatusLabel('warning', 'en')).toBe('Warning');
        expect(codingAgentQualityStatusLabel('failed', 'zh-Hans')).toBe('\u5931\u8d25');
        expect(codingAgentQualityStatusTone('passed').accent).toBe('#4f7f6f');
        expect(codingAgentQualityStatusTone('warning').accent).toBe('#64748b');
        expect(codingAgentQualityStatusTone('failed').accent).toBe('#c43d34');
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
        expect(codingAgentVerificationStatusLabel('failed', 'zh-Hans')).toBe('\u5931\u8d25');
        expect(codingAgentVerificationStatusTone('failed').accent).toBe('#c43d34');
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
        expect(codingAgentToolOutcomeTone('failed').accent).toBe('#c43d34');
        expect(codingAgentToolOutcomeTone('blocked').accent).toBe('#64748b');
        expect(formatCodingAgentDuration(0)).toBe('0ms');
        expect(formatCodingAgentDuration(250)).toBe('250ms');
        expect(formatCodingAgentDuration(1250)).toBe('1.3s');
        expect(formatCodingAgentDuration(65000)).toBe('1m 5s');
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
            .toBe('Coding Agent | Running | T2 | Fix stale edit guard');
        expect(codingAgentDisplayText({ phase: 'running', taskID: 'T2', title: 'Fix stale edit guard' }, 'zh-Hans'))
            .toBe('\u7f16\u7a0b\u667a\u80fd\u4f53 | \u6267\u884c\u4e2d | T2 | Fix stale edit guard');
        expect(codingAgentCompactText({ phase: 'running', taskID: 'T2', title: 'Fix stale edit guard' }, 'zh-Hans'))
            .toBe('\u7f16\u7a0b\u667a\u80fd\u4f53 | \u6267\u884c\u4e2d | T2');
        expect(codingAgentCompactText({ phase: 'running', taskID: ' t2 ', title: 'Fix stale edit guard' }, 'en'))
            .toBe('Coding Agent | Running | T2');
        expect(codingAgentCompactText({ phase: 'result', taskID: ' t2 ', title: 'Fix stale edit guard', event: 'diff_summary', detail: '3 files' }, 'en'))
            .toBe('Coding Agent | Result | T2 | 3 files');
        expect(codingAgentCompactText({ phase: 'result', taskID: 'T2', title: 'No commands', event: 'command_summary', count: 0 }, 'en'))
            .toBe('Coding Agent | Result | T2 | 0 commands');
        expect(codingAgentCompactText({ phase: 'result', taskID: 'T2', title: 'Clean quality', event: 'quality_summary', count: 0 }, 'en'))
            .toBe('Coding Agent | Result | T2 | 0 issues');
        expect(codingAgentCompactText({ phase: 'result', taskID: 'T2', title: 'No diff', event: 'diff_check', count: 0 }, 'zh-Hans'))
            .toBe('\u7f16\u7a0b\u667a\u80fd\u4f53 | \u7ed3\u679c | T2 | 0 \u4e2a\u53d8\u66f4');
        expect(codingAgentDisplayText({ phase: 'running', taskID: ' t2 ', title: '  Fix stale edit guard  ' }, 'en'))
            .toBe('Coding Agent | Running | T2 | Fix stale edit guard');
        expect(codingAgentVariantDisplayText({ phase: 'running', taskID: ' t2 ', title: '  Fix stale edit guard  ' }, 'en', 'sidebar'))
            .toBe('Coding Agent | Task status | Running | T2 | Fix stale edit guard');
        expect(codingAgentVariantDisplayText({ phase: 'running', taskID: ' t2 ', title: '  Fix stale edit guard  ' }, 'en', 'status-bar'))
            .toBe('Coding Agent | Running | T2 | Fix stale edit guard');
        expect(codingAgentFilePreviewText({ phase: 'result', taskID: 'T2', title: 'Done', files: ['a.go', 'b.go', 'c.go', 'd.go'] }, 'en'))
            .toBe('a.go, b.go, c.go +1 more');
        expect(codingAgentFilePreviewText({ phase: 'result', taskID: 'T2', title: 'Done', files: ['a.go', 'b.go', 'c.go', 'd.go'] }, 'zh-Hans'))
            .toBe('a.go, b.go, c.go \u7b49 1 \u4e2a');
    });

    it('maps status phases to distinct semantic tones', () => {
        expect(codingAgentStatusTone('running').accent).toBe('#2f5f98');
        expect(codingAgentStatusTone('retrying').accent).toBe('#64748b');
        expect(codingAgentStatusTone('failed').accent).toBe('#c43d34');
        expect(codingAgentStatusTone('completed').accent).toBe('#4f7f6f');
        expect(codingAgentStatusTone('result').accent).toBe('#4f7f6f');
        expect(codingAgentStatusTone('skipped').accent).toBe('#64748b');
        expect(codingAgentStatusTone('queued').accent).toBe('#2f5f98');
        expect(codingAgentStatusLabel('queued', 'en')).toBe('Status');
        expect(codingAgentStatusLabel('queued', 'zh-Hans')).toBe('\u72b6\u6001');
    });

    it('uses event outcome tones for coding-agent summary progress rows', () => {
        expect(codingAgentProgressTone({ phase: 'result', title: 'Quality summary', event: 'quality_summary', outcome: 'failed' }).accent).toBe('#c43d34');
        expect(codingAgentProgressTone({ phase: 'result', title: 'Verification summary', event: 'verification_summary', outcome: 'missing' }).accent).toBe('#64748b');
        expect(codingAgentProgressTone({ phase: 'result', title: 'Diff check', event: 'diff_check', outcome: 'checked' }).accent).toBe('#4f7f6f');
        expect(codingAgentProgressTone({ phase: 'failed', title: 'Failed' }).accent).toBe('#c43d34');
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

    it('renders an accessible visible status row for chat progress', () => {
        render(
            <>
                {renderCodingAgentProgressStatus(
                    makeProgressMsg('Coding Agent: failed T4 - Apply patch'),
                    { text: '#111827', fieldLabel: '#6b7280' },
                    'en',
                )}
            </>,
        );

        const status = screen.getByTestId('coding-agent-progress');
        expect(status.textContent).toContain('Coding Agent');
        expect(status.textContent).toContain('Failed');
        expect(status.textContent).toContain('T4');
        expect(status.textContent).toContain('Apply patch');
        expect(status.getAttribute('role')).toBe('status');
        expect(status.getAttribute('aria-live')).toBe('polite');
        expect(status.getAttribute('aria-label')).toBe('Coding Agent | Failed | T4 | Apply patch');
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
    });

    it('renders quality summary failures as failed chat progress instead of generic result rows', () => {
        render(
            <>
                {renderCodingAgentProgressStatus(
                    makeProgressMsg('Coding Agent Event: {"version":1,"agent":"coding","event":"quality_summary","phase":"result","task_id":"T4","title":"Apply patch","outcome":"failed","summary":"verification not run","count":1}'),
                    { text: '#111827', fieldLabel: '#6b7280' },
                    'en',
                )}
            </>,
        );

        const status = screen.getByTestId('coding-agent-progress');
        expect(status.textContent).toContain('Quality Failed');
        expect(status.textContent).toContain('1 issues');
        expect(status.textContent).toContain('T4');
        expect(status.getAttribute('aria-label')).toBe('Coding Agent | Quality Failed | T4 | 1 issues | Apply patch');
        expect(status.style.border).toContain('rgba(196, 61, 52, 0.22)');
    });
});
