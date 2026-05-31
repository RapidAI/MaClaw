# Approval Workflow Runtime Chain Bugfix Design

## Overview

The enterprise approval workflow (审批工作流) is split into two halves. The **design / submit / review / publish** half — workflow CRUD (`WorkflowAPI`), version lifecycle (`VersionManager`), admin review (`AdminReviewService`), and market listing (`MarketService`) — is wired into `hub/internal/httpapi/router.go` and is live. The **runtime / approval-execution** half — dispatch → decide → resume → confirm → directory — was built and unit-tested (`WorkflowExecutor`, `RuntimeAPI`, `ConfirmationTracker`, `WithdrawalHandler`, `DirectoryService`, `EscalationManager`) but **never integrated** into the live HTTP/A2A surface.

This is an **integration / wiring gap**, not missing business logic. The fix wires the existing mechanisms together at the mechanism level, per the workspace 修复原则 (mechanism-level fixes, not workarounds):

- Provide **real dependencies** where placeholders sit today (`noopApprovalDispatcher`, `noopAvailabilityChecker`).
- **Register the routes** that exist but are never mounted (`RuntimeAPI`).
- **Source real signals** (approver presence, published-version form schema, confirmation store).
- **Serialize concurrent writes** so countersign / any-N-of-M votes are not lost.
- **Stop dropping** critical state/audit writes during mid-graph execution.
- **Converge on one authoritative publish path** (`AdminReviewService.ApproveSubmission`).

The non-negotiable constraint is **regression-free integration**: the already-wired publish half, the four approval modes, owner isolation, `FormValidator` semantics, `EscalationManager`, and `ConfirmationTracker.Confirm` / `StartTracking` must behave exactly as they do today (Unchanged Behavior 3.1–3.12). Wherever a fix touches a shared mechanism, the concrete implementation changes but the interface and the existing call sites do not.

`F` is the system as it exists today (runtime half unwired). `F'` is the system after wiring. The fix is correct when, for every input that exercises the unwired runtime half, `F'` handles it through an integrated path; and for every other input, `F'(X) = F(X)`.

## Glossary

- **Bug_Condition (C)**: The condition that triggers the bug — an event that exercises the unwired runtime half of the chain (no decision entry point, noop dispatcher, unregistered `RuntimeAPI`, form-validation bypass, noop availability, reconcile stub, concurrent-vote race, dropped mid-graph writes, divergent publish, draft duplication, NOT_IMPLEMENTED confirm endpoints, empty-`requester_id` IDOR, dead thumbnail route). Formalized as `isBugCondition(X)` below.
- **Property (P)**: The desired behavior when `isBugCondition(X)` holds — the event is handled by an integrated path (not blocked, not no-op, not NOT_IMPLEMENTED, not a lost vote, not an IDOR leak, not a dead route) while preserving the existing per-mode / validation / audit semantics.
- **Preservation**: For every input where `isBugCondition(X)` is false, `F'(X) = F(X)`. The design→submit→review→publish half, the four approval modes, owner isolation, `FormValidator`, `EscalationManager`, and `ConfirmationTracker` are unchanged.
- **F / F'**: Original (unwired) and fixed (wired) system.
- **WorkflowExecutor**: `hub/internal/workflow/executor.go`. Owns `StartInstance`, `ResumeInstance`, `executeNode`, the four `process*Mode` handlers, and `HandleUnavailable` / `HandleTimeout` / `HandleQueueFull`.
- **ApprovalDispatcher**: Interface in `executor.go` with `Dispatch(ctx, *ApprovalRequest, approverID)` and `DispatchFallback(ctx, *ApprovalRequest, fallbackID, reason)`. Today implemented by `noopApprovalDispatcher` (`hub/internal/httpapi/workflow_noop_deps.go`).
- **HumanApproverChecker**: Interface in `escalation.go` with `IsAvailable(ctx, approverID) bool`. Today implemented by `noopAvailabilityChecker` (always true).
- **RuntimeAPI**: `hub/internal/workflow/api_runtime.go`. Provides `/initiate`, `/withdraw`, `/confirmations`, `/directory/*`. Has `RegisterRoutes`, `SetWithdrawalHandler`, `SetDirectoryService`. Never instantiated in `router.go`.
- **RuntimeExecutor**: Interface in `api_runtime.go` — `StartInstance(ctx, workflowID, initiatorID, formData, channel) (*WorkflowInstance, error)`. Note this 5-arg signature differs from `WorkflowExecutor.StartInstance(ctx, workflowID, triggerData)` (2 args); the wiring needs an adapter.
- **InstanceAPI**: `hub/internal/workflow/api_instance.go`. The live initiation path `handleTriggerWorkflow → WorkflowExecutor.TriggerFromMarket`, which bypasses `FormValidator`. Registered today.
- **ConfirmationTracker**: `confirmation_tracker.go`. `Confirm`, `StartTracking`, `ReconcileOrphanedInstances` (stub), reminder loop.
- **VersionManager / AdminReviewService**: `version_manager.go` / `admin_review.go`. `VersionManager.Approve` publishes without market registration; `AdminReviewService.ApproveSubmission` is the authoritative path (publish + supersede + capability-market registration + rollback).
- **FormValidator**: `form_validator.go`. `Validate(formData, schema)` + `ExtractFormSchema(graph)`.
- **InstanceStore.UpdateStatus**: Documented conditional-update contract (`WHERE status = 'running'`) used for race-safe transitions.

## Bug Details

### Bug Condition

The bug manifests whenever an event exercises the runtime half of the chain — the half that was built and unit-tested but never connected to the live HTTP/A2A surface, given real dependencies, or registered on the router. Concretely: an approver decision has no production caller for `WorkflowExecutor.ResumeInstance`; the executor's dispatcher is `noopApprovalDispatcher`; `RuntimeAPI` routes are unregistered so the live initiation path bypasses `FormValidator`; availability is hardcoded true; reconciliation/confirm endpoints are stubs; concurrent votes race; mid-graph crashes drop state/audit writes; the publish path diverges from the authoritative one; `SaveDraft` duplicates rows; access control leaks on empty `requester_id`; and the advertised thumbnail route does not exist.

**Formal Specification:**
```
FUNCTION isBugCondition(X)
  INPUT: X of type WorkflowEvent
  OUTPUT: boolean

  RETURN
    (X.kind = ApproverDecision)                                  // 1.1 no ResumeInstance caller / decision entry point
    OR (X.kind = ApprovalRequestDispatch)                        // 1.2 noopApprovalDispatcher never delivers
    OR (X.kind = Initiate AND X.routesThroughRuntimeAPI)         // 1.3 RuntimeAPI routes unregistered
    OR (X.kind = Initiate AND NOT X.formValidatedAgainstSchema)  // 1.3 live path bypasses FormValidator
    OR (X.kind = AvailabilityCheck)                              // 1.4 noopAvailabilityChecker always true
    OR (X.kind = CompletedInstanceWithoutConfirmations)          // 1.5 ReconcileOrphanedInstances stub
    OR (X.kind = ConcurrentDecision AND X.sameNode)              // 1.6 vote race (no optimistic locking)
    OR (X.kind = MidGraphCrash)                                  // 1.7 non-transactional exec, dropped writes
    OR (X.kind = Publish AND X.viaVersionManagerApprove)         // 1.8 divergent publish (no market registration)
    OR (X.kind = SaveDraft AND X.updatesExistingDraft)           // 1.9 draft duplication
    OR (X.kind = ConfirmEndpointCall)                            // 1.10 handleConfirm/handleList NOT_IMPLEMENTED
    OR (X.kind = InstanceAccess AND X.requesterID = "")          // 1.11 IDOR on empty requester_id
    OR (X.kind = ThumbnailFetch)                                 // 1.12 dead thumbnail route
END FUNCTION
```

### Examples

- **ApproverDecision (1.1)**: An instance reaches a `single`-mode approval node. The approver attempts to submit "approve". Expected: the decision routes into `ResumeInstance` and the instance advances. Actual: there is no HTTP handler or A2A inbound receiver that calls `ResumeInstance`; the instance blocks forever.
- **ApprovalRequestDispatch (1.2)**: `executeApprovalNode` calls `dispatcher.Dispatch(ctx, req, approverID)`. Expected: the approver is notified. Actual: `noopApprovalDispatcher.Dispatch` returns `nil` without delivering anything.
- **Initiate via RuntimeAPI (1.3)**: A client POSTs `form_data` to `/api/v1/workflows/{id}/initiate`. Expected: routed, validated against the published version's form schema, instance created. Actual: route is never registered (404); the only live path is `/trigger → TriggerFromMarket`, which never calls `FormValidator`.
- **AvailabilityCheck (1.4)**: `EscalationManager.Escalate` asks `checker.IsAvailable(ctx, approverID)` for an offline approver. Expected: false ⇒ queue/retry. Actual: `noopAvailabilityChecker` returns true, so escalation routing never fires.
- **CompletedInstanceWithoutConfirmations (1.5)**: Process crashes in `executeTerminalNode` between `UpdateStatus(completed)` and `StartTracking`. Expected: `ReconcileOrphanedInstances` later creates the missing confirmation records. Actual: it is `return nil`.
- **ConcurrentDecision sameNode (1.6)**: Two approvers of a countersign node respond near-simultaneously. Expected: both decisions recorded. Actual: read-modify-write of `InstanceData` + full `UpdateInstanceData` overwrite ⇒ one vote lost.
- **MidGraphCrash (1.7)**: `StartInstance` recurses through `executeNode`; an audit/status write fails and is ignored (`_ =`). Expected: resumable, consistent state. Actual: `current_node` advances while node-exec/status drift.
- **Publish via VersionManager.Approve (1.8)**: A version is published through `VersionManager.Approve`. Expected: appears in the capability market. Actual: status transitions only; no market registration; diverges from `AdminReviewService.ApproveSubmission`.
- **SaveDraft update branch (1.9)**: `SaveDraft` takes the "update existing draft" branch. Expected: update in place. Actual: a new version row is created every call (acknowledged in the code comment).
- **ConfirmEndpointCall (1.10)**: A recipient POSTs to `/api/v1/confirmations/{id}/confirm`. Expected: `ConfirmationTracker.Confirm` runs. Actual: handler returns `NOT_IMPLEMENTED`.
- **InstanceAccess empty requester_id (1.11)**: `handleGetInstance` for an instance whose `requester_id` is empty. Expected: deny. Actual: guard `requesterID != "" && requesterID != userID` is false ⇒ anyone reads it (IDOR).
- **ThumbnailFetch (1.12)**: Market listing advertises `thumbnail_url = /api/v1/workflow/{id}/thumbnail`. Expected: a served image or no advertised URL. Actual: no route registered ⇒ dead link.

## Expected Behavior

### Preservation Requirements

**Unchanged Behaviors:**
- The design → submit → review → publish half (`WorkflowAPI` CRUD, `VersionManager` lifecycle, `AdminReviewService` review, `MarketService` listing) behaves exactly as today (3.1).
- `ResumeInstance`'s per-mode decision logic — `single` advances on approve; `countersign` rejects on first reject and advances when all approve; `any_n_of_m` advances at N and rejects when N is unreachable; `sequential` advances in order and rejects on first reject — and the audit events it emits are unchanged (3.2, 3.3).
- The `ApprovalDispatcher` interface (`Dispatch` / `DispatchFallback`) and the existing executor call sites are unchanged; only the concrete implementation is swapped (3.4).
- The existing `InstanceAPI.handleTriggerWorkflow → TriggerFromMarket` route and its owner-isolation (`requester_id` injection) continue to work, and `FormValidator` semantics are applied unchanged where validation occurs (3.5).
- `EscalationManager` and `HandleUnavailable` / `HandleTimeout` / `HandleQueueFull` logic are unchanged; only the availability source changes (3.6).
- `ConfirmationTracker.Confirm` validation (recipient match, pending status, notes truncation to 2000 runes) and `StartTracking` are unchanged (3.7).
- Terminal-node completion ordering and `StartTracking` are preserved (3.8).
- Version auto-increment for genuinely new drafts is unchanged (3.9).
- `AdminReviewService.ApproveSubmission` (publish + supersede + market registration + rollback) is unchanged — it becomes the single authoritative path others delegate to (3.10).
- Legitimate owner/participant access to instances and audit trails is still granted; only access with unestablished ownership is denied (3.11).
- The documented conditional `UpdateStatus` contract and the existing node-dispatch / blocking-node (approval) semantics are honored (3.12).

**Scope:**
All inputs where `isBugCondition(X)` is false must be completely unaffected by this fix. This explicitly includes: creating/editing/submitting/reviewing/publishing workflows; the four approval-mode evaluations inside `ResumeInstance`; the existing `/trigger` initiation route and its owner isolation; escalation handling driven by `EscalationManager`; confirmation validation and tracking; and all existing unit and integration tests in `hub/internal/workflow/**` and `hub/internal/httpapi/**`, which must continue to pass unchanged.

**Note:** The actual expected correct behavior for buggy inputs is defined in the Correctness Properties section (Property 1). This section focuses on what must NOT change.

## Hypothesized Root Cause

The defects share a single root cause: **the runtime subsystems were implemented and unit-tested but never connected to the live surface.** The specific wiring omissions, grouped by finding:

1. **No decision entry point (1.1)**: `WorkflowExecutor.ResumeInstance` exists and is unit-tested (`executor_resume_test.go`) but has no production caller. There is no HTTP handler and no A2A inbound receiver that constructs an `ApprovalResponse` and invokes it. The router registers `InstanceAPI` (trigger/get/audit) but no decision route.

2. **Placeholder dependencies (1.2, 1.4)**: `router.go` constructs `dispatcher := &noopApprovalDispatcher{}` and `&noopAvailabilityChecker{}` and passes them to `NewWorkflowExecutor` and `NewEscalationManager`. The real delivery mechanism (`device.Service.SendToMachine`, used elsewhere via the `IMPushNotifier` / machine-sender pattern) and the real presence source (`device.Service.IsMachineOnline`) exist but are not wired.

3. **Unregistered routes + validation bypass (1.3)**: `RuntimeAPI` (`api_runtime.go`) is fully implemented including `handleInitiateWorkflow` (which loads the published version, calls `ExtractFormSchema`, and runs `FormValidator.Validate`) but `NewRuntimeAPI` / `RegisterRoutes` are never called in `router.go`. The only live initiation path, `handleTriggerWorkflow → TriggerFromMarket → StartInstance`, never validates form data. Additionally `RuntimeExecutor.StartInstance` (5-arg) ≠ `WorkflowExecutor.StartInstance` (2-arg), so an adapter is required.

4. **Stubs (1.5, 1.10)**: `ConfirmationTracker.ReconcileOrphanedInstances` is `return nil`; `RuntimeAPI.handleConfirm` / `handleListPendingConfirmations` return `NOT_IMPLEMENTED` even though `ConfirmationTracker.Confirm` and `ConfirmationStore` (`ListPending`, `ListByInstance`) are fully implemented.

5. **Non-serialized / non-transactional writes (1.6, 1.7)**: `ResumeInstance` does read-modify-write on `InstanceData` then a full `UpdateInstanceData` overwrite with no optimistic-locking or per-node serialization; `executeNode` and friends ignore audit/status write errors (`_ =`), so a mid-graph crash leaves inconsistent state.

6. **Divergent publish / draft duplication (1.8, 1.9)**: `VersionManager.Approve` transitions status and supersedes but does not register in the capability market, unlike `AdminReviewService.ApproveSubmission`; `SaveDraft`'s "update existing draft" branch creates a new version row each call (the store has no `UpdateVersion`, only `CreateVersion`).

7. **Access-control gap / dead route (1.11, 1.12)**: The ownership guard treats an empty `requester_id` as "open"; the market metadata advertises a thumbnail URL with no backing route.

## Correctness Properties

Property 1: Bug Condition — Runtime Chain Completes End-to-End

_For any_ event `X` where the bug condition holds (`isBugCondition(X)` returns true), the fixed system `F'` SHALL handle `X` through an integrated path — a registered route, a real dependency, or an implemented handler — such that the event is not blocked, not silently dropped by a no-op, not answered with `NOT_IMPLEMENTED`, not subject to a lost vote, not an IDOR leak, and not a dead route, while preserving the existing per-mode decision logic, `FormValidator` semantics, and audit events. Specifically: an approver decision routes into `ResumeInstance` only when the caller is a configured approver for that node (2.1); an approval request is delivered by a real `ApprovalDispatcher` (2.2); `RuntimeAPI` routes are registered and the validated initiation path accepts `form_data` iff `FormValidator.Validate` reports no errors against the published version's schema (2.3); availability mirrors real presence (2.4); reconciliation creates missing confirmation records for orphaned completed instances (2.5); concurrent decisions on the same node all persist (2.6); critical state/audit writes are not silently dropped (2.7); publishing always registers the workflow in the market with rollback on failure (2.8); `SaveDraft`'s update branch updates in place (2.9); confirm endpoints return real `ConfirmationTracker` results (2.10); access with unestablished ownership is denied (2.11); and the advertised thumbnail URL is either served or not advertised (2.12).

**Validates: Requirements 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 2.8, 2.9, 2.10, 2.11, 2.12**

Property 2: Preservation — Non-Runtime-Chain Behavior Is Unchanged

_For any_ event `X` where the bug condition does NOT hold (`isBugCondition(X)` returns false), the fixed system `F'` SHALL produce the same result as the original system `F`, preserving: the design → submit → review → publish half; the four approval modes' advance/reject/wait semantics and their audit events inside `ResumeInstance`; the `ApprovalDispatcher` interface and existing executor call sites; the existing `/trigger` initiation route, its owner isolation, and `FormValidator` semantics; `EscalationManager` and the `HandleUnavailable` / `HandleTimeout` / `HandleQueueFull` logic; `ConfirmationTracker.Confirm` validation and `StartTracking`; terminal-node completion ordering; version auto-increment for genuinely new drafts; `AdminReviewService.ApproveSubmission`; legitimate owner/participant access; and the conditional `UpdateStatus` contract.

**Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9, 3.10, 3.11, 3.12**

## Fix Implementation

### Changes Required

Assuming the root-cause analysis is correct, the fix is a set of mechanism-level wiring changes. Each finding/expected-behavior pair maps to a concrete change. Wherever a shared mechanism is touched, the interface and existing call sites are preserved (Property 2).

**File**: `hub/internal/httpapi/router.go` (the central wiring point) and supporting files noted below.

1. **Decision entry point → `ResumeInstance` (Finding 1.1 → 2.1)**
   - **File**: new `hub/internal/workflow/api_decision.go` (handler) + registration in `router.go` (or extend `RuntimeAPI`).
   - Add a route `POST /api/v1/instances/{id}/nodes/{nodeID}/decision` (and an A2A inbound receiver path that constructs the same call) that:
     - Extracts the authenticated caller via the existing `workflowUserAuth` middleware (`X-Owner-ID`).
     - Loads the instance + node config, parses an `ApprovalResponse` from the body (`decision`, `rationale`, `matched_rule`), setting `ApproverID` = authenticated caller.
     - **Authorizes** the caller using the existing `isConfiguredApprover(cfg, approverID)` predicate before calling `WorkflowExecutor.ResumeInstance`. Non-approvers receive `403`.
   - `ResumeInstance` and the four `process*Mode` handlers are unchanged (preserves 3.2, 3.3).

2. **Real `ApprovalDispatcher` (Finding 1.2 → 2.2)**
   - **File**: replace `hub/internal/httpapi/workflow_noop_deps.go`'s `noopApprovalDispatcher` with a real implementation (e.g. `hubApprovalDispatcher`) backed by the Hub's machine sender (`device.Service.SendToMachine`, the same mechanism `NotificationDispatcher` / VE messaging already use).
   - It implements the **unchanged** `ApprovalDispatcher` interface (`Dispatch`, `DispatchFallback`); it validates the payload via `ValidateApprovalRequest` and delivers an envelope to the approver machine. Executor call sites in `executeApprovalNode` and `handleFallbackRouting` are untouched (preserves 3.4).

3. **Register `RuntimeAPI` + validated initiation (Finding 1.3 → 2.3)**
   - **File**: `router.go` workflow block.
   - Construct a `RuntimeExecutor` adapter that bridges the 5-arg `RuntimeExecutor.StartInstance(ctx, workflowID, initiatorID, formData, channel)` to the existing 2-arg `WorkflowExecutor.StartInstance` by marshalling `{form_data, initiator_id, channel, submission_timestamp}` into trigger data (mirroring `enrichTriggerDataWithUser` / the `runtime_integration_test.go` convention). 
   - Call `NewRuntimeAPI(adapter, instStore, auditStore, NewFormValidator(), wfStore)`, then `SetWithdrawalHandler(NewWithdrawalHandler(...))`, `SetDirectoryService(NewDirectoryService(...))`, and `RegisterRoutes(mux, workflowUserAuth)`.
   - The existing `/trigger` route and its owner isolation remain registered unchanged (preserves 3.5). `handleInitiateWorkflow` already calls `ExtractFormSchema` + `FormValidator.Validate`; no change to validator semantics.

4. **Real availability (Finding 1.4 → 2.4)**
   - **File**: replace `noopAvailabilityChecker` with a real `HumanApproverChecker` backed by `device.Service.IsMachineOnline(approverID)`.
   - Implements the **unchanged** `HumanApproverChecker` interface; `EscalationManager` and `HandleUnavailable` / `HandleTimeout` / `HandleQueueFull` are untouched (preserves 3.6).

5. **Implement `ReconcileOrphanedInstances` (Finding 1.5 → 2.5)**
   - **File**: `hub/internal/workflow/confirmation_tracker.go` + a store query.
   - Implement the documented query (completed instances within the 7-day retention window with no rows in `confirmations`), re-derive the terminal node's `TerminalNodeConfig`, and call the existing `StartTracking` to create the missing records. Run on startup and on a periodic ticker. `StartTracking` and `Confirm` validation unchanged (preserves 3.7, 3.8).

6. **Serialize concurrent decisions (Finding 1.6 → 2.6)**
   - **File**: `hub/internal/workflow/executor.go` (`ResumeInstance`) + `InstanceStore` / `PgInstanceStore`.
   - Replace the read-modify-write + full-overwrite of `InstanceData` with a serialized/atomic apply on the same node. Mechanism: optimistic locking via a version/`updated_at` guard on the instance row (conditional `UPDATE ... WHERE version = ?`), retrying the read-modify-write on conflict; or an atomic per-node decision merge in the store. The per-mode decision logic in `process*Mode` is unchanged — only how `approvalNodeState` is persisted changes (preserves 3.2, 3.3, 3.12).

7. **Recoverable mid-graph execution (Finding 1.7 → 2.7)**
   - **File**: `hub/internal/workflow/executor.go` (`executeNode` and node handlers).
   - Stop discarding critical write errors: propagate (or at minimum surface and audit) failures from `UpdateCurrentNode`, `UpdateNodeExecution`, status transitions, and audit `Append`, so a mid-graph crash leaves a consistent, resumable state. Honor the conditional `UpdateStatus` contract (preserves 3.12). Terminal-node ordering preserved (preserves 3.8).

8. **Single authoritative publish path (Finding 1.8 → 2.8)**
   - **File**: `hub/internal/workflow/version_manager.go`.
   - Make `VersionManager.Approve` delegate to / mirror `AdminReviewService.ApproveSubmission`: register in the capability market (with rollback on failure) as part of the same publish operation, so any workflow published through either path appears in the market. `AdminReviewService.ApproveSubmission` itself is unchanged and remains the reference (preserves 3.10).

9. **Draft update-in-place (Finding 1.9 → 2.9)**
   - **File**: `hub/internal/workflow/version_manager.go` + `WorkflowStore`.
   - Add an `UpdateVersion` (graph + version number, status stays `draft`) to the store and have `SaveDraft`'s "update existing draft" branch call it instead of `CreateVersion`. Genuinely-new drafts still go through `CreateVersion` with minor auto-increment (preserves 3.9).

10. **Implement confirm endpoints (Finding 1.10 → 2.10)**
    - **File**: `hub/internal/workflow/api_runtime.go`.
    - `handleConfirm`: parse `{notes}`, authenticate the recipient, call `ConfirmationTracker.Confirm(ctx, confirmationID, userID, notes)`, map sentinel errors (`ErrConfirmationNotFound` → 404, `ErrRecipientMismatch` → 403, `ErrAlreadyConfirmed` → 409) to HTTP codes.
    - `handleListPendingConfirmations`: call `ConfirmationStore.ListPending(ctx, userID)` and return the list. `Confirm` validation unchanged (preserves 3.7).

11. **Deny on unestablished ownership (Finding 1.11 → 2.11)**
    - **File**: `hub/internal/workflow/api_instance.go` (`handleGetInstance`, `handleGetInstanceAudit`).
    - Change the guard from `requesterID != "" && requesterID != userID` (deny) to: grant access only when `requesterID != "" && requesterID == userID` (plus existing participant checks); deny when `requesterID == ""`. Legitimate owners/participants still pass (preserves 3.11).

12. **Resolve thumbnail route (Finding 1.12 → 2.12)**
    - **File**: `router.go` (register `GET /api/v1/workflow/{id}/thumbnail`) **or** `admin_review.go` (`registerInCapabilityMarket` metadata) / `market.go` (stop emitting `thumbnail_url`/`FlowPreviewURL`).
    - Choose one mechanism so the advertised state and the served state agree.

## Testing Strategy

### Validation Approach

Two phases. First, surface counterexamples that demonstrate the wiring gap on the **unfixed** code (confirm the root cause). Then verify the fix handles all buggy inputs through integrated paths (Fix Checking) and leaves all non-buggy behavior identical (Preservation Checking). Because the runtime subsystems already have unit tests, the new tests focus on the **integration seams**: route registration, real dependency invocation, decision authorization, validated initiation, concurrency, reconciliation, the publish/market invariant, draft counts, confirm endpoints, ownership denial, and the thumbnail route.

### Exploratory Bug Condition Checking

**Goal**: Surface counterexamples that demonstrate the bug BEFORE implementing the fix. Confirm or refute the root-cause analysis. If refuted, re-hypothesize.

**Test Plan**: Build the router (or the relevant handler with today's wiring) and exercise each `isBugCondition` branch against the UNFIXED code, asserting the defective outcome.

**Test Cases**:
1. **Decision entry point absent (1.1)**: assert no route serves an approver decision into `ResumeInstance` (request → 404 / instance stays running). Will fail to advance on unfixed code.
2. **Noop dispatch (1.2)**: assert `noopApprovalDispatcher.Dispatch` delivers nothing (a spy sender is never invoked). Demonstrates non-delivery.
3. **RuntimeAPI unregistered + form bypass (1.3)**: assert `/api/v1/workflows/{id}/initiate` is 404, and that `/trigger → TriggerFromMarket` accepts schema-violating `form_data`. Will fail (no validation) on unfixed code.
4. **Noop availability (1.4)**: assert `noopAvailabilityChecker.IsAvailable` returns true for an offline approver, so `EscalationManager` never queues.
5. **Reconcile stub (1.5)**: create a completed instance with no confirmations; assert `ReconcileOrphanedInstances` creates nothing.
6. **Vote race (1.6)**: drive two concurrent `ResumeInstance` calls on a countersign node; assert a vote is lost in `InstanceData`.
7. **Confirm NOT_IMPLEMENTED (1.10)**: assert `handleConfirm` / `handleListPendingConfirmations` return `NOT_IMPLEMENTED`.
8. **IDOR (1.11)**: assert an instance with empty `requester_id` is readable by an arbitrary caller.
9. **Dead thumbnail (1.12)**: assert the advertised `thumbnail_url` 404s while the listing still advertises it.
10. **Divergent publish (1.8)** and **draft duplication (1.9)**: assert `VersionManager.Approve` leaves the market without the workflow, and that the `SaveDraft` update branch increases the version-row count.

**Expected Counterexamples**:
- Approver decisions never reach `ResumeInstance`; dispatch delivers nothing; `/initiate` is 404; offline approvers report available; orphaned instances are never repaired; one of two concurrent votes is lost; confirm endpoints are NOT_IMPLEMENTED; empty-`requester_id` instances leak; thumbnail link is dead; published-via-`Approve` workflows are absent from the market; draft rows accumulate.
- Possible causes: placeholder dependencies, unregistered routes, stubbed handlers, non-atomic state writes, divergent publish path.

### Fix Checking

**Goal**: Verify that for all inputs where the bug condition holds, the fixed function produces the expected behavior.

**Pseudocode:**
```
FOR ALL X WHERE isBugCondition(X) DO
  result := F_prime(X)
  ASSERT expectedBehavior(result)
  // i.e. handled by an integrated path: not blocked, not no-op, not NOT_IMPLEMENTED,
  // not a lost vote, not an IDOR leak, not a dead route — and per-mode/validation/audit
  // semantics preserved.
END FOR
```

### Preservation Checking

**Goal**: Verify that for all inputs where the bug condition does NOT hold, the fixed function produces the same result as the original function.

**Pseudocode:**
```
FOR ALL X WHERE NOT isBugCondition(X) DO
  ASSERT F(X) = F_prime(X)
  // design/submit/review/publish, the four approval modes, owner isolation,
  // FormValidator semantics, EscalationManager, Confirm/StartTracking — identical to today.
END FOR
```

**Testing Approach**: Property-based testing is recommended for preservation because it generates many inputs across the non-buggy domain, catches edge cases manual tests miss, and gives strong assurance that behavior is unchanged. Additionally, the **entire existing unit/integration suite** in `hub/internal/workflow/**` and `hub/internal/httpapi/**` must pass unchanged — it is the strongest preservation signal, since the interfaces (`ApprovalDispatcher`, `HumanApproverChecker`, `InstanceStore`, `WorkflowStore`, `ConfirmationStore`) and call sites are held fixed.

**Test Plan**: Observe behavior on UNFIXED code first for non-buggy inputs (per-mode decisions, the `/trigger` route, escalation, confirmation validation, publish via `AdminReviewService`, genuine new-draft auto-increment), then assert the fixed code reproduces it.

**Test Cases**:
1. **Per-mode decision preservation**: observe `single`/`countersign`/`any_n_of_m`/`sequential` advance/reject/wait outcomes and audit events on unfixed code; assert identical after the fix (`executor_resume_test.go` must pass unchanged).
2. **Trigger route + owner isolation preservation**: observe `/trigger → TriggerFromMarket` `requester_id` injection on unfixed code; assert unchanged.
3. **EscalationManager preservation**: observe `HandleUnavailable`/`HandleTimeout`/`HandleQueueFull` fallback routing with the interface held fixed; assert unchanged (only the availability source differs).
4. **Confirm/StartTracking preservation**: observe `ConfirmationTracker.Confirm` validation (recipient match, pending status, 2000-rune truncation) and terminal-node `StartTracking` ordering; assert unchanged.
5. **Authoritative publish preservation**: observe `AdminReviewService.ApproveSubmission` (publish + supersede + market register + rollback) on unfixed code; assert unchanged after `VersionManager.Approve` converges onto it.
6. **New-draft auto-increment preservation**: observe minor-version increment for a genuinely new draft; assert unchanged.

### Unit Tests

- Decision handler: authorization via `isConfiguredApprover` (configured approver → routed; non-approver → 403); body parsing into `ApprovalResponse`.
- Real `ApprovalDispatcher`: `Dispatch`/`DispatchFallback` invoke the machine sender for the target approver; payload validated via `ValidateApprovalRequest`.
- Real availability: `IsAvailable` mirrors `device.Service.IsMachineOnline` (online → true, offline → false).
- `handleConfirm`/`handleListPendingConfirmations`: sentinel-error → HTTP-code mapping; pending list returned.
- Ownership guard: empty `requester_id` → deny for all callers; matching owner → grant.
- `SaveDraft` update branch: version-row count does not increase.
- `ReconcileOrphanedInstances`: creates missing confirmation records only for orphaned completed instances in the retention window.

### Property-Based Tests

- **Property 1 (Fix Checking)**: generate events across the `isBugCondition` branches and assert each is handled by an integrated path with semantics preserved — e.g. for all configured-approver decision tuples the instance advances/rejects per mode; for all `form_data` the `/initiate` accept/reject equals `FormValidator.Validate`; for all (requesterID, callerID) pairs access is granted iff requesterID is non-empty and equals callerID; for all interleavings of two decisions on the same node, both persist.
- **Property 2 (Preservation)**: generate non-buggy inputs and assert `F'(X) = F(X)` for per-mode decisions, the trigger route, escalation, confirmation validation, publish via `AdminReviewService`, and new-draft auto-increment.

### Integration Tests

- Full chain: initiate (validated) → dispatch (real) → decision (routed via authorized entry point) → resume (per mode) → terminal → confirmation tracking → directory views, asserting each seam.
- Concurrency: two near-simultaneous decisions on a countersign / any-N-of-M node end with both votes recorded (no lost update).
- Reconciliation: simulate the `executeTerminalNode` crash window (completed, no confirmations) and assert `ReconcileOrphanedInstances` repairs it and `FindOverdue` subsequently sees the records.
- Publish/market invariant: publishing via the unified `VersionManager.Approve` path registers the workflow in the capability market (queryable via `MarketService.ListWorkflows`), with rollback when market registration fails.
- Thumbnail: the advertised `thumbnail_url` resolves (route served) or the listing omits it — the advertised and served states agree.
