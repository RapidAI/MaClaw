//go:build linux

package guiapp

// Linux: no idle-time detection or display-dim support yet.
// The platform hooks remain nil, so the screen-dim goroutine
// will never trigger (getIdleDuration returns 0).
