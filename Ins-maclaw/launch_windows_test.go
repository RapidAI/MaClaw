//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func TestIsElevationRequired(t *testing.T) {
	if !isElevationRequired(errorElevationRequired) {
		t.Fatalf("expected raw elevation error to be detected")
	}
	if !isElevationRequired(&os.PathError{Op: "fork/exec", Path: "setup.exe", Err: errorElevationRequired}) {
		t.Fatalf("expected wrapped CreateProcess elevation error to be detected")
	}
	if isElevationRequired(&exec.ExitError{}) {
		t.Fatalf("installer exit errors should not trigger elevation fallback")
	}
	if isElevationRequired(syscall.Errno(2)) {
		t.Fatalf("non-elevation errors must remain visible")
	}
}
