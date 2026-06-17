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

// encodeCGImageToPNG encodes a CGImageRef to PNG in a malloc'd buffer.
// Caller must free the returned pointer. Returns NULL on failure.
static const void* encodeCGImageToPNG(CGImageRef image, size_t* outLen) {
    CFMutableDataRef pngData = CFDataCreateMutable(kCFAllocatorDefault, 0);
    if (!pngData) return NULL;
    CGImageDestinationRef dest = CGImageDestinationCreateWithData(pngData, CFSTR("public.png"), 1, NULL);
    if (!dest) { CFRelease(pngData); return NULL; }
    CGImageDestinationAddImage(dest, image, NULL);
    bool ok = CGImageDestinationFinalize(dest);
    CFRelease(dest);
    if (!ok) { CFRelease(pngData); return NULL; }
    CFIndex len = CFDataGetLength(pngData);
    if (len <= 0) { CFRelease(pngData); return NULL; }
    void* buf = malloc((size_t)len);
    if (!buf) { CFRelease(pngData); return NULL; }
    CFDataGetBytes(pngData, CFRangeMake(0, len), (UInt8*)buf);
    CFRelease(pngData);
    *outLen = (size_t)len;
    return buf;
}

// captureFullScreenPNG captures all online displays individually and stitches
// them horizontally into a single PNG image. Each display is captured using its
// own bounds to avoid CGRectInfinite coordinate issues with rotated displays.
// The caller must free() the returned pointer. Returns NULL on failure.
static const void* captureFullScreenPNG(size_t* outLen) {
    *outLen = 0;

    CGWindowListCreateImageFn fn = (CGWindowListCreateImageFn)dlsym(RTLD_DEFAULT, "CGWindowListCreateImage");
    if (!fn) return NULL;

    // Enumerate online displays.
    CGDirectDisplayID displays[16];
    uint32_t displayCount = 0;
    if (CGGetOnlineDisplayList(16, displays, &displayCount) != kCGErrorSuccess || displayCount == 0) {
        return NULL;
    }

    // Single display: capture and encode directly.
    if (displayCount == 1) {
        CGRect bounds = CGDisplayBounds(displays[0]);
        CGImageRef img = fn(bounds, kCGWindowListOptionOnScreenOnly, kCGNullWindowID, kCGWindowImageDefault);
        if (!img) return NULL;
        if (CGImageGetWidth(img) == 0) { CGImageRelease(img); return NULL; }
        const void* result = encodeCGImageToPNG(img, outLen);
        CGImageRelease(img);
        return result;
    }

    // Multiple displays: capture each individually.
    CGImageRef images[16];
    size_t widths[16], heights[16];
    uint32_t validCount = 0;
    size_t totalW = 0, maxH = 0;

    for (uint32_t i = 0; i < displayCount; i++) {
        CGRect bounds = CGDisplayBounds(displays[i]);
        CGImageRef img = fn(bounds, kCGWindowListOptionOnScreenOnly, kCGNullWindowID, kCGWindowImageDefault);
        if (!img) continue;
        size_t w = CGImageGetWidth(img);
        size_t h = CGImageGetHeight(img);
        if (w == 0 || h == 0) { CGImageRelease(img); continue; }
        images[validCount] = img;
        widths[validCount] = w;
        heights[validCount] = h;
        totalW += w;
        if (h > maxH) maxH = h;
        validCount++;
    }
    if (validCount == 0) return NULL;

    // Sanity check: cap total bitmap at ~256MB (64K x 1K pixels @ 4 bytes/px).
    // Prevents absurd memory allocation with many high-res displays.
    if (totalW > 65536 || maxH > 65536 || (totalW * maxH) > (256 * 1024 * 1024 / 4)) {
        for (uint32_t i = 0; i < validCount; i++) CGImageRelease(images[i]);
        // Fall back to just the first (primary) display.
        CGRect bounds = CGDisplayBounds(displays[0]);
        CGImageRef img = fn(bounds, kCGWindowListOptionOnScreenOnly, kCGNullWindowID, kCGWindowImageDefault);
        if (!img) return NULL;
        const void* result = encodeCGImageToPNG(img, outLen);
        CGImageRelease(img);
        return result;
    }

    // Create bitmap context for stitching.
    CGColorSpaceRef cs = CGColorSpaceCreateDeviceRGB();
    CGContextRef ctx = CGBitmapContextCreate(NULL, totalW, maxH, 8, totalW * 4,
        cs, kCGImageAlphaPremultipliedFirst | kCGBitmapByteOrder32Host);
    CGColorSpaceRelease(cs);
    if (!ctx) {
        for (uint32_t i = 0; i < validCount; i++) CGImageRelease(images[i]);
        return NULL;
    }

    // Draw each display side by side, vertically centered.
    size_t xOff = 0;
    for (uint32_t i = 0; i < validCount; i++) {
        CGFloat yOff = (CGFloat)(maxH - heights[i]) / 2.0;
        CGContextDrawImage(ctx, CGRectMake(xOff, yOff, widths[i], heights[i]), images[i]);
        xOff += widths[i];
        CGImageRelease(images[i]);
    }

    CGImageRef stitched = CGBitmapContextCreateImage(ctx);
    CGContextRelease(ctx);
    if (!stitched) return NULL;

    const void* result = encodeCGImageToPNG(stitched, outLen);
    CGImageRelease(stitched);
    return result;
}
*/
import "C"

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"
	"unsafe"
)

// nativeCaptureScreenshot captures the screen directly in-process on macOS.
//
// Strategy on macOS 26+:
//   1. Try screencapture CLI first — it's Apple-signed with full TCC privileges,
//      can see all windows. Does NOT trigger a TCC dialog (tested on macOS 26.5).
//   2. If screencapture fails, fall back to CGWindowListCreateImage per-display
//      stitch (limited to own windows on self-signed apps).
//
// Returns base64-encoded PNG data on success.
func nativeCaptureScreenshot() (string, error) {
	// Try screencapture first — Apple-signed, has full screen recording privilege.
	// Does not trigger TCC dialog because EnsureScreenRecordingPermission is NOT called.
	b64, err := captureViaScreencapture()
	if err == nil && b64 != "" {
		return b64, nil
	}
	if err != nil {
		log.Printf("[screenshot-native] screencapture failed, trying CGWindowListCreateImage: %v", err)
	}

	// Fallback: CGWindowListCreateImage per-display + stitch.
	// On self-signed apps this only captures own windows + desktop.
	var outLen C.size_t
	ptr := C.captureFullScreenPNG(&outLen)
	if ptr == nil {
		hasPermission := HasScreenRecordingPermission()
		return "", fmt.Errorf("CGWindowListCreateImage failed — hasPermission=%v", hasPermission)
	}
	defer C.free(unsafe.Pointer(ptr))

	pngBytes := C.GoBytes(unsafe.Pointer(ptr), C.int(outLen))
	if len(pngBytes) == 0 {
		return "", fmt.Errorf("CGWindowListCreateImage returned 0 bytes")
	}

	log.Printf("[screenshot-native] CGWindowListCreateImage capture succeeded: pngBytes=%d", len(pngBytes))
	b64 = base64.StdEncoding.EncodeToString(pngBytes)
	return b64, nil
}

// captureViaScreencapture runs /usr/sbin/screencapture and returns base64 PNG.
// screencapture is Apple-signed and has full TCC screen recording privilege
// independent of the calling app's TCC status.
func captureViaScreencapture() (string, error) {
	tmpFile := fmt.Sprintf("/tmp/maclaw_sc_%d.png", time.Now().UnixNano())
	defer os.Remove(tmpFile)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/usr/sbin/screencapture", "-x", tmpFile)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("screencapture failed: %w", err)
	}

	data, err := os.ReadFile(tmpFile)
	if err != nil {
		return "", fmt.Errorf("read screenshot file: %w", err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("screencapture produced empty file")
	}

	return base64.StdEncoding.EncodeToString(data), nil
}
