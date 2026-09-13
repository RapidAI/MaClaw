import { normalizeLang } from '../i18n/langSelect';

export const BRAND_LINE_VERSION = '8';
export const BRAND_LINE_GENERATION_HANS = '企缘';
export const BRAND_LINE_GENERATION_HANT = '企緣';
export const BRAND_LINE_DEFAULT_CN_HANS = '码卡龙';
export const BRAND_LINE_DEFAULT_CN_HANT = '碼卡龍';

export type BrandLineInput = {
    brandId?: string | null;
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
    if (normalizeLang(input.lang) === 'zh-Hant') return BRAND_LINE_GENERATION_HANT;
    const fallback = String(input.localizedDefault || '');
    if (fallback.includes(BRAND_LINE_GENERATION_HANT) || fallback.includes(BRAND_LINE_DEFAULT_CN_HANT)) {
        return BRAND_LINE_GENERATION_HANT;
    }
    return BRAND_LINE_GENERATION_HANS;
}

export function brandLineDefaultCN(lang?: string): string {
    return normalizeLang(lang) === 'zh-Hant' ? BRAND_LINE_DEFAULT_CN_HANT : BRAND_LINE_DEFAULT_CN_HANS;
}

function splitBrandLine(full: string): BrandLineParts | null {
    const match = full.match(new RegExp(`^(.*?)\\s+${BRAND_LINE_VERSION}\\s+(\\S+)\\s*$`));
    if (!match) return null;
    const name = match[1].trim();
    if (!name) return null;
    return { name, version: BRAND_LINE_VERSION, generation: match[2] };
}

export function brandLineParts(input: BrandLineInput = {}): BrandLineParts {
    const generation = brandLineGeneration(input);
    const oemCN = String(input.displayNameCN || '').trim();
    if (input.brandId && input.brandId !== 'maclaw') {
        return { name: oemCN, version: BRAND_LINE_VERSION, generation };
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
