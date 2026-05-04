import { useState, useRef, useCallback, useEffect, useMemo } from "react";
import { SelectProjectDir, SetWorkflowWorkingDir } from "../../../wailsjs/go/main/App";
import type { ChatMessage } from "./useAIAssistant";
import { findLastIndex, isPinnedNewsMessage, isImageFilePath, buildOutgoingMessageMulti } from "./useAIAssistant";
import { useVoiceInput, type VoiceInputSource } from "./useVoiceInput";
import { useWorkflowState } from "./useWorkflowState";
import { WorkflowDocPreview } from "./WorkflowDocPreview";
import { useCodePreviewState } from "./useCodePreviewState";
import { CodePreviewPanel, darkCodePreviewTheme, lightCodePreviewTheme } from "./CodePreviewPanel";
import { useBufferQueue } from "./useBufferQueue";
import type { AttachmentInfo } from "./useBufferQueue";
import { renderMessage } from "./aiAssistantMarkdown";
import { AI_PANEL_STATIC_STYLE_ID, AI_PANEL_STATIC_STYLE_TEXT, darkTheme, lightTheme, maximizedInlineStyle, overlayStyle, overlayTheme, type Theme } from "./aiAssistantPanelTheme";
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
import { AssistantInputStack } from "./AssistantInputStack";
import { AssistantWorkflowDocsBar } from "./AssistantWorkflowDocsBar";
import { AssistantInputComposer } from "./AssistantInputComposer";
import { AssistantWorkflowMaximizeSuggestion } from "./AssistantWorkflowMaximizeSuggestion";
import type { AIAssistantPanelProps } from "./aiAssistantPanelTypes";
import { useAssistantThemeMode } from "./useAssistantThemeMode";

/* Theme definitions live in aiAssistantPanelTheme.tsx. */

/* Project search UI lives in ProjectSearchPanel.tsx. */

/* Small AI panel controls live in aiAssistantControls.tsx. */

/* Themed inline markdown rendering lives in aiAssistantMarkdown.tsx. */

/* Inject static panel styles once at module level */
if (typeof document !== "undefined" && !document.getElementById(AI_PANEL_STATIC_STYLE_ID)) {
    const style = document.createElement("style");
    style.id = AI_PANEL_STATIC_STYLE_ID;
    style.textContent = AI_PANEL_STATIC_STYLE_TEXT;
    document.head.appendChild(style);
}

/* Main component */

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
    } = state;
    const {
        browseFile,
        clearSelectedFile,
        removeSelectedFile,
        sendMessage,
        clearHistory,
        recordSubmittedPrompt,
        setDraftInputValue,
        executeAction,
        refreshNews,
        onOpenOnboarding,
        cancelSession,
        onOpenTutorial,
        onTaskPrefsChanged,
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
    const [composing, setComposing] = useState(false);
    const [cancelPending, setCancelPending] = useState(false);
    const [editingEntryId, setEditingEntryId] = useState<string | null>(null);
    const inputRef = useRef<HTMLTextAreaElement | null>(null);
    const cancelRestoreSeqRef = useRef(0);
    const { themeMode, setThemeMode } = useAssistantThemeMode(controlledThemeMode, onThemeModeChange);
    const { ttsEnabled, setTtsEnabled } = useTTSReadback(audioOutputDeviceId);

    const { queue, addEntry, removeEntry, updateEntry, reorderEntry, mergeAndFire } = useBufferQueue();
    const { handlePaste, pendingAttachments, setPendingAttachments } = usePastedImageAttachments();
    const t = themeMode === 'dark' ? darkTheme : (inline ? lightTheme : overlayTheme);
    const showMaximizeToggle = inline && !!onToggleMaximize;


    const { state: workflowState, openDocPreview, closeDocPreview, setSplitRatio: setWorkflowSplitRatio, dismissMaximizeSuggestion, dismissDocsBar } = useWorkflowState();
    const { state: codePreviewState, closePanel: closeCodePreview, selectFile: selectCodeFile } = useCodePreviewState(workflowState.splitMode);
    const showWorkflowPreview = workflowState.splitMode;
    const showCodePreview = !showWorkflowPreview && codePreviewState.active;
    const anySplitActive = showWorkflowPreview || showCodePreview;
    const splitRatio = anySplitActive ? workflowState.splitRatio : 1;
    const startPreviewResize = useAssistantPreviewResize(setWorkflowSplitRatio);

    const title = lang === "en" ? "AI Assistant" : "AI \u52a9\u624b";
    const thinkingText = lang === "en" ? "Thinking..." : "\u6b63\u5728\u601d\u8003...";
    const processingText = lang === "en" ? "Executing tools and finishing task..." : "\u6b63\u5728\u6267\u884c\u5de5\u5177\u5e76\u5b8c\u6210\u4efb\u52a1...";
    const idlePlaceholderText = lang === "en" ? "Type a message..." : "\u8f93\u5165\u6d88\u606f...";
    const savedFileLabel = lang === "en" ? "Saved file" : "\u6587\u4ef6\u5df2\u4fdd\u5b58";
    const isBusy = sending;
    const inputLocked = isBusy || cancelPending;
    const submitLocked = inputLocked;
    const prevSubmitLockedRef = useRef(submitLocked);
    const showThinkingState = streaming;
    const showProcessingState = isBusy && !streaming;
    const showBusySpinner = isBusy;
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
                ? processingText
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
    const { handleScroll, outputContainerRef, outputEndRef, userScrolledUpRef } = useAssistantOutputScroll({ hasConversation, messages, ready, scrollToTopSeq });
    const { inputAreaHeight, resizeInput, startInputResize } = useResizableAssistantInput(inputRef, inputValue);

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

    const handleSelectWorkflowDir = useCallback(async () => {
        try {
            const dir = await SelectProjectDir();
            if (dir) SetWorkflowWorkingDir(dir);
        } catch (err) {
            console.error("Failed to set workflow working directory:", err);
        }
    }, []);
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
        if (submitLocked) {
            if (!text && pendingAttachments.length === 0 && selectedFilePaths.length === 0) return;
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
    }, [inputValue, submitLocked, pendingAttachments, selectedFilePaths, addEntry, recordSubmittedPrompt, resetHistoryBrowsing, updateInputValue, clearSelectedFile, sendMessage]);

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

    const handleEditEntry = useCallback((id: string) => setEditingEntryId(id), []);
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
        return otherMessages.map((msg: ChatMessage, idx: number) => renderMessage(msg, executeAction, t, idx === lastAssistantIdx, savedFileLabel));
    }, [otherMessages, executeAction, t, lastAssistantIdx, savedFileLabel]);

    const renderedProgressMessages = useMemo(() => {
        return progressMessages.map((msg: ChatMessage) => renderMessage(msg, executeAction, t, false, savedFileLabel));
    }, [progressMessages, executeAction, t, savedFileLabel]);

    const containerStyle: React.CSSProperties = inline
        ? (maximized
            ? maximizedInlineStyle
            : { display: "flex", flexDirection: "column", background: t.bg, textAlign: "left", width: "100%", height: "100%", position: "relative" })
        : overlayStyle;

    return (
        <div data-testid="ai-panel-root" style={{ ...containerStyle, flexDirection: "row" }}>
            <div style={{ display: "flex", flexDirection: "column", flex: splitRatio, minWidth: 0, height: "100%" }}>
            {/* Drag overlay (inline mode) */}
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
                    processingText={processingText}
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
                composing={composing}
                dismissDocsBar={dismissDocsBar}
                editingEntryId={editingEntryId}
                exitHistoryBrowsing={exitHistoryBrowsing}
                finishVoicePointer={finishVoicePointer}
                handleCancel={handleCancel}
                handleCancelEdit={handleCancelEdit}
                handleEditEntry={handleEditEntry}
                handlePaste={handlePaste}
                handleSaveEdit={handleSaveEdit}
                handleSelectWorkflowDir={handleSelectWorkflowDir}
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
                openDocPreview={openDocPreview}
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
                setComposing={setComposing}
                setPendingAttachments={setPendingAttachments}
                showBusySpinner={showBusySpinner}
                startInputResize={startInputResize}
                theme={t}
                themeMode={themeMode}
                updateInputValue={updateInputValue}
                voiceInput={voiceInput}
                workflowState={workflowState}
            />
            </div>
            {showWorkflowPreview && (
                <div style={{ flex: Math.max(0.2, 1 - splitRatio), minWidth: 0, height: "100%" }}>
                    <WorkflowDocPreview
                        phaseDocuments={workflowState.phaseDocuments}
                        currentPhaseID={workflowState.currentPhaseID}
                        gateResults={workflowState.gateResults}
                        onClose={closeDocPreview}
                        onToggleMaximize={inline ? onToggleMaximize : undefined}
                        onResizeStart={startPreviewResize}
                        theme={{
                            bg: t.bg,
                            text: t.text,
                            textMuted: t.textMuted,
                            border: t.divider,
                            headerBg: t.titleBarBg,
                            accentColor: t.headingColor,
                            accentBg: themeMode === 'dark' ? "rgba(99,102,241,0.15)" : "rgba(99,102,241,0.08)",
                            codeBg: t.codeBg,
                            codeText: t.codeText,
                            codeBlockBg: t.codeBlockBg,
                            codeBlockBorder: t.codeBlockBorder,
                            headingColor: t.headingColor,
                            linkColor: t.linkColor,
                            quoteBorder: t.quoteBorder,
                            quoteText: t.quoteText,
                            quoteBg: themeMode === 'dark' ? "rgba(99,102,241,0.08)" : "rgba(99,102,241,0.04)",
                        }}
                    />
                </div>
            )}
            {showCodePreview && (
                <div style={{ flex: Math.max(0.2, 1 - splitRatio), minWidth: 0, height: "100%" }}>
                    <CodePreviewPanel
                        files={codePreviewState.files}
                        activeFilePath={codePreviewState.activeFilePath}
                        onSelectFile={selectCodeFile}
                        onClose={closeCodePreview}
                        onResizeStart={startPreviewResize}
                        onToggleMaximize={inline ? onToggleMaximize : undefined}
                        theme={themeMode === 'dark' ? darkCodePreviewTheme : lightCodePreviewTheme}
                    />
                </div>
            )}
        </div>
    );
}
