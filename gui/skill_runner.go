package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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
	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/security"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// ── Run Status ──────────────────────────────────────────────────────────

// SkillRunSessionMeta captures the remote session associated with a skill run.
type SkillRunSessionMeta struct {
	SessionID       string        `json:"session_id,omitempty"`
	Tool            string        `json:"tool,omitempty"`
	ProjectPath     string        `json:"project_path,omitempty"`
	Status          SessionStatus `json:"status,omitempty"`
	JobID           string        `json:"job_id,omitempty"`
	RunID           string        `json:"run_id,omitempty"`
	ResumeSessionID string        `json:"resume_session_id,omitempty"`
	LaunchSource    string        `json:"launch_source,omitempty"`
}

// SkillRunSummary provides a compact, user-facing summary of the most
// important state for a skill run.
type SkillRunSummary struct {
	CurrentStepIndex          int                 `json:"current_step_index,omitempty"`
	CurrentStep               string              `json:"current_step,omitempty"`
	CurrentStepStatus         skillStepStatus     `json:"current_step_status,omitempty"`
	LastCompletedStep         string              `json:"last_completed_step,omitempty"`
	LastCompletedStepIndex    int                 `json:"last_completed_step_index,omitempty"`
	LastOutputSnippet         string              `json:"last_output_snippet,omitempty"`
	LastErrorSnippet          string              `json:"last_error_snippet,omitempty"`
	HasSessionBinding         bool                `json:"has_session_binding,omitempty"`
	NeedsArtifactVerification bool                `json:"needs_artifact_verification,omitempty"`
	ArtifactPath              string              `json:"artifact_path,omitempty"`
	ArtifactStatus            skillArtifactStatus `json:"artifact_status,omitempty"`
}

// SkillRunStatus represents one skill execution.
type SkillRunStatus struct {
	RunID             string                  `json:"run_id"`
	Skill             string                  `json:"skill"`
	Status            skillRunLifecycleStatus `json:"status"`
	Steps             []StepResult            `json:"steps"`
	Session           *SkillRunSessionMeta    `json:"session,omitempty"`
	SessionProgress   *SessionProgressInfo    `json:"session_progress,omitempty"`
	Summary           SkillRunSummary         `json:"summary,omitempty"`
	ExpectedOutput    string                  `json:"expected_output,omitempty"`
	ExpectedArtifact  bool                    `json:"expected_artifact,omitempty"`
	StartedAt         string                  `json:"started_at"`
	EndedAt           string                  `json:"ended_at,omitempty"`
	Error             string                  `json:"error,omitempty"`
	Warnings          []string                `json:"warnings,omitempty"`
	DurationMs        int64                   `json:"duration_ms,omitempty"`
	TotalSteps        int                     `json:"total_steps,omitempty"`
	FailedSteps       int                     `json:"failed_steps,omitempty"`
	SkippedSteps      int                     `json:"skipped_steps,omitempty"`
	SelfRepairPending bool                    `json:"self_repair_pending,omitempty"` // true when async self-repair is in progress
}

// SessionProgressInfo captures the latest state from the session's internal
// AI agent, polled in the background by the SkillRunner. This gives callers
// visibility into what the session agent is doing without needing to call
// query_session separately.
type SessionProgressInfo struct {
	SessionStatus   SessionStatus `json:"session_status"`
	CurrentTask     string        `json:"current_task,omitempty"`      // what the session agent is currently doing
	ProgressSummary string        `json:"progress_summary,omitempty"`  // human-readable progress
	LastResult      string        `json:"last_result,omitempty"`       // last tool call result or output
	LastCommand     string        `json:"last_command,omitempty"`      // last command executed
	WaitingForUser  bool          `json:"waiting_for_user,omitempty"`  // session agent is waiting for input
	LastOutputLines []string      `json:"last_output_lines,omitempty"` // last N raw output lines (max 10)
	UpdatedAt       string        `json:"updated_at,omitempty"`        // when this snapshot was taken
	PollCount       int           `json:"poll_count,omitempty"`        // how many times we've polled
}

// StepResult records a single step result.
type StepResult struct {
	Index           int             `json:"index"`
	Name            string          `json:"name,omitempty"`
	Action          string          `json:"action"`
	Status          skillStepStatus `json:"status"`
	Output          string          `json:"output,omitempty"`
	Error           string          `json:"error,omitempty"`
	ExitCode        int             `json:"exit_code,omitempty"`
	StdoutLastLines []string        `json:"stdout_last_lines,omitempty"`
	StderrLastLines []string        `json:"stderr_last_lines,omitempty"`
	ShellPath       string          `json:"shell_path,omitempty"`
	CommandResolved string          `json:"command_resolved,omitempty"`
	DurationMs      int64           `json:"duration_ms,omitempty"`
	Timeout         bool            `json:"timeout,omitempty"`
}

// ── Skill Runner ────────────────────────────────────────────────────────

// SkillRunner provides asynchronous, platform-aware skill execution.
type SkillRunner struct {
	executor      *SkillExecutor
	mu            sync.RWMutex
	runs          map[string]*skillRun
	counter       int
	uploadTrigger *AutoUploadTrigger
	packageFn     func(skillName string) (string, error) // packageSkillForMarket

	// recentRepairs tracks skills that were recently auto-repaired.
	// Consumed by the system prompt builder to notify the LLM about
	// repaired skills so it can adjust its calling strategy.
	// Key: skill name, Value: repair explanation.
	// Entries are consumed (deleted) after being injected into the prompt.
	recentRepairs sync.Map
}

type skillRun struct {
	status        SkillRunStatus
	cancel        context.CancelFunc
	monitorCancel context.CancelFunc // cancels the session monitor goroutine
	templateVars  map[string]string
	runArgs       map[string]interface{}
	selectedSteps []string          // api_workflow mode: only run steps with these labels
	extraEnv      map[string]string // env vars from run_skill caller, injected into subprocesses
}

// NewSkillRunner creates a SkillRunner.
func NewSkillRunner(executor *SkillExecutor) *SkillRunner {
	return &SkillRunner{
		executor: executor,
		runs:     make(map[string]*skillRun),
	}
}

// StartRun starts a skill asynchronously and returns a run ID for polling.
func (r *SkillRunner) StartRun(skillName string, runArgs map[string]interface{}) (string, error) {
	return r.StartRunForOwner(r.defaultSkillRunPolicyOwnerID(), skillName, runArgs)
}

// StartRunForOwner starts a skill run under an explicit workflow policy owner.
func (r *SkillRunner) StartRunForOwner(policyOwnerID, skillName string, runArgs map[string]interface{}) (string, error) {
	if r != nil && r.executor != nil && r.executor.app != nil {
		if err := r.executor.app.ensureWorkflowAllowsRemoteToolCallForOwner(policyOwnerID, "manage_skill", map[string]interface{}{"action": "run", "name": skillName, "args": runArgs}); err != nil {
			return "", err
		}
	}
	// ?? skill ? match by name regardless of status so we can provide
	// specific error messages for disabled/needs_setup skills (Bug #3).
	r.executor.mu.RLock()
	var target *corelib.NLSkillEntry
	var collisions []corelib.NLSkillEntry // track bare name collisions across publishers
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
			// Multiple skills match the bare name ? require qualified name.
			var qualifiedNames []string
			for _, s := range collisions {
				if s.Publisher != "" {
					qualifiedNames = append(qualifiedNames, s.Publisher+":"+s.Name)
				} else {
					qualifiedNames = append(qualifiedNames, s.Name+" (local)")
				}
			}
			r.executor.mu.RUnlock()
			return "", fmt.Errorf("skill name %q is ambiguous ? multiple skills match:\n  %s\nPlease use the qualified name (publisher:name) to disambiguate",
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
	switch normalizeSkillEntryStatus(target.Status) {
	case skillEntryStatusNeedsSetup:
		return "", fmt.Errorf("skill %q needs setup. Installation was incomplete (missing dependencies or files). Please check the skill directory (%s) and complete configuration", skillName, target.SkillDir)
	case skillEntryStatusDisabled:
		return "", fmt.Errorf("skill %q is disabled. Please enable it first", skillName)
	case skillEntryStatusActive, skillEntryStatusUnknown:
	default:
		return "", fmt.Errorf("skill %q status is %q, expected active", skillName, target.Status)
	}

	// BUG-005: Normalize skill directory path (resolve 8.3 short paths on Windows)
	if runtime.GOOS == "windows" && target.SkillDir != "" {
		target.SkillDir = normalizeWindowsShortPathGUI(target.SkillDir)
	}

	// Migrate legacy .cceasy paths to .maclaw ? crafted skills from older
	// versions may reference scripts in the old directory structure.
	migrateLegacyCceasyPaths(target)
	if err := cskill.HydrateRunMetadataFromDir(target); err != nil {
		log.Printf("[skill-runner] hydrate skill metadata from %q failed: %v", target.SkillDir, err)
	}

	// Normalize community/imported skill shapes before pre-checks and execution.
	cskill.NormalizeSkillForRunner(target)

	if cskill.IsKnowledgeSkillType(target.Type) {
		return "", fmt.Errorf("%s", cskill.FormatNoExecutableStepsMessage(skillName, target, cskill.RunnerBackendGUI))
	}

	templateVars := normalizeSkillRunVars(runArgs)
	extraEnv := cskill.ExtractRunExtraEnvFromArgs(runArgs)
	if cskill.IsPipelineSkill(target) {
		return r.startPipelineRun(skillName, target, runArgs, templateVars, extraEnv)
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
			docContent = truncateRunesMarker(docContent, 8000, "\n\n... (truncated)")
			// Build task description from user context + documentation.
			var userContext string
			for _, key := range []string{"user_prompt", "input", "query"} {
				if value := strings.TrimSpace(templateVars[key]); value != "" {
					userContext = value
					break
				}
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
			return "", fmt.Errorf("%s", cskill.FormatNoExecutableStepsMessage(skillName, target, cskill.RunnerBackendGUI))
		}
	}

	if !strings.EqualFold(target.Mode, "interactive") {
		if err := r.ensureSkillSecurityScanned(target); err != nil {
			return "", err
		}
	}

	// Shared runner preparation handles step selection, parameter completion,
	// requirements, implicit placeholders, and local file diagnostics.
	prep, err := cskill.PrepareRunnerExecution(target, templateVars, runArgs, extraEnv, cskill.RunnerBackendGUI)
	if err != nil {
		return "", err
	}
	selectedSteps := prep.SelectedSteps
	warnings := prep.Warnings
	fileWarnings := prep.FileWarnings
	for _, v := range prep.RequirementWarnings {
		log.Printf("[skill-runner] requirement warning for %q: %s", skillName, v.Message)
	}
	for _, warning := range fileWarnings {
		log.Printf("[skill-runner] file warning for %q: %s", skillName, warning)
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
			Status:         skillRunStatusRunning,
			ExpectedOutput: strings.TrimSpace(templateVars["output"]),
			ExpectedArtifact: skillRunExpectsArtifactForSteps(target, prep.ExecutionSteps,
				strings.TrimSpace(templateVars["output"]), len(prep.SelectedSteps) == 0),
			StartedAt: time.Now().Format(time.RFC3339),
			Steps:     make([]StepResult, len(target.Steps)),
			Warnings:  warnings,
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
			Status: skillStepStatusPending,
		}
	}
	r.runs[runID] = run
	r.mu.Unlock()

	// 异步执行
	go r.executeAsync(ctx, run, target)

	return runID, nil
}

func (r *SkillRunner) defaultSkillRunPolicyOwnerID() string {
	if r == nil || r.executor == nil || r.executor.app == nil {
		return ""
	}
	return r.executor.app.defaultManualPolicyOwnerID()
}

// startPipelineRun starts a pipeline skill asynchronously.
func (r *SkillRunner) startPipelineRun(skillName string, target *corelib.NLSkillEntry, runArgs map[string]interface{}, templateVars map[string]string, extraEnv map[string]string) (string, error) {
	if target == nil {
		return "", fmt.Errorf("skill entry is nil")
	}
	if len(target.Pipeline) == 0 {
		return "", fmt.Errorf("%s", cskill.FormatNoExecutableStepsMessage(skillName, target, cskill.RunnerBackendGUI))
	}
	if templateVars == nil {
		templateVars = map[string]string{}
	}
	prep, err := cskill.PreparePipelineRunnerExecution(target, templateVars, runArgs, extraEnv, cskill.RunnerBackendGUI)
	if err != nil {
		return "", err
	}
	for _, warning := range prep.RequirementWarnings {
		log.Printf("[skill-runner] pipeline requirement warning for %q: %s", skillName, warning.Message)
	}
	warnings := prep.Warnings

	r.mu.Lock()
	r.counter++
	runID := fmt.Sprintf("run-%d-%d", time.Now().UnixMilli(), r.counter)
	ctx, cancel := context.WithCancel(context.Background())
	run := &skillRun{
		status: SkillRunStatus{
			RunID:          runID,
			Skill:          skillName,
			Status:         skillRunStatusRunning,
			ExpectedOutput: strings.TrimSpace(templateVars["output"]),
			ExpectedArtifact: skillRunExpectsArtifactForSteps(target, nil,
				strings.TrimSpace(templateVars["output"]), true),
			StartedAt:  time.Now().Format(time.RFC3339),
			Steps:      make([]StepResult, len(target.Pipeline)),
			TotalSteps: len(target.Pipeline),
			Warnings:   warnings,
		},
		cancel:       cancel,
		templateVars: templateVars,
		runArgs:      cloneSkillRunArgs(runArgs),
		extraEnv:     extraEnv,
	}
	for i, step := range target.Pipeline {
		run.status.Steps[i] = StepResult{
			Index:  i,
			Name:   step.Skill,
			Action: "pipeline",
			Status: skillStepStatusPending,
		}
	}
	r.runs[runID] = run
	r.mu.Unlock()

	go r.executePipelineAsync(ctx, run, target)
	return runID, nil
}

func (r *SkillRunner) executePipelineAsync(ctx context.Context, run *skillRun, entry *corelib.NLSkillEntry) {
	execStart := time.Now()
	globalTimeout := 5 * time.Minute
	if entry.GlobalTimeout > 0 {
		globalTimeout = time.Duration(entry.GlobalTimeout) * time.Second
	}
	globalCtx, cancel := context.WithTimeout(ctx, globalTimeout)
	defer cancel()

	baseArgs := cloneSkillRunArgs(run.runArgs)
	if len(baseArgs) == 0 {
		baseArgs = map[string]interface{}{}
	}
	if len(run.extraEnv) > 0 {
		baseArgs["extra_env"] = run.extraEnv
	}
	baseRunArgs := cskill.WithPipelineRunStack(baseArgs, entry.Name)
	pr := &cskill.PipelineRunner{Executor: skillExecutorPipelineExecutor{exec: r.executor, baseRunArgs: baseRunArgs}}
	result, err := pr.Run(globalCtx, entry.Pipeline, run.templateVars)
	if err == nil && result == nil {
		err = fmt.Errorf("pipeline returned no result")
	}

	var execErr error
	r.mu.Lock()
	if err != nil {
		execErr = err
		for i := range run.status.Steps {
			if run.status.Steps[i].LifecycleStatus() == skillStepStatusPending {
				run.status.Steps[i].Status = skillStepStatusSkipped
			}
		}
		run.status.Error = err.Error()
		r.mu.Unlock()
		r.updateUsageStats(entry, execErr)
		r.finalizeRunOutcome(run, skillRunStatusFailed, execStart)
		return
	}

	if result.Vars != nil {
		run.templateVars = result.Vars
	}
	for i, stepResult := range result.StepResults {
		if i >= len(run.status.Steps) {
			break
		}
		run.status.Steps[i].Name = stepResult.Skill
		stepStatus := normalizeSkillPipelineStepStatus(stepResult.Status).StepStatus()
		run.status.Steps[i].Status = stepStatus
		if stepStatus == skillStepStatusFailed {
			run.status.Steps[i].Error = stepResult.Error
		}
		if stepResult.CapturedVars != nil {
			run.status.Steps[i].Output = stepResult.CapturedVars["output"]
		}
	}
	for i := len(result.StepResults); i < len(run.status.Steps); i++ {
		if run.status.Steps[i].LifecycleStatus() == skillStepStatusPending {
			run.status.Steps[i].Status = skillStepStatusSkipped
		}
	}
	finalStatus := skillRunStatusFailed
	switch normalizeSkillPipelineStatusFromCore(result.Status) {
	case skillPipelineStatusCompleted:
		finalStatus = skillRunStatusSuccess
	case skillPipelineStatusCancelled:
		finalStatus = skillRunStatusCancelled
	default:
		if result.Error != "" {
			run.status.Error = result.Error
			execErr = fmt.Errorf("%s", result.Error)
		}
		if execErr == nil {
			execErr = fmt.Errorf("pipeline status: %s", result.Status)
		}
	}
	r.mu.Unlock()
	if finalStatus != skillRunStatusCancelled {
		r.updateUsageStats(entry, execErr)
	}
	r.finalizeRunOutcome(run, finalStatus, execStart)
}

func (r *SkillRunner) GetRunStatus(runID string) (*SkillRunStatus, error) {
	r.mu.RLock()
	run, ok := r.runs[runID]
	if !ok {
		r.mu.RUnlock()
		return nil, fmt.Errorf("run %q not found", runID)
	}
	cp := snapshotRunStatus(&run.status)
	r.mu.RUnlock()

	r.hydrateRunSessionMeta(&cp)
	summarizeSkillRun(&cp)
	return &cp, nil
}

// snapshotRunStatus returns a deep copy of src safe to mutate outside the
// runner lock. Single source of truth for run-status copying so the
// GetRunStatus and ListRuns paths can never drift in which fields they
// deep-copy (the original Session race came from exactly that drift).
// Caller must hold r.mu.
func snapshotRunStatus(src *SkillRunStatus) SkillRunStatus {
	cp := *src
	cp.Steps = make([]StepResult, len(src.Steps))
	copy(cp.Steps, src.Steps)
	if len(src.Warnings) > 0 {
		cp.Warnings = append([]string(nil), src.Warnings...)
	}
	// Deep copy Session so post-lock hydrate/summarize mutate the snapshot,
	// not the shared live SkillRunSessionMeta that concurrent callers also
	// dereference.
	if src.Session != nil {
		sessCopy := *src.Session
		cp.Session = &sessCopy
	}
	// Deep copy SessionProgress (incl. its LastOutputLines slice) to avoid the
	// monitor goroutine mutating the returned snapshot.
	if src.SessionProgress != nil {
		spCopy := *src.SessionProgress
		if len(src.SessionProgress.LastOutputLines) > 0 {
			spCopy.LastOutputLines = make([]string, len(src.SessionProgress.LastOutputLines))
			copy(spCopy.LastOutputLines, src.SessionProgress.LastOutputLines)
		}
		cp.SessionProgress = &spCopy
	}
	return cp
}

// CancelRun cancels a running skill.
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

// ListRuns returns all run records.
func (r *SkillRunner) ListRuns() []SkillRunStatus {
	r.mu.RLock()
	result := make([]SkillRunStatus, 0, len(r.runs))
	for _, run := range r.runs {
		result = append(result, snapshotRunStatus(&run.status))
	}
	r.mu.RUnlock()
	for i := range result {
		r.hydrateRunSessionMeta(&result[i])
		summarizeSkillRun(&result[i])
	}
	return result
}

// CleanupFinished removes old finished run records, keeping the newest maxKeep items.
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
		if !run.status.IsRunning() {
			finished = append(finished, finishedEntry{id: id, endedAt: run.status.EndedAt})
		}
	}
	// Oldest first (EndedAt is RFC3339, lexically sortable).
	slices.SortFunc(finished, func(a, b finishedEntry) int {
		return strings.Compare(a.endedAt, b.endedAt)
	})
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
	status.Session.Status = normalizeSessionStatus(session.Status.String())
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
	artifactExpected := status.ExpectedArtifact || artifactPath != "" || isInstructionOnlySkillStatus(status)
	if artifactPath == "" {
		detectedPath := detectArtifactPathFromStatus(status)
		if detectedPath != "" && (artifactExpected || artifactExists(detectedPath)) {
			artifactPath = detectedPath
		}
	}
	if artifactPath != "" {
		status.Summary.ArtifactPath = artifactPath
		if status.IsRunning() {
			status.Summary.ArtifactStatus = skillArtifactStatusPending
		} else if artifactExists(artifactPath) {
			status.Summary.ArtifactStatus = skillArtifactStatusVerified
		} else if artifactExpected {
			status.Summary.ArtifactStatus = skillArtifactStatusMissing
		}
	}
	if craftVerificationPassedStatus(status) {
		status.Summary.ArtifactStatus = skillArtifactStatusVerified
	}
	if isInstructionOnlySkillStatus(status) {
		status.Summary.NeedsArtifactVerification = status.Summary.ArtifactStatus != skillArtifactStatusVerified
	}
	for i, step := range status.Steps {
		switch step.LifecycleStatus() {
		case skillStepStatusRunning:
			status.Summary.CurrentStepIndex = i
			status.Summary.CurrentStep = step.Action
			status.Summary.CurrentStepStatus = step.Status
		case skillStepStatusSuccess:
			status.Summary.LastCompletedStepIndex = i
			status.Summary.LastCompletedStep = step.Action
			if snippet := strings.TrimSpace(step.Output); snippet != "" {
				status.Summary.LastOutputSnippet = truncateSkillRunSnippet(snippet)
			}
		case skillStepStatusFailed:
			status.Summary.CurrentStepIndex = i
			status.Summary.CurrentStep = step.Action
			status.Summary.CurrentStepStatus = step.Status
			status.FailedSteps++
			if snippet := strings.TrimSpace(firstNonEmptyTraceText(step.Error, step.Output)); snippet != "" {
				status.Summary.LastErrorSnippet = truncateSkillRunSnippet(snippet)
			}
		case skillStepStatusSkipped:
			status.SkippedSteps++
		}
	}
	if status.Summary.CurrentStep == "" && len(status.Steps) > 0 {
		for i := len(status.Steps) - 1; i >= 0; i-- {
			step := status.Steps[i]
			if step.LifecycleStatus() != skillStepStatusPending {
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
	trimmed = strings.Trim(trimmed, "`\"' ,.;:()[]{}")
	if looksLikeArtifactPath(trimmed) && filepath.IsAbs(trimmed) {
		return trimmed
	}
	for _, field := range strings.Fields(trimmed) {
		candidate := strings.Trim(field, "`\"' ,.;:()[]{}")
		if looksLikeArtifactPath(candidate) && filepath.IsAbs(candidate) {
			return candidate
		}
	}
	return ""
}

func looksLikeArtifactPath(path string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case ".pdf", ".docx", ".pptx", ".xlsx", ".csv", ".tsv", ".md", ".txt", ".json", ".html", ".png", ".jpg", ".jpeg", ".svg", ".drawio", ".xml":
		return true
	default:
		return false
	}
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
	return cskill.NormalizeRunVars(runArgs)
}

func cloneSkillRunArgs(runArgs map[string]interface{}) map[string]interface{} {
	if len(runArgs) == 0 {
		return nil
	}
	dst := make(map[string]interface{}, len(runArgs))
	for key, value := range runArgs {
		dst[key] = value
	}
	return dst
}

// detectImplicitRequiredArgs scans step commands for {{key}} placeholders
// that are not provided in templateVars. Returns the list of missing keys.
// This catches skills that use {{input}}/{{output}} without declaring
// required_args in their frontmatter.
func detectImplicitRequiredArgs(steps []corelib.NLSkillStep, vars map[string]string) []string {
	result := cskill.DetectImplicitRequiredArgs(steps, vars)
	slices.Sort(result)
	return result
}

func resolveSkillStep(step corelib.NLSkillStep, vars map[string]string, skillDir string, params []corelib.NLSkillParam) (corelib.NLSkillStep, error) {
	// Delegate to the shared corelib ResolveStep, passing the GUI's
	// platform-aware quoteSkillInputForShell as the quoting function.
	// This ensures alias resolution, CLI args, craft_tool injection,
	// and working_dir resolution all use the single shared code path.
	result, err := cskill.ResolveStep(step, vars, skillDir, params, quoteSkillInputForStep(step))
	if err != nil {
		return step, err
	}
	return result.Step, nil
}

func withSkillPreferredShell(step corelib.NLSkillStep, preferredShell string) corelib.NLSkillStep {
	preferredShell = strings.TrimSpace(preferredShell)
	if preferredShell == "" {
		return step
	}
	if step.Params == nil {
		step.Params = map[string]interface{}{}
	} else {
		cp := make(map[string]interface{}, len(step.Params)+1)
		for k, v := range step.Params {
			cp[k] = v
		}
		step.Params = cp
	}
	if _, exists := step.Params["preferred_shell"]; !exists {
		step.Params["preferred_shell"] = preferredShell
	}
	return step
}

func quoteSkillInputForStep(step corelib.NLSkillStep) func(string) string {
	preferredShell, _ := step.Params["preferred_shell"].(string)
	return func(input string) string {
		return cskill.QuoteForShellPreference(input, preferredShell)
	}
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
	result := cskill.SubstituteVariablesWithQuote(command, vars, quoteSkillInputForShell)
	if result != original {
		log.Printf("[skill-runner] variable substitution: %q -> %q", original, result)
	}
	return result
}

// quoteSkillInputForShell wraps a user-supplied value for safe embedding
// in a shell command string.
//
// On Windows the skill runner dispatches simple commands (node, python, ...)
// through cmd.exe, which does NOT recognise single-quotes as delimiters.
// Using single-quotes caused the xh-md-to-pdf path-concatenation bug where
// the trailing backslash of {baseDir} merged with the opening single-quote.
// We therefore use double-quotes on Windows and single-quotes elsewhere.
func quoteSkillInputForShell(input string) string {
	return cskill.QuoteForRunnerShell(input)
}

func truncateSkillRunSnippet(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return truncateRunesWithEllipsis(text, 160)
}

// truncateRunesWithEllipsis truncates s to at most maxRunes runes, appending
// "..." when truncated. See truncateRunesMarker.
func truncateRunesWithEllipsis(s string, maxRunes int) string {
	return truncateRunesMarker(s, maxRunes, "...")
}

// truncateRunesMarker truncates s to at most maxRunes runes, appending marker
// when truncated. It is UTF-8 safe: byte slicing would split a multi-byte rune
// and emit invalid UTF-8 into JSON status payloads (this codebase is heavily
// Chinese, so byte slicing corrupts text routinely). Single-pass over byte
// offsets so it avoids both the RuneCount pre-scan and a full []rune copy.
func truncateRunesMarker(s string, maxRunes int, marker string) string {
	if maxRunes <= 0 {
		return ""
	}
	count := 0
	for i := range s { // range over string yields rune start byte offsets
		if count == maxRunes {
			return s[:i] + marker
		}
		count++
	}
	return s // fewer than or exactly maxRunes runes; no truncation
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

func isInstructionOnlySkillEntry(skill *corelib.NLSkillEntry) bool {
	if skill == nil || len(skill.Steps) != 1 {
		return false
	}
	step := skill.Steps[0]
	if !classifySkillStepAction(step.Action).IsCraftTool() {
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

func skillRunExpectsArtifact(skill *corelib.NLSkillEntry, expectedOutput string) bool {
	return skillRunExpectsArtifactForSteps(skill, nil, expectedOutput, true)
}

func skillRunExpectsArtifactForSteps(skill *corelib.NLSkillEntry, steps []corelib.NLSkillStep, expectedOutput string, useGlobalContract bool) bool {
	if strings.TrimSpace(expectedOutput) != "" {
		return true
	}
	if skill == nil {
		return false
	}
	if useGlobalContract && (skill.ProducesArtifact || isInstructionOnlySkillEntry(skill)) {
		return true
	}
	if steps == nil {
		steps = skill.Steps
	}
	for _, step := range steps {
		if stepExpectsArtifact(step) {
			return true
		}
	}
	return false
}

func stepExpectsArtifact(step corelib.NLSkillStep) bool {
	params := step.Params
	if len(params) == 0 {
		return false
	}
	if normalizeCraftVerificationModeKind(stringVal(params, "verification_mode")).RequiresArtifact() {
		return true
	}
	if strings.TrimSpace(stringVal(params, "output_path")) != "" {
		return true
	}
	if raw, ok := params["expected_artifacts"]; ok {
		switch typed := raw.(type) {
		case []string:
			return len(typed) > 0
		case []interface{}:
			return len(typed) > 0
		case string:
			return strings.TrimSpace(typed) != ""
		}
	}
	return false
}

func isInstructionOnlySkillStatus(status *SkillRunStatus) bool {
	if status == nil || len(status.Steps) != 1 {
		return false
	}
	step := status.Steps[0]
	if !classifySkillStepAction(step.Action).IsCraftTool() {
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

func resolveSkillStepSessionID(step corelib.NLSkillStep, fallback string, manager *RemoteSessionManager) string {
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

func (r *SkillRunner) resolveStepSessionID(runID string, step corelib.NLSkillStep) string {
	return resolveSkillStepSessionID(step, r.latestRunSessionID(runID), r.executor.manager)
}

// ── 异步执行核心 ────────────────────────────────────────────────────────

// finalizeRunOutcome sets the terminal status and timing on a run under the
// runner lock. It is the single tail shared by every finalization path
// (success, failure, cancel, timeout, panic) so the "set Status + EndedAt +
// DurationMs" triple can never drift between them. Caller must NOT hold r.mu;
// it is only safe to call where the prior mutation already released the lock
// (i.e. not fused into a single lock-hold with other status writes).
func (r *SkillRunner) finalizeRunOutcome(run *skillRun, status skillRunLifecycleStatus, execStart time.Time) {
	r.mu.Lock()
	run.status.Status = status
	run.status.EndedAt = time.Now().Format(time.RFC3339)
	run.status.DurationMs = time.Since(execStart).Milliseconds()
	r.mu.Unlock()
}

// failRunPendingSkipped marks all still-pending steps as skipped, records
// errMsg as the run error, updates usage stats, and finalizes the run as
// failed. It captures the recurring "fail-and-finalize" lock-dance used by the
// pre-execution guard paths (proxy config/start failures). Caller must NOT hold
// r.mu.
func (r *SkillRunner) failRunPendingSkipped(run *skillRun, skill *corelib.NLSkillEntry, errMsg string, execErr error, execStart time.Time) {
	r.mu.Lock()
	for i := range run.status.Steps {
		if run.status.Steps[i].LifecycleStatus() == skillStepStatusPending {
			run.status.Steps[i].Status = skillStepStatusSkipped
		}
	}
	run.status.Error = errMsg
	r.mu.Unlock()
	r.updateUsageStats(skill, execErr)
	r.finalizeRunOutcome(run, skillRunStatusFailed, execStart)
}

func (r *SkillRunner) executeAsync(ctx context.Context, run *skillRun, skill *corelib.NLSkillEntry) {
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
			execErr := fmt.Errorf("panic: %v", rec)
			r.mu.Lock()
			run.status.Error = execErr.Error()
			r.mu.Unlock()
			r.updateUsageStats(skill, execErr)
			r.finalizeRunOutcome(run, skillRunStatusFailed, execStart)
		}
	}()

	// Interactive skills should not be auto-executed ? they are meant to be
	// invoked on-demand by AI agents. If mode == "interactive", skip automatic
	// step execution and mark as success (the skill's instructions are available
	// for the AI context, not for runner auto-execution).
	if strings.EqualFold(skill.Mode, "interactive") {
		r.mu.Lock()
		for i := range skill.Steps {
			run.status.Steps[i].Status = skillStepStatusSkipped
		}
		r.mu.Unlock()
		r.updateUsageStats(skill, nil)
		r.finalizeRunOutcome(run, skillRunStatusSuccess, execStart)
		return
	}

	var execErr error
	hasFailure := false
	isAPIWorkflow := strings.EqualFold(skill.Mode, "api_workflow")
	executionSteps := cskill.SelectedExecutableSteps(skill.Steps, run.selectedSteps)

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
	proxyProbeVars := r.templateVarsForRun(run.status.RunID)
	if len(proxyProbeVars) == 0 && len(run.templateVars) > 0 {
		proxyProbeVars = make(map[string]string, len(run.templateVars))
		for key, value := range run.templateVars {
			proxyProbeVars[key] = value
		}
	}
	proxyProbeSteps := cskill.PrecheckExecutableSteps(executionSteps, proxyProbeVars)
	proxyRequiredEnv := skill.RequiredEnv
	if len(proxyProbeSteps) == 0 && len(executionSteps) > 0 {
		proxyRequiredEnv = nil
	}
	needsProxy := corelib.NeedsOpenAIProxyAuto(proxyRequiredEnv, run.extraEnv, proxyProbeSteps, skill.SkillDir)
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
		if strings.TrimSpace(proxyCfg.URL) == "" || strings.TrimSpace(proxyCfg.Model) == "" {
			errMsg := "skill requires OpenAI-compatible environment variables, but the GUI local proxy cannot start because no LLM provider URL/model is configured [action: configure_llm]"
			r.failRunPendingSkipped(run, skill, errMsg, fmt.Errorf("%s", errMsg), execStart)
			return
		}

		proxy := corelib.NewOpenAIProxy(proxyCfg)
		port, proxyErr := proxy.Start()
		if proxyErr != nil {
			errMsg := fmt.Sprintf("skill requires OpenAI-compatible environment variables, but the GUI local proxy failed to start: %v [action: retry]", proxyErr)
			r.failRunPendingSkipped(run, skill, errMsg, fmt.Errorf("%s", errMsg), execStart)
			return
		}
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

	// ── Dependency auto-install is now handled by the unified requirement
	// system in StartRun (Registry.FixAll). The pip/npm packages are checked
	// and installed before execution begins. ──

	log.Printf("[skill-runner] starting skill %q (%d steps, mode=%s, dir=%s)",
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

	// ── Parameter schema: ensure every skill has params (explicit or synthesized) ──
	// This is the single path that ensures BindParams is called for all skills,
	// closing the LLM↔Skill parameter name gap.
	skillParams := cskill.CompleteParamsForRunner(skill.Params, executionSteps, skill.RequiredArgs)
	if len(skillParams) > len(skill.Params) {
		log.Printf("[skill-runner] completed param schema for %q: explicit=%d complete=%d", skill.Name, len(skill.Params), len(skillParams))
	}

	for i, step := range skill.Steps {
		// Check for global timeout
		select {
		case <-ctx.Done():
			r.mu.Lock()
			for j := i; j < len(skill.Steps); j++ {
				run.status.Steps[j].Status = skillStepStatusSkipped
			}
			r.mu.Unlock()
			r.finalizeRunOutcome(run, skillRunStatusCancelled, execStart)
			return
		case <-globalCtx.Done():
			if ctx.Err() != nil {
				r.mu.Lock()
				for j := i; j < len(skill.Steps); j++ {
					run.status.Steps[j].Status = skillStepStatusSkipped
				}
				r.mu.Unlock()
				r.finalizeRunOutcome(run, skillRunStatusCancelled, execStart)
				return
			}
			execErr := fmt.Errorf("skill execution exceeded global timeout of %v", globalTimeout)
			r.mu.Lock()
			for j := i; j < len(skill.Steps); j++ {
				run.status.Steps[j].Status = skillStepStatusSkipped
				if j == i {
					run.status.Steps[j].Timeout = true
					run.status.Steps[j].Error = "global timeout exceeded"
				}
			}
			run.status.Error = execErr.Error()
			r.mu.Unlock()
			r.updateUsageStats(skill, execErr)
			r.finalizeRunOutcome(run, skillRunStatusFailed, execStart)
			return
		default:
		}

		condition := normalizeSkillStepConditionKind(step.Condition)
		onError := normalizeSkillStepOnErrorKind(step.OnError)

		// Handle condition: "on_failure" ? skip if no prior failure
		if condition == skillStepConditionOnFailure && !hasFailure {
			r.mu.Lock()
			run.status.Steps[i].Status = skillStepStatusSkipped
			r.mu.Unlock()
			continue
		}
		// Handle condition: "on_success" ? skip if there was a failure
		if condition == skillStepConditionOnSuccess && hasFailure {
			r.mu.Lock()
			run.status.Steps[i].Status = skillStepStatusSkipped
			r.mu.Unlock()
			continue
		}

		// api_workflow mode: skip steps not in selectedSteps (by label)
		if isAPIWorkflow && len(run.selectedSteps) > 0 && step.Label != "" {
			if !cskill.StepLabelSelected(step.Label, run.selectedSteps) {
				r.mu.Lock()
				run.status.Steps[i].Status = skillStepStatusSkipped
				r.mu.Unlock()
				log.Printf("[skill-runner] step %d/%d: skipped (label %q not in selected steps)", i+1, len(skill.Steps), step.Label)
				continue
			}
		}
		// api_workflow mode: skip unlabeled steps when step selection is active
		if isAPIWorkflow && len(run.selectedSteps) > 0 && step.Label == "" {
			r.mu.Lock()
			run.status.Steps[i].Status = skillStepStatusSkipped
			r.mu.Unlock()
			log.Printf("[skill-runner] step %d/%d: skipped (no label, step selection active)", i+1, len(skill.Steps))
			continue
		}

		// Dynamic when condition: evaluate expression with template vars.
		// Allows steps to be conditionally executed based on runtime parameters.
		if step.When != "" {
			vars := r.templateVarsForRun(run.status.RunID)
			if !cskill.EvaluateStepWhen(step.When, vars) {
				r.mu.Lock()
				run.status.Steps[i].Status = skillStepStatusSkipped
				r.mu.Unlock()
				log.Printf("[skill-runner] step %d/%d: skipped (when %q evaluated false)", i+1, len(skill.Steps), step.When)
				continue
			}
		}

		r.mu.Lock()
		run.status.Steps[i].Status = skillStepStatusRunning
		r.mu.Unlock()

		step = withSkillPreferredShell(step, skill.PreferredShell)
		resolvedStep, resolveErr := resolveSkillStep(step, r.templateVarsForRun(run.status.RunID), skill.SkillDir, skillParams)
		if resolveErr != nil {
			r.mu.Lock()
			run.status.Steps[i].Status = skillStepStatusFailed
			run.status.Steps[i].Error = resolveErr.Error()
			hasFailure = true
			execErr = resolveErr
			if onError.ShouldContinue() {
				r.mu.Unlock()
				log.Printf("[skill-runner] step %d/%d: param bind failed (on_error=%s): %v", i+1, len(skill.Steps), step.OnError, resolveErr)
				continue
			}
			run.status.Error = resolveErr.Error()
			r.mu.Unlock()
			log.Printf("[skill-runner] step %d/%d: param bind failed, aborting: %v", i+1, len(skill.Steps), resolveErr)
			break
		}
		// Propagate skill-level preferred_shell to bash steps so the shell
		// selection logic can respect it.
		resolvedStepAction := classifySkillStepAction(resolvedStep.Action)
		if resolvedStepAction.IsBash() && skill.PreferredShell != "" {
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
		if resolvedStepAction.IsCraftTool() {
			if resolvedStep.Params == nil {
				resolvedStep.Params = map[string]interface{}{}
			}
			if _, exists := resolvedStep.Params["user_prompt"]; !exists {
				vars := r.templateVarsForRun(run.status.RunID)
				if prompt := vars["user_prompt"]; prompt != "" {
					resolvedStep.Params["user_prompt"] = prompt
				}
			}
			if len(run.extraEnv) > 0 {
				cskill.MergeExtraEnvParam(resolvedStep.Params, run.extraEnv)
			}
		}
		// Poll steps run bash subprocesses internally via runBashStepWithContext,
		// which injects env from params through BuildCommandEnv (cmd.Env). Merge
		// required_env/extra_env into the poll step's params so the polled
		// subprocess receives them without the runner pinning the global
		// os.Setenv mutex for the entire (minutes-long) poll loop.
		if resolvedStepAction == skillStepActionPoll {
			if resolvedStep.Params == nil {
				resolvedStep.Params = map[string]interface{}{}
			}
			if len(skill.RequiredEnv) > 0 {
				cskill.MergeRequiredEnvParam(resolvedStep.Params, skill.RequiredEnv)
			}
			if len(run.extraEnv) > 0 {
				cskill.MergeExtraEnvParam(resolvedStep.Params, run.extraEnv)
			}
		}
		// Propagate skill-level required_env to bash steps for auto-injection.
		resolvedStep = cskill.PrepareResolvedStepEnv(resolvedStep, skill.RequiredEnv, run.extraEnv)
		restoreEnv := installSkillStepProcessEnv(resolvedStep.Action, run.extraEnv)
		log.Printf("[skill-runner] step %d/%d: action=%s command=%q", i+1, len(skill.Steps), resolvedStep.Action, resolveCommandForDisplay(resolvedStep))
		result, stepErr := func() (string, error) {
			defer restoreEnv()
			return r.executeStepWithPoll(globalCtx, run.status.RunID, resolvedStep, skill.SkillDir)
		}()
		captured := map[string]string(nil)
		if len(step.Capture) > 0 && result != "" {
			captured = captureOutputVariables(result, step.Capture)
		}
		if ctx.Err() != nil {
			r.mu.Lock()
			run.status.Steps[i].Name = resolvedStep.Name
			run.status.Steps[i].CommandResolved = resolveCommandForDisplay(resolvedStep)
			run.status.Steps[i].Status = skillStepStatusSkipped
			run.status.Steps[i].Error = ctx.Err().Error()
			for j := i + 1; j < len(skill.Steps); j++ {
				run.status.Steps[j].Status = skillStepStatusSkipped
			}
			run.status.Status = skillRunStatusCancelled
			run.status.EndedAt = time.Now().Format(time.RFC3339)
			run.status.DurationMs = time.Since(execStart).Milliseconds()
			if run.monitorCancel != nil {
				run.monitorCancel()
			}
			r.mu.Unlock()
			return
		}

		r.mu.Lock()
		if len(captured) > 0 {
			if run.templateVars == nil {
				run.templateVars = make(map[string]string)
			}
			for k, v := range captured {
				run.templateVars[k] = v
				log.Printf("[skill-runner] captured %s=%q from step %d output", k, truncateSkillRunSnippet(v), i+1)
			}
		}
		run.status.Steps[i].Name = resolvedStep.Name
		run.status.Steps[i].CommandResolved = resolveCommandForDisplay(resolvedStep)
		if stepErr != nil {
			run.status.Steps[i].Status = skillStepStatusFailed
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
			if onError != skillStepOnErrorContinue {
				run.status.Error = fmt.Sprintf("step %d (%s) failed: %s", i+1, step.Action, stepErr.Error())
				execErr = stepErr
				// Mark remaining steps as skipped.
				for j := i + 1; j < len(skill.Steps); j++ {
					run.status.Steps[j].Status = skillStepStatusSkipped
				}
				r.mu.Unlock()
				break
			}
			if execErr == nil {
				execErr = stepErr
			}
		} else {
			run.status.Steps[i].Status = skillStepStatusSuccess
			run.status.Steps[i].Output = result
			log.Printf("[skill-runner] step %d/%d OK (output %d bytes)", i+1, len(skill.Steps), len(result))
		}
		r.mu.Unlock()
	}

	r.mu.Lock()
	if ctx.Err() != nil {
		for i := range run.status.Steps {
			if run.status.Steps[i].LifecycleStatus() == skillStepStatusPending || run.status.Steps[i].LifecycleStatus() == skillStepStatusRunning {
				run.status.Steps[i].Status = skillStepStatusSkipped
				run.status.Steps[i].Error = ctx.Err().Error()
			}
		}
		if run.monitorCancel != nil {
			run.monitorCancel()
		}
		r.mu.Unlock()
		r.finalizeRunOutcome(run, skillRunStatusCancelled, execStart)
		return
	}
	finalStatus := skillRunStatusSuccess
	if hasFailure {
		finalStatus = skillRunStatusFailed
		if execErr != nil && strings.TrimSpace(run.status.Error) == "" {
			run.status.Error = execErr.Error()
		}
	}
	// Stop session monitor if running.
	if run.monitorCancel != nil {
		run.monitorCancel()
	}
	log.Printf("[skill-runner] skill %q finished: status=%s steps=%d elapsed=%s",
		skill.Name, finalStatus, len(skill.Steps), time.Since(execStart).Truncate(time.Millisecond))
	r.mu.Unlock()

	// 更新 skill 使用统计
	r.updateUsageStats(skill, execErr)

	r.finalizeRunOutcome(run, finalStatus, execStart)

	// 自动上传触发
	r.tryAutoUpload(skill, run)
}

func (r *SkillRunner) updateUsageStats(skill *corelib.NLSkillEntry, execErr error) {
	if r == nil || r.executor == nil || r.executor.app == nil || skill == nil {
		return
	}
	shouldEmit := false
	var updatedEntry *corelib.NLSkillEntry
	successfulSkillName := ""

	r.executor.mu.Lock()
	skills := r.executor.loadSkills()
	for i, s := range skills {
		if s.Name == skill.Name {
			skills[i].UsageCount++
			skills[i].LastUsedAt = time.Now().Format(time.RFC3339)
			if execErr == nil {
				skills[i].SuccessCount++
				skills[i].LastError = ""
				successfulSkillName = skills[i].Name
				// Skill succeeded after a previous repair; the fix worked.
				if skills[i].RepairAttemptCount > 0 {
					cskill.ResetRepairCount(&skills[i])
					cskill.MarkRepairVerified(&skills[i])
					log.Printf("[skill-repair-gui] skill %q succeeded after repair, reset repair count", skill.Name)
				}
			} else {
				skills[i].FailureCount++
				// Classify the error and store the LLM-formatted version
				// (includes [class: xxx] tag and action hint) so that:
				// 1. Self-repair can extract the error class via extractErrorClass()
				// 2. LLM receives actionable repair suggestions
				skills[i].LastError = formatExecErrorForStorage(execErr)
			}
			_ = r.executor.saveSkills(skills)
			log.Printf("[skill-runner] usage stats updated for %q: usage=%d success=%d failure=%d workaround=%d",
				skill.Name, skills[i].UsageCount, skills[i].SuccessCount, skills[i].FailureCount, skills[i].WorkaroundCount)
			shouldEmit = true
			// Deep copy for async self-repair (outside lock).
			if execErr != nil {
				cp := skills[i]
				cp.Steps = append([]corelib.NLSkillStep(nil), skills[i].Steps...)
				cp.RepairHistory = append([]corelib.SkillRepairRecord(nil), skills[i].RepairHistory...)
				updatedEntry = &cp
			}
			break
		}
	}
	r.executor.mu.Unlock()

	// Notify frontend to refresh skill list with updated stats (outside lock).
	if shouldEmit && r.executor.app != nil {
		r.executor.app.emitEvent("skill:usage_updated")
	}
	r.recordSkillUsageExperience(skill, execErr)

	// A successful verified run is runtime proof for previously blocked uploads.
	if successfulSkillName != "" && r.executor.app != nil {
		go func(skillName string) {
			r.executor.app.ensureSkillLifecycleManager()
			if r.executor.app.skillLifecycle == nil {
				return
			}
			if err := r.executor.app.skillLifecycle.RetryBlockedAndProcess(context.Background(), skillName, 1); err != nil {
				log.Printf("[auto-upload] blocked upload reevaluation failed for %s: %v", skillName, err)
			}
		}(successfulSkillName)
	}

	// Trigger async self-repair for failed skills (outside lock, non-blocking).
	if updatedEntry != nil {
		// Mark the run as having a pending self-repair so the LLM knows to
		// wait before retrying. The flag is set on the run status (if still
		// accessible) before launching the goroutine.
		if r.canStartRepairSkill(updatedEntry) {
			r.markSelfRepairPending(updatedEntry.Name)
			go r.maybeRepairSkill(updatedEntry)
		}
	}
}

func (r *SkillRunner) recordSkillUsageExperience(skill *corelib.NLSkillEntry, execErr error) {
	if r == nil || r.executor == nil || r.executor.app == nil || r.executor.app.usageTracker == nil || skill == nil {
		return
	}
	text := strings.TrimSpace(skill.Name + " " + skill.Description + " " + strings.Join(skill.Triggers, " "))
	tokens := bm25.Tokenize(text)
	if len(tokens) > 5 {
		tokens = tokens[:5]
	}
	success := execErr == nil
	finalOutcome := "completed"
	errorClass := ""
	followUp := "continue"
	if !success {
		finalOutcome = "failed"
		followUp = "abandon"
		errorClass = cskill.ExtractErrorClass(formatExecErrorForStorage(execErr))
	}
	r.executor.app.usageTracker.RecordExperience(coretool.ToolExperience{
		ToolName:     "skill:" + skill.Name,
		QueryTokens:  tokens,
		Success:      success,
		FollowUp:     followUp,
		TaskType:     "skill_execution",
		ErrorClass:   errorClass,
		FinalOutcome: finalOutcome,
	})
}

// markSelfRepairPending sets SelfRepairPending=true on the most recent run
// for the given skill name. This tells the LLM (via appendSkillRunSummary)
// that a repair is in progress and it should wait before retrying.
func (r *SkillRunner) markSelfRepairPending(skillName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, run := range r.runs {
		if run.status.Skill == skillName && run.status.IsFailed() {
			run.status.SelfRepairPending = true
			break
		}
	}
}

func (r *SkillRunner) canStartRepairSkill(entry *corelib.NLSkillEntry) bool {
	return cskill.ShouldAttemptRepair(entry) && r.buildSkillRepairer() != nil
}

// maybeRepairSkill checks if a skill is eligible for LLM-driven self-repair
// and attempts it in the background. The entry must be a deep copy; this
// method runs in a goroutine and must not hold any locks.
func (r *SkillRunner) maybeRepairSkill(entry *corelib.NLSkillEntry) {
	if !cskill.ShouldAttemptRepair(entry) {
		return
	}

	repairer := r.buildSkillRepairer()
	if repairer == nil {
		return
	}

	log.Printf("[skill-repair-gui] attempting repair for %q (attempt %d, usage=%d, success=%d)",
		entry.Name, entry.RepairAttemptCount+1, entry.UsageCount, entry.SuccessCount)

	result, err := cskill.AttemptRepair(repairer, entry)
	if err != nil {
		log.Printf("[skill-repair-gui] repair failed for %q: %v", entry.Name, err)
		return
	}

	originalSteps := cloneSkillSteps(entry.Steps)
	if !cskill.ApplyRepair(entry, result) {
		log.Printf("[skill-repair-gui] repair not applied for %q: repaired=%v should_disable=%v",
			entry.Name, result.Repaired, result.ShouldDisable)
		// If should_disable, persist the status change.
		if result.ShouldDisable {
			if err := r.persistRepairResult(entry); err != nil {
				log.Printf("[skill-repair-gui] persist disabled repair result for %q failed: %v", entry.Name, err)
			}
		}
		return
	}

	repairReport := r.scanRepairedSkill(entry)
	app := (*App)(nil)
	if r.executor != nil {
		app = r.executor.app
	}
	missingScanBlocked := repairReport == nil && (app == nil || app.skillInstallMissingScanShouldBlock())
	riskyScanBlocked := repairReport != nil && app != nil && app.skillInstallScanShouldBlock(repairReport)
	legacyRiskyScanBlocked := repairReport != nil && app == nil && repairReport.NeedsUserReview()
	if missingScanBlocked || riskyScanBlocked || legacyRiskyScanBlocked {
		markRepairBlockedBySecurity(entry, originalSteps, repairReport)
		log.Printf("[skill-repair-gui] blocked repaired skill %q by security scan: %s", entry.Name, entry.LastError)
		if err := r.persistRepairResult(entry); err != nil {
			log.Printf("[skill-repair-gui] persist blocked repair result for %q failed: %v", entry.Name, err)
		}
		if app != nil {
			app.logSkillInstallSecurityEvent(
				security.AuditActionHubSkillReject,
				"skill_auto_repair",
				repairScanRiskLevel(repairReport),
				security.PolicyDeny,
				fmt.Sprintf("auto-repair rejected for skill %s: %s", entry.Name, entry.LastError),
			)
		}
		return
	}
	if app != nil && repairReport != nil && repairReport.NeedsUserReview() {
		app.logSkillInstallSecurityEvent(
			security.AuditActionHubSkillUpdate,
			"skill_auto_repair",
			repairReport.FinalLevel,
			security.PolicyAudit,
			fmt.Sprintf("auto-repair allowed for skill %s by current policy: %s", entry.Name, repairReport.Summary),
		)
	}

	// Persist the repaired steps back to config.
	if err := r.persistRepairResult(entry); err != nil {
		log.Printf("[skill-repair-gui] repaired skill %q passed scan but failed to persist: %v", entry.Name, err)
		return
	}
	if err := writeSkillScanCacheForInstalledEntry(entry, repairReport); err != nil {
		log.Printf("[skill-repair-gui] repaired skill %q persisted but scan cache write failed: %v", entry.Name, err)
		return
	}
	r.refreshSkillIndexesAfterMutation(entry.Name)
	log.Printf("[skill-repair-gui] repaired skill %q: %s", entry.Name, result.Explanation)

	// Store repair notification for LLM context injection.
	r.recentRepairs.Store(entry.Name, result.Explanation)

	// Notify frontend and re-check any quality-blocked upload for this skill.
	if r.executor.app != nil {
		r.executor.app.emitEvent("skill:repaired")
		go func(skillName string) {
			r.executor.app.ensureSkillLifecycleManager()
			if r.executor.app.skillLifecycle == nil {
				return
			}
			if err := r.executor.app.skillLifecycle.RetryBlockedAndProcess(context.Background(), skillName, 1); err != nil {
				log.Printf("[skill-repair-gui] blocked upload reevaluation failed for %s: %v", skillName, err)
			}
		}(entry.Name)
	}
}

func (r *SkillRunner) refreshSkillIndexesAfterMutation(skillName string) {
	if r == nil || r.executor == nil {
		return
	}
	r.executor.invalidateSkillCache()
	if r.executor.app != nil {
		if r.executor.app.toolRouter != nil {
			r.executor.app.toolRouter.RefreshSkillIndex()
		}
		r.executor.app.emitEvent("skill:index_refreshed", map[string]string{"skill": skillName})
	}
}

func markRepairBlockedBySecurity(entry *corelib.NLSkillEntry, originalSteps []corelib.NLSkillStep, report *cskill.ScanReport) {
	if entry == nil {
		return
	}
	entry.Steps = cloneSkillSteps(originalSteps)
	entry.Status = "needs_review"
	entry.LastError = "auto-repair blocked by security scan: scan unavailable"
	if report != nil {
		entry.LastError = fmt.Sprintf("auto-repair blocked by security scan: level=%s summary=%s", report.FinalLevel, report.Summary)
	}

	now := time.Now().Format(time.RFC3339)
	if entry.LastRepairAt == "" {
		entry.LastRepairAt = now
	}
	blocked := corelib.SkillRepairRecord{
		Timestamp:   entry.LastRepairAt,
		ErrorClass:  "security_scan_blocked",
		Explanation: entry.LastError,
		Success:     false,
	}
	if len(entry.RepairHistory) == 0 {
		entry.RepairHistory = append(entry.RepairHistory, blocked)
		return
	}
	entry.RepairHistory[len(entry.RepairHistory)-1] = blocked
}

func repairScanRiskLevel(report *cskill.ScanReport) security.RiskLevel {
	if report == nil {
		return security.RiskCritical
	}
	return report.FinalLevel
}

func (r *SkillRunner) scanRepairedSkill(entry *corelib.NLSkillEntry) *cskill.ScanReport {
	if entry == nil {
		return nil
	}
	scanner := cskill.NewSecurityScanner(nil)
	return scanner.ScanInstallStaged(context.Background(), entry, entry.SkillDir, func(status string) {
		if r != nil && r.executor != nil && r.executor.app != nil {
			r.executor.app.log(fmt.Sprintf("[skill-repair-gui] security scan %s: %s", entry.Name, status))
		}
	})
}

// persistRepairResult writes the repaired skill entry back to the config and,
// for file-backed skills, to the authoritative skill.yaml definition.
func (r *SkillRunner) persistRepairResult(entry *corelib.NLSkillEntry) error {
	if r == nil || r.executor == nil || entry == nil {
		return nil
	}
	var yamlErr error
	if strings.TrimSpace(entry.SkillDir) != "" {
		if err := writeSkillYAMLForEntry(entry.SkillDir, entry); err != nil {
			yamlErr = err
			log.Printf("[skill-repair-gui] persist repaired skill.yaml for %q failed: %v", entry.Name, err)
		}
	}

	r.executor.mu.Lock()
	defer r.executor.mu.Unlock()

	skills := r.executor.loadSkills()
	for i, s := range skills {
		if s.Name == entry.Name {
			skills[i].Steps = entry.Steps
			skills[i].Status = entry.Status
			skills[i].LastError = entry.LastError
			skills[i].RepairAttemptCount = entry.RepairAttemptCount
			skills[i].LastRepairAt = entry.LastRepairAt
			skills[i].RepairHistory = entry.RepairHistory
			if err := r.executor.saveSkills(skills); err != nil {
				return err
			}
			return yamlErr
		}
	}
	return yamlErr
}

// ConsumeRepairNotifications returns and clears all pending repair
// notifications. Called by the system prompt builder to inject repair
// context into the LLM's next turn. Each entry is consumed exactly once.
func (r *SkillRunner) ConsumeRepairNotifications() map[string]string {
	result := make(map[string]string)
	r.recentRepairs.Range(func(key, value interface{}) bool {
		name, _ := key.(string)
		explanation, _ := value.(string)
		if name != "" {
			result[name] = explanation
		}
		r.recentRepairs.Delete(key)
		return true
	})
	return result
}

// guiSkillRepairer adapts the GUI's LLM calling to skill.LLMRepairer.
type guiSkillRepairer struct {
	cfg    corelib.MaclawLLMConfig
	client *http.Client
}

func (r *guiSkillRepairer) ChatCall(messages []map[string]string) (string, error) {
	ifaces := make([]interface{}, len(messages))
	for i, m := range messages {
		ifaces[i] = m
	}
	resp, err := doSimpleLLMRequest(context.Background(), r.cfg, ifaces, r.client, 60*time.Second)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func (r *guiSkillRepairer) IsConfigured() bool {
	return r.cfg.URL != "" && (r.cfg.Key != "" || r.cfg.Model != "")
}

// buildSkillRepairer creates an LLMRepairer from the current app config.
// Returns nil if LLM is not configured.
func (r *SkillRunner) buildSkillRepairer() cskill.LLMRepairer {
	if r.executor == nil || r.executor.app == nil {
		return nil
	}
	cfg := r.executor.app.GetMaclawLLMConfig()
	repairer := &guiSkillRepairer{
		cfg:    cfg,
		client: &http.Client{Timeout: 60 * time.Second},
	}
	if !repairer.IsConfigured() {
		return nil
	}
	return repairer
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
			switch normalizeSkillOutcomeStatus(outcome) {
			case skillOutcomeStatusSuccess:
				skills[i].SuccessCount++
				skills[i].LastError = ""
			case skillOutcomeStatusFailure:
				skills[i].FailureCount++
				if lastError != "" {
					skills[i].LastError = lastError
				}
			case skillOutcomeStatusWorkaround:
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

var skillStepProcessEnvMu sync.Mutex

func installSkillStepProcessEnv(action string, extraEnv map[string]string) func() {
	if !classifySkillStepAction(action).UsesManagedProcessEnv() || len(extraEnv) == 0 {
		return func() {}
	}
	skillStepProcessEnvMu.Lock()
	restores := make([]func(), 0, len(extraEnv))
	for k, v := range extraEnv {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		prev, hadPrev := os.LookupEnv(key)
		_ = os.Setenv(key, v)
		capturedKey, capturedPrev, capturedHad := key, prev, hadPrev
		if capturedHad {
			restores = append(restores, func() { _ = os.Setenv(capturedKey, capturedPrev) })
		} else {
			restores = append(restores, func() { _ = os.Unsetenv(capturedKey) })
		}
	}
	if len(restores) == 0 {
		skillStepProcessEnvMu.Unlock()
		return func() {}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			for i := len(restores) - 1; i >= 0; i-- {
				restores[i]()
			}
			skillStepProcessEnvMu.Unlock()
		})
	}
}

// tryAutoUpload attempts to upload to SkillMarket after a skill run finishes.
func (r *SkillRunner) tryAutoUpload(skill *corelib.NLSkillEntry, run *skillRun) {
	if r.uploadTrigger == nil || r.executor == nil || r.executor.app == nil {
		return
	}
	if skill.SkillDir == "" {
		return
	}

	r.mu.RLock()
	status := run.status.LifecycleStatus()
	hasErr := false
	for _, st := range run.status.Steps {
		if st.IsFailed() {
			hasErr = true
			break
		}
	}
	r.mu.RUnlock()

	result := &SkillExecutionResult{Success: status == skillRunStatusSuccess, HasError: hasErr, OutputQuality: "basic"}
	if status == skillRunStatusSuccess && !hasErr {
		result.OutputQuality = "good"
	}

	localHash := skillDirHash(skill.SkillDir)
	score := EvaluateSkillExecution(result)
	r.uploadTrigger.RecordExecution(skill.Name, score, localHash)
	if status != skillRunStatusSuccess || hasErr || score < 1 {
		return
	}

	r.executor.app.ensureSkillLifecycleManager()
	if r.executor.app.skillLifecycle == nil {
		log.Printf("[auto-upload] lifecycle manager unavailable for %s", skill.Name)
		return
	}
	if _, err := r.executor.app.skillLifecycle.EnqueueUpload(context.Background(), skill.Name, skill.SkillDir, "auto_upload", true, true); err != nil {
		log.Printf("[auto-upload] enqueue failed for %s: %v", skill.Name, err)
		return
	}
}

// skillDirHash computes a compact hash for skill directory changes.
func skillDirHash(dir string) string {
	h := sha256.New()
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		base := filepath.Base(path)
		if info.IsDir() {
			if isSkillRuntimePackageDir(base) {
				return filepath.SkipDir
			}
			return nil
		}
		if isSkillRuntimePackageFile(base) {
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

// -- Step execution with context ------------------------------------------------

func (r *SkillRunner) executeStepWithContext(ctx context.Context, runID string, step corelib.NLSkillStep, skillDir string) (string, error) {
	if err := cskill.EnsureStepActionSupported(cskill.RunnerBackendGUI, step.Action); err != nil {
		return "", err
	}
	step.Action = cskill.NormalizeStepActionName(step.Action)
	switch classifySkillStepAction(step.Action) {
	case skillStepActionCreateSession:
		return "", fmt.Errorf("external coding sessions are disabled; coding tasks must run through CodingSubAgent")

	case skillStepActionSendInput:
		return "", fmt.Errorf("external coding-session input steps are disabled; coding tasks must run through CodingSubAgent")

	case skillStepActionSendAndObserve:
		return "", fmt.Errorf("external coding-session observe steps are disabled; coding tasks must run through CodingSubAgent")

	case skillStepActionCallMCPTool:
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

	case skillStepActionBash:
		command, _ := step.Params["command"].(string)
		if command == "" {
			return "", fmt.Errorf("missing command parameter")
		}
		return runBashStepWithContext(ctx, command, step.Params, skillDir, r.executor.app)

	case skillStepActionCraftTool:
		if r.executor == nil || r.executor.app == nil {
			return "", fmt.Errorf("app not initialized")
		}
		return executeCraftToolCoreWithContext(ctx, r.executor.app, nil, step.Params, nil)

	case skillStepActionPoll:
		return r.executePollStep(ctx, step, skillDir)

	default:
		return "", fmt.Errorf("unknown action: %s", step.Action)
	}
}

// -- Bash step execution with context and skillDir working dir ------------------

func mergeRequiredEnvParam(params map[string]interface{}, required []string) {
	cskill.MergeRequiredEnvParam(params, required)
}

func mergeExtraEnvParam(params map[string]interface{}, extraEnv map[string]string) {
	cskill.MergeExtraEnvParam(params, extraEnv)
}

func skillParamSeconds(params map[string]interface{}, key string) (int, bool) {
	return cskill.SkillParamSeconds(params, key)
}
func resolveSkillWorkingDir(workDir, skillDir string) string {
	workDir = strings.TrimSpace(workDir)
	skillDir = strings.TrimSpace(skillDir)
	if workDir == "" {
		return skillDir
	}
	workDir = strings.TrimPrefix(workDir, "{baseDir}/")
	workDir = strings.TrimPrefix(workDir, "{baseDir}\\")
	if !filepath.IsAbs(workDir) && skillDir != "" {
		workDir = filepath.Join(skillDir, filepath.FromSlash(filepath.ToSlash(workDir)))
	}
	return filepath.Clean(workDir)
}
func runBashStepWithContext(ctx context.Context, command string, params map[string]interface{}, skillDir string, app *App) (string, error) {
	return runBashStepWithContextFull(ctx, command, params, skillDir, app)
}

func runBashStepWithContextFull(ctx context.Context, command string, params map[string]interface{}, skillDir string, app *App) (string, error) {
	// Strip UTF-8 BOM if present. SKILL.md files saved with BOM can leak
	// the BOM bytes into the command string, causing cmd.exe to fail with
	// "'@echo" is not recognized as an internal or external command.
	command = strings.TrimPrefix(command, "\xef\xbb\xbf")

	timeout := cskill.RunnerStepTimeoutSeconds(params, corelib.DefaultAgentTimeoutSec, corelib.MaxAgentTimeoutSec)

	// Expand portable home placeholders before Windows shell dispatch.
	// AutoFixPortability intentionally writes $HOME to keep skill.yaml portable,
	// but cmd.exe does not expand POSIX-style variables at runtime.
	if runtime.GOOS == "windows" {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			command = expandPortableHomeVars(command, home)
		}
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
	workDir = resolveSkillWorkingDir(workDir, skillDir)
	// BUG-001: Also normalize the working directory path
	if runtime.GOOS == "windows" && workDir != "" {
		workDir = normalizeWindowsShortPathGUI(workDir)
	}
	if workDir != "" {
		if info, err := os.Stat(workDir); err != nil || !info.IsDir() {
			if skillDir != "" {
				if skillInfo, skillErr := os.Stat(skillDir); skillErr == nil && skillInfo.IsDir() {
					log.Printf("[skill-runner] working_dir %q is unavailable; falling back to skill_dir %q", workDir, skillDir)
					workDir = skillDir
				}
			}
		}
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
		// Explicit preferred_shell metadata wins; otherwise detect bash-only syntax.
		preferredShell, _ := params["preferred_shell"].(string)
		preferredShell = strings.ToLower(strings.TrimSpace(preferredShell))
		useBash := needsBashShell(command)
		usePowerShell := false
		shellReason := "default (cmd.exe)"
		if useBash {
			shellReason = "detected Unix-specific syntax in command"
		}
		switch preferredShell {
		case "bash", "sh", "zsh":
			useBash = true
			usePowerShell = false
			shellReason = "skill metadata preferred_shell=bash"
		case "powershell", "pwsh", "ps", "ps1":
			useBash = false
			usePowerShell = true
			shellReason = "skill metadata preferred_shell=powershell"
		case "cmd", "cmd.exe", "windows", "win_cmd":
			useBash = false
			usePowerShell = false
			shellReason = "skill metadata preferred_shell=cmd"
		}
		if useBash {
			if app != nil {
				if shPath, err := app.findSh(); err == nil {
					shellName = shPath
					log.Printf("[skill-runner] shell selection: %s -> reason: %s", filepath.Base(shPath), shellReason)
				} else {
					return "", fmt.Errorf("missing Unix shell for bash step\n%v\nplease install Git for Windows: https://git-scm.com/download/win", err)
				}
			} else {
				if shPath, err := exec.LookPath("sh.exe"); err == nil {
					// Skip WSL bash on Windows (runtime check only)
					shellName = shPath
				} else {
					return "", fmt.Errorf("missing Unix shell for bash step and app is nil")
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
		} else if usePowerShell {
			if psPath, err := exec.LookPath("pwsh.exe"); err == nil {
				shellName = psPath
			} else if psPath, err := exec.LookPath("powershell.exe"); err == nil {
				shellName = psPath
			} else {
				return "", fmt.Errorf("PowerShell not found for skill step execution")
			}
			log.Printf("[skill-runner] shell selection: %s -> reason: %s", filepath.Base(shellName), shellReason)
			scriptFile, err := os.CreateTemp("", "skill-step-*.ps1")
			if err != nil {
				return "", fmt.Errorf("create temp script failed: %v", err)
			}
			tmpScript = scriptFile.Name()
			scriptContent := command + "\n"
			if _, err := scriptFile.WriteString(scriptContent); err != nil {
				scriptFile.Close()
				os.Remove(tmpScript)
				return "", fmt.Errorf("write temp script failed: %v", err)
			}
			scriptFile.Close()
			shellArgs = []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", tmpScript}
			log.Printf("[skill-runner] bash step: using temp powershell script %s", tmpScript)
		} else {
			// Direct command: use cmd.exe with a temp .cmd script.
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
			log.Printf("[skill-runner] shell selection: cmd.exe -> reason: %s", shellReason)
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
			// "'@echo" is not recognized as an internal or external command.
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

	cmd := exec.Command(shellName, shellArgs...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	// Force UTF-8 encoding for subprocess I/O on Windows to prevent
	// GBK/CP936 mojibake when scripts output non-ASCII text.
	cmd.Env = cskill.BuildCommandEnv(coretool.AppendUTF8Env(os.Environ()), params)
	hideCommandWindow(cmd)
	coretool.PrepareCommandForTreeKill(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	log.Printf("[skill-runner] bash exec: shell=%s workDir=%s timeout=%ds", filepath.Base(shellName), workDir, timeout)
	err := cmd.Start()
	if err == nil {
		err = coretool.WaitCommandWithContext(stepCtx, cmd)
	}
	elapsed := time.Since(startTime)

	// Sanitize invalid UTF-8 sequences (e.g. GBK remnants from cmd.exe on
	// Chinese Windows) so garbled replacement characters don't leak to the UI.
	sanitizeUTF8Buffer(&stdout)
	sanitizeUTF8Buffer(&stderr)

	isTimeout := stepCtx.Err() == context.DeadlineExceeded

	var b strings.Builder
	b.WriteString(fmt.Sprintf("shell: %s\n", filepath.Base(shellName)))
	b.WriteString(fmt.Sprintf("elapsed: %s\n", elapsed.Round(time.Millisecond)))
	b.WriteString(fmt.Sprintf("📂 %s\n", workDir))
	if tmpScript != "" {
		// Show original command instead of temp script path for readability
		b.WriteString(fmt.Sprintf("command: %s (via script)\n", command))
	} else {
		b.WriteString(fmt.Sprintf("command: %s %s\n", filepath.Base(shellName), strings.Join(shellArgs, " ")))
	}
	b.WriteString("───────────────\n")
	if stdout.Len() > 0 {
		out := truncateRunesMarker(stdout.String(), 8192, "\n... (truncated)")
		b.WriteString(out)
	}
	if stderr.Len() > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		errOut := truncateRunesMarker(stderr.String(), 4096, "\n... (truncated)")
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
			stderrText = truncateRunesMarker(stderrText, 2048, "...")
			errMsg = fmt.Sprintf("%s | stderr: %s", errMsg, stderrText)
		}
		stdoutText := strings.TrimSpace(stdout.String())
		if stdoutText != "" && stderrText == "" {
			stdoutText = truncateRunesMarker(stdoutText, 2048, "...")
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

func (e *bashStepError) ExitCode() int   { return e.exitCode }
func (e *bashStepError) IsTimeout() bool { return e.isTimeout }
func (e *bashStepError) Stdout() string  { return e.stdout }
func (e *bashStepError) Stderr() string  { return e.stderr }

// classifyBashError adds context to error messages by detecting common
// failure patterns. Delegates to the unified error classifier in
// corelib/skill/error_classifier.go is the single source of truth for all
// error patterns across GUI, TUI, and self-repair.
func expandPortableHomeVars(command, home string) string {
	command = strings.TrimPrefix(command, "\xef\xbb\xbf")
	home = filepath.ToSlash(strings.TrimSpace(home))
	if command == "" || home == "" {
		return command
	}
	for _, replacement := range []struct {
		from string
		to   string
	}{
		{from: "${HOME}/", to: home + "/"},
		{from: "${HOME}\\", to: home + "/"},
		{from: "${HOME}", to: home},
	} {
		command = strings.ReplaceAll(command, replacement.from, replacement.to)
	}
	var b strings.Builder
	for i := 0; i < len(command); {
		if strings.HasPrefix(command[i:], "$HOME") && isPortableHomeBoundary(command, i+len("$HOME")) {
			b.WriteString(home)
			if i+len("$HOME") < len(command) && (command[i+len("$HOME")] == '/' || command[i+len("$HOME")] == '\\') {
				b.WriteByte('/')
				i += len("$HOME") + 1
			} else {
				i += len("$HOME")
			}
			continue
		}
		b.WriteByte(command[i])
		i++
	}
	return b.String()
}

func isPortableHomeBoundary(command string, idx int) bool {
	if idx >= len(command) {
		return true
	}
	switch command[idx] {
	case '/', '\\', ' ', '\t', '\r', '\n', '"', '\'', ';', '&', '|', ')', '<', '>':
		return true
	default:
		return false
	}
}
func classifyBashError(errMsg, command string, exitCode int) string {
	result := cskill.ClassifyStepError(exitCode, "", errMsg, command)
	return result.UserMessage
}

// classifyBashErrorFull returns the full ClassifiedError for callers that
// need the error class and metadata (e.g., self-repair trigger).
func classifyBashErrorFull(errMsg, output, command string, exitCode int) cskill.ClassifiedError {
	return cskill.ClassifyStepError(exitCode, output, errMsg, command)
}

// formatExecErrorForStorage classifies an execution error and formats it
// with [class: xxx] tag and action hint for storage in LastError. This
// enables self-repair to extract the error class and LLM to receive
// actionable suggestions.
func formatExecErrorForStorage(execErr error) string {
	if execErr == nil {
		return ""
	}
	if formatted := strings.TrimSpace(execErr.Error()); strings.Contains(formatted, "[class:") {
		return cskill.TruncateFormattedErrorForStorage(formatted, 2000)
	}
	if bErr, ok := execErr.(*bashStepError); ok {
		ce := cskill.ClassifyStepError(bErr.ExitCode(), bErr.Stdout()+"\n"+bErr.Stderr(), bErr.Error(), "")
		return cskill.TruncateFormattedErrorForStorage(cskill.FormatErrorForLLM(ce), 2000)
	}
	// Non-bash errors: classify with zero exit code and error message only.
	ce := cskill.ClassifyStepError(0, "", execErr.Error(), "")
	return cskill.TruncateFormattedErrorForStorage(cskill.FormatErrorForLLM(ce), 2000)
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

	// Check for Unix shell builtins FIRST. These must use bash even if the
	// command also contains .py/.js paths (e.g. "export FOO=bar && python x.py").
	if strings.HasPrefix(lower, "export ") || strings.HasPrefix(lower, "source ") ||
		strings.HasPrefix(lower, "#!/") {
		log.Printf("[skill-runner] shell detection: found Unix shell builtin (export/source/shebang), needs bash")
		return true
	}
	// Multi-line commands containing export lines or # comment lines.
	// On Windows, cmd.exe treats # as a command, not a comment, so any
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
	// Direct script path invocation: cmd.exe handles this well.
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
func resolveCommandForDisplay(step corelib.NLSkillStep) string {
	cmd, _ := step.Params["command"].(string)
	return cmd
}

// -- Platform compatibility -----------------------------------------------------

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
// WindowsApps that opens the Store instead of running Python. We detect
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
func migrateLegacyCceasyPaths(skill *corelib.NLSkillEntry) {
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

// cceasyMigrationPaths returns the old/new directory pair if .cceasy->.maclaw
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

// loadSkillDocContent reads the SKILL.md documentation content from a skill
// directory. Returns the content string if found, empty string otherwise.
// Used as a fallback for documentation-only skills that have no executable steps.
func loadSkillDocContent(skillDir string) string {
	mdPath := findSkillMarkdownDocPath(skillDir)
	if mdPath == "" {
		return ""
	}
	data, err := os.ReadFile(mdPath)
	if err != nil {
		return ""
	}
	content := strings.TrimSpace(string(data))
	if content != "" {
		return content
	}
	return ""
}

// hasSkillDocFile checks whether a SKILL.md (or equivalent) exists in the
// skill directory without reading its content. Used by List() to populate
// HasDocumentation efficiently avoids file IO on every frontend refresh.
func hasSkillDocFile(skillDir string) bool {
	return findSkillMarkdownDocPath(skillDir) != ""
}

// ── poll / when / operation helpers ──────────────────────────────────────

// executeStepWithPoll wraps executeStepWithContext with optional poll loop.
// When step.Poll is configured, the step is re-executed at intervals until
// the output matches the termination condition or max attempts are exhausted.
func (r *SkillRunner) executeStepWithPoll(ctx context.Context, runID string, step corelib.NLSkillStep, skillDir string) (string, error) {
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
			// Fail loudly: a typo'd until_match must not silently degrade
			// polling into a single-shot execution (the step would appear to
			// "succeed" without ever waiting for its termination condition).
			return "", fmt.Errorf("poll: invalid until_match regex %q: %w", poll.UntilMatch, err)
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
		// No match condition configured; single execution is enough.
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

// substituteSkillVarsInString replaces runner placeholders in s with values
// from vars. Kept as a GUI-local wrapper for older tests/callers.
func substituteSkillVarsInString(s string, vars map[string]string) string {
	return cskill.SubstituteVarsInString(s, vars)
}

// evaluateSimpleCondition evaluates a simple condition expression.
// Supported forms:
//   - "value == expected"  -> true if equal (trimmed)
//   - "value != expected"  -> true if not equal
//   - "value contains sub" -> true if value contains sub
//   - bare non-empty string -> true
//   - empty string -> false
func evaluateSimpleCondition(expr string) bool {
	return cskill.EvaluateSimpleCondition(expr)
}

// ── api_workflow helpers ─────────────────────────────────────────────────

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
	return cskill.CaptureOutputVariables(output, captures)
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
func (r *SkillRunner) executePollStep(ctx context.Context, step corelib.NLSkillStep, skillDir string) (string, error) {
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
	if v, ok := skillParamSeconds(step.Params, "interval_seconds"); ok && v > 0 {
		interval = time.Duration(v) * time.Second
	}
	timeout := 180 * time.Second
	if v, ok := skillParamSeconds(step.Params, "timeout_seconds"); ok && v > 0 {
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

		log.Printf("[skill-runner] poll: attempt %d: no match yet (err=%v, output=%d bytes)",
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
