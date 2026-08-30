package main

type skillPreferenceMode string

const (
	skillPreferenceNone            skillPreferenceMode = "none"
	skillPreferenceLocalOnly       skillPreferenceMode = "local_only"
	skillPreferenceRemoteRequired  skillPreferenceMode = "remote_required"
	skillPreferenceFallbackAllowed skillPreferenceMode = "fallback_allowed"
	// skillPreferenceAgentGuided is an installed Markdown workflow the current
	// agent must follow with host tools. It is not a manage_skill target and
	// must not fall through to discover_tool / generate_pdf.
	skillPreferenceAgentGuided skillPreferenceMode = "agent_guided"
)

func (m skillPreferenceMode) String() string {
	return string(m)
}
