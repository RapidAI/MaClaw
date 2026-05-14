//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

func launchInstaller(path string, wait bool) error {
	switch runtime.GOOS {
	case "darwin":
		cmd := exec.Command("installer", "-pkg", path, "-target", "CurrentUserHome")
		if wait {
			return cmd.Run()
		}
		return cmd.Start()
	case "linux":
		if err := os.Chmod(path, 0755); err != nil {
			return err
		}
		cmd := exec.Command(path)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if wait {
			return cmd.Run()
		}
		return cmd.Start()
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}
