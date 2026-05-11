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

// readPersistedDoc reads the persisted markdown file for a phase.
// Returns empty string if the file doesn't exist or can't be read.
func (a *GUIWorkflowAdapter) readPersistedDoc(phaseID string) string {
	projectPath := a.workflowProjectPath()
	if projectPath == "" {
		return ""
	}
	fileName := workflowPhaseFileName(phaseID)
	filePath := filepath.Join(projectPath, ".maclaw", "workflow", fileName)
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

// persistWorkflowDoc writes the phase document to the workflow working
// directory's .maclaw/workflow/ subdirectory.
func (a *GUIWorkflowAdapter) persistWorkflowDoc(phaseID, content string) {
	if strings.TrimSpace(content) == "" {
		return
	}
	projectPath := a.workflowProjectPath()
	if projectPath == "" {
		return
	}
	dir := filepath.Join(projectPath, ".maclaw", "workflow")
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
