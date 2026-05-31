import { act, renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { useWorkflowState } from '../useWorkflowState';

// Mirror the event-driven harness used by useWorkflowState.test.ts: capture each
// Wails event handler so the test can drive workflow:phase_update / workflow:doc_update /
// workflow:gate_result / workflow:suggest_maximize(_dismiss) callbacks directly.
const eventHandlers = vi.hoisted(() => new Map<string, (data: any) => void>());
vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn((eventName: string, handler: (data: any) => void) => {
        eventHandlers.set(eventName, handler);
        return () => eventHandlers.delete(eventName);
    }),
    EventsOff: vi.fn((eventName: string) => eventHandlers.delete(eventName)),
}));

function emit(eventName: string, data: any) {
    act(() => {
        eventHandlers.get(eventName)?.(data);
    });
}

describe('workflow board/document decoupling regression (Requirement 6.1)', () => {
    beforeEach(() => {
        eventHandlers.clear();
        vi.useRealTimers();
    });

    it('auto-advance moves the active node forward while the latest-document phase stays on the just-completed phase', () => {
        const { result } = renderHook(() => useWorkflowState());

        // Instance starts on the requirements phase and produces its document.
        emit('workflow:phase_update', {
            id: 'wf-decouple',
            status: 'active',
            type: 'coding',
            current_phase: 'requirements',
        });
        emit('workflow:doc_update', {
            phase_id: 'requirements',
            content: '# Requirements',
        });

        // Before the advance the board node and the latest document agree.
        expect(result.current.state.currentPhaseID).toBe('requirements');
        expect(result.current.state.latestDocumentPhaseID).toBe('requirements');

        // The requirements phase auto-advances to the design phase. The new phase
        // carries no document of its own (it has not been produced yet).
        emit('workflow:phase_update', {
            id: 'wf-decouple',
            status: 'active',
            type: 'coding',
            current_phase: 'tech_design',
        });

        // Decoupling: the active board node jumps forward to the newly-advanced
        // phase while the latest-document phase id stays on the just-completed phase.
        expect(result.current.state.currentPhaseID).toBe('design');
        expect(result.current.state.latestDocumentPhaseID).toBe('requirements');
    });

    it('tracks the active-node id and the latest-document phase id as two values that each update without the other', () => {
        const { result } = renderHook(() => useWorkflowState());

        // A phase update with no matching document moves only the active node.
        emit('workflow:phase_update', {
            id: 'wf-independent',
            status: 'active',
            type: 'coding',
            current_phase: 'requirements',
        });
        expect(result.current.state.currentPhaseID).toBe('requirements');
        expect(result.current.state.latestDocumentPhaseID).toBe('');

        // A document update moves only the latest-document phase id; the active
        // board node is unchanged.
        emit('workflow:doc_update', {
            phase_id: 'requirements',
            content: '# Requirements',
        });
        expect(result.current.state.currentPhaseID).toBe('requirements');
        expect(result.current.state.latestDocumentPhaseID).toBe('requirements');

        // Another phase update (auto-advance) moves only the active board node.
        emit('workflow:phase_update', {
            id: 'wf-independent',
            status: 'active',
            type: 'coding',
            current_phase: 'tech_design',
        });
        expect(result.current.state.currentPhaseID).toBe('design');
        expect(result.current.state.latestDocumentPhaseID).toBe('requirements');

        // A document update for the new phase moves only the latest-document phase
        // id; the active board node stays where the engine put it.
        emit('workflow:doc_update', {
            phase_id: 'tech_design',
            content: '# Design',
        });
        expect(result.current.state.currentPhaseID).toBe('design');
        expect(result.current.state.latestDocumentPhaseID).toBe('design');
    });
});

describe('workflow instance reset regression (Requirement 6.3)', () => {
    beforeEach(() => {
        eventHandlers.clear();
        vi.useRealTimers();
    });

    it('replaces the phase-document and gate-result collections with empty collections scoped to the new instance', () => {
        const { result } = renderHook(() => useWorkflowState());

        // Instance 1 accumulates documents and gate results from both the phase
        // snapshot and the per-event channels.
        emit('workflow:phase_update', {
            id: 'workflow-1',
            status: 'active',
            type: 'coding',
            current_phase: 'requirements',
            phase_outputs: {
                requirements: '# Requirements',
            },
            gate_results: {
                requirements: {
                    phase_id: 'requirements',
                    passed: true,
                    items: [],
                    checked_at: '2026-05-09T00:00:00Z',
                },
            },
        });
        emit('workflow:doc_update', {
            phase_id: 'tech_design',
            content: '# Design',
        });
        emit('workflow:gate_result', {
            phase_id: 'tech_design',
            result: {
                phase_id: 'tech_design',
                passed: false,
                items: [],
                checked_at: '2026-05-09T00:10:00Z',
            },
        });

        expect(result.current.state.phaseDocuments.get('requirements')).toBe('# Requirements');
        expect(result.current.state.phaseDocuments.get('design')).toBe('# Design');
        expect(result.current.state.gateResults.has('requirements')).toBe(true);
        expect(result.current.state.gateResults.has('design')).toBe(true);

        // A new instance (different id) starts a different workflow type.
        emit('workflow:phase_update', {
            id: 'workflow-2',
            status: 'active',
            type: 'product_design',
            current_phase: 'problem_discovery',
            phase_outputs: {
                problem_discovery: '# Problem discovery',
            },
            gate_results: {
                problem_discovery: {
                    phase_id: 'problem_discovery',
                    passed: true,
                    items: [],
                    checked_at: '2026-05-09T01:00:00Z',
                },
            },
        });

        // No prior-instance document or gate result remains; the collections hold
        // only the new instance's content.
        expect(result.current.state.phaseDocuments.has('requirements')).toBe(false);
        expect(result.current.state.phaseDocuments.has('design')).toBe(false);
        expect(result.current.state.phaseDocuments.get('problem_discovery')).toBe('# Problem discovery');
        expect(result.current.state.phaseDocuments.size).toBe(1);

        expect(result.current.state.gateResults.has('requirements')).toBe(false);
        expect(result.current.state.gateResults.has('design')).toBe(false);
        expect(result.current.state.gateResults.get('problem_discovery')?.phase_id).toBe('problem_discovery');
        expect(result.current.state.gateResults.size).toBe(1);

        // The latest-document phase id is rescoped to the new instance as well.
        expect(result.current.state.latestDocumentPhaseID).toBe('problem_discovery');
    });
});

describe('workflow reset and completion regression (Requirement 6.7)', () => {
    beforeEach(() => {
        eventHandlers.clear();
        vi.useRealTimers();
    });

    it('on completion closes the board pane, dismisses the maximize suggestion, and retains produced documents until the next instance', () => {
        const { result } = renderHook(() => useWorkflowState());

        // Instance 1 is active, produced a document, and suggested maximizing.
        emit('workflow:phase_update', {
            id: 'workflow-complete-1',
            status: 'active',
            type: 'coding',
            current_phase: 'requirements',
        });
        emit('workflow:doc_update', {
            phase_id: 'requirements',
            content: '# Requirements',
        });
        emit('workflow:suggest_maximize', { workflow_type: 'coding' });

        expect(result.current.state.suggestMaximize).toBe(true);
        expect(result.current.state.splitMode).toBe(true);
        expect(result.current.state.phaseDocuments.get('requirements')).toBe('# Requirements');

        // The workflow reaches completion, and the backend dismisses the suggestion.
        emit('workflow:phase_update', {
            id: 'workflow-complete-1',
            status: 'completed',
            type: 'coding',
            current_phase: 'review',
        });
        emit('workflow:suggest_maximize_dismiss', {});

        // Board pane closed, maximize suggestion dismissed, but the produced
        // document remains viewable.
        expect(result.current.state.active).toBe(false);
        expect(result.current.state.splitMode).toBe(false);
        expect(result.current.state.suggestMaximize).toBe(false);
        expect(result.current.state.suggestMaximizeType).toBe('');
        expect(result.current.state.phaseDocuments.get('requirements')).toBe('# Requirements');

        // Documents are retained right up until the next instance starts, which
        // then replaces them.
        emit('workflow:phase_update', {
            id: 'workflow-complete-2',
            status: 'active',
            type: 'product_design',
            current_phase: 'problem_discovery',
            phase_outputs: {
                problem_discovery: '# Problem discovery',
            },
        });

        expect(result.current.state.phaseDocuments.has('requirements')).toBe(false);
        expect(result.current.state.phaseDocuments.get('problem_discovery')).toBe('# Problem discovery');
    });

    it('on a full reset clears the progress-board phase state and dismisses the maximize suggestion', () => {
        const { result } = renderHook(() => useWorkflowState());

        // Build up board phase state, documents, gate results, and a suggestion.
        emit('workflow:phase_update', {
            id: 'workflow-reset-1',
            status: 'active',
            type: 'coding',
            current_phase: 'requirements',
            phases: [
                { id: 'requirements', name: 'Requirements', index: 0, expects_document: true },
                { id: 'tech_design', name: 'Technical design', index: 1, expects_document: true },
            ],
            phase_outputs: {
                requirements: '# Requirements',
            },
            gate_results: {
                requirements: {
                    phase_id: 'requirements',
                    passed: true,
                    items: [],
                    checked_at: '2026-05-09T00:00:00Z',
                },
            },
        });
        emit('workflow:suggest_maximize', { workflow_type: 'coding' });

        expect(result.current.state.currentPhaseID).toBe('requirements');
        expect(result.current.state.phases.length).toBe(2);
        expect(result.current.state.phaseDocuments.size).toBe(1);
        expect(result.current.state.gateResults.size).toBe(1);
        expect(result.current.state.suggestMaximize).toBe(true);

        // A full reset (null state) tears the board down.
        emit('workflow:phase_update', null);

        expect(result.current.state.active).toBe(false);
        expect(result.current.state.splitMode).toBe(false);
        expect(result.current.state.workflowType).toBe('');
        expect(result.current.state.currentPhaseID).toBe('');
        expect(result.current.state.latestDocumentPhaseID).toBe('');
        expect(result.current.state.phases).toEqual([]);
        expect(result.current.state.phaseDocuments.size).toBe(0);
        expect(result.current.state.gateResults.size).toBe(0);
        expect(result.current.state.suggestMaximize).toBe(false);
        expect(result.current.state.suggestMaximizeType).toBe('');
    });
});
