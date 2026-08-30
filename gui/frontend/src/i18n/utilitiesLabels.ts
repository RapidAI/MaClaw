/**
 * Canonical naming for the left-rail "专家&工具" surface (utilities + AI experts).
 *
 * `title` is the product name (page, header, settings, back-links).
 * `short` is the 60px rail label: Chinese matches `title`; English is shortened
 * so it stays on one line like "Workflow" / "MiniAPP".
 */
import { localizeText } from './langSelect';

export type UtilitiesLabelPack = { en: string; zhHans: string; zhHant: string };

const title: UtilitiesLabelPack = {
    en: 'Experts & Tools',
    zhHans: '专家&工具',
    zhHant: '專家&工具',
};

const short: UtilitiesLabelPack = {
    en: 'Experts',
    zhHans: title.zhHans,
    zhHant: title.zhHant,
};

export const utilitiesLabels = {
    title,
    short,
    entry: {
        en: `${title.en} entry`,
        zhHans: `${title.zhHans}入口`,
        zhHant: `${title.zhHant}入口`,
    },
    back: {
        en: `Back to ${title.en}`,
        zhHans: `返回${title.zhHans}`,
        zhHant: `返回${title.zhHant}`,
    },
    backHint: {
        en: `Return to the ${title.en} page`,
        zhHans: `返回${title.zhHans}页面`,
        zhHant: `返回${title.zhHant}頁面`,
    },
} as const;

export function pickUtilitiesLabel(lang: string | undefined | null, pack: UtilitiesLabelPack): string {
    return localizeText(lang, pack.en, pack.zhHans, pack.zhHant);
}

export function utilitiesNavLabel(lang?: string | null): string {
    return pickUtilitiesLabel(lang, utilitiesLabels.short);
}

export function utilitiesPageTitle(lang?: string | null): string {
    return pickUtilitiesLabel(lang, utilitiesLabels.title);
}

export function utilitiesEntryLabel(lang?: string | null): string {
    return pickUtilitiesLabel(lang, utilitiesLabels.entry);
}

export function utilitiesBackLabel(lang?: string | null): string {
    return pickUtilitiesLabel(lang, utilitiesLabels.back);
}

export function utilitiesBackHint(lang?: string | null): string {
    return pickUtilitiesLabel(lang, utilitiesLabels.backHint);
}
