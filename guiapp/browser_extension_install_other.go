//go:build !windows

package guiapp

import (
	"errors"
	"os/exec"
	"strings"
)

func companionAppPath(string) string { return "" }

func browserProcessRunningOS(image string) bool {
	out, err := exec.Command("pgrep", "-x", image).Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func writeHKCUString(view, path, name, value string) error {
	if companionRegHook != nil {
		return companionRegHook(companionRegWrite{View: view, Path: path, Name: name, Value: value})
	}
	return errors.New("HKCU registration is only written on Windows")
}

func deleteHKCUKey(view, path string) error {
	if companionRegHook != nil {
		return companionRegHook(companionRegWrite{Delete: true, View: view, Path: path})
	}
	return errors.New("HKCU registration is only written on Windows")
}
