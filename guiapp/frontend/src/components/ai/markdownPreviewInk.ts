import { relativeLuminance } from "./aiAssistantPanelTheme";

/**
 * Neutral black/gray inks for markdown document preview.
 * Scheme blues stay on chrome and source highlighting; document prose should read as print.
 */
export interface MarkdownPreviewInk {
    /** Regular paragraph / list / table body. */
    body: string;
    /** Headings, bold, table headers, definition terms. */
    emphasis: string;
    /** Quotes, captions, secondary labels. */
    muted: string;
    /** Links — same family as body, distinguished by underline. */
    link: string;
    /** Heading underlines, hr, quote bar, table/code borders. */
    rule: string;
    /** Inline code, fenced code, table header / stripe, highlight fill. */
    wash: string;
}

const LIGHT_INK: MarkdownPreviewInk = {
    body: "#3f3f3f",
    emphasis: "#111111",
    muted: "#6b6b6b",
    link: "#1a1a1a",
    rule: "#d0d0d0",
    wash: "#f3f3f3",
};

const DARK_INK: MarkdownPreviewInk = {
    body: "#c8c8c8",
    emphasis: "#f5f5f5",
    muted: "#9a9a9a",
    link: "#e8e8e8",
    rule: "#3a3a3a",
    wash: "#2a2a2a",
};

export function markdownPreviewInk(isDark: boolean): MarkdownPreviewInk {
    return isDark ? DARK_INK : LIGHT_INK;
}

/** Prefer an explicit flag; otherwise infer from background luminance. */
export function markdownPreviewIsDark(bg: string, explicit?: boolean): boolean {
    if (explicit === true) return true;
    if (explicit === false) return false;
    const lum = relativeLuminance(bg);
    return lum != null ? lum < 0.45 : false;
}

export function markdownPreviewInkFromBg(bg: string, isDark?: boolean): MarkdownPreviewInk {
    return markdownPreviewInk(markdownPreviewIsDark(bg, isDark));
}

/** Theme fields the markdown document renderer recolors. Chrome should keep the original theme. */
export type MarkdownProseSurface = {
    bg: string;
    isDark?: boolean;
    text: string;
    textMuted: string;
    headingColor: string;
    linkColor: string;
    quoteText: string;
    quoteBorder: string;
    quoteBg: string;
    codeBg: string;
    codeText: string;
    codeBlockBg: string;
    codeBlockBorder: string;
    headerBg: string;
    border: string;
    accentColor?: string;
    accentBg?: string;
    accentText?: string;
};

export function withMarkdownProseTheme<T extends MarkdownProseSurface>(theme: T): T {
    const ink = markdownPreviewInkFromBg(theme.bg, theme.isDark);
    return {
        ...theme,
        text: ink.body,
        headingColor: ink.emphasis,
        linkColor: ink.link,
        quoteText: ink.muted,
        quoteBorder: ink.rule,
        quoteBg: ink.wash,
        codeBg: ink.wash,
        codeText: ink.body,
        codeBlockBg: ink.wash,
        codeBlockBorder: ink.rule,
        headerBg: ink.wash,
        textMuted: ink.muted,
        border: ink.rule,
        accentColor: ink.emphasis,
        accentBg: ink.wash,
        accentText: ink.emphasis,
    };
}
