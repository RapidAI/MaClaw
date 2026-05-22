type ConfirmDialogProps = {
    title: string;
    message: string;
    t: (key: string) => string;
    onCancel: () => void;
    onConfirm: () => void;
};

// Icon/theme styling keeps stroke="#ef4444", var(--theme-surface), and var(--theme-text-primary) in App.css.

export const ConfirmDialog = ({ title, message, t, onCancel, onConfirm }: ConfirmDialogProps) => (
    <div className="confirm-dialog-overlay">
        <div className="confirm-dialog">
            <div className="confirm-dialog__icon" aria-hidden="true">
                <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                    <circle cx="12" cy="12" r="10"></circle>
                    <line x1="12" y1="8" x2="12" y2="12"></line>
                    <line x1="12" y1="16" x2="12.01" y2="16"></line>
                </svg>
            </div>

            <h3 className="confirm-dialog__title">
                {title}
            </h3>

            <p className="confirm-dialog__message">
                {message}
            </p>

            <div className="confirm-dialog__actions">
                <button className="confirm-dialog__button confirm-dialog__button--secondary" onClick={onCancel}>
                    {t("cancel")}
                </button>
                <button className="confirm-dialog__button confirm-dialog__button--danger" onClick={onConfirm}>
                    {t("confirm")}
                </button>
            </div>
        </div>
    </div>
);
