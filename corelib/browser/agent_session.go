package browser

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

const browserAgentConsoleLimit = 12
const browserAgentNetworkLimit = 12
const browserAgentErrorLimit = 8
const browserAgentSnapshotLimit = 8

var (
	browserAgentMu       sync.Mutex
	browserAgentSessions = map[string]*BrowserAgentSession{}
)

// StartAgentSession creates or reuses a long-lived browser agent session.
// The mode parameter controls the connection strategy:
//   - SessionModeAuto (default): connect user Chrome first, fallback to user-profile launch
//   - SessionModeConnectUser: only connect to user's running Chrome (no launch)
//   - SessionModeIsolated: launch with isolated debug profile (no user data)
func StartAgentSession(addr string, policy BrowserPolicy, reuseExisting bool, mode SessionMode) (*BrowserAgentSession, error) {
	browserAgentMu.Lock()
	defer browserAgentMu.Unlock()

	if reuseExisting {
		for _, sess := range browserAgentSessions {
			if sess == nil {
				continue
			}
			if sess.session != nil && sess.session.client != nil && sess.session.client.IsAlive() {
				sess.Policy = policy
				sess.UpdatedAt = time.Now()
				return sess, nil
			}
		}
	}

	// Normalize mode.
	if mode == "" {
		mode = SessionModeAuto
	}

	cdpAddr := strings.TrimSpace(addr)
	if cdpAddr == "" {
		var err error
		switch mode {
		case SessionModeConnectUser:
			cdpAddr, err = ConnectUserChrome()
		case SessionModeIsolated:
			cdpAddr, err = DiscoverOrLaunch()
		default: // SessionModeAuto
			cdpAddr, err = DiscoverOrLaunchUserProfile()
		}
		if err != nil {
			return nil, err
		}
	}

	session, err := connectToAddr(cdpAddr)
	if err != nil {
		return nil, err
	}
	targetID := activeTargetID(session)
	now := time.Now()
	agentSession := &BrowserAgentSession{
		ID:             "browser-session-" + generateID(),
		Addr:           cdpAddr,
		TargetID:       targetID,
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
		Policy:         policy,
		Mode:           mode,
		session:        session,
		stopCh:         make(chan struct{}),
		snapshots:      map[string]*BrowserSnapshot{},
		recentConsole:  []string{},
		recentNetwork:  []string{},
		recentErrors:   []string{},
	}
	agentSession.startEventPump()
	// Start inactivity timeout for connect_user/auto modes (not isolated).
	if mode != SessionModeIsolated {
		agentSession.startInactivityTimer()
	}
	// Audit log for user-chrome connections.
	if mode != SessionModeIsolated {
		GetAuditLogger().LogConnect(agentSession.ID, cdpAddr)
	}
	browserAgentSessions[agentSession.ID] = agentSession
	return agentSession, nil
}

// GetAgentSession returns a previously started agent session.
func GetAgentSession(sessionID string) (*BrowserAgentSession, error) {
	browserAgentMu.Lock()
	defer browserAgentMu.Unlock()
	if sessionID == "" {
		return nil, fmt.Errorf("missing browser session id")
	}
	sess, ok := browserAgentSessions[sessionID]
	if !ok || sess == nil {
		return nil, fmt.Errorf("browser session not found: %s", sessionID)
	}
	if sess.session == nil || sess.session.client == nil || !sess.session.client.IsAlive() {
		reconnected, err := connectToAddr(sess.Addr)
		if err != nil {
			return nil, err
		}
		sess.session = reconnected
		sess.TargetID = activeTargetID(reconnected)
		sess.UpdatedAt = time.Now()
		sess.startEventPump()
	}
	// Touch activity on every access to reset the inactivity timer.
	sess.TouchActivity()
	return sess, nil
}

// StopAgentSession closes and removes a browser agent session.
// In connect_user/auto mode, closeBrowser is ignored — we never kill the user's Chrome.
func StopAgentSession(sessionID string, closeBrowser bool) error {
	browserAgentMu.Lock()
	sess, ok := browserAgentSessions[sessionID]
	if ok {
		delete(browserAgentSessions, sessionID)
	}
	browserAgentMu.Unlock()
	if !ok || sess == nil {
		return fmt.Errorf("browser session not found: %s", sessionID)
	}
	if sess.stopCh != nil {
		close(sess.stopCh)
		sess.stopCh = nil
	}
	// Audit log for user-chrome disconnections (skip if already logged by inactivity timer).
	if sess.Mode != SessionModeIsolated && !sess.timedOut {
		GetAuditLogger().LogDisconnect(sessionID, "session_stop")
	}
	if sess.session != nil && sess.session.client != nil {
		_ = sess.session.client.Close()
	}
	// Only kill the browser process in isolated mode.
	// In connect_user/auto mode, the browser belongs to the user — never kill it.
	if closeBrowser && sess.Mode == SessionModeIsolated {
		killManagedBrowser()
	}
	return nil
}

// ListAgentSessions returns all known browser sessions.
func ListAgentSessions() []*BrowserAgentSession {
	browserAgentMu.Lock()
	defer browserAgentMu.Unlock()
	out := make([]*BrowserAgentSession, 0, len(browserAgentSessions))
	for _, sess := range browserAgentSessions {
		if sess != nil {
			out = append(out, sess)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func activeTargetID(session *Session) string {
	if session == nil {
		return ""
	}
	pages, err := session.ListPages()
	if err != nil {
		return ""
	}
	for _, page := range pages {
		if page.Type == "page" && page.WebSocketDebugURL != "" {
			return page.ID
		}
	}
	if len(pages) > 0 {
		return pages[0].ID
	}
	return ""
}

func (s *BrowserAgentSession) startEventPump() {
	if s == nil || s.session == nil || s.session.client == nil {
		return
	}
	events := s.session.client.Events()
	stopCh := s.stopCh
	go func() {
		for {
			select {
			case <-stopCh:
				return
			case evt, ok := <-events:
				if !ok {
					return
				}
				s.handleCDPEvent(evt)
			}
		}
	}()
}

// connectUserInactivityTimeout is the duration after which a connect_user/auto
// session is automatically disconnected if no tool operations occur.
const connectUserInactivityTimeout = 30 * time.Minute

// startInactivityTimer starts a background goroutine that checks for inactivity
// and auto-disconnects the session after connectUserInactivityTimeout.
// Only used for connect_user/auto modes (user's Chrome should not be held indefinitely).
func (s *BrowserAgentSession) startInactivityTimer() {
	if s == nil {
		return
	}
	stopCh := s.stopCh
	sessionID := s.ID
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				s.mu.RLock()
				lastActivity := s.LastActivityAt
				s.mu.RUnlock()
				if time.Since(lastActivity) >= connectUserInactivityTimeout {
					log.Printf("[browser] 会话 %s 超过 30 分钟无操作，自动断开", sessionID)
					// Mark as timed-out so StopAgentSession skips duplicate audit log.
					s.mu.Lock()
					s.timedOut = true
					s.mu.Unlock()
					GetAuditLogger().LogDisconnect(sessionID, "inactivity_timeout")
					// Use a goroutine to avoid deadlock (StopAgentSession acquires browserAgentMu).
					go StopAgentSession(sessionID, false)
					return
				}
			}
		}
	}()
}

// TouchActivity updates the last activity timestamp. Called on every
// GetAgentSession access to reset the inactivity timer.
func (s *BrowserAgentSession) TouchActivity() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.LastActivityAt = time.Now()
	s.mu.Unlock()
}

func (s *BrowserAgentSession) handleCDPEvent(evt CDPEvent) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.UpdatedAt = time.Now()
	switch evt.Method {
	case "Runtime.consoleAPICalled":
		var payload struct {
			Args []struct {
				Value interface{} `json:"value"`
			} `json:"args"`
		}
		if json.Unmarshal(evt.Params, &payload) == nil {
			parts := make([]string, 0, len(payload.Args))
			for _, arg := range payload.Args {
				if arg.Value == nil {
					continue
				}
				parts = append(parts, fmt.Sprint(arg.Value))
			}
			if len(parts) > 0 {
				line := strings.Join(parts, " ")
				now := time.Now().UnixMilli()
				s.recentConsole = appendCapped(s.recentConsole, line, browserAgentConsoleLimit)
				s.recentTrace = appendCappedTrace(s.recentTrace, BrowserTraceEvent{Kind: "console", Summary: line, CreatedAt: now}, browserAgentConsoleLimit)
				s.recentTimeline = appendCappedTimeline(s.recentTimeline, BrowserTimelineSlice{Kind: "console", Summary: line, CreatedAt: now}, browserAgentConsoleLimit)
				s.recentConsoleEntries = appendCappedConsoleEntries(s.recentConsoleEntries, BrowserConsoleEvent{Type: "console", Level: "info", Text: line, CreatedAt: now}, browserAgentConsoleLimit)
			}
		}
	case "Runtime.exceptionThrown":
		line := compactJSONString(evt.Params)
		now := time.Now().UnixMilli()
		s.recentErrors = appendCapped(s.recentErrors, line, browserAgentErrorLimit)
		if s.session != nil {
			s.session.recentErrors = appendCapped(s.session.recentErrors, line, browserAgentErrorLimit)
		}
		s.recentTrace = appendCappedTrace(s.recentTrace, BrowserTraceEvent{Kind: "error", Summary: line, CreatedAt: now}, browserAgentConsoleLimit)
		s.recentTimeline = appendCappedTimeline(s.recentTimeline, BrowserTimelineSlice{Kind: "error", Summary: line, CreatedAt: now}, browserAgentConsoleLimit)
		s.recentConsoleEntries = appendCappedConsoleEntries(s.recentConsoleEntries, BrowserConsoleEvent{Type: "exception", Level: "error", Text: line, CreatedAt: now}, browserAgentConsoleLimit)
	case "Network.requestWillBeSent":
		var payload struct {
			Request struct {
				Method string `json:"method"`
				URL    string `json:"url"`
			} `json:"request"`
		}
		if json.Unmarshal(evt.Params, &payload) == nil {
			line := strings.TrimSpace(payload.Request.Method + " " + payload.Request.URL)
			if line != "" {
				now := time.Now().UnixMilli()
				s.recentNetwork = appendCapped(s.recentNetwork, line, browserAgentNetworkLimit)
				if s.session != nil {
					s.session.recentNetwork = appendCapped(s.session.recentNetwork, line, browserAgentNetworkLimit)
				}
				s.recentTrace = appendCappedTrace(s.recentTrace, BrowserTraceEvent{Kind: "network", Summary: line, CreatedAt: now}, browserAgentConsoleLimit)
				s.recentTimeline = appendCappedTimeline(s.recentTimeline, BrowserTimelineSlice{Kind: "network", Summary: line, CreatedAt: now}, browserAgentConsoleLimit)
				s.recentNetworkEntries = appendCappedNetworkEntries(s.recentNetworkEntries, BrowserNetworkEvent{Method: payload.Request.Method, URL: payload.Request.URL, Kind: "request", CreatedAt: now}, browserAgentNetworkLimit)
			}
		}
	}
}

func (s *BrowserAgentSession) addSnapshot(snapshot BrowserSnapshot) {
	if s == nil {
		return
	}
	if s.snapshots == nil {
		s.snapshots = map[string]*BrowserSnapshot{}
	}
	cp := snapshot
	s.snapshots[snapshot.SnapshotID] = &cp
	s.lastSnapshotID = snapshot.SnapshotID
	if len(s.snapshots) > browserAgentSnapshotLimit {
		oldestID := ""
		oldestAt := int64(0)
		for id, item := range s.snapshots {
			if item == nil {
				continue
			}
			if oldestID == "" || item.CreatedAt < oldestAt {
				oldestID = id
				oldestAt = item.CreatedAt
			}
		}
		if oldestID != "" {
			delete(s.snapshots, oldestID)
		}
	}
}

// GetSnapshot returns a previously captured snapshot.
func (s *BrowserAgentSession) GetSnapshot(snapshotID string) (*BrowserSnapshot, bool) {
	if s == nil || snapshotID == "" {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap, ok := s.snapshots[snapshotID]
	if !ok || snap == nil {
		return nil, false
	}
	cp := *snap
	return &cp, true
}

// State returns an immutable view of the session for UI/trace layers.
func (s *BrowserAgentSession) State() BrowserAgentState {
	if s == nil {
		return BrowserAgentState{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	state := BrowserAgentState{
		ID:             s.ID,
		Addr:           s.Addr,
		TargetID:       s.TargetID,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
		Policy:         s.Policy,
		ActivityLog:    append([]string(nil), s.activityLog...),
		ConsoleLines:   append([]string(nil), s.recentConsole...),
		NetworkLines:   append([]string(nil), s.recentNetwork...),
		ErrorLines:     append([]string(nil), s.recentErrors...),
		Trace:          append([]BrowserTraceEvent(nil), s.recentTrace...),
		Timeline:       append([]BrowserTimelineSlice(nil), s.recentTimeline...),
		ConsoleEntries: append([]BrowserConsoleEvent(nil), s.recentConsoleEntries...),
		NetworkEntries: append([]BrowserNetworkEvent(nil), s.recentNetworkEntries...),
		LastSnapshotID: s.lastSnapshotID,
		ActiveTabID:    s.TargetID,
		Alive:          s.session != nil && s.session.client != nil && s.session.client.IsAlive(),
	}
	if s.lastSnapshotID != "" {
		if snap, ok := s.snapshots[s.lastSnapshotID]; ok && snap != nil {
			state.CurrentURL = snap.URL
			state.CurrentTitle = snap.Title
			state.ReadyState = snap.ReadyState
			state.LatestRefs = append([]BrowserElementRef(nil), snap.Refs...)
			if len(snap.FrameTree) > 0 {
				state.Frames = append([]BrowserFrameSnapshot(nil), snap.FrameTree...)
				state.ActiveFrameID = snap.FrameTree[0].FrameID
			}
		}
	}
	if state.CurrentURL != "" || state.CurrentTitle != "" {
		state.Tabs = []BrowserTabSnapshot{{TabID: s.TargetID, URL: state.CurrentURL, Title: state.CurrentTitle, Type: "page", Active: true}}
	}
	if len(s.snapshots) > 0 {
		state.Snapshots = make([]BrowserSnapshot, 0, len(s.snapshots))
		for _, snap := range s.snapshots {
			if snap == nil {
				continue
			}
			state.Snapshots = append(state.Snapshots, *snap)
		}
		sort.Slice(state.Snapshots, func(i, j int) bool {
			return state.Snapshots[i].CreatedAt < state.Snapshots[j].CreatedAt
		})
	}
	return state
}

func appendCapped(lines []string, value string, limit int) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return lines
	}
	lines = append(lines, value)
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines
}

func appendCappedTimeline(items []BrowserTimelineSlice, item BrowserTimelineSlice, limit int) []BrowserTimelineSlice {
	if strings.TrimSpace(item.Summary) == "" {
		return items
	}
	items = append(items, item)
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	return items
}

func appendCappedConsoleEntries(items []BrowserConsoleEvent, item BrowserConsoleEvent, limit int) []BrowserConsoleEvent {
	if strings.TrimSpace(item.Text) == "" {
		return items
	}
	items = append(items, item)
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	return items
}

func appendCappedNetworkEntries(items []BrowserNetworkEvent, item BrowserNetworkEvent, limit int) []BrowserNetworkEvent {
	if strings.TrimSpace(item.URL) == "" {
		return items
	}
	items = append(items, item)
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	return items
}

func compactJSONString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(b)
}
