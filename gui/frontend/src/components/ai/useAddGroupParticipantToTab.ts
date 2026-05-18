import { useCallback } from "react";
import type { AITab, AITabState } from "./AITabTypes";

function looksLikeRawParticipantId(value: string): boolean {
    return /^(m_[A-Za-z0-9]+|machine[-_][A-Za-z0-9-]+|ve[-_][A-Za-z0-9-]+|profile[-_][A-Za-z0-9-]+|disc[-_][A-Za-z0-9-]+|discussion[-_][A-Za-z0-9-]+|consultation[-_][A-Za-z0-9-]+|session[-_][A-Za-z0-9-]+|[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})$/i.test(value);
}

function participantNameFromInput(name: string, participantId: string): string {
    const trimmed = String(name || "").trim();
    const id = String(participantId || "").trim();
    return trimmed && trimmed !== id && !looksLikeRawParticipantId(trimmed) ? trimmed : "Digital employee";
}

function participantNameFromTab(tab: AITab): Record<string, string> {
    const id = String(tab.veId || "").trim();
    const mapped = String(id ? tab.participantNames?.[id] || "" : "").trim();
    if (id && mapped && mapped !== id && !looksLikeRawParticipantId(mapped)) return { [id]: mapped };
    const title = String(tab.title || "").trim();
    if (tab.type === "ve" && id && title && title !== id && !looksLikeRawParticipantId(title)) return { [id]: title };
    return {};
}

type UseAddGroupParticipantToTabOptions = {
    getTabState: (tabId: string) => AITabState | undefined;
    upgradeVETabToGroup: (tabId: string, participants: string[], discussionId?: string, participantNames?: Record<string, string>) => AITab | null;
};

export function useAddGroupParticipantToTab({ getTabState, upgradeVETabToGroup }: UseAddGroupParticipantToTabOptions) {
    return useCallback(async (tab: AITab, veId: string, veName: string) => {
        const participantId = String(veId || "").trim();
        if (!participantId) return null;

        const currentParticipants = tab.participants || (tab.veId ? [tab.veId] : []);
        if (currentParticipants.includes(participantId)) return null;

        const nextParticipants = [...currentParticipants, participantId];
        const participantName = participantNameFromInput(veName, participantId);
        const participantNames = { ...participantNameFromTab(tab), [participantId]: participantName };
        const updateUI = (sessionId?: string) => upgradeVETabToGroup(tab.id, nextParticipants, sessionId, participantNames);

        let sessionId = getTabState(tab.id)?.sessionId || tab.discussionId || "";

        try {
            const mod = await import("../../../wailsjs/go/main/App");

            if (!sessionId) {
                const initiateConversation = (mod as any).InitiateVEConversation;
                if (typeof initiateConversation !== "function" || !tab.veId) {
                    return null;
                }
                const created = await initiateConversation(tab.veId);
                sessionId = String(created?.session_id || created?.SessionID || "").trim();
                if (!sessionId) return null;
            }

            const addVEToGroup = (mod as any).AddVEToGroup;
            if (typeof addVEToGroup !== "function") {
                return null;
            }

            await addVEToGroup(sessionId, participantId);
            return updateUI(sessionId);
        } catch (err) {
            console.error("Failed to add group participant:", err);
            return null;
        }
    }, [getTabState, upgradeVETabToGroup]);
}
