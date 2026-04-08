//go:build windows

package main

import (
	"log"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"
)

const wsRegPath = `SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System`
const wsRegKey = "DisableLockWorkstation"

// setWorkstationMode enables or disables workstation mode on Windows.
// When enabled:
//   - Prevents system sleep via SetThreadExecutionState (ES_SYSTEM_REQUIRED, no ES_DISPLAY_REQUIRED)
//   - Prevents screen lock by setting the DisableLockWorkstation registry value
//   - Display is allowed to turn off; the screen-dim timer handles that separately
//
// We avoid SendInput-based anti-lock because injecting mouse events resets the
// OS idle timer, which would prevent the display from dimming.
func (a *App) setWorkstationMode(enabled bool, screenDimMin int) {
	lockStart := time.Now()
	a.powerStateMutex.Lock()
	lockWait := time.Since(lockStart)
	if lockWait > 50*time.Millisecond {
		log.Printf("[power] setWorkstationMode:lock_wait=%s enabled=%t", lockWait, enabled)
	}
	defer a.powerStateMutex.Unlock()

	if a.workstationCancel != nil {
		a.workstationCancel()
		a.workstationCancel = nil
	}

	if !enabled {
		a.workstationRestoreExecState()
		setLockWorkstationPolicy(false)
		return
	}

	// Prevent sleep, allow display off.
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("SetThreadExecutionState")
	proc.Call(uintptr(esContinuous | esSystemRequired))

	// Disable lock screen via registry policy.
	setLockWorkstationPolicy(true)
}

// workstationRestoreExecState resets SetThreadExecutionState.
// If PowerOptimization is also active it will be re-applied by
// refreshPowerOptimizationStateFromConfig which runs right before us.
func (a *App) workstationRestoreExecState() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("SetThreadExecutionState")
	proc.Call(uintptr(esContinuous))
}

// setLockWorkstationPolicy writes or deletes the DisableLockWorkstation
// DWORD under HKCU\...\Policies\System.
// disable=true  → set value to 1 (lock screen disabled)
// disable=false → delete the value (restore default behavior)
func setLockWorkstationPolicy(disable bool) {
	if disable {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, wsRegPath, registry.SET_VALUE)
		if err != nil {
			return
		}
		defer k.Close()
		_ = k.SetDWordValue(wsRegKey, 1)
	} else {
		k, err := registry.OpenKey(registry.CURRENT_USER, wsRegPath, registry.SET_VALUE)
		if err != nil {
			return
		}
		defer k.Close()
		_ = k.DeleteValue(wsRegKey)
	}
}
