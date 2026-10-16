/** User-facing labels for Hub official capability bands, with the fee coefficient. */

export function defaultCapabilityBillingMultiplier(modelId: string): number | undefined {
    const key = String(modelId || '').trim().toLowerCase();
    if (key === 'auto' || key === 'default') return 1;
    if (key === 'low' || key === 'official-low') return 0.5;
    if (key === 'mid' || key === 'official-mid') return 1;
    if (key === 'high' || key === 'official-high') return 2;
    return undefined;
}

export function formatCapabilityMultiplier(value: number): string {
    if (!Number.isFinite(value) || value <= 0) return '';
    const rounded = Math.round(value * 1000) / 1000;
    return `×${rounded}`;
}

export function capabilityBandName(modelId: string): string {
    const key = String(modelId || '').trim().toLowerCase();
    if (key === 'auto' || key === 'default') return 'auto';
    if (key === 'low' || key === 'official-low') return 'low';
    if (key === 'mid' || key === 'official-mid') return 'mid';
    if (key === 'high' || key === 'official-high') return 'high';
    return '';
}

function canonicalCapabilityModelId(modelId: string): string {
    const name = capabilityBandName(modelId);
    if (name === 'low') return 'official-low';
    if (name === 'mid') return 'official-mid';
    if (name === 'high') return 'official-high';
    if (name === 'auto') return 'auto';
    return '';
}

/** Published fee for this id, including low/official-low aliases. */
export function capabilityMultiplierFor(modelId: string, multipliers?: Record<string, number> | null): number | undefined {
    if (!multipliers) return undefined;
    const id = String(modelId || '').trim();
    const keys = [id, id.toLowerCase(), canonicalCapabilityModelId(id)];
    for (const key of keys) {
        if (!key) continue;
        const raw = multipliers[key];
        if (raw > 0 && Number.isFinite(raw)) return raw;
    }
    return undefined;
}

/**
 * Menu label: "low ×0.5". Other model ids stay unchanged.
 * Defaults are only invented for Hub capability catalogs. A published multiplier
 * is shown wherever it arrives.
 */
export function capabilityModelMenuLabel(modelId: string, multiplier?: number, showDefault = true): string {
    const id = String(modelId || '').trim();
    const name = capabilityBandName(id);
    if (!name) return id;
    const published = multiplier && multiplier > 0 && Number.isFinite(multiplier) ? multiplier : undefined;
    if (!published && !showDefault) return id;
    const factor = published ?? defaultCapabilityBillingMultiplier(id);
    const badge = factor ? formatCapabilityMultiplier(factor) : '';
    return badge ? `${name} ${badge}` : name;
}

const CAPABILITY_BAND_RANK: Record<string, number> = { auto: 0, low: 1, mid: 2, high: 3 };

const DEFAULT_HUB_CAPABILITY_IDS = ['auto', 'official-low', 'official-mid', 'official-high'];

/** Model-name aliases for the MaClaw official provider. These are the names
 * the assignment list and the quick switcher offer. They do not come from the
 * live upstream catalog, which changes between requests. */
export const HUB_OFFICIAL_MODEL_ALIASES = ['auto', 'low', 'mid', 'high'] as const;

export function hubOfficialModelAliases(): string[] {
    return [...HUB_OFFICIAL_MODEL_ALIASES];
}

/** Short name shown for a saved official band. official-mid and mid are both "mid". */
export function officialModelAlias(modelId: string): string {
    return capabilityBandName(modelId) || String(modelId || '').trim();
}

/**
 * Models the official card can offer. A status payload that only lists auto
 * still gets low/mid/high, because those bands are synthesized for the
 * official service. A catalog that already published a band keeps that id.
 */
export function hubCapabilityModelOptions(published: string[]): string[] {
    const ids = (published || []).map((id) => String(id || '').trim()).filter(Boolean);
    const bands = ids.filter((id) => capabilityBandName(id));
    const rest = ids.filter((id) => !capabilityBandName(id));
    const onlyAuto = bands.length === 0 || bands.every((id) => capabilityBandName(id) === 'auto');
    const source = onlyAuto ? [...bands, ...DEFAULT_HUB_CAPABILITY_IDS] : bands;
    return orderCapabilityModels([...source, ...rest]);
}

/** Put auto/low/mid/high first, one row per band. Keeps the first published id. */
export function orderCapabilityModels(ids: string[]): string[] {
    const bands: string[] = [];
    const rest: string[] = [];
    const seen = new Set<string>();
    for (const raw of ids) {
        const id = String(raw || '').trim();
        if (!id) continue;
        const band = capabilityBandName(id);
        if (!band) {
            rest.push(id);
            continue;
        }
        if (seen.has(band)) continue;
        seen.add(band);
        bands.push(id);
    }
    bands.sort((a, b) => (CAPABILITY_BAND_RANK[capabilityBandName(a)] ?? 9) - (CAPABILITY_BAND_RANK[capabilityBandName(b)] ?? 9));
    return bands.concat(rest);
}

export function multipliersFromModelItems(items: Array<Record<string, unknown>> | null | undefined): Record<string, number> {
    const out: Record<string, number> = {};
    for (const item of items || []) {
        const id = String(item?.id ?? item?.ID ?? item?.name ?? item?.Name ?? '').trim();
        const raw = Number(item?.billing_multiplier ?? item?.BillingMultiplier);
        if (!id || !(raw > 0) || !Number.isFinite(raw)) continue;
        out[id] = raw;
        const canon = canonicalCapabilityModelId(id);
        if (canon && canon !== id && out[canon] == null) out[canon] = raw;
    }
    return out;
}
