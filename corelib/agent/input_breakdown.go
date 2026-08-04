package agent

import "sync/atomic"

// LoopInputBreakdown is an exclusive estimate of one model request's input.
// It is intentionally provider-independent: provider-reported usage remains the
// billing source of truth, while this split identifies where context is spent.
type LoopInputBreakdown struct {
	SystemPromptTokens   int `json:"system_prompt_tokens"`
	ToolDefinitionTokens int `json:"tool_definition_tokens"`
	HistoryTokens        int `json:"history_tokens"`
	ToolResultTokens     int `json:"tool_result_tokens"`
	TotalEstimatedTokens int `json:"total_estimated_tokens"`
}

// LoopInputBreakdownObserver lets a host retain the per-request split alongside
// its existing turn telemetry without changing the model request or UI.
type LoopInputBreakdownObserver interface {
	OnLoopInputBreakdown(LoopInputBreakdown)
}

// EstimateLoopInputBreakdown classifies conversation messages into exclusive
// buckets. HistoryTokens contains user/assistant/other non-system messages.
func EstimateLoopInputBreakdown(conversation []interface{}, tools []map[string]interface{}) LoopInputBreakdown {
	var out LoopInputBreakdown
	for _, msg := range conversation {
		classifyLoopInputMessage(&out, msg)
	}
	out.ToolDefinitionTokens = EstimateToolsTokens(tools)
	out.TotalEstimatedTokens = out.SystemPromptTokens + out.ToolDefinitionTokens + out.HistoryTokens + out.ToolResultTokens
	return out
}

func classifyLoopInputMessage(out *LoopInputBreakdown, msg interface{}) {
	if out == nil {
		return
	}
	role := MsgRole(msg)
	if role != "tool" {
		tokens := EstimateConversationTokens([]interface{}{msg})
		if role == "system" {
			out.SystemPromptTokens += tokens
		} else {
			out.HistoryTokens += tokens
		}
		return
	}

	// A tool message contains protocol/history envelope plus the actual tool
	// payload. Split them so the buckets remain mutually exclusive and their
	// sum still equals the exact estimate of the original message.
	whole := EstimateConversationTokens([]interface{}{msg})
	contentOnly := loopToolMessageContentOnly(msg)
	contentTokens := EstimateConversationTokens([]interface{}{contentOnly})
	if contentTokens > whole {
		contentTokens = whole
	}
	// Attribute all estimator interaction/JSON overhead to history. This keeps
	// the content bucket conservative while preserving exact additivity.
	out.ToolResultTokens += contentTokens
	out.HistoryTokens += whole - contentTokens
}

func loopToolMessageContentOnly(msg interface{}) interface{} {
	switch m := msg.(type) {
	case map[string]interface{}:
		return map[string]interface{}{"content": m["content"]}
	case map[string]string:
		return map[string]string{"content": m["content"]}
	default:
		return map[string]string{"content": ""}
	}
}

type loopInputBreakdownCounters struct {
	requests       atomic.Int64
	system         atomic.Int64
	tools          atomic.Int64
	history        atomic.Int64
	toolResults    atomic.Int64
	totalEstimated atomic.Int64
}

var globalLoopInputBreakdown loopInputBreakdownCounters

// LoopInputBreakdownStats is the process-local baseline used by diagnostics.
type LoopInputBreakdownStats struct {
	Requests             int64 `json:"requests"`
	SystemPromptTokens   int64 `json:"system_prompt_tokens"`
	ToolDefinitionTokens int64 `json:"tool_definition_tokens"`
	HistoryTokens        int64 `json:"history_tokens"`
	ToolResultTokens     int64 `json:"tool_result_tokens"`
	TotalEstimatedTokens int64 `json:"total_estimated_tokens"`
}

// RecordLoopInputBreakdown records one request without retaining prompt text.
func RecordLoopInputBreakdown(b LoopInputBreakdown) {
	globalLoopInputBreakdown.requests.Add(1)
	globalLoopInputBreakdown.system.Add(int64(b.SystemPromptTokens))
	globalLoopInputBreakdown.tools.Add(int64(b.ToolDefinitionTokens))
	globalLoopInputBreakdown.history.Add(int64(b.HistoryTokens))
	globalLoopInputBreakdown.toolResults.Add(int64(b.ToolResultTokens))
	globalLoopInputBreakdown.totalEstimated.Add(int64(b.TotalEstimatedTokens))
}

// CurrentLoopInputBreakdownStats returns a race-safe process-local snapshot.
func CurrentLoopInputBreakdownStats() LoopInputBreakdownStats {
	return LoopInputBreakdownStats{
		Requests:             globalLoopInputBreakdown.requests.Load(),
		SystemPromptTokens:   globalLoopInputBreakdown.system.Load(),
		ToolDefinitionTokens: globalLoopInputBreakdown.tools.Load(),
		HistoryTokens:        globalLoopInputBreakdown.history.Load(),
		ToolResultTokens:     globalLoopInputBreakdown.toolResults.Load(),
		TotalEstimatedTokens: globalLoopInputBreakdown.totalEstimated.Load(),
	}
}

func observeLoopInputBreakdown(cb LoopCallbacks, conversation []interface{}, tools []map[string]interface{}) {
	breakdown := EstimateLoopInputBreakdown(conversation, tools)
	RecordLoopInputBreakdown(breakdown)
	if observer, ok := cb.(LoopInputBreakdownObserver); ok {
		observer.OnLoopInputBreakdown(breakdown)
	}
}
