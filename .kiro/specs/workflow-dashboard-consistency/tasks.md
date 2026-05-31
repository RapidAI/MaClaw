# Implementation Plan: workflow-dashboard-consistency

## Overview

This plan makes the backend workflow templates the single source of truth for dashboard
phase metadata. It builds the mechanism bottom-up: first the one `workflow.PhaseMetadata`
deriver and `WorkflowRegistry.All()` in `corelib/workflow`, then the GUI/TUI adapters that
feed metadata to renderers, then the `cmd/genphasemeta` code generator plus anti-drift
contract tests, and finally the frontend `deriveProgressPhases` reducer that renders purely
from emitted metadata with the hardcoded maps retained only as a degraded-mode fallback.

Each task builds on the previous ones and ends by wiring the new code into a caller, so no
code is left orphaned. The 7 correctness properties from the design are turned into
property-based test sub-tasks (Go: `pgregory.net/rapid`, already a module dependency;
TypeScript: `fast-check`, already a frontend dev dependency), placed next to the code they
validate. Build/test conventions: Go via `go build ./...` / `go test ./...`; frontend via
`npm run build` / `npm test` (vitest) inside `gui/frontend`.

## Tasks

- [x] 1. Backend phase-metadata deriver (corelib/workflow)
  - [x] 1.1 Implement PhaseMeta, CanonicalPhaseID, PhaseExpectsDocument, and PhaseMetadata
    - Create `corelib/workflow/phase_metadata.go`
    - Define `PhaseMeta` struct with json tags `id`, `name`, `index`, `expects_document`, `can_skip`, `needs_confirm`
    - Implement `CanonicalPhaseID(phaseID string) string` applying the alias table (`tech_design`->`design`, `task_breakdown`->`tasks`)
    - Implement `PhaseExpectsDocument(p PhaseTemplate) bool` returning false iff ToolPolicy is `ToolFilterFull` or `ToolFilterOpsControlled`
    - Implement `PhaseMetadata(tmpl *WorkflowTemplate) []PhaseMeta`: return nil for nil/empty template; canonicalize IDs; drop empty/duplicate canonical IDs keeping first occurrence; assign contiguous 0-based `Index`; copy `Name`, `CanSkip`, `NeedsConfirm`
    - _Requirements: 1.1, 1.2, 1.3_

  - [x]* 1.2 Write property test for phase-order derivation
    - **Property 1: Dashboard-derived phase order equals template phase order**
    - Use `pgregory.net/rapid` over `registry.All()` and synthetic templates; assert `idsOf(PhaseMetadata(t))` equals `dedup(canonical(t.Phases))` and `Index` is contiguous 0..n-1
    - **Validates: Requirements 1.1**

  - [x]* 1.3 Write property test for document-expectation rule
    - **Property 6: Document-expectation is determined solely by ToolPolicy**
    - Use `pgregory.net/rapid` over all template phases and synthetic ToolPolicy values; assert `PhaseExpectsDocument(p) == (p.ToolPolicy != ToolFilterFull && p.ToolPolicy != ToolFilterOpsControlled)`
    - **Validates: Requirements 1.3**

  - [x]* 1.4 Write property test for non-empty labels
    - **Property 2: Every emitted phaseID has a non-empty label**
    - Use `pgregory.net/rapid` over `registry.All()`; assert every `PhaseMeta.Name` has at least one non-whitespace character
    - **Validates: Requirements 1.2**

  - [x]* 1.5 Write unit tests for the deriver
    - Table tests for `CanonicalPhaseID` (alias collapse `tech_design`->`design`), `PhaseExpectsDocument` (`ToolFilterFull`/`ToolFilterOpsControlled` vs document policies), and `PhaseMetadata` (coding/PPT/ops templates, `CanSkip` propagation, duplicate canonical-ID collapse, nil template returns nil)
    - _Requirements: 1.1, 1.2, 1.3_

- [x] 2. Deterministic registry enumeration
  - [x] 2.1 Implement WorkflowRegistry.All()
    - Add `func (r *WorkflowRegistry) All() []*WorkflowTemplate` in `corelib/workflow/registry.go`
    - Return one pointer per registered type under a read lock, sorted by `Type` for byte-stable downstream generation
    - _Requirements: 2.4_

  - [x]* 2.2 Write unit test for All() determinism
    - Assert `All()` returns every registered type exactly once and in the same sorted order across repeated calls
    - _Requirements: 2.4_

- [x] 3. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. GUI adapter: emit metadata from the single deriver
  - [x] 4.1 Refactor normalizeWorkflowStateForFrontendWithRegistry to use PhaseMetadata
    - In `gui/workflow_adapter_frontend_state.go`, change `frontendWorkflowState.Phases` to `[]workflow.PhaseMeta` and remove the local `frontendWorkflowPhase` type
    - Replace the inlined `normalizeWorkflowPhasesForFrontend` logic with a call to `workflow.PhaseMetadata(registry.Match(state.Type))`; keep `Phases` `omitempty` so a nil registry omits the field
    - Confirm `gui/workflow_adapter.go` still emits `workflow:phase_update` with the new shape and that `EmitPhaseUpdate(userID, *WorkflowState) error` signature is unchanged
    - _Requirements: 1.4, 3.3, 7.2_

  - [x]* 4.2 Write property test for emitted-payload round trip
    - **Property 7: Round-trip stability of the emitted payload**
    - Use `pgregory.net/rapid` over `registry.All()`; marshal `PhaseMetadata(t)` to JSON and unmarshal back into `[]PhaseMeta`, asserting identical IDs, ascending index order, and identical `expects_document`/`can_skip`/`needs_confirm` flags with nothing dropped, duplicated, or reordered
    - **Validates: Requirements 1.4**

  - [x]* 4.3 Write unit tests for the adapter
    - Assert emitted JSON shape with a registry present (phases populated) and with a nil registry (`phases` omitted); assert `CurrentPhase`/`PhaseOutputs`/`GateResults` canonicalization is preserved
    - _Requirements: 1.4, 3.3_

- [x] 5. TUI parity through the same deriver
  - [x] 5.1 Derive metadata in TUIWorkflowCallbacks.EmitPhaseUpdate
    - In `tui/workflow_integration.go`, have `TUIWorkflowCallbacks.EmitPhaseUpdate` obtain `PhaseMeta` via `workflow.PhaseMetadata` (using the registry the callbacks hold) instead of maintaining a separate phase list; log/render structurally
    - _Requirements: 1.5_

  - [x]* 5.2 Write TUI parity test
    - Assert the `PhaseMeta` slice the TUI derives for a given template is identical to what the GUI adapter produces via `workflow.PhaseMetadata` (same function, no separate list)
    - _Requirements: 1.5_

- [x] 6. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 7. Anti-drift code generator and Go contract test
  - [x] 7.1 Create the cmd/genphasemeta generator
    - Create `cmd/genphasemeta/main.go` that builds `map[string][]workflow.PhaseMeta` from `registry.All()` via `workflow.PhaseMetadata` and renders `gui/frontend/src/components/ai/workflowPhaseMeta.generated.ts` (AUTO-GENERATED header, `GeneratedPhaseMeta` interface, `WORKFLOW_PHASE_META` record), iterating templates in deterministic sorted order
    - _Requirements: 2.4_

  - [x] 7.2 Generate and commit the artifact
    - Run the generator to produce `gui/frontend/src/components/ai/workflowPhaseMeta.generated.ts` and commit it as the canonical generated file
    - _Requirements: 2.4_

  - [x]* 7.3 Write Go contract test for artifact freshness
    - **Property 5: Generated artifact is up to date**
    - Create `cmd/genphasemeta/registry_contract_test.go` that regenerates the artifact in memory from the live registry and fails (with a "run `go generate ./...`" message) if it does not byte-equal the committed file after line-ending normalization
    - **Validates: Requirements 2.3**

- [x] 8. Frontend: render from emitted metadata with fallback as degraded mode
  - [x] 8.1 Extend PhaseInfo and collectWorkflowPhases
    - In `gui/frontend/src/components/ai/workflowPhase.ts`, add optional `canSkip`/`needsConfirm` to `PhaseInfo`; have `collectWorkflowPhases` drop empty/duplicate ids, sort by `index`, and only set the optional booleans when the payload provides them (so `undefined` signals "use fallback" per field)
    - _Requirements: 1.4, 3.2_

  - [x]* 8.2 Write unit tests for collectWorkflowPhases
    - Test dropping empty/duplicate ids, sorting by ascending index, and preserving/omitting the optional booleans
    - _Requirements: 1.4, 3.2_

  - [x] 8.3 Implement deriveProgressPhases and wire it into the board
    - In `gui/frontend/src/components/ai/WorkflowDocPreview.tsx`, implement `deriveProgressPhases(workflowType, phases, phaseDocuments, currentPhaseID)`: when `phases` is non-empty, build order/labels/doc-flags from metadata only and do not read order/labels/doc-expectation from the fallback maps; when empty/absent, use `workflowPhaseOrders`/`phaseLabels`/`fallbackNonDocumentPhaseIDs`; append ids seen only in `phaseDocuments`/`currentPhaseID`, resolving labels via metadata → fallback map → id-derived
    - Drive each phase card's single document-expectation value (document-producing vs execution) from the derived metadata so generation/execution indicators never disagree
    - Render `WorkflowProgressBoard` from the derived output; retain `workflowPhaseOrders`, `phaseLabels`, `fallbackNonDocumentPhaseIDs`, and `WorkflowProgressBoard` symbols
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 5.1, 5.2, 5.3, 7.1_

  - [x] 8.4 Implement current-phase highlighting and progress
    - In the board render path, mark exactly one active node whose canonical id equals the supplied current phase id (after aliasing) within the resolved id list; set progress as a monotonic function of that node's zero-based index, reaching maximum only at the final phase
    - _Requirements: 4.1, 4.2_

  - [x]* 8.5 Write unit tests for deriveProgressPhases
    - Test metadata-first ordering/labels/doc-flags, fallback-when-empty, no fallback reads when metadata present, and appending of document-only/current-phase ids with non-empty labels
    - _Requirements: 3.2, 3.3, 3.4_

  - [x]* 8.6 Write unit tests for highlighting and progress
    - Test exactly-one active node selection (including alias resolution) and monotonic progress that maxes only at the final phase
    - _Requirements: 4.1, 4.2_

- [x] 9. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 10. Frontend contract tests and UI guard wiring
  - [x]* 10.1 Write frontend contract test for metadata superset
    - **Property 3: Metadata-present rendering ⊇ hardcoded-fallback rendering**
    - Create `gui/frontend/src/components/ai/__tests__/workflowPhaseMeta.contract.test.ts` using `fast-check` over the known workflow types; assert the id set rendered from `WORKFLOW_PHASE_META` via `deriveProgressPhases` contains every id in `workflowPhaseOrders[type]` and each resolves to a non-empty label
    - **Validates: Requirements 3.1**

  - [x]* 10.2 Write frontend contract test for fallback/artifact agreement
    - **Property 4: Fallback maps agree with generated artifact (anti-drift)**
    - In `workflowPhaseMeta.contract.test.ts`, for each overlapping id assert `phaseLabels[id]` is absent or character-for-character equal to the generated `name`, and `fallbackNonDocumentPhaseIDs.has(id) === !generated.expectsDocument`; on divergence the failure identifies the workflow type and phase id
    - **Validates: Requirements 2.1, 2.2**

  - [x] 10.3 Extend the UI guard invariants
    - In `scripts/check-main-ui-guards.mjs`, keep asserting `WorkflowProgressBoard` and `workflowPhaseOrders` are present in `WorkflowDocPreview.tsx`, and add checks that `workflowPhaseMeta.generated.ts` exists and is imported by `workflowPhaseMeta.contract.test.ts`, so the anti-drift wiring cannot be deleted silently
    - _Requirements: 7.1, 7.3_

- [x] 11. Regression preservation and final wiring
  - [x]* 11.1 Write regression tests for board/document decoupling and instance reset
    - In a frontend test (e.g. `__tests__/workflowBoardRegression.test.ts`), assert the active-node id and latest-document phase id update independently on auto-advance; assert a new workflow instance replaces phase-document and gate-result collections with empty ones; assert reset/completion clears board phase state, dismisses the maximize suggestion, and retains produced documents until the next instance
    - _Requirements: 6.1, 6.3, 6.7_

  - [x]* 11.2 Write regression tests for the NeedsConfirm preview flow
    - In a frontend test (e.g. `__tests__/workflowPreviewGate.test.ts`), assert that a NeedsConfirm phase producing a document opens the split-pane preview, renders that phase's content, and surfaces its gate result; assert that once the user manually closes the preview it stays closed across subsequent phase/doc/gate events until re-open or a new instance
    - _Requirements: 6.5, 6.6_

  - [x]* 11.3 Write regression tests for all-template coverage and phase-index alignment
    - Add a test asserting every registered template renders through the shared `PhaseMetadata` deriver with no template-specific dashboard path (e.g. iterate `registry.All()`); add a test asserting the engine's current phase index equals the position of the current phase id within the canonical (alias-applied, de-duplicated) phase order
    - _Requirements: 6.2, 6.4_

  - [x] 11.4 Wire go:generate and verify the full build
    - Add a `//go:generate go run ./cmd/genphasemeta` directive (e.g. in `corelib/workflow` or a generate.go) and run `go generate ./...`, `go build ./...`, `go test ./...`, then `npm run build`, `npm test`, and `node scripts/check-main-ui-guards.mjs` in `gui/frontend` to confirm the mechanism, contract tests, and guard all pass end to end
    - _Requirements: 2.3, 2.4_

- [x] 12. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional test tasks and can be skipped for a faster MVP.
- Each task references specific requirement acceptance criteria for traceability.
- Property tests validate the 7 universal correctness properties; unit and regression tests cover examples, edge cases, and preserved behaviors.
- `pgregory.net/rapid` (Go) and `fast-check` (frontend) are already project dependencies, so no new packages are required.
- Build/test commands follow existing conventions: `go build ./...` / `go test ./...` for backend; `npm run build` / `npm test` (vitest) and `node scripts/check-main-ui-guards.mjs` in `gui/frontend` for frontend.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "2.1", "8.1"] },
    { "id": 1, "tasks": ["1.2", "1.3", "1.4", "1.5", "2.2", "4.1", "5.1", "7.1", "8.2", "8.3"] },
    { "id": 2, "tasks": ["4.2", "4.3", "5.2", "7.2", "8.4", "8.5"] },
    { "id": 3, "tasks": ["7.3", "8.6", "10.1"] },
    { "id": 4, "tasks": ["10.2", "11.1", "11.2", "11.3"] },
    { "id": 5, "tasks": ["10.3", "11.4"] }
  ]
}
```
