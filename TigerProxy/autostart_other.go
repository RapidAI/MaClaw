//go:build !windows

package main

import "fmt"

func autoStartSupported() bool {
	return false
}

func isAutoStartEnabled() (bool, error) {
	return false, nil
}

func setAutoStartEnabled(bool) error {
	return fmt.Errorf("auto start is only supported on Windows")
}
