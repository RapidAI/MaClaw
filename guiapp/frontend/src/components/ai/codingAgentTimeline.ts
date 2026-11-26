import {
    isCodingAgentChatHiddenEvent,
    isCodingAgentProgressContent,
    mergeCodingToolProgressContent,
    parseCodingAgentProgress,
} from "./CodingAgentProgressStatus";
import type { CodingAgentTimelineItem } from "./useAIAssistant";

export type { CodingAgentTimelineItem };

/**
 * Timeline building shared by the local coding transcript (useAIAssistant)
 * and the mirrored remote/cloud rounds (cloudAgentSession /
 * remoteAssistantSession). The folding rules live here once so a cloud
 * coding turn paints exactly the rows a local turn paints: one row per tool
 * call (tool_finished replaces its tool_started), progress merged into the
 * open row, and audit banners dropped.
 */

/** Local feed rows cap — mirrored by the remote trail builders. */
export const MAX_CODING_TIMELINE_ITEMS = 300;

export function normalizeCodingTimelineSequence(value?: number): number {
    return Number.isFinite(value) && (value || 0) > 0 ? Math.trunc(value || 0) : 0;
}

export function codingTimelineSequenceIsTaken(timeline: CodingAgentTimelineItem[], sequence: number): boolean {
    return timeline.some((item) =>
        (sequence >= item.sequence && sequence <= (item.lastSequence || item.sequence))
        || item.eventSequences?.includes(sequence),
    );
}

export function nextCodingTimelineSequence(timeline: CodingAgentTimelineItem[], preferred?: number): number {
    const normalizedPreferred = normalizeCodingTimelineSequence(preferred);
    if (normalizedPreferred > 0 && !codingTimelineSequenceIsTaken(timeline, normalizedPreferred)) {
        return normalizedPreferred;
    }
    return timeline.reduce((max, item) => Math.max(max, item.lastSequence || item.sequence), 0) + 1;
}

/** Row start. A later rewrite of `timestamp` must not move this. */
export function codingRowStartedAt(item: { startedAt?: number; timestamp?: number }, fallback: number): number {
    const started = item.startedAt || 0;
    if (started > 0) return started;
    const stamp = item.timestamp || 0;
    return stamp > 0 ? stamp : fallback;
}

/** Keep the transport order recoverable even when Wails delivers a batch late. */
export function insertCodingTimelineItem(timeline: CodingAgentTimelineItem[], item: CodingAgentTimelineItem, maxItems = MAX_CODING_TIMELINE_ITEMS): CodingAgentTimelineItem[] {
    const insertAt = timeline.findIndex((current) => current.sequence > item.sequence);
    const next = insertAt < 0
        ? [...timeline, item]
        : [...timeline.slice(0, insertAt), item, ...timeline.slice(insertAt)];
    return next.length > maxItems ? next.slice(next.length - maxItems) : next;
}

function codingToolLifecycleMatches(startContent: string, finishedContent: string): boolean {
    const started = parseCodingAgentProgress(startContent);
    const finished = parseCodingAgentProgress(finishedContent);
    if (!started || !finished || started.event !== 'tool_started' || finished.event !== 'tool_finished') return false;

    // A turn is normally sequential, but never merge different turns/runs.
    for (const field of ['turnID', 'runID', 'taskID'] as const) {
        if (started[field] && finished[field] && started[field] !== finished[field]) return false;
    }
    // Tool name is the primary lifecycle identity; titles are a safe fallback
    // for older events that did not include a structured tool detail.
    if (started.detail && finished.detail && started.detail !== finished.detail) return false;
    if (!started.detail || !finished.detail) {
        if (!started.title || started.title !== finished.title) return false;
    }

    // A model may call the same tool repeatedly in one turn. The exact command
    // (when emitted) distinguishes those lifecycles; without it the nearest
    // outstanding matching start is the only safe pairing heuristic.
    const startedCommand = (started.command || '').trim();
    const finishedCommand = (finished.command || '').trim();
    return !startedCommand || !finishedCommand || startedCommand === finishedCommand;
}

export function replaceMatchingCodingToolStart(
    timeline: CodingAgentTimelineItem[],
    finishedContent: string,
    finishedSequence: number,
    timestamp: number,
): CodingAgentTimelineItem[] | null {
    for (let index = timeline.length - 1; index >= 0; index--) {
        const item = timeline[index];
        if (item.kind !== 'progress' || item.sequence > finishedSequence) continue;
        if (!codingToolLifecycleMatches(item.content, finishedContent)) continue;
        const eventSequences = Array.from(new Set([item.sequence, ...(item.eventSequences || []), finishedSequence]));
        return timeline.map((current, currentIndex) => currentIndex === index
            ? { ...current, content: finishedContent, timestamp, startedAt: codingRowStartedAt(current, timestamp), eventSequences }
            : current);
    }
    return null;
}

export function mergeCodingToolProgress(
    timeline: CodingAgentTimelineItem[],
    progressContent: string,
    progressSequence: number,
    timestamp: number,
): CodingAgentTimelineItem[] | null {
    for (let index = timeline.length - 1; index >= 0; index--) {
        const item = timeline[index];
        if (item.kind !== 'progress') continue;
        const merged = mergeCodingToolProgressContent(item.content, progressContent);
        if (!merged) continue;
        const eventSequences = Array.from(new Set([item.sequence, ...(item.eventSequences || []), progressSequence]));
        return timeline.map((current, currentIndex) => currentIndex === index
            ? { ...current, content: merged, timestamp, startedAt: codingRowStartedAt(current, timestamp), eventSequences }
            : current);
    }
    return null;
}

export function replaceLateCodingToolStart(
    timeline: CodingAgentTimelineItem[],
    startedContent: string,
    startedSequence: number,
    timestamp: number,
): CodingAgentTimelineItem[] | null {
    for (let index = 0; index < timeline.length; index++) {
        const item = timeline[index];
        if (item.kind !== 'progress' || item.sequence < startedSequence) continue;
        if (!codingToolLifecycleMatches(startedContent, item.content)) continue;
        const eventSequences = Array.from(new Set([startedSequence, ...(item.eventSequences || []), item.sequence]));
        return timeline.map((current, currentIndex) => currentIndex === index
            ? { ...current, sequence: startedSequence, content: item.content, timestamp, startedAt: codingRowStartedAt(current, timestamp), eventSequences }
            : current);
    }
    return null;
}

/**
 * Fold one structured coding progress line into a trail (the rules
 * appendCodingProgressToRound applies to the local transcript, minus the
 * message/pending-thought plumbing). Non-coding lines return the trail
 * unchanged — styled "工具 ·" session rows belong to the toolCalls timeline,
 * not here. The returned array is the same reference when nothing changed,
 * so callers keep identity-stable snapshots.
 */
export function appendCodingProgressEvent(
    timeline: CodingAgentTimelineItem[],
    progressText: string,
    eventSequence: number | undefined,
    now: number,
): CodingAgentTimelineItem[] {
    if (!isCodingAgentProgressContent(progressText)) return timeline;
    const last = timeline[timeline.length - 1];
    // Ignore a duplicated progress delivery without collapsing distinct tool
    // calls that happen to share the same display label.
    const preferredSequence = normalizeCodingTimelineSequence(eventSequence);
    if (!preferredSequence && last?.kind === 'progress' && last.content === progressText) return timeline;
    const toolProgress = parseCodingAgentProgress(progressText);
    // Audit rollups and leaked reasoning-lane notes already have a home
    // (sidebar / thinking panel). Inserting them here left empty feed slots
    // and □-filled truncated dumps between real thoughts.
    if (toolProgress && isCodingAgentChatHiddenEvent(toolProgress)) return timeline;
    if (preferredSequence && codingTimelineSequenceIsTaken(timeline, preferredSequence)) return timeline;
    const sequence = nextCodingTimelineSequence(timeline, eventSequence);
    if (toolProgress?.event === 'tool_finished') {
        const replaced = replaceMatchingCodingToolStart(timeline, progressText, sequence, now);
        if (replaced) return replaced;
    }
    if (toolProgress?.event === 'tool_started') {
        const replaced = replaceLateCodingToolStart(timeline, progressText, sequence, now);
        if (replaced) return replaced;
    }
    if (toolProgress?.event === 'tool_progress') {
        const merged = mergeCodingToolProgress(timeline, progressText, sequence, now);
        if (merged) return merged;
        // No open row owns this catch-up. Drop the event.
        return timeline;
    }
    return insertCodingTimelineItem(timeline, {
        id: `cat-${sequence}-${now}`,
        sequence,
        kind: 'progress',
        content: progressText,
        timestamp: now,
        startedAt: now,
    });
}

let trailIdCounter = 0;

/**
 * Ordered thinking+tool trail for one mirrored round. The frame stream is
 * the only place the true interleave order survives — the reduced viewer
 * state keeps thinking blocks and progress rows in separate lists, and the
 * srv snapshot emits thinking before progress — so the session layers feed
 * frames here in arrival order and paint the result on the live bubble,
 * exactly like the local coding transcript does.
 */
export class CodingAgentTrailBuilder {
    private items: CodingAgentTimelineItem[] = [];
    private openThoughtId: string | null = null;

    /** Current trail. Same reference between changes so render caches hold. */
    snapshot(): CodingAgentTimelineItem[] {
        return this.items;
    }

    get length(): number {
        return this.items.length;
    }

    get hasOpenThought(): boolean {
        return this.openThoughtId !== null;
    }

    /** thinking state=started — a fresh block. An already-open tail only repeats. */
    thinkingStarted(now = Date.now()): void {
        if (this.openThoughtId) return;
        trailIdCounter += 1;
        const sequence = nextCodingTimelineSequence(this.items);
        const item: CodingAgentTimelineItem = {
            id: `cat-thought-${trailIdCounter}`,
            sequence,
            kind: 'thinking',
            content: '',
            timestamp: now,
            startedAt: now,
        };
        this.items = insertCodingTimelineItem(this.items, item);
        this.openThoughtId = item.id;
    }

    /** thinking state=delta — append to the open block, opening one if needed. */
    thinkingDelta(text: string, now = Date.now()): void {
        if (!text) return;
        if (!this.openThoughtId) {
            this.thinkingStarted(now);
        }
        const index = this.items.findIndex((item) => item.id === this.openThoughtId);
        if (index < 0) {
            this.openThoughtId = null;
            this.thinkingStarted(now);
            return;
        }
        const next = this.items.slice();
        const item = next[index];
        next[index] = { ...item, content: `${item.content}${text}`, timestamp: now, startedAt: codingRowStartedAt(item, now) };
        this.items = next;
    }

    /** thinking state=done — seal the open block, adopting the full text when carried. */
    thinkingDone(fullText?: string, now = Date.now()): void {
        const index = this.items.findIndex((item) => item.id === this.openThoughtId);
        if (index < 0) {
            // A done event without a started/delta trail (snapshot edge): keep
            // a non-empty block so the thought is not lost.
            const text = (fullText || '').trim();
            if (!text) return;
            trailIdCounter += 1;
            const sequence = nextCodingTimelineSequence(this.items);
            this.items = insertCodingTimelineItem(this.items, {
                id: `cat-thought-${trailIdCounter}`,
                sequence,
                kind: 'thinking',
                content: text,
                timestamp: now,
                startedAt: now,
            });
            return;
        }
        const next = this.items.slice();
        const item = next[index];
        const content = (fullText ?? '') !== '' ? fullText! : item.content;
        next[index] = { ...item, content, timestamp: now, startedAt: codingRowStartedAt(item, now) };
        this.items = next;
        this.openThoughtId = null;
    }

    /** Close an open thinking block. Timeline items carry no done flag — the
     * block just stops growing — so this must not touch the array: it runs
     * on every token frame and a per-token copy would churn the snapshot
     * identity the render caches rely on. */
    sealThinking(): void {
        this.openThoughtId = null;
    }

    /** One progress frame folded with the local coding-timeline rules. */
    progress(progressText: string, eventSequence?: number, now = Date.now()): void {
        // A token/progress after a thought closes it — reasoning precedes its answer.
        this.sealThinking();
        this.items = appendCodingProgressEvent(this.items, progressText, eventSequence, now);
    }

    /** Replace the whole trail from an ordered snapshot (watch/resync rebuild). */
    reset(): void {
        this.items = [];
        this.openThoughtId = null;
    }
}
