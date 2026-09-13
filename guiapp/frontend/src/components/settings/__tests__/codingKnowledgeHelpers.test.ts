import { describe, expect, it } from 'vitest';
import { searchFilterFromScopeTab } from '../codingKnowledgeHelpers';

describe('searchFilterFromScopeTab', () => {
    it('does not constrain the all tab', () => {
        expect(searchFilterFromScopeTab('all')).toEqual({});
    });

    it('maps universal and project tabs to scope', () => {
        expect(searchFilterFromScopeTab('universal')).toEqual({ scope: 'universal' });
        expect(searchFilterFromScopeTab('project')).toEqual({ scope: 'project' });
    });

    it('maps language tabs to language scope', () => {
        expect(searchFilterFromScopeTab('go')).toEqual({ scope: 'language', language: 'go' });
        expect(searchFilterFromScopeTab('python')).toEqual({ scope: 'language', language: 'python' });
    });
});
