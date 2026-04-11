package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

// ── Run Status ──────────────────────────────────────────────────────────

// SkillRunSessionMeta captures the remote session associated with a skill run.
type SkillRunSessionMeta struct {
	SessionID       string `json:"session_id,omitempty"`
	Tool            string `json:"tool,omitempty"`
	ProjectPath     string `json:"project_path,omitempty"`
	Status          string `json:"status,omitempty"`
	JobID           string `json:"job_id,omitempty"`
	RunID           string `json:"run_id,omitempty"`
	ResumeSessionID string `json:"resume_session_id,omitempty"`
	LaunchSource    string `json:"launch_source,omitempty"`
}

// SkillRunSummary provides a compact, user-facing summary of the most
// important state for a skill run.
type SkillRunSummary struct {
	CurrentStepIndex          int    `json:"current_step_index,omitempty"`
	CurrentStep               string `json:"current_step,omitempty"`
	CurrentStepStatus         string `json:"current_step_status,omitempty"`
	LastCompletedStep         string `json:"last_completed_step,omitempty"`
	LastCompletedStepIndex    int    `json:"last_completed_step_index,omitempty"`
	LastOutputSnippet         string `json:"last_output_snippet,omitempty"`
	LastErrorSnippet          string `json:"last_error_snippet,omitempty"`
	HasSessionBinding         bool   `json:"has_session_binding,omitempty"`
	NeedsArtifactVerification bool   `json:"needs_artifact_verification,omitempty"`
	ArtifactPath              string `json:"artifact_path,omitempty"`
	ArtifactStatus            string `json:"artifact_status,omitempty"`
}

// SkillRunStatus 表示一次 skill 执行的状态。
type SkillRunStatus struct {
	RunID          string               `json:"run_id"`
	Skill          string               `json:"skill"`
	Status         string               `json:"status"` // "running", "success", "failed", "cancelled"
	Steps          []StepResult         `json:"steps"`
	Session        *SkillRunSessionMeta `json:"session,omitempty"`
	Summary        SkillRunSummary      `json:"summary,omitempty"`
	ExpectedOutput string               `json:"expected_output,omitempty"`
	StartedAt      string               `json:"started_at"`
	EndedAt        string               `json:"ended_at,omitempty"`
	Error          string               `json:"error,omitempty"`
	DurationMs     int64                `json:"duration_ms,omitempty"`
	TotalSteps     int                  `json:"total_steps,omitempty"`
	FailedSteps    int                  `json:"failed_steps,omitempty"`
	SkippedSteps   int                  `json:"skipped_steps,omitempty"`
}

// StepResult 记录单步执行结果。
type StepResult struct {
	Index           int      `json:"index"`
	Name            string   `json:"name,omitempty"`
	Action          string   `json:"action"`
	Status          string   `json:"status"` // "pending", "running", "success", "failed", "skipped", "timeout"
	Output          string   `json:"output,omitempty"`
	Error           string   `json:"error,omitempty"`
	ExitCode        int      `json:"exit_code,omitempty"`
	StdoutLastLines []string `json:"stdout_last_lines,omitempty"`
	StderrLastLines []string `json:"stderr_last_lines,omitempty"`
	ShellPath       string   `json:"shell_path,omitempty"` // 使用的 shell（仅 bash action）
	CommandResolved string   `json:"command_resolved,omitempty"`
	DurationMs      int64    `json:"duration_ms,omitempty"`
	Timeout         bool     `json:"timeout,omitempty"`
}

// ── Skill Runner ────────────────────────────────────────────────────────

// SkillRunner 提供异步、平台感知的 skill 执行能力。
type SkillRunner struct {
	executor      *SkillExecutor
	mu            sync.RWMutex
	runs          map[string]*skillRun
	counter       int
	uploadTrigger *AutoUploadTrigger
	packageFn     func(skillName string) (string, error) // packageSkillForMarket
}

type skillRun struct {
	status       SkillRunStatus
	cancel       context.CancelFunc
	templateVars map[string]string
}

// NewSkillRunner 创建 SkillRunner。
func NewSkillRunner(executor *SkillExecutor) *SkillRunner {
	return &SkillRunner{
		executor: executor,
		runs:     make(map[string]*skillRun),
	}
}

// StartRun 异步启动 skill 执行，返回 runID 供前端轮询。
func (r *SkillRunner) StartRun(skillName string, runArgs map[string]interface{}) (string, error) {
	// 查找 skill
	r.executor.mu.RLock()
	var target *NLSkillEntry
	for _, s := range r.executor.loadSkills() {
		if s.Name == skillName && s.Status == "active" {
			cp := s
			target = &cp
			break
		}
	}
	r.executor.mu.RUnlock()

	if target == nil {
		return "", fmt.Errorf("skill %q not found or disabled", skillName)
	}

	// 平台检查
	if err := checkPlatformCompat(target); err != nil {
		return "", err
	}

	// 文件存在性检查（bash step 中引用的文件）
	if err := checkFileReferences(target); err != nil {
		return "", err
	}
	if len(target.Steps) == 0 {
		return "", fmt.Errorf("skill %q has no executable steps", skillName)
	}

	templateVars := normalizeSkillRunVars(runArgs)

	// 生成 runID
	r.mu.Lock()
	r.counter++
	runID := fmt.Sprintf("run-%d-%d", time.Now().UnixMilli(), r.counter)

	ctx, cancel := context.WithCancel(context.Background())
	run := &skillRun{
		status: SkillRunStatus{
			RunID:          runID,
			Skill:          skillName,
			Status:         "running",
			ExpectedOutput: strings.TrimSpace(templateVars["output"]),
			StartedAt:      time.Now().Format(time.RFC3339),
			Steps:          make([]StepResult, len(target.Steps)),
		},
		cancel:       cancel,
		templateVars: templateVars,
	}
	for i, step := range target.Steps {
		run.status.Steps[i] = StepResult{
			Index:  i,
			Action: step.Action,
			Status: "pending",
		}
	}
	r.runs[runID] = run
	r.mu.Unlock()

	// 异步执行
	go r.executeAsync(ctx, run, target)

	return runID, nil
}

// GetRunStatus 返回指定 runID 的执行状态（深拷贝）。
func (r *SkillRunner) GetRunStatus(runID string) (*SkillRunStatus, error) {
	r.mu.RLock()
	run, ok := r.runs[runID]
	if !ok {
		r.mu.RUnlock()
		return nil, fmt.Errorf("run %q not found", runID)
	}
	cp := run.status
	cp.Steps = make([]StepResult, len(run.status.Steps))
	copy(cp.Steps, run.status.Steps)
	r.mu.RUnlock()

	r.hydrateRunSessionMeta(&cp)
	summarizeSkillRun(&cp)
	return &cp, nil
}

// CancelRun 取消正在执行的 skill。
func (r *SkillRunner) CancelRun(runID string) error {
	r.mu.RLock()
	run, ok := r.runs[runID]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("run %q not found", runID)
	}
	run.cancel()
	return nil
}

// ListRuns 返回所有执行记录。
func (r *SkillRunner) ListRuns() []SkillRunStatus {
	r.mu.RLock()
	result := make([]SkillRunStatus, 0, len(r.runs))
	for _, run := range r.runs {
		cp := run.status
		cp.Steps = make([]StepResult, len(run.status.Steps))
		copy(cp.Steps, run.status.Steps)
		result = append(result, cp)
	}
	r.mu.RUnlock()
	for i := range result {
		r.hydrateRunSessionMeta(&result[i])
		summarizeSkillRun(&result[i])
	}
	return result
}

// CleanupFinished 清理已完成的执行记录（保留最近 maxKeep 条）。
func (r *SkillRunner) CleanupFinished(maxKeep int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.runs) <= maxKeep {
		return
	}
	// 收集已完成的 run，按结束时间排序后删除最旧的
	type finishedEntry struct {
		id      string
		endedAt string
	}
	var finished []finishedEntry
	for id, run := range r.runs {
		if run.status.Status != "running" {
			finished = append(finished, finishedEntry{id: id, endedAt: run.status.EndedAt})
		}
	}
	// 按 EndedAt 升序排序（最旧的在前）
	for i := 0; i < len(finished); i++ {
		for j := i + 1; j < len(finished); j++ {
			if finished[j].endedAt < finished[i].endedAt {
				finished[i], finished[j] = finished[j], finished[i]
			}
		}
	}
	// 删除最旧的，直到总数 <= maxKeep
	for _, f := range finished {
		if len(r.runs) <= maxKeep {
			break
		}
		delete(r.runs, f.id)
	}
}

func (r *SkillRunner) SetRunSessionMeta(runID string, meta SkillRunSessionMeta) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[runID]
	if !ok {
		return
	}
	metaCopy := meta
	run.status.Session = &metaCopy
}

func (r *SkillRunner) hydrateRunSessionMeta(status *SkillRunStatus) {
	if status == nil || status.Session == nil || status.Session.SessionID == "" || r.executor == nil || r.executor.manager == nil {
		return
	}
	session, ok := r.executor.manager.Get(status.Session.SessionID)
	if !ok || session == nil {
		return
	}
	session.mu.RLock()
	status.Session.Status = string(session.Status)
	if status.Session.JobID == "" {
		status.Session.JobID = session.JobID
	}
	if status.Session.RunID == "" {
		status.Session.RunID = session.RunID
	}
	session.mu.RUnlock()
}

func summarizeSkillRun(status *SkillRunStatus) {
	if status == nil {
		return
	}
	status.Summary = SkillRunSummary{}
	status.TotalSteps = len(status.Steps)
	status.FailedSteps = 0
	status.SkippedSteps = 0
	if t, err := time.Parse(time.RFC3339, status.EndedAt); err == nil {
		if s, err := time.Parse(time.RFC3339, status.StartedAt); err == nil {
			status.DurationMs = t.Sub(s).Milliseconds()
		}
	}
	if status.Session != nil && strings.TrimSpace(status.Session.SessionID) != "" {
		status.Summary.HasSessionBinding = true
	}
	artifactPath := strings.TrimSpace(status.ExpectedOutput)
	if artifactPath == "" {
		artifactPath = detectArtifactPathFromStatus(status)
	}
	if artifactPath != "" {
		status.Summary.ArtifactPath = artifactPath
		if status.Status == "running" {
			status.Summary.ArtifactStatus = "pending"
		} else if artifactExists(artifactPath) {
			status.Summary.ArtifactStatus = "verified"
		} else {
			status.Summary.ArtifactStatus = "missing"
		}
	}
	if craftVerificationPassedStatus(status) {
		status.Summary.ArtifactStatus = "verified"
	}
	if isInstructionOnlySkillStatus(status) {
		status.Summary.NeedsArtifactVerification = status.Summary.ArtifactStatus != "verified"
	}
	for i, step := range status.Steps {
		switch step.Status {
		case "running":
			status.Summary.CurrentStepIndex = i
			status.Summary.CurrentStep = step.Action
			status.Summary.CurrentStepStatus = step.Status
		case "success":
			status.Summary.LastCompletedStepIndex = i
			status.Summary.LastCompletedStep = step.Action
			if snippet := strings.TrimSpace(step.Output); snippet != "" {
				status.Summary.LastOutputSnippet = truncateSkillRunSnippet(snippet)
			}
		case "failed":
			status.Summary.CurrentStepIndex = i
			status.Summary.CurrentStep = step.Action
			status.Summary.CurrentStepStatus = step.Status
			status.FailedSteps++
			if snippet := strings.TrimSpace(firstNonEmptyTraceText(step.Error, step.Output)); snippet != "" {
				status.Summary.LastErrorSnippet = truncateSkillRunSnippet(snippet)
			}
		case "skipped":
			status.SkippedSteps++
		}
	}
	if status.Summary.CurrentStep == "" && len(status.Steps) > 0 {
		for i := len(status.Steps) - 1; i >= 0; i-- {
			step := status.Steps[i]
			if step.Status != "pending" {
				status.Summary.CurrentStepIndex = i
				status.Summary.CurrentStep = step.Action
				status.Summary.CurrentStepStatus = step.Status
				break
			}
		}
	}
	if status.Summary.LastOutputSnippet == "" {
		for i := len(status.Steps) - 1; i >= 0; i-- {
			if snippet := strings.TrimSpace(status.Steps[i].Output); snippet != "" {
				status.Summary.LastOutputSnippet = truncateSkillRunSnippet(snippet)
				break
			}
		}
	}
	if status.Summary.LastErrorSnippet == "" {
		if snippet := strings.TrimSpace(status.Error); snippet != "" {
			status.Summary.LastErrorSnippet = truncateSkillRunSnippet(snippet)
		}
	}
}

func detectArtifactPathFromStatus(status *SkillRunStatus) string {
	if status == nil {
		return ""
	}
	for i := len(status.Steps) - 1; i >= 0; i-- {
		path := detectArtifactPathFromText(status.Steps[i].Output)
		if path != "" {
			return path
		}
	}
	return ""
}

func detectArtifactPathFromText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "📁 脚本路径:") {
			continue
		}
		if candidate := extractArtifactPathCandidate(line); candidate != "" {
			return candidate
		}
	}
	return ""
}

func extractArtifactPathCandidate(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.Trim(trimmed, "`\"'“””)。；，")
	if strings.HasSuffix(strings.ToLower(trimmed), ".pdf") && filepath.IsAbs(trimmed) {
		return trimmed
	}
	for _, field := range strings.Fields(trimmed) {
		candidate := strings.Trim(field, "`\"'“””)。；，")
		if strings.HasSuffix(strings.ToLower(candidate), ".pdf") && filepath.IsAbs(candidate) {
			return candidate
		}
	}
	return ""
}

func artifactExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Size() > 0
}

func normalizeSkillRunVars(runArgs map[string]interface{}) map[string]string {
	vars := map[string]string{}
	if rawArgs, ok := runArgs["args"].(map[string]interface{}); ok {
		for key, value := range rawArgs {
			if str, ok := value.(string); ok {
				vars[key] = str
			}
		}
	}
	if _, ok := vars["input"]; !ok {
		if input, _ := runArgs["input"].(string); input != "" {
			vars["input"] = input
		}
	}
	if _, ok := vars["output"]; !ok {
		if output, _ := runArgs["output"].(string); output != "" {
			vars["output"] = output
		}
	}
	if len(vars) == 0 {
		return nil
	}
	return vars
}

func resolveSkillStep(step NLSkillStep, vars map[string]string, skillDir string) NLSkillStep {
	resolved := step
	if params, ok := resolveSkillValue(step.Params, vars).(map[string]interface{}); ok {
		resolved.Params = params
	} else if step.Params != nil {
		resolved.Params = map[string]interface{}{}
	}
	if resolved.Action == "craft_tool" && len(vars) != 0 {
		if resolved.Params == nil {
			resolved.Params = map[string]interface{}{}
		}
		for _, key := range []string{"input", "output", "topic"} {
			if strings.TrimSpace(stringVal(resolved.Params, key)) != "" {
				continue
			}
			if value := strings.TrimSpace(vars[key]); value != "" {
				resolved.Params[key] = value
			}
		}
	}
	if workDir, _ := resolved.Params["working_dir"].(string); workDir != "" && !filepath.IsAbs(workDir) && skillDir != "" {
		resolved.Params["working_dir"] = filepath.Clean(filepath.Join(skillDir, workDir))
	}
	return resolved
}

func resolveSkillValue(value interface{}, vars map[string]string) interface{} {
	switch typed := value.(type) {
	case string:
		return substituteSkillVariables(typed, vars)
	case map[string]interface{}:
		resolved := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			resolved[key] = resolveSkillValue(item, vars)
		}
		return resolved
	case []interface{}:
		resolved := make([]interface{}, len(typed))
		for i, item := range typed {
			resolved[i] = resolveSkillValue(item, vars)
		}
		return resolved
	default:
		return value
	}
}

func substituteSkillVariables(command string, vars map[string]string) string {
	if command == "" {
		return command
	}
	original := command
	if len(vars) != 0 {
		keys := make([]string, 0, len(vars))
		for key := range vars {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			value := quoteSkillInputForShell(vars[key])
			command = strings.ReplaceAll(command, "{{"+key+"}}", value)
			command = strings.ReplaceAll(command, "${"+key+"}", value)
		}
	}
	result := stripUnresolvedSkillPlaceholders(command)
	// 记录变量替换结果，方便排查跨平台路径问题
	if result != original {
		log.Printf("[skill-runner] variable substitution: %q → %q", original, result)
	}
	return result
}

var unresolvedSkillPlaceholderPattern = regexp.MustCompile(`\{\{[^{}]+\}\}|\$\{[^{}]+\}`)

func stripUnresolvedSkillPlaceholders(text string) string {
	if text == "" {
		return text
	}
	return unresolvedSkillPlaceholderPattern.ReplaceAllString(text, "")
}

func quoteSkillInputForShell(input string) string {
	if input == "" {
		return "''"
	}
	if runtime.GOOS == "windows" {
		return "'" + strings.ReplaceAll(input, "'", "''") + "'"
	}
	return "'" + strings.ReplaceAll(input, "'", `'"'"'`) + "'"
}

func truncateSkillRunSnippet(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if len(text) > 160 {
		return text[:160] + "..."
	}
	return text
}

func isInstructionOnlySkillEntry(skill *NLSkillEntry) bool {
	if skill == nil || len(skill.Steps) != 1 {
		return false
	}
	step := skill.Steps[0]
	if step.Action != "craft_tool" {
		return false
	}
	params := step.Params
	if len(params) == 0 {
		return false
	}
	instructions, _ := params["instructions"].(string)
	if strings.TrimSpace(instructions) == "" {
		return false
	}
	if strings.TrimSpace(stringVal(params, "task")) != "" {
		return false
	}
	if strings.TrimSpace(stringVal(params, "output_path")) != "" {
		return false
	}
	if raw, ok := params["expected_artifacts"]; ok {
		switch typed := raw.(type) {
		case []string:
			if len(typed) > 0 {
				return false
			}
		case []interface{}:
			if len(typed) > 0 {
				return false
			}
		}
	}
	return true
}

func isInstructionOnlySkillStatus(status *SkillRunStatus) bool {
	if status == nil || len(status.Steps) != 1 {
		return false
	}
	step := status.Steps[0]
	if step.Action != "craft_tool" {
		return false
	}
	output := step.Output
	if output == "" {
		output = status.Summary.LastOutputSnippet
	}
	if strings.Contains(output, "verification: passed") {
		return true
	}
	return strings.Contains(output, "📝 脚本语言:") && strings.Contains(output, "📁 脚本路径:")
}

func craftVerificationPassedStatus(status *SkillRunStatus) bool {
	if status == nil {
		return false
	}
	for _, step := range status.Steps {
		if strings.Contains(step.Output, "verification: passed") {
			return true
		}
	}
	return false
}

func (r *SkillRunner) latestRunSessionID(runID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	run, ok := r.runs[runID]
	if !ok || run.status.Session == nil {
		return ""
	}
	return strings.TrimSpace(run.status.Session.SessionID)
}

func (r *SkillRunner) templateVarsForRun(runID string) map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	run, ok := r.runs[runID]
	if !ok || len(run.templateVars) == 0 {
		return nil
	}
	vars := make(map[string]string, len(run.templateVars))
	for key, value := range run.templateVars {
		vars[key] = value
	}
	return vars
}

func resolveSkillStepSessionID(step NLSkillStep, fallback string, manager *RemoteSessionManager) string {
	explicitSessionID, _ := step.Params["session_id"].(string)
	explicitSessionID = strings.TrimSpace(explicitSessionID)
	fallback = strings.TrimSpace(fallback)
	if explicitSessionID == "" {
		return fallback
	}
	if manager == nil || fallback == "" || explicitSessionID == fallback {
		return explicitSessionID
	}
	if _, ok := manager.Get(explicitSessionID); ok {
		return explicitSessionID
	}
	if _, ok := manager.Get(fallback); ok {
		return fallback
	}
	return explicitSessionID
}

func (r *SkillRunner) resolveStepSessionID(runID string, step NLSkillStep) string {
	return resolveSkillStepSessionID(step, r.latestRunSessionID(runID), r.executor.manager)
}

// ── 异步执行核心 ────────────────────────────────────────────────────────

func (r *SkillRunner) executeAsync(ctx context.Context, run *skillRun, skill *NLSkillEntry) {
	// Global timeout: 5 minutes default for the entire skill execution.
	globalTimeout := 5 * time.Minute
	globalCtx, globalCancel := context.WithTimeout(ctx, globalTimeout)
	defer globalCancel()

	defer func() {
		if rec := recover(); rec != nil {
			r.mu.Lock()
			run.status.Status = "failed"
			run.status.Error = fmt.Sprintf("panic: %v", rec)
			run.status.EndedAt = time.Now().Format(time.RFC3339)
			r.mu.Unlock()
		}
	}()

	// Interactive skills should not be auto-executed — they're meant to be
	// invoked on-demand by AI agents. If mode == "interactive", skip automatic
	// step execution and mark as success (the skill's instructions are available
	// for the AI context, not for runner auto-execution).
	if strings.EqualFold(skill.Mode, "interactive") {
		r.mu.Lock()
		for i := range skill.Steps {
			run.status.Steps[i].Status = "skipped"
		}
		run.status.Status = "success"
		run.status.EndedAt = time.Now().Format(time.RFC3339)
		r.mu.Unlock()
		r.updateUsageStats(skill, nil)
		return
	}

	var execErr error
	hasFailure := false
	for i, step := range skill.Steps {
		// Check for global timeout
		select {
		case <-globalCtx.Done():
			r.mu.Lock()
			for j := i; j < len(skill.Steps); j++ {
				run.status.Steps[j].Status = "skipped"
				if j == i {
					run.status.Steps[j].Timeout = true
					run.status.Steps[j].Error = "global timeout exceeded"
				}
			}
			run.status.Status = "failed"
			run.status.Error = fmt.Sprintf("skill execution exceeded global timeout of %v", globalTimeout)
			run.status.EndedAt = time.Now().Format(time.RFC3339)
			r.mu.Unlock()
			return
		case <-ctx.Done():
			r.mu.Lock()
			for j := i; j < len(skill.Steps); j++ {
				run.status.Steps[j].Status = "skipped"
			}
			run.status.Status = "cancelled"
			run.status.EndedAt = time.Now().Format(time.RFC3339)
			r.mu.Unlock()
			return
		default:
		}

		// Handle condition: "on_failure" — skip if no prior failure
		if step.Condition == "on_failure" && !hasFailure {
			r.mu.Lock()
			run.status.Steps[i].Status = "skipped"
			r.mu.Unlock()
			continue
		}
		// Handle condition: "on_success" — skip if there was a failure
		if step.Condition == "on_success" && hasFailure {
			r.mu.Lock()
			run.status.Steps[i].Status = "skipped"
			r.mu.Unlock()
			continue
		}

		r.mu.Lock()
		run.status.Steps[i].Status = "running"
		r.mu.Unlock()

		resolvedStep := resolveSkillStep(step, r.templateVarsForRun(run.status.RunID), skill.SkillDir)
		log.Printf("[skill-runner] step %d/%d: action=%s command=%q", i+1, len(skill.Steps), resolvedStep.Action, resolveCommandForDisplay(resolvedStep))
		result, stepErr := r.executeStepWithContext(globalCtx, run.status.RunID, resolvedStep, skill.SkillDir)

		r.mu.Lock()
		run.status.Steps[i].Name = resolvedStep.Name
		run.status.Steps[i].CommandResolved = resolveCommandForDisplay(resolvedStep)
		if stepErr != nil {
			run.status.Steps[i].Status = "failed"
			run.status.Steps[i].Error = stepErr.Error()
			run.status.Steps[i].Output = result
			// Extract error details if it's a bashStepError
			if bErr, ok := stepErr.(*bashStepError); ok {
				run.status.Steps[i].ExitCode = bErr.ExitCode()
				run.status.Steps[i].Timeout = bErr.IsTimeout()
				run.status.Steps[i].StdoutLastLines = lastNLines(bErr.Stdout(), 10)
				run.status.Steps[i].StderrLastLines = lastNLines(bErr.Stderr(), 10)
			}
			hasFailure = true
			if step.OnError != "continue" {
				run.status.Status = "failed"
				run.status.Error = fmt.Sprintf("step %d (%s) failed: %s", i+1, step.Action, stepErr.Error())
				run.status.EndedAt = time.Now().Format(time.RFC3339)
				execErr = stepErr
				// 标记剩余 step 为 skipped
				for j := i + 1; j < len(skill.Steps); j++ {
					run.status.Steps[j].Status = "skipped"
				}
				r.mu.Unlock()
				break
			}
			if execErr == nil {
				execErr = stepErr // 记录第一个错误
			}
		} else {
			run.status.Steps[i].Status = "success"
			run.status.Steps[i].Output = result
		}
		r.mu.Unlock()
	}

	r.mu.Lock()
	if run.status.Status == "running" {
		if hasFailure {
			run.status.Status = "failed"
			if execErr != nil {
				run.status.Error = execErr.Error()
			}
		} else {
			run.status.Status = "success"
		}
	}
	run.status.EndedAt = time.Now().Format(time.RFC3339)
	r.mu.Unlock()

	// 更新 skill 使用统计
	r.updateUsageStats(skill, execErr)

	// 自动上传触发
	r.tryAutoUpload(skill, run)
}

func (r *SkillRunner) updateUsageStats(skill *NLSkillEntry, execErr error) {
	if skill.Source == "file" {
		return
	}
	r.executor.mu.Lock()
	defer r.executor.mu.Unlock()

	skills := r.executor.loadSkills()
	for i, s := range skills {
		if s.Name == skill.Name && s.Source != "file" {
			skills[i].UsageCount++
			skills[i].LastUsedAt = time.Now().Format(time.RFC3339)
			if execErr == nil {
				skills[i].SuccessCount++
				skills[i].LastError = ""
			} else {
				skills[i].LastError = execErr.Error()
			}
			_ = r.executor.saveSkills(skills)
			break
		}
	}
}

// tryAutoUpload 在 skill 执行完成后尝试自动上传到 SkillMarket。
func (r *SkillRunner) tryAutoUpload(skill *NLSkillEntry, run *skillRun) {
	if r.uploadTrigger == nil || r.packageFn == nil {
		return
	}
	if skill.SkillDir == "" {
		return
	}

	// 从 run status 构建 SkillExecutionResult
	r.mu.RLock()
	status := run.status.Status
	hasErr := false
	for _, st := range run.status.Steps {
		if st.Status == "failed" {
			hasErr = true
			break
		}
	}
	r.mu.RUnlock()

	result := &SkillExecutionResult{
		Success:       status == "success",
		HasError:      hasErr,
		OutputQuality: "basic",
	}
	if status == "success" && !hasErr {
		result.OutputQuality = "good"
	}

	localHash := skillDirHash(skill.SkillDir)

	// 记录执行并检查是否满足上传条件
	r.uploadTrigger.RecordExecution(skill.Name, EvaluateSkillExecution(result), localHash)
	if !r.uploadTrigger.ShouldUpload(skill.Name) {
		return
	}

	// 满足条件，打包 zip 并上传（使用独立 context，不受 skill 执行 ctx 影响）
	zipPath, err := r.packageFn(skill.Name)
	if err != nil {
		log.Printf("[auto-upload] package failed for %s: %v", skill.Name, err)
		return
	}
	defer os.Remove(zipPath)

	uploadCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := r.uploadTrigger.SubmitAndMark(uploadCtx, skill.Name, zipPath, localHash); err != nil {
		log.Printf("[auto-upload] upload failed for %s: %v", skill.Name, err)
	}
}

// skillDirHash 计算 skill 目录内容的简单 hash（用于变更检测）。
func skillDirHash(dir string) string {
	h := sha256.New()
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		h.Write([]byte(rel))
		data, err := os.ReadFile(path)
		if err == nil {
			h.Write(data)
		}
		return nil
	})
	return fmt.Sprintf("%x", h.Sum(nil))
}

// ── Step 执行（带 context） ─────────────────────────────────────────────

func (r *SkillRunner) executeStepWithContext(ctx context.Context, runID string, step NLSkillStep, skillDir string) (string, error) {
	switch step.Action {
	case "create_session":
		tool, _ := step.Params["tool"].(string)
		projectPath, _ := step.Params["project_path"].(string)
		projectID, _ := step.Params["project_id"].(string)
		provider, _ := step.Params["provider"].(string)
		resumeSessionID, _ := step.Params["resume_session_id"].(string)
		if tool == "" {
			return "", fmt.Errorf("missing tool parameter")
		}
		starter := r.executor.app.sessionStarter
		if starter == nil {
			r.executor.app.ensureInteractionInfra()
			starter = r.executor.app.sessionStarter
		}
		if starter == nil {
			return "", fmt.Errorf("session starter not initialized")
		}
		startResult, err := starter.Start(CodingSessionStartRequest{
			Tool:               tool,
			ProjectID:          projectID,
			ProjectPath:        projectPath,
			Provider:           provider,
			ResumeSessionID:    resumeSessionID,
			InjectResumePrompt: false,
			LaunchSource:       RemoteLaunchSourceAI,
			ParentRunID:        runID,
		})
		if err != nil {
			return "", err
		}
		resolvedProjectPath := projectPath
		if strings.TrimSpace(startResult.ResolvedProjectPath) != "" {
			resolvedProjectPath = startResult.ResolvedProjectPath
		}
		r.SetRunSessionMeta(runID, SkillRunSessionMeta{
			SessionID:       startResult.View.ID,
			Tool:            startResult.View.Tool,
			ProjectPath:     resolvedProjectPath,
			Status:          string(startResult.View.Status),
			JobID:           startResult.View.JobID,
			RunID:           startResult.View.RunID,
			ResumeSessionID: strings.TrimSpace(resumeSessionID),
			LaunchSource:    string(normalizeRemoteLaunchSource(RemoteLaunchSourceAI)),
		})
		return fmt.Sprintf("会话已创建: ID=%s", startResult.View.ID), nil

	case "send_input":
		sessionID := r.resolveStepSessionID(runID, step)
		text, _ := step.Params["text"].(string)
		if sessionID == "" || text == "" {
			return "", fmt.Errorf("missing session_id or text parameter")
		}
		if r.executor.manager == nil {
			return "", fmt.Errorf("session manager not initialized")
		}
		if err := r.executor.manager.WriteInput(sessionID, text); err != nil {
			return "", err
		}
		return fmt.Sprintf("已发送到会话 %s", sessionID), nil

	case "send_and_observe":
		sessionID := r.resolveStepSessionID(runID, step)
		text, _ := step.Params["text"].(string)
		timeoutSeconds, _ := step.Params["timeout_seconds"].(float64)
		if sessionID == "" || text == "" {
			return "", fmt.Errorf("missing session_id or text parameter")
		}
		if r.executor.manager == nil {
			return "", fmt.Errorf("session manager not initialized")
		}
		return SendAndObserveSession(r.executor.manager, sessionID, text, SessionObserveOptions{
			TimeoutSeconds: timeoutSeconds,
			Lines:          40,
		}, func(renderArgs map[string]interface{}) string {
			h := &IMMessageHandler{app: r.executor.app, manager: r.executor.manager}
			return h.toolGetSessionOutput(renderArgs)
		}), nil

	case "call_mcp_tool":
		serverRef, _ := step.Params["server_id"].(string)
		toolName, _ := step.Params["tool_name"].(string)
		args, _ := step.Params["arguments"].(map[string]interface{})
		if serverRef == "" || toolName == "" {
			return "", fmt.Errorf("missing server_id or tool_name parameter")
		}
		resolvedID, isLocal, err := r.executor.app.resolveMCPServerRef(serverRef)
		if err != nil {
			return "", err
		}
		if isLocal {
			if r.executor.app.localMCPManager == nil {
				return "", fmt.Errorf("local MCP manager not initialized")
			}
			return r.executor.app.localMCPManager.CallTool(resolvedID, toolName, args)
		}
		if r.executor.mcpRegistry == nil {
			return "", fmt.Errorf("MCP registry not initialized")
		}
		return r.executor.mcpRegistry.CallTool(resolvedID, toolName, args)

	case "bash":
		command, _ := step.Params["command"].(string)
		if command == "" {
			return "", fmt.Errorf("missing command parameter")
		}
		return runBashStepWithContext(ctx, command, step.Params, skillDir, r.executor.app)

	case "craft_tool":
		if r.executor == nil || r.executor.app == nil {
			return "", fmt.Errorf("app not initialized")
		}
		return executeCraftToolCore(r.executor.app, nil, step.Params, nil)

	default:
		return "", fmt.Errorf("unknown action: %s", step.Action)
	}
}

// ── bash step 执行（带 context + skillDir 作为默认 working_dir） ────────

func runBashStepWithContext(ctx context.Context, command string, params map[string]interface{}, skillDir string, app *App) (string, error) {
	return runBashStepWithContextFull(ctx, command, params, skillDir, app)
}

func runBashStepWithContextFull(ctx context.Context, command string, params map[string]interface{}, skillDir string, app *App) (string, error) {
	timeout := 120 // default 120 seconds per step
	if t, ok := params["timeout"].(float64); ok && t > 0 {
		timeout = int(t)
		if timeout > 300 {
			timeout = 300
		}
	}

	workDir, _ := params["working_dir"].(string)
	// 如果没有指定 working_dir，使用 skill 目录
	if workDir == "" && skillDir != "" {
		workDir = skillDir
	}

	stepCtx, stepCancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer stepCancel()

	// [Bug #1 fix] On Windows, convert backslash paths to forward slashes
	// before passing to bash. This prevents bash from interpreting
	// backslashes as escape characters in `bash -c "..."` context.
	// Note: only applied for bash path; cmd.exe handles backslashes natively.
	bashCommand := command
	if runtime.GOOS == "windows" {
		bashCommand = convertWindowsPathsInCommand(command)
	}

	var shellName string
	var shellArgs []string
	var tmpScript string // temp script file for bash on Windows
	if runtime.GOOS == "windows" {
		// On Windows, prefer cmd.exe for direct script execution to avoid
		// Git Bash subprocess restrictions that can block nested execFileSync.
		// Only use sh.exe when the command contains shell-specific features.
		if needsBashShell(command) {
			if app != nil {
				if shPath, err := app.findSh(); err == nil {
					shellName = shPath
					log.Printf("[skill-runner] bash step: using shell %s", shPath)
				} else {
					return "", fmt.Errorf("找不到 Unix shell 用于执行 bash 步骤\n%v\n请安装 Git for Windows: https://git-scm.com/download/win", err)
				}
			} else {
				if shPath, err := exec.LookPath("sh.exe"); err == nil {
					// Skip WSL bash on Windows (runtime check only)
					shellName = shPath
				} else {
					return "", fmt.Errorf("找不到 Unix shell 用于执行 bash 步骤，且 app 实例为空")
				}
			}
			// [Bug #1 fix] Use temp script file instead of bash -c on Windows.
			// bash -c "..." with double quotes causes backslash escaping issues
			// even after path conversion, because users may have other backslash
			// paths. Writing to a script file avoids all escaping problems.
			scriptFile, err := os.CreateTemp("", "skill-step-*.sh")
			if err != nil {
				return "", fmt.Errorf("创建临时脚本文件失败: %v", err)
			}
			tmpScript = scriptFile.Name()
			scriptContent := "#!/bin/bash\n" + bashCommand + "\n"
			if _, err := scriptFile.WriteString(scriptContent); err != nil {
				scriptFile.Close()
				os.Remove(tmpScript)
				return "", fmt.Errorf("写入临时脚本文件失败: %v", err)
			}
			scriptFile.Close()
			// Use script file path (convert to forward slashes for bash)
			shellArgs = []string{filepath.ToSlash(tmpScript)}
			log.Printf("[skill-runner] bash step: using temp script %s", tmpScript)
		} else {
			// Direct command — use cmd.exe for better subprocess nesting support.
			// cmd.exe handles backslash paths natively, no conversion needed.
			cmdPath := os.Getenv("ComSpec")
			if cmdPath == "" {
				cmdPath = "C:\\WINDOWS\\system32\\cmd.exe"
				if _, err := os.Stat(cmdPath); err != nil {
					cmdPath = "cmd.exe"
				}
			}
			shellName = cmdPath
			shellArgs = []string{"/c", command}
		}
	} else {
		shellName = "bash"
		shellArgs = []string{"-c", command}
	}
	if tmpScript != "" {
		defer os.Remove(tmpScript)
	}

	cmd := exec.CommandContext(stepCtx, shellName, shellArgs...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	hideCommandWindow(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	err := cmd.Run()
	elapsed := time.Since(startTime)

	isTimeout := stepCtx.Err() == context.DeadlineExceeded

	var b strings.Builder
	b.WriteString(fmt.Sprintf("🐚 shell: %s\n", filepath.Base(shellName)))
	b.WriteString(fmt.Sprintf("⏱  %s\n", elapsed.Round(time.Millisecond)))
	b.WriteString(fmt.Sprintf("📂 %s\n", workDir))
	if tmpScript != "" {
		// Show original command instead of temp script path for readability
		b.WriteString(fmt.Sprintf("💻 %s (via script)\n", command))
	} else {
		b.WriteString(fmt.Sprintf("💻 %s %s\n", filepath.Base(shellName), strings.Join(shellArgs, " ")))
	}
	b.WriteString("───────────────\n")
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
		if isTimeout {
			b.WriteString(fmt.Sprintf("\n[error] timeout after %ds", timeout))
		} else {
			b.WriteString(fmt.Sprintf("\n[error] %v", err))
		}
		// 构建包含 stderr 的错误消息，方便上层排查
		errMsg := err.Error()
		stderrText := strings.TrimSpace(stderr.String())
		if stderrText != "" {
			if len(stderrText) > 2048 {
				stderrText = stderrText[:2048] + "..."
			}
			errMsg = fmt.Sprintf("%s | stderr: %s", errMsg, stderrText)
		}
		stdoutText := strings.TrimSpace(stdout.String())
		if stdoutText != "" && stderrText == "" {
			if len(stdoutText) > 2048 {
				stdoutText = stdoutText[:2048] + "..."
			}
			errMsg = fmt.Sprintf("%s | stdout: %s", errMsg, stdoutText)
		}
		return b.String(), &bashStepError{
			message:   errMsg,
			exitCode:  extractExitCode(err),
			isTimeout: isTimeout,
			stdout:    stdout.String(),
			stderr:    stderr.String(),
		}
	}
	if b.Len() == 0 {
		return fmt.Sprintf("(completed, no output, %s)", elapsed.Round(time.Millisecond)), nil
	}
	return b.String(), nil
}

// bashStepError wraps a bash execution error with exit code and output.
type bashStepError struct {
	message   string
	exitCode  int
	isTimeout bool
	stdout    string
	stderr    string
}

func (e *bashStepError) Error() string {
	if e.isTimeout {
		return fmt.Sprintf("timeout: %s (exit code: %d)", e.message, e.exitCode)
	}
	return fmt.Sprintf("%s (exit code: %d)", e.message, e.exitCode)
}

func (e *bashStepError) ExitCode() int    { return e.exitCode }
func (e *bashStepError) IsTimeout() bool  { return e.isTimeout }
func (e *bashStepError) Stdout() string   { return e.stdout }
func (e *bashStepError) Stderr() string   { return e.stderr }

// extractExitCode extracts the exit code from an error if possible.
func extractExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return 1
}

// lastNLines returns the last n non-empty lines of text.
func lastNLines(text string, n int) []string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) <= n {
		return lines
	}
	return lines[len(lines)-n:]
}

// needsBashShell checks whether a command contains shell-specific
// features that require bash/sh instead of cmd.exe on Windows.
// Default: prefer cmd.exe for better subprocess nesting support
// (e.g. node execFileSync calls to powershell).
func needsBashShell(command string) bool {
	// If command starts with known interpreters that run fine under cmd.exe,
	// prefer cmd.exe for better subprocess nesting.
	lower := strings.TrimSpace(strings.ToLower(command))
	for _, prefix := range []string{"node ", "python ", "python3 ", "java ", "npm ", "pip ", "npx ", "go run ", "cargo run ", "pnpm "} {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}
	// Direct script path invocation — cmd.exe handles this well.
	if strings.Contains(lower, ".mjs") || strings.Contains(lower, ".js") ||
		strings.Contains(lower, ".py") || strings.Contains(lower, ".bat") ||
		strings.Contains(lower, ".cmd") {
		return false
	}
	// Only use bash for genuine bash-specific syntax.
	// Pipes, redirections, heredocs.
	if strings.ContainsAny(command, "|<>") {
		return true
	}
	if strings.Contains(command, "&&") || strings.Contains(command, "||") {
		return true
	}
	// Command substitution.
	if strings.Contains(command, "$(") || strings.Contains(command, "`") {
		return true
	}
	// Globbing with path separators.
	if strings.Contains(command, "*/") || strings.Contains(command, "/*") {
		return true
	}
	// Default: prefer cmd.exe for Windows subprocess compatibility.
	return false
}

// winPathInCommandRe matches Windows absolute paths (e.g. C:\Users\...)
// in command strings. Used to convert backslashes to forward slashes for bash.
var winPathInCommandRe = regexp.MustCompile(`([A-Za-z]):\\([^\s"'` + "`" + `<>|*?]+)`)

// winPathInQuotesRe matches Windows absolute paths inside double quotes
// (e.g. "C:\Program Files\Git\bin\bash.exe") where spaces are allowed.
var winPathInQuotesRe = regexp.MustCompile(`"([A-Za-z]):\\([^"]+)"`)

// convertWindowsPathsInCommand converts Windows backslash paths to forward
// slashes in a command string. This is critical for bash execution on Windows
// where backslashes are interpreted as escape characters.
func convertWindowsPathsInCommand(command string) string {
	if !strings.Contains(command, `\`) {
		return command
	}
	// First pass: handle quoted paths (may contain spaces)
	result := winPathInQuotesRe.ReplaceAllStringFunc(command, func(match string) string {
		return strings.ReplaceAll(match, `\`, `/`)
	})
	// Second pass: handle unquoted paths
	result = winPathInCommandRe.ReplaceAllStringFunc(result, func(match string) string {
		return strings.ReplaceAll(match, `\`, `/`)
	})
	return result
}

// resolveCommandForDisplay returns the command string from a resolved step for display purposes.
func resolveCommandForDisplay(step NLSkillStep) string {
	cmd, _ := step.Params["command"].(string)
	return cmd
}

// ── 平台兼容性检查 ──────────────────────────────────────────────────────

// checkPlatformCompat 检查当前平台是否匹配 skill 的 platforms 声明。
// platforms 为空视为 universal（兼容所有平台）。
func checkPlatformCompat(skill *NLSkillEntry) error {
	if len(skill.Platforms) == 0 {
		return nil // universal
	}

	currentOS := runtime.GOOS // "windows", "linux", "darwin"
	// 标准化：darwin -> macos
	platformName := currentOS
	if platformName == "darwin" {
		platformName = "macos"
	}

	matched := false
	for _, p := range skill.Platforms {
		if strings.EqualFold(strings.TrimSpace(p), platformName) {
			matched = true
			break
		}
		if strings.EqualFold(strings.TrimSpace(p), "universal") {
			matched = true
			break
		}
	}
	if !matched {
		return fmt.Errorf("skill %q 不支持当前平台 %s（支持: %s）",
			skill.Name, platformName, strings.Join(skill.Platforms, ", "))
	}

	// Linux 下检查 GUI 环境需求
	if currentOS == "linux" && skill.RequiresGUI {
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return fmt.Errorf("skill %q 需要 GUI 环境，但当前 Linux 未检测到 DISPLAY 或 WAYLAND_DISPLAY",
				skill.Name)
		}
	}

	return nil
}

// ── 文件引用存在性检查 ──────────────────────────────────────────────────

// checkFileReferences 检查 skill 中 bash step 引用的文件/命令是否存在。
// 对于有 skillDir 的 skill，相对路径基于 skillDir 解析。
func checkFileReferences(skill *NLSkillEntry) error {
	for i, step := range skill.Steps {
		if step.Action != "bash" {
			continue
		}
		command, _ := step.Params["command"].(string)
		if command == "" || strings.Contains(command, "{{") || strings.Contains(command, "${") {
			continue
		}

		// 检查命令中是否引用了绝对路径的文件
		refs := extractFileReferences(command)
		for _, ref := range refs {
			var fullPath string
			if filepath.IsAbs(ref) {
				fullPath = ref
			} else if skill.SkillDir != "" {
				fullPath = filepath.Join(skill.SkillDir, ref)
			} else {
				continue // 无法解析相对路径，跳过检查
			}
			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				return fmt.Errorf("skill %q step %d 引用的文件不存在: %s",
					skill.Name, i+1, fullPath)
			}
		}
	}
	return nil
}

// extractFileReferences 从 bash 命令中提取可能的文件路径引用。
// 识别模式：以 / 或 ./ 或 ../ 开头的路径，以及 .sh/.py/.js/.bat/.ps1 结尾的文件名。
func extractFileReferences(command string) []string {
	var refs []string
	seen := make(map[string]bool)

	fields := strings.Fields(command)
	for _, f := range fields {
		// 去掉常见的 shell 引号
		f = strings.Trim(f, "'\"")

		isPath := false
		// 绝对路径
		if filepath.IsAbs(f) {
			isPath = true
		}
		// 相对路径
		if strings.HasPrefix(f, "./") || strings.HasPrefix(f, "../") {
			isPath = true
		}
		// 脚本文件扩展名
		for _, ext := range []string{".sh", ".py", ".js", ".bat", ".ps1", ".rb", ".pl"} {
			if strings.HasSuffix(f, ext) {
				isPath = true
				break
			}
		}

		if isPath && !seen[f] {
			refs = append(refs, f)
			seen[f] = true
		}
	}
	return refs
}
