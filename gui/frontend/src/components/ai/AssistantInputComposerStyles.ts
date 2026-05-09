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
    promptStyle: CSSProperties;
    textareaStyle: CSSProperties;
    inputActionsStyle: CSSProperties;
} {
    return {
        inputBarStyle: {
            display: "flex",
            flexDirection: "column",
            gap: "6px",
            padding: "8px 12px",
            paddingBottom: "max(8px, env(safe-area-inset-bottom))",
            background: t.inputBarBg,
            borderTop: inline ? `1px solid ${t.inputBarBorder}` : "none",
            flex: isExpandedInput ? "1 1 auto" : undefined,
            flexShrink: 0,
            minWidth: 0,
            minHeight: "76px",
            boxSizing: "border-box",
            overflow: "hidden",
            ...(inline ? {} : { margin: "0 10px 10px 10px", borderRadius: "8px", border: `1.5px solid ${t.inputBarBorder}` }),
        },
        inputRowStyle: {
            display: "flex",
            alignItems: isExpandedInput ? "flex-start" : "flex-end",
            gap: "8px",
            flex: isExpandedInput ? 1 : undefined,
            minHeight: 0,
        },
        promptStyle: {
            color: t.promptColor,
            fontFamily: "Consolas, monospace",
            fontSize: "13px",
            flexShrink: 0,
            userSelect: "none",
            paddingTop: isExpandedInput ? "8px" : 0,
            paddingBottom: isExpandedInput ? 0 : "8px",
        },
        textareaStyle: {
            flex: 1,
            minWidth: 0,
            height: isExpandedInput ? "100%" : undefined,
            background: "transparent",
            border: "none",
            outline: "none",
            color: t.inputText,
            fontFamily: "Consolas, 'Courier New', monospace",
            fontSize: "14px",
            padding: "8px 0",
            resize: "none",
            overflow: "auto",
            minHeight: "36px",
            maxHeight: isExpandedInput ? "none" : "120px",
            lineHeight: 1.4,
            opacity: (!ready || cancelPending) ? 0.5 : 1,
            cursor: cancelPending ? "default" : "text",
        },
        inputActionsStyle: {
            display: "flex",
            alignItems: "center",
            gap: "8px",
            flexShrink: 0,
            paddingTop: isExpandedInput ? "2px" : 0,
            paddingBottom: isExpandedInput ? 0 : "4px",
        },
    };
}
