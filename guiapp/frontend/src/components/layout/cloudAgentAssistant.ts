import { useSyncExternalStore } from 'react';
import { EventsOn } from '../../../wailsjs/runtime';
import { localizeText } from '../../i18n';
import {
    applyCloudAgentStream,
    applyCloudAgentWatch,
    cloudAgentRound,
    cloudAgentSessionKey,
    cloudAgentStoreKey,
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
    /** Sends the srv parked while a turn was running, in FIFO order. */
    queued: string[];
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

function historyMessages(lines: string[], files: string[][]): RemoteAssistantTranscriptMessage[] {
    const messages: RemoteAssistantTranscriptMessage[] = [];
    for (const [index, line] of lines.entries()) {
        const split = line.indexOf('\n');
        const role = (split < 0 ? line : line.slice(0, split)).trim() || 'assistant';
        const content = split < 0 ? '' : line.slice(split + 1);
        if (!content.trim()) continue;
        const produced = (files[index] || []).map(path => String(path || '').trim()).filter(Boolean);
        messages.push({ role, content, localFilePaths: produced.length ? produced : undefined });
    }
    return messages;
}

function stripOrigin(text: string): string {
    return text.replace(/^〔[^〕]*〕/, '').trim();
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
    if (!op || !grant || !session || !taskId || !instanceId) return;
    const baseline = cloudAgentRound(key).last;
    void op(grant, 'cloud_watch_task', JSON.stringify({ task_id: taskId, session_key: session }))
        .then(raw => applyWatchedTurn(instanceId, grant, session, parseJSON(raw, {}), baseline))
        .catch(() => { /* the next gapped frame retries */ });
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

function publishOwner() {
    const current = owner;
    if (!current) return;
    const text = (en: string, zh: string, hant?: string) => localizeText(current.lang, en, zh, hant);
    const sessionKey = cloudAgentSessionKey(current.item.id, current.taskId);
    const storeKey = cloudAgentStoreKey(current.grant, sessionKey);
    const round = storeKey ? cloudAgentRound(storeKey) : null;
    const messages = historyMessages(current.history, current.historyFiles);
    const prompt = stripOrigin(round?.displayText || '');
    if (prompt && !messages.some(message => message.role === 'user' && message.content.trim() === prompt)) {
        messages.push({ role: 'user', content: prompt });
    }
    const live = cloudLive(round);
    const liveText = (round?.visible || round?.body || '').trim();
    const reasoning = (round?.thinking || []).map(block => block.text).filter(Boolean).join('\n\n');
    // Styled progress rows become the same tool timeline a local turn keeps.
    const folded = remoteLiveToolCalls(live?.parts || [], liveText);
    const last = messages[messages.length - 1];
    // The round still on screen is the one this send already saw. Keep it above
    // the new line; a later request paints its own text after that line.
    // Files the turn produced on the instance ride the round's artifacts list.
    const produced = (round?.artifacts || []).map(path => String(path || '').trim()).filter(Boolean);
    const producedFiles = produced.length ? produced : undefined;
    const previousRound = current.pending && !!round?.requestId && round.requestId === current.pendingBaseline;
    if (previousRound && last?.role === 'user') {
        const prior = messages.slice(0, -1);
        const seen = !!liveText && prior.some(message => message.role !== 'user' && message.content.trim() === liveText);
        if (!seen && (liveText || reasoning || folded.toolCalls)) {
            messages.splice(messages.length - 1, 0, {
                role: 'assistant',
                content: folded.content,
                reasoning: reasoning || undefined,
                remoteLive: true,
                liveThinking: !!round?.thinking.some(block => !block.done),
                toolCalls: folded.toolCalls,
                localFilePaths: producedFiles,
            });
        }
    } else if (liveText && last && last.role !== 'user' && last.content.trim() === liveText) {
        if (reasoning && !last.reasoning) last.reasoning = reasoning;
        if (folded.toolCalls && !last.toolCalls) last.toolCalls = folded.toolCalls;
        if (producedFiles && !last.localFilePaths) last.localFilePaths = producedFiles;
    } else if (liveText || reasoning || folded.toolCalls) {
        messages.push({
            role: 'assistant',
            content: folded.content,
            reasoning: reasoning || undefined,
            remoteLive: true,
            liveThinking: !!round?.thinking.some(block => !block.done),
            toolCalls: folded.toolCalls,
            localFilePaths: producedFiles,
        });
    }
    // Sends the srv parked wait for the running turn; a round_started drops
    // the drained entries, so mark exactly the still-waiting ones. The live
    // round's own line is never one of them — its drain could have raced
    // ahead of the send ack that would have removed it here.
    if (current.queued.length) {
        const parked = current.queued.slice();
        for (let i = messages.length - 1, qi = parked.length - 1; i >= 0 && qi >= 0; i--) {
            const row = messages[i];
            if (row.role !== 'user') continue;
            if (row.content.trim() === prompt) continue;
            if (row.content.trim() === parked[qi]) {
                row.queued = true;
                qi--;
            }
        }
    }
    const card = round?.cards.find(item => !item.expired && (item.actions?.length || item.text || item.confirmationId));
    if (card) {
        const host = messages[messages.length - 1];
        const actions = (card.actions || []).filter(action => action.command);
        const confirmation = card.confirmationId ? { id: card.confirmationId, summary: card.text } : undefined;
        if (host && host.role !== 'user') {
            host.actions = actions;
            host.confirmation = confirmation;
            if (!host.content.trim() && card.text) host.content = card.text;
        } else {
            messages.push({
                role: card.asError ? 'error' : 'assistant',
                content: card.text || liveText,
                actions,
                confirmation,
            });
        }
    }
    for (const note of round?.notices || []) {
        if (note.text) messages.push({ role: note.asError ? 'error' : 'assistant', content: note.text });
    }
    publishRemoteAssistantSession({
        grantId: current.grant || current.item.id,
        machineName: text('Cloud', '云端', '雲端'),
        projectPath: `cloud-agent:${current.item.id}`,
        taskName: current.item.name || current.item.id,
        cloud: true,
        canSend: !!current.grant,
        offline: false,
        error: current.error,
        loading: !current.grant && !current.error,
        busy: observeTurn(current, round),
        activity: current.stopping ? { stopping: true } : undefined,
        live,
        messages,
    });
    bindRemoteAssistantActions({
        send: textToSend => sendCloud(current.generation, textToSend),
        stop: () => stopCloud(current.generation),
        close: closeCloudAssistant,
    });
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
        // A fresh round means the srv drained a parked send. Drop every entry
        // up to it — the queue is FIFO, so anything older started as well
        // (its frame may have raced past the send ack).
        const started = streamStarted(payload);
        const drained = started ? current.queued.indexOf(started) : -1;
        if (drained >= 0) {
            current.queued = current.queued.slice(drained + 1);
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

/** Server history is the transcript, plus local user lines it does not have yet. */
function keepUnackedUserLines(local: string[], incoming: string[]): string[] {
    const missing = userLineCount(local) - userLineCount(incoming);
    if (missing <= 0) return incoming;
    const suffix: string[] = [];
    for (let index = local.length - 1; index >= 0 && suffix.length < missing; index -= 1) {
        if (!local[index].startsWith('user\n')) break;
        suffix.push(local[index]);
    }
    if (suffix.length !== missing) return incoming;
    suffix.reverse();
    return [...incoming, ...suffix];
}

/** A fetch writes only when no send and no newer fetch has happened since it started. */
function acceptHistory(current: Owner, ticket: HistoryTicket, raw: unknown): boolean {
    if (current.historyEpoch !== ticket.epoch || current.historyLoad !== ticket.load) return false;
    const body = parseJSON<{ messages?: string[]; files?: unknown[] }>(raw, {});
    const incoming = Array.isArray(body.messages) ? body.messages : [];
    const merged = keepUnackedUserLines(current.history, incoming);
    // files[i] belongs to messages[i]; locally-appended user lines have none.
    const files: string[][] = [];
    for (let index = 0; index < merged.length; index += 1) {
        const row = Array.isArray(body.files?.[index]) ? body.files[index] as string[] : [];
        files.push(row);
    }
    current.history = merged;
    current.historyFiles = files;
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
            const created = parseJSON<{ task_id?: string }>(
                await op(current.grant, 'cloud_create_task', JSON.stringify({
                    title: Array.from(outgoing).slice(0, 48).join(''),
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
            const session = cloudAgentSessionKey(current.item.id, taskId);
            const baseline = cloudAgentRound(cloudAgentStoreKey(current.grant, session)).last;
            const watchRaw = await op(current.grant, 'cloud_watch_task', JSON.stringify({ task_id: taskId, session_key: session }));
            applyWatchedTurn(current.item.id, current.grant, session, parseJSON(watchRaw, {}), baseline);
        }
        if (!owner || owner.generation !== gen) {
            dropUnansweredSend(current);
            return false;
        }
        const sent = parseJSON<{ queued?: boolean }>(
            await op(current.grant, 'cloud_send_message', JSON.stringify({
                task_id: current.taskId,
                text: outgoing,
                client_message_id: `cm_${Date.now()}`,
            })),
            {},
        );
        // A parked send keeps a FIFO slot; the next round_started drains it.
        if (sent.queued === true) {
            current.queued = [...current.queued, outgoing];
        }
        const asked = cloudAgentStoreKey(current.grant, cloudAgentSessionKey(current.item.id, current.taskId));
        if (asked) expireCloudAgentCards(asked);
        current.historyEpoch += 1;
        current.history = [...current.history, `user\n${outgoing}`];
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
    owner = { generation: gen, lang, item, grant: '', taskId: '', history: [], historyFiles: [], historyRequest: '', historyEpoch: 0, historyLoad: 0, error: '', pending: false, pendingBaseline: '', stopping: false, queued: [] };
    notify();
    ensureStream();
    publishOwner();
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
 * New-task send when 位置 is 云端. Same composer text, different place:
 * create the instance, open it in the main assistant, and deliver the first message.
 * The returned id is the created instance. A warning means that instance exists
 * and the first instruction was not accepted.
 */
export async function startCloudAgentFromNewTask(name: string, kind: 'session' | 'coding', text: string, lang: string): Promise<{ id: string; warning?: string }> {
    const create = binding('CreateCloudAgent');
    if (!create) {
        throw new Error(localizeText(lang, 'Cloud runtime is unavailable', '云端运行时不可用', '雲端運行時不可用'));
    }
    const row = await create(name, kind);
    const item = asCloudAgentListItem(row);
    if (!item) {
        throw new Error(localizeText(lang, 'The cloud task was not created', '云端任务没有创建', '雲端任務沒有建立'));
    }
    requestCloudAgentListRefresh();
    await openCloudAgent(item, lang);
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
