import React from "react";
import { IconBell } from "./WorkbenchIcons";

export type NotificationCategory =
    | "system_announcement"
    | "feature_update"
    | "security_alert"
    | "maintenance"
    | "custom";

export type NotificationPriority = "normal" | "important" | "urgent";

export interface AdminNotification {
    id: string;
    title: string;
    content: string;
    category: NotificationCategory;
    priority: NotificationPriority;
    is_read: boolean;
    created_at: string;
}

export interface NotificationPanelTheme {
    bg: string;
    text: string;
    textMuted: string;
    headingColor: string;
    divider: string;
    inputBarBg: string;
    inputBarBorder: string;
}

export interface NotificationPanelProps {
    notifications: AdminNotification[];
    categoryFilter: NotificationCategory | null;
    onCategoryChange: (category: NotificationCategory | null) => void;
    onMarkAllRead: () => void;
    onSelectNotification: (notification: AdminNotification) => void;
    onClose: () => void;
    lang?: string;
    theme: NotificationPanelTheme;
}

interface CategoryMeta {
    labelEn: string;
    labelZh: string;
    color: string;
}

interface NotificationItemProps {
    notification: AdminNotification;
    lang?: string;
    theme: NotificationPanelTheme;
    onSelect: (notification: AdminNotification) => void;
}

const localizeText = (
    lang: string | undefined,
    en: string,
    zhHans: string,
    zhHant: string = zhHans,
) => (lang === "zh-Hant" ? zhHant : lang?.startsWith("zh") ? zhHans : en);

const CATEGORY_META: Record<NotificationCategory, CategoryMeta> = {
    system_announcement: { labelEn: "System", labelZh: "系统公告", color: "#3b82f6" },
    feature_update: { labelEn: "Feature", labelZh: "功能更新", color: "#10b981" },
    security_alert: { labelEn: "Security", labelZh: "安全警报", color: "#ef4444" },
    maintenance: { labelEn: "Ops", labelZh: "运维通知", color: "#f59e0b" },
    custom: { labelEn: "Custom", labelZh: "自定义", color: "#8b5cf6" },
};

const ALL_CATEGORIES: (NotificationCategory | null)[] = [
    null,
    "system_announcement",
    "feature_update",
    "security_alert",
    "maintenance",
    "custom",
];

function formatRelativeTime(isoString: string, lang?: string): string {
    const now = Date.now();
    const then = new Date(isoString).getTime();
    if (isNaN(then)) return isoString;

    const diffMs = now - then;
    const diffMin = Math.floor(diffMs / 60000);
    const diffHour = Math.floor(diffMs / 3600000);
    const diffDay = Math.floor(diffMs / 86400000);
    const isZh = lang?.startsWith("zh");

    if (diffMin < 1) return isZh ? "刚刚" : "just now";
    if (diffMin < 60) return isZh ? `${diffMin}分钟前` : `${diffMin}m ago`;
    if (diffHour < 24) return isZh ? `${diffHour}小时前` : `${diffHour}h ago`;
    if (diffDay < 7) return isZh ? `${diffDay}天前` : `${diffDay}d ago`;

    const d = new Date(isoString);
    return `${d.getMonth() + 1}/${d.getDate()}`;
}

const NotificationItem: React.FC<NotificationItemProps> = React.memo(({
    notification,
    lang,
    theme: t,
    onSelect,
}) => {
    const meta = CATEGORY_META[notification.category];
    const categoryLabel = lang?.startsWith("zh") ? meta.labelZh : meta.labelEn;
    const priorityText =
        notification.priority === "urgent"
            ? localizeText(lang, "Urgent", "紧急", "緊急")
            : notification.priority === "important"
              ? localizeText(lang, "Important", "重要", "重要")
              : "";
    const priorityStyle: React.CSSProperties | undefined =
        notification.priority === "urgent"
            ? { color: "#ef4444", fontWeight: 700 }
            : notification.priority === "important"
              ? { color: "#f59e0b", fontWeight: 600 }
              : undefined;

    return (
        <div
            data-testid={`notification-item-${notification.id}`}
            onClick={() => onSelect(notification)}
            style={{
                display: "flex",
                flexDirection: "column",
                gap: "3px",
                padding: "8px 14px",
                cursor: "pointer",
                borderBottom: `1px solid ${t.divider}`,
                background: notification.is_read ? "transparent" : `${t.headingColor}08`,
                transition: "background 0.1s ease",
            }}
            onMouseEnter={(e) => {
                e.currentTarget.style.background = `${t.headingColor}12`;
            }}
            onMouseLeave={(e) => {
                e.currentTarget.style.background = notification.is_read
                    ? "transparent"
                    : `${t.headingColor}08`;
            }}
        >
            <div
                style={{
                    display: "flex",
                    alignItems: "center",
                    gap: "6px",
                    fontSize: "11px",
                }}
            >
                <span
                    style={{
                        padding: "1px 6px",
                        borderRadius: "8px",
                        background: `${meta.color}18`,
                        color: meta.color,
                        fontSize: "10px",
                        fontWeight: 500,
                        lineHeight: 1.4,
                        flexShrink: 0,
                    }}
                >
                    {categoryLabel}
                </span>

                {priorityText && (
                    <span style={{ fontSize: "10px", ...priorityStyle }}>
                        {priorityText}
                    </span>
                )}

                <span
                    style={{
                        marginLeft: "auto",
                        color: t.textMuted,
                        fontSize: "10px",
                        flexShrink: 0,
                    }}
                >
                    {formatRelativeTime(notification.created_at, lang)}
                </span>

                {!notification.is_read && (
                    <span
                        style={{
                            width: "6px",
                            height: "6px",
                            borderRadius: "50%",
                            background: t.headingColor,
                            flexShrink: 0,
                        }}
                    />
                )}
            </div>

            <span
                style={{
                    fontSize: "13px",
                    fontWeight: notification.is_read ? 400 : 600,
                    color: t.text,
                    overflow: "hidden",
                    textOverflow: "ellipsis",
                    whiteSpace: "nowrap",
                }}
            >
                {notification.title}
            </span>
        </div>
    );
});

export const NotificationPanel: React.FC<NotificationPanelProps> = ({
    notifications,
    categoryFilter,
    onCategoryChange,
    onMarkAllRead,
    onSelectNotification,
    onClose,
    lang,
    theme: t,
}) => {
    const filteredNotifications = categoryFilter
        ? notifications.filter((n) => n.category === categoryFilter)
        : notifications;
    const hasUnread = notifications.some((n) => !n.is_read);

    return (
        <div
            data-testid="notification-panel"
            style={{
                position: "absolute",
                top: "100%",
                right: 0,
                width: "340px",
                maxHeight: "420px",
                background: t.bg,
                border: `1px solid ${t.divider}`,
                borderRadius: "8px",
                boxShadow: "0 4px 24px rgba(0, 0, 0, 0.15)",
                display: "flex",
                flexDirection: "column",
                zIndex: 1000,
                overflow: "hidden",
            }}
            onClick={(e) => e.stopPropagation()}
        >
            <div
                style={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    padding: "10px 14px 8px",
                    borderBottom: `1px solid ${t.divider}`,
                    flexShrink: 0,
                }}
            >
                <span
                    style={{
                        fontSize: "14px",
                        fontWeight: 600,
                        color: t.text,
                    }}
                >
                    {localizeText(lang, "Notifications", "通知", "通知")}
                </span>
                <button
                    data-testid="notification-panel-close"
                    onClick={onClose}
                    style={{
                        background: "none",
                        border: "none",
                        cursor: "pointer",
                        color: t.textMuted,
                        fontSize: "16px",
                        padding: "2px 4px",
                        lineHeight: 1,
                    }}
                    aria-label={localizeText(lang, "Close", "关闭", "關閉")}
                >
                    ×
                </button>
            </div>

            <div
                data-testid="notification-category-filter"
                style={{
                    display: "flex",
                    flexWrap: "wrap",
                    gap: "4px",
                    padding: "8px 14px",
                    borderBottom: `1px solid ${t.divider}`,
                    flexShrink: 0,
                }}
            >
                {ALL_CATEGORIES.map((cat) => {
                    const isActive = categoryFilter === cat;
                    const label =
                        cat === null
                            ? localizeText(lang, "All", "全部", "全部")
                            : lang?.startsWith("zh")
                              ? CATEGORY_META[cat].labelZh
                              : CATEGORY_META[cat].labelEn;
                    const pillColor = cat ? CATEGORY_META[cat].color : t.headingColor;

                    return (
                        <button
                            key={cat ?? "all"}
                            data-testid={`notification-filter-${cat ?? "all"}`}
                            onClick={() => onCategoryChange(cat)}
                            style={{
                                padding: "3px 8px",
                                fontSize: "11px",
                                fontWeight: isActive ? 600 : 400,
                                borderRadius: "10px",
                                border: `1px solid ${isActive ? pillColor : t.divider}`,
                                background: isActive ? `${pillColor}18` : "transparent",
                                color: isActive ? pillColor : t.textMuted,
                                cursor: "pointer",
                                lineHeight: 1.3,
                                transition: "all 0.15s ease",
                            }}
                        >
                            {label}
                        </button>
                    );
                })}
            </div>

            <div
                data-testid="notification-list"
                style={{
                    flex: 1,
                    overflowY: "auto",
                    padding: "4px 0",
                }}
            >
                {filteredNotifications.length === 0 ? (
                    <div
                        data-testid="notification-empty-state"
                        style={{
                            display: "flex",
                            flexDirection: "column",
                            alignItems: "center",
                            justifyContent: "center",
                            padding: "32px 16px",
                            color: t.textMuted,
                            fontSize: "13px",
                            textAlign: "center",
                            gap: "8px",
                        }}
                    >
                        <span style={{ opacity: 0.45, display: "inline-flex" }}><IconBell size={28} /></span>
                        <span>
                            {localizeText(lang, "No notifications", "没有通知", "沒有通知")}
                        </span>
                    </div>
                ) : (
                    filteredNotifications.map((notification) => (
                        <NotificationItem
                            key={notification.id}
                            notification={notification}
                            lang={lang}
                            theme={t}
                            onSelect={onSelectNotification}
                        />
                    ))
                )}
            </div>

            <div
                style={{
                    padding: "8px 14px",
                    borderTop: `1px solid ${t.divider}`,
                    flexShrink: 0,
                    display: "flex",
                    justifyContent: "center",
                }}
            >
                <button
                    data-testid="notification-mark-all-read"
                    onClick={onMarkAllRead}
                    disabled={!hasUnread}
                    style={{
                        background: "none",
                        border: "none",
                        cursor: hasUnread ? "pointer" : "default",
                        color: hasUnread ? t.headingColor : t.textMuted,
                        fontSize: "12px",
                        fontWeight: 500,
                        padding: "4px 12px",
                        borderRadius: "4px",
                        opacity: hasUnread ? 1 : 0.5,
                        transition: "opacity 0.15s ease",
                    }}
                >
                    {localizeText(lang, "Mark all as read", "全部标为已读", "全部標為已讀")}
                </button>
            </div>
        </div>
    );
};

export default NotificationPanel;
