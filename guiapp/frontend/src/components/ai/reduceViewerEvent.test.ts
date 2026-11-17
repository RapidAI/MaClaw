import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { emptyViewerState, reduceViewerEvent, type ViewerReduceState } from './reduceViewerEvent';

const vectors = JSON.parse(readFileSync(join(dirname(fileURLToPath(import.meta.url)), '../../../../testdata/viewer_contract_vectors.json'), 'utf8')) as {
    cases: Array<{ name: string; events: Record<string, unknown>[]; expect: Record<string, unknown> }>;
};

function fold(events: Record<string, unknown>[]): ViewerReduceState {
    return events.reduce((state, event) => reduceViewerEvent(state, event), emptyViewerState());
}

function expectSubset(actual: unknown, expected: unknown): void {
    if (Array.isArray(expected)) {
        expect(Array.isArray(actual)).toBe(true);
        expected.forEach((item, index) => expectSubset((actual as unknown[])[index], item));
        return;
    }
    if (expected && typeof expected === 'object') {
        const record = actual as Record<string, unknown>;
        for (const [key, value] of Object.entries(expected as Record<string, unknown>)) {
            expectSubset(record[key], value);
        }
        return;
    }
    expect(actual).toEqual(expected);
}

describe('reduceViewerEvent golden vectors', () => {
    for (const item of vectors.cases) {
        it(item.name, () => {
            expectSubset(fold(item.events), item.expect);
        });
    }

    it('does not stringify the live parts list', () => {
        const source = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '../layout/remoteAssistantSession.ts'), 'utf8');
        expect(source.includes('JSON.stringify(session.live.parts)')).toBe(false);
    });
});

const wireSnapshots = JSON.parse(readFileSync(join(dirname(fileURLToPath(import.meta.url)), '../../../../testdata/viewer_wire_snapshots.json'), 'utf8')) as {
    watch_event: Record<string, unknown>;
    responded_event: Record<string, unknown>;
};

function assertMarshaledSnapshot(event: Record<string, unknown>, body: string, responded: boolean): void {
    const live = event.live as Record<string, unknown>;
    expect(live.text).toBeUndefined();
    expect(Array.isArray(live.parts)).toBe(true);
    const textParts = (live.parts as Array<Record<string, unknown>>).filter(part => part.kind === 'text').map(part => part.text);
    expect(textParts).toContain(body);
    const state = reduceViewerEvent(emptyViewerState(), event);
    expect(state.body).toBe(body);
    expect(state.last).toBe(event.cursor);
    expect(state.last).toBe(live.cursor);
    expect(state.gap).toBe(false);
    expect(state.responded).toBe(responded);
    expect(state.requestId).toBe('r1');
    expect(state.displayText).toBe('问');
    expect(state.thinking).toEqual([{ text: '想想', done: true }]);
    expect(state.timeline.map(row => ({ phase: row.phase, taskId: row.taskId, title: row.title, runId: row.runId }))).toEqual([
        { phase: 'running', taskId: 'T2', title: 'edit parser', runId: 'run-1' },
    ]);
}

describe('marshaled wire snapshots', () => {
    it('reduces the watch_task live and the responded snapshotFrameLocked envelope', () => {
        assertMarshaledSnapshot(wireSnapshots.watch_event, 'helloworld', false);
        assertMarshaledSnapshot(wireSnapshots.responded_event, '终稿', true);
        const live = wireSnapshots.watch_event.live as Record<string, unknown>;
        const seeded = reduceViewerEvent(emptyViewerState(), {
            event: 'snapshot',
            resync: true,
            cursor: live.cursor,
            live,
        });
        const nextCursor = Number(live.cursor) + 1;
        const next = reduceViewerEvent(seeded, {
            event: 'token',
            cursor: nextCursor,
            cursor_from: nextCursor,
            text: '续',
        });
        expect(next.body).toBe('helloworld续');
        expect(next.last).toBe(nextCursor);
        expect(next.gap).toBe(false);
        expect(next.thinking).toEqual([{ text: '想想', done: true }]);
    });
});

describe('resync snapshot preserves same-round event state', () => {
    const base = (): Record<string, unknown>[] => [
        { event: 'round_started', request_id: 'req-1', cursor: 1 },
        { event: 'token', text: 'hello', cursor: 2, cursor_from: 2 },
        { event: 'card', kind: 'confirmation', request_id: 'req-1', confirmation: { id: 'c1' }, actions: [{ id: 'approve', label: 'OK' }], cursor: 3, cursor_from: 3 },
        { event: 'card', kind: 'notice', request_id: 'req-1', text: 'heads up', cursor: 4, cursor_from: 4 },
    ];

    it('keeps cards and notices when a same-request resync lands', () => {
        let state = fold(base());
        state = reduceViewerEvent(state, {
            event: 'snapshot', resync: true, cursor: 4,
            live: { request_id: 'req-1', parts: [{ kind: 'text', seq: 1, text: 'hello' }] },
        });
        expect(state.cards.length).toBe(1);
        expect(state.notices.length).toBe(1);
        expect(state.body).toBe('hello');
        expect(state.last).toBe(4);
    });

    it('drops event state when the resync belongs to a different request', () => {
        let state = fold(base());
        state = reduceViewerEvent(state, {
            event: 'snapshot', resync: true, cursor: 10,
            live: { request_id: 'req-2', parts: [{ kind: 'text', seq: 1, text: 'fresh' }] },
        });
        expect(state.cards.length).toBe(0);
        expect(state.notices.length).toBe(0);
        expect(state.body).toBe('fresh');
        expect(state.requestId).toBe('req-2');
    });
});
