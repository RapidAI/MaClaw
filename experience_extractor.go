package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ExperienceExtractor analyses completed remote sessions and extracts
// reusable operation patterns, registering them as NL Skills.
type ExperienceExtractor struct {
	app           *App
	skillExecutor *SkillExecutor
	llmConfig     MaclawLLMConfig
	client        *http.Client
}

// NewExperienceExtractor creates a new ExperienceExtractor.
func NewExperienceExtractor(app *App, skillExecutor *SkillExecutor, cfg MaclawLLMConfig) *ExperienceExtractor {
	return &ExperienceExtractor{
		app:           app,
		skillExecutor: skillExecutor,
		llmConfig:     cfg,
		client:        &http.Client{Timeout: 30 * time.Second},
	}
}

// extractedPattern is the JSON structure returned by the LLM.
type extractedPattern struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Triggers    []string        `json:"triggers"`
	Steps       []extractedStep `json:"steps"`
}

type extractedStep struct {
	Action  string                 `json:"action"`
	Params  map[string]interface{} `json:"params"`
	OnError string                 `json:"on_error"`
}

// Extract analyses the session history via LLM and registers any discovered
// patterns as NL Skills.  It silently returns nil when the LLM is not
// configured or the session has no meaningful output.
func (e *ExperienceExtractor) Extract(session *RemoteSession) error {
	// Skip when LLM is not configured.
	if !e.isConfigured() {
		return nil
	}

	if session == nil {
		return nil
	}

	// Build a textual summary of the session history.
	history := e.buildSessionHistory(session)
	if strings.TrimSpace(history) == "" {
		return nil
	}

	patterns, err := e.callLLM(history)
	if err != nil {
		// LLM errors are non-fatal; we simply skip extraction.
		return fmt.Errorf("experience extraction LLM call failed: %w", err)
	}

	for _, p := range patterns {
		if err := e.registerPattern(p, session); err != nil {
			// Log but continue with remaining patterns.
			continue
		}
	}
	return nil
}

// isConfigured returns true when the LLM endpoint and model are set.
func (e *ExperienceExtractor) isConfigured() bool {
	return strings.TrimSpace(e.llmConfig.URL) != "" &&
		strings.TrimSpace(e.llmConfig.Model) != ""
}

// buildSessionHistory constructs a textual representation of the session
// for the LLM prompt, combining raw output lines and important events.
func (e *ExperienceExtractor) buildSessionHistory(session *RemoteSession) string {
	session.mu.RLock()
	rawLines := make([]string, len(session.RawOutputLines))
	copy(rawLines, session.RawOutputLines)
	events := make([]ImportantEvent, len(session.Events))
	copy(events, session.Events)
	tool := session.Tool
	title := session.Title
	projectPath := session.ProjectPath
	session.mu.RUnlock()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Tool: %s\n", tool))
	sb.WriteString(fmt.Sprintf("Title: %s\n", title))
	sb.WriteString(fmt.Sprintf("Project: %s\n\n", projectPath))

	if len(events) > 0 {
		sb.WriteString("=== Important Events ===\n")
		for _, ev := range events {
			sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", ev.Type, ev.Title, ev.Summary))
		}
		sb.WriteString("\n")
	}

	if len(rawLines) > 0 {
		sb.WriteString("=== Session Output (last 200 lines) ===\n")
		start := 0
		if len(rawLines) > 200 {
			start = len(rawLines) - 200
		}
		for _, line := range rawLines[start:] {
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// callLLM sends the session history to the configured LLM and parses the
// response into a list of extracted patterns.
func (e *ExperienceExtractor) callLLM(history string) ([]extractedPattern, error) {
	systemPrompt := `You are an expert at analysing coding session histories and extracting reusable operation patterns.
Given a session history, identify reusable patterns and return them as a JSON array.
Each pattern must have:
- "name": a short, descriptive kebab-case name
- "description": what the pattern does
- "triggers": list of keywords or phrases that would trigger this pattern
- "steps": list of steps, each with "action" (create_session/send_input/call_mcp_tool), "params" (key-value map), and optional "on_error" ("stop" or "continue")

Return ONLY a JSON array of patterns. If no reusable patterns are found, return an empty array [].`

	userPrompt := fmt.Sprintf("Analyse the following session history and extract reusable operation patterns:\n\n%s", history)

	messages := []interface{}{
		map[string]string{"role": "system", "content": systemPrompt},
		map[string]string{"role": "user", "content": userPrompt},
	}

	result, err := doSimpleLLMRequest(e.llmConfig, messages, e.client, 30*time.Second)
	if err != nil {
		return nil, err
	}

	content := strings.TrimSpace(result.Content)
	content = stripCodeFences(content)

	var patterns []extractedPattern
	if err := json.Unmarshal([]byte(content), &patterns); err != nil {
		return nil, fmt.Errorf("parse patterns JSON: %w", err)
	}
	return patterns, nil
}

// stripCodeFences removes optional ```json ... ``` wrapping from LLM output.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// Remove opening fence line.
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
		// Remove closing fence.
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
		s = strings.TrimSpace(s)
	}
	return s
}

// registerPattern converts an extracted pattern to an NLSkillEntry and
// registers or updates it via the SkillExecutor.
func (e *ExperienceExtractor) registerPattern(p extractedPattern, session *RemoteSession) error {
	name := strings.TrimSpace(p.Name)
	if name == "" || len(p.Steps) == 0 {
		return nil // skip invalid patterns
	}

	steps := make([]NLSkillStep, 0, len(p.Steps))
	for _, s := range p.Steps {
		step := NLSkillStep{
			Action:  s.Action,
			Params:  s.Params,
			OnError: s.OnError,
		}
		if step.OnError == "" {
			step.OnError = "stop"
		}
		steps = append(steps, step)
	}

	session.mu.RLock()
	projectPath := session.ProjectPath
	session.mu.RUnlock()

	entry := NLSkillEntry{
		Name:          name,
		Description:   p.Description,
		Triggers:      p.Triggers,
		Steps:         steps,
		Status:        "active",
		CreatedAt:     time.Now().Format(time.RFC3339),
		Source:        "learned",
		SourceProject: projectPath,
	}

	// Check if a skill with the same name already exists.
	existing := e.findSkillByName(name)
	if existing != nil {
		// Only update if the new pattern has more steps (more detailed).
		if len(steps) > len(existing.Steps) {
			entry.CreatedAt = existing.CreatedAt // preserve original creation time
			return e.skillExecutor.Update(entry)
		}
		// Existing skill is equally or more detailed; skip.
		return nil
	}

	return e.skillExecutor.Register(entry)
}

// findSkillByName looks up an existing skill by name.
func (e *ExperienceExtractor) findSkillByName(name string) *NLSkillEntry {
	skills := e.skillExecutor.loadSkills()
	for _, s := range skills {
		if s.Name == name {
			cp := s
			return &cp
		}
	}
	return nil
}
