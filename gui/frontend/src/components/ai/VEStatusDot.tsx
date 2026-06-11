/**
 * VEStatusDot — 数字员工在线状态统一视觉组件
 *
 * 三态模型：
 * - online:  绿色实心圆点（确认在线）
 * - offline: 灰色空心圆圈（确认离线）
 * - unknown: 橙色脉冲圆点（状态不确定）
 */

import { CSSProperties, useEffect } from "react";

export type VEOnlineStatus = "online" | "offline" | "unknown";

interface VEStatusDotProps {
    status: VEOnlineStatus;
    /** Dot diameter in px. Default 8 */
    size?: number;
    /** "dot" = plain circle; "badge" = with border for avatar overlay */
    variant?: "dot" | "badge";
    style?: CSSProperties;
}

const STATUS_COLORS: Record<VEOnlineStatus, string> = {
    online: "#4f7f6f",
    offline: "#6b7280",
    unknown: "#64748b",
};

// Keyframes ID for deduplication in the DOM
const KEYFRAMES_ID = "ve-status-pulse-keyframes";

export function VEStatusDot({ status, size = 8, variant = "dot", style }: VEStatusDotProps) {
    const color = STATUS_COLORS[status];
    const isOffline = status === "offline";
    const isUnknown = status === "unknown";

    // Inject keyframes stylesheet into <head> once, survives HMR/StrictMode remounts
    useEffect(() => {
        if (!isUnknown) return;
        if (document.getElementById(KEYFRAMES_ID)) return;
        const styleEl = document.createElement("style");
        styleEl.id = KEYFRAMES_ID;
        styleEl.textContent = `
            @keyframes ve-status-pulse {
                0%, 100% { opacity: 0.4; }
                50% { opacity: 1; }
            }
        `;
        document.head.appendChild(styleEl);
        // Never remove — keyframes are global and cheap
    }, [isUnknown]);

    // Compute border once based on both status and variant
    const computeBorder = (): string => {
        if (variant === "badge") {
            return isOffline
                ? `1.5px solid ${color}`
                : `1.5px solid var(--theme-page-bg, #1a1a2e)`;
        }
        return isOffline ? `1.5px solid ${color}` : "none";
    };

    const dotStyle: CSSProperties = {
        width: variant === "badge" ? size + 2 : size,
        height: variant === "badge" ? size + 2 : size,
        borderRadius: "50%",
        flexShrink: 0,
        background: isOffline ? "transparent" : color,
        border: computeBorder(),
        ...(isUnknown && {
            animation: "ve-status-pulse 1.5s ease-in-out infinite",
        }),
        ...style,
    };

    return <span style={dotStyle} data-ve-status={status} />;
}
