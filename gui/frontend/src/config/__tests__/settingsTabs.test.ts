import { describe, expect, it } from 'vitest';
import {
    getSettingsTabOptions,
    isSettingsContentTabId,
    resolveSettingsTabId,
    SETTINGS_CONTENT_TAB_IDS,
} from '../settingsTabs';

describe('getSettingsTabOptions', () => {
    it('places migration after security and before system', () => {
        const ids = getSettingsTabOptions('en').map((tab) => tab.id);
        const securityIndex = ids.indexOf('security');

        expect(securityIndex).toBeGreaterThanOrEqual(0);
        expect(ids.slice(securityIndex, securityIndex + 3)).toEqual(['security', 'migration', 'system']);
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
