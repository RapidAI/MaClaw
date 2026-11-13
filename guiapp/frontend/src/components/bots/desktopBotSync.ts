import { EventsOn } from '../../../wailsjs/runtime';
import { PublishDesktopBotConversation, StartDesktopBotConversationSync } from '../../../wailsjs/go/main/App';
import {
    botMessageStartedAt,
    desktopUserKey,
    eachDesktopBotTranscript,
    messagesForBot,
    saveBotMessages,
    setDesktopBotTranscriptSyncHook,
    type DesktopBotMessage,
} from './desktopBots';
import { desktopBotIsPlaceholderAck } from './desktopBotAck';

// Multi-client conversation sync.
//
// One person can have the same bot open in several GUIs. Each GUI keeps its
// own transcript (localStorage); this module makes them the same chat:
//
//  - every transcript write offers its settled bubbles to Hub's journal,
//    keyed by the bubble id (the person's message, the instant
//    acknowledgement and its upgrade, the reply with its ask card, outcome
//    and run id, the delivery of a missed run, bot-to-bot feed lines);
//  - a bubble that disappears here (a reading folded into the plan) is
//    written as deleted, so the other clients drop it as well;
//  - a bubble another client wrote is merged in by id, in time order. It is
//    display only: it is never pending, queued or dispatched here, so this
//    client never sends or recovers someone else's turn. Answering its ask
//    card marks it answered for every client (a flag patch);
//  - the same run shown twice (this client's delivery and another client's
//    reply) is recognised by its run id and kept once.
//
// The run's progress (busy and the working-row step) is synced by the Go
// side; /clear stays local to this client.

export const BOT_SYNCED_EVENT = 'maclaw-bot-synced';

export interface DesktopBotSyncEntry {
    bot_id: string;
    id: string;
    seq: number;
    created_seq: number;
    origin?: string;
    by?: string;
    data: Record<string, unknown> | null;
}

type Write = { bot_id: string; id: string; kind?: string; patch?: boolean; data: Record<string, unknown> };

const LOCAL_ONLY: Array<keyof DesktopBotMessage> = [
    'id', 'pending', 'dispatched', 'queued', 'images', 'files', 'shotPaths', 'localPaths',
    'crossNoteId', 'noReading', 'syncSeq', 'syncOrigin',
];
// Hub keeps one entry under 32 KB; a long reply is cut, not refused.
const CONTENT_MAX = 6000;
const CLEARED_KEY = 'maclaw.desktopBotSyncCleared.v1';
const FLUSH_MS = 250;
const RETRY_MS = 15_000;

// syncProjection is what other clients may see of one local bubble, or null
// for a bubble that is not settled yet (its reply is still on the way) or a
// parked one (its controls send from this client only).
export function syncProjection(message: DesktopBotMessage): Record<string, unknown> | null {
    if (!message || !message.id || (message.role !== 'user' && message.role !== 'assistant')) return null;
    if (message.pending || message.noReading) return null;
    const out: Record<string, unknown> = { ...message };
    for (const key of LOCAL_ONLY) delete out[key];
    if (typeof out.requestId === 'string' && out.requestId.startsWith('local:')) delete out.requestId;
    if (typeof out.content === 'string' && out.content.length > CONTENT_MAX) out.content = out.content.slice(0, CONTENT_MAX);
    for (const key of Object.keys(out)) {
        if (out[key] === undefined) delete out[key];
    }
    return out;
}

function syncFlags(message: Pick<DesktopBotMessage, 'askResolved' | 'optionsUsed'>): Record<string, true> {
    const flags: Record<string, true> = {};
    if (message.askResolved) flags.askResolved = true;
    if (message.optionsUsed) flags.optionsUsed = true;
    return flags;
}

function withFlags(message: DesktopBotMessage, data: Record<string, unknown>): DesktopBotMessage {
    let next = message;
    if (data.askResolved === true && !message.askResolved) next = { ...next, askResolved: true };
    if (data.optionsUsed === true && !message.optionsUsed) next = { ...next, optionsUsed: true };
    return next;
}

function remoteMessage(entry: DesktopBotSyncEntry, data: Record<string, unknown>): DesktopBotMessage {
    const message = { ...data, id: entry.id, syncSeq: entry.created_seq || entry.seq, syncOrigin: entry.origin || '' } as DesktopBotMessage;
    delete message.pending;
    delete message.dispatched;
    delete message.queued;
    delete message.noReading;
    delete message.crossNoteId;
    // Option chips redirect the run of the client that started it; here they
    // could only hide themselves, so they are not offered.
    delete message.options;
    delete message.optionsUsed;
    if (!message.content) message.content = '';
    return message;
}

function sameMessage(a: DesktopBotMessage, b: DesktopBotMessage): boolean {
    return JSON.stringify(a) === JSON.stringify(b);
}

// mergeSyncEntries folds other clients' entries into one stored transcript.
// clearedBefore drops entries created before this client's last /clear.
export function mergeSyncEntries(stored: DesktopBotMessage[], entries: DesktopBotSyncEntry[], clearedBefore = 0): { messages: DesktopBotMessage[]; changed: boolean } {
    const list = stored.slice();
    let changed = false;
    const ordered = [...entries].sort((a, b) => (a.seq || 0) - (b.seq || 0));
    for (const entry of ordered) {
        const data = entry && entry.data && typeof entry.data === 'object' ? entry.data : null;
        if (!data || !entry.id) continue;
        const at = list.findIndex(item => item.id === entry.id);
        if (data.deleted === true) {
            if (at >= 0 && list[at].syncSeq && !list[at].pending) {
                list.splice(at, 1);
                changed = true;
            }
            continue;
        }
        if (data.role !== 'user' && data.role !== 'assistant') continue;
        if (at >= 0) {
            const current = list[at];
            if (current.syncSeq) {
                let next = remoteMessage(entry, data);
                next = { ...next, syncSeq: current.syncSeq };
                if (current.askResolved) next.askResolved = true;
                // A written understanding must not be rolled back to the
                // stock receipt if an older entry arrives out of order.
                if (current.ack && !desktopBotIsPlaceholderAck(String(current.content || '')) && desktopBotIsPlaceholderAck(String(next.content || ''))) {
                    const flagged = withFlags(current, data);
                    if (flagged !== current) {
                        list[at] = flagged;
                        changed = true;
                    }
                    continue;
                }
                if (!sameMessage(current, next)) {
                    list[at] = next;
                    changed = true;
                }
            } else {
                // This client's own bubble, read back (an earlier run of this
                // app): only an answer given elsewhere is taken.
                const next = withFlags(current, data);
                if (next !== current) {
                    list[at] = next;
                    changed = true;
                }
            }
            continue;
        }
        const runId = typeof data.runId === 'string' ? data.runId.trim() : '';
        if (runId) {
            const twin = list.findIndex(item => item.runId === runId || item.requestId === `desktop-bot-deliver-${runId}`);
            if (twin >= 0) {
                const next = withFlags(list[twin], data);
                if (next !== list[twin]) {
                    list[twin] = next;
                    changed = true;
                }
                continue;
            }
        }
        if ((entry.created_seq || entry.seq) <= clearedBefore) continue;
        const message = remoteMessage(entry, data);
        if (message.ack && message.ackFor) {
            const twin = list.findIndex(item => item.ack && item.ackFor === message.ackFor);
            if (twin >= 0) {
                const current = list[twin];
                if (desktopBotIsPlaceholderAck(String(current.content || '')) && !desktopBotIsPlaceholderAck(String(message.content || ''))) {
                    list[twin] = { ...current, content: message.content, understood: true };
                    changed = true;
                }
                continue;
            }
        }
        const started = botMessageStartedAt(message.id);
        let pos = list.length;
        if (started > 0) {
            const later = list.findIndex(item => {
                const t = botMessageStartedAt(item.id);
                return t > started;
            });
            if (later >= 0) pos = later;
        }
        list.splice(pos, 0, message);
        changed = true;
    }
    return { messages: list, changed };
}

// ---- publisher -----------------------------------------------------------

const published = new Map<string, string>();
const ownIds = new Map<string, string[]>();
const queued = new Map<string, { userId: string; botId: string; messages: DesktopBotMessage[] }>();
const suppressTombstones = new Set<string>();
let flushTimer: ReturnType<typeof setTimeout> | null = null;
let started = false;
let publishFn: (owner: string, writes: Write[]) => Promise<void> = (owner, writes) => PublishDesktopBotConversation(owner, writes);

function botKey(userId: string, botId: string) {
    return `${desktopUserKey(userId)}\0${botId}`;
}

function messageKey(userId: string, botId: string, id: string) {
    return `${botKey(userId, botId)}\0${id}`;
}

function offer(userId: string, botId: string, messages: DesktopBotMessage[]) {
    if (!started) return;
    if (desktopUserKey(userId) === 'local' || !botId) return;
    queued.set(botKey(userId, botId), { userId, botId, messages });
    if (!flushTimer) flushTimer = setTimeout(() => { void flushDesktopBotSync(); }, FLUSH_MS);
}

// syncWrites is what one transcript write needs to send: new or changed own
// bubbles, flag patches on others' bubbles, and deletions of own bubbles.
// It also returns the bookkeeping to apply once Hub has them.
export function syncWrites(userId: string, botId: string, messages: DesktopBotMessage[], previousOwn: string[] | undefined, isPublished: (key: string, fp: string) => boolean, tombstones = true) {
    const writes: Write[] = [];
    const marks: Array<[string, string]> = [];
    const own: string[] = [];
    const ids = new Set<string>();
    for (const message of messages) {
        if (!message || !message.id) continue;
        ids.add(message.id);
        const key = messageKey(userId, botId, message.id);
        if (message.syncSeq) {
            const flags = syncFlags(message);
            if (Object.keys(flags).length === 0) continue;
            const fp = `patch:${JSON.stringify(flags)}`;
            if (isPublished(key, fp)) continue;
            writes.push({ bot_id: botId, id: message.id, kind: 'message', patch: true, data: flags });
            marks.push([key, fp]);
            continue;
        }
        const data = syncProjection(message);
        if (!data) continue;
        own.push(message.id);
        const fp = JSON.stringify(data);
        if (isPublished(key, fp)) continue;
        writes.push({ bot_id: botId, id: message.id, kind: 'message', data });
        marks.push([key, fp]);
    }
    if (tombstones && previousOwn && messages.length > 0) {
        // A bubble older than the oldest one still kept fell off the
        // 80-bubble window; only a newer one was removed on purpose.
        const floor = botMessageStartedAt(messages[0].id);
        for (const id of previousOwn) {
            if (ids.has(id)) continue;
            const at = botMessageStartedAt(id);
            if (floor <= 0 || at <= 0 || at < floor) continue;
            const key = messageKey(userId, botId, id);
            if (isPublished(key, 'deleted')) continue;
            writes.push({ bot_id: botId, id, kind: 'message', data: { deleted: true } });
            marks.push([key, 'deleted']);
        }
    }
    return { writes, marks, own };
}

export async function flushDesktopBotSync() {
    flushTimer = null;
    const jobs = [...queued.values()];
    queued.clear();
    for (const job of jobs) {
        const key = botKey(job.userId, job.botId);
        const quiet = suppressTombstones.delete(key);
        const { writes, marks, own } = syncWrites(job.userId, job.botId, job.messages, ownIds.get(key), (k, fp) => published.get(k) === fp, !quiet);
        ownIds.set(key, own);
        if (writes.length === 0) continue;
        try {
            await publishFn(job.userId, writes);
            for (const [k, fp] of marks) published.set(k, fp);
        } catch {
            setTimeout(() => offer(job.userId, job.botId, messagesForBot(job.userId, job.botId)), RETRY_MS);
        }
    }
}

// ---- receiver ------------------------------------------------------------

const lastSeq = new Map<string, number>();

function readCleared(): Record<string, Record<string, number>> {
    try {
        const parsed = JSON.parse(window.localStorage.getItem(CLEARED_KEY) || '{}');
        return parsed && typeof parsed === 'object' ? parsed : {};
    } catch {
        return {};
    }
}

function clearedBefore(userId: string, botId: string): number {
    const value = readCleared()[desktopUserKey(userId)]?.[botId];
    return typeof value === 'number' && Number.isFinite(value) ? value : 0;
}

// noteDesktopBotCleared records this client's /clear: entries already in the
// journal stay off this transcript, and the bubbles the clear removed are
// not written as deleted for the other clients.
export function noteDesktopBotCleared(userId: string, botId: string) {
    const userKey = desktopUserKey(userId);
    suppressTombstones.add(botKey(userId, botId));
    try {
        const all = readCleared();
        all[userKey] = { ...(all[userKey] || {}), [botId]: lastSeq.get(userKey) || 0 };
        window.localStorage.setItem(CLEARED_KEY, JSON.stringify(all));
    } catch {
        // A full storage only loses the watermark; old lines may return.
    }
}

export function applyDesktopBotSync(payload: unknown) {
    let data: Record<string, unknown>;
    try {
        data = (typeof payload === 'string' ? JSON.parse(payload) : payload) as Record<string, unknown>;
    } catch {
        return;
    }
    if (!data || typeof data !== 'object') return;
    const owner = String(data.owner_id || '').trim();
    if (!owner || desktopUserKey(owner) === 'local') return;
    const entries = Array.isArray(data.entries) ? data.entries as DesktopBotSyncEntry[] : [];
    const userKey = desktopUserKey(owner);
    const byBot = new Map<string, DesktopBotSyncEntry[]>();
    for (const entry of entries) {
        if (!entry || !entry.bot_id || !entry.id) continue;
        lastSeq.set(userKey, Math.max(lastSeq.get(userKey) || 0, Number(entry.seq) || 0));
        const list = byBot.get(entry.bot_id) || [];
        list.push(entry);
        byBot.set(entry.bot_id, list);
    }
    const head = Number(data.head) || 0;
    if (head > 0) lastSeq.set(userKey, Math.max(lastSeq.get(userKey) || 0, head));
    for (const [botId, list] of byBot) {
        const merged = mergeSyncEntries(messagesForBot(owner, botId), list, clearedBefore(owner, botId));
        if (!merged.changed) continue;
        // The others' flags are already in the journal; do not echo them.
        for (const message of merged.messages) {
            if (!message.syncSeq) continue;
            const flags = syncFlags(message);
            if (Object.keys(flags).length > 0) published.set(messageKey(owner, botId, message.id), `patch:${JSON.stringify(flags)}`);
        }
        saveBotMessages(owner, botId, merged.messages);
        if (typeof window !== 'undefined') window.dispatchEvent(new CustomEvent(BOT_SYNCED_EVENT, { detail: { userId: owner, botId } }));
    }
}

export function startDesktopBotSync() {
    if (started) return;
    started = true;
    setDesktopBotTranscriptSyncHook(offer);
    try {
        EventsOn('desktop-bot-sync', applyDesktopBotSync);
    } catch {
        // No Wails runtime (tests, a plain browser): nothing to sync with.
    }
    void Promise.resolve()
        .then(() => StartDesktopBotConversationSync())
        .catch(() => '')
        .then(() => {
            // What was written while the journal was out of reach goes now;
            // Hub keeps an identical bubble as it is.
            eachDesktopBotTranscript((userKey, botId, messages) => offer(userKey, botId, messages));
        });
}

// Test seams.
export function resetDesktopBotSyncForTest(publish?: (owner: string, writes: Write[]) => Promise<void>) {
    published.clear();
    ownIds.clear();
    queued.clear();
    suppressTombstones.clear();
    lastSeq.clear();
    if (flushTimer) clearTimeout(flushTimer);
    flushTimer = null;
    started = !!publish;
    if (publish) {
        publishFn = publish;
        setDesktopBotTranscriptSyncHook(offer);
    } else {
        publishFn = (owner, writes) => PublishDesktopBotConversation(owner, writes);
        setDesktopBotTranscriptSyncHook(null);
    }
}
