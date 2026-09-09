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

/** Dedicated rail/page labels after separating Tools from AI Experts. */
const toolsTitle: UtilitiesLabelPack = {
    en: 'Tools',
    zhHans: '工具',
    zhHant: '工具',
};

const expertsTitle: UtilitiesLabelPack = {
    en: 'AI Experts',
    zhHans: 'AI 专家',
    zhHant: 'AI 專家',
};

export function toolsNavLabel(lang?: string | null): string {
    return pickUtilitiesLabel(lang, toolsTitle);
}

export function toolsPageTitle(lang?: string | null): string {
    return pickUtilitiesLabel(lang, toolsTitle);
}

export function expertsNavLabel(lang?: string | null): string {
    return pickUtilitiesLabel(lang, expertsTitle);
}

export function expertsPageTitle(lang?: string | null): string {
    return pickUtilitiesLabel(lang, expertsTitle);
}

const toolsEntry: UtilitiesLabelPack = {
    en: `${toolsTitle.en} entry`,
    zhHans: `${toolsTitle.zhHans}入口`,
    zhHant: `${toolsTitle.zhHant}入口`,
};

const toolsBack: UtilitiesLabelPack = {
    en: `Back to ${toolsTitle.en}`,
    zhHans: `返回${toolsTitle.zhHans}`,
    zhHant: `返回${toolsTitle.zhHant}`,
};

const toolsBackHint: UtilitiesLabelPack = {
    en: `Return to the ${toolsTitle.en} page`,
    zhHans: `返回${toolsTitle.zhHans}页面`,
    zhHant: `返回${toolsTitle.zhHant}頁面`,
};

const expertsEntry: UtilitiesLabelPack = {
    en: `${expertsTitle.en} entry`,
    zhHans: `${expertsTitle.zhHans}入口`,
    zhHant: `${expertsTitle.zhHant}入口`,
};

export function toolsEntryLabel(lang?: string | null): string {
    return pickUtilitiesLabel(lang, toolsEntry);
}

export function toolsBackLabel(lang?: string | null): string {
    return pickUtilitiesLabel(lang, toolsBack);
}

export function toolsBackHintLabel(lang?: string | null): string {
    return pickUtilitiesLabel(lang, toolsBackHint);
}

export function expertsEntryLabel(lang?: string | null): string {
    return pickUtilitiesLabel(lang, expertsEntry);
}
