package agent

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// TurnUsage records token and cost accounting for one agent loop / turn.
// Fields are zero-valued when the host does not track usage yet.
//
// This is the P0 contract shared by GUI, TUI, CLI, and future TurnRunner
// surfaces — hosts should fill what they know and leave the rest zero.
type TurnUsage struct {
	// Model is the effective model id used for the turn (after routing).
	Model string `json:"model,omitempty"`
	// Provider is an optional human-readable provider label.
	Provider string `json:"provider,omitempty"`

	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	// CachedTokens is provider-reported cached/prompt-cache hit input tokens.
	CachedTokens int `json:"cached_tokens,omitempty"`
	// CacheWriteTokens is provider-reported cache write tokens when available.
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`

	// EstCostRMB is an optional local estimate in RMB (when price table known).
	EstCostRMB float64 `json:"est_cost_rmb,omitempty"`
	// EstCostUSD is an optional local estimate in USD.
	EstCostUSD float64 `json:"est_cost_usd,omitempty"`

	// Requests is the number of LLM HTTP rounds in this turn/loop.
	Requests int `json:"requests,omitempty"`
}

// Add merges other into u (token counters sum; model/provider keep first non-empty).
func (u *TurnUsage) Add(other TurnUsage) {
	if u == nil {
		return
	}
	if u.Model == "" {
		u.Model = other.Model
	}
	if u.Provider == "" {
		u.Provider = other.Provider
	}
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.CachedTokens += other.CachedTokens
	u.CacheWriteTokens += other.CacheWriteTokens
	u.EstCostRMB += other.EstCostRMB
	u.EstCostUSD += other.EstCostUSD
	u.Requests += other.Requests
}

// TotalTokens returns input + output (cached is a subset of input when reported).
func (u TurnUsage) TotalTokens() int {
	return u.InputTokens + u.OutputTokens
}

// Empty reports whether no meaningful usage was recorded.
func (u TurnUsage) Empty() bool {
	return u.InputTokens == 0 && u.OutputTokens == 0 && u.CachedTokens == 0 &&
		u.CacheWriteTokens == 0 && u.Requests == 0 && u.EstCostRMB == 0 && u.EstCostUSD == 0
}

// Summary returns a compact one-line cost/token string for doctor/TUI/CLI.
// Empty usage returns "".
// Example: "in=1000 out=200 total=1200 · req=2 · ~¥0.0123 · model m1"
func (u TurnUsage) Summary() string {
	if u.Empty() {
		return ""
	}
	parts := make([]string, 0, 6)
	parts = append(parts, fmt.Sprintf("in=%d out=%d total=%d", u.InputTokens, u.OutputTokens, u.TotalTokens()))
	if u.CachedTokens > 0 {
		parts = append(parts, fmt.Sprintf("cache_read=%d", u.CachedTokens))
	}
	if u.CacheWriteTokens > 0 {
		parts = append(parts, fmt.Sprintf("cache_write=%d", u.CacheWriteTokens))
	}
	if u.Requests > 0 {
		parts = append(parts, fmt.Sprintf("req=%d", u.Requests))
	}
	if u.EstCostRMB > 0 {
		parts = append(parts, fmt.Sprintf("~¥%.4f", u.EstCostRMB))
	} else if u.EstCostUSD > 0 {
		parts = append(parts, fmt.Sprintf("~$%.4f", u.EstCostUSD))
	}
	if m := strings.TrimSpace(u.Model); m != "" {
		parts = append(parts, "model "+m)
	} else if p := strings.TrimSpace(u.Provider); p != "" {
		parts = append(parts, "provider "+p)
	}
	return strings.Join(parts, " · ")
}

// TurnMetaOptions configures the always-on chat Turn footer.
type TurnMetaOptions struct {
	Route         RouteDecision
	Usage         TurnUsage
	PromptProfile string
	// PromptSavedTokens is estimated system-prompt tokens avoided when light
	// profile was chosen (full-light dual-build delta; not LLM usage).
	PromptSavedTokens int
	// PromptUpgraded is true when the turn started light and recovered to full
	// mid-loop (tool-deny retry) or soft-intent upgrade is worth surfacing.
	PromptUpgraded bool
	// PromptABSample is true when light-eligible turn was forced full by
	// MACLAW_PROMPT_AB_PERCENT quality sampling.
	PromptABSample bool
	// PromptSoftFull is true when SoftFullAgentIntent upgraded light→full
	// before the turn (terse ops/shell cues).
	PromptSoftFull bool
}

// FormatTurnMeta builds a compact always-on chat footer for route + tokens + cost.
// Example: "fast · aux · glm-4-flash · in=1.2k out=340 · ~¥0.0123"
// Empty when neither route nor usage is known.
func FormatTurnMeta(route RouteDecision, usage TurnUsage) string {
	return FormatTurnMetaOpts(TurnMetaOptions{Route: route, Usage: usage})
}

// FormatTurnMetaWithPrompt is FormatTurnMeta plus optional prompt profile tag.
func FormatTurnMetaWithPrompt(route RouteDecision, usage TurnUsage, promptProfile string) string {
	return FormatTurnMetaOpts(TurnMetaOptions{
		Route:         route,
		Usage:         usage,
		PromptProfile: promptProfile,
	})
}

// FormatTurnMetaOpts builds the Turn chip with optional prompt thickness tags.
// Priority: upgraded (mid-loop) > ab (quality sample) > soft (preemptive ops cues) > light.
// Example: "fast · aux · m1 · in=1.2k out=340 · prompt=light(-3.8k)"
// Example: "reasoning · primary · m1 · in=2k out=400 · prompt=full(upgraded)"
// Example: "fast · aux · m1 · in=1k out=200 · prompt=full(ab)"
// Example: "fast · aux · m1 · prompt=full(soft)"  // SoftFullAgentIntent
func FormatTurnMetaOpts(opts TurnMetaOptions) string {
	route := opts.Route
	usage := opts.Usage
	parts := make([]string, 0, 10)
	if t := strings.TrimSpace(route.TaskType); t != "" {
		parts = append(parts, t)
	}
	if s := strings.TrimSpace(route.Source); s != "" {
		parts = append(parts, s)
	}
	model := strings.TrimSpace(route.Model)
	if model == "" {
		model = strings.TrimSpace(usage.Model)
	}
	if model != "" {
		parts = append(parts, model)
	}
	if usage.InputTokens > 0 || usage.OutputTokens > 0 {
		parts = append(parts, fmt.Sprintf("in=%s out=%s",
			formatCompactTokenCount(usage.InputTokens),
			formatCompactTokenCount(usage.OutputTokens)))
	}
	if usage.CachedTokens > 0 {
		parts = append(parts, "cache="+formatCompactTokenCount(usage.CachedTokens))
	}
	if usage.EstCostRMB > 0 {
		parts = append(parts, fmt.Sprintf("~¥%.4f", usage.EstCostRMB))
	} else if usage.EstCostUSD > 0 {
		parts = append(parts, fmt.Sprintf("~$%.4f", usage.EstCostUSD))
	}
	switch {
	case opts.PromptUpgraded:
		// Mid-loop light→full recovery takes precedence.
		parts = append(parts, "prompt=full(upgraded)")
	case opts.PromptABSample:
		parts = append(parts, "prompt=full(ab)")
	case opts.PromptSoftFull:
		parts = append(parts, "prompt=full(soft)")
	case NormalizePromptProfile(opts.PromptProfile).IsLight():
		if opts.PromptSavedTokens > 0 {
			parts = append(parts, fmt.Sprintf("prompt=light(-%s)", formatCompactTokenCount(opts.PromptSavedTokens)))
		} else {
			parts = append(parts, "prompt=light")
		}
	}
	if tier := strings.TrimSpace(route.CostTier); tier != "" {
		mode := strings.ToLower(strings.TrimSpace(route.CostRouteMode))
		switch mode {
		case "shadow", "observe", "dry-run", "dryrun":
			parts = append(parts, "tier="+tier+"(shadow)")
		case "on", "apply", "1", "true":
			if route.CostRouteApplied {
				parts = append(parts, "tier="+tier)
			} else {
				parts = append(parts, "tier="+tier+"(shadow)")
			}
		}
	}
	if think := strings.TrimSpace(route.ThinkingPolicy); think != "" {
		mode := strings.ToLower(strings.TrimSpace(route.CostRouteMode))
		switch mode {
		case "shadow", "observe", "dry-run", "dryrun":
			parts = append(parts, "think="+think+"(shadow)")
		case "on", "apply", "1", "true":
			if route.CostRouteApplied {
				parts = append(parts, "think="+think)
			} else {
				parts = append(parts, "think="+think+"(shadow)")
			}
		}
	}
	if route.Source == "escalate" || strings.Contains(strings.ToLower(route.Reason), "escalat") {
		// Avoid double-tagging when source already says escalate.
		if strings.ToLower(strings.TrimSpace(route.Source)) != "escalate" {
			parts = append(parts, "escalated")
		}
	}
	if p := strings.TrimSpace(route.MoAPreset); p != "" {
		// Prefer ok/total (design §9); never emit moa=name 0/0.
		switch {
		case route.MoAFanOut && route.MoAReferences > 0:
			parts = append(parts, fmt.Sprintf("moa=%s %d/%d", p, route.MoARefOK, route.MoAReferences))
		case route.MoAReferences > 0:
			// Sticky/session MoA on aggregator-only iteration after a prior fan-out.
			parts = append(parts, fmt.Sprintf("moa=%s %d/%d(agg)", p, route.MoARefOK, route.MoAReferences))
		case route.MoAFanouts > 0:
			// Legacy fallback when only wave count is known.
			parts = append(parts, fmt.Sprintf("moa=%s/%d", p, route.MoAFanouts))
		default:
			parts = append(parts, "moa="+p)
		}
	}
	return strings.Join(parts, " · ")
}

func formatCompactTokenCount(n int) string {
	if n < 0 {
		n = 0
	}
	if n < 1000 {
		return strconv.Itoa(n)
	}
	if n < 10_000 {
		// one decimal for 1.0k–9.9k
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%dk", n/1000)
}

// RouteDecision explains which model tier/path was chosen for a turn.
// Source values are conventional, not enforced: "primary", "aux", "route",
// "disabled", "override", "fallback".
type RouteDecision struct {
	// TaskType is the routing task key (intent/fast/reasoning/vision/summary/default).
	TaskType string `json:"task_type,omitempty"`
	// Model is the selected model id.
	Model string `json:"model,omitempty"`
	// Provider is an optional provider label.
	Provider string `json:"provider,omitempty"`
	// Source describes how the model was chosen.
	Source string `json:"source,omitempty"`
	// Reason is a short human-readable explanation for UI / diagnostics.
	Reason string `json:"reason,omitempty"`
	// Applied is false when routing was observe-only or disabled after decision.
	Applied bool `json:"applied,omitempty"`
	// CostTier is OpenSquilla-style c0–c3 recommendation (Phase 1 observe).
	CostTier string `json:"cost_tier,omitempty"`
	// CostRouteMode is off|shadow|on from MACLAW_COST_ROUTE.
	CostRouteMode string `json:"cost_route_mode,omitempty"`
	// CostRouteApplied is true only when Phase 2+ actually switches models / thinking.
	CostRouteApplied bool `json:"cost_route_applied,omitempty"`
	// ThinkingPolicy is off|low|high (cost-route Phase 3).
	ThinkingPolicy string `json:"thinking_policy,omitempty"`
	// MoAPreset is the active MoA preset name when multi-model council ran.
	MoAPreset string `json:"moa_preset,omitempty"`
	// MoAFanouts is how many reference fan-out waves completed this turn (0 if none).
	MoAFanouts int `json:"moa_fanouts,omitempty"`
	// MoAReferences is the number of advisors in the last fan-out (or preset size).
	MoAReferences int `json:"moa_references,omitempty"`
	// MoARefOK is how many advisors returned content on the last fan-out.
	MoARefOK int `json:"moa_ref_ok,omitempty"`
	// MoARefFailed is how many advisors failed on the last fan-out.
	MoARefFailed int `json:"moa_ref_failed,omitempty"`
	// MoAFanOut is true when this iteration ran reference models (false = aggregator-only under MoA).
	MoAFanOut bool `json:"moa_fanout,omitempty"`
	// MoASource is how MoA was armed: sticky | one_shot | auto | picker (optional).
	MoASource string `json:"moa_source,omitempty"`
}

// TurnUsageFromLLM maps a provider usage payload into TurnUsage.
// cfg supplies model/provider labels; prices use corelib defaults when unknown.
func TurnUsageFromLLM(cfg corelib.MaclawLLMConfig, u *llm.Usage) TurnUsage {
	if u == nil {
		return TurnUsage{}
	}
	in := u.PromptTokens
	if in == 0 {
		in = u.InputTokens
	}
	out := u.CompletionTokens
	if out == 0 {
		out = u.OutputTokens
	}
	_, _, totalCost := corelib.CalculateLLMCostRMB(
		int64(in),
		int64(out),
		corelib.DefaultLLMInputPricePerMTokensRMB,
		corelib.DefaultLLMOutputPricePerMTokensRMB,
	)
	return TurnUsage{
		Model:            cfg.Model,
		Provider:         cfg.ProviderName,
		InputTokens:      in,
		OutputTokens:     out,
		CachedTokens:     u.CachedInputTokens,
		CacheWriteTokens: u.CacheWriteTokens,
		EstCostRMB:       totalCost,
		Requests:         1,
	}
}

// PrimaryRouteDecision is the default route metadata when the loop uses the
// host-provided primary LLM config without a separate router decision.
func PrimaryRouteDecision(cfg corelib.MaclawLLMConfig) RouteDecision {
	return RouteDecision{
		TaskType: string(llm.TaskDefault),
		Model:    cfg.Model,
		Provider: cfg.ProviderName,
		Source:   "primary",
		Reason:   "loop uses host GetLLMConfig()",
		Applied:  true,
	}
}
