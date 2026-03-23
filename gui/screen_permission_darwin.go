//go:build darwin

package main

/*
#cgo darwin CFLAGS: -DDARWIN
#cgo darwin LDFLAGS: -framework CoreGraphics

#include <CoreGraphics/CoreGraphics.h>
#include <AvailabilityMacros.h>
#include <stdlib.h>
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

// probeScreenCapture performs a real 1x1 pixel capture via
// CGWindowListCreateImage. Returns:
//   1 = capture succeeded (permission works)
//   0 = capture failed (permission may be stale, or screen is off/locked)
static int probeScreenCapture(void) {
    CGRect rect = CGRectMake(0, 0, 1, 1);
    CGImageRef img = CGWindowListCreateImage(
        rect,
        kCGWindowListOptionOnScreenOnly,
        kCGNullWindowID,
        kCGWindowImageDefault);
    if (img == NULL) {
        return 0;
    }
    size_t w = CGImageGetWidth(img);
    size_t h = CGImageGetHeight(img);
    CGImageRelease(img);
    return (w > 0 && h > 0) ? 1 : 0;
}

// isScreenLocked checks CGSessionCopyCurrentDictionary for the lock flag.
// Returns 1 if locked, 0 if unlocked, -1 if unknown.
static int isScreenLocked(void) {
    CFDictionaryRef dict = CGSessionCopyCurrentDictionary();
    if (dict == NULL) {
        return -1;
    }
    CFStringRef key = CFSTR("CGSSessionScreenIsLocked");
    CFTypeRef val = CFDictionaryGetValue(dict, key);
    int result = -1;
    if (val != NULL && CFGetTypeID(val) == CFBooleanGetTypeID()) {
        result = CFBooleanGetValue((CFBooleanRef)val) ? 1 : 0;
    }
    CFRelease(dict);
    return result;
}
*/
import "C"

// HasScreenRecordingPermission returns true if the app has screen
// recording permission. It first checks the TCC API, then performs
// a real 1x1 pixel capture probe to handle macOS 26+ where the API
// may return true for stale TCC records.
//
// If the probe fails but the screen is locked, we trust the API result
// since a locked screen also causes capture to fail.
func HasScreenRecordingPermission() bool {
	if !bool(C.preflightScreenCapture()) {
		return false
	}
	// On macOS 26+, CGPreflightScreenCaptureAccess can return true even
	// when the permission is effectively revoked (stale TCC entry).
	// Verify with an actual capture probe.
	if C.probeScreenCapture() == 1 {
		return true
	}
	// Probe failed. If the screen is locked, the failure is expected —
	// trust the API result (which said "granted").
	if C.isScreenLocked() == 1 {
		return true
	}
	// Screen is not locked but probe failed → permission is stale.
	return false
}

// RequestScreenRecordingPermission triggers the macOS system prompt to grant
// screen recording permission. This only shows the dialog once; subsequent
// calls are no-ops if the user already responded.
func RequestScreenRecordingPermission() {
	C.requestScreenCapture()
}

// EnsureScreenRecordingPermission checks and, if needed, requests screen
// recording permission. Returns true if permission is already granted.
// If not granted, it triggers the system permission dialog (once) and
// returns false — the caller should treat this as "permission pending".
func EnsureScreenRecordingPermission() bool {
	if HasScreenRecordingPermission() {
		return true
	}
	RequestScreenRecordingPermission()
	return HasScreenRecordingPermission()
}
