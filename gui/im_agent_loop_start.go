package main

import (
	"log"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/progress"
)

type agentLoopStartOptions struct {
	Context          *LoopContext
	UserID           string
	UserText         string
	SystemPrompt     string
	Platform         string
	Attachments      []MessageAttachment
	History          []agent.ConversationEntry
	MinIterations    int
	PriorReplanCount int
	AdaptiveRetry    *AdaptiveRetry
	MilestoneTracker *progress.AgentProgressTracker
	Telemetry        *agentLoopTelemetry
}

type agentLoopStartState struct {
	Config                        corelib.MaclawLLMConfig
	TrialState                    *trialReflectState
	MaxIterations                 int
	SystemPrompt                  string
	Phase                         agentLoopPhase
	BaseTools                     []map[string]interface{}
	Tools                         []map[string]interface{}
	ToolsTokenBudget              int
	HTTPClient                    *http.Client
	Recorder                      *TrajectoryRecorder
	AdaptiveRetry                 *AdaptiveRetry
	VisibleArtifacts              *pendingVisibleArtifacts
	AttachPendingVisibleArtifacts func(*IMAgentResponse)
	RecordSystemMessages          func(int, []interface{})
	RecordToolCall                func(string, string, string)
	RecordToolResult              func(string, interface{})
	ReportActivity                func(int, int, string)
	Conversation                  []interface{}
	History                       []agent.ConversationEntry
	ConversationStartedAt         time.Time
	EffectiveMax                  int
	ChatFinalizeGrace             int
	Cleanup                       func()
}

func (h *IMMessageHandler) prepareAgentLoopStartState(opts agentLoopStartOptions) agentLoopStartState {
	ctx := opts.Context
	telemetry := opts.Telemetry
	cleanupFns := make([]func(), 0, 2)

	configStart := h.prepareAgentLoopConfig(ctx)
	cfg := configStart.Config
	if telemetry != nil {
		telemetry.PreLLMConfigElapsed = configStart.Elapsed
	}
	phase := h.initialAgentLoopPhase(opts.UserText, ctx)

	toolSet := h.prepareAgentLoopTools(opts.UserID, opts.UserText, ctx, phase)
	tools := toolSet.Tools
	toolsTokenBudget := toolSet.ToolsTokenBudget
	if telemetry != nil {
		telemetry.PreLLMToolsElapsed = toolSet.PreparationTime
	}

	recorderBundle := h.prepareAgentLoopRecorderBundle(opts.AdaptiveRetry)
	cleanupFns = append(cleanupFns, recorderBundle.Cleanup)
	visibleArtifacts := &pendingVisibleArtifacts{}

	reportActivity, cleanupActivity, crossChannelPrompt := h.startAgentLoopActivity(opts.Platform, opts.UserText, configStart.MaxIterations)
	cleanupFns = append(cleanupFns, cleanupActivity)
	systemPrompt := opts.SystemPrompt
	if extra := crossChannelPrompt; extra != "" {
		systemPrompt += extra
	}

	conversationStart := h.buildAgentLoopConversationStart(ctx.ID, opts.UserID, opts.UserText, systemPrompt, opts.Platform, opts.Attachments, cfg, opts.History, opts.PriorReplanCount, recorderBundle.Recorder, tools)
	if telemetry != nil {
		telemetry.PreLLMConversationElapsed = conversationStart.Elapsed
	}

	BrowserDiagCP4_FinalToolList(tools, 0, len(tools))

	limits := computeAgentLoopIterationLimits(ctx, configStart.MaxIterations, opts.MinIterations)
	log.Printf("[AgentLoop] start loop=%s kind=%d maxIter=%d effectiveMax=%d minIterations=%d configCap=%d grace=%d user=%q task=%q",
		ctx.ID, ctx.Kind, configStart.MaxIterations, limits.EffectiveMax, opts.MinIterations, config.MaxAgentIterationsCap, limits.ChatFinalizeGrace, opts.UserID, truncateRunes(opts.UserText, 80))
	if ctx.Runtime.Execution.Layer != "" {
		log.Printf("[exec-router] request_id=%q user=%q layer=%s task=%s prompt=%s confidence=%.2f reason=%q tool_budget=%d iteration_budget=%d",
			ctx.Runtime.RequestID, opts.UserID, ctx.Runtime.Execution.Layer, ctx.Runtime.Execution.TaskType, ctx.Runtime.Execution.PromptProfile, ctx.Runtime.Execution.Confidence, ctx.Runtime.Execution.Reason, ctx.Runtime.Execution.ToolBudget, ctx.Runtime.Execution.IterationBudget)
	}

	// V1 coding gate removed; tools pass through unfiltered.

	return agentLoopStartState{
		Config:                        cfg,
		TrialState:                    configStart.TrialState,
		MaxIterations:                 configStart.MaxIterations,
		SystemPrompt:                  systemPrompt,
		Phase:                         phase,
		BaseTools:                     toolSet.BaseTools,
		Tools:                         tools,
		ToolsTokenBudget:              toolsTokenBudget,
		HTTPClient:                    ctx.HTTPClient,
		Recorder:                      recorderBundle.Recorder,
		AdaptiveRetry:                 recorderBundle.AdaptiveRetry,
		VisibleArtifacts:              visibleArtifacts,
		AttachPendingVisibleArtifacts: visibleArtifacts.Attach,
		RecordSystemMessages:          recorderBundle.RecordSystemMessages,
		RecordToolCall:                recorderBundle.RecordToolCall,
		RecordToolResult:              recorderBundle.RecordToolResult,
		ReportActivity:                reportActivity,
		Conversation:                  conversationStart.Conversation,
		History:                       conversationStart.History,
		ConversationStartedAt:         conversationStart.StartedAt,
		EffectiveMax:                  limits.EffectiveMax,
		ChatFinalizeGrace:             limits.ChatFinalizeGrace,
		Cleanup: func() {
			for i := len(cleanupFns) - 1; i >= 0; i-- {
				if cleanupFns[i] != nil {
					cleanupFns[i]()
				}
			}
		},
	}
}
