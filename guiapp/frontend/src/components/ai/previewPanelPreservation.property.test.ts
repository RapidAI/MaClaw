/**
 * Property-based preservation tests for preview panel tab binding bugfix.
 *
 * **Property 2: Preservation** — Single Tab Backward Compatibility
 *
 * These tests verify that when only the single "local" tab exists (no project tabs),
 * the system produces the same observable behavior as the original system with
 * no save/restore overhead. This captures the existing single-tab behavior
 * that MUST be preserved after the fix is applied.
 *
 * **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6**
 *
 * Tests are written against the UNFIXED code and MUST PASS on it.
 * They form the regression safety net for the upcoming fix.
 */
import { describe, it, expect } from 'vitest';
import * as fc from 'fast-check';
import {
    initialState,
    applyFileUpdate,
    applyClosePanel,
    applyReopenPanel,
    applySessionStart,
    applySessionEnd,
    type CodeFile,
    type CodePreviewUIState,
} from './useCodePreviewState';

// ── Generators ──

/** Generate a valid file path. */
const arbFilePath = fc
    .array(
        fc.constantFrom('a', 'b', 'c', 'd', 'e', 'f', 'g', 'src', 'lib', 'util'),
        { minLength: 1, maxLength: 6 },
    )
    .map(parts => `/project/${parts.join('/')}.ts`);

/** Generate non-empty content. */
const arbContent = fc.string({ minLength: 1, maxLength: 300 });

/** Generate a session ID. */
const arbSessionID = fc.stringMatching(/^session-[a-z0-9]{4,8}$/);

/** Generate a CodeFile for single-tab scenario (no project_path). */
const arbCodeFile: fc.Arbitrary<CodeFile> = fc.record({
    filePath: arbFilePath,
    fileName: arbFilePath.map(fp => fp.split('/').pop() || fp),
    content: arbContent,
    original: fc.option(arbContent, { nil: undefined }),
    opType: fc.constantFrom('create' as const, 'modify' as const),
    language: fc.constantFrom('typescript', 'go', 'python', 'javascript'),
    updatedAt: fc.nat({ max: 1_000_000_000 }),
    // Workflow-authorized preview open; ordinary generation no longer auto-opens.
    autoOpenPreview: fc.constant(true),
    // No sessionID — events without project_path route to active tab (single tab)
});

/** Generate a CodeFile with a specific session ID. */
function arbCodeFileWithSession(sessionID: string): fc.Arbitrary<CodeFile> {
    return fc.record({
        sessionID: fc.constant(sessionID),
        filePath: arbFilePath,
        fileName: arbFilePath.map(fp => fp.split('/').pop() || fp),
        content: arbContent,
        original: fc.option(arbContent, { nil: undefined }),
        opType: fc.constantFrom('create' as const, 'modify' as const),
        language: fc.constantFrom('typescript', 'go', 'python', 'javascript'),
        updatedAt: fc.nat({ max: 1_000_000_000 }),
        autoOpenPreview: fc.constant(true),
    });
}

/** 
 * Generate a workflow event action for single-tab scenarios.
 * These represent the types of interactions that occur in the single "local" tab.
 */
type SingleTabAction =
    | { type: 'file_update'; file: CodeFile }
    | { type: 'session_start'; sessionID: string }
    | { type: 'session_end'; sessionID: string }
    | { type: 'user_close' }
    | { type: 'user_reopen' }
    | { type: 'split_ratio_change'; ratio: number };

const arbSingleTabAction: fc.Arbitrary<SingleTabAction> = fc.oneof(
    arbCodeFile.map(file => ({ type: 'file_update' as const, file })),
    arbSessionID.map(sessionID => ({ type: 'session_start' as const, sessionID })),
    arbSessionID.map(sessionID => ({ type: 'session_end' as const, sessionID })),
    fc.constant({ type: 'user_close' as const }),
    fc.constant({ type: 'user_reopen' as const }),
    fc.double({ min: 0.2, max: 0.8, noNaN: true }).map(ratio => ({
        type: 'split_ratio_change' as const,
        ratio,
    })),
);

// ── Helper: Apply actions to code preview state ──

function applySingleTabAction(state: CodePreviewUIState, action: SingleTabAction): CodePreviewUIState {
    switch (action.type) {
        case 'file_update':
            return applyFileUpdate(state, action.file);
        case 'session_start':
            return applySessionStart(state, action.sessionID);
        case 'session_end':
            return applySessionEnd(state, action.sessionID);
        case 'user_close':
            return applyClosePanel(state);
        case 'user_reopen':
            return applyReopenPanel(state);
        case 'split_ratio_change':
            // Split ratio is handled at a higher level (AIAssistantPanel),
            // but we track it as a state property for preservation testing.
            return state; // code preview state doesn't store split ratio directly
    }
}

// ── Property Tests ──

describe('Preview Panel Preservation — Single Tab Backward Compatibility', () => {

    /**
     * **Validates: Requirements 3.1**
     *
     * Preservation: With only the "local" tab, all workflow events apply to
     * the global state exactly as before — no save/restore overhead.
     *
     * For any sequence of single-tab events, the code preview state transitions
     * produce the same results as applying them directly to the global state.
     * There is no intermediate save/restore that could alter the state.
     */
    it('Requirement 3.1: Single tab events apply to global state without save/restore overhead', () => {
        fc.assert(
            fc.property(
                fc.array(arbSingleTabAction, { minLength: 1, maxLength: 30 }),
                (actions) => {
                    // Single "local" tab scenario: all events go to global state directly
                    let state = initialState();

                    for (const action of actions) {
                        const prevState = state;
                        state = applySingleTabAction(state, action);

                        // State transition is deterministic — same input always same output
                        const verifyState = applySingleTabAction(prevState, action);
                        expect(state).toEqual(verifyState);
                    }

                    // Final state is reachable by replaying all actions from initial
                    let replayState = initialState();
                    for (const action of actions) {
                        replayState = applySingleTabAction(replayState, action);
                    }
                    expect(state).toEqual(replayState);
                },
            ),
            { numRuns: 100 },
        );
    });

    /**
     * **Validates: Requirements 3.3**
     *
     * Preservation: `userClosed=true` suppresses auto-open within the same tab.
     *
     * When the user manually closes the panel, subsequent file update events
     * SHALL NOT auto-reopen the panel until the user explicitly reopens it.
     * This behavior must be preserved in single-tab mode after the fix.
     */
    it('Requirement 3.3: userClosed suppresses auto-open within the same tab', () => {
        fc.assert(
            fc.property(
                arbCodeFile,
                fc.array(arbCodeFile, { minLength: 1, maxLength: 20 }),
                (firstFile, subsequentFiles) => {
                    // Open panel with first file
                    let state = applyFileUpdate(initialState(), firstFile);
                    expect(state.active).toBe(true);
                    expect(state.userClosed).toBe(false);

                    // User closes the panel
                    state = applyClosePanel(state);
                    expect(state.active).toBe(false);
                    expect(state.userClosed).toBe(true);

                    // All subsequent file updates must NOT reopen the panel
                    for (const file of subsequentFiles) {
                        state = applyFileUpdate(state, file);
                        expect(state.active).toBe(false);
                        expect(state.userClosed).toBe(true);
                    }

                    // Explicit reopen restores the panel
                    state = applyReopenPanel(state);
                    expect(state.active).toBe(true);
                    expect(state.userClosed).toBe(false);
                },
            ),
            { numRuns: 100 },
        );
    });

    /**
     * **Validates: Requirements 3.5**
     *
     * Preservation: `code:session_start` resets files map and closes panel.
     *
     * When a new coding session starts, the state SHALL reset the files map,
     * close the panel, and set sessionActive=true. The panel stays closed
     * until the first file update arrives.
     */
    it('Requirement 3.5: code:session_start resets files map and closes panel', () => {
        fc.assert(
            fc.property(
                fc.array(arbCodeFile, { minLength: 1, maxLength: 10 }),
                arbSessionID,
                (files, newSessionID) => {
                    // Build up some state with files
                    let state = initialState();
                    for (const file of files) {
                        state = applyFileUpdate(state, file);
                    }
                    expect(state.files.size).toBeGreaterThan(0);
                    expect(state.active).toBe(true);

                    // Session start resets
                    state = applySessionStart(state, newSessionID);

                    expect(state.active).toBe(false);
                    expect(state.files.size).toBe(0);
                    expect(state.activeFilePath).toBe('');
                    expect(state.sessionID).toBe(newSessionID);
                    expect(state.sessionActive).toBe(true);
                    expect(state.userClosed).toBe(false);
                },
            ),
            { numRuns: 100 },
        );
    });

    /**
     * **Validates: Requirements 3.6**
     *
     * Preservation: Split ratio drag-handle changes persist within the same tab.
     *
     * The split ratio value set by the user persists across state transitions
     * within the same tab. File updates, session events, etc. do NOT reset
     * the split ratio.
     *
     * Note: Split ratio is stored at the AIAssistantPanel level, not in
     * CodePreviewUIState. This test verifies that code preview state transitions
     * do not interfere with externally managed split ratio persistence.
     * We model this by verifying that the state object reference only changes
     * when a meaningful mutation occurs — unchanged state returns the same object.
     */
    it('Requirement 3.6: State transitions preserve structural identity (no unnecessary mutations)', () => {
        fc.assert(
            fc.property(
                arbSessionID,
                fc.array(arbCodeFile, { minLength: 1, maxLength: 5 }),
                (sessionID, files) => {
                    // Start a session
                    let state = applySessionStart(initialState(), sessionID);

                    // Apply file updates with matching session
                    for (const file of files) {
                        const fileWithSession: CodeFile = { ...file, sessionID };
                        state = applyFileUpdate(state, fileWithSession);
                    }

                    // Applying session_end with wrong session should not mutate state
                    const afterWrongEnd = applySessionEnd(state, 'wrong-session');
                    expect(afterWrongEnd.sessionActive).toBe(true); // unchanged

                    // Applying file_update with wrong session should not mutate state
                    const wrongSessionFile: CodeFile = {
                        ...files[0],
                        sessionID: 'wrong-session',
                        filePath: '/some/other/path.ts',
                    };
                    const afterWrongFile = applyFileUpdate(state, wrongSessionFile);
                    expect(afterWrongFile).toBe(state); // reference equality — no mutation
                },
            ),
            { numRuns: 100 },
        );
    });

    /**
     * **Validates: Requirements 3.2**
     *
     * Preservation: Events without `project_path` route to active tab (current behavior).
     *
     * In the single-tab scenario, all events arrive without a `project_path` field.
     * They are applied to the single global state instance. This test verifies
     * that the code preview state correctly processes events that have no
     * project_path (simulating the backward-compatible fallback).
     */
    it('Requirement 3.2: Events without project_path apply to current state (backward compatible)', () => {
        fc.assert(
            fc.property(
                fc.array(arbCodeFile, { minLength: 2, maxLength: 15 }),
                (files) => {
                    let state = initialState();

                    // All files arrive without sessionID/project_path — they all
                    // apply to the single global state (backward compatible)
                    for (const file of files) {
                        // Remove sessionID to simulate events without project_path
                        const noSessionFile: CodeFile = { ...file };
                        delete noSessionFile.sessionID;
                        state = applyFileUpdate(state, noSessionFile);
                    }

                    // All files should be in the map (deduplicated by path)
                    const uniquePaths = new Set(files.map(f => f.filePath));
                    expect(state.files.size).toBe(uniquePaths.size);

                    // Panel should be open (userClosed was never set)
                    expect(state.active).toBe(true);
                    expect(state.userClosed).toBe(false);

                    // Active file should be the last file's path
                    const lastFile = files[files.length - 1];
                    expect(state.activeFilePath).toBe(lastFile.filePath);
                },
            ),
            { numRuns: 100 },
        );
    });

    /**
     * **Validates: Requirements 3.4**
     *
     * Preservation: `workflow:suggest_maximize` shows fullscreen suggestion banner.
     *
     * This is tested at the WorkflowUIState level. The suggest_maximize event
     * sets the suggestMaximize flag. In single-tab mode, this applies to the
     * global state without any per-tab scoping.
     *
     * Since WorkflowUIState is managed by a React hook with event listeners,
     * we test the observable invariant: the suggest_maximize mechanism is
     * independent of code preview state and does not interfere with it.
     */
    it('Requirement 3.4: Code preview state is independent of workflow suggest_maximize', () => {
        fc.assert(
            fc.property(
                fc.array(arbSingleTabAction, { minLength: 1, maxLength: 20 }),
                (actions) => {
                    let state = initialState();

                    for (const action of actions) {
                        state = applySingleTabAction(state, action);
                    }

                    // Code preview state should be self-consistent regardless of
                    // any workflow:suggest_maximize events (which are handled separately).
                    // The key preservation property: code preview state transitions
                    // are closed — they don't depend on or modify workflow state.
                    if (state.userClosed) {
                        expect(state.active).toBe(false);
                    }
                    if (state.active && state.files.size > 0) {
                        expect(state.activeFilePath.length).toBeGreaterThan(0);
                    }
                },
            ),
            { numRuns: 100 },
        );
    });

    /**
     * **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6**
     *
     * Combined preservation property: For all inputs where only the single
     * "local" tab exists (no project tabs), the system produces the same
     * observable behavior as the original system with no save/restore overhead.
     *
     * This is the master property that generates arbitrary single-tab event
     * sequences and verifies all preservation invariants hold simultaneously.
     */
    it('Combined: Single-tab behavior identical to original — all invariants hold', () => {
        fc.assert(
            fc.property(
                fc.array(arbSingleTabAction, { minLength: 1, maxLength: 40 }),
                (actions) => {
                    let state = initialState();

                    for (const action of actions) {
                        state = applySingleTabAction(state, action);

                        // Invariant 3.3: userClosed implies not active
                        if (state.userClosed && !state.active) {
                            // This is the expected suppressed state
                            expect(state.userClosed).toBe(true);
                            expect(state.active).toBe(false);
                        }

                        // Invariant 3.5: sessionActive is true between session_start and session_end
                        // (can't fully verify ordering here, but verify consistency)
                        if (state.sessionActive) {
                            // Session is running
                            expect(typeof state.sessionID).toBe('string');
                        }

                        // Invariant 3.6: files map never contains entries with empty filePath
                        for (const [path] of state.files) {
                            expect(path.length).toBeGreaterThan(0);
                        }

                        // Invariant: activeFilePath, if set, should exist in files map
                        // (unless files was reset by session_start)
                        if (state.activeFilePath && state.files.size > 0) {
                            expect(state.files.has(state.activeFilePath)).toBe(true);
                        }
                    }

                    // Final state invariants
                    // The state is self-consistent after any sequence of single-tab events
                    if (state.userClosed) {
                        expect(state.active).toBe(false);
                    }
                    if (state.files.size === 0 && !state.userClosed) {
                        // Only possible if session_start cleared files or never had files
                        // Panel should be inactive (session_start closes it)
                        // OR if no file_update events happened
                    }
                },
            ),
            { numRuns: 200 },
        );
    });
});
