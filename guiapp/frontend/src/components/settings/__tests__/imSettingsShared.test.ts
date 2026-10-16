import { describe, expect, it } from 'vitest';
import {
    aggregateIMConnectionKind,
    connectionBadgeStyle,
    connectionStatusGlyphKind,
    connectionStatusLabel,
    imPendingIsConnecting,
} from '../imSettingsShared';

describe('IM connection status mapping', () => {
    it('normalizes case and treats session expiry as an error', () => {
        expect(connectionStatusGlyphKind('Connected')).toBe('ok');
        expect(connectionStatusGlyphKind('SESSION_EXPIRED')).toBe('error');
        expect(connectionStatusLabel('session_expired', 'en')).toBe('session expired');
        expect(connectionStatusLabel('session_expired', 'zh-Hans')).toBe('会话已过期');
    });

    it('treats QR confirmed and paused as pending', () => {
        expect(connectionStatusGlyphKind('confirmed')).toBe('pending');
        expect(connectionStatusGlyphKind('paused')).toBe('pending');
        expect(connectionStatusGlyphKind('reconnecting')).toBe('pending');
        expect(connectionBadgeStyle('confirmed').color).toBe('var(--theme-primary)');
    });

    it('rolls channel statuses up with connected winning over errors', () => {
        expect(aggregateIMConnectionKind(['disconnected', 'error'])).toBe('error');
        expect(aggregateIMConnectionKind(['connecting', 'error'])).toBe('pending');
        expect(aggregateIMConnectionKind(['error', 'connected', 'connecting'])).toBe('online');
        expect(aggregateIMConnectionKind(['', 'off', 'disconnected'])).toBe('offline');
    });

    it('treats paused-only pending as paused, and any active pending as connecting', () => {
        expect(imPendingIsConnecting(['paused'])).toBe(false);
        expect(imPendingIsConnecting(['paused', 'disconnected'])).toBe(false);
        expect(imPendingIsConnecting(['paused', 'connecting'])).toBe(true);
        expect(imPendingIsConnecting(['reconnecting'])).toBe(true);
        expect(imPendingIsConnecting(['confirmed'])).toBe(true);
    });
});
