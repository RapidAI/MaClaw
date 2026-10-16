import type { NewsCardData, NewsCategory } from "./useAIAssistant";
import type { StatusGlyphKind } from "./WorkbenchIcons";

export type NewsBadge = {
    /** Short ASCII label for the chip (never emoji). */
    label: string;
    /** Optional StatusGlyph kind for visual weight. */
    glyph?: StatusGlyphKind;
};

const LABEL_BY_CATEGORY: Record<Exclude<NewsCategory, "">, NewsBadge> = {
    notice: { label: "INFO", glyph: "info" },
    update: { label: "NEW", glyph: "info" },
    tip: { label: "TIP", glyph: "info" },
    alert: { label: "ALERT", glyph: "warn" },
};

/** Map news category / legacy icon strings to a professional text+SVG badge. */
export function resolveNewsBadge(news: Pick<NewsCardData, "category" | "icon">): NewsBadge {
    if (news.category && news.category in LABEL_BY_CATEGORY) {
        return LABEL_BY_CATEGORY[news.category as Exclude<NewsCategory, "">];
    }
    const raw = String(news.icon || "").trim();
    // Already a plain label from createNewsMessage (INFO/NEW/TIP/ALERT).
    if (/^(INFO|NEW|TIP|ALERT)$/i.test(raw)) {
        const upper = raw.toUpperCase();
        if (upper === "ALERT") return { label: "ALERT", glyph: "warn" };
        if (upper === "NEW") return { label: "NEW", glyph: "info" };
        if (upper === "TIP") return { label: "TIP", glyph: "info" };
        return { label: "INFO", glyph: "info" };
    }
    // Legacy pictograph or unknown → generic INFO (no emoji rendered).
    return { label: "INFO", glyph: "info" };
}
