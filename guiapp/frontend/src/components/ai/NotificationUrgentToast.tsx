import { IconRecord } from "./WorkbenchIcons";
import { localizeText } from "./aiAssistantI18n";
import { stripMarkdownPreview } from "./NotificationPanel";
import type { AdminNotification } from "./useNotifications";

/**
 * The urgent notice that shows while the bell is closed. It is the only entry
 * point to its detail, so opening it must reset any error left by the notice
 * previously on screen.
 */
export function NotificationUrgentToast({
    notification,
    lang,
    onDismiss,
    onOpen,
}: {
    notification: AdminNotification;
    lang: string;
    onDismiss: () => void;
    onOpen: () => void;
}) {
    return (
        <div
            data-testid="notification-urgent-toast"
            data-notification-toast="true"
            role="status"
            aria-live="assertive"
            className="notification-toast"
        >
            <div className="notification-toast__head">
                <span className="notification-toast__label">
                    <IconRecord size={12} className="notification-toast__glyph" />
                    {localizeText(lang, "Urgent Notification", "紧急通知", "緊急通知")}
                </span>
                <button
                    onClick={onDismiss}
                    className="notification-toast__close"
                    aria-label={localizeText(lang, "Dismiss", "关闭", "關閉")}
                >
                    ×
                </button>
            </div>
            <button
                data-testid="notification-urgent-toast-open"
                onClick={onOpen}
                className="notification-toast__open"
            >
                <span className="notification-toast__title">{notification.title}</span>
                <span className="notification-toast__preview">
                    {stripMarkdownPreview(notification.content)}
                </span>
                <span className="notification-toast__cta">
                    {localizeText(lang, "View full notice", "查看全文", "查看全文")}
                </span>
            </button>
        </div>
    );
}
