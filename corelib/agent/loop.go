package agent

// loop.go defines the shared agent loop that both GUI and TUI use.
// The loop is parameterized by callback interfaces so it doesn't depend
// on any gui/ types directly.
//
// This is the mechanism that eliminates the duplicated RunAgentLoop in TUI.
// GUI and TUI each provide their own implementations of the callbacks,
// but the loop logic (LLM call → tool execution → repeat) is written once.

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// LoopCallbacks defines the capabilities the agent loop needs from its host.
// GUI provides a full implementation; TUI provides a simpler one.
type LoopCallbacks interface {
	// GetLLMConfig returns the current LLM configuration.
	GetLLMConfig() corelib.MaclawLLMConfig

	// GetMaxIterations returns the maximum number of loop iterations.
	GetMaxIterations() int

	// BuildSystemPrompt constructs the system prompt for the LLM.
	BuildSystemPrompt(userText string, isFirstTurn bool) string

	// BuildTools returns the tool definitions to send to the LLM.
	BuildTools(userText string) []map[string]interface{}

	// ExecuteTool executes a tool call and returns the result string.
	ExecuteTool(name, argsJSON string) string

	// OnToken is called with each streaming text delta (may be nil).
	OnToken(delta string)

	// OnProgress is called with progress updates (may be nil).
	OnProgress(text string)

	// OnToolCall is called before a tool is executed (for UI updates).
	OnToolCall(name string)

	// OnToolResult is called after a tool is executed (for UI updates).
	OnToolResult(name string)

	// ShouldStop returns true if the loop should be terminated early.
	ShouldStop() bool
}

// LoopHooks provides optional extension points for the agent loop.
// Hosts that don't need these features can embed DefaultLoopHooks.
type LoopHooks interface {
	// OnToolExecuted is called after a tool is executed with its result.
	// Used for session pinning, outcome recording, etc.
	OnToolExecuted(name, argsJSON, result string, success bool)

	// OnEmptyResponse is called when the LLM returns an empty response.
	// Returns true to continue the loop (retry), false to exit.
	OnEmptyResponse(iteration int) bool
}

// DefaultLoopHooks provides no-op implementations of all optional hooks.
type DefaultLoopHooks struct{}

func (DefaultLoopHooks) OnToolExecuted(string, string, string, bool) {}
func (DefaultLoopHooks) OnEmptyResponse(int) bool                    { return false }

// LoopResult is the output of RunLoop.
type LoopResult struct {
	Text       string
	Error      string
	Iterations int
	ToolCalls  int
	AskUser    *AskUserRequest
	HardExit   bool // true when loop exited due to consecutive empty responses
}

// RunLoop executes the core agent loop: LLM call → tool execution → repeat.
// This is the single implementation shared by GUI and TUI.
//
// hooks is optional — pass nil to use DefaultLoopHooks.
func RunLoop(cb LoopCallbacks, userText string, history []ConversationEntry, httpClient *http.Client, hooks ...LoopHooks) LoopResult {
	var h LoopHooks = DefaultLoopHooks{}
	if len(hooks) > 0 && hooks[0] != nil {
		h = hooks[0]
	}

	cfg := cb.GetLLMConfig()
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return LoopResult{Error: "LLM not configured"}
	}

	maxIter := cb.GetMaxIterations()
	if maxIter <= 0 {
		maxIter = 30
	}

	systemPrompt := cb.BuildSystemPrompt(userText, len(history) == 0)
	tools := cb.BuildTools(userText)

	// Build conversation from history + current message.
	var conversation []interface{}
	conversation = append(conversation, map[string]string{"role": "system", "content": systemPrompt})
	for _, entry := range history {
		conversation = append(conversation, entry.ToMessage())
	}
	conversation = append(conversation, map[string]string{"role": "user", "content": userText})

	if httpClient == nil {
		httpClient = &http.Client{
			Transport: &http.Transport{
				ResponseHeaderTimeout: time.Duration(cfg.EffectiveTimeoutSec()) * time.Second,
			},
		}
	}

	totalToolCalls := 0
	consecutiveEmpty := 0
	const maxConsecutiveEmpty = 3
	var lastNonEmptyContent string

	// Drift detection: track recent tool calls to detect loops.
	type toolCallRecord struct {
		name   string
		args   string
		result string
	}
	var recentCalls []toolCallRecord
	const driftWindow = 4 // check last N calls for repetition
	consecutiveSame := 0

	for iteration := 0; iteration < maxIter; iteration++ {
		if cb.ShouldStop() {
			return LoopResult{Error: "cancelled", Iterations: iteration, ToolCalls: totalToolCalls}
		}

		// Call LLM with tools via corelib/llm.
		ctx := context.Background()
		resp, err := doLLMRequestWithTools(ctx, cfg, conversation, tools, httpClient)
		if err != nil {
			if shouldRetrySimpleLLMError(err) {
				time.Sleep(2 * time.Second)
				resp, err = doLLMRequestWithTools(ctx, cfg, conversation, tools, httpClient)
			}
			if err != nil {
				return LoopResult{Error: fmt.Sprintf("LLM call failed: %v", err), Iterations: iteration, ToolCalls: totalToolCalls}
			}
		}

		if len(resp.Choices) == 0 {
			if h.OnEmptyResponse(iteration) {
				continue // hook says retry
			}
			return LoopResult{Error: "LLM returned no choices", Iterations: iteration, ToolCalls: totalToolCalls}
		}

		choice := resp.Choices[0]
		content := choice.Message.Content
		if content == "" && choice.Message.ReasoningContent != "" {
			content = choice.Message.ReasoningContent
		}

		// Track consecutive empty responses for hard exit.
		if strings.TrimSpace(content) == "" && len(choice.Message.ToolCalls) == 0 {
			consecutiveEmpty++
			log.Printf("[agent-loop] empty response #%d (iteration=%d)", consecutiveEmpty, iteration)
			if consecutiveEmpty >= maxConsecutiveEmpty {
				log.Printf("[agent-loop] hard exit: %d consecutive empty responses", consecutiveEmpty)
				// Return the last non-empty content as a fallback.
				return LoopResult{
					Text:       lastNonEmptyContent,
					Iterations: iteration + 1,
					ToolCalls:  totalToolCalls,
					HardExit:   true,
				}
			}
			// Inject a recover prompt to nudge the LLM.
			conversation = append(conversation, map[string]interface{}{
				"role":    "assistant",
				"content": content,
			})
			conversation = append(conversation, map[string]interface{}{
				"role":    "user",
				"content": "[系统] 你的上一条回复为空。请直接回答用户的问题，或说明你需要什么信息。",
			})
			continue
		}
		consecutiveEmpty = 0
		if strings.TrimSpace(content) != "" {
			lastNonEmptyContent = content
		}

		// Build assistant message for conversation history.
		assistantMsg := map[string]interface{}{
			"role":    "assistant",
			"content": content,
		}
		if len(choice.Message.ToolCalls) > 0 {
			assistantMsg["tool_calls"] = choice.Message.ToolCalls
		}
		conversation = append(conversation, assistantMsg)

		// No tool calls → final answer.
		if len(choice.Message.ToolCalls) == 0 {
			finalText := StripThinkingTags(content)
			// Note: we do NOT call cb.OnToken here. The final text is returned
			// via LoopResult.Text, and the caller (handleChatSend) sends it as
			// ChatResponseMsg. Calling OnToken would cause duplicate display.
			return LoopResult{Text: finalText, Iterations: iteration + 1, ToolCalls: totalToolCalls}
		}

		// Execute tool calls.
		totalToolCalls += len(choice.Message.ToolCalls)
		for _, tc := range choice.Message.ToolCalls {
			cb.OnToolCall(tc.Function.Name)
			result := cb.ExecuteTool(tc.Function.Name, tc.Function.Arguments)
			cb.OnToolResult(tc.Function.Name)

			if askReq, ok := ParseAskUserResult(result); ok {
				return LoopResult{
					Text:       FormatAskUserForDisplay(askReq),
					AskUser:    askReq,
					Iterations: iteration + 1,
					ToolCalls:  totalToolCalls,
				}
			}

			// Determine success for outcome tracking.
			toolSuccess := !strings.HasPrefix(result, "未知工具") &&
				!strings.HasPrefix(result, "工具执行异常") &&
				!strings.HasPrefix(result, "错误:") &&
				!strings.HasPrefix(result, "Error:")
			h.OnToolExecuted(tc.Function.Name, tc.Function.Arguments, result, toolSuccess)

			// Drift detection: track this call and check for repetition.
			record := toolCallRecord{name: tc.Function.Name, args: tc.Function.Arguments, result: result}
			recentCalls = append(recentCalls, record)
			if len(recentCalls) > driftWindow*2 {
				recentCalls = recentCalls[len(recentCalls)-driftWindow*2:]
			}

			// Truncate long results.
			maxLen := 4000
			if tc.Function.Name == "web_fetch" {
				maxLen = 20000
			}
			if len(result) > maxLen {
				result = result[:maxLen] + "\n...(truncated)"
			}

			conversation = append(conversation, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": tc.ID,
				"content":      result,
			})
		}

		// Drift detection: check if the last N calls are the same tool+args+result.
		// Same input + same output = dead loop. Same input + different output = polling (OK).
		if len(recentCalls) >= driftWindow {
			tail := recentCalls[len(recentCalls)-driftWindow:]
			allSame := true
			allSameResult := true
			for i := 1; i < len(tail); i++ {
				if tail[i].name != tail[0].name || tail[i].args != tail[0].args {
					allSame = false
					break
				}
				if tail[i].result != tail[0].result {
					allSameResult = false
				}
			}
			if allSame && allSameResult {
				consecutiveSame++
				if consecutiveSame >= 2 {
					driftTool := tail[0].name
					log.Printf("[agent-loop] drift detected: tool %q called %d times with same args+result, stopping", driftTool, driftWindow*consecutiveSame)
					// Inject a message telling the LLM to stop and explain.
					conversation = append(conversation, map[string]interface{}{
						"role":    "user",
						"content": fmt.Sprintf("[系统] 检测到重复调用 %s 且结果相同，请停止重试并直接告诉用户当前的限制或问题。", driftTool),
					})
				}
			} else {
				consecutiveSame = 0
			}
		}
	}

	log.Printf("[agent-loop] max iterations (%d) reached", maxIter)
	return LoopResult{Error: "max iterations reached", Iterations: maxIter, ToolCalls: totalToolCalls}
}

// doLLMRequestWithTools sends a chat completion request with tool definitions.
// Dispatches to the correct protocol based on cfg.Protocol:
//   - "anthropic" → Anthropic Messages API
//   - everything else → OpenAI-compatible chat completions
func doLLMRequestWithTools(ctx context.Context, cfg corelib.MaclawLLMConfig, conversation []interface{}, tools []map[string]interface{}, httpClient *http.Client) (*llm.Response, error) {
	if cfg.Protocol == "anthropic" {
		return llm.DoAnthropicRequest(ctx, cfg, conversation, tools, httpClient)
	}
	return llm.DoOpenAIRequest(ctx, cfg, conversation, tools, httpClient)
}
