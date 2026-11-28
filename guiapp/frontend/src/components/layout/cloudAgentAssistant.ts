import { useSyncExternalStore } from 'react';
import { EventsOn } from '../../../wailsjs/runtime';
import { localizeText } from '../../i18n';
import type { CodingAgentTimelineItem } from '../ai/codingAgentTimeline';
import {
    applyCloudAgentStream,
    applyCloudAgentWatch,
    cloudAgentRound,
    cloudAgentSessionKey,
    cloudAgentStoreKey,
    cloudAgentTrail,
    cloudAgentTrailRequestId,
    cloudAgentWorkbench,
    cloudAgentWorkflow,
    codingTimelineFromTurnEvents,
    expireCloudAgentCards,
} from './cloudAgentSession';
import {
    bindRemoteAssistantActions,
    publishRemoteAssistantSession,
    remoteLiveToolCalls,
    type RemoteAssistantTranscriptMessage,
    type RemoteLivePart,
    type RemoteLiveRound,
} from './remoteAssistantSession';
import { asCloudAgentListItem, cloudTurnRunning, noteCloudTurn, rememberCloudOpen, type CloudAgentListItem } from './taskPlace';

const CLOUD_LIST_REFRESH = 'maclaw:cloud-agents-refresh';

/** Ask the task list to reload cloud rows after a new instance is created. */
export function requestCloudAgentListRefresh() {
    if (typeof window === 'undefined') return;
    window.dispatchEvent(new CustomEvent(CLOUD_LIST_REFRESH));
}

export function onCloudAgentListRefresh(listener: () => void): () => void {
    if (typeof window === 'undefined') return () => {};
    window.addEventListener(CLOUD_LIST_REFRESH, listener);
    return () => window.removeEventListener(CLOUD_LIST_REFRESH, listener);
}

type Binding = (...args: unknown[]) => Promise<unknown>;

type CloudHistoryEvent = { kind?: string; text?: string; seq?: number };

const settledTrailCap = 32;

function rememberSettledTrail(current: Owner, requestId: string, trail: CodingAgentTimelineItem[]): void {
    const id = requestId.trim();
    if (!id || trail.length === 0) return;
    if (current.settledTrails[id]) delete current.settledTrails[id];
    current.settledTrails[id] = trail;
    const ids = Object.keys(current.settledTrails);
    while (ids.length > settledTrailCap) {
        const drop = ids.shift();
        if (drop) delete current.settledTrails[drop];
    }
}

function normalizeHistoryEvents(raw: unknown): CloudHistoryEvent[] {
    if (!Array.isArray(raw)) return [];
    const events: CloudHistoryEvent[] = [];
    for (const item of raw) {
        if (!item || typeof item !== 'object') continue;
        const row = item as Record<string, unknown>;
        const text = String(row.text || '');
        if (!text) continue;
        const seq = typeof row.seq === 'number' && row.seq > 0 ? row.seq : undefined;
        events.push({ kind: String(row.kind || ''), text, seq });
    }
    return events;
}

type Owner = {
    generation: number;
    lang: string;
    item: CloudAgentListItem;
    grant: string;
    taskId: string;
    history: string[];
    /** Produced-file lists parallel to history — files stay on the instance. */
    historyFiles: string[][];
    historyRequest: string;
    /** Parallel to history. Empty for a local send or a row written before request ids. */
    historyRequestIDs: string[];
    /** Parallel to history. The desktop's client message id for a user row; empty on book rows that predate it and on assistant rows. */
    historyClientIDs: string[];
    /** Parallel to history. Process rows cloud_get_task attached to that line. */
    historyEvents: CloudHistoryEvent[][];
    /**
     * Timelines copied off the live trail before the next round clears it.
     * A book page with events replaces the cached copy for that request.
     */
    settledTrails: Record<string, CodingAgentTimelineItem[]>;
    /** round_started named this client id before the send ack appended the line. */
    pendingClientBinds: Record<string, string>;
    /** How many leading history lines came from the book. The tail is unacked local sends. */
    bookLines: number;
    /**
     * Rounds that left the live slot before the book reported their request id.
     * Each stays until cloud_get_task returns an assistant row with that id.
     * A later round does not drop an earlier one.
     */
    retired: { requestId: string; text: string; files: string[] }[];
    /** Bumped when a send appends a line. A fetch from before that send is stale. */
    historyEpoch: number;
    /** Bumped when a history fetch starts. Only the latest fetch may write. */
    historyLoad: number;
    error: string;
    /** A send was accepted and this round has not closed. */
    pending: boolean;
    /** Request id already stored when the send left. The same closed round must not end the wait. */
    pendingBaseline: string;
    /** cloud_stop_task is in flight. The stream does not announce the cancel. */
    stopping: boolean;
    /**
     * Sends the srv parked while a turn was running, in FIFO order.
     * Each slot is that send's client message id. Display text is not an identity:
     * two parked lines can say the same thing.
     */
    queued: string[];
    /** Coding-instance repository binding shown by the repo banner. `checked`
     *  stays false until GetCloudAgentGit answers, so the banner never flashes
     *  "not connected" on a bound instance while the read is in flight. */
    git: { connected: boolean; repo: string; service: string; origin: string; checked: boolean };
    /** The private node srv this instance runs on, resolved from the user's
     *  srv-capable nodes. Null while the lookup is pending or after it failed —
     *  the header falls back to the node id. */
    srv: { nodeId: string; name: string; address: string; online: boolean } | null;
    /**
     * Wizard choices the first cloud_create_task ships: the workflow template
     * + its form params and the expert persona resolved to name/prompt.
     * Read once by sendCloud's lazy create; later tasks on the same instance
     * start plain.
     */
    taskSetup?: {
        workflowTemplateId: string;
        workflowParams: Record<string, string>;
        expertName: string;
        expertPrompt: string;
    } | null;
};

let generation = 0;
let owner: Owner | null = null;
let streamOff: (() => void) | null = null;
const listeners = new Set<() => void>();

function binding(name: string): Binding | null {
    const app = (window as unknown as { go?: { main?: { App?: Record<string, unknown> } } }).go?.main?.App;
    const fn = app?.[name];
    return typeof fn === 'function' ? fn as Binding : null;
}

function notify() {
    listeners.forEach(listener => listener());
}

export function cloudAssistantActive(): boolean {
    return owner != null;
}

export function cloudAssistantInstanceId(): string {
    return owner?.item.id || '';
}

export function useCloudAssistantId(): string {
    return useSyncExternalStore(
        (listener: () => void) => {
            listeners.add(listener);
            return () => listeners.delete(listener);
        },
        cloudAssistantInstanceId,
        () => '',
    );
}

function parseJSON<T>(raw: unknown, fallback: T): T {
    if (typeof raw !== 'string' || !raw.trim()) return fallback;
    try {
        return JSON.parse(raw) as T;
    } catch {
        return fallback;
    }
}

function splitTranscriptLine(line: string): { role: string; content: string } {
    const split = line.indexOf('\n');
    return {
        role: (split < 0 ? line : line.slice(0, split)).trim() || 'assistant',
        content: split < 0 ? '' : line.slice(split + 1),
    };
}

function producedFilesAt(files: string[][], index: number): string[] {
    return (files[index] || []).map(path => String(path || '').trim()).filter(Boolean);
}

/** A book row is on screen when it has text, a produced file, or turn events. */
function shownTranscriptRow(content: string, files: string[], events?: CloudHistoryEvent[]): boolean {
    if (content.trim() || files.length > 0) return true;
    return !!events?.some(event => String(event?.text || '').trim());
}

export function historyMessages(
    lines: string[],
    files: string[][],
    events: CloudHistoryEvent[][],
    requestIDs: string[],
    settledTrails: Record<string, CodingAgentTimelineItem[]>,
    liveTrailRequest: string,
    retiredRequestIDs: ReadonlySet<string>,
): RemoteAssistantTranscriptMessage[] {
    const lastIndex = new Map<string, number>();
    const bookedEvents = new Set<string>();
    for (let index = 0; index < lines.length; index += 1) {
        const requestId = requestIDs[index] || '';
        if (!requestId) continue;
        lastIndex.set(requestId, index);
        if ((events[index]?.length || 0) > 0) bookedEvents.add(requestId);
    }
    const messages: RemoteAssistantTranscriptMessage[] = [];
    for (const [index, line] of lines.entries()) {
        const { role, content } = splitTranscriptLine(line);
        const produced = producedFilesAt(files, index);
        if (!shownTranscriptRow(content, produced, events[index])) continue;
        const requestId = requestIDs[index] || '';
        let codingTimeline = codingTimelineFromTurnEvents(events[index]);
        // Events sit on the last row of the request. The cache covers only the
        // gap after the live trail resets and before that page arrives, and
        // only on that same last row. A retired stand-in and the open live
        // row carry the trail themselves.
        if (
            !codingTimeline
            && requestId
            && lastIndex.get(requestId) === index
            && !bookedEvents.has(requestId)
            && requestId !== liveTrailRequest
            && !retiredRequestIDs.has(requestId)
        ) {
            const cached = settledTrails[requestId];
            if (cached?.length) codingTimeline = cached;
        }
        messages.push({
            role,
            content,
            localFilePaths: produced.length ? produced : undefined,
            codingTimeline,
        });
    }
    return messages;
}

/** Request ids of the shown rows, parallel to historyMessages. Empty ids never match. */
function shownRequestIDs(lines: string[], files: string[][], ids: string[], events: CloudHistoryEvent[][]): string[] {
    const shown: string[] = [];
    for (let index = 0; index < lines.length; index += 1) {
        const { content } = splitTranscriptLine(lines[index] || '');
        if (!shownTranscriptRow(content, producedFilesAt(files, index), events[index])) continue;
        shown.push(ids[index] || '');
    }
    return shown;
}

/**
 * Where this round's assistant material is inserted, or -1 to append.
 * A queued row is a send that has not started. The open request ends before
 * the first of those that follows its own user line. While this is still the
 * round those sends are waiting on, that cut is the first queued row, or the
 * single unbound follow-up at the tail.
 */
function openRoundCut(messages: RemoteAssistantTranscriptMessage[], openUserAt: number, waitingOnThisRound: boolean): number {
    const start = openUserAt >= 0 || waitingOnThisRound ? openUserAt + 1 : messages.length;
    for (let index = start; index < messages.length; index += 1) {
        const message = messages[index];
        if (message?.role === 'user' && message.queued) return index;
    }
    if (waitingOnThisRound) {
        const last = messages[messages.length - 1];
        if (last?.role === 'user') return messages.length - 1;
    }
    return -1;
}

function stripOrigin(text: string): string {
    return text.replace(/^〔[^〕]*〕/, '').trim();
}

/** Index of the assistant row for this turn in the filtered transcript, or -1. */
function assistantMessageIndex(lines: string[], ids: string[], files: string[][], events: CloudHistoryEvent[][], requestId: string): number {
    if (!requestId) return -1;
    let visible = 0;
    for (let index = 0; index < lines.length; index += 1) {
        const { role, content } = splitTranscriptLine(lines[index]);
        if (!shownTranscriptRow(content, producedFilesAt(files, index), events[index])) continue;
        if (role === 'assistant' && ids[index] === requestId) return visible;
        visible += 1;
    }
    return -1;
}

/** How many shown transcript rows the book prefix contributes. */
function bookMessageCount(lines: string[], files: string[][], events: CloudHistoryEvent[][], bookLines: number): number {
    let count = 0;
    const end = Math.min(Math.max(bookLines, 0), lines.length);
    for (let index = 0; index < end; index += 1) {
        const { content } = splitTranscriptLine(lines[index]);
        if (shownTranscriptRow(content, producedFilesAt(files, index), events[index])) count += 1;
    }
    return count;
}

/** The user row Begin stored for this turn. Empty ids never match. */
function historyHasUserRequest(lines: string[], ids: string[], requestId: string): boolean {
    if (!requestId) return false;
    for (let index = 0; index < lines.length; index += 1) {
        if (ids[index] !== requestId) continue;
        if (splitTranscriptLine(lines[index]).role === 'user') return true;
    }
    return false;
}

let clientMessageSeq = 0;

/** One id per send. The book stores it on admission and round_started echoes it back. */
function newClientMessageID(): string {
    clientMessageSeq += 1;
    return `cm_${Date.now()}_${clientMessageSeq}`;
}

function historyClientIndex(current: Owner, clientID: string): number {
    const id = clientID.trim();
    if (!id) return -1;
    for (let index = 0; index < current.historyClientIDs.length; index += 1) {
        if (current.historyClientIDs[index] === id) return index;
    }
    return -1;
}

/** Drop one parked slot. An earlier client id stays until its own round or the book binds it. */
function releaseQueuedClient(current: Owner, clientID: string): void {
    const at = current.queued.indexOf(clientID);
    if (at < 0) return;
    current.queued = current.queued.slice(0, at).concat(current.queued.slice(at + 1));
}

/**
 * The parked send this frame admitted. The line may not exist yet — the ack
 * appends it — so remember the request id under the client id until then.
 * The queue slot is that same client id, so an identical earlier text stays parked.
 */
function admitClientRound(current: Owner, clientID: string, requestID: string): void {
    const id = clientID.trim();
    const request = requestID.trim();
    if (!id || !request) return;
    const index = historyClientIndex(current, id);
    if (index < 0) {
        current.pendingClientBinds[id] = request;
        return;
    }
    delete current.pendingClientBinds[id];
    if (!current.historyRequestIDs[index]) current.historyRequestIDs[index] = request;
    releaseQueuedClient(current, id);
}

/** A book snapshot that already bound a line drops that client id from the queue. */
function reconcileQueued(current: Owner): void {
    const open = new Set<string>();
    for (let index = 0; index < current.history.length; index += 1) {
        if (current.historyRequestIDs[index]) continue;
        const clientID = (current.historyClientIDs[index] || '').trim();
        if (!clientID) continue;
        const line = current.history[index] || '';
        if (!line.startsWith('user\n')) continue;
        open.add(clientID);
    }
    current.queued = current.queued.filter(id => open.has(id));
}

/**
 * A round_started that omits the client id still names one parked send.
 * Display text selects the oldest unbound line with that text; anything
 * parked before that slot already started, same as the old FIFO drain.
 */
function admitDisplayRound(current: Owner, display: string, requestID: string): void {
    const text = display.trim();
    if (!text) return;
    const line = `user\n${text}`;
    const request = requestID.trim();
    for (let at = 0; at < current.queued.length; at += 1) {
        const row = historyClientIndex(current, current.queued[at]);
        if (row < 0 || current.historyRequestIDs[row]) continue;
        if ((current.history[row] || '') !== line) continue;
        if (request) current.historyRequestIDs[row] = request;
        current.queued = current.queued.slice(at + 1);
        return;
    }
}

/**
 * The badge belongs to the parked client id's own user row. Call this on the
 * history objects before any splice: the flag stays on the object.
 */
function markQueuedBadges(current: Owner, messages: RemoteAssistantTranscriptMessage[]): void {
    if (!current.queued.length || !messages.length) return;
    const waiting = new Set(current.queued);
    let shown = 0;
    for (let index = 0; index < current.history.length && shown < messages.length; index += 1) {
        const { role, content } = splitTranscriptLine(current.history[index] || '');
        if (!shownTranscriptRow(content, producedFilesAt(current.historyFiles, index), current.historyEvents[index])) continue;
        const row = messages[shown];
        shown += 1;
        if (!row || role !== 'user') continue;
        const clientID = (current.historyClientIDs[index] || '').trim();
        if (!clientID || current.historyRequestIDs[index]) continue;
        if (waiting.has(clientID)) row.queued = true;
    }
}

function currentRound(current: Owner): ReturnType<typeof cloudAgentRound> | null {
    const storeKey = cloudAgentStoreKey(current.grant, cloudAgentSessionKey(current.item.id, current.taskId));
    return storeKey ? cloudAgentRound(storeKey) : null;
}

function roundHasWork(round: ReturnType<typeof cloudAgentRound> | null): boolean {
    if (!round) return false;
    if (round.requestId) return true;
    if ((round.visible || round.body).trim()) return true;
    if (round.thinking.some(block => block.text.trim())) return true;
    return round.timeline.length > 0;
}

/** The send's own round has responded. The round already on screen does not count. */
function followUpClosed(round: ReturnType<typeof cloudAgentRound> | null, pending: boolean, baseline: string): boolean {
    return pending && !!round?.responded && !!round.requestId && round.requestId !== baseline;
}

/** A send waits even when the previous round already responded. That round is not this send. */
function turnPhase(round: ReturnType<typeof cloudAgentRound> | null, pending: boolean, baseline: string): 'running' | 'idle' | 'unknown' {
    if (pending && !followUpClosed(round, pending, baseline)) return 'running';
    if (round?.responded) return 'idle';
    if (roundHasWork(round)) return 'running';
    return 'unknown';
}

function instanceIdFromStoreKey(key: string): string {
    const breakAt = key.indexOf('\n');
    const session = breakAt < 0 ? '' : key.slice(breakAt + 1);
    const prefix = 'cloud-agent:';
    if (!session.startsWith(prefix)) return '';
    const rest = session.slice(prefix.length);
    const split = rest.indexOf(':');
    return split < 0 ? '' : rest.slice(0, split);
}

/** Instance id to the request id already on screen when its send left. */
const awaitingBaseline = new Map<string, string>();

/** Last resync attempt per store key — a dropped frame gaps the reducer and
 * every later frame keeps gapping, so retries ride a short throttle, not a latch. */
const gapRecoveryAt = new Map<string, number>();
const GAP_RECOVERY_MIN_MS = 2000;

/** publishOwner re-runs per frame; log a live row's artifacts once, not per paint. */
let artifactLogKey = '';

/** A lost stream frame leaves the reducer gapped; the watch snapshot is the
 * resync — the same recovery the local viewer's resync snapshot performs. */
function recoverCloudStream(key: string) {
    const at = gapRecoveryAt.get(key) || 0;
    if (Date.now() - at < GAP_RECOVERY_MIN_MS) return;
    gapRecoveryAt.set(key, Date.now());
    const breakAt = key.indexOf('\n');
    const grant = breakAt < 0 ? '' : key.slice(0, breakAt);
    const session = breakAt < 0 ? '' : key.slice(breakAt + 1);
    const taskId = session.slice(session.lastIndexOf(':') + 1).trim();
    const instanceId = instanceIdFromStoreKey(key);
    const op = binding('RemoteControlOp');
    if (!op || !grant || !session || !taskId || !instanceId) {
        console.debug('[cloud-agent] resync skipped: incomplete key or binding', { key, hasOp: !!op, taskId });
        return;
    }
    const baseline = cloudAgentRound(key).last;
    console.debug('[cloud-agent] stream gap: issuing resync watch', { instanceId, taskId, baseline });
    void op(grant, 'cloud_watch_task', JSON.stringify({ task_id: taskId, session_key: session }))
        .then(raw => {
            applyWatchedTurn(instanceId, grant, session, parseJSON(raw, {}), baseline);
            console.debug('[cloud-agent] resync watch applied', { instanceId, taskId, gap: cloudAgentRound(key).gap });
        })
        .catch((err) => {
            console.debug('[cloud-agent] resync watch failed', { instanceId, taskId, error: String(err) });
        });
}

function setAwaiting(current: Owner) {
    awaitingBaseline.set(current.item.id, current.pendingBaseline);
}

function clearAwaiting(instanceId: string) {
    awaitingBaseline.delete(instanceId);
}

/** A responded round closes the wait only when it is not the round the send already saw. */
function awaitingClosed(instanceId: string, round: ReturnType<typeof cloudAgentRound> | null): boolean {
    const baseline = awaitingBaseline.get(instanceId);
    if (baseline === undefined) return true;
    return !!round?.responded && !!round.requestId && round.requestId !== baseline;
}

/** Project one observed turn onto the list. An unknown phase leaves the last observation. */
function observeTurn(current: Owner, round: ReturnType<typeof cloudAgentRound> | null): boolean {
    if (followUpClosed(round, current.pending, current.pendingBaseline)) {
        current.pending = false;
        clearAwaiting(current.item.id);
    }
    const phase = turnPhase(round, current.pending, current.pendingBaseline);
    if (phase === 'running') noteCloudTurn(current.item.id, true);
    else if (phase === 'idle') noteCloudTurn(current.item.id, false);
    if (phase === 'idle') return false;
    if (phase === 'running') return true;
    return cloudTurnRunning(current.item.id, current.item.status);
}

/** A watch snapshot replaces the stored round, so it also replaces the list mark.
 * baselineCursor is the cursor already stored when the watch was issued. */
function applyWatchedTurn(instanceId: string, grant: string, session: string, snapshot: unknown, baselineCursor: number) {
    const key = applyCloudAgentWatch(grant, session, snapshot, baselineCursor);
    if (!key) return;
    const round = cloudAgentRound(key);
    const running = !round.responded && roundHasWork(round);
    const pendingHere = !!owner && owner.pending && owner.item.id === instanceId;
    // A detached send must not keep 进行中 after this watch shows the lane is idle.
    if (running || pendingHere) {
        if (awaitingClosed(instanceId, round)) clearAwaiting(instanceId);
        noteCloudTurn(instanceId, true);
        return;
    }
    clearAwaiting(instanceId);
    noteCloudTurn(instanceId, false);
}

/** The send did not start a turn. Drop the in-flight mark when the list is not running. */
function dropUnansweredSend(current: Owner) {
    current.pending = false;
    clearAwaiting(current.item.id);
    const round = currentRound(current);
    const phase = turnPhase(round, false, current.pendingBaseline);
    if (phase === 'running') return;
    if (phase === 'idle' || current.item.status !== 'running') noteCloudTurn(current.item.id, false);
}

/** The same live round a remote task projects: draft body, thinking, and progress. */
function cloudLive(round: ReturnType<typeof cloudAgentRound> | null): RemoteLiveRound | null {
    if (!round) return null;
    const text = (round.visible || round.body || '').trim();
    const thinking = round.thinking.some(block => block.text);
    if (!round.requestId && !text && !thinking && round.timeline.length === 0) return null;
    const parts: RemoteLivePart[] = [];
    round.thinking.forEach((block, index) => {
        parts.push({ kind: 'thinking', seq: index + 1, text: block.text, done: block.done });
    });
    if (text) parts.push({ kind: 'text', seq: parts.length + 1, text });
    for (const row of round.timeline) {
        const label = row.text || row.title;
        if (label) parts.push({ kind: 'progress', seq: parts.length + 1, text: label });
    }
    return {
        requestId: round.requestId,
        displayText: round.displayText || '',
        parts,
        streamDone: round.responded,
        responded: round.responded,
        baseline: -1,
        lastEventAt: Date.now(),
    };
}

/**
 * The header's server line. An empty srv_node_id is the official cloud
 * service; a private node names itself and its address (the ip).
 */
function cloudServerLabel(current: Owner, text: (en: string, zh: string, hant?: string) => string): string {
    const nodeId = String(current.item.srvNodeId || current.srv?.nodeId || '').trim();
    if (!nodeId) return text('Official cloud service', '官方云端服务', '官方雲端服務');
    const detail = [current.srv?.name || '', current.srv?.address || ''].filter(Boolean).join(' · ') || nodeId;
    const offline = current.srv && !current.srv.online ? ` · ${text('offline', '离线', '離線')}` : '';
    return `${text('Private maclawsrv', '私有 maclawsrv')} · ${detail}${offline}`;
}

function publishOwner() {
    const current = owner;
    if (!current) return;
    const text = (en: string, zh: string, hant?: string) => localizeText(current.lang, en, zh, hant);
    const sessionKey = cloudAgentSessionKey(current.item.id, current.taskId);
    const storeKey = cloudAgentStoreKey(current.grant, sessionKey);
    const round = storeKey ? cloudAgentRound(storeKey) : null;
    const trail = storeKey ? cloudAgentTrail(storeKey) : null;
    const liveTrailRequest = trail ? cloudAgentTrailRequestId(storeKey) : '';
    const retiredIDs = new Set(current.retired.map(item => item.requestId));
    const messages = historyMessages(current.history, current.historyFiles, current.historyEvents, current.historyRequestIDs, current.settledTrails, liveTrailRequest, retiredIDs);
    const shownIDs = shownRequestIDs(current.history, current.historyFiles, current.historyRequestIDs, current.historyEvents);
    markQueuedBadges(current, messages);
    // The book has not reported these request ids. Keep each terminal between
    // the book prefix and local sends, in the order the rounds closed.
    let retiredAt = bookMessageCount(current.history, current.historyFiles, current.historyEvents, current.bookLines);
    for (const item of current.retired) {
        if (assistantMessageIndex(current.history, current.historyRequestIDs, current.historyFiles, current.historyEvents, item.requestId) >= 0) continue;
        const files = item.files.length ? item.files : undefined;
        if (!item.text && !files) continue;
        const cachedTrail = current.settledTrails[item.requestId];
        messages.splice(retiredAt, 0, {
            role: 'assistant',
            content: item.text,
            localFilePaths: files,
            codingTimeline: cachedTrail?.length ? cachedTrail : undefined,
        });
        shownIDs.splice(retiredAt, 0, '');
        retiredAt += 1;
    }
    const prompt = stripOrigin(round?.displayText || '');
    const promptBound = historyHasUserRequest(current.history, current.historyRequestIDs, round?.requestId || '');
    // Parked rows already carry the queued flag. An echoed prompt is the open
    // question, so it takes the same cut the answer will.
    const parkedAt = messages.findIndex(message => message.role === 'user' && message.queued);
    if (prompt && !promptBound && !messages.some(message => message.role === 'user' && message.content.trim() === prompt)) {
        const echo = { role: 'user' as const, content: prompt };
        if (parkedAt >= 0) {
            messages.splice(parkedAt, 0, echo);
            shownIDs.splice(parkedAt, 0, '');
        } else {
            messages.push(echo);
            shownIDs.push('');
        }
    }
    const live = cloudLive(round);
    const liveText = (round?.visible || round?.body || '').trim();
    const reasoning = (round?.thinking || []).map(block => block.text).filter(Boolean).join('\n\n');
    // Ordered thinking+tool trail of this round (frame order, tool_started
    // merged with its tool_finished) — the same process timeline a local
    // coding turn keeps on its bubble. The open row takes the live trail
    // over a stale book page. settledTrails keeps it across the next
    // round_started until cloud_get_task returns events.
    if (trail && round?.requestId) rememberSettledTrail(current, round.requestId, trail);
    // Styled progress rows become the same tool timeline a local turn keeps.
    const folded = remoteLiveToolCalls(live?.parts || [], liveText);
    // The open request's user line is the anchor. Later parked sends stay
    // below this round even after the queue has admitted this one.
    const openRequest = round?.requestId || '';
    const openUserAt = openRequest
        ? shownIDs.findIndex((id, index) => id === openRequest && messages[index]?.role === 'user')
        : -1;
    const waitingOnThisRound = current.pending && !!openRequest && openRequest === current.pendingBaseline;
    const cut = openRoundCut(messages, openUserAt, waitingOnThisRound);
    // Files the turn produced on the instance ride the round's artifacts list.
    const produced = (round?.artifacts || []).map(path => String(path || '').trim()).filter(Boolean);
    const producedFiles = produced.length ? produced : undefined;
    if (producedFiles) {
        const logKey = `${current.taskId}|${round?.requestId}|${produced.join(',')}`;
        if (logKey !== artifactLogKey) {
            artifactLogKey = logKey;
            console.debug('[cloud-agent] artifacts on live row', { taskId: current.taskId, requestId: round?.requestId, files: produced });
        }
    }
    // The book already stored this turn. The live slot must not paint it again.
    const bookedAt = assistantMessageIndex(current.history, current.historyRequestIDs, current.historyFiles, current.historyEvents, round?.requestId || '');
    const booked = bookedAt >= 0 ? messages[bookedAt] : undefined;
    const liveRow = liveText || reasoning || folded.toolCalls || trail || producedFiles;
    // The card and notices belong to this row. Parked sends can be the tail
    // after it; they are not the round those controls came from.
    let openHost: RemoteAssistantTranscriptMessage | undefined;
    if (booked) {
        if (reasoning && !booked.reasoning) booked.reasoning = reasoning;
        if (folded.toolCalls && !booked.toolCalls) booked.toolCalls = folded.toolCalls;
        if (trail) booked.codingTimeline = trail;
        if (producedFiles && !booked.localFilePaths) booked.localFilePaths = producedFiles;
        openHost = booked;
    } else if (liveRow) {
        const liveMessage: RemoteAssistantTranscriptMessage = {
            role: 'assistant',
            content: folded.content,
            reasoning: reasoning || undefined,
            remoteLive: true,
            liveThinking: !!round?.thinking.some(block => !block.done),
            toolCalls: folded.toolCalls,
            codingTimeline: trail || undefined,
            localFilePaths: producedFiles,
        };
        if (cut >= 0) messages.splice(cut, 0, liveMessage);
        else messages.push(liveMessage);
        openHost = liveMessage;
    }
    const hostAt = openHost ? messages.indexOf(openHost) : -1;
    // No assistant row yet: the card and notices still belong in front of the
    // sends this round has not started.
    let tailAt = hostAt >= 0 ? hostAt + 1 : (cut >= 0 ? cut : messages.length);
    const card = round?.cards.find(item => !item.expired && (item.actions?.length || item.text || item.confirmationId));
    if (card) {
        const actions = (card.actions || []).filter(action => action.command);
        const confirmation = card.confirmationId ? { id: card.confirmationId, summary: card.text } : undefined;
        const tail = messages[messages.length - 1];
        const host = openHost && openHost.role !== 'user'
            ? openHost
            : (tail && tail.role !== 'user' ? tail : undefined);
        if (host) {
            host.actions = actions;
            host.confirmation = confirmation;
            if (!host.content.trim() && card.text) host.content = card.text;
        } else {
            messages.splice(tailAt, 0, {
                role: card.asError ? 'error' : 'assistant',
                content: card.text || liveText,
                actions,
                confirmation,
            });
            tailAt += 1;
        }
    }
    for (const note of round?.notices || []) {
        if (!note.text) continue;
        messages.splice(tailAt, 0, { role: note.asError ? 'error' : 'assistant', content: note.text });
        tailAt += 1;
    }
    publishRemoteAssistantSession({
        grantId: current.grant || current.item.id,
        machineName: text('Cloud', '云端', '雲端'),
        serverLabel: cloudServerLabel(current, text),
        projectPath: `cloud-agent:${current.item.id}`,
        taskName: current.item.name || current.item.id,
        cloud: true,
        cloudKind: current.item.kind === 'coding' ? 'coding' : 'session',
        canSend: !!current.grant,
        offline: false,
        error: current.error,
        cloudGit: current.item.kind === 'coding' && current.git.checked
            ? {
                connected: current.git.connected,
                repo: current.git.repo.trim() || undefined,
                service: current.git.service.trim() || undefined,
                origin: current.git.origin.trim() || undefined,
            }
            : undefined,
        loading: !current.grant && !current.error,
        busy: observeTurn(current, round),
        activity: current.stopping ? { stopping: true } : undefined,
        live,
        workbench: storeKey ? cloudAgentWorkbench(storeKey) : null,
        cloudWorkflow: storeKey ? cloudAgentWorkflow(storeKey) : null,
        messages,
    });
    bindRemoteAssistantActions({
        send: textToSend => sendCloud(current.generation, textToSend),
        stop: () => stopCloud(current.generation),
        close: closeCloudAssistant,
    });
}

function streamFrame(payload: unknown): { event: string; requestId: string; grant: string; session: string; clientMessageID: string } {
    const parsed = typeof payload === 'string'
        ? parseJSON<Record<string, unknown>>(payload, {})
        : (payload && typeof payload === 'object' ? payload as Record<string, unknown> : {});
    return {
        event: String(parsed.event || ''),
        requestId: String(parsed.request_id || ''),
        grant: String(parsed.grant_id || ''),
        session: String(parsed.session_key || ''),
        clientMessageID: String(parsed.client_message_id || ''),
    };
}

/**
 * A new request takes the live slot. Hold the previous round's terminal until
 * the book reports that request id on an assistant row.
 */
function retireOpenRound(current: Owner): void {
    const round = currentRound(current);
    if (!round?.responded || !round.requestId) return;
    const requestId = round.requestId;
    current.retired = current.retired.filter(item => item.requestId !== requestId);
    if (assistantMessageIndex(current.history, current.historyRequestIDs, current.historyFiles, current.historyEvents, requestId) >= 0) {
        return;
    }
    const text = (round.visible || round.body || '').trim();
    const files = (round.artifacts || []).map(path => String(path || '').trim()).filter(Boolean);
    if (!text && files.length === 0) return;
    current.retired.push({ requestId, text, files });
}

/** The stream payload's event name and display text, or ''s when absent. */
function streamStarted(payload: unknown): string {
    const parsed = typeof payload === 'string' ? parseJSON<{ event?: string; display_text?: string }>(payload, {}) : (payload && typeof payload === 'object' ? payload as { event?: unknown; display_text?: unknown } : {});
    if (String(parsed.event || '') !== 'round_started') return '';
    return stripOrigin(String(parsed.display_text || ''));
}

function ensureStream() {
    if (streamOff) return;
    const off = EventsOn('remote-control:stream', (payload: unknown) => {
        const preview = owner;
        if (preview) {
            const frame = streamFrame(payload);
            const want = cloudAgentStoreKey(preview.grant, cloudAgentSessionKey(preview.item.id, preview.taskId));
            const frameKey = cloudAgentStoreKey(frame.grant, frame.session);
            const openId = currentRound(preview)?.requestId || '';
            if (frame.event === 'round_started' && frame.requestId && frameKey && frameKey === want) {
                const previousTrailId = cloudAgentTrailRequestId(want);
                if (previousTrailId && previousTrailId !== frame.requestId) {
                    const existing = cloudAgentTrail(want);
                    if (existing) rememberSettledTrail(preview, previousTrailId, existing);
                }
                if (frame.requestId !== openId) retireOpenRound(preview);
            }
        }
        const key = applyCloudAgentStream(payload);
        if (!key) return;
        if (cloudAgentRound(key).gap) recoverCloudStream(key);
        const current = owner;
        const want = current ? cloudAgentStoreKey(current.grant, cloudAgentSessionKey(current.item.id, current.taskId)) : '';
        if (!current || key !== want) {
            const instanceId = instanceIdFromStoreKey(key);
            const streamed = cloudAgentRound(key);
            if (!instanceId) return;
            if (streamed.responded && awaitingClosed(instanceId, streamed)) {
                clearAwaiting(instanceId);
                noteCloudTurn(instanceId, false);
            } else if (!streamed.responded && roundHasWork(streamed)) {
                noteCloudTurn(instanceId, true);
            }
            return;
        }
        // A fresh round means the srv drained one parked send. The client
        // message id is the queue slot: the ack may still be in flight, so
        // the line is not in the queue yet. Without that id, display text
        // selects the oldest unbound line and drains the FIFO through it.
        const frame = streamFrame(payload);
        if (frame.event === 'round_started' && frame.clientMessageID) {
            admitClientRound(current, frame.clientMessageID, frame.requestId);
        } else {
            const started = streamStarted(payload);
            if (started) admitDisplayRound(current, started, frame.requestId);
        }
        if (current.error) current.error = '';
        publishOwner();
        const round = cloudAgentRound(key);
        if (round.responded && round.requestId && current.historyRequest !== round.requestId) {
            current.historyRequest = round.requestId;
            const ticket = takeHistoryTicket(current);
            void reloadHistory(current.generation, ticket);
        }
    });
    streamOff = typeof off === 'function' ? off : () => {};
}

type HistoryTicket = { epoch: number; load: number };

function takeHistoryTicket(current: Owner): HistoryTicket {
    current.historyLoad += 1;
    return { epoch: current.historyEpoch, load: current.historyLoad };
}

function userLineCount(lines: string[]): number {
    return lines.reduce((count, line) => count + (line.startsWith('user\n') ? 1 : 0), 0);
}

/**
 * Server history is the transcript, plus local user lines it does not have yet.
 * A trailing line whose client id is already in the book was admitted while
 * its ack was in flight — keeping it would paint the prompt twice.
 */
function keepUnackedUserLines(local: string[], incoming: string[], localClientIDs: string[] = [], incomingClientIDs: string[] = []): { lines: string[]; suffixLocalIndexes: number[] } {
    const missing = userLineCount(local) - userLineCount(incoming);
    if (missing <= 0) return { lines: incoming, suffixLocalIndexes: [] };
    const suffix: { line: string; index: number }[] = [];
    for (let index = local.length - 1; index >= 0 && suffix.length < missing; index -= 1) {
        if (!local[index].startsWith('user\n')) break;
        suffix.push({ line: local[index], index });
    }
    if (suffix.length !== missing) return { lines: incoming, suffixLocalIndexes: [] };
    const known = new Set(incomingClientIDs.filter(id => id));
    const kept = suffix.filter(item => {
        const clientID = localClientIDs[item.index] || '';
        return !(clientID && known.has(clientID));
    });
    kept.reverse();
    return {
        lines: [...incoming, ...kept.map(item => item.line)],
        suffixLocalIndexes: kept.map(item => item.index),
    };
}

/** A fetch writes only when no send and no newer fetch has happened since it started. */
function acceptHistory(current: Owner, ticket: HistoryTicket, raw: unknown): boolean {
    if (current.historyEpoch !== ticket.epoch || current.historyLoad !== ticket.load) return false;
    const body = parseJSON<{ messages?: string[]; files?: unknown[]; request_ids?: unknown[]; client_message_ids?: unknown[]; events?: unknown[] }>(raw, {});
    const incoming = Array.isArray(body.messages) ? body.messages : [];
    const incomingFiles: string[][] = [];
    for (let index = 0; index < incoming.length; index += 1) {
        const row = Array.isArray(body.files?.[index]) ? body.files[index] as string[] : [];
        incomingFiles.push(row.map(path => String(path || '').trim()).filter(Boolean));
    }
    const rawIDs = Array.isArray(body.request_ids) ? body.request_ids : [];
    const incomingIDs = incoming.map((_, index) => String(rawIDs[index] || ''));
    const rawClientIDs = Array.isArray(body.client_message_ids) ? body.client_message_ids : [];
    const incomingClientIDs = incoming.map((_, index) => String(rawClientIDs[index] || ''));
    const rawEvents = Array.isArray(body.events) ? body.events : [];
    const incomingEvents = incoming.map((_, index) => normalizeHistoryEvents(rawEvents[index]));
    const merged = keepUnackedUserLines(current.history, incoming, current.historyClientIDs, incomingClientIDs);
    // files[i] and request_ids[i] belong to messages[i]. A local suffix keeps
    // the request id and client id the send already bound; the book wins for its own rows.
    const files: string[][] = [];
    const requestIDs: string[] = [];
    const clientIDs: string[] = [];
    const events: CloudHistoryEvent[][] = [];
    for (let index = 0; index < merged.lines.length; index += 1) {
        if (index < incoming.length) {
            files.push(incomingFiles[index] || []);
            requestIDs.push(incomingIDs[index] || '');
            clientIDs.push(incomingClientIDs[index] || '');
            events.push(incomingEvents[index] || []);
            continue;
        }
        const localIndex = merged.suffixLocalIndexes[index - incoming.length];
        files.push(current.historyFiles[localIndex] || []);
        requestIDs.push(current.historyRequestIDs[localIndex] || '');
        clientIDs.push(current.historyClientIDs[localIndex] || '');
        events.push(current.historyEvents[localIndex] || []);
    }
    const artifactRows = files.filter(row => row.length > 0).length;
    if (artifactRows > 0 || merged.lines.length !== incoming.length) {
        console.debug('[cloud-agent] history accepted', {
            taskId: current.taskId, lines: merged.lines.length, artifactRows,
            localOnlyLines: merged.lines.length - incoming.length,
        });
    }
    current.history = merged.lines;
    current.historyFiles = files;
    current.historyRequestIDs = requestIDs;
    current.historyClientIDs = clientIDs;
    current.historyEvents = events;
    current.bookLines = incoming.length;
    current.retired = current.retired.filter(item => assistantMessageIndex(merged.lines, requestIDs, files, events, item.requestId) < 0);
    reconcileQueued(current);
    return true;
}

async function reloadHistory(gen: number, ticket: HistoryTicket) {
    const current = owner;
    const op = binding('RemoteControlOp');
    if (!current || current.generation !== gen || !op || !current.grant || !current.taskId) return;
    try {
        const raw = await op(current.grant, 'cloud_get_task', JSON.stringify({ task_id: current.taskId }));
        if (!owner || owner.generation !== gen) return;
        if (!acceptHistory(owner, ticket, raw)) return;
        publishOwner();
    } catch {
        // The live round still shows what already arrived.
    }
}

/** Drop the cloud view without clearing a session the caller is about to publish. */
export function releaseCloudAssistant() {
    if (!owner) return;
    generation += 1;
    owner = null;
    notify();
}

/** Test isolation. Production close goes through closeCloudAssistant. */
export function resetCloudAssistantForTests() {
    generation += 1;
    owner = null;
    awaitingBaseline.clear();
    gapRecoveryAt.clear();
    if (streamOff) {
        const off = streamOff;
        streamOff = null;
        off();
    }
    notify();
    publishRemoteAssistantSession(null);
    bindRemoteAssistantActions(null);
}

/** Leave the cloud task and return the main panel to the local conversation. */
export function closeCloudAssistant() {
    if (!owner) return;
    generation += 1;
    owner = null;
    notify();
    publishRemoteAssistantSession(null);
    bindRemoteAssistantActions(null);
}

async function sendCloud(gen: number, text: string): Promise<boolean> {
    const outgoing = text.trim();
    const current = owner;
    const op = binding('RemoteControlOp');
    if (!current || current.generation !== gen || !op || !current.grant || !outgoing) return false;
    current.pending = true;
    current.pendingBaseline = currentRound(current)?.requestId || '';
    setAwaiting(current);
    publishOwner();
    try {
        let taskId = current.taskId;
        if (!taskId) {
            const setup = current.taskSetup;
            const created = parseJSON<{ task_id?: string; workflow_warning?: string }>(
                await op(current.grant, 'cloud_create_task', JSON.stringify({
                    title: Array.from(outgoing).slice(0, 48).join(''),
                    // The full first instruction doubles as the workflow
                    // summary — cancel-and-execute replays it verbatim.
                    text: outgoing,
                    ...(setup?.workflowTemplateId ? {
                        workflow_template_id: setup.workflowTemplateId,
                        workflow_params: setup.workflowParams || {},
                    } : {}),
                    ...(setup && (setup.expertName || setup.expertPrompt) ? {
                        expert_name: setup.expertName,
                        expert_prompt: setup.expertPrompt,
                    } : {}),
                })),
                {},
            );
            taskId = String(created.task_id || '').trim();
            if (!owner || owner.generation !== gen || !taskId) {
                if (owner && owner.generation === gen) {
                    dropUnansweredSend(owner);
                    publishOwner();
                } else {
                    dropUnansweredSend(current);
                }
                return false;
            }
            owner.taskId = taskId;
            // The wizard's workflow/expert belong to THIS first task only —
            // a later task on the same instance starts plain.
            current.taskSetup = null;
            // A bad template id leaves the task itself usable — the warning
            // lands as a transcript line instead of failing the send.
            const workflowWarning = String(created.workflow_warning || '').trim();
            if (workflowWarning) {
                owner.history = [...owner.history, `assistant\n⚠ ${workflowWarning}`];
                owner.historyRequestIDs = [...owner.historyRequestIDs, ''];
                owner.historyClientIDs = [...owner.historyClientIDs, ''];
                owner.historyEvents = [...owner.historyEvents, []];
                owner.historyEpoch += 1;
            }
            const session = cloudAgentSessionKey(current.item.id, taskId);
            const baseline = cloudAgentRound(cloudAgentStoreKey(current.grant, session)).last;
            const watchRaw = await op(current.grant, 'cloud_watch_task', JSON.stringify({ task_id: taskId, session_key: session }));
            applyWatchedTurn(current.item.id, current.grant, session, parseJSON(watchRaw, {}), baseline);
        }
        if (!owner || owner.generation !== gen) {
            dropUnansweredSend(current);
            return false;
        }
        const clientMessageID = newClientMessageID();
        const sent = parseJSON<{ queued?: boolean; request_id?: string }>(
            await op(current.grant, 'cloud_send_message', JSON.stringify({
                task_id: current.taskId,
                text: outgoing,
                client_message_id: clientMessageID,
            })),
            {},
        );
        // round_started can admit this send before the ack returns. That frame
        // carries the same client id; the book snapshot can already hold the row.
        const raced = current.pendingClientBinds[clientMessageID] || '';
        if (raced) delete current.pendingClientBinds[clientMessageID];
        const bookedAt = historyClientIndex(current, clientMessageID);
        const asked = cloudAgentStoreKey(current.grant, cloudAgentSessionKey(current.item.id, current.taskId));
        if (asked) expireCloudAgentCards(asked);
        // cloud_send_message returns after the control-plane gate decision,
        // but before a round_started stream frame necessarily exists. Pull
        // one authoritative watch snapshot now so plan-gate buttons and the
        // waiting state cannot remain stuck merely because the websocket
        // frame was missed or the panel had not opened the stream yet.
        const refreshSession = cloudAgentSessionKey(current.item.id, current.taskId);
        const refreshBaseline = cloudAgentRound(asked).last;
        void op(current.grant, 'cloud_watch_task', JSON.stringify({ task_id: current.taskId, session_key: refreshSession }))
            .then((watchRaw) => {
                applyWatchedTurn(current.item.id, current.grant, refreshSession, parseJSON(watchRaw, {}), refreshBaseline);
                if (owner && owner.generation === gen) publishOwner();
            })
            .catch(() => undefined);
        current.historyEpoch += 1;
        if (bookedAt >= 0) {
            if (raced && !current.historyRequestIDs[bookedAt]) current.historyRequestIDs[bookedAt] = raced;
        } else {
            const boundRequest = sent.queued === true ? raced : (raced || String(sent.request_id || '').trim());
            if (sent.queued === true && !boundRequest) current.queued = [...current.queued, clientMessageID];
            current.history = [...current.history, `user\n${outgoing}`];
            current.historyRequestIDs = [...current.historyRequestIDs, boundRequest];
            current.historyClientIDs = [...current.historyClientIDs, clientMessageID];
            while (current.historyFiles.length < current.history.length) current.historyFiles.push([]);
            while (current.historyEvents.length < current.history.length) current.historyEvents.push([]);
        }
        current.error = '';
        if (!owner || owner.generation !== gen) return true;
        publishOwner();
        return true;
    } catch (err) {
        if (!owner || owner.generation !== gen) {
            dropUnansweredSend(current);
            return false;
        }
        owner.error = err instanceof Error ? err.message : String(err);
        dropUnansweredSend(owner);
        publishOwner();
        return false;
    }
}

async function stopCloud(gen: number): Promise<boolean> {
    const current = owner;
    const op = binding('RemoteControlOp');
    if (!current || current.generation !== gen || !op || !current.grant || !current.taskId) return false;
    current.stopping = true;
    publishOwner();
    try {
        await op(current.grant, 'cloud_stop_task', JSON.stringify({ task_id: current.taskId }));
        // Cancel returns without a stream frame. Close this instance's round
        // even when the panel has already moved to another task.
        const session = cloudAgentSessionKey(current.item.id, current.taskId);
        applyCloudAgentStream({
            grant_id: current.grant,
            session_key: session,
            project_path: '',
            event: 'response',
        });
        expireCloudAgentCards(cloudAgentStoreKey(current.grant, session));
        current.stopping = false;
        current.pending = false;
        clearAwaiting(current.item.id);
        noteCloudTurn(current.item.id, false);
        if (!owner || owner.generation !== gen) return true;
        publishOwner();
        // Stop writes the interrupt note into the book and does not stream it.
        // The note is that turn's assistant row; reload so the request id joins.
        const ticket = takeHistoryTicket(owner);
        void reloadHistory(gen, ticket);
        return true;
    } catch {
        current.stopping = false;
        if (!owner || owner.generation !== gen) return false;
        publishOwner();
        return false;
    }
}

/**
 * Open one cloud instance in the same panel a remote task uses.
 * List position is the only local/cloud distinction; send, watch, and stop stay here.
 */
export async function openCloudAgent(item: CloudAgentListItem, lang: string): Promise<void> {
    const gen = ++generation;
    rememberCloudOpen(item.id);
    owner = { generation: gen, lang, item, grant: '', taskId: '', history: [], historyFiles: [], historyRequest: '', historyRequestIDs: [], historyClientIDs: [], historyEvents: [], settledTrails: {}, pendingClientBinds: {}, bookLines: 0, retired: [], historyEpoch: 0, historyLoad: 0, error: '', pending: false, pendingBaseline: '', stopping: false, queued: [], git: { connected: false, repo: '', service: '', origin: '', checked: false }, srv: null };
    notify();
    ensureStream();
    publishOwner();
    void refreshCloudSrvNode(gen);
    const open = binding('OpenCloudAgentGrant');
    const op = binding('RemoteControlOp');
    if (!open || !op) {
        if (!owner || owner.generation !== gen) return;
        owner.error = localizeText(lang, 'Cloud control is unavailable', '云端控制不可用', '雲端控制不可用');
        publishOwner();
        return;
    }
    try {
        const grant = String(await open(item.id) || '').trim();
        if (!owner || owner.generation !== gen) return;
        if (!grant) {
            owner.error = localizeText(lang, 'Cloud control is unavailable', '云端控制不可用', '雲端控制不可用');
            publishOwner();
            return;
        }
        owner.grant = grant;
        void refreshCloudGit();
        const listed = parseJSON<{ tasks?: Array<{ id?: string }> }>(
            await op(grant, 'cloud_list_tasks', ''),
            {},
        );
        if (!owner || owner.generation !== gen) return;
        const tasks = Array.isArray(listed.tasks) ? listed.tasks : [];
        const taskId = String(tasks[tasks.length - 1]?.id || '').trim();
        owner.taskId = taskId;
        if (taskId) {
            const session = cloudAgentSessionKey(item.id, taskId);
            const baseline = cloudAgentRound(cloudAgentStoreKey(grant, session)).last;
            const watchRaw = await op(grant, 'cloud_watch_task', JSON.stringify({ task_id: taskId, session_key: session }));
            if (!owner || owner.generation !== gen) return;
            applyWatchedTurn(item.id, grant, session, parseJSON(watchRaw, {}), baseline);
            const ticket = takeHistoryTicket(owner);
            const raw = await op(grant, 'cloud_get_task', JSON.stringify({ task_id: taskId }));
            if (!owner || owner.generation !== gen) return;
            acceptHistory(owner, ticket, raw);
        }
        publishOwner();
    } catch (err) {
        if (!owner || owner.generation !== gen) return;
        owner.error = err instanceof Error ? err.message : String(err);
        publishOwner();
    }
}

/**
 * Wizard selections a cloud task carries. The expert id resolves through the
 * desktop's store to the name + persona the instance replays every turn;
 * workflow fields land on cloud_create_task untouched.
 */
export type CloudTaskSetup = {
    workflowTemplateId?: string;
    workflowParams?: Record<string, string>;
    expertId?: string;
};

/** expert id → {name, system_prompt} via the CloudAgentExpertProfile binding. */
async function resolveCloudExpertProfile(expertId: string): Promise<{ name: string; prompt: string } | null> {
    const id = (expertId || '').trim();
    if (!id) return null;
    const profile = binding('CloudAgentExpertProfile');
    if (!profile) return null;
    try {
        const parsed = parseJSON<{ name?: string; system_prompt?: string }>(await profile(id), {});
        return { name: String(parsed.name || '').trim(), prompt: String(parsed.system_prompt || '').trim() };
    } catch {
        return null;
    }
}

/**
 * New-task send when 位置 is 云端. Same composer text, different place:
 * create the instance, open it in the main assistant, and deliver the first message.
 * The returned id is the created instance. A warning means that instance exists
 * and the first instruction was not accepted.
 */
export async function startCloudAgentFromNewTask(name: string, kind: 'session' | 'coding', text: string, lang: string, setup?: CloudTaskSetup): Promise<{ id: string; warning?: string }> {
    const create = binding('CreateCloudAgent');
    if (!create) {
        throw new Error(localizeText(lang, 'Cloud runtime is unavailable', '云端运行时不可用', '雲端運行時不可用'));
    }
    const row = await create(name, kind);
    const item = asCloudAgentListItem(row);
    if (!item) {
        throw new Error(localizeText(lang, 'The cloud task was not created', '云端任务没有创建', '雲端任務沒有建立'));
    }
    // Resolve the picked expert before the open races the first send — the
    // profile rides cloud_create_task once, not every message.
    const workflowTemplateId = (setup?.workflowTemplateId || '').trim();
    const expertId = (setup?.expertId || '').trim();
    const expert = expertId ? await resolveCloudExpertProfile(expertId) : null;
    const taskSetup = (workflowTemplateId || expertId) ? {
        workflowTemplateId,
        workflowParams: setup?.workflowParams || {},
        expertName: expert?.name || '',
        expertPrompt: expert?.prompt || '',
    } : null;
    requestCloudAgentListRefresh();
    await openCloudAgent(item, lang);
    if (owner && owner.item.id === item.id) owner.taskSetup = taskSetup;
    // The instance already exists. A later open or send failure must not look
    // like "nothing was created", or a scheduled launch will create another one.
    if (!owner || owner.item.id !== item.id || !owner.grant) {
        const message = owner && owner.item.id === item.id ? owner.error : '';
        return {
            id: item.id,
            warning: message || localizeText(lang, 'Cloud control is unavailable', '云端控制不可用', '雲端控制不可用'),
        };
    }
    const ok = await sendCloud(owner.generation, text);
    if (!ok) {
        const message = owner && owner.item.id === item.id ? owner.error : '';
        return {
            id: item.id,
            warning: message || localizeText(lang, 'The cloud task was created, but the first instruction was not sent', '云端任务已创建，但首条指令未发出', '雲端任務已建立，但第一則指令沒有送出'),
        };
    }
    return { id: item.id };
}

/**
 * Resolves the node id recorded on the instance to a name/address the header
 * can show. Only a private srv needs the lookup — the official label needs
 * no round trip. A failed read leaves the node id on the header.
 */
async function refreshCloudSrvNode(gen: number): Promise<void> {
    const current = owner;
    if (!current) return;
    // The instance records the srv it provisioned on. A row that never
    // landed (provisioning/provision_failed) has no record yet; its next
    // attempt goes to the user's current pin, so the pin is the right label.
    const recorded = String(current.item.srvNodeId || '').trim();
    const pending = current.item.status === 'provisioning' || current.item.status === 'provision_failed';
    if (!recorded && !pending) return;
    const choice = binding('GetCloudSrvChoice');
    if (!choice) return;
    try {
        const state = await choice() as {
            node_id?: string;
            nodes?: Array<{ node_id?: string; name?: string; address?: string; online?: boolean }>;
        };
        const nodeId = recorded || String(state?.node_id || '').trim();
        if (!nodeId || !owner || owner.generation !== gen) return;
        const nodes = Array.isArray(state?.nodes) ? state.nodes : [];
        const node = nodes.find(candidate => String(candidate?.node_id || '').trim() === nodeId);
        owner.srv = {
            nodeId,
            name: String(node?.name || '').trim(),
            address: String(node?.address || '').trim(),
            online: node ? node.online === true : true,
        };
        publishOwner();
    } catch {
        // The node id still identifies the private server on the header.
    }
}

/**
 * Re-read the open instance's repository binding. The connect dialog calls
 * this after ConnectCloudAgentGit so the banner flips without a reopen.
 */
export async function refreshCloudGit(instanceId = ''): Promise<void> {
    const current = owner;
    if (!current || current.item.kind !== 'coding') return;
    if (instanceId && current.item.id !== instanceId) return;
    const get = binding('GetCloudAgentGit');
    if (!get) return;
    try {
        const view = await get(current.item.id) as { connected?: boolean; repo?: string; service?: string; origin?: string };
        if (!owner || owner.generation !== current.generation) return;
        owner.git = {
            connected: view?.connected === true,
            repo: String(view?.repo || ''),
            service: String(view?.service || ''),
            origin: String(view?.origin || ''),
            checked: true,
        };
        publishOwner();
        // ConnectGit rewrites produced paths in the book. The dialog passes
        // the instance id; the open-time refresh does not, so it cannot
        // retire the history fetch that is already in flight.
        if (instanceId && owner.taskId) {
            const ticket = takeHistoryTicket(owner);
            void reloadHistory(owner.generation, ticket);
        }
    } catch {
        // A failed read still opens the entry: a wrong banner on a bound
        // instance only leads to a clear "already connected" error, while
        // hiding it on a repo-less instance removes the only connect path.
        if (owner && owner.generation === current.generation) {
            owner.git.checked = true;
            publishOwner();
            if (instanceId && owner.taskId) {
                const ticket = takeHistoryTicket(owner);
                void reloadHistory(owner.generation, ticket);
            }
        }
    }
}

/**
 * Optimistically record a successful ConnectCloudAgentGit before the slower
 * GetCloudAgentGit re-read lands, so the 未连接代码库 hint disappears the
 * moment the connect call resolves instead of one Hub round-trip later.
 * view is the connect response itself (a CloudGitPublic), so the recorded
 * binding is what the backend just accepted. refreshCloudGit still runs and
 * reconciles; its failure path only flips `checked` and keeps this state.
 */
export function noteCloudGitConnected(instanceId: string, view: { service?: string; origin?: string; repo?: string } | null): void {
    const current = owner;
    if (!current || current.item.kind !== 'coding') return;
    if (instanceId && current.item.id !== instanceId) return;
    // Synchronous: `current` is still `owner`, and mutating through the
    // narrowed alias keeps TS happy without a redundant re-guard.
    current.git = {
        connected: true,
        repo: String(view?.repo || '').trim(),
        service: String(view?.service || '').trim(),
        origin: String(view?.origin || '').trim(),
        checked: true,
    };
    publishOwner();
}

/** Send through the open panel when it is already on this instance and task. */
export function sendShownCloudAgent(instanceId: string, taskId: string, text: string): Promise<boolean> | null {
    if (!owner || owner.item.id !== instanceId || !owner.grant) return null;
    if ((taskId || '') !== (owner.taskId || '')) return null;
    return sendCloud(owner.generation, text);
}

/** Stop through the open panel when it is already on this instance and task. */
export function stopShownCloudAgent(instanceId: string, taskId: string): Promise<boolean> | null {
    if (!owner || owner.item.id !== instanceId || owner.taskId !== taskId) return null;
    return stopCloud(owner.generation);
}

/**
 * Submit the workflow form the open cloud task is gating on. The instance
 * continues with a phase turn behind the same send slot, so this borrows
 * sendCloud's pending bookkeeping rather than tracking a separate flight.
 */
export async function submitCloudWorkflowForm(values: Record<string, unknown>): Promise<boolean> {
    const current = owner;
    const op = binding('RemoteControlOp');
    if (!current || !op || !current.grant || !current.taskId) return false;
    const gen = current.generation;
    current.pending = true;
    current.pendingBaseline = currentRound(current)?.requestId || '';
    setAwaiting(current);
    publishOwner();
    try {
        await op(current.grant, 'cloud_submit_workflow_form', JSON.stringify({
            task_id: current.taskId,
            values,
            client_message_id: newClientMessageID(),
        }));
        if (!owner || owner.generation !== gen) return true;
        publishOwner();
        return true;
    } catch (err) {
        if (!owner || owner.generation !== gen) {
            dropUnansweredSend(current);
            return false;
        }
        owner.error = err instanceof Error ? err.message : String(err);
        dropUnansweredSend(owner);
        publishOwner();
        return false;
    }
}

export function cloudAgentsFromList(value: unknown): CloudAgentListItem[] {
    if (!Array.isArray(value)) return [];
    return value.map(asCloudAgentListItem).filter((item): item is CloudAgentListItem => item != null);
}

/** Hub list. enabled is the tenant switch. A bare array has no switch, so the place stays hidden. */
export function cloudAgentCatalog(value: unknown): { instances: unknown[]; enabled: boolean } {
    if (Array.isArray(value)) return { instances: value, enabled: false };
    if (!value || typeof value !== 'object') return { instances: [], enabled: false };
    const body = value as { instances?: unknown; enabled?: unknown };
    return {
        instances: Array.isArray(body.instances) ? body.instances : [],
        enabled: body.enabled === true,
    };
}
