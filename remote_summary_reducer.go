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
		next.Severity = "info"
	}
	if next.Status == "" {
		next.Status = string(SessionRunning)
	}

	for _, evt := range events {
		switch evt.Type {
		case "session.init":
			next.Status = string(SessionRunning)
			next.Severity = "info"
			next.WaitingForUser = false
			next.CurrentTask = "Starting session"
			next.ProgressSummary = evt.Summary
			next.SuggestedAction = "Wait for the first tool action"
		case "file.read":
			next.Status = string(SessionBusy)
			next.CurrentTask = "Inspecting project files"
			next.ProgressSummary = evt.Summary
			next.ImportantFiles = appendRecentUnique(next.ImportantFiles, evt.RelatedFile, 5)
		case "file.change":
			next.Status = string(SessionBusy)
			next.CurrentTask = "Modifying source files"
			next.ProgressSummary = evt.Summary
			next.LastResult = "Applied code changes"
			next.ImportantFiles = appendRecentUnique(next.ImportantFiles, evt.RelatedFile, 5)
			next.SuggestedAction = "Continue and verify the changes"
		case "command.started":
			next.Status = string(SessionBusy)
			next.WaitingForUser = false
			next.LastCommand = evt.Command
			next.CurrentTask = "Running validation command"
			next.ProgressSummary = evt.Summary
			next.SuggestedAction = "Continue"
		case "input.required":
			next.Status = string(SessionWaitingInput)
			next.Severity = "warn"
			next.WaitingForUser = true
			next.LastResult = evt.Summary
			next.SuggestedAction = "Review status and send next instruction"
		case "session.error":
			next.Status = string(SessionError)
			next.Severity = "error"
			next.WaitingForUser = false
			next.LastResult = evt.Summary
			next.SuggestedAction = "Fix the current error and continue"
		case "session.failed":
			next.Status = string(SessionError)
			next.Severity = "error"
			next.WaitingForUser = false
			next.CurrentTask = "Starting session"
			next.ProgressSummary = "Session failed before becoming interactive"
			next.LastResult = evt.Summary
			next.SuggestedAction = "Review the launch error and try again"
		case "session.closed":
			next.Status = string(SessionExited)
			next.WaitingForUser = false
			next.CurrentTask = "Session finished"
			next.ProgressSummary = evt.Summary
			next.LastResult = evt.Summary
			next.SuggestedAction = "Start a new session when ready"
			switch evt.Severity {
			case "error":
				next.Severity = "error"
			case "warn":
				next.Severity = "warn"
			default:
				next.Severity = "info"
			}
		}
	}

	if len(events) == 0 && len(lines) > 0 {
		joined := strings.ToLower(strings.Join(lines, " "))
		if next.Status != string(SessionWaitingInput) && next.Status != string(SessionError) && next.Status != string(SessionExited) {
			if strings.Contains(joined, "running") || strings.Contains(joined, "reading") || strings.Contains(joined, "editing") {
				next.Status = string(SessionBusy)
			} else {
				next.Status = string(SessionRunning)
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
