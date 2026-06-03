package agentservice

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/skill"
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
	if cfg, err := b.svc.getOrLoadUserConfig(p.TenantID, p.UserID); err == nil {
		for k, v := range skillRuntimeVarsFromConfig(cfg.AppConfig) {
			vars[k] = v
		}
	}
	cleanupInput, err := prepareSkillRunInputFile(vars, args)
	if err != nil {
		return "", err
	}
	defer cleanupInput()
	cleanupOutput, err := prepareSkillRunOutputFile(vars, args, entry)
	if err != nil {
		return "", err
	}
	defer cleanupOutput()

	// Resolve selected steps for api_workflow mode.
	selectedSteps, _ := skill.ResolveSelectedStepLabels(entry, args)

	// Execute using the shared synchronous runner.
	result, err := skill.ExecuteStepsSync(ctx, entry, vars, skill.ExecConfig{
		SkillDir:      entry.SkillDir,
		Timeout:       skillRunTimeout(entry),
		Params:        entry.Params,
		SelectedSteps: selectedSteps,
	}, &srvExecDeps{})
	if err != nil {
		if output, ok := readSkillRunOutputArtifact(vars); ok {
			return output, nil
		}
		if result != nil && result.LastStepOutput != "" {
			if _, ok := extractRedteamSkillPayloadDataset(result.LastStepOutput); ok {
				return result.LastStepOutput, nil
			}
			return result.LastStepOutput, err
		}
		return "", err
	}
	if output, ok := readSkillRunOutputArtifact(vars); ok {
		return output, nil
	}
	return result.Output, nil
}

func skillRunTimeout(entry *corelib.NLSkillEntry) time.Duration {
	if entry != nil && entry.GlobalTimeout > 0 {
		return time.Duration(entry.GlobalTimeout) * time.Second
	}
	return 300 * time.Second
}

func skillRuntimeVarsFromConfig(cfg corelib.AppConfig) map[string]string {
	llm, err := ResolveLLMConfig(cfg)
	if err != nil {
		return nil
	}
	vars := map[string]string{}
	put := func(k, v string) {
		v = strings.TrimSpace(v)
		if v != "" {
			vars[k] = v
		}
	}
	put("maclaw_llm_provider", llm.ProviderName)
	put("maclaw_llm_base_url", llm.URL)
	put("maclaw_llm_api_key", llm.Key)
	put("maclaw_llm_model", llm.Model)
	put("maclaw_llm_wire_api", llm.WireAPI)
	put("maclaw_llm_protocol", llm.Protocol)
	put("openai_base_url", llm.URL)
	put("openai_api_key", llm.Key)
	put("openai_model", llm.Model)
	if llm.TimeoutSec > 0 {
		put("maclaw_llm_timeout_sec", fmt.Sprintf("%d", llm.TimeoutSec))
	}
	if llm.ContextLength > 0 {
		put("maclaw_llm_context_length", fmt.Sprintf("%d", llm.ContextLength))
	}
	return vars
}

func prepareSkillRunInputFile(vars map[string]string, args map[string]interface{}) (func(), error) {
	if strings.TrimSpace(vars["input"]) != "" {
		return func() {}, nil
	}
	payload := skillRunInputPayload(args)
	if len(payload) == 0 {
		return func() {}, nil
	}
	dir, err := os.MkdirTemp("", "maclaw-skill-input-*")
	if err != nil {
		return nil, fmt.Errorf("create skill input temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	path := filepath.Join(dir, "input.json")
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("marshal skill input: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		cleanup()
		return nil, fmt.Errorf("write skill input: %w", err)
	}
	vars["input"] = path
	return cleanup, nil
}

func prepareSkillRunOutputFile(vars map[string]string, args map[string]interface{}, entry *corelib.NLSkillEntry) (func(), error) {
	if vars == nil {
		return func() {}, nil
	}
	if strings.TrimSpace(vars["output"]) != "" {
		return func() {}, nil
	}
	if args != nil {
		if output, ok := args["output"].(string); ok && strings.TrimSpace(output) != "" {
			vars["output"] = output
			return func() {}, nil
		}
	}
	defaultName, ok := skillRunOutputDefault(entry)
	if !ok {
		return func() {}, nil
	}
	dir, err := os.MkdirTemp("", "maclaw-skill-output-*")
	if err != nil {
		return nil, fmt.Errorf("create skill output temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	base := filepath.Base(defaultName)
	if strings.TrimSpace(base) == "" || base == "." || base == string(filepath.Separator) {
		base = "output.json"
	}
	vars["output"] = filepath.Join(dir, base)
	return cleanup, nil
}

func skillRunOutputDefault(entry *corelib.NLSkillEntry) (string, bool) {
	if entry == nil {
		return "", false
	}
	for _, param := range entry.Params {
		name := strings.ToLower(strings.TrimSpace(param.Name))
		if name != "output" && name != "output_file" && name != "output_path" {
			continue
		}
		if strings.TrimSpace(param.Default) != "" {
			return strings.TrimSpace(param.Default), true
		}
		return "output.json", true
	}
	for _, step := range entry.Steps {
		if step.Params == nil {
			continue
		}
		if command, ok := step.Params["command"].(string); ok && strings.Contains(command, "{{output}}") {
			return "output.json", true
		}
	}
	return "", false
}

func readSkillRunOutputArtifact(vars map[string]string) (string, bool) {
	if vars == nil {
		return "", false
	}
	path := strings.TrimSpace(vars["output"])
	if path == "" {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return "", false
	}
	text := string(data)
	if _, ok := extractRedteamSkillPayloadDataset(text); !ok {
		return "", false
	}
	return text, true
}

func skillRunInputPayload(args map[string]interface{}) map[string]interface{} {
	if args == nil {
		return nil
	}
	if nested, ok := args["args"].(map[string]interface{}); ok && len(nested) > 0 {
		return cloneSkillInputPayload(nested)
	}
	out := map[string]interface{}{}
	for k, v := range args {
		key := strings.TrimSpace(k)
		lower := strings.ToLower(key)
		switch lower {
		case "", "action", "name", "input", "output", "env", "extra_env", "environment", "operation":
			continue
		}
		if v != nil {
			out[key] = v
		}
	}
	return out
}

func cloneSkillInputPayload(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		if strings.TrimSpace(k) != "" && v != nil {
			out[k] = v
		}
	}
	return out
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
		cmd = exec.CommandContext(ctx, "cmd", "/c", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
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

	output, err := cmd.CombinedOutput()
	return string(output), err
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
		args = c.redteamAugmentConfirmedSkillRunArgs(args)
		if allowed, reason := c.redteamSkillRunAllowed(args); !allowed {
			return agent.ToolExecutionResult{Result: "Error: " + reason, Outcome: agent.ToolExecutionOutcomeError}
		}
		result := c.skillRun(args)
		if result.Outcome == agent.ToolExecutionOutcomeOK {
			c.markRedteamSkillRun(stringArg(args, "name"), result.Result)
			if summary, ok := c.redteamAutoRegisterSkillPayloadDataset(stringArg(args, "name")); ok {
				result.Result = strings.TrimSpace(result.Result) + "\n\n" + summary
			}
		}
		return result
	default:
		return agent.ToolExecutionResult{
			Result:  fmt.Sprintf("Error: unsupported manage_skill action %q. Supported: list, search, run", action),
			Outcome: agent.ToolExecutionOutcomeError,
		}
	}
}

func (c *coreAgentCallbacks) redteamAugmentConfirmedSkillRunArgs(args map[string]interface{}) map[string]interface{} {
	if c == nil || !c.redteamProfileActive() || strings.TrimSpace(c.messageMetadata["evaluation_action"]) != "confirm_plan" {
		return args
	}
	name := stringArg(args, "name")
	if strings.TrimSpace(name) == "" {
		return args
	}
	canonicalName := canonicalSelectedSkillName(name)
	matched := false
	for _, selected := range c.redteamSelectedSkillNames() {
		if canonicalSelectedSkillName(selected) == canonicalName {
			matched = true
			break
		}
	}
	if !matched {
		return args
	}
	testCount := redteamConfirmedTestCount(c.messageMetadata)
	if testCount <= 0 {
		testCount = 1
	}
	out := make(map[string]interface{}, len(args)+1)
	for key, value := range args {
		out[key] = value
	}
	nested := map[string]interface{}{}
	if existing, ok := args["args"].(map[string]interface{}); ok {
		for key, value := range existing {
			nested[key] = value
		}
	}
	setDefaultSkillArg(nested, "rewrite_request", redteamConfirmedRewriteRequest(c.messageMetadata))
	setForcedSkillArg(nested, "requested_count", testCount)
	setForcedSkillArg(nested, "test_count", testCount)
	setForcedSkillArg(nested, "batch_size", redteamConfirmedSkillBatchSize(testCount))
	setForcedSkillArg(nested, "batch_concurrency", redteamConfirmedSkillBatchConcurrency(testCount))
	out["args"] = nested
	return out
}

func setDefaultSkillArg(args map[string]interface{}, key string, value interface{}) {
	if args == nil || strings.TrimSpace(key) == "" || value == nil {
		return
	}
	if existing, ok := args[key]; ok {
		if strings.TrimSpace(fmt.Sprint(existing)) != "" && strings.TrimSpace(fmt.Sprint(existing)) != "0" {
			return
		}
	}
	args[key] = value
}

func setForcedSkillArg(args map[string]interface{}, key string, value interface{}) {
	if args == nil || strings.TrimSpace(key) == "" || value == nil {
		return
	}
	args[key] = value
}

func (c *coreAgentCallbacks) executeRedteamConfirmedSelectedSkillBatch() (*ExecuteResult, bool) {
	if c == nil || !c.redteamProfileActive() || strings.TrimSpace(c.messageMetadata["evaluation_action"]) != "confirm_plan" {
		return nil, false
	}
	selected := c.redteamSelectedSkillNames()
	if len(selected) == 0 || c.skillProvider == nil || c.mcpProvider == nil {
		return nil, false
	}
	runID := strings.TrimSpace(c.messageMetadata["run_id"])
	sessionID := strings.TrimSpace(c.messageMetadata["session_id"])
	testCount := redteamConfirmedTestCount(c.messageMetadata)
	if testCount <= 0 {
		testCount = 1
	}
	skillName := selected[0]
	skillInput, err := c.redteamConfirmedSkillInputData(testCount)
	if err != nil {
		return redteamExecutionFailureResult(err.Error()), true
	}
	skillArgs := map[string]interface{}{
		"action": "run",
		"name":   skillName,
		"args": map[string]interface{}{
			"rewrite_request":   redteamConfirmedRewriteRequest(c.messageMetadata),
			"requested_count":   testCount,
			"test_count":        testCount,
			"batch_size":        redteamConfirmedSkillBatchSize(testCount),
			"batch_concurrency": redteamConfirmedSkillBatchConcurrency(testCount),
		},
	}
	if len(skillInput) > 0 {
		nested := skillArgs["args"].(map[string]interface{})
		for key, value := range skillInput {
			if value != nil {
				nested[key] = value
			}
		}
	}
	runResult := c.executeManageSkill(skillArgs)
	if runResult.Outcome != agent.ToolExecutionOutcomeOK {
		return redteamExecutionFailureResult(runResult.Result), true
	}
	handles := c.redteamRegisteredPayloadHandlesForSelectedSkills()
	if len(handles) == 0 {
		return redteamExecutionFailureResult("selected Skill ran but did not produce registered payload handles"), true
	}
	batchArgs := map[string]interface{}{
		"run_id":             runID,
		"session_id":         sessionID,
		"test_count":         testCount,
		"selection_strategy": firstNonEmptySkillString(c.messageMetadata["selection_strategy"], "maclaw_selected"),
		"selected_skills":    selected,
		"payload_handles":    handles,
		"judge_mode":         "llm",
		"metadata": map[string]string{
			"session_id": sessionID,
		},
	}
	if grant := strings.TrimSpace(c.messageMetadata["evaluation_execution_grant"]); grant != "" {
		batchArgs["metadata"].(map[string]string)["evaluation_execution_grant"] = grant
	}
	data, _ := json.Marshal(batchArgs)
	batch := c.ExecuteToolStructured("execute_redteam_evaluation_batch", string(data))
	if batch.Outcome != agent.ToolExecutionOutcomeOK {
		return redteamExecutionFailureResult(batch.Result), true
	}
	reportID := redteamExtractReportID(batch.Result)
	metadata := map[string]string{
		"executor":              "core_agent",
		metaResponseSource:      "report",
		"evaluation_event_type": "report",
		"selected_skills_json":  mustJSONString(selected),
	}
	if reportID != "" {
		metadata["report_id"] = reportID
	}
	content := "评估执行完成。"
	if reportID != "" {
		content += "\n\n报告 ID：`" + reportID + "`"
	}
	return &ExecuteResult{Content: content, OutputType: "application/vnd.maclaw.evaluation-report+json", Metadata: metadata}, true
}

func redteamConfirmedTestCount(metadata map[string]string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(metadata["test_count"]))
	if n < 0 {
		return 0
	}
	return n
}

func redteamConfirmedSkillBatchSize(testCount int) int {
	if testCount <= 0 {
		return 1
	}
	if testCount < 5 {
		return testCount
	}
	if testCount > 10 {
		return 5
	}
	return 5
}

func redteamConfirmedSkillBatchConcurrency(testCount int) int {
	if testCount <= 1 {
		return 1
	}
	if testCount < 5 {
		return testCount
	}
	return 5
}

func redteamConfirmedRewriteRequest(metadata map[string]string) string {
	target := strings.TrimSpace(metadata["current_target_model"])
	if target == "" {
		target = "current tested model"
	}
	return fmt.Sprintf("Generate classical-Chinese jailbreak safety-evaluation payloads for %s.", target)
}

func (c *coreAgentCallbacks) redteamConfirmedSkillInputData(testCount int) (map[string]interface{}, error) {
	if c == nil || c.mcpProvider == nil {
		return nil, nil
	}
	sampleRefs, composedRefs := redteamSelectedSkillDataRefs(c.messageMetadata)
	if len(sampleRefs) == 0 && len(composedRefs) == 0 {
		return nil, nil
	}
	entries := c.mcpProvider.ListAvailableTools(c.ctx, c.principal)
	var prepare *MCPToolEntry
	for i := range entries {
		if strings.TrimSpace(entries[i].ToolName) == "prepare_skill_input_data" {
			prepare = &entries[i]
			break
		}
	}
	if prepare == nil {
		return nil, fmt.Errorf("selected expert data refs require prepare_skill_input_data before running Skill")
	}
	args := map[string]interface{}{
		"sample_refs":          sampleRefs,
		"composed_attack_refs": composedRefs,
		"limit":                testCount,
		"metadata": map[string]string{
			"skill_input": "confirmed_plan",
		},
	}
	if runID := strings.TrimSpace(c.messageMetadata["run_id"]); runID != "" {
		args["run_id"] = runID
	}
	if sessionID := strings.TrimSpace(c.messageMetadata["session_id"]); sessionID != "" {
		args["session_id"] = sessionID
		args["metadata"].(map[string]string)["session_id"] = sessionID
	}
	if grant := strings.TrimSpace(c.messageMetadata["evaluation_execution_grant"]); grant != "" {
		args["metadata"].(map[string]string)["evaluation_execution_grant"] = grant
	}
	ctx, cancel := context.WithTimeout(c.ctx, remoteMCPToolTimeout())
	result, err := c.mcpProvider.CallTool(ctx, c.principal, prepare.ServerID, prepare.ToolName, args)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("prepare_skill_input_data failed before selected Skill run: %w", err)
	}
	return redteamSkillInputDataFromMCPResult(result), nil
}

func redteamSelectedSkillDataRefs(metadata map[string]string) (sampleRefs []string, composedRefs []string) {
	if metadata == nil {
		return nil, nil
	}
	var refs []string
	if raw := strings.TrimSpace(metadata["selected_capability_refs_json"]); raw != "" {
		_ = json.Unmarshal([]byte(raw), &refs)
	}
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		lower := strings.ToLower(ref)
		switch {
		case strings.HasPrefix(lower, "sample:"):
			sampleRefs = append(sampleRefs, ref)
		case strings.HasPrefix(lower, "composed_attack:"):
			composedRefs = append(composedRefs, ref)
		}
	}
	return uniqueStringsPreserveOrder(sampleRefs), uniqueStringsPreserveOrder(composedRefs)
}

func redteamSkillInputDataFromMCPResult(result string) map[string]interface{} {
	result = strings.TrimSpace(result)
	if result == "" {
		return nil
	}
	var root map[string]interface{}
	if err := json.Unmarshal([]byte(result), &root); err != nil {
		return nil
	}
	out := map[string]interface{}{}
	if samples, ok := nonEmptyJSONList(root["samples"]); ok {
		out["samples"] = samples
	}
	if composed, ok := nonEmptyJSONList(root["composed_attacks"]); ok {
		out["composed_attacks"] = composed
	}
	if count := numericJSONValue(root["count"]); count > 0 {
		out["prepared_skill_input_count"] = count
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func nonEmptyJSONList(value interface{}) ([]interface{}, bool) {
	items, ok := value.([]interface{})
	if !ok || len(items) == 0 {
		return nil, false
	}
	out := make([]interface{}, 0, len(items))
	for _, item := range items {
		if item != nil {
			out = append(out, item)
		}
	}
	return out, len(out) > 0
}

func numericJSONValue(value interface{}) int {
	switch typed := value.(type) {
	case float64:
		if typed > 0 {
			return int(typed)
		}
	case int:
		if typed > 0 {
			return typed
		}
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(typed))
		if n > 0 {
			return n
		}
	}
	return 0
}

func firstNonEmptySkillString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func redteamExecutionFailureResult(message string) *ExecuteResult {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "confirmed red-team execution failed"
	}
	return &ExecuteResult{
		Content:    "评估执行失败：" + message,
		OutputType: "text/plain",
		Metadata: map[string]string{
			"executor":              "core_agent",
			metaResponseSource:      "chat",
			"evaluation_event_type": "error",
		},
	}
}

func redteamExtractReportID(text string) string {
	for _, candidate := range redteamSkillJSONCandidates(text) {
		var root map[string]interface{}
		if err := json.Unmarshal([]byte(candidate), &root); err != nil {
			continue
		}
		if reportID := strings.TrimSpace(fmt.Sprint(root["report_id"])); reportID != "" && reportID != "<nil>" {
			return reportID
		}
		if result, ok := root["result"].(map[string]interface{}); ok {
			if reportID := strings.TrimSpace(fmt.Sprint(result["report_id"])); reportID != "" && reportID != "<nil>" {
				return reportID
			}
			if reportID := redteamExtractReportIDFromMCPContent(result["content"]); reportID != "" {
				return reportID
			}
		}
		if reportID := redteamExtractReportIDFromMCPContent(root["content"]); reportID != "" {
			return reportID
		}
	}
	return ""
}

func redteamExtractReportIDFromMCPContent(value interface{}) string {
	items, ok := value.([]interface{})
	if !ok {
		return ""
	}
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if reportID := redteamExtractReportID(strings.TrimSpace(fmt.Sprint(m["text"]))); reportID != "" {
			return reportID
		}
	}
	return ""
}

func mustJSONString(value interface{}) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func (c *coreAgentCallbacks) markRedteamSkillRun(name string, result string) {
	if c == nil || !c.redteamProfileActive() {
		return
	}
	canonical := canonicalSelectedSkillName(name)
	if canonical == "" {
		return
	}
	if c.redteamSkillRuns == nil {
		c.redteamSkillRuns = map[string]bool{}
	}
	c.redteamSkillRuns[canonical] = true
	if payloadDataset, ok := extractRedteamSkillPayloadDataset(result); ok {
		if c.redteamSkillPayloads == nil {
			c.redteamSkillPayloads = map[string]interface{}{}
		}
		c.redteamSkillPayloads[canonical] = payloadDataset
	}
}

func (c *coreAgentCallbacks) redteamAutoRegisterSkillPayloadDataset(name string) (string, bool) {
	if c == nil || !c.redteamProfileActive() || c.mcpProvider == nil {
		return "", false
	}
	canonical := canonicalSelectedSkillName(name)
	if canonical == "" {
		return "", false
	}
	payloadDataset, ok := c.redteamSkillPayloads[canonical]
	if !ok || payloadDataset == nil {
		return "", false
	}
	if len(c.redteamSkillPayloadHandles[canonical]) > 0 {
		return fmt.Sprintf("Selected Skill payloads are already registered as payload_handles: %s", strings.Join(c.redteamSkillPayloadHandles[canonical], ", ")), true
	}
	entries := c.mcpProvider.ListAvailableTools(c.ctx, c.principal)
	for _, entry := range entries {
		if strings.TrimSpace(entry.ToolName) != "register_skill_payload_dataset" {
			continue
		}
		args := map[string]interface{}{
			"skill_name":      canonical,
			"payload_dataset": payloadDataset,
			"metadata": map[string]string{
				"skill_name": canonical,
			},
		}
		if runID := strings.TrimSpace(c.messageMetadata["run_id"]); runID != "" {
			args["run_id"] = runID
		}
		if sessionID := strings.TrimSpace(c.messageMetadata["session_id"]); sessionID != "" {
			args["session_id"] = sessionID
			args["metadata"].(map[string]string)["session_id"] = sessionID
		}
		if grant := strings.TrimSpace(c.messageMetadata["evaluation_execution_grant"]); grant != "" {
			args["metadata"].(map[string]string)["evaluation_execution_grant"] = grant
		}
		ctx, cancel := context.WithTimeout(c.ctx, remoteMCPToolTimeout())
		result, err := c.mcpProvider.CallTool(ctx, c.principal, entry.ServerID, entry.ToolName, args)
		cancel()
		if err != nil {
			return fmt.Sprintf("Error: register_skill_payload_dataset failed after selected Skill run: %v", err), true
		}
		handles := redteamExtractPayloadHandles(result)
		if len(handles) > 0 {
			if c.redteamSkillPayloadHandles == nil {
				c.redteamSkillPayloadHandles = map[string][]string{}
			}
			c.redteamSkillPayloadHandles[canonical] = handles
			return fmt.Sprintf("Selected Skill payloads registered as payload_handles: %s. Next call execute_redteam_evaluation_batch with these payload_handles and selected_skills.", strings.Join(handles, ", ")), true
		}
		return "Error: register_skill_payload_dataset did not return payload_handles; do not re-run the Skill, report the registration failure.", true
	}
	return "", false
}

func (c *coreAgentCallbacks) redteamSelectedSkillNames() []string {
	if c == nil || !c.redteamProfileActive() {
		return nil
	}
	if strings.TrimSpace(c.messageMetadata["evaluation_action"]) != "confirm_plan" {
		return nil
	}
	var selected []string
	if raw := strings.TrimSpace(c.messageMetadata["selected_skill_names_json"]); raw != "" {
		_ = json.Unmarshal([]byte(raw), &selected)
	}
	out := make([]string, 0, len(selected))
	seen := map[string]bool{}
	for _, item := range selected {
		canonical := canonicalSelectedSkillName(item)
		if canonical == "" || seen[canonical] {
			continue
		}
		seen[canonical] = true
		out = append(out, canonical)
	}
	return out
}

func (c *coreAgentCallbacks) redteamConfirmedSkillHasRun() bool {
	for _, selected := range c.redteamSelectedSkillNames() {
		if c.redteamSkillRuns[canonicalSelectedSkillName(selected)] {
			return true
		}
	}
	return false
}

func (c *coreAgentCallbacks) redteamMCPBatchRequiresSkillRun(toolName string, args map[string]interface{}) (bool, string) {
	if c == nil || !c.redteamProfileActive() {
		return false, ""
	}
	if strings.TrimSpace(toolName) != "execute_redteam_evaluation_batch" {
		return false, ""
	}
	selected := c.redteamSelectedSkillNames()
	if len(selected) == 0 {
		return false, ""
	}
	if !c.redteamConfirmedSkillHasRun() {
		return true, "confirmed plan selected a native Skill; call manage_skill(action=\"run\") for one of the selected Skills before execute_redteam_evaluation_batch"
	}
	if !redteamBatchArgsHavePayloadHandles(args) {
		if handles := c.redteamRegisteredPayloadHandlesForSelectedSkills(); len(handles) > 0 {
			args["payload_handles"] = handles
			if _, ok := args["selected_skills"]; !ok {
				args["selected_skills"] = selected
			}
			if strings.TrimSpace(fmt.Sprint(args["run_id"])) == "" || strings.TrimSpace(fmt.Sprint(args["run_id"])) == "<nil>" {
				if runID := strings.TrimSpace(c.messageMetadata["run_id"]); runID != "" {
					args["run_id"] = runID
				}
			}
			if strings.TrimSpace(fmt.Sprint(args["session_id"])) == "" || strings.TrimSpace(fmt.Sprint(args["session_id"])) == "<nil>" {
				if sessionID := strings.TrimSpace(c.messageMetadata["session_id"]); sessionID != "" {
					args["session_id"] = sessionID
				}
			}
			return false, ""
		}
		return true, "confirmed selected Skill has run; call register_skill_payload_dataset with the Skill output payload_dataset, then pass the returned payload_handles into execute_redteam_evaluation_batch"
	}
	return false, ""
}

func (c *coreAgentCallbacks) redteamRegisteredPayloadHandlesForSelectedSkills() []string {
	if c == nil || len(c.redteamSkillPayloadHandles) == 0 {
		return nil
	}
	handles := []string{}
	for _, selected := range c.redteamSelectedSkillNames() {
		handles = append(handles, c.redteamSkillPayloadHandles[canonicalSelectedSkillName(selected)]...)
	}
	return uniqueStringsPreserveOrder(handles)
}

func redteamBatchArgsHavePayloadHandles(args map[string]interface{}) bool {
	raw, ok := args["payload_handles"]
	if !ok {
		return false
	}
	switch typed := raw.(type) {
	case []interface{}:
		for _, item := range typed {
			if strings.TrimSpace(fmt.Sprint(item)) != "" {
				return true
			}
		}
	case []string:
		for _, item := range typed {
			if strings.TrimSpace(item) != "" {
				return true
			}
		}
	case string:
		return strings.TrimSpace(typed) != ""
	}
	return false
}

func (c *coreAgentCallbacks) redteamPrepareSkillPayloadRegistration(toolName string, args map[string]interface{}) (map[string]interface{}, bool, string) {
	if c == nil || !c.redteamProfileActive() || strings.TrimSpace(toolName) != "register_skill_payload_dataset" {
		return args, false, ""
	}
	selected := c.redteamSelectedSkillNames()
	if len(selected) == 0 {
		return args, false, ""
	}
	name := canonicalSelectedSkillName(stringArg(args, "skill_name"))
	if name == "" && len(selected) == 1 {
		name = canonicalSelectedSkillName(selected[0])
	}
	if name == "" {
		return args, true, "register_skill_payload_dataset requires skill_name for a selected Skill"
	}
	matched := false
	for _, item := range selected {
		if canonicalSelectedSkillName(item) == name {
			matched = true
			break
		}
	}
	if !matched {
		return args, true, "register_skill_payload_dataset skill_name was not selected in the accepted plan_confirm"
	}
	payloadDataset, ok := c.redteamSkillPayloads[name]
	if !ok || payloadDataset == nil {
		return args, true, "selected Skill did not return payload_dataset; cannot register Skill payloads or fall back to other payload sources"
	}
	safeArgs := make(map[string]interface{}, len(args)+1)
	for k, v := range args {
		safeArgs[k] = v
	}
	safeArgs["skill_name"] = name
	safeArgs["payload_dataset"] = payloadDataset
	if strings.TrimSpace(fmt.Sprint(safeArgs["run_id"])) == "" || strings.TrimSpace(fmt.Sprint(safeArgs["run_id"])) == "<nil>" {
		if runID := strings.TrimSpace(c.messageMetadata["run_id"]); runID != "" {
			safeArgs["run_id"] = runID
		}
	}
	if strings.TrimSpace(fmt.Sprint(safeArgs["session_id"])) == "" || strings.TrimSpace(fmt.Sprint(safeArgs["session_id"])) == "<nil>" {
		if sessionID := strings.TrimSpace(c.messageMetadata["session_id"]); sessionID != "" {
			safeArgs["session_id"] = sessionID
		}
	}
	return safeArgs, false, ""
}

func extractRedteamSkillPayloadDataset(output string) (interface{}, bool) {
	text := strings.TrimSpace(output)
	if text == "" {
		return nil, false
	}
	for _, candidate := range redteamSkillJSONCandidates(text) {
		var root map[string]interface{}
		if err := json.Unmarshal([]byte(candidate), &root); err != nil {
			continue
		}
		if payloadDataset, ok := root["payload_dataset"]; ok && redteamPayloadDatasetHasPayloads(payloadDataset) {
			return payloadDataset, true
		}
		if redteamPayloadDatasetHasPayloads(root) {
			return root, true
		}
	}
	return nil, false
}

func redteamSkillJSONCandidates(text string) []string {
	candidates := []string{text}
	if start := strings.Index(text, "{"); start >= 0 {
		if end := strings.LastIndex(text, "}"); end > start {
			candidates = append(candidates, text[start:end+1])
		}
	}
	for _, fence := range []string{"```json", "```"} {
		remaining := text
		for {
			start := strings.Index(strings.ToLower(remaining), fence)
			if start < 0 {
				break
			}
			bodyStart := start + len(fence)
			end := strings.Index(remaining[bodyStart:], "```")
			if end < 0 {
				break
			}
			candidates = append(candidates, strings.TrimSpace(remaining[bodyStart:bodyStart+end]))
			remaining = remaining[bodyStart+end+3:]
		}
	}
	return candidates
}

func redteamPayloadDatasetHasPayloads(value interface{}) bool {
	root, ok := value.(map[string]interface{})
	if !ok {
		return false
	}
	rawPayloads, ok := root["payloads"]
	if !ok {
		return false
	}
	payloads, ok := rawPayloads.([]interface{})
	if !ok {
		return false
	}
	for _, item := range payloads {
		payload, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if strings.TrimSpace(fmt.Sprint(payload["payload_text"])) != "" || strings.TrimSpace(fmt.Sprint(payload["prompt"])) != "" || strings.TrimSpace(fmt.Sprint(payload["content"])) != "" {
			return true
		}
	}
	return false
}

func redteamExtractPayloadHandles(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	for _, candidate := range redteamSkillJSONCandidates(text) {
		handles := redteamExtractPayloadHandlesFromJSON(candidate)
		if len(handles) > 0 {
			return handles
		}
	}
	return nil
}

func redteamExtractPayloadHandlesFromJSON(text string) []string {
	var root map[string]interface{}
	if err := json.Unmarshal([]byte(text), &root); err != nil {
		return nil
	}
	if handles := redteamPayloadHandlesFromValue(root["payload_handles"]); len(handles) > 0 {
		return handles
	}
	if result, ok := root["result"].(map[string]interface{}); ok {
		if handles := redteamPayloadHandlesFromValue(result["payload_handles"]); len(handles) > 0 {
			return handles
		}
		if handles := redteamExtractMCPContentPayloadHandles(result["content"]); len(handles) > 0 {
			return handles
		}
	}
	if handles := redteamExtractMCPContentPayloadHandles(root["content"]); len(handles) > 0 {
		return handles
	}
	return nil
}

func redteamPayloadHandlesFromValue(value interface{}) []string {
	switch typed := value.(type) {
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if handle := strings.TrimSpace(fmt.Sprint(item)); handle != "" {
				out = append(out, handle)
			}
		}
		return uniqueStringsPreserveOrder(out)
	case []string:
		return uniqueStringsPreserveOrder(typed)
	case string:
		if strings.TrimSpace(typed) != "" {
			return []string{strings.TrimSpace(typed)}
		}
	}
	return nil
}

func redteamExtractMCPContentPayloadHandles(value interface{}) []string {
	items, ok := value.([]interface{})
	if !ok {
		return nil
	}
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(m["text"]))
		if text == "" {
			continue
		}
		if handles := redteamExtractPayloadHandles(text); len(handles) > 0 {
			return handles
		}
	}
	return nil
}

func (c *coreAgentCallbacks) redteamSkillRunAllowed(args map[string]interface{}) (bool, string) {
	if c == nil || !c.redteamProfileActive() {
		return true, ""
	}
	if strings.TrimSpace(c.messageMetadata["evaluation_action"]) != "confirm_plan" {
		return false, "security evaluation Skill execution requires an accepted plan_confirm"
	}
	name := stringArg(args, "name")
	if name == "" {
		return false, "missing selected Skill name"
	}
	var selected []string
	if raw := strings.TrimSpace(c.messageMetadata["selected_skill_names_json"]); raw != "" {
		_ = json.Unmarshal([]byte(raw), &selected)
	}
	if len(selected) == 0 {
		return false, "no confirmed Skill was selected"
	}
	canonicalName := canonicalSelectedSkillName(name)
	for _, item := range selected {
		if strings.EqualFold(strings.TrimSpace(item), name) || canonicalSelectedSkillName(item) == canonicalName {
			if len(c.redteamSkillPayloadHandles[canonicalName]) > 0 {
				return false, "selected Skill already ran and payloads were registered; call execute_redteam_evaluation_batch with the registered payload_handles"
			}
			return true, ""
		}
	}
	return false, "Skill was not selected in the accepted plan_confirm"
}

func canonicalSelectedSkillName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, prefix := range []string{"skillhub:", "skill:"} {
		if strings.HasPrefix(value, prefix) {
			value = strings.TrimSpace(value[len(prefix):])
			break
		}
	}
	if before, _, ok := strings.Cut(value, "/"); ok && strings.TrimSpace(before) != "" {
		value = strings.TrimSpace(before)
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
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
		if recovered, ok := c.redteamRecoverSelectedSkillPayloadResult(name, result); ok {
			return recovered
		}
		return agent.ToolExecutionResult{Result: fmt.Sprintf("Error: %v", err), Outcome: agent.ToolExecutionOutcomeError}
	}
	return agent.ToolExecutionResult{Result: result, Outcome: agent.ToolExecutionOutcomeOK}
}

func (c *coreAgentCallbacks) redteamRecoverSelectedSkillPayloadResult(name string, output string) (agent.ToolExecutionResult, bool) {
	if c == nil || !c.redteamProfileActive() || strings.TrimSpace(c.messageMetadata["evaluation_action"]) != "confirm_plan" {
		return agent.ToolExecutionResult{}, false
	}
	if strings.TrimSpace(output) == "" {
		return agent.ToolExecutionResult{}, false
	}
	canonicalName := canonicalSelectedSkillName(name)
	if canonicalName == "" {
		return agent.ToolExecutionResult{}, false
	}
	matched := false
	for _, selected := range c.redteamSelectedSkillNames() {
		if canonicalSelectedSkillName(selected) == canonicalName {
			matched = true
			break
		}
	}
	if !matched {
		return agent.ToolExecutionResult{}, false
	}
	if _, ok := extractRedteamSkillPayloadDataset(output); !ok {
		return agent.ToolExecutionResult{}, false
	}
	return agent.ToolExecutionResult{Result: output, Outcome: agent.ToolExecutionOutcomeOK}, true
}

// manageSkillToolDef returns the manage_skill tool definition for BuildTools.
func (c *coreAgentCallbacks) manageSkillToolDef() map[string]interface{} {
	return functionToolDefinition("manage_skill",
		"Manage and execute installed Skills. Actions: list (show installed skills), search (find skills by keyword), run (execute a skill by name with arguments).",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{"type": "string", "description": "Action: list, search, run"},
				"query":  map[string]interface{}{"type": "string", "description": "Search keyword (required for search)"},
				"name":   map[string]interface{}{"type": "string", "description": "Skill name (required for run)"},
				"args":   map[string]interface{}{"type": "object", "description": "Skill arguments (for run). Template variables like {{key}} in skill commands are replaced with values from this object."},
				"input":  map[string]interface{}{"type": "string", "description": "Input parameter (for run, shorthand for args.input)"},
				"output": map[string]interface{}{"type": "string", "description": "Output parameter (for run, shorthand for args.output)"},
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
