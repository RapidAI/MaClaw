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
// configured, the session has no meaningful output, or the session exited
// with a non-zero code (failed sessions are poor pattern candidates).
func (e *ExperienceExtractor) Extract(session *RemoteSession) error {
	// Skip when LLM is not configured.
	if !e.isConfigured() {
		return nil
	}

	if session == nil {
		return nil
	}

	// Skip sessions that exited with errors — they're unlikely to contain
	// good reusable patterns.
	session.mu.RLock()
	var exitCodeVal int
	hasExitCode := session.ExitCode != nil
	if hasExitCode {
		exitCodeVal = *session.ExitCode
	}
	eventCount := len(session.Events)
	session.mu.RUnlock()
	if hasExitCode && exitCodeVal != 0 {
		return nil
	}
	// Skip sessions with no events — too little signal to extract from.
	if eventCount == 0 {
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
// for the LLM prompt, combining important events and a filtered subset of
// raw output. Events are prioritized over raw output for better signal.
func (e *ExperienceExtractor) buildSessionHistory(session *RemoteSession) string {
	session.mu.RLock()
	rawLines := make([]string, len(session.RawOutputLines))
	copy(rawLines, session.RawOutputLines)
	events := make([]ImportantEvent, len(session.Events))
	copy(events, session.Events)
	tool := session.Tool
	title := session.Title
	projectPath := session.ProjectPath
	var exitCode *int
	if session.ExitCode != nil {
		cp := *session.ExitCode
		exitCode = &cp
	}
	session.mu.RUnlock()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Tool: %s\n", tool))
	sb.WriteString(fmt.Sprintf("Title: %s\n", title))
	sb.WriteString(fmt.Sprintf("Project: %s\n", projectPath))
	if exitCode != nil {
		sb.WriteString(fmt.Sprintf("Exit Code: %d\n", *exitCode))
	}
	sb.WriteString("\n")

	// Events are the most valuable signal for pattern extraction.
	if len(events) > 0 {
		sb.WriteString("=== Important Events ===\n")
		for _, ev := range events {
			sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", ev.Type, ev.Title, ev.Summary))
		}
		sb.WriteString("\n")
	}

	// For raw output, only include the last 100 lines (reduced from 200)
	// and skip empty/whitespace-only lines to reduce noise.
	if len(rawLines) > 0 {
		sb.WriteString("=== Session Output (filtered) ===\n")
		start := 0
		if len(rawLines) > 100 {
			start = len(rawLines) - 100
		}
		lineCount := 0
		for _, line := range rawLines[start:] {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			sb.WriteString(line)
			sb.WriteString("\n")
			lineCount++
		}
		if lineCount == 0 {
			sb.WriteString("(no meaningful output)\n")
		}
	}

	return sb.String()
}

// callLLM sends the session history to the configured LLM and parses the
// response into a list of extracted patterns.
func (e *ExperienceExtractor) callLLM(history string) ([]extractedPattern, error) {
	systemPrompt := `You are an expert at analysing coding session histories and extracting reusable operation patterns.
Given a session history, identify patterns that are GENUINELY reusable — not one-off tasks.

A good reusable pattern:
- Solves a recurring problem (e.g. "deploy to staging", "run test suite with coverage")
- Has clear, parameterizable steps (not hardcoded to one specific file/project)
- Would save time if automated

A bad pattern (DO NOT extract):
- One-off debugging sessions with no repeatable structure
- Simple single-command operations (e.g. just "git pull")
- Patterns too specific to one project's file paths

Return a JSON array. Each pattern must have:
- "name": a short, descriptive kebab-case name (e.g. "deploy-staging", "run-coverage-tests")
- "description": what the pattern does and when to use it
- "triggers": list of 3-5 keywords or phrases that would trigger this pattern
- "steps": list of steps, each with "action" (create_session/send_input/call_mcp_tool/bash), "params" (key-value map), and optional "on_error" ("stop" or "continue")

Return ONLY a JSON array. If no genuinely reusable patterns are found, return [].
Quality over quantity — only extract patterns you're confident are reusable.`

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
// Quality assessment: compares step count, description detail, and trigger
// coverage to decide whether to update an existing skill.
func (e *ExperienceExtractor) registerPattern(p extractedPattern, session *RemoteSession) error {
	name := strings.TrimSpace(p.Name)
	if name == "" || len(p.Steps) == 0 {
		return nil // skip invalid patterns
	}

	// Validate step actions — reject patterns with unknown actions.
	validActions := map[string]bool{
		"create_session": true, "send_input": true,
		"call_mcp_tool": true, "bash": true, "skill_md": true,
	}
	for _, s := range p.Steps {
		if !validActions[s.Action] {
			return nil // skip patterns with unknown actions
		}
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
		// Quality comparison: new pattern must be meaningfully better.
		if !e.isPatternBetter(p, existing) {
			return nil
		}
		entry.CreatedAt = existing.CreatedAt // preserve original creation time
		entry.UsageCount = existing.UsageCount
		entry.SuccessCount = existing.SuccessCount
		entry.LastUsedAt = existing.LastUsedAt
		return e.skillExecutor.Update(entry)
	}

	return e.skillExecutor.Register(entry)
}

// isPatternBetter returns true if the new pattern is meaningfully better
// than the existing skill. Considers step count, description detail, and
// trigger keyword coverage.
func (e *ExperienceExtractor) isPatternBetter(newP extractedPattern, existing *NLSkillEntry) bool {
	score := 0

	// More steps = more detailed workflow.
	if len(newP.Steps) > len(existing.Steps) {
		score += 2
	}

	// Longer description = more context.
	if len(newP.Description) > len(existing.Description)+20 {
		score++
	}

	// More trigger keywords = better discoverability.
	if len(newP.Triggers) > len(existing.Triggers) {
		score++
	}

	// If existing skill has a poor success rate, prefer the new pattern.
	if existing.UsageCount >= 3 && existing.SuccessCount*2 < existing.UsageCount {
		score += 2
	}

	return score >= 2
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
