import type { SidebarHubCredits, SidebarHubServiceStatus } from '../types/appShell';

export function normalizeSidebarHubCredits(status?: SidebarHubServiceStatus | null): SidebarHubCredits | null {
    const active = status?.active ?? status?.Active ?? false;
    const creditGrants = status?.credit_grants ?? status?.CreditGrants ?? [];
    const activeGrants = status?.active_grants ?? status?.ActiveGrants ?? [];
    const grants = creditGrants.length ? creditGrants : activeGrants;
    const hasGrant = grants.length > 0;
    const grantStatusPriority = ['period_limited', 'queued', 'exhausted', 'expired'];
    const activeGrant = grants.find((grant) => {
        const status = String(grant.status ?? grant.Status ?? '').toLowerCase();
        return status === 'active' || grant.active === true || grant.Active === true;
    });
    const statusGrant = active
        ? (activeGrant || grants[0])
        : (grantStatusPriority
            .map((status) => grants.find((grant) => String(grant.status ?? grant.Status ?? '').toLowerCase() === status))
            .find(Boolean) || grants[0]);
    const grantStatus = active ? 'active' : String(statusGrant?.status ?? statusGrant?.Status ?? '').toLowerCase();
    const retryAfterSeconds = Number(statusGrant?.retry_after_seconds ?? statusGrant?.RetryAfterSeconds ?? 0);
    const retryAfterAt = String(statusGrant?.retry_after_at ?? statusGrant?.RetryAfterAt ?? '');
    if (!active && !hasGrant) {
        return { authorized: false, total: 0, used: 0, remaining: 0, tokensPerCredit: 0, expiresAt: '', unlimited: false, status: '', retryAfterSeconds: 0, retryAfterAt: '' };
    }

    let total = 0;
    let used = 0;
    let remaining = 0;
    for (const grant of grants) {
        total += Number(grant.credits_total ?? grant.CreditsTotal ?? 0);
        used += Number(grant.credits_used ?? grant.CreditsUsed ?? 0);
        remaining += Number(grant.credits_remaining ?? grant.CreditsRemaining ?? 0);
    }
    total = Number(status?.credits_total ?? status?.CreditsTotal ?? total);
    used = Number(status?.credits_used ?? status?.CreditsUsed ?? used);
    remaining = Number(status?.credits_remaining ?? status?.CreditsRemaining ?? remaining);
    const available = Number(status?.credits_available ?? status?.CreditsAvailable ?? statusGrant?.credits_available ?? statusGrant?.CreditsAvailable ?? 0);
    if (!active && grantStatus === 'expired') remaining = Math.max(0, available);
    if (remaining <= 0 && available > 0) remaining = available;
    if (total <= 0 && remaining > 0) total = used + remaining;
    const unlimited = total <= 0;
    const expirySourceGrants = active
        ? grants.filter((grant) => {
            const status = String(grant.status ?? grant.Status ?? '').toLowerCase();
            return status === 'active' || grant.active === true || grant.Active === true;
        })
        : grants;
    const nearestGrantExpiry = (expirySourceGrants.length ? expirySourceGrants : grants)
        .map((grant) => String(grant.expires_at ?? grant.ExpiresAt ?? ''))
        .filter(Boolean)
        .sort()[0] || '';
    return {
        authorized: true,
        total,
        used,
        remaining,
        tokensPerCredit: Number(status?.tokens_per_credit ?? status?.TokensPerCredit ?? 0),
        expiresAt: String(status?.effective_expires_at ?? status?.EffectiveExpiresAt ?? status?.nearest_expires_at ?? status?.NearestExpiresAt ?? nearestGrantExpiry),
        unlimited,
        status: grantStatus || (active ? 'active' : ''),
        retryAfterSeconds,
        retryAfterAt,
    };
}
