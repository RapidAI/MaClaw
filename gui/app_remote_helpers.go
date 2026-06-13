package main

import (
	"strings"
	"time"
)

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (a *App) hubClient() *RemoteHubClient {
	if a == nil || a.remoteSessions == nil {
		return nil
	}
	return a.remoteSessions.GetHubClient()
}

func (a *App) ensureHubClient() *RemoteHubClient {
	if a == nil {
		return nil
	}
	a.ensureRemoteInfra()
	if a.remoteSessions == nil {
		return nil
	}
	if client := a.remoteSessions.GetHubClient(); client != nil {
		return client
	}
	a.prepareHubClientSync()
	return a.remoteSessions.GetHubClient()
}

// awaitHubClient waits for the Hub client to become available (up to timeout).
// During application startup, hubClient() may return nil while asyncHubConnect
// is still running. This avoids reporting "not initialized" to the user when
// the system is simply still booting.
func (a *App) awaitHubClient(timeout time.Duration) *RemoteHubClient {
	if client := a.hubClient(); client != nil {
		return client
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		if client := a.hubClient(); client != nil {
			return client
		}
	}
	return nil
}
