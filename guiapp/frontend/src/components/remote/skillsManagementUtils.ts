import type { CSSProperties } from "react";
import { colors } from "./styles";

export const executionClassBadgeStyle: CSSProperties = {
    display: "inline-block",
    padding: "1px 8px",
    borderRadius: "999px",
    fontSize: "0.68rem",
    fontWeight: 600,
    color: colors.text,
    background: colors.surfaceMuted,
    border: `1px solid ${colors.border}`,
    whiteSpace: "nowrap",
};

export const statusDotStyle = (active: boolean): CSSProperties => ({
    display: "inline-flex",
    alignItems: "center",
    gap: "5px",
    fontSize: "0.72rem",
    color: active ? colors.success : colors.textMuted,
    whiteSpace: "nowrap",
});

export const uploadBtnStyle: CSSProperties = {
    fontSize: "0.7rem",
    padding: "2px 10px",
    whiteSpace: "nowrap",
    minWidth: "60px",
    textAlign: "center",
};

export const trustBadgeStyle = (level: string): CSSProperties => {
    const levelColors: Record<string, { bg: string; color: string; border: string }> = {
        official: { bg: colors.successBg, color: colors.success, border: colors.success },
        builtin: { bg: colors.successBg, color: colors.success, border: colors.success },
        trusted: { bg: colors.successBg, color: colors.success, border: colors.success },
        enterprise: { bg: colors.successBg, color: colors.success, border: colors.success },
        community: { bg: colors.infoBg, color: colors.primary, border: colors.primary },
        "agent-created": { bg: colors.surfaceMuted, color: colors.textMuted, border: colors.border },
    };
    const c = levelColors[level] || levelColors.community;
    return {
        display: "inline-block",
        padding: "0px 6px",
        borderRadius: "999px",
        fontSize: "0.66rem",
        fontWeight: 600,
        background: c.bg,
        color: c.color,
        border: `1px solid ${c.border}`,
    };
};

export function trustLevelLabel(level: string, localizeText: (en: string, zhHans: string, zhHant: string) => string): string {
    switch (level) {
        case "official":
        case "builtin":
            return localizeText("Official", "官方", "官方");
        case "trusted":
        case "enterprise":
            return localizeText("Trusted", "可信", "可信");
        case "community":
            return localizeText("Community", "社区", "社區");
        case "agent-created":
            return localizeText("Agent", "Agent生成", "Agent生成");
        default:
            return localizeText("3rd-party", "第三方", "第三方");
    }
}

export function shouldShowTrustBadge(level: string | undefined): boolean {
    return !!level && level !== "unknown";
}

export function formatDownloads(n: number): string {
    if (n >= 10000) return (n / 10000).toFixed(1).replace(/\.0$/, "") + "w";
    if (n >= 1000) return (n / 1000).toFixed(1).replace(/\.0$/, "") + "k";
    return String(n);
}

export function formatDate(dateStr: string): string {
    if (!dateStr) return "";
    try {
        const d = new Date(dateStr);
        if (isNaN(d.getTime())) return "";
        return d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
    } catch {
        return "";
    }
}

const HUB_VERSION_PATTERN = /^[vV0-9][A-Za-z0-9._+-]*$/;

export function displayHubVersion(version?: string): string {
    const value = String(version || "").trim();
    if (!value || value.length > 32 || !HUB_VERSION_PATTERN.test(value)) {
        return "";
    }
    return value;
}

export function renderStars(avg: number): string {
    if (!Number.isFinite(avg) || avg <= 0) return "Rating -";
    return `Rating ${avg.toFixed(1)}`;
}
