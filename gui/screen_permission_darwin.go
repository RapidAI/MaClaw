//go:build darwin

package main

/*
#cgo darwin CFLAGS: -DDARWIN
#cgo darwin LDFLAGS: -framework CoreGraphics -framework CoreFoundation

#include <CoreGraphics/CoreGraphics.h>
#include <AvailabilityMacros.h>
#include <stdlib.h>
#include <dlfcn.h>
#include <sys/utsname.h>

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

// darwinKernelMajor returns the major version of the Darwin kernel.
// macOS 26 (Tahoe) = Darwin 25.x
// macOS 15 (Sequoia) = Darwin 24.x
static int darwinKernelMajor(void) {
    struct utsname u;
    if (uname(&u) != 0) return 0;
    int major = 0;
    for (int i = 0; u.release[i] != '\0' && u.release[i] != '.'; i++) {
        major = major * 10 + (u.release[i] - '0');
    }
    return major;
}

// getBundleID returns the CFBundleIdentifier of the main bundle, or NULL.
static const char* getBundleID(void) {
    CFBundleRef bundle = CFBundleGetMainBundle();
    if (bundle == NULL) return NULL;
    CFStringRef bid = CFBundleGetIdentifier(bundle);
    if (bid == NULL) return NULL;
    // Use a static buffer — this is called rarely and from a single goroutine.
    static char buf[256];
    if (!CFStringGetCString(bid, buf, sizeof(buf), kCFStringEncodingUTF8)) {
        return NULL;
    }
    return buf;
}
*/
import "C"

import (
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// staleTCCResetOnce ensures we only attempt the tccutil reset once per
// process lifetime to avoid spamming the user with password dialogs.
var staleTCCResetOnce sync.Once

// isMacOS26OrLater returns true if running on macOS 26 (Tahoe) or later.
// macOS 26 = Darwin kernel 25.x.
func isMacOS26OrLater() bool {
	return int(C.darwinKernelMajor()) >= 25
}

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

// resetStaleTCCRecord attempts to clear the stale TCC record for screen
// capture using tccutil, then re-requests permission. On macOS 26+ this
// requires admin privileges, so we use osascript to show a system password
// dialog. This is called at most once per process lifetime.
//
// After the reset, it triggers CGRequestScreenCaptureAccess which opens
// System Settings. The user must manually toggle the permission there,
// so this function returns false — the caller should inform the user
// to restart the app after granting permission.
func resetStaleTCCRecord() bool {
	bundleID := C.getBundleID()
	if bundleID == nil {
		log.Println("[screen_permission] cannot determine bundle ID, skipping TCC reset")
		return false
	}
	bid := C.GoString(bundleID)
	if bid == "" {
		log.Println("[screen_permission] empty bundle ID, skipping TCC reset")
		return false
	}

	// Validate bundle ID to prevent shell injection. Bundle IDs should
	// only contain alphanumeric characters, dots, and hyphens.
	for _, c := range bid {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '-') {
			log.Printf("[screen_permission] suspicious bundle ID %q, skipping TCC reset", bid)
			return false
		}
	}

	log.Printf("[screen_permission] detected stale TCC record on macOS 26+, resetting ScreenCapture for %s", bid)

	// Use osascript with administrator privileges to run tccutil reset.
	// This pops a system password dialog for the user.
	script := `do shell script "tccutil reset ScreenCapture ` + bid + `" with administrator privileges`
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		outStr := strings.TrimSpace(string(out))
		log.Printf("[screen_permission] tccutil reset failed: %v (output: %s)", err, outStr)
		return false
	}
	log.Println("[screen_permission] tccutil reset succeeded, re-requesting permission")

	// Give TCC daemon a moment to process the reset.
	time.Sleep(500 * time.Millisecond)

	// Re-request — this will show the system permission dialog / open
	// System Settings. On macOS 26+ the user must manually toggle the
	// switch, so we poll for a reasonable window.
	RequestScreenRecordingPermission()

	// Poll for up to 30 seconds — the user needs time to find the toggle
	// in System Settings and enable it.
	for i := 0; i < 30; i++ {
		time.Sleep(time.Second)
		if HasScreenRecordingPermission() {
			log.Println("[screen_permission] permission granted after TCC reset")
			return true
		}
	}

	log.Println("[screen_permission] permission not yet granted after TCC reset, user may need to restart")
	return false
}

// EnsureScreenRecordingPermission checks and, if needed, requests screen
// recording permission. Returns true if permission is already granted.
//
// On macOS 26+, if the TCC API says "granted" but the actual capture probe
// fails (stale TCC record after pkg upgrade / re-signing), this function
// will attempt a one-time tccutil reset with admin privileges (system
// password dialog) followed by a fresh permission request.
func EnsureScreenRecordingPermission() bool {
	if HasScreenRecordingPermission() {
		return true
	}

	// Check if this is a stale TCC situation (API says yes, probe says no).
	apiSaysGranted := bool(C.preflightScreenCapture())
	screenLocked := C.isScreenLocked() == 1

	if apiSaysGranted && !screenLocked && isMacOS26OrLater() {
		// Stale TCC record on macOS 26+. Try reset once.
		var resetOK bool
		staleTCCResetOnce.Do(func() {
			resetOK = resetStaleTCCRecord()
		})
		if resetOK {
			return true
		}
	}

	// Normal path: request permission (first-time grant).
	RequestScreenRecordingPermission()
	return HasScreenRecordingPermission()
}
