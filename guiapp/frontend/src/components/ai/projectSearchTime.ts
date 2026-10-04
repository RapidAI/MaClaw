import { localizeText } from "./aiAssistantI18n";

export function formatProjectSearchTime(lang: string, iso?: string): string {
    if (!iso) return "";
    try {
        const d = new Date(iso);
        const diffH = Math.floor((Date.now() - d.getTime()) / 3600000);
        if (diffH < 1) return localizeText(lang, "just now", "\u521a\u521a");
        if (diffH < 24) return `${diffH}${localizeText(lang, "h ago", "\u5c0f\u65f6\u524d")}`;
        const diffD = Math.floor(diffH / 24);
        return diffD < 7 ? `${diffD}${localizeText(lang, "d ago", "\u5929\u524d")}` : d.toLocaleDateString();
    } catch { return ""; }
}
