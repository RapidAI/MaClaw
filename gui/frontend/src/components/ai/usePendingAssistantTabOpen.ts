import { useCallback, useEffect, useRef } from "react";
import type { CreateGroupTabOptions, CreateProjectTabOptions, CreateVETabOptions } from "./useAITabManager";
import type { AITab, AITabState } from "./AITabTypes";
import type { VirtualEmployeeEntry } from "./VirtualEmployeeTab";
import type { ExpertDefinition } from "./expertTypes";
import { expertTabId, expertWelcomeMessageText } from "./expertTypes";
import { expertSessionKey } from "./aiAssistantPanelSessionUtils";
import { isHistoryDiscussionReadOnly } from "./historyDiscussionUtils";
import { isLocalHumanParticipantId } from "./localAIIdentity";
import { addParticipantIdentityKeys } from "./participantIdentity";
import type { CodingTaskLaunch } from "./codingTaskLaunch";

/** Pending project tab open request from external (e.g. sidebar "create task") */
export type PendingProjectTabOpen = CodingTaskLaunch;

/** Pending expert tab open request (e.g. clicking an expert card on the utilities page). */
export interface PendingExpertOpen {
    expert: ExpertDefinition;
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
    if (ids.length === 1) return ids[0];
    // When participant_ids contains both local machine ID (m_xxx) and remote VE
    // (ve_emp_xxx / ve-xxx / ve_xxx), identify VE participants by prefix.
    if (ids.length > 1) {
        const veIds = ids.filter(id => {
            const lower = id.toLowerCase();
            return lower.startsWith("ve_emp_") || lower.startsWith("ve-") || lower.startsWith("ve_");
        });
        if (veIds.length === 1) return veIds[0];
    }
    return "";
}

function isConversationMessage(value: unknown): value is { role?: unknown } {
    return !!value && typeof value === "object";
}

const textForPendingTabLang = (lang: string | undefined, en: string, zhHans: string, zhHant = zhHans): string => (
    lang === "zh-Hant" ? zhHant : lang?.startsWith("zh") || !lang ? zhHans : en
);

function newTaskContextMessage(
    launch: Pick<CodingTaskLaunch, "agentMode" | "remoteHost" | "newTaskContext">,
    lang?: string,
) {
    const context = launch.newTaskContext;
    if (!context || context.kind !== "new-task") return null;
    const t = (en: string, zhHans: string, zhHant = zhHans) => textForPendingTabLang(lang, en, zhHans, zhHant);
    const type = launch.agentMode === "remote_coding_dev"
        ? t("Remote coding", "\u8fdc\u7a0b\u7f16\u7a0b", "\u9060\u7aef\u7a0b\u5f0f")
        : launch.agentMode === "coding_dev"
            ? t("Local coding", "\u672c\u5730\u7f16\u7a0b", "\u672c\u6a5f\u7a0b\u5f0f")
            : t("Chat", "\u5bf9\u8bdd", "\u5c0d\u8a71");
    const lines = [
        `${t("Type", "\u7c7b\u578b", "\u985e\u578b")}\uff1a${type}`,
        launch.agentMode === "remote_coding_dev" && launch.remoteHost
            ? `${t("Remote host", "\u8fdc\u7a0b\u4e3b\u673a", "\u9060\u7aef\u4e3b\u6a5f")}\uff1a${launch.remoteHost}`
            : "",
        launch.agentMode === "remote_coding_dev" && context.remoteUser
            ? `${t("User", "\u7528\u6237", "\u4f7f\u7528\u8005")}\uff1a${context.remoteUser}`
            : "",
        launch.agentMode === "remote_coding_dev" && context.remotePort
            ? `${t("Port", "\u7aef\u53e3", "\u9023\u63a5\u57e0")}\uff1a${context.remotePort}`
            : "",
        launch.agentMode === "remote_coding_dev" && context.remoteWorkDir
            ? `${t("Remote working directory", "\u8fdc\u7a0b\u5de5\u4f5c\u76ee\u5f55", "\u9060\u7aef\u5de5\u4f5c\u76ee\u9304")}\uff1a${context.remoteWorkDir}`
            : context.workingDir
                ? `${t("Working directory", "\u5de5\u4f5c\u76ee\u5f55", "\u5de5\u4f5c\u76ee\u9304")}\uff1a${context.workingDir}`
                : `${t("Working directory", "\u5de5\u4f5c\u76ee\u5f55", "\u5de5\u4f5c\u76ee\u9304")}\uff1a${t("Default workspace", "\u9ed8\u8ba4\u5de5\u4f5c\u533a", "\u9810\u8a2d\u5de5\u4f5c\u5340")}`,
        t(
            "Task is ready. Enter your request below; this information is not sent to AI.",
            "\u4efb\u52a1\u5df2\u5c31\u7eea\uff0c\u8bf7\u5728\u4e0b\u65b9\u8f93\u5165\u4efb\u52a1\u547d\u4ee4\u3002\u6b64\u4fe1\u606f\u4e0d\u4f1a\u53d1\u9001\u7ed9 AI\u3002",
            "\u4efb\u52d9\u5df2\u5c31\u7dd2\uff0c\u8acb\u5728\u4e0b\u65b9\u8f38\u5165\u4efb\u52d9\u547d\u4ee4\u3002\u6b64\u8cc7\u8a0a\u4e0d\u6703\u50b3\u9001\u7d66 AI\u3002",
        ),
    ].filter(Boolean);
    return {
        id: `task-context-${Date.now()}`,
        role: "system" as const,
        kind: "taskContext" as const,
        content: lines.join("\n"),
        timestamp: Date.now(),
    };
}
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
    createVETab: (veId: string, veName: string, sessionId?: string, onlineStatus?: "online" | "offline", avatarDataURL?: string, veSkillDescription?: string, options?: CreateVETabOptions) => AITab | null;
    createGroupTab: (id: string, title: string, participants: string[], options?: CreateGroupTabOptions) => AITab | null;
    createProjectTab: (projectPath: string, taskTitle: string, options?: CreateProjectTabOptions) => AITab | null;
    createExpertTab?: (expert: ExpertDefinition) => AITab | null;
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
    pendingExpertOpen?: PendingExpertOpen | null;
    onPendingExpertOpenHandled?: () => void;
    /** Persist the expert in task management before its tab is opened. */
    onEnsureExpertTask?: (expert: ExpertDefinition) => Promise<void> | void;
}

export function usePendingAssistantTabOpen({
    lang,
    createVETab,
    createGroupTab,
    createProjectTab,
    createExpertTab,
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
    pendingExpertOpen,
    onPendingExpertOpenHandled,
    onEnsureExpertTask,
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

            // Participant identity is not enough to identify a historical row:
            // the same VE can have several discussions, each with a distinct ID.
        }

        const title = readableHistoryDiscussionTitle(discussion, discussionId, lang);
        const role = discussion?.local_relation || discussion?.role;

        // For continuable 1:1 discussions (non-read-only with a single VE participant),
        // open as a live VE tab so the full-featured input area is rendered instead of
        // the simplified history textarea. This matches the user expectation that
        // "我发起 - 可继续讨论" sessions behave identically to active sessions.
        if (!readOnly) {
            const singleVE = singlePendingParticipantId(discussion);
            if (singleVE) {
                const veTab = createVETab(singleVE, title, discussionId, undefined, undefined, undefined, { allowIdentityReuse: false });
                if (veTab) return;
                // Tab limit reached — fall through to createGroupTab as degraded fallback.
            }
        }

        createGroupTab(`history-${discussionId}`, title, discussion?.participant_ids || [], { discussionId, readOnly, role, groupTitle: title });
    }, [activateTab, createGroupTab, createVETab, getTabList, getTabState, lang, saveTabState]);

    useEffect(() => {
        if (!pendingVEOpen) return;
        const participantId = String(pendingVEOpen.machine_id || pendingVEOpen.id || '').trim();
        createVETab(
            participantId || pendingVEOpen.id,
            pendingVEOpen.name,
            undefined,
            pendingVEOpen.online_status,
            pendingVEOpen.avatar_data_url,
            pendingVEOpen.skill_description,
        );
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
    const saveTabStateRef = useRef(saveTabState);
    saveTabStateRef.current = saveTabState;
    const hasProjectTabRef = useRef(hasProjectTab);
    hasProjectTabRef.current = hasProjectTab;

    useEffect(() => {
        if (!pendingProjectTabOpen) return;

        // Capture request data and clear pending state synchronously.
        // The guard above prevents re-entry when pending becomes null.
        const { projectPath, taskTitle, initialMessage, autoSend, prepareMode, agentMode, remoteHost, remoteSafety, remoteNeedsReconnect, imPlatform, imTargetUID, imIsGroup, newTaskContext } = pendingProjectTabOpen;
        onProjectTabHandledRef.current?.();

        // Check if the tab already exists in the tab list BEFORE creating it.
        // This is a synchronous read of tabStateRef — reliable for tabs that were
        // restored from localStorage (synchronous on mount) or from the backend
        // index merge (async, but completes before user interaction in practice).
        // Combined with the post-creation history check, this provides two-layer
        // protection against duplicate autoSend.
        const tabExistedInList = hasProjectTabRef.current?.(projectPath) ?? false;

        const tab = createProjectTabRef.current(projectPath, taskTitle, { prepareMode, agentMode, remoteHost, remoteSafety, remoteNeedsReconnect });
        if (!tab) return;
        const initialState = getTabStateRef.current?.(tab.id);
        const existingHistory = Array.isArray(initialState?.history) ? initialState.history : [];
        const hasExistingConversation = existingHistory.some((m) => isConversationMessage(m) && (m.role === "user" || m.role === "assistant"));
        // A just-closed tab can retain its local state until persistence catches
        // up. Keep the creation receipt one-shot when that tab is reopened.
        const hasExistingTaskContext = existingHistory.some((m) => (
            isConversationMessage(m) && (m as { kind?: unknown }).kind === "taskContext"
        ));
        const taskContext = newTaskContextMessage({ agentMode, remoteHost, newTaskContext }, lang);
        // A task-management creation opens a prepared workspace for the user's
        // next deliberate prompt. Keep that invariant even if an upstream event
        // accidentally carries autoSend: true alongside its local task receipt.
        const shouldAutoSend = autoSend === true && !newTaskContext;
        const shouldAddTaskContext = !tabExistedInList && !hasExistingConversation && !hasExistingTaskContext && !!taskContext;
        const stateAfterTaskContext = shouldAddTaskContext && taskContext
            ? {
                ...initialState,
                history: [...(Array.isArray(initialState?.history) ? initialState.history : []), taskContext],
                scrollTop: 0,
                inputText: "",
                lastActiveAt: Date.now(),
            }
            : initialState;
        if (shouldAddTaskContext) {
            saveTabStateRef.current?.(tab.id, stateAfterTaskContext || {});
        }
        // A duplicate/stale event can focus a pre-existing task tab. Do not
        // attach its original IM completion route to that tab: the next manual
        // message would otherwise send an unrelated result back to IM.
        // IM launches can defer their initial prompt until reconnect. A newly
        // created task never carries a hidden initial prompt to send later.
        const shouldDeferRemoteInitialSend = shouldAutoSend && agentMode === "remote_coding_dev" && !!remoteNeedsReconnect;
        if (!newTaskContext && !tabExistedInList && !hasExistingConversation && (imPlatform && imTargetUID || shouldDeferRemoteInitialSend)) {
            saveTabStateRef.current?.(tab.id, {
                ...stateAfterTaskContext,
                ...(imPlatform && imTargetUID ? {
                    pendingIMCompletion: {
                        platform: imPlatform,
                        targetUID: imTargetUID,
						isGroup: !!imIsGroup,
                        taskTitle,
                    },
                } : {}),
                ...(shouldDeferRemoteInitialSend ? { pendingRemoteInitialMessage: { text: initialMessage || taskTitle } } : {}),
            });
        }
        console.info("[usePendingAssistantTabOpen] project tab opened", {
            projectPath,
            tabId: tab.id,
            taskTitle,
            autoSend: shouldAutoSend,
            agentMode: agentMode || null,
            remoteHost: remoteHost || null,
            remoteNeedsReconnect: !!remoteNeedsReconnect,
            tabExistedInList,
        });

        // Async operations use refs — no stale closure, no dependency churn.
        (async () => {
            if (!shouldAutoSend) return;
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
            if (hasExistingConversation || tabExistedInList) return;

            // The remote panel flushes this prompt only after its SSH workbench
            // has reconnected successfully.
            if (shouldDeferRemoteInitialSend) return;

            const msg = initialMessage || taskTitle;
            await send(msg, { tabId: tab.id, project_path: tab.projectPath, im_platform: imPlatform, im_target_uid: imTargetUID, im_is_group: !!imIsGroup, im_task_title: taskTitle }).catch(() => {});
        })();
    }, [pendingProjectTabOpen]);
    // ↑ ONLY pendingProjectTabOpen in deps. All callbacks accessed via refs.
    // This ensures the effect fires exactly once per non-null pendingProjectTabOpen,
    // regardless of how often the parent re-renders.

    // --- Expert Tab: open (or focus) an expert conversation tab ---
    // First creation seeds a local welcome message (no LLM call); re-opening an
    // existing tab only activates it and never re-seeds.
    const createExpertTabRef = useRef(createExpertTab);
    createExpertTabRef.current = createExpertTab;
    const saveTabStateForExpertRef = useRef(saveTabState);
    saveTabStateForExpertRef.current = saveTabState;
    const getTabListForExpertRef = useRef(getTabList);
    getTabListForExpertRef.current = getTabList;
    const onExpertHandledRef = useRef(onPendingExpertOpenHandled);
    onExpertHandledRef.current = onPendingExpertOpenHandled;
    const ensureExpertTaskRef = useRef(onEnsureExpertTask);
    ensureExpertTaskRef.current = onEnsureExpertTask;
    /** Reject a stale async registration when a newer expert launch wins. */
    const expertOpenRequestRef = useRef(0);
    const expertLangRef = useRef(lang);
    expertLangRef.current = lang;

    useEffect(() => {
        if (!pendingExpertOpen) return;
        const expert = pendingExpertOpen.expert;
        onExpertHandledRef.current?.();
        const expertId = String(expert?.id || "").trim();
        if (!expertId) return;
        const requestID = ++expertOpenRequestRef.current;

        const openExpertTab = () => {
            if (requestID !== expertOpenRequestRef.current) return;
            const create = createExpertTabRef.current;
            if (!create) return;

            // Check existence BEFORE creating so the welcome seed only happens on
            // first creation (dedupe-activation must not re-seed).
            const tabId = expertTabId(expertId);
            const existedBefore = (getTabListForExpertRef.current?.() || [])
                .some(t => t.id === tabId || (t.type === "expert" && t.expertId === expertId));

            const tab = create(expert);
            if (!tab || existedBefore) return;

            const existing = getTabStateRef.current?.(tab.id);
            const hasConversation = Array.isArray(existing?.history)
                && existing.history.some((m) => isConversationMessage(m) && (m.role === "user" || m.role === "assistant"));
            if (hasConversation) return;

            const sessionKey = expertSessionKey(expertId) || undefined;
            saveTabStateForExpertRef.current?.(tab.id, {
                history: [{
                    id: `expert-welcome-${expertId}`,
                    role: "assistant",
                    content: expertWelcomeMessageText(expert, expertLangRef.current),
                    sessionKey,
                    timestamp: Date.now(),
                }],
                scrollTop: 0,
                inputText: "",
                lastActiveAt: Date.now(),
            });
        };
        const ensureTask = ensureExpertTaskRef.current;
        if (!ensureTask) {
            openExpertTab();
            return;
        }
        void Promise.resolve(ensureTask(expert)).then(openExpertTab).catch((error) => {
            // Task management is the durable entry point for experts. Do not
            // open a tab that cannot be reached again from the sidebar.
            console.error("[task_management] create expert task failed:", error);
        });
    }, [pendingExpertOpen]);
    // ↑ ONLY pendingExpertOpen in deps. All callbacks accessed via refs.
}
