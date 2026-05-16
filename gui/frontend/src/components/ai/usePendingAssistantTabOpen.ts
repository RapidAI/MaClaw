import { useCallback, useEffect, useRef } from "react";
import type { CreateGroupTabOptions } from "./useAITabManager";
import type { AITab, AITabState } from "./AITabTypes";
import type { VirtualEmployeeEntry } from "./VirtualEmployeeTab";
import { isHistoryDiscussionReadOnly, getHistoryDiscussionRelation } from "./historyDiscussionUtils";

/** Pending project tab open request from external (e.g. sidebar "create task") */
export interface PendingProjectTabOpen {
    projectPath: string;
    taskTitle: string;
    /** Message to send as the first message in the tab. Defaults to taskTitle if not specified. */
    initialMessage?: string;
    /** If true, send initialMessage (or taskTitle) as the first message after tab creation */
    autoSend?: boolean;
}

interface PendingAssistantTabOpenOptions {
    createVETab: (veId: string, veName: string, sessionId?: string) => AITab | null;
    createGroupTab: (id: string, title: string, participants: string[], options?: CreateGroupTabOptions) => AITab | null;
    createProjectTab: (projectPath: string, taskTitle: string) => AITab | null;
    activateTab?: (tabId: string) => void;
    getTabState?: (tabId: string) => AITabState | undefined;
    getTabList?: () => AITab[];
    hasProjectTab?: (projectPath: string) => boolean;
    sendMessage?: (text: string, options?: Record<string, unknown>) => Promise<boolean>;
    pendingVEOpen?: VirtualEmployeeEntry | null;
    onPendingVEOpenHandled?: () => void;
    pendingHistoryDiscussionOpen?: any;
    onPendingHistoryDiscussionOpenHandled?: () => void;
    pendingProjectTabOpen?: PendingProjectTabOpen | null;
    onPendingProjectTabOpenHandled?: () => void;
}

export function usePendingAssistantTabOpen({
    createVETab,
    createGroupTab,
    createProjectTab,
    activateTab,
    getTabState,
    getTabList,
    hasProjectTab,
    sendMessage,
    pendingVEOpen,
    onPendingVEOpenHandled,
    pendingHistoryDiscussionOpen,
    onPendingHistoryDiscussionOpenHandled,
    pendingProjectTabOpen,
    onPendingProjectTabOpenHandled,
}: PendingAssistantTabOpenOptions) {
    const openHistoryDiscussion = useCallback((discussion: any) => {
        const discussionId = String(discussion?.id || "").trim();
        if (!discussionId) return;

        const relation = getHistoryDiscussionRelation(discussion);
        const readOnly = isHistoryDiscussionReadOnly(discussion);

        // If this is a discussion I initiated and it's not read-only, check if
        // there's already an active VE tab for the same session. If so,
        // switch to it instead of creating a new read-only history tab.
        if (relation === "initiated_by_me" && !readOnly && activateTab && getTabList && getTabState) {
            const tabs = getTabList();
            // Match by session/discussion ID: VE tabs store their A2A session ID
            // in tabState.sessionId, which equals the discussion ID from Hub.
            const existingVETab = tabs.find(t => {
                if (t.type !== "ve") return false;
                const state = getTabState(t.id);
                return state?.sessionId === discussionId;
            });
            if (existingVETab) {
                activateTab(existingVETab.id);
                return;
            }
            // Also check if there's already a group tab for this discussion
            const existingGroupTab = tabs.find(t =>
                t.type === "group" && t.discussionId === discussionId && !t.readOnly
            );
            if (existingGroupTab) {
                activateTab(existingGroupTab.id);
                return;
            }
        }

        const title = discussion?.topic || discussion?.question || discussionId;
        const role = discussion?.local_relation || discussion?.role;
        createGroupTab(`history-${discussionId}`, title, discussion?.participant_ids || [], { discussionId, readOnly, role });
    }, [activateTab, createGroupTab, getTabList, getTabState]);

    useEffect(() => {
        if (!pendingVEOpen) return;
        createVETab(pendingVEOpen.id, pendingVEOpen.name);
        onPendingVEOpenHandled?.();
    }, [createVETab, onPendingVEOpenHandled, pendingVEOpen]);

    useEffect(() => {
        if (!pendingHistoryDiscussionOpen) return;
        openHistoryDiscussion(pendingHistoryDiscussionOpen);
        onPendingHistoryDiscussionOpenHandled?.();
    }, [onPendingHistoryDiscussionOpenHandled, openHistoryDiscussion, pendingHistoryDiscussionOpen]);

    // --- Project Tab: use refs to avoid stale closures in async operations ---
    // The effect only triggers on `pendingProjectTabOpen` changes (the signal).
    // All other dependencies are accessed via refs to ensure the async operation
    // always uses the latest values without causing effect re-runs.
    const createProjectTabRef = useRef(createProjectTab);
    createProjectTabRef.current = createProjectTab;
    const hasProjectTabRef = useRef(hasProjectTab);
    hasProjectTabRef.current = hasProjectTab;
    const sendMessageRef = useRef(sendMessage);
    sendMessageRef.current = sendMessage;
    const onProjectTabHandledRef = useRef(onPendingProjectTabOpenHandled);
    onProjectTabHandledRef.current = onPendingProjectTabOpenHandled;

    useEffect(() => {
        if (!pendingProjectTabOpen) return;

        // Capture request data and clear pending state synchronously.
        // The guard above prevents re-entry when pending becomes null.
        const { projectPath, taskTitle, initialMessage, autoSend } = pendingProjectTabOpen;
        onProjectTabHandledRef.current?.();

        // Determine whether the tab already exists BEFORE creating it.
        // This is the definitive signal for "reused tab vs new tab" — it cannot
        // be polluted by async context injection or any other concurrent operation.
        const tabExistedBefore = hasProjectTabRef.current?.(projectPath) ?? false;

        const tab = createProjectTabRef.current(projectPath, taskTitle);
        if (!tab) return;

        // Async operations use refs — no stale closure, no dependency churn.
        (async () => {
            if (!autoSend) return;
            const send = sendMessageRef.current;
            if (!send) return;

            // Skip autoSend for reused tabs — they already have conversation.
            // For new tabs, always send regardless of what's in history (context
            // injection may have added a system message, but that's not conversation).
            if (tabExistedBefore) return;

            const msg = initialMessage || taskTitle;
            await send(msg, { tabId: tab.id, project_path: tab.projectPath }).catch(() => {});
        })();
    }, [pendingProjectTabOpen]);
    // ↑ ONLY pendingProjectTabOpen in deps. All callbacks accessed via refs.
    // This ensures the effect fires exactly once per non-null pendingProjectTabOpen,
    // regardless of how often the parent re-renders.
}
