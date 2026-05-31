export type WorkflowPhaseID = string;

import { WORKFLOW_PHASE_META } from "./workflowPhaseMeta.generated";

const PHASE_ID_ALIASES: Record<string, WorkflowPhaseID> = {
    tech_design: "design",
    task_breakdown: "tasks",
};

/**
 * The set of phase IDs that do NOT produce a preview document, used only in
 * degraded mode (when backend-emitted metadata is absent). This is the single
 * frontend owner of that set: `WorkflowDocPreview` re-exports it as
 * `fallbackNonDocumentPhaseIDs` so the anti-drift contract test
 * (`workflowPhaseMeta.contract.test.ts`) validates this one set against the
 * generated artifact. It mirrors the backend rule
 * `PhaseExpectsDocument == false` (ToolFilterFull / ToolFilterOpsControlled).
 */
export const FALLBACK_NON_DOCUMENT_PHASE_IDS = new Set<WorkflowPhaseID>([
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
    canSkip?: boolean;
    needsConfirm?: boolean;
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
        const canSkip = typeof phase.can_skip === "boolean" ? phase.can_skip : undefined;
        const needsConfirm = typeof phase.needs_confirm === "boolean" ? phase.needs_confirm : undefined;
        if (!id || seen.has(id)) continue;
        seen.add(id);
        const item: PhaseInfo = { id, name, index };
        if (typeof expectsDocument === "boolean") item.expectsDocument = expectsDocument;
        if (typeof canSkip === "boolean") item.canSkip = canSkip;
        if (typeof needsConfirm === "boolean") item.needsConfirm = needsConfirm;
        collected.push(item);
    }
    return collected.sort((a, b) => a.index - b.index);
}

/**
 * Resolve whether a phase produces a preview document. This is the shared
 * doc-expectation resolver used by the auto-open decision (useWorkflowState) so
 * it agrees with the board's resolution (deriveProgressPhases) — both consult the
 * same authoritative sources in the same order, preventing the Finding 3 class of
 * divergence where the board shows an execution phase while auto-open wrongly
 * opens a preview pane.
 *
 * Resolution order:
 *   1. Emitted metadata (`phases`) — authoritative when the backend sent it.
 *   2. The generated artifact (`WORKFLOW_PHASE_META[workflowType]`) — the byte-stable
 *      backend projection covering every registered template, used in degraded mode.
 *   3. The hardcoded `FALLBACK_NON_DOCUMENT_PHASE_IDS` — last resort for a type the
 *      generated artifact does not cover.
 */
export function workflowPhaseExpectsDocument(
    phaseID: WorkflowPhaseID,
    phases: PhaseInfo[],
    workflowType?: string,
): boolean {
    const phase = phases.find(item => item.id === phaseID);
    if (phase && typeof phase.expectsDocument === "boolean") return phase.expectsDocument;

    // Degraded mode: prefer the generated artifact (backend truth for all templates)
    // before the hardcoded fallback set, matching deriveProgressPhases.
    if (workflowType) {
        const generated = WORKFLOW_PHASE_META[workflowType];
        if (generated) {
            const match = generated.find(m => m.id === phaseID);
            if (match) return match.expectsDocument;
        }
    }

    return !FALLBACK_NON_DOCUMENT_PHASE_IDS.has(phaseID);
}
