import { useCallback, useEffect, useMemo, useState } from "react";
import { localizeText } from "./aiAssistantI18n";
import type { Theme } from "./aiAssistantPanelTheme";
import { NotificationBell } from "./NotificationBell";
import { asNotificationCategory, NotificationPanel, stripMarkdownPreview } from "./NotificationPanel";
import { useCompanionGroupInvites, type GroupInviteError } from "./CompanionGroupInvites";
import { useNotifications, type AdminNotification } from "./useNotifications";
import { IconRecord } from "./WorkbenchIcons";

function groupInviteElsewhereText(error: GroupInviteError, lang: string): string {
    const name = error.name.trim();
    if (!name) return error.message;
    return localizeText(
        lang,
        `Couldn't handle “${name}”: ${error.message}`,
        `「${name}」未处理：${error.message}`,
        `「${name}」未處理：${error.message}`,
    );
}

export function AssistantTitleBarNotifications({
    active = true,
    inline,
    lang,
    theme: t,
}: {
    active?: boolean;
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
    const { notices: groupInvites, decide: decideGroupInvite, busyId: groupInviteBusyId, error: groupInviteError, listError: groupInviteListError } = useCompanionGroupInvites(active, lang);
    const notificationsWithInvites = useMemo(
        () => [...groupInvites, ...notifications],
        [groupInvites, notifications],
    );
    const [selectedNotification, setSelectedNotification] = useState<AdminNotification | null>(null);
    const handleSelectNotification = useCallback((notification: AdminNotification) => {
        if (!notification.groupInviteId && !notification.is_read) markRead(notification.id);
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
    useEffect(() => {
        if (!active) return;
        const openSystemNotifications = (event: Event) => {
            const shouldToggle = (event as CustomEvent<{ toggle?: boolean }>).detail?.toggle !== false;
            if (!shouldToggle) { if (!panelOpen) togglePanel(); return; }
            if (panelOpen) setSelectedNotification(null);
            togglePanel();
        };
        window.addEventListener('maclaw:open-system-notifications', openSystemNotifications);
        return () => window.removeEventListener('maclaw:open-system-notifications', openSystemNotifications);
    }, [active, panelOpen, togglePanel]);
    useEffect(() => { if (active || !panelOpen) return; setSelectedNotification(null); togglePanel(); const focused = document.activeElement; if (focused instanceof HTMLElement && focused.closest('[data-testid="notification-panel"]')) focused.blur(); }, [active, panelOpen, togglePanel]);
    const resolvedSelected = selectedNotification
        ? notificationsWithInvites.find((item) => item.id === selectedNotification.id)
            ?? (selectedNotification.groupInviteId ? null : selectedNotification)
        : null;
    const openGroupInviteId = resolvedSelected?.groupInviteId || "";
    // One decision is in flight for the whole list. Every open invite's buttons
    // follow that lock, otherwise another row looks ready and the click is dropped.
    const groupInviteBusy = Boolean(openGroupInviteId) && Boolean(groupInviteBusyId);
    const groupInviteHold = groupInviteBusy && !groupInviteError
        ? groupInviteBusyId === openGroupInviteId
            ? localizeText(lang, "Handling this invitation.", "正在处理这条邀请。", "正在處理這條邀請。")
            : localizeText(lang, "Another invitation is still being handled.", "正在处理另一条邀请。", "正在處理另一條邀請。")
        : "";
    // The detail only explains a failure for the invitation on screen. Anywhere
    // else, keep that sentence so leaving the row does not hide the result.
    const groupInviteElsewhere = groupInviteError && groupInviteError.id !== openGroupInviteId
        ? groupInviteElsewhereText(groupInviteError, lang)
        : "";
    useEffect(() => {
        if (!urgentToast || panelOpen) return;
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === "Escape") dismissUrgentToast();
        };
        document.addEventListener("keydown", onKeyDown);
        return () => document.removeEventListener("keydown", onKeyDown);
    }, [dismissUrgentToast, panelOpen, urgentToast]);
    return (
        <>
            <div data-notification-anchor="true" className="atbn-anchor">
                <NotificationBell
                    unreadCount={unreadCount + groupInvites.length}
                    onClick={handleTogglePanel}
                    theme={t}
                    inline={inline}
                    open={active && panelOpen}
                    lang={lang}
                />
            </div>
            {panelOpen && (
                <NotificationPanel
                    notifications={notificationsWithInvites}
                    categoryFilter={asNotificationCategory(categoryFilter)}
                    onCategoryChange={(category) => setCategoryFilter(category)}
                    onMarkAllRead={markAllRead}
                    onSelectNotification={handleSelectNotification}
                    onClose={handleClosePanel}
                    selectedNotification={resolvedSelected}
                    onBackFromDetail={handleBackFromDetail}
                    onGroupInviteAction={decideGroupInvite}
                    groupInviteBusy={groupInviteBusy}
                    groupInviteHold={groupInviteHold}
                    groupInviteActionError={groupInviteElsewhere}
                    groupInviteListError={groupInviteListError}
                    groupInviteError={resolvedSelected?.groupInviteId && groupInviteError?.id === resolvedSelected.groupInviteId ? groupInviteError.message : ""}
                    ariaHidden={!active}
                    lang={lang}
                />
            )}
            {urgentToast && active && !panelOpen && (
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
                            {localizeText(lang, "Urgent Notification", "\u7d27\u6025\u901a\u77e5", "\u7dca\u6025\u901a\u77e5")}
                        </span>
                        <button
                            onClick={dismissUrgentToast}
                            className="notification-toast__close"
                            aria-label={localizeText(lang, "Dismiss", "\u5173\u95ed", "\u95dc\u9589")}
                        >
                            ×
                        </button>
                    </div>
                    <button
                        data-testid="notification-urgent-toast-open"
                        onClick={handleOpenFromToast}
                        className="notification-toast__open"
                    >
                        <span className="notification-toast__title">{urgentToast.title}</span>
                        <span className="notification-toast__preview">
                            {stripMarkdownPreview(urgentToast.content)}
                        </span>
                        <span className="notification-toast__cta">
                            {localizeText(lang, "View full notice", "\u67e5\u770b\u5168\u6587", "\u67e5\u770b\u5168\u6587")}
                        </span>
                    </button>
                </div>
            )}
        </>
    );
}
