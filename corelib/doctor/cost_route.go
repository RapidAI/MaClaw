package doctor

import (
	"fmt"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// CostRouteCheck reports MACLAW_COST_ROUTE mode and durable tier counters.
func CostRouteCheck() Check {
	mode := llm.ResolveCostRouteMode()
	envRaw := strings.TrimSpace(os.Getenv(llm.CostRouteEnvKey))
	st := llm.LoadCostRouteStats()
	detail := map[string]any{
		"mode":       string(mode),
		"env":        envRaw,
		"decisions":  st.Decisions,
		"applied":    st.Applied,
		"shadow":     st.Shadow,
		"last_tier":  st.LastTier,
		"last_mode":  st.LastMode,
		"last_think": st.LastThink,
		"stats_path": st.Path,
	}
	if len(st.ByTier) > 0 {
		detail["by_tier"] = st.ByTier
	}
	if len(st.ByThinking) > 0 {
		detail["by_thinking"] = st.ByThinking
	}

	fleet := llm.LoadCostDailyFleet()
	if fleet.Calls > 0 || fleet.CostUSD > 0 {
		detail["fleet_cost_usd"] = fleet.CostUSD
		detail["fleet_calls"] = fleet.Calls
		detail["fleet_instances"] = fleet.Instances
		if len(fleet.ByModel) > 0 {
			detail["fleet_by_model"] = fleet.ByModel
		}
	}

	hint := "MACLAW_COST_ROUTE=off|shadow|on; maclaw-cli cost; budget: daily_llm_budget_usd"
	if st.Decisions == 0 && mode == llm.CostRouteOff {
		return Check{
			ID:      "llm.cost_route",
			Status:  StatusInfo,
			Message: "cost-route off (no tier decisions yet); set MACLAW_COST_ROUTE=shadow|on to observe/apply C0–C3",
			Hint:    hint,
			Detail:  detail,
		}
	}
	msg := fmt.Sprintf("cost-route mode=%s", mode)
	if st.Decisions > 0 {
		msg += fmt.Sprintf("; decisions=%d applied=%d shadow=%d", st.Decisions, st.Applied, st.Shadow)
		if st.LastTier != "" {
			msg += "; last=" + st.LastTier
			if st.LastMode != "" {
				msg += "/" + st.LastMode
			}
		}
	}
	if fleet.CostUSD > 0 {
		msg += fmt.Sprintf("; fleet today=$%.4f", fleet.CostUSD)
	}
	return Check{
		ID:      "llm.cost_route",
		Status:  StatusInfo,
		Message: msg,
		Hint:    hint,
		Detail:  detail,
	}
}
