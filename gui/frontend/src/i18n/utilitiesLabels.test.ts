import { describe, expect, it } from 'vitest';
import {
    utilitiesBackHint,
    utilitiesBackLabel,
    utilitiesEntryLabel,
    utilitiesLabels,
    utilitiesNavLabel,
    utilitiesPageTitle,
} from './utilitiesLabels';

describe('utilitiesLabels', () => {
    it('keeps Chinese rail and page titles identical', () => {
        expect(utilitiesNavLabel('zh-Hans')).toBe('专家&工具');
        expect(utilitiesNavLabel('zh-Hant')).toBe('專家&工具');
        expect(utilitiesPageTitle('zh-Hans')).toBe(utilitiesNavLabel('zh-Hans'));
        expect(utilitiesPageTitle('zh-Hant')).toBe(utilitiesNavLabel('zh-Hant'));
    });

    it('shortens the English rail label so it fits the 60px rail', () => {
        expect(utilitiesNavLabel('en')).toBe('Experts');
        expect(utilitiesPageTitle('en')).toBe('Experts & Tools');
        expect(utilitiesNavLabel('en').length).toBeLessThan(utilitiesPageTitle('en').length);
    });

    it('derives entry and back copy from the full title', () => {
        expect(utilitiesEntryLabel('zh-Hans')).toBe(`${utilitiesLabels.title.zhHans}入口`);
        expect(utilitiesEntryLabel('en')).toBe('Experts & Tools entry');
        expect(utilitiesBackLabel('zh-Hans')).toBe(`返回${utilitiesLabels.title.zhHans}`);
        expect(utilitiesBackLabel('en')).toBe('Back to Experts & Tools');
        expect(utilitiesBackHint('zh-Hant')).toContain(utilitiesLabels.title.zhHant);
        expect(utilitiesBackHint('en')).toContain(utilitiesLabels.title.en);
    });

    it('treats missing lang as Simplified Chinese, matching localizeText', () => {
        expect(utilitiesNavLabel()).toBe('专家&工具');
        expect(utilitiesNavLabel('zh')).toBe('专家&工具');
        expect(utilitiesPageTitle()).toBe('专家&工具');
    });
});
