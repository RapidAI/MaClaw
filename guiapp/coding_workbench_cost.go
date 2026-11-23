package guiapp

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// codingLoopUsageFields extracts token/cost from a loop usage snapshot.
// input is the lifetime sum of prompt sizes. The context-window fill is
// codingLoopWindowUsage's lastPrompt, which is one request.
func codingLoopUsageFields(u agent.TurnUsage) (input, output int, estCostRMB float64) {
	input, output, estCostRMB, _, _ = codingLoopWindowUsage(u)
	return input, output, estCostRMB
}

// codingLoopWindowUsage splits the session sums from the latest request.
// A single-request usage with no last-request field uses that request's own
// input or output. A multi-request sum does not: the window meter and 本轮
// out must not treat the lifetime sum as one request.
func codingLoopWindowUsage(u agent.TurnUsage) (input, output int, estCostRMB float64, lastPrompt, lastCompletion int) {
	input = u.InputTokens
	output = u.OutputTokens
	lastPrompt = u.LastPromptTokens
	if lastPrompt <= 0 && u.Requests <= 1 && input > 0 {
		lastPrompt = input
	}
	lastCompletion = u.LastCompletionTokens
	if lastCompletion <= 0 && u.Requests <= 1 && output > 0 {
		lastCompletion = output
	}
	if u.EstCostRMB > 0 {
		estCostRMB = u.EstCostRMB
	} else if input > 0 || output > 0 {
		_, _, _, _, total := corelib.CalculateLLMCostRMBWithCache(
			int64(input), int64(output), int64(u.CachedTokens), int64(u.CacheWriteTokens),
			corelib.DefaultLLMInputPricePerMTokensRMB,
			corelib.DefaultLLMOutputPricePerMTokensRMB,
			corelib.DefaultLLMInputPricePerMTokensRMB/10,
			corelib.DefaultLLMInputPricePerMTokensRMB,
		)
		estCostRMB = total
	}
	return input, output, estCostRMB, lastPrompt, lastCompletion
}

func (h *IMMessageHandler) accumulateStickyCodingUsage(userID string, input, output int, cost float64) {
	// One call is one prompt: the same value is both the session delta and the
	// window fill. A multi-request sum must use recordStickyCodingUsage so the
	// fill stays the last prompt.
	h.recordStickyCodingUsage(userID, input, input, output, output, cost)
}

// recordStickyCodingUsage adds the session sums and records one request as
// the window fill and 本轮 out. lastInput is one prompt. lastOutput is one
// completion. Passing either lifetime sum paints the meter or 本轮 out as
// the whole task. A zero lastOutput leaves the live completion in place.
func (h *IMMessageHandler) recordStickyCodingUsage(userID string, sessionInput, lastInput, sessionOutput, lastOutput int, cost float64) {
	if h == nil || userID == "" {
		return
	}
	if sessionInput == 0 && sessionOutput == 0 && cost == 0 && lastInput == 0 {
		return
	}
	window := h.codingContextWindowTokens(userID)
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.SessionInputTokens += sessionInput
		mem.SessionOutputTokens += sessionOutput
		mem.SessionEstCostRMB += cost
		if lastInput > 0 {
			mem.LastTurnInputTokens = codingPromptFill(mem.LastTurnInputTokens, lastInput, window)
		}
		if lastOutput > 0 {
			mem.LastTurnOutputTokens = lastOutput
		}
		if cost > 0 || lastInput > 0 || sessionInput > 0 {
			mem.LastTurnEstCostRMB = cost
		}
	})
}

// noteCodingContextFill publishes the latest provider prompt as the live
// window fill. It does not add to the session total; that sum is recorded
// once when the task finishes.
func (h *IMMessageHandler) noteCodingContextFill(userID string, promptTokens, outputTokens int) {
	if h == nil || userID == "" || promptTokens <= 0 {
		return
	}
	window := h.codingContextWindowTokens(userID)
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		next := codingPromptFill(mem.LastTurnInputTokens, promptTokens, window)
		if next <= 0 {
			return
		}
		mem.LastTurnInputTokens = next
		if outputTokens > 0 {
			mem.LastTurnOutputTokens = outputTokens
		}
	})
	mem := h.getStickyCodingWorkbenchMemory(userID)
	h.publishContextWindowUsage(userID, mem.LastTurnInputTokens, window)
}

func (h *IMMessageHandler) publishContextWindowUsage(userID string, usedTokens, maxTokens int) {
	if h == nil || h.app == nil || userID == "" || maxTokens <= 0 {
		return
	}
	if usedTokens < 0 {
		usedTokens = 0
	}
	h.app.emitAIAssistantStreamEvent("ai-assistant-context-usage", AIAssistantStreamEvent{
		SessionKey:        userID,
		ContextUsedTokens: usedTokens,
		ContextMaxTokens:  maxTokens,
	})
}

func (h *IMMessageHandler) recordStickyCodingRoute(userID, model, source, task, reason string) {
	if h == nil || userID == "" {
		return
	}
	model = strings.TrimSpace(model)
	if model == "" && source == "" && task == "" {
		return
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.LastRouteModel = model
		mem.LastRouteSource = strings.TrimSpace(source)
		mem.LastRouteTask = strings.TrimSpace(task)
		mem.LastRouteReason = truncateRunesForSubAgent(strings.TrimSpace(reason), 200)
	})
}

func (h *IMMessageHandler) applyCodingUsageToResponse(userID string, resp *IMAgentResponse, input, output int, cost float64) {
	if resp == nil {
		return
	}
	if input > 0 {
		resp.InputTokens = input
	}
	if output > 0 {
		resp.OutputTokens = output
	}
	if input > 0 || output > 0 {
		resp.TotalTokens = input + output
	}
	if cost > 0 {
		resp.EstCostRMB = cost
	}
	// Session totals as extra fields for observability.
	if h != nil && userID != "" {
		mem := h.getStickyCodingWorkbenchMemory(userID)
		if mem.SessionInputTokens > 0 || mem.SessionOutputTokens > 0 || mem.SessionEstCostRMB > 0 {
			resp.Fields = append(resp.Fields,
				IMResponseField{Label: "session_tokens", Value: fmt.Sprintf("in=%d out=%d total=%d", mem.SessionInputTokens, mem.SessionOutputTokens, mem.SessionInputTokens+mem.SessionOutputTokens), Internal: true},
			)
			if mem.SessionEstCostRMB > 0 {
				resp.Fields = append(resp.Fields,
					IMResponseField{Label: "session_est_cost_rmb", Value: fmt.Sprintf("%.4f", mem.SessionEstCostRMB), Internal: true},
				)
			}
		}
	}
	if cost > 0 || input > 0 || output > 0 {
		// Compact footer for chat text when not already present.
		if resp.Text != "" && !strings.Contains(resp.Text, "token") && !strings.Contains(resp.Text, "¥") {
			// keep text clean — fields carry the detail
		}
	}
}

// codingContextWindowTokens resolves the context-window cap for a session's
// effective coding model. A task pin or route pref can override the coding
// assignment's context_length, so the turn's own resolution order is kept:
// mcp live round → task pin → coding assignment → route pref. The pin read is
// skipped while the memory store is still cold — a status poll must not
// initialize it (same rule remoteCodingMetaFromTaskTags follows). 0 means no
// model could be resolved — the UI must not invent a cap.
func (h *IMMessageHandler) codingContextWindowTokens(userID string) int {
	if h == nil {
		return 0
	}
	cfg := h.getCodingLLMConfig()
	if h.app != nil && (h.standaloneConfig == nil || h.standaloneConfig.LLMConfigFunc == nil) {
		if caller, ok := h.app.mcpLoopModelConfig(userID); ok && strings.TrimSpace(caller.Model) != "" {
			caller.Profile = maclawLLMProfileCoding
			caller.RouteSource = mcpCallerModelRoute
			cfg = caller
		} else if h.app.memoryStore != nil {
			if pinned, ok := h.app.taskBoundLLMConfig(projectPathFromSessionOwnerID(userID), maclawLLMProfileCoding); ok {
				cfg = pinned
			}
		}
	}
	cfg = h.applyCodingRoutePreference(userID, cfg, false)
	if strings.TrimSpace(cfg.Model) == "" {
		return 0
	}
	return cfg.EffectiveContextTokens()
}

func formatCodingSessionCostLine(mem stickyCodingWorkbenchMemory) string {
	if mem.SessionInputTokens == 0 && mem.SessionOutputTokens == 0 && mem.SessionEstCostRMB == 0 {
		return ""
	}
	line := fmt.Sprintf("会话用量: in=%d out=%d total=%d",
		mem.SessionInputTokens, mem.SessionOutputTokens, mem.SessionInputTokens+mem.SessionOutputTokens)
	if mem.SessionEstCostRMB > 0 {
		line += fmt.Sprintf(" · ~¥%.4f", mem.SessionEstCostRMB)
	}
	if mem.LastTurnInputTokens > 0 || mem.LastTurnOutputTokens > 0 {
		line += fmt.Sprintf("（本轮 in=%d out=%d", mem.LastTurnInputTokens, mem.LastTurnOutputTokens)
		if mem.LastTurnEstCostRMB > 0 {
			line += fmt.Sprintf(" · ~¥%.4f", mem.LastTurnEstCostRMB)
		}
		line += "）"
	}
	return line
}
