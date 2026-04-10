import type { CSSProperties } from "react";

/* ── Semantic color tokens – resolved from CSS variables so light/dark can share the same components ── */
export const colors = {
    primary: "var(--theme-primary)",
    primaryDark: "var(--theme-primary-strong)",
    primaryLight: "var(--theme-primary-soft)",
    accentBg: "var(--theme-surface-muted)",
    bg: "var(--theme-page-bg)",
    surface: "var(--theme-surface)",
    text: "var(--theme-text-primary)",
    textSecondary: "var(--theme-text-secondary)",
    textMuted: "var(--theme-text-muted)",
    border: "var(--theme-border)",
    borderLight: "var(--theme-border-subtle)",
    success: "var(--theme-success)",
    successBg: "var(--theme-success-bg)",
    warning: "var(--theme-warning)",
    warningBg: "var(--theme-warning-bg)",
    danger: "var(--theme-danger)",
    dangerBg: "var(--theme-danger-bg)",
    link: "var(--theme-link-color)",
    infoBg: "var(--theme-info-bg)",
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
