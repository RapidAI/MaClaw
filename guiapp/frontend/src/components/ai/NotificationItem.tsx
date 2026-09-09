import type { CSSProperties, MouseEvent } from "react";
import type { Theme } from "./aiAssistantPanelTheme";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface AdminNotification {
  id: string;
  title: string;
  content: string;
  category: "system_announcement" | "feature_update" | "security_alert" | "maintenance" | "custom";
  priority: "normal" | "important" | "urgent";
  is_read: boolean;
  created_at: string; // ISO8601
}

export interface NotificationItemProps {
  notification: AdminNotification;
  onClick: (notification: AdminNotification) => void;
  theme: Theme;
  lang?: string;
}

// ---------------------------------------------------------------------------
// Category config — pill color mapping
// ---------------------------------------------------------------------------

const CATEGORY_COLORS: Record<AdminNotification["category"], { bg: string; text: string; border: string }> = {
  system_announcement: { bg: "rgba(59, 130, 246, 0.10)", text: "#2563eb", border: "rgba(59, 130, 246, 0.25)" },
  feature_update: { bg: "rgba(16, 185, 129, 0.10)", text: "#059669", border: "rgba(16, 185, 129, 0.25)" },
  security_alert: { bg: "rgba(239, 68, 68, 0.10)", text: "#dc2626", border: "rgba(239, 68, 68, 0.25)" },
  maintenance: { bg: "rgba(245, 158, 11, 0.10)", text: "#d97706", border: "rgba(245, 158, 11, 0.25)" },
  custom: { bg: "rgba(107, 114, 128, 0.10)", text: "#4b5563", border: "rgba(107, 114, 128, 0.25)" },
};

const CATEGORY_LABELS: Record<AdminNotification["category"], { en: string; zh: string }> = {
  system_announcement: { en: "Announcement", zh: "系统公告" },
  feature_update: { en: "Update", zh: "功能更新" },
  security_alert: { en: "Security", zh: "安全告警" },
  maintenance: { en: "Maintenance", zh: "运维通知" },
  custom: { en: "Custom", zh: "自定义" },
};

// ---------------------------------------------------------------------------
// Relative time formatting
// ---------------------------------------------------------------------------

export function formatRelativeTime(isoStr: string, lang?: string): string {
  const now = Date.now();
  const then = new Date(isoStr).getTime();
  if (isNaN(then)) return isoStr;

  const diffMs = now - then;
  if (diffMs < 0) return lang === "en" ? "just now" : "刚刚";

  const seconds = Math.floor(diffMs / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);

  if (lang === "en") {
    if (seconds < 60) return "just now";
    if (minutes < 60) return `${minutes}m ago`;
    if (hours < 24) return `${hours}h ago`;
    if (days < 30) return `${days}d ago`;
    return new Date(isoStr).toLocaleDateString("en");
  }

  // Chinese (default)
  if (seconds < 60) return "刚刚";
  if (minutes < 60) return `${minutes}分钟前`;
  if (hours < 24) return `${hours}小时前`;
  if (days < 30) return `${days}天前`;
  return new Date(isoStr).toLocaleDateString("zh-CN");
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export function NotificationItem({ notification, onClick, theme, lang }: NotificationItemProps) {
  const { category, title, priority, is_read, created_at } = notification;
  const catColor = CATEGORY_COLORS[category] || CATEGORY_COLORS.custom;
  const catLabel = CATEGORY_LABELS[category] || CATEGORY_LABELS.custom;
  const isZh = lang !== "en";

  const handleClick = (e: MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    onClick(notification);
  };

  // Container style — unread gets subtle highlight background
  const containerStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "4px",
    padding: "10px 12px",
    cursor: "pointer",
    borderRadius: "6px",
    transition: "background 150ms ease",
    background: is_read ? "transparent" : (theme.isDark ? "rgba(59, 130, 246, 0.06)" : "rgba(59, 130, 246, 0.04)"),
    borderLeft: is_read ? "3px solid transparent" : `3px solid ${theme.linkColor || "#2563eb"}`,
  };

  // Title style — unread is bold
  const titleStyle: CSSProperties = {
    fontSize: "12px",
    lineHeight: "1.4",
    color: theme.text,
    fontWeight: is_read ? 400 : 600,
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  };

  // Category pill style
  const pillStyle: CSSProperties = {
    display: "inline-flex",
    alignItems: "center",
    padding: "1px 6px",
    borderRadius: "999px",
    fontSize: "10px",
    fontWeight: 500,
    lineHeight: "1.4",
    background: catColor.bg,
    color: catColor.text,
    border: `1px solid ${catColor.border}`,
    flexShrink: 0,
    whiteSpace: "nowrap",
  };

  // Priority indicator style
  const priorityStyle: CSSProperties | null =
    priority === "urgent"
      ? { display: "inline-block", width: "6px", height: "6px", borderRadius: "50%", background: "#dc2626", flexShrink: 0 }
      : priority === "important"
        ? { display: "inline-block", width: "6px", height: "6px", borderRadius: "50%", background: "#f59e0b", flexShrink: 0 }
        : null;

  // Time style
  const timeStyle: CSSProperties = {
    fontSize: "10px",
    color: theme.textMuted || "#64748b",
    flexShrink: 0,
    whiteSpace: "nowrap",
  };

  return (
    <div
      className="notification-item"
      data-testid={`notification-item-${notification.id}`}
      onClick={handleClick}
      role="button"
      tabIndex={0}
      aria-label={`${is_read ? "" : (isZh ? "未读：" : "Unread: ")}${title}`}
      onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onClick(notification); } }}
      style={containerStyle}
    >
      {/* Top row: category pill + priority + time */}
      <div style={{ display: "flex", alignItems: "center", gap: "6px" }}>
        <span style={pillStyle}>{isZh ? catLabel.zh : catLabel.en}</span>
        {priorityStyle && (
          <span
            style={priorityStyle}
            title={priority === "urgent" ? (isZh ? "紧急" : "Urgent") : (isZh ? "重要" : "Important")}
            aria-label={priority === "urgent" ? (isZh ? "紧急" : "Urgent") : (isZh ? "重要" : "Important")}
          />
        )}
        <span style={{ flex: 1 }} />
        <span style={timeStyle}>{formatRelativeTime(created_at, lang)}</span>
      </div>

      {/* Title row */}
      <div style={titleStyle}>{title}</div>
    </div>
  );
}
