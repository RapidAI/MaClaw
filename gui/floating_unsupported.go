//go:build !(windows || (linux && cgo) || (darwin && cgo))

package main

import (
	"sync"

	"github.com/RapidAI/CodeClaw/gui/petpack"
)

// unsupportedFloatingWindow is a no-op constructor target for platforms or
// build tags that do not compile a native pet window (notably Linux CI with
// CGO_ENABLED=0). The real GTK/Cocoa/Win32 implementations remain in the
// OS-specific files and are unchanged.
type unsupportedFloatingWindow struct {
	app     *App
	created bool
	mu      sync.Mutex
	runtime string
	skin    string
	variant string
}

func newFloatingWindow(app *App) floatingWindow {
	return &unsupportedFloatingWindow{app: app}
}

func (w *unsupportedFloatingWindow) Create(_, _, _, _ int) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.created = true
	return nil
}

func (w *unsupportedFloatingWindow) Show() {}

func (w *unsupportedFloatingWindow) Hide() {}

func (w *unsupportedFloatingWindow) Destroy() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.created = false
}

func (w *unsupportedFloatingWindow) MoveTo(_, _ int) {}

func (w *unsupportedFloatingWindow) IsCreated() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.created
}

func (w *unsupportedFloatingWindow) UpdateSoundConfig(bool, string) {}

func (w *unsupportedFloatingWindow) UpdateMotionConfig(_, _, _ bool, _, skin, variant string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if skin != "" {
		w.skin = skin
	}
	if variant != "" {
		w.variant = petpack.ResolveVariantForRuntime(variant)
	}
}

func (w *unsupportedFloatingWindow) InvalidatePetPackAssets() {}

func (w *unsupportedFloatingWindow) SetPetRuntimeState(state string, _ int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.runtime = string(petpack.NormalizeState(state))
}

func (w *unsupportedFloatingWindow) CurrentPetRuntimeState() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.runtime == "" {
		return string(petpack.StateIdle)
	}
	return w.runtime
}

func (w *unsupportedFloatingWindow) PetPackRuntimeLevel(declared string) (string, string) {
	if declared == petpack.RendererNative {
		return declared, ""
	}
	return petpack.RendererNative, "当前平台暂不支持宠物窗口"
}
