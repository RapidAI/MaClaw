package llm

// Cost-route (OpenSquilla-inspired): rule-based C0–C3 tier recommendation.
//
// Env: MACLAW_COST_ROUTE=off|shadow|on
//   off     — do not surface tier on Turn chip
//   shadow  — record + display tier; keep existing DecideTurn model selection
//   on      — Phase 2: map tier → aux / ModelRoutes / primary / reasoning

import (
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// CostRouteEnvKey controls observe/apply mode for cost tiers.
const CostRouteEnvKey = "MACLAW_COST_ROUTE"

// CostTier is an OpenSquilla-style model cost tier (cheapest → strongest).
type CostTier string

const (
	// CostTierC0 — trivial chat / intent (cheapest).
	CostTierC0 CostTier = "c0"
	// CostTierC1 — light work (summary, short structured).
	CostTierC1 CostTier = "c1"
	// CostTierC2 — default agent / moderate coding.
	CostTierC2 CostTier = "c2"
	// CostTierC3 — hard reasoning, vision, tool-heavy recovery.
	CostTierC3 CostTier = "c3"
)

// CostRouteMode is off | shadow | on.
type CostRouteMode string

const (
	CostRouteOff    CostRouteMode = "off"
	CostRouteShadow CostRouteMode = "shadow"
	CostRouteOn     CostRouteMode = "on"
)

// ResolveCostRouteMode reads MACLAW_COST_ROUTE (default off).
func ResolveCostRouteMode() CostRouteMode {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(CostRouteEnvKey)))
	switch raw {
	case "1", "true", "yes", "on", "apply":
		return CostRouteOn
	case "shadow", "observe", "dry-run", "dryrun":
		return CostRouteShadow
	case "0", "false", "no", "off", "":
		return CostRouteOff
	default:
		return CostRouteOff
	}
}

// CostRouteSurfaces reports whether tier should appear on Turn chip / doctor.
func CostRouteSurfaces(mode CostRouteMode) bool {
	return mode == CostRouteShadow || mode == CostRouteOn
}

// RecommendCostTier maps ClassifyTurn task + hints to C0–C3.
// Pure function — no I/O, no model calls.
func RecommendCostTier(task TaskType, hints ClassifyHints) CostTier {
	tier := costTierFromTask(task)
	if hints.ForceReasoning {
		return CostTierC3
	}
	if hints.HasAttachments {
		// Multimodal / files: prefer strong tier.
		return maxCostTier(tier, CostTierC3)
	}
	if hints.ToolHeavy {
		return maxCostTier(tier, CostTierC2)
	}
	return tier
}

func costTierFromTask(task TaskType) CostTier {
	switch task {
	case TaskFast, TaskIntent:
		return CostTierC0
	case TaskSummary:
		return CostTierC1
	case TaskVision:
		return CostTierC3
	case TaskReasoning:
		return CostTierC3
	case TaskDefault:
		return CostTierC2
	default:
		return CostTierC2
	}
}

func maxCostTier(a, b CostTier) CostTier {
	if costTierRank(a) >= costTierRank(b) {
		return a
	}
	return b
}

func costTierRank(t CostTier) int {
	switch t {
	case CostTierC0:
		return 0
	case CostTierC1:
		return 1
	case CostTierC2:
		return 2
	case CostTierC3:
		return 3
	default:
		return 2
	}
}

// CostRouteDecision is the cost-tier observation (and optional apply result).
type CostRouteDecision struct {
	Tier CostTier      `json:"tier"`
	Mode CostRouteMode `json:"mode"`
	// Thinking is the recommended extended-thinking posture (Phase 3).
	Thinking ThinkingPolicy `json:"thinking,omitempty"`
	// Applied is true when mode=on and model/thinking selection was applied.
	Applied bool   `json:"applied"`
	Reason  string `json:"reason,omitempty"`
}

// DecideCostRoute recommends a tier + thinking policy under current mode.
// Does not alter model selection by itself — call ApplyCostTierConfig when mode=on.
func DecideCostRoute(task TaskType, hints ClassifyHints, classifyReason string) CostRouteDecision {
	mode := ResolveCostRouteMode()
	tier := RecommendCostTier(task, hints)
	think := RecommendThinkingPolicy(tier)
	d := CostRouteDecision{
		Tier:     tier,
		Mode:     mode,
		Thinking: think,
		Applied:  false,
		Reason:   "cost-route " + string(tier) + " think=" + string(think) + " from task=" + string(task),
	}
	if r := strings.TrimSpace(classifyReason); r != "" {
		d.Reason = d.Reason + "; " + r
	}
	RecordCostRouteDecision(d)
	return d
}

// ThinkingPolicy is the Phase-3 extended-thinking posture for a cost tier.
type ThinkingPolicy string

const (
	// ThinkingOff — disable extended thinking / use minimal reasoning effort.
	ThinkingOff ThinkingPolicy = "off"
	// ThinkingLow — allow light reasoning (low effort / small budget).
	ThinkingLow ThinkingPolicy = "low"
	// ThinkingHigh — full reasoning / high effort.
	ThinkingHigh ThinkingPolicy = "high"
)

// RecommendThinkingPolicy maps cost tier → thinking policy.
// c0/c1 off, c2 low, c3 high.
func RecommendThinkingPolicy(tier CostTier) ThinkingPolicy {
	switch tier {
	case CostTierC0, CostTierC1:
		return ThinkingOff
	case CostTierC2:
		return ThinkingLow
	case CostTierC3:
		return ThinkingHigh
	default:
		return ThinkingLow
	}
}

// ApplyThinkingPolicy mutates cfg for provider request controls.
// Safe no-op fields when empty policy.
func ApplyThinkingPolicy(cfg corelib.MaclawLLMConfig, policy ThinkingPolicy) corelib.MaclawLLMConfig {
	// The global setting is an explicit user choice. Cost routing is an
	// optimization heuristic and must never silently override it.
	if !corelib.IsAutoThinkingMode(cfg.ThinkingMode) {
		return cfg
	}
	switch policy {
	case ThinkingOff:
		if corelib.IsAlwaysOnThinkingModel(cfg) {
			return cfg
		}
		cfg.ThinkingMode = "disabled"
		cfg.ReasoningEffort = "none"
	case ThinkingLow:
		cfg.ThinkingMode = "enabled"
		cfg.ReasoningEffort = "low"
	case ThinkingHigh:
		cfg.ThinkingMode = "enabled"
		cfg.ReasoningEffort = "high"
	}
	return cfg
}

// ApplyCostTierConfig maps C0–C3 → LLM config when mode=on.
//
//	c0 → ModelRoutes[fast|intent] > aux > primary
//	c1 → ModelRoutes[summary|fast] > aux > primary
//	c2 → ModelRoutes[default] > primary
//	c3 → ModelRoutes[reasoning|vision] > primary
//
// Returns applied=false when mode is not on (caller should keep baseline cfg).
func ApplyCostTierConfig(
	router *ModelRouter,
	primary corelib.MaclawLLMConfig,
	aux corelib.AuxiliaryLLMConfig,
	tier CostTier,
	mode CostRouteMode,
) (cfg corelib.MaclawLLMConfig, applied bool, source, detail string) {
	if mode != CostRouteOn {
		return primary, false, "", ""
	}
	switch tier {
	case CostTierC0:
		cfg, source, detail = pickCostTier(router, primary, aux, []TaskType{TaskFast, TaskIntent}, true)
	case CostTierC1:
		cfg, source, detail = pickCostTier(router, primary, aux, []TaskType{TaskSummary, TaskFast, TaskIntent}, true)
	case CostTierC2:
		cfg, source, detail = pickCostTier(router, primary, aux, []TaskType{TaskDefault}, false)
	case CostTierC3:
		cfg, source, detail = pickCostTier(router, primary, aux, []TaskType{TaskReasoning, TaskVision, TaskDefault}, false)
	default:
		cfg, source, detail = primary, "primary", "cost-route unknown tier → primary"
	}
	RecordCostRouteApplied(true)
	return cfg, true, source, "cost-route apply " + string(tier) + " → " + detail
}

// pickCostTier tries explicit routes in order, then optional aux, then primary.
func pickCostTier(
	router *ModelRouter,
	primary corelib.MaclawLLMConfig,
	aux corelib.AuxiliaryLLMConfig,
	tasks []TaskType,
	allowAux bool,
) (cfg corelib.MaclawLLMConfig, source, detail string) {
	for _, task := range tasks {
		if router != nil && router.HasRoute(task) {
			return router.Route(task, primary), "route", "route:" + string(task)
		}
	}
	if allowAux && aux.IsConfigured() {
		return applyAuxiliaryLLM(primary, aux), "aux", "aux"
	}
	return primary, "primary", "primary"
}
