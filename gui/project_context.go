package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// ProjectContextSummary holds the structured project context loaded from
// long-term memory when a Project Tab is first opened.
type ProjectContextSummary struct {
	ProjectName    string   `json:"project_name"`
	RecentProgress string   `json:"recent_progress"`
	KeyArtifacts   []string `json:"key_artifacts"`
	ActiveWorkflow string   `json:"active_workflow"`
}

// LoadProjectContext recalls project-specific knowledge from long-term memory
// and checks for an active workflow, returning a structured summary suitable
// for injection as the initial system message in a Project Tab.
func (a *App) LoadProjectContext(projectPath string) (*ProjectContextSummary, error) {
	if strings.TrimSpace(projectPath) == "" {
		return nil, fmt.Errorf("projectPath is required")
	}

	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return nil, fmt.Errorf("memory store not initialized")
	}

	summary := &ProjectContextSummary{
		ProjectName:  deriveProjectName(projectPath),
		KeyArtifacts: []string{},
	}

	// Recall task_artifact entries for this project.
	taskArtifacts := a.memoryStore.RecallDynamicStrict(
		"task artifact progress",
		memory.CategoryTaskArtifact,
		projectPath,
	)

	// Recall project_knowledge entries for this project.
	projectKnowledge := a.memoryStore.RecallDynamicStrict(
		"project knowledge",
		memory.CategoryProjectKnowledge,
		projectPath,
	)

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

	// Extract key artifact paths from tags and content of both entry sets.
	pathSet := make(map[string]bool)
	allEntries := append(taskArtifacts, projectKnowledge...)
	for _, entry := range allEntries {
		// Check tags for file paths.
		for _, tag := range entry.Tags {
			if looksLikeFilePathForContext(tag) && !pathSet[tag] {
				pathSet[tag] = true
				summary.KeyArtifacts = append(summary.KeyArtifacts, tag)
			}
		}
		// Check SourceURL for file paths.
		if entry.SourceURL != "" && looksLikeFilePathForContext(entry.SourceURL) && !pathSet[entry.SourceURL] {
			pathSet[entry.SourceURL] = true
			summary.KeyArtifacts = append(summary.KeyArtifacts, entry.SourceURL)
		}
	}

	// Limit key artifacts to a reasonable number.
	if len(summary.KeyArtifacts) > 10 {
		summary.KeyArtifacts = summary.KeyArtifacts[:10]
	}

	// Check for active workflow via synthesized userID.
	synthesizedUserID := fmt.Sprintf("desktop-user:%s", projectPath)
	if a.workflowEngine != nil && !a.workflowDisabled.Load() {
		ws := a.workflowEngine.GetActiveWorkflow(synthesizedUserID)
		if ws != nil {
			summary.ActiveWorkflow = fmt.Sprintf("%s (阶段: %s)", string(ws.Type), ws.CurrentPhase)
		}
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
	// Home directory path
	if strings.HasPrefix(s, "~/") {
		return true
	}
	return false
}

