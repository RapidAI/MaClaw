package browser

import (
	"sync"
	"testing"
)

func TestDomainMatchesSupportsSubdomains(t *testing.T) {
	if !domainMatches("sub.example.com", "example.com") {
		t.Fatal("expected subdomain match")
	}
	if domainMatches("example.org", "example.com") {
		t.Fatal("unexpected cross-domain match")
	}
}

func TestAppendCappedTraceKeepsNewest(t *testing.T) {
	items := []BrowserTraceEvent{{Kind: "a"}, {Kind: "b"}}
	items = appendCappedTrace(items, BrowserTraceEvent{Kind: "c"}, 2)
	if len(items) != 2 {
		t.Fatalf("len = %d", len(items))
	}
	if items[0].Kind != "b" || items[1].Kind != "c" {
		t.Fatalf("items = %#v", items)
	}
}

func TestBrowserAgentSessionMatchesRequestRequiresModeAndAddrMatch(t *testing.T) {
	sess := &BrowserAgentSession{Addr: "http://127.0.0.1:9222", Mode: SessionModeIsolated}
	if !browserAgentSessionMatchesRequest(sess, "", SessionModeIsolated) {
		t.Fatal("expected isolated request to match isolated session")
	}
	if browserAgentSessionMatchesRequest(sess, "", normalizeBrowserAgentMode(SessionModeAuto)) {
		t.Fatal("auto request normalizes to persistent and should not reuse isolated session")
	}
	if browserAgentSessionMatchesRequest(sess, "http://127.0.0.1:9333", SessionModeIsolated) {
		t.Fatal("different addr should not match")
	}
}

func TestBrowserAgentSessionMatchesRequestRequiresOwnerMatch(t *testing.T) {
	sess := &BrowserAgentSession{OwnerID: "owner-a", Addr: "http://127.0.0.1:9222", Mode: SessionModePersistent}
	if !browserAgentSessionMatchesRequestForOwner(sess, "owner-a", "", SessionModePersistent) {
		t.Fatal("expected same owner to reuse matching persistent session")
	}
	if browserAgentSessionMatchesRequestForOwner(sess, "owner-b", "", SessionModePersistent) {
		t.Fatal("different owners must not reuse the same browser agent session")
	}
	if browserAgentSessionMatchesRequest(sess, "", SessionModePersistent) {
		t.Fatal("legacy owner must not reuse an owner-scoped browser agent session")
	}
}

func TestBrowserAgentStartLockIsScopedByOwner(t *testing.T) {
	browserAgentMu.Lock()
	browserAgentStarts = map[string]*sync.Mutex{}
	browserAgentMu.Unlock()

	lockA1 := browserAgentStartLockForRequest("owner-a", "", SessionModePersistent)
	lockA2 := browserAgentStartLockForRequest("owner-a", "", SessionModePersistent)
	lockB := browserAgentStartLockForRequest("owner-b", "", SessionModePersistent)
	if lockA1 == nil || lockA2 == nil || lockB == nil {
		t.Fatal("expected start locks")
	}
	if lockA1 != lockA2 {
		t.Fatal("same owner/request should share one start lock")
	}
	if lockA1 == lockB {
		t.Fatal("different owners must not serialize browser-agent startup on the same start lock")
	}
}

func TestPersistentModeIsManagedSingletonCandidate(t *testing.T) {
	sess := &BrowserAgentSession{Addr: "http://127.0.0.1:9222", Mode: SessionModePersistent}
	if !browserAgentSessionMatchesRequest(sess, "", SessionModePersistent) {
		t.Fatal("persistent request should match existing persistent session")
	}
	if browserAgentSessionMatchesRequest(sess, "http://127.0.0.1:9333", SessionModePersistent) {
		t.Fatal("persistent request with different explicit addr should not match")
	}
}

func TestNormalizeBrowserAgentModeDefaultsToPersistent(t *testing.T) {
	for _, mode := range []SessionMode{"", "bad", " AUTO "} {
		want := SessionModePersistent
		if got := normalizeBrowserAgentMode(mode); got != want {
			t.Fatalf("normalizeBrowserAgentMode(%q) = %q, want %q", mode, got, want)
		}
	}
}

func TestBrowserAgentModeAllowsProcessKillOnlyForIsolated(t *testing.T) {
	if browserAgentModeAllowsProcessKill(SessionModePersistent) {
		t.Fatal("persistent browser profile must not be killable from session_stop")
	}
	if browserAgentModeAllowsProcessKill(SessionModeConnectUser) {
		t.Fatal("user Chrome must not be killable from session_stop")
	}
	if !browserAgentModeAllowsProcessKill(SessionModeIsolated) {
		t.Fatal("isolated debug profile should remain disposable")
	}
}

func TestGetAgentSessionMissing(t *testing.T) {
	if _, err := GetAgentSession(""); err == nil {
		t.Fatal("expected missing session id")
	}
	if _, err := GetAgentSession("missing-session"); err == nil {
		t.Fatal("expected session not found")
	}
}

func TestReusableDropsDeadAgentSession(t *testing.T) {
	closed := make(chan struct{})
	close(closed)
	sess := &BrowserAgentSession{
		ID:           "browser-session-dead-reuse",
		Addr:         "http://127.0.0.1:1",
		Mode:         SessionModePersistent,
		session:      &Session{client: &CDPClient{closed: closed}},
		stopCh:       make(chan struct{}),
		targetGoneCh: make(chan struct{}),
	}
	browserAgentMu.Lock()
	prev, existed := browserAgentSessions[sess.ID]
	browserAgentSessions[sess.ID] = sess
	browserAgentMu.Unlock()
	t.Cleanup(func() {
		browserAgentMu.Lock()
		if existed {
			browserAgentSessions[sess.ID] = prev
		} else {
			delete(browserAgentSessions, sess.ID)
		}
		browserAgentMu.Unlock()
	})

	got := reusableBrowserAgentSessionForOwner("", sess.Addr, SessionModePersistent, BrowserPolicy{}, true)
	if got != nil {
		t.Fatal("expected dead session not to be reused")
	}
	browserAgentMu.Lock()
	_, still := browserAgentSessions[sess.ID]
	browserAgentMu.Unlock()
	if still {
		t.Fatal("dead session should be unregistered so a later start can create a replacement")
	}
}

func TestStartEventPumpIdempotentSameClient(t *testing.T) {
	client := &CDPClient{closed: make(chan struct{}), events: make(chan CDPEvent)}
	sess := &BrowserAgentSession{
		session:      &Session{client: client},
		stopCh:       make(chan struct{}),
		targetGoneCh: make(chan struct{}),
	}
	t.Cleanup(func() {
		close(sess.stopCh)
	})
	sess.startEventPump()
	sess.startEventPump()
	if sess.eventPumpClient != client {
		t.Fatal("expected event pump to attach to the live client")
	}
}

func TestBrowserAgentStateAliveUsesClosedChannel(t *testing.T) {
	s := &BrowserAgentSession{session: &Session{client: &CDPClient{closed: make(chan struct{})}}}
	if !s.State().Alive {
		t.Fatal("open client should snapshot alive")
	}
	close(s.session.client.closed)
	if s.State().Alive {
		t.Fatal("closed client should snapshot dead")
	}
}

func TestChooseRecoveryPageTargetPrefersNonPopup(t *testing.T) {
	s := &Session{}
	s.notePopupTarget("popup", "opener", "page", "https://example.com")
	id, err := chooseRecoveryPageTarget(s, BrowserPolicy{}, "dead", []TargetInfo{
		{Type: "page", ID: "popup"},
		{Type: "page", ID: "main"},
	})
	if err != nil || id != "main" {
		t.Fatalf("id=%q err=%v", id, err)
	}
}

func TestChooseRecoveryPageTargetBlocksOnlyPopup(t *testing.T) {
	s := &Session{}
	s.notePopupTarget("popup", "opener", "page", "https://example.com")
	_, err := chooseRecoveryPageTarget(s, BrowserPolicy{}, "dead", []TargetInfo{
		{Type: "page", ID: "popup"},
	})
	if !isPolicyDenied(err) {
		t.Fatalf("err=%v", err)
	}
}

func TestChooseRecoveryPageTargetAllowsPopupWhenEnabled(t *testing.T) {
	s := &Session{}
	s.notePopupTarget("popup", "opener", "page", "https://example.com")
	id, err := chooseRecoveryPageTarget(s, BrowserPolicy{AllowPopup: true}, "dead", []TargetInfo{
		{Type: "page", ID: "popup"},
	})
	if err != nil || id != "popup" {
		t.Fatalf("id=%q err=%v", id, err)
	}
}

func TestChooseRecoveryPageTargetPrefersOtherPage(t *testing.T) {
	id, err := chooseRecoveryPageTarget(&Session{}, BrowserPolicy{}, "main", []TargetInfo{
		{Type: "page", ID: "main"},
		{Type: "page", ID: "next"},
	})
	if err != nil || id != "next" {
		t.Fatalf("id=%q err=%v", id, err)
	}
}

func TestSwitchToRecoverablePageBlocksOnlyPopup(t *testing.T) {
	s := &Session{activeTabID: "popup"}
	s.notePopupTarget("popup", "opener", "page", "https://example.com")
	err := switchToRecoverablePage(s, BrowserPolicy{}, "dead", []TargetInfo{{Type: "page", ID: "popup"}})
	if !isPolicyDenied(err) {
		t.Fatalf("err=%v", err)
	}
}

func TestSwitchToRecoverablePageNoopWhenActive(t *testing.T) {
	s := &Session{activeTabID: "main"}
	if err := switchToRecoverablePage(s, BrowserPolicy{}, "dead", []TargetInfo{{Type: "page", ID: "main"}}); err != nil {
		t.Fatal(err)
	}
}

func TestHydratePopupTargetsFromMarksOpener(t *testing.T) {
	s := &Session{}
	s.hydratePopupTargetsFrom([]cdpTargetInfo{
		{TargetID: "main", Type: "page", URL: "https://example.com"},
		{TargetID: "popup", Type: "page", OpenerID: "main", URL: "https://ads.example"},
	})
	if s.isPopupTarget("main") {
		t.Fatal("main page should not be marked popup")
	}
	if !s.isPopupTarget("popup") {
		t.Fatal("page with openerId should be marked popup")
	}
}

func TestChooseRecoveryAfterHydrateBlocksOnlyPopup(t *testing.T) {
	s := &Session{}
	infos := []cdpTargetInfo{{TargetID: "popup", Type: "page", OpenerID: "gone"}}
	s.hydratePopupTargetsFrom(infos)
	_, err := chooseRecoveryPageTarget(s, BrowserPolicy{}, "dead", pageTargetsFromInfos(infos))
	if !isPolicyDenied(err) {
		t.Fatalf("err=%v", err)
	}
}

func TestPageTargetsFromInfosSkipsEmptyIDs(t *testing.T) {
	got := pageTargetsFromInfos([]cdpTargetInfo{
		{TargetID: "", Type: "page"},
		{TargetID: "main", Type: "page", Title: "Home", URL: "https://example.com"},
	})
	if len(got) != 1 || got[0].ID != "main" || got[0].Title != "Home" {
		t.Fatalf("got=%#v", got)
	}
}

func TestInstallGlobalSessionKeepsOpenWinner(t *testing.T) {
	globalSessionStartMu.Lock()
	defer globalSessionStartMu.Unlock()
	globalSessionMu.Lock()
	prev := globalSession
	globalSession = nil
	globalSessionMu.Unlock()
	defer func() {
		globalSessionMu.Lock()
		globalSession = prev
		globalSessionMu.Unlock()
	}()

	first := &Session{client: &CDPClient{closed: make(chan struct{})}}
	if got := installGlobalSession(first); got != first {
		t.Fatal("expected first session installed")
	}
	second := &Session{client: &CDPClient{closed: make(chan struct{})}}
	if got := installGlobalSession(second); got != first {
		t.Fatal("expected open winner to be kept")
	}
	globalSessionMu.Lock()
	globalSession = nil
	globalSessionMu.Unlock()
}
