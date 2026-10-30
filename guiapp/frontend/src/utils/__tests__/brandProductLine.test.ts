import { describe, expect, it } from 'vitest';
import {
    BRAND_LINE_VERSION,
    brandLineDefaultCN,
    brandLineGeneration,
    brandLineParts,
    brandLineProductName,
    formatBrandLine,
    localizedBrandContact,
    localizedBrandSlogan,
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
        expect(brandLineDefaultCN('en')).toBe('MaClaw');
        expect(brandLineDefaultCN()).toBe('码卡龙');
        expect(brandLineGeneration({ lang: 'en' })).toBe('');
        expect(brandLineGeneration({})).toBe('企缘');
    });

    it('keeps the Chinese brand slogan and contact in Chinese UI', () => {
        expect(localizedBrandSlogan('AI Native 组织操作系统', 'zh-Hans', 'AI Native organizational operating system')).toBe('AI Native 组织操作系统');
        expect(localizedBrandSlogan('AI Native 组织操作系统', 'zh-Hant', 'AI Native organizational operating system')).toBe('AI Native 組織操作系統');
        expect(localizedBrandSlogan('AI Native 组织操作系统', 'zh-TW', 'AI Native 組織操作系統')).toBe('AI Native 組織操作系統');
        expect(localizedBrandContact('联系信息：微信 znsoft', 'zh-CN', 'Contact: WeChat znsoft')).toBe('联系信息：微信 znsoft');
        expect(localizedBrandContact('联系信息：QianXin', 'zh-Hans', 'Contact: WeChat znsoft')).toBe('联系信息：QianXin');
        expect(localizedBrandContact('联系信息：微信 znsoft', 'zh-Hant', '商業合作：微信 znsoft')).toBe('聯絡資訊：微信 znsoft');
        expect(localizedBrandContact('联系信息：QianXin', 'zh-TW', '商業合作：微信 znsoft')).toBe('聯絡資訊：QianXin');
    });

    it('translates the Chinese brand slogan and contact for Mongolian and English', () => {
        expect(localizedBrandSlogan('AI Native 组织操作系统', 'mn', 'AI Native байгууллагын үйлдлийн систем')).toBe('AI Native байгууллагын үйлдлийн систем');
        expect(localizedBrandSlogan('AI Native 组织操作系统', 'en', 'AI Native organizational operating system')).toBe('AI Native organizational operating system');
        expect(localizedBrandSlogan('', 'mn', 'AI Native байгууллагын үйлдлийн систем')).toBe('AI Native байгууллагын үйлдлийн систем');
        expect(localizedBrandContact('联系信息：微信 znsoft', 'mn', 'Холбоо барих: WeChat znsoft')).toBe('Холбоо барих: WeChat znsoft');
        expect(localizedBrandContact('联系信息：QianXin', 'mn-MN', 'Холбоо барих: WeChat znsoft')).toBe('Холбоо барих: QianXin');
        expect(localizedBrandContact('联系信息：微信 znsoft', 'en', 'Contact: WeChat znsoft')).toBe('Contact: WeChat znsoft');
        expect(localizedBrandContact('Contact: QianXin', 'mn', 'Холбоо барих: WeChat znsoft')).toBe('Contact: QianXin');
        expect(localizedBrandContact('商业合作：微信 znsoft', 'mn', 'Холбоо барих: WeChat znsoft')).toBe('商业合作：微信 znsoft');
        expect(localizedBrandContact('', 'mn', 'Холбоо барих: WeChat znsoft')).toBe('Холбоо барих: WeChat znsoft');
    });

    it('uses the Latin OEM name and a bare version mark in Mongolian', () => {
        expect(brandLineParts({ lang: 'mn', localizedDefault: 'MaClaw 8' })).toEqual({
            name: 'MaClaw',
            version: BRAND_LINE_VERSION,
            generation: '',
        });
        expect(brandLineProductName({
            brandId: 'qianxin',
            displayName: 'TigerClaw',
            displayNameCN: '虎爪',
            lang: 'mn',
            localizedDefault: 'MaClaw 8',
        })).toBe('TigerClaw 8');
        expect(brandLineProductName({
            brandId: 'qianxin',
            displayNameCN: '虎爪',
            lang: 'zh-Hans',
        })).toBe('虎爪 8 企缘');
        expect(brandLineProductName({
            brandId: 'qianxin',
            displayName: 'TigerClaw',
            displayNameCN: '虎爪',
            lang: 'en',
        })).toBe('TigerClaw 8');
        expect(brandLineParts({ lang: 'en', localizedDefault: 'MaClaw 8' })).toEqual({
            name: 'MaClaw',
            version: BRAND_LINE_VERSION,
            generation: '',
        });
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
