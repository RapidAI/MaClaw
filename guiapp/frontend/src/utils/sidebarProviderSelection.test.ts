import { describe, expect, it } from 'vitest';
import { selectSidebarCurrentProvider, sidebarEditorUsageProfile, sidebarTurnUsageProfile, sumSidebarProfileUsage, sumSidebarProviderProfiles } from './sidebarProviderSelection';

describe('selectSidebarCurrentProvider', () => {
    it('keeps the configured current provider even when another provider has usage', () => {
        const selected = selectSidebarCurrentProvider(
            [
                { name: 'MaClaw Official', url: 'https://hub.example/v1', isHubService: true },
                { name: 'DeepSeek', url: 'https://deepseek.example/v1', isHubService: false },
            ],
            'DeepSeek',
            {
                'MaClaw Official': { total_tokens: 88000000 },
                DeepSeek: { total_tokens: 0 },
            },
        );

        expect(selected).toBe('DeepSeek');
    });

    it('falls back to provider with usage when current provider is unknown', () => {
        const selected = selectSidebarCurrentProvider(
            [
                { name: 'MaClaw Official', url: 'https://hub.example/v1', isHubService: true },
                { name: 'DeepSeek', url: 'https://deepseek.example/v1', isHubService: false },
            ],
            'Removed Provider',
            {
                DeepSeek: { total_tokens: 120 },
            },
        );

        expect(selected).toBe('DeepSeek');
    });
});

describe('sidebarTurnUsageProfile', () => {
    const assistant = { provider_id: 'workbuddy', provider_name: 'WorkBuddy 国际版', model: 'deepseek-v4.1-flash' };
    const foreground = { provider_id: 'hub', provider_name: 'MaClaw官方', model: 'auto' };

    it('counts the foreground role when the card names that role', () => {
        expect(sidebarTurnUsageProfile('none', { assistant, foreground })).toBe('foreground');
        expect(sidebarTurnUsageProfile('assistant', { assistant, foreground })).toBe('foreground');
    });

    it('counts foreground rows when the role uses the same provider as the assistant', () => {
        const same = { provider_id: 'workbuddy', provider_name: 'WorkBuddy 国际版', model: 'deepseek-v4.1-flash' };
        expect(sidebarTurnUsageProfile('assistant', { assistant, foreground: same })).toBe('foreground');
        expect(sidebarTurnUsageProfile('none', { assistant, foreground: same })).toBe('foreground');
        expect(sidebarEditorUsageProfile('assistant')).toBe('assistant');
        expect(sidebarEditorUsageProfile('none')).toBe('');
    });

    it('keeps a coding task on the coding bucket', () => {
        expect(sidebarTurnUsageProfile('coding', { assistant, foreground, coding: { provider_id: 'code', provider_name: '智谱编程', model: 'glm' } })).toBe('coding');
    });

    it('keeps the switcher on the assignment it edits', () => {
        expect(sidebarEditorUsageProfile('none')).toBe('');
        expect(sidebarEditorUsageProfile('assistant')).toBe('assistant');
        expect(sidebarEditorUsageProfile('coding')).toBe('coding');
        expect(sidebarTurnUsageProfile('none', { assistant, foreground })).toBe('foreground');
        expect(sidebarEditorUsageProfile('none')).not.toBe(sidebarTurnUsageProfile('none', { assistant, foreground }));
    });
});

describe('sumSidebarProfileUsage', () => {
    it('sums only the contacted role and provider', () => {
        const total = sumSidebarProfileUsage({
            hubAuto: { profile: 'foreground', provider_id: 'hub', total_tokens: 1200, today_tokens: 30 },
            hubOther: { profile: 'foreground', provider_id: 'hub', total_tokens: 80 },
            assistant: { profile: 'assistant', provider_id: 'workbuddy', total_tokens: 52000000 },
            otherHub: { profile: 'foreground', provider_id: 'other', total_tokens: 999 },
        }, 'foreground', 'hub');
        expect(total.total).toBe(1280);
        expect(total.today).toBe(30);
    });

    it('keeps assistant history for the foreground provider and leaves coding and other providers out', () => {
        const total = sumSidebarProviderProfiles({
            foreground: { profile: 'foreground', provider_id: 'hub', total_tokens: 650 },
            assistant: { profile: 'assistant', provider_id: 'hub', total_tokens: 8500 },
            coding: { profile: 'coding', provider_id: 'hub', total_tokens: 10100 },
            other: { profile: 'assistant', provider_id: 'workbuddy', total_tokens: 52000 },
        }, 'hub', ['foreground', 'assistant']);
        expect(total.total).toBe(9150);
    });
});
