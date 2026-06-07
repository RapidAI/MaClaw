# Implementation Plan: workflow-confirmation-notifier

## Overview

This feature closes the two notification-delivery gaps left by the nil-notifier construction in `hub/internal/httpapi/router.go`. It adds exactly **two production artifacts**:

1. A new file `hub/internal/httpapi/workflow_notifier.go` defining `HubNotifier`, a real implementation of the existing `workflow.HubInAppNotifier` interface backed by the existing `machineCommandSender` / `machinePresenceChecker` abstractions (`device.Service.SendToMachine` / `device.Service.IsMachineOnline`).
2. A one-line change in `hub/internal/httpapi/router.go` wiring `NewHubNotifier(deviceSvc).WithPresence(deviceSvc)` as the first argument to `NewNotificationDispatcher` (args 2 and 4 stay `nil`, arg 3 `auditStore` unchanged).

The implementation is incremental and test-driven: implement `HubNotifier`, prove its 6 correctness properties (Properties 1–4 and 6 at the `Send` level, Property 5 at the `DispatchBatch` fan-out level), wire the router, assert the wiring, then run the existing `hub/internal/workflow` and `hub/internal/httpapi` suites to confirm nothing regressed (Req 8.6).

Language: **Go**. Property library: `pgregory.net/rapid` or `testing/quick` (≥100 iterations per property). Test doubles mirror the existing `capturingMachineSender` in `hub/internal/httpapi/workflow_approval_dispatcher_test.go`. No existing test source file may be modified (Req 8.6).

### Out of scope (intentionally excluded — see design Scope decisions)

- **IM_Push_Notifier** (`workflow.IMPushNotifier`) — Router passes `nil` for `imPusher`; dispatcher already skips IM push when nil (Req 9.6, OQ 3).
- **Blocked-node Workflow_Notifier** (`workflow.WorkflowNotifier.NotifyInitiator`) — executor keeps its existing nil-guard no-op (Req 10.6, OQ 4).
- **Notification_Store wiring** — Router keeps passing `nil` for `notifStore`; no production `NotificationStore` exists (Req 7.6, OQ 2).

No tasks are created for these items beyond this note.

## Tasks

- [x] 1. Implement HubNotifier in `hub/internal/httpapi/workflow_notifier.go`
  - [x] 1.1 Create the HubNotifier type, constructor, presence setter, and Send method
    - Create `hub/internal/httpapi/workflow_notifier.go` in package `httpapi`
    - Add `import "github.com/RapidAI/CodeClaw/hub/internal/device"` so `device.ErrMachineOffline` is matchable via `errors.Is`
    - Define `const workflowNotificationWireType = "ve:workflow_notification"` (mirrors `ve:approval_request`)
    - Define `type HubNotifier struct { sender machineCommandSender; presence machinePresenceChecker }` (presence optional, nil supported)
    - Add the compile-time assertion `var _ workflow.HubInAppNotifier = (*HubNotifier)(nil)` (mirrors `var _ workflow.ApprovalDispatcher = (*HubApprovalDispatcher)(nil)`)
    - Implement `NewHubNotifier(sender machineCommandSender) *HubNotifier` and `WithPresence(p machinePresenceChecker) *HubNotifier` (chaining setter)
    - Implement `Send(ctx, recipientID string, notif *InAppNotification) error`: guard nil sender, nil payload, and empty/whitespace recipient (return error, zero delivery); marshal `notif`; build the `{type, ts, payload}` envelope where `payload` carries `notification_type` (= `notif.Type`) and the marshaled notification; make exactly one `SendToMachine` call; wrap a non-nil send error with `%w` (preserving `device.ErrMachineOffline` for `errors.Is`); hold NO cadence/counting/dedup state
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 2.1, 2.2, 2.3, 2.4, 3.1, 3.4, 5.1, 5.2, 8.1, 8.2_

  - [x]* 1.2 Write example/edge tests for HubNotifier construction and guards
    - In a NEW file `hub/internal/httpapi/workflow_notifier_test.go`
    - Static-assertion compiles (interface satisfied) — example for Req 1.1 / 8.1
    - Nil-sender HubNotifier returns an error from Send and makes zero delivery — example for Req 1.5
    - `WithPresence` presence read returns the source's bool for a non-empty recipient — example for Req 3.4
    - Add the counting/erroring `machineCommandSender` test double (mirrors `capturingMachineSender`) used by all property tests in this file
    - _Requirements: 1.1, 1.5, 3.4, 8.1_

- [x] 2. Property tests for HubNotifier.Send (Properties 1–4, 6)
  - [x]* 2.1 Write property test for single-delivery behavior
    - In `hub/internal/httpapi/workflow_notifier_test.go`
    - **Property 1: One Send invocation makes exactly one delivery to the recipient**
    - Tag comment: `// Feature: workflow-confirmation-notifier, Property 1: One Send invocation makes exactly one delivery to the recipient`
    - ≥100 iterations; generators: non-empty recipient ids (incl. unicode/whitespace-padded), all notification types, payload fields; assert exactly one `SendToMachine` call to the trimmed recipient id
    - **Validates: Requirements 1.2, 2.1, 2.2, 2.4, 5.5**

  - [x]* 2.2 Write property test for the typed envelope and discriminator
    - In `hub/internal/httpapi/workflow_notifier_test.go`
    - **Property 2: Delivered message uses the typed envelope with a faithful type discriminator**
    - Tag comment: `// Feature: workflow-confirmation-notifier, Property 2: Delivered message uses the typed envelope with a faithful type discriminator`
    - ≥100 iterations; assert captured message is `{type, ts, payload}` with `type == ve:workflow_notification`, integer `ts`, and `payload.notification_type == notif.Type`; envelope shape identical across all notification types
    - **Validates: Requirements 1.3, 2.3**

  - [x]* 2.3 Write property test for return-value mirroring with offline distinguishable
    - In `hub/internal/httpapi/workflow_notifier_test.go`
    - **Property 3: Send's return value mirrors the sender outcome, with offline distinguishable**
    - Tag comment: `// Feature: workflow-confirmation-notifier, Property 3: Send's return value mirrors the sender outcome, with offline distinguishable`
    - ≥100 iterations; injected sender outcome ∈ {nil, `device.ErrMachineOffline`, arbitrary error}; assert nil iff outcome nil, `errors.Is(err, device.ErrMachineOffline)` for offline, non-nil for other errors; never reports success on failure
    - **Validates: Requirements 1.6, 2.5, 3.1, 4.5, 6.4**

  - [x]* 2.4 Write property test for invalid-input rejection with zero delivery
    - In `hub/internal/httpapi/workflow_notifier_test.go`
    - **Property 4: Invalid input is rejected with an error and zero delivery attempts**
    - Tag comment: `// Feature: workflow-confirmation-notifier, Property 4: Invalid input is rejected with an error and zero delivery attempts`
    - ≥100 iterations; invalid class ∈ {empty/whitespace recipient, nil payload, nil sender}; assert non-nil error and zero `SendToMachine` calls
    - **Validates: Requirements 1.4, 1.5, 1.7**

  - [x]* 2.5 Write property test for the absence of internal cadence/counting/dedup state
    - In `hub/internal/httpapi/workflow_notifier_test.go`
    - **Property 6: HubNotifier holds no cadence, counting, or dedup state across invocations**
    - Tag comment: `// Feature: workflow-confirmation-notifier, Property 6: HubNotifier holds no cadence, counting, or dedup state across invocations`
    - ≥100 iterations; for any recipient/payload and k ≥ 1, invoking Send k times yields exactly k `SendToMachine` calls (no suppression/collapse/dedup/rate-limit)
    - **Validates: Requirements 2.4, 5.1, 5.2, 5.6**

- [x] 3. Checkpoint - Ensure HubNotifier and its Send-level property tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. Dispatch-level property test and IM-skip example
  - [x]* 4.1 Write property test for batch fan-out attempting delivery to every recipient
    - In a NEW file `hub/internal/httpapi/workflow_notifier_dispatch_test.go`
    - Drive `workflow.NewNotificationDispatcher(hubNotifier, nil, auditStore, nil).DispatchBatch` with `HubNotifier` + the counting sender
    - **Property 5: Batch dispatch attempts delivery to every recipient (attempts == N)**
    - Tag comment: `// Feature: workflow-confirmation-notifier, Property 5: Batch dispatch attempts delivery to every recipient (attempts == N)`
    - ≥100 iterations; generators: N ≥ 1 distinct recipients + a random failing subset; assert exactly N `SendToMachine` attempts, no abort on first failure
    - **Validates: Requirements 6.1**

  - [x]* 4.2 Write example test for the IM-push skip when imPusher is nil
    - In `hub/internal/httpapi/workflow_notifier_dispatch_test.go`
    - With `imPusher == nil`, dispatch delivers through HubNotifier and skips IM push without returning a delivery error (existing optional-channel behavior)
    - _Requirements: 9.6_

- [x] 5. Wire HubNotifier into the production router
  - [x] 5.1 Replace the first nil notifier in `router.go` with the real HubNotifier
    - In `hub/internal/httpapi/router.go`, change `workflow.NewNotificationDispatcher(nil, nil, auditStore, nil)` to construct `hubNotifier := NewHubNotifier(deviceSvc).WithPresence(deviceSvc)` and pass it as arg 1: `workflow.NewNotificationDispatcher(hubNotifier, nil, auditStore, nil)`
    - Keep arg 2 (`imPusher`) nil, arg 3 (`auditStore`) unchanged, arg 4 (`notifStore`) nil
    - Do not change the constructor signature, any existing interface, or any other call site (Req 8.3, 8.4)
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 8.3, 8.4, 9.6, 10.6_

  - [x]* 5.2 Write router wiring assertion test
    - In a NEW file `hub/internal/httpapi/workflow_notifier_router_test.go`
    - Assert the router constructs `NewHubNotifier(deviceSvc).WithPresence(deviceSvc)` and passes it as arg 1 with `auditStore` as arg 3 and `nil` for args 2 and 4 (inspection or constructed-deps check)
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 9.6_

- [x] 6. Final verification - run existing suites to confirm no regression (Req 8.6)
  - Run `go test ./hub/internal/workflow/...` and `go test ./hub/internal/httpapi/...`; confirm all pass with no existing test source file modified.
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional test tasks and can be skipped for a faster MVP; the core implementation tasks (1.1 HubNotifier, 5.1 router wiring) are never optional.
- Each property test is its own sub-task, annotated with its property number and the requirement clauses it validates, and carries the required `// Feature: workflow-confirmation-notifier, Property {number}: {property_text}` tag comment.
- Property tests live close to the implementation (Properties 1–4, 6 next to `HubNotifier`; Property 5 at the dispatch level) so errors are caught early.
- All test artifacts are NEW files only — no existing test source file is modified (Req 8.6).
- Out-of-scope items (IM_Push_Notifier, blocked-node Workflow_Notifier, Notification_Store wiring) are intentionally excluded per the design's scope decisions.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["1.2", "4.1", "5.1"] },
    { "id": 2, "tasks": ["2.1", "4.2", "5.2"] },
    { "id": 3, "tasks": ["2.2"] },
    { "id": 4, "tasks": ["2.3"] },
    { "id": 5, "tasks": ["2.4"] },
    { "id": 6, "tasks": ["2.5"] }
  ]
}
```
!