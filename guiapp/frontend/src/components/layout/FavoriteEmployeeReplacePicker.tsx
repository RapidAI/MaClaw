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
        <div className="ferp-overlay">
            <div
                ref={ref}
                data-testid="fav-replace-picker"
                className="ferp-dialog"
            >
                <div className="ferp-title">
                    {isZh ? '常用已满，选择要替换的位置' : 'Favorites full — pick a slot to replace'}
                </div>
                <div className="ferp-hint">
                    {isZh ? `将「${newVeName}」替换到：` : `Replace with "${newVeName}":`}
                </div>
                {currentSlots.map((slot, index) => (
                    <button
                        key={slot.veId}
                        type="button"
                        data-testid={`replace-slot-${index}`}
                        aria-label={isZh ? `替换第 ${index + 1} 个常用数字员工：${slot.name}` : `Replace favorite slot ${index + 1}: ${slot.name}`}
                        onClick={() => onReplace(index)}
                        className="ferp-slot-btn"
                        onMouseEnter={e => { (e.currentTarget as HTMLElement).style.background = 'var(--theme-hover, rgba(0,0,0,0.05))'; }}
                        onMouseLeave={e => { (e.currentTarget as HTMLElement).style.background = ''; }}
                    >
                        <span className="ferp-slot-index">
                            {index + 1}
                        </span>
                        <span className="ferp-slot-name">
                            {slot.name}
                        </span>
                        <span className="ferp-slot-action">
                            {isZh ? '点击替换' : 'click to replace'}
                        </span>
                    </button>
                ))}
                <div className="ferp-footer">
                    <button
                        type="button"
                        onClick={onCancel}
                        className="ferp-cancel-btn"
                    >
                        {isZh ? '取消' : 'Cancel'}
                    </button>
                </div>
            </div>
        </div>
    );
}
