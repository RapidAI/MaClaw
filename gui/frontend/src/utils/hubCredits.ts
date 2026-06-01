function appendQuery(path: string, params: Record<string, string | undefined>) {
    const query = Object.entries(params)
        .map(([key, value]) => [key, String(value || '').trim()] as const)
        .filter(([, value]) => value)
        .map(([key, value]) => `${encodeURIComponent(key)}=${encodeURIComponent(value)}`)
        .join('&');
    return query ? `${path}?${query}` : path;
}

export interface HubCreditGrantLike {
    status?: unknown;
    Status?: unknown;
    credits_total?: unknown;
    CreditsTotal?: unknown;
    credits_remaining?: unknown;
    CreditsRemaining?: unknown;
    credits_available?: unknown;
    CreditsAvailable?: unknown;
}

export function numeric(value: unknown): number {
    const parsed = Number(value ?? 0);
    return Number.isFinite(parsed) ? parsed : 0;
}

export function latestExpiry(values: string[]): string {
    return values
        .filter(Boolean)
        .sort((a, b) => {
            const aTime = Date.parse(a);
            const bTime = Date.parse(b);
            if (Number.isFinite(aTime) !== Number.isFinite(bTime)) return Number.isFinite(aTime) ? 1 : -1;
            if (Number.isFinite(aTime) && Number.isFinite(bTime)) return aTime - bTime;
            return a.localeCompare(b);
        })
        .pop() || '';
}

export function grantCanContributeExpiry(grant: HubCreditGrantLike): boolean {
    const status = String(grant.status ?? grant.Status ?? '').toLowerCase();
    if (status === 'expired' || status === 'exhausted') return false;
    const total = numeric(grant.credits_total ?? grant.CreditsTotal);
    if (total <= 0) return true;
    const hasRemaining = grant.credits_remaining !== undefined || grant.CreditsRemaining !== undefined;
    const hasAvailable = grant.credits_available !== undefined || grant.CreditsAvailable !== undefined;
    const remaining = numeric(grant.credits_remaining ?? grant.CreditsRemaining);
    const available = numeric(grant.credits_available ?? grant.CreditsAvailable);
    return remaining > 0 || available > 0 || (!hasRemaining && !hasAvailable);
}

export function buildHubCreditsURL(hubURL?: string, viewerToken?: string, tenantID?: string, email?: string) {
    const base = String(hubURL || '').trim().replace(/\/+$/, '');
    const token = String(viewerToken || '').trim();
    if (!base) return '';
    const creditsURL = appendQuery(`${base}/get-credits`, { tenant_id: tenantID, email });
    return token ? `${creditsURL}#token=${encodeURIComponent(token)}` : creditsURL;
}

export function buildHubCardStoreURL(hubURL?: string, tenantID?: string, email?: string, viewerToken?: string) {
    const base = String(hubURL || '').trim().replace(/\/+$/, '');
    const token = String(viewerToken || '').trim();
    if (!base) return '';
    const storeURL = appendQuery(`${base}/card_store`, { tenant_id: tenantID, email });
    return token ? `${storeURL}#token=${encodeURIComponent(token)}` : storeURL;
}
