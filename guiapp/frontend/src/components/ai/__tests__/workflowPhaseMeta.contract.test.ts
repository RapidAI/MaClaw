/**
 * Anti-drift contract tests between the frontend hardcoded fallback maps
 * (`workflowPhaseOrders`, `phaseLabels`, `fallbackNonDocumentPhaseIDs`) and the
 * code-generated artifact (`workflowPhaseMeta.generated.ts`, the byte-stable
 * projection of the backend `workflow.PhaseMetadata` deriver).
 *
 * The generated artifact is the canonical mirror of the Go templates; these tests
 * guarantee the retained degraded-mode fallback maps can never silently diverge
 * from it.
 *
 * Task 10.1 — Property 3: Metadata-present rendering ⊇ hardcoded-fallback rendering
 *   **Validates: Requirements 3.1**
 * Task 10.2 — Property 4: Fallback maps agree with generated artifact (anti-drift)
 *   **Validates: Requirements 2.1, 2.2**
 *
 * The contract domain is the OVERLAP: we iterate the fallback workflow types
 * (`Object.keys(workflowPhaseOrders)`) and only assert against generated entries
 * that exist. A fallback type with no generated entry is reported, not silently
 * skipped.
 */
import { describe, it, expect } from 'vitest';
import * as fc from 'fast-check';
import {
    deriveProgressPhases,
    workflowPhaseOrders,
    phaseLabels,
    fallbackNonDocumentPhaseIDs,
} from '../WorkflowDocPreview';
import { normalizeWorkflowPhaseID, workflowPhaseExpectsDocument } from '../workflowPhase';
import type { PhaseInfo } from '../useWorkflowState';
import { WORKFLOW_PHASE_META } from '../workflowPhaseMeta.generated';

// The generated artifact is the backend's full set of registered workflow types
// (the single source of truth). Coverage assertions iterate THIS set so a newly
// added backend template is caught, rather than only the subset that happens to
// appear in the hand-maintained workflowPhaseOrders fallback.
const generatedTypes = Object.keys(WORKFLOW_PHASE_META);
// The hand-maintained fallback types. Property 4 (label/doc-expectation agreement)
// is only meaningful for the OVERLAP between this and the generated artifact.
const fallbackTypes = Object.keys(workflowPhaseOrders);

/**
 * Project the generated metadata for a workflow type into the PhaseInfo[] shape
 * that deriveProgressPhases consumes. GeneratedPhaseMeta already uses camelCase
 * (id/name/index/expectsDocument/canSkip/needsConfirm), so this is a structural copy.
 */
function generatedAsPhaseInfo(type: string): PhaseInfo[] {
    const gen = WORKFLOW_PHASE_META[type] ?? [];
    return gen.map(m => ({
        id: m.id,
        name: m.name,
        index: m.index,
        expectsDocument: m.expectsDocument,
        canSkip: m.canSkip,
        needsConfirm: m.needsConfirm,
        kind: m.kind,
        toolPolicy: m.toolPolicy,
        mutationScope: m.mutationScope,
        activatesOrchestrator: m.activatesOrchestrator,
    }));
}

/** Look up a generated phase by id, applying canonicalization as a safety net. */
function generatedPhase(type: string, id: string) {
    const gen = new Map((WORKFLOW_PHASE_META[type] ?? []).map(m => [m.id, m]));
    return gen.get(normalizeWorkflowPhaseID(id)) ?? gen.get(id);
}

// ── Task 10.1 — Property 3: Metadata-present rendering ⊇ hardcoded-fallback rendering ──
// **Validates: Requirements 3.1**
describe('Property 3: metadata-present rendering is a superset of the hardcoded fallback', () => {
    // Sanity: the contract is only meaningful where a generated entry exists. Any
    // fallback type missing from the artifact is reported (not silently skipped).
    it('every fallback workflow type has a generated metadata entry', () => {
        const missing = fallbackTypes.filter(
            t => !WORKFLOW_PHASE_META[t] || WORKFLOW_PHASE_META[t].length === 0,
        );
        expect(
            missing,
            `fallback types absent from the generated artifact (no entry to compare against): ${missing.join(', ')}`,
        ).toEqual([]);
    });

    // For every fallback type, the id set rendered from the generated metadata via
    // deriveProgressPhases contains every id in workflowPhaseOrders[type], and each
    // such id resolves to a non-empty label. (Switching to metadata never loses a phase.)
    it('renders a superset of the fallback order with every id resolving to a non-empty label', () => {
        fc.assert(
            fc.property(fc.constantFrom(...fallbackTypes), (type) => {
                const meta = generatedAsPhaseInfo(type);
                // Guard: a fallback type with no generated entry cannot be checked here
                // (reported by the test above); treat as vacuously satisfied.
                if (meta.length === 0) return true;

                const rendered = deriveProgressPhases(type, meta, new Map(), '');
                const ids = new Set(rendered.map(p => p.id));
                const labelByID = new Map(rendered.map(p => [p.id, p.label]));

                for (const fallbackID of workflowPhaseOrders[type]) {
                    const canonical = normalizeWorkflowPhaseID(fallbackID);
                    if (!ids.has(canonical)) {
                        throw new Error(
                            `${type}: rendered metadata is missing fallback phase id "${fallbackID}" (canonical "${canonical}")`,
                        );
                    }
                    const label = labelByID.get(canonical) ?? '';
                    if (label.trim().length === 0) {
                        throw new Error(`${type}: phase id "${canonical}" resolved to an empty label`);
                    }
                }
                return true;
            }),
            { numRuns: 200 },
        );
    });
});

// ── Degraded-mode coverage: every backend template renders, even without a fallback entry ──
// **Validates: Requirements 3.1, 3.3**
//
// Root-cause guard: degraded-mode rendering must track the BACKEND template set
// (the generated artifact), not the hand-maintained workflowPhaseOrders subset.
// deriveProgressPhases now uses WORKFLOW_PHASE_META[type] as the primary
// degraded-mode source, so a backend template with no workflowPhaseOrders entry
// (e.g. ops_maintenance, changjiang_scholar) still renders a complete board.
describe('Degraded-mode rendering covers every registered backend template', () => {
    it('renders a complete, non-empty, well-labeled board for every generated type with no emitted metadata', () => {
        const failures: string[] = [];
        for (const type of generatedTypes) {
            const generated = WORKFLOW_PHASE_META[type] ?? [];
            // Degraded mode: pass NO emitted metadata; deriveProgressPhases must fall
            // back to the generated artifact for this type.
            const rendered = deriveProgressPhases(type, undefined, new Map(), '');
            const renderedIDs = rendered.map(p => p.id);

            if (renderedIDs.length !== generated.length) {
                failures.push(`${type}: rendered ${renderedIDs.length} phases, expected ${generated.length} from the generated artifact`);
                continue;
            }
            for (let i = 0; i < generated.length; i++) {
                if (renderedIDs[i] !== generated[i].id) {
                    failures.push(`${type}: phase[${i}] id "${renderedIDs[i]}" != generated "${generated[i].id}" (order must match the backend)`);
                }
                if (rendered[i].label.trim().length === 0) {
                    failures.push(`${type}: phase "${renderedIDs[i]}" rendered an empty label in degraded mode`);
                }
                if (rendered[i].expectsDocument !== generated[i].expectsDocument) {
                    failures.push(`${type}: phase "${renderedIDs[i]}" doc-expectation ${rendered[i].expectsDocument} != generated ${generated[i].expectsDocument}`);
                }
            }
        }
        expect(failures, `degraded-mode rendering gaps (type/phase):\n${failures.join('\n')}`).toEqual([]);
    });

    it('renders a complete board in degraded mode for backend types absent from the hand-maintained fallback', () => {
        const backendOnlyTypes = generatedTypes.filter(t => !fallbackTypes.includes(t));
        // This list is expected to be non-empty (the backend has more templates than
        // the legacy fallback map); each must still render fully from the artifact.
        for (const type of backendOnlyTypes) {
            const generated = WORKFLOW_PHASE_META[type] ?? [];
            const rendered = deriveProgressPhases(type, undefined, new Map(), '');
            expect(
                rendered.map(p => p.id),
                `${type} (backend-only, no workflowPhaseOrders entry) must render from the generated artifact`,
            ).toEqual(generated.map(m => m.id));
            for (const phase of rendered) {
                expect(phase.label.trim().length, `${type}/${phase.id} must have a non-empty degraded-mode label`).toBeGreaterThan(0);
            }
        }
    });
});

// ── Resolver agreement: auto-open doc-expectation == board doc-expectation ──
// **Validates: Requirements 5.1 (single derived doc-expectation value)**
//
// The board (deriveProgressPhases) and the auto-open decision
// (workflowPhaseExpectsDocument in useWorkflowState) are two independent
// consumers of doc-expectation. In degraded mode they must resolve the SAME
// value for every phase of every backend template, or the board could show an
// execution phase while auto-open wrongly opens an empty preview pane (the
// Finding 3 class of dual-source divergence). This guard pins that agreement to
// the backend template set so a future divergence fails CI.
describe('Doc-expectation resolvers agree across board and auto-open in degraded mode', () => {
    it('workflowPhaseExpectsDocument matches the board doc-expectation for every generated phase', () => {
        const divergences: string[] = [];
        for (const type of generatedTypes) {
            // Board side: derive with NO emitted metadata (degraded mode); the board
            // resolves doc-expectation from the generated artifact via deriveProgressPhases.
            const boardByID = new Map(
                deriveProgressPhases(type, undefined, new Map(), '').map(p => [p.id, p.expectsDocument]),
            );
            for (const m of WORKFLOW_PHASE_META[type] ?? []) {
                // Auto-open side: workflowPhaseExpectsDocument with empty emitted phases,
                // so it too falls back to the generated artifact for this type.
                const autoOpen = workflowPhaseExpectsDocument(m.id, [], type);
                const board = boardByID.get(m.id);
                if (autoOpen !== board) {
                    divergences.push(`${type}/${m.id}: auto-open=${autoOpen} but board=${board}`);
                }
                // Both must also equal the backend source of truth.
                if (autoOpen !== m.expectsDocument) {
                    divergences.push(`${type}/${m.id}: auto-open=${autoOpen} but generated.expectsDocument=${m.expectsDocument}`);
                }
            }
        }
        expect(divergences, `doc-expectation resolver divergences (type/phase):\n${divergences.join('\n')}`).toEqual([]);
    });
});

describe('Generated workflow phase contract metadata', () => {
    it('marks coding implementation as project execution and PPT generation as non-orchestrated artifact work', () => {
        expect(generatedPhase('coding', 'implementation')).toMatchObject({
            kind: 'execution',
            toolPolicy: 'full',
            mutationScope: 'project',
            activatesOrchestrator: true,
        });

        expect(generatedPhase('presentation_design', 'ppt_generation')).toMatchObject({
            kind: 'artifact_generation',
            toolPolicy: 'full',
            mutationScope: 'artifact',
            activatesOrchestrator: false,
        });
    });

    it('keeps coding task breakdown as reviewable workflow-doc planning, not project execution', () => {
        expect(generatedPhase('coding', 'tasks')).toMatchObject({
            kind: 'code_planning',
            toolPolicy: 'planning',
            mutationScope: 'workflow_doc',
            expectsDocument: true,
            needsConfirm: true,
            activatesOrchestrator: false,
        });
    });
});

// ── Task 10.2 — Property 4: Fallback maps agree with generated artifact (anti-drift) ──
// **Validates: Requirements 2.1, 2.2**
describe('Property 4: hardcoded fallback maps never drift from the generated artifact', () => {
    // Requirement 2.1 (document-expectation half) + 2.2: for each overlapping id,
    // fallbackNonDocumentPhaseIDs.has(id) must equal !generated.expectsDocument.
    // On divergence the message identifies the workflow type and phase id.
    it('fallback document-expectation agrees with the generated expects_document flag', () => {
        const divergences: string[] = [];
        for (const type of fallbackTypes) {
            for (const id of workflowPhaseOrders[type]) {
                const g = generatedPhase(type, id);
                if (!g) continue; // no overlapping generated entry → nothing to compare
                const fallbackNonDoc = fallbackNonDocumentPhaseIDs.has(id);
                if (fallbackNonDoc !== !g.expectsDocument) {
                    divergences.push(
                        `${type}/${id}: fallbackNonDocumentPhaseIDs.has=${fallbackNonDoc} but generated.expectsDocument=${g.expectsDocument}`,
                    );
                }
            }
        }
        expect(
            divergences,
            `document-expectation divergences (type/phase):\n${divergences.join('\n')}`,
        ).toEqual([]);
    });

    // Requirement 2.1 (label half) + 2.2: where a fallback label is defined, it must be
    // character-for-character identical to the generated name (an absent fallback label
    // is treated as agreeing). On divergence the message identifies the type and phase id.
    it('fallback labels, when defined, are character-for-character equal to the generated name', () => {
        const divergences: string[] = [];
        for (const type of fallbackTypes) {
            for (const id of workflowPhaseOrders[type]) {
                const g = generatedPhase(type, id);
                if (!g) continue; // no overlapping generated entry → nothing to compare
                const fallbackLabel = phaseLabels[id];
                if (fallbackLabel !== undefined && fallbackLabel !== g.name) {
                    divergences.push(
                        `${type}/${id}: phaseLabels["${id}"]="${fallbackLabel}" but generated.name="${g.name}"`,
                    );
                }
            }
        }
        expect(
            divergences,
            `label divergences (type/phase):\n${divergences.join('\n')}`,
        ).toEqual([]);
    });
});
