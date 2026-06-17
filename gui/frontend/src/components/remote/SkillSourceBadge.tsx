import type { CSSProperties } from "react";
import { colors } from "./styles";

type LocalizeText = (en: string, zhHans: string, zhHant: string) => string;

export interface SkillSourceBadgeSource {
    source: string;
    source_label: string;
}

export function getSkillSourceTooltip(skill: SkillSourceBadgeSource, localizeText: LocalizeText): string {
    switch ((skill.source || "").trim().toLowerCase()) {
        case "enterprise_hub":
            return localizeText(
                "Private market: capability market from your current Hub or organization.",
                "私有市场：来自你当前所属 Hub 或组织的能力市场。",
                "私有市場：來自你目前所屬 Hub 或組織的能力市場。"
            );
        case "skillmarket":
        case "skillhub":
            return localizeText(
                "Public market: public SkillMarket from HubCenter.",
                "公共市场：来自 HubCenter 的公共 SkillMarket 能力市场。",
                "公共市場：來自 HubCenter 的公共 SkillMarket 能力市場。"
            );
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
    const label = (skill.source_label || skill.source || "").trim();
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
