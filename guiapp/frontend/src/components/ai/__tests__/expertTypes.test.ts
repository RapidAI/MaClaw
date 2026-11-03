// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { expertDefinitionFromInstallResult, expertPageForIndex, overlayInstalledItems, parseInstalledManagedIndustryExpertsJSON, parseExpertListJSON, withInstalledExpert } from '../expertTypes';

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

describe('installed expert list helpers', () => {
    const installed = expertDefinitionFromInstallResult({
        id: 'pkgexp-new',
        name: '新装专家',
        description: '刚安装',
        icon: '✨',
    })!;

    it('normalizes a market install result into a usable expert card', () => {
        expect(installed).toMatchObject({ id: 'pkgexp-new', name: '新装专家', builtin: false, tools: [], skills: [] });
        expect(expertDefinitionFromInstallResult({ name: 'missing id' })).toBeNull();
        expect(expertDefinitionFromInstallResult(null)).toBeNull();
    });

    it('appends a just-installed expert until the snapshot already contains it', () => {
        const current = [{ id: 'builtin-x', name: 'X', description: '', icon: '', system_prompt: '', tools: [], skills: [], builtin: true, created_at: '', updated_at: '' }];
        const added = withInstalledExpert(current, installed);
        expect(added.map(expert => expert.id)).toEqual(['builtin-x', 'pkgexp-new']);
        expect(withInstalledExpert(added, installed)).toBe(added);
        const renamed = withInstalledExpert(added, { ...installed, name: '新装专家 2', updated_at: '2026-11-03T00:00:00Z' });
        expect(renamed.find(expert => expert.id === 'pkgexp-new')?.name).toBe('新装专家 2');
        expect(added.find(expert => expert.id === 'pkgexp-new')?.name).toBe('新装专家');
    });

    it('points at the page that contains the new card', () => {
        expect(expertPageForIndex(0, 16)).toBe(1);
        expect(expertPageForIndex(16, 16)).toBe(2);
        expect(expertPageForIndex(-1, 16)).toBe(1);
    });

    it('keeps an install on the in-flight list read and drops it once a newer read is authoritative', () => {
        const card = { id: 'pkgexp-new', name: '新装专家' };
        const pending = [{ item: card, stamp: 2 }];
        const early = overlayInstalledItems([{ id: 'builtin-x', name: 'X' }], pending, 2);
        expect(early.list.map(item => item.id)).toEqual(['builtin-x', 'pkgexp-new']);
        expect(early.pending).toEqual(pending);

        const seen = overlayInstalledItems([{ id: 'pkgexp-new', name: '新装专家' }], pending, 2);
        expect(seen.list.map(item => item.id)).toEqual(['pkgexp-new']);
        expect(seen.pending).toEqual([]);

        const later = overlayInstalledItems([{ id: 'builtin-x', name: 'X' }], pending, 3);
        expect(later.list.map(item => item.id)).toEqual(['builtin-x']);
        expect(later.pending).toEqual([]);

        const stale = overlayInstalledItems(
            [{ id: 'pkgexp-new', name: '旧名称', updated_at: '2026-11-02T00:00:00Z' }],
            [{ item: { id: 'pkgexp-new', name: '新装专家', updated_at: '2026-11-03T00:00:00Z' }, stamp: 2 }],
            2,
        );
        expect(stale.list.map(item => item.name)).toEqual(['新装专家']);
        expect(stale.pending).toEqual([]);
        const fresh = overlayInstalledItems(
            [{ id: 'pkgexp-new', name: '服务器名', updated_at: '2026-11-03T00:00:00Z' }],
            [{ item: { id: 'pkgexp-new', name: '新装专家', updated_at: '2026-11-03T00:00:00Z' }, stamp: 2 }],
            2,
        );
        expect(fresh.list.map(item => item.name)).toEqual(['服务器名']);
    });
});
