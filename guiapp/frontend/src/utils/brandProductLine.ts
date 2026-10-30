import { isChineseLang, isTraditionalLang, normalizeLang } from '../i18n/langSelect';

export const BRAND_LINE_VERSION = '8';
export const BRAND_LINE_GENERATION_HANS = '企缘';
export const BRAND_LINE_GENERATION_HANT = '企緣';
export const BRAND_LINE_DEFAULT_CN_HANS = '码卡龙';
export const BRAND_LINE_DEFAULT_CN_HANT = '碼卡龍';

const brandLineWithGeneration = new RegExp(`^(.*?)\\s+${BRAND_LINE_VERSION}\\s+(\\S+)\\s*$`);
const brandLineBareVersion = new RegExp(`^(.*?)\\s+${BRAND_LINE_VERSION}\\s*$`);

export type BrandLineInput = {
    brandId?: string | null;
    /** Latin product name. Non-Chinese OEM titles prefer this over displayNameCN. */
    displayName?: string | null;
    displayNameCN?: string | null;
    lang?: string;
    localizedDefault?: string;
};

export type BrandLineParts = {
    name: string;
    version: string;
    generation: string;
};

export function formatBrandLine(parts: BrandLineParts): string {
    return [parts.name, parts.version, parts.generation].filter(Boolean).join(' ');
}

export function brandLineGeneration(input: Pick<BrandLineInput, 'lang' | 'localizedDefault'> = {}): string {
    const raw = String(input.lang || '').trim();
    const normalized = normalizeLang(raw);
    if (normalized === 'mn') return '';
    if (normalized === 'zh-Hant') return BRAND_LINE_GENERATION_HANT;
    // An omitted language keeps the historical simplified lockup. An explicit
    // non-Chinese language, including English, does not.
    if (!raw || normalized === 'zh-Hans') {
        const fallback = String(input.localizedDefault || '');
        if (fallback.includes(BRAND_LINE_GENERATION_HANT) || fallback.includes(BRAND_LINE_DEFAULT_CN_HANT)) {
            return BRAND_LINE_GENERATION_HANT;
        }
        return BRAND_LINE_GENERATION_HANS;
    }
    return '';
}

export function brandLineDefaultCN(lang?: string): string {
    const raw = String(lang || '').trim();
    if (!raw) return BRAND_LINE_DEFAULT_CN_HANS;
    const normalized = normalizeLang(raw);
    if (normalized === 'zh-Hant') return BRAND_LINE_DEFAULT_CN_HANT;
    if (normalized === 'zh-Hans') return BRAND_LINE_DEFAULT_CN_HANS;
    return 'MaClaw';
}

function splitBrandLine(full: string): BrandLineParts | null {
    const withGeneration = full.match(brandLineWithGeneration);
    if (withGeneration) {
        const name = withGeneration[1].trim();
        if (!name) return null;
        return { name, version: BRAND_LINE_VERSION, generation: withGeneration[2] };
    }
    const bare = full.match(brandLineBareVersion);
    if (!bare) return null;
    const name = bare[1].trim();
    if (!name) return null;
    return { name, version: BRAND_LINE_VERSION, generation: '' };
}

export function brandLineParts(input: BrandLineInput = {}): BrandLineParts {
    const generation = brandLineGeneration(input);
    const oemCN = String(input.displayNameCN || '').trim();
    const oemLatin = String(input.displayName || '').trim();
    if (input.brandId && input.brandId !== 'maclaw') {
        const name = generation === '' && oemLatin ? oemLatin : oemCN;
        return { name, version: BRAND_LINE_VERSION, generation };
    }
    const localized = String(input.localizedDefault || '').trim();
    if (localized) {
        return splitBrandLine(localized) || { name: localized, version: '', generation: '' };
    }
    return { name: brandLineDefaultCN(input.lang), version: BRAND_LINE_VERSION, generation };
}

export function brandLineProductName(input: BrandLineInput = {}): string {
    return formatBrandLine(brandLineParts(input));
}

/** Canonical Simplified Chinese slogan shipped on every brand payload. */
export const BRAND_SLOGAN_ZH = 'AI Native 组织操作系统';
/** Traditional form of the canonical slogan. The brand payload is simplified. */
export const BRAND_SLOGAN_HANT = 'AI Native 組織操作系統';
const brandContactPrefixHant = '聯絡資訊';

const brandContactPrefixZh = /^联系信息\s*[:：]\s*/;
const brandContactWeChatDetail = 'WeChat znsoft';

/**
 * Brand payloads store the slogan in Simplified Chinese. Simplified UI keeps
 * that string. Traditional UI uses the traditional form of the canonical
 * slogan. Other languages use the catalog sentence when the payload is empty
 * or still the canonical Chinese slogan.
 */
export function localizedBrandSlogan(raw: string | null | undefined, lang: string | undefined | null, catalogSlogan: string): string {
    const value = String(raw || '').trim();
    const catalog = String(catalogSlogan || '').trim();
    if (isTraditionalLang(lang) && (!value || value === BRAND_SLOGAN_ZH)) return BRAND_SLOGAN_HANT;
    if (isChineseLang(lang)) return value || catalog;
    if (!value || value === BRAND_SLOGAN_ZH) return catalog || value;
    return value;
}

/**
 * Brand payloads store the contact line in Simplified Chinese
 * (`联系信息：微信 znsoft`, `联系信息：QianXin`). Chinese UI keeps that string.
 * A line that does not start with that prefix is returned unchanged, including
 * one that merely contains 微信. The default WeChat line uses the catalog.
 * Other prefixed lines keep the detail and translate only the prefix.
 */
export function localizedBrandContact(raw: string | null | undefined, lang: string | undefined | null, catalogContact: string): string {
    const value = String(raw || '').trim();
    const catalog = String(catalogContact || '').trim();
    if (isChineseLang(lang)) {
        if (!value) return catalog;
        if (isTraditionalLang(lang)) return value.replace(/^联系信息/, brandContactPrefixHant);
        return value;
    }
    if (!value) return catalog;
    if (!brandContactPrefixZh.test(value)) return value;
    const detail = value.replace(brandContactPrefixZh, '').replace(/微信/g, 'WeChat').trim();
    if (!detail) return catalog;
    if (detail === brandContactWeChatDetail && catalog) return catalog;
    const splitAt = catalog.indexOf(':');
    const catalogPrefix = splitAt > 0 ? catalog.slice(0, splitAt).trim() : '';
    const prefix = catalogPrefix || (normalizeLang(lang) === 'mn' ? 'Холбоо барих' : 'Contact');
    return `${prefix}: ${detail}`;
}
