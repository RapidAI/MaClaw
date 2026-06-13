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
}

/** UI state for the code preview panel. */
export interface CodePreviewUIState {
    active: boolean;              // panel is visible
    files: Map<string, CodeFile>; // filePath -> CodeFile
    activeFilePath: string;       // currently selected file
    sessionID: string;            // latest code session id
    sessionActive: boolean;       // coding session in progress
    userClosed: boolean;          // user manually closed, suppress auto-open
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
    };
}

/**
 * Apply a code:file_update event to the state.
 * Updates the files map, auto-opens the panel if not userClosed,
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
        // from a snapshot of a completed session), allow a new session to take
        // over by clearing old files and accepting the new file.
        if (file.sessionID && !state.sessionActive) {
            // New session starting — clear stale state and accept
            const nextFiles = new Map<string, CodeFile>();
            nextFiles.set(file.filePath, file);
            return {
                ...state,
                files: nextFiles,
                activeFilePath: file.filePath,
                sessionID: file.sessionID,
                active: !state.userClosed || file.forceOpen ? true : state.active,
                userClosed: file.forceOpen ? false : state.userClosed,
            };
        }
        // Active session should block foreign events
        return state;
    }

    const nextFiles = new Map(state.files);
    nextFiles.set(file.filePath, file);

    const shouldAutoOpen = file.forceOpen || !state.userClosed;
    // Auto-select: always for create/modify, but for read only when panel
    // is first opening (no active file yet). This prevents rapid tab-switching
    // during the SubAgent's initial file exploration phase.
    const shouldAutoSelect = file.opType !== 'read' || !state.activeFilePath;

    return {
        ...state,
        files: nextFiles,
        activeFilePath: shouldAutoSelect ? file.filePath : state.activeFilePath,
        sessionID: file.sessionID || state.sessionID,
        active: shouldAutoOpen ? true : state.active,
        userClosed: file.forceOpen ? false : state.userClosed,
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
 * Resets files map, closes the panel until the first file update,
 * sets sessionActive=true, resets userClosed.
 */
export function applySessionStart(state: CodePreviewUIState, sessionID = ""): CodePreviewUIState {
    if (state.sessionID && !sessionID) {
        return state;
    }
    return {
        ...state,
        active: false,
        files: new Map(),
        activeFilePath: "",
        sessionID,
        sessionActive: true,
        userClosed: false,
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
    return {
        ...state,
        sessionActive: false,
    };
}

/**
 * Close the panel. Sets active=false, userClosed=true.
 */
export function applyClosePanel(state: CodePreviewUIState): CodePreviewUIState {
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
    return {
        ...state,
        active: true,
    };
}

/**
 * Select a file by path. Sets activeFilePath.
 */
export function applySelectFile(
    state: CodePreviewUIState,
    filePath: string,
): CodePreviewUIState {
    return {
        ...state,
        activeFilePath: filePath,
    };
}

/**
 * Reset the entire session. Clears all state back to initial.
 */
export function applyResetSession(): CodePreviewUIState {
    return initialState();
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
 *   Used to route code:file_update events: if the event carries a project_path
 *   that doesn't match the active tab, the update is skipped (it belongs to
 *   another tab and will be handled via per-tab save/restore). Events without
 *   project_path are applied to the current state (backward compatible).
 */
export function useCodePreviewState(activeTabProjectPath?: string) {
    const [state, setState] = useState<CodePreviewUIState>(initialState);

    // Listen for code:file_update
    useEffect(() => {
        const unsub = EventsOn("code:file_update", (data: any) => {
            if (!data?.file_path || data?.content === undefined || data?.content === null) return;

            // Route by project_path: if the event carries a project_path that
            // doesn't match the active tab, skip the update. The per-tab
            // save/restore (task 3.6) handles cross-tab state.
            // If project_path is absent, apply to current state (backward compatible).
            const eventProjectPath: string | undefined = data.project_path;
            if (eventProjectPath && activeTabProjectPath && eventProjectPath !== activeTabProjectPath) {
                return;
            }

            const file: CodeFile = {
                sessionID: data.session_id || "",
                filePath: data.file_path,
                fileName: data.file_name || data.file_path.split(/[/\\]/).pop() || data.file_path,
                absPath: data.abs_path || undefined,
                content: data.content,
                original: data.original || undefined,
                opType: data.op_type === "modify" ? "modify" : data.op_type === "read" ? "read" : "create",
                language: data.language || "plaintext",
                updatedAt: Date.now(),
                forceOpen: data.force_open === true,
            };

            setState(prev => applyFileUpdate(prev, file));
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("code:file_update");
        };
    }, [activeTabProjectPath]);

    // Listen for code:session_start
    useEffect(() => {
        const unsub = EventsOn("code:session_start", (data: any) => {
            const eventProjectPath: string | undefined = data?.project_path;
            if (eventProjectPath && activeTabProjectPath && eventProjectPath !== activeTabProjectPath) {
                return;
            }
            setState(prev => applySessionStart(prev, data?.session_id || ""));
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("code:session_start");
        };
    }, [activeTabProjectPath]);

    // Listen for code:session_end
    useEffect(() => {
        const unsub = EventsOn("code:session_end", (data: any) => {
            const eventProjectPath: string | undefined = data?.project_path;
            if (eventProjectPath && activeTabProjectPath && eventProjectPath !== activeTabProjectPath) {
                return;
            }
            setState(prev => applySessionEnd(prev, data?.session_id || ""));
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("code:session_end");
        };
    }, [activeTabProjectPath]);

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

    const resetSession = useCallback(() => {
        setState(applyResetSession());
    }, []);

    /** Overwrites the entire code preview state from a saved snapshot. */
    const restoreState = useCallback((snapshot: CodePreviewUIState) => {
        setState(snapshot);
    }, []);

    return {
        state,
        closePanel,
        reopenPanel,
        activatePassive,
        selectFile,
        resetSession,
        restoreState,
    };
}
