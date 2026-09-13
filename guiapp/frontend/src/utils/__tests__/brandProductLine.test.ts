import { describe, expect, it } from 'vitest';
import {
    BRAND_LINE_VERSION,
    brandLineDefaultCN,
    brandLineGeneration,
    brandLineParts,
    brandLineProductName,
    formatBrandLine,
} from '../brandProductLine';

describe('brandProductLine', () => {
    it('uses the localized default name for MaClaw and missing brand', () => {
        expect(brandLineProductName({ localizedDefault: '码卡龙 8 企缘' })).toBe('码卡龙 8 企缘');
        expect(brandLineProductName({ brandId: 'maclaw', localizedDefault: '碼卡龍 8 企緣' })).toBe('碼卡龍 8 企緣');
        expect(brandLineProductName({ localizedDefault: 'MaClaw Bedrock' })).toBe('MaClaw Bedrock');
    });

    it('composes OEM names from displayNameCN plus the generation mark', () => {
        expect(brandLineProductName({
            brandId: 'qianxin',
            displayNameCN: '虎爪',
            localizedDefault: '码卡龙 8 企缘',
        })).toBe('虎爪 8 企缘');
        expect(brandLineProductName({
            brandId: 'metastaff',
            displayNameCN: '智员',
            localizedDefault: '码卡龙 8 企缘',
        })).toBe('智员 8 企缘');
        expect(brandLineProductName({
            brandId: 'future-oem',
            displayNameCN: '新牌',
            localizedDefault: '码卡龙 8 企缘',
        })).toBe('新牌 8 企缘');
    });

    it('does not fall back to the MaClaw name for a known OEM without displayNameCN', () => {
        expect(brandLineProductName({
            brandId: 'qianxin',
            localizedDefault: '码卡龙 8 企缘',
        })).toBe('8 企缘');
        expect(brandLineParts({ brandId: 'qianxin' })).toEqual({
            name: '',
            version: BRAND_LINE_VERSION,
            generation: '企缘',
        });
    });

    it('uses traditional generation with zh-Hant aliases or a traditional default', () => {
        expect(brandLineGeneration({ lang: 'zh-Hant' })).toBe('企緣');
        expect(brandLineGeneration({ lang: 'zh-TW' })).toBe('企緣');
        expect(brandLineGeneration({ localizedDefault: '碼卡龍 8 企緣' })).toBe('企緣');
        expect(brandLineProductName({
            brandId: 'qianxin',
            displayNameCN: '虎爪',
            lang: 'zh-Hant',
        })).toBe('虎爪 8 企緣');
        expect(brandLineDefaultCN('zh-hk')).toBe('碼卡龍');
    });

    it('joins parts and falls back to the default MaClaw line', () => {
        expect(formatBrandLine({ name: '码卡龙', version: '8', generation: '企缘' })).toBe('码卡龙 8 企缘');
        expect(formatBrandLine({ name: 'MaClaw Bedrock', version: '', generation: '' })).toBe('MaClaw Bedrock');
        expect(brandLineParts({ lang: 'zh-Hans' })).toEqual({
            name: '码卡龙',
            version: BRAND_LINE_VERSION,
            generation: '企缘',
        });
    });
});
