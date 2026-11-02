//go:build !windows

package guiapp

import "time"

// Click-echo detection is Windows-only (WebView2-specific failure mode; the
// GTK/Mac backends have their own event plumbing).

func startClickEchoMonitor() {}

func clickEchoMonitorRunning() bool { return false }

func clickEchoEnsureHWND() {}

func recentWindowClicks(window time.Duration) int { return 0 }

func windowClickStamps(window time.Duration) []time.Time { return nil }

func oldestWindowClickAge(window time.Duration) time.Duration { return 0 }
