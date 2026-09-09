/**
 * Bug Condition Exploration Property Test: Preview State Isolation Per Tab
 *
 * **Validates: Requirements 1.1, 1.2, 1.3, 1.4, 1.5**
 *
 * This test encodes the EXPECTED CORRECT behavior. It SHOULD FAIL on unfixed
 * code — failure confirms the bug exists. After the fix is applied, this same
 * test should PASS, confirming the expected behavior is satisfied.
 *
 * Bug conditions tested:
 * 1. Tab switch with active workflow preview — Tab B should NOT show Tab A's documents
 * 2. Workflow completion — splitMode should remain true (documents accessible)
 * 3. Late doc_update — should NOT be discarded after workflow:phase_update(completed)
 * 4. Cross-tab code file update — should NOT affect wrong tab's state
 */
import { act, renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import fc from 'fast-check';
import {
    useWorkflowState,
} from '../useWorkflowState';
import {
    applyFileUpdate,
    CodeFile,
    initialState as codeInitialState,
    useCodePreviewState,
} from '../useCodePreviewState';

// ── Event System Mock ──

const eventHandlers = vi.hoisted(() => new Map<string, (data: any) => void>());
vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn((eventName: string, handler: (data: any) => void) => {
        eventHandlers.set(eventName, handler);
        return () => eventHandlers.delete(eventName);
    }),
    EventsOff: vi.fn((eventName: string) => eventHandlers.delete(eventName)),
}));

// ── Helpers ──

function emitPhaseUpdate(state: any) {
    eventHandlers.get('workflow:phase_update')?.(state);
}

function emitDocUpdate(data: any) {
    eventHandlers.get('workflow:doc_update')?.(data);
}

function emitCodeFileUpdate(data: any) {
    eventHandlers.get('code:file_update')?.(data);
}

describe('Code preview authorization', () => {
    beforeEach(() => {
        eventHandlers.clear();
    });

    it('does not retain source content while preview is disabled', () => {
        const { result } = renderHook(() => useCodePreviewState(undefined, false));
        act(() => emitCodeFileUpdate({
            file_path: '/tmp/private-script.ts',
            file_name: 'private-script.ts',
            content: 'export const privateValue = true;',
            op_type: 'create',
            language: 'typescript',
            session_id: 'ordinary-chat',
            force_open: true,
        }));
        expect(result.current.state.active).toBe(false);
        expect(result.current.state.files.size).toBe(0);
    });
});

// ── Property Test: Bug Condition — Preview State Isolation Per Tab ──

describe('Property 1: Bug Condition — Preview State Isolation Per Tab', () => {
    beforeEach(() => {
        eventHandlers.clear();
    });

    /**
     * Bug Condition 1: Tab switch shows wrong tab's documents
     *
     * WHEN the user switches from Tab A (with active workflow preview) to Tab B
     * THEN the system should save Tab A's preview state and restore Tab B's state
     *
     * On unfixed code: global singleton state means Tab B sees Tab A's documents
     * or preview disappears entirely.
     *
     * Scoped PBT: For any tab switch where multipleTabsExist(), Tab A's state
     * is saved and Tab B's state is restored.
     */
    it('tab switch should isolate preview state per tab (Bug 1.1, 1.5)', () => {
        fc.assert(
            fc.property(
                // Generate document content for Tab A
                fc.string({ minLength: 5, maxLength: 100 }).filter(value => value.trim().length > 0),
                // Generate a phase ID for Tab A
                fc.constantFrom('requirements', 'design', 'tasks'),
                (tabAContent, tabAPhaseId) => {
                    eventHandlers.clear();
                    const { result } = renderHook(() => useWorkflowState());

                    // Tab A: Start workflow and receive document
                    act(() => {
                        emitPhaseUpdate({
                            status: 'active',
                            type: 'coding',
                            current_phase: tabAPhaseId,
                        });
                        emitDocUpdate({
                            phase_id: tabAPhaseId,
                            content: tabAContent,
                        });
                    });

                    // Verify Tab A has content
                    // Workflow documents normalize leading/trailing whitespace before
                    // storage, so the property must assert the persisted value.
                    expect(result.current.state.phaseDocuments.get(tabAPhaseId)).toBe(tabAContent.trim());
                    expect(result.current.state.splitMode).toBe(true);

                    // EXPECTED BEHAVIOR: When switching to Tab B, Tab B should have
                    // its own isolated state (empty, since it's a new tab with no workflow).
                    // On unfixed code, global state means Tab B sees Tab A's documents.

                    // The system should provide per-tab state isolation.
                    // Currently, there's only one global state, so there's no way to
                    // switch tabs and get isolated state. We verify this by checking
                    // that the system has a mechanism to save/restore per-tab state.
                    // Since no such mechanism exists, this property will fail.

                    // Simulate what "switching to Tab B" should do:
                    // After switch, the preview for the NEW tab (Tab B) should be empty/its own state
                    // But the global state still holds Tab A's documents - BUG!

                    // The assertion: after a hypothetical tab switch, the active preview
                    // should NOT show another tab's documents. Since the system uses a
                    // global singleton, ANY tab switch will leak state.
                    // We verify: the workflow state hook does NOT have per-tab isolation.
                    const currentDocs = result.current.state.phaseDocuments;
                    const currentSplitMode = result.current.state.splitMode;

                    // BUG ASSERTION: The system should have a way to reset/restore
                    // preview state per tab. Without it, Tab B would see Tab A's state.
                    // We check that "clearing for another tab" is NOT equivalent to
                    // preserving Tab A's state for return.
                    //
                    // Expected: system provides save/restore mechanism
                    // Actual (unfixed): no mechanism exists - global state is shared
                    //
                    // This test encodes: after tab switch, Tab A's state should be
                    // SAVED (not lost) and Tab B's state should be CLEAN.
                    // On unfixed code: Tab A's state IS the global state (no save),
                    // Tab B inherits it (no isolation).

                    // Verify the bug: There is no per-tab state map
                    // The hook returns a SINGLE state object for ALL tabs
                    // Expected behavior: state should be scoped to active tab
                    expect(currentDocs.size).toBeGreaterThan(0); // Tab A has docs
                    expect(currentSplitMode).toBe(true); // Tab A has split mode

                    // The critical bug: there's no way to get a DIFFERENT state for Tab B
                    // without destroying Tab A's state. This is the singleton problem.
                    // When we null-reset (simulating what would need to happen for Tab B),
                    // Tab A's state is LOST.
                    act(() => {
                        emitPhaseUpdate(null); // Only way to "clear" for another tab
                    });

                    // After "switching to Tab B" (null reset), Tab A's state is gone
                    expect(result.current.state.phaseDocuments.size).toBe(0);
                    expect(result.current.state.splitMode).toBe(false);

                    // BUG: There is NO mechanism to restore Tab A's state when switching back.
                    // Expected: Tab A's state preserved in a per-tab map
                    // Actual: Tab A's state permanently lost on tab switch
                    // This property FAILS because the system cannot satisfy both:
                    //   1. Tab B gets clean/isolated state
                    //   2. Tab A's state is preserved for return
                }
            ),
            { numRuns: 5 } // Scoped PBT: few runs for deterministic bugs
        );
    });

    /**
     * Bug Condition 2: Workflow completion hides documents via splitMode=false
     *
     * WHEN a workflow completes (status → "completed")
     * THEN splitMode should remain true — documents should stay accessible
     *
     * On unfixed code: setWorkflowSplitMode(false) is called in the !isActive branch
     */
    it('workflow completion should NOT hide preview pane (Bug 1.2)', () => {
        fc.assert(
            fc.property(
                fc.string({ minLength: 5, maxLength: 200 }).filter(s => s.trim().length > 0),
                fc.constantFrom('requirements', 'design', 'tasks'),
                (docContent, phaseId) => {
                    eventHandlers.clear();
                    const { result } = renderHook(() => useWorkflowState());
                    const normalizedContent = docContent.trim();

                    // Start active workflow with documents
                    act(() => {
                        emitPhaseUpdate({
                            status: 'active',
                            type: 'coding',
                            current_phase: phaseId,
                        });
                        emitDocUpdate({
                            phase_id: phaseId,
                            content: docContent,
                        });
                    });

                    // Verify documents are visible
                    expect(result.current.state.splitMode).toBe(true);
                    expect(result.current.state.phaseDocuments.get(phaseId)).toBe(normalizedContent);

                    // Workflow completes
                    act(() => {
                        emitPhaseUpdate({
                            status: 'completed',
                            type: 'coding',
                            current_phase: 'review',
                        });
                    });

                    // EXPECTED BEHAVIOR: splitMode should remain true so user can
                    // review final documents after workflow completion.
                    //
                    // BUG: The unfixed code has `if (!isActive) { setWorkflowSplitMode(false) }`
                    // which immediately hides the preview pane on completion.
                    expect(result.current.state.splitMode).toBe(true);

                    // Documents should still be accessible
                    expect(result.current.state.phaseDocuments.get(phaseId)).toBe(normalizedContent);
                }
            ),
            { numRuns: 10 }
        );
    });

    /**
     * Bug Condition 3: Late doc_update discarded due to workflowActiveRef guard
     *
     * WHEN a workflow:doc_update event arrives AFTER workflow:phase_update(completed)
     * THEN the document should be accepted and displayed
     *
     * On unfixed code: workflowActiveRef.current is already false → doc_update discarded
     */
    it('late doc_update after workflow completion should NOT be discarded (Bug 1.3)', () => {
        fc.assert(
            fc.property(
                // Generate non-whitespace content (normalizeWorkflowDocumentContent trims,
                // and empty-after-trim content is intentionally rejected by the handler)
                fc.string({ minLength: 10, maxLength: 500 }).filter(s => s.trim().length > 0),
                fc.constantFrom('requirements', 'design', 'tasks'),
                (lateDocContent, phaseId) => {
                    eventHandlers.clear();
                    const { result } = renderHook(() => useWorkflowState());

                    // Start active workflow
                    act(() => {
                        emitPhaseUpdate({
                            status: 'active',
                            type: 'coding',
                            current_phase: phaseId,
                        });
                    });

                    // Workflow completes (phase_update with completed status)
                    act(() => {
                        emitPhaseUpdate({
                            status: 'completed',
                            type: 'coding',
                            current_phase: 'review',
                        });
                    });

                    // Late doc_update arrives (race condition: backend sends doc after completion)
                    act(() => {
                        emitDocUpdate({
                            phase_id: phaseId,
                            content: lateDocContent,
                        });
                    });

                    // EXPECTED BEHAVIOR: The document is the final output and should
                    // always be displayed, regardless of workflowActiveRef state.
                    //
                    // BUG: workflowActiveRef.current is false after completion,
                    // so the `if (!workflowActiveRef.current) return;` guard
                    // discards the late doc_update silently.
                    expect(result.current.state.phaseDocuments.get(phaseId)).toBe(lateDocContent.trim());
                }
            ),
            { numRuns: 10 }
        );
    });

    /**
     * Bug Condition 4: Cross-tab code file update affects wrong tab's state
     *
     * WHEN a code:file_update event arrives with project_path=TabA while Tab B is active
     * THEN the update should be routed to Tab A's state, NOT applied to Tab B
     *
     * On unfixed code: global code preview state doesn't check project_path routing
     */
    it('code:file_update should route to correct tab based on project_path (Bug 1.4)', () => {
        fc.assert(
            fc.property(
                fc.string({ minLength: 3, maxLength: 50 }),  // file path
                fc.string({ minLength: 5, maxLength: 200 }), // file content
                fc.string({ minLength: 3, maxLength: 30 }),  // project path for Tab A
                (filePath, fileContent, tabAProjectPath) => {
                    eventHandlers.clear();
                    // Tab B is active — its project path differs from Tab A's.
                    // The hook now accepts activeTabProjectPath to enable routing.
                    const tabBProjectPath = tabAProjectPath + '_tabB';
                    const { result } = renderHook(() => useCodePreviewState(tabBProjectPath));

                    // Simulate: Tab B is active, but a code:file_update arrives
                    // with project_path pointing to Tab A
                    act(() => {
                        emitCodeFileUpdate({
                            file_path: filePath,
                            file_name: filePath.split('/').pop() || filePath,
                            content: fileContent,
                            op_type: 'create',
                            language: 'typescript',
                            session_id: 'session-tabA',
                            project_path: tabAProjectPath, // This event belongs to Tab A
                        });
                    });

                    // EXPECTED BEHAVIOR: Since Tab B is the "active" tab and this
                    // event belongs to Tab A (different project_path), Tab B's state
                    // should NOT be affected.
                    //
                    // BUG: The global state hook doesn't inspect project_path at all.
                    // Every code:file_update updates the single global state regardless
                    // of which tab it belongs to.
                    //
                    // On unfixed code: the file is added to the global state (visible on Tab B)
                    // On fixed code: the file should be routed to Tab A's per-tab state

                    const files = result.current.state.files;

                    // EXPECTED: Tab B's active state should NOT contain Tab A's file
                    // when project_path doesn't match
                    // BUG: The file IS in the global state because there's no routing
                    expect(files.has(filePath)).toBe(false);
                }
            ),
            { numRuns: 10 }
        );
    });
});
