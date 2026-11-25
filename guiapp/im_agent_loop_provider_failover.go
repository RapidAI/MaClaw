package guiapp

import (
	"context"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// FailoverLLMProvider keeps a worker turn alive when its assigned endpoint is
// unreachable. Model allocation deliberately lets the background role differ
// from the interactive assistant (often Hub auto), but an outage of that one
// service must not park a user-visible task behind repeated retries while the
// foreground model is healthy. The turn falls back to the foreground selection;
// the saved role assignment is untouched, so the next turn tries the assigned
// worker model again.
//
// A desktop chat task that drives the screen runs the computer-use role, not
// the background role (chat_task.go sets ComputerHelper/ComputerUseActive).
// Both roles are worker roles and both must be able to leave a dead endpoint;
// computer use only accepts a fallback that can still see images.
func (c *sharedAgentLoopCallbacks) FailoverLLMProvider(_ context.Context, current corelib.MaclawLLMConfig, err error) (corelib.MaclawLLMConfig, bool) {
	if c == nil || c.handler == nil {
		return current, false
	}
	return c.handler.workerRoleFailoverLLMConfig(current, err)
}

// agentLoopRoleMayFailOver reports the loop roles whose endpoint may be
// replaced for one turn when that endpoint is unreachable. They are the worker
// roles: background tasks and the desktop helper (computer use). The foreground
// role keeps its own assignment, which is already the fallback.
func agentLoopRoleMayFailOver(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case maclawLLMRoleBackground, maclawLLMRoleComputerUse:
		return true
	default:
		return false
	}
}

// workerRoleFailoverLLMConfig is shared by the shared loop and the legacy
// retry loop so both paths degrade the same way.
func (h *IMMessageHandler) workerRoleFailoverLLMConfig(current corelib.MaclawLLMConfig, err error) (corelib.MaclawLLMConfig, bool) {
	if !llm.ProviderFailoverEligible(err) {
		return current, false
	}
	fallback, ok := h.foregroundFallbackForWorkerRole(current)
	if !ok {
		return current, false
	}
	log.Printf("[LLM] %s endpoint %q unavailable (%v), falling back to foreground model %q for this turn", current.Profile, current.ProviderName, err, fallback.Model)
	return fallback, true
}

// applyWorkerRoleStartFallback avoids paying for a known-dead worker endpoint
// again when the endpoint gate still remembers a recent network failure. The
// core loop can only fail over after the first attempt returns; a goal
// continuation often starts seconds later, so selecting the healthy foreground
// model up front removes that repeated 20s wait. A pinned MCP caller model is
// never replaced.
func (h *IMMessageHandler) applyWorkerRoleStartFallback(cfg corelib.MaclawLLMConfig, role string) corelib.MaclawLLMConfig {
	if h == nil || h.app == nil || !agentLoopRoleMayFailOver(role) || cfg.RouteSource == mcpCallerModelRoute {
		return cfg
	}
	if !h.app.llmEndpointRecentlyFailed(cfg, llmEndpointCategoryMainStream) {
		return cfg
	}
	fallback, ok := h.foregroundFallbackForWorkerRole(cfg)
	if !ok {
		return cfg
	}
	log.Printf("[LLM] %s endpoint %q recently failed, starting on foreground model %q for this turn", role, cfg.ProviderName, fallback.Model)
	return fallback
}

// foregroundFallbackForWorkerRole returns the interactive model when a worker
// role points somewhere else. The saved allocation is never rewritten;
// callers apply the fallback to this turn only.
func (h *IMMessageHandler) foregroundFallbackForWorkerRole(current corelib.MaclawLLMConfig) (corelib.MaclawLLMConfig, bool) {
	if h == nil || h.app == nil {
		return current, false
	}
	if !agentLoopRoleMayFailOver(current.Profile) {
		return current, false
	}
	// The MCP caller picked this model for its tool pair; substituting another
	// model mid-session breaks that pairing even when the endpoint is down.
	if current.RouteSource == mcpCallerModelRoute {
		return current, false
	}
	fallback, resolveErr := h.app.ResolveMaclawLLMRole(maclawLLMRoleForeground)
	if resolveErr != nil || strings.TrimSpace(fallback.Model) == "" {
		return current, false
	}
	if !agent.LLMSelectionChanged(current, fallback) {
		return current, false
	}
	// A different model on the same unreachable host is not a fallback.
	if strings.EqualFold(strings.TrimRight(strings.TrimSpace(current.URL), "/"), strings.TrimRight(strings.TrimSpace(fallback.URL), "/")) {
		return current, false
	}
	// Do not trade one dead endpoint for another one the gate still bans.
	if h.app.llmEndpointRecentlyFailed(fallback, llmEndpointCategoryMainStream) {
		return current, false
	}
	// Computer use sends screenshots. Do not downgrade a vision-capable role to
	// a text-only model; a role already pinned to text stays eligible.
	if strings.EqualFold(strings.TrimSpace(current.Profile), maclawLLMRoleComputerUse) &&
		current.SupportsVision && !fallback.SupportsVision {
		return current, false
	}
	fallback.Profile = current.Profile
	fallback.RouteSource = "role-failover"
	return fallback, true
}
