import { useCallback, useEffect, useId, useMemo, useRef } from "react";
import { imageCountsForHistoryMatches } from "./promptHistory";
import { AssistantAttachmentsStrip } from "./AssistantAttachmentsStrip";
import { AssistantInputActionsLeft, AssistantInputActionsRight } from "./AssistantInputActions";
import { getAssistantInputComposerStyles } from "./AssistantInputComposerStyles";
import type { AssistantInputComposerProps } from "./AssistantInputComposerTypes";
import { InputHistoryAutocomplete } from "./InputHistoryAutocomplete";
import { useBufferedComposerValue } from "./useBufferedComposerValue";
import { useInputHistoryAutocomplete } from "./useInputHistoryAutocomplete";
import { useTextCompositionGuard } from "./useTextCompositionGuard";
import { MemoryUsageRing } from "./MemoryUsageRing";
import { asTextareaRef, insertTextareaLineBreak, isLineBreakShortcut, isPlainEnter } from "./assistantInputShortcuts";
import { tailText } from "../../i18n/mnTail"
import { COMPOSER_SHORTCUT_HINT_FONT, COMPOSER_SHORTCUT_MEASURE_HOST_STYLE, COMPOSER_SHORTCUT_MEASURE_STYLE, composerShortcutHint, useComposerShortcutHintVisible } from "./composerShortcutHint"

const EMPTY_SUBMITTED_PROMPTS: string[] = [];

export function AssistantInputComposer(props: AssistantInputComposerProps) {
    const {
        active = true, attachButtonTestId, browseFile, canSend, cancelPending, cancelSession, cancelTitle, clearSelectedFile, composeAction,
        exitHistoryBrowsing, finishVoicePointer, handleCancel, handleClearInput, handleDragOver, handleDrop, handlePaste,
        handleSend, handleTextareaClick, handleTextareaKeyDownBefore, handleTextareaKeyUp, handleVoiceClick,
        handleVoicePointerDown, handleVoicePointerLeave, inputAreaHeight, inputBarTestId = "ai-input-bar", inputLocked,
        hardLockInput = false,
        inputOverlay, inputRef, inputRowTestId = "ai-input-row", inputValue, inline, flushBottom = false, isBusy, isSelectionCollapsedAtBoundary,
        lang, onComposeActionChange, onFireSlashCommand, onInsertTemplate, onPlusMenuAction, pendingAttachments,
        pendingAttachmentsTestId, permissionMode, taskCommandAllowed, showPermissionMode, showWorkspacePermissionOption, onPermissionModeChange, placeholderText, ready, recallHistory, rememberHistoryEdit, removeSelectedFile,
        resizeInput, selectedFilePaths, sendButtonStyle, sendButtonTestId, setPendingAttachments, showBusySpinner,
        showMemoryUsage = true, showVoiceInput = true, submittedPrompts: submittedPromptsProp, submittedPromptAttachments, onHistoryTextAccepted, textareaAriaLabel, textareaTestId = "ai-input",
        theme: t, themeMode, toolbarAddon, toolbarEndContent, toolbarTestId = "ai-input-toolbar", trailingToolbarContent, updateInputValue, voiceInput,
    } = props;

    // Stable empty fallback — default param `= []` would allocate a new array every render.
    const submittedPrompts = submittedPromptsProp ?? EMPTY_SUBMITTED_PROMPTS;

    const textComposition = useTextCompositionGuard();
    const buffered = useBufferedComposerValue(inputValue, useCallback((next: string) => {
        rememberHistoryEdit(next);
        updateInputValue(next, { deferParent: true });
    }, [rememberHistoryEdit, updateInputValue]));
    // State (not ref) so IME start/end re-renders and keeps matches closed during composition.
    const isComposing = buffered.isComposing;
    const displayedInputValue = buffered.value;
    const listboxDomId = useId();
    const historyListboxId = `ai-input-history-listbox-${listboxDomId}`;
    const isExpandedInput = inputAreaHeight !== null;
    const textareaRef = asTextareaRef(inputRef);

    const applyAutocompleteValue = useCallback((next: string) => {
        rememberHistoryEdit(next);
        updateInputValue(next);
        onHistoryTextAccepted?.(next);
        requestAnimationFrame(() => resizeInput());
    }, [onHistoryTextAccepted, rememberHistoryEdit, resizeInput, updateInputValue]);

    const insertLineBreak = useCallback((textarea: HTMLTextAreaElement) => {
        insertTextareaLineBreak(textarea, (next) => {
            rememberHistoryEdit(next);
            updateInputValue(next);
        }, resizeInput);
    }, [rememberHistoryEdit, resizeInput, updateInputValue]);

    const inputHardDisabled = !ready || cancelPending || hardLockInput;
    const autocompleteDisabled = inputHardDisabled || isComposing;
    const autocomplete = useInputHistoryAutocomplete({
        inputValue: displayedInputValue,
        submittedPrompts,
        applyInputValue: applyAutocompleteValue,
        inputRef: textareaRef,
        disabled: autocompleteDisabled,
    });

    const autocompleteOpen = autocomplete.open;
    const toolbarRef = useRef<HTMLDivElement>(null);
    const shortcutHint = composerShortcutHint(lang);
    const shortcutHintFits = useComposerShortcutHintVisible(toolbarRef, shortcutHint);
    const historyImageCounts = useMemo(
        () => imageCountsForHistoryMatches(submittedPrompts, submittedPromptAttachments, autocomplete.matches),
        [autocomplete.matches, submittedPromptAttachments, submittedPrompts],
    );

    const { inputBarStyle, inputRowStyle, textareaStyle, toolbarStyle, toolbarLeftStyle, toolbarRightStyle } = useMemo(
        () => getAssistantInputComposerStyles({
            cancelPending,
            hasInputOverlay: !!inputOverlay || autocompleteOpen,
            inline,
            flushBottom,
            isExpandedInput,
            ready,
            theme: t,
        }),
        [autocompleteOpen, cancelPending, flushBottom, inline, inputOverlay, isExpandedInput, ready, t],
    );

    useEffect(() => {
        resizeInput(textareaRef.current ?? undefined);
    }, [displayedInputValue, resizeInput, textareaRef]);

    return (
        <div data-testid={inputBarTestId} className="ai-composer" style={inputBarStyle} onDragOver={handleDragOver} onDrop={handleDrop}>
            <AssistantAttachmentsStrip
                cancelPending={cancelPending}
                clearSelectedFile={clearSelectedFile}
                lang={lang}
                pendingAttachments={pendingAttachments}
                pendingAttachmentsTestId={pendingAttachmentsTestId}
                removeSelectedFile={removeSelectedFile}
                selectedFilePaths={selectedFilePaths}
                setPendingAttachments={setPendingAttachments}
                theme={t}
            />
            <div data-testid={inputRowTestId} style={{ ...inputRowStyle, position: "relative", overflow: (autocompleteOpen || !!inputOverlay) ? "visible" : "hidden" }}>
                {inputOverlay}
                <InputHistoryAutocomplete
                    open={autocompleteOpen}
                    matches={autocomplete.matches}
                    selectedIndex={autocomplete.selectedIndex}
                    prefix={displayedInputValue}
                    imageCounts={historyImageCounts}
                    listboxId={historyListboxId}
                    onSelectIndex={autocomplete.setSelectedIndex}
                    onAccept={autocomplete.accept}
                    theme={t}
                    lang={lang}
                />
                <textarea
                    ref={inputRef}
                    data-testid={textareaTestId}
                    disabled={inputHardDisabled}
                    readOnly={cancelPending || hardLockInput}
                    aria-label={textareaAriaLabel || placeholderText}
                    aria-readonly={cancelPending || hardLockInput}
                    aria-autocomplete="list"
                    aria-expanded={autocompleteOpen}
                    aria-controls={autocompleteOpen ? historyListboxId : undefined}
                    aria-activedescendant={autocompleteOpen ? `${historyListboxId}-option-${autocomplete.selectedIndex}` : undefined}
                    style={{ ...textareaStyle, boxSizing: "border-box" }}
                    rows={1}
                    value={displayedInputValue}
                    onChange={buffered.onChange}
                    onPaste={handlePaste}
                    onClick={handleTextareaClick}
                    onKeyUp={handleTextareaKeyUp}
                    onCompositionStart={(e) => {
                        // Hide suggestions while composing; sticky Esc-dismiss is preserved.
                        buffered.onCompositionStart(e);
                        textComposition.onCompositionStart();
                    }}
                    onCompositionEnd={(e) => {
                        buffered.onCompositionEnd(e);
                        textComposition.onCompositionEnd();
                    }}
                    onKeyDown={(e) => {
                        if (textComposition.shouldIgnoreKeyDown(e)) return;
                        if (handleTextareaKeyDownBefore?.(e)) return;
                        // Prefix autocomplete takes priority over send and ↑↓ history browse.
                        if (autocomplete.handleKeyDown(e)) return;
                        if (e.key === "ArrowUp" && isSelectionCollapsedAtBoundary("up")) {
                            if (recallHistory("up")) { e.preventDefault(); return; }
                        }
                        if (e.key === "ArrowDown" && isSelectionCollapsedAtBoundary("down")) {
                            if (recallHistory("down")) { e.preventDefault(); return; }
                        }
                        if (e.key === "Escape") {
                            if (exitHistoryBrowsing()) e.preventDefault();
                            return;
                        }
                        // Some embedded WebViews do not insert a newline for Ctrl/Cmd+Enter
                        // in a controlled textarea, so make the requested shortcut explicit.
                        if (isLineBreakShortcut(e)) {
                            e.preventDefault();
                            insertLineBreak(e.currentTarget);
                            return;
                        }
                        // Plain Enter sends. Modifier combinations keep the textarea's
                        // native multiline behavior, including Shift+Enter for a newline.
                        if (isPlainEnter(e)) {
                            e.preventDefault();
                            handleSend();
                        }
                    }}
                    placeholder={placeholderText}
                    autoCapitalize="off"
                    autoCorrect="off"
                    spellCheck={false}
                />
            </div>
            <div ref={toolbarRef} data-testid={toolbarTestId} style={toolbarStyle}>
                <div data-toolbar-slot="start" style={toolbarLeftStyle} role="group" aria-label={tailText(lang ?? "", "Input actions", "\u8f93\u5165\u64cd\u4f5c")}>
                    <AssistantInputActionsLeft
                        active={active}
                        attachButtonTestId={attachButtonTestId}
                        browseFile={browseFile}
                        composeAction={composeAction}
                        inputLocked={inputLocked}
                        lang={lang}
                        onComposeActionChange={onComposeActionChange}
                        onPermissionModeChange={onPermissionModeChange}
                        onFireSlashCommand={onFireSlashCommand}
                        onInsertTemplate={onInsertTemplate}
                        onPlusMenuAction={onPlusMenuAction}
                        ready={ready}
                        theme={t}
                        themeMode={themeMode}
                        voiceInput={voiceInput}
                        permissionMode={permissionMode}
                        taskCommandAllowed={taskCommandAllowed}
                        showPermissionMode={showPermissionMode}
                        showWorkspacePermissionOption={showWorkspacePermissionOption}
                        showVoiceInput={showVoiceInput}
                        handleVoiceClick={handleVoiceClick}
                        handleVoicePointerDown={handleVoicePointerDown}
                        handleVoicePointerLeave={handleVoicePointerLeave}
                        finishVoicePointer={finishVoicePointer}
                    />
                    {trailingToolbarContent}
                    {toolbarAddon ? (
                        // An expanded config bar wraps inside the left group.
                        // It does not decide whether the shortcut sentence fits.
                        <div data-testid="ai-input-toolbar-addon" className="aic-toolbar-addon">
                            {toolbarAddon}
                        </div>
                    ) : null}
                </div>
                {toolbarEndContent ? (
                    // Sibling of the wrapping action group so a long working-directory
                    // chip cannot push the model switcher onto the next row.
                    <div data-testid="ai-input-toolbar-end" style={{ marginLeft: "auto", flexShrink: 0, minWidth: 0, maxWidth: "46%" }}>
                        {toolbarEndContent}
                    </div>
                ) : null}
                <div data-toolbar-slot="trailing" style={toolbarRightStyle}>
                    {showMemoryUsage && <MemoryUsageRing theme={t} themeMode={themeMode} lang={lang} size={20} />}
                    <span data-shortcut-measure-host="" aria-hidden="true" style={COMPOSER_SHORTCUT_MEASURE_HOST_STYLE}>
                        <span data-shortcut-measure="" style={COMPOSER_SHORTCUT_MEASURE_STYLE}>{shortcutHint}</span>
                    </span>
                    {shortcutHintFits ? (
                        <span data-shortcut-hint="" data-testid="ai-composer-shortcut-hint" style={{ ...COMPOSER_SHORTCUT_HINT_FONT, color: t.textMuted, userSelect: "none", flexShrink: 0 }}>
                            {shortcutHint}
                        </span>
                    ) : null}
                    <AssistantInputActionsRight
                        canSend={canSend}
                        cancelSession={cancelSession}
                        cancelTitle={cancelTitle}
                        handleCancel={handleCancel}
                        handleClearInput={handleClearInput}
                        handleSend={handleSend}
                        inputValue={inputValue}
                        isBusy={isBusy}
                        lang={lang}
                        ready={ready}
                        sendButtonStyle={sendButtonStyle}
                        sendButtonTestId={sendButtonTestId}
                        sendShortcutTitle={shortcutHintFits ? undefined : shortcutHint}
                        showBusySpinner={showBusySpinner}
                        theme={t}
                        themeMode={themeMode}
                    />
                </div>
            </div>
        </div>
    );
}
