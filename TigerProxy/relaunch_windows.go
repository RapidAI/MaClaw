//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func startRelaunchProcess(exe string) error {
	// Wait until this PID is gone so the new process does not hit the
	// single-instance lock and exit immediately.
	script := fmt.Sprintf(
		`Start-Sleep -Seconds 1; $deadline = (Get-Date).AddSeconds(25); while ((Get-Date) -lt $deadline) { if (-not (Get-Process -Id %d -ErrorAction SilentlyContinue)) { break }; Start-Sleep -Milliseconds 300 }; Start-Process -FilePath %s`,
		os.Getpid(),
		powershellSingleQuote(exe),
	)
	cmd := exec.Command(windowsPowerShellExe(), "-NoProfile", "-WindowStyle", "Hidden", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
		CreationFlags: 0x00000200 | // CREATE_NEW_PROCESS_GROUP
			0x00000008 | // DETACHED_PROCESS
			0x01000000, // CREATE_BREAKAWAY_FROM_JOB
	}
	return cmd.Start()
}

func powershellSingleQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "''") + "'"
}

// windowsPowerShellExe returns an absolute PowerShell path. A GUI process often
// inherits a PATH without System32, and a bare "powershell.exe" then fails with
// "executable file not found in %PATH%".
func windowsPowerShellExe() string {
	var candidates []string
	seen := map[string]struct{}{}
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || !filepath.IsAbs(path) {
			return
		}
		key := strings.ToLower(filepath.Clean(path))
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		candidates = append(candidates, path)
	}
	for _, root := range []string{os.Getenv("SystemRoot"), os.Getenv("WINDIR"), `C:\Windows`} {
		add(filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"))
		// 32-bit processes are redirected out of System32. Sysnative is the real directory.
		add(filepath.Join(root, "Sysnative", "WindowsPowerShell", "v1.0", "powershell.exe"))
	}
	if prog := strings.TrimSpace(os.Getenv("ProgramFiles")); prog != "" {
		add(filepath.Join(prog, "PowerShell", "7", "pwsh.exe"))
	}
	add(`C:\Program Files\PowerShell\7\pwsh.exe`)
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate
		}
	}
	for _, name := range []string{"powershell.exe", "pwsh.exe"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if abs, err := filepath.Abs(path); err == nil && filepath.IsAbs(abs) {
			return abs
		}
		return path
	}
	return "powershell.exe"
}
