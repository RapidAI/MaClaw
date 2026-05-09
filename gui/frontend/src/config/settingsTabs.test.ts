import { describe, expect, it } from 'vitest';
import { getSettingsTabOptions } from './settingsTabs';

describe('getSettingsTabOptions', () => {
    it('hides AgentNet when requested', () => {
        const tabs = getSettingsTabOptions('en', { hideAgentNet: true });

        expect(tabs.some(tab => tab.id === 'agentnet')).toBe(false);
    });

    it('keeps AgentNet by default', () => {
        const tabs = getSettingsTabOptions('en');

        expect(tabs.some(tab => tab.id === 'agentnet')).toBe(true);
    });

    it('includes Search Engine settings tab', () => {
        const tabs = getSettingsTabOptions('zh-Hans');
        const tab = tabs.find(item => item.id === 'searchEngine');

        expect(tab?.label).toBe('搜索引擎');
    });
});
