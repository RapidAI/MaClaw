import type { CSSProperties } from "react";
import type { Theme } from "./aiAssistantPanelTheme";

/**
 * NotificationBell — 铃铛图标组件
 *
 * 位置：AI 助手面板顶部标题栏区域
 * 功能：
 *   - Bell icon + CSS blink animation when unreadCount > 0
 *   - 未读计数 badge（红点，最大显示 10+）
 *   - 点击触发 panelOpen toggle
 *
 * Validates: Requirements FR-4
 */

export interface NotificationBellProps {
    /** Number of unread notifications (0 means no animation/badge). */
    unreadCount: number;
    /** Callback when the bell is clicked to toggle the notification panel. */
    onClick: () => void;
    /** Current theme for consistent styling. */
    theme: Theme;
    /** Whether the parent is in inline (Wails drag) mode. */
    inline?: boolean;
}

/** Bell SVG icon rendered inline for zero external dependencies. */
function BellIcon({ animate, theme }: { animate: boolean; theme: Theme }) {
    const style: CSSProperties = {
        display: "block",
        width: "15px",
        height: "15px",
        transition: "transform 200ms ease",
        ...(animate
            ? { animation: "notification-bell-ring 1.5s ease-in-out infinite" }
            : {}),
    };
    return (
        <svg
            xmlns="http://www.w3.org/2000/svg"
            viewBox="0 0 24 24"
            fill="none"
            stroke={theme.actionBtnColor}
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            style={style}
            aria-hidden="true"
            focusable="false"
        >
            <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9" />
            <path d="M13.73 21a2 2 0 0 1-3.46 0" />
        </svg>
    );
}

/** CSS keyframes injected once into the document head. */
let keyframesInjected = false;
function ensureKeyframes() {
    if (keyframesInjected) return;
    keyframesInjected = true;
    const style = document.createElement("style");
    style.textContent = `
@keyframes notification-bell-ring {
  0% { transform: rotate(0deg); }
  10% { transform: rotate(14deg); }
  20% { transform: rotate(-12deg); }
  30% { transform: rotate(10deg); }
  40% { transform: rotate(-8deg); }
  50% { transform: rotate(4deg); }
  60% { transform: rotate(0deg); }
  100% { transform: rotate(0deg); }
}
`;
    document.head.appendChild(style);
}

export function NotificationBell({
    unreadCount,
    onClick,
    theme,
    inline,
}: NotificationBellProps) {
    // Inject keyframes on first render with animation
    if (unreadCount > 0) {
        ensureKeyframes();
    }

    const shouldAnimate = unreadCount > 0;
    const displayCount = Math.min(unreadCount, 10);

    const buttonStyle: CSSProperties = {
        position: "relative",
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        width: "24px",
        height: "24px",
        padding: 0,
        border: "none",
        borderRadius: "4px",
        background: "transparent",
        cursor: "pointer",
        transition: "background 150ms ease",
        ["--ai-titlebar-tool-hover-bg" as any]: "rgba(148, 163, 184, 0.12)",
        ...(inline
            ? ({ "--wails-draggable": "no-drag" } as CSSProperties)
            : {}),
    };

    const badgeStyle: CSSProperties = {
        position: "absolute",
        top: "2px",
        right: "1px",
        minWidth: "14px",
        height: "14px",
        padding: "0 3px",
        borderRadius: "7px",
        background: "#ef4444",
        color: "#ffffff",
        fontSize: "9px",
        fontWeight: 700,
        lineHeight: "14px",
        textAlign: "center",
        pointerEvents: "none",
        boxShadow: `0 0 0 1.5px ${theme.titleBarBg}`,
        whiteSpace: "nowrap",
    };

    return (
        <button
            className="ai-titlebar-tool notification-bell-btn"
            data-testid="notification-bell-btn"
            onClick={onClick}
            style={buttonStyle}
            aria-label={
                unreadCount > 0
                    ? `${unreadCount} unread notifications`
                    : "Notifications"
            }
            title={
                unreadCount > 0
                    ? `${unreadCount} 条未读通知`
                    : "通知"
            }
        >
            <BellIcon animate={shouldAnimate} theme={theme} />
            {unreadCount > 0 && (
                <span style={badgeStyle} aria-hidden="true">
                    {displayCount}
                    {unreadCount > 10 ? "+" : ""}
                </span>
            )}
        </button>
    );
}
