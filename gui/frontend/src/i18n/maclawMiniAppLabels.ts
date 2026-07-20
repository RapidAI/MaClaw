/**
 * Canonical product naming for the MaClaw mini-program feature.
 *
 * Chinese product: 码卡龙小程序
 * English product: MaClaw MiniAPP
 * Short nav/badge: 小程序 / MiniAPP
 *
 * Atomic names live in `miniAppNames`. Derived UI sentences compose from them
 * so a product rename only needs one place updated.
 */
import { localizeText } from './langSelect';

export type MiniAppLabelPack = { en: string; zhHans: string; zhHant: string };

/** Atomic product names — edit these when branding changes. */
export const miniAppNames = {
    short: {
        en: 'MiniAPP',
        zhHans: '小程序',
        zhHant: '小程序',
    },
    product: {
        en: 'MaClaw MiniAPP',
        zhHans: '码卡龙小程序',
        zhHant: '碼卡龍小程序',
    },
} as const satisfies Record<string, MiniAppLabelPack>;

const short = miniAppNames.short;
const product = miniAppNames.product;

const panel: MiniAppLabelPack = {
    en: `${short.en} panel`,
    zhHans: `${short.zhHans}面板`,
    zhHant: `${short.zhHant}面板`,
};

const studio: MiniAppLabelPack = {
    en: `${product.en} Studio`,
    zhHans: `${product.zhHans}工作室`,
    zhHant: `${product.zhHant}工作室`,
};

const skill: MiniAppLabelPack = {
    en: `${product.en} Skill`,
    zhHans: `${product.zhHans}技能`,
    zhHant: `${product.zhHant}技能`,
};

/** Compact skill label for forms / snapshots / tooltips (shorter than full product skill name). */
const skillField: MiniAppLabelPack = {
    en: `${short.en} Skill`,
    zhHans: `${short.zhHans}技能`,
    zhHant: `${short.zhHant}技能`,
};

/** Full UI packs used by settings, sidebar, studio, and skills surfaces. */
export const miniAppLabels = {
    short,
    product,
    entry: {
        en: `${short.en} entry`,
        zhHans: `${short.zhHans}入口`,
        zhHant: `${short.zhHant}入口`,
    },
    studio,
    studioManual: {
        en: `${short.en} Studio manual`,
        zhHans: '使用说明',
        zhHant: '使用說明',
    },
    studioManualAria: {
        en: `Open the ${studio.en} manual`,
        zhHans: `打开${studio.zhHans}使用说明`,
        zhHant: `開啟${studio.zhHant}使用說明`,
    },
    skill,
    skillField,
    panel,
    openPanel: {
        en: `Open ${short.en} Panel`,
        zhHans: `打开${panel.zhHans}`,
        zhHant: `開啟${panel.zhHant}`,
    },
    defaultStudioDescription: {
        en: `${short.en} entry created in ${studio.en}.`,
        zhHans: `由${studio.zhHans}创建的${short.zhHans}入口。`,
        zhHant: `由${studio.zhHant}建立的${short.zhHant}入口。`,
    },
    missingDefinition: {
        en: `Skill installed, but no installable ${product.en} definition was found (missing maclaw.app.json).`,
        zhHans: `技能已下载，但未发现可安装的${product.zhHans}定义（缺少 maclaw.app.json）。`,
        zhHant: `技能已下載，但未發現可安裝的${product.zhHant}定義（缺少 maclaw.app.json）。`,
    },
    publishRequiresPanelTest: {
        en: `Save to Skill and run this version successfully in the ${panel.en} before uploading to SkillMarket.`,
        zhHans: `请先保存到 Skill，并在${panel.zhHans}成功测试一次当前版本，再上传到 SkillMarket。`,
        zhHant: `請先儲存到 Skill，並在${panel.zhHant}成功測試一次當前版本，再上傳到 SkillMarket。`,
    },
    skillAppsSyncedMeta: {
        en: `Found from installed capabilities and synced to the ${panel.en}`,
        zhHans: `从已安装能力中找到，已自动同步到左侧${panel.zhHans}`,
        zhHant: `從已安裝能力中找到，已自動同步到左側${panel.zhHant}`,
    },
    manualMissingHub: {
        en: `Hub URL is missing, so the ${studio.en} manual cannot be opened.`,
        zhHans: `Hub 地址缺失，暂时无法打开${studio.zhHans}使用说明。`,
        zhHant: `Hub 位址缺失，暫時無法打開${studio.zhHant}使用說明。`,
    },
    manualOpenFailed: {
        en: `Failed to open the ${studio.en} manual`,
        zhHans: `打开${studio.zhHans}使用说明失败`,
        zhHant: `打開${studio.zhHant}使用說明失敗`,
    },
    noSkillsYet: {
        en: `No ${product.en} skills yet`,
        zhHans: `暂无${skill.zhHans}`,
        zhHant: `暫無${skill.zhHant}`,
    },
    skillsHint: {
        en: `${product.en} skills contain maclaw.app.json or maclaw.apps.json and can be opened from the ${panel.en} after registration.`,
        zhHans: `${skill.zhHans}包含 maclaw.app.json 或 maclaw.apps.json；注册后可加入${panel.zhHans}打开。`,
        zhHant: `${skill.zhHant}包含 maclaw.app.json 或 maclaw.apps.json；註冊後可加入${panel.zhHant}開啟。`,
    },
    emptyMarketBrowse: {
        en: `No ${product.en} skills. Install from the Capability Market.`,
        zhHans: `暂无${skill.zhHans}。请在能力市场安装。`,
        zhHant: `暫無${skill.zhHant}。請在能力市場安裝。`,
    },
    browseMarketSkills: {
        en: `Browse the Market to find ${product.en} skills →`,
        zhHans: `前往能力市场搜索${skill.zhHans} →`,
        zhHant: `前往能力市場搜尋${skill.zhHant} →`,
    },
    definitionColumn: {
        en: `${short.en} definition`,
        zhHans: `${short.zhHans}定义`,
        zhHant: `${short.zhHant}定義`,
    },
    countColumn: {
        en: short.en,
        zhHans: `${short.zhHans}数`,
        zhHant: `${short.zhHant}數`,
    },
    /** Compact edit action (tooltip / aria) — derived from skillField. */
    editSkill: {
        en: `Edit ${skillField.en}`,
        zhHans: `编辑${skillField.zhHans}`,
        zhHant: `編輯${skillField.zhHant}`,
    },
} as const;

/** Pick a pack by app language (en / zh-Hans / zh-Hant). */
export function pickMiniAppLabel(lang: string | undefined | null, pack: MiniAppLabelPack): string {
    return localizeText(lang, pack.en, pack.zhHans, pack.zhHant);
}

export function miniAppShortLabel(lang?: string | null): string {
    return pickMiniAppLabel(lang, miniAppLabels.short);
}

export function miniAppEntryLabel(lang?: string | null): string {
    return pickMiniAppLabel(lang, miniAppLabels.entry);
}

/** For components that use localizeText(en, zhHans, zhHant) without a lang arg. */
export function localizeMiniAppPack(
    localize: (en: string, zhHans: string, zhHant: string) => string,
    pack: MiniAppLabelPack,
): string {
    return localize(pack.en, pack.zhHans, pack.zhHant);
}

/** "1 MaClaw MiniAPP skill" / "3个码卡龙小程序技能" — EN pluralizes; ZH has no space before 个. */
export function formatMiniAppSkillCount(
    count: number,
    localize: (en: string, zhHans: string, zhHant: string) => string,
): string {
    const n = Number.isFinite(count) ? Math.max(0, Math.floor(count)) : 0;
    const enUnit = n === 1 ? 'skill' : 'skills';
    return localize(
        `${n} ${product.en} ${enUnit}`,
        `${n}个${product.zhHans}技能`,
        `${n}個${product.zhHant}技能`,
    );
}

/** Install success toast that points users at the MiniAPP panel. */
export function formatInstalledOpenPanelMessage(
    name: string,
    localize: (en: string, zhHans: string, zhHant: string) => string,
): string {
    const safe = String(name || '').trim() || product.en;
    return localize(
        `"${safe}" installed! Open the ${panel.en} to use it.`,
        `「${safe}」安装成功！打开${panel.zhHans}即可使用。`,
        `「${safe}」安裝成功！開啟${panel.zhHant}即可使用。`,
    );
}
