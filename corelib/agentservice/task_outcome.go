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
	// TaskOutcomeReplyKey replaces the model reply with the host's
	// record-backed text before the notice is attached. It is not stored.
	TaskOutcomeReplyKey = "task_outcome_reply"

	TaskOutcomeDone         = "done"
	TaskOutcomePartial      = "partial"
	TaskOutcomeBlocked      = "blocked"
	TaskOutcomeNotSubmitted = "not_submitted"
	TaskOutcomeUnconfirmed  = "unconfirmed"
	TaskOutcomeAwaitingUser = "awaiting_user"
	TaskOutcomeRunning      = "running"
)

// ValidTaskOutcome reports whether s is one of the defined outcomes.
func ValidTaskOutcome(s string) bool {
	switch s {
	case TaskOutcomeDone, TaskOutcomePartial, TaskOutcomeBlocked, TaskOutcomeNotSubmitted, TaskOutcomeUnconfirmed, TaskOutcomeAwaitingUser, TaskOutcomeRunning:
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
	if reply := strings.TrimSpace(msg.Metadata[TaskOutcomeReplyKey]); reply != "" {
		msg.Content = reply
	}
	delete(msg.Metadata, TaskOutcomeNoticeKey)
	delete(msg.Metadata, TaskOutcomeReplyKey)
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
	if outcome != TaskOutcomeAwaitingUser && strings.TrimSpace(msg.Metadata["send_to_user"]) == "" && strings.TrimSpace(msg.Content) != "" {
		msg.Metadata["send_to_user"] = strings.TrimSpace(msg.Content)
		if strings.TrimSpace(msg.Metadata["send_to_user_kind"]) == "" {
			msg.Metadata["send_to_user_kind"] = "report"
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
