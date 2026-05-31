# Implementation Plan: approval-workflow-runtime-chain

## Overview

This is an integration/wiring bugfix, not missing logic. The runtime half of the enterprise approval workflow (dispatch → decide → resume → confirm → directory) was built and unit-tested but never connected to the live HTTP/A2A surface. The plan follows the exploratory bugfix workflow:

1. **Explore** (task 1) — write a property-based test BEFORE the fix that exercises every `isBugCondition` branch from the design's Bug Condition specification. This test FAILS on the unfixed code, confirming the wiring gap and surfacing counterexamples.
2. **Preserve** (task 2) — write observation-first property-based tests for non-bug-condition inputs (the design→submit→review→publish half, the four approval modes, owner isolation, `FormValidator`, `EscalationManager`, `Confirm`/`StartTracking`). These PASS on the unfixed code, establishing the baseline to preserve.
3. **Implement** (task 3) — wire the existing mechanisms together at the mechanism level (real dispatcher, registered `RuntimeAPI` with validation, real availability source, implemented reconciliation, decision entry point with approver auth, optimistic locking, recoverable execution, single authoritative publish path, draft-in-place updates, ownership-deny guard, thumbnail route resolved), then re-run the Property 1 and Property 2 tests.
4. **Checkpoint** (task 4) — ensure all tests pass with no regressions.

Property-based testing uses `pgregory.net/rapid` (the project's established Go PBT library). `F` is the system today (runtime half unwired); `F'` is the system after wiring. The fix is correct when, for every input where `isBugCondition(X)` holds, `F'` handles it through an integrated path; and for every other input, `F'(X) = F(X)`.

## Tasks

- [x] 1. Write bug condition exploration test
  - **Property 1: Bug Condition** - Runtime Chain Completes End-to-End
  - **CRITICAL**: This test MUST FAIL on unfixed code - failure confirms the runtime half of the chain is unwired
  - **DO NOT attempt to fix the test or the code when it fails** - the failure is the goal of this task
  - **NOTE**: This test encodes the expected behavior (Property 1 / Requirements 2.1–2.12) - it will validate the fix when it passes after implementation
  - **GOAL**: Surface counterexamples that demonstrate the wiring gap exists on the UNFIXED code
  - **Scoped PBT Approach**: This bug is deterministic per `isBugCondition` branch. Scope the property to concrete failing cases for each branch (kind = ApproverDecision, ApprovalRequestDispatch, Initiate-via-RuntimeAPI, Initiate-form-bypass, AvailabilityCheck, CompletedInstanceWithoutConfirmations, ConcurrentDecision-sameNode, ConfirmEndpointCall, InstanceAccess-emptyRequesterID, ThumbnailFetch, Publish-viaVersionManagerApprove, SaveDraft-updatesExistingDraft) so failures are reproducible
  - Build the router (or the relevant handler with today's wiring) and exercise each `isBugCondition` branch from the design's Bug Condition specification, asserting the integrated-path behavior expected after the fix:
    - `ApproverDecision` (2.1): assert a registered route serves an approver decision into `WorkflowExecutor.ResumeInstance` and the instance advances
    - `ApprovalRequestDispatch` (2.2): assert the dispatcher actually delivers (a spy sender is invoked)
    - `Initiate via RuntimeAPI` (2.3): assert `/api/v1/workflows/{id}/initiate` is routed and validates `form_data` against the published version's form schema; assert schema-violating data is rejected
    - `AvailabilityCheck` (2.4): assert availability mirrors real presence for an offline approver
    - `CompletedInstanceWithoutConfirmations` (2.5): assert `ReconcileOrphanedInstances` creates the missing confirmation records
    - `ConcurrentDecision sameNode` (2.6): assert two near-simultaneous countersign/any-N-of-M decisions both persist (no lost vote)
    - `ConfirmEndpointCall` (2.10): assert `handleConfirm` / `handleListPendingConfirmations` return real `ConfirmationTracker` results (not NOT_IMPLEMENTED)
    - `InstanceAccess requesterID=""` (2.11): assert an instance with empty `requester_id` is denied to an arbitrary caller
    - `ThumbnailFetch` (2.12): assert the advertised `thumbnail_url` is served or not advertised
    - `Publish via VersionManager.Approve` (2.8): assert a workflow published through this path appears in the capability market
    - `SaveDraft updatesExistingDraft` (2.9): assert the update branch does not increase the version-row count
  - The test assertions must match the Expected Behavior Properties (Property 1) from the design
  - Use `pgregory.net/rapid` for property generation; scope generators to the concrete failing cases above for deterministic branches
  - Run test on UNFIXED code
  - **EXPECTED OUTCOME**: Test FAILS (this is correct - it proves the runtime half is unwired)
  - Document counterexamples found (e.g., "approver decision never reaches ResumeInstance; instance stays running", "/initiate returns 404", "handleConfirm returns NOT_IMPLEMENTED", "empty requester_id instance is readable by arbitrary caller") to understand the root cause
  - Mark task complete when test is written, run, and failure is documented
  - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.8, 1.9, 1.10, 1.11, 1.12_

- [x] 2. Write preservation property tests (BEFORE implementing fix)
  - **Property 2: Preservation** - Non-Runtime-Chain Behavior Is Unchanged
  - **IMPORTANT**: Follow observation-first methodology - observe behavior on UNFIXED code for non-bug-condition inputs, then encode the observed behavior as property-based tests
  - Observe behavior on UNFIXED code for inputs where `isBugCondition(X)` is false, and write property-based tests capturing the observed patterns from the Preservation Requirements section:
    - `ResumeInstance` per-mode decision logic (3.2, 3.3): observe and assert `single` advances on approve; `countersign` rejects on first reject and advances when all approve; `any_n_of_m` advances at N and rejects when N unreachable; `sequential` advances in order and rejects on first reject — and the same audit events are emitted
    - `ApprovalDispatcher` interface (3.4): observe that the existing `Dispatch` / `DispatchFallback` interface and executor call sites are unchanged (only the concrete impl is swapped)
    - Existing `/trigger` route + owner isolation + `FormValidator` semantics (3.5): observe `InstanceAPI.handleTriggerWorkflow → TriggerFromMarket` and its `requester_id` injection are unchanged
    - `EscalationManager` + `HandleUnavailable` / `HandleTimeout` / `HandleQueueFull` (3.6): observe escalation logic is unchanged (only availability source changes)
    - `ConfirmationTracker.Confirm` validation + `StartTracking` (3.7, 3.8): observe recipient match, pending status, notes truncation (2000 runes), and terminal-node completion ordering are unchanged
    - Version auto-increment for genuinely new drafts (3.9): observe new drafts still go through `CreateVersion` with minor auto-increment
    - `AdminReviewService.ApproveSubmission` (3.10): observe publish + supersede + market registration + rollback are unchanged
    - Legitimate owner/participant access (3.11): observe access is still granted when ownership is established
    - Conditional `UpdateStatus` contract + node-dispatch / blocking-node semantics (3.12): observe these are honored
    - Design → submit → review → publish half (3.1): observe `WorkflowAPI` CRUD, `VersionManager` lifecycle, `AdminReviewService` review, `MarketService` listing behave as today
  - Property-based testing generates many test cases for stronger preservation guarantees - use `pgregory.net/rapid` to generate non-bug-condition inputs across the input domain
  - Run tests on UNFIXED code
  - **EXPECTED OUTCOME**: Tests PASS (this confirms the baseline behavior to preserve)
  - Mark task complete when tests are written, run, and passing on unfixed code
  - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9, 3.10, 3.11, 3.12_

- [x] 3. Fix for the approval-workflow runtime-chain integration gap

  - [x] 3.1 Wire the decision entry point into ResumeInstance (P0)
    - Add a new `hub/internal/workflow/api_decision.go` handler (or extend `RuntimeAPI`) and register `POST /api/v1/instances/{id}/nodes/{nodeID}/decision` in `router.go` (plus an A2A inbound receiver path that constructs the same call)
    - Extract the authenticated caller via the existing `workflowUserAuth` middleware (`X-Owner-ID`); load the instance + node config; parse an `ApprovalResponse` from the body (`decision`, `rationale`, `matched_rule`) with `ApproverID` = authenticated caller
    - Authorize the caller with the existing `isConfiguredApprover(cfg, approverID)` predicate before calling `WorkflowExecutor.ResumeInstance`; non-approvers receive `403`
    - Leave `ResumeInstance` and the four `process*Mode` handlers unchanged
    - _Bug_Condition: isBugCondition(X) where X.kind = ApproverDecision_
    - _Expected_Behavior: expectedBehavior(result) - decision routes into ResumeInstance only when caller is a configured approver for that node (2.1)_
    - _Preservation: per-mode decision logic and audit events unchanged (3.2, 3.3)_
    - _Requirements: 2.1_

  - [x] 3.2 Provide a real ApprovalDispatcher (P0)
    - Replace `noopApprovalDispatcher` in `hub/internal/httpapi/workflow_noop_deps.go` with a real implementation (e.g. `hubApprovalDispatcher`) backed by the Hub machine sender (`device.Service.SendToMachine`, the mechanism `NotificationDispatcher` / VE messaging already use)
    - Implement the unchanged `ApprovalDispatcher` interface (`Dispatch`, `DispatchFallback`); validate the payload via `ValidateApprovalRequest` and deliver an envelope to the approver machine
    - Leave the executor call sites in `executeApprovalNode` and `handleFallbackRouting` untouched
    - _Bug_Condition: isBugCondition(X) where X.kind = ApprovalRequestDispatch_
    - _Expected_Behavior: expectedBehavior(result) - an approval request is delivered by a real ApprovalDispatcher (2.2)_
    - _Preservation: ApprovalDispatcher interface and existing executor call sites unchanged (3.4)_
    - _Requirements: 2.2_

  - [x] 3.3 Register RuntimeAPI and route validated initiation (P0)
    - In the `router.go` workflow block, construct a `RuntimeExecutor` adapter bridging the 5-arg `RuntimeExecutor.StartInstance(ctx, workflowID, initiatorID, formData, channel)` to the existing 2-arg `WorkflowExecutor.StartInstance` by marshalling `{form_data, initiator_id, channel, submission_timestamp}` into trigger data (mirroring `enrichTriggerDataWithUser` / the `runtime_integration_test.go` convention)
    - Call `NewRuntimeAPI(adapter, instStore, auditStore, NewFormValidator(), wfStore)`, then `SetWithdrawalHandler(NewWithdrawalHandler(...))`, `SetDirectoryService(NewDirectoryService(...))`, and `RegisterRoutes(mux, workflowUserAuth)`
    - Keep the existing `/trigger` route and its owner isolation registered unchanged; `handleInitiateWorkflow` already calls `ExtractFormSchema` + `FormValidator.Validate`, so do not change validator semantics
    - _Bug_Condition: isBugCondition(X) where X.kind = Initiate AND (X.routesThroughRuntimeAPI OR NOT X.formValidatedAgainstSchema)_
    - _Expected_Behavior: expectedBehavior(result) - RuntimeAPI routes registered and validated initiation accepts form_data iff FormValidator.Validate reports no errors against the published version's schema (2.3)_
    - _Preservation: existing /trigger route, owner isolation, and FormValidator semantics unchanged (3.5)_
    - _Requirements: 2.3_

  - [x] 3.4 Source real human-approver availability (P1)
    - Replace `noopAvailabilityChecker` with a real `HumanApproverChecker` backed by `device.Service.IsMachineOnline(approverID)`
    - Implement the unchanged `HumanApproverChecker` interface; leave `EscalationManager` and `HandleUnavailable` / `HandleTimeout` / `HandleQueueFull` untouched
    - _Bug_Condition: isBugCondition(X) where X.kind = AvailabilityCheck_
    - _Expected_Behavior: expectedBehavior(result) - availability mirrors real presence so unavailable/queue-full/timeout conditions can route to fallback/escalation (2.4)_
    - _Preservation: EscalationManager and HandleUnavailable/HandleTimeout/HandleQueueFull logic unchanged; only the availability source changes (3.6)_
    - _Requirements: 2.4_

  - [x] 3.5 Implement ReconcileOrphanedInstances (P1)
    - In `hub/internal/workflow/confirmation_tracker.go` (plus a store query), implement the documented query: completed instances within the 7-day retention window with no rows in `confirmations`
    - Re-derive the terminal node's `TerminalNodeConfig` and call the existing `StartTracking` to create the missing records; run on startup and on a periodic ticker
    - Leave `StartTracking` and `Confirm` validation unchanged
    - _Bug_Condition: isBugCondition(X) where X.kind = CompletedInstanceWithoutConfirmations_
    - _Expected_Behavior: expectedBehavior(result) - reconciliation creates missing confirmation records for orphaned completed instances (2.5)_
    - _Preservation: StartTracking and Confirm validation, terminal-node completion ordering unchanged (3.7, 3.8)_
    - _Requirements: 2.5_

  - [x] 3.6 Serialize concurrent decisions on the same node (P1)
    - In `hub/internal/workflow/executor.go` (`ResumeInstance`) and `InstanceStore` / `PgInstanceStore`, replace the read-modify-write + full `UpdateInstanceData` overwrite with a serialized/atomic apply on the same node
    - Mechanism: optimistic locking via a version/`updated_at` guard on the instance row (conditional `UPDATE ... WHERE version = ?`), retrying the read-modify-write on conflict; or an atomic per-node decision merge in the store
    - Leave the per-mode decision logic in `process*Mode` unchanged - only how `approvalNodeState` is persisted changes
    - _Bug_Condition: isBugCondition(X) where X.kind = ConcurrentDecision AND X.sameNode_
    - _Expected_Behavior: expectedBehavior(result) - concurrent decisions on the same node all persist (2.6)_
    - _Preservation: per-mode decision logic and the conditional UpdateStatus contract unchanged (3.2, 3.3, 3.12)_
    - _Requirements: 2.6_

  - [x] 3.7 Make mid-graph execution recoverable (P1)
    - In `hub/internal/workflow/executor.go` (`executeNode` and node handlers), stop discarding critical write errors: propagate (or at minimum surface and audit) failures from `UpdateCurrentNode`, `UpdateNodeExecution`, status transitions, and audit `Append`
    - Honor the conditional `UpdateStatus` contract so a mid-graph crash leaves a consistent, resumable state; preserve terminal-node ordering
    - _Bug_Condition: isBugCondition(X) where X.kind = MidGraphCrash_
    - _Expected_Behavior: expectedBehavior(result) - critical state/audit writes are not silently dropped; a mid-graph crash leaves resumable, consistent state (2.7)_
    - _Preservation: conditional UpdateStatus contract and terminal-node ordering honored (3.8, 3.12)_
    - _Requirements: 2.7_

  - [x] 3.8 Converge on a single authoritative publish path (P2)
    - In `hub/internal/workflow/version_manager.go`, make `VersionManager.Approve` delegate to / mirror `AdminReviewService.ApproveSubmission`: register in the capability market (with rollback on failure) as part of the same publish operation
    - Leave `AdminReviewService.ApproveSubmission` itself unchanged as the reference path
    - _Bug_Condition: isBugCondition(X) where X.kind = Publish AND X.viaVersionManagerApprove_
    - _Expected_Behavior: expectedBehavior(result) - publishing always registers the workflow in the market with rollback on failure (2.8)_
    - _Preservation: AdminReviewService.ApproveSubmission (publish + supersede + market registration + rollback) unchanged (3.10)_
    - _Requirements: 2.8_

  - [x] 3.9 Update existing drafts in place (P2)
    - In `hub/internal/workflow/version_manager.go` and `WorkflowStore`, add an `UpdateVersion` (graph + version number, status stays `draft`) to the store and have `SaveDraft`'s "update existing draft" branch call it instead of `CreateVersion`
    - Keep genuinely-new drafts going through `CreateVersion` with minor auto-increment
    - _Bug_Condition: isBugCondition(X) where X.kind = SaveDraft AND X.updatesExistingDraft_
    - _Expected_Behavior: expectedBehavior(result) - SaveDraft's update branch updates in place rather than creating a new version row (2.9)_
    - _Preservation: version auto-increment for genuinely new drafts unchanged (3.9)_
    - _Requirements: 2.9_

  - [x] 3.10 Implement the confirm endpoints (P2)
    - In `hub/internal/workflow/api_runtime.go`, implement `handleConfirm`: parse `{notes}`, authenticate the recipient, call `ConfirmationTracker.Confirm(ctx, confirmationID, userID, notes)`, and map sentinel errors (`ErrConfirmationNotFound` → 404, `ErrRecipientMismatch` → 403, `ErrAlreadyConfirmed` → 409) to HTTP codes
    - Implement `handleListPendingConfirmations`: call `ConfirmationStore.ListPending(ctx, userID)` and return the list
    - Leave `Confirm` validation unchanged
    - _Bug_Condition: isBugCondition(X) where X.kind = ConfirmEndpointCall_
    - _Expected_Behavior: expectedBehavior(result) - confirm endpoints return real ConfirmationTracker results instead of NOT_IMPLEMENTED (2.10)_
    - _Preservation: ConfirmationTracker.Confirm validation (recipient match, pending status, notes truncation) unchanged (3.7)_
    - _Requirements: 2.10_

  - [x] 3.11 Deny access on unestablished ownership (P2)
    - In `hub/internal/workflow/api_instance.go` (`handleGetInstance`, `handleGetInstanceAudit`), change the guard from `requesterID != "" && requesterID != userID` (deny) to: grant access only when `requesterID != "" && requesterID == userID` (plus existing participant checks); deny when `requesterID == ""`
    - Ensure legitimate owners/participants still pass
    - _Bug_Condition: isBugCondition(X) where X.kind = InstanceAccess AND X.requesterID = ""_
    - _Expected_Behavior: expectedBehavior(result) - access with unestablished ownership is denied (2.11)_
    - _Preservation: legitimate owner/participant access still granted (3.11)_
    - _Requirements: 2.11_

  - [x] 3.12 Resolve the advertised thumbnail route (P2)
    - Choose one mechanism so the advertised state and the served state agree: either register `GET /api/v1/workflow/{id}/thumbnail` in `router.go`, or stop emitting `thumbnail_url`/`FlowPreviewURL` in `admin_review.go` (`registerInCapabilityMarket` metadata) / `market.go`
    - _Bug_Condition: isBugCondition(X) where X.kind = ThumbnailFetch_
    - _Expected_Behavior: expectedBehavior(result) - the advertised thumbnail URL is either served or not advertised (2.12)_
    - _Preservation: market listing behavior otherwise unchanged (3.1)_
    - _Requirements: 2.12_

  - [x] 3.13 Verify the bug condition exploration test now passes
    - **Property 1: Expected Behavior** - Runtime Chain Completes End-to-End
    - **IMPORTANT**: Re-run the SAME test from task 1 - do NOT write a new test
    - The test from task 1 encodes the expected behavior (Property 1 / Requirements 2.1–2.12); when it passes, it confirms every buggy input is now handled by an integrated path
    - Run the bug condition exploration test from task 1 against the FIXED code
    - **EXPECTED OUTCOME**: Test PASSES (confirms the runtime half is wired - no blocked decision, no no-op dispatch, no NOT_IMPLEMENTED, no lost vote, no IDOR leak, no dead route)
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 2.8, 2.9, 2.10, 2.11, 2.12_

  - [x] 3.14 Verify preservation tests still pass
    - **Property 2: Preservation** - Non-Runtime-Chain Behavior Is Unchanged
    - **IMPORTANT**: Re-run the SAME tests from task 2 - do NOT write new tests
    - Run the preservation property tests from task 2 against the FIXED code
    - **EXPECTED OUTCOME**: Tests PASS (confirms no regressions in the design→submit→review→publish half, the four approval modes, owner isolation, FormValidator semantics, EscalationManager, Confirm/StartTracking)
    - Confirm all existing unit and integration tests in `hub/internal/workflow/**` and `hub/internal/httpapi/**` continue to pass unchanged
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9, 3.10, 3.11, 3.12_

- [x] 4. Checkpoint - Ensure all tests pass
  - Run the full `hub/internal/workflow/**` and `hub/internal/httpapi/**` test suites plus the new Property 1 and Property 2 tests
  - Ensure the bug condition exploration test (task 1) now passes and the preservation tests (task 2) still pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- This is an integration/wiring fix: wherever a shared mechanism is touched, the interface and existing call sites do not change — only the concrete implementation, route registration, or signal source changes (Property 2 / Preservation).
- **Property 1 (Bug Condition)** is the exploration test (task 1): it must FAIL on unfixed code and PASS after the fix (re-run in task 3.13).
- **Property 2 (Preservation)** is the preservation test (task 2): it must PASS on unfixed code and STILL PASS after the fix (re-run in task 3.14).
- `pgregory.net/rapid` is already a project dependency, so no new packages are required. Build/test via `go build ./...` / `go test ./hub/internal/workflow/... ./hub/internal/httpapi/...`.
- The P0 findings (3.1–3.3) are the highest impact — they are why the approval loop dead-ends. P1 (3.4–3.7) are robustness/data-integrity. P2 (3.8–3.12) are drift/hygiene.
- Each implementation sub-task carries `_Bug_Condition_`, `_Expected_Behavior_`, `_Preservation_`, and `_Requirements_` annotations for traceability to the design.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1", "2"] },
    { "id": 1, "tasks": ["3.1", "3.2", "3.3", "3.4", "3.5", "3.6", "3.7", "3.8", "3.9", "3.10", "3.11", "3.12"] },
    { "id": 2, "tasks": ["3.13", "3.14"] },
    { "id": 3, "tasks": ["4"] }
  ]
}
```
