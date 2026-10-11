import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';

// One entry the owner can pick for their bots. Key is carried here because the
// bot needs it to call the provider; it never reaches the dialog's markup, only
// the save payload.
export type BotLLMProviderOption = {
    name: string;
    url: string;
    key: string;
    model: string;
    protocol: string;
    wireAPI: string;
};

export type BotLLMSettingsSubmit = {
    mode: 'default' | 'custom';
    provider?: BotLLMProviderOption;
    model: string;
};

export type BotLLMSettingsDialogLabels = {
    title: string;
    sharedHint: string;
    useDefault: string;
    defaultHint: string;
    useProvider: string;
    providerLabel: string;
    modelLabel: string;
    modelPlaceholder: string;
    noProviders: string;
    modelRequired: string;
    save: string;
    cancel: string;
    close: string;
};

type BotLLMSettingsDialogProps = {
    labels: BotLLMSettingsDialogLabels;
    providers: BotLLMProviderOption[];
    initialMode: 'default' | 'custom';
    initialName: string;
    initialModel: string;
    /** Save failure shown inside the dialog; the dialog stays open for another try. */
    notice: string;
    saving: boolean;
    onClose: () => void;
    onSubmit: (payload: BotLLMSettingsSubmit) => void;
};

/**
 * Chooses the model this user's bots run on. "Default" is not one fixed value:
 * it hands the bots back to whatever the service resolves for this user, either
 * their own Hub route or the provider an administrator picked. That is why the
 * dialog does not show what the default currently is — it changes underneath,
 * and showing a stale reading would misdescribe what the user is returning to.
 */
export function BotLLMSettingsDialog({
    labels,
    providers,
    initialMode,
    initialName,
    initialModel,
    notice,
    saving,
    onClose,
    onSubmit,
}: BotLLMSettingsDialogProps) {
    const titleId = useId();
    const providerId = useId();
    const modelId = useId();
    const [mode, setMode] = useState<'default' | 'custom'>(initialMode);
    const usable = useMemo(() => providers.filter(item => item.key.trim() !== ''), [providers]);
    const [name, setName] = useState(() => {
        if (initialName.trim() !== '') return initialName;
        return usable[0]?.name ?? '';
    });
    const [model, setModel] = useState(() => initialModel.trim());
    const dialogRef = useRef<HTMLDivElement | null>(null);
    const previousFocusRef = useRef<HTMLElement | null>(null);
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose, { enabled: !saving });

    useEffect(() => {
        previousFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        return () => {
            const previousFocus = previousFocusRef.current;
            previousFocusRef.current = null;
            if (previousFocus?.isConnected) previousFocus.focus({ preventScroll: true });
        };
    }, []);

    // Keep keyboard focus inside the modal while it is open, as BotEditDialog does.
    useEffect(() => {
        const handleFocusIn = (event: FocusEvent) => {
            if (event.target instanceof Node && !dialogRef.current?.contains(event.target)) {
                dialogRef.current?.focus();
            }
        };
        document.addEventListener('focusin', handleFocusIn, true);
        return () => document.removeEventListener('focusin', handleFocusIn, true);
    }, []);

    // Falling back to the first usable entry keeps the select's value inside its
    // own option list: a saved provider can lose its key after the fact.
    const selected = usable.find(item => item.name === name) ?? usable[0];
    const canSave = !saving && (mode === 'default' || (selected != null && model.trim() !== ''));

    const pickProvider = (nextName: string) => {
        setName(nextName);
        const next = providers.find(item => item.name === nextName);
        // Fill from the provider so the common case needs no typing. A model the
        // user already typed for a different provider is not carried over.
        setModel(next?.model.trim() ?? '');
    };

    // Turning on "pick a provider" seeds the model from the provider too, so the
    // option is never half-open: it either shows a usable model or says it needs
    // one, rather than waiting for a click nobody knows to make.
    const chooseMode = (next: 'default' | 'custom') => {
        setMode(next);
        if (next === 'custom' && model.trim() === '') setModel(selected?.model.trim() ?? '');
    };

    const submit = () => {
        if (!canSave) return;
        if (mode === 'default') {
            onSubmit({ mode: 'default', model: '' });
            return;
        }
        if (!selected) return;
        onSubmit({ mode: 'custom', provider: selected, model: model.trim() });
    };

    const onKeyDown = (event: ReactKeyboardEvent) => {
        if (event.key === 'Escape') {
            event.preventDefault();
            event.stopPropagation();
            if (!saving) onClose();
            return;
        }
        if (event.key === 'Enter') {
            const native = event.nativeEvent as KeyboardEvent;
            if (native.isComposing || native.keyCode === 229) return;
            if (event.target instanceof HTMLButtonElement) return;
            event.preventDefault();
            event.stopPropagation();
            submit();
            return;
        }
        if (event.key === 'Tab') {
            const focusable = dialogRef.current?.querySelectorAll<HTMLElement>(
                'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [href]',
            );
            if (!focusable?.length) return;
            const items = Array.from(focusable);
            const currentIndex = items.indexOf(document.activeElement as HTMLElement);
            const nextIndex = event.shiftKey
                ? (currentIndex <= 0 ? items.length - 1 : currentIndex - 1)
                : (currentIndex === items.length - 1 ? 0 : currentIndex + 1);
            event.preventDefault();
            items[nextIndex].focus();
        }
    };

    return (
        <div className="modal-backdrop custom-dialog-backdrop bot-llm-dialog-backdrop" {...backdropProps} onKeyDown={onKeyDown}>
            <div
                className="modal-content custom-dialog bot-llm-dialog"
                ref={dialogRef}
                role="dialog"
                aria-modal="true"
                aria-labelledby={titleId}
                data-testid="bot-llm-dialog"
                tabIndex={-1}
                {...dialogProps}
            >
                <div className="modal-header custom-dialog__header">
                    <div className="custom-dialog__heading">
                        <h3 id={titleId} className="custom-dialog__title">{labels.title}</h3>
                    </div>
                    <button type="button" className="btn-close custom-dialog__close" aria-label={labels.close} disabled={saving} onClick={onClose}>
                        <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true">
                            <path d="M3.2 3.2 10.8 10.8M10.8 3.2 3.2 10.8" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
                        </svg>
                    </button>
                </div>
                <div className="modal-body custom-dialog__body">
                    <p className="bot-llm-dialog__hint">{labels.sharedHint}</p>

                    <div className="bot-llm-dialog__option">
                        <label className="bot-llm-dialog__radio">
                            <input
                                type="radio"
                                name="bot-llm-mode"
                                value="default"
                                checked={mode === 'default'}
                                disabled={saving}
                                onChange={() => chooseMode('default')}
                                data-testid="bot-llm-mode-default"
                            />
                            <span>{labels.useDefault}</span>
                        </label>
                        <p className="bot-llm-dialog__subhint">{labels.defaultHint}</p>
                    </div>

                    <div className="bot-llm-dialog__option">
                        <label className="bot-llm-dialog__radio">
                            <input
                                type="radio"
                                name="bot-llm-mode"
                                value="custom"
                                checked={mode === 'custom'}
                                disabled={saving || usable.length === 0}
                                onChange={() => chooseMode('custom')}
                                data-testid="bot-llm-mode-custom"
                            />
                            <span>{labels.useProvider}</span>
                        </label>

                        {usable.length === 0 ? <p className="bot-llm-dialog__subhint" role="status">{labels.noProviders}</p> : null}

                        {usable.length > 0 ? (
                            <div className="bot-llm-dialog__field">
                                <label className="form-label bot-llm-dialog__label" htmlFor={providerId}>{labels.providerLabel}</label>
                                <select
                                    id={providerId}
                                    className="form-input bot-llm-dialog__select"
                                    value={selected?.name ?? ''}
                                    disabled={saving || mode !== 'custom'}
                                    data-testid="bot-llm-provider"
                                    onChange={event => pickProvider(event.target.value)}
                                >
                                    {usable.map(item => (
                                        <option key={item.name} value={item.name}>{item.name}</option>
                                    ))}
                                </select>
                            </div>
                        ) : null}

                        {usable.length > 0 ? (
                            <div className="bot-llm-dialog__field">
                                <label className="form-label bot-llm-dialog__label" htmlFor={modelId}>{labels.modelLabel}</label>
                                <input
                                    id={modelId}
                                    className="form-input bot-llm-dialog__input"
                                    type="text"
                                    value={model}
                                    placeholder={labels.modelPlaceholder}
                                    disabled={saving || mode !== 'custom'}
                                    data-testid="bot-llm-model"
                                    onChange={event => setModel(event.target.value)}
                                    autoComplete="off"
                                    spellCheck={false}
                                />
                                {selected && model.trim() === '' ? <p className="bot-llm-dialog__subhint" role="status">{labels.modelRequired}</p> : null}
                            </div>
                        ) : null}
                    </div>

                    {notice ? <p className="bot-llm-dialog__notice" role="status">{notice}</p> : null}
                </div>
                <div className="modal-footer custom-dialog__footer">
                    <button type="button" className="btn-secondary" onClick={onClose} disabled={saving}>{labels.cancel}</button>
                    <button type="button" className="btn-primary" data-testid="bot-llm-save" onClick={submit} disabled={!canSave}>{labels.save}</button>
                </div>
            </div>
        </div>
    );
}
