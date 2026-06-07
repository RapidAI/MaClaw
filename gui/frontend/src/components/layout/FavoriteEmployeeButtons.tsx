import { useEffect, useRef, useState, type CSSProperties, type DragEvent, type MouseEvent } from 'react';
import { safeAvatarDataURL } from '../ai/virtualEmployeeAvatar';
import { MAX_USER_FAVORITES } from '../settings/favoriteEmployees';

export interface FavoriteEmployeeSlot {
    veId: string;
    name: string;
    online: boolean;
    skillDescription?: string;
    avatarDataURL?: string;
    resident?: boolean;
    /** Access policy: public / whitelist / blacklist / per_request */
    accessPolicy?: string;
    /** Registration time (ISO string) */
    registeredAt?: string;
    /** Machine ID of the owner (present for remote VEs) */
    machineId?: string;
    /** Accessible department names (whitelist). Empty/undefined means no restriction. */
    allowedDepartments?: string[];
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
        startChat: 'Start Conversation',
        rename: 'Rename',
        viewInfo: 'View Info',
        moveUp: 'Move Up',
        moveDown: 'Move Down',
        remove: 'Remove from Favorites',
        resident: 'Resident',
        renameTitle: 'Rename digital employee',
        nameLabel: 'Display name',
        cancel: 'Cancel',
        save: 'Save',
        saveFailed: 'Rename failed. Please try again.',
        offline: 'offline',
        infoTitle: 'Digital Employee Info',
        infoSource: 'Source',
        infoSourceVirtual: 'Virtual Employee (Remote)',
        infoSourceLocal: 'Local Employee (This Machine)',
        infoSkill: 'Skill',
        infoPolicy: 'Access Policy',
        infoRegistered: 'Registered',
        infoStatus: 'Status',
        infoOnline: 'Online',
        infoOffline: 'Offline',
        infoResident: 'Resident (always displayed)',
        infoPolicyPublic: 'Public',
        infoPolicyWhitelist: 'Allowlist',
        infoPolicyBlacklist: 'Blocklist',
        infoPolicyPerRequest: 'Approval required',
        infoClose: 'Close',
        infoNoDescription: 'No description available',
        infoDepartments: 'Accessible Departments',
        infoDepartmentsUnrestricted: 'Unrestricted',
    },
    zhHans: {
        startChat: '\u5f00\u59cb\u5bf9\u8bdd',
        rename: '\u6539\u540d',
        viewInfo: '\u67e5\u770b\u4fe1\u606f',
        moveUp: '\u4e0a\u79fb',
        moveDown: '\u4e0b\u79fb',
        remove: '\u4ece\u5e38\u7528\u4e2d\u79fb\u9664',
        resident: '\u5e38\u9a7b',
        renameTitle: '\u6539\u540d\u6570\u5b57\u5458\u5de5',
        nameLabel: '\u663e\u793a\u540d\u79f0',
        cancel: '\u53d6\u6d88',
        save: '\u4fdd\u5b58',
        saveFailed: '\u6539\u540d\u5931\u8d25\uff0c\u8bf7\u91cd\u8bd5\u3002',
        offline: '\u79bb\u7ebf',
        infoTitle: '\u6570\u5b57\u5458\u5de5\u4fe1\u606f',
        infoSource: '\u6765\u6e90',
        infoSourceVirtual: '\u865a\u62df\u5458\u5de5\uff08\u8fdc\u7a0b\uff09',
        infoSourceLocal: '\u672c\u673a\u5458\u5de5',
        infoSkill: '\u6280\u80fd\u4ecb\u7ecd',
        infoPolicy: '\u8bbf\u95ee\u7b56\u7565',
        infoRegistered: '\u6ce8\u518c\u65f6\u95f4',
        infoStatus: '\u72b6\u6001',
        infoOnline: '\u5728\u7ebf',
        infoOffline: '\u79bb\u7ebf',
        infoResident: '\u5e38\u9a7b\uff08\u59cb\u7ec8\u663e\u793a\uff09',
        infoPolicyPublic: '\u516c\u5f00\u8bbf\u95ee',
        infoPolicyWhitelist: '\u767d\u540d\u5355',
        infoPolicyBlacklist: '\u9ed1\u540d\u5355',
        infoPolicyPerRequest: '\u9700\u8981\u5ba1\u6279',
        infoClose: '\u5173\u95ed',
        infoNoDescription: '\u6682\u65e0\u6280\u80fd\u4ecb\u7ecd',
        infoDepartments: '\u53ef\u8bbf\u95ee\u90e8\u95e8',
        infoDepartmentsUnrestricted: '\u65e0\u9650\u5236',
    },
    zhHant: {
        startChat: '\u958b\u59cb\u5c0d\u8a71',
        rename: '\u6539\u540d',
        viewInfo: '\u67e5\u770b\u8cc7\u8a0a',
        moveUp: '\u4e0a\u79fb',
        moveDown: '\u4e0b\u79fb',
        remove: '\u5f9e\u5e38\u7528\u4e2d\u79fb\u9664',
        resident: '\u5e38\u99d0',
        renameTitle: '\u6539\u540d\u6578\u5b57\u54e1\u5de5',
        nameLabel: '\u986f\u793a\u540d\u7a31',
        cancel: '\u53d6\u6d88',
        save: '\u5132\u5b58',
        saveFailed: '\u6539\u540d\u5931\u6557\uff0c\u8acb\u91cd\u8a66\u3002',
        offline: '\u96e2\u7dda',
        infoTitle: '\u6578\u5b57\u54e1\u5de5\u8cc7\u8a0a',
        infoSource: '\u4f86\u6e90',
        infoSourceVirtual: '\u865b\u64ec\u54e1\u5de5\uff08\u9060\u7a0b\uff09',
        infoSourceLocal: '\u672c\u6a5f\u54e1\u5de5',
        infoSkill: '\u6280\u80fd\u4ecb\u7d39',
        infoPolicy: '\u5b58\u53d6\u7b56\u7565',
        infoRegistered: '\u8a3b\u518a\u6642\u9593',
        infoStatus: '\u72c0\u614b',
        infoOnline: '\u5728\u7dda',
        infoOffline: '\u96e2\u7dda',
        infoResident: '\u5e38\u99d0\uff08\u59cb\u7d42\u986f\u793a\uff09',
        infoPolicyPublic: '\u516c\u958b\u5b58\u53d6',
        infoPolicyWhitelist: '\u767d\u540d\u55ae',
        infoPolicyBlacklist: '\u9ed1\u540d\u55ae',
        infoPolicyPerRequest: '\u9700\u8981\u5be9\u6279',
        infoClose: '\u95dc\u9589',
        infoNoDescription: '\u66ab\u7121\u6280\u80fd\u4ecb\u7d39',
        infoDepartments: '\u53ef\u5b58\u53d6\u90e8\u9580',
        infoDepartmentsUnrestricted: '\u7121\u9650\u5236',
    },
};

const MENU_WIDTH = 160;
const MENU_HEIGHT = 240;
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
    const [contextMenu, setContextMenu] = useState<{ x: number; y: number; slot: FavoriteEmployeeSlot; index: number } | null>(null);
    const [renamingSlot, setRenamingSlot] = useState<FavoriteEmployeeSlot | null>(null);
    const [viewInfoSlot, setViewInfoSlot] = useState<FavoriteEmployeeSlot | null>(null);
    const [renameValue, setRenameValue] = useState('');
    const [renameSaving, setRenameSaving] = useState(false);
    const [renameError, setRenameError] = useState('');
    const dragSourceIndex = useRef<number | null>(null);
    const didDrag = useRef(false);
    const inputRef = useRef<HTMLInputElement | null>(null);
    const menuRef = useRef<HTMLDivElement | null>(null);
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

    // Focus context menu when opened so Escape works immediately
    useEffect(() => {
        if (contextMenu) {
            const timer = window.setTimeout(() => menuRef.current?.focus(), 0);
            return () => window.clearTimeout(timer);
        }
    }, [contextMenu]);

    if (!veAuthorized || slots.length === 0) return null;

    const handleDragStart = (index: number) => (e: DragEvent) => {
        if (slots[index]?.resident) {
            e.preventDefault();
            return;
        }
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
        if (slots[sourceIndex]?.resident || slots[targetIndex]?.resident) return;
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

    const handleContextMenu = (slot: FavoriteEmployeeSlot, index: number) => (e: MouseEvent) => {
        e.preventDefault();
        e.stopPropagation();
        setContextMenu({ ...clampMenuPosition(e.clientX, e.clientY), slot, index });
    };

    const openRenameDialog = (slot: FavoriteEmployeeSlot) => {
        setContextMenu(null);
        setRenamingSlot(slot);
        setRenameValue(slot.name);
        setRenameSaving(false);
        setRenameError('');
    };

    const moveSlot = (fromIndex: number, direction: 'up' | 'down') => {
        if (slots[fromIndex]?.resident) return;
        const toIndex = direction === 'up' ? fromIndex - 1 : fromIndex + 1;
        if (toIndex < 0 || toIndex >= slots.length) return;
        if (slots[toIndex]?.resident) return;
        const newOrder = slots.map(s => s.veId);
        const [moved] = newOrder.splice(fromIndex, 1);
        newOrder.splice(toIndex, 0, moved);
        onReorder(newOrder);
        setContextMenu(null);
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

    const isFull = slots.filter(slot => !slot.resident).length >= MAX_USER_FAVORITES;
    const canMoveContextUp = !!contextMenu && contextMenu.index > 0 && !contextMenu.slot.resident && !slots[contextMenu.index - 1]?.resident;
    const canMoveContextDown = !!contextMenu && contextMenu.index < slots.length - 1 && !contextMenu.slot.resident && !slots[contextMenu.index + 1]?.resident;

    return (
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '2px', width: '100%' }}>
            {slots.map((slot, index) => {
                const avatarDataURL = safeAvatarDataURL(slot.avatarDataURL);
                return (
                <button
                    key={slot.veId}
                    type="button"
                    data-testid={`fav-ve-${slot.veId}`}
                    draggable={!slot.resident}
                    onDragStart={handleDragStart(index)}
                    onDragOver={handleDragOver(index)}
                    onDrop={handleDrop(index)}
                    onDragEnd={handleDragEnd}
                    onClick={handleClick(slot.veId)}
                    onContextMenu={handleContextMenu(slot, index)}
                    aria-label={`${slot.name}${slot.resident ? ` ${text.resident}` : ''}${slot.online ? '' : ` ${text.offline}`}`}
                    title={`${slot.name}${slot.resident ? '\n' + text.resident : ''}${slot.skillDescription ? '\n' + slot.skillDescription : ''}`}
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
                        {avatarDataURL ? (
                            <img
                                key={avatarDataURL}
                                src={avatarDataURL}
                                alt=""
                                data-testid={`fav-ve-avatar-${slot.veId}`}
                                style={{ width: '28px', height: '28px', borderRadius: '50%', objectFit: 'cover', display: 'block' }}
                            />
                        ) : (
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
                        )}
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
                    {/* Name (truncated by CSS) */}
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
                        {slot.name}
                    </span>
                </button>
                );
            })}
            {contextMenu && (
                <div
                    ref={menuRef}
                    role="menu"
                    tabIndex={-1}
                    onPointerDown={(e) => e.stopPropagation()}
                    onKeyDown={(e) => { if (e.key === 'Escape') setContextMenu(null); }}
                    style={{
                        position: 'fixed',
                        left: contextMenu.x,
                        top: contextMenu.y,
                        minWidth: MENU_WIDTH,
                        zIndex: 4000,
                        padding: '4px',
                        borderRadius: 10,
                        border: '1px solid var(--theme-border)',
                        background: 'var(--theme-surface, var(--theme-page-bg))',
                        boxShadow: '0 12px 32px rgba(15, 23, 42, 0.22), 0 2px 6px rgba(15, 23, 42, 0.08)',
                        backdropFilter: 'blur(8px)',
                        outline: 'none',
                    }}
                >
                    {/* Header: employee name */}
                    <div style={{
                        padding: '6px 10px 4px',
                        fontSize: 11,
                        fontWeight: 600,
                        color: 'var(--theme-text-muted)',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                        maxWidth: 200,
                        borderBottom: '1px solid var(--theme-border)',
                        marginBottom: 4,
                    }}>
                        {contextMenu.slot.name}
                    </div>

                    {/* Start conversation */}
                    <button
                        type="button"
                        role="menuitem"
                        onClick={() => { onStartConversation(contextMenu.slot.veId); setContextMenu(null); }}
                        style={menuItemStyle}
                        onMouseEnter={menuItemHover}
                        onMouseLeave={menuItemUnhover}
                    >
                        <span aria-hidden="true" style={menuIconStyle}>💬</span>
                        {text.startChat}
                    </button>

                    {/* Divider */}
                    <div style={menuDividerStyle} />

                    {/* Rename */}
                    {onRename && (
                    <button
                        type="button"
                        role="menuitem"
                        onClick={() => openRenameDialog(contextMenu.slot)}
                        style={menuItemStyle}
                        onMouseEnter={menuItemHover}
                        onMouseLeave={menuItemUnhover}
                    >
                        <span aria-hidden="true" style={menuIconStyle}>✏️</span>
                        {text.rename}
                    </button>
                    )}

                    {/* View Info */}
                    <button
                        type="button"
                        role="menuitem"
                        onClick={() => { setViewInfoSlot(contextMenu.slot); setContextMenu(null); }}
                        style={menuItemStyle}
                        onMouseEnter={menuItemHover}
                        onMouseLeave={menuItemUnhover}
                    >
                        <span aria-hidden="true" style={menuIconStyle}>ℹ️</span>
                        {text.viewInfo}
                    </button>

                    {/* Divider + Move controls (only when >1 slot) */}
                    {slots.length > 1 && (<>
                    <div style={menuDividerStyle} />

                    {/* Move up */}
                    <button
                        type="button"
                        role="menuitem"
                        disabled={!canMoveContextUp}
                        onClick={() => moveSlot(contextMenu.index, 'up')}
                        style={{ ...menuItemStyle, opacity: canMoveContextUp ? 1 : 0.4, cursor: canMoveContextUp ? 'pointer' : 'default' }}
                        onMouseEnter={canMoveContextUp ? menuItemHover : undefined}
                        onMouseLeave={menuItemUnhover}
                    >
                        <span aria-hidden="true" style={menuIconStyle}>↑</span>
                        {text.moveUp}
                    </button>

                    {/* Move down */}
                    <button
                        type="button"
                        role="menuitem"
                        disabled={!canMoveContextDown}
                        onClick={() => moveSlot(contextMenu.index, 'down')}
                        style={{ ...menuItemStyle, opacity: canMoveContextDown ? 1 : 0.4, cursor: canMoveContextDown ? 'pointer' : 'default' }}
                        onMouseEnter={canMoveContextDown ? menuItemHover : undefined}
                        onMouseLeave={menuItemUnhover}
                    >
                        <span aria-hidden="true" style={menuIconStyle}>↓</span>
                        {text.moveDown}
                    </button>
                    </>)}

                    {/* Divider + Remove */}
                    {onRemove && (<>
                    <div style={menuDividerStyle} />

                    <button
                        type="button"
                        role="menuitem"
                        disabled={contextMenu.slot.resident}
                        onClick={() => {
                            if (contextMenu.slot.resident) return;
                            onRemove(contextMenu.slot.veId);
                            setContextMenu(null);
                        }}
                        style={{ ...menuItemStyle, color: 'var(--theme-danger, #dc2626)', opacity: contextMenu.slot.resident ? 0.4 : 1, cursor: contextMenu.slot.resident ? 'default' : 'pointer' }}
                        onMouseEnter={contextMenu.slot.resident ? undefined : menuItemHover}
                        onMouseLeave={menuItemUnhover}
                    >
                        <span aria-hidden="true" style={menuIconStyle}>🗑</span>
                        {text.remove}
                    </button>
                    </>)}
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
            {/* Info dialog */}
            {viewInfoSlot && (
                <div
                    role="dialog"
                    aria-modal="true"
                    aria-labelledby="favorite-employee-info-title"
                    onPointerDown={() => setViewInfoSlot(null)}
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
                    <div
                        onPointerDown={(e) => e.stopPropagation()}
                        onKeyDown={(e) => { if (e.key === 'Escape') setViewInfoSlot(null); }}
                        tabIndex={-1}
                        style={{
                            width: 'min(400px, calc(100vw - 32px))',
                            maxHeight: 'calc(100vh - 64px)',
                            overflow: 'auto',
                            padding: '24px',
                            borderRadius: 12,
                            border: '1px solid var(--theme-border)',
                            background: 'var(--theme-page-bg)',
                            boxShadow: '0 18px 44px rgba(15, 23, 42, 0.24)',
                            outline: 'none',
                        }}
                    >
                        {/* Large avatar + name header */}
                        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 12, marginBottom: 20 }}>
                            <div style={{ position: 'relative' }}>
                                {safeAvatarDataURL(viewInfoSlot.avatarDataURL) ? (
                                    <img
                                        src={safeAvatarDataURL(viewInfoSlot.avatarDataURL)!}
                                        alt={viewInfoSlot.name}
                                        style={{ width: 72, height: 72, borderRadius: '50%', objectFit: 'cover', display: 'block', border: '3px solid var(--theme-border)' }}
                                    />
                                ) : (
                                    <div style={{
                                        width: 72, height: 72, borderRadius: '50%',
                                        background: avatarColor(viewInfoSlot.name),
                                        display: 'flex', alignItems: 'center', justifyContent: 'center',
                                        fontSize: 28, fontWeight: 700, color: '#fff',
                                        border: '3px solid var(--theme-border)',
                                    }}>
                                        {avatarInitials(viewInfoSlot.name)}
                                    </div>
                                )}
                                {/* Online indicator */}
                                <span style={{
                                    position: 'absolute', bottom: 2, right: 2,
                                    width: 14, height: 14, borderRadius: '50%',
                                    background: viewInfoSlot.online ? '#22c55e' : '#9ca3af',
                                    border: '2.5px solid var(--theme-page-bg)',
                                }} />
                            </div>
                            <h2 id="favorite-employee-info-title" style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--theme-text-primary)', textAlign: 'center' }}>
                                {viewInfoSlot.name}
                            </h2>
                            {viewInfoSlot.resident && (
                                <span style={{ fontSize: 11, color: 'var(--theme-primary)', background: 'var(--theme-hover, rgba(99,102,241,0.1))', padding: '2px 8px', borderRadius: 4, fontWeight: 600 }}>
                                    {text.infoResident}
                                </span>
                            )}
                        </div>

                        {/* Info rows */}
                        <div style={{ display: 'grid', gap: 12 }}>
                            {/* Status */}
                            <div style={infoRowStyle}>
                                <span style={infoLabelStyle}>{text.infoStatus}</span>
                                <span style={{ ...infoValueStyle, color: viewInfoSlot.online ? '#22c55e' : '#9ca3af', fontWeight: 600 }}>
                                    ● {viewInfoSlot.online ? text.infoOnline : text.infoOffline}
                                </span>
                            </div>

                            {/* Source */}
                            <div style={infoRowStyle}>
                                <span style={infoLabelStyle}>{text.infoSource}</span>
                                <span style={infoValueStyle}>
                                    {viewInfoSlot.machineId ? text.infoSourceVirtual : text.infoSourceLocal}
                                </span>
                            </div>

                            {/* Access policy */}
                            {viewInfoSlot.accessPolicy && (
                            <div style={infoRowStyle}>
                                <span style={infoLabelStyle}>{text.infoPolicy}</span>
                                <span style={infoValueStyle}>
                                    {formatPolicy(viewInfoSlot.accessPolicy, text)}
                                </span>
                            </div>
                            )}

                            {/* Accessible departments */}
                            <div style={infoRowStyle}>
                                <span style={infoLabelStyle}>{text.infoDepartments}</span>
                                <span style={infoValueStyle}>
                                    {viewInfoSlot.allowedDepartments && viewInfoSlot.allowedDepartments.length > 0
                                        ? viewInfoSlot.allowedDepartments.join('\uff1b')
                                        : text.infoDepartmentsUnrestricted}
                                </span>
                            </div>

                            {/* Registration time */}
                            {viewInfoSlot.registeredAt && (
                            <div style={infoRowStyle}>
                                <span style={infoLabelStyle}>{text.infoRegistered}</span>
                                <span style={infoValueStyle}>
                                    {formatRegisteredAt(viewInfoSlot.registeredAt)}
                                </span>
                            </div>
                            )}

                            {/* ID */}
                            <div style={infoRowStyle}>
                                <span style={infoLabelStyle}>ID</span>
                                <span style={{ ...infoValueStyle, fontSize: 11, fontFamily: 'monospace', opacity: 0.7 }}>
                                    {viewInfoSlot.veId}
                                </span>
                            </div>
                        </div>

                        {/* Skill description */}
                        <div style={{ marginTop: 16 }}>
                            <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--theme-text-muted)', marginBottom: 6 }}>{text.infoSkill}</div>
                            <div style={{
                                fontSize: 13, lineHeight: 1.6, color: 'var(--theme-text-primary)',
                                padding: '10px 12px', borderRadius: 8,
                                background: 'var(--theme-hover, rgba(255,255,255,0.03))',
                                border: '1px solid var(--theme-border)',
                                whiteSpace: 'pre-wrap', wordBreak: 'break-word',
                            }}>
                                {viewInfoSlot.skillDescription || text.infoNoDescription}
                            </div>
                        </div>

                        {/* Close button */}
                        <div style={{ display: 'flex', justifyContent: 'center', marginTop: 20 }}>
                            <button
                                type="button"
                                onClick={() => setViewInfoSlot(null)}
                                style={{ ...dialogButtonStyle, minWidth: 100 }}
                            >
                                {text.infoClose}
                            </button>
                        </div>
                    </div>
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
    display: 'flex',
    alignItems: 'center',
    gap: '8px',
    width: '100%',
    minHeight: 32,
    border: 0,
    borderRadius: 6,
    padding: '0 10px',
    background: 'transparent',
    color: 'var(--theme-text-primary)',
    textAlign: 'left',
    cursor: 'pointer',
    font: 'inherit',
    fontSize: 12,
    transition: 'background 0.12s',
};

function menuItemHover(e: { currentTarget: EventTarget & Element }) {
    (e.currentTarget as HTMLElement).style.background = 'var(--theme-hover, rgba(255,255,255,0.06))';
}
function menuItemUnhover(e: { currentTarget: EventTarget & Element }) {
    (e.currentTarget as HTMLElement).style.background = 'transparent';
}

const menuIconStyle: CSSProperties = {
    width: '18px',
    fontSize: '13px',
    textAlign: 'center',
    flexShrink: 0,
};

const menuDividerStyle: CSSProperties = {
    height: '1px',
    margin: '3px 8px',
    background: 'var(--theme-border)',
    opacity: 0.6,
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

const infoRowStyle: CSSProperties = {
    display: 'flex',
    alignItems: 'baseline',
    gap: 12,
};

const infoLabelStyle: CSSProperties = {
    fontSize: 12,
    fontWeight: 600,
    color: 'var(--theme-text-muted)',
    minWidth: 72,
    flexShrink: 0,
};

const infoValueStyle: CSSProperties = {
    fontSize: 13,
    color: 'var(--theme-text-primary)',
    wordBreak: 'break-word',
};

function formatPolicy(policy: string, text: { infoPolicyPublic: string; infoPolicyWhitelist: string; infoPolicyBlacklist: string; infoPolicyPerRequest: string }): string {
    switch (policy) {
        case 'public': return text.infoPolicyPublic;
        case 'whitelist': return text.infoPolicyWhitelist;
        case 'blacklist': return text.infoPolicyBlacklist;
        case 'per_request': return text.infoPolicyPerRequest;
        default: return policy;
    }
}

function formatRegisteredAt(isoStr: string): string {
    try {
        const d = new Date(isoStr);
        if (isNaN(d.getTime())) return isoStr;
        return d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
    } catch {
        return isoStr;
    }
}
