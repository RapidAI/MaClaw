package main

import (
	"fmt"
	"log"

	"github.com/RapidAI/CodeClaw/corelib"
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
	if projectPath := a.resolveProjectPathForTab(tabID); projectPath != "" {
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
	// Project tabs: look up cached projectPath → synthesize ownerID
	if projectPath := a.resolveProjectPathForTab(tabID); projectPath != "" {
		return desktopUserID + ":" + projectPath
	}
	// Fallback: use desktop-user (local tab behavior)
	return desktopUserID
}

// resolveProjectPathForTab returns the project path for a given tabID, or empty string.
func (a *App) resolveProjectPathForTab(tabID string) string {
	if tabID == "" || tabID == "local" {
		return ""
	}
	if v, ok := a.tabProjectPaths.Load(tabID); ok {
		if path, ok := v.(string); ok && path != "" {
			return path
		}
	}
	return ""
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
	summary := a.skillRecorder.OperationSummary()
	count := a.skillRecorder.EntryCount()

	// Scan for potential credential leakage
	a.skillRecorder.mu.Lock()
	entriesCopy := make([]RecordedOp, len(a.skillRecorder.entries))
	copy(entriesCopy, a.skillRecorder.entries)
	a.skillRecorder.mu.Unlock()
	credWarnings := detectCredentialWarnings(entriesCopy)

	a.emitEvent("skill-recording-state-changed", map[string]interface{}{
		"recording":      false,
		"pendingConfirm": true,
		"count":          count,
		"tabId":          a.skillRecorder.TabID(),
	})

	result := map[string]interface{}{
		"suggested_name":        suggestedName,
		"suggested_description": a.skillRecorder.SuggestDescription(),
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

	// action == "save"
	skillDir, err := a.skillRecorder.Stop(name, description)
	if err != nil {
		log.Printf("[skill-recorder] stop failed: %v", err)
		return map[string]interface{}{"error": err.Error()}
	}

	log.Printf("[skill-recorder] skill saved: name=%s dir=%s", name, skillDir)

	// Refresh skill indexes so the new skill is immediately available.
	// Scanner loads skill.yaml from disk; a thin config overlay preserves
	// Source="learned" without duplicating steps into config.json.
	if a.skillExecutor != nil {
		entry := corelib.NLSkillEntry{
			Name:     name,
			Source:   "learned",
			SkillDir: skillDir,
			Status:   "active",
		}
		if regErr := a.skillExecutor.UpdateLearnedSource(entry); regErr != nil {
			log.Printf("[skill-recorder] learned source overlay failed (non-fatal): %v", regErr)
		}
	}

	if a.cachedSkillScanner != nil {
		a.cachedSkillScanner.Invalidate()
	}

	a.emitEvent("skill-recording-state-changed", map[string]interface{}{
		"recording":      false,
		"pendingConfirm": false,
		"tabId":          a.skillRecorder.TabID(),
	})

	return map[string]interface{}{
		"status":    "saved",
		"name":      name,
		"skill_dir": skillDir,
	}
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
