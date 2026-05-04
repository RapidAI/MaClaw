export function localizeText(lang: string, en: string, zhHans: string, zhHant?: string): string {
    if (lang === "en") return en;
    if (lang === "zh-Hant") return zhHant || zhHans;
    return zhHans;
}
