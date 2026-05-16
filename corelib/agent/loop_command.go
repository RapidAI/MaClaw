package agent

// loop_command.go defines the /loop command engine — a goal-driven verification
// loop that repeatedly modifies code and runs a verification command until it
// passes (exit 0) or max iterations are exhausted.
//
// This is NOT a workaround or wrapper around RunLoop. It IS a RunLoop consumer
// that adds a verification-feedback cycle on top. Each iteration:
//
//   1. RunLoop executes with a focused prompt (goal + last verification output)
//   2. After RunLoop returns (LLM made changes), run the verification command
//   3. If exit 0 → success. If non-zero → inject stderr/stdout as feedback,
//      start a new RunLoop iteration.
//
// The verification command is the SINGLE SOURCE OF TRUTH for success/failure.
// The LLM's self-assessment is irrelevant — only the exit code matters.

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// LoopCommandConfig defines the parameters for a /loop execution.
type LoopCommandConfig struct {
	// Goal is the user's description of what should be achieved.
	Goal string

	// VerifyCmd is the shell command whose exit code determines success.
	// Exit 0 = goal achieved. Non-zero = keep iterating.
	VerifyCmd string

	// MaxIterations is the maximum number of modify→verify cycles.
	// Each cycle may internally run multiple LLM iterations (via RunLoop).
	// Default: 10.
	MaxIterations int

	// WorkDir is the working directory for the verification command.
	// If empty, uses the current directory.
	WorkDir string

	// VerifyTimeout is the timeout for each verification command execution.
	// Default: 120 seconds.
	VerifyTimeout time.Duration

	// MaxLLMIterationsPerCycle limits how many LLM iterations RunLoop can
	// use within a single modify cycle. Default: 30.
	MaxLLMIterationsPerCycle int
}

// LoopCommandState tracks the execution state of a /loop command.
type LoopCommandState struct {
	Config     LoopCommandConfig
	Iterations []LoopIterationRecord
	Status     LoopCommandStatus
	StartedAt  time.Time
	EndedAt    time.Time
}

// LoopCommandStatus represents the terminal state of a /loop execution.
type LoopCommandStatus string

const (
	LoopStatusRunning   LoopCommandStatus = "running"
	LoopStatusSucceeded LoopCommandStatus = "succeeded"
	LoopStatusFailed    LoopCommandStatus = "failed"
	LoopStatusCancelled LoopCommandStatus = "cancelled"
)

// LoopIterationRecord captures the outcome of a single modify→verify cycle.
type LoopIterationRecord struct {
	Index        int
	LLMResult    LoopResult // what RunLoop produced
	VerifyResult VerifyCommandResult
	Duration     time.Duration
}

// VerifyCommandResult captures the outcome of running the verification command.
type VerifyCommandResult struct {
	ExitCode int
	Stdout   string // truncated to MaxVerifyOutputLen
	Stderr   string // truncated to MaxVerifyOutputLen
	Duration time.Duration
	TimedOut bool
}

// Passed returns true if the verification command exited with code 0.
func (v VerifyCommandResult) Passed() bool {
	return v.ExitCode == 0 && !v.TimedOut
}

// CombinedOutput returns stdout+stderr for injection into the next LLM prompt.
func (v VerifyCommandResult) CombinedOutput() string {
	var parts []string
	if v.Stdout != "" {
		parts = append(parts, v.Stdout)
	}
	if v.Stderr != "" {
		parts = append(parts, v.Stderr)
	}
	return strings.Join(parts, "\n")
}

const (
	// MaxVerifyOutputLen is the maximum length of stdout/stderr captured
	// from the verification command. Longer output is truncated from the
	// beginning (tail is preserved — errors are usually at the end).
	MaxVerifyOutputLen = 4000

	defaultLoopMaxIterations         = 10
	defaultLoopVerifyTimeout         = 120 * time.Second
	defaultLoopMaxLLMItersPerCycle   = 30
)

// LoopVerifyTimeoutFromSeconds converts seconds to a Duration for VerifyTimeout.
func LoopVerifyTimeoutFromSeconds(seconds int) time.Duration {
	return time.Duration(seconds) * time.Second
}

// NormalizeLoopConfig fills in defaults for zero-valued fields.
func NormalizeLoopConfig(cfg LoopCommandConfig) LoopCommandConfig {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = defaultLoopMaxIterations
	}
	if cfg.VerifyTimeout <= 0 {
		cfg.VerifyTimeout = defaultLoopVerifyTimeout
	}
	if cfg.MaxLLMIterationsPerCycle <= 0 {
		cfg.MaxLLMIterationsPerCycle = defaultLoopMaxLLMItersPerCycle
	}
	return cfg
}

// LoopCommandCallbacks is the interface that the host must implement to
// provide LLM and tool capabilities to the loop engine.
type LoopCommandCallbacks interface {
	// RunModifyCycle executes one LLM-driven modification cycle.
	// The prompt includes the goal and the last verification output.
	// Returns the RunLoop result.
	RunModifyCycle(ctx context.Context, prompt string, iteration int) LoopResult

	// OnIterationStart is called at the beginning of each modify→verify cycle.
	OnIterationStart(iteration, maxIterations int)

	// OnVerifyStart is called before running the verification command.
	OnVerifyStart(cmd string, iteration int)

	// OnVerifyDone is called after the verification command completes.
	OnVerifyDone(result VerifyCommandResult, iteration int)

	// OnSuccess is called when the verification command passes.
	OnSuccess(state *LoopCommandState)

	// OnFailure is called when all iterations are exhausted without success.
	OnFailure(state *LoopCommandState)

	// IsCancelled returns true if the loop should be terminated.
	IsCancelled() bool
}

// RunLoopCommand executes the goal-driven verification loop.
// This is the core engine — it does not know about GUI/TUI/IM specifics.
func RunLoopCommand(ctx context.Context, cfg LoopCommandConfig, cb LoopCommandCallbacks) *LoopCommandState {
	cfg = NormalizeLoopConfig(cfg)

	// Derive a cancellable context from the IsCancelled() polling signal.
	// This allows in-flight verify commands and LLM calls to be interrupted
	// immediately when the user cancels, rather than waiting for the next
	// iteration boundary.
	loopCtx, loopCancel := context.WithCancel(ctx)
	defer loopCancel()
	go func() {
		// Poll IsCancelled at 200ms intervals and cancel the context when true.
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-ticker.C:
				if cb.IsCancelled() {
					loopCancel()
					return
				}
			}
		}
	}()

	state := &LoopCommandState{
		Config:    cfg,
		Status:    LoopStatusRunning,
		StartedAt: time.Now(),
	}

	log.Printf("[loop-command] starting: goal=%q verify=%q max_iters=%d work_dir=%q",
		truncateForLog(cfg.Goal, 80), cfg.VerifyCmd, cfg.MaxIterations, cfg.WorkDir)

	for i := 0; i < cfg.MaxIterations; i++ {
		if cb.IsCancelled() || loopCtx.Err() != nil {
			state.Status = LoopStatusCancelled
			state.EndedAt = time.Now()
			log.Printf("[loop-command] cancelled at iteration %d", i)
			return state
		}

		iterStart := time.Now()
		cb.OnIterationStart(i, cfg.MaxIterations)

		// Build the prompt for this cycle.
		prompt := buildLoopCyclePrompt(cfg, state, i)

		// Run the LLM modification cycle.
		llmResult := cb.RunModifyCycle(loopCtx, prompt, i)

		if cb.IsCancelled() || loopCtx.Err() != nil {
			state.Status = LoopStatusCancelled
			state.EndedAt = time.Now()
			return state
		}

		// Run the verification command.
		cb.OnVerifyStart(cfg.VerifyCmd, i)
		verifyResult := ExecuteVerifyCommand(loopCtx, cfg.VerifyCmd, cfg.WorkDir, cfg.VerifyTimeout)

		// Check if cancellation happened during verify.
		if loopCtx.Err() != nil && !verifyResult.Passed() {
			state.Status = LoopStatusCancelled
			state.EndedAt = time.Now()
			return state
		}

		cb.OnVerifyDone(verifyResult, i)

		record := LoopIterationRecord{
			Index:        i,
			LLMResult:    llmResult,
			VerifyResult: verifyResult,
			Duration:     time.Since(iterStart),
		}
		state.Iterations = append(state.Iterations, record)

		log.Printf("[loop-command] iteration %d/%d: exit_code=%d passed=%v duration=%v",
			i+1, cfg.MaxIterations, verifyResult.ExitCode, verifyResult.Passed(), record.Duration)

		if verifyResult.Passed() {
			state.Status = LoopStatusSucceeded
			state.EndedAt = time.Now()
			cb.OnSuccess(state)
			log.Printf("[loop-command] succeeded after %d iterations (total %v)",
				i+1, state.EndedAt.Sub(state.StartedAt))
			return state
		}
	}

	// All iterations exhausted without success.
	state.Status = LoopStatusFailed
	state.EndedAt = time.Now()
	cb.OnFailure(state)
	log.Printf("[loop-command] failed after %d iterations (total %v)",
		cfg.MaxIterations, state.EndedAt.Sub(state.StartedAt))
	return state
}

// ExecuteVerifyCommand runs a shell command and captures its output.
// This is exported for testing and reuse.
func ExecuteVerifyCommand(ctx context.Context, command, workDir string, timeout time.Duration) VerifyCommandResult {
	if timeout <= 0 {
		timeout = defaultLoopVerifyTimeout
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(cmdCtx, "cmd", "/c", command)
	} else {
		cmd = exec.CommandContext(cmdCtx, "sh", "-c", command)
	}

	if workDir != "" {
		cmd.Dir = workDir
	}

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	err := cmd.Run()
	elapsed := time.Since(startTime)

	result := VerifyCommandResult{
		Stdout:   truncateVerifyOutput(stdout.String()),
		Stderr:   truncateVerifyOutput(stderr.String()),
		Duration: elapsed,
	}

	if cmdCtx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.ExitCode = -1
		result.Duration = timeout
		return result
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
			if result.Stderr == "" {
				result.Stderr = err.Error()
			}
		}
	}

	return result
}

// --- Prompt construction ---

func buildLoopCyclePrompt(cfg LoopCommandConfig, state *LoopCommandState, iteration int) string {
	var sb strings.Builder

	sb.WriteString("## Goal\n\n")
	sb.WriteString(cfg.Goal)
	sb.WriteString("\n\n")

	sb.WriteString("## Verification Command\n\n")
	sb.WriteString("After you make changes, the following command will be run automatically:\n")
	sb.WriteString("```\n")
	sb.WriteString(cfg.VerifyCmd)
	sb.WriteString("\n```\n")
	sb.WriteString("Your changes are successful ONLY when this command exits with code 0.\n\n")

	sb.WriteString(fmt.Sprintf("## Iteration %d/%d\n\n", iteration+1, cfg.MaxIterations))

	if iteration == 0 {
		sb.WriteString("This is the first iteration. Analyze the goal, read relevant files, ")
		sb.WriteString("make the necessary changes, and ensure the verification command will pass.\n")
	} else {
		// Include the last verification output as feedback.
		lastRecord := state.Iterations[len(state.Iterations)-1]
		sb.WriteString("The previous attempt **failed**. Here is the verification output:\n\n")
		sb.WriteString("```\n")
		output := lastRecord.VerifyResult.CombinedOutput()
		if output == "" {
			output = fmt.Sprintf("(exit code %d, no output)", lastRecord.VerifyResult.ExitCode)
		}
		sb.WriteString(output)
		sb.WriteString("\n```\n\n")

		if lastRecord.VerifyResult.TimedOut {
			sb.WriteString("⚠️ The verification command **timed out**. The changes may have caused an infinite loop or hang.\n\n")
		}

		sb.WriteString("Analyze the error output above, identify what went wrong, and fix it. ")
		sb.WriteString("Focus on the specific errors — do not rewrite everything from scratch.\n")
	}

	sb.WriteString("\n## Rules\n\n")
	sb.WriteString("- Make targeted changes to fix the issue\n")
	sb.WriteString("- Do NOT run the verification command yourself — it runs automatically after you finish\n")
	sb.WriteString("- When you are done making changes, simply stop calling tools (return your final message)\n")
	sb.WriteString("- If you believe the goal is impossible to achieve, explain why in your response\n")

	return sb.String()
}

// --- Helpers ---

func truncateVerifyOutput(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= MaxVerifyOutputLen {
		return s
	}
	// Keep the tail (errors are usually at the end).
	return "...(truncated)\n" + s[len(s)-MaxVerifyOutputLen:]
}

func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
