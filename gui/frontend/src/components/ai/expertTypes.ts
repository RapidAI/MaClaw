/**
 * AI Expert definitions shared by the utilities page (expert cards / editor)
 * and the AI assistant panel (expert conversation tabs).
 *
 * Mirrors the backend ExpertDefinition Go struct (gui/expert_store.go).
 * The Wails bindings exchange it as JSON strings (ListExperts/SaveExpert).
 */

/** An AI expert persona: custom system prompt + optional tool/skill allow-lists. */
export interface ExpertDefinition {
    /** Builtin: "builtin-…"; user-created: uuid. */
    id: string;
    name: string;
    description: string;
    /** Emoji icon shown on cards and tab badges. */
    icon: string;
    system_prompt: string;
    /** Tool name allow-list; empty = no restriction. */
    tools: string[];
    /** Skill name allow-list; empty = no restriction. */
    skills: string[];
    builtin: boolean;
    /**
     * Lineage ("专家优化"): non-empty means this expert was distilled from the
     * referenced source expert's session. Each source has at most one direct
     * optimized expert; optimized experts may themselves be optimized (chained).
     */
    optimized_from_id?: string;
    /** Free-form "关于" text (author, copyright, notes) shown via the card's About button. */
    about?: string;
    created_at: string;
    updated_at: string;
}

/** Shape returned by OptimizeExpertFromSession: an editor-prefillable draft. */
export interface ExpertOptimizeDraft {
    /** Non-empty when updating the source's existing optimized expert. */
    id?: string;
    name?: string;
    description?: string;
    icon?: string;
    system_prompt?: string;
    tools?: string[];
    skills?: string[];
    optimized_from_id?: string;
    /** Carried over from the existing optimized expert when updating. */
    about?: string;
    /** True when the draft updates the existing optimized expert (id set). */
    update_existing?: boolean;
    /** Name of the source expert (for the different-name validation). */
    source_name?: string;
    /** Original configuration, used only to show an optimization diff before save. */
    source_system_prompt?: string;
    source_tools?: string[];
    source_skills?: string[];
}

/** Shape returned by GenerateExpertProfile (fields are suggestions, editable before save). */
export interface GeneratedExpertProfile {
    name?: string;
    description?: string;
    icon?: string;
    system_prompt?: string;
    suggested_tools?: string[];
    suggested_skills?: string[];
}

/** Deterministic tab id for an expert conversation tab. */
export function expertTabId(expertId: string): string {
    return `expert-${String(expertId || "").trim()}`;
}

/** Default emoji badge when an expert has no icon configured. */
export const DEFAULT_EXPERT_ICON = "\u{1F916}"; // 🤖

/** First-run welcome message seeded into a freshly created expert tab (local only, no LLM call). */
export function expertWelcomeMessageText(expert: Pick<ExpertDefinition, "name" | "description">, lang?: string): string {
    const name = String(expert?.name || "").trim();
    const description = String(expert?.description || "").trim();
    if (lang === "zh-Hant") {
        return description
            ? `你好，我是${name}。${description} 有什麼可以幫你？`
            : `你好，我是${name}。有什麼可以幫你？`;
    }
    if (lang?.startsWith("en")) {
        return description
            ? `Hi, I'm ${name}. ${description} How can I help you?`
            : `Hi, I'm ${name}. How can I help you?`;
    }
    return description
        ? `你好，我是${name}。${description} 有什么可以帮你？`
        : `你好，我是${name}。有什么可以帮你？`;
}

/** Parse a JSON payload from ListExperts into expert definitions (defensive). */
export function parseExpertListJSON(raw: string | null | undefined): ExpertDefinition[] {
    if (!raw) return [];
    try {
        const parsed = JSON.parse(raw);
        if (!Array.isArray(parsed)) return [];
        return parsed.filter((item): item is ExpertDefinition =>
            !!item && typeof item === "object" && typeof item.id === "string" && !!item.id
        ).map(item => ({
            ...item,
            name: String(item.name || ""),
            description: String(item.description || ""),
            icon: String(item.icon || ""),
            system_prompt: String(item.system_prompt || ""),
            tools: Array.isArray(item.tools) ? item.tools : [],
            skills: Array.isArray(item.skills) ? item.skills : [],
            builtin: !!item.builtin,
            optimized_from_id: String(item.optimized_from_id || ""),
            about: String(item.about || ""),
            created_at: String(item.created_at || ""),
            updated_at: String(item.updated_at || ""),
        }));
    } catch {
        return [];
    }
}
