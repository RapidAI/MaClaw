//go:build !windows

package guiapp

import "time"

// Input-liveness watchdog is Windows-only for now (WebView2-specific failure
// mode; the GTK/Mac backends have their own event plumbing).

func frontendInputWatchSupported() bool { return false }

func systemInputIdleDuration() time.Duration { return 0 }

func isMaclawWindowForeground() bool { return true }

func nudgeMainWindowInput() {}
