import { describe, expect, it } from 'vitest';
import {
    countActiveBackgroundLoops,
    countLiveAISessions,
    isAILaunchedSession,
    countPassthroughCommands,
    countVisibleScheduledTasks,
    formatWorkbenchTaskCountLine,
} from '../backgroundTaskCount';

describe('countActiveBackgroundLoops', () => {
    it('counts active loops the same way as the background monitor badge', () => {
        expect(countActiveBackgroundLoops([
            { slot_kind: 'ssh', status: 'running' },
            { slotKind: 'ssh', status: 'paused' },
            { slot_kind: ' SSH ', status: ' Running ' },
            { SlotKind: 'ssh', Status: 'completed' },
            { slot_kind: 'coding', status: 'running' },
            { slot_kind: 'browser', status: 'paused' },
            null,
        ])).toBe(5);
    });

    it('treats non-array background loop payloads as empty', () => {
        expect(countActiveBackgroundLoops(null)).toBe(0);
        expect(countActiveBackgroundLoops({ slot_kind: 'ssh', status: 'running' })).toBe(0);
    });
});

describe('workbench monitor task counts', () => {
    it('counts live AI sessions for the background tab', () => {
        expect(isAILaunchedSession({ launchSource: 'AI' })).toBe(true);
        expect(isAILaunchedSession({ launch_source: 'desktop' })).toBe(false);
        expect(countLiveAISessions([
            { launch_source: 'ai', status: 'running' },
            { launchSource: 'AI', status: 'paused' },
            { launch_source: 'ai', status: 'completed' },
            { launch_source: 'desktop', status: 'running' },
        ])).toBe(2);
    });

    it('counts non-expired scheduled tasks and all passthrough commands', () => {
        expect(countVisibleScheduledTasks([
            { status: 'active' },
            { status: 'paused' },
            { status: 'expired' },
            { status: 'Expired' },
        ])).toBe(2);
        expect(countVisibleScheduledTasks(null)).toBe(0);
        expect(countPassthroughCommands([{ name: 'a' }, { name: 'b' }])).toBe(2);
        expect(countPassthroughCommands(null)).toBe(0);
    });

    it('formats the three remaining monitor counts on one line', () => {
        expect(formatWorkbenchTaskCountLine(
            { background: 1, scheduled: 0, passthrough: 4 },
            { background: '后台', scheduled: '计划', passthrough: '直通' },
            ' · ',
        )).toBe('后台 1 · 计划 0 · 直通 4');
    });
});
