package petpack

import "testing"

func TestEffectiveSoundProfileUserWins(t *testing.T) {
	got := EffectiveSoundProfileFrom("soft", "chime", 1.2)
	if got.Preset != "soft" {
		t.Fatalf("preset = %q, want soft (user wins)", got.Preset)
	}
	if got.PackRecommended != "chime" {
		t.Fatalf("pack recommended = %q, want chime", got.PackRecommended)
	}
	if got.Pitch != 1.2 {
		t.Fatalf("pitch = %v, want 1.2", got.Pitch)
	}
	// empty user falls back to classic, not pack
	got2 := EffectiveSoundProfileFrom("", "bubble", 0)
	if got2.Preset != "classic" {
		t.Fatalf("empty user preset = %q, want classic", got2.Preset)
	}
	if got2.Pitch != 1 {
		t.Fatalf("default pitch = %v, want 1", got2.Pitch)
	}
}

func TestEffectiveMotionQuietAndReduced(t *testing.T) {
	base := PetPackMotion{IdleMs: 4000, ListeningMs: 1200, ThinkingMs: 1800, SpeakingMs: 950, Amplitude: 0.9}
	quiet := EffectiveMotionFrom(EffectiveMotionInput{
		Pack: base, InteractionMode: "balanced", QuietMode: true,
		MotionEnabled: true, SoundEnabled: true,
	})
	if quiet.SoundAllowed {
		t.Fatal("quiet must disable sound")
	}
	if quiet.Amplitude >= 0.9 {
		t.Fatalf("quiet amplitude should shrink, got %v", quiet.Amplitude)
	}

	reduced := EffectiveMotionFrom(EffectiveMotionInput{
		Pack: base, InteractionMode: "active", ReducedMotion: true,
		MotionEnabled: true, SoundEnabled: true,
	})
	if !reduced.StaticOnly || reduced.SoundAllowed || reduced.Amplitude != 0 {
		t.Fatalf("reduced-motion must be static/silent: %+v", reduced)
	}

	off := EffectiveMotionFrom(EffectiveMotionInput{
		Pack: base, MotionEnabled: false, SoundEnabled: true,
	})
	if !off.StaticOnly || off.SoundAllowed {
		t.Fatalf("motion disabled must be static: %+v", off)
	}
}

func TestNormalizeState(t *testing.T) {
	if NormalizeState("listening") != StateListening {
		t.Fatal("listening")
	}
	if NormalizeState("think") != StateThinking {
		t.Fatal("think")
	}
	if NormalizeState("SPEAKING") != StateSpeaking {
		t.Fatal("speaking")
	}
	if NormalizeState("done") != StateDone {
		t.Fatal("done")
	}
	if NormalizeState("nope") != StateIdle {
		t.Fatal("idle fallback")
	}
}
