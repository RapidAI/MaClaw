//go:build !windows

package guiapp

// WebView2 GPU policy is Windows-only: the options type and the blank-window
// failure mode this record do not exist on the other platforms, whose webviews
// handle acceleration themselves.

func webviewGPUDisabled(string) bool         { return false }
func noteWebviewGPUActive(bool)              {}
func recordWebviewGPUFailure(string, string) {}
func clearWebviewGPUFailure(string)          {}

// perfSessionIsRemote reports whether the process runs in a Remote Desktop
// session. RDP visual-hosting is a Windows-only concern (see
// usevisualhost_wails3_windows.go); on other platforms it is always false.
func perfSessionIsRemote() bool { return false }
