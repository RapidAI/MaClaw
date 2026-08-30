//go:build linux && cgo

package main

import (
	"os"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/gui/petpack"
)

func TestLinuxPetPackRuntimeLevelIsStatic(t *testing.T) {
	w := &linuxFloatingWindow{}
	got, reason := w.PetPackRuntimeLevel(petpack.RendererCharacter)
	if got != petpack.RendererNative {
		t.Fatalf("effective=%q, want %s", got, petpack.RendererNative)
	}
	if reason == "" {
		t.Fatal("expected degradation reason for character packs on Linux")
	}
	got, reason = w.PetPackRuntimeLevel(petpack.RendererNative)
	if got != petpack.RendererNative || reason != "" {
		t.Fatalf("native declared should pass through, got %q %q", got, reason)
	}
}

func TestLinuxSetPetRuntimeStateAndTTL(t *testing.T) {
	w := &linuxFloatingWindow{}
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

func TestLinuxInvalidatePetPackAssetsDoesNotPanic(t *testing.T) {
	w := &linuxFloatingWindow{}
	w.InvalidatePetPackAssets()
	w.UpdateMotionConfig(true, false, false, "balanced", "clawmate", "default")
}

func TestLinuxTryLoadPackFrameStill(t *testing.T) {
	reg := petpack.NewRegistry(t.TempDir(), petpack.BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	petpack.SetGlobalForTest(reg)
	frame := tryLoadPackFrame("clawmate", "default", "idle", 88, petpack.NewFrameCache())
	if frame == nil {
		t.Fatal("bundled clawmate idle still should load on Linux")
	}
	if b := encodeNRGBAToPNG(frame); len(b) < 32 {
		t.Fatalf("png encode too small: %d", len(b))
	}
}

func TestLinuxFloatingWindowMapsWhenDisplayAvailable(t *testing.T) {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("no display")
	}
	reg := petpack.NewRegistry(t.TempDir(), petpack.BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	petpack.SetGlobalForTest(reg)

	w := &linuxFloatingWindow{}
	if err := w.Create(40, 40, 104, 104); err != nil {
		t.Skipf("gtk create failed: %v", err)
	}
	defer w.Destroy()
	w.Show()
	linuxPumpGTKMs(250)
	if !w.IsCreated() {
		t.Fatal("expected created=true after successful Create")
	}
	if !linuxFloatingVisible() {
		t.Fatal("pet window should be gtk-visible after Show")
	}
	if !linuxFloatingMapped() {
		t.Fatal("pet window should be mapped (on screen), not a skip-taskbar ghost")
	}
	out := "/tmp/maclaw-linux-pet.png"
	if !linuxSaveWindowPNG(out) {
		t.Log("window pixmap capture unavailable (still mapped)")
		return
	}
	info, err := os.Stat(out)
	if err != nil || info.Size() < 200 {
		t.Fatalf("captured pet png missing or empty: %v", err)
	}
}

func TestLinuxCreateReportsFailureWithoutDisplay(t *testing.T) {
	if os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		t.Skip("display is available; failure path is for headless")
	}
	w := &linuxFloatingWindow{}
	if err := w.Create(10, 10, 88, 88); err == nil {
		w.Destroy()
		t.Fatal("Create should fail without a display")
	}
	if w.IsCreated() {
		t.Fatal("failed Create must not set created")
	}
}
