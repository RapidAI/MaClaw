import { useRef, useState } from 'react';
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
    onStartConversation: (veId: string) => void;
    onReorder: (newOrder: string[]) => void;
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

export function FavoriteEmployeeButtons({ slots, veAuthorized, onStartConversation, onReorder }: FavoriteEmployeeButtonsProps) {
    const [dragOverIndex, setDragOverIndex] = useState<number | null>(null);
    const dragSourceIndex = useRef<number | null>(null);
    const didDrag = useRef(false);

    if (!veAuthorized || slots.length === 0) return null;

    const handleDragStart = (index: number) => (e: React.DragEvent) => {
        dragSourceIndex.current = index;
        didDrag.current = true;
        if (e.dataTransfer) {
            e.dataTransfer.effectAllowed = 'move';
            e.dataTransfer.setData('text/plain', String(index));
        }
    };

    const handleDragOver = (index: number) => (e: React.DragEvent) => {
        e.preventDefault();
        if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
        setDragOverIndex(index);
    };

    const handleDrop = (targetIndex: number) => (e: React.DragEvent) => {
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
                    aria-label={`${slot.name}${slot.online ? '' : ' offline'}`}
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
                            letterSpacing: '-0.5px',
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
            {/* Separator when at full capacity — visually separates employees from system buttons below */}
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
