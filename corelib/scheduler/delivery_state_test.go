package scheduler

import (
	"path/filepath"
	"testing"
)

func TestDeliveryStateStoreRememberAndResolve(t *testing.T) {
	dir := t.TempDir()
	s := NewDeliveryStateStore(dir)
	if s.GetLastPeer("telegram") != "" {
		t.Fatal("expected empty")
	}
	s.RememberPeer("telegram", "42")
	if got := s.GetLastPeer("telegram"); got != "42" {
		t.Fatalf("got %q", got)
	}
	// Reload
	s2 := NewDeliveryStateStore(dir)
	if got := s2.ResolveSelfPeer("telegram", "self"); got != "42" {
		t.Fatalf("resolve self=%q", got)
	}
	if got := s2.ResolveSelfPeer("telegram", "99"); got != "99" {
		t.Fatalf("explicit=%q", got)
	}
	// self must not overwrite
	s2.RememberPeer("telegram", "self")
	if got := s2.GetLastPeer("telegram"); got != "42" {
		t.Fatalf("self clobber=%q", got)
	}
	if !IsSelfPeerID("SELF") || IsSelfPeerID("42") {
		t.Fatal("IsSelfPeerID")
	}
	if (*DeliveryStateStore)(nil).ResolveSelfPeer("telegram", "self") != "" {
		t.Fatal("nil store resolve self")
	}
	if (*DeliveryStateStore)(nil).ResolveSelfPeer("telegram", "9") != "9" {
		t.Fatal("nil store resolve explicit")
	}
	// No-op rewrite should not error
	s2.RememberPeer("telegram", "42")
	if got := s2.GetLastPeer("telegram"); got != "42" {
		t.Fatalf("after noop write: %q", got)
	}
	if _, err := filepath.Glob(filepath.Join(dir, DeliveryStateFileName)); err != nil {
		t.Fatal(err)
	}
}

func TestPeerIDFromTarget(t *testing.T) {
	t.Parallel()
	if PeerIDFromTarget(DeliveryTarget{UserID: "u", GroupID: "g"}) != "u" {
		t.Fatal("prefer user")
	}
	if PeerIDFromTarget(DeliveryTarget{GroupID: "g"}) != "g" {
		t.Fatal("group")
	}
}
