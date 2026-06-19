import { describe, expect, it } from 'vitest';
import { buildHubCardStoreURL, buildHubCreditsURL, grantCanContributeExpiry, latestExpiry, numeric } from '../hubCredits';

describe('hubCredits URL builders', () => {
    it('builds card store URL from a trimmed Hub URL', () => {
        expect(buildHubCardStoreURL(' https://hub.example.com/// ')).toBe('https://hub.example.com/card_store');
    });

    it('builds card store URL with tenant scope, email, and token when available', () => {
        expect(buildHubCardStoreURL('https://hub.example.com/', 'tenant acme', 'dev@example.com', 'viewer token')).toBe('https://hub.example.com/card_store?tenant_id=tenant%20acme&email=dev%40example.com#token=viewer%20token');
    });

    it('keeps card store token fallback when email is missing', () => {
        expect(buildHubCardStoreURL('https://hub.example.com/', 'tenant acme', '', 'viewer token')).toBe('https://hub.example.com/card_store?tenant_id=tenant%20acme#token=viewer%20token');
    });

    it('prefers Hub card_store over HubCenter compute-store when both are available', () => {
        expect(buildHubCardStoreURL('https://hub.example.com/', 'tenant acme', 'dev@example.com', 'viewer token', 'https://hubs.example.com/', 'hub_1')).toBe('https://hub.example.com/card_store?tenant_id=tenant%20acme&email=dev%40example.com#token=viewer%20token');
    });

    it('falls back to HubCenter compute-store when Hub URL is missing', () => {
        expect(buildHubCardStoreURL('', 'tenant acme', 'dev@example.com', '', 'https://hubs.example.com/', 'hub_1')).toBe('https://hubs.example.com/compute-store?hub_id=hub_1&tenant_id=tenant%20acme&email=dev%40example.com');
    });

    it('returns empty card store URL when Hub URL is missing', () => {
        expect(buildHubCardStoreURL('   ')).toBe('');
    });

    it('builds credits URL with an encoded viewer token', () => {
        expect(buildHubCreditsURL('https://hub.example.com/', 'viewer token')).toBe('https://hub.example.com/get-credits#token=viewer%20token');
    });

    it('keeps tenant and email query before the credits token hash', () => {
        expect(buildHubCreditsURL('https://hub.example.com/', 'viewer token', 'tenant acme', 'dev@example.com')).toBe('https://hub.example.com/get-credits?tenant_id=tenant%20acme&email=dev%40example.com#token=viewer%20token');
    });

    it('opens the credits page even when viewer token is missing', () => {
        expect(buildHubCreditsURL('https://hub.example.com/', '', 'tenant acme')).toBe('https://hub.example.com/get-credits?tenant_id=tenant%20acme');
    });
});

describe('hubCredits grant helpers', () => {
    it('coerces malformed numeric values to zero', () => {
        expect(numeric('25')).toBe(25);
        expect(numeric('bad')).toBe(0);
        expect(numeric(Number.NaN)).toBe(0);
    });

    it('chooses latest valid expiry before invalid fallback values', () => {
        expect(latestExpiry(['not-a-date', '2026-05-01T00:00:00Z', '2026-06-01T00:00:00Z'])).toBe('2026-06-01T00:00:00Z');
    });

    it('excludes spent point cards but keeps queued legacy cards and unlimited grants for expiry', () => {
        expect(grantCanContributeExpiry({ status: 'exhausted', credits_total: 100, credits_remaining: 0 })).toBe(false);
        expect(grantCanContributeExpiry({ status: 'active', credits_total: 100, credits_remaining: 0, credits_available: 0 })).toBe(false);
        expect(grantCanContributeExpiry({ status: 'queued', credits_total: 100 })).toBe(true);
        expect(grantCanContributeExpiry({ status: 'active', credits_total: 0 })).toBe(true);
    });
});
