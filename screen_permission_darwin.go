//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework CoreGraphics -framework Foundation

#include <CoreGraphics/CoreGraphics.h>

// CGPreflightScreenCaptureAccess and CGRequestScreenCaptureAccess are
// available since macOS 10.15 (Catalina). We call them to ensure the
// permission prompt is associated with our app's bundle ID, not with
// the child bash/screencapture process.

static bool preflightScreenCapture(void) {
    if (@available(macOS 10.15, *)) {
        return CGPreflightScreenCaptureAccess();
    }
    return true; // pre-Catalina: no TCC for screen recording
}

static void requestScreenCapture(void) {
    if (@available(macOS 10.15, *)) {
        CGRequestScreenCaptureAccess();
    }
}
*/
import "C"

// HasScreenRecordingPermission returns true if the app already has screen
// recording permission in the macOS TCC database.
func HasScreenRecordingPermission() bool {
	return bool(C.preflightScreenCapture())
}

// RequestScreenRecordingPermission triggers the macOS system prompt to grant
// screen recording permission. This only shows the dialog once; subsequent
// calls are no-ops if the user already responded. The permission is tied to
// the app's bundle ID, so granting it here covers child processes like
// screencapture and bash.
func RequestScreenRecordingPermission() {
	C.requestScreenCapture()
}

// EnsureScreenRecordingPermission checks and, if needed, requests screen
// recording permission. Returns true if permission is already granted.
// If not granted, it triggers the system permission dialog (once) and
// returns false — the caller should treat this as "permission pending"
// since the user hasn't responded to the dialog yet.
func EnsureScreenRecordingPermission() bool {
	if HasScreenRecordingPermission() {
		return true
	}
	// Trigger the system dialog. CGRequestScreenCaptureAccess shows the
	// prompt asynchronously, so permission won't be granted until the user
	// clicks "Allow" and (typically) restarts the app.
	RequestScreenRecordingPermission()
	// Re-check in case the app was already approved (e.g. user toggled
	// the switch in System Settings before this call).
	return HasScreenRecordingPermission()
}
