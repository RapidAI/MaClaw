/**
 * Property-based tests for useCodePreviewState pure state logic.
 *
 * Tests the exported pure functions directly (no React rendering needed).
 * Uses fast-check with minimum 100 iterations per property.
 *
 * Properties tested:
 *   Property 1: Panel idempotent open on repeated events
 *   Property 2: User close suppresses auto-reopen
 *   Property 3: File map completeness and content correctness
 *   Property 5: No duplicate files on repeated events
 */
import { describe, it, expect } from 'vitest';
import * as fc from 'fast-check';
import {
    initialState,
    applyFileUpdate,
    applyClosePanel,
    applyReopenPanel,
    applyActivatePassive,
    applySelectFile,
    applyCloseFile,
    applyCloseOtherFiles,
    applyCloseFilesToTheRight,
    applyCloseAllFiles,
    applyMoveFile,
    applySetFilePinned,
    applyToggleFilePinned,
    getDisplayFilePaths,
    getMruCycleOrder,
    isCodeFileDirty,
    prunePathList,
    applySessionStart,
    applySessionEnd,
    applyResetSession,
    cloneCodePreviewState,
    type CodeFile,
    type CodePreviewUIState,
} from './useCodePreviewState';

// ── Generators ──

/** Generate a valid file path (Unix-style). */
const arbFilePath = fc
    .array(
        fc.constantFrom(
            'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm',
            'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z',
            '0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
        ),
        { minLength: 1, maxLength: 12 },
    )
    .map(chars => `/src/${chars.join('')}.ts`);

/** Generate a non-empty content string. */
const arbContent = fc.string({ minLength: 1, maxLength: 200 });

/** Generate a CodeFile with a given filePath. */
function arbCodeFileForPath(filePath: string): fc.Arbitrary<CodeFile> {
    return fc.record({
        filePath: fc.constant(filePath),
        fileName: fc.constant(filePath.split(/[/\\]/).pop() || filePath),
        content: arbContent,
        original: fc.option(arbContent, { nil: undefined }),
        opType: fc.constantFrom('create' as const, 'modify' as const),
        language: fc.constantFrom('typescript', 'go', 'python', 'plaintext'),
        updatedAt: fc.nat(),
    });
}

/** Generate a CodeFile with a random path. */
const arbCodeFile: fc.Arbitrary<CodeFile> = arbFilePath.chain(fp => arbCodeFileForPath(fp));
const arbAutoOpenCodeFile: fc.Arbitrary<CodeFile> = arbCodeFile.map(file => ({ ...file, autoOpenPreview: true }));

/** Generate a list of CodeFiles with unique paths. */
const arbUniqueCodeFiles: fc.Arbitrary<CodeFile[]> = fc
    .uniqueArray(arbFilePath, { minLength: 1, maxLength: 10 })
    .chain(paths =>
        fc.tuple(...paths.map(p => arbCodeFileForPath(p)))
    );

// ── Property Tests ──

describe('useCodePreviewState — Property Tests', () => {

    it('ordinary code events never auto-open the preview', () => {
        fc.assert(
            fc.property(
                fc.array(arbCodeFile, { minLength: 1, maxLength: 20 }),
                (files) => {
                    let state = initialState();
                    for (const file of files) {
                        state = applyFileUpdate(state, file);
                        expect(state.active).toBe(false);
                    }
                },
            ),
            { numRuns: 100 },
        );
    });

    /**
     * **Validates: Requirements 1.5**
     *
     * Property 1: Authorized workflow preview remains open on repeated events
     *
     * For any sequence of code file events emitted while the panel is already
     * open, the panel SHALL remain in the active/open state and SHALL NOT
     * re-trigger the open transition.
     */
    it('Property 1: authorized workflow preview remains open on repeated events', () => {
        fc.assert(
            fc.property(
                fc.array(arbAutoOpenCodeFile, { minLength: 2, maxLength: 20 }),
                (files) => {
                    let state = initialState();

                    // Apply all file updates
                    for (const file of files) {
                        state = applyFileUpdate(state, file);
                        // Panel should be active after every event
                        // (userClosed is false in initial state)
                        expect(state.active).toBe(true);
                    }

                    // Panel is still active after all events
                    expect(state.active).toBe(true);
                },
            ),
            { numRuns: 100 },
        );
    });

    /**
     * **Validates: Requirements 1.6**
     *
     * Property 2: User close suppresses auto-reopen
     *
     * For any sequence of workflow-authorized code file events emitted after the user manually
     * closes the panel, the panel SHALL remain closed until reopenPanel()
     * is explicitly called.
     */
    it('Property 2: User close suppresses auto-reopen', () => {
        fc.assert(
            fc.property(
                arbAutoOpenCodeFile,
                fc.array(arbAutoOpenCodeFile, { minLength: 1, maxLength: 20 }),
                (firstFile, subsequentFiles) => {
                    // Open the panel with the first file
                    let state = applyFileUpdate(initialState(), firstFile);
                    expect(state.active).toBe(true);

                    // User closes the panel
                    state = applyClosePanel(state);
                    expect(state.active).toBe(false);
                    expect(state.userClosed).toBe(true);

                    // Subsequent file events should NOT reopen the panel
                    for (const file of subsequentFiles) {
                        state = applyFileUpdate(state, file);
                        expect(state.active).toBe(false);
                        expect(state.userClosed).toBe(true);
                    }

                    // Explicit reopen should work
                    state = applyReopenPanel(state);
                    expect(state.active).toBe(true);
                    expect(state.userClosed).toBe(false);
                },
            ),
            { numRuns: 100 },
        );
    });

    it('forceOpen reopens after user close and while workflow preview is active', () => {
        let state = applyFileUpdate(initialState(), {
            filePath: '/src/a.ts', fileName: 'a.ts', content: 'hello',
            opType: 'modify', language: 'typescript', updatedAt: 1,
        });
        state = applyClosePanel(state);
        expect(state.active).toBe(false);
        expect(state.userClosed).toBe(true);

        state = applyFileUpdate(state, {
            filePath: '/src/b.ts', fileName: 'b.ts', content: 'world',
            opType: 'modify', language: 'typescript', updatedAt: 2, forceOpen: true,
        });

        expect(state.active).toBe(true);
        expect(state.userClosed).toBe(false);
        expect(state.activeFilePath).toBe('/src/b.ts');
    });

    it('forceOpen file update takes over a stale active session', () => {
        let state = applySessionStart(initialState(), 'old-session');
        state = applyFileUpdate(state, {
            sessionID: 'old-session',
            filePath: '/src/old.ts',
            fileName: 'old.ts',
            content: 'old',
            opType: 'modify',
            language: 'typescript',
            updatedAt: 1,
        });
        state = applyClosePanel(state);

        state = applyFileUpdate(state, {
            sessionID: 'local-tools:new',
            filePath: '/src/new.ts',
            fileName: 'new.ts',
            content: 'new',
            opType: 'modify',
            language: 'typescript',
            updatedAt: 2,
            forceOpen: true,
        });

        expect(state.sessionID).toBe('local-tools:new');
        expect(state.sessionActive).toBe(true);
        expect(state.files.has('/src/old.ts')).toBe(false);
        expect(state.files.has('/src/new.ts')).toBe(true);
        expect(state.active).toBe(true);
        expect(state.userClosed).toBe(false);
        expect(state.activeFilePath).toBe('/src/new.ts');
    });

    /**
     * **Validates: Requirements 2.1, 2.4**
     *
     * Property 3: File map completeness and content correctness
     *
     * For any set of code file events with distinct file paths, the files map
     * SHALL contain exactly one entry per unique file path, and selecting any
     * file path via selectFile() SHALL set activeFilePath to that path.
     */
    it('Property 3: File map completeness and content correctness', () => {
        fc.assert(
            fc.property(
                arbUniqueCodeFiles,
                (files) => {
                    let state = initialState();

                    // Apply all file updates
                    for (const file of files) {
                        state = applyFileUpdate(state, file);
                    }

                    // Files map should contain exactly one entry per unique path
                    expect(state.files.size).toBe(files.length);

                    // Each file should be present with correct content
                    for (const file of files) {
                        expect(state.files.has(file.filePath)).toBe(true);
                        const stored = state.files.get(file.filePath)!;
                        expect(stored.content).toBe(file.content);
                        expect(stored.filePath).toBe(file.filePath);
                    }

                    // selectFile should work for any file in the map
                    for (const file of files) {
                        const selected = applySelectFile(state, file.filePath);
                        expect(selected.activeFilePath).toBe(file.filePath);
                    }
                },
            ),
            { numRuns: 100 },
        );
    });

    /**
     * **Validates: Requirements 2.8**
     *
     * Property 5: No duplicate files on repeated events
     *
     * For any sequence of code file events where the same file path appears
     * multiple times, the files map SHALL contain exactly one entry for that
     * path, and its content SHALL equal the content from the most recent event.
     */
    it('Property 5: No duplicate files on repeated events', () => {
        fc.assert(
            fc.property(
                arbFilePath,
                fc.array(arbContent, { minLength: 2, maxLength: 15 }),
                (filePath, contents) => {
                    let state = initialState();

                    // Send multiple updates for the same file path
                    for (const content of contents) {
                        const file: CodeFile = {
                            filePath,
                            fileName: filePath.split(/[/\\]/).pop() || filePath,
                            content,
                            opType: 'modify',
                            language: 'typescript',
                            updatedAt: Date.now(),
                        };
                        state = applyFileUpdate(state, file);
                    }

                    // Should have exactly one entry
                    expect(state.files.size).toBe(1);
                    expect(state.files.has(filePath)).toBe(true);

                    // Content should be from the last update
                    const lastContent = contents[contents.length - 1];
                    expect(state.files.get(filePath)!.content).toBe(lastContent);
                },
            ),
            { numRuns: 100 },
        );
    });

    // ── Additional unit tests for session lifecycle ──

    it('applyCloseFile removes a tab and selects a neighbor when active', () => {
        let state = initialState();
        for (const path of ['/src/a.ts', '/src/b.ts', '/src/c.ts']) {
            state = applyFileUpdate(state, {
                filePath: path,
                fileName: path.split('/').pop()!,
                content: path,
                opType: 'modify',
                language: 'typescript',
                updatedAt: 1,
                forceOpen: true,
            });
        }
        expect(state.activeFilePath).toBe('/src/c.ts');

        state = applyCloseFile(state, '/src/c.ts');
        expect(state.files.has('/src/c.ts')).toBe(false);
        expect(state.files.size).toBe(2);
        // Prefer previous neighbor.
        expect(state.activeFilePath).toBe('/src/b.ts');

        state = applyCloseFile(state, '/src/a.ts');
        expect(state.files.has('/src/a.ts')).toBe(false);
        // Active was b, closing non-active keeps b.
        expect(state.activeFilePath).toBe('/src/b.ts');

        state = applyCloseFile(state, '/src/b.ts');
        expect(state.files.size).toBe(0);
        expect(state.activeFilePath).toBe('');
    });

    it('applyCloseFile is a no-op for unknown paths', () => {
        let state = applyFileUpdate(initialState(), {
            filePath: '/src/a.ts', fileName: 'a.ts', content: 'a',
            opType: 'create', language: 'typescript', updatedAt: 1, forceOpen: true,
        });
        const before = state;
        state = applyCloseFile(state, '/src/missing.ts');
        expect(state).toBe(before);
    });

    it('applyCloseOtherFiles / to-the-right / all match VS Code semantics', () => {
        let state = initialState();
        for (const path of ['/src/a.ts', '/src/b.ts', '/src/c.ts', '/src/d.ts']) {
            state = applyFileUpdate(state, {
                filePath: path,
                fileName: path.split('/').pop()!,
                content: path,
                opType: 'modify',
                language: 'typescript',
                updatedAt: 1,
                forceOpen: true,
            });
        }

        state = applySelectFile(state, '/src/b.ts');
        state = applyCloseOtherFiles(state, '/src/b.ts');
        expect(Array.from(state.files.keys())).toEqual(['/src/b.ts']);
        expect(state.activeFilePath).toBe('/src/b.ts');

        // Rebuild four files in a clean open order (Map insertion order).
        state = applyCloseAllFiles(state);
        for (const path of ['/src/a.ts', '/src/b.ts', '/src/c.ts', '/src/d.ts']) {
            state = applyFileUpdate(state, {
                filePath: path,
                fileName: path.split('/').pop()!,
                content: path,
                opType: 'modify',
                language: 'typescript',
                updatedAt: 1,
                forceOpen: true,
            });
        }
        state = applySelectFile(state, '/src/d.ts');
        state = applyCloseFilesToTheRight(state, '/src/b.ts');
        expect(Array.from(state.files.keys())).toEqual(['/src/a.ts', '/src/b.ts']);
        expect(state.activeFilePath).toBe('/src/b.ts');

        state = applyCloseAllFiles(state);
        expect(state.files.size).toBe(0);
        expect(state.activeFilePath).toBe('');
    });

    it('pin + MRU: pin sorts left, close others/all keep pinned, select updates MRU', () => {
        let state = initialState();
        for (const path of ['/src/a.ts', '/src/b.ts', '/src/c.ts', '/src/d.ts']) {
            state = applyFileUpdate(state, {
                filePath: path,
                fileName: path.split('/').pop()!,
                content: path,
                opType: 'modify',
                language: 'typescript',
                updatedAt: 1,
                forceOpen: true,
            });
        }
        // Last auto-selected is d
        expect(state.mruOrder[0]).toBe('/src/d.ts');

        state = applySelectFile(state, '/src/b.ts');
        expect(state.mruOrder[0]).toBe('/src/b.ts');
        expect(getMruCycleOrder(state.files, state.mruOrder)[0]).toBe('/src/b.ts');

        state = applySetFilePinned(state, '/src/c.ts', true);
        expect(state.pinnedPaths).toEqual(['/src/c.ts']);
        expect(getDisplayFilePaths(state.files, state.pinnedPaths)[0]).toBe('/src/c.ts');

        // Close others keeps keepPath + pinned c
        state = applyCloseOtherFiles(state, '/src/b.ts');
        expect(new Set(state.files.keys())).toEqual(new Set(['/src/b.ts', '/src/c.ts']));

        // Close all keeps pinned
        state = applyFileUpdate(state, {
            filePath: '/src/e.ts', fileName: 'e.ts', content: 'e',
            opType: 'create', language: 'typescript', updatedAt: 1, forceOpen: true,
        });
        state = applyCloseAllFiles(state);
        expect(Array.from(state.files.keys())).toEqual(['/src/c.ts']);
        expect(state.pinnedPaths).toEqual(['/src/c.ts']);

        state = applyToggleFilePinned(state, '/src/c.ts');
        expect(state.pinnedPaths).toEqual([]);
        state = applyCloseAllFiles(state);
        expect(state.files.size).toBe(0);
    });

    it('isCodeFileDirty reflects create/modify/read semantics', () => {
        expect(isCodeFileDirty({ opType: 'read', content: 'a' })).toBe(false);
        expect(isCodeFileDirty({ opType: 'create', content: 'a' })).toBe(true);
        expect(isCodeFileDirty({ opType: 'modify', content: 'a', original: 'a' })).toBe(false);
        expect(isCodeFileDirty({ opType: 'modify', content: 'b', original: 'a' })).toBe(true);
        expect(isCodeFileDirty({ opType: 'modify', content: 'a' })).toBe(true);
    });

    it('prunePathList keeps the same array reference when nothing is removed', () => {
        const paths = ['/a.ts', '/b.ts'];
        expect(prunePathList(paths, paths)).toBe(paths);
        expect(prunePathList(paths, new Set(paths))).toBe(paths);
        expect(prunePathList(paths, ['/a.ts'])).toEqual(['/a.ts']);
        expect(prunePathList(paths, ['/a.ts'])).not.toBe(paths);
    });

    it('applyFileUpdate no-ops on identical redelivery of the same file', () => {
        const file = {
            filePath: '/src/a.ts',
            fileName: 'a.ts',
            content: 'hello',
            opType: 'modify' as const,
            language: 'typescript',
            updatedAt: 1,
            original: 'hi',
            forceOpen: true,
            sessionID: 's1',
        };
        let state = applySessionStart(initialState(), 's1');
        state = applyFileUpdate(state, file);
        const afterFirst = state;
        state = applyFileUpdate(state, { ...file, updatedAt: 99 });
        // Same content/metadata — should reuse state object (no Map churn).
        expect(state).toBe(afterFirst);
        expect(state.files.get('/src/a.ts')?.updatedAt).toBe(1);
    });

    it('applySelectFile ignores unknown paths and no-ops when already active', () => {
        let state = applyFileUpdate(initialState(), {
            filePath: '/src/a.ts', fileName: 'a.ts', content: 'a',
            opType: 'create', language: 'typescript', updatedAt: 1, forceOpen: true,
        });
        state = applyFileUpdate(state, {
            filePath: '/src/b.ts', fileName: 'b.ts', content: 'b',
            opType: 'create', language: 'typescript', updatedAt: 2, forceOpen: true,
        });
        state = applySelectFile(state, '/src/a.ts');
        expect(state.activeFilePath).toBe('/src/a.ts');
        expect(state.mruOrder[0]).toBe('/src/a.ts');

        const same = applySelectFile(state, '/src/a.ts');
        expect(same).toBe(state);

        const unknown = applySelectFile(state, '/src/missing.ts');
        expect(unknown).toBe(state);
        expect(unknown.activeFilePath).toBe('/src/a.ts');

        const cleared = applySelectFile(state, '');
        expect(cleared.activeFilePath).toBe('');
    });

    it('applyMoveFile reorders open tabs without changing active selection', () => {
        let state = initialState();
        for (const path of ['/src/a.ts', '/src/b.ts', '/src/c.ts']) {
            state = applyFileUpdate(state, {
                filePath: path,
                fileName: path.split('/').pop()!,
                content: path,
                opType: 'modify',
                language: 'typescript',
                updatedAt: 1,
                forceOpen: true,
            });
        }
        state = applySelectFile(state, '/src/b.ts');
        state = applyMoveFile(state, '/src/a.ts', 2);
        expect(Array.from(state.files.keys())).toEqual(['/src/b.ts', '/src/c.ts', '/src/a.ts']);
        expect(state.activeFilePath).toBe('/src/b.ts');

        // No-op same index / unknown path
        const before = state;
        expect(applyMoveFile(state, '/src/missing.ts', 0)).toBe(before);
        expect(applyMoveFile(state, '/src/b.ts', 0)).toBe(before);
    });

    it('close/reopen/activate are identity no-ops when already in target state', () => {
        let state = applyFileUpdate(initialState(), {
            filePath: '/src/a.ts', fileName: 'a.ts', content: 'hello',
            opType: 'create', language: 'typescript', updatedAt: 1, forceOpen: true,
        });
        expect(state.active).toBe(true);
        // Already active + not userClosed
        expect(applyReopenPanel(state)).toBe(state);
        expect(applyActivatePassive(state)).toBe(state);

        const closed = applyClosePanel(state);
        expect(closed.active).toBe(false);
        expect(closed.userClosed).toBe(true);
        expect(applyClosePanel(closed)).toBe(closed);
    });

    it('session_start resets files and userClosed', () => {
        let state = initialState();
        state = applyFileUpdate(state, {
            filePath: '/src/a.ts', fileName: 'a.ts', content: 'hello',
            opType: 'create', language: 'typescript', updatedAt: 1,
        });
        state = applyClosePanel(state);
        expect(state.files.size).toBe(1);
        expect(state.userClosed).toBe(true);

        state = applySessionStart(state);
        expect(state.active).toBe(false);
        expect(state.files.size).toBe(0);
        expect(state.activeFilePath).toBe('');
        expect(state.sessionID).toBe('');
        expect(state.sessionActive).toBe(true);
        expect(state.userClosed).toBe(false);
    });

    it('session_start with autoOpenPreview keeps open tabs for pure-coding continuity', () => {
        let state = initialState();
        state = applyFileUpdate(state, {
            sessionID: 'turn-1', filePath: '/src/a.ts', fileName: 'a.ts', content: 'hello',
            opType: 'create', language: 'typescript', updatedAt: 1, forceOpen: true,
        });
        expect(state.files.size).toBe(1);
        expect(state.active).toBe(true);

        state = applySessionStart(state, 'turn-2', true);
        expect(state.active).toBe(true);
        expect(state.sessionID).toBe('turn-2');
        expect(state.sessionActive).toBe(true);
        expect(state.userClosed).toBe(false);
        // Tabs from the previous turn remain visible.
        expect(state.files.size).toBe(1);
        expect(state.files.has('/src/a.ts')).toBe(true);
        expect(state.activeFilePath).toBe('/src/a.ts');
    });

    it('session scoped events ignore stale file updates and stale session_end', () => {
        let state = applySessionStart(initialState(), 'session-a');
        state = applyFileUpdate(state, {
            sessionID: 'session-a', filePath: '/src/a.ts', fileName: 'a.ts', content: 'a',
            opType: 'modify', language: 'typescript', updatedAt: 1, forceOpen: true,
        });
        expect(state.files.size).toBe(1);
        expect(state.activeFilePath).toBe('/src/a.ts');

        const afterStaleFile = applyFileUpdate(state, {
            sessionID: 'session-b', filePath: '/src/b.ts', fileName: 'b.ts', content: 'b',
            opType: 'modify', language: 'typescript', updatedAt: 2,
        });
        expect(afterStaleFile).toBe(state);
        expect(afterStaleFile.files.has('/src/b.ts')).toBe(false);

        const afterUnscopedFile = applyFileUpdate(state, {
            filePath: '/src/unscoped.ts', fileName: 'unscoped.ts', content: 'u',
            opType: 'modify', language: 'typescript', updatedAt: 3, forceOpen: true,
        });
        expect(afterUnscopedFile).toBe(state);
        expect(afterUnscopedFile.files.has('/src/unscoped.ts')).toBe(false);

        const afterStaleEnd = applySessionEnd(state, 'session-b');
        expect(afterStaleEnd.sessionActive).toBe(true);

        const afterUnscopedEnd = applySessionEnd(state);
        expect(afterUnscopedEnd.sessionActive).toBe(true);

        const afterUnscopedStart = applySessionStart(state);
        expect(afterUnscopedStart).toBe(state);
        expect(afterUnscopedStart.files.size).toBe(1);

        const afterMatchingEnd = applySessionEnd(state, 'session-a');
        expect(afterMatchingEnd.sessionActive).toBe(false);
        // Already-ended sessions are a no-op (stable identity).
        expect(applySessionEnd(afterMatchingEnd, 'session-a')).toBe(afterMatchingEnd);

        const afterEndedUnscopedStart = applySessionStart(afterMatchingEnd);
        expect(afterEndedUnscopedStart).not.toBe(afterMatchingEnd);
        expect(afterEndedUnscopedStart.files.size).toBe(0);
        expect(afterEndedUnscopedStart.activeFilePath).toBe('');
        expect(afterEndedUnscopedStart.sessionID).toBe('');
        expect(afterEndedUnscopedStart.sessionActive).toBe(true);
    });

    it('resetSession clears all state', () => {
        let state = initialState();
        state = applyFileUpdate(state, {
            filePath: '/src/a.ts', fileName: 'a.ts', content: 'hello',
            opType: 'create', language: 'typescript', updatedAt: 1,
        });
        state = applyResetSession();
        expect(state).toEqual(initialState());
    });

    it('cloneCodePreviewState copies the files map for restore isolation', () => {
        let snapshot = applyFileUpdate(initialState(), {
            filePath: '/src/a.ts', fileName: 'a.ts', content: 'hello',
            opType: 'create', language: 'typescript', updatedAt: 1,
        });

        const restored = cloneCodePreviewState(snapshot);
        snapshot.files.set('/src/b.ts', {
            filePath: '/src/b.ts', fileName: 'b.ts', content: 'later',
            opType: 'create', language: 'typescript', updatedAt: 2,
        });

        expect(restored.files).not.toBe(snapshot.files);
        expect(restored.files.has('/src/a.ts')).toBe(true);
        expect(restored.files.has('/src/b.ts')).toBe(false);
    });

    it('applyFileUpdate ignores events with missing filePath', () => {
        const state = initialState();
        const result = applyFileUpdate(state, {
            filePath: '', fileName: '', content: 'hello',
            opType: 'create', language: 'typescript', updatedAt: 1,
        });
        expect(result.files.size).toBe(0);
        expect(result.active).toBe(false);
    });
});
