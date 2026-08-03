package main

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestDevicePairingStoreConsumesCodeOnceAndExpires(t *testing.T) {
	store := newSrvDevicePairingStore()
	now := time.Date(2026, time.August, 2, 9, 0, 0, 0, time.UTC)
	principal := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	code, expiresAt, err := store.create(principal, now)
	if err != nil || !isSixDigitCode(code) || !expiresAt.Equal(now.Add(srvDevicePairingTTL)) {
		t.Fatalf("create() = code %q expires %s err %v", code, expiresAt, err)
	}
	got, ok := store.consume(code, now.Add(time.Minute))
	if !ok || got.Principal.TenantID != principal.TenantID || got.Principal.UserID != principal.UserID {
		t.Fatalf("first consume() = %#v, %v", got, ok)
	}
	if _, ok := store.consume(code, now.Add(time.Minute)); ok {
		t.Fatal("pairing code was accepted a second time")
	}
	code, _, err = store.create(principal, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.consume(code, now.Add(srvDevicePairingTTL)); ok {
		t.Fatal("expired pairing code was accepted")
	}
}

func TestDevicePairCodeFromTranscript(t *testing.T) {
	cases := map[string]string{
		"645432":          "645432",
		"请配对 六 四 五 四 三 二": "645432",
		"零幺两三四五":          "012345",
	}
	for input, want := range cases {
		got, ok := devicePairCodeFromTranscript(input)
		if !ok || got != want {
			t.Errorf("devicePairCodeFromTranscript(%q) = %q, %v; want %q, true", input, got, ok, want)
		}
	}
	if _, ok := devicePairCodeFromTranscript("六码 64 54 32 七"); ok {
		t.Error("accepted transcript with seven digits")
	}
}
