package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

// WorkflowManifest is the JSON index file written to Project_Storage on workflow
// completion or cancellation. It records metadata about the workflow run.
type WorkflowManifest struct {
	WorkflowType string               `json:"workflow_type"`
	TemplateName string               `json:"template_name"`
	StartedAt    string               `json:"started_at"`
	CompletedAt  string               `json:"completed_at"`
	Status       string               `json:"status"` // "completed" or "cancelled"
	Phases       []ManifestPhaseEntry `json:"phases"`
}

// ManifestPhaseEntry describes a single confirmed phase in the workflow manifest.
type ManifestPhaseEntry struct {
	PhaseID  string `json:"phase_id"`
	FileName string `json:"file_name"`
	Title    string `json:"title"`
}

// workflowTypeToKebab converts a WorkflowType constant to a kebab-case
// directory name by replacing underscores with hyphens.
// Example: "product_design" → "product-design"
func workflowTypeToKebab(wt workflow.WorkflowType) string {
	return strings.ReplaceAll(string(wt), "_", "-")
}

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

// publishToProjectStorage publishes a confirmed phase document to Project_Storage.
// Called from the phase confirmation handler (advancePhase or equivalent).
// This is a best-effort operation: failures are logged but never block workflow execution.
func (a *GUIWorkflowAdapter) publishToProjectStorage(phaseID, content string) {
	if strings.TrimSpace(content) == "" {
		return
	}
	dir := a.resolveProjectStorageDir()
	if dir == "" {
		return // no workingDir or no workflow type
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("[WorkflowAdapter] publish: failed to create dir %s: %v", dir, err)
		return // non-blocking
	}
	fileName := workflowPhaseFileName(phaseID)
	filePath := filepath.Join(dir, fileName)
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		log.Printf("[WorkflowAdapter] publish: failed to write %s: %v", filePath, err)
	} else {
		log.Printf("[WorkflowAdapter] published to project storage: %s (%d bytes)", filePath, len(content))
	}
}

// resolveProjectStorageDir resolves and caches the Project_Storage date directory
// path. On first call it computes the path from workflowProjectPath, activeWorkflowType,
// and workflowStartDate, using resolveCollisionFreeDir for collision avoidance.
// Subsequent calls return the cached value. Returns empty string if workingDir or
// activeWorkflowType is not set.
func (a *GUIWorkflowAdapter) resolveProjectStorageDir() string {
	a.mu.RLock()
	cached := a.projectStorageDir
	a.mu.RUnlock()
	if cached != "" {
		return cached
	}

	projectPath := a.workflowProjectPath()
	if projectPath == "" {
		return ""
	}

	a.mu.RLock()
	wfType := a.activeWorkflowType
	startDate := a.workflowStartDate
	a.mu.RUnlock()
	if wfType == "" {
		return ""
	}

	typeDir := workflowTypeToKebab(wfType)
	dateStr := startDate.Format("2006-01-02")
	baseDir := filepath.Join(projectPath, "docs", "workflow", typeDir)
	dir := resolveCollisionFreeDir(baseDir, dateStr)

	a.mu.Lock()
	// Double-check: another goroutine may have resolved and cached while we
	// were computing. Use the first-writer's value for consistency.
	if a.projectStorageDir == "" {
		a.projectStorageDir = dir
	} else {
		dir = a.projectStorageDir
	}
	a.mu.Unlock()
	return dir
}

// resolveCollisionFreeDir scans existing date directories under baseDir and
// returns a collision-free path for the given dateStr. If the candidate path
// does not exist, it is returned as-is. Otherwise, numeric suffixes -2, -3, ...
// up to -100 are tried. In the extremely unlikely case that all 100 suffixes
// are taken, a Unix nanosecond timestamp suffix is used as a final fallback.
//
// When os.Stat returns an error other than IsNotExist (e.g., permission denied),
// the path is treated as "exists" and the next suffix is tried. This is the
// conservative choice: we avoid writing to a path whose status is uncertain.
func resolveCollisionFreeDir(baseDir, dateStr string) string {
	candidate := filepath.Join(baseDir, dateStr)
	if _, err := os.Stat(candidate); err != nil && os.IsNotExist(err) {
		return candidate
	}
	for i := 2; i <= 100; i++ {
		suffixed := filepath.Join(baseDir, fmt.Sprintf("%s-%d", dateStr, i))
		if _, err := os.Stat(suffixed); err != nil && os.IsNotExist(err) {
			return suffixed
		}
	}
	// Extremely unlikely: fall back to nanosecond timestamp suffix.
	return filepath.Join(baseDir, fmt.Sprintf("%s-%d", dateStr, time.Now().UnixNano()))
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
	// Safety: only clean under a .maclaw/workflow/ path that looks legitimate.
	baseDir := filepath.Join(projectPath, ".maclaw", "workflow")
	if !strings.Contains(baseDir, ".maclaw") {
		return
	}
	// Clean both the legacy flat directory and any workflow-ID subdirectories.
	// workflowDocDir() uses {projectPath}/.maclaw/workflow/{workflowID}/ for
	// instance isolation, but older sessions may have written to the flat path.
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

// cleanInternalStorageOnCompletion removes the active workflow-ID subdirectory
// from Internal_Storage after a successful workflow completion. This is called
// after all confirmed phase documents have been published to Project_Storage,
// so the intermediate files are no longer needed.
//
// Unlike CleanPersistedWorkflowDocs (which removes ALL content from .maclaw/workflow/
// when a new workflow starts), this method only removes the specific workflow-ID
// subdirectory for the just-completed workflow instance.
//
// This method NEVER touches Project_Storage (docs/workflow/).
//
// If removal fails (permission error, file locked), the error is logged and
// workflow completion continues without interruption.
func (a *GUIWorkflowAdapter) cleanInternalStorageOnCompletion() {
	projectPath := a.workflowProjectPath()
	if projectPath == "" {
		return
	}
	a.mu.RLock()
	wfID := a.activeWorkflowID
	a.mu.RUnlock()
	if wfID == "" {
		return
	}
	// Only remove the specific workflow-ID subdirectory, not the entire
	// .maclaw/workflow/ directory (other workflow instances may exist).
	wfDir := filepath.Join(projectPath, ".maclaw", "workflow", wfID)
	// Safety check: ensure the resolved path is strictly under the expected
	// .maclaw/workflow/ base directory. This guards against path traversal
	// if wfID contains ".." or other unexpected characters.
	expectedBase := filepath.Join(projectPath, ".maclaw", "workflow") + string(filepath.Separator)
	if !strings.HasPrefix(wfDir, expectedBase) {
		log.Printf("[WorkflowAdapter] clean: refusing to remove suspicious path %s (not under %s)", wfDir, expectedBase)
		return
	}
	if err := os.RemoveAll(wfDir); err != nil {
		log.Printf("[WorkflowAdapter] clean: failed to remove completed workflow dir %s: %v", wfDir, err)
	} else {
		log.Printf("[WorkflowAdapter] clean: removed completed workflow dir: %s", wfDir)
	}
}

// resolveTemplateName returns the display name of the active workflow template.
// It looks up the template from the engine's registry using activeWorkflowType.
// Falls back to string(activeWorkflowType) if the registry is unavailable or
// the template is not found.
func (a *GUIWorkflowAdapter) resolveTemplateName() string {
	a.mu.RLock()
	wfType := a.activeWorkflowType
	a.mu.RUnlock()

	if a.engine != nil {
		if reg := a.engine.GetRegistry(); reg != nil {
			if tmpl := reg.Match(wfType); tmpl != nil && tmpl.Name != "" {
				return tmpl.Name
			}
		}
	}
	return string(wfType)
}

// writeWorkflowManifest writes workflow-manifest.json to the Project_Storage
// date directory. Called on workflow completion or cancellation to record
// metadata about the workflow run. Errors are logged but never block workflow
// execution.
func (a *GUIWorkflowAdapter) writeWorkflowManifest(status string, phases []ManifestPhaseEntry) {
	dir := a.resolveProjectStorageDir()
	if dir == "" {
		return
	}

	a.mu.RLock()
	wfType := a.activeWorkflowType
	startDate := a.workflowStartDate
	a.mu.RUnlock()

	manifest := WorkflowManifest{
		WorkflowType: string(wfType),
		TemplateName: a.resolveTemplateName(),
		StartedAt:    startDate.Format(time.RFC3339),
		CompletedAt:  time.Now().Format(time.RFC3339),
		Status:       status,
		Phases:       phases,
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		log.Printf("[WorkflowAdapter] manifest: marshal error: %v", err)
		return
	}
	filePath := filepath.Join(dir, "workflow-manifest.json")
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("[WorkflowAdapter] manifest: mkdir error %s: %v", dir, err)
		return
	}
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		log.Printf("[WorkflowAdapter] manifest: write error %s: %v", filePath, err)
	} else {
		log.Printf("[WorkflowAdapter] wrote manifest: %s", filePath)
	}
}

// writeManifestOnCompletion is called from EmitPhaseUpdate when a workflow
// transitions to completed or cancelled status. It builds the manifest phase
// entries from the workflow state's PhaseOutputs and calls writeWorkflowManifest
// with the appropriate status string.
//
// This method must be called BEFORE the adapter's activeWorkflowType and
// workflowStartDate fields are cleared, because writeWorkflowManifest depends
// on them (via resolveProjectStorageDir).
func (a *GUIWorkflowAdapter) writeManifestOnCompletion(state *workflow.WorkflowState) {
	if state == nil {
		return
	}

	// Determine status string.
	var status string
	switch state.Status {
	case workflow.WorkflowCompleted:
		status = "completed"
	case workflow.WorkflowCancelled:
		status = "cancelled"
	default:
		return
	}

	// Build []ManifestPhaseEntry from the workflow state's confirmed phases.
	// PhaseOutputs maps phase_id → content for all phases that produced output.
	// We use the template's phase order to produce a deterministic ordering.
	phases := a.buildManifestPhaseEntries(state)

	a.writeWorkflowManifest(status, phases)
}

// buildManifestPhaseEntries constructs an ordered list of ManifestPhaseEntry
// from the workflow state's PhaseOutputs. It uses the template's phase ordering
// (from the registry) to produce deterministic output. Only phases that have
// non-empty output in PhaseOutputs are included.
func (a *GUIWorkflowAdapter) buildManifestPhaseEntries(state *workflow.WorkflowState) []ManifestPhaseEntry {
	if state == nil || len(state.PhaseOutputs) == 0 {
		return nil
	}

	// Try to get the template for ordered phase iteration.
	var tmpl *workflow.WorkflowTemplate
	if a.engine != nil {
		if reg := a.engine.GetRegistry(); reg != nil {
			tmpl = reg.Match(state.Type)
		}
	}

	var entries []ManifestPhaseEntry

	if tmpl != nil && len(tmpl.Phases) > 0 {
		// Use template phase order for deterministic output.
		for _, phase := range tmpl.Phases {
			if _, hasOutput := state.PhaseOutputs[phase.ID]; hasOutput {
				entries = append(entries, ManifestPhaseEntry{
					PhaseID:  phase.ID,
					FileName: workflowPhaseFileName(phase.ID),
					Title:    phase.Name,
				})
			}
		}
	} else {
		// Fallback: no template available, iterate PhaseOutputs in map order.
		// This is non-deterministic but better than nothing.
		for phaseID := range state.PhaseOutputs {
			entries = append(entries, ManifestPhaseEntry{
				PhaseID:  phaseID,
				FileName: workflowPhaseFileName(phaseID),
				Title:    phaseID, // no template → use phase ID as title
			})
		}
	}

	return entries
}
