import { describe, expect, it } from 'vitest';
import { normalizeSidebarHubCredits } from '../sidebarHubCredits';

describe('normalizeSidebarHubCredits', () => {
    it('returns unauthorized when Hub has no active state and no grant history', () => {
        expect(normalizeSidebarHubCredits({ active: false })).toEqual({
            authorized: false,
            total: 0,
            used: 0,
            remaining: 0,
            tokensPerCredit: 0,
            expiresAt: '',
            unlimited: false,
            status: '',
            retryAfterSeconds: 0,
            retryAfterAt: '',
        });
    });

    it('surfaces active period limit so the sidebar can warn about the current route', () => {
        const credits = normalizeSidebarHubCredits({
            active: true,
            credit_grants: [
                { status: 'period_limited', credits_total: 100, credits_used: 10, credits_remaining: 90, retry_after_seconds: 3600, expires_at: '2026-05-01T00:00:00Z' },
                { status: 'active', active: true, credits_total: 100, credits_used: 1, credits_remaining: 99, expires_at: '2026-05-06T00:00:00Z' },
            ],
        });

        expect(credits?.authorized).toBe(true);
        expect(credits?.serviceActive).toBe(true);
        expect(credits?.status).toBe('period_limited');
        expect(credits?.total).toBe(200);
        expect(credits?.remaining).toBe(189);
        expect(credits?.expiresAt).toBe('2026-05-06T00:00:00Z');
        expect(credits?.retryAfterSeconds).toBe(3600);
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
});
