package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"gopkg.in/yaml.v3"
)

// NLSkillEntry, NLSkillStep — see corelib_aliases.go

// SkillDiagEntry reports the scan result for a single skill directory.
type SkillDiagEntry struct {
	Dir    string `json:"dir"`
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// NLSkillDefinition is the Wails-facing view of a Skill.
type NLSkillDefinition struct {
	Name          string        `json:"name"`
	Description   string        `json:"description"`
	Triggers      []string      `json:"triggers"`
	Steps         []NLSkillStep `json:"steps"`
	Status        string        `json:"status"`
	CreatedAt     time.Time     `json:"created_at"`
	Source        string        `json:"source"`
	SourceProject string        `json:"source_project"`
	HubSkillID    string        `json:"hub_skill_id,omitempty"`
	HubVersion    string        `json:"hub_version,omitempty"`
	TrustLevel    string        `json:"trust_level,omitempty"`
	UsageCount    int           `json:"usage_count"`
	SuccessCount  int           `json:"success_count"`
	SuccessRate   float64       `json:"success_rate"` // computed: SuccessCount / UsageCount
	LastUsedAt    *time.Time    `json:"last_used_at,omitempty"`
	LastError     string        `json:"last_error,omitempty"`
}

// SkillExecutor manages and executes locally-defined NL Skills.
type SkillExecutor struct {
	app         *App
	mcpRegistry *MCPRegistry
	manager     *RemoteSessionManager
	mu          sync.RWMutex
}

// NewSkillExecutor creates a new client-side Skill executor.
func NewSkillExecutor(app *App, mcpRegistry *MCPRegistry, manager *RemoteSessionManager) *SkillExecutor {
	return &SkillExecutor{
		app:         app,
		mcpRegistry: mcpRegistry,
		manager:     manager,
	}
}

// loadSkills reads skill entries from config and merges skills discovered
// from on-disk YAML files under ~/.maclaw/data/skills/ and ~/.agents/skills/.
// Config-based skills take precedence over file-based ones with the same name.
func (e *SkillExecutor) loadSkills() []NLSkillEntry {
	cfg, err := e.app.LoadConfig()
	if err != nil {
		return nil
	}
	skills := cfg.NLSkills

	// Build a set of known skill names for dedup.
	known := make(map[string]bool, len(skills))
	for _, s := range skills {
		known[s.Name] = true
	}

	// Scan ~/.maclaw/data/skills/*/skill.yaml for file-based skills.
	fileSkills := e.scanSkillYAMLFiles()
	for _, fs := range fileSkills {
		if !known[fs.Name] {
			skills = append(skills, fs)
			known[fs.Name] = true
		}
	}

	return skills
}

// scanSkillYAMLFiles discovers skill definitions from all known skill
// directories (e.g. ~/.maclaw/data/skills, ~/.agents/skills) plus
// user-configured external directories via corelib.
func (e *SkillExecutor) scanSkillYAMLFiles() []NLSkillEntry {
	cfg, err := e.app.LoadConfig()
	if err != nil {
		return skill.ScanAllSkillDirs()
	}
	return skill.ScanAllSkillDirsWithExternal(cfg.ExternalSkillDirs)
}

// skillYAMLFile is a local alias for the corelib type, used by delete and diag.
type skillYAMLFile = skill.SkillYAMLFile

// saveSkills persists skill entries to config.
// File-based skills (source == "file") are excluded to avoid polluting config.json.
func (e *SkillExecutor) saveSkills(skills []NLSkillEntry) error {
	cfg, err := e.app.LoadConfig()
	if err != nil {
		return err
	}
	filtered := make([]NLSkillEntry, 0, len(skills))
	for _, s := range skills {
		if s.Source != "file" {
			filtered = append(filtered, s)
		}
	}
	cfg.NLSkills = filtered
	return e.app.SaveConfig(cfg)
}

// Register adds a new Skill definition.
func (e *SkillExecutor) Register(entry NLSkillEntry) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	name := strings.TrimSpace(entry.Name)
	if name == "" {
		return fmt.Errorf("skill name is required")
	}
	skills := e.loadSkills()
	primaryDir, primaryErr := skill.PrimarySkillsDir()
	for _, s := range skills {
		if s.Name != name {
			continue
		}
		if entry.Source == "hub" && s.Source == "file" && primaryErr == nil {
			extractedDir := filepath.Join(primaryDir, name)
			if filepath.Clean(s.SkillDir) == filepath.Clean(extractedDir) {
				continue
			}
		}
		return fmt.Errorf("skill %q already exists", name)
	}
	entry.Name = name
	if entry.Status == "" {
		entry.Status = "active"
	}
	if entry.CreatedAt == "" {
		entry.CreatedAt = time.Now().Format(time.RFC3339)
	}
	if entry.Source == "" {
		entry.Source = "manual"
	}
	if entry.Triggers == nil {
		entry.Triggers = []string{}
	}
	if entry.Steps == nil {
		entry.Steps = []NLSkillStep{}
	}
	skills = append(skills, entry)
	return e.saveSkills(skills)
}

// Update modifies an existing Skill definition.
// Usage tracking fields (UsageCount, SuccessCount, LastUsedAt, LastError)
// are preserved from the caller if non-zero, allowing the experience
// extractor to carry forward stats when replacing a pattern.
func (e *SkillExecutor) Update(entry NLSkillEntry) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	skills := e.loadSkills()
	for i, s := range skills {
		if s.Name == entry.Name {
			skills[i].Description = entry.Description
			skills[i].Triggers = entry.Triggers
			skills[i].Steps = entry.Steps
			skills[i].Status = entry.Status
			// Preserve usage stats from caller if provided (experience extractor
			// carries forward existing stats); otherwise keep what's on disk.
			if entry.UsageCount > 0 {
				skills[i].UsageCount = entry.UsageCount
				skills[i].SuccessCount = entry.SuccessCount
				skills[i].LastUsedAt = entry.LastUsedAt
				skills[i].LastError = entry.LastError
			}
			return e.saveSkills(skills)
		}
	}
	return fmt.Errorf("skill %q not found", entry.Name)
}

// UpdateFromHub checks for a newer version of a Hub Skill and updates it locally.
// It preserves Name, Source, HubSkillID, SourceProject, Status, and CreatedAt.
// Network calls are made outside the mutex to avoid blocking other skill operations.
func (e *SkillExecutor) UpdateFromHub(name string) error {
	// Phase 1: Read skill info under read lock.
	e.mu.RLock()
	skills := e.loadSkills()
	var skill NLSkillEntry
	found := false
	for _, s := range skills {
		if s.Name == name {
			skill = s
			found = true
			break
		}
	}
	e.mu.RUnlock()

	if !found {
		return fmt.Errorf("skill %q not found", name)
	}
	if skill.Source != "hub" || skill.HubSkillID == "" {
		return fmt.Errorf("skill %q is not a hub skill", name)
	}
	if e.app.skillHubClient == nil {
		return fmt.Errorf("skill hub client not initialized")
	}

	// Phase 2: Network calls without holding the lock.
	ctx := context.Background()

	meta, err := e.app.skillHubClient.CheckUpdate(ctx, skill.HubSkillID, skill.HubVersion)
	if err != nil {
		return fmt.Errorf("failed to check update for skill %q: %w", name, err)
	}
	if meta == nil {
		return nil // already up to date
	}

	updated, err := e.app.skillHubClient.Install(ctx, skill.HubSkillID, meta.HubURL)
	if err != nil {
		return fmt.Errorf("failed to download update for skill %q: %w", name, err)
	}

	// Phase 3: Apply update under write lock.
	e.mu.Lock()
	defer e.mu.Unlock()

	// Re-read skills in case they changed while we were doing network I/O.
	skills = e.loadSkills()
	idx := -1
	for i, s := range skills {
		if s.Name == name {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("skill %q was removed during update", name)
	}

	// Replace mutable fields, preserve identity fields.
	skills[idx].Description = updated.Description
	skills[idx].Triggers = updated.Triggers
	skills[idx].Steps = updated.Steps
	skills[idx].HubVersion = updated.HubVersion
	skills[idx].TrustLevel = updated.TrustLevel

	return e.saveSkills(skills)
}

// Delete removes a Skill by name.
func (e *SkillExecutor) Delete(name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	skills := e.loadSkills()
	found := false
	for i, s := range skills {
		if s.Name == name {
			found = true
			// Remove from config (only config-backed entries live here;
			// file-only skills won't be in the slice, but the flag still
			// gets set so we proceed to disk cleanup below).
			if s.Source != "file" {
				skills = append(skills[:i], skills[i+1:]...)
				if err := e.saveSkills(skills); err != nil {
					return err
				}
			}
			break
		}
	}
	if !found {
		return fmt.Errorf("skill %q not found", name)
	}
	// Always clean up on-disk skill directories so that loadSkills
	// (which scans disk via scanSkillYAMLFiles) won't rediscover it.
	e.removeSkillDirs(name)
	return nil
}

// removeSkillDirs scans all skill directories and removes any whose
// skill.yaml name matches the given name. Errors are silently ignored
// so that config deletion is never blocked by a disk cleanup failure.
func (e *SkillExecutor) removeSkillDirs(name string) {
	cfg, _ := e.app.LoadConfig()
	for _, root := range skill.SkillScanRootsWithExternal(cfg.ExternalSkillDirs) {
		entries, _ := os.ReadDir(root)
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			yamlPath := filepath.Join(root, entry.Name(), "skill.yaml")
			if _, err := os.Stat(yamlPath); err != nil {
				yamlPath = filepath.Join(root, entry.Name(), "skill.yml")
				if _, err := os.Stat(yamlPath); err != nil {
					continue
				}
			}
			data, err := os.ReadFile(yamlPath)
			if err != nil {
				continue
			}
			var sf skillYAMLFile
			if err := yaml.Unmarshal(data, &sf); err != nil {
				continue
			}
			parsedName := strings.TrimSpace(sf.Name)
			if parsedName == "" {
				parsedName = entry.Name()
			}
			if parsedName == name {
				_ = os.RemoveAll(filepath.Join(root, entry.Name()))
			}
		}
	}
}

// uploadStatusFile is a small JSON file stored alongside file-based skills
// to persist upload metadata that can't be saved in config.json.
type uploadStatusFile struct {
	SubmissionID string `json:"submission_id"`
	UploadedAt   string `json:"uploaded_at"`
}

// MarkUploaded records that a skill has been uploaded to SkillMarket.
// For config-based skills, it writes hub_skill_id into config.
// For file-based skills, it writes an upload_status.json next to skill.yaml.
func (e *SkillExecutor) MarkUploaded(name, submissionID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	skills := e.loadSkills()
	for i, s := range skills {
		if s.Name != name {
			continue
		}
		if s.Source == "file" && s.SkillDir != "" {
			// File-based skill: write upload_status.json to skill directory.
			status := uploadStatusFile{
				SubmissionID: submissionID,
				UploadedAt:   time.Now().Format(time.RFC3339),
			}
			data, err := json.MarshalIndent(status, "", "  ")
			if err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(s.SkillDir, "upload_status.json"), data, 0644)
		}
		// Config-based skill: persist in config.json.
		skills[i].HubSkillID = submissionID
		return e.saveSkills(skills)
	}
	return fmt.Errorf("skill %q not found", name)
}

// List returns all skill definitions.
func (e *SkillExecutor) List() []NLSkillDefinition {
	e.mu.RLock()
	defer e.mu.RUnlock()

	skills := e.loadSkills()
	defs := make([]NLSkillDefinition, 0, len(skills))
	for _, s := range skills {
		triggers := s.Triggers
		if triggers == nil {
			triggers = []string{}
		}
		steps := s.Steps
		if steps == nil {
			steps = []NLSkillStep{}
		}
		d := NLSkillDefinition{
			Name:          s.Name,
			Description:   s.Description,
			Triggers:      triggers,
			Steps:         steps,
			Status:        s.Status,
			Source:        s.Source,
			SourceProject: s.SourceProject,
			HubSkillID:    s.HubSkillID,
			HubVersion:    s.HubVersion,
			TrustLevel:    s.TrustLevel,
			UsageCount:    s.UsageCount,
			SuccessCount:  s.SuccessCount,
			LastError:     s.LastError,
		}
		if s.UsageCount > 0 {
			d.SuccessRate = float64(s.SuccessCount) / float64(s.UsageCount)
		}
		if t, err := time.Parse(time.RFC3339, s.CreatedAt); err == nil {
			d.CreatedAt = t
		}
		if s.LastUsedAt != "" {
			if t, err := time.Parse(time.RFC3339, s.LastUsedAt); err == nil {
				d.LastUsedAt = &t
			}
		}
		defs = append(defs, d)
	}
	return defs
}

// AsRegisteredTools converts all active NL Skills to corelib tool.RegisteredTool
// entries with Body populated from SKILL.md content. This is the bridge between
// the NL Skill system and the body-aware tool routing pipeline.
func (e *SkillExecutor) AsRegisteredTools() []tool.RegisteredTool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	skills := e.loadSkills()
	var result []tool.RegisteredTool
	for _, s := range skills {
		if s.Status != "active" {
			continue
		}
		body := e.readSkillBody(s)
		var bodySummary string
		if body != "" {
			bodySummary = tool.TruncateBody(body, tool.DefaultBodyMaxChars)
		}
		rt := tool.RegisteredTool{
			Name:        s.Name,
			Description: s.Description,
			Category:    tool.CategorySkill,
			Tags:        s.Triggers,
			Status:      tool.StatusAvailable,
			Body:        body,
			BodySummary: bodySummary,
		}
		result = append(result, rt)
	}
	return result
}

// readSkillBody reads the SKILL.md content for a skill entry.
// For file-based skills with a SkillDir, it reads SKILL.md from that directory.
// For hub/other skills without SkillDir, it checks the primary skills directory.
// Errors are logged as warnings and do not prevent skill registration.
func (e *SkillExecutor) readSkillBody(entry NLSkillEntry) string {
	// Try SkillDir first (file-based skills).
	if entry.SkillDir != "" {
		mdPath := filepath.Join(entry.SkillDir, "SKILL.md")
		data, err := os.ReadFile(mdPath)
		if err != nil {
			if !os.IsNotExist(err) {
				log.Printf("[SkillRegister] WARN: cannot read SKILL.md for %s: %v", entry.Name, err)
			}
			return ""
		}
		return string(data)
	}

	// For hub-installed skills, check the primary skills directory where
	// extractFiles writes SKILL.md during installation.
	if entry.Source == "hub" || entry.Source == "agent_skill" {
		primaryDir, err := skill.PrimarySkillsDir()
		if err != nil {
			return ""
		}
		mdPath := filepath.Join(primaryDir, entry.Name, "SKILL.md")
		data, err := os.ReadFile(mdPath)
		if err != nil {
			return ""
		}
		return string(data)
	}

	return ""
}

// Execute runs a Skill by name. Steps are executed sequentially; if a step
// fails and OnError is "stop" (default), execution halts.
// Usage statistics (count, success rate, last error) are updated after execution.
func (e *SkillExecutor) Execute(name string) (string, error) {
	e.mu.RLock()
	var target *NLSkillEntry
	for _, s := range e.loadSkills() {
		if s.Name == name && s.Status == "active" {
			cp := s
			target = &cp
			break
		}
	}
	e.mu.RUnlock()

	if target == nil {
		return "", fmt.Errorf("skill %q not found or disabled", name)
	}

	var results []string
	var execErr error
	for i, step := range target.Steps {
		result, err := e.executeStep(step)
		if err != nil {
			errMsg := fmt.Sprintf("步骤 %d (%s) 失败: %s", i+1, step.Action, err.Error())
			if step.OnError == "continue" {
				results = append(results, errMsg)
				continue
			}
			results = append(results, errMsg)
			execErr = fmt.Errorf("skill execution stopped at step %d: %w", i+1, err)
			break
		}
		results = append(results, result)
	}

	// Update usage statistics under write lock.
	// Skip for file-based skills since stats can't be persisted back to YAML.
	if target.Source != "file" {
		e.mu.Lock()
		skills := e.loadSkills()
		for i, s := range skills {
			if s.Name == name && s.Source != "file" {
				skills[i].UsageCount++
				skills[i].LastUsedAt = time.Now().Format(time.RFC3339)
				if execErr == nil {
					skills[i].SuccessCount++
					skills[i].LastError = ""
				} else {
					skills[i].LastError = execErr.Error()
				}
				_ = e.saveSkills(skills)

				// Auto-rate hub skills after execution.
				if s.Source == "hub" && s.HubSkillID != "" && e.app.capabilityGapDetector != nil {
					resultText := strings.Join(results, "\n")
					go e.app.capabilityGapDetector.autoRate(
						context.Background(), s.HubSkillID, resultText, execErr,
					)
				}
				break
			}
		}
		e.mu.Unlock()
	}

	output := strings.Join(results, "\n")
	if execErr != nil {
		return output, execErr
	}
	return output, nil
}

// executeStep runs a single skill step.
func (e *SkillExecutor) executeStep(step NLSkillStep) (string, error) {
	switch step.Action {
	case "create_session":
		tool, _ := step.Params["tool"].(string)
		projectPath, _ := step.Params["project_path"].(string)
		if tool == "" {
			return "", fmt.Errorf("missing tool parameter")
		}
		view, err := e.app.StartRemoteSessionForProject(RemoteStartSessionRequest{
			Tool:         tool,
			ProjectPath:  projectPath,
			LaunchSource: RemoteLaunchSourceAI,
		})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("会话已创建: ID=%s", view.ID), nil

	case "send_input":
		sessionID, _ := step.Params["session_id"].(string)
		text, _ := step.Params["text"].(string)
		if sessionID == "" || text == "" {
			return "", fmt.Errorf("missing session_id or text parameter")
		}
		if e.manager == nil {
			return "", fmt.Errorf("session manager not initialized")
		}
		if err := e.manager.WriteInput(sessionID, text); err != nil {
			return "", err
		}
		return fmt.Sprintf("已发送到会话 %s", sessionID), nil

	case "call_mcp_tool":
		serverID, _ := step.Params["server_id"].(string)
		toolName, _ := step.Params["tool_name"].(string)
		args, _ := step.Params["arguments"].(map[string]interface{})
		if serverID == "" || toolName == "" {
			return "", fmt.Errorf("missing server_id or tool_name parameter")
		}
		// Try local MCP manager first
		if mgr := e.app.localMCPManager; mgr != nil && mgr.IsRunning(serverID) {
			return mgr.CallTool(serverID, toolName, args)
		}
		// Fall back to remote MCP registry
		if e.mcpRegistry == nil {
			return "", fmt.Errorf("MCP registry not initialized")
		}
		return e.mcpRegistry.CallTool(serverID, toolName, args)

	case "bash":
		command, _ := step.Params["command"].(string)
		if command == "" {
			return "", fmt.Errorf("missing command parameter")
		}
		return executeBashStep(command, step.Params)

	default:
		return "", fmt.Errorf("unknown action: %s", step.Action)
	}
}

// --- Wails binding functions ---

// ListNLSkills returns all registered NL Skill definitions (Wails binding).
func (a *App) ListNLSkills() []NLSkillDefinition {
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return nil
	}
	return a.skillExecutor.List()
}

// DiagnoseSkillFiles scans ~/.maclaw/data/skills/ and reports load status for each
// subdirectory, including the reason if a skill failed to load (Wails binding).
func (a *App) DiagnoseSkillFiles() []SkillDiagEntry {
	skillsRoot, err := skill.PrimarySkillsDir()
	if err != nil {
		return []SkillDiagEntry{{Dir: "~", Reason: "无法获取用户主目录: " + err.Error()}}
	}

	// Check if directory exists at all.
	if info, err := os.Stat(skillsRoot); err != nil {
		if os.IsNotExist(err) {
			return []SkillDiagEntry{{Dir: skillsRoot, Reason: "skills 目录不存在，请创建 " + skillsRoot}}
		}
		return []SkillDiagEntry{{Dir: skillsRoot, Reason: "无法访问 skills 目录: " + err.Error()}}
	} else if !info.IsDir() {
		return []SkillDiagEntry{{Dir: skillsRoot, Reason: skillsRoot + " 不是目录"}}
	}

	entries, err := os.ReadDir(skillsRoot)
	if err != nil {
		return []SkillDiagEntry{{Dir: skillsRoot, Reason: "无法读取 skills 目录: " + err.Error()}}
	}
	if len(entries) == 0 {
		return []SkillDiagEntry{{Dir: skillsRoot, Reason: "skills 目录为空，没有子目录"}}
	}

	// Collect config-based skill names to detect dedup conflicts.
	configNames := make(map[string]bool)
	if cfg, err := a.LoadConfig(); err == nil {
		for _, s := range cfg.NLSkills {
			configNames[s.Name] = true
		}
	}

	var result []SkillDiagEntry
	for _, entry := range entries {
		dirName := entry.Name()
		dirPath := filepath.Join(skillsRoot, dirName)
		if !entry.IsDir() {
			result = append(result, SkillDiagEntry{Dir: dirName, Reason: "不是目录，已跳过"})
			continue
		}
		yamlPath := filepath.Join(dirPath, "skill.yaml")
		data, err := os.ReadFile(yamlPath)
		if err != nil {
			yamlPath = filepath.Join(dirPath, "skill.yml")
			data, err = os.ReadFile(yamlPath)
			if err != nil {
				result = append(result, SkillDiagEntry{Dir: dirName, Reason: "找不到 skill.yaml 或 skill.yml"})
				continue
			}
		}
		var sf skillYAMLFile
		if err := yaml.Unmarshal(data, &sf); err != nil {
			result = append(result, SkillDiagEntry{Dir: dirName, Reason: "YAML 解析失败: " + err.Error()})
			continue
		}
		name := strings.TrimSpace(sf.Name)
		if name == "" {
			name = dirName
		}
		if configNames[name] {
			result = append(result, SkillDiagEntry{Dir: dirName, Name: name, OK: false, Reason: "与配置中同名 Skill 冲突，被去重跳过"})
			continue
		}
		result = append(result, SkillDiagEntry{Dir: dirName, Name: name, OK: true})
	}
	return result
}

// ---------------------------------------------------------------------------
// External Skill Directories — Wails bindings
// ---------------------------------------------------------------------------

// ListExternalSkillDirs returns the user-configured external skill directories (Wails binding).
func (a *App) ListExternalSkillDirs() []string {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil
	}
	return cfg.ExternalSkillDirs
}

// ExternalSkillDirInfo is the Wails-facing view of an external skill directory.
type ExternalSkillDirInfo struct {
	Path       string `json:"path"`
	SkillCount int    `json:"skill_count"`
	Error      string `json:"error,omitempty"`
}

// ListExternalSkillDirsDetailed returns external dirs with skill counts (Wails binding).
func (a *App) ListExternalSkillDirsDetailed() []ExternalSkillDirInfo {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil
	}
	var result []ExternalSkillDirInfo
	for _, dir := range cfg.ExternalSkillDirs {
		count, verr := skill.ValidateExternalSkillDir(dir)
		info := ExternalSkillDirInfo{Path: dir, SkillCount: count}
		if verr != nil {
			info.Error = verr.Error()
		}
		result = append(result, info)
	}
	return result
}

// AddExternalSkillDir validates and adds an external skill directory (Wails binding).
func (a *App) AddExternalSkillDir(dir string) (int, error) {
	dir = filepath.Clean(dir)
	count, err := skill.ValidateExternalSkillDir(dir)
	if err != nil {
		return 0, err
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return 0, err
	}
	for _, d := range cfg.ExternalSkillDirs {
		if filepath.Clean(d) == dir {
			return 0, fmt.Errorf("directory already added")
		}
	}
	cfg.ExternalSkillDirs = append(cfg.ExternalSkillDirs, dir)
	return count, a.SaveConfig(cfg)
}

// RemoveExternalSkillDir removes an external skill directory from config (Wails binding).
func (a *App) RemoveExternalSkillDir(dir string) error {
	dir = filepath.Clean(dir)
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	var filtered []string
	found := false
	for _, d := range cfg.ExternalSkillDirs {
		if filepath.Clean(d) == dir {
			found = true
			continue
		}
		filtered = append(filtered, d)
	}
	if !found {
		return fmt.Errorf("directory not found in config")
	}
	cfg.ExternalSkillDirs = filtered
	return a.SaveConfig(cfg)
}

// CreateNLSkill registers a new NL Skill definition (Wails binding).
func (a *App) CreateNLSkill(def NLSkillEntry) error {
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	return a.skillExecutor.Register(def)
}

// UpdateNLSkill updates an existing NL Skill definition (Wails binding).
func (a *App) UpdateNLSkill(def NLSkillEntry) error {
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	return a.skillExecutor.Update(def)
}

// DeleteNLSkill removes an NL Skill by name (Wails binding).
func (a *App) DeleteNLSkill(name string) error {
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	return a.skillExecutor.Delete(name)
}

// ImportNLSkillZip opens a file dialog to select a zip file, validates it as a
// standard NL Skill package (must contain skill.json with valid NLSkillEntry),
// and registers the skill. Returns the imported skill name on success.
func (a *App) ImportNLSkillZip() (string, error) {
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return "", fmt.Errorf("skill executor not initialized")
	}

	// Open file dialog to select zip
	selection := a.SelectSkillFile()
	if selection == "" {
		return "", nil // user cancelled
	}

	// Open and validate zip
	r, err := zip.OpenReader(selection)
	if err != nil {
		return "", fmt.Errorf("无法打开 zip 文件: %v", err)
	}
	defer r.Close()

	// Find skill.json in the zip
	var skillJSON []byte
	for _, f := range r.File {
		name := strings.ToValidUTF8(f.Name, "")
		name = strings.ReplaceAll(name, "\\", "/")
		// Skip Mac/System junk
		parts := strings.Split(name, "/")
		if len(parts) > 0 && (strings.HasPrefix(parts[0], "__MACOSX") || strings.HasPrefix(parts[0], ".")) {
			continue
		}
		// Accept skill.json at root or inside a single top-level directory
		base := parts[len(parts)-1]
		if strings.EqualFold(base, "skill.json") && !f.FileInfo().IsDir() {
			rc, err := f.Open()
			if err != nil {
				return "", fmt.Errorf("无法读取 skill.json: %v", err)
			}
			skillJSON, err = io.ReadAll(io.LimitReader(rc, 1<<20)) // 1MB limit
			rc.Close()
			if err != nil {
				return "", fmt.Errorf("读取 skill.json 失败: %v", err)
			}
			break
		}
	}

	if skillJSON == nil {
		return "", fmt.Errorf("zip 包中未找到 skill.json，不是有效的 Skill 包")
	}

	// Parse skill.json
	var entry NLSkillEntry
	if err := json.Unmarshal(skillJSON, &entry); err != nil {
		return "", fmt.Errorf("skill.json 格式无效: %v", err)
	}

	// Validate required fields
	if strings.TrimSpace(entry.Name) == "" {
		return "", fmt.Errorf("skill.json 中缺少 name 字段")
	}
	if len(entry.Steps) == 0 {
		return "", fmt.Errorf("skill.json 中缺少 steps 定义")
	}

	// Mark source as imported zip
	entry.Source = "zip_import"

	// Register the skill
	if err := a.skillExecutor.Register(entry); err != nil {
		return "", err
	}

	return entry.Name, nil
}

// CleanupStaleSkills disables learned/crafted Skills that have been unused
// for over 30 days and have a success rate below 50% (or were never used).
// Returns the names of disabled Skills.
func (e *SkillExecutor) CleanupStaleSkills() []string {
	e.mu.Lock()
	defer e.mu.Unlock()

	skills := e.loadSkills()
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	var disabled []string

	for i, s := range skills {
		if s.Status != "active" {
			continue
		}
		// Only auto-cleanup learned and crafted skills; manual and hub skills are user-managed.
		if s.Source != "learned" && s.Source != "crafted" {
			continue
		}
		// Never used and older than 30 days.
		if s.UsageCount == 0 {
			created, err := time.Parse(time.RFC3339, s.CreatedAt)
			if err == nil && created.Before(cutoff) {
				skills[i].Status = "disabled"
				disabled = append(disabled, s.Name)
			}
			continue
		}
		// Used but low success rate and not recently used.
		successRate := float64(s.SuccessCount) / float64(s.UsageCount)
		if successRate < 0.5 {
			lastUsed, err := time.Parse(time.RFC3339, s.LastUsedAt)
			if err == nil && lastUsed.Before(cutoff) {
				skills[i].Status = "disabled"
				disabled = append(disabled, s.Name)
			}
		}
	}

	if len(disabled) > 0 {
		_ = e.saveSkills(skills)
	}
	return disabled
}

// CleanupStaleNLSkills disables stale learned/crafted Skills (Wails binding).
func (a *App) CleanupStaleNLSkills() []string {
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return nil
	}
	return a.skillExecutor.CleanupStaleSkills()
}

// ── Skill Runner Wails 绑定 ─────────────────────────────────────────────

// RunNLSkillAsync 异步启动 skill 执行，返回 runID（Wails binding）。
func (a *App) RunNLSkillAsync(skillName string) (string, error) {
	a.ensureInteractionInfra()
	if a.skillRunner == nil {
		return "", fmt.Errorf("skill runner not initialized")
	}
	return a.skillRunner.StartRun(skillName)
}

// GetNLSkillRunStatus 获取 skill 执行状态（Wails binding）。
func (a *App) GetNLSkillRunStatus(runID string) (*SkillRunStatus, error) {
	a.ensureInteractionInfra()
	if a.skillRunner == nil {
		return nil, fmt.Errorf("skill runner not initialized")
	}
	return a.skillRunner.GetRunStatus(runID)
}

// CancelNLSkillRun 取消正在执行的 skill（Wails binding）。
func (a *App) CancelNLSkillRun(runID string) error {
	a.ensureInteractionInfra()
	if a.skillRunner == nil {
		return fmt.Errorf("skill runner not initialized")
	}
	return a.skillRunner.CancelRun(runID)
}

// UploadNLSkillToMarket 手动打包并上传 skill 到 SkillMarket（Wails binding）。
func (a *App) UploadNLSkillToMarket(skillName string) (string, error) {
	a.ensureInteractionInfra()
	if a.skillExecutor == nil {
		return "", fmt.Errorf("skill executor not initialized")
	}
	if a.skillMarketClient == nil {
		return "", fmt.Errorf("skill market client not initialized")
	}

	// 打包 skill
	zipPath, err := a.packageSkillForMarket(skillName)
	if err != nil {
		return "", fmt.Errorf("打包失败: %w", err)
	}
	defer os.Remove(zipPath)

	// 获取用户 email
	cfg, err := a.LoadConfig()
	if err != nil {
		return "", fmt.Errorf("加载配置失败: %w", err)
	}
	email := strings.TrimSpace(cfg.RemoteEmail)
	if email == "" {
		return "", fmt.Errorf("未配置 remote_email，无法上传到 SkillMarket")
	}

	// 上传
	submissionID, err := a.skillMarketClient.SubmitSkill(context.Background(), zipPath, email)
	if err != nil {
		return "", fmt.Errorf("上传失败: %w", err)
	}

	// 上传成功后，标记 skill 已上传
	_ = a.skillExecutor.MarkUploaded(skillName, submissionID)

	return submissionID, nil
}

// packageSkillForMarket 将 skill 打包为 SkillMarket 规范的 zip 文件。
// 对于 file-based skill，直接打包 skill 目录。
// 对于 config-based skill，生成 skill.json + skill.yaml 到临时目录后打包。
func (a *App) packageSkillForMarket(skillName string) (string, error) {
	a.skillExecutor.mu.RLock()
	var target *NLSkillEntry
	for _, s := range a.skillExecutor.loadSkills() {
		if s.Name == skillName {
			cp := s
			target = &cp
			break
		}
	}
	a.skillExecutor.mu.RUnlock()

	if target == nil {
		return "", fmt.Errorf("skill %q not found", skillName)
	}

	// 验证平台字段
	if len(target.Platforms) == 0 {
		target.Platforms = []string{"universal"}
	}

	tmpDir, err := os.MkdirTemp("", "skill-package-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	// 如果是 file-based skill，复制整个 skill 目录内容
	if target.SkillDir != "" {
		if err := copyDirContents(target.SkillDir, tmpDir); err != nil {
			return "", fmt.Errorf("复制 skill 目录失败: %w", err)
		}
	}

	// 写入 skill.json（SkillMarket 标准格式）
	// 清除运行时字段，避免泄露本机路径
	target.SkillDir = ""
	target.LastError = ""
	skillJSON, err := json.MarshalIndent(target, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "skill.json"), skillJSON, 0644); err != nil {
		return "", err
	}

	// 生成 skill.yaml（服务端 ValidatePackage 必需）
	// 如果 file-based skill 已自带 skill.yaml，跳过生成
	yamlPath := filepath.Join(tmpDir, "skill.yaml")
	if _, statErr := os.Stat(yamlPath); statErr != nil {
		skillYAML := map[string]interface{}{
			"name":        target.Name,
			"description": target.Description,
		}
		if len(target.Triggers) > 0 {
			skillYAML["triggers"] = target.Triggers
		}
		if len(target.Platforms) > 0 {
			skillYAML["platforms"] = target.Platforms
		}
		if target.RequiresGUI {
			skillYAML["requires_gui"] = true
		}
		yamlData, err := yaml.Marshal(skillYAML)
		if err != nil {
			return "", fmt.Errorf("生成 skill.yaml 失败: %w", err)
		}
		if err := os.WriteFile(yamlPath, yamlData, 0644); err != nil {
			return "", err
		}
	}

	// 打包为 zip
	zipPath := filepath.Join(a.GetTempDir(), fmt.Sprintf("skill-%s-%d.zip", toKebabCase(skillName), time.Now().UnixMilli()))
	if err := zipDirectory(tmpDir, zipPath); err != nil {
		return "", err
	}
	return zipPath, nil
}

// executeBashStep runs a shell command as a skill step.
// Supports optional "working_dir" and "timeout" params.
func executeBashStep(command string, params map[string]interface{}) (string, error) {
	timeout := 30
	if t, ok := params["timeout"].(float64); ok && t > 0 {
		timeout = int(t)
		if timeout > 120 {
			timeout = 120
		}
	}

	workDir, _ := params["working_dir"].(string)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	var shellName string
	var shellArgs []string
	if runtime.GOOS == "windows" {
		shellName = "powershell"
		shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command", command}
	} else {
		shellName = "bash"
		shellArgs = []string{"-c", command}
	}

	cmd := exec.CommandContext(ctx, shellName, shellArgs...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	hideCommandWindow(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	var b strings.Builder
	if stdout.Len() > 0 {
		out := stdout.String()
		if len(out) > 8192 {
			out = out[:8192] + "\n... (truncated)"
		}
		b.WriteString(out)
	}
	if stderr.Len() > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		errOut := stderr.String()
		if len(errOut) > 4096 {
			errOut = errOut[:4096] + "\n... (truncated)"
		}
		b.WriteString("[stderr] ")
		b.WriteString(errOut)
	}
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			b.WriteString(fmt.Sprintf("\n[error] timeout after %ds", timeout))
		} else {
			b.WriteString(fmt.Sprintf("\n[error] %v", err))
		}
		return b.String(), err
	}
	if b.Len() == 0 {
		return "(completed, no output)", nil
	}
	return b.String(), nil
}

// ── 文件系统 helper ─────────────────────────────────────────────────────

// copyDirContents 将 src 目录下的所有文件/子目录复制到 dst 目录。
func copyDirContents(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

// zipDirectory 将 srcDir 目录打包为 zip 文件。
func zipDirectory(srcDir, zipPath string) error {
	outFile, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	zw := zip.NewWriter(outFile)
	defer zw.Close()

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		// 使用 forward slash 作为 zip 内路径分隔符
		zipName := filepath.ToSlash(rel)
		if info.IsDir() {
			zipName += "/"
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = zipName
		if !info.IsDir() {
			header.Method = zip.Deflate
		}

		w, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
}
