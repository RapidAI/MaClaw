import type { CSSProperties } from "react";
import { colors, radius } from "./styles";

/** Shared card style for ClawNet sub-panels */
export const cnCard: CSSProperties = {
    border: `1px solid ${colors.border}`,
    borderRadius: radius.lg,
    padding: "10px 14px",
    marginBottom: "8px",
    background: colors.surface,
};

/** Muted label style */
export const cnLabel: CSSProperties = {
    fontSize: "0.72rem",
    color: colors.textMuted,
};

/** Section heading */
export const cnHeading: CSSProperties = {
    fontSize: "0.78rem",
    fontWeight: 600,
    color: colors.text,
    marginBottom: "8px",
};

/** Shared input style */
export const cnInput: CSSProperties = {
    border: `1px solid ${colors.border}`,
    borderRadius: radius.md,
    padding: "4px 8px",
    fontSize: "0.72rem",
    width: "100%",
};

/** Action button factory — returns CSSProperties */
export const cnActionBtn = (disabled?: boolean): CSSProperties => ({
    background: "transparent",
    color: disabled ? colors.textMuted : colors.primary,
    border: `1px solid ${disabled ? colors.border : colors.primary}`,
    borderRadius: radius.md,
    padding: "3px 10px",
    fontSize: "0.72rem",
    cursor: disabled ? "not-allowed" : "pointer",
    opacity: disabled ? 0.5 : 1,
});

/** Tab button factory */
export const cnTabStyle = (active: boolean): CSSProperties => ({
    background: active ? colors.primary : colors.bg,
    color: active ? "#fff" : colors.textSecondary,
    border: "none",
    borderRadius: radius.md,
    padding: "4px 12px",
    fontSize: "0.72rem",
    fontWeight: active ? 600 : 400,
    cursor: "pointer",
});

/** Tab button with icon layout — used by ClawNetTabContainer */
export const cnTabBtn = (active: boolean): CSSProperties => ({
    background: active ? colors.primary : "transparent",
    color: active ? "#fff" : colors.textSecondary,
    border: active ? "none" : `1px solid ${colors.border}`,
    borderRadius: radius.md,
    padding: "5px 12px",
    fontSize: "0.72rem",
    fontWeight: active ? 600 : 400,
    cursor: "pointer",
    display: "flex",
    alignItems: "center",
    gap: "4px",
    whiteSpace: "nowrap",
    transition: "all 0.15s ease",
});
