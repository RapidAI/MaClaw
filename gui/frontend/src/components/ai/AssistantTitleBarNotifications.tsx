import { useCallback, useEffect, useMemo, useState } from "react";
import { localizeText } from "./aiAssistantI18n";
import type { Theme } from "./aiAssistantPanelTheme";
import { NotificationBell } from "./NotificationBell";
import { asNotificationCategory, NotificationPanel, stripMarkdownPreview } from "./NotificationPanel";
import { useNotifications, type AdminNotification } from "./useNotifications";
import { IconRecord } from "./WorkbenchIcons";

export function AssistantTitleBarNotifications({
    inline,
    lang,
    theme: t,
}: {
    inline: boolean;
    lang: string;
    theme: Theme;
}) {
    const {
        notifications,
        unreadCount,
        panelOpen,
        categoryFilter,
        togglePanel,
        setCategoryFilter,
        markRead,
        markAllRead,
        urgentToast,
        dismissUrgentToast,
    } = useNotifications();
    const [selectedNotification, setSelectedNotification] = useState<AdminNotification | null>(null);

    const handleSelectNotification = useCallback((notification: AdminNotification) => {
        if (!notification.is_read) markRead(notification.id);
        setSelectedNotification(notification);
    }, [markRead]);

    const handleClosePanel = useCallback(() => {
        setSelectedNotification(null);
        if (panelOpen) togglePanel();
    }, [panelOpen, togglePanel]);

    const handleBackFromDetail = useCallback(() => {
        setSelectedNotification(null);
    }, []);

    const handleOpenFromToast = useCallback(() => {
        if (!urgentToast) return;
        if (!urgentToast.is_read) markRead(urgentToast.id);
        setSelectedNotification(urgentToast);
        if (!panelOpen) togglePanel();
        dismissUrgentToast();
    }, [dismissUrgentToast, markRead, panelOpen, togglePanel, urgentToast]);

    const handleTogglePanel = useCallback(() => {
        if (panelOpen) setSelectedNotification(null);
        togglePanel();
    }, [panelOpen, togglePanel]);

    const resolvedSelected = selectedNotification
        ? notifications.find((item) => item.id === selectedNotification.id) ?? selectedNotification
        : null;

    useEffect(() => {
        if (!urgentToast || panelOpen) return;
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === "Escape") dismissUrgentToast();
        };
        document.addEventListener("keydown", onKeyDown);
        return () => document.removeEventListener("keydown", onKeyDown);
    }, [dismissUrgentToast, panelOpen, urgentToast]);

    const notificationPanelTheme = useMemo(() => ({
        bg: t.titleBarBg,
        text: t.text,
        textMuted: t.textMuted || t.promptColor,
        headingColor: t.headingColor,
        divider: t.titleBarBorder,
        inputBarBg: t.fieldBg,
        inputBarBorder: t.titleBarBorder,
    }), [t.fieldBg, t.headingColor, t.promptColor, t.text, t.textMuted, t.titleBarBg, t.titleBarBorder]);

    return (
        <>
            <div data-notification-anchor="true" style={{ position: "relative" }}>
                <NotificationBell
                    unreadCount={unreadCount}
                    onClick={handleTogglePanel}
                    theme={t}
                    inline={inline}
                    open={panelOpen}
                    lang={lang}
                />
            </div>
            {panelOpen && (
                <NotificationPanel
                    notifications={notifications}
                    categoryFilter={asNotificationCategory(categoryFilter)}
                    onCategoryChange={(category) => setCategoryFilter(category)}
                    onMarkAllRead={markAllRead}
                    onSelectNotification={handleSelectNotification}
                    onClose={handleClosePanel}
                    selectedNotification={resolvedSelected}
                    onBackFromDetail={handleBackFromDetail}
                    detailTheme={t}
                    lang={lang}
                    theme={notificationPanelTheme}
                />
            )}
            {urgentToast && !panelOpen && (
                <div
                    data-testid="notification-urgent-toast"
                    data-notification-toast="true"
                    role="status"
                    aria-live="assertive"
                    style={{
                        position: "fixed",
                        top: "48px",
                        right: "16px",
                        width: "320px",
                        padding: "12px 16px",
                        background: t.titleBarBg,
                        border: "1px solid #ef4444",
                        borderRadius: "8px",
                        boxShadow: "0 4px 24px rgba(239, 68, 68, 0.2)",
                        zIndex: 50000,
                        display: "flex",
                        flexDirection: "column",
                        gap: "6px",
                    }}
                >
                    <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
                        <span style={{ fontSize: "12px", fontWeight: 600, color: "#ef4444", display: "inline-flex", alignItems: "center", gap: 6 }}>
                            <IconRecord size={12} color="#ef4444" />
                            {localizeText(lang, "Urgent Notification", "\u7d27\u6025\u901a\u77e5", "\u7dca\u6025\u901a\u77e5")}
                        </span>
                        <button
                            onClick={dismissUrgentToast}
                            style={{ background: "none", border: "none", cursor: "pointer", color: t.promptColor, fontSize: "14px", padding: "0 2px", lineHeight: 1 }}
                            aria-label={localizeText(lang, "Dismiss", "\u5173\u95ed", "\u95dc\u9589")}
                        >
                            ×
                        </button>
                    </div>
                    <button
                        data-testid="notification-urgent-toast-open"
                        onClick={handleOpenFromToast}
                        style={{
                            background: "none",
                            border: "none",
                            padding: 0,
                            textAlign: "left",
                            cursor: "pointer",
                            display: "flex",
                            flexDirection: "column",
                            gap: "6px",
                        }}
                    >
                        <span style={{ fontSize: "13px", fontWeight: 500, color: t.text, lineHeight: 1.45, wordBreak: "break-word" }}>{urgentToast.title}</span>
                        <span style={{ fontSize: "12px", color: t.promptColor, lineHeight: 1.5, display: "-webkit-box", WebkitLineClamp: 3, WebkitBoxOrient: "vertical", overflow: "hidden", wordBreak: "break-word" }}>
                            {stripMarkdownPreview(urgentToast.content)}
                        </span>
                        <span style={{ fontSize: "11px", fontWeight: 600, color: t.linkColor || t.headingColor }}>
                            {localizeText(lang, "View full notice", "\u67e5\u770b\u5168\u6587", "\u67e5\u770b\u5168\u6587")}
                        </span>
                    </button>
                </div>
            )}
        </>
    );
}
