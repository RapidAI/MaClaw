import { useEffect, useRef } from 'react';

interface FavoriteEmployeeReplacePickerProps {
    currentSlots: { veId: string; name: string }[];
    newVeName: string;
    onReplace: (index: number) => void;
    onCancel: () => void;
    lang?: string;
}

export function FavoriteEmployeeReplacePicker({ currentSlots, newVeName, onReplace, onCancel, lang }: FavoriteEmployeeReplacePickerProps) {
    const ref = useRef<HTMLDivElement>(null);
    const isZh = !lang || lang.startsWith('zh');

    useEffect(() => {
        const handleClickOutside = (e: MouseEvent) => {
            if (ref.current && !ref.current.contains(e.target as Node)) {
                onCancel();
            }
        };
        const handleEscape = (e: KeyboardEvent) => {
            if (e.key === 'Escape') onCancel();
        };
        const timer = setTimeout(() => document.addEventListener('mousedown', handleClickOutside), 0);
        document.addEventListener('keydown', handleEscape);
        return () => {
            clearTimeout(timer);
            document.removeEventListener('mousedown', handleClickOutside);
            document.removeEventListener('keydown', handleEscape);
        };
    }, [onCancel]);

    return (
        <div style={{
            position: 'fixed',
            inset: 0,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            background: 'rgba(0,0,0,0.3)',
            zIndex: 99999,
        }}>
            <div
                ref={ref}
                data-testid="fav-replace-picker"
                style={{
                    background: 'var(--theme-surface)',
                    border: '1px solid var(--theme-border)',
                    borderRadius: '12px',
                    boxShadow: '0 8px 32px rgba(0,0,0,0.18)',
                    padding: '16px',
                    minWidth: '240px',
                    maxWidth: '320px',
                }}
            >
                <div style={{ fontSize: '13px', fontWeight: 600, marginBottom: '12px', color: 'var(--theme-text-primary)' }}>
                    {isZh ? '常用已满，选择要替换的位置' : 'Favorites full — pick a slot to replace'}
                </div>
                <div style={{ fontSize: '11px', color: 'var(--theme-text-muted)', marginBottom: '10px' }}>
                    {isZh ? `将「${newVeName}」替换到：` : `Replace with "${newVeName}":`}
                </div>
                {currentSlots.map((slot, index) => (
                    <div
                        key={slot.veId}
                        data-testid={`replace-slot-${index}`}
                        onClick={() => onReplace(index)}
                        style={{
                            display: 'flex',
                            alignItems: 'center',
                            gap: '8px',
                            padding: '8px 12px',
                            borderRadius: '8px',
                            cursor: 'pointer',
                            transition: 'background 0.12s',
                            marginBottom: '4px',
                        }}
                        onMouseEnter={e => { (e.currentTarget as HTMLElement).style.background = 'var(--theme-hover, rgba(0,0,0,0.05))'; }}
                        onMouseLeave={e => { (e.currentTarget as HTMLElement).style.background = ''; }}
                    >
                        <span style={{
                            width: '20px', height: '20px', borderRadius: '50%',
                            background: 'var(--theme-primary)', color: '#fff',
                            display: 'flex', alignItems: 'center', justifyContent: 'center',
                            fontSize: '10px', fontWeight: 700, flexShrink: 0,
                        }}>
                            {index + 1}
                        </span>
                        <span style={{ fontSize: '12px', color: 'var(--theme-text-primary)', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                            {slot.name}
                        </span>
                        <span style={{ fontSize: '10px', color: 'var(--theme-text-muted)' }}>
                            {isZh ? '点击替换' : 'click to replace'}
                        </span>
                    </div>
                ))}
                <div style={{ marginTop: '10px', textAlign: 'right' }}>
                    <button
                        onClick={onCancel}
                        style={{
                            fontSize: '11px', padding: '4px 12px', borderRadius: '6px',
                            border: '1px solid var(--theme-border)', background: 'transparent',
                            color: 'var(--theme-text-muted)', cursor: 'pointer',
                        }}
                    >
                        {isZh ? '取消' : 'Cancel'}
                    </button>
                </div>
            </div>
        </div>
    );
}
