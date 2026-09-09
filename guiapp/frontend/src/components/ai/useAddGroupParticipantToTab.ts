import { useCallback } from "react";
import type { AITab, AITabState } from "./AITabTypes";
import { looksLikeRawParticipantId } from "./localAIIdentity";
import { extractErrorMessage } from "./participantAddError";
import { addParticipantIdentityKeys } from "./participantIdentity";
import { getWailsAppModule } from "../../utils/wailsAppModule";


function participantNameFromInput(name: string, participantId: string): string {
    const trimmed = String(name || "").trim();
    const id = String(participantId || "").trim();
    return trimmed && trimmed !== id && !looksLikeRawParticipantId(trimmed) ? trimmed : "Digital employee";
}


function participantNameFromTab(tab: AITab): Record<string, string> {
    const names: Record<string, string> = { ...(tab.participantNames || {}) };
    const id = String(tab.veId || "").trim();
    const mapped = String(id ? names[id] || "" : "").trim();
    if (id && mapped && mapped !== id && !looksLikeRawParticipantId(mapped)) return names;
    const title = String(tab.title || "").trim();
    if (tab.type === "ve" && id && title && title !== id && !looksLikeRawParticipantId(title)) {
        return { ...names, [id]: title };
    }
    return names;
}

function hasParticipantIdentity(participants: string[], participantId: string): boolean {
    const current = new Set<string>();
    participants.forEach((id) => addParticipantIdentityKeys(current, id));
    const candidate = new Set<string>();
    addParticipantIdentityKeys(candidate, participantId);
    for (const key of candidate) {
        if (current.has(key)) return true;
    }
    return false;
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
        if (hasParticipantIdentity(currentParticipants, participantId)) return null;

        const nextParticipants = [...currentParticipants, participantId];
        const participantName = participantNameFromInput(veName, participantId);
        const participantNames = { ...participantNameFromTab(tab), [participantId]: participantName };
        const updateUI = (sessionId?: string) => upgradeVETabToGroup(tab.id, nextParticipants, sessionId, participantNames);

        let sessionId = getTabState(tab.id)?.sessionId || tab.discussionId || "";

        try {
            const mod = await getWailsAppModule();

            if (!sessionId) {
                const initiateConversation = (mod as any).InitiateVEConversation;
                if (typeof initiateConversation !== "function" || !tab.veId) {
                    throw new Error("missing InitiateVEConversation binding");
                }
                const created = await initiateConversation(tab.veId);
                sessionId = String(created?.session_id || created?.SessionID || "").trim();
                if (!sessionId) throw new Error("created discussion has no session id");
            }

            const addVEToGroup = (mod as any).AddVEToGroup;
            if (typeof addVEToGroup !== "function") {
                throw new Error("missing AddVEToGroup binding");
            }

            await addVEToGroup(sessionId, participantId);
            return updateUI(sessionId);
        } catch (err) {
            console.error("Failed to add group participant:", err);
            const message = extractErrorMessage(err);
            throw new Error(message || "participant_add_failed");
        }
    }, [getTabState, upgradeVETabToGroup]);
}
