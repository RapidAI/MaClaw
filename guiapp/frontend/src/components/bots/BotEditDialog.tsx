import { useEffect, useId, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';
import { BotAvatar } from './BotAvatar';
import { BOT_COLORS, BOT_SHAPES, type BotAppearance, type BotShape } from './botAppearance';

export type BotEditDialogLabels = {
    title: string;
    name: string;
    description: string;
    save: string;
    cancel: string;
    close: string;
    color?: string;
    shape?: string;
    /** Shown when another bot already uses the name. */
    nameTaken?: string;
};

type BotEditDialogProps = {
    initialTitle: string;
    initialDescription: string;
    labels: BotEditDialogLabels;
    /** Save failure shown inside the dialog; the dialog stays open for another try. */
    notice: string;
    saving: boolean;
    onClose: () => void;
    onSubmit: (title: string, description: string, look: BotAppearance) => void;
    initialAppearance?: BotAppearance;
    /** Returns true when the name is already used by another bot. */
    isNameTaken?: (title: string) => boolean;
    testId?: string;
};

// Same caps as the rename inputs this dialog replaces.
const TITLE_MAX = 40;
const DESCRIPTION_MAX = 80;

/**
 * Modal editor for a bot's name and description. Rendered inside #App, so the
 * custom-dialog classes pick up the theme variables directly.
 */
export function BotEditDialog({ initialTitle, initialDescription, labels, notice, saving, onClose, onSubmit, initialAppearance, isNameTaken, testId }: BotEditDialogProps) {
    const titleId = useId();
    const nameId = useId();
    const descriptionId = useId();
    const [title, setTitle] = useState(initialTitle);
    const [description, setDescription] = useState(initialDescription);
    const titleRef = useRef<HTMLInputElement | null>(null);
    const dialogRef = useRef<HTMLDivElement | null>(null);
    const previousFocusRef = useRef<HTMLElement | null>(null);
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose, { enabled: !saving });
    const [color, setColor] = useState(initialAppearance?.color || BOT_COLORS[0]);
    const [shape, setShape] = useState<BotShape>(initialAppearance?.shape || 'circle');
    const taken = !!isNameTaken && title.trim() !== '' && isNameTaken(title.trim());
    const canSave = title.trim() !== '' && !taken && !saving;

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
        onSubmit(title.trim(), description.trim(), { color, shape });
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
                data-testid={testId || "bot-edit-dialog"}
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
                    {taken ? <p className="bot-edit-dialog__notice" role="alert" data-testid="bot-edit-name-taken">{labels.nameTaken || 'Name already used'}</p> : null}
                    <div className="bot-edit-dialog__field bot-edit-dialog__look">
                        <BotAvatar color={color} shape={shape} className="bot-edit-dialog__preview" testId="bot-edit-preview" />
                    </div>
                    <div className="bot-edit-dialog__field" role="radiogroup" aria-label={labels.color || 'Color'}>
                        <span className="form-label bot-edit-dialog__label">{labels.color || 'Color'}</span>
                        <div className="bot-edit-dialog__swatches">
                            {BOT_COLORS.map(c => (
                                <button
                                    key={c}
                                    type="button"
                                    role="radio"
                                    aria-checked={c === color}
                                    aria-label={c}
                                    data-testid={'bot-edit-color-' + c.slice(1)}
                                    className={'bot-edit-dialog__swatch' + (c === color ? ' is-selected' : '')}
                                    style={{ background: c }}
                                    disabled={saving}
                                    onClick={() => setColor(c)}
                                />
                            ))}
                        </div>
                    </div>
                    <div className="bot-edit-dialog__field" role="radiogroup" aria-label={labels.shape || 'Shape'}>
                        <span className="form-label bot-edit-dialog__label">{labels.shape || 'Shape'}</span>
                        <div className="bot-edit-dialog__shapes">
                            {BOT_SHAPES.map(sh => (
                                <button
                                    key={sh}
                                    type="button"
                                    role="radio"
                                    aria-checked={sh === shape}
                                    aria-label={sh}
                                    data-testid={'bot-edit-shape-' + sh}
                                    className={'bot-edit-dialog__shape' + (sh === shape ? ' is-selected' : '')}
                                    disabled={saving}
                                    onClick={() => setShape(sh)}
                                >
                                    <BotAvatar color={color} shape={sh} />
                                </button>
                            ))}
                        </div>
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
