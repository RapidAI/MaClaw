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
}

/** UI state for the workflow split-pane document preview. */
export interface WorkflowUIState {
    active: boolean;
    splitMode: boolean;
    splitRatio: number;
    currentPhaseID: string;
    phaseDocuments: Map<string, string>;
    gateResults: Map<string, QualityGateResult>;
    phases: PhaseInfo[];
    suggestMaximize: boolean;
    suggestMaximizeType: string;
    transientText: string;
}

const DEFAULT_SPLIT_RATIO = 0.5;

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
    const [currentPhaseID, setCurrentPhaseID] = useState("");
    const [phaseDocuments, setPhaseDocuments] = useState<Map<string, string>>(new Map());
    const [gateResults, setGateResults] = useState<Map<string, QualityGateResult>>(new Map());
    const [phases, setPhases] = useState<PhaseInfo[]>([]);
    const [suggestMaximize, setSuggestMaximize] = useState(false);
    const [suggestMaximizeType, setSuggestMaximizeType] = useState("");
    const [transientText, setTransientText] = useState("");
    const userClosedRef = useRef(false);

    // Listen for phase updates
    useEffect(() => {
        const unsub = EventsOn("workflow:phase_update", (state: any) => {
            if (!state) {
                setActive(false);
                setSplitMode(false);
                setPhaseDocuments(new Map());
                setGateResults(new Map());
                return;
            }
            setActive(state.status === "active");
            setCurrentPhaseID(state.current_phase || "");

            // Auto-open split mode for doc phases (not implementation)
            if (state.status === "active" && !userClosedRef.current) {
                const isImplPhase = state.current_phase === "implementation" || state.current_phase === "test_execution";
                setSplitMode(!isImplPhase);
            }
            if (state.status !== "active") {
                setSplitMode(false);
                userClosedRef.current = false;
                setPhaseDocuments(new Map());
                setGateResults(new Map());
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
            if (!data?.phase_id || !data?.content) return;
            setPhaseDocuments(prev => {
                const next = new Map(prev);
                next.set(data.phase_id, data.content);
                return next;
            });
            // Auto-set currentPhaseID to the latest document's phase. This
            // ensures the preview panel shows the most recent document,
            // especially in the steering-based flow where doc_update events
            // arrive without prior phase_update events.
            setCurrentPhaseID(data.phase_id);
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
            if (!data?.phase_id || !data?.result) return;
            setGateResults(prev => {
                const next = new Map(prev);
                next.set(data.phase_id, data.result);
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

    // Listen for transient text messages (e.g. phase transition notifications)
    useEffect(() => {
        const unsub = EventsOn("workflow:text", (data: any) => {
            if (!data?.text) return;
            setTransientText(data.text);
            // Auto-clear after 5 seconds
            setTimeout(() => setTransientText(""), 5000);
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("workflow:text");
        };
    }, []);

    const openDocPreview = useCallback((phaseID?: string) => {
        userClosedRef.current = false;
        setSplitMode(true);
        if (phaseID) setCurrentPhaseID(phaseID);
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
            currentPhaseID,
            phaseDocuments,
            gateResults,
            phases,
            suggestMaximize,
            suggestMaximizeType,
            transientText,
        } as WorkflowUIState,
        openDocPreview,
        closeDocPreview,
        setSplitRatio,
        dismissMaximizeSuggestion,
    };
}
