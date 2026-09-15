import { describe, expect, it } from 'vitest';
import { SESSION_TABS, resolveSessionTab } from '../sessionTabs';

describe('resolveSessionTab', () => {
    it('keeps scheduled and passthrough, and maps everything else to background', () => {
        expect(SESSION_TABS).toEqual(['background', 'scheduled', 'passthrough']);
        expect(resolveSessionTab('scheduled')).toBe('scheduled');
        expect(resolveSessionTab('passthrough')).toBe('passthrough');
        expect(resolveSessionTab('background')).toBe('background');
        expect(resolveSessionTab('remote')).toBe('background');
        expect(resolveSessionTab(undefined)).toBe('background');
    });
});
