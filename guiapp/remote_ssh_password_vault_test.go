package guiapp

import (
	"testing"

	"github.com/zalando/go-keyring"
)

func TestCanonicalRemoteSSHPasswordKeyNormalizesHostAndPort(t *testing.T) {
	key, ok := canonicalRemoteSSHPasswordKey(" [Home.RapidAI.tech] ", "root", 55)
	if !ok || key != "root@home.rapidai.tech:55" {
		t.Fatalf("key=%q ok=%v", key, ok)
	}
	key, ok = canonicalRemoteSSHPasswordKey("db.example", "root", 0)
	if !ok || key != "root@db.example:22" {
		t.Fatalf("default port key=%q ok=%v", key, ok)
	}
	if _, ok := canonicalRemoteSSHPasswordKey("", "root", 22); ok {
		t.Fatal("empty host must not produce a key")
	}
	if shouldRememberRemoteSSHPassword(codingRequestInquiry) {
		t.Fatal("diagnosis passwords must not be remembered")
	}
	if !shouldRememberRemoteSSHPassword("") {
		t.Fatal("standard remote coding passwords should be remembered")
	}
}

func TestRemoteSSHPasswordKeyringRoundTripIsScopedByPort(t *testing.T) {
	keyring.MockInit()
	if err := rememberRemoteSSHPassword("Home.Example", "root", "port-22", 22); err != nil {
		t.Fatalf("remember 22: %v", err)
	}
	if err := rememberRemoteSSHPassword("home.example", "root", "port-2222", 2222); err != nil {
		t.Fatalf("remember 2222: %v", err)
	}
	got22, err := recallRemoteSSHPassword("[home.example]", "root", 22)
	if err != nil || got22 != "port-22" {
		t.Fatalf("recall 22=%q err=%v", got22, err)
	}
	got2222, err := recallRemoteSSHPassword("home.example", "root", 2222)
	if err != nil || got2222 != "port-2222" {
		t.Fatalf("recall 2222=%q err=%v", got2222, err)
	}
	if err := forgetRemoteSSHPassword("home.example", "root", 22); err != nil {
		t.Fatalf("forget: %v", err)
	}
	got22, err = recallRemoteSSHPassword("home.example", "root", 22)
	if err != nil || got22 != "" {
		t.Fatalf("forgotten password=%q err=%v", got22, err)
	}
	got2222, err = recallRemoteSSHPassword("home.example", "root", 2222)
	if err != nil || got2222 != "port-2222" {
		t.Fatalf("other port should remain=%q err=%v", got2222, err)
	}
	if err := rememberRemoteSSHPassword("home.example", "root", "   ", 22); err != nil {
		t.Fatalf("blank remember: %v", err)
	}
	if got, err := recallRemoteSSHPassword("home.example", "root", 22); err != nil || got != "" {
		t.Fatalf("blank password must not be stored, got %q err=%v", got, err)
	}
}
