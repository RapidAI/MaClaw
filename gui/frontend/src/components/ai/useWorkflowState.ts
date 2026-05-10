import { useCallback, useEffect, useRef, useState } from "react";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";

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

/** Phase info from the backend WorkflowState. */
export interface PhaseInfo {
    id: string;
    name: string;
    index: number;
    expectsDocument?: boolean;
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
}

const DEFAULT_SPLIT_RATIO = 0.42;
const PHASE_ID_ALIASES: Record<string, string> = {
    tech_design: "design",
    task_breakdown: "tasks",
};
const FALLBACK_NON_DOCUMENT_PHASE_IDS = new Set([
    "implementation",
    "test_execution",
    "ppt_generation",
    "bp_doc_generation",
    "controlled_execution",
]);

export function normalizeWorkflowPhaseID(phaseID: unknown): string {
    if (typeof phaseID !== "string") return "";
    const trimmed = phaseID.trim();
    return PHASE_ID_ALIASES[trimmed] || trimmed;
}

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

function collectWorkflowPhases(phases: unknown): PhaseInfo[] {
    if (!Array.isArray(phases)) return [];
    const collected: PhaseInfo[] = [];
    const seen = new Set<string>();
    for (const raw of phases) {
        if (!raw || typeof raw !== "object") continue;
        const phase = raw as Record<string, unknown>;
        const id = normalizeWorkflowPhaseID(phase.id);
        const name = typeof phase.name === "string" ? phase.name.trim() : "";
        const index = typeof phase.index === "number" ? phase.index : collected.length;
        const expectsDocument = typeof phase.expects_document === "boolean" ? phase.expects_document : undefined;
        if (!id || seen.has(id)) continue;
        seen.add(id);
        const item: PhaseInfo = { id, name, index };
        if (typeof expectsDocument === "boolean") item.expectsDocument = expectsDocument;
        collected.push(item);
    }
    return collected.sort((a, b) => a.index - b.index);
}

function workflowPhaseExpectsDocument(phaseID: string, phases: PhaseInfo[]): boolean {
    const phase = phases.find(item => item.id === phaseID);
    if (phase && typeof phase.expectsDocument === "boolean") return phase.expectsDocument;
    return !FALLBACK_NON_DOCUMENT_PHASE_IDS.has(phaseID);
}

function resolveWorkflowInstanceKey(state: any): string {
    const id = typeof state?.id === "string" ? state.id.trim() : "";
    if (id) return id;
    const createdAt = typeof state?.created_at === "string" ? state.created_at.trim() : "";
    const type = typeof state?.type === "string" ? state.type.trim() : "";
    return createdAt && type ? `${type}:${createdAt}` : "";
}

/**
 * useWorkflowState manages the workflow UI state for the split-pane
 * document preview in AIAssistantPanel.
 *
 * Listens to Wails events:
 *   - workflow:phase_update  — phase changes
 *   - workflow:doc_update    — document content updates
 *   - workflow:gate_result   — quality gate results
 */
export function useWorkflowState() {
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

    // Listen for phase updates
    useEffect(() => {
        const unsub = EventsOn("workflow:phase_update", (state: any) => {
            if (!state) {
                // Workflow fully reset — clear everything.
                setActive(false);
                setSplitMode(false);
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
            const workflowID = resolveWorkflowInstanceKey(state);
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
            } else if (!workflowID && state.status === "active" && workflowTypeRef.current && incomingWorkflowType && incomingWorkflowType !== workflowTypeRef.current) {
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
            const isActive = state.status === "active";
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
            if (isActive && !userClosedRef.current) {
                setSplitMode(currentPhase ? workflowPhaseExpectsDocument(currentPhase, incomingPhases) : false);
            }
            if (!isActive) {
                setSplitMode(false);
                userClosedRef.current = false;
                // Don't clear phaseDocuments here — preserve documents so
                // the user can still view them (e.g. task decomposition)
                // after the workflow phase ends. Documents are only cleared
                // on full workflow reset (null state above).
            }
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("workflow:phase_update");
        };
    }, []);

    // Listen for document updates
    useEffect(() => {
        const unsub = EventsOn("workflow:doc_update", (data: any) => {
            if (!workflowActiveRef.current) return;
            if (!data?.phase_id) return;
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
                setSplitMode(true);
            }
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("workflow:doc_update");
        };
    }, []);

    // Listen for gate results
    useEffect(() => {
        const unsub = EventsOn("workflow:gate_result", (data: any) => {
            if (!workflowActiveRef.current) return;
            if (!data?.phase_id || !data?.result) return;
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
        setSplitMode(true);
        const normalizedPhaseID = normalizeWorkflowPhaseID(phaseID);
        if (normalizedPhaseID) setLatestDocumentPhaseID(normalizedPhaseID);
    }, []);

    const closeDocPreview = useCallback(() => {
        userClosedRef.current = true;
        setSplitMode(false);
    }, []);

    const setSplitRatio = useCallback((ratio: number) => {
        setSplitRatioState(Math.max(0.2, Math.min(0.8, ratio)));
    }, []);

    const dismissMaximizeSuggestion = useCallback(() => {
        setSuggestMaximize(false);
        setSuggestMaximizeType("");
    }, []);

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
        } as WorkflowUIState,
        openDocPreview,
        closeDocPreview,
        setSplitRatio,
        dismissMaximizeSuggestion,
    };
}
