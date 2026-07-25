import type { SettingsTabId } from './settingsTabs';
import { SETTINGS_CONTENT_TAB_IDS } from './settingsTabs';

/**
 * Settings content tabs whose body panels read AppConfig fields.
 * Self-loading tabs (memory, knowledge, …) use dedicated APIs and skip this fetch.
 * Keep aligned with gui/config_settings_tab.go settingsTabFieldKeys.
 */
export const SETTINGS_TABS_NEEDING_CONFIG = [
    'general',
    'proxy',
    'ui',
    'display',
    'pet',
    'llm',
    'llmCache',
    'virtualEmployee',
    'im',
    'security',
    'system',
] as const satisfies readonly SettingsTabId[];

/** Tabs that load their own data; GetSettingsTabConfig is a no-op. */
export const SETTINGS_TABS_SELF_LOADING = [
    'searchEngine',
    'redeem',
    'memory',
    'knowledge',
    'misData',
    'embedding',
    'migration',
] as const satisfies readonly SettingsTabId[];

const needsConfigSet: ReadonlySet<string> = new Set(SETTINGS_TABS_NEEDING_CONFIG);
const selfLoadingSet: ReadonlySet<string> = new Set(SETTINGS_TABS_SELF_LOADING);

export function settingsTabNeedsConfig(tab: string | undefined | null): boolean {
    return !!tab && needsConfigSet.has(tab);
}

export function settingsTabIsSelfLoading(tab: string | undefined | null): boolean {
    return !!tab && selfLoadingSet.has(tab);
}

/** Every content tab is either config-backed or self-loading — no orphans. */
export function assertSettingsTabConfigCoverage(): string[] {
    const missing: string[] = [];
    for (const id of SETTINGS_CONTENT_TAB_IDS) {
        if (!needsConfigSet.has(id) && !selfLoadingSet.has(id)) {
            missing.push(id);
        }
    }
    return missing;
}

/**
 * Merge a partial settings-tab DTO into an existing AppConfig-shaped object.
 * Never replaces the base wholesale — preserves projects, providers, etc.
 */
export function mergeSettingsTabConfig<T extends Record<string, any>>(
    base: T | null | undefined,
    partial: Record<string, any> | null | undefined,
): T {
    const safePartial = partial && typeof partial === 'object' ? partial : {};
    return { ...(base || {}), ...safePartial } as T;
}

/**
 * Merge DTO keys without stomping fields the user edited after the fetch started.
 *
 * For each key in `partial`: apply only if `prev[key]` still equals `snapshot[key]`
 * (value at request start). If the user changed it while the request was in flight,
 * keep their edit.
 */
export function mergeSettingsTabConfigSafe<T extends Record<string, any>>(
    prev: T | null | undefined,
    partial: Record<string, any> | null | undefined,
    snapshot: Record<string, any> | null | undefined,
): T {
    const base = { ...(prev || {}) } as T;
    if (!partial || typeof partial !== 'object') return base;
    const snap = snapshot && typeof snapshot === 'object' ? snapshot : {};
    for (const [key, value] of Object.entries(partial)) {
        const current = (base as any)[key];
        const atStart = (snap as any)[key];
        // User edited this key while the request was in flight — keep their value.
        if (!Object.is(current, atStart)) {
            continue;
        }
        (base as any)[key] = value;
    }
    return base;
}

/**
 * Shallow-clone a config-like object for request snapshots.
 * Prefer Object.assign over object spread so class-instance own props copy reliably.
 */
export function snapshotConfigFields(
    config: Record<string, any> | null | undefined,
): Record<string, any> | null {
    if (!config || typeof config !== 'object') return null;
    return Object.assign({}, config);
}

/**
 * Build an AppConfig-shaped object while preserving keys the generated Wails
 * model constructor may omit (fields not yet in models.ts).
 * Only backfills keys present on `merged` so safe-merge user edits stay intact.
 */
export function appConfigFromMergedPlain(
    merged: Record<string, any>,
    construct: (source: Record<string, any>) => Record<string, any>,
): Record<string, any> {
    const next = construct(merged || {});
    for (const [key, value] of Object.entries(merged || {})) {
        const cur = (next as any)[key];
        // Preserve false / 0 / "" / null when the model drops them as undefined.
        if (cur === undefined && value !== undefined) {
            (next as any)[key] = value;
        }
    }
    return next;
}

/**
 * True when a maclaw-config-changed event already carries a real config payload
 * (saver applied full config via setConfig before dispatch). Empty `{}` is
 * treated as signal-only invalidation and should still trigger a tab re-fetch.
 */
export function configChangeEventHasPayload(detail: unknown): boolean {
    if (!detail || typeof detail !== 'object') return false;
    return Object.keys(detail as Record<string, unknown>).length > 0;
}

/**
 * Value equality for config fields. Primitives use Object.is; plain objects/arrays
 * fall back to JSON.stringify so a re-fetched DTO with identical nested content
 * (e.g. llm_prompt_cache) does not force a React re-render.
 */
export function configFieldValuesEqual(a: unknown, b: unknown): boolean {
    if (Object.is(a, b)) return true;
    if (a == null || b == null) return false;
    if (typeof a !== 'object' || typeof b !== 'object') return false;
    try {
        return JSON.stringify(a) === JSON.stringify(b);
    } catch {
        return false;
    }
}

/**
 * Whether `prev` and `merged` agree on every key in `keys`.
 * Used to skip setConfig when a tab DTO merge is a pure no-op.
 */
export function configKeysUnchanged(
    prev: Record<string, any> | null | undefined,
    merged: Record<string, any> | null | undefined,
    keys: string[],
): boolean {
    if (!prev || !merged || keys.length === 0) return false;
    for (const key of keys) {
        if (!configFieldValuesEqual((prev as any)[key], (merged as any)[key])) {
            return false;
        }
    }
    return true;
}
