import { describe, expect, it } from 'vitest';
import { buildBuddySnapshot, truncateUtf8Bytes, type BuddySnapshot } from '../buddySnapshot';

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
