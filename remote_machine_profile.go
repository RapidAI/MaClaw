package main

import (
	"os"
	goruntime "runtime"
)

const (
	defaultRemoteHeartbeatSec = 60
	minRemoteHeartbeatSec     = 30
)

type remoteMachineProfile struct {
	Name           string
	Platform       string
	Hostname       string
	Arch           string
	AppVersion     string
	HeartbeatSec   int
	ActiveSessions int
}

func normalizeRemoteHeartbeatIntervalSec(value int) int {
	if value < minRemoteHeartbeatSec {
		return defaultRemoteHeartbeatSec
	}
	return value
}

func remoteAppVersion() string {
	return "1.0.0"
}

func (a *App) currentRemoteMachineProfile(heartbeatSec int, activeSessions int) remoteMachineProfile {
	name := "CodeClaw Desktop"
	hostname, err := os.Hostname()
	if err == nil && hostname != "" {
		name = hostname
	}
	return remoteMachineProfile{
		Name:           name,
		Platform:       normalizedRemotePlatform(),
		Hostname:       hostname,
		Arch:           goruntime.GOARCH,
		AppVersion:     remoteAppVersion(),
		HeartbeatSec:   normalizeRemoteHeartbeatIntervalSec(heartbeatSec),
		ActiveSessions: activeSessions,
	}
}
