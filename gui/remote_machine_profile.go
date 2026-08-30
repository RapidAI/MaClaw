package main

import (
	"os"
	goruntime "runtime"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

type remoteMachineProfile struct {
	Name           string
	Nickname       string
	Platform       string
	Hostname       string
	Arch           string
	AppVersion     string
	HeartbeatSec   int
	ActiveSessions int
}

func normalizeRemoteHeartbeatIntervalSec(value int) int {
	return corelib.NormalizeRemoteHeartbeatIntervalSec(value)
}

func remoteAppVersion() string {
	if version != "" && version != "dev" {
		return version
	}
	// Keep the historical fallback for remote machine profiles and bug reports.
	// Update checks use hasLinkedReleaseVersion, so this compatibility value is
	// never used for release ordering.
	return "1.0.0"
}

func hasLinkedReleaseVersion() bool {
	_, ok := linkedReleaseVersion()
	return ok
}

// linkedReleaseVersion returns the linker-injected release version, if one is
// available. Keep this separate from remoteAppVersion, whose compatibility
// fallback is still used by telemetry and bug reports.
func linkedReleaseVersion() (string, bool) {
	value := strings.TrimSpace(version)
	if value == "" || strings.EqualFold(value, "dev") {
		return "", false
	}
	checkValue := strings.TrimPrefix(strings.TrimPrefix(strings.Split(value, " ")[0], "v"), "V")
	numeric, _ := splitVersionPreRelease(checkValue)
	parts := strings.Split(numeric, ".")
	if len(parts) < 2 {
		return "", false
	}
	for _, part := range parts {
		if part == "" {
			return "", false
		}
		for i := 0; i < len(part); i++ {
			if part[i] < '0' || part[i] > '9' {
				return "", false
			}
		}
	}
	return value, true
}

func (a *App) currentRemoteMachineProfile(heartbeatSec int, activeSessions int) remoteMachineProfile {
	name := "MaClaw Desktop"
	hostname, err := os.Hostname()
	if err == nil && hostname != "" {
		name = hostname
	}
	cfg, _ := a.LoadConfig()
	return remoteMachineProfile{
		Name:           name,
		Nickname:       cfg.RemoteNickname,
		Platform:       normalizedRemotePlatform(),
		Hostname:       hostname,
		Arch:           goruntime.GOARCH,
		AppVersion:     remoteAppVersion(),
		HeartbeatSec:   normalizeRemoteHeartbeatIntervalSec(heartbeatSec),
		ActiveSessions: activeSessions,
	}
}
