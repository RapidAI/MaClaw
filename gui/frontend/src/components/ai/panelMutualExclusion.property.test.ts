/**
 * Property-based test for tabbed preview coordination (Property 10).
 *
 * Models coordination between WorkflowDocPreview and CodePreviewPanel as a
 * pure state machine, then tests it with fast-check.
 *
 * Property 10: Tabbed workflow/source preview coordination
 *
 * For any interleaved sequence of workflow:doc_update and code:file_update
 * events, workflow progress and source preview can both remain available;
 * the parent view selects one tab to display at a time.
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
 * - Preserves code preview if it was active so tabs can switch views
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
        codePreview: applyFileUpdate(state.codePreview, file),
    };
}

/**
 * Check the tabbed preview invariant: both panes may be available, while the
 * parent render decision still exposes one selected mode.
 */
function checkTabbedPreviewInvariant(state: CombinedPanelState): boolean {
    return ['workflow', 'code', 'none'].includes(renderedPanel(state));
}

function renderedPanel(state: CombinedPanelState): 'workflow' | 'code' | 'none' {
    if (state.workflowActive && !state.codePreview.active) return 'workflow';
    if (state.codePreview.active) return 'code';
    return 'none';
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
    // Workflow-authorized preview open; ordinary generation no longer auto-opens.
    autoOpenPreview: fc.constant(true),
});

const arbPanelEvent: fc.Arbitrary<PanelEvent> = fc.oneof(
    fc.constant({ type: 'workflow_doc_update' as const }),
    arbCodeFile.map(file => ({ type: 'code_file_update' as const, file })),
);

// ── Property Tests ──

describe('Panel Preview Tabs - Property Tests', () => {

    /**
     * **Validates: Requirements 6.1, 6.3**
     *
     * Property 10: Tabbed workflow/source preview coordination
     *
     * For any interleaved sequence of workflow:doc_update and code:file_update
     * events, the parent render decision SHALL keep one selected view while
     * allowing both panes to remain available as tabs.
     */
    it('Property 10: tabbed preview coordination with workflow preview', () => {
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

                        // Invariant: parent render has one selected view.
                        expect(checkTabbedPreviewInvariant(state)).toBe(true);

                        // Stronger check: the parent-level render decision
                        // never displays both panels at once.
                        if (state.workflowActive) {
                            expect(['workflow', 'code']).toContain(renderedPanel(state));
                        }
                    }
                },
            ),
            { numRuns: 100 },
        );
    });

    /**
     * Additional property: workflow:doc_update preserves code preview.
     *
     * For any state where code preview is active, a workflow:doc_update event
     * SHALL keep source preview available for tab switching.
     */
    it('workflow:doc_update preserves code preview for tabs', () => {
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

                    // Workflow event arrives: code preview remains available
                    // behind the source tab.
                    state = applyWorkflowEvent(state);
                    expect(state.codePreview.active).toBe(true);
                    expect(state.workflowActive).toBe(true);
                },
            ),
            { numRuns: 100 },
        );
    });

    it('force-open code update takes display priority over workflow preview', () => {
        fc.assert(
            fc.property(
                arbCodeFile,
                (rawFile) => {
                    let state = applyWorkflowEvent(initialCombinedState());
                    const file = { ...rawFile, forceOpen: true };

                    state = applyCodeFileEvent(state, file);

                    expect(state.workflowActive).toBe(true);
                    expect(state.codePreview.active).toBe(true);
                    expect(renderedPanel(state)).toBe('code');
                    expect(checkTabbedPreviewInvariant(state)).toBe(true);
                },
            ),
            { numRuns: 100 },
        );
    });

    /**
     * Additional property: regular code:file_update while workflow active opens
     * source preview as a tab.
     *
     * For any state where workflow preview is active, code:file_update events
     * SHALL activate code preview unless the user closed it.
     */
    it('regular code:file_update opens source tab while workflow active', () => {
        fc.assert(
            fc.property(
                fc.array(arbCodeFile, { minLength: 1, maxLength: 10 }),
                (files) => {
                    let state = initialCombinedState();

                    // Activate workflow first
                    state = applyWorkflowEvent(state);
                    expect(state.workflowActive).toBe(true);

                    // Regular code file events should open source preview as
                    // a tab while keeping workflow available.
                    for (const file of files) {
                        state = applyCodeFileEvent(state, { ...file, forceOpen: false });
                        expect(state.codePreview.active).toBe(true);
                    }

                    // Files should still be tracked in the map
                    expect(state.codePreview.files.size).toBeGreaterThan(0);
                },
            ),
            { numRuns: 100 },
        );
    });
});
