import { useCallback, useEffect, useRef } from "react";
import type { CreateGroupTabOptions, CreateProjectTabOptions } from "./useAITabManager";
import type { AITab, AITabState } from "./AITabTypes";
import type { VirtualEmployeeEntry } from "./VirtualEmployeeTab";
import { isHistoryDiscussionReadOnly } from "./historyDiscussionUtils";
import { isLocalHumanParticipantId } from "./localAIIdentity";
import { addParticipantIdentityKeys, participantIdentityMatches } from "./participantIdentity";

/** Pending project tab open request from external (e.g. sidebar "create task") */
export interface PendingProjectTabOpen {
    projectPath: string;
    taskTitle: string;
    /** Message to send as the first message in the tab. Defaults to taskTitle if not specified. */
    initialMessage?: string;
    /** If true, send initialMessage (or taskTitle) as the first message after tab creation */
    autoSend?: boolean;
    /** Changes the preparation copy shown while the new project-backed agent session is being created. */
    prepareMode?: "restore-context" | "new-agent";
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

function singlePendingParticipantId(discussion: PendingHistoryDiscussionOpen): string {
    const seen = new Set<string>();
    const ids: string[] = [];
    for (const rawId of discussion?.participant_ids || []) {
        const id = String(rawId || "").trim();
        if (!id || isLocalHumanParticipantId(id)) continue;
        const before = seen.size;
        addParticipantIdentityKeys(seen, id);
        if (seen.size !== before) ids.push(id);
    }
    return ids.length === 1 ? ids[0] : "";
}

function isConversationMessage(value: unknown): value is { role?: unknown } {
    return !!value && typeof value === "object";
}

const textForPendingTabLang = (lang: string | undefined, en: string, zhHans: string, zhHant = zhHans): string => (
    lang === "zh-Hant" ? zhHant : lang?.startsWith("zh") || !lang ? zhHans : en
);

function looksLikeRawDiscussionTitle(value: string): boolean {
    return /^(disc|discussion|consultation|session)[-_][A-Za-z0-9-]+$|^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(value);
}

function readableHistoryDiscussionTitle(discussion: PendingHistoryDiscussionOpen, discussionId: string, lang?: string): string {
    const candidate = String(discussion?.topic || discussion?.question || "").trim();
    if (candidate && candidate !== discussionId && !looksLikeRawDiscussionTitle(candidate)) return candidate;
    return textForPendingTabLang(lang, "Group discussion", "\u7fa4\u7ec4\u8ba8\u8bba", "\u7fa4\u7ec4\u8ba8\u8bba");
}

interface PendingAssistantTabOpenOptions {
    lang?: string;
    createVETab: (veId: string, veName: string, sessionId?: string, onlineStatus?: "online" | "offline", avatarDataURL?: string) => AITab | null;
    createGroupTab: (id: string, title: string, participants: string[], options?: CreateGroupTabOptions) => AITab | null;
    createProjectTab: (projectPath: string, taskTitle: string, options?: CreateProjectTabOptions) => AITab | null;
    activateTab?: (tabId: string) => void;
    getTabState?: (tabId: string) => AITabState | undefined;
    saveTabState?: (tabId: string, state: Partial<AITabState>) => void;
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
    lang,
    createVETab,
    createGroupTab,
    createProjectTab,
    activateTab,
    getTabState,
    saveTabState,
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

        const bindHistoryStateToTab = (tabId: string) => {
            if (!saveTabState) return;
            const current = getTabState?.(tabId);
            saveTabState(tabId, {
                history: current?.history || [],
                scrollTop: current?.scrollTop || 0,
                inputText: current?.inputText || "",
                ...current,
                sessionId: current?.sessionId || discussionId,
                discussionId,
                readOnly,
                lastActiveAt: Date.now(),
            });
        };

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
                bindHistoryStateToTab(existingSessionTab.id);
                activateTab(existingSessionTab.id);
                return;
            }
            // Also check if there's already a group tab for this discussion.
            const existingGroupTab = tabs.find(t => t.type === "group" && t.discussionId === discussionId);
            if (existingGroupTab) {
                bindHistoryStateToTab(existingGroupTab.id);
                activateTab(existingGroupTab.id);
                return;
            }

            // If the history row is a 1:1 VE discussion and the direct VE tab
            // already exists but has not saved its session ID yet, use the VE
            // participant identity to avoid creating a duplicate history tab.
            const onlyParticipant = singlePendingParticipantId(discussion);
            if (onlyParticipant) {
                const existingParticipantTab = tabs.find(t => {
                    if (t.type !== "ve" && t.type !== "group") return false;
                    if (participantIdentityMatches(t.veId, onlyParticipant)) return true;
                    const seen = new Set<string>();
                    const participants: string[] = [];
                    for (const rawId of t.participants || []) {
                        const id = String(rawId || "").trim();
                        if (!id || isLocalHumanParticipantId(id)) continue;
                        const before = seen.size;
                        addParticipantIdentityKeys(seen, id);
                        if (seen.size !== before) participants.push(id);
                    }
                    return !t.veId && participants.length === 1 && participantIdentityMatches(participants[0], onlyParticipant);
                });
                if (existingParticipantTab) {
                    bindHistoryStateToTab(existingParticipantTab.id);
                    activateTab(existingParticipantTab.id);
                    return;
                }
            }
        }

        const title = readableHistoryDiscussionTitle(discussion, discussionId, lang);
        const role = discussion?.local_relation || discussion?.role;
        createGroupTab(`history-${discussionId}`, title, discussion?.participant_ids || [], { discussionId, readOnly, role, groupTitle: title });
    }, [activateTab, createGroupTab, getTabList, getTabState, lang, saveTabState]);

    useEffect(() => {
        if (!pendingVEOpen) return;
        const participantId = String(pendingVEOpen.machine_id || pendingVEOpen.id || '').trim();
        createVETab(participantId || pendingVEOpen.id, pendingVEOpen.name, undefined, pendingVEOpen.online_status, pendingVEOpen.avatar_data_url);
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
        const { projectPath, taskTitle, initialMessage, autoSend, prepareMode } = pendingProjectTabOpen;
        onProjectTabHandledRef.current?.();

        // Check if the tab already exists in the tab list BEFORE creating it.
        // This is a synchronous read of tabStateRef — reliable for tabs that were
        // restored from localStorage (synchronous on mount) or from the backend
        // index merge (async, but completes before user interaction in practice).
        // Combined with the post-creation history check, this provides two-layer
        // protection against duplicate autoSend.
        const tabExistedInList = hasProjectTabRef.current?.(projectPath) ?? false;

        const tab = createProjectTabRef.current(projectPath, taskTitle, { prepareMode });
        if (!tab) return;
        console.info("[usePendingAssistantTabOpen] project tab opened", {
            projectPath,
            tabId: tab.id,
            taskTitle,
            autoSend: !!autoSend,
            tabExistedInList,
        });

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
