package guiapp

import (
	"log"
	"math/rand"
	"time"
)

const (
	hubReconnectMinBackoff = 2 * time.Second
	hubReconnectMaxBackoff = 60 * time.Second
	hubReconnectWatchEvery = 30 * time.Second
)

// hubReconnectJitter spreads retries by ±20% so many desktops do not hit a
// restarting Hub in the same second.
func hubReconnectJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return hubReconnectMinBackoff
	}
	span := int64(d) / 5
	if span <= 0 {
		return d
	}
	return d - time.Duration(span) + time.Duration(rand.Int63n(2*span+1))
}

// onHubReconnected finishes what asyncHubConnect would have done when the
// first connect failed: the app leaves degraded mode and IM gateways start.
func (c *RemoteHubClient) onHubReconnected() {
	c.retryAttempts.Store(0)
	c.nextRetryAt.Store(0)
	log.Printf("[hub-client] reconnected")
	if c.app == nil {
		return
	}
	if c.degradedAtStart.Swap(false) {
		c.app.emitEvent("ai-assistant-init-progress", "ready")
		go c.app.syncIMGatewaysFromConfig()
	}
	c.app.emitHubConnectionState()
}

// ReconnectNow wakes a sleeping reconnect loop, or starts one, so a network
// change or a resumed laptop does not wait out the backoff.
func (c *RemoteHubClient) ReconnectNow(reason string) {
	if c == nil || c.IsConnected() || !c.allowReconnect.Load() {
		return
	}
	log.Printf("[hub-client] reconnect now: %s", reason)
	if c.reconnecting.Load() {
		select {
		case c.reconnectCh <- struct{}{}:
		default:
		}
		return
	}
	c.triggerReconnect()
}

// watchHubConnection restarts reconnecting if it ever stopped while the
// client should be online, and notices a sleep/resume by the wall clock
// jumping well past the tick.
func (c *RemoteHubClient) watchHubConnection(stop <-chan struct{}) {
	ticker := time.NewTicker(hubReconnectWatchEvery)
	defer ticker.Stop()
	last := time.Now()
	for {
		select {
		case <-stop:
			return
		case now := <-ticker.C:
			resumed := now.Sub(last) > 3*hubReconnectWatchEvery
			last = now
			if c.IsConnected() || !c.allowReconnect.Load() {
				continue
			}
			if resumed {
				c.ReconnectNow("resume")
			} else if !c.reconnecting.Load() {
				c.ReconnectNow("watchdog")
			}
		}
	}
}

// HubConnectionState is what the UI shows about the Hub link.
type HubConnectionState struct {
	// State is connected, connecting (retrying) or offline (not configured
	// or reconnect disabled).
	State       string `json:"state"`
	Attempts    int    `json:"attempts,omitempty"`
	NextRetryMs int64  `json:"next_retry_ms,omitempty"`
	LastError   string `json:"last_error,omitempty"`
}

func (c *RemoteHubClient) connectionState() HubConnectionState {
	if c == nil {
		return HubConnectionState{State: "offline"}
	}
	if c.IsConnected() {
		return HubConnectionState{State: "connected"}
	}
	st := HubConnectionState{State: "offline", LastError: c.LastError()}
	if c.allowReconnect.Load() {
		st.State = "connecting"
		st.Attempts = int(c.retryAttempts.Load())
		if at := c.nextRetryAt.Load(); at > 0 {
			if ms := at - time.Now().UnixMilli(); ms > 0 {
				st.NextRetryMs = ms
			}
		}
	}
	return st
}

// GetHubConnectionState is the Wails binding behind the connection badge.
func (a *App) GetHubConnectionState() HubConnectionState {
	if a == nil || a.remoteSessions == nil {
		return HubConnectionState{State: "offline"}
	}
	return a.remoteSessions.GetHubClient().connectionState()
}

// ReconnectHub is called by the UI when the network comes back, the window
// becomes visible again, or the person taps the badge.
func (a *App) ReconnectHub(reason string) HubConnectionState {
	if a == nil || a.remoteSessions == nil {
		return HubConnectionState{State: "offline"}
	}
	c := a.remoteSessions.GetHubClient()
	c.ReconnectNow("ui:" + reason)
	return c.connectionState()
}

func (a *App) emitHubConnectionState() {
	if a == nil {
		return
	}
	a.emitEvent("hub-connection-state")
}
