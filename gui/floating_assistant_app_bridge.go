package main

import "github.com/wailsapp/wails/v2/pkg/runtime"

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

func (a *App) QuitApp() {
	if fa := a.ensureFloatingAssistant(); fa != nil {
		fa.QuitApp()
		return
	}
	if a != nil && a.ctx != nil {
		runtime.Quit(a.ctx)
	}
}
