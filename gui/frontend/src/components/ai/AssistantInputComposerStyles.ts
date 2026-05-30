import type { CSSProperties } from "react";
import type { Theme } from "./aiAssistantPanelTheme";

interface ComposerStyleOptions {
    cancelPending: boolean;
    inline: boolean;
    isExpandedInput: boolean;
    ready: boolean;
    theme: Theme;
}

export function getAssistantInputComposerStyles({ cancelPending, inline, isExpandedInput, ready, theme: t }: ComposerStyleOptions): {
    inputBarStyle: CSSProperties;
    inputRowStyle: CSSProperties;
    textareaStyle: CSSProperties;
    toolbarStyle: CSSProperties;
    toolbarLeftStyle: CSSProperties;
    toolbarRightStyle: CSSProperties;
} {
    return {
        inputBarStyle: {
            display: "flex",
            flexDirection: "column",
            gap: "0px",
            padding: "8px 12px",
            paddingBottom: "max(6px, env(safe-area-inset-bottom))",
            background: t.inputBarBg,
            borderTop: inline ? `1px solid ${t.inputBarBorder}` : "none",
            flex: isExpandedInput ? "1 1 auto" : undefined,
            flexShrink: 0,
            minWidth: 0,
            minHeight: "56px",
            boxSizing: "border-box",
            overflow: "hidden",
            ...(inline ? {} : {
                margin: "0 10px 10px 10px",
                borderRadius: "12px",
                border: `1.5px solid ${t.inputBarBorder}`,
                boxShadow: t.bg.startsWith("#0")
                    ? "0 2px 12px rgba(0, 0, 0, 0.32), 0 1px 4px rgba(0, 0, 0, 0.18)"
                    : "0 2px 8px rgba(0, 0, 0, 0.08), 0 1px 2px rgba(0, 0, 0, 0.04)",
            }),
        },
        inputRowStyle: {
            display: "flex",
            alignItems: "flex-start",
            gap: "0px",
            flex: isExpandedInput ? 1 : undefined,
            minHeight: 0,
            overflow: "hidden",
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
            minHeight: "32px",
            maxHeight: isExpandedInput ? "none" : "120px",
            lineHeight: 1.5,
            opacity: (!ready || cancelPending) ? 0.5 : 1,
            cursor: cancelPending ? "default" : "text",
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
            gap: "2px",
        },
        toolbarRightStyle: {
            display: "flex",
            alignItems: "center",
            gap: "6px",
        },
    };
}
