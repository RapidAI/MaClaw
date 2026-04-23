package views

// ConfigSavedMsg is sent after a config value is successfully persisted.
// The main loop uses this to update the status bar and reload derived state.
type ConfigSavedMsg struct {
	Key   string
	Value string
}
