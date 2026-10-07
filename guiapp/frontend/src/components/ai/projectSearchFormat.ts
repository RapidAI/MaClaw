import { localizeText } from "./aiAssistantI18n";
import type { ProjectSearchItem } from "./projectSearchTypes";

export function formatArtifactSummary(item: ProjectSearchItem, lang: string, includeSource = false): string {
    const artifact = item.recent_artifacts?.find(a => a.title || a.preview || a.source_url);
    if (!artifact) return "";
    const label = artifact.title || artifact.preview || artifact.source_url || "";
    const prefix = localizeText(lang, "Latest artifact", "最近产物");
    const hint = artifact.source_hint ? "; " + artifact.source_hint : "";
    const source = includeSource && artifact.source_url ? " | " + artifact.source_url + hint : "";
    return prefix + ": " + label + source;
}

export function formatWorkflowType(type: string | undefined, lang: string): string {
    if (!type) return "";
    const labels: Record<string, { en: string; zh: string }> = {
        coding: { en: "Coding", zh: "\u7f16\u7a0b" },
        product_design: { en: "Product Design", zh: "\u4ea7\u54c1\u8bbe\u8ba1" },
        research: { en: "Research", zh: "\u7814\u7a76" },
        writing: { en: "Writing", zh: "\u5199\u4f5c" },
    };
    const hit = labels[type];
    return hit ? (lang === "en" ? hit.en : hit.zh) : type.replace(/_/g, " ");
}
