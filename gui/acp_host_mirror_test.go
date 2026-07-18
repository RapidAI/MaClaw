package main

import "testing"

func TestAcpMirrorUISessionKey(t *testing.T) {
	// Prefer explicit backend session key.
	if got := acpMirrorUISessionKey("D:\\proj", "desktop-user:D:/proj"); got != "desktop-user:D:/proj" {
		t.Fatalf("got %q", got)
	}
	// Derive from project path.
	got := acpMirrorUISessionKey(`C:\work\demo`, "")
	if got == "" || got == "desktop-user" {
		t.Fatalf("expected project session key, got %q", got)
	}
	if got[:len("desktop-user:")] != "desktop-user:" {
		t.Fatalf("expected desktop-user: prefix, got %q", got)
	}
	// Empty path → main assistant.
	if got := acpMirrorUISessionKey("", ""); got != "desktop-user" {
		t.Fatalf("got %q", got)
	}
}
