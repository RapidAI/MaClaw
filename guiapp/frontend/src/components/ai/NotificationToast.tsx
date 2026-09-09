import { useEffect, useRef, useCallback, type CSSProperties } from "react";
import type { Theme } from "./aiAssistantPanelTheme";
import type { AdminNotification } from "./useNotifications";

/**
 * NotificationToast — urgent 通知 Toast/Banner 提醒
 *
 * 类似新版本提醒的 UI 模式：
 * - 自动消失（8 秒）或手动关闭
 * - 显示标题 + 简短内容预览
 * - 点击跳转到通知详情
 * - 以 slide-in 动画从顶部滑入 AI 面板
 *
 * Validates: Requirements FR-4
 */

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/** Auto-dismiss timeout in milliseconds. */
const AUTO_DISMISS_MS = 8000;

/** Maximum characters for content preview. */
const CONTENT_PREVIEW_MAX = 80;

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface NotificationToastProps {
  /** The urgent notification to display. null = hidden. */
  notification: AdminNotification | null;
  /** Callback when user dismisses the toast (close button or auto-dismiss). */
  onDismiss: () => void;
  /** Callback when user clicks the toast body to view full details. */
  onClick: (notification: AdminNotification) => void;
  /** Current theme for styling consistency. */
  theme: Theme;
}

// ---------------------------------------------------------------------------
// CSS Keyframes injection (singleton)
// ---------------------------------------------------------------------------

let toastKeyframesInjected = false;
function ensureToastKeyframes() {
  if (toastKeyframesInjected) return;
  toastKeyframesInjected = true;
  const style = document.createElement("style");
  style.textContent = `
@keyframes notification-toast-slide-in {
  from {
    transform: translateY(-100%);
    opacity: 0;
  }
  to {
    transform: translateY(0);
    opacity: 1;
  }
}
@keyframes notification-toast-slide-out {
  from {
    transform: translateY(0);
    opacity: 1;
  }
  to {
    transform: translateY(-100%);
    opacity: 0;
  }
}
`;
  document.head.appendChild(style);
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Strip Markdown formatting for plain-text preview. */
function stripMarkdown(md: string): string {
  return md
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1") // images first (before links)
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")  // links
    .replace(/[#*_~`>]/g, "")                  // inline formatting chars
    .replace(/\n+/g, " ")
    .trim();
}

/** Truncate text to maxLen with ellipsis. */
function truncateText(text: string, maxLen: number): string {
  if (text.length <= maxLen) return text;
  return text.slice(0, maxLen).trimEnd() + "…";
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export function NotificationToast({
  notification,
  onDismiss,
  onClick,
  theme,
}: NotificationToastProps) {
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const exitingRef = useRef(false);
  const containerRef = useRef<HTMLDivElement | null>(null);

  // Inject keyframes on first render with a notification
  if (notification) {
    ensureToastKeyframes();
  }

  // -------------------------------------------------------------------------
  // Auto-dismiss timer
  // -------------------------------------------------------------------------

  // Store onDismiss in a ref to avoid recreating startDismissTimer on every
  // parent re-render (onDismiss is typically an unstable closure).
  const onDismissRef = useRef(onDismiss);
  onDismissRef.current = onDismiss;

  const startDismissTimer = useCallback(() => {
    if (timerRef.current) clearTimeout(timerRef.current);
    timerRef.current = setTimeout(() => {
      onDismissRef.current();
    }, AUTO_DISMISS_MS);
  }, []);

  useEffect(() => {
    if (!notification) {
      if (timerRef.current) {
        clearTimeout(timerRef.current);
        timerRef.current = null;
      }
      return;
    }

    exitingRef.current = false;
    startDismissTimer();

    return () => {
      if (timerRef.current) {
        clearTimeout(timerRef.current);
        timerRef.current = null;
      }
    };
  }, [notification, startDismissTimer]);

  // -------------------------------------------------------------------------
  // Handlers
  // -------------------------------------------------------------------------

  const handleClick = useCallback(() => {
    if (!notification) return;
    if (timerRef.current) clearTimeout(timerRef.current);
    onClick(notification);
  }, [notification, onClick]);

  const handleClose = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      e.stopPropagation();
      if (timerRef.current) clearTimeout(timerRef.current);
      onDismiss();
    },
    [onDismiss]
  );

  // Pause timer on hover, resume on leave
  const handleMouseEnter = useCallback(() => {
    if (timerRef.current) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  const handleMouseLeave = useCallback(() => {
    if (notification) {
      startDismissTimer();
    }
  }, [notification, startDismissTimer]);

  // -------------------------------------------------------------------------
  // Render
  // -------------------------------------------------------------------------

  if (!notification) return null;

  const contentPreview = truncateText(
    stripMarkdown(notification.content),
    CONTENT_PREVIEW_MAX
  );

  // Styles
  const containerStyle: CSSProperties = {
    position: "absolute",
    top: "40px", // below the title bar
    left: "8px",
    right: "8px",
    zIndex: 30050,
    display: "flex",
    alignItems: "flex-start",
    gap: "10px",
    padding: "12px 14px",
    borderRadius: "8px",
    background: theme.isDark
      ? "linear-gradient(135deg, rgba(220, 38, 38, 0.14) 0%, rgba(30, 30, 44, 0.97) 100%)"
      : "linear-gradient(135deg, rgba(220, 38, 38, 0.08) 0%, rgba(255, 255, 255, 0.98) 100%)",
    border: `1px solid ${
      theme.isDark ? "rgba(220, 38, 38, 0.35)" : "rgba(220, 38, 38, 0.25)"
    }`,
    boxShadow: theme.isDark
      ? "0 8px 24px rgba(0, 0, 0, 0.5), 0 2px 8px rgba(220, 38, 38, 0.15)"
      : "0 8px 24px rgba(0, 0, 0, 0.12), 0 2px 8px rgba(220, 38, 38, 0.08)",
    cursor: "pointer",
    animation: "notification-toast-slide-in 300ms ease-out forwards",
    transition: "opacity 200ms ease, transform 200ms ease",
    // Prevent wails drag
    ["--wails-draggable" as string]: "no-drag",
  };

  const urgentDotStyle: CSSProperties = {
    width: "8px",
    height: "8px",
    borderRadius: "50%",
    background: "#dc2626",
    flexShrink: 0,
    marginTop: "4px",
    boxShadow: "0 0 6px rgba(220, 38, 38, 0.5)",
  };

  const bodyStyle: CSSProperties = {
    flex: 1,
    minWidth: 0,
    display: "flex",
    flexDirection: "column",
    gap: "3px",
  };

  const titleStyle: CSSProperties = {
    fontSize: "12px",
    fontWeight: 600,
    lineHeight: "1.4",
    color: theme.text,
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  };

  const previewStyle: CSSProperties = {
    fontSize: "11px",
    lineHeight: "1.4",
    color: theme.textMuted,
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  };

  const closeBtnStyle: CSSProperties = {
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    width: "20px",
    height: "20px",
    padding: 0,
    border: "none",
    borderRadius: "4px",
    background: "transparent",
    color: theme.textMuted,
    cursor: "pointer",
    flexShrink: 0,
    transition: "background 150ms ease, color 150ms ease",
  };

  return (
    <div
      ref={containerRef}
      className="notification-toast"
      data-testid="notification-toast"
      role="alert"
      aria-live="assertive"
      onClick={handleClick}
      onMouseEnter={handleMouseEnter}
      onMouseLeave={handleMouseLeave}
      style={containerStyle}
    >
      {/* Urgent indicator dot */}
      <span style={urgentDotStyle} aria-hidden="true" />

      {/* Content body */}
      <div style={bodyStyle}>
        <div style={titleStyle} title={notification.title}>
          {notification.title}
        </div>
        {contentPreview && (
          <div style={previewStyle} title={contentPreview}>
            {contentPreview}
          </div>
        )}
      </div>

      {/* Close button */}
      <button
        className="notification-toast-close"
        data-testid="notification-toast-close"
        onClick={handleClose}
        onMouseDown={(e) => e.stopPropagation()}
        aria-label="Close notification"
        title="关闭"
        style={closeBtnStyle}
      >
        <svg
          viewBox="0 0 24 24"
          width="14"
          height="14"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <line x1="18" y1="6" x2="6" y2="18" />
          <line x1="6" y1="6" x2="18" y2="18" />
        </svg>
      </button>
    </div>
  );
}
