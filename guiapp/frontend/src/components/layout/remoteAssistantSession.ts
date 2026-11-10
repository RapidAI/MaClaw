import { useSyncExternalStore } from 'react';

export type RemoteAssistantTranscriptMessage = {
    role: string;
    content: string;
    reasoning?: string;
};

export type RemoteAssistantSession = {
    grantId: string;
    machineName: string;
    projectPath: string;
    taskName: string;
    canSend: boolean;
    offline: boolean;
    error: string;
    messages: RemoteAssistantTranscriptMessage[];
};

type RemoteAssistantActions = {
    send: (text: string) => Promise<boolean | void>;
    stop: () => Promise<boolean | void>;
    close: () => void;
};

let raw: RemoteAssistantSession | null = null;
let pending = '';
let pendingSeen = 0;
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

function withPending(session: RemoteAssistantSession | null): RemoteAssistantSession | null {
    if (!session || !pending) return session;
    if (userTextCount(session.messages, pending) > pendingSeen) return session;
    return {
        ...session,
        messages: [...session.messages, { role: 'user', content: pending }],
    };
}

function sessionKey(session: RemoteAssistantSession | null) {
    if (!session) return '';
    return [
        session.grantId,
        session.projectPath,
        session.canSend ? '1' : '0',
        session.offline ? '1' : '0',
        session.taskName,
        session.error,
        session.messages.map(message => `${message.role}\n${message.content}\n${message.reasoning || ''}`).join('\n--\n'),
    ].join('\n');
}

function emit() {
    const next = withPending(raw);
    if (sessionKey(next) === sessionKey(current)) return;
    current = next;
    listeners.forEach(listener => listener());
}

export function publishRemoteAssistantSession(next: RemoteAssistantSession | null) {
    if (next && pending && userTextCount(next.messages, pending) > pendingSeen) pending = '';
    if (!next) {
        pending = '';
        pendingSeen = 0;
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
    pending = trimmed;
    pendingSeen = userTextCount(raw.messages, trimmed);
    const run = Promise.resolve(send(trimmed)).then(ok => {
        if (ok === false) pending = '';
        emit();
        return ok !== false;
    }, () => {
        pending = '';
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
