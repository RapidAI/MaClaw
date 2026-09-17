import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type KeyboardEvent as ReactKeyboardEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import type { Theme } from "../aiAssistantPanelTheme";
import { useSafeBackdropDismiss } from "../../../hooks/useSafeBackdropDismiss";

/** 弹层列表项的键盘激活：Enter / Space 触发 onPick（配合 tabIndex={0}）。 */
export function popoverItemKeyDown(onPick?: () => void): (event: ReactKeyboardEvent<HTMLElement>) => void {
    return (event) => {
        if (!onPick) return;
        if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            onPick();
        }
    };
}

/**
 * z-index 约定与 WelcomePromptParamDialog 一致：高于应用外壳
 * （~50k–99k），低于创建任务弹窗（100000）。
 */
export const TASK_CONFIG_POPOVER_Z_INDEX = 90000;

function getPortalThemeAttrs(): {
    "data-ai-theme"?: string;
    "data-ai-dark-scheme"?: string;
    "data-ai-light-scheme"?: string;
} {
    const app = typeof document !== "undefined" ? document.getElementById("App") : null;
    if (!app) return {};
    return {
        "data-ai-theme": app.getAttribute("data-ai-theme") || undefined,
        "data-ai-dark-scheme": app.getAttribute("data-ai-dark-scheme") || undefined,
        "data-ai-light-scheme": app.getAttribute("data-ai-light-scheme") || undefined,
    };
}

export interface TaskConfigPopoverShellProps {
    /** 触发 chip（用于定位弹层）。 */
    anchor: HTMLElement | null;
    theme: Theme;
    onClose: () => void;
    width?: number;
    "data-testid"?: string;
    children: ReactNode;
}

/**
 * 配置条弹层外壳：portal 到 body、透明全屏背景（点外部关闭）、Esc 关闭、
 * 向下弹出并自动夹取在视口内。进入动画见 App.css 的 .mc-taskcfg-pop。
 */
export function TaskConfigPopoverShell({
    anchor,
    theme: t,
    onClose,
    width = 448,
    "data-testid": testId,
    children,
}: TaskConfigPopoverShellProps) {
    const panelRef = useRef<HTMLDivElement | null>(null);
    const [pos, setPos] = useState<{ left: number; top: number } | null>(null);
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose);

    const GAP = 8;

    // First pass: horizontal clamp only; the panel renders offscreen (-9999).
    useLayoutEffect(() => {
        if (!anchor || typeof window === "undefined") return;
        const rect = anchor.getBoundingClientRect();
        const vw = window.innerWidth || 1024;
        const left = Math.min(Math.max(GAP, rect.left), Math.max(GAP, vw - width - GAP));
        setPos({ left, top: rect.bottom + GAP });
    }, [anchor, width]);

    // Second pass: measure the panel, flip above the anchor when there is no
    // room below, and clamp vertically inside the viewport.
    useLayoutEffect(() => {
        const panel = panelRef.current;
        if (!panel || !anchor || pos === null) return;
        const rect = anchor.getBoundingClientRect();
        const vh = window.innerHeight || 768;
        const height = panel.offsetHeight || 0;
        let top = pos.top;
        if (top + height > vh - GAP && rect.top - height - GAP >= GAP) {
            top = rect.top - height - GAP;
        } else {
            top = Math.min(top, Math.max(GAP, vh - height - GAP));
        }
        if (top !== pos.top) setPos({ left: pos.left, top });
    }, [pos, anchor]);

    useEffect(() => {
        const onKey = (event: KeyboardEvent) => {
            if (event.key === "Escape") {
                event.preventDefault();
                event.stopPropagation();
                onClose();
            }
        };
        document.addEventListener("keydown", onKey, true);
        return () => document.removeEventListener("keydown", onKey, true);
    }, [onClose]);

    useEffect(() => {
        panelRef.current?.focus();
    }, []);

    if (typeof document === "undefined") return null;

    const backdropStyle: CSSProperties = {
        position: "fixed",
        inset: 0,
        zIndex: TASK_CONFIG_POPOVER_Z_INDEX,
        background: "transparent",
    };

    const panelStyle: CSSProperties = {
        position: "fixed",
        left: pos?.left ?? -9999,
        top: pos?.top ?? -9999,
        width,
        maxWidth: "calc(100vw - 16px)",
        maxHeight: "min(70vh, 480px)",
        display: "flex",
        flexDirection: "column",
        background: t.bg,
        color: t.text,
        border: `1px solid ${t.fieldBorder}`,
        borderRadius: 14,
        boxShadow: "0 10px 32px rgba(26, 35, 51, 0.14)",
        overflow: "hidden",
        outline: "none",
        textAlign: "left",
    };

    const panel = (
        <div style={backdropStyle} {...backdropProps} {...getPortalThemeAttrs()}>
            <div
                ref={panelRef}
                role="dialog"
                tabIndex={-1}
                className="mc-taskcfg-pop"
                style={panelStyle}
                {...dialogProps}
                data-testid={testId}
            >
                {children}
            </div>
        </div>
    );

    return createPortal(panel, document.body);
}

/** 弹层内搜索输入框样式（专家 / 工作流选项多时过滤用）。 */
export function popoverSearchInputStyle(t: Theme): CSSProperties {
    return {
        width: "100%",
        boxSizing: "border-box",
        height: 32,
        borderRadius: 9,
        border: `1px solid ${t.fieldBorder}`,
        background: t.fieldBg,
        color: t.inputText || t.text,
        padding: "0 10px",
        fontSize: 13,
        outline: "none",
        fontFamily: "system-ui, -apple-system, sans-serif",
    };
}
