import type { CSSProperties } from "react";
import type { Theme } from "./aiAssistantPanelTheme";

/** Shared with the floating chat card and the welcome workbench field. */
export const ASSISTANT_COMPOSER_RADIUS = "14px";

interface ComposerStyleOptions {
    cancelPending: boolean;
    hasInputOverlay: boolean;
    inline: boolean;
    /**
     * When true (composer docks above the panel quick-settings bar — main chat
     * and VE/group tabs), drop the floating card's bottom margin/safe-area so
     * the bar owns the window bottom edge.
     * When false (standalone / no footer chrome), keep bottom breathing room.
     */
    flushBottom?: boolean;
    isExpandedInput: boolean;
    ready: boolean;
    theme: Theme;
}

export function getAssistantInputComposerStyles({
    cancelPending,
    hasInputOverlay,
    inline,
    flushBottom = false,
    isExpandedInput,
    ready,
    theme: t,
}: ComposerStyleOptions): {
    inputBarStyle: CSSProperties;
    inputRowStyle: CSSProperties;
    textareaStyle: CSSProperties;
    toolbarStyle: CSSProperties;
    toolbarLeftStyle: CSSProperties;
    toolbarRightStyle: CSSProperties;
} {
    // Safe-area belongs on window-bottom chrome. Welcome (inline, mid-page)
    // and flushBottom (footer owns the inset) keep a fixed inner pad only.
    const paddingBottom = (flushBottom || inline)
        ? "6px"
        : "max(6px, env(safe-area-inset-bottom))";
    const fieldBorder = `1.5px solid ${t.inputBarBorder}`;

    return {
        inputBarStyle: {
            display: "flex",
            flexDirection: "column",
            gap: "0px",
            padding: "8px 12px",
            paddingBottom,
            background: t.inputBarBg,
            flex: isExpandedInput ? "1 1 auto" : undefined,
            flexShrink: 0,
            minWidth: 0,
            minHeight: "56px",
            boxSizing: "border-box",
            overflow: hasInputOverlay ? "visible" : "hidden",
            ["--wails-draggable" as any]: "no-drag",
            ...(inline ? {
                width: "100%",
                borderRadius: ASSISTANT_COMPOSER_RADIUS,
                border: fieldBorder,
            } : {
                margin: flushBottom ? "0 10px 0 10px" : "0 10px 10px 10px",
                borderRadius: flushBottom
                    ? `${ASSISTANT_COMPOSER_RADIUS} ${ASSISTANT_COMPOSER_RADIUS} 0 0`
                    : ASSISTANT_COMPOSER_RADIUS,
                border: fieldBorder,
                borderBottom: flushBottom ? "none" : undefined,
                boxShadow: t.bg.startsWith("#0")
                    ? "0 2px 12px rgba(0, 0, 0, 0.32), 0 1px 4px rgba(0, 0, 0, 0.18)"
                    : "0 2px 8px rgba(0, 0, 0, 0.08), 0 1px 2px rgba(0, 0, 0, 0.04)",
                transition: "border-color 140ms cubic-bezier(0.22, 1, 0.36, 1), box-shadow 140ms cubic-bezier(0.22, 1, 0.36, 1)",
            }),
        },
        inputRowStyle: {
            display: "flex",
            alignItems: "flex-start",
            gap: "0px",
            flex: isExpandedInput ? 1 : undefined,
            minHeight: 0,
            overflow: hasInputOverlay ? "visible" : "hidden",
            ["--wails-draggable" as any]: "no-drag",
        },
        textareaStyle: {
            flex: 1,
            minWidth: 0,
            width: "100%",
            boxSizing: "border-box",
            height: isExpandedInput ? "100%" : undefined,
            background: "transparent",
            border: "none",
            outline: "none",
            color: t.inputText,
            fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', sans-serif",
            fontSize: "14px",
            padding: "4px 4px 6px 4px",
            resize: "none",
            overflow: "auto",
            minHeight: "74px",
            maxHeight: isExpandedInput ? "none" : "120px",
            lineHeight: 1.5,
            opacity: (!ready || cancelPending) ? 0.5 : 1,
            cursor: cancelPending ? "default" : "text",
            ["--wails-draggable" as any]: "no-drag",
        },
        toolbarStyle: {
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            gap: "4px",
            padding: "2px 0 0 0",
            minHeight: "24px",
            flexShrink: 0,
        },
        toolbarLeftStyle: {
            display: "flex",
            alignItems: "center",
            gap: "6px",
        },
        toolbarRightStyle: {
            display: "flex",
            alignItems: "center",
            gap: "6px",
        },
    };
}
