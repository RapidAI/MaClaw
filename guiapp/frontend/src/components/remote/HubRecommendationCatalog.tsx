import { useEffect, useRef, useState } from "react";
import type { CSSProperties, ReactNode } from "react";
import { colors, remoteCardStyle } from "./styles";
import {
    SKILL_CARD_COLUMNS,
    SKILL_CARD_PAGE_SIZE,
    hubCatalogMetaLineStyle,
    hubCatalogNameLineStyle,
    hubCatalogVersionStyle,
    hubCatalogRowBits,
    type MixedSkillSearchResult,
} from "./skillsManagementUtils";
import { MaclawAppMarketPreview } from "./MaclawAppMarketPreview";

type LocalizeText = (en: string, zhHans: string, zhHant: string) => string;

interface HubRecommendationCatalogProps {
    skills: MixedSkillSearchResult[];
    localizeText: LocalizeText;
    /** Source / product / trust badge line rendered by the parent panel. */
    renderBadges: (skill: MixedSkillSearchResult) => ReactNode;
    /** Install / Update / Installed action button rendered by the parent panel. */
    renderAction: (skill: MixedSkillSearchResult) => ReactNode;
}

// Recommendation catalog of the Capability Market: card grid — four cards
// across (SKILL_CARD_COLUMNS), twenty cards per page (SKILL_CARD_PAGE_SIZE),
// mirroring the installed-skill catalog geometry. Owns its pagination state;
// resets back to page 1 whenever the recommendations list is refreshed.
export function HubRecommendationCatalog({ skills, localizeText, renderBadges, renderAction }: HubRecommendationCatalogProps) {
    const [page, setPage] = useState(1);
    const rootRef = useRef<HTMLDivElement>(null);

    useEffect(() => {
        // Recommendations refreshed (tab switch / install state change /
        // "Recommended" button). Keep the current page while it still exists —
        // resetting to page 1 would kick a user off page 2 after a simple
        // Install — and clamp when the list shrank below it.
        setPage(prev => Math.min(
            Math.max(1, prev),
            Math.max(1, Math.ceil(skills.length / SKILL_CARD_PAGE_SIZE)),
        ));
    }, [skills]);

    const pageCount = Math.max(1, Math.ceil(skills.length / SKILL_CARD_PAGE_SIZE));
    const pageSafe = Math.min(Math.max(1, page), pageCount);

    useEffect(() => {
        // The scroll container is the parent catalog list; keep the freshly
        // paged grid in view instead of leaving the scroll offset mid-list.
        // Guarded: scrollIntoView is not implemented in some test environments.
        const node = rootRef.current;
        if (node && typeof node.scrollIntoView === "function") {
            node.scrollIntoView({ block: "start" });
        }
    }, [pageSafe]);
    const rangeStart = skills.length === 0 ? 0 : (pageSafe - 1) * SKILL_CARD_PAGE_SIZE + 1;
    const rangeEnd = Math.min(pageSafe * SKILL_CARD_PAGE_SIZE, skills.length);
    const paged = skills.slice((pageSafe - 1) * SKILL_CARD_PAGE_SIZE, pageSafe * SKILL_CARD_PAGE_SIZE);

    return (
        <div ref={rootRef} data-testid="hub-recommendation-catalog">
            <div style={hubRecGridStyle}>
                {paged.map((skill, index) => (
                    <HubRecommendationCard
                        key={`${skill.source || "skill"}-${skill.id || skill.name}-${(pageSafe - 1) * SKILL_CARD_PAGE_SIZE + index}`}
                        skill={skill}
                        localizeText={localizeText}
                        renderBadges={renderBadges}
                        renderAction={renderAction}
                    />
                ))}
            </div>
            {pageCount > 1 && (
                <div style={{ ...skillCardPagerStyle, padding: "4px 10px 6px" }}>
                    <span style={skillCardPagerMetaStyle}>
                        {localizeText(
                            `${rangeStart}–${rangeEnd} of ${skills.length}`,
                            `第 ${rangeStart}–${rangeEnd} 个，共 ${skills.length} 个`,
                            `第 ${rangeStart}–${rangeEnd} 個，共 ${skills.length} 個`,
                        )}
                    </span>
                    <div style={{ display: "flex", alignItems: "center", gap: "6px" }}>
                        <button
                            type="button"
                            className="btn-secondary"
                            style={skillCardPageBtnStyle}
                            disabled={pageSafe <= 1}
                            onClick={() => setPage(pageSafe - 1)}
                        >
                            {localizeText("Previous", "上一页", "上一頁")}
                        </button>
                        <span style={skillCardPagerMetaStyle}>
                            {localizeText(
                                `Page ${pageSafe} / ${pageCount}`,
                                `第 ${pageSafe} / ${pageCount} 页`,
                                `第 ${pageSafe} / ${pageCount} 頁`,
                            )}
                        </span>
                        <button
                            type="button"
                            className="btn-secondary"
                            style={skillCardPageBtnStyle}
                            disabled={pageSafe >= pageCount}
                            onClick={() => setPage(pageSafe + 1)}
                        >
                            {localizeText("Next", "下一页", "下一頁")}
                        </button>
                    </div>
                </div>
            )}
        </div>
    );
}

// Card variant of the catalog row: same content as the search-result row,
// reflowed vertically; the Install/Update action anchors to the bottom.
function HubRecommendationCard({ skill, localizeText, renderBadges, renderAction }: {
    skill: MixedSkillSearchResult;
    localizeText: LocalizeText;
    renderBadges: (skill: MixedSkillSearchResult) => ReactNode;
    renderAction: (skill: MixedSkillSearchResult) => ReactNode;
}) {
    const { descTitle, metaParts, showSourceBadges } = hubCatalogRowBits(skill, localizeText);
    return (
        <div style={hubRecCardStyle}>
            <div style={hubCatalogNameLineStyle}>
                <span style={hubRecCardNameStyle} title={skill.name}>{skill.name}</span>
                {skill.version && <span style={hubCatalogVersionStyle}>v{skill.version}</span>}
            </div>
            {showSourceBadges && renderBadges(skill)}
            <div style={hubRecCardDescStyle} title={descTitle || undefined}>
                {skill.description || localizeText("No description", "暂无描述", "暫無描述")}
            </div>
            {metaParts.length > 0 && (
                <div style={hubCatalogMetaLineStyle} title={metaParts.join(" · ")}>{metaParts.join(" · ")}</div>
            )}
            <MaclawAppMarketPreview skill={skill} localizeText={localizeText} />
            <div style={hubRecCardFootStyle}>
                {renderAction(skill)}
            </div>
        </div>
    );
}

const hubRecGridStyle: CSSProperties = {
    display: "grid",
    gridTemplateColumns: `repeat(${SKILL_CARD_COLUMNS}, minmax(0, 1fr))`,
    gap: "8px",
    alignItems: "stretch",
    padding: "8px 10px",
};

const hubRecCardStyle: CSSProperties = {
    ...remoteCardStyle,
    display: "flex",
    flexDirection: "column",
    gap: "4px",
    minWidth: 0,
    padding: "8px 10px",
    minHeight: "128px",
};

const hubRecCardNameStyle: CSSProperties = {
    flex: "1 1 auto",
    minWidth: 0,
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
    fontWeight: 600,
    fontSize: "0.78rem",
    color: colors.text,
};

const hubRecCardDescStyle: CSSProperties = {
    display: "-webkit-box",
    WebkitLineClamp: 2,
    WebkitBoxOrient: "vertical",
    overflow: "hidden",
    fontSize: "0.7rem",
    color: colors.textSecondary,
    lineHeight: 1.4,
    wordBreak: "break-word",
};

const hubRecCardFootStyle: CSSProperties = {
    display: "flex",
    justifyContent: "flex-end",
    marginTop: "auto",
    paddingTop: "2px",
};

const skillCardPagerStyle: CSSProperties = {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    gap: "12px",
    flexWrap: "wrap",
    padding: "2px 2px 0",
};

const skillCardPagerMetaStyle: CSSProperties = {
    fontSize: "0.74rem",
    color: colors.textSecondary,
    fontVariantNumeric: "tabular-nums",
};

const skillCardPageBtnStyle: CSSProperties = {
    fontSize: "0.74rem",
    padding: "3px 10px",
};
