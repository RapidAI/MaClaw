import { describe, expect, it } from 'vitest';
import { selectSidebarCurrentProvider } from './sidebarProviderSelection';

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
