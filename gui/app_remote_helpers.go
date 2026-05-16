package main

import "strings"

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
