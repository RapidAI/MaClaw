package botregistry

import "strings"

// RunView is one active run as Hub reports it (GET /api/v1/bots/{id}/runs/active).
type RunView struct {
	RunID          string
	Status         string
	Task           string
	Step           string
	NextStep       string
	WaitingForUser bool
	Stale          bool
	Error          string
}

// ApplyRuns folds a bot's active runs into its state. Waiting on the person
// wins over running, running over queued; a stale run counts as an error so
// the main bot can tell the person instead of waiting forever.
func ApplyRuns(b *Bot, runs []RunView) {
	b.State = StateIdle
	b.Task, b.Step, b.NextStep, b.RunID, b.StopReason, b.LastError = "", "", "", "", "", ""
	b.Queue = 0
	var running, waiting, stale *RunView
	for i := range runs {
		r := &runs[i]
		status := strings.ToLower(strings.TrimSpace(r.Status))
		switch {
		case r.WaitingForUser || status == "waiting_for_user" || status == "waiting":
			if waiting == nil {
				waiting = r
			}
		case status == "queued" || status == "pending":
			b.Queue++
		case r.Stale:
			if stale == nil {
				stale = r
			}
		case status == "failed" || status == "error":
			if stale == nil {
				stale = r
			}
		default:
			if running == nil {
				running = r
			}
		}
	}
	pick := func(r *RunView) {
		b.RunID, b.Task, b.Step, b.NextStep = r.RunID, r.Task, r.Step, r.NextStep
	}
	switch {
	case waiting != nil:
		b.State = StateWaitingUser
		pick(waiting)
		b.StopReason = firstNonEmpty(waiting.Step, "waiting for the person")
	case running != nil:
		b.State = StateRunning
		pick(running)
	case stale != nil:
		b.State = StateError
		pick(stale)
		b.LastError = firstNonEmpty(stale.Error, "run looks stuck (no progress)")
	case b.Queue > 0:
		b.State = StateQueued
	}
	if b.Paused && b.State == StateIdle {
		b.State = StatePaused
	}
}
