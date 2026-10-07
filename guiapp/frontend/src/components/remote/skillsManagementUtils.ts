import type { CSSProperties } from "react";
import { colors } from "./styles";
import { getSkillSourceLabel } from "./SkillSourceBadge";
import { isMaclawAppSearchResult } from "./SkillProductBadge";

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

export const hubCatalogDetailStyle: CSSProperties = {
    flex: "1 1 auto",
    minWidth: 0,
};

export const hubCatalogDescStyle: CSSProperties = {
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
    fontSize: "0.72rem",
    color: colors.textSecondary,
    lineHeight: 1.35,
};

export const hubCatalogGithubStyle: CSSProperties = {
    display: "flex",
    gap: "8px",
    minWidth: 0,
    marginTop: "2px",
    overflow: "hidden",
    fontSize: "0.66rem",
    color: colors.textMuted,
};

export const hubCatalogLinkStyle: CSSProperties = {
    padding: 0,
    border: "none",
    background: "transparent",
    color: colors.link,
    cursor: "pointer",
    fontSize: "0.66rem",
    textAlign: "left",
    textDecoration: "underline",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
    minWidth: 0,
    flex: "1 1 auto",
    maxWidth: "100%",
};

export const hubCatalogPathStyle: CSSProperties = {
    minWidth: 0,
    flex: "1 1 auto",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
};

export const hubCatalogTrailStyle: CSSProperties = {
    display: "flex",
    alignItems: "center",
    justifyContent: "flex-end",
    gap: "8px",
    flexShrink: 0,
};

export const hubCatalogNoteStyle: CSSProperties = {
    padding: "8px 10px",
    fontSize: "0.72rem",
    color: colors.textMuted,
    lineHeight: 1.4,
};

export const hubCatalogMetaLineStyle: CSSProperties = {
    marginTop: "1px",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
    color: colors.textMuted,
    fontSize: "0.66rem",
    fontVariantNumeric: "tabular-nums",
};

export const hubCatalogNameLineStyle: CSSProperties = {
    display: "flex",
    alignItems: "baseline",
    gap: "6px",
    minWidth: 0,
};

export const hubCatalogVersionStyle: CSSProperties = {
    flex: "0 0 auto",
    fontSize: "0.66rem",
    color: colors.textMuted,
    fontVariantNumeric: "tabular-nums",
};

export const hubCatalogActionStyle: CSSProperties = {
    fontSize: "0.72rem",
    padding: "2px 10px",
    flexShrink: 0,
    alignSelf: "center",
};

export const hubMarketToolbarStyle: CSSProperties = {
    display: "flex",
    gap: "6px",
    alignItems: "center",
    flexShrink: 0,
    padding: "6px 8px",
    background: colors.surfaceMuted,
    borderBottom: `1px solid ${colors.borderLight}`,
};

export const hubMarketFilterStyle: CSSProperties = {
    display: "flex",
    gap: "6px",
    alignItems: "center",
    flexWrap: "wrap",
    flexShrink: 0,
    fontSize: "0.72rem",
    padding: "4px 8px",
    background: colors.surface,
    borderBottom: `1px solid ${colors.borderLight}`,
};

export const settingsSegmentStyle: CSSProperties = {
    display: "inline-flex",
    alignSelf: "flex-start",
    flexShrink: 0,
    border: `1px solid ${colors.border}`,
    borderRadius: "6px",
    overflow: "hidden",
    background: colors.surfaceMuted,
};

export const settingsSegmentBtnStyle: CSSProperties = {
    border: "none",
    background: "transparent",
    color: colors.textSecondary,
    fontSize: "0.72rem",
    fontWeight: 600,
    padding: "4px 12px",
    cursor: "pointer",
};

export const settingsSegmentBtnActiveStyle: CSSProperties = {
    background: colors.surface,
    color: colors.text,
    boxShadow: `inset 0 -2px 0 ${colors.primary}`,
};

export const settingsControlStyle: CSSProperties = {
    display: "inline-flex",
    alignItems: "center",
    gap: "6px",
    minWidth: 0,
};

export const settingsNumberStyle: CSSProperties = {
    width: "88px",
    fontSize: "0.74rem",
    padding: "2px 6px",
    fontVariantNumeric: "tabular-nums",
};

export const settingsSaveBtnStyle: CSSProperties = {
    fontSize: "0.7rem",
    padding: "2px 8px",
};

export const settingsFootStyle: CSSProperties = {
    padding: "6px 10px",
    borderTop: `1px solid ${colors.borderLight}`,
    fontSize: "0.68rem",
    color: colors.textMuted,
    lineHeight: 1.4,
};

// ===== Shared capability-market catalog bits (row + card renderers) =====

/** Card-catalog geometry: four cards across, twenty cards on a page. */
export const SKILL_CARD_COLUMNS = 4;
export const SKILL_CARD_PAGE_SIZE = 20;

export interface MixedSkillSearchResult {
    id: string;
    name: string;
    description: string;
    tags: string[];
    source: string;
    source_label: string;
    install_ref?: string;
    file_path?: string;
    version?: string;
    author?: string;
    permissions?: string[];
    created_at?: string;
    trust_level?: string;
    avg_rating: number;
    rating_count: number;
    downloads: number;
    score: number;
    price: number;
    repo_url?: string;
    installed: boolean;
    installed_name?: string;
    can_update: boolean;
    has_update: boolean;
    product_kind?: string;
    is_maclaw_app?: boolean;
    maclaw_app_id?: string;
    maclaw_app_name?: string;
    maclaw_app_description?: string;
    maclaw_app_category?: string;
    maclaw_app_icon?: string;
    maclaw_app_input_mode?: string;
    maclaw_app_output_modes?: string[];
    maclaw_app_definition_sha256?: string;
    maclaw_app_test_evidence?: {
        run_id?: string;
        verified_at?: string;
        definition_fingerprint?: string;
        artifact_present?: boolean;
        artifact_name?: string;
        output_count?: number;
        primary_result?: string;
        result_payload?: Record<string, unknown>;
    };
    artifact_contract_required?: boolean;
    artifact_contract_output_modes?: string[];
    artifact_contract_presentation?: string;
}

type LocalizeText = (en: string, zhHans: string, zhHant: string) => string;

/** Single source of truth for the source/product/trust badge visibility. */
export function hasHubSourceBadges(skill: MixedSkillSearchResult): boolean {
    return Boolean(
        getSkillSourceLabel(skill)
        || isMaclawAppSearchResult(skill)
        || shouldShowTrustBadge(skill.trust_level),
    );
}

/** Derived display bits shared by the catalog row and card renderers. */
export function hubCatalogRowBits(skill: MixedSkillSearchResult, localizeText: LocalizeText): {
    descTitle: string;
    metaParts: string[];
    showSourceBadges: boolean;
} {
    const descTitle = [skill.description, ...(skill.tags || [])].filter(Boolean).join(" · ");
    const priceLabel = skill.price > 0
        ? localizeText(`Price ${skill.price}`, `价格 ${skill.price}`, `價格 ${skill.price}`)
        : "";
    const ratingValue = Number(skill.avg_rating);
    const ratingLabel = skill.rating_count > 0 && Number.isFinite(ratingValue)
        ? ratingValue.toFixed(1) + " (" + skill.rating_count + ")"
        : "";
    const metaParts = [
        skill.author,
        skill.downloads > 0 ? formatDownloads(skill.downloads) : "",
        ratingLabel,
        priceLabel,
    ].filter((part): part is string => Boolean(part));
    return { descTitle, metaParts, showSourceBadges: hasHubSourceBadges(skill) };
}
