import { describe, expect, it } from 'vitest';
import {
    modelIdsEqual,
    resolveQuickModelList,
    resolveQuickModelMenuSections,
} from '../assistantQuickModelMenu';

describe('resolveQuickModelList', () => {
    it('puts configured first, trims, de-dupes, and merges catalog options', () => {
        expect(resolveQuickModelList(['a', 'b'], 'a')).toEqual(['a', 'b']);
        expect(resolveQuickModelList([' grok-3 '], 'grok-4.5')).toEqual(['grok-4.5', 'grok-3']);
        expect(resolveQuickModelList([], '  grok-4.5  ')).toEqual(['grok-4.5']);
        expect(resolveQuickModelList(undefined, '  ')).toEqual([]);
        expect(resolveQuickModelList(['a', 'a', ' b '], 'a')).toEqual(['a', 'b']);
    });
});

describe('resolveQuickModelMenuSections', () => {
    it('hides the provider section when only one provider and models are listed', () => {
        const sections = resolveQuickModelMenuSections({
            providers: [{ name: 'xAI-Grok', url: '', isHubService: false, configured: true }],
            modelList: ['grok-4.5', 'grok-3'],
            currentModel: 'grok-4.5',
            hasSwitchModel: true,
        });
        expect(sections.showModels).toBe(true);
        expect(sections.showProviders).toBe(false);
        expect(sections.switchableProviders).toEqual([]);
    });

    it('shows providers when alternatives exist or models are unavailable', () => {
        const multi = resolveQuickModelMenuSections({
            providers: [
                { name: 'hub', url: '', isHubService: true, configured: true },
                { name: 'openai', url: 'https://x', isHubService: false, configured: true },
            ],
            modelList: ['gpt-5'],
            currentModel: 'gpt-5',
            hasSwitchModel: true,
        });
        expect(multi.showProviders).toBe(true);
        expect(multi.switchableProviders.map((p) => p.name)).toEqual(['openai']);

        const noModels = resolveQuickModelMenuSections({
            providers: [{ name: 'hub', url: '', isHubService: true, configured: true }],
            modelList: [],
            hasSwitchModel: true,
        });
        expect(noModels.showModels).toBe(false);
        expect(noModels.showProviders).toBe(true);
    });

    it('treats whitespace-only currentModel as empty for section visibility', () => {
        const sections = resolveQuickModelMenuSections({
            providers: [{ name: 'hub', url: '', isHubService: true, configured: true }],
            modelList: [],
            currentModel: '   ',
            hasSwitchModel: true,
        });
        expect(sections.showModels).toBe(false);
        expect(sections.showProviders).toBe(true);
    });
});

describe('modelIdsEqual', () => {
    it('compares trimmed model ids', () => {
        expect(modelIdsEqual('grok-4.5', ' grok-4.5 ')).toBe(true);
        expect(modelIdsEqual('a', 'b')).toBe(false);
        expect(modelIdsEqual(undefined, '')).toBe(true);
    });
});
