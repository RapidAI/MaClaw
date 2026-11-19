import { describe, expect, it } from 'vitest';
import { formatMemoryDate } from '../memoryDate';

describe('formatMemoryDate', () => {
    it('keeps invalid input instead of rendering Invalid Date', () => {
        expect(formatMemoryDate('not-a-date', 'en')).toBe('not-a-date');
    });

    it('uses a placeholder for empty input', () => {
        expect(formatMemoryDate('', 'en')).toBe('-');
    });

    it('formats valid timestamps', () => {
        expect(formatMemoryDate('2026-01-01T00:00:00Z', 'en')).not.toBe('Invalid Date');
    });
});
