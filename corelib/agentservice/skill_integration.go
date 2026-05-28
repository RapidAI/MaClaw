package agentservice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	corelib "github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// SkillToolProvider enables the agent loop to discover and execute Skills.
// Satisfied by *SkillToolBridge.
type SkillToolProvider interface {
	// ListSkills returns all active skills for the principal.
	ListSkills(ctx context.Context, p Principal) []SkillToolEntry

	// RunSkill executes a skill by name with the given arguments.
	RunSkill(ctx context.Context, p Principal, name string, args map[string]interface{}) (string, error)

	// SearchSkills searches for skills across configured sources.
	SearchSkills(ctx context.Context, p Principal, query string) ([]SkillSearchResult, error)
}

type skillMaintenancePlanner interface {
	BuildSkillMaintenancePlan(ctx context.Context, p Principal, opts skill.SkillMaintenancePlanOptions) (skill.SkillMaintenancePlan, error)
}

// SkillToolEntry represents a single installed skill available for the agent.
type SkillToolEntry struct {
	Name        string
	Description string
	Mode        string // sequential/interactive/api_workflow
}

// SkillToolBridge implements SkillToolProvider by delegating to the Service's
// existing skill management infrastructure.
type SkillToolBridge struct {
	svc *Service
}

// NewSkillToolBridge creates a bridge that connects the CoreAgentExecutor to
// the Service's skill management layer.
func NewSkillToolBridge(svc *Service) *SkillToolBridge {
	return &SkillToolBridge{svc: svc}
}

// ListSkills returns all active skills for the principal.
func (b *SkillToolBridge) ListSkills(ctx context.Context, p Principal) []SkillToolEntry {
	items, err := b.svc.ListSkills(ctx, p)
	if err != nil {
		return nil
	}
	entries := make([]SkillToolEntry, 0, len(items))
	for _, item := range items {
		if item.Status == "disabled" {
			continue
		}
		entries = append(entries, SkillToolEntry{
			Name:        item.Name,
			Description: item.Description,
			Mode:        item.Mode,
		})
	}
	return entries
}

// BuildSkillMaintenancePlan returns a read-only local curator plan for the
// principal's installed skills. It does not mutate, execute, archive, or merge
// any skill.
func (b *SkillToolBridge) BuildSkillMaintenancePlan(ctx context.Context, p Principal, opts skill.SkillMaintenancePlanOptions) (skill.SkillMaintenancePlan, error) {
	items, err := b.svc.ListSkills(ctx, p)
	if err != nil {
		return skill.SkillMaintenancePlan{}, err
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	return skill.BuildSkillMaintenancePlan(items, opts), nil
}

// RunSkill executes a skill by name. It finds the skill, validates it,
// and executes its bash steps synchronously using the shared corelib runner.
func (b *SkillToolBridge) RunSkill(ctx context.Context, p Principal, name string, args map[string]interface{}) (string, error) {
	entry, err := b.svc.GetSkill(ctx, p, name)
	if err != nil {
		return "", fmt.Errorf("skill %q not found: %w", name, err)
	}
	if entry.Status == "disabled" {
		return "", fmt.Errorf("skill %q is disabled", name)
	}

	// Normalize the skill for execution.
	skill.NormalizeSkillForRunner(entry)

	if len(entry.Steps) == 0 {
		return "", fmt.Errorf("skill %q has no executable steps", name)
	}

	// Build template variables from args.
	vars := make(map[string]string)
	if input, ok := args["input"].(string); ok && input != "" {
		vars["input"] = input
	}
	if output, ok := args["output"].(string); ok && output != "" {
		vars["output"] = output
	}
	if argsMap, ok := args["args"].(map[string]interface{}); ok {
		for k, v := range argsMap {
			if s, ok := v.(string); ok {
				vars[k] = s
			} else if v != nil {
				data, _ := json.Marshal(v)
				vars[k] = string(data)
			}
		}
	}

	// Resolve selected steps for api_workflow mode.
	selectedSteps, _ := skill.ResolveSelectedStepLabels(entry, args)

	// Execute using the shared synchronous runner.
	result, err := skill.ExecuteStepsSync(ctx, entry, vars, skill.ExecConfig{
		SkillDir:      entry.SkillDir,
		Timeout:       time.Duration(corelib.DefaultAgentTimeoutSec) * time.Second,
		Params:        entry.Params,
		SelectedSteps: selectedSteps,
	}, &srvExecDeps{})
	if err != nil {
		if result != nil && result.LastStepOutput != "" {
			return result.LastStepOutput, err
		}
		return "", err
	}
	return result.Output, nil
}

// SearchSkills searches for skills across configured sources.
func (b *SkillToolBridge) SearchSkills(ctx context.Context, p Principal, query string) ([]SkillSearchResult, error) {
	cfg, err := b.svc.getOrLoadUserConfig(p.TenantID, p.UserID)
	if err != nil {
		return nil, err
	}
	return b.svc.SearchSkills(ctx, p, SkillSearchInput{
		Query:       query,
		SkillHubURL: cfg.AppConfig.RemoteHubURL,
		TopN:        10,
	})
}

// srvExecDeps implements skill.ExecDeps for MaClawSrv.
type srvExecDeps struct{}

func (d *srvExecDeps) ExecuteBash(ctx context.Context, command, workDir string, env map[string]string) (string, error) {
	return executeBashCommand(ctx, command, workDir, env)
}

func (d *srvExecDeps) OnStepProgress(stepIndex, totalSteps int, stepAction, status string) {
	// MaClawSrv: no-op for now. Could be wired to streaming progress in the future.
}

// executeBashCommand runs a command and returns combined stdout+stderr.
// The caller controls timeout via the context — this function does NOT
// add its own timeout to avoid double-timeout issues with ExecuteStepsSync.
func executeBashCommand(ctx context.Context, command, workDir string, extraEnv map[string]string) (string, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}
	if workDir != "" {
		cmd.Dir = workDir
	}
	// Set UTF-8 environment + caller-provided env vars.
	env := append(cmd.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1")
	for k, v := range extraEnv {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	coretool.PrepareCommandForTreeKill(cmd)

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err := cmd.Start()
	if err == nil {
		err = coretool.WaitCommandWithContext(ctx, cmd)
	}
	return output.String(), err
}

// SetSkillToolProvider wires the skill tool provider into the executor.
func (e *CoreAgentExecutor) SetSkillToolProvider(provider SkillToolProvider) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.skillProvider = provider
}

// --- Integration into coreAgentCallbacks ---

// executeManageSkill dispatches the manage_skill tool.
func (c *coreAgentCallbacks) executeManageSkill(args map[string]interface{}) agent.ToolExecutionResult {
	if c.skillProvider == nil {
		return agent.ToolExecutionResult{Result: "Error: skill system is not configured", Outcome: agent.ToolExecutionOutcomeError}
	}
	action := stringArg(args, "action")
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "list":
		return c.skillList()
	case "search":
		return c.skillSearch(args)
	case "run":
		return c.skillRun(args)
	case "maintenance_plan":
		return c.skillMaintenancePlan(args)
	case "execute_maintenance_plan":
		return agent.ToolExecutionResult{Result: "Error: execute_maintenance_plan is not supported by this provider", Outcome: agent.ToolExecutionOutcomeError}
	default:
		return agent.ToolExecutionResult{
			Result:  skill.ManageSkillUnknownActionError(action),
			Outcome: agent.ToolExecutionOutcomeError,
		}
	}
}

func (c *coreAgentCallbacks) skillList() agent.ToolExecutionResult {
	entries := c.skillProvider.ListSkills(c.ctx, c.principal)
	if len(entries) == 0 {
		return agent.ToolExecutionResult{Result: "No skills installed.", Outcome: agent.ToolExecutionOutcomeOK}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Installed skills (%d):\n", len(entries)))
	for _, e := range entries {
		desc := e.Description
		if len(desc) > 80 {
			desc = desc[:80] + "..."
		}
		b.WriteString(fmt.Sprintf("  - %s: %s\n", e.Name, desc))
	}
	return agent.ToolExecutionResult{Result: b.String(), Outcome: agent.ToolExecutionOutcomeOK}
}

func (c *coreAgentCallbacks) skillSearch(args map[string]interface{}) agent.ToolExecutionResult {
	query := stringArg(args, "query")
	if query == "" {
		return agent.ToolExecutionResult{Result: "Error: missing query parameter", Outcome: agent.ToolExecutionOutcomeError}
	}
	results, err := c.skillProvider.SearchSkills(c.ctx, c.principal, query)
	if err != nil {
		return agent.ToolExecutionResult{Result: fmt.Sprintf("Error: search failed: %v", err), Outcome: agent.ToolExecutionOutcomeError}
	}
	if len(results) == 0 {
		return agent.ToolExecutionResult{Result: "No skills found for query: " + query, Outcome: agent.ToolExecutionOutcomeOK}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Search results for %q (%d):\n", query, len(results)))
	for i, r := range results {
		if i >= 10 {
			b.WriteString(fmt.Sprintf("  ... and %d more\n", len(results)-10))
			break
		}
		b.WriteString(fmt.Sprintf("  %d. [%s] %s — %s\n", i+1, r.Source, r.Name, r.Description))
		if r.ID != "" {
			b.WriteString(fmt.Sprintf("     ID: %s\n", r.ID))
		}
	}
	return agent.ToolExecutionResult{Result: b.String(), Outcome: agent.ToolExecutionOutcomeOK}
}

func (c *coreAgentCallbacks) skillRun(args map[string]interface{}) agent.ToolExecutionResult {
	name := stringArg(args, "name")
	if name == "" {
		return agent.ToolExecutionResult{Result: "Error: missing name parameter", Outcome: agent.ToolExecutionOutcomeError}
	}
	result, err := c.skillProvider.RunSkill(c.ctx, c.principal, name, args)
	if err != nil {
		return agent.ToolExecutionResult{Result: fmt.Sprintf("Error: %v", err), Outcome: agent.ToolExecutionOutcomeError}
	}
	return agent.ToolExecutionResult{Result: result, Outcome: agent.ToolExecutionOutcomeOK}
}

func (c *coreAgentCallbacks) skillMaintenancePlan(args map[string]interface{}) agent.ToolExecutionResult {
	planner, ok := c.skillProvider.(skillMaintenancePlanner)
	if !ok {
		return agent.ToolExecutionResult{Result: "Error: skill maintenance planning is not supported by this provider", Outcome: agent.ToolExecutionOutcomeError}
	}
	plan, err := planner.BuildSkillMaintenancePlan(c.ctx, c.principal, skill.SkillMaintenancePlanOptions{
		Now:                 time.Now(),
		StaleAfterDays:      intArg(args, "stale_after_days", 0),
		MinFailureRuns:      intArg(args, "min_failure_runs", 0),
		MaxActions:          intArg(args, "max_actions", 0),
		DuplicateSimilarity: skillFloatArg(args, "duplicate_similarity"),
	})
	if err != nil {
		return agent.ToolExecutionResult{Result: fmt.Sprintf("Error: maintenance plan failed: %v", err), Outcome: agent.ToolExecutionOutcomeError}
	}
	payload := map[string]interface{}{
		"ok":                      true,
		"non_executing":           true,
		"boundary":                "read-only skill maintenance plan; no skill was modified, archived, merged, deleted, installed, or executed",
		"maintenance_plan_status": "local_skill_maintenance_plan_no_llm",
		"plan":                    plan,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return agent.ToolExecutionResult{Result: fmt.Sprintf("Error: marshal maintenance plan failed: %v", err), Outcome: agent.ToolExecutionOutcomeError}
	}
	return agent.ToolExecutionResult{Result: string(data), Outcome: agent.ToolExecutionOutcomeOK}
}

func skillFloatArg(args map[string]interface{}, key string) float64 {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case float32:
			return float64(n)
		case int:
			return float64(n)
		case json.Number:
			if f, err := n.Float64(); err == nil {
				return f
			}
		}
	}
	return 0
}

// manageSkillToolDef returns the manage_skill tool definition for BuildTools.
func (c *coreAgentCallbacks) manageSkillToolDef() map[string]interface{} {
	return functionToolDefinition("manage_skill",
		"Manage and execute installed Skills. Actions: "+skill.ManageSkillActionSlash()+". maintenance_plan is read-only and never modifies, archives, merges, installs, or executes skills.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":                 map[string]interface{}{"type": "string", "description": "Action: " + skill.ManageSkillActionSlash()},
				"query":                  map[string]interface{}{"type": "string", "description": "Search keyword (required for search)"},
				"name":                   map[string]interface{}{"type": "string", "description": "Skill name (required for run)"},
				"args":                   map[string]interface{}{"type": "object", "description": "Skill arguments (for run). Template variables like {{key}} in skill commands are replaced with values from this object."},
				"input":                  map[string]interface{}{"type": "string", "description": "Input parameter (for run, shorthand for args.input)"},
				"output":                 map[string]interface{}{"type": "string", "description": "Output parameter (for run, shorthand for args.output)"},
				"max_actions":            map[string]interface{}{"type": "integer", "description": "Maximum number of maintenance actions returned by maintenance_plan"},
				"stale_after_days":       map[string]interface{}{"type": "integer", "description": "Days before an unused learned skill is considered stale for maintenance_plan"},
				"min_failure_runs":       map[string]interface{}{"type": "integer", "description": "Minimum failed runs before maintenance_plan recommends review or repair"},
				"duplicate_similarity":   map[string]interface{}{"type": "number", "description": "Name/description similarity threshold for duplicate skill recommendations"},
				"dry_run":                map[string]interface{}{"type": "boolean", "description": "execute_maintenance_plan preview mode; defaults true"},
				"confirm":                map[string]interface{}{"type": "boolean", "description": "Required true when execute_maintenance_plan uses dry_run=false"},
				"approved_actions":       map[string]interface{}{"type": "array", "description": "Approved maintenance action names for execute_maintenance_plan"},
				"allow_duplicate_retire": map[string]interface{}{"type": "boolean", "description": "Allow execute_maintenance_plan to disable the recommended duplicate skill after merge draft review"},
			},
			"required": []string{"action"},
		})
}

// skillToolDefs returns the manage_skill tool definition if provider is available.
func (c *coreAgentCallbacks) skillToolDefs() []map[string]interface{} {
	if c.skillProvider == nil {
		return nil
	}
	return []map[string]interface{}{c.manageSkillToolDef()}
}
