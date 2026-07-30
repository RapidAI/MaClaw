import { useCallback, useId, useMemo, useState, type RefObject } from "react";
import { AssistantAttachmentsStrip } from "./AssistantAttachmentsStrip";
import { AssistantInputActionsLeft, AssistantInputActionsRight } from "./AssistantInputActions";
import { getAssistantInputComposerStyles } from "./AssistantInputComposerStyles";
import type { AssistantInputComposerProps } from "./AssistantInputComposerTypes";
import { InputHistoryAutocomplete } from "./InputHistoryAutocomplete";
import { useInputHistoryAutocomplete } from "./useInputHistoryAutocomplete";
import { useTextCompositionGuard } from "./useTextCompositionGuard";
import { MemoryUsageRing } from "./MemoryUsageRing";
import { insertTextareaLineBreak, isLineBreakShortcut, isPlainEnter } from "./assistantInputShortcuts";

const EMPTY_TEXTAREA_REF: RefObject<HTMLTextAreaElement | null> = { current: null };
const EMPTY_SUBMITTED_PROMPTS: string[] = [];

function asTextareaRef(ref: AssistantInputComposerProps["inputRef"]): RefObject<HTMLTextAreaElement | null> {
    if (ref && typeof ref === "object" && "current" in ref) {
        return ref as RefObject<HTMLTextAreaElement | null>;
    }
    return EMPTY_TEXTAREA_REF;
}

export function AssistantInputComposer(props: AssistantInputComposerProps) {
    const {
        active = true, attachButtonTestId, browseFile, canSend, cancelPending, cancelSession, clearSelectedFile, composeAction,
        exitHistoryBrowsing, finishVoicePointer, handleCancel, handleClearInput, handleDragOver, handleDrop, handlePaste,
        handleSend, handleTextareaClick, handleTextareaKeyDownBefore, handleTextareaKeyUp, handleVoiceClick,
        handleVoicePointerDown, handleVoicePointerLeave, inputAreaHeight, inputBarTestId = "ai-input-bar", inputLocked,
        hardLockInput = false,
        inputOverlay, inputRef, inputRowTestId = "ai-input-row", inputValue, inline, flushBottom = false, isBusy, isSelectionCollapsedAtBoundary,
        lang, onComposeActionChange, onFireSlashCommand, onInsertTemplate, onPlusMenuAction, pendingAttachments,
        pendingAttachmentsTestId, permissionMode, showPermissionMode, showWorkspacePermissionOption, onPermissionModeChange, placeholderText, ready, recallHistory, rememberHistoryEdit, removeSelectedFile,
        resizeInput, selectedFilePaths, sendButtonStyle, sendButtonTestId, setPendingAttachments, showBusySpinner,
        showMemoryUsage = true, showVoiceInput = true, submittedPrompts: submittedPromptsProp, textareaAriaLabel, textareaTestId = "ai-input",
        theme: t, themeMode, toolbarTestId = "ai-input-toolbar", updateInputValue, voiceInput,
    } = props;

    // Stable empty fallback — default param `= []` would allocate a new array every render.
    const submittedPrompts = submittedPromptsProp ?? EMPTY_SUBMITTED_PROMPTS;

    const textComposition = useTextCompositionGuard();
    // State (not ref) so IME start/end re-renders and keeps matches closed during composition.
    const [isComposing, setIsComposing] = useState(false);
    const listboxDomId = useId();
    const historyListboxId = `ai-input-history-listbox-${listboxDomId}`;
    const isExpandedInput = inputAreaHeight !== null;
    const textareaRef = asTextareaRef(inputRef);

    const applyAutocompleteValue = useCallback((next: string) => {
        rememberHistoryEdit(next);
        updateInputValue(next);
        requestAnimationFrame(() => resizeInput());
    }, [rememberHistoryEdit, resizeInput, updateInputValue]);

    const insertLineBreak = useCallback((textarea: HTMLTextAreaElement) => {
        insertTextareaLineBreak(textarea, (next) => {
            rememberHistoryEdit(next);
            updateInputValue(next);
        }, resizeInput);
    }, [rememberHistoryEdit, resizeInput, updateInputValue]);

    const inputHardDisabled = !ready || cancelPending || hardLockInput;
    const autocompleteDisabled = inputHardDisabled || isComposing;
    const autocomplete = useInputHistoryAutocomplete({
        inputValue,
        submittedPrompts,
        applyInputValue: applyAutocompleteValue,
        inputRef: textareaRef,
        disabled: autocompleteDisabled,
    });

    const autocompleteOpen = autocomplete.open;

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

    return (
        <div data-testid={inputBarTestId} style={inputBarStyle} onDragOver={handleDragOver} onDrop={handleDrop}>
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
                    prefix={inputValue}
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
                    value={inputValue}
                    onChange={(e) => {
                        rememberHistoryEdit(e.target.value);
                        updateInputValue(e.target.value);
                        resizeInput();
                    }}
                    onPaste={handlePaste}
                    onClick={handleTextareaClick}
                    onKeyUp={handleTextareaKeyUp}
                    onCompositionStart={() => {
                        // Hide suggestions while composing; sticky Esc-dismiss is preserved.
                        setIsComposing(true);
                        textComposition.onCompositionStart();
                    }}
                    onCompositionEnd={() => {
                        setIsComposing(false);
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
            <div data-testid={toolbarTestId} style={toolbarStyle}>
                <div style={toolbarLeftStyle} role="group" aria-label={lang?.startsWith("zh") ? "\u8f93\u5165\u64cd\u4f5c" : "Input actions"}>
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
                        showPermissionMode={showPermissionMode}
                        showWorkspacePermissionOption={showWorkspacePermissionOption}
                        showVoiceInput={showVoiceInput}
                        handleVoiceClick={handleVoiceClick}
                        handleVoicePointerDown={handleVoicePointerDown}
                        handleVoicePointerLeave={handleVoicePointerLeave}
                        finishVoicePointer={finishVoicePointer}
                    />
                </div>
                <div style={toolbarRightStyle}>
                    {showMemoryUsage && <MemoryUsageRing theme={t} themeMode={themeMode} lang={lang} size={20} />}
                    <span aria-hidden="true" style={{ fontSize: "11px", color: t.textMuted, userSelect: "none", whiteSpace: "nowrap" }}>
                        {lang?.startsWith("zh") ? "Enter \u53d1\u9001 · Ctrl/⌘+Enter \u6362\u884c" : "Enter to send · Ctrl/⌘+Enter for a new line"}
                    </span>
                    <AssistantInputActionsRight
                        canSend={canSend}
                        cancelSession={cancelSession}
                        handleCancel={handleCancel}
                        handleClearInput={handleClearInput}
                        handleSend={handleSend}
                        inputValue={inputValue}
                        isBusy={isBusy}
                        lang={lang}
                        ready={ready}
                        sendButtonStyle={sendButtonStyle}
                        sendButtonTestId={sendButtonTestId}
                        showBusySpinner={showBusySpinner}
                        theme={t}
                        themeMode={themeMode}
                    />
                </div>
            </div>
        </div>
    );
}
