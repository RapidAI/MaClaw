package guiapp

import (
	"encoding/json"
	"log"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/toolresult"
)

const projectedToolArgumentsMinBytes = 2000

func (h *IMMessageHandler) projectOversizedToolCallArguments(conversation []interface{}, history []agent.ConversationEntry, tc llm.ToolCall, userID, recordToolCallID string, recorder *TrajectoryRecorder) ([]interface{}, []agent.ConversationEntry) {
	if len(tc.Function.Arguments) <= projectedToolArgumentsMinBytes {
		return conversation, history
	}
	projection, err := toolresult.Project(toolresult.ProjectOptions{
		ToolName:   "tool_call_arguments",
		SessionKey: userID,
		Content:    tc.Function.Arguments,
		Preview:    tc.Function.Arguments,
		Limit:      2048,
		ForceSpill: true,
		Encrypt:    true,
	})
	if err != nil || projection.Handle == nil || !projection.Spilled {
		log.Printf("[tool-args] keep original arguments: spill failed tool=%s call=%s err=%v", tc.Function.Name, tc.ID, err)
		return conversation, history
	}
	payload, err := json.Marshal(map[string]interface{}{
		"_context_compressed": true,
		"tool":                tc.Function.Name,
		"original_bytes":      len(tc.Function.Arguments),
		"preview":             projection.Preview,
	})
	if err != nil {
		log.Printf("[tool-args] keep original arguments: preview marshal failed: %v", err)
		return conversation, history
	}
	summary := string(payload)
	for _, msg := range conversation {
		if msgRole(msg) != "assistant" {
			continue
		}
		m, ok := msg.(map[string]interface{})
		if !ok {
			continue
		}
		m["tool_calls"] = rewriteToolCallsArguments(m["tool_calls"], recordToolCallID, summary)
	}
	for i := range history {
		if history[i].Role != "assistant" || history[i].ToolCalls == nil {
			continue
		}
		history[i].ToolCalls = rewriteToolCallsArguments(history[i].ToolCalls, recordToolCallID, summary)
	}
	if recorder != nil {
		recorder.TruncateToolCallArguments(recordToolCallID, summary)
	}
	log.Printf("[tool-args] projected oversized arguments tool=%s call=%s bytes=%d handle=%s", tc.Function.Name, recordToolCallID, len(tc.Function.Arguments), projection.Handle.ID)
	return conversation, history
}
