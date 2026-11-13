package guiapp

import (
	"log"
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
	if a == nil {
		return nil
	}
	var sessions *RemoteSessionManager
	if a.remoteInfraMu.TryRLock() {
		sessions = a.remoteSessions
		a.remoteInfraMu.RUnlock()
	} else {
		// A first-time ensureRemoteInfra holds remoteInfraMu while running
		// initCoreInfra. hubClient is reachable inside that critical section
		// (initCoreInfra → LoadConfig → resetPathBoundStateForDataDirChange →
		// sharedMemoryStoreHandlers), where a blocking RLock self-deadlocks:
		// RWMutex is not reentrant. In that case the field read is on the
		// goroutine performing the writes, so it is safe; concurrent external
		// callers simply observe the pre-init value, which hubClient already
		// tolerates by returning nil.
		sessions = a.remoteSessions
	}
	if sessions == nil {
		return nil
	}
	return sessions.GetHubClient()
}

func (a *App) ensureHubClient() *RemoteHubClient {
	if a == nil {
		return nil
	}
	a.ensureRemoteInfra()
	sessions := a.remoteSessionManagerIfReady()
	if sessions == nil {
		return nil
	}
	if client := sessions.GetHubClient(); client != nil {
		return client
	}
	log.Printf("[AI assistant] hubClient missing; preparing local/degraded Hub client")
	a.prepareHubClientSync()
	return sessions.GetHubClient()
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
