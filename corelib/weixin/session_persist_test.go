package weixin

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionPersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "weixin_sessions.json")

	gw1 := NewGateway(Config{Token: "t", SessionPersistPath: path}, func(IncomingMessage) {})
	if got := gw1.GetContextToken("u1"); got != "" {
		t.Fatalf("fresh gateway should have empty cache, got %q", got)
	}

	gw1.rememberInboundSession("u1", "tok-1")
	gw1.rememberInboundSession("u2", "tok-2")
	gw1.flushSessionPersist() // debounce would otherwise delay disk write

	if gw1.GetContextToken("u1") != "tok-1" || gw1.GetContextToken("u2") != "tok-2" {
		t.Fatalf("in-memory tokens missing: u1=%q u2=%q", gw1.GetContextToken("u1"), gw1.GetContextToken("u2"))
	}
	if gw1.LastActiveUserID() != "u2" {
		t.Fatalf("LastActiveUserID = %q, want u2 (most recent inbound)", gw1.LastActiveUserID())
	}

	// Simulate app restart: new gateway, same path.
	gw2 := NewGateway(Config{Token: "t", SessionPersistPath: path}, func(IncomingMessage) {})
	if got := gw2.GetContextToken("u1"); got != "tok-1" {
		t.Fatalf("restored u1 token = %q, want tok-1", got)
	}
	if got := gw2.GetContextToken("u2"); got != "tok-2" {
		t.Fatalf("restored u2 token = %q, want tok-2", got)
	}
	if got := gw2.LastActiveUserID(); got != "u2" {
		t.Fatalf("restored LastActiveUserID = %q, want u2", got)
	}

	// HasProactive-style check: recency list non-empty.
	if n := len(gw2.ContextSessionsByRecency()); n < 2 {
		t.Fatalf("ContextSessionsByRecency len = %d, want >= 2", n)
	}
}

func TestSessionPersistMissingFileOK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.json")
	gw := NewGateway(Config{Token: "t", SessionPersistPath: path}, func(IncomingMessage) {})
	if got := gw.GetContextToken("x"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestSessionPersistPreservesUpdatedTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.json")
	old := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)

	gw1 := NewGateway(Config{Token: "t", SessionPersistPath: path}, func(IncomingMessage) {})
	gw1.ctxTokens.SetWithTime("u-old", "tok-old", old)
	gw1.persistSessions()

	gw2 := NewGateway(Config{Token: "t", SessionPersistPath: path}, func(IncomingMessage) {})
	// Newest (just set) should come first if we add a fresh one after restore.
	gw2.rememberInboundSession("u-new", "tok-new")
	list := gw2.ContextSessionsByRecency()
	if len(list) < 2 {
		t.Fatalf("want 2 sessions, got %d", len(list))
	}
	if list[0][0] != "u-new" {
		t.Fatalf("newest first: got %v", list)
	}

	// Debounced write + Stop flush must still land on disk.
	gw2.flushSessionPersist()
	gw3 := NewGateway(Config{Token: "t", SessionPersistPath: path}, func(IncomingMessage) {})
	if got := gw3.GetContextToken("u-new"); got != "tok-new" {
		t.Fatalf("after flush restore u-new=%q", got)
	}
}

func TestSessionPersistDebounceCoalesces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "debounce.json")
	gw := NewGateway(Config{Token: "t", SessionPersistPath: path}, func(IncomingMessage) {})
	for i := 0; i < 5; i++ {
		gw.rememberInboundSession("u", "tok")
	}
	// Before debounce fires, file may be missing.
	// After flush, file exists with token.
	gw.flushSessionPersist()
	gw2 := NewGateway(Config{Token: "t", SessionPersistPath: path}, func(IncomingMessage) {})
	if got := gw2.GetContextToken("u"); got != "tok" {
		t.Fatalf("got %q", got)
	}
}

func TestSessionPersistPathForKeyStableAndScoped(t *testing.T) {
	p1 := SessionPersistPathForKey("tenant\x00user-a")
	p2 := SessionPersistPathForKey("tenant\x00user-a")
	p3 := SessionPersistPathForKey("tenant\x00user-b")
	if p1 != p2 {
		t.Fatalf("same key should map to same path: %q vs %q", p1, p2)
	}
	if p1 == p3 {
		t.Fatal("different principals must not share session file")
	}
	if SessionPersistPathForKey("") != DefaultSessionPersistPath() {
		t.Fatal("empty key should use default path")
	}
	if SessionPersistPathForKey("  ") != DefaultSessionPersistPath() {
		t.Fatal("whitespace key should use default path")
	}
}

func TestSessionPersistDisabledWithDash(t *testing.T) {
	gw := NewGateway(Config{Token: "t", SessionPersistPath: "-"}, func(IncomingMessage) {})
	gw.rememberInboundSession("u", "tok")
	gw.flushSessionPersist()
	// No path → nothing on default location required; in-memory still works.
	if got := gw.GetContextToken("u"); got != "tok" {
		t.Fatalf("in-memory = %q", got)
	}
}

func TestInvalidateContextSessionClearsTokenAndDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inv.json")
	gw := NewGateway(Config{Token: "t", SessionPersistPath: path}, func(IncomingMessage) {})
	gw.rememberInboundSession("u1", "tok-1")
	gw.flushSessionPersist()
	if gw.GetContextToken("u1") == "" {
		t.Fatal("expected token before invalidate")
	}
	gw.InvalidateContextSession("u1")
	if got := gw.GetContextToken("u1"); got != "" {
		t.Fatalf("token should be cleared, got %q", got)
	}
	if gw.LastActiveUserID() != "" {
		t.Fatalf("last active should clear when matched, got %q", gw.LastActiveUserID())
	}
	// Disk must not restore the dead token after restart.
	gw2 := NewGateway(Config{Token: "t", SessionPersistPath: path}, func(IncomingMessage) {})
	if got := gw2.GetContextToken("u1"); got != "" {
		t.Fatalf("disk restored dead token %q", got)
	}
}

func TestIsContextSessionError(t *testing.T) {
	if IsContextSessionError(nil) {
		t.Fatal("nil")
	}
	if !IsContextSessionError(&APIStatusError{Errcode: sessionExpiredErrcode, ErrMsg: "x"}) {
		t.Fatal("session expired errcode")
	}
	if !IsContextSessionError(&APIStatusError{ErrMsg: "invalid context token"}) {
		t.Fatal("errmsg context token")
	}
	if !IsContextSessionError(&APIStatusError{ErrMsg: "context_token expired"}) {
		t.Fatal("context_token expired")
	}
	if IsContextSessionError(&APIStatusError{Errcode: -1, ErrMsg: "bad voice"}) {
		t.Fatal("generic error should not match")
	}
	// Bare "token" (e.g. access token) must NOT wipe private session cache.
	if IsContextSessionError(&APIStatusError{ErrMsg: "invalid access token"}) {
		t.Fatal("access token error must not be treated as context session")
	}
	if IsContextSessionError(&APIStatusError{ErrMsg: "illegal request"}) {
		t.Fatal("bare 非法/illegal should not match")
	}
	if !IsContextSessionError(fmt.Errorf("wrap: %w", &APIStatusError{ErrMsg: "context expired"})) {
		t.Fatal("wrapped context error")
	}
}

func TestMaybeInvalidateOnSendError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "send_inv.json")
	gw := NewGateway(Config{Token: "t", SessionPersistPath: path}, func(IncomingMessage) {})
	gw.rememberInboundSession("u2", "tok-2")
	gw.flushSessionPersist()

	err := gw.maybeInvalidateOnSendError("u2", &APIStatusError{ErrMsg: "context token invalid"})
	if err == nil || !strings.Contains(err.Error(), "会话已失效") {
		t.Fatalf("want re-chat hint, got %v", err)
	}
	if gw.GetContextToken("u2") != "" {
		t.Fatal("token should be cleared")
	}
	// non-context error leaves token alone
	gw.rememberInboundSession("u3", "tok-3")
	err2 := gw.maybeInvalidateOnSendError("u3", &APIStatusError{Errcode: -1, ErrMsg: "rate limit"})
	if err2 == nil || strings.Contains(err2.Error(), "会话已失效") {
		t.Fatalf("rate limit should pass through, got %v", err2)
	}
	if gw.GetContextToken("u3") != "tok-3" {
		t.Fatal("token should remain for non-context errors")
	}
}
