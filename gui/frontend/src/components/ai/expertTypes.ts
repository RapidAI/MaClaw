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
    created_at: string;
    updated_at: string;
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
            created_at: String(item.created_at || ""),
            updated_at: String(item.updated_at || ""),
        }));
    } catch {
        return [];
    }
}
