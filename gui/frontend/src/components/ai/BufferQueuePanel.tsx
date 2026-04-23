import React, { useState, useCallback, useRef } from "react";
import { BufferEntry, AttachmentInfo, getTextPreview } from "./useBufferQueue";

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
    /** Fire (send) a single queued entry, interrupting the current session. */
    onFireEntry?: (id: string) => void;
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
                maxHeight: "40vh",
                overflowY: "auto",
                background: t.inputBarBg,
                borderTop: `1px solid ${t.inputBarBorder}`,
                borderBottom: `1px solid ${t.divider}`,
                padding: "4px 12px 4px 12px",
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
                    padding: "2px 0 4px 0",
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
}) => {
    const [editText, setEditText] = useState(entry.text);
    const [editAttachments, setEditAttachments] = useState<AttachmentInfo[]>(
        entry.attachments,
    );

    // Reset edit state when entering edit mode
    const handleStartEdit = useCallback(() => {
        setEditText(entry.text);
        setEditAttachments([...entry.attachments]);
        onEdit(entry.id);
    }, [entry, onEdit]);

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
                                    padding: "2px 4px",
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
                            fontSize: "13px",
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
                            fontSize: "13px",
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
                gap: "6px",
                padding: "4px 0",
                borderBottom: `1px solid ${t.divider}`,
                fontSize: "12px",
                color: t.text,
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
                onPointerDown={(e) => onPointerDown(e, entry.id, index)}
                style={{
                    cursor: isDragging ? "grabbing" : "grab",
                    color: t.textMuted,
                    fontSize: "14px",
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
                }}
            >
                <span
                    style={{
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        whiteSpace: "nowrap",
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
                                    width: "24px",
                                    height: "24px",
                                    objectFit: "cover",
                                    borderRadius: "2px",
                                }}
                            />
                        ) : att.isImage ? (
                            <span style={{ fontSize: "14px" }}>🖼️</span>
                        ) : (
                            <span style={{ fontSize: "14px" }}>
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
                    onClick={() => onFireEntry(entry.id)}
                    style={{
                        background: "none",
                        border: `1px solid ${t.headingColor}`,
                        borderRadius: "3px",
                        cursor: "pointer",
                        color: t.headingColor,
                        fontSize: "12px",
                        padding: "1px 5px",
                        flexShrink: 0,
                        lineHeight: 1.2,
                    }}
                    aria-label={localizeText(lang, "Send now (interrupt current)", "立即发送（打断当前会话）", "立即發送（打斷當前會話）")}
                    title={localizeText(lang, "Send now", "发射", "發射")}
                >
                    ⏎
                </button>
            )}
            <button
                data-testid={`edit-btn-${entry.id}`}
                onClick={handleStartEdit}
                style={{
                    background: "none",
                    border: "none",
                    cursor: "pointer",
                    color: t.textMuted,
                    fontSize: "13px",
                    padding: "2px 4px",
                    flexShrink: 0,
                    lineHeight: 1,
                }}
                aria-label={localizeText(lang, "Edit entry", "编辑条目", "編輯條目")}
                title={localizeText(lang, "Edit", "编辑", "編輯")}
            >
                ✏️
            </button>
            <button
                data-testid={`delete-btn-${entry.id}`}
                onClick={() => onDelete(entry.id)}
                style={{
                    background: "none",
                    border: "none",
                    cursor: "pointer",
                    color: t.textMuted,
                    fontSize: "13px",
                    padding: "2px 4px",
                    flexShrink: 0,
                    lineHeight: 1,
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
