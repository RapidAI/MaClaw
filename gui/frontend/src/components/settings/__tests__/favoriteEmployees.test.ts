import { describe, expect, it } from 'vitest';
import { MAX_USER_FAVORITES, normalizeFavoriteEmployeeIds } from '../favoriteEmployees';

describe('normalizeFavoriteEmployeeIds', () => {
    it('deduplicates, trims, and caps favorite employee ids', () => {
        const overflow = Array.from({ length: MAX_USER_FAVORITES + 3 }, (_, i) => `ve-${i + 1}`);
        expect(normalizeFavoriteEmployeeIds([
            ' ve-1 ',
            've-2',
            've-1',
            '',
            null,
            ...overflow.slice(2),
        ])).toEqual(Array.from({ length: MAX_USER_FAVORITES }, (_, i) => `ve-${i + 1}`));
    });

    it('treats missing or malformed config values as empty', () => {
        expect(normalizeFavoriteEmployeeIds(undefined)).toEqual([]);
        expect(normalizeFavoriteEmployeeIds('ve-1')).toEqual([]);
    });
});
