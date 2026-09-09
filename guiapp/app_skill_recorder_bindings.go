package guiapp

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/skill"
)

// StartSkillRecording begins recording tool operations for skill generation.
// tabID identifies which tab initiated the recording. For the local tab use "local",
// for project tabs use the tab ID (which maps to a project path internally).
// Returns a status message.
func (a *App) StartSkillRecording(tabID string) string {
	if a.skillRecorder == nil {
		a.skillRecorder = NewSkillOperationRecorder()
	}

	// Resolve ownerID from tabID for capture-point filtering.
	ownerID := a.resolveSkillRecordingOwnerID(tabID)
	workDir := a.GetCurrentProjectPath()
	// For project tabs, use the project path as workDir.
	if projectPath := a.resolveProjectPathForTab(tabID, ""); projectPath != "" {
		workDir = projectPath
	}

	if err := a.skillRecorder.StartWithTab(workDir, ownerID, tabID); err != nil {
		return fmt.Sprintf("录制启动失败: %s", err)
	}

	log.Printf("[skill-recorder] recording started, workDir=%s ownerID=%s tabID=%s", workDir, ownerID, tabID)
	a.emitEvent("skill-recording-state-changed", map[string]interface{}{
		"recording": true,
		"count":     0,
		"tabId":     tabID,
	})
	return "ok"
}

// resolveSkillRecordingOwnerID maps a frontend tabID to the backend ownerID
// used for filtering tool calls during recording.
func (a *App) resolveSkillRecordingOwnerID(tabID string) string {
	if tabID == "" || tabID == "local" {
		return desktopUserID
	}
	// Project tabs: look up cached/session projectPath → synthesize ownerID
	if projectPath := a.resolveProjectPathForTab(tabID, ""); projectPath != "" {
		return desktopUserID + ":" + projectPath
	}
	// Fallback: use desktop-user (local tab behavior)
	return desktopUserID
}

// StopSkillRecording stops recording and returns data for the inline card.
// The actual skill generation happens when the user confirms via ResolveSkillRecording.
func (a *App) StopSkillRecording() map[string]interface{} {
	if a.skillRecorder == nil {
		return map[string]interface{}{"error": "not recording"}
	}

	// Atomically check and pause — single-lock to avoid TOCTOU
	a.skillRecorder.mu.Lock()
	if !a.skillRecorder.recording && a.skillRecorder.entries == nil {
		a.skillRecorder.mu.Unlock()
		return map[string]interface{}{"error": "not recording"}
	}
	entryCount := len(a.skillRecorder.entries)
	a.skillRecorder.recording = false
	a.skillRecorder.active.Store(false)
	a.skillRecorder.mu.Unlock()

	// If nothing was recorded, cancel immediately instead of showing the card
	if entryCount == 0 {
		a.skillRecorder.Cancel()
		a.emitEvent("skill-recording-state-changed", map[string]interface{}{
			"recording":      false,
			"pendingConfirm": false,
			"tabId":          a.skillRecorder.TabID(),
		})
		return map[string]interface{}{"error": "no operations recorded"}
	}

	suggestedName := a.skillRecorder.SuggestSkillName()
	suggestedDescription := a.skillRecorder.SuggestDescription()
	summary := a.skillRecorder.OperationSummary()
	count := a.skillRecorder.EntryCount()

	// Scan for potential credential leakage
	a.skillRecorder.mu.Lock()
	entriesCopy := make([]RecordedOp, len(a.skillRecorder.entries))
	copy(entriesCopy, a.skillRecorder.entries)
	workDir := a.skillRecorder.workDir
	a.skillRecorder.mu.Unlock()
	credWarnings := detectCredentialWarnings(entriesCopy)

	// Ask the LLM for a professional name/description/step titles. Falls back
	// to the heuristics above when the LLM is unconfigured or fails.
	var existingNames map[string]bool
	if a.skillExecutor != nil {
		existingNames = make(map[string]bool)
		for _, s := range a.skillExecutor.loadSkills() {
			existingNames[s.Name] = true
		}
	}
	llmAdapter := NewSkillEvolutionLLMAdapter(a.GetMaclawLLMConfig).WithTimeout(25 * time.Second)
	// Pass the consolidated op list so LLM step titles align with the final
	// steps written by generateSkillYAML (which consolidates the same way).
	// Clone first: consolidation rewrites write_file contents in place, and
	// entriesCopy shares the Args maps with the recorder's pending entries.
	if name, desc, titles, ok := SuggestRecordingMetadataWithLLM(llmAdapter, consolidateRecordedOps(cloneRecordedOps(entriesCopy)), workDir, existingNames); ok {
		suggestedName = name
		if desc != "" {
			suggestedDescription = desc
		}
		if len(titles) > 0 {
			a.skillRecorder.SetSuggestedStepTitles(titles)
		}
	}

	a.emitEvent("skill-recording-state-changed", map[string]interface{}{
		"recording":      false,
		"pendingConfirm": true,
		"count":          count,
		"tabId":          a.skillRecorder.TabID(),
	})

	result := map[string]interface{}{
		"suggested_name":        suggestedName,
		"suggested_description": suggestedDescription,
		"summary":               summary,
		"count":                 count,
	}
	if len(credWarnings) > 0 {
		result["security_warnings"] = credWarnings
	}
	return result
}

// ResolveSkillRecording finalizes or cancels the skill recording.
// action: "save" | "cancel"
func (a *App) ResolveSkillRecording(action string, name string, description string) map[string]interface{} {
	if a.skillRecorder == nil {
		return map[string]interface{}{"error": "no recorder"}
	}

	if action == "cancel" {
		a.skillRecorder.Cancel()
		a.emitEvent("skill-recording-state-changed", map[string]interface{}{
			"recording":      false,
			"pendingConfirm": false,
			"tabId":          a.skillRecorder.TabID(),
		})
		return map[string]interface{}{"status": "cancelled"}
	}
	if action != "save" {
		return map[string]interface{}{"error": "unsupported recording action"}
	}
	if a.skillExecutor == nil {
		return map[string]interface{}{"error": "skill executor not initialized"}
	}

	fail := func(err error) map[string]interface{} {
		log.Printf("[skill-recorder] save failed: %v", err)
		a.emitEvent("skill-recording-state-changed", map[string]interface{}{
			"recording":      false,
			"pendingConfirm": false,
			"tabId":          a.skillRecorder.TabID(),
		})
		return map[string]interface{}{"error": err.Error()}
	}

	// A manual confirmation authorizes the request, but not a partial write.
	// Generate only in a managed staging directory; scan and publish it through
	// the same directory/config/index/audit transaction as other GUI installs.
	stagingRoot, err := a.skillStagingDir()
	if err != nil {
		return fail(fmt.Errorf("resolve recorded skill staging root: %w", err))
	}
	stagingDir, err := skill.PrepareStagingDirInRoot(stagingRoot, fmt.Sprintf("skill-recording-%d", time.Now().UnixNano()))
	if err != nil {
		return fail(fmt.Errorf("prepare recorded skill staging: %w", err))
	}
	stagingOwned := true
	defer func() {
		if stagingOwned {
			skill.CleanupStaging(stagingDir)
		}
	}()
	skillDir, portabWarnings, err := a.skillRecorder.StopToDirectory(name, description, stagingDir)
	if err != nil {
		return fail(err)
	}
	entry, err := loadImportedSkillEntry(skillDir)
	if err != nil {
		return fail(fmt.Errorf("load recorded skill staging: %w", err))
	}
	entry.Source = "learned"
	entry.SkillDir = skillDir
	report, err := a.scanAndAdmitSkillBeforeRegister(context.Background(), entry, "skill recording")
	if err != nil {
		return fail(fmt.Errorf("recorded skill security admission: %w", err))
	}
	if err := ensureSkillEvolutionMutationAdmission(a, entry.Name); err != nil {
		return fail(err)
	}
	requestID := fmt.Sprintf("evo_skill_recording_%d", time.Now().UnixNano())
	if err := a.commitStagedSkillInstall(context.Background(), entry, stagingDir, "skill_recording", report, requestID, skillEvolutionConfigRevision(a)); err != nil {
		return fail(fmt.Errorf("recorded skill not committed: %w", err))
	}
	stagingOwned = false
	skillDir = entry.SkillDir
	name = entry.Name
	log.Printf("[skill-recorder] skill committed: name=%s dir=%s", name, skillDir)

	if a.cachedSkillScanner != nil {
		a.cachedSkillScanner.Invalidate()
	}

	a.emitEvent("skill-recording-state-changed", map[string]interface{}{
		"recording":      false,
		"pendingConfirm": false,
		"tabId":          a.skillRecorder.TabID(),
	})

	result := map[string]interface{}{
		"status":    "saved",
		"name":      name,
		"skill_dir": skillDir,
	}
	if len(portabWarnings) > 0 {
		result["portability_warnings"] = portabWarnings
	}
	return result
}

// IsSkillRecording returns whether the recorder is currently active.
func (a *App) IsSkillRecording() bool {
	if a.skillRecorder == nil {
		return false
	}
	return a.skillRecorder.IsRecording()
}

// GetSkillRecordingTabID returns the tab ID that owns the current recording.
func (a *App) GetSkillRecordingTabID() string {
	if a.skillRecorder == nil || !a.skillRecorder.IsRecording() {
		return ""
	}
	return a.skillRecorder.TabID()
}

// GetSkillRecordingCount returns the number of operations recorded so far.
func (a *App) GetSkillRecordingCount() int {
	if a.skillRecorder == nil {
		return 0
	}
	return a.skillRecorder.EntryCount()
}
