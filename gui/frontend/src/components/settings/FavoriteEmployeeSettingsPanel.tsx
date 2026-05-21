import { useCallback, useEffect, useRef, useState } from 'react';
import type { VirtualEmployeeEntry } from '../ai/VirtualEmployeeTab';
import { MAX_FAVORITE_EMPLOYEES } from './favoriteEmployees';

interface FavoriteEmployeeSettingsPanelProps {
    favoriteEmployeeIds: string[];
    veList: VirtualEmployeeEntry[];
    onAdd: (veId: string) => void;
    onRemove: (veId: string) => void;
    onReorder: (newOrder: string[]) => void;
    lang?: string;
}

export function FavoriteEmployeeSettingsPanel({ favoriteEmployeeIds, veList, onAdd, onRemove, onReorder, lang }: FavoriteEmployeeSettingsPanelProps) {
    const isZh = !lang || lang.startsWith('zh');
    const [showAddPicker, setShowAddPicker] = useState(false);
    const dragSourceIndex = useRef<number | null>(null);
    const [dragOverIndex, setDragOverIndex] = useState<number | null>(null);

    const favoriteVEs = favoriteEmployeeIds.map(id => {
        const ve = veList.find(v => v.id === id);
        return { id, name: ve?.name || id.slice(0, 8), online: ve?.online_status === 'online' };
    });

    const availableVEs = veList.filter(ve => !favoriteEmployeeIds.includes(ve.id));

    const handleDragStart = (index: number) => (e: React.DragEvent) => {
        dragSourceIndex.current = index;
        if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move';
    };

    const handleDragOver = (index: number) => (e: React.DragEvent) => {
        e.preventDefault();
        setDragOverIndex(index);
    };

    const handleDrop = (targetIndex: number) => (e: React.DragEvent) => {
        e.preventDefault();
        setDragOverIndex(null);
        const sourceIndex = dragSourceIndex.current;
        if (sourceIndex === null || sourceIndex === targetIndex) return;
        const newOrder = [...favoriteEmployeeIds];
        const [moved] = newOrder.splice(sourceIndex, 1);
        newOrder.splice(targetIndex, 0, moved);
        onReorder(newOrder);
        dragSourceIndex.current = null;
    };

    return (
        <div data-testid="fav-employee-settings">
            <h3 style={{ marginBottom: '12px' }}>{isZh ? '常用数字员工' : 'Favorite Employees'}</h3>
            <p style={{ fontSize: '12px', color: 'var(--theme-text-muted)', marginBottom: '16px' }}>
                {isZh ? '最多设置 6 个常用数字员工，显示在左侧导航栏中。拖动调整顺序。' : 'Pin up to 6 digital employees to the sidebar. Drag to reorder.'}
            </p>

            {/* Current favorites list */}
            {favoriteVEs.length === 0 ? (
                <div style={{ padding: '16px', textAlign: 'center', color: 'var(--theme-text-muted)', fontSize: '12px' }}>
                    {isZh ? '暂无常用数字员工' : 'No favorites set'}
                </div>
            ) : (
                <div style={{ marginBottom: '12px' }}>
                    {favoriteVEs.map((fav, index) => (
                        <div
                            key={fav.id}
                            draggable
                            onDragStart={handleDragStart(index)}
                            onDragOver={handleDragOver(index)}
                            onDrop={handleDrop(index)}
                            onDragEnd={() => { setDragOverIndex(null); dragSourceIndex.current = null; }}
                            style={{
                                display: 'flex',
                                alignItems: 'center',
                                gap: '10px',
                                padding: '8px 12px',
                                borderRadius: '6px',
                                marginBottom: '4px',
                                cursor: 'grab',
                                borderTop: dragOverIndex === index ? '2px solid var(--theme-primary)' : '2px solid transparent',
                                background: 'var(--theme-field-bg, rgba(0,0,0,0.02))',
                            }}
                        >
                            <span style={{ fontSize: '11px', color: 'var(--theme-text-muted)', width: '16px' }}>{index + 1}.</span>
                            <span style={{
                                width: '8px', height: '8px', borderRadius: '50%', flexShrink: 0,
                                background: fav.online ? '#22c55e' : '#9ca3af',
                            }} />
                            <span style={{ flex: 1, fontSize: '13px', fontWeight: 500 }}>{fav.name}</span>
                            <button
                                type="button"
                                onClick={() => onRemove(fav.id)}
                                style={{
                                    fontSize: '11px', padding: '2px 8px', borderRadius: '4px',
                                    border: '1px solid var(--theme-border)', background: 'transparent',
                                    color: 'var(--theme-text-muted)', cursor: 'pointer',
                                }}
                            >
                                {isZh ? '移除' : 'Remove'}
                            </button>
                        </div>
                    ))}
                </div>
            )}

            {/* Add button */}
            {favoriteVEs.length < MAX_FAVORITE_EMPLOYEES && (
                <div style={{ position: 'relative' }}>
                    <button
                        type="button"
                        onClick={() => setShowAddPicker(!showAddPicker)}
                        style={{
                            fontSize: '12px', padding: '6px 14px', borderRadius: '6px',
                            border: '1px solid var(--theme-primary)', background: 'transparent',
                            color: 'var(--theme-primary)', cursor: 'pointer',
                        }}
                    >
                        + {isZh ? '添加常用' : 'Add Favorite'}
                    </button>

                    {showAddPicker && (
                        <div style={{
                            position: 'absolute', top: '100%', left: 0, marginTop: '4px',
                            background: 'var(--theme-surface)', border: '1px solid var(--theme-border)',
                            borderRadius: '8px', boxShadow: '0 4px 12px rgba(0,0,0,0.12)',
                            maxHeight: '200px', overflowY: 'auto', minWidth: '200px', zIndex: 100,
                            padding: '4px 0',
                        }}>
                            {availableVEs.length === 0 ? (
                                <div style={{ padding: '12px', fontSize: '12px', color: 'var(--theme-text-muted)', textAlign: 'center' }}>
                                    {isZh ? '无可添加的数字员工' : 'No employees available'}
                                </div>
                            ) : (
                                availableVEs.map(ve => (
                                    <button
                                        key={ve.id}
                                        type="button"
                                        onClick={() => { onAdd(ve.id); setShowAddPicker(false); }}
                                        style={{
                                            width: '100%', border: 0, background: 'transparent', color: 'inherit',
                                            padding: '6px 14px', cursor: 'pointer', fontSize: '12px', textAlign: 'left',
                                            display: 'flex', alignItems: 'center', gap: '8px',
                                        }}
                                        onMouseEnter={e => { (e.currentTarget as HTMLElement).style.background = 'var(--theme-hover, rgba(0,0,0,0.04))'; }}
                                        onMouseLeave={e => { (e.currentTarget as HTMLElement).style.background = ''; }}
                                    >
                                        <span style={{
                                            width: '6px', height: '6px', borderRadius: '50%',
                                            background: ve.online_status === 'online' ? '#22c55e' : '#9ca3af',
                                        }} />
                                        <span>{ve.name}</span>
                                    </button>
                                ))
                            )}
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}
