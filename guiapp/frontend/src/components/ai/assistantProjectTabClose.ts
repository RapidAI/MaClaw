import type { ChatMessage } from "./useAIAssistant";
import { forgetAIAssistantSessionRounds } from "./useAIAssistant";
import type { AITab, AITabState } from "./AITabTypes";
import { expertSessionKey, messageBelongsToSessionOrLegacy, projectSessionKey } from "./aiAssistantPanelSessionUtils";
import { mergeChatMessages, withoutProjectContextMessages } from "./aiAssistantProjectTabState";

type ProjectRound = { tabId: string | null; projectPath: string; sessionKey: string; baseline: number; seq?: number };
type DetachedRound = { tabId: string; messageIds: Set<string> };
type CloseSnapshot = {
    tabId: string;
    projectPath?: string;
    messages: ChatMessage[];
    inputText: string;
    scrollTop: number;
};

/** Refs and setters the project-tab close path mutates. Kept outside the panel so the panel file can stay under its line cap. */
export type AssistantProjectTabCloseDeps = {
    getTabs: () => AITab[];
    getTabState: (tabId: string) => AITabState | undefined;
    saveTabState: (tabId: string, state: Partial<AITabState>) => void;
    closeTab: (tabId: string) => void;
    messages: ChatMessage[];
    activeTabIdRef: { current: string };
    latestDisplayMessagesRef: { current: ChatMessage[] };
    latestProjectCloseSnapshotRef: { current: CloseSnapshot | null };
    clearedProjectTabIdsRef: { current: Set<string> };
    projectConversationHydrationGenerationByTabIdRef: { current: Map<string, number> };
    projectConversationHydrationByTabIdRef: { current: Map<string, Promise<void>> };
    projectTabRoundsRef: { current: Map<string, ProjectRound> };
    detachedProjectRoundsRef: { current: Map<string, DetachedRound> };
    projectTabMsgIdsRef: { current: Set<string> };
    projectPrepareTimersRef: { current: Map<string, number> };
    deferredProjectInitialSendsRef: { current: Map<string, unknown> };
    pendingRemoteInitialSendRef: { current: Map<string, unknown> };
    previewStateMapRef: { current: Map<string, unknown> };
    previewOwnerTabRef: { current: string };
    previewOwnerResetPendingRef: { current: boolean };
    skillRecordingTabId: string | null;
    setSkillRecordingTabId: (id: string | null) => void;
    setProjectTabPreparing: (tabId: string, preparing: boolean) => void;
    setProjectTabRouteVersion: (update: (version: number) => number) => void;
    setDetachedProjectRoundVersion: (update: (version: number) => number) => void;
    persistProjectTabMsgIds: () => void;
};

function clearProjectRoundTracking(tabId: string, deps: AssistantProjectTabCloseDeps) {
    let changed = false;
    const tab = deps.getTabs().find(item => item.id === tabId);
    if (tab?.type === "project" && tab.projectPath) {
        forgetAIAssistantSessionRounds(tab.sessionKey || `desktop-user:${tab.projectPath}`);
        const prepareTimer = deps.projectPrepareTimersRef.current.get(tabId);
        if (prepareTimer !== undefined) {
            window.clearTimeout(prepareTimer);
            deps.projectPrepareTimersRef.current.delete(tabId);
        }
        deps.setProjectTabPreparing(tabId, false);
        deps.deferredProjectInitialSendsRef.current.delete(tabId);
        deps.pendingRemoteInitialSendRef.current.delete(tabId);
    }
    for (const [roundKey, round] of deps.projectTabRoundsRef.current) {
        if (round.tabId !== tabId) continue;
        const sessionKey = tab?.type === "project" ? (tab.sessionKey || projectSessionKey(tab.projectPath)) : projectSessionKey(round.projectPath);
        const messagesToMark = sessionKey
            ? deps.messages.slice(round.baseline).filter(message => messageBelongsToSessionOrLegacy(message, sessionKey))
            : [];
        for (const message of messagesToMark) {
            deps.projectTabMsgIdsRef.current.add(message.id);
        }
        deps.projectTabRoundsRef.current.delete(roundKey);
        changed = true;
    }
    for (const [key, detached] of deps.detachedProjectRoundsRef.current) {
        if (detached.tabId !== tabId) continue;
        for (const messageId of detached.messageIds) {
            deps.projectTabMsgIdsRef.current.add(messageId);
        }
        deps.detachedProjectRoundsRef.current.delete(key);
        changed = true;
    }
    if (changed) {
        deps.persistProjectTabMsgIds();
        deps.setProjectTabRouteVersion(version => version + 1);
        deps.setDetachedProjectRoundVersion(version => version + 1);
    }
}

/** Close one assistant tab and drop its in-panel project bookkeeping. History stays resumable. */
export function closeAssistantProjectTab(tabId: string, deps: AssistantProjectTabCloseDeps) {
    const tab = deps.getTabs().find(item => item.id === tabId);
    if (tab?.type === "project") {
        deps.projectConversationHydrationGenerationByTabIdRef.current.set(
            tabId,
            (deps.projectConversationHydrationGenerationByTabIdRef.current.get(tabId) || 0) + 1,
        );
        deps.projectConversationHydrationByTabIdRef.current.delete(tabId);
        const snapshot = deps.latestProjectCloseSnapshotRef.current;
        const existingState = deps.getTabState(tabId);
        if (snapshot?.tabId === tabId) {
            const wasCleared = deps.clearedProjectTabIdsRef.current.has(tabId);
            const snapshotMessages = !wasCleared && deps.activeTabIdRef.current === tabId ? deps.latestDisplayMessagesRef.current : snapshot.messages;
            const nextHistory = wasCleared
                ? withoutProjectContextMessages(snapshotMessages)
                : mergeChatMessages(
                    withoutProjectContextMessages(existingState?.history),
                    withoutProjectContextMessages(snapshotMessages),
                );
            deps.saveTabState(tabId, {
                ...existingState,
                history: nextHistory,
                scrollTop: snapshot.scrollTop,
                inputText: snapshot.inputText,
                projectPath: snapshot.projectPath || tab.projectPath,
                lastActiveAt: Date.now(),
            });
        }
    }
    // Closing an expert removes its UI owner. Revoke hook-owned work before the
    // tab disappears, so a late file-picker result cannot land in a reopened session.
    if (tab?.type === "expert") {
        const sessionKey = expertSessionKey(tab.expertId);
        if (sessionKey) forgetAIAssistantSessionRounds(sessionKey);
    }
    clearProjectRoundTracking(tabId, deps);
    deps.previewStateMapRef.current.delete(tabId);
    if (deps.previewOwnerTabRef.current === tabId) {
        deps.previewOwnerTabRef.current = "local";
        deps.previewOwnerResetPendingRef.current = true;
    }
    if (deps.skillRecordingTabId === tabId) {
        deps.setSkillRecordingTabId(null);
        (window as any).go?.main?.App?.StopSkillRecording?.().catch(() => {});
    }
    deps.closeTab(tabId);
}
