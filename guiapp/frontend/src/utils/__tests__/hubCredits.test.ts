import { describe, expect, it } from 'vitest';
import { buildHubCardStoreURL, buildHubCreditsURL, buildHubMaclawAppManualURL, buildHubPetPackHelpURL, buildHubPetStoreURL, normalizeHubGuideLang, grantCanContributeExpiry, latestExpiry, numeric, summarizeHubCreditTotals } from '../hubCredits';

describe('hubCredits URL builders', () => {
    it('builds card store URL from a trimmed Hub URL', () => {
        expect(buildHubCardStoreURL(' https://hub.example.com/// ')).toBe('https://hub.example.com/card_store');
    });

    it('builds card store URL with tenant scope, account identity, email, and token when available', () => {
        expect(buildHubCardStoreURL('https://hub.example.com/', 'tenant acme', 'dev@example.com', 'viewer token')).toBe('https://hub.example.com/card_store?tenant_id=tenant%20acme&account=dev%40example.com&email=dev%40example.com#token=viewer%20token');
    });

    it('keeps card store token fallback when email is missing', () => {
        expect(buildHubCardStoreURL('https://hub.example.com/', 'tenant acme', '', 'viewer token')).toBe('https://hub.example.com/card_store?tenant_id=tenant%20acme#token=viewer%20token');
    });

    it('prefers Hub card_store over HubCenter compute-store when both are available', () => {
        expect(buildHubCardStoreURL('https://hub.example.com/', 'tenant acme', 'dev@example.com', 'viewer token', 'https://hubs.example.com/', 'hub_1')).toBe('https://hub.example.com/card_store?tenant_id=tenant%20acme&account=dev%40example.com&email=dev%40example.com#token=viewer%20token');
    });

    it('falls back to HubCenter compute-store when Hub URL is missing', () => {
        expect(buildHubCardStoreURL('', 'tenant acme', 'dev@example.com', '', 'https://hubs.example.com/', 'hub_1')).toBe('https://hubs.example.com/compute-store?hub_id=hub_1&tenant_id=tenant%20acme&account=dev%40example.com&email=dev%40example.com');
    });

    it('includes friendly hub and tenant names in the HubCenter compute-store fallback', () => {
        expect(buildHubCardStoreURL('', 'tenant acme', 'dev@example.com', '', 'https://hubs.example.com/', 'hub_1', 'Acme Hub', 'Acme Tenant')).toBe('https://hubs.example.com/compute-store?hub_id=hub_1&tenant_id=tenant%20acme&hub_name=Acme%20Hub&tenant_name=Acme%20Tenant&account=dev%40example.com&email=dev%40example.com');
    });

    it('prefers phone/email account identity and still carries stable user ID', () => {
        expect(buildHubCardStoreURL('https://hub.example.com/', 'tenant acme', 'phone:19900001111', 'viewer token', '', '', '', '', 'usr_123', '19900001111')).toBe('https://hub.example.com/card_store?tenant_id=tenant%20acme&account=phone%3A19900001111&user_id=usr_123&email=phone%3A19900001111&mobile=19900001111#token=viewer%20token');
    });

    it('falls back to phone account when email and user ID are missing', () => {
        expect(buildHubCardStoreURL('https://hub.example.com/', 'tenant acme', '', '', '', '', '', '', '', '19900001111')).toBe('https://hub.example.com/card_store?tenant_id=tenant%20acme&account=phone%3A19900001111&mobile=19900001111');
    });

    it('returns empty card store URL when Hub URL is missing', () => {
        expect(buildHubCardStoreURL('   ')).toBe('');
    });

    it('builds credits URL with an encoded viewer token', () => {
        expect(buildHubCreditsURL('https://hub.example.com/', 'viewer token')).toBe('https://hub.example.com/get-credits#token=viewer%20token');
    });

    it('keeps tenant and email query before the credits token hash', () => {
        expect(buildHubCreditsURL('https://hub.example.com/', 'viewer token', 'tenant acme', 'dev@example.com')).toBe('https://hub.example.com/get-credits?tenant_id=tenant%20acme&account=dev%40example.com&email=dev%40example.com#token=viewer%20token');
    });

    it('prefers phone/email account identity and still carries stable user ID for credits', () => {
        expect(buildHubCreditsURL('https://hub.example.com/', 'viewer token', 'tenant acme', 'phone:19900001111', 'usr_123', '19900001111')).toBe('https://hub.example.com/get-credits?tenant_id=tenant%20acme&account=phone%3A19900001111&user_id=usr_123&email=phone%3A19900001111&mobile=19900001111#token=viewer%20token');
    });

    it('opens the credits page even when viewer token is missing', () => {
        expect(buildHubCreditsURL('https://hub.example.com/', '', 'tenant acme')).toBe('https://hub.example.com/get-credits?tenant_id=tenant%20acme');
    });

    it('builds the MaClaw App Studio manual URL from the Hub URL', () => {
        expect(buildHubMaclawAppManualURL(' https://hub.example.com/// ')).toBe('https://hub.example.com/maclaw-app-manual');
        expect(buildHubMaclawAppManualURL('https://hub.example.com', 'en')).toBe('https://hub.example.com/maclaw-app-manual?lang=en');
        expect(buildHubMaclawAppManualURL('https://hub.example.com', 'zh-Hans')).toBe('https://hub.example.com/maclaw-app-manual?lang=zh');
        expect(buildHubPetPackHelpURL(' https://hub.example.com/// ')).toBe('https://hub.example.com/pet-pack-help');
        expect(buildHubPetPackHelpURL('https://hub.example.com', 'en')).toBe('https://hub.example.com/pet-pack-help?lang=en');
        expect(buildHubPetPackHelpURL('https://hub.example.com', 'zh-Hans')).toBe('https://hub.example.com/pet-pack-help?lang=zh');
        expect(buildHubPetPackHelpURL('')).toBe('');
        expect(buildHubPetStoreURL(' https://hub.example.com/// ', 'en', 'viewer token')).toBe('https://hub.example.com/pet-store?lang=en#token=viewer%20token');
        expect(buildHubPetStoreURL('')).toBe('');
        expect(normalizeHubGuideLang('en-US')).toBe('en');
        expect(normalizeHubGuideLang('zh-Hans')).toBe('zh');
        expect(normalizeHubGuideLang('')).toBe('');
        expect(buildHubMaclawAppManualURL('')).toBe('');
    });
});

describe('summarizeHubCreditTotals', () => {
    it('keeps Total ≈ Used + Remaining when queued point cards are present', () => {
        const totals = summarizeHubCreditTotals({
            active: true,
            credits_total: 21000,
            credits_used: 11015,
            credits_remaining: 9985,
            credits_available: 9985,
            grants: [
                { status: 'active', credits_total: 21000, credits_used: 11015, credits_remaining: 9985 },
                { status: 'queued', credits_total: 10000, credits_used: 0, credits_remaining: 10000 },
            ],
        });
        expect(totals.total).toBe(31000);
        expect(totals.used).toBe(11015);
        expect(totals.remaining).toBe(19985);
        expect(totals.used + totals.remaining).toBe(totals.total);
    });

    it('does not shrink lifetime remaining to a period-available window', () => {
        const totals = summarizeHubCreditTotals({
            active: true,
            credits_remaining: 2293.43,
            credits_available: 2293.43,
            grants: [{
                status: 'active',
                effective: true,
                credits_total: 70000,
                credits_used: 1658.7,
                credits_remaining: 68341.3,
                credits_available: 2293.43,
            }],
        });
        expect(totals.remaining).toBe(68341.3);
        expect(totals.total).toBeGreaterThanOrEqual(70000);
    });

    it('ignores hubcenter compute grants', () => {
        const totals = summarizeHubCreditTotals({
            active: true,
            grants: [{
                source: 'hubcenter_compute',
                status: 'active',
                credits_total: 999999,
                credits_remaining: 999999,
            }],
        });
        expect(totals).toEqual({ total: 0, used: 0, remaining: 0, available: 0 });
    });

    it('counts exhausted grants toward used even when effective is false', () => {
        const totals = summarizeHubCreditTotals({
            active: true,
            grants: [
                { status: 'active', effective: true, credits_total: 1000, credits_used: 100, credits_remaining: 900 },
                { status: 'exhausted', effective: false, credits_total: 500, credits_used: 500, credits_remaining: 0 },
            ],
        });
        expect(totals.total).toBe(1500);
        expect(totals.used).toBe(600);
        expect(totals.remaining).toBe(900);
        expect(totals.used + totals.remaining).toBe(totals.total);
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
