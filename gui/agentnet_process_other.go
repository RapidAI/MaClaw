//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// clawnetFindProcessByName returns the PID of a running process whose
// executable name matches the given name. Returns 0 if not found. It skips
// the current process so the caller never matches itself.
//
// On Linux it reads /proc directly. On other Unix systems (macOS, BSDs) it
// falls back to pgrep.
func clawnetFindProcessByName(name string) int {
	if pid := guiFindViaProc(name); pid != 0 {
		return pid
	}
	return guiFindViaPgrep(name)
}

func guiFindViaProc(name string) int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	self := os.Getpid()
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		exe, err := os.Readlink(filepath.Join("/proc", e.Name(), "exe"))
		if err != nil {
			continue
		}
		if filepath.Base(exe) == name {
			return pid
		}
	}
	return 0
}

func guiFindViaPgrep(name string) int {
	out, err := exec.Command("pgrep", "-x", name).Output()
	if err != nil {
		return 0
	}
	self := os.Getpid()
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pid, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || pid == self {
			continue
		}
		return pid
	}
	return 0
}
