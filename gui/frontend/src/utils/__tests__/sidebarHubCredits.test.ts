import { describe, expect, it } from 'vitest';
import { normalizeSidebarHubCredits } from '../sidebarHubCredits';

describe('normalizeSidebarHubCredits', () => {
    it('returns unauthorized when Hub has no active state and no grant history', () => {
        expect(normalizeSidebarHubCredits({ active: false })).toEqual({
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
        });
    });

    it('keeps sidebar active when another official grant can cover a period-limited grant', () => {
        const credits = normalizeSidebarHubCredits({
            active: true,
            credit_grants: [
                { status: 'period_limited', credits_total: 100, credits_used: 10, credits_remaining: 90, retry_after_seconds: 3600, expires_at: '2026-05-01T00:00:00Z' },
                { status: 'active', active: true, credits_total: 100, credits_used: 1, credits_remaining: 99, expires_at: '2026-05-06T00:00:00Z' },
                { status: 'queued', active: false, credits_total: 100, credits_used: 0, credits_remaining: 100, expires_at: '2026-06-06T00:00:00Z' },
            ],
        });

        expect(credits?.authorized).toBe(true);
        expect(credits?.serviceActive).toBe(true);
        expect(credits?.status).toBe('active');
        expect(credits?.total).toBe(300);
        // Includes queued remaining so Total ≈ Used + Left (11 used + 289 left).
        expect(credits?.remaining).toBe(289);
        expect(credits?.expiresAt).toBe('2026-06-06T00:00:00Z');
        expect(credits?.retryAfterSeconds).toBe(0);
    });

    it('surfaces period limit retry metadata when all official routes are limited', () => {
        const credits = normalizeSidebarHubCredits({
            active: false,
            credit_grants: [{
                status: 'period_limited',
                credits_total: 100,
                credits_used: 10,
                credits_remaining: 90,
                retry_after_seconds: 3600,
                retry_after_at: '2026-05-06T10:00:00Z',
            }],
        });

        expect(credits?.status).toBe('period_limited');
        expect(credits?.retryAfterSeconds).toBe(3600);
        expect(credits?.retryAfterAt).toBe('2026-05-06T10:00:00Z');
        expect(credits?.remaining).toBe(90);
    });

    it('preserves new-user limit-card windows for the system-status display', () => {
        const credits = normalizeSidebarHubCredits({
            active: true,
            credit_grants: [{
                source: 'new_user_limit_card', status: 'active', active: true, permanent: true,
                period_limits: { five_hour: 10, daily: 25 },
                period_usage: {
                    five_hour: { credits_used: 3, window_end: '2026-08-22T15:00:00Z', rolling: true },
                    daily: { credits_used: 7, window_end: '2026-08-23T00:00:00Z' },
                },
            }],
        });

        expect(credits?.unlimited).toBe(true);
        expect(credits?.expiresAt).toBe('');
        expect(credits?.newUserLimitCards).toEqual([expect.objectContaining({
            serviceGroupID: '-',
            fiveHourLimit: 10, fiveHourUsed: 3, fiveHourRolling: true,
            dailyLimit: 25, dailyUsed: 7, permanent: true,
        })]);
    });

    it('does not render an empty benefit row for a validity-only new-user limit card', () => {
        const credits = normalizeSidebarHubCredits({
            active: true,
            credit_grants: [{
                source: 'new_user_limit_card', status: 'active', active: true, permanent: true,
                period_limits: { five_hour: 0, daily: 0 },
            }],
        });

        expect(credits?.unlimited).toBe(true);
        expect(credits?.newUserLimitCards).toBeUndefined();
    });

    it('treats a validity-only new-user limit card as unlimited access, not a zero-credit wallet', () => {
        const credits = normalizeSidebarHubCredits({
            active: true,
            credit_grants: [{
                source: 'new_user_limit_card', status: 'active', active: true, permanent: true,
                period_limits: { five_hour: 0, daily: 0 },
            }],
        });

        expect(credits?.unlimited).toBe(true);
        expect(credits?.total).toBe(0);
        expect(credits?.remaining).toBe(0);
        expect(credits?.newUserLimitCards).toBeUndefined();
    });

    it('aggregates active limit-card windows and preserves finite benefit validity', () => {
        const credits = normalizeSidebarHubCredits({
            active: true,
            credit_grants: [
                {
                    source: 'new_user_limit_card', status: 'active', active: true,
                    expires_at: '2026-09-01T00:00:00Z',
                    period_limits: { five_hour: 10 },
                    period_usage: { five_hour: { credits_used: 3, window_end: '2026-08-23T10:00:00Z', rolling: true } },
                },
                {
                    source: 'new_user_limit_card', status: 'period_limited', active: false,
                    expires_at: '2026-09-10T00:00:00Z', retry_after_seconds: 3600,
                    period_limits: { daily: 25 },
                    period_usage: { daily: { credits_used: 25, window_end: '2026-08-24T00:00:00Z' } },
                },
            ],
        });

        expect(credits?.newUserLimitCards).toEqual([expect.objectContaining({
            serviceGroupID: '-',
            fiveHourLimit: 10, fiveHourUsed: 3, fiveHourRolling: true,
            dailyLimit: 25, dailyUsed: 25,
            permanent: false, expiresAt: '2026-09-10T00:00:00Z',
            status: 'period_limited', retryAfterSeconds: 3600,
        })]);
    });

    it('keeps independent new-user allowances separate by service group', () => {
        const credits = normalizeSidebarHubCredits({
            active: true,
            credit_grants: [
                {
                    source: 'new_user_limit_card', service_group_id: 'welcome-a', status: 'active', active: true, permanent: true,
                    period_limits: { five_hour: 10 }, period_usage: { five_hour: { credits_used: 3, rolling: true } },
                },
                {
                    source: 'new_user_limit_card', service_group_id: 'welcome-b', status: 'active', active: true, permanent: true,
                    period_limits: { five_hour: 20 }, period_usage: { five_hour: { credits_used: 5, rolling: true } },
                },
            ],
        });

        expect(credits?.newUserLimitCards).toEqual([
            expect.objectContaining({ serviceGroupID: 'welcome-a', fiveHourLimit: 10, fiveHourUsed: 3 }),
            expect.objectContaining({ serviceGroupID: 'welcome-b', fiveHourLimit: 20, fiveHourUsed: 5 }),
        ]);
    });

    it('keeps benefit-group order stable when Hub returns grants in a different order', () => {
        const credits = normalizeSidebarHubCredits({
            active: true,
            credit_grants: [
                { source: 'new_user_limit_card', service_group_id: 'welcome-b', status: 'active', active: true, period_limits: { five_hour: 20 } },
                { source: 'new_user_limit_card', service_group_id: 'welcome-a', status: 'active', active: true, period_limits: { five_hour: 10 } },
            ],
        });

        expect(credits?.newUserLimitCards?.map((card) => card.serviceGroupID)).toEqual(['welcome-a', 'welcome-b']);
    });

    it('falls back to legacy active grants when credit_grants is present but empty', () => {
        const credits = normalizeSidebarHubCredits({
            active: true,
            credit_grants: [],
            active_grants: [{ status: 'active', credits_total: 100, credits_used: 20, credits_remaining: 80 }],
        });

        expect(credits?.authorized).toBe(true);
        expect(credits?.status).toBe('active');
        expect(credits?.remaining).toBe(80);
    });

    it('does not count tenant compute cards as personal sidebar credits', () => {
        const credits = normalizeSidebarHubCredits({
            active: true,
            credit_grants: [{
                source: 'hubcenter_compute',
                status: 'active',
                active: true,
                credits_total: 520000,
                credits_used: 12754.79,
                credits_remaining: 507245.21,
            }],
        });

        expect(credits).toEqual({
            authorized: true,
            serviceActive: true,
            total: 0,
            used: 0,
            remaining: 0,
            available: 0,
            showPeriodAvailable: false,
            tokensPerCredit: 0,
            expiresAt: '',
            unlimited: true,
            status: 'active',
            retryAfterSeconds: 0,
            retryAfterAt: '',
        });
    });

    it('prioritizes a queued future grant over an exhausted older grant', () => {
        const credits = normalizeSidebarHubCredits({
            active: false,
            credit_grants: [
                { status: 'exhausted', credits_total: 100, credits_used: 100, credits_remaining: 0 },
                { status: 'queued', credits_total: 100, credits_used: 0, credits_remaining: 100, retry_after_seconds: 7200 },
            ],
        });

        expect(credits?.status).toBe('queued');
        expect(credits?.retryAfterSeconds).toBe(7200);
    });

    it('uses credits_available as visible remaining credits when remaining is zero', () => {
        const credits = normalizeSidebarHubCredits({
            active: false,
            credit_grants: [{ status: 'period_limited', credits_total: 100, credits_used: 10, credits_remaining: 0, credits_available: 25 }],
        });

        expect(credits?.remaining).toBe(25);
        expect(credits?.total).toBe(100);
    });

    it('sums grant-level available credits when top-level available credits are missing', () => {
        const credits = normalizeSidebarHubCredits({
            active: true,
            credits_total: 0,
            credits_used: 0,
            credits_remaining: 0,
            credits_available: 0,
            credit_grants: [
                { status: 'active', active: true, credits_total: 0, credits_used: 0, credits_remaining: 0, credits_available: 120.25 },
                { status: 'active', active: true, credits_total: 0, credits_used: 0, credits_remaining: 0, credits_available: 80.75 },
            ],
        });

        expect(credits?.serviceActive).toBe(true);
        expect(credits?.remaining).toBe(201);
        expect(credits?.total).toBe(201);
    });

    it('prefers currently available credits over blocked remaining credits while service is active', () => {
        const credits = normalizeSidebarHubCredits({
            active: true,
            credits_remaining: 4900,
            credits_available: 10000,
            credit_grants: [
                { status: 'period_limited', active: false, credits_total: 5000, credits_used: 100, credits_remaining: 4900, expires_at: '2026-05-31T00:00:00Z' },
                { status: 'queued', active: false, credits_total: 10000, credits_used: 0, credits_remaining: 10000, expires_at: '2027-05-31T00:00:00Z' },
            ],
        });

        expect(credits?.serviceActive).toBe(true);
        // Lifetime remaining (period-limited left + queued) for Total ≈ Used + Left.
        expect(credits?.remaining).toBe(14900);
        expect(credits?.total).toBe(15000);
    });

    it('reports active status when service is covered by an early-start eligible queued point card', () => {
        const credits = normalizeSidebarHubCredits({
            active: true,
            credits_remaining: 10000,
            credits_available: 10000,
            credit_grants: [
                { status: 'period_limited', active: false, retry_after_seconds: 3600, credits_total: 5000, credits_used: 100, credits_remaining: 4900, expires_at: '2026-05-31T00:00:00Z' },
                { status: 'queued', active: false, retry_after_seconds: 7200, credits_total: 10000, credits_used: 0, credits_remaining: 10000, expires_at: '2027-05-31T00:00:00Z' },
            ],
        });

        expect(credits?.status).toBe('active');
        expect(credits?.retryAfterSeconds).toBe(0);
        expect(credits?.retryAfterAt).toBe('');
        // Lifetime remaining: period-limited left (4900) + queued (10000).
        expect(credits?.remaining).toBe(14900);
    });

    it('matches service redemption total by including queued future credits', () => {
        const credits = normalizeSidebarHubCredits({
            active: true,
            credits_total: 55000,
            credits_used: 5757.027,
            credits_remaining: 49064.005,
            credits_available: 49064.005,
            credit_grants: [
                { status: 'active', active: true, credits_total: 5000, credits_used: 4607.093, credits_remaining: 392.907, expires_at: '2026-06-06T06:44:00Z' },
                { status: 'active', active: true, credits_total: 50000, credits_used: 1149.934, credits_remaining: 48850.066, expires_at: '2026-08-01T09:36:00Z' },
                { status: 'queued', active: false, credits_total: 1, credits_used: 0, credits_remaining: 1, expires_at: '2026-08-06T06:44:00Z' },
                { status: 'queued', active: false, credits_total: 300, credits_used: 0, credits_remaining: 300, expires_at: '2026-08-07T06:44:00Z' },
            ],
        });

        expect(credits?.total).toBe(55301);
        expect(credits?.used).toBe(5757.027);
        // Remaining includes queued balances so Total ≈ Used + Left.
        expect(credits?.remaining).toBeCloseTo(55301 - 5757.027, 3);
    });

    it('keeps Total ≈ Used + Left when total includes a queued point-card top-up', () => {
        const credits = normalizeSidebarHubCredits({
            active: true,
            credits_total: 21000,
            credits_used: 11015,
            credits_remaining: 9985,
            credits_available: 9985,
            credit_grants: [
                { status: 'active', active: true, credits_total: 21000, credits_used: 11015, credits_remaining: 9985, expires_at: '2028-07-05T00:00:00Z' },
                { status: 'queued', active: false, credits_total: 10000, credits_used: 0, credits_remaining: 10000, expires_at: '2029-07-05T00:00:00Z' },
            ],
        });

        expect(credits?.total).toBe(31000);
        expect(credits?.used).toBe(11015);
        expect(credits?.remaining).toBe(19985);
        expect((credits?.used || 0) + (credits?.remaining || 0)).toBe(credits?.total);
    });

    it('keeps the effective recharge-card balance when top-level remaining is only the period window', () => {
        const credits = normalizeSidebarHubCredits({
            active: true,
            credits_total: 81001,
            credits_used: 12659.7,
            credits_remaining: 2293.43,
            credits_available: 2293.43,
            credit_grants: [
                {
                    status: 'active',
                    active: true,
                    effective: true,
                    source: 'card',
                    credits_total: 70000,
                    credits_used: 1658.7,
                    credits_remaining: 68341.3,
                    credits_available: 2293.43,
                    period_limits: { monthly: 40000 },
                    expires_at: '2027-06-21T10:09:00Z',
                },
                {
                    status: 'exhausted',
                    active: false,
                    effective: false,
                    source: 'default_new_user_backfill',
                    credits_total: 1000,
                    credits_used: 1000,
                    credits_remaining: 0,
                    expires_at: '2026-07-18T11:20:00Z',
                },
                {
                    status: 'exhausted',
                    active: false,
                    effective: false,
                    source: 'card',
                    credits_total: 1,
                    credits_used: 1,
                    credits_remaining: 0,
                    expires_at: '2027-06-20T05:58:00Z',
                },
                {
                    status: 'exhausted',
                    active: false,
                    effective: false,
                    source: 'card',
                    credits_total: 10000,
                    credits_used: 10000,
                    credits_remaining: 0,
                    expires_at: '2027-06-20T07:30:00Z',
                },
            ],
        });

        expect(credits?.total).toBe(81001);
        expect(credits?.used).toBe(12659.7);
        expect(credits?.remaining).toBe(68341.3);
    });

    it('recognizes PascalCase period limits from backend credit grants', () => {
        const credits = normalizeSidebarHubCredits({
            Active: true,
            CreditsTotal: 70000,
            CreditsUsed: 1658.7,
            CreditsRemaining: 2293.43,
            CreditsAvailable: 2293.43,
            CreditGrants: [{
                Status: 'active',
                Active: true,
                Effective: true,
                Source: 'card',
                CreditsTotal: 70000,
                CreditsUsed: 1658.7,
                CreditsRemaining: 68341.3,
                CreditsAvailable: 2293.43,
                PeriodLimits: { Monthly: 40000 },
            }],
        });

        expect(credits?.remaining).toBe(68341.3);
    });

    it('keeps expired grants authorized for explanation but exposes zero currently available credits', () => {
        const credits = normalizeSidebarHubCredits({
            active: false,
            credits_available: 0,
            credit_grants: [{
                status: 'expired',
                credits_total: 100,
                credits_used: 10,
                credits_remaining: 90,
                expires_at: '2026-05-05T12:13:17Z',
            }],
        });

        expect(credits?.authorized).toBe(true);
        expect(credits?.status).toBe('expired');
        expect(credits?.remaining).toBe(0);
        expect(credits?.expiresAt).toBe('2026-05-05T12:13:17Z');
    });

    it('ignores invalid expiry values when a valid grant expiry exists', () => {
        const credits = normalizeSidebarHubCredits({
            active: true,
            credit_grants: [
                { status: 'active', active: true, expires_at: 'not-a-date', credits_total: 10, credits_remaining: 10 },
                { status: 'queued', active: false, expires_at: '2026-06-06T00:00:00Z', credits_total: 10, credits_remaining: 10 },
            ],
        });

        expect(credits?.expiresAt).toBe('2026-06-06T00:00:00Z');
    });

    it('does not extend visible validity with spent point-card grants', () => {
        const credits = normalizeSidebarHubCredits({
            active: true,
            credit_grants: [
                { status: 'active', active: true, expires_at: '2026-05-06T00:00:00Z', credits_total: 10, credits_remaining: 10 },
                { status: 'exhausted', active: false, expires_at: '2027-06-06T00:00:00Z', credits_total: 100, credits_used: 100, credits_remaining: 0 },
            ],
        });

        expect(credits?.expiresAt).toBe('2026-05-06T00:00:00Z');
    });

    it('keeps queued legacy grant expiry when remaining fields are absent', () => {
        const credits = normalizeSidebarHubCredits({
            active: true,
            credit_grants: [
                { status: 'active', active: true, expires_at: '2026-05-06T00:00:00Z', credits_total: 10, credits_remaining: 10 },
                { status: 'queued', active: false, expires_at: '2026-06-06T00:00:00Z', credits_total: 100 },
            ],
        });

        expect(credits?.expiresAt).toBe('2026-06-06T00:00:00Z');
    });

    it('guards malformed numeric fields from leaking NaN into sidebar credits', () => {
        const credits = normalizeSidebarHubCredits({
            active: true,
            credits_total: 'bad' as any,
            credits_used: 'also-bad' as any,
            credits_remaining: 'nope' as any,
            credits_available: '25' as any,
            tokens_per_credit: 'bad' as any,
            credit_grants: [{ status: 'active', active: true, credits_total: 'bad' as any, credits_remaining: 'bad' as any }],
        });

        expect(credits?.total).toBe(25);
        expect(credits?.used).toBe(0);
        expect(credits?.remaining).toBe(25);
        expect(credits?.tokensPerCredit).toBe(0);
    });


    it('exposes period available separately when below lifetime remaining', () => {
        const credits = normalizeSidebarHubCredits({
            active: true,
            credits_total: 70000,
            credits_used: 1658.7,
            credits_remaining: 68341.3,
            credits_available: 2293.43,
            credit_grants: [{
                status: 'active',
                active: true,
                credits_total: 70000,
                credits_used: 1658.7,
                credits_remaining: 68341.3,
                credits_available: 2293.43,
                period_limits: { monthly: 40000 },
            }],
        });
        expect(credits?.remaining).toBe(68341.3);
        expect(credits?.available).toBe(2293.43);
        expect(credits?.showPeriodAvailable).toBe(true);
    });
});
