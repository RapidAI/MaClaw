package experience

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

type LLMClient interface {
	Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

type SkillStore interface {
	List() []corelib.NLSkillEntry
	Register(corelib.NLSkillEntry) error
	Update(corelib.NLSkillEntry) error
}

type Extractor struct {
	llm   LLMClient
	store SkillStore
	now   func() time.Time
	opts  Options
}

func NewExtractor(llm LLMClient, store SkillStore) *Extractor {
	return NewExtractorWithOptions(llm, store, Options{})
}

func NewExtractorWithOptions(llm LLMClient, store SkillStore, opts Options) *Extractor {
	return &Extractor{llm: llm, store: store, now: time.Now, opts: normalizeOptions(opts)}
}

type DecisionAction string

const (
	DecisionRegistered DecisionAction = "registered"
	DecisionUpdated    DecisionAction = "updated"
	DecisionSkipped    DecisionAction = "skipped"
)

type Decision struct {
	PatternName      string         `json:"pattern_name"`
	Action           DecisionAction `json:"action"`
	Reason           string         `json:"reason,omitempty"`
	MatchedSkillName string         `json:"matched_skill_name,omitempty"`
	Quality          QualityReport  `json:"quality"`
	Evidence         EvidenceReport `json:"evidence"`
}

type Result struct {
	Upserted  []corelib.NLSkillEntry `json:"upserted"`
	Decisions []Decision             `json:"decisions"`
}

// ResultSummary is a compact, stable view of an extraction run for logs,
// telemetry, and UI surfaces that do not need every decision detail.
type ResultSummary struct {
	TotalCandidates  int            `json:"total_candidates"`
	Registered       int            `json:"registered"`
	Updated          int            `json:"updated"`
	Skipped          int            `json:"skipped"`
	SkipReasons      map[string]int `json:"skip_reasons,omitempty"`
	UnsupportedSteps map[string]int `json:"unsupported_steps,omitempty"`
}

// Summary aggregates extraction decisions without losing the important failure
// modes that explain why experience was not persisted.
func (r Result) Summary() ResultSummary {
	summary := ResultSummary{TotalCandidates: len(r.Decisions)}
	for _, decision := range r.Decisions {
		switch decision.Action {
		case DecisionRegistered:
			summary.Registered++
		case DecisionUpdated:
			summary.Updated++
		case DecisionSkipped:
			summary.Skipped++
			if decision.Reason != "" {
				if summary.SkipReasons == nil {
					summary.SkipReasons = map[string]int{}
				}
				summary.SkipReasons[decision.Reason]++
			}
		}
		for _, step := range decision.Evidence.UnsupportedSteps {
			if step == "" {
				continue
			}
			if summary.UnsupportedSteps == nil {
				summary.UnsupportedSteps = map[string]int{}
			}
			summary.UnsupportedSteps[step]++
		}
	}
	return summary
}

type ImportantEvent struct {
	Type    string
	Title   string
	Summary string
}

type SessionSnapshot struct {
	Tool              string
	Title             string
	ProjectPath       string
	ExitCode          *int
	StructuredSession bool
	Events            []ImportantEvent
	RawOutputLines    []string
}

type Pattern struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Triggers    []string `json:"triggers"`
	Steps       []Step   `json:"steps"`
}

type Step struct {
	Action  string                 `json:"action"`
	Params  map[string]interface{} `json:"params"`
	OnError string                 `json:"on_error"`
}

var learnedSkillNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$`)

var validStepActions = map[string]bool{
	"create_session":   true,
	"send_input":       true,
	"send_and_observe": true,
	"call_mcp_tool":    true,
	"bash":             true,
	"skill_md":         true,
}

func (e *Extractor) Extract(ctx context.Context, snapshot SessionSnapshot) ([]corelib.NLSkillEntry, error) {
	result, err := e.ExtractDetailed(ctx, snapshot)
	return result.Upserted, err
}

func (e *Extractor) ExtractDetailed(ctx context.Context, snapshot SessionSnapshot) (Result, error) {
	if e == nil || e.llm == nil || e.store == nil {
		return Result{}, nil
	}
	if !Eligible(snapshot) {
		return Result{}, nil
	}

	history := BuildSessionHistory(snapshot)
	if strings.TrimSpace(history) == "" {
		return Result{}, nil
	}

	content, err := e.llm.Generate(ctx, SystemPrompt(), fmt.Sprintf("Analyse the following session history and extract reusable operation patterns:\n\n%s", history))
	if err != nil {
		return Result{}, fmt.Errorf("experience extraction LLM call failed: %w", err)
	}

	patterns, err := ParsePatterns(RedactExperienceText(content))
	if err != nil {
		return Result{}, err
	}

	existing := e.store.List()
	result := Result{}

	for _, rawPattern := range patterns {
		p := GeneralizePattern(rawPattern, snapshot.ProjectPath)
		entry, quality, reason, ok := e.evaluatePattern(p, snapshot.ProjectPath)
		evidence := EvaluateEvidenceSupport(p, history)
		decision := Decision{PatternName: strings.TrimSpace(rawPattern.Name), Quality: quality, Evidence: evidence}
		if !ok {
			decision.Action = DecisionSkipped
			decision.Reason = reason
			result.Decisions = append(result.Decisions, decision)
			continue
		}
		if !evidence.PassesThreshold(e.opts.MinEvidenceScore) {
			decision.Action = DecisionSkipped
			decision.Reason = "insufficient session evidence"
			result.Decisions = append(result.Decisions, decision)
			continue
		}
		if len(result.Upserted) >= e.opts.MaxPatternsPerExtraction {
			decision.Action = DecisionSkipped
			decision.Reason = "pattern budget exceeded"
			result.Decisions = append(result.Decisions, decision)
			continue
		}
		if current, found := matchExistingSkill(entry, existing, e.opts.SimilarTriggerThreshold); found {
			decision.MatchedSkillName = current.Name
			if !IsPatternBetter(p, current) {
				decision.Action = DecisionSkipped
				decision.Reason = "existing skill is equal or better"
				result.Decisions = append(result.Decisions, decision)
				continue
			}
			entry = preserveExistingSkillIdentity(entry, current)
			if err := e.store.Update(entry); err != nil {
				decision.Action = DecisionSkipped
				decision.Reason = "update failed: " + err.Error()
				result.Decisions = append(result.Decisions, decision)
				continue
			}
			existing = replaceExistingSkill(existing, entry)
			decision.Action = DecisionUpdated
		} else {
			if err := e.store.Register(entry); err != nil {
				decision.Action = DecisionSkipped
				decision.Reason = "register failed: " + err.Error()
				result.Decisions = append(result.Decisions, decision)
				continue
			}
			existing = append(existing, entry)
			decision.Action = DecisionRegistered
		}
		result.Upserted = append(result.Upserted, entry)
		result.Decisions = append(result.Decisions, decision)
	}

	return result, nil
}

func Eligible(s SessionSnapshot) bool {
	if s.ExitCode != nil && *s.ExitCode != 0 {
		if !(s.StructuredSession && *s.ExitCode == 1) {
			return false
		}
	}
	return len(s.Events) > 0 || hasMeaningfulOutput(s.RawOutputLines)
}

func BuildSessionHistory(s SessionSnapshot) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Tool: %s\n", s.Tool))
	sb.WriteString(fmt.Sprintf("Title: %s\n", s.Title))
	sb.WriteString(fmt.Sprintf("Project: %s\n", s.ProjectPath))
	if s.ExitCode != nil {
		sb.WriteString(fmt.Sprintf("Exit Code: %d\n", *s.ExitCode))
	}
	sb.WriteString("\n")

	if len(s.Events) > 0 {
		sb.WriteString("=== Important Events ===\n")
		for _, ev := range s.Events {
			sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", ev.Type, ev.Title, ev.Summary))
		}
		sb.WriteString("\n")
	}

	if len(s.RawOutputLines) > 0 {
		sb.WriteString("=== Session Output (filtered) ===\n")
		start := 0
		if len(s.RawOutputLines) > 100 {
			start = len(s.RawOutputLines) - 100
		}
		lineCount := 0
		for _, line := range s.RawOutputLines[start:] {
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

	return RedactExperienceText(sb.String())
}

func (e *Extractor) PatternToSkill(p Pattern, projectPath string) (corelib.NLSkillEntry, bool) {
	entry, _, _, ok := e.evaluatePattern(p, projectPath)
	return entry, ok
}

func (e *Extractor) evaluatePattern(p Pattern, projectPath string) (corelib.NLSkillEntry, QualityReport, string, bool) {
	quality := EvaluatePatternQuality(p)
	name := NormalizePatternName(p.Name)
	if !learnedSkillNamePattern.MatchString(name) {
		return corelib.NLSkillEntry{}, quality, "invalid skill name", false
	}
	if len(p.Steps) == 0 {
		return corelib.NLSkillEntry{}, quality, "pattern has no steps", false
	}
	description := strings.TrimSpace(p.Description)
	if len(description) < 20 {
		return corelib.NLSkillEntry{}, quality, "description is too short", false
	}
	if len(p.Triggers) < 2 {
		return corelib.NLSkillEntry{}, quality, "not enough triggers", false
	}
	if !quality.PassesThreshold(e.opts.MinPatternQualityScore) {
		return corelib.NLSkillEntry{}, quality, "quality score below threshold", false
	}

	steps := make([]corelib.NLSkillStep, 0, len(p.Steps))
	for _, s := range p.Steps {
		action := strings.TrimSpace(s.Action)
		if !validStepActions[action] {
			return corelib.NLSkillEntry{}, quality, "unsupported step action: " + action, false
		}
		onError := strings.TrimSpace(s.OnError)
		if onError == "" {
			onError = "stop"
		}
		if onError != "stop" && onError != "continue" {
			return corelib.NLSkillEntry{}, quality, "unsupported on_error policy: " + onError, false
		}
		steps = append(steps, corelib.NLSkillStep{Action: action, Params: s.Params, OnError: onError})
	}

	createdAt := time.Now().Format(time.RFC3339)
	if e != nil && e.now != nil {
		createdAt = e.now().Format(time.RFC3339)
	}

	requiredArgs := ExtractRequiredArgs(p)
	return corelib.NLSkillEntry{
		Name:          name,
		Description:   description,
		Triggers:      normalizeTriggers(p.Triggers),
		Steps:         steps,
		Status:        "active",
		CreatedAt:     createdAt,
		Source:        "learned",
		SourceProject: projectPath,
		RequiredArgs:  requiredArgs,
		Params:        synthesizeSkillParams(steps, requiredArgs),
	}, quality, "", true
}
func ParsePatterns(content string) ([]Pattern, error) {
	content = stripCodeFences(strings.TrimSpace(content))
	if content == "" {
		return nil, nil
	}
	var patterns []Pattern
	if err := json.Unmarshal([]byte(content), &patterns); err == nil {
		return patterns, nil
	}
	var wrapped struct {
		Patterns []Pattern `json:"patterns"`
	}
	if err := json.Unmarshal([]byte(content), &wrapped); err != nil {
		return nil, fmt.Errorf("parse patterns JSON: %w", err)
	}
	return wrapped.Patterns, nil
}

func IsPatternBetter(newP Pattern, existing corelib.NLSkillEntry) bool {
	existingP := patternFromSkill(existing)
	newQuality := EvaluatePatternQuality(newP).Score
	existingQuality := EvaluatePatternQuality(existingP).Score

	score := 0
	if delta := newQuality - existingQuality; delta >= 2 {
		score += 2
	} else if delta == 1 {
		score++
	}
	if len(newP.Steps) > len(existing.Steps) {
		score += 2
	}
	if len(ExtractRequiredArgs(newP)) > len(ExtractRequiredArgs(existingP)) {
		score++
	}
	if explicitErrorPolicyCount(newP) > explicitErrorPolicyCount(existingP) {
		score++
	}
	if len(strings.TrimSpace(newP.Description)) > len(existing.Description)+20 {
		score++
	}
	if len(normalizeTriggers(newP.Triggers)) > len(existing.Triggers) {
		score++
	}
	if containsDangerousOperation(existingP) && !containsDangerousOperation(newP) {
		score += 3
	}
	if containsOneOffPath(existingP) && !containsOneOffPath(newP) {
		score += 2
	}
	if existing.UsageCount >= 3 && existing.SuccessCount*2 < existing.UsageCount {
		score += 2
	}
	return score >= 2
}

func patternFromSkill(entry corelib.NLSkillEntry) Pattern {
	steps := make([]Step, 0, len(entry.Steps))
	for _, step := range entry.Steps {
		steps = append(steps, Step{Action: step.Action, Params: step.Params, OnError: step.OnError})
	}
	return Pattern{Name: entry.Name, Description: entry.Description, Triggers: entry.Triggers, Steps: steps}
}

func explicitErrorPolicyCount(p Pattern) int {
	count := 0
	for _, step := range p.Steps {
		onError := strings.TrimSpace(step.OnError)
		if onError == "stop" || onError == "continue" {
			count++
		}
	}
	return count
}

func SystemPrompt() string {
	return `You are an expert at analysing coding session histories and extracting reusable operation patterns.
Given a session history, identify patterns that are GENUINELY reusable, not one-off tasks.

A good reusable pattern:
- Solves a recurring problem, such as deploy to staging or run tests with coverage
- Has clear, parameterizable steps, not hardcoded to one specific file or project
- Would save time if automated

A bad pattern, do not extract:
- One-off debugging sessions with no repeatable structure
- Simple single-command operations, such as just git pull
- Patterns too specific to one project's file paths

Return a JSON array. Each pattern must have:
- "name": a short, descriptive kebab-case name, such as "deploy-staging" or "run-coverage-tests"
- "description": what the pattern does and when to use it
- "triggers": list of 3-5 keywords or phrases that would trigger this pattern
- "steps": list of steps, each with "action" (create_session/send_input/send_and_observe/call_mcp_tool/bash), "params" (key-value map), and optional "on_error" ("stop" or "continue")

Return only a JSON array. If no genuinely reusable patterns are found, return [].
Quality over quantity: only extract patterns you are confident are reusable.`
}

func normalizeTriggers(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, trigger := range in {
		trigger = strings.TrimSpace(trigger)
		if trigger == "" {
			continue
		}
		key := strings.ToLower(trigger)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, trigger)
	}
	sort.Strings(out)
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

func hasMeaningfulOutput(lines []string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			return true
		}
	}
	return false
}

func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
	}
	return strings.TrimSpace(s)
}
