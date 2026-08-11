import { describe, expect, it } from 'vitest';
import {
    appConfigFromMergedPlain,
    assertSettingsTabConfigCoverage,
    configChangeEventHasPayload,
    configFieldValuesEqual,
    configKeysUnchanged,
    mergeSettingsTabConfig,
    mergeSettingsTabConfigSafe,
    settingsTabIsSelfLoading,
    settingsTabNeedsConfig,
    snapshotConfigFields,
    SETTINGS_TABS_NEEDING_CONFIG,
    SETTINGS_TABS_SELF_LOADING,
} from '../settingsTabConfig';
import { SETTINGS_CONTENT_TAB_IDS } from '../settingsTabs';

describe('settingsTabConfig', () => {
    it('covers every content tab as either config-backed or self-loading', () => {
        expect(assertSettingsTabConfigCoverage()).toEqual([]);
    });

    it('marks config-backed tabs and skips self-loading ones', () => {
        expect(settingsTabNeedsConfig('general')).toBe(true);
        expect(settingsTabNeedsConfig('proxy')).toBe(true);
        expect(settingsTabNeedsConfig('im')).toBe(true);
        expect(settingsTabNeedsConfig('memory')).toBe(false);
        expect(settingsTabNeedsConfig('knowledge')).toBe(false);
        expect(settingsTabNeedsConfig('searchEngine')).toBe(false);
        expect(settingsTabNeedsConfig('')).toBe(false);
        expect(settingsTabNeedsConfig(null)).toBe(false);
        expect(settingsTabIsSelfLoading('memory')).toBe(true);
        expect(settingsTabIsSelfLoading('proxy')).toBe(false);
    });

    it('lists only known content tab ids', () => {
        const content = new Set<string>(SETTINGS_CONTENT_TAB_IDS);
        for (const id of SETTINGS_TABS_NEEDING_CONFIG) {
            expect(content.has(id)).toBe(true);
        }
        for (const id of SETTINGS_TABS_SELF_LOADING) {
            expect(content.has(id)).toBe(true);
        }
    });

    it('merges partial DTO without dropping base fields', () => {
        const merged: Record<string, any> = mergeSettingsTabConfig(
            { language: 'en', projects: [{ id: 'p1' }], default_proxy_host: 'old' },
            { default_proxy_host: 'new', default_proxy_port: '7890' },
        );
        expect(merged.language).toBe('en');
        expect(merged.projects).toEqual([{ id: 'p1' }]);
        expect(merged.default_proxy_host).toBe('new');
        expect(merged.default_proxy_port).toBe('7890');
    });

    it('tolerates null base and empty partial', () => {
        expect(mergeSettingsTabConfig(null, { language: 'zh-Hans' })).toEqual({ language: 'zh-Hans' });
        expect(mergeSettingsTabConfig({ a: 1 }, null)).toEqual({ a: 1 });
        expect(mergeSettingsTabConfig(null, null)).toEqual({});
    });

    it('safe merge keeps keys the user edited after snapshot', () => {
        const snapshot = { default_proxy_host: 'old', default_proxy_port: '80', language: 'en' };
        const prev = { default_proxy_host: 'user-edit', default_proxy_port: '80', language: 'en', projects: [1] };
        const partial = { default_proxy_host: 'server', default_proxy_port: '443', default_proxy_enabled: false };
        const merged: Record<string, any> = mergeSettingsTabConfigSafe(prev, partial, snapshot);
        // User changed host after fetch started — keep edit.
        expect(merged.default_proxy_host).toBe('user-edit');
        // Port unchanged since snapshot — take server value.
        expect(merged.default_proxy_port).toBe('443');
        // New key from server — apply (prev/snapshot both undefined for it).
        expect(merged.default_proxy_enabled).toBe(false);
        expect(merged.language).toBe('en');
        expect(merged.projects).toEqual([1]);
    });

    it('safe merge applies all keys when prev still matches snapshot', () => {
        const snapshot = { default_proxy_host: 'old' };
        const prev = { default_proxy_host: 'old', language: 'en' };
        const merged = mergeSettingsTabConfigSafe(prev, { default_proxy_host: 'fresh' }, snapshot);
        expect(merged.default_proxy_host).toBe('fresh');
        expect(merged.language).toBe('en');
    });

    it('snapshotConfigFields shallow-clones and handles null', () => {
        expect(snapshotConfigFields(null)).toBeNull();
        const src = { a: 1, b: 'x' };
        const snap = snapshotConfigFields(src);
        expect(snap).toEqual(src);
        expect(snap).not.toBe(src);
    });

    it('appConfigFromMergedPlain backfills keys the constructor drops', () => {
        const construct = (src: Record<string, any>) => {
            // Simulate a sparse Wails model that only keeps language.
            return { language: src.language };
        };
        const out = appConfigFromMergedPlain(
            { language: 'en', show_utilities_entry: false, default_proxy_enabled: false },
            construct,
        );
        expect(out.language).toBe('en');
        expect(out.show_utilities_entry).toBe(false);
        expect(out.default_proxy_enabled).toBe(false);
    });

    it('generated AppConfig preserves general entry and survey toggles', async () => {
        const { corelib } = await import('../../../wailsjs/go/models');
        const config = new corelib.AppConfig({
            show_utilities_entry: false,
            survey_enabled: false,
        });

        expect(config.show_utilities_entry).toBe(false);
        expect(config.survey_enabled).toBe(false);
    });

    it('generated AppConfig preserves the OfficeRead rollout policy', async () => {
        const { corelib } = await import('../../../wailsjs/go/models');
        const config = new corelib.AppConfig({
            office_read_engine: 'dual',
            office_read_formats: ['doc', 'xls'],
            office_read_fallback: false,
            office_read_emit_markdown: true,
        });

        expect(config.office_read_engine).toBe('dual');
        expect(config.office_read_formats).toEqual(['doc', 'xls']);
        expect(config.office_read_fallback).toBe(false);
        expect(config.office_read_emit_markdown).toBe(true);
    });

    it('configChangeEventHasPayload treats empty detail as signal-only', () => {
        expect(configChangeEventHasPayload(null)).toBe(false);
        expect(configChangeEventHasPayload(undefined)).toBe(false);
        expect(configChangeEventHasPayload({})).toBe(false);
        expect(configChangeEventHasPayload({ language: 'en' })).toBe(true);
        expect(configChangeEventHasPayload('x')).toBe(false);
    });

    it('configKeysUnchanged detects no-op merges', () => {
        const prev = { a: 1, b: false, c: 'x' };
        expect(configKeysUnchanged(prev, { a: 1, b: false, c: 'x' }, ['a', 'b'])).toBe(true);
        expect(configKeysUnchanged(prev, { a: 2, b: false }, ['a', 'b'])).toBe(false);
        expect(configKeysUnchanged(null, prev, ['a'])).toBe(false);
        expect(configKeysUnchanged(prev, prev, [])).toBe(false);
    });

    it('configFieldValuesEqual treats identical nested objects as equal', () => {
        expect(configFieldValuesEqual(1, 1)).toBe(true);
        expect(configFieldValuesEqual(false, false)).toBe(true);
        expect(configFieldValuesEqual({ enabled: true, n: 1 }, { enabled: true, n: 1 })).toBe(true);
        expect(configFieldValuesEqual({ enabled: true }, { enabled: false })).toBe(false);
        expect(configFieldValuesEqual([1, 2], [1, 2])).toBe(true);
        expect(configFieldValuesEqual(null, undefined)).toBe(false);
    });

    it('configKeysUnchanged ignores new object identity when nested content matches', () => {
        const prev = { llm_prompt_cache: { enabled: true, cache_dir: '/tmp' } };
        const merged = { llm_prompt_cache: { enabled: true, cache_dir: '/tmp' } };
        expect(prev.llm_prompt_cache).not.toBe(merged.llm_prompt_cache);
        expect(configKeysUnchanged(prev, merged, ['llm_prompt_cache'])).toBe(true);
    });
});
