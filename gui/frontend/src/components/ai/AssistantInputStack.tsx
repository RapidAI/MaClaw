import type React from "react";
import { BufferQueuePanel } from "./BufferQueuePanel";
import { AssistantInputComposer } from "./AssistantInputComposer";
import type { Theme } from "./aiAssistantPanelTheme";

interface AssistantInputStackProps {
    browseFile: () => void;
    canSend: boolean;
    cancelPending: boolean;
    cancelSession?: unknown;
    clearSelectedFile?: () => void;
    editingEntryId: string | null;
    exitHistoryBrowsing: () => boolean;
    finishVoicePointer: (event: React.PointerEvent<HTMLButtonElement>) => void;
    handleCancel: () => void;
    handleEditEntry: (id: string) => void;
    handleCancelEdit: () => void;
    handlePaste: (event: React.ClipboardEvent<HTMLTextAreaElement>) => void;
    handleSaveEdit: (id: string, text: string, attachments: any[]) => void;
    handleFireEntry: (id: string) => void;
    isEntryInFlight?: (id: string) => boolean;
    handleSend: () => void;
    handleVoiceClick: () => void;
    handleVoicePointerDown: (event: React.PointerEvent<HTMLButtonElement>) => void;
    handleVoicePointerLeave: (event: React.PointerEvent<HTMLButtonElement>) => void;
    inputAreaHeight: number | null;
    inputLocked: boolean;
    inputRef: React.Ref<HTMLTextAreaElement>;
    inputValue: string;
    inline: boolean;
    isBusy: boolean;
    isSelectionCollapsedAtBoundary: (direction: "up" | "down") => boolean;
    lang: string;
    pendingAttachments: any[];
    placeholderText: string;
    queue: any[];
    ready: boolean;
    recallHistory: (direction: "up" | "down") => boolean;
    rememberHistoryEdit: (value: string) => void;
    removeEntry: (id: string) => void;
    removeSelectedFile?: (index: number) => void;
    reorderEntry: (fromIndex: number, toIndex: number) => void;
    resizeInput: () => void;
    selectedFilePaths: string[];
    setPendingAttachments: React.Dispatch<React.SetStateAction<any>>;
    showBusySpinner: boolean;
    startInputResize: (event: React.MouseEvent<HTMLDivElement>) => void;
    theme: Theme;
    themeMode: "light" | "dark";
    updateInputValue: (value: string) => void;
    voiceInput: any;
}

export function AssistantInputStack(props: AssistantInputStackProps) {
    const {
        browseFile, canSend, cancelPending, cancelSession, clearSelectedFile, editingEntryId,
        exitHistoryBrowsing, finishVoicePointer, handleCancel, handleEditEntry, handleCancelEdit, handlePaste,
        handleSaveEdit, handleFireEntry, handleSend, handleVoiceClick, handleVoicePointerDown, handleVoicePointerLeave, inputAreaHeight,
        isEntryInFlight,
        inputLocked, inputRef, inputValue, inline, isBusy, isSelectionCollapsedAtBoundary, lang, pendingAttachments,
        placeholderText, queue, ready, recallHistory, rememberHistoryEdit, removeEntry, removeSelectedFile, reorderEntry,
        resizeInput, selectedFilePaths, setPendingAttachments, showBusySpinner, startInputResize, theme: t,
        themeMode, updateInputValue, voiceInput,
    } = props;

    return (
        <>
            <div
                data-testid="ai-input-resize-handle"
                onMouseDown={startInputResize}
                title={lang?.startsWith("zh") ? "拖动调整输入区高度" : "Drag to resize the input area"}
                style={{
                    height: "8px",
                    cursor: "ns-resize",
                    background: t.bg,
                    borderTop: `1px solid ${t.divider}`,
                    borderBottom: `1px solid ${t.inputBarBorder}`,
                    flexShrink: 0,
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                    ['--wails-draggable' as any]: 'no-drag',
                }}
            >
                <span style={{ width: "42px", height: "2px", borderRadius: "999px", background: t.textMuted, opacity: 0.42, pointerEvents: "none" }} />
            </div>
            <div
                data-testid="ai-input-stack"
                style={{
                    display: "flex",
                    flexDirection: "column",
                    flexShrink: 0,
                    minHeight: inputAreaHeight ? "72px" : undefined,
                    maxHeight: "55%",
                    height: inputAreaHeight ? `${inputAreaHeight}px` : undefined,
                    minWidth: 0,
                    overflow: "hidden",
                    background: t.inputBarBg,
                    borderTop: `1px solid ${t.inputBarBorder}`,
                }}
            >
                <BufferQueuePanel
                    queue={queue}
                    lang={lang}
                    theme={{ bg: t.bg, text: t.text, textMuted: t.textMuted, headingColor: t.headingColor, inputBarBg: t.inputBarBg, inputBarBorder: t.inputBarBorder, codeBlockBg: t.codeBlockBg, codeBlockBorder: t.codeBlockBorder, divider: t.divider }}
                    editingEntryId={editingEntryId}
                    onEdit={handleEditEntry}
                    onCancelEdit={handleCancelEdit}
                    onSaveEdit={handleSaveEdit}
                    onDelete={removeEntry}
                    onReorder={reorderEntry}
                    onFireEntry={handleFireEntry}
                    isEntryInFlight={isEntryInFlight}
                />
                <AssistantInputComposer
                    browseFile={browseFile}
                    canSend={canSend}
                    cancelPending={cancelPending}
                    cancelSession={cancelSession}
                    clearSelectedFile={clearSelectedFile}
                    exitHistoryBrowsing={exitHistoryBrowsing}
                    finishVoicePointer={finishVoicePointer}
                    handleCancel={handleCancel}
                    handlePaste={handlePaste}
                    handleSend={handleSend}
                    handleVoiceClick={handleVoiceClick}
                    handleVoicePointerDown={handleVoicePointerDown}
                    handleVoicePointerLeave={handleVoicePointerLeave}
                    inputAreaHeight={inputAreaHeight}
                    inputLocked={inputLocked}
                    inputRef={inputRef}
                    inputValue={inputValue}
                    inline={inline}
                    isBusy={isBusy}
                    isSelectionCollapsedAtBoundary={isSelectionCollapsedAtBoundary}
                    lang={lang}
                    pendingAttachments={pendingAttachments}
                    placeholderText={placeholderText}
                    ready={ready}
                    recallHistory={recallHistory}
                    rememberHistoryEdit={rememberHistoryEdit}
                    removeSelectedFile={removeSelectedFile}
                    resizeInput={resizeInput}
                    selectedFilePaths={selectedFilePaths}
                    setPendingAttachments={setPendingAttachments}
                    showBusySpinner={showBusySpinner}
                    theme={t}
                    themeMode={themeMode}
                    updateInputValue={updateInputValue}
                    voiceInput={voiceInput}
                />
            </div>
        </>
    );
}
