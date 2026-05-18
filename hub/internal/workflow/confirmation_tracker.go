package workflow

import (
	"context"
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

// Stop stops the internal ticker. Should be called when shutting down.
func (ct *ConfirmationTracker) Stop() {
	if ct.ticker != nil {
		ct.ticker.Stop()
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

// ReconcileOrphanedInstances finds completed instances that have no confirmation records
// (due to crash between completion and confirmation creation) and creates the missing records.
// This should be called on startup or periodically (e.g., every hour).
// TODO: Implement in a future task.
func (ct *ConfirmationTracker) ReconcileOrphanedInstances(ctx context.Context) error {
	// Query: SELECT * FROM workflow_instances WHERE status='completed'
	//        AND id NOT IN (SELECT DISTINCT instance_id FROM confirmations)
	//        AND completed_at > NOW() - INTERVAL '7 days'
	return nil // stub
}
