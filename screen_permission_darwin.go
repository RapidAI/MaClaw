//go:build darwin

package main

/*
#cgo darwin CFLAGS: -DDARWIN
#cgo darwin LDFLAGS: -framework CoreGraphics

#include <CoreGraphics/CoreGraphics.h>
#include <AvailabilityMacros.h>
#include <stdlib.h>

// CGPreflightScreenCaptureAccess (macOS 10.15+) and
// CGRequestScreenCaptureAccess (macOS 10.15+) are weak-linked so the
// binary still loads on older systems. We resolve them at runtime via
// dlsym to avoid a hard link-time dependency on the 10.15 symbols.

#include <dlfcn.h>

typedef bool (*preflight_fn)(void);
typedef bool (*request_fn)(void);

static bool preflightScreenCapture(void) {
    preflight_fn fn = (preflight_fn)dlsym(RTLD_DEFAULT, "CGPreflightScreenCaptureAccess");
    if (fn) {
        return fn();
    }
    return true; // pre-Catalina: no TCC for screen recording
}

static bool requestScreenCapture(void) {
    request_fn fn = (request_fn)dlsym(RTLD_DEFAULT, "CGRequestScreenCaptureAccess");
    if (fn) {
        return fn();
    }
    return true; // pre-Catalina: always allowed
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
// returns false - the caller should treat this as "permission pending"
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
