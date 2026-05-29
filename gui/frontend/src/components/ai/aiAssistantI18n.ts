export function localizeText(lang: string, en: string, zhHans: string, zhHant?: string): string {
    const normalized = (lang || "").trim().toLowerCase();
    if (normalized === "en" || normalized.startsWith("en-")) return en;
    if (normalized === "zh-hant" || normalized === "zh-tw" || normalized === "zh-hk" || normalized === "zh-mo") return zhHant || zhHans;
    return zhHans;
}
