package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

func (a *GUIWorkflowAdapter) workflowProjectPath() string {
	if a.app == nil {
		return ""
	}
	a.mu.RLock()
	projectPath := a.workingDir
	a.mu.RUnlock()
	if projectPath == "" {
		projectPath = strings.TrimSpace(a.app.GetCurrentProjectPath())
	}
	return projectPath
}

// workflowDocDir returns the directory for persisting workflow documents.
// Each workflow instance gets its own subdirectory to prevent cross-instance
// contamination: {projectPath}/.maclaw/workflow/{workflowID}/
//
// When activeWorkflowID is empty (no active workflow), falls back to the
// legacy flat path {projectPath}/.maclaw/workflow/ for backward compatibility
// with documents persisted before instance isolation was introduced.
func (a *GUIWorkflowAdapter) workflowDocDir() string {
	projectPath := a.workflowProjectPath()
	if projectPath == "" {
		return ""
	}
	a.mu.RLock()
	wfID := a.activeWorkflowID
	a.mu.RUnlock()
	if wfID != "" {
		return filepath.Join(projectPath, ".maclaw", "workflow", wfID)
	}
	// Fallback: no active workflow ID (shouldn't happen during normal operation,
	// but provides backward compatibility).
	return filepath.Join(projectPath, ".maclaw", "workflow")
}

// readPersistedDoc reads the persisted markdown file for a phase.
// Returns empty string if the file doesn't exist or can't be read.
func (a *GUIWorkflowAdapter) readPersistedDoc(phaseID string) string {
	dir := a.workflowDocDir()
	if dir == "" {
		return ""
	}
	fileName := workflowPhaseFileName(phaseID)
	filePath := filepath.Join(dir, fileName)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	return string(data)
}

// stripDocPreamble removes conversational text before the first Markdown
// heading (#).
func stripDocPreamble(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			stripped := strings.TrimSpace(strings.Join(lines[i:], "\n"))
			if len(stripped) > 100 {
				return stripped
			}
			break
		}
	}
	return text
}

// persistWorkflowDoc writes the phase document to the workflow instance's
// dedicated subdirectory: {workingDir}/.maclaw/workflow/{workflowID}/
func (a *GUIWorkflowAdapter) persistWorkflowDoc(phaseID, content string) {
	if strings.TrimSpace(content) == "" {
		return
	}
	dir := a.workflowDocDir()
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("[WorkflowAdapter] failed to create workflow dir %s: %v", dir, err)
		return
	}
	fileName := workflowPhaseFileName(phaseID)
	filePath := filepath.Join(dir, fileName)
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		log.Printf("[WorkflowAdapter] failed to write workflow doc %s: %v", filePath, err)
	} else {
		log.Printf("[WorkflowAdapter] persisted workflow doc: %s (%d bytes)", filePath, len(content))
	}
}

// CleanPersistedWorkflowDocs removes all persisted workflow documents from
// the .maclaw/workflow/ directory. Called when a new workflow starts to prevent
// stale documents from a previous workflow session from leaking into the new
// workflow's phase output capture.
//
// Without this cleanup, EmitDocUpdate's readPersistedDoc could read back old
// content if the new workflow's LLM output happens to be empty or rejected by
// the quality gate. More importantly, the old files on disk represent a
// previous task's deliverables and should not persist across workflow sessions.
//
// NOTE: Only removes .md and .txt files. If persistWorkflowDoc is extended to
// write other formats in the future, update the extension check here.
func (a *GUIWorkflowAdapter) CleanPersistedWorkflowDocs() {
	projectPath := a.workflowProjectPath()
	if projectPath == "" {
		return
	}
	// Clean both the legacy flat directory and any workflow-ID subdirectories.
	// workflowDocDir() uses {projectPath}/.maclaw/workflow/{workflowID}/ for
	// instance isolation, but older sessions may have written to the flat path.
	baseDir := filepath.Join(projectPath, ".maclaw", "workflow")
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		// Directory doesn't exist or can't be read — nothing to clean.
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		fullPath := filepath.Join(baseDir, name)
		if entry.IsDir() {
			// Remove entire workflow-ID subdirectory (previous workflow instance).
			if err := os.RemoveAll(fullPath); err != nil {
				log.Printf("[WorkflowAdapter] failed to remove stale workflow dir %s: %v", fullPath, err)
			} else {
				log.Printf("[WorkflowAdapter] cleaned stale workflow dir: %s", fullPath)
			}
			continue
		}
		// Remove flat-path legacy files.
		if strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".txt") {
			if err := os.Remove(fullPath); err != nil {
				log.Printf("[WorkflowAdapter] failed to remove stale workflow doc %s: %v", fullPath, err)
			} else {
				log.Printf("[WorkflowAdapter] cleaned stale workflow doc: %s", fullPath)
			}
		}
	}
}
