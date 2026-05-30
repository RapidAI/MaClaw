import { useEffect, useRef, useState, type CSSProperties, type DragEvent, type MouseEvent } from 'react';
import { MAX_FAVORITE_EMPLOYEES } from '../settings/favoriteEmployees';

export interface FavoriteEmployeeSlot {
    veId: string;
    name: string;
    online: boolean;
    skillDescription?: string;
}

interface FavoriteEmployeeButtonsProps {
    slots: FavoriteEmployeeSlot[];
    veAuthorized: boolean;
    lang?: string;
    onStartConversation: (veId: string) => void;
    onReorder: (newOrder: string[]) => void;
    onRemove?: (veId: string) => void;
    onRename?: (veId: string, name: string) => void | Promise<void>;
}

const labels = {
    en: {
        rename: 'Rename',
        remove: 'Remove',
        renameTitle: 'Rename digital employee',
        nameLabel: 'Display name',
        cancel: 'Cancel',
        save: 'Save',
        saveFailed: 'Rename failed. Please try again.',
        offline: 'offline',
    },
    zhHans: {
        rename: '\u6539\u540d',
        remove: '\u79fb\u9664',
        renameTitle: '\u6539\u540d\u6570\u5b57\u5458\u5de5',
        nameLabel: '\u663e\u793a\u540d\u79f0',
        cancel: '\u53d6\u6d88',
        save: '\u4fdd\u5b58',
        saveFailed: '\u6539\u540d\u5931\u8d25\uff0c\u8bf7\u91cd\u8bd5\u3002',
        offline: '\u79bb\u7ebf',
    },
    zhHant: {
        rename: '\u6539\u540d',
        remove: '\u79fb\u9664',
        renameTitle: '\u6539\u540d\u6578\u5b57\u54e1\u5de5',
        nameLabel: '\u986f\u793a\u540d\u7a31',
        cancel: '\u53d6\u6d88',
        save: '\u5132\u5b58',
        saveFailed: '\u6539\u540d\u5931\u6557\uff0c\u8acb\u91cd\u8a66\u3002',
        offline: '\u96e2\u7dda',
    },
};

const MENU_WIDTH = 120;
const MENU_HEIGHT = 100;
const MENU_MARGIN = 8;

function favoriteLabels(lang?: string) {
    if (lang === 'zh-Hans' || lang === 'zh') return labels.zhHans;
    if (lang === 'zh-Hant') return labels.zhHant;
    return labels.en;
}

function clampMenuPosition(x: number, y: number) {
    const viewportWidth = window.innerWidth || 0;
    const viewportHeight = window.innerHeight || 0;
    return {
        x: Math.max(MENU_MARGIN, Math.min(x, Math.max(MENU_MARGIN, viewportWidth - MENU_WIDTH - MENU_MARGIN))),
        y: Math.max(MENU_MARGIN, Math.min(y, Math.max(MENU_MARGIN, viewportHeight - MENU_HEIGHT - MENU_MARGIN))),
    };
}

// Generate a stable hue from a string so each employee gets a unique avatar color.
const AVATAR_HUES = [210, 260, 330, 160, 30, 190, 290, 50, 130, 350];
function avatarColor(name: string): string {
    let hash = 0;
    for (let i = 0; i < name.length; i++) {
        hash = ((hash << 5) - hash + name.charCodeAt(i)) | 0;
    }
    const hue = AVATAR_HUES[Math.abs(hash) % AVATAR_HUES.length];
    return `hsl(${hue}, 55%, 48%)`;
}

// Extract up to 2 display characters for the avatar.
// For CJK names: first 2 chars. For latin: first 2 uppercase letters.
function avatarInitials(name: string): string {
    const trimmed = name.trim();
    if (!trimmed) return "?";
    // Check if first char is CJK
    const code = trimmed.charCodeAt(0);
    const isCJK = code >= 0x4e00 && code <= 0x9fff;
    if (isCJK) {
        return trimmed.slice(0, 2);
    }
    // Latin: take first letter of first two words, or first two chars
    const words = trimmed.split(/\s+/);
    if (words.length >= 2) {
        return (words[0][0] + words[1][0]).toUpperCase();
    }
    return trimmed.slice(0, 2).toUpperCase();
}

export function FavoriteEmployeeButtons({ slots, veAuthorized, lang, onStartConversation, onReorder, onRemove, onRename }: FavoriteEmployeeButtonsProps) {
    const [dragOverIndex, setDragOverIndex] = useState<number | null>(null);
    const [contextMenu, setContextMenu] = useState<{ x: number; y: number; slot: FavoriteEmployeeSlot } | null>(null);
    const [renamingSlot, setRenamingSlot] = useState<FavoriteEmployeeSlot | null>(null);
    const [renameValue, setRenameValue] = useState('');
    const [renameSaving, setRenameSaving] = useState(false);
    const [renameError, setRenameError] = useState('');
    const dragSourceIndex = useRef<number | null>(null);
    const didDrag = useRef(false);
    const inputRef = useRef<HTMLInputElement | null>(null);
    const mountedRef = useRef(true);
    const text = favoriteLabels(lang);

    useEffect(() => {
        return () => { mountedRef.current = false; };
    }, []);

    useEffect(() => {
        if (!contextMenu && !renamingSlot) return;
        const handlePointerDown = () => setContextMenu(null);
        const handleKeyDown = (event: KeyboardEvent) => {
            if (event.key === 'Escape') {
                setContextMenu(null);
                if (!renameSaving) setRenamingSlot(null);
            }
        };
        window.addEventListener('pointerdown', handlePointerDown);
        window.addEventListener('keydown', handleKeyDown);
        return () => {
            window.removeEventListener('pointerdown', handlePointerDown);
            window.removeEventListener('keydown', handleKeyDown);
        };
    }, [contextMenu, renamingSlot, renameSaving]);

    useEffect(() => {
        if (!renamingSlot) return;
        const timer = window.setTimeout(() => inputRef.current?.focus(), 0);
        return () => window.clearTimeout(timer);
    }, [renamingSlot]);

    if (!veAuthorized || slots.length === 0) return null;

    const handleDragStart = (index: number) => (e: DragEvent) => {
        dragSourceIndex.current = index;
        didDrag.current = true;
        if (e.dataTransfer) {
            e.dataTransfer.effectAllowed = 'move';
            e.dataTransfer.setData('text/plain', String(index));
        }
    };

    const handleDragOver = (index: number) => (e: DragEvent) => {
        e.preventDefault();
        if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
        setDragOverIndex(index);
    };

    const handleDrop = (targetIndex: number) => (e: DragEvent) => {
        e.preventDefault();
        setDragOverIndex(null);
        const sourceIndex = dragSourceIndex.current;
        if (sourceIndex === null || sourceIndex === targetIndex) return;
        const newOrder = slots.map(s => s.veId);
        const [moved] = newOrder.splice(sourceIndex, 1);
        newOrder.splice(targetIndex, 0, moved);
        onReorder(newOrder);
        dragSourceIndex.current = null;
    };

    const handleDragEnd = () => {
        setDragOverIndex(null);
        dragSourceIndex.current = null;
        // Reset drag flag after a tick so onClick can check it
        setTimeout(() => { didDrag.current = false; }, 0);
    };

    const handleClick = (veId: string) => () => {
        // Prevent click from firing after a drag operation
        if (didDrag.current) return;
        onStartConversation(veId);
    };

    const handleContextMenu = (slot: FavoriteEmployeeSlot) => (e: MouseEvent) => {
        e.preventDefault();
        e.stopPropagation();
        setContextMenu({ ...clampMenuPosition(e.clientX, e.clientY), slot });
    };

    const openRenameDialog = (slot: FavoriteEmployeeSlot) => {
        setContextMenu(null);
        setRenamingSlot(slot);
        setRenameValue(slot.name);
        setRenameSaving(false);
        setRenameError('');
    };

    const saveRename = async () => {
        if (!renamingSlot || renameSaving) return;
        const nextName = renameValue.trim();
        if (!nextName) return;
        setRenameSaving(true);
        setRenameError('');
        try {
            await onRename?.(renamingSlot.veId, nextName);
            if (!mountedRef.current) return;
            setRenamingSlot(null);
        } catch (error) {
            if (!mountedRef.current) return;
            console.error('Failed to rename favorite digital employee:', error);
            setRenameError(text.saveFailed);
        } finally {
            if (mountedRef.current) setRenameSaving(false);
        }
    };

    const isFull = slots.length >= MAX_FAVORITE_EMPLOYEES;

    return (
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '2px', width: '100%' }}>
            {slots.map((slot, index) => (
                <button
                    key={slot.veId}
                    type="button"
                    data-testid={`fav-ve-${slot.veId}`}
                    draggable
                    onDragStart={handleDragStart(index)}
                    onDragOver={handleDragOver(index)}
                    onDrop={handleDrop(index)}
                    onDragEnd={handleDragEnd}
                    onClick={handleClick(slot.veId)}
                    onContextMenu={handleContextMenu(slot)}
                    aria-label={`${slot.name}${slot.online ? '' : ` ${text.offline}`}`}
                    title={`${slot.name}${slot.skillDescription ? '\n' + slot.skillDescription : ''}`}
                    style={{
                        display: 'flex',
                        flexDirection: 'column',
                        alignItems: 'center',
                        gap: '2px',
                        padding: '4px 0',
                        width: '100%',
                        minHeight: 44,
                        cursor: 'pointer',
                        opacity: slot.online ? 1 : 0.5,
                        border: 0,
                        borderTop: dragOverIndex === index ? '2px solid var(--theme-primary)' : '2px solid transparent',
                        background: 'transparent',
                        font: 'inherit',
                        transition: 'opacity 0.15s',
                    }}
                >
                    {/* Avatar circle with online indicator */}
                    <div style={{ position: 'relative', width: '28px', height: '28px' }}>
                        <div style={{
                            width: '28px',
                            height: '28px',
                            borderRadius: '50%',
                            background: avatarColor(slot.name),
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            fontSize: '11px',
                            fontWeight: 700,
                            color: '#fff',
                            letterSpacing: 0,
                        }}>
                            {avatarInitials(slot.name)}
                        </div>
                        <span style={{
                            position: 'absolute',
                            bottom: '0px',
                            right: '0px',
                            width: '8px',
                            height: '8px',
                            borderRadius: '50%',
                            background: slot.online ? '#22c55e' : '#9ca3af',
                            border: '1.5px solid var(--theme-page-bg)',
                        }} />
                    </div>
                    {/* Name (truncated) */}
                    <span style={{
                        fontSize: '0.6rem',
                        lineHeight: 1,
                        fontWeight: 600,
                        color: 'var(--theme-text-primary)',
                        maxWidth: '52px',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                        textAlign: 'center',
                    }}>
                        {slot.name.length > 4 ? slot.name.slice(0, 4) + '...' : slot.name}
                    </span>
                </button>
            ))}
            {contextMenu && (
                <div
                    role="menu"
                    onPointerDown={(e) => e.stopPropagation()}
                    style={{
                        position: 'fixed',
                        left: contextMenu.x,
                        top: contextMenu.y,
                        minWidth: MENU_WIDTH,
                        zIndex: 4000,
                        padding: '6px',
                        borderRadius: 8,
                        border: '1px solid var(--theme-border)',
                        background: 'var(--theme-surface, var(--theme-page-bg))',
                        boxShadow: '0 10px 28px rgba(15, 23, 42, 0.18)',
                    }}
                >
                    <button type="button" role="menuitem" onClick={() => openRenameDialog(contextMenu.slot)} style={menuItemStyle}>
                        {text.rename}
                    </button>
                    <button
                        type="button"
                        role="menuitem"
                        onClick={() => {
                            onRemove?.(contextMenu.slot.veId);
                            setContextMenu(null);
                        }}
                        style={{ ...menuItemStyle, color: 'var(--theme-danger, #dc2626)' }}
                    >
                        {text.remove}
                    </button>
                </div>
            )}
            {renamingSlot && (
                <div
                    role="dialog"
                    aria-modal="true"
                    aria-labelledby="favorite-employee-rename-title"
                    onPointerDown={() => { if (!renameSaving) setRenamingSlot(null); }}
                    style={{
                        position: 'fixed',
                        inset: 0,
                        zIndex: 4100,
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        background: 'rgba(15, 23, 42, 0.32)',
                    }}
                >
                    <form
                        onPointerDown={(e) => e.stopPropagation()}
                        onSubmit={(e) => { e.preventDefault(); void saveRename(); }}
                        style={{
                            width: 'min(360px, calc(100vw - 32px))',
                            padding: '18px',
                            borderRadius: 8,
                            border: '1px solid var(--theme-border)',
                            background: 'var(--theme-page-bg)',
                            boxShadow: '0 18px 44px rgba(15, 23, 42, 0.24)',
                        }}
                    >
                        <h2 id="favorite-employee-rename-title" style={{ margin: '0 0 14px', fontSize: 16, lineHeight: 1.3, color: 'var(--theme-text-primary)' }}>
                            {text.renameTitle}
                        </h2>
                        <label style={{ display: 'grid', gap: 8, fontSize: 13, fontWeight: 600, color: 'var(--theme-text-primary)' }}>
                            {text.nameLabel}
                            <input
                                ref={inputRef}
                                value={renameValue}
                                disabled={renameSaving}
                                aria-invalid={renameError ? 'true' : undefined}
                                aria-describedby={renameError ? 'favorite-employee-rename-error' : undefined}
                                onChange={(e) => setRenameValue(e.target.value)}
                                maxLength={32}
                                style={{
                                    height: 44,
                                    borderRadius: 8,
                                    border: '1px solid var(--theme-border)',
                                    padding: '0 10px',
                                    background: 'var(--theme-surface, #fff)',
                                    color: 'var(--theme-text-primary)',
                                    font: 'inherit',
                                }}
                            />
                            {renameError && (
                                <span id="favorite-employee-rename-error" role="alert" style={{ color: 'var(--theme-danger, #dc2626)', fontSize: 12, lineHeight: 1.4 }}>
                                    {renameError}
                                </span>
                            )}
                        </label>
                        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 18 }}>
                            <button type="button" onClick={() => setRenamingSlot(null)} disabled={renameSaving} style={{ ...dialogButtonStyle, opacity: renameSaving ? 0.55 : 1 }}>
                                {text.cancel}
                            </button>
                            <button
                                type="submit"
                                disabled={!renameValue.trim() || renameSaving}
                                style={{
                                    ...dialogButtonStyle,
                                    borderColor: 'var(--theme-primary)',
                                    background: 'var(--theme-primary)',
                                    color: '#fff',
                                    opacity: renameValue.trim() && !renameSaving ? 1 : 0.55,
                                }}
                            >
                                {text.save}
                            </button>
                        </div>
                    </form>
                </div>
            )}
            {/* Separator when the rail is full. */}
            {isFull && (
                <div style={{
                    width: '24px',
                    height: '1px',
                    margin: '6px 0 2px',
                    background: 'linear-gradient(90deg, transparent, var(--theme-border) 20%, var(--theme-text-muted) 50%, var(--theme-border) 80%, transparent)',
                    opacity: 0.6,
                    borderRadius: '1px',
                }} />
            )}
        </div>
    );
}

const menuItemStyle: CSSProperties = {
    width: '100%',
    minHeight: 44,
    border: 0,
    borderRadius: 6,
    padding: '0 10px',
    background: 'transparent',
    color: 'var(--theme-text-primary)',
    textAlign: 'left',
    cursor: 'pointer',
    font: 'inherit',
    fontSize: 13,
};

const dialogButtonStyle: CSSProperties = {
    minWidth: 72,
    minHeight: 44,
    borderRadius: 8,
    border: '1px solid var(--theme-border)',
    background: 'var(--theme-surface, transparent)',
    color: 'var(--theme-text-primary)',
    font: 'inherit',
    fontWeight: 700,
    cursor: 'pointer',
};
