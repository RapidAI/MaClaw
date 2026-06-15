//go:build darwin && cgo

package remote

/*
#cgo darwin CFLAGS: -DDARWIN -Wno-deprecated-declarations -Wno-unguarded-availability-new
#cgo darwin LDFLAGS: -framework CoreGraphics -framework CoreFoundation

#include <CoreGraphics/CoreGraphics.h>
#include <stdlib.h>
#include <dlfcn.h>
#include <sys/utsname.h>

typedef bool (*preflight_fn)(void);

static bool preflightScreenCapture(void) {
    preflight_fn fn = (preflight_fn)dlsym(RTLD_DEFAULT, "CGPreflightScreenCaptureAccess");
    if (fn) {
        return fn();
    }
    return true; // pre-Catalina: no TCC for screen recording
}

// CGWindowListCreateImage is marked API_UNAVAILABLE in macOS 15+ SDK headers
// but the symbol still exists at runtime. Use dlsym to bypass compile-time check.
typedef CGImageRef (*CGWindowListCreateImageFn)(CGRect, uint32_t, uint32_t, uint32_t);

static int probeScreenCapture(void) {
    CGWindowListCreateImageFn fn = (CGWindowListCreateImageFn)dlsym(RTLD_DEFAULT, "CGWindowListCreateImage");
    if (!fn) {
        return 0; // symbol truly removed in future OS
    }
    CGRect rect = CGRectMake(0, 0, 1, 1);
    CGImageRef img = fn(rect, kCGWindowListOptionOnScreenOnly, kCGNullWindowID, kCGWindowImageDefault);
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

static int darwinKernelMajor(void) {
    struct utsname u;
    if (uname(&u) != 0) return 0;
    int major = 0;
    for (int i = 0; u.release[i] != '\0' && u.release[i] != '.'; i++) {
        major = major * 10 + (u.release[i] - '0');
    }
    return major;
}
*/
import "C"

// IsMacOS26OrLater returns true if running on macOS 26 (Tahoe) or later.
func IsMacOS26OrLater() bool {
	return int(C.darwinKernelMajor()) >= 25
}

// CheckScreenRecordingPermission returns true if the current process has
// screen recording permission. Uses the actual capture probe as ground
// truth, falling back to the TCC API only when the screen is locked
// (since locked screens also cause capture failure).
func CheckScreenRecordingPermission() bool {
	apiGranted := bool(C.preflightScreenCapture())

	// Probe is the ground truth — if it succeeds, we have permission
	// regardless of what the API says (handles TCC cache lag).
	if C.probeScreenCapture() == 1 {
		return true
	}
	// Probe failed — if screen is locked, trust the API.
	if C.isScreenLocked() == 1 {
		return apiGranted
	}
	return false
}

// IsScreenRecordingStale returns true when the TCC API reports permission
// granted but the actual capture probe fails (and the screen is not locked).
// This indicates a stale TCC record, common on macOS 26+ after pkg upgrades.
func IsScreenRecordingStale() bool {
	if !bool(C.preflightScreenCapture()) {
		return false // API says not granted — not stale, just denied
	}
	if C.probeScreenCapture() == 1 {
		return false // works fine
	}
	if C.isScreenLocked() == 1 {
		return false // locked screen causes probe failure, not stale
	}
	return true
}
