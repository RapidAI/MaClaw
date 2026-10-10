import { useCallback, useEffect, useId, useRef, useState, type KeyboardEvent } from 'react';
import { InputHistoryAutocomplete } from '../ai/InputHistoryAutocomplete';
import { useAssistantInputHistory } from '../ai/useAssistantInputHistory';
import { useInputHistoryAutocomplete } from '../ai/useInputHistoryAutocomplete';
import { useTextCompositionGuard } from '../ai/useTextCompositionGuard';
import { lightTheme } from '../ai/aiAssistantPanelTheme';
import { botInputHistory, rememberBotInput } from './desktopBotInputHistory';

interface DesktopBotComposerProps {
    userId: string;
    botId: string;
    lang: string;
    value: string;
    onChange: (next: string) => void;
    onSubmit: () => void;
    disabled?: boolean;
    placeholder: string;
    ariaLabel: string;
    sendLabel: string;
}

// The popup borrows the assistant list. These colors follow the bot surface
// tokens so the list does not pick up the assistant panel's own theme.
const historyTheme = {
    ...lightTheme,
    bg: 'var(--theme-surface, #fff)',
    text: 'var(--theme-text, #1d2939)',
    textMuted: 'var(--theme-text-muted, #98a2b3)',
    inputBarBg: 'var(--theme-surface, #fff)',
    inputBarBorder: 'var(--theme-border, #e6e6e8)',
    fieldBorder: 'var(--theme-border, #e6e6e8)',
    btnColor: 'var(--mc-accent, #2f6fbc)',
};

export function DesktopBotComposer({
    userId,
    botId,
    lang,
    value,
    onChange,
    onSubmit,
    disabled = false,
    placeholder,
    ariaLabel,
    sendLabel,
}: DesktopBotComposerProps) {
    const inputRef = useRef<HTMLTextAreaElement>(null);
    const textComposition = useTextCompositionGuard();
    const [isComposing, setIsComposing] = useState(false);
    const [history, setHistory] = useState<string[]>(() => botInputHistory(userId));
    const seenScope = useRef({ userId, botId });
    const exitHistoryRef = useRef<() => boolean>(() => false);
    const listboxDomId = useId();
    const historyListboxId = `desktop-bot-input-history-${listboxDomId}`;

    const applyInputValue = useCallback((next: string) => {
        onChange(next);
        requestAnimationFrame(() => {
            const el = inputRef.current;
            if (!el) return;
            el.focus();
            el.setSelectionRange(next.length, next.length);
        });
    }, [onChange]);

    const { exitHistoryBrowsing, isSelectionCollapsedAtBoundary, recallHistory, rememberHistoryEdit, resetHistoryBrowsing } = useAssistantInputHistory({
        applyInputValue,
        inputRef,
        inputValue: value,
        submittedPrompts: history,
    });

    exitHistoryRef.current = exitHistoryBrowsing;
    // Mount already read this account. A later account loads its own cache.
    // Switching bots leaves the ↑↓ walk and puts back the draft from before it,
    // so the recalled line is not stuck in the other bot's box.
    useEffect(() => {
        const prev = seenScope.current;
        const userChanged = prev.userId !== userId;
        const botChanged = prev.botId !== botId;
        seenScope.current = { userId, botId };
        if (!userChanged && !botChanged) return;
        if (userChanged) setHistory(botInputHistory(userId));
        if (!exitHistoryRef.current()) resetHistoryBrowsing();
    }, [botId, resetHistoryBrowsing, userId]);

    const applyAutocompleteValue = useCallback((next: string) => {
        rememberHistoryEdit(next);
        onChange(next);
    }, [onChange, rememberHistoryEdit]);

    const autocomplete = useInputHistoryAutocomplete({
        inputValue: value,
        submittedPrompts: history,
        applyInputValue: applyAutocompleteValue,
        inputRef,
        disabled: isComposing,
    });

    const submitTyped = () => {
        const text = value.trim();
        if (!text || disabled) return;
        setHistory(rememberBotInput(userId, text));
        resetHistoryBrowsing();
        onSubmit();
    };

    const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
        if (textComposition.shouldIgnoreKeyDown(event)) return;
        if (autocomplete.handleKeyDown(event)) return;
        if (event.key === 'ArrowUp' && isSelectionCollapsedAtBoundary('up')) {
            if (recallHistory('up')) {
                event.preventDefault();
                return;
            }
        }
        if (event.key === 'ArrowDown' && isSelectionCollapsedAtBoundary('down')) {
            if (recallHistory('down')) {
                event.preventDefault();
                return;
            }
        }
        if (event.key === 'Escape') {
            if (exitHistoryBrowsing()) event.preventDefault();
            return;
        }
        // Shift+Enter stays a newline. Any other Enter sends, including Ctrl+Enter.
        if (event.key === 'Enter' && !event.shiftKey) {
            event.preventDefault();
            submitTyped();
        }
    };

    return (
        <form
            className="desktop-bot-chat__form"
            onSubmit={event => {
                event.preventDefault();
                submitTyped();
            }}
        >
            <InputHistoryAutocomplete
                open={autocomplete.open}
                matches={autocomplete.matches}
                selectedIndex={autocomplete.selectedIndex}
                prefix={value}
                listboxId={historyListboxId}
                onSelectIndex={autocomplete.setSelectedIndex}
                onAccept={autocomplete.accept}
                theme={historyTheme}
                lang={lang}
                testId="desktop-bot-input-history"
                itemTestIdPrefix="desktop-bot-input-history-item"
            />
            <textarea
                ref={inputRef}
                data-testid="desktop-bot-command"
                aria-label={ariaLabel}
                aria-autocomplete="list"
                aria-expanded={autocomplete.open}
                aria-controls={autocomplete.open ? historyListboxId : undefined}
                aria-activedescendant={autocomplete.open ? `${historyListboxId}-option-${autocomplete.selectedIndex}` : undefined}
                placeholder={placeholder}
                value={value}
                onChange={event => {
                    rememberHistoryEdit(event.target.value);
                    onChange(event.target.value);
                }}
                onCompositionStart={() => {
                    setIsComposing(true);
                    textComposition.onCompositionStart();
                }}
                onCompositionEnd={() => {
                    setIsComposing(false);
                    textComposition.onCompositionEnd();
                }}
                onKeyDown={onKeyDown}
            />
            <button type="submit" className="desktop-bot-chat__send" data-testid="desktop-bot-send" disabled={!value.trim() || disabled}>{sendLabel}</button>
        </form>
    );
}
