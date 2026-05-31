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

func (a *App) OnFloatingButtonClicked() {
	if fa := a.ensureFloatingAssistant(); fa != nil {
		fa.OnFloatingButtonClicked()
	}
}

func (a *App) OnFloatingButtonDragged(x, y int) {
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

func (a *App) OpenPetSettingsFromMenu() {
	if a == nil || a.ctx == nil {
		return
	}
	runtime.WindowShow(a.ctx)
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
	runtime.WindowSetAlwaysOnTop(a.ctx, false)
	runtime.EventsEmit(a.ctx, "open-pet-settings", map[string]any{"source": "pet"})
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
