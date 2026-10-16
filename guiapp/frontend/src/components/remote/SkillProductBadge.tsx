import type { CSSProperties } from "react";
import { localizeMiniAppPack, miniAppLabels } from "../../i18n/maclawMiniAppLabels";
import { colors } from "./styles";

type LocalizeText = (en: string, zhHans: string, zhHant: string) => string;

export interface SkillProductBadgeSource {
    product_kind?: string;
    is_maclaw_app?: boolean;
}

export function isMaclawAppSearchResult(skill: SkillProductBadgeSource): boolean {
    return !!skill.is_maclaw_app || (skill.product_kind || "").trim().toLowerCase() === "maclaw_app_skill";
}

function AppIcon() {
    return (
        <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor" fillOpacity="0.15" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <rect x="3" y="3" width="8" height="8" rx="2" />
            <rect x="13" y="3" width="8" height="8" rx="2" />
            <rect x="3" y="13" width="8" height="8" rx="2" />
            <rect x="13" y="13" width="8" height="8" rx="2" />
        </svg>
    );
}

export function SkillProductBadge({ skill, localizeText }: { skill: SkillProductBadgeSource; localizeText: LocalizeText }) {
    if (!isMaclawAppSearchResult(skill)) return null;
    return (
        <span style={maclawAppProductBadgeStyle} title={localizeMiniAppPack(localizeText, miniAppLabels.skill)}>
            <AppIcon />
            {localizeMiniAppPack(localizeText, miniAppLabels.short)}
        </span>
    );
}

const maclawAppProductBadgeStyle: CSSProperties = {
    display: "inline-flex",
    alignItems: "center",
    gap: "4px",
    fontSize: "0.66rem",
    padding: "2px 6px",
    borderRadius: "999px",
    background: colors.successBg,
    color: colors.success,
    border: `1px solid ${colors.success}33`,
    fontWeight: 600,
};
