package guiapp

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/intent"
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
	ClientToolNames               []string
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
	TaskAnchor                    *taskIdentityAnchor
	ConversationStartedAt         time.Time
	EffectiveMax                  int
	ChatFinalizeGrace             int
	SemanticSurface               *semanticCallSurface
	HostReject                    *IMAgentResponse
	Cleanup                       func()
}

func (h *IMMessageHandler) prepareAgentLoopStartState(opts agentLoopStartOptions) agentLoopStartState {
	ctx := opts.Context
	if ctx != nil {
		ctx.autoExtractExpandedText = ""
		if len(opts.History) > 0 {
			// Planning reads LoopContext.History. The live conversation is the
			// start-state snapshot; without this bind, a restated weather+PDF
			// turn still thinks lookup facts are missing.
			ctx.History = opts.History
		}
	}
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
	if err := h.captureSelectedLocalDocuments(opts.UserID, opts.Platform, sessionGovernedDestination(ctx), userText); err != nil {
		// The semantic planner will fail closed if this turn actually requests a
		// document read. Keep non-document work usable while never retaining the
		// prior resource after a failed new picker selection.
		log.Printf("[semantic-routing] selected document context rejected user=%q reason=%v", opts.UserID, err)
	}
	if ctx != nil {
		applyStagedImageUnderstandRuntime(ctx, userText, attachments)
	}
	routedCfg, routeDecision := h.applyTurnModelRoute(cfg, userText, ctx, attachments)
	cfg = routedCfg
	phase := h.initialAgentLoopPhase(opts.UserText, ctx)

	// Desktop image paths are materialized above before routing. Reuse that
	// same current-turn attachment set for tool routing as well.
	toolRoutingText := computerUseRoutingText(userText, attachments)
	if ctx != nil {
		if strings.TrimSpace(ctx.Platform) == "" {
			ctx.Platform = opts.Platform
		}
		ctx.ComputerUseBlockedForLocalFileWork = localFileWorkBlocksComputerUse(toolRoutingText)
		if strings.TrimSpace(ctx.ComputerUseRoutingText) == "" {
			ctx.ComputerUseRoutingText = toolRoutingText
		}
		// Record fresh even when semantic routing skips prepareAgentLoopTools.
		syncComputerUseTurn(h, ctx, opts.UserID, opts.UserText)
	}
	// A governed capability is planned before the legacy pipeline is even
	// considered.  Calling prepareAgentLoopTools first was not harmless: it
	// still exercised the name/keyword router and its pin/filter side effects,
	// even though its output was later overwritten by the semantic surface.
	// The ToolPlanner is the selection authority for a managed turn.
	var toolSet agentLoopToolSet
	var tools []map[string]interface{}
	var baseTools []map[string]interface{}
	var semanticSurface *semanticCallSurface
	var hostReject *IMAgentResponse
	semanticHandled := false
	if petCompanionToolsDisabled(opts.UserID) {
		semanticHandled = true
		log.Printf("[pet-companion] chat turn without tools user=%q", opts.UserID)
	} else if fileCompanionTurnWithoutTools(opts.UserID, opts.Platform) {
		// Skipping prepareAgentLoopTools is not an empty surface: semantic
		// routing would still attach the shared tools. Mark the turn handled
		// before that call and leave both slices nil.
		semanticHandled = true
		tools = nil
		baseTools = nil
		log.Printf("[file-companion] turn without tools user=%q", opts.UserID)
	} else if loopContextTurnAnswerOnly(ctx) {
		semanticHandled = true
		tools = nil
		baseTools = nil
		requestID := ""
		if ctx != nil {
			requestID = ctx.Runtime.RequestID
		}
		log.Printf("[semantic-routing] social turn while desktop task open; answer without tools request_id=%q user=%q", requestID, opts.UserID)
	} else if loopContextHasClassificationProtocolFailure(ctx) {
		semanticHandled = true
		log.Printf("[semantic-routing] classifier structured-output protocol violation request_id=%q user=%q", ctx.Runtime.RequestID, opts.UserID)
		hostReject = semanticHostRejectResponseForClassifierProtocolFailure()
	} else if semanticTools, surface, handled, semanticErr := h.semanticCallSurfaceForSharedTurnWithContextAndAttachments(ctx, opts.UserID, opts.UserText, opts.Platform, attachments); handled {
		// A handled capability family is owned by the semantic planner for the
		// complete lifetime of this turn.  In particular, a catalog/planner/
		// materialization failure is not a routing miss: falling through here
		// would re-expose legacy tools precisely when the governed surface is
		// incomplete.
		semanticHandled = true
		if errors.Is(semanticErr, errSemanticSessionCeilingSpent) {
			if ctx != nil {
				ctx.semanticSessionCeilingSpent = true
			}
			tools = nil
			baseTools = nil
		} else if semanticPlanErrorBlocksSession(semanticErr) {
			log.Printf("[semantic-routing] managed plan rejected user=%q reason=%v", opts.UserID, semanticErr)
			hostReject = semanticHostRejectResponseForManagedSurfaceFailure(semanticErr)
		} else if surface == nil || len(semanticTools) == 0 {
			log.Printf("[semantic-routing] managed surface unavailable user=%q reason=empty surface", opts.UserID)
			hostReject = semanticHostRejectResponseForManagedSurfaceFailure(nil)
		} else {
			lightPrompt := ctx != nil && ctx.Runtime.Execution.PromptIsLight()
			var keepCarried func(string) bool
			if lightPrompt && lookupContinuationProfile(ctx.Runtime.Execution) {
				carried := h.parentExecutionTools(opts.UserID)
				profile := ctx.Runtime.Execution
				keepCarried = func(name string) bool {
					return lookupContinuationCarriedGrant(profile, carried, name)
				}
			}
			tools = closedManagedSemanticDefinitionsKeeping(semanticTools, surface, lightPrompt, keepCarried)
			if len(tools) == 0 {
				log.Printf("[semantic-routing] managed surface unavailable user=%q reason=closed surface empty", opts.UserID)
				hostReject = semanticHostRejectResponseForManagedSurfaceFailure(nil)
			} else {
				semanticSurface = surface
			}
		}
	}
	if semanticHandled && !loopContextTurnAnswerOnly(ctx) {
		// prepareAgentLoopTools does not run on this path, so it cannot update
		// the legacy carry. A light managed turn must drop it; a full one
		// replaces it with the tools this turn actually rendered. A short
		// continuation, including a light project-task lookup, keeps it.
		// A greeting leaves the carry untouched.
		profile := ExecutionProfile{}
		if ctx != nil {
			profile = ctx.Runtime.Execution
		}
		h.recordSemanticExecutionSurfacePlan(opts.UserID, profile, tools, semanticResidueCandidateNeeds(ctx))
	}
	if !semanticHandled {
		markClassifierTimeoutLookup(ctx)
		applySemanticChatProjection(ctx)
		applySemanticRoutingMissFallback(ctx)
		// HostKeep continuation pins look for truncated=true on auto_extract
		// markers. Those markers are produced by expansion, which otherwise
		// happens only when building user content after this leftover route.
		// Rank leftover tools on the raw picker prompt; HostKeep reads the
		// expanded extract from LoopContext so BM25 does not score the body.
		if agent.CurrentLocalFilePathPromptIndex(userText) >= 0 {
			originalText := userText
			userText = agent.ExpandUserSelectedFilePathsWithContext(userText, cfg.EffectiveContextTokens())
			if ctx != nil {
				ctx.autoExtractExpandedText = userText
				toolRoutingText = computerUseRoutingText(originalText, attachments)
			} else {
				toolRoutingText = computerUseRoutingText(userText, attachments)
			}
		}
		toolSet = h.prepareAgentLoopTools(opts.UserID, toolRoutingText, ctx, phase)
		tools = toolSet.Tools
		baseTools = toolSet.BaseTools
	}
	if !semanticHandled {
		if diagnostic := h.semanticRouteDiagnosticForTurnWithContext(ctx, opts.UserID, toolRoutingText, opts.Platform, attachments); diagnostic.Handled {
			log.Printf("[semantic-routing] shadow plan=%q user=%q outcome=%s", diagnostic.PlanID, opts.UserID, diagnostic.Reason)
		}
	}
	if !loopContextTurnAnswerOnly(ctx) && !petCompanionToolsDisabled(opts.UserID) && !fileCompanionTurnWithoutTools(opts.UserID, opts.Platform) {
		if attached := h.attachVisionFallthroughExecutionTools(ctx, tools, hostReject, opts.UserID, userText, opts.History); len(attached) > 0 && len(tools) == 0 {
			tools = attached
			baseTools = attached
		}
	}
	toolsTokenBudget := toolSet.ToolsTokenBudget
	if semanticSurface != nil || (loopContextIsVisionFallthrough(ctx) && len(tools) > 0) {
		toolsTokenBudget = estimateToolsTokens(tools)
	}
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
	if semanticSurface != nil {
		systemPrompt = ensureSemanticGrantPromptFence(systemPrompt)
	}
	if ctx != nil && ctx.semanticSessionCeilingSpent {
		systemPrompt += "\n\n" + sessionCeilingTurnPrompt()
	}

	loopID := ""
	if ctx != nil {
		loopID = ctx.ID
	}
	var turnSemantic *intent.ClassificationResult
	planClosed := false
	if ctx != nil {
		turnSemantic = ctx.Runtime.SemanticIntent
		planClosed = ctx.semanticPriorPlanClosed
	}
	conversationStart := h.buildAgentLoopConversationStart(loopID, opts.UserID, userText, systemPrompt, opts.Platform, attachments, cfg, opts.History, opts.PriorReplanCount, recorderBundle.Recorder, tools, opts.SendProgress, allowLocalAttachmentStaging, turnSemantic, planClosed)
	if telemetry != nil {
		telemetry.PreLLMConversationElapsed = conversationStart.Elapsed
	}

	emitFinalToolSurfaceDiagnostics(tools, semanticSurface, 0)

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
		BaseTools:                     baseTools,
		Tools:                         tools,
		ClientToolNames:               toolSet.ClientToolNames,
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
		TaskAnchor:                    conversationStart.TaskAnchor,
		ConversationStartedAt:         conversationStart.StartedAt,
		EffectiveMax:                  limits.EffectiveMax,
		ChatFinalizeGrace:             limits.ChatFinalizeGrace,
		SemanticSurface:               semanticSurface,
		HostReject:                    hostReject,
		Cleanup: func() {
			for i := len(cleanupFns) - 1; i >= 0; i-- {
				if cleanupFns[i] != nil {
					cleanupFns[i]()
				}
			}
		},
	}
}
