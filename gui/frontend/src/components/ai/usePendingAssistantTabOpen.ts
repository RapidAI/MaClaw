import { useCallback, useEffect, useRef } from "react";
import type { CreateGroupTabOptions } from "./useAITabManager";
import type { AITab, AITabState } from "./AITabTypes";
import type { VirtualEmployeeEntry } from "./VirtualEmployeeTab";
import { isHistoryDiscussionReadOnly } from "./historyDiscussionUtils";

/** Pending project tab open request from external (e.g. sidebar "create task") */
export interface PendingProjectTabOpen {
    projectPath: string;
    taskTitle: string;
    /** Message to send as the first message in the tab. Defaults to taskTitle if not specified. */
    initialMessage?: string;
    /** If true, send initialMessage (or taskTitle) as the first message after tab creation */
    autoSend?: boolean;
}

export interface PendingHistoryDiscussionOpen {
    id?: string;
    topic?: string;
    question?: string;
    local_relation?: string;
    role?: string;
    readonly?: boolean;
    status?: string;
    participant_ids?: string[];
}

function isConversationMessage(value: unknown): value is { role?: unknown } {
    return !!value && typeof value === "object";
}

interface PendingAssistantTabOpenOptions {
    createVETab: (veId: string, veName: string, sessionId?: string, onlineStatus?: "online" | "offline") => AITab | null;
    createGroupTab: (id: string, title: string, participants: string[], options?: CreateGroupTabOptions) => AITab | null;
    createProjectTab: (projectPath: string, taskTitle: string) => AITab | null;
    activateTab?: (tabId: string) => void;
    getTabState?: (tabId: string) => AITabState | undefined;
    getTabList?: () => AITab[];
    hasProjectTab?: (projectPath: string) => boolean;
    sendMessage?: (text: string, options?: Record<string, unknown>) => Promise<boolean>;
    pendingVEOpen?: VirtualEmployeeEntry | null;
    onPendingVEOpenHandled?: () => void;
    pendingHistoryDiscussionOpen?: PendingHistoryDiscussionOpen | null;
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
    const openHistoryDiscussion = useCallback((discussion: PendingHistoryDiscussionOpen) => {
        const discussionId = String(discussion?.id || "").trim();
        if (!discussionId) return;

        const readOnly = isHistoryDiscussionReadOnly(discussion);

        // First prefer any already-open tab for this discussion/session. History
        // rows can represent active, read-only, or invited conversations, but a
        // double-click should focus the live tab when it is already present.
        if (activateTab && getTabList) {
            const tabs = getTabList();
            // Match by session/discussion ID: VE tabs store their A2A session ID
            // in tabState.sessionId, which equals the discussion ID from Hub.
            const existingSessionTab = getTabState ? tabs.find(t => {
                if (t.type !== "ve" && t.type !== "group") return false;
                const state = getTabState(t.id);
                return String(state?.sessionId || state?.discussionId || "").trim() === discussionId;
            }) : undefined;
            if (existingSessionTab) {
                activateTab(existingSessionTab.id);
                return;
            }
            // Also check if there's already a group tab for this discussion.
            const existingGroupTab = tabs.find(t => t.type === "group" && t.discussionId === discussionId);
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
        createVETab(pendingVEOpen.id, pendingVEOpen.name, undefined, pendingVEOpen.online_status);
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
    const sendMessageRef = useRef(sendMessage);
    sendMessageRef.current = sendMessage;
    const onProjectTabHandledRef = useRef(onPendingProjectTabOpenHandled);
    onProjectTabHandledRef.current = onPendingProjectTabOpenHandled;
    const getTabStateRef = useRef(getTabState);
    getTabStateRef.current = getTabState;
    const hasProjectTabRef = useRef(hasProjectTab);
    hasProjectTabRef.current = hasProjectTab;

    useEffect(() => {
        if (!pendingProjectTabOpen) return;

        // Capture request data and clear pending state synchronously.
        // The guard above prevents re-entry when pending becomes null.
        const { projectPath, taskTitle, initialMessage, autoSend } = pendingProjectTabOpen;
        onProjectTabHandledRef.current?.();

        // Check if the tab already exists in the tab list BEFORE creating it.
        // This is a synchronous read of tabStateRef — reliable for tabs that were
        // restored from localStorage (synchronous on mount) or from the backend
        // index merge (async, but completes before user interaction in practice).
        // Combined with the post-creation history check, this provides two-layer
        // protection against duplicate autoSend.
        const tabExistedInList = hasProjectTabRef.current?.(projectPath) ?? false;

        const tab = createProjectTabRef.current(projectPath, taskTitle);
        if (!tab) return;

        // Async operations use refs — no stale closure, no dependency churn.
        (async () => {
            if (!autoSend) return;
            const send = sendMessageRef.current;
            if (!send) return;

            // Determine whether this is a reused tab by two complementary signals:
            //
            // Signal 1: The tab already has conversation history in tabStatesRef.
            // This catches tabs that were actively used in this session.
            //
            // Signal 2: The tab existed in the tab list BEFORE createProjectTab was
            // called (hasProjectTab check done synchronously before createProjectTab).
            // This catches tabs restored from localStorage/backend that don't have
            // state in tabStatesRef yet (restoration only adds to tabs array, not
            // to the state map).
            //
            // Either signal being true means this is a reused tab → skip autoSend.
            const existingState = getTabStateRef.current?.(tab.id);
            const hasExistingConversation = existingState?.history &&
                Array.isArray(existingState.history) &&
                existingState.history.some((m) => isConversationMessage(m) && (m.role === "user" || m.role === "assistant"));
            if (hasExistingConversation || tabExistedInList) return;

            const msg = initialMessage || taskTitle;
            await send(msg, { tabId: tab.id, project_path: tab.projectPath }).catch(() => {});
        })();
    }, [pendingProjectTabOpen]);
    // ↑ ONLY pendingProjectTabOpen in deps. All callbacks accessed via refs.
    // This ensures the effect fires exactly once per non-null pendingProjectTabOpen,
    // regardless of how often the parent re-renders.
}
