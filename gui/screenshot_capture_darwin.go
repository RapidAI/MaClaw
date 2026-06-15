//go:build darwin

package main

/*
#cgo darwin CFLAGS: -DDARWIN -Wno-deprecated-declarations -Wno-unguarded-availability-new
#cgo darwin LDFLAGS: -framework CoreGraphics -framework ImageIO -framework CoreFoundation

#include <CoreGraphics/CoreGraphics.h>
#include <ImageIO/ImageIO.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <dlfcn.h>

// CGWindowListCreateImage is marked API_UNAVAILABLE in macOS 15+ SDK headers
// but the symbol still exists at runtime. Use dlsym to bypass.
typedef CGImageRef (*CGWindowListCreateImageFn)(CGRect, uint32_t, uint32_t, uint32_t);

// captureFullScreenPNG captures the entire screen using CGWindowListCreateImage
// (loaded dynamically) and encodes it as PNG into a malloc'd buffer.
// The caller must free() the returned pointer. Returns NULL on failure.
static const void* captureFullScreenPNG(size_t* outLen) {
    *outLen = 0;

    CGWindowListCreateImageFn fn = (CGWindowListCreateImageFn)dlsym(RTLD_DEFAULT, "CGWindowListCreateImage");
    if (!fn) {
        return NULL;
    }

    CGImageRef image = fn(
        CGRectInfinite,
        kCGWindowListOptionOnScreenOnly,
        kCGNullWindowID,
        kCGWindowImageDefault);
    if (image == NULL) {
        return NULL;
    }

    size_t w = CGImageGetWidth(image);
    size_t h = CGImageGetHeight(image);
    if (w == 0 || h == 0) {
        CGImageRelease(image);
        return NULL;
    }

    // Encode to PNG in memory using CGImageDestination with a CFMutableData.
    CFMutableDataRef pngData = CFDataCreateMutable(kCFAllocatorDefault, 0);
    if (pngData == NULL) {
        CGImageRelease(image);
        return NULL;
    }

    CGImageDestinationRef dest = CGImageDestinationCreateWithData(
        pngData,
        CFSTR("public.png"),
        1,
        NULL);
    if (dest == NULL) {
        CFRelease(pngData);
        CGImageRelease(image);
        return NULL;
    }

    CGImageDestinationAddImage(dest, image, NULL);
    bool ok = CGImageDestinationFinalize(dest);
    CFRelease(dest);
    CGImageRelease(image);

    if (!ok) {
        CFRelease(pngData);
        return NULL;
    }

    CFIndex len = CFDataGetLength(pngData);
    if (len <= 0) {
        CFRelease(pngData);
        return NULL;
    }

    // Copy to a malloc'd buffer so Go can take ownership.
    void* buf = malloc((size_t)len);
    if (buf == NULL) {
        CFRelease(pngData);
        return NULL;
    }
    CFDataGetBytes(pngData, CFRangeMake(0, len), (UInt8*)buf);
    CFRelease(pngData);

    *outLen = (size_t)len;
    return buf;
}
*/
import "C"

import (
	"encoding/base64"
	"fmt"
	"log"
	"unsafe"
)

// nativeCaptureScreenshot uses ScreenCaptureKit (macOS 14+) or CoreGraphics
// CGWindowListCreateImage (legacy) to capture the full screen directly in-process.
// This avoids spawning screencapture as a child process, which on macOS 26+
// triggers its own TCC permission dialog.
// Returns base64-encoded PNG data on success.
func nativeCaptureScreenshot() (string, error) {
	// Try ScreenCaptureKit first (macOS 14+, recommended by Apple)
	if b64, err := sckCaptureScreenshot(); err == nil && b64 != "" {
		return b64, nil
	} else if err != nil {
		log.Printf("[screenshot-native] SCK failed, trying legacy CGWindowListCreateImage: %v", err)
	}

	// Legacy fallback: CGWindowListCreateImage (deprecated in macOS 15, but still functional)
	var outLen C.size_t
	ptr := C.captureFullScreenPNG(&outLen)
	if ptr == nil {
		hasPermission := HasScreenRecordingPermission()
		return "", fmt.Errorf("CGWindowListCreateImage(CGRectInfinite) returned NULL — hasPermission=%v, outLen=%d",
			hasPermission, outLen)
	}
	defer C.free(unsafe.Pointer(ptr))

	pngBytes := C.GoBytes(unsafe.Pointer(ptr), C.int(outLen))
	if len(pngBytes) == 0 {
		return "", fmt.Errorf("CGWindowListCreateImage returned non-nil but 0 bytes")
	}

	log.Printf("[screenshot-native] legacy CGWindowListCreateImage succeeded: pngBytes=%d", len(pngBytes))
	b64 := base64.StdEncoding.EncodeToString(pngBytes)
	return b64, nil
}
