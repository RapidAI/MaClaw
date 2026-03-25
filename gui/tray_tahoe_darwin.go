//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa

#include <stdlib.h>
#include "tray_tahoe_darwin.h"
*/
import "C"

import (
	"log"
	"unsafe"
)

// tahoeShowCallback and tahoeQuitCallback are set by setupTahoeTray.
var tahoeShowCallback func()
var tahoeQuitCallback func()

//export tahoeOnShowClicked
func tahoeOnShowClicked() {
	if tahoeShowCallback != nil {
		go tahoeShowCallback()
	}
}

//export tahoeOnQuitClicked
func tahoeOnQuitClicked() {
	if tahoeQuitCallback != nil {
		go tahoeQuitCallback()
	}
}

// setupTahoeTray creates a minimal NSStatusItem tray without energye/systray.
func setupTahoeTray(iconBytes []byte, tooltip, showLabel, quitLabel string,
	onShow func(), onQuit func()) {

	log.Println("[tray] using native NSStatusItem (pure Cocoa, no energye/systray)")

	tahoeShowCallback = onShow
	tahoeQuitCallback = onQuit

	var iconPtr unsafe.Pointer
	var iconLen C.int
	if len(iconBytes) > 0 {
		iconPtr = unsafe.Pointer(&iconBytes[0])
		iconLen = C.int(len(iconBytes))
	}

	cTooltip := C.CString(tooltip)
	cShow := C.CString(showLabel)
	cQuit := C.CString(quitLabel)
	C.TahoeCreateTray(iconPtr, iconLen, cTooltip, cShow, cQuit)
	C.free(unsafe.Pointer(cTooltip))
	C.free(unsafe.Pointer(cShow))
	C.free(unsafe.Pointer(cQuit))
}

// updateTahoeTrayMenu updates the Tahoe tray labels.
func updateTahoeTrayMenu(tooltip, showLabel, quitLabel string) {
	cTooltip := C.CString(tooltip)
	cShow := C.CString(showLabel)
	cQuit := C.CString(quitLabel)
	C.TahoeUpdateMenu(cTooltip, cShow, cQuit)
	C.free(unsafe.Pointer(cTooltip))
	C.free(unsafe.Pointer(cShow))
	C.free(unsafe.Pointer(cQuit))
}

// tahoeDockBounce bounces the dock icon to draw user attention.
func tahoeDockBounce() {
	C.TahoeDockBounce()
}
