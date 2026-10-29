import { useEffect, useRef } from 'react';
import { tailText } from '../../i18n/mnTail'

interface FavoriteEmployeeReplacePickerProps {
    currentSlots: { veId: string; name: string }[];
    newVeName: string;
    onReplace: (index: number) => void;
    onCancel: () => void;
    lang?: string;
}

export function FavoriteEmployeeReplacePicker({ currentSlots, newVeName, onReplace, onCancel, lang }: FavoriteEmployeeReplacePickerProps) {
    const ref = useRef<HTMLDivElement>(null);

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
                    {tailText(lang ?? "", 'Favorites full — pick a slot to replace', '常用已满，选择要替换的位置')}
                </div>
                <div className="ferp-hint">
                    {tailText(lang ?? "", `Replace with "${newVeName}":`, `将「${newVeName}」替换到：`)}
                </div>
                {currentSlots.map((slot, index) => (
                    <button
                        key={slot.veId}
                        type="button"
                        data-testid={`replace-slot-${index}`}
                        aria-label={tailText(lang ?? "", `Replace favorite slot ${index + 1}: ${slot.name}`, `替换第 ${index + 1} 个常用数字员工：${slot.name}`)}
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
                            {tailText(lang ?? "", 'click to replace', '点击替换')}
                        </span>
                    </button>
                ))}
                <div className="ferp-footer">
                    <button
                        type="button"
                        onClick={onCancel}
                        className="ferp-cancel-btn"
                    >
                        {tailText(lang ?? "", 'Cancel', '取消')}
                    </button>
                </div>
            </div>
        </div>
    );
}
