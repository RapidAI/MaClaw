import React from "react";

/* Theme definitions */

export interface Theme {
    bg: string;
    titleBarBg: string;
    titleBarBorder: string;
    titleText: string;
    text: string;
    textMuted: string;
    inputBarBg: string;
    inputBarBorder: string;
    inputText: string;
    codeBg: string;
    codeText: string;
    codeBlockBg: string;
    codeBlockBorder: string;
    codeBlockLang: string;
    borderLeft: string;
    responseBorderLeft: string;
    headingColor: string;
    linkColor: string;
    pathColor: string;
    promptColor: string;
    userColor: string;
    divider: string;
    fieldBg: string;
    fieldBorder: string;
    fieldLabel: string;
    errorText: string;
    errorBg: string;
    errorBorder: string;
    emptyHint: string;
    boldColor: string;
    italicColor: string;
    bulletColor: string;
    quoteBorder: string;
    quoteText: string;
    btnColor: string;
    btnBorder: string;
    actionBtnColor: string;
    closeBtnColor: string;
    sendBtnColor: string;
    sendBtnBorder: string;
    /** Send button background color — use this for button backgrounds, not sendBtnColor.
     *  sendBtnColor is the button text/foreground color. */
    sendBtnBg: string;
    /** Optional flag indicating dark mode — used by sub-components to adapt contrast. */
    isDark?: boolean;
}

export const overlayTheme: Theme = {
    bg: "#f3f5f8",
    titleBarBg: "#eef2f7",
    titleBarBorder: "#d8dee8",
    titleText: "#334155",
    text: "#1f2937",
    textMuted: "#64748b",
    inputBarBg: "#ffffff",
    inputBarBorder: "#cbd5e1",
    inputText: "#111827",
    codeBg: "#eef2f7",
    codeText: "#334155",
    codeBlockBg: "#f8fafc",
    codeBlockBorder: "#d8dee8",
    codeBlockLang: "#64748b",
    borderLeft: "#d8dee8",
    responseBorderLeft: "#8aa4bf",
    headingColor: "#1f2937",
    linkColor: "#2f5f98",
    pathColor: "#334155",
    promptColor: "#334155",
    userColor: "#334155",
    divider: "#d8dee8",
    fieldBg: "#f8fafc",
    fieldBorder: "#d8dee8",
    fieldLabel: "#64748b",
    errorText: "#c43d34",
    errorBg: "rgba(196, 61, 52, 0.06)",
    errorBorder: "#c43d34",
    emptyHint: "#94a3b8",
    boldColor: "#111827",
    italicColor: "#334155",
    bulletColor: "#64748b",
    quoteBorder: "#b7c5d4",
    quoteText: "#526579",
    btnColor: "#2f5f98",
    btnBorder: "#b7c5d4",
    actionBtnColor: "#64748b",
    closeBtnColor: "#64748b",
    sendBtnColor: "#fff",
    sendBtnBorder: "#2f5f98",
    sendBtnBg: "#2f5f98",
};

export const lightTheme: Theme = {
    bg: "#f7f9fc",
    titleBarBg: "#eef2f7",
    titleBarBorder: "#d8dee8",
    titleText: "#334155",
    text: "#1f2937",
    textMuted: "#64748b",
    inputBarBg: "#ffffff",
    inputBarBorder: "#cbd5e1",
    inputText: "#111827",
    codeBg: "#eef2f7",
    codeText: "#334155",
    codeBlockBg: "#f8fafc",
    codeBlockBorder: "#d8dee8",
    codeBlockLang: "#64748b",
    borderLeft: "#d8dee8",
    responseBorderLeft: "#8aa4bf",
    headingColor: "#1f2937",
    linkColor: "#2f5f98",
    pathColor: "#334155",
    promptColor: "#334155",
    userColor: "#334155",
    divider: "#d8dee8",
    fieldBg: "#f8fafc",
    fieldBorder: "#d8dee8",
    fieldLabel: "#64748b",
    errorText: "#c43d34",
    errorBg: "rgba(196, 61, 52, 0.06)",
    errorBorder: "#c43d34",
    emptyHint: "#94a3b8",
    boldColor: "#111827",
    italicColor: "#334155",
    bulletColor: "#64748b",
    quoteBorder: "#b7c5d4",
    quoteText: "#526579",
    btnColor: "#2f5f98",
    btnBorder: "#b7c5d4",
    actionBtnColor: "#64748b",
    closeBtnColor: "#64748b",
    sendBtnColor: "#ffffff",
    sendBtnBorder: "#2f5f98",
    sendBtnBg: "#2f5f98",
};


export const darkTheme: Theme = {
    ...lightTheme,
    bg: "#0b1220",
    titleBarBg: "#111827",
    titleBarBorder: "#334155",
    titleText: "#f1f5f9",
    text: "#e2e8f0",
    textMuted: "#a8b8c8",
    inputBarBg: "#0f172a",
    inputBarBorder: "#334155",
    inputText: "#e5e7eb",
    codeBg: "#1e293b",
    codeText: "#b7d3ef",
    codeBlockBg: "#0f172a",
    codeBlockBorder: "#1e3a5f",
    codeBlockLang: "#8fb4dc",
    borderLeft: "#334155",
    responseBorderLeft: "#5b7898",
    headingColor: "#d9e7f5",
    linkColor: "#9bc2ea",
    pathColor: "#b7d3ef",
    promptColor: "#b7d3ef",
    userColor: "#c7d7e8",
    divider: "#1e293b",
    fieldBg: "#111827",
    fieldBorder: "#475569",
    fieldLabel: "#cbd5e1",
    errorText: "#e07a72",
    errorBg: "rgba(196, 61, 52, 0.10)",
    errorBorder: "#b95b52",
    emptyHint: "#7a8a9b",
    boldColor: "#f8fafc",
    italicColor: "#e2e8f0",
    bulletColor: "#8a9ab0",
    quoteBorder: "#5b7898",
    quoteText: "#c7d7e8",
    actionBtnColor: "#cbd5e1",
    closeBtnColor: "#cbd5e1",
    btnColor: "#b7d3ef",
    btnBorder: "#5b7898",
    sendBtnColor: "#ffffff",
    sendBtnBorder: "#5b7898",
    sendBtnBg: "#2f5f98",
};

export const AI_THEME_MODE_STORAGE_KEY = "ai_assistant_theme_mode";
export const AI_THEME_MODE_LEGACY_STORAGE_KEY = "maclaw.ai.themeMode";
/* Style constants */

export const overlayStyle: React.CSSProperties = {
    position: "fixed",
    inset: 0,
    zIndex: 10000,
    display: "flex",
    flexDirection: "column",
    background: overlayTheme.bg,
    textAlign: "left",
    boxShadow: "0 0 40px rgba(0,0,0,0.08)",
};

export const maximizedInlineStyle: React.CSSProperties = {
    position: "fixed",
    inset: 0,
    zIndex: 12000,
    display: "flex",
    flexDirection: "column",
    width: "100vw",
    height: "100vh",
    minHeight: 0,
    textAlign: "left",
    boxShadow: "0 0 40px rgba(0,0,0,0.12)",
    overflow: "hidden",
};

export const dotBase: React.CSSProperties = {
    width: 10,
    height: 10,
    borderRadius: "50%",
    display: "inline-block",
    cursor: "pointer",
};

export const baseInputBtnStyle: React.CSSProperties = {
    background: "transparent",
    border: "1px solid",
    borderRadius: "7px",
    padding: 0,
    fontSize: "13px",
    fontFamily: "'Segoe UI Symbol', 'Segoe UI', sans-serif",
    cursor: "pointer",
    lineHeight: 1,
    minHeight: "28px",
    minWidth: "30px",
    width: "30px",
    height: "28px",
    flexShrink: 0,
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    transition: "transform 120ms ease, box-shadow 120ms ease, background 120ms ease, border-color 120ms ease, opacity 120ms ease",
};

export type AssistantInputIconName =
    | "paperclip"
    | "mic"
    | "cornerDownLeft"
    | "stop"
    | "edit"
    | "trash"
    | "eraser"
    | "plus"
    | "target"
    | "search"
    | "repeat"
    | "brain"
    | "compress"
    | "helpCircle"
    | "messagePlus"
    | "layers"
    | "shieldCheck"
    | "alertTriangle"
    | "folder";

export function AssistantInputIcon({ name, size = 17 }: { name: AssistantInputIconName; size?: number }) {
    const common = {
        fill: "none",
        stroke: "currentColor",
        strokeWidth: 2,
        strokeLinecap: "round" as const,
        strokeLinejoin: "round" as const,
    };
    return (
        <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true" focusable="false" style={{ display: "block" }}>
            {name === "paperclip" && (
                <path {...common} d="m21.44 11.05-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 1 1-2.83-2.83l8.49-8.48" />
            )}
            {name === "mic" && (
                <>
                    <path {...common} d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3Z" />
                    <path {...common} d="M19 10v2a7 7 0 0 1-14 0v-2" />
                    <path {...common} d="M12 19v3" />
                </>
            )}
            {name === "cornerDownLeft" && (
                <path {...common} d="M9 10 4 15l5 5M20 4v7a4 4 0 0 1-4 4H4" />
            )}
            {name === "stop" && (
                <rect {...common} x="7" y="7" width="10" height="10" rx="1.8" />
            )}
            {name === "edit" && (
                <>
                    <path {...common} d="M12 20h9" />
                    <path {...common} d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4 12.5-12.5Z" />
                </>
            )}
            {name === "trash" && (
                <>
                    <path {...common} d="M3 6h18" />
                    <path {...common} d="M8 6V4h8v2" />
                    <path {...common} d="M6 6l1 14h10l1-14" />
                    <path {...common} d="M10 11v5" />
                    <path {...common} d="M14 11v5" />
                </>
            )}
            {name === "eraser" && (
                <>
                    <path {...common} d="m7 21-4-4L14.5 5.5a2.8 2.8 0 0 1 4 4L7 21Z" />
                    <path {...common} d="m11 8 5 5" />
                    <path {...common} d="M7 21h10" />
                </>
            )}
            {name === "plus" && (
                <>
                    <path {...common} d="M12 5v14" />
                    <path {...common} d="M5 12h14" />
                </>
            )}
            {/* 目标 /goal — 箭靶 */}
            {name === "target" && (
                <>
                    <circle {...common} cx="12" cy="12" r="9" />
                    <circle {...common} cx="12" cy="12" r="5" />
                    <circle {...common} cx="12" cy="12" r="1.5" fill="currentColor" stroke="none" />
                </>
            )}
            {/* 旁路查询 /btw — 放大镜（侧问/检索） */}
            {name === "search" && (
                <>
                    <circle {...common} cx="11" cy="11" r="7" />
                    <path {...common} d="m20 20-3.5-3.5" />
                </>
            )}
            {/* 验证循环 /loop — 循环箭头 */}
            {name === "repeat" && (
                <>
                    <path {...common} d="m17 2 4 4-4 4" />
                    <path {...common} d="M3 11v-1a4 4 0 0 1 4-4h14" />
                    <path {...common} d="m7 22-4-4 4-4" />
                    <path {...common} d="M21 13v1a4 4 0 0 1-4 4H3" />
                </>
            )}
            {/* 记忆状态 /memory — 简化大脑轮廓（stroke 友好） */}
            {name === "brain" && (
                <>
                    <path {...common} d="M9.5 2a2.5 2.5 0 0 1 2.45 2H12a2.5 2.5 0 0 1 2.45-2 2.5 2.5 0 0 1 2.5 2.5c0 .4-.1.78-.27 1.12A3.5 3.5 0 0 1 19 9a3.5 3.5 0 0 1-1.4 2.75A3 3 0 0 1 17 17H7a3 3 0 0 1-.6-5.25A3.5 3.5 0 0 1 5 9a3.5 3.5 0 0 1 2.32-3.38A2.5 2.5 0 0 1 7 4.5 2.5 2.5 0 0 1 9.5 2Z" />
                    <path {...common} d="M12 5v12" />
                    <path {...common} d="M9 9.5h.01" />
                    <path {...common} d="M15 9.5h.01" />
                    <path {...common} d="M9 13h.01" />
                    <path {...common} d="M15 13h.01" />
                </>
            )}
            {/* 压缩历史 /compress — 双向收拢箭头 */}
            {name === "compress" && (
                <>
                    <path {...common} d="m7 7 5 5 5-5" />
                    <path {...common} d="m7 17 5-5 5 5" />
                    <path {...common} d="M4 12h16" />
                </>
            )}
            {/* 帮助 /help — 问号圆 */}
            {name === "helpCircle" && (
                <>
                    <circle {...common} cx="12" cy="12" r="9" />
                    <path {...common} d="M9.1 9a3 3 0 0 1 5.8 1c0 2-3 2-3 4" />
                    <circle {...common} cx="12" cy="17" r="0.8" fill="currentColor" stroke="none" />
                </>
            )}
            {/* 开始新对话 — 消息气泡 + */}
            {name === "messagePlus" && (
                <>
                    <path {...common} d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
                    <path {...common} d="M12 7v6" />
                    <path {...common} d="M9 10h6" />
                </>
            )}
            {/* 多模型会诊 /moa — 层叠（多视角合成） */}
            {name === "layers" && (
                <>
                    <path {...common} d="M12 2 2 7l10 5 10-5-10-5Z" />
                    <path {...common} d="m2 12 10 5 10-5" />
                    <path {...common} d="m2 17 10 5 10-5" />
                </>
            )}
            {name === "shieldCheck" && (
                <>
                    <path {...common} d="M12 3 20 6v5c0 5-3.4 8.6-8 10-4.6-1.4-8-5-8-10V6l8-3Z" />
                    <path {...common} d="m8.5 12 2.2 2.2 4.8-4.8" />
                </>
            )}
            {name === "alertTriangle" && (
                <>
                    <path {...common} d="m12 3 9 17H3L12 3Z" />
                    <path {...common} d="M12 9v4" />
                    <path {...common} d="M12 17h.01" />
                </>
            )}
            {name === "folder" && (
                <>
                    <path {...common} d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z" />
                </>
            )}
        </svg>
    );
}

/** Parse #rgb / #rrggbb (ignores alpha / non-hex). Returns null if unparseable. */
export function parseCssHexColor(input: string | undefined | null): { r: number; g: number; b: number } | null {
    if (!input) return null;
    const s = input.trim();
    const m = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(s);
    if (!m) return null;
    let h = m[1];
    if (h.length === 3) {
        h = h.split("").map((c) => c + c).join("");
    }
    const n = parseInt(h, 16);
    return { r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255 };
}

/** Relative luminance 0–1 (sRGB, WCAG). Non-hex colors return null. */
export function relativeLuminance(cssColor: string | undefined | null): number | null {
    const rgb = parseCssHexColor(cssColor);
    if (!rgb) return null;
    const lin = (c: number) => {
        const s = c / 255;
        return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
    };
    const R = lin(rgb.r);
    const G = lin(rgb.g);
    const B = lin(rgb.b);
    return 0.2126 * R + 0.7152 * G + 0.0722 * B;
}

/**
 * Pick ink color for a solid fill so label stays readable.
 * Light fills (graphite sendBtn) → dark ink; dark fills → white ink.
 */
export function contrastingInkOnFill(fillCss: string, preferred?: string): string {
    if (preferred) {
        // If preferred is also hex, ensure contrast ≥ ~3:1 for large/bold UI text; else keep preferred.
        const Lbg = relativeLuminance(fillCss);
        const Lfg = relativeLuminance(preferred);
        if (Lbg != null && Lfg != null) {
            const lighter = Math.max(Lbg, Lfg);
            const darker = Math.min(Lbg, Lfg);
            const ratio = (lighter + 0.05) / (darker + 0.05);
            if (ratio >= 3) return preferred;
        } else {
            return preferred; // non-hex preferred (e.g. currentColor) — trust caller
        }
    }
    const L = relativeLuminance(fillCss);
    // Threshold ~0.45: mid-light fills get dark ink (graphite #d4d4d4 ≈ 0.64)
    if (L == null) return "#ffffff";
    return L > 0.45 ? "#111111" : "#ffffff";
}


/**
 * Resolve filled-CTA background + foreground from theme tokens.
 * Prefer sendBtn* pair; never paint light `btnColor` accent with forced white text.
 */
export function resolvePrimaryFilledColors(
    t: Pick<Theme, "sendBtnBg" | "sendBtnColor" | "btnColor">,
): { bg: string; fg: string } {
    const bg = (t.sendBtnBg && t.sendBtnBg.trim()) || t.btnColor || "#2f5f98";
    const preferredFg = (t.sendBtnColor && t.sendBtnColor.trim()) || undefined;
    // If caller only has light btnColor as bg (no sendBtn), auto-ink prevents white-on-light.
    const fg = contrastingInkOnFill(bg, preferredFg);
    return { bg, fg };
}

/**
 * Filled primary CTA styles (Reconnect, Save, Submit, etc.).
 *
 * Dark schemes use light `btnColor` as an *accent/foreground* (links, outlines).
 * Never use `btnColor` as a solid button background with white text — that is
 * the low-contrast failure on graphite/classic/aurora dark modes.
 * Always pair sendBtnBg (fill) + sendBtnColor (label).
 */
export function primaryFilledButtonStyle(
    t: Pick<Theme, "sendBtnBg" | "sendBtnColor" | "sendBtnBorder" | "btnColor">,
    extras: React.CSSProperties = {},
): React.CSSProperties {
    const { bg, fg } = resolvePrimaryFilledColors(t);
    return {
        border: "none",
        background: bg,
        color: fg,
        boxShadow: `inset 0 0 0 1px ${t.sendBtnBorder || bg}`,
        ...extras,
    };
}

/** Form field label color with dark-mode-safe contrast (≥ muted body ink). */
export function formFieldLabelColor(t: Pick<Theme, "fieldLabel" | "textMuted" | "text">): string {
    // Prefer dedicated fieldLabel; fall back to text (not emptyHint / ultra-muted).
    return t.fieldLabel || t.textMuted || t.text || "#c4c4c4";
}

/** Shared input chrome for forms inside dark/light assistant surfaces. */
export function formFieldInputStyle(
    t: Pick<Theme, "fieldBorder" | "fieldBg" | "text" | "inputText">,
    extras: React.CSSProperties = {},
): React.CSSProperties {
    return {
        border: `1px solid ${t.fieldBorder}`,
        background: t.fieldBg,
        color: t.inputText || t.text,
        // Stronger outline on focus is set by callers; default keeps visible edge on dark mixes.
        outline: "none",
        ...extras,
    };
}

export function getInputActionButtonStyle(
    t: Theme,
    themeMode: 'light' | 'dark',
    tone: "neutral" | "attach" | "voice" | "voiceHold" | "send" | "cancel",
    disabled = false,
): React.CSSProperties {
    const dark = themeMode === 'dark';
    const palette = {
        neutral: { color: t.textMuted, border: dark ? t.fieldBorder : "rgba(47, 95, 152, 0.12)", bg: dark ? t.inputBarBg : "rgba(255, 255, 255, 0.92)", shadow: dark ? "inset 0 1px 0 rgba(255,255,255,0.04)" : "0 1px 2px rgba(15,23,42,0.05), inset 0 1px 0 rgba(255,255,255,0.88)" },
        attach: { color: dark ? t.btnColor : "#2f5f98", border: dark ? t.btnBorder : "rgba(47, 95, 152, 0.24)", bg: dark ? `color-mix(in srgb, ${t.btnColor} 12%, ${t.fieldBg})` : "rgba(243, 247, 251, 0.94)", shadow: dark ? "inset 0 1px 0 rgba(255,255,255,0.05)" : "0 1px 2px rgba(47,95,152,0.07), inset 0 1px 0 rgba(255,255,255,0.9)" },
        voice: { color: dark ? t.text : "#475569", border: dark ? t.fieldBorder : "rgba(71, 85, 105, 0.20)", bg: dark ? t.inputBarBg : "rgba(248, 250, 252, 0.92)", shadow: dark ? "inset 0 1px 0 rgba(255,255,255,0.04)" : "0 1px 2px rgba(15,23,42,0.05), inset 0 1px 0 rgba(255,255,255,0.86)" },
        voiceHold: { color: dark ? t.quoteText : "#475569", border: dark ? t.btnBorder : "rgba(100, 116, 139, 0.28)", bg: dark ? `color-mix(in srgb, ${t.btnColor} 10%, ${t.fieldBg})` : "rgba(248, 250, 252, 0.94)", shadow: dark ? `0 0 0 2px color-mix(in srgb, ${t.btnColor} 10%, transparent), inset 0 1px 0 rgba(255,255,255,0.05)` : "0 0 0 2px rgba(100, 116, 139, 0.08), inset 0 1px 0 rgba(255,255,255,0.9)" },
        send: { color: t.sendBtnColor, border: dark ? t.sendBtnBorder : "rgba(47, 95, 152, 0.70)", bg: dark ? t.sendBtnBg : "#2f5f98", shadow: dark ? `0 4px 10px color-mix(in srgb, ${t.sendBtnBg} 24%, transparent), inset 0 1px 0 rgba(255,255,255,0.10)` : "0 3px 8px rgba(47,95,152,0.14), inset 0 1px 0 rgba(255,255,255,0.16)" },
        cancel: { color: dark ? t.btnColor : "#2f5f98", border: dark ? t.btnBorder : "rgba(47, 95, 152, 0.24)", bg: dark ? `color-mix(in srgb, ${t.btnColor} 10%, ${t.fieldBg})` : "rgba(243, 247, 251, 0.96)", shadow: dark ? "inset 0 1px 0 rgba(255,255,255,0.05)" : "0 1px 2px rgba(47,95,152,0.07), inset 0 1px 0 rgba(255,255,255,0.9)" },
    }[tone];
    return { ...baseInputBtnStyle, color: palette.color, borderColor: palette.border, background: palette.bg, boxShadow: palette.shadow, opacity: disabled ? 0.45 : 1, cursor: disabled ? "default" : "pointer" };
}

export const baseWindowControlBtnStyle: React.CSSProperties = {
    width: "36px",
    height: "28px",
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    background: "transparent",
    border: "none",
    borderRadius: "4px",
    cursor: "pointer",
    padding: 0,
    lineHeight: 1,
    flexShrink: 0,
    fontFamily: "'Segoe UI Symbol', 'Segoe UI', sans-serif",
    transition: "background 120ms ease, color 120ms ease",
};


export const baseActionBtnStyle: React.CSSProperties = {
    background: "transparent",
    border: "none",
    fontSize: "12px",
    fontFamily: "'Segoe UI Symbol', 'Segoe UI', sans-serif",
    cursor: "pointer",
    padding: 0,
    borderRadius: "4px",
    lineHeight: 1,
    minHeight: "28px",
    minWidth: "28px",
    width: "32px",
    height: "28px",
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    transition: "background 120ms ease, color 120ms ease",
};

export const AI_PANEL_STATIC_STYLE_ID = "ai-panel-static-style";
export const AI_PANEL_STATIC_STYLE_TEXT = `
    @keyframes blink { 50% { opacity: 0; } }
    @keyframes ai-spinner-spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }
    @keyframes maclaw-spin { to { transform: rotate(360deg); } }
    @keyframes maclaw-brand-breathe {
        0%, 100% { transform: scale(1); opacity: 1; }
        50% { transform: scale(1.03); opacity: 0.92; }
    }
    @keyframes maclaw-brand-shimmer {
        0%, 100% { filter: drop-shadow(0 0 3px rgba(47, 95, 152, 0.28)) brightness(1); }
        50% { filter: drop-shadow(0 0 7px rgba(47, 95, 152, 0.38)) brightness(1.08); }
    }
    @keyframes ai-update-notice-pulse {
        0%, 100% { box-shadow: inset 0 0 0 1px rgba(79, 127, 111, 0.34), 0 0 0 0 rgba(79, 127, 111, 0.28); }
        50% { box-shadow: inset 0 0 0 1px rgba(79, 127, 111, 0.52), 0 0 0 5px rgba(79, 127, 111, 0.10); }
    }
    .pinned-news-card > div { margin-top: 0 !important; margin-bottom: 0 !important; }
    .ai-window-control:hover { background: var(--ai-window-control-hover-bg, rgba(148, 163, 184, 0.14)) !important; }
    .ai-window-control:active { filter: brightness(0.96); }
    .ai-window-control:focus-visible {
        outline: 2px solid rgba(47, 95, 152, 0.48);
        outline-offset: 1px;
    }
    .ai-titlebar-tool:hover:not(:disabled) { background: var(--ai-titlebar-tool-hover-bg, rgba(148, 163, 184, 0.12)) !important; }
    .ai-titlebar-tool:active:not(:disabled) { filter: brightness(0.96); }
    .ai-titlebar-tool:focus-visible {
        outline: 2px solid rgba(47, 95, 152, 0.38);
        outline-offset: 1px;
    }
    .ai-update-notice-button { animation: ai-update-notice-pulse 1.35s ease-in-out infinite; }
    .ai-update-menu-item:hover { background: rgba(148, 163, 184, 0.14) !important; }
    .ai-plus-menu-item:hover:not(:disabled) { background: var(--ai-plus-menu-item-hover-bg, rgba(47, 95, 152, 0.08)) !important; }
    .ai-plus-menu-item:focus-visible:not(:disabled) {
        outline: 2px solid rgba(47, 95, 152, 0.38);
        outline-offset: -1px;
    }
    .ai-plus-menu-item[data-active="true"]:not(:disabled) {
        background: var(--ai-plus-menu-item-hover-bg, rgba(47, 95, 152, 0.08)) !important;
    }
    .ai-plus-menu-item:disabled {
        cursor: not-allowed !important;
    }
    .ai-permission-mode-trigger:focus-visible,
    .ai-permission-mode-item:focus-visible {
        outline: 2px solid rgba(47, 95, 152, 0.48);
        outline-offset: 1px;
    }
    @media (prefers-reduced-motion: reduce) {
        .ai-update-notice-button { animation: none; }
    }
`;

export function getTitleBarToolButtonStyle(t: Theme, variant: "default" | "danger" | "active" = "default"): React.CSSProperties {
    const isDanger = variant === "danger";
    const isActive = variant === "active";
    return {
        ...baseActionBtnStyle,
        color: isDanger ? t.errorText : (isActive ? t.text : t.actionBtnColor),
        background: isDanger ? t.errorBg : (isActive ? t.divider : "transparent"),
        boxShadow: isActive ? `inset 0 0 0 1px ${t.fieldBorder}` : "none",
        ['--ai-titlebar-tool-hover-bg' as any]: isDanger ? t.errorBg : (isActive ? t.divider : "rgba(148, 163, 184, 0.12)"),
    };
}
