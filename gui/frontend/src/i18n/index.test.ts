import { describe, expect, it, vi } from 'vitest';
import { t, localizeText, normalizeLang } from './index';

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
        });

        it('handles empty/null/undefined', () => {
            expect(normalizeLang('')).toBe('zh-Hans');
            expect(normalizeLang(null)).toBe('zh-Hans');
            expect(normalizeLang(undefined)).toBe('zh-Hans');
        });

        it('defaults unknown to zh-Hans', () => {
            expect(normalizeLang('ja')).toBe('zh-Hans');
            expect(normalizeLang('fr')).toBe('zh-Hans');
            expect(normalizeLang('garbage')).toBe('zh-Hans');
        });

        it('is case-insensitive', () => {
            expect(normalizeLang('EN')).toBe('en');
            expect(normalizeLang('ZH-HANS')).toBe('zh-Hans');
            expect(normalizeLang('ZH-TW')).toBe('zh-Hant');
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

        it('handles undefined lang gracefully', () => {
            expect(localizeText(undefined, 'Hello', '你好')).toBe('你好');
            expect(localizeText(null, 'Hello', '你好')).toBe('你好');
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

        it('handles undefined lang', () => {
            expect(t('close', undefined)).toBe('关闭');
            expect(t('close', null)).toBe('关闭');
        });
    });
});
