import { useState, useRef, useCallback, useEffect, useMemo } from "react";
import type { ChatMessage } from "./useAIAssistant";
import { findLastIndex, isPinnedNewsMessage, isImageFilePath, buildOutgoingMessageMulti } from "./useAIAssistant";
import { useVoiceInput, type VoiceInputSource } from "./useVoiceInput";
import { useWorkflowState } from "./useWorkflowState";
import { useCodePreviewState } from "./useCodePreviewState";
import { useBufferQueue } from "./useBufferQueue";
import type { AttachmentInfo } from "./useBufferQueue";
import { renderMessage } from "./aiAssistantMarkdown";
import { AI_PANEL_STATIC_STYLE_ID, AI_PANEL_STATIC_STYLE_TEXT, darkTheme, lightTheme, maximizedInlineStyle, overlayStyle, overlayTheme } from "./aiAssistantPanelTheme";
import { localizeText } from "./aiAssistantI18n";
import { ProjectSearchPanel, useProjectSearch } from "./ProjectSearchPanel";
import { useTTSReadback } from "./useTTSReadback";
import { useAIAssistantVoiceControls } from "./useAIAssistantVoiceControls";
import { useAssistantOutputScroll } from "./useAssistantOutputScroll";
import { useResizableAssistantInput } from "./useResizableAssistantInput";
import { useAssistantInputHistory } from "./useAssistantInputHistory";
import { usePastedImageAttachments } from "./usePastedImageAttachments";
import { useGroupDiscussionControls } from "./useGroupDiscussionControls";
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

if (typeof document !== "undefined" && !document.getElementById(AI_PANEL_STATIC_STYLE_ID)) {
    const style = document.createElement("style");
    style.id = AI_PANEL_STATIC_STYLE_ID;
    style.textContent = AI_PANEL_STATIC_STYLE_TEXT;
    document.head.appendChild(style);
}

export function AIAssistantPanel(props: any) {
    const { onClose, lang, chatFontSize = 14, groupDiscussion, themeMode: controlledThemeMode, onThemeModeChange, audioInputDeviceId, audioOutputDeviceId, petVoiceStartSeq = 0, petFocusInputSeq = 0 } = props;
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
    const { ttsEnabled, setTtsEnabled } = useTTSReadback(audioOutputDeviceId);

    const { queue, addEntry, removeEntry, updateEntry, reorderEntry, mergeAndFire, extractEntry } = useBufferQueue();
    const { handlePaste, pendingAttachments, setPendingAttachments } = usePastedImageAttachments();
    const t = themeMode === 'dark' ? darkTheme : (inline ? lightTheme : overlayTheme);
    const showMaximizeToggle = inline && !!onToggleMaximize;


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
    const processingText = lang === "en" ? "Running tools... (you can type ahead)" : "\u6b63\u5728\u6267\u884c\u5de5\u5177...\uff08\u53ef\u7ee7\u7eed\u8f93\u5165\uff09";
    const idlePlaceholderText = lang === "en" ? "Type a message..." : "\u8f93\u5165\u6d88\u606f...";
    const savedFileLabel = lang === "en" ? "Saved file" : "\u6587\u4ef6\u5df2\u4fdd\u5b58";
    const isBusy = sending;
    const inputLocked = isBusy || cancelPending;
    const submitLocked = inputLocked;
    const prevSubmitLockedRef = useRef(submitLocked);
    const showThinkingState = streaming;
    const showProcessingState = isBusy && !streaming;
    const showBusySpinner = isBusy;
    const codingAgentTurnSnapshot = useMemo(() => sending ? latestCodingAgentTurnSnapshot(progressMessages) : null, [progressMessages, sending]);
    const codingAgentProgress = useMemo(() => codingAgentTurnSnapshot?.latest || activeCodingAgentProgress(progressMessages, sending), [codingAgentTurnSnapshot, progressMessages, sending]);
    const activeProcessingText = codingAgentProgress ? codingAgentCompactText(codingAgentProgress, lang) : processingText;
    const projectSearch = useProjectSearch(lang);
    const handleProjectSearchSwitch = useCallback(async (msg: string) => {
        if (isBusy && cancelSession) {
            const ok = window.confirm(localizeText(lang, "A task is running. Stop it and switch tasks?", "\u5f53\u524d\u6709\u4efb\u52a1\u6b63\u5728\u6267\u884c\u3002\u662f\u5426\u4e2d\u6b62\u5f53\u524d\u4efb\u52a1\u5e76\u5207\u6362\uff1f"));
            if (!ok) return;
            await cancelSession();
        }
        await sendMessage(msg);
    }, [cancelSession, isBusy, lang, sendMessage]);


    const {
        bindGroupDiscussionPress,
        groupActiveTalks,
        groupDiscussionBusy,
        groupDiscussionConfig,
        groupDiscussionDiscoverable,
        groupDiscussionEnabled,
        groupDiscussionLabel,
        groupDiscussionOpen,
        groupDiscussionScopeText,
        groupDiscussionStatus,
        groupPendingInvites,
        groupReadyTalks,
        groupStaleTalks,
        groupWaitingTalks,
        runGroupDiscussionAction,
        setGroupDiscussionOpen,
    } = useGroupDiscussionControls(groupDiscussion, inline, lang);
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
        for (const m of messages) {
            if (isPinnedNewsMessage(m)) {
                pinned.push(m);
            } else {
                other.push(m);
            }
        }
        return { pinnedNews: pinned.slice(0, 2), otherMessages: other };
    }, [messages]);
    const hasConversation = otherMessages.length + progressMessages.length > 0;
    const { handleScroll, outputContainerRef, outputEndRef, scrollToBottom, userScrolledUpRef } = useAssistantOutputScroll({ hasConversation, messages, ready, scrollToTopSeq });
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
        void sendMessage(trimmed).catch((err: unknown) => {
            console.warn("[AIAssistantPanel] Voice prompt send failed", err);
        });
    }, [inputLocked, ready, recordSubmittedPrompt, resetHistoryBrowsing, sendMessage, setDraftInputValue]);

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
        await sendMessage(outgoing);
    }, [inputValue, submitLocked, queueEditDraftActive, pendingAttachments, selectedFilePaths, addEntry, recordSubmittedPrompt, resetHistoryBrowsing, updateInputValue, clearSelectedFile, sendMessage]);

    useEffect(() => {
        if (prevSubmitLockedRef.current && !submitLocked && queue.length > 0) {
            const result = mergeAndFire();
            if (result) {
                const outgoing = buildOutgoingMessageMulti(result.mergedText, result.allFilePaths);
                recordSubmittedPrompt?.(result.mergedText);
                sendMessage(outgoing).catch(() => {});
            }
        }
        prevSubmitLockedRef.current = submitLocked;
    }, [submitLocked, queue.length, mergeAndFire, sendMessage, recordSubmittedPrompt]);

    const handleFireEntry = useCallback(async (id: string) => {
        const entry = extractEntry(id);
        if (!entry) return;
        const outgoing = buildOutgoingMessageMulti(entry.text, entry.attachments.map(att => att.filePath));
        try {
            const injected = injectSupplementary ? await injectSupplementary(outgoing) : false;
            if (!injected) {
                const sent = await sendMessage(outgoing);
                if (sent === false) {
                    addEntry(entry.text, entry.attachments);
                    return;
                }
            }
            recordSubmittedPrompt?.(entry.text);
        } catch {
            addEntry(entry.text, entry.attachments);
        }
    }, [addEntry, extractEntry, injectSupplementary, recordSubmittedPrompt, sendMessage]);

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
        return progressMessages.map((msg: ChatMessage) => renderMessage(msg, executeAction, t, false, savedFileLabel, lang));
    }, [progressMessages, executeAction, t, savedFileLabel, lang]);
    const containerStyle: React.CSSProperties = inline
        ? (maximized
            ? maximizedInlineStyle
            : { display: "flex", flex: "1 1 0%", flexDirection: "column", minWidth: 0, minHeight: 0, boxSizing: "border-box", overflow: "hidden", background: t.bg, textAlign: "left", width: "100%", height: "100%", position: "relative" })
        : overlayStyle;

    return (
        <div data-testid="ai-panel-root" style={{ ...containerStyle, flexDirection: "row" }}>
            <div data-testid="ai-panel-body" style={{ display: "flex", flexDirection: "column", flex: splitRatio, minWidth: 0, minHeight: 0, height: "100%", boxSizing: "border-box", overflow: "hidden", position: "relative" }}>
            {/* Drag overlay (inline mode) — scoped to ai-panel-body only */}
            {inline && !maximized && (
                <div style={{
                    height: "30px", width: "100%",
                    position: "absolute", top: 0, left: 0, zIndex: 999,
                    '--wails-draggable': 'drag',
                } as any} />
            )}
            <AssistantTitleBar
                bindGroupDiscussionPress={bindGroupDiscussionPress}
                clearHistory={clearHistory}
                codingAgentProgress={codingAgentProgress}
                groupActiveTalks={groupActiveTalks}
                groupDiscussion={groupDiscussion}
                groupDiscussionBusy={groupDiscussionBusy}
                groupDiscussionDiscoverable={groupDiscussionDiscoverable}
                groupDiscussionEnabled={groupDiscussionEnabled}
                groupDiscussionLabel={groupDiscussionLabel}
                groupDiscussionOpen={groupDiscussionOpen}
                groupDiscussionScopeText={groupDiscussionScopeText}
                groupDiscussionStatus={groupDiscussionStatus}
                groupPendingInvites={groupPendingInvites}
                groupReadyTalks={groupReadyTalks}
                groupStaleTalks={groupStaleTalks}
                groupWaitingTalks={groupWaitingTalks}
                inline={!!inline}
                lang={lang}
                maximized={!!maximized}
                onClose={onClose}
                onHideWindow={onHideWindow}
                onOpenKnowledge={() => setKnowledgeDialogOpen(true)}
                onOpenTutorial={onOpenTutorial}
                onToggleMaximize={onToggleMaximize}
                projectSearchOpen={projectSearch.open}
                refreshNews={refreshNews}
                runGroupDiscussionAction={runGroupDiscussionAction}
                setGroupDiscussionOpen={setGroupDiscussionOpen}
                setThemeMode={setThemeMode}
                setTtsEnabled={setTtsEnabled}
                showMaximizeToggle={showMaximizeToggle}
                theme={t}
                themeMode={themeMode}
                title={title}
                trialReflectEnabled={trialReflectEnabled}
                ttsEnabled={ttsEnabled}
                toggleProjectSearch={projectSearch.toggle}
            />
            <KnowledgeDialog
                open={knowledgeDialogOpen}
                onClose={() => setKnowledgeDialogOpen(false)}
                lang={lang}
                theme={t}
            />
            {/* Chat area */}
            <AssistantWorkflowMaximizeSuggestion
                inline={!!inline}
                lang={lang}
                maximized={!!maximized}
                onDismiss={dismissMaximizeSuggestion}
                onToggleMaximize={onToggleMaximize}
                suggestMaximize={workflowState.suggestMaximize}
                theme={t}
                themeMode={themeMode}
            />

            <ProjectSearchPanel
                search={projectSearch}
                lang={lang}
                theme={t}
                inline={!!inline}
                onProjectSwitch={handleProjectSearchSwitch}
                onTaskPrefsChanged={onTaskPrefsChanged}
            />

            <div
                ref={outputContainerRef}
                data-testid="ai-output-container"
                style={{
                    flex: 1, minHeight: 0, maxHeight: "none",
                    padding: "8px 10px", fontSize: `${chatFontSize}px`, lineHeight: 1.5,
                    overflowY: "auto", overflowX: "hidden", textAlign: "left",
                    color: t.text, background: t.bg,
                    fontFamily: "'Cascadia Code', 'Cascadia Mono', 'Consolas', 'Courier New', monospace",
                    whiteSpace: "pre-wrap", wordBreak: "break-all",
                }}
                onScroll={handleScroll}
            >
                <AssistantConversationBody
                    initLabel={initLabel}
                    lang={lang}
                    messages={messages}
                    onOpenOnboarding={onOpenOnboarding}
                    onboardingIncomplete={onboardingIncomplete}
                    pinnedNews={pinnedNews}
                    processingText={activeProcessingText}
                    ready={ready}
                    renderedOtherMessages={renderedOtherMessages}
                    renderedProgressMessages={renderedProgressMessages}
                    showProcessingState={showProcessingState}
                    showThinkingState={showThinkingState}
                    theme={t}
                    thinkingText={thinkingText}
                />
                <div ref={outputEndRef} />
            </div>

            {/* Input bar */}
            <AssistantInputStack
                browseFile={browseFile}
                canSend={canSend}
                cancelPending={cancelPending}
                cancelSession={cancelSession}
                clearSelectedFile={clearSelectedFile}
                editingEntryId={editingEntryId}
                exitHistoryBrowsing={exitHistoryBrowsing}
                finishVoicePointer={finishVoicePointer}
                handleCancel={handleCancel}
                handleCancelEdit={handleCancelEdit}
                handleEditEntry={handleEditEntry}
                handlePaste={handlePaste}
                handleSaveEdit={handleSaveEdit}
                handleFireEntry={handleFireEntry}
                handleSend={handleSend}
                handleVoiceClick={handleVoiceClick}
                handleVoicePointerDown={handleVoicePointerDown}
                handleVoicePointerLeave={handleVoicePointerLeave}
                inputAreaHeight={inputAreaHeight}
                inputLocked={inputLocked}
                inputRef={inputRef}
                inputValue={inputValue}
                inline={!!inline}
                isBusy={isBusy}
                isSelectionCollapsedAtBoundary={isSelectionCollapsedAtBoundary}
                lang={lang}
                pendingAttachments={pendingAttachments}
                placeholderText={placeholderText}
                queue={queue}
                ready={ready}
                recallHistory={recallHistory}
                rememberHistoryEdit={rememberHistoryEdit}
                removeEntry={removeEntry}
                removeSelectedFile={removeSelectedFile}
                reorderEntry={reorderEntry}
                resizeInput={resizeInput}
                selectedFilePaths={selectedFilePaths}
                setPendingAttachments={setPendingAttachments}
                showBusySpinner={showBusySpinner}
                startInputResize={startInputResize}
                theme={t}
                themeMode={themeMode}
                updateInputValue={updateInputValue}
                voiceInput={voiceInput}
            />
            </div>
            <AssistantPreviewPane
                agentView={agentView}
                codePreviewState={codePreviewState}
                closeCodePreview={closeCodePreview}
                closeDocPreview={closeDocPreview}
                dismissAgentView={dismissAgentView}
                inline={!!inline}
                lang={lang}
                onToggleMaximize={onToggleMaximize}
                selectCodeFile={selectCodeFile}
                submitAgentView={submitAgentView}
                showCodePreview={showCodePreview}
                showAgentView={showAgentView}
                showWorkflowPreview={showWorkflowPreview}
                splitRatio={splitRatio}
                startPreviewResize={startPreviewResize}
                theme={t}
                themeMode={themeMode}
                workflowState={workflowState}
            />
        </div>
    );
}
