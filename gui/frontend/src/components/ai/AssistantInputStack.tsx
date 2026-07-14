import type React from "react";
import { BufferQueuePanel } from "./BufferQueuePanel";
import { AssistantInputComposer } from "./AssistantInputComposer";
import type { Theme } from "./aiAssistantPanelTheme";
import type { ComposeAction, FireSlashCommand, PlusMenuActionId } from "./composeAction";
import type { AttachmentInfo, BufferEntry } from "./useBufferQueue";
import type { UseVoiceInputResult } from "./useVoiceInput";
import type { AssistantPermissionMode } from "./AssistantInputComposerTypes";

interface AssistantInputStackProps {
    attachButtonTestId?: string;
    browseFile: () => void;
    canSend: boolean;
    composeAction?: ComposeAction | null;
    onComposeActionChange?: (action: ComposeAction | null) => void;
    onFireSlashCommand?: (command: FireSlashCommand) => void;
    onInsertTemplate?: (template: string) => void;
    onPlusMenuAction?: (actionId: PlusMenuActionId) => void;
    cancelPending: boolean;
    cancelSession?: unknown;
    clearSelectedFile?: () => void;
    editingEntryId: string | null;
    exitHistoryBrowsing: () => boolean;
    finishVoicePointer: (event: React.PointerEvent<HTMLButtonElement>) => void;
    handleCancel: () => void;
    handleEditEntry: (id: string) => void;
    handleCancelEdit: () => void;
    handleClearInput: () => void;
    handleDragOver: (event: React.DragEvent<HTMLElement>) => void;
    handleDrop: (event: React.DragEvent<HTMLElement>) => void;
    handlePaste: (event: React.ClipboardEvent<HTMLTextAreaElement>) => void;
    handleSaveEdit: (id: string, text: string, attachments: AttachmentInfo[]) => void;
    handleFireEntry: (id: string) => void;
    handleTextareaClick?: (event: React.MouseEvent<HTMLTextAreaElement>) => void;
    handleTextareaKeyDownBefore?: (event: React.KeyboardEvent<HTMLTextAreaElement>) => boolean;
    handleTextareaKeyUp?: (event: React.KeyboardEvent<HTMLTextAreaElement>) => void;
    isEntryInFlight?: (id: string) => boolean;
    handleSend: () => void;
    handleVoiceClick: () => void;
    handleVoicePointerDown: (event: React.PointerEvent<HTMLButtonElement>) => void;
    handleVoicePointerLeave: (event: React.PointerEvent<HTMLButtonElement>) => void;
    inputAreaHeight: number | null;
    inputBarTestId?: string;
    inputLocked: boolean;
    inputOverlay?: React.ReactNode;
    allowInputOverflow?: boolean;
    inputRef: React.Ref<HTMLTextAreaElement>;
    inputRowTestId?: string;
    inputValue: string;
    inline: boolean;
    isBusy: boolean;
    isSelectionCollapsedAtBoundary: (direction: "up" | "down") => boolean;
    lang: string;
    pendingAttachments: AttachmentInfo[];
    permissionMode?: AssistantPermissionMode;
    showWorkspacePermissionOption?: boolean;
    onPermissionModeChange?: (mode: AssistantPermissionMode) => void;
    pendingAttachmentsTestId?: string;
    placeholderText: string;
    queuePanelTestId?: string;
    queue: BufferEntry[];
    ready: boolean;
    recallHistory: (direction: "up" | "down") => boolean;
    rememberHistoryEdit: (value: string) => void;
    removeEntry: (id: string) => void;
    removeSelectedFile?: (index: number) => void;
    reorderEntry: (fromIndex: number, toIndex: number) => void;
    resizeInput: () => void;
    selectedFilePaths: string[];
    setPendingAttachments: React.Dispatch<React.SetStateAction<AttachmentInfo[]>>;
    showBusySpinner: boolean;
    showMemoryUsage?: boolean;
    showResizeHandle?: boolean;
    showVoiceInput?: boolean;
    submittedPrompts?: string[];
    sendButtonStyle?: React.CSSProperties;
    sendButtonTestId?: string;
    startInputResize: (event: React.MouseEvent<HTMLDivElement>) => void;
    textareaAriaLabel?: string;
    textareaTestId?: string;
    theme: Theme;
    themeMode: "light" | "dark";
    toolbarTestId?: string;
    updateInputValue: (value: string) => void;
    voiceInput: UseVoiceInputResult;
}

export function AssistantInputStack(props: AssistantInputStackProps) {
    const {
        attachButtonTestId, browseFile, canSend, cancelPending, cancelSession, clearSelectedFile, composeAction, editingEntryId,
        exitHistoryBrowsing, finishVoicePointer, handleCancel, handleEditEntry, handleCancelEdit, handleClearInput, handleDragOver, handleDrop, handlePaste,
        handleSaveEdit, handleFireEntry, handleSend, handleTextareaClick, handleTextareaKeyDownBefore, handleTextareaKeyUp,
        handleVoiceClick, handleVoicePointerDown, handleVoicePointerLeave, inputAreaHeight, inputBarTestId,
        isEntryInFlight,
        inputLocked, inputOverlay, allowInputOverflow = true, inputRef, inputRowTestId, inputValue, inline, isBusy, isSelectionCollapsedAtBoundary, lang, onComposeActionChange, onFireSlashCommand, onInsertTemplate, onPlusMenuAction, pendingAttachments,
        pendingAttachmentsTestId, permissionMode, showWorkspacePermissionOption, onPermissionModeChange, placeholderText, queue, queuePanelTestId, ready, recallHistory, rememberHistoryEdit, removeEntry, removeSelectedFile, reorderEntry,
        resizeInput, selectedFilePaths, setPendingAttachments, showBusySpinner, showMemoryUsage, showResizeHandle = true,
        showVoiceInput, submittedPrompts, sendButtonStyle, sendButtonTestId, startInputResize, textareaAriaLabel, textareaTestId, theme: t,
        themeMode, toolbarTestId, updateInputValue, voiceInput,
    } = props;

    const queuePanel = queue.length > 0 ? (
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
    ) : null;

    return (
        <>
            {showResizeHandle && <div
                data-testid="ai-input-resize-handle"
                onMouseDown={startInputResize}
                title={lang?.startsWith("zh") ? "拖动调整输入区高度" : "Drag to resize the input area"}
                style={{
                    height: "8px",
                    cursor: "ns-resize",
                    background: "transparent",
                    borderTop: inline ? `1px solid ${t.divider}` : "none",
                    borderBottom: inline ? `1px solid ${t.inputBarBorder}` : "none",
                    flexShrink: 0,
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                    ['--wails-draggable' as any]: 'no-drag',
                }}
            >
                <span style={{ width: "42px", height: "2px", borderRadius: "999px", background: t.textMuted, opacity: 0.42, pointerEvents: "none" }} />
            </div>}
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
                    overflow: allowInputOverflow ? "visible" : "hidden",
                    background: inline ? t.inputBarBg : "transparent",
                    borderTop: inline ? `1px solid ${t.inputBarBorder}` : "none",
                    ['--wails-draggable' as any]: 'no-drag',
                }}
            >
                {queuePanelTestId && queuePanel ? <div data-testid={queuePanelTestId}>{queuePanel}</div> : queuePanel}
                <AssistantInputComposer
                    attachButtonTestId={attachButtonTestId}
                    browseFile={browseFile}
                    canSend={canSend}
                    cancelPending={cancelPending}
                    cancelSession={cancelSession}
                    clearSelectedFile={clearSelectedFile}
                    composeAction={composeAction}
                    exitHistoryBrowsing={exitHistoryBrowsing}
                    onComposeActionChange={onComposeActionChange}
                    onFireSlashCommand={onFireSlashCommand}
                    onInsertTemplate={onInsertTemplate}
                    onPlusMenuAction={onPlusMenuAction}
                    finishVoicePointer={finishVoicePointer}
                    handleCancel={handleCancel}
                    handleClearInput={handleClearInput}
                    handleDragOver={handleDragOver}
                    handleDrop={handleDrop}
                    handlePaste={handlePaste}
                    handleSend={handleSend}
                    handleTextareaClick={handleTextareaClick}
                    handleTextareaKeyDownBefore={handleTextareaKeyDownBefore}
                    handleTextareaKeyUp={handleTextareaKeyUp}
                    handleVoiceClick={handleVoiceClick}
                    handleVoicePointerDown={handleVoicePointerDown}
                    handleVoicePointerLeave={handleVoicePointerLeave}
                    inputAreaHeight={inputAreaHeight}
                    inputBarTestId={inputBarTestId}
                    inputLocked={inputLocked}
                    inputOverlay={inputOverlay}
                    inputRef={inputRef}
                    inputRowTestId={inputRowTestId}
                    inputValue={inputValue}
                    inline={inline}
                    isBusy={isBusy}
                    isSelectionCollapsedAtBoundary={isSelectionCollapsedAtBoundary}
                    lang={lang}
                    pendingAttachments={pendingAttachments}
                    pendingAttachmentsTestId={pendingAttachmentsTestId}
                    permissionMode={permissionMode}
                    showWorkspacePermissionOption={showWorkspacePermissionOption}
                    onPermissionModeChange={onPermissionModeChange}
                    placeholderText={placeholderText}
                    ready={ready}
                    recallHistory={recallHistory}
                    rememberHistoryEdit={rememberHistoryEdit}
                    removeSelectedFile={removeSelectedFile}
                    resizeInput={resizeInput}
                    selectedFilePaths={selectedFilePaths}
                    setPendingAttachments={setPendingAttachments}
                    showBusySpinner={showBusySpinner}
                    showMemoryUsage={showMemoryUsage}
                    showVoiceInput={showVoiceInput}
                    submittedPrompts={submittedPrompts}
                    sendButtonStyle={sendButtonStyle}
                    sendButtonTestId={sendButtonTestId}
                    textareaAriaLabel={textareaAriaLabel}
                    textareaTestId={textareaTestId}
                    theme={t}
                    themeMode={themeMode}
                    toolbarTestId={toolbarTestId}
                    updateInputValue={updateInputValue}
                    voiceInput={voiceInput}
                />
            </div>
        </>
    );
}
