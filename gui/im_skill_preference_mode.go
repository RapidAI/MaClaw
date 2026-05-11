package main

type skillPreferenceMode string

const (
	skillPreferenceNone            skillPreferenceMode = "none"
	skillPreferenceLocalOnly       skillPreferenceMode = "local_only"
	skillPreferenceRemoteRequired  skillPreferenceMode = "remote_required"
	skillPreferenceFallbackAllowed skillPreferenceMode = "fallback_allowed"
)

func (m skillPreferenceMode) String() string {
	return string(m)
}
