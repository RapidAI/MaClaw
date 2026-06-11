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
    errorText: "#b42318",
    errorBg: "rgba(180, 35, 24, 0.06)",
    errorBorder: "#b42318",
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
    errorText: "#b42318",
    errorBg: "rgba(180, 35, 24, 0.06)",
    errorBorder: "#b42318",
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
    textMuted: "#94a3b8",
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
    fieldBorder: "#334155",
    fieldLabel: "#94a3b8",
    errorText: "#e08b84",
    errorBg: "rgba(180, 35, 24, 0.10)",
    errorBorder: "#b95b52",
    emptyHint: "#64748b",
    boldColor: "#f8fafc",
    italicColor: "#e2e8f0",
    bulletColor: "#64748b",
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
    background: lightTheme.bg,
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

export type AssistantInputIconName = "paperclip" | "mic" | "cornerDownLeft" | "stop" | "edit" | "trash";

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
        </svg>
    );
}

export function getInputActionButtonStyle(
    t: Theme,
    themeMode: 'light' | 'dark',
    tone: "neutral" | "attach" | "voice" | "voiceHold" | "send" | "cancel",
    disabled = false,
): React.CSSProperties {
    const dark = themeMode === 'dark';
    const palette = {
        neutral: { color: t.textMuted, border: dark ? "rgba(148, 163, 184, 0.28)" : "rgba(47, 95, 152, 0.12)", bg: dark ? "rgba(15, 23, 42, 0.72)" : "rgba(255, 255, 255, 0.92)", shadow: dark ? "inset 0 1px 0 rgba(255,255,255,0.04)" : "0 1px 2px rgba(15,23,42,0.05), inset 0 1px 0 rgba(255,255,255,0.88)" },
        attach: { color: dark ? "#b7d3ef" : "#2f5f98", border: dark ? "rgba(91, 120, 152, 0.44)" : "rgba(47, 95, 152, 0.24)", bg: dark ? "rgba(91, 120, 152, 0.12)" : "rgba(243, 247, 251, 0.94)", shadow: dark ? "inset 0 1px 0 rgba(255,255,255,0.05)" : "0 1px 2px rgba(47,95,152,0.07), inset 0 1px 0 rgba(255,255,255,0.9)" },
        voice: { color: dark ? "#cbd5e1" : "#475569", border: dark ? "rgba(148, 163, 184, 0.30)" : "rgba(71, 85, 105, 0.20)", bg: dark ? "rgba(15, 23, 42, 0.74)" : "rgba(248, 250, 252, 0.92)", shadow: dark ? "inset 0 1px 0 rgba(255,255,255,0.04)" : "0 1px 2px rgba(15,23,42,0.05), inset 0 1px 0 rgba(255,255,255,0.86)" },
        voiceHold: { color: dark ? "#c7d7e8" : "#475569", border: dark ? "rgba(148, 163, 184, 0.42)" : "rgba(100, 116, 139, 0.28)", bg: dark ? "rgba(148, 163, 184, 0.12)" : "rgba(248, 250, 252, 0.94)", shadow: dark ? "0 0 0 2px rgba(148, 163, 184, 0.10), inset 0 1px 0 rgba(255,255,255,0.05)" : "0 0 0 2px rgba(100, 116, 139, 0.08), inset 0 1px 0 rgba(255,255,255,0.9)" },
        send: { color: "#ffffff", border: dark ? "rgba(91, 120, 152, 0.78)" : "rgba(47, 95, 152, 0.70)", bg: dark ? "#386b9f" : "#2f5f98", shadow: dark ? "0 4px 10px rgba(47,95,152,0.22), inset 0 1px 0 rgba(255,255,255,0.10)" : "0 3px 8px rgba(47,95,152,0.14), inset 0 1px 0 rgba(255,255,255,0.16)" },
        cancel: { color: dark ? "#c7d7e8" : "#2f5f98", border: dark ? "rgba(91, 120, 152, 0.55)" : "rgba(47, 95, 152, 0.24)", bg: dark ? "rgba(91, 120, 152, 0.14)" : "rgba(243, 247, 251, 0.96)", shadow: dark ? "inset 0 1px 0 rgba(255,255,255,0.05)" : "0 1px 2px rgba(47,95,152,0.07), inset 0 1px 0 rgba(255,255,255,0.9)" },
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
    .ai-titlebar-tool:hover { background: var(--ai-titlebar-tool-hover-bg, rgba(148, 163, 184, 0.12)) !important; }
    .ai-titlebar-tool:active { filter: brightness(0.96); }
    .ai-titlebar-tool:focus-visible {
        outline: 2px solid rgba(47, 95, 152, 0.38);
        outline-offset: 1px;
    }
    .ai-update-notice-button { animation: ai-update-notice-pulse 1.35s ease-in-out infinite; }
    .ai-update-menu-item:hover { background: rgba(148, 163, 184, 0.14) !important; }
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
