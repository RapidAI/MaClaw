package guiapp

import (
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// agentLoopLLMRole picks the frozen model for this loop.
// Background wins over computer use. A desktop chat task is a background
// worker even though its Kind stays chat: that Kind owns iteration limits
// and does not pause on ContinueC. Computer use is the sticky or freshly
// gated desktop loop. Everything else is the foreground planning turn.
// The caller string is intentionally not consulted: the scheduler lane and
// this role are different decisions.
func agentLoopLLMRole(h *IMMessageHandler, ctx *LoopContext) string {
	if ctx != nil && (ctx.Kind == LoopKindBackground || strings.TrimSpace(ctx.ChatTaskID) != "") {
		return maclawLLMRoleBackground
	}
	if agentLoopUsesComputerUse(h, ctx) {
		return maclawLLMRoleComputerUse
	}
	return maclawLLMRoleForeground
}

func agentLoopUsesComputerUse(h *IMMessageHandler, ctx *LoopContext) bool {
	if ctx == nil {
		return false
	}
	if ctx.ComputerUseGateSettled {
		return ctx.ComputerUseActive
	}
	text := strings.TrimSpace(ctx.ComputerUseRoutingText)
	if text == "" || h == nil {
		return false
	}
	active, _ := recordComputerUseGate(h, ctx, text)
	return active
}

func (h *IMMessageHandler) resolveAgentLoopLLMConfig(ctx *LoopContext) corelib.MaclawLLMConfig {
	role := agentLoopLLMRole(h, ctx)
	if h != nil && h.app != nil {
		cfg, err := h.app.ResolveMaclawLLMRole(role)
		if err == nil && strings.TrimSpace(cfg.Model) != "" {
			return cfg
		}
		if err != nil {
			log.Printf("[LLM] role %s resolve failed, using assistant: %v", role, err)
		}
	}
	cfg := h.getMaclawLLMConfig()
	if strings.TrimSpace(cfg.Profile) == "" {
		cfg.Profile = role
	}
	return cfg
}

func maclawLLMRoleSkipsLocalRoute(profile string) bool {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case maclawLLMRoleBackground, maclawLLMRoleComputerUse:
		return true
	default:
		return false
	}
}
