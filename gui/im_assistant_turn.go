package main

import (
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

type agentLoopAssistantTurn struct {
	Content      string
	Reasoning    string
	Message      map[string]interface{}
	HistoryEntry agent.ConversationEntry
}

type agentLoopAssistantTurnCommitOptions struct {
	Context        *LoopContext
	Choice         llm.Choice
	Conversation   []interface{}
	History        []agent.ConversationEntry
	Recorder       *TrajectoryRecorder
	Iteration      int
	EffectiveMax   int
	StreamDone     bool
	ReportActivity func(int, int, string)
}

type agentLoopAssistantTurnCommitResult struct {
	Turn                 agentLoopAssistantTurn
	Conversation         []interface{}
	History              []agent.ConversationEntry
	AssistantMsgElapsed  time.Duration
	HistoryAppendElapsed time.Duration
}

type agentLoopPostLLMTurnOptions struct {
	Context                       *LoopContext
	Response                      *llm.Response
	UserID                        string
	Iteration                     int
	Platform                      string
	EffectiveMax                  int
	GateConfig                    codingToolGateConfig
	SkipCodingGate                bool
	OrchestratorActive            func() bool
	Conversation                  []interface{}
	History                       []agent.ConversationEntry
	Recorder                      *TrajectoryRecorder
	Phase                         *agentLoopPhase
	SteeringDetector              *SteeringWorkflowDetector
	StreamDone                    bool
	ReportActivity                func(int, int, string)
	RecordSystemMessages          func(int, []interface{})
	AttachLLMTelemetry            func(*IMAgentResponse)
	AttachPendingVisibleArtifacts func(*IMAgentResponse)
}

type agentLoopPostLLMTurnResult struct {
	Choice                  llm.Choice
	MessageContent          string
	MessageReasoning        string
	AssistantMessage        map[string]interface{}
	Conversation            []interface{}
	History                 []agent.ConversationEntry
	AssistantMessageElapsed time.Duration
	HistoryAppendElapsed    time.Duration
	PostStreamChoiceElapsed time.Duration
	Response                *IMAgentResponse
	ContinueLoop            bool
}

func (h *IMMessageHandler) handleAgentLoopPostLLMTurn(opts agentLoopPostLLMTurnOptions) agentLoopPostLLMTurnResult {
	choiceStartedAt := time.Now()
	choice := opts.Response.Choices[0]
	result := agentLoopPostLLMTurnResult{
		Choice:       choice,
		Conversation: opts.Conversation,
		History:      opts.History,
	}
	if opts.StreamDone {
		result.PostStreamChoiceElapsed = time.Since(choiceStartedAt)
	}

	assistantCommit := h.commitAgentLoopAssistantTurn(agentLoopAssistantTurnCommitOptions{
		Context:        opts.Context,
		Choice:         choice,
		Conversation:   opts.Conversation,
		History:        opts.History,
		Recorder:       opts.Recorder,
		Iteration:      opts.Iteration,
		EffectiveMax:   opts.EffectiveMax,
		StreamDone:     opts.StreamDone,
		ReportActivity: opts.ReportActivity,
	})
	assistantTurn := assistantCommit.Turn
	result.MessageContent = assistantTurn.Content
	result.MessageReasoning = assistantTurn.Reasoning
	result.AssistantMessage = assistantTurn.Message
	result.Conversation = assistantCommit.Conversation
	result.History = assistantCommit.History
	result.AssistantMessageElapsed = assistantCommit.AssistantMsgElapsed
	result.HistoryAppendElapsed = assistantCommit.HistoryAppendElapsed

	codingGateResult := h.applyAgentLoopCodingGateAfterAssistantTurn(
		opts.Context,
		opts.UserID,
		opts.Iteration,
		opts.Platform,
		opts.GateConfig,
		opts.SkipCodingGate,
		opts.OrchestratorActive,
		&choice,
		result.AssistantMessage,
		result.Conversation,
		result.History,
		result.MessageContent,
		result.MessageReasoning,
		opts.Phase,
		opts.SteeringDetector,
		opts.RecordSystemMessages,
		opts.AttachLLMTelemetry,
		opts.AttachPendingVisibleArtifacts,
	)
	result.Choice = choice
	result.Conversation = codingGateResult.Conversation
	result.History = codingGateResult.History
	result.Response = codingGateResult.Response
	result.ContinueLoop = codingGateResult.ContinueLoop
	return result
}

func (h *IMMessageHandler) commitAgentLoopAssistantTurn(opts agentLoopAssistantTurnCommitOptions) agentLoopAssistantTurnCommitResult {
	assistantMsgStartedAt := time.Now()
	assistantTurn := h.buildAgentLoopAssistantTurn(opts.Context, opts.Choice)
	conversation := append(opts.Conversation, assistantTurn.Message)
	if opts.Recorder != nil {
		opts.Recorder.Record("assistant", assistantTurn.Content, opts.Choice.Message.ToolCalls, "", assistantTurn.Reasoning)
	}
	result := agentLoopAssistantTurnCommitResult{
		Turn:         assistantTurn,
		Conversation: conversation,
		History:      opts.History,
	}
	if opts.StreamDone {
		result.AssistantMsgElapsed = time.Since(assistantMsgStartedAt)
	}

	if opts.ReportActivity != nil && opts.Iteration%5 == 0 {
		opts.ReportActivity(opts.Iteration, opts.EffectiveMax, assistantTurn.Content)
	}

	historyAppendStartedAt := time.Now()
	result.History = append(result.History, assistantTurn.HistoryEntry)
	if opts.StreamDone {
		result.HistoryAppendElapsed = time.Since(historyAppendStartedAt)
	}
	return result
}

func (h *IMMessageHandler) buildAgentLoopAssistantTurn(ctx *LoopContext, choice llm.Choice) agentLoopAssistantTurn {
	msgContent := choice.Message.Content
	msgReasoning := stripRolePrefixHallucination(choice.Message.ReasoningContent)
	if msgContent == "" && msgReasoning != "" {
		msgContent = msgReasoning
	}

	beforeStripRP := msgContent
	msgContent = stripRolePrefixHallucination(msgContent)
	BrowserDiagCP6_PostProcess(beforeStripRP, msgContent)

	assistantMsg := map[string]interface{}{
		"role":    "assistant",
		"content": msgContent,
	}
	if h.traceService != nil && ctx.RunID != "" {
		h.appendTraceEvent(ctx, "assistant.response", "info", "Assistant response", truncateTraceText(msgContent, 220), "", "")
	}
	if msgReasoning != "" {
		assistantMsg["reasoning_content"] = msgReasoning
	} else {
		// DeepSeek V4+ thinking mode: when tools are present in the request,
		// reasoning_content must exist on all assistant messages.
		assistantMsg["reasoning_content"] = ""
	}
	if len(choice.Message.ToolCalls) > 0 {
		assistantMsg["tool_calls"] = choice.Message.ToolCalls
	}

	historyEntry := agent.ConversationEntry{
		Role:             "assistant",
		Content:          msgContent,
		ReasoningContent: msgReasoning,
	}
	if len(choice.Message.ToolCalls) > 0 {
		historyEntry.ToolCalls = choice.Message.ToolCalls
	}

	return agentLoopAssistantTurn{
		Content:      msgContent,
		Reasoning:    msgReasoning,
		Message:      assistantMsg,
		HistoryEntry: historyEntry,
	}
}
