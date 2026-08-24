import React, { useMemo } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { BrowserOpenURL } from "../../../wailsjs/runtime";
import type { AdminNotification } from "./useNotifications";
import type { Theme } from "./aiAssistantPanelTheme";

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------

export interface NotificationDetailProps {
  notification: AdminNotification;
  onBack: () => void;
  onClose?: () => void;
  theme: Theme;
  lang?: string;
}

// ---------------------------------------------------------------------------
// Localization helper
// ---------------------------------------------------------------------------

const localizeText = (
  lang: string | undefined,
  en: string,
  zhHans: string,
  zhHant: string = zhHans,
) => (lang === "zh-Hant" ? zhHant : lang?.startsWith("zh") ? zhHans : en);

// ---------------------------------------------------------------------------
// Category display
// ---------------------------------------------------------------------------

const CATEGORY_LABELS: Record<AdminNotification["category"], { en: string; zh: string }> = {
  system_announcement: { en: "System Announcement", zh: "系统公告" },
  feature_update: { en: "Feature Update", zh: "功能更新" },
  security_alert: { en: "Security Alert", zh: "安全警报" },
  maintenance: { en: "Maintenance", zh: "运维通知" },
  custom: { en: "Custom", zh: "自定义" },
};

const CATEGORY_COLORS: Record<AdminNotification["category"], string> = {
  system_announcement: "#3b82f6",
  feature_update: "#10b981",
  security_alert: "#ef4444",
  maintenance: "#f59e0b",
  custom: "#8b5cf6",
};

// ---------------------------------------------------------------------------
// NotificationDetail component
//
// Renders the full notification content as sanitized Markdown.
// XSS protection: react-markdown without rehype-raw does NOT render raw HTML.
// All HTML tags in the Markdown source are escaped (displayed as text), which
// prevents script injection, iframe embedding, and on* event handler attacks.
// ---------------------------------------------------------------------------

export const NotificationDetail: React.FC<NotificationDetailProps> = ({
  notification,
  onBack,
  onClose,
  theme: t,
  lang,
}) => {
  const isZh = lang?.startsWith("zh");
  const catLabel = CATEGORY_LABELS[notification.category] || CATEGORY_LABELS.custom;
  const catColor = CATEGORY_COLORS[notification.category] || CATEGORY_COLORS.custom;

  const formattedDate = formatDate(notification.created_at, lang);
  const titleText = (notification.title || "").trim();
  const bodyText = (notification.content || "").trim();
  const hasDistinctBody = Boolean(bodyText) && bodyText !== titleText;
  const markdownComponents = useMemo(() => buildMarkdownComponents(t), [t]);

  return (
    <div
      data-testid="notification-detail"
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        overflow: "hidden",
      }}
    >
      {/* Header with back button */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: "8px",
          padding: "10px 14px 8px",
          borderBottom: `1px solid ${t.divider}`,
          flexShrink: 0,
        }}
      >
        <button
          data-testid="notification-detail-back"
          onClick={onBack}
          style={{
            background: "none",
            border: "none",
            cursor: "pointer",
            color: t.linkColor || t.headingColor,
            fontSize: "13px",
            padding: "4px 6px",
            borderRadius: "4px",
            display: "inline-flex",
            alignItems: "center",
            gap: "4px",
            minHeight: "28px",
            transition: "opacity 0.15s ease",
          }}
          aria-label={localizeText(lang, "Back to list", "返回列表", "返回列表")}
        >
          <span style={{ fontSize: "14px" }}>←</span>
          <span>{localizeText(lang, "Back", "返回", "返回")}</span>
        </button>
        {onClose && (
          <button
            data-testid="notification-detail-close"
            onClick={onClose}
            style={{
              background: "none",
              border: "none",
              cursor: "pointer",
              color: t.textMuted,
              fontSize: "16px",
              padding: "2px 6px",
              lineHeight: 1,
              minHeight: "28px",
            }}
            aria-label={localizeText(lang, "Close", "关闭", "關閉")}
          >
            ×
          </button>
        )}
      </div>

      {/* Content area */}
      <div
        style={{
          flex: 1,
          overflowY: "auto",
          padding: "12px 14px 16px",
        }}
      >
        {/* Title */}
        <h3
          style={{
            margin: "0 0 8px",
            fontSize: "15px",
            fontWeight: 600,
            color: t.text,
            lineHeight: 1.4,
            wordBreak: "break-word",
          }}
        >
          {notification.title}
        </h3>

        {/* Meta row: category + date + priority */}
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: "8px",
            marginBottom: "12px",
            flexWrap: "wrap",
          }}
        >
          {/* Category pill */}
          <span
            style={{
              padding: "2px 8px",
              borderRadius: "10px",
              background: `${catColor}18`,
              color: catColor,
              fontSize: "11px",
              fontWeight: 500,
              lineHeight: 1.4,
              border: `1px solid ${catColor}30`,
            }}
          >
            {isZh ? catLabel.zh : catLabel.en}
          </span>

          {/* Priority badge */}
          {notification.priority !== "normal" && (
            <span
              style={{
                padding: "2px 8px",
                borderRadius: "10px",
                fontSize: "11px",
                fontWeight: 500,
                lineHeight: 1.4,
                background:
                  notification.priority === "urgent"
                    ? "rgba(239, 68, 68, 0.10)"
                    : "rgba(245, 158, 11, 0.10)",
                color:
                  notification.priority === "urgent" ? "#dc2626" : "#d97706",
                border: `1px solid ${
                  notification.priority === "urgent"
                    ? "rgba(239, 68, 68, 0.25)"
                    : "rgba(245, 158, 11, 0.25)"
                }`,
              }}
            >
              {notification.priority === "urgent"
                ? localizeText(lang, "Urgent", "紧急", "緊急")
                : localizeText(lang, "Important", "重要", "重要")}
            </span>
          )}

          {/* Date */}
          <span
            style={{
              fontSize: "11px",
              color: t.textMuted,
              marginLeft: "auto",
            }}
          >
            {formattedDate}
          </span>
        </div>

        {hasDistinctBody ? (
          <div
            data-testid="notification-detail-content"
            className="notification-markdown-content"
            style={{
              fontSize: "13px",
              lineHeight: 1.7,
              color: t.text,
              wordBreak: "break-word",
              overflowWrap: "anywhere",
            }}
          >
            <ReactMarkdown
              remarkPlugins={[remarkGfm]}
              components={markdownComponents}
            >
              {notification.content}
            </ReactMarkdown>
          </div>
        ) : !bodyText ? (
          <div
            data-testid="notification-detail-content"
            style={{
              fontSize: "13px",
              lineHeight: 1.7,
              color: t.textMuted,
            }}
          >
            {localizeText(lang, "No additional details.", "没有更多正文。", "沒有更多正文。")}
          </div>
        ) : null}
      </div>
    </div>
  );
};

// ---------------------------------------------------------------------------
// Custom Markdown components for themed rendering
// ---------------------------------------------------------------------------

function buildMarkdownComponents(t: Theme) {
  return {
    // Links open externally
    a: ({ href, children }: any) => {
      const safeHref = typeof href === "string" && /^(https?:\/\/|mailto:)/i.test(href) ? href : "";
      if (!safeHref) {
        return (
          <span style={{ color: t.linkColor || t.headingColor }}>
            {children}
          </span>
        );
      }
      return (
        <a
          href={safeHref}
          onClick={(event) => {
            event.preventDefault();
            BrowserOpenURL(safeHref);
          }}
          style={{
            color: t.linkColor || t.headingColor,
            textDecoration: "underline",
            cursor: "pointer",
          }}
        >
          {children}
        </a>
      );
    },
    // Do not fetch remote images from announcement markdown.
    img: ({ alt }: any) =>
      alt ? (
        <span style={{ color: t.textMuted, fontSize: "12px" }}>[{alt}]</span>
      ) : null,
    // Code blocks
    code: ({ inline, children, ...props }: any) =>
      inline ? (
        <code
          {...props}
          style={{
            background: t.codeBg,
            color: t.codeText,
            padding: "1px 4px",
            borderRadius: "3px",
            fontSize: "12px",
            fontFamily: "monospace",
          }}
        >
          {children}
        </code>
      ) : (
        <code
          {...props}
          style={{
            display: "block",
            background: t.codeBlockBg,
            color: t.codeText,
            padding: "10px 12px",
            borderRadius: "6px",
            border: `1px solid ${t.codeBlockBorder}`,
            fontSize: "12px",
            fontFamily: "monospace",
            overflowX: "auto",
            whiteSpace: "pre-wrap",
            wordBreak: "break-all",
          }}
        >
          {children}
        </code>
      ),
    // Block quotes
    blockquote: ({ children, ...props }: any) => (
      <blockquote
        {...props}
        style={{
          borderLeft: `3px solid ${t.quoteBorder || t.divider}`,
          margin: "8px 0",
          padding: "4px 12px",
          color: t.quoteText || t.textMuted,
          fontSize: "12px",
        }}
      >
        {children}
      </blockquote>
    ),
    // Headings
    h1: ({ children, ...props }: any) => (
      <h1 {...props} style={{ fontSize: "16px", fontWeight: 700, color: t.headingColor, margin: "12px 0 6px" }}>
        {children}
      </h1>
    ),
    h2: ({ children, ...props }: any) => (
      <h2 {...props} style={{ fontSize: "15px", fontWeight: 600, color: t.headingColor, margin: "10px 0 5px" }}>
        {children}
      </h2>
    ),
    h3: ({ children, ...props }: any) => (
      <h3 {...props} style={{ fontSize: "14px", fontWeight: 600, color: t.headingColor, margin: "8px 0 4px" }}>
        {children}
      </h3>
    ),
    // Paragraphs
    p: ({ children, ...props }: any) => (
      <p {...props} style={{ margin: "6px 0", lineHeight: 1.7 }}>
        {children}
      </p>
    ),
    // Lists
    ul: ({ children, ...props }: any) => (
      <ul {...props} style={{ margin: "6px 0", paddingLeft: "20px" }}>
        {children}
      </ul>
    ),
    ol: ({ children, ...props }: any) => (
      <ol {...props} style={{ margin: "6px 0", paddingLeft: "20px" }}>
        {children}
      </ol>
    ),
    li: ({ children, ...props }: any) => (
      <li {...props} style={{ margin: "3px 0", lineHeight: 1.6 }}>
        {children}
      </li>
    ),
    // Tables (GFM)
    table: ({ children, ...props }: any) => (
      <div style={{ overflowX: "auto", margin: "8px 0" }}>
        <table
          {...props}
          style={{
            borderCollapse: "collapse",
            width: "100%",
            fontSize: "12px",
          }}
        >
          {children}
        </table>
      </div>
    ),
    th: ({ children, ...props }: any) => (
      <th
        {...props}
        style={{
          border: `1px solid ${t.divider}`,
          padding: "6px 8px",
          background: t.codeBg || t.fieldBg,
          fontWeight: 600,
          textAlign: "left",
          fontSize: "11px",
        }}
      >
        {children}
      </th>
    ),
    td: ({ children, ...props }: any) => (
      <td
        {...props}
        style={{
          border: `1px solid ${t.divider}`,
          padding: "5px 8px",
          fontSize: "12px",
        }}
      >
        {children}
      </td>
    ),
    // Horizontal rules
    hr: (props: any) => (
      <hr
        {...props}
        style={{
          border: "none",
          borderTop: `1px solid ${t.divider}`,
          margin: "12px 0",
        }}
      />
    ),
    // Strong / emphasis
    strong: ({ children, ...props }: any) => (
      <strong {...props} style={{ fontWeight: 600, color: t.boldColor || t.text }}>
        {children}
      </strong>
    ),
    em: ({ children, ...props }: any) => (
      <em {...props} style={{ color: t.italicColor || t.text }}>
        {children}
      </em>
    ),
  };
}

// ---------------------------------------------------------------------------
// Date formatting helper
// ---------------------------------------------------------------------------

function formatDate(isoString: string, lang?: string): string {
  const date = new Date(isoString);
  if (isNaN(date.getTime())) return isoString;

  if (lang?.startsWith("zh")) {
    return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")} ${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;
  }

  return date.toLocaleDateString("en", {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export default NotificationDetail;
