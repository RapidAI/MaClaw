import type { SidebarHubCredits, SidebarHubServiceStatus } from '../types/appShell';
import { grantCanContributeExpiry, latestExpiry, numeric, summarizeHubCreditTotals } from './hubCredits';

type NewUserLimitCardSummary = NonNullable<SidebarHubCredits['newUserLimitCards']>[number];

function grantIsPermanent(grant: { permanent?: unknown; Permanent?: unknown }): boolean {
    return Boolean(grant.permanent ?? grant.Permanent);
}

export function normalizeSidebarHubCredits(status?: SidebarHubServiceStatus | null): SidebarHubCredits | null {
    const active = status?.active ?? status?.Active ?? false;
    const creditGrants = status?.credit_grants ?? status?.CreditGrants ?? [];
    const activeGrants = status?.active_grants ?? status?.ActiveGrants ?? [];
    // summarizeHubCreditTotals also filters hubcenter_compute for wallet math.
    const grants = (creditGrants.length ? creditGrants : activeGrants)
        .filter((grant) => String(grant.source ?? grant.Source ?? '').trim().toLowerCase() !== 'hubcenter_compute');
    const newUserLimitCardGrants = grants.filter((grant) => String(grant.source ?? grant.Source ?? '').trim().toLowerCase() === 'new_user_limit_card'
        && (String(grant.status ?? grant.Status ?? '').trim().toLowerCase() === 'active'
            || String(grant.status ?? grant.Status ?? '').trim().toLowerCase() === 'period_limited'));
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
    const newUserLimitCards = (() => {
        const latestWindowEnd = (current: string, next: unknown) => {
            const value = String(next ?? '');
            return value && (!current || value > current) ? value : current;
        };
        const cards = new Map<string, NewUserLimitCardSummary>();
        for (const grant of newUserLimitCardGrants) {
            const limits = grant.period_limits ?? grant.PeriodLimits;
            if (numeric(limits?.five_hour ?? limits?.FiveHour) <= 0 && numeric(limits?.daily ?? limits?.Daily) <= 0) continue;
            const usage = grant.period_usage ?? grant.PeriodUsage;
            const fiveHour = usage?.five_hour ?? usage?.FiveHour;
            const daily = usage?.daily ?? usage?.Daily;
            const grantStatus = String(grant.status ?? grant.Status ?? '').trim().toLowerCase();
            const serviceGroupID = String(grant.service_group_id ?? grant.ServiceGroupID ?? '').trim() || '-';
            const summary = cards.get(serviceGroupID) || {
                serviceGroupID,
                fiveHourLimit: 0,
                fiveHourUsed: 0,
                fiveHourRolling: false,
                fiveHourResetAt: '',
                dailyLimit: 0,
                dailyUsed: 0,
                dailyResetAt: '',
                permanent: false,
                expiresAt: '',
                status: '',
                retryAfterSeconds: 0,
                retryAfterAt: '',
            };
            cards.set(serviceGroupID, {
                ...summary,
                fiveHourLimit: summary.fiveHourLimit + numeric(limits?.five_hour ?? limits?.FiveHour),
                fiveHourUsed: summary.fiveHourUsed + numeric(fiveHour?.credits_used ?? fiveHour?.CreditsUsed),
                fiveHourRolling: summary.fiveHourRolling || Boolean(fiveHour?.rolling ?? fiveHour?.Rolling ?? grant.rolling_five_hour ?? grant.RollingFiveHour),
                fiveHourResetAt: latestWindowEnd(summary.fiveHourResetAt, fiveHour?.window_end ?? fiveHour?.WindowEnd),
                dailyLimit: summary.dailyLimit + numeric(limits?.daily ?? limits?.Daily),
                dailyUsed: summary.dailyUsed + numeric(daily?.credits_used ?? daily?.CreditsUsed),
                dailyResetAt: latestWindowEnd(summary.dailyResetAt, daily?.window_end ?? daily?.WindowEnd),
                permanent: summary.permanent || grantIsPermanent(grant),
                expiresAt: latestWindowEnd(summary.expiresAt, grant.expires_at ?? grant.ExpiresAt),
                status: summary.status === 'period_limited' || grantStatus === 'period_limited' ? 'period_limited' : grantStatus || summary.status,
                retryAfterSeconds: Math.max(summary.retryAfterSeconds, numeric(grant.retry_after_seconds ?? grant.RetryAfterSeconds)),
                retryAfterAt: latestWindowEnd(summary.retryAfterAt, grant.retry_after_at ?? grant.RetryAfterAt),
            });
        }
        return cards.size ? [...cards.values()].sort((a, b) => a.serviceGroupID.localeCompare(b.serviceGroupID)) : undefined;
    })();
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
            newUserLimitCards,
        };
    }

    const totals = summarizeHubCreditTotals({
        active,
        credits_total: status?.credits_total ?? status?.CreditsTotal,
        credits_used: status?.credits_used ?? status?.CreditsUsed,
        credits_remaining: status?.credits_remaining ?? status?.CreditsRemaining,
        // A new-user limit card reports its current window allowance through
        // credits_available. It must never become a wallet total/remaining.
        credits_available: !grants.some((grant) => String(grant.source ?? grant.Source ?? '').trim().toLowerCase() === 'new_user_limit_card')
            || grants.some((grant) => String(grant.source ?? grant.Source ?? '').trim().toLowerCase() !== 'new_user_limit_card'
            && (numeric(grant.credits_total ?? grant.CreditsTotal) > 0
                || numeric(grant.credits_used ?? grant.CreditsUsed) > 0
                || numeric(grant.credits_remaining ?? grant.CreditsRemaining) > 0
                || numeric(grant.credits_available ?? grant.CreditsAvailable) > 0))
            ? (status?.credits_available ?? status?.CreditsAvailable)
            : 0,
        grants: grants.filter((grant) => String(grant.source ?? grant.Source ?? '').trim().toLowerCase() !== 'new_user_limit_card'),
    });

    const latestGrantExpiry = latestExpiry(grants
        .filter((grant) => grantCanContributeExpiry(grant) && !grantIsPermanent(grant))
        .map((grant) => String(grant.expires_at ?? grant.ExpiresAt ?? '')));
    const latestExpiredGrantExpiry = latestExpiry(grants
        .filter((grant) => String(grant.status ?? grant.Status ?? '').toLowerCase() === 'expired')
        .map((grant) => String(grant.expires_at ?? grant.ExpiresAt ?? '')));
    const backendEffectiveExpiry = String(status?.effective_expires_at ?? status?.EffectiveExpiresAt ?? '');
    const backendNearestExpiry = String(status?.nearest_expires_at ?? status?.NearestExpiresAt ?? '');
    const expiresAt = backendEffectiveExpiry || latestGrantExpiry || backendNearestExpiry || latestExpiredGrantExpiry;
    // A validity-only limit card has no wallet balance and no current period
    // allowance. Treat it as unlimited access instead of rendering a fake
    // zero-credit account summary.
    const hasOnlyValidityOnlyBenefit = grants.length > 0
        && grants.every((grant) => String(grant.source ?? grant.Source ?? '').trim().toLowerCase() === 'new_user_limit_card')
        && !newUserLimitCards?.length;
    const unlimited = totals.total <= 0 || hasOnlyValidityOnlyBenefit;
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
        newUserLimitCards,
    };
}
