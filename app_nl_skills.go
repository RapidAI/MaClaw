package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// NLSkillEntry is a locally-persisted Skill definition in AppConfig.
type NLSkillEntry struct {
	Name          string        `json:"name"`
	Description   string        `json:"description"`
	Triggers      []string      `json:"triggers"`
	Steps         []NLSkillStep `json:"steps"`
	Status        string        `json:"status"` // "active", "disabled"
	CreatedAt     string        `json:"created_at"`
	Source        string        `json:"source"`         // "manual" | "learned"
	SourceProject string        `json:"source_project"` // originating project path
}

// NLSkillStep represents a single action within an NL Skill.
type NLSkillStep struct {
	Action  string                 `json:"action"`
	Params  map[string]interface{} `json:"params"`
	OnError string                 `json:"on_error"` // "stop" (default), "continue"
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

// loadSkills reads skill entries from config.
func (e *SkillExecutor) loadSkills() []NLSkillEntry {
	cfg, err := e.app.LoadConfig()
	if err != nil {
		return nil
	}
	return cfg.NLSkills
}

// saveSkills persists skill entries to config.
func (e *SkillExecutor) saveSkills(skills []NLSkillEntry) error {
	cfg, err := e.app.LoadConfig()
	if err != nil {
		return err
	}
	cfg.NLSkills = skills
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
	for _, s := range skills {
		if s.Name == name {
			return fmt.Errorf("skill %q already exists", name)
		}
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
	skills = append(skills, entry)
	return e.saveSkills(skills)
}

// Update modifies an existing Skill definition.
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
			return e.saveSkills(skills)
		}
	}
	return fmt.Errorf("skill %q not found", entry.Name)
}

// Delete removes a Skill by name.
func (e *SkillExecutor) Delete(name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	skills := e.loadSkills()
	for i, s := range skills {
		if s.Name == name {
			skills = append(skills[:i], skills[i+1:]...)
			return e.saveSkills(skills)
		}
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
		d := NLSkillDefinition{
			Name:          s.Name,
			Description:   s.Description,
			Triggers:      s.Triggers,
			Steps:         s.Steps,
			Status:        s.Status,
			Source:        s.Source,
			SourceProject: s.SourceProject,
		}
		if t, err := time.Parse(time.RFC3339, s.CreatedAt); err == nil {
			d.CreatedAt = t
		}
		defs = append(defs, d)
	}
	return defs
}

// Execute runs a Skill by name. Steps are executed sequentially; if a step
// fails and OnError is "stop" (default), execution halts.
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
	for i, step := range target.Steps {
		result, err := e.executeStep(step)
		if err != nil {
			errMsg := fmt.Sprintf("步骤 %d (%s) 失败: %s", i+1, step.Action, err.Error())
			if step.OnError == "continue" {
				results = append(results, errMsg)
				continue
			}
			results = append(results, errMsg)
			return strings.Join(results, "\n"), fmt.Errorf("skill execution stopped at step %d: %w", i+1, err)
		}
		results = append(results, result)
	}
	return strings.Join(results, "\n"), nil
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
			Tool:        tool,
			ProjectPath: projectPath,
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
		if e.mcpRegistry == nil {
			return "", fmt.Errorf("MCP registry not initialized")
		}
		return e.mcpRegistry.CallTool(serverID, toolName, args)

	default:
		return "", fmt.Errorf("unknown action: %s", step.Action)
	}
}

// --- Wails binding functions ---

// ListNLSkills returns all registered NL Skill definitions (Wails binding).
func (a *App) ListNLSkills() []NLSkillDefinition {
	if a.skillExecutor == nil {
		return nil
	}
	return a.skillExecutor.List()
}

// CreateNLSkill registers a new NL Skill definition (Wails binding).
func (a *App) CreateNLSkill(def NLSkillEntry) error {
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	return a.skillExecutor.Register(def)
}

// UpdateNLSkill updates an existing NL Skill definition (Wails binding).
func (a *App) UpdateNLSkill(def NLSkillEntry) error {
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	return a.skillExecutor.Update(def)
}

// DeleteNLSkill removes an NL Skill by name (Wails binding).
func (a *App) DeleteNLSkill(name string) error {
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	return a.skillExecutor.Delete(name)
}
