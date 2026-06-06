import { useEffect, useMemo, useRef, useState } from 'react';
import type { VirtualEmployeeEntry } from '../ai/VirtualEmployeeTab';
import { participantIdentityMatches } from '../ai/participantIdentity';
import { safeAvatarDataURL } from '../ai/virtualEmployeeAvatar';
import { isVirtualEmployeeOnline } from '../ai/virtualEmployeeStatus';
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
    const rootRef = useRef<HTMLDivElement | null>(null);
    const dragSourceIndex = useRef<number | null>(null);
    const [dragOverIndex, setDragOverIndex] = useState<number | null>(null);
    const onlineLabel = isZh ? '在线' : 'Online';
    const offlineLabel = isZh ? '离线' : 'Offline';

    const veById = useMemo(() => {
        const map = new Map<string, VirtualEmployeeEntry>();
        for (const ve of veList) {
            if (ve.id) map.set(ve.id, ve);
            if (ve.machine_id) map.set(ve.machine_id, ve);
        }
        return map;
    }, [veList]);
    const isFavoriteIdentity = (ve: Pick<VirtualEmployeeEntry, 'id' | 'machine_id'>) => {
        return favoriteEmployeeIds.some(id => participantIdentityMatches(id, ve.id) || participantIdentityMatches(id, ve.machine_id));
    };

    const favoriteVEs = favoriteEmployeeIds.map(id => {
        const ve = veById.get(id) || veList.find(item => participantIdentityMatches(item.id, id) || participantIdentityMatches(item.machine_id, id));
        return { id, name: ve?.name || id.slice(0, 8), online: isVirtualEmployeeOnline(ve), avatarDataURL: safeAvatarDataURL(ve?.avatar_data_url) };
    });

    const availableVEs = veList.filter(ve => isVirtualEmployeeOnline(ve) && !ve.resident && !isFavoriteIdentity(ve));

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

    useEffect(() => {
        if (!showAddPicker) return;
        const handlePointerDown = (event: MouseEvent) => {
            if (!rootRef.current?.contains(event.target as Node)) {
                setShowAddPicker(false);
            }
        };
        document.addEventListener('mousedown', handlePointerDown);
        return () => document.removeEventListener('mousedown', handlePointerDown);
    }, [showAddPicker]);

    const handleKeyDown = (event: React.KeyboardEvent) => {
        if (event.key === 'Escape') {
            setShowAddPicker(false);
        }
    };

    return (
        <div ref={rootRef} className="favorite-employee-settings" data-testid="fav-employee-settings" onKeyDown={handleKeyDown}>
            <h3 className="favorite-employee-settings__title">{isZh ? '常用数字员工' : 'Favorite Employees'}</h3>
            <p className="favorite-employee-settings__hint">
                {isZh ? '最多设置 6 个常用数字员工，显示在左侧导航栏中。拖动调整顺序。' : 'Pin up to 6 digital employees to the sidebar. Drag to reorder.'}
            </p>

            {/* Current favorites list */}
            {favoriteVEs.length === 0 ? (
                <div className="favorite-employee-settings__empty">
                    {isZh ? '暂无常用数字员工' : 'No favorites set'}
                </div>
            ) : (
                <div className="favorite-employee-settings__grid" role="list">
                    {favoriteVEs.map((fav, index) => (
                        <div
                            key={fav.id}
                            className="favorite-employee-settings__item"
                            data-drag-over={dragOverIndex === index ? 'true' : undefined}
                            draggable
                            role="listitem"
                            onDragStart={handleDragStart(index)}
                            onDragOver={handleDragOver(index)}
                            onDrop={handleDrop(index)}
                            onDragEnd={() => { setDragOverIndex(null); dragSourceIndex.current = null; }}
                        >
                            <span className="favorite-employee-settings__index">{index + 1}.</span>
                            {fav.avatarDataURL ? (
                                <img className="favorite-employee-settings__avatar" src={fav.avatarDataURL} alt="" />
                            ) : (
                                <span className="favorite-employee-settings__status" data-online={fav.online ? 'true' : 'false'} aria-hidden="true" />
                            )}
                            <span className="favorite-employee-settings__sr-only">{fav.online ? onlineLabel : offlineLabel}</span>
                            <span className="favorite-employee-settings__name" title={fav.name}>{fav.name}</span>
                            <button
                                className="favorite-employee-settings__remove"
                                type="button"
                                aria-label={isZh ? `移除常用数字员工：${fav.name}` : `Remove favorite employee: ${fav.name}`}
                                onClick={() => onRemove(fav.id)}
                            >
                                {isZh ? '移除' : 'Remove'}
                            </button>
                        </div>
                    ))}
                </div>
            )}

            {/* Add button */}
            {favoriteVEs.length < MAX_FAVORITE_EMPLOYEES && (
                <div className="favorite-employee-settings__add-wrap">
                    <button
                        className="favorite-employee-settings__add"
                        type="button"
                        aria-expanded={showAddPicker}
                        aria-haspopup="true"
                        onClick={() => setShowAddPicker(!showAddPicker)}
                    >
                        + {isZh ? '添加常用' : 'Add Favorite'}
                    </button>

                    {showAddPicker && (
                        <div className="favorite-employee-settings__picker" aria-label={isZh ? '可添加的数字员工' : 'Available employees'}>
                            {availableVEs.length === 0 ? (
                                <div className="favorite-employee-settings__picker-empty">
                                    {isZh ? '无可添加的数字员工' : 'No employees available'}
                                </div>
                            ) : (
                                availableVEs.map(ve => {
                                    const avatarDataURL = safeAvatarDataURL(ve.avatar_data_url);
                                    return (
                                        <button
                                            key={ve.id}
                                            className="favorite-employee-settings__picker-item"
                                            type="button"
                                            aria-label={ve.name}
                                            onClick={() => { onAdd(ve.machine_id || ve.id); setShowAddPicker(false); }}
                                        >
                                            {avatarDataURL ? (
                                                <img className="favorite-employee-settings__avatar favorite-employee-settings__avatar--picker" src={avatarDataURL} alt="" />
                                            ) : (
                                                <span className="favorite-employee-settings__picker-status" data-online={isVirtualEmployeeOnline(ve) ? 'true' : 'false'} aria-hidden="true" />
                                            )}
                                            <span className="favorite-employee-settings__sr-only">{isVirtualEmployeeOnline(ve) ? onlineLabel : offlineLabel}</span>
                                            <span className="favorite-employee-settings__picker-name" title={ve.name}>{ve.name}</span>
                                        </button>
                                    );
                                })
                            )}
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}
