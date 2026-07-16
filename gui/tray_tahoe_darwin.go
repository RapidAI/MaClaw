//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
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
var tahoeCUPauseCallback func()
var tahoeCUResumeCallback func()
var tahoeCUStopCallback func()
var tahoeCUResetCallback func()

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

//export tahoeOnCUPauseClicked
func tahoeOnCUPauseClicked() {
	if tahoeCUPauseCallback != nil {
		go tahoeCUPauseCallback()
	}
}

//export tahoeOnCUResumeClicked
func tahoeOnCUResumeClicked() {
	if tahoeCUResumeCallback != nil {
		go tahoeCUResumeCallback()
	}
}

//export tahoeOnCUStopClicked
func tahoeOnCUStopClicked() {
	if tahoeCUStopCallback != nil {
		go tahoeCUStopCallback()
	}
}

//export tahoeOnCUResetClicked
func tahoeOnCUResetClicked() {
	if tahoeCUResetCallback != nil {
		go tahoeCUResetCallback()
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

// updateTahoeComputerUseMenu pushes CU submenu labels and enable flags.
func updateTahoeComputerUseMenu(menuTitle, status, pause, resume, stop, reset string,
	pauseOn, resumeOn, stopOn, resetOn bool) {
	cMenu := C.CString(menuTitle)
	cStatus := C.CString(status)
	cPause := C.CString(pause)
	cResume := C.CString(resume)
	cStop := C.CString(stop)
	cReset := C.CString(reset)
	pe, re, se, xe := C.int(0), C.int(0), C.int(0), C.int(0)
	if pauseOn {
		pe = 1
	}
	if resumeOn {
		re = 1
	}
	if stopOn {
		se = 1
	}
	if resetOn {
		xe = 1
	}
	C.TahoeUpdateComputerUseMenu(cMenu, cStatus, cPause, pe, cResume, re, cStop, se, cReset, xe)
	C.free(unsafe.Pointer(cMenu))
	C.free(unsafe.Pointer(cStatus))
	C.free(unsafe.Pointer(cPause))
	C.free(unsafe.Pointer(cResume))
	C.free(unsafe.Pointer(cStop))
	C.free(unsafe.Pointer(cReset))
}

// tahoeDockBounce bounces the dock icon to draw user attention.
func tahoeDockBounce() {
	C.TahoeDockBounce()
}
