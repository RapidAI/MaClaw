import { useState, useRef, useCallback, useEffect, useMemo } from "react";
import type { ChatMessage } from "./useAIAssistant";
import { findLastIndex, isPinnedNewsMessage, isImageFilePath, buildOutgoingMessageMulti, setActiveSessionKey, getActiveSessionKey } from "./useAIAssistant";
import { useVoiceInput, type VoiceInputSource } from "./useVoiceInput";
import { useWorkflowState } from "./useWorkflowState";
import { useCodePreviewState } from "./useCodePreviewState";
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
import { AssistantWorkflowMaximizeSuggestion } from "./AssistantWorkflowMaximizeSuggestion";
import { useAssistantThemeMode } from "./useAssistantThemeMode";
import { AssistantPreviewPane } from "./AssistantPreviewPane";
import { activeCodingAgentProgress, codingAgentCompactText, latestCodingAgentTurnSnapshot, parseCodingAgentProgress } from "./CodingAgentProgressStatus";
import { findLatestToolProgressText } from "./aiAssistantProgressUtils";
import { AITabBar } from "./AITabBar";
import { useAITabManager } from "./useAITabManager";
import { looksLikeRawParticipantId } from "./localAIIdentity";
import { useAddGroupParticipantToTab } from "./useAddGroupParticipantToTab";
import { useAddLocalMaclawToTab } from "./useAddLocalMaclawToTab";
import { useProjectContextLoader } from "./useProjectContextLoader";
import { AssistantActiveTabContent } from "./AssistantActiveTabContent";
import { usePendingAssistantTabOpen } from "./usePendingAssistantTabOpen";
import type { AIAssistantPanelProps } from "./aiAssistantPanelTypes";
import { loadProjectTabMsgIds, mergeChatMessages, PROJECT_TAB_MSG_IDS_KEY, withoutProjectContextMessages } from "./aiAssistantProjectTabState";
export { isHistoryDiscussionReadOnly } from "./historyDiscussionUtils";
function compactCodingAgentProgressMessages(messages: ChatMessage[]): ChatMessage[] {
    let latestCodingIndex = -1;
    const isCodingProgress = messages.map(message => !!parseCodingAgentProgress(message.content || ""));
    for (let i = messages.length - 1; i >= 0; i--) {
        if (isCodingProgress[i]) {
            latestCodingIndex = i;
            break;
        }
    }
    if (latestCodingIndex < 0) return messages;
    return messages.filter((_message, index) => index === latestCodingIndex || !isCodingProgress[index]);
}
export function AIAssistantPanel(props: AIAssistantPanelProps & any) {
    const { onClose, lang, chatFontSize = 14, themeMode: controlledThemeMode, onThemeModeChange, audioInputDeviceId, audioOutputDeviceId, petVoiceStartSeq = 0, petFocusInputSeq = 0, pendingVEOpen, onPendingVEOpenHandled, pendingHistoryDiscussionOpen, onPendingHistoryDiscussionOpenHandled } = props;
    const state = props.state || props;
    const actions = props.actions || props;
    const panelWindow = props.window || props;
    const {
        messages,
        progressMessages = [],
        sending,
        streaming,
        visualBusy,
        ready,
        initStatus,
        selectedFilePath: selectedFilePathFromState = "",
        submittedPrompts = [],
        draftInputValue = "",
        trialReflectEnabled = false,
        scrollToTopSeq,
        onboardingIncomplete,
        showTraceEntry = false,
        agentView = null,
    } = state;
    const {
        browseFile,
        clearSelectedFile,
        removeSelectedFile,
        sendMessage,
        sendBtwMessage,
        injectSupplementary,
        guideLaunchReference,
        clearHistory,
        recordSubmittedPrompt,
        setDraftInputValue,
        executeAction,
        refreshNews,
        onOpenOnboarding,
        cancelSession,
        onOpenTutorial,
        onTaskPrefsChanged,
        submitAgentView,
        dismissAgentView,
    } = actions;
    const selectedFilePaths = Array.isArray(state.selectedFilePaths) ? state.selectedFilePaths : (selectedFilePathFromState ? [selectedFilePathFromState] : []);
    const selectedFilePath = selectedFilePaths[0] || "";
    const {
        inline,
        maximized = false,
        onToggleMaximize,
        onHideWindow,
    } = panelWindow || {};
    const [localDraftInputValue, setLocalDraftInputValue] = useState(draftInputValue);
    const [cancelPending, setCancelPending] = useState(false);
    const [editingEntryId, setEditingEntryId] = useState<string | null>(null);
    const [queueEditDraftActive, setQueueEditDraftActive] = useState(false);
    const [knowledgeDialogOpen, setKnowledgeDialogOpen] = useState(false);
    const inputRef = useRef<HTMLTextAreaElement | null>(null);
    const cancelRestoreSeqRef = useRef(0);
    const { themeMode, setThemeMode } = useAssistantThemeMode(controlledThemeMode, onThemeModeChange);
    const { ttsEnabled, setTtsEnabled, ttsPlaying } = useTTSReadback(audioOutputDeviceId);
    const { queue, addEntry, removeEntry, updateEntry, reorderEntry, extractEntry } = useBufferQueue();
    const firingEntryIdsRef = useRef<Set<string>>(new Set());
    const drainingEntryIdsRef = useRef<Set<string>>(new Set());
    const [queueInFlightVersion, setQueueInFlightVersion] = useState(0);
    const refreshQueueInFlight = useCallback(() => setQueueInFlightVersion(version => version + 1), []);
    const { handlePaste, pendingAttachments, setPendingAttachments } = usePastedImageAttachments();
    const t = themeMode === 'dark' ? darkTheme : (inline ? lightTheme : overlayTheme);
    const showMaximizeToggle = inline && !!onToggleMaximize;
    const {
        tabState,
        activeTab,
        activateTab,
        createVETab,
        createGroupTab,
        createProjectTab,
        closeTab,
        clearTabConversation,
        saveTabState,
        getTabState,
        getLastActiveAt,
        getTabs,
        hasProjectTab,
        upgradeVETabToGroup,
        tabLimitError,
        clearTabLimitError,
    } = useAITabManager();
    const clearActiveHistory = useCallback(async () => {
        if (activeTab.type === "ve" || (activeTab.type === "group" && !!activeTab.veId)) {
            clearTabConversation(activeTab.id);
            return;
        }
        await clearHistory();
    }, [activeTab.id, activeTab.type, activeTab.veId, clearHistory, clearTabConversation]);
    const isLocalTabActive = activeTab.id === "local";
    const isProjectTabActive = activeTab.type === "project";
    const showChatUI = isLocalTabActive || isProjectTabActive;
    const activeSessionKey = isProjectTabActive && activeTab.projectPath
        ? `desktop-user:${activeTab.projectPath}`
        : 'desktop-user';
    useEffect(() => {
        setActiveSessionKey(activeSessionKey);
        return () => {
            if (getActiveSessionKey() === activeSessionKey) setActiveSessionKey('');
        };
    }, [activeSessionKey]);
    const [projectTabMessages, setProjectTabMessages] = useState<ChatMessage[]>([]);
    const [projectTabRouteVersion, setProjectTabRouteVersion] = useState(0);
    const activeTabIdRef = useRef<string>(activeTab.id);
    activeTabIdRef.current = activeTab.id;
    const prevActiveTabIdRef = useRef<string>(activeTab.id);
    useEffect(() => {
        const prevTabId = prevActiveTabIdRef.current;
        const currentTabId = activeTab.id;
        if (prevTabId === currentTabId) return;
        const prevTab = tabState.tabs.find(t => t.id === prevTabId);
        if (prevTab && prevTab.type === "project") {
            const scrollTop = outputContainerRef.current?.scrollTop || 0;
            let historyToSave = projectTabMessages;
            if (sending && projectTabMsgBaselineRef.current >= 0 && projectTabRoundTabIdRef.current === prevTabId) {
                const inFlightMessages = messages.slice(projectTabMsgBaselineRef.current);
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
            const hasPendingRoundForTab = projectTabRoundTabIdRef.current === currentTabId ||
                (!!activeTab.projectPath && projectTabRoundProjectPathRef.current === activeTab.projectPath);
            if (!sending && !hasPendingRoundForTab) {
                projectTabMsgBaselineRef.current = -1;
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
    const projectTabMsgBaselineRef = useRef<number>(-1);
    const projectTabRoundTabIdRef = useRef<string | null>(null);
    const projectTabRoundProjectPathRef = useRef<string | null>(null);
    const projectTabRoundSeqRef = useRef(0);
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
    const displayMessages = useMemo(() => {
        if (!isProjectTabActive) {
            if (projectTabMsgBaselineRef.current >= 0) {
                return messages.slice(0, projectTabMsgBaselineRef.current);
            }
            if (projectTabMsgIdsRef.current.size > 0) {
                return messages.filter((m: ChatMessage) => !projectTabMsgIdsRef.current.has(m.id));
            }
            return messages;
        }
        const isActiveProjectRound = projectTabRoundTabIdRef.current === activeTab.id ||
            (!!activeTab.projectPath && projectTabRoundProjectPathRef.current === activeTab.projectPath);
        if (!sending || projectTabMsgBaselineRef.current < 0 || !isActiveProjectRound) return projectTabMessages;
        const newMessages = messages.slice(projectTabMsgBaselineRef.current);
        if (newMessages.length === 0) return projectTabMessages;
        const existingIds = new Set(projectTabMessages.map((m: ChatMessage) => m.id));
        const unique = newMessages.filter((m: ChatMessage) => !existingIds.has(m.id));
        if (unique.length === 0) return projectTabMessages;
        return [...projectTabMessages, ...unique];
    }, [activeTab.id, activeTab.projectPath, isProjectTabActive, messages, projectTabMessages, projectTabRouteVersion, sending]);
    const displayProgressMessages = sending
        ? (isProjectTabActive
            ? (projectTabRoundTabIdRef.current === activeTab.id || (!!activeTab.projectPath && projectTabRoundProjectPathRef.current === activeTab.projectPath) ? progressMessages : [])
            : (isLocalTabActive && !projectTabRoundTabIdRef.current ? progressMessages : []))
        : [];
    const prevSendingRef = useRef(sending);
    useEffect(() => {
        const wasSending = prevSendingRef.current;
        prevSendingRef.current = sending;
        if (!wasSending && sending && isProjectTabActive && projectTabMsgBaselineRef.current < 0) {
            projectTabMsgBaselineRef.current = messages.length;
            projectTabRoundTabIdRef.current = activeTab.id;
            projectTabRoundProjectPathRef.current = activeTab.projectPath || null;
        }
        if (wasSending && !sending && projectTabMsgBaselineRef.current >= 0) {
            const roundTabId = projectTabRoundTabIdRef.current;
            if (roundTabId) {
                const newMessages = messages.slice(projectTabMsgBaselineRef.current);
                if (newMessages.length > 0) {
                    const appendUnique = (history: unknown[] | undefined) => {
                        const existingHistory = (Array.isArray(history) ? history : []) as ChatMessage[];
                        const existingIds = new Set(existingHistory.map((m: ChatMessage) => m.id));
                        const unique = newMessages.filter((m: ChatMessage) => !existingIds.has(m.id));
                        return unique.length === 0 ? existingHistory : [...existingHistory, ...unique];
                    };
                    if (isProjectTabActive && activeTab.id === roundTabId) {
                        setProjectTabMessages(prev => appendUnique(prev));
                    }
                    const existingState = getTabState(roundTabId);
                    const baseHistory = isProjectTabActive && activeTab.id === roundTabId
                        ? mergeChatMessages(existingState?.history, projectTabMessages)
                        : existingState?.history;
                    saveTabState(roundTabId, {
                        ...existingState,
                        history: appendUnique(baseHistory),
                    });
                    for (const m of newMessages) {
                        projectTabMsgIdsRef.current.add(m.id);
                    }
                }
            } else {
                const newMessages = messages.slice(projectTabMsgBaselineRef.current);
                for (const m of newMessages) {
                    projectTabMsgIdsRef.current.add(m.id);
                }
            }
            projectTabMsgBaselineRef.current = -1;
            projectTabRoundTabIdRef.current = null;
            projectTabRoundProjectPathRef.current = null;
            setProjectTabRouteVersion(version => version + 1);
            persistProjectTabMsgIds();
        }
    }, [activeTab.id, getTabState, isProjectTabActive, messages, persistProjectTabMsgIds, projectTabMessages, saveTabState, sending]);
    const { loadProjectContext } = useProjectContextLoader();
    const createProjectTabWithContext = useCallback((projectPath: string, taskTitle: string, _archived?: boolean) => {
        const tab = createProjectTab(projectPath, taskTitle);
        if (tab && tab.projectPath) {
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
            });
        }
        return tab;
    }, [createProjectTab, loadProjectContext, getTabState, saveTabState]);
    const messagesLengthRef = useRef(messages.length);
    messagesLengthRef.current = messages.length;
    const sendMessageForTab = useCallback((text: string, options?: Record<string, unknown>): Promise<boolean> => {
        const optionProjectPath = typeof options?.project_path === "string" ? options.project_path : undefined;
        const optionTabId = typeof options?.tabId === "string" ? options.tabId : undefined;
        const isProjectSend = (activeTab.type === "project" && activeTab.projectPath) || !!optionProjectPath;
        if (isProjectSend) {
            projectTabMsgBaselineRef.current = messagesLengthRef.current;
            const mergedOptions = {
                ...options,
                tabId: optionTabId || activeTab.id,
                project_path: optionProjectPath || activeTab.projectPath,
            };
            const roundSeq = projectTabRoundSeqRef.current + 1;
            projectTabRoundSeqRef.current = roundSeq;
            projectTabRoundTabIdRef.current = mergedOptions.tabId || null;
            projectTabRoundProjectPathRef.current = mergedOptions.project_path || null;
            setProjectTabRouteVersion(version => version + 1);
            return sendMessage(text, mergedOptions).then((sent: boolean) => {
                if (sent === false && projectTabRoundSeqRef.current === roundSeq) {
                    projectTabMsgBaselineRef.current = -1;
                    projectTabRoundTabIdRef.current = null;
                    projectTabRoundProjectPathRef.current = null;
                    setProjectTabRouteVersion(version => version + 1);
                }
                return sent;
            }, (err: unknown) => {
                if (projectTabRoundSeqRef.current === roundSeq) {
                    projectTabMsgBaselineRef.current = -1;
                    projectTabRoundTabIdRef.current = null;
                    projectTabRoundProjectPathRef.current = null;
                    setProjectTabRouteVersion(version => version + 1);
                }
                throw err;
            });
        }
        return options === undefined ? sendMessage(text) : sendMessage(text, options);
    }, [sendMessage, activeTab]);
    const addParticipantToTab = useAddGroupParticipantToTab({ getTabState, upgradeVETabToGroup });
    const addLocalMaclawToTab = useAddLocalMaclawToTab({ getTabState, upgradeVETabToGroup });
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
        sendMessage: sendMessageForTab,
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
    const { state: workflowState, closeDocPreview, setSplitRatio: setWorkflowSplitRatio, dismissMaximizeSuggestion } = useWorkflowState();
    const { state: codePreviewState, closePanel: closeCodePreview, selectFile: selectCodeFile } = useCodePreviewState(workflowState.splitMode);
    const showAgentView = !!agentView;
    const showWorkflowPreview = !showAgentView && workflowState.splitMode && !codePreviewState.active;
    const showCodePreview = !showAgentView && !showWorkflowPreview && codePreviewState.active;
    const anySplitActive = showWorkflowPreview || showCodePreview || showAgentView;
    const splitRatio = anySplitActive ? workflowState.splitRatio : 1;
    const startPreviewResize = useAssistantPreviewResize(setWorkflowSplitRatio);
    const title = lang === "en" ? "AI Assistant" : "AI \u52a9\u624b";
    const thinkingText = lang === "en" ? "Thinking... (you can type ahead)" : "\u6b63\u5728\u601d\u8003...\uff08\u53ef\u7ee7\u7eed\u8f93\u5165\uff09";
    const processingText = lang === "en" ? "Running tools... (you can type ahead)" : "\u6b63\u5728\u6267\u884c\u5de5\u5177\u2026\uff08\u53ef\u7ee7\u7eed\u8f93\u5165\uff09";
    const idlePlaceholderText = lang === "en" ? "Type a message..." : "\u8f93\u5165\u6d88\u606f...";
    const savedFileLabel = lang === "en" ? "Saved file" : "\u6587\u4ef6\u5df2\u4fdd\u5b58";
    const isBusy = sending;
    const inputLocked = isBusy || cancelPending;
    const submitLocked = inputLocked;
    const prevSubmitLockedRef = useRef(submitLocked);
    const prevShowChatUIRef = useRef(showChatUI);
    const continueQueueDrainRef = useRef(false);
    const queueAutoDrainArmedRef = useRef(false);
    const latestSubmitLockedRef = useRef(submitLocked);
    const latestShowChatUIRef = useRef(showChatUI);
    latestSubmitLockedRef.current = submitLocked;
    latestShowChatUIRef.current = showChatUI;

    const showThinkingState = streaming;
    const showProcessingState = isBusy && !streaming;
    const showBusySpinner = isBusy;
    const codingAgentTurnSnapshot = useMemo(() => sending ? latestCodingAgentTurnSnapshot(displayProgressMessages) : null, [displayProgressMessages, sending]);
    const codingAgentProgress = useMemo(() => codingAgentTurnSnapshot?.latest || activeCodingAgentProgress(displayProgressMessages, sending), [codingAgentTurnSnapshot, displayProgressMessages, sending]);
    const latestToolProgress = useMemo(() => findLatestToolProgressText(displayProgressMessages, sending), [displayProgressMessages, sending]);
    const activeProcessingText = codingAgentProgress
        ? codingAgentCompactText(codingAgentProgress, lang)
        : latestToolProgress
            ? `${latestToolProgress}${lang === "en" ? " (you can type ahead)" : "\uff08\u53ef\u7ee7\u7eed\u8f93\u5165\uff09"}`
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
            const { CreateRecentTask, ForkConversationToProject } = await import("../../../wailsjs/go/main/App");
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
    const placeholderText = !ready
        ? initLabel
        : showThinkingState
            ? thinkingText
            : showProcessingState
                ? activeProcessingText
                : idlePlaceholderText;
    const inputValue = localDraftInputValue;
    const updateInputValue = useCallback((nextValue: string) => {
        setLocalDraftInputValue(nextValue);
        setDraftInputValue?.(nextValue);
    }, [setDraftInputValue]);
    const canSend = ready && (!!inputValue.trim() || pendingAttachments.length > 0 || selectedFilePaths.length > 0);
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
    const hasConversation = otherMessages.length + displayProgressMessages.length > 0;
    const { handleScroll, outputContainerRef, outputEndRef, scrollToBottom, userScrolledUpRef } = useAssistantOutputScroll({ hasConversation, messages: displayMessages, ready, scrollToTopSeq });
    const handleInputResizeEnd = useCallback(() => {
        scrollToBottom("auto", true, 2);
    }, [scrollToBottom]);
    const { inputAreaHeight, resizeInput, startInputResize } = useResizableAssistantInput(inputRef, inputValue, handleInputResizeEnd);
    useEffect(() => {
        setLocalDraftInputValue(draftInputValue);
    }, [draftInputValue]);
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
    const submitRecognizedVoiceText = useCallback(async (text: string, _source?: VoiceInputSource) => {
        const trimmed = text.trim();
        if (!trimmed || !ready || inputLocked) return;
        resetHistoryBrowsing();
        setLocalDraftInputValue("");
        setDraftInputValue?.("");
        void sendMessageForTab(trimmed).then((sent) => {
            if (sent !== false) recordSubmittedPrompt?.(trimmed);
        }).catch((err: unknown) => {
            console.warn("[AIAssistantPanel] Voice prompt send failed", err);
        });
    }, [inputLocked, ready, recordSubmittedPrompt, resetHistoryBrowsing, sendMessageForTab, setDraftInputValue]);
    const voiceInput = useVoiceInput(submitRecognizedVoiceText, audioInputDeviceId || '');
    const { finishVoicePointer, handleVoiceClick, handleVoicePointerDown, handleVoicePointerLeave } = useAIAssistantVoiceControls({
        inputRef,
        petFocusInputSeq,
        petVoiceStartSeq,
        ready,
        voiceInput,
    });
    const handleSend = useCallback(async () => {
        const text = inputValue.trim();
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
            const attachments: AttachmentInfo[] = [...pendingAttachments];
            for (const fp of selectedFilePaths) {
                const fileName = fp.split(/[/\\]/).pop() || fp;
                const ext = "." + (fileName.split(".").pop() || "").toLowerCase();
                attachments.push({ filePath: fp, isImage: isImageFilePath(fp), fileName, extension: ext });
            }
            addEntry(inputValue, attachments, { autoDrain: submitLocked });
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
    }, [inputValue, submitLocked, queueEditDraftActive, pendingAttachments, selectedFilePaths, addEntry, recordSubmittedPrompt, resetHistoryBrowsing, updateInputValue, clearSelectedFile, sendMessageForTab, sendBtwMessage]);
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
    }, [queue, queueEditDraftActive, ready, recordSubmittedPrompt, refreshQueueInFlight, removeEntry, sendMessageForTab, showChatUI, submitLocked]);
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
    const isQueueEntryInFlight = useCallback((id: string) => firingEntryIdsRef.current.has(id) || drainingEntryIdsRef.current.has(id), [queueInFlightVersion]);
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
    const renderedOtherMessages = useMemo(() => otherMessages.map((msg: ChatMessage, idx: number) => renderMessage(msg, executeAction, t, idx === lastAssistantIdx, savedFileLabel, lang)), [otherMessages, executeAction, t, lastAssistantIdx, savedFileLabel, lang]);
    const compactProgressMessages = useMemo(() => compactCodingAgentProgressMessages(displayProgressMessages), [displayProgressMessages]);
    const renderedProgressMessages = useMemo(() => compactProgressMessages.map((msg: ChatMessage) => renderMessage(msg, executeAction, t, false, savedFileLabel, lang)), [compactProgressMessages, executeAction, t, savedFileLabel, lang]);
    const containerStyle: React.CSSProperties = inline
        ? (maximized
            ? maximizedInlineStyle
            : { display: "flex", flex: "1 1 0%", flexDirection: "column", minWidth: 0, minHeight: 0, boxSizing: "border-box", overflow: "hidden", background: t.bg, textAlign: "left", width: "100%", height: "100%", position: "relative" })
        : overlayStyle;
    return (
        <div data-testid="ai-panel-root" style={{ ...containerStyle, flexDirection: "row" }}>
            <div data-testid="ai-panel-body" style={{ display: "flex", flexDirection: "column", flex: splitRatio, minWidth: 0, minHeight: 0, height: "100%", boxSizing: "border-box", overflow: "hidden", position: "relative" }}>
            {inline && (
                <div style={{
                    height: "30px", width: "100%",
                    position: "absolute", top: 0, left: 0, zIndex: 999,
                    '--wails-draggable': 'drag',
                } as any} />
            )}
            <AssistantTitleBar clearHistory={clearActiveHistory} codingAgentProgress={codingAgentProgress} inline={!!inline} lang={lang} maximized={!!maximized} onClose={onClose} onHideWindow={onHideWindow} onOpenKnowledge={() => setKnowledgeDialogOpen(true)} onOpenTutorial={onOpenTutorial} onToggleMaximize={onToggleMaximize} projectSearchOpen={projectSearch.open} refreshNews={refreshNews} setThemeMode={setThemeMode} setTtsEnabled={setTtsEnabled} showMaximizeToggle={showMaximizeToggle} theme={t} themeMode={themeMode} title={title} trialReflectEnabled={trialReflectEnabled} ttsEnabled={ttsEnabled} ttsPlaying={ttsPlaying} toggleProjectSearch={projectSearch.toggle} />
            <KnowledgeDialog open={knowledgeDialogOpen} onClose={() => setKnowledgeDialogOpen(false)} lang={lang} theme={t} />
            <AITabBar tabs={tabState.tabs} activeTabId={tabState.activeTabId} theme={t} onActivate={activateTab} onClose={closeTab} onInviteToTab={(tab) => {
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
                activateTab(tab.id);
            }} onAddLocalMaclawToTab={addLocalMaclawToTab} lang={lang} getLastActiveAt={getLastActiveAt} />
            {tabLimitError && <div data-testid="ai-tab-limit-error" style={{ padding: "6px 12px", fontSize: 12, color: t.errorText, background: t.errorBg, borderBottom: `1px solid ${t.errorBorder}`, textAlign: "center" }}>{tabLimitError}</div>}
            {showChatUI && <>
                <AssistantWorkflowMaximizeSuggestion inline={!!inline} lang={lang} maximized={!!maximized} onDismiss={dismissMaximizeSuggestion} onToggleMaximize={onToggleMaximize} suggestMaximize={workflowState.suggestMaximize} theme={t} themeMode={themeMode} />
                <ProjectSearchPanel search={projectSearch} lang={lang} theme={t} inline={!!inline} onProjectSwitch={handleProjectSearchSwitch} onCreateProjectTab={createProjectTabWithContext} onForkCurrentChat={handleForkCurrentChat} onTaskPrefsChanged={onTaskPrefsChanged} />
                <div ref={outputContainerRef} data-testid="ai-output-container" style={{ flex: 1, minHeight: 0, maxHeight: "none", padding: "8px 10px", fontSize: `${chatFontSize}px`, lineHeight: 1.5, overflowY: "auto", overflowX: "hidden", textAlign: "left", color: t.text, background: t.bg, fontFamily: "'Cascadia Code', 'Cascadia Mono', 'Consolas', 'Courier New', monospace", whiteSpace: "pre-wrap", wordBreak: "break-all" }} onScroll={handleScroll}>
                    <AssistantConversationBody initLabel={initLabel} lang={lang} messages={displayMessages} onOpenOnboarding={onOpenOnboarding} onboardingIncomplete={onboardingIncomplete} pinnedNews={pinnedNews} processingText={activeProcessingText} ready={ready} renderedOtherMessages={renderedOtherMessages} renderedProgressMessages={renderedProgressMessages} showProcessingState={showProcessingState} showThinkingState={showThinkingState} theme={t} thinkingText={thinkingText} />
                    <div ref={outputEndRef} />
                </div>
                <AssistantInputStack browseFile={browseFile} canSend={canSend} cancelPending={cancelPending} cancelSession={cancelSession} clearSelectedFile={clearSelectedFile} editingEntryId={editingEntryId} exitHistoryBrowsing={exitHistoryBrowsing} finishVoicePointer={finishVoicePointer} handleCancel={handleCancel} handleCancelEdit={handleCancelEdit} handleEditEntry={handleEditEntry} handlePaste={handlePaste} handleSaveEdit={handleSaveEdit} handleFireEntry={handleFireEntry} handleSend={handleSend} isEntryInFlight={isQueueEntryInFlight} handleVoiceClick={handleVoiceClick} handleVoicePointerDown={handleVoicePointerDown} handleVoicePointerLeave={handleVoicePointerLeave} inputAreaHeight={inputAreaHeight} inputLocked={inputLocked} inputRef={inputRef} inputValue={inputValue} inline={!!inline} isBusy={isBusy} isSelectionCollapsedAtBoundary={isSelectionCollapsedAtBoundary} lang={lang} pendingAttachments={pendingAttachments} placeholderText={placeholderText} queue={queue} ready={ready} recallHistory={recallHistory} rememberHistoryEdit={rememberHistoryEdit} removeEntry={handleDeleteEntry} removeSelectedFile={removeSelectedFile} reorderEntry={handleReorderEntry} resizeInput={resizeInput} selectedFilePaths={selectedFilePaths} setPendingAttachments={setPendingAttachments} showBusySpinner={showBusySpinner} startInputResize={startInputResize} theme={t} themeMode={themeMode} updateInputValue={updateInputValue} voiceInput={voiceInput} />
            </>}
            <AssistantActiveTabContent activeTab={activeTab} tabs={tabState.tabs} isLocalTabActive={isLocalTabActive} isProjectTabActive={isProjectTabActive} lang={lang} theme={t} getTabState={getTabState} saveTabState={saveTabState} onAddParticipantToTab={addParticipantToTab} />
            </div>
            <AssistantPreviewPane agentView={agentView} codePreviewState={codePreviewState} closeCodePreview={closeCodePreview} closeDocPreview={closeDocPreview} dismissAgentView={dismissAgentView} inline={!!inline} lang={lang} onToggleMaximize={onToggleMaximize} selectCodeFile={selectCodeFile} submitAgentView={submitAgentView} showCodePreview={showCodePreview} showAgentView={showAgentView} showWorkflowPreview={showWorkflowPreview} splitRatio={splitRatio} startPreviewResize={startPreviewResize} theme={t} themeMode={themeMode} workflowState={workflowState} />
        </div>
    );
}
