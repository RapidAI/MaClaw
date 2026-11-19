// A picture that is not in this process. The person can ask Hub for it again.
export function DesktopPictureMissing({ onRetry, label }: { onRetry: () => void; label: string }) {
    return (
        <button type="button" className="desktop-bot-chat__picture-missing" data-testid="desktop-picture-missing" onClick={onRetry}>
            {label}
        </button>
    );
}
