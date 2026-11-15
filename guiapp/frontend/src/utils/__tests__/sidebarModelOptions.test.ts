import { describe, expect, it } from 'vitest';
import { buildSidebarModelOptions, replaceCachedModelsWithLiveCatalog } from '../sidebarModelOptions';

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

    it('drops cached short keys once a live catalog arrives and keeps the current model', () => {
        expect(replaceCachedModelsWithLiveCatalog({
            configuredModel: 'dfmodel',
            cachedModels: ['auto', 'dfmodel', 'qmodel_38max'],
            fetchedModels: ['Fast', 'DeepSeek-Flash', 'Qwen3.8-Max'],
        })).toEqual(['dfmodel', 'Fast', 'DeepSeek-Flash', 'Qwen3.8-Max']);
    });

    it('keeps the configured name when the live catalog already lists it', () => {
        expect(replaceCachedModelsWithLiveCatalog({
            configuredModel: 'Qwen3.8-Max',
            cachedModels: ['auto'],
            fetchedModels: ['Fast', 'Qwen3.8-Max'],
        })).toEqual(['Qwen3.8-Max', 'Fast']);
    });
});
