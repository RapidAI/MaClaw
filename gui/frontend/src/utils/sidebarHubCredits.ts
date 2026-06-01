import type { SidebarHubCredits, SidebarHubServiceStatus } from '../types/appShell';
import { grantCanContributeExpiry, latestExpiry, numeric } from './hubCredits';

export function normalizeSidebarHubCredits(status?: SidebarHubServiceStatus | null): SidebarHubCredits | null {
    const active = status?.active ?? status?.Active ?? false;
    const creditGrants = status?.credit_grants ?? status?.CreditGrants ?? [];
    const activeGrants = status?.active_grants ?? status?.ActiveGrants ?? [];
    const grants = creditGrants.length ? creditGrants : activeGrants;
    const hasGrant = grants.length > 0;
    const grantStatusPriority = ['period_limited', 'queued', 'exhausted', 'expired'];
    const periodLimitedGrant = grants.find((grant) => String(grant.status ?? grant.Status ?? '').toLowerCase() === 'period_limited');
    const activeGrant = grants.find((grant) => {
        const status = String(grant.status ?? grant.Status ?? '').toLowerCase();
        return status === 'active' || grant.active === true || grant.Active === true;
    });
    const statusAvailable = numeric(status?.credits_available ?? status?.CreditsAvailable);
    const activeViaFallbackGrant = active && !activeGrant && statusAvailable > 0;
    const statusGrant = active
        ? (activeGrant || (activeViaFallbackGrant ? undefined : periodLimitedGrant) || grants[0])
        : (grantStatusPriority
            .map((status) => grants.find((grant) => String(grant.status ?? grant.Status ?? '').toLowerCase() === status))
            .find(Boolean) || grants[0]);
    const grantStatus = activeViaFallbackGrant ? 'active' : (String(statusGrant?.status ?? statusGrant?.Status ?? '').toLowerCase() || (active ? 'active' : ''));
    const retryAfterSeconds = activeViaFallbackGrant ? 0 : numeric(statusGrant?.retry_after_seconds ?? statusGrant?.RetryAfterSeconds);
    const retryAfterAt = activeViaFallbackGrant ? '' : String(statusGrant?.retry_after_at ?? statusGrant?.RetryAfterAt ?? '');
    if (!active && !hasGrant) {
        return { authorized: false, total: 0, used: 0, remaining: 0, tokensPerCredit: 0, expiresAt: '', unlimited: false, status: '', retryAfterSeconds: 0, retryAfterAt: '' };
    }

    let total = 0;
    let used = 0;
    let remaining = 0;
    for (const grant of grants) {
        // Use backend's "effective" flag as single source of truth.
        // Fall back to status string check for old hub versions without the field.
        const eff = grant.effective ?? grant.Effective;
        const isEffective = typeof eff === 'boolean'
            ? eff
            : String(grant.status ?? grant.Status ?? '').toLowerCase() !== 'queued'
              && String(grant.status ?? grant.Status ?? '').toLowerCase() !== 'expired';
        if (!isEffective) continue;
        total += numeric(grant.credits_total ?? grant.CreditsTotal);
        used += numeric(grant.credits_used ?? grant.CreditsUsed);
        remaining += numeric(grant.credits_remaining ?? grant.CreditsRemaining);
    }
    total = numeric(status?.credits_total ?? status?.CreditsTotal ?? total);
    used = numeric(status?.credits_used ?? status?.CreditsUsed ?? used);
    remaining = numeric(status?.credits_remaining ?? status?.CreditsRemaining ?? remaining);
    const available = numeric(status?.credits_available ?? status?.CreditsAvailable ?? statusGrant?.credits_available ?? statusGrant?.CreditsAvailable);
    if (!active && grantStatus === 'expired') remaining = Math.max(0, available);
    if ((active || remaining <= 0) && available > 0) remaining = available;
    if (remaining > 0 && total < used + remaining) total = used + remaining;
    const unlimited = total <= 0;
    const latestGrantExpiry = latestExpiry(grants
        .filter(grantCanContributeExpiry)
        .map((grant) => String(grant.expires_at ?? grant.ExpiresAt ?? '')));
    const latestExpiredGrantExpiry = latestExpiry(grants
        .filter((grant) => String(grant.status ?? grant.Status ?? '').toLowerCase() === 'expired')
        .map((grant) => String(grant.expires_at ?? grant.ExpiresAt ?? '')));
    const backendEffectiveExpiry = String(status?.effective_expires_at ?? status?.EffectiveExpiresAt ?? '');
    const backendNearestExpiry = String(status?.nearest_expires_at ?? status?.NearestExpiresAt ?? '');
    const expiresAt = backendEffectiveExpiry || latestGrantExpiry || backendNearestExpiry || latestExpiredGrantExpiry;
    return {
        authorized: true,
        serviceActive: active,
        total,
        used,
        remaining,
        tokensPerCredit: numeric(status?.tokens_per_credit ?? status?.TokensPerCredit),
        expiresAt,
        unlimited,
        status: grantStatus || (active ? 'active' : ''),
        retryAfterSeconds,
        retryAfterAt,
    };
}
