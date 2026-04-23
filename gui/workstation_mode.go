package main

import "github.com/RapidAI/CodeClaw/corelib"

// refreshWorkstationMode reads the config and applies workstation mode state.
// Called on startup and after config save.
func (a *App) refreshWorkstationMode(config corelib.AppConfig) {
	a.setWorkstationMode(config.WorkstationMode, config.ScreenDimTimeoutMin)
	// When workstation mode is on, also start the screen-dim timer so the
	// display turns off after the configured idle timeout.
	// Only touch the dim timer here if workstation mode is enabled;
	// otherwise let refreshPowerOptimizationStateFromConfig manage it.
	if config.WorkstationMode {
		a.updateScreenDimTimer(true, config.ScreenDimTimeoutMin)
	}
}
