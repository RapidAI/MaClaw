import { describe, expect, it } from 'vitest';
import { normalizeFavoriteEmployeeIds } from '../favoriteEmployees';

describe('normalizeFavoriteEmployeeIds', () => {
    it('deduplicates, trims, and caps favorite employee ids', () => {
        expect(normalizeFavoriteEmployeeIds([
            ' ve-1 ',
            've-2',
            've-1',
            '',
            null,
            've-3',
            've-4',
            've-5',
            've-6',
            've-7',
        ])).toEqual(['ve-1', 've-2', 've-3', 've-4', 've-5', 've-6']);
    });

    it('treats missing or malformed config values as empty', () => {
        expect(normalizeFavoriteEmployeeIds(undefined)).toEqual([]);
        expect(normalizeFavoriteEmployeeIds('ve-1')).toEqual([]);
    });
});
