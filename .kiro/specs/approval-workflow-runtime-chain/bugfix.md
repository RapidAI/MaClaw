# Bugfix Requirements Document

## Introduction

This bugfix addresses an integration/wiring gap in the enterprise approval workflow (审批工作流) implemented under `hub/internal/workflow/*` with its designer in `hub/web/approval_workflow/*`.

A full-chain review (design → submit → download/trigger → execute → approve) found that the **design / submit / review / publish** half of the chain is wired and live, but the **runtime / approval-execution** half was built (with tests) yet **never integrated**. The runtime half — dispatch → decide → resume → confirm → directory — exists as functioning units, but no production code path connects them:

- No dispatcher delivers approval requests to approvers (`noopApprovalDispatcher`).
- No HTTP handler or A2A receiver routes an approver's decision into `WorkflowExecutor.ResumeInstance`.
- The `RuntimeAPI` (initiate/withdraw/confirmations/directory) is never registered in `router.go`, so the only live initiation path bypasses `FormValidator`.
- Availability is hardcoded to "always available" (`noopAvailabilityChecker`), so escalation routing can never fire.
- Reconciliation, confirmation endpoints, optimistic locking, transactional execution, the authoritative publish path, draft-in-place updates, ownership guards, and the advertised thumbnail route are either stubbed, divergent, or missing.

**Root cause:** This is an integration/wiring gap, not missing logic. The runtime subsystems were implemented and unit-tested but never connected to the live HTTP/A2A surface, never given real dependencies, and never registered on the router. The fix must wire the existing mechanisms together at the mechanism level (provide real dependencies, register routes, source real signals, serialize concurrent writes) rather than patch around the symptoms.

**Impact:** Any workflow instance that reaches an approval node blocks forever, because there is no path for an approver to submit a decision and no dispatcher to notify them. The live initiation path skips form validation, orphaned instances are never repaired, concurrent votes can be lost, mid-graph crashes leave inconsistent state, and several published-workflow/market invariants drift.

The defects are grouped by impact: **P0 (Findings 1–3)** are the highest-impact — they are the reason the approval loop dead-ends. **P1 (Findings 4–7)** are robustness and data-integrity defects. **P2 (Findings 8–12)** are drift/hygiene defects.

## Bug Analysis

### Current Behavior (Defect)

**P0 — Approval loop dead-ends (highest impact)**

1.1 WHEN a workflow instance reaches an approval node and an approver attempts to submit a decision THEN the system has no production caller for `WorkflowExecutor.ResumeInstance` (no HTTP handler, no A2A inbound receiver), so the decision cannot be routed into the executor and the instance blocks forever.

1.2 WHEN the executor needs to deliver an approval request to a configured approver THEN the system uses `noopApprovalDispatcher` (`hub/internal/httpapi/workflow_noop_deps.go`), a no-op that never delivers the request, so no approver is ever notified.

1.3 WHEN a workflow instance is initiated THEN the system routes through the only live path `InstanceAPI.handleTriggerWorkflow` → `TriggerFromMarket`, which bypasses `FormValidator`; the `RuntimeAPI` routes (`/initiate`, `/withdraw`, `/confirmations`, `/directory/*`) are never registered in `router.go`, so submitted `form_data` is never validated against the published version's form schema.

**P1 — Robustness / data integrity**

1.4 WHEN the executor checks whether a human approver is available (for unavailable/escalation routing) THEN `noopAvailabilityChecker` always returns true, so `HandleUnavailable` / escalation routing can never be triggered.

1.5 WHEN an instance is marked completed before its confirmation records are created (the crash window documented in `executeTerminalNode`) THEN `ConfirmationTracker.ReconcileOrphanedInstances` is a `return nil` stub, so the orphaned completed instance is never found by `FindOverdue` and never repaired.

1.6 WHEN two approver responses for a countersign or any-N-of-M node arrive nearly simultaneously THEN the system performs read-modify-write on `InstanceData` followed by a full overwrite via `UpdateInstanceData` with no optimistic locking, so one of the two votes can be lost.

1.7 WHEN `StartInstance` recurses through `executeNode` in the request goroutine and a crash occurs mid-graph THEN status/audit write errors are ignored (`_ =`), leaving `current_node` advanced but node-exec/status inconsistent with no resume path.

**P2 — Drift / hygiene**

1.8 WHEN a version is published via `VersionManager.Approve` THEN it transitions/supersedes status but does NOT register the workflow in the capability market (and has no market rollback), diverging from `AdminReviewService.ApproveSubmission` which does both, so a workflow published through this path does not appear in the market.

1.9 WHEN `VersionManager.SaveDraft` takes its "update existing draft" branch THEN it actually CREATES a new version every call (acknowledged in the code comment), accumulating orphan draft rows that pollute `ListVersions` / `findLatestVersion`.

1.10 WHEN a confirmation is submitted or pending confirmations are listed via `RuntimeAPI.handleConfirm` / `handleListPendingConfirmations` THEN the system returns NOT_IMPLEMENTED even though `ConfirmationTracker.Confirm` and the store are fully implemented.

1.11 WHEN `handleGetInstance` / audit access is checked THEN the guard is `requesterID != "" && requesterID != userID`, so an instance with an empty `requester_id` is readable by anyone (IDOR).

1.12 WHEN the market listing advertises `thumbnail_url = /api/v1/workflow/{id}/thumbnail` THEN no such route is registered, so the preview link is dead.

### Expected Behavior (Correct)

**P0 — Approval loop dead-ends (highest impact)**

2.1 WHEN an approver submits a decision THEN the system SHALL provide an integrated path (an HTTP endpoint and/or an A2A inbound receiver) that routes the decision into `WorkflowExecutor.ResumeInstance`, with auth that verifies the caller is a configured approver for that node, so an instance reaching an approval node can be advanced or rejected.

2.2 WHEN the executor needs to deliver an approval request to a configured approver THEN the system SHALL use a real `ApprovalDispatcher` implementation that delivers the request (e.g., via Hub A2A / VE messaging) to the approver.

2.3 WHEN a workflow instance is initiated THEN the system SHALL register the `RuntimeAPI` routes (with `WithdrawalHandler` and `DirectoryService` set) and the live initiation path SHALL validate submitted `form_data` against the published version's form schema using `FormValidator` before creating the instance.

**P1 — Robustness / data integrity**

2.4 WHEN the executor checks whether a human approver is available THEN the system SHALL source availability from real presence information, so unavailable/queue-full/timeout conditions can route to fallback or escalation.

2.5 WHEN an instance was marked completed before its confirmation records were created THEN `ConfirmationTracker.ReconcileOrphanedInstances` SHALL actually find completed instances that have no confirmation records and repair them (create the missing confirmation records).

2.6 WHEN two approver responses for a countersign or any-N-of-M node arrive nearly simultaneously THEN the system SHALL serialize/atomically apply the decisions on the same node so that no vote is lost.

2.7 WHEN execution advances through nodes THEN the system SHALL advance state consistently and recoverably, and critical state/audit writes SHALL NOT be silently dropped, so a mid-graph crash leaves resumable, consistent state.

**P2 — Drift / hygiene**

2.8 WHEN a version is published THEN the system SHALL use a single authoritative publish path so that a published workflow always appears in the capability market (with rollback on failure).

2.9 WHEN `VersionManager.SaveDraft` updates an existing draft THEN it SHALL update the existing draft version in place rather than create a new version row.

2.10 WHEN a confirmation is submitted or pending confirmations are listed THEN `RuntimeAPI.handleConfirm` / `handleListPendingConfirmations` SHALL call the implemented `ConfirmationTracker` (and store) and return the real result instead of NOT_IMPLEMENTED.

2.11 WHEN instance/audit access is checked and ownership cannot be established (e.g., empty `requester_id`) THEN the system SHALL deny access rather than allow it.

2.12 WHEN the market listing advertises a thumbnail URL THEN the system SHALL either register the `/api/v1/workflow/{id}/thumbnail` route or stop advertising the dead URL.

### Unchanged Behavior (Regression Prevention)

The fix is an integration/wiring change. The following existing behaviors MUST be preserved exactly.

3.1 WHEN an instance progresses through the existing design → submit → review → publish flow THEN the system SHALL CONTINUE TO behave exactly as before (this half of the chain is already wired and must not regress).

3.2 WHEN `ResumeInstance` processes a decision in any mode (single / countersign / any_n_of_m / sequential) THEN the system SHALL CONTINUE TO apply the existing per-mode decision logic and emit the same audit events.

3.3 WHEN any of the four approval modes evaluates approver decisions THEN the system SHALL CONTINUE TO use the existing advance/reject/wait semantics for that mode (single advances on approve; countersign rejects on first reject and advances when all approve; any-N-of-M advances at N and rejects when N is unreachable; sequential advances in order and rejects on first reject).

3.4 WHEN the executor calls the dispatcher THEN the system SHALL CONTINUE TO use the existing `ApprovalDispatcher` interface (`Dispatch` / `DispatchFallback`) and the existing executor call sites unchanged (only the concrete implementation is replaced).

3.5 WHEN an instance is initiated via the existing `InstanceAPI.handleTriggerWorkflow` trigger route THEN the system SHALL CONTINUE TO honor that route and its owner-isolation behavior, and SHALL CONTINUE TO apply `FormValidator` semantics unchanged.

3.6 WHEN unavailability, timeout, or queue-full conditions are handled THEN the system SHALL CONTINUE TO use the existing `EscalationManager` and the existing `HandleUnavailable` / `HandleTimeout` / `HandleQueueFull` logic (only the availability source changes).

3.7 WHEN a confirmation is submitted THEN the system SHALL CONTINUE TO apply the existing `ConfirmationTracker.Confirm` validation (recipient match, pending status, notes truncation) and `StartTracking` behavior.

3.8 WHEN a workflow reaches a terminal node THEN the system SHALL CONTINUE TO preserve the existing terminal-node completion ordering and `StartTracking` behavior.

3.9 WHEN a genuinely new draft is saved THEN the system SHALL CONTINUE TO apply the existing version auto-increment rules.

3.10 WHEN a submission is approved via `AdminReviewService.ApproveSubmission` THEN the system SHALL CONTINUE TO publish, supersede the previous published version, register in the capability market, and roll back on failure exactly as it does today.

3.11 WHEN a legitimate owner or participant accesses an instance or its audit trail THEN the system SHALL CONTINUE TO grant access (only access with unestablished ownership is denied).

3.12 WHEN concurrent decisions are serialized on a node THEN the system SHALL CONTINUE TO honor the documented conditional `UpdateStatus` contract and the existing node dispatch and blocking-node (approval) semantics.

## Deriving the Bug Condition

From the requirements above, the runtime-chain integration gap can be expressed with a single bug-condition predicate over the live system surface. **F** is the system as it exists today (runtime half unwired); **F'** is the system after wiring the runtime half.

```pascal
FUNCTION isBugCondition(X)
  INPUT: X of type WorkflowEvent
  OUTPUT: boolean

  // X triggers the bug when it exercises the unwired runtime half of the chain.
  RETURN
    (X.kind = ApproverDecision)                                  // 1.1 no ResumeInstance caller
    OR (X.kind = ApprovalRequestDispatch)                        // 1.2 noop dispatcher
    OR (X.kind = Initiate AND X.routesThroughRuntimeAPI)         // 1.3 RuntimeAPI unregistered
    OR (X.kind = Initiate AND NOT X.formValidatedAgainstSchema)  // 1.3 form bypass
    OR (X.kind = AvailabilityCheck)                              // 1.4 noop availability
    OR (X.kind = CompletedInstanceWithoutConfirmations)          // 1.5 reconcile stub
    OR (X.kind = ConcurrentDecision AND X.sameNode)              // 1.6 vote race
    OR (X.kind = MidGraphCrash)                                  // 1.7 non-transactional exec
    OR (X.kind = Publish AND X.viaVersionManagerApprove)         // 1.8 divergent publish
    OR (X.kind = SaveDraft AND X.updatesExistingDraft)           // 1.9 draft duplication
    OR (X.kind = ConfirmEndpointCall)                            // 1.10 NOT_IMPLEMENTED
    OR (X.kind = InstanceAccess AND X.requesterID = "")          // 1.11 IDOR
    OR (X.kind = ThumbnailFetch)                                 // 1.12 dead route
END FUNCTION
```

```pascal
// Property: Fix Checking — the runtime chain completes end-to-end for buggy inputs.
FOR ALL X WHERE isBugCondition(X) DO
  result ← F'(X)
  ASSERT result is handled by an integrated path (not blocked, not no-op,
         not NOT_IMPLEMENTED, not a lost vote, not an IDOR leak, not a dead route)
         AND existing per-mode / validation / audit semantics are preserved
END FOR
```

```pascal
// Property: Preservation Checking — non-runtime-chain behavior is unchanged.
FOR ALL X WHERE NOT isBugCondition(X) DO
  ASSERT F(X) = F'(X)   // design/submit/review/publish, per-mode logic, owner isolation,
                        // FormValidator semantics, EscalationManager, Confirm/StartTracking,
                        // the four approval modes — all identical to today.
END FOR
```

**Key definitions:**
- **F**: the original system — the runtime half of the chain is built but unwired (noop dispatcher, unregistered RuntimeAPI, noop availability, reconcile stub, NOT_IMPLEMENTED endpoints, no decision entry point).
- **F'**: the fixed system — the runtime half is integrated (real dispatcher, registered RuntimeAPI with validation, real availability source, implemented reconciliation, decision entry point with approver auth, optimistic locking, recoverable execution, single authoritative publish path, draft-in-place updates, ownership-deny guard, thumbnail route resolved).
