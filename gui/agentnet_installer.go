package main

// agentnet_installer.go — Auto-install anet binary via npm registry + GitHub fallback.
// Delegates to corelib/agentnet.Download() which implements the three-tier fallback:
//   1. npmmirror (China) — download npm tgz, extract binary
//   2. npm official      — same approach
//   3. GitHub Releases   — direct binary download
//
// No external tools (curl/sh/powershell) required.

import (
	"github.com/RapidAI/CodeClaw/corelib/agentnet"
)

// anetLocalBinaryName returns "anet.exe" on Windows, "anet" otherwise.
func anetLocalBinaryName() string {
	return agentnet.LocalBinaryName()
}

// anetInstallDir returns the expected install directory for the anet binary.
func anetInstallDir() (string, error) {
	return agentnet.InstallDir()
}

// anetManualBinaryPath checks if the anet binary exists in the install dir.
func anetManualBinaryPath() (string, bool) {
	return agentnet.ManualBinaryPath()
}

// DownloadAnet installs the anet binary using the three-tier npm fallback chain.
// Returns the path to the installed binary.
func DownloadAnet(emitProgress func(stage string, pct int, msg string)) (string, error) {
	return agentnet.Download(emitProgress)
}
