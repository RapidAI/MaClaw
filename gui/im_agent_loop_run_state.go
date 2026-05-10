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
}

func newAgentLoopRunState(cfg corelib.MaclawLLMConfig) *agentLoopRunState {
	return &agentLoopRunState{
		EffectiveTokenLimit: cfg.EffectiveContextTokens(),
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
