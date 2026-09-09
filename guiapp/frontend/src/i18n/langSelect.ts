/**
 * Language selection helpers shared by key-based and inline i18n.
 * Kept free of product-label imports to avoid circular module graphs.
 */

/**
 * Normalize language code to one of: 'en' | 'zh-Hans' | 'zh-Hant'
 */
export function normalizeLang(lang: string | undefined | null): string {
    const raw = (lang || '').trim().toLowerCase();
    if (raw === 'en' || raw.startsWith('en-')) return 'en';
    if (raw === 'zh-hant' || raw === 'zh-tw' || raw === 'zh-hk' || raw === 'zh-mo') return 'zh-Hant';
    // Covers: 'zh', 'zh-cn', 'zh-hans', 'zh-sg', '', and any unrecognized value
    return 'zh-Hans';
}

/**
 * Inline tri-lingual text selection.
 * Use when the text is dynamic or doesn't warrant a dedicated key.
 */
export function localizeText(lang: string | undefined | null, en: string, zhHans: string, zhHant?: string): string {
    const normalized = normalizeLang(lang);
    if (normalized === 'en') return en;
    if (normalized === 'zh-Hant') return zhHant || zhHans;
    return zhHans;
}
