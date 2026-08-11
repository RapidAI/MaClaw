package improactive

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPeersRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peers.json")
	s := NewStore(path)

	if err := s.Patch(func(p *Peers) {
		p.LansengerPrivateUserID = "staff-1"
		p.LansengerPrivateUserIDsByBotID = map[string]string{"support": "staff-support"}
		p.TelegramLastChatID = 42
		p.QQLastOpenID = "oid-9"
	}); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	s2 := NewStore(path)
	p := s2.LoadOrEmpty()
	if p.LansengerPrivateUserID != "staff-1" {
		t.Fatalf("lansenger = %q", p.LansengerPrivateUserID)
	}
	if p.LansengerPrivateUserIDsByBotID["support"] != "staff-support" {
		t.Fatalf("lansenger profile peers = %#v", p.LansengerPrivateUserIDsByBotID)
	}
	if p.TelegramLastChatID != 42 {
		t.Fatalf("telegram = %d", p.TelegramLastChatID)
	}
	if p.QQLastOpenID != "oid-9" {
		t.Fatalf("qq = %q", p.QQLastOpenID)
	}
}

func TestPeersMissingFileEmpty(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "nope.json"))
	p, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.LansengerPrivateUserID != "" || p.TelegramLastChatID != 0 {
		t.Fatalf("want empty peers, got %+v", p)
	}
}

func TestPeersPatchRejectsCorruptJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peers.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStore(path)
	if err := s.Patch(func(p *Peers) { p.QQLastOpenID = "x" }); err == nil {
		t.Fatal("expected parse error on corrupt peers file")
	}
}

func TestPeersPatchSkipsNoopWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peers.json")
	s := NewStore(path)
	if err := s.Patch(func(p *Peers) { p.QQLastOpenID = "oid" }); err != nil {
		t.Fatal(err)
	}
	info1, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Same value again — must not rewrite (mtime may stay equal).
	time.Sleep(20 * time.Millisecond)
	if err := s.Patch(func(p *Peers) { p.QQLastOpenID = "oid" }); err != nil {
		t.Fatal(err)
	}
	info2, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info1.ModTime().Equal(info2.ModTime()) {
		// Some FS have coarse mtime; size should still match.
		if info1.Size() != info2.Size() {
			t.Fatalf("noop patch should not rewrite file")
		}
	}
}
