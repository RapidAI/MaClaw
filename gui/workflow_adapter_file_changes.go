package main

import (
)

// FileChangesPayload is the workflow:file_changes event structure.
type FileChangesPayload struct {
	UserID    string           `json:"user_id"`
	PhaseID   string           `json:"phase_id"`   // always "implementation"
	TaskID    string           `json:"task_id"`
	TaskTitle string           `json:"task_title"` // max 200 chars
	Files     []FileChangeItem `json:"files"`      // max 200 entries
	Truncated bool             `json:"truncated"`  // true if files > 200
}

// FileChangeItem is a single file change in the event payload.
type FileChangeItem struct {
	Path       string `json:"path"`        // project-relative, forward slashes, max 500 chars
	ChangeType string `json:"change_type"` // "added" | "modified" | "deleted"
	Diff       string `json:"diff"`        // unified diff format
	Language   string `json:"language"`    // from extension or "plaintext"
}

// FileActivityPayload is the workflow:file_activity event structure.
type FileActivityPayload struct {
	UserID     string `json:"user_id"`
	PhaseID    string `json:"phase_id"`    // always "implementation"
	TaskID     string `json:"task_id"`
	FilePath   string `json:"file_path"`   // project-relative, forward slashes, max 500 chars
	ChangeType string `json:"change_type"` // "added" | "modified" | "deleted"
}

const (
	maxFileChangesFiles    = 200
	maxTaskTitleChars      = 200
	maxFilePathChars       = 500
	fileChangesPhaseID     = "implementation"
)

// EmitFileChanges emits a workflow:file_changes event after task completion.
// It enforces payload limits: truncates Files to 200 entries and TaskTitle to 200 chars.
// Skips emission if the app context is nil (app shutting down).
func (a *GUIWorkflowAdapter) EmitFileChanges(userID string, payload FileChangesPayload) error {
	if a.app.ctx == nil {
		return nil
	}

	// Enforce TaskTitle max length.
	payload.TaskTitle = truncateString(payload.TaskTitle, maxTaskTitleChars)

	// Enforce PhaseID.
	payload.PhaseID = fileChangesPhaseID

	// Enforce UserID from parameter.
	payload.UserID = userID

	// Truncate file paths to max length.
	for i := range payload.Files {
		payload.Files[i].Path = truncateString(payload.Files[i].Path, maxFilePathChars)
	}

	// Enforce Files array max length.
	if len(payload.Files) > maxFileChangesFiles {
		payload.Files = payload.Files[:maxFileChangesFiles]
		payload.Truncated = true
	}

	a.app.emitEvent("workflow:file_changes", payload)
	return nil
}

// EmitFileActivity emits a lightweight workflow:file_activity event during execution.
// Skips emission if the app context is nil (app shutting down).
func (a *GUIWorkflowAdapter) EmitFileActivity(userID string, payload FileActivityPayload) error {
	if a.app.ctx == nil {
		return nil
	}

	// Enforce PhaseID.
	payload.PhaseID = fileChangesPhaseID

	// Enforce UserID from parameter.
	payload.UserID = userID

	// Truncate file path to max length.
	payload.FilePath = truncateString(payload.FilePath, maxFilePathChars)

	a.app.emitEvent("workflow:file_activity", payload)
	return nil
}

// truncateString truncates s to maxChars runes. If truncation occurs,
// the string is cut at the rune boundary.
func truncateString(s string, maxChars int) string {
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s
	}
	return string(runes[:maxChars])
}
