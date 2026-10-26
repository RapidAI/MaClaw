import type { ChatMessage } from "./useAIAssistant";

const resolvedUnfinishedSlotStatuses = new Set(["resumed", "completed", "dismissed"]);

/** A recovery card still waiting for resume or dismiss. Resolved slots stay in history. */
export function messageHasPendingUnfinishedSlot(message: ChatMessage): boolean {
    const slot = message.unfinishedSlot;
    if (!slot || (slot.actions?.length ?? 0) === 0) return false;
    const status = String(slot.status || "").trim().toLowerCase();
    return !resolvedUnfinishedSlotStatuses.has(status);
}

/** Slot id for a card that still needs a choice. Blank ids are not collapsed together. */
export function pendingUnfinishedSlotID(message: ChatMessage): string {
    if (!messageHasPendingUnfinishedSlot(message)) return "";
    return String(message.unfinishedSlot?.slotID || "").trim();
}

/** The latest assistant turn failed. A later prose or completed turn clears it. */
export function latestAssistantTurnIncomplete(messages: ChatMessage[]): boolean {
    for (let i = messages.length - 1; i >= 0; i--) {
        if (messages[i].role !== "assistant") continue;
        return messages[i].resultStatus === "incomplete";
    }
    return false;
}

/** Latest pending recovery status, or "" when every card is resolved. */
export function latestPendingUnfinishedStatus(messages: ChatMessage[]): string {
    let status = "";
    for (const message of messages) {
        if (!messageHasPendingUnfinishedSlot(message)) continue;
        status = String(message.unfinishedSlot?.status || "").trim().toLowerCase() || "unfinished";
    }
    return status;
}

function pendingSlotKeepRank(message: ChatMessage): number {
    if ((message.reasoning || "").trim() || (message.codingTimeline?.length ?? 0) > 0 || message.fields?.length || message.confirmation || message.localFilePath || (message.localFilePaths?.length ?? 0) > 0 || message.thumbnailBase64 || message.imageKey) {
        return 4;
    }
    const content = (message.content || "").trim();
    if (!content) return 0;
    if ((message.id || "").startsWith("startup-recovery-")) return 1;
    if (content.length <= 180 && /^(检测到未完成任务|偵測到未完成任務|Detected an unfinished task:)/.test(content)) return 2;
    return 3;
}

function keepPendingSlotIndex(messages: ChatMessage[], indexes: number[]): number {
    return indexes.reduce((best, index) => {
        const rank = pendingSlotKeepRank(messages[index]);
        const bestRank = pendingSlotKeepRank(messages[best]);
        if (rank !== bestRank) return rank > bestRank ? index : best;
        const leftTime = messages[index].timestamp || 0;
        const rightTime = messages[best].timestamp || 0;
        if (leftTime !== rightTime) return leftTime > rightTime ? index : best;
        return (messages[index].id || "") > (messages[best].id || "") ? index : best;
    });
}

/**
 * Startup recovery and the next user turn can each project the same slot.
 * Drop the extra notice and keep a reply that still has its own reasoning.
 */
function recoverySlotGroupID(message: ChatMessage): string {
    return String(message.unfinishedSlot?.slotID || "").trim();
}

export function collapseDuplicatePendingUnfinishedSlots(messages: ChatMessage[]): ChatMessage[] {
    const indexesBySlot = new Map<string, number[]>();
    messages.forEach((message, index) => {
        const slotID = recoverySlotGroupID(message);
        if (!slotID) return;
        const indexes = indexesBySlot.get(slotID);
        if (indexes) indexes.push(index);
        else indexesBySlot.set(slotID, [index]);
    });
    const drop = new Set<number>();
    for (const indexes of indexesBySlot.values()) {
        if (indexes.length < 2) continue;
        const keep = keepPendingSlotIndex(messages, indexes);
        for (const index of indexes) {
            if (index !== keep) drop.add(index);
        }
    }
    if (drop.size === 0) return messages;
    return messages.filter((_, index) => !drop.has(index));
}

export const PROJECT_TAB_MSG_IDS_KEY = 'ai-assistant-project-tab-msg-ids';

export function loadProjectTabMsgIds(): Set<string> {
    try {
        const raw = localStorage.getItem(PROJECT_TAB_MSG_IDS_KEY);
        if (raw) {
            const arr = JSON.parse(raw);
            if (Array.isArray(arr)) return new Set(arr as string[]);
        }
    } catch { /* ignore */ }
    return new Set<string>();
}

export function mergeChatMessages(...groups: Array<unknown[] | undefined>): ChatMessage[] {
    const merged: ChatMessage[] = [];
    const indexById = new Map<string, number>();
    const insertAt = (message: ChatMessage, index: number, id: string) => {
        merged.splice(index, 0, message);
        for (const [knownId, knownIndex] of indexById) {
            if (knownIndex >= index) indexById.set(knownId, knownIndex + 1);
        }
        if (id) indexById.set(id, index);
    };
    for (const group of groups) {
        if (!Array.isArray(group)) continue;
        // A new id belongs after the previous row of this same list. Appending
        // it past rows the list still has later would pull a steer out of its
        // round and onto the tail of the next turn.
        let anchor = -1;
        for (const message of group) {
            if (!message || typeof message !== "object") continue;
            const chatMessage = message as ChatMessage;
            const id = typeof chatMessage.id === "string" ? chatMessage.id : "";
            if (id) {
                const existingIndex = indexById.get(id);
                if (existingIndex !== undefined) {
                    merged[existingIndex] = chatMessage;
                    anchor = existingIndex;
                    continue;
                }
            }
            const index = anchor >= 0 ? anchor + 1 : merged.length;
            if (index >= merged.length) {
                if (id) indexById.set(id, merged.length);
                merged.push(chatMessage);
                anchor = merged.length - 1;
                continue;
            }
            insertAt(chatMessage, index, id);
            anchor = index;
        }
    }
    return merged;
}

type CodingTrail = NonNullable<ChatMessage["codingTimeline"]>;
type CodingTrailItem = CodingTrail[number];

/** Inclusive transport span. Sequence 0 is unset and must not overlap every row. */
function positiveSpan(item: CodingTrailItem): [number, number] | null {
    const start = item.sequence > 0 ? item.sequence : 0;
    if (start <= 0) return null;
    const last = item.lastSequence && item.lastSequence > start ? item.lastSequence : start;
    return [start, last];
}

function eventHitsSpan(events: number[] | undefined, span: [number, number] | null): boolean {
    if (!events || !span) return false;
    const [start, end] = span;
    for (const sequence of events) {
        if (sequence >= start && sequence <= end) return true;
    }
    return false;
}

function eventsIntersect(left: number[] | undefined, right: number[] | undefined): boolean {
    if (!left || !right) return false;
    for (const sequence of left) {
        if (sequence <= 0) continue;
        for (const other of right) {
            if (sequence === other) return true;
        }
    }
    return false;
}

/**
 * A finished tool keeps its start sequence and records the finish beside it.
 * Those two rows are one event. The same id is one row even when only the
 * later copy has a transport sequence: the list key is that id, so keeping
 * both paints the stale copy and drops the edit.
 */
function timelineItemsOverlap(earlier: CodingTrailItem, later: CodingTrailItem): boolean {
    if (!!earlier.id && earlier.id === later.id) return true;
    const left = positiveSpan(earlier);
    const right = positiveSpan(later);
    if (left && right && left[0] <= right[1] && right[0] <= left[1]) return true;
    if (eventHitsSpan(later.eventSequences, left) || eventHitsSpan(earlier.eventSequences, right)) return true;
    if (eventsIntersect(earlier.eventSequences, later.eventSequences)) return true;
    return false;
}

/**
 * Groups are older snapshot, then live message. The live copy replaces rows
 * it overlaps, including the same trail id. A row it never reached stays.
 * When nothing earlier survives, the live array is reused so an append-only
 * token does not allocate.
 */
function unionCodingTrails(earlier: CodingTrail, later: CodingTrail): CodingTrail {
    const kept: CodingTrailItem[] = [];
    for (const item of earlier) {
        let overlaps = false;
        for (const next of later) {
            if (!timelineItemsOverlap(item, next)) continue;
            overlaps = true;
            break;
        }
        if (!overlaps) kept.push(item);
    }
    if (kept.length === 0) return later;
    const combined = kept.concat(later);
    combined.sort((left, right) => (left.sequence || 0) - (right.sequence || 0));
    return combined;
}

function absorbCodingTrail(trails: Map<string, CodingTrail>, message: ChatMessage) {
    const trail = message.codingTimeline;
    if (!message.id || !trail?.length) return;
    const previous = trails.get(message.id);
    if (!previous) {
        trails.set(message.id, trail);
        return;
    }
    if (previous === trail) return;
    trails.set(message.id, unionCodingTrails(previous, trail));
}

/**
 * Finish a project round into one transcript.
 * A later row replaces the same id. Coding trails union by event span:
 * a later item replaces any earlier item it overlaps, and an earlier item
 * the later copy never reached stays. An error row clears that trail on purpose.
 */
export function mergeProjectRoundHistory(
    visible: ChatMessage[] | undefined,
    saved: ChatMessage[] | undefined,
    fresh: ChatMessage[] | undefined,
    held?: ChatMessage[] | undefined,
    // While this session is still sending, the live message is the authority.
    // A copy that left the coding trail must not have the saved trail painted
    // back beside 正在访问模型. Another session's busy flag does not count.
    // Settle still restores a thinner final row.
    trustMissingTimeline = false,
): ChatMessage[] {
    const groups = [visible, saved, held, fresh];
    const uniqueGroups: ChatMessage[][] = [];
    const seenGroups = new Set<ChatMessage[]>();
    for (const group of groups) {
        if (!Array.isArray(group) || seenGroups.has(group)) continue;
        seenGroups.add(group);
        uniqueGroups.push(group);
    }
    const merged = mergeChatMessages(...uniqueGroups);
    const trails = new Map<string, CodingTrail>();
    for (const group of uniqueGroups) {
        for (const message of group) {
            if (message && typeof message === "object") absorbCodingTrail(trails, message as ChatMessage);
        }
    }
    if (trails.size === 0) return merged;
    let changed = false;
    const next = merged.map(message => {
        if (message.role === "error") return message;
        const best = trails.get(message.id);
        if (!best || message.codingTimeline === best) return message;
        if (trustMissingTimeline && !(message.codingTimeline?.length)) return message;
        changed = true;
        return { ...message, codingTimeline: best };
    });
    return changed ? next : merged;
}

function assistantSubstantiveBody(message: ChatMessage): boolean {
    if ((message.content || "").trim()) return true;
    if ((message.fields?.length ?? 0) > 0) return true;
    if ((message.codingTimeline?.length ?? 0) > 0) return true;
    return (message.pendingCodingThoughts?.length ?? 0) > 0;
}

/** True when a tool row was added, removed, or replaced. Thought text does not count. */
function codingProgressRowsChanged(
    before: ChatMessage["codingTimeline"] | undefined,
    next: ChatMessage["codingTimeline"] | undefined,
): boolean {
    if (before === next) return false;
    let left = 0;
    let right = 0;
    const leftCount = before?.length ?? 0;
    const rightCount = next?.length ?? 0;
    while (left < leftCount || right < rightCount) {
        while (left < leftCount && before![left]?.kind !== "progress") left += 1;
        while (right < rightCount && next![right]?.kind !== "progress") right += 1;
        if (left >= leftCount || right >= rightCount) return left < leftCount || right < rightCount;
        const prevItem = before![left];
        const nextItem = next![right];
        if (prevItem !== nextItem && (prevItem.content || "") !== (nextItem.content || "")) return true;
        left += 1;
        right += 1;
    }
    return false;
}

/**
 * True when tab history should commit during a stream.
 * Token appends keep the previous prefix and stay on the stream ref.
 * A stage report, a field chip, a new or updated tool row, or the first thought commits.
 * A thought revision stays on the stream ref.
 */
export function projectHistorySubstantiveDelta(previous: ChatMessage[] | undefined, next: ChatMessage[]): boolean {
    const beforeById = new Map<string, ChatMessage>();
    for (const message of previous || []) {
        if (message?.id) beforeById.set(message.id, message);
    }
    for (const message of next) {
        if (!message?.id) continue;
        const before = beforeById.get(message.id);
        if (!before) {
            if (message.role !== "assistant" || assistantSubstantiveBody(message)) return true;
            continue;
        }
        if (message.role !== "assistant") continue;
        const prevContent = before.content || "";
        const nextContent = message.content || "";
        if (nextContent !== prevContent) {
            if (!prevContent.trim() && nextContent.trim()) return true;
            if (nextContent && !nextContent.startsWith(prevContent)) return true;
        }
        if ((message.fields?.length ?? 0) > (before.fields?.length ?? 0)) return true;
        if ((message.codingTimeline?.length ?? 0) > (before.codingTimeline?.length ?? 0)) return true;
        if (codingProgressRowsChanged(before.codingTimeline, message.codingTimeline)) return true;
        if ((before.pendingCodingThoughts?.length ?? 0) === 0 && (message.pendingCodingThoughts?.length ?? 0) > 0) return true;
    }
    return false;
}

export function withoutProjectContextMessages(history: unknown[] | undefined): ChatMessage[] {
    if (!Array.isArray(history)) return [];
    return history.filter((message): message is ChatMessage => {
        if (!message || typeof message !== "object") return false;
        return !(message as ChatMessage & { isProjectContext?: boolean }).isProjectContext;
    });
}
