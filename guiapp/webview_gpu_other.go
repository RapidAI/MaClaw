//go:build !windows

package guiapp

// WebView2 GPU policy is Windows-only: the options type and the blank-window
// failure mode this records do not exist on the other platforms, whose webviews
// handle acceleration themselves.

func webviewGPUDisabled(string) bool         { return false }
func noteWebviewGPUActive(bool)              {}
func recordWebviewGPUFailure(string, string) {}
func clearWebviewGPUFailure(string)          {}
