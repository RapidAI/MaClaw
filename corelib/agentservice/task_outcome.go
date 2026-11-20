package agentservice

import (
	"strings"
	"sync"
)

// Task outcome: what a finished run's task really reached, as measured by
// the host (e.g. the desktop runtime's goal ledger), independent of the run
// status. A run whose execution ended normally and whose task is done keeps
// RunStatusSucceeded. A task that is not done is waiting_for_user, and that
// run's status is RunStatusPaused: the person has to decide what happens
// next, so a status-only reader must not count it as finished work. The
// outcome also carries metadata task_outcome on the run, the assistant
// message and the terminal events. The host sets assistant metadata task_outcome (and an
// optional task_outcome_notice line) through AssistantMessageMetadataHook,
// which runs after the model's last reply, so a confident reply cannot hide
// the measured outcome.
const (
	TaskOutcomeKey       = "task_outcome"
	TaskOutcomeNoticeKey = "task_outcome_notice"
	// ReceiptPendingKey marks a terminal run whose finish hook has not
	// committed the reconciled receipt yet. ReceiptFinalKey replaces it
	// when that write succeeds.
	ReceiptPendingKey = "receipt_pending"
	ReceiptFinalKey   = "receipt_final"

	TaskOutcomeDone         = "done"
	TaskOutcomePartial      = "partial"
	TaskOutcomeBlocked      = "blocked"
	TaskOutcomeNotSubmitted = "not_submitted"
	TaskOutcomeUnconfirmed  = "unconfirmed"
	TaskOutcomeUnverified   = "unverified"
	TaskOutcomeAwaitingUser = "awaiting_user"
	TaskOutcomeRunning      = "running"
)

// ValidTaskOutcome reports whether s is one of the defined outcomes.
func ValidTaskOutcome(s string) bool {
	switch s {
	case TaskOutcomeDone, TaskOutcomePartial, TaskOutcomeBlocked, TaskOutcomeNotSubmitted, TaskOutcomeUnconfirmed, TaskOutcomeUnverified, TaskOutcomeAwaitingUser, TaskOutcomeRunning:
		return true
	}
	return false
}

// TaskOutcomeNeedsUser: the task is not done and the person must act.
func TaskOutcomeNeedsUser(s string) bool {
	return ValidTaskOutcome(s) && s != TaskOutcomeDone && s != TaskOutcomeRunning
}

// applyTaskOutcome moves a host-measured outcome from the assistant
// message's metadata onto the run and puts its notice in front of the reply.
// Unknown values are dropped (never invented for clients to parse).
func applyTaskOutcome(run *Run, msg *Message) string {
	if run == nil || msg == nil || msg.Metadata == nil {
		return ""
	}
	outcome := strings.TrimSpace(msg.Metadata[TaskOutcomeKey])
	notice := strings.TrimSpace(msg.Metadata[TaskOutcomeNoticeKey])
	delete(msg.Metadata, TaskOutcomeNoticeKey)
	if !ValidTaskOutcome(outcome) {
		delete(msg.Metadata, TaskOutcomeKey)
		return ""
	}
	msg.Metadata[TaskOutcomeKey] = outcome
	if run.Metadata == nil {
		run.Metadata = map[string]string{}
	}
	run.Metadata[TaskOutcomeKey] = outcome
	for _, key := range []string{"outcome_status", "outcome_results", "outcome_blocker", "outcome_unblock", "outcome_next", "outcome_plan", "outcome_remediated", "outcome_record"} {
		if v := strings.TrimSpace(msg.Metadata[key]); v != "" {
			run.Metadata[key] = v
		}
	}
	improving := msg.Metadata["outcome_next"] == "improve"
	if improving {
		// A follow-up run does the next step. The brief sentence already
		// in send_to_user stays; the raw reply does not replace it, and
		// the run does not wait for a reply.
		run.WaitingForUser = false
		if strings.TrimSpace(msg.Metadata["send_to_user"]) == "" {
			return outcome
		}
	} else if TaskOutcomeNeedsUser(outcome) {
		run.WaitingForUser = true
	}
	// The host sends a notice only when the reply needs one: the task is
	// not done, or the reply denies a result the host verified (a done
	// task whose reply says it failed gets the correction too).
	if notice != "" && !strings.HasPrefix(strings.TrimSpace(msg.Content), notice) {
		if strings.TrimSpace(msg.Content) == "" {
			msg.Content = notice
		} else {
			msg.Content = notice + "\n" + msg.Content
		}
	}
	// A measured task's final reply is its report to the person. Clients
	// show only send_to_user, so a run that never called it ended in
	// silence: the person saw the acknowledgement and then nothing (papers
	// 2026-11-11: Bot 3's 「这件没做成…桌面始终未能唤醒」 and Bot 2's post links
	// never appeared). The reply, with its outcome notice, becomes the
	// sentence. A question to the person keeps its own path.
	if !improving && outcome != TaskOutcomeAwaitingUser && strings.TrimSpace(msg.Metadata["send_to_user"]) == "" && strings.TrimSpace(msg.Content) != "" {
		msg.Metadata["send_to_user"] = strings.TrimSpace(msg.Content)
		if strings.TrimSpace(msg.Metadata["send_to_user_kind"]) == "" {
			msg.Metadata["send_to_user_kind"] = "report"
		}
	}
	return outcome
}

// PersistRunReceipt writes the receipt decided after the first save.
// The run metadata and the assistant message stay the same record a
// later read, a reconnect, or a restarted process returns.
func (s *Service) PersistRunReceipt(run *Run, msg *Message) error {
	if s == nil || s.store == nil || run == nil || msg == nil {
		return nil
	}
	if run.Metadata == nil {
		run.Metadata = map[string]string{}
	}
	outcome := ""
	if msg.Metadata != nil {
		outcome = strings.TrimSpace(msg.Metadata[TaskOutcomeKey])
	}
	if ValidTaskOutcome(outcome) {
		run.Metadata[TaskOutcomeKey] = outcome
		if TaskOutcomeNeedsUser(outcome) {
			run.WaitingForUser = true
			// The receipt is often decided after the first save, which already
			// wrote succeeded. A later reader of status alone must still see
			// paused (BR-20261120-8A708E128485). Failed and cancelled stay as
			// they are: those are not a person waiting to decide.
			if run.Status == RunStatusSucceeded {
				run.Status = RunStatusPaused
			}
		}
	} else {
		delete(run.Metadata, TaskOutcomeKey)
	}
	finalMeta := make(map[string]string, len(run.Metadata)+1)
	for key, value := range run.Metadata {
		if key == ReceiptPendingKey {
			continue
		}
		finalMeta[key] = value
	}
	finalMeta[ReceiptFinalKey] = "1"
	run.Metadata = finalMeta
	if both, ok := s.store.(interface {
		ReviseRunReceipt(Message, Run) error
	}); ok {
		return both.ReviseRunReceipt(*msg, *run)
	}
	if rev, ok := s.store.(interface{ ReviseMessage(Message) error }); ok {
		if err := rev.ReviseMessage(*msg); err != nil {
			return err
		}
	}
	return s.store.SaveRun(*run)
}

var runReceiptInFlight sync.Map

// MarkRunReceiptInFlight records that a finish hook still has to reconcile
// this run. The completion save sees the mark and stores ReceiptPendingKey
// before the terminal row is readable.
func MarkRunReceiptInFlight(runID string) {
	if strings.TrimSpace(runID) == "" {
		return
	}
	runReceiptInFlight.Store(runID, struct{}{})
}

// RunReceiptInFlight reports whether this process still owes the run a receipt.
func RunReceiptInFlight(runID string) bool {
	_, ok := runReceiptInFlight.Load(strings.TrimSpace(runID))
	return ok
}

// ClearRunReceiptInFlight drops the in-process mark. The durable pending
// flag stays until the receipt write clears it.
func ClearRunReceiptInFlight(runID string) {
	runReceiptInFlight.Delete(strings.TrimSpace(runID))
}

func runTaskOutcome(run Run, msg *Message) string {
	if o := run.Metadata[TaskOutcomeKey]; ValidTaskOutcome(o) {
		return o
	}
	if msg != nil {
		if o := msg.Metadata[TaskOutcomeKey]; ValidTaskOutcome(o) {
			return o
		}
	}
	return ""
}
