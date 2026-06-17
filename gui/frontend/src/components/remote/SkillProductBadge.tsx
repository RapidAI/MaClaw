import type { CSSProperties } from "react";
import { colors } from "./styles";

type LocalizeText = (en: string, zhHans: string, zhHant: string) => string;

export interface SkillProductBadgeSource {
    product_kind?: string;
    is_maclaw_app?: boolean;
}

export function isMaclawAppSearchResult(skill: SkillProductBadgeSource): boolean {
    return !!skill.is_maclaw_app || (skill.product_kind || "").trim().toLowerCase() === "maclaw_app_skill";
}

export function SkillProductBadge({ skill, localizeText }: { skill: SkillProductBadgeSource; localizeText: LocalizeText }) {
    if (!isMaclawAppSearchResult(skill)) return null;
    return (
        <span style={maclawAppProductBadgeStyle} title={localizeText("MaClaw App Skill", "MaClaw App Skill", "MaClaw App Skill")}>
            {localizeText("App Skill", "App Skill", "App Skill")}
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
    fontWeight: 700,
};
