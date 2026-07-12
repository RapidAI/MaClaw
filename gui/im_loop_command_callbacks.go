package main

// im_loop_command_callbacks.go implements agent.LoopCommandCallbacks for the
// GUI. Each modify cycle uses a fresh RunLoop with a clean coding context
// (same model as CodingSubAgent — minimal system prompt, only file/shell tools).

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// guiLoopCommandCallbacks implements agent.LoopCommandCallbacks.
type guiLoopCommandCallbacks struct {
	handler    *IMMessageHandler
	llmCfg     corelib.MaclawLLMConfig
	httpClient *http.Client
	projectDir string
	onProgress coretool.ProgressCallback
	onToken    llm.TokenCallback
	userID     string
	cancelCh   chan struct{}
	cancelOnce sync.Once
	cancelled  bool // fallback for nil cancelCh (test-only zero-value structs)
}

func (c *guiLoopCommandCallbacks) RunModifyCycle(ctx context.Context, prompt string, iteration int) agent.LoopResult {
	cb := &loopCycleCallbacks{
		parent:     c,
		prompt:     prompt,
		iteration:  iteration,
		projectDir: c.projectDir,
	}

	// RunLoop with clean context — no history, fresh conversation each cycle.
	result := agent.RunLoop(cb, prompt, nil, c.httpClient)
	if c.handler != nil {
		accumulateLoopResultUsage(c.handler.app, c.llmCfg, result)
	}
	return result
}

func (c *guiLoopCommandCallbacks) OnIterationStart(iteration, maxIterations int) {
	msg := fmt.Sprintf("Loop 迭代 %d/%d", iteration+1, maxIterations)
	log.Printf("[loop-command] %s", msg)
	if c.onProgress != nil {
		c.onProgress(msg)
	}
}

func (c *guiLoopCommandCallbacks) OnVerifyStart(cmd string, iteration int) {
	msg := fmt.Sprintf("运行验证命令: %s", cmd)
	if c.onProgress != nil {
		c.onProgress(msg)
	}
}

func (c *guiLoopCommandCallbacks) OnVerifyDone(result agent.VerifyCommandResult, iteration int) {
	if result.Passed() {
		if c.onProgress != nil {
			c.onProgress("验证通过!")
		}
	} else {
		msg := fmt.Sprintf("验证失败 (exit %d)", result.ExitCode)
		if result.TimedOut {
			msg = "验证命令超时"
		}
		if c.onProgress != nil {
			c.onProgress(msg)
		}
	}
}

func (c *guiLoopCommandCallbacks) OnSuccess(state *agent.LoopCommandState) {
	log.Printf("[loop-command] success after %d iterations", len(state.Iterations))
}

func (c *guiLoopCommandCallbacks) OnFailure(state *agent.LoopCommandState) {
	log.Printf("[loop-command] failed after %d iterations", len(state.Iterations))
}

func (c *guiLoopCommandCallbacks) IsCancelled() bool {
	if c.cancelCh != nil {
		select {
		case <-c.cancelCh:
			return true
		default:
			return false
		}
	}
	return c.cancelled
}

// CancelCh implements agent.CancelChanneler for zero-CPU cancel propagation.
func (c *guiLoopCommandCallbacks) CancelCh() <-chan struct{} {
	return c.cancelCh
}

// Cancel marks the loop as cancelled. Called from the main handler when
// the user sends /cancel or clicks the stop button.
func (c *guiLoopCommandCallbacks) Cancel() {
	c.cancelled = true
	if c.cancelCh != nil {
		c.cancelOnce.Do(func() { close(c.cancelCh) })
	}
}

// ---------------------------------------------------------------------------
// loopCycleCallbacks implements agent.LoopCallbacks for a single modify cycle.
// This is the inner RunLoop that the LLM uses to read/write files.
// ---------------------------------------------------------------------------

type loopCycleCallbacks struct {
	parent     *guiLoopCommandCallbacks
	prompt     string
	iteration  int
	projectDir string
}

func (c *loopCycleCallbacks) GetLLMConfig() corelib.MaclawLLMConfig {
	return c.parent.llmCfg
}

func (c *loopCycleCallbacks) GetMaxIterations() int {
	maxIter := c.parent.handler.getLoopMaxLLMIterations()
	if maxIter <= 0 {
		maxIter = 30
	}
	return config.EffectiveMaxIterations(maxIter)
}

func (c *loopCycleCallbacks) BuildSystemPrompt(userText string, isFirstTurn bool) string {
	var sb strings.Builder

	sb.WriteString("You are a coding assistant executing a goal-driven verification loop.\n\n")
	sb.WriteString("## Your Role\n\n")
	sb.WriteString("- Read files, understand the codebase, and make targeted changes\n")
	sb.WriteString("- After you finish making changes, stop calling tools and return a brief summary\n")
	sb.WriteString("- The verification command will be run AUTOMATICALLY after you finish — do NOT run it yourself\n")
	sb.WriteString("- Focus on making the minimum changes needed to pass the verification\n\n")

	if c.projectDir != "" {
		sb.WriteString(fmt.Sprintf("## Working Directory\n\n`%s`\n\n", c.projectDir))
	}

	sb.WriteString("## Available Tools\n\n")
	sb.WriteString("- `read_file`: Read file contents\n")
	sb.WriteString("- `write_file`: Create or overwrite a file; no content length limit. For very large content (>6000 chars), consider splitting into overwrite + append chunks\n")
	sb.WriteString("- `edit_file`: Make targeted edits to an existing file; keep old/new inline content under 1800 characters and split larger edits\n")
	sb.WriteString("- `bash`: Run shell commands (for exploration, NOT for running the verify command)\n")
	sb.WriteString("- `list_directory`: List directory contents\n")
	sb.WriteString("- `search_files`: Search for files by name pattern\n")
	sb.WriteString("- `grep_search`: Search file contents with regex\n\n")

	sb.WriteString("## Rules\n\n")
	sb.WriteString("- Do NOT run the verification command yourself\n")
	sb.WriteString("- Make focused, minimal changes\n")
	sb.WriteString("- If you need to understand the codebase first, read relevant files before editing\n")
	sb.WriteString("- When done, return a brief summary of what you changed and why\n")

	return sb.String()
}

func (c *loopCycleCallbacks) BuildTools(userText string) []map[string]interface{} {
	// Minimal tool set for coding — same philosophy as CodingSubAgent.
	tools := []map[string]interface{}{
		tooldef.BuildToolDef("read_file", "Read the contents of a file.", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":   map[string]interface{}{"type": "string", "description": "File path to read"},
				"offset": map[string]interface{}{"type": "integer", "description": "Read last N lines (tail mode)"},
			},
			"required": []string{"path"},
		}),
		tooldef.BuildToolDef("write_file", "Create or overwrite a file with the given content. No length limit; system handles large content automatically. For very large files (>6000 chars), consider splitting into overwrite + append chunks.", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string", "description": "File path to write"},
				"content": map[string]interface{}{"type": "string", "description": "File content. No length limit; you can write complete scripts or documents in a single call."},
				"mode":    map[string]interface{}{"type": "string", "description": "Write mode: overwrite for first chunk, append for later chunks."},
			},
			"required": []string{"path", "content"},
		}),
		tooldef.BuildToolDef("edit_file", "Make targeted edits to an existing file using search/replace.", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":        map[string]interface{}{"type": "string", "description": "File path to edit"},
				"old_content": map[string]interface{}{"type": "string", "description": "Exact text to find. Keep under 1800 characters; split large edits into smaller calls.", "maxLength": codingSubAgentInlineContentLimit},
				"new_content": map[string]interface{}{"type": "string", "description": "Replacement text. Keep under 1800 characters; split large edits into smaller calls.", "maxLength": codingSubAgentInlineContentLimit},
			},
			"required": []string{"path", "old_content", "new_content"},
		}),
		tooldef.BuildToolDef("bash", "Run read-only diagnostics and exploration commands. Do not use bash to edit files, create/delete/move files, rewrite Git state, stage/commit/apply patches, or run the verify command yourself.", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command":  map[string]interface{}{"type": "string", "description": "Shell command to execute"},
				"work_dir": map[string]interface{}{"type": "string", "description": "Working directory"},
			},
			"required": []string{"command"},
		}),
		tooldef.BuildToolDef("list_directory", "List files and directories.", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string", "description": "Directory path to list"},
			},
			"required": []string{"path"},
		}),
		tooldef.BuildToolDef("search_files", "Search for files by name pattern.", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{"type": "string", "description": "File name pattern (glob)"},
				"path":    map[string]interface{}{"type": "string", "description": "Directory to search in"},
			},
			"required": []string{"pattern"},
		}),
		tooldef.BuildToolDef("grep_search", "Search file contents with regex.", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{"type": "string", "description": "Regex pattern to search for"},
				"path":    map[string]interface{}{"type": "string", "description": "Directory or file to search in"},
			},
			"required": []string{"pattern"},
		}),
	}
	return tools
}

func (c *loopCycleCallbacks) ExecuteTool(name, argsJSON string) string {
	if c == nil || c.parent == nil || c.parent.handler == nil {
		return fmt.Sprintf("Unknown tool: %s", name)
	}
	// Delegate to the host's tool implementations.
	policyUserID := c.parent.handler.workflowPolicyUserID(strings.TrimSpace(c.parent.userID))
	ctx, cancel := c.toolContext()
	defer cancel()
	return c.parent.handler.executeToolDetailedWithRuntimeContext(ctx, policyUserID, strings.TrimSpace(policyUserID) != "", "", name, argsJSON, "", nil).Text
}

func (c *loopCycleCallbacks) toolContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	if c == nil || c.parent == nil || c.parent.cancelCh == nil {
		return ctx, cancel
	}
	// Zero-CPU cancel propagation via channel — no polling goroutine needed.
	go func() {
		select {
		case <-c.parent.cancelCh:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func (c *loopCycleCallbacks) IsToolAllowed(name string) bool {
	if c == nil || c.parent == nil || c.parent.handler == nil {
		return true
	}
	if c.disallowLoopCommandBashForDocOnly(name) {
		return false
	}
	return c.parent.handler.isWorkflowToolAllowedForOwner(c.parent.handler.workflowPolicyUserID(strings.TrimSpace(c.parent.userID)), name)
}

func (c *loopCycleCallbacks) IsToolCallAllowed(name, argsJSON string) (bool, string) {
	if c == nil || c.parent == nil || c.parent.handler == nil {
		return true, ""
	}
	if c.disallowLoopCommandBashForDocOnly(name) {
		return false, "bash is not allowed during doc-only workflow command cycles"
	}
	return c.parent.handler.isWorkflowToolCallAllowedForOwner(c.parent.handler.workflowPolicyUserID(strings.TrimSpace(c.parent.userID)), name, argsJSON)
}

func (c *loopCycleCallbacks) disallowLoopCommandBashForDocOnly(name string) bool {
	if strings.TrimSpace(name) != "bash" || c == nil || c.parent == nil || c.parent.handler == nil {
		return false
	}
	policyUserID := c.parent.handler.workflowPolicyUserID(strings.TrimSpace(c.parent.userID))
	if policyUserID == "" {
		return false
	}
	_, policy, apply := c.parent.handler.workflowToolFilterOwnerPolicyAndDecision(policyUserID, nil)
	return apply && policy == v2.ToolFilterDocOnly
}

func (c *loopCycleCallbacks) OnToken(delta string) {
	if c.parent.onToken != nil {
		c.parent.onToken(delta)
	}
}

func (c *loopCycleCallbacks) OnProgress(text string) {
	if c.parent.onProgress != nil {
		c.parent.onProgress(text)
	}
}

func (c *loopCycleCallbacks) OnToolCall(name string) {
	if c.parent.onProgress != nil {
		c.parent.onProgress(fmt.Sprintf("%s", name))
	}
}

func (c *loopCycleCallbacks) OnToolResult(name string) {}

func (c *loopCycleCallbacks) ShouldStop() bool {
	return c.parent.IsCancelled()
}

// getLoopMaxLLMIterations returns the configured max LLM iterations per
// loop cycle. Falls back to 30 if not configured.
func (h *IMMessageHandler) getLoopMaxLLMIterations() int {
	// Use the configured agent max iterations, capped at 30 for loop cycles.
	// Loop cycles should be focused — if the LLM can't fix it in 30 iterations,
	// the verification feedback in the next cycle will help more than more iterations.
	return 30
}
