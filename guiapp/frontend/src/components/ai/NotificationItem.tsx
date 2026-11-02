import type { CSSProperties, MouseEvent } from "react";
import type { Theme } from "./aiAssistantPanelTheme";
import { intlLocale, normalizeLang } from "../../i18n";
import { chromeText } from "../../i18n/chromeText";


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

// Status pills route through the --theme-* tokens (see styles/partials/00-tokens.css)
// so category colors follow the active light/dark scheme instead of a hardcoded
// Tailwind palette that went theme-blind in dark mode.
const CATEGORY_COLORS: Record<AdminNotification["category"], { bg: string; text: string; border: string }> = {
  system_announcement: { bg: "var(--theme-info-bg)", text: "var(--theme-primary)", border: "color-mix(in srgb, var(--theme-primary) 25%, transparent)" },
  feature_update: { bg: "var(--theme-success-bg)", text: "var(--theme-success)", border: "color-mix(in srgb, var(--theme-success) 25%, transparent)" },
  security_alert: { bg: "var(--theme-danger-bg)", text: "var(--theme-danger)", border: "color-mix(in srgb, var(--theme-danger) 25%, transparent)" },
  maintenance: { bg: "var(--theme-warning-bg)", text: "var(--theme-warning)", border: "color-mix(in srgb, var(--theme-warning) 25%, transparent)" },
  custom: { bg: "var(--theme-surface-muted)", text: "var(--theme-text-secondary)", border: "var(--theme-border-subtle)" },
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
  const seconds = Math.floor(Math.abs(diffMs) / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);

  if (diffMs < 0 || seconds < 60) return chromeText(lang, "just now", "刚刚");
  if (minutes < 60) return chromeText(lang, "{n}m ago", "{n}分钟前").replace("{n}", String(minutes));
  if (hours < 24) return chromeText(lang, "{n}h ago", "{n}小时前").replace("{n}", String(hours));
  if (days < 30) return chromeText(lang, "{n}d ago", "{n}天前").replace("{n}", String(days));
  const normalized = normalizeLang(lang);
  const dateLocale = normalized === "zh-Hans" || normalized === "zh-Hant" ? "zh-CN" : intlLocale(lang);
  return new Date(isoStr).toLocaleDateString(dateLocale);
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export function NotificationItem({ notification, onClick, theme, lang }: NotificationItemProps) {
  const { category, title, priority, is_read, created_at } = notification;
  const catColor = CATEGORY_COLORS[category] || CATEGORY_COLORS.custom;
  const catLabel = CATEGORY_LABELS[category] || CATEGORY_LABELS.custom;
  const priorityLabel = priority === "urgent"
    ? chromeText(lang, "Urgent", "紧急")
    : chromeText(lang, "Important", "重要");

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
    background: is_read ? "transparent" : "var(--theme-info-bg)",
    borderLeft: is_read ? "3px solid transparent" : `3px solid ${theme.linkColor || "var(--theme-primary)"}`,
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
      ? { display: "inline-block", width: "6px", height: "6px", borderRadius: "50%", background: "var(--theme-danger)", flexShrink: 0 }
      : priority === "important"
        ? { display: "inline-block", width: "6px", height: "6px", borderRadius: "50%", background: "var(--theme-warning)", flexShrink: 0 }
        : null;

  // Time style
  const timeStyle: CSSProperties = {
    fontSize: "10px",
    color: theme.textMuted || "var(--theme-text-muted)",
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
      aria-label={`${is_read ? "" : chromeText(lang, "Unread: ", "未读：")}${title}`}
      onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onClick(notification); } }}
      style={containerStyle}
    >
      {/* Top row: category pill + priority + time */}
      <div className="ni-top-row">
        <span style={pillStyle}>{chromeText(lang, catLabel.en, catLabel.zh)}</span>
        {priorityStyle && (
          <span
            style={priorityStyle}
            title={priorityLabel}
            aria-label={priorityLabel}
          />
        )}
        <span className="ni-spacer" />
        <span style={timeStyle}>{formatRelativeTime(created_at, lang)}</span>
      </div>

      {/* Title row */}
      <div style={titleStyle}>{title}</div>
    </div>
  );
}
