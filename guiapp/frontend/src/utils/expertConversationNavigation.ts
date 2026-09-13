export const OPEN_EXPERT_CONVERSATION_EVENT = "maclaw:open-expert";

export type ExpertConversationOpenDetail = {
    expert: {
        id: string;
        name: string;
        description?: string;
        icon?: string;
        [key: string]: unknown;
    };
};

export function openExpertConversation(expert: ExpertConversationOpenDetail["expert"] | null | undefined): void {
    const id = String(expert?.id || "").trim();
    if (!id || typeof window === "undefined") return;
    window.dispatchEvent(new CustomEvent(OPEN_EXPERT_CONVERSATION_EVENT, {
        detail: { expert: { ...expert, id, name: String(expert?.name || "").trim() || id } },
    }));
}
