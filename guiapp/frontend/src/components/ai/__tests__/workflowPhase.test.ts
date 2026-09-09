import { describe, expect, it } from 'vitest';
import { collectWorkflowPhases, normalizeWorkflowPhaseID } from '../workflowPhase';

describe('collectWorkflowPhases', () => {
    it('returns an empty array for non-array / invalid input', () => {
        expect(collectWorkflowPhases(undefined)).toEqual([]);
        expect(collectWorkflowPhases(null)).toEqual([]);
        expect(collectWorkflowPhases('requirements')).toEqual([]);
        expect(collectWorkflowPhases(42)).toEqual([]);
        expect(collectWorkflowPhases({ id: 'requirements' })).toEqual([]);
    });

    it('returns an empty array for an empty list', () => {
        expect(collectWorkflowPhases([])).toEqual([]);
    });

    it('drops entries with an empty or non-string id', () => {
        const phases = collectWorkflowPhases([
            { id: 'requirements', name: 'Requirements', index: 0 },
            { id: '', name: 'Empty id', index: 1 },
            { id: '   ', name: 'Whitespace id', index: 2 },
            { id: 123, name: 'Numeric id', index: 3 },
            { name: 'Missing id', index: 4 },
        ]);

        expect(phases).toEqual([
            { id: 'requirements', name: 'Requirements', index: 0 },
        ]);
    });

    it('skips entries that are not objects', () => {
        const phases = collectWorkflowPhases([
            null,
            undefined,
            'requirements',
            7,
            { id: 'requirements', name: 'Requirements', index: 0 },
        ]);

        expect(phases).toEqual([
            { id: 'requirements', name: 'Requirements', index: 0 },
        ]);
    });

    it('drops duplicate ids keeping the first occurrence', () => {
        const phases = collectWorkflowPhases([
            { id: 'requirements', name: 'First', index: 0 },
            { id: 'requirements', name: 'Second', index: 1 },
            { id: 'design', name: 'Design', index: 2 },
        ]);

        expect(phases).toEqual([
            { id: 'requirements', name: 'First', index: 0 },
            { id: 'design', name: 'Design', index: 2 },
        ]);
    });

    it('dedups by canonical id so aliased entries collapse to one', () => {
        const phases = collectWorkflowPhases([
            { id: 'design', name: 'Canonical design', index: 0 },
            { id: 'tech_design', name: 'Aliased design', index: 1 },
        ]);

        expect(phases).toEqual([
            { id: 'design', name: 'Canonical design', index: 0 },
        ]);
    });

    it('canonicalizes backend phase aliases (tech_design -> design, task_breakdown -> tasks)', () => {
        const phases = collectWorkflowPhases([
            { id: 'requirements', name: 'Requirements', index: 0 },
            { id: 'tech_design', name: 'Technical design', index: 1 },
            { id: 'task_breakdown', name: 'Task breakdown', index: 2 },
        ]);

        expect(phases).toEqual([
            { id: 'requirements', name: 'Requirements', index: 0 },
            { id: 'design', name: 'Technical design', index: 1 },
            { id: 'tasks', name: 'Task breakdown', index: 2 },
        ]);
        // Sanity-check that canonicalization matches the shared normalizer.
        expect(normalizeWorkflowPhaseID('tech_design')).toBe('design');
        expect(normalizeWorkflowPhaseID('task_breakdown')).toBe('tasks');
    });

    it('sorts entries by ascending index', () => {
        const phases = collectWorkflowPhases([
            { id: 'tasks', name: 'Tasks', index: 2 },
            { id: 'requirements', name: 'Requirements', index: 0 },
            { id: 'design', name: 'Design', index: 1 },
        ]);

        expect(phases.map(p => p.id)).toEqual(['requirements', 'design', 'tasks']);
        expect(phases.map(p => p.index)).toEqual([0, 1, 2]);
    });

    it('falls back to positional index when index is missing or non-numeric', () => {
        const phases = collectWorkflowPhases([
            { id: 'requirements', name: 'Requirements' },
            { id: 'design', name: 'Design', index: 'oops' },
            { id: 'tasks', name: 'Tasks' },
        ]);

        expect(phases).toEqual([
            { id: 'requirements', name: 'Requirements', index: 0 },
            { id: 'design', name: 'Design', index: 1 },
            { id: 'tasks', name: 'Tasks', index: 2 },
        ]);
    });

    it('trims the name and defaults to an empty string for a non-string name', () => {
        const phases = collectWorkflowPhases([
            { id: 'requirements', name: '  Requirements  ', index: 0 },
            { id: 'design', name: 42, index: 1 },
            { id: 'tasks', index: 2 },
        ]);

        expect(phases).toEqual([
            { id: 'requirements', name: 'Requirements', index: 0 },
            { id: 'design', name: '', index: 1 },
            { id: 'tasks', name: '', index: 2 },
        ]);
    });

    it('preserves the optional booleans when the payload provides them', () => {
        const phases = collectWorkflowPhases([
            {
                id: 'requirements',
                name: 'Requirements',
                index: 0,
                expects_document: true,
                can_skip: false,
                needs_confirm: true,
            },
        ]);

        expect(phases).toEqual([
            {
                id: 'requirements',
                name: 'Requirements',
                index: 0,
                expectsDocument: true,
                canSkip: false,
                needsConfirm: true,
            },
        ]);
    });

    it('preserves workflow contract metadata from backend payload', () => {
        const phases = collectWorkflowPhases([
            {
                id: 'implementation',
                name: 'Implementation',
                index: 3,
                expects_document: false,
                can_skip: false,
                needs_confirm: false,
                kind: 'execution',
                tool_policy: 'full',
                mutation_scope: 'project',
                activates_orchestrator: true,
            },
        ]);

        expect(phases).toEqual([
            {
                id: 'implementation',
                name: 'Implementation',
                index: 3,
                expectsDocument: false,
                canSkip: false,
                needsConfirm: false,
                kind: 'execution',
                toolPolicy: 'full',
                mutationScope: 'project',
                activatesOrchestrator: true,
            },
        ]);
    });

    it('preserves false-valued optional booleans (does not treat false as absent)', () => {
        const phases = collectWorkflowPhases([
            {
                id: 'implementation',
                name: 'Implementation',
                index: 0,
                expects_document: false,
                can_skip: false,
                needs_confirm: false,
            },
        ]);

        expect(phases).toEqual([
            {
                id: 'implementation',
                name: 'Implementation',
                index: 0,
                expectsDocument: false,
                canSkip: false,
                needsConfirm: false,
            },
        ]);
        const [phase] = phases;
        expect('expectsDocument' in phase).toBe(true);
        expect('canSkip' in phase).toBe(true);
        expect('needsConfirm' in phase).toBe(true);
    });

    it('omits optional booleans when the payload does not provide them', () => {
        const phases = collectWorkflowPhases([
            { id: 'requirements', name: 'Requirements', index: 0 },
        ]);

        expect(phases).toEqual([
            { id: 'requirements', name: 'Requirements', index: 0 },
        ]);
        const [phase] = phases;
        expect('expectsDocument' in phase).toBe(false);
        expect('canSkip' in phase).toBe(false);
        expect('needsConfirm' in phase).toBe(false);
    });

    it('omits optional booleans when the payload provides non-boolean values', () => {
        const phases = collectWorkflowPhases([
            {
                id: 'requirements',
                name: 'Requirements',
                index: 0,
                expects_document: 'true',
                can_skip: 1,
                needs_confirm: null,
            },
        ]);

        expect(phases).toEqual([
            { id: 'requirements', name: 'Requirements', index: 0 },
        ]);
        const [phase] = phases;
        expect('expectsDocument' in phase).toBe(false);
        expect('canSkip' in phase).toBe(false);
        expect('needsConfirm' in phase).toBe(false);
    });

    it('sets only the optional booleans that are provided as actual booleans', () => {
        const phases = collectWorkflowPhases([
            {
                id: 'requirements',
                name: 'Requirements',
                index: 0,
                expects_document: true,
                can_skip: 'no',
            },
        ]);

        expect(phases).toEqual([
            {
                id: 'requirements',
                name: 'Requirements',
                index: 0,
                expectsDocument: true,
            },
        ]);
        const [phase] = phases;
        expect('expectsDocument' in phase).toBe(true);
        expect('canSkip' in phase).toBe(false);
        expect('needsConfirm' in phase).toBe(false);
    });
});
