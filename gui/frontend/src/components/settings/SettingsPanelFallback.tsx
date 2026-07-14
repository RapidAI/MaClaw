type SettingsPanelFallbackProps = {
    message: string;
    /** Optional secondary action (e.g. retry after a chunk load failure). */
    actionLabel?: string;
    onAction?: () => void;
};

/** Shared loading / error chrome for settings body panels. */
export function SettingsPanelFallback({ message, actionLabel, onAction }: SettingsPanelFallbackProps) {
    return (
        <div className="settings-content settings-panel settings-content--loading" role="status" aria-live="polite">
            <div className="settings-content-loading-inner">
                <span>{message}</span>
                {actionLabel && onAction && (
                    <button type="button" className="btn btn-sm settings-content-loading-retry" onClick={onAction}>
                        {actionLabel}
                    </button>
                )}
            </div>
        </div>
    );
}
