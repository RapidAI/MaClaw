package petpack

import "strings"

// EffectiveMotion is the resolved motion profile for the native pet.
type EffectiveMotion struct {
	IdleMs       int
	ListeningMs  int
	ThinkingMs   int
	SpeakingMs   int
	Amplitude    float64
	SoundAllowed bool
	StaticOnly   bool
}

// EffectiveMotionInput is the control-plane input for motion resolution.
type EffectiveMotionInput struct {
	Pack           PetPackMotion
	InteractionMode string // quiet | balanced | active
	QuietMode      bool
	ReducedMotion  bool
	MotionEnabled  bool
	SoundEnabled   bool
}

// EffectiveMotionFrom computes durations/amplitude/soundAllowed.
// Precedence: reduced-motion and quiet win over pack; active multiplies base.
func EffectiveMotionFrom(in EffectiveMotionInput) EffectiveMotion {
	m := in.Pack
	if m.IdleMs <= 0 {
		m.IdleMs = 4000
	}
	if m.ListeningMs <= 0 {
		m.ListeningMs = 1200
	}
	if m.ThinkingMs <= 0 {
		m.ThinkingMs = 1800
	}
	if m.SpeakingMs <= 0 {
		m.SpeakingMs = 950
	}
	if m.Amplitude <= 0 {
		m.Amplitude = 0.85
	}
	if m.Amplitude > 1 {
		m.Amplitude = 1
	}

	out := EffectiveMotion{
		IdleMs:       m.IdleMs,
		ListeningMs:  m.ListeningMs,
		ThinkingMs:   m.ThinkingMs,
		SpeakingMs:   m.SpeakingMs,
		Amplitude:    m.Amplitude,
		SoundAllowed: in.SoundEnabled && in.MotionEnabled && !in.QuietMode && !in.ReducedMotion,
		StaticOnly:   in.ReducedMotion || !in.MotionEnabled,
	}

	if in.QuietMode || strings.EqualFold(in.InteractionMode, "quiet") {
		out.Amplitude *= 0.35
		out.IdleMs = int(float64(out.IdleMs) * 1.5)
		out.ListeningMs = int(float64(out.ListeningMs) * 1.4)
		out.ThinkingMs = int(float64(out.ThinkingMs) * 1.4)
		out.SpeakingMs = int(float64(out.SpeakingMs) * 1.5)
		out.SoundAllowed = false
	} else if strings.EqualFold(in.InteractionMode, "active") {
		out.Amplitude = clamp01(out.Amplitude * 1.15)
		out.IdleMs = int(float64(out.IdleMs) * 0.7)
		out.ListeningMs = int(float64(out.ListeningMs) * 0.75)
		out.ThinkingMs = int(float64(out.ThinkingMs) * 0.7)
		out.SpeakingMs = int(float64(out.SpeakingMs) * 0.75)
	}

	if in.ReducedMotion || !in.MotionEnabled {
		out.Amplitude = 0
		out.StaticOnly = true
		out.SoundAllowed = false
		out.IdleMs = 0
		out.ListeningMs = 0
		out.ThinkingMs = 0
		out.SpeakingMs = 0
	}
	return out
}

// EffectiveSoundProfile resolves which Beep tone preset to play.
// K21: user pet_motion_sound_preset always wins over pack sound_profile.
// Pack only contributes pitch (and optional UI recommendation).
type EffectiveSoundProfile struct {
	Preset string  // classic|bubble|chime|synth|soft
	Pitch  float64 // pack pitch × interaction boost applied by caller if needed
	// PackRecommended is the pack's sound_profile for UI hints only.
	PackRecommended string
}

// EffectiveSoundProfileFrom implements K21.
func EffectiveSoundProfileFrom(userPreset, packProfile string, packPitch float64) EffectiveSoundProfile {
	user := normalizeSoundPreset(userPreset)
	pack := normalizeSoundPreset(packProfile)
	pitch := packPitch
	if pitch <= 0 {
		pitch = 1
	}
	return EffectiveSoundProfile{
		Preset:          user, // user always wins
		Pitch:           pitch,
		PackRecommended: pack,
	}
}

func normalizeSoundPreset(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "bubble", "chime", "synth", "soft", "classic":
		return strings.ToLower(strings.TrimSpace(p))
	default:
		return "classic"
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
