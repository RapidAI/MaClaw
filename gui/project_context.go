package main

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// ProjectContextSummary holds the structured project context loaded from
// long-term memory when a Project Tab is first opened.
type ProjectContextSummary struct {
	ProjectName     string                   `json:"project_name"`
	RecentProgress  string                   `json:"recent_progress"`
	KeyArtifacts    []string                 `json:"key_artifacts"`
	RecentArtifacts []ProjectContextArtifact `json:"recent_artifacts,omitempty"`
	ActiveWorkflow  string                   `json:"active_workflow"`
	WorkflowState   *ProjectWorkflowState    `json:"workflow_state,omitempty"`
}

// ProjectContextArtifact is a compact, source-backed artifact summary for
// project tab context injection. Full content stays behind SourceURL.
type ProjectContextArtifact struct {
	Title      string `json:"title,omitempty"`
	SourceType string `json:"source_type,omitempty"`
	SourceURL  string `json:"source_url,omitempty"`
	SourceHint string `json:"source_hint,omitempty"`
	Preview    string `json:"preview,omitempty"`
	UpdatedAt  string `json:"updated_at,omitempty"`
}

// LoadProjectContext recalls project-specific knowledge from long-term memory
// and checks for an active workflow, returning a structured summary suitable
// for injection as the initial system message in a Project Tab.
func (a *App) LoadProjectContext(projectPath string) (*ProjectContextSummary, error) {
	started := time.Now()
	if strings.TrimSpace(projectPath) == "" {
		return nil, fmt.Errorf("projectPath is required")
	}
	defer func() {
		if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
			log.Printf("[project_context] LoadProjectContext slow project=%q elapsed=%s", projectPath, elapsed.Round(time.Millisecond))
		}
	}()

	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return nil, fmt.Errorf("memory store not initialized")
	}

	summary := &ProjectContextSummary{
		ProjectName:  deriveProjectName(projectPath),
		KeyArtifacts: []string{},
	}

	contextData := a.memoryStore.ProjectContextForHost(projectPath, 1)
	taskArtifacts := contextData.TaskArtifacts
	projectKnowledge := contextData.ProjectKnowledge

	// Build recent progress from task artifacts (most recent first from recall).
	var progressParts []string
	for _, entry := range taskArtifacts {
		snippet := truncateRunes(entry.Content, 200)
		if snippet != "" {
			progressParts = append(progressParts, snippet)
		}
		if len(progressParts) >= 3 {
			break
		}
	}
	if len(progressParts) > 0 {
		summary.RecentProgress = strings.Join(progressParts, "\n")
	}

	// Extract key artifact paths from tags, SourceURL fields, and the scene index.
	pathSet := make(map[string]bool)
	addArtifactPath := func(path string) {
		if path == "" || !looksLikeFilePathForContext(path) || pathSet[path] {
			return
		}
		pathSet[path] = true
		summary.KeyArtifacts = append(summary.KeyArtifacts, path)
	}

	allEntries := append(taskArtifacts, projectKnowledge...)
	for _, entry := range allEntries {
		for _, tag := range entry.Tags {
			addArtifactPath(tag)
		}
		addArtifactPath(entry.SourceURL)
	}

	if contextData.HasScene {
		scene := contextData.Scene
		for _, sourceURL := range scene.SourceURLs {
			addArtifactPath(sourceURL)
		}
		for _, artifact := range scene.RecentArtifacts {
			item := ProjectContextArtifact{
				Title:      artifact.Title,
				SourceType: artifact.SourceType,
				SourceURL:  artifact.SourceURL,
				SourceHint: projectTabSourceHint(artifact.SourceURL),
				Preview:    artifact.Preview,
			}
			if !artifact.UpdatedAt.IsZero() {
				item.UpdatedAt = artifact.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")
			}
			summary.RecentArtifacts = append(summary.RecentArtifacts, item)
			addArtifactPath(artifact.SourceURL)
			if len(summary.RecentArtifacts) >= 5 {
				break
			}
		}
	}

	// Limit key artifacts to a reasonable number.
	if len(summary.KeyArtifacts) > 10 {
		summary.KeyArtifacts = summary.KeyArtifacts[:10]
	}

	if state := a.activeWorkflowForProject(projectPath); state != nil {
		summary.WorkflowState = state
		summary.ActiveWorkflow = fmt.Sprintf("%s (phase: %s)", state.Type, state.Phase)
	}

	return summary, nil
}

// deriveProjectName extracts a human-readable project name from the path.
// Uses the last path component (directory name).
func deriveProjectName(projectPath string) string {
	name := filepath.Base(projectPath)
	if name == "." || name == "/" || name == "\\" {
		return projectPath
	}
	return name
}

// looksLikeFilePathForContext checks if a string looks like a file or directory path.
// This is a local helper to avoid depending on the steering package's looksLikeFilePath.
func looksLikeFilePathForContext(s string) bool {
	if s == "" {
		return false
	}
	// Windows absolute path (e.g., C:\..., D:\...)
	if len(s) >= 3 && s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
		return true
	}
	// Unix absolute path
	if strings.HasPrefix(s, "/") && len(s) > 1 {
		return true
	}
	// Home directory path (~/… or ~\… on Windows)
	if corelib.IsHomePath(s) {
		return true
	}
	return false
}
