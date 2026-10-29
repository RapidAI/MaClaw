import { useState } from 'react';
import { localizeText } from '../../i18n';

// One-tap corrections under an acceptance line, plus a last "Other" chip that
// opens a one-line input. Nothing here holds the run: the first option is
// already underway, and every choice only redirects it.
export function DesktopBotOptionChips({ options, lang, onPick, onOther }: {
    options: string[];
    lang: string;
    onPick: (option: string) => void;
    onOther: (text: string) => void;
}) {
    const [open, setOpen] = useState(false);
    const [draft, setDraft] = useState('');
    const submit = () => {
        const text = draft.trim();
        if (!text) return;
        setOpen(false);
        setDraft('');
        onOther(text);
    };
    return (
        <div className="desktop-bot-chat__options-wrap">
            <div className="desktop-bot-chat__options" data-testid="desktop-bot-options">
                {options.map((option, index) => (
                    <button
                        type="button"
                        key={option}
                        className={`desktop-bot-chat__action desktop-bot-chat__option${index === 0 ? ' is-default' : ''}`}
                        data-testid="desktop-bot-option"
                        disabled={index === 0}
                        onClick={() => onPick(option)}
                    >
                        {option}
                    </button>
                ))}
                <button
                    type="button"
                    className={`desktop-bot-chat__action desktop-bot-chat__option${open ? ' is-open' : ''}`}
                    data-testid="desktop-bot-option-other"
                    onClick={() => setOpen(value => !value)}
                >
                    {localizeText(lang, 'Other', '其它')}
                </button>
            </div>
            {open ? (
                <div className="desktop-bot-chat__other" data-testid="desktop-bot-other">
                    <input
                        autoFocus
                        className="desktop-bot-chat__other-input"
                        data-testid="desktop-bot-other-input"
                        value={draft}
                        placeholder={localizeText(lang, 'Tell me what you meant', '写下你想要的')}
                        onChange={event => setDraft(event.target.value)}
                        onKeyDown={event => {
                            if (event.key === 'Enter' && !event.nativeEvent.isComposing) {
                                event.preventDefault();
                                submit();
                            } else if (event.key === 'Escape') {
                                event.preventDefault();
                                setOpen(false);
                                setDraft('');
                            }
                        }}
                    />
                    <button
                        type="button"
                        className="desktop-bot-chat__action"
                        data-testid="desktop-bot-other-send"
                        disabled={!draft.trim()}
                        onClick={submit}
                    >
                        {localizeText(lang, 'Send', '发送')}
                    </button>
                </div>
            ) : null}
        </div>
    );
}
