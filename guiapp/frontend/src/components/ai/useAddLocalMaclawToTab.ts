import { useCallback } from "react";
import type { AITab, AITabState } from "./AITabTypes";
import { hasLocalAIParticipant, localExecutorDisplayName, localExecutorParticipantID, looksLikeRawParticipantId, type LocalGroupExecutorRegistration } from "./localAIIdentity";
import { participantIdentityMatches } from "./participantIdentity";
import { getWailsAppModule } from "../../utils/wailsAppModule";



function participantNamesFromTab(tab: AITab): Record<string, string> {
    const names: Record<string, string> = { ...(tab.participantNames || {}) };
    const id = String(tab.veId || "").trim();
    const mapped = String(id ? names[id] || "" : "").trim();
    if (id && mapped && mapped !== id && !looksLikeRawParticipantId(mapped)) return names;
    const title = String(tab.title || "").trim();
    if (id && title && title !== id && !looksLikeRawParticipantId(title)) {
        return { ...names, [id]: title };
    }
    return names;
}

type UseAddLocalMaclawToTabOptions = {
    getTabState: (tabId: string) => AITabState | undefined;
    upgradeVETabToGroup: (tabId: string, participants: string[], discussionId?: string, participantNames?: Record<string, string>, localParticipantIds?: string[]) => AITab | null;
};

export function useAddLocalMaclawToTab({ getTabState, upgradeVETabToGroup }: UseAddLocalMaclawToTabOptions) {
    return useCallback(async (tab: AITab) => {
        if (!tab.veId) return null;
        const currentParticipants = tab.participants || [tab.veId];
        if (hasLocalAIParticipant(tab)) return null;

        let sessionId = getTabState(tab.id)?.sessionId || tab.discussionId || "";

        try {
            const mod = await getWailsAppModule();

            if (!sessionId) {
                const initiateConversation = (mod as any).InitiateVEConversation;
                if (typeof initiateConversation !== "function" || !tab.veId) {
                    return null;
                }
                const created = await initiateConversation(tab.veId);
                sessionId = String(created?.session_id || created?.SessionID || "").trim();
                if (!sessionId) return null;
            }

            const registerLocalExecutor = (mod as any).RegisterLocalExecutorInGroup;
            if (typeof registerLocalExecutor !== "function") {
                return null;
            }

            const registered = await registerLocalExecutor(sessionId) as LocalGroupExecutorRegistration | undefined;
            const localParticipantId = localExecutorParticipantID(registered);
            if (!localParticipantId) return null;
            if (currentParticipants.some((id) => participantIdentityMatches(id, localParticipantId))) return null;
            const nextParticipants = [...currentParticipants, localParticipantId];
            const participantNames: Record<string, string> = {
                ...participantNamesFromTab(tab),
                [localParticipantId]: localExecutorDisplayName(registered),
            };
            return upgradeVETabToGroup(tab.id, nextParticipants, sessionId, participantNames, [localParticipantId]);
        } catch (err) {
            console.error("Failed to add local Maclaw to group:", err);
            return null;
        }
    }, [getTabState, upgradeVETabToGroup]);
}
