import { useCallback, useEffect, useRef, useState } from "react";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";

// ── Data Models ──

/** A single code file received from the backend code:file_update event. */
export interface CodeFile {
    filePath: string;
    fileName: string;
    content: string;
    original?: string;       // undefined for new files
    opType: 'create' | 'modify';
    language: string;
    updatedAt: number;
}

/** UI state for the code preview panel. */
export interface CodePreviewUIState {
    active: boolean;              // panel is visible
    files: Map<string, CodeFile>; // filePath -> CodeFile
    activeFilePath: string;       // currently selected file
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
        sessionActive: false,
        userClosed: false,
    };
}

/**
 * Apply a code:file_update event to the state.
 * Updates the files map, auto-opens the panel if not userClosed,
 * and auto-selects the latest file.
 *
 * When `workflowPreviewActive` is true, the code preview panel will NOT
 * auto-open (mutual exclusion with workflow preview).
 */
export function applyFileUpdate(
    state: CodePreviewUIState,
    file: CodeFile,
    workflowPreviewActive = false,
): CodePreviewUIState {
    // Validate required fields
    if (!file.filePath || file.content === undefined || file.content === null) {
        return state;
    }

    const nextFiles = new Map(state.files);
    nextFiles.set(file.filePath, file);

    // Suppress auto-open when workflow preview is active (mutual exclusion)
    const shouldAutoOpen = !state.userClosed && !workflowPreviewActive;

    return {
        ...state,
        files: nextFiles,
        activeFilePath: file.filePath,
        active: shouldAutoOpen ? true : state.active,
    };
}

/**
 * Apply a workflow:doc_update event — close code preview if it was active.
 * This implements mutual exclusion: workflow preview takes priority.
 */
export function applyWorkflowDocUpdate(state: CodePreviewUIState): CodePreviewUIState {
    if (!state.active) return state;
    return {
        ...state,
        active: false,
    };
}

/**
 * Apply a code:session_start event to the state.
 * Resets files map, sets sessionActive=true, resets userClosed.
 */
export function applySessionStart(state: CodePreviewUIState): CodePreviewUIState {
    return {
        ...state,
        files: new Map(),
        activeFilePath: "",
        sessionActive: true,
        userClosed: false,
    };
}

/**
 * Apply a code:session_end event to the state.
 * Sets sessionActive=false.
 */
export function applySessionEnd(state: CodePreviewUIState): CodePreviewUIState {
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
 *   - workflow:doc_update  — close code preview (mutual exclusion)
 *
 * @param workflowPreviewActive — when true, code:file_update will NOT auto-open the panel
 */
export function useCodePreviewState(workflowPreviewActive = false) {
    const [state, setState] = useState<CodePreviewUIState>(initialState);
    // Keep a ref to userClosed so event callbacks always see the latest value
    const userClosedRef = useRef(false);
    // Keep a ref to workflowPreviewActive for use in event callbacks
    const workflowActiveRef = useRef(workflowPreviewActive);

    // Sync refs with state/props
    useEffect(() => {
        userClosedRef.current = state.userClosed;
    }, [state.userClosed]);

    useEffect(() => {
        workflowActiveRef.current = workflowPreviewActive;
    }, [workflowPreviewActive]);

    // Listen for code:file_update
    useEffect(() => {
        const unsub = EventsOn("code:file_update", (data: any) => {
            if (!data?.file_path || data?.content === undefined || data?.content === null) return;

            const file: CodeFile = {
                filePath: data.file_path,
                fileName: data.file_name || data.file_path.split(/[/\\]/).pop() || data.file_path,
                content: data.content,
                original: data.original || undefined,
                opType: data.op_type === "modify" ? "modify" : "create",
                language: data.language || "plaintext",
                updatedAt: Date.now(),
            };

            setState(prev => applyFileUpdate(prev, file, workflowActiveRef.current));
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("code:file_update");
        };
    }, []);

    // Listen for code:session_start
    useEffect(() => {
        const unsub = EventsOn("code:session_start", () => {
            setState(prev => applySessionStart(prev));
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("code:session_start");
        };
    }, []);

    // Listen for code:session_end
    useEffect(() => {
        const unsub = EventsOn("code:session_end", () => {
            setState(prev => applySessionEnd(prev));
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("code:session_end");
        };
    }, []);

    // Listen for workflow:doc_update — close code preview (mutual exclusion)
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

    const selectFile = useCallback((filePath: string) => {
        setState(prev => applySelectFile(prev, filePath));
    }, []);

    const resetSession = useCallback(() => {
        setState(applyResetSession());
    }, []);

    return {
        state,
        closePanel,
        reopenPanel,
        selectFile,
        resetSession,
    };
}
