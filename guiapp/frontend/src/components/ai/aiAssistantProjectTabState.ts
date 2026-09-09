import type { ChatMessage } from "./useAIAssistant";

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
    for (const group of groups) {
        if (!Array.isArray(group)) continue;
        for (const message of group) {
            if (!message || typeof message !== "object") continue;
            const chatMessage = message as ChatMessage;
            const id = typeof chatMessage.id === "string" ? chatMessage.id : "";
            if (id) {
                const existingIndex = indexById.get(id);
                if (existingIndex !== undefined) {
                    merged[existingIndex] = chatMessage;
                    continue;
                }
                indexById.set(id, merged.length);
            }
            merged.push(chatMessage);
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
