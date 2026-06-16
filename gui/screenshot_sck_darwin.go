//go:build darwin && use_screencapturekit

package main

/*
#cgo darwin CFLAGS: -DDARWIN -Wno-deprecated-declarations -Wno-unguarded-availability-new
#cgo darwin LDFLAGS: -framework CoreGraphics -framework CoreFoundation -framework ImageIO -framework AppKit

#include <AvailabilityMacros.h>
#include <CoreGraphics/CoreGraphics.h>
#include <ImageIO/ImageIO.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <dispatch/dispatch.h>

// Forward-declare the ObjC function implemented in screenshot_sck_darwin.m
extern const void* SCKCaptureScreenshot(size_t* outLen, int* outErrCode);
*/
import "C"

import (
	"encoding/base64"
	"fmt"
	"log"
	"unsafe"
)

// sckCaptureScreenshot uses ScreenCaptureKit (macOS 14+) to capture the
// primary display. This is the Apple-recommended replacement for
// CGWindowListCreateImage on macOS 15+. The capture runs in-process so
// the TCC permission belongs to MaClaw.app — no child process TCC dialog.
//
// Returns base64-encoded PNG data on success.
func sckCaptureScreenshot() (string, error) {
	var outLen C.size_t
	var errCode C.int
	ptr := C.SCKCaptureScreenshot(&outLen, &errCode)
	if ptr == nil {
		switch int(errCode) {
		case 1:
			return "", fmt.Errorf("SCK: ScreenCaptureKit not available (requires macOS 14+)")
		case 2:
			return "", fmt.Errorf("SCK: failed to get shareable content (permission denied or no displays)")
		case 3:
			return "", fmt.Errorf("SCK: no displays found")
		case 4:
			return "", fmt.Errorf("SCK: captureImage failed (permission may not be granted)")
		case 5:
			return "", fmt.Errorf("SCK: PNG encoding failed")
		case 6:
			return "", fmt.Errorf("SCK: capture timed out (10s)")
		default:
			return "", fmt.Errorf("SCK: unknown error (code=%d)", int(errCode))
		}
	}
	defer C.free(unsafe.Pointer(ptr))

	pngBytes := C.GoBytes(unsafe.Pointer(ptr), C.int(outLen))
	if len(pngBytes) == 0 {
		return "", fmt.Errorf("SCK: returned non-nil but 0 bytes")
	}

	log.Printf("[screenshot-sck] ScreenCaptureKit capture succeeded: pngBytes=%d", len(pngBytes))
	b64 := base64.StdEncoding.EncodeToString(pngBytes)
	return b64, nil
}
