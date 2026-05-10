package main

import (
	"strings"
	"time"
)

type SummaryReducer interface {
	Apply(current SessionSummary, events []ImportantEvent, lines []string) SessionSummary
}

type ClaudeSummaryReducer struct{}

func NewClaudeSummaryReducer() *ClaudeSummaryReducer {
	return &ClaudeSummaryReducer{}
}

func (r *ClaudeSummaryReducer) Apply(current SessionSummary, events []ImportantEvent, lines []string) SessionSummary {
	next := current
	next.UpdatedAt = time.Now().Unix()

	if next.Severity == "" {
		next.Severity = string(summarySeverityInfo)
	}
	if next.Status == "" {
		next.Status = SessionRunning.String()
	}

	for _, evt := range events {
		switch normalizeSummaryEventType(evt.Type) {
		case summaryEventSessionInit:
			next.Status = SessionRunning.String()
			next.Severity = string(summarySeverityInfo)
			next.WaitingForUser = false
			next.CurrentTask = "Starting session"
			next.ProgressSummary = evt.Summary
			next.SuggestedAction = "Wait for the first tool action"
		case summaryEventFileRead:
			next.Status = SessionBusy.String()
			next.CurrentTask = "Inspecting project files"
			next.ProgressSummary = evt.Summary
			next.ImportantFiles = appendRecentUnique(next.ImportantFiles, evt.RelatedFile, 5)
		case summaryEventFileChange:
			next.Status = SessionBusy.String()
			next.CurrentTask = "Modifying source files"
			next.ProgressSummary = evt.Summary
			next.LastResult = "Applied code changes"
			next.ImportantFiles = appendRecentUnique(next.ImportantFiles, evt.RelatedFile, 5)
			next.SuggestedAction = "Continue and verify the changes"
		case summaryEventCommandStarted:
			next.Status = SessionBusy.String()
			next.WaitingForUser = false
			next.LastCommand = evt.Command
			next.CurrentTask = "Running validation command"
			next.ProgressSummary = evt.Summary
			next.SuggestedAction = "Continue"
		case summaryEventCommandSuccess:
			next.Status = SessionRunning.String()
			next.Severity = string(summarySeverityInfo)
			next.LastResult = evt.Summary
			next.ProgressSummary = "Command completed successfully"
			next.SuggestedAction = "Continue"
		case summaryEventCommandFailed:
			next.Status = SessionRunning.String()
			next.Severity = string(summarySeverityWarn)
			next.LastResult = evt.Summary
			next.ProgressSummary = "Command failed — reviewing results"
			next.SuggestedAction = "Check the error and decide next step"
		case summaryEventTaskCompleted:
			next.Status = SessionWaitingInput.String()
			next.Severity = string(summarySeverityInfo)
			next.WaitingForUser = true
			next.CurrentTask = "Task completed"
			next.LastResult = evt.Summary
			next.ProgressSummary = "Waiting for next instruction"
			next.SuggestedAction = "Review results and send next instruction"
		case summaryEventInputRequired:
			next.Status = SessionWaitingInput.String()
			next.Severity = string(summarySeverityWarn)
			next.WaitingForUser = true
			next.LastResult = evt.Summary
			next.SuggestedAction = "Review status and send next instruction"
		case summaryEventSessionError:
			next.Status = SessionError.String()
			next.Severity = string(summarySeverityError)
			next.WaitingForUser = false
			next.LastResult = evt.Summary
			next.SuggestedAction = "Fix the current error and continue"
		case summaryEventSessionFailed:
			next.Status = SessionError.String()
			next.Severity = string(summarySeverityError)
			next.WaitingForUser = false
			next.CurrentTask = "Starting session"
			next.ProgressSummary = "Session failed before becoming interactive"
			next.LastResult = evt.Summary
			next.SuggestedAction = "Review the launch error and try again"
		case summaryEventSessionClosed:
			next.Status = SessionExited.String()
			next.WaitingForUser = false
			next.CurrentTask = "Session finished"
			next.ProgressSummary = evt.Summary
			next.LastResult = evt.Summary
			next.SuggestedAction = "Start a new session when ready"
			next.Severity = string(normalizeSummarySeverity(evt.Severity))
		}
	}

	if len(events) == 0 && len(lines) > 0 {
		joined := strings.ToLower(strings.Join(lines, " "))
		// Only update status from raw output lines when the session is in an
		// active (non-terminal, non-waiting) state.  Once the session reaches
		// waiting_input, error, or exited, raw output should NOT reset it back
		// to running/busy — only a recognized event can change the status.
		status := normalizeSessionStatus(next.Status)
		if !status.IsWaitingOrTerminal() {
			if classifyRemoteSummaryOutputMarker(joined) == remoteSummaryOutputMarkerBusy {
				next.Status = SessionBusy.String()
			}
			// Otherwise keep the current status (don't force it to "running")
		}

		// Heuristic: detect idle/waiting patterns from raw output even when
		// no structured event was extracted.  Claude Code shows a prompt
		// character (e.g. ">") or certain phrases when it finishes a task.
		status = normalizeSessionStatus(next.Status)
		if status.IsRunning() || status.IsBusy() {
			if classifyRemoteSummaryOutputMarker(joined) == remoteSummaryOutputMarkerWaiting {
				next.Status = SessionWaitingInput.String()
				next.WaitingForUser = true
				next.SuggestedAction = "Review results and send next instruction"
			}
		}
	}

	return next
}

func appendRecentUnique(items []string, value string, limit int) []string {
	if value == "" {
		return items
	}

	filtered := make([]string, 0, len(items)+1)
	for _, item := range items {
		if item == "" || item == value {
			continue
		}
		filtered = append(filtered, item)
	}

	filtered = append(filtered, value)
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	return filtered
}
