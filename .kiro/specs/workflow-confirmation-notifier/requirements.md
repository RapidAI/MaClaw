# Requirements Document

## Introduction

The enterprise approval-workflow runtime chain (see `.kiro/specs/approval-workflow-runtime-chain/`) was wired into production in `hub/internal/httpapi/router.go`: the `ConfirmationTracker`, its reminder/reconcile loops, the real `HubApprovalDispatcher`, and the real `HubAvailabilityChecker` are all live. One gap remains.

The `NotificationDispatcher` is constructed with **nil notifiers** — `workflow.NewNotificationDispatcher(nil, nil, auditStore, nil)`. Because no `HubInAppNotifier` (and no `IMPushNotifier`) is wired, two runtime delivery paths degrade to "create the record but deliver nothing":

1. **Terminal-node completion notifications** — `WorkflowExecutor.executeTerminalNode` builds `WorkflowNotification`s for each configured result-executor and notifier and calls `NotificationDispatcher.DispatchBatch`. With a nil `hubNotifier`, every `Dispatch` returns `"hub notifier not configured"`, so nothing reaches the recipient.
2. **Confirmation reminders / escalations** — `ConfirmationTracker.RunReminderLoop` → `FindOverdue` → `sendReminder`/`escalateToInitiator` call `NotificationDispatcher.Dispatch`, which fails identically.

This feature builds a **real notifier** that actually delivers these notifications and reminders to recipient machines, using the **same machine-delivery mechanism** the production `HubApprovalDispatcher` already uses (`device.Service.SendToMachine`), with presence sourced from `device.Service.IsMachineOnline` (the same source `HubAvailabilityChecker` uses). The new notifier implements the **existing** `HubInAppNotifier` interface (and optionally `IMPushNotifier`) so that the `NotificationDispatcher`, `ConfirmationTracker`, and `WorkflowExecutor` interfaces and call sites are **unchanged** — only the concrete dependency wired into `NotificationDispatcher` at `router.go` changes from `nil` to a real implementation.

This is a **wiring + new-dependency-implementation** feature, not an interface change. It mirrors the pattern already established by `HubApprovalDispatcher` / `HubAvailabilityChecker` in `hub/internal/httpapi/workflow_runtime_deps.go`.

## Glossary

- **Hub_Notifier**: The new, machine-sender-backed implementation of the existing `workflow.HubInAppNotifier` interface (`Send(ctx, recipientID, *InAppNotification) error`). Delivers workflow notifications and reminders to recipient machines. Lives alongside `HubApprovalDispatcher` in `hub/internal/httpapi`.
- **IM_Push_Notifier**: An OPTIONAL machine-sender-backed implementation of the existing `workflow.IMPushNotifier` interface (`Push(ctx, recipientID, *IMPushMessage) error` and `IsConnected(ctx, recipientID) bool`). Whether this is implemented in this feature is an open question (see Requirement 8 / Open Questions).
- **Notification_Dispatcher**: The existing `workflow.NotificationDispatcher` (`hub/internal/workflow/notification_dispatcher.go`). Fans a `WorkflowNotification` out to `Hub_Notifier.Send` (required) and `IM_Push_Notifier.Push` (optional). Its `Dispatch` / `DispatchBatch` logic is unchanged by this feature.
- **Confirmation_Tracker**: The existing `workflow.ConfirmationTracker` (`hub/internal/workflow/confirmation_tracker.go`). Owns `StartTracking`, `Confirm`, `RunReminderLoop` → `FindOverdue` → `sendReminder` / `escalateToInitiator` / `autoClose`, and `ReconcileOrphanedInstances`. Its reminder cadence and dedup logic are unchanged by this feature.
- **Workflow_Executor**: The existing `workflow.WorkflowExecutor` (`hub/internal/workflow/executor.go`). At a terminal node it calls `Notification_Dispatcher.DispatchBatch`; at a blocked approval node it calls `WorkflowNotifier.NotifyInitiator`.
- **Workflow_Notifier**: The existing `workflow.WorkflowNotifier` interface (`NotifyInitiator(ctx, instanceID, reason, details) error`), used by `Workflow_Executor` for blocked-node initiator notifications. Whether this feature also provides a real implementation is an open question (see Requirement 9 / Open Questions).
- **Machine_Sender**: The Hub machine-delivery mechanism `device.Service.SendToMachine(machineID, msg any) error`. Returns `ErrMachineOffline` when the target machine has no live desktop connection. The `machineCommandSender` interface in `hub/internal/httpapi` abstracts it.
- **Presence_Source**: `device.Service.IsMachineOnline(machineID) bool`. The same source `HubAvailabilityChecker` uses. Abstracted by `machinePresenceChecker` in `hub/internal/httpapi`.
- **Recipient**: A result-executor, notifier, or initiator identified by a machine/user ID — the same identity model used for approvers by `HubApprovalDispatcher`.
- **Audit_Store**: The existing `workflow.AuditStore`; `Notification_Dispatcher` already records delivery-failure audit events through it.
- **Notification_Store**: The existing `workflow.NotificationStore`; `Notification_Dispatcher` records per-channel delivery status (`Delivered`, `FailureReason`) through it when configured.
- **Completion_Notification**: A `WorkflowNotification` of type `result_executor` or `notifier` produced at a terminal node.
- **Confirmation_Reminder**: A `WorkflowNotification` of type `reminder` (or `escalation`) produced by `Confirmation_Tracker`.
- **Router**: The wiring site `hub/internal/httpapi/router.go`, where `Notification_Dispatcher` is currently constructed with `nil` notifiers.

## Requirements

### Requirement 1: Deliver terminal-node completion notifications

**User Story:** As a result-executor or notifier configured on a workflow's terminal node, I want to receive the completion notification on my machine, so that I know an approved workflow needs my action or acknowledgement.

#### Acceptance Criteria

1. THE Hub_Notifier SHALL implement the existing `workflow.HubInAppNotifier` interface method `Send(ctx, recipientID, *InAppNotification) error` without modifying the interface.
2. WHEN Notification_Dispatcher invokes Hub_Notifier.Send for a Completion_Notification AND the Recipient's machine is online, THE Hub_Notifier SHALL make exactly one Machine_Sender call carrying the Completion_Notification payload to the Recipient's machine.
3. WHEN Hub_Notifier delivers a notification through Machine_Sender, THE Hub_Notifier SHALL wrap the payload in a typed machine message consistent with the existing wire-message convention used by HubApprovalDispatcher (a `type`, `ts`, and `payload` envelope).
4. IF the recipient identifier passed to Hub_Notifier.Send is empty, THEN THE Hub_Notifier SHALL return an error and SHALL NOT attempt delivery.
5. IF Hub_Notifier is constructed without a Machine_Sender, THEN THE Hub_Notifier SHALL return an error from Send rather than report success.
6. WHEN a Completion_Notification delivery through Machine_Sender returns no error, THE Hub_Notifier SHALL return a nil error from Send to indicate successful delivery.
7. IF the `*InAppNotification` payload passed to Hub_Notifier.Send is nil, THEN THE Hub_Notifier SHALL return an error and SHALL NOT attempt delivery.

### Requirement 2: Deliver confirmation reminders to non-confirming recipients

**User Story:** As a result-executor or notifier who has not yet confirmed a completed workflow, I want to receive reminder notifications on my machine, so that I am prompted to confirm before the deadline.

#### Acceptance Criteria

1. WHEN Confirmation_Tracker's reminder loop dispatches a Confirmation_Reminder to Hub_Notifier.Send through Notification_Dispatcher AND the Recipient's machine is online, THE Hub_Notifier SHALL deliver exactly one reminder machine message by invoking Machine_Sender once with the Recipient's machine identifier, and SHALL return success from Send only when Machine_Sender accepts the delivery without error.
2. WHEN Confirmation_Tracker dispatches an escalation Confirmation_Reminder to an initiator through Notification_Dispatcher to Hub_Notifier.Send AND the initiator's machine is online, THE Hub_Notifier SHALL deliver exactly one escalation machine message by invoking Machine_Sender once with the initiator's machine identifier, and SHALL return success from Send only when Machine_Sender accepts the delivery without error.
3. THE Hub_Notifier SHALL deliver Completion_Notifications and Confirmation_Reminders through the same Send delivery path, distinguishing them only by a notification-type discriminator field carried in the delivered payload.
4. THE Hub_Notifier SHALL NOT implement its own reminder cadence, reminder counting, reminder-interval gating, or deadline logic, SHALL deliver exactly one machine message per dispatch invocation it receives, and SHALL defer all cadence and counting control to Confirmation_Tracker.
5. IF Machine_Sender reports the Recipient's or initiator's machine is offline when Hub_Notifier dispatches a Confirmation_Reminder or escalation, THEN THE Hub_Notifier SHALL return an error identifying the offline condition to Notification_Dispatcher and SHALL NOT report success (the action taken beyond recording the failure remains the open decision in Requirement 3).

### Requirement 3: Handle offline recipients

**User Story:** As an operator, I want delivery to a Recipient whose machine is offline to be handled predictably, so that an offline Recipient does not cause undefined behavior or lost workflow progress.

#### Acceptance Criteria

1. IF Machine_Sender reports the Recipient's machine is offline when Hub_Notifier attempts delivery, THEN THE Hub_Notifier SHALL return an error that is distinguishable as the offline condition (wrapping or matching Machine_Sender's `ErrMachineOffline`) to Notification_Dispatcher AND SHALL NOT report success.
2. WHERE a Notification_Store is wired into Notification_Dispatcher, WHEN Hub_Notifier returns an offline error for a Completion_Notification, THE Notification_Dispatcher SHALL record the failed delivery in Notification_Store and append a delivery-failure event to Audit_Store using its existing failure-recording behavior.
3. WHEN a Completion_Notification delivery returns an offline error at a terminal node, THE Workflow_Executor SHALL keep the workflow instance in its completed state and SHALL NOT mark the instance as failed, using its existing terminal-node behavior.
4. WHERE a Presence_Source is available, THE Hub_Notifier SHALL be able to obtain a boolean online/offline result by querying Presence_Source (`device.Service.IsMachineOnline`) prior to or as part of a delivery attempt.
5. ⚠️ 待确认: The action taken for an offline Recipient beyond recording the failure — skip (the Recipient relies on the next reminder tick or the Hub UI), queue for re-delivery on reconnect, or retry on a schedule — is an open decision (see Open Questions).

### Requirement 4: Record delivery attempts in the audit trail and notification store

**User Story:** As an auditor, I want every notification delivery attempt and its outcome recorded, so that I can verify whether a Recipient was notified.

#### Acceptance Criteria

1. WHEN Hub_Notifier.Send returns a nil error for a delivery, THE Notification_Dispatcher SHALL treat the delivery as successful for its recording logic.
2. WHERE a Notification_Store is wired into Notification_Dispatcher, WHEN a delivery through Hub_Notifier succeeds, THE Notification_Dispatcher SHALL record the successful per-channel delivery status in Notification_Store using its existing recording behavior.
3. WHERE a Notification_Store is wired into Notification_Dispatcher, WHEN a delivery through Hub_Notifier returns a non-nil error, THE Notification_Dispatcher SHALL record the failure reason in Notification_Store using its existing recording behavior.
4. WHEN a delivery through Hub_Notifier returns a non-nil error, THE Notification_Dispatcher SHALL append a delivery-failure event to Audit_Store using its existing recording behavior.
5. THE Hub_Notifier SHALL surface delivery outcomes as the return value of Send (nil on success, non-nil on failure) rather than suppressing them, so that Notification_Dispatcher's audit and notification-store recording is driven by accurate outcomes.
6. ⚠️ 待确认: Whether successful deliveries are auditable when no Notification_Store is wired (today the Router passes nil) depends on Open Question 2 (Notification_Store wiring).

### Requirement 5: Avoid spamming recipients with duplicate reminders

**User Story:** As a Recipient, I want to not receive duplicate reminder messages for the same pending confirmation within a single reminder window, so that the notifications remain useful.

#### Acceptance Criteria

1. THE Hub_Notifier SHALL delegate all reminder deduplication, reminder-interval timing, and reminder counting to Confirmation_Tracker's existing reminder-interval gate (`shouldSendReminder`) and reminder counter (`IncrementReminders`, `MaxReminders`).
2. THE Hub_Notifier SHALL NOT maintain its own reminder-deduplication state — specifically no per-confirmation sent-timestamp cache, no reminder counter, and no already-notified Recipient set — and SHALL deliver each Send invocation it receives from Notification_Dispatcher as exactly one delivery attempt.
3. WHEN Confirmation_Tracker's reminder-interval gate (`shouldSendReminder`) suppresses a reminder for a pending confirmation on a reminder-loop tick, THE Confirmation_Tracker SHALL NOT dispatch that reminder through Notification_Dispatcher, so that Hub_Notifier.Send is not invoked for that confirmation on that tick.
4. IF a pending confirmation's reminder count has reached Confirmation_Tracker's `MaxReminders` limit, THEN THE Confirmation_Tracker SHALL NOT dispatch a further Confirmation_Reminder for that confirmation through Notification_Dispatcher, so that Hub_Notifier.Send is not invoked for an additional reminder for that confirmation.
5. WHEN Confirmation_Tracker's reminder-interval gate permits a reminder for a pending confirmation on a reminder-loop tick, THE Hub_Notifier SHALL deliver exactly one reminder payload to each non-confirming Recipient for that confirmation on that tick.
6. ⚠️ 待确认: Whether Hub_Notifier should additionally collapse multiple notifications to the same Recipient within a short interval at the delivery layer is an open decision (see Open Questions).

### Requirement 6: Handle delivery failures without halting the runtime chain

**User Story:** As an operator, I want a delivery failure to one Recipient to not block delivery to other Recipients or stall workflow completion, so that one offline or unreachable Recipient does not break the batch.

#### Acceptance Criteria

1. WHEN Notification_Dispatcher.DispatchBatch processes a batch of N Completion_Notifications (N ≥ 1) addressed to distinct Recipients AND Hub_Notifier.Send returns an error for one or more of those Recipients, THE Notification_Dispatcher SHALL attempt delivery to every Recipient in the batch without aborting on the first failure, such that the count of Hub_Notifier.Send attempts equals N, using its existing batch behavior.
2. WHEN one or more Completion_Notification deliveries fail at a terminal node, THE Workflow_Executor SHALL keep the workflow instance in its completed state and SHALL NOT roll back, re-open, or mark the instance as failed, using its existing terminal-node behavior.
3. WHEN one or more Completion_Notification deliveries fail at a terminal node, THE Workflow_Executor SHALL NOT propagate the dispatch error in any way that aborts or reverses terminal-node completion.
4. IF Machine_Sender returns a send failure that is not the offline condition handled by Requirement 3 (for example a full send buffer or other transient transport error), THEN THE Hub_Notifier SHALL return that error to Notification_Dispatcher for recording AND SHALL NOT report success.
5. IF every Hub_Notifier.Send attempt in a terminal-node Completion_Notification batch returns an error, THEN THE Workflow_Executor SHALL keep the instance in its completed state AND SHALL complete terminal-node processing without stalling, using its existing terminal-node behavior.

### Requirement 7: Wire the real notifier into the router

**User Story:** As a Hub operator, I want the production router to construct the Notification_Dispatcher with the real notifier instead of nil, so that completion notifications and reminders are actually delivered in production.

#### Acceptance Criteria

1. WHEN the Router wires the production workflow runtime chain, THE Router SHALL construct Hub_Notifier backed by Machine_Sender (`device.Service.SendToMachine`) before constructing Notification_Dispatcher.
2. THE Router SHALL pass Hub_Notifier as the `hubNotifier` argument to `NewNotificationDispatcher`, replacing the current nil value.
3. THE Router SHALL pass the existing Audit_Store as the `auditStore` argument to `NewNotificationDispatcher` so that delivery-failure audit recording remains active.
4. WHERE Hub_Notifier performs presence-based delivery decisions (subject to the offline-recipient decision in Open Questions), THE Router SHALL construct Hub_Notifier with a Presence_Source (`device.Service.IsMachineOnline`) consistent with the source HubAvailabilityChecker uses.
5. WHERE a Notification_Store is wired into Notification_Dispatcher, THE Router SHALL pass it as the `notifStore` argument to `NewNotificationDispatcher` so that per-channel delivery status is persisted.
6. ⚠️ 待确认: Whether the Router wires a Notification_Store instance (today it passes nil) as part of this feature is an open decision (see Open Questions).

### Requirement 8: Preserve existing interfaces and call sites

**User Story:** As a maintainer, I want the new notifier to implement the existing interfaces without changing them, so that the executor, dispatcher, tracker, and all existing tests are unaffected.

#### Acceptance Criteria

1. THE Hub_Notifier SHALL satisfy the existing `workflow.HubInAppNotifier` interface verified by the static assertion `var _ workflow.HubInAppNotifier = (*Hub_Notifier)(nil)`, mirroring the `var _ workflow.ApprovalDispatcher = (*HubApprovalDispatcher)(nil)` pattern.
2. THE feature SHALL NOT change the method set or signatures of the `HubInAppNotifier`, `IMPushNotifier`, `WorkflowNotifier`, `NotificationDispatcher`, or `ConfirmationTracker` types (no method added, removed, renamed, or re-typed).
3. THE feature SHALL keep the `NewNotificationDispatcher(hubNotifier, imPusher, auditStore, notifStore)` constructor's parameter count, order, and types unchanged.
4. THE feature SHALL keep the existing executor and tracker call sites (`NotificationDispatcher.Dispatch`, `NotificationDispatcher.DispatchBatch`, `WorkflowNotifier.NotifyInitiator`, and the tracker reminder/reconcile paths) calling with identical call expressions and arguments.
5. IF an input does not exercise notification delivery, THEN THE Hub system SHALL preserve its current observable outputs and side effects — approval-request dispatch, availability checks, Confirmation_Tracker reminder cadence and state, and audit records — exactly as today.
6. THE existing approval-workflow runtime-chain test suite SHALL continue to pass without modifying any existing test source file.

### Requirement 9: Optional IM push notifier (open scope)

**User Story:** As a Recipient who is reachable over an IM channel, I want to optionally receive workflow notifications over IM push, so that I am reached even when only the IM channel is connected.

#### Acceptance Criteria

1. WHERE the feature implements IM_Push_Notifier, THE IM_Push_Notifier SHALL implement the existing `workflow.IMPushNotifier` interface methods `Push(ctx, recipientID, *IMPushMessage) error` and `IsConnected(ctx, recipientID) bool` without modifying the interface, verified by a static assertion mirroring the `var _ workflow.ApprovalDispatcher = (*HubApprovalDispatcher)(nil)` pattern.
2. WHERE IM_Push_Notifier is implemented, WHEN Notification_Dispatcher invokes IM_Push_Notifier.Push for a Recipient reported connected, THE IM_Push_Notifier SHALL deliver the notification payload and return a nil error on acceptance.
3. WHERE IM_Push_Notifier is implemented, IF the recipient identifier passed to Push is empty, THEN THE IM_Push_Notifier SHALL return an error and SHALL NOT attempt delivery.
4. WHERE IM_Push_Notifier is implemented, IF the Recipient is not connected when Push is invoked, THEN THE IM_Push_Notifier SHALL return an error identifying the offline/not-connected condition and SHALL NOT report success.
5. WHERE IM_Push_Notifier is implemented, THE IM_Push_Notifier.IsConnected SHALL return the Presence_Source (`device.Service.IsMachineOnline`) result for a non-empty recipient and false for an empty recipient.
6. WHERE IM_Push_Notifier is not implemented in this feature, THE Router SHALL pass nil for the `imPusher` argument and THE Notification_Dispatcher SHALL skip IM push without returning a delivery error, using its existing optional-channel behavior.
7. ⚠️ 待确认: Whether IM_Push_Notifier is in scope for this feature, and whether a single machine-sender-backed implementation satisfies both `HubInAppNotifier` and `IMPushNotifier`, is an open decision (see Open Questions).

### Requirement 10: Optional initiator notifier (open scope)

**User Story:** As a workflow initiator, I want to be notified when my instance's approval node is blocked, so that I can intervene.

#### Acceptance Criteria

1. WHERE the feature provides a real Workflow_Notifier, THE Workflow_Notifier SHALL implement the existing `workflow.WorkflowNotifier` interface method `NotifyInitiator(ctx, instanceID, reason, details) error` without modifying the interface, verified by a static assertion mirroring the `var _ workflow.ApprovalDispatcher = (*HubApprovalDispatcher)(nil)` pattern.
2. WHERE the feature provides a real Workflow_Notifier, WHEN Workflow_Executor calls NotifyInitiator for a blocked approval node AND the initiator's machine is online, THE Workflow_Notifier SHALL deliver the blocked-node notification payload to the initiator's machine through Machine_Sender, wrapped in a typed machine message (a `type`, `ts`, and `payload` envelope) consistent with the wire-message convention used by HubApprovalDispatcher.
3. WHERE the feature provides a real Workflow_Notifier, THE Router SHALL pass it to Workflow_Executor via the existing `WithNotifier` option, backed by Machine_Sender (`device.Service.SendToMachine`) and Presence_Source (`device.Service.IsMachineOnline`).
4. WHERE the feature provides a real Workflow_Notifier, IF the initiator identifier resolved from the NotifyInitiator call is empty, OR no Machine_Sender is configured, OR Machine_Sender reports the initiator's machine is offline, THEN THE Workflow_Notifier SHALL return an error (identifying the offline condition when applicable) and SHALL NOT report success, and for an empty identifier it SHALL NOT attempt delivery.
5. IF a blocked-node NotifyInitiator call returns an error, THEN THE Workflow_Executor SHALL treat the dispatch failure as non-fatal and SHALL keep the instance in its existing blocked state without halting the runtime chain.
6. WHERE the feature does not provide a real Workflow_Notifier, THE Workflow_Executor SHALL no-op the blocked-node initiator notification using its existing nil-guard behavior, and SHALL NOT return an error or alter blocked-node handling.
7. ⚠️ 待确认: Whether blocked-node initiator notifications (`WorkflowNotifier.NotifyInitiator`) are in scope for this feature is an open decision (see Open Questions).

## Open Questions / Points to Clarify

These are genuinely ambiguous decisions deliberately left open rather than invented as hard guarantees. They should be resolved before or during design.

1. **Offline-recipient behavior (Requirement 3.4):** When a Recipient is offline at delivery time, should Hub_Notifier (a) skip and rely on the next `Confirmation_Tracker` reminder tick / Hub web UI, (b) queue the notification for re-delivery when the machine reconnects, or (c) retry on a schedule? Today `Machine_Sender` simply returns `ErrMachineOffline` and the failure is recorded; there is no re-delivery queue.

2. **Notification_Store wiring (Requirement 7.6):** `router.go` currently passes nil for `notifStore`. Should this feature also wire a real `NotificationStore` so per-channel delivery status is persisted (and could later back a "redeliver on reconnect" or a Hub-UI inbox), or keep it nil and record only audit-trail failures?

3. **IM push scope (Requirement 9):** Is `IMPushNotifier` in scope now, or is `HubInAppNotifier` (the required channel) sufficient for the first delivery? If both are in scope, does a single machine-sender-backed type implement both interfaces, or are they two distinct types?

4. **Initiator notifier scope (Requirement 10):** Is the blocked-node `WorkflowNotifier.NotifyInitiator` path in scope for this feature, or only the `NotificationDispatcher`-driven completion/reminder paths (the two delivery gaps explicitly called out by the nil-notifier construction)?

5. **Channel semantics vs. transport:** The existing `HubInAppNotifier` is described as "in-app notifications visible on the Hub web UI," while `Machine_Sender` (`SendToMachine`) delivers to a connected desktop machine over WebSocket — the same transport `HubApprovalDispatcher` uses for approval requests. Should the real notifier deliver to the desktop machine (consistent with the approval-request path), to a Hub-web-UI inbox, or both? This determines whether the machine-sender-backed type is the right fit for `HubInAppNotifier` or whether it more naturally maps to `IMPushNotifier`.

6. **Wire message type:** `HubApprovalDispatcher` uses the wire type `ve:approval_request`. Should completion/reminder notifications use an analogous type (for example `ve:workflow_notification`) and a `GroupEnvelope`, or a simpler typed message? This affects the desktop client's handler routing.

7. **Reminder/timeout/max-count defaults:** Reminder interval, max reminders, and timeout are already governed per-confirmation by `Confirmation_Tracker` defaults (`DefaultExecutorReminderInterval`, `DefaultExecutorMaxReminders`, etc.). Confirm that this feature does not need to introduce notifier-level defaults and should defer entirely to those existing values.
