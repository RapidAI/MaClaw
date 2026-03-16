package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// NLSkillStep represents a single action within an NL Skill.
type NLSkillStep struct {
	Action  string                 `json:"action"`
	Params  map[string]interface{} `json:"params"`
	OnError string                 `json:"on_error"`
}

// NLSkillDefinition mirrors the hub's skill.SkillDefinition for Wails bindings.
type NLSkillDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Triggers    []string    `json:"triggers"`
	Steps       []NLSkillStep `json:"steps"`
	Status      string      `json:"status"`
	CreatedAt   time.Time   `json:"created_at"`
}

// nlSkillHubURL returns the base hub URL from config, or an error if not configured.
func (a *App) nlSkillHubURL() (string, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	if hubURL == "" {
		return "", fmt.Errorf("remote hub URL is not configured")
	}
	return hubURL, nil
}

// nlSkillGet performs an HTTP GET to the hub and decodes the JSON response.
func (a *App) nlSkillGet(path string, target interface{}) error {
	hubURL, err := a.nlSkillHubURL()
	if err != nil {
		return err
	}
	resp, err := http.Get(hubURL + path)
	if err != nil {
		return fmt.Errorf("hub request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("hub returned status %d: %s", resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

// nlSkillPost performs an HTTP POST to the hub with a JSON body and checks the response.
func (a *App) nlSkillPost(path string, payload interface{}) error {
	hubURL, err := a.nlSkillHubURL()
	if err != nil {
		return err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}
	resp, err := http.Post(hubURL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("hub request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("hub returned status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// nlSkillDelete performs an HTTP DELETE to the hub and checks the response.
func (a *App) nlSkillDelete(path string) error {
	hubURL, err := a.nlSkillHubURL()
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodDelete, hubURL+path, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("hub request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("hub returned status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// ListNLSkills returns all registered NL Skill definitions from the hub.
func (a *App) ListNLSkills() []NLSkillDefinition {
	var skills []NLSkillDefinition
	if err := a.nlSkillGet("/api/admin/nl-skills", &skills); err != nil {
		a.log("ListNLSkills error: " + err.Error())
		return nil
	}
	return skills
}

// CreateNLSkill registers a new NL Skill definition on the hub.
func (a *App) CreateNLSkill(def NLSkillDefinition) error {
	return a.nlSkillPost("/api/admin/nl-skills", def)
}

// UpdateNLSkill updates an existing NL Skill definition on the hub.
func (a *App) UpdateNLSkill(def NLSkillDefinition) error {
	return a.nlSkillPost("/api/admin/nl-skills/update", def)
}

// DeleteNLSkill removes an NL Skill by name from the hub.
func (a *App) DeleteNLSkill(name string) error {
	return a.nlSkillDelete("/api/admin/nl-skills/" + name)
}

// ListCandidateSkills returns all candidate Skill definitions from the hub's crystallizer.
func (a *App) ListCandidateSkills() []NLSkillDefinition {
	var candidates []NLSkillDefinition
	if err := a.nlSkillGet("/api/admin/nl-skills/candidates", &candidates); err != nil {
		a.log("ListCandidateSkills error: " + err.Error())
		return nil
	}
	return candidates
}

// ConfirmCandidateSkill confirms a candidate Skill, promoting it to active status.
func (a *App) ConfirmCandidateSkill(def NLSkillDefinition) error {
	return a.nlSkillPost("/api/admin/nl-skills/candidates/confirm", def)
}

// IgnoreCandidateSkill ignores a candidate Skill, removing it from the candidates list.
func (a *App) IgnoreCandidateSkill(name string) error {
	return a.nlSkillPost("/api/admin/nl-skills/candidates/ignore", map[string]string{"name": name})
}

// UploadNLSkillPackageResult holds the result of a skill package upload.
type UploadNLSkillPackageResult struct {
	OK       bool     `json:"ok"`
	Imported []string `json:"imported"`
	Errors   []string `json:"errors"`
	Total    int      `json:"total"`
}

// UploadNLSkillPackage opens a file dialog for the user to select a .zip skill
// package, then uploads it to the hub for import.
func (a *App) UploadNLSkillPackage() (*UploadNLSkillPackageResult, error) {
	selection, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择技能包 (Zip)",
		Filters: []runtime.FileFilter{
			{DisplayName: "Zip Files (*.zip)", Pattern: "*.zip"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("file dialog error: %w", err)
	}
	if selection == "" {
		return nil, nil // user cancelled
	}

	hubURL, err := a.nlSkillHubURL()
	if err != nil {
		return nil, err
	}

	// Read the selected file.
	fileData, err := os.ReadFile(selection)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Build multipart form.
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filepath.Base(selection))
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(fileData); err != nil {
		return nil, fmt.Errorf("failed to write form file: %w", err)
	}
	writer.Close()

	resp, err := http.Post(hubURL+"/api/admin/nl-skills/upload", writer.FormDataContentType(), body)
	if err != nil {
		return nil, fmt.Errorf("hub request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("hub returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result UploadNLSkillPackageResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &result, nil
}
