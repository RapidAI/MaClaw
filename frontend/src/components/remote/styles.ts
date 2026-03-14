import type { CSSProperties } from "react";

/* ── Semantic color tokens – professional muted palette ── */
export const colors = {
    primary: "#4f5d75",
    primaryDark: "#2d3748",
    primaryLight: "#a0aec0",
    accentBg: "#f7f8fa",
    bg: "#f4f5f7",
    surface: "#ffffff",
    text: "#1a202c",
    textSecondary: "#5a6577",
    textMuted: "#8b95a5",
    border: "#e1e4e8",
    borderLight: "rgba(148, 163, 184, 0.14)",
    success: "#2f855a",
    successBg: "#f0fdf4",
    warning: "#b7791f",
    warningBg: "#fffbeb",
    danger: "#c53030",
    dangerBg: "#fff5f5",
} as const;

export const radius = {
    sm: "4px",
    md: "6px",
    lg: "8px",
    xl: "10px",
    pill: "999px",
} as const;

/* ── Shared card styles ── */

export const remoteCardStyle: CSSProperties = {
    border: `1px solid ${colors.border}`,
    borderRadius: radius.lg,
    padding: "10px 12px",
    background: colors.surface,
};

export const remoteMutedCardStyle: CSSProperties = {
    padding: "8px 10px",
    borderRadius: radius.md,
    background: colors.bg,
    border: `1px solid ${colors.border}`,
};

export const remoteSessionCardStyle: CSSProperties = {
    border: `1px solid ${colors.border}`,
    borderRadius: radius.lg,
    padding: "10px 12px",
    background: colors.surface,
};

export const remotePanelGridStyle: CSSProperties = {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fit, minmax(200px, 1fr))",
    gap: "8px",
    marginBottom: "10px",
};

export const remoteSectionTitleStyle: CSSProperties = {
    fontSize: "0.8rem",
    fontWeight: 600,
    color: colors.text,
    marginBottom: "8px",
    letterSpacing: "0.01em",
};

export const remoteLabelStyle: CSSProperties = {
    fontSize: "0.7rem",
    color: colors.textSecondary,
    marginBottom: "3px",
    fontWeight: 500,
};

export const remoteMetaLabelStyle: CSSProperties = {
    fontSize: "0.68rem",
    color: colors.textMuted,
    textTransform: "uppercase",
    letterSpacing: "0.04em",
    marginBottom: "4px",
    fontWeight: 600,
};

export const remoteBodyTextStyle: CSSProperties = {
    fontSize: "0.74rem",
    color: colors.textSecondary,
};

export const remoteActionButtonStyle: CSSProperties = {
    fontSize: "0.72rem",
    padding: "3px 8px",
};

export const remoteToolbarCardStyle: CSSProperties = {
    border: `1px solid ${colors.border}`,
    borderRadius: radius.lg,
    padding: "10px 12px",
    background: colors.bg,
};

export const remoteSessionMetricCardStyle: CSSProperties = {
    borderRadius: radius.md,
    border: `1px solid ${colors.border}`,
    background: colors.surface,
    padding: "8px 10px",
    minHeight: "60px",
};

export const remoteSessionSummaryCardStyle: CSSProperties = {
    borderRadius: radius.md,
    border: `1px solid ${colors.border}`,
    background: colors.bg,
    padding: "8px 10px",
    marginBottom: "8px",
};

/* ── Inline-style helpers for sub-components ── */

export const remoteSubLabelStyle: CSSProperties = {
    fontSize: "0.68rem",
    color: colors.textMuted,
    marginBottom: "3px",
    fontWeight: 500,
};

export const remoteSubHeadingStyle: CSSProperties = {
    fontSize: "0.72rem",
    fontWeight: 600,
    color: colors.text,
    marginBottom: "4px",
};

export const remoteDetailTextStyle: CSSProperties = {
    fontSize: "0.72rem",
    color: colors.textSecondary,
    lineHeight: 1.5,
};

export const remoteInfoCardStyle: CSSProperties = {
    borderRadius: radius.md,
    border: `1px solid ${colors.border}`,
    background: colors.bg,
    padding: "7px 10px",
};

export const remoteSidePanelStyle: CSSProperties = {
    borderLeft: `1px solid ${colors.border}`,
    background: colors.accentBg,
    padding: "10px 12px",
    display: "flex",
    flexDirection: "column",
    gap: "10px",
    justifyContent: "space-between",
};

export const remoteDescTextStyle: CSSProperties = {
    fontSize: "0.7rem",
    color: colors.textSecondary,
    marginBottom: "6px",
    lineHeight: 1.5,
};
