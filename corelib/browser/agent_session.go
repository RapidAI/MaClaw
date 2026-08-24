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
	browserAgentStarts   = map[string]*sync.Mutex{}
)

// StartAgentSession creates or reuses a long-lived browser agent session.
// The mode parameter controls the connection strategy:
//   - SessionModePersistent (default): managed durable MaClaw profile
//   - SessionModeAuto: legacy alias normalized to persistent
//   - SessionModeConnectUser: only connect to user's running Chrome (no launch)
//   - SessionModeIsolated: launch with isolated debug profile (no user data)
func StartAgentSession(addr string, policy BrowserPolicy, reuseExisting bool, mode SessionMode) (*BrowserAgentSession, error) {
	return StartAgentSessionForOwner("", addr, policy, reuseExisting, mode)
}

// StartAgentSessionForOwner scopes browser-agent reuse to a logical agent owner.
// Different owners may share the same underlying Chrome profile/port, but must
// never share the same BrowserAgentSession or page target.
func StartAgentSessionForOwner(ownerID, addr string, policy BrowserPolicy, reuseExisting bool, mode SessionMode) (*BrowserAgentSession, error) {
	// Normalize mode before reuse checks so an isolated request cannot silently
	// attach to an older user-profile session (or vice versa). Persistent mode is
	// always treated as a singleton because one durable profile must not be driven
	// by multiple independent browser sessions.
	startedAt := time.Now()
	mode = normalizeBrowserAgentMode(mode)
	requestedAddr := strings.TrimSpace(addr)
	ownerID = strings.TrimSpace(ownerID)

	if existing := reusableBrowserAgentSessionForOwner(ownerID, requestedAddr, mode, policy, reuseExisting); existing != nil {
		return existing, nil
	}

	startLock := browserAgentStartLockForRequest(ownerID, requestedAddr, mode)
	lockWaitStart := time.Now()
	startLock.Lock()
	if waited := time.Since(lockWaitStart); waited > 100*time.Millisecond {
		log.Printf("[browser] agent start lock waited owner=%q addr=%s mode=%s waited=%s", ownerID, requestedAddr, mode, waited.Round(time.Millisecond))
	}
	defer startLock.Unlock()
	if existing := reusableBrowserAgentSessionForOwner(ownerID, requestedAddr, mode, policy, reuseExisting); existing != nil {
		return existing, nil
	}

	sess, err := startAgentSessionForOwner(ownerID, requestedAddr, policy, reuseExisting, mode)
	if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
		status := "ok"
		if err != nil {
			status = "error"
		}
		log.Printf("[browser] agent start done owner=%q addr=%s mode=%s status=%s elapsed=%s err=%v", ownerID, requestedAddr, mode, status, elapsed.Round(time.Millisecond), err)
	}
	return sess, err
}

func reusableBrowserAgentSessionForOwner(ownerID, requestedAddr string, mode SessionMode, policy BrowserPolicy, reuseExisting bool) *BrowserAgentSession {
	if !reuseExisting && mode != SessionModePersistent {
		return nil
	}
	var candidates []*BrowserAgentSession
	browserAgentMu.Lock()
	if reuseExisting || mode == SessionModePersistent {
		for _, sess := range browserAgentSessions {
			if sess == nil {
				continue
			}
			if !browserAgentSessionMatchesRequestForOwner(sess, ownerID, requestedAddr, mode) {
				continue
			}
			candidates = append(candidates, sess)
		}
	}
	browserAgentMu.Unlock()
	for _, sess := range candidates {
		live, err := GetAgentSession(sess.ID)
		if err != nil || live == nil {
			_ = StopAgentSession(sess.ID, false)
			continue
		}
		if !agentSessionStillRegistered(live.ID, live) {
			continue
		}
		live.mu.Lock()
		live.Policy = policy
		live.UpdatedAt = time.Now()
		live.mu.Unlock()
		log.Printf("[browser] agent session reused id=%s owner=%q addr=%s mode=%s target=%s", live.ID, live.OwnerID, live.Addr, live.Mode, live.TargetID)
		return live
	}
	return nil
}

func browserAgentStartLockForRequest(ownerID, requestedAddr string, mode SessionMode) *sync.Mutex {
	key := strings.Join([]string{strings.TrimSpace(ownerID), strings.TrimSpace(requestedAddr), string(mode)}, "\x00")
	browserAgentMu.Lock()
	defer browserAgentMu.Unlock()
	lock := browserAgentStarts[key]
	if lock == nil {
		lock = &sync.Mutex{}
		browserAgentStarts[key] = lock
	}
	return lock
}

func startAgentSessionForOwner(ownerID, requestedAddr string, policy BrowserPolicy, reuseExisting bool, mode SessionMode) (*BrowserAgentSession, error) {
	cdpAddr := requestedAddr
	managedUserDataDir := ""
	if cdpAddr == "" {
		var err error
		discoverStart := time.Now()
		switch mode {
		case SessionModePersistent:
			managedUserDataDir = persistentProfileDir()
			cdpAddr, err = DiscoverOrLaunchPersistent()
		case SessionModeConnectUser:
			cdpAddr, err = ConnectUserChrome()
		case SessionModeIsolated:
			managedUserDataDir = debugProfileDir()
			cdpAddr, err = DiscoverOrLaunch()
		case SessionModeAuto:
			managedUserDataDir = persistentProfileDir()
			cdpAddr, err = DiscoverOrLaunchPersistent()
		default:
			managedUserDataDir = persistentProfileDir()
			cdpAddr, err = DiscoverOrLaunchPersistent()
		}
		if err != nil {
			return nil, err
		}
		if elapsed := time.Since(discoverStart); elapsed > 500*time.Millisecond {
			log.Printf("[browser] discover_or_launch owner=%q mode=%s addr=%s elapsed=%s", ownerID, mode, cdpAddr, elapsed.Round(time.Millisecond))
		}
	}
	if existing := reusableBrowserAgentSessionForOwner(ownerID, cdpAddr, mode, policy, reuseExisting); existing != nil {
		return existing, nil
	}

	connectStart := time.Now()
	session, err := connectToAddr(cdpAddr)
	if err != nil {
		return nil, err
	}
	if elapsed := time.Since(connectStart); elapsed > 500*time.Millisecond {
		log.Printf("[browser] connect owner=%q addr=%s mode=%s elapsed=%s", ownerID, cdpAddr, mode, elapsed.Round(time.Millisecond))
	}
	targetStart := time.Now()
	if err := ensureDedicatedAgentTarget(session); err != nil {
		session.closeClient()
		return nil, err
	}
	if elapsed := time.Since(targetStart); elapsed > 500*time.Millisecond {
		log.Printf("[browser] create_target owner=%q addr=%s mode=%s elapsed=%s", ownerID, cdpAddr, mode, elapsed.Round(time.Millisecond))
	}
	if browserAgentModeIsManaged(mode) {
		if closed := session.PruneDuplicatePages(); closed > 0 {
			log.Printf("[browser] pruned %d duplicate managed browser tabs owner=%q addr=%s mode=%s", closed, ownerID, cdpAddr, mode)
		}
	}
	if existing := reusableBrowserAgentSessionForOwner(ownerID, cdpAddr, mode, policy, reuseExisting); existing != nil {
		session.closeClient()
		return existing, nil
	}
	targetID := activeTargetID(session)
	now := time.Now()
	agentSession := &BrowserAgentSession{
		ID:                 "browser-session-" + generateID(),
		OwnerID:            ownerID,
		Addr:               cdpAddr,
		TargetID:           targetID,
		CreatedAt:          now,
		UpdatedAt:          now,
		LastActivityAt:     now,
		Policy:             policy,
		Mode:               mode,
		ManagedUserDataDir: managedUserDataDir,
		session:            session,
		stopCh:             make(chan struct{}),
		targetGoneCh:       make(chan struct{}),
		snapshots:          map[string]*BrowserSnapshot{},
		recentConsole:      []string{},
		recentNetwork:      []string{},
		recentErrors:       []string{},
		recentSubmitClicks: map[string]time.Time{},
	}
	agentSession.startEventPump()
	applySessionDownloadPolicy(session, policy, mode)
	// Start inactivity timeout only for sessions attached to the user's own Chrome.
	if browserAgentModeUsesUserChrome(mode) {
		agentSession.startInactivityTimer()
	}
	// Audit log for user-chrome connections.
	if browserAgentModeUsesUserChrome(mode) {
		GetAuditLogger().LogConnect(agentSession.ID, cdpAddr)
	}
	browserAgentMu.Lock()
	browserAgentSessions[agentSession.ID] = agentSession
	browserAgentMu.Unlock()
	log.Printf("[browser] agent session started id=%s owner=%q addr=%s mode=%s target=%s managed_dir=%q reuse=%v", agentSession.ID, ownerID, cdpAddr, mode, targetID, managedUserDataDir, reuseExisting)
	return agentSession, nil
}

func normalizeBrowserAgentMode(mode SessionMode) SessionMode {
	switch SessionMode(strings.ToLower(strings.TrimSpace(string(mode)))) {
	case "", SessionModePersistent:
		return SessionModePersistent
	case SessionModeIsolated:
		return SessionModeIsolated
	case SessionModeConnectUser:
		return SessionModeConnectUser
	case SessionModeAuto:
		return SessionModePersistent
	default:
		return SessionModePersistent
	}
}

func browserAgentSessionMatchesRequest(sess *BrowserAgentSession, requestedAddr string, mode SessionMode) bool {
	return browserAgentSessionMatchesRequestForOwner(sess, "", requestedAddr, mode)
}

func browserAgentSessionMatchesRequestForOwner(sess *BrowserAgentSession, ownerID, requestedAddr string, mode SessionMode) bool {
	if sess == nil {
		return false
	}
	if strings.TrimSpace(sess.OwnerID) != strings.TrimSpace(ownerID) {
		return false
	}
	if requestedAddr != "" && strings.TrimSpace(sess.Addr) != requestedAddr {
		return false
	}
	return sess.Mode == mode
}

func ensureDedicatedAgentTarget(session *Session) error {
	if session == nil {
		return fmt.Errorf("browser session is not connected")
	}
	session.mu.Lock()
	client := session.client
	session.mu.Unlock()
	if client == nil {
		return fmt.Errorf("browser session is not connected")
	}
	result, err := client.Send("Target.createTarget", map[string]interface{}{"url": "about:blank"}, 5*time.Second)
	if err != nil {
		return fmt.Errorf("create dedicated browser target: %w", err)
	}
	var payload struct {
		TargetID string `json:"targetId"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return fmt.Errorf("parse dedicated browser target: %w", err)
	}
	if strings.TrimSpace(payload.TargetID) == "" {
		return fmt.Errorf("create dedicated browser target returned empty target id")
	}
	if err := session.SwitchPage(payload.TargetID); err != nil {
		return fmt.Errorf("attach dedicated browser target: %w", err)
	}
	return nil
}

// GetAgentSession returns a previously started agent session.
func GetAgentSession(sessionID string) (*BrowserAgentSession, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("missing browser session id")
	}
	browserAgentMu.Lock()
	sess, ok := browserAgentSessions[sessionID]
	browserAgentMu.Unlock()
	if !ok || sess == nil {
		return nil, fmt.Errorf("browser session not found: %s", sessionID)
	}
	if sess.cdpClient().IsAlive() && sess.IsTargetAlive() {
		if !agentSessionStillRegistered(sessionID, sess) {
			return nil, fmt.Errorf("browser session not found: %s", sessionID)
		}
		sess.TouchActivity()
		return sess, nil
	}
	sess.recoverMu.Lock()
	defer sess.recoverMu.Unlock()
	if !sess.cdpClient().IsAlive() {
		if err := recoverAgentSessionConnection(sess); err != nil {
			return nil, err
		}
	} else if !sess.IsTargetAlive() {
		if err := recoverAgentSessionTarget(sess); err != nil {
			return nil, err
		}
	}
	if !agentSessionStillRegistered(sessionID, sess) {
		return nil, fmt.Errorf("browser session not found: %s", sessionID)
	}
	sess.TouchActivity()
	return sess, nil
}

func agentSessionStillRegistered(sessionID string, sess *BrowserAgentSession) bool {
	browserAgentMu.Lock()
	current := browserAgentSessions[sessionID]
	browserAgentMu.Unlock()
	return current == sess
}

func recoverAgentSessionConnection(sess *BrowserAgentSession) error {
	if sess == nil {
		return fmt.Errorf("browser session not found")
	}
	sess.mu.RLock()
	old := sess.session
	addr := sess.Addr
	policy := sess.Policy
	sess.mu.RUnlock()
	var client *CDPClient
	if old != nil {
		old.mu.Lock()
		client = old.client
		old.mu.Unlock()
	}
	if client != nil && client.IsAlive() {
		return nil
	}
	if client != nil {
		_ = client.Close()
	}
	reconnected, err := connectToAddr(addr)
	if err != nil {
		return err
	}
	if err := attachSessionToRecoverablePage(reconnected, policy, ""); err != nil {
		reconnected.closeClient()
		return err
	}
	if !agentSessionStillRegistered(sess.ID, sess) {
		reconnected.closeClient()
		return fmt.Errorf("browser session not found: %s", sess.ID)
	}
	targetID := activeTargetID(reconnected)
	sess.mu.Lock()
	cur := sess.session
	if cur != nil && cur != old {
		cur.mu.Lock()
		live := cur.client != nil && !cur.client.isClosed()
		cur.mu.Unlock()
		if live {
			sess.mu.Unlock()
			reconnected.closeClient()
			return nil
		}
	}
	sess.session = reconnected
	sess.TargetID = targetID
	sess.UpdatedAt = time.Now()
	sess.snapshots = map[string]*BrowserSnapshot{}
	sess.lastSnapshotID = ""
	sess.resetTargetGone()
	sess.mu.Unlock()
	sess.startEventPump()
	applySessionDownloadPolicy(reconnected, sess.Policy, sess.Mode)
	return nil
}

func recoverAgentSessionTarget(sess *BrowserAgentSession) error {
	if sess == nil {
		return fmt.Errorf("browser session not found")
	}
	log.Printf("[browser] target gone but connection alive session=%s; re-attaching to new target", sess.ID)
	sess.mu.RLock()
	session := sess.session
	policy := sess.Policy
	currentID := sess.TargetID
	sess.mu.RUnlock()
	if err := attachSessionToRecoverablePage(session, policy, currentID); err != nil {
		if isPolicyDenied(err) {
			return err
		}
		return fmt.Errorf("browser target gone and failed to attach to new target: %w", err)
	}
	if !agentSessionStillRegistered(sess.ID, sess) {
		if client := sess.cdpClient(); client != nil {
			_ = client.Close()
		}
		return fmt.Errorf("browser session not found: %s", sess.ID)
	}
	newTargetID := activeTargetID(sess.session)
	sess.mu.Lock()
	sess.TargetID = newTargetID
	sess.UpdatedAt = time.Now()
	sess.snapshots = map[string]*BrowserSnapshot{}
	sess.lastSnapshotID = ""
	sess.resetTargetGone()
	sess.mu.Unlock()
	sess.startEventPump()
	log.Printf("[browser] re-attached to target=%s session=%s", newTargetID, sess.ID)
	return nil
}

func chooseRecoveryPageTarget(session *Session, policy BrowserPolicy, currentID string, targets []TargetInfo) (string, error) {
	currentID = strings.TrimSpace(currentID)
	var otherSafe, anySafe string
	sawPage := false
	for _, t := range targets {
		id := strings.TrimSpace(t.ID)
		if t.Type != "page" || id == "" {
			continue
		}
		sawPage = true
		popup := session != nil && session.isPopupTarget(id)
		if !policy.AllowPopup && popup {
			continue
		}
		if anySafe == "" {
			anySafe = id
		}
		if id != currentID && otherSafe == "" {
			otherSafe = id
		}
	}
	if otherSafe != "" {
		return otherSafe, nil
	}
	if anySafe != "" {
		return anySafe, nil
	}
	if sawPage && !policy.AllowPopup {
		return "", policyDenied("browser policy blocked popup; pass allow_popup=true on session_start")
	}
	return "", nil
}

func attachSessionToRecoverablePage(session *Session, policy BrowserPolicy, currentID string) error {
	if session == nil {
		return fmt.Errorf("browser session not connected")
	}
	infos, err := session.getTargetInfos()
	if err != nil {
		pages, listErr := session.ListPages()
		if listErr != nil {
			return err
		}
		return switchToRecoverablePage(session, policy, currentID, pages)
	}
	session.hydratePopupTargetsFrom(infos)
	return switchToRecoverablePage(session, policy, currentID, pageTargetsFromInfos(infos))
}

func switchToRecoverablePage(session *Session, policy BrowserPolicy, currentID string, targets []TargetInfo) error {
	id, err := chooseRecoveryPageTarget(session, policy, currentID, targets)
	if err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("browser target gone and no page targets available; call browser(action=\"navigate\", url=\"...\") to open a new page")
	}
	session.mu.Lock()
	active := session.activeTabID
	session.mu.Unlock()
	if id == active {
		return nil
	}
	return session.SwitchPage(id)
}

// StopAgentSession closes and removes a browser agent session.
// Browser process lifetime is intentionally narrower than session lifetime:
// persistent/connect_user sessions preserve the browser process and login state;
// only isolated debug sessions may be killed by this API.
func StopAgentSession(sessionID string, closeBrowser bool) error {
	browserAgentMu.Lock()
	sess, ok := browserAgentSessions[sessionID]
	if ok {
		delete(browserAgentSessions, sessionID)
	}
	browserAgentMu.Unlock()
	forgetBrowserSessionTaskSupervisor(sessionID)
	if !ok || sess == nil {
		return fmt.Errorf("browser session not found: %s", sessionID)
	}
	sess.mu.Lock()
	if sess.stopCh != nil {
		close(sess.stopCh)
		sess.stopCh = nil
	}
	timedOut := sess.timedOut
	mode := sess.Mode
	ownerID := sess.OwnerID
	addr := sess.Addr
	targetID := sess.TargetID
	managedDir := sess.ManagedUserDataDir
	session := sess.session
	sess.mu.Unlock()
	if browserAgentModeUsesUserChrome(mode) && !timedOut {
		GetAuditLogger().LogDisconnect(sessionID, "session_stop")
	}
	if session != nil {
		log.Printf("[browser] closing CDP client session=%s owner=%q addr=%s mode=%s target=%s close_browser=%v", sessionID, ownerID, addr, mode, targetID, closeBrowser)
		session.closeClient()
	}
	if closeBrowser && browserAgentModeAllowsProcessKill(mode) {
		if managedDir != "" {
			log.Printf("[browser] killing managed browser session=%s dir=%q", sessionID, managedDir)
			killManagedBrowserForDir(managedDir)
		}
	}
	return nil
}

func browserAgentModeUsesUserChrome(mode SessionMode) bool {
	return mode == SessionModeConnectUser
}

func browserAgentModeIsManaged(mode SessionMode) bool {
	return mode == SessionModePersistent || mode == SessionModeIsolated
}

func applySessionDownloadPolicy(session *Session, policy BrowserPolicy, mode SessionMode) {
	if session == nil || !shouldDenyManagedDownloads(policy, mode) {
		return
	}
	session.mu.Lock()
	client := session.client
	session.mu.Unlock()
	if client == nil {
		return
	}
	_, _ = client.Send("Browser.setDownloadBehavior", map[string]interface{}{
		"behavior":      "deny",
		"eventsEnabled": true,
	}, 3*time.Second)
}

func browserAgentModeAllowsProcessKill(mode SessionMode) bool {
	return mode == SessionModeIsolated
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
	session.mu.Lock()
	id := strings.TrimSpace(session.activeTabID)
	session.mu.Unlock()
	if id != "" {
		return id
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

func (s *BrowserAgentSession) cdpClient() *CDPClient {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	sess := s.session
	s.mu.RUnlock()
	return sessionClient(sess)
}

func sessionClient(session *Session) *CDPClient {
	if session == nil {
		return nil
	}
	session.mu.Lock()
	client := session.client
	session.mu.Unlock()
	return client
}

func sessionClientAlive(session *Session) bool {
	client := sessionClient(session)
	return client != nil && !client.isClosed()
}

func (s *BrowserAgentSession) sessionClientLocked() *CDPClient {
	return sessionClient(s.session)
}

func (s *BrowserAgentSession) startEventPump() {
	client := s.cdpClient()
	if client == nil {
		return
	}
	events := client.Events()
	if events == nil {
		return
	}
	s.mu.Lock()
	if s.eventPumpClient == client {
		s.mu.Unlock()
		return
	}
	s.eventPumpClient = client
	stopCh := s.stopCh
	s.mu.Unlock()
	go func() {
		log.Printf("[browser] event pump started session=%s target=%s", s.ID, s.TargetID)
		for {
			select {
			case <-stopCh:
				log.Printf("[browser] event pump stopped session=%s reason=stop", s.ID)
				return
			case evt, ok := <-events:
				if !ok {
					log.Printf("[browser] event pump stopped session=%s reason=events_closed target=%s", s.ID, s.TargetID)
					return
				}
				if s.cdpClient() != client {
					log.Printf("[browser] event pump exiting session=%s reason=client_replaced", s.ID)
					return
				}
				s.handleCDPEvent(evt)
			}
		}
	}()
}

// connectUserInactivityTimeout is the duration after which a connect_user
// session is automatically disconnected if no tool operations occur.
const connectUserInactivityTimeout = 30 * time.Minute

// startInactivityTimer starts a background goroutine that checks for inactivity
// and auto-disconnects the session after connectUserInactivityTimeout.
// Only used for connect_user mode (user's Chrome should not be held indefinitely).
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
					log.Printf("[browser] session %s inactive for 30 minutes; disconnecting", sessionID)
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
	case "Target.targetDestroyed":
		var payload struct {
			TargetID string `json:"targetId"`
		}
		_ = json.Unmarshal(evt.Params, &payload)
		log.Printf("[browser] target destroyed session=%s active_target=%s destroyed_target=%s", s.ID, s.TargetID, payload.TargetID)
		s.recentTrace = appendCappedTrace(s.recentTrace, BrowserTraceEvent{Kind: "target", Summary: "target destroyed: " + payload.TargetID, CreatedAt: time.Now().UnixMilli()}, browserAgentConsoleLimit)
		if payload.TargetID == s.TargetID {
			s.signalTargetGone()
		}
	case "Target.detachedFromTarget":
		var payload struct {
			SessionID string `json:"sessionId"`
			TargetID  string `json:"targetId"`
		}
		_ = json.Unmarshal(evt.Params, &payload)
		log.Printf("[browser] target detached session=%s active_target=%s detached_target=%s target_session=%s", s.ID, s.TargetID, payload.TargetID, payload.SessionID)
		s.recentTrace = appendCappedTrace(s.recentTrace, BrowserTraceEvent{Kind: "target", Summary: "target detached: " + payload.TargetID, CreatedAt: time.Now().UnixMilli()}, browserAgentConsoleLimit)
		if payload.TargetID == s.TargetID {
			s.signalTargetGone()
		}
	case "Inspector.detached":
		log.Printf("[browser] inspector detached session=%s target=%s params=%s", s.ID, s.TargetID, compactJSONString(evt.Params))
		s.recentTrace = appendCappedTrace(s.recentTrace, BrowserTraceEvent{Kind: "target", Summary: "inspector detached", CreatedAt: time.Now().UnixMilli()}, browserAgentConsoleLimit)
		s.signalTargetGone()
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
		s.session.noteRecentError(line)
		s.recentTrace = appendCappedTrace(s.recentTrace, BrowserTraceEvent{Kind: "error", Summary: line, CreatedAt: now}, browserAgentConsoleLimit)
		s.recentTimeline = appendCappedTimeline(s.recentTimeline, BrowserTimelineSlice{Kind: "error", Summary: line, CreatedAt: now}, browserAgentConsoleLimit)
		s.recentConsoleEntries = appendCappedConsoleEntries(s.recentConsoleEntries, BrowserConsoleEvent{Type: "exception", Level: "error", Text: line, CreatedAt: now}, browserAgentConsoleLimit)
	case "Network.requestWillBeSent":
		var payload struct {
			RequestID string `json:"requestId"`
			Request   struct {
				Method string `json:"method"`
				URL    string `json:"url"`
			} `json:"request"`
		}
		if json.Unmarshal(evt.Params, &payload) == nil {
			if s.session != nil {
				s.session.trackNetwork(payload.RequestID, true)
			}
			line := strings.TrimSpace(payload.Request.Method + " " + payload.Request.URL)
			if line != "" {
				now := time.Now().UnixMilli()
				s.recentNetwork = appendCapped(s.recentNetwork, line, browserAgentNetworkLimit)
				s.session.noteRecentNetwork(line)
				s.recentTrace = appendCappedTrace(s.recentTrace, BrowserTraceEvent{Kind: "network", Summary: line, CreatedAt: now}, browserAgentConsoleLimit)
				s.recentTimeline = appendCappedTimeline(s.recentTimeline, BrowserTimelineSlice{Kind: "network", Summary: line, CreatedAt: now}, browserAgentConsoleLimit)
				s.recentNetworkEntries = appendCappedNetworkEntries(s.recentNetworkEntries, BrowserNetworkEvent{RequestID: payload.RequestID, Method: payload.Request.Method, URL: payload.Request.URL, Kind: "request", CreatedAt: now}, browserAgentNetworkLimit)
			}
		}
	case "Network.loadingFinished", "Network.loadingFailed":
		var payload struct {
			RequestID string `json:"requestId"`
		}
		if json.Unmarshal(evt.Params, &payload) == nil && s.session != nil {
			s.session.trackNetwork(payload.RequestID, false)
		}
	case "Page.javascriptDialogOpening":
		var payload struct {
			URL     string `json:"url"`
			Message string `json:"message"`
			Type    string `json:"type"`
		}
		_ = json.Unmarshal(evt.Params, &payload)
		if s.session != nil {
			s.session.noteDialog(payload.Message, payload.Type, payload.URL)
		}
		s.recentTrace = appendCappedTrace(s.recentTrace, BrowserTraceEvent{Kind: "dialog", Summary: "dialog opening: " + payload.Type + " " + payload.Message, CreatedAt: time.Now().UnixMilli()}, browserAgentConsoleLimit)
	case "Page.javascriptDialogClosed":
		if s.session != nil {
			s.session.clearDialog()
		}
	case "Page.downloadWillBegin", "Browser.downloadWillBegin":
		var payload struct {
			GUID string `json:"guid"`
			URL  string `json:"url"`
		}
		_ = json.Unmarshal(evt.Params, &payload)
		if err := validateDownloadPolicy(s.Policy); err != nil {
			if client := s.sessionClientLocked(); client != nil && strings.TrimSpace(payload.GUID) != "" {
				guid := payload.GUID
				go func() {
					_, _ = client.Send("Browser.cancelDownload", map[string]interface{}{"guid": guid}, 3*time.Second)
				}()
			}
			s.recentTrace = appendCappedTrace(s.recentTrace, BrowserTraceEvent{Kind: "download", Summary: err.Error(), CreatedAt: time.Now().UnixMilli()}, browserAgentConsoleLimit)
			s.recentErrors = appendCapped(s.recentErrors, err.Error(), browserAgentErrorLimit)
		}
	case "Target.attachedToTarget":
		var payload struct {
			SessionID          string `json:"sessionId"`
			WaitingForDebugger bool   `json:"waitingForDebugger"`
			TargetInfo         struct {
				TargetID string `json:"targetId"`
				Type     string `json:"type"`
				URL      string `json:"url"`
				OpenerID string `json:"openerId"`
			} `json:"targetInfo"`
		}
		if json.Unmarshal(evt.Params, &payload) == nil && s.session != nil {
			s.session.noteAttachedTarget(payload.SessionID, payload.TargetInfo.TargetID, payload.TargetInfo.Type, payload.TargetInfo.URL, payload.TargetInfo.OpenerID, payload.WaitingForDebugger)
			if payload.TargetInfo.OpenerID != "" && payload.TargetInfo.Type == "page" {
				if err := validatePopupPolicy(s.Policy); err != nil {
					client := s.sessionClientLocked()
					targetID := payload.TargetInfo.TargetID
					if client != nil && targetID != "" {
						go func() {
							_, _ = client.Send("Target.closeTarget", map[string]interface{}{"targetId": targetID}, 3*time.Second)
						}()
					}
					s.recentTrace = appendCappedTrace(s.recentTrace, BrowserTraceEvent{Kind: "popup", Summary: err.Error(), CreatedAt: time.Now().UnixMilli()}, browserAgentConsoleLimit)
				}
			}
		}
	case "Target.targetCreated":
		var payload struct {
			TargetInfo struct {
				TargetID string `json:"targetId"`
				Type     string `json:"type"`
				URL      string `json:"url"`
				OpenerID string `json:"openerId"`
			} `json:"targetInfo"`
		}
		if json.Unmarshal(evt.Params, &payload) == nil && payload.TargetInfo.OpenerID != "" {
			if s.session != nil {
				s.session.notePopupTarget(payload.TargetInfo.TargetID, payload.TargetInfo.OpenerID, payload.TargetInfo.Type, payload.TargetInfo.URL)
			}
			if payload.TargetInfo.Type == "page" {
				if err := validatePopupPolicy(s.Policy); err != nil {
					if client := s.sessionClientLocked(); client != nil {
						targetID := payload.TargetInfo.TargetID
						go func() {
							_, _ = client.Send("Target.closeTarget", map[string]interface{}{"targetId": targetID}, 3*time.Second)
						}()
					}
					s.recentTrace = appendCappedTrace(s.recentTrace, BrowserTraceEvent{Kind: "popup", Summary: err.Error(), CreatedAt: time.Now().UnixMilli()}, browserAgentConsoleLimit)
				}
			}
		}
	}
}

// signalTargetGone closes targetGoneCh to abort any in-flight operations waiting
// on CDP responses for the now-dead target. Must be called with s.mu write-lock
// held (from handleCDPEvent). The select guards against double-close if the
// channel was already closed by a prior event in the same session.
func (s *BrowserAgentSession) signalTargetGone() {
	if s.targetGoneCh == nil {
		return
	}
	select {
	case <-s.targetGoneCh:
		// already closed
	default:
		log.Printf("[browser] signaling target gone session=%s target=%s", s.ID, s.TargetID)
		close(s.targetGoneCh)
	}
}

// closedCh is a pre-closed channel returned by TargetGone() for nil sessions.
// Avoids per-call allocation.
var closedCh = func() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}()

// TargetGone returns a channel that is closed when the active target is
// destroyed, detached, or the inspector disconnects. Operations can select on
// this to abort immediately instead of waiting the full CDP timeout.
func (s *BrowserAgentSession) TargetGone() <-chan struct{} {
	if s == nil {
		return closedCh
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.targetGoneCh
}

// IsTargetAlive returns false if the active target has been destroyed or
// the inspector has been detached. Fast check before starting operations.
func (s *BrowserAgentSession) IsTargetAlive() bool {
	if s == nil {
		return false
	}
	select {
	case <-s.TargetGone():
		return false
	default:
		return true
	}
}

// resetTargetGone re-creates the targetGoneCh channel, allowing the session
// to be reused after reconnection to a new target. Must be called with s.mu
// write-lock held (it mutates targetGoneCh which is read by TargetGone under
// s.mu.RLock).
func (s *BrowserAgentSession) resetTargetGone() {
	select {
	case <-s.targetGoneCh:
		// was closed, create fresh channel
		s.targetGoneCh = make(chan struct{})
	default:
		// still open, no-op
	}
}

func (s *BrowserAgentSession) addSnapshot(snapshot BrowserSnapshot) {
	if s == nil {
		return
	}
	if s.snapshots == nil {
		s.snapshots = map[string]*BrowserSnapshot{}
	}
	if prev := s.snapshots[s.lastSnapshotID]; prev != nil && prev.SnapshotID != snapshot.SnapshotID {
		s.lastFingerprint = snapshotFingerprint(*prev)
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
		OwnerID:        s.OwnerID,
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
		Alive:          sessionClientAlive(s.session),
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
