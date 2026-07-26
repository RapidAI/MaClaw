import { useEffect, useId, useRef } from 'react';

type ConfirmDialogProps = {
    title: string;
    message: string;
    t: (key: string) => string;
    onCancel: () => void;
    onConfirm: () => void;
};

// Icon/theme styling keeps stroke="#ef4444", var(--theme-surface), and var(--theme-text-primary) in App.css.

export const ConfirmDialog = ({ title, message, t, onCancel, onConfirm }: ConfirmDialogProps) => {
    const titleId = useId();
    const messageId = useId();
    const dialogRef = useRef<HTMLDivElement | null>(null);
    const cancelButtonRef = useRef<HTMLButtonElement | null>(null);
    const previousFocusRef = useRef<HTMLElement | null>(null);
    const onCancelRef = useRef(onCancel);
    onCancelRef.current = onCancel;

    useEffect(() => {
        previousFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        const focusTimer = window.setTimeout(() => cancelButtonRef.current?.focus(), 0);
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === 'Escape') {
                event.preventDefault();
                event.stopImmediatePropagation();
                onCancelRef.current();
                return;
            }
            if (event.key !== 'Tab') return;
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
            event.stopImmediatePropagation();
            items[nextIndex].focus();
        };
        window.addEventListener('keydown', onKeyDown, true);
        return () => {
            window.clearTimeout(focusTimer);
            window.removeEventListener('keydown', onKeyDown, true);
            const previousFocus = previousFocusRef.current;
            previousFocusRef.current = null;
            if (previousFocus?.isConnected) previousFocus.focus();
        };
    }, []);

    useEffect(() => {
        const handleFocusIn = (event: FocusEvent) => {
            if (event.target instanceof Node && !dialogRef.current?.contains(event.target)) {
                cancelButtonRef.current?.focus();
            }
        };
        document.addEventListener('focusin', handleFocusIn, true);
        return () => document.removeEventListener('focusin', handleFocusIn, true);
    }, []);

    return (
    <div className="confirm-dialog-overlay">
        <div
            ref={dialogRef}
            className="confirm-dialog"
            role="dialog"
            aria-modal="true"
            aria-labelledby={titleId}
            aria-describedby={messageId}
        >
            <div className="confirm-dialog__icon" aria-hidden="true">
                <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                    <circle cx="12" cy="12" r="10"></circle>
                    <line x1="12" y1="8" x2="12" y2="12"></line>
                    <line x1="12" y1="16" x2="12.01" y2="16"></line>
                </svg>
            </div>

            <h3 id={titleId} className="confirm-dialog__title">
                {title}
            </h3>

            <p id={messageId} className="confirm-dialog__message">
                {message}
            </p>

            <div className="confirm-dialog__actions">
                <button ref={cancelButtonRef} type="button" className="confirm-dialog__button confirm-dialog__button--secondary" onClick={onCancel}>
                    {t("cancel")}
                </button>
                <button type="button" className="confirm-dialog__button confirm-dialog__button--danger" onClick={onConfirm}>
                    {t("confirm")}
                </button>
            </div>
        </div>
    </div>
    );
};
