import type React from "react";
import type { Theme } from "./aiAssistantPanelTheme";

export const CODE_FENCE_FONT = "ui-monospace, SFMono-Regular, Menlo, Consolas, 'Courier New', monospace";

/**
 * The code element must not pick up the global `:where(pre, code)` wash.
 * `min-width: max-content` keeps a long line inside this fence's scroller
 * instead of stretching the chat column or clipping at the block edge.
 */
export const codeFenceInnerStyle: React.CSSProperties = {
    display: "block",
    minWidth: "max-content",
    background: "transparent",
    border: "none",
    borderRadius: 0,
    padding: 0,
    margin: 0,
    color: "inherit",
    font: "inherit",
    whiteSpace: "inherit",
    // The chat bubble sets overflow-wrap:anywhere and word-break:break-word.
    // Those inherit, and would split a long token instead of scrolling it.
    overflowWrap: "normal",
    wordBreak: "normal",
};

const codeFenceScrollBase: React.CSSProperties = {
    display: "block",
    width: "100%",
    maxWidth: "100%",
    minWidth: 0,
    boxSizing: "border-box",
    margin: 0,
    border: "none",
    borderRadius: 0,
    background: "transparent",
    color: "inherit",
    font: "inherit",
    overflowX: "auto",
    overscrollBehaviorX: "contain",
    whiteSpace: "pre",
    overflowWrap: "normal",
    wordBreak: "normal",
    tabSize: 4,
};

/** Scroll only the source. Inline white-space and overflow-wrap beat the narrow-window pre-wrap rule. */
export const codeFenceScrollStyle: React.CSSProperties = {
    ...codeFenceScrollBase,
    padding: "8px 12px",
};

/** Tighter top padding when a language label already occupies the well's top. */
export const codeFenceScrollUnderLangStyle: React.CSSProperties = {
    ...codeFenceScrollBase,
    padding: "4px 12px 8px",
};

function inkOnPaper(t: Theme, percent: number): string {
    return `color-mix(in srgb, ${t.text} ${percent}%, ${t.bg})`;
}

const fenceBox: React.CSSProperties = {
    width: "100%",
    maxWidth: "100%",
    minWidth: 0,
    boxSizing: "border-box",
    fontFamily: CODE_FENCE_FONT,
};

/**
 * Result-area well. Body ink on a neutral step off the page: light schemes
 * used accent-blue code text on a pale blue wash, so ink and background
 * were the same hue. The well itself does not scroll.
 */
export function chatCodeWellStyle(t: Theme): React.CSSProperties {
    return {
        ...fenceBox,
        background: inkOnPaper(t, 8),
        color: t.text,
        border: `1px solid ${inkOnPaper(t, 22)}`,
        borderRadius: "8px",
        margin: "8px 0",
        fontSize: "13px",
        lineHeight: 1.5,
        overflow: "hidden",
    };
}

export function chatCodeLangStyle(t: Theme): React.CSSProperties {
    return {
        display: "block",
        maxWidth: "100%",
        margin: 0,
        padding: "8px 12px 0",
        border: "none",
        color: t.text,
        fontSize: "12px",
        fontWeight: 600,
        lineHeight: 1.2,
        letterSpacing: "0.04em",
        textTransform: "uppercase",
        whiteSpace: "nowrap",
        overflow: "hidden",
        textOverflow: "ellipsis",
        overflowWrap: "normal",
        wordBreak: "normal",
    };
}

export function reasoningCodeBlockStyle(t: Theme): React.CSSProperties {
    return {
        ...fenceBox,
        background: t.bg,
        color: t.text,
        border: `1px solid ${t.codeBlockBorder}`,
        borderRadius: "8px",
        padding: "8px 10px",
        margin: "6px 0",
        fontSize: "12px",
        lineHeight: 1.45,
        overflowX: "auto",
        overscrollBehaviorX: "contain",
        whiteSpace: "pre",
        overflowWrap: "normal",
        wordBreak: "normal",
        tabSize: 4,
    };
}

export function reasoningCodeLangStyle(t: Theme): React.CSSProperties {
    return {
        display: "block",
        width: "fit-content",
        maxWidth: "100%",
        margin: "0 0 6px",
        color: t.codeBlockLang,
        fontSize: "0.8em",
        fontWeight: 500,
        lineHeight: 1.2,
        textTransform: "uppercase",
        letterSpacing: "0.05em",
        whiteSpace: "nowrap",
        opacity: 0.8,
    };
}
