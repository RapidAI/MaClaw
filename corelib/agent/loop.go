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
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

// LoopCallbacks defines the capabilities the agent loop needs from its host.
// GUI provides a full implementation; TUI provides a simpler one.
type LoopCallbacks interface {
	// GetLLMConfig returns the current LLM configuration.
	GetLLMConfig() corelib.MaclawLLMConfig

	// GetMaxIterations returns the maximum number of loop iterations.
	// Implementations MUST use config.EffectiveMaxIterations(configuredValue)
	// to ensure consistent behavior across all hosts. Example:
	//
	//   func (c *myCallbacks) GetMaxIterations() int {
	//       return config.EffectiveMaxIterations(c.cfg.MaclawAgentMaxIterations)
	//   }
	//
	// This ensures the same default (300), minimum (30), and maximum (300)
	// are applied everywhere.
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

type ToolExecutionOutcome string

const (
	ToolExecutionOutcomeOK      ToolExecutionOutcome = "ok"
	ToolExecutionOutcomeTimeout ToolExecutionOutcome = "timeout"
	ToolExecutionOutcomeError   ToolExecutionOutcome = "error"
)

type ToolExecutionResult struct {
	Result  string
	Outcome ToolExecutionOutcome
}

type StructuredToolExecutor interface {
	ExecuteToolStructured(name, argsJSON string) ToolExecutionResult
}

// LLMRequestContextProvider lets hosts wrap each LLM round with their own
// scheduling, tracing, and cancellation boundary without making corelib/agent
// depend on GUI/runtime packages.
type LLMRequestContextProvider interface {
	LLMRequestContext(iteration int) (context.Context, func(error), error)
}

// ToolAuthorizer is an optional host callback implemented when tool execution
// must be constrained by an outer policy, such as a workflow phase.
type ToolAuthorizer interface {
	IsToolAllowed(name string) bool
}

// ToolCallAuthorizer is an optional stronger execution boundary for hosts that
// need to validate tool arguments, not just the tool name.
type ToolCallAuthorizer interface {
	IsToolCallAllowed(name, argsJSON string) (bool, string)
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
	HardExit   bool // true when loop exited abnormally (consecutive empty responses, same-tool hard stop, etc.)
}

// RunLoop executes the core agent loop: LLM call → tool execution → repeat.
// This is the single implementation shared by GUI and TUI.
//
// hooks is optional — pass nil to use DefaultLoopHooks.
func RunLoop(cb LoopCallbacks, userText string, history []ConversationEntry, httpClient *http.Client, hooks ...LoopHooks) LoopResult {
	return RunLoopWithUserContent(cb, userText, userText, history, httpClient, hooks...)
}

// RunLoopWithUserContent executes the core agent loop with a prebuilt user
// content payload. userText remains the plain text used for prompt/context
// construction, while userContent may be a multimodal content array.
func RunLoopWithUserContent(cb LoopCallbacks, userText string, userContent interface{}, history []ConversationEntry, httpClient *http.Client, hooks ...LoopHooks) LoopResult {
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
		// This should never happen if GetMaxIterations() is implemented correctly
		// (using config.EffectiveMaxIterations). Log a warning to surface bugs.
		log.Printf("[agent-loop] WARNING: GetMaxIterations() returned %d, using fallback. This indicates a bug in the LoopCallbacks implementation.", maxIter)
		maxIter = config.EffectiveMaxIterations(0)
	}

	systemPrompt := cb.BuildSystemPrompt(userText, len(history) == 0)
	tools := FilterToolDefinitionsByAuthorizer(cb, cb.BuildTools(userText))

	// Build conversation from history + current message.
	var conversation []interface{}
	conversation = append(conversation, map[string]string{"role": "system", "content": systemPrompt})
	for _, entry := range history {
		conversation = append(conversation, entry.ToMessage())
	}
	conversation = append(conversation, map[string]interface{}{"role": "user", "content": userContent})

	if httpClient == nil {
		httpClient = &http.Client{
			Transport: &http.Transport{
				ResponseHeaderTimeout: time.Duration(cfg.EffectiveTimeoutSec()) * time.Second,
			},
		}
	}

	totalToolCalls := 0
	consecutiveEmpty := 0
	const maxConsecutiveEmpty = 5
	var lastNonEmptyContent string
	var lastToolName string         // track last tool name for empty-response recovery
	var lastToolOutcome toolOutcome // structured outcome of last tool execution

	// Drift detection: track recent tool calls to detect loops.
	type toolCallRecord struct {
		name   string
		args   string
		result string
	}
	var recentCalls []toolCallRecord
	const driftWindow = 4 // check last N calls for repetition
	consecutiveSame := 0

	// Consecutive same-tool failure detection: catches non-repeating failure
	// loops where the agent keeps trying different approaches with the same
	// tool but all fail (e.g. repeatedly editing a bat script then running
	// bash — each attempt has unique args so the exact-match drift detector
	// above won't trigger).
	//
	// Escalation: inject guidance at maxConsecutiveSameToolFailures (8), then
	// force-stop the loop at hardStopSameToolFailures (12) if the LLM ignores
	// the guidance and keeps failing.
	var lastFailedTool string
	consecutiveSameToolFailures := 0
	sameToolFailureGuidanceInjected := false
	const maxConsecutiveSameToolFailures = 8
	const hardStopSameToolFailures = 12

	for iteration := 0; iteration < maxIter; iteration++ {
		if cb.ShouldStop() {
			return LoopResult{Error: "cancelled", Iterations: iteration, ToolCalls: totalToolCalls}
		}

		// Call LLM with tools via corelib/llm (streaming for real-time display).
		ctx, finishLLMRequest, ctxErr := llmRequestContextForLoop(cb, iteration)
		if ctxErr != nil {
			return LoopResult{Error: fmt.Sprintf("LLM request context failed: %v", ctxErr), Iterations: iteration, ToolCalls: totalToolCalls}
		}
		resp, err := doLLMRequestWithToolsStream(ctx, cfg, conversation, tools, httpClient, cb.OnToken)
		if err != nil {
			// Retry with exponential backoff for transient errors (503, timeout, network).
			// SubAgent tasks should be resilient to brief API outages.
			const maxLLMRetries = 3
			for retryAttempt := 1; retryAttempt <= maxLLMRetries && shouldRetrySimpleLLMError(err); retryAttempt++ {
				backoff := time.Duration(retryAttempt*2) * time.Second
				log.Printf("[agent-loop] LLM error (attempt %d/%d), retrying in %s: %v", retryAttempt, maxLLMRetries, backoff, err)
				time.Sleep(backoff)
				if cb.ShouldStop() {
					finishLLMRequest(err)
					return LoopResult{Error: "cancelled during LLM retry", Iterations: iteration, ToolCalls: totalToolCalls}
				}
				resp, err = doLLMRequestWithTools(ctx, cfg, conversation, tools, httpClient)
			}
			finishLLMRequest(err)
			if err != nil {
				return LoopResult{Error: fmt.Sprintf("LLM call failed: %v", err), Iterations: iteration, ToolCalls: totalToolCalls}
			}
		} else {
			finishLLMRequest(nil)
		}

		if len(resp.Choices) == 0 {
			if h.OnEmptyResponse(iteration) {
				continue // hook says retry
			}
			return LoopResult{Error: "LLM returned no choices", Iterations: iteration, ToolCalls: totalToolCalls}
		}

		choice := resp.Choices[0]
		reasoningContent := StripRolePrefixHallucination(choice.Message.ReasoningContent)
		content := choice.Message.Content
		if content == "" && reasoningContent != "" {
			content = reasoningContent
		}
		content = StripRolePrefixHallucination(content)

		if len(choice.TruncatedToolNames) > 0 {
			truncatedList := strings.Join(choice.TruncatedToolNames, ", ")
			log.Printf("[agent-loop] truncated tool call recovery (iteration=%d tools=%s valid_tools=%d)", iteration, truncatedList, len(choice.Message.ToolCalls))

			// Best-effort partial write: if write_file was truncated and we have
			// the raw (incomplete) args, extract path+content and write to disk.
			// This converts a failed call into a partially successful one.
			var recoveryPrompt string
			if rawArgs := truncatedToolArgsLookup(choice.TruncatedToolArgs, "write_file"); rawArgs != "" {
				if pw := attemptLoopPartialWriteFile(rawArgs); pw != nil {
					recoveryPrompt = buildLoopPartialWriteRecovery(pw)
					// Made progress — reset drift/failure counters.
					consecutiveSame = 0
					consecutiveSameToolFailures = 0
					lastFailedTool = ""
					sameToolFailureGuidanceInjected = false
				} else {
					// Partial write not possible. If file already exists from a
					// previous partial write, instruct LLM to use mode=append.
					if absPath := resolvePartialWritePath(rawArgs); absPath != "" {
						if info, statErr := os.Stat(absPath); statErr == nil && info.Size() > 0 {
							recoveryPrompt = buildLoopPartialWriteAppendHint(absPath, info.Size())
						}
					}
				}
			}
			if recoveryPrompt == "" {
				recoveryPrompt = buildToolCallTruncationRecovery(choice.TruncatedToolNames, tools, userText)
			}

			if strings.TrimSpace(content) != "" || reasoningContent != "" {
				assistantMsg := map[string]interface{}{
					"role":    "assistant",
					"content": content,
				}
				if reasoningContent != "" {
					assistantMsg["reasoning_content"] = reasoningContent
				} else {
					assistantMsg["reasoning_content"] = ""
				}
				conversation = append(conversation, assistantMsg)
			}
			conversation = append(conversation, map[string]interface{}{
				"role":    "user",
				"content": recoveryPrompt,
			})
			continue
		}

		if invalidToolNames := invalidLoopToolArgumentNames(choice.Message.ToolCalls); len(invalidToolNames) > 0 {
			invalidList := strings.Join(invalidToolNames, ", ")
			log.Printf("[agent-loop] invalid tool call argument recovery (iteration=%d tools=%s)", iteration, invalidList)
			recoveryPrompt := buildToolCallTruncationRecovery(invalidToolNames, tools, userText)
			if strings.TrimSpace(content) != "" || reasoningContent != "" {
				assistantMsg := map[string]interface{}{
					"role":    "assistant",
					"content": content,
				}
				if reasoningContent != "" {
					assistantMsg["reasoning_content"] = reasoningContent
				} else {
					assistantMsg["reasoning_content"] = ""
				}
				conversation = append(conversation, assistantMsg)
			}
			conversation = append(conversation, map[string]interface{}{
				"role":    "user",
				"content": recoveryPrompt,
			})
			continue
		}

		// Track consecutive empty responses for hard exit.
		if strings.TrimSpace(content) == "" && len(choice.Message.ToolCalls) == 0 {
			consecutiveEmpty++
			snippetLen := len([]rune(lastToolOutcome.snippet))
			log.Printf("[agent-loop] empty response #%d (iteration=%d, lastTool=%s, outcome=%d, snippet_len=%d)",
				consecutiveEmpty, iteration, lastToolName, lastToolOutcome.kind, snippetLen)
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

			// Brief pause before retry to avoid rapid-fire empty requests.
			time.Sleep(time.Duration(consecutiveEmpty) * time.Second)
			if cb.ShouldStop() {
				return LoopResult{Error: "cancelled", Iterations: iteration, ToolCalls: totalToolCalls}
			}

			// Build a context-aware recovery prompt.
			recoverPrompt := buildEmptyResponseRecovery(consecutiveEmpty, lastToolName, lastToolOutcome, userText)

			// Inject a recover prompt to nudge the LLM.
			conversation = append(conversation, map[string]interface{}{
				"role":              "assistant",
				"content":           content,
				"reasoning_content": "", // DeepSeek V4+: must exist on all assistant messages
			})
			conversation = append(conversation, map[string]interface{}{
				"role":    "user",
				"content": recoverPrompt,
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
		if reasoningContent != "" {
			assistantMsg["reasoning_content"] = reasoningContent
		} else {
			// DeepSeek V4+ thinking mode: when tools are present in the
			// request, reasoning_content must exist on ALL assistant messages.
			// An empty string is accepted. For non-DeepSeek providers, the
			// field is simply ignored.
			assistantMsg["reasoning_content"] = ""
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
		for _, tc := range choice.Message.ToolCalls {
			if cb.ShouldStop() {
				return LoopResult{Error: "cancelled", Iterations: iteration + 1, ToolCalls: totalToolCalls}
			}
			totalToolCalls++
			argsJSON := normalizeLoopToolArguments(tc.Function.Arguments)
			execResult, syntheticFailure := validateLoopToolArguments(tc.Function.Name, argsJSON)
			if !syntheticFailure {
				var policyRejected bool
				execResult, policyRejected = authorizeLoopTool(cb, tc.Function.Name, argsJSON)
				if !policyRejected {
					cb.OnToolCall(tc.Function.Name)
					execResult = executeAuthorizedLoopTool(cb, tc.Function.Name, argsJSON)
					cb.OnToolResult(tc.Function.Name)
				}
			}
			result := execResult.Result

			// Track last tool for empty-response recovery context.
			lastToolName = tc.Function.Name
			lastToolOutcome = toolOutcomeFromExecutionResult(execResult)

			if askReq, ok := ParseAskUserResult(result); ok {
				return LoopResult{
					Text:       FormatAskUserForDisplay(askReq),
					AskUser:    askReq,
					Iterations: iteration + 1,
					ToolCalls:  totalToolCalls,
				}
			}

			// Determine success for outcome tracking.
			toolSuccess := lastToolOutcome.kind == toolOutcomeOK
			h.OnToolExecuted(tc.Function.Name, argsJSON, result, toolSuccess)

			// Consecutive same-tool failure tracking.
			if !toolSuccess {
				if tc.Function.Name == lastFailedTool || lastFailedTool == "" {
					lastFailedTool = tc.Function.Name
					consecutiveSameToolFailures++
				}
				// Different tool failing: don't reset — it's likely noise
				// (e.g. an intermittent write_file permission error between
				// bash failures). Only the dominant failing tool matters.

				if consecutiveSameToolFailures >= hardStopSameToolFailures {
					// Hard stop: LLM ignored the guidance and keeps failing.
					log.Printf("[agent-loop] hard stop: tool=%q failed %d consecutive times, force-exiting loop", lastFailedTool, consecutiveSameToolFailures)
					return LoopResult{
						Text:       fmt.Sprintf("工具 %s 连续失败 %d 次，已停止执行。请检查环境或换一种方式完成任务。", lastFailedTool, consecutiveSameToolFailures),
						Iterations: iteration + 1,
						ToolCalls:  totalToolCalls,
						HardExit:   true,
					}
				}
			} else {
				// Success of the previously-failing tool resets the counter.
				if tc.Function.Name == lastFailedTool {
					consecutiveSameToolFailures = 0
					lastFailedTool = ""
					sameToolFailureGuidanceInjected = false
				}
			}

			// Drift detection: track this call and check for repetition.
			record := toolCallRecord{name: tc.Function.Name, args: argsJSON, result: result}
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

		// Consecutive same-tool failure guidance: inject AFTER all tool results
		// are appended (injecting between tool results creates invalid message
		// ordering that some LLM APIs reject).
		// Use >= threshold with a one-shot flag to handle cases where the
		// counter jumps past the exact threshold within a single multi-tool-call
		// iteration.
		if consecutiveSameToolFailures >= maxConsecutiveSameToolFailures && !sameToolFailureGuidanceInjected {
			sameToolFailureGuidanceInjected = true
			log.Printf("[agent-loop] consecutive same-tool failures: tool=%q count=%d, injecting stop guidance", lastFailedTool, consecutiveSameToolFailures)
			conversation = append(conversation, map[string]interface{}{
				"role":    "user",
				"content": fmt.Sprintf("[系统] 工具 %s 已连续失败 %d 次（每次方法不同但均未成功）。请停止继续尝试该工具，改用其他方式完成任务，或向用户说明当前遇到的具体问题和限制。", lastFailedTool, consecutiveSameToolFailures),
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

func executeLoopTool(cb LoopCallbacks, name, argsJSON string) ToolExecutionResult {
	argsJSON = normalizeLoopToolArguments(argsJSON)
	if result, rejected := authorizeLoopTool(cb, name, argsJSON); rejected {
		return result
	}
	return executeAuthorizedLoopTool(cb, name, argsJSON)
}

func normalizeLoopToolArguments(argsJSON string) string {
	if strings.TrimSpace(argsJSON) == "" {
		return "{}"
	}
	return argsJSON
}

func validateLoopToolArguments(name, argsJSON string) (ToolExecutionResult, bool) {
	args := strings.TrimSpace(argsJSON)
	if args == "" {
		return ToolExecutionResult{}, false
	}
	if loopToolArgumentsAreObject(args) {
		return ToolExecutionResult{}, false
	}
	msg := fmt.Sprintf("Error: tool call %q has invalid JSON object arguments. The tool was not executed. Regenerate the same tool call with complete valid JSON object arguments, including all required fields, and do not summarize or truncate file content.", name)
	log.Printf("[agent-loop] rejected invalid tool arguments tool=%q args_len=%d", strings.TrimSpace(name), len(args))
	return ToolExecutionResult{Result: msg, Outcome: ToolExecutionOutcomeError}, true
}

func loopToolArgumentsAreObject(args string) bool {
	var parsed map[string]interface{}
	return json.Unmarshal([]byte(args), &parsed) == nil && parsed != nil
}

func invalidLoopToolArgumentNames(calls []llm.ToolCall) []string {
	if len(calls) == 0 {
		return nil
	}
	var invalid []string
	for _, tc := range calls {
		args := normalizeLoopToolArguments(tc.Function.Arguments)
		if !loopToolArgumentsAreObject(args) {
			invalid = append(invalid, tc.Function.Name)
		}
	}
	return invalid
}

func authorizeLoopTool(cb LoopCallbacks, name, argsJSON string) (ToolExecutionResult, bool) {
	if authorizer, ok := cb.(ToolAuthorizer); ok && !authorizer.IsToolAllowed(name) {
		return ToolExecutionResult{
			Result:  fmt.Sprintf("Error: tool %q is not allowed by the current execution policy", name),
			Outcome: ToolExecutionOutcomeError,
		}, true
	}
	if authorizer, ok := cb.(ToolCallAuthorizer); ok {
		if allowed, reason := authorizer.IsToolCallAllowed(name, argsJSON); !allowed {
			if strings.TrimSpace(reason) == "" {
				reason = fmt.Sprintf("tool call %q is not allowed by the current execution policy", name)
			}
			return ToolExecutionResult{
				Result:  "Error: " + reason,
				Outcome: ToolExecutionOutcomeError,
			}, true
		}
	}
	return ToolExecutionResult{}, false
}

func executeAuthorizedLoopTool(cb LoopCallbacks, name, argsJSON string) ToolExecutionResult {
	if structured, ok := cb.(StructuredToolExecutor); ok {
		result := structured.ExecuteToolStructured(name, argsJSON)
		if result.Outcome == "" {
			outcome := classifyToolResult(result.Result)
			result.Outcome = executionOutcomeFromToolOutcome(outcome.kind)
		}
		return result
	}
	result := cb.ExecuteTool(name, argsJSON)
	outcome := classifyToolResult(result)
	return ToolExecutionResult{Result: result, Outcome: executionOutcomeFromToolOutcome(outcome.kind)}
}

// FilterToolDefinitionsByAuthorizer removes LLM-facing tool definitions that
// the callback's ToolAuthorizer would reject at execution time. This keeps
// exposure and execution governed by the same mechanism.
func FilterToolDefinitionsByAuthorizer(cb LoopCallbacks, tools []map[string]interface{}) []map[string]interface{} {
	authorizer, ok := cb.(ToolAuthorizer)
	if !ok || len(tools) == 0 {
		return tools
	}
	filtered := make([]map[string]interface{}, 0, len(tools))
	for _, def := range tools {
		if authorizer.IsToolAllowed(tooldef.Name(def)) {
			filtered = append(filtered, def)
		}
	}
	return filtered
}

func toolOutcomeFromExecutionResult(result ToolExecutionResult) toolOutcome {
	outcome := toolOutcome{kind: toolOutcomeOK, snippet: truncateRunesSuffix(result.Result, 300)}
	switch result.Outcome {
	case ToolExecutionOutcomeTimeout:
		outcome.kind = toolOutcomeTimeout
	case ToolExecutionOutcomeError:
		outcome.kind = toolOutcomeError
	default:
		outcome.kind = toolOutcomeOK
	}
	return outcome
}

func executionOutcomeFromToolOutcome(kind toolOutcomeKind) ToolExecutionOutcome {
	switch kind {
	case toolOutcomeTimeout:
		return ToolExecutionOutcomeTimeout
	case toolOutcomeError:
		return ToolExecutionOutcomeError
	default:
		return ToolExecutionOutcomeOK
	}
}

// doLLMRequestWithTools sends a chat completion request with tool definitions.
// Dispatches to the correct protocol based on cfg.Protocol:
//   - "anthropic" → Anthropic Messages API
//   - everything else → OpenAI-compatible chat completions
func doLLMRequestWithTools(ctx context.Context, cfg corelib.MaclawLLMConfig, conversation []interface{}, tools []map[string]interface{}, httpClient *http.Client) (*llm.Response, error) {
	if cfg.IsResponsesAPI() || cfg.IsResponsesWebSocket() {
		return doResponsesRequestWithTools(ctx, cfg, conversation, tools, httpClient)
	}
	if cfg.Protocol == "anthropic" {
		return llm.DoAnthropicRequest(ctx, cfg, conversation, tools, httpClient)
	}
	return llm.DoOpenAIRequest(ctx, cfg, conversation, tools, httpClient)
}

func doResponsesRequestWithTools(ctx context.Context, cfg corelib.MaclawLLMConfig, conversation []interface{}, tools []map[string]interface{}, httpClient *http.Client) (*llm.Response, error) {
	req, _, endpoint, err := llm.NewResponsesAPIRequest(ctx, cfg, conversation, llm.ResponsesAPIRequestOptions{
		Stream: false,
		Tools:  tools,
	})
	if err != nil {
		return nil, err
	}
	log.Printf("[agent-loop] POST %s model=%s configured_model=%s protocol=%s wire_api=%s (stream=false)", endpoint, cfg.UpstreamModel(), cfg.Model, cfg.Protocol, cfg.WireAPI)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if readErr != nil && len(body) == 0 {
		return nil, readErr
	}
	if resp.StatusCode != http.StatusOK {
		return nil, newLLMHTTPError(resp.StatusCode, fmt.Sprintf("llm responses request failed body_len=%d", len(body)))
	}
	parsed, err := llm.ParseNonStreamResponsesAPIBody(body)
	if err != nil {
		return nil, err
	}
	return parsed, nil
}

// buildEmptyResponseRecovery constructs a context-aware recovery prompt when
// the LLM returns an empty response (no content, no tool calls). The prompt
// includes information about the last tool execution to help the LLM resume
// its task, especially after tool timeouts or errors.
func buildEmptyResponseRecovery(emptyCount int, lastToolName string, outcome toolOutcome, userGoal string) string {
	var sb strings.Builder

	// Escalating urgency based on consecutive empty count.
	if emptyCount <= 2 {
		sb.WriteString("[系统] 你的上一条回复为空。")
	} else {
		sb.WriteString(fmt.Sprintf("[系统] 警告：你已经连续 %d 次返回空回复。你必须立即回复内容或调用工具，否则任务将被终止。", emptyCount))
	}

	// Include last tool context if available.
	// The outcome kind is determined structurally by classifyToolResult,
	// not by keyword matching on arbitrary output.
	if lastToolName != "" {
		switch outcome.kind {
		case toolOutcomeTimeout:
			sb.WriteString(fmt.Sprintf("\n上一个工具 %s 执行超时。请不要放弃——你应该：", lastToolName))
			sb.WriteString("\n1. 检查操作是否仍在后台运行（如适用）")
			sb.WriteString("\n2. 尝试用更短的超时或不同的方式重试")
			sb.WriteString("\n3. 如果无法继续，向用户说明当前进度和遇到的问题")
		case toolOutcomeError:
			sb.WriteString(fmt.Sprintf("\n上一个工具 %s 返回了错误。请分析错误原因并尝试其他方法继续完成任务。", lastToolName))
		default:
			sb.WriteString(fmt.Sprintf("\n上一个工具调用是 %s。请根据其结果继续执行任务。", lastToolName))
		}
	}

	// Remind the LLM of the original goal on later retries.
	if emptyCount >= 2 && userGoal != "" {
		goalSnippet := truncateRunesPrefix(userGoal, 200)
		sb.WriteString(fmt.Sprintf("\n\n用户的原始目标：%s", goalSnippet))
		sb.WriteString("\n请继续完成这个任务，或者告诉用户当前的进展和遇到的问题。")
	}

	return sb.String()
}

func buildToolCallTruncationRecovery(names []string, tools []map[string]interface{}, userGoal string) string {
	available := map[string]bool{}
	for _, item := range tools {
		if name := tooldef.Name(item); name != "" {
			available[name] = true
		}
	}
	var sb strings.Builder
	sb.WriteString("[system] Previous tool call arguments were incomplete or invalid JSON, likely because output was truncated. Do not repeat the same oversized call. Regenerate complete valid JSON.")
	if len(names) > 0 {
		sb.WriteString("\nTruncated tools: ")
		sb.WriteString(strings.Join(names, ", "))
	}
	if containsString(names, "write_file") && available["write_file"] {
		sb.WriteString("\nFor write_file: no per-call content length limit. For very large content (>6000 chars), split into overwrite + append chunks to avoid model output truncation. Prefer edit_file or edit_lines for existing files.")
	}
	if containsString(names, "bash") && available["bash"] {
		sb.WriteString("\nFor bash: keep command <= 4000 characters. Do not embed generated file bodies in shell heredocs; use write_file chunks or targeted edits.")
	}
	if (available["edit_file"] || available["edit_lines"]) && containsString(names, "write_file") {
		sb.WriteString("\nFor existing files, use targeted edits instead of rewriting whole files.")
	}
	if strings.TrimSpace(userGoal) != "" {
		sb.WriteString("\nOriginal user goal: ")
		sb.WriteString(truncateRunesPrefix(userGoal, 240))
	}
	return sb.String()
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func llmRequestContextForLoop(cb LoopCallbacks, iteration int) (context.Context, func(error), error) {
	if provider, ok := cb.(LLMRequestContextProvider); ok {
		ctx, finish, err := provider.LLMRequestContext(iteration)
		if err != nil {
			return nil, nil, err
		}
		if ctx == nil {
			ctx = context.Background()
		}
		if finish == nil {
			finish = func(error) {}
		}
		return ctx, finish, nil
	}
	return context.Background(), func(error) {}, nil
}

// ---------------------------------------------------------------------------
// toolOutcome — structured classification of tool execution results.
//
// All classification logic lives in classifyToolResult, which inspects the
// well-known prefixes produced by our own tool implementations (e.g.
// "[错误] 命令超时", "工具执行异常"). This is NOT keyword matching on
// arbitrary LLM output — these are structured markers we control.
// ---------------------------------------------------------------------------

type toolOutcomeKind int

const (
	toolOutcomeOK      toolOutcomeKind = iota // tool executed successfully
	toolOutcomeTimeout                        // tool hit a deadline / timeout
	toolOutcomeError                          // tool returned a known error
)

type toolOutcome struct {
	kind    toolOutcomeKind
	snippet string // last ~300 runes of the result for logging
}

// classifyToolResult inspects the tool result string and returns a structured
// outcome. The markers checked here are produced by our own tool code:
//
//   - "[错误] 命令超时"  → tools_local.go, im_tools_local.go
//   - "[错误] 退出码"    → tools_local.go, im_tools_local.go
//   - "[错误] 命令启动失败" → tools_local.go, im_tools_local.go
//   - "[错误] ..."       → im_tool_async_wait.go
//   - "工具执行异常"     → im_tool_execution.go (panic recovery)
//   - "未知工具"         → im_tool_execution.go, subagent callbacks
//   - "参数解析失败"     → im_tool_execution.go, tui callbacks
//   - "错误:"           → various tool handlers
//   - "Error:"          → various tool handlers
func classifyToolResult(result string) toolOutcome {
	snippet := truncateRunesSuffix(result, 300)

	// Timeout: our bash/ssh tools append "[错误] 命令超时（N 秒）".
	if strings.Contains(result, "[错误] 命令超时") {
		return toolOutcome{kind: toolOutcomeTimeout, snippet: snippet}
	}

	// Structured error markers from our tool implementations.
	errorPrefixes := []string{
		"[错误]",   // tools_local, im_tools_local, im_tool_async_wait
		"工具执行异常", // im_tool_execution.go panic recovery
		"未知工具",   // im_tool_execution.go, subagent callbacks
		"参数解析失败", // im_tool_execution.go, tui callbacks
		"错误:",    // various tool handlers
		"Error:", // various tool handlers
	}
	trimmed := strings.TrimSpace(result)
	for _, prefix := range errorPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return toolOutcome{kind: toolOutcomeError, snippet: snippet}
		}
	}
	// Also check for [错误] appearing mid-result (e.g. bash output + error).
	if strings.Contains(result, "\n[错误]") {
		return toolOutcome{kind: toolOutcomeError, snippet: snippet}
	}

	return toolOutcome{kind: toolOutcomeOK, snippet: snippet}
}

// truncateRunesSuffix returns the last n runes of s (UTF-8 safe).
// If s has fewer than n runes, returns s unchanged.
func truncateRunesSuffix(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}

// truncateRunesPrefix returns the first n runes of s with "..." appended (UTF-8 safe).
// If s has fewer than n runes, returns s unchanged.
func truncateRunesPrefix(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

// doLLMRequestWithToolsStream sends a streaming LLM request, calling onToken
// for each text delta. Falls back to non-streaming if the streaming request fails.
func doLLMRequestWithToolsStream(ctx context.Context, cfg corelib.MaclawLLMConfig, conversation []interface{}, tools []map[string]interface{}, httpClient *http.Client, onToken llm.TokenCallback) (*llm.Response, error) {
	var rolePrefixFilter *rolePrefixStreamFilter
	if onToken != nil {
		rolePrefixFilter = newRolePrefixStreamFilter(onToken)
		onToken = rolePrefixFilter.Write
	}
	flushRolePrefixFilter := func(err error) {
		if err == nil && rolePrefixFilter != nil {
			rolePrefixFilter.Flush()
		}
	}

	if cfg.IsResponsesAPI() || cfg.IsResponsesWebSocket() {
		resp, err := doResponsesRequestWithTools(ctx, cfg, conversation, tools, httpClient)
		if err == nil && resp != nil && len(resp.Choices) > 0 && len(resp.Choices[0].Message.ToolCalls) == 0 {
			text := resp.Choices[0].Message.Content
			if text == "" {
				text = resp.Choices[0].Message.ReasoningContent
			}
			if text != "" && onToken != nil {
				onToken(text)
			}
		}
		flushRolePrefixFilter(err)
		return resp, err
	}

	if cfg.Protocol == "anthropic" {
		resp, err := llm.DoAnthropicRequestStream(ctx, cfg, conversation, tools, httpClient, onToken)
		flushRolePrefixFilter(err)
		if err != nil {
			// Fallback to non-streaming on error.
			log.Printf("[agent-loop] streaming failed, falling back to non-stream: %v", err)
			return llm.DoAnthropicRequest(ctx, cfg, conversation, tools, httpClient)
		}
		return resp, nil
	}
	resp, err := llm.DoOpenAIRequestStream(ctx, cfg, conversation, tools, httpClient, onToken)
	flushRolePrefixFilter(err)
	if err != nil {
		log.Printf("[agent-loop] streaming failed, falling back to non-stream: %v", err)
		return llm.DoOpenAIRequest(ctx, cfg, conversation, tools, httpClient)
	}
	return resp, nil
}
