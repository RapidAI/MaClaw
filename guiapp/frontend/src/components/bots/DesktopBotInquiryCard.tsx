import { useMemo, useState } from 'react';
import { localizeText } from '../../i18n';

/** Compact gray option-inquiry card under an assistant message (Grok-style). */
export function DesktopBotInquiryCard({
    question,
    options,
    allowMultiple,
    lang,
    onSubmit,
}: {
    question?: string;
    options: string[];
    allowMultiple?: boolean;
    lang: string;
    onSubmit: (reply: string) => void;
}) {
    const [picked, setPicked] = useState<number[]>([]);
    const labels = useMemo(
        () => options.map((opt, i) => ({ opt, key: String.fromCharCode(65 + (i % 26)), index: i })),
        [options],
    );

    const joinReply = (indexes: number[]) =>
        indexes
            .slice()
            .sort((a, b) => a - b)
            .map(i => options[i]?.trim() || '')
            .filter(Boolean)
            .join('；');

    const toggle = (index: number) => {
        if (!allowMultiple) {
            onSubmit(options[index] || '');
            return;
        }
        setPicked(prev => (prev.includes(index) ? prev.filter(i => i !== index) : [...prev, index]));
    };

    const submitMulti = () => {
        const reply = joinReply(picked);
        if (!reply) return;
        onSubmit(reply);
    };

    return (
        <div className="desktop-bot-chat__inquiry" data-testid="desktop-bot-inquiry">
            {question?.trim() ? (
                <div className="desktop-bot-chat__inquiry-prompt" data-testid="desktop-bot-inquiry-prompt">
                    {question.trim()}
                </div>
            ) : null}
            <div className="desktop-bot-chat__inquiry-options" role={allowMultiple ? 'group' : 'radiogroup'}>
                {labels.map(({ opt, key, index }) => {
                    const selected = picked.includes(index);
                    return (
                        <button
                            type="button"
                            key={`${key}:${opt}`}
                            className={`desktop-bot-chat__inquiry-option${selected ? ' is-selected' : ''}`}
                            data-testid="desktop-bot-inquiry-option"
                            aria-pressed={allowMultiple ? selected : undefined}
                            onClick={() => toggle(index)}
                        >
                            <span className="desktop-bot-chat__choice-key" aria-hidden="true">{key}</span>
                            <span>{opt}</span>
                            <span className="desktop-bot-chat__choice-mark" aria-hidden="true">
                                {allowMultiple ? (selected ? '☑' : '☐') : '✓'}
                            </span>
                        </button>
                    );
                })}
            </div>
            {allowMultiple ? (
                <div className="desktop-bot-chat__inquiry-actions">
                    <button
                        type="button"
                        className="desktop-bot-chat__inquiry-submit"
                        data-testid="desktop-bot-inquiry-submit"
                        disabled={picked.length === 0}
                        onClick={submitMulti}
                    >
                        {localizeText(lang, 'Confirm', '确认')}
                    </button>
                </div>
            ) : null}
        </div>
    );
}

export function askUserAllowsMultiple(inputType?: string): boolean {
    return String(inputType || '').trim().toLowerCase() === 'multi_choice';
}
