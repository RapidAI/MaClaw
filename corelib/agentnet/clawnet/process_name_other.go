//go:build !windows

package clawnet

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// findProcessByName returns the PID of a running process whose executable
// name matches the given name. Returns 0 if not found. It skips the current
// process so the caller never matches itself.
//
// On Linux it reads /proc directly (no external dependency). On other Unix
// systems (macOS, BSDs) it falls back to pgrep.
func findProcessByName(name string) int {
	if pid := findViaProc(name); pid != 0 {
		return pid
	}
	return findViaPgrep(name)
}

// findViaProc scans /proc/[pid]/exe symlinks (Linux only).
func findViaProc(name string) int {
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
		link, err := os.Readlink(filepath.Join("/proc", e.Name(), "exe"))
		if err != nil {
			continue
		}
		if filepath.Base(link) == name {
			return pid
		}
	}
	return 0
}

// findViaPgrep uses the pgrep command as a fallback (macOS, BSDs).
func findViaPgrep(name string) int {
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
