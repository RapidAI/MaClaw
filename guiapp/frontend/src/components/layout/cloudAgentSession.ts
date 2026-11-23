import { emptyViewerState, reduceViewerEvent, type ViewerReduceState } from '../ai/reduceViewerEvent';

const rounds = new Map<string, ViewerReduceState>();

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
    rounds.clear();
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

/** Fold a cloud_watch_task snapshot onto this grant and session. */
export function applyCloudAgentWatch(grantID: string, sessionKey: string, snapshot: unknown): string {
    const key = cloudAgentStoreKey(grantID, sessionKey);
    if (!key) return '';
    const live = frameOf(snapshot);
    const cursor = Number(live.cursor);
    const shaped = typeof live.session_key === 'string' || Array.isArray(live.parts) || Number.isFinite(cursor);
    if (!shaped || !Number.isFinite(cursor) || cursor <= 0) return '';
    const event: CloudFrame = {
        grant_id: grantID,
        session_key: sessionKey,
        project_path: '',
        event: 'snapshot',
        resync: true,
        cursor,
        live: { ...live, project_path: '' },
    };
    let round = reduceCloudAgentEvent(cloudAgentRound(key), event);
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
    return key;
}

/** Store one stream payload. Returns the store key, or empty when the frame is not a cloud task. */
export function applyCloudAgentStream(payload: unknown): string {
    const event = frameOf(payload);
    const key = cloudAgentStoreKey(String(event.grant_id || ''), String(event.session_key || ''));
    if (!key) return '';
    const next = reduceCloudAgentEvent(cloudAgentRound(key), event);
    rounds.set(key, next);
    return key;
}
