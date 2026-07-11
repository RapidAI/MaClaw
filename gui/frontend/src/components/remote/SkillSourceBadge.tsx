import type { CSSProperties } from "react";
import { colors } from "./styles";

type LocalizeText = (en: string, zhHans: string, zhHant: string) => string;

export interface SkillSourceBadgeSource {
    source: string;
    source_label: string;
}

const HUB_CENTER_SOURCE_ALIASES = new Set(["enterprise_hub", "hub", "hubcenter", "skillmarket", "skillhub"]);

export function getSkillSourceLabel(skill: SkillSourceBadgeSource): string {
    const source = (skill.source || "").trim().toLowerCase();
    if (HUB_CENTER_SOURCE_ALIASES.has(source)) return "Hub / HubCenter";
    return (skill.source_label || skill.source || "").trim();
}

export function getSkillSourceTooltip(skill: SkillSourceBadgeSource, localizeText: LocalizeText): string {
    const source = (skill.source || "").trim().toLowerCase();
    if (HUB_CENTER_SOURCE_ALIASES.has(source)) {
        return localizeText(
            "Hub / HubCenter capability market.",
            "Hub / HubCenter 能力市场。",
            "Hub / HubCenter 能力市場。"
        );
    }
    switch (source) {
        case "clawhub":
            return localizeText(
                "ClawHub marketplace mirror. Security checks run before install.",
                "ClawHub 市场镜像，安装前会进行安全检查。",
                "ClawHub 市場鏡像，安裝前會進行安全檢查。"
            );
        case "github":
            return localizeText(
                "GitHub repository search result. Security checks run before install.",
                "GitHub 仓库搜索结果，安装前会进行安全检查。",
                "GitHub 倉庫搜尋結果，安裝前會進行安全檢查。"
            );
        default:
            return skill.source_label || skill.source || "";
    }
}

export function SkillSourceBadge({ skill, localizeText }: { skill: SkillSourceBadgeSource; localizeText: LocalizeText }) {
    const label = getSkillSourceLabel(skill);
    if (!label) return null;
    return (
        <span style={skillSourceBadgeStyle} title={getSkillSourceTooltip(skill, localizeText)}>
            {label}
        </span>
    );
}

const skillSourceBadgeStyle: CSSProperties = {
    display: "inline-flex",
    alignItems: "center",
    gap: "4px",
    fontSize: "0.66rem",
    padding: "2px 6px",
    borderRadius: "999px",
    background: colors.surfaceMuted,
    color: colors.textSecondary,
    border: `1px solid ${colors.borderLight}`,
};
