import { emptyViewerState, reduceViewerEvent, type ViewerReduceState } from '../ai/reduceViewerEvent';

const rounds = new Map<string, ViewerReduceState>();
const roundListeners = new Set<() => void>();

function notifyRounds() {
    roundListeners.forEach(listener => listener());
}

/** Both the main panel and the cloud page read this store. */
export function subscribeCloudAgentRounds(listener: () => void): () => void {
    roundListeners.add(listener);
    return () => { roundListeners.delete(listener); };
}

export function cloudAgentSessionKey(instanceID: string, taskID: string): string {
    const instance = instanceID.trim();
    const task = taskID.trim();
    if (!instance || !task) return '';
    return `cloud-agent:${instance}:${task}`;
}

/** Playback key for one cloud task. An empty project path is not part of it. */
export function cloudAgentStoreKey(grantID: string, sessionKey: string): string {
    const grant = grantID.trim();
    const session = sessionKey.trim();
    if (!grant || !session.startsWith('cloud-agent:')) return '';
    return `${grant}\n${session}`;
}

export function cloudAgentRound(key: string): ViewerReduceState {
    return rounds.get(key) || emptyViewerState();
}

export function resetCloudAgentRounds(): void {
    if (rounds.size === 0) return;
    rounds.clear();
    notifyRounds();
}

type CloudFrame = Record<string, unknown>;

function frameOf(payload: unknown): CloudFrame {
    const parsed = typeof payload === 'string'
        ? (() => { try { return JSON.parse(payload); } catch { return null; } })()
        : payload;
    return parsed && typeof parsed === 'object' ? parsed as CloudFrame : {};
}

function cloudDisplay(text: string): string {
    const raw = text.trim();
    if (!raw) return '〔云端〕';
    if (raw.startsWith('〔')) return raw;
    return `〔云端〕${raw}`;
}

function confirmTerminal(event: CloudFrame): CloudFrame {
    const raw = event.terminal;
    if (!raw || typeof raw !== 'object') return event;
    const terminal: CloudFrame = { ...(raw as CloudFrame) };
    const confirmation = terminal.confirmation && typeof terminal.confirmation === 'object'
        ? terminal.confirmation as CloudFrame
        : null;
    if (confirmation && String(confirmation.id || '').trim()) return { ...event, terminal };
    const question = String(terminal.ask_user_question || '').trim();
    const optionsRaw = String(terminal.ask_user_options_json || '');
    if (!question && !optionsRaw.trim()) return { ...event, terminal };
    let labels: string[] = [];
    try {
        const parsed = JSON.parse(optionsRaw);
        if (Array.isArray(parsed)) labels = parsed.map((item) => String(item || '').trim()).filter(Boolean);
    } catch {
        labels = [];
    }
    const actions = Array.isArray(terminal.actions) && terminal.actions.length > 0
        ? terminal.actions
        : labels.map((label) => ({ label, command: label }));
    return {
        ...event,
        terminal: {
            ...terminal,
            ask_user_input_type: String(terminal.ask_user_input_type || 'choice'),
            confirmation: { id: 'commit-approval' },
            actions,
        },
    };
}

/** Fold one remote_control.stream frame for a cloud task.
 * An empty project path stays on this grant and session key. */
export function reduceCloudAgentEvent(state: ViewerReduceState, event: CloudFrame): ViewerReduceState {
    const grant = String(event.grant_id || '').trim();
    const session = String(event.session_key || '').trim();
    if (!cloudAgentStoreKey(grant, session)) return state;
    const nextEvent: CloudFrame = confirmTerminal({ ...event, project_path: '' });
    if (typeof nextEvent.display_text === 'string') {
        nextEvent.display_text = cloudDisplay(nextEvent.display_text);
    }
    return reduceViewerEvent(state, nextEvent);
}

/** Fold a cloud_watch_task snapshot onto this grant and session.
 * The snapshot is the authority for this task, except for frames that
 * arrived after this watch was issued: a late snapshot must not wipe them.
 * Pass that issued cursor as baselineCursor. Omit it to always apply. */
export function applyCloudAgentWatch(grantID: string, sessionKey: string, snapshot: unknown, baselineCursor?: number): string {
    const key = cloudAgentStoreKey(grantID, sessionKey);
    if (!key) return '';
    const live = frameOf(snapshot);
    const cursor = Number(live.cursor);
    const shaped = typeof live.session_key === 'string' || Array.isArray(live.parts) || Number.isFinite(cursor);
    if (!shaped) {
        console.debug('[cloud-agent] watch snapshot dropped: unshaped payload', { sessionKey });
        return '';
    }
    const stored = rounds.get(key);
    const storedLast = stored?.last ?? 0;
    const floor = typeof baselineCursor === 'number' && Number.isFinite(baselineCursor) ? baselineCursor : storedLast;
    if (stored && storedLast > floor && (!Number.isFinite(cursor) || cursor < storedLast)) {
        console.debug('[cloud-agent] watch snapshot dropped: staler than live frames', { sessionKey, cursor, storedLast, floor });
        return key;
    }
    console.debug('[cloud-agent] watch snapshot apply', {
        sessionKey, cursor, responded: live.responded === true,
        parts: Array.isArray(live.parts) ? live.parts.length : 0,
        artifacts: Array.isArray(live.local_file_paths) ? live.local_file_paths.length : 0,
    });
    rounds.delete(key);
    if (!Number.isFinite(cursor) || cursor <= 0) {
        notifyRounds();
        return key;
    }
    const event: CloudFrame = {
        grant_id: grantID,
        session_key: sessionKey,
        project_path: '',
        event: 'snapshot',
        resync: true,
        cursor,
        live: { ...live, project_path: '' },
    };
    let round = reduceCloudAgentEvent(emptyViewerState(), event);
    const terminal = live.terminal;
    const hasCard = round.cards.some((card) => !card.expired && card.kind === 'confirmation');
    if (!hasCard && terminal && typeof terminal === 'object') {
        round = reduceCloudAgentEvent(round, {
            grant_id: grantID,
            session_key: sessionKey,
            project_path: '',
            event: 'response',
            terminal,
        });
    }
    rounds.set(key, round);
    notifyRounds();
    return key;
}

/** Store one stream payload. Returns the store key, or empty when the frame is not a cloud task.
 * A new request owns the round, so an ask from the previous request is closed. */
export function applyCloudAgentStream(payload: unknown): string {
    const event = frameOf(payload);
    const key = cloudAgentStoreKey(String(event.grant_id || ''), String(event.session_key || ''));
    if (!key) {
        console.debug('[cloud-agent] stream frame dropped: no grant/session key', {
            event: String(event.event || ''), grant: String(event.grant_id || ''),
            session: String(event.session_key || ''),
        });
        return '';
    }
    const previousId = cloudAgentRound(key).requestId;
    const next = reduceCloudAgentEvent(cloudAgentRound(key), event);
    if (next.requestId && previousId && next.requestId !== previousId) {
        for (const card of next.cards) {
            if (card.requestId !== next.requestId) card.expired = true;
        }
    }
    rounds.set(key, next);
    notifyRounds();
    return key;
}

/** The open ask belonged to a turn that has been sent or cancelled. */
export function expireCloudAgentCards(key: string): void {
    const round = rounds.get(key);
    if (!round) return;
    let changed = false;
    for (const card of round.cards) {
        if (card.expired) continue;
        card.expired = true;
        changed = true;
    }
    if (changed) notifyRounds();
}
