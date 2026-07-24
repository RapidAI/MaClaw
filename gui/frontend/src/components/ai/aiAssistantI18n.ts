// Re-export from unified i18n module for backward compatibility.
// New code should import from '../../i18n' directly.
export { localizeText } from '../../i18n';
import { localizeText } from '../../i18n';

/** English label for the fixed main AI assistant tab / panel chrome. */
export const LOCAL_ASSISTANT_TITLE_EN = "AI Assistant";
/** Chinese (Hans/Hant) label for the fixed main AI assistant tab / panel chrome. */
export const LOCAL_ASSISTANT_TITLE_ZH = "AI \u52a9\u624b";

/**
 * Localized title for the main AI assistant surface (local tab + panel chrome).
 * Uses normalizeLang so en-US / zh-CN / zh-TW resolve correctly.
 */
export function localAssistantTabTitle(lang?: string | null): string {
    return localizeText(lang, LOCAL_ASSISTANT_TITLE_EN, LOCAL_ASSISTANT_TITLE_ZH, LOCAL_ASSISTANT_TITLE_ZH);
}
