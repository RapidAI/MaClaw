import { describe, expect, it } from 'vitest';
import { localizeAIAssistantError } from '../aiAssistantI18n';
import { describeAIAssistantError } from '../aiAssistantErrorDescription';
import { localizeHubServiceReason } from '../../../utils/hubServiceI18n';

describe('e2e credit-error pipeline', () => {
    const raw = 'LLM call failed: insufficient credits for this request: need 56.003 credits, available 6.430 (993.570 held by in-flight requests)';

    it('carries held through localize -> describe in every language', () => {
        for (const lang of ['zh-Hans', 'zh-Hant', 'en'] as const) {
            const localized = localizeAIAssistantError(raw, lang);
            const described = describeAIAssistantError(localized, lang);
            expect(described.title).not.toBe('LLM call failed');
            expect(described.detail ?? '').toContain('56.003');
            expect(described.detail ?? '').toContain('6.430');
        }
    });

    it('survives the hubServiceI18n -> aiAssistantI18n chained localization', () => {
        for (const lang of ['zh-Hans', 'zh-Hant'] as const) {
            const serviceLocalized = localizeHubServiceReason(raw.replace(/^LLM call failed: /, ''), lang);
            const assistantLocalized = localizeAIAssistantError(serviceLocalized, lang);
            const described = describeAIAssistantError(assistantLocalized, lang);
            expect(described.detail ?? '').toContain('993.570');
        }
    });

    it('round-trips every localized form back to stable English', () => {
        const canonical = localizeAIAssistantError(raw, 'en');
        for (const lang of ['zh-Hans', 'zh-Hant'] as const) {
            const localized = localizeAIAssistantError(raw, lang);
            expect(localizeAIAssistantError(localized, 'en')).toBe(canonical);
        }
    });
});
