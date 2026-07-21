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
    effective?: unknown;
    Effective?: unknown;
    source?: unknown;
    Source?: unknown;
    credits_total?: unknown;
    CreditsTotal?: unknown;
    credits_used?: unknown;
    CreditsUsed?: unknown;
    credits_remaining?: unknown;
    CreditsRemaining?: unknown;
    credits_available?: unknown;
    CreditsAvailable?: unknown;
}

export type HubCreditTotalsInput = {
    active?: unknown;
    Active?: unknown;
    credits_total?: unknown;
    CreditsTotal?: unknown;
    credits_used?: unknown;
    CreditsUsed?: unknown;
    credits_remaining?: unknown;
    CreditsRemaining?: unknown;
    credits_available?: unknown;
    CreditsAvailable?: unknown;
    grants?: HubCreditGrantLike[] | null;
};

export type HubCreditTotals = {
    total: number;
    used: number;
    remaining: number;
    available: number;
};

export function numeric(value: unknown): number {
    const parsed = Number(value ?? 0);
    return Number.isFinite(parsed) ? parsed : 0;
}

function grantStatusOf(grant: HubCreditGrantLike): string {
    return String(grant.status ?? grant.Status ?? '').trim().toLowerCase();
}

function grantMeteredTotal(grant: HubCreditGrantLike): number {
    const total = numeric(grant.credits_total ?? grant.CreditsTotal);
    if (total > 0) return total;
    return Math.max(
        numeric(grant.credits_available ?? grant.CreditsAvailable),
        numeric(grant.credits_remaining ?? grant.CreditsRemaining),
    );
}

function grantOwnedRemaining(grant: HubCreditGrantLike): number {
    const left = numeric(grant.credits_remaining ?? grant.CreditsRemaining);
    if (left > 0) return left;
    return Math.max(
        numeric(grant.credits_total ?? grant.CreditsTotal),
        numeric(grant.credits_available ?? grant.CreditsAvailable),
    );
}

/**
 * Shared Total / Used / Remaining math for sidebar + service redeem UI.
 *
 * Semantics (aligned with Hub ResolveStatus accounting):
 * - Exclude only expired grants from wallet totals
 * - Queued grants raise Total and Remaining (owned but not started)
 * - Exhausted grants raise Total and Used (spent history), Remaining 0
 * - Never shrink Remaining to a period window; only fill from Available
 *   when Remaining is empty
 * - Prefer Total ≈ Used + Remaining after point-card purchases
 */
export function summarizeHubCreditTotals(input?: HubCreditTotalsInput | null): HubCreditTotals {
    const grants = (input?.grants || []).filter((grant) => {
        const source = String(grant.source ?? grant.Source ?? '').trim().toLowerCase();
        return source !== 'hubcenter_compute';
    });
    const active = Boolean(input?.active ?? input?.Active);

    let grantTotal = 0;
    let used = 0;
    let grantRemaining = 0;
    let grantAvailable = 0;
    let visibleGrantTotal = 0;
    let queuedRemaining = 0;

    for (const grant of grants) {
        const status = grantStatusOf(grant);
        if (status === 'expired') continue;

        visibleGrantTotal += grantMeteredTotal(grant);

        if (status === 'queued') {
            // Owned but not started: count toward remaining, not used.
            queuedRemaining += grantOwnedRemaining(grant);
            continue;
        }

        // active | period_limited | exhausted | unknown — same as Hub status
        // totals (effective flag is routing-only, not wallet accounting).
        grantTotal += numeric(grant.credits_total ?? grant.CreditsTotal);
        used += numeric(grant.credits_used ?? grant.CreditsUsed);
        grantRemaining += numeric(grant.credits_remaining ?? grant.CreditsRemaining);
        grantAvailable += numeric(grant.credits_available ?? grant.CreditsAvailable);
    }

    const effectiveVisibleTotal = Math.max(grantTotal, visibleGrantTotal);
    let total = Math.max(
        numeric(input?.credits_total ?? input?.CreditsTotal ?? effectiveVisibleTotal),
        effectiveVisibleTotal,
    );
    // Prefer backend used when present (includes all spent grants); fall back to sum.
    const backendUsed = input?.credits_used ?? input?.CreditsUsed;
    used = backendUsed !== undefined && backendUsed !== null && String(backendUsed).trim() !== ''
        ? numeric(backendUsed)
        : used;

    const statusRemaining = numeric(input?.credits_remaining ?? input?.CreditsRemaining);
    let remaining = 0;
    if (grantRemaining > 0 || queuedRemaining > 0) {
        remaining = grantRemaining + queuedRemaining;
    } else if (statusRemaining > 0) {
        remaining = statusRemaining;
    }

    const statusAvailable = numeric(input?.credits_available ?? input?.CreditsAvailable);
    const available = statusAvailable > 0 ? statusAvailable : grantAvailable;
    const onlyExpired = !active
        && grants.length > 0
        && grants.every((grant) => grantStatusOf(grant) === 'expired');
    if (onlyExpired) {
        return { total, used, remaining: Math.max(0, available), available };
    }
    // Fill from available only when remaining is empty — never shrink lifetime
    // remaining to the current period window.
    if (remaining <= 0 && available > 0) remaining = available;
    if (remaining > 0 && total < used + remaining) total = used + remaining;
    return { total, used, remaining, available };
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

function accountFromIdentity(email?: string, userID?: string, mobile?: string) {
    // Prefer human account identities for card-store / credits pages. User IDs
    // are still passed separately as user_id; using them as "account" breaks
    // buyer identity validation (email or phone: only).
    const mail = String(email || '').trim();
    if (mail) return mail;
    const phone = String(mobile || '').trim();
    if (phone) {
        const digits = phone.replace(/\D/g, '');
        if (digits) return `phone:${digits}`;
        if (phone.toLowerCase().startsWith('phone:')) return phone;
    }
    return String(userID || '').trim();
}

export function buildHubCreditsURL(hubURL?: string, viewerToken?: string, tenantID?: string, email?: string, userID?: string, mobile?: string) {
    const base = String(hubURL || '').trim().replace(/\/+$/, '');
    const token = String(viewerToken || '').trim();
    if (!base) return '';
    const creditsURL = appendQuery(`${base}/get-credits`, {
        tenant_id: tenantID,
        account: accountFromIdentity(email, userID, mobile),
        user_id: userID,
        email,
        mobile,
    });
    return token ? `${creditsURL}#token=${encodeURIComponent(token)}` : creditsURL;
}

/** Normalize UI lang to hub guide query: en | zh | '' (omit). */
export function normalizeHubGuideLang(lang?: string): string {
    const raw = String(lang || '').trim();
    if (!raw) return '';
    return raw.toLowerCase().startsWith('en') ? 'en' : 'zh';
}

function buildHubGuideURL(hubURL: string | undefined, path: string, lang?: string): string {
    const base = String(hubURL || '').trim().replace(/\/+$/, '');
    if (!base) return '';
    return appendQuery(`${base}${path.startsWith('/') ? path : `/${path}`}`, {
        lang: normalizeHubGuideLang(lang),
    });
}

export function buildHubMaclawAppManualURL(hubURL?: string, lang?: string) {
    return buildHubGuideURL(hubURL, '/maclaw-app-manual', lang);
}

/** Hub page: Pet Pack creation guide (bilingual ZH/EN). */
export function buildHubPetPackHelpURL(hubURL?: string, lang?: string) {
    return buildHubGuideURL(hubURL, '/pet-pack-help', lang);
}

/**
 * Build the URL for the card/credits purchase page.
 *
 * Priority: Hub's own `/card_store` page (tenant-scoped purchase).
 * Fallback: HubCenter `/compute-store` (only when Hub URL is unavailable).
 */
export function buildHubCardStoreURL(hubURL?: string, tenantID?: string, email?: string, viewerToken?: string, hubCenterURL?: string, hubID?: string, hubName?: string, tenantName?: string, userID?: string, mobile?: string) {
    const base = String(hubURL || '').trim().replace(/\/+$/, '');
    const hubCenterBase = String(hubCenterURL || '').trim().replace(/\/+$/, '');
    const resolvedHubID = String(hubID || '').trim();
    const token = String(viewerToken || '').trim();
    const identityParams = {
        account: accountFromIdentity(email, userID, mobile),
        user_id: userID,
        email,
        mobile,
    };
    // Always prefer the Hub's own card_store — the user buys credits scoped to
    // the Hub they are logged into, not from the HubCenter compute marketplace.
    if (base) {
        const storeURL = appendQuery(`${base}/card_store`, { tenant_id: tenantID, ...identityParams });
        return token ? `${storeURL}#token=${encodeURIComponent(token)}` : storeURL;
    }
    // Fallback: if Hub URL is unavailable but HubCenter context exists, link to
    // the HubCenter compute-store (legacy path, should rarely trigger).
    if (hubCenterBase && resolvedHubID) {
        return appendQuery(`${hubCenterBase}/compute-store`, { hub_id: resolvedHubID, tenant_id: tenantID, hub_name: hubName, tenant_name: tenantName, ...identityParams });
    }
    return '';
}
