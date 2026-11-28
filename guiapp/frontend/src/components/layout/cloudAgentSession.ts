import { CodingAgentTrailBuilder, type CodingAgentTimelineItem } from '../ai/codingAgentTimeline';
import { emptyViewerState, reduceViewerEvent, type ViewerReduceState } from '../ai/reduceViewerEvent';
import type { CloudWorkflowState, RemoteWorkbenchState } from './remoteAssistantSession';

const rounds = new Map<string, ViewerReduceState>();
// The workbench (plan checklist) is task state, not round state: it rides
// workbench_steps frames the round reducer deliberately ignores, and the
// watch snapshot's workbench field. Same shape the remote mirror keeps in
// remoteAssistantSession.workbenchByKey.
const workbenches = new Map<string, RemoteWorkbenchState>();
// The V2 workflow rides the same channel: workflow_state frames plus the
// watch snapshot's workflow field, keyed by task like the workbench.
const workflows = new Map<string, CloudWorkflowState>();
const roundListeners = new Set<() => void>();

function notifyRounds() {
    roundListeners.forEach(listener => listener());
}

// --- Ordered thinking+tool trail (frame-arrival order) ---
//
// The reduced viewer state keeps thinking blocks and progress rows in
// separate lists, and the srv watch snapshot emits thinking before progress,
// so neither preserves the true interleave order. Only the frame stream
// does. This journal folds frames per store key in arrival order with the
// same rules the local coding transcript applies (tool_finished replaces its
// tool_started; audit banners drop), so the live bubble paints one
// interleaved trail exactly like a local coding turn.

type CloudAgentTrail = {
    requestId: string;
    builder: CodingAgentTrailBuilder;
};

const trails = new Map<string, CloudAgentTrail>();

function trailOf(key: string): CloudAgentTrail {
    let trail = trails.get(key);
    if (trail) {
        // LRU touch: Map iteration is insertion-ordered, so re-inserting
        // moves this key to the back and keeps the open task's trail alive.
        trails.delete(key);
        trails.set(key, trail);
        return trail;
    }
    trail = { requestId: '', builder: new CodingAgentTrailBuilder() };
    trails.set(key, trail);
    // Soft cap: a long session opening many tasks must not accumulate a
    // trail per task forever — evict the oldest untouched key first.
    while (trails.size > 32) {
        const oldest = trails.keys().next().value;
        if (oldest === undefined) break;
        trails.delete(oldest);
    }
    return trail;
}

function feedTrailFrame(key: string, event: CloudFrame): void {
    const name = String(event.event || '');
    const requestId = String(event.request_id || '');
    if (!name || name === 'heartbeat' || name === 'workbench_steps' || name === 'workflow_state' || name === 'snapshot') return;
    const trail = trailOf(key);
    if (name === 'round_started') {
        if (requestId && trail.requestId && trail.requestId !== requestId) {
            // A fresh round owns the key; the previous trail is that round's
            // history (kept for the settled-row attachment until this point).
            trail.builder.reset();
        }
        if (requestId) trail.requestId = requestId;
        return;
    }
    if (requestId && trail.requestId && requestId !== trail.requestId) return;
    if (requestId && !trail.requestId) trail.requestId = requestId;
    const seq = typeof event.cursor === 'number' && event.cursor > 0
        ? event.cursor
        : (typeof event.seq === 'number' ? event.seq : undefined);
    switch (name) {
        case 'thinking': {
            const state = String(event.state || 'delta');
            const text = String(event.text || '');
            if (state === 'done') trail.builder.thinkingDone(text);
            else if (state === 'started') trail.builder.thinkingStarted();
            else trail.builder.thinkingDelta(text);
            return;
        }
        case 'progress':
            trail.builder.progress(String(event.text || ''), seq);
            return;
        case 'token':
        case 'new_round':
        case 'stream_done':
        case 'response':
            // A token/terminal after a thought closes it — reasoning precedes
            // its answer, the same seal the desktop relay applies.
            trail.builder.sealThinking();
            return;
        default:
            return;
    }
}

/** Rebuild one trail from a watch snapshot. Snapshot parts carry the same
 * order the frames arrived in, so a mid-turn join seeds the interleaved
 * trail instead of waiting for the next frame. */
function rebuildTrailFromSnapshot(key: string, requestId: string, parts: unknown): void {
    const trail = trailOf(key);
    trail.requestId = requestId || trail.requestId;
    trail.builder.reset();
    if (!Array.isArray(parts)) return;
    for (const part of parts) {
        if (!part || typeof part !== 'object') continue;
        const row = part as Record<string, unknown>;
        const kind = String(row.kind || '');
        const text = String(row.text || '');
        if (!text) continue;
        const seq = typeof row.seq === 'number' && row.seq > 0 ? row.seq : undefined;
        if (kind === 'thinking') {
            if (row.done === true) trail.builder.thinkingDone(text);
            else {
                trail.builder.thinkingDelta(text);
            }
        } else if (kind === 'progress') {
            trail.builder.progress(text, seq);
        }
    }
}

/** The ordered thinking+tool trail folded for this store key, or null. */
export function cloudAgentTrail(key: string): CodingAgentTimelineItem[] | null {
    const trail = trails.get(key);
    if (!trail || trail.builder.length === 0) return null;
    return trail.builder.snapshot();
}

/**
 * Persisted cloud_get_task events, in seq order. Thinking rows are already
 * sealed. An empty list stays undefined so a caller can fall back.
 */
export function codingTimelineFromTurnEvents(events: Array<{ kind?: string; text?: string; seq?: number }> | null | undefined): CodingAgentTimelineItem[] | undefined {
    if (!events?.length) return undefined;
    const builder = new CodingAgentTrailBuilder();
    for (const event of events) {
        const text = String(event?.text || '');
        if (!text) continue;
        const kind = String(event?.kind || '');
        const seq = typeof event?.seq === 'number' && event.seq > 0 ? event.seq : undefined;
        if (kind === 'thinking') builder.thinkingDone(text);
        else if (kind === 'progress') builder.progress(text, seq);
    }
    const snapshot = builder.snapshot();
    return snapshot.length ? snapshot : undefined;
}

/** Request id the stored trail belongs to ('' before the first round frame). */
export function cloudAgentTrailRequestId(key: string): string {
    return trails.get(key)?.requestId || '';
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

/** Latest workbench payload for one cloud task key; null when none is live. */
export function cloudAgentWorkbench(key: string): RemoteWorkbenchState | null {
    return workbenches.get(key) || null;
}

function noteCloudAgentWorkbench(key: string, workbench: unknown) {
    if (workbench && typeof workbench === 'object') {
        workbenches.set(key, workbench as RemoteWorkbenchState);
    } else {
        workbenches.delete(key);
    }
}

/** Latest workflow snapshot for one cloud task key; null when none is live. */
export function cloudAgentWorkflow(key: string): CloudWorkflowState | null {
    return workflows.get(key) || null;
}

function noteCloudAgentWorkflow(key: string, workflow: unknown) {
    if (workflow && typeof workflow === 'object') {
        workflows.set(key, workflow as CloudWorkflowState);
    } else {
        workflows.delete(key);
    }
}

export function resetCloudAgentRounds(): void {
    if (rounds.size === 0 && workbenches.size === 0 && workflows.size === 0 && trails.size === 0) return;
    rounds.clear();
    workbenches.clear();
    workflows.clear();
    trails.clear();
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
    // The frame-built trail is a superset of the snapshot's (the snapshot
    // keeps only the last 40 progress rows and splits thinking from
    // progress), so an existing trail for the same request stays. Rebuild
    // only joins a round this controller has no trail for (mid-turn watch)
    // or one whose request moved on.
    const storedTrailRequestId = cloudAgentTrailRequestId(key);
    const snapshotRequestId = String(live.request_id || '');
    const hasStoredTrail = cloudAgentTrail(key) !== null;
    const sameRequest = !snapshotRequestId || !storedTrailRequestId || storedTrailRequestId === snapshotRequestId;
    if (!(hasStoredTrail && sameRequest)) {
        rebuildTrailFromSnapshot(key, snapshotRequestId, live.parts);
    }
    // Task-level workbench rides the snapshot. A snapshot rejected above as
    // stale keeps the live frames' workbench instead.
    noteCloudAgentWorkbench(key, live.workbench);
    noteCloudAgentWorkflow(key, live.workflow);
    // Rebuilding the same cursor is not a resync: every round-frame change
    // allocates a fresh cursor, while rebuilding here would erase local
    // expiration state (for example a confirmation card closed by the user's
    // answer) and then put the stale card back. Task-level state above still
    // refreshes because it is not cursor-owned.
    if (stored && Number.isFinite(cursor) && cursor > 0 && cursor === storedLast && cursor === floor) {
        notifyRounds();
        return key;
    }
    console.debug('[cloud-agent] watch snapshot apply', {
        sessionKey, cursor, responded: live.responded === true,
        parts: Array.isArray(live.parts) ? live.parts.length : 0,
        artifacts: Array.isArray(live.local_file_paths) ? live.local_file_paths.length : 0,
    });
    rounds.delete(key);
    if (!Number.isFinite(cursor) || cursor <= 0) {
        // A srv restart drops the in-memory lane cursor, but the snapshot can
        // still carry a durable gate card rebuilt from task state. That card
        // is not round history — render it even when there is no cursor to
        // rebuild, or the reopened panel has no way to answer the plan gate.
        const terminal = live.terminal;
        if (terminal && typeof terminal === 'object') {
            const round = reduceCloudAgentEvent(emptyViewerState(), {
                grant_id: grantID,
                session_key: sessionKey,
                project_path: '',
                event: 'response',
                terminal,
            });
            rounds.set(key, round);
        }
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
    feedTrailFrame(key, event);
    if (String(event.event || '') === 'workbench_steps') {
        // The reducer ignores this event by design; the task-level payload
        // lives beside the round instead of inside it.
        noteCloudAgentWorkbench(key, event.workbench);
    }
    if (String(event.event || '') === 'workflow_state') {
        // Same deal as workbench_steps — the workflow snapshot is task
        // state, published with an empty request id, never part of a round.
        noteCloudAgentWorkflow(key, event.workflow);
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
