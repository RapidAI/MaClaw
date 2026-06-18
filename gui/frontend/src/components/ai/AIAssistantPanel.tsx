import { useState, useRef, useCallback, useEffect, useMemo } from "react";
import type { ChatMessage } from "./useAIAssistant";
import { findLastIndex, isPinnedNewsMessage, isImageFilePath, buildOutgoingMessageMulti, setActiveSessionKey, getActiveSessionKey, forgetAIAssistantSessionRounds } from "./useAIAssistant";
import { useVoiceInput, type VoiceInputSource } from "./useVoiceInput";
import { useWorkflowState, type WorkflowUIState } from "./useWorkflowState";
import { useCodePreviewState, type CodePreviewUIState } from "./useCodePreviewState";
import { useBufferQueue } from "./useBufferQueue";
import type { AttachmentInfo } from "./useBufferQueue";
import { renderMessage } from "./aiAssistantMarkdown";
import { darkTheme, lightTheme, maximizedInlineStyle, overlayStyle, overlayTheme } from "./aiAssistantPanelTheme";
import "./ensureAIAssistantPanelStyles";
import { localizeText } from "./aiAssistantI18n";
import { ProjectSearchPanel, useProjectSearch } from "./ProjectSearchPanel";
import { useTTSReadback } from "./useTTSReadback";
import { useAIAssistantVoiceControls } from "./useAIAssistantVoiceControls";
import { useAssistantOutputScroll } from "./useAssistantOutputScroll";
import { useResizableAssistantInput } from "./useResizableAssistantInput";
import { useAssistantInputHistory } from "./useAssistantInputHistory";
import { usePastedImageAttachments } from "./usePastedImageAttachments";
import { useAssistantPreviewResize } from "./useAssistantPreviewResize";
import { getAssistantInitLabel } from "./aiAssistantStatusLabels";
import { AssistantConversationBody } from "./AssistantConversationBody";
import { AssistantTitleBar } from "./AssistantTitleBar";
import { KnowledgeDialog } from "./KnowledgeDialog";
import { AssistantInputStack } from "./AssistantInputStack";
import { AssistantWelcomeView } from "./AssistantWelcomeView";
import { AssistantWorkflowMaximizeSuggestion } from "./AssistantWorkflowMaximizeSuggestion";
import { useAssistantThemeMode } from "./useAssistantThemeMode";
import { AssistantPreviewPane } from "./AssistantPreviewPane";
import { activeCodingAgentProgress, codingAgentCompactText, latestCodingAgentTurnSnapshot } from "./CodingAgentProgressStatus";
import { findLatestToolProgressText, formatToolProgressStatus, isToolProgressMessage } from "./aiAssistantProgressUtils";
import { AITabBar } from "./AITabBar";
import { getAITabDisplayTitle } from "./AITabItem";
import type { AITab } from "./AITabTypes";
import { useAITabManager } from "./useAITabManager";
import { looksLikeRawParticipantId } from "./localAIIdentity";
import { useAddGroupParticipantToTab } from "./useAddGroupParticipantToTab";
import { useAddLocalMaclawToTab } from "./useAddLocalMaclawToTab";
import { useProjectContextLoader } from "./useProjectContextLoader";
import { AssistantActiveTabContent } from "./AssistantActiveTabContent";
import { AssistantDragHandle } from "./AssistantDragHandle";
import { usePendingAssistantTabOpen } from "./usePendingAssistantTabOpen";
import type { PendingProjectTabOpen } from "./usePendingAssistantTabOpen";
import type { AIAssistantPanelProps } from "./aiAssistantPanelTypes";
import { loadProjectTabMsgIds, mergeChatMessages, PROJECT_TAB_MSG_IDS_KEY, withoutProjectContextMessages } from "./aiAssistantProjectTabState";
import { compactCodingAgentProgressMessages } from "./compactCodingAgentProgressMessages";
import { TabParticipantInviteDialog } from "./TabParticipantInviteDialog";
import { AIAssistantRenameGroupDialog } from "./AIAssistantRenameGroupDialog";
import { buildProjectTabRecentMessages, chatHistoriesEquivalent, logAIPanelDiagnostic, messageBelongsToSession, messageBelongsToSessionOrLegacy, messageIsLocalSession, projectPathFromSessionKey, projectSessionKey } from "./aiAssistantPanelSessionUtils";
import { GroupDiscussionRenameConsultation, LoadConfig, PatchConfigFields } from "../../../wailsjs/go/main/App";
import { EventsOff, EventsOn } from "../../../wailsjs/runtime";
import { EVENT_PROJECT_TASK_CLOSED } from "../../constants/events";
import { getWailsAppModule } from "../../utils/wailsAppModule";
export { isHistoryDiscussionReadOnly } from "./historyDiscussionUtils";

const LOCAL_CODING_PREVIEW_EVENT_SCOPE = "__maclaw_local_coding_preview__";
export function canShowAssistantCodingPreviewForTab(tab: Pick<AITab, "type"> | null | undefined): boolean { return tab?.type === "local" || tab?.type === "project"; }

export function AIAssistantPanel(props: AIAssistantPanelProps & any) {
    const { onClose, lang, chatFontSize = 14, themeMode: controlledThemeMode, onThemeModeChange, audioInputDeviceId, audioOutputDeviceId, petVoiceStartSeq = 0, petFocusInputSeq = 0, pendingVEOpen, onPendingVEOpenHandled, pendingHistoryDiscussionOpen, onPendingHistoryDiscussionOpenHandled, appUpdateAvailable, onOpenAppUpdate, onDismissAppUpdate } = props;
    const state = props.state || props;
    const actions = props.actions || props;
    const panelWindow = props.window || props;
    const { messages, progressMessages = [], sending, sendingSessionKey: rawSendingSessionKey, busySessionKeys: rawBusySessionKeys, streaming, streamingSessionKey: rawStreamingSessionKey, streamingSessionKeys: rawStreamingSessionKeys, visualBusy, ready, initStatus, selectedFilePath: selectedFilePathFromState = "", submittedPrompts = [], draftInputValue = "", trialReflectEnabled = false, scrollToTopSeq, onboardingIncomplete, showTraceEntry = false, agentView = null } = state;
    const { browseFile, clearSelectedFile, removeSelectedFile, sendMessage, sendBtwMessage, injectSupplementary, guideLaunchReference, clearHistory, recordSubmittedPrompt, setDraftInputValue, executeAction, refreshNews, onOpenOnboarding, cancelSession, onOpenTutorial, onTaskPrefsChanged, submitAgentView, dismissAgentView } = actions;
    const selectedFilePaths = Array.isArray(state.selectedFilePaths) ? state.selectedFilePaths : (selectedFilePathFromState ? [selectedFilePathFromState] : []);
    const selectedFilePath = selectedFilePaths[0] || "";
    const { inline, maximized = false, onToggleMaximize, onHideWindow } = panelWindow || {};
    const [localDraftInputValue, setLocalDraftInputValue] = useState(draftInputValue);
    const [cancelPending, setCancelPending] = useState(false);
    const [editingEntryId, setEditingEntryId] = useState<string | null>(null);
    const [queueEditDraftActive, setQueueEditDraftActive] = useState(false);
    const [knowledgeDialogOpen, setKnowledgeDialogOpen] = useState(false);
    const [workflowEnabled, setWorkflowEnabled] = useState(false);
    const inputRef = useRef<HTMLTextAreaElement | null>(null);
    const cancelRestoreSeqRef = useRef(0);
    const closeAllPreviewPanelsRef = useRef<(() => void) | null>(null);
    const { themeMode, setThemeMode } = useAssistantThemeMode(controlledThemeMode, onThemeModeChange);
    const { ttsEnabled, setTtsEnabled, ttsPlaying } = useTTSReadback(audioOutputDeviceId);
    const t = themeMode === 'dark' ? darkTheme : (inline ? lightTheme : overlayTheme);
    const showMaximizeToggle = inline && !!onToggleMaximize;
    // Workflow toggle: load initial state from config, sync on config-changed event
    useEffect(() => {
        LoadConfig().then((cfg) => {
            setWorkflowEnabled(cfg?.workflow_enabled === true);
        }).catch(() => { /* ignore */ });
        const off = EventsOn("config-changed", (cfg: any) => {
            if (cfg && typeof cfg.workflow_enabled === "boolean") {
                setWorkflowEnabled(cfg.workflow_enabled);
            } else if (cfg && cfg.workflow_enabled === undefined) {
                setWorkflowEnabled(false);
            }
        });
        return () => { if (typeof off === "function") off(); };
    }, []);
    const handleToggleWorkflow = useCallback(() => {
        setWorkflowEnabled(prev => {
            const next = !prev;
            PatchConfigFields({ workflow_enabled: next }).then((saved) => {
                setWorkflowEnabled(saved?.workflow_enabled === true);
            }).catch(() => {
                // Revert to actual backend state on failure
                LoadConfig().then(cfg => {
                    setWorkflowEnabled(cfg?.workflow_enabled === true);
                }).catch(() => {
                    setWorkflowEnabled(!next); // last resort: toggle back
                });
            });
            return next;
        });
    }, []);
    const { tabState, activeTab, activateTab, createVETab, createGroupTab, createProjectTab, closeTab, clearTabConversation, saveTabState, getTabState, getLastActiveAt, getTabs, hasProjectTab, upgradeVETabToGroup, renameGroupTab, tabLimitError, clearTabLimitError } = useAITabManager();
    const [renameGroupTargetTabId, setRenameGroupTargetTabId] = useState<string | null>(null);
    const [renameGroupValue, setRenameGroupValue] = useState("");
    const [renameGroupError, setRenameGroupError] = useState("");
    const [renameGroupSaving, setRenameGroupSaving] = useState(false);
    const renameGroupTargetTab = renameGroupTargetTabId ? tabState.tabs.find(tab => tab.id === renameGroupTargetTabId && tab.type === "group" && !tab.readOnly) : undefined;
    const openRenameGroupDialog = useCallback((tab: typeof tabState.tabs[number]) => {
        if (tab.type !== "group" || tab.readOnly) return;
        setRenameGroupTargetTabId(tab.id);
        setRenameGroupValue(getAITabDisplayTitle(tab, lang));
        setRenameGroupError("");
    }, [lang]);
    const closeRenameGroupDialog = useCallback(() => {
        setRenameGroupTargetTabId(null);
        setRenameGroupValue("");
        setRenameGroupError("");
        setRenameGroupSaving(false);
    }, []);
    const submitRenameGroupDialog = useCallback(async () => {
        if (!renameGroupTargetTab || renameGroupSaving) return;
        const title = renameGroupValue.trim();
        if (!title) {
            setRenameGroupError(localizeText(lang, "Group name cannot be empty", "群名不能为空", "群名不能為空"));
            return;
        }
        if (title.length > 60) {
            setRenameGroupError(localizeText(lang, "Group name must be 60 characters or fewer", "群名不能超过 60 个字符", "群名不能超過 60 個字元"));
            return;
        }
        setRenameGroupSaving(true);
        try {
            if (renameGroupTargetTab.discussionId) {
                await GroupDiscussionRenameConsultation(renameGroupTargetTab.discussionId, title);
            }
            renameGroupTab(renameGroupTargetTab.id, title);
            closeRenameGroupDialog();
        } catch (error) {
            setRenameGroupSaving(false);
            setRenameGroupError(error instanceof Error ? error.message : String(error || localizeText(lang, "Failed to rename group", "修改群名失败", "修改群名失敗")));
        }
    }, [closeRenameGroupDialog, lang, renameGroupSaving, renameGroupTab, renameGroupTargetTab, renameGroupValue]);
    const clearActiveHistory = useCallback(async () => {
        // Close all right-side preview panels (workflow doc, code preview, agent view)
        closeAllPreviewPanelsRef.current?.();
        if (activeTab.type === "project") {
            clearTabConversation(activeTab.id);
            setProjectTabMessages([]);
            return;
        }
        if (activeTab.type === "ve" || (activeTab.type === "group" && !!activeTab.veId)) {
            clearTabConversation(activeTab.id);
            return;
        }
        // Reset queue interaction state so the welcome view can reappear.
        setQueueInteractionStarted(false);
        setQueueEditDraftActive(false);
        setEditingEntryId(null);
        await clearHistory();
    }, [activeTab.id, activeTab.type, activeTab.veId, clearHistory, clearTabConversation]);
    const isLocalTabActive = activeTab.id === "local";
    const isProjectTabActive = activeTab.type === "project";
    const showChatUI = isLocalTabActive || isProjectTabActive;
    const activeSessionKey = isProjectTabActive && activeTab.projectPath ? `desktop-user:${activeTab.projectPath}` : 'desktop-user';
    const { handlePaste, pendingAttachments, setPendingAttachments } = usePastedImageAttachments(activeSessionKey);
    const { queue, addEntry, removeEntry, updateEntry, reorderEntry, extractEntry } = useBufferQueue(activeSessionKey);
    const firingEntryIdsRef = useRef<Set<string>>(new Set());
    const drainingEntryIdsRef = useRef<Set<string>>(new Set());
    const [queueInFlightVersion, setQueueInFlightVersion] = useState(0);
    const [queueInteractionStarted, setQueueInteractionStarted] = useState(false);
    const refreshQueueInFlight = useCallback(() => setQueueInFlightVersion(version => version + 1), []);
    useEffect(() => {
        setActiveSessionKey(activeSessionKey);
        return () => {
            if (getActiveSessionKey() === activeSessionKey) setActiveSessionKey('');
        };
    }, [activeSessionKey]);
    const [projectTabMessages, setProjectTabMessages] = useState<ChatMessage[]>([]);
    const [projectTabRouteVersion, setProjectTabRouteVersion] = useState(0);
    const [panelSendInFlightSessionKeys, setPanelSendInFlightSessionKeys] = useState<Set<string>>(() => new Set());
    const markPanelSendInFlight = useCallback((sessionKey: string, inFlight: boolean) => {
        const key = String(sessionKey || '').trim();
        if (!key) return;
        setPanelSendInFlightSessionKeys(prev => {
            const has = prev.has(key);
            if (has === inFlight) return prev;
            const next = new Set(prev);
            if (inFlight) next.add(key);
            else next.delete(key);
            return next;
        });
    }, []);
    const [preparingProjectTabIds, setPreparingProjectTabIds] = useState<Set<string>>(() => new Set());
    const preparingProjectTabIdsRef = useRef<Set<string>>(new Set());
    const [preparingProjectTabModes, setPreparingProjectTabModes] = useState<Map<string, NonNullable<PendingProjectTabOpen["prepareMode"]>>>(() => new Map());
    const deferredProjectInitialSendsRef = useRef<Map<string, string[]>>(new Map());
    const projectPrepareTimersRef = useRef<Map<string, number>>(new Map());
    const sendMessageForTabRef = useRef<((text: string, options?: Record<string, unknown>) => Promise<boolean>) | null>(null);
    const activeTabIdRef = useRef<string>(activeTab.id);
    const activeTabRef = useRef(activeTab);
    activeTabIdRef.current = activeTab.id;
    activeTabRef.current = activeTab;
    useEffect(() => () => {
        for (const timer of projectPrepareTimersRef.current.values()) {
            window.clearTimeout(timer);
        }
        projectPrepareTimersRef.current.clear();
    }, []);
    const prevActiveTabIdRef = useRef<string>(activeTab.id);
    const previewStateMapRef = useRef<Map<string, { workflow: WorkflowUIState; code: CodePreviewUIState; previewMode: "workflow" | "code" }>>(new Map());
    const previewOwnerTabRef = useRef<string>(canShowAssistantCodingPreviewForTab(activeTab) ? activeTab.id : "local");
    const previewOwnerResetPendingRef = useRef(false);
    const agentViewOwnerTabRef = useRef<string>("");
    useEffect(() => {
        const prevTabId = prevActiveTabIdRef.current;
        const currentTabId = activeTab.id;
        if (prevTabId === currentTabId) return;
        const multipleTabsExist = tabState.tabs.length > 1;
        if (multipleTabsExist) {
            const currentTabCanOwnPreview = canShowAssistantCodingPreviewForTab(activeTab);
            const currentPreviewMode: "workflow" | "code" = codePreviewState.active ? "code" : "workflow";
            const ownerTabId = previewOwnerTabRef.current;
            const ownerTab = tabState.tabs.find(t => t.id === ownerTabId);

            if (canShowAssistantCodingPreviewForTab(ownerTab) && ownerTabId !== currentTabId) {
                previewStateMapRef.current.set(ownerTabId, {
                    workflow: getWorkflowSnapshot(),
                    code: { ...codePreviewState, files: new Map(codePreviewState.files) },
                    previewMode: currentPreviewMode,
                });
            }

            if (currentTabCanOwnPreview && ownerTabId !== currentTabId) {
                const savedState = previewStateMapRef.current.get(currentTabId);
                if (savedState) {
                    restoreWorkflowState(savedState.workflow);
                    restoreCodePreviewState(savedState.code);
                } else {
                    resetWorkflowState();
                    resetCodePreviewState();
                }
                previewOwnerTabRef.current = currentTabId;
            }
        }

        const prevTab = tabState.tabs.find(t => t.id === prevTabId);
        if (prevTab && prevTab.type === "project") {
            const scrollTop = outputContainerRef.current?.scrollTop || 0;
            let historyToSave = projectTabMessages;
            const prevRound = findProjectRoundForTab(prevTabId, prevTab.projectPath);
            if (sending && prevRound) {
                const prevSessionKey = projectSessionKey(prevTab.projectPath);
                const inFlightMessages = prevSessionKey
                    ? messages.slice(prevRound.baseline).filter((message: ChatMessage) => messageBelongsToSessionOrLegacy(message, prevSessionKey))
                    : [];
                if (inFlightMessages.length > 0) {
                    const existingIds = new Set(projectTabMessages.map((m: ChatMessage) => m.id));
                    const unique = inFlightMessages.filter((m: ChatMessage) => !existingIds.has(m.id));
                    if (unique.length > 0) {
                        historyToSave = [...projectTabMessages, ...unique];
                    }
                }
            }

            saveTabState(prevTabId, {
                history: historyToSave,
                scrollTop,
                inputText: localDraftInputValue,
            });
        }
        if (activeTab.type === "project") {
            const restored = getTabState(currentTabId);
            const hasPendingRoundForTab = !!findProjectRoundForTab(currentTabId, activeTab.projectPath);
            if (!sending && !hasPendingRoundForTab) {
                projectTabRoundsRef.current.clear();
            }
            if (restored) {
                setProjectTabMessages((restored.history || []) as ChatMessage[]);
                setLocalDraftInputValue(restored.inputText || "");
                requestAnimationFrame(() => {
                    if (outputContainerRef.current && restored.scrollTop) {
                        outputContainerRef.current.scrollTop = restored.scrollTop;
                    }
                });
            } else {
                setProjectTabMessages([]);
                setLocalDraftInputValue("");
            }

        } else if (activeTab.id === "local") {
            setLocalDraftInputValue(draftInputValue);
        }
        prevActiveTabIdRef.current = currentTabId;
    }, [activeTab.id]); // eslint-disable-line react-hooks/exhaustive-deps
    // Track which tab owns the agentView — when agentView is set, record the
    // current active tab as its owner. Only show agentView when owning tab is active.
    useEffect(() => {
        if (agentView) {
            agentViewOwnerTabRef.current = activeTab.id;
        }
    }, [agentView]); // eslint-disable-line react-hooks/exhaustive-deps
    const projectTabRoundSeqRef = useRef(0);
    const projectTabRoundsRef = useRef<Map<string, { tabId: string | null; projectPath: string; baseline: number; seq: number }>>(new Map());
    const findProjectRoundForTab = useCallback((tabId: string, projectPath?: string | null) => {
        const sessionKey = projectSessionKey(projectPath);
        if (sessionKey) {
            const byPath = projectTabRoundsRef.current.get(sessionKey);
            if (byPath && (byPath.tabId === tabId || byPath.projectPath === projectPath)) return byPath;
        }
        for (const round of projectTabRoundsRef.current.values()) {
            if (round.tabId === tabId) return round;
        }
        return undefined;
    }, []);
    const detachedProjectRoundsRef = useRef<Map<string, { tabId: string; messageIds: Set<string> }>>(new Map());
    const [detachedProjectRoundVersion, setDetachedProjectRoundVersion] = useState(0);
    const projectTabMsgIdsRef = useRef<Set<string>>(null!);
    if (!projectTabMsgIdsRef.current) {
        projectTabMsgIdsRef.current = loadProjectTabMsgIds();
    }
    const projectTabIdsPersistTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const persistProjectTabMsgIds = useCallback(() => {
        if (projectTabIdsPersistTimerRef.current) clearTimeout(projectTabIdsPersistTimerRef.current);
        projectTabIdsPersistTimerRef.current = setTimeout(() => {
            projectTabIdsPersistTimerRef.current = null;
            try {
                const ids = projectTabMsgIdsRef.current;
                if (ids.size === 0) {
                    localStorage.removeItem(PROJECT_TAB_MSG_IDS_KEY);
                } else {
                    const arr = [...ids];
                    const toStore = arr.length > 200 ? arr.slice(-200) : arr;
                    localStorage.setItem(PROJECT_TAB_MSG_IDS_KEY, JSON.stringify(toStore));
                }
            } catch { /* ignore */ }
        }, 500);
    }, []);
    useEffect(() => {
        if (detachedProjectRoundsRef.current.size === 0) return;
        let changed = false;
        const latestById = new Map(messages.map((m: ChatMessage) => [m.id, m]));
        for (const [key, detached] of detachedProjectRoundsRef.current) {
            const latestDetachedMessages = [...detached.messageIds]
                .map(id => latestById.get(id))
                .filter((m): m is ChatMessage => !!m);
            if (latestDetachedMessages.length === 0) {
                detachedProjectRoundsRef.current.delete(key);
                changed = true;
                continue;
            }
            const existingState = getTabState(detached.tabId);
            const existingHistory = (Array.isArray(existingState?.history) ? existingState?.history : []) as ChatMessage[];
            const nextById = new Map(existingHistory.map((m: ChatMessage) => [m.id, m]));
            for (const message of latestDetachedMessages) {
                nextById.set(message.id, message);
            }
            const nextHistory = [
                ...existingHistory.map((m: ChatMessage) => nextById.get(m.id) || m),
                ...latestDetachedMessages.filter(message => !existingHistory.some((m: ChatMessage) => m.id === message.id)),
            ];
            saveTabState(detached.tabId, {
                ...existingState,
                history: nextHistory,
            });
            if (activeTabIdRef.current === detached.tabId) {
                setProjectTabMessages(nextHistory);
            }
            const assistantStillPending = latestDetachedMessages.some(message => message.role === "assistant" && !message.content && !message.fields?.length && !message.actions?.length && !message.localFilePath && !message.localFilePaths?.length && !message.thumbnailBase64);
            if (!assistantStillPending) {
                detachedProjectRoundsRef.current.delete(key);
                changed = true;
            }
        }
        if (changed) setDetachedProjectRoundVersion(version => version + 1);
    }, [getTabState, messages, saveTabState]);
    useEffect(() => {
        const projectTabs = getTabs().filter(tab => tab.type === "project" && tab.projectPath);
        if (projectTabs.length === 0) return;
        for (const tab of projectTabs) {
            const sessionKey = `desktop-user:${tab.projectPath}`;
            const liveMessages = messages.filter((message: ChatMessage) => messageBelongsToSession(message, sessionKey));
            if (liveMessages.length === 0) continue;
            const existingState = getTabState(tab.id);
            const nextHistory = mergeChatMessages(existingState?.history, liveMessages);
            if (chatHistoriesEquivalent(existingState?.history as ChatMessage[] | undefined, nextHistory)) continue;
            saveTabState(tab.id, {
                ...existingState,
                history: nextHistory,
            });
            if (activeTabIdRef.current === tab.id) {
                setProjectTabMessages(nextHistory);
            }
            for (const message of liveMessages) {
                projectTabMsgIdsRef.current.add(message.id);
            }
            persistProjectTabMsgIds();
        }
    }, [getTabState, getTabs, messages, persistProjectTabMsgIds, saveTabState]);
    const displayMessages = useMemo(() => {
        if (!isProjectTabActive) {
            if (projectTabRoundsRef.current.size > 0) {
                const earliestProjectBaseline = Math.min(...Array.from(projectTabRoundsRef.current.values()).map(round => round.baseline));
                return messages.filter((message: ChatMessage, index: number) => {
                    if (!messageIsLocalSession(message)) return false;
                    const owner = typeof message.sessionKey === "string" ? message.sessionKey.trim() : "";
                    return !!owner || index < earliestProjectBaseline;
                });
            }
            if (projectTabMsgIdsRef.current.size > 0) {
                return messages.filter((m: ChatMessage) => messageIsLocalSession(m) && !projectTabMsgIdsRef.current.has(m.id));
            }
            return messages.filter(messageIsLocalSession);
        }
        const liveProjectMessages = messages.filter((message: ChatMessage) => messageBelongsToSession(message, activeSessionKey));
        const mergedProjectMessages = liveProjectMessages.length > 0
            ? mergeChatMessages(projectTabMessages, liveProjectMessages)
            : projectTabMessages;
        const activeProjectRound = findProjectRoundForTab(activeTab.id, activeTab.projectPath);
        if (!sending || !activeProjectRound) return mergedProjectMessages;
        // During an active round, also include messages that arrived since the
        // round's baseline and belong to this session but may not carry an
        // explicit sessionKey (messageBelongsToSessionOrLegacy accepts both).
        // Use mergeChatMessages so that messages already present in
        // mergedProjectMessages are *replaced* rather than appended again.
        const roundMessages = messages.slice(activeProjectRound.baseline).filter((message: ChatMessage) => messageBelongsToSessionOrLegacy(message, activeSessionKey));
        if (roundMessages.length === 0) return mergedProjectMessages;
        return mergeChatMessages(mergedProjectMessages, roundMessages);
    }, [activeSessionKey, activeTab.id, activeTab.projectPath, findProjectRoundForTab, isProjectTabActive, messages, projectTabMessages, projectTabRouteVersion, sending]);
    const prevSendingRef = useRef(sending);
    useEffect(() => {
        const wasSending = prevSendingRef.current;
        prevSendingRef.current = sending;
        if (!wasSending && sending && isProjectTabActive && activeTab.projectPath && !findProjectRoundForTab(activeTab.id, activeTab.projectPath)) {
            const sessionKey = projectSessionKey(activeTab.projectPath);
            if (sessionKey) {
                projectTabRoundsRef.current.set(sessionKey, {
                    tabId: activeTab.id,
                    projectPath: activeTab.projectPath,
                    baseline: messages.length,
                    seq: projectTabRoundSeqRef.current,
                });
                setProjectTabRouteVersion(version => version + 1);
            }
        }
        if (wasSending && !sending && projectTabRoundsRef.current.size > 0) {
            const rounds = Array.from(projectTabRoundsRef.current.entries());
            for (const [roundKey, round] of rounds) {
                const roundSessionKey = projectSessionKey(round.projectPath);
                const newMessages = roundSessionKey
                    ? messages.slice(round.baseline).filter((message: ChatMessage) => messageBelongsToSessionOrLegacy(message, roundSessionKey))
                    : [];
                if (newMessages.length > 0 && round.tabId) {
                    // Use mergeChatMessages (not appendUnique) so that messages
                    // whose IDs are already present in the stored history are
                    // *replaced* with the latest version from `messages`.
                    // appendUnique (old code) only added new IDs and silently
                    // dropped updated versions (e.g. a streaming placeholder
                    // that already had its ID written by the live-sync effect
                    // but still held empty content would never get the final
                    // response text, leaving a ghost "思考中..." placeholder
                    // alongside the actual response).
                    if (isProjectTabActive && activeTab.id === round.tabId) {
                        setProjectTabMessages(prev => mergeChatMessages(prev, newMessages));
                    }
                    const existingState = getTabState(round.tabId);
                    // For the active tab, existingState?.history is the persisted
                    // snapshot; projectTabMessages is the live React state. Use
                    // the live state as the base when saving so the persisted copy
                    // does not lag behind what the user is currently seeing.
                    // For inactive tabs we have no in-memory live state, so use
                    // the persisted snapshot directly.
                    const baseHistory = isProjectTabActive && activeTab.id === round.tabId
                        ? projectTabMessages
                        : existingState?.history;
                    saveTabState(round.tabId, {
                        ...existingState,
                        history: mergeChatMessages(baseHistory, newMessages),
                    });
                    for (const m of newMessages) {
                        projectTabMsgIdsRef.current.add(m.id);
                    }
                }
                projectTabRoundsRef.current.delete(roundKey);
            }
            setProjectTabRouteVersion(version => version + 1);
            persistProjectTabMsgIds();
        }
    }, [activeTab.id, activeTab.projectPath, findProjectRoundForTab, getTabState, isProjectTabActive, messages, persistProjectTabMsgIds, projectTabMessages, saveTabState, sending]);
    const { loadProjectContext } = useProjectContextLoader();
    const setProjectTabPreparing = useCallback((tabId: string, preparing: boolean, mode: PendingProjectTabOpen["prepareMode"] = "restore-context") => {
        const currentRef = preparingProjectTabIdsRef.current;
        if (preparing) {
            currentRef.add(tabId);
        } else {
            currentRef.delete(tabId);
        }
        setPreparingProjectTabIds(prev => {
            const has = prev.has(tabId);
            if (has === preparing) return prev;
            const next = new Set(prev);
            if (preparing) next.add(tabId);
            else next.delete(tabId);
            return next;
        });
        setPreparingProjectTabModes(prev => {
            const current = prev.get(tabId);
            if (preparing && current === (mode || "restore-context")) return prev;
            if (!preparing && !prev.has(tabId)) return prev;
            const next = new Map(prev);
            if (preparing) next.set(tabId, mode || "restore-context");
            else next.delete(tabId);
            return next;
        });
    }, []);
    const finishProjectTabPreparing = useCallback((tabId: string, projectPath?: string) => {
        const timer = projectPrepareTimersRef.current.get(tabId);
        if (timer !== undefined) {
            window.clearTimeout(timer);
            projectPrepareTimersRef.current.delete(tabId);
        }
        setProjectTabPreparing(tabId, false);
        const deferred = deferredProjectInitialSendsRef.current.get(tabId) || [];
        deferredProjectInitialSendsRef.current.delete(tabId);
        for (const text of deferred) {
            void sendMessageForTabRef.current?.(text, { tabId, project_path: projectPath });
        }
    }, [setProjectTabPreparing]);
    const createProjectTabWithContext = useCallback((projectPath: string, taskTitle: string, options?: { prepareMode?: PendingProjectTabOpen["prepareMode"] } | boolean) => {
        const tabExisted = hasProjectTab(projectPath);
        const prepareMode = typeof options === "object" ? options.prepareMode : "restore-context";
        const startedAt = performance.now();
        const scheduleNewAgentReady = (readyTab: { id: string; projectPath?: string }, delayMs: number, reason: string, options?: { skipInitialOpenCheck?: boolean }) => {
            const isStillOpen = () => getTabs().some(openTab => openTab.id === readyTab.id && openTab.type === "project" && openTab.projectPath === readyTab.projectPath);
            if (!options?.skipInitialOpenCheck && !isStillOpen()) return;
            const existingTimer = projectPrepareTimersRef.current.get(readyTab.id);
            if (existingTimer !== undefined) window.clearTimeout(existingTimer);
            const timer = window.setTimeout(() => {
                projectPrepareTimersRef.current.delete(readyTab.id);
                if (!isStillOpen()) return;
                finishProjectTabPreparing(readyTab.id, readyTab.projectPath);
                console.info("[AIAssistantPanel] project tab prepared", { tabId: readyTab.id, projectPath: readyTab.projectPath, prepareMode, reason, elapsedMs: Math.round(performance.now() - startedAt) });
            }, Math.max(0, delayMs));
            projectPrepareTimersRef.current.set(readyTab.id, timer);
        };
        const tab = createProjectTab(projectPath, taskTitle, prepareMode === "new-agent" ? {
            onSessionReady: (readyTab) => {
                const minimumVisibleMs = Math.max(0, 120 - (performance.now() - startedAt));
                scheduleNewAgentReady(readyTab, minimumVisibleMs, "session-ready");
            },
        } : undefined);
        if (tab && tab.projectPath && !tabExisted) {
            setProjectTabPreparing(tab.id, true, prepareMode || "restore-context");
            console.info("[AIAssistantPanel] project tab preparing", { tabId: tab.id, projectPath: tab.projectPath, prepareMode: prepareMode || "restore-context" });
            if (prepareMode === "new-agent") {
                scheduleNewAgentReady(tab, 5000, "session-ready-timeout", { skipInitialOpenCheck: true });
                return tab;
            }
            loadProjectContext(tab.projectPath, (msg) => {
                const existing = getTabState(tab.id);
                const nextHistory = [msg, ...withoutProjectContextMessages(existing?.history)];
                saveTabState(tab.id, {
                    ...existing,
                    history: nextHistory,
                });
                if (activeTabIdRef.current === tab.id) {
                    setProjectTabMessages(nextHistory);
                }
            }, () => {
                finishProjectTabPreparing(tab.id, tab.projectPath);
                console.info("[AIAssistantPanel] project tab prepared", { tabId: tab.id, projectPath: tab.projectPath, elapsedMs: Math.round(performance.now() - startedAt) });
            });
        }
        return tab;
    }, [createProjectTab, finishProjectTabPreparing, getTabs, hasProjectTab, loadProjectContext, getTabState, saveTabState, setProjectTabPreparing]);
    const messagesLengthRef = useRef(messages.length);
    messagesLengthRef.current = messages.length;
    const sendMessageForTab = useCallback((text: string, options?: Record<string, unknown>): Promise<boolean> => {
        const optionProjectPath = typeof options?.project_path === "string" ? options.project_path : undefined;
        const optionTabId = typeof options?.tabId === "string" ? options.tabId : undefined;
        const liveActiveTab = activeTabRef.current;
        const activeSessionProjectPath = projectPathFromSessionKey(getActiveSessionKey());
        const resolvedProjectPath = optionProjectPath
            || (liveActiveTab.type === "project" ? liveActiveTab.projectPath : undefined)
            || activeSessionProjectPath
            || undefined;
        const resolvedTab = resolvedProjectPath
            ? getTabs().find(t => t.type === "project" && t.projectPath === resolvedProjectPath)
            : undefined;
        const resolvedTabId = optionTabId || resolvedTab?.id || (liveActiveTab.type === "project" ? liveActiveTab.id : undefined);
        const isProjectSend = !!resolvedProjectPath;
        if (isProjectSend && resolvedProjectPath) {
            const mergedOptions = {
                ...options,
                tabId: resolvedTabId,
                project_path: resolvedProjectPath,
            };
            const contextTabId = String(mergedOptions.tabId || '');
            const contextHistory = contextTabId === liveActiveTab.id
                ? projectTabMessages
                : ((getTabState(contextTabId)?.history || []) as ChatMessage[]);
            (mergedOptions as Record<string, unknown>).recentMessages = buildProjectTabRecentMessages(contextHistory);
            console.info("[AIAssistantPanel] send route project", {
                tabId: mergedOptions.tabId,
                projectPath: mergedOptions.project_path,
                activeTabId: liveActiveTab.id,
                activeTabType: liveActiveTab.type,
                activeSessionProjectPath: activeSessionProjectPath || undefined,
                textLength: text.trim().length,
                recentMessages: Array.isArray((mergedOptions as Record<string, unknown>).recentMessages) ? ((mergedOptions as Record<string, unknown>).recentMessages as unknown[]).length : 0,
            });
            logAIPanelDiagnostic({
                event: "send_route_project",
                tabId: mergedOptions.tabId,
                projectPath: mergedOptions.project_path,
                activeTabId: liveActiveTab.id,
                activeTabType: liveActiveTab.type,
                activeSessionProjectPath: activeSessionProjectPath || "",
                textLength: text.trim().length,
                recentMessages: Array.isArray((mergedOptions as Record<string, unknown>).recentMessages) ? ((mergedOptions as Record<string, unknown>).recentMessages as unknown[]).length : 0,
            });
            const roundSeq = projectTabRoundSeqRef.current + 1;
            projectTabRoundSeqRef.current = roundSeq;
            const roundKey = projectSessionKey(resolvedProjectPath);
            if (roundKey) {
                projectTabRoundsRef.current.set(roundKey, {
                    tabId: typeof mergedOptions.tabId === "string" ? mergedOptions.tabId : null,
                    projectPath: resolvedProjectPath,
                    baseline: messagesLengthRef.current,
                    seq: roundSeq,
                });
                setProjectTabRouteVersion(version => version + 1);
            }
            return sendMessage(text, mergedOptions).then((sent: boolean) => {
                const currentRound = roundKey ? projectTabRoundsRef.current.get(roundKey) : undefined;
                if (sent === false && currentRound?.seq === roundSeq) {
                    projectTabRoundsRef.current.delete(roundKey);
                    setProjectTabRouteVersion(version => version + 1);
                }
                return sent;
            }, (err: unknown) => {
                const currentRound = roundKey ? projectTabRoundsRef.current.get(roundKey) : undefined;
                if (currentRound?.seq === roundSeq) {
                    projectTabRoundsRef.current.delete(roundKey);
                    setProjectTabRouteVersion(version => version + 1);
                }
                throw err;
            });
        }
        const activeProjectRounds = Array.from(projectTabRoundsRef.current.values());
        console.info("[AIAssistantPanel] send route local", {
            activeTabId: liveActiveTab.id,
            textLength: text.trim().length,
            detachedProjectTabIds: activeProjectRounds.map(round => round.tabId).filter(Boolean),
            detachedProjectPaths: activeProjectRounds.map(round => round.projectPath).filter(Boolean),
        });
        logAIPanelDiagnostic({
            event: "send_route_local",
            activeTabId: liveActiveTab.id,
            activeTabType: liveActiveTab.type,
            activeSessionProjectPath: activeSessionProjectPath || "",
            textLength: text.trim().length,
            detachedProjectTabId: activeProjectRounds.map(round => round.tabId).filter(Boolean).join(","),
            detachedProjectPath: activeProjectRounds.map(round => round.projectPath).filter(Boolean).join(","),
        });
        const localSessionKey = 'desktop-user';
        markPanelSendInFlight(localSessionKey, true);
        const localSend = options === undefined ? sendMessage(text) : sendMessage(text, options);
        return localSend.finally(() => markPanelSendInFlight(localSessionKey, false));
    }, [getTabState, getTabs, markPanelSendInFlight, messages, persistProjectTabMsgIds, projectTabMessages, saveTabState, sendMessage]);
    sendMessageForTabRef.current = sendMessageForTab;
    const sendProjectMessageAfterPrepare = useCallback((text: string, options?: Record<string, unknown>): Promise<boolean> => {
        const tabId = typeof options?.tabId === "string" ? options.tabId : "";
        const projectPath = typeof options?.project_path === "string" ? options.project_path : "";
        if (tabId && projectPath && preparingProjectTabIdsRef.current.has(tabId)) {
            const deferred = deferredProjectInitialSendsRef.current.get(tabId) || [];
            deferred.push(text);
            deferredProjectInitialSendsRef.current.set(tabId, deferred);
            console.info("[AIAssistantPanel] defer project send until prepare completes", { tabId, projectPath, textLength: text.trim().length });
            return Promise.resolve(true);
        }
        return sendMessageForTab(text, options);
    }, [sendMessageForTab]);
    const clearProjectRoundTrackingForTab = useCallback((tabId: string) => {
        let changed = false;
        const tab = getTabs().find(t => t.id === tabId);
        if (tab?.type === "project" && tab.projectPath) {
            forgetAIAssistantSessionRounds(`desktop-user:${tab.projectPath}`);
            const prepareTimer = projectPrepareTimersRef.current.get(tabId);
            if (prepareTimer !== undefined) {
                window.clearTimeout(prepareTimer);
                projectPrepareTimersRef.current.delete(tabId);
            }
            setProjectTabPreparing(tabId, false);
            deferredProjectInitialSendsRef.current.delete(tabId);
        }
        for (const [roundKey, round] of projectTabRoundsRef.current) {
            if (round.tabId !== tabId) continue;
            const sessionKey = projectSessionKey(tab?.type === "project" ? tab.projectPath : round.projectPath);
            const messagesToMark = sessionKey
                ? messages.slice(round.baseline).filter((message: ChatMessage) => messageBelongsToSessionOrLegacy(message, sessionKey))
                : [];
            for (const message of messagesToMark) {
                projectTabMsgIdsRef.current.add(message.id);
            }
            projectTabRoundsRef.current.delete(roundKey);
            changed = true;
        }
        for (const [key, detached] of detachedProjectRoundsRef.current) {
            if (detached.tabId !== tabId) continue;
            for (const messageId of detached.messageIds) {
                projectTabMsgIdsRef.current.add(messageId);
            }
            detachedProjectRoundsRef.current.delete(key);
            changed = true;
        }
        if (changed) {
            persistProjectTabMsgIds();
            setProjectTabRouteVersion(version => version + 1);
            setDetachedProjectRoundVersion(version => version + 1);
        }
    }, [getTabs, messages, persistProjectTabMsgIds, setProjectTabPreparing]);
    const closeTabWithProjectCleanup = useCallback((tabId: string) => {
        clearProjectRoundTrackingForTab(tabId);
        previewStateMapRef.current.delete(tabId);
        if (previewOwnerTabRef.current === tabId) { previewOwnerTabRef.current = "local"; previewOwnerResetPendingRef.current = true; }
        closeTab(tabId);
    }, [clearProjectRoundTrackingForTab, closeTab]);
    const createProjectTabFromSearch = useCallback((projectPath: string, taskTitle: string, options?: { autoSend?: boolean }) => {
        const tabExistedInList = hasProjectTab(projectPath);
        const tab = createProjectTabWithContext(projectPath, taskTitle);
        if (!tab || !options?.autoSend || tabExistedInList) return tab;

        const existingState = getTabState(tab.id);
        const hasExistingConversation = existingState?.history?.some((m: any) => m && (m.role === "user" || m.role === "assistant"));
        if (!hasExistingConversation) {
            void sendProjectMessageAfterPrepare(taskTitle, { tabId: tab.id, project_path: tab.projectPath });
        }
        return tab;
    }, [createProjectTabWithContext, getTabState, hasProjectTab, sendProjectMessageAfterPrepare]);
    const closeProjectTabByPath = useCallback((projectPath: string) => {
        const tab = getTabs().find(t => t.type === "project" && t.projectPath === projectPath);
        if (tab) {
            console.info("[AIAssistantPanel] closing project tab", { projectPath, tabId: tab.id });
            closeTabWithProjectCleanup(tab.id);
        }
    }, [closeTabWithProjectCleanup, getTabs]);
    useEffect(() => {
        const off = EventsOn(EVENT_PROJECT_TASK_CLOSED, (projectPath: string) => {
            if (typeof projectPath === "string" && projectPath.trim()) {
                closeProjectTabByPath(projectPath);
            }
        });
        return () => {
            if (typeof off === "function") off();
            else EventsOff(EVENT_PROJECT_TASK_CLOSED);
        };
    }, [closeProjectTabByPath]);
    const addParticipantToTab = useAddGroupParticipantToTab({ getTabState, upgradeVETabToGroup });
    const addLocalMaclawToTab = useAddLocalMaclawToTab({ getTabState, upgradeVETabToGroup });
    const [participantInviteTargetTabId, setParticipantInviteTargetTabId] = useState<string | null>(null);
    const participantInviteTargetTab = participantInviteTargetTabId ? tabState.tabs.find(t => t.id === participantInviteTargetTabId) || null : null;
    usePendingAssistantTabOpen({
        lang,
        createVETab,
        createGroupTab,
        createProjectTab: createProjectTabWithContext,
        activateTab,
        getTabState,
        saveTabState,
        getTabList: getTabs,
        hasProjectTab,
        sendMessage: sendProjectMessageAfterPrepare,
        pendingVEOpen,
        onPendingVEOpenHandled,
        pendingHistoryDiscussionOpen,
        onPendingHistoryDiscussionOpenHandled,
        pendingProjectTabOpen: props.pendingProjectTabOpen,
        onPendingProjectTabOpenHandled: props.onPendingProjectTabOpenHandled,
    });
    useEffect(() => {
        if (!tabLimitError) return;
        const timer = setTimeout(clearTabLimitError, 3000);
        return () => clearTimeout(timer);
    }, [tabLimitError, clearTabLimitError]);
    const codingPreviewOwnerTab = tabState.tabs.find(tab => tab.id === previewOwnerTabRef.current);
    const codingPreviewEventScope = (canShowAssistantCodingPreviewForTab(activeTab) ? activeTab.projectPath : codingPreviewOwnerTab?.projectPath) || LOCAL_CODING_PREVIEW_EVENT_SCOPE;
    const { state: workflowState, openDocPreview, closeDocPreview, setSplitRatio: setWorkflowSplitRatio, dismissMaximizeSuggestion, getSnapshot: getWorkflowSnapshot, restoreState: restoreWorkflowState, resetState: resetWorkflowState } = useWorkflowState(codingPreviewEventScope);
    const { state: codePreviewState, closePanel: closeCodePreview, activatePassive: activateCodePreviewPassive, selectFile: selectCodeFile, restoreState: restoreCodePreviewState, resetSession: resetCodePreviewState } = useCodePreviewState(codingPreviewEventScope);
    useEffect(() => {
        if (!previewOwnerResetPendingRef.current) return; previewOwnerResetPendingRef.current = false;
        const state = previewStateMapRef.current.get("local");
        if (state) { restoreWorkflowState(state.workflow); restoreCodePreviewState(state.code); }
        else { resetWorkflowState(); resetCodePreviewState(); }
    }, [activeTab.id, restoreWorkflowState, restoreCodePreviewState, resetWorkflowState, resetCodePreviewState]);
    const showAgentView = !!agentView && agentViewOwnerTabRef.current === activeTab.id;
    const codingPreviewAllowed = canShowAssistantCodingPreviewForTab(activeTab);
    const showWorkflowPreview = codingPreviewAllowed && workflowState.splitMode;
    const showCodePreview = codingPreviewAllowed && !showAgentView && codePreviewState.active;
    const anySplitActive = showWorkflowPreview || showCodePreview || showAgentView;
    const splitRatio = anySplitActive ? workflowState.splitRatio : 1;
    const startPreviewResize = useAssistantPreviewResize(setWorkflowSplitRatio);
    const codePreviewStateRef = useRef(codePreviewState);
    codePreviewStateRef.current = codePreviewState;
    // Toggle the entire right-side area (workflow doc preview + code preview) open/closed
    const handleTogglePreviewPanel = useCallback(() => {
        if (!codingPreviewAllowed) return;
        if (workflowState.splitMode || codePreviewStateRef.current.active) {
            closeDocPreview();
            closeCodePreview();
        } else {
            openDocPreview();
            const cp = codePreviewStateRef.current;
            if (cp.files.size > 0 && !cp.active) {
                activateCodePreviewPassive();
            }
        }
    }, [codingPreviewAllowed, workflowState.splitMode, closeDocPreview, closeCodePreview, openDocPreview, activateCodePreviewPassive]);
    // Keep ref updated so clearActiveHistory (defined earlier) can close all preview panels
    closeAllPreviewPanelsRef.current = () => {
        closeDocPreview();
        closeCodePreview();
        resetWorkflowState();
        if (agentView) dismissAgentView(agentView.id, undefined, { force: true });
    };
    const title = lang === "en" ? "AI Assistant" : "AI \u52a9\u624b";
    const thinkingText = lang === "en" ? "Thinking... (you can type ahead)" : "\u6b63\u5728\u601d\u8003...\uff08\u53ef\u7ee7\u7eed\u8f93\u5165\uff09";
    const processingText = lang === "en" ? "Running tools... (you can type ahead)" : "\u6b63\u5728\u6267\u884c\u5de5\u5177\u2026\uff08\u53ef\u7ee7\u7eed\u8f93\u5165\uff09";
    const idlePlaceholderText = lang === "en" ? "Type a message..." : "\u8f93\u5165\u6d88\u606f...";
    const savedFileLabel = lang === "en" ? "Saved file" : "\u6587\u4ef6\u5df2\u4fdd\u5b58";
    const hasActiveDetachedProjectRound = useMemo(() => (
        isProjectTabActive && Array.from(detachedProjectRoundsRef.current.values()).some(detached => detached.tabId === activeTab.id)
    ), [activeTab.id, detachedProjectRoundVersion, isProjectTabActive]);
    const hasForegroundProjectRound = projectTabRoundsRef.current.size > 0;
    const hasForegroundRoundForActiveProject = isProjectTabActive && !!findProjectRoundForTab(activeTab.id, activeTab.projectPath);
    const sendingSessionKey = typeof rawSendingSessionKey === "string" && rawSendingSessionKey.trim() ? rawSendingSessionKey.trim() : "";
    const streamingSessionKey = typeof rawStreamingSessionKey === "string" && rawStreamingSessionKey.trim() ? rawStreamingSessionKey.trim() : "";
    const busySessionKeys = useMemo(
        () => Array.isArray(rawBusySessionKeys) ? rawBusySessionKeys.map(key => String(key || '').trim()).filter(Boolean) : [],
        [rawBusySessionKeys],
    );
    const streamingSessionKeys = useMemo(
        () => Array.isArray(rawStreamingSessionKeys) ? rawStreamingSessionKeys.map(key => String(key || '').trim()).filter(Boolean) : [],
        [rawStreamingSessionKeys],
    );
    useEffect(() => {
        if (projectTabRoundsRef.current.size === 0 || !Array.isArray(rawBusySessionKeys)) return;
        const busySet = new Set(busySessionKeys);
        let changed = false;
        for (const [roundKey, round] of Array.from(projectTabRoundsRef.current.entries())) {
            const roundSessionKey = projectSessionKey(round.projectPath);
            if (!roundSessionKey || busySet.has(roundSessionKey)) continue;
            const newMessages = messages.slice(round.baseline).filter((message: ChatMessage) => messageBelongsToSessionOrLegacy(message, roundSessionKey));
            if (newMessages.length > 0 && round.tabId) {
                const existingState = getTabState(round.tabId);
                const nextHistory = mergeChatMessages(existingState?.history, newMessages);
                saveTabState(round.tabId, {
                    ...existingState,
                    history: nextHistory,
                });
                if (activeTabIdRef.current === round.tabId) {
                    setProjectTabMessages(nextHistory);
                }
                for (const message of newMessages) {
                    projectTabMsgIdsRef.current.add(message.id);
                }
                persistProjectTabMsgIds();
            }
            projectTabRoundsRef.current.delete(roundKey);
            changed = true;
            console.info("[AIAssistantPanel] project round session idle", {
                tabId: round.tabId,
                projectPath: round.projectPath,
                sessionKey: roundSessionKey,
                messageCount: newMessages.length,
            });
            logAIPanelDiagnostic({
                event: "project_round_session_idle",
                tabId: round.tabId || "",
                projectPath: round.projectPath,
                sessionKey: roundSessionKey,
                messageCount: newMessages.length,
            });
        }
        if (changed) setProjectTabRouteVersion(version => version + 1);
    }, [busySessionKeys, getTabState, messages, persistProjectTabMsgIds, rawBusySessionKeys, saveTabState]);
    const hasExplicitBusySessionList = Array.isArray(rawBusySessionKeys);
    const hasExplicitStreamingSessionList = Array.isArray(rawStreamingSessionKeys);
    const panelSessionIsSending = panelSendInFlightSessionKeys.has(activeSessionKey);
    const activeSessionIsSending = panelSessionIsSending || (hasExplicitBusySessionList
        ? busySessionKeys.includes(activeSessionKey)
        : sending && (sendingSessionKey
            ? sendingSessionKey === activeSessionKey
            : (isProjectTabActive ? hasForegroundRoundForActiveProject : (isLocalTabActive && !hasForegroundProjectRound))));
    const activeSessionIsStreaming = hasExplicitStreamingSessionList
        ? streamingSessionKeys.includes(activeSessionKey)
        : streaming && (streamingSessionKey
            ? streamingSessionKey === activeSessionKey
            : (isProjectTabActive ? hasForegroundRoundForActiveProject : (isLocalTabActive && !hasForegroundProjectRound)));
    const isBusy = (hasExplicitBusySessionList ? activeSessionIsSending : hasActiveDetachedProjectRound || activeSessionIsSending);
    const activeSessionHasWork = isBusy || activeSessionIsStreaming;
    const displayProgressMessages = activeSessionHasWork ? progressMessages : [];
    useEffect(() => {
        if (!(sending || streaming) || isBusy || activeSessionIsStreaming) return;
        console.info("[AIAssistantPanel] active session idle while another session is busy", {
            activeTabId: activeTab.id,
            activeTabType: activeTab.type,
            activeSessionKey,
            sending,
            sendingSessionKey: sendingSessionKey || undefined,
            busySessionKeys,
            streaming,
            streamingSessionKey: streamingSessionKey || undefined,
            streamingSessionKeys,
        });
    }, [activeSessionIsStreaming, activeSessionKey, activeTab.id, activeTab.type, busySessionKeys, isBusy, panelSessionIsSending, sending, sendingSessionKey, streaming, streamingSessionKey, streamingSessionKeys]);
    const activeProjectPreparing = isProjectTabActive && preparingProjectTabIds.has(activeTab.id);
    const activeProjectPrepareMode = activeProjectPreparing ? (preparingProjectTabModes.get(activeTab.id) || "restore-context") : "restore-context";
    const inputLocked = isBusy || cancelPending || activeProjectPreparing;
    const submitLocked = inputLocked;
    const prevSubmitLockedRef = useRef(submitLocked);
    const prevShowChatUIRef = useRef(showChatUI);
    const continueQueueDrainRef = useRef(false);
    const queueAutoDrainArmedRef = useRef(false);
    const latestSubmitLockedRef = useRef(submitLocked);
    const latestShowChatUIRef = useRef(showChatUI);
    latestSubmitLockedRef.current = submitLocked;
    latestShowChatUIRef.current = showChatUI;

    const showThinkingState = activeSessionIsStreaming;
    const showProcessingState = isBusy && (!activeSessionIsStreaming || hasActiveDetachedProjectRound);
    const showBusySpinner = isBusy;
    const codingAgentTurnSnapshot = useMemo(() => activeSessionHasWork ? latestCodingAgentTurnSnapshot(displayProgressMessages) : null, [activeSessionHasWork, displayProgressMessages]);
    const codingAgentProgress = useMemo(() => codingAgentTurnSnapshot?.latest || activeCodingAgentProgress(displayProgressMessages, activeSessionHasWork), [activeSessionHasWork, codingAgentTurnSnapshot, displayProgressMessages]);
    const latestToolProgress = useMemo(() => findLatestToolProgressText(displayProgressMessages, activeSessionHasWork), [activeSessionHasWork, displayProgressMessages]);
    const activeProcessingText = codingAgentProgress
        ? codingAgentCompactText(codingAgentProgress, lang)
        : latestToolProgress
            ? `${formatToolProgressStatus(latestToolProgress, lang)} · ${lang === "en" ? "you can type ahead" : "\u53ef\u7ee7\u7eed\u8f93\u5165"}`
            : processingText;
    const projectSearch = useProjectSearch(lang);
    const handleProjectSearchSwitch = useCallback(async (msg: string) => {
        if (isBusy && cancelSession) {
            const ok = window.confirm(localizeText(lang, "A task is running. Stop it and switch tasks?", "\u5f53\u524d\u6709\u4efb\u52a1\u6b63\u5728\u6267\u884c\u3002\u662f\u5426\u4e2d\u6b62\u5f53\u524d\u4efb\u52a1\u5e76\u5207\u6362\uff1f"));
            if (!ok) return;
            await cancelSession();
        }
        await sendMessageForTab(msg);
    }, [cancelSession, isBusy, lang, sendMessageForTab]);
    const handleForkCurrentChat = useCallback(async (taskName: string) => {
        let derivedName = taskName;
        if (!derivedName) {
            const firstUser = messages.find((m: ChatMessage) => m.role === "user");
            const text = firstUser && typeof firstUser.content === "string" ? firstUser.content : "";
            const runes = [...text];
            derivedName = runes.length > 30 ? runes.slice(0, 30).join("") + "..." : text || (lang === "en" ? "New task" : "\u65b0\u4efb\u52a1");
        }
        try {
            const { CreateRecentTask, ForkConversationToProject } = await getWailsAppModule();
            const result = await CreateRecentTask(derivedName);
            if (!result || !result.project_path) return;
            await ForkConversationToProject(result.project_path);
            const tab = createProjectTab(result.project_path, result.name || derivedName);
            if (tab) {
                saveTabState(tab.id, { history: [...messages], scrollTop: 0, inputText: "" });
            }
        } catch (err) {
            console.error("[ForkCurrentChat] failed:", err);
        }
    }, [createProjectTab, lang, messages, saveTabState]);
    const initLabel = getAssistantInitLabel(initStatus, lang);
    const preparingPlaceholderText = activeProjectPrepareMode === "new-agent"
        ? (lang === "en" ? "Creating agent instance... type ahead, Enter will wait" : "正在创建 Agent 实例... 可预输入，Enter 会等待")
        : (lang === "en" ? "Restoring task context... type ahead, Enter will wait" : "正在恢复任务上下文... 可预输入，Enter 会等待");
    const placeholderText = !ready
        ? initLabel
        : activeProjectPreparing
            ? preparingPlaceholderText
            : showThinkingState
            ? thinkingText
            : showProcessingState
                ? activeProcessingText
                : idlePlaceholderText;
    const inputValue = localDraftInputValue;
    const updateInputValue = useCallback((nextValue: string) => {
        setLocalDraftInputValue(nextValue);
        if (activeTab.type === "project") {
            saveTabState(activeTab.id, { inputText: nextValue });
            return;
        }
        setDraftInputValue?.(nextValue);
    }, [activeTab.id, activeTab.type, saveTabState, setDraftInputValue]);
    const canSend = ready && (!!inputValue.trim() || pendingAttachments.length > 0 || selectedFilePaths.length > 0);
    const handleWelcomePromptSelect = useCallback((text: string) => {
        updateInputValue(text);
        requestAnimationFrame(() => {
            if (inputRef.current) {
                inputRef.current.focus();
                // Auto-grow textarea height to fit multi-line template
                inputRef.current.style.height = "auto";
                inputRef.current.style.height = inputRef.current.scrollHeight + "px";
                // Select the first [placeholder] so user can immediately type the value
                const firstBracket = text.indexOf('[');
                const closeBracket = text.indexOf(']', firstBracket);
                if (firstBracket >= 0 && closeBracket > firstBracket) {
                    inputRef.current.selectionStart = firstBracket;
                    inputRef.current.selectionEnd = closeBracket + 1;
                } else {
                    // No placeholder — move cursor to end
                    inputRef.current.selectionStart = text.length;
                    inputRef.current.selectionEnd = text.length;
                }
            }
        });
    }, [updateInputValue, inputRef]);
    const selectedFileName = selectedFilePath ? selectedFilePath.split(/[/\\]/).pop() || selectedFilePath : "";
    const { pinnedNews, otherMessages } = useMemo(() => {
        const pinned: ChatMessage[] = [];
        const other: ChatMessage[] = [];
        for (const m of displayMessages) {
            if (isPinnedNewsMessage(m)) {
                pinned.push(m);
            } else {
                other.push(m);
            }
        }
        return { pinnedNews: pinned.slice(0, 2), otherMessages: other };
    }, [displayMessages]);
    // Show welcome only for an idle, empty local conversation; active work/queues need the full composer.
    // Use isLocalTabActive instead of tabState.tabs.length === 1 so that the welcome/guide
    // interface still appears after "New Session" even when project tabs exist.
    // NOTE: welcome view is shown in both inline (embedded panel) and overlay (standalone window)
    // modes — the embedded panel is now the primary usage mode.
    const showWelcomeView = ready && !onboardingIncomplete && otherMessages.length === 0 && displayProgressMessages.length === 0 && !showThinkingState && !showProcessingState && !activeProjectPreparing && queue.length === 0 && !queueEditDraftActive && !queueInteractionStarted && isLocalTabActive;
    const hasConversation = otherMessages.length + displayProgressMessages.length > 0;
    const { handleScroll, outputContainerRef, outputEndRef, scrollToBottom, userScrolledUpRef } = useAssistantOutputScroll({ hasConversation, messages: displayMessages, ready, scrollToTopSeq });
    const handleInputResizeEnd = useCallback(() => {
        scrollToBottom("auto", true, 2);
    }, [scrollToBottom]);
    const { inputAreaHeight, resizeInput, startInputResize } = useResizableAssistantInput(inputRef, inputValue, handleInputResizeEnd);
    useEffect(() => {
        if (activeTab.type === "project") return;
        setLocalDraftInputValue(draftInputValue);
    }, [activeTab.type, draftInputValue]);
    useEffect(() => {
        const timer = setTimeout(() => inputRef.current?.focus(), 100);
        return () => clearTimeout(timer);
    }, []);
    useEffect(() => {
        return () => {
            if (projectTabIdsPersistTimerRef.current) {
                clearTimeout(projectTabIdsPersistTimerRef.current);
                projectTabIdsPersistTimerRef.current = null;
            }
            firingEntryIdsRef.current.clear();
            drainingEntryIdsRef.current.clear();
            continueQueueDrainRef.current = false;
        };
    }, []);
    useEffect(() => {
        if (!maximized && inline) return;
        const handler = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            if (inline && maximized) {
                onToggleMaximize?.();
                return;
            }
            if (!inline) onClose();
        };
        window.addEventListener("keydown", handler);
        return () => window.removeEventListener("keydown", handler);
    }, [onClose, inline, maximized, onToggleMaximize]);
    const applyInputValue = useCallback((nextValue: string) => {
        updateInputValue(nextValue);
        requestAnimationFrame(() => {
            resizeInput();
            if (!inputRef.current) return;
            inputRef.current.focus();
            const caret = nextValue.length;
            inputRef.current.setSelectionRange(caret, caret);
        });
    }, [resizeInput, updateInputValue]);
    const { exitHistoryBrowsing, isSelectionCollapsedAtBoundary, recallHistory, rememberHistoryEdit, resetHistoryBrowsing } = useAssistantInputHistory({ applyInputValue, inputRef, inputValue, submittedPrompts });
    const handleClearInput = useCallback(() => {
        resetHistoryBrowsing();
        updateInputValue("");
        if (inputRef.current) inputRef.current.style.height = "auto";
        requestAnimationFrame(() => inputRef.current?.focus());
    }, [resetHistoryBrowsing, updateInputValue]);
    const submitRecognizedVoiceText = useCallback(async (text: string, _source?: VoiceInputSource) => {
        const trimmed = text.trim();
        if (!trimmed || !ready || inputLocked) return;
        resetHistoryBrowsing();
        updateInputValue("");
        void sendMessageForTab(trimmed).then((sent) => {
            if (sent !== false) recordSubmittedPrompt?.(trimmed);
        }).catch((err: unknown) => {
            console.warn("[AIAssistantPanel] Voice prompt send failed", err);
        });
    }, [inputLocked, ready, recordSubmittedPrompt, resetHistoryBrowsing, sendMessageForTab, updateInputValue]);
    const voiceInput = useVoiceInput(submitRecognizedVoiceText, audioInputDeviceId || '');
    const { finishVoicePointer, handleVoiceClick, handleVoicePointerDown, handleVoicePointerLeave } = useAIAssistantVoiceControls({
        inputRef,
        petFocusInputSeq,
        petVoiceStartSeq,
        ready,
        voiceInput,
    });
    const handleSend = useCallback(async () => {
        const rawInputValue = inputRef.current?.value ?? inputValue;
        const text = rawInputValue.trim();
        if ((text === '/btw' || text.startsWith('/btw ')) && sendBtwMessage) {
            const btwQuery = text.slice(4).trim(); // strip "/btw" prefix
            recordSubmittedPrompt?.(text);
            resetHistoryBrowsing();
            updateInputValue("");
            if (inputRef.current) inputRef.current.style.height = "auto";
            setPendingAttachments([]);
            clearSelectedFile?.();
            requestAnimationFrame(() => inputRef.current?.focus());
            await sendBtwMessage(btwQuery);
            return;
        }
        if (submitLocked || queueEditDraftActive) {
            if (!text && pendingAttachments.length === 0 && selectedFilePaths.length === 0) {
                if (queueEditDraftActive) {
                    resetHistoryBrowsing();
                    updateInputValue("");
                    setQueueEditDraftActive(false);
                    if (inputRef.current) inputRef.current.style.height = "auto";
                    requestAnimationFrame(() => inputRef.current?.focus());
                }
                return;
            }
            console.info("[AIAssistantPanel] queue input", {
                activeTabId: activeTab.id,
                activeTabType: activeTab.type,
                projectPath: activeTab.projectPath || "",
                submitLocked,
                queueEditDraftActive,
                sending,
                sendingSessionKey: sendingSessionKey || undefined,
                busySessionKeys,
                streaming,
                streamingSessionKey: streamingSessionKey || undefined,
                streamingSessionKeys,
                activeSessionKey,
                activeSessionIsSending,
                activeSessionIsStreaming,
                cancelPending,
                textLength: text.length,
                attachmentCount: pendingAttachments.length + selectedFilePaths.length,
            });
            const attachments: AttachmentInfo[] = [...pendingAttachments];
            for (const fp of selectedFilePaths) {
                const fileName = fp.split(/[/\\]/).pop() || fp;
                const ext = "." + (fileName.split(".").pop() || "").toLowerCase();
                attachments.push({ filePath: fp, isImage: isImageFilePath(fp), fileName, extension: ext });
            }
            setQueueInteractionStarted(true);
            addEntry(rawInputValue, attachments, { autoDrain: submitLocked });
            if (submitLocked) {
                queueAutoDrainArmedRef.current = true;
            }
            resetHistoryBrowsing();
            updateInputValue("");
            setQueueEditDraftActive(false);
            if (inputRef.current) inputRef.current.style.height = "auto";
            setPendingAttachments([]);
            clearSelectedFile?.();
            requestAnimationFrame(() => inputRef.current?.focus());
            return;
        }
        if (!text && selectedFilePaths.length === 0 && pendingAttachments.length === 0) return;
        const allFilePaths: string[] = [...selectedFilePaths];
        for (const att of pendingAttachments) {
            if (att.filePath.trim()) allFilePaths.push(att.filePath.trim());
        }
        resetHistoryBrowsing();
        updateInputValue("");
        if (inputRef.current) inputRef.current.style.height = "auto";
        setPendingAttachments([]);
        clearSelectedFile?.();
        userScrolledUpRef.current = false;
        const outgoing = allFilePaths.length > 0 ? buildOutgoingMessageMulti(text, allFilePaths) : text;
        const sent = await sendMessageForTab(outgoing);
        if (sent !== false) recordSubmittedPrompt?.(text);
    }, [activeSessionIsSending, activeSessionIsStreaming, activeSessionKey, activeTab.id, activeTab.projectPath, activeTab.type, busySessionKeys, cancelPending, inputValue, sending, sendingSessionKey, streaming, streamingSessionKey, streamingSessionKeys, submitLocked, queueEditDraftActive, pendingAttachments, selectedFilePaths, addEntry, recordSubmittedPrompt, resetHistoryBrowsing, updateInputValue, clearSelectedFile, sendMessageForTab, sendBtwMessage]);
    useEffect(() => {
        if (queue.length === 0) {
            continueQueueDrainRef.current = false;
            queueAutoDrainArmedRef.current = false;
        }
        const readyToDrainQueue = ready && showChatUI && !submitLocked;
        const becameIdle = prevSubmitLockedRef.current && readyToDrainQueue;
        const returnedToChatIdle = !prevShowChatUIRef.current && readyToDrainQueue;
        const continueIdleDrain = continueQueueDrainRef.current && readyToDrainQueue;
        const armedIdleDrain = queueAutoDrainArmedRef.current && readyToDrainQueue;
        const persistedAutoDrain = !!queue[0]?.autoDrain && readyToDrainQueue;
        if ((becameIdle || returnedToChatIdle || continueIdleDrain || armedIdleDrain || persistedAutoDrain) && queue.length > 0 && !queueEditDraftActive) {
            const entry = queue[0];
            if (firingEntryIdsRef.current.has(entry.id) || drainingEntryIdsRef.current.has(entry.id)) {
                prevSubmitLockedRef.current = submitLocked;
                prevShowChatUIRef.current = showChatUI;
                return;
            }
            continueQueueDrainRef.current = false;
            queueAutoDrainArmedRef.current = false;
            drainingEntryIdsRef.current.add(entry.id);
            refreshQueueInFlight();
            const outgoing = buildOutgoingMessageMulti(entry.text, entry.attachments.map(att => att.filePath));
            console.info("[AIAssistantPanel] drain queued input", {
                activeTabId: activeTab.id,
                activeTabType: activeTab.type,
                projectPath: activeTab.projectPath || "",
                entryId: entry.id,
                textLength: entry.text.trim().length,
                attachmentCount: entry.attachments.length,
            });
            sendMessageForTab(outgoing).then((sent) => {
                if (sent === false) return;
                continueQueueDrainRef.current = true;
                removeEntry(entry.id);
                recordSubmittedPrompt?.(entry.text);
            }).catch(() => {}).finally(() => {
                drainingEntryIdsRef.current.delete(entry.id);
                refreshQueueInFlight();
            });
        }
        prevSubmitLockedRef.current = submitLocked;
        prevShowChatUIRef.current = showChatUI;
    }, [activeTab.id, activeTab.projectPath, activeTab.type, queue, queueEditDraftActive, ready, recordSubmittedPrompt, refreshQueueInFlight, removeEntry, sendMessageForTab, showChatUI, submitLocked]);
    const appendProjectGuideReferenceEcho = useCallback((text: string, targetTabId: string | null) => {
        if (!targetTabId) return;
        const targetTab = tabState.tabs.find(tab => tab.id === targetTabId);
        if (!targetTab || targetTab.type !== "project") return;
        const echo: ChatMessage = {
            id: `guide-reference-${Date.now()}-${Math.random().toString(36).slice(2)}`,
            role: 'system',
            content: `引导已注入下一轮：\n${text}`,
            timestamp: Date.now(),
        };
        const existingState = getTabState(targetTabId);
        const nextHistory = mergeChatMessages(existingState?.history, [echo]);
        saveTabState(targetTabId, {
            ...existingState,
            history: nextHistory,
        });
        if (activeTabIdRef.current === targetTabId) {
            setProjectTabMessages(nextHistory);
        }
    }, [getTabState, saveTabState, tabState.tabs]);
    const handleFireEntry = useCallback(async (id: string) => {
        if (firingEntryIdsRef.current.has(id) || drainingEntryIdsRef.current.has(id)) return;
        const entry = queue.find(item => item.id === id);
        if (!entry) return;
        const guideTargetTabId = isProjectTabActive ? activeTab.id : null;
        firingEntryIdsRef.current.add(id);
        refreshQueueInFlight();
        const outgoing = buildOutgoingMessageMulti(entry.text, entry.attachments.map(att => att.filePath));
        try {
            let injected = false;
            if (guideLaunchReference) {
                injected = await guideLaunchReference(outgoing, activeSessionKey);
            } else if (injectSupplementary) {
                injected = await injectSupplementary(outgoing);
            }
            if (!injected) return;
            appendProjectGuideReferenceEcho(outgoing, guideTargetTabId);
            removeEntry(id);
            recordSubmittedPrompt?.(entry.text);
        } catch {
            return;
        } finally {
            firingEntryIdsRef.current.delete(id);
            refreshQueueInFlight();
        }
    }, [activeSessionKey, activeTab.id, appendProjectGuideReferenceEcho, guideLaunchReference, injectSupplementary, isProjectTabActive, queue, recordSubmittedPrompt, refreshQueueInFlight, removeEntry]);
    const handleDeleteEntry = useCallback((id: string) => {
        if (firingEntryIdsRef.current.has(id) || drainingEntryIdsRef.current.has(id)) return;
        removeEntry(id);
    }, [removeEntry]);
    const isQueueEntryInFlight = useCallback((id: string) => activeProjectPreparing || firingEntryIdsRef.current.has(id) || drainingEntryIdsRef.current.has(id), [activeProjectPreparing, queueInFlightVersion]);
    const handleReorderEntry = useCallback((fromIndex: number, toIndex: number) => {
        const moving = queue[fromIndex];
        const target = queue[toIndex];
        if (!moving || !target) return;
        const isInFlight = (id: string) => firingEntryIdsRef.current.has(id) || drainingEntryIdsRef.current.has(id);
        if (isInFlight(moving.id) || isInFlight(target.id)) return;
        reorderEntry(fromIndex, toIndex);
    }, [queue, reorderEntry]);
    const handleEditEntry = useCallback((id: string) => {
        if (firingEntryIdsRef.current.has(id) || drainingEntryIdsRef.current.has(id)) return;
        const entry = extractEntry(id);
        if (!entry) return;
        setQueueInteractionStarted(true);
        setEditingEntryId(null);
        setQueueEditDraftActive(true);
        updateInputValue(entry.text);
        setPendingAttachments([...entry.attachments]);
        clearSelectedFile?.();
        resetHistoryBrowsing();
        requestAnimationFrame(() => {
            resizeInput();
            if (!inputRef.current) return;
            inputRef.current.focus();
            const caret = entry.text.length;
            inputRef.current.setSelectionRange(caret, caret);
        });
    }, [clearSelectedFile, extractEntry, resetHistoryBrowsing, resizeInput, setPendingAttachments, updateInputValue]);
    const handleCancelEdit = useCallback(() => setEditingEntryId(null), []);
    const handleSaveEdit = useCallback((id: string, text: string, attachments: AttachmentInfo[]) => {
        updateEntry(id, text, attachments);
        setEditingEntryId(null);
    }, [updateEntry]);
    const handleCancel = useCallback(async () => {
        if (!cancelSession || cancelPending) return;
        const restoreSeq = ++cancelRestoreSeqRef.current;
        const previousInputValue = inputValue;
        setCancelPending(true);
        try {
            const { canceledText } = await cancelSession();
            if (cancelRestoreSeqRef.current !== restoreSeq) return;
            if (draftInputValue === previousInputValue) {
                updateInputValue(canceledText);
            }
            resetHistoryBrowsing();
            requestAnimationFrame(() => {
                resizeInput();
                inputRef.current?.focus();
            });
        } finally {
            setCancelPending(false);
        }
    }, [cancelPending, cancelSession, draftInputValue, inputValue, resetHistoryBrowsing, resizeInput, updateInputValue]);
    const lastAssistantIdx = useMemo(() => findLastIndex(otherMessages, m => m.role === 'assistant'), [otherMessages]);
    const renderedOtherMessages = useMemo(() => otherMessages.map((msg: ChatMessage, idx: number) => renderMessage(msg, executeAction, t, idx === lastAssistantIdx, savedFileLabel, lang, isBusy)), [otherMessages, executeAction, t, lastAssistantIdx, savedFileLabel, lang, isBusy]);
    const chatProgressMessages = useMemo(
        () => activeSessionHasWork ? displayProgressMessages.filter((msg: ChatMessage) => !isToolProgressMessage(msg)) : displayProgressMessages,
        [activeSessionHasWork, displayProgressMessages],
    );
    const compactProgressMessages = useMemo(() => compactCodingAgentProgressMessages(chatProgressMessages), [chatProgressMessages]);
    const renderedProgressMessages = useMemo(() => compactProgressMessages.map((msg: ChatMessage) => renderMessage(msg, executeAction, t, false, savedFileLabel, lang)), [compactProgressMessages, executeAction, t, savedFileLabel, lang]);
    const containerStyle: React.CSSProperties = inline ? (maximized ? { ...maximizedInlineStyle, background: t.bg } : { display: "flex", flex: "1 1 0%", flexDirection: "column", minWidth: 0, minHeight: 0, boxSizing: "border-box", overflow: "hidden", background: t.bg, textAlign: "left", width: "100%", height: "100%", position: "relative" }) : overlayStyle;
    return (
        <div data-testid="ai-panel-root" style={containerStyle}>
            {inline && <AssistantDragHandle />}
            <AssistantTitleBar clearHistory={clearActiveHistory} codingAgentProgress={codingAgentProgress} inline={!!inline} lang={lang} maximized={!!maximized} onClose={onClose} onDismissAppUpdate={onDismissAppUpdate} onHideWindow={onHideWindow} onOpenAppUpdate={onOpenAppUpdate} onOpenKnowledge={() => setKnowledgeDialogOpen(true)} onOpenTutorial={onOpenTutorial} onToggleMaximize={onToggleMaximize} onTogglePreviewPanel={handleTogglePreviewPanel} onToggleWorkflow={handleToggleWorkflow} previewPanelOpen={showWorkflowPreview || showCodePreview} projectSearchOpen={projectSearch.open} refreshNews={refreshNews} setThemeMode={setThemeMode} setTtsEnabled={setTtsEnabled} showMaximizeToggle={showMaximizeToggle} theme={t} themeMode={themeMode} title={title} trialReflectEnabled={trialReflectEnabled} ttsEnabled={ttsEnabled} ttsPlaying={ttsPlaying} toggleProjectSearch={projectSearch.toggle} updateAvailable={appUpdateAvailable} workflowActive={workflowState.active} workflowEnabled={workflowEnabled} />
            <div data-testid="ai-panel-content-row" style={{ display: "flex", flexDirection: "row", flex: 1, minHeight: 0, minWidth: 0, overflow: "hidden" }}>
            <div data-testid="ai-panel-body" style={{ display: "flex", flexDirection: "column", flex: splitRatio, minWidth: 0, minHeight: 0, height: "100%", boxSizing: "border-box", overflow: "hidden", position: "relative" }}>
            <KnowledgeDialog open={knowledgeDialogOpen} onClose={() => setKnowledgeDialogOpen(false)} lang={lang} theme={t} />
            <AITabBar tabs={tabState.tabs} activeTabId={tabState.activeTabId} theme={t} onActivate={activateTab} onClose={closeTabWithProjectCleanup} onInviteToTab={(tab) => {
                if (tab.type === "ve") {
                    const tabSt = getTabState(tab.id);
                    const sessionId = tabSt?.sessionId || tab.discussionId;
                    const currentParticipants = tab.participants || (tab.veId ? [tab.veId] : []);
                    const title = String(tab.title || "").trim();
                    const veId = String(tab.veId || "").trim();
                    const titleLooksRaw = looksLikeRawParticipantId(title);
                    const participantNames = veId && title && title !== veId && !titleLooksRaw ? { [veId]: title } : undefined;
                    upgradeVETabToGroup(tab.id, currentParticipants, sessionId, participantNames);
                }
                setParticipantInviteTargetTabId(tab.id);
                activateTab(tab.id);
            }} onAddLocalMaclawToTab={addLocalMaclawToTab} onRenameGroupTab={openRenameGroupDialog} lang={lang} getLastActiveAt={getLastActiveAt} />
            {tabLimitError && <div data-testid="ai-tab-limit-error" style={{ padding: "6px 12px", fontSize: 12, color: t.errorText, background: t.errorBg, borderBottom: `1px solid ${t.errorBorder}`, textAlign: "center" }}>{tabLimitError}</div>}
            {showChatUI && <>
                <AssistantWorkflowMaximizeSuggestion inline={!!inline} lang={lang} maximized={!!maximized} onDismiss={dismissMaximizeSuggestion} onToggleMaximize={onToggleMaximize} suggestMaximize={workflowState.suggestMaximize} theme={t} themeMode={themeMode} />
                <ProjectSearchPanel search={projectSearch} lang={lang} theme={t} inline={!!inline} onProjectSwitch={handleProjectSearchSwitch} onCreateProjectTab={createProjectTabFromSearch} onCloseProjectTab={closeProjectTabByPath} onForkCurrentChat={handleForkCurrentChat} onTaskPrefsChanged={onTaskPrefsChanged} />
                {showWelcomeView ? (
                    <div data-testid="ai-welcome-container" style={{ flex: 1, minHeight: 0, overflow: "auto", background: t.bg }}>
                        <AssistantWelcomeView
                            lang={lang}
                            theme={t}
                            themeMode={themeMode}
                            onPromptSelect={handleWelcomePromptSelect}
                            pinnedNews={pinnedNews}
                            composer={{
                                browseFile,
                                canSend,
                                cancelPending,
                                cancelSession,
                                clearSelectedFile,
                                exitHistoryBrowsing,
                                finishVoicePointer,
                                handleCancel,
                                handleClearInput,
                                handlePaste,
                                handleSend,
                                handleVoiceClick,
                                handleVoicePointerDown,
                                handleVoicePointerLeave,
                                inputLocked,
                                inputRef,
                                inputValue,
                                isBusy,
                                isSelectionCollapsedAtBoundary,
                                pendingAttachments,
                                ready,
                                recallHistory,
                                rememberHistoryEdit,
                                removeSelectedFile,
                                resizeInput,
                                selectedFilePaths,
                                setPendingAttachments,
                                showBusySpinner,
                                updateInputValue,
                                voiceInput,
                            }}
                        />
                    </div>
                ) : (
                <div ref={outputContainerRef} data-testid="ai-output-container" style={{ flex: 1, minHeight: 0, maxHeight: "none", padding: "8px 10px", fontSize: `${chatFontSize}px`, lineHeight: 1.5, overflowY: "auto", overflowX: "hidden", textAlign: "left", color: t.text, background: t.bg, fontFamily: "'Cascadia Code', 'Cascadia Mono', 'Consolas', 'Courier New', monospace", whiteSpace: "normal", overflowWrap: "anywhere", wordBreak: "normal" }} onScroll={handleScroll}>
                    <AssistantConversationBody initLabel={initLabel} lang={lang} messages={displayMessages} onOpenOnboarding={onOpenOnboarding} onboardingIncomplete={onboardingIncomplete} pinnedNews={pinnedNews} processingText={activeProcessingText} ready={ready} renderedOtherMessages={renderedOtherMessages} renderedProgressMessages={renderedProgressMessages} showProcessingState={showProcessingState} showThinkingState={showThinkingState} theme={t} thinkingText={thinkingText} />
                    <div ref={outputEndRef} />
                </div>
                )}
                {activeProjectPreparing && <div data-testid="project-tab-restore-progress" style={{ flexShrink: 0, padding: "7px 10px 8px", borderTop: `1px solid ${t.inputBarBorder}`, background: t.inputBarBg, color: t.textMuted, fontSize: 12 }}>
                    <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 10, marginBottom: 6 }}>
                        <span>{activeProjectPrepareMode === "new-agent" ? (lang === "en" ? "Creating agent instance" : "正在创建 Agent 实例") : (lang === "en" ? "Restoring task context" : "正在恢复任务上下文")}</span>
                        <span style={{ opacity: 0.82 }}>{lang === "en" ? "Input will wait" : "输入会先等待"}</span>
                    </div>
                    <div style={{ height: 3, overflow: "hidden", borderRadius: 999, background: `color-mix(in srgb, ${t.headingColor} 16%, transparent)` }}>
                        <div style={{ width: "38%", height: "100%", borderRadius: "inherit", background: t.headingColor, animation: "sidebar-task-restore-progress 0.9s ease-in-out infinite alternate" }} />
                    </div>
                </div>}
                {!showWelcomeView && <AssistantInputStack browseFile={browseFile} canSend={canSend} cancelPending={cancelPending} cancelSession={cancelSession} clearSelectedFile={clearSelectedFile} editingEntryId={editingEntryId} exitHistoryBrowsing={exitHistoryBrowsing} finishVoicePointer={finishVoicePointer} handleCancel={handleCancel} handleCancelEdit={handleCancelEdit} handleClearInput={handleClearInput} handleEditEntry={handleEditEntry} handlePaste={handlePaste} handleSaveEdit={handleSaveEdit} handleFireEntry={handleFireEntry} handleSend={handleSend} isEntryInFlight={isQueueEntryInFlight} handleVoiceClick={handleVoiceClick} handleVoicePointerDown={handleVoicePointerDown} handleVoicePointerLeave={handleVoicePointerLeave} inputAreaHeight={inputAreaHeight} inputLocked={inputLocked} inputRef={inputRef} inputValue={inputValue} inline={false} isBusy={isBusy} isSelectionCollapsedAtBoundary={isSelectionCollapsedAtBoundary} lang={lang} pendingAttachments={pendingAttachments} placeholderText={placeholderText} queue={queue} ready={ready} recallHistory={recallHistory} rememberHistoryEdit={rememberHistoryEdit} removeEntry={handleDeleteEntry} removeSelectedFile={removeSelectedFile} reorderEntry={handleReorderEntry} resizeInput={resizeInput} selectedFilePaths={selectedFilePaths} setPendingAttachments={setPendingAttachments} showBusySpinner={showBusySpinner} startInputResize={startInputResize} theme={t} themeMode={themeMode} updateInputValue={updateInputValue} voiceInput={voiceInput} />}
            </>}
            <AssistantActiveTabContent activeTab={activeTab} tabs={tabState.tabs} isLocalTabActive={isLocalTabActive} isProjectTabActive={isProjectTabActive} lang={lang} theme={t} getTabState={getTabState} saveTabState={saveTabState} onAddParticipantToTab={addParticipantToTab} />
            {renameGroupTargetTab && (
                <AIAssistantRenameGroupDialog
                    error={renameGroupError}
                    lang={lang}
                    onClose={closeRenameGroupDialog}
                    onSubmit={submitRenameGroupDialog}
                    onValueChange={value => { setRenameGroupValue(value); if (renameGroupError) setRenameGroupError(""); }}
                    saving={renameGroupSaving}
                    theme={t}
                    value={renameGroupValue}
                />
            )}
            {participantInviteTargetTab && <TabParticipantInviteDialog key={participantInviteTargetTab.id} tab={participantInviteTargetTab} lang={lang} theme={t} onClose={() => setParticipantInviteTargetTabId(null)} onAddParticipantToTab={addParticipantToTab} />}
            </div>
            <AssistantPreviewPane agentView={agentView} codePreviewState={codePreviewState} closeCodePreview={closeCodePreview} closeDocPreview={closeDocPreview} dismissAgentView={dismissAgentView} lang={lang} selectCodeFile={selectCodeFile} submitAgentView={submitAgentView} showCodePreview={showCodePreview} showAgentView={showAgentView} showWorkflowPreview={showWorkflowPreview} splitRatio={splitRatio} startPreviewResize={startPreviewResize} theme={t} themeMode={themeMode} workflowState={workflowState} />
            </div>
        </div>
    );
}
