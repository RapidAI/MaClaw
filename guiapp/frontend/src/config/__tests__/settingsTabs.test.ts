import { describe, expect, it } from 'vitest';
import {
    getSettingsTabOptions,
    isSettingsContentTabId,
    resolveSettingsTabId,
    SETTINGS_CONTENT_TAB_IDS,
} from '../settingsTabs';

describe('getSettingsTabOptions', () => {
    it('places the system group last in the rail order', () => {
        const ids = getSettingsTabOptions('en').map((tab) => tab.id);

        expect(ids.slice(-5)).toEqual(['redeem', 'tokenBank', 'security', 'system', 'assetManagement']);
    });

    it('uses the Chinese migration label', () => {
        const migration = getSettingsTabOptions('zh-Hans').find((tab) => tab.id === 'migration');

        expect(migration?.label).toBe('\u8fc1\u51fa\u4e0e\u8fc1\u5165');
    });

    it('keeps core tabs including general when virtual employee is hidden', () => {
        const ids = getSettingsTabOptions('en', { hideVirtualEmployee: true }).map((tab) => tab.id);

        expect(ids).toContain('general');
        expect(ids).toContain('llm');
        expect(ids).toContain('im');
        expect(ids).not.toContain('virtualEmployee');
    });

    it('lists the same content tabs as SETTINGS_CONTENT_TAB_IDS', () => {
        expect(getSettingsTabOptions('en').map((tab) => tab.id)).toEqual([...SETTINGS_CONTENT_TAB_IDS]);
    });

    it('assigns a localized group and group label to every content tab', () => {
        const enTabs = getSettingsTabOptions('en');
        expect(enTabs.every((tab) => Boolean(tab.group) && Boolean(tab.groupLabel))).toBe(true);

        const general = enTabs.find((tab) => tab.id === 'general');
        expect(general?.group).toBe('essentials');
        expect(general?.groupLabel).toBe('Essentials');

        const llm = enTabs.find((tab) => tab.id === 'llm');
        expect(llm?.group).toBe('ai');
        expect(llm?.groupLabel).toBe('AI & Models');

        const zhGeneral = getSettingsTabOptions('zh-Hans').find((tab) => tab.id === 'general');
        expect(zhGeneral?.groupLabel).toBe('基础设置');

        const zhSystem = getSettingsTabOptions('zh-Hant').find((tab) => tab.id === 'system');
        expect(zhSystem?.group).toBe('system');
        expect(zhSystem?.groupLabel).toBe('系統');
    });

    it('keeps service, security, and assets in System', () => {
        const zh = getSettingsTabOptions('zh-Hans');
        const tokenBank = zh.find((tab) => tab.id === 'tokenBank');
        expect(tokenBank?.group).toBe('system');
        expect(tokenBank?.label).toBe('Token 银行');
        expect(tokenBank?.groupLabel).toBe('系统');
        expect(zh.find((tab) => tab.id === 'assetManagement')?.group).toBe('system');

        const system = zh.filter((tab) => tab.group === 'system').map((tab) => tab.id);
        expect(system).toEqual(['redeem', 'tokenBank', 'security', 'system', 'assetManagement']);

        expect(getSettingsTabOptions('zh-Hant').find((tab) => tab.id === 'tokenBank')?.label).toBe('Token 銀行');
        expect(getSettingsTabOptions('en').find((tab) => tab.id === 'tokenBank')?.label).toBe('Token Bank');
    });

    it('keeps memory, knowledge, MIS data, and migration in Data & Memory', () => {
        const zh = getSettingsTabOptions('zh-Hans');
        const data = zh.filter((tab) => tab.group === 'data').map((tab) => tab.id);
        expect(data).toEqual(['memory', 'knowledge', 'misData', 'migration']);

        const migration = zh.find((tab) => tab.id === 'migration');
        expect(migration?.label).toBe('迁出与迁入');
        expect(migration?.groupLabel).toBe('数据与记忆');
    });

    it('groups employees, IM, email, and hardware under Integrations', () => {
        const zh = getSettingsTabOptions('zh-Hans');
        const integration = zh.filter((tab) => tab.group === 'integration').map((tab) => tab.id);
        expect(integration).toEqual(['virtualEmployee', 'im', 'email', 'hardware']);

        const im = zh.find((tab) => tab.id === 'im');
        expect(im?.groupLabel).toBe('集成与接入');
        expect(zh.find((tab) => tab.id === 'email')?.label).toBe('邮箱');
        expect(getSettingsTabOptions('zh-Hant').find((tab) => tab.id === 'im')?.groupLabel).toBe('整合與接入');
        expect(getSettingsTabOptions('en').find((tab) => tab.id === 'im')?.groupLabel).toBe('Integrations');
        expect(getSettingsTabOptions('zh-Hant').find((tab) => tab.id === 'email')?.label).toBe('郵箱');
        expect(getSettingsTabOptions('en').find((tab) => tab.id === 'email')?.label).toBe('Email');

        const hidden = getSettingsTabOptions('zh-Hans', { hideVirtualEmployee: true });
        expect(hidden.filter((tab) => tab.group === 'integration').map((tab) => tab.id)).toEqual(['im', 'email', 'hardware']);
        expect(hidden.find((tab) => tab.id === 'assetManagement')?.group).toBe('system');
    });

    it('keeps group order stable by first appearance', () => {
        const groups = getSettingsTabOptions('en')
            .map((tab) => tab.group)
            .filter((group, index, all): group is NonNullable<typeof group> => Boolean(group) && all.indexOf(group) === index);

        expect(groups).toEqual(['essentials', 'ai', 'data', 'integration', 'system']);
    });
});

describe('resolveSettingsTabId', () => {
    it('accepts content tabs and rejects legacy/unknown ids', () => {
        expect(isSettingsContentTabId('llm')).toBe(true);
        expect(isSettingsContentTabId('skills')).toBe(false);
        expect(resolveSettingsTabId('llm')).toBe('llm');
        expect(resolveSettingsTabId('skills')).toBe('general');
        expect(resolveSettingsTabId('not-a-tab')).toBe('general');
        expect(resolveSettingsTabId('virtualEmployee', { hideVirtualEmployee: true })).toBe('general');
        expect(resolveSettingsTabId('virtualEmployee', { hideVirtualEmployee: false })).toBe('virtualEmployee');
    });
});
