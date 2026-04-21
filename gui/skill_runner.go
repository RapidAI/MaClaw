package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
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
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
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
	RunID           string               `json:"run_id"`
	Skill           string               `json:"skill"`
	Status          string               `json:"status"` // "running", "success", "failed", "cancelled"
	Steps           []StepResult         `json:"steps"`
	Session         *SkillRunSessionMeta `json:"session,omitempty"`
	SessionProgress *SessionProgressInfo `json:"session_progress,omitempty"`
	Summary         SkillRunSummary      `json:"summary,omitempty"`
	ExpectedOutput  string               `json:"expected_output,omitempty"`
	StartedAt       string               `json:"started_at"`
	EndedAt         string               `json:"ended_at,omitempty"`
	Error           string               `json:"error,omitempty"`
	DurationMs      int64                `json:"duration_ms,omitempty"`
	TotalSteps      int                  `json:"total_steps,omitempty"`
	FailedSteps     int                  `json:"failed_steps,omitempty"`
	SkippedSteps    int                  `json:"skipped_steps,omitempty"`
}

// SessionProgressInfo captures the latest state from the session's internal
// AI agent, polled in the background by the SkillRunner. This gives callers
// visibility into what the session agent is doing without needing to call
// query_session separately.
type SessionProgressInfo struct {
	SessionStatus   string   `json:"session_status"`             // "starting", "running", "busy", "completed", "failed"
	CurrentTask     string   `json:"current_task,omitempty"`     // what the session agent is currently doing
	ProgressSummary string   `json:"progress_summary,omitempty"` // human-readable progress
	LastResult      string   `json:"last_result,omitempty"`      // last tool call result or output
	LastCommand     string   `json:"last_command,omitempty"`     // last command executed
	WaitingForUser  bool     `json:"waiting_for_user,omitempty"` // session agent is waiting for input
	LastOutputLines []string `json:"last_output_lines,omitempty"` // last N raw output lines (max 10)
	UpdatedAt       string   `json:"updated_at,omitempty"`       // when this snapshot was taken
	PollCount       int      `json:"poll_count,omitempty"`       // how many times we've polled
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
	status        SkillRunStatus
	cancel        context.CancelFunc
	monitorCancel context.CancelFunc  // cancels the session monitor goroutine
	templateVars  map[string]string
	selectedSteps []string            // api_workflow mode: only run steps with these labels
	extraEnv      map[string]string   // env vars from run_skill caller, injected into subprocesses
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
	// 查找 skill — match by name regardless of status so we can provide
	// specific error messages for disabled/needs_setup skills (Bug #3).
	r.executor.mu.RLock()
	var target *NLSkillEntry
	var collisions []NLSkillEntry // track bare name collisions across publishers
	isQualified := strings.Contains(skillName, ":")
	for _, s := range r.executor.loadSkills() {
		if s.MatchesName(skillName) {
			if isQualified {
				// Qualified name: exact match, no collision possible.
				cp := s
				target = &cp
				break
			}
			// Bare name: collect all matches to detect collisions.
			collisions = append(collisions, s)
		}
	}
	// For bare name queries, resolve collisions.
	if !isQualified {
		if len(collisions) == 1 {
			cp := collisions[0]
			target = &cp
		} else if len(collisions) > 1 {
			// Multiple skills match the bare name — require qualified name.
			var qualifiedNames []string
			for _, s := range collisions {
				if s.Publisher != "" {
					qualifiedNames = append(qualifiedNames, s.Publisher+":"+s.Name)
				} else {
					qualifiedNames = append(qualifiedNames, s.Name+" (local)")
				}
			}
			r.executor.mu.RUnlock()
			return "", fmt.Errorf("skill name %q is ambiguous — multiple skills match:\n  %s\nPlease use the qualified name (publisher:name) to disambiguate",
				skillName, strings.Join(qualifiedNames, "\n  "))
		}
	}
	r.executor.mu.RUnlock()

	if target == nil {
		// Fuzzy match fallback: suggest the closest skill when exact match fails.
		if similar, score := cskill.FindSimilarSkill(skillName, 0.3); similar != nil {
			return "", fmt.Errorf("skill %q not found. Did you mean %q? (%.0f%% match)\nUse list_skills to see installed skills",
				skillName, similar.Name, score*100)
		}
		return "", fmt.Errorf("skill %q not found. Use list_skills to see installed skills", skillName)
	}

	// Bug #3: Distinguish needs_setup / disabled from active
	if target.Status == "needs_setup" {
		return "", fmt.Errorf("skill %q needs setup. Installation was incomplete (missing dependencies or files). Please check the skill directory (%s) and complete configuration", skillName, target.SkillDir)
	}
	if target.Status == "disabled" {
		return "", fmt.Errorf("skill %q is disabled. Please enable it first", skillName)
	}
	if target.Status != "active" && target.Status != "" {
		return "", fmt.Errorf("skill %q status is %q, expected active", skillName, target.Status)
	}

	// BUG-005: Normalize skill directory path (resolve 8.3 short paths on Windows)
	if runtime.GOOS == "windows" && target.SkillDir != "" {
		target.SkillDir = normalizeWindowsShortPathGUI(target.SkillDir)
	}

	// Migrate legacy .cceasy paths to .maclaw — crafted skills from older
	// versions may reference scripts in the old directory structure.
	migrateLegacyCceasyPaths(target)

	// 平台检查
	if err := checkPlatformCompat(target); err != nil {
		return "", err
	}

	// 文件存在性检查（bash step 中引用的文件）
	if err := checkFileReferences(target); err != nil {
		return "", err
	}

	// ── Credential file pre-check: validate required credential files exist locally ──
	if len(target.RequiredCredentialFiles) > 0 {
		missing := remote.ValidateCredentialFiles(target.RequiredCredentialFiles)
		if len(missing) > 0 {
			log.Printf("[skill-runner] credential pre-check: %d missing credential file(s)", len(missing))
			return "", fmt.Errorf("skill %q needs setup: missing credential file(s): %s. Please create the required credential files before running this skill",
				skillName, strings.Join(missing, ", "))
		}
	}

	if len(target.Steps) == 0 {
		// Documentation-only skill fallback: if the skill has a SKILL.md with
		// documentation content but no executable steps, synthesize a single
		// craft_tool step that uses the documentation as instructions for the
		// LLM to generate and execute a script. This enables pure-documentation
		// skills (like tts-to-mp3) to be executed automatically.
		docContent := loadSkillDocContent(target.SkillDir)
		if docContent != "" {
			log.Printf("[skill-runner] skill %q has no steps but has documentation (%d chars), creating craft_tool fallback", skillName, len(docContent))
			// Truncate very long documentation to avoid overwhelming the LLM.
			if len(docContent) > 8000 {
				docContent = docContent[:8000] + "\n\n... (truncated)"
			}
			// Build task description from user context + documentation.
			var userContext string
			if userPrompt, ok := runArgs["user_prompt"]; ok && fmt.Sprintf("%v", userPrompt) != "" {
				userContext = fmt.Sprintf("%v", userPrompt)
			} else if input, ok := runArgs["input"]; ok && fmt.Sprintf("%v", input) != "" {
				userContext = fmt.Sprintf("%v", input)
			}
			task := docContent
			if userContext != "" {
				task = fmt.Sprintf("用户请求: %s\n\n按照以下文档指引执行:\n%s", userContext, docContent)
			}
			target.Steps = []corelib.NLSkillStep{{
				Action:  "craft_tool",
				Params:  map[string]interface{}{"task": task},
				OnError: "stop",
			}}
		} else {
			// Bug #5: Better error for skills with no executable steps
			msg := fmt.Sprintf("skill %q has no executable steps", skillName)
			if len(target.RequiredArgs) > 0 {
				msg += fmt.Sprintf(". This skill requires parameters: %s", strings.Join(target.RequiredArgs, ", "))
			}
			desc := strings.TrimSpace(target.Description)
			if desc != "" {
				if len(desc) > 150 {
					desc = desc[:150] + "..."
				}
				msg += fmt.Sprintf("\nDescription: %s", desc)
			}
			return "", fmt.Errorf("%s", msg)
		}
	}

	templateVars := normalizeSkillRunVars(runArgs)

	// ── api_workflow mode: extract step selector from runArgs ──
	var selectedSteps []string
	if strings.EqualFold(target.Mode, "api_workflow") {
		if stepsArg, ok := runArgs["steps"]; ok {
			switch v := stepsArg.(type) {
			case string:
				for _, s := range strings.Split(v, ",") {
					if t := strings.TrimSpace(s); t != "" {
						selectedSteps = append(selectedSteps, t)
					}
				}
			case []interface{}:
				for _, item := range v {
					if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
						selectedSteps = append(selectedSteps, strings.TrimSpace(s))
					}
				}
			case []string:
				selectedSteps = v
			}
		}
		// Operation-based routing: resolve operation name to step labels.
		// This takes precedence over explicit step selection when both are provided.
		if opName, ok := runArgs["operation"].(string); ok && strings.TrimSpace(opName) != "" {
			opName = strings.TrimSpace(opName)
			for _, op := range target.Operations {
				if strings.EqualFold(op.Name, opName) {
					selectedSteps = op.Labels
					log.Printf("[skill-runner] operation %q resolved to labels: %v", opName, selectedSteps)
					break
				}
			}
		}
	}

	// ── Extract extra env vars from runArgs["env"] ──
	var extraEnv map[string]string
	if envRaw, ok := runArgs["env"].(map[string]interface{}); ok && len(envRaw) > 0 {
		extraEnv = make(map[string]string, len(envRaw))
		for k, v := range envRaw {
			if s, ok := v.(string); ok {
				extraEnv[k] = s
			}
		}
	}

	// ── P0: Validate required arguments before execution ──
	if len(target.RequiredArgs) > 0 {
		var missing []string
		for _, arg := range target.RequiredArgs {
			if templateVars == nil || strings.TrimSpace(templateVars[arg]) == "" {
				missing = append(missing, arg)
			}
		}
		if len(missing) > 0 {
			return "", fmt.Errorf("skill %q 缺少必需参数: %s", skillName, strings.Join(missing, ", "))
		}
	}

	// ── Implicit required args: detect {{key}} placeholders in step commands
	// that aren't provided via templateVars. This catches skills like
	// xh-md-to-pdf that use {{input}}/{{output}} without declaring required_args.
	if len(target.RequiredArgs) == 0 {
		implicit := detectImplicitRequiredArgs(target.Steps, templateVars)
		if len(implicit) > 0 {
			desc := strings.TrimSpace(target.Description)
			if len(desc) > 120 {
				desc = desc[:120] + "..."
			}
			msg := fmt.Sprintf("skill %q 的命令中包含未提供的参数: %s。请在 args 中传入，例如: args={%s}", skillName, strings.Join(implicit, ", "), buildArgsExample(implicit))
			if desc != "" {
				msg += fmt.Sprintf("\n说明: %s", desc)
			}
			return "", fmt.Errorf("%s", msg)
		}
	}

	// ── P2: Validate required environment variables before execution ──
	if len(target.RequiredEnv) > 0 {
		var missing []string
		for _, env := range target.RequiredEnv {
			// Skip OPENAI_API_KEY — the proxy will provide it if needed.
			if env == "OPENAI_API_KEY" {
				continue
			}
			if strings.TrimSpace(os.Getenv(env)) == "" {
				missing = append(missing, env)
			}
		}
		if len(missing) > 0 {
			return "", fmt.Errorf("skill %q 缺少必需的环境变量: %s", skillName, strings.Join(missing, ", "))
		}
	}

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
		cancel:        cancel,
		templateVars:  templateVars,
		selectedSteps: selectedSteps,
		extraEnv:      extraEnv,
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
	// Deep copy SessionProgress to avoid the monitor goroutine mutating
	// the returned snapshot's LastOutputLines slice.
	if run.status.SessionProgress != nil {
		spCopy := *run.status.SessionProgress
		if len(run.status.SessionProgress.LastOutputLines) > 0 {
			spCopy.LastOutputLines = make([]string, len(run.status.SessionProgress.LastOutputLines))
			copy(spCopy.LastOutputLines, run.status.SessionProgress.LastOutputLines)
		}
		cp.SessionProgress = &spCopy
	}
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
	if run.monitorCancel != nil {
		run.monitorCancel()
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
	run, ok := r.runs[runID]
	if !ok {
		r.mu.Unlock()
		return
	}
	metaCopy := meta
	run.status.Session = &metaCopy

	// Start session monitor if session_id is available and monitor not yet started.
	// Check and set under the same lock hold to prevent double-start.
	sessionID := strings.TrimSpace(meta.SessionID)
	needsMonitor := sessionID != "" && run.monitorCancel == nil
	if needsMonitor {
		monitorCtx, monitorCancel := context.WithCancel(context.Background())
		run.monitorCancel = monitorCancel
		r.mu.Unlock()
		r.startSessionMonitor(monitorCtx, run, sessionID)
	} else {
		r.mu.Unlock()
	}
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

// skillRunVarFallbackKeys re-exports the shared list from corelib/skill
// so that normalizeSkillRunVars stays in sync with the TUI.
var skillRunVarFallbackKeys = cskill.RunVarFallbackKeys

func normalizeSkillRunVars(runArgs map[string]interface{}) map[string]string {
	vars := map[string]string{}
	if rawArgs, ok := runArgs["args"].(map[string]interface{}); ok {
		for key, value := range rawArgs {
			if str, ok := value.(string); ok {
				vars[key] = str
			} else if value != nil {
				vars[key] = fmt.Sprintf("%v", value)
			}
		}
	}

	// tryParseJSONIntoVars: when args is empty and a string value looks like
	// a JSON object (e.g. "{\"city\":\"新加坡\"}"), parse it and merge into
	// vars so that {{key}} placeholders get resolved. This handles the common
	// case where the LLM puts structured params in input/output instead of args.
	tryParseJSONIntoVars := func(s string) {
		if len(vars) == 0 && len(s) > 2 && s[0] == '{' {
			var parsed map[string]interface{}
			if json.Unmarshal([]byte(s), &parsed) == nil && len(parsed) > 0 {
				for k, v := range parsed {
					if str, ok := v.(string); ok {
						vars[k] = str
					}
				}
			}
		}
	}

	for _, key := range skillRunVarFallbackKeys {
		if _, exists := vars[key]; exists {
			continue
		}
		if v, ok := runArgs[key].(string); ok && v != "" {
			// JSON parsing fallback only for input/output — same as TUI.
			if key == "input" || key == "output" {
				tryParseJSONIntoVars(v)
			}
			vars[key] = v
		}
	}
	if len(vars) == 0 {
		return nil
	}
	return vars
}

// detectImplicitRequiredArgs scans step commands for {{key}} placeholders
// that are not provided in templateVars. Returns the list of missing keys.
// This catches skills that use {{input}}/{{output}} without declaring
// required_args in their frontmatter.
func detectImplicitRequiredArgs(steps []NLSkillStep, vars map[string]string) []string {
	seen := make(map[string]bool)
	for _, step := range steps {
		if step.Action != "bash" {
			continue
		}
		cmd, _ := step.Params["command"].(string)
		if cmd == "" {
			continue
		}
		for _, m := range unresolvedSkillPlaceholderPattern.FindAllString(cmd, -1) {
			// Extract key from {{key}}, ${key}, or {key}
			key := strings.TrimPrefix(m, "{{")
			key = strings.TrimPrefix(key, "${")
			key = strings.TrimPrefix(key, "{")
			key = strings.TrimSuffix(key, "}}")
			key = strings.TrimSuffix(key, "}")
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			if vars != nil && strings.TrimSpace(vars[key]) != "" {
				continue // provided
			}
			if !seen[key] {
				seen[key] = true
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	result := make([]string, 0, len(seen))
	for key := range seen {
		result = append(result, key)
	}
	slices.Sort(result)
	return result
}

// buildArgsExample generates a JSON-like example string for missing args,
// e.g. `"city": "<city 值>"` — helps the LLM understand the expected format.
func buildArgsExample(keys []string) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%q: \"<%s 值>\"", k, k)
	}
	return strings.Join(parts, ", ")
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
			// Order matters: replace {{key}} and ${key} first, then {key} last.
			// If {key} were replaced first, it would partially consume {{key}}
			// leaving stray braces.
			for _, placeholder := range []string{"{{" + key + "}}", "${" + key + "}", "{" + key + "}"} {
				// Replace quoted-placeholder patterns first (e.g. "{{text}}" → value),
				// then replace any remaining bare placeholders ({{text}} → value).
				// This avoids double-quoting when SKILL.md authors wrap placeholders
				// in quotes that quoteSkillInputForShell would also add.
				doubleQuoted := `"` + placeholder + `"`
				singleQuoted := `'` + placeholder + `'`
				command = strings.ReplaceAll(command, doubleQuoted, value)
				command = strings.ReplaceAll(command, singleQuoted, value)
				command = strings.ReplaceAll(command, placeholder, value)
			}
		}
	}
	// Log any remaining unresolved placeholders as warnings before stripping.
	remaining := unresolvedSkillPlaceholderPattern.FindAllString(command, -1)
	if len(remaining) > 0 {
		log.Printf("[skill-runner] ⚠ unresolved placeholders (will be stripped): %v", remaining)
	}
	result := stripUnresolvedSkillPlaceholders(command)
	// 记录变量替换结果，方便排查跨平台路径问题
	if result != original {
		log.Printf("[skill-runner] variable substitution: %q → %q", original, result)
	}
	return result
}

var unresolvedSkillPlaceholderPattern = regexp.MustCompile(`\{\{[^{}]+\}\}|\$\{[^{}]+\}|\{[a-zA-Z_][a-zA-Z0-9_]*\}`)

func stripUnresolvedSkillPlaceholders(text string) string {
	if text == "" {
		return text
	}
	return unresolvedSkillPlaceholderPattern.ReplaceAllString(text, "")
}

// quoteSkillInputForShell wraps a user-supplied value for safe embedding
// in a shell command string.
//
// On Windows the skill runner dispatches simple commands (node, python, …)
// through cmd.exe, which does NOT recognise single-quotes as delimiters.
// Using single-quotes caused the xh-md-to-pdf path-concatenation bug where
// the trailing backslash of {baseDir} merged with the opening single-quote.
// We therefore use double-quotes on Windows and single-quotes elsewhere.
func quoteSkillInputForShell(input string) string {
	if input == "" {
		if runtime.GOOS == "windows" {
			return `""`
		}
		return "''"
	}
	if runtime.GOOS == "windows" {
		// Double-quote for cmd.exe compatibility.
		// Escape existing double-quotes with backslash (understood by C runtime
		// argument parsing used by node, python, etc.).
		// Also escape percent signs which cmd.exe interprets as variable expansion
		// in .cmd batch files.
		escaped := strings.ReplaceAll(input, `"`, `\"`)
		escaped = strings.ReplaceAll(escaped, `%`, `%%`)
		return `"` + escaped + `"`
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

// mapKeys returns the keys of a string map for diagnostic logging.
func mapKeys(m map[string]string) []string {
	if m == nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// truncateEnvForLog masks an environment variable value for safe logging.
// Shows first 6 chars + "..." for non-empty values.
func truncateEnvForLog(v string) string {
	if v == "" {
		return "(empty)"
	}
	if len(v) <= 6 {
		return v
	}
	return v[:6] + "..."
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
	execStart := time.Now()
	// Global timeout: use skill-level setting if available, otherwise 5 minutes.
	globalTimeout := 5 * time.Minute
	if skill.GlobalTimeout > 0 {
		globalTimeout = time.Duration(skill.GlobalTimeout) * time.Second
		log.Printf("[skill-runner] using skill-level global timeout: %v", globalTimeout)
	}
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
	isAPIWorkflow := strings.EqualFold(skill.Mode, "api_workflow")

	// ── Credential mounting for SSH execution ──
	// If the skill declares required_credential_files and an SSH session manager
	// is available, mount credentials to the remote host before step execution.
	var credentialCleanup func()
	if len(skill.RequiredCredentialFiles) > 0 {
		sshMgr := r.executor.ensureSSHManager()
		if sshMgr != nil {
			mounter := remote.NewCredentialMounter(sshMgr)
			// Use the run ID as a pseudo session identifier for credential isolation.
			cleanup, mountErr := mounter.MountCredentials(run.status.RunID, skill.RequiredCredentialFiles)
			if mountErr != nil {
				log.Printf("[skill-runner] credential mount failed: %v", mountErr)
				// Non-fatal: log warning but continue execution.
				// The skill may still work if credentials are not needed for this run.
			} else {
				credentialCleanup = cleanup
				log.Printf("[skill-runner] credentials mounted for run %s (%d files)", run.status.RunID, len(skill.RequiredCredentialFiles))
			}
		}
	}
	if credentialCleanup != nil {
		defer func() {
			credentialCleanup()
			log.Printf("[skill-runner] credentials cleaned up for run %s", run.status.RunID)
		}()
	}

	// ── OpenAI Proxy for skills requiring OPENAI_API_KEY ──
	// If the skill declares OPENAI_API_KEY in required_env, or if the skill's
	// scripts reference OPENAI_API_KEY/OPENAI_BASE_URL (auto-detection for
	// ClawHub skills that don't declare requires_env), and the user hasn't
	// provided them via extra_env, start a local proxy that forwards requests
	// to the currently configured LLM provider.
	needsProxy := corelib.NeedsOpenAIProxyAuto(skill.RequiredEnv, run.extraEnv, skill.Steps, skill.SkillDir)
	log.Printf("[skill-runner] openai proxy check: needsProxy=%v required_env=%v extraEnv_keys=%v processEnv_OPENAI_API_KEY=%q",
		needsProxy, skill.RequiredEnv, mapKeys(run.extraEnv), truncateEnvForLog(os.Getenv("OPENAI_API_KEY")))
	if needsProxy {
		// Build config from current LLM provider
		var proxyCfg corelib.OpenAIProxyConfig
		if r.executor != nil && r.executor.app != nil {
			llmCfg := r.executor.app.GetMaclawLLMConfig()
			proxyCfg = corelib.OpenAIProxyConfig{
				URL:      llmCfg.URL,
				Key:      llmCfg.Key,
				Model:    llmCfg.Model,
				Protocol: llmCfg.Protocol,
				WireAPI:  llmCfg.WireAPI,
			}
		}

		proxy := corelib.NewOpenAIProxy(proxyCfg)
		port, proxyErr := proxy.Start()
		if proxyErr != nil {
			log.Printf("[skill-runner] openai proxy start failed: %v (continuing without proxy)", proxyErr)
		} else {
			defer proxy.Stop()
			// Inject environment variables for the skill
			if run.extraEnv == nil {
				run.extraEnv = make(map[string]string)
			}
			run.extraEnv["OPENAI_API_KEY"] = "sk-maclaw-local-proxy"
			run.extraEnv["OPENAI_BASE_URL"] = fmt.Sprintf("http://127.0.0.1:%d/v1", port)
			run.extraEnv["OPENAI_MODEL"] = proxyCfg.Model
			log.Printf("[skill-runner] openai proxy started on port %d for skill %q", port, skill.Name)
		}
	}

	// ── Dependency auto-install: install pip/npm packages before execution ──
	if len(skill.RequiresPython) > 0 || len(skill.RequiresNode) > 0 {
		if installErr := autoInstallSkillDependencies(skill); installErr != nil {
			log.Printf("[skill-runner] dependency install warning: %v", installErr)
			// Non-fatal: log warning but continue execution.
			// The skill may still work if the packages are already installed.
		}
	}

	log.Printf("[skill-runner] ▶ starting skill %q (%d steps, mode=%s, dir=%s)",
		skill.Name, len(skill.Steps), skill.Mode, skill.SkillDir)
	if len(skill.RequiredArgs) > 0 {
		log.Printf("[skill-runner]   required_args: %v", skill.RequiredArgs)
	}
	if len(skill.RequiredEnv) > 0 {
		log.Printf("[skill-runner]   required_env: %v", skill.RequiredEnv)
	}
	if skill.PreferredShell != "" {
		log.Printf("[skill-runner]   preferred_shell: %s", skill.PreferredShell)
	}
	if isAPIWorkflow && len(run.selectedSteps) > 0 {
		log.Printf("[skill-runner]   selected_steps: %v", run.selectedSteps)
	}
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

		// api_workflow mode: skip steps not in selectedSteps (by label)
		if isAPIWorkflow && len(run.selectedSteps) > 0 && step.Label != "" {
			if !stepLabelSelected(step.Label, run.selectedSteps) {
				r.mu.Lock()
				run.status.Steps[i].Status = "skipped"
				r.mu.Unlock()
				log.Printf("[skill-runner] step %d/%d: skipped (label %q not in selected steps)", i+1, len(skill.Steps), step.Label)
				continue
			}
		}
		// api_workflow mode: skip unlabeled steps when step selection is active
		if isAPIWorkflow && len(run.selectedSteps) > 0 && step.Label == "" {
			r.mu.Lock()
			run.status.Steps[i].Status = "skipped"
			r.mu.Unlock()
			log.Printf("[skill-runner] step %d/%d: skipped (no label, step selection active)", i+1, len(skill.Steps))
			continue
		}

		// Dynamic when condition: evaluate expression with template vars.
		// Allows steps to be conditionally executed based on runtime parameters.
		if step.When != "" {
			vars := r.templateVarsForRun(run.status.RunID)
			resolved := substituteSkillVarsInString(step.When, vars)
			if !evaluateSimpleCondition(resolved) {
				r.mu.Lock()
				run.status.Steps[i].Status = "skipped"
				r.mu.Unlock()
				log.Printf("[skill-runner] step %d/%d: skipped (when %q evaluated false)", i+1, len(skill.Steps), step.When)
				continue
			}
		}

		r.mu.Lock()
		run.status.Steps[i].Status = "running"
		r.mu.Unlock()

		resolvedStep := resolveSkillStep(step, r.templateVarsForRun(run.status.RunID), skill.SkillDir)
		// Propagate skill-level preferred_shell to bash steps so the shell
		// selection logic can respect it.
		if resolvedStep.Action == "bash" && skill.PreferredShell != "" {
			if resolvedStep.Params == nil {
				resolvedStep.Params = map[string]interface{}{}
			}
			if _, exists := resolvedStep.Params["preferred_shell"]; !exists {
				resolvedStep.Params["preferred_shell"] = skill.PreferredShell
			}
		}
		// Inject user_prompt into craft_tool steps so the LLM has context
		// about what the user wants. Without this, craft_tool generates
		// scripts blindly from just the skill description, often failing.
		if resolvedStep.Action == "craft_tool" {
			if resolvedStep.Params == nil {
				resolvedStep.Params = map[string]interface{}{}
			}
			if _, exists := resolvedStep.Params["user_prompt"]; !exists {
				vars := r.templateVarsForRun(run.status.RunID)
				if prompt := vars["user_prompt"]; prompt != "" {
					resolvedStep.Params["user_prompt"] = prompt
				}
			}
		}
		// Propagate skill-level required_env to bash steps for auto-injection.
		if resolvedStep.Action == "bash" && len(skill.RequiredEnv) > 0 {
			if resolvedStep.Params == nil {
				resolvedStep.Params = map[string]interface{}{}
			}
			envList := make([]interface{}, len(skill.RequiredEnv))
			for idx, e := range skill.RequiredEnv {
				envList[idx] = e
			}
			resolvedStep.Params["required_env"] = envList
		}
		// Propagate caller-supplied extra env vars to subprocess steps.
		// For bash steps, inject via params["extra_env"] (read by runBashStepWithContextFull).
		// For craft_tool and other steps, temporarily set os env vars so child
		// processes inherit them, then restore after step execution.
		if resolvedStep.Action == "bash" && len(run.extraEnv) > 0 {
			if resolvedStep.Params == nil {
				resolvedStep.Params = map[string]interface{}{}
			}
			extra := make(map[string]interface{}, len(run.extraEnv))
			for k, v := range run.extraEnv {
				extra[k] = v
			}
			resolvedStep.Params["extra_env"] = extra
		}
		// For non-bash steps (craft_tool, call_mcp_tool, etc.), use os.Setenv
		// so that any subprocess spawned during execution inherits the env vars.
		var envRestore []func()
		if resolvedStep.Action != "bash" && len(run.extraEnv) > 0 {
			for k, v := range run.extraEnv {
				prev, hadPrev := os.LookupEnv(k)
				os.Setenv(k, v)
				// Capture loop variables for the restore closure.
				capturedK, capturedPrev, capturedHad := k, prev, hadPrev
				if capturedHad {
					envRestore = append(envRestore, func() { os.Setenv(capturedK, capturedPrev) })
				} else {
					envRestore = append(envRestore, func() { os.Unsetenv(capturedK) })
				}
			}
		}
		log.Printf("[skill-runner] step %d/%d: action=%s command=%q", i+1, len(skill.Steps), resolvedStep.Action, resolveCommandForDisplay(resolvedStep))
		result, stepErr := r.executeStepWithPoll(globalCtx, run.status.RunID, resolvedStep, skill.SkillDir)
		// Restore env vars after non-bash step execution.
		for _, restore := range envRestore {
			restore()
		}

		r.mu.Lock()
		run.status.Steps[i].Name = resolvedStep.Name
		run.status.Steps[i].CommandResolved = resolveCommandForDisplay(resolvedStep)
		if stepErr != nil {
			run.status.Steps[i].Status = "failed"
			run.status.Steps[i].Error = stepErr.Error()
			run.status.Steps[i].Output = result
			log.Printf("[skill-runner] step %d/%d FAILED: %v", i+1, len(skill.Steps), stepErr)
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
			log.Printf("[skill-runner] step %d/%d OK (output %d bytes)", i+1, len(skill.Steps), len(result))
			// Output capture: extract variables from step output via regex
			if len(step.Capture) > 0 && result != "" {
				captured := captureOutputVariables(result, step.Capture)
				if len(captured) > 0 {
					if run.templateVars == nil {
						run.templateVars = make(map[string]string)
					}
					for k, v := range captured {
						run.templateVars[k] = v
						log.Printf("[skill-runner] captured %s=%q from step %d output", k, truncateSkillRunSnippet(v), i+1)
					}
				}
			}
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
	// Stop session monitor if running.
	if run.monitorCancel != nil {
		run.monitorCancel()
	}
	log.Printf("[skill-runner] ◼ skill %q finished: status=%s steps=%d elapsed=%s",
		skill.Name, run.status.Status, len(skill.Steps), time.Since(execStart).Truncate(time.Millisecond))
	r.mu.Unlock()

	// 更新 skill 使用统计
	r.updateUsageStats(skill, execErr)

	// 自动上传触发
	r.tryAutoUpload(skill, run)
}

func (r *SkillRunner) updateUsageStats(skill *NLSkillEntry, execErr error) {
	shouldEmit := false

	r.executor.mu.Lock()
	skills := r.executor.loadSkills()
	for i, s := range skills {
		if s.Name == skill.Name {
			skills[i].UsageCount++
			skills[i].LastUsedAt = time.Now().Format(time.RFC3339)
			if execErr == nil {
				skills[i].SuccessCount++
				skills[i].LastError = ""
			} else {
				skills[i].FailureCount++
				skills[i].LastError = execErr.Error()
			}
			_ = r.executor.saveSkills(skills)
			log.Printf("[skill-runner] usage stats updated for %q: usage=%d success=%d failure=%d workaround=%d",
				skill.Name, skills[i].UsageCount, skills[i].SuccessCount, skills[i].FailureCount, skills[i].WorkaroundCount)
			shouldEmit = true
			break
		}
	}
	r.executor.mu.Unlock()

	// Notify frontend to refresh skill list with updated stats (outside lock).
	if shouldEmit && r.executor.app != nil {
		r.executor.app.emitEvent("skill:usage_updated")
	}
}

// RecordSkillOutcome records an execution outcome for a skill by name.
// outcome must be one of "success", "failure", or "workaround".
//
// NOTE: This method is no longer called from the agent loop to avoid
// double-counting with updateUsageStats(). For workaround recording,
// use RecordWorkaround() instead. Retained for backward compatibility
// and potential external callers.
func (r *SkillRunner) RecordSkillOutcome(skillName, outcome, lastError string) {
	if skillName == "" {
		return
	}
	shouldEmit := false

	r.executor.mu.Lock()
	skills := r.executor.loadSkills()
	for i, s := range skills {
		if s.MatchesName(skillName) {
			switch outcome {
			case "success":
				skills[i].SuccessCount++
				skills[i].LastError = ""
			case "failure":
				skills[i].FailureCount++
				if lastError != "" {
					skills[i].LastError = lastError
				}
			case "workaround":
				skills[i].WorkaroundCount++
				if lastError != "" {
					skills[i].LastError = lastError
				}
			default:
				r.executor.mu.Unlock()
				return // unknown outcome, skip
			}
			skills[i].UsageCount++
			skills[i].LastUsedAt = time.Now().Format(time.RFC3339)
			_ = r.executor.saveSkills(skills)
			log.Printf("[skill-runner] outcome recorded for %q: outcome=%s usage=%d success=%d failure=%d workaround=%d",
				skillName, outcome, skills[i].UsageCount, skills[i].SuccessCount, skills[i].FailureCount, skills[i].WorkaroundCount)
			shouldEmit = true
			break
		}
	}
	r.executor.mu.Unlock()

	// Notify frontend to refresh skill list with updated stats (outside lock).
	if shouldEmit && r.executor.app != nil {
		r.executor.app.emitEvent("skill:usage_updated")
	}
}

// RecordWorkaround records a workaround outcome for a skill without
// incrementing UsageCount. This is called from the agent loop when a skill
// failed but the LLM resolved the task through alternative tools. The
// UsageCount and FailureCount were already incremented by updateUsageStats()
// when the skill execution completed, so we only need to bump WorkaroundCount.
func (r *SkillRunner) RecordWorkaround(skillName, lastError string) {
	if skillName == "" {
		return
	}
	shouldEmit := false

	r.executor.mu.Lock()
	skills := r.executor.loadSkills()
	for i, s := range skills {
		if s.MatchesName(skillName) {
			skills[i].WorkaroundCount++
			if lastError != "" {
				skills[i].LastError = lastError
			}
			_ = r.executor.saveSkills(skills)
			log.Printf("[skill-runner] workaround recorded for %q: workaround=%d (usage unchanged at %d)",
				skillName, skills[i].WorkaroundCount, skills[i].UsageCount)
			shouldEmit = true
			break
		}
	}
	r.executor.mu.Unlock()

	if shouldEmit && r.executor.app != nil {
		r.executor.app.emitEvent("skill:usage_updated")
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
		var args map[string]interface{}
		switch v := step.Params["arguments"].(type) {
		case map[string]interface{}:
			args = v
		case string:
			if trimmed := strings.TrimSpace(v); trimmed != "" {
				_ = json.Unmarshal([]byte(trimmed), &args)
			}
		}
		if args == nil {
			args = map[string]interface{}{}
		}
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
		// BUG-003: Execute craft_tool with context awareness so it respects
		// the global timeout and can be cancelled. We run it in a goroutine
		// and select on context cancellation.
		type craftResult struct {
			output string
			err    error
		}
		ch := make(chan craftResult, 1)
		go func() {
			out, err := executeCraftToolCore(r.executor.app, nil, step.Params, nil)
			ch <- craftResult{out, err}
		}()
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("craft_tool 步骤超时: %v", ctx.Err())
		case result := <-ch:
			return result.output, result.err
		}

	case "poll":
		return r.executePollStep(ctx, step, skillDir)

	default:
		return "", fmt.Errorf("unknown action: %s", step.Action)
	}
}

// ── bash step 执行（带 context + skillDir 作为默认 working_dir） ────────

func runBashStepWithContext(ctx context.Context, command string, params map[string]interface{}, skillDir string, app *App) (string, error) {
	return runBashStepWithContextFull(ctx, command, params, skillDir, app)
}

func runBashStepWithContextFull(ctx context.Context, command string, params map[string]interface{}, skillDir string, app *App) (string, error) {
	// Strip UTF-8 BOM if present — SKILL.md files saved with BOM can leak
	// the BOM bytes into the command string, causing cmd.exe to fail with
	// "'@echo' 不是内部或外部命令".
	command = strings.TrimPrefix(command, "\xef\xbb\xbf")

	timeout := 120 // default 120 seconds per step
	if t, ok := params["timeout"].(float64); ok && t > 0 {
		timeout = int(t)
		if timeout > 600 {
			timeout = 600
		}
	}
	// Allow skill-level global_timeout to raise the per-step cap.
	if gt, ok := params["global_timeout"].(float64); ok && gt > 0 && int(gt) > timeout {
		timeout = int(gt)
	}

	// [Fix] On Windows, map `python3` to `python` since Windows Python
	// installs typically only provide `python.exe`, not `python3.exe`.
	if runtime.GOOS == "windows" {
		command = mapPython3ToWindows(command)
	}

	// BUG-001: Normalize Windows 8.3 short paths to long paths
	if runtime.GOOS == "windows" {
		command = normalizePathsInCommandGUI(command)
	}

	workDir, _ := params["working_dir"].(string)
	// 如果没有指定 working_dir，使用 skill 目录
	if workDir == "" && skillDir != "" {
		workDir = skillDir
	}
	// BUG-001: Also normalize the working directory path
	if runtime.GOOS == "windows" && workDir != "" {
		workDir = normalizeWindowsShortPathGUI(workDir)
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
		// Only use sh.exe when the command contains shell-specific features
		// or when the skill explicitly requests bash via preferred_shell.
		useBash := needsBashShell(command)
		shellReason := "default (cmd.exe)"
		if useBash {
			shellReason = "detected Unix-specific syntax in command"
		}
		if !useBash {
			// Check if preferred shell is set via skill metadata (passed in params)
			if pref, _ := params["preferred_shell"].(string); strings.EqualFold(pref, "bash") {
				useBash = true
				shellReason = "skill metadata preferred_shell=bash"
			}
		}
		if useBash {
			if app != nil {
				if shPath, err := app.findSh(); err == nil {
					shellName = shPath
					log.Printf("[skill-runner] shell selection: %s → reason: %s", filepath.Base(shPath), shellReason)
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
			// Direct command — use cmd.exe with a temp .cmd script.
			// We must NOT pass the command as exec.Command args because Go's
			// syscall.EscapeArg escapes inner quotes with backslashes, turning
			// node "C:/path/script.mjs" into node \"C:/path/script.mjs\" which
			// causes cmd.exe to treat \" as literal characters in the path.
			// Using a temp .cmd file avoids all argument escaping issues.
			cmdPath := os.Getenv("ComSpec")
			if cmdPath == "" {
				cmdPath = "C:\\WINDOWS\\system32\\cmd.exe"
				if _, err := os.Stat(cmdPath); err != nil {
					cmdPath = "cmd.exe"
				}
			}
			shellName = cmdPath
			log.Printf("[skill-runner] shell selection: cmd.exe → reason: %s", shellReason)
			scriptFile, err := os.CreateTemp("", "skill-step-*.cmd")
			if err != nil {
				return "", fmt.Errorf("创建临时脚本文件失败: %v", err)
			}
			tmpScript = scriptFile.Name()
			// Strip # comment lines from the command before writing to .cmd script.
			// cmd.exe treats # as a command, not a comment, causing
			// "'#' is not recognized as an internal or external command" errors.
			cmdSafeCommand := cskill.StripBashCommentLines(command)
			// Write UTF-8 (no BOM) with `chcp 65001` to switch cmd.exe to
			// UTF-8 mode before executing the command. This ensures non-ASCII
			// paths (e.g. Chinese directory names like "脚本目录") are decoded
			// correctly instead of being garbled by the system codepage (GBK).
			//
			// BOM-based approach does NOT work: cmd.exe on CP936 treats the
			// BOM bytes as part of the first command, turning "@echo off" into
			// "'@echo' 不是内部或外部命令".
			scriptContent := "@echo off\r\nchcp 65001 >nul\r\n" + cmdSafeCommand + "\r\n"
			if _, err := scriptFile.WriteString(scriptContent); err != nil {
				scriptFile.Close()
				os.Remove(tmpScript)
				return "", fmt.Errorf("写入临时脚本文件失败: %v", err)
			}
			scriptFile.Close()
			shellArgs = []string{"/c", tmpScript}
			log.Printf("[skill-runner] bash step: using temp cmd script %s", tmpScript)
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
	// Force UTF-8 encoding for subprocess I/O on Windows to prevent
	// GBK/CP936 mojibake when scripts output non-ASCII text.
	cmd.Env = coretool.AppendUTF8Env(os.Environ())
	// Auto-inject required environment variables declared in skill metadata.
	// This replaces the need for `export VAR=value` in SKILL.md bash blocks.
	if envList, ok := params["required_env"].([]interface{}); ok {
		for _, item := range envList {
			if envName, ok := item.(string); ok && envName != "" {
				if val := os.Getenv(envName); val != "" {
					cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", envName, val))
				}
			}
		}
	}
	// Inject caller-supplied extra env vars (from run_skill env parameter).
	// These take precedence over process-level env vars, allowing the agent
	// to pass API keys etc. that were set in a previous bash tool call.
	if extraEnv, ok := params["extra_env"].(map[string]interface{}); ok {
		for k, v := range extraEnv {
			if s, ok := v.(string); ok && k != "" {
				cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, s))
			}
		}
	}
	hideCommandWindow(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	log.Printf("[skill-runner] bash exec: shell=%s workDir=%s timeout=%ds", filepath.Base(shellName), workDir, timeout)
	err := cmd.Run()
	elapsed := time.Since(startTime)

	// Sanitize invalid UTF-8 sequences (e.g. GBK remnants from cmd.exe on
	// Chinese Windows) so garbled replacement characters don't leak to the UI.
	sanitizeUTF8Buffer(&stdout)
	sanitizeUTF8Buffer(&stderr)

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
			message:   classifyBashError(errMsg, command, extractExitCode(err)),
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

// classifyBashError adds context to error messages by detecting common
// failure patterns like missing parameters, missing commands, etc.
func classifyBashError(errMsg, command string, exitCode int) string {
	combined := strings.ToLower(errMsg)
	// Detect "command not found" (exit code 9009 on Windows, 127 on Unix)
	if exitCode == 9009 || exitCode == 127 {
		cmdName := strings.Fields(strings.TrimSpace(command))
		if len(cmdName) > 0 {
			hint := fmt.Sprintf("命令 %q 未找到 (exit %d)。", cmdName[0], exitCode)
			cmdLower := strings.ToLower(cmdName[0])
			switch {
			case cmdLower == "python3" || cmdLower == "python":
				hint += " 请安装 Python 3.x 并确保在 PATH 中。Windows 用户请从 python.org 安装。"
			case cmdLower == "pip" || cmdLower == "pip3":
				hint += " 请运行: python -m ensurepip --upgrade"
			case cmdLower == "node" || cmdLower == "npm" || cmdLower == "npx":
				hint += " 请安装 Node.js: https://nodejs.org/"
			default:
				hint += " 请确认已安装并在 PATH 中。"
			}
			return hint + " | " + errMsg
		}
	}
	// BUG-002: Shebang treated as command in Windows CMD/PowerShell
	if (strings.Contains(combined, "'#'") || strings.Contains(combined, "\"#\"")) &&
		strings.Contains(combined, "not recognized") {
		return fmt.Sprintf("Bash 脚本的 shebang 行在 Windows CMD 中被当作命令执行。建议设置 preferred_shell: bash 或改用跨平台脚本。%s", errMsg)
	}
	// BUG-001: Windows 8.3 short path resolution failure
	if runtime.GOOS == "windows" && strings.Contains(combined, "~") &&
		(strings.Contains(combined, "enoent") || strings.Contains(combined, "no such file")) {
		return fmt.Sprintf("Windows 8.3 短路径解析失败，文件路径中的 '~' 缩写无法被识别。建议使用完整路径。%s", errMsg)
	}
	// Detect missing parameter patterns (usage text, "required", "missing argument")
	if exitCode == 1 || exitCode == 2 {
		if strings.Contains(combined, "usage:") || strings.Contains(combined, "usage：") ||
			strings.Contains(combined, "missing argument") || strings.Contains(combined, "required") ||
			strings.Contains(combined, "no input") || strings.Contains(combined, "缺少") {
			return fmt.Sprintf("Skill 可能缺少必需参数。%s", errMsg)
		}
	}
	// Detect missing environment variable (common patterns from various languages)
	if strings.Contains(combined, "environment variable") ||
		strings.Contains(combined, "env var") ||
		(strings.Contains(combined, "_key") && strings.Contains(combined, "not set")) ||
		(strings.Contains(combined, "_token") && strings.Contains(combined, "not set")) {
		return fmt.Sprintf("Skill 可能缺少必需的环境变量。%s", errMsg)
	}
	// P4: HTTP 429 rate limit detection
	if strings.Contains(combined, "429") && (strings.Contains(combined, "rate limit") || strings.Contains(combined, "too many requests") || strings.Contains(combined, "频率限制")) {
		return fmt.Sprintf("API 调用过于频繁 (HTTP 429)，请稍后再试。%s", errMsg)
	}
	// P3: File not found (ENOENT) — provide friendly hint
	if strings.Contains(combined, "enoent") || (strings.Contains(combined, "no such file") && strings.Contains(combined, "directory")) {
		return fmt.Sprintf("输入文件不存在，请检查文件路径是否正确。%s", errMsg)
	}
	// BUG-003: craft_tool hanging / timeout detection
	if strings.Contains(combined, "context deadline exceeded") || strings.Contains(combined, "signal: killed") {
		return fmt.Sprintf("步骤执行超时，可能是脚本挂起。建议增加 timeout 参数或检查脚本是否有阻塞操作。%s", errMsg)
	}
	return errMsg
}

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

// sanitizeUTF8Buffer replaces invalid UTF-8 sequences in a bytes.Buffer
// with empty strings. This handles GBK remnants from cmd.exe on Chinese
// Windows systems where the code page is not UTF-8.
func sanitizeUTF8Buffer(buf *bytes.Buffer) {
	if buf.Len() == 0 {
		return
	}
	raw := buf.String()
	if utf8.ValidString(raw) {
		return
	}
	buf.Reset()
	buf.WriteString(strings.ToValidUTF8(raw, ""))
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

	// Check for Unix shell builtins FIRST — these must use bash even if the
	// command also contains .py/.js paths (e.g. "export FOO=bar && python x.py").
	if strings.HasPrefix(lower, "export ") || strings.HasPrefix(lower, "source ") ||
		strings.HasPrefix(lower, "#!/") {
		log.Printf("[skill-runner] shell detection: found Unix shell builtin (export/source/shebang), needs bash")
		return true
	}
	// Multi-line commands containing export lines or # comment lines.
	// On Windows, cmd.exe treats # as a command, not a comment — so any
	// script with # comments must be routed to bash.
	for _, line := range strings.Split(command, "\n") {
		trimmed := strings.TrimSpace(strings.ToLower(line))
		if strings.HasPrefix(trimmed, "export ") {
			log.Printf("[skill-runner] shell detection: found export in multi-line command, needs bash")
			return true
		}
		if strings.HasPrefix(trimmed, "#") {
			log.Printf("[skill-runner] shell detection: found # comment line in command, needs bash")
			return true
		}
	}

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
		log.Printf("[skill-runner] shell detection: found pipe/redirect in command, needs bash")
		return true
	}
	if strings.Contains(command, "&&") || strings.Contains(command, "||") {
		log.Printf("[skill-runner] shell detection: found && or || in command, needs bash")
		return true
	}
	// Command substitution.
	if strings.Contains(command, "$(") || strings.Contains(command, "`") {
		log.Printf("[skill-runner] shell detection: found $() or backtick in command, needs bash")
		return true
	}
	// Globbing with path separators.
	if strings.Contains(command, "*/") || strings.Contains(command, "/*") {
		log.Printf("[skill-runner] shell detection: found glob pattern in command, needs bash")
		return true
	}
	// Tilde expansion (~/path).
	if strings.Contains(command, "~/") {
		log.Printf("[skill-runner] shell detection: found ~/ tilde expansion in command, needs bash")
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

// mapPython3ToWindows replaces `python3` with `python` in commands on Windows,
// since Windows Python installations typically only provide `python.exe`.
// Only replaces when `python3` appears as a command (not inside a path).
func mapPython3ToWindows(command string) string {
	if !python3NeedsMapping() {
		return command
	}
	lines := strings.Split(command, "\n")
	changed := false
	for i, line := range lines {
		ltrimmed := strings.TrimSpace(line)
		ll := strings.ToLower(ltrimmed)
		if strings.HasPrefix(ll, "python3 ") || ll == "python3" {
			lines[i] = strings.Replace(line, "python3", "python", 1)
			changed = true
		}
	}
	if changed {
		return strings.Join(lines, "\n")
	}
	return command
}

// python3NeedsMapping returns true if `python3` is not available but `python` is.
// Result is cached after first call to avoid repeated filesystem lookups.
// On Windows, the Microsoft Store installs a stub `python3.exe` in
// WindowsApps that opens the Store instead of running Python — we detect
// this by checking if the resolved path contains "WindowsApps".
var python3NeedsMapping = sync.OnceValue(func() bool {
	p3, err := exec.LookPath("python3")
	if err == nil {
		// Check for Windows Store stub: the path typically contains
		// "AppData\Local\Microsoft\WindowsApps" or "WindowsApps".
		if runtime.GOOS == "windows" && strings.Contains(strings.ToLower(p3), "windowsapps") {
			// This is the Store redirect, not a real python3
		} else {
			return false // real python3 exists, no mapping needed
		}
	}
	_, err2 := exec.LookPath("python")
	return err2 == nil // map only if python exists
})

// migrateLegacyCceasyPaths replaces references to the old .cceasy directory
// with the current .maclaw directory in skill step commands. This fixes
// crafted skills from older versions that hardcode paths like
// C:\Users\xxx\.cceasy\crafted_tools\... which no longer exist.
func migrateLegacyCceasyPaths(skill *NLSkillEntry) {
	paths := cceasyMigrationPaths()
	if paths.oldDir == "" {
		return
	}
	oldSlash := filepath.ToSlash(paths.oldDir)
	newSlash := filepath.ToSlash(paths.newDir)

	// Migrate SkillDir itself
	if strings.Contains(skill.SkillDir, paths.oldDir) {
		skill.SkillDir = strings.ReplaceAll(skill.SkillDir, paths.oldDir, paths.newDir)
	} else if strings.Contains(skill.SkillDir, oldSlash) {
		skill.SkillDir = strings.ReplaceAll(skill.SkillDir, oldSlash, newSlash)
	}

	for i, step := range skill.Steps {
		if step.Params == nil {
			continue
		}
		cmd, _ := step.Params["command"].(string)
		if cmd == "" {
			continue
		}
		changed := false
		if strings.Contains(cmd, oldSlash) {
			cmd = strings.ReplaceAll(cmd, oldSlash, newSlash)
			changed = true
		}
		if strings.Contains(cmd, paths.oldDir) {
			cmd = strings.ReplaceAll(cmd, paths.oldDir, paths.newDir)
			changed = true
		}
		if changed {
			skill.Steps[i].Params["command"] = cmd
			log.Printf("[skill-runner] migrated .cceasy path to .maclaw in step %d of %s", i+1, skill.Name)
		}
	}
}

type cceasyPaths struct{ oldDir, newDir string }

// cceasyMigrationPaths returns the old/new directory pair if .cceasy→.maclaw
// migration is needed. Result is cached after first call to avoid repeated
// syscalls on every skill run.
var cceasyMigrationPaths = sync.OnceValue(func() cceasyPaths {
	home, err := os.UserHomeDir()
	if err != nil {
		return cceasyPaths{}
	}
	oldDir := filepath.Join(home, ".cceasy")
	newDir := filepath.Join(home, ".maclaw")
	if _, err := os.Stat(oldDir); err == nil {
		return cceasyPaths{} // old dir still exists, no migration needed
	}
	if _, err := os.Stat(newDir); err != nil {
		return cceasyPaths{} // new dir doesn't exist either
	}
	return cceasyPaths{oldDir, newDir}
})

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

// loadSkillDocContent reads the SKILL.md documentation content from a skill
// directory. Returns the content string if found, empty string otherwise.
// Used as a fallback for documentation-only skills that have no executable steps.
func loadSkillDocContent(skillDir string) string {
	if skillDir == "" {
		return ""
	}
	for _, name := range []string{"SKILL.md", "skill.md", "README.md"} {
		p := filepath.Join(skillDir, name)
		data, err := os.ReadFile(p)
		if err == nil {
			content := strings.TrimSpace(string(data))
			if content != "" {
				return content
			}
		}
	}
	return ""
}

// autoInstallSkillDependencies installs pip/npm packages declared in the skill's
// requires field. Runs `pip install` and `npm install -g` as needed.
// Returns an error if installation fails, but callers should treat this as
// non-fatal (the packages may already be installed).
func autoInstallSkillDependencies(skill *NLSkillEntry) error {
	var errs []string

	if len(skill.RequiresPython) > 0 {
		// Use the resolved absolute Python path to work in any shell environment.
		pythonCmd := findRealPythonViaCMD()
		if pythonCmd == "" {
			pythonCmd = "python"
			if runtime.GOOS != "windows" {
				pythonCmd = "python3"
			}
		}
		// Use pip install with --quiet to reduce noise
		args := append([]string{"-m", "pip", "install", "--quiet"}, skill.RequiresPython...)
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		cmd := exec.CommandContext(ctx, pythonCmd, args...)
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			errs = append(errs, fmt.Sprintf("pip install failed: %v\n%s", err, strings.TrimSpace(string(out))))
		} else {
			log.Printf("[skill-runner] pip install success: %v", skill.RequiresPython)
		}
	}

	if len(skill.RequiresNode) > 0 {
		// Install packages locally to the skill directory to avoid requiring
		// elevated permissions that npm install -g needs on some systems.
		args := []string{"install", "--silent"}
		args = append(args, skill.RequiresNode...)
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		cmd := exec.CommandContext(ctx, "npm", args...)
		if skill.SkillDir != "" {
			cmd.Dir = skill.SkillDir
		}
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			errs = append(errs, fmt.Sprintf("npm install failed: %v\n%s", err, strings.TrimSpace(string(out))))
		} else {
			log.Printf("[skill-runner] npm install success: %v", skill.RequiresNode)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
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

// ── poll / when / operation helpers ──────────────────────────────────────

// executeStepWithPoll wraps executeStepWithContext with optional poll loop.
// When step.Poll is configured, the step is re-executed at intervals until
// the output matches the termination condition or max attempts are exhausted.
func (r *SkillRunner) executeStepWithPoll(ctx context.Context, runID string, step NLSkillStep, skillDir string) (string, error) {
	if step.Poll == nil {
		return r.executeStepWithContext(ctx, runID, step, skillDir)
	}
	poll := step.Poll
	interval := time.Duration(poll.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	maxAttempts := poll.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 20
	}
	var matchRe *regexp.Regexp
	if poll.UntilMatch != "" {
		var err error
		matchRe, err = regexp.Compile(poll.UntilMatch)
		if err != nil {
			log.Printf("[skill-runner] poll: invalid until_match regex %q: %v", poll.UntilMatch, err)
		}
	}
	var lastOutput string
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		output, err := r.executeStepWithContext(ctx, runID, step, skillDir)
		lastOutput = output
		if err != nil {
			return output, err
		}
		// Check termination conditions.
		if matchRe != nil && matchRe.MatchString(output) {
			log.Printf("[skill-runner] poll: until_match hit on attempt %d/%d", attempt, maxAttempts)
			return output, nil
		}
		if poll.UntilStatus != "" && strings.Contains(output, poll.UntilStatus) {
			log.Printf("[skill-runner] poll: until_status %q found on attempt %d/%d", poll.UntilStatus, attempt, maxAttempts)
			return output, nil
		}
		// No match condition configured — single execution is enough.
		if matchRe == nil && poll.UntilStatus == "" {
			return output, nil
		}
		if attempt < maxAttempts {
			log.Printf("[skill-runner] poll: attempt %d/%d, waiting %v", attempt, maxAttempts, interval)
			select {
			case <-time.After(interval):
			case <-ctx.Done():
				return lastOutput, ctx.Err()
			}
		}
	}
	return lastOutput, fmt.Errorf("poll exhausted after %d attempts without matching condition", maxAttempts)
}

// substituteSkillVarsInString replaces {{key}} and ${key} placeholders in s
// with values from vars.
func substituteSkillVarsInString(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{{"+k+"}}", v)
		s = strings.ReplaceAll(s, "${"+k+"}", v)
	}
	return s
}

// evaluateSimpleCondition evaluates a simple condition expression.
// Supported forms:
//   - "value == expected"  → true if equal (trimmed)
//   - "value != expected"  → true if not equal
//   - "value contains sub" → true if value contains sub
//   - bare non-empty string → true
//   - empty string → false
func evaluateSimpleCondition(expr string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}
	// Try "contains" operator (space-delimited)
	if idx := strings.Index(expr, " contains "); idx > 0 {
		left := strings.TrimSpace(expr[:idx])
		right := strings.TrimSpace(expr[idx+len(" contains "):])
		return strings.Contains(left, right)
	}
	// Try " != " operator (space-delimited to avoid matching inside values)
	if idx := strings.Index(expr, " != "); idx > 0 {
		left := strings.TrimSpace(expr[:idx])
		right := strings.TrimSpace(expr[idx+len(" != "):])
		return left != right
	}
	// Try " == " operator (space-delimited)
	if idx := strings.Index(expr, " == "); idx > 0 {
		left := strings.TrimSpace(expr[:idx])
		right := strings.TrimSpace(expr[idx+len(" == "):])
		return left == right
	}
	// Fallback: try without spaces for compact expressions like "a==b"
	if parts := strings.SplitN(expr, "!=", 2); len(parts) == 2 {
		return strings.TrimSpace(parts[0]) != strings.TrimSpace(parts[1])
	}
	if parts := strings.SplitN(expr, "==", 2); len(parts) == 2 {
		return strings.TrimSpace(parts[0]) == strings.TrimSpace(parts[1])
	}
	// Bare truthy: non-empty = true
	return true
}

// ── api_workflow helpers ─────────────────────────────────────────────────

// stepLabelSelected checks if a step label matches any of the selected steps.
func stepLabelSelected(label string, selected []string) bool {
	for _, s := range selected {
		if strings.EqualFold(label, s) {
			return true
		}
	}
	return false
}

// captureOutputVariables extracts variables from step output using regex patterns.
// Each entry in captures maps a variable name to a regex pattern. The first
// submatch group (if present) is used as the value; otherwise the full match.
// normalizeWindowsShortPathGUI resolves Windows 8.3 short paths (e.g.
// C:\Users\ADMINI~1\...) to their full long-path equivalents (BUG-001).
// On non-Windows or if resolution fails, returns the original path unchanged.
func normalizeWindowsShortPathGUI(p string) string {
	if runtime.GOOS != "windows" {
		return p
	}
	if !strings.Contains(p, "~") {
		return p
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p
	}
	return resolved
}

// win83PathReGUI matches Windows paths containing 8.3 short name notation.
var win83PathReGUI = regexp.MustCompile(`[A-Za-z]:\\[^\s"']+~\d[^\s"']*`)

// normalizePathsInCommandGUI scans a command string for Windows 8.3 short
// paths and replaces them with their long-path equivalents (BUG-001).
func normalizePathsInCommandGUI(command string) string {
	if runtime.GOOS != "windows" || !strings.Contains(command, "~") {
		return command
	}
	return win83PathReGUI.ReplaceAllStringFunc(command, func(match string) string {
		resolved := normalizeWindowsShortPathGUI(match)
		if resolved != match {
			return resolved
		}
		return match
	})
}

func captureOutputVariables(output string, captures map[string]string) map[string]string {
	result := make(map[string]string)
	for varName, pattern := range captures {
		re, err := regexp.Compile(pattern)
		if err != nil {
			log.Printf("[skill-runner] capture: invalid regex for %s: %v", varName, err)
			continue
		}
		m := re.FindStringSubmatch(output)
		if len(m) > 1 {
			result[varName] = m[1] // first submatch group
		} else if len(m) == 1 {
			result[varName] = m[0] // full match
		}
	}
	return result
}

// ── poll action ─────────────────────────────────────────────────────────

// executePollStep runs a command repeatedly at a fixed interval until a
// success_pattern is matched in the output or a timeout is reached.
//
// Params:
//   - command:          bash command to execute each poll cycle
//   - interval_seconds: seconds between polls (default 8)
//   - timeout_seconds:  max total wait time (default 180)
//   - success_pattern:  regex that indicates success when matched in stdout
//   - working_dir:      optional working directory
//
// The step succeeds when success_pattern matches. The matched output is
// returned so that capture rules can extract variables from it.
func (r *SkillRunner) executePollStep(ctx context.Context, step NLSkillStep, skillDir string) (string, error) {
	command, _ := step.Params["command"].(string)
	if command == "" {
		return "", fmt.Errorf("poll step: missing command parameter")
	}
	successPattern, _ := step.Params["success_pattern"].(string)
	if successPattern == "" {
		return "", fmt.Errorf("poll step: missing success_pattern parameter")
	}
	successRe, err := regexp.Compile(successPattern)
	if err != nil {
		return "", fmt.Errorf("poll step: invalid success_pattern regex: %v", err)
	}

	interval := 8 * time.Second
	if v, ok := step.Params["interval_seconds"].(float64); ok && v > 0 {
		interval = time.Duration(v) * time.Second
	}
	timeout := 180 * time.Second
	if v, ok := step.Params["timeout_seconds"].(float64); ok && v > 0 {
		timeout = time.Duration(v) * time.Second
	}

	pollCtx, pollCancel := context.WithTimeout(ctx, timeout)
	defer pollCancel()

	log.Printf("[skill-runner] poll: command=%q interval=%v timeout=%v pattern=%q",
		command, interval, timeout, successPattern)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	attempt := 0
	var lastOutput string
	var lastErr error

	for {
		select {
		case <-pollCtx.Done():
			return lastOutput, fmt.Errorf("poll timeout after %v (%d attempts), last error: %v", timeout, attempt, lastErr)
		default:
		}

		attempt++
		output, execErr := runBashStepWithContext(pollCtx, command, step.Params, skillDir, r.executor.app)
		lastOutput = output
		lastErr = execErr

		if execErr == nil && successRe.MatchString(output) {
			log.Printf("[skill-runner] poll: success after %d attempts", attempt)
			return output, nil
		}

		log.Printf("[skill-runner] poll: attempt %d — no match yet (err=%v, output=%d bytes)",
			attempt, execErr, len(output))

		// Wait for next tick or context cancellation
		select {
		case <-pollCtx.Done():
			errMsg := "none"
			if lastErr != nil {
				errMsg = lastErr.Error()
			}
			return lastOutput, fmt.Errorf("poll timeout after %v (%d attempts), last error: %s", timeout, attempt, errMsg)
		case <-ticker.C:
			// continue to next poll
		}
	}
}
