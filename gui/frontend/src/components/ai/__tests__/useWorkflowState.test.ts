import { act, renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { normalizeWorkflowPhaseID } from '../workflowPhase';
import { isWorkflowActive, normalizeWorkflowStatus, WorkflowStatus } from '../workflowStatus';
import { collectWorkflowPhaseDocuments, useWorkflowState } from '../useWorkflowState';

const eventHandlers = vi.hoisted(() => new Map<string, (data: any) => void>());
vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn((eventName: string, handler: (data: any) => void) => {
        eventHandlers.set(eventName, handler);
        return () => eventHandlers.delete(eventName);
    }),
    EventsOff: vi.fn((eventName: string) => eventHandlers.delete(eventName)),
}));

describe('workflow state document collection', () => {
    beforeEach(() => {
        eventHandlers.clear();
        vi.useRealTimers();
    });

    it('normalizes backend phase aliases used by workflow phase_outputs', () => {
        expect(normalizeWorkflowPhaseID('tech_design')).toBe('design');
        expect(normalizeWorkflowPhaseID('task_breakdown')).toBe('tasks');
        expect(normalizeWorkflowPhaseID('requirements')).toBe('requirements');
    });

    it('normalizes workflow status before active-state decisions', () => {
        expect(normalizeWorkflowStatus('active')).toBe(WorkflowStatus.Active);
        expect(isWorkflowActive('active')).toBe(true);
        expect(isWorkflowActive('completed')).toBe(false);
        expect(isWorkflowActive('unknown')).toBe(false);
    });

    it('collects non-empty phase outputs into canonical document slots', () => {
        const docs = collectWorkflowPhaseDocuments({
            requirements: '# Requirements',
            tech_design: '# Design',
            task_breakdown: '   ',
        });

        expect(Array.from(docs.entries())).toEqual([
            ['requirements', '# Requirements'],
            ['design', '# Design'],
        ]);
    });

    it('keeps current workflow phase separate from latest document phase', () => {
        const { result } = renderHook(() => useWorkflowState());

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'coding',
                current_phase: 'tech_design',
                phase_outputs: {
                    requirements: '# Requirements',
                },
            });
        });

        expect(result.current.state.currentPhaseID).toBe('design');
        expect(result.current.state.latestDocumentPhaseID).toBe('');
        expect(result.current.state.phaseDocuments.get('requirements')).toBe('# Requirements');

        act(() => {
            eventHandlers.get('workflow:doc_update')?.({
                phase_id: 'requirements',
                content: '# Requirements v2',
            });
        });

        expect(result.current.state.currentPhaseID).toBe('design');
        expect(result.current.state.latestDocumentPhaseID).toBe('requirements');
        expect(result.current.state.phaseDocuments.get('requirements')).toBe('# Requirements v2');
    });

    it('ignores malformed or blank document update content', () => {
        const { result } = renderHook(() => useWorkflowState());

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'coding',
                current_phase: 'requirements',
            });
            eventHandlers.get('workflow:doc_update')?.({
                phase_id: 'requirements',
                content: { markdown: '# Not a string' },
            });
        });

        expect(result.current.state.phaseDocuments.size).toBe(0);
        expect(result.current.state.latestDocumentPhaseID).toBe('');

        act(() => {
            eventHandlers.get('workflow:doc_update')?.({
                phase_id: 'requirements',
                content: '   # Requirements trimmed   ',
            });
        });

        expect(result.current.state.phaseDocuments.get('requirements')).toBe('# Requirements trimmed');
        expect(result.current.state.latestDocumentPhaseID).toBe('requirements');
    });

    it('accepts late document and gate events even when no workflow is active', () => {
        const { result } = renderHook(() => useWorkflowState());

        act(() => {
            eventHandlers.get('workflow:doc_update')?.({
                phase_id: 'requirements',
                content: '# Late requirements',
            });
            eventHandlers.get('workflow:gate_result')?.({
                phase_id: 'requirements',
                result: { phase_id: 'requirements', passed: true, items: [] },
            });
        });

        // After the fix, late doc_update events are accepted regardless of
        // workflow active status — the document is the final output and should
        // always be displayed (Bug 1.3 / Requirement 2.3).
        expect(result.current.state.phaseDocuments.size).toBe(1);
        expect(result.current.state.phaseDocuments.get('requirements')).toBe('# Late requirements');
        // Gate results still require an active workflow (guard not removed for gates)
        expect(result.current.state.gateResults.size).toBe(0);

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'coding',
                current_phase: 'requirements',
            });
            eventHandlers.get('workflow:doc_update')?.({
                phase_id: 'requirements',
                content: '# Active requirements',
            });
        });

        expect(result.current.state.phaseDocuments.get('requirements')).toBe('# Active requirements');

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'completed',
                type: 'coding',
                current_phase: 'review',
            });
            eventHandlers.get('workflow:doc_update')?.({
                phase_id: 'design',
                content: '# Late design',
            });
            eventHandlers.get('workflow:gate_result')?.({
                phase_id: 'design',
                result: { phase_id: 'design', passed: true, items: [] },
            });
        });

        expect(result.current.state.phaseDocuments.has('design')).toBe(true);
        expect(result.current.state.phaseDocuments.get('design')).toBe('# Late design');
        // Gate results still require active workflow (guard not removed for gates)
        expect(result.current.state.gateResults.has('design')).toBe(false);
    });

    it('keeps an early workflow working directory until the workflow state arrives', () => {
        const { result } = renderHook(() => useWorkflowState());

        act(() => {
            eventHandlers.get('workflow:workdir_set')?.({ path: '  D:/workprj/early  ' });
        });

        expect(result.current.state.workingDir).toBe('');

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                id: 'workflow-early',
                status: 'active',
                type: 'coding',
                current_phase: 'requirements',
            });
        });

        expect(result.current.state.workingDir).toBe('D:/workprj/early');

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                id: 'workflow-early',
                status: 'completed',
                type: 'coding',
                current_phase: 'review',
            });
            eventHandlers.get('workflow:workdir_set')?.({ path: 'D:/workprj/late' });
        });

        expect(result.current.state.workingDir).toBe('D:/workprj/early');
    });

    it('collects backend phase metadata for the preview board', () => {
        const { result } = renderHook(() => useWorkflowState());

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'coding',
                current_phase: 'tech_design',
                phases: [
                    { id: 'requirements', name: 'Requirements', index: 0, expects_document: true },
                    { id: 'tech_design', name: 'Technical design', index: 1, expects_document: true },
                    { id: 'task_breakdown', name: 'Task breakdown', index: 2, expects_document: true },
                    { id: 'implementation', name: 'Implementation', index: 3, expects_document: false },
                ],
            });
        });

        expect(result.current.state.phases).toEqual([
            { id: 'requirements', name: 'Requirements', index: 0, expectsDocument: true },
            { id: 'design', name: 'Technical design', index: 1, expectsDocument: true },
            { id: 'tasks', name: 'Task breakdown', index: 2, expectsDocument: true },
            { id: 'implementation', name: 'Implementation', index: 3, expectsDocument: false },
        ]);
    });

    it('does not let phase output fallback overwrite a received document update', () => {
        const { result } = renderHook(() => useWorkflowState());

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'coding',
                current_phase: 'requirements',
            });
            eventHandlers.get('workflow:doc_update')?.({
                phase_id: 'requirements',
                content: '# Requirements v2',
            });
        });

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'coding',
                current_phase: 'tech_design',
                phase_outputs: {
                    requirements: '# Requirements v1',
                },
            });
        });

        expect(result.current.state.currentPhaseID).toBe('design');
        expect(result.current.state.phaseDocuments.get('requirements')).toBe('# Requirements v2');
    });

    it('preserves completed workflow documents but clears them when a new workflow starts', () => {
        const { result } = renderHook(() => useWorkflowState());

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                id: 'workflow-1',
                status: 'active',
                type: 'coding',
                current_phase: 'requirements',
                phase_outputs: {
                    requirements: '# Requirements',
                },
            });
        });

        expect(result.current.state.phaseDocuments.get('requirements')).toBe('# Requirements');

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                id: 'workflow-1',
                status: 'completed',
                type: 'coding',
                current_phase: 'review',
            });
        });

        expect(result.current.state.phaseDocuments.get('requirements')).toBe('# Requirements');

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                id: 'workflow-2',
                status: 'active',
                type: 'product_design',
                current_phase: 'problem_discovery',
                phase_outputs: {
                    problem_discovery: '# Problem discovery',
                },
            });
        });

        expect(result.current.state.phaseDocuments.has('requirements')).toBe(false);
        expect(result.current.state.phaseDocuments.get('problem_discovery')).toBe('# Problem discovery');
        expect(result.current.state.latestDocumentPhaseID).toBe('problem_discovery');
    });

    it('uses created_at and workflow type as an instance fallback when id is missing', () => {
        const { result } = renderHook(() => useWorkflowState());

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'coding',
                created_at: '2026-05-09T01:00:00Z',
                current_phase: 'requirements',
                phase_outputs: {
                    requirements: '# Requirements',
                },
            });
        });

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'coding',
                created_at: '2026-05-09T02:00:00Z',
                current_phase: 'requirements',
                phase_outputs: {
                    requirements: '# New requirements',
                },
            });
        });

        expect(result.current.state.phaseDocuments.get('requirements')).toBe('# New requirements');
        expect(result.current.state.phaseDocuments.size).toBe(1);
    });

    it('clears stale documents when active workflow type changes without a stable id', () => {
        const { result } = renderHook(() => useWorkflowState());

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'coding',
                current_phase: 'requirements',
                phase_outputs: {
                    requirements: '# Coding requirements',
                },
            });
        });

        expect(result.current.state.phaseDocuments.get('requirements')).toBe('# Coding requirements');

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'product_design',
                current_phase: 'problem_discovery',
                phase_outputs: {
                    problem_discovery: '# Problem discovery',
                },
            });
        });

        expect(result.current.state.workflowType).toBe('product_design');
        expect(result.current.state.phaseDocuments.has('requirements')).toBe(false);
        expect(result.current.state.phaseDocuments.get('problem_discovery')).toBe('# Problem discovery');
        expect(result.current.state.latestDocumentPhaseID).toBe('problem_discovery');
    });

    it('clears stale workflow working directory when a new workflow context starts', () => {
        const { result } = renderHook(() => useWorkflowState());

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                id: 'workflow-1',
                status: 'active',
                type: 'coding',
                current_phase: 'requirements',
            });
            eventHandlers.get('workflow:workdir_set')?.({ path: '  D:/workprj/old  ' });
        });

        expect(result.current.state.workingDir).toBe('D:/workprj/old');

        act(() => {
            eventHandlers.get('workflow:workdir_set')?.({ path: '   ' });
            eventHandlers.get('workflow:phase_update')?.({
                id: 'workflow-2',
                status: 'active',
                type: 'product_design',
                current_phase: 'problem_discovery',
            });
        });

        expect(result.current.state.workingDir).toBe('');
    });

    it('resets manual preview dismissal when a new workflow context starts', () => {
        const { result } = renderHook(() => useWorkflowState());

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                id: 'workflow-1',
                status: 'active',
                type: 'coding',
                current_phase: 'requirements',
            });
        });

        expect(result.current.state.splitMode).toBe(true);

        act(() => {
            result.current.closeDocPreview();
        });

        expect(result.current.state.splitMode).toBe(false);

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                id: 'workflow-2',
                status: 'active',
                type: 'coding',
                current_phase: 'requirements',
            });
        });

        expect(result.current.state.splitMode).toBe(true);
    });

    it('fully resets preview dismissal and transient UI state on workflow reset', () => {
        vi.useFakeTimers();
        const { result } = renderHook(() => useWorkflowState());

        act(() => {
            eventHandlers.get('workflow:text')?.({ text: '旧提示' });
            eventHandlers.get('workflow:suggest_maximize')?.({ workflow_type: 'coding' });
            result.current.closeDocPreview();
        });

        expect(result.current.state.transientText).toBe('旧提示');
        expect(result.current.state.suggestMaximize).toBe(true);
        expect(result.current.state.splitMode).toBe(false);

        act(() => {
            eventHandlers.get('workflow:phase_update')?.(null);
        });

        expect(result.current.state.transientText).toBe('');
        expect(result.current.state.suggestMaximize).toBe(false);

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'coding',
                current_phase: 'requirements',
            });
        });

        expect(result.current.state.splitMode).toBe(true);

        act(() => {
            vi.advanceTimersByTime(5000);
        });

        expect(result.current.state.transientText).toBe('');
        vi.useRealTimers();
    });

    it('does not auto-open the document preview for non-document execution phases', () => {
        const { result } = renderHook(() => useWorkflowState());

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'presentation_design',
                current_phase: 'ppt_generation',
            });
        });

        expect(result.current.state.splitMode).toBe(false);

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'ops_maintenance',
                current_phase: 'controlled_execution',
            });
        });

        expect(result.current.state.splitMode).toBe(false);

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'custom',
                current_phase: 'deploy',
                phases: [
                    { id: 'requirements', name: 'Requirements', index: 0, expects_document: true },
                    { id: 'deploy', name: 'Deploy', index: 1, expects_document: false },
                ],
            });
        });

        expect(result.current.state.splitMode).toBe(false);

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'custom',
                current_phase: 'requirements',
                phases: [
                    { id: 'requirements', name: 'Requirements', index: 0, expects_document: true },
                    { id: 'deploy', name: 'Deploy', index: 1, expects_document: false },
                ],
            });
        });

        expect(result.current.state.splitMode).toBe(true);
    });

    it('keeps workflow board visible when coding workflow enters implementation phase', () => {
        const { result } = renderHook(() => useWorkflowState());

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'coding',
                current_phase: 'requirements',
            });
        });

        expect(result.current.state.splitMode).toBe(true);

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'coding',
                current_phase: 'implementation',
            });
        });

        expect(result.current.state.splitMode).toBe(true);
        expect(result.current.state.currentPhaseID).toBe('implementation');
    });

    it('does not reopen implementation preview after the user closes it', () => {
        const { result } = renderHook(() => useWorkflowState());

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'coding',
                current_phase: 'requirements',
            });
        });
        act(() => {
            result.current.closeDocPreview();
        });
        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'coding',
                current_phase: 'implementation',
            });
        });

        expect(result.current.state.splitMode).toBe(false);
    });

    it('updates fallback phase output snapshots until a doc update becomes authoritative', () => {
        const { result } = renderHook(() => useWorkflowState());

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'coding',
                current_phase: 'requirements',
                phase_outputs: {
                    requirements: '# Requirements draft',
                },
            });
        });

        expect(result.current.state.phaseDocuments.get('requirements')).toBe('# Requirements draft');

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'coding',
                current_phase: 'requirements',
                phase_outputs: {
                    requirements: '# Requirements refined',
                },
            });
        });

        expect(result.current.state.phaseDocuments.get('requirements')).toBe('# Requirements refined');

        act(() => {
            eventHandlers.get('workflow:doc_update')?.({
                phase_id: 'requirements',
                content: '# Requirements persisted',
            });
        });

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'coding',
                current_phase: 'tech_design',
                phase_outputs: {
                    requirements: '# Requirements stale snapshot',
                    tech_design: '# Design fallback',
                },
            });
        });

        expect(result.current.state.phaseDocuments.get('requirements')).toBe('# Requirements persisted');
        expect(result.current.state.phaseDocuments.get('design')).toBe('# Design fallback');
    });

    it('normalizes gate result keys and payload phase ids from snapshots and events', () => {
        const { result } = renderHook(() => useWorkflowState());

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'coding',
                current_phase: 'tech_design',
                gate_results: {
                    tech_design: {
                        phase_id: 'tech_design',
                        passed: true,
                        items: [],
                        checked_at: '2026-05-09T00:00:00Z',
                    },
                },
            });
        });

        expect(result.current.state.gateResults.get('design')?.phase_id).toBe('design');

        act(() => {
            eventHandlers.get('workflow:gate_result')?.({
                phase_id: 'task_breakdown',
                result: {
                    phase_id: 'task_breakdown',
                    passed: false,
                    items: [],
                    checked_at: '2026-05-09T00:00:00Z',
                },
            });
        });

        expect(result.current.state.gateResults.get('tasks')?.phase_id).toBe('tasks');
    });

    it('normalizes missing gate result item arrays defensively', () => {
        const { result } = renderHook(() => useWorkflowState());

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'coding',
                current_phase: 'requirements',
                gate_results: {
                    requirements: {
                        phase_id: 'requirements',
                        passed: true,
                    },
                },
            });
        });

        expect(result.current.state.gateResults.get('requirements')?.items).toEqual([]);

        act(() => {
            eventHandlers.get('workflow:gate_result')?.({
                phase_id: 'task_breakdown',
                result: {
                    phase_id: 'task_breakdown',
                    passed: false,
                    items: null,
                },
            });
        });

        expect(result.current.state.gateResults.get('tasks')?.items).toEqual([]);
    });

    it('opens document preview without letting blank phase ids reset the selected document', () => {
        const { result } = renderHook(() => useWorkflowState());

        act(() => {
            eventHandlers.get('workflow:phase_update')?.({
                status: 'active',
                type: 'coding',
                current_phase: 'requirements',
            });
            eventHandlers.get('workflow:doc_update')?.({
                phase_id: 'requirements',
                content: '# Requirements',
            });
        });

        expect(result.current.state.latestDocumentPhaseID).toBe('requirements');

        act(() => {
            result.current.openDocPreview('   ');
        });

        expect(result.current.state.splitMode).toBe(true);
        expect(result.current.state.latestDocumentPhaseID).toBe('requirements');

        act(() => {
            result.current.openDocPreview('tech_design');
        });

        expect(result.current.state.latestDocumentPhaseID).toBe('design');
    });

    it('keeps the latest transient workflow text for a full timeout window', () => {
        vi.useFakeTimers();
        const { result, unmount } = renderHook(() => useWorkflowState());

        act(() => {
            eventHandlers.get('workflow:text')?.({ text: '第一条提示' });
        });

        expect(result.current.state.transientText).toBe('第一条提示');

        act(() => {
            vi.advanceTimersByTime(3000);
            eventHandlers.get('workflow:text')?.({ text: '第二条提示' });
        });

        expect(result.current.state.transientText).toBe('第二条提示');

        act(() => {
            vi.advanceTimersByTime(2500);
        });

        expect(result.current.state.transientText).toBe('第二条提示');

        act(() => {
            vi.advanceTimersByTime(2500);
        });

        expect(result.current.state.transientText).toBe('');
        unmount();
        vi.useRealTimers();
    });
});
