// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { parseInstalledManagedIndustryExpertsJSON, parseExpertListJSON } from '../expertTypes';

describe('parseInstalledManagedIndustryExpertsJSON', () => {
    it('maps installed entries to definitions keyed by their local expert id', () => {
        const raw = JSON.stringify([
            { asset_id: 'asset-1', local_expert_id: 'managed-local-1', installed: true, name: 'Industry Pro', description: 'Managed expert', icon: '🏭' },
        ]);
        const out = parseInstalledManagedIndustryExpertsJSON(raw);
        expect(out).toHaveLength(1);
        expect(out[0]).toMatchObject({
            id: 'managed-local-1',
            name: 'Industry Pro',
            description: 'Managed expert',
            icon: '🏭',
            managed_industry: true,
            industry_installed: true,
        });
    });

    it('drops placeholders that are not installed locally', () => {
        const raw = JSON.stringify([
            { asset_id: 'asset-1', local_expert_id: '', installed: false, name: 'Paid Expert', purchase_required: true },
            { asset_id: 'asset-2', local_expert_id: '', installed: false, name: 'Installing Expert', auto_installing: true },
            { asset_id: 'asset-3', local_expert_id: 'managed-local-3', installed: true, name: 'Usable Expert' },
        ]);
        const out = parseInstalledManagedIndustryExpertsJSON(raw);
        expect(out.map(e => e.name)).toEqual(['Usable Expert']);
    });

    it('returns an empty list for invalid payloads', () => {
        expect(parseInstalledManagedIndustryExpertsJSON('')).toEqual([]);
        expect(parseInstalledManagedIndustryExpertsJSON('not json')).toEqual([]);
        expect(parseInstalledManagedIndustryExpertsJSON('{}')).toEqual([]);
        expect(parseInstalledManagedIndustryExpertsJSON(null)).toEqual([]);
    });
});

describe('parseExpertListJSON', () => {
    it('still parses the ordinary expert list unchanged', () => {
        const raw = JSON.stringify([{ id: 'builtin-x', name: 'X', description: '', icon: '', system_prompt: '', tools: [], skills: [], builtin: true, created_at: '', updated_at: '' }]);
        const out = parseExpertListJSON(raw);
        expect(out).toHaveLength(1);
        expect(out[0].id).toBe('builtin-x');
    });
});
