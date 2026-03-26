//go:build darwin

package main

/*
#cgo darwin CFLAGS: -DDARWIN
#cgo darwin LDFLAGS: -framework CoreGraphics -framework ImageIO -framework CoreFoundation

#include <CoreGraphics/CoreGraphics.h>
#include <ImageIO/ImageIO.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>

// captureFullScreenPNG captures the entire screen using CGWindowListCreateImage
// and encodes it as PNG into a malloc'd buffer. The caller must free() the
// returned pointer. Returns NULL on failure; *outLen is set to the data length.
static const void* captureFullScreenPNG(size_t* outLen) {
    *outLen = 0;

    CGImageRef image = CGWindowListCreateImage(
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
	"unsafe"
)

// nativeCaptureScreenshot uses CoreGraphics CGWindowListCreateImage to capture
// the full screen directly in-process. This avoids spawning screencapture as a
// child process, which on macOS 26+ triggers its own TCC permission dialog.
// Returns base64-encoded PNG data on success.
func nativeCaptureScreenshot() (string, error) {
	var outLen C.size_t
	ptr := C.captureFullScreenPNG(&outLen)
	if ptr == nil {
		return "", fmt.Errorf("CGWindowListCreateImage failed — screen recording permission may not be granted")
	}
	defer C.free(unsafe.Pointer(ptr))

	pngBytes := C.GoBytes(unsafe.Pointer(ptr), C.int(outLen))
	b64 := base64.StdEncoding.EncodeToString(pngBytes)
	return b64, nil
}
