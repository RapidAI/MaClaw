import { describe, expect, it } from 'vitest';
import { capabilityModelMenuLabel, capabilityMultiplierFor, defaultCapabilityBillingMultiplier, hubCapabilityModelOptions, hubOfficialModelAliases, multipliersFromModelItems, officialModelAlias, orderCapabilityModels } from '../capabilityModelLabel';

describe('capabilityModelMenuLabel', () => {
    it('shows the default fee coefficient for each official band', () => {
        expect(capabilityModelMenuLabel('auto')).toBe('auto ×1');
        expect(capabilityModelMenuLabel('official-low')).toBe('low ×0.5');
        expect(capabilityModelMenuLabel('official-mid')).toBe('mid ×1');
        expect(capabilityModelMenuLabel('official-high')).toBe('high ×2');
        expect(capabilityModelMenuLabel('low')).toBe('low ×0.5');
    });

    it('prefers the multiplier published by the server', () => {
        expect(capabilityModelMenuLabel('official-high', 3)).toBe('high ×3');
        expect(capabilityModelMenuLabel('gpt-4o')).toBe('gpt-4o');
    });

    it('reads billing_multiplier from fetched model items', () => {
        expect(defaultCapabilityBillingMultiplier('official-low')).toBe(0.5);
        const table = multipliersFromModelItems([
            { id: 'official-low', billing_multiplier: 0.25 },
            { id: 'gpt-4o' },
        ]);
        expect(table['official-low']).toBe(0.25);
        const mixed = multipliersFromModelItems([
            { id: 'official-high', billing_multiplier: 3 },
            { id: 'high', billing_multiplier: 9 },
        ]);
        expect(mixed['official-high']).toBe(3);
        expect(mixed['high']).toBe(9);
        expect(capabilityMultiplierFor('official-high', mixed)).toBe(3);
        expect(capabilityMultiplierFor('low', table)).toBe(0.25);
        expect(capabilityModelMenuLabel('auto', undefined, false)).toBe('auto');
        expect(capabilityModelMenuLabel('official-high', 3, false)).toBe('high ×3');
    });

    it('lists each capability band once, in auto/low/mid/high order', () => {
        expect(orderCapabilityModels(['official-high', 'gpt-4o', 'auto', 'low', 'official-low', 'official-mid']))
            .toEqual(['auto', 'low', 'official-mid', 'official-high', 'gpt-4o']);
        expect(orderCapabilityModels(['official-low', 'auto'])).toEqual(['auto', 'official-low']);
    });

    it('offers low, mid, and high when the official catalog only published auto', () => {
        expect(hubOfficialModelAliases()).toEqual(['auto', 'low', 'mid', 'high']);
        expect(officialModelAlias('official-mid')).toBe('mid');
        expect(officialModelAlias('low')).toBe('low');
        expect(officialModelAlias('gpt-4o')).toBe('gpt-4o');
        expect(hubCapabilityModelOptions(['auto'])).toEqual(['auto', 'official-low', 'official-mid', 'official-high']);
        expect(hubCapabilityModelOptions([])).toEqual(['auto', 'official-low', 'official-mid', 'official-high']);
        expect(hubCapabilityModelOptions(['low', 'official-mid', 'gpt-4o'])).toEqual(['low', 'official-mid', 'gpt-4o']);
    });
});
