import { describe, expect, it } from 'vitest';
import { BOT_PENDING_REPLY_WAIT_MS } from './desktopBots';
import { activeRunsOccupy, botBusyPhase, reattachStalePending } from './desktopBotBusy';

const running = { run_id: 'run_6468df58', status: 'running', phase: 'execute' };

function staleId(now: number): string {
    return `m-${(now - BOT_PENDING_REPLY_WAIT_MS - 1000).toString(36)}-old`;
}

describe('bot busy from Hub active runs', () => {
    it('stays busy while Hub lists a running run, even when the bubble is past the clock', () => {
        const now = Date.now();
        const phase = botBusyPhase({
            hubReachable: true,
            runs: [running],
            turnLive: false,
            pending: [{ pending: true, phase: 'execute', stale: true }],
        });
        expect(phase).toBe('execute');
        expect(activeRunsOccupy([running])).toBe(true);
        expect(now).toBeGreaterThan(0);
    });

    it('is free when Hub answers and lists no run', () => {
        expect(botBusyPhase({
            hubReachable: true,
            runs: [],
            turnLive: false,
            pending: [{ pending: true, stale: true }],
        })).toBe('');
    });

    it('uses the clock only when Hub cannot be read', () => {
        expect(botBusyPhase({
            hubReachable: false,
            runs: [],
            turnLive: false,
            pending: [{ pending: true, phase: 'execute', stale: false }],
        })).toBe('execute');
        expect(botBusyPhase({
            hubReachable: false,
            runs: [running],
            turnLive: false,
            pending: [{ pending: true, stale: true }],
        })).toBe('');
    });

    it('keeps a turn this page just admitted before Hub lists it', () => {
        expect(botBusyPhase({
            hubReachable: true,
            runs: [],
            turnLive: true,
            pending: [{ pending: true, phase: 'execute', stale: false }],
        })).toBe('execute');
    });
});

describe('reattach only a bubble that already carries the run', () => {
    it('keeps that bubble pending and takes the run phase', () => {
        const now = Date.now();
        const bubble = { role: 'assistant' as const, pending: true, id: staleId(now), runId: 'run_6468df58', phase: 'plan' as const };
        const next = reattachStalePending([bubble], [running]);
        expect(next[0].pending).toBe(true);
        expect(next[0].runId).toBe('run_6468df58');
        expect(next[0].phase).toBe('execute');
    });

    it('does not attach a bubble that carries no run id', () => {
        const now = Date.now();
        const messages: Array<{ role: 'assistant'; pending: boolean; id: string; runId?: string }> = [
            { role: 'assistant', pending: true, id: staleId(now) },
        ];
        const next = reattachStalePending(messages, [running]);
        expect(next).toBe(messages);
        expect(next[0].runId).toBeUndefined();
    });

    it('does not move a bubble onto a different run', () => {
        const bubble = { role: 'assistant' as const, pending: true, id: 'm-1', runId: 'run_other' };
        const next = reattachStalePending([bubble], [running]);
        expect(next[0].runId).toBe('run_other');
        expect(next).toEqual([bubble]);
    });
});
