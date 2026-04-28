package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// downloadSkillJSONFromHubCenter fetches a skill definition through the
// current HubCenter discovery/failover pool.
func downloadSkillJSONFromHubCenter(ctx context.Context, app *App, path string) (*corelib.NLSkillEntry, error) {
	if app == nil {
		return nil, fmt.Errorf("app is nil")
	}
	_, _, data, err := app.getHubCenterBytes(ctx, &http.Client{Timeout: 30 * time.Second}, path, 5<<20)
	if err != nil {
		return nil, err
	}
	return decodeDownloadedSkillJSON(data)
}

func decodeDownloadedSkillJSON(data []byte) (*corelib.NLSkillEntry, error) {
	var full struct {
		ID          string            `json:"id"`
		Name        string            `json:"name"`
		Description string            `json:"description"`
		Triggers    []string          `json:"triggers"`
		TrustLevel  string            `json:"trust_level"`
		Version     string            `json:"version"`
		Steps       []json.RawMessage `json:"steps"`
		Files       map[string]string `json:"files"`
	}
	if err := json.Unmarshal(data, &full); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	var steps []corelib.NLSkillStep
	if len(full.Steps) > 0 {
		for _, raw := range full.Steps {
			var s struct {
				Action  string                 `json:"action"`
				Params  map[string]interface{} `json:"params"`
				OnError string                 `json:"on_error"`
			}
			if err := json.Unmarshal(raw, &s); err == nil && s.Action != "" {
				steps = append(steps, corelib.NLSkillStep{Action: s.Action, Params: s.Params, OnError: s.OnError})
			}
		}
	}

	installSkillDir := ""
	if len(full.Files) > 0 && full.Name != "" {
		extractSkillFiles(full.Name, full.Files, "")
		if skillsRoot, err := cskill.PrimarySkillsDir(); err == nil {
			installSkillDir = filepath.Join(skillsRoot, full.Name)
		}
	}
	if len(steps) == 0 {
		steps = craftToolStepsFromBundledSkillFiles(full.Files, installSkillDir)
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("skill %s has no steps and no SKILL.md", full.Name)
	}

	trustLevel := full.TrustLevel
	if trustLevel == "" || trustLevel == "community" {
		trustLevel = "trusted"
	}

	return &corelib.NLSkillEntry{
		Name:        full.Name,
		Description: full.Description,
		Triggers:    full.Triggers,
		Steps:       steps,
		Status:      "active",
		CreatedAt:   time.Now().Format(time.RFC3339),
		Source:      "hub",
		HubSkillID:  full.ID,
		HubVersion:  full.Version,
		TrustLevel:  trustLevel,
	}, nil
}
