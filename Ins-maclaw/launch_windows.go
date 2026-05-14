//go:build windows

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

const (
	seeMaskNoCloseProcess  = 0x00000040
	swShowNormal           = 1
	waitInfinite           = 0xFFFFFFFF
	stillActive            = 259
	errorElevationRequired = syscall.Errno(740)
	errorCancelled         = syscall.Errno(1223)
)

type shellExecuteInfo struct {
	CbSize     uint32
	FMask      uint32
	Hwnd       uintptr
	Verb       *uint16
	File       *uint16
	Parameters *uint16
	Directory  *uint16
	Show       int32
	InstApp    uintptr
	IDList     uintptr
	Class      *uint16
	KeyClass   uintptr
	HotKey     uint32
	IconOrMon  uintptr
	Process    uintptr
}

func launchInstaller(path string, wait bool) error {
	if err := launchInstallerDirect(path, wait); err == nil {
		return nil
	} else if !isElevationRequired(err) {
		return err
	}
	return launchInstallerElevated(path, wait)
}

func launchInstallerDirect(path string, wait bool) error {
	cmd := exec.Command(path, "/S")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if wait {
		if err := cmd.Run(); err != nil {
			return withInstallerOutput(err, stderr.String())
		}
		return nil
	}
	if err := cmd.Start(); err != nil {
		return withInstallerOutput(err, stderr.String())
	}
	return nil
}

func launchInstallerElevated(path string, wait bool) error {
	shell32 := syscall.NewLazyDLL("shell32.dll")
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	shellExecuteEx := shell32.NewProc("ShellExecuteExW")
	waitForSingleObject := kernel32.NewProc("WaitForSingleObject")
	closeHandle := kernel32.NewProc("CloseHandle")
	getExitCodeProcess := kernel32.NewProc("GetExitCodeProcess")

	verb, _ := syscall.UTF16PtrFromString("runas")
	file, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	params, _ := syscall.UTF16PtrFromString("/S")
	info := shellExecuteInfo{
		CbSize:     uint32(unsafe.Sizeof(shellExecuteInfo{})),
		FMask:      seeMaskNoCloseProcess,
		Verb:       verb,
		File:       file,
		Parameters: params,
		Show:       swShowNormal,
	}
	ret, _, callErr := shellExecuteEx.Call(uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		if callErr == errorCancelled {
			return fmt.Errorf("installer elevation was cancelled")
		}
		if callErr != syscall.Errno(0) {
			return callErr
		}
		return fmt.Errorf("ShellExecuteEx failed")
	}
	if info.Process == 0 {
		return nil
	}
	defer closeHandle.Call(info.Process)
	if !wait {
		return nil
	}
	waitResult, _, waitErr := waitForSingleObject.Call(info.Process, waitInfinite)
	if waitResult == 0xFFFFFFFF {
		if waitErr != syscall.Errno(0) {
			return waitErr
		}
		return fmt.Errorf("waiting for installer failed")
	}
	var exitCode uint32
	ret, _, exitErr := getExitCodeProcess.Call(info.Process, uintptr(unsafe.Pointer(&exitCode)))
	if ret == 0 {
		if exitErr != syscall.Errno(0) {
			return exitErr
		}
		return fmt.Errorf("could not read installer exit code")
	}
	if exitCode == stillActive {
		return fmt.Errorf("installer is still running after wait completed")
	}
	if exitCode != 0 {
		return fmt.Errorf("installer exited with code %d", exitCode)
	}
	return nil
}

func isElevationRequired(err error) bool {
	if err == nil {
		return false
	}
	if err == errorElevationRequired {
		return true
	}
	if exitErr, ok := err.(*exec.Error); ok && exitErr.Err == errorElevationRequired {
		return true
	}
	if pathErr, ok := err.(*exec.ExitError); ok && pathErr.ProcessState != nil {
		return false
	}
	if pathErr, ok := err.(*os.PathError); ok {
		return pathErr.Err == errorElevationRequired
	}
	return false
}

func withInstallerOutput(err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, stderr)
}
