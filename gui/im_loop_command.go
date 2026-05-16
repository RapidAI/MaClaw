package main

// im_loop_command.go implements the /loop command for the GUI (desktop panel
// and IM channels). The /loop command is a goal-driven verification loop:
//
//   /loop <verify_cmd> <goal_description>
//
// Example:
//   /loop go test ./... 让所有测试通过
//   /loop npm test fix the failing unit tests
//   /loop make build --max 5 修复编译错误
//
// The engine repeatedly: (1) runs an LLM coding cycle to modify files,
// (2) executes the verification command, (3) if exit 0 → done, else feeds
// the error output back to the LLM and repeats.
//
// Design principles:
// - Uses CodingSubAgent's clean-context model (no IM rules, no 40+ tools)
// - Verification command is the SINGLE SOURCE OF TRUTH for success
// - Drift detection is suppressed (intentional repetition)
// - Each cycle gets fresh LLM context with goal + last error output

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// parseLoopCommand parses a /loop command string into a LoopCommandConfig.
// Format: /loop [--max N] [--timeout N] [--dir path] <verify_cmd> <goal>
//
// The verify_cmd is the first non-flag argument. Everything after it is the goal.
// If the goal is empty, the verify_cmd is used as both.
//
// Returns (config, error). Error is non-nil if parsing fails.
func parseLoopCommand(text string) (agent.LoopCommandConfig, error) {
	// Strip the /loop prefix.
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "/loop") {
		text = strings.TrimSpace(text[5:])
	}

	if text == "" {
		return agent.LoopCommandConfig{}, fmt.Errorf("usage: /loop [--max N] [--timeout N] [--dir path] <verify_cmd> <goal>\n\nExamples:\n  /loop go test ./... 让所有测试通过\n  /loop npm test --max 5 fix failing tests\n  /loop make build 修复编译错误")
	}

	cfg := agent.LoopCommandConfig{}

	// Parse flags.
	args := tokenizeLoopArgs(text)
	var positional []string

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--max" && i+1 < len(args):
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n <= 0 {
				return cfg, fmt.Errorf("--max must be a positive integer, got %q", args[i])
			}
			cfg.MaxIterations = n
		case args[i] == "--timeout" && i+1 < len(args):
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n <= 0 {
				return cfg, fmt.Errorf("--timeout must be a positive integer (seconds), got %q", args[i])
			}
			cfg.VerifyTimeout = agent.LoopVerifyTimeoutFromSeconds(n)
		case args[i] == "--dir" && i+1 < len(args):
			i++
			cfg.WorkDir = args[i]
		default:
			positional = append(positional, args[i])
		}
	}

	if len(positional) == 0 {
		return cfg, fmt.Errorf("missing verification command. Usage: /loop <verify_cmd> <goal>")
	}

	// First positional is the verify command. Rest is the goal.
	cfg.VerifyCmd = positional[0]
	if len(positional) > 1 {
		cfg.Goal = strings.Join(positional[1:], " ")
	} else {
		cfg.Goal = fmt.Sprintf("Make the following command pass (exit 0): %s", cfg.VerifyCmd)
	}

	return cfg, nil
}

// tokenizeLoopArgs splits the argument string respecting quoted strings.
var loopQuotedRe = regexp.MustCompile(`"([^"]*)"`)

func tokenizeLoopArgs(s string) []string {
	// Replace quoted strings with placeholders, split, then restore.
	type placeholder struct {
		key   string
		value string
	}
	var placeholders []placeholder
	idx := 0
	replaced := loopQuotedRe.ReplaceAllStringFunc(s, func(match string) string {
		inner := match[1 : len(match)-1]
		key := fmt.Sprintf("\x00QUOTED%d\x00", idx)
		placeholders = append(placeholders, placeholder{key, inner})
		idx++
		return key
	})

	fields := strings.Fields(replaced)
	result := make([]string, 0, len(fields))
	for _, f := range fields {
		restored := f
		for _, p := range placeholders {
			restored = strings.ReplaceAll(restored, p.key, p.value)
		}
		result = append(result, restored)
	}
	return result
}

// handleLoopCommand is the entry point for /loop in the GUI message handler.
// It parses the command, creates the loop engine, and runs it synchronously
// (blocking the current agent loop slot).
func (h *IMMessageHandler) handleLoopCommand(
	msg IMUserMessage,
	text string,
	onProgress coretool.ProgressCallback,
	onToken llm.TokenCallback,
) *IMAgentResponse {
	cfg, err := parseLoopCommand(text)
	if err != nil {
		return &IMAgentResponse{Text: fmt.Sprintf("❌ %s", err.Error())}
	}

	// Resolve working directory.
	if cfg.WorkDir == "" {
		cfg.WorkDir = h.getCurrentProjectPath()
	}

	// Get LLM config.
	llmCfg := h.getMaclawLLMConfig()
	if strings.TrimSpace(llmCfg.URL) == "" || strings.TrimSpace(llmCfg.Model) == "" {
		return &IMAgentResponse{Error: "LLM is not configured. Cannot run /loop."}
	}

	log.Printf("[loop-command] parsed: verify=%q goal=%q max=%d dir=%q",
		cfg.VerifyCmd, cfg.Goal, cfg.MaxIterations, cfg.WorkDir)

	// Create the loop callbacks.
	cb := &guiLoopCommandCallbacks{
		handler:    h,
		llmCfg:     llmCfg,
		httpClient: h.client,
		projectDir: cfg.WorkDir,
		onProgress: onProgress,
		onToken:    onToken,
		userID:     msg.UserID,
	}

	// Wire cancellation: store the callbacks so /cancel can reach them.
	h.activeLoopCallbacks.Store(cb)
	defer h.activeLoopCallbacks.Store((*guiLoopCommandCallbacks)(nil))

	// Run the loop command (blocking).
	ctx := context.Background()
	state := agent.RunLoopCommand(ctx, cfg, cb)

	// Build the response.
	return buildLoopCommandResponse(state)
}

// buildLoopCommandResponse formats the final loop state into a user-facing response.
func buildLoopCommandResponse(state *agent.LoopCommandState) *IMAgentResponse {
	var sb strings.Builder

	switch state.Status {
	case agent.LoopStatusSucceeded:
		sb.WriteString(fmt.Sprintf("✅ **Loop 成功** — 验证命令在第 %d 轮通过\n\n", len(state.Iterations)))
		sb.WriteString(fmt.Sprintf("- 目标: %s\n", state.Config.Goal))
		sb.WriteString(fmt.Sprintf("- 验证命令: `%s`\n", state.Config.VerifyCmd))
		sb.WriteString(fmt.Sprintf("- 总耗时: %v\n", state.EndedAt.Sub(state.StartedAt).Round(100_000_000)))

	case agent.LoopStatusFailed:
		sb.WriteString(fmt.Sprintf("❌ **Loop 失败** — %d 轮迭代后验证命令仍未通过\n\n", len(state.Iterations)))
		sb.WriteString(fmt.Sprintf("- 目标: %s\n", state.Config.Goal))
		sb.WriteString(fmt.Sprintf("- 验证命令: `%s`\n", state.Config.VerifyCmd))
		sb.WriteString(fmt.Sprintf("- 总耗时: %v\n", state.EndedAt.Sub(state.StartedAt).Round(100_000_000)))
		// Show last error.
		if len(state.Iterations) > 0 {
			last := state.Iterations[len(state.Iterations)-1]
			output := last.VerifyResult.CombinedOutput()
			if output != "" {
				sb.WriteString("\n**最后一次验证输出:**\n```\n")
				if len(output) > 1000 {
					output = output[len(output)-1000:]
				}
				sb.WriteString(output)
				sb.WriteString("\n```\n")
			}
		}

	case agent.LoopStatusCancelled:
		sb.WriteString(fmt.Sprintf("⏹️ **Loop 已取消** — 在第 %d 轮被中断\n", len(state.Iterations)))

	default:
		sb.WriteString("Loop 执行完毕。\n")
	}

	return &IMAgentResponse{Text: sb.String()}
}


