import { useRef, useState } from 'react';

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

export function FavoriteEmployeeButtons({ slots, veAuthorized, onStartConversation, onReorder }: FavoriteEmployeeButtonsProps) {
    const [dragOverIndex, setDragOverIndex] = useState<number | null>(null);
    const dragSourceIndex = useRef<number | null>(null);
    const didDrag = useRef(false);

    if (!veAuthorized || slots.length === 0) return null;

    const handleDragStart = (index: number) => (e: React.DragEvent) => {
        dragSourceIndex.current = index;
        didDrag.current = true;
        e.dataTransfer.effectAllowed = 'move';
        e.dataTransfer.setData('text/plain', String(index));
    };

    const handleDragOver = (index: number) => (e: React.DragEvent) => {
        e.preventDefault();
        e.dataTransfer.dropEffect = 'move';
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

    return (
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '2px', width: '100%' }}>
            {slots.map((slot, index) => (
                <div
                    key={slot.veId}
                    data-testid={`fav-ve-${slot.veId}`}
                    draggable
                    onDragStart={handleDragStart(index)}
                    onDragOver={handleDragOver(index)}
                    onDrop={handleDrop(index)}
                    onDragEnd={handleDragEnd}
                    onClick={handleClick(slot.veId)}
                    title={`${slot.name}${slot.skillDescription ? '\n' + slot.skillDescription : ''}`}
                    style={{
                        display: 'flex',
                        flexDirection: 'column',
                        alignItems: 'center',
                        gap: '2px',
                        padding: '4px 0',
                        width: '100%',
                        cursor: 'pointer',
                        opacity: slot.online ? 1 : 0.5,
                        borderTop: dragOverIndex === index ? '2px solid var(--theme-primary)' : '2px solid transparent',
                        transition: 'opacity 0.15s',
                    }}
                >
                    {/* Avatar circle with online indicator */}
                    <div style={{ position: 'relative', width: '28px', height: '28px' }}>
                        <div style={{
                            width: '28px',
                            height: '28px',
                            borderRadius: '50%',
                            background: slot.online ? 'var(--theme-primary)' : 'var(--theme-text-muted)',
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            fontSize: '12px',
                            fontWeight: 700,
                            color: '#fff',
                        }}>
                            {slot.name.charAt(0).toUpperCase()}
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
                        {slot.name.length > 4 ? slot.name.slice(0, 4) + '…' : slot.name}
                    </span>
                </div>
            ))}
        </div>
    );
}
