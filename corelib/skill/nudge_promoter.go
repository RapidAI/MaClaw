package skill

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"gopkg.in/yaml.v3"
)

// PromotionThreshold defines when a nudge candidate is ready for automatic
// skill creation.
type PromotionThreshold struct {
	MinEvidence    int     // minimum evidence count, default 5
	MinSuccessRate float64 // minimum success rate, default 0.90
	MinConfidence  float64 // minimum confidence score, default 0.75
}

func defaultPromotionThreshold(t PromotionThreshold) PromotionThreshold {
	if t.MinEvidence <= 0 {
		t.MinEvidence = 5
	}
	if t.MinSuccessRate <= 0 {
		t.MinSuccessRate = 0.90
	}
	if t.MinConfidence <= 0 {
		t.MinConfidence = 0.75
	}
	return t
}

// PromotionResult records the outcome of a nudge-to-skill promotion attempt.
type PromotionResult struct {
	Promoted    bool   `json:"promoted"`
	SkillName   string `json:"skill_name,omitempty"`
	SkillDir    string `json:"skill_dir,omitempty"`
	Explanation string `json:"explanation"`
	Blocked     bool   `json:"blocked,omitempty"`
	BlockReason string `json:"block_reason,omitempty"`
}

// StagingValidator is the interface for security scanning before registration.
type StagingValidator interface {
	// ScanSkillDir scans a skill directory for security issues.
	// Returns nil if safe, error describing the risk otherwise.
	ScanSkillDir(skillDir string) error
}

// SkillRegistrar is the interface for registering a newly created skill.
type SkillRegistrar interface {
	// RegisterSkill adds a skill entry to the active skill library.
	RegisterSkill(entry *corelib.NLSkillEntry) error
}

// NudgePromoter converts high-confidence tool-sequence nudge candidates into
// real executable skills. It bridges the gap between pattern recognition
// (DistillSkillNudgeCandidates) and skill creation.
type NudgePromoter struct {
	Threshold PromotionThreshold
	LLM       LLMRepairer
	Staging   StagingValidator
	Registrar SkillRegistrar
	Versioner *Versioner
	SkillsDir string // base directory for skill storage
}

// NewNudgePromoter creates a NudgePromoter with default thresholds.
func NewNudgePromoter(llm LLMRepairer, staging StagingValidator, registrar SkillRegistrar, skillsDir string) *NudgePromoter {
	return &NudgePromoter{
		Threshold: defaultPromotionThreshold(PromotionThreshold{}),
		LLM:       llm,
		Staging:   staging,
		Registrar: registrar,
		SkillsDir: skillsDir,
		Versioner: &Versioner{},
	}
}

// IsPromotable checks if a nudge candidate meets the promotion threshold.
func (p *NudgePromoter) IsPromotable(candidate tool.ToolSkillNudgeCandidate) bool {
	if p == nil {
		return false
	}
	threshold := defaultPromotionThreshold(p.Threshold)
	return candidate.Evidence >= threshold.MinEvidence &&
		candidate.SuccessRate >= threshold.MinSuccessRate &&
		candidate.Confidence >= threshold.MinConfidence
}

// TryPromote attempts to convert a nudge candidate into a registered skill.
// Steps:
//  1. Check threshold
//  2. LLM generates skill.yaml content
//  3. Write to skill directory
//  4. Security scan
//  5. Register as active skill
//
// This is designed to run asynchronously (in a goroutine) after the main
// agent loop has returned a response to the user.
func (p *NudgePromoter) TryPromote(candidate tool.ToolSkillNudgeCandidate) (*PromotionResult, error) {
	if p == nil {
		return nil, fmt.Errorf("NudgePromoter is nil")
	}

	threshold := defaultPromotionThreshold(p.Threshold)

	// Step 1: Check threshold FIRST (no LLM needed for rejection).
	if candidate.Evidence < threshold.MinEvidence ||
		candidate.SuccessRate < threshold.MinSuccessRate ||
		candidate.Confidence < threshold.MinConfidence {
		return &PromotionResult{
			Promoted:    false,
			Explanation: fmt.Sprintf("below threshold: evidence=%d/<=%d, rate=%.2f/<=%.2f, conf=%.2f/<=%.2f", candidate.Evidence, threshold.MinEvidence, candidate.SuccessRate, threshold.MinSuccessRate, candidate.Confidence, threshold.MinConfidence),
		}, nil
	}

	if p.LLM == nil || !p.LLM.IsConfigured() {
		return nil, fmt.Errorf("LLM not configured for skill promotion")
	}

	skillName := candidate.SuggestedName
	if skillName == "" {
		skillName = generatePromotedSkillName(candidate)
	}

	log.Printf("[nudge-promoter] promoting candidate: name=%s sequence=%v evidence=%d rate=%.2f",
		skillName, candidate.ToolSequence, candidate.Evidence, candidate.SuccessRate)

	// Step 2: LLM generates skill.yaml content.
	yamlContent, err := p.generateSkillYAML(candidate, skillName)
	if err != nil {
		return nil, fmt.Errorf("generate skill YAML: %w", err)
	}
	// Pin the registry identity and lifecycle state in the durable definition.
	// Otherwise a restart that scans the directory without the config overlay
	// could silently rename the skill or promote it back to active.
	yamlContent, err = normalizeGeneratedSkillYAML(yamlContent, skillName)
	if err != nil {
		return nil, fmt.Errorf("normalize generated skill YAML: %w", err)
	}

	// Step 3: Write to skill directory.
	skillDir := filepath.Join(p.SkillsDir, skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return nil, fmt.Errorf("create skill dir: %w", err)
	}

	yamlPath := filepath.Join(skillDir, "skill.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o644); err != nil {
		// Clean up on failure.
		os.RemoveAll(skillDir)
		return nil, fmt.Errorf("write skill.yaml: %w", err)
	}

	// Step 4: Security scan.
	if p.Staging != nil {
		if err := p.Staging.ScanSkillDir(skillDir); err != nil {
			os.RemoveAll(skillDir)
			return &PromotionResult{
				Promoted:    false,
				SkillName:   skillName,
				Blocked:     true,
				BlockReason: err.Error(),
				Explanation: "security scan blocked the promoted skill",
			}, nil
		}
	}

	// Step 5: Register.
	entry := &corelib.NLSkillEntry{
		Name:        skillName,
		Description: candidate.Description,
		// Auto-discovered skills are staged until a user approves them and a
		// runtime proof is recorded. Staged entries are not routable/executable.
		Status:         "staged",
		Source:         "auto_discovered",
		SkillDir:       skillDir,
		CreatedAt:      time.Now().Format(time.RFC3339),
		DiscoveredFrom: candidate.ContextKey,
	}

	// Parse the YAML to populate steps/params.
	yamlData, readErr := os.ReadFile(yamlPath)
	var parsed *SkillYAMLFile
	var parseErr error
	if readErr == nil {
		parsed, parseErr = ParseSkillYAMLFile(yamlData)
	} else {
		parseErr = readErr
	}
	if parseErr != nil || parsed == nil {
		// A generated definition must be syntactically and structurally valid
		// before it can enter the skill registry.  Leaving a malformed file on
		// disk creates an active-but-unexecutable skill and can also expose it to
		// routing or auto-upload.
		_ = os.RemoveAll(skillDir)
		if parseErr != nil {
			return nil, fmt.Errorf("parse generated skill YAML: %w", parseErr)
		}
		return nil, fmt.Errorf("parse generated skill YAML: empty definition")
	}
	if err := validateGeneratedSkillDefinition(parsed, skillName); err != nil {
		_ = os.RemoveAll(skillDir)
		return nil, fmt.Errorf("validate generated skill YAML: %w", err)
	}
	{
		entry.Description = parsed.Description
		entry.RequiredArgs = parsed.RequiredArgs
		if len(parsed.Steps) > 0 {
			steps := make([]corelib.NLSkillStep, len(parsed.Steps))
			for i, s := range parsed.Steps {
				steps[i] = corelib.NLSkillStep{
					Action:  s.Action,
					Params:  s.Params,
					Label:   s.Label,
					OnError: s.OnError,
					When:    s.When,
				}
				if s.Capture != nil {
					steps[i].Capture = s.Capture
				}
			}
			entry.Steps = steps
		}
		if len(parsed.Triggers) > 0 {
			entry.Triggers = parsed.Triggers
		}
	}

	if p.Registrar != nil {
		if err := p.Registrar.RegisterSkill(entry); err != nil {
			os.RemoveAll(skillDir)
			return nil, fmt.Errorf("register skill: %w", err)
		}
	}

	log.Printf("[nudge-promoter] PROMOTED: skill=%s dir=%s from_sequence=%v",
		skillName, skillDir, candidate.ToolSequence)

	return &PromotionResult{
		Promoted:    true,
		SkillName:   skillName,
		SkillDir:    skillDir,
		Explanation: fmt.Sprintf("auto-discovered from %d successful executions of [%s]", candidate.Evidence, strings.Join(candidate.ToolSequence, " → ")),
	}, nil
}

// validateGeneratedSkillDefinition is the semantic admission gate for LLM
// generated definitions. YAML syntax alone is insufficient: an empty skill or
// an action outside the GUI runner contract would otherwise be registered and
// later appear as a broken capability.
func validateGeneratedSkillDefinition(def *SkillYAMLFile, expectedName string) error {
	if def == nil {
		return fmt.Errorf("empty definition")
	}
	if strings.TrimSpace(def.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if expectedName != "" && !strings.EqualFold(strings.TrimSpace(def.Name), strings.TrimSpace(expectedName)) {
		return fmt.Errorf("name %q does not match requested skill %q", def.Name, expectedName)
	}
	if strings.TrimSpace(def.Description) == "" {
		return fmt.Errorf("description is required")
	}
	if len(def.Steps) == 0 {
		return fmt.Errorf("at least one executable step is required")
	}
	if err := validateGeneratedSkillParams(def); err != nil {
		return err
	}
	mode := strings.ToLower(strings.TrimSpace(def.Mode))
	if mode != "" && mode != "sequential" && mode != "interactive" && mode != "api_workflow" && mode != "pipeline" {
		return fmt.Errorf("unsupported mode %q", def.Mode)
	}
	execMode := strings.ToLower(strings.TrimSpace(def.ExecMode))
	if execMode != "" && execMode != "all" && execMode != "first" && execMode != "named" {
		return fmt.Errorf("unsupported exec_mode %q", def.ExecMode)
	}
	if mode == "api_workflow" && len(def.Operations) == 0 {
		return fmt.Errorf("api_workflow requires at least one operation")
	}
	if mode != "api_workflow" && len(def.Operations) > 0 {
		return fmt.Errorf("operations require mode api_workflow")
	}
	if def.GlobalTimeout < 0 || def.GlobalTimeout > 86400 {
		return fmt.Errorf("global_timeout must be between 0 and 86400 seconds")
	}
	for i, step := range def.Steps {
		if strings.TrimSpace(step.Action) == "" {
			return fmt.Errorf("step %d has empty action", i)
		}
		normalized := NormalizeStepForRunnerCopy(corelib.NLSkillStep{Action: step.Action, Params: step.Params}, "")
		if err := EnsureStepActionSupported(RunnerBackendGUI, normalized.Action); err != nil {
			return fmt.Errorf("step %d action %q unsupported: %w", i, step.Action, err)
		}
		switch NormalizeStepActionName(normalized.Action) {
		case "bash":
			if strings.TrimSpace(fmt.Sprint(normalized.Params["command"])) == "" {
				return fmt.Errorf("step %d bash command is required", i)
			}
		case "craft_tool":
			if strings.TrimSpace(fmt.Sprint(normalized.Params["instructions"])) == "" {
				return fmt.Errorf("step %d craft_tool instructions are required", i)
			}
		case "call_mcp_tool":
			if len(normalized.Params) == 0 {
				return fmt.Errorf("step %d call_mcp_tool params are required", i)
			}
		}
		onError := strings.ToLower(strings.TrimSpace(step.OnError))
		if onError != "" && onError != "stop" && onError != "skip" && onError != "continue" {
			return fmt.Errorf("step %d has invalid on_error %q", i, step.OnError)
		}
		if step.TimeoutSeconds < 0 || step.TimeoutSeconds > 86400 {
			return fmt.Errorf("step %d timeout must be between 0 and 86400 seconds", i)
		}
		if step.Capture != nil {
			for name, pattern := range step.Capture {
				if !validGeneratedParamName(name) {
					return fmt.Errorf("step %d capture name %q is invalid", i, name)
				}
				re, err := regexp.Compile(pattern)
				if err != nil || re.NumSubexp() == 0 {
					if err == nil {
						err = fmt.Errorf("regex must contain a capture group")
					}
					return fmt.Errorf("step %d capture %q is invalid: %w", i, name, err)
				}
			}
		}
		if poll := step.Poll; poll != nil {
			if poll.Interval <= 0 || poll.Interval > 86400 || poll.MaxAttempts <= 0 || poll.MaxAttempts > 10000 {
				return fmt.Errorf("step %d poll interval/max_attempts out of range", i)
			}
			if strings.TrimSpace(poll.UntilMatch) == "" && strings.TrimSpace(poll.UntilStatus) == "" {
				return fmt.Errorf("step %d poll requires until_match or until_status", i)
			}
			if poll.UntilMatch != "" {
				if _, err := regexp.Compile(poll.UntilMatch); err != nil {
					return fmt.Errorf("step %d poll until_match is invalid: %w", i, err)
				}
			}
		}
		if loop := step.Loop; loop != nil {
			if loop.MaxIterations <= 0 || loop.MaxIterations > 1000 {
				return fmt.Errorf("step %d loop max_iterations out of range", i)
			}
			if loop.UntilMatch != "" {
				if _, err := regexp.Compile(loop.UntilMatch); err != nil {
					return fmt.Errorf("step %d loop until_match is invalid: %w", i, err)
				}
			}
		}
	}
	return nil
}

var generatedParamNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validGeneratedParamName(name string) bool {
	return generatedParamNameRE.MatchString(strings.TrimSpace(name))
}

func validateGeneratedSkillParams(def *SkillYAMLFile) error {
	declared := map[string]bool{}
	required := map[string]bool{}
	for _, raw := range def.RequiredArgs {
		name := strings.TrimSpace(raw)
		if !validGeneratedParamName(name) {
			return fmt.Errorf("required_args contains invalid name %q", raw)
		}
		key := strings.ToLower(name)
		if declared[key] {
			return fmt.Errorf("required_args contains duplicate name %q", name)
		}
		declared[key] = true
		required[key] = true
	}
	paramNames := map[string]bool{}
	for _, param := range def.Params {
		name := strings.TrimSpace(param.Name)
		if !validGeneratedParamName(name) {
			return fmt.Errorf("params contains invalid name %q", param.Name)
		}
		key := strings.ToLower(name)
		if paramNames[key] {
			return fmt.Errorf("params contains duplicate name %q", name)
		}
		paramNames[key] = true
		declared[key] = true
		if param.Required && strings.TrimSpace(param.Default) == "" {
			required[key] = true
		}
	}
	placeholders := map[string]bool{}
	var visit func(interface{})
	visit = func(value interface{}) {
		switch v := value.(type) {
		case string:
			for _, key := range ExtractPlaceholderKeys(v) {
				if !validGeneratedParamName(key) {
					return
				}
				placeholders[strings.ToLower(strings.TrimSpace(key))] = true
			}
		case map[string]interface{}:
			for _, child := range v {
				visit(child)
			}
		case []interface{}:
			for _, child := range v {
				visit(child)
			}
		}
	}
	for _, step := range def.Steps {
		visit(step.Params)
		visit(step.When)
		visit(step.Condition)
	}
	for key := range placeholders {
		if !declared[key] {
			return fmt.Errorf("placeholder %q is not declared in required_args or params", key)
		}
	}
	// A required declaration must be consumable by the generated definition.
	// Explicit input-only arguments are allowed only when represented in the
	// parameter schema; this prevents stale required_args from silently making
	// every invocation impossible to replay.
	for key := range required {
		if !placeholders[key] && !paramNames[key] {
			return fmt.Errorf("required argument %q is not used by any step or parameter schema", key)
		}
	}
	return nil
}

func normalizeGeneratedSkillYAML(content, skillName string) (string, error) {
	var raw map[string]interface{}
	if err := yaml.Unmarshal([]byte(content), &raw); err != nil {
		return "", fmt.Errorf("YAML parse error: %w", err)
	}
	if raw == nil {
		return "", fmt.Errorf("empty definition")
	}
	raw["name"] = strings.TrimSpace(skillName)
	raw["status"] = "staged"
	raw["source"] = "auto_discovered"
	data, err := yaml.Marshal(raw)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// --- LLM Skill Generation ---

const nudgePromoterSystemPrompt = `You are a skill definition generator. Given a tool sequence that has been
repeatedly and successfully used by an AI agent, generate a complete skill.yaml
definition that captures this workflow as a reusable skill.

Rules:
- The steps MUST use exactly the tools from the provided sequence (in order)
- Add appropriate required_args for inputs that would vary between invocations
- Use {{variable}} placeholders in step params for dynamic values
- Add capture directives where step outputs feed into subsequent steps
- Include a clear description and triggers (keywords that should activate this skill)
- Return ONLY valid YAML content, no markdown fences or explanations

YAML schema:
name: <skill-name>
description: <what it does>
triggers: [keyword1, keyword2]
required_args: [arg1, arg2]
steps:
  - action: <tool_name>
    params:
      command: "..."  # or other tool-specific params
    capture:
      var_name: "regex_pattern"
    on_error: "stop"  # or "skip" or "continue"`

func (p *NudgePromoter) generateSkillYAML(candidate tool.ToolSkillNudgeCandidate, skillName string) (string, error) {
	prompt := fmt.Sprintf(`Generate a skill.yaml for the following observed pattern:

Skill name: %s
Task type: %s
Tool sequence: %s
Query context: [%s]
Times observed successfully: %d
Success rate: %.0f%%

The skill should accept input parameters that capture what varies between invocations,
and produce the observed tool sequence as its steps.`,
		skillName,
		candidate.TaskType,
		strings.Join(candidate.ToolSequence, " → "),
		strings.Join(candidate.QueryTokens, ", "),
		candidate.Evidence,
		candidate.SuccessRate*100,
	)

	resp, err := p.LLM.ChatCall([]map[string]string{
		{"role": "system", "content": nudgePromoterSystemPrompt},
		{"role": "user", "content": prompt},
	})
	if err != nil {
		return "", err
	}

	// Strip markdown fences if present.
	yamlContent := strings.TrimSpace(resp)
	yamlContent = strings.TrimPrefix(yamlContent, "```yaml")
	yamlContent = strings.TrimPrefix(yamlContent, "```yml")
	yamlContent = strings.TrimPrefix(yamlContent, "```")
	yamlContent = strings.TrimSuffix(yamlContent, "```")
	yamlContent = strings.TrimSpace(yamlContent)

	if yamlContent == "" {
		return "", fmt.Errorf("LLM returned empty YAML")
	}

	return yamlContent, nil
}

// --- Helpers ---

func generatePromotedSkillName(candidate tool.ToolSkillNudgeCandidate) string {
	parts := make([]string, 0, 3)
	if candidate.TaskType != "" {
		parts = append(parts, sanitizeSkillNamePart(candidate.TaskType))
	}
	if len(candidate.ToolSequence) > 0 {
		// Use first and last tool to create a descriptive name.
		first := sanitizeSkillNamePart(candidate.ToolSequence[0])
		last := sanitizeSkillNamePart(candidate.ToolSequence[len(candidate.ToolSequence)-1])
		if first == last {
			parts = append(parts, first)
		} else {
			parts = append(parts, first+"-to-"+last)
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "auto-skill")
	}
	name := strings.Join(parts, "-")
	if len(name) > 40 {
		name = name[:40]
	}
	return strings.TrimRight(name, "-")
}

func sanitizeSkillNamePart(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else if r == ' ' || r == '/' || r == '\\' {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// FilterPromotable returns candidates that meet the promotion threshold.
func FilterPromotable(candidates []tool.ToolSkillNudgeCandidate, threshold PromotionThreshold) []tool.ToolSkillNudgeCandidate {
	threshold = defaultPromotionThreshold(threshold)
	var result []tool.ToolSkillNudgeCandidate
	for _, c := range candidates {
		if c.Evidence >= threshold.MinEvidence &&
			c.SuccessRate >= threshold.MinSuccessRate &&
			c.Confidence >= threshold.MinConfidence {
			result = append(result, c)
		}
	}
	return result
}
