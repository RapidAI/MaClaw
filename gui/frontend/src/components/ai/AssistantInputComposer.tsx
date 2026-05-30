import { AssistantAttachmentsStrip } from "./AssistantAttachmentsStrip";
import { AssistantInputActionsLeft, AssistantInputActionsRight } from "./AssistantInputActions";
import { getAssistantInputComposerStyles } from "./AssistantInputComposerStyles";
import type { AssistantInputComposerProps } from "./AssistantInputComposerTypes";
import { useTextCompositionGuard } from "./useTextCompositionGuard";
import { MemoryUsageRing } from "./MemoryUsageRing";

export function AssistantInputComposer(props: AssistantInputComposerProps) {
    const { attachButtonTestId, browseFile, canSend, cancelPending, cancelSession, clearSelectedFile, exitHistoryBrowsing, finishVoicePointer, handleCancel, handlePaste, handleSend, handleTextareaClick, handleTextareaKeyDownBefore, handleTextareaKeyUp, handleVoiceClick, handleVoicePointerDown, handleVoicePointerLeave, inputAreaHeight, inputBarTestId = "ai-input-bar", inputLocked, inputOverlay, inputRef, inputRowTestId = "ai-input-row", inputValue, inline, isBusy, isSelectionCollapsedAtBoundary, lang, pendingAttachments, pendingAttachmentsTestId, placeholderText, ready, recallHistory, rememberHistoryEdit, removeSelectedFile, resizeInput, selectedFilePaths, sendButtonStyle, sendButtonTestId, setPendingAttachments, showBusySpinner, showMemoryUsage = true, showVoiceInput = true, textareaTestId = "ai-input", theme: t, themeMode, toolbarTestId = "ai-input-toolbar", updateInputValue, voiceInput } = props;
    const textComposition = useTextCompositionGuard();
    const isExpandedInput = inputAreaHeight !== null;
    const { inputBarStyle, inputRowStyle, textareaStyle, toolbarStyle, toolbarLeftStyle, toolbarRightStyle } = getAssistantInputComposerStyles({
        cancelPending,
        inline,
        isExpandedInput,
        ready,
        theme: t,
    });

    return (
        <div data-testid={inputBarTestId} style={inputBarStyle}>
            <AssistantAttachmentsStrip cancelPending={cancelPending} clearSelectedFile={clearSelectedFile} lang={lang} pendingAttachments={pendingAttachments} pendingAttachmentsTestId={pendingAttachmentsTestId} removeSelectedFile={removeSelectedFile} selectedFilePaths={selectedFilePaths} setPendingAttachments={setPendingAttachments} theme={t} />
            {/* Textarea area */}
            <div data-testid={inputRowTestId} style={{ ...inputRowStyle, position: "relative" }}>
                {inputOverlay}
                {/* Main AI composer guard: data-testid="ai-input" */}
                {/* data-testid="ai-input" is provided by textareaTestId default. */}
                <textarea ref={inputRef} data-testid={textareaTestId} disabled={!ready || cancelPending} readOnly={cancelPending} aria-readonly={cancelPending} style={{ ...textareaStyle, boxSizing: "border-box" }} rows={1} value={inputValue} onChange={(e) => { rememberHistoryEdit(e.target.value); updateInputValue(e.target.value); resizeInput(); }} onPaste={handlePaste} onClick={handleTextareaClick} onKeyUp={handleTextareaKeyUp} onCompositionStart={textComposition.onCompositionStart} onCompositionEnd={textComposition.onCompositionEnd} onKeyDown={(e) => {
                    if (textComposition.shouldIgnoreKeyDown(e)) return;
                    if (handleTextareaKeyDownBefore?.(e)) return;
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
                    if (e.key === "Enter" && !e.shiftKey) {
                        e.preventDefault();
                        handleSend();
                    }
                }} placeholder={placeholderText} autoCapitalize="off" autoCorrect="off" spellCheck={false} />
            </div>
            {/* Inline toolbar: buttons embedded at the bottom of the input box */}
            <div data-testid={toolbarTestId} style={toolbarStyle}>
                <div style={toolbarLeftStyle} role="group" aria-label={lang?.startsWith("zh") ? "\u8f93\u5165\u64cd\u4f5c" : "Input actions"}>
                    <AssistantInputActionsLeft attachButtonTestId={attachButtonTestId} browseFile={browseFile} inputLocked={inputLocked} lang={lang} ready={ready} theme={t} themeMode={themeMode} voiceInput={voiceInput} showVoiceInput={showVoiceInput} handleVoiceClick={handleVoiceClick} handleVoicePointerDown={handleVoicePointerDown} handleVoicePointerLeave={handleVoicePointerLeave} finishVoicePointer={finishVoicePointer} />
                </div>
                <div style={toolbarRightStyle}>
                    {showMemoryUsage && <MemoryUsageRing theme={t} themeMode={themeMode} lang={lang} size={20} />}
                    <span aria-hidden="true" style={{ fontSize: "11px", color: t.textMuted, userSelect: "none", whiteSpace: "nowrap" }}>
                        {lang?.startsWith("zh") ? "Enter \u53d1\u9001" : "Enter to send"}
                    </span>
                    <AssistantInputActionsRight canSend={canSend} cancelSession={cancelSession} handleCancel={handleCancel} handleSend={handleSend} isBusy={isBusy} lang={lang} ready={ready} sendButtonStyle={sendButtonStyle} sendButtonTestId={sendButtonTestId} showBusySpinner={showBusySpinner} theme={t} themeMode={themeMode} />
                </div>
            </div>
        </div>
    );
}
