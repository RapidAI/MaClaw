/**
 * Unified i18n entry point for the frontend.
 *
 * Usage (import path depends on file location):
 *   import { t, localizeText } from '../../i18n';      // from src/components/**
 *   import { t, localizeText } from '../i18n';         // from src/config/
 *   import { t, localizeText } from './i18n';          // from src/App.tsx
 *
 *   // Key-based (preferred for static UI text):
 *   t('saveChanges', lang)  // → "保存并关闭" or "Save & Close"
 *
 *   // Inline tri-lingual (for dynamic/one-off text):
 *   localizeText(lang, 'Processing...', '处理中...', '處理中...')
 */

import { translations } from './appTranslations';

// Re-export the translations table for direct access
export { translations };

/**
 * Look up a translation key for the given language.
 * Falls back to zh-Hans → en → key itself.
 */
export function t(key: string, lang: string | undefined | null): string {
    const normalized = normalizeLang(lang);
    const table = translations[normalized];
    if (table && key in table) return table[key];
    // Fallback chain: zh-Hans → en → raw key
    if (normalized !== 'zh-Hans') {
        const zhTable = translations['zh-Hans'];
        if (zhTable && key in zhTable) return zhTable[key];
    }
    if (normalized !== 'en') {
        const enTable = translations['en'];
        if (enTable && key in enTable) return enTable[key];
    }
    if (import.meta.env?.DEV) {
        console.warn(`[i18n] missing key: "${key}"`);
    }
    return key;
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
