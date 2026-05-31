import { describe, expect, it } from 'vitest';
import { buildHubCardStoreURL, buildHubCreditsURL } from '../hubCredits';

describe('hubCredits URL builders', () => {
    it('builds card store URL from a trimmed Hub URL', () => {
        expect(buildHubCardStoreURL(' https://hub.example.com/// ')).toBe('https://hub.example.com/card_store');
    });

    it('builds card store URL with tenant scope when available', () => {
        expect(buildHubCardStoreURL('https://hub.example.com/', 'tenant acme')).toBe('https://hub.example.com/card_store?tenant_id=tenant%20acme');
    });

    it('returns empty card store URL when Hub URL is missing', () => {
        expect(buildHubCardStoreURL('   ')).toBe('');
    });

    it('builds credits URL with an encoded viewer token', () => {
        expect(buildHubCreditsURL('https://hub.example.com/', 'viewer token')).toBe('https://hub.example.com/get-credits#token=viewer%20token');
    });

    it('keeps tenant query before the credits token hash', () => {
        expect(buildHubCreditsURL('https://hub.example.com/', 'viewer token', 'tenant acme')).toBe('https://hub.example.com/get-credits?tenant_id=tenant%20acme#token=viewer%20token');
    });

    it('opens the credits page even when viewer token is missing', () => {
        expect(buildHubCreditsURL('https://hub.example.com/', '', 'tenant acme')).toBe('https://hub.example.com/get-credits?tenant_id=tenant%20acme');
    });
});
