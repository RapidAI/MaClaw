import type { ClipboardEvent, Dispatch, PointerEvent, Ref, SetStateAction } from "react";
import { AssistantAttachmentsStrip } from "./AssistantAttachmentsStrip";
import { AssistantInputActionsLeft, AssistantInputActionsRight } from "./AssistantInputActions";
import { getAssistantInputComposerStyles } from "./AssistantInputComposerStyles";
import type { AttachmentInfo } from "./useBufferQueue";
import type { UseVoiceInputResult } from "./useVoiceInput";
import type { Theme } from "./aiAssistantPanelTheme";
import { useTextCompositionGuard } from "./useTextCompositionGuard";

interface AssistantInputComposerProps {
    browseFile: () => void;
    canSend: boolean;
    cancelPending: boolean;
    cancelSession?: unknown;
    clearSelectedFile?: () => void;
    exitHistoryBrowsing: () => boolean;
    finishVoicePointer: (event: PointerEvent<HTMLButtonElement>) => void;
    handleCancel: () => void;
    handlePaste: (event: ClipboardEvent<HTMLTextAreaElement>) => void;
    handleSend: () => void;
    handleVoiceClick: () => void;
    handleVoicePointerDown: (event: PointerEvent<HTMLButtonElement>) => void;
    handleVoicePointerLeave: (event: PointerEvent<HTMLButtonElement>) => void;
    inputAreaHeight: number | null;
    inputLocked: boolean;
    inputRef: Ref<HTMLTextAreaElement>;
    inputValue: string;
    inline: boolean;
    isBusy: boolean;
    isSelectionCollapsedAtBoundary: (direction: "up" | "down") => boolean;
    lang: string;
    pendingAttachments: AttachmentInfo[];
    placeholderText: string;
    ready: boolean;
    recallHistory: (direction: "up" | "down") => boolean;
    rememberHistoryEdit: (value: string) => void;
    removeSelectedFile?: (index: number) => void;
    resizeInput: () => void;
    selectedFilePaths: string[];
    setPendingAttachments: Dispatch<SetStateAction<AttachmentInfo[]>>;
    showBusySpinner: boolean;
    theme: Theme;
    themeMode: "light" | "dark";
    updateInputValue: (value: string) => void;
    voiceInput: UseVoiceInputResult;
}

export function AssistantInputComposer(props: AssistantInputComposerProps) {
    const { browseFile, canSend, cancelPending, cancelSession, clearSelectedFile, exitHistoryBrowsing, finishVoicePointer, handleCancel, handlePaste, handleSend, handleVoiceClick, handleVoicePointerDown, handleVoicePointerLeave, inputAreaHeight, inputLocked, inputRef, inputValue, inline, isBusy, isSelectionCollapsedAtBoundary, lang, pendingAttachments, placeholderText, ready, recallHistory, rememberHistoryEdit, removeSelectedFile, resizeInput, selectedFilePaths, setPendingAttachments, showBusySpinner, theme: t, themeMode, updateInputValue, voiceInput } = props;
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
        <div data-testid="ai-input-bar" style={inputBarStyle}>
            <AssistantAttachmentsStrip cancelPending={cancelPending} clearSelectedFile={clearSelectedFile} lang={lang} pendingAttachments={pendingAttachments} removeSelectedFile={removeSelectedFile} selectedFilePaths={selectedFilePaths} setPendingAttachments={setPendingAttachments} theme={t} />
            {/* Textarea area */}
            <div data-testid="ai-input-row" style={inputRowStyle}>
                <textarea ref={inputRef} data-testid="ai-input" disabled={!ready || cancelPending} readOnly={cancelPending} aria-readonly={cancelPending} style={textareaStyle} rows={1} value={inputValue} onChange={(e) => { rememberHistoryEdit(e.target.value); updateInputValue(e.target.value); resizeInput(); }} onPaste={handlePaste} onCompositionStart={textComposition.onCompositionStart} onCompositionEnd={textComposition.onCompositionEnd} onKeyDown={(e) => {
                    if (textComposition.shouldIgnoreKeyDown(e)) return;
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
            {/* Bottom toolbar: [attach voice] ---- [hint] [send/cancel] */}
            <div data-testid="ai-input-toolbar" style={toolbarStyle}>
                <div style={toolbarLeftStyle} role="group" aria-label={lang?.startsWith("zh") ? "输入操作" : "Input actions"}>
                    <AssistantInputActionsLeft browseFile={browseFile} inputLocked={inputLocked} lang={lang} ready={ready} theme={t} themeMode={themeMode} voiceInput={voiceInput} handleVoiceClick={handleVoiceClick} handleVoicePointerDown={handleVoicePointerDown} handleVoicePointerLeave={handleVoicePointerLeave} finishVoicePointer={finishVoicePointer} />
                </div>
                <div style={toolbarRightStyle}>
                    <span aria-hidden="true" style={{ fontSize: "11px", color: t.textMuted, userSelect: "none", whiteSpace: "nowrap" }}>
                        {lang?.startsWith("zh") ? "Enter 发送" : "Enter to send"}
                    </span>
                    <AssistantInputActionsRight canSend={canSend} cancelSession={cancelSession} handleCancel={handleCancel} handleSend={handleSend} isBusy={isBusy} lang={lang} ready={ready} showBusySpinner={showBusySpinner} theme={t} themeMode={themeMode} />
                </div>
            </div>
        </div>
    );
}
