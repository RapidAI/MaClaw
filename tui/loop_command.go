package main

// loop_command.go implements the /loop command for the TUI.
// Uses the same corelib/agent.RunLoopCommand engine as the GUI,
// with TUI-specific callbacks that stream progress to the chat view.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
	"github.com/RapidAI/CodeClaw/tui/views"
)

// handleLoopCommand parses and executes a /loop command in the TUI.
func (m *tuiModel) handleLoopCommand(text string) tea.Cmd {
	prog := m.program
	app := m.app
	lang := m.uiLang()

	cb := &tuiLoopCommandCallbacks{
		app:     app,
		prog:    prog,
		llmCfg:  app.llmConfig,
		workDir: "",
	}
	m.activeCb = cb // enable Esc cancellation

	return func() tea.Msg {
		cfg, err := parseLoopCommandTUI(text)
		if err != nil {
			return views.ChatResponseMsg{Error: err.Error()}
		}

		if cfg.WorkDir == "" {
			cfg.WorkDir, _ = os.Getwd()
		}
		cb.workDir = cfg.WorkDir

		llmCfg := app.llmConfig
		if strings.TrimSpace(llmCfg.URL) == "" || strings.TrimSpace(llmCfg.Model) == "" {
			return views.ChatResponseMsg{Error: tuiText(lang, "llmNotConfiguredChat")}
		}
		cb.llmCfg = llmCfg

		log.Printf("[tui-loop] starting: verify=%q goal=%q max=%d dir=%q",
			cfg.VerifyCmd, cfg.Goal, cfg.MaxIterations, cfg.WorkDir)

		state := agent.RunLoopCommand(context.Background(), cfg, cb)

		return views.ChatResponseMsg{Text: buildLoopResponseTUI(state)}
	}
}

// parseLoopCommandTUI parses a /loop command string for the TUI.
func parseLoopCommandTUI(text string) (agent.LoopCommandConfig, error) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "/loop") {
		text = strings.TrimSpace(text[5:])
	}

	if text == "" {
		return agent.LoopCommandConfig{}, fmt.Errorf(
			"Usage: /loop [--max N] [--timeout N] <verify_cmd> <goal>\n\n" +
				"Examples:\n" +
				"  /loop \"go test ./...\" make all tests pass\n" +
				"  /loop \"npm test\" --max 5 fix failing tests\n" +
				"  /loop \"make build\" fix compilation errors")
	}

	cfg := agent.LoopCommandConfig{}
	args := strings.Fields(text)
	var positional []string

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--max" && i+1 < len(args):
			i++
			n := 0
			fmt.Sscanf(args[i], "%d", &n)
			if n > 0 {
				cfg.MaxIterations = n
			}
		case args[i] == "--timeout" && i+1 < len(args):
			i++
			n := 0
			fmt.Sscanf(args[i], "%d", &n)
			if n > 0 {
				cfg.VerifyTimeout = time.Duration(n) * time.Second
			}
		case args[i] == "--dir" && i+1 < len(args):
			i++
			cfg.WorkDir = args[i]
		default:
			positional = append(positional, args[i])
		}
	}

	if len(positional) == 0 {
		return cfg, fmt.Errorf("missing verification command")
	}

	cfg.VerifyCmd = positional[0]
	if len(positional) > 1 {
		cfg.Goal = strings.Join(positional[1:], " ")
	} else {
		cfg.Goal = fmt.Sprintf("Make the following command pass (exit 0): %s", cfg.VerifyCmd)
	}

	return cfg, nil
}

// buildLoopResponseTUI formats the loop result for TUI display.
func buildLoopResponseTUI(state *agent.LoopCommandState) string {
	var sb strings.Builder

	switch state.Status {
	case agent.LoopStatusSucceeded:
		sb.WriteString(fmt.Sprintf("[OK] Loop succeeded - verification passed on iteration %d\n", len(state.Iterations)))
		sb.WriteString(fmt.Sprintf("  Goal: %s\n", state.Config.Goal))
		sb.WriteString(fmt.Sprintf("  Verify: %s\n", state.Config.VerifyCmd))
		sb.WriteString(fmt.Sprintf("  Duration: %v\n", state.EndedAt.Sub(state.StartedAt).Round(time.Second)))

	case agent.LoopStatusFailed:
		sb.WriteString(fmt.Sprintf("[FAIL] Loop failed - %d iterations exhausted\n", len(state.Iterations)))
		sb.WriteString(fmt.Sprintf("  Goal: %s\n", state.Config.Goal))
		sb.WriteString(fmt.Sprintf("  Verify: %s\n", state.Config.VerifyCmd))
		if len(state.Iterations) > 0 {
			last := state.Iterations[len(state.Iterations)-1]
			output := last.VerifyResult.CombinedOutput()
			if output != "" {
				if len(output) > 500 {
					output = output[len(output)-500:]
				}
				sb.WriteString(fmt.Sprintf("\n  Last error:\n%s\n", output))
			}
		}

	case agent.LoopStatusCancelled:
		sb.WriteString(fmt.Sprintf("[STOP] Loop cancelled at iteration %d\n", len(state.Iterations)))

	default:
		sb.WriteString("Loop finished.\n")
	}

	return sb.String()
}

// ---------------------------------------------------------------------------
// tuiLoopCommandCallbacks implements agent.LoopCommandCallbacks for the TUI.
// ---------------------------------------------------------------------------

type tuiLoopCommandCallbacks struct {
	app       *TUIApp
	prog      *tea.Program
	llmCfg    corelib.MaclawLLMConfig
	workDir   string
	cancelled bool
}

func (c *tuiLoopCommandCallbacks) Cancel() {
	c.cancelled = true
}

func (c *tuiLoopCommandCallbacks) RunModifyCycle(ctx context.Context, prompt string, iteration int) agent.LoopResult {
	cb := &tuiLoopCycleCallbacks{
		parent:  c,
		workDir: c.workDir,
	}
	return agent.RunLoop(cb, prompt, nil, nil)
}

func (c *tuiLoopCommandCallbacks) OnIterationStart(iteration, maxIterations int) {
	msg := fmt.Sprintf("Loop %d/%d", iteration+1, maxIterations)
	if c.prog != nil {
		c.prog.Send(views.ChatStreamMsg{Type: "text_delta", Content: "\n" + msg + "\n"})
	}
}

func (c *tuiLoopCommandCallbacks) OnVerifyStart(cmd string, iteration int) {
	if c.prog != nil {
		c.prog.Send(views.ChatStreamMsg{Type: "text_delta", Content: fmt.Sprintf("Running: %s\n", cmd)})
	}
}

func (c *tuiLoopCommandCallbacks) OnVerifyDone(result agent.VerifyCommandResult, iteration int) {
	var msg string
	if result.Passed() {
		msg = "Verification passed!\n"
	} else if result.TimedOut {
		msg = "Verification timed out\n"
	} else {
		msg = fmt.Sprintf("Verification failed (exit %d)\n", result.ExitCode)
	}
	if c.prog != nil {
		c.prog.Send(views.ChatStreamMsg{Type: "text_delta", Content: msg})
	}
}

func (c *tuiLoopCommandCallbacks) OnSuccess(_ *agent.LoopCommandState) {}
func (c *tuiLoopCommandCallbacks) OnFailure(_ *agent.LoopCommandState) {}
func (c *tuiLoopCommandCallbacks) IsCancelled() bool                   { return c.cancelled }

// ---------------------------------------------------------------------------
// tuiLoopCycleCallbacks implements agent.LoopCallbacks for a single cycle.
// ---------------------------------------------------------------------------

type tuiLoopCycleCallbacks struct {
	parent  *tuiLoopCommandCallbacks
	workDir string
}

func (c *tuiLoopCycleCallbacks) GetLLMConfig() corelib.MaclawLLMConfig {
	return c.parent.llmCfg
}

func (c *tuiLoopCycleCallbacks) GetMaxIterations() int {
	return config.EffectiveMaxIterations(30)
}

func (c *tuiLoopCycleCallbacks) BuildSystemPrompt(userText string, isFirstTurn bool) string {
	var sb strings.Builder
	sb.WriteString("You are a coding assistant executing a goal-driven verification loop.\n\n")
	sb.WriteString("## Your Role\n\n")
	sb.WriteString("- Read files, understand the codebase, and make targeted changes\n")
	sb.WriteString("- After you finish making changes, stop calling tools and return a brief summary\n")
	sb.WriteString("- The verification command will be run AUTOMATICALLY after you finish - do NOT run it yourself\n")
	sb.WriteString("- Focus on making the minimum changes needed to pass the verification\n\n")
	if c.workDir != "" {
		sb.WriteString(fmt.Sprintf("## Working Directory\n\n`%s`\n\n", c.workDir))
	}
	sb.WriteString("## Rules\n\n")
	sb.WriteString("- Do NOT run the verification command yourself\n")
	sb.WriteString("- Make focused, minimal changes\n")
	sb.WriteString("- When done, return a brief summary of what you changed\n")
	return sb.String()
}

func (c *tuiLoopCycleCallbacks) BuildTools(userText string) []map[string]interface{} {
	return []map[string]interface{}{
		tooldef.BuildToolDef("read_file", "Read the contents of a file.", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string", "description": "File path to read"},
			},
			"required": []string{"path"},
		}),
		tooldef.BuildToolDef("write_file", "Create or overwrite a file.", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string", "description": "File path"},
				"content": map[string]interface{}{"type": "string", "description": "File content"},
				"mode":    map[string]interface{}{"type": "string", "description": "overwrite or append"},
			},
			"required": []string{"path", "content"},
		}),
		tooldef.BuildToolDef("edit_file", "Make targeted edits using search/replace.", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":        map[string]interface{}{"type": "string", "description": "File path"},
				"old_content": map[string]interface{}{"type": "string", "description": "Text to find"},
				"new_content": map[string]interface{}{"type": "string", "description": "Replacement text"},
			},
			"required": []string{"path", "old_content", "new_content"},
		}),
		tooldef.BuildToolDef("bash", "Execute a shell command.", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{"type": "string", "description": "Shell command"},
			},
			"required": []string{"command"},
		}),
		tooldef.BuildToolDef("list_directory", "List directory contents.", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string", "description": "Directory path"},
			},
			"required": []string{"path"},
		}),
	}
}

func (c *tuiLoopCycleCallbacks) ExecuteTool(name, argsJSON string) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("Error: failed to parse tool arguments: %v", err)
	}
	return c.parent.app.toolRegistry.Execute(name, args)
}

func (c *tuiLoopCycleCallbacks) OnToken(delta string) {
	if c.parent.prog != nil {
		c.parent.prog.Send(views.ChatStreamMsg{Type: "text_delta", Content: delta})
	}
}

func (c *tuiLoopCycleCallbacks) OnProgress(text string) {}
func (c *tuiLoopCycleCallbacks) OnToolCall(name string) {
	if c.parent.prog != nil {
		c.parent.prog.Send(views.ChatStreamMsg{Type: "tool_call", Tool: name})
	}
}
func (c *tuiLoopCycleCallbacks) OnToolResult(name string) {
	if c.parent.prog != nil {
		c.parent.prog.Send(views.ChatStreamMsg{Type: "tool_result", Tool: name})
	}
}
func (c *tuiLoopCycleCallbacks) ShouldStop() bool { return c.parent.cancelled }
