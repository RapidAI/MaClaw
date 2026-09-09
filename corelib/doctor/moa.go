package doctor

import (
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm/moa"
)

// MoACheck reports multi-model council (MoA) readiness from config + env.
func MoACheck(cfg corelib.AppConfig) Check {
	m := corelib.NormalizeMoAConfig(cfg.MoA)
	detail := map[string]any{
		"config_enabled":  m.Enabled,
		"allow_auto":      m.AllowAuto,
		"default_preset":  m.DefaultPreset,
		"presets":         len(m.Presets),
		"env":             moa.EnvStatusLabel(),
		"env_allows":      moa.EnvAllows(),
		"effective":       moa.EffectiveEnabled(m.Enabled),
		"fanout_max_iter": m.EffectiveFanoutMaxIterations(),
	}
	hint := "LLM settings → multi-model; kill switch MACLAW_MOA=off; force open MACLAW_MOA=on"

	if moa.EnvForcedOff() {
		return Check{
			ID:      "llm.moa",
			Status:  StatusInfo,
			Message: "moa env=off (kill switch); UI enable ignored until MACLAW_MOA unset or on",
			Hint:    hint,
			Detail:  detail,
		}
	}
	if !m.Enabled {
		return Check{
			ID:      "llm.moa",
			Status:  StatusInfo,
			Message: "moa disabled in config; enable in LLM settings for multi-model council",
			Hint:    hint,
			Detail:  detail,
		}
	}
	if len(m.Presets) == 0 {
		return Check{
			ID:      "llm.moa",
			Status:  StatusWarn,
			Message: "moa enabled but no presets (pick other models in LLM settings)",
			Hint:    hint,
			Detail:  detail,
		}
	}
	// Count refs on default preset
	name := m.DefaultPreset
	if name == "" {
		for k := range m.Presets {
			name = k
			break
		}
	}
	refs := 0
	if p, ok := m.Presets[name]; ok {
		refs = len(p.ReferenceModels)
		detail["default_refs"] = refs
		detail["default_display"] = p.DisplayName
	}
	msg := fmt.Sprintf("moa ready preset=%s refs=%d env=%s", name, refs, moa.EnvStatusLabel())
	if m.AllowAuto {
		msg += "; allow_auto=on"
	}
	status := StatusOK
	if refs == 0 {
		status = StatusWarn
		msg = "moa enabled but default preset has no reference models"
	}
	// Temperature knobs are honored by request builders (K21 Phase 2): validate
	// the configured range instead of ignoring the fields.
	invalidTemp := 0
	for pname, p := range m.Presets {
		if corelib.InvalidMoATemperature(p) {
			invalidTemp++
			detail["invalid_temperature_preset"] = pname
		}
	}
	if invalidTemp > 0 {
		status = StatusWarn
		msg += fmt.Sprintf("; %d preset(s) with temperature out of range [0,2]", invalidTemp)
	}
	// Runtime stats (today's process/disk snapshot).
	st := moa.LoadStats()
	if st.Fanouts > 0 {
		detail["stats_fanouts"] = st.Fanouts
		detail["stats_ref_ok"] = st.RefOK
		detail["stats_ref_fail"] = st.RefFail
		detail["stats_last_ms"] = st.LastMS
		detail["stats_last_preset"] = st.LastPreset
		if line := moa.FormatStatsLine(); line != "" {
			msg += "; " + line
		}
	}
	return Check{
		ID:      "llm.moa",
		Status:  status,
		Message: msg,
		Hint:    hint,
		Detail:  detail,
	}
}
