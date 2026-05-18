import { useCallback } from "react";
import type { AITab, AITabState } from "./AITabTypes";

function looksLikeRawParticipantId(value: string): boolean {
    return /^(m_[A-Za-z0-9]+|machine[-_][A-Za-z0-9-]+|ve[-_][A-Za-z0-9-]+|profile[-_][A-Za-z0-9-]+|disc[-_][A-Za-z0-9-]+|discussion[-_][A-Za-z0-9-]+|consultation[-_][A-Za-z0-9-]+|session[-_][A-Za-z0-9-]+|[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})$/i.test(value);
}

function participantNameFromTab(tab: AITab): string {
    const id = String(tab.veId || "").trim();
    const mapped = String(id ? tab.participantNames?.[id] || "" : "").trim();
    if (mapped && mapped !== id && !looksLikeRawParticipantId(mapped)) return mapped;
    const title = String(tab.title || "").trim();
    if (title && title !== id && !looksLikeRawParticipantId(title)) return title;
    return "Digital employee";
}

type UseAddLocalMaclawToTabOptions = {
    getTabState: (tabId: string) => AITabState | undefined;
    upgradeVETabToGroup: (tabId: string, participants: string[], discussionId?: string, participantNames?: Record<string, string>) => AITab | null;
};

export function useAddLocalMaclawToTab({ getTabState, upgradeVETabToGroup }: UseAddLocalMaclawToTabOptions) {
    return useCallback(async (tab: AITab) => {
        if (!tab.veId) return null;
        const currentParticipants = tab.participants || [tab.veId];
        if (currentParticipants.includes("local-maclaw")) return null;

        const nextParticipants = [...currentParticipants, "local-maclaw"];
        const participantNames: Record<string, string> = {
            ...(tab.veId ? { [tab.veId]: participantNameFromTab(tab) } : {}),
            "local-maclaw": "Local AI",
        };
        const updateUI = (sessionId?: string) => upgradeVETabToGroup(tab.id, nextParticipants, sessionId, participantNames);

        let sessionId = getTabState(tab.id)?.sessionId || tab.discussionId || "";
        const optimisticTab = updateUI(sessionId || undefined);

        try {
            const mod = await import("../../../wailsjs/go/main/App");

            if (!sessionId) {
                const initiateConversation = (mod as any).InitiateVEConversation;
                if (typeof initiateConversation !== "function" || !tab.veId) {
                    return optimisticTab;
                }
                const created = await initiateConversation(tab.veId);
                sessionId = String(created?.session_id || created?.SessionID || "").trim();
                if (!sessionId) return optimisticTab;
            }

            const registerLocalExecutor = (mod as any).RegisterLocalExecutorInGroup;
            if (typeof registerLocalExecutor !== "function") {
                return optimisticTab;
            }

            await registerLocalExecutor(sessionId);
            return updateUI(sessionId) || optimisticTab;
        } catch (err) {
            console.error("Failed to add local Maclaw to group:", err);
            return optimisticTab;
        }
    }, [getTabState, upgradeVETabToGroup]);
}
