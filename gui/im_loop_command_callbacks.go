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
	"sync/atomic"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
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
	cancelled  atomic.Bool
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
	return result
}

func (c *guiLoopCommandCallbacks) OnIterationStart(iteration, maxIterations int) {
	msg := fmt.Sprintf("🔄 Loop 迭代 %d/%d", iteration+1, maxIterations)
	log.Printf("[loop-command] %s", msg)
	if c.onProgress != nil {
		c.onProgress(msg)
	}
}

func (c *guiLoopCommandCallbacks) OnVerifyStart(cmd string, iteration int) {
	msg := fmt.Sprintf("🧪 运行验证命令: %s", cmd)
	if c.onProgress != nil {
		c.onProgress(msg)
	}
}

func (c *guiLoopCommandCallbacks) OnVerifyDone(result agent.VerifyCommandResult, iteration int) {
	if result.Passed() {
		if c.onProgress != nil {
			c.onProgress("✅ 验证通过!")
		}
	} else {
		msg := fmt.Sprintf("❌ 验证失败 (exit %d)", result.ExitCode)
		if result.TimedOut {
			msg = "⏱️ 验证命令超时"
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
	return c.cancelled.Load()
}

// Cancel marks the loop as cancelled. Called from the main handler when
// the user sends /cancel or clicks the stop button.
func (c *guiLoopCommandCallbacks) Cancel() {
	c.cancelled.Store(true)
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
	sb.WriteString("- `write_file`: Create or overwrite a file\n")
	sb.WriteString("- `edit_file`: Make targeted edits to an existing file\n")
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
		tooldef.BuildToolDef("write_file", "Create or overwrite a file with the given content.", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string", "description": "File path to write"},
				"content": map[string]interface{}{"type": "string", "description": "File content"},
				"mode":    map[string]interface{}{"type": "string", "description": "Write mode: overwrite (default) or append"},
			},
			"required": []string{"path", "content"},
		}),
		tooldef.BuildToolDef("edit_file", "Make targeted edits to an existing file using search/replace.", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":        map[string]interface{}{"type": "string", "description": "File path to edit"},
				"old_content": map[string]interface{}{"type": "string", "description": "Exact text to find"},
				"new_content": map[string]interface{}{"type": "string", "description": "Replacement text"},
			},
			"required": []string{"path", "old_content", "new_content"},
		}),
		tooldef.BuildToolDef("bash", "Execute a shell command.", map[string]interface{}{
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
	policyUserID := strings.TrimSpace(c.parent.userID)
	return c.parent.handler.executeToolDetailedWithPolicyUserText(policyUserID, name, argsJSON, "", nil).Text
}

func (c *loopCycleCallbacks) IsToolAllowed(name string) bool {
	if c == nil || c.parent == nil || c.parent.handler == nil {
		return true
	}
	return c.parent.handler.isWorkflowToolAllowedForOwner(strings.TrimSpace(c.parent.userID), name)
}

func (c *loopCycleCallbacks) IsToolCallAllowed(name, argsJSON string) (bool, string) {
	if c == nil || c.parent == nil || c.parent.handler == nil {
		return true, ""
	}
	return c.parent.handler.isWorkflowToolCallAllowedForOwner(strings.TrimSpace(c.parent.userID), name, argsJSON)
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
		c.parent.onProgress(fmt.Sprintf("🔧 %s", name))
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
