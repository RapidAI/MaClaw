//go:build !windows && !darwin && !linux

package main

import "fmt"

func autoStartSupported() bool {
	return false
}

func isAutoStartEnabled() (bool, error) {
	return false, nil
}

func setAutoStartEnabled(bool) error {
	return fmt.Errorf("auto start is not supported on this platform")
}
