export type AudienceChoice = {
    id: string;
    name: string;
    hubId: string;
    hubName: string;
};

export function audienceSelectionKey(id: string): string {
    return id.trim().toLowerCase();
}

export function readAudienceList(value: unknown): AudienceChoice[] {
    if (!Array.isArray(value)) return [];
    const out: AudienceChoice[] = [];
    const seen = new Set<string>();
    for (const item of value) {
        if (!item || typeof item !== 'object') continue;
        const row = item as Record<string, unknown>;
        const id = typeof row.id === 'string' ? row.id.trim() : '';
        if (!id) continue;
        const key = id.toLowerCase();
        if (seen.has(key)) continue;
        seen.add(key);
        out.push({
            id,
            name: typeof row.name === 'string' ? row.name.trim() : '',
            hubId: typeof row.hub_id === 'string' ? row.hub_id.trim() : '',
            hubName: typeof row.hub_name === 'string' ? row.hub_name.trim() : '',
        });
    }
    return out;
}

export type StoredAudience = { hub_id: string; tenant_id: string };

/** A stored row that names both ids. A call must match the hub and the tenant. */
export type AudiencePair = { hubId: string; tenantId: string };

export function pairSelectionKey(pair: AudiencePair): string {
    return `${audienceSelectionKey(pair.hubId)}\0${audienceSelectionKey(pair.tenantId)}`;
}

/**
 * Split a stored allow-list without turning a paired row into two wider grants.
 * A row with both ids stays a pair. A hub-only or tenant-only row stays that.
 */
export function splitStoredAudiences(rows: StoredAudience[] | undefined): {
    hubs: string[];
    tenants: string[];
    pairs: AudiencePair[];
} {
    const hubs: string[] = [];
    const tenants: string[] = [];
    const pairs: AudiencePair[] = [];
    const seenHub = new Set<string>();
    const seenTenant = new Set<string>();
    const seenPair = new Set<string>();
    for (const row of rows || []) {
        const hub = (row?.hub_id || '').trim();
        const tenant = (row?.tenant_id || '').trim();
        if (hub && tenant) {
            const pair = { hubId: hub, tenantId: tenant };
            const key = pairSelectionKey(pair);
            if (seenPair.has(key)) continue;
            seenPair.add(key);
            pairs.push(pair);
            continue;
        }
        if (hub) {
            const key = audienceSelectionKey(hub);
            if (seenHub.has(key)) continue;
            seenHub.add(key);
            hubs.push(hub);
            continue;
        }
        if (tenant) {
            const key = audienceSelectionKey(tenant);
            if (seenTenant.has(key)) continue;
            seenTenant.add(key);
            tenants.push(tenant);
        }
    }
    return { hubs, tenants, pairs };
}

/**
 * Keep a granted id visible when the account list no longer returns it.
 * Dropping it would change who can call the share without the owner seeing that id.
 */
export function withGrantedAudiences(choices: AudienceChoice[], granted: string[]): AudienceChoice[] {
    const seen = new Set(choices.map((item) => audienceSelectionKey(item.id)));
    const extra: AudienceChoice[] = [];
    for (const id of granted) {
        const trimmed = id.trim();
        const key = audienceSelectionKey(trimmed);
        if (!key || seen.has(key)) continue;
        seen.add(key);
        extra.push({ id: trimmed, name: '', hubId: '', hubName: '' });
    }
    return extra.length === 0 ? choices : [...choices, ...extra];
}

export function selectedAudienceMap(ids: string[]): Record<string, boolean> {
    const out: Record<string, boolean> = {};
    for (const id of ids) {
        const key = audienceSelectionKey(id);
        if (key) out[key] = true;
    }
    return out;
}

export function chosenAudienceIds(choices: AudienceChoice[], selected: Record<string, boolean>): string[] {
    return choices.filter((item) => selected[audienceSelectionKey(item.id)]).map((item) => item.id);
}

export type AudienceGrantRow = { hub_id: string; tenant_id: string };

/**
 * A checked hub allows every tenant on that hub, and a checked tenant allows
 * every hub. Either one already includes a pair that names it.
 */
export function audienceCoversPair(pair: AudiencePair, hubIds: Iterable<string>, tenantIds: Iterable<string>): boolean {
    const hubKey = audienceSelectionKey(pair.hubId);
    const tenantKey = audienceSelectionKey(pair.tenantId);
    for (const id of hubIds) {
        if (hubKey && audienceSelectionKey(id) === hubKey) return true;
    }
    for (const id of tenantIds) {
        if (tenantKey && audienceSelectionKey(id) === tenantKey) return true;
    }
    return false;
}

/** Pairs already allowed by a wider hub or tenant grant start unchecked. */
export function selectedPairMap(pairs: AudiencePair[], hubIds: string[], tenantIds: string[]): Record<string, boolean> {
    const initial: Record<string, boolean> = {};
    for (const pair of pairs) {
        if (audienceCoversPair(pair, hubIds, tenantIds)) continue;
        initial[pairSelectionKey(pair)] = true;
    }
    return initial;
}

/**
 * The rows a save will send. A pair covered by a checked hub or tenant is
 * omitted, because that wider row is what the call check will use.
 */
export function audienceGrantRows(
    hubChoices: AudienceChoice[],
    tenantChoices: AudienceChoice[],
    pairs: AudiencePair[],
    selectedHubs: Record<string, boolean>,
    selectedTenants: Record<string, boolean>,
    selectedPairs: Record<string, boolean>,
): AudienceGrantRow[] {
    const hubIds = chosenAudienceIds(hubChoices, selectedHubs);
    const tenantIds = chosenAudienceIds(tenantChoices, selectedTenants);
    const keptPairs = pairs.filter((pair) => {
        if (selectedPairs[pairSelectionKey(pair)] !== true) return false;
        return !audienceCoversPair(pair, hubIds, tenantIds);
    });
    return [
        ...hubIds.map((id) => ({ hub_id: id, tenant_id: '' })),
        ...tenantIds.map((id) => ({ hub_id: '', tenant_id: id })),
        ...keptPairs.map((pair) => ({ hub_id: pair.hubId, tenant_id: pair.tenantId })),
    ];
}

export function AudiencePickList({
    label,
    emptyText,
    choices,
    selected,
    onToggle,
    disabled,
    showHub = false,
}: {
    label: string;
    emptyText: string;
    choices: AudienceChoice[];
    selected: Record<string, boolean>;
    onToggle: (id: string) => void;
    disabled: boolean;
    showHub?: boolean;
}) {
    return (
        <section className="tbk-audience__column" aria-label={label}>
            <span className="tbk-audience__title">{label}</span>
            {choices.length === 0 ? <p className="tbk-audience__empty">{emptyText}</p> : (
                <ul className="tbk-audience__list">
                    {choices.map((choice) => {
                        const primary = choice.name || choice.id;
                        const hubContext = showHub ? choice.hubName || choice.hubId : '';
                        const selectionKey = audienceSelectionKey(choice.id);
                        return (
                            <li key={selectionKey}>
                                <label className="tbk-audience__option">
                                    <input
                                        type="checkbox"
                                        aria-label={primary}
                                        checked={selected[selectionKey] === true}
                                        disabled={disabled}
                                        onChange={() => onToggle(choice.id)}
                                    />
                                    <span className="tbk-audience__copy">
                                        <span className="tbk-audience__name">{primary}</span>
                                        {choice.name && choice.name !== choice.id ? (
                                            <span className="tbk-audience__id">{choice.id}</span>
                                        ) : null}
                                        {hubContext ? <span className="tbk-audience__meta">{hubContext}</span> : null}
                                    </span>
                                </label>
                            </li>
                        );
                    })}
                </ul>
            )}
        </section>
    );
}
