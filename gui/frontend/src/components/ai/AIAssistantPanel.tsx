import { useState, useRef, useCallback, useEffect, useMemo } from "react";
import type { ChatMessage } from "./useAIAssistant";
import { findLastIndex, isPinnedNewsMessage, isImageFilePath, buildOutgoingMessageMulti } from "./useAIAssistant";
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
import type { AIAssistantPanelProps } from "./aiAssistantPanelTypes";
import { useAssistantThemeMode } from "./useAssistantThemeMode";
import { AssistantPreviewPane } from "./AssistantPreviewPane";
import { activeCodingAgentProgress, codingAgentCompactText, latestCodingAgentTurnSnapshot } from "./CodingAgentProgressStatus";
import { findLatestToolProgressText } from "./aiAssistantProgressUtils";
import { AITabBar } from "./AITabBar";
import { useAITabManager } from "./useAITabManager";
import { useProjectContextLoader } from "./useProjectContextLoader";
import { AssistantActiveTabContent } from "./AssistantActiveTabContent";
import { usePendingAssistantTabOpen } from "./usePendingAssistantTabOpen";
export { isHistoryDiscussionReadOnly } from "./historyDiscussionUtils";
export function AIAssistantPanel(props: any) {
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
    const { queue, addEntry, removeEntry, updateEntry, reorderEntry, mergeAndFire, extractEntry } = useBufferQueue();
    const { handlePaste, pendingAttachments, setPendingAttachments } = usePastedImageAttachments();
    const t = themeMode === 'dark' ? darkTheme : (inline ? lightTheme : overlayTheme);
    const showMaximizeToggle = inline && !!onToggleMaximize;
    // --- Tab System ---
    const {
        tabState,
        activeTab,
        activateTab,
        createVETab,
        createGroupTab,
        createProjectTab,
        closeTab,
        saveTabState,
        getTabState,
        getLastActiveAt,
        hasProjectTab,
        tabLimitError,
        clearTabLimitError,
    } = useAITabManager();
    const isLocalTabActive = activeTab.id === "local";
    const isProjectTabActive = activeTab.type === "project";
    const showChatUI = isLocalTabActive || isProjectTabActive;
    // Each project tab maintains its own messages, scrollTop, and inputText.
    // When switching tabs, we save the current tab's state and restore the target tab's state.
    const [projectTabMessages, setProjectTabMessages] = useState<ChatMessage[]>([]);
    const [projectTabProgressMessages, setProjectTabProgressMessages] = useState<ChatMessage[]>([]);
    const prevActiveTabIdRef = useRef<string>(activeTab.id);
    useEffect(() => {
        const prevTabId = prevActiveTabIdRef.current;
        const currentTabId = activeTab.id;
        if (prevTabId === currentTabId) return;
        // Save state of the tab we're leaving
        const prevTab = tabState.tabs.find(t => t.id === prevTabId);
        if (prevTab && prevTab.type === "project") {
            const scrollTop = outputContainerRef.current?.scrollTop || 0;

            // Capture in-flight messages if streaming is active
            let historyToSave = projectTabMessages;
            if (sending && projectTabMsgBaselineRef.current >= 0) {
                const inFlightMessages = messages.slice(projectTabMsgBaselineRef.current);
                if (inFlightMessages.length > 0) {
                    historyToSave = [...projectTabMessages, ...inFlightMessages];
                }
            }

            saveTabState(prevTabId, {
                history: historyToSave,
                scrollTop,
                inputText: localDraftInputValue,
            });
        }
        // Restore state of the tab we're switching to
        if (activeTab.type === "project") {
            const restored = getTabState(currentTabId);
            if (restored) {
                setProjectTabMessages((restored.history || []) as ChatMessage[]);
                setProjectTabProgressMessages([]);
                setLocalDraftInputValue(restored.inputText || "");
                // Restore scroll position after render
                requestAnimationFrame(() => {
                    if (outputContainerRef.current && restored.scrollTop) {
                        outputContainerRef.current.scrollTop = restored.scrollTop;
                    }
                });
            } else {
                setProjectTabMessages([]);
                setProjectTabProgressMessages([]);
                setLocalDraftInputValue("");
            }
        } else if (activeTab.id === "local") {
            // Switching back to local tab - restore local draft from parent state
            setLocalDraftInputValue(draftInputValue);
        }
        prevActiveTabIdRef.current = currentTabId;
    }, [activeTab.id]); // eslint-disable-line react-hooks/exhaustive-deps
    // Determine which messages to display based on active tab.
    // When a project tab is active AND the hook is processing a request (sending=true),
    // we need to show the project tab's existing history plus the new messages being
    // streamed. The hook appends new messages (user + assistant placeholder) to its
    // internal `messages` state. We track the message count before sending to extract
    // only the newly added messages during this round.
    const projectTabMsgBaselineRef = useRef<number>(-1);
    const displayMessages = useMemo(() => {
        if (!isProjectTabActive) return messages;
        if (!sending || projectTabMsgBaselineRef.current < 0) return projectTabMessages;
        // During streaming: show project tab history + new messages from this round
        const newMessages = messages.slice(projectTabMsgBaselineRef.current);
        if (newMessages.length === 0) return projectTabMessages;
        return [...projectTabMessages, ...newMessages];
    }, [isProjectTabActive, sending, messages, projectTabMessages]);
    const displayProgressMessages = isProjectTabActive
        ? (sending ? progressMessages : projectTabProgressMessages)
        : progressMessages;
    // Before sending from a project tab, record the current messages length as baseline.
    // After the round completes, capture new messages into projectTabMessages.
    const prevSendingRef = useRef(sending);
    useEffect(() => {
        const wasSending = prevSendingRef.current;
        prevSendingRef.current = sending;
        // Round started — record baseline
        if (!wasSending && sending && isProjectTabActive) {
            projectTabMsgBaselineRef.current = messages.length;
        }
        // Round completed — capture new messages into project tab state
        if (wasSending && !sending && isProjectTabActive && projectTabMsgBaselineRef.current >= 0) {
            const newMessages = messages.slice(projectTabMsgBaselineRef.current);
            if (newMessages.length > 0) {
                setProjectTabMessages(prev => [...prev, ...newMessages]);
            }
            setProjectTabProgressMessages([]);
            projectTabMsgBaselineRef.current = -1;
        }
    }, [sending, isProjectTabActive, messages]);
    const { loadProjectContext } = useProjectContextLoader();
    // Wrap createProjectTab to automatically load context for new tabs
    const createProjectTabWithContext = useCallback((projectPath: string, taskTitle: string, _archived?: boolean) => {
        const tab = createProjectTab(projectPath, taskTitle);
        if (tab && tab.projectPath) {
            // Load project context and inject as system message in the tab's history
            loadProjectContext(tab.projectPath, (msg) => {
                const existing = getTabState(tab.id);
                const history = existing?.history || [];
                // Replace any existing project context message, or append
                const filtered = (history as any[]).filter(
                    (m: any) => !m.isProjectContext
                );
                saveTabState(tab.id, {
                    ...existing,
                    history: [msg, ...filtered],
                });
            });
        }
        return tab;
    }, [createProjectTab, loadProjectContext, getTabState, saveTabState]);
    // Wrap sendMessage to include project_path when sending from a project tab
    const sendMessageForTab = useCallback((text: string, options?: any): Promise<boolean> => {
        if (activeTab.type === "project" && activeTab.projectPath) {
            return sendMessage(text, { ...options, tabId: activeTab.id, project_path: activeTab.projectPath });
        }
        return options === undefined ? sendMessage(text) : sendMessage(text, options);
    }, [sendMessage, activeTab]);
    usePendingAssistantTabOpen({
        createVETab,
        createGroupTab,
        createProjectTab: createProjectTabWithContext,
        getTabState,
        hasProjectTab,
        sendMessage,
        pendingVEOpen,
        onPendingVEOpenHandled,
        pendingHistoryDiscussionOpen,
        onPendingHistoryDiscussionOpenHandled,
        pendingProjectTabOpen: props.pendingProjectTabOpen,
        onPendingProjectTabOpenHandled: props.onPendingProjectTabOpenHandled,
    });
    // Clear tab limit error after 3 seconds
    useEffect(() => {
        if (!tabLimitError) return;
        const timer = setTimeout(clearTabLimitError, 3000);
        return () => clearTimeout(timer);
    }, [tabLimitError, clearTabLimitError]);
    const { state: workflowState, closeDocPreview, setSplitRatio: setWorkflowSplitRatio, dismissMaximizeSuggestion } = useWorkflowState();
    const { state: codePreviewState, closePanel: closeCodePreview, selectFile: selectCodeFile } = useCodePreviewState(workflowState.splitMode);
    const showAgentView = !!agentView;
    const showWorkflowPreview = !showAgentView && workflowState.splitMode;
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
    const showThinkingState = streaming;
    const showProcessingState = isBusy && !streaming;
    const showBusySpinner = isBusy;
    const codingAgentTurnSnapshot = useMemo(() => sending ? latestCodingAgentTurnSnapshot(displayProgressMessages) : null, [displayProgressMessages, sending]);
    const codingAgentProgress = useMemo(() => codingAgentTurnSnapshot?.latest || activeCodingAgentProgress(displayProgressMessages, sending), [codingAgentTurnSnapshot, displayProgressMessages, sending]);
    // Use the latest tool-specific progress message if available.
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
    // Fork current local tab conversation into a new project tab.
    // Creates a new task in the recent list, opens it as a project tab,
    // and copies the current messages as initial context.
    const handleForkCurrentChat = useCallback(async (taskName: string) => {
        // Derive a name from the first user message if not provided.
        let derivedName = taskName;
        if (!derivedName) {
            const firstUser = messages.find((m: any) => m.role === "user");
            const text = firstUser && typeof firstUser.content === "string" ? firstUser.content : "";
            const runes = [...text];
            derivedName = runes.length > 30 ? runes.slice(0, 30).join("") + "..." : text || (lang === "en" ? "New task" : "\u65b0\u4efb\u52a1");
        }
        try {
            const { CreateRecentTask, ForkConversationToProject } = await import("../../../wailsjs/go/main/App");
            const result = await CreateRecentTask(derivedName);
            if (!result || !result.project_path) return;
            // Fork backend conversation history to the new project session.
            await ForkConversationToProject(result.project_path);
            // Create the project tab with the new task's path.
            const tab = createProjectTab(result.project_path, result.name || derivedName);
            if (tab) {
                // Fork frontend messages into the new tab's state for immediate display.
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
    // Sync local draft from parent-owned draft state.
    useEffect(() => {
        setLocalDraftInputValue(draftInputValue);
    }, [draftInputValue]);
    // Focus input on mount
    useEffect(() => {
        const timer = setTimeout(() => inputRef.current?.focus(), 100);
        return () => clearTimeout(timer);
    }, []);
    // Escape key closes overlay mode, or exits maximized inline mode.
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
        recordSubmittedPrompt?.(trimmed);
        resetHistoryBrowsing();
        setLocalDraftInputValue("");
        setDraftInputValue?.("");
        void sendMessageForTab(trimmed).catch((err: unknown) => {
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
        // /btw side query: always execute immediately, never buffer.
        // /btw runs in an independent agent loop and does not block the main loop.
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
            addEntry(inputValue, attachments);
            recordSubmittedPrompt?.(inputValue);
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
        recordSubmittedPrompt?.(text);
        resetHistoryBrowsing();
        updateInputValue("");
        if (inputRef.current) inputRef.current.style.height = "auto";
        setPendingAttachments([]);
        clearSelectedFile?.();
        userScrolledUpRef.current = false;
        const outgoing = allFilePaths.length > 0 ? buildOutgoingMessageMulti(text, allFilePaths) : text;
        await sendMessageForTab(outgoing);
    }, [inputValue, submitLocked, queueEditDraftActive, pendingAttachments, selectedFilePaths, addEntry, recordSubmittedPrompt, resetHistoryBrowsing, updateInputValue, clearSelectedFile, sendMessageForTab, sendBtwMessage]);
    useEffect(() => {
        if (prevSubmitLockedRef.current && !submitLocked && queue.length > 0) {
            const result = mergeAndFire();
            if (result) {
                const outgoing = buildOutgoingMessageMulti(result.mergedText, result.allFilePaths);
                recordSubmittedPrompt?.(result.mergedText);
                sendMessageForTab(outgoing).catch(() => {});
            }
        }
        prevSubmitLockedRef.current = submitLocked;
    }, [submitLocked, queue.length, mergeAndFire, sendMessageForTab, recordSubmittedPrompt]);
    const handleFireEntry = useCallback(async (id: string) => {
        const entry = extractEntry(id);
        if (!entry) return;
        const outgoing = buildOutgoingMessageMulti(entry.text, entry.attachments.map(att => att.filePath));
        try {
            const injected = injectSupplementary ? await injectSupplementary(outgoing) : false;
            if (!injected) {
                const sent = await sendMessageForTab(outgoing);
                if (sent === false) {
                    addEntry(entry.text, entry.attachments);
                    return;
                }
            }
            recordSubmittedPrompt?.(entry.text);
        } catch {
            addEntry(entry.text, entry.attachments);
        }
    }, [addEntry, extractEntry, injectSupplementary, recordSubmittedPrompt, sendMessageForTab]);
    const handleEditEntry = useCallback((id: string) => {
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
    const renderedOtherMessages = useMemo(() => {
        return otherMessages.map((msg: ChatMessage, idx: number) => renderMessage(msg, executeAction, t, idx === lastAssistantIdx, savedFileLabel, lang));
    }, [otherMessages, executeAction, t, lastAssistantIdx, savedFileLabel, lang]);
    const renderedProgressMessages = useMemo(() => {
        return displayProgressMessages.map((msg: ChatMessage) => renderMessage(msg, executeAction, t, false, savedFileLabel, lang));
    }, [displayProgressMessages, executeAction, t, savedFileLabel, lang]);
    const containerStyle: React.CSSProperties = inline
        ? (maximized
            ? maximizedInlineStyle
            : { display: "flex", flex: "1 1 0%", flexDirection: "column", minWidth: 0, minHeight: 0, boxSizing: "border-box", overflow: "hidden", background: t.bg, textAlign: "left", width: "100%", height: "100%", position: "relative" })
        : overlayStyle;
    return (
        <div data-testid="ai-panel-root" style={{ ...containerStyle, flexDirection: "row" }}>
            <div data-testid="ai-panel-body" style={{ display: "flex", flexDirection: "column", flex: splitRatio, minWidth: 0, minHeight: 0, height: "100%", boxSizing: "border-box", overflow: "hidden", position: "relative" }}>
            {/* Drag overlay (inline mode), scoped to ai-panel-body only */}
            {inline && (
                <div style={{
                    height: "30px", width: "100%",
                    position: "absolute", top: 0, left: 0, zIndex: 999,
                    '--wails-draggable': 'drag',
                } as any} />
            )}
            <AssistantTitleBar clearHistory={clearHistory} codingAgentProgress={codingAgentProgress} inline={!!inline} lang={lang} maximized={!!maximized} onClose={onClose} onHideWindow={onHideWindow} onOpenKnowledge={() => setKnowledgeDialogOpen(true)} onOpenTutorial={onOpenTutorial} onToggleMaximize={onToggleMaximize} projectSearchOpen={projectSearch.open} refreshNews={refreshNews} setThemeMode={setThemeMode} setTtsEnabled={setTtsEnabled} showMaximizeToggle={showMaximizeToggle} theme={t} themeMode={themeMode} title={title} trialReflectEnabled={trialReflectEnabled} ttsEnabled={ttsEnabled} ttsPlaying={ttsPlaying} toggleProjectSearch={projectSearch.toggle} />
            <KnowledgeDialog open={knowledgeDialogOpen} onClose={() => setKnowledgeDialogOpen(false)} lang={lang} theme={t} />
            <AITabBar tabs={tabState.tabs} activeTabId={tabState.activeTabId} theme={t} onActivate={activateTab} onClose={closeTab} lang={lang} getLastActiveAt={getLastActiveAt} />
            {tabLimitError && <div data-testid="ai-tab-limit-error" style={{ padding: "6px 12px", fontSize: 12, color: t.errorText, background: t.errorBg, borderBottom: `1px solid ${t.errorBorder}`, textAlign: "center" }}>{tabLimitError}</div>}
            {showChatUI && <>
                <AssistantWorkflowMaximizeSuggestion inline={!!inline} lang={lang} maximized={!!maximized} onDismiss={dismissMaximizeSuggestion} onToggleMaximize={onToggleMaximize} suggestMaximize={workflowState.suggestMaximize} theme={t} themeMode={themeMode} />
                <ProjectSearchPanel search={projectSearch} lang={lang} theme={t} inline={!!inline} onProjectSwitch={handleProjectSearchSwitch} onCreateProjectTab={createProjectTabWithContext} onForkCurrentChat={handleForkCurrentChat} onTaskPrefsChanged={onTaskPrefsChanged} />
                <div ref={outputContainerRef} data-testid="ai-output-container" style={{ flex: 1, minHeight: 0, maxHeight: "none", padding: "8px 10px", fontSize: `${chatFontSize}px`, lineHeight: 1.5, overflowY: "auto", overflowX: "hidden", textAlign: "left", color: t.text, background: t.bg, fontFamily: "'Cascadia Code', 'Cascadia Mono', 'Consolas', 'Courier New', monospace", whiteSpace: "pre-wrap", wordBreak: "break-all" }} onScroll={handleScroll}>
                    <AssistantConversationBody initLabel={initLabel} lang={lang} messages={displayMessages} onOpenOnboarding={onOpenOnboarding} onboardingIncomplete={onboardingIncomplete} pinnedNews={pinnedNews} processingText={activeProcessingText} ready={ready} renderedOtherMessages={renderedOtherMessages} renderedProgressMessages={renderedProgressMessages} showProcessingState={showProcessingState} showThinkingState={showThinkingState} theme={t} thinkingText={thinkingText} />
                    <div ref={outputEndRef} />
                </div>
                <AssistantInputStack browseFile={browseFile} canSend={canSend} cancelPending={cancelPending} cancelSession={cancelSession} clearSelectedFile={clearSelectedFile} editingEntryId={editingEntryId} exitHistoryBrowsing={exitHistoryBrowsing} finishVoicePointer={finishVoicePointer} handleCancel={handleCancel} handleCancelEdit={handleCancelEdit} handleEditEntry={handleEditEntry} handlePaste={handlePaste} handleSaveEdit={handleSaveEdit} handleFireEntry={handleFireEntry} handleSend={handleSend} handleVoiceClick={handleVoiceClick} handleVoicePointerDown={handleVoicePointerDown} handleVoicePointerLeave={handleVoicePointerLeave} inputAreaHeight={inputAreaHeight} inputLocked={inputLocked} inputRef={inputRef} inputValue={inputValue} inline={!!inline} isBusy={isBusy} isSelectionCollapsedAtBoundary={isSelectionCollapsedAtBoundary} lang={lang} pendingAttachments={pendingAttachments} placeholderText={placeholderText} queue={queue} ready={ready} recallHistory={recallHistory} rememberHistoryEdit={rememberHistoryEdit} removeEntry={removeEntry} removeSelectedFile={removeSelectedFile} reorderEntry={reorderEntry} resizeInput={resizeInput} selectedFilePaths={selectedFilePaths} setPendingAttachments={setPendingAttachments} showBusySpinner={showBusySpinner} startInputResize={startInputResize} theme={t} themeMode={themeMode} updateInputValue={updateInputValue} voiceInput={voiceInput} />
            </>}            <AssistantActiveTabContent activeTab={activeTab} isLocalTabActive={isLocalTabActive} isProjectTabActive={isProjectTabActive} lang={lang} theme={t} />
            </div>
            <AssistantPreviewPane agentView={agentView} codePreviewState={codePreviewState} closeCodePreview={closeCodePreview} closeDocPreview={closeDocPreview} dismissAgentView={dismissAgentView} inline={!!inline} lang={lang} onToggleMaximize={onToggleMaximize} selectCodeFile={selectCodeFile} submitAgentView={submitAgentView} showCodePreview={showCodePreview} showAgentView={showAgentView} showWorkflowPreview={showWorkflowPreview} splitRatio={splitRatio} startPreviewResize={startPreviewResize} theme={t} themeMode={themeMode} workflowState={workflowState} />
        </div>
    );
}