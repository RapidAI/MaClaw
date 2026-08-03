package main

// floatingWindow is the platform-specific floating window abstraction.
// Each supported platform provides its own implementation via build-tag files:
//   - gui/floating_windows.go  (//go:build windows)
//   - gui/floating_darwin.go   (//go:build darwin)
//   - gui/floating_linux.go    (//go:build linux)
//
// Each platform file also provides the factory function:
//
//	func newFloatingWindow(app *App) floatingWindow
//
// which constructs the platform-native floating window implementation.
// Requirements: 9.4
type floatingWindow interface {
	// Create initializes and creates the floating window at the given
	// screen coordinates with the specified dimensions.
	Create(x, y, w, h int) error

	// Show makes the floating window visible on screen.
	Show()

	// Hide hides the floating window without destroying it.
	Hide()

	// Destroy destroys the floating window and releases all associated
	// resources (WebView, native handles, etc.).
	Destroy()

	// MoveTo repositions the floating window to the given screen coordinates.
	MoveTo(x, y int)

	// IsCreated returns whether the floating window has been created and
	// is ready to be shown.
	IsCreated() bool

	// UpdateSoundConfig updates in-memory sound settings without rebuilding
	// the window. Called when only sound-related config changes.
	UpdateSoundConfig(soundEnabled bool, preset string)

	// UpdateMotionConfig updates motion/quiet/reduced-motion and active skin/variant
	// without full window recreation when possible.
	UpdateMotionConfig(motionEnabled, quiet, reducedMotion bool, interactionMode, skin, variant string)

	// InvalidatePetPackAssets drops decoded pet resources after a pack is
	// installed, upgraded, or removed without requiring a settings change.
	InvalidatePetPackAssets()

	// SetPetRuntimeState applies a semantic pet state (listening/thinking/…) with optional TTL.
	SetPetRuntimeState(state string, ttlMs int)

	// CurrentPetRuntimeState returns the active runtime state id.
	CurrentPetRuntimeState() string

	// PetPackRuntimeLevel reports the renderer level the window is actually
	// using for the currently selected pack, given the pack's declared level.
	// The returned reason is empty when there is no degradation.
	PetPackRuntimeLevel(declared string) (effective, reason string)
}
