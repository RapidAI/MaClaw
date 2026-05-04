package main

import "github.com/RapidAI/CodeClaw/corelib"

// refreshWorkstationMode reads the config and applies workstation mode state.
// Called on startup and after config save.
func (a *App) refreshWorkstationMode(config corelib.AppConfig) {
	a.setWorkstationMode(config.WorkstationMode, config.ScreenDimTimeoutMin)
	// The display-off timer is owned by refreshPowerOptimizationStateFromConfig.
	// Keeping a single owner avoids briefly overlapping screen-dim goroutines
	// during config refreshes, which can otherwise issue duplicate display-off
	// commands and make a sleeping display flash.
}
