package guiapp

import (
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// petPositionLockMu serializes the config writes for the pin. The menu thread
// does not take it: the window flips first, and a late write waits here so
// the newest click is the last one stored.
var petPositionLockMu sync.Mutex

var petPositionLockState struct {
	sync.Mutex
	valid   bool
	locked  bool
	seq     uint64
	pending uint64
}

// rememberPetPositionLock publishes the newest pin and returns its sequence.
// A later click or a backup restore bumps the sequence. A persist that still
// holds an older sequence must not write.
func rememberPetPositionLock(locked bool) uint64 {
	petPositionLockState.Lock()
	petPositionLockState.seq++
	petPositionLockState.valid = true
	petPositionLockState.locked = locked
	seq := petPositionLockState.seq
	petPositionLockState.Unlock()
	return seq
}

func petPositionLockSeqCurrent(seq uint64) bool {
	petPositionLockState.Lock()
	defer petPositionLockState.Unlock()
	return petPositionLockState.valid && petPositionLockState.seq == seq
}

// notePetPositionLockWrite marks the menu write about to enter the config
// lock. Zero clears it. The patch reads it again under configMu, so a backup
// restore that published a newer sequence can drop this write before the
// field changes.
func notePetPositionLockWrite(seq uint64) {
	petPositionLockState.Lock()
	petPositionLockState.pending = seq
	petPositionLockState.Unlock()
}

// petPositionLockWriteCurrent reports whether the marked menu write is still
// the newest pin. A pending of zero is not a menu write and may proceed.
func petPositionLockWriteCurrent() bool {
	petPositionLockState.Lock()
	defer petPositionLockState.Unlock()
	if petPositionLockState.pending == 0 {
		return true
	}
	return petPositionLockState.valid && petPositionLockState.seq == petPositionLockState.pending
}

// abandonPetPositionLock rolls the publish back when this sequence is still
// the newest. The sequence is not bumped: the failed write is not a new
// choice. A newer click is left alone.
func abandonPetPositionLock(seq uint64) (bool, bool) {
	petPositionLockState.Lock()
	defer petPositionLockState.Unlock()
	if !petPositionLockState.valid || petPositionLockState.seq != seq {
		return false, false
	}
	petPositionLockState.locked = !petPositionLockState.locked
	return true, petPositionLockState.locked
}

// petPositionLockForApply is the value the live window should show. A menu
// click publishes before the config file is written; that publish wins over
// the snapshot captured by an older save.
func petPositionLockForApply(saved bool) bool {
	petPositionLockState.Lock()
	defer petPositionLockState.Unlock()
	if petPositionLockState.valid {
		return petPositionLockState.locked
	}
	return saved
}

func applyPetPositionLock(fa *FloatingAssistantManager, saved bool) {
	if fa == nil {
		return
	}
	fa.UpdatePositionLock(corelib.AppConfig{PetPositionLocked: petPositionLockForApply(saved)})
}

func (a *App) ensureFloatingAssistant() *FloatingAssistantManager {
	if a == nil {
		return nil
	}
	a.floatingAssistantMu.Lock()
	defer a.floatingAssistantMu.Unlock()
	if a.floatingAssistant == nil {
		a.floatingAssistant = NewFloatingAssistantManager(a)
	}
	return a.floatingAssistant
}

func (a *App) existingFloatingAssistant() *FloatingAssistantManager {
	if a == nil {
		return nil
	}
	a.floatingAssistantMu.Lock()
	defer a.floatingAssistantMu.Unlock()
	return a.floatingAssistant
}

// onFloatingButtonClicked is the native-window entry point for a pet click.
// It is intentionally unexported: the retired WebView floating button was the
// only Wails caller, and the native window path calls it directly.
func (a *App) onFloatingButtonClicked() {
	if fa := a.ensureFloatingAssistant(); fa != nil {
		fa.OnFloatingButtonClicked()
	}
}

// onFloatingButtonDragged is kept for parity with the click path. The native
// drag flow currently reports positions through UpdatePosition instead.
func (a *App) onFloatingButtonDragged(x, y int) {
	if fa := a.existingFloatingAssistant(); fa != nil {
		fa.OnFloatingButtonDragged(x, y)
	}
}

func (a *App) HideFloatingButton() {
	if fa := a.existingFloatingAssistant(); fa != nil {
		fa.HideFloatingButton()
	}
}

func (a *App) DisablePetFromMenu() {
	if fa := a.existingFloatingAssistant(); fa != nil {
		fa.DisablePetFromMenu()
	}
}

func (a *App) syncPetVoiceCompanion() {
	if a == nil {
		return
	}
	go func() {
		if fa := a.existingFloatingAssistant(); fa != nil {
			fa.syncCompanion()
		}
	}()
}

func (a *App) showMainWindowFromPet() {
	if a == nil || a.ctx == nil {
		return
	}
	runtime.WindowShow(a.ctx)
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
	runtime.WindowSetAlwaysOnTop(a.ctx, false)
	a.emitEvent("switch-to-ai-panel", map[string]any{
		"source": "pet-menu",
		"voice":  false,
	})
}

func (a *App) openPetConversationFromMenu() {
	if a == nil || a.ctx == nil {
		return
	}
	runtime.WindowShow(a.ctx)
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
	runtime.WindowSetAlwaysOnTop(a.ctx, false)
	a.emitEvent("open-pet-conversation", map[string]any{"source": "pet"})
}

// openPetSettingsFromMenu opens the pet settings from the native context menu.
// Unexported for the same reason as onFloatingButtonClicked: the WebView
// floating button that used the Wails binding no longer exists.
func (a *App) openPetSettingsFromMenu() {
	if a == nil || a.ctx == nil {
		return
	}
	runtime.WindowShow(a.ctx)
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
	runtime.WindowSetAlwaysOnTop(a.ctx, false)
	a.emitEvent("open-pet-settings", map[string]any{"source": "pet"})
}

// persistPetPositionLockFromMenu writes the pin the menu already showed.
// The caller flipped the live window on the menu thread and must not hold
// that window's mutex here. This does not read config to choose a direction.
// The file write runs in the background, so the window thread is not blocked
// on configMu. A newer click or a backup restore bumps the sequence and this
// write is dropped. A failed write rolls the window back only while this
// sequence is still current.
func (a *App) persistPetPositionLockFromMenu(next bool) {
	if a == nil {
		return
	}
	seq := rememberPetPositionLock(next)
	go func() {
		petPositionLockMu.Lock()
		defer petPositionLockMu.Unlock()
		if !petPositionLockSeqCurrent(seq) {
			return
		}
		// Mark the write before the config lock. Patch checks it again while
		// holding configMu, after a restore may have published a newer pin.
		notePetPositionLockWrite(seq)
		defer notePetPositionLockWrite(0)
		if _, err := a.PatchConfigFields(map[string]interface{}{
			"pet_position_locked": next,
		}); err != nil {
			rolled, locked := abandonPetPositionLock(seq)
			if !rolled {
				return
			}
			if fa := a.existingFloatingAssistant(); fa != nil {
				fa.UpdatePositionLock(corelib.AppConfig{PetPositionLocked: locked})
			}
		}
	}()
}

func (a *App) QuitApp() {
	if fa := a.ensureFloatingAssistant(); fa != nil {
		fa.QuitApp()
		return
	}
	// requestAppQuit handles a nil ctx (startup not reached) by forcing exit.
	requestAppQuit(a.ctx)
}
