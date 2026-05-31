package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"
)

// Sentinel errors for Confirm method.
var (
	ErrConfirmationNotFound = errors.New("confirmation not found")
	ErrRecipientMismatch    = errors.New("user is not the recipient of this confirmation")
	ErrAlreadyConfirmed     = errors.New("confirmation is not pending")
)

// Reconciliation tuning for orphaned completed instances (the crash window
// documented in executeTerminalNode). The window mirrors the documented
// 7-day retention; the interval governs the periodic ticker.
const (
	orphanReconcileRetention = 7 * 24 * time.Hour
	orphanReconcileInterval  = time.Hour
)

// OrphanedInstanceFinder is an OPTIONAL capability an InstanceStore may
// implement to support reconciliation of completed instances that have no
// confirmation records (the crash window documented in executeTerminalNode:
// an instance is marked completed before StartTracking creates its records).
//
// It is intentionally NOT part of the InstanceStore interface so that the
// many test mocks of InstanceStore do not need to implement it; the production
// stores (PgInstanceStore and the sqlite instanceStore) satisfy it, and
// ReconcileOrphanedInstances type-asserts for it at runtime.
type OrphanedInstanceFinder interface {
	// FindCompletedWithoutConfirmations returns completed instances whose
	// completed_at is within the given retention window and that have no rows
	// in the confirmations table (i.e. orphaned by a crash between completion
	// and StartTracking).
	FindCompletedWithoutConfirmations(ctx context.Context, within time.Duration) ([]WorkflowInstance, error)
}

// ConfirmationTracker manages post-completion confirmation lifecycle:
// tracking pending confirmations, sending reminders, and escalating on timeout.
// It operates as a decoupled subsystem — once a workflow reaches a terminal node,
// the executor hands off to ConfirmationTracker which manages reminders,
// escalation, and auto-close independently.
type ConfirmationTracker struct {
	store           ConfirmationStore
	instanceStore   InstanceStore
	notifDispatcher *NotificationDispatcher
	auditStore      AuditStore
	ticker          *time.Ticker // runs every 5 minutes to check deadlines
	// workflowStore is OPTIONAL and used only by ReconcileOrphanedInstances to
	// re-derive a completed instance's terminal-node TerminalNodeConfig from its
	// published version graph. It is injected via SetWorkflowStore so the
	// existing 4-arg NewConfirmationTracker constructor (and its callers) are
	// unchanged.
	workflowStore WorkflowStore
	reconcileTick *time.Ticker // runs periodically to repair orphaned completed instances
}

// NewConfirmationTracker creates a new ConfirmationTracker with the given dependencies.
// The ticker is NOT initialized here — it is lazily created in RunReminderLoop to avoid
// leaking a goroutine when the tracker is created but RunReminderLoop is never called
// (e.g., during testing or when the feature is disabled).
func NewConfirmationTracker(
	store ConfirmationStore,
	instanceStore InstanceStore,
	notifDispatcher *NotificationDispatcher,
	auditStore AuditStore,
) *ConfirmationTracker {
	return &ConfirmationTracker{
		store:           store,
		instanceStore:   instanceStore,
		notifDispatcher: notifDispatcher,
		auditStore:      auditStore,
		// ticker is nil — created lazily in RunReminderLoop
	}
}

// SetWorkflowStore injects the optional WorkflowStore used by
// ReconcileOrphanedInstances to re-derive a completed instance's terminal-node
// configuration. It is a setter (rather than a constructor argument) so the
// existing 4-arg NewConfirmationTracker signature and all its callers remain
// unchanged. Returns the tracker for chaining.
func (ct *ConfirmationTracker) SetWorkflowStore(store WorkflowStore) *ConfirmationTracker {
	ct.workflowStore = store
	return ct
}

// StartTracking creates confirmation records for all executors and notifiers
// configured on the terminal node. Each executor and notifier gets a pending
// confirmation record with the configured timeout and reminder settings.
func (ct *ConfirmationTracker) StartTracking(ctx context.Context, inst *WorkflowInstance, terminalNodeConfig *TerminalNodeConfig) error {
	if terminalNodeConfig == nil {
		return nil
	}

	for _, exec := range terminalNodeConfig.ResultExecutors {
		conf := &Confirmation{
			InstanceID:            inst.ID,
			RecipientID:           exec.UserID,
			Type:                  ConfirmTypeExecutor,
			Status:                ConfirmPending,
			TimeoutHours:          exec.TimeoutHours,
			MaxReminders:          exec.MaxReminders,
			ReminderIntervalHours: exec.ReminderInterval,
		}
		if conf.TimeoutHours == 0 {
			conf.TimeoutHours = DefaultExecutorTimeoutHours
		}
		if conf.MaxReminders == 0 {
			conf.MaxReminders = DefaultExecutorMaxReminders
		}
		if conf.ReminderIntervalHours == 0 {
			conf.ReminderIntervalHours = DefaultExecutorReminderInterval
		}
		if err := ct.store.Create(ctx, conf); err != nil {
			return fmt.Errorf("create executor confirmation for %s: %w", exec.UserID, err)
		}
	}

	for _, notif := range terminalNodeConfig.Notifiers {
		conf := &Confirmation{
			InstanceID:            inst.ID,
			RecipientID:           notif.UserID,
			Type:                  ConfirmTypeNotifier,
			Status:                ConfirmPending,
			TimeoutHours:          notif.TimeoutHours,
			MaxReminders:          notif.MaxReminders,
			ReminderIntervalHours: notif.ReminderInterval,
		}
		if conf.TimeoutHours == 0 {
			conf.TimeoutHours = DefaultNotifierTimeoutHours
		}
		if conf.MaxReminders == 0 {
			conf.MaxReminders = DefaultNotifierMaxReminders
		}
		if conf.ReminderIntervalHours == 0 {
			conf.ReminderIntervalHours = DefaultNotifierReminderInterval
		}
		if err := ct.store.Create(ctx, conf); err != nil {
			return fmt.Errorf("create notifier confirmation for %s: %w", notif.UserID, err)
		}
	}

	return nil
}

// Confirm records a confirmation from an executor or notifier.
// It validates that the confirmation exists and the user is the intended recipient,
// then updates the status to confirmed. For executors, notes (max 2000 runes) are recorded.
func (ct *ConfirmationTracker) Confirm(ctx context.Context, confirmationID, userID, notes string) error {
	conf, err := ct.store.Get(ctx, confirmationID)
	if err != nil {
		return fmt.Errorf("get confirmation: %w", err)
	}
	if conf == nil {
		return ErrConfirmationNotFound
	}
	if conf.RecipientID != userID {
		return ErrRecipientMismatch
	}
	if conf.Status != ConfirmPending {
		return ErrAlreadyConfirmed
	}

	// Truncate notes to 2000 runes for executors.
	if conf.Type == ConfirmTypeExecutor {
		runes := []rune(notes)
		if len(runes) > 2000 {
			notes = string(runes[:2000])
		}
	}
	// Notifiers don't store notes.
	if conf.Type == ConfirmTypeNotifier {
		notes = ""
	}

	if err := ct.store.UpdateStatus(ctx, confirmationID, ConfirmConfirmed, notes); err != nil {
		return fmt.Errorf("update confirmation status: %w", err)
	}

	// Record audit trail event.
	if ct.auditStore != nil {
		eventType := "executor_confirmed"
		if conf.Type == ConfirmTypeNotifier {
			eventType = "notifier_acknowledged"
		}
		_ = ct.auditStore.Append(ctx, &AuditEntry{
			InstanceID: conf.InstanceID,
			EventType:  eventType,
			ActorID:    userID,
			Details:    notes,
			Timestamp:  time.Now().UTC().Truncate(time.Millisecond),
		})
	}

	return nil
}

// RunReminderLoop is a background goroutine that checks for overdue confirmations
// and sends reminders or triggers escalation.
//
// Logic per overdue confirmation:
//   - If reminders_sent < max_reminders AND time since last reminder >= reminder_interval_hours:
//     → Send reminder notification, increment reminders_sent
//   - If reminders_sent >= max_reminders:
//     → For executor: escalate to initiator, record escalation_triggered
//     → For notifier: auto-close, record auto_closed with reason "notifier_timeout"
func (ct *ConfirmationTracker) RunReminderLoop(ctx context.Context) {
	ct.ticker = time.NewTicker(5 * time.Minute)
	defer ct.ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ct.ticker.C:
			ct.processOverdueConfirmations(ctx)
		}
	}
}

// Stop stops the internal tickers. Should be called when shutting down.
func (ct *ConfirmationTracker) Stop() {
	if ct.ticker != nil {
		ct.ticker.Stop()
	}
	if ct.reconcileTick != nil {
		ct.reconcileTick.Stop()
	}
}

// processOverdueConfirmations checks for overdue confirmations and handles
// reminders and escalation. Called by RunReminderLoop on each tick.
func (ct *ConfirmationTracker) processOverdueConfirmations(ctx context.Context) {
	overdueConfs, err := ct.store.FindOverdue(ctx)
	if err != nil {
		log.Printf("[ConfirmationTracker] overdue query failed: error=%v", err)
		return
	}

	log.Printf("[ConfirmationTracker] processing overdue confirmations: count=%d", len(overdueConfs))

	for i := range overdueConfs {
		conf := &overdueConfs[i]

		if conf.RemindersSent < conf.MaxReminders {
			// Check if enough time has passed since last reminder.
			if !ct.shouldSendReminder(conf) {
				continue
			}
			ct.sendReminder(ctx, conf)
		} else {
			// Reminders exhausted — escalate or auto-close.
			switch conf.Type {
			case ConfirmTypeExecutor:
				ct.escalateToInitiator(ctx, conf)
			case ConfirmTypeNotifier:
				ct.autoClose(ctx, conf)
			}
		}
	}
}

// shouldSendReminder checks if enough time has passed since the last reminder
// (or since creation if no reminder has been sent yet).
func (ct *ConfirmationTracker) shouldSendReminder(conf *Confirmation) bool {
	interval := time.Duration(conf.ReminderIntervalHours) * time.Hour
	if interval <= 0 {
		interval = 24 * time.Hour // fallback default
	}

	var lastTime time.Time
	if conf.LastReminderAt != nil {
		lastTime = *conf.LastReminderAt
	} else {
		lastTime = conf.CreatedAt
	}

	return time.Since(lastTime) >= interval
}

// sendReminder dispatches a reminder notification and increments the reminder counter.
func (ct *ConfirmationTracker) sendReminder(ctx context.Context, conf *Confirmation) {
	notif := &WorkflowNotification{
		InstanceID:  conf.InstanceID,
		Type:        NotifTypeReminder,
		RecipientID: conf.RecipientID,
		InstanceURL: fmt.Sprintf("/instances/%s", conf.InstanceID),
	}

	// Try to enrich notification with workflow name from instance.
	if ct.instanceStore != nil {
		if inst, err := ct.instanceStore.Get(ctx, conf.InstanceID); err == nil && inst != nil {
			notif.WorkflowName = extractWorkflowName(inst)
		}
	}

	if err := ct.notifDispatcher.Dispatch(ctx, notif); err != nil {
		log.Printf("[ConfirmationTracker] reminder dispatch failed: conf_id=%s instance_id=%s recipient=%s type=%s error=%v",
			conf.ID, conf.InstanceID, conf.RecipientID, conf.Type, err)
		return
	}

	if err := ct.store.IncrementReminders(ctx, conf.ID); err != nil {
		log.Printf("[ConfirmationTracker] increment reminders failed: conf_id=%s instance_id=%s error=%v",
			conf.ID, conf.InstanceID, err)
		return
	}

	log.Printf("[ConfirmationTracker] reminder sent: conf_id=%s instance_id=%s recipient=%s type=%s reminders_sent=%d",
		conf.ID, conf.InstanceID, conf.RecipientID, conf.Type, conf.RemindersSent+1)
}

// escalateToInitiator notifies the workflow initiator that an executor has not
// confirmed after all reminders are exhausted. Records an "escalation_triggered" audit event.
func (ct *ConfirmationTracker) escalateToInitiator(ctx context.Context, conf *Confirmation) {
	var initiatorID string
	var workflowName string

	// Load instance to get initiator_id.
	if ct.instanceStore != nil {
		if inst, err := ct.instanceStore.Get(ctx, conf.InstanceID); err == nil && inst != nil {
			initiatorID = extractInitiatorID(inst)
			workflowName = extractWorkflowName(inst)
		}
	}

	if initiatorID == "" {
		log.Printf("[ConfirmationTracker] escalation skipped: conf_id=%s instance_id=%s reason=initiator_not_found",
			conf.ID, conf.InstanceID)
		return
	}

	// Send escalation notification to the initiator.
	notif := &WorkflowNotification{
		InstanceID:   conf.InstanceID,
		Type:         NotifTypeEscalation,
		RecipientID:  initiatorID,
		WorkflowName: workflowName,
		InstanceURL:  fmt.Sprintf("/instances/%s", conf.InstanceID),
	}

	if err := ct.notifDispatcher.Dispatch(ctx, notif); err != nil {
		log.Printf("[ConfirmationTracker] escalation dispatch failed: conf_id=%s instance_id=%s recipient=%s initiator=%s error=%v",
			conf.ID, conf.InstanceID, conf.RecipientID, initiatorID, err)
	} else {
		log.Printf("[ConfirmationTracker] escalation sent: conf_id=%s instance_id=%s recipient=%s initiator=%s max_reminders=%d",
			conf.ID, conf.InstanceID, conf.RecipientID, initiatorID, conf.MaxReminders)
	}

	// Record "escalation_triggered" audit event.
	if ct.auditStore != nil {
		_ = ct.auditStore.Append(ctx, &AuditEntry{
			InstanceID: conf.InstanceID,
			EventType:  "escalation_triggered",
			ActorID:    conf.RecipientID,
			Details:    fmt.Sprintf("executor %s did not confirm after %d reminders; escalated to initiator %s", conf.RecipientID, conf.MaxReminders, initiatorID),
			Timestamp:  time.Now().UTC().Truncate(time.Millisecond),
		})
	}

	// Auto-close the confirmation to prevent repeated escalation on next tick.
	_ = ct.store.UpdateStatus(ctx, conf.ID, ConfirmAutoClosed, "")
}

// autoClose marks a notifier confirmation as auto-closed when all reminders
// are exhausted. Records an "auto_closed" audit event with reason "notifier_timeout".
func (ct *ConfirmationTracker) autoClose(ctx context.Context, conf *Confirmation) {
	if err := ct.store.UpdateStatus(ctx, conf.ID, ConfirmAutoClosed, ""); err != nil {
		log.Printf("[ConfirmationTracker] auto-close failed: conf_id=%s instance_id=%s recipient=%s error=%v",
			conf.ID, conf.InstanceID, conf.RecipientID, err)
		return
	}

	log.Printf("[ConfirmationTracker] auto-closed: conf_id=%s instance_id=%s recipient=%s type=%s reason=notifier_timeout",
		conf.ID, conf.InstanceID, conf.RecipientID, conf.Type)

	// Record "auto_closed" audit event.
	if ct.auditStore != nil {
		_ = ct.auditStore.Append(ctx, &AuditEntry{
			InstanceID: conf.InstanceID,
			EventType:  "auto_closed",
			ActorID:    conf.RecipientID,
			Details:    "notifier_timeout",
			Timestamp:  time.Now().UTC().Truncate(time.Millisecond),
		})
	}
}

// ReconcileOrphanedInstances finds completed instances that have no confirmation
// records (due to a crash between completion and confirmation creation, the
// window documented in executeTerminalNode) and creates the missing records.
//
// Mechanism (mirrors the documented query):
//   - The instance store must implement OrphanedInstanceFinder; it returns
//     completed instances within the retention window that have no rows in the
//     confirmations table.
//   - For each orphan, the terminal node's TerminalNodeConfig is re-derived from
//     the instance's published version graph (via the injected WorkflowStore),
//     and the existing StartTracking is called to create the missing records.
//
// StartTracking and Confirm validation are unchanged (Preservation 3.7, 3.8):
// this only fills the gap left by the crash window. It is idempotent — orphans
// are exactly those instances with zero confirmation rows, so StartTracking is
// never invoked for instances whose records already exist.
//
// Call on startup and on a periodic ticker (see RunReconcileLoop).
func (ct *ConfirmationTracker) ReconcileOrphanedInstances(ctx context.Context) error {
	finder, ok := ct.instanceStore.(OrphanedInstanceFinder)
	if !ok {
		// The wired instance store does not support orphan detection; nothing
		// to reconcile. This keeps test mocks that don't need reconciliation
		// working without implementing the optional capability.
		return nil
	}

	orphans, err := finder.FindCompletedWithoutConfirmations(ctx, orphanReconcileRetention)
	if err != nil {
		return fmt.Errorf("find orphaned completed instances: %w", err)
	}
	if len(orphans) == 0 {
		return nil
	}

	log.Printf("[ConfirmationTracker] reconciling orphaned completed instances: count=%d", len(orphans))

	var firstErr error
	repaired := 0
	for i := range orphans {
		inst := &orphans[i]
		termConfig, derr := ct.deriveTerminalNodeConfig(ctx, inst)
		if derr != nil {
			log.Printf("[ConfirmationTracker] reconcile skipped: instance_id=%s reason=%v", inst.ID, derr)
			if firstErr == nil {
				firstErr = derr
			}
			continue
		}
		if termConfig == nil {
			// No terminal-node config (nothing to track) — not an error.
			continue
		}
		if err := ct.StartTracking(ctx, inst, termConfig); err != nil {
			log.Printf("[ConfirmationTracker] reconcile StartTracking failed: instance_id=%s error=%v", inst.ID, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		repaired++

		// Record an audit event so the repair is observable.
		if ct.auditStore != nil {
			_ = ct.auditStore.Append(ctx, &AuditEntry{
				InstanceID: inst.ID,
				EventType:  "confirmations_reconciled",
				Details:    fmt.Sprintf(`{"reason":"orphaned_completed_instance","node_id":"%s"}`, escapeJSON(inst.CurrentNodeID)),
				Timestamp:  time.Now().UTC().Truncate(time.Millisecond),
			})
		}
	}

	log.Printf("[ConfirmationTracker] reconciliation complete: orphans=%d repaired=%d", len(orphans), repaired)
	return firstErr
}

// deriveTerminalNodeConfig re-derives the TerminalNodeConfig for a completed
// instance from its published version graph. The terminal node is identified by
// the instance's current_node_id when present, otherwise the single terminal
// node in the graph. Returns (nil, nil) when there is no terminal-node config to
// track (e.g. no executors/notifiers configured).
func (ct *ConfirmationTracker) deriveTerminalNodeConfig(ctx context.Context, inst *WorkflowInstance) (*TerminalNodeConfig, error) {
	if ct.workflowStore == nil {
		return nil, fmt.Errorf("workflow store not configured; cannot derive terminal node config")
	}

	ver, err := ct.workflowStore.GetVersion(ctx, inst.VersionID)
	if err != nil {
		return nil, fmt.Errorf("get version %s: %w", inst.VersionID, err)
	}
	if ver == nil {
		return nil, fmt.Errorf("version %s not found", inst.VersionID)
	}

	node := findTerminalNode(&ver.Graph, inst.CurrentNodeID)
	if node == nil {
		return nil, fmt.Errorf("no terminal node found in version %s", inst.VersionID)
	}

	var termConfig TerminalNodeConfig
	if len(node.Config) > 0 {
		if err := json.Unmarshal(node.Config, &termConfig); err != nil {
			return nil, fmt.Errorf("parse terminal node config: %w", err)
		}
	}
	ApplyTerminalNodeDefaults(&termConfig)

	if len(termConfig.ResultExecutors) == 0 && len(termConfig.Notifiers) == 0 {
		// Nothing to track for this terminal node.
		return nil, nil
	}
	return &termConfig, nil
}

// findTerminalNode returns the terminal node identified by preferredNodeID when
// it exists and is a terminal node; otherwise it returns the single terminal
// node in the graph (or nil if there is none or it is ambiguous).
func findTerminalNode(graph *WorkflowGraph, preferredNodeID string) *WorkflowNode {
	if graph == nil {
		return nil
	}
	if preferredNodeID != "" {
		if node := findNodeByID(graph, preferredNodeID); node != nil && node.Type == NodeTypeTerminal {
			return node
		}
	}
	var terminals []*WorkflowNode
	for i := range graph.Nodes {
		if graph.Nodes[i].Type == NodeTypeTerminal {
			terminals = append(terminals, &graph.Nodes[i])
		}
	}
	if len(terminals) == 1 {
		return terminals[0]
	}
	return nil
}

// RunReconcileLoop runs ReconcileOrphanedInstances once on startup and then on a
// periodic ticker until ctx is cancelled. It complements RunReminderLoop: the
// reminder loop only inspects the confirmations table (so it can never see an
// instance that has no confirmation rows), while this loop detects and repairs
// exactly those orphaned completed instances.
func (ct *ConfirmationTracker) RunReconcileLoop(ctx context.Context) {
	// Run once on startup so a crash-orphaned instance is repaired promptly.
	if err := ct.ReconcileOrphanedInstances(ctx); err != nil {
		log.Printf("[ConfirmationTracker] startup reconcile error: %v", err)
	}

	ct.reconcileTick = time.NewTicker(orphanReconcileInterval)
	defer ct.reconcileTick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ct.reconcileTick.C:
			if err := ct.ReconcileOrphanedInstances(ctx); err != nil {
				log.Printf("[ConfirmationTracker] periodic reconcile error: %v", err)
			}
		}
	}
}
