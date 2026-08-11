//go:build windows

package main

import (
	"errors"
	"syscall"
	"testing"
)

func TestWorkstationModeUsesProcessSystemPowerRequest(t *testing.T) {
	originalCreate := workstationCreatePowerRequest
	originalSet := workstationSetPowerRequest
	originalClear := workstationClearPowerRequest
	originalClose := workstationClosePowerRequest
	t.Cleanup(func() {
		workstationCreatePowerRequest = originalCreate
		workstationSetPowerRequest = originalSet
		workstationClearPowerRequest = originalClear
		workstationClosePowerRequest = originalClose
	})

	const handle = uintptr(42)
	var createdReason string
	var setHandle, clearedHandle, closedHandle uintptr
	workstationCreatePowerRequest = func(reason string) (uintptr, error) {
		createdReason = reason
		return handle, nil
	}
	workstationSetPowerRequest = func(got uintptr) error {
		setHandle = got
		return nil
	}
	workstationClearPowerRequest = func(got uintptr) error {
		clearedHandle = got
		return nil
	}
	workstationClosePowerRequest = func(got uintptr) error {
		closedHandle = got
		return nil
	}

	app := &App{}
	app.setWorkstationMode(true, 3)
	if createdReason == "" || setHandle != handle {
		t.Fatalf("enabled workstation mode did not create and activate a system power request: reason=%q handle=%d", createdReason, setHandle)
	}
	if got := app.workstationPowerRequest; got != handle {
		t.Fatalf("power request handle = %d, want %d", got, handle)
	}

	app.setWorkstationMode(false, 0)
	if clearedHandle != handle || closedHandle != handle {
		t.Fatalf("disabled workstation mode must clear and close request: clear=%d close=%d want=%d", clearedHandle, closedHandle, handle)
	}
	if got := app.workstationPowerRequest; got != 0 {
		t.Fatalf("power request handle was not cleared: %d", got)
	}
}

func TestWorkstationModeClosesRequestWhenActivationFails(t *testing.T) {
	originalCreate := workstationCreatePowerRequest
	originalSet := workstationSetPowerRequest
	originalClose := workstationClosePowerRequest
	t.Cleanup(func() {
		workstationCreatePowerRequest = originalCreate
		workstationSetPowerRequest = originalSet
		workstationClosePowerRequest = originalClose
	})

	const handle = uintptr(8)
	var closedHandle uintptr
	workstationCreatePowerRequest = func(string) (uintptr, error) { return handle, nil }
	workstationSetPowerRequest = func(uintptr) error { return errors.New("access denied") }
	workstationClosePowerRequest = func(got uintptr) error { closedHandle = got; return nil }

	app := &App{}
	app.setWorkstationMode(true, 3)
	if got := app.workstationPowerRequest; got != 0 {
		t.Fatalf("failed request retained handle %d", got)
	}
	if closedHandle != handle {
		t.Fatalf("failed request handle was not closed: %d", closedHandle)
	}
}

func TestWorkstationModeLeavesNoRequestWhenCreationFails(t *testing.T) {
	originalCreate := workstationCreatePowerRequest
	t.Cleanup(func() {
		workstationCreatePowerRequest = originalCreate
	})

	workstationCreatePowerRequest = func(string) (uintptr, error) { return 0, errors.New("not supported") }

	app := &App{}
	app.setWorkstationMode(true, 3)
	if app.workstationPowerRequest != 0 {
		t.Fatalf("creation failure retained handle %d", app.workstationPowerRequest)
	}
}

func TestWindowsCallErrorOmitsPhantomErrnoZero(t *testing.T) {
	err := windowsCallError("PowerCreateRequest", syscall.Errno(0))
	if got, want := err.Error(), "PowerCreateRequest failed"; got != want {
		t.Fatalf("windowsCallError(errno 0) = %q, want %q", got, want)
	}
}

func TestWindowsCallErrorWrapsActualWindowsError(t *testing.T) {
	err := windowsCallError("PowerCreateRequest", syscall.Errno(5))
	if !errors.Is(err, syscall.Errno(5)) {
		t.Fatalf("windowsCallError did not wrap access-denied errno: %v", err)
	}
}

func TestWorkstationModeEnableIsIdempotent(t *testing.T) {
	originalCreate := workstationCreatePowerRequest
	originalSet := workstationSetPowerRequest
	originalClear := workstationClearPowerRequest
	originalClose := workstationClosePowerRequest
	t.Cleanup(func() {
		workstationCreatePowerRequest = originalCreate
		workstationSetPowerRequest = originalSet
		workstationClearPowerRequest = originalClear
		workstationClosePowerRequest = originalClose
	})

	const handle = uintptr(99)
	var createCalls, setCalls, clearCalls, closeCalls int
	workstationCreatePowerRequest = func(string) (uintptr, error) {
		createCalls++
		return handle, nil
	}
	workstationSetPowerRequest = func(uintptr) error { setCalls++; return nil }
	workstationClearPowerRequest = func(uintptr) error { clearCalls++; return nil }
	workstationClosePowerRequest = func(uintptr) error { closeCalls++; return nil }

	app := &App{}
	app.setWorkstationMode(true, 3)
	app.setWorkstationMode(true, 3)
	app.setWorkstationMode(false, 0)

	if createCalls != 1 || setCalls != 1 || clearCalls != 1 || closeCalls != 1 {
		t.Fatalf("power-request lifecycle calls: create=%d set=%d clear=%d close=%d; want one each", createCalls, setCalls, clearCalls, closeCalls)
	}
}

func TestWorkstationModeDisableIsIdempotent(t *testing.T) {
	originalClear := workstationClearPowerRequest
	originalClose := workstationClosePowerRequest
	t.Cleanup(func() {
		workstationClearPowerRequest = originalClear
		workstationClosePowerRequest = originalClose
	})

	var clearCalls, closeCalls int
	workstationClearPowerRequest = func(uintptr) error { clearCalls++; return nil }
	workstationClosePowerRequest = func(uintptr) error { closeCalls++; return nil }

	app := &App{workstationPowerRequest: 123}
	app.setWorkstationMode(false, 0)
	app.setWorkstationMode(false, 0)

	if clearCalls != 1 || closeCalls != 1 {
		t.Fatalf("power-request cleanup calls: clear=%d close=%d; want one each", clearCalls, closeCalls)
	}
}

func TestPowerOptimizationDoesNotReleaseWorkstationRequest(t *testing.T) {
	originalExecutionState := setPowerPolicyExecutionState
	originalClear := workstationClearPowerRequest
	originalClose := workstationClosePowerRequest
	t.Cleanup(func() {
		setPowerPolicyExecutionState = originalExecutionState
		workstationClearPowerRequest = originalClear
		workstationClosePowerRequest = originalClose
	})

	var executionStates []uintptr
	var clearCalls, closeCalls int
	setPowerPolicyExecutionState = func(flags uintptr) { executionStates = append(executionStates, flags) }
	workstationClearPowerRequest = func(uintptr) error { clearCalls++; return nil }
	workstationClosePowerRequest = func(uintptr) error { closeCalls++; return nil }

	app := &App{workstationPowerRequest: 321}
	app.reconcilePlatformPowerState(false, true)

	if app.workstationPowerRequest != 321 {
		t.Fatalf("power optimization refresh released workstation request %d", app.workstationPowerRequest)
	}
	if clearCalls != 0 || closeCalls != 0 {
		t.Fatalf("power optimization refresh cleaned workstation request: clear=%d close=%d", clearCalls, closeCalls)
	}
	if len(executionStates) != 1 || executionStates[0] != esContinuous {
		t.Fatalf("execution-state calls = %v, want [%d]", executionStates, esContinuous)
	}
}

func TestPlatformShutdownReleasesWorkstationRequest(t *testing.T) {
	originalExecutionState := setPowerPolicyExecutionState
	originalClear := workstationClearPowerRequest
	originalClose := workstationClosePowerRequest
	t.Cleanup(func() {
		setPowerPolicyExecutionState = originalExecutionState
		workstationClearPowerRequest = originalClear
		workstationClosePowerRequest = originalClose
	})

	var clearHandle, closeHandle uintptr
	var executionStates []uintptr
	setPowerPolicyExecutionState = func(flags uintptr) { executionStates = append(executionStates, flags) }
	workstationClearPowerRequest = func(handle uintptr) error { clearHandle = handle; return nil }
	workstationClosePowerRequest = func(handle uintptr) error { closeHandle = handle; return nil }

	app := &App{workstationPowerRequest: 654}
	app.platformShutdown()

	if clearHandle != 654 || closeHandle != 654 || app.workstationPowerRequest != 0 {
		t.Fatalf("platform shutdown did not release workstation request: clear=%d close=%d stored=%d", clearHandle, closeHandle, app.workstationPowerRequest)
	}
	if len(executionStates) != 1 || executionStates[0] != esContinuous {
		t.Fatalf("execution-state calls = %v, want [%d]", executionStates, esContinuous)
	}
}
