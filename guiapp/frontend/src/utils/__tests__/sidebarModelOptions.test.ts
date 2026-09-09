import { describe, expect, it } from 'vitest';
import { buildSidebarModelOptions } from '../sidebarModelOptions';

describe('buildSidebarModelOptions', () => {
    it('falls back to the configured model from LLM settings when fetch fails', () => {
        expect(buildSidebarModelOptions({
            configuredModel: 'glm-5.2',
            cachedModels: [],
            fetchedModels: [],
        })).toEqual(['glm-5.2']);
    });

    it('prefers fetched catalog and still keeps configured model first', () => {
        expect(buildSidebarModelOptions({
            configuredModel: 'glm-5.2',
            cachedModels: ['old-model'],
            fetchedModels: ['glm-5.2', 'glm-4', 'deepseek-chat'],
        })).toEqual(['glm-5.2', 'glm-4', 'deepseek-chat']);
    });

    it('uses cached models when fetch is empty', () => {
        expect(buildSidebarModelOptions({
            configuredModel: 'mine',
            cachedModels: ['a', 'b', 'mine'],
            fetchedModels: [],
        })).toEqual(['mine', 'a', 'b']);
    });

    it('dedupes and trims blanks', () => {
        expect(buildSidebarModelOptions({
            configuredModel: '  x  ',
            fetchedModels: ['x', '', ' y ', 'x'],
        })).toEqual(['x', 'y']);
    });

    it('returns empty when nothing is configured or discovered', () => {
        expect(buildSidebarModelOptions({})).toEqual([]);
    });
});
