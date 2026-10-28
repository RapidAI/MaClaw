import React from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { BrowserOpenURL } from "../../../wailsjs/runtime";
import type { AdminNotification } from "./useNotifications";
import { companionHostTransferToken, companionShareToken } from "./AssistantTitleBarNotifications.companionShare";

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------

export interface NotificationDetailProps {
  notification: AdminNotification;
  onBack: () => void;
  onClose?: () => void;
  lang?: string;
  onGroupInviteAction?: (groupId: string, action: "accept" | "decline" | "ignore") => void;
  groupInviteBusy?: boolean;
  groupInviteHold?: string;
  groupInviteActionError?: string;
  groupInviteError?: string;
  /** Opens the share delivered by this notice in the companion reader. */
  onShareOpen?: (token: string) => void;
  shareOpenBusy?: boolean;
  shareOpenError?: string;
  /** Accepts or declines a pending host transfer for this share. */
  onHostTransfer?: (token: string, decision: "accept" | "decline") => void;
  hostTransferBusy?: boolean;
  hostTransferError?: string;
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
// Category display. Colors live in CSS (notification-cat--*) so the pill
// follows the active theme like every other status chip in the app.
// ---------------------------------------------------------------------------

const CATEGORY_LABELS: Record<AdminNotification["category"], { en: string; zh: string }> = {
  system_announcement: { en: "System Announcement", zh: "系统公告" },
  feature_update: { en: "Feature Update", zh: "功能更新" },
  security_alert: { en: "Security Alert", zh: "安全警报" },
  maintenance: { en: "Maintenance", zh: "运维通知" },
  custom: { en: "Custom", zh: "自定义" },
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
  lang,
  onGroupInviteAction,
  groupInviteBusy = false,
  groupInviteHold = "",
  groupInviteActionError = "",
  groupInviteError = "",
  onShareOpen,
  shareOpenBusy = false,
  shareOpenError = "",
  onHostTransfer,
  hostTransferBusy = false,
  hostTransferError = "",
}) => {
  const isZh = lang?.startsWith("zh");
  const catLabel = CATEGORY_LABELS[notification.category] || CATEGORY_LABELS.custom;
  const shareToken = companionShareToken(notification);
  const hostTransferToken = companionHostTransferToken(notification);

  const formattedDate = formatDate(notification.created_at, lang);
  const titleText = (notification.title || "").trim();
  const bodyText = (notification.content || "").trim();
  const hasDistinctBody = Boolean(bodyText) && bodyText !== titleText;

  return (
    <div
      data-testid="notification-detail"
      className="ndet-root"
    >
      {/* Header with back button */}
      <div className="ndet-head">
        <button
          data-testid="notification-detail-back"
          onClick={onBack}
          className="ndet-back"
          aria-label={localizeText(lang, "Back to list", "返回列表", "返回列表")}
        >
          <span className="ndet-back-arrow" aria-hidden="true">←</span>
          <span>{localizeText(lang, "Back", "返回", "返回")}</span>
        </button>
        {onClose && (
          <button
            data-testid="notification-detail-close"
            onClick={onClose}
            className="ndet-close"
            aria-label={localizeText(lang, "Close", "关闭", "關閉")}
          >
            ×
          </button>
        )}
      </div>

      {/* Content area */}
      <div className="ndet-content">
        {/* Title */}
        <h3 className="ndet-title">
          {notification.title}
        </h3>

        {/* Meta row: category + date + priority */}
        <div className="ndet-meta">
          {/* Category pill */}
          <span className={`ndet-cat notification-cat--${notification.category}`}>
            {isZh ? catLabel.zh : catLabel.en}
          </span>

          {/* Priority badge */}
          {notification.priority !== "normal" && (
            <span className={`ndet-priority ndet-priority--${notification.priority}`}>
              {notification.priority === "urgent"
                ? localizeText(lang, "Urgent", "紧急", "緊急")
                : localizeText(lang, "Important", "重要", "重要")}
            </span>
          )}

          {/* Date */}
          <span className="ndet-date">
            {formattedDate}
          </span>
        </div>

        {(notification.groupInviteId || shareToken || hostTransferToken) && bodyText ? (
          <div data-testid="notification-detail-content" className="ndet-invite-body">
            {bodyText}
          </div>
        ) : hasDistinctBody ? (
          <div
            data-testid="notification-detail-content"
            className="notification-markdown-content"
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
            className="ndet-empty"
          >
            {localizeText(lang, "No additional details.", "没有更多正文。", "沒有更多正文。")}
          </div>
        ) : null}
        {notification.groupInviteId && onGroupInviteAction ? (
          <div className="ndet-invite-actions">
            <button
              type="button"
              className="ndet-invite-accept"
              data-testid="notification-group-invite-accept"
              disabled={groupInviteBusy}
              aria-busy={groupInviteBusy}
              onClick={() => onGroupInviteAction(notification.groupInviteId || "", "accept")}
            >
              {localizeText(lang, "Accept", "接受", "接受")}
            </button>
            <button
              type="button"
              className="ndet-invite-decline"
              data-testid="notification-group-invite-decline"
              disabled={groupInviteBusy}
              aria-busy={groupInviteBusy}
              onClick={() => onGroupInviteAction(notification.groupInviteId || "", "decline")}
            >
              {localizeText(lang, "Decline", "拒绝", "拒絕")}
            </button>
            <button
              type="button"
              className="ndet-invite-ignore"
              data-testid="notification-group-invite-ignore"
              disabled={groupInviteBusy}
              aria-busy={groupInviteBusy}
              onClick={() => onGroupInviteAction(notification.groupInviteId || "", "ignore")}
            >
              {localizeText(lang, "Ignore", "忽略", "忽略")}
            </button>
          </div>
        ) : null}
        {notification.groupInviteId && groupInviteHold ? (
          <p className="ndet-invite-note" data-testid="notification-group-invite-hold" role="status">{groupInviteHold}</p>
        ) : null}
        {notification.groupInviteId && groupInviteActionError.trim() ? (
          <p className="ndet-invite-error" data-testid="notification-group-invite-action-error" role="status">{groupInviteActionError.trim()}</p>
        ) : null}
        {notification.groupInviteId && groupInviteError ? (
          <p className="ndet-invite-error" data-testid="notification-group-invite-error" role="status">{groupInviteError}</p>
        ) : null}
        {shareToken && onShareOpen ? (
          <div className="ndet-invite-actions">
            <button
              type="button"
              className="ndet-invite-accept"
              data-testid="notification-share-open"
              disabled={shareOpenBusy}
              aria-busy={shareOpenBusy}
              onClick={() => onShareOpen?.(shareToken)}
            >
              {shareOpenBusy
                ? localizeText(lang, "Opening…", "打开中…", "開啟中…")
                : localizeText(lang, "Open", "打开", "開啟")}
            </button>
          </div>
        ) : null}
        {shareToken && shareOpenError.trim() ? (
          <p className="ndet-invite-error" data-testid="notification-share-error" role="alert">{shareOpenError.trim()}</p>
        ) : null}
        {hostTransferToken && onHostTransfer ? (
          <div className="ndet-invite-actions">
            <button
              type="button"
              className="ndet-invite-accept"
              data-testid="notification-host-transfer-accept"
              disabled={hostTransferBusy}
              aria-busy={hostTransferBusy}
              onClick={() => onHostTransfer(hostTransferToken, "accept")}
            >
              {localizeText(lang, "Accept", "接受", "接受")}
            </button>
            <button
              type="button"
              className="ndet-invite-decline"
              data-testid="notification-host-transfer-decline"
              disabled={hostTransferBusy}
              aria-busy={hostTransferBusy}
              onClick={() => onHostTransfer(hostTransferToken, "decline")}
            >
              {localizeText(lang, "Decline", "拒绝", "拒絕")}
            </button>
          </div>
        ) : null}
        {hostTransferToken && hostTransferError.trim() ? (
          <p className="ndet-invite-error" data-testid="notification-host-transfer-error" role="alert">{hostTransferError.trim()}</p>
        ) : null}
      </div>
    </div>
  );
};

// ---------------------------------------------------------------------------
// Markdown components. Every element is styled by the ndet-* classes in
// 90-im-channels-markdown.css, so announcements inherit the app typography
// and repaint correctly when the theme changes.
// ---------------------------------------------------------------------------

const markdownComponents = {
  // Links open externally
  a: ({ href, children }: any) => {
    const safeHref = typeof href === "string" && /^(https?:\/\/|mailto:)/i.test(href) ? href : "";
    if (!safeHref) {
      return <span>{children}</span>;
    }
    return (
      <a
        href={safeHref}
        className="ndet-link"
        onClick={(event) => {
          event.preventDefault();
          BrowserOpenURL(safeHref);
        }}
      >
        {children}
      </a>
    );
  },
  // Do not fetch remote images from announcement markdown.
  img: ({ alt }: any) =>
    alt ? <span className="ndet-img-alt">[{alt}]</span> : null,
  // Code. Keep the language-* class react-markdown supplies with fenced blocks.
  code: ({ inline, className, children, ...props }: any) => {
    const merged = `${inline ? "ndet-code" : "ndet-codeblock"}${className ? ` ${className}` : ""}`;
    return (
      <code {...props} className={merged}>
        {children}
      </code>
    );
  },
  // Block quotes
  blockquote: ({ children, ...props }: any) => (
    <blockquote {...props} className="ndet-quote">
      {children}
    </blockquote>
  ),
  // Headings
  h1: ({ children, ...props }: any) => (
    <h1 {...props} className="ndet-h1">{children}</h1>
  ),
  h2: ({ children, ...props }: any) => (
    <h2 {...props} className="ndet-h2">{children}</h2>
  ),
  h3: ({ children, ...props }: any) => (
    <h3 {...props} className="ndet-h3">{children}</h3>
  ),
  // Paragraphs
  p: ({ children, ...props }: any) => (
    <p {...props} className="ndet-p">
      {children}
    </p>
  ),
  // Lists
  ul: ({ children, ...props }: any) => (
    <ul {...props} className="ndet-list">
      {children}
    </ul>
  ),
  ol: ({ children, ...props }: any) => (
    <ol {...props} className="ndet-list">
      {children}
    </ol>
  ),
  li: ({ children, ...props }: any) => (
    <li {...props} className="ndet-li">
      {children}
    </li>
  ),
  // Tables (GFM)
  table: ({ children, ...props }: any) => (
    <div className="ndet-table-scroll">
      <table {...props} className="ndet-table">
        {children}
      </table>
    </div>
  ),
  th: ({ children, ...props }: any) => (
    <th {...props} className="ndet-th">
      {children}
    </th>
  ),
  td: ({ children, ...props }: any) => (
    <td {...props} className="ndet-td">
      {children}
    </td>
  ),
  // Horizontal rules
  hr: (props: any) => (
    <hr {...props} className="ndet-hr" />
  ),
};

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
