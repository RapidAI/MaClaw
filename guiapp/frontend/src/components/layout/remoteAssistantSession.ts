import { useSyncExternalStore } from 'react';

export type RemoteAssistantTranscriptMessage = {
    role: string;
    content: string;
    reasoning?: string;
};

/** Compact workflow snapshot mirrored from the remote task row. */
export type RemoteAssistantWorkflow = {
    type?: string;
    phase?: string;
    status?: string;
    pending_review?: boolean;
};

export type RemoteAssistantArtifact = {
    title: string;
    preview?: string;
    sourceType?: string;
    updatedAt?: string;
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
    /** Tail of the newest assistant reasoning, for the expandable body. */
    reasoningTail?: string;
    /** Recent artifacts the remote task produced. */
    artifacts?: RemoteAssistantArtifact[];
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

function withEchoes(session: RemoteAssistantSession | null): RemoteAssistantSession | null {
    if (!session || marks.length === 0) return session;
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
        session.messages.map(message => `${message.role}\n${message.content}\n${message.reasoning || ''}`).join('\n--\n'),
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
