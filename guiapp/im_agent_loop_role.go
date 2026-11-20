package guiapp

import (
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// agentLoopLLMRole picks the frozen model for this loop.
// Computer use is vision and desktop work, including inside a chat task.
// A desktop chat task otherwise stays on the background role even though
// its Kind remains chat: that Kind owns iteration limits and does not pause
// on ContinueC. Everything else is the foreground planning turn.
// The caller string is intentionally not consulted: the scheduler lane and
// this role are different decisions.
func agentLoopLLMRole(h *IMMessageHandler, ctx *LoopContext) string {
	if agentLoopUsesComputerUse(h, ctx) {
		return maclawLLMRoleComputerUse
	}
	if ctx != nil && (ctx.Kind == LoopKindBackground || strings.TrimSpace(ctx.ChatTaskID) != "") {
		return maclawLLMRoleBackground
	}
	return maclawLLMRoleForeground
}

func agentLoopUsesComputerUse(h *IMMessageHandler, ctx *LoopContext) bool {
	if ctx == nil || !loopKeepsDrivingTools(ctx) {
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
	// Resolve the role first so a pinned MCP model cannot skip the computer-use
	// gate. Computer use keeps its vision model. A background loop keeps the
	// background model. The pin still covers the user-visible turn, including a
	// chat task whose model role is background.
	role := agentLoopLLMRole(h, ctx)
	if mcpPinnedModelAllowed(role, ctx) && ctx != nil && h != nil && h.app != nil {
		if cfg, ok := h.app.mcpPinnedLLMConfig(ctx.UserID); ok && strings.TrimSpace(cfg.Model) != "" {
			cfg.Profile = role
			return cfg
		}
	}
	// A task pin replaces the role's model for this task only. It does not
	// rewrite 模型分配. An empty pin keeps the role, including official auto.
	if ctx != nil && h != nil && h.app != nil {
		if cfg, ok := h.app.taskBoundLLMConfig(projectPathFromSessionOwnerID(ctx.UserID), role); ok {
			return cfg
		}
	}
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

// mcpPinnedModelAllowed reports whether this loop should use the model the
// MCP client chose for the task. Computer use needs its own vision model, and
// a background loop is housekeeping rather than the task the person is watching.
func mcpPinnedModelAllowed(role string, ctx *LoopContext) bool {
	if role == maclawLLMRoleComputerUse {
		return false
	}
	if ctx != nil && ctx.Kind == LoopKindBackground {
		return false
	}
	return true
}

func maclawLLMRoleSkipsLocalRoute(profile string) bool {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case maclawLLMRoleBackground, maclawLLMRoleComputerUse:
		return true
	default:
		return false
	}
}
