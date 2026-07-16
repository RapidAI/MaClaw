import { describe, expect, it } from 'vitest';
import { parseWailsJSON } from '../UtilitiesPage';

describe('parseWailsJSON', () => {
    it('parses Hub list string into surveys array', () => {
        const raw = JSON.stringify({
            surveys: [
                { id: 's1', short_code: 'A3F9K2', title: '午餐', status: 'draft' },
            ],
        });
        const res = parseWailsJSON<{ surveys: Array<{ id: string }> }>(raw);
        expect(res.surveys).toHaveLength(1);
        expect(res.surveys[0].id).toBe('s1');
    });

    it('passes through already-parsed objects', () => {
        const obj = { id: 'x', title: 'T' };
        expect(parseWailsJSON(obj)).toEqual(obj);
    });

    it('parses create/get survey detail string for selected.id', () => {
        const raw = JSON.stringify({
            id: 'uuid-1',
            short_code: 'B1C2D3',
            title: 'Anon',
            status: 'draft',
            bindings: [],
            questions: [{ id: 'q1', type: 'single_choice', title: 'OK?' }],
        });
        const detail = parseWailsJSON<{ id: string; short_code: string }>(raw);
        expect(detail.id).toBe('uuid-1');
        expect(detail.short_code).toBe('B1C2D3');
    });

    it('parses stats string', () => {
        const raw = JSON.stringify({ survey_id: 's1', response_count: 2 });
        const stats = parseWailsJSON<{ response_count: number }>(raw);
        expect(stats.response_count).toBe(2);
    });
});
