import { useCallback, useEffect, useRef, useState } from "react";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import { collectWorkflowPhases, normalizeWorkflowPhaseID, PhaseInfo, workflowPhaseExpectsDocument } from "./workflowPhase";
import { isWorkflowActive } from "./workflowStatus";

export type { PhaseInfo } from "./workflowPhase";

/** Quality gate check item from the backend. */
export interface GateCheckItem {
    description: string;
    passed: boolean;
    note?: string;
}

/** Quality gate result for a phase. */
export interface QualityGateResult {
    phase_id: string;
    passed: boolean;
    items: GateCheckItem[];
    checked_at: string;
}

/** UI state for the workflow split-pane document preview. */
export interface WorkflowUIState {
    active: boolean;
    splitMode: boolean;
    splitRatio: number;
    workflowType: string;
    currentPhaseID: string;
    latestDocumentPhaseID: string;
    phaseDocuments: Map<string, string>;
    gateResults: Map<string, QualityGateResult>;
    phases: PhaseInfo[];
    suggestMaximize: boolean;
    suggestMaximizeType: string;
    transientText: string;
    workingDir: string;
    workflowID: string;
    docUpdatePhaseIDs: Set<string>;
}

const DEFAULT_SPLIT_RATIO = 0.42;

function normalizeWorkflowDocumentContent(content: unknown): string {
    return typeof content === "string" ? content.trim() : "";
}

export function collectWorkflowPhaseDocuments(outputs: unknown): Map<string, string> {
    const docs = new Map<string, string>();
    if (!outputs || typeof outputs !== "object") return docs;
    for (const [rawPhaseID, rawContent] of Object.entries(outputs as Record<string, unknown>)) {
        const phaseID = normalizeWorkflowPhaseID(rawPhaseID);
        const content = normalizeWorkflowDocumentContent(rawContent);
        if (phaseID && content) docs.set(phaseID, content);
    }
    return docs;
}

function mergeWorkflowPhaseDocuments(
    prev: Map<string, string>,
    incoming: Map<string, string>,
    docUpdatePhaseIDs: Set<string>,
): Map<string, string> {
    if (incoming.size === 0) return prev;
    const next = new Map(prev);
    for (const [phaseID, content] of incoming) {
        if (docUpdatePhaseIDs.has(phaseID)) continue;
        next.set(phaseID, content);
    }
    return next;
}

function collectWorkflowGateResults(results: unknown): Map<string, QualityGateResult> {
    const gates = new Map<string, QualityGateResult>();
    if (!results || typeof results !== "object") return gates;
    for (const [rawPhaseID, rawResult] of Object.entries(results as Record<string, unknown>)) {
        const phaseID = normalizeWorkflowPhaseID(rawPhaseID);
        if (!phaseID || !rawResult || typeof rawResult !== "object") continue;
        const result = rawResult as Partial<QualityGateResult>;
        gates.set(phaseID, {
            ...(result as QualityGateResult),
            phase_id: phaseID,
            passed: result.passed === true,
            items: Array.isArray(result.items) ? result.items : [],
            checked_at: typeof result.checked_at === "string" ? result.checked_at : "",
        });
    }
    return gates;
}

function resolveWorkflowInstanceKey(state: any): string {
    const id = typeof state?.id === "string" ? state.id.trim() : "";
    if (id) return id;
    const createdAt = typeof state?.created_at === "string" ? state.created_at.trim() : "";
    const type = typeof state?.type === "string" ? state.type.trim() : "";
    return createdAt && type ? `${type}:${createdAt}` : "";
}

/**
 * Determines whether a workflow event should be accepted by this hook instance.
 * Uses workflow_id as the primary isolation key (precise, per-instance).
 * Falls back to project_path comparison when workflow_id is unavailable.
 *
 * @param eventWorkflowID - The workflow_id carried by the event (may be empty for legacy events)
 * @param eventProjectPath - The project_path carried by the event
 * @param currentWorkflowID - The currently tracked workflow instance ID (workflowIDRef.current)
 * @param currentTabPath - The active tab's project path (activeTabProjectPathRef.current)
 * @param strict - When true, reject events from different workflow IDs (for doc_update/gate_result).
 *   When false, accept events from new workflows (for phase_update which may signal a new workflow start).
 * @returns true if the event should be accepted, false if it belongs to another tab/workflow
 */
function shouldAcceptWorkflowEvent(
    eventWorkflowID: string,
    eventProjectPath: string,
    currentWorkflowID: string,
    currentTabPath: string,
    strict: boolean = true,
): boolean {
    // Primary isolation: workflow_id match.
    // If both sides have a workflow_id, use it for precise per-instance routing.
    if (eventWorkflowID && currentWorkflowID) {
        if (eventWorkflowID === currentWorkflowID) return true;
        // Different workflow IDs:
        // - strict mode (doc_update/gate_result): reject — content belongs to another instance
        // - non-strict mode (phase_update): accept — may be a new workflow starting on this tab
        if (strict) return false;
        // In non-strict mode, fall through to project_path check to verify
        // the new workflow belongs to this tab's project scope.
    }

    // Fallback: project_path comparison (legacy behavior for events without workflow_id,
    // or non-strict mode validating a new workflow's project scope).
    // Only filter when the tab's path is a real filesystem path (contains / or \).
    // The LOCAL sentinel ("__maclaw_local_coding_preview__") accepts all events
    // when there's no workflow_id to compare — this is the backward-compat case.
    const isRealPath = currentTabPath.length > 0 && (currentTabPath.includes("/") || currentTabPath.includes("\\"));
    if (eventProjectPath && isRealPath) {
        // Normalize slashes for comparison: backend uses OS-native separators
        // (backslash on Windows) while frontend normalizes to forward slashes.
        const normEvent = eventProjectPath.replace(/\\/g, "/").toLowerCase();
        const normCurrent = currentTabPath.replace(/\\/g, "/").toLowerCase();
        if (normEvent !== normCurrent) {
            return false;
        }
    }

    return true;
}

/**
 * useWorkflowState manages the workflow UI state for the split-pane
 * document preview in AIAssistantPanel.
 *
 * Listens to Wails events:
 *   - workflow:phase_update  — phase changes
 *   - workflow:doc_update    — document content updates
 *   - workflow:gate_result   — quality gate results
 *
 * @param activeTabProjectPath - The project path of the currently active tab.
 *   Used for event routing: if an event carries a `project_path` that differs
 *   from the active tab's path, the update is skipped (it belongs to another
 *   tab and will be handled via the tab switch save/restore mechanism).
 *   If empty/undefined, all events are applied (backward compatible fallback).
 */
export function useWorkflowState(activeTabProjectPath?: string) {
    const [active, setActive] = useState(false);
    const [splitMode, setSplitMode] = useState(false);
    const [splitRatio, setSplitRatioState] = useState(DEFAULT_SPLIT_RATIO);
    const [workflowType, setWorkflowType] = useState("");
    const [currentPhaseID, setCurrentPhaseID] = useState("");
    const [latestDocumentPhaseID, setLatestDocumentPhaseID] = useState("");
    const [phaseDocuments, setPhaseDocuments] = useState<Map<string, string>>(new Map());
    const [gateResults, setGateResults] = useState<Map<string, QualityGateResult>>(new Map());
    const [phases, setPhases] = useState<PhaseInfo[]>([]);
    const [suggestMaximize, setSuggestMaximize] = useState(false);
    const [suggestMaximizeType, setSuggestMaximizeType] = useState("");
    const [transientText, setTransientText] = useState("");
    const [workingDir, setWorkingDir] = useState("");
    const userClosedRef = useRef(false);
    const docUpdatePhaseIDsRef = useRef<Set<string>>(new Set());
    const workflowIDRef = useRef("");
    const workflowTypeRef = useRef("");
    const workflowActiveRef = useRef(false);
    const pendingWorkingDirRef = useRef("");
    const transientTextTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const activeTabProjectPathRef = useRef(activeTabProjectPath || "");
    activeTabProjectPathRef.current = activeTabProjectPath || "";

    const setWorkflowSplitMode = useCallback((next: boolean | ((current: boolean) => boolean)) => {
        setSplitMode(current => {
            const resolved = typeof next === "function" ? next(current) : next;
            return resolved;
        });
    }, []);

    // Listen for phase updates
    useEffect(() => {
        const unsub = EventsOn("workflow:phase_update", (state: any) => {
            if (!state) {
                // Workflow fully reset — clear everything.
                setActive(false);
                setWorkflowSplitMode(false);
                setWorkflowType("");
                setCurrentPhaseID("");
                setLatestDocumentPhaseID("");
                setPhaseDocuments(new Map());
                setGateResults(new Map());
                setPhases([]);
                setSuggestMaximize(false);
                setSuggestMaximizeType("");
                setTransientText("");
                setWorkingDir("");
                userClosedRef.current = false;
                docUpdatePhaseIDsRef.current = new Set();
                workflowIDRef.current = "";
                workflowTypeRef.current = "";
                workflowActiveRef.current = false;
                pendingWorkingDirRef.current = "";
                if (transientTextTimerRef.current) {
                    clearTimeout(transientTextTimerRef.current);
                    transientTextTimerRef.current = null;
                }
                return;
            }

            // Workflow instance isolation: use workflow_id as primary routing key
            // for precise per-instance filtering. Falls back to project_path when
            // workflow_id is unavailable (legacy V1 events without id field).
            // Non-strict mode: accept events from new workflow instances (they may
            // signal a new workflow starting on this tab — the code below detects
            // the ID change and resets state accordingly).
            const eventProjectPath = typeof state.project_path === "string" ? state.project_path.trim() : "";
            const eventWorkflowID = resolveWorkflowInstanceKey(state);
            if (!shouldAcceptWorkflowEvent(eventWorkflowID, eventProjectPath, workflowIDRef.current, activeTabProjectPathRef.current, false)) {
                return;
            }
            const workflowID = eventWorkflowID;
            const incomingWorkflowType = typeof state.type === "string" ? state.type : "";
            const wasActive = workflowActiveRef.current;
            let appliedPendingWorkingDir = false;
            if (workflowID && workflowID !== workflowIDRef.current) {
                workflowIDRef.current = workflowID;
                docUpdatePhaseIDsRef.current = new Set();
                setLatestDocumentPhaseID("");
                setPhaseDocuments(new Map());
                setGateResults(new Map());
                setPhases([]);
                setWorkingDir(pendingWorkingDirRef.current);
                pendingWorkingDirRef.current = "";
                appliedPendingWorkingDir = true;
                userClosedRef.current = false;
            } else if (!workflowID && isWorkflowActive(state.status) && workflowTypeRef.current && incomingWorkflowType && incomingWorkflowType !== workflowTypeRef.current) {
                docUpdatePhaseIDsRef.current = new Set();
                setLatestDocumentPhaseID("");
                setPhaseDocuments(new Map());
                setGateResults(new Map());
                setPhases([]);
                setWorkingDir(pendingWorkingDirRef.current);
                pendingWorkingDirRef.current = "";
                appliedPendingWorkingDir = true;
                userClosedRef.current = false;
            }
            const isActive = isWorkflowActive(state.status);
            setActive(isActive);
            workflowActiveRef.current = isActive;
            if (isActive && !wasActive && !appliedPendingWorkingDir && pendingWorkingDirRef.current) {
                setWorkingDir(pendingWorkingDirRef.current);
                pendingWorkingDirRef.current = "";
            }
            setWorkflowType(incomingWorkflowType);
            if (incomingWorkflowType) workflowTypeRef.current = incomingWorkflowType;
            const incomingPhases = collectWorkflowPhases(state.phases);
            setPhases(incomingPhases);
            const currentPhase = normalizeWorkflowPhaseID(state.current_phase);
            setCurrentPhaseID(currentPhase);
            const outputDocuments = collectWorkflowPhaseDocuments(state.phase_outputs);
            setPhaseDocuments(prev => mergeWorkflowPhaseDocuments(prev, outputDocuments, docUpdatePhaseIDsRef.current));
            if (currentPhase && outputDocuments.has(currentPhase)) {
                setLatestDocumentPhaseID(currentPhase);
            }
            setGateResults(prev => {
                const incoming = collectWorkflowGateResults(state.gate_results);
                if (incoming.size === 0) return prev;
                const next = new Map(prev);
                for (const [phaseID, result] of incoming) next.set(phaseID, result);
                return next;
            });

            // Auto-open split mode for phases that are expected to produce preview documents.
            // When the active phase is awaiting form input (awaiting_form=true from backend),
            // never auto-open the doc preview — the AgentView form takes priority.
            const isAwaitingForm = state.awaiting_form === true;
            if (isActive && !userClosedRef.current && !isAwaitingForm) {
                setWorkflowSplitMode(prev => {
                    if (!currentPhase) return false;
                    if (workflowPhaseExpectsDocument(currentPhase, incomingPhases, incomingWorkflowType)) return true;
                    // Keep the workflow board visible while moving into coding/execution
                    // phases, but do not auto-open the pane if it was closed or never opened.
                    return prev;
                });
            } else if (isActive && isAwaitingForm) {
                // Actively close the doc preview if it was left open from a previous
                // workflow — the form panel needs visual priority and there's no
                // document content to display yet.
                setWorkflowSplitMode(false);
            }
            if (!isActive) {
                const isCancelled = typeof state.status === "string" && state.status === "cancelled";
                if (isCancelled) {
                    // Workflow was explicitly cancelled — close panel and clear state
                    // so stale documents from this workflow don't remain visible.
                    setWorkflowSplitMode(false);
                    setPhaseDocuments(new Map());
                    setGateResults(new Map());
                    setPhases([]);
                    setCurrentPhaseID("");
                    setLatestDocumentPhaseID("");
                    workflowIDRef.current = "";
                    workflowTypeRef.current = "";
                    docUpdatePhaseIDsRef.current = new Set();
                } else {
                    // Workflow completed naturally — keep documents visible for review.
                    // splitMode is only cleared on explicit user action (close button),
                    // new workflow start (new workflow ID detected), or full reset (null state).
                }
                userClosedRef.current = false;
            }
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("workflow:phase_update");
        };
    }, [setWorkflowSplitMode]);

    // Listen for document updates
    useEffect(() => {
        const unsub = EventsOn("workflow:doc_update", (data: any) => {
            if (!data?.phase_id) return;

            // Workflow instance isolation: use workflow_id as primary routing key.
            const eventProjectPath = typeof data.project_path === "string" ? data.project_path.trim() : "";
            const eventWorkflowID = typeof data.workflow_id === "string" ? data.workflow_id.trim() : "";
            if (!shouldAcceptWorkflowEvent(eventWorkflowID, eventProjectPath, workflowIDRef.current, activeTabProjectPathRef.current)) {
                return;
            }

            const phaseID = normalizeWorkflowPhaseID(data.phase_id);
            const content = normalizeWorkflowDocumentContent(data.content);
            if (!content) return;
            if (!phaseID) return;
            docUpdatePhaseIDsRef.current.add(phaseID);
            setPhaseDocuments(prev => {
                const next = new Map(prev);
                next.set(phaseID, content);
                return next;
            });
            // Track the latest document separately from the workflow's
            // current phase. A document event is a preview signal; it must
            // not rewrite the active workflow phase shown on the board.
            setLatestDocumentPhaseID(phaseID);
            // Auto-open split mode when new doc content arrives
            if (!userClosedRef.current) {
                setWorkflowSplitMode(true);
            }
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("workflow:doc_update");
        };
    }, [setWorkflowSplitMode]);

    // Listen for gate results
    useEffect(() => {
        const unsub = EventsOn("workflow:gate_result", (data: any) => {
            if (!workflowActiveRef.current) return;
            if (!data?.phase_id || !data?.result) return;

            // Workflow instance isolation: use workflow_id as primary routing key.
            const eventProjectPath = typeof data.project_path === "string" ? data.project_path.trim() : "";
            const eventWorkflowID = typeof data.workflow_id === "string" ? data.workflow_id.trim() : "";
            if (!shouldAcceptWorkflowEvent(eventWorkflowID, eventProjectPath, workflowIDRef.current, activeTabProjectPathRef.current)) {
                return;
            }

            const phaseID = normalizeWorkflowPhaseID(data.phase_id);
            if (!phaseID) return;
            setGateResults(prev => {
                const result = data.result as Partial<QualityGateResult>;
                const next = new Map(prev);
                next.set(phaseID, {
                    ...(result as QualityGateResult),
                    phase_id: phaseID,
                    passed: result.passed === true,
                    items: Array.isArray(result.items) ? result.items : [],
                    checked_at: typeof result.checked_at === "string" ? result.checked_at : "",
                });
                return next;
            });
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("workflow:gate_result");
        };
    }, []);

    // Listen for maximize suggestion when workflow starts
    useEffect(() => {
        const unsub = EventsOn("workflow:suggest_maximize", (data: any) => {
            if (!data?.workflow_type) return;
            setSuggestMaximize(true);
            setSuggestMaximizeType(data.workflow_type);
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("workflow:suggest_maximize");
        };
    }, []);

    // Listen for maximize suggestion dismissal (workflow cancelled/completed/reset)
    useEffect(() => {
        const unsub = EventsOn("workflow:suggest_maximize_dismiss", () => {
            setSuggestMaximize(false);
            setSuggestMaximizeType("");
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("workflow:suggest_maximize_dismiss");
        };
    }, []);

    // Listen for transient text messages (e.g. phase transition notifications)
    useEffect(() => {
        const unsub = EventsOn("workflow:text", (data: any) => {
            if (!data?.text) return;
            setTransientText(data.text);
            if (transientTextTimerRef.current) {
                clearTimeout(transientTextTimerRef.current);
            }
            // Auto-clear after 5 seconds
            transientTextTimerRef.current = setTimeout(() => {
                setTransientText("");
                transientTextTimerRef.current = null;
            }, 5000);
        });
        return () => {
            if (transientTextTimerRef.current) {
                clearTimeout(transientTextTimerRef.current);
                transientTextTimerRef.current = null;
            }
            if (typeof unsub === "function") unsub();
            else EventsOff("workflow:text");
        };
    }, []);

    // Listen for working directory changes
    useEffect(() => {
        const unsub = EventsOn("workflow:workdir_set", (data: any) => {
            const path = typeof data?.path === "string" ? data.path.trim() : "";
            if (!path) return;
            if (!workflowActiveRef.current) {
                if (!workflowIDRef.current) pendingWorkingDirRef.current = path;
                return;
            }
            setWorkingDir(path);
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("workflow:workdir_set");
        };
    }, []);

    const openDocPreview = useCallback((phaseID?: string) => {
        userClosedRef.current = false;
        setWorkflowSplitMode(true);
        const normalizedPhaseID = normalizeWorkflowPhaseID(phaseID);
        if (normalizedPhaseID) setLatestDocumentPhaseID(normalizedPhaseID);
    }, [setWorkflowSplitMode]);

    const closeDocPreview = useCallback(() => {
        userClosedRef.current = true;
        setWorkflowSplitMode(false);
    }, [setWorkflowSplitMode]);

    const setSplitRatio = useCallback((ratio: number) => {
        setSplitRatioState(Math.max(0.2, Math.min(0.8, ratio)));
    }, []);

    const dismissMaximizeSuggestion = useCallback(() => {
        setSuggestMaximize(false);
        setSuggestMaximizeType("");
    }, []);

    /** Returns a snapshot of the current workflow UI state for per-tab save/restore. */
    const stateRef = useRef<WorkflowUIState>({
        active,
        splitMode,
        splitRatio,
        workflowType,
        currentPhaseID,
        latestDocumentPhaseID,
        phaseDocuments,
        gateResults,
        phases,
        suggestMaximize,
        suggestMaximizeType,
        transientText,
        workingDir,
        workflowID: workflowIDRef.current,
        docUpdatePhaseIDs: new Set(docUpdatePhaseIDsRef.current),
    });
    stateRef.current = {
        active,
        splitMode,
        splitRatio,
        workflowType,
        currentPhaseID,
        latestDocumentPhaseID,
        phaseDocuments,
        gateResults,
        phases,
        suggestMaximize,
        suggestMaximizeType,
        transientText,
        workingDir,
        workflowID: workflowIDRef.current,
        docUpdatePhaseIDs: new Set(docUpdatePhaseIDsRef.current),
    };
    const getSnapshot = useCallback((): WorkflowUIState => stateRef.current, []);

    /** Overwrites the entire workflow UI state from a saved snapshot. */
    const restoreState = useCallback((snapshot: WorkflowUIState) => {
        setActive(snapshot.active);
        setWorkflowSplitMode(snapshot.splitMode);
        setSplitRatioState(snapshot.splitRatio);
        setWorkflowType(snapshot.workflowType);
        setCurrentPhaseID(snapshot.currentPhaseID);
        setLatestDocumentPhaseID(snapshot.latestDocumentPhaseID);
        setPhaseDocuments(snapshot.phaseDocuments);
        setGateResults(snapshot.gateResults);
        setPhases(snapshot.phases);
        setSuggestMaximize(snapshot.suggestMaximize);
        setSuggestMaximizeType(snapshot.suggestMaximizeType);
        setTransientText(snapshot.transientText);
        setWorkingDir(snapshot.workingDir);
        workflowActiveRef.current = snapshot.active;
        workflowTypeRef.current = snapshot.workflowType;
        workflowIDRef.current = snapshot.workflowID || "";
        docUpdatePhaseIDsRef.current = snapshot.docUpdatePhaseIDs
            ? new Set(snapshot.docUpdatePhaseIDs)
            : new Set();
    }, [setWorkflowSplitMode]);

    /** Resets workflow state to defaults (empty/inactive). */
    const resetState = useCallback(() => {
        setActive(false);
        setWorkflowSplitMode(false);
        setSplitRatioState(DEFAULT_SPLIT_RATIO);
        setWorkflowType("");
        setCurrentPhaseID("");
        setLatestDocumentPhaseID("");
        setPhaseDocuments(new Map());
        setGateResults(new Map());
        setPhases([]);
        setSuggestMaximize(false);
        setSuggestMaximizeType("");
        setTransientText("");
        setWorkingDir("");
        userClosedRef.current = false;
        docUpdatePhaseIDsRef.current = new Set();
        workflowIDRef.current = "";
        workflowTypeRef.current = "";
        workflowActiveRef.current = false;
        pendingWorkingDirRef.current = "";
    }, [setWorkflowSplitMode]);

    return {
        state: {
            active,
            splitMode,
            splitRatio,
            workflowType,
            currentPhaseID,
            latestDocumentPhaseID,
            phaseDocuments,
            gateResults,
            phases,
            suggestMaximize,
            suggestMaximizeType,
            transientText,
            workingDir,
            workflowID: workflowIDRef.current,
            docUpdatePhaseIDs: docUpdatePhaseIDsRef.current,
        } as WorkflowUIState,
        openDocPreview,
        closeDocPreview,
        setSplitRatio,
        dismissMaximizeSuggestion,
        getSnapshot,
        restoreState,
        resetState,
    };
}
