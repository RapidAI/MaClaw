package main

import (
	"log"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

type agentLoopConfigStart struct {
	Config        corelib.MaclawLLMConfig
	TrialState    *trialReflectState
	MaxIterations int
	Elapsed       time.Duration
}

func (h *IMMessageHandler) prepareAgentLoopConfig(ctx *LoopContext) agentLoopConfigStart {
	startedAt := time.Now()
	if err := h.ensureOAuthToken(); err != nil {
		log.Printf("[LLM] OAuth token refresh failed: %v", err)
	}
	cfg := h.getMaclawLLMConfig()
	if cfg.IsResponsesAPI() {
		keyPrefix := cfg.Key
		if len(keyPrefix) > 20 {
			keyPrefix = keyPrefix[:20] + "..."
		}
		log.Printf("[LLM] WARNING Responses API config: wire_api=%s key_prefix=%q key_len=%d url=%s", cfg.WireAPI, keyPrefix, len(cfg.Key), cfg.URL)
	}
	trialReflectEnabled := false
	if appCfg, err := h.loadConfig(); err == nil {
		trialReflectEnabled = normalizeUIModeKind(appCfg.UIMode).IsProExplicit() && appCfg.TrialReflectEnabled
	}
	maxIter := h.getMaclawAgentMaxIterations()
	h.loopMaxOverride = 0
	if ctx != nil && ctx.MaxIterations() <= 0 {
		ctx.SetMaxIterations(maxIter)
	}
	return agentLoopConfigStart{
		Config:        cfg,
		TrialState:    newTrialReflectState(trialReflectEnabled),
		MaxIterations: maxIter,
		Elapsed:       time.Since(startedAt),
	}
}
