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

func (a *App) HideFloatingButton() {
	if fa := a.existingFloatingAssistant(); fa != nil {
		fa.HideFloatingButton()
	}
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
