import React, { useEffect, useLayoutEffect, useRef, useState } from "react";
import { IconBell } from "./WorkbenchIcons";
import { companionHostTransferToken } from "./AssistantTitleBarNotifications.companionShare";
import { NotificationDetail } from "./NotificationDetail";
import type { AdminNotification as SharedNotification } from "./useNotifications";
import { localizeText } from "../../i18n";
import { tailText } from "../../i18n/mnTail"

export type NotificationCategory =
    | "system_announcement"
    | "feature_update"
    | "security_alert"
    | "maintenance"
    | "custom";

export type NotificationPriority = "normal" | "important" | "urgent";

export type AdminNotification = SharedNotification;

export interface NotificationPanelProps {
    notifications: AdminNotification[];
    categoryFilter: NotificationCategory | null;
    onCategoryChange: (category: NotificationCategory | null) => void;
    onMarkAllRead: () => void;
    onSelectNotification: (notification: AdminNotification) => void;
    onClose: () => void;
    selectedNotification?: AdminNotification | null;
    onBackFromDetail?: () => void;
    onGroupInviteAction?: (groupId: string, action: "accept" | "decline" | "ignore") => void;
    groupInviteBusy?: boolean;
    groupInviteHold?: string;
    groupInviteActionError?: string;
    groupInviteListError?: string;
    groupInviteError?: string;
    onShareOpen?: (token: string) => void;
    shareOpenBusy?: boolean;
    shareOpenError?: string;
    onHostTransfer?: (token: string, decision: "accept" | "decline") => void;
    hostTransferBusy?: boolean;
    hostTransferError?: string;
    /** Marks the transient panel inaccessible while its host page is hidden. */
    ariaHidden?: boolean;
    lang?: string;
}

interface CategoryMeta {
    labelEn: string;
    labelZh: string;
}

interface NotificationItemProps {
    notification: AdminNotification;
    lang?: string;
    onSelect: (notification: AdminNotification) => void;
}

const CATEGORY_META: Record<NotificationCategory, CategoryMeta> = {
    system_announcement: { labelEn: "System", labelZh: "系统公告" },
    feature_update: { labelEn: "Feature", labelZh: "功能更新" },
    security_alert: { labelEn: "Security", labelZh: "安全警报" },
    maintenance: { labelEn: "Ops", labelZh: "运维通知" },
    custom: { labelEn: "Custom", labelZh: "自定义" },
};

const ALL_CATEGORIES: (NotificationCategory | null)[] = [
    null,
    "system_announcement",
    "feature_update",
    "security_alert",
    "maintenance",
    "custom",
];

export function stripMarkdownPreview(markdown: string | null | undefined): string {
    return String(markdown ?? "")
        .replace(/!\[[^\]]*]\([^)]*\)/g, "")
        .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
        .replace(/[`*_~>#]/g, "")
        .replace(/\s+/g, " ")
        .trim();
}

const CATEGORY_VALUES: NotificationCategory[] = [
    "system_announcement",
    "feature_update",
    "security_alert",
    "maintenance",
    "custom",
];

export function asNotificationCategory(value: string | null | undefined): NotificationCategory | null {
    return value && CATEGORY_VALUES.includes(value as NotificationCategory)
        ? (value as NotificationCategory)
        : null;
}

export interface NotificationPanelSizeInput {
    /** Panel top in rendered pixels (post transform). */
    renderedTop: number;
    /** Panel right edge in rendered pixels; it stays put while width changes. */
    renderedRight: number;
    /** Left edge of the host canvas in rendered pixels. */
    clipLeft: number;
    /** Bottom edge of the host canvas in rendered pixels. */
    clipBottom: number;
    /** The --ui-scale applied to .app-scale-layer. */
    uiScale: number;
    /** Height the detail view prefers when there is room. */
    preferredHeight: number;
}

/**
 * Size the dropdown from its fixed right edge back into the host canvas.
 * A percentage width would resolve against the tiny bell anchor and collapse,
 * and rects are transform-scaled, so rendered pixels must be divided by the
 * UI scale before they become CSS width/height.
 */
export function measureNotificationPanel(input: NotificationPanelSizeInput): { width: number; maxHeight: number } {
    const scale = Number.isFinite(input.uiScale) && input.uiScale > 0 ? input.uiScale : 1;
    const layoutPx = (rendered: number) => rendered / scale;
    const available = Math.floor(layoutPx(input.clipBottom - input.renderedTop) - 8);
    const widthBudget = Math.floor(layoutPx(input.renderedRight - input.clipLeft) - 16);
    return {
        width: widthBudget > 0 ? Math.min(400, widthBudget) : 400,
        maxHeight: available > 0 ? Math.min(input.preferredHeight, available) : input.preferredHeight,
    };
}

function formatRelativeTime(isoString: string, lang?: string): string {
    const now = Date.now();
    const then = new Date(isoString).getTime();
    if (isNaN(then)) return isoString;

    const diffMs = now - then;
    const diffMin = Math.floor(diffMs / 60000);
    const diffHour = Math.floor(diffMs / 3600000);
    const diffDay = Math.floor(diffMs / 86400000);
    const isZh = lang?.startsWith("zh");

    if (diffMin < 1) return tailText(lang ?? "", "just now", "刚刚");
    if (diffMin < 60) return tailText(lang ?? "", `${diffMin}m ago`, `${diffMin}分钟前`);
    if (diffHour < 24) return tailText(lang ?? "", `${diffHour}h ago`, `${diffHour}小时前`);
    if (diffDay < 7) return tailText(lang ?? "", `${diffDay}d ago`, `${diffDay}天前`);

    const d = new Date(isoString);
    return `${d.getMonth() + 1}/${d.getDate()}`;
}

const NotificationItem: React.FC<NotificationItemProps> = React.memo(({
    notification,
    lang,
    onSelect,
}) => {
    const meta = CATEGORY_META[notification.category] ?? CATEGORY_META.custom;
    const categoryLabel = tailText(lang ?? "", meta.labelEn, meta.labelZh);
    const priorityText =
        notification.priority === "urgent"
            ? localizeText(lang, "Urgent", "紧急", "緊急")
            : notification.priority === "important"
              ? localizeText(lang, "Important", "重要", "重要")
              : "";
    const titleText = (notification.title || "").trim();
    const contentPreview = notification.groupInviteId
        ? String(notification.content || "").replace(/\s+/g, " ").trim()
        : stripMarkdownPreview(notification.content || "");
    const hasDistinctPreview = Boolean(contentPreview) && contentPreview !== titleText;
    const hasMoreToRead = titleText.length > 28 || hasDistinctPreview;
    const needsDecision = Boolean(notification.groupInviteId) || Boolean(companionHostTransferToken(notification));

    return (
        <div
            className={`notification-item-row${notification.is_read ? "" : " notification-item-row--unread"}`}
            data-testid={`notification-item-${notification.id}`}
            role="button"
            tabIndex={0}
            onClick={() => onSelect(notification)}
            onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    onSelect(notification);
                }
            }}
        >
            <div className="np-head-row">
                <span className={`notification-cat-badge notification-cat--${notification.category}`}>
                    {categoryLabel}
                </span>

                {priorityText && (
                    <span className={`notification-priority notification-priority--${notification.priority}`}>
                        {priorityText}
                    </span>
                )}

                <span className="notification-item-time">
                    {formatRelativeTime(notification.created_at, lang)}
                </span>

                {!notification.is_read && <span className="notification-unread-dot" aria-hidden="true" />}
            </div>

            <span className="notification-item-title">
                {notification.title}
            </span>

            {hasDistinctPreview && (
                <span
                    data-testid={`notification-item-preview-${notification.id}`}
                    className="notification-item-preview"
                >
                    {contentPreview}
                </span>
            )}

            {(needsDecision || hasMoreToRead) && (
                <span className="notification-item-more">
                    {needsDecision
                        ? localizeText(lang, "Open to respond", "打开处理", "打開處理")
                        : localizeText(lang, "View full notice", "查看全文", "查看全文")}
                </span>
            )}
        </div>
    );
});
NotificationItem.displayName = "NotificationItem";

export const NotificationPanel: React.FC<NotificationPanelProps> = ({
    notifications,
    categoryFilter,
    onCategoryChange,
    onMarkAllRead,
    onSelectNotification,
    onClose,
    selectedNotification,
    onBackFromDetail,
    onGroupInviteAction,
    groupInviteBusy = false,
    groupInviteHold = "",
    groupInviteActionError = "",
    groupInviteListError = "",
    groupInviteError = "",
    onShareOpen,
    shareOpenBusy = false,
    shareOpenError = "",
    onHostTransfer,
    hostTransferBusy = false,
    hostTransferError = "",
    ariaHidden = false,
    lang,
}) => {
    const panelRef = useRef<HTMLDivElement>(null);
    const onCloseRef = useRef(onClose);
    const onBackRef = useRef(onBackFromDetail);
    const selectedRef = useRef(selectedNotification);
    const lastSelectedIdRef = useRef<string | null>(null);
    onCloseRef.current = onClose;
    onBackRef.current = onBackFromDetail;
    selectedRef.current = selectedNotification;
    if (selectedNotification?.id) {
        lastSelectedIdRef.current = selectedNotification.id;
    }

    const inviteNotifications = notifications.filter((n) => n.groupInviteId);
    const hubNotifications = notifications.filter((n) => !n.groupInviteId);
    const filteredHub = categoryFilter
        ? hubNotifications.filter((n) => n.category === categoryFilter)
        : hubNotifications;
    const filteredNotifications = [...inviteNotifications, ...filteredHub];
    const hasUnread = hubNotifications.some((n) => !n.is_read);
    const inviteListError = groupInviteListError.trim();
    // A category with no matching rows still says that category is empty.
    // The invite-load error stays a banner, so it is not read as a category failure.
    const categoryMiss = Boolean(categoryFilter) && filteredNotifications.length === 0;
    const showInviteListErrorBanner = Boolean(inviteListError) && (filteredNotifications.length > 0 || categoryMiss);
    const emptyIsInviteError = Boolean(inviteListError) && !categoryFilter;
    const viewingDetail = Boolean(selectedNotification);
    const preferredHeight = viewingDetail ? 520 : 460;
    const [maxPanelHeight, setMaxPanelHeight] = useState(preferredHeight);
    // A percentage width would resolve against the bell anchor (a few dozen
    // pixels), not the title bar, and collapse the panel. Measure the host
    // canvas instead so the panel fits scaled windows.
    const [panelWidth, setPanelWidth] = useState(400);

    useEffect(() => {
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key !== "Escape") return;
            const active = document.activeElement;
            const focusInsidePanel = !active
                || active === document.body
                || Boolean(panelRef.current?.contains(active));
            if (focusInsidePanel) {
                event.preventDefault();
            }
            if (selectedRef.current) {
                (onBackRef.current ?? onCloseRef.current)();
                return;
            }
            onCloseRef.current();
        };
        const onPointerDown = (event: globalThis.MouseEvent) => {
            const target = event.target as Element | null;
            if (!target) return;
            if (panelRef.current?.contains(target)) return;
            if (target.closest?.("[data-notification-anchor='true']")) return;
            if (target.closest?.("[data-notification-toast='true']")) return;
            onCloseRef.current();
        };
        document.addEventListener("keydown", onKeyDown);
        document.addEventListener("mousedown", onPointerDown);
        return () => {
            document.removeEventListener("keydown", onKeyDown);
            document.removeEventListener("mousedown", onPointerDown);
        };
    }, []);

    useLayoutEffect(() => {
        const root = panelRef.current;
        if (!root) return;

        const measure = () => {
            // Do not fall back to offsetParent: the title bar is position:relative
            // and only 38px tall, which makes available space look empty.
            // Prefer the nearest ancestor that actually clips (the panel body has
            // overflow:hidden); the panel root is only the outer bound.
            const rootRect = root.getBoundingClientRect();
            const clip = root.closest("[data-testid='ai-panel-body']")
                ?? root.closest("[data-testid='ai-panel-root']");
            const clipRect = clip instanceof HTMLElement ? clip.getBoundingClientRect() : null;
            const { width, maxHeight } = measureNotificationPanel({
                renderedTop: rootRect.top,
                renderedRight: rootRect.right,
                clipLeft: clipRect ? clipRect.left : 0,
                clipBottom: clipRect ? clipRect.bottom : window.innerHeight,
                uiScale: Number.parseFloat(getComputedStyle(root).getPropertyValue("--ui-scale")),
                preferredHeight,
            });
            setPanelWidth(width);
            setMaxPanelHeight(maxHeight);
        };

        measure();
        const clip = root.closest("[data-testid='ai-panel-body']")
            ?? root.closest("[data-testid='ai-panel-root']");
        const observer = typeof ResizeObserver === "function" && clip instanceof HTMLElement
            ? new ResizeObserver(measure)
            : null;
        if (clip instanceof HTMLElement) observer?.observe(clip);
        window.addEventListener("resize", measure);
        return () => {
            observer?.disconnect();
            window.removeEventListener("resize", measure);
        };
    }, [preferredHeight, viewingDetail]);

    useEffect(() => {
        const root = panelRef.current;
        if (!root) return;
        if (ariaHidden) return;
        if (viewingDetail) {
            root.querySelector<HTMLElement>("[data-testid='notification-detail-back']")
                ?.focus({ preventScroll: true });
            return;
        }
        const lastId = lastSelectedIdRef.current;
        const escapedId = lastId && typeof CSS !== "undefined" && typeof CSS.escape === "function"
            ? CSS.escape(lastId)
            : lastId;
        const lastItem = escapedId
            ? root.querySelector<HTMLElement>(`[data-testid='notification-item-${escapedId}']`)
            : null;
        (lastItem ?? root).focus({ preventScroll: true });
    }, [ariaHidden, viewingDetail]);

    return (
        <div
            ref={panelRef}
            data-testid="notification-panel"
            role="dialog"
            aria-modal="false"
            aria-hidden={ariaHidden ? "true" : undefined}
            tabIndex={-1}
            aria-label={localizeText(lang, "Notifications", "通知", "通知")}
            className="notification-panel"
            style={{
                width: panelWidth,
                maxHeight: maxPanelHeight,
                height: viewingDetail ? maxPanelHeight : undefined,
            }}
            onClick={(e) => e.stopPropagation()}
        >
            {viewingDetail && selectedNotification ? (
                <NotificationDetail
                    notification={selectedNotification}
                    onBack={onBackFromDetail ?? onClose}
                    onClose={onClose}
                    lang={lang}
                    onGroupInviteAction={selectedNotification.groupInviteId ? onGroupInviteAction : undefined}
                    groupInviteBusy={Boolean(selectedNotification.groupInviteId) && groupInviteBusy}
                    groupInviteHold={selectedNotification.groupInviteId ? groupInviteHold : ""}
                    groupInviteActionError={selectedNotification.groupInviteId ? groupInviteActionError : ""}
                    groupInviteError={selectedNotification.groupInviteId ? groupInviteError : ""}
                    onShareOpen={onShareOpen}
                    shareOpenBusy={shareOpenBusy}
                    shareOpenError={shareOpenError}
                    onHostTransfer={onHostTransfer}
                    hostTransferBusy={hostTransferBusy}
                    hostTransferError={hostTransferError}
                />
            ) : (
            <>
            <div className="notification-panel__head">
                <span className="notification-panel__title">
                    {localizeText(lang, "Notifications", "通知", "通知")}
                </span>
                <button
                    data-testid="notification-panel-close"
                    onClick={onClose}
                    className="notification-panel__close"
                    aria-label={localizeText(lang, "Close", "关闭", "關閉")}
                >
                    ×
                </button>
            </div>

            <div
                data-testid="notification-category-filter"
                className="notification-panel__filters"
            >
                {ALL_CATEGORIES.map((cat) => {
                    const isActive = categoryFilter === cat;
                    const label =
                        cat === null
                            ? localizeText(lang, "All", "全部", "全部")
                            : tailText(lang ?? "", CATEGORY_META[cat].labelEn, CATEGORY_META[cat].labelZh);
                    return (
                        <button
                            key={cat ?? "all"}
                            type="button"
                            data-testid={`notification-filter-${cat ?? "all"}`}
                            data-category={cat ?? "all"}
                            aria-pressed={isActive}
                            onClick={() => onCategoryChange(cat)}
                            className="notification-filter"
                        >
                            {label}
                        </button>
                    );
                })}
            </div>

            <div
                data-testid="notification-list"
                className="np-list"
            >
                {showInviteListErrorBanner ? (
                    <p className="np-invite-list-error" data-testid="notification-group-invite-list-error" role="status">{inviteListError}</p>
                ) : null}
                {groupInviteActionError.trim() ? (
                    <p className="np-invite-list-error" data-testid="notification-group-invite-action-error" role="status">{groupInviteActionError.trim()}</p>
                ) : null}
                {filteredNotifications.length === 0 ? (
                    <div
                        data-testid="notification-empty-state"
                        className="np-empty"
                    >
                        <span className="np-empty-icon"><IconBell size={28} /></span>
                        <span className={emptyIsInviteError ? "np-invite-list-error" : undefined} data-testid={emptyIsInviteError ? "notification-group-invite-list-error" : undefined} role={emptyIsInviteError ? "status" : undefined}>
                            {emptyIsInviteError
                                ? inviteListError
                                : categoryFilter
                                ? localizeText(lang, "No notifications in this category", "该分类没有通知", "該分類沒有通知")
                                : localizeText(lang, "No notifications", "没有通知", "沒有通知")}
                        </span>
                    </div>
                ) : (
                    filteredNotifications.map((notification) => (
                        <NotificationItem
                            key={notification.id}
                            notification={notification}
                            lang={lang}
                            onSelect={onSelectNotification}
                        />
                    ))
                )}
            </div>

            <div className="notification-panel__footer">
                <button
                    data-testid="notification-mark-all-read"
                    onClick={onMarkAllRead}
                    disabled={!hasUnread}
                    className="notification-panel__markall"
                >
                    {localizeText(lang, "Mark all as read", "全部标为已读", "全部標為已讀")}
                </button>
            </div>
            </>
            )}
        </div>
    );
};

export default NotificationPanel;
