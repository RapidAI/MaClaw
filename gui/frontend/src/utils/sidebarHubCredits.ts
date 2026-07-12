import type { SidebarHubCredits, SidebarHubServiceStatus } from '../types/appShell';
import { grantCanContributeExpiry, latestExpiry, numeric, summarizeHubCreditTotals } from './hubCredits';

export function normalizeSidebarHubCredits(status?: SidebarHubServiceStatus | null): SidebarHubCredits | null {
    const active = status?.active ?? status?.Active ?? false;
    const creditGrants = status?.credit_grants ?? status?.CreditGrants ?? [];
    const activeGrants = status?.active_grants ?? status?.ActiveGrants ?? [];
    // summarizeHubCreditTotals also filters hubcenter_compute for wallet math.
    const grants = (creditGrants.length ? creditGrants : activeGrants)
        .filter((grant) => String(grant.source ?? grant.Source ?? '').trim().toLowerCase() !== 'hubcenter_compute');
    const hasGrant = grants.length > 0;
    const grantStatusPriority = ['period_limited', 'queued', 'exhausted', 'expired'];
    const periodLimitedGrant = grants.find((grant) => String(grant.status ?? grant.Status ?? '').toLowerCase() === 'period_limited');
    const activeGrant = grants.find((grant) => {
        const grantStatus = String(grant.status ?? grant.Status ?? '').toLowerCase();
        return grantStatus === 'active' || grant.active === true || grant.Active === true;
    });
    const initialStatusAvailable = numeric(status?.credits_available ?? status?.CreditsAvailable);
    const activeViaFallbackGrant = active && !activeGrant && initialStatusAvailable > 0;
    const statusGrant = active
        ? (activeGrant || (activeViaFallbackGrant ? undefined : periodLimitedGrant) || grants[0])
        : (grantStatusPriority
            .map((statusName) => grants.find((grant) => String(grant.status ?? grant.Status ?? '').toLowerCase() === statusName))
            .find(Boolean) || grants[0]);
    const grantStatus = activeViaFallbackGrant ? 'active' : (String(statusGrant?.status ?? statusGrant?.Status ?? '').toLowerCase() || (active ? 'active' : ''));
    const retryAfterSeconds = activeViaFallbackGrant ? 0 : numeric(statusGrant?.retry_after_seconds ?? statusGrant?.RetryAfterSeconds);
    const retryAfterAt = activeViaFallbackGrant ? '' : String(statusGrant?.retry_after_at ?? statusGrant?.RetryAfterAt ?? '');
    if (!active && !hasGrant) {
        return {
            authorized: false,
            total: 0,
            used: 0,
            remaining: 0,
            available: 0,
            showPeriodAvailable: false,
            tokensPerCredit: 0,
            expiresAt: '',
            unlimited: false,
            status: '',
            retryAfterSeconds: 0,
            retryAfterAt: '',
        };
    }

    const totals = summarizeHubCreditTotals({
        active,
        credits_total: status?.credits_total ?? status?.CreditsTotal,
        credits_used: status?.credits_used ?? status?.CreditsUsed,
        credits_remaining: status?.credits_remaining ?? status?.CreditsRemaining,
        credits_available: status?.credits_available ?? status?.CreditsAvailable,
        grants,
    });

    const latestGrantExpiry = latestExpiry(grants
        .filter(grantCanContributeExpiry)
        .map((grant) => String(grant.expires_at ?? grant.ExpiresAt ?? '')));
    const latestExpiredGrantExpiry = latestExpiry(grants
        .filter((grant) => String(grant.status ?? grant.Status ?? '').toLowerCase() === 'expired')
        .map((grant) => String(grant.expires_at ?? grant.ExpiresAt ?? '')));
    const backendEffectiveExpiry = String(status?.effective_expires_at ?? status?.EffectiveExpiresAt ?? '');
    const backendNearestExpiry = String(status?.nearest_expires_at ?? status?.NearestExpiresAt ?? '');
    const expiresAt = backendEffectiveExpiry || latestGrantExpiry || backendNearestExpiry || latestExpiredGrantExpiry;
    const unlimited = totals.total <= 0;
    const available = Math.max(0, totals.available);
    const remaining = Math.max(0, totals.remaining);
    // Surface period-available when spendable is below lifetime remaining, or
    // the official route is currently period-limited (available may be 0).
    const showPeriodAvailable = !unlimited
        && (grantStatus === 'period_limited' || available + 0.0005 < remaining);
    return {
        authorized: true,
        serviceActive: active,
        total: totals.total,
        used: totals.used,
        remaining,
        available,
        showPeriodAvailable,
        tokensPerCredit: numeric(status?.tokens_per_credit ?? status?.TokensPerCredit),
        expiresAt,
        unlimited,
        status: grantStatus || (active ? 'active' : ''),
        retryAfterSeconds,
        retryAfterAt,
    };
}
