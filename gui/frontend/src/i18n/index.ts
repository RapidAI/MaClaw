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
import { normalizeLang } from './langSelect';

// Re-export language helpers and the translations table for direct access
export { translations };
export { localizeText, normalizeLang } from './langSelect';

// Product naming for the MaClaw MiniAPP (码卡龙小程序) feature surface.
export {
    formatInstalledOpenPanelMessage,
    formatMiniAppSkillCount,
    localizeMiniAppPack,
    miniAppEntryLabel,
    miniAppLabels,
    miniAppNames,
    miniAppShortLabel,
    pickMiniAppLabel,
} from './maclawMiniAppLabels';
export type { MiniAppLabelPack } from './maclawMiniAppLabels';

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
