import type { ClipboardEvent, Dispatch, PointerEvent, Ref, SetStateAction } from "react";
import { AssistantAttachmentsStrip } from "./AssistantAttachmentsStrip";
import { AssistantInputActions } from "./AssistantInputActions";
import type { AttachmentInfo } from "./useBufferQueue";
import type { UseVoiceInputResult } from "./useVoiceInput";
import type { Theme } from "./aiAssistantPanelTheme";

interface AssistantInputComposerProps {
    browseFile: () => void;
    canSend: boolean;
    cancelPending: boolean;
    cancelSession?: unknown;
    clearSelectedFile?: () => void;
    composing: boolean;
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
    setComposing: (value: boolean) => void;
    setPendingAttachments: Dispatch<SetStateAction<AttachmentInfo[]>>;
    showBusySpinner: boolean;
    theme: Theme;
    themeMode: "light" | "dark";
    updateInputValue: (value: string) => void;
    voiceInput: UseVoiceInputResult;
}

export function AssistantInputComposer(props: AssistantInputComposerProps) {
    const { browseFile, canSend, cancelPending, cancelSession, clearSelectedFile, composing, exitHistoryBrowsing, finishVoicePointer, handleCancel, handlePaste, handleSend, handleVoiceClick, handleVoicePointerDown, handleVoicePointerLeave, inputAreaHeight, inputLocked, inputRef, inputValue, inline, isBusy, isSelectionCollapsedAtBoundary, lang, pendingAttachments, placeholderText, ready, recallHistory, rememberHistoryEdit, removeSelectedFile, resizeInput, selectedFilePaths, setComposing, setPendingAttachments, showBusySpinner, theme: t, themeMode, updateInputValue, voiceInput } = props;
    return (
        <div style={{ display: "flex", flexDirection: "column", gap: "6px", padding: "8px 12px", paddingBottom: "max(8px, env(safe-area-inset-bottom))", background: t.inputBarBg, borderTop: inline ? `1px solid ${t.inputBarBorder}` : "none", flexShrink: 0, ...(inline ? {} : { margin: "0 10px 10px 10px", borderRadius: "8px", border: `1.5px solid ${t.inputBarBorder}` }) }}>
            <AssistantAttachmentsStrip cancelPending={cancelPending} clearSelectedFile={clearSelectedFile} lang={lang} pendingAttachments={pendingAttachments} removeSelectedFile={removeSelectedFile} selectedFilePaths={selectedFilePaths} setPendingAttachments={setPendingAttachments} theme={t} />
            <div style={{ display: "flex", alignItems: "flex-end", gap: "8px" }}>
                <span style={{ color: t.promptColor, fontFamily: "Consolas, monospace", fontSize: "13px", flexShrink: 0, userSelect: "none", paddingBottom: "8px" }}>{">"}</span>
                <textarea ref={inputRef} data-testid="ai-input" disabled={!ready || cancelPending} readOnly={cancelPending} aria-readonly={cancelPending} style={{ flex: 1, minWidth: 0, background: "transparent", border: "none", outline: "none", color: t.inputText, fontFamily: "Consolas, 'Courier New', monospace", fontSize: "14px", padding: "8px 0", resize: "none", overflow: "auto", minHeight: "36px", maxHeight: inputAreaHeight ? `${inputAreaHeight}px` : "120px", lineHeight: 1.4, opacity: (!ready || cancelPending) ? 0.5 : 1, cursor: cancelPending ? "default" : "text" }} rows={1} value={inputValue} onChange={(e) => { rememberHistoryEdit(e.target.value); updateInputValue(e.target.value); resizeInput(); }} onPaste={handlePaste} onCompositionStart={() => setComposing(true)} onCompositionEnd={() => setComposing(false)} onKeyDown={(e) => {
                    if (composing) return;
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
                <AssistantInputActions browseFile={browseFile} canSend={canSend} cancelSession={cancelSession} finishVoicePointer={finishVoicePointer} handleCancel={handleCancel} handleSend={handleSend} handleVoiceClick={handleVoiceClick} handleVoicePointerDown={handleVoicePointerDown} handleVoicePointerLeave={handleVoicePointerLeave} inputLocked={inputLocked} isBusy={isBusy} lang={lang} ready={ready} showBusySpinner={showBusySpinner} theme={t} themeMode={themeMode} voiceInput={voiceInput} />
            </div>
        </div>
    );
}