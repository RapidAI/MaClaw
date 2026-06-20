import { describe, expect, it } from 'vitest';
import { getSettingsTabOptions } from '../settingsTabs';

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
});
