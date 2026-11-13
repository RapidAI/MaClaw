import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { EventsOn } from "../../../wailsjs/runtime";
import { windowNoDragRegionProps } from "../../utils/windowDrag";
import { localizeText } from "./aiAssistantI18n";
import type { Theme } from "./aiAssistantPanelTheme";
import { NotificationBell } from "./NotificationBell";
import { asNotificationCategory, NotificationPanel } from "./NotificationPanel";
import { refreshCompanionGroupInvites, useCompanionGroupInvites } from "./CompanionGroupInvites";
import { useNotifications, type AdminNotification } from "./useNotifications";
import { NotificationUrgentToast } from "./NotificationUrgentToast";
import { COMPANION_GROUP_INVITE_SOURCE, groupInviteElsewhereText, groupInviteGroupID } from "./AssistantTitleBarNotifications.groupInvite";
import { useShareActions } from "./AssistantTitleBarNotifications.shareActions";

export function AssistantTitleBarNotifications({
    active = true,
    inline,
    bellHidden = false,
    lang,
    theme: t,
}: {
    active?: boolean;
    inline: boolean;
    /** Hide the bell button but keep this host mounted: the panel, urgent
     * toasts and the open-system-notifications listener live here, and the
     * sidebar status row now owns the visible bell. */
    bellHidden?: boolean;
    lang: string;
    theme: Theme;
}) {
    const {
        notifications,
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
    const [remoteBusy, setRemoteBusy] = useState(false);
    const [remoteError, setRemoteError] = useState("");
    // A successful invite decision is sticky: the buttons stay settled so a
    // re-opened notice cannot look unanswered or be clicked twice.
    const [remoteDecided, setRemoteDecided] = useState<Record<string, string>>({});
    const decideRemoteControl = useCallback(async (grantId: string, action: "accept" | "decline" | "ignore" | "end") => {
        const bindingName = action === "accept"
            ? "AcceptRemoteControlInvite"
            : action === "decline"
                ? "RejectRemoteControlInvite"
                : action === "ignore"
                    ? "IgnoreRemoteControlInvite"
                    : "RevokeRemoteControl";
        const fn = (window as unknown as { go?: { main?: { App?: Record<string, (id: string) => Promise<unknown>> } } }).go?.main?.App?.[bindingName];
        if (typeof fn !== "function") {
            setRemoteError(localizeText(lang, "Remote control is not ready", "远控尚未就绪", "遠控尚未就緒"));
            return;
        }
        setRemoteBusy(true);
        setRemoteError("");
        try {
            await fn(grantId);
            setRemoteDecided(prev => ({ ...prev, [grantId]: action }));
        } catch (err) {
            const raw = err instanceof Error
                ? err.message
                : typeof err === "string"
                    ? err
                    : err && typeof err === "object" && "message" in err
                        ? String((err as { message?: unknown }).message || "")
                        : "";
            setRemoteError(raw.trim() || localizeText(lang, "Couldn't update remote control", "暂时无法处理远控", "暫時無法處理遠控"));
        } finally {
            setRemoteBusy(false);
        }
    }, [lang]);
    // A pushed invitation notice means the pending list changed. Refreshing it
    // here is what makes an invitation appear without waiting for the poll. A
    // reconnect pulls the list's notices too, so the sync event counts as well.
    useEffect(() => {
        if (!active) return;
        const onNew = (payload: unknown) => {
            const record = payload && typeof payload === "object" ? payload as Record<string, unknown> : {};
            if (record.source === COMPANION_GROUP_INVITE_SOURCE || record.Source === COMPANION_GROUP_INVITE_SOURCE) {
                refreshCompanionGroupInvites();
            }
        };
        const onSync = (payload: unknown) => {
            const items = Array.isArray(payload) ? payload : [];
            const hasInvite = items.some((item) => {
                const record = item && typeof item === "object" ? item as Record<string, unknown> : {};
                return record.source === COMPANION_GROUP_INVITE_SOURCE || record.Source === COMPANION_GROUP_INVITE_SOURCE;
            });
            if (hasInvite) refreshCompanionGroupInvites();
        };
        const unsubscribeNew = EventsOn("notification:new", onNew);
        const unsubscribeSync = EventsOn("notification:sync", onSync);
        return () => {
            if (typeof unsubscribeNew === "function") unsubscribeNew();
            if (typeof unsubscribeSync === "function") unsubscribeSync();
        };
    }, [active]);
    // An invite notice is redundant only while its invite row is on screen. If
    // the list could not be read, the notice is the only copy left: keep it and
    // make it actionable so the person can still answer from here.
    const hubNotifications = useMemo(() => {
        const shownGroups = new Set(groupInvites.map((invite) => invite.groupInviteId || ""));
        const out: AdminNotification[] = [];
        for (const notice of notifications) {
            const groupID = groupInviteGroupID(notice);
            if (groupID && shownGroups.has(groupID)) continue;
            out.push(groupID ? { ...notice, groupInviteId: groupID } : notice);
        }
        return out;
    }, [groupInvites, notifications]);
    const notificationsWithInvites = useMemo(
        () => [...groupInvites, ...hubNotifications],
        [groupInvites, hubNotifications],
    );
    // The badge counts what the bell actually lists, once per invitation group.
    // Taking the group set from both sources keeps the number steady while the
    // pending list and the delivery notice arrive at different moments.
    const inviteCount = useMemo(() => {
        const groups = new Set<string>();
        for (const invite of groupInvites) {
            if (invite.groupInviteId) groups.add(invite.groupInviteId);
        }
        for (const notice of notifications) {
            const groupID = groupInviteGroupID(notice);
            if (groupID) groups.add(groupID);
        }
        return groups.size;
    }, [groupInvites, notifications]);
    const hubUnreadCount = useMemo(
        () => hubNotifications.filter((notice) => !notice.is_read && !groupInviteGroupID(notice)).length,
        [hubNotifications],
    );
    const [selectedNotification, setSelectedNotification] = useState<AdminNotification | null>(null);
    // Clear the selection and close the panel: a successful share action puts
    // the companion in front, so the main window must not sit over it.
    const dismissSelectedNotification = useCallback(() => {
        setSelectedNotification(null);
        if (panelOpen) togglePanel();
    }, [panelOpen, togglePanel]);
    const share = useShareActions(lang, dismissSelectedNotification);
    // Destructured so the handlers below depend on the stable callbacks
    // rather than a fresh object identity on every render.
    const { resetShareErrors } = share;
    const handleSelectNotification = useCallback((notification: AdminNotification) => {
        if (!notification.groupInviteId && !notification.is_read) markRead(notification.id);
        resetShareErrors();
        setSelectedNotification(notification);
    }, [markRead, resetShareErrors]);
    const handleClosePanel = useCallback(() => {
        dismissSelectedNotification();
        resetShareErrors();
    }, [dismissSelectedNotification, resetShareErrors]);
    const handleBackFromDetail = useCallback(() => {
        setSelectedNotification(null);
    }, []);
    const handleOpenFromToast = useCallback(() => {
        if (!urgentToast) return;
        if (!urgentToast.is_read) markRead(urgentToast.id);
        // The toast opens a different notice, so an error from the previous
        // one must not surface on its detail.
        resetShareErrors();
        setSelectedNotification(urgentToast);
        if (!panelOpen) togglePanel();
        dismissUrgentToast();
    }, [dismissUrgentToast, markRead, panelOpen, resetShareErrors, togglePanel, urgentToast]);
    const handleTogglePanel = useCallback(() => {
        if (panelOpen) {
            setSelectedNotification(null);
            resetShareErrors();
        }
        togglePanel();
    }, [panelOpen, resetShareErrors, togglePanel]);
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
    const anchorRef = useRef<HTMLDivElement>(null);
    // The PDF preview is its own document. A click on the page never reaches
    // the listener on this document, so the panel stayed open over the file.
    // The companion title bar already paints above that frame; the same scrim
    // the 打开 menu uses is the hit target, and the existing outside-click
    // listener closes the panel. The main window has no such frame.
    const [companionBar, setCompanionBar] = useState<Element | null>(null);
    useLayoutEffect(() => {
        const next = panelOpen ? anchorRef.current?.closest(".file-companion-titlebar") ?? null : null;
        setCompanionBar((current) => (current === next ? current : next));
    }, [panelOpen]);
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
            <div data-notification-anchor="true" className="atbn-anchor" ref={anchorRef}>
                {bellHidden ? null : (
                    <NotificationBell
                        unreadCount={hubUnreadCount + inviteCount}
                        onClick={handleTogglePanel}
                        theme={t}
                        inline={inline}
                        open={active && panelOpen}
                        lang={lang}
                    />
                )}
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
                    onShareOpen={share.openSharedDocument}
                    shareOpenBusy={share.shareOpenBusy}
                    shareOpenError={share.shareOpenError}
                    onHostTransfer={share.handleHostTransfer}
                    hostTransferBusy={share.hostTransferBusy}
                    hostTransferError={share.hostTransferError}
                    onRemoteControl={(grantId, action) => { void decideRemoteControl(grantId, action); }}
                    remoteControlBusy={remoteBusy}
                    remoteControlError={remoteError}
                    remoteControlDecided={remoteDecided}
                    ariaHidden={!active}
                    lang={lang}
                />
            )}
            {companionBar
                ? createPortal(
                    <div
                        className="file-companion-open-scrim"
                        data-testid="file-companion-notification-scrim"
                        {...windowNoDragRegionProps()}
                    />,
                    companionBar,
                )
                : null}
            {urgentToast && active && !panelOpen && (
                <NotificationUrgentToast
                    notification={urgentToast}
                    lang={lang}
                    onDismiss={dismissUrgentToast}
                    onOpen={handleOpenFromToast}
                />
            )}
        </>
    );
}
