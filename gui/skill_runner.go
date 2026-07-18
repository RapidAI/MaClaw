package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/llm"
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
	CurrentStepIndex          int                   `json:"current_step_index,omitempty"`
	CurrentStep               string                `json:"current_step,omitempty"`
	CurrentStepStatus         skillStepStatus       `json:"current_step_status,omitempty"`
	LastCompletedStep         string                `json:"last_completed_step,omitempty"`
	LastCompletedStepIndex    int                   `json:"last_completed_step_index,omitempty"`
	LastOutputSnippet         string                `json:"last_output_snippet,omitempty"`
	LastErrorSnippet          string                `json:"last_error_snippet,omitempty"`
	HasSessionBinding         bool                  `json:"has_session_binding,omitempty"`
	NeedsArtifactVerification bool                  `json:"needs_artifact_verification,omitempty"`
	ArtifactPath              string                `json:"artifact_path,omitempty"`
	ArtifactStatus            skillArtifactStatus   `json:"artifact_status,omitempty"`
	Artifacts                 []SkillRunArtifact    `json:"artifacts,omitempty"`
	OutputBlocks              []SkillRunOutputBlock `json:"output_blocks,omitempty"`
}

// SkillRunArtifact is the normalized file output contract exposed to UI.
type SkillRunArtifact struct {
	ID            string              `json:"id"`
	URI           string              `json:"uri,omitempty"`
	Name          string              `json:"name,omitempty"`
	Path          string              `json:"path,omitempty"`
	MimeType      string              `json:"mime_type,omitempty"`
	SizeBytes     int64               `json:"size_bytes,omitempty"`
	RemoteURL     string              `json:"remote_url,omitempty"`
	Checksum      string              `json:"checksum,omitempty"`
	DownloadState string              `json:"download_state,omitempty"`
	Status        skillArtifactStatus `json:"status,omitempty"`
	Presentation  string              `json:"presentation,omitempty"`
}

// SkillRunOutputBlock is the normalized UI-facing output model for skill runs.
type SkillRunOutputBlock struct {
	ID         string            `json:"id"`
	Kind       string            `json:"kind"`
	Title      string            `json:"title,omitempty"`
	Text       string            `json:"text,omitempty"`
	Status     string            `json:"status,omitempty"`
	ArtifactID string            `json:"artifact_id,omitempty"`
	Artifact   *SkillRunArtifact `json:"artifact,omitempty"`
}

// SkillRunStatus represents one skill execution.
type SkillRunStatus struct {
	RunID             string                  `json:"run_id"`
	Skill             string                  `json:"skill"`
	OwnerID           string                  `json:"owner_id,omitempty"`
	Status            skillRunLifecycleStatus `json:"status"`
	Steps             []StepResult            `json:"steps"`
	Session           *SkillRunSessionMeta    `json:"session,omitempty"`
	SessionProgress   *SessionProgressInfo    `json:"session_progress,omitempty"`
	Summary           SkillRunSummary         `json:"summary,omitempty"`
	Outputs           []SkillRunOutputBlock   `json:"outputs,omitempty"`
	Artifacts         []SkillRunArtifact      `json:"artifacts,omitempty"`
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
	activeRuns    atomic.Int64

	// evolutionPipeline is the async background self-evolution engine.
	// Receives notifications after each skill execution; handles optimization,
	// nudge promotion, and upload triggering independently of the main agent loop.
	evolutionPipeline *cskill.EvolutionPipeline

	// outcomeReporter reports local execution outcomes to HubCenter,
	// closing the feedback loop between local quality and global ranking.
	outcomeReporter *skillOutcomeReporter

	// recentRepairs tracks skills that were recently auto-repaired.
	// Consumed by the system prompt builder to notify the LLM about
	// repaired skills so it can adjust its calling strategy.
	// Key: skill name, Value: repair explanation.
	// Entries are consumed (deleted) after being injected into the prompt.
	recentRepairs sync.Map

	// repairingSkills tracks skill names currently undergoing self-repair.
	// StartRunForOwner checks this before starting a new run to prevent
	// executing a skill whose definition is being actively modified by the
	// repair goroutine. Key: skill name (string), Value: time.Time (start).
	repairingSkills sync.Map

	// prepProgressByOwner stores per-owner progress callbacks for reporting
	// dependency installation during PrepareRunnerExecution. Set by toolRunSkill
	// before calling StartRunForOwner, cleared after. Using sync.Map (keyed by
	// policyOwnerID) ensures concurrent runs from different owners don't
	// interfere with each other's progress callbacks.
	prepProgressByOwner sync.Map // map[string]cskill.FixProgressCallback
}

func (r *SkillRunner) beginRunExecution(run *skillRun, kind string) (time.Time, func(string)) {
	startedAt := time.Now()
	active := int64(0)
	if r != nil {
		active = r.activeRuns.Add(1)
	}
	runID, ownerID, skillName := "", "", ""
	if run != nil {
		runID = run.status.RunID
		ownerID = run.status.OwnerID
		skillName = run.status.Skill
	}
	log.Printf("[skill-runner] exec_start kind=%s run=%s owner=%q skill=%q active=%d", strings.TrimSpace(kind), runID, ownerID, skillName, active)
	return startedAt, func(status string) {
		remaining := int64(0)
		if r != nil {
			remaining = r.activeRuns.Add(-1)
			if remaining < 0 {
				r.activeRuns.Store(0)
				remaining = 0
			}
		}
		log.Printf("[skill-runner] exec_done kind=%s run=%s owner=%q skill=%q status=%q elapsed=%s active=%d", strings.TrimSpace(kind), runID, ownerID, skillName, strings.TrimSpace(status), time.Since(startedAt).Round(time.Millisecond), remaining)
	}
}

type skillRun struct {
	status        SkillRunStatus
	cancel        context.CancelFunc
	monitorCancel context.CancelFunc // cancels the session monitor goroutine
	templateVars  map[string]string
	runArgs       map[string]interface{}
	selectedSteps []string            // api_workflow mode: only run steps with these labels
	extraEnv      map[string]string   // env vars from run_skill caller, injected into subprocesses
	workspaceDir  string              // isolated per-run copy of the skill directory
	timeoutSec    int                 // normalized system Skill Runner timeout captured when the run starts
	liveOutput    *skillRunLiveOutput // real-time subprocess output tail (last N lines)
}

// skillRunLiveOutput is a goroutine-safe ring buffer that captures the last N
// lines of subprocess stdout/stderr in real time. The skill runner writes to it
// during bash step execution; GetRunStatus reads from it during polling.
type skillRunLiveOutput struct {
	mu    sync.Mutex
	lines []string // ring buffer, max skillRunLiveOutputMaxLines
	total int      // total lines seen (for progress estimation)
}

const skillRunLiveOutputMaxLines = 20

func newSkillRunLiveOutput() *skillRunLiveOutput {
	return &skillRunLiveOutput{lines: make([]string, 0, skillRunLiveOutputMaxLines)}
}

// Append adds a line to the ring buffer (goroutine-safe).
func (lo *skillRunLiveOutput) Append(line string) {
	lo.mu.Lock()
	defer lo.mu.Unlock()
	lo.total++
	if len(lo.lines) >= skillRunLiveOutputMaxLines {
		// shift left
		copy(lo.lines, lo.lines[1:])
		lo.lines[len(lo.lines)-1] = line
	} else {
		lo.lines = append(lo.lines, line)
	}
}

// LastLines returns a copy of the last N lines (goroutine-safe).
func (lo *skillRunLiveOutput) LastLines(n int) []string {
	lo.mu.Lock()
	defer lo.mu.Unlock()
	if n <= 0 || len(lo.lines) == 0 {
		return nil
	}
	start := len(lo.lines) - n
	if start < 0 {
		start = 0
	}
	result := make([]string, len(lo.lines)-start)
	copy(result, lo.lines[start:])
	return result
}

// Snippet returns the last line as a short progress string (goroutine-safe).
// Truncated to 200 runes for UI display.
func (lo *skillRunLiveOutput) Snippet() string {
	lo.mu.Lock()
	defer lo.mu.Unlock()
	if len(lo.lines) == 0 {
		return ""
	}
	line := lo.lines[len(lo.lines)-1]
	runes := []rune(line)
	if len(runes) > 200 {
		return string(runes[:200]) + "..."
	}
	return line
}

// Total returns the total number of lines seen.
func (lo *skillRunLiveOutput) Total() int {
	lo.mu.Lock()
	defer lo.mu.Unlock()
	return lo.total
}

// NewSkillRunner creates a SkillRunner.
func NewSkillRunner(executor *SkillExecutor) *SkillRunner {
	r := &SkillRunner{
		executor: executor,
		runs:     make(map[string]*skillRun),
	}
	if executor != nil && executor.app != nil {
		r.outcomeReporter = newSkillOutcomeReporter(executor.app)
	}
	return r
}

func (r *SkillRunner) defaultTimeoutSec() int {
	if r != nil && r.executor != nil && r.executor.app != nil {
		if cfg, err := r.executor.app.LoadConfig(); err == nil {
			return corelib.NormalizeSkillRunnerTimeoutSec(cfg.SkillRunnerTimeoutSec)
		}
	}
	return corelib.DefaultSkillRunnerTimeoutSec
}

func (r *SkillRunner) dataDir() string {
	if r != nil && r.executor != nil && r.executor.app != nil {
		return r.executor.app.GetDataDir()
	}
	return ""
}

func (r *SkillRunner) runDefaultTimeoutSec(run *skillRun) int {
	if run != nil && run.timeoutSec > 0 {
		return corelib.NormalizeSkillRunnerTimeoutSec(run.timeoutSec)
	}
	return r.defaultTimeoutSec()
}

const maclawAppRunTimeoutSec = corelib.MaxSkillRunnerTimeoutSec

func (r *SkillRunner) effectiveSkillGlobalTimeoutSec(run *skillRun, skill *corelib.NLSkillEntry) int {
	timeout := r.runDefaultTimeoutSec(run)
	if isMaclawAppSkillRun(run) && timeout < maclawAppRunTimeoutSec {
		timeout = maclawAppRunTimeoutSec
	}
	if run != nil {
		if requested, ok := cskill.SkillParamSeconds(run.runArgs, "global_timeout"); ok && requested > timeout {
			timeout = requested
		}
	}
	if skill != nil && skill.GlobalTimeout > timeout {
		timeout = skill.GlobalTimeout
	}
	return corelib.NormalizeSkillRunnerTimeoutSec(timeout)
}

func (r *SkillRunner) applyEffectiveSkillGlobalTimeoutSec(run *skillRun, skill *corelib.NLSkillEntry) int {
	timeout := r.effectiveSkillGlobalTimeoutSec(run, skill)
	if run != nil {
		r.mu.Lock()
		run.timeoutSec = timeout
		r.mu.Unlock()
	}
	return timeout
}

func isMaclawAppSkillRun(run *skillRun) bool {
	if run == nil || len(run.runArgs) == 0 {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(fmt.Sprint(firstNonNilRunArg(run.runArgs, "_maclaw_app"))), "true") {
		return true
	}
	if strings.TrimSpace(fmt.Sprint(firstNonNilRunArg(run.runArgs, "app_id", "app_name", "app_kind"))) != "" {
		return true
	}
	return false
}

func firstNonNilRunArg(runArgs map[string]interface{}, keys ...string) interface{} {
	for _, key := range keys {
		if value, ok := runArgs[key]; ok && value != nil {
			return value
		}
	}
	return ""
}

func (r *SkillRunner) runDefaultTimeoutSecForID(runID string) int {
	if r == nil {
		return corelib.DefaultSkillRunnerTimeoutSec
	}
	r.mu.RLock()
	run := r.runs[runID]
	r.mu.RUnlock()
	return r.runDefaultTimeoutSec(run)
}

// resolveLoadedSkillForRun picks an installed skill for run_skill / app execution.
//
// Resolution order:
//  1. Stable IDs only (SkillID / HubSkillID / Publisher:Name) via MatchesQualifiedID
//  2. If none, loose MatchesName (display Name / DirName / path basename)
//  3. Ambiguous multi-match → error (stable hits preferred when filtering)
//
// Returns (nil, nil) when no candidate matches so the caller can attach fuzzy suggestions.
func resolveLoadedSkillForRun(skillName string, loadedSkills []corelib.NLSkillEntry) (*corelib.NLSkillEntry, error) {
	skillName = strings.TrimSpace(skillName)
	if skillName == "" || len(loadedSkills) == 0 {
		return nil, nil
	}

	pickOne := func(hits []corelib.NLSkillEntry) (*corelib.NLSkillEntry, error) {
		if len(hits) == 0 {
			return nil, nil
		}
		if len(hits) == 1 {
			cp := hits[0]
			return &cp, nil
		}
		var qualifiedNames []string
		for _, s := range hits {
			if s.Publisher != "" {
				qualifiedNames = append(qualifiedNames, s.Publisher+":"+s.Name)
			} else if hubID := strings.TrimSpace(s.HubSkillID); hubID != "" {
				qualifiedNames = append(qualifiedNames, hubID+" ("+s.Name+")")
			} else {
				qualifiedNames = append(qualifiedNames, s.Name+" (local)")
			}
		}
		return nil, fmt.Errorf("skill name %q is ambiguous — multiple skills match:\n  %s\nPlease use the qualified name (publisher:name) to disambiguate",
			skillName, strings.Join(qualifiedNames, "\n  "))
	}

	// Pass 1: stable identity (covers PDF app → hub_skill_id paper_pdf_translator).
	var idHits []corelib.NLSkillEntry
	for _, s := range loadedSkills {
		if s.MatchesQualifiedID(skillName) {
			idHits = append(idHits, s)
		}
	}
	if target, err := pickOne(idHits); target != nil || err != nil {
		return target, err
	}

	// Pass 2: loose display / dir name (only when no stable-ID hit).
	var looseHits []corelib.NLSkillEntry
	for _, s := range loadedSkills {
		// Skip entries already considered by pass 1 (none matched query as ID).
		if s.MatchesName(skillName) {
			looseHits = append(looseHits, s)
		}
	}
	return pickOne(looseHits)
}

// StartRun starts a skill asynchronously and returns a run ID for polling.
func (r *SkillRunner) StartRun(skillName string, runArgs map[string]interface{}) (string, error) {
	return r.StartRunForOwner(r.defaultSkillRunPolicyOwnerID(), skillName, runArgs)
}

// StartRunForOwner starts a skill run under an explicit workflow policy owner.
func (r *SkillRunner) StartRunForOwner(policyOwnerID, skillName string, runArgs map[string]interface{}) (runID string, retErr error) {
	startedAt := time.Now()
	policyOwnerID = strings.TrimSpace(policyOwnerID)
	log.Printf("[skill-runner] start_run requested owner=%q skill=%q args=%d", policyOwnerID, skillName, len(runArgs))
	defer func() {
		if retErr != nil {
			logSkillRunnerFailure("", policyOwnerID, skillName, "start_run", retErr.Error())
		}
	}()
	if r == nil || r.executor == nil {
		return "", fmt.Errorf("skill runner not initialized")
	}
	if r.executor.app != nil {
		policyStart := time.Now()
		if err := r.executor.app.ensureWorkflowAllowsRemoteToolCallForOwner(policyOwnerID, "manage_skill", map[string]interface{}{"action": "run", "name": skillName, "args": runArgs}); err != nil {
			log.Printf("[skill-runner] start_run policy_denied owner=%q skill=%q elapsed=%s err=%v", policyOwnerID, skillName, time.Since(policyStart).Round(time.Millisecond), err)
			return "", err
		}
		if elapsed := time.Since(policyStart); elapsed > 100*time.Millisecond {
			log.Printf("[skill-runner] start_run policy_check owner=%q skill=%q elapsed=%s", policyOwnerID, skillName, elapsed.Round(time.Millisecond))
		}
	}
	// Match by name regardless of status so we can provide specific error
	// messages for disabled/needs_setup skills. Do not take SkillExecutor.mu
	// here: loadSkills is protected by config/cache locks, and holding the
	// executor read lock makes new agent runs wait behind unrelated usage-stat
	// writes from other agent instances.
	loadStart := time.Now()
	loadedSkills := r.executor.loadSkills()
	if elapsed := time.Since(loadStart); elapsed > 100*time.Millisecond {
		log.Printf("[skill-runner] start_run load_skills owner=%q skill=%q count=%d elapsed=%s", policyOwnerID, skillName, len(loadedSkills), elapsed.Round(time.Millisecond))
	}
	target, resolveErr := resolveLoadedSkillForRun(skillName, loadedSkills)
	if resolveErr != nil {
		return "", resolveErr
	}
	if target == nil {
		// Fuzzy match fallback: suggest only — never auto-run disk-scanned names
		// that are not admitted into the config registry.
		if similar, score := cskill.FindSimilarSkill(skillName, 0.3); similar != nil {
			suggest := similar.Name
			if hubID := strings.TrimSpace(similar.HubSkillID); hubID != "" && !strings.EqualFold(hubID, suggest) {
				suggest = fmt.Sprintf("%s (hub_skill_id=%s)", suggest, hubID)
			}
			return "", fmt.Errorf("skill %q not found. Did you mean %q? (%.0f%% match)\nUse list_skills to see installed skills",
				skillName, suggest, score*100)
		}
		return "", fmt.Errorf("skill %q not found. Use list_skills to see installed skills", skillName)
	}

	// BUG-005: Normalize skill directory path (resolve 8.3 short paths on Windows)
	if runtime.GOOS == "windows" && target.SkillDir != "" {
		target.SkillDir = normalizeWindowsShortPathGUI(target.SkillDir)
	}
	configuredType := strings.TrimSpace(target.Type)
	if err := refreshSkillRunDefinitionFromDir(target); err != nil {
		return "", fmt.Errorf("reload skill %q from disk: %w", skillName, err)
	}
	if strings.TrimSpace(target.Type) == "" {
		target.Type = configuredType
	}

	// Bug #3: Distinguish needs_setup / disabled / needs_review from active
	switch normalizeSkillEntryStatus(target.Status) {
	case skillEntryStatusNeedsSetup:
		return "", fmt.Errorf("skill %q needs setup. Installation was incomplete (missing dependencies or files). Please check the skill directory (%s) and complete configuration", skillName, target.SkillDir)
	case skillEntryStatusDisabled:
		return "", fmt.Errorf("skill %q is disabled. Please enable it first", skillName)
	case skillEntryStatusNeedsReview:
		hint := skillRunBlockedDownloadHint(target)
		errMsg := fmt.Sprintf("skill %q is needs_review and cannot run", skillName)
		if last := strings.TrimSpace(target.LastError); last != "" {
			errMsg += ": " + last
		}
		if hint != "" {
			errMsg += " " + hint
		} else {
			errMsg += ". Prefer download_file / web_fetch(save_path=...) for simple HTTP downloads, or fix/re-enable the skill."
		}
		return "", fmt.Errorf("%s", errMsg)
	case skillEntryStatusActive, skillEntryStatusUnknown:
	default:
		return "", fmt.Errorf("skill %q status is %q, expected active", skillName, target.Status)
	}

	// Preflight: refuse to start skills that still have zero GUI-supported steps
	// after normalization (prevents install→run loops on broken Hub packages).
	if compat := cskill.AssessRunnerCompatibility(target, cskill.RunnerBackendGUI); !compat.Runnable && len(target.Steps) > 0 {
		hint := skillRunBlockedDownloadHint(target)
		msg := fmt.Sprintf("skill %q is not runnable on GUI runner: %s", skillName, cskill.FormatRunnerCompatReport(compat))
		if hint != "" {
			msg += " " + hint
		}
		return "", fmt.Errorf("%s", msg)
	}

	// Guard: reject runs for skills currently undergoing self-repair.
	// The repair goroutine modifies skill steps and persists to disk; running
	// a skill during repair may execute partially-updated definitions or
	// conflict with the repair's sandbox verification.
	if startedAt, repairing := r.repairingSkills.Load(target.Name); repairing {
		repairStart, _ := startedAt.(time.Time)
		if time.Since(repairStart) < 5*time.Minute {
			return "", fmt.Errorf("skill %q is currently being auto-repaired (started %s ago). Please retry in a moment [action: retry]",
				skillName, time.Since(repairStart).Round(time.Second))
		}
		// Stale repair marker (>5 min) — the repair goroutine likely crashed
		// or timed out. Clear it and proceed.
		r.repairingSkills.Delete(target.Name)
	}
	// Migrate legacy .cceasy paths to .maclaw ? crafted skills from older
	// versions may reference scripts in the old directory structure.
	migrateLegacyCceasyPaths(target)
	if err := cskill.HydrateRunMetadataFromDir(target); err != nil {
		log.Printf("[skill-runner] hydrate skill metadata from %q failed: %v", target.SkillDir, err)
	}

	// Normalize community/imported skill shapes before pre-checks and execution.
	cskill.NormalizeSkillForRunner(target)
	if isShellBrowserAutomationSkillEntry(*target) {
		return "", browserAutomationSkillRejectedError(skillName)
	}

	if cskill.IsKnowledgeSkillType(target.Type) {
		return "", fmt.Errorf("%s", cskill.FormatNoExecutableStepsMessage(skillName, target, cskill.RunnerBackendGUI))
	}

	templateVars := normalizeSkillRunVars(runArgs)
	extraEnv := cskill.ExtractRunExtraEnvFromArgs(runArgs)
	// Inject the owner's project/workbench directory so skills can write
	// downloads and artifacts under the user-configured workdir instead of %TEMP%.
	extraEnv = r.injectOwnerWorkDirEnv(policyOwnerID, extraEnv)

	// Mechanism: For contractless skills (no declared params, no required_args,
	// no {{placeholders}}), fold LLM-provided args into the "input" carrier key
	// so documentation/craft_tool fallbacks receive the same semantic context.
	if cskill.IsContractlessSkill(target) {
		cskill.FoldUnconsumedArgsToInput(templateVars, target.Params)
	}

	if cskill.IsPipelineSkill(target) {
		return r.startPipelineRun(policyOwnerID, skillName, target, runArgs, templateVars, extraEnv)
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

	// Pre-flight: validate OpenAI proxy availability early (synchronous path)
	// so the LLM receives an immediate actionable error instead of discovering
	// the failure asynchronously after polling run status.
	if r.executor != nil && r.executor.app != nil {
		// Use the same step selection that PrepareRunnerExecution will compute,
		// to avoid false positives for api_workflow skills where only some
		// operations need the proxy.
		selectedForProbe, _ := cskill.ResolveSelectedStepLabels(target, runArgs)
		probeSteps := cskill.PrecheckExecutableSteps(
			cskill.SelectedExecutableSteps(target.Steps, selectedForProbe), templateVars)
		if corelib.NeedsOpenAIProxyAuto(target.RequiredEnv, extraEnv, probeSteps, target.SkillDir) {
			llmCfg := r.executor.app.GetMaclawLLMConfig()
			proxyCfg := corelib.OpenAIProxyConfig{
				URL:      llmCfg.URL,
				Key:      llmCfg.Key,
				Model:    llmCfg.Model,
				Protocol: llmCfg.Protocol,
				WireAPI:  llmCfg.WireAPI,
				AuthType: llmCfg.AuthType,
			}
			if err := corelib.ValidateOpenAIProxyUpstreamConfig(proxyCfg); err != nil {
				return "", fmt.Errorf("skill %q requires OpenAI-compatible API access, but %s [action: configure_llm]",
					skillName, err)
			}
		}
	}

	// Shared runner preparation handles step selection, parameter completion,
	// requirements, implicit placeholders, and local file diagnostics.
	prepStart := time.Now()
	var prepProgress cskill.FixProgressCallback
	if cb, ok := r.prepProgressByOwner.Load(policyOwnerID); ok {
		prepProgress, _ = cb.(cskill.FixProgressCallback)
	}
	prep, err := cskill.PrepareRunnerExecutionWithProgressWithDataDir(r.dataDir(), target, templateVars, runArgs, extraEnv, cskill.RunnerBackendGUI, prepProgress)
	if err != nil {
		log.Printf("[skill-runner] start_run prepare_failed owner=%q skill=%q elapsed=%s err=%v", policyOwnerID, skillName, time.Since(prepStart).Round(time.Millisecond), err)
		// Detailed diagnostic dump for AI debugging tools
		log.Printf("[skill-runner-diag] === PREPARE FAILURE DIAGNOSTIC ===")
		log.Printf("[skill-runner-diag] skill=%q skill_dir=%s", skillName, target.SkillDir)
		log.Printf("[skill-runner-diag] template_vars=%v", templateVars)
		log.Printf("[skill-runner-diag] run_args_keys=%v", mapKeysAny(runArgs))
		log.Printf("[skill-runner-diag] extra_env_keys=%v", mapKeys(extraEnv))
		log.Printf("[skill-runner-diag] steps_count=%d required_args=%v params_count=%d", len(target.Steps), target.RequiredArgs, len(target.Params))
		if len(target.Steps) > 0 {
			for si, s := range target.Steps {
				cmd, _ := s.Params["command"].(string)
				log.Printf("[skill-runner-diag] step[%d] action=%s command=%s", si, s.Action, truncateRunesMarker(cmd, 300, "..."))
			}
		}
		log.Printf("[skill-runner-diag] error=%v", err)
		log.Printf("[skill-runner-diag] === END DIAGNOSTIC ===")
		return "", err
	}
	if elapsed := time.Since(prepStart); elapsed > 100*time.Millisecond {
		log.Printf("[skill-runner] start_run prepare owner=%q skill=%q elapsed=%s", policyOwnerID, skillName, elapsed.Round(time.Millisecond))
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
	defaultTimeoutSec := r.defaultTimeoutSec()

	// 生成 runID
	runnerLockWaitStart := time.Now()
	r.mu.Lock()
	if waited := time.Since(runnerLockWaitStart); waited > 100*time.Millisecond {
		log.Printf("[skill-runner] start_run runner_lock_wait owner=%q skill=%q waited=%s", policyOwnerID, skillName, waited.Round(time.Millisecond))
	}
	r.counter++
	runID = fmt.Sprintf("run-%d-%d", time.Now().UnixMilli(), r.counter)

	ctx, cancel := context.WithCancel(context.Background())
	run := &skillRun{
		status: SkillRunStatus{
			RunID:          runID,
			Skill:          skillName,
			OwnerID:        strings.TrimSpace(policyOwnerID),
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
		runArgs:       cloneSkillRunArgs(runArgs),
		selectedSteps: selectedSteps,
		extraEnv:      extraEnv,
		timeoutSec:    defaultTimeoutSec,
		liveOutput:    newSkillRunLiveOutput(),
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
	log.Printf("[skill-runner] start_run accepted owner=%q skill=%q run=%s steps=%d total=%s", policyOwnerID, skillName, runID, len(target.Steps), time.Since(startedAt).Round(time.Millisecond))

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

func refreshSkillRunDefinitionFromDir(target *corelib.NLSkillEntry) error {
	if target == nil || strings.TrimSpace(target.SkillDir) == "" {
		return nil
	}
	// Always attempt to reload from disk if the directory exists.
	// The previous check (importedSkillDefinitionExists) was overly conservative:
	// it rejected legacy formats (SKILL.md + _meta.json) and didn't handle
	// renamed files. This caused stale step definitions to persist in the
	// runner cache, losing runtime placeholders like {{input}}.
	if _, err := os.Stat(target.SkillDir); err != nil {
		return nil
	}
	reloaded, err := loadImportedSkillEntry(target.SkillDir)
	if err != nil {
		// If reload fails, fall back to the existing target silently.
		// This preserves backward compat for config-only skills whose
		// directory was deleted but config entry remains.
		log.Printf("[skill-runner] refresh_from_dir: reload %q failed (using cached): %v", target.SkillDir, err)
		return nil
	}
	oldStepCount := len(target.Steps)
	overlayStatus := target.Status
	lastError := target.LastError
	mergeSkillPackagingRuntimeFields(reloaded, target)
	if fileSkillStatusIsOverlay(overlayStatus) {
		reloaded.Status = strings.TrimSpace(overlayStatus)
	}
	if strings.TrimSpace(lastError) != "" {
		reloaded.LastError = lastError
	}
	reloaded.Source = firstNonEmptySkillString(target.Source, reloaded.Source)
	reloaded.SourceProject = firstNonEmptySkillString(target.SourceProject, reloaded.SourceProject)
	reloaded.HubSkillID = firstNonEmptySkillString(target.HubSkillID, reloaded.HubSkillID)
	reloaded.HubVersion = firstNonEmptySkillString(target.HubVersion, reloaded.HubVersion)
	reloaded.TrustLevel = firstNonEmptySkillString(target.TrustLevel, reloaded.TrustLevel)
	reloaded.Publisher = firstNonEmptySkillString(target.Publisher, reloaded.Publisher)
	reloaded.DirName = firstNonEmptySkillString(reloaded.DirName, target.DirName)
	reloaded.CreatedAt = firstNonEmptySkillString(target.CreatedAt, reloaded.CreatedAt)
	*target = *reloaded
	if len(target.Steps) != oldStepCount {
		log.Printf("[skill-runner] refresh_from_dir: %q steps changed (%d -> %d)", target.Name, oldStepCount, len(target.Steps))
	}
	return nil
}

func firstNonEmptySkillString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// startPipelineRun starts a pipeline skill asynchronously.
func (r *SkillRunner) startPipelineRun(policyOwnerID, skillName string, target *corelib.NLSkillEntry, runArgs map[string]interface{}, templateVars map[string]string, extraEnv map[string]string) (string, error) {
	if target == nil {
		return "", fmt.Errorf("skill entry is nil")
	}
	if len(target.Pipeline) == 0 {
		return "", fmt.Errorf("%s", cskill.FormatNoExecutableStepsMessage(skillName, target, cskill.RunnerBackendGUI))
	}
	if templateVars == nil {
		templateVars = map[string]string{}
	}
	prep, err := cskill.PreparePipelineRunnerExecutionWithDataDir(r.dataDir(), target, templateVars, runArgs, extraEnv, cskill.RunnerBackendGUI)
	if err != nil {
		return "", err
	}
	for _, warning := range prep.RequirementWarnings {
		log.Printf("[skill-runner] pipeline requirement warning for %q: %s", skillName, warning.Message)
	}
	warnings := prep.Warnings
	defaultTimeoutSec := r.defaultTimeoutSec()

	r.mu.Lock()
	r.counter++
	runID := fmt.Sprintf("run-%d-%d", time.Now().UnixMilli(), r.counter)
	ctx, cancel := context.WithCancel(context.Background())
	run := &skillRun{
		status: SkillRunStatus{
			RunID:          runID,
			Skill:          skillName,
			OwnerID:        strings.TrimSpace(policyOwnerID),
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
		timeoutSec:   defaultTimeoutSec,
		liveOutput:   newSkillRunLiveOutput(),
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
	execStart, finishExecution := r.beginRunExecution(run, "pipeline")
	finishStatus := "unknown"
	defer func() { finishExecution(finishStatus) }()
	globalTimeout := time.Duration(r.applyEffectiveSkillGlobalTimeoutSec(run, entry)) * time.Second
	globalCtx, cancel := context.WithTimeout(ctx, globalTimeout)
	defer cancel()

	baseArgs := cloneSkillRunArgs(run.runArgs)
	if len(baseArgs) == 0 {
		baseArgs = map[string]interface{}{}
	}
	if len(run.extraEnv) > 0 {
		baseArgs["extra_env"] = run.extraEnv
	}
	if ownerID := strings.TrimSpace(run.status.OwnerID); ownerID != "" {
		baseArgs["_skill_owner_id"] = ownerID
	}
	baseRunArgs := cskill.WithPipelineRunStack(baseArgs, entry.Name)
	pr := &cskill.PipelineRunner{Executor: skillExecutorPipelineExecutor{exec: r.executor, baseRunArgs: baseRunArgs, ownerID: run.status.OwnerID}}
	result, err := pr.Run(globalCtx, entry.Pipeline, run.templateVars)
	if err == nil && result == nil {
		err = fmt.Errorf("pipeline returned no result")
	}

	var execErr error
	r.mu.Lock()
	if err != nil {
		execErr = err
		finishStatus = skillRunStatusFailed.String()
		for i := range run.status.Steps {
			if run.status.Steps[i].LifecycleStatus() == skillStepStatusPending {
				run.status.Steps[i].Status = skillStepStatusSkipped
			}
		}
		run.status.Error = err.Error()
		r.mu.Unlock()
		r.finalizeRunOutcome(run, skillRunStatusFailed, execStart)
		r.updateUsageStats(entry, execErr)
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
		if strings.TrimSpace(run.status.Error) == "" {
			run.status.Error = execErr.Error()
		}
	}
	r.mu.Unlock()
	finishStatus = finalStatus.String()
	r.finalizeRunOutcome(run, finalStatus, execStart)
	if finalStatus != skillRunStatusCancelled {
		r.updateUsageStats(entry, execErr)
	}

	// Pipeline skills also participate in the outcome reporting + auto-upload loop.
	r.tryAutoUpload(entry, run)
}

func (r *SkillRunner) GetRunStatus(runID string) (*SkillRunStatus, error) {
	startedAt := time.Now()
	lockWaitStart := time.Now()
	r.mu.RLock()
	if waited := time.Since(lockWaitStart); waited > 100*time.Millisecond {
		log.Printf("[skill-runner] get_status lock_wait run=%s waited=%s", runID, waited.Round(time.Millisecond))
	}
	run, ok := r.runs[runID]
	if !ok {
		r.mu.RUnlock()
		return nil, fmt.Errorf("run %q not found", runID)
	}
	cp := snapshotRunStatus(&run.status)
	liveOut := run.liveOutput
	r.mu.RUnlock()

	r.hydrateRunSessionMeta(&cp)
	summarizeSkillRun(&cp)

	// Inject live subprocess output into the status snapshot AFTER summarize
	// (summarizeSkillRun resets Summary, so injecting before would be wiped).
	// This gives the polling frontend real-time visibility into what the skill
	// is doing without waiting for step completion.
	if liveOut != nil && cp.Status == skillRunStatusRunning {
		if snippet := liveOut.Snippet(); snippet != "" && cp.Summary.LastOutputSnippet == "" {
			cp.Summary.LastOutputSnippet = snippet
		}
		if lines := liveOut.LastLines(10); len(lines) > 0 {
			for i := range cp.Steps {
				if cp.Steps[i].Status == skillStepStatusRunning {
					cp.Steps[i].StdoutLastLines = lines
					break
				}
			}
		}
	}
	if elapsed := time.Since(startedAt); elapsed > 100*time.Millisecond {
		log.Printf("[skill-runner] get_status slow run=%s owner=%q skill=%q status=%q elapsed=%s", runID, cp.OwnerID, cp.Skill, cp.Status.String(), elapsed.Round(time.Millisecond))
	}
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
	// Fast path: cheap read-lock size check avoids write-lock contention
	// when no pruning is needed (the common case for most users).
	r.mu.RLock()
	needsPrune := len(r.runs) > maxKeep
	r.mu.RUnlock()
	if !needsPrune {
		return
	}

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
	detectedPath := detectArtifactPathFromStatus(status)
	if artifactPath == "" {
		if detectedPath != "" && (artifactExpected || artifactExists(detectedPath)) {
			artifactPath = detectedPath
		}
	} else if !status.IsRunning() && !artifactExists(artifactPath) && detectedPath != "" && artifactExists(detectedPath) {
		artifactPath = detectedPath
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
	populateSkillRunOutputProtocol(status)
}

func populateSkillRunOutputProtocol(status *SkillRunStatus) {
	if status == nil {
		return
	}
	artifacts := make([]SkillRunArtifact, 0, 1)
	blocks := make([]SkillRunOutputBlock, 0, 2)
	if artifactPath := strings.TrimSpace(status.Summary.ArtifactPath); artifactPath != "" {
		artifact := buildSkillRunArtifact(status.RunID, "artifact-1", artifactPath, status.Summary.ArtifactStatus)
		artifacts = append(artifacts, artifact)
		blocks = append(blocks, SkillRunOutputBlock{
			ID:         "artifact-1",
			Kind:       "artifact",
			Title:      artifact.Name,
			Status:     artifact.Status.String(),
			ArtifactID: artifact.ID,
			Artifact:   &artifact,
		})
	}
	if text := strings.TrimSpace(status.Summary.LastOutputSnippet); text != "" {
		blocks = append(blocks, SkillRunOutputBlock{ID: "text-1", Kind: "text", Title: "Output", Text: text, Status: status.Status.String()})
	}
	if text := strings.TrimSpace(status.Summary.LastErrorSnippet); text != "" {
		blocks = append(blocks, SkillRunOutputBlock{ID: "error-1", Kind: "error", Title: "Error", Text: text, Status: status.Status.String()})
	}
	status.Artifacts = artifacts
	status.Outputs = blocks
	status.Summary.Artifacts = artifacts
	status.Summary.OutputBlocks = blocks
}

func resolveSkillRunArtifactPath(status *SkillRunStatus, artifactRef string) (string, error) {
	if status == nil {
		return "", fmt.Errorf("skill run status is nil")
	}
	artifactID := skillRunArtifactIDFromRef(artifactRef)
	artifacts := status.Artifacts
	if len(artifacts) == 0 {
		artifacts = status.Summary.Artifacts
	}
	for _, artifact := range artifacts {
		if artifactID != "" && artifact.ID != artifactID {
			continue
		}
		path := strings.TrimSpace(artifact.Path)
		if path == "" {
			continue
		}
		return path, nil
	}
	if artifactID == "" && strings.TrimSpace(status.Summary.ArtifactPath) != "" {
		return strings.TrimSpace(status.Summary.ArtifactPath), nil
	}
	return "", fmt.Errorf("artifact %q not found", artifactID)
}

func buildSkillRunArtifact(runID, id, artifactPath string, status skillArtifactStatus) SkillRunArtifact {
	artifactPath = strings.TrimSpace(artifactPath)
	id = strings.TrimSpace(id)
	if id == "" {
		id = "artifact-1"
	}
	artifact := SkillRunArtifact{
		ID:            id,
		URI:           skillRunArtifactURI(runID, id),
		Name:          filepath.Base(artifactPath),
		Path:          artifactPath,
		DownloadState: "downloaded",
		Status:        status,
		Presentation:  skillRunArtifactPresentation(artifactPath),
	}
	if ext := strings.ToLower(filepath.Ext(artifactPath)); ext != "" {
		artifact.MimeType = mime.TypeByExtension(ext)
	}
	if info, err := os.Stat(artifactPath); err == nil && !info.IsDir() {
		artifact.SizeBytes = info.Size()
		if artifact.Status == "" {
			artifact.Status = skillArtifactStatusVerified
		}
	}
	return artifact
}

func skillRunArtifactURI(runID, artifactID string) string {
	runID = strings.TrimSpace(runID)
	artifactID = strings.TrimSpace(artifactID)
	if runID == "" || artifactID == "" {
		return ""
	}
	return "artifact://skill-run/" + url.PathEscape(runID) + "/" + url.PathEscape(artifactID)
}

func skillRunArtifactIDFromRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(ref), "artifact://") {
		parsed, err := url.Parse(ref)
		if err == nil && parsed.Scheme == "artifact" && parsed.Host == "skill-run" {
			parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
			if len(parts) >= 2 {
				return strings.TrimSpace(parts[len(parts)-1])
			}
		}
	}
	return ref
}

func skillRunArtifactPresentation(path string) string {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case ".pdf", ".png", ".jpg", ".jpeg", ".webp", ".gif", ".txt", ".md", ".html", ".htm", ".csv", ".json":
		return "preview_or_file"
	default:
		return "file"
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
		if strings.HasPrefix(line, "脚本路径:") {
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
	for _, label := range []string{"output:", "artifact:", "file:", "path:"} {
		if strings.HasPrefix(strings.ToLower(trimmed), label) {
			candidate := strings.Trim(strings.TrimSpace(trimmed[len(label):]), "`\"' ,.;:()[]{}")
			if looksLikeArtifactPath(candidate) && filepath.IsAbs(candidate) {
				return candidate
			}
		}
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

// resolveSkillStepWithWarnings is like resolveSkillStep but also returns
// parameter binding warnings (e.g. "参数 'foo' 未被 Skill 声明"). Used by
// executeAsync to propagate bind warnings into the run status for LLM visibility.
func resolveSkillStepWithWarnings(step corelib.NLSkillStep, vars map[string]string, skillDir string, params []corelib.NLSkillParam) (corelib.NLSkillStep, []string, error) {
	result, err := cskill.ResolveStep(step, vars, skillDir, params, quoteSkillInputForStep(step))
	if err != nil {
		return step, nil, err
	}
	return result.Step, result.BindResult.Warnings, nil
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

// mapKeysAny returns the keys of an interface{} map for diagnostic logging.
func mapKeysAny(m map[string]interface{}) []string {
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
	if craftInstructionText(params) == "" {
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
	return strings.Contains(output, "脚本语言:") && strings.Contains(output, "脚本路径:")
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
	if r == nil || run == nil {
		return
	}
	if status == skillRunStatusSuccess || status == skillRunStatusFailed {
		r.materializeStdoutToExpectedOutput(run)
	}
	r.mu.Lock()
	if status == skillRunStatusFailed && runHasVerifiedArtifactLocked(&run.status) {
		if strings.TrimSpace(run.status.Error) != "" {
			run.status.Warnings = append(run.status.Warnings, "execution reported an error after producing the expected artifact: "+run.status.Error)
		}
		run.status.Error = ""
		status = skillRunStatusSuccess
	}
	run.status.Status = status
	run.status.EndedAt = time.Now().Format(time.RFC3339)
	run.status.DurationMs = time.Since(execStart).Milliseconds()
	statusSnapshot := snapshotRunStatus(&run.status)
	r.mu.Unlock()
	if status == skillRunStatusFailed {
		logSkillRunnerFailure(statusSnapshot.RunID, statusSnapshot.OwnerID, statusSnapshot.Skill, "execution", skillRunFailureReason(statusSnapshot))
	}

	if r.executor == nil || r.executor.app == nil {
		return
	}
	if err := r.executor.app.cleanupStagedSkillAppInputFilesFromRunArgs(run.runArgs); err != nil {
		r.mu.Lock()
		run.status.Warnings = append(run.status.Warnings, err.Error())
		r.mu.Unlock()
	}

	// Prune old finished runs to prevent unbounded memory growth.
	// Each skillRun holds step outputs, templateVars, runArgs, liveOutput.
	// Without pruning, hundreds of runs accumulate over a long session.
	r.CleanupFinished(50)
}

func runHasVerifiedArtifactLocked(status *SkillRunStatus) bool {
	if status == nil {
		return false
	}
	expectedOutput := strings.TrimSpace(status.ExpectedOutput)
	if expectedOutput != "" && artifactExists(expectedOutput) {
		return true
	}
	detectedPath := detectArtifactPathFromStatus(status)
	return detectedPath != "" && artifactExists(detectedPath)
}

// materializeStdoutToExpectedOutput saves step stdout to the ExpectedOutput
// file path when the skill didn't create the file itself. This bridges the gap
// between stdout-only skills and the App panel's file-based artifact display.
func (r *SkillRunner) materializeStdoutToExpectedOutput(run *skillRun) {
	if run == nil {
		return
	}
	r.mu.RLock()
	expectedOutput := strings.TrimSpace(run.status.ExpectedOutput)
	steps := run.status.Steps
	r.mu.RUnlock()

	if expectedOutput == "" {
		return
	}
	// If the file already exists, the skill wrote it — nothing to do.
	if artifactExists(expectedOutput) {
		return
	}

	// Strategy 1: Scan ALL successful steps for JSON output containing a file
	// path matching the expected extension. Multi-step skills often produce the
	// artifact in an intermediate step (e.g. step 1 generates the file, step 2
	// logs a confirmation message). Scanning only the last step would miss it.
	expectedExt := filepath.Ext(expectedOutput)
	for i := len(steps) - 1; i >= 0; i-- {
		step := steps[i]
		if step.LifecycleStatus() != skillStepStatusSuccess {
			continue
		}
		output := strings.TrimSpace(step.Output)
		if output == "" {
			continue
		}
		if r.materializeArtifactFileFromStepOutput(expectedOutput, output) {
			return
		}
	}

	// Strategy 2: Fall back to last successful step's text content for
	// stdout-only skills (e.g. OCR output → .txt file).
	var content string
	for i := len(steps) - 1; i >= 0; i-- {
		step := steps[i]
		if step.LifecycleStatus() != skillStepStatusSuccess {
			continue
		}
		output := strings.TrimSpace(step.Output)
		if output == "" {
			continue
		}
		cleaned := stripSkillRunnerMetadataFromOutput(output)
		if cleaned != "" {
			content = cleaned
			break
		}
	}
	if content == "" {
		return
	}

	if !canMaterializePlainStdoutToOutput(expectedOutput) {
		log.Printf("[skill-runner] materialize stdout: skip plain stdout for non-text expected output path=%q", expectedOutput)
		return
	}

	// If the expected output format is plain text (.txt) but the content looks
	// like a JSON response with a "text" field, extract the text value.
	// This handles OCR-type skills that output structured JSON to stdout while
	// the user selected "TXT" output format expecting readable text.
	if strings.ToLower(expectedExt) == ".txt" {
		if extracted := extractTextFieldFromJSON(content); extracted != "" {
			content = extracted
		} else if looksLikeJSONObject(content) {
			// Content is valid JSON but has no usable "text" field (e.g. OCR
			// returned {"ok":true,"text":"","lines":[]} for a blank image).
			// Don't write raw JSON to a .txt file — it's not useful to the user.
			log.Printf("[skill-runner] materialize stdout: skip empty-text JSON for .txt output (content=%d bytes)", len(content))
			return
		}
	}

	// Ensure parent directory exists.
	dir := filepath.Dir(expectedOutput)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[skill-runner] materialize stdout: mkdir failed path=%q err=%v", dir, err)
		return
	}

	if err := os.WriteFile(expectedOutput, []byte(content), 0o644); err != nil {
		log.Printf("[skill-runner] materialize stdout: write failed path=%q err=%v", expectedOutput, err)
		return
	}
	log.Printf("[skill-runner] materialized stdout to expected output: %s (%d bytes)", expectedOutput, len(content))
}

func canMaterializePlainStdoutToOutput(path string) bool {
	switch strings.ToLower(strings.TrimSpace(filepath.Ext(path))) {
	case "", ".txt", ".text", ".md", ".markdown", ".csv", ".tsv", ".json", ".jsonl", ".xml", ".html", ".htm", ".log", ".yaml", ".yml":
		return true
	default:
		return false
	}
}

func (r *SkillRunner) materializeArtifactFileFromStepOutput(expectedOutput, content string) bool {
	expectedOutput = strings.TrimSpace(expectedOutput)
	if expectedOutput == "" {
		return false
	}
	sourcePath := selectArtifactFileFromJSONOutput(content, filepath.Ext(expectedOutput))
	if sourcePath == "" {
		return false
	}
	if samePath(sourcePath, expectedOutput) || !artifactExists(sourcePath) {
		return false
	}
	if err := os.MkdirAll(filepath.Dir(expectedOutput), 0o755); err != nil {
		log.Printf("[skill-runner] materialize artifact: mkdir failed path=%q err=%v", filepath.Dir(expectedOutput), err)
		return false
	}
	in, err := os.Open(sourcePath)
	if err != nil {
		log.Printf("[skill-runner] materialize artifact: open failed path=%q err=%v", sourcePath, err)
		return false
	}
	defer in.Close()
	out, err := os.Create(expectedOutput)
	if err != nil {
		log.Printf("[skill-runner] materialize artifact: create failed path=%q err=%v", expectedOutput, err)
		return false
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(expectedOutput)
		log.Printf("[skill-runner] materialize artifact: copy failed src=%q dst=%q err=%v", sourcePath, expectedOutput, err)
		return false
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(expectedOutput)
		log.Printf("[skill-runner] materialize artifact: close failed path=%q err=%v", expectedOutput, err)
		return false
	}
	log.Printf("[skill-runner] materialized artifact file to expected output: %s -> %s", sourcePath, expectedOutput)
	return true
}

func selectArtifactFileFromJSONOutput(content, expectedExt string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	for _, jsonContent := range artifactJSONPayloadCandidates(content) {
		sourcePath, blocked := selectArtifactFileFromJSONPayload(jsonContent, expectedExt)
		if sourcePath != "" {
			return sourcePath
		}
		if blocked {
			return ""
		}
	}
	return ""
}

func artifactJSONPayloadCandidates(content string) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	candidates := make([]string, 0, 2)
	seen := map[string]struct{}{}
	add := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return
		}
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}
	add(content)
	for idx := nextJSONPayloadStart(content, 0); idx >= 0; {
		if candidate := extractJSONPayloadCandidate(content[idx:]); candidate != "" {
			add(candidate)
		}
		nextStart := idx + 1
		if nextStart >= len(content) {
			break
		}
		idx = nextJSONPayloadStart(content, nextStart)
	}
	return candidates
}

func nextJSONPayloadStart(content string, start int) int {
	if start < 0 {
		start = 0
	}
	if start >= len(content) {
		return -1
	}
	objectIdx := strings.Index(content[start:], "{")
	arrayIdx := strings.Index(content[start:], "[")
	switch {
	case objectIdx < 0 && arrayIdx < 0:
		return -1
	case objectIdx < 0:
		return start + arrayIdx
	case arrayIdx < 0:
		return start + objectIdx
	case objectIdx < arrayIdx:
		return start + objectIdx
	default:
		return start + arrayIdx
	}
}

func extractJSONPayloadCandidate(content string) string {
	if content == "" {
		return ""
	}
	var open, close byte
	switch content[0] {
	case '{':
		open, close = '{', '}'
	case '[':
		open, close = '[', ']'
	default:
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := 0; i < len(content); i++ {
		c := content[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch c {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return strings.TrimSpace(content[:i+1])
			}
			if depth < 0 {
				return ""
			}
		}
	}
	return ""
}

func selectArtifactFileFromJSONPayload(content, expectedExt string) (string, bool) {
	pathKeys := []string{
		"output_path",
		"outputPath",
		"output",
		"artifact_path",
		"artifactPath",
		"path",
		"file",
		"files",
		"local_path",
		"local_file_path",
		"localFilePath",
	}
	containerKeys := []string{
		"result",
		"results",
		"data",
		"payload",
		"response",
		"outputs",
		"items",
	}
	var rawPathCandidates func(json.RawMessage) []string
	rawPathCandidates = func(raw json.RawMessage) []string {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
			return nil
		}
		var path string
		if err := json.Unmarshal(raw, &path); err == nil {
			return []string{path}
		}
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err == nil {
			var out []string
			for _, item := range items {
				out = append(out, rawPathCandidates(item)...)
			}
			return out
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err == nil {
			var out []string
			for _, key := range pathKeys {
				if value, ok := object[key]; ok {
					out = append(out, rawPathCandidates(value)...)
				}
			}
			for _, key := range containerKeys {
				if value, ok := object[key]; ok {
					out = append(out, rawPathCandidates(value)...)
				}
			}
			return out
		}
		return nil
	}
	var payload struct {
		Files        json.RawMessage `json:"files"`
		File         json.RawMessage `json:"file"`
		Path         json.RawMessage `json:"path"`
		Output       json.RawMessage `json:"output"`
		OutputPath   json.RawMessage `json:"output_path"`
		OutputPathJS json.RawMessage `json:"outputPath"`
		Outputs      json.RawMessage `json:"outputs"`
		Result       json.RawMessage `json:"result"`
		Results      json.RawMessage `json:"results"`
		Data         json.RawMessage `json:"data"`
		Payload      json.RawMessage `json:"payload"`
		Response     json.RawMessage `json:"response"`
		Artifact     json.RawMessage `json:"artifact"`
		ArtifactPath json.RawMessage `json:"artifact_path"`
		Artifacts    json.RawMessage `json:"artifacts"`
	}
	var raw json.RawMessage
	decoder := json.NewDecoder(strings.NewReader(content))
	if err := decoder.Decode(&raw); err != nil {
		return "", false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return "", false
	}
	var explicitCandidates []string
	var genericCandidates []string
	if err := json.Unmarshal(raw, &payload); err == nil {
		explicitCandidates = append(explicitCandidates, rawPathCandidates(payload.Artifact)...)
		explicitCandidates = append(explicitCandidates, rawPathCandidates(payload.Artifacts)...)
		explicitCandidates = append(explicitCandidates, rawPathCandidates(payload.ArtifactPath)...)
		explicitCandidates = append(explicitCandidates, rawPathCandidates(payload.OutputPath)...)
		explicitCandidates = append(explicitCandidates, rawPathCandidates(payload.OutputPathJS)...)
		explicitCandidates = append(explicitCandidates, rawPathCandidates(payload.Output)...)
		explicitCandidates = append(explicitCandidates, rawPathCandidates(payload.Outputs)...)
		explicitCandidates = append(explicitCandidates, rawPathCandidates(payload.Result)...)
		explicitCandidates = append(explicitCandidates, rawPathCandidates(payload.Results)...)
		explicitCandidates = append(explicitCandidates, rawPathCandidates(payload.Data)...)
		explicitCandidates = append(explicitCandidates, rawPathCandidates(payload.Payload)...)
		explicitCandidates = append(explicitCandidates, rawPathCandidates(payload.Response)...)
		genericCandidates = append(genericCandidates, rawPathCandidates(payload.Files)...)
		genericCandidates = append(genericCandidates, rawPathCandidates(payload.File)...)
		genericCandidates = append(genericCandidates, rawPathCandidates(payload.Path)...)
	}
	expectedExt = strings.ToLower(strings.TrimSpace(expectedExt))
	if selected, blocked := selectArtifactPathCandidate(explicitCandidates, expectedExt); selected != "" || blocked {
		return selected, blocked
	}
	genericCandidates = append(genericCandidates, rawPathCandidates(raw)...)
	selected, _ := selectArtifactPathCandidate(genericCandidates, expectedExt)
	return selected, false
}

func selectArtifactPathCandidate(candidates []string, expectedExt string) (string, bool) {
	var fallback string
	var extensionlessFallback string
	hasMissingAbsolute := false
	hasExistingMismatch := false
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || !filepath.IsAbs(candidate) {
			continue
		}
		if !artifactExists(candidate) {
			hasMissingAbsolute = true
			continue
		}
		if fallback == "" {
			fallback = candidate
		}
		if expectedExt != "" && strings.EqualFold(filepath.Ext(candidate), expectedExt) {
			return candidate, false
		}
		if expectedExt != "" && filepath.Ext(candidate) == "" && extensionlessFallback == "" {
			extensionlessFallback = candidate
		} else if expectedExt != "" {
			hasExistingMismatch = true
		}
	}
	if expectedExt != "" {
		if extensionlessFallback != "" {
			return extensionlessFallback, false
		}
		return "", hasExistingMismatch || hasMissingAbsolute
	}
	return fallback, false
}

func samePath(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// stripSkillRunnerMetadataFromOutput removes the runner-injected metadata
// header block from step output, leaving only the skill's actual content.
// The metadata header is a contiguous block at the top of the output:
//
//	shell: cmd.exe
//	elapsed: 5.857s
//	/path/to/workspace
//	command: cmd.exe /c ...
//	───────────────
//	{actual content starts here}
//
// Only leading metadata lines are stripped. Once actual content begins,
// all subsequent lines are preserved verbatim (even if they happen to
// start with "command: " etc — that's the skill's output, not metadata).
func stripSkillRunnerMetadataFromOutput(output string) string {
	lines := strings.Split(output, "\n")
	// Find where actual content starts (first non-metadata line after header).
	contentStart := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			// Blank lines within the header block are part of metadata
			continue
		}
		if isSkillRunnerMetadataLine(trimmed) {
			// Exception: [stderr] lines containing structured JSON data should
			// be treated as content, not metadata. Some CLI tools (e.g.
			// rapidocr_onnxruntime v1.4+) output valid results to stderr via
			// Python logging frameworks.
			if strings.HasPrefix(trimmed, "[stderr] ") {
				stderrContent := strings.TrimSpace(strings.TrimPrefix(trimmed, "[stderr] "))
				if stderrContent != "" && (stderrContent[0] == '{' || stderrContent[0] == '[') {
					// Replace this line with the stripped content (without [stderr] prefix)
					// so downstream JSON parsers can consume it directly.
					lines[i] = stderrContent
					contentStart = i
					break
				}
			}
			contentStart = i + 1
			continue
		}
		// First non-metadata, non-blank line — content starts here
		contentStart = i
		break
	}
	if contentStart >= len(lines) {
		return ""
	}
	// Also strip trailing [stderr] and [error] blocks that the runner appends.
	// Exception: if a [stderr] line contains structured JSON output (starts with
	// '{' after the prefix), preserve it — some CLI tools (e.g. rapidocr v1.4+)
	// output valid results to stderr via logging frameworks.
	contentEnd := len(lines)
	for i := len(lines) - 1; i >= contentStart; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			contentEnd = i
			continue
		}
		if strings.HasPrefix(trimmed, "[error] ") {
			contentEnd = i
			continue
		}
		if strings.HasPrefix(trimmed, "[stderr] ") {
			// Check if the stderr content after the prefix looks like useful data
			stderrContent := strings.TrimSpace(strings.TrimPrefix(trimmed, "[stderr] "))
			if stderrContent != "" && (stderrContent[0] == '{' || stderrContent[0] == '[') {
				// Structured data in stderr — strip prefix and keep as content
				lines[i] = stderrContent
				break
			}
			contentEnd = i
			continue
		}
		break
	}
	if contentEnd <= contentStart {
		return ""
	}
	result := strings.Join(lines[contentStart:contentEnd], "\n")
	return strings.TrimSpace(result)
}

// extractTextFieldFromJSON attempts to extract the "text" field from a JSON
// string. Returns the extracted text or empty string if the content is not
// JSON or doesn't have a usable "text" field.
// Falls back to joining the "lines" array if "text" is empty but "lines" exists.
func extractTextFieldFromJSON(content string) string {
	content = strings.TrimSpace(content)
	if len(content) < 2 || content[0] != '{' {
		return ""
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(content), &obj); err != nil {
		return ""
	}
	// Primary: use "text" field
	if text, ok := obj["text"].(string); ok && strings.TrimSpace(text) != "" {
		return strings.TrimSpace(text)
	}
	// Fallback: join "lines" array
	if linesRaw, ok := obj["lines"].([]interface{}); ok && len(linesRaw) > 0 {
		var parts []string
		for _, item := range linesRaw {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return ""
}

// looksLikeJSONObject returns true if content starts with '{' and is valid JSON.
// Used to distinguish structured JSON output (that should not be written as-is
// to .txt files) from plain text output.
func looksLikeJSONObject(content string) bool {
	content = strings.TrimSpace(content)
	if len(content) < 2 || content[0] != '{' {
		return false
	}
	var obj map[string]interface{}
	return json.Unmarshal([]byte(content), &obj) == nil
}

func isSkillRunnerMetadataLine(line string) bool {
	if strings.HasPrefix(line, "shell: ") {
		return true
	}
	if strings.HasPrefix(line, "elapsed: ") {
		return true
	}
	if strings.HasPrefix(line, "") {
		return true
	}
	if strings.HasPrefix(line, "command: ") {
		return true
	}
	if strings.HasPrefix(line, "exit code: ") {
		return true
	}
	if strings.HasPrefix(line, "Exit code: ") {
		return true
	}
	// Separator line between metadata header and actual output
	if strings.TrimSpace(line) == "───────────────" {
		return true
	}
	// stderr marker (runner-injected, not skill output)
	if strings.HasPrefix(line, "[stderr] ") {
		return true
	}
	// error marker (runner-injected on failure)
	if strings.HasPrefix(line, "[error] ") {
		return true
	}
	return false
}

func logSkillRunnerFailure(runID, ownerID, skillName, stage, reason string) {
	log.Printf("[skill-runner] skill_failure stage=%s run=%s owner=%q skill=%q reason=%q",
		strings.TrimSpace(stage),
		strings.TrimSpace(runID),
		strings.TrimSpace(ownerID),
		strings.TrimSpace(skillName),
		strings.TrimSpace(reason),
	)
}

func skillRunFailureReason(status SkillRunStatus) string {
	if reason := strings.TrimSpace(status.Error); reason != "" {
		return reason
	}
	for _, step := range status.Steps {
		if step.LifecycleStatus() != skillStepStatusFailed {
			continue
		}
		if reason := strings.TrimSpace(step.Error); reason != "" {
			if name := strings.TrimSpace(step.Name); name != "" {
				return fmt.Sprintf("step %d (%s) failed: %s", step.Index+1, name, reason)
			}
			if action := strings.TrimSpace(step.Action); action != "" {
				return fmt.Sprintf("step %d (%s) failed: %s", step.Index+1, action, reason)
			}
			return fmt.Sprintf("step %d failed: %s", step.Index+1, reason)
		}
	}
	return "skill run failed"
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
	r.finalizeRunOutcome(run, skillRunStatusFailed, execStart)
	r.updateUsageStats(skill, execErr)
}

func prepareSkillRunWorkspace(runID, skillName, skillDir string) (string, func(), error) {
	return prepareSkillRunWorkspaceInRoot(runID, skillName, skillDir, "")
}

func prepareSkillRunWorkspaceInRoot(runID, skillName, skillDir, workspaceRoot string) (string, func(), error) {
	skillDir = strings.TrimSpace(skillDir)
	if skillDir == "" {
		return "", func() {}, nil
	}
	cleanupStaleSkillRunWorkspaces()
	info, err := os.Stat(skillDir)
	if err != nil {
		return "", func() {}, err
	}
	if !info.IsDir() {
		return "", func() {}, fmt.Errorf("skill dir is not a directory: %s", skillDir)
	}
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		root = skillRunWorkspaceRoot()
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", func() {}, err
	}
	prefix := sanitizeSkillRunWorkspacePart(skillName)
	if prefix == "" {
		prefix = "skill"
	}
	workspace, err := os.MkdirTemp(root, prefix+"-*")
	if err != nil {
		return "", func() {}, err
	}
	copyStartedAt := time.Now()
	if err := copyDirContentsForSkillRun(skillDir, workspace); err != nil {
		_ = os.RemoveAll(workspace)
		return "", func() {}, err
	}
	log.Printf("[skill-runner] run=%s skill=%q workspace prepared elapsed=%s source_dir=%s workspace=%s", runID, skillName, time.Since(copyStartedAt).Truncate(time.Millisecond), skillDir, workspace)
	cleanup := func() {
		if err := os.RemoveAll(workspace); err != nil {
			log.Printf("[skill-runner] run=%s workspace cleanup failed dir=%q err=%v", runID, workspace, err)
		}
	}
	return workspace, cleanup, nil
}

// cleanupStaleSkillRunWorkspacesLastRun tracks the last cleanup time.
// Cleanup runs at most once per hour (not sync.Once) to handle:
// 1. Retained workspaces (artifacts) that are later pruned from r.runs by CleanupFinished
// 2. Long-running maclaw sessions where orphaned workspaces accumulate
var (
	cleanupStaleSkillRunWorkspacesMu      sync.Mutex
	cleanupStaleSkillRunWorkspacesLastRun time.Time
)

func cleanupStaleSkillRunWorkspaces() {
	cleanupStaleSkillRunWorkspacesMu.Lock()
	if time.Since(cleanupStaleSkillRunWorkspacesLastRun) < time.Hour {
		cleanupStaleSkillRunWorkspacesMu.Unlock()
		return
	}
	cleanupStaleSkillRunWorkspacesLastRun = time.Now()
	cleanupStaleSkillRunWorkspacesMu.Unlock()

	roots := []string{skillRunWorkspaceRoot()}
	// Also prune project-local isolation roots when the configured workdir is set.
	if wd := strings.TrimSpace(corelib.EffectiveWorkspaceDir()); wd != "" {
		if info, err := os.Stat(wd); err == nil && info.IsDir() {
			roots = append(roots, filepath.Join(wd, cskill.MaclawSkillTmpDirName, "skill-runs"))
		}
	}
	for _, root := range roots {
		cleanupStaleSkillRunWorkspacesInRoot(root)
	}
}

func cleanupStaleSkillRunWorkspacesInRoot(root string) {
	root = strings.TrimSpace(root)
	if root == "" {
		return
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if !pathWithinDir(root, path) {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			log.Printf("[skill-runner] stale workspace cleanup failed dir=%q err=%v", path, err)
		}
	}
}

func skillRunWorkspaceRoot() string {
	return filepath.Join(os.TempDir(), "maclaw-skill-runs")
}

// skillRunWorkspaceRootFromEnv returns the isolated skill-run workspace root.
// When MACLAW_WORKDIR is available (user project/workbench), isolation
// workspaces live under <workdir>/.maclaw-tmp/skill-runs so artifacts stay
// inside the project tree. Falls back to the system temp root otherwise.
func skillRunWorkspaceRootFromEnv(extraEnv map[string]string) string {
	if root := cskill.SkillWorkdirTmpSubdir(extraEnv, "skill-runs"); root != "" {
		return root
	}
	return skillRunWorkspaceRoot()
}

// injectOwnerWorkDirEnv resolves the session owner's workbench/project directory
// and applies the shared Skill Runner workdir binding (MACLAW_WORKDIR + TEMP
// redirect) so third-party skills need no per-skill patches.
func (r *SkillRunner) injectOwnerWorkDirEnv(ownerID string, extraEnv map[string]string) map[string]string {
	wd := ""
	if r != nil && r.executor != nil && r.executor.app != nil {
		wd = strings.TrimSpace(r.executor.app.EffectiveWorkingDirForOwner(ownerID))
	}
	out := cskill.InjectSkillWorkDirEnv(wd, extraEnv)
	if bound := cskill.SkillWorkdirFromEnv(out); bound != "" {
		log.Printf("[skill-runner] inject workdir owner=%q workdir=%q temp=%q",
			ownerID, bound, filepath.Join(bound, cskill.MaclawSkillTmpDirName))
	} else {
		log.Printf("[skill-runner] inject workdir skipped owner=%q resolved=%q (downloads may use system TEMP)",
			ownerID, wd)
	}
	return out
}

// skillRunBlockedDownloadHint steers the agent away from broken Hub download skills.
func skillRunBlockedDownloadHint(skill *corelib.NLSkillEntry) string {
	if skill == nil {
		return ""
	}
	blob := strings.ToLower(strings.TrimSpace(skill.Name) + " " + strings.TrimSpace(skill.Description) + " " + strings.TrimSpace(skill.LastError))
	for _, key := range []string{"wget", "curl", "download", "fetch", "paper fetch", "python runtime not found", "shell_tool"} {
		if strings.Contains(blob, key) {
			return "[action: use_builtin] For simple HTTP/PDF downloads use download_file or web_fetch with save_path under the working directory instead of this skill."
		}
	}
	return ""
}


func shouldRetainSkillRunWorkspaceStatus(status SkillRunStatus, workspace string) bool {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return false
	}
	if artifactPath := strings.TrimSpace(detectArtifactPathFromStatus(&status)); artifactPath != "" && pathWithinDir(workspace, artifactPath) {
		return true
	}
	summarizeSkillRun(&status)
	artifactPath := strings.TrimSpace(status.Summary.ArtifactPath)
	if artifactPath == "" {
		return false
	}
	return pathWithinDir(workspace, artifactPath)
}

func sanitizeSkillRunWorkspacePart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}

func remapSkillRunStepToWorkspace(step corelib.NLSkillStep, sourceDir, workspaceDir string) corelib.NLSkillStep {
	sourceDir = strings.TrimSpace(sourceDir)
	workspaceDir = strings.TrimSpace(workspaceDir)
	if sourceDir == "" || workspaceDir == "" || step.Params == nil {
		return step
	}
	sourceDir = filepath.Clean(sourceDir)
	workspaceDir = filepath.Clean(workspaceDir)
	if sameCleanPath(sourceDir, workspaceDir) {
		return step
	}
	params := make(map[string]interface{}, len(step.Params))
	for key, value := range step.Params {
		params[key] = remapSkillRunParamValue(value, sourceDir, workspaceDir)
	}
	step.Params = params
	return step
}

func remapSkillRunParamValue(value interface{}, sourceDir, workspaceDir string) interface{} {
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return value
		}
		return remapSkillRunPathString(v, sourceDir, workspaceDir)
	case map[string]interface{}:
		if len(v) == 0 {
			return value
		}
		out := make(map[string]interface{}, len(v))
		for key, child := range v {
			out[key] = remapSkillRunParamValue(child, sourceDir, workspaceDir)
		}
		return out
	case map[string]string:
		if len(v) == 0 {
			return value
		}
		out := make(map[string]string, len(v))
		for key, child := range v {
			out[key] = remapSkillRunPathString(child, sourceDir, workspaceDir)
		}
		return out
	case []interface{}:
		if len(v) == 0 {
			return value
		}
		out := make([]interface{}, len(v))
		for i, child := range v {
			out[i] = remapSkillRunParamValue(child, sourceDir, workspaceDir)
		}
		return out
	case []string:
		if len(v) == 0 {
			return value
		}
		out := make([]string, len(v))
		for i, child := range v {
			out[i] = remapSkillRunPathString(child, sourceDir, workspaceDir)
		}
		return out
	default:
		return value
	}
}

func remapSkillRunPathString(value, sourceDir, workspaceDir string) string {
	if strings.TrimSpace(value) == "" || sourceDir == "" || workspaceDir == "" {
		return value
	}
	variants := []string{sourceDir, filepath.ToSlash(sourceDir)}
	if runtime.GOOS == "windows" {
		variants = append(variants, strings.ReplaceAll(sourceDir, `\`, `/`))
	}
	out := value
	for _, from := range variants {
		from = strings.TrimSpace(from)
		if from == "" {
			continue
		}
		to := workspaceDir
		if strings.Contains(from, "/") && !strings.Contains(from, `\`) {
			to = filepath.ToSlash(workspaceDir)
		}
		out = strings.ReplaceAll(out, from, to)
	}
	return out
}

func sameCleanPath(a, b string) bool {
	a = filepath.Clean(strings.TrimSpace(a))
	b = filepath.Clean(strings.TrimSpace(b))
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func (r *SkillRunner) executeAsync(ctx context.Context, run *skillRun, skill *corelib.NLSkillEntry) {
	execStart, finishExecution := r.beginRunExecution(run, "steps")
	finishStatus := "unknown"
	defer func() {
		if finishStatus == "unknown" {
			r.mu.RLock()
			if run != nil {
				if status := run.status.LifecycleStatus(); status != skillRunStatusUnknown {
					finishStatus = status.String()
				}
			}
			r.mu.RUnlock()
		}
		finishExecution(finishStatus)
	}()
	originalSkill := skill
	execSkill := *skill
	skill = &execSkill
	sourceSkillDir := skill.SkillDir
	wsRoot := skillRunWorkspaceRootFromEnv(run.extraEnv)
	if workspace, cleanup, err := prepareSkillRunWorkspaceInRoot(run.status.RunID, skill.Name, skill.SkillDir, wsRoot); err != nil {
		log.Printf("[skill-runner] run=%s owner=%q skill=%q workspace isolation unavailable dir=%q err=%v; using installed dir", run.status.RunID, run.status.OwnerID, skill.Name, skill.SkillDir, err)
	} else if workspace != "" {
		run.workspaceDir = workspace
		log.Printf("[skill-runner] run=%s owner=%q skill=%q workspace=%s source_dir=%s", run.status.RunID, run.status.OwnerID, skill.Name, workspace, skill.SkillDir)
		skill.SkillDir = workspace
		defer func() {
			r.mu.RLock()
			statusSnapshot := run.status
			r.mu.RUnlock()
			if shouldRetainSkillRunWorkspaceStatus(statusSnapshot, workspace) {
				log.Printf("[skill-runner] run=%s owner=%q retaining workspace for artifact access: %s", run.status.RunID, run.status.OwnerID, workspace)
				return
			}
			cleanup()
		}()
	}
	// Global timeout: skill/app settings and long document translation runs
	// can extend the system Skill Runner default.
	globalTimeout := time.Duration(r.applyEffectiveSkillGlobalTimeoutSec(run, skill)) * time.Second
	if skill.GlobalTimeout > 0 {
		log.Printf("[skill-runner] using skill-level global timeout: %v", globalTimeout)
	}
	globalCtx, globalCancel := context.WithTimeout(ctx, globalTimeout)
	defer globalCancel()

	defer func() {
		if rec := recover(); rec != nil {
			execErr := fmt.Errorf("panic: %v", rec)
			finishStatus = skillRunStatusFailed.String()
			r.mu.Lock()
			run.status.Error = execErr.Error()
			r.mu.Unlock()
			r.finalizeRunOutcome(run, skillRunStatusFailed, execStart)
			r.updateUsageStats(skill, execErr)
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
		finishStatus = skillRunStatusSuccess.String()
		r.finalizeRunOutcome(run, skillRunStatusSuccess, execStart)
		r.updateUsageStats(skill, nil)
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
	log.Printf("[skill-runner] run=%s owner=%q openai proxy check: needsProxy=%v required_env=%v extraEnv_keys=%v processEnv_OPENAI_API_KEY=%q",
		run.status.RunID, run.status.OwnerID, needsProxy, skill.RequiredEnv, mapKeys(run.extraEnv), truncateEnvForLog(os.Getenv("OPENAI_API_KEY")))
	if needsProxy {
		// Build config from current LLM provider
		var proxyCfg corelib.OpenAIProxyConfig
		if r.executor != nil && r.executor.app != nil {
			llmCfg := r.executor.app.GetMaclawLLMConfig()
			providerName := maclawLLMUsageProviderName(r.executor.app, llmCfg)
			proxyCfg = corelib.OpenAIProxyConfig{
				URL:      llmCfg.URL,
				Key:      llmCfg.Key,
				Model:    llmCfg.Model,
				Protocol: llmCfg.Protocol,
				WireAPI:  llmCfg.WireAPI,
				AuthType: llmCfg.AuthType,
				UsageCallback: func(usage corelib.OpenAIProxyUsage) {
					if providerName == "" {
						return
					}
					r.executor.app.AccumulateLLMTokenUsageWithCache(providerName, usage.InputTokens, usage.OutputTokens, usage.CachedInputTokens, usage.CacheWriteTokens)
				},
			}
		}
		if err := corelib.ValidateOpenAIProxyUpstreamConfig(proxyCfg); err != nil {
			errMsg := fmt.Sprintf("skill requires OpenAI-compatible environment variables, but the GUI local proxy cannot start because %s [action: configure_llm]", err)
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
		log.Printf("[skill-runner] run=%s owner=%q openai proxy started on port %d for skill %q", run.status.RunID, run.status.OwnerID, port, skill.Name)
	}

	// ── Dependency auto-install is now handled by the unified requirement
	// system in StartRun (Registry.FixAll). The pip/npm packages are checked
	// and installed before execution begins. ──

	log.Printf("[skill-runner] run=%s owner=%q starting skill %q (%d steps, mode=%s, dir=%s)",
		run.status.RunID, run.status.OwnerID, skill.Name, len(skill.Steps), skill.Mode, skill.SkillDir)
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

	// Compute the shared Python runtime extra env once — it doesn't change between
	// steps (same skill.RequiresPython, same dataDir, same system PATH).
	runtimeExtraEnv := cskill.SharedPythonRuntimeExtraEnvWithDataDir(r.dataDir(), skill, os.Environ())

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
			r.finalizeRunOutcome(run, skillRunStatusFailed, execStart)
			r.updateUsageStats(skill, execErr)
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
		resolvedStep, bindWarnings, resolveErr := resolveSkillStepWithWarnings(step, r.templateVarsForRun(run.status.RunID), skill.SkillDir, skillParams)
		if len(bindWarnings) > 0 {
			r.mu.Lock()
			// De-duplicate: same param mismatch warning appears for every step
			// sharing the same schema. Only add if not already present.
			for _, w := range bindWarnings {
				isDuplicate := false
				for _, existing := range run.status.Warnings {
					if existing == w {
						isDuplicate = true
						break
					}
				}
				if !isDuplicate {
					run.status.Warnings = append(run.status.Warnings, w)
				}
			}
			r.mu.Unlock()
		}
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
		stepExtraEnv := mergeSkillRuntimeExtraEnv(run.extraEnv, runtimeExtraEnv)
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
			if len(stepExtraEnv) > 0 {
				cskill.MergeExtraEnvParam(resolvedStep.Params, stepExtraEnv)
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
			if len(stepExtraEnv) > 0 {
				cskill.MergeExtraEnvParam(resolvedStep.Params, stepExtraEnv)
			}
		}
		// Propagate skill-level required_env to bash steps for auto-injection.
		resolvedStep = cskill.PrepareResolvedStepEnv(resolvedStep, skill.RequiredEnv, stepExtraEnv)
		resolvedStep = remapSkillRunStepToWorkspace(resolvedStep, sourceSkillDir, run.workspaceDir)
		if resolvedStep.Params == nil {
			resolvedStep.Params = map[string]interface{}{}
		}
		resolvedStep.Params["_skill_run_id"] = run.status.RunID
		resolvedStep.Params["_skill_owner_id"] = run.status.OwnerID
		resolvedStep.Params["_live_output"] = run.liveOutput
		restoreEnv := installSkillStepProcessEnvForRun(run.status.RunID, run.status.OwnerID, resolvedStep.Action, stepExtraEnv)
		stepStart := time.Now()
		log.Printf("[skill-runner] run=%s owner=%q step %d/%d: action=%s command=%q", run.status.RunID, run.status.OwnerID, i+1, len(skill.Steps), resolvedStep.Action, resolveCommandForDisplay(resolvedStep))
		result, stepErr := func() (string, error) {
			defer restoreEnv()
			return r.executeStepWithPoll(globalCtx, run.status.RunID, resolvedStep, skill.SkillDir)
		}()

		// ── Runtime dependency auto-install + retry ──
		// If the step failed with a missing Python/Node package (ModuleNotFoundError,
		// Cannot find module), attempt to install it and retry the step.
		// This handles undeclared transitive dependencies that weren't caught by
		// the pre-execution requirement check (which only covers explicit requires_python).
		// Supports up to 3 rounds to handle scripts that import multiple missing packages.
		if stepErr != nil && resolvedStepAction.IsBash() {
			const maxDepInstallRetries = 3
			// Resolve the Python that this step actually runs with. If the skill
			// uses a shared Python runtime (RequiresPython non-empty), stepExtraEnv
			// contains MACLAW_PYTHON pointing to the runtime's Python. We must
			// install into the SAME Python, not the system default.
			depInstallPython := stepExtraEnv["MACLAW_PYTHON"]
			for depRetry := 0; depRetry < maxDepInstallRetries && stepErr != nil; depRetry++ {
				if ctx.Err() != nil {
					break // user cancelled during install cycle
				}
				bErr, ok := stepErr.(*bashStepError)
				if !ok {
					break
				}
				classified := classifyBashErrorFull(bErr.Stderr(), bErr.Stdout(), resolveCommandForDisplay(resolvedStep), bErr.ExitCode())
				if classified.Class != cskill.ErrMissingDependency {
					break
				}
				// Report progress to user via live output.
				depKind, depPkg := cskill.MissingDependencyInstallNameFromError(bErr.Stderr() + " " + bErr.Stdout())
				if depPkg != "" && run.liveOutput != nil {
					run.liveOutput.Append(fmt.Sprintf("正在自动安装缺失的 %s 包: %s ...", depKind, depPkg))
				}
				installErr := cskill.AutoInstallMissingDependencyWithPython(bErr.Stderr(), bErr.Stdout(), resolveCommandForDisplay(resolvedStep), skill.SkillDir, depInstallPython)
				if installErr != nil {
					log.Printf("[skill-runner] run=%s step %d/%d: dependency auto-install failed: %v", run.status.RunID, i+1, len(skill.Steps), installErr)
					break
				}
				// Re-check cancellation after install (pip install has no context
				// and can take 10-60s on slow networks).
				if ctx.Err() != nil {
					break
				}
				// Dependency installed — retry the step.
				log.Printf("[skill-runner] run=%s step %d/%d: dependency auto-installed (%s), retrying step (attempt %d/%d)",
					run.status.RunID, i+1, len(skill.Steps), depPkg, depRetry+1, maxDepInstallRetries)
				restoreEnvRetry := installSkillStepProcessEnvForRun(run.status.RunID, run.status.OwnerID, resolvedStep.Action, stepExtraEnv)
				retryResult, retryErr := func() (string, error) {
					defer restoreEnvRetry()
					return r.executeStepWithPoll(globalCtx, run.status.RunID, resolvedStep, skill.SkillDir)
				}()
				result = retryResult
				stepErr = retryErr
				if retryErr == nil {
					log.Printf("[skill-runner] run=%s step %d/%d: retry after auto-install succeeded", run.status.RunID, i+1, len(skill.Steps))
					if run.liveOutput != nil {
						run.liveOutput.Append("依赖安装完成，步骤执行成功")
					}
				}
			}
		}

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
			if run.monitorCancel != nil {
				run.monitorCancel()
			}
			r.mu.Unlock()
			finishStatus = skillRunStatusCancelled.String()
			r.finalizeRunOutcome(run, skillRunStatusCancelled, execStart)
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
			log.Printf("[skill-runner] run=%s owner=%q step %d/%d FAILED elapsed=%s: %v", run.status.RunID, run.status.OwnerID, i+1, len(skill.Steps), time.Since(stepStart).Truncate(time.Millisecond), stepErr)
			// Detailed diagnostic dump for AI debugging tools (logged to file, not shown to user)
			log.Printf("[skill-runner-diag] === STEP FAILURE DIAGNOSTIC ===")
			log.Printf("[skill-runner-diag] run_id=%s skill=%q step=%d/%d action=%s", run.status.RunID, skill.Name, i+1, len(skill.Steps), resolvedStep.Action)
			log.Printf("[skill-runner-diag] command=%s", resolveCommandForDisplay(resolvedStep))
			log.Printf("[skill-runner-diag] skill_dir=%s workspace=%s", skill.SkillDir, run.workspaceDir)
			log.Printf("[skill-runner-diag] template_vars=%v", run.templateVars)
			log.Printf("[skill-runner-diag] extra_env_keys=%v", mapKeys(run.extraEnv))
			log.Printf("[skill-runner-diag] error=%v", stepErr)
			if result != "" {
				log.Printf("[skill-runner-diag] output_tail=%s", truncateRunesMarker(result, 2000, "\n...(truncated)"))
			}
			// Extract error details if it's a bashStepError
			if bErr, ok := stepErr.(*bashStepError); ok {
				run.status.Steps[i].ExitCode = bErr.ExitCode()
				run.status.Steps[i].Timeout = bErr.IsTimeout()
				run.status.Steps[i].StdoutLastLines = lastNLines(bErr.Stdout(), 10)
				run.status.Steps[i].StderrLastLines = lastNLines(bErr.Stderr(), 10)
				log.Printf("[skill-runner-diag] exit_code=%d timeout=%v", bErr.ExitCode(), bErr.IsTimeout())
				if stderrTail := strings.TrimSpace(truncateRunesMarker(bErr.Stderr(), 1000, "...")); stderrTail != "" {
					log.Printf("[skill-runner-diag] stderr_tail=%s", stderrTail)
				}
			}
			log.Printf("[skill-runner-diag] === END DIAGNOSTIC ===")
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
			log.Printf("[skill-runner] run=%s owner=%q step %d/%d OK elapsed=%s (output %d bytes)", run.status.RunID, run.status.OwnerID, i+1, len(skill.Steps), time.Since(stepStart).Truncate(time.Millisecond), len(result))
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
		finishStatus = skillRunStatusCancelled.String()
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
	log.Printf("[skill-runner] run=%s owner=%q skill %q finished: status=%s steps=%d elapsed=%s",
		run.status.RunID, run.status.OwnerID, skill.Name, finalStatus, len(skill.Steps), time.Since(execStart).Truncate(time.Millisecond))
	r.mu.Unlock()

	// 更新 skill 使用统计
	finishStatus = finalStatus.String()
	r.finalizeRunOutcome(run, finalStatus, execStart)

	statsEntry := r.updateUsageStats(skill, execErr)
	r.recordSkillUsageExperience(skill, execErr, run.runArgs)

	// 自动上传触发
	r.tryAutoUpload(originalSkill, run)

	// Notify evolution pipeline (async, non-blocking).
	// Skip when env kill switch or config skill_evolution_enabled=false.
	if r.evolutionPipeline != nil && !cskill.EvolutionEnvDisabled() {
		cfgEnabled := true
		if r.executor != nil && r.executor.app != nil {
			if cfg, err := r.executor.app.LoadConfig(); err == nil {
				cfgEnabled = cfg.IsSkillEvolutionEnabled()
			}
		}
		if !cfgEnabled {
			return
		}
		var runArgsStr map[string]string
		if run.runArgs != nil {
			runArgsStr = make(map[string]string, len(run.runArgs))
			for k, v := range run.runArgs {
				switch val := v.(type) {
				case string:
					runArgsStr[k] = val
				case nil:
					// skip
				default:
					// Best-effort for non-string values (e.g. nested maps).
					// RepairGate primarily needs the top-level string params
					// (input, output, message) for replay.
					runArgsStr[k] = fmt.Sprintf("%v", val)
				}
			}
		}
		// Prefer stats-persisted entry (UsageCount/LastError) over runtime pointer.
		evoEntry := skill
		if statsEntry != nil {
			evoEntry = statsEntry
		}
		r.evolutionPipeline.NotifySkillExecution(skill.Name, evoEntry, &cskill.SkillExecutionResultCompat{
			Success:       execErr == nil,
			OutputQuality: r.skillRunOutputQuality(run, execErr),
		}, runArgsStr)
	}
}

// skillRunOutputQuality maps a finished run to the evolution quality band.
func (r *SkillRunner) skillRunOutputQuality(run *skillRun, execErr error) string {
	if execErr != nil || run == nil {
		return "basic"
	}
	if r != nil {
		r.mu.RLock()
		defer r.mu.RUnlock()
	}
	if run.status.LifecycleStatus() != skillRunStatusSuccess {
		return "basic"
	}
	for _, st := range run.status.Steps {
		if st.IsFailed() {
			return "basic"
		}
	}
	// All steps succeeded — treat as good (excellent reserved for scorer).
	return "good"
}

// updateUsageStats persists usage counters and returns a deep copy of the updated
// entry (for evolution/self-repair). Nil when the skill was not found in storage.
func (r *SkillRunner) updateUsageStats(skill *corelib.NLSkillEntry, execErr error) *corelib.NLSkillEntry {
	if r == nil || r.executor == nil || r.executor.app == nil || skill == nil {
		return nil
	}
	startedAt := time.Now()
	shouldEmit := false
	var updatedEntry *corelib.NLSkillEntry
	successfulSkillName := ""

	lockWaitStart := time.Now()
	r.executor.mu.Lock()
	if waited := time.Since(lockWaitStart); waited > 100*time.Millisecond {
		log.Printf("[skill-runner] usage_stats lock_wait skill=%q waited=%s", skill.Name, waited.Round(time.Millisecond))
	}
	loadStart := time.Now()
	skills := r.executor.loadSkills()
	loadElapsed := time.Since(loadStart)
	for i, s := range skills {
		if s.Name == skill.Name {
			mergeSkillRunIdentityForUsageStats(&skills[i], skill)
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
			saveStart := time.Now()
			_ = r.executor.saveSkills(skills)
			saveElapsed := time.Since(saveStart)
			log.Printf("[skill-runner] usage stats updated for %q: usage=%d success=%d failure=%d workaround=%d",
				skill.Name, skills[i].UsageCount, skills[i].SuccessCount, skills[i].FailureCount, skills[i].WorkaroundCount)
			if loadElapsed > 100*time.Millisecond || saveElapsed > 100*time.Millisecond {
				log.Printf("[skill-runner] usage_stats io skill=%q load=%s save=%s", skill.Name, loadElapsed.Round(time.Millisecond), saveElapsed.Round(time.Millisecond))
			}
			shouldEmit = true
			// Deep copy for evolution / self-repair (outside lock).
			updatedEntry = cskill.CloneNLSkillEntry(&skills[i])
			break
		}
	}
	r.executor.mu.Unlock()
	if elapsed := time.Since(startedAt); elapsed > 100*time.Millisecond {
		log.Printf("[skill-runner] usage_stats done skill=%q elapsed=%s", skill.Name, elapsed.Round(time.Millisecond))
	}

	// Notify frontend to refresh skill list with updated stats (outside lock).
	if shouldEmit && r.executor.app != nil {
		r.executor.app.emitEvent(EventSkillUsageUpdated)
	}

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
	// Prefer EvolutionPipeline unified scheduling when wired (throttled +
	// coalesced with optimize/promote). Fall back to a local goroutine when
	// the pipeline is unavailable or repair is disabled there.
	if updatedEntry != nil && execErr != nil {
		if r.canStartRepairSkill(updatedEntry) {
			r.markSelfRepairPending(updatedEntry.Name)
			if !r.evolutionOwnsRepair() {
				go r.maybeRepairSkill(updatedEntry)
			}
			// When evolution owns repair, NotifySkillExecution (caller) schedules it.
		}
	}
	return updatedEntry
}

// evolutionOwnsRepair reports whether self-repair is scheduled by EvolutionPipeline.
func (r *SkillRunner) evolutionOwnsRepair() bool {
	return r != nil && r.evolutionPipeline != nil &&
		r.evolutionPipeline.EnableRepair && r.evolutionPipeline.RepairHook != nil
}

func mergeSkillRunIdentityForUsageStats(dst, runtimeEntry *corelib.NLSkillEntry) {
	if dst == nil || runtimeEntry == nil {
		return
	}
	if normalizeSkillEntrySource(runtimeEntry.Source) == skillEntrySourceUnknown {
		return
	}
	if normalizeSkillEntrySource(dst.Source) == skillEntrySourceUnknown {
		dst.Source = runtimeEntry.Source
	}
	if strings.TrimSpace(dst.SkillDir) == "" || skillDirIdentityKey(dst.SkillDir) == skillDirIdentityKey(runtimeEntry.SkillDir) {
		dst.SkillDir = firstNonEmptySkillString(dst.SkillDir, runtimeEntry.SkillDir)
	}
	dst.DirName = firstNonEmptySkillString(dst.DirName, runtimeEntry.DirName)
	dst.SourceProject = firstNonEmptySkillString(dst.SourceProject, runtimeEntry.SourceProject)
	dst.HubSkillID = firstNonEmptySkillString(dst.HubSkillID, runtimeEntry.HubSkillID)
	dst.HubVersion = firstNonEmptySkillString(dst.HubVersion, runtimeEntry.HubVersion)
	dst.TrustLevel = firstNonEmptySkillString(dst.TrustLevel, runtimeEntry.TrustLevel)
	dst.Publisher = firstNonEmptySkillString(dst.Publisher, runtimeEntry.Publisher)
}

func (r *SkillRunner) recordSkillUsageExperience(skill *corelib.NLSkillEntry, execErr error, runArgs map[string]interface{}) {
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
	if !success {
		finalOutcome = "failed"
		errorClass = cskill.ExtractErrorClass(formatExecErrorForStorage(execErr))
	}
	followUp := cskill.SkillExecutionFollowUp(success, errorClass)
	// Convert runArgs to string map for UsageTracker persistence.
	// These are used by RepairGate.Verify to replay historical executions.
	var argsStr map[string]string
	if len(runArgs) > 0 {
		argsStr = make(map[string]string, len(runArgs))
		for k, v := range runArgs {
			switch val := v.(type) {
			case string:
				argsStr[k] = val
			case nil:
				// skip
			default:
				argsStr[k] = fmt.Sprintf("%v", val)
			}
		}
	}
	r.executor.app.usageTracker.RecordExperience(coretool.ToolExperience{
		ToolName:     "skill:" + skill.Name,
		QueryTokens:  tokens,
		Success:      success,
		FollowUp:     followUp,
		TaskType:     "skill_execution",
		ErrorClass:   errorClass,
		FinalOutcome: finalOutcome,
		RunArgs:      argsStr,
	})

	// Invalidate manage_skill outcome records when a qualifying error class
	// indicates the skill environment is broken (config, dependencies, setup).
	// This ensures the router does not continue recommending manage_skill based
	// on stale success data from before the breakage occurred.
	if cskill.ShouldInvalidateManageSkillOutcomes(errorClass) {
		r.executor.app.usageTracker.InvalidateOutcomes("manage_skill",
			fmt.Sprintf("%s: %s", errorClass, skill.Name))
	}
}

// markSelfRepairPending sets SelfRepairPending=true on the most recent run
// for the given skill name. This tells the LLM (via appendSkillRunSummary)
// that a repair is in progress and it should wait before retrying.
// Also sets repairingSkills marker to block new StartRunForOwner calls.
func (r *SkillRunner) markSelfRepairPending(skillName string) {
	r.repairingSkills.Store(skillName, time.Now())
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
	r.maybeRepairSkillWithForce(entry, false)
}

// maybeRepairSkillWithForce is like maybeRepairSkill but force=true allows
// repair when CanForceAttemptRepair holds even if usage-rate thresholds fail
// (used by manage_skill trigger_repair).
func (r *SkillRunner) maybeRepairSkillWithForce(entry *corelib.NLSkillEntry, force bool) {
	if entry == nil {
		return
	}
	ok := cskill.ShouldAttemptRepair(entry)
	if !ok && force {
		ok = cskill.CanForceAttemptRepair(entry)
	}
	if !ok {
		r.repairingSkills.Delete(entry.Name)
		return
	}
	// Ensure the repair marker is cleared when this goroutine exits,
	// regardless of success/failure/panic.
	defer r.repairingSkills.Delete(entry.Name)
	repairer := r.buildSkillRepairer()
	if repairer == nil {
		return
	}

	log.Printf("[skill-repair-gui] attempting repair for %q (attempt %d, usage=%d, success=%d)",
		entry.Name, entry.RepairAttemptCount+1, entry.UsageCount, entry.SuccessCount)

	// Build rich repair context: error class + param contract (declared vs actual).
	var runArgs map[string]string
	if r.executor != nil && r.executor.app != nil && r.executor.app.usageTracker != nil {
		if argsList := r.executor.app.usageTracker.RecentRunArgs("skill:"+entry.Name, 1); len(argsList) > 0 {
			runArgs = argsList[len(argsList)-1]
		}
	}
	repairCtx := cskill.NewRepairContext(entry, runArgs)

	result, err := cskill.AttemptRepairWithContext(repairer, entry, repairCtx)
	if err != nil {
		log.Printf("[skill-repair-gui] repair failed for %q: %v", entry.Name, err)
		return
	}

	// RepairGate verification: replay historical args in sandbox before applying.
	if r.evolutionPipeline != nil && r.evolutionPipeline.Gate != nil && result.Repaired && len(result.NewSteps) > 0 {
		var historicalArgs []map[string]string
		if r.executor != nil && r.executor.app != nil && r.executor.app.usageTracker != nil {
			historicalArgs = r.executor.app.usageTracker.RecentRunArgs("skill:"+entry.Name, 3)
		}
		if len(historicalArgs) > 0 {
			gateCtx, gateCancel := context.WithTimeout(context.Background(), 3*time.Minute)
			nlSteps := make([]corelib.NLSkillStep, len(result.NewSteps))
			for i, s := range result.NewSteps {
				nlSteps[i] = corelib.NLSkillStep{Action: s.Action, Params: s.Params, OnError: s.OnError}
			}
			gateResult, gateErr := r.evolutionPipeline.Gate.Verify(gateCtx, entry, nlSteps, historicalArgs)
			gateCancel()
			if gateErr != nil {
				// Fail closed: a broken gate must not silently accept a repair.
				log.Printf("[skill-repair-gui] gate verification error for %q: %v — rejecting repair", entry.Name, gateErr)
				return
			}
			if gateResult == nil || !gateResult.Passed {
				reason := "nil gate result"
				if gateResult != nil {
					reason = gateResult.Reason
				}
				log.Printf("[skill-repair-gui] gate REJECTED repair for %q: %s", entry.Name, reason)
				return
			}
			log.Printf("[skill-repair-gui] gate PASSED repair for %q: %s", entry.Name, gateResult.Reason)
		}
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

	app := (*App)(nil)
	if r.executor != nil {
		app = r.executor.app
	}
	var repairReport *cskill.ScanReport
	if app == nil || !app.isRiskGuardrailOffMode() {
		repairReport = r.scanRepairedSkill(entry)
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
		r.executor.app.emitEvent(EventSkillRepaired)
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
	if r == nil || r.executor == nil || r.executor.app == nil {
		return
	}
	// Single policy with import/UI/IM paths (invalidate list + scanner, emit event).
	r.executor.app.refreshSkillIndexesAfterMutation(skillName)
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
	if isShellBrowserAutomationSkillEntry(*entry) {
		return browserAutomationSkillRejectedError(entry.Name)
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
	ctx := llm.WithRequestTrace(context.Background(), llm.RequestTrace{Caller: "skill-repair"})
	resp, err := doSimpleLLMRequest(ctx, r.cfg, ifaces, r.client, 60*time.Second)
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
		r.executor.app.emitEvent(EventSkillUsageUpdated)
	}
}

var skillStepProcessEnvMu sync.Mutex

func installSkillStepProcessEnv(action string, extraEnv map[string]string) func() {
	return installSkillStepProcessEnvForRun("", "", action, extraEnv)
}

func installSkillStepProcessEnvForRun(runID, ownerID, action string, extraEnv map[string]string) func() {
	actionKind := classifySkillStepAction(action)
	if !actionKind.UsesManagedProcessEnv() && !actionKind.UsesLegacySessionProcessEnv() || len(extraEnv) == 0 {
		return func() {}
	}
	waitStart := time.Now()
	skillStepProcessEnvMu.Lock()
	if waited := time.Since(waitStart); waited > 200*time.Millisecond {
		log.Printf("[skill-runner] process env lock waited %s run=%s owner=%q action=%s env_keys=%d", waited.Round(time.Millisecond), runID, ownerID, action, len(extraEnv))
	}
	lockAcquiredAt := time.Now()
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
			if held := time.Since(lockAcquiredAt); held > time.Second {
				log.Printf("[skill-runner] process env lock held %s run=%s owner=%q action=%s env_keys=%d", held.Round(time.Millisecond), runID, ownerID, action, len(extraEnv))
			}
			skillStepProcessEnvMu.Unlock()
		})
	}
}

// tryAutoUpload attempts to upload to SkillMarket after a skill run finishes.
// Also reports execution outcome to HubCenter for global quality signals.
func (r *SkillRunner) tryAutoUpload(skill *corelib.NLSkillEntry, run *skillRun) {
	if r == nil || r.executor == nil || r.executor.app == nil || skill == nil {
		return
	}
	if skill.SkillDir == "" {
		return
	}

	r.mu.RLock()
	status := run.status.LifecycleStatus()
	hasErr := false
	stepTotal := len(run.status.Steps)
	stepSuccessCount := 0
	outputSizeBytes := 0
	for _, st := range run.status.Steps {
		if st.IsFailed() {
			hasErr = true
		}
		if st.LifecycleStatus() == skillStepStatusSuccess {
			stepSuccessCount++
		}
		outputSizeBytes += len(st.Output)
	}
	durationMs := run.status.DurationMs
	runID := run.status.RunID
	r.mu.RUnlock()

	result := &SkillExecutionResult{
		Success:          status == skillRunStatusSuccess,
		HasError:         hasErr,
		OutputQuality:    "basic",
		StepTotal:        stepTotal,
		StepSuccessCount: stepSuccessCount,
		OutputSizeBytes:  outputSizeBytes,
		DurationMs:       durationMs,
		TimeoutMs:        int64(r.runDefaultTimeoutSec(run)) * 1000,
	}
	if status == skillRunStatusSuccess && !hasErr {
		result.OutputQuality = "good"
	}

	score := EvaluateSkillExecution(result)

	// Report execution outcome to HubCenter (async, throttled, idempotent).
	// This closes the feedback loop: local execution quality → global AvgRating.
	// Independent of uploadTrigger — always report when possible.
	if r.outcomeReporter != nil {
		r.outcomeReporter.ReportOutcome(skill, runID, score)
	}

	// Auto-upload logic requires uploadTrigger.
	if r.uploadTrigger == nil {
		return
	}

	localHash := skillDirHash(skill.SkillDir)
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

	case skillStepActionControlSession:
		return "", fmt.Errorf("external coding-session control steps are disabled; coding tasks must run through CodingSubAgent")

	case skillStepActionCallMCPTool:
		serverRef, _ := step.Params["server_id"].(string)
		toolName, _ := step.Params["tool_name"].(string)
		if isDisabledExternalCodingSessionTool(toolName) {
			return "", fmt.Errorf("external coding-session MCP target %q is disabled", toolName)
		}
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
			return r.executor.app.localMCPManager.CallToolForOwner(strings.TrimSpace(nonEmptyStringFromAny(step.Params["_skill_owner_id"])), resolvedID, toolName, args)
		}
		if r.executor.mcpRegistry == nil {
			return "", fmt.Errorf("MCP registry not initialized")
		}
		return r.executor.mcpRegistry.CallToolForOwner(strings.TrimSpace(nonEmptyStringFromAny(step.Params["_skill_owner_id"])), resolvedID, toolName, args)

	case skillStepActionBash:
		command, _ := step.Params["command"].(string)
		if command == "" {
			return "", fmt.Errorf("missing command parameter")
		}
		return runBashStepWithContextFull(ctx, command, step.Params, skillDir, r.executor.app, r.runDefaultTimeoutSecForID(runID))

	case skillStepActionCraftTool:
		if r.executor == nil || r.executor.app == nil {
			return "", fmt.Errorf("app not initialized")
		}
		return executeCraftToolCoreWithContext(ctx, r.executor.app, nil, step.Params, nil)

	case skillStepActionPoll:
		return r.executePollStep(ctx, step, skillDir, r.runDefaultTimeoutSecForID(runID))

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

func mergeSkillRuntimeExtraEnv(base, runtimeEnv map[string]string) map[string]string {
	return cskill.MergeRuntimeExtraEnv(base, runtimeEnv)
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
	return runBashStepWithContextFull(ctx, command, params, skillDir, app, 0)
}

func runBashStepWithContextFull(ctx context.Context, command string, params map[string]interface{}, skillDir string, app *App, defaultTimeoutSec int) (string, error) {
	// Strip UTF-8 BOM if present. SKILL.md files saved with BOM can leak
	// the BOM bytes into the command string, causing cmd.exe to fail with
	// "'@echo" is not recognized as an internal or external command.
	command = strings.TrimPrefix(command, "\xef\xbb\xbf")

	defaultTimeout := corelib.NormalizeSkillRunnerTimeoutSec(defaultTimeoutSec)
	if defaultTimeoutSec <= 0 && app != nil {
		if cfg, err := app.LoadConfig(); err == nil {
			defaultTimeout = corelib.NormalizeSkillRunnerTimeoutSec(cfg.SkillRunnerTimeoutSec)
		}
	}
	timeout := cskill.RunnerStepTimeoutSecondsWithMin(params, defaultTimeout, corelib.MinSkillRunnerTimeoutSec, corelib.MaxSkillRunnerTimeoutSec)

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

	// Map bare `pip`/`pip3` commands to `python -m pip` when pip.exe is not
	// available on PATH. This handles the common case where maclaw's bundled
	// Python has pip as a module but no standalone pip.exe.
	command = mapBarePipToModule(command)

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
	// Warn if the step's declared timeout is capped by the parent (global)
	// context deadline. Go's context.WithTimeout cannot exceed the parent's
	// deadline, so the step will be killed earlier than its declared timeout.
	// This makes timeout failures diagnosable: the LLM sees a concrete warning
	// explaining why the step was killed before its own timeout expired.
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		stepDuration := time.Duration(timeout) * time.Second
		if stepDuration > remaining+time.Second {
			log.Printf("[skill-runner] step timeout %ds exceeds remaining global timeout %s — step will be capped at global deadline",
				timeout, remaining.Round(time.Second))
		}
	}
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
			scriptContent := "@echo off\r\n" +
				`if exist "%SystemRoot%\System32\chcp.com" ("%SystemRoot%\System32\chcp.com" 65001 >nul 2>nul) else (chcp 65001 >nul 2>nul)` + "\r\n" +
				cmdSafeCommand + "\r\n"
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

	// Live output streaming: if a liveOutput ring buffer is provided via params,
	// tee stdout/stderr to both the full buffer and the live tail for real-time
	// progress visibility during long-running skill steps.
	var stdout, stderr bytes.Buffer
	var liveOut *skillRunLiveOutput
	if lo, ok := params["_live_output"].(*skillRunLiveOutput); ok && lo != nil {
		liveOut = lo
	}
	if liveOut != nil {
		// Use pipe + goroutine to scan lines in real time
		stdoutPipe, pipeErr1 := cmd.StdoutPipe()
		stderrPipe, pipeErr2 := cmd.StderrPipe()
		if pipeErr1 != nil || pipeErr2 != nil {
			// Pipe creation failed — fall through to non-streaming mode
			log.Printf("[skill-runner] live output pipe failed (stdout_err=%v stderr_err=%v), falling back to buffer mode", pipeErr1, pipeErr2)
			liveOut = nil
		} else {
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				scanner := bufio.NewScanner(stdoutPipe)
				scanner.Buffer(make([]byte, 256*1024), 256*1024)
				for scanner.Scan() {
					line := scanner.Text()
					stdout.WriteString(line)
					stdout.WriteByte('\n')
					trimmed := strings.TrimSpace(line)
					if trimmed != "" {
						liveOut.Append(trimmed)
					}
				}
			}()
			go func() {
				defer wg.Done()
				scanner := bufio.NewScanner(stderrPipe)
				scanner.Buffer(make([]byte, 256*1024), 256*1024)
				for scanner.Scan() {
					line := scanner.Text()
					stderr.WriteString(line)
					stderr.WriteByte('\n')
					trimmed := strings.TrimSpace(line)
					if trimmed != "" {
						liveOut.Append(trimmed)
					}
				}
			}()

			startTime := time.Now()
			runID, _ := params["_skill_run_id"].(string)
			ownerID, _ := params["_skill_owner_id"].(string)
			log.Printf("[skill-runner] bash exec: run=%s owner=%q shell=%s workDir=%s timeout=%ds live_output=true", strings.TrimSpace(runID), strings.TrimSpace(ownerID), filepath.Base(shellName), workDir, timeout)
			err := cmd.Start()
			if err == nil {
				// Monitor stepCtx for timeout/cancellation — kill the process tree
				// so pipe readers get EOF and wg.Wait() unblocks.
				done := make(chan struct{})
				go func() {
					select {
					case <-done:
					case <-stepCtx.Done():
						coretool.TerminateCommandTree(cmd)
					}
				}()
				wg.Wait()
				err = cmd.Wait()
				close(done)
				if stepCtx.Err() != nil && err == nil {
					err = stepCtx.Err()
				}
			}
			elapsed := time.Since(startTime)
			sanitizeUTF8Buffer(&stdout)
			sanitizeUTF8Buffer(&stderr)
			isTimeout := stepCtx.Err() == context.DeadlineExceeded
			return formatBashStepResult(command, shellName, shellArgs, tmpScript, workDir, timeout, elapsed, &stdout, &stderr, err, isTimeout)
		}
	}

	// Non-streaming fallback (no live output buffer, or pipe creation failed)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	runID, _ := params["_skill_run_id"].(string)
	ownerID, _ := params["_skill_owner_id"].(string)
	log.Printf("[skill-runner] bash exec: run=%s owner=%q shell=%s workDir=%s timeout=%ds", strings.TrimSpace(runID), strings.TrimSpace(ownerID), filepath.Base(shellName), workDir, timeout)
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
	return formatBashStepResult(command, shellName, shellArgs, tmpScript, workDir, timeout, elapsed, &stdout, &stderr, err, isTimeout)
}

// formatBashStepResult builds the final output string and error for a bash step.
func formatBashStepResult(command, shellName string, shellArgs []string, tmpScript, workDir string, timeout int, elapsed time.Duration, stdout, stderr *bytes.Buffer, err error, isTimeout bool) (string, error) {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("shell: %s\n", filepath.Base(shellName)))
	b.WriteString(fmt.Sprintf("elapsed: %s\n", elapsed.Round(time.Millisecond)))
	b.WriteString(fmt.Sprintf("%s\n", workDir))
	if tmpScript != "" {
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

// mapBarePipToModule replaces bare `pip`/`pip3` command invocations with
// `python -m pip` when pip.exe is not available on PATH. This is critical for
// maclaw's bundled Python environment where pip exists only as a module
// (no standalone pip.exe in Scripts/), and for SkillHub-installed skills that
// use bare `pip install ...` commands.
//
// Only replaces when pip/pip3 appears at the start of a command line or after
// a shell operator (&&, ||, ;). Does NOT replace `pip` inside paths or arguments.
func mapBarePipToModule(command string) string {
	return cskill.MapBarePipToModule(command)
}

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
	newDir := corelib.MaclawBaseDir()
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
func (r *SkillRunner) executePollStep(ctx context.Context, step corelib.NLSkillStep, skillDir string, defaultTimeoutSec int) (string, error) {
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
		output, execErr := runBashStepWithContextFull(pollCtx, command, step.Params, skillDir, r.executor.app, defaultTimeoutSec)
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
