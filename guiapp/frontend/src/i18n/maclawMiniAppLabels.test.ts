import { describe, expect, it } from 'vitest';
import {
    formatInstalledOpenPanelMessage,
    formatMiniAppSkillCount,
    localizeMiniAppPack,
    miniAppEntryLabel,
    miniAppLabels,
    miniAppNames,
    miniAppShortLabel,
    pickMiniAppLabel,
} from './maclawMiniAppLabels';

const zhFirst = (en: string, zhHans: string, _zhHant: string) => zhHans || en;
const enFirst = (en: string, _zhHans: string, _zhHant: string) => en;

describe('maclawMiniAppLabels', () => {
    it('uses short nav labels', () => {
        expect(miniAppShortLabel('en')).toBe('MiniAPP');
        expect(miniAppShortLabel('zh-Hans')).toBe('小程序');
        expect(miniAppShortLabel('zh-Hant')).toBe('小程序');
    });

    it('uses settings entry labels', () => {
        expect(miniAppEntryLabel('en')).toBe('MiniAPP entry');
        expect(miniAppEntryLabel('zh-Hans')).toBe('小程序入口');
        expect(miniAppEntryLabel('zh-Hant')).toBe('小程序入口');
    });

    it('derives product and studio names from atomic names', () => {
        expect(miniAppNames.product.en).toBe('MaClaw MiniAPP');
        expect(miniAppNames.product.zhHans).toBe('码卡龙小程序');
        expect(pickMiniAppLabel('en', miniAppLabels.studio)).toBe('MaClaw MiniAPP Studio');
        expect(pickMiniAppLabel('zh-Hans', miniAppLabels.studio)).toBe('码卡龙小程序工作室');
        expect(pickMiniAppLabel('zh-Hant', miniAppLabels.studio)).toBe('碼卡龍小程序工作室');
        expect(miniAppLabels.studio.en.startsWith(miniAppNames.product.en)).toBe(true);
        expect(miniAppLabels.panel.zhHans).toBe(`${miniAppNames.short.zhHans}面板`);
    });

    it('keeps panel-related copy on the MiniAPP product name', () => {
        expect(miniAppLabels.skillAppsSyncedMeta.en).toContain('MiniAPP panel');
        expect(miniAppLabels.skillAppsSyncedMeta.zhHans).toContain('小程序面板');
        expect(miniAppLabels.publishRequiresPanelTest.zhHans).toContain('小程序面板');
        expect(miniAppLabels.manualMissingHub.zhHans).toContain('码卡龙小程序工作室');
        expect(miniAppLabels.skillsHint.en).toContain(miniAppNames.product.en);
        expect(miniAppLabels.skillsHint.en).toContain(miniAppLabels.panel.en);
    });

    it('supports prop-style localize helpers', () => {
        expect(localizeMiniAppPack(zhFirst, miniAppLabels.skill)).toBe('码卡龙小程序技能');
        expect(localizeMiniAppPack(zhFirst, miniAppLabels.skillField)).toBe('小程序技能');
        expect(localizeMiniAppPack(enFirst, miniAppLabels.skillField)).toBe('MiniAPP Skill');
        expect(localizeMiniAppPack(zhFirst, miniAppLabels.short)).toBe('小程序');
    });

    it('formats skill counts with EN pluralization and no space before 个', () => {
        expect(formatMiniAppSkillCount(1, enFirst)).toBe('1 MaClaw MiniAPP skill');
        expect(formatMiniAppSkillCount(3, enFirst)).toBe('3 MaClaw MiniAPP skills');
        expect(formatMiniAppSkillCount(3, zhFirst)).toBe('3个码卡龙小程序技能');
        expect(formatMiniAppSkillCount(-1, zhFirst)).toBe('0个码卡龙小程序技能');
        expect(formatMiniAppSkillCount(0, enFirst)).toBe('0 MaClaw MiniAPP skills');
    });

    it('uses compact edit-skill labels', () => {
        expect(miniAppLabels.editSkill.en).toBe('Edit MiniAPP Skill');
        expect(miniAppLabels.editSkill.zhHans).toBe('编辑小程序技能');
        expect(miniAppLabels.editSkill.en.length).toBeLessThan(miniAppLabels.skill.en.length + 6);
    });

    it('formats install-open-panel toasts from panel labels', () => {
        expect(formatInstalledOpenPanelMessage('Invoice', enFirst)).toContain('MiniAPP panel');
        expect(formatInstalledOpenPanelMessage('Invoice', zhFirst)).toBe(
            '「Invoice」安装成功！打开小程序面板即可使用。',
        );
    });

    it('provides dependency source hint and warnings title in all languages', () => {
        for (const pack of [miniAppLabels.dependencySourceLocalHint, miniAppLabels.dependencyWarningsTitle]) {
            expect(pack.en.trim()).not.toBe('');
            expect(pack.zhHans.trim()).not.toBe('');
            expect(pack.zhHant.trim()).not.toBe('');
        }
        expect(miniAppLabels.dependencySourceLocalHint.en).toContain('embedded bundle');
        expect(miniAppLabels.dependencySourceLocalHint.zhHans).toContain('内嵌包');
        expect(miniAppLabels.dependencyWarningsTitle.zhHans).toBe('依赖警告');
    });
});
