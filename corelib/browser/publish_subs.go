package browser

import "sync"

// Per-session CDP event subscribers (publish runs). The session's event
// pump hands every event to them without blocking.
var (
	sessionSubsMu sync.Mutex
	sessionSubs   = map[*BrowserAgentSession]map[chan CDPEvent]bool{}
)

func subscribeSessionEvents(s *BrowserAgentSession) (<-chan CDPEvent, func()) {
	ch := make(chan CDPEvent, 256)
	sessionSubsMu.Lock()
	if sessionSubs[s] == nil {
		sessionSubs[s] = map[chan CDPEvent]bool{}
	}
	sessionSubs[s][ch] = true
	sessionSubsMu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			sessionSubsMu.Lock()
			delete(sessionSubs[s], ch)
			if len(sessionSubs[s]) == 0 {
				delete(sessionSubs, s)
			}
			sessionSubsMu.Unlock()
			close(ch)
		})
	}
}

func publishSessionEvent(s *BrowserAgentSession, evt CDPEvent) {
	sessionSubsMu.Lock()
	defer sessionSubsMu.Unlock()
	for ch := range sessionSubs[s] {
		select {
		case ch <- evt:
		default:
		}
	}
}
