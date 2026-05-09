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
}

export const overlayTheme: Theme = {
    bg: "#eef0f5",
    titleBarBg: "#e2e4ea",
    titleBarBorder: "#c8cad0",
    titleText: "#555",
    text: "#2d2d2d",
    textMuted: "#777",
    inputBarBg: "#ffffff",
    inputBarBorder: "#6366f1",
    inputText: "#222",
    codeBg: "#e4e6ec",
    codeText: "#b5314a",
    codeBlockBg: "#e8eaf0",
    codeBlockBorder: "#c8cad0",
    codeBlockLang: "#999",
    borderLeft: "#c8cad0",
    responseBorderLeft: "#b4b6d4",
    headingColor: "#5558d6",
    linkColor: "#5558d6",
    pathColor: "#059669",
    promptColor: "#5558d6",
    userColor: "#5558d6",
    divider: "#d0d2d8",
    fieldBg: "#e8eaf0",
    fieldBorder: "#c8cad0",
    fieldLabel: "#777",
    errorText: "#dc2626",
    errorBg: "rgba(220, 38, 38, 0.06)",
    errorBorder: "#dc2626",
    emptyHint: "#999",
    boldColor: "#1a1a1a",
    italicColor: "#333",
    bulletColor: "#888",
    quoteBorder: "#b4b6d4",
    quoteText: "#666",
    btnColor: "#5558d6",
    btnBorder: "#5558d6",
    actionBtnColor: "#777",
    closeBtnColor: "#888",
    sendBtnColor: "#fff",
    sendBtnBorder: "#5558d6",
};

export const lightTheme: Theme = {
    bg: "#fafbff",
    titleBarBg: "#f0f1f5",
    titleBarBorder: "#ddd",
    titleText: "#666",
    text: "#333",
    textMuted: "#888",
    inputBarBg: "#f5f6fa",
    inputBarBorder: "#ddd",
    inputText: "#333",
    codeBg: "#f0f0f5",
    codeText: "#c7254e",
    codeBlockBg: "#f5f6fa",
    codeBlockBorder: "#ddd",
    codeBlockLang: "#aaa",
    borderLeft: "#ddd",
    responseBorderLeft: "#d4d4f7",
    headingColor: "#6366f1",
    linkColor: "#6366f1",
    pathColor: "#059669",
    promptColor: "#6366f1",
    userColor: "#6366f1",
    divider: "#e5e7eb",
    fieldBg: "#f5f6fa",
    fieldBorder: "#ddd",
    fieldLabel: "#888",
    errorText: "#dc2626",
    errorBg: "rgba(220, 38, 38, 0.06)",
    errorBorder: "#dc2626",
    emptyHint: "#aaa",
    boldColor: "#222",
    italicColor: "#444",
    bulletColor: "#999",
    quoteBorder: "#d4d4f7",
    quoteText: "#777",
    btnColor: "#6366f1",
    btnBorder: "#6366f1",
    actionBtnColor: "#888",
    closeBtnColor: "#999",
    sendBtnColor: "#6366f1",
    sendBtnBorder: "#6366f1",
};


export const darkTheme: Theme = {
    ...lightTheme,
    bg: "#0b1220",
    titleBarBg: "#111827",
    titleBarBorder: "#334155",
    titleText: "#f1f5f9",
    text: "#f1f5f9",
    textMuted: "#cbd5e1",
    inputBarBg: "#0f172a",
    inputBarBorder: "#334155",
    inputText: "#e5e7eb",
    codeBg: "#122033",
    codeText: "#bfdbfe",
    codeBlockBg: "#0d1829",
    codeBlockBorder: "#2b4363",
    codeBlockLang: "#7dd3fc",
    borderLeft: "#334155",
    responseBorderLeft: "#475569",
    headingColor: "#a5b4fc",
    linkColor: "#93c5fd",
    pathColor: "#67e8f9",
    promptColor: "#a5b4fc",
    userColor: "#c4b5fd",
    divider: "#1e293b",
    fieldBg: "#111827",
    fieldBorder: "#334155",
    fieldLabel: "#94a3b8",
    emptyHint: "#64748b",
    boldColor: "#f8fafc",
    italicColor: "#cbd5e1",
    bulletColor: "#94a3b8",
    quoteBorder: "#475569",
    quoteText: "#cbd5e1",
    actionBtnColor: "#cbd5e1",
    closeBtnColor: "#cbd5e1",
    sendBtnColor: "#ffffff",
    sendBtnBorder: "#818cf8",
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
    borderRadius: "8px",
    padding: 0,
    fontSize: "14px",
    fontFamily: "'Segoe UI Symbol', 'Segoe UI', sans-serif",
    cursor: "pointer",
    lineHeight: 1,
    minHeight: "34px",
    minWidth: "36px",
    width: "36px",
    height: "34px",
    flexShrink: 0,
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    transition: "transform 120ms ease, box-shadow 120ms ease, background 120ms ease, border-color 120ms ease, opacity 120ms ease",
};

export type AssistantInputIconName = "paperclip" | "mic" | "cornerDownLeft" | "stop";

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
        neutral: { color: t.textMuted, border: dark ? "rgba(148, 163, 184, 0.28)" : "rgba(99, 102, 241, 0.16)", bg: dark ? "rgba(15, 23, 42, 0.72)" : "rgba(255, 255, 255, 0.86)", shadow: dark ? "inset 0 1px 0 rgba(255,255,255,0.04)" : "0 1px 2px rgba(15,23,42,0.05), inset 0 1px 0 rgba(255,255,255,0.82)" },
        attach: { color: dark ? "#67e8f9" : "#0891b2", border: dark ? "rgba(34, 211, 238, 0.38)" : "rgba(8, 145, 178, 0.28)", bg: dark ? "rgba(8, 145, 178, 0.10)" : "rgba(236, 254, 255, 0.86)", shadow: dark ? "inset 0 1px 0 rgba(255,255,255,0.05)" : "0 1px 2px rgba(8,145,178,0.08), inset 0 1px 0 rgba(255,255,255,0.9)" },
        voice: { color: dark ? "#cbd5e1" : "#475569", border: dark ? "rgba(148, 163, 184, 0.30)" : "rgba(71, 85, 105, 0.20)", bg: dark ? "rgba(15, 23, 42, 0.74)" : "rgba(248, 250, 252, 0.92)", shadow: dark ? "inset 0 1px 0 rgba(255,255,255,0.04)" : "0 1px 2px rgba(15,23,42,0.05), inset 0 1px 0 rgba(255,255,255,0.86)" },
        voiceHold: { color: dark ? "#fbbf24" : "#dc2626", border: dark ? "rgba(251, 191, 36, 0.48)" : "rgba(220, 38, 38, 0.34)", bg: dark ? "rgba(251, 191, 36, 0.13)" : "rgba(254, 242, 242, 0.96)", shadow: dark ? "0 0 0 2px rgba(251, 191, 36, 0.10), inset 0 1px 0 rgba(255,255,255,0.05)" : "0 0 0 2px rgba(248, 113, 113, 0.12), inset 0 1px 0 rgba(255,255,255,0.9)" },
        send: { color: "#ffffff", border: dark ? "rgba(129, 140, 248, 0.78)" : "rgba(79, 70, 229, 0.72)", bg: dark ? "linear-gradient(180deg, #818cf8 0%, #6366f1 100%)" : "linear-gradient(180deg, #6366f1 0%, #4f46e5 100%)", shadow: dark ? "0 8px 18px rgba(79,70,229,0.28), inset 0 1px 0 rgba(255,255,255,0.18)" : "0 8px 18px rgba(79,70,229,0.20), inset 0 1px 0 rgba(255,255,255,0.22)" },
        cancel: { color: dark ? "#ddd6fe" : "#4f46e5", border: dark ? "rgba(129, 140, 248, 0.56)" : "rgba(79, 70, 229, 0.34)", bg: dark ? "rgba(99, 102, 241, 0.16)" : "rgba(238, 242, 255, 0.94)", shadow: dark ? "inset 0 1px 0 rgba(255,255,255,0.05)" : "0 1px 2px rgba(79,70,229,0.08), inset 0 1px 0 rgba(255,255,255,0.9)" },
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
    .pinned-news-card > div { margin-top: 0 !important; margin-bottom: 0 !important; }
    .ai-window-control:hover { background: var(--ai-window-control-hover-bg, rgba(148, 163, 184, 0.14)) !important; }
    .ai-window-control:active { filter: brightness(0.96); }
    .ai-window-control:focus-visible {
        outline: 2px solid rgba(99, 102, 241, 0.55);
        outline-offset: 1px;
    }
    .ai-titlebar-tool:hover { background: var(--ai-titlebar-tool-hover-bg, rgba(148, 163, 184, 0.12)) !important; }
    .ai-titlebar-tool:active { filter: brightness(0.96); }
    .ai-titlebar-tool:focus-visible {
        outline: 2px solid rgba(99, 102, 241, 0.4);
        outline-offset: 1px;
    }
`;

export function getTitleBarToolButtonStyle(t: Theme, variant: "default" | "danger" | "active" = "default"): React.CSSProperties {
    const isDanger = variant === "danger";
    const isActive = variant === "active";
    return {
        ...baseActionBtnStyle,
        color: isDanger ? "#b91c1c" : (isActive ? t.text : t.actionBtnColor),
        background: isDanger ? "rgba(220, 38, 38, 0.04)" : (isActive ? t.divider : "transparent"),
        boxShadow: isActive ? `inset 0 0 0 1px ${t.fieldBorder}` : "none",
        ['--ai-titlebar-tool-hover-bg' as any]: isDanger ? "rgba(220, 38, 38, 0.12)" : (isActive ? t.divider : "rgba(148, 163, 184, 0.12)"),
    };
}
