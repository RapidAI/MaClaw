//go:build windows

package main

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	autoStartRegistryPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	autoStartRegistryName = "TigerProxy"
)

func autoStartSupported() bool {
	return true
}

func isAutoStartEnabled() (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, autoStartRegistryPath, registry.QUERY_VALUE)
	if err == registry.ErrNotExist {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer key.Close()

	value, _, err := key.GetStringValue(autoStartRegistryName)
	if err == registry.ErrNotExist {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return autoStartCommandMatchesExecutable(value), nil
}

func setAutoStartEnabled(enabled bool) error {
	if !enabled {
		key, err := registry.OpenKey(registry.CURRENT_USER, autoStartRegistryPath, registry.SET_VALUE)
		if err == registry.ErrNotExist {
			return nil
		}
		if err != nil {
			return err
		}
		defer key.Close()
		if err := key.DeleteValue(autoStartRegistryName); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}

	key, _, err := registry.CreateKey(registry.CURRENT_USER, autoStartRegistryPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	command, err := autoStartCommand()
	if err != nil {
		return err
	}
	return key.SetStringValue(autoStartRegistryName, command)
}

func autoStartCommand() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe = strings.TrimSpace(exe)
	if exe == "" {
		return "", fmt.Errorf("executable path is empty")
	}
	return quoteWindowsCommandPath(exe) + " --hidden", nil
}

func autoStartCommandMatchesExecutable(command string) bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.EqualFold(unquoteWindowsCommandPath(command), strings.TrimSpace(exe)) &&
		autoStartCommandHasHiddenArg(command)
}

func autoStartCommandHasHiddenArg(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	if strings.HasPrefix(command, `"`) {
		rest := command[1:]
		if idx := strings.Index(rest, `"`); idx >= 0 {
			return hasStartHiddenArg(strings.Fields(rest[idx+1:]))
		}
		return false
	}
	if idx := strings.IndexAny(command, " \t"); idx >= 0 {
		return hasStartHiddenArg(strings.Fields(command[idx+1:]))
	}
	return false
}

func quoteWindowsCommandPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.Trim(path, `"`)
	return `"` + strings.ReplaceAll(path, `"`, `\"`) + `"`
}

func unquoteWindowsCommandPath(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	if strings.HasPrefix(command, `"`) {
		rest := command[1:]
		if idx := strings.Index(rest, `"`); idx >= 0 {
			return rest[:idx]
		}
		return strings.TrimSuffix(rest, `"`)
	}
	if idx := strings.IndexAny(command, " \t"); idx >= 0 {
		return command[:idx]
	}
	return command
}
