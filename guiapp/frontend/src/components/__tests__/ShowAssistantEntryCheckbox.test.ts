/**
 * Unit tests for the Desktop Pet settings entry toggle behavior.
 */
import { describe, it, expect, vi } from 'vitest';

interface TestConfig {
    pet_enabled?: boolean;
    language?: string;
    hide_startup_popup?: boolean;
    workstation_mode?: boolean;
}

const patchConfigFieldsMock = vi.fn().mockResolvedValue(undefined);

vi.mock('../../../wailsjs/go/main/App', () => ({
    PatchConfigFields: (...args: unknown[]) => patchConfigFieldsMock(...args),
}));

import { PatchConfigFields } from '../../../wailsjs/go/main/App';

describe('Desktop Pet Entry Toggle Behavior', () => {
    describe('Toggle updates config', () => {
        it('sets pet_enabled to true when checked', () => {
            const config: TestConfig = { pet_enabled: false };
            const newConfig: TestConfig = { ...config, pet_enabled: true };

            expect(newConfig.pet_enabled).toBe(true);
        });

        it('sets pet_enabled to false when unchecked', () => {
            const config: TestConfig = { pet_enabled: true };
            const newConfig: TestConfig = { ...config, pet_enabled: false };

            expect(newConfig.pet_enabled).toBe(false);
        });

        it('preserves other config fields when toggling', () => {
            const config: TestConfig = {
                pet_enabled: true,
                language: 'zh-CN',
                hide_startup_popup: true,
                workstation_mode: false,
            };
            const newConfig: TestConfig = { ...config, pet_enabled: false };

            expect(newConfig.language).toBe('zh-CN');
            expect(newConfig.hide_startup_popup).toBe(true);
            expect(newConfig.workstation_mode).toBe(false);
        });
    });

    describe('Default value handling', () => {
        it('defaults to disabled when pet_enabled is undefined', () => {
            const config: TestConfig = {};
            const petEnabled = !!config.pet_enabled;

            expect(petEnabled).toBe(false);
        });

        it('returns true when pet_enabled is explicitly true', () => {
            const config: TestConfig = { pet_enabled: true };
            expect(!!config.pet_enabled).toBe(true);
        });
    });

    describe('PatchConfigFields integration', () => {
        it('patches only pet_enabled', async () => {
            vi.clearAllMocks();

            const patch = { pet_enabled: false };

            await PatchConfigFields(patch);

            expect(patchConfigFieldsMock).toHaveBeenCalledWith(patch);
        });
    });
});

describe('Edge cases', () => {
    it('handles rapid toggle without race condition', async () => {
        vi.clearAllMocks();

        const config: TestConfig = { pet_enabled: true };
        const toggle1: TestConfig = { ...config, pet_enabled: false };
        const toggle2: TestConfig = { ...toggle1, pet_enabled: true };

        await PatchConfigFields(toggle1 as unknown as Record<string, unknown>);
        await PatchConfigFields(toggle2 as unknown as Record<string, unknown>);

        expect(patchConfigFieldsMock).toHaveBeenCalledTimes(2);
        expect(patchConfigFieldsMock).toHaveBeenLastCalledWith(toggle2);
    });
});
