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
