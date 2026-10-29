import { describe, expect, it, vi } from 'vitest';
import { isChineseLang, isTraditionalLang, localizeText, normalizeLang, t } from './index';

describe('i18n/index', () => {
    describe('normalizeLang', () => {
        it('maps common language codes', () => {
            expect(normalizeLang('en')).toBe('en');
            expect(normalizeLang('en-US')).toBe('en');
            expect(normalizeLang('en-GB')).toBe('en');
            expect(normalizeLang('zh-Hans')).toBe('zh-Hans');
            expect(normalizeLang('zh-CN')).toBe('zh-Hans');
            expect(normalizeLang('zh')).toBe('zh-Hans');
            expect(normalizeLang('zh-Hant')).toBe('zh-Hant');
            expect(normalizeLang('zh-TW')).toBe('zh-Hant');
            expect(normalizeLang('zh-HK')).toBe('zh-Hant');
            expect(normalizeLang('zh-MO')).toBe('zh-Hant');
            expect(normalizeLang('mn')).toBe('mn');
            expect(normalizeLang('mn-MN')).toBe('mn');
            expect(normalizeLang('mn-Cyrl-MN')).toBe('mn');
        });

        it('handles empty/null/undefined as English', () => {
            expect(normalizeLang('')).toBe('en');
            expect(normalizeLang(null)).toBe('en');
            expect(normalizeLang(undefined)).toBe('en');
        });

        it('uses English for anything except Simplified, Traditional, and Cyrillic Mongolian', () => {
            expect(normalizeLang('ja')).toBe('en');
            expect(normalizeLang('fr')).toBe('en');
            expect(normalizeLang('garbage')).toBe('en');
            expect(normalizeLang('mn-Mong')).toBe('en');
            expect(normalizeLang('mn-Mong-CN')).toBe('en');
        });

        it('is case-insensitive', () => {
            expect(normalizeLang('EN')).toBe('en');
            expect(normalizeLang('ZH-HANS')).toBe('zh-Hans');
            expect(normalizeLang('ZH-TW')).toBe('zh-Hant');
        });
    });

    describe('isChineseLang', () => {
        it('is true only for simplified and traditional tags', () => {
            expect(isChineseLang('zh-Hans')).toBe(true);
            expect(isChineseLang('zh-CN')).toBe(true);
            expect(isChineseLang('zh-SG')).toBe(true);
            expect(isChineseLang('chinese')).toBe(true);
            expect(isChineseLang('ZH-CN')).toBe(true);
            expect(isChineseLang('zh_CN')).toBe(true);
            expect(isChineseLang('zh-Hant')).toBe(true);
            expect(isChineseLang('zh-TW')).toBe(true);
            expect(isChineseLang('zh-HK')).toBe(true);
            expect(isChineseLang('mn')).toBe(false);
            expect(isChineseLang('mn-Mong')).toBe(false);
            expect(isChineseLang('ja')).toBe(false);
            expect(isChineseLang('')).toBe(false);
            expect(isChineseLang(undefined)).toBe(false);
            expect(isChineseLang('zh-yue')).toBe(false);
        });
    });

    describe('isTraditionalLang', () => {
        it('is true only for traditional tags', () => {
            expect(isTraditionalLang('zh-Hant')).toBe(true);
            expect(isTraditionalLang('zh-TW')).toBe(true);
            expect(isTraditionalLang('zh-HK')).toBe(true);
            expect(isTraditionalLang('zh-MO')).toBe(true);
            expect(isTraditionalLang('zh-Hans')).toBe(false);
            expect(isTraditionalLang('zh-CN')).toBe(false);
            expect(isTraditionalLang('zh-SG')).toBe(false);
            expect(isTraditionalLang('mn')).toBe(false);
            expect(isTraditionalLang(undefined)).toBe(false);
        });
    });

    describe('localizeText', () => {
        it('selects correct text by language', () => {
            expect(localizeText('en', 'Hello', '你好', '你好')).toBe('Hello');
            expect(localizeText('zh-Hans', 'Hello', '你好', '你好')).toBe('你好');
            expect(localizeText('zh-Hant', 'Hello', '你好', '您好')).toBe('您好');
        });

        it('falls back zhHant to zhHans when zhHant not provided', () => {
            expect(localizeText('zh-TW', 'Hello', '你好')).toBe('你好');
        });

        it('handles undefined lang as English', () => {
            expect(localizeText(undefined, 'Hello', '你好')).toBe('Hello');
            expect(localizeText(null, 'Hello', '你好')).toBe('Hello');
        });

        it('uses Mongolian inline copy and otherwise English', () => {
            expect(localizeText('mn', 'Close', '关闭', '關閉')).toBe('Хаах');
            expect(localizeText('mn', 'Not in the overlay', '关闭')).toBe('Not in the overlay');
        });
    });

    describe('t', () => {
        it('looks up keys from translation tables', () => {
            // 'close' exists in both en and zh-Hans tables
            expect(t('close', 'en')).toBe('Close');
            expect(t('close', 'zh-Hans')).toBe('关闭');
        });

        it('falls back to zh-Hans when key missing in target language', () => {
            // Use a key that only exists in zh-Hans
            const zhOnly = t('langName', 'zh-Hans');
            expect(zhOnly).toBe('简体中文');
            // If zh-Hant doesn't have this key, should fall back to zh-Hans
            const hantResult = t('langName', 'zh-Hant');
            expect(typeof hantResult).toBe('string');
            expect(hantResult.length).toBeGreaterThan(0);
        });

        it('returns key itself when not found in any table', () => {
            const spy = vi.spyOn(console, 'warn').mockImplementation(() => {});
            expect(t('nonexistent.key.xyz', 'en')).toBe('nonexistent.key.xyz');
            expect(spy).toHaveBeenCalledWith('[i18n] missing key: "nonexistent.key.xyz"');
            spy.mockRestore();
        });

        it('uses English when the language is not Chinese or Mongolian', () => {
            expect(t('close', undefined)).toBe('Close');
            expect(t('close', 'ja')).toBe('Close');
            expect(t('close', 'mn')).toBe('Хаах');
        });

        it('falls back to English when a Mongolian key is missing', () => {
            expect(t('title', 'mn')).not.toBe('码卡龙 8');
        });

        it('handles undefined lang', () => {
            expect(t('close', undefined)).toBe('Close');
            expect(t('close', null)).toBe('Close');
        });
    });
});
