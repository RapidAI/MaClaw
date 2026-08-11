//go:build windows

package main

import (
	"fmt"
	"log"
	"syscall"
	"time"
	"unsafe"
)

const (
	powerRequestContextVersion      = 0
	powerRequestContextSimpleString = 0x1
	powerRequestSystemRequired      = 0
)

// powerRequestReasonContext is the simple-string form of REASON_CONTEXT used
// by PowerCreateRequest.
type powerRequestReasonContext struct {
	Version            uint32
	Flags              uint32
	SimpleReasonString *uint16
}

var (
	workstationKernel32           = syscall.NewLazyDLL("kernel32.dll")
	procPowerCreateRequest        = workstationKernel32.NewProc("PowerCreateRequest")
	procPowerSetRequest           = workstationKernel32.NewProc("PowerSetRequest")
	procPowerClearRequest         = workstationKernel32.NewProc("PowerClearRequest")
	procWorkstationCloseHandle    = workstationKernel32.NewProc("CloseHandle")
	workstationCreatePowerRequest = createWorkstationPowerRequest
	workstationSetPowerRequest    = setWorkstationSystemRequired
	workstationClearPowerRequest  = clearWorkstationSystemRequired
	workstationClosePowerRequest  = closeWorkstationPowerRequest
)

// setWorkstationMode enables or disables workstation mode on Windows.
// When enabled:
//   - Prevents system sleep with a process-level Power Request (no display request)
//   - Display is allowed to turn off; the screen-dim timer handles that separately
//
// A Power Request is process-scoped. This is important for Go applications:
// SetThreadExecutionState is thread-scoped and its sleep requirement can be
// lost when the runtime retires or moves away from the OS thread that set it.
func (a *App) setWorkstationMode(enabled bool, screenDimMin int) {
	lockStart := time.Now()
	a.powerStateMutex.Lock()
	lockWait := time.Since(lockStart)
	if lockWait > 50*time.Millisecond {
		log.Printf("[power] setWorkstationMode:lock_wait=%s enabled=%t", lockWait, enabled)
	}
	defer a.powerStateMutex.Unlock()
	a.setWorkstationModeLocked(enabled)
}

func (a *App) setWorkstationModeLocked(enabled bool) {
	if a.workstationCancel != nil {
		a.workstationCancel()
		a.workstationCancel = nil
	}

	if !enabled {
		a.releaseWorkstationPowerRequest()
		return
	}

	if a.workstationPowerRequest == 0 {
		handle, err := workstationCreatePowerRequest("MaClaw workstation mode keeps automated work running while the display is off")
		if err != nil {
			log.Printf("[power] workstation mode: unable to create system-required power request: %v", err)
			return
		} else if err := workstationSetPowerRequest(handle); err != nil {
			log.Printf("[power] workstation mode: unable to activate system-required power request: %v", err)
			if closeErr := workstationClosePowerRequest(handle); closeErr != nil {
				log.Printf("[power] workstation mode: unable to close failed power request: %v", closeErr)
			}
			return
		} else {
			a.workstationPowerRequest = handle
		}
	}
}

// reconcileWindowsPowerState applies mutually independent power features in
// one critical section. A workstation Power Request remains active when the
// screenshot-oriented power optimization turns itself off, so its display-off
// contract cannot accidentally re-enable system sleep.
func (a *App) reconcilePlatformPowerState(powerOptimization, workstationMode bool) {
	a.powerStateMutex.Lock()
	defer a.powerStateMutex.Unlock()
	a.setPowerOptimizationEnabledLocked(powerOptimization)
	a.setWorkstationModeLocked(workstationMode)
}

func (a *App) releaseWorkstationPowerRequest() {
	handle := a.workstationPowerRequest
	if handle == 0 {
		return
	}
	a.workstationPowerRequest = 0
	if err := workstationClearPowerRequest(handle); err != nil {
		log.Printf("[power] workstation mode: unable to clear system-required power request: %v", err)
	}
	if err := workstationClosePowerRequest(handle); err != nil {
		log.Printf("[power] workstation mode: unable to close power request: %v", err)
	}
}

func createWorkstationPowerRequest(reason string) (uintptr, error) {
	reasonPtr, err := syscall.UTF16PtrFromString(reason)
	if err != nil {
		return 0, err
	}
	context := powerRequestReasonContext{
		Version:            powerRequestContextVersion,
		Flags:              powerRequestContextSimpleString,
		SimpleReasonString: reasonPtr,
	}
	handle, _, callErr := procPowerCreateRequest.Call(uintptr(unsafe.Pointer(&context)))
	if handle == 0 {
		return 0, windowsCallError("PowerCreateRequest", callErr)
	}
	return handle, nil
}

func setWorkstationSystemRequired(handle uintptr) error {
	result, _, callErr := procPowerSetRequest.Call(handle, powerRequestSystemRequired)
	if result == 0 {
		return windowsCallError("PowerSetRequest(PowerRequestSystemRequired)", callErr)
	}
	return nil
}

func clearWorkstationSystemRequired(handle uintptr) error {
	result, _, callErr := procPowerClearRequest.Call(handle, powerRequestSystemRequired)
	if result == 0 {
		return windowsCallError("PowerClearRequest(PowerRequestSystemRequired)", callErr)
	}
	return nil
}

func closeWorkstationPowerRequest(handle uintptr) error {
	result, _, callErr := procWorkstationCloseHandle.Call(handle)
	if result == 0 {
		return windowsCallError("CloseHandle", callErr)
	}
	return nil
}

func windowsCallError(operation string, callErr error) error {
	if callErr == nil || callErr == syscall.Errno(0) {
		return fmt.Errorf("%s failed", operation)
	}
	return fmt.Errorf("%s: %w", operation, callErr)
}

// disable=true  → set value to 1 (lock screen disabled)
// disable=false → delete the value (restore default behavior)
