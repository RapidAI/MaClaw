import { describe, expect, it } from 'vitest';
import { buddyConfirmTarget, buddyElapsedLabel, buddyFocusTarget, buddyTaskProgress, buildBuddySnapshot, truncateUtf8Bytes, type BuddySnapshot } from '../buddySnapshot';

type Row = Parameters<typeof buildBuddySnapshot>[0][number];

function row(name: string, status?: string, opts: Partial<Row> = {}): Row {
    return {
        id: name,
        name,
        project_path: `D:/tasks/${name}/workspace`,
        active_workflow: status ? { status } : undefined,
        ...opts,
    } as Row;
}

function invariant(snapshot: BuddySnapshot) {
    const c = snapshot.counts;
    expect(c.total).toBe(c.in_progress + c.pending + c.paused + c.done + c.failed);
}

describe('buildBuddySnapshot', () => {
    it('returns zeros for an empty list', () => {
        const snap = buildBuddySnapshot([], null, 1234);
        expect(snap.counts).toEqual({
            total: 0,
            in_progress: 0,
            pending: 0,
            paused: 0,
            done: 0,
            failed: 0,
        });
        expect(snap.current).toEqual([]);
        expect(snap.updated_at).toBe(1234);
    });

    it('partitions rows into exactly one bucket each', () => {
        const snap = buildBuddySnapshot(
            [
                row('run-a', 'running'),
                row('done-b', 'completed'),
                row('pending-c', 'pending'),
                row('paused-d', 'paused'),
                row('failed-e', 'failed'),
            ],
            null,
        );
        expect(snap.counts).toEqual({
            total: 5,
            in_progress: 1,
            pending: 1,
            paused: 1,
            done: 1,
            failed: 1,
        });
        invariant(snap);
        expect(snap.current.map((t) => t.title)).toEqual(['run-a']);
    });

    it('moves a live-running row out of pending instead of double counting', () => {
        const snap = buildBuddySnapshot(
            [row('pending-live', 'pending'), row('pending-idle', 'pending')],
            { busyTaskRuns: { projectPaths: ['D:/tasks/pending-live/workspace'], expertIds: [] } },
        );
        expect(snap.counts).toEqual({
            total: 2,
            in_progress: 1,
            pending: 1,
            paused: 0,
            done: 0,
            failed: 0,
        });
        invariant(snap);
    });

    it('caps the current list at five rows', () => {
        const rows = Array.from({ length: 8 }, (_, i) => row(`run-${i}`, 'running'));
        const snap = buildBuddySnapshot(rows, null);
        expect(snap.counts.in_progress).toBe(8);
        expect(snap.current).toHaveLength(5);
        invariant(snap);
    });

    it('truncates titles on a rune boundary and reports no numeric progress', () => {
        const longTitle = '会'.repeat(40);
        const snap = buildBuddySnapshot([row(longTitle, 'running')], null);
        const title = snap.current[0].title;
        expect(new TextEncoder().encode(title).length).toBeLessThanOrEqual(64);
        expect(title.length % 3).toBe(0);
        expect(longTitle.startsWith(title)).toBe(true);
        expect(snap.current[0].progress).toBe(-1);
    });

    it('names the pending row ahead of a failure and sends a known phase ratio', () => {
        const running = row('impl', 'running', {
            active_workflow: { type: 'coding', status: 'running', phase: 'implementation' },
        });
        const pending = row('need-confirm', 'active', {
            active_workflow: { status: 'active', pending_review: true },
        });
        const failed = row('broke', 'failed');
        const snap = buildBuddySnapshot([failed, running, pending], null);
        expect(snap.current[0].progress).toBe(0.8);
        expect(snap.attention).toEqual({ kind: 'pending', id: 'need-confirm', title: 'need-confirm' });
        expect(buddyTaskProgress(row('mystery', 'running', {
            active_workflow: { type: 'coding', status: 'running', phase: 'nope' },
        }))).toBe(-1);
    });

    it('uses the generated catalog, including the coding review alias, and an age label', () => {
        const now = Date.parse('2026-11-01T12:00:00Z');
        const running = row('impl', 'running', {
            active_workflow: { type: 'coding', status: 'running', phase: 'review' },
            last_activity: '2026-11-01T11:48:00Z',
        });
        const snap = buildBuddySnapshot([running], null, now);
        expect(snap.current[0].progress).toBe(1);
        expect(snap.current[0].phase).toBe('验收确认');
        expect(snap.current[0].elapsed).toBe('12 分钟');
        expect(buddyTaskProgress(row('verify', 'running', {
            active_workflow: { type: 'coding', phase: 'verification' },
        }))).toBe(1);
        expect(buddyTaskProgress(row('draft', 'running', {
            active_workflow: { type: 'paper_writing', phase: 'drafting' },
        }))).toBe(0.6);
        expect(buddyElapsedLabel('2026-11-01T11:59:40Z', now)).toBe('刚刚');
        expect(buddyElapsedLabel('2026-11-01T10:00:00Z', now)).toBe('2 小时');
        expect(buddyElapsedLabel('2026-10-30T12:00:00Z', now)).toBe('2 天');
        const aged = buildBuddySnapshot([
            row('old', 'running', { created_at: '2026-11-01T10:00:00Z' }),
        ], null, now);
        expect(aged.current[0].elapsed).toBe('2 小时');
        expect(aged.current[0].phase).toBeUndefined();
    });

    it('keeps the sidebar bucket for completed rows that have output', () => {
        const snap = buildBuddySnapshot(
            [row('done', 'completed', { has_output: true }), row('fallback-done', '')],
            null,
        );
        // The second row has no workflow status but no output either: `other`.
        expect(snap.counts.done).toBe(1);
        expect(snap.counts.failed).toBe(1);
        invariant(snap);
    });
});

describe('buddyConfirmTarget', () => {
    it('confirms only a row that is waiting', () => {
        const running = row('run-a', 'running');
        const pending = row('need-confirm', 'active', {
            active_workflow: { status: 'active', pending_review: true },
        });
        expect(buddyConfirmTarget([running, pending], null)?.id).toBe('need-confirm');
        expect(buddyConfirmTarget([running], null)).toBeUndefined();
    });

    it('names a failure when nothing is waiting', () => {
        const snap = buildBuddySnapshot([row('broke', 'failed'), row('hold', 'paused')], null);
        expect(snap.attention?.kind).toBe('failed');
        expect(snap.attention?.id).toBe('broke');
    });
});

describe('buddyFocusTarget', () => {
    it('opens the confirmation row ahead of the carousel', () => {
        const running = row('run-a', 'running');
        const pending = row('need-confirm', 'active', {
            active_workflow: { status: 'active', pending_review: true },
        });
        expect(buddyFocusTarget([running, pending], null, 0)?.id).toBe('need-confirm');
    });

    it('opens the task shown on the glass when nothing is waiting', () => {
        const first = row('run-a', 'running');
        const second = row('run-b', 'running');
        expect(buddyFocusTarget([first, second], null, 1)?.id).toBe('run-b');
        expect(buddyFocusTarget([first], null, -1)).toBeUndefined();
    });
});

describe('truncateUtf8Bytes', () => {
    it('never splits a multibyte rune', () => {
        const value = '会议纪要'.repeat(10); // 120 bytes
        for (let max = 1; max <= 30; max++) {
            const out = truncateUtf8Bytes(value, max);
            const bytes = new TextEncoder().encode(out);
            expect(bytes.length).toBeLessThanOrEqual(max);
            expect(() => new TextDecoder('utf-8', { fatal: true }).decode(bytes)).not.toThrow();
            expect(out).not.toContain('\uFFFD');
        }
    });

    it('returns the input when it already fits', () => {
        expect(truncateUtf8Bytes('晴 23°C', 64)).toBe('晴 23°C');
    });
});
