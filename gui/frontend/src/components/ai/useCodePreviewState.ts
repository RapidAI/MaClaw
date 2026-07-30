import { useCallback, useEffect, useState } from "react";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";

// ── Data Models ──

/** A single code file received from the backend code:file_update event. */
export interface CodeFile {
    sessionID?: string;
    filePath: string;
    fileName: string;
    absPath?: string;        // absolute path for tooltip/context menu
    content: string;
    original?: string;       // undefined for new files
    opType: 'create' | 'modify' | 'read';
    language: string;
    updatedAt: number;
    forceOpen?: boolean;
    autoOpenPreview?: boolean;
    previewTruncated?: boolean;
}

/**
 * Whether a preview file should show a "changed" (dirty) marker, VS Code-style.
 * - create: always dirty (new file)
 * - modify: dirty when content differs from original (or original missing)
 * - read: never dirty
 */
export function isCodeFileDirty(file: Pick<CodeFile, 'opType' | 'content' | 'original'>): boolean {
    if (file.opType === 'read') return false;
    if (file.opType === 'create') return true;
    if (file.original === undefined) return true;
    return file.original !== file.content;
}

/** UI state for the code preview panel. */
export interface CodePreviewUIState {
    active: boolean;              // panel is visible
    files: Map<string, CodeFile>; // filePath -> CodeFile
    activeFilePath: string;       // currently selected file
    sessionID: string;            // latest code session id
    sessionActive: boolean;       // coding session in progress
    userClosed: boolean;          // user manually closed, suppress auto-open
    /** Pinned file paths (VS Code-style). Kept left / preferred visible. */
    pinnedPaths: string[];
    /** Most-recently-used open order (most recent first) for Ctrl+Tab. */
    mruOrder: string[];
}

/** Move path to the front of the MRU list. */
export function touchMruOrder(mruOrder: string[], filePath: string): string[] {
    if (!filePath) return mruOrder.slice();
    return [filePath, ...mruOrder.filter((p) => p !== filePath)];
}

/** Drop paths that are no longer open. Returns the same array reference when nothing is removed. */
export function prunePathList(paths: string[], openPaths: Iterable<string>): string[] {
    if (paths.length === 0) return paths;
    const open = openPaths instanceof Set ? openPaths : new Set(openPaths);
    for (let i = 0; i < paths.length; i++) {
        if (!open.has(paths[i])) {
            return paths.filter((p) => open.has(p));
        }
    }
    return paths;
}

/**
 * Display order for the tab bar: pinned files first (stable pin order),
 * then unpinned files in Map open order.
 */
export function getDisplayFilePaths(
    files: Map<string, CodeFile>,
    pinnedPaths: string[],
): string[] {
    const open = Array.from(files.keys());
    const openSet = new Set(open);
    const pinned = pinnedPaths.filter((p) => openSet.has(p));
    const pinnedSet = new Set(pinned);
    const unpinned = open.filter((p) => !pinnedSet.has(p));
    return [...pinned, ...unpinned];
}

/**
 * MRU cycle order: most recent first, then any open files missing from MRU.
 */
export function getMruCycleOrder(
    files: Map<string, CodeFile>,
    mruOrder: string[],
): string[] {
    const open = Array.from(files.keys());
    const openSet = new Set(open);
    const mru = mruOrder.filter((p) => openSet.has(p));
    const seen = new Set(mru);
    for (const p of open) {
        if (!seen.has(p)) mru.push(p);
    }
    return mru;
}

function withOpenFileLists(
    state: CodePreviewUIState,
    nextFiles: Map<string, CodeFile>,
    nextActive: string,
): Pick<CodePreviewUIState, 'files' | 'activeFilePath' | 'pinnedPaths' | 'mruOrder'> {
    // Snapshot keys once — Map.keys() is a single-use iterator.
    const openKeys = Array.from(nextFiles.keys());
    let mruOrder = prunePathList(state.mruOrder, openKeys);
    // Only rebuild MRU when the head actually changes (touch always allocates).
    if (nextActive && mruOrder[0] !== nextActive) {
        mruOrder = touchMruOrder(mruOrder, nextActive);
    }
    return {
        files: nextFiles,
        activeFilePath: nextActive,
        pinnedPaths: prunePathList(state.pinnedPaths, openKeys),
        mruOrder,
    };
}

function normalizeCodeEventProjectPath(projectPath?: string): string {
    let normalized = (projectPath || "").trim().replace(/\\/g, "/");
    normalized = normalized.replace(/\/+$/, "");
    if (/^[a-z]:\//i.test(normalized) || normalized.startsWith("//")) {
        normalized = normalized.toLowerCase();
    }
    return normalized;
}

/** Exported for tests. Accepts exact project match or nested worktree paths under the active project. */
export function shouldAcceptCodeEventForProject(eventProjectPath?: string, activeTabProjectPath?: string, forceOpen = false): boolean {
    const eventPath = normalizeCodeEventProjectPath(eventProjectPath);
    if (!eventPath) {
        return true;
    }
    const activePath = normalizeCodeEventProjectPath(activeTabProjectPath);
    if (!activePath) {
        // Local / unbound tab: accept force-open workbench events; ignore others
        // that target a specific project to avoid cross-tab pollution.
        return forceOpen;
    }
    if (eventPath === activePath) {
        return true;
    }
    // Worktree / isolate dirs live under the main project — still show in preview.
    if (eventPath.startsWith(activePath + "/") || activePath.startsWith(eventPath + "/")) {
        return true;
    }
    return false;
}

// ── Pure State Logic Functions (exported for testing) ──

/** Returns a fresh default state. */
export function initialState(): CodePreviewUIState {
    return {
        active: false,
        files: new Map(),
        activeFilePath: "",
        sessionID: "",
        sessionActive: false,
        userClosed: false,
        pinnedPaths: [],
        mruOrder: [],
    };
}

/**
 * Apply a code:file_update event to the state.
 * Updates the files map. Only workflow-authorized events auto-open the panel;
 * ordinary code generation remains available without taking over the UI.
 * and auto-selects the latest file.
 *
 * Workflow and source preview coexist behind tabs, so workflow state does not
 * suppress regular source preview updates.
 */
export function applyFileUpdate(
    state: CodePreviewUIState,
    file: CodeFile,
): CodePreviewUIState {
    // Validate required fields
    if (!file.filePath || file.content === undefined || file.content === null) {
        return state;
    }
    if (state.sessionID && file.sessionID !== state.sessionID) {
        // Session mismatch detected.
        // If the current session is NOT active (i.e., it ended or was restored
        // from a snapshot of a completed session), or if the backend explicitly
        // marks this file as forceOpen, allow the new source preview session to
        // take over by clearing old files and accepting the new file.
        //
        // Note: multi-file batches with forceOpen process one file per event —
        // the first event wipes; later events share the new sessionID and merge.
        // Callers that must not wipe (arm restore, turn-start sticky seed) should
        // emit forceOpen=false so an active session blocks them instead.
        if (file.sessionID && (!state.sessionActive || file.forceOpen)) {
            const nextFiles = new Map<string, CodeFile>();
            nextFiles.set(file.filePath, file);
            return {
                ...state,
                ...withOpenFileLists(state, nextFiles, file.filePath),
                pinnedPaths: [],
                mruOrder: [file.filePath],
                sessionID: file.sessionID,
                sessionActive: state.sessionActive || file.forceOpen === true,
                active: file.forceOpen || (file.autoOpenPreview && !state.userClosed) ? true : state.active,
                userClosed: file.forceOpen ? false : state.userClosed,
            };
        }
        // Active session should block foreign events
        return state;
    }

    const shouldAutoOpen = file.forceOpen || (file.autoOpenPreview && !state.userClosed);
    // Auto-select: always for create/modify, but for read only when panel
    // is first opening (no active file yet). This prevents rapid tab-switching
    // during the SubAgent's initial file exploration phase.
    const shouldAutoSelect = file.opType !== 'read' || !state.activeFilePath;
    const nextActive = shouldAutoSelect ? file.filePath : state.activeFilePath;
    const nextSessionID = file.sessionID || state.sessionID;
    const nextSessionActive = file.forceOpen && file.sessionID ? true : state.sessionActive;
    const nextActiveFlag = shouldAutoOpen ? true : state.active;
    const nextUserClosed = file.forceOpen ? false : state.userClosed;

    // Skip no-op updates (identical payload / redelivery) to avoid Map churn during streaming.
    const existing = state.files.get(file.filePath);
    if (
        existing
        && existing.content === file.content
        && existing.original === file.original
        && existing.opType === file.opType
        && existing.language === file.language
        && existing.fileName === file.fileName
        && existing.absPath === file.absPath
        && existing.previewTruncated === file.previewTruncated
        && existing.sessionID === file.sessionID
        && state.activeFilePath === nextActive
        && state.active === nextActiveFlag
        && state.userClosed === nextUserClosed
        && state.sessionID === nextSessionID
        && state.sessionActive === nextSessionActive
        && (!shouldAutoSelect || state.mruOrder[0] === file.filePath)
    ) {
        return state;
    }

    const nextFiles = new Map(state.files);
    nextFiles.set(file.filePath, file);

    return {
        ...state,
        ...withOpenFileLists(state, nextFiles, nextActive),
        sessionID: nextSessionID,
        sessionActive: nextSessionActive,
        active: nextActiveFlag,
        userClosed: nextUserClosed,
    };
}

/**
 * Apply a workflow:doc_update event.
 * Workflow and source preview now coexist behind tabs, so keep code state.
 */
export function applyWorkflowDocUpdate(state: CodePreviewUIState): CodePreviewUIState {
    return state;
}

/**
 * Apply a code:session_start event to the state.
 * Sets sessionActive=true and resets userClosed.
 *
 * autoOpenPreview=false (default / historical): clear files and close the panel
 * until the first forceOpen file update.
 *
 * autoOpenPreview=true (CodingSubAgent / pure-coding): keep existing open tabs
 * and panel visibility across multi-turn boundaries so bash-only turns do not
 * blank the right-hand preview.
 */
export function applySessionStart(state: CodePreviewUIState, sessionID = "", autoOpenPreview = false): CodePreviewUIState {
    if (state.sessionID && !sessionID && state.sessionActive) {
        return state;
    }
    if (autoOpenPreview) {
        return {
            ...state,
            active: true,
            sessionID,
            sessionActive: true,
            userClosed: false,
            // Keep files / activeFilePath / pinnedPaths / mruOrder for continuity.
        };
    }
    return {
        ...state,
        active: false,
        files: new Map(),
        activeFilePath: "",
        sessionID,
        sessionActive: true,
        userClosed: false,
        pinnedPaths: [],
        mruOrder: [],
    };
}

/**
 * Apply a code:session_end event to the state.
 * Sets sessionActive=false.
 */
export function applySessionEnd(state: CodePreviewUIState, sessionID = ""): CodePreviewUIState {
    if (state.sessionID && state.sessionID !== sessionID) {
        return state;
    }
    if (!state.sessionActive) {
        return state;
    }
    return {
        ...state,
        sessionActive: false,
    };
}

/**
 * Close the panel. Sets active=false, userClosed=true.
 */
export function applyClosePanel(state: CodePreviewUIState): CodePreviewUIState {
    if (!state.active && state.userClosed) return state;
    return {
        ...state,
        active: false,
        userClosed: true,
    };
}

/**
 * Reopen the panel. Sets active=true, userClosed=false.
 */
export function applyReopenPanel(state: CodePreviewUIState): CodePreviewUIState {
    if (state.active && !state.userClosed) return state;
    return {
        ...state,
        active: true,
        userClosed: false,
    };
}

/**
 * Activate the panel without clearing userClosed.
 * Used when another UI action (e.g. opening the workflow panel) wants to
 * make the code preview visible as a side-effect, but should not alter
 * the user's prior close decision for auto-open purposes.
 */
export function applyActivatePassive(state: CodePreviewUIState): CodePreviewUIState {
    if (state.active) return state;
    return {
        ...state,
        active: true,
    };
}

/**
 * Select a file by path. Sets activeFilePath and updates MRU.
 * Empty path clears the selection. Unknown paths are ignored.
 */
export function applySelectFile(
    state: CodePreviewUIState,
    filePath: string,
): CodePreviewUIState {
    if (!filePath) {
        if (state.activeFilePath === '') return state;
        return {
            ...state,
            activeFilePath: '',
        };
    }
    if (!state.files.has(filePath)) {
        return state;
    }
    // No-op when already active and already MRU head (avoids needless re-renders).
    if (state.activeFilePath === filePath && state.mruOrder[0] === filePath) {
        return state;
    }
    return {
        ...state,
        activeFilePath: filePath,
        mruOrder: touchMruOrder(state.mruOrder, filePath),
    };
}

/** Open a file deliberately chosen from the working-directory explorer. */
export function applyOpenWorkspaceFile(
    state: CodePreviewUIState,
    file: CodeFile,
): CodePreviewUIState {
    if (!file.filePath || file.content === undefined || file.content === null) return state;
    const files = new Map(state.files);
    files.set(file.filePath, file);
    return {
        ...state,
        ...withOpenFileLists(state, files, file.filePath),
        active: true,
        userClosed: false,
    };
}

/**
 * Pin or unpin a file tab. Pinned tabs sort to the left and stay preferred-visible.
 */
export function applySetFilePinned(
    state: CodePreviewUIState,
    filePath: string,
    pinned: boolean,
): CodePreviewUIState {
    if (!state.files.has(filePath)) {
        return state;
    }
    const isPinned = state.pinnedPaths.includes(filePath);
    if (pinned === isPinned) {
        return state;
    }
    const pinnedPaths = pinned
        ? [...state.pinnedPaths.filter((p) => p !== filePath), filePath]
        : state.pinnedPaths.filter((p) => p !== filePath);
    return {
        ...state,
        pinnedPaths,
    };
}

/** Toggle pin for a file tab. */
export function applyToggleFilePinned(
    state: CodePreviewUIState,
    filePath: string,
): CodePreviewUIState {
    return applySetFilePinned(state, filePath, !state.pinnedPaths.includes(filePath));
}

/**
 * Close a single file tab (VS Code-style).
 * Removes the file from the map. If the closed file was active, selects a
 * neighbor (prefer previous) or clears activeFilePath when none remain.
 * Does not close the whole panel; empty state is handled by the UI.
 */
export function applyCloseFile(
    state: CodePreviewUIState,
    filePath: string,
): CodePreviewUIState {
    if (!state.files.has(filePath)) {
        return state;
    }
    const paths = getDisplayFilePaths(state.files, state.pinnedPaths);
    const index = paths.indexOf(filePath);
    const nextFiles = new Map(state.files);
    nextFiles.delete(filePath);

    let nextActive = state.activeFilePath;
    if (state.activeFilePath === filePath) {
        if (nextFiles.size === 0) {
            nextActive = '';
        } else {
            // Prefer previous neighbor in display order, then next.
            const preferred = paths[index - 1] ?? paths[index + 1] ?? paths.find((p) => p !== filePath);
            nextActive = preferred && nextFiles.has(preferred)
                ? preferred
                : (Array.from(nextFiles.keys())[0] ?? '');
        }
    }

    return {
        ...state,
        ...withOpenFileLists(state, nextFiles, nextActive),
    };
}

/**
 * Close every open file except `keepPath` (VS Code "Close Others").
 * Also keeps other pinned tabs (common editor convenience).
 */
export function applyCloseOtherFiles(
    state: CodePreviewUIState,
    keepPath: string,
): CodePreviewUIState {
    if (!state.files.has(keepPath)) {
        return state;
    }
    if (state.files.size <= 1) {
        return state;
    }
    const keep = new Set<string>([keepPath, ...state.pinnedPaths.filter((p) => state.files.has(p))]);
    const nextFiles = new Map<string, CodeFile>();
    for (const [path, file] of state.files) {
        if (keep.has(path)) nextFiles.set(path, file);
    }
    if (nextFiles.size === state.files.size) {
        return state;
    }
    const nextActive = nextFiles.has(state.activeFilePath) ? state.activeFilePath : keepPath;
    return {
        ...state,
        ...withOpenFileLists(state, nextFiles, nextActive),
    };
}

/**
 * Close all tabs to the right of `fromPath` (VS Code "Close to the Right").
 * Order follows display order (pinned first).
 * Pinned tabs to the right are preserved.
 */
export function applyCloseFilesToTheRight(
    state: CodePreviewUIState,
    fromPath: string,
): CodePreviewUIState {
    const paths = getDisplayFilePaths(state.files, state.pinnedPaths);
    const index = paths.indexOf(fromPath);
    if (index < 0 || index >= paths.length - 1) {
        return state;
    }
    const pinnedSet = new Set(state.pinnedPaths);
    const nextFiles = new Map(state.files);
    for (let i = index + 1; i < paths.length; i++) {
        if (!pinnedSet.has(paths[i])) {
            nextFiles.delete(paths[i]);
        }
    }
    if (nextFiles.size === state.files.size) {
        return state;
    }
    let nextActive = state.activeFilePath;
    if (nextActive && !nextFiles.has(nextActive)) {
        nextActive = fromPath;
    }
    return {
        ...state,
        ...withOpenFileLists(state, nextFiles, nextActive),
    };
}

/**
 * Close every open file tab (VS Code "Close All").
 * Clears the files map without resetting session metadata.
 * Pinned tabs are kept open.
 */
export function applyCloseAllFiles(state: CodePreviewUIState): CodePreviewUIState {
    if (state.files.size === 0) {
        return state;
    }
    const pinnedSet = new Set(state.pinnedPaths.filter((p) => state.files.has(p)));
    if (pinnedSet.size === 0) {
        return {
            ...state,
            files: new Map(),
            activeFilePath: '',
            pinnedPaths: [],
            mruOrder: [],
        };
    }
    // Everything is already pinned — nothing to close.
    if (pinnedSet.size === state.files.size) {
        return state;
    }
    const nextFiles = new Map<string, CodeFile>();
    for (const [path, file] of state.files) {
        if (pinnedSet.has(path)) nextFiles.set(path, file);
    }
    const nextActive = nextFiles.has(state.activeFilePath)
        ? state.activeFilePath
        : (Array.from(nextFiles.keys())[0] ?? '');
    return {
        ...state,
        ...withOpenFileLists(state, nextFiles, nextActive),
    };
}

/**
 * Move an open file tab to a new index in display order (drag-reorder).
 * Index is clamped to [0, files.size - 1]. No-op when path is missing or index unchanged.
 * Pin membership is preserved; pinned tabs remain sorted left after the move within groups.
 */
export function applyMoveFile(
    state: CodePreviewUIState,
    fromPath: string,
    toIndex: number,
): CodePreviewUIState {
    const paths = getDisplayFilePaths(state.files, state.pinnedPaths);
    const fromIndex = paths.indexOf(fromPath);
    if (fromIndex < 0 || paths.length <= 1) {
        return state;
    }
    if (!Number.isFinite(toIndex)) {
        return state;
    }
    const clamped = Math.max(0, Math.min(paths.length - 1, Math.floor(toIndex)));
    if (clamped === fromIndex) {
        return state;
    }
    const nextPaths = paths.slice();
    nextPaths.splice(fromIndex, 1);
    nextPaths.splice(clamped, 0, fromPath);

    const pinnedSet = new Set(state.pinnedPaths.filter((p) => state.files.has(p)));
    // Rebuild pin list as pinned paths in the new display order.
    const pinnedPaths = nextPaths.filter((p) => pinnedSet.has(p));

    // Preserve Map open-order as the new display order (pinned still sort left via getDisplayFilePaths).
    const nextFiles = new Map<string, CodeFile>();
    for (const path of nextPaths) {
        const file = state.files.get(path);
        if (file) nextFiles.set(path, file);
    }
    return {
        ...state,
        files: nextFiles,
        pinnedPaths,
    };
}

/**
 * Reset the entire session. Clears all state back to initial.
 */
export function applyResetSession(): CodePreviewUIState {
    return initialState();
}

export function cloneCodePreviewState(state: CodePreviewUIState): CodePreviewUIState {
    return {
        ...state,
        files: new Map(state.files ?? []),
        pinnedPaths: Array.isArray(state.pinnedPaths) ? state.pinnedPaths.slice() : [],
        mruOrder: Array.isArray(state.mruOrder) ? state.mruOrder.slice() : [],
    };
}

// ── React Hook ──

/**
 * useCodePreviewState manages the code preview panel state.
 *
 * Listens to Wails events:
 *   - code:file_update    — update files map, auto-open, auto-select
 *   - code:session_start  — reset files, activate session
 *   - code:session_end    — deactivate session
 *   - workflow:doc_update  — preserve code preview for tabbed switching
 *
 * @param activeTabProjectPath - The project_path of the currently active tab.
 *   Used to route code events: if an event carries a project_path that doesn't
 *   match the active tab's project path, the update is skipped. Local tab state
 *   only accepts events without project_path for backward compatibility.
 */
export function useCodePreviewState(activeTabProjectPath?: string, previewEnabled = true) {
    const [state, setState] = useState<CodePreviewUIState>(initialState);

    // Listen for code:file_update
    useEffect(() => {
        const unsub = EventsOn("code:file_update", (data: any) => {
            if (!previewEnabled) return;
            if (!data?.file_path || data?.content === undefined || data?.content === null) return;

            const eventProjectPath: string | undefined = data.project_path;
            const forceOpen = data.force_open === true;
            if (!shouldAcceptCodeEventForProject(eventProjectPath, activeTabProjectPath, forceOpen)) {
                return;
            }

            const opType: CodeFile['opType'] = data.op_type === "modify" ? "modify" : data.op_type === "read" ? "read" : "create";
            const original = opType === "modify" && data.original_missing !== true && typeof data.original === "string" ? data.original : undefined;
            const file: CodeFile = {
                sessionID: data.session_id || "",
                filePath: data.file_path,
                fileName: data.file_name || data.file_path.split(/[/\\]/).pop() || data.file_path,
                absPath: data.abs_path || undefined,
                content: data.content,
                original,
                opType,
                language: data.language || "plaintext",
                updatedAt: Date.now(),
                forceOpen,
                autoOpenPreview: data.auto_open_preview === true,
                previewTruncated: data.preview_truncated === true,
            };

            setState(prev => applyFileUpdate(prev, file));
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("code:file_update");
        };
    }, [activeTabProjectPath, previewEnabled]);

    // Listen for code:session_start
    useEffect(() => {
        const unsub = EventsOn("code:session_start", (data: any) => {
            if (!previewEnabled) return;
            const eventProjectPath: string | undefined = data?.project_path;
            if (!shouldAcceptCodeEventForProject(eventProjectPath, activeTabProjectPath)) {
                return;
            }
            setState(prev => applySessionStart(prev, data?.session_id || "", data?.auto_open_preview === true));
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("code:session_start");
        };
    }, [activeTabProjectPath, previewEnabled]);

    // Listen for code:session_end
    useEffect(() => {
        const unsub = EventsOn("code:session_end", (data: any) => {
            if (!previewEnabled) return;
            const eventProjectPath: string | undefined = data?.project_path;
            const sessionID = data?.session_id || "";
            setState(prev => {
                if (!shouldAcceptCodeEventForProject(eventProjectPath, activeTabProjectPath) && prev.sessionID !== sessionID) {
                    return prev;
                }
                return applySessionEnd(prev, sessionID);
            });
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("code:session_end");
        };
    }, [activeTabProjectPath, previewEnabled]);

    // Listen for workflow:doc_update — keep source preview available for tabs.
    useEffect(() => {
        const unsub = EventsOn("workflow:doc_update", () => {
            setState(prev => applyWorkflowDocUpdate(prev));
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("workflow:doc_update");
        };
    }, []);

    const closePanel = useCallback(() => {
        setState(prev => applyClosePanel(prev));
    }, []);

    const reopenPanel = useCallback(() => {
        setState(prev => applyReopenPanel(prev));
    }, []);

    const activatePassive = useCallback(() => {
        setState(prev => applyActivatePassive(prev));
    }, []);

    const selectFile = useCallback((filePath: string) => {
        setState(prev => applySelectFile(prev, filePath));
    }, []);

    const openWorkspaceFile = useCallback((file: CodeFile) => {
        setState(prev => applyOpenWorkspaceFile(prev, file));
    }, []);

    const closeFile = useCallback((filePath: string) => {
        setState(prev => applyCloseFile(prev, filePath));
    }, []);

    const closeOtherFiles = useCallback((keepPath: string) => {
        setState(prev => applyCloseOtherFiles(prev, keepPath));
    }, []);

    const closeFilesToTheRight = useCallback((fromPath: string) => {
        setState(prev => applyCloseFilesToTheRight(prev, fromPath));
    }, []);

    const closeAllFiles = useCallback(() => {
        setState(prev => applyCloseAllFiles(prev));
    }, []);

    const moveFile = useCallback((fromPath: string, toIndex: number) => {
        setState(prev => applyMoveFile(prev, fromPath, toIndex));
    }, []);

    const setFilePinned = useCallback((filePath: string, pinned: boolean) => {
        setState(prev => applySetFilePinned(prev, filePath, pinned));
    }, []);

    const toggleFilePinned = useCallback((filePath: string) => {
        setState(prev => applyToggleFilePinned(prev, filePath));
    }, []);

    const resetSession = useCallback(() => {
        setState(applyResetSession());
    }, []);

    /** Overwrites the entire code preview state from a saved snapshot. */
    const restoreState = useCallback((snapshot: CodePreviewUIState) => {
        setState(cloneCodePreviewState({
            ...initialState(),
            ...snapshot,
            files: snapshot.files ?? new Map(),
            pinnedPaths: snapshot.pinnedPaths ?? [],
            mruOrder: snapshot.mruOrder ?? [],
        }));
    }, []);

    return {
        state,
        closePanel,
        reopenPanel,
        activatePassive,
        selectFile,
        openWorkspaceFile,
        closeFile,
        closeOtherFiles,
        closeFilesToTheRight,
        closeAllFiles,
        moveFile,
        setFilePinned,
        toggleFilePinned,
        resetSession,
        restoreState,
    };
}
