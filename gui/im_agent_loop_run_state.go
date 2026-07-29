package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

type agentLoopRunState struct {
	ConsecutiveWriteFileErrors int
	DirectModeToolsFiltered    bool
	TotalToolCallsInLoop       int
	CodingIterCount            int
	LastCompressionSummary     string
	EffectiveTokenLimit        int
	LengthContinuationBuffer   strings.Builder
	VoiceData                  string
	VoiceFileName              string
	VoiceMimeType              string

	// ActiveConfig is the LLM config used for the current (and subsequent)
	// iterations. It may start on a cheap route and escalate to reasoning
	// after tools appear.
	ActiveConfig        corelib.MaclawLLMConfig
	RouteTask           string
	RouteSource         string
	RouteModel          string
	RouteProvider       string
	RouteReason         string
	RouteBaseline       string
	RouteEscalated      bool
	RouteCostTier       string
	RouteCostMode       string
	RouteCostApplied    bool
	RouteThinkingPolicy string
	// Telemetry is optional; set by the dispatcher so escalations stay observable.
	Telemetry *agentLoopTelemetry
}

func newAgentLoopRunState(cfg corelib.MaclawLLMConfig) *agentLoopRunState {
	return &agentLoopRunState{
		EffectiveTokenLimit: cfg.EffectiveContextTokens(),
		ActiveConfig:        cfg,
		RouteTask:           "default",
		RouteSource:         "primary",
		RouteModel:          cfg.Model,
		RouteProvider:       cfg.ProviderName,
		RouteReason:         "primary config",
		RouteBaseline:       cfg.Model,
	}
}

func (s *agentLoopRunState) applyRouteDecision(d modelRouteDecision, cfg corelib.MaclawLLMConfig) {
	if s == nil {
		return
	}
	s.ActiveConfig = cfg
	s.EffectiveTokenLimit = cfg.EffectiveContextTokens()
	s.RouteTask = d.Task
	s.RouteSource = d.Source
	s.RouteModel = d.Model
	s.RouteProvider = d.Provider
	s.RouteReason = d.Reason
	if d.Baseline != "" {
		s.RouteBaseline = d.Baseline
	}
	s.RouteEscalated = d.Escalated
	s.RouteCostTier = d.CostTier
	s.RouteCostMode = d.CostRouteMode
	s.RouteCostApplied = d.CostRouteApplied
	s.RouteThinkingPolicy = d.ThinkingPolicy
	if s.Telemetry != nil {
		s.Telemetry.Route = d
	}
}

func (s *agentLoopRunState) routeDecision() modelRouteDecision {
	if s == nil {
		return modelRouteDecision{}
	}
	return modelRouteDecision{
		Task:             s.RouteTask,
		Source:           s.RouteSource,
		Model:            s.RouteModel,
		Provider:         s.RouteProvider,
		Reason:           s.RouteReason,
		Baseline:         s.RouteBaseline,
		Escalated:        s.RouteEscalated,
		CostTier:         s.RouteCostTier,
		CostRouteMode:    s.RouteCostMode,
		CostRouteApplied: s.RouteCostApplied,
		ThinkingPolicy:   s.RouteThinkingPolicy,
	}
}

func (s *agentLoopRunState) ApplyRoundPrep(result agentLoopRoundPrepResult) {
	if s == nil {
		return
	}
	s.EffectiveTokenLimit = result.EffectiveTokenLimit
	s.DirectModeToolsFiltered = result.DirectModeToolsFiltered
}

func (s *agentLoopRunState) ApplyToolPath(result agentLoopToolPathResult) {
	if s == nil {
		return
	}
	s.CodingIterCount = result.CodingIterCount
	s.TotalToolCallsInLoop = result.TotalToolCallsInLoop
	s.VoiceData = result.VoiceData
	s.VoiceFileName = result.VoiceFileName
	s.VoiceMimeType = result.VoiceMimeType
}
