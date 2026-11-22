package guiapp

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// codingLoopUsageFields extracts token/cost from a loop usage snapshot.
func codingLoopUsageFields(u agent.TurnUsage) (input, output int, estCostRMB float64) {
	input = u.InputTokens
	output = u.OutputTokens
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
	return input, output, estCostRMB
}

func (h *IMMessageHandler) accumulateStickyCodingUsage(userID string, input, output int, cost float64) {
	if h == nil || userID == "" {
		return
	}
	if input == 0 && output == 0 && cost == 0 {
		return
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.SessionInputTokens += input
		mem.SessionOutputTokens += output
		mem.SessionEstCostRMB += cost
		mem.LastTurnInputTokens = input
		mem.LastTurnOutputTokens = output
		mem.LastTurnEstCostRMB = cost
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
