package views

// ConfigSavedMsg is sent after a config value is successfully persisted.
// The main loop uses this to update the status bar and reload derived state.
type ConfigSavedMsg struct {
	Key   string
	Value string
}

// ConfigSaveFailedMsg is sent when a config save operation fails.
type ConfigSaveFailedMsg struct {
	Key   string
	Error string
}
