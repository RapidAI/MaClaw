import { parseCodingAgentProgress } from './CodingAgentProgressStatus';

export interface ViewerThinking {
    text: string;
    done: boolean;
}

export interface ViewerTimelineRow {
    phase: string;
    taskId: string;
    title: string;
    runId: string;
    text: string;
}

export interface ViewerCard {
    requestId: string;
    kind: string;
    expired: boolean;
    confirmationId: string;
    text: string;
    asError: boolean;
    actions?: Array<{ label: string; command: string }>;
}

export interface ViewerNotice {
    text: string;
    asError: boolean;
}

export interface ViewerReduceState {
    last: number;
    requestId: string;
    body: string;
    thinking: ViewerThinking[];
    timeline: ViewerTimelineRow[];
    cards: ViewerCard[];
    notices: ViewerNotice[];
    visible: string;
    trialStatus: string;
    trialFailures: number;
    gap: boolean;
    responded: boolean;
    displayText: string;
}

/** Progress rows kept in the reduced state — the remote mirror caps rendered
 * progress parts at 80, so this leaves comfortable headroom. */
const VIEWER_TIMELINE_KEPT = 160;

export function emptyViewerState(): ViewerReduceState {
    return {
        last: 0,
        requestId: '',
        body: '',
        thinking: [],
        timeline: [],
        cards: [],
        notices: [],
        visible: '',
        trialStatus: '',
        trialFailures: 0,
        gap: false,
        responded: false,
        displayText: '',
    };
}

type ViewerEvent = Record<string, unknown>;

function num(value: unknown): number {
    return typeof value === 'number' && Number.isFinite(value) ? value : 0;
}

function text(value: unknown): string {
    return typeof value === 'string' ? value : '';
}

function piecesOf(event: ViewerEvent): string[] {
    if (Array.isArray(event.pieces) && event.pieces.length > 0) {
        return event.pieces.map(piece => String(piece ?? ''));
    }
    const joined = text(event.text);
    return joined ? [joined] : [];
}

function applyPieces(body: string, pieces: string[]): string {
    let next = body;
    for (const piece of pieces) {
        if (piece === '\x02' || piece.startsWith('\x02')) {
            next = piece.slice(1);
            continue;
        }
        next += piece;
    }
    return next;
}

function sealThinking(state: ViewerReduceState): void {
    for (const block of state.thinking) block.done = true;
}

function appendThinking(state: ViewerReduceState, delta: string): void {
    const open = [...state.thinking].reverse().find(block => !block.done);
    if (open) open.text += delta;
    else state.thinking.push({ text: delta, done: false });
}

function rowFromCoding(coding: Record<string, unknown>, fallback: string): ViewerTimelineRow {
    return {
        phase: text(coding.phase),
        taskId: text(coding.task_id || coding.taskId),
        title: text(coding.title),
        runId: text(coding.run_id || coding.runId),
        text: fallback,
    };
}

function rowFromProgressText(raw: string): ViewerTimelineRow | null {
    const parsed = parseCodingAgentProgress(raw);
    if (!parsed) return null;
    return {
        phase: parsed.phase === 'unknown' ? '' : parsed.phase,
        taskId: parsed.taskID || '',
        title: parsed.title || '',
        runId: parsed.runID || '',
        text: raw,
    };
}

function eventActions(raw: unknown): Array<{ label: string; command: string }> | undefined {
    if (!Array.isArray(raw)) return undefined;
    const actions = raw.map(item => {
        const row = item && typeof item === 'object' ? item as Record<string, unknown> : {};
        return { label: text(row.label), command: text(row.command) };
    }).filter(item => item.command);
    return actions.length > 0 ? actions : undefined;
}

function shortCodingLine(row: ViewerTimelineRow): string {
    const phase = row.phase || 'running';
    const task = row.taskId ? ` ${row.taskId}` : '';
    const title = row.title ? ` - ${row.title}` : '';
    return `Coding Agent:${phase ? ` ${phase}` : ''}${task}${title}`.trim();
}

function applyPayload(state: ViewerReduceState, event: ViewerEvent): void {
    const name = text(event.event);
    const requestId = text(event.request_id);
    if (requestId && name === 'round_started' && requestId !== state.requestId) {
        state.requestId = requestId;
        state.body = '';
        state.thinking = [];
        state.responded = false;
        state.gap = false;
        state.displayText = text(event.display_text);
    } else if (requestId && !state.requestId) {
        state.requestId = requestId;
    }
    switch (name) {
        case 'round_started':
            state.displayText = text(event.display_text) || state.displayText;
            return;
        case 'token': {
            sealThinking(state);
            const pieces = piecesOf(event);
            if (event.replace === true) state.body = '';
            state.body = applyPieces(event.replace === true ? '' : state.body, pieces);
            return;
        }
        case 'thinking': {
            const mode = text(event.state) || 'delta';
            if (mode === 'started') {
                const tail = state.thinking[state.thinking.length - 1];
                if (!(tail && !tail.done)) state.thinking.push({ text: '', done: false });
                return;
            }
            if (mode === 'done') {
                const tail = [...state.thinking].reverse().find(block => !block.done);
                const doneText = text(event.text);
                if (tail) {
                    if (doneText) tail.text = doneText;
                    tail.done = true;
                } else {
                    state.thinking.push({ text: doneText, done: true });
                }
                return;
            }
            const deltas = Array.isArray(event.pieces) && event.pieces.length > 0
                ? event.pieces.map(piece => String(piece ?? ''))
                : (text(event.text) ? [text(event.text)] : []);
            for (const delta of deltas) if (delta) appendThinking(state, delta);
            return;
        }
        case 'progress': {
            const coding = event.coding && typeof event.coding === 'object' ? event.coding as Record<string, unknown> : null;
            const raw = text(event.text);
            if (coding) {
                state.timeline.push(rowFromCoding(coding, shortCodingLine(rowFromCoding(coding, raw))));
            } else {
                const row = raw ? rowFromProgressText(raw) : null;
                if (row) state.timeline.push(row);
            }
            // The trail is a tail view — bound it like the backend snapshot
            // cap so a long tool run cannot grow parts (or the per-event
            // parts rebuild in the remote mirror) without limit.
            if (state.timeline.length > VIEWER_TIMELINE_KEPT) {
                state.timeline.splice(0, state.timeline.length - VIEWER_TIMELINE_KEPT);
            }
            return;
        }
        case 'heartbeat':
        case 'new_round':
        case 'stream_done':
            if (name !== 'heartbeat') sealThinking(state);
            if (name === 'stream_done') state.responded = false;
            return;
        case 'workbench_steps':
            return;
        case 'card': {
            const kind = text(event.kind) || 'notice';
            const cardRequest = requestId;
            if (kind === 'notice' && /^credential-card-.+/.test(cardRequest)) {
                for (const card of state.cards) {
                    if (card.requestId === cardRequest) card.expired = true;
                }
                const note = text(event.text);
                if (note) state.notices.push({ text: note, asError: event.as_error === true });
                return;
            }
            if (kind === 'confirmation') {
                const confirmation = event.confirmation && typeof event.confirmation === 'object'
                    ? event.confirmation as Record<string, unknown>
                    : {};
                state.cards.push({
                    requestId: cardRequest,
                    kind,
                    expired: false,
                    confirmationId: text(confirmation.id),
                    text: text(event.text),
                    asError: false,
                    actions: eventActions(event.actions),
                });
                return;
            }
            const note = text(event.text);
            if (note) state.notices.push({ text: note, asError: event.as_error === true });
            return;
        }
        case 'response': {
            if (requestId && state.requestId && requestId !== state.requestId && !state.responded) return;
            sealThinking(state);
            state.responded = true;
            const terminal = event.terminal && typeof event.terminal === 'object'
                ? event.terminal as Record<string, unknown>
                : (event.response && typeof event.response === 'object' ? event.response as Record<string, unknown> : {});
            const body = text(terminal.text);
            const summary = text(terminal.trial_reflect_summary) || text(terminal.trace_summary);
            state.visible = body || summary;
            state.trialStatus = text(terminal.trial_reflect_status);
            state.trialFailures = num(terminal.trial_reflect_failures);
            if (!state.body && body) state.body = body;
            const confirmation = terminal.confirmation && typeof terminal.confirmation === 'object'
                ? terminal.confirmation as Record<string, unknown>
                : null;
            if (confirmation && text(confirmation.id)) {
                state.cards.push({
                    requestId,
                    kind: 'confirmation',
                    expired: false,
                    confirmationId: text(confirmation.id),
                    actions: eventActions(terminal.actions),
                    text: '',
                    asError: false,
                });
            }
            return;
        }
        case 'snapshot':
            return;
        default:
            return;
    }
}

function applyLiveParts(state: ViewerReduceState, parts: unknown[]): void {
    let body = '';
    for (const item of parts) {
        const part = item && typeof item === 'object' ? item as Record<string, unknown> : null;
        if (!part) continue;
        const kind = text(part.kind);
        if (kind === 'text') {
            body += text(part.text);
            continue;
        }
        if (kind === 'thinking') {
            state.thinking.push({ text: text(part.text), done: part.done === true });
            continue;
        }
        if (kind === 'progress') {
            const coding = part.coding && typeof part.coding === 'object' ? part.coding as Record<string, unknown> : null;
            if (coding) {
                const row = rowFromCoding(coding, text(part.text));
                if (!row.text) row.text = shortCodingLine(row);
                state.timeline.push(row);
                continue;
            }
            const raw = text(part.text);
            const row = raw ? rowFromProgressText(raw) : null;
            if (row) state.timeline.push(row);
        }
    }
    if (state.timeline.length > VIEWER_TIMELINE_KEPT) {
        state.timeline.splice(0, state.timeline.length - VIEWER_TIMELINE_KEPT);
    }
    state.body = body;
}

function replaceFromLive(state: ViewerReduceState, event: ViewerEvent, cursor: number): ViewerReduceState {
    const live = event.live && typeof event.live === 'object' ? event.live as Record<string, unknown> : null;
    const next = emptyViewerState();
    next.last = cursor;
    if (!live) {
        next.gap = false;
        return next;
    }
    next.requestId = text(live.request_id);
    next.displayText = text(live.display_text);
    next.responded = live.responded === true;
    // Snapshots cannot carry event-only state (cards, notices, trial echo).
    // For a resync of the same round it stays valid; a different requestId
    // means a new round owns the key and nothing is preserved.
    if (next.requestId && next.requestId === state.requestId) {
        next.visible = state.visible;
        next.cards = state.cards;
        next.notices = state.notices;
        next.trialStatus = state.trialStatus;
        next.trialFailures = state.trialFailures;
    }
    // Wire snapshots carry remoteLiveSnapshot.parts. live.text is the older
    // golden shape and applies only when parts is absent.
    if (Array.isArray(live.parts)) {
        applyLiveParts(next, live.parts);
        return next;
    }
    next.body = text(live.text);
    if (Array.isArray(live.thinking)) {
        next.thinking = live.thinking.map(item => {
            const block = item && typeof item === 'object' ? item as Record<string, unknown> : {};
            return { text: text(block.text), done: block.done === true };
        });
    }
    return next;
}

/** Pure fold of one viewer-contract event. Same rules on the desktop and in the golden vectors. */
export function reduceViewerEvent(state: ViewerReduceState, event: ViewerEvent): ViewerReduceState {
    const name = text(event.event);
    if (!name || name === 'mystery' || (name !== 'snapshot' && name !== 'round_started' && name !== 'token' && name !== 'thinking' && name !== 'progress' && name !== 'heartbeat' && name !== 'workbench_steps' && name !== 'new_round' && name !== 'stream_done' && name !== 'card' && name !== 'response' && !known(name))) {
        if (!known(name)) return state;
    }
    const cursor = num(event.cursor);
    const hasFrom = Object.prototype.hasOwnProperty.call(event, 'cursor_from');
    const from = hasFrom ? num(event.cursor_from) : cursor;
    if (cursor <= 0 && !hasFrom) {
        const next = cloneState(state);
        applyPayload(next, event);
        return next;
    }
    if (name === 'snapshot' && event.resync === true && cursor > state.last) {
        const live = event.live && typeof event.live === 'object' ? event.live : null;
        if (!live) {
            const next = cloneState(state);
            next.gap = true;
            return next;
        }
        return replaceFromLive(state, event, cursor);
    }
    if (cursor <= state.last) return state;
    if (from === state.last + 1) {
        const next = cloneState(state);
        applyPayload(next, event);
        next.last = cursor;
        next.gap = false;
        return next;
    }
    if (from > state.last + 1 && name === 'round_started' && text(event.request_id) && text(event.request_id) !== state.requestId) {
        const next = cloneState(state);
        applyPayload(next, event);
        next.last = cursor;
        next.gap = false;
        return next;
    }
    const next = cloneState(state);
    next.gap = true;
    return next;
}

function known(name: string): boolean {
    return name === 'snapshot' || name === 'round_started' || name === 'token' || name === 'thinking' || name === 'progress' || name === 'heartbeat' || name === 'workbench_steps' || name === 'new_round' || name === 'stream_done' || name === 'card' || name === 'response';
}

function cloneState(state: ViewerReduceState): ViewerReduceState {
    return {
        ...state,
        thinking: state.thinking.map(block => ({ ...block })),
        timeline: state.timeline.map(row => ({ ...row })),
        cards: state.cards.map(card => ({ ...card })),
        notices: state.notices.map(note => ({ ...note })),
    };
}

/** Legacy payloads the task panel already paints, marked so raw listeners do not drop them. */
export function legacyFromViewerEvent(event: ViewerEvent): { kind: 'token' | 'round' | 'new_round' | 'stream_done' | 'progress' | 'response' | 'card' | 'workbench'; payload: Record<string, unknown> } | null {
    const name = text(event.event);
    const base = {
        __viewerAdapted: true,
        request_id: event.request_id,
        session_key: event.session_key,
        sequence: num(event.cursor) || num(event.seq),
    };
    if (name === 'token') {
        const pieces = piecesOf(event);
        return { kind: 'token', payload: { ...base, text: pieces.join(''), deltas: pieces } };
    }
    if (name === 'thinking') {
        const mode = text(event.state) || 'delta';
        if (mode === 'done') return null;
        const delta = text(event.text);
        return { kind: 'token', payload: { ...base, text: `\x01${delta}`, deltas: [`\x01${delta}`] } };
    }
    if (name === 'round_started') {
        return { kind: 'round', payload: { ...base, display_text: event.display_text } };
    }
    if (name === 'new_round') return { kind: 'new_round', payload: base };
    if (name === 'stream_done') return { kind: 'stream_done', payload: base };
    if (name === 'heartbeat') {
        // Liveness only: the legacy progress handler resets the response
        // timeout on the __heartbeat__ marker and then hides the line.
        return { kind: 'progress', payload: { ...base, text: '__heartbeat__' } };
    }
    if (name === 'progress') {
        const coding = event.coding && typeof event.coding === 'object' ? event.coding as Record<string, unknown> : null;
        let shown = text(event.text);
        if (coding && !parseCodingAgentProgress(shown)) shown = shortCodingLine(rowFromCoding(coding, shown));
        return { kind: 'progress', payload: { ...base, text: shown } };
    }
    if (name === 'response') {
        const terminal = event.terminal && typeof event.terminal === 'object' ? event.terminal as Record<string, unknown> : {};
        const body = text(terminal.text);
        const summary = text(terminal.trial_reflect_summary) || text(terminal.trace_summary);
        const fields = Array.isArray(terminal.fields) ? terminal.fields.slice() : [];
        const status = text(terminal.trial_reflect_status);
        const failures = num(terminal.trial_reflect_failures);
        if (status) fields.push({ label: 'Recovery', value: status });
        if (failures > 0) fields.push({ label: 'Failures', value: String(failures) });
        return { kind: 'response', payload: { ...base, ...terminal, text: body || summary, ...(fields.length ? { fields } : {}) } };
    }
    if (name === 'card') {
        return {
            kind: 'card',
            payload: {
                ...base,
                kind: event.kind,
                text: event.text,
                as_error: event.as_error,
                confirmation: event.confirmation,
                actions: event.actions,
            },
        };
    }
    if (name === 'workbench_steps') {
        const workbench = event.workbench && typeof event.workbench === 'object' ? event.workbench as Record<string, unknown> : {};
        return { kind: 'workbench', payload: { ...base, ...workbench, project_path: event.project_path } };
    }
    return null;
}

const panelStates = new Map<string, ViewerReduceState>();

/** Test isolation. Production starts empty and seeds last on the first frame. */
export function resetViewerPanelStates(): void {
    panelStates.clear();
}

export function parseViewerEvent(payload: unknown): ViewerEvent | null {
    let event = payload;
    if (typeof payload === 'string') {
        const trimmed = payload.trim();
        if (!trimmed) return null;
        try {
            event = JSON.parse(trimmed);
        } catch {
            return null;
        }
    }
    if (!event || typeof event !== 'object') return null;
    return event as ViewerEvent;
}

/**
 * Local in-process frames are contiguous per session, but the panel may
 * subscribe after the cursor has already moved. The first frame seeds last
 * so that one cursor is not treated as a gap. A later hole still does not append.
 */
export function acceptViewerEventForPanel(event: ViewerEvent): boolean {
    const key = text(event.session_key) || 'desktop-user';
    let prev = panelStates.get(key) || emptyViewerState();
    const cursor = num(event.cursor);
    const hasFrom = Object.prototype.hasOwnProperty.call(event, 'cursor_from');
    if (prev.last === 0 && cursor > 1 && (!hasFrom || num(event.cursor_from) === cursor)) {
        prev = { ...prev, last: cursor - 1 };
    }
    const next = reduceViewerEvent(prev, event);
    if (next === prev) return false;
    panelStates.set(key, next);
    if (next.gap && next.last === prev.last && next.body === prev.body && next.visible === prev.visible && next.timeline.length === prev.timeline.length && next.cards.length === prev.cards.length) {
        return false;
    }
    return true;
}
