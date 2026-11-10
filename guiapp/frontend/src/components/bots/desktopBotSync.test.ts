import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import {
    appendDesktopBotReport,
    messagesForBot,
    saveBotMessages,
    type DesktopBotMessage,
} from './desktopBots';
import {
    applyDesktopBotSync,
    flushDesktopBotSync,
    mergeSyncEntries,
    noteDesktopBotCleared,
    resetDesktopBotSyncForTest,
    syncProjection,
    syncWrites,
    type DesktopBotSyncEntry,
} from './desktopBotSync';

const sorted = (v: unknown): unknown => Array.isArray(v) ? v.map(sorted) : v && typeof v === 'object'
    ? Object.fromEntries(Object.keys(v as object).sort().map(k => [k, sorted((v as Record<string, unknown>)[k])])) : v;
const same = (a: unknown, b: unknown) => JSON.stringify(sorted(a)) === JSON.stringify(sorted(b));

// A tiny Hub journal with the same rules as hub/internal/botmgmt:
// upsert by bot+id, one seq per change, identical data is no change, a patch
// only turns true flags on, the first writer stays the origin.
class Journal {
    head = 0;
    entries: DesktopBotSyncEntry[] = [];
    write(origin: string, writes: Array<{ bot_id: string; id: string; patch?: boolean; data: Record<string, unknown> }>) {
        for (const w of writes) {
            const at = this.entries.findIndex(e => e.bot_id === w.bot_id && e.id === w.id);
            if (w.patch) {
                if (at < 0) continue;
                const base = { ...(this.entries[at].data || {}) };
                let changed = false;
                for (const [k, v] of Object.entries(w.data)) {
                    if (v === true && base[k] !== true) { base[k] = true; changed = true; }
                }
                if (!changed) continue;
                const e = this.entries.splice(at, 1)[0];
                this.entries.push({ ...e, seq: ++this.head, data: base, by: origin });
                continue;
            }
            if (at >= 0 && same(this.entries[at].data, w.data)) continue;
            const prev = at >= 0 ? this.entries.splice(at, 1)[0] : undefined;
            const seq = ++this.head;
            this.entries.push({ bot_id: w.bot_id, id: w.id, seq, created_seq: prev?.created_seq || seq, origin: prev?.origin || origin, by: origin, data: w.data });
        }
    }
    read(after: number, self: string) {
        return this.entries.filter(e => e.seq > after && (e.by || e.origin) !== self).sort((a, b) => a.seq - b.seq);
    }
}

// One GUI: its own transcript, cursor and published marks.
class Client {
    messages: DesktopBotMessage[] = [];
    cursor = 0;
    own: string[] | undefined;
    marks = new Map<string, string>();
    constructor(readonly name: string, readonly journal: Journal) {}
    publish(tombstones = true) {
        const { writes, marks, own } = syncWrites('alice', 'bot_1', this.messages, this.own, (k, fp) => this.marks.get(k) === fp, tombstones);
        this.own = own;
        this.journal.write(this.name, writes);
        for (const [k, fp] of marks) this.marks.set(k, fp);
        return writes;
    }
    pull() {
        const entries = this.journal.read(this.cursor, this.name);
        this.cursor = this.journal.head;
        const merged = mergeSyncEntries(this.messages, entries);
        this.messages = merged.messages;
        for (const m of merged.messages) {
            if (!m.syncSeq) continue;
            const flags: Record<string, true> = {};
            if (m.askResolved) flags.askResolved = true;
            if (m.optionsUsed) flags.optionsUsed = true;
            if (Object.keys(flags).length) this.marks.set(`alice\0bot_1\0${m.id}`, `patch:${JSON.stringify(flags)}`);
        }
        return merged.changed;
    }
}

const at = (ms: number, tag: string) => `m-${ms.toString(36)}-${tag}`;

describe('multi-client conversation sync', () => {
    it('shows every step of one turn on the other client once', () => {
        const journal = new Journal();
        const a = new Client('A', journal);
        const b = new Client('B', journal);
        const t0 = 1_800_000_000_000;
        // A: the person's message and the instant acknowledgement.
        a.messages = [
            { id: at(t0, 'u1'), role: 'user', content: '发一篇文章', queued: true },
            { id: at(t0 + 1, 'ack'), role: 'assistant', content: '好的，我来发。', ack: true, options: ['发到知乎'] },
            { id: at(t0 + 2, 'p'), role: 'assistant', content: '', pending: true, dispatched: true, requestId: 'desktop-bot-1' },
        ];
        a.publish();
        expect(b.pull()).toBe(true);
        expect(b.messages.map(m => m.content)).toEqual(['发一篇文章', '好的，我来发。']);
        // Shown only: B never sends, queues or redirects A's turn.
        expect(b.messages.some(m => m.pending || m.queued || m.dispatched || m.options)).toBe(false);
        expect(b.messages.every(m => m.syncOrigin === 'A' && (m.syncSeq || 0) > 0)).toBe(true);

        // The ack line is upgraded in place, then the run settles to a card.
        a.messages[1] = { ...a.messages[1], content: '好的，正在写稿。' };
        a.messages[2] = { ...a.messages[2], pending: false, content: '要发到哪个账号？', askUser: { question: '要发到哪个账号？', options: ['A', 'B'] }, runId: 'run_1' };
        a.publish();
        b.pull();
        expect(b.messages.map(m => m.content)).toEqual(['发一篇文章', '好的，正在写稿。', '要发到哪个账号？']);
        const card = b.messages[2];
        expect(card.runId).toBe('run_1');
        expect(card.askUser?.options).toEqual(['A', 'B']);

        // B answers the card: it is answered on A too, and B's words show once.
        b.messages = b.messages.map(m => m.id === card.id ? { ...m, askResolved: true } : m);
        b.messages.push({ id: at(t0 + 5, 'u2'), role: 'user', content: 'A' });
        const sent = b.publish();
        expect(sent.find(w => w.id === card.id)).toMatchObject({ patch: true, data: { askResolved: true } });
        a.pull();
        expect(a.messages.find(m => m.id === card.id)?.askResolved).toBe(true);
        expect(a.messages.filter(m => m.content === 'A')).toHaveLength(1);
        // Nothing echoes back and forth: A restating its card with the
        // flag Hub already holds is no change there.
        const head = journal.head;
        a.publish();
        expect(b.publish()).toEqual([]);
        expect(journal.head).toBe(head);
        expect(a.pull()).toBe(false);
        expect(b.pull()).toBe(false);
    });

    it('keeps one bubble per run when a delivery and a reply meet', () => {
        const journal = new Journal();
        const a = new Client('A', journal);
        const b = new Client('B', journal);
        a.messages = [{ id: at(1_800_000_000_000, 'r'), role: 'assistant', content: '写好了', runId: 'run_9', taskOutcome: 'done' } as DesktopBotMessage];
        // B pulled the same finished run as a delivery before A acked it.
        b.messages = [{ id: at(1_800_000_000_100, 'd'), role: 'assistant', content: '写好了', requestId: 'desktop-bot-deliver-run_9', runId: 'run_9' }];
        a.publish();
        b.publish();
        a.pull();
        b.pull();
        expect(a.messages).toHaveLength(1);
        expect(b.messages).toHaveLength(1);
    });

    it('backfills after a reconnect and is idempotent', () => {
        const journal = new Journal();
        const a = new Client('A', journal);
        a.messages = [
            { id: at(1_800_000_000_000, 'u'), role: 'user', content: '一' },
            { id: at(1_800_000_000_010, 'r'), role: 'assistant', content: '二' },
        ];
        a.publish();
        const late = new Client('B', journal);
        late.messages = [{ id: at(1_800_000_000_005, 'own'), role: 'user', content: 'B 的' }];
        late.pull();
        // Time order, not arrival order.
        expect(late.messages.map(m => m.content)).toEqual(['一', 'B 的', '二']);
        late.cursor = 0;
        expect(late.pull()).toBe(false);
    });

    it('drops a bubble the writer removed, but not one that fell off the window', () => {
        const journal = new Journal();
        const a = new Client('A', journal);
        const b = new Client('B', journal);
        const t0 = 1_800_000_000_000;
        a.messages = [
            { id: at(t0, 'old'), role: 'user', content: 'old' },
            { id: at(t0 + 1, 'u'), role: 'user', content: 'u' },
            { id: at(t0 + 2, 'reading'), role: 'assistant', content: '我理解为…', ack: true, understood: true },
        ];
        a.publish();
        b.pull();
        // The plan replaced the reading; the oldest bubble left the window.
        a.messages = [a.messages[1], { id: at(t0 + 3, 'plan'), role: 'assistant', content: '计划：…' }];
        a.publish();
        b.pull();
        expect(b.messages.map(m => m.content)).toEqual(['old', 'u', '计划：…']);
    });

    it('keeps a /clear local and does not bring cleared lines back', () => {
        const journal = new Journal();
        const a = new Client('A', journal);
        const b = new Client('B', journal);
        a.messages = [{ id: at(1_800_000_000_000, 'u'), role: 'user', content: 'x' }];
        a.publish();
        b.pull();
        const clearedAt = journal.head;
        b.messages = [];
        b.publish(false);
        a.pull();
        expect(a.messages).toHaveLength(1);
        const merged = mergeSyncEntries([], journal.read(0, 'B'), clearedAt);
        expect(merged.messages).toHaveLength(0);
    });

    it('projects only settled, shareable fields', () => {
        expect(syncProjection({ id: 'm-1', role: 'assistant', content: '', pending: true })).toBeNull();
        expect(syncProjection({ id: 'm-1', role: 'user', content: 'x', noReading: { text: 'x', phase: 'execute', kind: 'unread' } })).toBeNull();
        const out = syncProjection({ id: 'm-1', role: 'assistant', content: 'ok', requestId: 'local:1', images: [{ mime: 'image/png', data: 'AAA' }], shotPaths: ['C:/x.png'], localPaths: ['C:/a.docx'], queued: true, crossNoteId: 'n1' } as DesktopBotMessage);
        expect(out).toEqual({ role: 'assistant', content: 'ok' });
    });

    it('does not replace a written acknowledgement with a later receipt for the same bubble', () => {
        const id = at(1_800_000_000_100, 'ack');
        const stored = [
            { id, role: 'assistant' as const, content: '我去网上查黄仁勋的资料，做成带照片的简历发给你。', ack: true, understood: true, syncSeq: 2, syncOrigin: 'A' },
        ];
        const merged = mergeSyncEntries(stored, [{
            bot_id: 'bot_1', id, seq: 1, created_seq: 1, origin: 'A',
            data: { role: 'assistant', content: '收到，正在看。', ack: true, understood: true },
        }]);
        expect(merged.changed).toBe(false);
        expect(merged.messages[0].content).toBe('我去网上查黄仁勋的资料，做成带照片的简历发给你。');
    });
});

describe('sync through the stored transcript', () => {
    const writes: Array<{ owner: string; writes: unknown[] }> = [];
    beforeEach(() => {
        window.localStorage.clear();
        writes.length = 0;
        resetDesktopBotSyncForTest(async (owner, w) => { writes.push({ owner, writes: w }); });
    });
    afterEach(() => resetDesktopBotSyncForTest());

    it('publishes a saved transcript for a signed-in account only', async () => {
        saveBotMessages('alice', 'bot_1', [{ id: 'm-1', role: 'user', content: 'hi' }]);
        saveBotMessages('', 'bot_1', [{ id: 'm-2', role: 'user', content: 'local' }]);
        await flushDesktopBotSync();
        expect(writes).toHaveLength(1);
        expect(writes[0].owner).toBe('alice');
        expect(writes[0].writes).toEqual([{ bot_id: 'bot_1', id: 'm-1', kind: 'message', data: { role: 'user', content: 'hi' } }]);
        saveBotMessages('alice', 'bot_1', messagesForBot('alice', 'bot_1'));
        await flushDesktopBotSync();
        expect(writes).toHaveLength(1);
    });

    it('merges another client per user and per bot, without echo', async () => {
        saveBotMessages('alice', 'bot_1', [{ id: 'm-1', role: 'user', content: 'mine' }]);
        await flushDesktopBotSync();
        writes.length = 0;
        applyDesktopBotSync({
            owner_id: 'alice', head: 3, entries: [
                { bot_id: 'bot_1', id: 'm-9', seq: 2, created_seq: 2, origin: 'B', data: { role: 'assistant', content: 'from B', askUser: { question: 'q' }, askResolved: true } },
                { bot_id: 'bot_2', id: 'm-8', seq: 3, created_seq: 3, origin: 'B', data: { role: 'user', content: 'other bot' } },
            ],
        });
        expect(messagesForBot('alice', 'bot_1').map(m => m.content)).toEqual(['mine', 'from B']);
        expect(messagesForBot('alice', 'bot_2').map(m => m.content)).toEqual(['other bot']);
        expect(messagesForBot('bob', 'bot_1')).toEqual([]);
        await flushDesktopBotSync();
        expect(writes).toEqual([]);
        // Bob's GUI page on the same machine is a different transcript key.
        applyDesktopBotSync({ owner_id: 'bob', entries: [{ bot_id: 'bot_1', id: 'm-7', seq: 1, created_seq: 1, origin: 'X', data: { role: 'user', content: 'bob' } }] });
        expect(messagesForBot('alice', 'bot_1').map(m => m.content)).toEqual(['mine', 'from B']);
    });

    it('keeps a delivery off a run another client already showed', () => {
        applyDesktopBotSync({ owner_id: 'alice', entries: [{ bot_id: 'bot_1', id: 'm-r', seq: 1, created_seq: 1, origin: 'A', data: { role: 'assistant', content: '写好了', runId: 'run_5' } }] });
        appendDesktopBotReport('alice', 'bot_1', { content: '写好了', failed: false, requestId: 'desktop-bot-deliver-run_5', runId: 'run_5' });
        expect(messagesForBot('alice', 'bot_1')).toHaveLength(1);
    });

    it('does not resurrect lines after /clear', () => {
        applyDesktopBotSync({ owner_id: 'alice', head: 4, entries: [{ bot_id: 'bot_1', id: 'm-1', seq: 4, created_seq: 4, origin: 'A', data: { role: 'user', content: 'old' } }] });
        noteDesktopBotCleared('alice', 'bot_1');
        saveBotMessages('alice', 'bot_1', []);
        applyDesktopBotSync({ owner_id: 'alice', head: 5, reset: true, entries: [
            { bot_id: 'bot_1', id: 'm-1', seq: 4, created_seq: 4, origin: 'A', data: { role: 'user', content: 'old' } },
            { bot_id: 'bot_1', id: 'm-2', seq: 5, created_seq: 5, origin: 'A', data: { role: 'user', content: 'new' } },
        ] });
        expect(messagesForBot('alice', 'bot_1').map(m => m.content)).toEqual(['new']);
    });
});
