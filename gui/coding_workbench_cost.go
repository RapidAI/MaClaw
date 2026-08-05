package main

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
		_, _, total := corelib.CalculateLLMCostRMB(
			int64(input),
			int64(output),
			corelib.DefaultLLMInputPricePerMTokensRMB,
			corelib.DefaultLLMOutputPricePerMTokensRMB,
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
