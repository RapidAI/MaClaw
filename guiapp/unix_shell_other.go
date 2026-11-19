//go:build !windows

package guiapp

import "os/exec"

// resolveUnixShell finds bash, then sh. Windows has its own probe that
// rejects the WSL launcher.
func resolveUnixShell() (string, error) {
	if path, err := exec.LookPath("bash"); err == nil {
		return path, nil
	}
	return exec.LookPath("sh")
}
