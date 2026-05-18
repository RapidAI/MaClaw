import React, { useState, useCallback, useRef } from "react";
import { BufferEntry, AttachmentInfo, getTextPreview } from "./useBufferQueue";
import { AssistantInputIcon } from "./aiAssistantPanelTheme";

// ---------------------------------------------------------------------------
// Localization helper (same pattern as AIAssistantPanel)
// ---------------------------------------------------------------------------

const localizeText = (
    lang: string | undefined,
    en: string,
    zhHans: string,
    zhHant: string = zhHans,
) => (lang === "zh-Hant" ? zhHant : lang?.startsWith("zh") ? zhHans : en);

// ---------------------------------------------------------------------------
// Theme interface (subset of AIAssistantPanel Theme used by this component)
// ---------------------------------------------------------------------------

export interface Theme {
    bg: string;
    text: string;
    textMuted: string;
    headingColor: string;
    inputBarBg: string;
    inputBarBorder: string;
    codeBlockBg: string;
    codeBlockBorder: string;
    divider: string;
}

// ---------------------------------------------------------------------------
// File-type icon mapping (Task 4.2)
// ---------------------------------------------------------------------------

export const FILE_TYPE_ICONS: Record<string, string> = {
    // Document
    ".pdf": "📕",
    ".doc": "📘",
    ".docx": "📘",
    ".xls": "📗",
    ".xlsx": "📗",
    ".ppt": "📙",
    ".pptx": "📙",
    ".txt": "📝",
    ".md": "📝",
    ".csv": "📊",
    // Code
    ".js": "🟨",
    ".jsx": "🟨",
    ".ts": "🔷",
    ".tsx": "🔷",
    ".py": "🐍",
    ".go": "🔵",
    ".rs": "🦀",
    ".java": "☕",
    ".c": "🔧",
    ".cpp": "🔧",
    ".h": "🔧",
    ".cs": "🟣",
    ".html": "🌐",
    ".css": "🎨",
    ".json": "📋",
    ".xml": "📋",
    ".yaml": "📋",
    ".yml": "📋",
    ".toml": "📋",
    // Image
    ".png": "🖼️",
    ".jpg": "🖼️",
    ".jpeg": "🖼️",
    ".gif": "🖼️",
    ".svg": "🖼️",
    ".webp": "🖼️",
    ".bmp": "🖼️",
    // Archive
    ".zip": "📦",
    ".tar": "📦",
    ".gz": "📦",
    ".rar": "📦",
    // Script
    ".sh": "⚙️",
    ".bat": "⚙️",
    ".ps1": "⚙️",
};

const DEFAULT_FILE_ICON = "📄";

export function getFileTypeIcon(extension: string): string {
    return FILE_TYPE_ICONS[extension.toLowerCase()] || DEFAULT_FILE_ICON;
}

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------

export interface BufferQueuePanelProps {
    queue: BufferEntry[];
    lang: string;
    theme: Theme;
    editingEntryId: string | null;
    onEdit: (id: string) => void;
    onCancelEdit: () => void;
    onSaveEdit: (id: string, text: string, attachments: AttachmentInfo[]) => void;
    onDelete: (id: string) => void;
    onReorder: (fromIndex: number, toIndex: number) => void;
    /** Fire (send) a single queued entry as supplementary info to the running task. */
    onFireEntry?: (id: string) => void;
    isEntryInFlight?: (id: string) => boolean;
}

// ---------------------------------------------------------------------------
// Drag state
// ---------------------------------------------------------------------------

interface DragState {
    draggingId: string;
    startY: number;
    currentY: number;
    startIndex: number;
    targetIndex: number;
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export const BufferQueuePanel: React.FC<BufferQueuePanelProps> = ({
    queue,
    lang,
    theme: t,
    editingEntryId,
    onEdit,
    onCancelEdit,
    onSaveEdit,
    onDelete,
    onReorder,
    onFireEntry,
    isEntryInFlight,
}) => {
    const [dragState, setDragState] = useState<DragState | null>(null);

    // Refs to track entry row DOM elements for position calculation
    const rowRefsMap = useRef<Map<string, HTMLDivElement>>(new Map());

    const setRowRef = useCallback((id: string, el: HTMLDivElement | null) => {
        if (el) {
            rowRefsMap.current.set(id, el);
        } else {
            rowRefsMap.current.delete(id);
        }
    }, []);

    // Calculate target insertion index based on current pointer Y position
    const calcTargetIndex = useCallback(
        (clientY: number): number => {
            const midpoints: number[] = [];
            for (let i = 0; i < queue.length; i++) {
                const el = rowRefsMap.current.get(queue[i].id);
                if (el) {
                    const rect = el.getBoundingClientRect();
                    midpoints.push(rect.top + rect.height / 2);
                }
            }
            // Find the insertion index: the first row whose midpoint is below clientY
            let target = queue.length;
            for (let i = 0; i < midpoints.length; i++) {
                if (clientY < midpoints[i]) {
                    target = i;
                    break;
                }
            }
            return target;
        },
        [queue],
    );

    const handlePointerDown = useCallback(
        (e: React.PointerEvent<HTMLSpanElement>, entryId: string, index: number) => {
            // Only handle primary button (left click / touch)
            if (e.button !== 0) return;
            e.preventDefault();
            (e.target as HTMLElement).setPointerCapture(e.pointerId);
            setDragState({
                draggingId: entryId,
                startY: e.clientY,
                currentY: e.clientY,
                startIndex: index,
                targetIndex: index,
            });
        },
        [],
    );

    const handlePointerMove = useCallback(
        (e: React.PointerEvent<HTMLDivElement>) => {
            if (!dragState) return;
            const newTarget = calcTargetIndex(e.clientY);
            setDragState((prev) =>
                prev ? { ...prev, currentY: e.clientY, targetIndex: newTarget } : null,
            );
        },
        [dragState, calcTargetIndex],
    );

    const handlePointerUp = useCallback(
        (e: React.PointerEvent<HTMLDivElement>) => {
            if (!dragState) return;
            try {
                (e.target as HTMLElement).releasePointerCapture(e.pointerId);
            } catch {
                // Pointer capture may already be released
            }
            const from = dragState.startIndex;
            // Adjust target: when moving down, the visual insertion line is at targetIndex,
            // but the reorder function expects the final position after removal.
            let to = dragState.targetIndex;
            if (to > from) {
                to = to - 1;
            }
            if (from !== to) {
                onReorder(from, to);
            }
            setDragState(null);
        },
        [dragState, onReorder],
    );

    const handlePointerCancel = useCallback(() => {
        setDragState(null);
    }, []);

    // Don't render when queue is empty
    if (queue.length === 0) return null;

    const headerText = localizeText(
        lang,
        `${queue.length} queued`,
        `${queue.length} 条待发送`,
        `${queue.length} 條待發送`,
    );

    return (
        <div
            data-testid="buffer-queue-panel"
            style={{
                maxHeight: queue.length > 1 ? "92px" : "44px",
                overflowY: "auto",
                background: t.inputBarBg,
                borderTop: `1px solid ${t.inputBarBorder}`,
                borderBottom: `1px solid ${t.divider}`,
                padding: "2px 10px",
                flexShrink: 0,
                display: "flex",
                flexDirection: "column",
                gap: "2px",
            }}
            onPointerMove={handlePointerMove}
            onPointerUp={handlePointerUp}
            onPointerCancel={handlePointerCancel}
        >
            {/* Header */}
            <div
                data-testid="buffer-queue-header"
                style={{
                    fontSize: "11px",
                    fontWeight: 600,
                    color: t.headingColor,
                    padding: 0,
                    lineHeight: 1.2,
                    minHeight: "14px",
                    userSelect: "none",
                }}
            >
                {headerText}
            </div>

            {/* Entry rows */}
            {queue.map((entry, index) => {
                const isDragging = dragState?.draggingId === entry.id;
                const deltaY = isDragging ? dragState.currentY - dragState.startY : 0;

                // Show insertion line above this entry if it's the target position
                const showInsertionBefore =
                    dragState !== null &&
                    dragState.targetIndex === index &&
                    dragState.startIndex !== index &&
                    dragState.startIndex !== index - 1;

                // Show insertion line after the last entry if target is at the end
                const showInsertionAfter =
                    dragState !== null &&
                    index === queue.length - 1 &&
                    dragState.targetIndex === queue.length &&
                    dragState.startIndex !== queue.length - 1;

                return (
                    <React.Fragment key={entry.id}>
                        {showInsertionBefore && (
                            <div
                                data-testid="drag-insertion-line"
                                style={{
                                    height: "2px",
                                    background: t.headingColor,
                                    margin: "0 0",
                                    borderRadius: "1px",
                                }}
                            />
                        )}
                        <BufferEntryRow
                            entry={entry}
                            index={index}
                            lang={lang}
                            theme={t}
                            isEditing={editingEntryId === entry.id}
                            onEdit={onEdit}
                            onCancelEdit={onCancelEdit}
                            onSaveEdit={onSaveEdit}
                            onDelete={onDelete}
                            onReorder={onReorder}
                            isDragging={isDragging}
                            dragDeltaY={deltaY}
                            onPointerDown={handlePointerDown}
                            setRowRef={setRowRef}
                            onFireEntry={onFireEntry}
                            inFlight={!!isEntryInFlight?.(entry.id)}
                        />
                        {showInsertionAfter && (
                            <div
                                data-testid="drag-insertion-line"
                                style={{
                                    height: "2px",
                                    background: t.headingColor,
                                    margin: "0 0",
                                    borderRadius: "1px",
                                }}
                            />
                        )}
                    </React.Fragment>
                );
            })}
        </div>
    );
};

// ---------------------------------------------------------------------------
// BufferEntryRow (internal)
// ---------------------------------------------------------------------------

interface BufferEntryRowProps {
    entry: BufferEntry;
    index: number;
    lang: string;
    theme: Theme;
    isEditing: boolean;
    onEdit: (id: string) => void;
    onCancelEdit: () => void;
    onSaveEdit: (id: string, text: string, attachments: AttachmentInfo[]) => void;
    onDelete: (id: string) => void;
    onReorder: (fromIndex: number, toIndex: number) => void;
    isDragging: boolean;
    dragDeltaY: number;
    onPointerDown: (e: React.PointerEvent<HTMLSpanElement>, entryId: string, index: number) => void;
    setRowRef: (id: string, el: HTMLDivElement | null) => void;
    onFireEntry?: (id: string) => void;
    inFlight?: boolean;
}

const BufferEntryRow: React.FC<BufferEntryRowProps> = ({
    entry,
    index,
    lang,
    theme: t,
    isEditing,
    onEdit,
    onCancelEdit,
    onSaveEdit,
    onDelete,
    isDragging,
    dragDeltaY,
    onPointerDown,
    setRowRef,
    onFireEntry,
    inFlight = false,
}) => {
    const [editText, setEditText] = useState(entry.text);
    const [editAttachments, setEditAttachments] = useState<AttachmentInfo[]>(
        entry.attachments,
    );

    // Reset edit state when entering edit mode
    const handleStartEdit = useCallback(() => {
        if (inFlight) return;
        setEditText(entry.text);
        setEditAttachments([...entry.attachments]);
        onEdit(entry.id);
    }, [entry, inFlight, onEdit]);

    const handleSave = useCallback(() => {
        const trimmed = editText.trim();
        if (!trimmed && editAttachments.length === 0) {
            // Empty text + no attachments → delete
            onDelete(entry.id);
        } else {
            onSaveEdit(entry.id, editText, editAttachments);
        }
    }, [editText, editAttachments, entry.id, onSaveEdit, onDelete]);

    const handleCancel = useCallback(() => {
        onCancelEdit();
    }, [onCancelEdit]);

    const handleKeyDown = useCallback(
        (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
            if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                handleSave();
            } else if (e.key === "Escape") {
                e.preventDefault();
                handleCancel();
            }
        },
        [handleSave, handleCancel],
    );

    const handleRemoveAttachment = useCallback((idx: number) => {
        setEditAttachments((prev) => prev.filter((_, i) => i !== idx));
    }, []);

    // ── Edit mode ──
    if (isEditing) {
        return (
            <div
                data-testid={`buffer-entry-edit-${entry.id}`}
                style={{
                    padding: "6px 0",
                    borderBottom: `1px solid ${t.divider}`,
                }}
            >
                <textarea
                    data-testid={`buffer-entry-textarea-${entry.id}`}
                    value={editText}
                    onChange={(e) => setEditText(e.target.value)}
                    onKeyDown={handleKeyDown}
                    autoFocus
                    style={{
                        width: "100%",
                        minHeight: "48px",
                        padding: "6px 8px",
                        fontSize: "12px",
                        fontFamily: "inherit",
                        color: t.text,
                        background: t.codeBlockBg,
                        border: `1px solid ${t.codeBlockBorder}`,
                        borderRadius: "4px",
                        resize: "vertical",
                        outline: "none",
                        boxSizing: "border-box",
                    }}
                />

                {/* Attachment management area */}
                {editAttachments.length > 0 && (
                    <div
                        style={{
                            display: "flex",
                            flexWrap: "wrap",
                            gap: "4px",
                            marginTop: "4px",
                        }}
                    >
                        {editAttachments.map((att, idx) => (
                            <div
                                key={att.filePath}
                                title={att.filePath}
                                style={{
                                    display: "inline-flex",
                                    alignItems: "center",
                                    gap: "2px",
                                    padding: "1px 3px",
                                    borderRadius: "3px",
                                    background: t.codeBlockBg,
                                    border: `1px solid ${t.codeBlockBorder}`,
                                    fontSize: "11px",
                                    color: t.textMuted,
                                }}
                            >
                                {att.isImage && att.thumbnailDataUrl ? (
                                    <img
                                        src={att.thumbnailDataUrl}
                                        alt={att.fileName}
                                        style={{
                                            width: "20px",
                                            height: "20px",
                                            objectFit: "cover",
                                            borderRadius: "2px",
                                        }}
                                    />
                                ) : att.isImage ? (
                                    <span>🖼️</span>
                                ) : (
                                    <span>{getFileTypeIcon(att.extension)}</span>
                                )}
                                <span
                                    style={{
                                        maxWidth: "80px",
                                        overflow: "hidden",
                                        textOverflow: "ellipsis",
                                        whiteSpace: "nowrap",
                                    }}
                                >
                                    {att.fileName}
                                </span>
                                <button
                                    data-testid={`remove-attachment-${idx}`}
                                    onClick={() => handleRemoveAttachment(idx)}
                                    style={{
                                        background: "none",
                                        border: "none",
                                        cursor: "pointer",
                                        color: t.textMuted,
                                        fontSize: "11px",
                                        padding: "0 2px",
                                        lineHeight: 1,
                                    }}
                                    aria-label={localizeText(
                                        lang,
                                        "Remove attachment",
                                        "移除附件",
                                        "移除附件",
                                    )}
                                >
                                    ✕
                                </button>
                            </div>
                        ))}
                    </div>
                )}

                {/* Confirm / Cancel buttons */}
                <div
                    style={{
                        display: "flex",
                        gap: "6px",
                        marginTop: "4px",
                        justifyContent: "flex-end",
                    }}
                >
                    <button
                        data-testid={`buffer-entry-cancel-${entry.id}`}
                        onClick={handleCancel}
                        style={{
                            background: "none",
                            border: "none",
                            cursor: "pointer",
                            color: t.textMuted,
                            fontSize: "12px",
                            padding: "2px 6px",
                        }}
                        aria-label={localizeText(lang, "Cancel edit", "取消编辑", "取消編輯")}
                    >
                        ✕
                    </button>
                    <button
                        data-testid={`buffer-entry-confirm-${entry.id}`}
                        onClick={handleSave}
                        style={{
                            background: "none",
                            border: "none",
                            cursor: "pointer",
                            color: t.headingColor,
                            fontSize: "12px",
                            padding: "2px 6px",
                        }}
                        aria-label={localizeText(lang, "Confirm edit", "确认编辑", "確認編輯")}
                    >
                        ✓
                    </button>
                </div>
            </div>
        );
    }

    // ── Display mode ──
    return (
        <div
            ref={(el) => setRowRef(entry.id, el)}
            data-testid={`buffer-entry-${entry.id}`}
            style={{
                display: "flex",
                alignItems: "center",
                gap: "5px",
                padding: "1px 0 2px",
                minHeight: "22px",
                borderBottom: `1px solid ${t.divider}`,
                fontSize: "12px",
                color: t.text,
                opacity: inFlight ? 0.55 : 1,
                ...(isDragging
                    ? {
                          opacity: 0.5,
                          transform: `translateY(${dragDeltaY}px)`,
                          position: "relative" as const,
                          zIndex: 10,
                          pointerEvents: "none" as const,
                      }
                    : {}),
            }}
        >
            {/* Drag handle */}
            <span
                data-testid={`drag-handle-${entry.id}`}
                onPointerDown={(e) => { if (!inFlight) onPointerDown(e, entry.id, index); }}
                style={{
                    cursor: inFlight ? "default" : isDragging ? "grabbing" : "grab",
                    color: t.textMuted,
                    fontSize: "12px",
                    userSelect: "none",
                    flexShrink: 0,
                    lineHeight: 1,
                    touchAction: "none",
                }}
                aria-label={localizeText(lang, "Drag to reorder", "拖拽排序", "拖曳排序")}
            >
                ⠿
            </span>

            {/* Middle: text preview + attachment indicators */}
            <div
                style={{
                    flex: 1,
                    minWidth: 0,
                    display: "flex",
                    alignItems: "center",
                    gap: "4px",
                    lineHeight: 1.25,
                }}
            >
                <span
                    style={{
                        overflow: "hidden",
                        display: "-webkit-box",
                        WebkitLineClamp: 2,
                        WebkitBoxOrient: "vertical",
                        whiteSpace: "normal",
                        flexShrink: 1,
                        minWidth: 0,
                    }}
                >
                    {getTextPreview(entry.text)}
                </span>

                {/* Attachment indicators */}
                {entry.attachments.map((att, idx) => (
                    <span
                        key={`${att.filePath}-${idx}`}
                        title={att.filePath}
                        style={{
                            flexShrink: 0,
                            display: "inline-flex",
                            alignItems: "center",
                        }}
                    >
                        {att.isImage && att.thumbnailDataUrl ? (
                            <img
                                src={att.thumbnailDataUrl}
                                alt={att.fileName}
                                style={{
                                    width: "18px",
                                    height: "18px",
                                    objectFit: "cover",
                                    borderRadius: "2px",
                                }}
                            />
                        ) : att.isImage ? (
                            <span style={{ fontSize: "12px" }}>🖼️</span>
                        ) : (
                            <span style={{ fontSize: "12px" }}>
                                {getFileTypeIcon(att.extension)}
                            </span>
                        )}
                    </span>
                ))}
            </div>

            {/* Right: Fire + Edit + Delete buttons */}
            {onFireEntry && (
                <button
                    data-testid={`fire-btn-${entry.id}`}
                    onClick={() => { if (!inFlight) onFireEntry(entry.id); }}
                    disabled={inFlight}
                    style={{
                        background: "none",
                        border: `1px solid ${t.headingColor}`,
                        borderRadius: "3px",
                        cursor: inFlight ? "default" : "pointer",
                        color: t.headingColor,
                        fontSize: "12px",
                        padding: "1px 4px",
                        flexShrink: 0,
                        lineHeight: 1.2,
                        opacity: inFlight ? 0.45 : 1,
                    }}
                    aria-label={localizeText(lang, "Guide into next agent loop", "引导进入下一次 agent loop", "引導進入下一次 agent loop")}
                    title={localizeText(lang, "Guide into next agent loop", "引导发射", "引導發射")}
                >
                    <AssistantInputIcon name="cornerDownLeft" size={13} />
                </button>
            )}
            <button
                data-testid={`edit-btn-${entry.id}`}
                onClick={handleStartEdit}
                disabled={inFlight}
                style={{
                    background: "none",
                    border: "none",
                    cursor: inFlight ? "default" : "pointer",
                    color: t.textMuted,
                    fontSize: "12px",
                    padding: "1px 3px",
                    flexShrink: 0,
                    lineHeight: 1,
                    opacity: inFlight ? 0.45 : 1,
                }}
                aria-label={localizeText(lang, "Edit entry", "编辑条目", "編輯條目")}
                title={localizeText(lang, "Edit", "编辑", "編輯")}
            >
                ✏️
            </button>
            <button
                data-testid={`delete-btn-${entry.id}`}
                onClick={() => { if (!inFlight) onDelete(entry.id); }}
                disabled={inFlight}
                style={{
                    background: "none",
                    border: "none",
                    cursor: inFlight ? "default" : "pointer",
                    color: t.textMuted,
                    fontSize: "12px",
                    padding: "1px 3px",
                    flexShrink: 0,
                    lineHeight: 1,
                    opacity: inFlight ? 0.45 : 1,
                }}
                aria-label={localizeText(lang, "Delete entry", "删除条目", "刪除條目")}
                title={localizeText(lang, "Delete", "删除", "刪除")}
            >
                🗑
            </button>
        </div>
    );
};

export default BufferQueuePanel;
