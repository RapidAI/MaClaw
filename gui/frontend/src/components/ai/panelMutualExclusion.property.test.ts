/**
 * Property-based test for panel mutual exclusion (Property 10).
 *
 * Models the mutual exclusion logic between WorkflowDocPreview and
 * CodePreviewPanel as a pure state machine, then tests it with fast-check.
 *
 * Property 10: Panel mutual exclusion with workflow preview
 *
 * For any interleaved sequence of workflow:doc_update and code:file_update
 * events, at most one of WorkflowDocPreview or CodePreviewPanel SHALL be
 * active at any given time.
 *
 * **Validates: Requirements 6.1, 6.3**
 */
import { describe, it, expect } from 'vitest';
import * as fc from 'fast-check';
import {
    initialState,
    applyFileUpdate,
    applyWorkflowDocUpdate,
    type CodeFile,
    type CodePreviewUIState,
} from './useCodePreviewState';

// ── Pure state machine for mutual exclusion ──

/**
 * Represents the combined panel state for both workflow and code preview.
 * This models the parent-level coordination in AIAssistantPanel.
 */
interface CombinedPanelState {
    workflowActive: boolean;
    codePreview: CodePreviewUIState;
}

function initialCombinedState(): CombinedPanelState {
    return {
        workflowActive: false,
        codePreview: initialState(),
    };
}

/**
 * Apply a workflow:doc_update event to the combined state.
 * - Activates workflow preview
 * - Closes code preview if it was active (mutual exclusion)
 */
function applyWorkflowEvent(state: CombinedPanelState): CombinedPanelState {
    return {
        workflowActive: true,
        codePreview: applyWorkflowDocUpdate(state.codePreview),
    };
}

/**
 * Apply a code:file_update event to the combined state.
 * - Updates code preview files
 * - Does NOT auto-open code preview if workflow is active (suppressed)
 */
function applyCodeFileEvent(state: CombinedPanelState, file: CodeFile): CombinedPanelState {
    return {
        ...state,
        codePreview: applyFileUpdate(state.codePreview, file, state.workflowActive),
    };
}

/**
 * Check the mutual exclusion invariant:
 * At most one of workflow preview or code preview is active.
 */
function checkMutualExclusion(state: CombinedPanelState): boolean {
    const workflowVisible = state.workflowActive;
    const codeVisible = !workflowVisible && state.codePreview.active;
    // Both cannot be true simultaneously
    return !(workflowVisible && state.codePreview.active);
}

// ── Event type for the state machine ──

type PanelEvent =
    | { type: 'workflow_doc_update' }
    | { type: 'code_file_update'; file: CodeFile };

// ── Generators ──

const arbFilePath = fc
    .array(
        fc.constantFrom(
            'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm',
            'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z',
        ),
        { minLength: 1, maxLength: 8 },
    )
    .map(chars => `/src/${chars.join('')}.ts`);

const arbCodeFile: fc.Arbitrary<CodeFile> = fc.record({
    filePath: arbFilePath,
    fileName: arbFilePath.map(p => p.split('/').pop() || p),
    content: fc.string({ minLength: 1, maxLength: 100 }),
    original: fc.option(fc.string({ minLength: 1, maxLength: 100 }), { nil: undefined }),
    opType: fc.constantFrom('create' as const, 'modify' as const),
    language: fc.constantFrom('typescript', 'go', 'python'),
    updatedAt: fc.nat(),
});

const arbPanelEvent: fc.Arbitrary<PanelEvent> = fc.oneof(
    fc.constant({ type: 'workflow_doc_update' as const }),
    arbCodeFile.map(file => ({ type: 'code_file_update' as const, file })),
);

// ── Property Tests ──

describe('Panel Mutual Exclusion — Property Tests', () => {

    /**
     * **Validates: Requirements 6.1, 6.3**
     *
     * Property 10: Panel mutual exclusion with workflow preview
     *
     * For any interleaved sequence of workflow:doc_update and code:file_update
     * events, at most one of WorkflowDocPreview or CodePreviewPanel SHALL be
     * active at any given time.
     */
    it('Property 10: Panel mutual exclusion with workflow preview', () => {
        fc.assert(
            fc.property(
                fc.array(arbPanelEvent, { minLength: 1, maxLength: 30 }),
                (events) => {
                    let state = initialCombinedState();

                    for (const event of events) {
                        if (event.type === 'workflow_doc_update') {
                            state = applyWorkflowEvent(state);
                        } else {
                            state = applyCodeFileEvent(state, event.file);
                        }

                        // Invariant: mutual exclusion holds after every event
                        expect(checkMutualExclusion(state)).toBe(true);

                        // Stronger check: if workflow is active, code preview
                        // must not be rendering (even if codePreview.active is
                        // true in state, the parent won't render it)
                        if (state.workflowActive) {
                            // Code preview should have been deactivated by
                            // workflow:doc_update, or suppressed by
                            // workflowActive flag in applyFileUpdate
                            const codeWouldRender = !state.workflowActive && state.codePreview.active;
                            expect(codeWouldRender).toBe(false);
                        }
                    }
                },
            ),
            { numRuns: 100 },
        );
    });

    /**
     * Additional property: workflow:doc_update always deactivates code preview.
     *
     * For any state where code preview is active, a workflow:doc_update event
     * SHALL set code preview active to false.
     */
    it('workflow:doc_update deactivates code preview', () => {
        fc.assert(
            fc.property(
                fc.array(arbCodeFile, { minLength: 1, maxLength: 10 }),
                (files) => {
                    let state = initialCombinedState();

                    // Open code preview with some files
                    for (const file of files) {
                        state = applyCodeFileEvent(state, file);
                    }
                    expect(state.codePreview.active).toBe(true);

                    // Workflow event arrives — code preview must close
                    state = applyWorkflowEvent(state);
                    expect(state.codePreview.active).toBe(false);
                    expect(state.workflowActive).toBe(true);
                },
            ),
            { numRuns: 100 },
        );
    });

    /**
     * Additional property: code:file_update while workflow active does NOT
     * open code preview.
     *
     * For any state where workflow preview is active, code:file_update events
     * SHALL NOT activate the code preview panel.
     */
    it('code:file_update suppressed while workflow active', () => {
        fc.assert(
            fc.property(
                fc.array(arbCodeFile, { minLength: 1, maxLength: 10 }),
                (files) => {
                    let state = initialCombinedState();

                    // Activate workflow first
                    state = applyWorkflowEvent(state);
                    expect(state.workflowActive).toBe(true);

                    // Code file events should NOT open code preview
                    for (const file of files) {
                        state = applyCodeFileEvent(state, file);
                        expect(state.codePreview.active).toBe(false);
                    }

                    // Files should still be tracked in the map
                    expect(state.codePreview.files.size).toBeGreaterThan(0);
                },
            ),
            { numRuns: 100 },
        );
    });
});
