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

export function withoutProjectContextMessages(history: unknown[] | undefined): ChatMessage[] {
    if (!Array.isArray(history)) return [];
    return history.filter((message): message is ChatMessage => {
        if (!message || typeof message !== "object") return false;
        return !(message as ChatMessage & { isProjectContext?: boolean }).isProjectContext;
    });
}
