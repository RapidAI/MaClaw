import { describe, expect, it } from 'vitest';
import { formatCodingKnowledgeActionError, searchFilterFromScopeTab } from '../codingKnowledgeHelpers';

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

describe('formatCodingKnowledgeActionError', () => {
    it('prefers message, then error, then a fallback', () => {
        expect(formatCodingKnowledgeActionError({ message: ' project path is required ' }, 'fallback')).toBe('project path is required');
        expect(formatCodingKnowledgeActionError({ error: 'store unavailable' }, 'fallback')).toBe('store unavailable');
        expect(formatCodingKnowledgeActionError({ data: { message: 'nested store error' } }, 'fallback')).toBe('nested store error');
        expect(formatCodingKnowledgeActionError({}, 'Save failed.')).toBe('Save failed.');
        expect(formatCodingKnowledgeActionError('plain failure', 'fallback')).toBe('plain failure');
    });
});
