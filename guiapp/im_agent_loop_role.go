package guiapp

import (
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// agentLoopLLMRole picks the frozen model for this loop.
// Computer use is desktop work (a chat task's helper included). A desktop chat
// task otherwise stays on the background role even though its Kind remains
// chat: that Kind owns iteration limits and does not pause on ContinueC.
// Everything else is the foreground planning turn. All three roles resolve to
// whatever the current assistant is; the role only decides the call surface
// (screenshots, cost route) and the usage profile.
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
	// gate. Every role then resolves to the current assistant (a task pin or an
	// MCP caller pin may replace it for its own turn). A worker role running on
	// a dead endpoint starts on the foreground model instead.
	role := agentLoopLLMRole(h, ctx)
	cfg := h.resolveAgentLoopBaseLLMConfig(ctx, role)
	return h.applyWorkerRoleStartFallback(cfg, role)
}

func (h *IMMessageHandler) resolveAgentLoopBaseLLMConfig(ctx *LoopContext, role string) corelib.MaclawLLMConfig {
	if mcpPinnedModelAllowed(role, ctx) && ctx != nil && h != nil && h.app != nil {
		// The live run is the model send_command just wrote. The create-time
		// pin is only the fallback when that run has no complete pair.
		// RouteSource tells the later cost route to keep this pair.
		if cfg, ok := h.app.mcpLoopModelConfig(ctx.UserID); ok && strings.TrimSpace(cfg.Model) != "" {
			cfg.Profile = role
			cfg.RouteSource = mcpCallerModelRoute
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

// mcpCallerModelRoute marks a config the MCP client chose for this task.
// Desktop cost routing must not replace that pair with another model.
const mcpCallerModelRoute = "mcp-caller"

// mcpPinnedModelAllowed reports whether this loop should use the model the
// MCP client chose for the task. That pin belongs to the caller's own turn; a
// desktop helper and housekeeping work follow the assistant instead.
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
