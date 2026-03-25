//go:build darwin && cgo

package remote

/*
#cgo darwin CFLAGS: -DDARWIN
#cgo darwin LDFLAGS: -framework CoreGraphics

#include <CoreGraphics/CoreGraphics.h>
#include <stdlib.h>
#include <dlfcn.h>

typedef bool (*preflight_fn)(void);

static bool preflightScreenCapture(void) {
    preflight_fn fn = (preflight_fn)dlsym(RTLD_DEFAULT, "CGPreflightScreenCaptureAccess");
    if (fn) {
        return fn();
    }
    return true; // pre-Catalina: no TCC for screen recording
}

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

// CheckScreenRecordingPermission returns true if the current process has
// screen recording permission. Uses the TCC API check plus a real capture
// probe to handle macOS 26+ stale TCC records. If the probe fails but the
// screen is locked, trusts the API result since locked screens also cause
// capture failure.
func CheckScreenRecordingPermission() bool {
	if !bool(C.preflightScreenCapture()) {
		return false
	}
	if C.probeScreenCapture() == 1 {
		return true
	}
	// Probe failed — if screen is locked, trust the API.
	if C.isScreenLocked() == 1 {
		return true
	}
	return false
}
