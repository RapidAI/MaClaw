import { useSyncExternalStore } from 'react';
import { EventsOn } from '../../../wailsjs/runtime';

export type RemoteAssistantTranscriptMessage = {
    role: string;
    content: string;
    reasoning?: string;
    /** Files this turn produced on the controlled machine; not local paths. */
    localFilePaths?: string[];
    /** Synthetic row built from the live mirror — not in the transcript yet. */
    remoteLive?: boolean;
    /** The live row's thinking is still open on the controlled machine. */
    liveThinking?: boolean;
};

/** One ordered piece of a mirrored round; identical to remoteStreamPart in guiapp. */
export type RemoteLivePart = {
    kind: 'text' | 'thinking' | 'progress';
    seq: number;
    text: string;
    done?: boolean;
};

/** The in-flight round mirrored from the controlled machine over remote_control.stream. */
export type RemoteLiveRound = {
    requestId: string;
    displayText: string;
    parts: RemoteLivePart[];
    streamDone: boolean;
    responded: boolean;
    /** Transcript length when the round was first seen; growth past it settles the round. */
    baseline: number;
    startedAt?: number;
    /** Local receive time of the last stream event; stale rounds are dropped. */
    lastEventAt: number;
    /** Settled answer text carried by the response event, for transcript dedupe. */
    responseText?: string;
};

/** Compact workflow snapshot mirrored from the remote task row. */
export type RemoteAssistantWorkflow = {
    type?: string;
    phase?: string;
    status?: string;
    pending_review?: boolean;
};

/**
 * Live status mirrored from the remote task snapshot. Everything here comes
 * from the get_task poll, so it refreshes on the same cadence (3s while the
 * task runs) rather than streaming.
 */
export type RemoteAssistantActivity = {
    /** A send was accepted but the remote transcript has not answered yet. */
    awaitingReply?: boolean;
    /** The in-flight op is a stop, not a send — graded "stopping" label. */
    stopping?: boolean;
    /** active_workflow snapshot on the remote task row. */
    workflow?: RemoteAssistantWorkflow | null;
    /** workflow_type of the remote task, e.g. "coding". */
    workflowType?: string;
    /** RFC3339 heartbeat from the remote task row's last_activity. */
    lastActivityAt?: string;
    /** Local timestamp when this run was first observed, for elapsed time. */
    busySince?: number;
};

export type RemoteAssistantSession = {
    grantId: string;
    machineName: string;
    projectPath: string;
    taskName: string;
    canSend: boolean;
    offline: boolean;
    error: string;
    /** The task is open but its transcript is still on the way. */
    loading?: boolean;
    /** The other computer is working on the open task. */
    busy?: boolean;
    /** Live status mirrored from the remote task snapshot. */
    activity?: RemoteAssistantActivity;
    /** The in-flight round mirrored live; absent when nothing streams. */
    live?: RemoteLiveRound | null;
    messages: RemoteAssistantTranscriptMessage[];
};

type RemoteAssistantActions = {
    send: (text: string) => Promise<boolean | void>;
    stop: () => Promise<boolean | void>;
    close: () => void;
};

// A sent line the remote transcript has not echoed yet. The other computer
// can swallow an input without storing a visible user row (a workflow form
// answer, for example), so the line stays rendered until a real copy lands.
type SentMark = {
    key: string;
    text: string;
    /** Copies of this text already in the transcript when the send left. */
    baseline: number;
    /**
     * Transcript length at send time; the echo slot for this line. -1 while
     * the transcript is still loading — the first real fetch anchors it.
     */
    at: number;
};

let raw: RemoteAssistantSession | null = null;
let marks: SentMark[] = [];
let current: RemoteAssistantSession | null = null;
let actions: RemoteAssistantActions | null = null;
let flight: Promise<boolean> | null = null;
const listeners = new Set<() => void>();

function userTextCount(messages: RemoteAssistantTranscriptMessage[], text: string) {
    let count = 0;
    for (const message of messages) {
        if (message.role === 'user' && message.content.trim() === text) count += 1;
    }
    return count;
}

function markKey(session: RemoteAssistantSession | null) {
    return session ? `${session.grantId}${session.projectPath}` : '';
}

// --- Live mirror (remote_control.stream) ---
//
// The controlled desktop relays its assistant stream events while a task is
// watched. This store folds them into one RemoteLiveRound per session, and
// withEchoes projects the round onto the transcript as a synthetic assistant
// row: streamed text plus the finished thinking blocks. Reasoning deltas are
// never held live — the controlled side ships a thinking block only when it
// closes, so the 思考 box appears first and fills in afterwards.

const liveRounds = new Map<string, RemoteLiveRound>();

function liveKey(grantId: string, projectPath: string) {
    return grantId && projectPath ? `${grantId}${projectPath}` : '';
}

/** External-origin user rows carry a 〔远控〕/〔…〕 prefix — strip it for dedupe and display. */
function stripRemoteOriginMarker(text: string) {
    return String(text || '').replace(/^〔[^〕]*〕/, '').trim();
}

function closeLiveThinking(round: RemoteLiveRound) {
    for (const part of round.parts) {
        if (part.kind === 'thinking' && !part.done) part.done = true;
    }
}

function ensureLiveRound(key: string, requestId: string): RemoteLiveRound {
    let round = liveRounds.get(key);
    if (!round || (requestId && round.requestId && round.requestId !== requestId)) {
        round = {
            requestId: requestId || '',
            displayText: '',
            parts: [],
            streamDone: false,
            responded: false,
            baseline: -1,
            lastEventAt: 0,
        };
        liveRounds.set(key, round);
    }
    if (requestId && !round.requestId) round.requestId = requestId;
    return round;
}

type RemoteStreamPayload = {
    grant_id?: string;
    project_path?: string;
    request_id?: string;
    event?: string;
    seq?: number;
    text?: string;
    state?: string;
    display_text?: string;
    response?: { text?: string } | null;
};

/** Folds one remote_control.stream event into the session's live round. */
export function applyRemoteStreamPayload(payload: unknown) {
    const parsed = typeof payload === 'string' ? (() => { try { return JSON.parse(payload); } catch { return null; } })() : payload;
    const p = (parsed && typeof parsed === 'object' ? parsed : {}) as RemoteStreamPayload;
    const key = liveKey(String(p.grant_id || '').trim(), String(p.project_path || '').trim());
    const eventName = String(p.event || '');
    if (!key || !eventName) return;
    const requestId = String(p.request_id || '');
    const seq = typeof p.seq === 'number' ? p.seq : 0;
    let round: RemoteLiveRound | undefined;
    switch (eventName) {
        case 'round_started': {
            round = ensureLiveRound(key, requestId);
            round.displayText = String(p.display_text || '');
            break;
        }
        case 'token': {
            round = ensureLiveRound(key, requestId);
            const text = String(p.text || '');
            if (text.startsWith('\x02')) {
                // Rejected-round reset: drop the trailing draft text run, keep
                // whatever follows the marker as the fresh content.
                while (round.parts.length && round.parts[round.parts.length - 1].kind === 'text') round.parts.pop();
                const body = text.slice(1);
                if (body) round.parts.push({ kind: 'text', seq, text: body });
            } else if (text) {
                const tail = round.parts[round.parts.length - 1];
                if (tail && tail.kind === 'text') tail.text += text;
                else round.parts.push({ kind: 'text', seq, text });
            }
            break;
        }
        case 'thinking': {
            round = ensureLiveRound(key, requestId);
            if (p.state === 'done') {
                const tail = round.parts[round.parts.length - 1];
                if (tail && tail.kind === 'thinking' && !tail.done) {
                    tail.text = String(p.text || '');
                    tail.done = true;
                } else {
                    round.parts.push({ kind: 'thinking', seq, text: String(p.text || ''), done: true });
                }
            } else {
                round.parts.push({ kind: 'thinking', seq, text: '', done: false });
            }
            break;
        }
        case 'progress': {
            round = ensureLiveRound(key, requestId);
            const text = String(p.text || '');
            if (text) round.parts.push({ kind: 'progress', seq, text });
            break;
        }
        case 'new_round': {
            round = ensureLiveRound(key, requestId);
            closeLiveThinking(round);
            break;
        }
        case 'stream_done': {
            round = ensureLiveRound(key, requestId);
            closeLiveThinking(round);
            round.streamDone = true;
            break;
        }
        case 'response': {
            round = ensureLiveRound(key, requestId);
            closeLiveThinking(round);
            round.streamDone = true;
            round.responded = true;
            // A turn that streamed nothing still has an answer to show.
            const text = p.response && typeof p.response.text === 'string' ? p.response.text : '';
            if (text) round.responseText = text;
            if (text && !round.parts.some(part => part.kind === 'text' && part.text)) {
                round.parts.push({ kind: 'text', seq, text });
            }
            break;
        }
        default:
            return;
    }
    round.lastEventAt = Date.now();
    emit();
}

type RemoteWatchSnapshot = {
    request_id?: string;
    project_path?: string;
    display_text?: string;
    parts?: RemoteLivePart[];
    stream_done?: boolean;
    responded?: boolean;
    started_at?: number;
};

/** Installs the in-flight turn a watch_task reply carried (mid-turn catch-up). */
export function applyRemoteWatchSnapshot(grantId: string, live: unknown) {
    const p = (live && typeof live === 'object' ? live : null) as RemoteWatchSnapshot | null;
    if (!p || !String(p.request_id || '')) return;
    const key = liveKey(grantId, String(p.project_path || '').trim());
    if (!key) return;
    liveRounds.set(key, {
        requestId: String(p.request_id || ''),
        displayText: String(p.display_text || ''),
        parts: Array.isArray(p.parts) ? p.parts.filter(part => part && typeof part === 'object') : [],
        streamDone: !!p.stream_done,
        responded: !!p.responded,
        baseline: raw ? raw.messages.length : -1,
        startedAt: typeof p.started_at === 'number' && p.started_at > 0 ? p.started_at : undefined,
        lastEventAt: Date.now(),
    });
    emit();
}

// Settles the live round against the polled transcript. The response having
// landed plus a fresh non-user tail means the transcript now owns the turn.
// The timeout path covers answers the transcript never grows for (stopped or
// unwritten turns), so a mirrored draft cannot outlive its round forever.
function settleLiveRounds() {
    if (!raw) {
        liveRounds.clear();
        return;
    }
    const key = markKey(raw);
    for (const roundKey of [...liveRounds.keys()]) {
        if (roundKey !== key) liveRounds.delete(roundKey);
    }
    const round = liveRounds.get(key);
    if (!round) return;
    if (round.baseline < 0) round.baseline = raw.messages.length;
    const staleMs = round.responded ? 90_000 : 180_000;
    if (Date.now() - (round.lastEventAt || 0) > staleMs) {
        liveRounds.delete(key);
        return;
    }
    if (!round.responded) return;
    const tail = raw.messages[raw.messages.length - 1];
    if (!tail || tail.role === 'user') return;
    // The transcript owns the turn once it grows past the round's baseline,
    // or when its tail already carries this answer — a response can land
    // between polls and baseline counts whatever was fetched, not what the
    // round saw, so the content check is the real arbiter.
    const roundText = (round.responseText || round.parts.filter(part => part.kind === 'text').map(part => part.text).join('')).trim();
    const tailText = (tail.content || '').trim();
    const covered = raw.messages.length > round.baseline
        || (roundText !== '' && tailText !== '' && tailText.startsWith(roundText.slice(0, Math.min(60, roundText.length))));
    if (covered) liveRounds.delete(key);
}

let remoteStreamBound = false;

/** Starts forwarding remote-control:stream Wails events into the live store. */
export function bindRemoteStreamEvents() {
    if (remoteStreamBound) return;
    remoteStreamBound = true;
    try {
        EventsOn('remote-control:stream', (payload: unknown) => applyRemoteStreamPayload(payload));
    } catch {
        remoteStreamBound = false;
    }
}

function withEchoes(session: RemoteAssistantSession | null): RemoteAssistantSession | null {
    if (!session) return session;
    const key = markKey(session);
    let merged: RemoteAssistantTranscriptMessage[] | null = null;
    // Two sends can share one anchor (the transcript did not move between
    // them). Counting earlier injections keeps their send order intact.
    let offset = 0;
    for (const mark of marks) {
        if (mark.key !== key) continue;
        // publish already drops marks the transcript echoed; every mark left
        // here is an unsettled send and keeps its own rendered line.
        merged = merged ?? session.messages.slice();
        const at = mark.at < 0 ? merged.length : Math.min(mark.at + offset, merged.length);
        merged.splice(at, 0, { role: 'user', content: mark.text });
        offset += 1;
    }
    const round = liveRounds.get(key);
    if (round) {
        merged = merged ?? session.messages.slice();
        // A round the controller did not send (started on the other machine,
        // or by a second controller) still owns its user row until the
        // transcript echoes it.
        const displayText = stripRemoteOriginMarker(round.displayText);
        if (displayText && !merged.some(message => message.role === 'user' && message.content.trim() === displayText)) {
            merged.push({ role: 'user', content: displayText });
        }
        const thinking = round.parts
            .filter(part => part.kind === 'thinking' && part.text)
            .map(part => part.text)
            .join('\n\n');
        const content = round.parts
            .filter(part => part.kind === 'text')
            .map(part => part.text)
            .join('');
        const liveThinking = round.parts.some(part => part.kind === 'thinking' && !part.done);
        if (content || thinking || liveThinking) {
            merged.push({ role: 'assistant', content, reasoning: thinking || undefined, remoteLive: true, liveThinking });
        }
        return { ...session, messages: merged, live: round };
    }
    return merged ? { ...session, messages: merged } : session;
}

function sessionKey(session: RemoteAssistantSession | null) {
    if (!session) return '';
    return [
        session.grantId,
        session.projectPath,
        session.canSend ? '1' : '0',
        session.offline ? '1' : '0',
        session.loading ? '1' : '0',
        session.busy ? '1' : '0',
        session.machineName,
        session.taskName,
        session.error,
        session.activity ? JSON.stringify(session.activity) : '',
        session.live ? JSON.stringify(session.live.parts) + (session.live.responded ? 'r' : '') : '',
        session.messages.map(message => `${message.role}\n${message.content}\n${message.reasoning || ''}\n${(message.localFilePaths || []).join(',')}`).join('\n--\n'),
    ].join('\n');
}

function emit() {
    const next = withEchoes(raw);
    if (sessionKey(next) === sessionKey(current)) return;
    current = next;
    listeners.forEach(listener => listener());
}

export function publishRemoteAssistantSession(next: RemoteAssistantSession | null) {
    if (next) {
        const key = markKey(next);
        // A send made while the transcript was loading anchors its echo at
        // the end of the history this fetch just delivered.
        for (const mark of marks) {
            if (mark.key === key && mark.at < 0) mark.at = next.messages.length;
        }
        // Each transcript copy above a mark's baseline settles one mark with
        // that text, so two sends of the same line keep two echoes.
        const consumed = new Map<string, number>();
        marks = marks.filter(mark => {
            if (mark.key !== key) return true;
            const seen = userTextCount(next.messages, mark.text) - (consumed.get(mark.text) || 0);
            if (seen > mark.baseline) {
                consumed.set(mark.text, (consumed.get(mark.text) || 0) + 1);
                return false;
            }
            return true;
        });
    } else {
        // Only the session that just closed loses its unsettled echoes. A
        // send on another task keeps its line for when it is reopened.
        const key = markKey(raw);
        marks = marks.filter(mark => mark.key !== key);
    }
    raw = next;
    settleLiveRounds();
    emit();
}

export function bindRemoteAssistantActions(next: RemoteAssistantActions | null) {
    actions = next;
}

export function useRemoteAssistantSession(): RemoteAssistantSession | null {
    return useSyncExternalStore(
        (listener: () => void) => {
            listeners.add(listener);
            return () => listeners.delete(listener);
        },
        () => current,
        () => null,
    );
}

export function remoteAssistantActive() {
    return raw != null;
}

export function remoteAssistantCanSend() {
    return !!raw?.canSend;
}

export function remoteAssistantSending() {
    return flight != null;
}

export function remoteAssistantSend(text: string) {
    const trimmed = text.trim();
    const send = actions?.send;
    if (!raw?.canSend || !send || !trimmed || flight) return Promise.resolve(false);
    const mark: SentMark = {
        key: markKey(raw),
        text: trimmed,
        baseline: userTextCount(raw.messages, trimmed),
        at: raw.loading ? -1 : raw.messages.length,
    };
    marks = [...marks, mark];
    const run = Promise.resolve(send(trimmed)).then(ok => {
        if (ok === false) marks = marks.filter(item => item !== mark);
        emit();
        return ok !== false;
    }, () => {
        marks = marks.filter(item => item !== mark);
        emit();
        return false;
    }).finally(() => {
        if (flight === run) flight = null;
    });
    flight = run;
    emit();
    return run;
}

export function remoteAssistantStop() {
    const stop = actions?.stop;
    if (!raw?.canSend || !stop) return Promise.resolve(false);
    const pendingSend = flight;
    return Promise.resolve(pendingSend ?? undefined).catch(() => undefined).then(() => {
        if (!raw?.canSend || actions?.stop !== stop) return false;
        return Promise.resolve(stop()).then(ok => ok !== false, () => false);
    });
}

export function remoteAssistantClose() {
    actions?.close();
}

export function remoteAssistantSessionSnapshot() {
    return current;
}

// --- Remote machine target (which computer a new task runs on) ---

/** A controlled machine offered as a new-task destination. */
export type RemoteMachineTarget = {
    grantId: string;
    machineName: string;
    online: boolean;
    /** operate permission — view-only grants cannot create tasks. */
    operate: boolean;
};

export type RemoteMachineTargetSnapshot = {
    machines: RemoteMachineTarget[];
    /** Grant of the machine tab currently selected in the task sidebar. */
    selectedGrantId: string;
};

let machineTarget: RemoteMachineTargetSnapshot = { machines: [], selectedGrantId: '' };
const machineTargetListeners = new Set<() => void>();

function sameMachineTarget(a: RemoteMachineTargetSnapshot, b: RemoteMachineTargetSnapshot) {
    if (a.selectedGrantId !== b.selectedGrantId || a.machines.length !== b.machines.length) return false;
    return a.machines.every((m, i) => {
        const o = b.machines[i];
        return o && m.grantId === o.grantId && m.machineName === o.machineName && m.online === o.online && m.operate === o.operate;
    });
}

/** Published by useTaskRemoteControl whenever grants or the selection change. */
export function publishRemoteMachineTarget(next: RemoteMachineTargetSnapshot) {
    if (sameMachineTarget(machineTarget, next)) return;
    machineTarget = next;
    machineTargetListeners.forEach(listener => listener());
}

/** Imperative read for send paths that run outside React. */
export function remoteMachineTargetSnapshot(): RemoteMachineTargetSnapshot {
    return machineTarget;
}

export function useRemoteMachineTarget(): RemoteMachineTargetSnapshot {
    return useSyncExternalStore(
        (listener: () => void) => {
            machineTargetListeners.add(listener);
            return () => machineTargetListeners.delete(listener);
        },
        () => machineTarget,
        () => machineTarget,
    );
}

// --- Remote task opener ---

let taskOpener: ((grantId: string, projectPath: string) => void) | null = null;

/** Bound by the component owning useTaskRemoteControl (TaskMachineBar). */
export function bindRemoteTaskOpener(fn: ((grantId: string, projectPath: string) => void) | null) {
    taskOpener = fn;
}

/** Opens a task on the named controlled machine; no-op when nothing is bound. */
export function remoteOpenTask(grantId: string, projectPath: string) {
    taskOpener?.(grantId, projectPath);
}
