package main

import (
	"log"
	"net/http"
	"strings"
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
	// SendProgress surfaces early work (e.g. IM voice ASR) before the main loop.
	SendProgress func(string)
}

type agentLoopStartState struct {
	Config                        corelib.MaclawLLMConfig
	RouteDecision                 modelRouteDecision
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
	RecordToolResult              func(string, interface{}, string, string)
	ReportActivity                func(int, int, string)
	Conversation                  []interface{}
	History                       []agent.ConversationEntry
	UserContent                   interface{}
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
	// Rule-based turn routing: cheap tasks → aux/fast routes; coding → primary/reasoning.
	// Decision is applied onto runState in the dispatcher after startState returns.
	allowLocalAttachmentStaging := ctx == nil || ctx.LansengerGroupPermissions == nil
	userText := opts.UserText
	attachments := append([]MessageAttachment(nil), opts.Attachments...)
	if allowLocalAttachmentStaging {
		localImages, notes := selectedLocalImageAttachments(userText)
		attachments = append(attachments, localImages...)
		if len(notes) > 0 {
			userText = strings.TrimSpace(userText + "\n\n" + strings.Join(notes, "\n"))
		}
	}
	routedCfg, routeDecision := h.applyTurnModelRoute(cfg, userText, ctx, attachments)
	cfg = routedCfg
	phase := h.initialAgentLoopPhase(userText, ctx)

	// Desktop image paths are materialized above before routing. Reuse that
	// same current-turn attachment set for tool routing as well.
	toolRoutingText := computerUseRoutingText(userText, attachments)
	if ctx != nil {
		ctx.ComputerUseBlockedForLocalFileWork = localFileWorkBlocksComputerUse(toolRoutingText)
	}
	toolSet := h.prepareAgentLoopTools(opts.UserID, toolRoutingText, ctx, phase)
	tools := toolSet.Tools
	toolsTokenBudget := toolSet.ToolsTokenBudget
	if telemetry != nil {
		telemetry.PreLLMToolsElapsed = toolSet.PreparationTime
	}

	recorderBundle := h.prepareAgentLoopRecorderBundle(opts.AdaptiveRetry)
	cleanupFns = append(cleanupFns, recorderBundle.Cleanup)
	visibleArtifacts := &pendingVisibleArtifacts{}

	reportActivity, cleanupActivity, crossChannelPrompt := h.startAgentLoopActivity(opts.UserID, opts.Platform, opts.UserText, configStart.MaxIterations)
	cleanupFns = append(cleanupFns, cleanupActivity)
	systemPrompt := opts.SystemPrompt
	if extra := crossChannelPrompt; extra != "" {
		systemPrompt += extra
	}

	loopID := ""
	if ctx != nil {
		loopID = ctx.ID
	}
	conversationStart := h.buildAgentLoopConversationStart(loopID, opts.UserID, userText, systemPrompt, opts.Platform, attachments, cfg, opts.History, opts.PriorReplanCount, recorderBundle.Recorder, tools, opts.SendProgress, allowLocalAttachmentStaging)
	if telemetry != nil {
		telemetry.PreLLMConversationElapsed = conversationStart.Elapsed
	}

	BrowserDiagCP4_FinalToolList(tools, 0, len(tools))

	limits := computeAgentLoopIterationLimits(ctx, configStart.MaxIterations, opts.MinIterations)
	loopKind := LoopKind(0)
	if ctx != nil {
		loopKind = ctx.Kind
	}
	log.Printf("[AgentLoop] start loop=%s kind=%d maxIter=%d effectiveMax=%d minIterations=%d configCap=%d grace=%d user=%q task=%q",
		loopID, loopKind, configStart.MaxIterations, limits.EffectiveMax, opts.MinIterations, config.MaxAgentIterationsCap, limits.ChatFinalizeGrace, opts.UserID, truncateRunes(userText, 80))
	if ctx != nil && ctx.Runtime.Execution.Layer != "" {
		log.Printf("[exec-router] request_id=%q user=%q layer=%s task=%s prompt=%s confidence=%.2f reason=%q tool_budget=%d iteration_budget=%d",
			ctx.Runtime.RequestID, opts.UserID, ctx.Runtime.Execution.Layer, ctx.Runtime.Execution.TaskType, ctx.Runtime.Execution.PromptProfile, ctx.Runtime.Execution.Confidence, ctx.Runtime.Execution.Reason, ctx.Runtime.Execution.ToolBudget, ctx.Runtime.Execution.IterationBudget)
	}

	// Legacy coding gate removed; tools pass through unfiltered.

	return agentLoopStartState{
		Config:                        cfg,
		RouteDecision:                 routeDecision,
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
		UserContent:                   conversationStart.UserContent,
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
