import { act, renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { useWorkflowState } from '../useWorkflowState';

// Simulated Wails event bus: EventsOn registers handlers we can fire from the test.
const eventHandlers = vi.hoisted(() => new Map<string, (data: any) => void>());
vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn((eventName: string, handler: (data: any) => void) => {
        eventHandlers.set(eventName, handler);
        return () => eventHandlers.delete(eventName);
    }),
    EventsOff: vi.fn((eventName: string) => eventHandlers.delete(eventName)),
}));

function emitPhaseUpdate(state: any): void {
    eventHandlers.get('workflow:phase_update')?.(state);
}

function emitDocUpdate(phaseID: string, content: string): void {
    eventHandlers.get('workflow:doc_update')?.({ phase_id: phaseID, content });
}

function emitGateResult(phaseID: string, result: any): void {
    eventHandlers.get('workflow:gate_result')?.({ phase_id: phaseID, result });
}

// A NeedsConfirm phase that also produces a preview document.
function needsConfirmDocumentPhases() {
    return [
        { id: 'requirements', name: 'Requirements', index: 0, expects_document: true, needs_confirm: true },
        { id: 'tech_design', name: 'Technical design', index: 1, expects_document: true, needs_confirm: true },
        { id: 'implementation', name: 'Implementation', index: 2, expects_document: false, needs_confirm: false },
    ];
}

describe('workflow preview gate — NeedsConfirm preview flow (Requirement 6.5)', () => {
    beforeEach(() => {
        eventHandlers.clear();
        vi.useRealTimers();
    });

    it('opens the split-pane preview, renders the phase document, and surfaces the gate result when a NeedsConfirm phase produces a document', () => {
        const { result } = renderHook(() => useWorkflowState());

        // A confirmation-required (NeedsConfirm) phase produces its requirement
        // document together with its quality-gate result, mirroring the engine's
        // confirmation-phase snapshot.
        act(() => {
            emitPhaseUpdate({
                id: 'workflow-1',
                status: 'active',
                type: 'coding',
                current_phase: 'requirements',
                phases: needsConfirmDocumentPhases(),
                phase_outputs: {
                    requirements: '# Requirements\n\nBuild a snake game.',
                },
                gate_results: {
                    requirements: {
                        phase_id: 'requirements',
                        passed: true,
                        items: [
                            { description: 'Functional requirements present', passed: true },
                            { description: 'Acceptance criteria present', passed: true },
                        ],
                        checked_at: '2026-05-09T00:00:00Z',
                    },
                },
            });
        });

        // Preview opens automatically (the user has not manually closed it).
        expect(result.current.state.splitMode).toBe(true);

        // The preview renders that phase's document content.
        expect(result.current.state.phaseDocuments.get('requirements')).toBe('# Requirements\n\nBuild a snake game.');
        expect(result.current.state.latestDocumentPhaseID).toBe('requirements');

        // The phase's quality-gate result (pass + checked items) is surfaced.
        const gate = result.current.state.gateResults.get('requirements');
        expect(gate?.passed).toBe(true);
        expect(gate?.items).toEqual([
            { description: 'Functional requirements present', passed: true },
            { description: 'Acceptance criteria present', passed: true },
        ]);
    });

    it('surfaces a failing gate result with its checked items for a NeedsConfirm phase delivered via live doc/gate events', () => {
        const { result } = renderHook(() => useWorkflowState());

        // Establish the active NeedsConfirm phase first (no document yet).
        act(() => {
            emitPhaseUpdate({
                id: 'workflow-1',
                status: 'active',
                type: 'coding',
                current_phase: 'tech_design',
                phases: needsConfirmDocumentPhases(),
            });
        });

        // Document and gate arrive as live preview signals.
        act(() => {
            emitDocUpdate('tech_design', '# Technical design\n\nModule layout.');
            emitGateResult('tech_design', {
                phase_id: 'tech_design',
                passed: false,
                items: [
                    { description: 'Architecture defined', passed: true },
                    { description: 'Interfaces defined', passed: false, note: 'missing API contract' },
                ],
                checked_at: '2026-05-09T01:00:00Z',
            });
        });

        // Preview opened, content rendered for that phase (alias tech_design -> design).
        expect(result.current.state.splitMode).toBe(true);
        expect(result.current.state.phaseDocuments.get('design')).toBe('# Technical design\n\nModule layout.');
        expect(result.current.state.latestDocumentPhaseID).toBe('design');

        // Failing gate result with its checked items is surfaced.
        const gate = result.current.state.gateResults.get('design');
        expect(gate?.passed).toBe(false);
        expect(gate?.items).toEqual([
            { description: 'Architecture defined', passed: true },
            { description: 'Interfaces defined', passed: false, note: 'missing API contract' },
        ]);
    });
});

describe('workflow preview gate — manual close is respected (Requirement 6.6)', () => {
    beforeEach(() => {
        eventHandlers.clear();
        vi.useRealTimers();
    });

    it('keeps the preview closed across subsequent phase-update, doc-update, and gate-result events after the user manually closes it', () => {
        const { result } = renderHook(() => useWorkflowState());

        // Active NeedsConfirm workflow auto-opens the preview.
        act(() => {
            emitPhaseUpdate({
                id: 'workflow-1',
                status: 'active',
                type: 'coding',
                current_phase: 'requirements',
                phases: needsConfirmDocumentPhases(),
                phase_outputs: { requirements: '# Requirements' },
            });
        });
        expect(result.current.state.splitMode).toBe(true);

        // User manually closes the preview.
        act(() => {
            result.current.closeDocPreview();
        });
        expect(result.current.state.splitMode).toBe(false);

        // Subsequent phase-update for the SAME instance into another
        // document-producing NeedsConfirm phase must not reopen the preview.
        act(() => {
            emitPhaseUpdate({
                id: 'workflow-1',
                status: 'active',
                type: 'coding',
                current_phase: 'tech_design',
                phases: needsConfirmDocumentPhases(),
                phase_outputs: { tech_design: '# Technical design' },
            });
        });
        expect(result.current.state.splitMode).toBe(false);

        // Subsequent document-update must not reopen the preview...
        act(() => {
            emitDocUpdate('tech_design', '# Technical design v2');
        });
        expect(result.current.state.splitMode).toBe(false);
        // ...but the document is still tracked (close hides the pane, not the data).
        expect(result.current.state.phaseDocuments.get('design')).toBe('# Technical design v2');

        // Subsequent gate-result must not reopen the preview.
        act(() => {
            emitGateResult('tech_design', {
                phase_id: 'tech_design',
                passed: true,
                items: [],
                checked_at: '2026-05-09T02:00:00Z',
            });
        });
        expect(result.current.state.splitMode).toBe(false);
        expect(result.current.state.gateResults.get('design')?.passed).toBe(true);
    });

    it('reopens the preview only when the user explicitly re-opens it', () => {
        const { result } = renderHook(() => useWorkflowState());

        act(() => {
            emitPhaseUpdate({
                id: 'workflow-1',
                status: 'active',
                type: 'coding',
                current_phase: 'requirements',
                phases: needsConfirmDocumentPhases(),
                phase_outputs: { requirements: '# Requirements' },
            });
            result.current.closeDocPreview();
        });
        expect(result.current.state.splitMode).toBe(false);

        // Events keep arriving while closed — preview stays closed.
        act(() => {
            emitDocUpdate('requirements', '# Requirements v2');
            emitPhaseUpdate({
                id: 'workflow-1',
                status: 'active',
                type: 'coding',
                current_phase: 'requirements',
                phases: needsConfirmDocumentPhases(),
            });
        });
        expect(result.current.state.splitMode).toBe(false);

        // User explicitly re-opens the preview.
        act(() => {
            result.current.openDocPreview('requirements');
        });
        expect(result.current.state.splitMode).toBe(true);

        // After re-open, the auto-open logic is active again on the next event.
        act(() => {
            emitPhaseUpdate({
                id: 'workflow-1',
                status: 'active',
                type: 'coding',
                current_phase: 'tech_design',
                phases: needsConfirmDocumentPhases(),
                phase_outputs: { tech_design: '# Technical design' },
            });
        });
        expect(result.current.state.splitMode).toBe(true);
    });

    it('reopens the preview when a new workflow instance starts after a manual close', () => {
        const { result } = renderHook(() => useWorkflowState());

        act(() => {
            emitPhaseUpdate({
                id: 'workflow-1',
                status: 'active',
                type: 'coding',
                current_phase: 'requirements',
                phases: needsConfirmDocumentPhases(),
                phase_outputs: { requirements: '# Requirements' },
            });
            result.current.closeDocPreview();
        });
        expect(result.current.state.splitMode).toBe(false);

        // Events for the same instance must not reopen.
        act(() => {
            emitPhaseUpdate({
                id: 'workflow-1',
                status: 'active',
                type: 'coding',
                current_phase: 'tech_design',
                phases: needsConfirmDocumentPhases(),
                phase_outputs: { tech_design: '# Technical design' },
            });
        });
        expect(result.current.state.splitMode).toBe(false);

        // A new workflow instance starts (different id) — the manual-close
        // choice is scoped to the prior instance and the preview auto-opens.
        act(() => {
            emitPhaseUpdate({
                id: 'workflow-2',
                status: 'active',
                type: 'product_design',
                current_phase: 'problem_discovery',
                phases: [
                    { id: 'problem_discovery', name: 'Problem discovery', index: 0, expects_document: true, needs_confirm: true },
                ],
                phase_outputs: { problem_discovery: '# Problem discovery' },
            });
        });
        expect(result.current.state.splitMode).toBe(true);
        // Prior-instance documents are cleared; the new instance's document shows.
        expect(result.current.state.phaseDocuments.has('requirements')).toBe(false);
        expect(result.current.state.phaseDocuments.get('problem_discovery')).toBe('# Problem discovery');
    });
});
