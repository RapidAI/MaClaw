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

/** UI state for the code preview panel. */
export interface CodePreviewUIState {
    active: boolean;              // panel is visible
    files: Map<string, CodeFile>; // filePath -> CodeFile
    activeFilePath: string;       // currently selected file
    sessionID: string;            // latest code session id
    sessionActive: boolean;       // coding session in progress
    userClosed: boolean;          // user manually closed, suppress auto-open
}

function normalizeCodeEventProjectPath(projectPath?: string): string {
    let normalized = (projectPath || "").trim().replace(/\\/g, "/");
    normalized = normalized.replace(/\/+$/, "");
    if (/^[a-z]:\//i.test(normalized) || normalized.startsWith("//")) {
        normalized = normalized.toLowerCase();
    }
    return normalized;
}

function shouldAcceptCodeEventForProject(eventProjectPath?: string, activeTabProjectPath?: string, forceOpen = false): boolean {
    const eventPath = normalizeCodeEventProjectPath(eventProjectPath);
    if (!eventPath) {
        return true;
    }
    const activePath = normalizeCodeEventProjectPath(activeTabProjectPath);
    if (activePath) {
        return eventPath === activePath;
    }
    return forceOpen;
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
        if (file.sessionID && (!state.sessionActive || file.forceOpen)) {
            const nextFiles = new Map<string, CodeFile>();
            nextFiles.set(file.filePath, file);
            return {
                ...state,
                files: nextFiles,
                activeFilePath: file.filePath,
                sessionID: file.sessionID,
                sessionActive: state.sessionActive || file.forceOpen === true,
                active: file.forceOpen || (file.autoOpenPreview && !state.userClosed) ? true : state.active,
                userClosed: file.forceOpen ? false : state.userClosed,
            };
        }
        // Active session should block foreign events
        return state;
    }

    const nextFiles = new Map(state.files);
    nextFiles.set(file.filePath, file);

    const shouldAutoOpen = file.forceOpen || (file.autoOpenPreview && !state.userClosed);
    // Auto-select: always for create/modify, but for read only when panel
    // is first opening (no active file yet). This prevents rapid tab-switching
    // during the SubAgent's initial file exploration phase.
    const shouldAutoSelect = file.opType !== 'read' || !state.activeFilePath;

    return {
        ...state,
        files: nextFiles,
        activeFilePath: shouldAutoSelect ? file.filePath : state.activeFilePath,
        sessionID: file.sessionID || state.sessionID,
        sessionActive: file.forceOpen && file.sessionID ? true : state.sessionActive,
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
export function applySessionStart(state: CodePreviewUIState, sessionID = "", autoOpenPreview = false): CodePreviewUIState {
    if (state.sessionID && !sessionID && state.sessionActive) {
        return state;
    }
    return {
        ...state,
        active: autoOpenPreview,
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

export function cloneCodePreviewState(state: CodePreviewUIState): CodePreviewUIState {
    return {
        ...state,
        files: new Map(state.files),
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
export function useCodePreviewState(activeTabProjectPath?: string) {
    const [state, setState] = useState<CodePreviewUIState>(initialState);

    // Listen for code:file_update
    useEffect(() => {
        const unsub = EventsOn("code:file_update", (data: any) => {
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
    }, [activeTabProjectPath]);

    // Listen for code:session_start
    useEffect(() => {
        const unsub = EventsOn("code:session_start", (data: any) => {
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
    }, [activeTabProjectPath]);

    // Listen for code:session_end
    useEffect(() => {
        const unsub = EventsOn("code:session_end", (data: any) => {
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
        setState(cloneCodePreviewState(snapshot));
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
