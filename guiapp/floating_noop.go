package guiapp

import (
	"sync"

	"github.com/RapidAI/CodeClaw/guiapp/petpack"
)

// noopFloatingWindow is the testing.Testing() substitute returned by
// newFloatingWindow on every supported platform. A full `go test` run must
// never paint real desktop-pet windows across the screen, so window creation
// and visibility are recorded as flags instead of going to the OS. The flags
// keep IsCreated-based assertions working exactly like the real backends.
type noopFloatingWindow struct {
	app     *App
	mu      sync.Mutex
	created bool
	shown   bool
	runtime string
}

func (w *noopFloatingWindow) Create(_, _, _, _ int) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.created = true
	return nil
}

func (w *noopFloatingWindow) Show() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.shown = true
}

func (w *noopFloatingWindow) Hide() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.shown = false
}

func (w *noopFloatingWindow) Destroy() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.created = false
	w.shown = false
}

func (w *noopFloatingWindow) MoveTo(_, _ int) {}

func (w *noopFloatingWindow) IsCreated() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.created
}

func (w *noopFloatingWindow) UpdateSoundConfig(bool, string) {}

func (w *noopFloatingWindow) UpdateMotionConfig(_, _, _ bool, _, _, _ string) {}

func (w *noopFloatingWindow) InvalidatePetPackAssets() {}

func (w *noopFloatingWindow) SetPetRuntimeState(state string, _ int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.runtime = string(petpack.NormalizeState(state))
}

func (w *noopFloatingWindow) CurrentPetRuntimeState() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.runtime == "" {
		return string(petpack.StateIdle)
	}
	return w.runtime
}

func (w *noopFloatingWindow) PetPackRuntimeLevel(declared string) (string, string) {
	return declared, ""
}
