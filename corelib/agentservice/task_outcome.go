package agentservice

import "strings"

// Task outcome: what a finished run's task really reached, as measured by
// the host (e.g. the desktop runtime's goal ledger), independent of the run
// status. A run whose execution ended normally keeps RunStatusSucceeded (the
// status clients already parse); a task that is not done is additionally
// marked waiting_for_user — the person has to decide what happens next — and
// carries metadata task_outcome on the run, the assistant message and the
// terminal events. The host sets assistant metadata task_outcome (and an
// optional task_outcome_notice line) through AssistantMessageMetadataHook,
// which runs after the model's last reply, so a confident reply cannot hide
// the measured outcome.
const (
	TaskOutcomeKey       = "task_outcome"
	TaskOutcomeNoticeKey = "task_outcome_notice"

	TaskOutcomeDone         = "done"
	TaskOutcomePartial      = "partial"
	TaskOutcomeBlocked      = "blocked"
	TaskOutcomeNotSubmitted = "not_submitted"
	TaskOutcomeUnconfirmed  = "unconfirmed"
	TaskOutcomeAwaitingUser = "awaiting_user"
)

// ValidTaskOutcome reports whether s is one of the defined outcomes.
func ValidTaskOutcome(s string) bool {
	switch s {
	case TaskOutcomeDone, TaskOutcomePartial, TaskOutcomeBlocked, TaskOutcomeNotSubmitted, TaskOutcomeUnconfirmed, TaskOutcomeAwaitingUser:
		return true
	}
	return false
}

// TaskOutcomeNeedsUser: the task is not done and the person must act.
func TaskOutcomeNeedsUser(s string) bool {
	return ValidTaskOutcome(s) && s != TaskOutcomeDone
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
	if TaskOutcomeNeedsUser(outcome) {
		run.WaitingForUser = true
		if notice != "" && !strings.HasPrefix(strings.TrimSpace(msg.Content), notice) {
			if strings.TrimSpace(msg.Content) == "" {
				msg.Content = notice
			} else {
				msg.Content = notice + "\n" + msg.Content
			}
		}
	}
	return outcome
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
