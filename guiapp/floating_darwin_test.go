//go:build darwin

package guiapp

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/guiapp/petpack"
)

func TestDarwinPetPackRuntimeLevelIsStatic(t *testing.T) {
	w := &darwinFloatingWindow{}
	got, reason := w.PetPackRuntimeLevel(petpack.RendererCharacter)
	if got != petpack.RendererNative {
		t.Fatalf("effective=%q, want %s", got, petpack.RendererNative)
	}
	if reason == "" {
		t.Fatal("expected degradation reason for character packs on macOS")
	}
	got, reason = w.PetPackRuntimeLevel(petpack.RendererNative)
	if got != petpack.RendererNative || reason != "" {
		t.Fatalf("native declared should pass through, got %q %q", got, reason)
	}
}

func TestDarwinSetPetRuntimeStateAndTTL(t *testing.T) {
	w := &darwinFloatingWindow{}
	w.SetPetRuntimeState("listening", 0)
	if got := w.CurrentPetRuntimeState(); got != "listening" {
		t.Fatalf("state=%q", got)
	}
	w.SetPetRuntimeState("thinking", 40)
	if got := w.CurrentPetRuntimeState(); got != "thinking" {
		t.Fatalf("ttl live state=%q", got)
	}
	time.Sleep(60 * time.Millisecond)
	if got := w.CurrentPetRuntimeState(); got != "idle" {
		t.Fatalf("after ttl state=%q", got)
	}
}

func TestDarwinInvalidatePetPackAssetsDoesNotPanic(t *testing.T) {
	w := &darwinFloatingWindow{}
	w.InvalidatePetPackAssets()
	w.UpdateMotionConfig(true, false, false, "balanced", "clawmate", "default")
	w.UpdateSoundConfig(false, "")
	if w.IsCreated() {
		t.Fatal("unshown window should not be created")
	}
}
