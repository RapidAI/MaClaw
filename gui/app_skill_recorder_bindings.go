package main

import (
	"fmt"
	"log"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// StartSkillRecording begins recording tool operations for skill generation.
// Returns a status message.
func (a *App) StartSkillRecording() string {
	if a.skillRecorder == nil {
		a.skillRecorder = NewSkillOperationRecorder()
	}

	workDir := a.GetCurrentProjectPath()
	if err := a.skillRecorder.Start(workDir); err != nil {
		return fmt.Sprintf("录制启动失败: %s", err)
	}

	log.Printf("[skill-recorder] recording started, workDir=%s", workDir)
	a.emitEvent("skill-recording-state-changed", map[string]interface{}{
		"recording": true,
		"count":     0,
	})
	return "ok"
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
	// The scanner will load the skill.yaml from disk — Source will be "file" in scanner's view.
	// To preserve Source="learned", register via SkillExecutor which persists to skill.json.
	if a.skillExecutor != nil {
		entry := corelib.NLSkillEntry{
			Name:          name,
			Description:   description,
			Source:        "learned",
			SourceProject: a.GetCurrentProjectPath(),
			Status:        "active",
			SkillDir:      skillDir,
			CreatedAt:     time.Now().Format(time.RFC3339),
		}
		if regErr := a.skillExecutor.Register(entry); regErr != nil {
			// If already exists, try Update instead (re-recording same skill name)
			entry.CreatedAt = "" // let Update preserve original creation time
			if updErr := a.skillExecutor.Update(entry); updErr != nil {
				log.Printf("[skill-recorder] register/update in executor failed (non-fatal): register=%v update=%v", regErr, updErr)
			}
		}
	}

	if a.cachedSkillScanner != nil {
		a.cachedSkillScanner.Invalidate()
	}

	a.emitEvent("skill-recording-state-changed", map[string]interface{}{
		"recording":      false,
		"pendingConfirm": false,
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

// GetSkillRecordingCount returns the number of operations recorded so far.
func (a *App) GetSkillRecordingCount() int {
	if a.skillRecorder == nil {
		return 0
	}
	return a.skillRecorder.EntryCount()
}
