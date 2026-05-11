export type WorkflowPhaseID = string;

const PHASE_ID_ALIASES: Record<string, WorkflowPhaseID> = {
    tech_design: "design",
    task_breakdown: "tasks",
};

const FALLBACK_NON_DOCUMENT_PHASE_IDS = new Set<WorkflowPhaseID>([
    "implementation",
    "test_execution",
    "ppt_generation",
    "bp_doc_generation",
    "controlled_execution",
]);

export interface PhaseInfo {
    id: WorkflowPhaseID;
    name: string;
    index: number;
    expectsDocument?: boolean;
}

export function normalizeWorkflowPhaseID(phaseID: unknown): WorkflowPhaseID {
    if (typeof phaseID !== "string") return "";
    const trimmed = phaseID.trim();
    return PHASE_ID_ALIASES[trimmed] || trimmed;
}

export function collectWorkflowPhases(phases: unknown): PhaseInfo[] {
    if (!Array.isArray(phases)) return [];
    const collected: PhaseInfo[] = [];
    const seen = new Set<WorkflowPhaseID>();
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

export function workflowPhaseExpectsDocument(phaseID: WorkflowPhaseID, phases: PhaseInfo[]): boolean {
    const phase = phases.find(item => item.id === phaseID);
    if (phase && typeof phase.expectsDocument === "boolean") return phase.expectsDocument;
    return !FALLBACK_NON_DOCUMENT_PHASE_IDS.has(phaseID);
}
