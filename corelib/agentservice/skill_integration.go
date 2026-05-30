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
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// SkillToolProvider enables the agent loop to discover and execute Skills.
// Satisfied by *SkillToolBridge.
type SkillToolProvider interface {
	// ListSkills returns all active skills for the principal.
	ListSkills(ctx context.Context, p Principal) []SkillToolEntry

	// InstallSkill installs a skill from an allowed source for the principal.
	InstallSkill(ctx context.Context, p Principal, args map[string]interface{}) ([]corelib.NLSkillEntry, error)

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

// InstallSkill installs a skill using the Service's user-scoped skill
// lifecycle. Source allow-lists are enforced by Service.InstallSkill.
func (b *SkillToolBridge) InstallSkill(ctx context.Context, p Principal, args map[string]interface{}) ([]corelib.NLSkillEntry, error) {
	in := SkillInstallInput{
		Source:         normalizeSkillInstallToolSource(firstNonEmptySkillArg(args, "source", "origin")),
		RepoURL:        stringArg(args, "repo_url"),
		RawURL:         stringArg(args, "raw_url"),
		RepoFullName:   stringArg(args, "repo_full_name"),
		FilePath:       stringArg(args, "file_path"),
		Branch:         stringArg(args, "branch"),
		DefinitionType: stringArg(args, "definition_type"),
		ZipBase64:      stringArg(args, "zip_base64"),
		SkillHubURL:    firstNonEmptySkillArg(args, "skill_hub_url", "hub_url"),
		SkillMarketURL: firstNonEmptySkillArg(args, "skill_market_url", "market_url"),
		SkillID:        firstNonEmptySkillArg(args, "skill_id", "id"),
		Overwrite:      skillBoolArg(args, "overwrite"),
		GitHubToken:    stringArg(args, "github_token"),
	}
	applySkillInstallRef(&in, stringArg(args, "install_ref"))
	if in.Source == "" {
		in.Source = inferSkillInstallInputSource(in)
	}
	if in.Source == "github" && in.RawURL == "" && in.RepoURL != "" {
		in.Source = "github_repo"
	}
	if in.Source == "" {
		in.Source = inferSkillInstallSource(args)
	}
	if in.SkillMarketURL == "" {
		if cfg, err := b.svc.getOrLoadUserConfig(p.TenantID, p.UserID); err == nil {
			in.SkillMarketURL = cfg.AppConfig.SkillMarketBaseURL(remote.DefaultRemoteHubCenterURL)
		}
	}
	return b.svc.InstallSkill(ctx, p, in)
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

func firstNonEmptySkillArg(args map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(stringArg(args, key)); value != "" {
			return value
		}
	}
	return ""
}

func skillBoolArg(args map[string]interface{}, key string) bool {
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true") || strings.TrimSpace(v) == "1"
	default:
		return false
	}
}

func inferSkillInstallSource(args map[string]interface{}) string {
	hubURL := strings.ToLower(strings.TrimSpace(stringArg(args, "hub_url")))
	switch {
	case hubURL == "github" || strings.Contains(hubURL, "github.com"):
		return "github"
	case strings.Contains(hubURL, "clawhub"):
		return "clawhub"
	}
	if strings.TrimSpace(stringArg(args, "zip_base64")) != "" {
		return "zip"
	}
	if strings.TrimSpace(stringArg(args, "repo_url")) != "" {
		return "github_repo"
	}
	if strings.TrimSpace(stringArg(args, "raw_url")) != "" || strings.TrimSpace(stringArg(args, "repo_full_name")) != "" {
		return "github"
	}
	if strings.TrimSpace(stringArg(args, "skill_id")) != "" || strings.TrimSpace(stringArg(args, "id")) != "" {
		if strings.TrimSpace(firstNonEmptySkillArg(args, "skill_market_url", "market_url")) != "" {
			return "skillmarket"
		}
		return "skillhub"
	}
	return ""
}

func inferSkillInstallInputSource(in SkillInstallInput) string {
	if strings.TrimSpace(in.ZipBase64) != "" {
		return "zip"
	}
	if strings.TrimSpace(in.RawURL) != "" || strings.TrimSpace(in.RepoFullName) != "" {
		return "github"
	}
	if strings.TrimSpace(in.RepoURL) != "" {
		return "github_repo"
	}
	if strings.TrimSpace(in.SkillID) != "" {
		if strings.TrimSpace(in.SkillMarketURL) != "" {
			return "skillmarket"
		}
		return "skillhub"
	}
	return ""
}

func normalizeSkillInstallToolSource(source string) string {
	source = strings.ToLower(strings.TrimSpace(source))
	switch {
	case source == "github" || strings.Contains(source, "github.com"):
		return "github"
	case strings.Contains(source, "clawhub"):
		return "clawhub"
	case source == "hubcenter" || source == "hub_center" || source == "market":
		return "skillmarket"
	default:
		return source
	}
}

func applySkillInstallRef(in *SkillInstallInput, installRef string) {
	if in == nil {
		return
	}
	installRef = strings.TrimSpace(installRef)
	if installRef == "" {
		return
	}
	var cand skill.GitHubSkillCandidate
	if strings.HasPrefix(installRef, "{") && json.Unmarshal([]byte(installRef), &cand) == nil {
		if in.RepoURL == "" {
			in.RepoURL = cand.RepoURL
		}
		if in.RawURL == "" {
			in.RawURL = cand.RawURL
		}
		if in.RepoFullName == "" {
			in.RepoFullName = cand.RepoFullName
		}
		if in.FilePath == "" {
			in.FilePath = cand.FilePath
		}
		if in.Branch == "" {
			in.Branch = cand.Branch
		}
		if in.DefinitionType == "" {
			in.DefinitionType = cand.DefinitionType
		}
		return
	}
	if in.RawURL == "" && (strings.Contains(installRef, "raw.githubusercontent.com") || strings.Contains(installRef, "/raw/")) {
		in.RawURL = installRef
		return
	}
	if in.RepoURL == "" {
		in.RepoURL = installRef
	}
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
	case "install":
		return c.skillInstall(args)
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

func (c *coreAgentCallbacks) skillInstall(args map[string]interface{}) agent.ToolExecutionResult {
	entries, err := c.skillProvider.InstallSkill(c.ctx, c.principal, args)
	if err != nil {
		return agent.ToolExecutionResult{Result: fmt.Sprintf("Error: install failed: %v", err), Outcome: agent.ToolExecutionOutcomeError}
	}
	if len(entries) == 0 {
		return agent.ToolExecutionResult{Result: "No skill was installed.", Outcome: agent.ToolExecutionOutcomeOK}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Installed skills (%d):\n", len(entries)))
	for _, entry := range entries {
		b.WriteString(fmt.Sprintf("  - %s", entry.Name))
		if strings.TrimSpace(entry.Description) != "" {
			b.WriteString(": ")
			b.WriteString(entry.Description)
		}
		b.WriteByte('\n')
	}
	return agent.ToolExecutionResult{Result: b.String(), Outcome: agent.ToolExecutionOutcomeOK}
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
				"source":                 map[string]interface{}{"type": "string", "description": "Skill install source: skillmarket, skillhub, clawhub, github, github_repo, or zip"},
				"skill_id":               map[string]interface{}{"type": "string", "description": "Skill ID from search results (for install)"},
				"hub_url":                map[string]interface{}{"type": "string", "description": "SkillHub URL (for install/search when using skillhub)"},
				"skill_hub_url":          map[string]interface{}{"type": "string", "description": "SkillHub URL (for install/search when using skillhub)"},
				"skill_market_url":       map[string]interface{}{"type": "string", "description": "SkillMarket/HubCenter URL (for install/search when using skillmarket)"},
				"repo_url":               map[string]interface{}{"type": "string", "description": "GitHub repository URL (for github_repo install)"},
				"raw_url":                map[string]interface{}{"type": "string", "description": "Raw GitHub skill file URL (for github install)"},
				"install_ref":            map[string]interface{}{"type": "string", "description": "Install reference from search results, such as a GitHub raw URL or repo URL"},
				"repo_full_name":         map[string]interface{}{"type": "string", "description": "GitHub owner/repo from search results (for github install)"},
				"file_path":              map[string]interface{}{"type": "string", "description": "Skill file path in repo (for github install)"},
				"branch":                 map[string]interface{}{"type": "string", "description": "Git branch (for github install)"},
				"definition_type":        map[string]interface{}{"type": "string", "description": "Skill definition type from search results (for github install)"},
				"zip_base64":             map[string]interface{}{"type": "string", "description": "Base64-encoded skill zip archive (for zip install)"},
				"overwrite":              map[string]interface{}{"type": "boolean", "description": "Overwrite an installed skill with the same name"},
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
