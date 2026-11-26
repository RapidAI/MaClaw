import { useSyncExternalStore } from 'react';
import { EventsOn } from '../../../wailsjs/runtime';
import { CodingAgentTrailBuilder, type CodingAgentTimelineItem } from '../ai/codingAgentTimeline';
import { emptyViewerState, reduceViewerEvent, type ViewerReduceState } from '../ai/reduceViewerEvent';
import {
    appendToolCallMarker,
    isProgressOnlyToolAction,
    parseAssistantToolStatus,
    type AssistantToolCall,
} from '../ai/assistantToolCall';

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
    /** User row parked in the cloud send queue; cleared when its turn starts. */
    queued?: boolean;
    /** Tool calls folded from styled progress lines, same as a local turn. */
    toolCalls?: AssistantToolCall[];
    /** Ordered thinking+tool trail of the mirrored coding round (frame order). */
    codingTimeline?: CodingAgentTimelineItem[];
    fields?: Array<{ label: string; value: string }>;
    confirmation?: { id: string; summary?: string };
    actions?: Array<{ label: string; command: string }>;
    cardExpired?: boolean;
};

/** One ordered piece of a mirrored round; identical to remoteStreamPart in guiapp. */
export type RemoteLivePart = {
    kind: 'text' | 'thinking' | 'progress';
    seq: number;
    text: string;
    done?: boolean;
    coding?: { phase?: string; task_id?: string; taskId?: string; title?: string; run_id?: string; runId?: string };
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

/** One row of the coding workbench checklist mirrored from the controlled desktop. */
export type RemoteWorkbenchStep = {
    index?: number;
    title?: string;
    status?: string;
    summary?: string;
    verify_cmd?: string;
    verify_ok?: boolean;
};

/** Session-level coding workbench state mirrored over remote_control.stream. */
export type RemoteWorkbenchState = {
    step_statuses?: RemoteWorkbenchStep[];
    execution_plan?: string;
    requirement_restatement?: string;
    pending_approval?: boolean;
    plan_dialogue?: boolean;
    steps_revision?: number;
};

/** One phase row of a cloud workflow (MaClawSrv/cloudagent V2 machine). */
export type CloudWorkflowPhase = {
    id?: string;
    name?: string;
    status?: string;
    needs_confirm?: boolean;
    expects_document?: boolean;
};

/** One field of a cloud workflow phase form (v2 PhaseInputField). */
export type CloudWorkflowFormField = {
    name: string;
    label?: string;
    type?: string;
    required?: boolean;
    description?: string;
    placeholder?: string;
    options?: { label: string; value: string }[];
    default?: unknown;
};

/** The gated phase's input schema (v2 PhaseInputSchema). */
export type CloudWorkflowFormSchema = {
    title?: string;
    description?: string;
    fields?: CloudWorkflowFormField[];
    variants?: { id: string; label: string; fields: CloudWorkflowFormField[] }[];
};

/**
 * Full workflow snapshot for a cloud task, serialized by the instance's V2
 * state machine and riding workflow_state frames + the watch snapshot.
 */
export type CloudWorkflowState = {
    id?: string;
    status?: string;
    type?: string;
    current_phase?: string;
    phases?: CloudWorkflowPhase[];
    project_path?: string;
    suspended?: boolean;
    awaiting_form?: boolean;
    form_schema?: CloudWorkflowFormSchema;
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
    /** Cloud agent task projected through this channel — the UI says 云端, not 对方电脑. */
    cloud?: boolean;
    /** Which maclawsrv runs this cloud task: private node address or the official service label. */
    serverLabel?: string;
    /** Coding-instance repository binding; absent for session instances. */
    cloudGit?: { connected: boolean; repo?: string };
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
    /** A cursor hole is open. The bubble stays; the status says output is catching up. */
    syncing?: boolean;
    /** Coding workbench checklist mirrored over workbench_steps events. */
    workbench?: RemoteWorkbenchState | null;
    /** V2 workflow state mirrored over workflow_state events (cloud tasks). */
    cloudWorkflow?: CloudWorkflowState | null;
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
// row: streamed text plus the thinking blocks. Reasoning rides live "delta"
// events into the open block so the 思考 box grows with the controlled
// desktop's, and the closing "done" event still carries the full text.

const liveRounds = new Map<string, RemoteLiveRound>();
const viewerStates = new Map<string, ViewerReduceState>();
// Ordered thinking+tool trails keyed like liveRounds. The reduced viewer
// state keeps thinking and progress rows in separate lists, so the frame
// stream is the only place the true interleave order survives — these
// builders fold frames in arrival order with the local coding-timeline
// rules (tool_finished replaces its tool_started), and the live row paints
// the result as the same process timeline a local coding turn keeps.
const trailBuilders = new Map<string, { requestId: string; builder: CodingAgentTrailBuilder }>();

function trailBuilderOf(key: string): { requestId: string; builder: CodingAgentTrailBuilder } {
    let trail = trailBuilders.get(key);
    if (!trail) {
        trail = { requestId: '', builder: new CodingAgentTrailBuilder() };
        trailBuilders.set(key, trail);
    }
    return trail;
}

function remoteLiveTrail(key: string): CodingAgentTimelineItem[] | null {
    const trail = trailBuilders.get(key);
    if (!trail || trail.builder.length === 0) return null;
    return trail.builder.snapshot();
}

/** Fold one stream frame into the key's trail. Returns the event name consumed. */
function feedRemoteTrailFrame(key: string, eventName: string, requestId: string, seq: number, text: string, state: string): void {
    if (!eventName || eventName === 'heartbeat' || eventName === 'workbench_steps' || eventName === 'card' || eventName === 'response' && !requestId) return;
    const trail = trailBuilderOf(key);
    if (eventName === 'round_started') {
        if (requestId && trail.requestId && trail.requestId !== requestId) {
            trail.builder.reset();
        }
        if (requestId) trail.requestId = requestId;
        return;
    }
    if (requestId && trail.requestId && requestId !== trail.requestId) return;
    if (requestId && !trail.requestId) trail.requestId = requestId;
    switch (eventName) {
        case 'thinking':
            if (state === 'done') trail.builder.thinkingDone(text);
            else if (state === 'started') trail.builder.thinkingStarted();
            else trail.builder.thinkingDelta(text);
            return;
        case 'progress':
            trail.builder.progress(text, seq > 0 ? seq : undefined);
            return;
        case 'token':
        case 'new_round':
        case 'stream_done':
        case 'response':
            trail.builder.sealThinking();
            return;
        default:
            return;
    }
}

/** Rebuild one trail from a watch snapshot's ordered parts (mid-turn join). */
function rebuildRemoteTrailFromSnapshot(key: string, requestId: string, parts: unknown): void {
    const trail = trailBuilderOf(key);
    trail.requestId = requestId || trail.requestId;
    trail.builder.reset();
    if (!Array.isArray(parts)) return;
    for (const part of parts) {
        if (!part || typeof part !== 'object') continue;
        const row = part as Record<string, unknown>;
        const kind = String(row.kind || '');
        const text = String(row.text || '');
        if (!text) continue;
        const seq = typeof row.seq === 'number' && row.seq > 0 ? row.seq : 0;
        if (kind === 'thinking') {
            if (row.done === true) trail.builder.thinkingDone(text);
            else trail.builder.thinkingDelta(text);
        } else if (kind === 'progress') {
            trail.builder.progress(text, seq);
        }
    }
}
// Highest cursor reduced while this key's hole was open, and one follow-up
// credit for the watch that rising edge started. A reply covers the hole
// only when its watermark reaches that cursor. Clearing the gap latch when
// the watermark merely moves (so the next hole can rise) would otherwise
// treat frames already dropped during that watch as covered.
const gapSeenCursor = new Map<string, number>();
const gapFollowCredit = new Map<string, number>();
// Progress parts kept per round — twice the backend snapshot cap so a watcher
// that joined mid-turn keeps a margin of older lines.
const REMOTE_PROGRESS_PARTS_KEPT = 80;
// Session-level workbench checklist, keyed like liveRounds. Outlives a round:
// the plan list belongs to the session, not to one request.
const workbenchByKey = new Map<string, RemoteWorkbenchState>();

function liveKey(grantId: string, projectPath: string) {
    return grantId && projectPath ? `${grantId}${projectPath}` : '';
}

/** External-origin user rows carry a 〔远控〕/〔…〕 prefix — strip it for dedupe and display. */
function stripRemoteOriginMarker(text: string) {
    return String(text || '').replace(/^〔[^〕]*〕/, '').trim();
}

/**
 * Styled tool-status progress parts (工具 · action (name)) ride the same wire
 * shape the local tray emits. Fold them into the live row's toolCalls so a
 * mirrored round renders the same tool timeline a local turn does; the
 * progress-only rows stay in parts for the progress renderer.
 */
export function remoteLiveToolCalls(parts: RemoteLivePart[], content: string): { content: string; toolCalls?: AssistantToolCall[] } {
    let next = content;
    const calls: AssistantToolCall[] = [];
    for (const part of parts) {
        if (part.kind !== 'progress' || !part.text) continue;
        const parsed = parseAssistantToolStatus(part.text);
        if (!parsed || isProgressOnlyToolAction(parsed.action)) continue;
        const tail = calls[calls.length - 1];
        if (tail && tail.name === parsed.name && tail.action === parsed.action && tail.detail === parsed.detail) continue;
        const id = `remote-tool-${calls.length}`;
        calls.push({ id, ...parsed });
        next = appendToolCallMarker(next, id);
    }
    return calls.length ? { content: next, toolCalls: calls } : { content: next };
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
    workbench?: RemoteWorkbenchState | null;
};

/** Folds one remote_control.stream event into the session's live round. */
function liveStamp(round: RemoteLiveRound | null | undefined): string {
    if (!round) return '';
    let weight = 0;
    for (const part of round.parts) weight += part.text.length + (part.done ? 1 : 0) + part.seq;
    const tail = round.parts[round.parts.length - 1];
    return [
        round.requestId,
        round.parts.length,
        weight,
        tail ? `${tail.kind}:${tail.done ? 1 : 0}` : '',
        round.responded ? 'r' : '',
        round.streamDone ? 'd' : '',
    ].join(':');
}

// body is the draft (a late joiner only has the snapshot's newest slice).
// visible is the terminal. Show the terminal when it is longer and the draft
// is its prefix or its suffix. A shorter terminal leaves the draft in place.
function shownAnswer(draft: string, terminal: string): string {
    if (!draft) return terminal;
    if (!terminal || terminal.length <= draft.length) return draft;
    if (terminal.startsWith(draft) || terminal.endsWith(draft)) return terminal;
    return draft;
}

function projectReducedRound(key: string, state: ViewerReduceState, eventName: string, workbench: RemoteWorkbenchState | null): void {
    if (eventName === 'workbench_steps') {
        if (workbench) workbenchByKey.set(key, workbench);
        else workbenchByKey.delete(key);
        emit();
        return;
    }
    if (!state.requestId) {
        // A heartbeat before round_started refreshes an existing round's
        // liveness only — it must not fabricate an empty one.
        if (eventName === 'heartbeat') {
            const round = liveRounds.get(key);
            if (round) {
                round.lastEventAt = Date.now();
                emit();
            }
        }
        return;
    }
    const round = ensureLiveRound(key, state.requestId);
    round.displayText = state.displayText;
    round.responded = state.responded;
    round.streamDone = state.responded;
    const parts: RemoteLivePart[] = [];
    state.thinking.forEach((block, index) => {
        parts.push({ kind: 'thinking', seq: index + 1, text: block.text, done: block.done });
    });
    const shown = shownAnswer(state.body, state.visible);
    if (shown) parts.push({ kind: 'text', seq: parts.length + 1, text: shown });
    for (const row of state.timeline) {
        const label = row.text || `Coding Agent: ${row.phase} ${row.taskId} - ${row.title}`.trim();
        parts.push({ kind: 'progress', seq: parts.length + 1, text: label });
    }
    round.parts = parts;
    round.lastEventAt = Date.now();
    emit();
}

export function applyRemoteStreamPayload(payload: unknown) {
    const parsed = typeof payload === 'string' ? (() => { try { return JSON.parse(payload); } catch { return null; } })() : payload;
    const p = (parsed && typeof parsed === 'object' ? parsed : {}) as RemoteStreamPayload;
    const key = liveKey(String(p.grant_id || '').trim(), String(p.project_path || '').trim());
    const eventName = String(p.event || '');
    if (!key || !eventName) return;
    const record = p as unknown as Record<string, unknown>;
    const cursor = typeof record.cursor === 'number' ? record.cursor : 0;
    if (cursor > 0 || Object.prototype.hasOwnProperty.call(record, 'cursor_from')) {
        const prev = viewerStates.get(key) || emptyViewerState();
        const next = reduceViewerEvent(prev, record);
        if (next === prev) return;
        viewerStates.set(key, next);
        // The trail folds accepted frames only — a gapped frame is missing
        // text and would leave a hole; the watch recovery reseeds instead.
        if (!next.gap) {
            feedRemoteTrailFrame(key, eventName, String(p.request_id || ''), cursor, String(p.text || ''), String(p.state || ''));
        }
        // A rising hole means this controller missed a cursor. The account
        // broker's resync frame has no live body, and a dropped WebSocket
        // frame never arrives. The turn is only on the controlled desktop.
        // Later frames of the same hole do not ask again; they only raise
        // the cursor the reply still has to cover.
        if (next.gap) {
            if (cursor > (gapSeenCursor.get(key) || 0)) gapSeenCursor.set(key, cursor);
            if (!prev.gap) {
                gapFollowCredit.set(key, 1);
                requestRemoteStreamRecovery(String(p.grant_id || '').trim(), String(p.project_path || '').trim());
            }
        }
        if (next.gap && next.last === prev.last && next.body === prev.body) {
            // Keep the text already on screen. The status still has to flip
            // to "catching up", or a hole looks like the other computer stopped.
            emit();
            return;
        }
        const workbench = record.workbench && typeof record.workbench === 'object' ? record.workbench as RemoteWorkbenchState : null;
        projectReducedRound(key, next, eventName, workbench);
        return;
    }
    const requestId = String(p.request_id || '');
    const seq = typeof p.seq === 'number' ? p.seq : 0;
    let round: RemoteLiveRound | undefined;
    feedRemoteTrailFrame(key, eventName, requestId, seq, String(p.text || ''), String(p.state || ''));
    switch (eventName) {
        case 'round_started': {
            round = ensureLiveRound(key, requestId);
            round.displayText = String(p.display_text || '');
            break;
        }
        case 'token': {
            round = ensureLiveRound(key, requestId);
            const pieces = Array.isArray((p as { pieces?: unknown }).pieces)
                ? ((p as { pieces?: unknown[] }).pieces || []).map(piece => String(piece ?? ''))
                : [];
            const text = pieces.length > 0 ? '' : String(p.text || '');
            if (pieces.length > 0) {
                for (const piece of pieces) {
                    if (piece === '\x02' || piece.startsWith('\x02')) {
                        while (round.parts.length && round.parts[round.parts.length - 1].kind === 'text') round.parts.pop();
                        const body = piece.slice(1);
                        if (body) round.parts.push({ kind: 'text', seq, text: body });
                    } else if (piece) {
                        const tail = round.parts[round.parts.length - 1];
                        if (tail && tail.kind === 'text') tail.text += piece;
                        else round.parts.push({ kind: 'text', seq, text: piece });
                    }
                }
                break;
            }
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
            } else if (p.state === 'delta') {
                // Live reasoning increment: append to the open block instead
                // of stacking a new part per delta.
                const text = String(p.text || '');
                if (text) {
                    let target = -1;
                    for (let i = round.parts.length - 1; i >= 0; i--) {
                        const part = round.parts[i];
                        if (part.kind === 'thinking' && !part.done) { target = i; break; }
                    }
                    if (target >= 0) round.parts[target].text += text;
                    else round.parts.push({ kind: 'thinking', seq, text, done: false });
                }
            } else {
                // 'started' — a fresh block. An already-open tail only repeats.
                const tail = round.parts[round.parts.length - 1];
                if (!(tail && tail.kind === 'thinking' && !tail.done)) {
                    round.parts.push({ kind: 'thinking', seq, text: '', done: false });
                }
            }
            break;
        }
        case 'heartbeat': {
            // Liveness only: refreshes the stale-round window without a part.
            round = ensureLiveRound(key, requestId);
            break;
        }
        case 'workbench_steps': {
            // Session-level checklist: no live round is created for it.
            const wb = p.workbench && typeof p.workbench === 'object' ? p.workbench : null;
            if (wb) workbenchByKey.set(key, wb);
            else workbenchByKey.delete(key);
            emit();
            return;
        }
        case 'progress': {
            round = ensureLiveRound(key, requestId);
            const text = String(p.text || '');
            if (text) {
                round.parts.push({ kind: 'progress', seq, text });
                // The trail is a tail view — bound it like the backend's
                // snapshot cap so a long tool run cannot grow parts (and the
                // sessionKey stringify over them) without limit.
                let progressCount = 0;
                for (const part of round.parts) if (part.kind === 'progress') progressCount++;
                const overflow = progressCount - REMOTE_PROGRESS_PARTS_KEPT;
                if (overflow > 0) {
                    let dropped = 0;
                    round.parts = round.parts.filter(part => part.kind !== 'progress' || ++dropped > overflow);
                }
            }
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
            // A terminal event for a different request than the live round is
            // a straggler: folding it would replace the newer round's parts.
            round = liveRounds.get(key);
            if (round && requestId && round.requestId && round.requestId !== requestId) return;
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
    cursor?: number;
};

/**
 * Seeds the proto-2 reduce state from a watch reply. Without this, a
 * controller joining mid-turn drops every envelope after the snapshot as a
 * cursor gap — the mirror stays frozen until a new round or a resync frame.
 * The seeded projection also mirrors what the snapshot installed, so the
 * first accepted frame does not blank the pre-watch content.
 */
function seedViewerState(key: string, live: RemoteWatchSnapshot | null, cursor: number): ViewerReduceState | null {
    if (!key) return null;
    const liveCursor = live && typeof live.cursor === 'number' && live.cursor > 0 ? live.cursor : 0;
    const at = liveCursor || (cursor > 0 ? cursor : 0);
    if (!live || !String(live.request_id || '')) {
        const prev = viewerStates.get(key) || emptyViewerState();
        if (at > prev.last) {
            // Drop the reducer latch when the watermark moves, so the next
            // dropped frame is a new hole. Cursors already seen above this
            // watermark are not covered; the watch reply asks once more.
            viewerStates.set(key, { ...prev, last: at, gap: false });
        }
        return null;
    }
    if (at <= 0) return null;
    // Same fold as a resync snapshot. A cursor the reducer already passed
    // comes back unchanged, so a late reply cannot rewind the round.
    const prev = viewerStates.get(key) || emptyViewerState();
    const next = reduceViewerEvent(prev, {
        event: 'snapshot',
        resync: true,
        cursor: at,
        live,
    });
    if (next !== prev) viewerStates.set(key, next);
    return next;
}

/** Installs the in-flight turn and workbench snapshot a watch_task reply carried (mid-turn catch-up). */
export function applyRemoteWatchSnapshot(grantId: string, projectPath: string, live: unknown, workbench?: unknown, cursor?: number) {
    const p = (live && typeof live === 'object' ? live : null) as RemoteWatchSnapshot | null;
    // The live snapshot's path is the backend's canonical spelling — the same
    // one stream envelopes carry — so it stays the key when present. The
    // watch path covers workbench-only replies where live is nil.
    const key = liveKey(grantId, String((p && p.project_path) || projectPath || '').trim());
    if (!key) return;
    if (workbench && typeof workbench === 'object') {
        workbenchByKey.set(key, workbench as RemoteWorkbenchState);
    }
    // Same superset rule as the cloud trail: frames beat the snapshot's
    // capped parts, so an existing same-request trail stays. Rebuild only
    // joins a round this controller has no trail for.
    const snapshotRequestId = String((p && p.request_id) || '');
    const storedTrailRequestId = trailBuilders.get(key)?.requestId || '';
    const hasStoredTrail = remoteLiveTrail(key) !== null;
    const sameTrailRequest = !snapshotRequestId || !storedTrailRequestId || storedTrailRequestId === snapshotRequestId;
    if (!(hasStoredTrail && sameTrailRequest)) {
        rebuildRemoteTrailFromSnapshot(key, snapshotRequestId, p ? p.parts : undefined);
    }
    if (!p || !String(p.request_id || '')) {
        const before = viewerStates.get(key)?.last || 0;
        seedViewerState(key, null, typeof cursor === 'number' ? cursor : 0);
        settleWatchedGap(key, grantId, projectPath, before);
        emit();
        return;
    }
    // The reply rides a different ws write than queued stream events, so an
    // older snapshot can land after newer ones. A cursorless or already-passed
    // snapshot of another request must not replace the round. A higher cursor
    // is the turn this controller missed; the reducer still refuses to rewind.
    const current = liveRounds.get(key);
    if (current && current.requestId && current.requestId !== String(p.request_id)) {
        const liveCursor = typeof p.cursor === 'number' && p.cursor > 0 ? p.cursor : 0;
        const at = liveCursor || (typeof cursor === 'number' && cursor > 0 ? cursor : 0);
        const last = viewerStates.get(key)?.last || 0;
        if (at <= last) {
            emit();
            return;
        }
    }
    const before = viewerStates.get(key)?.last || 0;
    const seeded = seedViewerState(key, p, typeof cursor === 'number' ? cursor : 0);
    settleWatchedGap(key, grantId, projectPath, before);
    if (seeded && seeded.last > 0) {
        projectReducedRound(key, seeded, 'snapshot', null);
        const round = liveRounds.get(key);
        if (round) {
            if (typeof p.started_at === 'number' && p.started_at > 0) round.startedAt = p.started_at;
            round.streamDone = seeded.responded || !!p.stream_done;
            if (raw && round.baseline < 0) round.baseline = raw.messages.length;
        }
        return;
    }
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

// get_task keeps the first 4000 runes and appends "…" (trimRemoteControlText).
// That prefix is not the answer the stream already showed.
const remoteHistoryTextCap = 4000;

function trimmedHistoryPrefix(history: string): string | null {
    const h = history.trim();
    if (!h.endsWith('…') || h.length < remoteHistoryTextCap + 1) return null;
    return h.slice(0, -1);
}

function historyIsCappedPrefix(history: string, live: string): boolean {
    const prefix = trimmedHistoryPrefix(history);
    const l = live.trim();
    return !!prefix && l.length > prefix.length && l.startsWith(prefix);
}

// The 4000-rune row is the opening. It already shows a shorter live copy.
// Anything else — the full stream, or a snapshot of the ending — is still
// only on this round. get_task will not grow that row past the cut.
function transcriptMissesRound(history: string, live: string): boolean {
    const prefix = trimmedHistoryPrefix(history);
    const l = live.trim();
    if (!prefix || !l) return false;
    return !prefix.startsWith(l);
}

// A shared run shorter than a clause is punctuation, not the same sentence.
const answerSliceOverlapMin = 16;

// Longest prefix of tail that is a suffix of prefix. The snapshot keeps the
// newest slice, so that run is where the opening and the ending meet.
function sharedEdge(prefix: string, tail: string): number {
    if (!prefix || !tail) return 0;
    const s = `${tail}\0${prefix}`;
    const pi = new Array<number>(s.length);
    pi[0] = 0;
    let k = 0;
    for (let i = 1; i < s.length; i++) {
        while (k > 0 && s.charCodeAt(i) !== s.charCodeAt(k)) k = pi[k - 1];
        if (s.charCodeAt(i) === s.charCodeAt(k)) k += 1;
        pi[i] = k;
    }
    const n = pi[s.length - 1] || 0;
    return n >= answerSliceOverlapMin ? n : 0;
}

function joinedAnswerSlices(prefix: string, tail: string): string {
    const overlap = sharedEdge(prefix, tail);
    if (overlap > 0) return prefix + tail.slice(overlap);
    return `${prefix}\n\n…\n\n${tail}`;
}

// The trimmed row belongs to this round when its prompt is the round's
// prompt, or when the row arrived after the round was first seen.
function roundOwnsTrimmedTail(messages: RemoteAssistantTranscriptMessage[], index: number, round: RemoteLiveRound): boolean {
    const prompt = stripRemoteOriginMarker(round.displayText);
    if (prompt) {
        for (let i = index - 1; i >= 0; i--) {
            const row = messages[i];
            if (row.role !== 'user') continue;
            return row.content.trim() === prompt;
        }
        return false;
    }
    return round.baseline >= 0 && index >= round.baseline;
}

// Settles the live round against the polled transcript. The response having
// landed plus a fresh non-user tail means the transcript now owns the turn.
// A quiet round stays while the polled row is only the opening and this
// round still holds the rest. The timeout drops a round the transcript
// already shows, and a draft the transcript never grows for.
function settleLiveRounds() {
    if (!raw) {
        liveRounds.clear();
        viewerStates.clear();
        workbenchByKey.clear();
        trailBuilders.clear();
        gapSeenCursor.clear();
        gapFollowCredit.clear();
        return;
    }
    const key = markKey(raw);
    for (const roundKey of [...liveRounds.keys()]) {
        if (roundKey !== key) {
            liveRounds.delete(roundKey);
            viewerStates.delete(roundKey);
            trailBuilders.delete(roundKey);
            forgetGapTracking(roundKey);
        }
    }
    for (const wbKey of [...workbenchByKey.keys()]) {
        if (wbKey !== key) workbenchByKey.delete(wbKey);
    }
    const round = liveRounds.get(key);
    if (!round) return;
    if (round.baseline < 0) round.baseline = raw.messages.length;
    const tail = raw.messages[raw.messages.length - 1];
    const roundText = (round.responseText || round.parts.filter(part => part.kind === 'text').map(part => part.text).join('')).trim();
    const tailText = tail && tail.role !== 'user' ? (tail.content || '').trim() : '';
    if (transcriptMissesRound(tailText, roundText)) return;
    const staleMs = round.responded ? 90_000 : 180_000;
    if (Date.now() - (round.lastEventAt || 0) > staleMs) {
        liveRounds.delete(key);
        viewerStates.delete(key);
        trailBuilders.delete(key);
        forgetGapTracking(key);
        return;
    }
    if (!round.responded) return;
    if (!tail || tail.role === 'user') return;
    // The transcript owns the turn once it grows past the round's baseline,
    // or when its tail already carries this answer — a response can land
    // between polls and baseline counts whatever was fetched, not what the
    // round saw, so the content check is the real arbiter.
    const covered = raw.messages.length > round.baseline
        || (roundText !== '' && tailText !== '' && tailText.startsWith(roundText.slice(0, Math.min(60, roundText.length))));
    if (covered) {
        liveRounds.delete(key);
        viewerStates.delete(key);
        trailBuilders.delete(key);
        forgetGapTracking(key);
    }
}

function forgetGapTracking(key: string) {
    gapSeenCursor.delete(key);
    gapFollowCredit.delete(key);
}

// A watch reply that moved the watermark but stopped short of a cursor this
// controller already reduced is not coverage: those frames were dropped and
// will not be replayed. Ask once. A reply that does not move the watermark
// does not ask, or a hollow read would spin. The credit is spent by that
// single follow-up; the next rising edge is the next ask.
function settleWatchedGap(key: string, grantId: string, projectPath: string, before: number) {
    const after = viewerStates.get(key)?.last || 0;
    const seen = gapSeenCursor.get(key) || 0;
    if (seen === 0 || after >= seen) {
        forgetGapTracking(key);
        return;
    }
    if ((gapFollowCredit.get(key) || 0) > 0 && after > before) {
        gapFollowCredit.set(key, 0);
        requestRemoteStreamRecovery(grantId, projectPath);
    }
}

let remoteStreamBound = false;
let streamRecovery: ((grantId: string, projectPath: string) => void) | null = null;

/** The open controller re-reads watch_task after one proto-2 cursor hole. A short reply asks once more. */
export function bindRemoteStreamRecovery(fn: ((grantId: string, projectPath: string) => void) | null) {
    streamRecovery = fn;
}

function requestRemoteStreamRecovery(grantId: string, projectPath: string) {
    if (!grantId || !projectPath || !streamRecovery) return;
    streamRecovery(grantId, projectPath);
}

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
        // One answer. The polled row is its opening. Drop that row and keep
        // its files on the live row. A shared run is stitched once. Otherwise
        // the opening, a gap, and the ending stay in that order.
        let carriedFiles: string[] | undefined;
        let shown = content;
        if (content.trim()) {
            for (let i = merged.length - 1; i >= 0; i--) {
                const row = merged[i];
                if (row.role !== 'assistant' || row.remoteLive) continue;
                const live = content.trim();
                const prefix = trimmedHistoryPrefix(row.content);
                if (historyIsCappedPrefix(row.content, live)) {
                    carriedFiles = row.localFilePaths;
                    merged.splice(i, 1);
                } else if (prefix && !prefix.startsWith(live) && !live.startsWith(prefix) && roundOwnsTrimmedTail(merged, i, round)) {
                    carriedFiles = row.localFilePaths;
                    shown = joinedAnswerSlices(prefix, live);
                    merged.splice(i, 1);
                }
                break;
            }
        }
        const reduced = viewerStates.get(key);
        const fields: Array<{ label: string; value: string }> = [];
        if (reduced?.trialStatus) fields.push({ label: 'Recovery', value: reduced.trialStatus });
        if (reduced && reduced.trialFailures > 0) fields.push({ label: 'Failures', value: String(reduced.trialFailures) });
        const folded = remoteLiveToolCalls(round.parts, shown);
        const trail = remoteLiveTrail(key) || undefined;
        if (folded.content || thinking || liveThinking || fields.length || folded.toolCalls || trail) {
            merged.push({
                role: 'assistant',
                content: folded.content,
                reasoning: thinking || undefined,
                remoteLive: true,
                liveThinking,
                localFilePaths: carriedFiles,
                fields: fields.length ? fields : undefined,
                toolCalls: folded.toolCalls,
                codingTimeline: trail,
            });
        }
        if (reduced) {
            for (const card of reduced.cards) {
                if (card.kind !== 'confirmation' && !card.text) continue;
                merged.push({
                    role: 'assistant',
                    content: card.text || '',
                    confirmation: card.confirmationId ? { id: card.confirmationId, summary: card.text } : undefined,
                    actions: card.expired ? undefined : card.actions,
                    cardExpired: card.expired,
                });
            }
            for (const note of reduced.notices) {
                merged.push({ role: note.asError ? 'error' : 'assistant', content: note.text });
            }
        }
        return { ...session, messages: merged, live: round, workbench: workbenchByKey.get(key) ?? session.workbench ?? null, syncing: !!reduced?.gap };
    }
    // A cloud session's workbench arrives on the published session itself —
    // workbenchByKey only mirrors controlled-desktop tasks.
    const workbench = workbenchByKey.get(key) ?? session.workbench ?? null;
    if (merged || workbench) return { ...session, messages: merged || session.messages, workbench };
    return session;
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
        session.cloud ? 'cloud' : '',
        session.machineName,
        session.taskName,
        session.serverLabel || '',
        session.error,
        session.activity ? JSON.stringify(session.activity) : '',
        liveStamp(session.live),
        session.syncing ? 'sync' : '',
        session.workbench ? JSON.stringify(session.workbench) : '',
        session.cloudWorkflow ? JSON.stringify(session.cloudWorkflow) : '',
        session.messages.map(message => `${message.role}\n${message.content}\n${message.reasoning || ''}\n${(message.localFilePaths || []).join(',')}\n${message.cardExpired ? 'expired' : ''}\n${message.confirmation?.id || ''}\n${message.queued ? 'queued' : ''}`).join('\n--\n'),
    ].join('\n');
}

let currentSessionKey = '';

function emit() {
    const next = withEchoes(raw);
    const nextSessionKey = sessionKey(next);
    if (nextSessionKey === currentSessionKey) return;
    current = next;
    currentSessionKey = nextSessionKey;
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
