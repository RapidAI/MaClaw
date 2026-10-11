import { useEffect, useId, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';

export type BotEditDialogLabels = {
    title: string;
    name: string;
    description: string;
    save: string;
    cancel: string;
    close: string;
};

type BotEditDialogProps = {
    initialTitle: string;
    initialDescription: string;
    labels: BotEditDialogLabels;
    /** Save failure shown inside the dialog; the dialog stays open for another try. */
    notice: string;
    saving: boolean;
    onClose: () => void;
    onSubmit: (title: string, description: string) => void;
};

// Same caps as the rename inputs this dialog replaces.
const TITLE_MAX = 40;
const DESCRIPTION_MAX = 80;

/**
 * Modal editor for a bot's name and description. Rendered inside #App, so the
 * custom-dialog classes pick up the theme variables directly.
 */
export function BotEditDialog({ initialTitle, initialDescription, labels, notice, saving, onClose, onSubmit }: BotEditDialogProps) {
    const titleId = useId();
    const nameId = useId();
    const descriptionId = useId();
    const [title, setTitle] = useState(initialTitle);
    const [description, setDescription] = useState(initialDescription);
    const titleRef = useRef<HTMLInputElement | null>(null);
    const dialogRef = useRef<HTMLDivElement | null>(null);
    const previousFocusRef = useRef<HTMLElement | null>(null);
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose, { enabled: !saving });
    const canSave = title.trim() !== '' && !saving;

    useEffect(() => {
        previousFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        const focusTimer = window.setTimeout(() => {
            titleRef.current?.focus();
            titleRef.current?.select();
        }, 0);
        return () => {
            window.clearTimeout(focusTimer);
            const previousFocus = previousFocusRef.current;
            previousFocusRef.current = null;
            if (previousFocus?.isConnected) previousFocus.focus({ preventScroll: true });
        };
    }, []);

    // Keep keyboard focus inside the modal while it is open, as CustomDialog does.
    useEffect(() => {
        const handleFocusIn = (event: FocusEvent) => {
            if (event.target instanceof Node && !dialogRef.current?.contains(event.target)) {
                titleRef.current?.focus();
            }
        };
        document.addEventListener('focusin', handleFocusIn, true);
        return () => document.removeEventListener('focusin', handleFocusIn, true);
    }, []);

    const submit = () => {
        if (!canSave) return;
        onSubmit(title.trim(), description.trim());
    };

    const onKeyDown = (event: ReactKeyboardEvent) => {
        if (event.key === 'Escape') {
            event.preventDefault();
            event.stopPropagation();
            if (!saving) onClose();
            return;
        }
        if (event.key === 'Enter') {
            // IME composition and a focused button own Enter.
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
                'button:not([disabled]), input:not([disabled]), textarea:not([disabled]), [href]',
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
        <div className="modal-backdrop custom-dialog-backdrop bot-edit-dialog-backdrop" {...backdropProps} onKeyDown={onKeyDown}>
            <div
                className="modal-content custom-dialog bot-edit-dialog"
                ref={dialogRef}
                role="dialog"
                aria-modal="true"
                aria-labelledby={titleId}
                data-testid="bot-edit-dialog"
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
                    <div className="bot-edit-dialog__field">
                        <label className="form-label bot-edit-dialog__label" htmlFor={nameId}>{labels.name}</label>
                        <input
                            id={nameId}
                            ref={titleRef}
                            className="form-input bot-edit-dialog__input"
                            type="text"
                            value={title}
                            maxLength={TITLE_MAX}
                            onChange={event => setTitle(event.target.value)}
                            autoComplete="off"
                            spellCheck={false}
                            disabled={saving}
                        />
                    </div>
                    <div className="bot-edit-dialog__field">
                        <label className="form-label bot-edit-dialog__label" htmlFor={descriptionId}>{labels.description}</label>
                        <input
                            id={descriptionId}
                            className="form-input bot-edit-dialog__input"
                            type="text"
                            value={description}
                            maxLength={DESCRIPTION_MAX}
                            onChange={event => setDescription(event.target.value)}
                            autoComplete="off"
                            spellCheck={false}
                            disabled={saving}
                        />
                    </div>
                    {notice ? <p className="bot-edit-dialog__notice" role="status">{notice}</p> : null}
                </div>
                <div className="modal-footer custom-dialog__footer">
                    <button type="button" className="btn-secondary" onClick={onClose} disabled={saving}>{labels.cancel}</button>
                    <button type="button" className="btn-primary" data-testid="bot-edit-save" onClick={submit} disabled={!canSave}>{labels.save}</button>
                </div>
            </div>
        </div>
    );
}
